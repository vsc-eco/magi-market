package main

import (
	"math/big"
	"magi_market/sdk"
	"strconv"

	"github.com/CosmWasm/tinyjson/jwriter"
)

// ===================================
// Safe Math Utilities
// ===================================

func safeAdd(a, b uint64) uint64 {
	sum := a + b
	if sum < a {
		sdk.Abort("safeAdd overflow")
	}
	return sum
}

func safeSub(a, b uint64) uint64 {
	if b > a {
		sdk.Abort("safeSub underflow")
	}
	return a - b
}

func safeMul(a, b uint64) uint64 {
	if a == 0 || b == 0 {
		return 0
	}
	result := a * b
	if result/a != b {
		sdk.Abort("safeMul overflow")
	}
	return result
}

// ===================================
// Environment Helpers
// ===================================

func getCaller() string {
	caller := sdk.GetEnvKey("msg.caller")
	if caller == nil {
		sdk.Abort("Caller required")
	}
	return *caller
}

func getContractId() string {
	id := sdk.GetEnvKey("contract.id")
	if id == nil {
		sdk.Abort("contract.id not available")
	}
	return *id
}

func getContractAddress() string {
	return "contract:" + getContractId()
}

func getCurrentBlockHeight() uint64 {
	h := sdk.GetEnvKey("block.height")
	if h == nil {
		return 0
	}
	val, _ := strconv.ParseUint(*h, 10, 64)
	return val
}

// ===================================
// Expiration Helper
// ===================================

func isExpired(expirationBlock uint64) bool {
	if expirationBlock == 0 {
		return false
	}
	return getCurrentBlockHeight() > expirationBlock
}

// ===================================
// State Helpers
// ===================================

func getUint64State(key string) uint64 {
	v := sdk.StateGetObject(key)
	if v == nil || *v == "" {
		return 0
	}
	val, _ := strconv.ParseUint(*v, 10, 64)
	return val
}

func setUint64State(key string, val uint64) {
	sdk.StateSetObject(key, strconv.FormatUint(val, 10))
}

func getStringState(key string) string {
	v := sdk.StateGetObject(key)
	if v == nil {
		return ""
	}
	return *v
}

func setStringState(key string, val string) {
	sdk.StateSetObject(key, val)
}

// ===================================
// Listing State Helpers
// ===================================

func listingKey(id uint64, field string) string {
	return "ls|" + strconv.FormatUint(id, 10) + "|" + field
}

func setListingField(id uint64, field, value string) {
	sdk.StateSetObject(listingKey(id, field), value)
}

func getListingField(id uint64, field string) string {
	return getStringState(listingKey(id, field))
}

func getListingUint64(id uint64, field string) uint64 {
	return getUint64State(listingKey(id, field))
}

func setListingUint64(id uint64, field string, val uint64) {
	setUint64State(listingKey(id, field), val)
}

func isListingActive(id uint64) bool {
	return getListingField(id, "act") == "1"
}

func getNextListingId() uint64 {
	return getUint64State("nxt_lst")
}

func setNextListingId(id uint64) {
	setUint64State("nxt_lst", id)
}

// ===================================
// Offer State Helpers
// ===================================

func offerKey(id uint64, field string) string {
	return "of|" + strconv.FormatUint(id, 10) + "|" + field
}

func setOfferField(id uint64, field, value string) {
	sdk.StateSetObject(offerKey(id, field), value)
}

func getOfferField(id uint64, field string) string {
	return getStringState(offerKey(id, field))
}

func getOfferUint64(id uint64, field string) uint64 {
	return getUint64State(offerKey(id, field))
}

func setOfferUint64(id uint64, field string, val uint64) {
	setUint64State(offerKey(id, field), val)
}

func isOfferActive(id uint64) bool {
	return getOfferField(id, "act") == "1"
}

func getNextOfferId() uint64 {
	return getUint64State("nxt_ofr")
}

func setNextOfferId(id uint64) {
	setUint64State("nxt_ofr", id)
}

func isCollectionOffer(id uint64) bool {
	return getOfferField(id, "col") == "1"
}

// ===================================
// Auction State Helpers
// ===================================

func auctionKey(id uint64, field string) string {
	return "au|" + strconv.FormatUint(id, 10) + "|" + field
}

func setAuctionField(id uint64, field, value string) {
	sdk.StateSetObject(auctionKey(id, field), value)
}

func getAuctionField(id uint64, field string) string {
	return getStringState(auctionKey(id, field))
}

func getAuctionUint64(id uint64, field string) uint64 {
	return getUint64State(auctionKey(id, field))
}

func setAuctionUint64(id uint64, field string, val uint64) {
	setUint64State(auctionKey(id, field), val)
}

func isAuctionActive(id uint64) bool {
	return getAuctionField(id, "act") == "1"
}

func isAuctionSettled(id uint64) bool {
	return getAuctionField(id, "stl") == "1"
}

func getNextAuctionId() uint64 {
	return getUint64State("nxt_auc")
}

func setNextAuctionId(id uint64) {
	setUint64State("nxt_auc", id)
}

// ===================================
// Fee Helpers
// ===================================

func getFeeBps() uint64 {
	return getUint64State("fee_bps")
}

func getFeeRecipient() string {
	return getStringState("fee_rcpt")
}

// calculateFeeWithBps computes fee and seller payment using a specific fee bps value.
func calculateFeeWithBps(total, feeBps uint64) (uint64, uint64) {
	if feeBps == 0 {
		return 0, total
	}
	fee := safeMul(total, feeBps) / 10000
	return fee, safeSub(total, fee)
}

// calculateFee computes fee and seller payment using the current global fee bps.
func calculateFee(total uint64) (uint64, uint64) {
	return calculateFeeWithBps(total, getFeeBps())
}

// ===================================
// Royalty Helpers
// ===================================

func royaltyKey(nftContract, field string) string {
	return "ry|" + nftContract + "|" + field
}

func getRoyaltyBps(nftContract string) uint64 {
	return getUint64State(royaltyKey(nftContract, "bps"))
}

func getRoyaltyRecipient(nftContract string) string {
	return getStringState(royaltyKey(nftContract, "rcpt"))
}

func setRoyaltyBps(nftContract string, bps uint64) {
	setUint64State(royaltyKey(nftContract, "bps"), bps)
}

func setRoyaltyRecipientState(nftContract, recipient string) {
	setStringState(royaltyKey(nftContract, "rcpt"), recipient)
}

// ===================================
// Min Offer Helpers
// ===================================

func getMinOffer() uint64 {
	return getUint64State("min_ofr")
}

func setMinOfferState(v uint64) {
	setUint64State("min_ofr", v)
}

// ===================================
// Min Bid Increment Helpers
// ===================================

func getMinBidIncrementBps() uint64 {
	return getUint64State("min_bid_inc")
}

func setMinBidIncrementBps(v uint64) {
	setUint64State("min_bid_inc", v)
}

// ===================================
// Anti-Snipe Helpers
// ===================================

func getAntiSnipeBlocks() uint64 {
	return getUint64State("snipe_ext")
}

func setAntiSnipeBlocksState(v uint64) {
	setUint64State("snipe_ext", v)
}

// ===================================
// Payment Token Whitelist Helpers
// ===================================

func paymentTokenKey(token string) string {
	return "ptw|" + token
}

func isWhitelistEnabled() bool {
	return getStringState("ptw_on") == "1"
}

func isPaymentTokenAllowedCheck(token string) bool {
	if !isWhitelistEnabled() {
		return true
	}
	return getStringState(paymentTokenKey(token)) == "1"
}

func setPaymentTokenAllowed(token string, allowed bool) {
	if allowed {
		setStringState(paymentTokenKey(token), "1")
		setStringState("ptw_on", "1")
	} else {
		setStringState(paymentTokenKey(token), "0")
	}
}

func assertPaymentTokenAllowed(token string) {
	if !isPaymentTokenAllowedCheck(token) {
		sdk.Abort("Payment token not allowed")
	}
}

// ===================================
// Cross-Contract Token Helpers (ERC-20)
// ===================================

func tokenTransferFrom(tokenContract, from, to string, amount uint64) {
	payload := `{"from":"` + from + `","to":"` + to + `","amount":"` + strconv.FormatUint(amount, 10) + `"}`
	result := sdk.ContractCallSimple(tokenContract, "transferFrom", payload)
	if result == nil {
		sdk.Abort("transferFrom call failed")
	}
}

func tokenTransfer(tokenContract, to string, amount uint64) {
	payload := `{"to":"` + to + `","amount":"` + strconv.FormatUint(amount, 10) + `"}`
	result := sdk.ContractCallSimple(tokenContract, "transfer", payload)
	if result == nil {
		sdk.Abort("transfer call failed")
	}
}

// ===================================
// Cross-Contract NFT Helpers (ERC-1155)
// ===================================

func nftSafeTransferFrom(nftContract, from, to, tokenId string, amount uint64) {
	payload := `{"from":"` + from + `","to":"` + to + `","id":"` + tokenId + `","amount":` + strconv.FormatUint(amount, 10) + `,"data":""}`
	result := sdk.ContractCallSimple(nftContract, "safeTransferFrom", payload)
	if result == nil {
		sdk.Abort("safeTransferFrom call failed")
	}
}

func nftIsSoulbound(nftContract, tokenId string) bool {
	payload := `{"id":"` + tokenId + `"}`
	result := sdk.ContractCallSimple(nftContract, "isSoulbound", payload)
	if result == nil {
		return false
	}
	return jsonBoolField(*result, "soulbound")
}

func nftBalanceOf(nftContract, account, tokenId string) uint64 {
	payload := `{"account":"` + account + `","id":"` + tokenId + `"}`
	result := sdk.ContractCallSimple(nftContract, "balanceOf", payload)
	if result == nil {
		return 0
	}
	// Parse {"balance":N} or {"balance":"N"} or {"balance": N}
	s := *result
	key := `"balance":`
	idx := -1
	for i := 0; i <= len(s)-len(key); i++ {
		if s[i:i+len(key)] == key {
			idx = i + len(key)
			break
		}
	}
	if idx < 0 {
		return 0
	}
	// Skip whitespace
	for idx < len(s) && (s[idx] == ' ' || s[idx] == '\t' || s[idx] == '\n') {
		idx++
	}
	if idx >= len(s) {
		return 0
	}
	// Handle quoted value: "balance":"123"
	quoted := false
	if s[idx] == '"' {
		quoted = true
		idx++
	}
	// Read digits
	end := idx
	for end < len(s) && s[end] >= '0' && s[end] <= '9' {
		end++
	}
	if end == idx {
		return 0
	}
	// Verify closing quote if quoted
	if quoted && (end >= len(s) || s[end] != '"') {
		return 0
	}
	val, _ := strconv.ParseUint(s[idx:end], 10, 64)
	return val
}

func nftGetOwner(nftContract string) string {
	result := sdk.ContractCallSimple(nftContract, "getOwner", "{}")
	if result == nil {
		return ""
	}
	// Parse {"owner":"..."} - extract the owner value
	s := *result
	key := `"owner":"`
	idx := -1
	for i := 0; i <= len(s)-len(key); i++ {
		if s[i:i+len(key)] == key {
			idx = i + len(key)
			break
		}
	}
	if idx < 0 {
		return ""
	}
	end := idx
	for end < len(s) && s[end] != '"' {
		end++
	}
	return s[idx:end]
}

// ===================================
// Dutch Auction Price Calculation
// ===================================

func getDutchAuctionCurrentPrice(startPrice, endPrice, startBlock, endBlock, currentBlock uint64) uint64 {
	if currentBlock <= startBlock {
		return startPrice
	}
	if currentBlock >= endBlock {
		return endPrice
	}
	elapsed := currentBlock - startBlock
	duration := endBlock - startBlock
	priceDrop := safeMul(startPrice-endPrice, elapsed) / duration
	return startPrice - priceDrop
}

func getDutchAuctionCurrentPriceBig(startPrice, endPrice *big.Int, startBlock, endBlock, currentBlock uint64) *big.Int {
	if currentBlock <= startBlock {
		return new(big.Int).Set(startPrice)
	}
	if currentBlock >= endBlock {
		return new(big.Int).Set(endPrice)
	}
	elapsed := new(big.Int).SetUint64(currentBlock - startBlock)
	duration := new(big.Int).SetUint64(endBlock - startBlock)
	drop := new(big.Int).Mul(mSub(startPrice, endPrice), elapsed)
	drop.Quo(drop, duration)
	return mSub(startPrice, drop)
}

// ===================================
// Fee Distribution Helper
// ===================================

// distributeFees calculates marketplace fee, royalty, and seller payment from a total price.
// Returns (fee, royalty, sellerPayment).
func distributeFees(totalPrice, lockedFeeBps, lockedRoyaltyBps uint64) (uint64, uint64, uint64) {
	fee, afterFee := calculateFeeWithBps(totalPrice, lockedFeeBps)
	royalty := uint64(0)
	if lockedRoyaltyBps > 0 {
		royalty = safeMul(totalPrice, lockedRoyaltyBps) / 10000
	}
	sellerPayment := safeSub(afterFee, royalty)
	return fee, royalty, sellerPayment
}

// distributeFeesBig splits totalPrice into (fee, royalty, sellerPayment).
// big.Int counterpart of distributeFees; the uint64 distributeFees stays
// until its last caller (offers/auctions) is migrated, then is removed in Task 5.
func distributeFeesBig(totalPrice *big.Int, lockedFeeBps, lockedRoyaltyBps uint64) (*big.Int, *big.Int, *big.Int) {
	fee := mMulBpsDiv(totalPrice, lockedFeeBps)
	royalty := mMulBpsDiv(totalPrice, lockedRoyaltyBps)
	sellerPayment := mSub(mSub(totalPrice, fee), royalty)
	return fee, royalty, sellerPayment
}

// ===================================
// JSON Response Helper
// ===================================

func jsonResponse(marshaler interface{ MarshalTinyJSON(*jwriter.Writer) }) *string {
	w := jwriter.Writer{}
	marshaler.MarshalTinyJSON(&w)
	result := string(w.Buffer.BuildBytes())
	return &result
}

// ===================================
// Cross-Contract Helpers (big.Int / approval)
// ===================================

// jsonBoolField returns true iff "<field>": true appears in s.
func jsonBoolField(s, field string) bool {
	key := `"` + field + `":`
	idx := -1
	for i := 0; i <= len(s)-len(key); i++ {
		if s[i:i+len(key)] == key {
			idx = i + len(key)
			break
		}
	}
	if idx < 0 {
		return false
	}
	for idx < len(s) && (s[idx] == ' ' || s[idx] == '\t' || s[idx] == '\n') {
		idx++
	}
	return idx+4 <= len(s) && s[idx:idx+4] == "true"
}

// nftIsApprovedForAll checks operator approval on the NFT contract.
func nftIsApprovedForAll(nftContract, account, operator string) bool {
	payload := `{"account":"` + account + `","operator":"` + operator + `"}`
	result := sdk.ContractCallSimple(nftContract, "isApprovedForAll", payload)
	if result == nil {
		return false
	}
	return jsonBoolField(*result, "approved")
}

// tokenBalanceOf reads an ERC-20-style balance as big.Int (string or numeric JSON).
func tokenBalanceOf(tokenContract, account string) *big.Int {
	payload := `{"account":"` + account + `"}`
	result := sdk.ContractCallSimple(tokenContract, "balanceOf", payload)
	if result == nil {
		return big.NewInt(0)
	}
	s := *result
	key := `"balance":`
	idx := -1
	for i := 0; i <= len(s)-len(key); i++ {
		if s[i:i+len(key)] == key {
			idx = i + len(key)
			break
		}
	}
	if idx < 0 {
		return big.NewInt(0)
	}
	for idx < len(s) && (s[idx] == ' ' || s[idx] == '\t' || s[idx] == '\n') {
		idx++
	}
	if idx < len(s) && s[idx] == '"' {
		idx++
	}
	end := idx
	for end < len(s) && s[end] >= '0' && s[end] <= '9' {
		end++
	}
	if end == idx {
		return big.NewInt(0)
	}
	v, ok := new(big.Int).SetString(s[idx:end], 10)
	if !ok {
		return big.NewInt(0)
	}
	return v
}

// escrowIn pulls `requested` from payer into the marketplace and returns the
// ACTUAL amount received (balance-delta), robust to fee-on-transfer / utxo deduct_fee.
func escrowIn(paymentToken, payer string, requested *big.Int) *big.Int {
	contractAddr := getContractAddress()
	before := tokenBalanceOf(paymentToken, contractAddr)
	tokenTransferFromBig(paymentToken, payer, contractAddr, requested)
	after := tokenBalanceOf(paymentToken, contractAddr)
	received := mSub(after, before)
	if mIsZero(received) {
		sdk.Abort("no payment received")
	}
	return received
}

// big.Int variants of the token call helpers (added now, swapped in later tasks).
func tokenTransferFromBig(tokenContract, from, to string, amount *big.Int) {
	payload := `{"from":"` + from + `","to":"` + to + `","amount":"` + formatMoney(amount) + `"}`
	if sdk.ContractCallSimple(tokenContract, "transferFrom", payload) == nil {
		sdk.Abort("transferFrom call failed")
	}
}

func tokenTransferBig(tokenContract, to string, amount *big.Int) {
	payload := `{"to":"` + to + `","amount":"` + formatMoney(amount) + `"}`
	if sdk.ContractCallSimple(tokenContract, "transfer", payload) == nil {
		sdk.Abort("transfer call failed")
	}
}
