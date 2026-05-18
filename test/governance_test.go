package contract_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	"vsc-node/lib/test_utils"
)

func ParseCollectionDenied(result test_utils.ContractTestCallResult) bool {
	var resp struct {
		Denied bool `json:"denied"`
	}
	json.Unmarshal([]byte(result.Ret), &resp)
	return resp.Denied
}

func ParsePendingOwner(result test_utils.ContractTestCallResult) string {
	var resp struct {
		PendingOwner string `json:"pendingOwner"`
	}
	json.Unmarshal([]byte(result.Ret), &resp)
	return resp.PendingOwner
}

func TestTwoStepTransferProposeDoesNotMoveOwner(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	_, _, logs := CallMarket(t, ct, "changeOwner", []byte(`{"newOwner":"hive:newowner"}`), nil, ownerAddress, "", true, gas, "")
	AssertEventEmitted(t, logs, "ownerTransferInitiated")

	res, _, _ := CallMarket(t, ct, "getOwner", nil, nil, "hive:anyone", "", true, gas, "")
	assert.Equal(t, ownerAddress, ParseOwner(res), "owner unchanged until accepted")

	pend, _, _ := CallMarket(t, ct, "getPendingOwner", nil, nil, "hive:anyone", "", true, gas, "")
	assert.Equal(t, "hive:newowner", ParsePendingOwner(pend))
}

func TestTwoStepTransferAcceptByNonPendingRejected(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	CallMarket(t, ct, "changeOwner", []byte(`{"newOwner":"hive:newowner"}`), nil, ownerAddress, "", true, gas, "")
	CallMarket(t, ct, "acceptOwnership", nil, nil, "hive:someoneelse", "", false, gas, "Not the pending owner")
}

func TestTwoStepTransferAcceptFinalizes(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	CallMarket(t, ct, "changeOwner", []byte(`{"newOwner":"hive:newowner"}`), nil, ownerAddress, "", true, gas, "")
	_, _, logs := CallMarket(t, ct, "acceptOwnership", nil, nil, "hive:newowner", "", true, gas, "")
	AssertEventEmitted(t, logs, "ownerChange")

	res, _, _ := CallMarket(t, ct, "getOwner", nil, nil, "hive:anyone", "", true, gas, "")
	assert.Equal(t, "hive:newowner", ParseOwner(res))

	pend, _, _ := CallMarket(t, ct, "getPendingOwner", nil, nil, "hive:anyone", "", true, gas, "")
	assert.Equal(t, "", ParsePendingOwner(pend), "pending cleared after accept")

	// new owner can admin; old owner cannot
	CallMarket(t, ct, "setFee", []byte(`{"feeBps":500}`), nil, "hive:newowner", "", true, gas, "")
	CallMarket(t, ct, "setFee", []byte(`{"feeBps":500}`), nil, ownerAddress, "", false, gas, "Only owner can set fee")
}

func TestTwoStepTransferAcceptNoPendingRejected(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	CallMarket(t, ct, "acceptOwnership", nil, nil, "hive:newowner", "", false, gas, "No pending ownership transfer")
}

func TestTwoStepTransferCancel(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	CallMarket(t, ct, "changeOwner", []byte(`{"newOwner":"hive:newowner"}`), nil, ownerAddress, "", true, gas, "")
	_, _, logs := CallMarket(t, ct, "cancelOwnershipTransfer", nil, nil, ownerAddress, "", true, gas, "")
	AssertEventEmitted(t, logs, "ownerTransferCancelled")
	pend, _, _ := CallMarket(t, ct, "getPendingOwner", nil, nil, "hive:anyone", "", true, gas, "")
	assert.Equal(t, "", ParsePendingOwner(pend))
	CallMarket(t, ct, "acceptOwnership", nil, nil, "hive:newowner", "", false, gas, "No pending ownership transfer")
}

func TestTwoStepTransferWorksWhilePaused(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	CallMarket(t, ct, "pause", nil, nil, ownerAddress, "", true, gas, "")
	CallMarket(t, ct, "changeOwner", []byte(`{"newOwner":"hive:newowner"}`), nil, ownerAddress, "", true, gas, "")
	CallMarket(t, ct, "acceptOwnership", nil, nil, "hive:newowner", "", true, gas, "")
	res, _, _ := CallMarket(t, ct, "getOwner", nil, nil, "hive:anyone", "", true, gas, "")
	assert.Equal(t, "hive:newowner", ParseOwner(res))
}

func TestDenyAllowCollectionAdmin(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	q := `{"nftContract":"` + NftContractID + `"}`
	res, _, _ := CallMarket(t, ct, "isCollectionDenied", []byte(q), nil, "hive:anyone", "", true, gas, "")
	assert.False(t, ParseCollectionDenied(res), "default allowed")

	CallMarket(t, ct, "denyCollection", []byte(q), nil, "hive:alice", "", false, gas, "Only owner can deny collection")
	_, _, logs := CallMarket(t, ct, "denyCollection", []byte(q), nil, ownerAddress, "", true, gas, "")
	AssertEventEmitted(t, logs, "collectionDenied")

	res2, _, _ := CallMarket(t, ct, "isCollectionDenied", []byte(q), nil, "hive:anyone", "", true, gas, "")
	assert.True(t, ParseCollectionDenied(res2))

	CallMarket(t, ct, "allowCollection", []byte(q), nil, "hive:alice", "", false, gas, "Only owner can allow collection")
	_, _, logs2 := CallMarket(t, ct, "allowCollection", []byte(q), nil, ownerAddress, "", true, gas, "")
	AssertEventEmitted(t, logs2, "collectionAllowed")

	res3, _, _ := CallMarket(t, ct, "isCollectionDenied", []byte(q), nil, "hive:anyone", "", true, gas, "")
	assert.False(t, ParseCollectionDenied(res3))
}

// ===================================
// Task 3: Enforcement + SettleAuction tests
// ===================================

// TestDenyCreationBlocked verifies that list/createAuction/makeOffer are blocked
// when the collection is on the denylist.
func TestDenyCreationBlocked(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	seller := ownerAddress
	buyer := "hive:buyer"
	MintNft(t, ct, seller, "1", 10, 100)
	ApproveNftForMarket(t, ct, seller)
	MintAndApproveToken(t, ct, buyer, 5000)

	// Deny the collection
	q := fmt.Sprintf(`{"nftContract":"%s"}`, NftContractID)
	CallMarket(t, ct, "denyCollection", []byte(q), nil, ownerAddress, "", true, gas, "")

	// list must abort
	listPayload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":1,"paymentToken":"%s","pricePerUnit":"1000"}`, NftContractID, TokenID)
	CallMarket(t, ct, "list", []byte(listPayload), nil, seller, "", false, gas, "Collection is denied")

	// createAuction must abort
	ct.BlockHeight = 100
	auctionPayload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":1,"paymentToken":"%s","auctionType":"english","startPrice":"1000","endPrice":"0","startBlock":100,"endBlock":200}`, NftContractID, TokenID)
	CallMarket(t, ct, "createAuction", []byte(auctionPayload), nil, seller, "", false, gas, "Collection is denied")

	// makeOffer must abort
	offerPayload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":1,"paymentToken":"%s","pricePerUnit":"1000"}`, NftContractID, TokenID)
	CallMarket(t, ct, "makeOffer", []byte(offerPayload), nil, buyer, "", false, gas, "Collection is denied")
}

// TestDenyRetroactiveBuyBlocked verifies that a listing created while allowed is blocked
// from purchase after the collection is denied.
func TestDenyRetroactiveBuyBlocked(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	seller := ownerAddress
	buyer := "hive:buyer"
	MintNft(t, ct, seller, "1", 10, 100)
	ApproveNftForMarket(t, ct, seller)
	MintAndApproveToken(t, ct, buyer, 5000)

	// Create listing while allowed
	listPayload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":1,"paymentToken":"%s","pricePerUnit":"1000"}`, NftContractID, TokenID)
	CallMarket(t, ct, "list", []byte(listPayload), nil, seller, "", true, gas, "")

	// Deny the collection
	q := fmt.Sprintf(`{"nftContract":"%s"}`, NftContractID)
	CallMarket(t, ct, "denyCollection", []byte(q), nil, ownerAddress, "", true, gas, "")

	// buy must be blocked retroactively
	CallMarket(t, ct, "buy", []byte(`{"listingId":0,"amount":1}`), nil, buyer, "", false, gas, "Collection is denied")

	// seller's NFT balance is unchanged (no escrow in listings, approval-custody)
	assert.Equal(t, uint64(10), QueryNftBalance(t, ct, seller, "1"))
	// buyer's token balance is unchanged (buy was rejected before escrow)
	assert.Equal(t, uint64(5000), QueryTokenBalance(t, ct, buyer))
}

// TestDenyRetroactivePlaceBidBlocked verifies that placeBid on an active auction is
// blocked after the collection is denied.
func TestDenyRetroactivePlaceBidBlocked(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	seller := ownerAddress
	bidder := "hive:bidder"
	MintNft(t, ct, seller, "1", 1, 1)
	ApproveNftForMarket(t, ct, seller)
	MintAndApproveToken(t, ct, bidder, 5000)

	// Create auction while allowed
	ct.BlockHeight = 100
	auctionPayload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":1,"paymentToken":"%s","auctionType":"english","startPrice":"1000","endPrice":"0","startBlock":100,"endBlock":200}`, NftContractID, TokenID)
	CallMarket(t, ct, "createAuction", []byte(auctionPayload), nil, seller, "", true, gas, "")

	// Deny the collection
	q := fmt.Sprintf(`{"nftContract":"%s"}`, NftContractID)
	CallMarket(t, ct, "denyCollection", []byte(q), nil, ownerAddress, "", true, gas, "")

	// placeBid must be blocked retroactively
	ct.BlockHeight = 120
	CallMarket(t, ct, "placeBid", []byte(`{"auctionId":0,"bidAmount":"1000"}`), nil, bidder, "", false, gas, "Collection is denied")

	// bidder's token balance is unchanged
	assert.Equal(t, uint64(5000), QueryTokenBalance(t, ct, bidder))
}

// TestDenyRetroactiveAcceptOfferBlocked verifies that acceptOffer is blocked
// after the collection is denied.
func TestDenyRetroactiveAcceptOfferBlocked(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	seller := ownerAddress
	buyer := "hive:buyer"
	MintNft(t, ct, seller, "1", 10, 100)
	ApproveNftForMarket(t, ct, seller)
	MintAndApproveToken(t, ct, buyer, 5000)

	// Create offer while allowed
	offerPayload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":1,"paymentToken":"%s","pricePerUnit":"1000"}`, NftContractID, TokenID)
	CallMarket(t, ct, "makeOffer", []byte(offerPayload), nil, buyer, "", true, gas, "")

	// Payment is escrowed
	assert.Equal(t, uint64(1000), QueryTokenBalance(t, ct, MarketContractAddress))
	assert.Equal(t, uint64(4000), QueryTokenBalance(t, ct, buyer))

	// Deny the collection
	q := fmt.Sprintf(`{"nftContract":"%s"}`, NftContractID)
	CallMarket(t, ct, "denyCollection", []byte(q), nil, ownerAddress, "", true, gas, "")

	// acceptOffer must be blocked retroactively
	CallMarket(t, ct, "acceptOffer", []byte(`{"offerId":0,"amount":1}`), nil, seller, "", false, gas, "Collection is denied")

	// seller's NFT balance is unchanged (no NFT transferred)
	assert.Equal(t, uint64(10), QueryNftBalance(t, ct, seller, "1"))
	// buyer's escrow is still in market (offer not accepted, not cancelled)
	assert.Equal(t, uint64(1000), QueryTokenBalance(t, ct, MarketContractAddress))
}

// TestDenyRecoveryDelistWorks verifies that a seller can delist an active listing
// even when the collection is denied (no NFT trapped).
func TestDenyRecoveryDelistWorks(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	seller := ownerAddress
	MintNft(t, ct, seller, "1", 10, 100)
	ApproveNftForMarket(t, ct, seller)

	// Create listing while allowed
	listPayload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","pricePerUnit":"1000"}`, NftContractID, TokenID)
	CallMarket(t, ct, "list", []byte(listPayload), nil, seller, "", true, gas, "")

	// Deny the collection
	q := fmt.Sprintf(`{"nftContract":"%s"}`, NftContractID)
	CallMarket(t, ct, "denyCollection", []byte(q), nil, ownerAddress, "", true, gas, "")

	// delist must succeed (recovery path is ungated)
	CallMarket(t, ct, "delist", []byte(`{"listingId":0}`), nil, seller, "", true, gas, "")

	// listing is now inactive
	listingResult, _, _ := CallMarket(t, ct, "getListing", []byte(`{"listingId":0}`), nil, "hive:anyone", "", true, gas, "")
	listing := ParseListing(listingResult)
	assert.False(t, listing.Active)

	// seller's NFT balance is unchanged (listings never escrow NFTs)
	assert.Equal(t, uint64(10), QueryNftBalance(t, ct, seller, "1"))
}

// TestDenyRecoveryCancelOfferWorks verifies that a buyer can cancel an active offer
// and receive their escrowed payment back even when the collection is denied.
func TestDenyRecoveryCancelOfferWorks(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	buyer := "hive:buyer"
	MintNft(t, ct, ownerAddress, "1", 10, 100)
	MintAndApproveToken(t, ct, buyer, 5000)

	// Create offer while allowed
	offerPayload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":1,"paymentToken":"%s","pricePerUnit":"1000"}`, NftContractID, TokenID)
	CallMarket(t, ct, "makeOffer", []byte(offerPayload), nil, buyer, "", true, gas, "")

	// Payment is escrowed
	assert.Equal(t, uint64(1000), QueryTokenBalance(t, ct, MarketContractAddress))
	assert.Equal(t, uint64(4000), QueryTokenBalance(t, ct, buyer))

	// Deny the collection
	q := fmt.Sprintf(`{"nftContract":"%s"}`, NftContractID)
	CallMarket(t, ct, "denyCollection", []byte(q), nil, ownerAddress, "", true, gas, "")

	// cancelOffer must succeed (recovery path is ungated)
	CallMarket(t, ct, "cancelOffer", []byte(`{"offerId":0}`), nil, buyer, "", true, gas, "")

	// buyer gets their escrow refunded
	assert.Equal(t, uint64(5000), QueryTokenBalance(t, ct, buyer))
	assert.Equal(t, uint64(0), QueryTokenBalance(t, ct, MarketContractAddress))

	// offer is now inactive
	offerResult, _, _ := CallMarket(t, ct, "getOffer", []byte(`{"offerId":0}`), nil, "hive:anyone", "", true, gas, "")
	offer := ParseOffer(offerResult)
	assert.False(t, offer.Active)
}

// TestDenyRecoveryCancelAuctionNoBidWorks verifies that a seller can cancel
// a no-bid English auction and get their escrowed NFT back even when denied.
func TestDenyRecoveryCancelAuctionNoBidWorks(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	seller := ownerAddress
	MintNft(t, ct, seller, "1", 1, 1)
	ApproveNftForMarket(t, ct, seller)

	// Create auction while allowed — NFT is escrowed by the market
	ct.BlockHeight = 100
	auctionPayload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":1,"paymentToken":"%s","auctionType":"english","startPrice":"1000","endPrice":"0","startBlock":100,"endBlock":200}`, NftContractID, TokenID)
	CallMarket(t, ct, "createAuction", []byte(auctionPayload), nil, seller, "", true, gas, "")

	// NFT is now held by the market
	assert.Equal(t, uint64(1), QueryNftBalance(t, ct, MarketContractAddress, "1"))
	assert.Equal(t, uint64(0), QueryNftBalance(t, ct, seller, "1"))

	// Deny the collection
	q := fmt.Sprintf(`{"nftContract":"%s"}`, NftContractID)
	CallMarket(t, ct, "denyCollection", []byte(q), nil, ownerAddress, "", true, gas, "")

	// cancelAuction must succeed (recovery path is ungated)
	ct.BlockHeight = 120
	CallMarket(t, ct, "cancelAuction", []byte(`{"auctionId":0}`), nil, seller, "", true, gas, "")

	// NFT returned to seller
	assert.Equal(t, uint64(0), QueryNftBalance(t, ct, MarketContractAddress, "1"))
	assert.Equal(t, uint64(1), QueryNftBalance(t, ct, seller, "1"))
}

// TestDenySettleAuctionUnwinds verifies that settling a denied+ended English auction
// unwinds: NFT returns to seller, high bidder is refunded, seller is NOT paid,
// winner does NOT receive the NFT.
func TestDenySettleAuctionUnwinds(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	seller := ownerAddress
	bidder := "hive:bidder"
	MintNft(t, ct, seller, "1", 1, 1)
	ApproveNftForMarket(t, ct, seller)
	MintAndApproveToken(t, ct, bidder, 5000)

	// Create auction while allowed — NFT escrowed
	ct.BlockHeight = 100
	auctionPayload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":1,"paymentToken":"%s","auctionType":"english","startPrice":"1000","endPrice":"0","startBlock":100,"endBlock":200}`, NftContractID, TokenID)
	CallMarket(t, ct, "createAuction", []byte(auctionPayload), nil, seller, "", true, gas, "")

	// Place a bid while allowed
	ct.BlockHeight = 120
	CallMarket(t, ct, "placeBid", []byte(`{"auctionId":0,"bidAmount":"1500"}`), nil, bidder, "", true, gas, "")

	// Bidder's token is now escrowed
	assert.Equal(t, uint64(1500), QueryTokenBalance(t, ct, MarketContractAddress))
	assert.Equal(t, uint64(3500), QueryTokenBalance(t, ct, bidder))
	// NFT held by market
	assert.Equal(t, uint64(1), QueryNftBalance(t, ct, MarketContractAddress, "1"))
	assert.Equal(t, uint64(0), QueryNftBalance(t, ct, seller, "1"))

	// Deny the collection mid-auction
	q := fmt.Sprintf(`{"nftContract":"%s"}`, NftContractID)
	CallMarket(t, ct, "denyCollection", []byte(q), nil, ownerAddress, "", true, gas, "")

	// Advance past end block
	ct.BlockHeight = 201
	_, _, logs := CallMarket(t, ct, "settleAuction", []byte(`{"auctionId":0}`), nil, "hive:anyone", "", true, gas, "")
	AssertEventEmitted(t, logs, "auction_settled")

	// NFT returned to seller (not to bidder/winner)
	assert.Equal(t, uint64(0), QueryNftBalance(t, ct, MarketContractAddress, "1"))
	assert.Equal(t, uint64(1), QueryNftBalance(t, ct, seller, "1"))
	assert.Equal(t, uint64(0), QueryNftBalance(t, ct, bidder, "1"))

	// Bidder is fully refunded their escrowed bid
	assert.Equal(t, uint64(5000), QueryTokenBalance(t, ct, bidder))
	// Seller is NOT paid
	assert.Equal(t, uint64(0), QueryTokenBalance(t, ct, seller))
	// Market holds no residual
	assert.Equal(t, uint64(0), QueryTokenBalance(t, ct, MarketContractAddress))

	// Auction is marked settled
	auctionResult, _, _ := CallMarket(t, ct, "getAuction", []byte(`{"auctionId":0}`), nil, "hive:anyone", "", true, gas, "")
	auction := ParseAuction(auctionResult)
	assert.True(t, auction.Settled)
	assert.False(t, auction.Active)
}

// TestDenyAllowReEnables verifies that after allowCollection, fresh list + buy succeeds.
func TestDenyAllowReEnables(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	seller := ownerAddress
	buyer := "hive:buyer"
	MintNft(t, ct, seller, "1", 10, 100)
	ApproveNftForMarket(t, ct, seller)
	MintAndApproveToken(t, ct, buyer, 5000)

	q := fmt.Sprintf(`{"nftContract":"%s"}`, NftContractID)

	// Deny, then re-allow
	CallMarket(t, ct, "denyCollection", []byte(q), nil, ownerAddress, "", true, gas, "")
	CallMarket(t, ct, "allowCollection", []byte(q), nil, ownerAddress, "", true, gas, "")

	// Fresh list succeeds
	listPayload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":1,"paymentToken":"%s","pricePerUnit":"1000"}`, NftContractID, TokenID)
	CallMarket(t, ct, "list", []byte(listPayload), nil, seller, "", true, gas, "")

	// Buy succeeds
	CallMarket(t, ct, "buy", []byte(`{"listingId":0,"amount":1}`), nil, buyer, "", true, gas, "")

	// Buyer received the NFT
	assert.Equal(t, uint64(1), QueryNftBalance(t, ct, buyer, "1"))
	// Seller received payment (minus fee)
	sellerBalance := QueryTokenBalance(t, ct, seller)
	assert.Greater(t, sellerBalance, uint64(0))
}
