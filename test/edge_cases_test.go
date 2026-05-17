package contract_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

// ===================================
// Soulbound Token Tests
// ===================================

func TestListSoulboundToken(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	// Mint soulbound NFT to owner
	payload := `{"to":"hive:tibfox","id":"soul1","amount":1,"maxSupply":1,"soulbound":true}`
	CallNft(t, ct, "mint", []byte(payload), nil, ownerAddress, true, gas, "")

	ApproveNftForMarket(t, ct, ownerAddress)

	// Try to list soulbound token
	listPayload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"soul1","amount":1,"paymentToken":"%s","pricePerUnit":1000}`, NftContractID, TokenID)
	CallMarket(t, ct, "list", []byte(listPayload), nil, ownerAddress, "", false, gas, "Cannot list soulbound tokens")
}

// ===================================
// Before Init Tests
// ===================================

func TestListBeforeInit(t *testing.T) {
	ct := SetupContractTest()
	InitToken(t, ct)
	InitNft(t, ct)

	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","pricePerUnit":1000}`, NftContractID, TokenID)
	CallMarket(t, ct, "list", []byte(payload), nil, ownerAddress, "", false, gas, "")
}

func TestBuyBeforeInit(t *testing.T) {
	ct := SetupContractTest()
	CallMarket(t, ct, "buy", []byte(`{"listingId":0,"amount":1}`), nil, "hive:buyer", "", false, gas, "")
}

func TestMakeOfferBeforeInit(t *testing.T) {
	ct := SetupContractTest()
	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","pricePerUnit":1000}`, NftContractID, TokenID)
	CallMarket(t, ct, "makeOffer", []byte(payload), nil, "hive:buyer", "", false, gas, "")
}

func TestSetFeeBeforeInit(t *testing.T) {
	ct := SetupContractTest()
	CallMarket(t, ct, "setFee", []byte(`{"feeBps":500}`), nil, ownerAddress, "", false, gas, "")
}

func TestPauseBeforeInit(t *testing.T) {
	ct := SetupContractTest()
	CallMarket(t, ct, "pause", nil, nil, ownerAddress, "", false, gas, "")
}

// ===================================
// Invalid Payload Tests
// ===================================

func TestListNoPayload(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	CallMarket(t, ct, "list", nil, nil, ownerAddress, "", false, gas, "Payload required")
}

func TestBuyNoPayload(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	CallMarket(t, ct, "buy", nil, nil, "hive:buyer", "", false, gas, "Payload required")
}

func TestDelistNoPayload(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	CallMarket(t, ct, "delist", nil, nil, ownerAddress, "", false, gas, "Payload required")
}

func TestMakeOfferNoPayload(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	CallMarket(t, ct, "makeOffer", nil, nil, "hive:buyer", "", false, gas, "Payload required")
}

func TestCancelOfferNoPayload(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	CallMarket(t, ct, "cancelOffer", nil, nil, "hive:buyer", "", false, gas, "Payload required")
}

func TestAcceptOfferNoPayload(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	CallMarket(t, ct, "acceptOffer", nil, nil, ownerAddress, "", false, gas, "Payload required")
}

func TestUpdateListingNoPayload(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	CallMarket(t, ct, "updateListing", nil, nil, ownerAddress, "", false, gas, "Payload required")
}

func TestSetFeeNoPayload(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	CallMarket(t, ct, "setFee", nil, nil, ownerAddress, "", false, gas, "Payload required")
}

func TestSetFeeRecipientNoPayload(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	CallMarket(t, ct, "setFeeRecipient", nil, nil, ownerAddress, "", false, gas, "Payload required")
}

func TestChangeOwnerNoPayload(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	CallMarket(t, ct, "changeOwner", nil, nil, ownerAddress, "", false, gas, "Payload required")
}

// ===================================
// Nonexistent Listing/Offer Tests
// ===================================

func TestGetNonexistentListing(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	CallMarket(t, ct, "getListing", []byte(`{"listingId":999}`), nil, "hive:anyone", "", false, gas, "Listing not found")
}

func TestGetNonexistentOffer(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	CallMarket(t, ct, "getOffer", []byte(`{"offerId":999}`), nil, "hive:anyone", "", false, gas, "Offer not found")
}

func TestBuyNonexistentListing(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	CallMarket(t, ct, "buy", []byte(`{"listingId":999,"amount":1}`), nil, "hive:buyer", "", false, gas, "Listing not active")
}

func TestDelistNonexistentListing(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	CallMarket(t, ct, "delist", []byte(`{"listingId":999}`), nil, ownerAddress, "", false, gas, "Listing not active")
}

func TestCancelNonexistentOffer(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	CallMarket(t, ct, "cancelOffer", []byte(`{"offerId":999}`), nil, "hive:buyer", "", false, gas, "Offer not active")
}

func TestAcceptNonexistentOffer(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	CallMarket(t, ct, "acceptOffer", []byte(`{"offerId":999}`), nil, ownerAddress, "", false, gas, "Offer not active")
}

// ===================================
// Pause Edge Cases
// ===================================

func TestDelistWhenPaused(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	seller := ownerAddress
	MintNft(t, ct, seller, "1", 5, 100)
	ApproveNftForMarket(t, ct, seller)

	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","pricePerUnit":1000}`, NftContractID, TokenID)
	CallMarket(t, ct, "list", []byte(payload), nil, seller, "", true, gas, "")

	CallMarket(t, ct, "pause", nil, nil, ownerAddress, "", true, gas, "")
	// Delist should work when paused so sellers can recover escrowed NFTs
	CallMarket(t, ct, "delist", []byte(`{"listingId":0}`), nil, seller, "", true, gas, "")

	// Verify NFT returned
	assert.Equal(t, uint64(5), QueryNftBalance(t, ct, seller, "1"))
}

func TestCancelOfferWhenPaused(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	buyer := "hive:buyer"
	MintNft(t, ct, ownerAddress, "1", 10, 100)
	MintAndApproveToken(t, ct, buyer, 5000)

	offerPayload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","pricePerUnit":1000}`, NftContractID, TokenID)
	CallMarket(t, ct, "makeOffer", []byte(offerPayload), nil, buyer, "", true, gas, "")

	CallMarket(t, ct, "pause", nil, nil, ownerAddress, "", true, gas, "")
	// CancelOffer should work when paused so buyers can recover escrowed payments
	CallMarket(t, ct, "cancelOffer", []byte(`{"offerId":0}`), nil, buyer, "", true, gas, "")

	// Verify payment returned
	assert.Equal(t, uint64(5000), QueryTokenBalance(t, ct, buyer))
}

func TestUpdateListingWhenPaused(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	seller := ownerAddress
	MintNft(t, ct, seller, "1", 5, 100)
	ApproveNftForMarket(t, ct, seller)

	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","pricePerUnit":1000}`, NftContractID, TokenID)
	CallMarket(t, ct, "list", []byte(payload), nil, seller, "", true, gas, "")

	CallMarket(t, ct, "pause", nil, nil, ownerAddress, "", true, gas, "")
	CallMarket(t, ct, "updateListing", []byte(`{"listingId":0,"newPrice":2000}`), nil, seller, "", false, gas, "Contract is paused")
}

func TestQueriesWorkWhenPaused(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	seller := ownerAddress
	MintNft(t, ct, seller, "1", 5, 100)
	ApproveNftForMarket(t, ct, seller)

	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","pricePerUnit":1000}`, NftContractID, TokenID)
	CallMarket(t, ct, "list", []byte(payload), nil, seller, "", true, gas, "")

	CallMarket(t, ct, "pause", nil, nil, ownerAddress, "", true, gas, "")

	// Queries should still work
	result, _, _ := CallMarket(t, ct, "getListing", []byte(`{"listingId":0}`), nil, "hive:anyone", "", true, gas, "")
	listing := ParseListing(result)
	assert.True(t, listing.Active)

	CallMarket(t, ct, "getInfo", nil, nil, "hive:anyone", "", true, gas, "")
	CallMarket(t, ct, "getOwner", nil, nil, "hive:anyone", "", true, gas, "")
	CallMarket(t, ct, "isPaused", nil, nil, "hive:anyone", "", true, gas, "")
}

// ===================================
// Admin Works When Paused
// ===================================

func TestAdminWorksWhenPaused(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	CallMarket(t, ct, "pause", nil, nil, ownerAddress, "", true, gas, "")

	// Admin functions should still work
	CallMarket(t, ct, "setFee", []byte(`{"feeBps":500}`), nil, ownerAddress, "", true, gas, "")
	CallMarket(t, ct, "setFeeRecipient", []byte(`{"feeRecipient":"hive:newfee"}`), nil, ownerAddress, "", true, gas, "")
	CallMarket(t, ct, "changeOwner", []byte(`{"newOwner":"hive:newowner"}`), nil, ownerAddress, "", true, gas, "")
}

// ===================================
// Listing and Offer After Unpause
// ===================================

func TestListAfterUnpause(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	seller := ownerAddress
	MintNft(t, ct, seller, "1", 5, 100)
	ApproveNftForMarket(t, ct, seller)

	// Pause then unpause
	CallMarket(t, ct, "pause", nil, nil, ownerAddress, "", true, gas, "")
	CallMarket(t, ct, "unpause", nil, nil, ownerAddress, "", true, gas, "")

	// Should work again
	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","pricePerUnit":1000}`, NftContractID, TokenID)
	CallMarket(t, ct, "list", []byte(payload), nil, seller, "", true, gas, "")
}

// ===================================
// Full E2E Flow Tests
// ===================================

func TestE2EListBuyDelist(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	seller := ownerAddress
	buyer := "hive:buyer"

	// Mint 10 NFTs
	MintNft(t, ct, seller, "1", 10, 100)
	ApproveNftForMarket(t, ct, seller)

	// List all 10
	listPayload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":10,"paymentToken":"%s","pricePerUnit":100}`, NftContractID, TokenID)
	CallMarket(t, ct, "list", []byte(listPayload), nil, seller, "", true, gas, "")

	// Buy 3
	MintAndApproveToken(t, ct, buyer, 300)
	CallMarket(t, ct, "buy", []byte(`{"listingId":0,"amount":3}`), nil, buyer, "", true, gas, "")

	// Verify 7 remaining
	result, _, _ := CallMarket(t, ct, "getListing", []byte(`{"listingId":0}`), nil, "hive:anyone", "", true, gas, "")
	listing := ParseListing(result)
	assert.Equal(t, uint64(7), listing.Amount)
	assert.True(t, listing.Active)

	// Delist remaining
	CallMarket(t, ct, "delist", []byte(`{"listingId":0}`), nil, seller, "", true, gas, "")

	// Verify listing inactive and NFTs returned
	result2, _, _ := CallMarket(t, ct, "getListing", []byte(`{"listingId":0}`), nil, "hive:anyone", "", true, gas, "")
	listing2 := ParseListing(result2)
	assert.False(t, listing2.Active)

	// Seller has 7 back, buyer has 3
	assert.Equal(t, uint64(7), QueryNftBalance(t, ct, seller, "1"))
	assert.Equal(t, uint64(3), QueryNftBalance(t, ct, buyer, "1"))
}

func TestE2EOfferCancelAccept(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	seller := ownerAddress
	buyer1 := "hive:buyer1"
	buyer2 := "hive:buyer2"

	MintNft(t, ct, seller, "1", 10, 100)

	// Buyer 1 makes offer then cancels
	MintAndApproveToken(t, ct, buyer1, 3000)
	offerPayload1 := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":3,"paymentToken":"%s","pricePerUnit":1000}`, NftContractID, TokenID)
	CallMarket(t, ct, "makeOffer", []byte(offerPayload1), nil, buyer1, "", true, gas, "")
	CallMarket(t, ct, "cancelOffer", []byte(`{"offerId":0}`), nil, buyer1, "", true, gas, "")

	// Buyer 1 gets refund
	assert.Equal(t, uint64(3000), QueryTokenBalance(t, ct, buyer1))

	// Buyer 2 makes offer and seller accepts
	MintAndApproveToken(t, ct, buyer2, 5000)
	offerPayload2 := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","pricePerUnit":1000}`, NftContractID, TokenID)
	CallMarket(t, ct, "makeOffer", []byte(offerPayload2), nil, buyer2, "", true, gas, "")

	ApproveNftForMarket(t, ct, seller)
	CallMarket(t, ct, "acceptOffer", []byte(`{"offerId":1}`), nil, seller, "", true, gas, "")

	// Buyer 2 gets NFTs
	assert.Equal(t, uint64(5), QueryNftBalance(t, ct, buyer2, "1"))
	assert.Equal(t, uint64(5), QueryNftBalance(t, ct, seller, "1"))

	// Seller gets payment minus fee
	// Fee: 5000 * 250 / 10000 = 125
	assert.Equal(t, uint64(4875), QueryTokenBalance(t, ct, seller))
	assert.Equal(t, uint64(125), QueryTokenBalance(t, ct, feeRecipientAddress))
}

// ===================================
// Unique NFT (amount=1) Tests
// ===================================

func TestListAndBuyUniqueNft(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	seller := ownerAddress
	buyer := "hive:buyer"

	// Mint 1 unique NFT
	MintNft(t, ct, seller, "unique1", 1, 1)
	ApproveNftForMarket(t, ct, seller)

	// List it
	listPayload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"unique1","amount":1,"paymentToken":"%s","pricePerUnit":50000}`, NftContractID, TokenID)
	CallMarket(t, ct, "list", []byte(listPayload), nil, seller, "", true, gas, "")

	// Buy it
	MintAndApproveToken(t, ct, buyer, 50000)
	CallMarket(t, ct, "buy", []byte(`{"listingId":0,"amount":1}`), nil, buyer, "", true, gas, "")

	// Verify ownership transfer
	assert.Equal(t, uint64(1), QueryNftBalance(t, ct, buyer, "unique1"))
	assert.Equal(t, uint64(0), QueryNftBalance(t, ct, seller, "unique1"))
	assert.Equal(t, uint64(0), QueryNftBalance(t, ct, MarketContractAddress, "unique1"))

	// Fee: 50000 * 250 / 10000 = 1250
	assert.Equal(t, uint64(1250), QueryTokenBalance(t, ct, feeRecipientAddress))
	assert.Equal(t, uint64(48750), QueryTokenBalance(t, ct, seller))
}

// ===================================
// Ownership Change Tests
// ===================================

func TestChangeOwnerThenAdminActions(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	newOwner := "hive:newowner"
	CallMarket(t, ct, "changeOwner", []byte(fmt.Sprintf(`{"newOwner":"%s"}`, newOwner)), nil, ownerAddress, "", true, gas, "")

	// Old owner can no longer admin
	CallMarket(t, ct, "setFee", []byte(`{"feeBps":500}`), nil, ownerAddress, "", false, gas, "Only owner can set fee")
	CallMarket(t, ct, "pause", nil, nil, ownerAddress, "", false, gas, "Only owner can pause")

	// New owner can admin
	CallMarket(t, ct, "setFee", []byte(`{"feeBps":500}`), nil, newOwner, "", true, gas, "")
	CallMarket(t, ct, "pause", nil, nil, newOwner, "", true, gas, "")
}

// ===================================
// Re-listing After Buy Tests
// ===================================

func TestBuyerRelistsNft(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	seller := ownerAddress
	buyer := "hive:buyer"

	// Mint and list
	MintNft(t, ct, seller, "1", 5, 100)
	ApproveNftForMarket(t, ct, seller)
	listPayload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","pricePerUnit":1000}`, NftContractID, TokenID)
	CallMarket(t, ct, "list", []byte(listPayload), nil, seller, "", true, gas, "")

	// Buyer buys all
	MintAndApproveToken(t, ct, buyer, 5000)
	CallMarket(t, ct, "buy", []byte(`{"listingId":0,"amount":5}`), nil, buyer, "", true, gas, "")

	// Buyer now re-lists at higher price
	ApproveNftForMarket(t, ct, buyer)
	relistPayload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","pricePerUnit":2000}`, NftContractID, TokenID)
	CallMarket(t, ct, "list", []byte(relistPayload), nil, buyer, "", true, gas, "")

	// Verify new listing
	result, _, _ := CallMarket(t, ct, "getListing", []byte(`{"listingId":1}`), nil, "hive:anyone", "", true, gas, "")
	listing := ParseListing(result)
	assert.Equal(t, uint64(1), listing.ListingId)
	assert.Equal(t, buyer, listing.Seller)
	assert.Equal(t, uint64(5), listing.Amount)
	assert.Equal(t, uint64(2000), listing.PricePerUnit)
	assert.True(t, listing.Active)
}

// ===================================
// Event Verification Tests
// ===================================

func TestListEventDetails(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	seller := ownerAddress
	MintNft(t, ct, seller, "1", 5, 100)
	ApproveNftForMarket(t, ct, seller)

	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","pricePerUnit":1000}`, NftContractID, TokenID)
	_, _, logs := CallMarket(t, ct, "list", []byte(payload), nil, seller, "", true, gas, "")

	AssertEventContains(t, logs, "listed", `"seller":"hive:tibfox"`)
	AssertEventContains(t, logs, "listed", `"amount":5`)
	AssertEventContains(t, logs, "listed", `"pricePerUnit":1000`)
}

func TestBuyEventDetails(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	seller := ownerAddress
	buyer := "hive:buyer"

	MintNft(t, ct, seller, "1", 5, 100)
	ApproveNftForMarket(t, ct, seller)
	listPayload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","pricePerUnit":1000}`, NftContractID, TokenID)
	CallMarket(t, ct, "list", []byte(listPayload), nil, seller, "", true, gas, "")

	MintAndApproveToken(t, ct, buyer, 2000)
	_, _, logs := CallMarket(t, ct, "buy", []byte(`{"listingId":0,"amount":2}`), nil, buyer, "", true, gas, "")

	AssertEventContains(t, logs, "bought", `"buyer":"hive:buyer"`)
	AssertEventContains(t, logs, "bought", `"amount":2`)
	AssertEventContains(t, logs, "bought", `"totalPrice":2000`)
	// Fee: 2000 * 250 / 10000 = 50
	AssertEventContains(t, logs, "bought", `"fee":50`)
}

func TestOfferMadeEventDetails(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	buyer := "hive:buyer"
	MintNft(t, ct, ownerAddress, "1", 10, 100)
	MintAndApproveToken(t, ct, buyer, 5000)

	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","pricePerUnit":1000}`, NftContractID, TokenID)
	_, _, logs := CallMarket(t, ct, "makeOffer", []byte(payload), nil, buyer, "", true, gas, "")

	AssertEventContains(t, logs, "offer_made", `"buyer":"hive:buyer"`)
	AssertEventContains(t, logs, "offer_made", `"amount":5`)
	AssertEventContains(t, logs, "offer_made", `"pricePerUnit":1000`)
}

func TestOfferAcceptedEventDetails(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	seller := ownerAddress
	buyer := "hive:buyer"

	MintNft(t, ct, seller, "1", 5, 100)
	MintAndApproveToken(t, ct, buyer, 5000)

	offerPayload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","pricePerUnit":1000}`, NftContractID, TokenID)
	CallMarket(t, ct, "makeOffer", []byte(offerPayload), nil, buyer, "", true, gas, "")

	ApproveNftForMarket(t, ct, seller)
	_, _, logs := CallMarket(t, ct, "acceptOffer", []byte(`{"offerId":0}`), nil, seller, "", true, gas, "")

	AssertEventContains(t, logs, "offer_accepted", `"seller":"hive:tibfox"`)
	AssertEventContains(t, logs, "offer_accepted", `"buyer":"hive:buyer"`)
	AssertEventContains(t, logs, "offer_accepted", `"totalPrice":5000`)
	// Fee: 5000 * 250 / 10000 = 125
	AssertEventContains(t, logs, "offer_accepted", `"fee":125`)
}
