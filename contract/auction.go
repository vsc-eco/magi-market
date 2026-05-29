package main

import (
	"math/big"

	"magi_market/sdk"

	"github.com/CosmWasm/tinyjson/jlexer"
)

// Dutch-auction current-price math is getDutchAuctionCurrentPriceBig in internal.go (big.Int).

// ===================================
// Auction Functions
// ===================================

//go:wasmexport createAuction
func CreateAuction(payload *string) *string {
	assertInit()
	assertNotPaused()

	caller := getCaller()

	if payload == nil || *payload == "" {
		sdk.Abort("Payload required")
	}

	var p CreateAuctionPayload
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
	if p.AuctionType != "english" && p.AuctionType != "dutch" {
		sdk.Abort("Auction type must be 'english' or 'dutch'")
	}
	startP := parseMoney(p.StartPrice)
	if mIsZero(startP) {
		sdk.Abort("Start price must be greater than zero")
	}
	if p.EndBlock == 0 {
		sdk.Abort("End block required")
	}

	currentBlock := getCurrentBlockHeight()
	if p.EndBlock <= currentBlock {
		sdk.Abort("End block must be in the future")
	}

	assertPaymentTokenAllowed(p.PaymentToken)

	var endP *big.Int
	if p.AuctionType == "dutch" {
		endP = parseMoney(p.EndPrice)
		if mCmp(endP, startP) >= 0 {
			sdk.Abort("Dutch auction end price must be less than start price")
		}
		if p.StartBlock == 0 {
			p.StartBlock = currentBlock
		}
		if p.StartBlock >= p.EndBlock {
			sdk.Abort("Start block must be before end block")
		}
	} else {
		// English auction: startPrice is the reserve price
		endP = mZero()
		if p.StartBlock == 0 {
			p.StartBlock = currentBlock
		}
	}

	// Auctions escrow the NFT into the market, which is NOT the collection
	// owner — so the market could never transfer a soulbound NFT back out
	// (to the winner, or back to the seller on no-sale/cancel), permanently
	// stranding it. Block soulbound unconditionally, even for the owner.
	if nftIsSoulbound(p.NftContract, p.TokenId) {
		sdk.Abort("Cannot auction soulbound tokens (auctions escrow to the market, which can't transfer them back out)")
	}
	assertCollectionAllowed(p.NftContract)

	// Lock current fees and royalties
	currentFeeBps := getEffectiveFeeBps(p.NftContract)
	currentRoyaltyBps := getRoyaltyBps(p.NftContract)
	if currentFeeBps+currentRoyaltyBps > 10000 {
		sdk.Abort("Combined fee and royalty exceed 100%")
	}

	// Escrow NFT
	contractAddr := getContractAddress()
	nftSafeTransferFrom(p.NftContract, caller, contractAddr, p.TokenId, p.Amount)

	// Create auction
	id := getNextAuctionId()
	setAuctionField(id, "s", caller)
	setAuctionField(id, "nc", p.NftContract)
	setAuctionField(id, "ti", p.TokenId)
	setAuctionUint64(id, "a", p.Amount)
	setAuctionField(id, "pt", p.PaymentToken)
	setAuctionField(id, "at", p.AuctionType)
	setAuctionMoney(id, "sp", startP)
	setAuctionMoney(id, "ep", endP)
	setAuctionUint64(id, "sb", p.StartBlock)
	setAuctionUint64(id, "eb", p.EndBlock)
	setAuctionField(id, "act", "1")
	setAuctionField(id, "stl", "0")
	setAuctionUint64(id, "fb", currentFeeBps)
	setAuctionUint64(id, "rb", currentRoyaltyBps)
	setAuctionField(id, "rr", getRoyaltyRecipient(p.NftContract))
	// Snapshot resolved royalty splits so in-flight auctions are unaffected by later split changes.
	aucSnapRecips, aucSnapBps := resolveRoyaltySplits(p.NftContract)
	snapshotRoyaltySplitsForAuction(id, aucSnapRecips, aucSnapBps)
	setNextAuctionId(id + 1)

	emitAuctionCreated(id, caller, p.NftContract, p.TokenId, p.Amount, p.AuctionType, formatMoney(startP), formatMoney(endP), p.StartBlock, p.EndBlock)
	return jsonResponse(&CreatedResponse{Success: true, Id: id})
}

//go:wasmexport placeBid
func PlaceBid(payload *string) *string {
	assertInit()
	assertNotPaused()

	caller := getCaller()

	if payload == nil || *payload == "" {
		sdk.Abort("Payload required")
	}

	var p PlaceBidPayload
	r := jlexer.Lexer{Data: []byte(*payload)}
	p.UnmarshalTinyJSON(&r)
	if r.Error() != nil {
		sdk.Abort("Invalid payload")
	}

	if !isAuctionActive(p.AuctionId) {
		sdk.Abort("Auction not active")
	}
	if isAuctionSettled(p.AuctionId) {
		sdk.Abort("Auction already settled")
	}

	currentBlock := getCurrentBlockHeight()
	auctionType := getAuctionField(p.AuctionId, "at")
	endBlock := getAuctionUint64(p.AuctionId, "eb")
	startBlock := getAuctionUint64(p.AuctionId, "sb")

	if currentBlock < startBlock {
		sdk.Abort("Auction has not started yet")
	}

	// Prevent seller from bidding on own auction
	seller := getAuctionField(p.AuctionId, "s")
	if caller == seller {
		sdk.Abort("Seller cannot bid on own auction")
	}

	paymentToken := getAuctionField(p.AuctionId, "pt")
	// Re-validate the auction's payment token at bid-time so a token
	// removed from the whitelist post-listing cannot mediate new bids.
	assertPaymentTokenAllowed(paymentToken)
	nftContract := getAuctionField(p.AuctionId, "nc")
	assertCollectionAllowed(nftContract)
	contractAddr := getContractAddress()

	if auctionType == "english" {
		if currentBlock > endBlock {
			sdk.Abort("Auction has ended")
		}

		amount := getAuctionUint64(p.AuctionId, "a")
		reserveTotal := mMulU64(getAuctionMoney(p.AuctionId, "sp"), amount)
		currentHighBid := getAuctionMoney(p.AuctionId, "ha")

		bid := parseMoney(p.BidAmount)
		if mIsZero(bid) {
			sdk.Abort("Bid amount must be greater than zero")
		}

		// Must exceed reserve price (startPrice is per-unit, bidAmount is total)
		if mCmp(bid, reserveTotal) < 0 {
			sdk.Abort("Bid must be at least the reserve price")
		}

		// Must exceed current high bid
		if !mIsZero(currentHighBid) && mCmp(bid, currentHighBid) <= 0 {
			sdk.Abort("Bid must exceed current high bid")
		}

		// Enforce minimum bid increment. effectiveMinBidIncrementBps floors at
		// 1% when unset, so this always applies once there's a high bid —
		// closing the anti-snipe indefinite-extension grief that a 0 increment
		// (the un-set default) would otherwise allow.
		minIncBps := effectiveMinBidIncrementBps()
		if !mIsZero(currentHighBid) {
			minBid := mAdd(currentHighBid, mMulBpsDiv(currentHighBid, minIncBps))
			if mCmp(bid, minBid) < 0 {
				sdk.Abort("Bid must exceed current high bid by minimum increment")
			}
		}

		// Escrow new bid FIRST (before refunding previous bidder).
		// Use balance-delta so fee-on-transfer / utxo deduct_fee tokens
		// credit only what actually arrived.
		received := escrowIn(paymentToken, caller, bid)

		// Read prev high bidder/bid from STATE *after* escrowIn — if the
		// paymentToken's transferFrom callback re-entered placeBid, the
		// state now reflects the inner-call's bidder (whose deposit our
		// outer-call's higher bid is supplanting); refunding from that
		// state is the only correct path. Reading a local snapshot from
		// before escrowIn would either double-refund the original prev
		// bidder (if the local was captured pre-escrowIn) or refund the
		// wrong amount (if the local was captured post-escrowIn).
		prevHighBidder := getAuctionField(p.AuctionId, "hb")
		prevHighBid := getAuctionMoney(p.AuctionId, "ha")

		// Sanity: the new credit must strictly exceed the prev high bid
		// we are about to refund. Without this, a paymentToken whose
		// `transferFrom` under-delivers (mislabeled-magi_token decoder,
		// fee-on-transfer token, utxo deduct_fee crossing the existing
		// high water-mark, etc.) lets `bid > prevHighBid+minInc` pass
		// the comparison guards above while `received <= prevHighBid` —
		// the refund below would then pay out `prevHighBid` from a
		// smaller credit, draining the pool by `prevHighBid - received`
		// on every supplanting bid. When there is no prior high bid,
		// any non-zero `received` is fine.
		if !mIsZero(prevHighBid) && mCmp(received, prevHighBid) <= 0 {
			sdk.Abort("Escrowed bid does not cover the refund of the previous high bid")
		}

		// Checks-Effects-Interactions: commit the new hb/ha BEFORE the
		// refund external call so a re-entry through the paymentToken's
		// `transfer` hook can't see stale state.
		setAuctionField(p.AuctionId, "hb", caller)
		// Stored high bid is the ACTUAL escrowed amount (received), not the
		// nominal bid. Reserve/min-increment guards above intentionally compare
		// the nominal bid; storing post-fee `received` keeps auction economic
		// state equal to funds the contract truly holds. Do not "simplify" this
		// to store the nominal bid.
		setAuctionMoney(p.AuctionId, "ha", received)

		// Refund whoever's deposit was supplanted (read from state above).
		if prevHighBidder != "" && !mIsZero(prevHighBid) {
			tokenTransferBig(paymentToken, prevHighBidder, prevHighBid)
		}

		// Anti-snipe: extend endBlock if bid is placed near the end
		antiSnipeBlocks := getAntiSnipeBlocks()
		if antiSnipeBlocks > 0 {
			remaining := endBlock - currentBlock
			if remaining < antiSnipeBlocks {
				newEndBlock := currentBlock + antiSnipeBlocks
				setAuctionUint64(p.AuctionId, "eb", newEndBlock)
			}
		}

		emitBidPlaced(p.AuctionId, caller, formatMoney(received))

	} else {
		// Dutch auction: bid acts as immediate buy at current price
		if currentBlock > endBlock {
			sdk.Abort("Auction has ended")
		}

		currentPrice := getDutchAuctionCurrentPriceBig(
			getAuctionMoney(p.AuctionId, "sp"),
			getAuctionMoney(p.AuctionId, "ep"),
			startBlock, endBlock, currentBlock)

		amount := getAuctionUint64(p.AuctionId, "a")
		totalPrice := mMulU64(currentPrice, amount)

		// Preserve the original buyer-declared-bid guard (bid < totalPrice)
		// in its original position, before any escrow.
		bid := parseMoney(p.BidAmount)
		if mCmp(bid, totalPrice) < 0 {
			sdk.Abort("Bid must be at least the current total price")
		}

		// CEI: flip stl=1 / act=0 BEFORE any external call so a re-entry
		// through the paymentToken's `transferFrom` callback hits the
		// `isAuctionSettled` gate at the top of placeBid and aborts.
		// Without this, a malicious whitelisted paymentToken can re-enter
		// during `escrowIn` (state still active), let the inner call also
		// transfer this auction's NFT (and, if the market holds the same
		// (nc,ti) for another auction's escrow, drain that one too), and
		// settle twice. `hb` is set here to the caller; `ha` is rewritten
		// post-escrow to the actual `received` for accurate accounting.
		setAuctionField(p.AuctionId, "hb", caller)
		setAuctionMoney(p.AuctionId, "ha", totalPrice)
		setAuctionField(p.AuctionId, "stl", "1")
		setAuctionField(p.AuctionId, "act", "0")

		// The buyer is only ever charged totalPrice (the current Dutch price);
		// the declared bid is used solely as the pre-escrow lower-bound guard
		// above. Any excess the buyer declared is never pulled.
		// Escrow payment using balance-delta; require the credited amount
		// to cover the current total price.
		received := escrowIn(paymentToken, caller, totalPrice)
		if mCmp(received, totalPrice) < 0 {
			sdk.Abort("Bid must be at least the current total price")
		}
		// Overwrite ha with the actual escrowed amount (mirrors the
		// English branch's "store received, not nominal" rule).
		setAuctionMoney(p.AuctionId, "ha", received)

		// Transfer NFT to buyer (state is already settled; re-entry is blocked).
		tokenId := getAuctionField(p.AuctionId, "ti")
		nftSafeTransferFrom(nftContract, contractAddr, caller, tokenId, amount)

		// Distribute payment
		lockedFeeBps := getAuctionUint64(p.AuctionId, "fb")
		lockedRoyaltyBps := getAuctionUint64(p.AuctionId, "rb")
		royaltyRecipient := getAuctionField(p.AuctionId, "rr")

		// Load royalty split snapshot; fall back to legacy single-entry for pre-B2 in-flight entries.
		dutchSnapRecips, dutchSnapBps := loadAuctionRoyaltySplitSnapshot(p.AuctionId, royaltyRecipient, lockedRoyaltyBps)
		fee, royTot, sellerPayment := feeAndRoyaltyOf(received, lockedFeeBps, dutchSnapRecips, dutchSnapBps)

		if !mIsZero(fee) {
			feeRecipient := getFeeRecipient()
			tokenTransferBig(paymentToken, feeRecipient, fee)
		}
		distributeRoyaltySplitsResolved(paymentToken, received, dutchSnapRecips, dutchSnapBps)
		if !mIsZero(sellerPayment) {
			tokenTransferBig(paymentToken, seller, sellerPayment)
		}

		emitBidPlaced(p.AuctionId, caller, formatMoney(received))
		emitAuctionSettled(p.AuctionId, caller, formatMoney(received), formatMoney(fee), formatMoney(royTot))
	}

	return jsonResponse(&SuccessResponse{Success: true})
}

//go:wasmexport settleAuction
func SettleAuction(payload *string) *string {
	assertInit()
	assertNotPaused()

	if payload == nil || *payload == "" {
		sdk.Abort("Payload required")
	}

	var p SettleAuctionPayload
	r := jlexer.Lexer{Data: []byte(*payload)}
	p.UnmarshalTinyJSON(&r)
	if r.Error() != nil {
		sdk.Abort("Invalid payload")
	}

	if !isAuctionActive(p.AuctionId) {
		sdk.Abort("Auction not active")
	}
	if isAuctionSettled(p.AuctionId) {
		sdk.Abort("Auction already settled")
	}

	auctionType := getAuctionField(p.AuctionId, "at")
	if auctionType != "english" {
		sdk.Abort("Only English auctions need settlement")
	}

	currentBlock := getCurrentBlockHeight()
	endBlock := getAuctionUint64(p.AuctionId, "eb")
	if currentBlock <= endBlock {
		sdk.Abort("Auction has not ended yet")
	}

	seller := getAuctionField(p.AuctionId, "s")
	nftContract := getAuctionField(p.AuctionId, "nc")
	tokenId := getAuctionField(p.AuctionId, "ti")
	amount := getAuctionUint64(p.AuctionId, "a")
	paymentToken := getAuctionField(p.AuctionId, "pt")
	// Re-validate at settle-time: a payment token removed from the
	// whitelist post-bid should not continue to mediate the payout.
	assertPaymentTokenAllowed(paymentToken)
	highBidder := getAuctionField(p.AuctionId, "hb")
	highBid := getAuctionMoney(p.AuctionId, "ha")
	contractAddr := getContractAddress()

	// CEI: flip stl=1 and act=0 BEFORE any external call. A malicious
	// nftContract (seller-controlled at create time) would otherwise
	// re-enter settleAuction via its safeTransferFrom callback; the inner
	// re-entry sees act=1,stl=0, runs the full payout flow, and the outer
	// continuation then pays AGAIN — draining the pool by one highBid.
	// All three branches below must commit the flip first.
	if isCollectionDenied(nftContract) {
		// Collection denied mid-auction: treat as no-sale. Return the
		// escrowed NFT to the seller and refund the high bidder (if any).
		setAuctionField(p.AuctionId, "stl", "1")
		setAuctionField(p.AuctionId, "act", "0")
		nftSafeTransferFrom(nftContract, contractAddr, seller, tokenId, amount)
		if highBidder != "" && !mIsZero(highBid) {
			tokenTransferBig(paymentToken, highBidder, highBid)
		}
		emitAuctionSettled(p.AuctionId, "", "0", "0", "0")
		return jsonResponse(&SuccessResponse{Success: true})
	}

	if highBidder == "" || mIsZero(highBid) {
		// No bids - return NFT to seller
		setAuctionField(p.AuctionId, "stl", "1")
		setAuctionField(p.AuctionId, "act", "0")
		nftSafeTransferFrom(nftContract, contractAddr, seller, tokenId, amount)

		emitAuctionSettled(p.AuctionId, "", "0", "0", "0")
	} else {
		// Mark settled BEFORE the NFT transfer + payouts so a re-entry
		// through the seller-controlled nftContract sees stl=1 and aborts.
		setAuctionField(p.AuctionId, "stl", "1")
		setAuctionField(p.AuctionId, "act", "0")

		// Transfer NFT to winner first (before distributing payments)
		nftSafeTransferFrom(nftContract, contractAddr, highBidder, tokenId, amount)

		// Distribute payment
		lockedFeeBps := getAuctionUint64(p.AuctionId, "fb")
		lockedRoyaltyBps := getAuctionUint64(p.AuctionId, "rb")
		royaltyRecipient := getAuctionField(p.AuctionId, "rr")

		// Load royalty split snapshot; fall back to legacy single-entry for pre-B2 in-flight entries.
		settleSnapRecips, settleSnapBps := loadAuctionRoyaltySplitSnapshot(p.AuctionId, royaltyRecipient, lockedRoyaltyBps)
		fee, royTot, sellerPayment := feeAndRoyaltyOf(highBid, lockedFeeBps, settleSnapRecips, settleSnapBps)

		if !mIsZero(fee) {
			feeRecipient := getFeeRecipient()
			tokenTransferBig(paymentToken, feeRecipient, fee)
		}
		distributeRoyaltySplitsResolved(paymentToken, highBid, settleSnapRecips, settleSnapBps)
		if !mIsZero(sellerPayment) {
			tokenTransferBig(paymentToken, seller, sellerPayment)
		}

		emitAuctionSettled(p.AuctionId, highBidder, formatMoney(highBid), formatMoney(fee), formatMoney(royTot))
	}

	return jsonResponse(&SuccessResponse{Success: true})
}

//go:wasmexport cancelAuction
func CancelAuction(payload *string) *string {
	assertInit()
	// Note: cancelAuction works even when paused so sellers can recover escrowed NFTs

	caller := getCaller()

	if payload == nil || *payload == "" {
		sdk.Abort("Payload required")
	}

	var p CancelAuctionPayload
	r := jlexer.Lexer{Data: []byte(*payload)}
	p.UnmarshalTinyJSON(&r)
	if r.Error() != nil {
		sdk.Abort("Invalid payload")
	}

	if !isAuctionActive(p.AuctionId) {
		sdk.Abort("Auction not active")
	}
	if isAuctionSettled(p.AuctionId) {
		sdk.Abort("Auction already settled")
	}

	seller := getAuctionField(p.AuctionId, "s")
	if caller != seller {
		sdk.Abort("Only seller can cancel auction")
	}

	// Can only cancel if no bids placed (English) or not yet started
	auctionType := getAuctionField(p.AuctionId, "at")
	if auctionType == "english" {
		highBidder := getAuctionField(p.AuctionId, "hb")
		if highBidder != "" {
			sdk.Abort("Cannot cancel auction with active bids")
		}
	}

	// CEI: flip act=0 BEFORE the NFT return. The caller-equality check
	// above already blocks inner re-entry from a malicious nftContract
	// (inner caller != seller), but committing state first keeps the
	// invariant uniform with the other entrypoints and rules out any
	// future bypass.
	nftContract := getAuctionField(p.AuctionId, "nc")
	tokenId := getAuctionField(p.AuctionId, "ti")
	amount := getAuctionUint64(p.AuctionId, "a")
	contractAddr := getContractAddress()
	setAuctionField(p.AuctionId, "act", "0")
	nftSafeTransferFrom(nftContract, contractAddr, seller, tokenId, amount)

	emitAuctionCancelled(p.AuctionId, seller)
	return jsonResponse(&SuccessResponse{Success: true})
}

//go:wasmexport getAuction
func GetAuction(payload *string) *string {
	assertInit()

	if payload == nil || *payload == "" {
		sdk.Abort("Payload required")
	}

	var p AuctionIdPayload
	r := jlexer.Lexer{Data: []byte(*payload)}
	p.UnmarshalTinyJSON(&r)
	if r.Error() != nil {
		sdk.Abort("Invalid payload")
	}

	seller := getAuctionField(p.AuctionId, "s")
	if seller == "" {
		sdk.Abort("Auction not found")
	}

	return jsonResponse(&AuctionResponse{
		AuctionId:    p.AuctionId,
		Seller:       seller,
		NftContract:  getAuctionField(p.AuctionId, "nc"),
		TokenId:      getAuctionField(p.AuctionId, "ti"),
		Amount:       getAuctionUint64(p.AuctionId, "a"),
		PaymentToken: getAuctionField(p.AuctionId, "pt"),
		AuctionType:  getAuctionField(p.AuctionId, "at"),
		StartPrice:   formatMoney(getAuctionMoney(p.AuctionId, "sp")),
		EndPrice:     formatMoney(getAuctionMoney(p.AuctionId, "ep")),
		StartBlock:   getAuctionUint64(p.AuctionId, "sb"),
		EndBlock:     getAuctionUint64(p.AuctionId, "eb"),
		HighBidder:   getAuctionField(p.AuctionId, "hb"),
		HighBid:      formatMoney(getAuctionMoney(p.AuctionId, "ha")),
		Active:       isAuctionActive(p.AuctionId),
		Settled:      isAuctionSettled(p.AuctionId),
		FeeBps:       getAuctionUint64(p.AuctionId, "fb"),
		RoyaltyBps:   getAuctionUint64(p.AuctionId, "rb"),
	})
}
