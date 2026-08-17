package main

import (
	"magi_market/sdk"
	"math/big"
	"strconv"

	"github.com/CosmWasm/tinyjson/jlexer"
)

// ===================================
// Buckets — fixed-price sales with a contract-chosen prize
// ===================================
//
// Every other purchase path here makes the buyer name what they are buying
// (`buy` and `buyMintSpot` take a listingId, `sweep`/`batchBuy` take id arrays,
// `buyBundle` hands over the whole lot). A bucket instead holds a pool of
// already-minted units and the CONTRACT picks which one the buyer receives.
//
// Doing the pick client-side would not work: `buy` is a public custom_json, so
// anyone can read the listings, find the rare token and buy it directly. The
// pick has to be enforced where it cannot be bypassed.
//
// Because the contract enforces it, a bucket's contents can be fully public —
// buyers see exactly what is inside and can compute their own odds.

// ---- Randomness -------------------------------------------------------
//
// The seed's unpredictable component is `block.id`: the Hive L1 block id of
// the block that includes this transaction. The buyer cannot know it when they
// sign, so they cannot grind offline for a favourable draw — which is exactly
// what a tx.id-derived seed would allow, since the signer knows their own tx id
// before broadcasting.
//
// A Hive block id is 40 hex chars whose FIRST 8 are the block number in
// big-endian hex (verified against live blocks: consecutive ids differ by
// exactly 1 there). Only the trailing 32 hex chars carry block-hash entropy, so
// the whole string is folded into the mixer — truncating from the left would
// seed from the block height and be trivially predictable.

// mixSeed folds a string into an accumulator with the FNV-1a 64 constants.
// Every byte contributes, so the block number in block.id's prefix cannot
// dominate the trailing hash bytes.
func mixSeed(acc uint64, s string) uint64 {
	for i := 0; i < len(s); i++ {
		acc ^= uint64(s[i])
		acc *= 1099511628211
	}
	return acc
}

// splitmix64 — a strong finalizer. An LCG would be cheaper to predict from a
// partially-known seed; splitmix64 avalanches every input bit across the whole
// output for the same cost.
func splitmix64(x uint64) uint64 {
	x += 0x9E3779B97F4A7C15
	z := x
	z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
	z = (z ^ (z >> 27)) * 0x94D049BB133111EB
	return z ^ (z >> 31)
}

// drawSeed derives the seed for one draw.
//
// block.id supplies unpredictability. tx.id, op_index, bucketId and drawIndex
// are all buyer-known and that is fine — XOR-ing a known value into an unknown
// one leaves it unknown. They exist only so two buys landing in the same L1
// block, or two draws inside one pack, do not resolve identically.
func drawSeed(bucketId, drawIndex uint64) uint64 {
	blockId := sdk.GetEnvKey("block.id")
	if blockId == nil || len(*blockId) < 16 {
		// Fail loudly rather than quietly drawing from a degenerate seed: an
		// absent or truncated block id would collapse the entropy to values the
		// buyer controls.
		sdk.Abort("block.id unavailable — cannot draw")
	}
	acc := mixSeed(1469598103934665603, *blockId)

	if txId := sdk.GetEnvKey("tx.id"); txId != nil {
		acc = mixSeed(acc, *txId)
	}
	if opIdx := sdk.GetEnvKey("tx.op_index"); opIdx != nil {
		acc = mixSeed(acc, *opIdx)
	}
	acc ^= bucketId * 0x9E3779B97F4A7C15
	acc ^= drawIndex + 0x165667B19E3779F9
	return splitmix64(acc)
}

// ---- Draw -------------------------------------------------------------

// drawOne picks one unit from the bucket, weighted by units remaining, and
// returns the token id it landed on. It decrements that entry.
//
// The seller keeps custody (no escrow), so an entry can go stale between
// listing and draw — units transferred away, approval revoked. A stale entry is
// zeroed and the draw retried rather than aborting the purchase; pruning is
// deterministic, so every validator reaches the same result.
// drawOne picks from ONE pool. Entries carry their pool id, so a pool is just a
// filter over the same entry list — no separate storage, and the read cost per
// draw is unchanged from the single-pile case.
func drawOne(bucketId uint64, nc, seller string, pool, drawIndex uint64) string {
	n := getBucketUint64(bucketId, "n")
	contractAddr := getContractAddress()
	operatorApproved := nftIsApprovedForAll(nc, seller, contractAddr)

	// Read every entry's units ONCE per attempt and reuse that slice for both
	// the total and the weighted walk. Reading them twice (a separate
	// bucketUnitsRemaining plus the walk) doubled the state reads, and state
	// reads are what this call's RC bill is made of.
	amts := make([]uint64, n)

	// Bounded by the entry count: every attempt either delivers or removes one
	// entry from play, so this cannot spin.
	for attempt := uint64(0); attempt <= n; attempt++ {
		total := uint64(0)
		for i := uint64(0); i < n; i++ {
			is := strconv.FormatUint(i, 10)
			// Entries outside this pool are invisible to the draw. This is what
			// makes a slot a guarantee: a rare slot can only ever be filled from
			// the rare pool.
			if getBucketUint64(bucketId, is+"_pl") != pool {
				amts[i] = 0
				continue
			}
			amts[i] = getBucketUint64(bucketId, is+"_amt")
			total = safeAdd(total, amts[i])
		}
		if total == 0 {
			sdk.Abort("Bucket pool is sold out")
		}

		r := drawSeed(bucketId, (drawIndex*(n+1)+attempt)*MaxBucketPools+pool) % total

		// Walk entries accumulating units until r lands inside one. Weighted by
		// units with no per-unit storage.
		acc := uint64(0)
		idx := uint64(0)
		for i := uint64(0); i < n; i++ {
			if amts[i] == 0 {
				continue
			}
			acc = safeAdd(acc, amts[i])
			if r < acc {
				idx = i
				break
			}
		}

		is := strconv.FormatUint(idx, 10)
		amt := amts[idx]
		if amt == 0 {
			continue
		}
		tokenId := getBucketField(bucketId, is+"_ti")

		// Still deliverable? The market never escrowed it.
		if !operatorApproved && nftAllowanceOf(nc, seller, contractAddr, tokenId) < 1 {
			setBucketUint64(bucketId, is+"_amt", 0)
			emitBucketEntryDropped(bucketId, tokenId, "approval revoked")
			continue
		}
		if nftBalanceOf(nc, seller, tokenId) < 1 {
			setBucketUint64(bucketId, is+"_amt", 0)
			emitBucketEntryDropped(bucketId, tokenId, "seller no longer holds it")
			continue
		}

		setBucketUint64(bucketId, is+"_amt", amt-1)
		return tokenId
	}

	sdk.Abort("No deliverable entries left in bucket")
	return ""
}

// ---- Entrypoints ------------------------------------------------------

//go:wasmexport listBucket
func ListBucket(payload *string) *string {
	assertInit()
	assertNotPaused()

	caller := getCaller()

	if payload == nil || *payload == "" {
		sdk.Abort("Payload required")
	}

	var p ListBucketPayload
	r := jlexer.Lexer{Data: []byte(*payload)}
	p.UnmarshalTinyJSON(&r)
	if r.Error() != nil {
		sdk.Abort("Invalid payload")
	}

	if p.NftContract == "" || p.PaymentToken == "" {
		sdk.Abort("NFT contract and payment token required")
	}
	if len(p.Entries) == 0 {
		sdk.Abort("At least one entry required")
	}
	if len(p.Entries) > MaxBucketEntries {
		sdk.Abort("Too many bucket entries")
	}

	priceSingle := parseMoney(p.PricePerDraw)
	pricePack := parseMoney(p.PricePerPack)
	singleOn := !mIsZero(priceSingle)
	packOn := !mIsZero(pricePack)
	if !singleOn && !packOn {
		sdk.Abort("Set a single-draw price, a pack price, or both")
	}
	// packDraws is draws-per-pool: [5] is a flat five-card pack, [4,3,1,1] is a
	// card pack whose last slot can only be filled from pool 3.
	packSize := uint64(0)
	if packOn {
		if len(p.PackDraws) == 0 {
			sdk.Abort("Pack sales need packDraws, e.g. [5] or [4,3,1,1]")
		}
		if len(p.PackDraws) > MaxBucketPools {
			sdk.Abort("Too many pools in packDraws")
		}
		for _, d := range p.PackDraws {
			packSize = safeAdd(packSize, d)
		}
		if packSize < 1 {
			sdk.Abort("A pack must draw at least one card")
		}
		if packSize > MaxDrawsPerTx {
			sdk.Abort("Pack size exceeds the per-transaction draw cap")
		}
	}

	assertPaymentTokenAllowed(p.PaymentToken)
	assertCollectionAllowed(p.NftContract)

	// Approval-custody preflight, same as bundles: the seller keeps the units
	// and the market moves them per draw, so it needs authorization now and the
	// units have to exist. Soulbound tokens can only be moved by the collection
	// owner, so only they may stock one.
	sellerIsOwner := nftGetOwner(p.NftContract) == caller
	contractAddr := getContractAddress()
	seen := make(map[string]bool, len(p.Entries))
	for _, e := range p.Entries {
		if e.TokenId == "" {
			sdk.Abort("Token ID required for each bucket entry")
		}
		assertValidTokenId(e.TokenId)
		if e.Amount == 0 {
			sdk.Abort("Amount must be greater than zero for each bucket entry")
		}
		if e.Pool >= MaxBucketPools {
			sdk.Abort("Entry pool out of range")
		}
		// A duplicate id would split one token's units across two entries and
		// silently double its draw weight relative to its real supply.
		if seen[e.TokenId] {
			sdk.Abort("Duplicate token ID in bucket entries")
		}
		seen[e.TokenId] = true

		if nftIsSoulbound(p.NftContract, e.TokenId) && !sellerIsOwner {
			sdk.Abort("Cannot stock soulbound tokens (only the collection owner can transfer them)")
		}
		if !nftIsApprovedForAll(p.NftContract, caller, contractAddr) {
			if nftAllowanceOf(p.NftContract, caller, contractAddr, e.TokenId) < e.Amount {
				sdk.Abort("Marketplace not approved as operator or sufficient per-token allowance for this NFT collection")
			}
		}
		if nftBalanceOf(p.NftContract, caller, e.TokenId) < e.Amount {
			sdk.Abort("Insufficient NFT balance to stock bucket")
		}
	}

	// Every slot a pack promises must actually be stocked, or the bucket would
	// sell a guarantee it cannot keep. Same for pool 0 when single draws are on.
	for pool := uint64(0); pool < MaxBucketPools; pool++ {
		need := false
		if packOn && pool < uint64(len(p.PackDraws)) && p.PackDraws[pool] > 0 {
			need = true
		}
		if singleOn && pool == 0 {
			need = true
		}
		if !need {
			continue
		}
		stocked := uint64(0)
		for _, e := range p.Entries {
			if e.Pool == pool {
				stocked = safeAdd(stocked, e.Amount)
			}
		}
		if stocked == 0 {
			sdk.Abort("A pack slot has no entries in its pool")
		}
	}

	feeBps := getEffectiveFeeBps(p.NftContract)
	royaltyBps := getRoyaltyBps(p.NftContract)
	if feeBps+royaltyBps > 10000 {
		sdk.Abort("Combined fee and royalty exceed 100%")
	}

	id := getNextBucketId()
	setBucketField(id, "s", caller)
	setBucketField(id, "nc", p.NftContract)
	setBucketField(id, "pt", p.PaymentToken)
	setBucketMoney(id, "p1", priceSingle)
	setBucketMoney(id, "pp", pricePack)
	setBucketField(id, "act", "1")
	setBucketUint64(id, "exp", p.ExpirationBlock)
	setBucketUint64(id, "n", uint64(len(p.Entries)))
	units := uint64(0)
	for i, e := range p.Entries {
		is := strconv.FormatUint(uint64(i), 10)
		setBucketField(id, is+"_ti", e.TokenId)
		setBucketUint64(id, is+"_amt", e.Amount)
		setBucketUint64(id, is+"_pl", e.Pool)
		units = safeAdd(units, e.Amount)
	}
	setBucketUint64(id, "pd_n", uint64(len(p.PackDraws)))
	for j, d := range p.PackDraws {
		setBucketUint64(id, "pd_"+strconv.FormatUint(uint64(j), 10), d)
	}
	setBucketUint64(id, "ps", packSize)
	setBucketUint64(id, "fb", feeBps)
	setBucketUint64(id, "rb", royaltyBps)
	setBucketField(id, "rr", getRoyaltyRecipient(p.NftContract))
	// Snapshot resolved splits so in-flight buckets are unaffected by later changes.
	snapRecips, snapBps := resolveRoyaltySplits(p.NftContract)
	snapshotRoyaltySplitsForBucket(id, snapRecips, snapBps)
	setNextBucketId(id + 1)

	emitBucketListed(id, caller, p.NftContract, uint64(len(p.Entries)), units)
	return jsonResponse(&CreatedResponse{Success: true, Id: id})
}

//go:wasmexport buyFromBucket
func BuyFromBucket(payload *string) *string {
	assertInit()
	assertNotPaused()

	caller := getCaller()

	// Reject contract callers. Otherwise a buyer wraps this in their own
	// contract, inspects the drawn token and aborts on a bad result — the
	// revert costs only RC, so they retry until they win. `sender` is
	// necessarily a user address; msg.caller may be a contract.
	sender := sdk.GetEnvKey("msg.sender")
	if sender == nil || *sender == "" || *sender != caller {
		sdk.Abort("Buckets must be bought directly by a user account, not via a contract")
	}

	if payload == nil || *payload == "" {
		sdk.Abort("Payload required")
	}

	var p BuyFromBucketPayload
	r := jlexer.Lexer{Data: []byte(*payload)}
	p.UnmarshalTinyJSON(&r)
	if r.Error() != nil {
		sdk.Abort("Invalid payload")
	}

	if !isBucketActive(p.BucketId) {
		sdk.Abort("Bucket not active")
	}
	if isExpired(getBucketUint64(p.BucketId, "exp")) {
		sdk.Abort("Bucket has expired")
	}
	if p.Quantity == 0 {
		sdk.Abort("Quantity must be greater than zero")
	}

	nc := getBucketField(p.BucketId, "nc")
	assertCollectionAllowed(nc)

	seller := getBucketField(p.BucketId, "s")
	if caller == seller {
		sdk.Abort("Seller cannot buy from own bucket")
	}

	pt := getBucketField(p.BucketId, "pt")
	// Re-validate at buy time (de-whitelisted-after-list halt).
	assertPaymentTokenAllowed(pt)

	// Resolve mode -> draws + unit price.
	var draws uint64
	var unitPrice *big.Int
	isPack := false
	switch p.Mode {
	case "single", "":
		unitPrice = getBucketMoney(p.BucketId, "p1")
		if mIsZero(unitPrice) {
			sdk.Abort("This bucket does not sell single draws")
		}
		draws = p.Quantity
	case "pack":
		isPack = true
		unitPrice = getBucketMoney(p.BucketId, "pp")
		if mIsZero(unitPrice) {
			sdk.Abort("This bucket does not sell packs")
		}
		packSize := getBucketUint64(p.BucketId, "ps")
		if packSize == 0 {
			sdk.Abort("Bucket has no pack size")
		}
		draws = safeMul(p.Quantity, packSize)
	default:
		sdk.Abort("Mode must be \"single\" or \"pack\"")
	}

	if draws > MaxDrawsPerTx {
		sdk.Abort("Too many draws in one transaction")
	}
	// Short-fill would make a fixed pack price meaningless, so require the
	// whole purchase to be deliverable up front.
	if bucketUnitsRemaining(p.BucketId) < draws {
		sdk.Abort("Not enough units left in bucket")
	}

	total := mMulU64(unitPrice, p.Quantity)
	if p.MaxTotalPrice != "" {
		if mCmp(total, parseMoney(p.MaxTotalPrice)) > 0 {
			sdk.Abort("Total price exceeds maxTotalPrice")
		}
	}

	// Build the draw plan: which pool each draw comes from. A single draw is
	// always pool 0; a pack repeats the seller's packDraws layout once per pack.
	plan := make([]uint64, 0, draws)
	if isPack {
		pdN := getBucketUint64(p.BucketId, "pd_n")
		for q := uint64(0); q < p.Quantity; q++ {
			for j := uint64(0); j < pdN; j++ {
				cnt := getBucketUint64(p.BucketId, "pd_"+strconv.FormatUint(j, 10))
				for k := uint64(0); k < cnt; k++ {
					plan = append(plan, j)
				}
			}
		}
	} else {
		for i := uint64(0); i < draws; i++ {
			plan = append(plan, 0)
		}
	}

	// Every pool must hold enough for its share of this purchase BEFORE any
	// money moves. Checking only the grand total would let a pack promising a
	// rare take payment and then fail mid-delivery.
	for pool := uint64(0); pool < MaxBucketPools; pool++ {
		want := uint64(0)
		for _, pl := range plan {
			if pl == pool {
				want++
			}
		}
		if want > 0 && bucketPoolUnits(p.BucketId, pool) < want {
			sdk.Abort("Not enough units left in a required pool")
		}
	}

	// CEI: every state field this purchase touches is written before any
	// external call. drawOne decrements entries first, and the sold-out flag is
	// flipped below — a re-entry through a malicious payment token or NFT
	// contract must never see units it has already been promised.
	drawn := make([]string, 0, draws)
	for i, pool := range plan {
		drawn = append(drawn, drawOne(p.BucketId, nc, seller, pool, uint64(i)))
	}
	if bucketUnitsRemaining(p.BucketId) == 0 {
		setBucketField(p.BucketId, "act", "0")
	}

	received := escrowIn(pt, caller, total)

	lockedFeeBps := getBucketUint64(p.BucketId, "fb")
	lockedRoyaltyBps := getBucketUint64(p.BucketId, "rb")
	royaltyRecipient := getBucketField(p.BucketId, "rr")
	snapRecips, snapBps := loadBucketRoyaltySplitSnapshot(p.BucketId, royaltyRecipient, lockedRoyaltyBps)
	fee, royTot, sellerPayment := feeAndRoyaltyOf(received, lockedFeeBps, snapRecips, snapBps)

	// Deliver every drawn unit before any payout, so an abort anywhere reverts
	// the whole purchase. One event per delivered unit — the indexer wants a
	// row per NFT, not a list it has to unpack.
	for i, tokenId := range drawn {
		nftSafeTransferFrom(nc, seller, caller, tokenId, 1)
		emitBucketDraw(p.BucketId, caller, tokenId, uint64(i))
	}

	if !mIsZero(fee) {
		tokenTransferBig(pt, getFeeRecipient(), fee)
	}
	distributeRoyaltySplitsResolved(pt, received, snapRecips, snapBps)
	if !mIsZero(sellerPayment) {
		tokenTransferBig(pt, seller, sellerPayment)
	}

	mode := p.Mode
	if mode == "" {
		mode = "single"
	}
	emitBucketPurchase(p.BucketId, caller, mode, draws, formatMoney(received), formatMoney(fee), formatMoney(royTot))
	return jsonResponse(&SuccessResponse{Success: true})
}

//go:wasmexport delistBucket
func DelistBucket(payload *string) *string {
	assertInit()

	caller := getCaller()

	if payload == nil || *payload == "" {
		sdk.Abort("Payload required")
	}

	var p BucketIdPayload
	r := jlexer.Lexer{Data: []byte(*payload)}
	p.UnmarshalTinyJSON(&r)
	if r.Error() != nil {
		sdk.Abort("Invalid payload")
	}

	if !isBucketActive(p.BucketId) {
		sdk.Abort("Bucket not active")
	}
	if getBucketField(p.BucketId, "s") != caller {
		sdk.Abort("Only seller can delist bucket")
	}

	setBucketField(p.BucketId, "act", "0")
	emitBucketDelisted(p.BucketId, caller)
	return jsonResponse(&SuccessResponse{Success: true})
}
