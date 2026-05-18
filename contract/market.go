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

	sdk.StateSetObject("isInit", "1")
	sdk.StateSetObject("owner", *caller)
	sdk.StateSetObject("paused", "0")
	setUint64State("fee_bps", p.FeeBps)
	setStringState("fee_rcpt", p.FeeRecipient)
	setNextListingId(0)
	setNextOfferId(0)
	setNextAuctionId(0)

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
	price := parseMoney(p.PricePerUnit)
	if mIsZero(price) {
		sdk.Abort("Price must be greater than zero")
	}

	assertPaymentTokenAllowed(p.PaymentToken)

	if nftIsSoulbound(p.NftContract, p.TokenId) {
		sdk.Abort("Cannot list soulbound tokens")
	}
	assertCollectionAllowed(p.NftContract)

	currentFeeBps := getEffectiveFeeBps(p.NftContract)
	currentRoyaltyBps := getRoyaltyBps(p.NftContract)
	if currentFeeBps+currentRoyaltyBps > 10000 {
		sdk.Abort("Combined fee and royalty exceed 100%")
	}

	contractAddr := getContractAddress()
	if !nftIsApprovedForAll(p.NftContract, caller, contractAddr) {
		sdk.Abort("Marketplace not approved as operator for this NFT collection")
	}
	if nftBalanceOf(p.NftContract, caller, p.TokenId) < p.Amount {
		sdk.Abort("Insufficient NFT balance to list")
	}

	// F1: validate payoutMode / payoutL1Address
	if p.PayoutMode == "unmap" {
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
	pricePerUnit := getListingMoney(p.ListingId, "p")
	nftContract := getListingField(p.ListingId, "nc")
	assertCollectionAllowed(nftContract)
	tokenId := getListingField(p.ListingId, "ti")
	lockedFeeBps := getListingUint64(p.ListingId, "fb")
	lockedRoyaltyBps := getListingUint64(p.ListingId, "rb")
	royaltyRecipient := getListingField(p.ListingId, "rr")

	totalCost := mMulU64(pricePerUnit, p.Amount)

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
	// Seller-leg: F1 unmap, F2 DEX-routed settlement, or legacy mapped-token transfer.
	pm := getListingField(p.ListingId, "pm")
	st := getListingField(p.ListingId, "st")
	if pm == "unmap" {
		if !mIsZero(sellerPayment) {
			unmapTo(paymentToken, getListingField(p.ListingId, "pl1"), sellerPayment)
		}
	} else if st != "" {
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

	newRemaining := safeSub(remaining, p.Amount)
	if newRemaining == 0 {
		setListingField(p.ListingId, "act", "0")
	}
	setListingUint64(p.ListingId, "a", newRemaining)

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

	// Escrow payment with balance-delta; store the ACTUAL received total so
	// cancel refunds and accept payouts can never over-distribute.
	received := escrowIn(p.PaymentToken, caller, totalOffer)

	// Create offer
	id := getNextOfferId()
	isCol := p.TokenId == ""

	setOfferField(id, "b", caller)
	setOfferField(id, "nc", p.NftContract)
	setOfferField(id, "ti", p.TokenId)
	setOfferUint64(id, "a", p.Amount)
	setOfferMoney(id, "p", price)
	setOfferMoney(id, "esc", received)
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
	setNextOfferId(id + 1)

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
	if !mIsZero(refund) {
		tokenTransferBig(paymentToken, buyer, refund)
	}
	setOfferField(p.OfferId, "act", "0")
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
	lockedFeeBps := getOfferUint64(offerId, "fb")
	lockedRoyaltyBps := getOfferUint64(offerId, "rb")
	royaltyRecipient := getOfferField(offerId, "rr")

	if acceptAmount == 0 {
		acceptAmount = offerAmount
	}
	if acceptAmount > offerAmount {
		sdk.Abort("Accept amount exceeds offer amount")
	}

	// Clean preflight instead of a raw cross-call abort.
	if !nftIsApprovedForAll(nftContract, caller, getContractAddress()) {
		sdk.Abort("Marketplace not approved as operator to fulfill offer")
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

	nftSafeTransferFrom(nftContract, caller, buyer, tokenId, acceptAmount)

	if !mIsZero(sellerPayment) {
		tokenTransferBig(paymentToken, caller, sellerPayment)
	}
	if !mIsZero(fee) {
		tokenTransferBig(paymentToken, getFeeRecipient(), fee)
	}
	distributeRoyaltySplitsResolved(paymentToken, totalPrice, offerSnapRecips, offerSnapBps)

	newRemaining := safeSub(offerAmount, acceptAmount)
	setOfferMoney(offerId, "esc", mSub(escrowed, totalPrice))
	if newRemaining == 0 {
		setOfferField(offerId, "act", "0")
	} else {
		setOfferUint64(offerId, "a", newRemaining)
	}

	emitOfferAccepted(offerId, caller, buyer, acceptAmount, formatMoney(totalPrice), formatMoney(fee), formatMoney(royTot), tokenId)
}

//go:wasmexport acceptOffer
func AcceptOffer(payload *string) *string {
	assertInit()
	// Note: acceptOffer works even when paused so sellers can finalize existing offers

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
	// Note: acceptCollectionOffer works even when paused so sellers can finalize existing offers

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

	setRoyaltyBps(p.NftContract, p.RoyaltyBps)
	setRoyaltyRecipientState(p.NftContract, p.RoyaltyRecipient)

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
		if split.Recipient == "" {
			sdk.Abort("Royalty split recipient required")
		}
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

	setPaymentTokenAllowed(p.Token, true)
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
	if p.TokenType == "nft" {
		if p.TokenId == "" {
			sdk.Abort("Token ID required for NFT withdraw")
		}
		qty := parseMoney(p.Amount)
		if !qty.IsUint64() {
			sdk.Abort("NFT amount too large")
		}
		nftSafeTransferFrom(p.Contract, getContractAddress(), p.To, p.TokenId, qty.Uint64())
	} else if p.TokenType == "token" {
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

	emitSwept(caller, uint64(len(p.ListingIds)), formatMoney(total))
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
	}

	price := parseMoney(p.Price)
	if mIsZero(price) {
		sdk.Abort("Price must be greater than zero")
	}

	assertPaymentTokenAllowed(p.PaymentToken)
	assertCollectionAllowed(p.NftContract)

	// Approval-custody preflight for every item
	contractAddr := getContractAddress()
	for _, item := range p.Items {
		if nftIsSoulbound(p.NftContract, item.TokenId) {
			sdk.Abort("Cannot list soulbound tokens")
		}
		if !nftIsApprovedForAll(p.NftContract, caller, contractAddr) {
			sdk.Abort("Marketplace not approved as operator for this NFT collection")
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
	price := getBundleMoney(p.BundleId, "p")

	received := escrowIn(pt, caller, price)

	lockedFeeBps := getBundleUint64(p.BundleId, "fb")
	lockedRoyaltyBps := getRoyaltyBps(nc) // for fallback in snapshot loader
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

	setBundleField(p.BundleId, "act", "0")
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

	assertCollectionAllowed(p.OfferedNft)

	// Proposer preflight on offered NFT
	contractAddr := getContractAddress()
	if !nftIsApprovedForAll(p.OfferedNft, caller, contractAddr) {
		sdk.Abort("Marketplace not approved as operator for this NFT collection")
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
		sdk.Abort("Proposer no longer holds offered NFT")
	}
	if nftBalanceOf(on, proposer, oti) < oa {
		sdk.Abort("Proposer no longer holds offered NFT")
	}

	// Preflight: acceptor holds + approved for wanted NFT
	if !nftIsApprovedForAll(wn, acceptor, mkt) {
		sdk.Abort("Marketplace not approved as operator for this NFT collection")
	}
	if nftBalanceOf(wn, acceptor, wti) < wa {
		sdk.Abort("Insufficient NFT balance")
	}

	// TopUp escrow + fee distribution (order: escrow+pay, then both NFT legs)
	// Any abort reverts the whole tx → atomic
	if !mIsZero(tu) {
		received := escrowIn(tt, proposer, tu)
		// fee using effective fee bps for offered NFT collection; nil,nil → no royalty on barter
		fee, _, acceptorPay := feeAndRoyaltyOf(received, getEffectiveFeeBps(on), nil, nil)
		if !mIsZero(fee) {
			tokenTransferBig(tt, getFeeRecipient(), fee)
		}
		if !mIsZero(acceptorPay) {
			tokenTransferBig(tt, acceptor, acceptorPay)
		}
	}

	// Both NFT legs — any abort reverts whole tx
	nftSafeTransferFrom(on, proposer, acceptor, oti, oa)
	nftSafeTransferFrom(wn, acceptor, proposer, wti, wa)

	setSwapField(p.SwapId, "act", "0")
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

	ppb := parseMoney(p.PricePerBlock)
	if mIsZero(ppb) {
		sdk.Abort("Price per block must be greater than zero")
	}

	if p.MinBlocks == 0 || p.MinBlocks > p.MaxBlocks {
		sdk.Abort("Invalid block range")
	}

	assertPaymentTokenAllowed(p.PaymentToken)
	assertCollectionAllowed(p.NftContract)

	if nftIsSoulbound(p.NftContract, p.TokenId) {
		sdk.Abort("Cannot list soulbound tokens")
	}

	contractAddr := getContractAddress()
	if !nftIsApprovedForAll(p.NftContract, caller, contractAddr) {
		sdk.Abort("Marketplace not approved as operator for this NFT collection")
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
	ti := getRentalField(p.RentalId, "ti")
	amt := getRentalUint64(p.RentalId, "amt")
	lockedFeeBps := getRentalUint64(p.RentalId, "fb")
	lockedRoyaltyBps := getRentalUint64(p.RentalId, "rb")
	royaltyRecipient := getRentalField(p.RentalId, "rr")

	cost := mMulU64(getRentalMoney(p.RentalId, "ppb"), p.Blocks)
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

	until := getCurrentBlockHeight() + p.Blocks
	setRentalField(p.RentalId, "rby", renter)
	setRentalUint64(p.RentalId, "until", until)
	setRentalField(p.RentalId, "rented", "1")

	// Index: ract|<nc>|<ti>|<renter> = until
	setUint64State("ract|"+nc+"|"+ti+"|"+renter, until)

	emitRented(p.RentalId, renter, until)
	return jsonResponse(&SuccessResponse{Success: true})
}

//go:wasmexport endRental
func EndRental(payload *string) *string {
	assertInit()
	// Works while paused — recovery path (mirrors CancelAuction).

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

	// Return escrowed NFT from market to owner.
	nftSafeTransferFrom(nc, contractAddr, owner, ti, amt)

	setRentalField(p.RentalId, "rented", "0")
	setRentalField(p.RentalId, "rby", "")

	// Delete active rental index.
	sdk.StateDeleteObject("ract|" + nc + "|" + ti + "|" + prevRenter)

	emitRentalEnded(p.RentalId, getCaller())
	return jsonResponse(&SuccessResponse{Success: true})
}

//go:wasmexport endRentalEarly
func EndRentalEarly(payload *string) *string {
	assertInit()

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

	// Return escrowed NFT from market to owner. No refund of unused term.
	nftSafeTransferFrom(nc, contractAddr, owner, ti, amt)

	setRentalField(p.RentalId, "rented", "0")
	setRentalField(p.RentalId, "rby", "")

	// Delete active rental index.
	sdk.StateDeleteObject("ract|" + nc + "|" + ti + "|" + caller)

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

