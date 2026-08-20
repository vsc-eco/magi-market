package contract_test

import (
	"fmt"
	"testing"

	"vsc-node/lib/test_utils"

	"github.com/stretchr/testify/assert"
)

// setupMixedTokenSweep lists two NFTs from the SAME collection at the same
// price in two DIFFERENT payment tokens (listing 0 = TokenID, listing 1 =
// AssetTokenID) and funds the buyer in both.
func setupMixedTokenSweep(t *testing.T, ct *test_utils.ContractTest) (buyer string) {
	t.Helper()
	seller := ownerAddress
	buyer = "hive:buyer"

	MintNft(t, ct, seller, "30", 1, 10)
	MintNft(t, ct, seller, "31", 1, 10)
	ApproveNftForMarket(t, ct, seller)

	MintAndApproveToken(t, ct, buyer, 10000)
	MintAsset(t, ct, buyer, 10000)
	callAsset(t, ct, "approve",
		fmt.Sprintf(`{"spender":"%s","amount":"10000"}`, MarketContractAddress), buyer, true, "")

	CallMarket(t, ct, "list", []byte(fmt.Sprintf(
		`{"nftContract":"%s","tokenId":"30","amount":1,"paymentToken":"%s","pricePerUnit":"1000"}`,
		NftContractID, TokenID)), nil, seller, "", true, gas, "")
	CallMarket(t, ct, "list", []byte(fmt.Sprintf(
		`{"nftContract":"%s","tokenId":"31","amount":1,"paymentToken":"%s","pricePerUnit":"1000"}`,
		NftContractID, AssetTokenID)), nil, seller, "", true, gas, "")
	return buyer
}

// TestFloorSweepRejectsForeignPaymentToken is the counterpart to
// TestFloorSweepRejectsForeignCollection.
//
// `maxTotal` is a bare integer with no currency of its own, and each listing
// is paid for in whatever token it was priced in. Summing across currencies
// therefore produces a number that bounds nothing: before this guard, a cap
// of 2000 authorised 1000 of one asset AND 1000 of another, and both were
// pulled.
func TestFloorSweepRejectsForeignPaymentToken(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	InitAssetToken(t, ct)
	buyer := setupMixedTokenSweep(t, ct)

	payBefore := QueryTokenBalance(t, ct, buyer)
	assetBefore := QueryAssetBalance(t, ct, buyer)

	// No paymentToken given: it is taken from the first listing, so listing 1
	// is the odd one out.
	CallMarket(t, ct, "sweep", []byte(fmt.Sprintf(
		`{"nftContract":"%s","listingIds":[0,1],"maxTotal":"2000"}`, NftContractID)),
		nil, buyer, "", false, gas, "Listing not in payment token")

	assert.Equal(t, payBefore, QueryTokenBalance(t, ct, buyer), "no paytoken may be pulled")
	assert.Equal(t, assetBefore, QueryAssetBalance(t, ct, buyer), "no assettoken may be pulled")

	for _, id := range []uint64{0, 1} {
		r, _, _ := CallMarket(t, ct, "getListing",
			[]byte(fmt.Sprintf(`{"listingId":%d}`, id)), nil, "hive:anyone", "", true, gas, "")
		assert.True(t, ParseListing(r).Active, "listing %d must survive the failed sweep", id)
	}
}

// An explicit paymentToken is enforced too — including against the FIRST
// listing, which is the one the implicit path would have trusted.
func TestFloorSweepRejectsExplicitPaymentTokenMismatch(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	InitAssetToken(t, ct)
	buyer := setupMixedTokenSweep(t, ct)

	CallMarket(t, ct, "sweep", []byte(fmt.Sprintf(
		`{"nftContract":"%s","listingIds":[0],"maxTotal":"2000","paymentToken":"%s"}`,
		NftContractID, AssetTokenID)),
		nil, buyer, "", false, gas, "Listing not in payment token")
}

// The legitimate case still works, and the swept event now says which asset
// the total is denominated in — without it the number is not interpretable
// by anything downstream, the indexer included.
func TestFloorSweepSingleTokenStillWorks(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	InitAssetToken(t, ct)
	buyer := setupMixedTokenSweep(t, ct)

	payBefore := QueryTokenBalance(t, ct, buyer)
	assetBefore := QueryAssetBalance(t, ct, buyer)

	_, _, logs := CallMarket(t, ct, "sweep", []byte(fmt.Sprintf(
		`{"nftContract":"%s","listingIds":[0],"maxTotal":"2000","paymentToken":"%s"}`,
		NftContractID, TokenID)),
		nil, buyer, "", true, gas, "")

	assert.Equal(t, uint64(1000), payBefore-QueryTokenBalance(t, ct, buyer))
	assert.Equal(t, assetBefore, QueryAssetBalance(t, ct, buyer), "the other asset is untouched")
	assert.Contains(t, fmt.Sprint(logs), fmt.Sprintf(`"paymentToken":"%s"`, TokenID),
		"swept event must name the asset its total is in")
}
