package contract_test

import (
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"

	"vsc-node/lib/test_utils"
	contract_session "vsc-node/modules/contract/session"
	"vsc-node/modules/db/vsc/contracts"
	stateEngine "vsc-node/modules/state-processing"

	"github.com/stretchr/testify/assert"
)

var _ = embed.FS{}

const MarketContractID = "market"
const TokenID = "paytoken"
const NftContractID = "nft"
const ownerAddress = "hive:tibfox"
const feeRecipientAddress = "hive:feerecipient"

const MarketContractAddress = "contract:" + MarketContractID

var DefaultTokenInitPayload = []byte(`{"name":"Pay Token","symbol":"PAY","decimals":3,"maxSupply":"1000000000"}`)
var DefaultNftInitPayload = []byte(`{"name":"Magi NFT","symbol":"MNFT","baseUri":"https://api.magi.network/metadata/"}`)

//go:embed artifacts/main.wasm
var MarketWasm []byte

//go:embed artifacts/token.wasm
var TokenWasm []byte

//go:embed artifacts/nft.wasm
var NftWasm []byte

const defaultTimestamp = "2025-09-03T00:00:00"

const gas = uint(500_000_000)

// SetupContractTest creates a fresh test instance with marketplace + token + NFT contracts.
func SetupContractTest() *test_utils.ContractTest {
	CleanBadgerDB()
	ct := test_utils.NewContractTest()
	ct.RegisterContract(MarketContractID, ownerAddress, MarketWasm)
	ct.RegisterContract(TokenID, ownerAddress, TokenWasm)
	ct.RegisterContract(NftContractID, ownerAddress, NftWasm)
	return &ct
}

func CleanBadgerDB() {
	err := os.RemoveAll("data/badger")
	if err != nil {
		panic("failed to remove data/badger")
	}
}

// CallMarket calls the marketplace contract.
func CallMarket(
	t *testing.T,
	ct *test_utils.ContractTest,
	action string,
	payload json.RawMessage,
	intents []contracts.Intent,
	authUser string,
	timestamp string,
	expectedResult bool,
	maxGas uint,
	expectedOutput string,
) (test_utils.ContractTestCallResult, uint, map[string]contract_session.LogOutput) {
	if timestamp == "" {
		timestamp = defaultTimestamp
	}
	return callContract(t, ct, MarketContractID, action, payload, intents, authUser, timestamp, expectedResult, maxGas, expectedOutput)
}

// CallToken calls the payment token contract.
func CallToken(
	t *testing.T,
	ct *test_utils.ContractTest,
	action string,
	payload json.RawMessage,
	intents []contracts.Intent,
	authUser string,
	expectedResult bool,
	maxGas uint,
	expectedOutput string,
) (test_utils.ContractTestCallResult, uint, map[string]contract_session.LogOutput) {
	return callContract(t, ct, TokenID, action, payload, intents, authUser, defaultTimestamp, expectedResult, maxGas, expectedOutput)
}

// CallNft calls the NFT contract.
func CallNft(
	t *testing.T,
	ct *test_utils.ContractTest,
	action string,
	payload json.RawMessage,
	intents []contracts.Intent,
	authUser string,
	expectedResult bool,
	maxGas uint,
	expectedOutput string,
) (test_utils.ContractTestCallResult, uint, map[string]contract_session.LogOutput) {
	return callContract(t, ct, NftContractID, action, payload, intents, authUser, defaultTimestamp, expectedResult, maxGas, expectedOutput)
}

func callContract(
	t *testing.T,
	ct *test_utils.ContractTest,
	contractId string,
	action string,
	payload json.RawMessage,
	intents []contracts.Intent,
	authUser string,
	timestamp string,
	expectedResult bool,
	maxGas uint,
	expectedOutput string,
) (test_utils.ContractTestCallResult, uint, map[string]contract_session.LogOutput) {
	fmt.Printf("[%s] %s %s\n", contractId, action, string(payload))
	cr := ct.Call(stateEngine.TxVscCallContract{
		Caller: authUser,
		Self: stateEngine.TxSelf{
			TxId:                 fmt.Sprintf("%s-%s-tx", contractId, action),
			BlockId:              "block1",
			Index:                0,
			OpIndex:              0,
			Timestamp:            timestamp,
			RequiredAuths:        []string{authUser},
			RequiredPostingAuths: []string{},
		},
		ContractId: contractId,
		Action:     action,
		Payload:    payload,
		RcLimit:    10000,
		Intents:    intents,
	})
	PrintLogs(cr.Logs)
	PrintErrorIfFailed(cr)
	fmt.Printf("return msg: %s\n", cr.Ret)
	fmt.Printf("RC used: %d\n", cr.RcUsed)
	fmt.Printf("gas used: %d\n", cr.GasUsed)

	assert.LessOrEqual(t, cr.GasUsed, maxGas, fmt.Sprintf("Gas %d exceeded limit %d", cr.GasUsed, maxGas))

	if expectedResult {
		assert.True(t, cr.Success, "Contract action failed with "+cr.Ret)
	} else {
		assert.False(t, cr.Success, "Contract action did not fail (as expected)")
	}
	if expectedOutput != "" {
		combined := cr.Ret + cr.ErrMsg
		assert.True(t, strings.Contains(combined, expectedOutput), fmt.Sprintf("Expected output to contain %q but got ret=%q errMsg=%q", expectedOutput, cr.Ret, cr.ErrMsg))
	}
	return cr, cr.GasUsed, cr.Logs
}

func startsWith(s, prefix string) bool {
	if len(prefix) > len(s) {
		return false
	}
	for i := 0; i < len(prefix); i++ {
		if s[i] != prefix[i] {
			return false
		}
	}
	return true
}

func PrintLogs(logs map[string]contract_session.LogOutput) {
	for key, v := range logs {
		fmt.Printf("[%s] %+v\n", key, v)
	}
}

func PrintErrorIfFailed(result test_utils.ContractTestCallResult) {
	if !result.Success {
		fmt.Println(result.ErrMsg)
	}
}

// ===================================
// Setup Helpers
// ===================================

// InitToken initializes the payment token.
func InitToken(t *testing.T, ct *test_utils.ContractTest) {
	CallToken(t, ct, "init", DefaultTokenInitPayload, nil, ownerAddress, true, gas, "")
}

// InitNft initializes the NFT contract.
func InitNft(t *testing.T, ct *test_utils.ContractTest) {
	CallNft(t, ct, "init", DefaultNftInitPayload, nil, ownerAddress, true, gas, "")
}

// InitMarket initializes the marketplace with 2.5% fee (250 bps).
func InitMarket(t *testing.T, ct *test_utils.ContractTest) {
	payload := fmt.Sprintf(`{"feeBps":250,"feeRecipient":"%s"}`, feeRecipientAddress)
	CallMarket(t, ct, "init", []byte(payload), nil, ownerAddress, "", true, gas, "")
}

// InitFullSetup initializes token + NFT + marketplace.
func InitFullSetup(t *testing.T, ct *test_utils.ContractTest) {
	InitToken(t, ct)
	InitNft(t, ct)
	InitMarket(t, ct)
}

// MintNft mints NFTs to a user. If user is not owner, mints to owner then transfers.
func MintNft(t *testing.T, ct *test_utils.ContractTest, to, tokenId string, amount, maxSupply uint64) {
	payload := fmt.Sprintf(`{"to":"%s","id":"%s","amount":%d,"maxSupply":%d}`, to, tokenId, amount, maxSupply)
	CallNft(t, ct, "mint", []byte(payload), nil, ownerAddress, true, gas, "")
}

// MintAndApproveToken mints payment tokens to a user and approves the marketplace to spend them.
func MintAndApproveToken(t *testing.T, ct *test_utils.ContractTest, user string, amount uint64) {
	// Mint to owner
	CallToken(t, ct, "mint", []byte(fmt.Sprintf(`{"amount":"%d"}`, amount)), nil, ownerAddress, true, gas, "")
	// Transfer to user if not owner
	if user != ownerAddress {
		CallToken(t, ct, "transfer", []byte(fmt.Sprintf(`{"to":"%s","amount":"%d"}`, user, amount)), nil, ownerAddress, true, gas, "")
	}
	// Approve marketplace
	CallToken(t, ct, "approve", []byte(fmt.Sprintf(`{"spender":"%s","amount":"%d"}`, MarketContractAddress, amount)), nil, user, true, gas, "")
}

// ApproveNftForMarket sets approval for all on NFT contract for marketplace.
func ApproveNftForMarket(t *testing.T, ct *test_utils.ContractTest, user string) {
	payload := fmt.Sprintf(`{"operator":"%s","approved":true}`, MarketContractAddress)
	CallNft(t, ct, "setApprovalForAll", []byte(payload), nil, user, true, gas, "")
}

// ===================================
// Result Parsing Helpers
// ===================================

type ListingResult struct {
	ListingId       uint64 `json:"listingId"`
	Seller          string `json:"seller"`
	NftContract     string `json:"nftContract"`
	TokenId         string `json:"tokenId"`
	Amount          uint64 `json:"amount"`
	PricePerUnit    string `json:"pricePerUnit"`
	PaymentToken    string `json:"paymentToken"`
	Active          bool   `json:"active"`
	ExpirationBlock uint64 `json:"expirationBlock"`
	FeeBps          uint64 `json:"feeBps"`
	RoyaltyBps      uint64 `json:"royaltyBps"`
}

func ParseListing(result test_utils.ContractTestCallResult) ListingResult {
	var resp ListingResult
	json.Unmarshal([]byte(result.Ret), &resp)
	return resp
}

type OfferResult struct {
	OfferId         uint64 `json:"offerId"`
	Buyer           string `json:"buyer"`
	NftContract     string `json:"nftContract"`
	TokenId         string `json:"tokenId"`
	Amount          uint64 `json:"amount"`
	PricePerUnit    uint64 `json:"pricePerUnit"`
	PaymentToken    string `json:"paymentToken"`
	Active          bool   `json:"active"`
	ExpirationBlock uint64 `json:"expirationBlock"`
	FeeBps          uint64 `json:"feeBps"`
	RoyaltyBps      uint64 `json:"royaltyBps"`
	IsCollection    bool   `json:"isCollection"`
}

func ParseOffer(result test_utils.ContractTestCallResult) OfferResult {
	var resp OfferResult
	json.Unmarshal([]byte(result.Ret), &resp)
	return resp
}

func ParseOwner(result test_utils.ContractTestCallResult) string {
	var resp struct {
		Owner string `json:"owner"`
	}
	json.Unmarshal([]byte(result.Ret), &resp)
	return resp.Owner
}

func ParsePaused(result test_utils.ContractTestCallResult) bool {
	var resp struct {
		Paused bool `json:"paused"`
	}
	json.Unmarshal([]byte(result.Ret), &resp)
	return resp.Paused
}

type InfoResult struct {
	Owner              string `json:"owner"`
	FeeBps             uint64 `json:"feeBps"`
	FeeRecipient       string `json:"feeRecipient"`
	Paused             bool   `json:"paused"`
	MinOffer           uint64 `json:"minOffer"`
	MinBidIncrementBps uint64 `json:"minBidIncrementBps"`
	AntiSnipeBlocks    uint64 `json:"antiSnipeBlocks"`
}

type AuctionResult struct {
	AuctionId    uint64 `json:"auctionId"`
	Seller       string `json:"seller"`
	NftContract  string `json:"nftContract"`
	TokenId      string `json:"tokenId"`
	Amount       uint64 `json:"amount"`
	PaymentToken string `json:"paymentToken"`
	AuctionType  string `json:"auctionType"`
	StartPrice   uint64 `json:"startPrice"`
	EndPrice     uint64 `json:"endPrice"`
	StartBlock   uint64 `json:"startBlock"`
	EndBlock     uint64 `json:"endBlock"`
	HighBidder   string `json:"highBidder"`
	HighBid      uint64 `json:"highBid"`
	Active       bool   `json:"active"`
	Settled      bool   `json:"settled"`
	FeeBps       uint64 `json:"feeBps"`
	RoyaltyBps   uint64 `json:"royaltyBps"`
}

func ParseAuction(result test_utils.ContractTestCallResult) AuctionResult {
	var resp AuctionResult
	json.Unmarshal([]byte(result.Ret), &resp)
	return resp
}

type RoyaltyResult struct {
	NftContract      string `json:"nftContract"`
	RoyaltyBps       uint64 `json:"royaltyBps"`
	RoyaltyRecipient string `json:"royaltyRecipient"`
}

func ParseRoyalty(result test_utils.ContractTestCallResult) RoyaltyResult {
	var resp RoyaltyResult
	json.Unmarshal([]byte(result.Ret), &resp)
	return resp
}

func ParseInfo(result test_utils.ContractTestCallResult) InfoResult {
	var resp InfoResult
	json.Unmarshal([]byte(result.Ret), &resp)
	return resp
}

type CreatedResult struct {
	Success bool   `json:"success"`
	Id      uint64 `json:"id"`
}

func ParseCreated(result test_utils.ContractTestCallResult) CreatedResult {
	var resp CreatedResult
	json.Unmarshal([]byte(result.Ret), &resp)
	return resp
}

func ParseBalance(result test_utils.ContractTestCallResult) uint64 {
	// Try string balance first (magi_token returns string), then numeric (magi_nft returns uint64)
	var respStr struct {
		Balance string `json:"balance"`
	}
	json.Unmarshal([]byte(result.Ret), &respStr)
	if respStr.Balance != "" {
		val, _ := strconv.ParseUint(respStr.Balance, 10, 64)
		return val
	}
	var resp struct {
		Balance uint64 `json:"balance"`
	}
	json.Unmarshal([]byte(result.Ret), &resp)
	return resp.Balance
}

// QueryTokenBalance queries balanceOf on the payment token contract.
func QueryTokenBalance(t *testing.T, ct *test_utils.ContractTest, account string) uint64 {
	result, _, _ := CallToken(t, ct, "balanceOf",
		[]byte(fmt.Sprintf(`{"account":"%s"}`, account)), nil, "hive:anyone", true, gas, "")
	return ParseBalance(result)
}

// QueryNftBalance queries balanceOf on the NFT contract.
func QueryNftBalance(t *testing.T, ct *test_utils.ContractTest, account, tokenId string) uint64 {
	result, _, _ := CallNft(t, ct, "balanceOf",
		[]byte(fmt.Sprintf(`{"account":"%s","id":"%s"}`, account, tokenId)), nil, "hive:anyone", true, gas, "")
	return ParseBalance(result)
}

// ===================================
// Event Verification Helpers
// ===================================

func FindEventsInLogs(logs map[string]contract_session.LogOutput, eventType string) []string {
	var found []string
	for _, output := range logs {
		for _, entry := range output.Logs {
			if strings.Contains(entry, `"type":"`+eventType+`"`) {
				found = append(found, entry)
			}
		}
	}
	return found
}

func AssertEventEmitted(t *testing.T, logs map[string]contract_session.LogOutput, eventType string) {
	t.Helper()
	events := FindEventsInLogs(logs, eventType)
	assert.NotEmpty(t, events, "Expected event '%s' to be emitted", eventType)
}

func AssertEventContains(t *testing.T, logs map[string]contract_session.LogOutput, eventType, substring string) {
	t.Helper()
	events := FindEventsInLogs(logs, eventType)
	assert.NotEmpty(t, events, "Expected event '%s' to be emitted", eventType)
	for _, e := range events {
		if strings.Contains(e, substring) {
			return
		}
	}
	t.Errorf("Event '%s' found but none contain '%s'. Events: %v", eventType, substring, events)
}
