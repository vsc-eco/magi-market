package contract_test

import (
	"fmt"
	"testing"

	"vsc-node/lib/test_utils"

	"github.com/stretchr/testify/assert"
)

// ---- asset-token helpers (a 2nd magi_token instance, AssetTokenID) ----

func callAsset(t *testing.T, ct *test_utils.ContractTest, action, payload, authUser string, expectOk bool, expOut string) test_utils.ContractTestCallResult {
	r, _, _ := callContract(t, ct, AssetTokenID, action, []byte(payload), nil, authUser, defaultTimestamp, expectOk, gas, expOut)
	return r
}

func InitAssetToken(t *testing.T, ct *test_utils.ContractTest) {
	callAsset(t, ct, "init", string(DefaultTokenInitPayload), ownerAddress, true, "")
}

// MintAsset mints `amount` of the asset token to `to` (mint to owner, then
// transfer if `to` differs — mirrors MintAndApproveToken's pattern).
func MintAsset(t *testing.T, ct *test_utils.ContractTest, to string, amount uint64) {
	callAsset(t, ct, "mint", fmt.Sprintf(`{"amount":"%d"}`, amount), ownerAddress, true, "")
	if to != ownerAddress {
		callAsset(t, ct, "transfer", fmt.Sprintf(`{"to":"%s","amount":"%d"}`, to, amount), ownerAddress, true, "")
	}
}

// ApproveAssetForMarket: `user` grants the marketplace an ERC-20 allowance
// on the asset token (spender = the market's on-chain identity).
func ApproveAssetForMarket(t *testing.T, ct *test_utils.ContractTest, user string, amount uint64) {
	callAsset(t, ct, "approve", fmt.Sprintf(`{"spender":"%s","amount":"%d"}`, MarketContractAddress, amount), user, true, "")
}

func QueryAssetBalance(t *testing.T, ct *test_utils.ContractTest, account string) uint64 {
	return ParseBalance(callAsset(t, ct, "balanceOf", fmt.Sprintf(`{"account":"%s"}`, account), "hive:anyone", true, ""))
}

// ---- tests ----

// TestTokenSaleHappyPath: list 1000 of an ERC-20 asset token priced in the
// payment token; a buyer buys 400. Asset moves seller→buyer via the
// allowance; payment splits fee/seller; market residual zero.
func TestTokenSaleHappyPath(t *testing.T) {
	ct := SetupContractTest()
	InitToken(t, ct)
	InitAssetToken(t, ct)
	InitMarket(t, ct) // feeBps 250 (2.5%)

	seller := ownerAddress
	buyer := "hive:buyer"

	MintAsset(t, ct, seller, 1000)
	ApproveAssetForMarket(t, ct, seller, 1000)

	listRes, _, _ := CallMarket(t, ct, "listToken",
		[]byte(fmt.Sprintf(`{"tokenContract":"%s","amount":"1000","paymentToken":"%s","pricePerUnit":"2","expirationBlock":0,"startBlock":0}`, AssetTokenID, TokenID)),
		nil, seller, "", true, gas, "")
	assert.Equal(t, uint64(0), ParseCreated(listRes).Id)

	MintAndApproveToken(t, ct, buyer, 100000)

	buyerPayBefore := QueryTokenBalance(t, ct, buyer)
	sellerPayBefore := QueryTokenBalance(t, ct, seller)
	feeBefore := QueryTokenBalance(t, ct, feeRecipientAddress)

	// Buy 400 → total = 2*400 = 800; fee = floor(800*250/10000)=20; seller 780.
	CallMarket(t, ct, "buyToken", []byte(`{"listingId":0,"amount":"400"}`), nil, buyer, "", true, gas, "")

	assert.Equal(t, uint64(400), QueryAssetBalance(t, ct, buyer), "buyer received 400 asset")
	assert.Equal(t, uint64(600), QueryAssetBalance(t, ct, seller), "seller has 600 asset left")
	assert.Equal(t, uint64(800), buyerPayBefore-QueryTokenBalance(t, ct, buyer), "buyer paid 800")
	assert.Equal(t, uint64(780), QueryTokenBalance(t, ct, seller)-sellerPayBefore, "seller got 780 (800-20 fee)")
	assert.Equal(t, uint64(20), QueryTokenBalance(t, ct, feeRecipientAddress)-feeBefore, "fee recipient got 20")
	assert.Equal(t, uint64(0), QueryTokenBalance(t, ct, MarketContractAddress), "market payment residual 0")
	assert.Equal(t, uint64(0), QueryAssetBalance(t, ct, MarketContractAddress), "market asset residual 0")

	gl, _, _ := CallMarket(t, ct, "getTokenListing", []byte(`{"listingId":0}`), nil, "hive:anyone", "", true, gas, "")
	assert.Contains(t, gl.Ret, `"amount":"600"`)
	assert.Contains(t, gl.Ret, `"active":true`)
}

func TestTokenSaleInsufficientBalanceToList(t *testing.T) {
	ct := SetupContractTest()
	InitToken(t, ct)
	InitAssetToken(t, ct)
	InitMarket(t, ct)
	seller := ownerAddress
	MintAsset(t, ct, seller, 100)
	ApproveAssetForMarket(t, ct, seller, 500)
	CallMarket(t, ct, "listToken",
		[]byte(fmt.Sprintf(`{"tokenContract":"%s","amount":"500","paymentToken":"%s","pricePerUnit":"1","expirationBlock":0,"startBlock":0}`, AssetTokenID, TokenID)),
		nil, seller, "", false, gas, "Insufficient token balance to list")
}

func TestTokenSaleDelist(t *testing.T) {
	ct := SetupContractTest()
	InitToken(t, ct)
	InitAssetToken(t, ct)
	InitMarket(t, ct)
	seller := ownerAddress
	MintAsset(t, ct, seller, 50)
	ApproveAssetForMarket(t, ct, seller, 50)
	CallMarket(t, ct, "listToken",
		[]byte(fmt.Sprintf(`{"tokenContract":"%s","amount":"50","paymentToken":"%s","pricePerUnit":"1","expirationBlock":0,"startBlock":0}`, AssetTokenID, TokenID)),
		nil, seller, "", true, gas, "")
	// Non-seller cannot delist.
	CallMarket(t, ct, "delistToken", []byte(`{"listingId":0}`), nil, "hive:mallory", "", false, gas, "Only seller can delist")
	// Seller delists.
	CallMarket(t, ct, "delistToken", []byte(`{"listingId":0}`), nil, seller, "", true, gas, "")
	gl, _, _ := CallMarket(t, ct, "getTokenListing", []byte(`{"listingId":0}`), nil, "hive:anyone", "", true, gas, "")
	assert.Contains(t, gl.Ret, `"active":false`)
}

// TestTokenSaleRevokedAllowanceRefunds: after listing, the seller revokes
// the asset allowance. buyToken's transferFrom aborts → whole tx reverts →
// the buyer's payment is fully refunded (no orphaned funds).
func TestTokenSaleRevokedAllowanceRefunds(t *testing.T) {
	ct := SetupContractTest()
	InitToken(t, ct)
	InitAssetToken(t, ct)
	InitMarket(t, ct)
	seller := ownerAddress
	buyer := "hive:buyer"

	MintAsset(t, ct, seller, 1000)
	ApproveAssetForMarket(t, ct, seller, 1000)
	CallMarket(t, ct, "listToken",
		[]byte(fmt.Sprintf(`{"tokenContract":"%s","amount":"1000","paymentToken":"%s","pricePerUnit":"2","expirationBlock":0,"startBlock":0}`, AssetTokenID, TokenID)),
		nil, seller, "", true, gas, "")
	MintAndApproveToken(t, ct, buyer, 100000)

	// Seller revokes the allowance.
	ApproveAssetForMarket(t, ct, seller, 0)

	buyerBefore := QueryTokenBalance(t, ct, buyer)
	CallMarket(t, ct, "buyToken", []byte(`{"listingId":0,"amount":"100"}`), nil, buyer, "", false, gas, "")
	assert.Equal(t, buyerBefore, QueryTokenBalance(t, ct, buyer), "buyer fully refunded on revoked-allowance revert")
	assert.Equal(t, uint64(0), QueryAssetBalance(t, ct, buyer), "buyer received no asset")
}
