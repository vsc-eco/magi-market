package contract_test

import (
	"fmt"
	"testing"

	"vsc-node/lib/test_utils"

	"github.com/stretchr/testify/assert"
)

// ============================================================================
// Fee-on-transfer payment-token tests.
//
// The fee token (test/mocks/feetoken) burns 1% (floor) on EVERY transfer /
// transferFrom hop: the sender is debited the full amount, the recipient is
// credited amount - floor(amount/100). It is registered under the contract id
// "contract:feetoken" (= FeeTokenID) because the marketplace passes the
// configured paymentToken string verbatim as the cross-contract call target
// (go-vsc-node does not strip a "contract:" prefix).
//
// These tests prove the marketplace distributes the BALANCE-DELTA it actually
// received (escrowIn) rather than the nominal amount, and that underfunded
// offers / Dutch underpays abort cleanly with no value created. The market is
// initialised with feeBps:0 so the ONLY value loss is the token's 1% transfer
// fee — making the deltas unambiguous.
//
// Honest-accounting note: because EVERY hop is taxed, a payout the marketplace
// sends out (seller/refund) is itself taxed again. So e.g. a buy at 1000 nets
// the seller 1000 -> escrow 990 (received, what the market distributes) ->
// payout 990 -> seller credited 981. The "bought" event's totalPrice is the
// received 990 (proving balance-delta distribution, NOT the nominal 1000); the
// seller's on-ledger gain is 981 (post-payout-fee). This is correct
// fee-on-transfer behaviour, not a contract bug.
// ============================================================================

// initFeeTokenSetup initialises NFT + a feeBps:0 marketplace, registers the
// fee token as an allowed payment token, and inits the fee token.
func initFeeTokenSetup(t *testing.T, ct *test_utils.ContractTest) {
	InitNft(t, ct)
	// feeBps:0 isolates the token's 1% transfer fee as the only value loss.
	CallMarket(t, ct, "init",
		[]byte(fmt.Sprintf(`{"feeBps":0,"feeRecipient":"%s"}`, feeRecipientAddress)),
		nil, ownerAddress, "", true, gas, "")
	CallMarket(t, ct, "addPaymentToken",
		[]byte(fmt.Sprintf(`{"token":"%s"}`, FeeTokenID)),
		nil, ownerAddress, "", true, gas, "")
	// init the fee token (owner only, no initial supply minted).
	CallFeeToken(t, ct, "init",
		[]byte(fmt.Sprintf(`{"owner":"%s"}`, ownerAddress)),
		ownerAddress, true, "")
}

// CallFeeToken calls the fee-on-transfer mock token contract.
func CallFeeToken(t *testing.T, ct *test_utils.ContractTest, action string, payload []byte, authUser string, expectOk bool, expectedOutput string) test_utils.ContractTestCallResult {
	cr, _, _ := callContract(t, ct, FeeTokenID, action, payload, nil, authUser, defaultTimestamp, expectOk, gas, expectedOutput)
	return cr
}

// MintFeeToken credits `amount` fee tokens to `to` (mint is fee-free).
func MintFeeToken(t *testing.T, ct *test_utils.ContractTest, to string, amount uint64) {
	CallFeeToken(t, ct, "mint",
		[]byte(fmt.Sprintf(`{"to":"%s","amount":"%d"}`, to, amount)),
		ownerAddress, true, "")
}

// QueryFeeTokenBalance reads balanceOf on the fee token (returns {"balance":"<dec>"}).
func QueryFeeTokenBalance(t *testing.T, ct *test_utils.ContractTest, account string) uint64 {
	cr := CallFeeToken(t, ct, "balanceOf",
		[]byte(fmt.Sprintf(`{"account":"%s"}`, account)),
		"hive:anyone", true, "")
	return ParseBalance(cr)
}

// ---------------------------------------------------------------------------
// (a) BUY: balance-delta distribution
// ---------------------------------------------------------------------------

func TestFeeTokenBuyDistributesReceived(t *testing.T) {
	ct := SetupContractTest()
	initFeeTokenSetup(t, ct)

	seller := ownerAddress
	buyer := "hive:buyer"

	MintNft(t, ct, seller, "1", 1, 1)
	ApproveNftForMarket(t, ct, seller)
	MintFeeToken(t, ct, buyer, 100000)

	// List 1 NFT at 1000 fee-token, paid in the fee token.
	listPayload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":1,"paymentToken":"%s","pricePerUnit":"1000"}`, NftContractID, FeeTokenID)
	CallMarket(t, ct, "list", []byte(listPayload), nil, seller, "", true, gas, "")

	sellerBefore := QueryFeeTokenBalance(t, ct, seller)
	buyerBefore := QueryFeeTokenBalance(t, ct, buyer)

	_, _, logs := CallMarket(t, ct, "buy", []byte(`{"listingId":0,"amount":1}`), nil, buyer, "", true, gas, "")

	sellerAfter := QueryFeeTokenBalance(t, ct, seller)
	buyerAfter := QueryFeeTokenBalance(t, ct, buyer)

	// Buyer paid the full nominal 1000 (debited in transferFrom).
	assert.Equal(t, uint64(1000), buyerBefore-buyerAfter, "buyer debited the full nominal price")

	// escrowIn received 1000 - floor(1000/100) = 990. The marketplace
	// distributes that received 990 to the seller (feeBps:0). The seller
	// payout transfer is itself taxed: 990 - floor(990/100) = 981.
	sellerGain := sellerAfter - sellerBefore
	assert.Equal(t, uint64(981), sellerGain, "seller credited post-payout-fee amount")
	assert.NotEqual(t, uint64(1000), sellerGain, "seller did NOT get the nominal 1000")
	assert.NotEqual(t, uint64(990), sellerGain, "payout hop is also taxed (981, not 990)")

	// The event records totalPrice = received (990), proving the marketplace
	// distributes the balance-delta, NOT the nominal 1000.
	AssertEventContains(t, logs, "bought", `"totalPrice":"990"`)

	// No funds stranded in the marketplace.
	assert.Equal(t, uint64(0), QueryFeeTokenBalance(t, ct, MarketContractAddress), "no residual in market")

	// Buyer received the NFT.
	assert.Equal(t, uint64(1), QueryNftBalance(t, ct, buyer, "1"))
}

// ---------------------------------------------------------------------------
// (b) OFFER: underfunded escrow -> accept aborts, cancel refunds residual esc
// ---------------------------------------------------------------------------

func TestFeeTokenOfferUnderfundedEscrow(t *testing.T) {
	ct := SetupContractTest()
	initFeeTokenSetup(t, ct)

	seller := ownerAddress
	buyer := "hive:buyer"

	MintNft(t, ct, seller, "1", 1, 1)
	MintFeeToken(t, ct, buyer, 100000)

	buyerStart := QueryFeeTokenBalance(t, ct, buyer)

	// Offer 1 unit at 1000 -> totalOffer 1000. escrowIn pulls 1000 from buyer
	// but the market only receives 990 (1% fee). Stored esc = 990.
	offerPayload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":1,"paymentToken":"%s","pricePerUnit":"1000"}`, NftContractID, FeeTokenID)
	CallMarket(t, ct, "makeOffer", []byte(offerPayload), nil, buyer, "", true, gas, "")

	// Buyer debited the full nominal 1000; market holds 990.
	assert.Equal(t, uint64(1000), buyerStart-QueryFeeTokenBalance(t, ct, buyer), "buyer debited full nominal offer")
	assert.Equal(t, uint64(990), QueryFeeTokenBalance(t, ct, MarketContractAddress), "market escrow holds received 990")

	// Seller accepts: totalPrice = 1000*1 = 1000 > esc 990 -> ABORT.
	// This documents the underfunded-offer behaviour: the contract never
	// distributes more than it actually received.
	ApproveNftForMarket(t, ct, seller)
	CallMarket(t, ct, "acceptOffer", []byte(`{"offerId":0}`), nil, seller, "", false, gas, "Accept exceeds escrowed funds")

	// NFT never moved (still with seller, market never escrowed it for offers).
	assert.Equal(t, uint64(1), QueryNftBalance(t, ct, seller, "1"))
	assert.Equal(t, uint64(0), QueryNftBalance(t, ct, buyer, "1"))

	// Buyer cancels: contract refunds the residual esc (990) via a transfer
	// that is ITSELF taxed -> buyer credited 990 - floor(990/100) = 981.
	balBeforeCancel := QueryFeeTokenBalance(t, ct, buyer)
	_, _, logs := CallMarket(t, ct, "cancelOffer", []byte(`{"offerId":0}`), nil, buyer, "", true, gas, "")
	balAfterCancel := QueryFeeTokenBalance(t, ct, buyer)

	assert.Equal(t, uint64(981), balAfterCancel-balBeforeCancel, "refund of residual esc 990, post-fee 981")
	// Market fully drained of this offer's escrow (sent the full residual 990).
	assert.Equal(t, uint64(0), QueryFeeTokenBalance(t, ct, MarketContractAddress), "market drained the full residual esc")
	AssertEventEmitted(t, logs, "offer_cancelled")

	// Offer is now inactive.
	result, _, _ := CallMarket(t, ct, "getOffer", []byte(`{"offerId":0}`), nil, "hive:anyone", "", true, gas, "")
	assert.False(t, ParseOffer(result).Active, "offer inactive after cancel")
}

// ---------------------------------------------------------------------------
// (c) ENGLISH auction: bids escrow post-fee, outbid refunds the escrowed
// amount, settle distributes the post-fee high bid.
// ---------------------------------------------------------------------------

func TestFeeTokenEnglishAuction(t *testing.T) {
	ct := SetupContractTest()
	initFeeTokenSetup(t, ct)

	seller := ownerAddress
	bidder1 := "hive:bidder1"
	bidder2 := "hive:bidder2"

	MintNft(t, ct, seller, "1", 1, 1)
	ApproveNftForMarket(t, ct, seller)
	MintFeeToken(t, ct, bidder1, 100000)
	MintFeeToken(t, ct, bidder2, 100000)

	ct.BlockHeight = 100
	auctionPayload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":1,"paymentToken":"%s","auctionType":"english","startPrice":"1000","endPrice":"0","startBlock":100,"endBlock":200}`, NftContractID, FeeTokenID)
	CallMarket(t, ct, "createAuction", []byte(auctionPayload), nil, seller, "", true, gas, "")

	ct.BlockHeight = 120

	// bidder1 bids 5000 -> escrow receives 5000 - 50 = 4950 stored as high bid.
	b1Before := QueryFeeTokenBalance(t, ct, bidder1)
	CallMarket(t, ct, "placeBid", []byte(`{"auctionId":0,"bidAmount":"5000"}`), nil, bidder1, "", true, gas, "")
	assert.Equal(t, uint64(5000), b1Before-QueryFeeTokenBalance(t, ct, bidder1), "bidder1 debited full nominal bid")

	r1, _, _ := CallMarket(t, ct, "getAuction", []byte(`{"auctionId":0}`), nil, "hive:anyone", "", true, gas, "")
	assert.Equal(t, "4950", ParseAuction(r1).HighBid, "stored high bid is the post-fee escrowed 4950")

	// bidder2 outbids 6000 -> escrow receives 5940. bidder1 must be refunded
	// EXACTLY their previously-escrowed 4950 (not nominal 5000); that refund
	// transfer is itself taxed -> bidder1 net-credited 4950 - 49 = 4901.
	b1PreRefund := QueryFeeTokenBalance(t, ct, bidder1)
	CallMarket(t, ct, "placeBid", []byte(`{"auctionId":0,"bidAmount":"6000"}`), nil, bidder2, "", true, gas, "")
	b1PostRefund := QueryFeeTokenBalance(t, ct, bidder1)
	assert.Equal(t, uint64(4901), b1PostRefund-b1PreRefund, "bidder1 refunded the escrowed 4950 (post-fee 4901)")

	r2, _, _ := CallMarket(t, ct, "getAuction", []byte(`{"auctionId":0}`), nil, "hive:anyone", "", true, gas, "")
	a2 := ParseAuction(r2)
	assert.Equal(t, "5940", a2.HighBid, "stored high bid is the post-fee escrowed 5940")
	assert.Equal(t, bidder2, a2.HighBidder)

	// Settle after end. feeBps:0 so seller payout = highBid 5940; that payout
	// transfer is itself taxed -> seller net-credited 5940 - 59 = 5881.
	ct.BlockHeight = 201
	sellerBefore := QueryFeeTokenBalance(t, ct, seller)
	_, _, logs := CallMarket(t, ct, "settleAuction", []byte(`{"auctionId":0}`), nil, "hive:anyone", "", true, gas, "")
	sellerAfter := QueryFeeTokenBalance(t, ct, seller)

	assert.Equal(t, uint64(5881), sellerAfter-sellerBefore, "seller paid the post-fee high bid distribution")
	// finalPrice event is the post-fee high bid 5940 (balance-delta, not 6000).
	AssertEventContains(t, logs, "auction_settled", `"finalPrice":"5940"`)
	// Winner got the NFT.
	assert.Equal(t, uint64(1), QueryNftBalance(t, ct, bidder2, "1"))
	assert.Equal(t, uint64(0), QueryFeeTokenBalance(t, ct, MarketContractAddress), "no residual in market")
}

// ---------------------------------------------------------------------------
// (d) DUTCH immediate-buy underpay: escrowIn received < totalPrice -> abort
// cleanly with full rollback (NFT never moved, no payout).
// ---------------------------------------------------------------------------

func TestFeeTokenDutchUnderpayRollback(t *testing.T) {
	ct := SetupContractTest()
	initFeeTokenSetup(t, ct)

	seller := ownerAddress
	buyer := "hive:buyer"

	MintNft(t, ct, seller, "1", 1, 1)
	ApproveNftForMarket(t, ct, seller)
	MintFeeToken(t, ct, buyer, 100000)

	ct.BlockHeight = 100
	// Dutch auction: price decays 10000 -> 1000 over blocks 100..200.
	auctionPayload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":1,"paymentToken":"%s","auctionType":"dutch","startPrice":"10000","endPrice":"1000","startBlock":100,"endBlock":200}`, NftContractID, FeeTokenID)
	CallMarket(t, ct, "createAuction", []byte(auctionPayload), nil, seller, "", true, gas, "")

	// At startBlock the current price P = 10000 (full start price). Bid P.
	// escrowIn pulls P=10000, but receives 10000 - 100 = 9900 < totalPrice
	// 10000 -> the Dutch received<totalPrice guard aborts.
	ct.BlockHeight = 100
	buyerBefore := QueryFeeTokenBalance(t, ct, buyer)
	CallMarket(t, ct, "placeBid", []byte(`{"auctionId":0,"bidAmount":"10000"}`), nil, buyer, "", false, gas, "Bid must be at least the current total price")

	// Full rollback: state changes from the aborted call are discarded.
	// NFT never moved (still escrowed in the market from createAuction).
	assert.Equal(t, uint64(0), QueryNftBalance(t, ct, buyer, "1"), "buyer did not receive the NFT")
	assert.Equal(t, uint64(1), QueryNftBalance(t, ct, MarketContractAddress, "1"), "NFT still escrowed in market")
	assert.Equal(t, uint64(0), QueryNftBalance(t, ct, seller, "1"), "seller did not get NFT back")

	// No payment moved: buyer balance unchanged, no payout to seller, market
	// holds no fee-token escrow.
	assert.Equal(t, buyerBefore, QueryFeeTokenBalance(t, ct, buyer), "buyer fee-token balance unchanged (rolled back)")
	assert.Equal(t, uint64(0), QueryFeeTokenBalance(t, ct, seller), "seller received no payout")
	assert.Equal(t, uint64(0), QueryFeeTokenBalance(t, ct, MarketContractAddress), "no escrow in market")

	// Auction still active & unsettled.
	result, _, _ := CallMarket(t, ct, "getAuction", []byte(`{"auctionId":0}`), nil, "hive:anyone", "", true, gas, "")
	a := ParseAuction(result)
	assert.True(t, a.Active, "auction still active after rolled-back underpay")
	assert.False(t, a.Settled, "auction not settled")
}
