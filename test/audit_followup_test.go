package contract_test

import (
	"fmt"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Batch-size cap (audit fix #1): batchList / batchBuy reject > 20 items.
// ---------------------------------------------------------------------------

func TestBatchListCapAborts(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	// 21 minimal list items — the cap check runs before the per-item loop, so
	// the items don't need to be valid; the abort must fire on count alone.
	items := make([]string, 21)
	for i := range items {
		items[i] = fmt.Sprintf(`{"nftContract":"%s","tokenId":"%d","amount":1,"paymentToken":"%s","pricePerUnit":"1000"}`, NftContractID, i, TokenID)
	}
	payload := `{"items":[` + strings.Join(items, ",") + `]}`
	CallMarket(t, ct, "batchList", []byte(payload), nil, ownerAddress, "", false, gas, "Too many items")
}

func TestBatchBuyCapAborts(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	items := make([]string, 21)
	for i := range items {
		items[i] = fmt.Sprintf(`{"listingId":%d,"amount":1}`, i)
	}
	payload := `{"items":[` + strings.Join(items, ",") + `]}`
	CallMarket(t, ct, "batchBuy", []byte(payload), nil, "hive:buyer", "", false, gas, "Too many items")
}

// ---------------------------------------------------------------------------
// Min-bid-increment floor (audit fix #2): even with no explicit min increment
// set, placeBid enforces a 1% floor so anti-snipe can't be griefed by +1 bids.
// ---------------------------------------------------------------------------

func TestMinBidIncrementFloorEnforced(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	MintNft(t, ct, ownerAddress, "1", 1, 1)
	ApproveNftForMarket(t, ct, ownerAddress)

	ct.BlockHeight = 100
	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":1,"paymentToken":"%s","auctionType":"english","startPrice":"1000","endPrice":"0","startBlock":100,"endBlock":200}`, NftContractID, TokenID)
	CallMarket(t, ct, "createAuction", []byte(payload), nil, ownerAddress, "", true, gas, "")

	b1 := "hive:bidder1"
	b2 := "hive:bidder2"
	MintAndApproveToken(t, ct, b1, 5000)
	MintAndApproveToken(t, ct, b2, 5000)

	ct.BlockHeight = 120
	// Opening bid at reserve.
	CallMarket(t, ct, "placeBid", []byte(`{"auctionId":0,"bidAmount":"1000"}`), nil, b1, "", true, gas, "")
	// +0.5% (1005) is below the 1% floor (min would be 1010) → rejected, even
	// though no explicit min increment was ever configured.
	CallMarket(t, ct, "placeBid", []byte(`{"auctionId":0,"bidAmount":"1005"}`), nil, b2, "", false, gas, "minimum increment")
	// Exactly +1% (1010) clears the floor.
	CallMarket(t, ct, "placeBid", []byte(`{"auctionId":0,"bidAmount":"1010"}`), nil, b2, "", true, gas, "")
}

// ---------------------------------------------------------------------------
// Mint-spot cap overflow (audit fix #3): a huge `amount` must not wrap past
// the per-listing maxSpots cap.
// ---------------------------------------------------------------------------

func TestMintSpotCapOverflowSafe(t *testing.T) {
	ct := setupMintSpotBase(t, 0)
	buyer := "hive:buyer"
	DefineMintEdition(t, ct, "1", 5)
	ApproveMintNftMockForMarket(t, ct)
	MintAndApproveToken(t, ct, buyer, 1000)

	listMintSpots(t, ct, MintNftMockID, "1", TokenID, "100", 5, 0, 0, ownerAddress, true, "")
	// Legitimately consume 3 of 5 spots so sold=3.
	buyMintSpot(t, ct, 0, 3, buyer, true, "")

	// amount = 2^64 - 3 → old `sold+amount` (= 3 + 2^64-3) wraps to 0 (≤5) and
	// would bypass the cap; the overflow-safe `amount > ms-sold` form aborts.
	huge := "18446744073709551613"
	CallMarket(t, ct, "buyMintSpot", []byte(fmt.Sprintf(`{"listingId":0,"amount":%s}`, huge)), nil, buyer, "", false, gas, "Exceeds listing mint-spot cap")
}

// ---------------------------------------------------------------------------
// Payment-token decoder registry (audit fix #4): addPaymentToken validates the
// optional decoder field.
// ---------------------------------------------------------------------------

func TestAddPaymentTokenDecoderValidation(t *testing.T) {
	ct := SetupContractTest()
	InitMarket(t, ct)

	// Unknown decoder is rejected.
	CallMarket(t, ct, "addPaymentToken",
		[]byte(`{"token":"vsc1Bexample","decoder":"bogus"}`), nil, ownerAddress, "", false, gas, "Invalid decoder")
	// Known decoders are accepted.
	CallMarket(t, ct, "addPaymentToken",
		[]byte(`{"token":"vsc1Bexample","decoder":"magi_token"}`), nil, ownerAddress, "", true, gas, "")
	CallMarket(t, ct, "addPaymentToken",
		[]byte(`{"token":"vsc1Butxo","decoder":"utxo"}`), nil, ownerAddress, "", true, gas, "")
	// Omitted decoder still works (legacy auto-probe).
	CallMarket(t, ct, "addPaymentToken",
		[]byte(`{"token":"vsc1Bauto"}`), nil, ownerAddress, "", true, gas, "")
}
