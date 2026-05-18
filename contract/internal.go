package main

import (
	"encoding/binary"
	"math/big"
	"magi_market/sdk"
	"strconv"

	"github.com/CosmWasm/tinyjson/jwriter"
)

// ===================================
// Safe Math Utilities
// ===================================

func safeSub(a, b uint64) uint64 {
	if b > a {
		sdk.Abort("safeSub underflow")
	}
	return a - b
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

func getMinOfferMoney() *big.Int {
	return getMoneyState("min_ofr")
}

func setMinOfferMoney(v *big.Int) {
	setMoneyState("min_ofr", v)
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

// COUPLING WARNING: the three decoders below mirror the INTERNAL binary
// storage formats of magi_nft, magi_token, and utxo-mapping. If any of those
// contracts changes its internal key format or byte encoding, magi-market
// will SILENTLY misread balances (a fund-safety bug, no compile error).
// Before upgrading any of those contracts, re-verify these decoders against
// the new source. Rationale:
// docs/superpowers/specs/2026-05-17-magi-market-contract-compatibility-design.md

// decodeTokenBig mirrors magi_token-contract bytesToBigInt (commit a819106):
// big.Int.Bytes() == big-endian unsigned magnitude; absent/empty => 0.
func decodeTokenBig(s *string) *big.Int {
	if s == nil || *s == "" {
		return big.NewInt(0)
	}
	return new(big.Int).SetBytes([]byte(*s))
}

// decodeUtxoU64 mirrors utxo-mapping getAccBal (commit 6039c43,
// contract/mapping/utils.go): big-endian uint64, leading zero bytes trimmed;
// absent/empty => 0.
func decodeUtxoU64(s *string) uint64 {
	if s == nil || *s == "" {
		return 0
	}
	b := []byte(*s)
	if len(b) > 8 {
		sdk.Abort("utxo balance bytes >8")
	}
	var buf [8]byte
	copy(buf[8-len(b):], b) // BE: right-align
	return binary.BigEndian.Uint64(buf[:])
}

// decodeNftU64 mirrors magi_nft-contract bytesToU64 (commit 223c728,
// contract/internal.go): little-endian uint64, trailing zero bytes trimmed;
// absent/empty => 0.
func decodeNftU64(s *string) uint64 {
	if s == nil || *s == "" {
		return 0
	}
	b := []byte(*s)
	if len(b) > 8 {
		sdk.Abort("nft balance bytes >8")
	}
	var buf [8]byte
	copy(buf[:], b) // LE: stored bytes are least-significant, at the start
	return binary.LittleEndian.Uint64(buf[:])
}

func tokenBalanceOf(tokenContract, account string) *big.Int {
	if v := sdk.ContractStateGet(tokenContract, "bal|"+account); v != nil && *v != "" {
		return decodeTokenBig(v)
	}
	if v := sdk.ContractStateGet(tokenContract, "a-"+account); v != nil && *v != "" {
		return new(big.Int).SetUint64(decodeUtxoU64(v))
	}
	return big.NewInt(0)
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

func nftBalanceOf(nftContract, account, tokenId string) uint64 {
	return decodeNftU64(sdk.ContractStateGet(nftContract, "bal|"+account+"|"+tokenId))
}

func nftIsApprovedForAll(nftContract, account, operator string) bool {
	v := sdk.ContractStateGet(nftContract, "op|"+account+"|"+operator)
	return v != nil && *v == "1"
}

func nftIsSoulbound(nftContract, tokenId string) bool {
	v := sdk.ContractStateGet(nftContract, "sb|"+tokenId)
	return v != nil && *v == "1"
}

func nftGetOwner(nftContract string) string {
	v := sdk.ContractStateGet(nftContract, "owner")
	if v == nil {
		return ""
	}
	return *v
}

// ===================================
// Dutch Auction Price Calculation
// ===================================

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

// distributeFeesBig splits totalPrice into (fee, royalty, sellerPayment).
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

// ---- collection denylist ----

func denylistKey(nftContract string) string {
	return "dl|" + nftContract
}

func isCollectionDenied(nftContract string) bool {
	v := sdk.StateGetObject(denylistKey(nftContract))
	return v != nil && *v == "1"
}

func setCollectionDenied(nftContract string) {
	sdk.StateSetObject(denylistKey(nftContract), "1")
}

func clearCollectionDenied(nftContract string) {
	sdk.StateDeleteObject(denylistKey(nftContract))
}

// assertCollectionAllowed aborts if the collection is on the denylist.
func assertCollectionAllowed(nftContract string) {
	if isCollectionDenied(nftContract) {
		sdk.Abort("Collection is denied")
	}
}

// ---- pending-owner (2-step transfer) ----

func getPendingOwner() string {
	v := sdk.StateGetObject("pending_owner")
	if v == nil {
		return ""
	}
	return *v
}

func setPendingOwner(addr string) {
	sdk.StateSetObject("pending_owner", addr)
}

func clearPendingOwner() {
	sdk.StateDeleteObject("pending_owner")
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
