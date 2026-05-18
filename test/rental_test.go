package contract_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"vsc-node/lib/test_utils"

	"github.com/stretchr/testify/assert"
)

// ===================================
// E1: Rental Result Types
// ===================================

type RentalResult struct {
	RentalId      uint64 `json:"rentalId"`
	Owner         string `json:"owner"`
	NftContract   string `json:"nftContract"`
	TokenId       string `json:"tokenId"`
	Amount        uint64 `json:"amount"`
	PaymentToken  string `json:"paymentToken"`
	PricePerBlock string `json:"pricePerBlock"`
	MinBlocks     uint64 `json:"minBlocks"`
	MaxBlocks     uint64 `json:"maxBlocks"`
	Active        bool   `json:"active"`
	Renter        string `json:"renter"`
	Until         uint64 `json:"until"`
	Rented        bool   `json:"rented"`
}

type ActiveRentalResult struct {
	Active bool   `json:"active"`
	Until  uint64 `json:"until"`
}

// ===================================
// E1: Rental Helper Functions
// ===================================

func GetRental(t *testing.T, ct *test_utils.ContractTest, rentalId uint64) RentalResult {
	payload := fmt.Sprintf(`{"rentalId":%d}`, rentalId)
	result, _, _ := CallMarket(t, ct, "getRental", []byte(payload), nil, "hive:anyone", "", true, gas, "")
	var resp RentalResult
	json.Unmarshal([]byte(result.Ret), &resp)
	return resp
}

func GetActiveRentalOf(t *testing.T, ct *test_utils.ContractTest, account, nftContract, tokenId string) ActiveRentalResult {
	payload := fmt.Sprintf(`{"account":%q,"nftContract":%q,"tokenId":%q}`, account, nftContract, tokenId)
	result, _, _ := CallMarket(t, ct, "getActiveRentalOf", []byte(payload), nil, "hive:anyone", "", true, gas, "")
	var resp ActiveRentalResult
	json.Unmarshal([]byte(result.Ret), &resp)
	return resp
}

// ListRental lists an NFT for rental and returns the created rental ID.
func ListRentalNft(t *testing.T, ct *test_utils.ContractTest, owner, tokenId string, amount uint64, pricePerBlock string, minBlocks, maxBlocks uint64) uint64 {
	payload := fmt.Sprintf(
		`{"nftContract":%q,"tokenId":%q,"amount":%d,"paymentToken":%q,"pricePerBlock":%q,"minBlocks":%d,"maxBlocks":%d}`,
		NftContractID, tokenId, amount, TokenID, pricePerBlock, minBlocks, maxBlocks,
	)
	result, _, logs := CallMarket(t, ct, "listRental", []byte(payload), nil, owner, "", true, gas, "")
	AssertEventEmitted(t, logs, "rentalListed")
	created := ParseCreated(result)
	return created.Id
}

// RentNft executes the rent action for a rental ID and block count.
func RentNft(t *testing.T, ct *test_utils.ContractTest, renter string, rentalId, blocks uint64) {
	payload := fmt.Sprintf(`{"rentalId":%d,"blocks":%d}`, rentalId, blocks)
	_, _, logs := CallMarket(t, ct, "rent", []byte(payload), nil, renter, "", true, gas, "")
	AssertEventEmitted(t, logs, "rented")
}

// ===================================
// E1: NFT Rental Tests
// ===================================

// TestRentEscrowsNftAndPaysOwner: rent → NFT now market-held (market balance up,
// owner balance down), owner paid received−fee−royalty, fee/royalty recipients paid,
// market token residual 0, getActiveRentalOf true with until==currentBlock+blocks, rented event.
func TestRentEscrowsNftAndPaysOwner(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	owner := ownerAddress
	renter := "hive:renter1"

	// Set royalty: 500 bps (5%) to a dedicated recipient.
	royaltyRecipient := "hive:royalty"
	royaltyPayload := fmt.Sprintf(`{"nftContract":%q,"royaltyBps":500,"royaltyRecipient":%q}`, NftContractID, royaltyRecipient)
	CallMarket(t, ct, "setRoyalty", []byte(royaltyPayload), nil, owner, "", true, gas, "")

	// Mint NFT and approve marketplace.
	MintNft(t, ct, owner, "1", 5, 100)
	ApproveNftForMarket(t, ct, owner)

	// Renter gets 50000 tokens (pricePerBlock=1000, blocks=10 => cost=10000).
	MintAndApproveToken(t, ct, renter, 50000)

	// Start at block 100.
	ct.BlockHeight = 100

	// List rental: price 1000/block, min 5 blocks, max 20 blocks.
	rentalId := ListRentalNft(t, ct, owner, "1", 1, "1000", 5, 20)

	// Snapshot balances before rent.
	ownerTokenBefore := QueryTokenBalance(t, ct, owner)
	feeRecipientBefore := QueryTokenBalance(t, ct, feeRecipientAddress)
	royaltyRecipientBefore := QueryTokenBalance(t, ct, royaltyRecipient)
	marketNftBefore := QueryNftBalance(t, ct, MarketContractAddress, "1")
	ownerNftBefore := QueryNftBalance(t, ct, owner, "1")

	// Rent for 10 blocks.
	RentNft(t, ct, renter, rentalId, 10)

	// Cost = 1000 * 10 = 10000 tokens.
	// fee = 10000 * 250/10000 = 250
	// royalty = 10000 * 500/10000 = 500
	// ownerPay = 10000 - 250 - 500 = 9250

	ownerTokenAfter := QueryTokenBalance(t, ct, owner)
	feeRecipientAfter := QueryTokenBalance(t, ct, feeRecipientAddress)
	royaltyRecipientAfter := QueryTokenBalance(t, ct, royaltyRecipient)
	marketNftAfter := QueryNftBalance(t, ct, MarketContractAddress, "1")
	ownerNftAfter := QueryNftBalance(t, ct, owner, "1")

	// NFT should be escrowed: market balance +1, owner balance -1.
	assert.Equal(t, marketNftBefore+1, marketNftAfter, "Market should hold the escrowed NFT")
	assert.Equal(t, ownerNftBefore-1, ownerNftAfter, "Owner NFT balance should decrease by 1")

	// Fee, royalty, owner payment checks.
	assert.Equal(t, uint64(250), feeRecipientAfter-feeRecipientBefore, "Fee recipient should receive 250")
	assert.Equal(t, uint64(500), royaltyRecipientAfter-royaltyRecipientBefore, "Royalty recipient should receive 500")
	assert.Equal(t, uint64(9250), ownerTokenAfter-ownerTokenBefore, "Owner should receive 9250")

	// Renter paid 10000.
	renterBalance := QueryTokenBalance(t, ct, renter)
	assert.Equal(t, uint64(40000), renterBalance, "Renter should have 40000 left")

	// Market token residual must be 0.
	marketTokenBalance := QueryTokenBalance(t, ct, MarketContractAddress)
	assert.Equal(t, uint64(0), marketTokenBalance, "Market token balance should be 0")

	// getActiveRentalOf: until == 100 + 10 = 110.
	ar := GetActiveRentalOf(t, ct, renter, NftContractID, "1")
	assert.True(t, ar.Active, "Rental should be active")
	assert.Equal(t, uint64(110), ar.Until, "Until should be current block + blocks")

	// getRental state check.
	rental := GetRental(t, ct, rentalId)
	assert.True(t, rental.Rented, "Rental record should show rented=true")
	assert.Equal(t, renter, rental.Renter)
	assert.Equal(t, uint64(110), rental.Until)
}

// TestEndRentalBeforeUntilRejected: endRental before until → abort "Rental term not over";
// then advance past until → endRental returns NFT to owner, rented=0, getActiveRentalOf false.
func TestEndRentalBeforeUntilRejected(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	owner := ownerAddress
	renter := "hive:renter2"

	MintNft(t, ct, owner, "2", 1, 10)
	ApproveNftForMarket(t, ct, owner)
	MintAndApproveToken(t, ct, renter, 50000)

	ct.BlockHeight = 50

	rentalId := ListRentalNft(t, ct, owner, "2", 1, "500", 5, 30)
	RentNft(t, ct, renter, rentalId, 10) // until = 60

	// Try endRental at block 55 (before until=60).
	ct.BlockHeight = 55
	payload := fmt.Sprintf(`{"rentalId":%d}`, rentalId)
	CallMarket(t, ct, "endRental", []byte(payload), nil, "hive:anyone", "", false, gas, "Rental term not over")

	// Advance past until.
	ct.BlockHeight = 61
	ownerNftBefore := QueryNftBalance(t, ct, owner, "2")
	_, _, logs := CallMarket(t, ct, "endRental", []byte(payload), nil, "hive:anyone", "", true, gas, "")
	AssertEventEmitted(t, logs, "rentalEnded")

	// NFT returned to owner.
	ownerNftAfter := QueryNftBalance(t, ct, owner, "2")
	assert.Equal(t, ownerNftBefore+1, ownerNftAfter, "Owner should have NFT back")

	// Rental record: rented=false.
	rental := GetRental(t, ct, rentalId)
	assert.False(t, rental.Rented)
	assert.Equal(t, "", rental.Renter)

	// getActiveRentalOf: inactive.
	ar := GetActiveRentalOf(t, ct, renter, NftContractID, "2")
	assert.False(t, ar.Active, "Rental should be inactive after end")
}

// TestEndRentalEarlyByRenterNoRefund: renter endRentalEarly mid-term → NFT back to owner,
// no token refund to renter.
func TestEndRentalEarlyByRenterNoRefund(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	owner := ownerAddress
	renter := "hive:renter3"

	MintNft(t, ct, owner, "3", 1, 10)
	ApproveNftForMarket(t, ct, owner)
	MintAndApproveToken(t, ct, renter, 50000)

	ct.BlockHeight = 100

	rentalId := ListRentalNft(t, ct, owner, "3", 1, "200", 5, 20)
	RentNft(t, ct, renter, rentalId, 10) // until = 110

	renterTokenAfterRent := QueryTokenBalance(t, ct, renter)

	// End early at block 105 (mid-term).
	ct.BlockHeight = 105
	ownerNftBefore := QueryNftBalance(t, ct, owner, "3")
	payload := fmt.Sprintf(`{"rentalId":%d}`, rentalId)
	_, _, logs := CallMarket(t, ct, "endRentalEarly", []byte(payload), nil, renter, "", true, gas, "")
	AssertEventEmitted(t, logs, "rentalEnded")

	// NFT returned to owner.
	ownerNftAfter := QueryNftBalance(t, ct, owner, "3")
	assert.Equal(t, ownerNftBefore+1, ownerNftAfter, "Owner should have NFT back after early end")

	// No token refund: renter balance unchanged.
	renterTokenAfterEnd := QueryTokenBalance(t, ct, renter)
	assert.Equal(t, renterTokenAfterRent, renterTokenAfterEnd, "Renter should get no refund")

	// Rental record: rented=false.
	rental := GetRental(t, ct, rentalId)
	assert.False(t, rental.Rented)

	// getActiveRentalOf: inactive.
	ar := GetActiveRentalOf(t, ct, renter, NftContractID, "3")
	assert.False(t, ar.Active)
}

// TestDoubleRentBlocked: rent then rent again while rented → "Already rented".
func TestDoubleRentBlocked(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	owner := ownerAddress
	renter1 := "hive:renter_a"
	renter2 := "hive:renter_b"

	MintNft(t, ct, owner, "4", 1, 10)
	ApproveNftForMarket(t, ct, owner)
	MintAndApproveToken(t, ct, renter1, 50000)
	MintAndApproveToken(t, ct, renter2, 50000)

	ct.BlockHeight = 1

	rentalId := ListRentalNft(t, ct, owner, "4", 1, "100", 5, 50)
	RentNft(t, ct, renter1, rentalId, 10)

	// Second rent attempt while rented.
	payload := fmt.Sprintf(`{"rentalId":%d,"blocks":5}`, rentalId)
	CallMarket(t, ct, "rent", []byte(payload), nil, renter2, "", false, gas, "Already rented")
}

// TestRentDeniedCollectionBlocked: deny collection → rent aborts "Collection is denied".
func TestRentDeniedCollectionBlocked(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	owner := ownerAddress
	renter := "hive:renter_deny"

	MintNft(t, ct, owner, "5", 1, 10)
	ApproveNftForMarket(t, ct, owner)
	MintAndApproveToken(t, ct, renter, 50000)

	ct.BlockHeight = 1

	rentalId := ListRentalNft(t, ct, owner, "5", 1, "100", 5, 50)

	// Deny the collection.
	denyPayload := fmt.Sprintf(`{"nftContract":%q}`, NftContractID)
	CallMarket(t, ct, "denyCollection", []byte(denyPayload), nil, owner, "", true, gas, "")

	// Rent should fail.
	rentPayload := fmt.Sprintf(`{"rentalId":%d,"blocks":5}`, rentalId)
	CallMarket(t, ct, "rent", []byte(rentPayload), nil, renter, "", false, gas, "Collection is denied")
}

// TestDelistRentalBlockedWhileRented: rent then delistRental → "Cannot delist while rented";
// delist allowed before and after rental.
func TestDelistRentalBlockedWhileRented(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	owner := ownerAddress
	renter := "hive:renter_delist"

	MintNft(t, ct, owner, "6", 1, 10)
	ApproveNftForMarket(t, ct, owner)
	MintAndApproveToken(t, ct, renter, 50000)

	ct.BlockHeight = 1

	// List, then rent it.
	rentalId := ListRentalNft(t, ct, owner, "6", 1, "100", 5, 50)
	RentNft(t, ct, renter, rentalId, 10)

	// Try to delist while rented — should fail.
	delistPayload := fmt.Sprintf(`{"rentalId":%d}`, rentalId)
	CallMarket(t, ct, "delistRental", []byte(delistPayload), nil, owner, "", false, gas, "Cannot delist while rented")

	// End the rental (advance past until=11).
	ct.BlockHeight = 12
	endPayload := fmt.Sprintf(`{"rentalId":%d}`, rentalId)
	CallMarket(t, ct, "endRental", []byte(endPayload), nil, "hive:anyone", "", true, gas, "")

	// Now delist should succeed.
	_, _, logs := CallMarket(t, ct, "delistRental", []byte(delistPayload), nil, owner, "", true, gas, "")
	AssertEventEmitted(t, logs, "rentalDelisted")

	rental := GetRental(t, ct, rentalId)
	assert.False(t, rental.Active, "Rental should be inactive after delist")
}

// TestGetActiveRentalOfExpires: after until block height, getActiveRentalOf reports inactive
// even before endRental called.
func TestGetActiveRentalOfExpires(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	owner := ownerAddress
	renter := "hive:renter_expire"

	MintNft(t, ct, owner, "7", 1, 10)
	ApproveNftForMarket(t, ct, owner)
	MintAndApproveToken(t, ct, renter, 50000)

	ct.BlockHeight = 200

	rentalId := ListRentalNft(t, ct, owner, "7", 1, "100", 5, 30)

	_ = rentalId

	RentNft(t, ct, renter, rentalId, 5) // until = 205

	// At block 204 (< until=205): still active.
	ct.BlockHeight = 204
	ar := GetActiveRentalOf(t, ct, renter, NftContractID, "7")
	assert.True(t, ar.Active, "Rental should be active before until")
	assert.Equal(t, uint64(205), ar.Until)

	// At block 205 (== until): NOT active (getCurrentBlockHeight() < until fails when equal).
	ct.BlockHeight = 205
	ar2 := GetActiveRentalOf(t, ct, renter, NftContractID, "7")
	assert.False(t, ar2.Active, "Rental should be inactive at until (not strictly <)")

	// Rental record still shows rented=true (not yet ended on-chain).
	rental := GetRental(t, ct, rentalId)
	assert.True(t, rental.Rented, "Contract state still rented until endRental called")
}
