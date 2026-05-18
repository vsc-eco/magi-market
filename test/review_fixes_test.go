package contract_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

// ===================================
// Self-buy prevention
// ===================================

func TestSellerCannotBuyOwnListing(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	MintNft(t, ct, ownerAddress, "1", 5, 100)
	ApproveNftForMarket(t, ct, ownerAddress)
	MintAndApproveToken(t, ct, ownerAddress, 50000)

	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","pricePerUnit":"1000"}`, NftContractID, TokenID)
	CallMarket(t, ct, "list", []byte(payload), nil, ownerAddress, "", true, gas, "")

	CallMarket(t, ct, "buy", []byte(`{"listingId":0,"amount":1}`), nil, ownerAddress, "", false, gas, "Seller cannot buy own listing")
}

// ===================================
// Expiration boundary conditions
// ===================================

func TestBuyAtExactExpirationBlock(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	MintNft(t, ct, ownerAddress, "1", 5, 100)
	ApproveNftForMarket(t, ct, ownerAddress)

	ct.BlockHeight = 100
	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","pricePerUnit":"1000","expirationBlock":150}`, NftContractID, TokenID)
	CallMarket(t, ct, "list", []byte(payload), nil, ownerAddress, "", true, gas, "")

	buyer := "hive:buyer"
	MintAndApproveToken(t, ct, buyer, 50000)

	// At exact expirationBlock, item is NOT expired (> not >=)
	ct.BlockHeight = 150
	CallMarket(t, ct, "buy", []byte(`{"listingId":0,"amount":1}`), nil, buyer, "", true, gas, "")
}

func TestBuyOneBlockAfterExpiration(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	MintNft(t, ct, ownerAddress, "1", 5, 100)
	ApproveNftForMarket(t, ct, ownerAddress)

	ct.BlockHeight = 100
	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","pricePerUnit":"1000","expirationBlock":150}`, NftContractID, TokenID)
	CallMarket(t, ct, "list", []byte(payload), nil, ownerAddress, "", true, gas, "")

	buyer := "hive:buyer"
	MintAndApproveToken(t, ct, buyer, 50000)

	// One block after expiration
	ct.BlockHeight = 151
	CallMarket(t, ct, "buy", []byte(`{"listingId":0,"amount":1}`), nil, buyer, "", false, gas, "Listing has expired")
}

func TestAcceptOfferAtExactExpiration(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	MintNft(t, ct, ownerAddress, "1", 5, 100)
	ApproveNftForMarket(t, ct, ownerAddress)

	ct.BlockHeight = 100
	buyer := "hive:buyer"
	MintAndApproveToken(t, ct, buyer, 50000)

	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","pricePerUnit":"1000","expirationBlock":150}`, NftContractID, TokenID)
	CallMarket(t, ct, "makeOffer", []byte(payload), nil, buyer, "", true, gas, "")

	// At exact expirationBlock, NOT expired
	ct.BlockHeight = 150
	CallMarket(t, ct, "acceptOffer", []byte(`{"offerId":0}`), nil, ownerAddress, "", true, gas, "")
}

// ===================================
// Auction boundary conditions
// ===================================

func TestEnglishAuctionBidAtExactEndBlock(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	MintNft(t, ct, ownerAddress, "1", 1, 1)
	ApproveNftForMarket(t, ct, ownerAddress)

	ct.BlockHeight = 100
	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":1,"paymentToken":"%s","auctionType":"english","startPrice":"1000","endPrice":"0","startBlock":100,"endBlock":200}`, NftContractID, TokenID)
	CallMarket(t, ct, "createAuction", []byte(payload), nil, ownerAddress, "", true, gas, "")

	bidder := "hive:bidder"
	MintAndApproveToken(t, ct, bidder, 5000)

	// Bid AT exact endBlock should succeed (> not >=)
	ct.BlockHeight = 200
	CallMarket(t, ct, "placeBid", []byte(`{"auctionId":0,"bidAmount":"1000"}`), nil, bidder, "", true, gas, "")
}

func TestEnglishAuctionSettleAtExactEndBlock(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	MintNft(t, ct, ownerAddress, "1", 1, 1)
	ApproveNftForMarket(t, ct, ownerAddress)

	ct.BlockHeight = 100
	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":1,"paymentToken":"%s","auctionType":"english","startPrice":"1000","endPrice":"0","startBlock":100,"endBlock":200}`, NftContractID, TokenID)
	CallMarket(t, ct, "createAuction", []byte(payload), nil, ownerAddress, "", true, gas, "")

	// Settlement AT endBlock should fail (<= check)
	ct.BlockHeight = 200
	CallMarket(t, ct, "settleAuction", []byte(`{"auctionId":0}`), nil, "hive:anyone", "", false, gas, "Auction has not ended yet")
}

func TestEnglishAuctionBidAtReserveExact(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	MintNft(t, ct, ownerAddress, "1", 1, 1)
	ApproveNftForMarket(t, ct, ownerAddress)

	ct.BlockHeight = 100
	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":1,"paymentToken":"%s","auctionType":"english","startPrice":"1000","endPrice":"0","startBlock":100,"endBlock":200}`, NftContractID, TokenID)
	CallMarket(t, ct, "createAuction", []byte(payload), nil, ownerAddress, "", true, gas, "")

	bidder := "hive:bidder"
	MintAndApproveToken(t, ct, bidder, 5000)

	ct.BlockHeight = 120
	// Bid exactly at reserve should succeed (>= check)
	CallMarket(t, ct, "placeBid", []byte(`{"auctionId":0,"bidAmount":"1000"}`), nil, bidder, "", true, gas, "")
}

func TestEnglishAuctionBidBeforeStart(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	MintNft(t, ct, ownerAddress, "1", 1, 1)
	ApproveNftForMarket(t, ct, ownerAddress)

	ct.BlockHeight = 50
	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":1,"paymentToken":"%s","auctionType":"english","startPrice":"1000","endPrice":"0","startBlock":100,"endBlock":200}`, NftContractID, TokenID)
	CallMarket(t, ct, "createAuction", []byte(payload), nil, ownerAddress, "", true, gas, "")

	bidder := "hive:bidder"
	MintAndApproveToken(t, ct, bidder, 5000)

	ct.BlockHeight = 80
	CallMarket(t, ct, "placeBid", []byte(`{"auctionId":0,"bidAmount":"1000"}`), nil, bidder, "", false, gas, "Auction has not started yet")
}

func TestDutchAuctionBuyAtEndBlock(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	MintNft(t, ct, ownerAddress, "1", 1, 1)
	ApproveNftForMarket(t, ct, ownerAddress)

	ct.BlockHeight = 100
	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":1,"paymentToken":"%s","auctionType":"dutch","startPrice":"10000","endPrice":"1000","startBlock":100,"endBlock":200}`, NftContractID, TokenID)
	CallMarket(t, ct, "createAuction", []byte(payload), nil, ownerAddress, "", true, gas, "")

	buyer := "hive:buyer"
	MintAndApproveToken(t, ct, buyer, 20000)

	// At endBlock, price should be endPrice (1000)
	ct.BlockHeight = 200
	CallMarket(t, ct, "placeBid", []byte(`{"auctionId":0,"bidAmount":"1000"}`), nil, buyer, "", true, gas, "")

	buyerNft := QueryNftBalance(t, ct, buyer, "1")
	assert.Equal(t, uint64(1), buyerNft)
}

func TestDutchAuctionMultipleAmount(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	MintNft(t, ct, ownerAddress, "1", 5, 100)
	ApproveNftForMarket(t, ct, ownerAddress)

	ct.BlockHeight = 100
	// 5 units, price decays from 10000 to 1000 per unit
	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","auctionType":"dutch","startPrice":"10000","endPrice":"1000","startBlock":100,"endBlock":200}`, NftContractID, TokenID)
	CallMarket(t, ct, "createAuction", []byte(payload), nil, ownerAddress, "", true, gas, "")

	buyer := "hive:buyer"
	MintAndApproveToken(t, ct, buyer, 100000)

	// At midpoint, price per unit = 5500, total = 5 * 5500 = 27500
	ct.BlockHeight = 150
	CallMarket(t, ct, "placeBid", []byte(`{"auctionId":0,"bidAmount":"27500"}`), nil, buyer, "", true, gas, "")

	buyerNft := QueryNftBalance(t, ct, buyer, "1")
	assert.Equal(t, uint64(5), buyerNft)
}

// ===================================
// Collection offer with expiration
// ===================================

func TestCollectionOfferExpiration(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	ct.BlockHeight = 100
	buyer := "hive:buyer"
	MintAndApproveToken(t, ct, buyer, 50000)
	MintNft(t, ct, ownerAddress, "42", 5, 100)

	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"","amount":5,"paymentToken":"%s","pricePerUnit":"1000","expirationBlock":150}`, NftContractID, TokenID)
	CallMarket(t, ct, "makeOffer", []byte(payload), nil, buyer, "", true, gas, "")

	ApproveNftForMarket(t, ct, ownerAddress)

	// After expiration, cannot accept
	ct.BlockHeight = 200
	CallMarket(t, ct, "acceptCollectionOffer", []byte(`{"offerId":0,"tokenId":"42"}`), nil, ownerAddress, "", false, gas, "Offer has expired")
}

// ===================================
// Collection offer with royalty
// ===================================

func TestCollectionOfferWithRoyalty(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	// Set 10% royalty
	royaltyPayload := fmt.Sprintf(`{"nftContract":"%s","royaltyBps":1000,"royaltyRecipient":"hive:creator"}`, NftContractID)
	CallMarket(t, ct, "setRoyalty", []byte(royaltyPayload), nil, ownerAddress, "", true, gas, "")

	buyer := "hive:buyer"
	MintAndApproveToken(t, ct, buyer, 50000)
	MintNft(t, ct, ownerAddress, "42", 5, 100)

	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"","amount":5,"paymentToken":"%s","pricePerUnit":"1000"}`, NftContractID, TokenID)
	CallMarket(t, ct, "makeOffer", []byte(payload), nil, buyer, "", true, gas, "")

	ApproveNftForMarket(t, ct, ownerAddress)
	CallMarket(t, ct, "acceptCollectionOffer", []byte(`{"offerId":0,"tokenId":"42"}`), nil, ownerAddress, "", true, gas, "")

	// Total = 5000, fee = 125, royalty = 500, seller = 4375
	sellerBalance := QueryTokenBalance(t, ct, ownerAddress)
	assert.Equal(t, uint64(4375), sellerBalance)
	creatorBalance := QueryTokenBalance(t, ct, "hive:creator")
	assert.Equal(t, uint64(500), creatorBalance)
}

// ===================================
// Payment token whitelist for auctions
// ===================================

func TestAuctionWithUnwhitelistedToken(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	MintNft(t, ct, ownerAddress, "1", 1, 1)
	ApproveNftForMarket(t, ct, ownerAddress)

	// Activate whitelist with a different token
	CallMarket(t, ct, "addPaymentToken", []byte(`{"token":"other_token"}`), nil, ownerAddress, "", true, gas, "")

	ct.BlockHeight = 100
	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":1,"paymentToken":"%s","auctionType":"english","startPrice":"1000","endPrice":"0","startBlock":100,"endBlock":200}`, NftContractID, TokenID)
	CallMarket(t, ct, "createAuction", []byte(payload), nil, ownerAddress, "", false, gas, "Payment token not allowed")
}

// ===================================
// Offer whitelist enforcement
// ===================================

func TestOfferWithUnwhitelistedToken(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	CallMarket(t, ct, "addPaymentToken", []byte(`{"token":"other_token"}`), nil, ownerAddress, "", true, gas, "")

	buyer := "hive:buyer"
	MintAndApproveToken(t, ct, buyer, 50000)

	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","pricePerUnit":"1000"}`, NftContractID, TokenID)
	CallMarket(t, ct, "makeOffer", []byte(payload), nil, buyer, "", false, gas, "Payment token not allowed")
}

// ===================================
// English auction fee/royalty exact verification
// ===================================

func TestEnglishAuctionSettlementFeeBreakdown(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	// Set 10% royalty
	royaltyPayload := fmt.Sprintf(`{"nftContract":"%s","royaltyBps":1000,"royaltyRecipient":"hive:creator"}`, NftContractID)
	CallMarket(t, ct, "setRoyalty", []byte(royaltyPayload), nil, ownerAddress, "", true, gas, "")

	MintNft(t, ct, ownerAddress, "1", 1, 1)
	ApproveNftForMarket(t, ct, ownerAddress)

	ct.BlockHeight = 100
	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":1,"paymentToken":"%s","auctionType":"english","startPrice":"1000","endPrice":"0","startBlock":100,"endBlock":200}`, NftContractID, TokenID)
	CallMarket(t, ct, "createAuction", []byte(payload), nil, ownerAddress, "", true, gas, "")

	bidder := "hive:bidder"
	MintAndApproveToken(t, ct, bidder, 10000)

	ct.BlockHeight = 150
	CallMarket(t, ct, "placeBid", []byte(`{"auctionId":0,"bidAmount":"10000"}`), nil, bidder, "", true, gas, "")

	ct.BlockHeight = 201
	_, _, logs := CallMarket(t, ct, "settleAuction", []byte(`{"auctionId":0}`), nil, "hive:anyone", "", true, gas, "")
	AssertEventEmitted(t, logs, "auction_settled")

	// bid=10000, fee=2.5%=250, royalty=10%=1000, seller=8750
	sellerBalance := QueryTokenBalance(t, ct, ownerAddress)
	assert.Equal(t, uint64(8750), sellerBalance)
	feeBalance := QueryTokenBalance(t, ct, feeRecipientAddress)
	assert.Equal(t, uint64(250), feeBalance)
	creatorBalance := QueryTokenBalance(t, ct, "hive:creator")
	assert.Equal(t, uint64(1000), creatorBalance)
	bidderNft := QueryNftBalance(t, ct, bidder, "1")
	assert.Equal(t, uint64(1), bidderNft)
}

// ===================================
// English auction bidder refund verification
// ===================================

func TestEnglishAuctionBidderRefundOnOutbid(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	MintNft(t, ct, ownerAddress, "1", 1, 1)
	ApproveNftForMarket(t, ct, ownerAddress)

	ct.BlockHeight = 100
	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":1,"paymentToken":"%s","auctionType":"english","startPrice":"1000","endPrice":"0","startBlock":100,"endBlock":200}`, NftContractID, TokenID)
	CallMarket(t, ct, "createAuction", []byte(payload), nil, ownerAddress, "", true, gas, "")

	b1 := "hive:bidder1"
	b2 := "hive:bidder2"
	b3 := "hive:bidder3"
	MintAndApproveToken(t, ct, b1, 5000)
	MintAndApproveToken(t, ct, b2, 5000)
	MintAndApproveToken(t, ct, b3, 5000)

	ct.BlockHeight = 120
	CallMarket(t, ct, "placeBid", []byte(`{"auctionId":0,"bidAmount":"1000"}`), nil, b1, "", true, gas, "")
	assert.Equal(t, uint64(4000), QueryTokenBalance(t, ct, b1))

	CallMarket(t, ct, "placeBid", []byte(`{"auctionId":0,"bidAmount":"2000"}`), nil, b2, "", true, gas, "")
	assert.Equal(t, uint64(5000), QueryTokenBalance(t, ct, b1)) // b1 refunded
	assert.Equal(t, uint64(3000), QueryTokenBalance(t, ct, b2))

	CallMarket(t, ct, "placeBid", []byte(`{"auctionId":0,"bidAmount":"3000"}`), nil, b3, "", true, gas, "")
	assert.Equal(t, uint64(5000), QueryTokenBalance(t, ct, b2)) // b2 refunded
	assert.Equal(t, uint64(2000), QueryTokenBalance(t, ct, b3))
}

// ===================================
// Batch operation partial failure (atomic)
// ===================================

func TestBatchBuyOneInvalidAborts(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	MintNft(t, ct, ownerAddress, "1", 5, 100)
	ApproveNftForMarket(t, ct, ownerAddress)

	listPayload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","pricePerUnit":"1000"}`, NftContractID, TokenID)
	CallMarket(t, ct, "list", []byte(listPayload), nil, ownerAddress, "", true, gas, "")

	buyer := "hive:buyer"
	MintAndApproveToken(t, ct, buyer, 50000)

	// listingId 0 exists, listingId 999 does not — the tx fails on the second item
	batchPayload := `{"items":[{"listingId":0,"amount":1},{"listingId":999,"amount":1}]}`
	CallMarket(t, ct, "batchBuy", []byte(batchPayload), nil, buyer, "", false, gas, "Listing not active")

	// The VSC runtime rolls back ALL state mutations of a failed call: when
	// the second batch item aborts, the first item's buy is reverted too.
	// batchBuy is therefore atomic (all-or-nothing), and the marketplace's
	// escrow/approval flows depend on this guarantee.
	listing, _, _ := CallMarket(t, ct, "getListing", []byte(`{"listingId":0}`), nil, "hive:anyone", "", true, gas, "")
	l := ParseListing(listing)
	assert.Equal(t, uint64(5), l.Amount) // full rollback: nothing consumed
}

// ===================================
// Partial offer cancel refund
// ===================================

func TestCancelPartiallyAcceptedOffer(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	MintNft(t, ct, ownerAddress, "1", 10, 100)

	buyer := "hive:buyer"
	MintAndApproveToken(t, ct, buyer, 50000)

	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":10,"paymentToken":"%s","pricePerUnit":"1000"}`, NftContractID, TokenID)
	CallMarket(t, ct, "makeOffer", []byte(payload), nil, buyer, "", true, gas, "")

	// Accept 4 of 10
	ApproveNftForMarket(t, ct, ownerAddress)
	CallMarket(t, ct, "acceptOffer", []byte(`{"offerId":0,"amount":4}`), nil, ownerAddress, "", true, gas, "")

	// Cancel remaining 6 (refund 6 * 1000 = 6000)
	CallMarket(t, ct, "cancelOffer", []byte(`{"offerId":0}`), nil, buyer, "", true, gas, "")

	// Buyer started with 50000, escrowed 10000, got back 6000 = 46000 remaining
	// (The 4000 was distributed to seller, fee, etc.)
	buyerBalance := QueryTokenBalance(t, ct, buyer)
	// 50000 - 10000 (escrowed) + 6000 (refund) = 46000
	assert.Equal(t, uint64(46000), buyerBalance)
}

// ===================================
// Royalty edge cases
// ===================================

func TestRoyaltyMaxValue(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	// Set max royalty (50%)
	payload := fmt.Sprintf(`{"nftContract":"%s","royaltyBps":5000,"royaltyRecipient":"hive:creator"}`, NftContractID)
	CallMarket(t, ct, "setRoyalty", []byte(payload), nil, ownerAddress, "", true, gas, "")

	MintNft(t, ct, ownerAddress, "1", 1, 1)
	ApproveNftForMarket(t, ct, ownerAddress)

	listPayload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":1,"paymentToken":"%s","pricePerUnit":"10000"}`, NftContractID, TokenID)
	CallMarket(t, ct, "list", []byte(listPayload), nil, ownerAddress, "", true, gas, "")

	buyer := "hive:buyer"
	MintAndApproveToken(t, ct, buyer, 10000)
	CallMarket(t, ct, "buy", []byte(`{"listingId":0,"amount":1}`), nil, buyer, "", true, gas, "")

	// fee=250 (2.5%), royalty=5000 (50%), seller=10000-250-5000=4750
	sellerBalance := QueryTokenBalance(t, ct, ownerAddress)
	assert.Equal(t, uint64(4750), sellerBalance)
	creatorBalance := QueryTokenBalance(t, ct, "hive:creator")
	assert.Equal(t, uint64(5000), creatorBalance)
}

func TestRoyaltyUpdateDoesNotAffectExistingOffers(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	// No royalty initially
	MintNft(t, ct, ownerAddress, "1", 5, 100)

	buyer := "hive:buyer"
	MintAndApproveToken(t, ct, buyer, 50000)

	offerPayload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","pricePerUnit":"1000"}`, NftContractID, TokenID)
	CallMarket(t, ct, "makeOffer", []byte(offerPayload), nil, buyer, "", true, gas, "")

	// Set 10% royalty AFTER offer was made
	royaltyPayload := fmt.Sprintf(`{"nftContract":"%s","royaltyBps":1000,"royaltyRecipient":"hive:creator"}`, NftContractID)
	CallMarket(t, ct, "setRoyalty", []byte(royaltyPayload), nil, ownerAddress, "", true, gas, "")

	ApproveNftForMarket(t, ct, ownerAddress)
	CallMarket(t, ct, "acceptOffer", []byte(`{"offerId":0}`), nil, ownerAddress, "", true, gas, "")

	// Offer locked 0% royalty, so creator gets nothing
	creatorBalance := QueryTokenBalance(t, ct, "hive:creator")
	assert.Equal(t, uint64(0), creatorBalance)

	// Total = 5000, fee = 125, royalty = 0, seller = 4875
	sellerBalance := QueryTokenBalance(t, ct, ownerAddress)
	assert.Equal(t, uint64(4875), sellerBalance)
}

// ===================================
// Emergency withdraw edge case
// ===================================

func TestEmergencyWithdrawNftMissingTokenId(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	CallMarket(t, ct, "pause", nil, nil, ownerAddress, "", true, gas, "")

	withdrawPayload := fmt.Sprintf(`{"tokenType":"nft","contract":"%s","tokenId":"","amount":"5","to":"hive:rescue"}`, NftContractID)
	CallMarket(t, ct, "emergencyWithdraw", []byte(withdrawPayload), nil, ownerAddress, "", false, gas, "Token ID required for NFT withdraw")
}

// ===================================
// Auction when paused
// ===================================

func TestCreateAuctionWhenPaused(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	CallMarket(t, ct, "pause", nil, nil, ownerAddress, "", true, gas, "")

	ct.BlockHeight = 100
	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":1,"paymentToken":"%s","auctionType":"english","startPrice":"1000","endPrice":"0","startBlock":100,"endBlock":200}`, NftContractID, TokenID)
	CallMarket(t, ct, "createAuction", []byte(payload), nil, ownerAddress, "", false, gas, "Contract is paused")
}

func TestPlaceBidWhenPaused(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	MintNft(t, ct, ownerAddress, "1", 1, 1)
	ApproveNftForMarket(t, ct, ownerAddress)

	ct.BlockHeight = 100
	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":1,"paymentToken":"%s","auctionType":"english","startPrice":"1000","endPrice":"0","startBlock":100,"endBlock":200}`, NftContractID, TokenID)
	CallMarket(t, ct, "createAuction", []byte(payload), nil, ownerAddress, "", true, gas, "")

	CallMarket(t, ct, "pause", nil, nil, ownerAddress, "", true, gas, "")

	bidder := "hive:bidder"
	MintAndApproveToken(t, ct, bidder, 5000)

	ct.BlockHeight = 120
	CallMarket(t, ct, "placeBid", []byte(`{"auctionId":0,"bidAmount":"1000"}`), nil, bidder, "", false, gas, "Contract is paused")
}

// ===================================
// Settle not blocked by pause (anyone should be able to settle)
// ===================================

func TestSettleAuctionNotBlockedByPause(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	MintNft(t, ct, ownerAddress, "1", 1, 1)
	ApproveNftForMarket(t, ct, ownerAddress)

	ct.BlockHeight = 100
	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":1,"paymentToken":"%s","auctionType":"english","startPrice":"1000","endPrice":"0","startBlock":100,"endBlock":200}`, NftContractID, TokenID)
	CallMarket(t, ct, "createAuction", []byte(payload), nil, ownerAddress, "", true, gas, "")

	bidder := "hive:bidder"
	MintAndApproveToken(t, ct, bidder, 5000)
	ct.BlockHeight = 150
	CallMarket(t, ct, "placeBid", []byte(`{"auctionId":0,"bidAmount":"2000"}`), nil, bidder, "", true, gas, "")

	// Pause contract
	CallMarket(t, ct, "pause", nil, nil, ownerAddress, "", true, gas, "")

	// Settle should still work (settleAuction doesn't assertNotPaused)
	ct.BlockHeight = 201
	CallMarket(t, ct, "settleAuction", []byte(`{"auctionId":0}`), nil, "hive:anyone", "", true, gas, "")

	winnerNft := QueryNftBalance(t, ct, bidder, "1")
	assert.Equal(t, uint64(1), winnerNft)
}

// ===================================
// Multiple offers same NFT
// ===================================

func TestMultipleOffersAcceptSpecific(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	MintNft(t, ct, ownerAddress, "1", 10, 100)

	buyer1 := "hive:buyer1"
	buyer2 := "hive:buyer2"
	MintAndApproveToken(t, ct, buyer1, 50000)
	MintAndApproveToken(t, ct, buyer2, 50000)

	// Two different offers on same NFT
	offer1 := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":3,"paymentToken":"%s","pricePerUnit":"1000"}`, NftContractID, TokenID)
	CallMarket(t, ct, "makeOffer", []byte(offer1), nil, buyer1, "", true, gas, "")

	offer2 := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","pricePerUnit":"2000"}`, NftContractID, TokenID)
	CallMarket(t, ct, "makeOffer", []byte(offer2), nil, buyer2, "", true, gas, "")

	// Accept offer 1 only
	ApproveNftForMarket(t, ct, ownerAddress)
	CallMarket(t, ct, "acceptOffer", []byte(`{"offerId":0}`), nil, ownerAddress, "", true, gas, "")

	// Offer 0 should be inactive, offer 1 still active
	result0, _, _ := CallMarket(t, ct, "getOffer", []byte(`{"offerId":0}`), nil, "hive:anyone", "", true, gas, "")
	assert.False(t, ParseOffer(result0).Active)

	result1, _, _ := CallMarket(t, ct, "getOffer", []byte(`{"offerId":1}`), nil, "hive:anyone", "", true, gas, "")
	assert.True(t, ParseOffer(result1).Active)
}
