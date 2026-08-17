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

func safeAdd(a, b uint64) uint64 {
	if a+b < a {
		sdk.Abort("safeAdd overflow")
	}
	return a + b
}

func safeMul(a, b uint64) uint64 {
	if a != 0 && b > ^uint64(0)/a {
		sdk.Abort("safeMul overflow")
	}
	return a * b
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

// ---- Per-collection fee override helpers (B3) ----

func collectionFeeKey(nftContract string) string {
	return "cfee|" + nftContract
}

func setCollectionFeeState(nftContract string, bps uint64) {
	setStringState(collectionFeeKey(nftContract), strconv.FormatUint(bps, 10))
}

func clearCollectionFeeState(nftContract string) {
	sdk.StateDeleteObject(collectionFeeKey(nftContract))
}

// getEffectiveFeeBps returns the per-collection override bps if set, else the global fee bps.
// Absent override ⇒ identical to getFeeBps() — the key regression guarantee.
func getEffectiveFeeBps(nftContract string) uint64 {
	v := sdk.StateGetObject(collectionFeeKey(nftContract))
	if v == nil || *v == "" {
		return getFeeBps()
	}
	bps, _ := strconv.ParseUint(*v, 10, 64)
	return bps
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
// Royalty Split Helpers (B1)
// ===================================

func rsplitKey(nftContract string, suffix string) string {
	return "rsplit|" + nftContract + "|" + suffix
}

func setRoyaltySplits(nft string, recips []string, bpss []uint64) {
	// Drop any stale entries from a previous (longer) split set before
	// rewriting — otherwise resolveRoyaltySplits would still see them via
	// getRoyaltySplitCount once n is bumped back up later.
	clearRoyaltySplits(nft)
	n := uint64(len(recips))
	setUint64State(rsplitKey(nft, "n"), n)
	for i := uint64(0); i < n; i++ {
		is := strconv.FormatUint(i, 10)
		setStringState(rsplitKey(nft, is+"|r"), recips[i])
		setUint64State(rsplitKey(nft, is+"|b"), bpss[i])
	}
}

// clearRoyaltySplits drops the multi-split state for a collection so
// resolveRoyaltySplits falls back to the legacy single entry. Called from
// SetRoyalty (otherwise stale splits silently win over the new single
// recipient) and from setRoyaltySplits before re-writing (otherwise a
// shorter new set leaves old indices around).
func clearRoyaltySplits(nft string) {
	n := getUint64State(rsplitKey(nft, "n"))
	for i := uint64(0); i < n; i++ {
		is := strconv.FormatUint(i, 10)
		sdk.StateDeleteObject(rsplitKey(nft, is+"|r"))
		sdk.StateDeleteObject(rsplitKey(nft, is+"|b"))
	}
	sdk.StateDeleteObject(rsplitKey(nft, "n"))
}

func getRoyaltySplitCount(nft string) uint64 {
	return getUint64State(rsplitKey(nft, "n"))
}

func getRoyaltySplit(nft string, i uint64) (string, uint64) {
	is := strconv.FormatUint(i, 10)
	recip := getStringState(rsplitKey(nft, is+"|r"))
	bps := getUint64State(rsplitKey(nft, is+"|b"))
	return recip, bps
}

// resolveRoyaltySplits returns the effective royalty splits for an NFT collection.
// If multi-split state exists (rsplit|<nft>|n > 0) it returns those.
// Otherwise falls back to the legacy single-entry (getRoyaltyRecipient/getRoyaltyBps).
// Returns empty slices if no royalty is configured.
func resolveRoyaltySplits(nft string) ([]string, []uint64) {
	n := getRoyaltySplitCount(nft)
	if n > 0 {
		recips := make([]string, n)
		bpss := make([]uint64, n)
		for i := uint64(0); i < n; i++ {
			recips[i], bpss[i] = getRoyaltySplit(nft, i)
		}
		return recips, bpss
	}
	// Legacy fallback: single-entry split
	bps := getRoyaltyBps(nft)
	if bps > 0 {
		return []string{getRoyaltyRecipient(nft)}, []uint64{bps}
	}
	return []string{}, []uint64{}
}

// ===================================
// Royalty Split Snapshot Helpers (B2)
// Snapshots are stored per-entity (listing/offer/auction) at creation time so
// later changes to collection splits do not affect in-flight trades.
// Key scheme:
//   ls|<id>|rs_n         / of|<id>|rs_n         / au|<id>|rs_n          = count
//   ls|<id>|rs_<i>_r     / of|<id>|rs_<i>_r     / au|<id>|rs_<i>_r     = recipient
//   ls|<id>|rs_<i>_b     / of|<id>|rs_<i>_b     / au|<id>|rs_<i>_b     = bps
// ===================================

func snapshotRoyaltySplitsForListing(id uint64, recips []string, bpss []uint64) {
	n := uint64(len(recips))
	for i := uint64(0); i < n; i++ {
		is := strconv.FormatUint(i, 10)
		setListingField(id, "rs_"+is+"_r", recips[i])
		setListingUint64(id, "rs_"+is+"_b", bpss[i])
	}
	setListingUint64(id, "rs_n", n)
}

func loadListingRoyaltySplitSnapshot(id uint64, fallbackRecip string, fallbackBps uint64) ([]string, []uint64) {
	n := getListingUint64(id, "rs_n")
	if n > 0 {
		recips := make([]string, n)
		bpss := make([]uint64, n)
		for i := uint64(0); i < n; i++ {
			is := strconv.FormatUint(i, 10)
			recips[i] = getListingField(id, "rs_"+is+"_r")
			bpss[i] = getListingUint64(id, "rs_"+is+"_b")
		}
		return recips, bpss
	}
	// Legacy fallback: single entry from rr/rb
	if fallbackBps > 0 && fallbackRecip != "" {
		return []string{fallbackRecip}, []uint64{fallbackBps}
	}
	return []string{}, []uint64{}
}

func snapshotRoyaltySplitsForOffer(id uint64, recips []string, bpss []uint64) {
	n := uint64(len(recips))
	for i := uint64(0); i < n; i++ {
		is := strconv.FormatUint(i, 10)
		setOfferField(id, "rs_"+is+"_r", recips[i])
		setOfferUint64(id, "rs_"+is+"_b", bpss[i])
	}
	setOfferUint64(id, "rs_n", n)
}

func loadOfferRoyaltySplitSnapshot(id uint64, fallbackRecip string, fallbackBps uint64) ([]string, []uint64) {
	n := getOfferUint64(id, "rs_n")
	if n > 0 {
		recips := make([]string, n)
		bpss := make([]uint64, n)
		for i := uint64(0); i < n; i++ {
			is := strconv.FormatUint(i, 10)
			recips[i] = getOfferField(id, "rs_"+is+"_r")
			bpss[i] = getOfferUint64(id, "rs_"+is+"_b")
		}
		return recips, bpss
	}
	if fallbackBps > 0 && fallbackRecip != "" {
		return []string{fallbackRecip}, []uint64{fallbackBps}
	}
	return []string{}, []uint64{}
}

func snapshotRoyaltySplitsForAuction(id uint64, recips []string, bpss []uint64) {
	n := uint64(len(recips))
	for i := uint64(0); i < n; i++ {
		is := strconv.FormatUint(i, 10)
		setAuctionField(id, "rs_"+is+"_r", recips[i])
		setAuctionUint64(id, "rs_"+is+"_b", bpss[i])
	}
	setAuctionUint64(id, "rs_n", n)
}

func loadAuctionRoyaltySplitSnapshot(id uint64, fallbackRecip string, fallbackBps uint64) ([]string, []uint64) {
	n := getAuctionUint64(id, "rs_n")
	if n > 0 {
		recips := make([]string, n)
		bpss := make([]uint64, n)
		for i := uint64(0); i < n; i++ {
			is := strconv.FormatUint(i, 10)
			recips[i] = getAuctionField(id, "rs_"+is+"_r")
			bpss[i] = getAuctionUint64(id, "rs_"+is+"_b")
		}
		return recips, bpss
	}
	if fallbackBps > 0 && fallbackRecip != "" {
		return []string{fallbackRecip}, []uint64{fallbackBps}
	}
	return []string{}, []uint64{}
}

// ===================================
// Bundle State Helpers (C3)
// ===================================

func bundleKey(id uint64, field string) string {
	return "bnd|" + strconv.FormatUint(id, 10) + "|" + field
}

func setBundleField(id uint64, field, value string) {
	sdk.StateSetObject(bundleKey(id, field), value)
}

func getBundleField(id uint64, field string) string {
	return getStringState(bundleKey(id, field))
}

func getBundleUint64(id uint64, field string) uint64 {
	return getUint64State(bundleKey(id, field))
}

func setBundleUint64(id uint64, field string, val uint64) {
	setUint64State(bundleKey(id, field), val)
}

func isBundleActive(id uint64) bool {
	return getBundleField(id, "act") == "1"
}

func getNextBundleId() uint64 {
	return getUint64State("nxt_bnd")
}

func setNextBundleId(id uint64) {
	setUint64State("nxt_bnd", id)
}

// ===================================
// Bucket State Helpers (random-draw sales)
// ===================================
//
// A bucket is a pool of already-minted NFT units from ONE collection, sold at
// a fixed price where the CONTRACT picks which unit the buyer receives. Layout
// mirrors bundles (`bnd|`) so the two read the same way:
//
//	bkt|<id>|s        seller
//	bkt|<id>|nc       nft contract (one per bucket, like bundles)
//	bkt|<id>|pt       payment token
//	bkt|<id>|ps       draws per pack
//	bkt|<id>|p1       price for a single draw   ("" / zero = single sales off)
//	bkt|<id>|pp       price for one pack        ("" / zero = pack sales off)
//	bkt|<id>|act      active flag
//	bkt|<id>|exp      expiration block
//	bkt|<id>|n        entry count
//	bkt|<id>|<i>_ti   entry i token id
//	bkt|<id>|<i>_amt  entry i units REMAINING — this is what makes editions work
//	bkt|<id>|fb|rb|rr fee/royalty snapshot, as bundles
//	bkt|<id>|rs_*     resolved royalty split snapshot
//
// Entries hold already-minted units only: the seller keeps custody and the
// market moves a unit seller->buyer per draw, exactly like a listing.

func bucketKey(id uint64, field string) string {
	return "bkt|" + strconv.FormatUint(id, 10) + "|" + field
}

func setBucketField(id uint64, field, value string) {
	sdk.StateSetObject(bucketKey(id, field), value)
}

func getBucketField(id uint64, field string) string {
	return getStringState(bucketKey(id, field))
}

func getBucketUint64(id uint64, field string) uint64 {
	return getUint64State(bucketKey(id, field))
}

func setBucketUint64(id uint64, field string, val uint64) {
	setUint64State(bucketKey(id, field), val)
}

func isBucketActive(id uint64) bool {
	return getBucketField(id, "act") == "1"
}

func getNextBucketId() uint64 {
	return getUint64State("nxt_bkt")
}

func setNextBucketId(id uint64) {
	setUint64State("nxt_bkt", id)
}

// snapshotRoyaltySplitsForBucket mirrors snapshotRoyaltySplitsForBundle using bucketKey prefix.
func snapshotRoyaltySplitsForBucket(id uint64, recips []string, bpss []uint64) {
	n := uint64(len(recips))
	for i := uint64(0); i < n; i++ {
		is := strconv.FormatUint(i, 10)
		setBucketField(id, "rs_"+is+"_r", recips[i])
		setBucketUint64(id, "rs_"+is+"_b", bpss[i])
	}
	setBucketUint64(id, "rs_n", n)
}

// loadBucketRoyaltySplitSnapshot mirrors loadBundleRoyaltySplitSnapshot using bucketKey prefix.
func loadBucketRoyaltySplitSnapshot(id uint64, fallbackRecip string, fallbackBps uint64) ([]string, []uint64) {
	n := getBucketUint64(id, "rs_n")
	if n > 0 {
		recips := make([]string, n)
		bpss := make([]uint64, n)
		for i := uint64(0); i < n; i++ {
			is := strconv.FormatUint(i, 10)
			recips[i] = getBucketField(id, "rs_"+is+"_r")
			bpss[i] = getBucketUint64(id, "rs_"+is+"_b")
		}
		return recips, bpss
	}
	if fallbackBps > 0 && fallbackRecip != "" {
		return []string{fallbackRecip}, []uint64{fallbackBps}
	}
	return []string{}, []uint64{}
}

// bucketUnitsRemaining sums the units left across every entry. Entries are
// capped at MaxBucketEntries so this linear scan stays cheap, and deriving the
// total instead of storing it means it can never drift from the entries.
func bucketUnitsRemaining(id uint64) uint64 {
	n := getBucketUint64(id, "n")
	total := uint64(0)
	for i := uint64(0); i < n; i++ {
		total = safeAdd(total, getBucketUint64(id, strconv.FormatUint(i, 10)+"_amt"))
	}
	return total
}

// snapshotRoyaltySplitsForBundle mirrors snapshotRoyaltySplitsForListing using bundleKey prefix.
func snapshotRoyaltySplitsForBundle(id uint64, recips []string, bpss []uint64) {
	n := uint64(len(recips))
	for i := uint64(0); i < n; i++ {
		is := strconv.FormatUint(i, 10)
		setBundleField(id, "rs_"+is+"_r", recips[i])
		setBundleUint64(id, "rs_"+is+"_b", bpss[i])
	}
	setBundleUint64(id, "rs_n", n)
}

// loadBundleRoyaltySplitSnapshot mirrors loadListingRoyaltySplitSnapshot using bundleKey prefix.
func loadBundleRoyaltySplitSnapshot(id uint64, fallbackRecip string, fallbackBps uint64) ([]string, []uint64) {
	n := getBundleUint64(id, "rs_n")
	if n > 0 {
		recips := make([]string, n)
		bpss := make([]uint64, n)
		for i := uint64(0); i < n; i++ {
			is := strconv.FormatUint(i, 10)
			recips[i] = getBundleField(id, "rs_"+is+"_r")
			bpss[i] = getBundleUint64(id, "rs_"+is+"_b")
		}
		return recips, bpss
	}
	if fallbackBps > 0 && fallbackRecip != "" {
		return []string{fallbackRecip}, []uint64{fallbackBps}
	}
	return []string{}, []uint64{}
}

// snapshotRoyaltySplitsForSwap mirrors the bundle pair, locking the
// offered-collection royalty splits into the swap entity at propose time
// so the top-up can pay creator royalty even though the swap is barter.
func snapshotRoyaltySplitsForSwap(id uint64, recips []string, bpss []uint64) {
	n := uint64(len(recips))
	for i := uint64(0); i < n; i++ {
		is := strconv.FormatUint(i, 10)
		setSwapField(id, "rs_"+is+"_r", recips[i])
		setSwapUint64(id, "rs_"+is+"_b", bpss[i])
	}
	setSwapUint64(id, "rs_n", n)
}

func loadSwapRoyaltySplitSnapshot(id uint64, fallbackRecip string, fallbackBps uint64) ([]string, []uint64) {
	n := getSwapUint64(id, "rs_n")
	if n > 0 {
		recips := make([]string, n)
		bpss := make([]uint64, n)
		for i := uint64(0); i < n; i++ {
			is := strconv.FormatUint(i, 10)
			recips[i] = getSwapField(id, "rs_"+is+"_r")
			bpss[i] = getSwapUint64(id, "rs_"+is+"_b")
		}
		return recips, bpss
	}
	if fallbackBps > 0 && fallbackRecip != "" {
		return []string{fallbackRecip}, []uint64{fallbackBps}
	}
	return []string{}, []uint64{}
}

// ===================================
// Swap State Helpers (D1)
// ===================================

func swapKey(id uint64, field string) string {
	return "sw|" + strconv.FormatUint(id, 10) + "|" + field
}

func setSwapField(id uint64, field, value string) {
	sdk.StateSetObject(swapKey(id, field), value)
}

func getSwapField(id uint64, field string) string {
	return getStringState(swapKey(id, field))
}

func getSwapUint64(id uint64, field string) uint64 {
	return getUint64State(swapKey(id, field))
}

func setSwapUint64(id uint64, field string, val uint64) {
	setUint64State(swapKey(id, field), val)
}

func isSwapActive(id uint64) bool {
	return getSwapField(id, "act") == "1"
}

func getNextSwapId() uint64 {
	return getUint64State("nxt_sw")
}

func setNextSwapId(id uint64) {
	setUint64State("nxt_sw", id)
}

func setSwapMoney(id uint64, field string, v *big.Int) { setMoneyState(swapKey(id, field), v) }
func getSwapMoney(id uint64, field string) *big.Int    { return getMoneyState(swapKey(id, field)) }

// ===================================
// Rental State Helpers (E1)
// ===================================

func rentalKey(id uint64, field string) string {
	return "rnt|" + strconv.FormatUint(id, 10) + "|" + field
}

func setRentalField(id uint64, field, value string) {
	sdk.StateSetObject(rentalKey(id, field), value)
}

func getRentalField(id uint64, field string) string {
	return getStringState(rentalKey(id, field))
}

func getRentalUint64(id uint64, field string) uint64 {
	return getUint64State(rentalKey(id, field))
}

func setRentalUint64(id uint64, field string, val uint64) {
	setUint64State(rentalKey(id, field), val)
}

func getNextRentalId() uint64 {
	return getUint64State("nxt_rnt")
}

func setNextRentalId(id uint64) {
	setUint64State("nxt_rnt", id)
}

// snapshotRoyaltySplitsForRental mirrors snapshotRoyaltySplitsForListing using rentalKey prefix.
func snapshotRoyaltySplitsForRental(id uint64, recips []string, bpss []uint64) {
	n := uint64(len(recips))
	for i := uint64(0); i < n; i++ {
		is := strconv.FormatUint(i, 10)
		setRentalField(id, "rs_"+is+"_r", recips[i])
		setRentalUint64(id, "rs_"+is+"_b", bpss[i])
	}
	setRentalUint64(id, "rs_n", n)
}

// loadRentalRoyaltySplitSnapshot mirrors loadListingRoyaltySplitSnapshot using rentalKey prefix.
func loadRentalRoyaltySplitSnapshot(id uint64, fallbackRecip string, fallbackBps uint64) ([]string, []uint64) {
	n := getRentalUint64(id, "rs_n")
	if n > 0 {
		recips := make([]string, n)
		bpss := make([]uint64, n)
		for i := uint64(0); i < n; i++ {
			is := strconv.FormatUint(i, 10)
			recips[i] = getRentalField(id, "rs_"+is+"_r")
			bpss[i] = getRentalUint64(id, "rs_"+is+"_b")
		}
		return recips, bpss
	}
	if fallbackBps > 0 && fallbackRecip != "" {
		return []string{fallbackRecip}, []uint64{fallbackBps}
	}
	return []string{}, []uint64{}
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

// defaultMinBidIncrementBps is the floor applied in placeBid when no explicit
// min increment is configured (min_bid_inc unset = 0). Without a floor a
// bidder could outbid by a single unit repeatedly and — combined with
// anti-snipe endBlock extension — keep an English auction open indefinitely
// at near-zero cost. 1% makes the grief budget grow geometrically.
const defaultMinBidIncrementBps = 100

// effectiveMinBidIncrementBps returns the configured increment, or the 1%
// floor when unset. getMinBidIncrementBps stays raw for getInfo reporting.
func effectiveMinBidIncrementBps() uint64 {
	v := getMinBidIncrementBps()
	if v == 0 {
		return defaultMinBidIncrementBps
	}
	return v
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

// ---- Payment-token balance-decoder registry ----
//
// Binds a whitelisted token to a KNOWN raw-state layout so tokenBalanceOf
// reads the right key + encoding instead of probing `bal|` then `a-` and
// guessing — probing can silently misread a token that uses neither (or
// both) layouts as 0, which would corrupt escrowIn's balance-delta. Set per
// token via addPaymentToken's optional `decoder`. Unset = "auto" (probe),
// preserved for tokens whitelisted before this existed and for native
// HIVE/HBD (handled by isNativeAsset, never reaches the registry).
const (
	decoderMagiToken = "magi_token" // bal|<acct>, decodeTokenBig (big-endian)
	decoderUtxo      = "utxo"       // a-<acct>, decodeUtxoU64 (big-endian-trimmed)
	decoderNative    = "native"     // hive/hbd via sdk.GetBalance
)

func paymentTokenDecoderKey(token string) string {
	return "ptwd|" + token
}

func getPaymentTokenDecoder(token string) string {
	return getStringState(paymentTokenDecoderKey(token))
}

func setPaymentTokenDecoder(token, decoder string) {
	if decoder != "" {
		setStringState(paymentTokenDecoderKey(token), decoder)
	}
}

func isValidDecoder(d string) bool {
	return d == decoderMagiToken || d == decoderUtxo || d == decoderNative
}

// assertValidTokenId rejects tokenIds containing characters that would break
// out of the JSON string when concatenated into a magi_nft/delegated-mint
// payload. magi_nft itself only blocks `|`, so without this gate an
// attacker who minted an NFT with a quote-bearing id could overwrite the
// `from` field at safeTransferFrom and steal NFTs from any account that
// has approved this market. Mirrors the listMintSpots allowlist; every
// entrypoint that accepts a user-supplied tokenId must call this.
func assertValidTokenId(s string) {
	if s == "" {
		return
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') || c == ':' || c == '-' ||
			c == '_' || c == '.') {
			sdk.Abort("tokenId contains invalid characters")
		}
	}
}

// assertValidAccount rejects address/account strings that contain
// JSON-breaking characters. The market concatenates royalty/fee
// recipients into the {"to":"…","amount":"…"} payload it sends to a
// magi_token contract — a recipient with embedded quotes/braces could
// inject a second `to` field via tinyjson's last-key-wins decoder and
// redirect the payment. Same defensive shape as assertValidTokenId,
// scoped to L2 account formats (`hive:<acct>`, `did:<did>`,
// `contract:<id>`). Empty is permitted (callers pre-validate non-empty
// where required).
func assertValidAccount(s string) {
	if s == "" {
		return
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') || c == ':' || c == '-' ||
			c == '_' || c == '.') {
			sdk.Abort("account contains invalid characters")
		}
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
// the new source. The same hazard applies to every raw magi_nft state read
// keyed below (bal|, op|, sb|, owner, and allow| via nftAllowanceOf) — they
// hardcode magi_nft's internal key strings and encodings. Rationale:
// docs/superpowers/specs/2026-05-17-magi-market-contract-compatibility-design.md

// decodeTokenBig mirrors magi_token-contract bytesToBigInt (commit a819106):
// big.Int.Bytes() == big-endian unsigned magnitude; absent/empty => 0.
// Cap at 32 bytes (256-bit, the practical ERC-20 ceiling) so a malicious
// or buggy token contract that writes a multi-megabyte bal|<account> can't
// burn gas / corrupt the contract via inflated balance reads.
func decodeTokenBig(s *string) *big.Int {
	if s == nil || *s == "" {
		return big.NewInt(0)
	}
	if len(*s) > 32 {
		sdk.Abort("token balance bytes >32")
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

// ===================================
// Native asset helpers (HBD / HIVE)
// ===================================
//
// `paymentToken` was previously interpreted exclusively as a contract id
// (magi_token / utxo-mapping). The strings "hive" and "hbd" route through
// the L2 ledger via `hive.draw` (pull from caller's transfer.allow intent)
// and `hive.transfer` (pay out from the contract). Balance reads use
// `hive.get_balance`. This lets every flow that goes through the three
// primitives below — listings, auctions, mint spots, offers, bundles,
// rentals, token sales — accept native HBD/HIVE without per-flow changes.

const nativeAssetHive = "hive"
const nativeAssetHbd = "hbd"

func isNativeAsset(s string) bool {
	return s == nativeAssetHive || s == nativeAssetHbd
}

func nativeAssetOf(s string) sdk.Asset {
	if s == nativeAssetHbd {
		return sdk.AssetHbd
	}
	return sdk.AssetHive
}

// nativeAmountInt64 narrows a *big.Int to int64. The native sdk helpers
// take int64; aborts on overflow rather than silently truncating.
func nativeAmountInt64(v *big.Int) int64 {
	if v == nil {
		return 0
	}
	if !v.IsInt64() {
		sdk.Abort("native amount overflows int64")
	}
	return v.Int64()
}

func tokenBalanceOf(tokenContract, account string) *big.Int {
	d := ""
	if !isNativeAsset(tokenContract) {
		d = getPaymentTokenDecoder(tokenContract)
	}
	return tokenBalanceOfWithDecoder(tokenContract, account, d)
}

// tokenBalanceOfWithDecoder reads a balance using an ALREADY-resolved decoder
// type, so callers that already know it (escrowIn) don't re-read the registry
// key. A known type reads exactly one key with the right encoding; "auto"
// (unset) falls back to legacy probing for tokens whitelisted before the
// registry existed.
func tokenBalanceOfWithDecoder(tokenContract, account, decoder string) *big.Int {
	if isNativeAsset(tokenContract) {
		return big.NewInt(sdk.GetBalance(sdk.Address(account), nativeAssetOf(tokenContract)))
	}
	switch decoder {
	case decoderMagiToken:
		if v := sdk.ContractStateGet(tokenContract, "bal|"+account); v != nil && *v != "" {
			return decodeTokenBig(v)
		}
		return big.NewInt(0)
	case decoderUtxo:
		if v := sdk.ContractStateGet(tokenContract, "a-"+account); v != nil && *v != "" {
			return new(big.Int).SetUint64(decodeUtxoU64(v))
		}
		return big.NewInt(0)
	default:
		// "auto"/unset: probe bal| then a-.
		if v := sdk.ContractStateGet(tokenContract, "bal|"+account); v != nil && *v != "" {
			return decodeTokenBig(v)
		}
		if v := sdk.ContractStateGet(tokenContract, "a-"+account); v != nil && *v != "" {
			return new(big.Int).SetUint64(decodeUtxoU64(v))
		}
		return big.NewInt(0)
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

func nftBalanceOf(nftContract, account, tokenId string) uint64 {
	return decodeNftU64(sdk.ContractStateGet(nftContract, "bal|"+account+"|"+tokenId))
}

func nftIsApprovedForAll(nftContract, account, operator string) bool {
	v := sdk.ContractStateGet(nftContract, "op|"+account+"|"+operator)
	return v != nil && *v == "1"
}

// nftAllowanceOf reads the per-token ERC-6909 allowance the owner granted a
// spender on tokenId. Mirrors magi_nft allowanceKey
// ("allow|"+owner+"|"+spender+"|"+tokenId, verified against upstream
// feat/editioned-define-delegated-mint commit cebd5a0) whose value is
// u64ToBytes (the same little-endian-trimmed encoding decodeNftU64 reads;
// the key is deleted at 0, so absent => 0). This is a raw-state coupling
// surface — see the COUPLING WARNING above the decoders.
func nftAllowanceOf(nftContract, owner, spender, tokenId string) uint64 {
	return decodeNftU64(sdk.ContractStateGet(nftContract, "allow|"+owner+"|"+spender+"|"+tokenId))
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

// recips and bpss MUST be equal length (snapshot/resolve helpers guarantee this).
// distributeRoyaltySplitsResolved pays each recipient their bps-fraction of total,
// returns Σparts (the total actually paid out as royalties).
func distributeRoyaltySplitsResolved(paymentToken string, total *big.Int, recips []string, bpss []uint64) *big.Int {
	if len(recips) != len(bpss) {
		sdk.Abort("royalty split slices length mismatch")
	}
	sum := mZero()
	for i := range bpss {
		part := mMulBpsDiv(total, bpss[i])
		if !mIsZero(part) {
			tokenTransferBig(paymentToken, recips[i], part)
			sum = mAdd(sum, part)
		}
	}
	return sum
}

// recips and bpss MUST be equal length (snapshot/resolve helpers guarantee this).
// feeAndRoyaltyOf computes (fee, royaltyTotal, sellerPayment) from total, feeBps, and split bps slices.
// royaltyTotal = Σ mMulBpsDiv(total, bpss[i]); sellerPayment = total - fee - royaltyTotal.
func feeAndRoyaltyOf(total *big.Int, feeBps uint64, recips []string, bpss []uint64) (*big.Int, *big.Int, *big.Int) {
	if len(recips) != len(bpss) {
		sdk.Abort("royalty split slices length mismatch")
	}
	fee := mMulBpsDiv(total, feeBps)
	royaltyTotal := mZero()
	for i := range bpss {
		royaltyTotal = mAdd(royaltyTotal, mMulBpsDiv(total, bpss[i]))
	}
	sellerPayment := mSub(mSub(total, fee), royaltyTotal)
	return fee, royaltyTotal, sellerPayment
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
	native := isNativeAsset(paymentToken)
	decoder := ""
	if !native {
		decoder = getPaymentTokenDecoder(paymentToken)
	}
	// Exact-transfer tokens — native HIVE/HBD, and tokens explicitly bound to
	// the standard magi_token decoder — move precisely `requested` with no
	// fee-on-transfer, and the transfer aborts the whole tx on failure. So
	// the before/after balance-read pair is pure overhead; skip it and trust
	// the amount. Keep the delta for utxo (deduct_fee) and auto/unset tokens
	// where the received amount can legitimately differ from `requested`.
	if native || decoder == decoderMagiToken {
		tokenTransferFromBig(paymentToken, payer, contractAddr, requested)
		return new(big.Int).Set(requested)
	}
	before := tokenBalanceOfWithDecoder(paymentToken, contractAddr, decoder)
	tokenTransferFromBig(paymentToken, payer, contractAddr, requested)
	after := tokenBalanceOfWithDecoder(paymentToken, contractAddr, decoder)
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

// ===================================
// G2: Mint-Spot State Helpers
// ===================================

func mintSpotKey(id uint64, field string) string {
	return "msp|" + strconv.FormatUint(id, 10) + "|" + field
}

func setMintSpotField(id uint64, field, value string) {
	sdk.StateSetObject(mintSpotKey(id, field), value)
}

func getMintSpotField(id uint64, field string) string {
	return getStringState(mintSpotKey(id, field))
}

func getMintSpotUint64(id uint64, field string) uint64 {
	return getUint64State(mintSpotKey(id, field))
}

func setMintSpotUint64(id uint64, field string, val uint64) {
	setUint64State(mintSpotKey(id, field), val)
}

func setMintSpotMoney(id uint64, field string, v *big.Int) {
	setMoneyState(mintSpotKey(id, field), v)
}

func getMintSpotMoney(id uint64, field string) *big.Int {
	return getMoneyState(mintSpotKey(id, field))
}

func isMintSpotActive(id uint64) bool {
	return getMintSpotField(id, "act") == "1"
}

func getNextMintSpotId() uint64 {
	return getUint64State("nxt_msp")
}

func setNextMintSpotId(id uint64) {
	setUint64State("nxt_msp", id)
}

// nftMaxSupplyOf calls the nft `maxSupply` ABI for tokenId, returning 0 if
// absent/nil/non-numeric. PRECONDITION: callers must have validated tokenId
// against the [a-zA-Z0-9:-] allowlist before passing it here (it is
// concatenated into the call payload). listMintSpots enforces this.
//
// The real magi_nft contract returns {"maxSupply":<uint64>} as an UNQUOTED
// JSON number (MaxSupplyResponse marshals via out.Uint64; verified against
// upstream feat/editioned-define-delegated-mint). A quoted "<dec>" form is
// also tolerated for robustness. Returns 0 if nil/absent/unparseable.
// This is a stable-ABI call (not internal-state read) per spec Section 3.
func nftMaxSupplyOf(nftContract, tokenId string) uint64 {
	payload := `{"id":"` + tokenId + `"}`
	result := sdk.ContractCallSimple(nftContract, "maxSupply", payload)
	if result == nil || *result == "" {
		return 0
	}
	// Find the value after `"maxSupply":`, skip whitespace and an optional
	// surrounding quote, then read the decimal digit run.
	s := *result
	key := `"maxSupply":`
	idx := -1
	for i := 0; i+len(key) <= len(s); i++ {
		if s[i:i+len(key)] == key {
			idx = i + len(key)
			break
		}
	}
	if idx < 0 {
		return 0
	}
	for idx < len(s) && (s[idx] == ' ' || s[idx] == '\t' || s[idx] == '\n') {
		idx++
	}
	if idx < len(s) && s[idx] == '"' {
		idx++ // quoted-number form
	}
	end := idx
	for end < len(s) && s[end] >= '0' && s[end] <= '9' {
		end++
	}
	if end <= idx {
		return 0
	}
	val, err := strconv.ParseUint(s[idx:end], 10, 64)
	if err != nil {
		return 0
	}
	return val
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
//
// Native branch (paymentToken in {"hive","hbd"}): the L2 ledger handles
// balances via `hive.draw` (pull from the caller's signed transfer.allow
// intent) and `hive.transfer` (pay out from the contract). The contract
// itself is always the recipient of an escrow draw, so the explicit `to`
// argument is unused on the native path; we still assert `from == msg.caller`
// so the semantics match the magi_token path (caller pays).
func tokenTransferFromBig(tokenContract, from, to string, amount *big.Int) {
	if isNativeAsset(tokenContract) {
		caller := sdk.GetEnvKey("msg.caller")
		if caller == nil || *caller != from {
			sdk.Abort("native payment requires intent from msg.caller")
		}
		sdk.HiveDraw(nativeAmountInt64(amount), nativeAssetOf(tokenContract))
		return
	}
	payload := `{"from":"` + from + `","to":"` + to + `","amount":"` + formatMoney(amount) + `"}`
	if sdk.ContractCallSimple(tokenContract, "transferFrom", payload) == nil {
		sdk.Abort("transferFrom call failed")
	}
}

func tokenTransferBig(tokenContract, to string, amount *big.Int) {
	if isNativeAsset(tokenContract) {
		sdk.HiveTransfer(sdk.Address(to), nativeAmountInt64(amount), nativeAssetOf(tokenContract))
		return
	}
	payload := `{"to":"` + to + `","amount":"` + formatMoney(amount) + `"}`
	if sdk.ContractCallSimple(tokenContract, "transfer", payload) == nil {
		sdk.Abort("transfer call failed")
	}
}
