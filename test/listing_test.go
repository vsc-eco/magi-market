package contract_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

// ===================================
// Basic Listing Tests
// ===================================

func TestListNft(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	seller := ownerAddress
	// Mint NFT to seller
	MintNft(t, ct, seller, "1", 10, 100)

	// Approve marketplace
	ApproveNftForMarket(t, ct, seller)

	// List 5 NFTs at price 1000 each
	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","pricePerUnit":1000}`, NftContractID, TokenID)
	_, _, logs := CallMarket(t, ct, "list", []byte(payload), nil, seller, "", true, gas, "")
	AssertEventEmitted(t, logs, "listed")
	AssertEventContains(t, logs, "listed", `"listingId":0`)

	// Verify listing
	result, _, _ := CallMarket(t, ct, "getListing", []byte(`{"listingId":0}`), nil, "hive:anyone", "", true, gas, "")
	listing := ParseListing(result)
	assert.Equal(t, uint64(0), listing.ListingId)
	assert.Equal(t, seller, listing.Seller)
	assert.Equal(t, NftContractID, listing.NftContract)
	assert.Equal(t, "1", listing.TokenId)
	assert.Equal(t, uint64(5), listing.Amount)
	assert.Equal(t, uint64(1000), listing.PricePerUnit)
	assert.Equal(t, TokenID, listing.PaymentToken)
	assert.True(t, listing.Active)

	// Verify NFT was escrowed (marketplace has 5, seller has 5)
	assert.Equal(t, uint64(5), QueryNftBalance(t, ct, MarketContractAddress, "1"))
	assert.Equal(t, uint64(5), QueryNftBalance(t, ct, seller, "1"))
}

func TestListMultipleNfts(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	seller := ownerAddress
	MintNft(t, ct, seller, "1", 10, 100)
	MintNft(t, ct, seller, "2", 5, 50)
	ApproveNftForMarket(t, ct, seller)

	// List token 1
	payload1 := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":3,"paymentToken":"%s","pricePerUnit":500}`, NftContractID, TokenID)
	CallMarket(t, ct, "list", []byte(payload1), nil, seller, "", true, gas, "")

	// List token 2
	payload2 := fmt.Sprintf(`{"nftContract":"%s","tokenId":"2","amount":2,"paymentToken":"%s","pricePerUnit":2000}`, NftContractID, TokenID)
	CallMarket(t, ct, "list", []byte(payload2), nil, seller, "", true, gas, "")

	// Verify both listings exist
	result1, _, _ := CallMarket(t, ct, "getListing", []byte(`{"listingId":0}`), nil, "hive:anyone", "", true, gas, "")
	l1 := ParseListing(result1)
	assert.Equal(t, "1", l1.TokenId)
	assert.Equal(t, uint64(3), l1.Amount)

	result2, _, _ := CallMarket(t, ct, "getListing", []byte(`{"listingId":1}`), nil, "hive:anyone", "", true, gas, "")
	l2 := ParseListing(result2)
	assert.Equal(t, "2", l2.TokenId)
	assert.Equal(t, uint64(2), l2.Amount)
}

func TestListZeroAmount(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":0,"paymentToken":"%s","pricePerUnit":1000}`, NftContractID, TokenID)
	CallMarket(t, ct, "list", []byte(payload), nil, ownerAddress, "", false, gas, "Amount must be greater than zero")
}

func TestListZeroPrice(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	MintNft(t, ct, ownerAddress, "1", 10, 100)
	ApproveNftForMarket(t, ct, ownerAddress)

	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","pricePerUnit":0}`, NftContractID, TokenID)
	CallMarket(t, ct, "list", []byte(payload), nil, ownerAddress, "", false, gas, "Price must be greater than zero")
}

func TestListMissingFields(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	// Missing nftContract
	CallMarket(t, ct, "list", []byte(fmt.Sprintf(`{"nftContract":"","tokenId":"1","amount":5,"paymentToken":"%s","pricePerUnit":1000}`, TokenID)), nil, ownerAddress, "", false, gas, "NFT contract, token ID, and payment token required")

	// Missing tokenId
	CallMarket(t, ct, "list", []byte(fmt.Sprintf(`{"nftContract":"%s","tokenId":"","amount":5,"paymentToken":"%s","pricePerUnit":1000}`, NftContractID, TokenID)), nil, ownerAddress, "", false, gas, "NFT contract, token ID, and payment token required")

	// Missing paymentToken
	CallMarket(t, ct, "list", []byte(fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"","pricePerUnit":1000}`, NftContractID)), nil, ownerAddress, "", false, gas, "NFT contract, token ID, and payment token required")
}

func TestListWhenPaused(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	CallMarket(t, ct, "pause", nil, nil, ownerAddress, "", true, gas, "")

	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","pricePerUnit":1000}`, NftContractID, TokenID)
	CallMarket(t, ct, "list", []byte(payload), nil, ownerAddress, "", false, gas, "Contract is paused")
}

// ===================================
// Delist Tests
// ===================================

func TestDelistNft(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	seller := ownerAddress
	MintNft(t, ct, seller, "1", 10, 100)
	ApproveNftForMarket(t, ct, seller)

	// List
	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","pricePerUnit":1000}`, NftContractID, TokenID)
	CallMarket(t, ct, "list", []byte(payload), nil, seller, "", true, gas, "")

	// Delist
	_, _, logs := CallMarket(t, ct, "delist", []byte(`{"listingId":0}`), nil, seller, "", true, gas, "")
	AssertEventEmitted(t, logs, "delisted")

	// Verify listing is inactive
	result, _, _ := CallMarket(t, ct, "getListing", []byte(`{"listingId":0}`), nil, "hive:anyone", "", true, gas, "")
	listing := ParseListing(result)
	assert.False(t, listing.Active)

	// Verify NFT returned to seller
	assert.Equal(t, uint64(10), QueryNftBalance(t, ct, seller, "1"))
	assert.Equal(t, uint64(0), QueryNftBalance(t, ct, MarketContractAddress, "1"))
}

func TestDelistNotSeller(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	seller := ownerAddress
	MintNft(t, ct, seller, "1", 10, 100)
	ApproveNftForMarket(t, ct, seller)

	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","pricePerUnit":1000}`, NftContractID, TokenID)
	CallMarket(t, ct, "list", []byte(payload), nil, seller, "", true, gas, "")

	// Try to delist as different user
	CallMarket(t, ct, "delist", []byte(`{"listingId":0}`), nil, "hive:alice", "", false, gas, "Only seller can delist")
}

func TestDelistInactiveListing(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	seller := ownerAddress
	MintNft(t, ct, seller, "1", 10, 100)
	ApproveNftForMarket(t, ct, seller)

	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","pricePerUnit":1000}`, NftContractID, TokenID)
	CallMarket(t, ct, "list", []byte(payload), nil, seller, "", true, gas, "")

	// Delist once
	CallMarket(t, ct, "delist", []byte(`{"listingId":0}`), nil, seller, "", true, gas, "")

	// Try to delist again
	CallMarket(t, ct, "delist", []byte(`{"listingId":0}`), nil, seller, "", false, gas, "Listing not active")
}

// ===================================
// Buy Tests
// ===================================

func TestBuyFullListing(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	seller := ownerAddress
	buyer := "hive:buyer"

	// Mint NFT to seller, list it
	MintNft(t, ct, seller, "1", 5, 100)
	ApproveNftForMarket(t, ct, seller)
	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","pricePerUnit":1000}`, NftContractID, TokenID)
	CallMarket(t, ct, "list", []byte(payload), nil, seller, "", true, gas, "")

	// Mint payment tokens to buyer (5 * 1000 = 5000)
	MintAndApproveToken(t, ct, buyer, 5000)

	// Buy all 5
	_, _, logs := CallMarket(t, ct, "buy", []byte(`{"listingId":0,"amount":5}`), nil, buyer, "", true, gas, "")
	AssertEventEmitted(t, logs, "bought")

	// Verify listing is now inactive (fully sold)
	result, _, _ := CallMarket(t, ct, "getListing", []byte(`{"listingId":0}`), nil, "hive:anyone", "", true, gas, "")
	listing := ParseListing(result)
	assert.False(t, listing.Active)
	assert.Equal(t, uint64(0), listing.Amount)

	// Verify buyer has NFTs
	assert.Equal(t, uint64(5), QueryNftBalance(t, ct, buyer, "1"))

	// Verify fee distribution: 5000 * 250 / 10000 = 125 fee, 4875 to seller
	assert.Equal(t, uint64(125), QueryTokenBalance(t, ct, feeRecipientAddress))
	assert.Equal(t, uint64(4875), QueryTokenBalance(t, ct, seller))
	assert.Equal(t, uint64(0), QueryTokenBalance(t, ct, buyer))
}

func TestBuyPartialListing(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	seller := ownerAddress
	buyer := "hive:buyer"

	MintNft(t, ct, seller, "1", 10, 100)
	ApproveNftForMarket(t, ct, seller)
	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":10,"paymentToken":"%s","pricePerUnit":1000}`, NftContractID, TokenID)
	CallMarket(t, ct, "list", []byte(payload), nil, seller, "", true, gas, "")

	// Buy only 3 out of 10
	MintAndApproveToken(t, ct, buyer, 3000)
	CallMarket(t, ct, "buy", []byte(`{"listingId":0,"amount":3}`), nil, buyer, "", true, gas, "")

	// Listing should still be active with 7 remaining
	result, _, _ := CallMarket(t, ct, "getListing", []byte(`{"listingId":0}`), nil, "hive:anyone", "", true, gas, "")
	listing := ParseListing(result)
	assert.True(t, listing.Active)
	assert.Equal(t, uint64(7), listing.Amount)

	// Buyer has 3 NFTs
	assert.Equal(t, uint64(3), QueryNftBalance(t, ct, buyer, "1"))
}

func TestBuyMultiplePartialFills(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	seller := ownerAddress
	buyer1 := "hive:buyer1"
	buyer2 := "hive:buyer2"

	MintNft(t, ct, seller, "1", 10, 100)
	ApproveNftForMarket(t, ct, seller)
	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":10,"paymentToken":"%s","pricePerUnit":1000}`, NftContractID, TokenID)
	CallMarket(t, ct, "list", []byte(payload), nil, seller, "", true, gas, "")

	// Buyer 1 buys 4
	MintAndApproveToken(t, ct, buyer1, 4000)
	CallMarket(t, ct, "buy", []byte(`{"listingId":0,"amount":4}`), nil, buyer1, "", true, gas, "")

	// Buyer 2 buys 6 (remaining)
	MintAndApproveToken(t, ct, buyer2, 6000)
	CallMarket(t, ct, "buy", []byte(`{"listingId":0,"amount":6}`), nil, buyer2, "", true, gas, "")

	// Listing should be inactive
	result, _, _ := CallMarket(t, ct, "getListing", []byte(`{"listingId":0}`), nil, "hive:anyone", "", true, gas, "")
	listing := ParseListing(result)
	assert.False(t, listing.Active)
	assert.Equal(t, uint64(0), listing.Amount)

	// Both buyers have their NFTs
	assert.Equal(t, uint64(4), QueryNftBalance(t, ct, buyer1, "1"))
	assert.Equal(t, uint64(6), QueryNftBalance(t, ct, buyer2, "1"))
}

func TestBuyExceedsListingAmount(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	seller := ownerAddress
	buyer := "hive:buyer"

	MintNft(t, ct, seller, "1", 5, 100)
	ApproveNftForMarket(t, ct, seller)
	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","pricePerUnit":1000}`, NftContractID, TokenID)
	CallMarket(t, ct, "list", []byte(payload), nil, seller, "", true, gas, "")

	MintAndApproveToken(t, ct, buyer, 10000)
	CallMarket(t, ct, "buy", []byte(`{"listingId":0,"amount":10}`), nil, buyer, "", false, gas, "Insufficient listing amount")
}

func TestBuyZeroAmount(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	seller := ownerAddress
	MintNft(t, ct, seller, "1", 5, 100)
	ApproveNftForMarket(t, ct, seller)
	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","pricePerUnit":1000}`, NftContractID, TokenID)
	CallMarket(t, ct, "list", []byte(payload), nil, seller, "", true, gas, "")

	CallMarket(t, ct, "buy", []byte(`{"listingId":0,"amount":0}`), nil, "hive:buyer", "", false, gas, "Amount must be greater than zero")
}

func TestBuyInactiveListing(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	seller := ownerAddress
	MintNft(t, ct, seller, "1", 5, 100)
	ApproveNftForMarket(t, ct, seller)
	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","pricePerUnit":1000}`, NftContractID, TokenID)
	CallMarket(t, ct, "list", []byte(payload), nil, seller, "", true, gas, "")

	// Delist
	CallMarket(t, ct, "delist", []byte(`{"listingId":0}`), nil, seller, "", true, gas, "")

	// Try to buy
	CallMarket(t, ct, "buy", []byte(`{"listingId":0,"amount":1}`), nil, "hive:buyer", "", false, gas, "Listing not active")
}

func TestBuyWhenPaused(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	seller := ownerAddress
	MintNft(t, ct, seller, "1", 5, 100)
	ApproveNftForMarket(t, ct, seller)
	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","pricePerUnit":1000}`, NftContractID, TokenID)
	CallMarket(t, ct, "list", []byte(payload), nil, seller, "", true, gas, "")

	CallMarket(t, ct, "pause", nil, nil, ownerAddress, "", true, gas, "")
	CallMarket(t, ct, "buy", []byte(`{"listingId":0,"amount":1}`), nil, "hive:buyer", "", false, gas, "Contract is paused")
}

// ===================================
// Fee Verification Tests
// ===================================

func TestBuyWithZeroFee(t *testing.T) {
	ct := SetupContractTest()
	InitToken(t, ct)
	InitNft(t, ct)

	// Init with 0% fee
	payload := fmt.Sprintf(`{"feeBps":0,"feeRecipient":"%s"}`, feeRecipientAddress)
	CallMarket(t, ct, "init", []byte(payload), nil, ownerAddress, "", true, gas, "")

	seller := ownerAddress
	buyer := "hive:buyer"

	MintNft(t, ct, seller, "1", 5, 100)
	ApproveNftForMarket(t, ct, seller)
	listPayload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","pricePerUnit":1000}`, NftContractID, TokenID)
	CallMarket(t, ct, "list", []byte(listPayload), nil, seller, "", true, gas, "")

	MintAndApproveToken(t, ct, buyer, 5000)
	CallMarket(t, ct, "buy", []byte(`{"listingId":0,"amount":5}`), nil, buyer, "", true, gas, "")

	// No fee, seller gets all
	assert.Equal(t, uint64(0), QueryTokenBalance(t, ct, feeRecipientAddress))
	assert.Equal(t, uint64(5000), QueryTokenBalance(t, ct, seller))
}

func TestBuyFeeCalculation(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct) // 250 bps = 2.5%

	seller := ownerAddress
	buyer := "hive:buyer"

	MintNft(t, ct, seller, "1", 1, 1)
	ApproveNftForMarket(t, ct, seller)
	listPayload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":1,"paymentToken":"%s","pricePerUnit":10000}`, NftContractID, TokenID)
	CallMarket(t, ct, "list", []byte(listPayload), nil, seller, "", true, gas, "")

	MintAndApproveToken(t, ct, buyer, 10000)
	CallMarket(t, ct, "buy", []byte(`{"listingId":0,"amount":1}`), nil, buyer, "", true, gas, "")

	// 10000 * 250 / 10000 = 250 fee
	assert.Equal(t, uint64(250), QueryTokenBalance(t, ct, feeRecipientAddress))
	assert.Equal(t, uint64(9750), QueryTokenBalance(t, ct, seller))
}

// ===================================
// Update Listing Tests
// ===================================

func TestUpdateListing(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	seller := ownerAddress
	MintNft(t, ct, seller, "1", 5, 100)
	ApproveNftForMarket(t, ct, seller)
	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","pricePerUnit":1000}`, NftContractID, TokenID)
	CallMarket(t, ct, "list", []byte(payload), nil, seller, "", true, gas, "")

	// Update price
	_, _, logs := CallMarket(t, ct, "updateListing", []byte(`{"listingId":0,"newPrice":2000}`), nil, seller, "", true, gas, "")
	AssertEventEmitted(t, logs, "listing_updated")

	// Verify
	result, _, _ := CallMarket(t, ct, "getListing", []byte(`{"listingId":0}`), nil, "hive:anyone", "", true, gas, "")
	listing := ParseListing(result)
	assert.Equal(t, uint64(2000), listing.PricePerUnit)
}

func TestUpdateListingNotSeller(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	seller := ownerAddress
	MintNft(t, ct, seller, "1", 5, 100)
	ApproveNftForMarket(t, ct, seller)
	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","pricePerUnit":1000}`, NftContractID, TokenID)
	CallMarket(t, ct, "list", []byte(payload), nil, seller, "", true, gas, "")

	CallMarket(t, ct, "updateListing", []byte(`{"listingId":0,"newPrice":2000}`), nil, "hive:alice", "", false, gas, "Only seller can update listing")
}

func TestUpdateListingZeroPrice(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	seller := ownerAddress
	MintNft(t, ct, seller, "1", 5, 100)
	ApproveNftForMarket(t, ct, seller)
	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","pricePerUnit":1000}`, NftContractID, TokenID)
	CallMarket(t, ct, "list", []byte(payload), nil, seller, "", true, gas, "")

	CallMarket(t, ct, "updateListing", []byte(`{"listingId":0,"newPrice":0}`), nil, seller, "", false, gas, "Price must be greater than zero")
}

func TestUpdateListingInactive(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	seller := ownerAddress
	MintNft(t, ct, seller, "1", 5, 100)
	ApproveNftForMarket(t, ct, seller)
	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","pricePerUnit":1000}`, NftContractID, TokenID)
	CallMarket(t, ct, "list", []byte(payload), nil, seller, "", true, gas, "")

	// Delist first
	CallMarket(t, ct, "delist", []byte(`{"listingId":0}`), nil, seller, "", true, gas, "")

	// Try to update
	CallMarket(t, ct, "updateListing", []byte(`{"listingId":0,"newPrice":2000}`), nil, seller, "", false, gas, "Listing not active")
}

func TestBuyAfterPriceUpdate(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	seller := ownerAddress
	buyer := "hive:buyer"

	MintNft(t, ct, seller, "1", 5, 100)
	ApproveNftForMarket(t, ct, seller)
	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","pricePerUnit":1000}`, NftContractID, TokenID)
	CallMarket(t, ct, "list", []byte(payload), nil, seller, "", true, gas, "")

	// Update price to 2000
	CallMarket(t, ct, "updateListing", []byte(`{"listingId":0,"newPrice":2000}`), nil, seller, "", true, gas, "")

	// Buy 2 at new price: 2 * 2000 = 4000
	MintAndApproveToken(t, ct, buyer, 4000)
	CallMarket(t, ct, "buy", []byte(`{"listingId":0,"amount":2}`), nil, buyer, "", true, gas, "")

	// Fee: 4000 * 250 / 10000 = 100
	assert.Equal(t, uint64(100), QueryTokenBalance(t, ct, feeRecipientAddress))
	assert.Equal(t, uint64(3900), QueryTokenBalance(t, ct, seller))
	assert.Equal(t, uint64(2), QueryNftBalance(t, ct, buyer, "1"))
}
