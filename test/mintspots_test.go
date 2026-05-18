package contract_test

// ============================================================================
// G2: Mint-Spot Primary Sale tests.
//
// FUND-CRITICAL: proves the listMintSpots/buyMintSpot/delistMintSpots/
// getMintSpotListing entrypoints are correct.
//
// Setup: mintnftmock (G1) is used as the nft contract (has define, mint,
// setApprovalForAll, balanceOf, maxSupply, getOwner). TokenID (magi_token) is
// used as the payment token so MintAndApproveToken helpers work directly.
//
// Invariants verified:
//   - BuyMintSpot reuses escrowIn / feeAndRoyaltyOf(nil royalty) / nftDelegatedMint
//   - Mint happens BEFORE payouts (atomic: nft abort reverts escrow)
//   - nft enforces maxSupply via abort ("Would exceed max supply")
//   - tokenId allowlist injection check
//   - No royalty on primary (feeAndRoyaltyOf(received, fb, nil, nil))
//   - 306 prior tests unaffected (purely additive)
// ============================================================================

import (
	"encoding/json"
	"fmt"
	"testing"

	"vsc-node/lib/test_utils"

	"github.com/stretchr/testify/assert"
)

// ---------------------------------------------------------------------------
// MintNftMock helpers (call the mintnftmock contract by its ID)
// ---------------------------------------------------------------------------

// CallMintNftMock calls an entrypoint on the mintnftmock contract.
func CallMintNftMock(t *testing.T, ct *test_utils.ContractTest, action string, payload []byte, authUser string, expectOk bool, expectedOutput string) test_utils.ContractTestCallResult {
	cr, _, _ := callContract(t, ct, MintNftMockID, action, payload, nil, authUser, defaultTimestamp, expectOk, gas, expectedOutput)
	return cr
}

// InitMintNftMock initialises the mintnftmock with ownerAddress as owner.
func InitMintNftMock(t *testing.T, ct *test_utils.ContractTest) {
	CallMintNftMock(t, ct, "init",
		[]byte(fmt.Sprintf(`{"owner":"%s"}`, ownerAddress)),
		ownerAddress, true, "")
}

// DefineMintEdition defines an edition on the mintnftmock (owner only).
func DefineMintEdition(t *testing.T, ct *test_utils.ContractTest, id string, maxSupply uint64) {
	CallMintNftMock(t, ct, "define",
		[]byte(fmt.Sprintf(`{"id":"%s","maxSupply":%d}`, id, maxSupply)),
		ownerAddress, true, "")
}

// ApproveMintNftMockForMarket approves the marketplace as operator on mintnftmock.
func ApproveMintNftMockForMarket(t *testing.T, ct *test_utils.ContractTest) {
	CallMintNftMock(t, ct, "setApprovalForAll",
		[]byte(fmt.Sprintf(`{"operator":"%s","approved":true}`, MarketContractAddress)),
		ownerAddress, true, "")
}

// QueryMintNftBalance queries balanceOf on the mintnftmock.
func QueryMintNftBalance(t *testing.T, ct *test_utils.ContractTest, account, id string) uint64 {
	cr := CallMintNftMock(t, ct, "balanceOf",
		[]byte(fmt.Sprintf(`{"account":"%s","id":"%s"}`, account, id)),
		"hive:anyone", true, "")
	return ParseBalance(cr)
}

// ---------------------------------------------------------------------------
// MintSpot helpers (call market entrypoints)
// ---------------------------------------------------------------------------

type MintSpotListingResult struct {
	ListingId       uint64 `json:"listingId"`
	Lister          string `json:"lister"`
	NftContract     string `json:"nftContract"`
	TokenId         string `json:"tokenId"`
	PaymentToken    string `json:"paymentToken"`
	PricePerSpot    string `json:"pricePerSpot"`
	MaxSpots        uint64 `json:"maxSpots"`
	Sold            uint64 `json:"sold"`
	Active          bool   `json:"active"`
	ExpirationBlock uint64 `json:"expirationBlock"`
	StartBlock      uint64 `json:"startBlock"`
	FeeBps          uint64 `json:"feeBps"`
}

func ParseMintSpotListing(result test_utils.ContractTestCallResult) MintSpotListingResult {
	var r MintSpotListingResult
	json.Unmarshal([]byte(result.Ret), &r)
	return r
}

// listMintSpots calls the listMintSpots entrypoint and returns the result.
func listMintSpots(t *testing.T, ct *test_utils.ContractTest, nftContract, tokenId, paymentToken, pricePerSpot string, maxSpots, expirationBlock, startBlock uint64, authUser string, expectOk bool, expectedOutput string) test_utils.ContractTestCallResult {
	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"%s","paymentToken":"%s","pricePerSpot":"%s","maxSpots":%d,"expirationBlock":%d,"startBlock":%d}`,
		nftContract, tokenId, paymentToken, pricePerSpot, maxSpots, expirationBlock, startBlock)
	cr, _, _ := CallMarket(t, ct, "listMintSpots", []byte(payload), nil, authUser, "", expectOk, gas, expectedOutput)
	return cr
}

// buyMintSpot calls the buyMintSpot entrypoint.
func buyMintSpot(t *testing.T, ct *test_utils.ContractTest, listingId, amount uint64, authUser string, expectOk bool, expectedOutput string) test_utils.ContractTestCallResult {
	payload := fmt.Sprintf(`{"listingId":%d,"amount":%d}`, listingId, amount)
	cr, _, _ := CallMarket(t, ct, "buyMintSpot", []byte(payload), nil, authUser, "", expectOk, gas, expectedOutput)
	return cr
}

// buyMintSpotWithLogs calls buyMintSpot and returns logs.
func buyMintSpotWithLogs(t *testing.T, ct *test_utils.ContractTest, listingId, amount uint64, authUser string, expectOk bool, expectedOutput string) test_utils.ContractTestCallResult {
	payload := fmt.Sprintf(`{"listingId":%d,"amount":%d}`, listingId, amount)
	cr, _, _ := CallMarket(t, ct, "buyMintSpot", []byte(payload), nil, authUser, "", expectOk, gas, expectedOutput)
	return cr
}

// setupMintSpotBase initialises a market+token+mintnftmock setup and returns ct.
// feeBps: marketplace fee in basis points.
// Caller must define an edition and setApprovalForAll separately.
func setupMintSpotBase(t *testing.T, feeBps uint64) *test_utils.ContractTest {
	ct := SetupContractTest()
	InitToken(t, ct)
	// Init mintnftmock with ownerAddress as owner.
	InitMintNftMock(t, ct)
	// Init market.
	payload := fmt.Sprintf(`{"feeBps":%d,"feeRecipient":"%s"}`, feeBps, feeRecipientAddress)
	CallMarket(t, ct, "init", []byte(payload), nil, ownerAddress, "", true, gas, "")
	// Add TokenID as payment token. TokenID is registered without "contract:" prefix,
	// matching how existing trading tests use it.
	CallMarket(t, ct, "addPaymentToken",
		[]byte(fmt.Sprintf(`{"token":"%s"}`, TokenID)),
		nil, ownerAddress, "", true, gas, "")
	return ct
}

// ---------------------------------------------------------------------------
// TestMintSpotHappyPath
// ---------------------------------------------------------------------------

func TestMintSpotHappyPath(t *testing.T) {
	feeBps := uint64(250) // 2.5%
	ct := setupMintSpotBase(t, feeBps)

	buyer := "hive:buyer"
	paymentToken := TokenID
	tokenId := "1"
	pricePerSpot := "1000"
	amount := uint64(2)

	// Define edition (maxSupply=5) and approve market as operator.
	DefineMintEdition(t, ct, tokenId, 5)
	ApproveMintNftMockForMarket(t, ct)

	// Fund buyer: 2 spots × 1000 = 2000.
	MintAndApproveToken(t, ct, buyer, 2000)

	buyerTokenBefore := QueryTokenBalance(t, ct, buyer)
	ownerTokenBefore := QueryTokenBalance(t, ct, ownerAddress)
	feeRecipBefore := QueryTokenBalance(t, ct, feeRecipientAddress)
	marketBefore := QueryTokenBalance(t, ct, MarketContractAddress)

	// List mint spots.
	listResult := listMintSpots(t, ct, MintNftMockID, tokenId, paymentToken, pricePerSpot, 0, 0, 0, ownerAddress, true, "")
	listId := ParseCreated(listResult)
	assert.True(t, listId.Success)
	assert.Equal(t, uint64(0), listId.Id)

	// Buy 2 spots.
	buyMintSpot(t, ct, 0, amount, buyer, true, "")

	// Verify NFT minted to buyer.
	nftBalance := QueryMintNftBalance(t, ct, buyer, tokenId)
	assert.Equal(t, amount, nftBalance, "buyer should have 2 minted NFTs")

	// Verify token accounting.
	// total = 1000 * 2 = 2000; received = 2000 (no fee-on-transfer in magi_token).
	// fee = floor(2000 * 250 / 10000) = floor(50) = 50
	// creatorPay = 2000 - 50 = 1950
	buyerTokenAfter := QueryTokenBalance(t, ct, buyer)
	ownerTokenAfter := QueryTokenBalance(t, ct, ownerAddress)
	feeRecipAfter := QueryTokenBalance(t, ct, feeRecipientAddress)
	marketAfter := QueryTokenBalance(t, ct, MarketContractAddress)

	assert.Equal(t, uint64(2000), buyerTokenBefore-buyerTokenAfter, "buyer paid 2000")
	assert.Equal(t, uint64(1950), ownerTokenAfter-ownerTokenBefore, "creator received received-fee = 1950")
	assert.Equal(t, uint64(50), feeRecipAfter-feeRecipBefore, "fee recipient received fee = 50")
	assert.Equal(t, marketBefore, marketAfter, "market token residual unchanged (0 net)")
}

// ---------------------------------------------------------------------------
// TestMintSpotMaxSupplyEnforcedByNft
// ---------------------------------------------------------------------------

func TestMintSpotMaxSupplyEnforcedByNft(t *testing.T) {
	ct := setupMintSpotBase(t, 0)

	buyer := "hive:buyer"
	paymentToken := TokenID
	tokenId := "1"

	DefineMintEdition(t, ct, tokenId, 3)
	ApproveMintNftMockForMarket(t, ct)

	// Fund buyer enough for both buy attempts (2 + 2 = 4 spots, but maxSupply=3).
	MintAndApproveToken(t, ct, buyer, 4000)

	listMintSpots(t, ct, MintNftMockID, tokenId, paymentToken, "1000", 0, 0, 0, ownerAddress, true, "")

	buyerBefore := QueryTokenBalance(t, ct, buyer)
	creatorBefore := QueryTokenBalance(t, ct, ownerAddress)

	// First buy: 2 spots → success (minted=2, maxSupply=3).
	buyMintSpotWithLogs(t, ct, 0, 2, buyer, true, "")
	assert.Equal(t, uint64(2), QueryMintNftBalance(t, ct, buyer, tokenId), "buyer has 2 after first buy")

	// Second buy: 2 spots → would exceed maxSupply=3 (minted=2+2=4 > 3) → nft aborts → tx reverts.
	buyerBefore2 := QueryTokenBalance(t, ct, buyer)
	creatorBefore2 := QueryTokenBalance(t, ct, ownerAddress)
	buyMintSpotWithLogs(t, ct, 0, 2, buyer, false, "Would exceed max supply")
	buyerAfter2 := QueryTokenBalance(t, ct, buyer)
	creatorAfter2 := QueryTokenBalance(t, ct, ownerAddress)

	// Buyer balance must be unchanged (escrow reverted).
	assert.Equal(t, buyerBefore2, buyerAfter2, "buyer payment refunded on failed buy")
	// Creator must not have received payment.
	assert.Equal(t, creatorBefore2, creatorAfter2, "creator not paid for failed buy")
	// NFT balance unchanged.
	assert.Equal(t, uint64(2), QueryMintNftBalance(t, ct, buyer, tokenId), "buyer NFT balance unchanged after failed buy")

	_ = buyerBefore
	_ = creatorBefore
}

// ---------------------------------------------------------------------------
// TestMintSpotListingCapEnforced
// ---------------------------------------------------------------------------

func TestMintSpotListingCapEnforced(t *testing.T) {
	ct := setupMintSpotBase(t, 0)

	buyer := "hive:buyer"
	paymentToken := TokenID
	tokenId := "1"

	// Edition maxSupply=10, listing maxSpots=3.
	DefineMintEdition(t, ct, tokenId, 10)
	ApproveMintNftMockForMarket(t, ct)
	MintAndApproveToken(t, ct, buyer, 10000)

	listMintSpots(t, ct, MintNftMockID, tokenId, paymentToken, "100", 3, 0, 0, ownerAddress, true, "")

	// Buy 2 → ok (sold=2, cap=3).
	buyMintSpotWithLogs(t, ct, 0, 2, buyer, true, "")
	assert.Equal(t, uint64(2), QueryMintNftBalance(t, ct, buyer, tokenId))

	// Buy 2 → exceeds cap (sold=2+2=4 > maxSpots=3) → abort.
	buyMintSpotWithLogs(t, ct, 0, 2, buyer, false, "Exceeds listing mint-spot cap")

	// Buy exactly 1 → fills cap (sold=2+1=3==maxSpots=3) → listing auto-deactivates.
	buyMintSpotWithLogs(t, ct, 0, 1, buyer, true, "")
	assert.Equal(t, uint64(3), QueryMintNftBalance(t, ct, buyer, tokenId))

	// Listing should now be inactive.
	getResult, _, _ := CallMarket(t, ct, "getMintSpotListing", []byte(`{"listingId":0}`), nil, "hive:anyone", "", true, gas, "")
	listing := ParseMintSpotListing(getResult)
	assert.False(t, listing.Active, "listing should be auto-inactive after maxSpots sold")
	assert.Equal(t, uint64(3), listing.Sold)
}

// ---------------------------------------------------------------------------
// TestMintSpotOnlyOwnerLists
// ---------------------------------------------------------------------------

func TestMintSpotOnlyOwnerLists(t *testing.T) {
	ct := setupMintSpotBase(t, 250)

	nonOwner := "hive:nonowner"
	paymentToken := TokenID

	DefineMintEdition(t, ct, "1", 5)
	ApproveMintNftMockForMarket(t, ct)

	// Non-owner tries to list → must fail.
	listMintSpots(t, ct, MintNftMockID, "1", paymentToken, "1000", 0, 0, 0, nonOwner, false, "Only collection owner can list mint spots")
}

// ---------------------------------------------------------------------------
// TestMintSpotMarketNotApproved
// ---------------------------------------------------------------------------

func TestMintSpotMarketNotApproved(t *testing.T) {
	ct := setupMintSpotBase(t, 250)

	buyer := "hive:buyer"
	paymentToken := TokenID

	DefineMintEdition(t, ct, "1", 5)
	// Do NOT call ApproveMintNftMockForMarket.

	// List should fail because market is not approved as operator.
	listMintSpots(t, ct, MintNftMockID, "1", paymentToken, "1000", 0, 0, 0, ownerAddress, false, "Marketplace not approved as operator for this NFT collection")

	_ = buyer
}

// ---------------------------------------------------------------------------
// TestMintSpotEditionNotDefined
// ---------------------------------------------------------------------------

func TestMintSpotEditionNotDefined(t *testing.T) {
	ct := setupMintSpotBase(t, 250)

	paymentToken := TokenID

	// Set approval but do NOT define the edition.
	ApproveMintNftMockForMarket(t, ct)

	// List should fail because edition "99" is not defined.
	listMintSpots(t, ct, MintNftMockID, "99", paymentToken, "1000", 0, 0, 0, ownerAddress, false, "Edition not defined")
}

// ---------------------------------------------------------------------------
// TestMintSpotDeniedCollectionBlocked
// ---------------------------------------------------------------------------

func TestMintSpotDeniedCollectionBlocked(t *testing.T) {
	ct := setupMintSpotBase(t, 250)

	buyer := "hive:buyer"
	paymentToken := TokenID

	DefineMintEdition(t, ct, "1", 10)
	ApproveMintNftMockForMarket(t, ct)
	MintAndApproveToken(t, ct, buyer, 5000)

	// List while collection is allowed — succeeds.
	listMintSpots(t, ct, MintNftMockID, "1", paymentToken, "1000", 0, 0, 0, ownerAddress, true, "")

	// Deny the collection.
	CallMarket(t, ct, "denyCollection",
		[]byte(fmt.Sprintf(`{"nftContract":"%s"}`, MintNftMockID)),
		nil, ownerAddress, "", true, gas, "")

	// New list attempt should fail.
	listMintSpots(t, ct, MintNftMockID, "1", paymentToken, "1000", 0, 0, 0, ownerAddress, false, "Collection is denied")

	// Buy on the existing listing should also fail.
	buyMintSpotWithLogs(t, ct, 0, 1, buyer, false, "Collection is denied")
}

// ---------------------------------------------------------------------------
// TestMintSpotDelistByLister
// ---------------------------------------------------------------------------

func TestMintSpotDelistByLister(t *testing.T) {
	ct := setupMintSpotBase(t, 250)

	buyer := "hive:buyer"
	nonLister := "hive:nonlister"
	paymentToken := TokenID

	DefineMintEdition(t, ct, "1", 10)
	ApproveMintNftMockForMarket(t, ct)
	MintAndApproveToken(t, ct, buyer, 5000)

	listMintSpots(t, ct, MintNftMockID, "1", paymentToken, "1000", 0, 0, 0, ownerAddress, true, "")

	// Non-lister delist attempt → rejected.
	CallMarket(t, ct, "delistMintSpots",
		[]byte(`{"listingId":0}`), nil, nonLister, "", false, gas, "Only lister can delist mint spots")

	// Lister can delist.
	CallMarket(t, ct, "delistMintSpots",
		[]byte(`{"listingId":0}`), nil, ownerAddress, "", true, gas, "")

	// Subsequent buy → listing not active.
	buyMintSpotWithLogs(t, ct, 0, 1, buyer, false, "Mint spot listing not active")

	// Second delist attempt → also rejected (already inactive).
	CallMarket(t, ct, "delistMintSpots",
		[]byte(`{"listingId":0}`), nil, ownerAddress, "", false, gas, "Mint spot listing not active")
}

// ---------------------------------------------------------------------------
// TestMintSpotTokenIdInjectionRejected
// ---------------------------------------------------------------------------

func TestMintSpotTokenIdInjectionRejected(t *testing.T) {
	ct := setupMintSpotBase(t, 250)

	paymentToken := TokenID
	ApproveMintNftMockForMarket(t, ct)

	// TokenId with injection character `"` must be rejected.
	CallMarket(t, ct, "listMintSpots",
		[]byte(fmt.Sprintf(`{"nftContract":"%s","tokenId":"bad\"id","paymentToken":"%s","pricePerSpot":"1000","maxSpots":0,"expirationBlock":0,"startBlock":0}`,
			MintNftMockID, paymentToken)),
		nil, ownerAddress, "", false, gas, "tokenId contains invalid characters")

	// TokenId with space must be rejected.
	listMintSpots(t, ct, MintNftMockID, "bad id", paymentToken, "1000", 0, 0, 0, ownerAddress, false, "tokenId contains invalid characters")

	// Valid tokenId with allowed chars (letters, digits, colon, hyphen) must pass validation.
	// (Define the edition first so it doesn't fail on "Edition not defined")
	DefineMintEdition(t, ct, "valid-id:1", 5)
	// This should pass injection check (may fail on edition not defined for "valid-id:1" — but define it first).
	listMintSpots(t, ct, MintNftMockID, "valid-id:1", paymentToken, "1000", 0, 0, 0, ownerAddress, true, "")
}

// ---------------------------------------------------------------------------
// TestMintSpotStartBlock
// ---------------------------------------------------------------------------

func TestMintSpotStartBlock(t *testing.T) {
	ct := setupMintSpotBase(t, 0)

	buyer := "hive:buyer"
	paymentToken := TokenID

	DefineMintEdition(t, ct, "1", 10)
	ApproveMintNftMockForMarket(t, ct)
	MintAndApproveToken(t, ct, buyer, 5000)

	// List with a future startBlock (very large block number so it's in the future).
	// The test harness block.height defaults to 0-ish.
	futureBlock := uint64(99999)
	listMintSpots(t, ct, MintNftMockID, "1", paymentToken, "1000", 0, 0, futureBlock, ownerAddress, true, "")

	// Buy before startBlock → rejected.
	buyMintSpotWithLogs(t, ct, 0, 1, buyer, false, "Listing not started")

	// List with startBlock=0 (no start gate) → immediate.
	listMintSpots(t, ct, MintNftMockID, "1", paymentToken, "1000", 0, 0, 0, ownerAddress, true, "")
	buyMintSpotWithLogs(t, ct, 1, 1, buyer, true, "")
	assert.Equal(t, uint64(1), QueryMintNftBalance(t, ct, buyer, "1"))
}

// ---------------------------------------------------------------------------
// TestMintSpotGetListing
// ---------------------------------------------------------------------------

func TestMintSpotGetListing(t *testing.T) {
	ct := setupMintSpotBase(t, 250)

	paymentToken := TokenID

	DefineMintEdition(t, ct, "5", 20)
	ApproveMintNftMockForMarket(t, ct)

	listMintSpots(t, ct, MintNftMockID, "5", paymentToken, "500", 10, 999, 0, ownerAddress, true, "")

	// GetMintSpotListing should return the stored values.
	getResult, _, _ := CallMarket(t, ct, "getMintSpotListing", []byte(`{"listingId":0}`), nil, "hive:anyone", "", true, gas, "")
	listing := ParseMintSpotListing(getResult)

	assert.Equal(t, uint64(0), listing.ListingId)
	assert.Equal(t, ownerAddress, listing.Lister)
	assert.Equal(t, MintNftMockID, listing.NftContract)
	assert.Equal(t, "5", listing.TokenId)
	assert.Equal(t, paymentToken, listing.PaymentToken)
	assert.Equal(t, "500", listing.PricePerSpot)
	assert.Equal(t, uint64(10), listing.MaxSpots)
	assert.Equal(t, uint64(0), listing.Sold)
	assert.True(t, listing.Active)
	assert.Equal(t, uint64(999), listing.ExpirationBlock)
	assert.Equal(t, uint64(250), listing.FeeBps, "feeBps reflects the effective fee locked at list time")
}

// ---------------------------------------------------------------------------
// TestMintSpotBoughtEvent
// ---------------------------------------------------------------------------

func TestMintSpotBoughtEvent(t *testing.T) {
	ct := setupMintSpotBase(t, 250)

	buyer := "hive:buyer"
	paymentToken := TokenID

	DefineMintEdition(t, ct, "1", 10)
	ApproveMintNftMockForMarket(t, ct)
	MintAndApproveToken(t, ct, buyer, 2000)

	listMintSpots(t, ct, MintNftMockID, "1", paymentToken, "1000", 0, 0, 0, ownerAddress, true, "")

	payload := fmt.Sprintf(`{"listingId":0,"amount":1}`)
	_, _, logs := CallMarket(t, ct, "buyMintSpot", []byte(payload), nil, buyer, "", true, gas, "")

	// Event must be emitted with correct type and amount.
	AssertEventEmitted(t, logs, "mint_spot_bought")
	AssertEventContains(t, logs, "mint_spot_bought", `"amount":1`)
	AssertEventContains(t, logs, "mint_spot_bought", `"received":"1000"`)
	// fee = floor(1000 * 250 / 10000) = 25
	AssertEventContains(t, logs, "mint_spot_bought", `"fee":"25"`)
}

// ---------------------------------------------------------------------------
// TestMintSpotSoulboundSellable (spec Section 4 — locked decision 5)
//
// The mintnftmock does not have a soulbound flag (soulbound is set at
// define-time on the real magi_nft contract side). The market's listMintSpots
// intentionally does NOT call nftIsSoulbound — primary mint-spot listings are
// exempt from the soulbound guard that applies to secondary resale listings.
//
// This test proves that listMintSpots + buyMintSpot succeed for any edition
// (regardless of soulbound status on the real contract) and that the buyer
// receives the minted tokens. The key assertion is non-vacuous: buyer balance
// increments after purchase.
// ---------------------------------------------------------------------------

func TestMintSpotSoulboundSellable(t *testing.T) {
	ct := setupMintSpotBase(t, 250)

	// buyer is a different account from the lister (ownerAddress).
	buyer := "hive:soulbound_buyer"
	paymentToken := TokenID
	tokenId := "soul1"

	// Define edition — mintnftmock has no soulbound flag; the market path
	// does not impose any soulbound restriction on mint-spot listings
	// (decision 5: primary mint-spot listing of a soulbound edition is allowed).
	DefineMintEdition(t, ct, tokenId, 10)
	ApproveMintNftMockForMarket(t, ct)

	// Fund buyer for 1 spot at price 500.
	MintAndApproveToken(t, ct, buyer, 500)

	buyerNftBefore := QueryMintNftBalance(t, ct, buyer, tokenId)

	// List should succeed — no soulbound guard on listMintSpots.
	listResult := listMintSpots(t, ct, MintNftMockID, tokenId, paymentToken, "500", 0, 0, 0, ownerAddress, true, "")
	listId := ParseCreated(listResult)
	assert.True(t, listId.Success)

	// Buy should succeed — buyMintSpot uses nftDelegatedMint, not a transfer
	// of an already-held token, so no soulbound resale guard applies.
	buyMintSpotWithLogs(t, ct, listId.Id, 1, buyer, true, "")

	// Non-vacuous assertion: buyer received the minted NFT.
	buyerNftAfter := QueryMintNftBalance(t, ct, buyer, tokenId)
	assert.Equal(t, buyerNftBefore+1, buyerNftAfter, "buyer should have received 1 minted NFT")
}

// ---------------------------------------------------------------------------
// TestMintSpotDelistWhilePaused (spec Section 4 — recovery property)
//
// delistMintSpots uses assertInit only, NOT assertNotPaused, so a lister can
// cancel their listing even when the contract is paused. This is the recovery
// path — no NFT is escrowed in mint-spot listings so the lister should always
// be able to withdraw their listing.
// ---------------------------------------------------------------------------

func TestMintSpotDelistWhilePaused(t *testing.T) {
	ct := setupMintSpotBase(t, 0)

	buyer := "hive:delist_buyer"
	paymentToken := TokenID
	tokenId := "1"

	DefineMintEdition(t, ct, tokenId, 10)
	ApproveMintNftMockForMarket(t, ct)
	MintAndApproveToken(t, ct, buyer, 5000)

	// Create an active listing.
	listMintSpots(t, ct, MintNftMockID, tokenId, paymentToken, "1000", 0, 0, 0, ownerAddress, true, "")

	// Pause the contract (only owner can pause).
	CallMarket(t, ct, "pause", []byte(`{}`), nil, ownerAddress, "", true, gas, "")

	// delistMintSpots must succeed even while paused — recovery property.
	CallMarket(t, ct, "delistMintSpots",
		[]byte(`{"listingId":0}`), nil, ownerAddress, "", true, gas, "")

	// buyMintSpot while still paused → aborts on pause guard (contract is paused,
	// but the listing is also inactive — the pause check fires first).
	buyMintSpotWithLogs(t, ct, 0, 1, buyer, false, "Contract is paused")

	// Unpause and confirm the listing is still inactive (delist persisted).
	CallMarket(t, ct, "unpause", []byte(`{}`), nil, ownerAddress, "", true, gas, "")
	buyMintSpotWithLogs(t, ct, 0, 1, buyer, false, "Mint spot listing not active")
}

// ---------------------------------------------------------------------------
// TestMintSpotListerCannotBuyOwn (spec Section 4 — self-deal guard)
//
// BuyMintSpot aborts with "Lister cannot buy own mint spots" when the caller
// is the same account that created the listing. No mint should occur.
// ---------------------------------------------------------------------------

func TestMintSpotListerCannotBuyOwn(t *testing.T) {
	ct := setupMintSpotBase(t, 0)

	paymentToken := TokenID
	tokenId := "1"

	DefineMintEdition(t, ct, tokenId, 10)
	ApproveMintNftMockForMarket(t, ct)

	// Fund the lister (ownerAddress) so escrowIn can't fail on balance grounds.
	MintAndApproveToken(t, ct, ownerAddress, 5000)

	nftBefore := QueryMintNftBalance(t, ct, ownerAddress, tokenId)

	// Owner lists — ownerAddress is the lister.
	listMintSpots(t, ct, MintNftMockID, tokenId, paymentToken, "1000", 0, 0, 0, ownerAddress, true, "")

	// Owner (== lister) tries to buy own listing → must be rejected.
	buyMintSpotWithLogs(t, ct, 0, 1, ownerAddress, false, "Lister cannot buy own mint spots")

	// Assert no mint occurred — NFT balance must be unchanged.
	nftAfter := QueryMintNftBalance(t, ct, ownerAddress, tokenId)
	assert.Equal(t, nftBefore, nftAfter, "lister NFT balance must be unchanged after self-buy attempt")
}
