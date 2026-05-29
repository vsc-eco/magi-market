package contract_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"vsc-node/lib/test_utils"

	"github.com/stretchr/testify/assert"
)

// ===================================
// Partial Offer Acceptance Tests
// ===================================

func TestAcceptOfferPartial(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	MintNft(t, ct, ownerAddress, "1", 10, 100)

	buyer := "hive:buyer"
	MintAndApproveToken(t, ct, buyer, 50000)

	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":10,"paymentToken":"%s","pricePerUnit":"1000"}`, NftContractID, TokenID)
	CallMarket(t, ct, "makeOffer", []byte(payload), nil, buyer, "", true, gas, "")

	ApproveNftForMarket(t, ct, ownerAddress)
	// Accept only 3 of 10
	CallMarket(t, ct, "acceptOffer", []byte(`{"offerId":0,"amount":3}`), nil, ownerAddress, "", true, gas, "")

	// Offer should still be active with 7 remaining
	result, _, _ := CallMarket(t, ct, "getOffer", []byte(`{"offerId":0}`), nil, "hive:anyone", "", true, gas, "")
	offer := ParseOffer(result)
	assert.True(t, offer.Active)
	assert.Equal(t, uint64(7), offer.Amount)

	// Buyer should have 3 NFTs
	buyerNft := QueryNftBalance(t, ct, buyer, "1")
	assert.Equal(t, uint64(3), buyerNft)
}

func TestAcceptOfferPartialThenFull(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	MintNft(t, ct, ownerAddress, "1", 10, 100)

	buyer := "hive:buyer"
	MintAndApproveToken(t, ct, buyer, 50000)

	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":10,"paymentToken":"%s","pricePerUnit":"1000"}`, NftContractID, TokenID)
	CallMarket(t, ct, "makeOffer", []byte(payload), nil, buyer, "", true, gas, "")

	ApproveNftForMarket(t, ct, ownerAddress)
	// Accept 4
	CallMarket(t, ct, "acceptOffer", []byte(`{"offerId":0,"amount":4}`), nil, ownerAddress, "", true, gas, "")
	// Accept remaining 6 (amount=0 accepts all)
	CallMarket(t, ct, "acceptOffer", []byte(`{"offerId":0}`), nil, ownerAddress, "", true, gas, "")

	// Offer should be inactive
	result, _, _ := CallMarket(t, ct, "getOffer", []byte(`{"offerId":0}`), nil, "hive:anyone", "", true, gas, "")
	offer := ParseOffer(result)
	assert.False(t, offer.Active)
}

func TestAcceptOfferExceedsAmount(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	MintNft(t, ct, ownerAddress, "1", 10, 100)

	buyer := "hive:buyer"
	MintAndApproveToken(t, ct, buyer, 50000)

	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","pricePerUnit":"1000"}`, NftContractID, TokenID)
	CallMarket(t, ct, "makeOffer", []byte(payload), nil, buyer, "", true, gas, "")

	ApproveNftForMarket(t, ct, ownerAddress)
	CallMarket(t, ct, "acceptOffer", []byte(`{"offerId":0,"amount":10}`), nil, ownerAddress, "", false, gas, "Accept amount exceeds offer amount")
}

func TestAcceptOfferInsufficientNftBalance(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	// Seller has 0 NFTs

	buyer := "hive:buyer"
	MintAndApproveToken(t, ct, buyer, 50000)

	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","pricePerUnit":"1000"}`, NftContractID, TokenID)
	CallMarket(t, ct, "makeOffer", []byte(payload), nil, buyer, "", true, gas, "")

	// Approval-custody model: seller must approve the market as operator first;
	// the contract gates on approval before the NFT-balance check, so without
	// this the abort would be "not approved" instead of "Insufficient NFT balance".
	ApproveNftForMarket(t, ct, ownerAddress)
	CallMarket(t, ct, "acceptOffer", []byte(`{"offerId":0}`), nil, ownerAddress, "", false, gas, "Insufficient NFT balance")
}

// ===================================
// Fee Locking Tests
// ===================================

func TestFeeLockedAtListingCreation(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	MintNft(t, ct, ownerAddress, "1", 1, 1)
	ApproveNftForMarket(t, ct, ownerAddress)

	listPayload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":1,"paymentToken":"%s","pricePerUnit":"10000"}`, NftContractID, TokenID)
	CallMarket(t, ct, "list", []byte(listPayload), nil, ownerAddress, "", true, gas, "")

	// Verify locked fee bps
	result, _, _ := CallMarket(t, ct, "getListing", []byte(`{"listingId":0}`), nil, "hive:anyone", "", true, gas, "")
	listing := ParseListing(result)
	assert.Equal(t, uint64(250), listing.FeeBps) // 2.5% from init

	// Change fee to 10%
	CallMarket(t, ct, "setFee", []byte(`{"feeBps":1000}`), nil, ownerAddress, "", true, gas, "")

	buyer := "hive:buyer"
	MintAndApproveToken(t, ct, buyer, 10000)
	CallMarket(t, ct, "buy", []byte(`{"listingId":0,"amount":1}`), nil, buyer, "", true, gas, "")

	// Fee should be 2.5% (250), not 10% (1000)
	feeBalance := QueryTokenBalance(t, ct, feeRecipientAddress)
	assert.Equal(t, uint64(250), feeBalance)

	sellerBalance := QueryTokenBalance(t, ct, ownerAddress)
	assert.Equal(t, uint64(9750), sellerBalance)
}

func TestFeeLockedAtOfferCreation(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	MintNft(t, ct, ownerAddress, "1", 5, 100)

	buyer := "hive:buyer"
	MintAndApproveToken(t, ct, buyer, 50000)

	offerPayload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","pricePerUnit":"1000"}`, NftContractID, TokenID)
	CallMarket(t, ct, "makeOffer", []byte(offerPayload), nil, buyer, "", true, gas, "")

	// Change fee to 10%
	CallMarket(t, ct, "setFee", []byte(`{"feeBps":1000}`), nil, ownerAddress, "", true, gas, "")

	// Accept uses locked 2.5% fee
	ApproveNftForMarket(t, ct, ownerAddress)
	CallMarket(t, ct, "acceptOffer", []byte(`{"offerId":0}`), nil, ownerAddress, "", true, gas, "")

	// Fee should be 2.5% of 5000 = 125
	feeBalance := QueryTokenBalance(t, ct, feeRecipientAddress)
	assert.Equal(t, uint64(125), feeBalance)
}

// ===================================
// Minimum Offer Threshold Tests
// ===================================

func TestSetMinOffer(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	CallMarket(t, ct, "setMinOffer", []byte(`{"minOffer":"100"}`), nil, ownerAddress, "", true, gas, "")

	result, _, _ := CallMarket(t, ct, "getMinOffer", nil, nil, "hive:anyone", "", true, gas, "")
	var resp struct{ MinOffer string `json:"minOffer"` }
	parseJSON(result, &resp)
	assert.Equal(t, "100", resp.MinOffer)
}

func TestSetMinOfferNonOwner(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	CallMarket(t, ct, "setMinOffer", []byte(`{"minOffer":"100"}`), nil, "hive:random", "", false, gas, "Only owner can set minimum offer")
}

func TestOfferBelowMinimum(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	CallMarket(t, ct, "setMinOffer", []byte(`{"minOffer":"500"}`), nil, ownerAddress, "", true, gas, "")

	buyer := "hive:buyer"
	MintAndApproveToken(t, ct, buyer, 50000)

	// Total = 1 * 100 = 100 < 500
	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":1,"paymentToken":"%s","pricePerUnit":"100"}`, NftContractID, TokenID)
	CallMarket(t, ct, "makeOffer", []byte(payload), nil, buyer, "", false, gas, "Offer below minimum threshold")
}

func TestOfferAboveMinimum(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	CallMarket(t, ct, "setMinOffer", []byte(`{"minOffer":"500"}`), nil, ownerAddress, "", true, gas, "")

	buyer := "hive:buyer"
	MintAndApproveToken(t, ct, buyer, 50000)

	// Total = 5 * 200 = 1000 >= 500
	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","pricePerUnit":"200"}`, NftContractID, TokenID)
	CallMarket(t, ct, "makeOffer", []byte(payload), nil, buyer, "", true, gas, "")
}

func TestMinOfferInGetInfo(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	CallMarket(t, ct, "setMinOffer", []byte(`{"minOffer":"999"}`), nil, ownerAddress, "", true, gas, "")

	result, _, _ := CallMarket(t, ct, "getInfo", nil, nil, "hive:anyone", "", true, gas, "")
	info := ParseInfo(result)
	assert.Equal(t, "999", info.MinOffer)
}

// ===================================
// Payment Token Whitelist Tests
// ===================================

func TestAddPaymentToken(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	CallMarket(t, ct, "addPaymentToken", []byte(fmt.Sprintf(`{"token":"%s"}`, TokenID)), nil, ownerAddress, "", true, gas, "")

	result, _, _ := CallMarket(t, ct, "isPaymentTokenAllowed", []byte(fmt.Sprintf(`{"token":"%s"}`, TokenID)), nil, "hive:anyone", "", true, gas, "")
	var resp struct{ Allowed bool `json:"allowed"` }
	parseJSON(result, &resp)
	assert.True(t, resp.Allowed)
}

func TestRemovePaymentToken(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	CallMarket(t, ct, "addPaymentToken", []byte(fmt.Sprintf(`{"token":"%s"}`, TokenID)), nil, ownerAddress, "", true, gas, "")
	CallMarket(t, ct, "removePaymentToken", []byte(fmt.Sprintf(`{"token":"%s"}`, TokenID)), nil, ownerAddress, "", true, gas, "")

	result, _, _ := CallMarket(t, ct, "isPaymentTokenAllowed", []byte(fmt.Sprintf(`{"token":"%s"}`, TokenID)), nil, "hive:anyone", "", true, gas, "")
	var resp struct{ Allowed bool `json:"allowed"` }
	parseJSON(result, &resp)
	assert.False(t, resp.Allowed)
}

func TestListWithUnwhitelistedToken(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	// InitFullSetup → InitMarket auto-seeds hive/hbd + the standard test
	// tokens (TokenID, FeeTokenID, etc) per the 2026-05-27 audit hardening.
	// To prove the gate fires, pass an id we KNOW isn't seeded.
	MintNft(t, ct, ownerAddress, "1", 5, 100)
	ApproveNftForMarket(t, ct, ownerAddress)

	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"contract:never_whitelisted","pricePerUnit":"1000"}`, NftContractID)
	CallMarket(t, ct, "list", []byte(payload), nil, ownerAddress, "", false, gas, "Payment token not allowed")
}

func TestListWithWhitelistedToken(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	CallMarket(t, ct, "addPaymentToken", []byte(fmt.Sprintf(`{"token":"%s"}`, TokenID)), nil, ownerAddress, "", true, gas, "")

	MintNft(t, ct, ownerAddress, "1", 5, 100)
	ApproveNftForMarket(t, ct, ownerAddress)

	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","pricePerUnit":"1000"}`, NftContractID, TokenID)
	CallMarket(t, ct, "list", []byte(payload), nil, ownerAddress, "", true, gas, "")
}

// Post-audit (2026-05-27) Init seeds the whitelist with native HBD/HIVE
// AND the test helper seeds the mock tokens; an unseeded random id is
// now ALWAYS rejected. The old "whitelist defaults off → accepts any"
// behaviour was the security flaw the audit closed.
func TestWhitelistEnabledByDefault(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	// Native HIVE/HBD seeded at init → allowed.
	for _, tok := range []string{"hive", "hbd"} {
		result, _, _ := CallMarket(t, ct, "isPaymentTokenAllowed", []byte(fmt.Sprintf(`{"token":"%s"}`, tok)), nil, "hive:anyone", "", true, gas, "")
		var resp struct{ Allowed bool `json:"allowed"` }
		parseJSON(result, &resp)
		assert.True(t, resp.Allowed, "%s should be whitelisted after init", tok)
	}
	// An arbitrary unseeded id is NOT allowed.
	result, _, _ := CallMarket(t, ct, "isPaymentTokenAllowed", []byte(`{"token":"contract:random_unseeded"}`), nil, "hive:anyone", "", true, gas, "")
	var resp struct{ Allowed bool `json:"allowed"` }
	parseJSON(result, &resp)
	assert.False(t, resp.Allowed, "unseeded token must be rejected")
}

func TestWhitelistNonOwner(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	CallMarket(t, ct, "addPaymentToken", []byte(`{"token":"x"}`), nil, "hive:random", "", false, gas, "Only owner can manage payment tokens")
}

// ===================================
// Emergency Withdraw Tests
// ===================================

func TestEmergencyWithdrawNft(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	MintNft(t, ct, ownerAddress, "1", 5, 100)
	ApproveNftForMarket(t, ct, ownerAddress)

	// Under the approval-custody model listings never escrow the NFT, but
	// auctions still do. Create an auction so the marketplace genuinely holds
	// the NFT, giving emergencyWithdraw something real to rescue.
	auctionPayload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","auctionType":"english","startPrice":"1000","endPrice":"0","startBlock":100,"endBlock":200}`, NftContractID, TokenID)
	CallMarket(t, ct, "createAuction", []byte(auctionPayload), nil, ownerAddress, "", true, gas, "")
	assert.Equal(t, uint64(5), QueryNftBalance(t, ct, MarketContractAddress, "1"))

	// Pause
	CallMarket(t, ct, "pause", nil, nil, ownerAddress, "", true, gas, "")

	// Emergency withdraw
	withdrawPayload := fmt.Sprintf(`{"tokenType":"nft","contract":"%s","tokenId":"1","amount":"5","to":"hive:rescue"}`, NftContractID)
	_, _, logs := CallMarket(t, ct, "emergencyWithdraw", []byte(withdrawPayload), nil, ownerAddress, "", true, gas, "")
	AssertEventEmitted(t, logs, "emergency_withdraw")

	// NFT sent to rescue address
	rescueNft := QueryNftBalance(t, ct, "hive:rescue", "1")
	assert.Equal(t, uint64(5), rescueNft)
}

func TestEmergencyWithdrawToken(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	buyer := "hive:buyer"
	MintAndApproveToken(t, ct, buyer, 50000)

	// Make offer (escrow tokens into marketplace)
	offerPayload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","pricePerUnit":"1000"}`, NftContractID, TokenID)
	CallMarket(t, ct, "makeOffer", []byte(offerPayload), nil, buyer, "", true, gas, "")

	// Pause
	CallMarket(t, ct, "pause", nil, nil, ownerAddress, "", true, gas, "")

	// Emergency withdraw tokens
	withdrawPayload := fmt.Sprintf(`{"tokenType":"token","contract":"%s","tokenId":"","amount":"5000","to":"%s"}`, TokenID, buyer)
	CallMarket(t, ct, "emergencyWithdraw", []byte(withdrawPayload), nil, ownerAddress, "", true, gas, "")

	buyerBalance := QueryTokenBalance(t, ct, buyer)
	assert.Equal(t, uint64(50000), buyerBalance)
}

func TestEmergencyWithdrawNotPaused(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	withdrawPayload := fmt.Sprintf(`{"tokenType":"token","contract":"%s","tokenId":"","amount":"100","to":"hive:rescue"}`, TokenID)
	CallMarket(t, ct, "emergencyWithdraw", []byte(withdrawPayload), nil, ownerAddress, "", false, gas, "Contract must be paused")
}

func TestEmergencyWithdrawNotOwner(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	CallMarket(t, ct, "pause", nil, nil, ownerAddress, "", true, gas, "")

	withdrawPayload := fmt.Sprintf(`{"tokenType":"token","contract":"%s","tokenId":"","amount":"100","to":"hive:rescue"}`, TokenID)
	CallMarket(t, ct, "emergencyWithdraw", []byte(withdrawPayload), nil, "hive:random", "", false, gas, "Only owner can emergency withdraw")
}

func TestEmergencyWithdrawInvalidType(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	CallMarket(t, ct, "pause", nil, nil, ownerAddress, "", true, gas, "")

	withdrawPayload := fmt.Sprintf(`{"tokenType":"invalid","contract":"%s","tokenId":"","amount":"100","to":"hive:rescue"}`, TokenID)
	CallMarket(t, ct, "emergencyWithdraw", []byte(withdrawPayload), nil, ownerAddress, "", false, gas, "Token type must be")
}

// ===================================
// Batch Operations Tests
// ===================================

func TestBatchList(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	MintNft(t, ct, ownerAddress, "1", 5, 100)
	MintNft(t, ct, ownerAddress, "2", 3, 100)
	ApproveNftForMarket(t, ct, ownerAddress)

	items := fmt.Sprintf(`{"items":[{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","pricePerUnit":"1000"},{"nftContract":"%s","tokenId":"2","amount":3,"paymentToken":"%s","pricePerUnit":"2000"}]}`, NftContractID, TokenID, NftContractID, TokenID)
	CallMarket(t, ct, "batchList", []byte(items), nil, ownerAddress, "", true, gas, "")

	// Verify both listings
	result1, _, _ := CallMarket(t, ct, "getListing", []byte(`{"listingId":0}`), nil, "hive:anyone", "", true, gas, "")
	listing1 := ParseListing(result1)
	assert.Equal(t, "1", listing1.TokenId)
	assert.Equal(t, uint64(5), listing1.Amount)

	result2, _, _ := CallMarket(t, ct, "getListing", []byte(`{"listingId":1}`), nil, "hive:anyone", "", true, gas, "")
	listing2 := ParseListing(result2)
	assert.Equal(t, "2", listing2.TokenId)
	assert.Equal(t, uint64(3), listing2.Amount)
}

func TestBatchBuy(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	MintNft(t, ct, ownerAddress, "1", 5, 100)
	MintNft(t, ct, ownerAddress, "2", 3, 100)
	ApproveNftForMarket(t, ct, ownerAddress)

	list1 := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","pricePerUnit":"1000"}`, NftContractID, TokenID)
	CallMarket(t, ct, "list", []byte(list1), nil, ownerAddress, "", true, gas, "")
	list2 := fmt.Sprintf(`{"nftContract":"%s","tokenId":"2","amount":3,"paymentToken":"%s","pricePerUnit":"2000"}`, NftContractID, TokenID)
	CallMarket(t, ct, "list", []byte(list2), nil, ownerAddress, "", true, gas, "")

	buyer := "hive:buyer"
	MintAndApproveToken(t, ct, buyer, 50000)

	batchPayload := `{"items":[{"listingId":0,"amount":2},{"listingId":1,"amount":1}]}`
	CallMarket(t, ct, "batchBuy", []byte(batchPayload), nil, buyer, "", true, gas, "")

	// Verify buyer owns NFTs
	buyerNft1 := QueryNftBalance(t, ct, buyer, "1")
	assert.Equal(t, uint64(2), buyerNft1)
	buyerNft2 := QueryNftBalance(t, ct, buyer, "2")
	assert.Equal(t, uint64(1), buyerNft2)
}

func TestBatchListEmpty(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	CallMarket(t, ct, "batchList", []byte(`{"items":[]}`), nil, ownerAddress, "", false, gas, "At least one item required")
}

func TestBatchBuyEmpty(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	CallMarket(t, ct, "batchBuy", []byte(`{"items":[]}`), nil, "hive:buyer", "", false, gas, "At least one item required")
}

// ===================================
// JSON helper
// ===================================

func parseJSON(result test_utils.ContractTestCallResult, v interface{}) {
	json.Unmarshal([]byte(result.Ret), v)
}
