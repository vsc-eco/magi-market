package contract_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

// ===================================
// Fix 1: Created IDs returned in responses
// ===================================

func TestListReturnsId(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	seller := ownerAddress
	MintNft(t, ct, seller, "1", 10, 100)
	ApproveNftForMarket(t, ct, seller)

	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","pricePerUnit":"100"}`, NftContractID, TokenID)
	result, _, _ := CallMarket(t, ct, "list", []byte(payload), nil, seller, "", true, gas, "")
	created := ParseCreated(result)
	assert.True(t, created.Success)
	assert.Equal(t, uint64(0), created.Id)

	// Second listing should get id=1
	payload2 := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","pricePerUnit":"200"}`, NftContractID, TokenID)
	result2, _, _ := CallMarket(t, ct, "list", []byte(payload2), nil, seller, "", true, gas, "")
	created2 := ParseCreated(result2)
	assert.Equal(t, uint64(1), created2.Id)
}

func TestMakeOfferReturnsId(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	buyer := "hive:buyer"
	MintAndApproveToken(t, ct, buyer, 10000)

	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","pricePerUnit":"100"}`, NftContractID, TokenID)
	result, _, _ := CallMarket(t, ct, "makeOffer", []byte(payload), nil, buyer, "", true, gas, "")
	created := ParseCreated(result)
	assert.True(t, created.Success)
	assert.Equal(t, uint64(0), created.Id)

	// Second offer should get id=1
	payload2 := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","pricePerUnit":"100"}`, NftContractID, TokenID)
	result2, _, _ := CallMarket(t, ct, "makeOffer", []byte(payload2), nil, buyer, "", true, gas, "")
	created2 := ParseCreated(result2)
	assert.Equal(t, uint64(1), created2.Id)
}

func TestCreateAuctionReturnsId(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	ct.BlockHeight = 10

	seller := ownerAddress
	MintNft(t, ct, seller, "1", 10, 100)
	ApproveNftForMarket(t, ct, seller)

	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","auctionType":"english","startPrice":"100","endBlock":100}`, NftContractID, TokenID)
	result, _, _ := CallMarket(t, ct, "createAuction", []byte(payload), nil, seller, "", true, gas, "")
	created := ParseCreated(result)
	assert.True(t, created.Success)
	assert.Equal(t, uint64(0), created.Id)
}

// ===================================
// Fix 2: SettleAuction state-after-transfer
// ===================================

func TestSettleAuctionStateAfterTransfer(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	ct.BlockHeight = 10

	seller := ownerAddress
	buyer := "hive:buyer"
	MintNft(t, ct, seller, "1", 5, 100)
	ApproveNftForMarket(t, ct, seller)
	MintAndApproveToken(t, ct, buyer, 10000)

	// Create and bid on English auction
	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","auctionType":"english","startPrice":"100","endBlock":50}`, NftContractID, TokenID)
	CallMarket(t, ct, "createAuction", []byte(payload), nil, seller, "", true, gas, "")
	CallMarket(t, ct, "placeBid", []byte(`{"auctionId":0,"bidAmount":"2000"}`), nil, buyer, "", true, gas, "")

	// Settle after end
	ct.BlockHeight = 51
	CallMarket(t, ct, "settleAuction", []byte(`{"auctionId":0}`), nil, "hive:anyone", "", true, gas, "")

	// Verify auction is settled and inactive
	result, _, _ := CallMarket(t, ct, "getAuction", []byte(`{"auctionId":0}`), nil, "hive:anyone", "", true, gas, "")
	auction := ParseAuction(result)
	assert.True(t, auction.Settled)
	assert.False(t, auction.Active)

	// Verify NFT transferred to winner
	assert.Equal(t, uint64(5), QueryNftBalance(t, ct, buyer, "1"))
}

// ===================================
// Fix 3: Self-bid prevention
// ===================================

func TestSellerCannotBidOwnEnglishAuction(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	ct.BlockHeight = 10

	seller := ownerAddress
	MintNft(t, ct, seller, "1", 5, 100)
	ApproveNftForMarket(t, ct, seller)
	MintAndApproveToken(t, ct, seller, 10000)

	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","auctionType":"english","startPrice":"100","endBlock":100}`, NftContractID, TokenID)
	CallMarket(t, ct, "createAuction", []byte(payload), nil, seller, "", true, gas, "")

	// Seller tries to bid on own auction
	CallMarket(t, ct, "placeBid", []byte(`{"auctionId":0,"bidAmount":"1000"}`), nil, seller, "", false, gas, "Seller cannot bid on own auction")
}

func TestSellerCannotBidOwnDutchAuction(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	ct.BlockHeight = 10

	seller := ownerAddress
	MintNft(t, ct, seller, "1", 5, 100)
	ApproveNftForMarket(t, ct, seller)
	MintAndApproveToken(t, ct, seller, 10000)

	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","auctionType":"dutch","startPrice":"200","endPrice":"50","endBlock":100}`, NftContractID, TokenID)
	CallMarket(t, ct, "createAuction", []byte(payload), nil, seller, "", true, gas, "")

	CallMarket(t, ct, "placeBid", []byte(`{"auctionId":0,"bidAmount":"5000"}`), nil, seller, "", false, gas, "Seller cannot bid on own auction")
}

func TestOtherUserCanBidOnAuction(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	ct.BlockHeight = 10

	seller := ownerAddress
	buyer := "hive:buyer"
	MintNft(t, ct, seller, "1", 5, 100)
	ApproveNftForMarket(t, ct, seller)
	MintAndApproveToken(t, ct, buyer, 10000)

	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","auctionType":"english","startPrice":"100","endBlock":100}`, NftContractID, TokenID)
	CallMarket(t, ct, "createAuction", []byte(payload), nil, seller, "", true, gas, "")

	// Other user can bid
	CallMarket(t, ct, "placeBid", []byte(`{"auctionId":0,"bidAmount":"1000"}`), nil, buyer, "", true, gas, "")
}

// ===================================
// Fix 5: Pause asymmetry fixed
// ===================================

func TestAcceptOfferWorksWhenPaused(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	seller := ownerAddress
	buyer := "hive:buyer"
	MintNft(t, ct, seller, "1", 10, 100)
	ApproveNftForMarket(t, ct, seller)
	MintAndApproveToken(t, ct, buyer, 5000)

	offerPayload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","pricePerUnit":"100"}`, NftContractID, TokenID)
	CallMarket(t, ct, "makeOffer", []byte(offerPayload), nil, buyer, "", true, gas, "")

	// Pause the contract
	CallMarket(t, ct, "pause", nil, nil, ownerAddress, "", true, gas, "")

	// Seller can still accept offers when paused
	CallMarket(t, ct, "acceptOffer", []byte(`{"offerId":0}`), nil, seller, "", true, gas, "")

	// Verify transfers happened
	assert.Equal(t, uint64(5), QueryNftBalance(t, ct, buyer, "1"))
}

func TestAcceptCollectionOfferWorksWhenPaused(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	seller := ownerAddress
	buyer := "hive:buyer"
	MintNft(t, ct, seller, "1", 10, 100)
	ApproveNftForMarket(t, ct, seller)
	MintAndApproveToken(t, ct, buyer, 5000)

	// Collection offer (no tokenId)
	offerPayload := fmt.Sprintf(`{"nftContract":"%s","amount":5,"paymentToken":"%s","pricePerUnit":"100"}`, NftContractID, TokenID)
	CallMarket(t, ct, "makeOffer", []byte(offerPayload), nil, buyer, "", true, gas, "")

	// Pause
	CallMarket(t, ct, "pause", nil, nil, ownerAddress, "", true, gas, "")

	// Seller can still accept collection offers when paused
	CallMarket(t, ct, "acceptCollectionOffer", []byte(`{"offerId":0,"tokenId":"1","amount":5}`), nil, seller, "", true, gas, "")

	assert.Equal(t, uint64(5), QueryNftBalance(t, ct, buyer, "1"))
}

func TestMakeOfferBlockedWhenPaused(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	buyer := "hive:buyer"
	MintAndApproveToken(t, ct, buyer, 5000)

	CallMarket(t, ct, "pause", nil, nil, ownerAddress, "", true, gas, "")

	// Making new offers is still blocked
	offerPayload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","pricePerUnit":"100"}`, NftContractID, TokenID)
	CallMarket(t, ct, "makeOffer", []byte(offerPayload), nil, buyer, "", false, gas, "Contract is paused")
}

// ===================================
// Fix 6: Refund-before-escrow ordering
// (Escrow new bid BEFORE refunding previous)
// ===================================

func TestEnglishAuctionBidOrderingEscrowFirst(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	ct.BlockHeight = 10

	seller := ownerAddress
	bidder1 := "hive:bidder1"
	bidder2 := "hive:bidder2"

	MintNft(t, ct, seller, "1", 5, 100)
	ApproveNftForMarket(t, ct, seller)
	MintAndApproveToken(t, ct, bidder1, 1000)
	MintAndApproveToken(t, ct, bidder2, 2000)

	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","auctionType":"english","startPrice":"100","endBlock":100}`, NftContractID, TokenID)
	CallMarket(t, ct, "createAuction", []byte(payload), nil, seller, "", true, gas, "")

	// Bidder1 bids 1000
	CallMarket(t, ct, "placeBid", []byte(`{"auctionId":0,"bidAmount":"1000"}`), nil, bidder1, "", true, gas, "")
	assert.Equal(t, uint64(0), QueryTokenBalance(t, ct, bidder1))

	// Bidder2 bids 2000 — bidder1 should get refunded
	CallMarket(t, ct, "placeBid", []byte(`{"auctionId":0,"bidAmount":"2000"}`), nil, bidder2, "", true, gas, "")

	// Bidder1 got refunded
	assert.Equal(t, uint64(1000), QueryTokenBalance(t, ct, bidder1))
	// Bidder2's funds are escrowed
	assert.Equal(t, uint64(0), QueryTokenBalance(t, ct, bidder2))
	// Marketplace holds bidder2's bid
	assert.Equal(t, uint64(2000), QueryTokenBalance(t, ct, MarketContractAddress))
}

// ===================================
// Fix 7: Minimum bid increment
// ===================================

func TestSetMinBidIncrement(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	// Owner sets 5% minimum bid increment (500 bps)
	CallMarket(t, ct, "setMinBidIncrement", []byte(`{"minBidIncrementBps":500}`), nil, ownerAddress, "", true, gas, "")

	// Verify via getInfo
	result, _, _ := CallMarket(t, ct, "getInfo", nil, nil, ownerAddress, "", true, gas, "")
	info := ParseInfo(result)
	assert.Equal(t, uint64(500), info.MinBidIncrementBps)
}

func TestSetMinBidIncrementNonOwner(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	CallMarket(t, ct, "setMinBidIncrement", []byte(`{"minBidIncrementBps":500}`), nil, "hive:anyone", "", false, gas, "Only owner can set minimum bid increment")
}

func TestSetMinBidIncrementTooHigh(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	CallMarket(t, ct, "setMinBidIncrement", []byte(`{"minBidIncrementBps":10001}`), nil, ownerAddress, "", false, gas, "Min bid increment must be <= 10000")
}

func TestMinBidIncrementEnforced(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	ct.BlockHeight = 10

	seller := ownerAddress
	bidder1 := "hive:bidder1"
	bidder2 := "hive:bidder2"

	MintNft(t, ct, seller, "1", 5, 100)
	ApproveNftForMarket(t, ct, seller)
	MintAndApproveToken(t, ct, bidder1, 10000)
	MintAndApproveToken(t, ct, bidder2, 10000)

	// Set 10% min increment (1000 bps)
	CallMarket(t, ct, "setMinBidIncrement", []byte(`{"minBidIncrementBps":1000}`), nil, ownerAddress, "", true, gas, "")

	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","auctionType":"english","startPrice":"100","endBlock":100}`, NftContractID, TokenID)
	CallMarket(t, ct, "createAuction", []byte(payload), nil, seller, "", true, gas, "")

	// Bidder1 bids 1000 (meets reserve of 500)
	CallMarket(t, ct, "placeBid", []byte(`{"auctionId":0,"bidAmount":"1000"}`), nil, bidder1, "", true, gas, "")

	// Bidder2 tries 1050 (5% over 1000, needs 10% = 1100)
	CallMarket(t, ct, "placeBid", []byte(`{"auctionId":0,"bidAmount":"1050"}`), nil, bidder2, "", false, gas, "Bid must exceed current high bid by minimum increment")

	// Bidder2 bids 1100 (exactly 10% over 1000) — should succeed
	CallMarket(t, ct, "placeBid", []byte(`{"auctionId":0,"bidAmount":"1100"}`), nil, bidder2, "", true, gas, "")
}

func TestMinBidIncrementNotAppliedToFirstBid(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	ct.BlockHeight = 10

	seller := ownerAddress
	bidder := "hive:bidder"

	MintNft(t, ct, seller, "1", 1, 100)
	ApproveNftForMarket(t, ct, seller)
	MintAndApproveToken(t, ct, bidder, 10000)

	// Set 50% min increment
	CallMarket(t, ct, "setMinBidIncrement", []byte(`{"minBidIncrementBps":5000}`), nil, ownerAddress, "", true, gas, "")

	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":1,"paymentToken":"%s","auctionType":"english","startPrice":"100","endBlock":100}`, NftContractID, TokenID)
	CallMarket(t, ct, "createAuction", []byte(payload), nil, seller, "", true, gas, "")

	// First bid at exactly reserve — should succeed (no increment applied to first bid)
	CallMarket(t, ct, "placeBid", []byte(`{"auctionId":0,"bidAmount":"100"}`), nil, bidder, "", true, gas, "")
}

// ===================================
// Fix 8: Anti-snipe extension
// ===================================

func TestSetAntiSnipeBlocks(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	CallMarket(t, ct, "setAntiSnipeBlocks", []byte(`{"antiSnipeBlocks":5}`), nil, ownerAddress, "", true, gas, "")

	result, _, _ := CallMarket(t, ct, "getInfo", nil, nil, ownerAddress, "", true, gas, "")
	info := ParseInfo(result)
	assert.Equal(t, uint64(5), info.AntiSnipeBlocks)
}

func TestSetAntiSnipeBlocksNonOwner(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	CallMarket(t, ct, "setAntiSnipeBlocks", []byte(`{"antiSnipeBlocks":5}`), nil, "hive:anyone", "", false, gas, "Only owner can set anti-snipe blocks")
}

func TestAntiSnipeExtension(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	ct.BlockHeight = 10

	seller := ownerAddress
	bidder := "hive:bidder"

	MintNft(t, ct, seller, "1", 5, 100)
	ApproveNftForMarket(t, ct, seller)
	MintAndApproveToken(t, ct, bidder, 10000)

	// Set anti-snipe extension to 10 blocks
	CallMarket(t, ct, "setAntiSnipeBlocks", []byte(`{"antiSnipeBlocks":10}`), nil, ownerAddress, "", true, gas, "")

	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","auctionType":"english","startPrice":"100","endBlock":50}`, NftContractID, TokenID)
	CallMarket(t, ct, "createAuction", []byte(payload), nil, seller, "", true, gas, "")

	// Bid at block 45 (5 blocks before end, within anti-snipe window of 10)
	ct.BlockHeight = 45
	CallMarket(t, ct, "placeBid", []byte(`{"auctionId":0,"bidAmount":"1000"}`), nil, bidder, "", true, gas, "")

	// Verify endBlock was extended to currentBlock + antiSnipeBlocks = 45 + 10 = 55
	result, _, _ := CallMarket(t, ct, "getAuction", []byte(`{"auctionId":0}`), nil, "hive:anyone", "", true, gas, "")
	auction := ParseAuction(result)
	assert.Equal(t, uint64(55), auction.EndBlock)
}

func TestAntiSnipeNoExtensionWhenFarFromEnd(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	ct.BlockHeight = 10

	seller := ownerAddress
	bidder := "hive:bidder"

	MintNft(t, ct, seller, "1", 5, 100)
	ApproveNftForMarket(t, ct, seller)
	MintAndApproveToken(t, ct, bidder, 10000)

	// Set anti-snipe extension to 5 blocks
	CallMarket(t, ct, "setAntiSnipeBlocks", []byte(`{"antiSnipeBlocks":5}`), nil, ownerAddress, "", true, gas, "")

	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","auctionType":"english","startPrice":"100","endBlock":100}`, NftContractID, TokenID)
	CallMarket(t, ct, "createAuction", []byte(payload), nil, seller, "", true, gas, "")

	// Bid at block 20 (80 blocks before end, NOT within anti-snipe window)
	ct.BlockHeight = 20
	CallMarket(t, ct, "placeBid", []byte(`{"auctionId":0,"bidAmount":"1000"}`), nil, bidder, "", true, gas, "")

	// EndBlock should NOT be extended
	result, _, _ := CallMarket(t, ct, "getAuction", []byte(`{"auctionId":0}`), nil, "hive:anyone", "", true, gas, "")
	auction := ParseAuction(result)
	assert.Equal(t, uint64(100), auction.EndBlock)
}

func TestAntiSnipeNotAppliedWhenDisabled(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	ct.BlockHeight = 10

	seller := ownerAddress
	bidder := "hive:bidder"

	MintNft(t, ct, seller, "1", 5, 100)
	ApproveNftForMarket(t, ct, seller)
	MintAndApproveToken(t, ct, bidder, 10000)

	// antiSnipeBlocks defaults to 0 (disabled)

	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","auctionType":"english","startPrice":"100","endBlock":50}`, NftContractID, TokenID)
	CallMarket(t, ct, "createAuction", []byte(payload), nil, seller, "", true, gas, "")

	// Bid at block 49 (1 block before end)
	ct.BlockHeight = 49
	CallMarket(t, ct, "placeBid", []byte(`{"auctionId":0,"bidAmount":"1000"}`), nil, bidder, "", true, gas, "")

	// EndBlock should NOT be extended when anti-snipe is disabled
	result, _, _ := CallMarket(t, ct, "getAuction", []byte(`{"auctionId":0}`), nil, "hive:anyone", "", true, gas, "")
	auction := ParseAuction(result)
	assert.Equal(t, uint64(50), auction.EndBlock)
}

func TestAntiSnipeMultipleExtensions(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	ct.BlockHeight = 10

	seller := ownerAddress
	bidder1 := "hive:bidder1"
	bidder2 := "hive:bidder2"

	MintNft(t, ct, seller, "1", 5, 100)
	ApproveNftForMarket(t, ct, seller)
	MintAndApproveToken(t, ct, bidder1, 10000)
	MintAndApproveToken(t, ct, bidder2, 10000)

	// 5 block anti-snipe window
	CallMarket(t, ct, "setAntiSnipeBlocks", []byte(`{"antiSnipeBlocks":5}`), nil, ownerAddress, "", true, gas, "")

	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","auctionType":"english","startPrice":"100","endBlock":50}`, NftContractID, TokenID)
	CallMarket(t, ct, "createAuction", []byte(payload), nil, seller, "", true, gas, "")

	// First bid at block 48 (within window) → extends to 53
	ct.BlockHeight = 48
	CallMarket(t, ct, "placeBid", []byte(`{"auctionId":0,"bidAmount":"1000"}`), nil, bidder1, "", true, gas, "")
	result, _, _ := CallMarket(t, ct, "getAuction", []byte(`{"auctionId":0}`), nil, "hive:anyone", "", true, gas, "")
	assert.Equal(t, uint64(53), ParseAuction(result).EndBlock)

	// Second bid at block 52 (within new window of 53) → extends to 57
	ct.BlockHeight = 52
	CallMarket(t, ct, "placeBid", []byte(`{"auctionId":0,"bidAmount":"2000"}`), nil, bidder2, "", true, gas, "")
	result2, _, _ := CallMarket(t, ct, "getAuction", []byte(`{"auctionId":0}`), nil, "hive:anyone", "", true, gas, "")
	assert.Equal(t, uint64(57), ParseAuction(result2).EndBlock)
}
