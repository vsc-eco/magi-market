package contract_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

// ===================================
// B1: Royalty Splits Tests
// ===================================

type RoyaltySplitResult struct {
	Recipient string `json:"recipient"`
	Bps       uint64 `json:"bps"`
}

type RoyaltySplitsResult struct {
	NftContract string               `json:"nftContract"`
	Splits      []RoyaltySplitResult `json:"splits"`
}

func ParseRoyaltySplits(result interface{ GetRet() string }) RoyaltySplitsResult {
	var resp RoyaltySplitsResult
	json.Unmarshal([]byte(getRet(result)), &resp)
	return resp
}

// ContractTestCallResultRet is a small adapter since ContractTestCallResult has a Ret field.
type retHolder struct{ ret string }

func getRet(v interface{}) string {
	// Direct type switch — the result from CallMarket is test_utils.ContractTestCallResult
	// which has a Ret string field. Use json round-trip via the struct returned by callContract.
	// Actually we just pass the ret string directly as a helper below.
	return ""
}

func ParseRoyaltySplitsFromRet(ret string) RoyaltySplitsResult {
	var resp RoyaltySplitsResult
	json.Unmarshal([]byte(ret), &resp)
	return resp
}

func TestSetRoyaltySplits(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	// ownerAddress is the collection owner (NFT contract owner = ownerAddress = "hive:tibfox")
	// Set 3 splits: 250/150/100 bps to hive:a / hive:b / hive:c
	payload := fmt.Sprintf(`{"nftContract":"%s","splits":[{"recipient":"hive:a","bps":250},{"recipient":"hive:b","bps":150},{"recipient":"hive:c","bps":100}]}`, NftContractID)
	CallMarket(t, ct, "setRoyaltySplits", []byte(payload), nil, ownerAddress, "", true, gas, "")

	// getRoyaltySplits should return them in order
	getResult, _, _ := CallMarket(t, ct, "getRoyaltySplits", []byte(fmt.Sprintf(`{"nftContract":"%s"}`, NftContractID)), nil, "hive:anyone", "", true, gas, "")
	splits := ParseRoyaltySplitsFromRet(getResult.Ret)
	assert.Equal(t, NftContractID, splits.NftContract)
	assert.Equal(t, 3, len(splits.Splits))
	assert.Equal(t, "hive:a", splits.Splits[0].Recipient)
	assert.Equal(t, uint64(250), splits.Splits[0].Bps)
	assert.Equal(t, "hive:b", splits.Splits[1].Recipient)
	assert.Equal(t, uint64(150), splits.Splits[1].Bps)
	assert.Equal(t, "hive:c", splits.Splits[2].Recipient)
	assert.Equal(t, uint64(100), splits.Splits[2].Bps)

	// Non-collection-owner rejected
	CallMarket(t, ct, "setRoyaltySplits", []byte(payload), nil, "hive:notowner", "", false, gas, "Only collection owner can set royalty")

	// 0 splits rejected
	zeroPayload := fmt.Sprintf(`{"nftContract":"%s","splits":[]}`, NftContractID)
	CallMarket(t, ct, "setRoyaltySplits", []byte(zeroPayload), nil, ownerAddress, "", false, gas, "At least one royalty split required")

	// >10 splits rejected
	tenPlusPayload := fmt.Sprintf(`{"nftContract":"%s","splits":[{"recipient":"hive:a","bps":10},{"recipient":"hive:b","bps":10},{"recipient":"hive:c","bps":10},{"recipient":"hive:d","bps":10},{"recipient":"hive:e","bps":10},{"recipient":"hive:f","bps":10},{"recipient":"hive:g","bps":10},{"recipient":"hive:h","bps":10},{"recipient":"hive:i","bps":10},{"recipient":"hive:j","bps":10},{"recipient":"hive:k","bps":10}]}`, NftContractID)
	CallMarket(t, ct, "setRoyaltySplits", []byte(tenPlusPayload), nil, ownerAddress, "", false, gas, "Too many royalty splits")

	// Σbps > 5000 rejected
	tooBigPayload := fmt.Sprintf(`{"nftContract":"%s","splits":[{"recipient":"hive:a","bps":3000},{"recipient":"hive:b","bps":2001}]}`, NftContractID)
	CallMarket(t, ct, "setRoyaltySplits", []byte(tooBigPayload), nil, ownerAddress, "", false, gas, "Royalty must be <= 5000 basis points")
}
