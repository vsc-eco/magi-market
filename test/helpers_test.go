package contract_test

import (
	"embed"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"

	"vsc-node/lib/test_utils"
	contract_session "vsc-node/modules/contract/session"
	"vsc-node/modules/db/vsc/contracts"
	ledgerDb "vsc-node/modules/db/vsc/ledger"
	stateEngine "vsc-node/modules/state-processing"

	"github.com/stretchr/testify/assert"
)

var _ = embed.FS{}

const MarketContractID = "market"
const TokenID = "paytoken"

// AssetTokenID is a 2nd magi_token instance used as the SELLABLE asset in
// token-for-token sale tests (distinct from the payment token).
const AssetTokenID = "assettoken"
const NftContractID = "nft"

// FeeTokenID is the fee-on-transfer mock's contract id. The marketplace passes
// the configured paymentToken string verbatim as the cross-contract call
// target id (no "contract:" prefix stripping in go-vsc-node's
// GetContractFromDb), so the contract must be REGISTERED under the exact
// "contract:feetoken" string the tests use as the paymentToken.
const FeeTokenID = "contract:feetoken"

// UtxoMockID is the UTXO-mapping-style mock's contract id. Like the fee token
// it is registered under the exact "contract:utxomock" string the tests pass
// verbatim as the marketplace paymentToken (no "contract:" prefix stripping in
// go-vsc-node). magi-market raw-reads its `a-<acct>` BE-u64 balance state; the
// mock has NO balanceOf entrypoint, proving the raw-read path is exercised.
const UtxoMockID = "contract:utxomock"

// DexMockID is the DEX pool mock's contract id. Used as paymentToken in F2
// tests so that escrowIn (which calls transferFrom on the paymentToken) works
// against dexmock's ledger, and as the DEX pool address for swap calls.
// The mock's a-<acct> BE-u64 storage is identical to utxomock so
// magi-market's raw-read tokenBalanceOf works for balance-delta accounting.
const DexMockID = "contract:dexmock"

// MintNftMockID is the editioned-NFT mock's contract id. Models the
// magi_nft-contract post-feature delegated-mint ABI documented in spec
// 2026-05-18-editioned-nft-define-delegated-mint-design.md. Used by G1/G2
// mint-spot tests to prove the market side against the documented ABI without
// requiring the real nft contract to implement the feature first.
const MintNftMockID = "mintnftmock"

// CallerMockID is a contract that calls the market on a user's behalf. It
// exists to prove buyFromBucket refuses contract callers, which is what closes
// the retry-on-loss attack on random draws.
const CallerMockID = "callermock"

// HostileNftID is a collection that misbehaves during delivery — it can refuse
// a transfer outright, or read the market's own state from inside one. Both are
// needed to test claims that a well-behaved collection can never exercise: that
// a failed transfer aborts the purchase, and that the market writes its state
// BEFORE calling out (CEI).
const HostileNftID = "hostilenft"

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

//go:embed artifacts/feetoken.wasm
var FeeTokenWasm []byte

//go:embed artifacts/utxomock.wasm
var UtxoMockWasm []byte

//go:embed artifacts/dexmock.wasm
var DexMockWasm []byte

//go:embed artifacts/mintnftmock.wasm
var MintNftMockWasm []byte

//go:embed artifacts/callermock.wasm
var CallerMockWasm []byte

//go:embed artifacts/hostilenft.wasm
var HostileNftWasm []byte

const defaultTimestamp = "2025-09-03T00:00:00"

const gas = uint(500_000_000)

// bigGas is for fixture-building and large-bucket calls. maxGas in this harness
// is an assert.LessOrEqual AFTER the call, not an execution limit — RcLimit is
// the only budget that actually rejects anything — so a fixture that mints 500
// NFTs trips the default assertion without saying anything about whether the
// contract is affordable on chain.
const bigGas = uint(4_000_000_000)

// SetupContractTest creates a fresh test instance with marketplace + token + NFT contracts.
func SetupContractTest() *test_utils.ContractTest {
	CleanBadgerDB()
	ct := test_utils.NewContractTest()
	ct.RegisterContract(MarketContractID, ownerAddress, MarketWasm)
	ct.RegisterContract(TokenID, ownerAddress, TokenWasm)
	ct.RegisterContract(AssetTokenID, ownerAddress, TokenWasm)
	ct.RegisterContract(NftContractID, ownerAddress, NftWasm)
	ct.RegisterContract(FeeTokenID, ownerAddress, FeeTokenWasm)
	ct.RegisterContract(UtxoMockID, ownerAddress, UtxoMockWasm)
	ct.RegisterContract(DexMockID, ownerAddress, DexMockWasm)
	ct.RegisterContract(MintNftMockID, ownerAddress, MintNftMockWasm)
	ct.RegisterContract(CallerMockID, ownerAddress, CallerMockWasm)
	ct.RegisterContract(HostileNftID, ownerAddress, HostileNftWasm)
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

// CallHostileNft drives the misbehaving collection mock.
func CallHostileNft(
	t *testing.T,
	ct *test_utils.ContractTest,
	action string,
	payload json.RawMessage,
	authUser string,
	expectedResult bool,
	expectedOutput string,
) (test_utils.ContractTestCallResult, uint, map[string]contract_session.LogOutput) {
	return callContract(t, ct, HostileNftID, action, payload, nil, authUser, defaultTimestamp, expectedResult, gas, expectedOutput)
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
			TxId: fmt.Sprintf("%s-%s-tx", contractId, action),
			// A realistic Hive block id: 40 hex chars whose first 8 are the
			// block number. Contracts that derive randomness from block.id
			// (buckets) validate its shape, and a short mock id would both fail
			// that check and hide how little entropy a truncated id carries.
			BlockId:              "05a995bf3a096cb001d6f541f46e6f67394cea62",
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
// After init, the payment-token whitelist is auto-seeded with native
// HBD/HIVE only (per the 2026-05-27 audit hardening). This helper also
// runs SeedTestPaymentTokens so the rest of the test suite, which
// passes `TokenID`/`FeeTokenID`/`UtxoMockID`/`DexMockID` as
// paymentToken, keeps working. Mirrors production deploy where the
// admin runs `addPaymentToken` per custom token after init.
func InitMarket(t *testing.T, ct *test_utils.ContractTest) {
	payload := fmt.Sprintf(`{"feeBps":250,"feeRecipient":"%s"}`, feeRecipientAddress)
	CallMarket(t, ct, "init", []byte(payload), nil, ownerAddress, "", true, gas, "")
	SeedTestPaymentTokens(t, ct)
}

// SeedTestPaymentTokens whitelists every mock-token contract id the test
// suite uses as a paymentToken. Init-bypassing tests (those that build
// their own init payload directly) should call this AFTER their custom
// init to re-establish the standard paymentToken set.
func SeedTestPaymentTokens(t *testing.T, ct *test_utils.ContractTest) {
	for _, tok := range []string{TokenID, AssetTokenID, FeeTokenID, UtxoMockID, DexMockID} {
		add := fmt.Sprintf(`{"token":"%s"}`, tok)
		CallMarket(t, ct, "addPaymentToken", []byte(add), nil, ownerAddress, "", true, gas, "")
	}
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

// FundRc raises an account's RC budget for the rest of the test.
//
// RC is not per call. The harness accumulates consumption per ACCOUNT across
// the whole test, and an account's budget is its HBD ledger balance plus a 10k
// free tier — so a test that mints, approves and lists from one account
// silently spends the same 10k it wanted to measure with.
//
// The deposit target must be a REAL Hive account name: the ledger checks it
// against Hive's rules and a length limit (the full "hive:name" under 17
// characters), and on a mismatch it quietly credits hive:contract-test-account
// instead — leaving the intended account on the free tier while this call
// appears to succeed. That silence cost a long debugging detour, so the credit
// is verified here rather than assumed.
func FundRc(t *testing.T, ct *test_utils.ContractTest, account string, amount int64) {
	t.Helper()
	before := ct.GetAvailableRCs(account)
	ct.Deposit(account, amount, ledgerDb.AssetHbd)
	after := ct.GetAvailableRCs(account)
	if after <= before {
		t.Fatalf("FundRc(%q) credited nothing (rc %d -> %d). The ledger redirects "+
			"deposits whose target is not a valid Hive account — the full %q must be "+
			"lowercase, alphanumeric and under 17 characters (it is %d).",
			account, before, after, account, len(account))
	}
}

// MintNftBatch mints many token ids, splitting them across as many calls as the
// per-call rcLimit allows.
//
// Minting one id per call is what made big fixtures unaffordable. The NFT
// contract accepts 256 ids per batch, but that is a batch-size cap, not a cost
// cap: 250 ids in one call needs well over the 10000 RC a real transaction
// gets. So batch generously, but stay inside one transaction's budget.
const mintBatchChunk = 20

func MintNftBatch(t *testing.T, ct *test_utils.ContractTest, to string, tokenIds []string, amount, maxSupply uint64) {
	for off := 0; off < len(tokenIds); off += mintBatchChunk {
		end := off + mintBatchChunk
		if end > len(tokenIds) {
			end = len(tokenIds)
		}
		ids := "["
		amts := "["
		sup := "["
		for i, id := range tokenIds[off:end] {
			if i > 0 {
				ids += ","
				amts += ","
				sup += ","
			}
			ids += `"` + id + `"`
			amts += fmt.Sprintf("%d", amount)
			sup += fmt.Sprintf("%d", maxSupply)
		}
		ids += "]"
		amts += "]"
		sup += "]"
		payload := fmt.Sprintf(`{"to":"%s","ids":%s,"amounts":%s,"maxSupplies":%s}`, to, ids, amts, sup)
		CallNft(t, ct, "mintBatch", []byte(payload), nil, ownerAddress, true, bigGas, "")
	}
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
	PricePerUnit    string `json:"pricePerUnit"`
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
	MinOffer           string `json:"minOffer"`
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
	StartPrice   string `json:"startPrice"`
	EndPrice     string `json:"endPrice"`
	StartBlock   uint64 `json:"startBlock"`
	EndBlock     uint64 `json:"endBlock"`
	HighBidder   string `json:"highBidder"`
	HighBid      string `json:"highBid"`
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
// NftBalanceState reads a holder's balance straight from the NFT contract's
// state instead of calling balanceOf.
//
// Every QueryNftBalance is a contract call billed to one shared account on the
// 10k free tier, so a test with a few hundred assertions exhausts it and starts
// failing on the ASSERTIONS rather than on anything it meant to test. A state
// read costs nothing. Use it where the assertion count is large; prefer the
// contract call where the read itself is part of what is under test.
//
// magi_nft stores balances as raw little-endian bytes — a balance of 4 is the
// single byte 0x04, not "4" — which is what the market's decodeNftU64 expects.
func NftBalanceState(ct *test_utils.ContractTest, account, tokenId string) uint64 {
	raw := ct.StateGet(NftContractID, "bal|"+account+"|"+tokenId)
	b := []byte(raw)
	if len(b) == 0 || len(b) > 8 {
		return 0
	}
	var buf [8]byte
	copy(buf[:], b)
	return binary.LittleEndian.Uint64(buf[:])
}

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
