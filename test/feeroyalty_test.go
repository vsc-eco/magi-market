package contract_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

// mMulBpsDiv mirrors the contract's big.Int formula: floor(total * bps / 10000).
// Used in test assertions to compute expected amounts without importing the contract package.
func testMulBpsDiv(total, bps uint64) uint64 {
	return total * bps / 10000
}

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

// ===================================
// B2: Multi-Recipient Royalty Distribution Tests
// ===================================

// TestBuyWithRoyaltySplits: 2-recipient split (300+200 bps), 250 bps market fee.
// List 1 @ 10000 in TokenID; assert exact per-party amounts.
func TestBuyWithRoyaltySplits(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	const total = uint64(10000)
	const feeBps = uint64(250)
	const raBps = uint64(300)
	const rbBps = uint64(200)

	// Set 2 royalty splits: 300 bps to hive:ra, 200 bps to hive:rb
	splitsPayload := fmt.Sprintf(`{"nftContract":"%s","splits":[{"recipient":"hive:ra","bps":%d},{"recipient":"hive:rb","bps":%d}]}`, NftContractID, raBps, rbBps)
	CallMarket(t, ct, "setRoyaltySplits", []byte(splitsPayload), nil, ownerAddress, "", true, gas, "")

	// Mint and list
	MintNft(t, ct, ownerAddress, "1", 1, 1)
	ApproveNftForMarket(t, ct, ownerAddress)
	listPayload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":1,"paymentToken":"%s","pricePerUnit":"%d"}`, NftContractID, TokenID, total)
	CallMarket(t, ct, "list", []byte(listPayload), nil, ownerAddress, "", true, gas, "")

	// Buyer mints and buys
	buyer := "hive:buyer2"
	MintAndApproveToken(t, ct, buyer, total)
	_, _, logs := CallMarket(t, ct, "buy", []byte(`{"listingId":0,"amount":1}`), nil, buyer, "", true, gas, "")

	// Compute expected amounts
	expectedFee := testMulBpsDiv(total, feeBps)         // 250
	expectedRa := testMulBpsDiv(total, raBps)            // 300
	expectedRb := testMulBpsDiv(total, rbBps)            // 200
	expectedSeller := total - expectedFee - expectedRa - expectedRb // 9250

	assert.Equal(t, expectedFee, QueryTokenBalance(t, ct, feeRecipientAddress), "fee recipient")
	assert.Equal(t, expectedRa, QueryTokenBalance(t, ct, "hive:ra"), "royalty recipient A")
	assert.Equal(t, expectedRb, QueryTokenBalance(t, ct, "hive:rb"), "royalty recipient B")
	assert.Equal(t, expectedSeller, QueryTokenBalance(t, ct, ownerAddress), "seller")

	// Market residual should be 0
	assert.Equal(t, uint64(0), QueryTokenBalance(t, ct, MarketContractAddress), "market residual")

	// bought event totalPrice == received (10000)
	AssertEventContains(t, logs, "bought", fmt.Sprintf(`"totalPrice":"%d"`, total))
}

// TestOfferAcceptRoyaltySplits: same split config via makeOffer->acceptOffer.
func TestOfferAcceptRoyaltySplits(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	const total = uint64(10000)
	const feeBps = uint64(250)
	const raBps = uint64(300)
	const rbBps = uint64(200)

	splitsPayload := fmt.Sprintf(`{"nftContract":"%s","splits":[{"recipient":"hive:ra","bps":%d},{"recipient":"hive:rb","bps":%d}]}`, NftContractID, raBps, rbBps)
	CallMarket(t, ct, "setRoyaltySplits", []byte(splitsPayload), nil, ownerAddress, "", true, gas, "")

	// Mint NFT
	MintNft(t, ct, ownerAddress, "1", 1, 100)

	// Buyer makes offer
	buyer := "hive:offerbuyer"
	MintAndApproveToken(t, ct, buyer, total)
	offerPayload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":1,"paymentToken":"%s","pricePerUnit":"%d"}`, NftContractID, TokenID, total)
	CallMarket(t, ct, "makeOffer", []byte(offerPayload), nil, buyer, "", true, gas, "")

	// Seller approves and accepts offer
	ApproveNftForMarket(t, ct, ownerAddress)
	_, _, logs := CallMarket(t, ct, "acceptOffer", []byte(`{"offerId":0}`), nil, ownerAddress, "", true, gas, "")

	expectedFee := testMulBpsDiv(total, feeBps)
	expectedRa := testMulBpsDiv(total, raBps)
	expectedRb := testMulBpsDiv(total, rbBps)
	expectedSeller := total - expectedFee - expectedRa - expectedRb

	assert.Equal(t, expectedFee, QueryTokenBalance(t, ct, feeRecipientAddress), "fee recipient")
	assert.Equal(t, expectedRa, QueryTokenBalance(t, ct, "hive:ra"), "royalty recipient A")
	assert.Equal(t, expectedRb, QueryTokenBalance(t, ct, "hive:rb"), "royalty recipient B")
	assert.Equal(t, expectedSeller, QueryTokenBalance(t, ct, ownerAddress), "seller")
	assert.Equal(t, uint64(0), QueryTokenBalance(t, ct, MarketContractAddress), "market residual")

	AssertEventEmitted(t, logs, "offer_accepted")
	AssertEventContains(t, logs, "offer_accepted", fmt.Sprintf(`"royalty":"%d"`, expectedRa+expectedRb))
}

// TestAuctionSettleRoyaltySplits: same split config via English createAuction->placeBid->advance->settleAuction.
func TestAuctionSettleRoyaltySplits(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	const total = uint64(10000)
	const feeBps = uint64(250)
	const raBps = uint64(300)
	const rbBps = uint64(200)

	splitsPayload := fmt.Sprintf(`{"nftContract":"%s","splits":[{"recipient":"hive:ra","bps":%d},{"recipient":"hive:rb","bps":%d}]}`, NftContractID, raBps, rbBps)
	CallMarket(t, ct, "setRoyaltySplits", []byte(splitsPayload), nil, ownerAddress, "", true, gas, "")

	MintNft(t, ct, ownerAddress, "1", 1, 1)
	ApproveNftForMarket(t, ct, ownerAddress)

	ct.BlockHeight = 100
	auctionPayload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":1,"paymentToken":"%s","auctionType":"english","startPrice":"1000","endPrice":"0","startBlock":100,"endBlock":200}`, NftContractID, TokenID)
	CallMarket(t, ct, "createAuction", []byte(auctionPayload), nil, ownerAddress, "", true, gas, "")

	bidder := "hive:auctionbidder"
	MintAndApproveToken(t, ct, bidder, total)

	ct.BlockHeight = 150
	CallMarket(t, ct, "placeBid", []byte(fmt.Sprintf(`{"auctionId":0,"bidAmount":"%d"}`, total)), nil, bidder, "", true, gas, "")

	// Advance past end block and settle
	ct.BlockHeight = 201
	_, _, logs := CallMarket(t, ct, "settleAuction", []byte(`{"auctionId":0}`), nil, "hive:anyone", "", true, gas, "")

	expectedFee := testMulBpsDiv(total, feeBps)
	expectedRa := testMulBpsDiv(total, raBps)
	expectedRb := testMulBpsDiv(total, rbBps)
	expectedSeller := total - expectedFee - expectedRa - expectedRb

	assert.Equal(t, expectedFee, QueryTokenBalance(t, ct, feeRecipientAddress), "fee recipient")
	assert.Equal(t, expectedRa, QueryTokenBalance(t, ct, "hive:ra"), "royalty recipient A")
	assert.Equal(t, expectedRb, QueryTokenBalance(t, ct, "hive:rb"), "royalty recipient B")
	assert.Equal(t, expectedSeller, QueryTokenBalance(t, ct, ownerAddress), "seller")
	assert.Equal(t, uint64(0), QueryTokenBalance(t, ct, MarketContractAddress), "market residual")

	AssertEventEmitted(t, logs, "auction_settled")
	AssertEventContains(t, logs, "auction_settled", fmt.Sprintf(`"royalty":"%d"`, expectedRa+expectedRb))
}
