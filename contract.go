package contract

import (
	"math/rand"

	"google.golang.org/protobuf/proto"
)

type Contract struct {
	Config        Config
	FSMConfig     *PluginFSMConfig
	plugin        *Plugin
	fsmId         uint64
	currentHeight uint64
}

var ContractConfig = &PluginConfig{
	Name:                 "go_plugin_contract",
	Id:                   1,
	Version:              1,
	SupportedTransactions: []string{
		"send",
		"create_will",
		"reset_timer",
		"claim_will",
		"cancel_will",
	},
	TransactionTypeUrls: []string{
		"type.googleapis.com/types.MessageSend",
		"type.googleapis.com/types.MessageCreateWill",
		"type.googleapis.com/types.MessageResetTimer",
		"type.googleapis.com/types.MessageClaimWill",
		"type.googleapis.com/types.MessageCancelWill",
	},
}

func (c *Contract) Genesis(request *PluginGenesisRequest) *PluginGenesisResponse {
	return &PluginGenesisResponse{}
}

func (c *Contract) BeginBlock(request *PluginBeginRequest) *PluginBeginResponse {
	c.currentHeight = request.Height
	return &PluginBeginResponse{}
}

func (c *Contract) EndBlock(request *PluginEndRequest) *PluginEndResponse {
	return &PluginEndResponse{}
}

func (c *Contract) CheckTx(request *PluginCheckRequest) *PluginCheckResponse {
	msg, err := FromAny(request.Tx.Msg)
	if err != nil {
		return &PluginCheckResponse{Error: err}
	}

	switch x := msg.(type) {
	case *MessageSend:
		return c.CheckMessageSend(x)
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

func KeyForAccount(address []byte) []byte {
	return JoinLenPrefix([]byte{0x01}, address)
}

func KeyForWill(ownerAddress []byte) []byte {
	return JoinLenPrefix([]byte{0x08}, ownerAddress)
}

func (c *Contract) CheckMessageSend(msg *MessageSend) *PluginCheckResponse {
	if len(msg.FromAddress) != 20 || len(msg.ToAddress) != 20 {
		return &PluginCheckResponse{Error: ErrInvalidAddress()}
	}
	if msg.Amount == 0 {
		return &PluginCheckResponse{Error: ErrInvalidAmount()}
	}
	return &PluginCheckResponse{AuthorizedSigners: [][]byte{msg.FromAddress}}
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
	fromQId := rand.Uint64()
	toQId := rand.Uint64()

	resp, err := c.plugin.StateRead(c, &PluginStateReadRequest{
		Keys: []*PluginKeyRead{
			{QueryId: fromQId, Key: KeyForAccount(msg.FromAddress)},
			{QueryId: toQId, Key: KeyForAccount(msg.ToAddress)},
		},
	})
	if err != nil {
		return &PluginDeliverResponse{Error: err}
	}

	var fromAccount, toAccount Account
	for _, r := range resp.Results {
		if r.QueryId == fromQId && len(r.Entries) > 0 {
			proto.Unmarshal(r.Entries[0].Value, &fromAccount)
		}
		if r.QueryId == toQId && len(r.Entries) > 0 {
			proto.Unmarshal(r.Entries[0].Value, &toAccount)
		}
	}

	if fromAccount.Amount < msg.Amount {
		return &PluginDeliverResponse{Error: ErrInsufficientFunds()}
	}

	fromAccount.Amount -= msg.Amount
	toAccount.Amount += msg.Amount

	fromBytes, _ := proto.Marshal(&fromAccount)
	toBytes, _ := proto.Marshal(&toAccount)

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

func (c *Contract) DeliverMessageCreateWill(msg *MessageCreateWill, fee uint64) *PluginDeliverResponse {
	accountQId := rand.Uint64()
	resp, err := c.plugin.StateRead(c, &PluginStateReadRequest{
		Keys: []*PluginKeyRead{
			{QueryId: accountQId, Key: KeyForAccount(msg.OwnerAddress)},
		},
	})
	if err != nil {
		return &PluginDeliverResponse{Error: err}
	}

	var account Account
	for _, r := range resp.Results {
		if r.QueryId == accountQId && len(r.Entries) > 0 {
			proto.Unmarshal(r.Entries[0].Value, &account)
		}
	}

	if account.Amount < msg.Amount {
		return &PluginDeliverResponse{Error: ErrInsufficientFunds()}
	}

	account.Amount -= msg.Amount
	accountBytes, _ := proto.Marshal(&account)

	will := &Will{
		OwnerAddress:       msg.OwnerAddress,
		BeneficiaryAddress: msg.BeneficiaryAddress,
		Amount:             msg.Amount,
		LockHeight:         c.currentHeight,
		TimerBlocks:        msg.TimerBlocks,
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
	willQId := rand.Uint64()
	resp, err := c.plugin.StateRead(c, &PluginStateReadRequest{
		Keys: []*PluginKeyRead{
			{QueryId: willQId, Key: KeyForWill(msg.OwnerAddress)},
		},
	})
	if err != nil {
		return &PluginDeliverResponse{Error: err}
	}

	var will Will
	for _, r := range resp.Results {
		if r.QueryId == willQId && len(r.Entries) > 0 {
			proto.Unmarshal(r.Entries[0].Value, &will)
		}
	}

	if will.Claimed || will.Cancelled {
		return &PluginDeliverResponse{Error: &PluginError{Code: 15, Module: "will", Msg: "will is not active"}}
	}

	will.LastResetHeight = c.currentHeight
	willBytes, _ := proto.Marshal(&will)

	_, err = c.plugin.StateWrite(c, &PluginStateWriteRequest{
		Sets: []*PluginSetOp{
			{Key: KeyForWill(msg.OwnerAddress), Value: willBytes},
		},
	})
	if err != nil {
		return &PluginDeliverResponse{Error: err}
	}
	return &PluginDeliverResponse{}
}

func (c *Contract) DeliverMessageClaimWill(msg *MessageClaimWill, fee uint64) *PluginDeliverResponse {
	willQId := rand.Uint64()
	beneficiaryQId := rand.Uint64()
	resp, err := c.plugin.StateRead(c, &PluginStateReadRequest{
		Keys: []*PluginKeyRead{
			{QueryId: willQId, Key: KeyForWill(msg.OwnerAddress)},
			{QueryId: beneficiaryQId, Key: KeyForAccount(msg.BeneficiaryAddress)},
		},
	})
	if err != nil {
		return &PluginDeliverResponse{Error: err}
	}

	var will Will
	var beneficiary Account
	for _, r := range resp.Results {
		if r.QueryId == willQId && len(r.Entries) > 0 {
			proto.Unmarshal(r.Entries[0].Value, &will)
		}
		if r.QueryId == beneficiaryQId && len(r.Entries) > 0 {
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
	willQId := rand.Uint64()
	accountQId := rand.Uint64()
	resp, err := c.plugin.StateRead(c, &PluginStateReadRequest{
		Keys: []*PluginKeyRead{
			{QueryId: willQId, Key: KeyForWill(msg.OwnerAddress)},
			{QueryId: accountQId, Key: KeyForAccount(msg.OwnerAddress)},
		},
	})
	if err != nil {
		return &PluginDeliverResponse{Error: err}
	}

	var will Will
	var account Account
	for _, r := range resp.Results {
		if r.QueryId == willQId && len(r.Entries) > 0 {
			proto.Unmarshal(r.Entries[0].Value, &will)
		}
		if r.QueryId == accountQId && len(r.Entries) > 0 {
			proto.Unmarshal(r.Entries[0].Value, &account)
		}
	}

	if will.Claimed || will.Cancelled {
		return &PluginDeliverResponse{Error: &PluginError{Code: 15, Module: "will", Msg: "will is not active"}}
	}

	account.Amount += will.Amount
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
