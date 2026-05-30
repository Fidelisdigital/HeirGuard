package main

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/canopy-network/go-plugin/tutorial/contract"
	"github.com/canopy-network/go-plugin/tutorial/crypto"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

func TestHeirGuardTransactions(t *testing.T) {
	queryRPCURL := "http://localhost:50002"
	adminRPCURL := "http://localhost:50003"
	networkID := uint64(1)
	chainID := uint64(1)
	testPassword := "testpassword123"

	t.Log("Step 1: Creating owner and heir accounts...")
	suffix := randomSuffix()
	ownerAddr, err := keystoreNewKey(adminRPCURL, "hg_owner_"+suffix, testPassword)
	if err != nil {
		t.Fatalf("Failed to create owner account: %v", err)
	}
	t.Logf("Owner address: %s", ownerAddr)

	heirAddr, err := keystoreNewKey(adminRPCURL, "hg_heir_"+suffix, testPassword)
	if err != nil {
		t.Fatalf("Failed to create heir account: %v", err)
	}
	t.Logf("Heir address: %s", heirAddr)

	ownerKey, err := keystoreGetKey(adminRPCURL, ownerAddr, testPassword)
	if err != nil {
		t.Fatalf("Failed to get owner key: %v", err)
	}
	heirKey, err := keystoreGetKey(adminRPCURL, heirAddr, testPassword)
	if err != nil {
		t.Fatalf("Failed to get heir key: %v", err)
	}

	height, err := getHeight(queryRPCURL)
	if err != nil {
		t.Fatalf("Failed to get height: %v", err)
	}
	t.Logf("Current height: %d", height)

	t.Log("Step 2: Funding owner via faucet...")
	faucetTxHash, err := sendFaucetTx(queryRPCURL, ownerKey, ownerAddr, 1000000000, 10000, networkID, chainID, height)
	if err != nil {
		t.Fatalf("Failed to send faucet tx: %v", err)
	}
	t.Logf("Faucet tx: %s", faucetTxHash)
	included, err := waitForTxInclusion(queryRPCURL, ownerAddr, faucetTxHash, 60*time.Second)
	if err != nil || !included {
		t.Fatalf("Faucet tx not confirmed: %v", err)
	}
	t.Log("Faucet confirmed!")
	bal, _ := getAccountBalance(queryRPCURL, ownerAddr)
	t.Logf("Owner balance after faucet: %d", bal)

	t.Log("Step 3: Creating a will (create_will)...")
	height, _ = getHeight(queryRPCURL)
	createWillHash, err := sendCreateWillTx(queryRPCURL, ownerKey, ownerAddr, heirAddr, 500000000, 100, "Take care of the family. The seed phrase is in the red envelope.", 10000, networkID, chainID, height)
	if err != nil {
		t.Fatalf("Failed to send create_will tx: %v", err)
	}
	t.Logf("CreateWill tx: %s", createWillHash)
	included, err = waitForTxInclusion(queryRPCURL, ownerAddr, createWillHash, 60*time.Second)
	if err != nil || !included {
		t.Fatalf("CreateWill tx not confirmed: %v", err)
	}
	t.Log("CreateWill confirmed!")
	bal, _ = getAccountBalance(queryRPCURL, ownerAddr)
	t.Logf("Owner balance after create_will: %d", bal)

	failedCount, _ := checkTxNotFailed(queryRPCURL, ownerAddr)
	if failedCount > 0 {
		t.Fatalf("Owner has %d failed transactions after create_will", failedCount)
	}

	t.Log("Step 4: Resetting timer (reset_timer)...")
	height, _ = getHeight(queryRPCURL)
	resetTimerHash, err := sendResetTimerTx(queryRPCURL, ownerKey, ownerAddr, 10000, networkID, chainID, height)
	if err != nil {
		t.Fatalf("Failed to send reset_timer tx: %v", err)
	}
	t.Logf("ResetTimer tx: %s", resetTimerHash)
	included, err = waitForTxInclusion(queryRPCURL, ownerAddr, resetTimerHash, 60*time.Second)
	if err != nil || !included {
		t.Fatalf("ResetTimer tx not confirmed: %v", err)
	}
	t.Log("ResetTimer confirmed!")

	t.Log("Step 5: Cancelling the will (cancel_will)...")
	height, _ = getHeight(queryRPCURL)
	cancelWillHash, err := sendCancelWillTx(queryRPCURL, ownerKey, ownerAddr, 10000, networkID, chainID, height)
	if err != nil {
		t.Fatalf("Failed to send cancel_will tx: %v", err)
	}
	t.Logf("CancelWill tx: %s", cancelWillHash)
	included, err = waitForTxInclusion(queryRPCURL, ownerAddr, cancelWillHash, 60*time.Second)
	if err != nil || !included {
		t.Fatalf("CancelWill tx not confirmed: %v", err)
	}
	t.Log("CancelWill confirmed!")
	bal, _ = getAccountBalance(queryRPCURL, ownerAddr)
	t.Logf("Owner balance after cancel: %d", bal)

	t.Log("Step 6: Creating new owner account for claim test...")
	suffix2 := randomSuffix()
	owner2Addr, err := keystoreNewKey(adminRPCURL, "hg_owner2_"+suffix2, testPassword)
	if err != nil {
		t.Fatalf("Failed to create owner2 account: %v", err)
	}
	t.Logf("Owner2 address: %s", owner2Addr)

	owner2Key, err := keystoreGetKey(adminRPCURL, owner2Addr, testPassword)
	if err != nil {
		t.Fatalf("Failed to get owner2 key: %v", err)
	}

	height, _ = getHeight(queryRPCURL)
	faucet2Hash, err := sendFaucetTx(queryRPCURL, owner2Key, owner2Addr, 1000000000, 10000, networkID, chainID, height)
	if err != nil {
		t.Fatalf("Failed to fund owner2: %v", err)
	}
	t.Logf("Faucet2 tx: %s", faucet2Hash)
	included, err = waitForTxInclusion(queryRPCURL, owner2Addr, faucet2Hash, 60*time.Second)
	if err != nil || !included {
		t.Fatalf("Faucet2 tx not confirmed: %v", err)
	}
	t.Log("Owner2 funded!")

	t.Log("Creating will for claim test (timer=100 blocks)...")
	height, _ = getHeight(queryRPCURL)
	createWill2Hash, err := sendCreateWillTx(queryRPCURL, owner2Key, owner2Addr, heirAddr, 200000000, 100, "Second will for claim test.", 10000, networkID, chainID, height)
	if err != nil {
		t.Fatalf("Failed to send second create_will tx: %v", err)
	}
	t.Logf("CreateWill2 tx: %s", createWill2Hash)
	included, err = waitForTxInclusion(queryRPCURL, owner2Addr, createWill2Hash, 60*time.Second)
	if err != nil || !included {
		t.Fatalf("CreateWill2 tx not confirmed: %v", err)
	}
	t.Log("CreateWill2 confirmed!")

	t.Log("Waiting for timer to expire (100 blocks, polling every 30s)...")
	startHeight, _ := getHeight(queryRPCURL)
	for {
		current, _ := getHeight(queryRPCURL)
		remaining := int64(startHeight) + 100 - int64(current)
		if remaining <= 0 {
			break
		}
		t.Logf("Block %d - need %d more blocks...", current, remaining)
		time.Sleep(30 * time.Second)
	}
	t.Log("Timer expired!")

	t.Log("Step 7: Heir claiming the will (claim_will)...")
	height, _ = getHeight(queryRPCURL)
	claimWillHash, err := sendClaimWillTx(queryRPCURL, heirKey, owner2Addr, heirAddr, 10000, networkID, chainID, height)
	if err != nil {
		t.Fatalf("Failed to send claim_will tx: %v", err)
	}
	t.Logf("ClaimWill tx: %s", claimWillHash)
	included, err = waitForTxInclusion(queryRPCURL, heirAddr, claimWillHash, 60*time.Second)
	if err != nil || !included {
		t.Fatalf("ClaimWill tx not confirmed: %v", err)
	}
	t.Log("ClaimWill confirmed!")

	heirBal, _ := getAccountBalance(queryRPCURL, heirAddr)
	ownerBal, _ := getAccountBalance(queryRPCURL, ownerAddr)
	t.Logf("Final owner balance: %d", ownerBal)
	t.Logf("Final heir balance:  %d", heirBal)

	if heirBal == 0 {
		t.Fatal("FAIL: Heir balance should be > 0 after claiming will")
	}

	t.Log("")
	t.Log("All HeirGuard transactions confirmed successfully!")
	t.Log("   create_will OK  reset_timer OK  cancel_will OK  claim_will OK")
}

func sendCreateWillTx(rpcURL string, signerKey *keyGroup, ownerAddr, beneficiaryAddr string, amount, timerBlocks uint64, message string, fee, networkID, chainID, height uint64) (string, error) {
	ownerBytes, _ := hex.DecodeString(ownerAddr)
	benefBytes, _ := hex.DecodeString(beneficiaryAddr)
	msgProto := &contract.MessageCreateWill{
		OwnerAddress:       ownerBytes,
		BeneficiaryAddress: benefBytes,
		Amount:             amount,
		TimerBlocks:        timerBlocks,
		Message:            message,
	}
	return buildAndSendHGTx(rpcURL, signerKey, "create_will", "type.googleapis.com/types.MessageCreateWill", msgProto, fee, networkID, chainID, height)
}

func sendResetTimerTx(rpcURL string, signerKey *keyGroup, ownerAddr string, fee, networkID, chainID, height uint64) (string, error) {
	ownerBytes, _ := hex.DecodeString(ownerAddr)
	msgProto := &contract.MessageResetTimer{
		OwnerAddress: ownerBytes,
	}
	return buildAndSendHGTx(rpcURL, signerKey, "reset_timer", "type.googleapis.com/types.MessageResetTimer", msgProto, fee, networkID, chainID, height)
}

func sendClaimWillTx(rpcURL string, signerKey *keyGroup, ownerAddr, beneficiaryAddr string, fee, networkID, chainID, height uint64) (string, error) {
	ownerBytes, _ := hex.DecodeString(ownerAddr)
	benefBytes, _ := hex.DecodeString(beneficiaryAddr)
	msgProto := &contract.MessageClaimWill{
		OwnerAddress:       ownerBytes,
		BeneficiaryAddress: benefBytes,
	}
	return buildAndSendHGTx(rpcURL, signerKey, "claim_will", "type.googleapis.com/types.MessageClaimWill", msgProto, fee, networkID, chainID, height)
}

func sendCancelWillTx(rpcURL string, signerKey *keyGroup, ownerAddr string, fee, networkID, chainID, height uint64) (string, error) {
	ownerBytes, _ := hex.DecodeString(ownerAddr)
	msgProto := &contract.MessageCancelWill{
		OwnerAddress: ownerBytes,
	}
	return buildAndSendHGTx(rpcURL, signerKey, "cancel_will", "type.googleapis.com/types.MessageCancelWill", msgProto, fee, networkID, chainID, height)
}

func buildAndSendHGTx(rpcURL string, signerKey *keyGroup, msgType, typeURL string, msgProto proto.Message, fee, networkID, chainID, height uint64) (string, error) {
	txTime := uint64(time.Now().UnixMicro())

	msgBytes, err := proto.Marshal(msgProto)
	if err != nil {
		return "", fmt.Errorf("failed to marshal message: %v", err)
	}

	msgAny := &anypb.Any{
		TypeUrl: typeURL,
		Value:   msgBytes,
	}

	signBytes, err := crypto.GetSignBytes(msgType, msgAny, txTime, height, fee, "", networkID, chainID)
	if err != nil {
		return "", fmt.Errorf("failed to get sign bytes: %v", err)
	}

	privKey, err := crypto.StringToBLS12381PrivateKey(signerKey.PrivateKey)
	if err != nil {
		return "", fmt.Errorf("failed to parse private key: %v", err)
	}

	signature := privKey.Sign(signBytes)

	pubKeyBytes, err := hex.DecodeString(signerKey.PublicKey)
	if err != nil {
		return "", fmt.Errorf("failed to decode public key: %v", err)
	}

	tx := map[string]interface{}{
		"type":       msgType,
		"msgTypeUrl": typeURL,
		"msgBytes":   hex.EncodeToString(msgBytes),
		"signature": map[string]string{
			"publicKey": hex.EncodeToString(pubKeyBytes),
			"signature": hex.EncodeToString(signature),
		},
		"time":          txTime,
		"createdHeight": height,
		"fee":           fee,
		"memo":          "",
		"networkID":     networkID,
		"chainID":       chainID,
	}

	txJSONBytes, err := json.MarshalIndent(tx, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal tx: %v", err)
	}

	respBody, err := postRawJSON(rpcURL+"/v1/tx", string(txJSONBytes))
	if err != nil {
		return "", fmt.Errorf("failed to send tx: %v", err)
	}

	var txHash string
	if err := json.Unmarshal(respBody, &txHash); err != nil {
		return "", fmt.Errorf("failed to parse response: %v, body: %s", err, string(respBody))
	}

	return txHash, nil
}

var _ = base64.StdEncoding
