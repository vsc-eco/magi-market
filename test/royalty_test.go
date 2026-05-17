package contract_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

// ===================================
// Royalty Configuration Tests
// ===================================

func TestSetRoyaltyByCollectionOwner(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	payload := fmt.Sprintf(`{"nftContract":"%s","royaltyBps":500,"royaltyRecipient":"hive:creator"}`, NftContractID)
	CallMarket(t, ct, "setRoyalty", []byte(payload), nil, ownerAddress, "", true, gas, "")

	// Query royalty
	result, _, _ := CallMarket(t, ct, "getRoyalty", []byte(fmt.Sprintf(`{"nftContract":"%s"}`, NftContractID)), nil, "hive:anyone", "", true, gas, "")
	royalty := ParseRoyalty(result)
	assert.Equal(t, uint64(500), royalty.RoyaltyBps)
	assert.Equal(t, "hive:creator", royalty.RoyaltyRecipient)
}

func TestSetRoyaltyByNonOwner(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	payload := fmt.Sprintf(`{"nftContract":"%s","royaltyBps":500,"royaltyRecipient":"hive:creator"}`, NftContractID)
	CallMarket(t, ct, "setRoyalty", []byte(payload), nil, "hive:random", "", false, gas, "Only collection owner can set royalty")
}

func TestSetRoyaltyTooHigh(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	payload := fmt.Sprintf(`{"nftContract":"%s","royaltyBps":5001,"royaltyRecipient":"hive:creator"}`, NftContractID)
	CallMarket(t, ct, "setRoyalty", []byte(payload), nil, ownerAddress, "", false, gas, "Royalty must be <= 5000")
}

func TestSetRoyaltyNoRecipient(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	payload := fmt.Sprintf(`{"nftContract":"%s","royaltyBps":500,"royaltyRecipient":""}`, NftContractID)
	CallMarket(t, ct, "setRoyalty", []byte(payload), nil, ownerAddress, "", false, gas, "Royalty recipient required")
}

func TestSetRoyaltyZero(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	// Set royalty to 0 with empty recipient (allowed)
	payload := fmt.Sprintf(`{"nftContract":"%s","royaltyBps":0,"royaltyRecipient":""}`, NftContractID)
	CallMarket(t, ct, "setRoyalty", []byte(payload), nil, ownerAddress, "", true, gas, "")
}

func TestGetRoyaltyNoContract(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	CallMarket(t, ct, "getRoyalty", []byte(`{"nftContract":""}`), nil, "hive:anyone", "", false, gas, "NFT contract required")
}

func TestGetRoyaltyDefault(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	// No royalty set - should return 0
	result, _, _ := CallMarket(t, ct, "getRoyalty", []byte(fmt.Sprintf(`{"nftContract":"%s"}`, NftContractID)), nil, "hive:anyone", "", true, gas, "")
	royalty := ParseRoyalty(result)
	assert.Equal(t, uint64(0), royalty.RoyaltyBps)
}

// ===================================
// Royalty Distribution Tests
// ===================================

func TestBuyWithRoyalty(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	// Set 10% royalty
	payload := fmt.Sprintf(`{"nftContract":"%s","royaltyBps":1000,"royaltyRecipient":"hive:creator"}`, NftContractID)
	CallMarket(t, ct, "setRoyalty", []byte(payload), nil, ownerAddress, "", true, gas, "")

	MintNft(t, ct, ownerAddress, "1", 1, 1)
	ApproveNftForMarket(t, ct, ownerAddress)

	listPayload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":1,"paymentToken":"%s","pricePerUnit":10000}`, NftContractID, TokenID)
	CallMarket(t, ct, "list", []byte(listPayload), nil, ownerAddress, "", true, gas, "")

	buyer := "hive:buyer"
	MintAndApproveToken(t, ct, buyer, 10000)

	CallMarket(t, ct, "buy", []byte(`{"listingId":0,"amount":1}`), nil, buyer, "", true, gas, "")

	// Total = 10000, fee = 2.5% = 250, royalty = 10% = 1000, seller = 10000 - 250 - 1000 = 8750
	sellerBalance := QueryTokenBalance(t, ct, ownerAddress)
	assert.Equal(t, uint64(8750), sellerBalance)

	feeBalance := QueryTokenBalance(t, ct, feeRecipientAddress)
	assert.Equal(t, uint64(250), feeBalance)

	creatorBalance := QueryTokenBalance(t, ct, "hive:creator")
	assert.Equal(t, uint64(1000), creatorBalance)
}

func TestAcceptOfferWithRoyalty(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	// Set 5% royalty
	payload := fmt.Sprintf(`{"nftContract":"%s","royaltyBps":500,"royaltyRecipient":"hive:creator"}`, NftContractID)
	CallMarket(t, ct, "setRoyalty", []byte(payload), nil, ownerAddress, "", true, gas, "")

	MintNft(t, ct, ownerAddress, "1", 5, 100)

	buyer := "hive:buyer"
	MintAndApproveToken(t, ct, buyer, 50000)

	offerPayload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","pricePerUnit":1000}`, NftContractID, TokenID)
	CallMarket(t, ct, "makeOffer", []byte(offerPayload), nil, buyer, "", true, gas, "")

	// Owner accepts offer
	ApproveNftForMarket(t, ct, ownerAddress)
	CallMarket(t, ct, "acceptOffer", []byte(`{"offerId":0}`), nil, ownerAddress, "", true, gas, "")

	// Total = 5000, fee = 2.5% = 125, royalty = 5% = 250, seller = 5000 - 125 - 250 = 4625
	sellerBalance := QueryTokenBalance(t, ct, ownerAddress)
	assert.Equal(t, uint64(4625), sellerBalance)
}

func TestRoyaltyLockedAtListingCreation(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	// Set 5% royalty
	setPayload := fmt.Sprintf(`{"nftContract":"%s","royaltyBps":500,"royaltyRecipient":"hive:creator"}`, NftContractID)
	CallMarket(t, ct, "setRoyalty", []byte(setPayload), nil, ownerAddress, "", true, gas, "")

	MintNft(t, ct, ownerAddress, "1", 1, 1)
	ApproveNftForMarket(t, ct, ownerAddress)

	listPayload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":1,"paymentToken":"%s","pricePerUnit":10000}`, NftContractID, TokenID)
	CallMarket(t, ct, "list", []byte(listPayload), nil, ownerAddress, "", true, gas, "")

	// Change royalty to 20% after listing
	setPayload2 := fmt.Sprintf(`{"nftContract":"%s","royaltyBps":2000,"royaltyRecipient":"hive:creator"}`, NftContractID)
	CallMarket(t, ct, "setRoyalty", []byte(setPayload2), nil, ownerAddress, "", true, gas, "")

	buyer := "hive:buyer"
	MintAndApproveToken(t, ct, buyer, 10000)
	CallMarket(t, ct, "buy", []byte(`{"listingId":0,"amount":1}`), nil, buyer, "", true, gas, "")

	// Should use locked 5% royalty, not 20%
	// Total = 10000, fee = 250, royalty = 500, seller = 9250
	sellerBalance := QueryTokenBalance(t, ct, ownerAddress)
	assert.Equal(t, uint64(9250), sellerBalance)

	creatorBalance := QueryTokenBalance(t, ct, "hive:creator")
	assert.Equal(t, uint64(500), creatorBalance)
}

func TestRoyaltySetEvent(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	payload := fmt.Sprintf(`{"nftContract":"%s","royaltyBps":500,"royaltyRecipient":"hive:creator"}`, NftContractID)
	_, _, logs := CallMarket(t, ct, "setRoyalty", []byte(payload), nil, ownerAddress, "", true, gas, "")
	AssertEventEmitted(t, logs, "royalty_set")
}
