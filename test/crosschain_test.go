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

// ===========================================================================
// Task F2: DEX-routed settlement
//
// paymentToken == contract:dexmock (so escrowIn calls transferFrom on dexmock,
// which moves buyer → market on the a-<acct> ledger). dexPool == same
// contract:dexmock address (the mock IS both the payment token and the pool).
// swap() debits market's balance by amount_in and credits seller by out
// (95% of amount_in). settleToken is a nominal id "settleasset" — the mock
// ignores it and just moves the dexmock ledger, which is sufficient to prove
// the swap call path and the slippage guard.
// ===========================================================================

// CallDexMock calls the DEX mock contract.
func CallDexMock(t *testing.T, ct *test_utils.ContractTest, action string, payload []byte, authUser string, expectOk bool, expectedOutput string) test_utils.ContractTestCallResult {
	cr, _, _ := callContract(t, ct, DexMockID, action, payload, nil, authUser, defaultTimestamp, expectOk, gas, expectedOutput)
	return cr
}

// MintDexMock credits `amount` dexmock coins to `to`.
func MintDexMock(t *testing.T, ct *test_utils.ContractTest, to string, amount uint64) {
	CallDexMock(t, ct, "mint",
		[]byte(fmt.Sprintf(`{"to":"%s","amount":"%d"}`, to, amount)),
		ownerAddress, true, "")
}

// QueryDexMockBalance queries the TEST-ONLY `bal` entrypoint on dexmock.
func QueryDexMockBalance(t *testing.T, ct *test_utils.ContractTest, account string) uint64 {
	cr := CallDexMock(t, ct, "bal",
		[]byte(fmt.Sprintf(`{"account":"%s"}`, account)),
		"hive:anyone", true, "")
	return ParseBalance(cr)
}

// dexMockSetup initialises NFT + a marketplace + registers dexmock as payment token.
// Returns ct with dexmock initialised.
func dexMockSetup(t *testing.T, feeBps uint64) *test_utils.ContractTest {
	ct := SetupContractTest()
	InitNft(t, ct)
	CallMarket(t, ct, "init",
		[]byte(fmt.Sprintf(`{"feeBps":%d,"feeRecipient":"%s"}`, feeBps, feeRecipientAddress)),
		nil, ownerAddress, "", true, gas, "")
	CallMarket(t, ct, "addPaymentToken",
		[]byte(fmt.Sprintf(`{"token":"%s"}`, DexMockID)),
		nil, ownerAddress, "", true, gas, "")
	CallDexMock(t, ct, "init",
		[]byte(fmt.Sprintf(`{"owner":"%s"}`, ownerAddress)),
		ownerAddress, true, "")
	return ct
}

// ---------------------------------------------------------------------------
// TestDexRoutedSettlement: happy path — settleToken set → swap called, seller
// receives out (≈95% of sellerNet), fee/royalty recipients get paymentToken,
// NFT → buyer, market residual 0.
// ---------------------------------------------------------------------------

func TestDexRoutedSettlement(t *testing.T) {
	ct := dexMockSetup(t, 250)

	seller := ownerAddress
	buyer := "hive:buyer"
	royaltyRecip := "hive:royaltyrecip"

	// Set royalty: 200 bps.
	CallMarket(t, ct, "setRoyalty",
		[]byte(fmt.Sprintf(`{"nftContract":"%s","royaltyBps":200,"royaltyRecipient":"%s"}`, NftContractID, royaltyRecip)),
		nil, ownerAddress, "", true, gas, "")

	// Mint dexmock to buyer: 100000 units.
	MintDexMock(t, ct, buyer, 100000)

	// Seller mints NFT and approves market.
	MintNft(t, ct, seller, "1", 1, 1)
	ApproveNftForMarket(t, ct, seller)

	// Seller lists with settleToken + dexPool (dexmock serves as both).
	// pricePerUnit=10000; no transfer fee in dexmock → received = 10000.
	// fee (250 bps): floor(10000*250/10000) = 250
	// royalty (200 bps): floor(10000*200/10000) = 200
	// sellerNet = 10000 - 250 - 200 = 9550
	// out = floor(9550 * 95 / 100) = floor(9072.5) = 9072
	// minSettleOut set at 9000 (< 9072 → no slippage abort).
	listPayload := fmt.Sprintf(
		`{"nftContract":"%s","tokenId":"1","amount":1,"paymentToken":"%s","pricePerUnit":"10000","dexPool":"%s","settleToken":"settleasset","minSettleOut":"9000"}`,
		NftContractID, DexMockID, DexMockID)
	CallMarket(t, ct, "list", []byte(listPayload), nil, seller, "", true, gas, "")

	sellerBefore := QueryDexMockBalance(t, ct, seller)
	feeRecipBefore := QueryDexMockBalance(t, ct, feeRecipientAddress)
	royaltyBefore := QueryDexMockBalance(t, ct, royaltyRecip)
	buyerBefore := QueryDexMockBalance(t, ct, buyer)

	CallMarket(t, ct, "buy", []byte(`{"listingId":0,"amount":1}`), nil, buyer, "", true, gas, "")

	sellerAfter := QueryDexMockBalance(t, ct, seller)
	feeRecipAfter := QueryDexMockBalance(t, ct, feeRecipientAddress)
	royaltyAfter := QueryDexMockBalance(t, ct, royaltyRecip)
	buyerAfter := QueryDexMockBalance(t, ct, buyer)

	// Buyer paid 10000 (no transfer fee in dexmock).
	assert.Equal(t, uint64(10000), buyerBefore-buyerAfter, "buyer debited 10000")

	// Fee recipient received 250 (paymentToken = dexmock, no transfer fee → full 250).
	assert.Equal(t, uint64(250), feeRecipAfter-feeRecipBefore, "fee recipient received 250")

	// Royalty recipient received 200.
	assert.Equal(t, uint64(200), royaltyAfter-royaltyBefore, "royalty recipient received 200")

	// Seller received out = floor(9550 * 95 / 100) = 9072 (via swap, NOT raw sellerNet).
	assert.Equal(t, uint64(9072), sellerAfter-sellerBefore, "seller received dex-swapped out 9072")

	// NFT delivered to buyer.
	assert.Equal(t, uint64(1), QueryNftBalance(t, ct, buyer, "1"), "buyer received NFT")

	// Market residual should be 0:
	// escrowIn: market +10000
	// fee pay: market -250
	// royalty pay: market -200
	// dexSwapTo: market -9550 (debited by swap call), seller +9072
	// 10000 - 250 - 200 - 9550 = 0
	assert.Equal(t, uint64(0), QueryDexMockBalance(t, ct, MarketContractAddress), "market residual 0")
}

// ---------------------------------------------------------------------------
// TestDexSlippageAbortReverts: minSettleOut above achievable out → buy aborts
// with "slippage tolerance exceeded"; NOTHING moved (full atomic revert).
// ---------------------------------------------------------------------------

func TestDexSlippageAbortReverts(t *testing.T) {
	ct := dexMockSetup(t, 0)

	seller := ownerAddress
	buyer := "hive:buyer"

	MintDexMock(t, ct, buyer, 50000)
	MintNft(t, ct, seller, "1", 1, 1)
	ApproveNftForMarket(t, ct, seller)

	// pricePerUnit=10000, fee=0, royalty=0 → sellerNet=10000
	// out = floor(10000 * 95 / 100) = 9500
	// minSettleOut=9999 >> 9500 → slippage abort expected.
	listPayload := fmt.Sprintf(
		`{"nftContract":"%s","tokenId":"1","amount":1,"paymentToken":"%s","pricePerUnit":"10000","dexPool":"%s","settleToken":"settleasset","minSettleOut":"9999"}`,
		NftContractID, DexMockID, DexMockID)
	CallMarket(t, ct, "list", []byte(listPayload), nil, seller, "", true, gas, "")

	buyerBefore := QueryDexMockBalance(t, ct, buyer)
	sellerBefore := QueryDexMockBalance(t, ct, seller)

	// Buy must abort due to slippage.
	CallMarket(t, ct, "buy", []byte(`{"listingId":0,"amount":1}`), nil, buyer, "", false, gas, "slippage tolerance exceeded")

	// Full revert: buyer balance unchanged, seller balance unchanged.
	assert.Equal(t, buyerBefore, QueryDexMockBalance(t, ct, buyer), "buyer balance unchanged after revert")
	assert.Equal(t, sellerBefore, QueryDexMockBalance(t, ct, seller), "seller balance unchanged after revert")

	// NFT NOT transferred to buyer (revert).
	assert.Equal(t, uint64(0), QueryNftBalance(t, ct, buyer, "1"), "NFT not transferred after revert")

	// Market residual 0 (revert undid escrowIn).
	assert.Equal(t, uint64(0), QueryDexMockBalance(t, ct, MarketContractAddress), "market residual 0 after revert")
}

// ---------------------------------------------------------------------------
// TestSettleTokenDefaultUnchanged: omit settleToken → legacy seller payment
// path (regression: seller receives paymentToken normally, no swap).
// ---------------------------------------------------------------------------

func TestSettleTokenDefaultUnchanged(t *testing.T) {
	ct := dexMockSetup(t, 0)

	seller := ownerAddress
	buyer := "hive:buyer"

	MintDexMock(t, ct, buyer, 50000)
	MintNft(t, ct, seller, "1", 1, 1)
	ApproveNftForMarket(t, ct, seller)

	// List WITHOUT settleToken — legacy path (no dexPool / settleToken).
	listPayload := fmt.Sprintf(
		`{"nftContract":"%s","tokenId":"1","amount":1,"paymentToken":"%s","pricePerUnit":"1000"}`,
		NftContractID, DexMockID)
	CallMarket(t, ct, "list", []byte(listPayload), nil, seller, "", true, gas, "")

	sellerBefore := QueryDexMockBalance(t, ct, seller)
	CallMarket(t, ct, "buy", []byte(`{"listingId":0,"amount":1}`), nil, buyer, "", true, gas, "")
	sellerAfter := QueryDexMockBalance(t, ct, seller)

	// feeBps=0, no royalty → seller gets full 1000 (dexmock has no transfer fee).
	assert.Equal(t, uint64(1000), sellerAfter-sellerBefore, "seller received full payment in legacy path")
	assert.Equal(t, uint64(0), QueryDexMockBalance(t, ct, MarketContractAddress), "market residual 0")
}

// ---------------------------------------------------------------------------
// TestPayoutAndSettleMutuallyExclusiveAtList: listing with BOTH payoutMode:"unmap"
// and settleToken set → list aborts "payout and settleToken are mutually exclusive".
// ---------------------------------------------------------------------------

func TestPayoutAndSettleMutuallyExclusiveAtList(t *testing.T) {
	ct := SetupContractTest()
	InitNft(t, ct)
	CallMarket(t, ct, "init",
		[]byte(fmt.Sprintf(`{"feeBps":0,"feeRecipient":"%s"}`, feeRecipientAddress)),
		nil, ownerAddress, "", true, gas, "")
	// Register both payment tokens.
	CallMarket(t, ct, "addPaymentToken",
		[]byte(fmt.Sprintf(`{"token":"%s"}`, UtxoMockID)),
		nil, ownerAddress, "", true, gas, "")
	CallMarket(t, ct, "addPaymentToken",
		[]byte(fmt.Sprintf(`{"token":"%s"}`, DexMockID)),
		nil, ownerAddress, "", true, gas, "")

	seller := ownerAddress
	MintNft(t, ct, seller, "1", 1, 1)
	ApproveNftForMarket(t, ct, seller)

	// List with BOTH payoutMode:"unmap" AND settleToken — must abort.
	listPayload := fmt.Sprintf(
		`{"nftContract":"%s","tokenId":"1","amount":1,"paymentToken":"%s","pricePerUnit":"1000","payoutMode":"unmap","payoutL1Address":"bc1qexample","dexPool":"%s","settleToken":"settleasset","minSettleOut":"0"}`,
		NftContractID, UtxoMockID, DexMockID)
	CallMarket(t, ct, "list", []byte(listPayload), nil, seller, "", false, gas,
		"payout and settleToken are mutually exclusive")
}

// ---------------------------------------------------------------------------
// TestSettleTokenInjectionRejected: dexPool or settleToken containing a
// double-quote → list aborts "...contains invalid characters".
// ---------------------------------------------------------------------------

func TestSettleTokenInjectionRejected(t *testing.T) {
	setupCt := func() *test_utils.ContractTest {
		ct := SetupContractTest()
		InitNft(t, ct)
		CallMarket(t, ct, "init",
			[]byte(fmt.Sprintf(`{"feeBps":0,"feeRecipient":"%s"}`, feeRecipientAddress)),
			nil, ownerAddress, "", true, gas, "")
		CallMarket(t, ct, "addPaymentToken",
			[]byte(fmt.Sprintf(`{"token":"%s"}`, DexMockID)),
			nil, ownerAddress, "", true, gas, "")
		return ct
	}

	seller := ownerAddress

	// --- settleToken with injection character ---
	ct1 := setupCt()
	MintNft(t, ct1, seller, "1", 1, 1)
	ApproveNftForMarket(t, ct1, seller)
	// JSON-encode an injection string to get a literal '"' inside the field value.
	settleTokenInjection := `settleasset","asset_in":"injected`
	settleTokenJSON, _ := json.Marshal(settleTokenInjection)
	listPayload1 := fmt.Sprintf(
		`{"nftContract":"%s","tokenId":"1","amount":1,"paymentToken":"%s","pricePerUnit":"1000","dexPool":"%s","settleToken":%s,"minSettleOut":"1"}`,
		NftContractID, DexMockID, DexMockID, string(settleTokenJSON))
	CallMarket(t, ct1, "list", []byte(listPayload1), nil, seller, "", false, gas,
		"settleToken contains invalid characters")

	// --- dexPool with injection character ---
	ct2 := setupCt()
	MintNft(t, ct2, seller, "1", 1, 1)
	ApproveNftForMarket(t, ct2, seller)
	dexPoolInjection := `contract:dexmock","swap":"injected`
	dexPoolJSON, _ := json.Marshal(dexPoolInjection)
	listPayload2 := fmt.Sprintf(
		`{"nftContract":"%s","tokenId":"1","amount":1,"paymentToken":"%s","pricePerUnit":"1000","dexPool":%s,"settleToken":"settleasset","minSettleOut":"1"}`,
		NftContractID, DexMockID, string(dexPoolJSON))
	CallMarket(t, ct2, "list", []byte(listPayload2), nil, seller, "", false, gas,
		"dexPool contains invalid characters")

	// --- minSettleOut:"0" with settleToken set must abort (Fix I1) ---
	ct3zero := setupCt()
	MintNft(t, ct3zero, seller, "1", 1, 1)
	ApproveNftForMarket(t, ct3zero, seller)
	listPayload3zero := fmt.Sprintf(
		`{"nftContract":"%s","tokenId":"1","amount":1,"paymentToken":"%s","pricePerUnit":"1000","dexPool":"%s","settleToken":"settleasset","minSettleOut":"0"}`,
		NftContractID, DexMockID, DexMockID)
	CallMarket(t, ct3zero, "list", []byte(listPayload3zero), nil, seller, "", false, gas,
		"minSettleOut must be greater than zero")

	// --- valid dexPool + settleToken with non-zero minSettleOut must be accepted ---
	ct3 := setupCt()
	MintNft(t, ct3, seller, "1", 1, 1)
	ApproveNftForMarket(t, ct3, seller)
	listPayload3 := fmt.Sprintf(
		`{"nftContract":"%s","tokenId":"1","amount":1,"paymentToken":"%s","pricePerUnit":"1000","dexPool":"%s","settleToken":"settleasset","minSettleOut":"1"}`,
		NftContractID, DexMockID, DexMockID)
	CallMarket(t, ct3, "list", []byte(listPayload3), nil, seller, "", true, gas, "")
}

// ---------------------------------------------------------------------------
// TestSettleTokenMissingMinSettleOutRejected: list with settleToken+dexPool
// but minSettleOut absent/empty → list aborts at list time (fund-critical:
// a zero/empty minSettleOut would allow unlimited slippage on the fund path).
// ---------------------------------------------------------------------------

func TestSettleTokenMissingMinSettleOutRejected(t *testing.T) {
	ct := SetupContractTest()
	InitNft(t, ct)
	CallMarket(t, ct, "init",
		[]byte(fmt.Sprintf(`{"feeBps":0,"feeRecipient":"%s"}`, feeRecipientAddress)),
		nil, ownerAddress, "", true, gas, "")
	CallMarket(t, ct, "addPaymentToken",
		[]byte(fmt.Sprintf(`{"token":"%s"}`, DexMockID)),
		nil, ownerAddress, "", true, gas, "")

	seller := ownerAddress
	MintNft(t, ct, seller, "1", 1, 1)
	ApproveNftForMarket(t, ct, seller)

	// minSettleOut absent (field omitted) → parseMoney("") aborts "amount required",
	// or Fix I1's positivity guard aborts "minSettleOut must be greater than zero" —
	// either way it must be a list-time abort (expectedResult=false asserts abort).
	listPayload := fmt.Sprintf(
		`{"nftContract":"%s","tokenId":"1","amount":1,"paymentToken":"%s","pricePerUnit":"1000","dexPool":"%s","settleToken":"settleasset"}`,
		NftContractID, DexMockID, DexMockID)
	CallMarket(t, ct, "list", []byte(listPayload), nil, seller, "", false, gas, "")
}
