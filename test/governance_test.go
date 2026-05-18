package contract_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"

	"vsc-node/lib/test_utils"
)

func ParsePendingOwner(result test_utils.ContractTestCallResult) string {
	var resp struct {
		PendingOwner string `json:"pendingOwner"`
	}
	json.Unmarshal([]byte(result.Ret), &resp)
	return resp.PendingOwner
}

func TestTwoStepTransferProposeDoesNotMoveOwner(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	_, _, logs := CallMarket(t, ct, "changeOwner", []byte(`{"newOwner":"hive:newowner"}`), nil, ownerAddress, "", true, gas, "")
	AssertEventEmitted(t, logs, "ownerTransferInitiated")

	res, _, _ := CallMarket(t, ct, "getOwner", nil, nil, "hive:anyone", "", true, gas, "")
	assert.Equal(t, ownerAddress, ParseOwner(res), "owner unchanged until accepted")

	pend, _, _ := CallMarket(t, ct, "getPendingOwner", nil, nil, "hive:anyone", "", true, gas, "")
	assert.Equal(t, "hive:newowner", ParsePendingOwner(pend))
}

func TestTwoStepTransferAcceptByNonPendingRejected(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	CallMarket(t, ct, "changeOwner", []byte(`{"newOwner":"hive:newowner"}`), nil, ownerAddress, "", true, gas, "")
	CallMarket(t, ct, "acceptOwnership", nil, nil, "hive:someoneelse", "", false, gas, "Not the pending owner")
}

func TestTwoStepTransferAcceptFinalizes(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	CallMarket(t, ct, "changeOwner", []byte(`{"newOwner":"hive:newowner"}`), nil, ownerAddress, "", true, gas, "")
	_, _, logs := CallMarket(t, ct, "acceptOwnership", nil, nil, "hive:newowner", "", true, gas, "")
	AssertEventEmitted(t, logs, "ownerChange")

	res, _, _ := CallMarket(t, ct, "getOwner", nil, nil, "hive:anyone", "", true, gas, "")
	assert.Equal(t, "hive:newowner", ParseOwner(res))

	pend, _, _ := CallMarket(t, ct, "getPendingOwner", nil, nil, "hive:anyone", "", true, gas, "")
	assert.Equal(t, "", ParsePendingOwner(pend), "pending cleared after accept")

	// new owner can admin; old owner cannot
	CallMarket(t, ct, "setFee", []byte(`{"feeBps":500}`), nil, "hive:newowner", "", true, gas, "")
	CallMarket(t, ct, "setFee", []byte(`{"feeBps":500}`), nil, ownerAddress, "", false, gas, "Only owner can set fee")
}

func TestTwoStepTransferAcceptNoPendingRejected(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	CallMarket(t, ct, "acceptOwnership", nil, nil, "hive:newowner", "", false, gas, "No pending ownership transfer")
}

func TestTwoStepTransferCancel(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	CallMarket(t, ct, "changeOwner", []byte(`{"newOwner":"hive:newowner"}`), nil, ownerAddress, "", true, gas, "")
	_, _, logs := CallMarket(t, ct, "cancelOwnershipTransfer", nil, nil, ownerAddress, "", true, gas, "")
	AssertEventEmitted(t, logs, "ownerTransferCancelled")
	pend, _, _ := CallMarket(t, ct, "getPendingOwner", nil, nil, "hive:anyone", "", true, gas, "")
	assert.Equal(t, "", ParsePendingOwner(pend))
	CallMarket(t, ct, "acceptOwnership", nil, nil, "hive:newowner", "", false, gas, "No pending ownership transfer")
}

func TestTwoStepTransferWorksWhilePaused(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	CallMarket(t, ct, "pause", nil, nil, ownerAddress, "", true, gas, "")
	CallMarket(t, ct, "changeOwner", []byte(`{"newOwner":"hive:newowner"}`), nil, ownerAddress, "", true, gas, "")
	CallMarket(t, ct, "acceptOwnership", nil, nil, "hive:newowner", "", true, gas, "")
	res, _, _ := CallMarket(t, ct, "getOwner", nil, nil, "hive:anyone", "", true, gas, "")
	assert.Equal(t, "hive:newowner", ParseOwner(res))
}
