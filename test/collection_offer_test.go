package contract_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

// ===================================
// Collection Offer Tests
// ===================================

func TestMakeCollectionOffer(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	buyer := "hive:buyer"
	MintAndApproveToken(t, ct, buyer, 50000)

	// Empty tokenId = collection offer
	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"","amount":5,"paymentToken":"%s","pricePerUnit":"1000"}`, NftContractID, TokenID)
	CallMarket(t, ct, "makeOffer", []byte(payload), nil, buyer, "", true, gas, "")

	result, _, _ := CallMarket(t, ct, "getOffer", []byte(`{"offerId":0}`), nil, "hive:anyone", "", true, gas, "")
	offer := ParseOffer(result)
	assert.True(t, offer.IsCollection)
	assert.Equal(t, "", offer.TokenId)
}

func TestAcceptCollectionOffer(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	buyer := "hive:buyer"
	MintAndApproveToken(t, ct, buyer, 50000)
	MintNft(t, ct, ownerAddress, "42", 5, 100)

	// Make collection offer
	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"","amount":5,"paymentToken":"%s","pricePerUnit":"1000"}`, NftContractID, TokenID)
	CallMarket(t, ct, "makeOffer", []byte(payload), nil, buyer, "", true, gas, "")

	// Seller accepts with specific tokenId
	ApproveNftForMarket(t, ct, ownerAddress)
	acceptPayload := `{"offerId":0,"tokenId":"42"}`
	CallMarket(t, ct, "acceptCollectionOffer", []byte(acceptPayload), nil, ownerAddress, "", true, gas, "")

	// Verify buyer got NFT
	buyerNftBalance := QueryNftBalance(t, ct, buyer, "42")
	assert.Equal(t, uint64(5), buyerNftBalance)
}

func TestAcceptCollectionOfferPartial(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	buyer := "hive:buyer"
	MintAndApproveToken(t, ct, buyer, 50000)
	MintNft(t, ct, ownerAddress, "42", 10, 100)

	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"","amount":5,"paymentToken":"%s","pricePerUnit":"1000"}`, NftContractID, TokenID)
	CallMarket(t, ct, "makeOffer", []byte(payload), nil, buyer, "", true, gas, "")

	ApproveNftForMarket(t, ct, ownerAddress)
	acceptPayload := `{"offerId":0,"tokenId":"42","amount":3}`
	CallMarket(t, ct, "acceptCollectionOffer", []byte(acceptPayload), nil, ownerAddress, "", true, gas, "")

	// Verify offer is still active with reduced amount
	result, _, _ := CallMarket(t, ct, "getOffer", []byte(`{"offerId":0}`), nil, "hive:anyone", "", true, gas, "")
	offer := ParseOffer(result)
	assert.True(t, offer.Active)
	assert.Equal(t, uint64(2), offer.Amount)
}

func TestAcceptCollectionOfferNoTokenId(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	buyer := "hive:buyer"
	MintAndApproveToken(t, ct, buyer, 50000)

	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"","amount":5,"paymentToken":"%s","pricePerUnit":"1000"}`, NftContractID, TokenID)
	CallMarket(t, ct, "makeOffer", []byte(payload), nil, buyer, "", true, gas, "")

	CallMarket(t, ct, "acceptCollectionOffer", []byte(`{"offerId":0,"tokenId":""}`), nil, ownerAddress, "", false, gas, "Token ID required")
}

func TestAcceptTokenOfferAsCollectionOffer(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	buyer := "hive:buyer"
	MintAndApproveToken(t, ct, buyer, 50000)

	// Token-specific offer (not collection)
	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","pricePerUnit":"1000"}`, NftContractID, TokenID)
	CallMarket(t, ct, "makeOffer", []byte(payload), nil, buyer, "", true, gas, "")

	// Trying to accept via acceptCollectionOffer should fail
	CallMarket(t, ct, "acceptCollectionOffer", []byte(`{"offerId":0,"tokenId":"1"}`), nil, ownerAddress, "", false, gas, "Not a collection offer")
}

func TestAcceptCollectionOfferViaAcceptOffer(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	buyer := "hive:buyer"
	MintAndApproveToken(t, ct, buyer, 50000)

	// Collection offer
	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"","amount":5,"paymentToken":"%s","pricePerUnit":"1000"}`, NftContractID, TokenID)
	CallMarket(t, ct, "makeOffer", []byte(payload), nil, buyer, "", true, gas, "")

	// Trying to accept via acceptOffer should fail
	CallMarket(t, ct, "acceptOffer", []byte(`{"offerId":0}`), nil, ownerAddress, "", false, gas, "Use acceptCollectionOffer")
}

func TestCollectionOfferEvent(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	buyer := "hive:buyer"
	MintAndApproveToken(t, ct, buyer, 50000)

	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"","amount":5,"paymentToken":"%s","pricePerUnit":"1000"}`, NftContractID, TokenID)
	_, _, logs := CallMarket(t, ct, "makeOffer", []byte(payload), nil, buyer, "", true, gas, "")
	AssertEventContains(t, logs, "offer_made", `"isCollection":true`)
}
