package main

import (
	"magi_market/sdk"

	"github.com/CosmWasm/tinyjson/jlexer"
)

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
	if p.StartPrice == 0 {
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

	if p.AuctionType == "dutch" {
		if p.EndPrice >= p.StartPrice {
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
		p.EndPrice = 0
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
	setAuctionUint64(id, "sp", p.StartPrice)
	setAuctionUint64(id, "ep", p.EndPrice)
	setAuctionUint64(id, "sb", p.StartBlock)
	setAuctionUint64(id, "eb", p.EndBlock)
	setAuctionField(id, "act", "1")
	setAuctionField(id, "stl", "0")
	setAuctionUint64(id, "fb", currentFeeBps)
	setAuctionUint64(id, "rb", currentRoyaltyBps)
	setAuctionField(id, "rr", getRoyaltyRecipient(p.NftContract))
	setNextAuctionId(id + 1)

	emitAuctionCreated(id, caller, p.NftContract, p.TokenId, p.Amount, p.AuctionType, p.StartPrice, p.EndPrice, p.StartBlock, p.EndBlock)
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

		startPrice := getAuctionUint64(p.AuctionId, "sp")
		amount := getAuctionUint64(p.AuctionId, "a")
		reserveTotal := safeMul(amount, startPrice)
		currentHighBid := getAuctionUint64(p.AuctionId, "ha")

		if p.BidAmount == 0 {
			sdk.Abort("Bid amount must be greater than zero")
		}

		// Must exceed reserve price (startPrice is per-unit, bidAmount is total)
		if p.BidAmount < reserveTotal {
			sdk.Abort("Bid must be at least the reserve price")
		}

		// Must exceed current high bid
		if currentHighBid > 0 && p.BidAmount <= currentHighBid {
			sdk.Abort("Bid must exceed current high bid")
		}

		// Enforce minimum bid increment if configured
		minIncBps := getMinBidIncrementBps()
		if minIncBps > 0 && currentHighBid > 0 {
			minBid := safeAdd(currentHighBid, safeMul(currentHighBid, minIncBps)/10000)
			if p.BidAmount < minBid {
				sdk.Abort("Bid must exceed current high bid by minimum increment")
			}
		}

		// Escrow new bid FIRST (before refunding previous bidder)
		tokenTransferFrom(paymentToken, caller, contractAddr, p.BidAmount)

		// Refund previous high bidder (after new bid is secured)
		currentHighBidder := getAuctionField(p.AuctionId, "hb")
		if currentHighBidder != "" && currentHighBid > 0 {
			tokenTransfer(paymentToken, currentHighBidder, currentHighBid)
		}

		// Update auction state
		setAuctionField(p.AuctionId, "hb", caller)
		setAuctionUint64(p.AuctionId, "ha", p.BidAmount)

		// Anti-snipe: extend endBlock if bid is placed near the end
		antiSnipeBlocks := getAntiSnipeBlocks()
		if antiSnipeBlocks > 0 {
			remaining := endBlock - currentBlock
			if remaining < antiSnipeBlocks {
				newEndBlock := currentBlock + antiSnipeBlocks
				setAuctionUint64(p.AuctionId, "eb", newEndBlock)
			}
		}

		emitBidPlaced(p.AuctionId, caller, p.BidAmount)

	} else {
		// Dutch auction: bid acts as immediate buy at current price
		if currentBlock > endBlock {
			sdk.Abort("Auction has ended")
		}

		startPrice := getAuctionUint64(p.AuctionId, "sp")
		endPrice := getAuctionUint64(p.AuctionId, "ep")
		currentPrice := getDutchAuctionCurrentPrice(startPrice, endPrice, startBlock, endBlock, currentBlock)

		amount := getAuctionUint64(p.AuctionId, "a")
		totalPrice := safeMul(amount, currentPrice)

		if p.BidAmount < totalPrice {
			sdk.Abort("Bid must be at least the current total price")
		}

		// Escrow payment
		tokenTransferFrom(paymentToken, caller, contractAddr, totalPrice)

		// Transfer NFT to buyer first (before distributing payments)
		nftContract := getAuctionField(p.AuctionId, "nc")
		tokenId := getAuctionField(p.AuctionId, "ti")
		nftSafeTransferFrom(nftContract, contractAddr, caller, tokenId, amount)

		// Mark settled
		setAuctionField(p.AuctionId, "hb", caller)
		setAuctionUint64(p.AuctionId, "ha", totalPrice)
		setAuctionField(p.AuctionId, "stl", "1")
		setAuctionField(p.AuctionId, "act", "0")

		// Distribute payment
		lockedFeeBps := getAuctionUint64(p.AuctionId, "fb")
		lockedRoyaltyBps := getAuctionUint64(p.AuctionId, "rb")
		royaltyRecipient := getAuctionField(p.AuctionId, "rr")

		fee, royalty, sellerPayment := distributeFees(totalPrice, lockedFeeBps, lockedRoyaltyBps)

		if fee > 0 {
			feeRecipient := getFeeRecipient()
			tokenTransfer(paymentToken, feeRecipient, fee)
		}
		if royalty > 0 && royaltyRecipient != "" {
			tokenTransfer(paymentToken, royaltyRecipient, royalty)
		}
		if sellerPayment > 0 {
			tokenTransfer(paymentToken, seller, sellerPayment)
		}

		emitBidPlaced(p.AuctionId, caller, totalPrice)
		emitAuctionSettled(p.AuctionId, caller, totalPrice, fee, royalty)
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
	highBid := getAuctionUint64(p.AuctionId, "ha")
	contractAddr := getContractAddress()

	if highBidder == "" || highBid == 0 {
		// No bids - return NFT to seller
		nftSafeTransferFrom(nftContract, contractAddr, seller, tokenId, amount)

		// Mark settled after successful NFT transfer
		setAuctionField(p.AuctionId, "stl", "1")
		setAuctionField(p.AuctionId, "act", "0")

		emitAuctionSettled(p.AuctionId, "", 0, 0, 0)
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

		fee, royalty, sellerPayment := distributeFees(highBid, lockedFeeBps, lockedRoyaltyBps)

		if fee > 0 {
			feeRecipient := getFeeRecipient()
			tokenTransfer(paymentToken, feeRecipient, fee)
		}
		if royalty > 0 && royaltyRecipient != "" {
			tokenTransfer(paymentToken, royaltyRecipient, royalty)
		}
		if sellerPayment > 0 {
			tokenTransfer(paymentToken, seller, sellerPayment)
		}

		emitAuctionSettled(p.AuctionId, highBidder, highBid, fee, royalty)
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
		StartPrice:   getAuctionUint64(p.AuctionId, "sp"),
		EndPrice:     getAuctionUint64(p.AuctionId, "ep"),
		StartBlock:   getAuctionUint64(p.AuctionId, "sb"),
		EndBlock:     getAuctionUint64(p.AuctionId, "eb"),
		HighBidder:   getAuctionField(p.AuctionId, "hb"),
		HighBid:      getAuctionUint64(p.AuctionId, "ha"),
		Active:       isAuctionActive(p.AuctionId),
		Settled:      isAuctionSettled(p.AuctionId),
		FeeBps:       getAuctionUint64(p.AuctionId, "fb"),
		RoyaltyBps:   getAuctionUint64(p.AuctionId, "rb"),
	})
}
