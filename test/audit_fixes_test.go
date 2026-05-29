package contract_test

// Regression coverage for the six-phase security audit patches applied on
// 2026-05-27 (see memory note `magi_market_tokenid_injection_fix.md`).
// These tests do NOT cover re-entry behaviour — that requires a malicious
// callback mock contract, which the test harness doesn't include yet.
// They DO cover every other gate the audits added: input validators,
// slippage check, configuration floors, and existing-contract regression
// for the tokenId-injection fix.

import (
	"fmt"
	"testing"
)

// ===================================
// Pass 1 (tokenId injection) — regression
// ===================================

// listMintSpots used to be the only entrypoint that rejected JSON-breaking
// tokenIds; the audit extended `assertValidTokenId` to every user-supplied
// tokenId site. Verify each rejects a quote-bearing id.
func TestAuditValidTokenIdEnforcedOnList(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	seller := ownerAddress
	// magi_nft only blocks `|`, so an attacker CAN mint with a quote-bearing
	// id. The fix lives at market-side: doList must reject.
	MintNft(t, ct, seller, `pwn","from":"hive:victim`, 1, 1)
	ApproveNftForMarket(t, ct, seller)

	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"pwn\",\"from\":\"hive:victim","amount":1,"paymentToken":"%s","pricePerUnit":"100"}`, NftContractID, TokenID)
	CallMarket(t, ct, "list", []byte(payload), nil, seller, "", false, gas, "tokenId contains invalid characters")
}

func TestAuditValidTokenIdEnforcedOnMakeOffer(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	buyer := "hive:buyer"
	MintAndApproveToken(t, ct, buyer, 10000)

	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"pwn\",\"from\":\"hive:victim","amount":1,"paymentToken":"%s","pricePerUnit":"100"}`, NftContractID, TokenID)
	CallMarket(t, ct, "makeOffer", []byte(payload), nil, buyer, "", false, gas, "tokenId contains invalid characters")
}

func TestAuditValidTokenIdEnforcedOnCreateAuction(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	seller := ownerAddress
	MintNft(t, ct, seller, `pwn","from":"hive:victim`, 1, 1)
	ApproveNftForMarket(t, ct, seller)

	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"pwn\",\"from\":\"hive:victim","amount":1,"paymentToken":"%s","auctionType":"english","startPrice":"100","endBlock":100}`, NftContractID, TokenID)
	CallMarket(t, ct, "createAuction", []byte(payload), nil, seller, "", false, gas, "tokenId contains invalid characters")
}

// ===================================
// Pass 5 (game-theoretic) — slippage guard on Buy
// ===================================

// MaxTotalPrice is an optional field on BuyPayload — present and tight
// enough to abort if the price moved.
func TestAuditBuyMaxTotalPriceTooLow(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	seller := ownerAddress
	buyer := "hive:buyer"
	MintNft(t, ct, seller, "1", 5, 100)
	ApproveNftForMarket(t, ct, seller)
	MintAndApproveToken(t, ct, buyer, 10000)

	listPayload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","pricePerUnit":"100"}`, NftContractID, TokenID)
	CallMarket(t, ct, "list", []byte(listPayload), nil, seller, "", true, gas, "")

	// totalCost = 100 * 2 = 200. MaxTotalPrice=199 must abort.
	buyPayload := `{"listingId":0,"amount":2,"maxTotalPrice":"199"}`
	CallMarket(t, ct, "buy", []byte(buyPayload), nil, buyer, "", false, gas, "Total cost exceeds buyer's MaxTotalPrice")
}

// MaxTotalPrice sufficient — buy succeeds.
func TestAuditBuyMaxTotalPriceSufficient(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	seller := ownerAddress
	buyer := "hive:buyer"
	MintNft(t, ct, seller, "1", 5, 100)
	ApproveNftForMarket(t, ct, seller)
	MintAndApproveToken(t, ct, buyer, 10000)

	listPayload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","pricePerUnit":"100"}`, NftContractID, TokenID)
	CallMarket(t, ct, "list", []byte(listPayload), nil, seller, "", true, gas, "")

	// totalCost = 100 * 2 = 200. MaxTotalPrice=200 must pass.
	buyPayload := `{"listingId":0,"amount":2,"maxTotalPrice":"200"}`
	CallMarket(t, ct, "buy", []byte(buyPayload), nil, buyer, "", true, gas, "")
}

// Back-compat: empty MaxTotalPrice (not set) disables the check entirely.
func TestAuditBuyMaxTotalPriceEmptyIsBackCompat(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	seller := ownerAddress
	buyer := "hive:buyer"
	MintNft(t, ct, seller, "1", 5, 100)
	ApproveNftForMarket(t, ct, seller)
	MintAndApproveToken(t, ct, buyer, 10000)

	listPayload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"%s","pricePerUnit":"100"}`, NftContractID, TokenID)
	CallMarket(t, ct, "list", []byte(listPayload), nil, seller, "", true, gas, "")

	// No maxTotalPrice field — must succeed.
	CallMarket(t, ct, "buy", []byte(`{"listingId":0,"amount":2}`), nil, buyer, "", true, gas, "")
}

// ===================================
// Pass 5 (game-theoretic) — anti-snipe grief floor
// ===================================

// SetMinBidIncrement must reject values below 100 bps (1%).
func TestAuditSetMinBidIncrementTooLow(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	CallMarket(t, ct, "setMinBidIncrement", []byte(`{"minBidIncrementBps":50}`), nil, ownerAddress, "", false, gas, "Min bid increment must be >= 100 basis points (1%)")
}

// Exactly 100 bps is permitted (the floor itself).
func TestAuditSetMinBidIncrementAtFloor(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	CallMarket(t, ct, "setMinBidIncrement", []byte(`{"minBidIncrementBps":100}`), nil, ownerAddress, "", true, gas, "")
}

// 0 bps (would disable anti-snipe grief protection) is now rejected.
func TestAuditSetMinBidIncrementZeroRejected(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	CallMarket(t, ct, "setMinBidIncrement", []byte(`{"minBidIncrementBps":0}`), nil, ownerAddress, "", false, gas, "Min bid increment must be >= 100 basis points (1%)")
}

// ===================================
// Pass 6 (coupling) — assertValidAccount on recipient strings
// ===================================

// SetRoyalty must reject a recipient containing JSON-breaking characters.
// The actual exploit redirects payouts via tinyjson last-key-wins on
// `tokenTransferBig`'s {"to":...,"amount":...} payload; the gate is the
// `assertValidAccount` allowlist.
func TestAuditSetRoyaltyRejectInvalidRecipient(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	// Collection owner must call setRoyalty — InitFullSetup wires nft owner
	// to ownerAddress.
	payload := fmt.Sprintf(`{"nftContract":"%s","royaltyBps":100,"royaltyRecipient":"legit\",\"to\":\"attacker"}`, NftContractID)
	CallMarket(t, ct, "setRoyalty", []byte(payload), nil, ownerAddress, "", false, gas, "account contains invalid characters")
}

// SetRoyaltySplits must reject any split with an invalid recipient.
func TestAuditSetRoyaltySplitsRejectInvalidRecipient(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	payload := fmt.Sprintf(`{"nftContract":"%s","splits":[{"recipient":"hive:good","bps":200},{"recipient":"bad\",\"to\":\"attacker","bps":300}]}`, NftContractID)
	CallMarket(t, ct, "setRoyaltySplits", []byte(payload), nil, ownerAddress, "", false, gas, "account contains invalid characters")
}

// SetFeeRecipient must reject a malicious recipient.
func TestAuditSetFeeRecipientRejectInvalidAccount(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	CallMarket(t, ct, "setFeeRecipient", []byte(`{"feeRecipient":"legit\",\"to\":\"attacker"}`), nil, ownerAddress, "", false, gas, "account contains invalid characters")
}

// ===================================
// Pass 6 (coupling) — reject native paymentToken in unmap/dex listing
// ===================================

// Listing with paymentToken=hbd AND payoutMode=unmap must abort at list
// time. The `unmapTo` cross-chain helper invokes
// `ContractCallSimple("hbd"/"hive", ...)` which has no contract — every
// buy would otherwise abort silently. Reject at list time.
func TestAuditListNativeUnmapRejected(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	seller := ownerAddress
	MintNft(t, ct, seller, "1", 5, 100)
	ApproveNftForMarket(t, ct, seller)

	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"hbd","pricePerUnit":"100","payoutMode":"unmap","payoutL1Address":"hive:somewhere"}`, NftContractID)
	CallMarket(t, ct, "list", []byte(payload), nil, seller, "", false, gas, "native paymentToken cannot use unmap payout")
}

func TestAuditListNativeDexRejected(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	seller := ownerAddress
	MintNft(t, ct, seller, "1", 5, 100)
	ApproveNftForMarket(t, ct, seller)

	// settleToken != "" + paymentToken=hive should reject at list time.
	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":5,"paymentToken":"hive","pricePerUnit":"100","settleToken":"%s","dexPool":"contract:somepool","minSettleOut":"50"}`, NftContractID, TokenID)
	CallMarket(t, ct, "list", []byte(payload), nil, seller, "", false, gas, "native paymentToken cannot use settleToken/dex payout")
}

// ===================================
// Pass 2 (whitelist) — Init seeds native HBD/HIVE
// ===================================

// After init, hive/hbd should be in the whitelist. Make a HIVE-paid offer
// to verify hive is allowed; the test framework doesn't have hive intents
// wired, but the assertPaymentTokenAllowed gate fires BEFORE the intent
// processing — if we get past that gate to a different abort it means the
// whitelist permits hive.
func TestAuditInitSeedsNativeWhitelist(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	seller := ownerAddress
	MintNft(t, ct, seller, "1", 1, 100)
	ApproveNftForMarket(t, ct, seller)

	// list with paymentToken=hive — would abort "Payment token not allowed"
	// if hive weren't seeded. Should succeed past that gate.
	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":1,"paymentToken":"hive","pricePerUnit":"100"}`, NftContractID)
	CallMarket(t, ct, "list", []byte(payload), nil, seller, "", true, gas, "")
}

// And an unknown (non-seeded) token must be rejected.
func TestAuditUnwhitelistedTokenRejected(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	buyer := "hive:buyer"
	MintAndApproveToken(t, ct, buyer, 10000)

	// makeOffer with an unknown payment-token contract id — must reject.
	payload := fmt.Sprintf(`{"nftContract":"%s","tokenId":"1","amount":1,"paymentToken":"contract:notwhitelisted","pricePerUnit":"100"}`, NftContractID)
	CallMarket(t, ct, "makeOffer", []byte(payload), nil, buyer, "", false, gas, "Payment token not allowed")
}
