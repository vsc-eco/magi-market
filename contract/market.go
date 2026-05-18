package main

import (
	"magi_market/sdk"

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

	currentFeeBps := getFeeBps()
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
	if !mIsZero(sellerPayment) {
		tokenTransferBig(paymentToken, seller, sellerPayment)
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

	currentFeeBps := getFeeBps()
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

