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

// ===================================
// C2: Floor Sweep Tests
// ===================================

// sweepPayload builds a JSON payload for the sweep entrypoint.
func sweepPayload(nftContract string, listingIds []uint64, maxTotal string) []byte {
	ids := ""
	for i, id := range listingIds {
		if i > 0 {
			ids += ","
		}
		ids += fmt.Sprintf("%d", id)
	}
	return []byte(fmt.Sprintf(`{"nftContract":"%s","listingIds":[%s],"maxTotal":"%s"}`,
		nftContract, ids, maxTotal))
}

// TestFloorSweepBuysAll verifies that a sweep with sufficient maxTotal purchases
// all listed NFTs, marks each listing inactive, and transfers funds correctly.
func TestFloorSweepBuysAll(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	seller := ownerAddress
	buyer := "hive:buyer"

	// Mint three distinct token IDs, each amount=1, price=1000.
	// Total cost = 3000.
	MintNft(t, ct, seller, "10", 1, 10)
	MintNft(t, ct, seller, "11", 1, 10)
	MintNft(t, ct, seller, "12", 1, 10)
	ApproveNftForMarket(t, ct, seller)
	MintAndApproveToken(t, ct, buyer, 10000)

	// Record seller's initial token balance.
	sellerBefore := QueryTokenBalance(t, ct, seller)

	// Create three listings (IDs 0, 1, 2).
	for _, tid := range []string{"10", "11", "12"} {
		p := fmt.Sprintf(`{"nftContract":"%s","tokenId":"%s","amount":1,"paymentToken":"%s","pricePerUnit":"1000"}`,
			NftContractID, tid, TokenID)
		CallMarket(t, ct, "list", []byte(p), nil, seller, "", true, gas, "")
	}

	buyerBefore := QueryTokenBalance(t, ct, buyer)

	// Sweep all 3 listings with maxTotal >= sum (3000).
	_, _, logs := CallMarket(t, ct, "sweep",
		sweepPayload(NftContractID, []uint64{0, 1, 2}, "3000"),
		nil, buyer, "", true, gas, "")

	// All three NFTs should now be with the buyer.
	assert.Equal(t, uint64(1), QueryNftBalance(t, ct, buyer, "10"), "buyer should have token 10")
	assert.Equal(t, uint64(1), QueryNftBalance(t, ct, buyer, "11"), "buyer should have token 11")
	assert.Equal(t, uint64(1), QueryNftBalance(t, ct, buyer, "12"), "buyer should have token 12")

	// All three listings should be inactive.
	for _, id := range []uint64{0, 1, 2} {
		r, _, _ := CallMarket(t, ct, "getListing",
			[]byte(fmt.Sprintf(`{"listingId":%d}`, id)),
			nil, "hive:anyone", "", true, gas, "")
		l := ParseListing(r)
		assert.False(t, l.Active, "listing %d should be inactive after sweep", id)
	}

	// Buyer paid 3000 (fee=250*3=750 deducted, but buyer's escrow is 3000 total).
	buyerAfter := QueryTokenBalance(t, ct, buyer)
	assert.Equal(t, buyerBefore-3000, buyerAfter, "buyer should have paid 3000 total")

	// Market residual should be 0 (all funds distributed).
	marketBalance := QueryTokenBalance(t, ct, MarketContractAddress)
	assert.Equal(t, uint64(0), marketBalance, "market should have zero residual")

	// Seller received net payment (3000 less fee and royalty).
	sellerAfter := QueryTokenBalance(t, ct, seller)
	assert.Greater(t, sellerAfter, sellerBefore, "seller should have received payment")

	// Event "swept" should be emitted with count=3.
	AssertEventEmitted(t, logs, "swept")
	AssertEventContains(t, logs, "swept", `"count":3`)
}

// TestFloorSweepSlippageGuard verifies that a sweep with maxTotal below the
// true sum aborts, and NO listing is consumed (all-or-nothing revert).
func TestFloorSweepSlippageGuard(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	seller := ownerAddress
	buyer := "hive:buyer"

	MintNft(t, ct, seller, "10", 1, 10)
	MintNft(t, ct, seller, "11", 1, 10)
	MintNft(t, ct, seller, "12", 1, 10)
	ApproveNftForMarket(t, ct, seller)
	MintAndApproveToken(t, ct, buyer, 10000)

	for _, tid := range []string{"10", "11", "12"} {
		p := fmt.Sprintf(`{"nftContract":"%s","tokenId":"%s","amount":1,"paymentToken":"%s","pricePerUnit":"1000"}`,
			NftContractID, tid, TokenID)
		CallMarket(t, ct, "list", []byte(p), nil, seller, "", true, gas, "")
	}

	buyerBefore := QueryTokenBalance(t, ct, buyer)

	// Sweep with maxTotal=2999 (below true sum 3000) — must abort.
	CallMarket(t, ct, "sweep",
		sweepPayload(NftContractID, []uint64{0, 1, 2}, "2999"),
		nil, buyer, "", false, gas, "Sweep exceeds maxTotal")

	// All listings must still be active (first-pass guard + atomic revert).
	for _, id := range []uint64{0, 1, 2} {
		r, _, _ := CallMarket(t, ct, "getListing",
			[]byte(fmt.Sprintf(`{"listingId":%d}`, id)),
			nil, "hive:anyone", "", true, gas, "")
		l := ParseListing(r)
		assert.True(t, l.Active, "listing %d should still be active after failed sweep", id)
	}

	// Buyer NFT balance unchanged.
	assert.Equal(t, uint64(0), QueryNftBalance(t, ct, buyer, "10"))
	assert.Equal(t, uint64(0), QueryNftBalance(t, ct, buyer, "11"))
	assert.Equal(t, uint64(0), QueryNftBalance(t, ct, buyer, "12"))

	// Buyer token balance unchanged (no escrow leak).
	buyerAfter := QueryTokenBalance(t, ct, buyer)
	assert.Equal(t, buyerBefore, buyerAfter, "buyer should not have lost tokens on failed sweep")
}

// TestFloorSweepRejectsForeignCollection verifies that including a listing from
// a different NFT collection aborts the entire sweep.
func TestFloorSweepRejectsForeignCollection(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	seller := ownerAddress
	buyer := "hive:buyer"

	// Register a second NFT collection.  Use the bare id (same pattern as
	// NftContractID="nft") so doList's cross-contract calls resolve correctly.
	const NftContract2ID = "nft2"
	ct.RegisterContract(NftContract2ID, ownerAddress, NftWasm)
	callContract(t, ct, NftContract2ID, "init",
		[]byte(`{"name":"Second NFT","symbol":"SNFT","baseUri":"https://other/"}`),
		nil, ownerAddress, defaultTimestamp, true, gas, "")

	MintNft(t, ct, seller, "20", 1, 10) // on NftContractID
	ApproveNftForMarket(t, ct, seller)

	// Mint + approve on second collection.
	callContract(t, ct, NftContract2ID, "mint",
		[]byte(fmt.Sprintf(`{"to":"%s","id":"99","amount":1,"maxSupply":10}`, seller)),
		nil, ownerAddress, defaultTimestamp, true, gas, "")
	callContract(t, ct, NftContract2ID, "setApprovalForAll",
		[]byte(fmt.Sprintf(`{"operator":"%s","approved":true}`, MarketContractAddress)),
		nil, seller, defaultTimestamp, true, gas, "")

	MintAndApproveToken(t, ct, buyer, 10000)

	// List one on the main collection (listing 0).
	p1 := fmt.Sprintf(`{"nftContract":"%s","tokenId":"20","amount":1,"paymentToken":"%s","pricePerUnit":"1000"}`,
		NftContractID, TokenID)
	CallMarket(t, ct, "list", []byte(p1), nil, seller, "", true, gas, "")

	// List one on the foreign collection (listing 1).
	p2 := fmt.Sprintf(`{"nftContract":"%s","tokenId":"99","amount":1,"paymentToken":"%s","pricePerUnit":"1000"}`,
		NftContract2ID, TokenID)
	CallMarket(t, ct, "list", []byte(p2), nil, seller, "", true, gas, "")

	// Sweep both listing ids claiming they're all from NftContractID — must abort
	// because listing 1's nc == NftContract2ID != NftContractID.
	CallMarket(t, ct, "sweep",
		sweepPayload(NftContractID, []uint64{0, 1}, "5000"),
		nil, buyer, "", false, gas, "Listing not in collection")

	// Both listings still active.
	for _, id := range []uint64{0, 1} {
		r, _, _ := CallMarket(t, ct, "getListing",
			[]byte(fmt.Sprintf(`{"listingId":%d}`, id)),
			nil, "hive:anyone", "", true, gas, "")
		l := ParseListing(r)
		assert.True(t, l.Active, "listing %d should still be active after failed sweep", id)
	}
}
