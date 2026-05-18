package contract_test

// ============================================================================
// Cross-chain settlement tests — Task F1: native L1 payout via utxo unmap.
//
// FUND-CRITICAL: these tests prove that when a listing is created with
// payoutMode:"unmap" + payoutL1Address, the seller's net payment is sent to
// the L1 address via the utxo-mapping contract's `unmap` entrypoint (which
// debits the CALLER's — i.e. the marketplace's — balance), rather than via a
// mapped-token transfer to the seller's Hive account.
//
// Fee and royalty recipients STILL receive the mapped token as in the default
// path. Only the seller-leg destination changes when pm=="unmap".
//
// The utxomock now has an `unmap` entrypoint that debits the caller's balance
// and records the payout under "unmapped|<l1addr>" for test assertions via
// the TEST-ONLY `getUnmapped {addr}` query (returns {"balance":"<dec>"},
// reusing ParseBalance).
// ============================================================================

import (
	"encoding/json"
	"fmt"
	"testing"

	"vsc-node/lib/test_utils"

	"github.com/stretchr/testify/assert"
)

// QueryUnmapped reads the total unmap payout recorded for an L1 address via
// the TEST-ONLY `getUnmapped {addr}` entrypoint on utxomock.
func QueryUnmapped(t *testing.T, ct *test_utils.ContractTest, addr string) uint64 {
	cr := CallUtxoMock(t, ct, "getUnmapped",
		[]byte(fmt.Sprintf(`{"addr":"%s"}`, addr)),
		"hive:anyone", true, "")
	return ParseBalance(cr)
}

// ---------------------------------------------------------------------------
// TestUnmapPayoutOnSale: happy path — payoutMode:"unmap" sends seller's net
// to L1 via unmap; fee/royalty recipients get mapped token; NFT → buyer;
// market residual 0.
// ---------------------------------------------------------------------------

func TestUnmapPayoutOnSale(t *testing.T) {
	ct := SetupContractTest()

	seller := ownerAddress
	buyer := "hive:buyer"
	royaltyRecip := "hive:royaltyrecip"
	l1addr := "bc1qexampleexampleexample"

	// Init market (250 bps fee), NFT, utxomock.
	InitNft(t, ct)
	CallMarket(t, ct, "init",
		[]byte(fmt.Sprintf(`{"feeBps":250,"feeRecipient":"%s"}`, feeRecipientAddress)),
		nil, ownerAddress, "", true, gas, "")
	CallMarket(t, ct, "addPaymentToken",
		[]byte(fmt.Sprintf(`{"token":"%s"}`, UtxoMockID)),
		nil, ownerAddress, "", true, gas, "")
	CallUtxoMock(t, ct, "init",
		[]byte(fmt.Sprintf(`{"owner":"%s"}`, ownerAddress)),
		ownerAddress, true, "")

	// Set royalty: 200 bps to royaltyRecip.
	CallMarket(t, ct, "setRoyalty",
		[]byte(fmt.Sprintf(`{"nftContract":"%s","royaltyBps":200,"royaltyRecipient":"%s"}`, NftContractID, royaltyRecip)),
		nil, ownerAddress, "", true, gas, "")

	// Mint utxomock to buyer: 100000 units.
	MintUtxoMock(t, ct, buyer, 100000)

	// Seller mints NFT and approves market.
	MintNft(t, ct, seller, "1", 1, 1)
	ApproveNftForMarket(t, ct, seller)

	// List with payoutMode:"unmap" and payoutL1Address.
	listPayload := fmt.Sprintf(
		`{"nftContract":"%s","tokenId":"1","amount":1,"paymentToken":"%s","pricePerUnit":"10000","payoutMode":"unmap","payoutL1Address":"%s"}`,
		NftContractID, UtxoMockID, l1addr)
	CallMarket(t, ct, "list", []byte(listPayload), nil, seller, "", true, gas, "")

	sellerBefore := QueryUtxoMockBalance(t, ct, seller)

	// Buyer buys the listing.
	_, _, logs := CallMarket(t, ct, "buy", []byte(`{"listingId":0,"amount":1}`), nil, buyer, "", true, gas, "")

	// Accounting:
	//   nominal price = 10000
	//   received (balance-delta via utxomock 1% deduct on transferFrom):
	//     = 10000 - floor(10000/100) = 10000 - 100 = 9900
	//   fee (250 bps of received): floor(9900 * 250 / 10000) = floor(247.5) = 247
	//   royalty (200 bps of received): floor(9900 * 200 / 10000) = floor(198) = 198
	//   sellerNet = 9900 - 247 - 198 = 9455
	//
	// payoutMode == "unmap":
	//   seller's mapped balance is UNCHANGED (no tokenTransfer to seller)
	//   utxomock.unmap debits market's balance (9455) and records unmapped|l1addr += 9455
	//   fee recipient gets tokenTransfer(247), taxed: 247 - floor(247/100) = 247 - 2 = 245
	//   royalty recipient gets tokenTransfer(198), taxed: 198 - floor(198/100) = 198 - 1 = 197

	sellerAfter := QueryUtxoMockBalance(t, ct, seller)
	assert.Equal(t, sellerBefore, sellerAfter, "seller did NOT receive mapped token in unmap mode")

	// The utxomock should have recorded the unmap for l1addr == sellerNet == 9455.
	unmapRecorded := QueryUnmapped(t, ct, l1addr)
	assert.Equal(t, uint64(9455), unmapRecorded, "utxomock recorded unmap of sellerNet 9455 for l1addr")

	// Fee recipient received mapped token (post-transfer-tax).
	feeRecipBalance := QueryUtxoMockBalance(t, ct, feeRecipientAddress)
	assert.Equal(t, uint64(245), feeRecipBalance, "fee recipient received fee 245 (247 - 2 tax)")

	// Royalty recipient received mapped token (post-transfer-tax).
	royaltyBalance := QueryUtxoMockBalance(t, ct, royaltyRecip)
	assert.Equal(t, uint64(197), royaltyBalance, "royalty recipient received royalty 197 (198 - 1 tax)")

	// NFT delivered to buyer.
	assert.Equal(t, uint64(1), QueryNftBalance(t, ct, buyer, "1"), "buyer received NFT")

	// Market residual should be 0.
	// After escrowIn: market holds 9900.
	// fee payout (247): market debits 247 (full), feeRecip credits 245 (taxed) → market: 9900-247=9653
	// royalty payout (198): market debits 198, royaltyRecip credits 197 → market: 9653-198=9455
	// unmap (9455): market debits 9455 → market: 0
	assert.Equal(t, uint64(0), QueryUtxoMockBalance(t, ct, MarketContractAddress), "market residual 0")

	// Bought event shows totalPrice = received = 9900.
	AssertEventContains(t, logs, "bought", `"totalPrice":"9900"`)
}

// ---------------------------------------------------------------------------
// TestUnmapDefaultUnchanged: omitting payoutMode → legacy seller payment path
// (regression: seller receives mapped token normally; 295 prior tests green).
// ---------------------------------------------------------------------------

func TestUnmapDefaultUnchanged(t *testing.T) {
	ct := SetupContractTest()

	seller := ownerAddress
	buyer := "hive:buyer"

	// Init market (feeBps:0), NFT, utxomock.
	InitNft(t, ct)
	CallMarket(t, ct, "init",
		[]byte(fmt.Sprintf(`{"feeBps":0,"feeRecipient":"%s"}`, feeRecipientAddress)),
		nil, ownerAddress, "", true, gas, "")
	CallMarket(t, ct, "addPaymentToken",
		[]byte(fmt.Sprintf(`{"token":"%s"}`, UtxoMockID)),
		nil, ownerAddress, "", true, gas, "")
	CallUtxoMock(t, ct, "init",
		[]byte(fmt.Sprintf(`{"owner":"%s"}`, ownerAddress)),
		ownerAddress, true, "")
	MintUtxoMock(t, ct, buyer, 100000)

	MintNft(t, ct, seller, "1", 1, 1)
	ApproveNftForMarket(t, ct, seller)

	// List WITHOUT payoutMode — legacy path.
	listPayload := fmt.Sprintf(
		`{"nftContract":"%s","tokenId":"1","amount":1,"paymentToken":"%s","pricePerUnit":"1000"}`,
		NftContractID, UtxoMockID)
	CallMarket(t, ct, "list", []byte(listPayload), nil, seller, "", true, gas, "")

	sellerBefore := QueryUtxoMockBalance(t, ct, seller)
	CallMarket(t, ct, "buy", []byte(`{"listingId":0,"amount":1}`), nil, buyer, "", true, gas, "")
	sellerAfter := QueryUtxoMockBalance(t, ct, seller)

	// feeBps:0 → seller gets the full post-escrow received (taxed on payout).
	// received = 1000 - 10 = 990; seller payout = 990; taxed: 990 - 9 = 981.
	assert.Equal(t, uint64(981), sellerAfter-sellerBefore, "seller received mapped token via legacy path (981)")
	assert.Greater(t, sellerAfter, sellerBefore, "seller balance increased (mapped token)")

	// No unmap was recorded (default path → no unmapTo call).
	unmapRecorded := QueryUnmapped(t, ct, "bc1qexampleexampleexample")
	assert.Equal(t, uint64(0), unmapRecorded, "no unmap recorded in default path")
}

// ---------------------------------------------------------------------------
// TestUnmapMissingL1Rejected: listing with payoutMode:"unmap" but empty
// payoutL1Address must abort with the expected error.
// ---------------------------------------------------------------------------

func TestUnmapMissingL1Rejected(t *testing.T) {
	ct := SetupContractTest()

	seller := ownerAddress

	InitNft(t, ct)
	CallMarket(t, ct, "init",
		[]byte(fmt.Sprintf(`{"feeBps":0,"feeRecipient":"%s"}`, feeRecipientAddress)),
		nil, ownerAddress, "", true, gas, "")
	CallMarket(t, ct, "addPaymentToken",
		[]byte(fmt.Sprintf(`{"token":"%s"}`, UtxoMockID)),
		nil, ownerAddress, "", true, gas, "")

	MintNft(t, ct, seller, "1", 1, 1)
	ApproveNftForMarket(t, ct, seller)

	// List with payoutMode:"unmap" but NO payoutL1Address → must abort.
	listPayload := fmt.Sprintf(
		`{"nftContract":"%s","tokenId":"1","amount":1,"paymentToken":"%s","pricePerUnit":"1000","payoutMode":"unmap"}`,
		NftContractID, UtxoMockID)
	CallMarket(t, ct, "list", []byte(listPayload), nil, seller, "", false, gas,
		"payoutL1Address required for unmap payout")
}

// ---------------------------------------------------------------------------
// TestUnmapL1AddressRejectsInjection: a payoutL1Address containing a
// double-quote (or any character outside [a-zA-Z0-9:-]) must be rejected at
// list time so it cannot inject a duplicate field into the utxo unmap payload.
// Valid addresses (bech32, dash, hive:-prefixed) must still be accepted.
// ---------------------------------------------------------------------------

func TestUnmapL1AddressRejectsInjection(t *testing.T) {
	setupCt := func() *test_utils.ContractTest {
		ct := SetupContractTest()
		InitNft(t, ct)
		CallMarket(t, ct, "init",
			[]byte(fmt.Sprintf(`{"feeBps":0,"feeRecipient":"%s"}`, feeRecipientAddress)),
			nil, ownerAddress, "", true, gas, "")
		CallMarket(t, ct, "addPaymentToken",
			[]byte(fmt.Sprintf(`{"token":"%s"}`, UtxoMockID)),
			nil, ownerAddress, "", true, gas, "")
		return ct
	}

	seller := ownerAddress

	// --- injection payload: double-quote breaks out of the JSON string field ---
	// Build a well-formed JSON payload where the address value contains a literal
	// double-quote (JSON-encoded as \"). The WASM deserialiser will decode it back
	// to the raw string with the '"' character, which the allowlist must then reject.
	ct1 := setupCt()
	MintNft(t, ct1, seller, "1", 1, 1)
	ApproveNftForMarket(t, ct1, seller)
	injectionAddr := `bc1qfoo","amount":"0`
	injectionAddrJSON, _ := json.Marshal(injectionAddr) // → "bc1qfoo\",\"amount\":\"0"
	listPayload := fmt.Sprintf(
		`{"nftContract":"%s","tokenId":"1","amount":1,"paymentToken":"%s","pricePerUnit":"1000","payoutMode":"unmap","payoutL1Address":%s}`,
		NftContractID, UtxoMockID, string(injectionAddrJSON))
	CallMarket(t, ct1, "list", []byte(listPayload), nil, seller, "", false, gas,
		"payoutL1Address contains invalid characters")

	// --- valid plain bech32 address must still be accepted ---
	ct2 := setupCt()
	MintNft(t, ct2, seller, "1", 1, 1)
	ApproveNftForMarket(t, ct2, seller)
	validAddr := "bc1qexampleexampleexample"
	listPayload2 := fmt.Sprintf(
		`{"nftContract":"%s","tokenId":"1","amount":1,"paymentToken":"%s","pricePerUnit":"1000","payoutMode":"unmap","payoutL1Address":"%s"}`,
		NftContractID, UtxoMockID, validAddr)
	CallMarket(t, ct2, "list", []byte(listPayload2), nil, seller, "", true, gas, "")

	// --- valid colon-prefixed address (hive: style) must still be accepted ---
	ct3 := setupCt()
	MintNft(t, ct3, seller, "1", 1, 1)
	ApproveNftForMarket(t, ct3, seller)
	colonAddr := "hive:seller"
	listPayload3 := fmt.Sprintf(
		`{"nftContract":"%s","tokenId":"1","amount":1,"paymentToken":"%s","pricePerUnit":"1000","payoutMode":"unmap","payoutL1Address":"%s"}`,
		NftContractID, UtxoMockID, colonAddr)
	CallMarket(t, ct3, "list", []byte(listPayload3), nil, seller, "", true, gas, "")
}
