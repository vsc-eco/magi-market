package main

import (
	"magi_market/sdk"
	"strconv"

	"github.com/CosmWasm/tinyjson/jlexer"
)

// ===================================
// Initialization
// ===================================

//go:wasmexport init
func Init(payload *string) *string {
	if isInit() {
		sdk.Abort("Already initialized")
	}

	owner := sdk.GetEnvKey("contract.owner")
	caller := sdk.GetEnvKey("msg.caller")
	if caller == nil {
		sdk.Abort("Caller required")
	}
	if *caller != *owner {
		sdk.Abort("Only contract owner can initialize")
	}

	if payload == nil || *payload == "" {
		sdk.Abort("Payload required")
	}

	var p InitPayload
	r := jlexer.Lexer{Data: []byte(*payload)}
	p.UnmarshalTinyJSON(&r)
	if r.Error() != nil {
		sdk.Abort("Invalid payload")
	}

	if p.FeeBps > 10000 {
		sdk.Abort("Fee must be <= 10000 basis points")
	}
	if p.FeeRecipient == "" {
		sdk.Abort("Fee recipient required")
	}
	assertValidAccount(p.FeeRecipient)

	sdk.StateSetObject("isInit", "1")
	sdk.StateSetObject("owner", *caller)
	sdk.StateSetObject("paused", "0")
	setUint64State("fee_bps", p.FeeBps)
	setStringState("fee_rcpt", p.FeeRecipient)
	setNextListingId(0)
	setNextOfferId(0)
	setNextAuctionId(0)
	// Seed the min-bid-increment to the 1% floor so anti-snipe can't be
	// griefed into indefinite extension on a fresh deploy. placeBid also
	// floors at runtime (effectiveMinBidIncrementBps), so this is belt-and-
	// suspenders for getInfo reporting / already-deployed instances.
	setMinBidIncrementBps(defaultMinBidIncrementBps)

	// Seed the payment-token whitelist with native HBD/HIVE so the contract
	// is safe-by-default: any unrecognized custom token is rejected until
	// the owner explicitly allows it. Without this, a malicious magi_token
	// (lying balance reads + no-op transfer) could drive the
	// MakeOffer/AcceptOffer flow into delivering NFTs for fake payments,
	// and a malicious paymentToken's transfer hook could re-enter
	// placeBid's refund path to drain pool funds. setPaymentTokenAllowed
	// flips ptw_on=1 on the first allowed token.
	setPaymentTokenAllowed(nativeAssetHive, true)
	setPaymentTokenAllowed(nativeAssetHbd, true)

	emitInit(*caller, p.FeeBps, p.FeeRecipient)
	return jsonResponse(&SuccessResponse{Success: true})
}

// ===================================
// Listing Functions
// ===================================

// doList is the core listing logic shared by List and BatchList.
func doList(caller string, p *ListPayload) uint64 {
	if p.NftContract == "" || p.TokenId == "" || p.PaymentToken == "" {
		sdk.Abort("NFT contract, token ID, and payment token required")
	}
	if p.Amount == 0 {
		sdk.Abort("Amount must be greater than zero")
	}
	assertValidTokenId(p.TokenId)
	price := parseMoney(p.PricePerUnit)
	if mIsZero(price) {
		sdk.Abort("Price must be greater than zero")
	}

	assertPaymentTokenAllowed(p.PaymentToken)

	// Soulbound NFTs can only be transferred by the collection owner
	// (magi_nft: `isSoulbound(id) && from != ownerAddr → abort`). Listings
	// don't escrow — the buy-time transfer is seller→buyer directly — so a
	// collection owner CAN list their own soulbound editions (the
	// seller==owner transfer succeeds). A non-owner holder can't, so block
	// them. (Auctions/rentals escrow to the market, which is not the
	// collection owner, so they stay unconditionally blocked.)
	if nftIsSoulbound(p.NftContract, p.TokenId) && nftGetOwner(p.NftContract) != caller {
		sdk.Abort("Cannot list soulbound tokens (only the collection owner can transfer them)")
	}
	assertCollectionAllowed(p.NftContract)

	currentFeeBps := getEffectiveFeeBps(p.NftContract)
	currentRoyaltyBps := getRoyaltyBps(p.NftContract)
	if currentFeeBps+currentRoyaltyBps > 10000 {
		sdk.Abort("Combined fee and royalty exceed 100%")
	}

	contractAddr := getContractAddress()
	// Authorization: operator approval (setApprovalForAll) OR a per-token
	// ERC-6909 allowance (approve) covering the listed amount. The
	// per-token path is least-privilege — scoped to exactly this token id
	// instead of blanket-approving the whole collection. magi_nft's
	// safeTransferFrom (driven by doBuy) honours the same allowance
	// fallback and decrements it per sale. Mirrors the listMintSpots gate.
	if !nftIsApprovedForAll(p.NftContract, caller, contractAddr) {
		if nftAllowanceOf(p.NftContract, caller, contractAddr, p.TokenId) < p.Amount {
			sdk.Abort("Marketplace not approved as operator or sufficient per-token allowance for this NFT collection")
		}
	}
	if nftBalanceOf(p.NftContract, caller, p.TokenId) < p.Amount {
		sdk.Abort("Insufficient NFT balance to list")
	}

	// F1: validate payoutMode / payoutL1Address
	if p.PayoutMode == "unmap" {
		if isNativeAsset(p.PaymentToken) {
			// `unmapTo` routes through ContractCallSimple("hive"/"hbd", "unmap", …)
			// which has no contract to call — every buy would abort at
			// buy time, bricking the listing. Reject at list time.
			sdk.Abort("native paymentToken cannot use unmap payout")
		}
		if p.PayoutL1Address == "" {
			sdk.Abort("payoutL1Address required for unmap payout")
		}
		for i := 0; i < len(p.PayoutL1Address); i++ {
			c := p.PayoutL1Address[i]
			if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
				(c >= '0' && c <= '9') || c == ':' || c == '-') {
				sdk.Abort("payoutL1Address contains invalid characters")
			}
		}
	}

	// F2: validate settleToken / dexPool / minSettleOut
	if p.SettleToken != "" {
		if p.PayoutMode == "unmap" {
			sdk.Abort("payout and settleToken are mutually exclusive")
		}
		if isNativeAsset(p.PaymentToken) {
			// Same as unmap: dexSwapTo would invoke a non-existent
			// contract id for native paymentToken, aborting every buy.
			sdk.Abort("native paymentToken cannot use settleToken/dex payout")
		}
		if p.DexPool == "" {
			sdk.Abort("dexPool required for settleToken")
		}
		if p.SettleToken == p.PaymentToken {
			sdk.Abort("settleToken must differ from paymentToken")
		}
		// minSettleOut must be a valid positive decimal (parseMoney aborts on invalid).
		mso := parseMoney(p.MinSettleOut)
		if mIsZero(mso) {
			sdk.Abort("minSettleOut must be greater than zero")
		}
		// Injection allowlist: dexPool and settleToken are string-concatenated into JSON.
		for i := 0; i < len(p.DexPool); i++ {
			c := p.DexPool[i]
			if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
				(c >= '0' && c <= '9') || c == ':' || c == '-') {
				sdk.Abort("dexPool contains invalid characters")
			}
		}
		for i := 0; i < len(p.SettleToken); i++ {
			c := p.SettleToken[i]
			if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
				(c >= '0' && c <= '9') || c == ':' || c == '-') {
				sdk.Abort("settleToken contains invalid characters")
			}
		}
	}

	id := getNextListingId()
	setListingField(id, "s", caller)
	setListingField(id, "nc", p.NftContract)
	setListingField(id, "ti", p.TokenId)
	setListingUint64(id, "a", p.Amount)
	setListingMoney(id, "p", price)
	setListingField(id, "pt", p.PaymentToken)
	setListingField(id, "act", "1")
	setListingUint64(id, "exp", p.ExpirationBlock)
	setListingUint64(id, "fb", currentFeeBps)
	setListingUint64(id, "rb", currentRoyaltyBps)
	setListingField(id, "rr", getRoyaltyRecipient(p.NftContract))
	setListingUint64(id, "sb", p.StartBlock)
	setListingField(id, "pm", p.PayoutMode)
	setListingField(id, "pl1", p.PayoutL1Address)
	setListingField(id, "dp", p.DexPool)
	setListingField(id, "st", p.SettleToken)
	setListingField(id, "mso", p.MinSettleOut)
	// Snapshot resolved royalty splits so in-flight trades are unaffected by later split changes.
	snapRecips, snapBps := resolveRoyaltySplits(p.NftContract)
	snapshotRoyaltySplitsForListing(id, snapRecips, snapBps)
	setNextListingId(id + 1)

	emitListed(id, caller, p.NftContract, p.TokenId, p.Amount, formatMoney(price), p.PaymentToken, p.ExpirationBlock)
	return id
}

//go:wasmexport list
func List(payload *string) *string {
	assertInit()
	assertNotPaused()

	caller := getCaller()

	if payload == nil || *payload == "" {
		sdk.Abort("Payload required")
	}

	var p ListPayload
	r := jlexer.Lexer{Data: []byte(*payload)}
	p.UnmarshalTinyJSON(&r)
	if r.Error() != nil {
		sdk.Abort("Invalid payload")
	}

	id := doList(caller, &p)
	return jsonResponse(&CreatedResponse{Success: true, Id: id})
}

//go:wasmexport delist
func Delist(payload *string) *string {
	assertInit()
	// Note: delist is intentionally allowed when paused so sellers can
	// cancel listings freely during a contract pause (no NFT is escrowed).

	caller := getCaller()

	if payload == nil || *payload == "" {
		sdk.Abort("Payload required")
	}

	var p DelistPayload
	r := jlexer.Lexer{Data: []byte(*payload)}
	p.UnmarshalTinyJSON(&r)
	if r.Error() != nil {
		sdk.Abort("Invalid payload")
	}

	if !isListingActive(p.ListingId) {
		sdk.Abort("Listing not active")
	}

	seller := getListingField(p.ListingId, "s")
	if caller != seller {
		sdk.Abort("Only seller can delist")
	}

	// Mark inactive
	setListingField(p.ListingId, "act", "0")

	emitDelisted(p.ListingId, seller)
	return jsonResponse(&SuccessResponse{Success: true})
}

// doBuy is the core buy logic shared by Buy and BatchBuy.
func doBuy(caller string, p *BuyPayload) {
	if !isListingActive(p.ListingId) {
		sdk.Abort("Listing not active")
	}
	if isExpired(getListingUint64(p.ListingId, "exp")) {
		sdk.Abort("Listing has expired")
	}
	if sb := getListingUint64(p.ListingId, "sb"); sb != 0 && getCurrentBlockHeight() < sb {
		sdk.Abort("Listing not started")
	}
	if p.Amount == 0 {
		sdk.Abort("Amount must be greater than zero")
	}
	remaining := getListingUint64(p.ListingId, "a")
	if p.Amount > remaining {
		sdk.Abort("Insufficient listing amount")
	}

	seller := getListingField(p.ListingId, "s")
	if caller == seller {
		sdk.Abort("Seller cannot buy own listing")
	}
	paymentToken := getListingField(p.ListingId, "pt")
	// Re-validate the listing's payment token at buy-time. If the owner
	// has since removed the token from the whitelist (e.g. it turned out
	// to misbehave), in-flight listings using it are halted.
	assertPaymentTokenAllowed(paymentToken)
	pricePerUnit := getListingMoney(p.ListingId, "p")
	nftContract := getListingField(p.ListingId, "nc")
	assertCollectionAllowed(nftContract)
	tokenId := getListingField(p.ListingId, "ti")
	lockedFeeBps := getListingUint64(p.ListingId, "fb")
	lockedRoyaltyBps := getListingUint64(p.ListingId, "rb")
	royaltyRecipient := getListingField(p.ListingId, "rr")

	totalCost := mMulU64(pricePerUnit, p.Amount)

	// Slippage guard: a seller who races `updateListing` to bump the price
	// just before this tx lands would otherwise silently drain extra funds
	// from any buyer who pre-approved more than the listing's old total.
	// Empty MaxTotalPrice disables the check (back-compat for callers that
	// don't supply it).
	if p.MaxTotalPrice != "" {
		maxTotal := parseMoney(p.MaxTotalPrice)
		if mCmp(totalCost, maxTotal) > 0 {
			sdk.Abort("Total cost exceeds buyer's MaxTotalPrice")
		}
	}

	// CEI (F5 template): decrement remaining + flip active BEFORE ANY
	// external call (including escrowIn). A re-entry through a malicious
	// whitelisted paymentToken's transferFrom hook would otherwise read
	// the stale `a` and let the same listing be bought twice in nested
	// frames; this ordering plus tx-revert-on-abort closes the window.
	newRemaining := safeSub(remaining, p.Amount)
	if newRemaining == 0 {
		setListingField(p.ListingId, "act", "0")
	}
	setListingUint64(p.ListingId, "a", newRemaining)

	received := escrowIn(paymentToken, caller, totalCost)

	// Load royalty split snapshot; fall back to legacy single-entry for pre-B2 in-flight entries.
	snapRecips, snapBps := loadListingRoyaltySplitSnapshot(p.ListingId, royaltyRecipient, lockedRoyaltyBps)
	fee, royTot, sellerPayment := feeAndRoyaltyOf(received, lockedFeeBps, snapRecips, snapBps)

	// Transfer NFT from seller -> buyer using operator approval. If the seller
	// moved/burned the NFT or revoked approval, this aborts and the whole tx
	// (including the escrow leg) reverts — no orphaned funds.
	nftSafeTransferFrom(nftContract, seller, caller, tokenId, p.Amount)

	if !mIsZero(fee) {
		tokenTransferBig(paymentToken, getFeeRecipient(), fee)
	}
	distributeRoyaltySplitsResolved(paymentToken, received, snapRecips, snapBps)
	// Seller-leg: F1 unmap, F2 DEX-routed settlement, or legacy mapped-token
	// transfer. Read `pm` first and only read `st`/`dp`/`pl1`/`mso` on the
	// branch that needs them (they're mutually exclusive per the F2 checks),
	// so an unmap listing skips the `st` read and a legacy listing reads only
	// `pm`+`st`.
	pm := getListingField(p.ListingId, "pm")
	if pm == "unmap" {
		if !mIsZero(sellerPayment) {
			unmapTo(paymentToken, getListingField(p.ListingId, "pl1"), sellerPayment)
		}
	} else if st := getListingField(p.ListingId, "st"); st != "" {
		if !mIsZero(sellerPayment) {
			dexSwapTo(
				getListingField(p.ListingId, "dp"),
				paymentToken,
				st,
				seller,
				formatMoney(sellerPayment),
				formatMoney(parseMoney(getListingField(p.ListingId, "mso"))),
			)
		}
	} else {
		if !mIsZero(sellerPayment) {
			tokenTransferBig(paymentToken, seller, sellerPayment)
		}
	}

	emitBought(p.ListingId, caller, p.Amount, formatMoney(received), formatMoney(fee), formatMoney(royTot))
}

//go:wasmexport buy
func Buy(payload *string) *string {
	assertInit()
	assertNotPaused()

	caller := getCaller()

	if payload == nil || *payload == "" {
		sdk.Abort("Payload required")
	}

	var p BuyPayload
	r := jlexer.Lexer{Data: []byte(*payload)}
	p.UnmarshalTinyJSON(&r)
	if r.Error() != nil {
		sdk.Abort("Invalid payload")
	}

	doBuy(caller, &p)
	return jsonResponse(&SuccessResponse{Success: true})
}

//go:wasmexport updateListing
func UpdateListing(payload *string) *string {
	assertInit()
	assertNotPaused()

	caller := getCaller()

	if payload == nil || *payload == "" {
		sdk.Abort("Payload required")
	}

	var p UpdateListingPayload
	r := jlexer.Lexer{Data: []byte(*payload)}
	p.UnmarshalTinyJSON(&r)
	if r.Error() != nil {
		sdk.Abort("Invalid payload")
	}

	if !isListingActive(p.ListingId) {
		sdk.Abort("Listing not active")
	}

	expBlock := getListingUint64(p.ListingId, "exp")
	if isExpired(expBlock) {
		sdk.Abort("Listing has expired")
	}

	seller := getListingField(p.ListingId, "s")
	if caller != seller {
		sdk.Abort("Only seller can update listing")
	}

	newPrice := parseMoney(p.NewPrice)
	if mIsZero(newPrice) {
		sdk.Abort("Price must be greater than zero")
	}
	setListingMoney(p.ListingId, "p", newPrice)
	emitListingUpdated(p.ListingId, formatMoney(newPrice))
	return jsonResponse(&SuccessResponse{Success: true})
}

// ===================================
// Offer Functions
// ===================================

//go:wasmexport makeOffer
func MakeOffer(payload *string) *string {
	assertInit()
	assertNotPaused()

	caller := getCaller()

	if payload == nil || *payload == "" {
		sdk.Abort("Payload required")
	}

	var p MakeOfferPayload
	r := jlexer.Lexer{Data: []byte(*payload)}
	p.UnmarshalTinyJSON(&r)
	if r.Error() != nil {
		sdk.Abort("Invalid payload")
	}

	if p.NftContract == "" || p.PaymentToken == "" {
		sdk.Abort("NFT contract and payment token required")
	}
	// MakeOffer allows TokenId == "" (collection offer); when set, validate.
	assertValidTokenId(p.TokenId)
	assertCollectionAllowed(p.NftContract)

	price := parseMoney(p.PricePerUnit)
	if mIsZero(price) {
		sdk.Abort("Price must be greater than zero")
	}
	if p.Amount == 0 {
		sdk.Abort("Amount must be greater than zero")
	}
	assertPaymentTokenAllowed(p.PaymentToken)

	totalOffer := mMulU64(price, p.Amount)
	minOffer := getMinOfferMoney()
	if !mIsZero(minOffer) && mCmp(totalOffer, minOffer) < 0 {
		sdk.Abort("Offer below minimum threshold")
	}

	currentFeeBps := getEffectiveFeeBps(p.NftContract)
	currentRoyaltyBps := getRoyaltyBps(p.NftContract)
	if currentFeeBps+currentRoyaltyBps > 10000 {
		sdk.Abort("Combined fee and royalty exceed 100%")
	}

	// CEI (F5 template): claim the new offer-id AND write all of its
	// state fields (except `esc`, which needs `received`) BEFORE the
	// external `escrowIn`. Inner re-entry through a malicious whitelisted
	// paymentToken's transferFrom callback would otherwise read the same
	// `id` from getNextOfferId() and collide with outer's writes; with
	// nxt_o bumped here, the inner gets id+1 and writes to a separate
	// row, leaving outer's `id`-row intact.
	id := getNextOfferId()
	setNextOfferId(id + 1)
	isCol := p.TokenId == ""

	setOfferField(id, "b", caller)
	setOfferField(id, "nc", p.NftContract)
	setOfferField(id, "ti", p.TokenId)
	setOfferUint64(id, "a", p.Amount)
	setOfferMoney(id, "p", price)
	setOfferField(id, "pt", p.PaymentToken)
	setOfferField(id, "act", "1")
	setOfferUint64(id, "exp", p.ExpirationBlock)
	setOfferUint64(id, "fb", currentFeeBps)
	setOfferUint64(id, "rb", currentRoyaltyBps)
	setOfferField(id, "rr", getRoyaltyRecipient(p.NftContract))
	// Snapshot resolved royalty splits so in-flight offers are unaffected by later split changes.
	offerSnapRecips, offerSnapBps := resolveRoyaltySplits(p.NftContract)
	snapshotRoyaltySplitsForOffer(id, offerSnapRecips, offerSnapBps)
	if isCol {
		setOfferField(id, "col", "1")
	}

	// Escrow payment with balance-delta; store the ACTUAL received total so
	// cancel refunds and accept payouts can never over-distribute.
	received := escrowIn(p.PaymentToken, caller, totalOffer)
	setOfferMoney(id, "esc", received)

	emitOfferMade(id, caller, p.NftContract, p.TokenId, p.Amount, formatMoney(price), p.PaymentToken, p.ExpirationBlock, isCol)
	return jsonResponse(&CreatedResponse{Success: true, Id: id})
}

//go:wasmexport cancelOffer
func CancelOffer(payload *string) *string {
	assertInit()
	// Note: cancelOffer works even when paused so buyers can recover escrowed payments

	caller := getCaller()

	if payload == nil || *payload == "" {
		sdk.Abort("Payload required")
	}

	var p CancelOfferPayload
	r := jlexer.Lexer{Data: []byte(*payload)}
	p.UnmarshalTinyJSON(&r)
	if r.Error() != nil {
		sdk.Abort("Invalid payload")
	}

	if !isOfferActive(p.OfferId) {
		sdk.Abort("Offer not active")
	}

	buyer := getOfferField(p.OfferId, "b")
	expBlock := getOfferUint64(p.OfferId, "exp")
	if !isExpired(expBlock) && caller != buyer {
		sdk.Abort("Only buyer can cancel offer")
	}
	paymentToken := getOfferField(p.OfferId, "pt")
	refund := getOfferMoney(p.OfferId, "esc")
	// CEI: flip the offer inactive (and zero its escrow) BEFORE the refund
	// call. A malicious whitelisted paymentToken's transfer callback could
	// otherwise re-enter cancelOffer, see act=1 and the unchanged esc, and
	// keep refunding `esc` worth of paymentToken per recursion level until
	// the call stack runs out — draining pool funds.
	setOfferField(p.OfferId, "act", "0")
	if !mIsZero(refund) {
		setOfferMoney(p.OfferId, "esc", mZero())
		tokenTransferBig(paymentToken, buyer, refund)
	}
	emitOfferCancelled(p.OfferId, buyer)
	return jsonResponse(&SuccessResponse{Success: true})
}

// doAcceptOffer is the core logic for accepting an offer (token-specific or collection).
func doAcceptOffer(caller string, offerId uint64, acceptAmount uint64, tokenId string) {
	if !isOfferActive(offerId) {
		sdk.Abort("Offer not active")
	}
	if isExpired(getOfferUint64(offerId, "exp")) {
		sdk.Abort("Offer has expired")
	}

	buyer := getOfferField(offerId, "b")
	if caller == buyer {
		// Mirrors doBuy's "Seller cannot buy own listing": a self-deal is
		// economically meaningless and would be a self-transfer on the NFT.
		sdk.Abort("Buyer cannot accept own offer")
	}
	nftContract := getOfferField(offerId, "nc")
	assertCollectionAllowed(nftContract)
	offerAmount := getOfferUint64(offerId, "a")
	pricePerUnit := getOfferMoney(offerId, "p")
	paymentToken := getOfferField(offerId, "pt")
	// Re-validate at accept-time (de-whitelisted-after-offer halt).
	// `cancelOffer` deliberately skips this — buyers must always be able
	// to reclaim their escrow even after the token is removed.
	assertPaymentTokenAllowed(paymentToken)
	lockedFeeBps := getOfferUint64(offerId, "fb")
	lockedRoyaltyBps := getOfferUint64(offerId, "rb")
	royaltyRecipient := getOfferField(offerId, "rr")

	if acceptAmount == 0 {
		acceptAmount = offerAmount
	}
	if acceptAmount > offerAmount {
		sdk.Abort("Accept amount exceeds offer amount")
	}

	// Clean preflight instead of a raw cross-call abort. Operator approval
	// OR a per-token allowance (least-privilege) covering the accepted
	// amount — magi_nft's safeTransferFrom honours either.
	if !nftIsApprovedForAll(nftContract, caller, getContractAddress()) {
		if nftAllowanceOf(nftContract, caller, getContractAddress(), tokenId) < acceptAmount {
			sdk.Abort("Marketplace not approved as operator or sufficient per-token allowance to fulfill offer")
		}
	}
	if nftBalanceOf(nftContract, caller, tokenId) < acceptAmount {
		sdk.Abort("Insufficient NFT balance to fulfill offer")
	}

	totalPrice := mMulU64(pricePerUnit, acceptAmount)
	escrowed := getOfferMoney(offerId, "esc")
	// "esc" is the amount the contract ACTUALLY received at makeOffer time
	// (balance-delta). For a fee-on-transfer / UTXO deduct_fee payment token,
	// esc < pricePerUnit*offerAmount, so a full accept (totalPrice =
	// pricePerUnit*offerAmount) aborts here and only partial accepts up to
	// `received` worth succeed. This is intentional: the contract only ever
	// transacts funds it truly holds. The buyer recovers any unaccepted
	// remainder via cancelOffer (refunded from the residual esc). Sellers
	// accepting fee-on-transfer-token offers should accept amounts sized to
	// the received escrow, not the nominal offer amount.
	if mCmp(totalPrice, escrowed) > 0 {
		sdk.Abort("Accept exceeds escrowed funds")
	}

	// Load royalty split snapshot; fall back to legacy single-entry for pre-B2 in-flight entries.
	offerSnapRecips, offerSnapBps := loadOfferRoyaltySplitSnapshot(offerId, royaltyRecipient, lockedRoyaltyBps)
	fee, royTot, sellerPayment := feeAndRoyaltyOf(totalPrice, lockedFeeBps, offerSnapRecips, offerSnapBps)

	// Checks-Effects-Interactions: flip every state field BEFORE any
	// external call so a re-entry through the buyer-supplied nftContract
	// (during nftSafeTransferFrom) or through the paymentToken (during
	// tokenTransferBig) cannot read stale `esc`/`a`/`act` and double-spend
	// against this offer. Without this ordering, the outer continuation's
	// stale local `escrowed` would overwrite the inner's decrement and
	// leak pool funds on cancel.
	newRemaining := safeSub(offerAmount, acceptAmount)
	setOfferMoney(offerId, "esc", mSub(escrowed, totalPrice))
	if newRemaining == 0 {
		setOfferField(offerId, "act", "0")
	} else {
		setOfferUint64(offerId, "a", newRemaining)
	}

	nftSafeTransferFrom(nftContract, caller, buyer, tokenId, acceptAmount)

	if !mIsZero(sellerPayment) {
		tokenTransferBig(paymentToken, caller, sellerPayment)
	}
	if !mIsZero(fee) {
		tokenTransferBig(paymentToken, getFeeRecipient(), fee)
	}
	distributeRoyaltySplitsResolved(paymentToken, totalPrice, offerSnapRecips, offerSnapBps)

	emitOfferAccepted(offerId, caller, buyer, acceptAmount, formatMoney(totalPrice), formatMoney(fee), formatMoney(royTot), tokenId)
}

//go:wasmexport acceptOffer
func AcceptOffer(payload *string) *string {
	assertInit()
	assertNotPaused()

	caller := getCaller()

	if payload == nil || *payload == "" {
		sdk.Abort("Payload required")
	}

	var p AcceptOfferPayload
	r := jlexer.Lexer{Data: []byte(*payload)}
	p.UnmarshalTinyJSON(&r)
	if r.Error() != nil {
		sdk.Abort("Invalid payload")
	}

	// Token-specific offers only via acceptOffer
	if isCollectionOffer(p.OfferId) {
		sdk.Abort("Use acceptCollectionOffer for collection offers")
	}

	tokenId := getOfferField(p.OfferId, "ti")
	doAcceptOffer(caller, p.OfferId, p.Amount, tokenId)
	return jsonResponse(&SuccessResponse{Success: true})
}

//go:wasmexport acceptCollectionOffer
func AcceptCollectionOffer(payload *string) *string {
	assertInit()
	assertNotPaused()

	caller := getCaller()

	if payload == nil || *payload == "" {
		sdk.Abort("Payload required")
	}

	var p AcceptCollectionOfferPayload
	r := jlexer.Lexer{Data: []byte(*payload)}
	p.UnmarshalTinyJSON(&r)
	if r.Error() != nil {
		sdk.Abort("Invalid payload")
	}

	if !isCollectionOffer(p.OfferId) {
		sdk.Abort("Not a collection offer")
	}

	if p.TokenId == "" {
		sdk.Abort("Token ID required for collection offer acceptance")
	}
	assertValidTokenId(p.TokenId)

	doAcceptOffer(caller, p.OfferId, p.Amount, p.TokenId)
	return jsonResponse(&SuccessResponse{Success: true})
}

// ===================================
// Admin Functions
// ===================================

//go:wasmexport setFee
func SetFee(payload *string) *string {
	assertInit()

	_, isOwner := getOwner()
	if !isOwner {
		sdk.Abort("Only owner can set fee")
	}

	if payload == nil || *payload == "" {
		sdk.Abort("Payload required")
	}

	var p FeePayload
	r := jlexer.Lexer{Data: []byte(*payload)}
	p.UnmarshalTinyJSON(&r)
	if r.Error() != nil {
		sdk.Abort("Invalid payload")
	}

	if p.FeeBps > 10000 {
		sdk.Abort("Fee must be <= 10000 basis points")
	}

	setUint64State("fee_bps", p.FeeBps)
	return jsonResponse(&SuccessResponse{Success: true})
}

//go:wasmexport setFeeRecipient
func SetFeeRecipient(payload *string) *string {
	assertInit()

	_, isOwner := getOwner()
	if !isOwner {
		sdk.Abort("Only owner can set fee recipient")
	}

	if payload == nil || *payload == "" {
		sdk.Abort("Payload required")
	}

	var p FeeRecipientPayload
	r := jlexer.Lexer{Data: []byte(*payload)}
	p.UnmarshalTinyJSON(&r)
	if r.Error() != nil {
		sdk.Abort("Invalid payload")
	}

	if p.FeeRecipient == "" {
		sdk.Abort("Fee recipient required")
	}
	assertValidAccount(p.FeeRecipient)

	setStringState("fee_rcpt", p.FeeRecipient)
	return jsonResponse(&SuccessResponse{Success: true})
}

//go:wasmexport changeOwner
func ChangeOwner(payload *string) *string {
	assertInit()

	owner, isOwner := getOwner()
	if !isOwner {
		sdk.Abort("Only owner can change owner")
	}

	if payload == nil || *payload == "" {
		sdk.Abort("Payload required")
	}

	var p ChangeOwnerPayload
	r := jlexer.Lexer{Data: []byte(*payload)}
	p.UnmarshalTinyJSON(&r)
	if r.Error() != nil {
		sdk.Abort("Invalid payload")
	}

	if p.NewOwner == "" {
		sdk.Abort("New owner address required")
	}
	assertValidAccount(p.NewOwner)

	// 2-step: propose only. Ownership does not move until the proposed
	// owner calls acceptOwnership. Re-calling overwrites the candidate.
	setPendingOwner(p.NewOwner)
	emitOwnerTransferInitiated(owner, p.NewOwner)
	return jsonResponse(&SuccessResponse{Success: true})
}

//go:wasmexport acceptOwnership
func AcceptOwnership(payload *string) *string {
	assertInit()

	pending := getPendingOwner()
	if pending == "" {
		sdk.Abort("No pending ownership transfer")
	}

	caller := getCaller()
	if caller != pending {
		sdk.Abort("Not the pending owner")
	}

	previous, _ := getOwner()
	sdk.StateSetObject("owner", pending)
	clearPendingOwner()
	emitOwnerChange(previous, pending)
	return jsonResponse(&SuccessResponse{Success: true})
}

//go:wasmexport cancelOwnershipTransfer
func CancelOwnershipTransfer(payload *string) *string {
	assertInit()

	_, isOwner := getOwner()
	if !isOwner {
		sdk.Abort("Only owner can cancel ownership transfer")
	}

	if getPendingOwner() == "" {
		sdk.Abort("No pending ownership transfer")
	}

	caller := getCaller()
	clearPendingOwner()
	emitOwnerTransferCancelled(caller)
	return jsonResponse(&SuccessResponse{Success: true})
}

//go:wasmexport getPendingOwner
func GetPendingOwner(payload *string) *string {
	assertInit()
	return jsonResponse(&PendingOwnerResponse{PendingOwner: getPendingOwner()})
}

//go:wasmexport pause
func Pause(payload *string) *string {
	assertInit()

	_, isOwner := getOwner()
	if !isOwner {
		sdk.Abort("Only owner can pause")
	}

	if isPaused() {
		sdk.Abort("Already paused")
	}

	sdk.StateSetObject("paused", "1")
	caller := getCaller()
	emitPaused(caller)
	return jsonResponse(&SuccessResponse{Success: true})
}

//go:wasmexport unpause
func Unpause(payload *string) *string {
	assertInit()

	_, isOwner := getOwner()
	if !isOwner {
		sdk.Abort("Only owner can unpause")
	}

	if !isPaused() {
		sdk.Abort("Not paused")
	}

	sdk.StateSetObject("paused", "0")
	caller := getCaller()
	emitUnpaused(caller)
	return jsonResponse(&SuccessResponse{Success: true})
}

//go:wasmexport setRoyalty
func SetRoyalty(payload *string) *string {
	assertInit()

	caller := getCaller()

	if payload == nil || *payload == "" {
		sdk.Abort("Payload required")
	}

	var p SetRoyaltyPayload
	r := jlexer.Lexer{Data: []byte(*payload)}
	p.UnmarshalTinyJSON(&r)
	if r.Error() != nil {
		sdk.Abort("Invalid payload")
	}

	if p.NftContract == "" {
		sdk.Abort("NFT contract required")
	}

	// Verify caller is the owner of the NFT collection
	collectionOwner := nftGetOwner(p.NftContract)
	if collectionOwner == "" || caller != collectionOwner {
		sdk.Abort("Only collection owner can set royalty")
	}

	if p.RoyaltyBps > 5000 {
		sdk.Abort("Royalty must be <= 5000 basis points (50%)")
	}

	if p.RoyaltyBps > 0 && p.RoyaltyRecipient == "" {
		sdk.Abort("Royalty recipient required when royalty > 0")
	}
	assertValidAccount(p.RoyaltyRecipient)

	setRoyaltyBps(p.NftContract, p.RoyaltyBps)
	setRoyaltyRecipientState(p.NftContract, p.RoyaltyRecipient)
	// Clear any stale multi-split state from a previous SetRoyaltySplits.
	// Without this, resolveRoyaltySplits keeps returning the OLD multi
	// splits (because it short-circuits on n > 0), silently sending
	// royalties to addresses the owner intended to retire.
	clearRoyaltySplits(p.NftContract)

	emitRoyaltySet(p.NftContract, p.RoyaltyBps, p.RoyaltyRecipient)
	return jsonResponse(&SuccessResponse{Success: true})
}

//go:wasmexport setRoyaltySplits
func SetRoyaltySplits(payload *string) *string {
	assertInit()

	caller := getCaller()

	if payload == nil || *payload == "" {
		sdk.Abort("Payload required")
	}

	var p SetRoyaltySplitsPayload
	r := jlexer.Lexer{Data: []byte(*payload)}
	p.UnmarshalTinyJSON(&r)
	if r.Error() != nil {
		sdk.Abort("Invalid payload")
	}

	if p.NftContract == "" {
		sdk.Abort("NFT contract required")
	}

	// Verify caller is the owner of the NFT collection (mirror SetRoyalty's check exactly)
	collectionOwner := nftGetOwner(p.NftContract)
	if collectionOwner == "" || caller != collectionOwner {
		sdk.Abort("Only collection owner can set royalty")
	}

	if len(p.Splits) == 0 {
		sdk.Abort("At least one royalty split required")
	}
	if len(p.Splits) > 10 {
		sdk.Abort("Too many royalty splits")
	}

	var totalBps uint64
	for _, split := range p.Splits {
		if split.Bps == 0 {
			sdk.Abort("Royalty split bps must be > 0")
		}
		// Per-split cap of 5000 (== global cap) — also closes the
		// uint64-overflow door on the accumulator: 10 splits * 5000 =
		// 50_000 << 2^64, so totalBps can never wrap before the
		// post-loop check.
		if split.Bps > 5000 {
			sdk.Abort("Royalty split bps must be <= 5000")
		}
		if split.Recipient == "" {
			sdk.Abort("Royalty split recipient required")
		}
		assertValidAccount(split.Recipient)
		totalBps += split.Bps
	}
	if totalBps > 5000 {
		sdk.Abort("Royalty must be <= 5000 basis points")
	}

	recips := make([]string, len(p.Splits))
	bpss := make([]uint64, len(p.Splits))
	for i, split := range p.Splits {
		recips[i] = split.Recipient
		bpss[i] = split.Bps
	}
	setRoyaltySplits(p.NftContract, recips, bpss)

	// Keep legacy single-entry view coherent
	setRoyaltyBps(p.NftContract, totalBps)
	setRoyaltyRecipientState(p.NftContract, p.Splits[0].Recipient)

	emitRoyaltySplitsSet(p.NftContract, uint64(len(p.Splits)))
	return jsonResponse(&SuccessResponse{Success: true})
}

//go:wasmexport getRoyaltySplits
func GetRoyaltySplits(payload *string) *string {
	assertInit()

	if payload == nil || *payload == "" {
		sdk.Abort("Payload required")
	}

	var p CollectionPayload
	r := jlexer.Lexer{Data: []byte(*payload)}
	p.UnmarshalTinyJSON(&r)
	if r.Error() != nil {
		sdk.Abort("Invalid payload")
	}

	if p.NftContract == "" {
		sdk.Abort("NFT contract required")
	}

	recips, bpss := resolveRoyaltySplits(p.NftContract)
	splits := make([]RoyaltySplit, len(recips))
	for i := range recips {
		splits[i] = RoyaltySplit{Recipient: recips[i], Bps: bpss[i]}
	}

	return jsonResponse(&RoyaltySplitsResponse{NftContract: p.NftContract, Splits: splits})
}

//go:wasmexport setMinOffer
func SetMinOffer(payload *string) *string {
	assertInit()

	_, isOwner := getOwner()
	if !isOwner {
		sdk.Abort("Only owner can set minimum offer")
	}

	if payload == nil || *payload == "" {
		sdk.Abort("Payload required")
	}

	var p SetMinOfferPayload
	r := jlexer.Lexer{Data: []byte(*payload)}
	p.UnmarshalTinyJSON(&r)
	if r.Error() != nil {
		sdk.Abort("Invalid payload")
	}

	setMinOfferMoney(parseMoney(p.MinOffer))
	return jsonResponse(&SuccessResponse{Success: true})
}

//go:wasmexport addPaymentToken
func AddPaymentToken(payload *string) *string {
	assertInit()

	_, isOwner := getOwner()
	if !isOwner {
		sdk.Abort("Only owner can manage payment tokens")
	}

	if payload == nil || *payload == "" {
		sdk.Abort("Payload required")
	}

	var p PaymentTokenPayload
	r := jlexer.Lexer{Data: []byte(*payload)}
	p.UnmarshalTinyJSON(&r)
	if r.Error() != nil {
		sdk.Abort("Invalid payload")
	}

	if p.Token == "" {
		sdk.Abort("Token address required")
	}
	assertValidAccount(p.Token)
	// Optional balance-decoder binding. If provided it must be a known type;
	// omitted leaves the token on legacy auto-probe.
	if p.Decoder != "" && !isValidDecoder(p.Decoder) {
		sdk.Abort("Invalid decoder (expected magi_token, utxo, or native)")
	}

	setPaymentTokenAllowed(p.Token, true)
	setPaymentTokenDecoder(p.Token, p.Decoder)
	emitPaymentToken("payment_token_added", p.Token)
	return jsonResponse(&SuccessResponse{Success: true})
}

//go:wasmexport removePaymentToken
func RemovePaymentToken(payload *string) *string {
	assertInit()

	_, isOwner := getOwner()
	if !isOwner {
		sdk.Abort("Only owner can manage payment tokens")
	}

	if payload == nil || *payload == "" {
		sdk.Abort("Payload required")
	}

	var p PaymentTokenPayload
	r := jlexer.Lexer{Data: []byte(*payload)}
	p.UnmarshalTinyJSON(&r)
	if r.Error() != nil {
		sdk.Abort("Invalid payload")
	}

	if p.Token == "" {
		sdk.Abort("Token address required")
	}

	setPaymentTokenAllowed(p.Token, false)
	// Clear the decoder binding too — a later re-add without a decoder
	// (auto-probe) must not silently inherit the prior magi_token/utxo
	// classification (avoids stale-decoder over/under-credit).
	sdk.StateDeleteObject(paymentTokenDecoderKey(p.Token))
	emitPaymentToken("payment_token_removed", p.Token)
	return jsonResponse(&SuccessResponse{Success: true})
}

//go:wasmexport emergencyWithdraw
func EmergencyWithdraw(payload *string) *string {
	assertInit()

	if !isPaused() {
		sdk.Abort("Contract must be paused for emergency withdraw")
	}

	_, isOwner := getOwner()
	if !isOwner {
		sdk.Abort("Only owner can emergency withdraw")
	}

	if payload == nil || *payload == "" {
		sdk.Abort("Payload required")
	}

	var p EmergencyWithdrawPayload
	r := jlexer.Lexer{Data: []byte(*payload)}
	p.UnmarshalTinyJSON(&r)
	if r.Error() != nil {
		sdk.Abort("Invalid payload")
	}

	if p.Contract == "" || p.To == "" || p.Amount == "" {
		sdk.Abort("Contract, to, and amount required")
	}
	assertValidAccount(p.To)
	if p.TokenType == "nft" {
		// NFTs in the market are ALWAYS escrowed for an active entity
		// (auction lot, rental lock, bundle lot, mint-spot delegated mint
		// hasn't transferred until purchase). There is no legitimate "NFT
		// dust" scenario — anyone sending an NFT here intends it for
		// escrow. Allowing the owner to pull arbitrary NFTs out lets a
		// compromised/rogue owner steal live escrows, which the cancel
		// paths (still callable while paused) already cover for refunds.
		sdk.Abort("Emergency NFT withdraw disabled; use cancel paths to release escrowed NFTs")
	} else if p.TokenType == "token" {
		// Block withdrawals of any currently-whitelisted payment token —
		// those funds back active offer/auction/swap/rental/mint-spot
		// escrows that users can still reclaim via cancel/refund paths
		// while paused. Only non-whitelisted accidentally-sent tokens
		// (true dust) are recoverable here.
		if isPaymentTokenAllowedCheck(p.Contract) {
			sdk.Abort("Cannot emergency-withdraw an active payment token; remove it from the whitelist first")
		}
		tokenTransferBig(p.Contract, p.To, parseMoney(p.Amount))
	} else {
		sdk.Abort("Token type must be 'nft' or 'token'")
	}
	emitEmergencyWithdraw(p.TokenType, p.Contract, p.TokenId, p.Amount, p.To)
	return jsonResponse(&SuccessResponse{Success: true})
}

//go:wasmexport setMinBidIncrement
func SetMinBidIncrement(payload *string) *string {
	assertInit()

	_, isOwner := getOwner()
	if !isOwner {
		sdk.Abort("Only owner can set minimum bid increment")
	}

	if payload == nil || *payload == "" {
		sdk.Abort("Payload required")
	}

	var p SetMinBidIncrementPayload
	r := jlexer.Lexer{Data: []byte(*payload)}
	p.UnmarshalTinyJSON(&r)
	if r.Error() != nil {
		sdk.Abort("Invalid payload")
	}

	if p.MinBidIncrementBps > 10000 {
		sdk.Abort("Min bid increment must be <= 10000 basis points")
	}
	// Floor at 100 bps (1%) so that an English auction with anti-snipe
	// cannot be indefinitely extended by an attacker placing `+1` bids
	// each cycle at near-zero capital cost. The geometric growth caps
	// the grief budget at roughly (1.01)^n * startPrice.
	if p.MinBidIncrementBps < 100 {
		sdk.Abort("Min bid increment must be >= 100 basis points (1%)")
	}

	setMinBidIncrementBps(p.MinBidIncrementBps)
	return jsonResponse(&SuccessResponse{Success: true})
}

//go:wasmexport setAntiSnipeBlocks
func SetAntiSnipeBlocks(payload *string) *string {
	assertInit()

	_, isOwner := getOwner()
	if !isOwner {
		sdk.Abort("Only owner can set anti-snipe blocks")
	}

	if payload == nil || *payload == "" {
		sdk.Abort("Payload required")
	}

	var p SetAntiSnipePayload
	r := jlexer.Lexer{Data: []byte(*payload)}
	p.UnmarshalTinyJSON(&r)
	if r.Error() != nil {
		sdk.Abort("Invalid payload")
	}

	setAntiSnipeBlocksState(p.AntiSnipeBlocks)
	return jsonResponse(&SuccessResponse{Success: true})
}

// ===================================
// C2: Floor Sweep
// ===================================

//go:wasmexport sweep
func Sweep(payload *string) *string {
	assertInit()
	assertNotPaused()

	caller := getCaller()

	if payload == nil || *payload == "" {
		sdk.Abort("Payload required")
	}

	var p SweepPayload
	r := jlexer.Lexer{Data: []byte(*payload)}
	p.UnmarshalTinyJSON(&r)
	if r.Error() != nil {
		sdk.Abort("Invalid payload")
	}

	if p.NftContract == "" {
		sdk.Abort("NFT contract required")
	}
	if len(p.ListingIds) == 0 {
		sdk.Abort("At least one listing required")
	}

	maxTotal := parseMoney(p.MaxTotal)

	// A sweep spends ONE asset. Money here is a bare integer with no currency
	// attached (see parseMoney), while each buy escrows in whatever token its
	// own listing was priced in — so without this, the loop below would add
	// prices denominated in different currencies into a single `total`,
	// compare that to a cap belonging to none of them, and then pull every
	// one of those assets. The cap would look like a budget and bound
	// nothing. Callers that omit paymentToken get the first listing's, which
	// keeps single-token sweeps working and turns mixed ones into an abort.
	payToken := p.PaymentToken
	if payToken == "" {
		payToken = getListingField(p.ListingIds[0], "pt")
	}

	// FIRST PASS: validate all listings and accumulate total cost.
	total := mZero()
	for _, id := range p.ListingIds {
		if !isListingActive(id) {
			sdk.Abort("Listing not active")
		}
		if isExpired(getListingUint64(id, "exp")) {
			sdk.Abort("Listing has expired")
		}
		if getListingField(id, "nc") != p.NftContract {
			sdk.Abort("Listing not in collection")
		}
		if getListingField(id, "pt") != payToken {
			sdk.Abort("Listing not in payment token")
		}
		cost := mMulU64(getListingMoney(id, "p"), getListingUint64(id, "a"))
		total = mAdd(total, cost)
	}

	if mCmp(total, maxTotal) > 0 {
		sdk.Abort("Sweep exceeds maxTotal")
	}

	// SECOND PASS: execute each buy (reuses balance-delta/fee/royalty-splits/denylist/atomic-revert).
	for _, id := range p.ListingIds {
		doBuy(caller, &BuyPayload{ListingId: id, Amount: getListingUint64(id, "a")})
	}

	emitSwept(caller, uint64(len(p.ListingIds)), formatMoney(total), payToken)
	return jsonResponse(&SuccessResponse{Success: true})
}

// ===================================
// C3: Bundle Functions
// ===================================

//go:wasmexport listBundle
func ListBundle(payload *string) *string {
	assertInit()
	assertNotPaused()

	caller := getCaller()

	if payload == nil || *payload == "" {
		sdk.Abort("Payload required")
	}

	var p ListBundlePayload
	r := jlexer.Lexer{Data: []byte(*payload)}
	p.UnmarshalTinyJSON(&r)
	if r.Error() != nil {
		sdk.Abort("Invalid payload")
	}

	if p.NftContract == "" || p.PaymentToken == "" {
		sdk.Abort("NFT contract and payment token required")
	}
	if len(p.Items) == 0 {
		sdk.Abort("At least one item required")
	}
	if len(p.Items) > 20 {
		sdk.Abort("Too many bundle items")
	}
	for _, item := range p.Items {
		if item.TokenId == "" {
			sdk.Abort("Token ID required for each bundle item")
		}
		if item.Amount == 0 {
			sdk.Abort("Amount must be greater than zero for each bundle item")
		}
		assertValidTokenId(item.TokenId)
	}

	price := parseMoney(p.Price)
	if mIsZero(price) {
		sdk.Abort("Price must be greater than zero")
	}

	assertPaymentTokenAllowed(p.PaymentToken)
	assertCollectionAllowed(p.NftContract)

	// Like listings, bundles are approval-custody (no escrow) so the
	// buy-time transfer is seller→buyer; the collection owner can include
	// their own soulbound editions, a non-owner can't.
	bundleSellerIsOwner := nftGetOwner(p.NftContract) == caller

	// Approval-custody preflight for every item
	contractAddr := getContractAddress()
	for _, item := range p.Items {
		if nftIsSoulbound(p.NftContract, item.TokenId) && !bundleSellerIsOwner {
			sdk.Abort("Cannot list soulbound tokens (only the collection owner can transfer them)")
		}
		if !nftIsApprovedForAll(p.NftContract, caller, contractAddr) {
			if nftAllowanceOf(p.NftContract, caller, contractAddr, item.TokenId) < item.Amount {
				sdk.Abort("Marketplace not approved as operator or sufficient per-token allowance for this NFT collection")
			}
		}
		if nftBalanceOf(p.NftContract, caller, item.TokenId) < item.Amount {
			sdk.Abort("Insufficient NFT balance to list")
		}
	}

	feeBps := getEffectiveFeeBps(p.NftContract)
	royaltyBps := getRoyaltyBps(p.NftContract)
	if feeBps+royaltyBps > 10000 {
		sdk.Abort("Combined fee and royalty exceed 100%")
	}

	id := getNextBundleId()
	setBundleField(id, "s", caller)
	setBundleField(id, "nc", p.NftContract)
	setBundleField(id, "pt", p.PaymentToken)
	setBundleMoney(id, "p", price)
	setBundleField(id, "act", "1")
	setBundleUint64(id, "exp", p.ExpirationBlock)
	setBundleUint64(id, "n", uint64(len(p.Items)))
	for i, item := range p.Items {
		is := strconv.FormatUint(uint64(i), 10)
		setBundleField(id, is+"_ti", item.TokenId)
		setBundleUint64(id, is+"_amt", item.Amount)
	}
	setBundleUint64(id, "fb", feeBps)
	setBundleUint64(id, "rb", royaltyBps)
	setBundleField(id, "rr", getRoyaltyRecipient(p.NftContract))
	// Snapshot resolved royalty splits so in-flight bundles are unaffected by later split changes.
	snapRecips, snapBps := resolveRoyaltySplits(p.NftContract)
	snapshotRoyaltySplitsForBundle(id, snapRecips, snapBps)
	setNextBundleId(id + 1)

	emitBundleListed(id, caller, p.NftContract, uint64(len(p.Items)), formatMoney(price))
	return jsonResponse(&CreatedResponse{Success: true, Id: id})
}

//go:wasmexport buyBundle
func BuyBundle(payload *string) *string {
	assertInit()
	assertNotPaused()

	caller := getCaller()

	if payload == nil || *payload == "" {
		sdk.Abort("Payload required")
	}

	var p BundleIdPayload
	r := jlexer.Lexer{Data: []byte(*payload)}
	p.UnmarshalTinyJSON(&r)
	if r.Error() != nil {
		sdk.Abort("Invalid payload")
	}

	if !isBundleActive(p.BundleId) {
		sdk.Abort("Bundle not active")
	}
	if isExpired(getBundleUint64(p.BundleId, "exp")) {
		sdk.Abort("Bundle has expired")
	}

	nc := getBundleField(p.BundleId, "nc")
	assertCollectionAllowed(nc)

	seller := getBundleField(p.BundleId, "s")
	if caller == seller {
		sdk.Abort("Seller cannot buy own bundle")
	}

	pt := getBundleField(p.BundleId, "pt")
	// Re-validate at buy-time (de-whitelisted-after-list halt).
	assertPaymentTokenAllowed(pt)
	price := getBundleMoney(p.BundleId, "p")

	// CEI (F5 template): flip `act=0` BEFORE any external call (including
	// `escrowIn`). A re-entry through a malicious whitelisted paymentToken
	// would otherwise see act=1 and let the same bundle be bought twice
	// in nested frames; this ordering plus tx-revert-on-abort closes the
	// window.
	setBundleField(p.BundleId, "act", "0")

	received := escrowIn(pt, caller, price)

	lockedFeeBps := getBundleUint64(p.BundleId, "fb")
	lockedRoyaltyBps := getBundleUint64(p.BundleId, "rb") // locked at list time, not live
	royaltyRecipient := getBundleField(p.BundleId, "rr")

	// Load royalty split snapshot; fall back to legacy single-entry for pre-B2 in-flight entries.
	snapRecips, snapBps := loadBundleRoyaltySplitSnapshot(p.BundleId, royaltyRecipient, lockedRoyaltyBps)
	fee, royTot, sellerPayment := feeAndRoyaltyOf(received, lockedFeeBps, snapRecips, snapBps)

	// Transfer ALL items seller -> buyer BEFORE any payouts (atomic: any abort reverts whole tx)
	n := getBundleUint64(p.BundleId, "n")
	for i := uint64(0); i < n; i++ {
		is := strconv.FormatUint(i, 10)
		ti := getBundleField(p.BundleId, is+"_ti")
		amt := getBundleUint64(p.BundleId, is+"_amt")
		nftSafeTransferFrom(nc, seller, caller, ti, amt)
	}

	// Payouts
	if !mIsZero(fee) {
		tokenTransferBig(pt, getFeeRecipient(), fee)
	}
	distributeRoyaltySplitsResolved(pt, received, snapRecips, snapBps)
	if !mIsZero(sellerPayment) {
		tokenTransferBig(pt, seller, sellerPayment)
	}

	emitBundleBought(p.BundleId, caller, formatMoney(received), formatMoney(fee), formatMoney(royTot))
	return jsonResponse(&SuccessResponse{Success: true})
}

//go:wasmexport delistBundle
func DelistBundle(payload *string) *string {
	assertInit()
	// Note: delistBundle is intentionally allowed when paused so sellers can
	// cancel bundles freely during a contract pause (no NFT is escrowed).

	caller := getCaller()

	if payload == nil || *payload == "" {
		sdk.Abort("Payload required")
	}

	var p BundleIdPayload
	r := jlexer.Lexer{Data: []byte(*payload)}
	p.UnmarshalTinyJSON(&r)
	if r.Error() != nil {
		sdk.Abort("Invalid payload")
	}

	if !isBundleActive(p.BundleId) {
		sdk.Abort("Bundle not active")
	}

	seller := getBundleField(p.BundleId, "s")
	if caller != seller {
		sdk.Abort("Only seller can delist bundle")
	}

	setBundleField(p.BundleId, "act", "0")
	emitBundleDelisted(p.BundleId, seller)
	return jsonResponse(&SuccessResponse{Success: true})
}

//go:wasmexport getBundle
func GetBundle(payload *string) *string {
	assertInit()

	if payload == nil || *payload == "" {
		sdk.Abort("Payload required")
	}

	var p BundleIdPayload
	r := jlexer.Lexer{Data: []byte(*payload)}
	p.UnmarshalTinyJSON(&r)
	if r.Error() != nil {
		sdk.Abort("Invalid payload")
	}

	seller := getBundleField(p.BundleId, "s")
	if seller == "" {
		sdk.Abort("Bundle not found")
	}

	n := getBundleUint64(p.BundleId, "n")
	items := make([]BundleItem, n)
	for i := uint64(0); i < n; i++ {
		is := strconv.FormatUint(i, 10)
		items[i] = BundleItem{
			TokenId: getBundleField(p.BundleId, is+"_ti"),
			Amount:  getBundleUint64(p.BundleId, is+"_amt"),
		}
	}

	return jsonResponse(&BundleResponse{
		BundleId:        p.BundleId,
		Seller:          seller,
		NftContract:     getBundleField(p.BundleId, "nc"),
		Items:           items,
		PaymentToken:    getBundleField(p.BundleId, "pt"),
		Price:           formatMoney(getBundleMoney(p.BundleId, "p")),
		Active:          isBundleActive(p.BundleId),
		ExpirationBlock: getBundleUint64(p.BundleId, "exp"),
	})
}

// ===================================
// Batch Functions
// ===================================

//go:wasmexport batchList
func BatchList(payload *string) *string {
	assertInit()
	assertNotPaused()

	caller := getCaller()

	if payload == nil || *payload == "" {
		sdk.Abort("Payload required")
	}

	var p BatchListPayload
	r := jlexer.Lexer{Data: []byte(*payload)}
	p.UnmarshalTinyJSON(&r)
	if r.Error() != nil {
		sdk.Abort("Invalid payload")
	}

	if len(p.Items) == 0 {
		sdk.Abort("At least one item required")
	}
	// Cap batch size so a single tx can't exceed the rc_limit (each item is a
	// full doList: ~16 state writes + cross-contract reads). Mirrors the
	// bundle cap. Sweep is intentionally left uncapped.
	if len(p.Items) > 20 {
		sdk.Abort("Too many items (max 20)")
	}

	for i := range p.Items {
		doList(caller, &p.Items[i])
	}

	return jsonResponse(&SuccessResponse{Success: true})
}

//go:wasmexport batchBuy
func BatchBuy(payload *string) *string {
	assertInit()
	assertNotPaused()

	caller := getCaller()

	if payload == nil || *payload == "" {
		sdk.Abort("Payload required")
	}

	var p BatchBuyPayload
	r := jlexer.Lexer{Data: []byte(*payload)}
	p.UnmarshalTinyJSON(&r)
	if r.Error() != nil {
		sdk.Abort("Invalid payload")
	}

	if len(p.Items) == 0 {
		sdk.Abort("At least one item required")
	}
	// Cap batch size so a single tx can't exceed the rc_limit (each item is a
	// full doBuy: ~16 state reads + 6-8 cross-contract calls). Mirrors the
	// bundle cap. Sweep is intentionally left uncapped.
	if len(p.Items) > 20 {
		sdk.Abort("Too many items (max 20)")
	}

	for i := range p.Items {
		doBuy(caller, &p.Items[i])
	}

	return jsonResponse(&SuccessResponse{Success: true})
}

// ===================================
// Query Functions
// ===================================

//go:wasmexport getListing
func GetListing(payload *string) *string {
	assertInit()

	if payload == nil || *payload == "" {
		sdk.Abort("Payload required")
	}

	var p ListingIdPayload
	r := jlexer.Lexer{Data: []byte(*payload)}
	p.UnmarshalTinyJSON(&r)
	if r.Error() != nil {
		sdk.Abort("Invalid payload")
	}

	seller := getListingField(p.ListingId, "s")
	if seller == "" {
		sdk.Abort("Listing not found")
	}

	return jsonResponse(&ListingResponse{
		ListingId:       p.ListingId,
		Seller:          seller,
		NftContract:     getListingField(p.ListingId, "nc"),
		TokenId:         getListingField(p.ListingId, "ti"),
		Amount:          getListingUint64(p.ListingId, "a"),
		PricePerUnit:    formatMoney(getListingMoney(p.ListingId, "p")),
		PaymentToken:    getListingField(p.ListingId, "pt"),
		Active:          isListingActive(p.ListingId),
		ExpirationBlock: getListingUint64(p.ListingId, "exp"),
		FeeBps:          getListingUint64(p.ListingId, "fb"),
		RoyaltyBps:      getListingUint64(p.ListingId, "rb"),
		StartBlock:      getListingUint64(p.ListingId, "sb"),
	})
}

//go:wasmexport getOffer
func GetOffer(payload *string) *string {
	assertInit()

	if payload == nil || *payload == "" {
		sdk.Abort("Payload required")
	}

	var p OfferIdPayload
	r := jlexer.Lexer{Data: []byte(*payload)}
	p.UnmarshalTinyJSON(&r)
	if r.Error() != nil {
		sdk.Abort("Invalid payload")
	}

	buyer := getOfferField(p.OfferId, "b")
	if buyer == "" {
		sdk.Abort("Offer not found")
	}

	return jsonResponse(&OfferResponse{
		OfferId:         p.OfferId,
		Buyer:           buyer,
		NftContract:     getOfferField(p.OfferId, "nc"),
		TokenId:         getOfferField(p.OfferId, "ti"),
		Amount:          getOfferUint64(p.OfferId, "a"),
		PricePerUnit:    formatMoney(getOfferMoney(p.OfferId, "p")),
		PaymentToken:    getOfferField(p.OfferId, "pt"),
		Active:          isOfferActive(p.OfferId),
		ExpirationBlock: getOfferUint64(p.OfferId, "exp"),
		FeeBps:          getOfferUint64(p.OfferId, "fb"),
		RoyaltyBps:      getOfferUint64(p.OfferId, "rb"),
		IsCollection:    isCollectionOffer(p.OfferId),
	})
}

//go:wasmexport getInfo
func GetInfo(payload *string) *string {
	assertInit()
	return jsonResponse(&InfoResponse{
		Owner:              getStringState("owner"),
		FeeBps:             getFeeBps(),
		FeeRecipient:       getFeeRecipient(),
		Paused:             isPaused(),
		MinOffer:           formatMoney(getMinOfferMoney()),
		MinBidIncrementBps: getMinBidIncrementBps(),
		AntiSnipeBlocks:    getAntiSnipeBlocks(),
	})
}

//go:wasmexport getOwner
func GetOwner(payload *string) *string {
	assertInit()
	return jsonResponse(&OwnerResponse{Owner: getStringState("owner")})
}

//go:wasmexport isPaused
func IsPaused(payload *string) *string {
	assertInit()
	return jsonResponse(&PausedResponse{Paused: isPaused()})
}

//go:wasmexport getRoyalty
func GetRoyalty(payload *string) *string {
	assertInit()

	if payload == nil || *payload == "" {
		sdk.Abort("Payload required")
	}

	var p GetRoyaltyPayload
	r := jlexer.Lexer{Data: []byte(*payload)}
	p.UnmarshalTinyJSON(&r)
	if r.Error() != nil {
		sdk.Abort("Invalid payload")
	}

	if p.NftContract == "" {
		sdk.Abort("NFT contract required")
	}

	return jsonResponse(&RoyaltyResponse{
		NftContract:      p.NftContract,
		RoyaltyBps:       getRoyaltyBps(p.NftContract),
		RoyaltyRecipient: getRoyaltyRecipient(p.NftContract),
	})
}

//go:wasmexport getMinOffer
func GetMinOffer(payload *string) *string {
	assertInit()
	return jsonResponse(&MinOfferResponse{MinOffer: formatMoney(getMinOfferMoney())})
}

//go:wasmexport isPaymentTokenAllowed
func IsPaymentTokenAllowed(payload *string) *string {
	assertInit()

	if payload == nil || *payload == "" {
		sdk.Abort("Payload required")
	}

	var p PaymentTokenPayload
	r := jlexer.Lexer{Data: []byte(*payload)}
	p.UnmarshalTinyJSON(&r)
	if r.Error() != nil {
		sdk.Abort("Invalid payload")
	}

	return jsonResponse(&PaymentTokenAllowedResponse{Allowed: isPaymentTokenAllowedCheck(p.Token)})
}

//go:wasmexport denyCollection
func DenyCollection(payload *string) *string {
	assertInit()

	_, isOwner := getOwner()
	if !isOwner {
		sdk.Abort("Only owner can deny collection")
	}

	if payload == nil || *payload == "" {
		sdk.Abort("Payload required")
	}
	var p CollectionPayload
	r := jlexer.Lexer{Data: []byte(*payload)}
	p.UnmarshalTinyJSON(&r)
	if r.Error() != nil {
		sdk.Abort("Invalid payload")
	}
	if p.NftContract == "" {
		sdk.Abort("NFT contract required")
	}

	setCollectionDenied(p.NftContract)
	emitCollectionDenied(p.NftContract, getCaller())
	return jsonResponse(&SuccessResponse{Success: true})
}

//go:wasmexport allowCollection
func AllowCollection(payload *string) *string {
	assertInit()

	_, isOwner := getOwner()
	if !isOwner {
		sdk.Abort("Only owner can allow collection")
	}

	if payload == nil || *payload == "" {
		sdk.Abort("Payload required")
	}
	var p CollectionPayload
	r := jlexer.Lexer{Data: []byte(*payload)}
	p.UnmarshalTinyJSON(&r)
	if r.Error() != nil {
		sdk.Abort("Invalid payload")
	}
	if p.NftContract == "" {
		sdk.Abort("NFT contract required")
	}

	clearCollectionDenied(p.NftContract)
	emitCollectionAllowed(p.NftContract, getCaller())
	return jsonResponse(&SuccessResponse{Success: true})
}

//go:wasmexport isCollectionDenied
func IsCollectionDenied(payload *string) *string {
	assertInit()

	if payload == nil || *payload == "" {
		sdk.Abort("Payload required")
	}
	var p CollectionPayload
	r := jlexer.Lexer{Data: []byte(*payload)}
	p.UnmarshalTinyJSON(&r)
	if r.Error() != nil {
		sdk.Abort("Invalid payload")
	}

	return jsonResponse(&CollectionDeniedResponse{Denied: isCollectionDenied(p.NftContract)})
}

// ===================================
// D1: NFT-for-NFT Swap Functions
// ===================================

//go:wasmexport proposeSwap
func ProposeSwap(payload *string) *string {
	assertInit()
	assertNotPaused()

	caller := getCaller()

	if payload == nil || *payload == "" {
		sdk.Abort("Payload required")
	}

	var p ProposeSwapPayload
	r := jlexer.Lexer{Data: []byte(*payload)}
	p.UnmarshalTinyJSON(&r)
	if r.Error() != nil {
		sdk.Abort("Invalid payload")
	}

	if p.OfferedNft == "" || p.OfferedTokenId == "" {
		sdk.Abort("Offered NFT and token ID required")
	}
	if p.WantedNft == "" || p.WantedTokenId == "" {
		sdk.Abort("Wanted NFT and token ID required")
	}
	if p.OfferedAmount == 0 {
		sdk.Abort("Offered amount must be greater than zero")
	}
	if p.WantedAmount == 0 {
		sdk.Abort("Wanted amount must be greater than zero")
	}
	assertValidTokenId(p.OfferedTokenId)
	assertValidTokenId(p.WantedTokenId)

	assertCollectionAllowed(p.OfferedNft)

	// Proposer preflight on offered NFT
	contractAddr := getContractAddress()
	if !nftIsApprovedForAll(p.OfferedNft, caller, contractAddr) {
		if nftAllowanceOf(p.OfferedNft, caller, contractAddr, p.OfferedTokenId) < p.OfferedAmount {
			sdk.Abort("Marketplace not approved as operator or sufficient per-token allowance for this NFT collection")
		}
	}
	if nftBalanceOf(p.OfferedNft, caller, p.OfferedTokenId) < p.OfferedAmount {
		sdk.Abort("Insufficient NFT balance")
	}

	// TopUp validation: topUp may be "0"; if > 0 require TopUpToken + assertPaymentTokenAllowed
	topUp := parseMoney(p.TopUp)
	if !mIsZero(topUp) {
		if p.TopUpToken == "" {
			sdk.Abort("TopUpToken required when topUp > 0")
		}
		assertPaymentTokenAllowed(p.TopUpToken)
	}

	// Store swap state — NO escrow at propose (top-up pulled only at accept)
	id := getNextSwapId()
	setSwapField(id, "p", caller)
	setSwapField(id, "on", p.OfferedNft)
	setSwapField(id, "oti", p.OfferedTokenId)
	setSwapUint64(id, "oa", p.OfferedAmount)
	setSwapField(id, "wn", p.WantedNft)
	setSwapField(id, "wti", p.WantedTokenId)
	setSwapUint64(id, "wa", p.WantedAmount)
	setSwapMoney(id, "tu", topUp)
	setSwapField(id, "tt", p.TopUpToken)
	setSwapField(id, "act", "1")
	setSwapUint64(id, "exp", p.ExpirationBlock)
	// Lock fee + royalty for the offered collection at propose time, so a
	// later admin/owner change cannot retroactively shift payouts. Royalty
	// applies on the top-up only (the barter sides aren't priced).
	lockedFeeBps := getEffectiveFeeBps(p.OfferedNft)
	lockedRoyaltyBps := getRoyaltyBps(p.OfferedNft)
	if lockedFeeBps+lockedRoyaltyBps > 10000 {
		sdk.Abort("Combined fee and royalty exceed 100%")
	}
	setSwapUint64(id, "fb", lockedFeeBps)
	setSwapUint64(id, "rb", lockedRoyaltyBps)
	setSwapField(id, "rr", getRoyaltyRecipient(p.OfferedNft))
	swapSnapRecips, swapSnapBps := resolveRoyaltySplits(p.OfferedNft)
	snapshotRoyaltySplitsForSwap(id, swapSnapRecips, swapSnapBps)
	setNextSwapId(id + 1)

	emitSwapProposed(id, caller, p.OfferedNft, p.WantedNft)
	return jsonResponse(&CreatedResponse{Success: true, Id: id})
}

//go:wasmexport acceptSwap
func AcceptSwap(payload *string) *string {
	assertInit()
	assertNotPaused()

	acceptor := getCaller()

	if payload == nil || *payload == "" {
		sdk.Abort("Payload required")
	}

	var p SwapIdPayload
	r := jlexer.Lexer{Data: []byte(*payload)}
	p.UnmarshalTinyJSON(&r)
	if r.Error() != nil {
		sdk.Abort("Invalid payload")
	}

	if !isSwapActive(p.SwapId) {
		sdk.Abort("Swap not active")
	}
	if isExpired(getSwapUint64(p.SwapId, "exp")) {
		sdk.Abort("Swap has expired")
	}

	proposer := getSwapField(p.SwapId, "p")
	if acceptor == proposer {
		sdk.Abort("Cannot accept own swap")
	}

	on := getSwapField(p.SwapId, "on")   // offered NFT
	oti := getSwapField(p.SwapId, "oti") // offered token id
	oa := getSwapUint64(p.SwapId, "oa")  // offered amount
	wn := getSwapField(p.SwapId, "wn")   // wanted NFT
	wti := getSwapField(p.SwapId, "wti") // wanted token id
	wa := getSwapUint64(p.SwapId, "wa")  // wanted amount
	tu := getSwapMoney(p.SwapId, "tu")   // topUp money
	tt := getSwapField(p.SwapId, "tt")   // topUp token

	// Both collections must pass denylist check
	assertCollectionAllowed(on)
	assertCollectionAllowed(wn)

	// Preflight: proposer still holds + approved for offered NFT
	mkt := getContractAddress()
	if !nftIsApprovedForAll(on, proposer, mkt) {
		if nftAllowanceOf(on, proposer, mkt, oti) < oa {
			sdk.Abort("Proposer no longer holds offered NFT")
		}
	}
	if nftBalanceOf(on, proposer, oti) < oa {
		sdk.Abort("Proposer no longer holds offered NFT")
	}

	// Preflight: acceptor holds + approved for wanted NFT
	if !nftIsApprovedForAll(wn, acceptor, mkt) {
		if nftAllowanceOf(wn, acceptor, mkt, wti) < wa {
			sdk.Abort("Marketplace not approved as operator or sufficient per-token allowance for this NFT collection")
		}
	}
	if nftBalanceOf(wn, acceptor, wti) < wa {
		sdk.Abort("Insufficient NFT balance")
	}

	// CEI: flip act=0 BEFORE any external call. A malicious topUpToken
	// or either malicious NFT contract on the legs could re-enter
	// AcceptSwap; with act=0 set up front, the inner re-entry aborts at
	// the isSwapActive check and the tx remains single-execution.
	setSwapField(p.SwapId, "act", "0")

	// TopUp escrow + fee + royalty distribution. Both fee bps and the
	// royalty recipient list are read from the snapshot taken at propose
	// time so that mid-flow admin/owner changes can't retroactively
	// shift the proposer's payout. Royalty applies to the top-up — the
	// barter sides aren't priced, so trading-as-swap-with-cash no longer
	// bypasses creator royalty.
	// Any abort reverts the whole tx → atomic.
	if !mIsZero(tu) {
		received := escrowIn(tt, proposer, tu)
		lockedFeeBps := getSwapUint64(p.SwapId, "fb")
		lockedRoyaltyBps := getSwapUint64(p.SwapId, "rb")
		royaltyRecipient := getSwapField(p.SwapId, "rr")
		// Legacy fallback: pre-snapshot swaps (proposed before this patch)
		// have fb=0, rb=0, rr="" — the snapshot loader would then return
		// empty slices, charging zero fee and zero royalty. That's a
		// silent fee/royalty bypass on in-flight pre-patch swaps. Detect
		// and re-derive from live config in that case.
		legacy := getSwapUint64(p.SwapId, "rs_n") == 0 && lockedFeeBps == 0 && lockedRoyaltyBps == 0 && royaltyRecipient == ""
		if legacy {
			lockedFeeBps = getEffectiveFeeBps(on)
			lockedRoyaltyBps = getRoyaltyBps(on)
			royaltyRecipient = getRoyaltyRecipient(on)
		}
		swapSnapRecips, swapSnapBps := loadSwapRoyaltySplitSnapshot(p.SwapId, royaltyRecipient, lockedRoyaltyBps)
		if legacy {
			// loadSwapRoyaltySplitSnapshot returns the legacy single-entry
			// fallback when rs_n==0 — but only if fallbackBps>0 && fallbackRecip!=""
			// (which we've just sourced from live state). If a multi-split
			// resolves on the collection, prefer that.
			if mres, mbps := resolveRoyaltySplits(on); len(mres) > 0 {
				swapSnapRecips, swapSnapBps = mres, mbps
			}
		}
		fee, _, acceptorPay := feeAndRoyaltyOf(received, lockedFeeBps, swapSnapRecips, swapSnapBps)
		if !mIsZero(fee) {
			tokenTransferBig(tt, getFeeRecipient(), fee)
		}
		distributeRoyaltySplitsResolved(tt, received, swapSnapRecips, swapSnapBps)
		if !mIsZero(acceptorPay) {
			tokenTransferBig(tt, acceptor, acceptorPay)
		}
	}

	// Both NFT legs — any abort reverts whole tx
	nftSafeTransferFrom(on, proposer, acceptor, oti, oa)
	nftSafeTransferFrom(wn, acceptor, proposer, wti, wa)

	emitSwapAccepted(p.SwapId, proposer, acceptor)
	return jsonResponse(&SuccessResponse{Success: true})
}

//go:wasmexport cancelSwap
func CancelSwap(payload *string) *string {
	assertInit()
	// Note: cancelSwap works even when paused (like CancelOffer), so proposers can
	// cancel swaps freely during a contract pause. No escrow to refund.

	caller := getCaller()

	if payload == nil || *payload == "" {
		sdk.Abort("Payload required")
	}

	var p SwapIdPayload
	r := jlexer.Lexer{Data: []byte(*payload)}
	p.UnmarshalTinyJSON(&r)
	if r.Error() != nil {
		sdk.Abort("Invalid payload")
	}

	if !isSwapActive(p.SwapId) {
		sdk.Abort("Swap not active")
	}

	proposer := getSwapField(p.SwapId, "p")
	expBlock := getSwapUint64(p.SwapId, "exp")
	// Allow cancel if caller is proposer OR swap is expired
	if !isExpired(expBlock) && caller != proposer {
		sdk.Abort("Only proposer can cancel swap")
	}

	setSwapField(p.SwapId, "act", "0")
	emitSwapCancelled(p.SwapId, caller)
	return jsonResponse(&SuccessResponse{Success: true})
}

//go:wasmexport getSwap
func GetSwap(payload *string) *string {
	assertInit()

	if payload == nil || *payload == "" {
		sdk.Abort("Payload required")
	}

	var p SwapIdPayload
	r := jlexer.Lexer{Data: []byte(*payload)}
	p.UnmarshalTinyJSON(&r)
	if r.Error() != nil {
		sdk.Abort("Invalid payload")
	}

	proposer := getSwapField(p.SwapId, "p")
	if proposer == "" {
		sdk.Abort("Swap not found")
	}

	return jsonResponse(&SwapResponse{
		SwapId:          p.SwapId,
		Proposer:        proposer,
		OfferedNft:      getSwapField(p.SwapId, "on"),
		OfferedTokenId:  getSwapField(p.SwapId, "oti"),
		OfferedAmount:   getSwapUint64(p.SwapId, "oa"),
		WantedNft:       getSwapField(p.SwapId, "wn"),
		WantedTokenId:   getSwapField(p.SwapId, "wti"),
		WantedAmount:    getSwapUint64(p.SwapId, "wa"),
		TopUp:           formatMoney(getSwapMoney(p.SwapId, "tu")),
		TopUpToken:      getSwapField(p.SwapId, "tt"),
		Active:          isSwapActive(p.SwapId),
		ExpirationBlock: getSwapUint64(p.SwapId, "exp"),
	})
}

// ===================================
// B3: Per-Collection Fee Override Functions
// ===================================

//go:wasmexport setCollectionFee
func SetCollectionFee(payload *string) *string {
	assertInit()

	_, isOwner := getOwner()
	if !isOwner {
		sdk.Abort("Only owner can set collection fee")
	}

	if payload == nil || *payload == "" {
		sdk.Abort("Payload required")
	}
	var p CollectionFeePayload
	r := jlexer.Lexer{Data: []byte(*payload)}
	p.UnmarshalTinyJSON(&r)
	if r.Error() != nil {
		sdk.Abort("Invalid payload")
	}

	if p.NftContract == "" {
		sdk.Abort("NFT contract required")
	}
	if p.FeeBps > 10000 {
		sdk.Abort("Fee must be <= 10000 basis points")
	}

	setCollectionFeeState(p.NftContract, p.FeeBps)
	emitCollectionFeeSet(p.NftContract, p.FeeBps)
	return jsonResponse(&SuccessResponse{Success: true})
}

//go:wasmexport clearCollectionFee
func ClearCollectionFee(payload *string) *string {
	assertInit()

	_, isOwner := getOwner()
	if !isOwner {
		sdk.Abort("Only owner can set collection fee")
	}

	if payload == nil || *payload == "" {
		sdk.Abort("Payload required")
	}
	var p CollectionPayload
	r := jlexer.Lexer{Data: []byte(*payload)}
	p.UnmarshalTinyJSON(&r)
	if r.Error() != nil {
		sdk.Abort("Invalid payload")
	}

	if p.NftContract == "" {
		sdk.Abort("NFT contract required")
	}

	clearCollectionFeeState(p.NftContract)
	emitCollectionFeeCleared(p.NftContract)
	return jsonResponse(&SuccessResponse{Success: true})
}

//go:wasmexport getEffectiveFee
func GetEffectiveFee(payload *string) *string {
	assertInit()

	if payload == nil || *payload == "" {
		sdk.Abort("Payload required")
	}
	var p CollectionPayload
	r := jlexer.Lexer{Data: []byte(*payload)}
	p.UnmarshalTinyJSON(&r)
	if r.Error() != nil {
		sdk.Abort("Invalid payload")
	}

	return jsonResponse(&EffectiveFeeResponse{FeeBps: getEffectiveFeeBps(p.NftContract)})
}

// ===================================
// E1: NFT Rental Functions
// ===================================

//go:wasmexport listRental
func ListRental(payload *string) *string {
	assertInit()
	assertNotPaused()

	caller := getCaller()

	if payload == nil || *payload == "" {
		sdk.Abort("Payload required")
	}

	var p ListRentalPayload
	r := jlexer.Lexer{Data: []byte(*payload)}
	p.UnmarshalTinyJSON(&r)
	if r.Error() != nil {
		sdk.Abort("Invalid payload")
	}

	if p.NftContract == "" || p.TokenId == "" || p.PaymentToken == "" {
		sdk.Abort("NFT contract, token ID, and payment token required")
	}
	if p.Amount == 0 {
		sdk.Abort("Amount must be greater than zero")
	}
	assertValidTokenId(p.TokenId)

	ppb := parseMoney(p.PricePerBlock)
	if mIsZero(ppb) {
		sdk.Abort("Price per block must be greater than zero")
	}

	if p.MinBlocks == 0 || p.MinBlocks > p.MaxBlocks {
		sdk.Abort("Invalid block range")
	}

	assertPaymentTokenAllowed(p.PaymentToken)
	assertCollectionAllowed(p.NftContract)

	// Rentals escrow the NFT into the market at `rent` time; the market is
	// not the collection owner, so it could never return a soulbound NFT to
	// the owner at endRental (stranding it). Block soulbound unconditionally.
	if nftIsSoulbound(p.NftContract, p.TokenId) {
		sdk.Abort("Cannot rent out soulbound tokens (rentals escrow to the market, which can't transfer them back out)")
	}

	contractAddr := getContractAddress()
	if !nftIsApprovedForAll(p.NftContract, caller, contractAddr) {
		if nftAllowanceOf(p.NftContract, caller, contractAddr, p.TokenId) < p.Amount {
			sdk.Abort("Marketplace not approved as operator or sufficient per-token allowance for this NFT collection")
		}
	}
	if nftBalanceOf(p.NftContract, caller, p.TokenId) < p.Amount {
		sdk.Abort("Insufficient NFT balance to list")
	}

	feeBps := getEffectiveFeeBps(p.NftContract)
	royaltyBps := getRoyaltyBps(p.NftContract)
	if feeBps+royaltyBps > 10000 {
		sdk.Abort("Combined fee and royalty exceed 100%")
	}

	id := getNextRentalId()
	setRentalField(id, "o", caller)
	setRentalField(id, "nc", p.NftContract)
	setRentalField(id, "ti", p.TokenId)
	setRentalUint64(id, "amt", p.Amount)
	setRentalField(id, "pt", p.PaymentToken)
	setRentalMoney(id, "ppb", ppb)
	setRentalUint64(id, "minb", p.MinBlocks)
	setRentalUint64(id, "maxb", p.MaxBlocks)
	setRentalField(id, "act", "1")
	setRentalField(id, "rby", "")
	setRentalUint64(id, "until", 0)
	setRentalField(id, "rented", "0")
	setRentalUint64(id, "fb", feeBps)
	setRentalField(id, "rr", getRoyaltyRecipient(p.NftContract))
	setRentalUint64(id, "rb", royaltyBps)

	// Snapshot resolved royalty splits so in-flight rentals are unaffected by later split changes.
	snapRecips, snapBps := resolveRoyaltySplits(p.NftContract)
	snapshotRoyaltySplitsForRental(id, snapRecips, snapBps)
	setNextRentalId(id + 1)

	emitRentalListed(id, caller, p.NftContract, p.TokenId)
	return jsonResponse(&CreatedResponse{Success: true, Id: id})
}

//go:wasmexport rent
func Rent(payload *string) *string {
	assertInit()
	assertNotPaused()

	renter := getCaller()

	if payload == nil || *payload == "" {
		sdk.Abort("Payload required")
	}

	var p RentPayload
	r := jlexer.Lexer{Data: []byte(*payload)}
	p.UnmarshalTinyJSON(&r)
	if r.Error() != nil {
		sdk.Abort("Invalid payload")
	}

	if getRentalField(p.RentalId, "act") != "1" {
		sdk.Abort("Rental not active")
	}
	if getRentalField(p.RentalId, "rented") == "1" {
		sdk.Abort("Already rented")
	}

	minBlocks := getRentalUint64(p.RentalId, "minb")
	maxBlocks := getRentalUint64(p.RentalId, "maxb")
	if p.Blocks < minBlocks || p.Blocks > maxBlocks {
		sdk.Abort("Blocks out of range")
	}

	nc := getRentalField(p.RentalId, "nc")
	assertCollectionAllowed(nc)

	owner := getRentalField(p.RentalId, "o")
	if renter == owner {
		sdk.Abort("Owner cannot rent own listing")
	}

	pt := getRentalField(p.RentalId, "pt")
	// Re-validate at rent-time (de-whitelisted-after-list halt).
	assertPaymentTokenAllowed(pt)
	ti := getRentalField(p.RentalId, "ti")
	amt := getRentalUint64(p.RentalId, "amt")
	lockedFeeBps := getRentalUint64(p.RentalId, "fb")
	lockedRoyaltyBps := getRentalUint64(p.RentalId, "rb")
	royaltyRecipient := getRentalField(p.RentalId, "rr")

	// Prevent attestation-key collision: the `ract|<nc>|<ti>|<renter>`
	// slot is single-valued. Allowing the same renter to hold two
	// concurrent rentals on the same (collection, tokenId) — even from
	// different owners (editioned NFTs) — would alias one row's
	// attestation onto the other, so ending one wipes the survivor's
	// proof-of-rental. Disallow until the prior attestation expires.
	if existing := getUint64State("ract|" + nc + "|" + ti + "|" + renter); existing != 0 && getCurrentBlockHeight() < existing {
		sdk.Abort("Renter already has an active rental for this token")
	}

	cost := mMulU64(getRentalMoney(p.RentalId, "ppb"), p.Blocks)

	// CEI (F5 template): flip `rented`/`rby`/`until` + the `ract|` index
	// BEFORE any external call. Inner re-entry through a malicious
	// whitelisted paymentToken's transferFrom callback would otherwise
	// see rented=0 and run a second concurrent rental on the same row;
	// outer's stale local would then overwrite. This ordering plus
	// tx-revert-on-abort closes the window.
	until := getCurrentBlockHeight() + p.Blocks
	setRentalField(p.RentalId, "rby", renter)
	setRentalUint64(p.RentalId, "until", until)
	setRentalField(p.RentalId, "rented", "1")
	// Index: ract|<nc>|<ti>|<renter> = until
	setUint64State("ract|"+nc+"|"+ti+"|"+renter, until)

	received := escrowIn(pt, renter, cost)

	// Escrow NFT from owner into market contract.
	contractAddr := getContractAddress()
	nftSafeTransferFrom(nc, owner, contractAddr, ti, amt)

	// Load royalty split snapshot; fall back to legacy single-entry for pre-E1 in-flight entries.
	snapRecips, snapBps := loadRentalRoyaltySplitSnapshot(p.RentalId, royaltyRecipient, lockedRoyaltyBps)
	fee, _, ownerPay := feeAndRoyaltyOf(received, lockedFeeBps, snapRecips, snapBps)

	if !mIsZero(fee) {
		tokenTransferBig(pt, getFeeRecipient(), fee)
	}
	distributeRoyaltySplitsResolved(pt, received, snapRecips, snapBps)
	if !mIsZero(ownerPay) {
		tokenTransferBig(pt, owner, ownerPay)
	}

	emitRented(p.RentalId, renter, until)
	return jsonResponse(&SuccessResponse{Success: true})
}

//go:wasmexport endRental
func EndRental(payload *string) *string {
	assertInit()
	assertNotPaused()
	// Value-mover (returns escrowed NFT). Paused for symmetry with the
	// rest of the surface; the owner can unpause once the all-clear is
	// given and the NFT is then recoverable.

	if payload == nil || *payload == "" {
		sdk.Abort("Payload required")
	}

	var p RentalIdPayload
	r := jlexer.Lexer{Data: []byte(*payload)}
	p.UnmarshalTinyJSON(&r)
	if r.Error() != nil {
		sdk.Abort("Invalid payload")
	}

	if getRentalField(p.RentalId, "rented") != "1" {
		sdk.Abort("Not currently rented")
	}

	until := getRentalUint64(p.RentalId, "until")
	if getCurrentBlockHeight() < until {
		sdk.Abort("Rental term not over")
	}

	nc := getRentalField(p.RentalId, "nc")
	ti := getRentalField(p.RentalId, "ti")
	amt := getRentalUint64(p.RentalId, "amt")
	owner := getRentalField(p.RentalId, "o")
	prevRenter := getRentalField(p.RentalId, "rby")
	contractAddr := getContractAddress()

	// CEI: clear `rented`/`rby` + index BEFORE the NFT return so a
	// re-entry through a malicious nftContract cannot re-enter EndRental
	// and trigger a second `nftSafeTransferFrom` (and corrupt the rental
	// row by overwriting it with stale locals).
	setRentalField(p.RentalId, "rented", "0")
	setRentalField(p.RentalId, "rby", "")
	sdk.StateDeleteObject("ract|" + nc + "|" + ti + "|" + prevRenter)

	// Return escrowed NFT from market to owner.
	nftSafeTransferFrom(nc, contractAddr, owner, ti, amt)

	emitRentalEnded(p.RentalId, getCaller())
	return jsonResponse(&SuccessResponse{Success: true})
}

//go:wasmexport endRentalEarly
func EndRentalEarly(payload *string) *string {
	assertInit()
	assertNotPaused()

	caller := getCaller()

	if payload == nil || *payload == "" {
		sdk.Abort("Payload required")
	}

	var p RentalIdPayload
	r := jlexer.Lexer{Data: []byte(*payload)}
	p.UnmarshalTinyJSON(&r)
	if r.Error() != nil {
		sdk.Abort("Invalid payload")
	}

	if getRentalField(p.RentalId, "rented") != "1" {
		sdk.Abort("Not currently rented")
	}

	if caller != getRentalField(p.RentalId, "rby") {
		sdk.Abort("Only renter can end early")
	}

	nc := getRentalField(p.RentalId, "nc")
	ti := getRentalField(p.RentalId, "ti")
	amt := getRentalUint64(p.RentalId, "amt")
	owner := getRentalField(p.RentalId, "o")
	contractAddr := getContractAddress()

	// CEI: clear `rented`/`rby` + index BEFORE the NFT return.
	setRentalField(p.RentalId, "rented", "0")
	setRentalField(p.RentalId, "rby", "")
	sdk.StateDeleteObject("ract|" + nc + "|" + ti + "|" + caller)

	// Return escrowed NFT from market to owner. No refund of unused term.
	nftSafeTransferFrom(nc, contractAddr, owner, ti, amt)

	emitRentalEnded(p.RentalId, caller)
	return jsonResponse(&SuccessResponse{Success: true})
}

//go:wasmexport delistRental
func DelistRental(payload *string) *string {
	assertInit()
	// Works while paused — recovery path (mirrors Delist).

	caller := getCaller()

	if payload == nil || *payload == "" {
		sdk.Abort("Payload required")
	}

	var p RentalIdPayload
	r := jlexer.Lexer{Data: []byte(*payload)}
	p.UnmarshalTinyJSON(&r)
	if r.Error() != nil {
		sdk.Abort("Invalid payload")
	}

	if getRentalField(p.RentalId, "act") != "1" {
		sdk.Abort("Rental not active")
	}
	if caller != getRentalField(p.RentalId, "o") {
		sdk.Abort("Only owner can delist rental")
	}
	if getRentalField(p.RentalId, "rented") == "1" {
		sdk.Abort("Cannot delist while rented")
	}

	// No NFT movement needed — approval-custody, nothing escrowed when not rented.
	setRentalField(p.RentalId, "act", "0")

	emitRentalDelisted(p.RentalId, caller)
	return jsonResponse(&SuccessResponse{Success: true})
}

//go:wasmexport getRental
func GetRental(payload *string) *string {
	assertInit()

	if payload == nil || *payload == "" {
		sdk.Abort("Payload required")
	}

	var p RentalIdPayload
	r := jlexer.Lexer{Data: []byte(*payload)}
	p.UnmarshalTinyJSON(&r)
	if r.Error() != nil {
		sdk.Abort("Invalid payload")
	}

	owner := getRentalField(p.RentalId, "o")
	if owner == "" {
		sdk.Abort("Rental not found")
	}

	return jsonResponse(&RentalResponse{
		RentalId:      p.RentalId,
		Owner:         owner,
		NftContract:   getRentalField(p.RentalId, "nc"),
		TokenId:       getRentalField(p.RentalId, "ti"),
		Amount:        getRentalUint64(p.RentalId, "amt"),
		PaymentToken:  getRentalField(p.RentalId, "pt"),
		PricePerBlock: formatMoney(getRentalMoney(p.RentalId, "ppb")),
		MinBlocks:     getRentalUint64(p.RentalId, "minb"),
		MaxBlocks:     getRentalUint64(p.RentalId, "maxb"),
		Active:        getRentalField(p.RentalId, "act") == "1",
		Renter:        getRentalField(p.RentalId, "rby"),
		Until:         getRentalUint64(p.RentalId, "until"),
		Rented:        getRentalField(p.RentalId, "rented") == "1",
	})
}

//go:wasmexport getActiveRentalOf
func GetActiveRentalOf(payload *string) *string {
	assertInit()

	if payload == nil || *payload == "" {
		sdk.Abort("Payload required")
	}

	var p ActiveRentalQuery
	r := jlexer.Lexer{Data: []byte(*payload)}
	p.UnmarshalTinyJSON(&r)
	if r.Error() != nil {
		sdk.Abort("Invalid payload")
	}

	u := getUint64State("ract|" + p.NftContract + "|" + p.TokenId + "|" + p.Account)
	if u != 0 && getCurrentBlockHeight() < u {
		return jsonResponse(&ActiveRentalResponse{Active: true, Until: u})
	}
	return jsonResponse(&ActiveRentalResponse{Active: false, Until: 0})
}

// ===================================
// G2: Mint-Spot Primary Sale Entrypoints
// ===================================

//go:wasmexport listMintSpots
func ListMintSpots(payload *string) *string {
	assertInit()
	assertNotPaused()

	caller := getCaller()

	if payload == nil || *payload == "" {
		sdk.Abort("Payload required")
	}

	var p ListMintSpotsPayload
	r := jlexer.Lexer{Data: []byte(*payload)}
	p.UnmarshalTinyJSON(&r)
	if r.Error() != nil {
		sdk.Abort("Invalid payload")
	}

	nc := p.NftContract
	ti := p.TokenId
	pt := p.PaymentToken
	if nc == "" || ti == "" || pt == "" {
		sdk.Abort("NFT contract, token ID, and payment token required")
	}

	price := parseMoney(p.PricePerSpot)
	if mIsZero(price) {
		sdk.Abort("Price must be greater than zero")
	}

	assertPaymentTokenAllowed(pt)
	assertCollectionAllowed(nc)

	// tokenId is concatenated into the delegated-mint JSON payload — apply
	// the shared validator (same gate now used at every entrypoint).
	assertValidTokenId(ti)

	if nftGetOwner(nc) != caller {
		sdk.Abort("Only collection owner can list mint spots")
	}

	// Authorization: the marketplace must be able to delegated-mint this
	// edition. Two accepted modes (mirrors magi_nft Mint auth):
	//   1. operator approval (setApprovalForAll) — uncapped, maxSpots may be
	//      0 (unbounded, bounded only by the nft maxSupply).
	//   2. per-token ERC-6909 allowance (approve) — finite and decremented
	//      per mint by magi_nft, so the listing must declare an explicit
	//      maxSpots in (0, allowance].
	mkt := getContractAddress()
	if !nftIsApprovedForAll(nc, caller, mkt) {
		allowance := nftAllowanceOf(nc, caller, mkt, ti)
		if allowance == 0 {
			sdk.Abort("Marketplace not approved as operator or per-token allowance for this NFT collection")
		}
		if p.MaxSpots == 0 {
			sdk.Abort("maxSpots required when listing via per-token allowance")
		}
		if p.MaxSpots > allowance {
			sdk.Abort("maxSpots exceeds nft allowance")
		}
	}

	if nftMaxSupplyOf(nc, ti) == 0 {
		sdk.Abort("Edition not defined")
	}

	feeBps := getEffectiveFeeBps(nc)
	if feeBps > 10000 {
		sdk.Abort("Fee must be <= 10000 basis points")
	}

	id := getNextMintSpotId()
	setMintSpotField(id, "s", caller)
	setMintSpotField(id, "nc", nc)
	setMintSpotField(id, "ti", ti)
	setMintSpotField(id, "pt", pt)
	setMintSpotMoney(id, "p", price)
	setMintSpotUint64(id, "ms", p.MaxSpots)
	setMintSpotUint64(id, "sold", 0)
	setMintSpotField(id, "act", "1")
	setMintSpotUint64(id, "exp", p.ExpirationBlock)
	setMintSpotUint64(id, "sb", p.StartBlock)
	setMintSpotUint64(id, "fb", feeBps)
	setNextMintSpotId(id + 1)

	emitMintSpotsListed(id, caller, nc, ti, p.MaxSpots)
	return jsonResponse(&CreatedResponse{Success: true, Id: id})
}

//go:wasmexport buyMintSpot
func BuyMintSpot(payload *string) *string {
	assertInit()
	assertNotPaused()

	buyer := getCaller()

	if payload == nil || *payload == "" {
		sdk.Abort("Payload required")
	}

	var p BuyMintSpotPayload
	r := jlexer.Lexer{Data: []byte(*payload)}
	p.UnmarshalTinyJSON(&r)
	if r.Error() != nil {
		sdk.Abort("Invalid payload")
	}

	id := p.ListingId

	if !isMintSpotActive(id) {
		sdk.Abort("Mint spot listing not active")
	}
	if isExpired(getMintSpotUint64(id, "exp")) {
		sdk.Abort("Mint spot listing has expired")
	}
	if sb := getMintSpotUint64(id, "sb"); sb != 0 && getCurrentBlockHeight() < sb {
		sdk.Abort("Listing not started")
	}

	nc := getMintSpotField(id, "nc")
	assertCollectionAllowed(nc)

	if p.Amount == 0 {
		sdk.Abort("Amount must be greater than zero")
	}

	lister := getMintSpotField(id, "s")
	if buyer == lister {
		sdk.Abort("Lister cannot buy own mint spots")
	}

	ms := getMintSpotUint64(id, "ms")
	sold := getMintSpotUint64(id, "sold")
	// Overflow-safe cap check: `sold+p.Amount` would wrap for a huge p.Amount
	// and bypass the cap. The invariant sold ≤ ms holds (the listing closes at
	// sold==ms below), so ms-sold ≥ 0 and this form can't overflow.
	if ms != 0 && p.Amount > ms-sold {
		sdk.Abort("Exceeds listing mint-spot cap")
	}

	pt := getMintSpotField(id, "pt")
	price := getMintSpotMoney(id, "p")
	total := mMulU64(price, p.Amount)

	// CEI (F5 template): increment `sold`/flip `act` BEFORE any external
	// call (including `escrowIn`). Inner re-entry through a malicious
	// whitelisted paymentToken's transferFrom callback would otherwise
	// see the old `sold` and mint past the cap; outer's stale local
	// would then overwrite. This ordering plus tx-revert-on-abort closes
	// the window.
	newSold := sold + p.Amount
	setMintSpotUint64(id, "sold", newSold)
	if ms != 0 && newSold == ms {
		setMintSpotField(id, "act", "0")
	}

	received := escrowIn(pt, buyer, total)
	fee, _, creatorPay := feeAndRoyaltyOf(received, getMintSpotUint64(id, "fb"), nil, nil)

	// Delegated mint BEFORE payouts: if the nft aborts (maxSupply exceeded,
	// auth revoked, etc.) the whole tx reverts including the escrow leg —
	// buyer is fully refunded. Mirrors doBuy's NFT-before-payout ordering.
	ti := getMintSpotField(id, "ti")
	nftDelegatedMint(nc, buyer, ti, p.Amount)

	if !mIsZero(fee) {
		tokenTransferBig(pt, getFeeRecipient(), fee)
	}
	if !mIsZero(creatorPay) {
		tokenTransferBig(pt, lister, creatorPay)
	}

	emitMintSpotBought(id, buyer, p.Amount, formatMoney(received), formatMoney(fee))
	return jsonResponse(&SuccessResponse{Success: true})
}

//go:wasmexport delistMintSpots
func DelistMintSpots(payload *string) *string {
	assertInit()
	// Intentionally allowed while paused (recovery path, mirrors Delist).

	caller := getCaller()

	if payload == nil || *payload == "" {
		sdk.Abort("Payload required")
	}

	var p MintSpotIdPayload
	r := jlexer.Lexer{Data: []byte(*payload)}
	p.UnmarshalTinyJSON(&r)
	if r.Error() != nil {
		sdk.Abort("Invalid payload")
	}

	id := p.ListingId
	if !isMintSpotActive(id) {
		sdk.Abort("Mint spot listing not active")
	}

	lister := getMintSpotField(id, "s")
	if caller != lister {
		sdk.Abort("Only lister can delist mint spots")
	}

	setMintSpotField(id, "act", "0")
	emitMintSpotsDelisted(id, lister)
	return jsonResponse(&SuccessResponse{Success: true})
}

//go:wasmexport getMintSpotListing
func GetMintSpotListing(payload *string) *string {
	assertInit()

	if payload == nil || *payload == "" {
		sdk.Abort("Payload required")
	}

	var p MintSpotIdPayload
	r := jlexer.Lexer{Data: []byte(*payload)}
	p.UnmarshalTinyJSON(&r)
	if r.Error() != nil {
		sdk.Abort("Invalid payload")
	}

	id := p.ListingId
	lister := getMintSpotField(id, "s")
	if lister == "" {
		sdk.Abort("Mint spot listing not found")
	}

	return jsonResponse(&MintSpotListingResponse{
		ListingId:       id,
		Lister:          lister,
		NftContract:     getMintSpotField(id, "nc"),
		TokenId:         getMintSpotField(id, "ti"),
		PaymentToken:    getMintSpotField(id, "pt"),
		PricePerSpot:    formatMoney(getMintSpotMoney(id, "p")),
		MaxSpots:        getMintSpotUint64(id, "ms"),
		Sold:            getMintSpotUint64(id, "sold"),
		Active:          isMintSpotActive(id),
		ExpirationBlock: getMintSpotUint64(id, "exp"),
		StartBlock:      getMintSpotUint64(id, "sb"),
		FeeBps:          getMintSpotUint64(id, "fb"),
	})
}

