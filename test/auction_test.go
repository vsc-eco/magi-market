package contract_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

// ===================================
// English Auction Tests
// ===================================

func TestCreateEnglishAuction(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	MintNft(t, ct, ownerAddress, "1", 1, 1)
	ApproveNftForMarket(t, ct, ownerAddress)

	ct.BlockHeight = 100

	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":1,"paymentToken":"%s","auctionType":"english","startPrice":"1000","endPrice":"0","startBlock":100,"endBlock":200}`, NftContractID, TokenID)
	_, _, logs := CallMarket(t, ct, "createAuction", []byte(payload), nil, ownerAddress, "", true, gas, "")
	AssertEventEmitted(t, logs, "auction_created")

	result, _, _ := CallMarket(t, ct, "getAuction", []byte(`{"auctionId":0}`), nil, "hive:anyone", "", true, gas, "")
	auction := ParseAuction(result)
	assert.Equal(t, "english", auction.AuctionType)
	assert.Equal(t, "1000", auction.StartPrice)
	assert.Equal(t, uint64(200), auction.EndBlock)
	assert.True(t, auction.Active)
	assert.False(t, auction.Settled)
}

func TestEnglishAuctionBidding(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	MintNft(t, ct, ownerAddress, "1", 1, 1)
	ApproveNftForMarket(t, ct, ownerAddress)

	ct.BlockHeight = 100
	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":1,"paymentToken":"%s","auctionType":"english","startPrice":"1000","endPrice":"0","startBlock":100,"endBlock":200}`, NftContractID, TokenID)
	CallMarket(t, ct, "createAuction", []byte(payload), nil, ownerAddress, "", true, gas, "")

	bidder1 := "hive:bidder1"
	bidder2 := "hive:bidder2"
	MintAndApproveToken(t, ct, bidder1, 5000)
	MintAndApproveToken(t, ct, bidder2, 5000)

	ct.BlockHeight = 120
	// First bid at reserve
	CallMarket(t, ct, "placeBid", []byte(`{"auctionId":0,"bidAmount":"1000"}`), nil, bidder1, "", true, gas, "")

	// Second bid must exceed first
	CallMarket(t, ct, "placeBid", []byte(`{"auctionId":0,"bidAmount":"1500"}`), nil, bidder2, "", true, gas, "")

	// First bidder should be refunded
	bidder1Balance := QueryTokenBalance(t, ct, bidder1)
	assert.Equal(t, uint64(5000), bidder1Balance)

	// Verify high bidder
	result, _, _ := CallMarket(t, ct, "getAuction", []byte(`{"auctionId":0}`), nil, "hive:anyone", "", true, gas, "")
	auction := ParseAuction(result)
	assert.Equal(t, "hive:bidder2", auction.HighBidder)
	assert.Equal(t, "1500", auction.HighBid)
}

func TestEnglishAuctionBidBelowReserve(t *testing.T) {
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
	CallMarket(t, ct, "placeBid", []byte(`{"auctionId":0,"bidAmount":"500"}`), nil, bidder, "", false, gas, "Bid must be at least the reserve price")
}

func TestEnglishAuctionBidNotHigherThanCurrent(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	MintNft(t, ct, ownerAddress, "1", 1, 1)
	ApproveNftForMarket(t, ct, ownerAddress)

	ct.BlockHeight = 100
	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":1,"paymentToken":"%s","auctionType":"english","startPrice":"1000","endPrice":"0","startBlock":100,"endBlock":200}`, NftContractID, TokenID)
	CallMarket(t, ct, "createAuction", []byte(payload), nil, ownerAddress, "", true, gas, "")

	bidder1 := "hive:bidder1"
	bidder2 := "hive:bidder2"
	MintAndApproveToken(t, ct, bidder1, 5000)
	MintAndApproveToken(t, ct, bidder2, 5000)

	ct.BlockHeight = 120
	CallMarket(t, ct, "placeBid", []byte(`{"auctionId":0,"bidAmount":"2000"}`), nil, bidder1, "", true, gas, "")
	CallMarket(t, ct, "placeBid", []byte(`{"auctionId":0,"bidAmount":"2000"}`), nil, bidder2, "", false, gas, "Bid must exceed current high bid")
}

func TestEnglishAuctionSettlement(t *testing.T) {
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

	// Cannot settle before end
	CallMarket(t, ct, "settleAuction", []byte(`{"auctionId":0}`), nil, "hive:anyone", "", false, gas, "Auction has not ended yet")

	// Settle after end
	ct.BlockHeight = 201
	_, _, logs := CallMarket(t, ct, "settleAuction", []byte(`{"auctionId":0}`), nil, "hive:anyone", "", true, gas, "")
	AssertEventEmitted(t, logs, "auction_settled")

	// Winner should have NFT
	winnerNft := QueryNftBalance(t, ct, bidder, "1")
	assert.Equal(t, uint64(1), winnerNft)

	// Seller should have payment minus fee (2000 - 2.5% = 1950)
	sellerBalance := QueryTokenBalance(t, ct, ownerAddress)
	assert.Equal(t, uint64(1950), sellerBalance)
}

func TestEnglishAuctionSettlementNoBids(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	MintNft(t, ct, ownerAddress, "1", 1, 1)
	ApproveNftForMarket(t, ct, ownerAddress)

	ct.BlockHeight = 100
	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":1,"paymentToken":"%s","auctionType":"english","startPrice":"1000","endPrice":"0","startBlock":100,"endBlock":200}`, NftContractID, TokenID)
	CallMarket(t, ct, "createAuction", []byte(payload), nil, ownerAddress, "", true, gas, "")

	ct.BlockHeight = 201
	CallMarket(t, ct, "settleAuction", []byte(`{"auctionId":0}`), nil, "hive:anyone", "", true, gas, "")

	// NFT should be returned to seller
	sellerNft := QueryNftBalance(t, ct, ownerAddress, "1")
	assert.Equal(t, uint64(1), sellerNft)
}

func TestEnglishAuctionBidAfterEnd(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	MintNft(t, ct, ownerAddress, "1", 1, 1)
	ApproveNftForMarket(t, ct, ownerAddress)

	ct.BlockHeight = 100
	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":1,"paymentToken":"%s","auctionType":"english","startPrice":"1000","endPrice":"0","startBlock":100,"endBlock":200}`, NftContractID, TokenID)
	CallMarket(t, ct, "createAuction", []byte(payload), nil, ownerAddress, "", true, gas, "")

	bidder := "hive:bidder"
	MintAndApproveToken(t, ct, bidder, 5000)

	ct.BlockHeight = 201
	CallMarket(t, ct, "placeBid", []byte(`{"auctionId":0,"bidAmount":"2000"}`), nil, bidder, "", false, gas, "Auction has ended")
}

// ===================================
// Dutch Auction Tests
// ===================================

func TestCreateDutchAuction(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	MintNft(t, ct, ownerAddress, "1", 1, 1)
	ApproveNftForMarket(t, ct, ownerAddress)

	ct.BlockHeight = 100

	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":1,"paymentToken":"%s","auctionType":"dutch","startPrice":"10000","endPrice":"1000","startBlock":100,"endBlock":200}`, NftContractID, TokenID)
	CallMarket(t, ct, "createAuction", []byte(payload), nil, ownerAddress, "", true, gas, "")

	result, _, _ := CallMarket(t, ct, "getAuction", []byte(`{"auctionId":0}`), nil, "hive:anyone", "", true, gas, "")
	auction := ParseAuction(result)
	assert.Equal(t, "dutch", auction.AuctionType)
	assert.Equal(t, "10000", auction.StartPrice)
	assert.Equal(t, "1000", auction.EndPrice)
}

func TestDutchAuctionBuyAtMidpoint(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	MintNft(t, ct, ownerAddress, "1", 1, 1)
	ApproveNftForMarket(t, ct, ownerAddress)

	ct.BlockHeight = 100
	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":1,"paymentToken":"%s","auctionType":"dutch","startPrice":"10000","endPrice":"1000","startBlock":100,"endBlock":200}`, NftContractID, TokenID)
	CallMarket(t, ct, "createAuction", []byte(payload), nil, ownerAddress, "", true, gas, "")

	buyer := "hive:buyer"
	MintAndApproveToken(t, ct, buyer, 20000)

	// At midpoint (block 150), price should be ~5500 (10000 - (9000 * 50 / 100))
	ct.BlockHeight = 150
	CallMarket(t, ct, "placeBid", []byte(`{"auctionId":0,"bidAmount":"5500"}`), nil, buyer, "", true, gas, "")

	// Verify auction is settled
	result, _, _ := CallMarket(t, ct, "getAuction", []byte(`{"auctionId":0}`), nil, "hive:anyone", "", true, gas, "")
	auction := ParseAuction(result)
	assert.True(t, auction.Settled)
	assert.False(t, auction.Active)

	// Buyer has NFT
	buyerNft := QueryNftBalance(t, ct, buyer, "1")
	assert.Equal(t, uint64(1), buyerNft)
}

func TestDutchAuctionBidTooLow(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	MintNft(t, ct, ownerAddress, "1", 1, 1)
	ApproveNftForMarket(t, ct, ownerAddress)

	ct.BlockHeight = 100
	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":1,"paymentToken":"%s","auctionType":"dutch","startPrice":"10000","endPrice":"1000","startBlock":100,"endBlock":200}`, NftContractID, TokenID)
	CallMarket(t, ct, "createAuction", []byte(payload), nil, ownerAddress, "", true, gas, "")

	buyer := "hive:buyer"
	MintAndApproveToken(t, ct, buyer, 20000)

	ct.BlockHeight = 150
	// Price at midpoint is 5500, bid below that
	CallMarket(t, ct, "placeBid", []byte(`{"auctionId":0,"bidAmount":"3000"}`), nil, buyer, "", false, gas, "Bid must be at least the current total price")
}

func TestDutchAuctionEndPriceNotLess(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	MintNft(t, ct, ownerAddress, "1", 1, 1)
	ApproveNftForMarket(t, ct, ownerAddress)

	ct.BlockHeight = 100
	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":1,"paymentToken":"%s","auctionType":"dutch","startPrice":"1000","endPrice":"2000","startBlock":100,"endBlock":200}`, NftContractID, TokenID)
	CallMarket(t, ct, "createAuction", []byte(payload), nil, ownerAddress, "", false, gas, "Dutch auction end price must be less than start price")
}

// ===================================
// Auction Validation Tests
// ===================================

func TestCreateAuctionInvalidType(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	MintNft(t, ct, ownerAddress, "1", 1, 1)
	ApproveNftForMarket(t, ct, ownerAddress)

	ct.BlockHeight = 100
	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":1,"paymentToken":"%s","auctionType":"reverse","startPrice":"1000","endPrice":"0","startBlock":100,"endBlock":200}`, NftContractID, TokenID)
	CallMarket(t, ct, "createAuction", []byte(payload), nil, ownerAddress, "", false, gas, "Auction type must be")
}

func TestCreateAuctionEndBlockPast(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	MintNft(t, ct, ownerAddress, "1", 1, 1)
	ApproveNftForMarket(t, ct, ownerAddress)

	ct.BlockHeight = 300
	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":1,"paymentToken":"%s","auctionType":"english","startPrice":"1000","endPrice":"0","startBlock":100,"endBlock":200}`, NftContractID, TokenID)
	CallMarket(t, ct, "createAuction", []byte(payload), nil, ownerAddress, "", false, gas, "End block must be in the future")
}

func TestCancelAuctionNoBids(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	MintNft(t, ct, ownerAddress, "1", 1, 1)
	ApproveNftForMarket(t, ct, ownerAddress)

	ct.BlockHeight = 100
	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":1,"paymentToken":"%s","auctionType":"english","startPrice":"1000","endPrice":"0","startBlock":100,"endBlock":200}`, NftContractID, TokenID)
	CallMarket(t, ct, "createAuction", []byte(payload), nil, ownerAddress, "", true, gas, "")

	_, _, logs := CallMarket(t, ct, "cancelAuction", []byte(`{"auctionId":0}`), nil, ownerAddress, "", true, gas, "")
	AssertEventEmitted(t, logs, "auction_cancelled")

	// NFT returned to seller
	sellerNft := QueryNftBalance(t, ct, ownerAddress, "1")
	assert.Equal(t, uint64(1), sellerNft)
}

func TestCancelAuctionWithBids(t *testing.T) {
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
	CallMarket(t, ct, "placeBid", []byte(`{"auctionId":0,"bidAmount":"1500"}`), nil, bidder, "", true, gas, "")

	// Cannot cancel with active bids
	CallMarket(t, ct, "cancelAuction", []byte(`{"auctionId":0}`), nil, ownerAddress, "", false, gas, "Cannot cancel auction with active bids")
}

func TestCancelAuctionNotSeller(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	MintNft(t, ct, ownerAddress, "1", 1, 1)
	ApproveNftForMarket(t, ct, ownerAddress)

	ct.BlockHeight = 100
	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":1,"paymentToken":"%s","auctionType":"english","startPrice":"1000","endPrice":"0","startBlock":100,"endBlock":200}`, NftContractID, TokenID)
	CallMarket(t, ct, "createAuction", []byte(payload), nil, ownerAddress, "", true, gas, "")

	CallMarket(t, ct, "cancelAuction", []byte(`{"auctionId":0}`), nil, "hive:random", "", false, gas, "Only seller can cancel auction")
}

func TestAuctionSoulboundToken(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	// Mint soulbound
	payload := `{"to":"hive:tibfox","id":"soul1","amount":1,"maxSupply":1,"soulbound":true}`
	CallNft(t, ct, "mint", []byte(payload), nil, ownerAddress, true, gas, "")
	ApproveNftForMarket(t, ct, ownerAddress)

	ct.BlockHeight = 100
	auctionPayload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"soul1","amount":1,"paymentToken":"%s","auctionType":"english","startPrice":"1000","endPrice":"0","startBlock":100,"endBlock":200}`, NftContractID, TokenID)
	CallMarket(t, ct, "createAuction", []byte(auctionPayload), nil, ownerAddress, "", false, gas, "Cannot auction soulbound tokens")
}

func TestAuctionWithRoyalty(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	// Set 10% royalty
	royaltyPayload := fmt.Sprintf(`{"nftContract":"%s","royaltyBps":1000,"royaltyRecipient":"hive:creator"}`, NftContractID)
	CallMarket(t, ct, "setRoyalty", []byte(royaltyPayload), nil, ownerAddress, "", true, gas, "")

	MintNft(t, ct, ownerAddress, "1", 1, 1)
	ApproveNftForMarket(t, ct, ownerAddress)

	ct.BlockHeight = 100
	auctionPayload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":1,"paymentToken":"%s","auctionType":"english","startPrice":"1000","endPrice":"0","startBlock":100,"endBlock":200}`, NftContractID, TokenID)
	CallMarket(t, ct, "createAuction", []byte(auctionPayload), nil, ownerAddress, "", true, gas, "")

	bidder := "hive:bidder"
	MintAndApproveToken(t, ct, bidder, 10000)

	ct.BlockHeight = 150
	CallMarket(t, ct, "placeBid", []byte(`{"auctionId":0,"bidAmount":"5000"}`), nil, bidder, "", true, gas, "")

	ct.BlockHeight = 201
	CallMarket(t, ct, "settleAuction", []byte(`{"auctionId":0}`), nil, "hive:anyone", "", true, gas, "")

	// fee=125, royalty=500, seller=4375
	sellerBalance := QueryTokenBalance(t, ct, ownerAddress)
	assert.Equal(t, uint64(4375), sellerBalance)
	creatorBalance := QueryTokenBalance(t, ct, "hive:creator")
	assert.Equal(t, uint64(500), creatorBalance)
}
