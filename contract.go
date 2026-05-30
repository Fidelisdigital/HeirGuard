package contract

import (
	"encoding/binary"
	"math/rand"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/anypb"
)

var ContractConfig = &PluginConfig{
	Name:    "go_plugin_contract",
	Id:      1,
	Version: 1,
	SupportedTransactions: []string{
		"send",
		"faucet",
		"create_will",
		"reset_timer",
		"claim_will",
		"cancel_will",
	},
	TransactionTypeUrls: []string{
		"type.googleapis.com/types.MessageSend",
		"type.googleapis.com/types.MessageFaucet",
		"type.googleapis.com/types.MessageCreateWill",
		"type.googleapis.com/types.MessageResetTimer",
		"type.googleapis.com/types.MessageClaimWill",
		"type.googleapis.com/types.MessageCancelWill",
	},
	EventTypeUrls: nil,
}

func init() {
	file_account_proto_init()
	file_event_proto_init()
	file_plugin_proto_init()
	file_tx_proto_init()
	var fds [][]byte
	for _, file := range []protoreflect.FileDescriptor{
		anypb.File_google_protobuf_any_proto,
		File_account_proto, File_event_proto, File_plugin_proto, File_tx_proto,
	} {
		fd, _ := proto.Marshal(protodesc.ToFileDescriptorProto(file))
		fds = append(fds, fd)
	}
	ContractConfig.FileDescriptorProtos = fds
}

type Contract struct {
	Config        Config
	FSMConfig     *PluginFSMConfig
	plugin        *Plugin
	fsmId         uint64
	currentHeight uint64
}

func (c *Contract) Genesis(_ *PluginGenesisRequest) *PluginGenesisResponse {
	return &PluginGenesisResponse{}
}

func (c *Contract) BeginBlock(request *PluginBeginRequest) *PluginBeginResponse {
	c.currentHeight = request.Height
	return &PluginBeginResponse{}
}

func (c *Contract) EndBlock(_ *PluginEndRequest) *PluginEndResponse {
	return &PluginEndResponse{}
}

func (c *Contract) CheckTx(request *PluginCheckRequest) *PluginCheckResponse {
	resp, err := c.plugin.StateRead(c, &PluginStateReadRequest{
		Keys: []*PluginKeyRead{
			{QueryId: rand.Uint64(), Key: KeyForFeeParams()},
		}})
	if err == nil {
		err = resp.Error
	}
	if err != nil {
		return &PluginCheckResponse{Error: err}
	}
	minFees := new(FeeParams)
	if err = Unmarshal(resp.Results[0].Entries[0].Value, minFees); err != nil {
		return &PluginCheckResponse{Error: err}
	}
	if request.Tx.Fee < minFees.SendFee {
		return &PluginCheckResponse{Error: ErrTxFeeBelowStateLimit()}
	}
	msg, err := FromAny(request.Tx.Msg)
	if err != nil {
		return &PluginCheckResponse{Error: err}
	}
	switch x := msg.(type) {
	case *MessageSend:
		return c.CheckMessageSend(x)
	case *MessageFaucet:
		return c.CheckMessageFaucet(x)
	case *MessageCreateWill:
		return c.CheckMessageCreateWill(x)
	case *MessageResetTimer:
		return c.CheckMessageResetTimer(x)
	case *MessageClaimWill:
		return c.CheckMessageClaimWill(x)
	case *MessageCancelWill:
		return c.CheckMessageCancelWill(x)
	default:
		return &PluginCheckResponse{Error: ErrInvalidMessageCast()}
	}
}

func (c *Contract) DeliverTx(request *PluginDeliverRequest) *PluginDeliverResponse {
	msg, err := FromAny(request.Tx.Msg)
	if err != nil {
		return &PluginDeliverResponse{Error: err}
	}
	switch x := msg.(type) {
	case *MessageSend:
		return c.DeliverMessageSend(x, request.Tx.Fee)
	case *MessageFaucet:
		return c.DeliverMessageFaucet(x)
	case *MessageCreateWill:
		return c.DeliverMessageCreateWill(x, request.Tx.Fee)
	case *MessageResetTimer:
		return c.DeliverMessageResetTimer(x, request.Tx.Fee)
	case *MessageClaimWill:
		return c.DeliverMessageClaimWill(x, request.Tx.Fee)
	case *MessageCancelWill:
		return c.DeliverMessageCancelWill(x, request.Tx.Fee)
	default:
		return &PluginDeliverResponse{Error: ErrInvalidMessageCast()}
	}
}

func KeyForAccount(addr []byte) []byte {
	return JoinLenPrefix([]byte{0x01}, addr)
}

func KeyForWill(ownerAddress []byte) []byte {
	return JoinLenPrefix([]byte{0x08}, ownerAddress)
}

func KeyForFeePool(chainId uint64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, chainId)
	return JoinLenPrefix([]byte{0x02}, b)
}

func KeyForFeeParams() []byte {
	return JoinLenPrefix([]byte{0x07}, []byte("/f/"))
}

func (c *Contract) CheckMessageSend(msg *MessageSend) *PluginCheckResponse {
	if len(msg.FromAddress) != 20 || len(msg.ToAddress) != 20 {
		return &PluginCheckResponse{Error: ErrInvalidAddress()}
	}
	if msg.Amount == 0 {
		return &PluginCheckResponse{Error: ErrInvalidAmount()}
	}
	return &PluginCheckResponse{Recipient: msg.ToAddress, AuthorizedSigners: [][]byte{msg.FromAddress}}
}

func (c *Contract) CheckMessageFaucet(msg *MessageFaucet) *PluginCheckResponse {
	if len(msg.SignerAddress) != 20 {
		return &PluginCheckResponse{Error: ErrInvalidAddress()}
	}
	if len(msg.RecipientAddress) != 20 {
		return &PluginCheckResponse{Error: ErrInvalidAddress()}
	}
	if msg.Amount == 0 {
		return &PluginCheckResponse{Error: ErrInvalidAmount()}
	}
	return &PluginCheckResponse{
		Recipient:         msg.RecipientAddress,
		AuthorizedSigners: [][]byte{msg.SignerAddress},
	}
}

func (c *Contract) CheckMessageCreateWill(msg *MessageCreateWill) *PluginCheckResponse {
	if len(msg.OwnerAddress) != 20 {
		return &PluginCheckResponse{Error: ErrInvalidAddress()}
	}
	if len(msg.BeneficiaryAddress) != 20 {
		return &PluginCheckResponse{Error: ErrInvalidAddress()}
	}
	if msg.Amount == 0 {
		return &PluginCheckResponse{Error: ErrInvalidAmount()}
	}
	if msg.TimerBlocks < 100 {
		return &PluginCheckResponse{Error: &PluginError{Code: 13, Module: "will", Msg: "timer_blocks must be at least 100"}}
	}
	return &PluginCheckResponse{AuthorizedSigners: [][]byte{msg.OwnerAddress}}
}

func (c *Contract) CheckMessageResetTimer(msg *MessageResetTimer) *PluginCheckResponse {
	if len(msg.OwnerAddress) != 20 {
		return &PluginCheckResponse{Error: ErrInvalidAddress()}
	}
	return &PluginCheckResponse{AuthorizedSigners: [][]byte{msg.OwnerAddress}}
}

func (c *Contract) CheckMessageClaimWill(msg *MessageClaimWill) *PluginCheckResponse {
	if len(msg.OwnerAddress) != 20 {
		return &PluginCheckResponse{Error: ErrInvalidAddress()}
	}
	if len(msg.BeneficiaryAddress) != 20 {
		return &PluginCheckResponse{Error: ErrInvalidAddress()}
	}
	return &PluginCheckResponse{AuthorizedSigners: [][]byte{msg.BeneficiaryAddress}}
}

func (c *Contract) CheckMessageCancelWill(msg *MessageCancelWill) *PluginCheckResponse {
	if len(msg.OwnerAddress) != 20 {
		return &PluginCheckResponse{Error: ErrInvalidAddress()}
	}
	return &PluginCheckResponse{AuthorizedSigners: [][]byte{msg.OwnerAddress}}
}

func (c *Contract) DeliverMessageSend(msg *MessageSend, fee uint64) *PluginDeliverResponse {
	fromQId, toQId := rand.Uint64(), rand.Uint64()
	resp, err := c.plugin.StateRead(c, &PluginStateReadRequest{
		Keys: []*PluginKeyRead{
			{QueryId: fromQId, Key: KeyForAccount(msg.FromAddress)},
			{QueryId: toQId, Key: KeyForAccount(msg.ToAddress)},
		},
	})
	if err != nil {
		return &PluginDeliverResponse{Error: err}
	}
	if resp.Error != nil {
		return &PluginDeliverResponse{Error: resp.Error}
	}
	var from, to Account
	for _, r := range resp.Results {
		if len(r.Entries) == 0 {
			continue
		}
		if r.QueryId == fromQId {
			proto.Unmarshal(r.Entries[0].Value, &from)
		}
		if r.QueryId == toQId {
			proto.Unmarshal(r.Entries[0].Value, &to)
		}
	}
	total := msg.Amount + fee
	if from.Amount < total {
		return &PluginDeliverResponse{Error: ErrInsufficientFunds()}
	}
	from.Amount -= total
	to.Amount += msg.Amount
	fromBytes, _ := proto.Marshal(&from)
	toBytes, _ := proto.Marshal(&to)
	_, err = c.plugin.StateWrite(c, &PluginStateWriteRequest{
		Sets: []*PluginSetOp{
			{Key: KeyForAccount(msg.FromAddress), Value: fromBytes},
			{Key: KeyForAccount(msg.ToAddress), Value: toBytes},
		},
	})
	if err != nil {
		return &PluginDeliverResponse{Error: err}
	}
	return &PluginDeliverResponse{}
}

func (c *Contract) DeliverMessageFaucet(msg *MessageFaucet) *PluginDeliverResponse {
	recipientKey := KeyForAccount(msg.RecipientAddress)
	qId := rand.Uint64()
	resp, err := c.plugin.StateRead(c, &PluginStateReadRequest{
		Keys: []*PluginKeyRead{
			{QueryId: qId, Key: recipientKey},
		},
	})
	if err != nil {
		return &PluginDeliverResponse{Error: err}
	}
	if resp.Error != nil {
		return &PluginDeliverResponse{Error: resp.Error}
	}
	var recipient Account
	for _, r := range resp.Results {
		if r.QueryId == qId && len(r.Entries) > 0 {
			proto.Unmarshal(r.Entries[0].Value, &recipient)
		}
	}
	recipient.Amount += msg.Amount
	recipientBytes, _ := proto.Marshal(&recipient)
	_, err = c.plugin.StateWrite(c, &PluginStateWriteRequest{
		Sets: []*PluginSetOp{
			{Key: recipientKey, Value: recipientBytes},
		},
	})
	if err != nil {
		return &PluginDeliverResponse{Error: err}
	}
	return &PluginDeliverResponse{}
}

func (c *Contract) DeliverMessageCreateWill(msg *MessageCreateWill, fee uint64) *PluginDeliverResponse {
	qId := rand.Uint64()
	resp, err := c.plugin.StateRead(c, &PluginStateReadRequest{
		Keys: []*PluginKeyRead{
			{QueryId: qId, Key: KeyForAccount(msg.OwnerAddress)},
		},
	})
	if err != nil {
		return &PluginDeliverResponse{Error: err}
	}
	if resp.Error != nil {
		return &PluginDeliverResponse{Error: resp.Error}
	}
	var account Account
	for _, r := range resp.Results {
		if r.QueryId == qId && len(r.Entries) > 0 {
			proto.Unmarshal(r.Entries[0].Value, &account)
		}
	}
	total := msg.Amount + fee
	if account.Amount < total {
		return &PluginDeliverResponse{Error: ErrInsufficientFunds()}
	}
	account.Amount -= total
	accountBytes, _ := proto.Marshal(&account)
	will := &Will{
		OwnerAddress:       msg.OwnerAddress,
		BeneficiaryAddress: msg.BeneficiaryAddress,
		Amount:             msg.Amount,
		LockHeight:         c.currentHeight,
		TimerBlocks:        msg.TimerBlocks,
		LastResetHeight:    c.currentHeight,
		Message:            msg.Message,
		Claimed:            false,
		Cancelled:          false,
	}
	willBytes, _ := proto.Marshal(will)
	_, err = c.plugin.StateWrite(c, &PluginStateWriteRequest{
		Sets: []*PluginSetOp{
			{Key: KeyForAccount(msg.OwnerAddress), Value: accountBytes},
			{Key: KeyForWill(msg.OwnerAddress), Value: willBytes},
		},
	})
	if err != nil {
		return &PluginDeliverResponse{Error: err}
	}
	return &PluginDeliverResponse{}
}

func (c *Contract) DeliverMessageResetTimer(msg *MessageResetTimer, fee uint64) *PluginDeliverResponse {
	willQId, accountQId := rand.Uint64(), rand.Uint64()
	resp, err := c.plugin.StateRead(c, &PluginStateReadRequest{
		Keys: []*PluginKeyRead{
			{QueryId: willQId, Key: KeyForWill(msg.OwnerAddress)},
			{QueryId: accountQId, Key: KeyForAccount(msg.OwnerAddress)},
		},
	})
	if err != nil {
		return &PluginDeliverResponse{Error: err}
	}
	if resp.Error != nil {
		return &PluginDeliverResponse{Error: resp.Error}
	}
	var will Will
	var account Account
	for _, r := range resp.Results {
		if len(r.Entries) == 0 {
			continue
		}
		if r.QueryId == willQId {
			proto.Unmarshal(r.Entries[0].Value, &will)
		}
		if r.QueryId == accountQId {
			proto.Unmarshal(r.Entries[0].Value, &account)
		}
	}
	if will.Claimed || will.Cancelled {
		return &PluginDeliverResponse{Error: &PluginError{Code: 15, Module: "will", Msg: "will is not active"}}
	}
	if account.Amount < fee {
		return &PluginDeliverResponse{Error: ErrInsufficientFunds()}
	}
	account.Amount -= fee
	will.LastResetHeight = c.currentHeight
	willBytes, _ := proto.Marshal(&will)
	accountBytes, _ := proto.Marshal(&account)
	_, err = c.plugin.StateWrite(c, &PluginStateWriteRequest{
		Sets: []*PluginSetOp{
			{Key: KeyForWill(msg.OwnerAddress), Value: willBytes},
			{Key: KeyForAccount(msg.OwnerAddress), Value: accountBytes},
		},
	})
	if err != nil {
		return &PluginDeliverResponse{Error: err}
	}
	return &PluginDeliverResponse{}
}

func (c *Contract) DeliverMessageClaimWill(msg *MessageClaimWill, fee uint64) *PluginDeliverResponse {
	willQId, benefQId := rand.Uint64(), rand.Uint64()
	resp, err := c.plugin.StateRead(c, &PluginStateReadRequest{
		Keys: []*PluginKeyRead{
			{QueryId: willQId, Key: KeyForWill(msg.OwnerAddress)},
			{QueryId: benefQId, Key: KeyForAccount(msg.BeneficiaryAddress)},
		},
	})
	if err != nil {
		return &PluginDeliverResponse{Error: err}
	}
	if resp.Error != nil {
		return &PluginDeliverResponse{Error: resp.Error}
	}
	var will Will
	var beneficiary Account
	for _, r := range resp.Results {
		if len(r.Entries) == 0 {
			continue
		}
		if r.QueryId == willQId {
			proto.Unmarshal(r.Entries[0].Value, &will)
		}
		if r.QueryId == benefQId {
			proto.Unmarshal(r.Entries[0].Value, &beneficiary)
		}
	}
	if will.Claimed || will.Cancelled {
		return &PluginDeliverResponse{Error: &PluginError{Code: 15, Module: "will", Msg: "will is not active"}}
	}
	if c.currentHeight < will.LockHeight+will.TimerBlocks {
		return &PluginDeliverResponse{Error: &PluginError{Code: 16, Module: "will", Msg: "timer not expired yet"}}
	}
	beneficiary.Amount += will.Amount
	will.Claimed = true
	beneficiaryBytes, _ := proto.Marshal(&beneficiary)
	willBytes, _ := proto.Marshal(&will)
	_, err = c.plugin.StateWrite(c, &PluginStateWriteRequest{
		Sets: []*PluginSetOp{
			{Key: KeyForAccount(msg.BeneficiaryAddress), Value: beneficiaryBytes},
			{Key: KeyForWill(msg.OwnerAddress), Value: willBytes},
		},
	})
	if err != nil {
		return &PluginDeliverResponse{Error: err}
	}
	return &PluginDeliverResponse{}
}

func (c *Contract) DeliverMessageCancelWill(msg *MessageCancelWill, fee uint64) *PluginDeliverResponse {
	willQId, accountQId := rand.Uint64(), rand.Uint64()
	resp, err := c.plugin.StateRead(c, &PluginStateReadRequest{
		Keys: []*PluginKeyRead{
			{QueryId: willQId, Key: KeyForWill(msg.OwnerAddress)},
			{QueryId: accountQId, Key: KeyForAccount(msg.OwnerAddress)},
		},
	})
	if err != nil {
		return &PluginDeliverResponse{Error: err}
	}
	if resp.Error != nil {
		return &PluginDeliverResponse{Error: resp.Error}
	}
	var will Will
	var account Account
	for _, r := range resp.Results {
		if len(r.Entries) == 0 {
			continue
		}
		if r.QueryId == willQId {
			proto.Unmarshal(r.Entries[0].Value, &will)
		}
		if r.QueryId == accountQId {
			proto.Unmarshal(r.Entries[0].Value, &account)
		}
	}
	if will.Claimed || will.Cancelled {
		return &PluginDeliverResponse{Error: &PluginError{Code: 15, Module: "will", Msg: "will is not active"}}
	}
	if account.Amount < fee {
		return &PluginDeliverResponse{Error: ErrInsufficientFunds()}
	}
	account.Amount += will.Amount - fee
	will.Cancelled = true
	accountBytes, _ := proto.Marshal(&account)
	willBytes, _ := proto.Marshal(&will)
	_, err = c.plugin.StateWrite(c, &PluginStateWriteRequest{
		Sets: []*PluginSetOp{
			{Key: KeyForAccount(msg.OwnerAddress), Value: accountBytes},
			{Key: KeyForWill(msg.OwnerAddress), Value: willBytes},
		},
	})
	if err != nil {
		return &PluginDeliverResponse{Error: err}
	}
	return &PluginDeliverResponse{}
}
