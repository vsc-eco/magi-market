package contract_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

// ===================================
// UpdateListing expiration check
// ===================================

func TestUpdateListingExpired(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	MintNft(t, ct, ownerAddress, "1", 5, 100)
	ApproveNftForMarket(t, ct, ownerAddress)

	ct.BlockHeight = 100
	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","pricePerUnit":1000,"expirationBlock":150}`, NftContractID, TokenID)
	CallMarket(t, ct, "list", []byte(payload), nil, ownerAddress, "", true, gas, "")

	ct.BlockHeight = 200
	CallMarket(t, ct, "updateListing", []byte(`{"listingId":0,"newPrice":2000}`), nil, ownerAddress, "", false, gas, "Listing has expired")
}

func TestUpdateListingBeforeExpiration(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	MintNft(t, ct, ownerAddress, "1", 5, 100)
	ApproveNftForMarket(t, ct, ownerAddress)

	ct.BlockHeight = 100
	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","pricePerUnit":1000,"expirationBlock":200}`, NftContractID, TokenID)
	CallMarket(t, ct, "list", []byte(payload), nil, ownerAddress, "", true, gas, "")

	ct.BlockHeight = 150
	CallMarket(t, ct, "updateListing", []byte(`{"listingId":0,"newPrice":2000}`), nil, ownerAddress, "", true, gas, "")

	result, _, _ := CallMarket(t, ct, "getListing", []byte(`{"listingId":0}`), nil, "hive:anyone", "", true, gas, "")
	listing := ParseListing(result)
	assert.Equal(t, uint64(2000), listing.PricePerUnit)
}

// ===================================
// Seller cannot accept own offer
// ===================================

func TestBuyerCannotAcceptOwnOffer(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	MintNft(t, ct, ownerAddress, "1", 10, 100)

	// Owner makes offer (they're also the NFT holder)
	MintAndApproveToken(t, ct, ownerAddress, 50000)
	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","pricePerUnit":1000}`, NftContractID, TokenID)
	CallMarket(t, ct, "makeOffer", []byte(payload), nil, ownerAddress, "", true, gas, "")

	// Owner tries to accept own offer — NFT contract rejects self-transfer
	ApproveNftForMarket(t, ct, ownerAddress)
	CallMarket(t, ct, "acceptOffer", []byte(`{"offerId":0}`), nil, ownerAddress, "", false, gas, "Cannot transfer to self")
}

// ===================================
// Fee recipient = seller edge case
// ===================================

func TestFeeRecipientIsSeller(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	// Set fee recipient to the seller (ownerAddress)
	CallMarket(t, ct, "setFeeRecipient", []byte(fmt.Sprintf(`{"feeRecipient":"%s"}`, ownerAddress)), nil, ownerAddress, "", true, gas, "")

	MintNft(t, ct, ownerAddress, "1", 1, 1)
	ApproveNftForMarket(t, ct, ownerAddress)

	listPayload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":1,"paymentToken":"%s","pricePerUnit":10000}`, NftContractID, TokenID)
	CallMarket(t, ct, "list", []byte(listPayload), nil, ownerAddress, "", true, gas, "")

	buyer := "hive:buyer"
	MintAndApproveToken(t, ct, buyer, 10000)
	CallMarket(t, ct, "buy", []byte(`{"listingId":0,"amount":1}`), nil, buyer, "", true, gas, "")

	// Seller gets both sellerPayment + fee = 9750 + 250 = 10000
	sellerBalance := QueryTokenBalance(t, ct, ownerAddress)
	assert.Equal(t, uint64(10000), sellerBalance)
}

// ===================================
// Royalty recipient = seller edge case
// ===================================

func TestRoyaltyRecipientIsSeller(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	// Set royalty recipient to the seller
	royaltyPayload := fmt.Sprintf(`{"nftContract":"%s","royaltyBps":1000,"royaltyRecipient":"%s"}`, NftContractID, ownerAddress)
	CallMarket(t, ct, "setRoyalty", []byte(royaltyPayload), nil, ownerAddress, "", true, gas, "")

	MintNft(t, ct, ownerAddress, "1", 1, 1)
	ApproveNftForMarket(t, ct, ownerAddress)

	listPayload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":1,"paymentToken":"%s","pricePerUnit":10000}`, NftContractID, TokenID)
	CallMarket(t, ct, "list", []byte(listPayload), nil, ownerAddress, "", true, gas, "")

	buyer := "hive:buyer"
	MintAndApproveToken(t, ct, buyer, 10000)
	CallMarket(t, ct, "buy", []byte(`{"listingId":0,"amount":1}`), nil, buyer, "", true, gas, "")

	// Seller gets sellerPayment + royalty = 8750 + 1000 = 9750, fee goes to feeRecipient = 250
	sellerBalance := QueryTokenBalance(t, ct, ownerAddress)
	assert.Equal(t, uint64(9750), sellerBalance)
	feeBalance := QueryTokenBalance(t, ct, feeRecipientAddress)
	assert.Equal(t, uint64(250), feeBalance)
}

// ===================================
// Whitelist: remove all tokens, still enforced
// ===================================

func TestWhitelistRemoveAllTokensStillEnforced(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	// Add then remove a token — whitelist is still "on"
	CallMarket(t, ct, "addPaymentToken", []byte(fmt.Sprintf(`{"token":"%s"}`, TokenID)), nil, ownerAddress, "", true, gas, "")
	CallMarket(t, ct, "removePaymentToken", []byte(fmt.Sprintf(`{"token":"%s"}`, TokenID)), nil, ownerAddress, "", true, gas, "")

	// Whitelist enabled but no tokens allowed
	MintNft(t, ct, ownerAddress, "1", 5, 100)
	ApproveNftForMarket(t, ct, ownerAddress)

	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","pricePerUnit":1000}`, NftContractID, TokenID)
	CallMarket(t, ct, "list", []byte(payload), nil, ownerAddress, "", false, gas, "Payment token not allowed")
}

// ===================================
// Delist then offer can still be accepted (NFTs back with seller)
// ===================================

func TestDelistThenAcceptOffer(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	MintNft(t, ct, ownerAddress, "1", 5, 100)
	ApproveNftForMarket(t, ct, ownerAddress)

	// List NFTs
	listPayload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","pricePerUnit":1000}`, NftContractID, TokenID)
	CallMarket(t, ct, "list", []byte(listPayload), nil, ownerAddress, "", true, gas, "")

	// Buyer makes offer
	buyer := "hive:buyer"
	MintAndApproveToken(t, ct, buyer, 50000)
	offerPayload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","pricePerUnit":800}`, NftContractID, TokenID)
	CallMarket(t, ct, "makeOffer", []byte(offerPayload), nil, buyer, "", true, gas, "")

	// Seller delists (gets NFTs back)
	CallMarket(t, ct, "delist", []byte(`{"listingId":0}`), nil, ownerAddress, "", true, gas, "")

	// Seller can still accept the offer (has NFTs now)
	CallMarket(t, ct, "acceptOffer", []byte(`{"offerId":0}`), nil, ownerAddress, "", true, gas, "")

	// Buyer should have NFTs
	buyerNft := QueryNftBalance(t, ct, buyer, "1")
	assert.Equal(t, uint64(5), buyerNft)
}

// ===================================
// Multiple partial accepts don't exceed total
// ===================================

func TestPartialAcceptExceedsTotalFails(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	MintNft(t, ct, ownerAddress, "1", 10, 100)

	buyer := "hive:buyer"
	MintAndApproveToken(t, ct, buyer, 50000)

	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","pricePerUnit":1000}`, NftContractID, TokenID)
	CallMarket(t, ct, "makeOffer", []byte(payload), nil, buyer, "", true, gas, "")

	ApproveNftForMarket(t, ct, ownerAddress)

	// Accept 3
	CallMarket(t, ct, "acceptOffer", []byte(`{"offerId":0,"amount":3}`), nil, ownerAddress, "", true, gas, "")

	// Try to accept 3 more (only 2 remain)
	CallMarket(t, ct, "acceptOffer", []byte(`{"offerId":0,"amount":3}`), nil, ownerAddress, "", false, gas, "Accept amount exceeds offer amount")

	// Accept remaining 2
	CallMarket(t, ct, "acceptOffer", []byte(`{"offerId":0,"amount":2}`), nil, ownerAddress, "", true, gas, "")

	// Now inactive
	result, _, _ := CallMarket(t, ct, "getOffer", []byte(`{"offerId":0}`), nil, "hive:anyone", "", true, gas, "")
	assert.False(t, ParseOffer(result).Active)
}

// ===================================
// Marketplace balance accounting after buy
// ===================================

func TestMarketplaceHasZeroBalanceAfterBuy(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	MintNft(t, ct, ownerAddress, "1", 1, 1)
	ApproveNftForMarket(t, ct, ownerAddress)

	listPayload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":1,"paymentToken":"%s","pricePerUnit":10000}`, NftContractID, TokenID)
	CallMarket(t, ct, "list", []byte(listPayload), nil, ownerAddress, "", true, gas, "")

	buyer := "hive:buyer"
	MintAndApproveToken(t, ct, buyer, 10000)
	CallMarket(t, ct, "buy", []byte(`{"listingId":0,"amount":1}`), nil, buyer, "", true, gas, "")

	// Marketplace should have 0 tokens and 0 NFTs after complete sale
	marketTokenBalance := QueryTokenBalance(t, ct, MarketContractAddress)
	assert.Equal(t, uint64(0), marketTokenBalance)

	marketNftBalance := QueryNftBalance(t, ct, MarketContractAddress, "1")
	assert.Equal(t, uint64(0), marketNftBalance)
}

func TestMarketplaceHasZeroBalanceAfterAcceptOffer(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	MintNft(t, ct, ownerAddress, "1", 5, 100)

	buyer := "hive:buyer"
	MintAndApproveToken(t, ct, buyer, 5000)

	offerPayload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","pricePerUnit":1000}`, NftContractID, TokenID)
	CallMarket(t, ct, "makeOffer", []byte(offerPayload), nil, buyer, "", true, gas, "")

	ApproveNftForMarket(t, ct, ownerAddress)
	CallMarket(t, ct, "acceptOffer", []byte(`{"offerId":0}`), nil, ownerAddress, "", true, gas, "")

	// Marketplace should have 0 tokens after full acceptance
	marketTokenBalance := QueryTokenBalance(t, ct, MarketContractAddress)
	assert.Equal(t, uint64(0), marketTokenBalance)
}

// ===================================
// Listing NFT escrow correctness
// ===================================

func TestListDelistNftBalances(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	MintNft(t, ct, ownerAddress, "1", 5, 100)
	ApproveNftForMarket(t, ct, ownerAddress)

	// Before listing
	assert.Equal(t, uint64(5), QueryNftBalance(t, ct, ownerAddress, "1"))
	assert.Equal(t, uint64(0), QueryNftBalance(t, ct, MarketContractAddress, "1"))

	listPayload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","pricePerUnit":1000}`, NftContractID, TokenID)
	CallMarket(t, ct, "list", []byte(listPayload), nil, ownerAddress, "", true, gas, "")

	// After listing
	assert.Equal(t, uint64(0), QueryNftBalance(t, ct, ownerAddress, "1"))
	assert.Equal(t, uint64(5), QueryNftBalance(t, ct, MarketContractAddress, "1"))

	// After delist
	CallMarket(t, ct, "delist", []byte(`{"listingId":0}`), nil, ownerAddress, "", true, gas, "")
	assert.Equal(t, uint64(5), QueryNftBalance(t, ct, ownerAddress, "1"))
	assert.Equal(t, uint64(0), QueryNftBalance(t, ct, MarketContractAddress, "1"))
}

// ===================================
// Auction: English auction bid event contents
// ===================================

func TestEnglishAuctionBidEventContents(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	MintNft(t, ct, ownerAddress, "1", 1, 1)
	ApproveNftForMarket(t, ct, ownerAddress)

	ct.BlockHeight = 100
	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":1,"paymentToken":"%s","auctionType":"english","startPrice":1000,"endPrice":0,"startBlock":100,"endBlock":200}`, NftContractID, TokenID)
	CallMarket(t, ct, "createAuction", []byte(payload), nil, ownerAddress, "", true, gas, "")

	bidder := "hive:bidder"
	MintAndApproveToken(t, ct, bidder, 5000)

	ct.BlockHeight = 120
	_, _, logs := CallMarket(t, ct, "placeBid", []byte(`{"auctionId":0,"bidAmount":2000}`), nil, bidder, "", true, gas, "")
	AssertEventContains(t, logs, "bid_placed", `"bidder":"hive:bidder"`)
	AssertEventContains(t, logs, "bid_placed", `"bidAmount":2000`)
}

// ===================================
// Auction: settlement event contents
// ===================================

func TestEnglishAuctionSettledEventContents(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	MintNft(t, ct, ownerAddress, "1", 1, 1)
	ApproveNftForMarket(t, ct, ownerAddress)

	ct.BlockHeight = 100
	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":1,"paymentToken":"%s","auctionType":"english","startPrice":1000,"endPrice":0,"startBlock":100,"endBlock":200}`, NftContractID, TokenID)
	CallMarket(t, ct, "createAuction", []byte(payload), nil, ownerAddress, "", true, gas, "")

	bidder := "hive:bidder"
	MintAndApproveToken(t, ct, bidder, 5000)
	ct.BlockHeight = 150
	CallMarket(t, ct, "placeBid", []byte(`{"auctionId":0,"bidAmount":3000}`), nil, bidder, "", true, gas, "")

	ct.BlockHeight = 201
	_, _, logs := CallMarket(t, ct, "settleAuction", []byte(`{"auctionId":0}`), nil, "hive:anyone", "", true, gas, "")
	AssertEventContains(t, logs, "auction_settled", `"winner":"hive:bidder"`)
	AssertEventContains(t, logs, "auction_settled", `"finalPrice":3000`)
}

// ===================================
// Batch list event emission
// ===================================

func TestBatchListEmitsIndividualEvents(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	MintNft(t, ct, ownerAddress, "1", 5, 100)
	MintNft(t, ct, ownerAddress, "2", 3, 100)
	ApproveNftForMarket(t, ct, ownerAddress)

	items := fmt.Sprintf(`{"items":[{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","pricePerUnit":1000},{"nftContract":"%s","tokenId":"2","amount":3,"paymentToken":"%s","pricePerUnit":2000}]}`, NftContractID, TokenID, NftContractID, TokenID)
	_, _, logs := CallMarket(t, ct, "batchList", []byte(items), nil, ownerAddress, "", true, gas, "")

	events := FindEventsInLogs(logs, "listed")
	assert.Equal(t, 2, len(events))
}

// ===================================
// Fee + Royalty combined = 100% edge case
// ===================================

func TestFeeAndRoyaltyCombined100Percent(t *testing.T) {
	ct := SetupContractTest()

	// Initialize with 50% fee
	CleanBadgerDB()
	ct2 := SetupContractTest()
	InitToken(t, ct2)
	InitNft(t, ct2)
	feePayload := fmt.Sprintf(`{"feeBps":5000,"feeRecipient":"%s"}`, feeRecipientAddress)
	CallMarket(t, ct2, "init", []byte(feePayload), nil, ownerAddress, "", true, gas, "")

	// Set 50% royalty (combined = 100%)
	royaltyPayload := fmt.Sprintf(`{"nftContract":"%s","royaltyBps":5000,"royaltyRecipient":"hive:creator"}`, NftContractID)
	CallMarket(t, ct2, "setRoyalty", []byte(royaltyPayload), nil, ownerAddress, "", true, gas, "")

	MintNft(t, ct2, ownerAddress, "1", 1, 1)
	ApproveNftForMarket(t, ct2, ownerAddress)

	listPayload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":1,"paymentToken":"%s","pricePerUnit":10000}`, NftContractID, TokenID)
	CallMarket(t, ct2, "list", []byte(listPayload), nil, ownerAddress, "", true, gas, "")

	buyer := "hive:buyer"
	MintAndApproveToken(t, ct2, buyer, 10000)
	CallMarket(t, ct2, "buy", []byte(`{"listingId":0,"amount":1}`), nil, buyer, "", true, gas, "")

	// fee=5000, royalty=5000, seller=0
	sellerBalance := QueryTokenBalance(t, ct2, ownerAddress)
	assert.Equal(t, uint64(0), sellerBalance)
	feeBalance := QueryTokenBalance(t, ct2, feeRecipientAddress)
	assert.Equal(t, uint64(5000), feeBalance)
	creatorBalance := QueryTokenBalance(t, ct2, "hive:creator")
	assert.Equal(t, uint64(5000), creatorBalance)

	_ = ct // suppress unused
}

// ===================================
// Fee + Royalty > 100% rejected at listing
// ===================================

func TestFeeAndRoyaltyExceed100PercentRejected(t *testing.T) {
	ct := SetupContractTest()

	CleanBadgerDB()
	ct2 := SetupContractTest()
	InitToken(t, ct2)
	InitNft(t, ct2)
	feePayload := fmt.Sprintf(`{"feeBps":6000,"feeRecipient":"%s"}`, feeRecipientAddress)
	CallMarket(t, ct2, "init", []byte(feePayload), nil, ownerAddress, "", true, gas, "")

	// Set 50% royalty → combined = 110% > 100%
	royaltyPayload := fmt.Sprintf(`{"nftContract":"%s","royaltyBps":5000,"royaltyRecipient":"hive:creator"}`, NftContractID)
	CallMarket(t, ct2, "setRoyalty", []byte(royaltyPayload), nil, ownerAddress, "", true, gas, "")

	MintNft(t, ct2, ownerAddress, "1", 1, 1)
	ApproveNftForMarket(t, ct2, ownerAddress)

	listPayload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":1,"paymentToken":"%s","pricePerUnit":10000}`, NftContractID, TokenID)
	CallMarket(t, ct2, "list", []byte(listPayload), nil, ownerAddress, "", false, gas, "Combined fee and royalty exceed 100%")

	_ = ct // suppress unused
}
