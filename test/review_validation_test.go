package contract_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Regression tests for the 2026-05-29 team review + the second-pass
// independent review. Each test ASSERTS the post-fix behaviour — a PASS
// means the bug is closed, a FAIL means it has regressed.

// C1 — emergencyWithdraw must not be usable to drain a live escrowed NFT
// (the NFT branch is fully disabled).
func TestReview_C1_EmergencyWithdrawCannotDrainEscrow(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	MintNft(t, ct, ownerAddress, "n1", 1, 1)
	ApproveNftForMarket(t, ct, ownerAddress)

	ct.BlockHeight = 100
	auc := fmt.Sprintf(`{"nftContract":"%s","tokenId":"n1","amount":1,"paymentToken":"hive","auctionType":"english","startPrice":"100","endPrice":"0","startBlock":100,"endBlock":200}`, NftContractID)
	CallMarket(t, ct, "createAuction", []byte(auc), nil, ownerAddress, "", true, gas, "")
	assert.Equal(t, uint64(1), QueryNftBalance(t, ct, MarketContractAddress, "n1"))

	CallMarket(t, ct, "pause", []byte(`{}`), nil, ownerAddress, "", true, gas, "")
	ew := fmt.Sprintf(`{"tokenType":"nft","contract":"%s","tokenId":"n1","amount":"1","to":"hive:attacker"}`, NftContractID)
	CallMarket(t, ct, "emergencyWithdraw", []byte(ew), nil, ownerAddress, "", false, gas, "Emergency NFT withdraw disabled")
	assert.Equal(t, uint64(1), QueryNftBalance(t, ct, MarketContractAddress, "n1"), "escrowed NFT remains in the market")
	assert.Equal(t, uint64(0), QueryNftBalance(t, ct, "hive:attacker", "n1"), "attacker received nothing")
}

// C1b — emergencyWithdraw must also refuse to touch a whitelisted payment
// token (those funds back live offer/auction/swap/rental escrows).
func TestReview_C1b_EmergencyWithdrawBlocksWhitelistedToken(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	CallMarket(t, ct, "pause", []byte(`{}`), nil, ownerAddress, "", true, gas, "")
	ew := fmt.Sprintf(`{"tokenType":"token","contract":"%s","amount":"1"}`, TokenID)
	CallMarket(t, ct, "emergencyWithdraw", []byte(`{"tokenType":"token","contract":"`+TokenID+`","amount":"1","to":"hive:attacker"}`), nil, ownerAddress, "", false, gas, "active payment token")
	_ = ew
}

// H2 — setRoyaltySplits must reject an individual bps that exceeds 5000
// (closes the Σ uint64-overflow door).
func TestReview_H2_RoyaltySplitsOverflowBlocked(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	splits := `{"nftContract":"` + NftContractID + `","splits":[{"recipient":"hive:a","bps":18446744073709548617},{"recipient":"hive:b","bps":3000}]}`
	CallMarket(t, ct, "setRoyaltySplits", []byte(splits), nil, ownerAddress, "", false, gas, "Royalty split bps must be <= 5000")
}

// H3 — a renter can no longer hold two concurrent rentals for the same
// (nftContract, tokenId): the second `rent` aborts, preserving the
// attestation invariant.
func TestReview_H3_RentalCollisionBlocked(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	alice, bob, carol := "hive:alice", "hive:bob", "hive:carol"
	MintNft(t, ct, alice, "ed", 1, 2)
	MintNft(t, ct, bob, "ed", 1, 2)
	ApproveNftForMarket(t, ct, alice)
	ApproveNftForMarket(t, ct, bob)
	MintAndApproveToken(t, ct, carol, 1000)

	lr := func(owner string) {
		p := fmt.Sprintf(`{"nftContract":"%s","tokenId":"ed","amount":1,"paymentToken":"%s","pricePerBlock":"1","minBlocks":1,"maxBlocks":1000}`, NftContractID, TokenID)
		CallMarket(t, ct, "listRental", []byte(p), nil, owner, "", true, gas, "")
	}
	lr(alice)
	lr(bob)

	ct.BlockHeight = 100
	CallMarket(t, ct, "rent", []byte(`{"rentalId":0,"blocks":5}`), nil, carol, "", true, gas, "")
	// Second rent of same (nc,ti) by same renter must be refused.
	CallMarket(t, ct, "rent", []byte(`{"rentalId":1,"blocks":50}`), nil, carol, "", false, gas, "active rental")
}

// M3a — settleAuction is now pause-gated.
func TestReview_M3a_SettleAuctionGatedByPause(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	MintNft(t, ct, ownerAddress, "n1", 1, 1)
	ApproveNftForMarket(t, ct, ownerAddress)
	ct.BlockHeight = 100
	auc := fmt.Sprintf(`{"nftContract":"%s","tokenId":"n1","amount":1,"paymentToken":"hive","auctionType":"english","startPrice":"100","endPrice":"0","startBlock":100,"endBlock":200}`, NftContractID)
	CallMarket(t, ct, "createAuction", []byte(auc), nil, ownerAddress, "", true, gas, "")
	ct.BlockHeight = 201
	CallMarket(t, ct, "pause", []byte(`{}`), nil, ownerAddress, "", true, gas, "")
	CallMarket(t, ct, "settleAuction", []byte(`{"auctionId":0}`), nil, "hive:anyone", "", false, gas, "paused")
}

// M3b — acceptOffer is now pause-gated.
func TestReview_M3b_AcceptOfferGatedByPause(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	MintNft(t, ct, ownerAddress, "n1", 1, 1)
	ApproveNftForMarket(t, ct, ownerAddress)
	buyer := "hive:buyer"
	MintAndApproveToken(t, ct, buyer, 5000)
	off := fmt.Sprintf(`{"nftContract":"%s","tokenId":"n1","amount":1,"paymentToken":"%s","pricePerUnit":"2000"}`, NftContractID, TokenID)
	CallMarket(t, ct, "makeOffer", []byte(off), nil, buyer, "", true, gas, "")
	CallMarket(t, ct, "pause", []byte(`{}`), nil, ownerAddress, "", true, gas, "")
	CallMarket(t, ct, "acceptOffer", []byte(`{"offerId":0,"amount":1}`), nil, ownerAddress, "", false, gas, "paused")
}

// M3c — endRental + endRentalEarly are now pause-gated.
func TestReview_M3c_EndRentalGatedByPause(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	MintNft(t, ct, "hive:alice", "n1", 1, 1)
	ApproveNftForMarket(t, ct, "hive:alice")
	carol := "hive:carol"
	MintAndApproveToken(t, ct, carol, 1000)
	p := fmt.Sprintf(`{"nftContract":"%s","tokenId":"n1","amount":1,"paymentToken":"%s","pricePerBlock":"1","minBlocks":1,"maxBlocks":1000}`, NftContractID, TokenID)
	CallMarket(t, ct, "listRental", []byte(p), nil, "hive:alice", "", true, gas, "")
	ct.BlockHeight = 100
	CallMarket(t, ct, "rent", []byte(`{"rentalId":0,"blocks":5}`), nil, carol, "", true, gas, "")
	ct.BlockHeight = 110
	CallMarket(t, ct, "pause", []byte(`{}`), nil, ownerAddress, "", true, gas, "")
	CallMarket(t, ct, "endRental", []byte(`{"rentalId":0}`), nil, "hive:anyone", "", false, gas, "paused")
	CallMarket(t, ct, "endRentalEarly", []byte(`{"rentalId":0}`), nil, carol, "", false, gas, "paused")
}

// M6 — listToken now enforces the collection denylist.
func TestReview_M6_TokenSaleDenylistEnforced(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	InitAssetToken(t, ct)
	seller := "hive:seller"
	MintAsset(t, ct, seller, 1000)
	ApproveAssetForMarket(t, ct, seller, 1000)
	CallMarket(t, ct, "denyCollection", []byte(`{"nftContract":"`+AssetTokenID+`"}`), nil, ownerAddress, "", true, gas, "")
	list := fmt.Sprintf(`{"tokenContract":"%s","amount":"1000","paymentToken":"%s","pricePerUnit":"2","expirationBlock":0,"startBlock":0}`, AssetTokenID, TokenID)
	CallMarket(t, ct, "listToken", []byte(list), nil, seller, "", false, gas, "Collection is denied")
}

// M7 — buy on a listing whose payment token was de-whitelisted now aborts.
func TestReview_M7_BuyOnDeWhitelistedTokenBlocked(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	MintNft(t, ct, ownerAddress, "n1", 1, 1)
	ApproveNftForMarket(t, ct, ownerAddress)
	buyer := "hive:buyer"
	MintAndApproveToken(t, ct, buyer, 5000)
	list := fmt.Sprintf(`{"nftContract":"%s","tokenId":"n1","amount":1,"paymentToken":"%s","pricePerUnit":"1000"}`, NftContractID, TokenID)
	CallMarket(t, ct, "list", []byte(list), nil, ownerAddress, "", true, gas, "")
	CallMarket(t, ct, "removePaymentToken", []byte(`{"token":"`+TokenID+`"}`), nil, ownerAddress, "", true, gas, "")
	CallMarket(t, ct, "buy", []byte(`{"listingId":0,"amount":1}`), nil, buyer, "", false, gas, "Payment token not allowed")
}

// L3 — changeOwner now validates the proposed owner's account format.
func TestReview_L3_ChangeOwnerRejectsInvalidAccount(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	CallMarket(t, ct, "changeOwner", []byte(`{"newOwner":"NOT A VALID ACCOUNT!!"}`), nil, ownerAddress, "", false, gas, "account contains invalid characters")
}
