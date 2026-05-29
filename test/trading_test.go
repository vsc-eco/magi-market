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

// ===================================
// C3: Bundle Tests
// ===================================

// bundleItemsJSON builds the JSON items array for a listBundle payload.
func bundleItemsJSON(items [][2]string) string {
	// items: each [tokenId, amount]
	s := "["
	for i, item := range items {
		if i > 0 {
			s += ","
		}
		s += fmt.Sprintf(`{"tokenId":"%s","amount":%s}`, item[0], item[1])
	}
	s += "]"
	return s
}

// BundleResult mirrors BundleResponse from the contract.
type BundleResult struct {
	BundleId        uint64              `json:"bundleId"`
	Seller          string              `json:"seller"`
	NftContract     string              `json:"nftContract"`
	Items           []BundleItemResult  `json:"items"`
	PaymentToken    string              `json:"paymentToken"`
	Price           string              `json:"price"`
	Active          bool                `json:"active"`
	ExpirationBlock uint64              `json:"expirationBlock"`
}

type BundleItemResult struct {
	TokenId string `json:"tokenId"`
	Amount  uint64 `json:"amount"`
}

func ParseBundle(result test_utils.ContractTestCallResult) BundleResult {
	var resp BundleResult
	json.Unmarshal([]byte(result.Ret), &resp)
	return resp
}

// TestBundleAtomicBuy: seller mints 3 tokenIds, approves, listBundle 3 items price "10000";
// buyer buyBundle; assert all 3 items now buyer-held, seller paid received-fee-royalty,
// fee+royalty recipients paid, market residual 0, bundle inactive, bundle_bought event totalPrice=="10000".
func TestBundleAtomicBuy(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	seller := ownerAddress
	buyer := "hive:buyer"

	// Mint 3 distinct token IDs to seller
	MintNft(t, ct, seller, "101", 2, 10)
	MintNft(t, ct, seller, "102", 3, 10)
	MintNft(t, ct, seller, "103", 1, 10)
	ApproveNftForMarket(t, ct, seller)
	MintAndApproveToken(t, ct, buyer, 20000)

	feeRecipBefore := QueryTokenBalance(t, ct, feeRecipientAddress)
	sellerBefore := QueryTokenBalance(t, ct, seller)

	// List bundle: 3 items (amounts 2, 3, 1), total price 10000
	items := bundleItemsJSON([][2]string{{"101", "2"}, {"102", "3"}, {"103", "1"}})
	listPayload := fmt.Sprintf(`{"nftContract":"%s","items":%s,"paymentToken":"%s","price":"10000","expirationBlock":0}`,
		NftContractID, items, TokenID)
	listResult, _, _ := CallMarket(t, ct, "listBundle", []byte(listPayload), nil, seller, "", true, gas, "")
	created := ParseCreated(listResult)
	bundleId := created.Id

	// getBundle should show active
	getPayload := fmt.Sprintf(`{"bundleId":%d}`, bundleId)
	getResult, _, _ := CallMarket(t, ct, "getBundle", []byte(getPayload), nil, "hive:anyone", "", true, gas, "")
	bundle := ParseBundle(getResult)
	assert.True(t, bundle.Active, "bundle should be active after listing")
	assert.Equal(t, seller, bundle.Seller)
	assert.Equal(t, NftContractID, bundle.NftContract)
	assert.Equal(t, "10000", bundle.Price)
	assert.Len(t, bundle.Items, 3)

	// Buy bundle
	buyPayload := fmt.Sprintf(`{"bundleId":%d}`, bundleId)
	_, _, logs := CallMarket(t, ct, "buyBundle", []byte(buyPayload), nil, buyer, "", true, gas, "")

	// All 3 NFTs should be with buyer
	assert.Equal(t, uint64(2), QueryNftBalance(t, ct, buyer, "101"), "buyer should have 2 of token 101")
	assert.Equal(t, uint64(3), QueryNftBalance(t, ct, buyer, "102"), "buyer should have 3 of token 102")
	assert.Equal(t, uint64(1), QueryNftBalance(t, ct, buyer, "103"), "buyer should have 1 of token 103")

	// Seller should have NFTs gone
	assert.Equal(t, uint64(0), QueryNftBalance(t, ct, seller, "101"))
	assert.Equal(t, uint64(0), QueryNftBalance(t, ct, seller, "102"))
	assert.Equal(t, uint64(0), QueryNftBalance(t, ct, seller, "103"))

	// Fee recipient should have received fee (250 bps of 10000 = 250)
	feeRecipAfter := QueryTokenBalance(t, ct, feeRecipientAddress)
	assert.Equal(t, feeRecipBefore+250, feeRecipAfter, "fee recipient should have received 250")

	// Seller should have received net payment (10000 - 250 fee - 0 royalty = 9750)
	sellerAfter := QueryTokenBalance(t, ct, seller)
	assert.Equal(t, sellerBefore+9750, sellerAfter, "seller should have received 9750")

	// Market residual should be 0
	marketBal := QueryTokenBalance(t, ct, MarketContractAddress)
	assert.Equal(t, uint64(0), marketBal, "market should have zero residual")

	// Bundle should be inactive
	getResult2, _, _ := CallMarket(t, ct, "getBundle", []byte(getPayload), nil, "hive:anyone", "", true, gas, "")
	bundle2 := ParseBundle(getResult2)
	assert.False(t, bundle2.Active, "bundle should be inactive after buy")

	// Event bundle_bought should have totalPrice==10000
	AssertEventEmitted(t, logs, "bundle_bought")
	AssertEventContains(t, logs, "bundle_bought", `"totalPrice":"10000"`)
}

// TestBundleOneItemMissingRevertsAll: list bundle, seller transfers one item away so it's no longer held;
// buyBundle aborts; assert NO item moved to buyer and escrow returned (buyer token balance unchanged).
func TestBundleOneItemMissingRevertsAll(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	seller := ownerAddress
	buyer := "hive:buyer"

	MintNft(t, ct, seller, "201", 1, 10)
	MintNft(t, ct, seller, "202", 1, 10)
	MintNft(t, ct, seller, "203", 1, 10)
	ApproveNftForMarket(t, ct, seller)
	MintAndApproveToken(t, ct, buyer, 20000)

	// List bundle
	items := bundleItemsJSON([][2]string{{"201", "1"}, {"202", "1"}, {"203", "1"}})
	listPayload := fmt.Sprintf(`{"nftContract":"%s","items":%s,"paymentToken":"%s","price":"5000","expirationBlock":0}`,
		NftContractID, items, TokenID)
	listResult, _, _ := CallMarket(t, ct, "listBundle", []byte(listPayload), nil, seller, "", true, gas, "")
	bundleId := ParseCreated(listResult).Id

	// Seller transfers token 202 away to a third party (making the bundle unbuyable)
	thirdParty := "hive:thirdparty"
	CallNft(t, ct, "safeTransferFrom",
		[]byte(fmt.Sprintf(`{"from":"%s","to":"%s","id":"202","amount":1,"data":""}`, seller, thirdParty)),
		nil, seller, true, gas, "")

	buyerBefore := QueryTokenBalance(t, ct, buyer)

	// buyBundle should abort (seller no longer holds token 202)
	buyPayload := fmt.Sprintf(`{"bundleId":%d}`, bundleId)
	CallMarket(t, ct, "buyBundle", []byte(buyPayload), nil, buyer, "", false, gas, "")

	// No items should have moved to buyer
	assert.Equal(t, uint64(0), QueryNftBalance(t, ct, buyer, "201"), "buyer should NOT have token 201")
	assert.Equal(t, uint64(0), QueryNftBalance(t, ct, buyer, "202"), "buyer should NOT have token 202")
	assert.Equal(t, uint64(0), QueryNftBalance(t, ct, buyer, "203"), "buyer should NOT have token 203")

	// Buyer token balance should be unchanged (escrow reverted)
	buyerAfter := QueryTokenBalance(t, ct, buyer)
	assert.Equal(t, buyerBefore, buyerAfter, "buyer token balance should be unchanged after failed buyBundle")
}

// TestDelistBundleNoNftMove: list then delistBundle → bundle inactive, seller NFT balances unchanged.
func TestDelistBundleNoNftMove(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	seller := ownerAddress

	MintNft(t, ct, seller, "301", 2, 10)
	MintNft(t, ct, seller, "302", 1, 10)
	ApproveNftForMarket(t, ct, seller)

	items := bundleItemsJSON([][2]string{{"301", "2"}, {"302", "1"}})
	listPayload := fmt.Sprintf(`{"nftContract":"%s","items":%s,"paymentToken":"%s","price":"3000","expirationBlock":0}`,
		NftContractID, items, TokenID)
	listResult, _, _ := CallMarket(t, ct, "listBundle", []byte(listPayload), nil, seller, "", true, gas, "")
	bundleId := ParseCreated(listResult).Id

	// Seller NFTs still held (approval-custody, nothing escrowed)
	assert.Equal(t, uint64(2), QueryNftBalance(t, ct, seller, "301"))
	assert.Equal(t, uint64(1), QueryNftBalance(t, ct, seller, "302"))

	// Delist
	delistPayload := fmt.Sprintf(`{"bundleId":%d}`, bundleId)
	CallMarket(t, ct, "delistBundle", []byte(delistPayload), nil, seller, "", true, gas, "")

	// Bundle inactive
	getResult, _, _ := CallMarket(t, ct, "getBundle", []byte(fmt.Sprintf(`{"bundleId":%d}`, bundleId)),
		nil, "hive:anyone", "", true, gas, "")
	bundle := ParseBundle(getResult)
	assert.False(t, bundle.Active, "bundle should be inactive after delist")

	// Seller still holds their NFTs
	assert.Equal(t, uint64(2), QueryNftBalance(t, ct, seller, "301"), "seller should still have token 301")
	assert.Equal(t, uint64(1), QueryNftBalance(t, ct, seller, "302"), "seller should still have token 302")
}

// TestBundleDeniedCollectionBlocked: denyCollection(nc) then listBundle aborts "Collection is denied";
// a pre-listed bundle then denied → buyBundle aborts.
func TestBundleDeniedCollectionBlocked(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	seller := ownerAddress
	buyer := "hive:buyer"

	MintNft(t, ct, seller, "401", 1, 10)
	MintNft(t, ct, seller, "402", 1, 10)
	ApproveNftForMarket(t, ct, seller)
	MintAndApproveToken(t, ct, buyer, 10000)

	// List a bundle BEFORE denying
	items := bundleItemsJSON([][2]string{{"401", "1"}, {"402", "1"}})
	listPayload := fmt.Sprintf(`{"nftContract":"%s","items":%s,"paymentToken":"%s","price":"2000","expirationBlock":0}`,
		NftContractID, items, TokenID)
	listResult, _, _ := CallMarket(t, ct, "listBundle", []byte(listPayload), nil, seller, "", true, gas, "")
	bundleId := ParseCreated(listResult).Id

	// Deny the collection
	denyPayload := fmt.Sprintf(`{"nftContract":"%s"}`, NftContractID)
	CallMarket(t, ct, "denyCollection", []byte(denyPayload), nil, ownerAddress, "", true, gas, "")

	// listBundle on denied collection should abort
	listPayload2 := fmt.Sprintf(`{"nftContract":"%s","items":%s,"paymentToken":"%s","price":"1000","expirationBlock":0}`,
		NftContractID, items, TokenID)
	CallMarket(t, ct, "listBundle", []byte(listPayload2), nil, seller, "", false, gas, "Collection is denied")

	// buyBundle on pre-listed bundle with denied collection should also abort
	buyPayload := fmt.Sprintf(`{"bundleId":%d}`, bundleId)
	CallMarket(t, ct, "buyBundle", []byte(buyPayload), nil, buyer, "", false, gas, "Collection is denied")
}

// TestBundleSoulboundOwnerAllowed verifies the collection owner CAN bundle their
// own soulbound editions (bundles don't escrow; the buy-time transfer is
// owner→buyer, which magi_nft permits when from==ownerAddr).
func TestBundleSoulboundOwnerAllowed(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	seller := ownerAddress

	// One soulbound + one regular edition, both held by the collection owner.
	CallNft(t, ct, "mint", []byte(`{"to":"hive:tibfox","id":"sb1","amount":1,"maxSupply":1,"soulbound":true}`), nil, ownerAddress, true, gas, "")
	MintNft(t, ct, seller, "reg1", 1, 10)
	ApproveNftForMarket(t, ct, seller)

	items := bundleItemsJSON([][2]string{{"sb1", "1"}, {"reg1", "1"}})
	listPayload := fmt.Sprintf(`{"nftContract":"%s","items":%s,"paymentToken":"%s","price":"2000","expirationBlock":0}`,
		NftContractID, items, TokenID)
	CallMarket(t, ct, "listBundle", []byte(listPayload), nil, seller, "", true, gas, "")
}

// TestBundleSoulboundNonOwnerBlocked verifies a non-owner holder CANNOT bundle a
// soulbound token (the owner→buyer leg would never succeed for them).
func TestBundleSoulboundNonOwnerBlocked(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	seller := "hive:buyer"

	// Soulbound minted directly to the non-owner, plus a regular edition.
	CallNft(t, ct, "mint", []byte(`{"to":"hive:buyer","id":"sb1","amount":1,"maxSupply":1,"soulbound":true}`), nil, ownerAddress, true, gas, "")
	CallNft(t, ct, "mint", []byte(`{"to":"hive:buyer","id":"reg1","amount":1,"maxSupply":10}`), nil, ownerAddress, true, gas, "")
	ApproveNftForMarket(t, ct, seller)

	items := bundleItemsJSON([][2]string{{"sb1", "1"}, {"reg1", "1"}})
	listPayload := fmt.Sprintf(`{"nftContract":"%s","items":%s,"paymentToken":"%s","price":"2000","expirationBlock":0}`,
		NftContractID, items, TokenID)
	CallMarket(t, ct, "listBundle", []byte(listPayload), nil, seller, "", false, gas, "Cannot list soulbound tokens")
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

// ===================================
// C3: Bundle Royalty Lock Test
// ===================================

// TestBundleRoyaltyLockedAtListing verifies that the royalty bps locked at
// listBundle time is used for payout, even if the collection owner raises the
// royalty after listing and before purchase.  Mirrors TestRoyaltyLockedAtListingCreation
// for the bundle path.
func TestBundleRoyaltyLockedAtListing(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	seller := ownerAddress
	buyer := "hive:buyer"
	creator := "hive:creator"

	// Collection starts with NO royalty-split snapshot and zero royalty.
	// Confirm baseline: no royalty set yet.

	MintNft(t, ct, seller, "701", 1, 10)
	MintNft(t, ct, seller, "702", 1, 10)
	ApproveNftForMarket(t, ct, seller)
	// Buyer funded with exact bundle price.
	MintAndApproveToken(t, ct, buyer, 10000)

	// List bundle BEFORE any royalty is set (royalty = 0 bps at listing time).
	items := bundleItemsJSON([][2]string{{"701", "1"}, {"702", "1"}})
	listPayload := fmt.Sprintf(
		`{"nftContract":"%s","items":%s,"paymentToken":"%s","price":"10000","expirationBlock":0}`,
		NftContractID, items, TokenID)
	listResult, _, _ := CallMarket(t, ct, "listBundle", []byte(listPayload), nil, seller, "", true, gas, "")
	bundleId := ParseCreated(listResult).Id

	// NOW raise collection royalty to 20% (2000 bps) after the bundle is already listed.
	royaltyPayload := fmt.Sprintf(`{"nftContract":"%s","royaltyBps":2000,"royaltyRecipient":"%s"}`, NftContractID, creator)
	CallMarket(t, ct, "setRoyalty", []byte(royaltyPayload), nil, ownerAddress, "", true, gas, "")

	// Record balances before buy.
	sellerBefore := QueryTokenBalance(t, ct, seller)
	feeRecipBefore := QueryTokenBalance(t, ct, feeRecipientAddress)

	// Buyer purchases the bundle.
	buyPayload := fmt.Sprintf(`{"bundleId":%d}`, bundleId)
	CallMarket(t, ct, "buyBundle", []byte(buyPayload), nil, buyer, "", true, gas, "")

	// Expected payouts based on listing-time royalty (0 bps):
	//   price = 10000
	//   fee   = 250 (250 bps locked fb)
	//   royalty = 0 (0 bps locked rb — royalty was zero at listing time)
	//   seller = 10000 - 250 - 0 = 9750
	sellerAfter := QueryTokenBalance(t, ct, seller)
	feeRecipAfter := QueryTokenBalance(t, ct, feeRecipientAddress)
	creatorBalance := QueryTokenBalance(t, ct, creator)

	assert.Equal(t, sellerBefore+9750, sellerAfter,
		"seller should receive 9750 (listing-time royalty=0, not post-listing 20%)")
	assert.Equal(t, feeRecipBefore+250, feeRecipAfter,
		"fee recipient should receive 250 (2.5% fee)")
	assert.Equal(t, uint64(0), creatorBalance,
		"royalty recipient should receive 0 (royalty was 0 at listing time)")

	// Buyer should hold both NFTs.
	assert.Equal(t, uint64(1), QueryNftBalance(t, ct, buyer, "701"))
	assert.Equal(t, uint64(1), QueryNftBalance(t, ct, buyer, "702"))

	// Market should have zero residual.
	marketBal := QueryTokenBalance(t, ct, MarketContractAddress)
	assert.Equal(t, uint64(0), marketBal, "market should have zero residual")
}

// TestListViaPerTokenApprove: the seller authorizes the marketplace with a
// per-token ERC-6909 approve(spender=market, id, amount) instead of a
// blanket setApprovalForAll. doList must accept it, and doBuy must drive
// magi_nft's safeTransferFrom through that allowance (decremented per sale).
func TestListViaPerTokenApprove(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	seller := ownerAddress
	buyer := "hive:buyer"

	MintNft(t, ct, seller, "1", 5, 100)
	// Per-token approve ONLY — no setApprovalForAll.
	CallNft(t, ct, "approve",
		[]byte(fmt.Sprintf(`{"spender":"%s","id":"1","amount":5}`, MarketContractAddress)),
		nil, seller, true, gas, "")
	MintAndApproveToken(t, ct, buyer, 5000)

	// List 5 — allowance (5) >= amount (5) → accepted.
	CallMarket(t, ct, "list",
		[]byte(fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","pricePerUnit":"1000"}`, NftContractID, TokenID)),
		nil, seller, "", true, gas, "")

	// Buy 2 then 3 — safeTransferFrom rides the per-token allowance.
	CallMarket(t, ct, "buy", []byte(`{"listingId":0,"amount":2}`), nil, buyer, "", true, gas, "")
	assert.Equal(t, uint64(2), QueryNftBalance(t, ct, buyer, "1"), "buyer has 2 via allowance-driven transfer")
	CallMarket(t, ct, "buy", []byte(`{"listingId":0,"amount":3}`), nil, buyer, "", true, gas, "")
	assert.Equal(t, uint64(5), QueryNftBalance(t, ct, buyer, "1"), "buyer has all 5")
	assert.Equal(t, uint64(0), QueryNftBalance(t, ct, seller, "1"), "seller transferred all 5")
}

// TestListPerTokenApproveInsufficient: a per-token allowance smaller than
// the listed amount is rejected by doList (no operator approval either).
func TestListPerTokenApproveInsufficient(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	seller := ownerAddress

	MintNft(t, ct, seller, "1", 5, 100)
	CallNft(t, ct, "approve",
		[]byte(fmt.Sprintf(`{"spender":"%s","id":"1","amount":2}`, MarketContractAddress)),
		nil, seller, true, gas, "")

	CallMarket(t, ct, "list",
		[]byte(fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","pricePerUnit":"1000"}`, NftContractID, TokenID)),
		nil, seller, "", false, gas, "Marketplace not approved as operator or sufficient per-token allowance for this NFT collection")
}
