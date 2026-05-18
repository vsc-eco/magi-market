package contract_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

// ===================================
// Listing Expiration Tests
// ===================================

func TestListWithExpiration(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	MintNft(t, ct, ownerAddress, "1", 5, 100)
	ApproveNftForMarket(t, ct, ownerAddress)

	ct.BlockHeight = 100

	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","pricePerUnit":"1000","expirationBlock":200}`, NftContractID, TokenID)
	CallMarket(t, ct, "list", []byte(payload), nil, ownerAddress, "", true, gas, "")

	// Verify listing has expiration
	result, _, _ := CallMarket(t, ct, "getListing", []byte(`{"listingId":0}`), nil, "hive:anyone", "", true, gas, "")
	listing := ParseListing(result)
	assert.Equal(t, uint64(200), listing.ExpirationBlock)
	assert.True(t, listing.Active)
}

func TestBuyExpiredListing(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	MintNft(t, ct, ownerAddress, "1", 5, 100)
	ApproveNftForMarket(t, ct, ownerAddress)

	ct.BlockHeight = 100

	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","pricePerUnit":"1000","expirationBlock":150}`, NftContractID, TokenID)
	CallMarket(t, ct, "list", []byte(payload), nil, ownerAddress, "", true, gas, "")

	buyer := "hive:buyer"
	MintAndApproveToken(t, ct, buyer, 50000)

	// Advance past expiration
	ct.BlockHeight = 200

	CallMarket(t, ct, "buy", []byte(`{"listingId":0,"amount":1}`), nil, buyer, "", false, gas, "Listing has expired")
}

func TestBuyBeforeExpiration(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	MintNft(t, ct, ownerAddress, "1", 5, 100)
	ApproveNftForMarket(t, ct, ownerAddress)

	ct.BlockHeight = 100
	buyer := "hive:buyer"
	MintAndApproveToken(t, ct, buyer, 50000)

	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","pricePerUnit":"1000","expirationBlock":200}`, NftContractID, TokenID)
	CallMarket(t, ct, "list", []byte(payload), nil, ownerAddress, "", true, gas, "")

	// Buy before expiration
	ct.BlockHeight = 150
	CallMarket(t, ct, "buy", []byte(`{"listingId":0,"amount":1}`), nil, buyer, "", true, gas, "")
}

func TestListNoExpiration(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	MintNft(t, ct, ownerAddress, "1", 5, 100)
	ApproveNftForMarket(t, ct, ownerAddress)

	// ExpirationBlock=0 means no expiration
	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","pricePerUnit":"1000"}`, NftContractID, TokenID)
	CallMarket(t, ct, "list", []byte(payload), nil, ownerAddress, "", true, gas, "")

	buyer := "hive:buyer"
	MintAndApproveToken(t, ct, buyer, 50000)

	// Should work at any block height
	ct.BlockHeight = 999999
	CallMarket(t, ct, "buy", []byte(`{"listingId":0,"amount":1}`), nil, buyer, "", true, gas, "")
}

func TestDelistExpiredListing(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	MintNft(t, ct, ownerAddress, "1", 5, 100)
	ApproveNftForMarket(t, ct, ownerAddress)

	ct.BlockHeight = 100
	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","pricePerUnit":"1000","expirationBlock":150}`, NftContractID, TokenID)
	CallMarket(t, ct, "list", []byte(payload), nil, ownerAddress, "", true, gas, "")

	// Advance past expiration, seller can still delist
	ct.BlockHeight = 200
	CallMarket(t, ct, "delist", []byte(`{"listingId":0}`), nil, ownerAddress, "", true, gas, "")
}

// ===================================
// Offer Expiration Tests
// ===================================

func TestOfferWithExpiration(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	ct.BlockHeight = 100
	buyer := "hive:buyer"
	MintAndApproveToken(t, ct, buyer, 50000)

	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","pricePerUnit":"1000","expirationBlock":200}`, NftContractID, TokenID)
	CallMarket(t, ct, "makeOffer", []byte(payload), nil, buyer, "", true, gas, "")

	result, _, _ := CallMarket(t, ct, "getOffer", []byte(`{"offerId":0}`), nil, "hive:anyone", "", true, gas, "")
	offer := ParseOffer(result)
	assert.Equal(t, uint64(200), offer.ExpirationBlock)
}

func TestAcceptExpiredOffer(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	MintNft(t, ct, ownerAddress, "1", 5, 100)
	ApproveNftForMarket(t, ct, ownerAddress)

	ct.BlockHeight = 100
	buyer := "hive:buyer"
	MintAndApproveToken(t, ct, buyer, 50000)

	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","pricePerUnit":"1000","expirationBlock":150}`, NftContractID, TokenID)
	CallMarket(t, ct, "makeOffer", []byte(payload), nil, buyer, "", true, gas, "")

	// Advance past expiration
	ct.BlockHeight = 200
	CallMarket(t, ct, "acceptOffer", []byte(`{"offerId":0}`), nil, ownerAddress, "", false, gas, "Offer has expired")
}

func TestCancelExpiredOfferByAnyone(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	ct.BlockHeight = 100
	buyer := "hive:buyer"
	MintAndApproveToken(t, ct, buyer, 50000)

	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","pricePerUnit":"1000","expirationBlock":150}`, NftContractID, TokenID)
	CallMarket(t, ct, "makeOffer", []byte(payload), nil, buyer, "", true, gas, "")

	// Anyone can cancel an expired offer
	ct.BlockHeight = 200
	CallMarket(t, ct, "cancelOffer", []byte(`{"offerId":0}`), nil, "hive:random", "", true, gas, "")

	// Verify buyer got refund
	buyerBalance := QueryTokenBalance(t, ct, buyer)
	assert.Equal(t, uint64(50000), buyerBalance)
}

func TestCancelNonExpiredOfferByNonBuyer(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	ct.BlockHeight = 100
	buyer := "hive:buyer"
	MintAndApproveToken(t, ct, buyer, 50000)

	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","pricePerUnit":"1000","expirationBlock":200}`, NftContractID, TokenID)
	CallMarket(t, ct, "makeOffer", []byte(payload), nil, buyer, "", true, gas, "")

	// Non-buyer cannot cancel non-expired offer
	ct.BlockHeight = 150
	CallMarket(t, ct, "cancelOffer", []byte(`{"offerId":0}`), nil, "hive:random", "", false, gas, "Only buyer can cancel offer")
}
