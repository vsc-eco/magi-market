package contract_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"vsc-node/lib/test_utils"

	"github.com/stretchr/testify/assert"
)

// ===================================
// Swap Result Type
// ===================================

type SwapResult struct {
	SwapId          uint64 `json:"swapId"`
	Proposer        string `json:"proposer"`
	OfferedNft      string `json:"offeredNft"`
	OfferedTokenId  string `json:"offeredTokenId"`
	OfferedAmount   uint64 `json:"offeredAmount"`
	WantedNft       string `json:"wantedNft"`
	WantedTokenId   string `json:"wantedTokenId"`
	WantedAmount    uint64 `json:"wantedAmount"`
	TopUp           string `json:"topUp"`
	TopUpToken      string `json:"topUpToken"`
	Active          bool   `json:"active"`
	ExpirationBlock uint64 `json:"expirationBlock"`
}

// ===================================
// Swap Helper Functions
// ===================================

func GetSwap(t *testing.T, ct *test_utils.ContractTest, swapId uint64) SwapResult {
	payload := fmt.Sprintf(`{"swapId":%d}`, swapId)
	result, _, _ := CallMarket(t, ct, "getSwap", []byte(payload), nil, "hive:anyone", "", true, gas, "")
	var resp SwapResult
	json.Unmarshal([]byte(result.Ret), &resp)
	return resp
}

// ===================================
// D1 Swap Tests
// ===================================

// TestSwapHappyPathNoTopUp: both NFTs change hands proposer↔acceptor, no token movement, swapAccepted emitted.
func TestSwapHappyPathNoTopUp(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	proposer := ownerAddress // "hive:tibfox"
	acceptor := "hive:alice"

	// Mint offered NFT (tokenId "10") to proposer, wanted NFT (tokenId "20") to acceptor
	MintNft(t, ct, proposer, "10", 5, 100)
	MintNft(t, ct, acceptor, "20", 3, 100)

	// Both approve marketplace
	ApproveNftForMarket(t, ct, proposer)
	ApproveNftForMarket(t, ct, acceptor)

	// Propose swap: proposer offers 2 of tokenId "10", wants 1 of tokenId "20", no topUp
	proposePayload := fmt.Sprintf(
		`{"offeredNft":%q,"offeredTokenId":"10","offeredAmount":2,"wantedNft":%q,"wantedTokenId":"20","wantedAmount":1,"topUp":"0","topUpToken":"","expirationBlock":0}`,
		NftContractID, NftContractID,
	)
	result, _, logs := CallMarket(t, ct, "proposeSwap", []byte(proposePayload), nil, proposer, "", true, gas, "")
	AssertEventEmitted(t, logs, "swapProposed")

	created := ParseCreated(result)
	swapId := created.Id

	// Verify swap state
	swap := GetSwap(t, ct, swapId)
	assert.True(t, swap.Active)
	assert.Equal(t, proposer, swap.Proposer)
	assert.Equal(t, NftContractID, swap.OfferedNft)
	assert.Equal(t, "10", swap.OfferedTokenId)
	assert.Equal(t, uint64(2), swap.OfferedAmount)
	assert.Equal(t, NftContractID, swap.WantedNft)
	assert.Equal(t, "20", swap.WantedTokenId)
	assert.Equal(t, uint64(1), swap.WantedAmount)
	assert.Equal(t, "0", swap.TopUp)

	// No token escrow at propose time
	assert.Equal(t, uint64(0), QueryTokenBalance(t, ct, MarketContractAddress))

	// Accept swap
	acceptPayload := fmt.Sprintf(`{"swapId":%d}`, swapId)
	_, _, acceptLogs := CallMarket(t, ct, "acceptSwap", []byte(acceptPayload), nil, acceptor, "", true, gas, "")
	AssertEventEmitted(t, acceptLogs, "swapAccepted")

	// Verify NFTs changed hands
	// Proposer gave 2 of "10" → acceptor, received 1 of "20"
	assert.Equal(t, uint64(3), QueryNftBalance(t, ct, proposer, "10")) // 5-2=3
	assert.Equal(t, uint64(2), QueryNftBalance(t, ct, acceptor, "10")) // 0+2=2
	assert.Equal(t, uint64(2), QueryNftBalance(t, ct, acceptor, "20")) // 3-1=2
	assert.Equal(t, uint64(1), QueryNftBalance(t, ct, proposer, "20")) // 0+1=1

	// No token movement
	assert.Equal(t, uint64(0), QueryTokenBalance(t, ct, MarketContractAddress))
	assert.Equal(t, uint64(0), QueryTokenBalance(t, ct, feeRecipientAddress))

	// Swap is now inactive
	swap2 := GetSwap(t, ct, swapId)
	assert.False(t, swap2.Active)
}

// TestSwapWithTopUpFeeOnly: topUp > 0, escrowed from proposer at accept, fee taken, acceptor gets received−fee, no royalty.
func TestSwapWithTopUpFeeOnly(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct) // 250 bps fee

	proposer := ownerAddress
	acceptor := "hive:alice"

	// Mint NFTs
	MintNft(t, ct, proposer, "10", 5, 100)
	MintNft(t, ct, acceptor, "20", 3, 100)
	ApproveNftForMarket(t, ct, proposer)
	ApproveNftForMarket(t, ct, acceptor)

	// Proposer gets tokens for topUp (10000 units)
	MintAndApproveToken(t, ct, proposer, 10000)

	// Propose with topUp of 10000 tokens
	proposePayload := fmt.Sprintf(
		`{"offeredNft":%q,"offeredTokenId":"10","offeredAmount":2,"wantedNft":%q,"wantedTokenId":"20","wantedAmount":1,"topUp":"10000","topUpToken":%q,"expirationBlock":0}`,
		NftContractID, NftContractID, TokenID,
	)
	result, _, _ := CallMarket(t, ct, "proposeSwap", []byte(proposePayload), nil, proposer, "", true, gas, "")
	created := ParseCreated(result)
	swapId := created.Id

	// No escrow yet at propose time — top-up is only pulled at accept
	assert.Equal(t, uint64(0), QueryTokenBalance(t, ct, MarketContractAddress))
	assert.Equal(t, uint64(10000), QueryTokenBalance(t, ct, proposer))

	// Accept swap — this triggers the topUp escrow
	acceptPayload := fmt.Sprintf(`{"swapId":%d}`, swapId)
	_, _, acceptLogs := CallMarket(t, ct, "acceptSwap", []byte(acceptPayload), nil, acceptor, "", true, gas, "")
	AssertEventEmitted(t, acceptLogs, "swapAccepted")

	// Top-up pulled from proposer: 10000 tokens
	// fee = 10000 * 250 / 10000 = 250 (bps applied to offered NFT's effective fee)
	// No royalty (nil,nil → zero royalty on barter, documented)
	// acceptorPay = 10000 - 250 = 9750
	assert.Equal(t, uint64(250), QueryTokenBalance(t, ct, feeRecipientAddress))
	assert.Equal(t, uint64(9750), QueryTokenBalance(t, ct, acceptor))
	assert.Equal(t, uint64(0), QueryTokenBalance(t, ct, proposer))
	assert.Equal(t, uint64(0), QueryTokenBalance(t, ct, MarketContractAddress))

	// NFTs changed hands
	assert.Equal(t, uint64(3), QueryNftBalance(t, ct, proposer, "10")) // 5-2=3
	assert.Equal(t, uint64(2), QueryNftBalance(t, ct, acceptor, "10")) // 0+2=2
	assert.Equal(t, uint64(2), QueryNftBalance(t, ct, acceptor, "20")) // 3-1=2
	assert.Equal(t, uint64(1), QueryNftBalance(t, ct, proposer, "20")) // 0+1=1
}

// TestSwapDeniedEitherSideBlocked: deny the NFT collection → acceptSwap aborts "Collection is denied".
func TestSwapDeniedEitherSideBlocked(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	proposer := ownerAddress
	acceptor := "hive:alice"

	MintNft(t, ct, proposer, "10", 5, 100)
	MintNft(t, ct, acceptor, "20", 3, 100)
	ApproveNftForMarket(t, ct, proposer)
	ApproveNftForMarket(t, ct, acceptor)

	// Propose swap
	proposePayload := fmt.Sprintf(
		`{"offeredNft":%q,"offeredTokenId":"10","offeredAmount":2,"wantedNft":%q,"wantedTokenId":"20","wantedAmount":1,"topUp":"0","topUpToken":"","expirationBlock":0}`,
		NftContractID, NftContractID,
	)
	result, _, _ := CallMarket(t, ct, "proposeSwap", []byte(proposePayload), nil, proposer, "", true, gas, "")
	swapId := ParseCreated(result).Id

	// Deny the NFT collection — retroactive denylist parity
	denyPayload := fmt.Sprintf(`{"nftContract":"%s"}`, NftContractID)
	CallMarket(t, ct, "denyCollection", []byte(denyPayload), nil, ownerAddress, "", true, gas, "")

	// acceptSwap should abort with "Collection is denied"
	acceptPayload := fmt.Sprintf(`{"swapId":%d}`, swapId)
	CallMarket(t, ct, "acceptSwap", []byte(acceptPayload), nil, acceptor, "", false, gas, "Collection is denied")

	// Nothing moved — NFTs unchanged
	assert.Equal(t, uint64(5), QueryNftBalance(t, ct, proposer, "10"))
	assert.Equal(t, uint64(3), QueryNftBalance(t, ct, acceptor, "20"))
}

// TestSwapCancelByProposer: proposer cancels → act=0, accept then aborts "Swap not active".
func TestSwapCancelByProposer(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	proposer := ownerAddress
	acceptor := "hive:alice"

	MintNft(t, ct, proposer, "10", 5, 100)
	MintNft(t, ct, acceptor, "20", 3, 100)
	ApproveNftForMarket(t, ct, proposer)
	ApproveNftForMarket(t, ct, acceptor)

	// Propose
	proposePayload := fmt.Sprintf(
		`{"offeredNft":%q,"offeredTokenId":"10","offeredAmount":2,"wantedNft":%q,"wantedTokenId":"20","wantedAmount":1,"topUp":"0","topUpToken":"","expirationBlock":0}`,
		NftContractID, NftContractID,
	)
	result, _, _ := CallMarket(t, ct, "proposeSwap", []byte(proposePayload), nil, proposer, "", true, gas, "")
	swapId := ParseCreated(result).Id

	// Cancel by proposer
	cancelPayload := fmt.Sprintf(`{"swapId":%d}`, swapId)
	_, _, cancelLogs := CallMarket(t, ct, "cancelSwap", []byte(cancelPayload), nil, proposer, "", true, gas, "")
	AssertEventEmitted(t, cancelLogs, "swapCancelled")

	// Swap is now inactive
	swap := GetSwap(t, ct, swapId)
	assert.False(t, swap.Active)

	// Accept should now fail
	acceptPayload := fmt.Sprintf(`{"swapId":%d}`, swapId)
	CallMarket(t, ct, "acceptSwap", []byte(acceptPayload), nil, acceptor, "", false, gas, "Swap not active")
}

// TestSwapExpiredAnyoneCancels: after expirationBlock, non-proposer cancelSwap succeeds.
func TestSwapExpiredAnyoneCancels(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	proposer := ownerAddress
	nonProposer := "hive:carol"

	MintNft(t, ct, proposer, "10", 5, 100)
	ApproveNftForMarket(t, ct, proposer)

	// Set block height to 100, propose with expirationBlock=150
	ct.BlockHeight = 100

	proposePayload := fmt.Sprintf(
		`{"offeredNft":%q,"offeredTokenId":"10","offeredAmount":2,"wantedNft":%q,"wantedTokenId":"20","wantedAmount":1,"topUp":"0","topUpToken":"","expirationBlock":150}`,
		NftContractID, NftContractID,
	)
	result, _, _ := CallMarket(t, ct, "proposeSwap", []byte(proposePayload), nil, proposer, "", true, gas, "")
	swapId := ParseCreated(result).Id

	// Non-proposer tries to cancel while NOT expired — should fail
	cancelPayload := fmt.Sprintf(`{"swapId":%d}`, swapId)
	CallMarket(t, ct, "cancelSwap", []byte(cancelPayload), nil, nonProposer, "", false, gas, "Only proposer can cancel swap")

	// Advance past expiration
	ct.BlockHeight = 200

	// Now non-proposer can cancel (expired path)
	_, _, cancelLogs := CallMarket(t, ct, "cancelSwap", []byte(cancelPayload), nil, nonProposer, "", true, gas, "")
	AssertEventEmitted(t, cancelLogs, "swapCancelled")

	// Swap is inactive
	swap := GetSwap(t, ct, swapId)
	assert.False(t, swap.Active)
}

// TestAcceptSwapProposerNoLongerHoldsAborts: proposer transfers offered away post-propose → acceptSwap aborts.
func TestAcceptSwapProposerNoLongerHoldsAborts(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	proposer := ownerAddress
	acceptor := "hive:alice"
	thirdParty := "hive:bob"

	MintNft(t, ct, proposer, "10", 5, 100)
	MintNft(t, ct, acceptor, "20", 3, 100)
	ApproveNftForMarket(t, ct, proposer)
	ApproveNftForMarket(t, ct, acceptor)

	// Propose swap
	proposePayload := fmt.Sprintf(
		`{"offeredNft":%q,"offeredTokenId":"10","offeredAmount":2,"wantedNft":%q,"wantedTokenId":"20","wantedAmount":1,"topUp":"0","topUpToken":"","expirationBlock":0}`,
		NftContractID, NftContractID,
	)
	result, _, _ := CallMarket(t, ct, "proposeSwap", []byte(proposePayload), nil, proposer, "", true, gas, "")
	swapId := ParseCreated(result).Id

	// Proposer transfers ALL 5 offered NFTs away → now holds 0 of tokenId "10"
	transferPayload := fmt.Sprintf(`{"from":"%s","to":"%s","id":"10","amount":5,"data":""}`, proposer, thirdParty)
	CallNft(t, ct, "safeTransferFrom", []byte(transferPayload), nil, proposer, true, gas, "")

	// acceptSwap should abort because proposer no longer holds the offered NFT
	acceptPayload := fmt.Sprintf(`{"swapId":%d}`, swapId)
	CallMarket(t, ct, "acceptSwap", []byte(acceptPayload), nil, acceptor, "", false, gas, "Proposer no longer holds offered NFT")

	// Nothing moved — acceptor's NFT unchanged, no escrow
	assert.Equal(t, uint64(3), QueryNftBalance(t, ct, acceptor, "20"))
	assert.Equal(t, uint64(0), QueryTokenBalance(t, ct, MarketContractAddress))
}
