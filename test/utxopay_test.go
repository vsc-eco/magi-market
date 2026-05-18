package contract_test

import (
	"fmt"
	"testing"

	"vsc-node/lib/test_utils"

	"github.com/stretchr/testify/assert"
)

// ============================================================================
// UTXO-mapping payment-token tests.
//
// FUND-CRITICAL: these prove a UTXO-mapping-style coin (no balanceOf
// entrypoint, balances stored only under the raw `a-<acct>` BE-u64 state key)
// is usable end-to-end as a magi-market payment token. This is the concrete
// payoff of switching every cross-contract balance read to the raw
// sdk.ContractStateGet (contracts.read) path: magi-market's new
// tokenBalanceOf probes `bal|<acct>` (magi_token form, ABSENT on this mock)
// and falls back to raw-reading `a-<acct>` and decoding it via decodeUtxoU64.
//
// The mock (test/mocks/utxomock) mirrors utxo-mapping's getAccBal/setAccBal
// byte logic verbatim and applies a 1% (floor) deduct fee on every
// transfer/transferFrom hop — exactly the utxo-mapping unmap deduct_fee
// behaviour. It has NO balanceOf method; balances are observable to the test
// ONLY through the TEST-ONLY `tbal` query (distinct name, never called by
// magi-market). So every balance the marketplace acts on is proof the raw
// `a-<acct>` BE-u64 read path works.
//
// The market is initialised with feeBps:0 so the token's 1% transfer fee is
// the ONLY value loss, making every balance-delta unambiguous. Because EVERY
// hop is taxed, a payout the market sends out is itself taxed again (same
// honest-accounting note as feetoken_test.go).
// ============================================================================

// QueryUtxoMockBalance reads the TEST-ONLY `tbal` query on the utxo mock
// (returns {"balance":"<dec>"}). magi-market never calls this entrypoint.
func QueryUtxoMockBalance(t *testing.T, ct *test_utils.ContractTest, account string) uint64 {
	cr := CallUtxoMock(t, ct, "tbal",
		[]byte(fmt.Sprintf(`{"account":"%s"}`, account)),
		"hive:anyone", true, "")
	return ParseBalance(cr)
}

// CallUtxoMock calls the UTXO-mapping-style mock token contract.
func CallUtxoMock(t *testing.T, ct *test_utils.ContractTest, action string, payload []byte, authUser string, expectOk bool, expectedOutput string) test_utils.ContractTestCallResult {
	cr, _, _ := callContract(t, ct, UtxoMockID, action, payload, nil, authUser, defaultTimestamp, expectOk, gas, expectedOutput)
	return cr
}

// MintUtxoMock credits `amount` mock coins to `to`.
func MintUtxoMock(t *testing.T, ct *test_utils.ContractTest, to string, amount uint64) {
	CallUtxoMock(t, ct, "mint",
		[]byte(fmt.Sprintf(`{"to":"%s","amount":"%d"}`, to, amount)),
		ownerAddress, true, "")
}

// initUtxoPaySetup initialises NFT + a feeBps:0 marketplace, registers the
// utxo mock as an allowed payment token, and inits + mints the mock.
func initUtxoPaySetup(t *testing.T, ct *test_utils.ContractTest, fundAccount string, fundAmount uint64) {
	InitNft(t, ct)
	// feeBps:0 isolates the token's 1% transfer fee as the only value loss.
	CallMarket(t, ct, "init",
		[]byte(fmt.Sprintf(`{"feeBps":0,"feeRecipient":"%s"}`, feeRecipientAddress)),
		nil, ownerAddress, "", true, gas, "")
	CallMarket(t, ct, "addPaymentToken",
		[]byte(fmt.Sprintf(`{"token":"%s"}`, UtxoMockID)),
		nil, ownerAddress, "", true, gas, "")
	CallUtxoMock(t, ct, "init",
		[]byte(fmt.Sprintf(`{"owner":"%s"}`, ownerAddress)),
		ownerAddress, true, "")
	MintUtxoMock(t, ct, fundAccount, fundAmount)
}

// ---------------------------------------------------------------------------
// (a) BUY a listing paid with the UTXO-mapped coin.
// ---------------------------------------------------------------------------

func TestUtxoPayBuyDistributesReceived(t *testing.T) {
	ct := SetupContractTest()

	seller := ownerAddress
	buyer := "hive:buyer"

	initUtxoPaySetup(t, ct, buyer, 100000)
	MintNft(t, ct, seller, "1", 1, 1)
	ApproveNftForMarket(t, ct, seller)

	// List 1 NFT at 1000, payable in the utxo-mapped coin.
	listPayload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":1,"paymentToken":"%s","pricePerUnit":"1000"}`, NftContractID, UtxoMockID)
	CallMarket(t, ct, "list", []byte(listPayload), nil, seller, "", true, gas, "")

	sellerBefore := QueryUtxoMockBalance(t, ct, seller)
	buyerBefore := QueryUtxoMockBalance(t, ct, buyer)

	_, _, logs := CallMarket(t, ct, "buy", []byte(`{"listingId":0,"amount":1}`), nil, buyer, "", true, gas, "")

	sellerAfter := QueryUtxoMockBalance(t, ct, seller)
	buyerAfter := QueryUtxoMockBalance(t, ct, buyer)

	// Buyer debited the FULL nominal 1000 (transferFrom debits `from` in full).
	assert.Equal(t, uint64(1000), buyerBefore-buyerAfter, "buyer debited the full nominal price")

	// escrowIn (raw-reading a-<market> before/after) measured received =
	// 1000 - floor(1000/100) = 990. feeBps:0 -> the market distributes that
	// 990 to the seller; that payout transfer is itself taxed:
	// 990 - floor(990/100) = 981.
	sellerGain := sellerAfter - sellerBefore
	assert.Equal(t, uint64(981), sellerGain, "seller credited post-payout-fee amount")
	assert.NotEqual(t, uint64(1000), sellerGain, "seller did NOT get the nominal 1000")
	assert.NotEqual(t, uint64(990), sellerGain, "payout hop is also taxed (981, not 990)")

	// The `bought` event records totalPrice = received (990), proving the
	// marketplace measured the balance-delta via the raw a-<acct> read path
	// (NOT the nominal 1000).
	AssertEventContains(t, logs, "bought", `"totalPrice":"990"`)

	// No funds stranded in the marketplace (raw a-<market> read == 0).
	assert.Equal(t, uint64(0), QueryUtxoMockBalance(t, ct, MarketContractAddress), "no residual in market")

	// Buyer received the NFT.
	assert.Equal(t, uint64(1), QueryNftBalance(t, ct, buyer, "1"))
}

// ---------------------------------------------------------------------------
// (b) OFFER happy path: makeOffer -> acceptOffer with the UTXO-mapped coin.
// ---------------------------------------------------------------------------

func TestUtxoPayOfferHappyPath(t *testing.T) {
	ct := SetupContractTest()

	seller := ownerAddress
	buyer := "hive:buyer"

	initUtxoPaySetup(t, ct, buyer, 100000)
	// Seller holds 10 units of token id "1".
	MintNft(t, ct, seller, "1", 10, 10)
	ApproveNftForMarket(t, ct, seller)

	buyerStart := QueryUtxoMockBalance(t, ct, buyer)

	// Offer 10 units at pricePerUnit 100 -> totalOffer = 1000. escrowIn pulls
	// 1000 from buyer (full nominal debit) but the market RECEIVES only
	// 1000 - floor(1000/100) = 990 (1% deduct). Stored esc = the raw-read
	// balance-delta 990, NOT the nominal 1000.
	offerPayload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":10,"paymentToken":"%s","pricePerUnit":"100"}`, NftContractID, UtxoMockID)
	CallMarket(t, ct, "makeOffer", []byte(offerPayload), nil, buyer, "", true, gas, "")

	// Buyer debited the full nominal 1000; market escrow holds received 990.
	assert.Equal(t, uint64(1000), buyerStart-QueryUtxoMockBalance(t, ct, buyer), "buyer debited full nominal offer")
	assert.Equal(t, uint64(990), QueryUtxoMockBalance(t, ct, MarketContractAddress), "market escrow holds the raw-read received 990")

	sellerBefore := QueryUtxoMockBalance(t, ct, seller)

	// Seller accepts 9 of the 10 units. totalPrice = pricePerUnit*acceptAmount
	// = 100*9 = 900, which is <= the post-fee escrowed 990 (a full 10-unit
	// accept = 1000 would exceed esc 990 and abort — that underfunded path is
	// already covered by feetoken_test.go). feeBps:0 -> seller payout = 900;
	// that payout transfer is itself taxed -> seller nets 900 - 9 = 891.
	_, _, logs := CallMarket(t, ct, "acceptOffer", []byte(`{"offerId":0,"amount":9}`), nil, seller, "", true, gas, "")

	sellerAfter := QueryUtxoMockBalance(t, ct, seller)
	sellerGain := sellerAfter - sellerBefore

	assert.Equal(t, uint64(891), sellerGain, "seller paid the post-fee distribution of totalPrice 900 (891)")
	assert.NotEqual(t, uint64(900), sellerGain, "payout hop is also taxed (891, not 900)")

	// offer_accepted event records totalPrice == 900 (price*acceptAmount, paid
	// out of the raw-read post-fee escrow).
	AssertEventContains(t, logs, "offer_accepted", `"totalPrice":"900"`)

	// 9 NFTs moved seller -> buyer; seller keeps the 1 un-accepted unit.
	assert.Equal(t, uint64(1), QueryNftBalance(t, ct, seller, "1"), "seller keeps the 1 un-accepted unit")
	assert.Equal(t, uint64(9), QueryNftBalance(t, ct, buyer, "1"), "buyer received the 9 accepted units")

	// Residual escrow = esc(990) - totalPrice(900) = 90 still held by market.
	assert.Equal(t, uint64(90), QueryUtxoMockBalance(t, ct, MarketContractAddress), "market holds residual esc 90")

	// Offer still active for the remaining 1 unit.
	r1, _, _ := CallMarket(t, ct, "getOffer", []byte(`{"offerId":0}`), nil, "hive:anyone", "", true, gas, "")
	assert.True(t, ParseOffer(r1).Active, "offer still active for remaining unit")

	// Buyer cancels the remainder: refunded residual esc 90 via a taxed
	// transfer -> buyer net-credited 90 - floor(90/100) = 90 (90/100 floors
	// to 0). This drains the market escrow to zero.
	balBeforeCancel := QueryUtxoMockBalance(t, ct, buyer)
	CallMarket(t, ct, "cancelOffer", []byte(`{"offerId":0}`), nil, buyer, "", true, gas, "")
	assert.Equal(t, uint64(90), QueryUtxoMockBalance(t, ct, buyer)-balBeforeCancel, "buyer refunded residual esc 90")
	assert.Equal(t, uint64(0), QueryUtxoMockBalance(t, ct, MarketContractAddress), "market escrow fully drained")

	// Offer now inactive.
	r2, _, _ := CallMarket(t, ct, "getOffer", []byte(`{"offerId":0}`), nil, "hive:anyone", "", true, gas, "")
	assert.False(t, ParseOffer(r2).Active, "offer inactive after cancel")
}

// ---------------------------------------------------------------------------
// (c) ENGLISH auction: createAuction -> placeBid -> settleAuction with the
// UTXO-mapped coin.
// ---------------------------------------------------------------------------

func TestUtxoPayEnglishAuction(t *testing.T) {
	ct := SetupContractTest()

	seller := ownerAddress
	bidder := "hive:bidder1"

	initUtxoPaySetup(t, ct, bidder, 100000)
	MintNft(t, ct, seller, "1", 1, 1)
	ApproveNftForMarket(t, ct, seller)

	ct.BlockHeight = 100
	auctionPayload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":1,"paymentToken":"%s","auctionType":"english","startPrice":"1000","endPrice":"0","startBlock":100,"endBlock":200}`, NftContractID, UtxoMockID)
	CallMarket(t, ct, "createAuction", []byte(auctionPayload), nil, seller, "", true, gas, "")

	ct.BlockHeight = 120

	// bidder bids 5000 -> escrowIn (raw a-<market> delta) receives
	// 5000 - floor(5000/100) = 4950, stored as the high bid.
	bBefore := QueryUtxoMockBalance(t, ct, bidder)
	CallMarket(t, ct, "placeBid", []byte(`{"auctionId":0,"bidAmount":"5000"}`), nil, bidder, "", true, gas, "")
	assert.Equal(t, uint64(5000), bBefore-QueryUtxoMockBalance(t, ct, bidder), "bidder debited full nominal bid")

	r1, _, _ := CallMarket(t, ct, "getAuction", []byte(`{"auctionId":0}`), nil, "hive:anyone", "", true, gas, "")
	assert.Equal(t, "4950", ParseAuction(r1).HighBid, "stored high bid is the post-fee escrowed 4950 (raw-read balance-delta)")

	// Settle after end. feeBps:0 so seller payout = highBid 4950; that payout
	// transfer is itself taxed -> seller net-credited 4950 - 49 = 4901.
	ct.BlockHeight = 201
	sellerBefore := QueryUtxoMockBalance(t, ct, seller)
	_, _, logs := CallMarket(t, ct, "settleAuction", []byte(`{"auctionId":0}`), nil, "hive:anyone", "", true, gas, "")
	sellerAfter := QueryUtxoMockBalance(t, ct, seller)

	assert.Equal(t, uint64(4901), sellerAfter-sellerBefore, "seller paid the post-fee high-bid distribution (4901)")
	assert.NotEqual(t, uint64(5000), sellerAfter-sellerBefore, "seller did NOT get the nominal 5000")

	// finalPrice event is the post-fee high bid 4950 (balance-delta, not 5000).
	AssertEventContains(t, logs, "auction_settled", `"finalPrice":"4950"`)

	// Winner got the NFT; no residual in market.
	assert.Equal(t, uint64(1), QueryNftBalance(t, ct, bidder, "1"), "winner received NFT")
	assert.Equal(t, uint64(0), QueryNftBalance(t, ct, seller, "1"), "seller no longer holds NFT")
	assert.Equal(t, uint64(0), QueryUtxoMockBalance(t, ct, MarketContractAddress), "no residual in market")
}
