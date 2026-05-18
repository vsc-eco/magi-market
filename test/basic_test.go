package contract_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

// ===================================
// Initialization Tests
// ===================================

func TestInitMarketplace(t *testing.T) {
	ct := SetupContractTest()
	InitToken(t, ct)
	InitNft(t, ct)

	payload := fmt.Sprintf(`{"feeBps":250,"feeRecipient":"%s"}`, feeRecipientAddress)
	_, _, logs := CallMarket(t, ct, "init", []byte(payload), nil, ownerAddress, "", true, gas, "")
	AssertEventEmitted(t, logs, "init_magi_market")
}

func TestInitMarketplaceAlreadyInitialized(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	payload := fmt.Sprintf(`{"feeBps":250,"feeRecipient":"%s"}`, feeRecipientAddress)
	CallMarket(t, ct, "init", []byte(payload), nil, ownerAddress, "", false, gas, "Already initialized")
}

func TestInitMarketplaceNonOwner(t *testing.T) {
	ct := SetupContractTest()
	InitToken(t, ct)
	InitNft(t, ct)

	payload := fmt.Sprintf(`{"feeBps":250,"feeRecipient":"%s"}`, feeRecipientAddress)
	CallMarket(t, ct, "init", []byte(payload), nil, "hive:alice", "", false, gas, "Only contract owner can initialize")
}

func TestInitMarketplaceNoPayload(t *testing.T) {
	ct := SetupContractTest()
	CallMarket(t, ct, "init", nil, nil, ownerAddress, "", false, gas, "Payload required")
}

func TestInitMarketplaceFeeTooHigh(t *testing.T) {
	ct := SetupContractTest()
	payload := `{"feeBps":10001,"feeRecipient":"hive:feerecipient"}`
	CallMarket(t, ct, "init", []byte(payload), nil, ownerAddress, "", false, gas, "Fee must be <= 10000")
}

func TestInitMarketplaceNoFeeRecipient(t *testing.T) {
	ct := SetupContractTest()
	payload := `{"feeBps":250,"feeRecipient":""}`
	CallMarket(t, ct, "init", []byte(payload), nil, ownerAddress, "", false, gas, "Fee recipient required")
}

func TestInitMarketplaceZeroFee(t *testing.T) {
	ct := SetupContractTest()
	InitToken(t, ct)
	InitNft(t, ct)

	payload := fmt.Sprintf(`{"feeBps":0,"feeRecipient":"%s"}`, feeRecipientAddress)
	CallMarket(t, ct, "init", []byte(payload), nil, ownerAddress, "", true, gas, "")
}

// ===================================
// Query Tests
// ===================================

func TestGetInfo(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	result, _, _ := CallMarket(t, ct, "getInfo", nil, nil, "hive:anyone", "", true, gas, "")
	info := ParseInfo(result)
	assert.Equal(t, ownerAddress, info.Owner)
	assert.Equal(t, uint64(250), info.FeeBps)
	assert.Equal(t, feeRecipientAddress, info.FeeRecipient)
	assert.False(t, info.Paused)
}

func TestGetOwner(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	result, _, _ := CallMarket(t, ct, "getOwner", nil, nil, "hive:anyone", "", true, gas, "")
	assert.Equal(t, ownerAddress, ParseOwner(result))
}

func TestIsPaused(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	result, _, _ := CallMarket(t, ct, "isPaused", nil, nil, "hive:anyone", "", true, gas, "")
	assert.False(t, ParsePaused(result))
}

// ===================================
// Admin Tests
// ===================================

func TestSetFee(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	CallMarket(t, ct, "setFee", []byte(`{"feeBps":500}`), nil, ownerAddress, "", true, gas, "")

	result, _, _ := CallMarket(t, ct, "getInfo", nil, nil, "hive:anyone", "", true, gas, "")
	info := ParseInfo(result)
	assert.Equal(t, uint64(500), info.FeeBps)
}

func TestSetFeeNonOwner(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	CallMarket(t, ct, "setFee", []byte(`{"feeBps":500}`), nil, "hive:alice", "", false, gas, "Only owner can set fee")
}

func TestSetFeeTooHigh(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	CallMarket(t, ct, "setFee", []byte(`{"feeBps":10001}`), nil, ownerAddress, "", false, gas, "Fee must be <= 10000")
}

func TestSetFeeRecipient(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	CallMarket(t, ct, "setFeeRecipient", []byte(`{"feeRecipient":"hive:newfeerecipient"}`), nil, ownerAddress, "", true, gas, "")

	result, _, _ := CallMarket(t, ct, "getInfo", nil, nil, "hive:anyone", "", true, gas, "")
	info := ParseInfo(result)
	assert.Equal(t, "hive:newfeerecipient", info.FeeRecipient)
}

func TestSetFeeRecipientNonOwner(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	CallMarket(t, ct, "setFeeRecipient", []byte(`{"feeRecipient":"hive:newfeerecipient"}`), nil, "hive:alice", "", false, gas, "Only owner can set fee recipient")
}

func TestSetFeeRecipientEmpty(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	CallMarket(t, ct, "setFeeRecipient", []byte(`{"feeRecipient":""}`), nil, ownerAddress, "", false, gas, "Fee recipient required")
}

func TestChangeOwner(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	_, _, logs := CallMarket(t, ct, "changeOwner", []byte(`{"newOwner":"hive:newowner"}`), nil, ownerAddress, "", true, gas, "")
	AssertEventEmitted(t, logs, "ownerTransferInitiated")

	_, _, logs2 := CallMarket(t, ct, "acceptOwnership", nil, nil, "hive:newowner", "", true, gas, "")
	AssertEventEmitted(t, logs2, "ownerChange")

	result, _, _ := CallMarket(t, ct, "getOwner", nil, nil, "hive:anyone", "", true, gas, "")
	assert.Equal(t, "hive:newowner", ParseOwner(result))
}

func TestChangeOwnerNonOwner(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	CallMarket(t, ct, "changeOwner", []byte(`{"newOwner":"hive:newowner"}`), nil, "hive:alice", "", false, gas, "Only owner can change owner")
}

func TestChangeOwnerEmpty(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	CallMarket(t, ct, "changeOwner", []byte(`{"newOwner":""}`), nil, ownerAddress, "", false, gas, "New owner address required")
}

// ===================================
// Pause Tests
// ===================================

func TestPause(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	_, _, logs := CallMarket(t, ct, "pause", nil, nil, ownerAddress, "", true, gas, "")
	AssertEventEmitted(t, logs, "paused")

	result, _, _ := CallMarket(t, ct, "isPaused", nil, nil, "hive:anyone", "", true, gas, "")
	assert.True(t, ParsePaused(result))
}

func TestPauseNonOwner(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	CallMarket(t, ct, "pause", nil, nil, "hive:alice", "", false, gas, "Only owner can pause")
}

func TestPauseAlreadyPaused(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	CallMarket(t, ct, "pause", nil, nil, ownerAddress, "", true, gas, "")
	CallMarket(t, ct, "pause", nil, nil, ownerAddress, "", false, gas, "Already paused")
}

func TestUnpause(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	CallMarket(t, ct, "pause", nil, nil, ownerAddress, "", true, gas, "")
	_, _, logs := CallMarket(t, ct, "unpause", nil, nil, ownerAddress, "", true, gas, "")
	AssertEventEmitted(t, logs, "unpaused")

	result, _, _ := CallMarket(t, ct, "isPaused", nil, nil, "hive:anyone", "", true, gas, "")
	assert.False(t, ParsePaused(result))
}

func TestUnpauseNonOwner(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	CallMarket(t, ct, "pause", nil, nil, ownerAddress, "", true, gas, "")
	CallMarket(t, ct, "unpause", nil, nil, "hive:alice", "", false, gas, "Only owner can unpause")
}

func TestUnpauseNotPaused(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	CallMarket(t, ct, "unpause", nil, nil, ownerAddress, "", false, gas, "Not paused")
}

// ===================================
// Queries Before Init
// ===================================

func TestGetInfoBeforeInit(t *testing.T) {
	ct := SetupContractTest()
	CallMarket(t, ct, "getInfo", nil, nil, "hive:anyone", "", false, gas, "")
}

func TestGetOwnerBeforeInit(t *testing.T) {
	ct := SetupContractTest()
	CallMarket(t, ct, "getOwner", nil, nil, "hive:anyone", "", false, gas, "")
}

func TestIsPausedBeforeInit(t *testing.T) {
	ct := SetupContractTest()
	CallMarket(t, ct, "isPaused", nil, nil, "hive:anyone", "", false, gas, "")
}
