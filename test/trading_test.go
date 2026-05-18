package contract_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"vsc-node/lib/test_utils"

	"github.com/stretchr/testify/assert"
)

// ===================================
// C1: Scheduled Listing Tests
// ===================================

// TestScheduledListingNotBuyableBeforeStart verifies that a listing with a
// future startBlock cannot be bought before that block, but succeeds after.
func TestScheduledListingNotBuyableBeforeStart(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	seller := ownerAddress
	buyer := "hive:buyer"

	MintNft(t, ct, seller, "1", 5, 100)
	ApproveNftForMarket(t, ct, seller)
	MintAndApproveToken(t, ct, buyer, 5000)

	// Start at block 100; set startBlock to 200 (future).
	ct.BlockHeight = 100

	payload := fmt.Sprintf(
		`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","pricePerUnit":"1000","startBlock":200}`,
		NftContractID, TokenID,
	)
	result, _, _ := CallMarket(t, ct, "list", []byte(payload), nil, seller, "", true, gas, "")
	created := ParseCreated(result)
	listingId := created.Id

	// Buy at block 100 (before startBlock 200) — must abort.
	buyPayload := fmt.Sprintf(`{"listingId":%d,"amount":1}`, listingId)
	CallMarket(t, ct, "buy", []byte(buyPayload), nil, buyer, "", false, gas, "Listing not started")

	// Advance to startBlock; guard is getCurrentBlockHeight() < sb so at 200 it passes.
	ct.BlockHeight = 200
	CallMarket(t, ct, "buy", []byte(buyPayload), nil, buyer, "", true, gas, "")

	// getListing should include startBlock field.
	listingPayload := fmt.Sprintf(`{"listingId":%d}`, listingId)
	listResult, _, _ := CallMarket(t, ct, "getListing", []byte(listingPayload), nil, "hive:anyone", "", true, gas, "")
	listing := ParseScheduledListing(listResult)
	assert.Equal(t, uint64(200), listing.StartBlock)
}

// TestListingNoStartBlockImmediate verifies that omitting startBlock (or 0)
// allows buying immediately — regression sanity test.
func TestListingNoStartBlockImmediate(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	seller := ownerAddress
	buyer := "hive:buyer"

	MintNft(t, ct, seller, "1", 5, 100)
	ApproveNftForMarket(t, ct, seller)
	MintAndApproveToken(t, ct, buyer, 5000)

	ct.BlockHeight = 100

	// List WITHOUT startBlock field.
	payload := fmt.Sprintf(
		`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","pricePerUnit":"1000"}`,
		NftContractID, TokenID,
	)
	CallMarket(t, ct, "list", []byte(payload), nil, seller, "", true, gas, "")

	// Immediate buy must succeed.
	CallMarket(t, ct, "buy", []byte(`{"listingId":0,"amount":1}`), nil, buyer, "", true, gas, "")

	assert.Equal(t, uint64(1), QueryNftBalance(t, ct, buyer, "1"))
}

// ===================================
// Helpers for scheduled listing
// ===================================

// ScheduledListingResult extends the base listing result with StartBlock.
type ScheduledListingResult struct {
	ListingResult
	StartBlock uint64 `json:"startBlock"`
}

func ParseScheduledListing(result test_utils.ContractTestCallResult) ScheduledListingResult {
	var resp ScheduledListingResult
	json.Unmarshal([]byte(result.Ret), &resp)
	return resp
}
