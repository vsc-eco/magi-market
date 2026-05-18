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

	// Check NFT is not soulbound
	if nftIsSoulbound(p.NftContract, p.TokenId) {
		sdk.Abort("Cannot auction soulbound tokens")
	}

	// Lock current fees and royalties
	currentFeeBps := getFeeBps()
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

		// Enforce minimum bid increment if configured
		minIncBps := getMinBidIncrementBps()
		if minIncBps > 0 && !mIsZero(currentHighBid) {
			minBid := mAdd(currentHighBid, mMulBpsDiv(currentHighBid, minIncBps))
			if mCmp(bid, minBid) < 0 {
				sdk.Abort("Bid must exceed current high bid by minimum increment")
			}
		}

		// Escrow new bid FIRST (before refunding previous bidder).
		// Use balance-delta so fee-on-transfer / utxo deduct_fee tokens
		// credit only what actually arrived.
		received := escrowIn(paymentToken, caller, bid)

		// Refund previous high bidder (after new bid is secured).
		// Read the PREVIOUS stored high bid before overwriting it.
		prevHighBidder := getAuctionField(p.AuctionId, "hb")
		prevHighBid := currentHighBid
		if prevHighBidder != "" && !mIsZero(prevHighBid) {
			tokenTransferBig(paymentToken, prevHighBidder, prevHighBid)
		}

		// Update auction state
		setAuctionField(p.AuctionId, "hb", caller)
		// Stored high bid is the ACTUAL escrowed amount (received), not the
		// nominal bid. Reserve/min-increment guards above intentionally compare
		// the nominal bid; storing post-fee `received` keeps auction economic
		// state equal to funds the contract truly holds. Do not "simplify" this
		// to store the nominal bid.
		setAuctionMoney(p.AuctionId, "ha", received)

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

		// The buyer is only ever charged totalPrice (the current Dutch price);
		// the declared bid is used solely as the pre-escrow lower-bound guard
		// above. Any excess the buyer declared is never pulled.
		// Escrow payment using balance-delta; require the credited amount
		// to cover the current total price.
		received := escrowIn(paymentToken, caller, totalPrice)
		if mCmp(received, totalPrice) < 0 {
			sdk.Abort("Bid must be at least the current total price")
		}

		// Transfer NFT to buyer first (before distributing payments)
		nftContract := getAuctionField(p.AuctionId, "nc")
		tokenId := getAuctionField(p.AuctionId, "ti")
		nftSafeTransferFrom(nftContract, contractAddr, caller, tokenId, amount)

		// Mark settled
		setAuctionField(p.AuctionId, "hb", caller)
		setAuctionMoney(p.AuctionId, "ha", received)
		setAuctionField(p.AuctionId, "stl", "1")
		setAuctionField(p.AuctionId, "act", "0")

		// Distribute payment
		lockedFeeBps := getAuctionUint64(p.AuctionId, "fb")
		lockedRoyaltyBps := getAuctionUint64(p.AuctionId, "rb")
		royaltyRecipient := getAuctionField(p.AuctionId, "rr")

		fee, royalty, sellerPayment := distributeFeesBig(received, lockedFeeBps, lockedRoyaltyBps)

		if !mIsZero(fee) {
			feeRecipient := getFeeRecipient()
			tokenTransferBig(paymentToken, feeRecipient, fee)
		}
		if !mIsZero(royalty) && royaltyRecipient != "" {
			tokenTransferBig(paymentToken, royaltyRecipient, royalty)
		}
		if !mIsZero(sellerPayment) {
			tokenTransferBig(paymentToken, seller, sellerPayment)
		}

		emitBidPlaced(p.AuctionId, caller, formatMoney(received))
		emitAuctionSettled(p.AuctionId, caller, formatMoney(received), formatMoney(fee), formatMoney(royalty))
	}

	return jsonResponse(&SuccessResponse{Success: true})
}

//go:wasmexport settleAuction
func SettleAuction(payload *string) *string {
	assertInit()

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
	highBidder := getAuctionField(p.AuctionId, "hb")
	highBid := getAuctionMoney(p.AuctionId, "ha")
	contractAddr := getContractAddress()

	if highBidder == "" || mIsZero(highBid) {
		// No bids - return NFT to seller
		nftSafeTransferFrom(nftContract, contractAddr, seller, tokenId, amount)

		// Mark settled after successful NFT transfer
		setAuctionField(p.AuctionId, "stl", "1")
		setAuctionField(p.AuctionId, "act", "0")

		emitAuctionSettled(p.AuctionId, "", "0", "0", "0")
	} else {
		// Transfer NFT to winner first (before distributing payments)
		nftSafeTransferFrom(nftContract, contractAddr, highBidder, tokenId, amount)

		// Mark settled after successful NFT transfer (prevents double-settlement)
		setAuctionField(p.AuctionId, "stl", "1")
		setAuctionField(p.AuctionId, "act", "0")

		// Distribute payment
		lockedFeeBps := getAuctionUint64(p.AuctionId, "fb")
		lockedRoyaltyBps := getAuctionUint64(p.AuctionId, "rb")
		royaltyRecipient := getAuctionField(p.AuctionId, "rr")

		fee, royalty, sellerPayment := distributeFeesBig(highBid, lockedFeeBps, lockedRoyaltyBps)

		if !mIsZero(fee) {
			feeRecipient := getFeeRecipient()
			tokenTransferBig(paymentToken, feeRecipient, fee)
		}
		if !mIsZero(royalty) && royaltyRecipient != "" {
			tokenTransferBig(paymentToken, royaltyRecipient, royalty)
		}
		if !mIsZero(sellerPayment) {
			tokenTransferBig(paymentToken, seller, sellerPayment)
		}

		emitAuctionSettled(p.AuctionId, highBidder, formatMoney(highBid), formatMoney(fee), formatMoney(royalty))
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

	// Return escrowed NFT to seller
	nftContract := getAuctionField(p.AuctionId, "nc")
	tokenId := getAuctionField(p.AuctionId, "ti")
	amount := getAuctionUint64(p.AuctionId, "a")
	contractAddr := getContractAddress()
	nftSafeTransferFrom(nftContract, contractAddr, seller, tokenId, amount)

	setAuctionField(p.AuctionId, "act", "0")
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
