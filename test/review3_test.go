package contract_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

// ===================================
// English auction reserve = per-unit, multi-amount
// ===================================

func TestEnglishAuctionReserveIsPerUnit(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	MintNft(t, ct, ownerAddress, "1", 5, 100)
	ApproveNftForMarket(t, ct, ownerAddress)

	ct.BlockHeight = 100
	// 5 units, reserve 1000 per unit → minimum total bid = 5000
	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","auctionType":"english","startPrice":1000,"endPrice":0,"startBlock":100,"endBlock":200}`, NftContractID, TokenID)
	CallMarket(t, ct, "createAuction", []byte(payload), nil, ownerAddress, "", true, gas, "")

	bidder := "hive:bidder"
	MintAndApproveToken(t, ct, bidder, 10000)

	ct.BlockHeight = 120

	// Bid 3000 (below reserve total 5000) should fail
	CallMarket(t, ct, "placeBid", []byte(`{"auctionId":0,"bidAmount":3000}`), nil, bidder, "", false, gas, "Bid must be at least the reserve price")

	// Bid 5000 (= reserve total) should succeed
	CallMarket(t, ct, "placeBid", []byte(`{"auctionId":0,"bidAmount":5000}`), nil, bidder, "", true, gas, "")
}

func TestEnglishAuctionMultiAmountSettlement(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	MintNft(t, ct, ownerAddress, "1", 3, 100)
	ApproveNftForMarket(t, ct, ownerAddress)

	ct.BlockHeight = 100
	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":3,"paymentToken":"%s","auctionType":"english","startPrice":1000,"endPrice":0,"startBlock":100,"endBlock":200}`, NftContractID, TokenID)
	CallMarket(t, ct, "createAuction", []byte(payload), nil, ownerAddress, "", true, gas, "")

	bidder := "hive:bidder"
	MintAndApproveToken(t, ct, bidder, 10000)

	ct.BlockHeight = 150
	CallMarket(t, ct, "placeBid", []byte(`{"auctionId":0,"bidAmount":6000}`), nil, bidder, "", true, gas, "")

	ct.BlockHeight = 201
	CallMarket(t, ct, "settleAuction", []byte(`{"auctionId":0}`), nil, "hive:anyone", "", true, gas, "")

	// Winner should have 3 NFTs
	assert.Equal(t, uint64(3), QueryNftBalance(t, ct, bidder, "1"))

	// Seller: 6000 - 2.5% fee (150) = 5850
	sellerBalance := QueryTokenBalance(t, ct, ownerAddress)
	assert.Equal(t, uint64(5850), sellerBalance)
}

// ===================================
// Delist/CancelOffer work when paused (asset recovery)
// ===================================

func TestCancelAuctionWhenPaused(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	MintNft(t, ct, ownerAddress, "1", 1, 1)
	ApproveNftForMarket(t, ct, ownerAddress)

	ct.BlockHeight = 100
	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":1,"paymentToken":"%s","auctionType":"english","startPrice":1000,"endPrice":0,"startBlock":100,"endBlock":200}`, NftContractID, TokenID)
	CallMarket(t, ct, "createAuction", []byte(payload), nil, ownerAddress, "", true, gas, "")

	CallMarket(t, ct, "pause", nil, nil, ownerAddress, "", true, gas, "")

	// Cancel should work when paused (no bids)
	CallMarket(t, ct, "cancelAuction", []byte(`{"auctionId":0}`), nil, ownerAddress, "", true, gas, "")

	assert.Equal(t, uint64(1), QueryNftBalance(t, ct, ownerAddress, "1"))
}

// ===================================
// Listing: buyer != seller enforced in batch
// ===================================

func TestBatchBuySellerCannotBuyOwn(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	MintNft(t, ct, ownerAddress, "1", 5, 100)
	ApproveNftForMarket(t, ct, ownerAddress)
	MintAndApproveToken(t, ct, ownerAddress, 50000)

	listPayload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","pricePerUnit":1000}`, NftContractID, TokenID)
	CallMarket(t, ct, "list", []byte(listPayload), nil, ownerAddress, "", true, gas, "")

	batchPayload := `{"items":[{"listingId":0,"amount":1}]}`
	CallMarket(t, ct, "batchBuy", []byte(batchPayload), nil, ownerAddress, "", false, gas, "Seller cannot buy own listing")
}

// ===================================
// Expired listing: delist still works
// ===================================

func TestDelistExpiredListingWhenPaused(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	MintNft(t, ct, ownerAddress, "1", 5, 100)
	ApproveNftForMarket(t, ct, ownerAddress)

	ct.BlockHeight = 100
	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","pricePerUnit":1000,"expirationBlock":150}`, NftContractID, TokenID)
	CallMarket(t, ct, "list", []byte(payload), nil, ownerAddress, "", true, gas, "")

	ct.BlockHeight = 200
	CallMarket(t, ct, "pause", nil, nil, ownerAddress, "", true, gas, "")

	// Should still be able to delist even when paused AND expired
	CallMarket(t, ct, "delist", []byte(`{"listingId":0}`), nil, ownerAddress, "", true, gas, "")
	assert.Equal(t, uint64(5), QueryNftBalance(t, ct, ownerAddress, "1"))
}

// ===================================
// Offer: partial cancel refund correctness
// ===================================

func TestPartialAcceptThenCancelRefundCorrect(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	MintNft(t, ct, ownerAddress, "1", 10, 100)

	buyer := "hive:buyer"
	MintAndApproveToken(t, ct, buyer, 10000)

	// Offer: 10 units at 1000 each = 10000 escrowed
	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":10,"paymentToken":"%s","pricePerUnit":1000}`, NftContractID, TokenID)
	CallMarket(t, ct, "makeOffer", []byte(payload), nil, buyer, "", true, gas, "")

	// Accept 3 → 3000 distributed, 7000 remain escrowed
	ApproveNftForMarket(t, ct, ownerAddress)
	CallMarket(t, ct, "acceptOffer", []byte(`{"offerId":0,"amount":3}`), nil, ownerAddress, "", true, gas, "")

	// Cancel remaining → 7 * 1000 = 7000 refunded
	CallMarket(t, ct, "cancelOffer", []byte(`{"offerId":0}`), nil, buyer, "", true, gas, "")

	// Marketplace should have 0 token balance
	assert.Equal(t, uint64(0), QueryTokenBalance(t, ct, MarketContractAddress))

	// Buyer: started with 10000, got 7000 back
	assert.Equal(t, uint64(7000), QueryTokenBalance(t, ct, buyer))
}

// ===================================
// Dutch auction: startPrice stored is per-unit
// ===================================

func TestDutchAuctionStartPriceIsPerUnit(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	MintNft(t, ct, ownerAddress, "1", 3, 100)
	ApproveNftForMarket(t, ct, ownerAddress)

	ct.BlockHeight = 100
	// 3 units, start 10000/unit, end 1000/unit
	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":3,"paymentToken":"%s","auctionType":"dutch","startPrice":10000,"endPrice":1000,"startBlock":100,"endBlock":200}`, NftContractID, TokenID)
	CallMarket(t, ct, "createAuction", []byte(payload), nil, ownerAddress, "", true, gas, "")

	buyer := "hive:buyer"
	MintAndApproveToken(t, ct, buyer, 100000)

	// At start, price = 3 * 10000 = 30000 total
	ct.BlockHeight = 100
	CallMarket(t, ct, "placeBid", []byte(`{"auctionId":0,"bidAmount":30000}`), nil, buyer, "", true, gas, "")

	assert.Equal(t, uint64(3), QueryNftBalance(t, ct, buyer, "1"))
}

// ===================================
// English auction: getAuction shows locked royalty recipient
// ===================================

func TestAuctionLocksRoyalty(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	royaltyPayload := fmt.Sprintf(`{"nftContract":"%s","royaltyBps":500,"royaltyRecipient":"hive:creator"}`, NftContractID)
	CallMarket(t, ct, "setRoyalty", []byte(royaltyPayload), nil, ownerAddress, "", true, gas, "")

	MintNft(t, ct, ownerAddress, "1", 1, 1)
	ApproveNftForMarket(t, ct, ownerAddress)

	ct.BlockHeight = 100
	auctionPayload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":1,"paymentToken":"%s","auctionType":"english","startPrice":1000,"endPrice":0,"startBlock":100,"endBlock":200}`, NftContractID, TokenID)
	CallMarket(t, ct, "createAuction", []byte(auctionPayload), nil, ownerAddress, "", true, gas, "")

	// Change royalty after auction created
	royaltyPayload2 := fmt.Sprintf(`{"nftContract":"%s","royaltyBps":2000,"royaltyRecipient":"hive:newcreator"}`, NftContractID)
	CallMarket(t, ct, "setRoyalty", []byte(royaltyPayload2), nil, ownerAddress, "", true, gas, "")

	// Auction should still show locked 500 bps
	result, _, _ := CallMarket(t, ct, "getAuction", []byte(`{"auctionId":0}`), nil, "hive:anyone", "", true, gas, "")
	auction := ParseAuction(result)
	assert.Equal(t, uint64(500), auction.RoyaltyBps)
}
