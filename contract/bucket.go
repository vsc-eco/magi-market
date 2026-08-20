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
// `buyBundle` hands over the whole lot). A bucket instead holds a stack of
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

// seedBase folds the environment into an accumulator ONCE per purchase.
//
// This used to be read per draw: three GetEnvKey calls inside the draw loop
// meant a ten-card pack made thirty env reads for three distinct values that
// cannot change mid-transaction.
func seedBase() uint64 {
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
	return acc
}

// drawSeed mixes the per-draw salt into the purchase's base seed. Cheap: no
// state or environment access, just arithmetic.
func drawSeed(base, bucketId, salt uint64) uint64 {
	acc := base
	acc ^= bucketId * 0x9E3779B97F4A7C15
	acc ^= salt + 0x165667B19E3779F9
	return splitmix64(acc)
}

// ---- Draw -------------------------------------------------------------

// MaxStaleRetries bounds how many stale entries one draw will prune before
// giving up. The seller keeps custody, so entries CAN go stale — they sold or
// moved the unit elsewhere — and pruning them is the right response. But each
// prune costs a cross-contract call, so an all-stale bucket of five hundred
// entries would burn the buyer's entire rcLimit discovering that. Prune a
// bounded number and refuse, rather than failing expensively.
const MaxStaleRetries = 12

// bucketDraw is one purchase's working set.
//
// Stack counters and chunk sums are read once and kept in memory: a ten-card
// pack re-walking the chunk sums from state on every draw would pay for the
// same handful of values ten times. Slots are written through as they change,
// since each draw touches a different one; the counters are flushed before any
// external call, which is what keeps CEI intact.
type bucketDraw struct {
	id     uint64
	loaded []bool
	nSlots []uint64
	units  []uint64
	chunks [][]uint64
	dirty  []bool
	total  uint64

	// Deliverability is asked at most once per entry per purchase. Only entries
	// actually drawn are ever checked, so this stays small — the old code had to
	// check every entry in the bucket.
	ckStack []uint64
	ckIdx  []uint64
	ckLive []bool
}

func newBucketDraw(id uint64) *bucketDraw {
	return &bucketDraw{
		id:     id,
		loaded: make([]bool, MaxBucketStacks),
		nSlots: make([]uint64, MaxBucketStacks),
		units:  make([]uint64, MaxBucketStacks),
		chunks: make([][]uint64, MaxBucketStacks),
		dirty:  make([]bool, MaxBucketStacks),
		total:  bucketUnitsRemaining(id),
	}
}

func (d *bucketDraw) load(stack uint64) {
	if d.loaded[stack] {
		return
	}
	d.loaded[stack] = true
	n := bucketStackSlots(d.id, stack)
	d.nSlots[stack] = n
	d.units[stack] = bucketStackUnits(d.id, stack)
	nc := (n + BucketChunk - 1) / BucketChunk
	cs := make([]uint64, nc)
	for j := uint64(0); j < nc; j++ {
		cs[j] = getBucketUint64(d.id, chunkField(stack, j))
	}
	d.chunks[stack] = cs
}

func (d *bucketDraw) stackUnits(stack uint64) uint64 {
	d.load(stack)
	return d.units[stack]
}

// locate maps a unit offset r in [0, units) onto the slot holding it, walking
// the chunk sums first so only one chunk is ever scanned.
func (d *bucketDraw) locate(stack, r uint64) (uint64, uint64, string, bool) {
	d.load(stack)
	acc := uint64(0)
	for j := range d.chunks[stack] {
		c := d.chunks[stack][j]
		if r < safeAdd(acc, c) {
			base := uint64(j) * BucketChunk
			end := safeAdd(base, BucketChunk)
			if end > d.nSlots[stack] {
				end = d.nSlots[stack]
			}
			for i := base; i < end; i++ {
				amt, tokenId := getBucketSlot(d.id, stack, i)
				if amt == 0 {
					continue
				}
				if r < safeAdd(acc, amt) {
					return i, amt, tokenId, true
				}
				acc = safeAdd(acc, amt)
			}
			return 0, 0, "", false
		}
		acc = safeAdd(acc, c)
	}
	return 0, 0, "", false
}

// take removes `n` units from a slot, keeping every cached sum in step.
func (d *bucketDraw) take(stack, idx, n, amt uint64, tokenId string) {
	setBucketSlot(d.id, stack, idx, amt-n, tokenId)
	d.chunks[stack][idx/BucketChunk] -= n
	d.units[stack] -= n
	d.total -= n
	d.dirty[stack] = true
}

// flush writes back the counters this purchase changed. Called BEFORE any
// external call: a re-entry must never see units that have already been
// promised to this buyer.
func (d *bucketDraw) flush() {
	for stack := uint64(0); stack < MaxBucketStacks; stack++ {
		if !d.dirty[stack] {
			continue
		}
		setBucketUint64(d.id, stackPrefix(stack)+"u", d.units[stack])
		for j := range d.chunks[stack] {
			setBucketUint64(d.id, chunkField(stack, uint64(j)), d.chunks[stack][j])
		}
	}
	setBucketUint64(d.id, "u", d.total)
}

// deliverable answers "can the seller still hand this entry over", and returns
// the units genuinely still backed. Asked at most once per entry per purchase.
func (d *bucketDraw) deliverable(stack, idx, amt uint64, tokenId, nc, seller, contractAddr string, operatorApproved bool) (uint64, bool) {
	for i := range d.ckStack {
		if d.ckStack[i] == stack && d.ckIdx[i] == idx {
			if !d.ckLive[i] {
				return 0, false
			}
			return amt, true
		}
	}

	live := true
	backed := amt
	if !operatorApproved && nftAllowanceOf(nc, seller, contractAddr, tokenId) < 1 {
		live = false
		emitBucketEntryDropped(d.id, tokenId, stack, amt, "approval revoked")
	} else if held := nftBalanceOf(nc, seller, tokenId); held < amt {
		if held == 0 {
			live = false
			emitBucketEntryDropped(d.id, tokenId, stack, amt, "seller no longer holds it")
		} else {
			// Partially moved on: keep what is genuinely still there.
			backed = held
			d.take(stack, idx, amt-held, amt, tokenId)
		}
	}

	d.ckStack = append(d.ckStack, stack)
	d.ckIdx = append(d.ckIdx, idx)
	d.ckLive = append(d.ckLive, live)
	if !live {
		return 0, false
	}
	return backed, true
}

// drawOne picks one unit from a stack, weighted by units remaining, and returns
// the token id.
func drawOne(d *bucketDraw, base, bucketId uint64, nc, seller, contractAddr string, operatorApproved bool, stack, salt uint64) string {
	attempts := d.nSlots[stack]
	if attempts > MaxStaleRetries {
		attempts = MaxStaleRetries
	}
	for attempt := uint64(0); attempt <= attempts; attempt++ {
		total := d.stackUnits(stack)
		if total == 0 {
			sdk.Abort("Bucket stack is sold out")
		}

		r := drawSeed(base, bucketId, (salt*(d.nSlots[stack]+1)+attempt)*MaxBucketStacks+stack) % total

		idx, amt, tokenId, found := d.locate(stack, r)
		if !found {
			sdk.Abort("Bucket entry table is inconsistent")
		}

		backed, ok := d.deliverable(stack, idx, amt, tokenId, nc, seller, contractAddr, operatorApproved)
		if !ok {
			// Take the whole stale entry out of play so the redraw cannot find
			// it again, and so later buyers do not pay to rediscover it.
			d.take(stack, idx, amt, amt, tokenId)
			continue
		}
		d.take(stack, idx, 1, backed, tokenId)
		return tokenId
	}

	sdk.Abort("No deliverable entries left in bucket")
	return ""
}

// nftSafeBatchTransferFrom hands over several token ids in ONE cross-contract
// call. A ten-card pack was making ten separate safeTransferFrom calls, which
// dominated the purchase's RC bill.
func nftSafeBatchTransferFrom(nftContract, from, to string, ids []string, amounts []uint64) {
	idsJSON := "["
	amtsJSON := "["
	for i := range ids {
		if i > 0 {
			idsJSON += ","
			amtsJSON += ","
		}
		idsJSON += `"` + ids[i] + `"`
		amtsJSON += strconv.FormatUint(amounts[i], 10)
	}
	idsJSON += "]"
	amtsJSON += "]"

	payload := `{"from":"` + from + `","to":"` + to + `","ids":` + idsJSON +
		`,"amounts":` + amtsJSON + `,"data":""}`
	if sdk.ContractCallSimple(nftContract, "safeBatchTransferFrom", payload) == nil {
		sdk.Abort("safeBatchTransferFrom call failed")
	}
}

// assertEntriesStockable runs the per-entry checks shared by listBucket and
// addToBucket: the ids are well-formed, not already stocked, transferable, and
// genuinely backed by units the seller holds and has authorised.
//
// `existing` says whether the bucket already holds entries. A fresh bucket only
// has to be checked against the batch in hand; a restock also has to be checked
// against what is already in state, which is what the presence markers are for.
func assertEntriesStockable(bucketId uint64, existing bool, nftContract, caller, contractAddr string, sellerIsOwner, operatorApproved bool, entries []BucketEntry) {
	// A linear scan over at most MaxEntriesPerCall ids, rather than a map: a
	// TinyGo string map allocates and hashes for what is a few hundred
	// comparisons at this size.
	seenIds := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.TokenId == "" {
			sdk.Abort("Token ID required for each bucket entry")
		}
		assertValidTokenId(e.TokenId)
		if e.Amount == 0 {
			sdk.Abort("Amount must be greater than zero for each bucket entry")
		}
		if e.Stack >= MaxBucketStacks {
			sdk.Abort("Entry stack out of range")
		}
		// A duplicate id would split one token's units across two entries and
		// silently double its draw weight relative to its real supply.
		for _, prev := range seenIds {
			if prev == e.TokenId {
				sdk.Abort("Duplicate token ID in bucket entries")
			}
		}
		seenIds = append(seenIds, e.TokenId)
		if existing && hasBucketToken(bucketId, e.TokenId) {
			sdk.Abort("Token ID already stocked in this bucket")
		}

		// Order matters: `sellerIsOwner` is one comparison, the soulbound flag is
		// a state read per entry. Testing it first skipped that read entirely for
		// a collection owner stocking their own bucket — which is the common
		// case, and the one where entry counts get large.
		if !sellerIsOwner && nftIsSoulbound(nftContract, e.TokenId) {
			sdk.Abort("Cannot stock soulbound tokens (only the collection owner can transfer them)")
		}
		if !operatorApproved {
			if nftAllowanceOf(nftContract, caller, contractAddr, e.TokenId) < e.Amount {
				sdk.Abort("Marketplace not approved as operator or sufficient per-token allowance for this NFT collection")
			}
		}
		if nftBalanceOf(nftContract, caller, e.TokenId) < e.Amount {
			sdk.Abort("Insufficient NFT balance to stock bucket")
		}
	}
}

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
	if len(p.Entries) > MaxEntriesPerCall {
		sdk.Abort("Too many bucket entries in one call — use addToBucket for the rest")
	}

	priceSingle := parseMoney(p.PricePerDraw)
	pricePack := parseMoney(p.PricePerPack)
	singleOn := !mIsZero(priceSingle)
	packOn := !mIsZero(pricePack)
	if !singleOn && !packOn {
		sdk.Abort("Set a single-draw price, a pack price, or both")
	}
	// packDraws is draws-per-stack: [5] is a flat five-card pack, [4,3,1,1] is a
	// card pack whose last slot can only be filled from stack 3.
	packSize := uint64(0)
	if packOn {
		if len(p.PackDraws) == 0 {
			sdk.Abort("Pack sales need packDraws, e.g. [5] or [4,3,1,1]")
		}
		if len(p.PackDraws) > MaxBucketStacks {
			sdk.Abort("Too many stacks in packDraws")
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
	// Hoisted: the operator approval is per (collection, seller), not per entry.
	// Asking inside the loop made listing cost scale with entry count for no
	// reason — a ten-entry bucket could not be listed inside the default
	// rcLimit at all.
	operatorApproved := nftIsApprovedForAll(p.NftContract, caller, contractAddr)
	assertEntriesStockable(0, false, p.NftContract, caller, contractAddr, sellerIsOwner, operatorApproved, p.Entries)

	// The pack-guarantee check deliberately does NOT live here.
	//
	// It used to: listing refused a bucket whose promised stacks were not all
	// stocked. That was right when a bucket arrived whole, and became wrong the
	// moment stocking was split across calls — a 500-card bucket whose rares are
	// added in a later batch would be refused on sight, which is precisely the
	// shape a Pokemon-style pack has.
	//
	// buyFromBucket re-checks every promised stack against LIVE state before any
	// money moves, which is the check that actually protects the buyer: stock can
	// drain after listing, so a list-time check was never sufficient anyway. A
	// bucket missing its rare stack is simply not buyable until the seller stocks
	// it.

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
	units := appendBucketEntries(id, p.Entries)
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

	emitBucketListed(id, caller, p.NftContract, p.PaymentToken,
		formatMoney(priceSingle), formatMoney(pricePack), p.PackDraws, p.ExpirationBlock,
		feeBps, royaltyBps, getRoyaltyRecipient(p.NftContract), p.Entries, units)
	return jsonResponse(&CreatedResponse{Success: true, Id: id})
}

//go:wasmexport addToBucket
func AddToBucket(payload *string) *string {
	assertInit()
	assertNotPaused()

	caller := getCaller()

	if payload == nil || *payload == "" {
		sdk.Abort("Payload required")
	}

	var p AddToBucketPayload
	r := jlexer.Lexer{Data: []byte(*payload)}
	p.UnmarshalTinyJSON(&r)
	if r.Error() != nil {
		sdk.Abort("Invalid payload")
	}

	// A sold-out bucket has already been deactivated, so it cannot be restocked
	// — and neither can a delisted one, which is the point: reviving a bucket
	// the seller deliberately closed should not be a side effect of restocking.
	// List a new bucket instead.
	if !isBucketActive(p.BucketId) {
		sdk.Abort("Bucket not active")
	}
	if getBucketField(p.BucketId, "s") != caller {
		sdk.Abort("Only seller can add to bucket")
	}
	if isExpired(getBucketUint64(p.BucketId, "exp")) {
		sdk.Abort("Bucket has expired")
	}
	if len(p.Entries) == 0 {
		sdk.Abort("At least one entry required")
	}
	if len(p.Entries) > MaxEntriesPerCall {
		sdk.Abort("Too many bucket entries in one call — split across several calls")
	}
	total := safeAdd(getBucketUint64(p.BucketId, "n"), uint64(len(p.Entries)))
	if total > MaxBucketEntries {
		sdk.Abort("Bucket is full")
	}

	nc := getBucketField(p.BucketId, "nc")
	// Re-validated at restock time, not just at list time: a collection denied
	// after the bucket was opened must not keep accepting stock.
	assertCollectionAllowed(nc)

	sellerIsOwner := nftGetOwner(nc) == caller
	contractAddr := getContractAddress()
	operatorApproved := nftIsApprovedForAll(nc, caller, contractAddr)
	assertEntriesStockable(p.BucketId, true, nc, caller, contractAddr, sellerIsOwner, operatorApproved, p.Entries)

	units := appendBucketEntries(p.BucketId, p.Entries)

	emitBucketRestocked(p.BucketId, caller, p.Entries, total, units)
	return jsonResponse(&SuccessResponse{Success: true})
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
	// Cost still scales with the entry count, but through the chunk walk plus
	// one in-chunk scan rather than a pass over everything — so a big bucket is
	// affordable where it used to be impossible. Refuse an oversized purchase
	// here, with a message the buyer can act on, rather than letting it run out
	// of RC mid-execution.
	entryCount := getBucketUint64(p.BucketId, "n")
	inChunk := entryCount
	if inChunk > BucketChunk {
		inChunk = BucketChunk
	}
	if safeMul(draws, safeAdd(safeAdd(entryCount/BucketChunk, inChunk), 8)) > MaxDrawWork {
		sdk.Abort("Purchase too large for this bucket — buy fewer packs at once")
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

	// Build the draw plan: which stack each draw comes from. A single draw is
	// always stack 0; a pack repeats the seller's packDraws layout once per pack.
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

	// One working set for the whole purchase: stack counters and chunk sums are
	// read once and decremented in memory, then written back before any
	// external call.
	table := newBucketDraw(p.BucketId)

	// Every stack must hold enough for its share of this purchase BEFORE any
	// money moves. Checking only the grand total would let a pack promising a
	// rare take payment and then fail mid-delivery.
	for stack := uint64(0); stack < MaxBucketStacks; stack++ {
		want := uint64(0)
		for _, pl := range plan {
			if pl == stack {
				want++
			}
		}
		if want > 0 && table.stackUnits(stack) < want {
			sdk.Abort("Not enough units left in a required stack")
		}
	}

	// Hoisted out of the draw: the operator approval and the contract address
	// are the same for every draw in the purchase, so asking per draw was ten
	// identical cross-contract calls for a ten-card pack.
	contractAddr := getContractAddress()
	operatorApproved := nftIsApprovedForAll(nc, seller, contractAddr)
	base := seedBase()

	drawn := make([]string, 0, draws)
	drawnStacks := make([]uint64, 0, draws)
	for i, stack := range plan {
		drawn = append(drawn, drawOne(table, base, p.BucketId, nc, seller, contractAddr, operatorApproved, stack, uint64(i)))
		drawnStacks = append(drawnStacks, stack)
	}

	// CEI: flush the decremented table and the sold-out flag BEFORE any external
	// call, so a re-entry through a malicious payment token or NFT contract
	// cannot see units it has already been promised.
	table.flush()
	soldOut := table.total == 0
	if soldOut {
		setBucketField(p.BucketId, "act", "0")
	}

	received := escrowIn(pt, caller, total)

	lockedFeeBps := getBucketUint64(p.BucketId, "fb")
	lockedRoyaltyBps := getBucketUint64(p.BucketId, "rb")
	royaltyRecipient := getBucketField(p.BucketId, "rr")
	snapRecips, snapBps := loadBucketRoyaltySplitSnapshot(p.BucketId, royaltyRecipient, lockedRoyaltyBps)
	fee, royTot, sellerPayment := feeAndRoyaltyOf(received, lockedFeeBps, snapRecips, snapBps)

	// Deliver everything in ONE batch transfer. Repeated draws of the same
	// token collapse into one id with a count, so a ten-card pack of four
	// distinct tokens is a single cross-contract call instead of ten.
	ids := make([]string, 0, len(drawn))
	amounts := make([]uint64, 0, len(drawn))
	for _, tokenId := range drawn {
		found := false
		for j := range ids {
			if ids[j] == tokenId {
				amounts[j]++
				found = true
				break
			}
		}
		if !found {
			ids = append(ids, tokenId)
			amounts = append(amounts, 1)
		}
	}
	nftSafeBatchTransferFrom(nc, seller, caller, ids, amounts)

	// One event per delivered unit — the indexer wants a row per NFT, not a
	// list it has to unpack.
	for i, tokenId := range drawn {
		emitBucketDraw(p.BucketId, caller, tokenId, drawnStacks[i], uint64(i))
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
	emitBucketPurchase(p.BucketId, caller, mode, draws, pt,
		formatMoney(received), formatMoney(fee), formatMoney(royTot), table.total)
	// Emitted AFTER the purchase event so a consumer sees the sale that closed
	// the bucket before it sees the bucket close.
	if soldOut {
		emitBucketSoldOut(p.BucketId, seller)
	}
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
