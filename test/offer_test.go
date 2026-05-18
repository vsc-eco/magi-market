package contract_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

// ===================================
// Make Offer Tests
// ===================================

func TestMakeOffer(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	buyer := "hive:buyer"

	// Mint NFT so it exists
	MintNft(t, ct, ownerAddress, "1", 10, 100)

	// Buyer gets tokens and approves marketplace
	MintAndApproveToken(t, ct, buyer, 5000)

	// Make offer: 5 units at 1000 each = 5000 total
	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","pricePerUnit":"1000"}`, NftContractID, TokenID)
	_, _, logs := CallMarket(t, ct, "makeOffer", []byte(payload), nil, buyer, "", true, gas, "")
	AssertEventEmitted(t, logs, "offer_made")
	AssertEventContains(t, logs, "offer_made", `"offerId":0`)

	// Verify offer
	result, _, _ := CallMarket(t, ct, "getOffer", []byte(`{"offerId":0}`), nil, "hive:anyone", "", true, gas, "")
	offer := ParseOffer(result)
	assert.Equal(t, uint64(0), offer.OfferId)
	assert.Equal(t, buyer, offer.Buyer)
	assert.Equal(t, NftContractID, offer.NftContract)
	assert.Equal(t, "1", offer.TokenId)
	assert.Equal(t, uint64(5), offer.Amount)
	assert.Equal(t, "1000", offer.PricePerUnit)
	assert.Equal(t, TokenID, offer.PaymentToken)
	assert.True(t, offer.Active)

	// Payment escrowed in marketplace
	assert.Equal(t, uint64(5000), QueryTokenBalance(t, ct, MarketContractAddress))
	assert.Equal(t, uint64(0), QueryTokenBalance(t, ct, buyer))
}

func TestMakeMultipleOffers(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	buyer1 := "hive:buyer1"
	buyer2 := "hive:buyer2"

	MintNft(t, ct, ownerAddress, "1", 10, 100)

	MintAndApproveToken(t, ct, buyer1, 3000)
	MintAndApproveToken(t, ct, buyer2, 5000)

	payload1 := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":3,"paymentToken":"%s","pricePerUnit":"1000"}`, NftContractID, TokenID)
	CallMarket(t, ct, "makeOffer", []byte(payload1), nil, buyer1, "", true, gas, "")

	payload2 := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","pricePerUnit":"1000"}`, NftContractID, TokenID)
	CallMarket(t, ct, "makeOffer", []byte(payload2), nil, buyer2, "", true, gas, "")

	// Verify both offers
	result1, _, _ := CallMarket(t, ct, "getOffer", []byte(`{"offerId":0}`), nil, "hive:anyone", "", true, gas, "")
	offer1 := ParseOffer(result1)
	assert.Equal(t, buyer1, offer1.Buyer)
	assert.Equal(t, uint64(3), offer1.Amount)

	result2, _, _ := CallMarket(t, ct, "getOffer", []byte(`{"offerId":1}`), nil, "hive:anyone", "", true, gas, "")
	offer2 := ParseOffer(result2)
	assert.Equal(t, buyer2, offer2.Buyer)
	assert.Equal(t, uint64(5), offer2.Amount)
}

func TestMakeOfferZeroAmount(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":0,"paymentToken":"%s","pricePerUnit":"1000"}`, NftContractID, TokenID)
	CallMarket(t, ct, "makeOffer", []byte(payload), nil, "hive:buyer", "", false, gas, "Amount must be greater than zero")
}

func TestMakeOfferZeroPrice(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","pricePerUnit":"0"}`, NftContractID, TokenID)
	CallMarket(t, ct, "makeOffer", []byte(payload), nil, "hive:buyer", "", false, gas, "Price must be greater than zero")
}

func TestMakeOfferMissingFields(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	CallMarket(t, ct, "makeOffer", []byte(fmt.Sprintf(`{"nftContract":"","tokenId":"1","amount":5,"paymentToken":"%s","pricePerUnit":"1000"}`, TokenID)), nil, "hive:buyer", "", false, gas, "NFT contract and payment token required")
}

func TestMakeOfferWhenPaused(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	CallMarket(t, ct, "pause", nil, nil, ownerAddress, "", true, gas, "")

	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","pricePerUnit":"1000"}`, NftContractID, TokenID)
	CallMarket(t, ct, "makeOffer", []byte(payload), nil, "hive:buyer", "", false, gas, "Contract is paused")
}

// ===================================
// Cancel Offer Tests
// ===================================

func TestCancelOffer(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	buyer := "hive:buyer"
	MintNft(t, ct, ownerAddress, "1", 10, 100)
	MintAndApproveToken(t, ct, buyer, 5000)

	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","pricePerUnit":"1000"}`, NftContractID, TokenID)
	CallMarket(t, ct, "makeOffer", []byte(payload), nil, buyer, "", true, gas, "")

	// Cancel offer
	_, _, logs := CallMarket(t, ct, "cancelOffer", []byte(`{"offerId":0}`), nil, buyer, "", true, gas, "")
	AssertEventEmitted(t, logs, "offer_cancelled")

	// Verify offer inactive
	result, _, _ := CallMarket(t, ct, "getOffer", []byte(`{"offerId":0}`), nil, "hive:anyone", "", true, gas, "")
	offer := ParseOffer(result)
	assert.False(t, offer.Active)

	// Payment returned to buyer
	assert.Equal(t, uint64(5000), QueryTokenBalance(t, ct, buyer))
	assert.Equal(t, uint64(0), QueryTokenBalance(t, ct, MarketContractAddress))
}

func TestCancelOfferNotBuyer(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	buyer := "hive:buyer"
	MintNft(t, ct, ownerAddress, "1", 10, 100)
	MintAndApproveToken(t, ct, buyer, 5000)

	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","pricePerUnit":"1000"}`, NftContractID, TokenID)
	CallMarket(t, ct, "makeOffer", []byte(payload), nil, buyer, "", true, gas, "")

	// Try to cancel as different user
	CallMarket(t, ct, "cancelOffer", []byte(`{"offerId":0}`), nil, "hive:alice", "", false, gas, "Only buyer can cancel offer")
}

func TestCancelOfferInactive(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	buyer := "hive:buyer"
	MintNft(t, ct, ownerAddress, "1", 10, 100)
	MintAndApproveToken(t, ct, buyer, 5000)

	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","pricePerUnit":"1000"}`, NftContractID, TokenID)
	CallMarket(t, ct, "makeOffer", []byte(payload), nil, buyer, "", true, gas, "")

	// Cancel once
	CallMarket(t, ct, "cancelOffer", []byte(`{"offerId":0}`), nil, buyer, "", true, gas, "")

	// Try again
	CallMarket(t, ct, "cancelOffer", []byte(`{"offerId":0}`), nil, buyer, "", false, gas, "Offer not active")
}

// ===================================
// Accept Offer Tests
// ===================================

func TestAcceptOffer(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	seller := ownerAddress
	buyer := "hive:buyer"

	// Mint NFT to seller
	MintNft(t, ct, seller, "1", 10, 100)

	// Buyer makes offer
	MintAndApproveToken(t, ct, buyer, 5000)
	offerPayload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","pricePerUnit":"1000"}`, NftContractID, TokenID)
	CallMarket(t, ct, "makeOffer", []byte(offerPayload), nil, buyer, "", true, gas, "")

	// Seller approves marketplace for NFT
	ApproveNftForMarket(t, ct, seller)

	// Seller accepts offer
	_, _, logs := CallMarket(t, ct, "acceptOffer", []byte(`{"offerId":0}`), nil, seller, "", true, gas, "")
	AssertEventEmitted(t, logs, "offer_accepted")

	// Verify offer is inactive
	result, _, _ := CallMarket(t, ct, "getOffer", []byte(`{"offerId":0}`), nil, "hive:anyone", "", true, gas, "")
	offer := ParseOffer(result)
	assert.False(t, offer.Active)

	// Buyer gets NFT
	assert.Equal(t, uint64(5), QueryNftBalance(t, ct, buyer, "1"))
	assert.Equal(t, uint64(5), QueryNftBalance(t, ct, seller, "1"))

	// Fee: 5000 * 250 / 10000 = 125
	assert.Equal(t, uint64(125), QueryTokenBalance(t, ct, feeRecipientAddress))
	assert.Equal(t, uint64(4875), QueryTokenBalance(t, ct, seller))
	assert.Equal(t, uint64(0), QueryTokenBalance(t, ct, MarketContractAddress))
}

func TestAcceptOfferInactive(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	buyer := "hive:buyer"
	MintNft(t, ct, ownerAddress, "1", 10, 100)
	MintAndApproveToken(t, ct, buyer, 5000)

	offerPayload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","pricePerUnit":"1000"}`, NftContractID, TokenID)
	CallMarket(t, ct, "makeOffer", []byte(offerPayload), nil, buyer, "", true, gas, "")

	// Cancel the offer
	CallMarket(t, ct, "cancelOffer", []byte(`{"offerId":0}`), nil, buyer, "", true, gas, "")

	// Try to accept cancelled offer
	ApproveNftForMarket(t, ct, ownerAddress)
	CallMarket(t, ct, "acceptOffer", []byte(`{"offerId":0}`), nil, ownerAddress, "", false, gas, "Offer not active")
}

func TestAcceptOfferWithZeroFee(t *testing.T) {
	ct := SetupContractTest()
	InitToken(t, ct)
	InitNft(t, ct)

	// Init with 0% fee
	payload := fmt.Sprintf(`{"feeBps":0,"feeRecipient":"%s"}`, feeRecipientAddress)
	CallMarket(t, ct, "init", []byte(payload), nil, ownerAddress, "", true, gas, "")

	seller := ownerAddress
	buyer := "hive:buyer"

	MintNft(t, ct, seller, "1", 5, 100)
	MintAndApproveToken(t, ct, buyer, 5000)

	offerPayload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","pricePerUnit":"1000"}`, NftContractID, TokenID)
	CallMarket(t, ct, "makeOffer", []byte(offerPayload), nil, buyer, "", true, gas, "")

	ApproveNftForMarket(t, ct, seller)
	CallMarket(t, ct, "acceptOffer", []byte(`{"offerId":0}`), nil, seller, "", true, gas, "")

	// No fee
	assert.Equal(t, uint64(0), QueryTokenBalance(t, ct, feeRecipientAddress))
	assert.Equal(t, uint64(5000), QueryTokenBalance(t, ct, seller))
	assert.Equal(t, uint64(5), QueryNftBalance(t, ct, buyer, "1"))
}

func TestAcceptOfferWhenPaused(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	buyer := "hive:buyer"
	MintNft(t, ct, ownerAddress, "1", 10, 100)
	ApproveNftForMarket(t, ct, ownerAddress)
	MintAndApproveToken(t, ct, buyer, 5000)

	offerPayload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","pricePerUnit":"1000"}`, NftContractID, TokenID)
	CallMarket(t, ct, "makeOffer", []byte(offerPayload), nil, buyer, "", true, gas, "")

	// acceptOffer works even when paused so sellers can finalize existing offers
	CallMarket(t, ct, "pause", nil, nil, ownerAddress, "", true, gas, "")
	CallMarket(t, ct, "acceptOffer", []byte(`{"offerId":0}`), nil, ownerAddress, "", true, gas, "")

	// Verify NFT transferred to buyer
	assert.Equal(t, uint64(5), QueryNftBalance(t, ct, buyer, "1"))
}

// ===================================
// Full Offer Flow Tests
// ===================================

func TestFullOfferFlowWithFee(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct) // 250 bps = 2.5%

	seller := ownerAddress
	buyer := "hive:buyer"

	// Mint 1 unique NFT to seller
	MintNft(t, ct, seller, "42", 1, 1)

	// Buyer offers 10000 for it
	MintAndApproveToken(t, ct, buyer, 10000)
	offerPayload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"42","amount":1,"paymentToken":"%s","pricePerUnit":"10000"}`, NftContractID, TokenID)
	CallMarket(t, ct, "makeOffer", []byte(offerPayload), nil, buyer, "", true, gas, "")

	// Payment escrowed
	assert.Equal(t, uint64(10000), QueryTokenBalance(t, ct, MarketContractAddress))

	// Seller accepts
	ApproveNftForMarket(t, ct, seller)
	CallMarket(t, ct, "acceptOffer", []byte(`{"offerId":0}`), nil, seller, "", true, gas, "")

	// Buyer has NFT
	assert.Equal(t, uint64(1), QueryNftBalance(t, ct, buyer, "42"))
	assert.Equal(t, uint64(0), QueryNftBalance(t, ct, seller, "42"))

	// Fee: 10000 * 250 / 10000 = 250
	assert.Equal(t, uint64(250), QueryTokenBalance(t, ct, feeRecipientAddress))
	assert.Equal(t, uint64(9750), QueryTokenBalance(t, ct, seller))
	assert.Equal(t, uint64(0), QueryTokenBalance(t, ct, MarketContractAddress))
}

func TestOfferAndListingSameNft(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	seller := ownerAddress
	buyer := "hive:buyer"

	// Mint 10 NFTs
	MintNft(t, ct, seller, "1", 10, 100)
	ApproveNftForMarket(t, ct, seller)

	// List 5 of them
	listPayload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","pricePerUnit":"1000"}`, NftContractID, TokenID)
	CallMarket(t, ct, "list", []byte(listPayload), nil, seller, "", true, gas, "")

	// Buyer makes offer for 3 (separate from listing)
	MintAndApproveToken(t, ct, buyer, 6000)
	offerPayload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":3,"paymentToken":"%s","pricePerUnit":"2000"}`, NftContractID, TokenID)
	CallMarket(t, ct, "makeOffer", []byte(offerPayload), nil, buyer, "", true, gas, "")

	// Both listing and offer coexist
	result1, _, _ := CallMarket(t, ct, "getListing", []byte(`{"listingId":0}`), nil, "hive:anyone", "", true, gas, "")
	listing := ParseListing(result1)
	assert.True(t, listing.Active)

	result2, _, _ := CallMarket(t, ct, "getOffer", []byte(`{"offerId":0}`), nil, "hive:anyone", "", true, gas, "")
	offer := ParseOffer(result2)
	assert.True(t, offer.Active)
}
