package contract_test

import (
	"fmt"
	"strings"
	"testing"

	"vsc-node/lib/test_utils"
	contract_session "vsc-node/modules/contract/session"

	"github.com/stretchr/testify/assert"
)

// Mirrors the contract's own caps. MaxBucketEntries is what a bucket may hold
// in TOTAL; MaxEntriesPerCall is what one listBucket/addToBucket call may add.
const MaxBucketEntriesContract = 512
const MaxEntriesPerCallContract = 24
const BucketChunkContract = 32

const MaxBucketEntriesTest = 5

// Buckets: fixed-price sales where the CONTRACT picks which unit the buyer
// gets. These tests pin the behaviour that makes that safe and fair — the draw
// is weighted by units, every drawn unit is actually delivered, disabled sale
// modes are refused, and the pool cannot be over-drawn.

// bucketEntriesJSON builds the entries array for a listBucket payload.
func bucketEntriesJSON(entries [][2]string) string {
	s := "["
	for i, e := range entries {
		if i > 0 {
			s += ","
		}
		s += fmt.Sprintf(`{"tokenId":"%s","amount":%s,"pool":0}`, e[0], e[1])
	}
	return s + "]"
}

// bucketPoolEntriesJSON builds entries spread across pools: {tokenId, amount, pool}.
func bucketPoolEntriesJSON(entries [][3]string) string {
	s := "["
	for i, e := range entries {
		if i > 0 {
			s += ","
		}
		s += fmt.Sprintf(`{"tokenId":"%s","amount":%s,"pool":%s}`, e[0], e[1], e[2])
	}
	return s + "]"
}

// listBucket stocks a bucket and returns its id.
func listBucket(t *testing.T, ct *test_utils.ContractTest, seller, entries, priceDraw, pricePack, packDraws string) uint64 {
	t.Helper()
	payload := fmt.Sprintf(
		`{"nftContract":"%s","entries":%s,"paymentToken":"%s","pricePerDraw":"%s","pricePerPack":"%s","packDraws":%s,"expirationBlock":0}`,
		NftContractID, entries, TokenID, priceDraw, pricePack, packDraws)
	res, _, _ := CallMarket(t, ct, "listBucket", []byte(payload), nil, seller, "", true, gas, "")
	return ParseCreated(res).Id
}

// TestBucketSingleDrawDelivers: one draw hands over exactly one unit of one of
// the stocked tokens, and the money splits like any other sale.
func TestBucketSingleDrawDelivers(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	seller := ownerAddress
	buyer := "hive:buyer"

	MintNft(t, ct, seller, "b1", 1, 10)
	MintNft(t, ct, seller, "b2", 1, 10)
	ApproveNftForMarket(t, ct, seller)
	MintAndApproveToken(t, ct, buyer, 20000)

	feeBefore := QueryTokenBalance(t, ct, feeRecipientAddress)
	sellerBefore := QueryTokenBalance(t, ct, seller)

	id := listBucket(t, ct, seller, bucketEntriesJSON([][2]string{{"b1", "1"}, {"b2", "1"}}), "10000", "0", "[]")

	buy := fmt.Sprintf(`{"bucketId":%d,"mode":"single","quantity":1,"maxTotalPrice":""}`, id)
	_, _, logs := CallMarket(t, ct, "buyFromBucket", []byte(buy), nil, buyer, "", true, gas, "")

	// Exactly one unit moved, and it is one of the two stocked tokens.
	got := QueryNftBalance(t, ct, buyer, "b1") + QueryNftBalance(t, ct, buyer, "b2")
	assert.Equal(t, uint64(1), got, "buyer should hold exactly one drawn unit")

	// The seller keeps the other one — nothing else left the wallet.
	left := QueryNftBalance(t, ct, seller, "b1") + QueryNftBalance(t, ct, seller, "b2")
	assert.Equal(t, uint64(1), left, "seller should keep the undrawn unit")

	// 250 bps fee on 10000, no royalty configured.
	assert.Equal(t, feeBefore+250, QueryTokenBalance(t, ct, feeRecipientAddress), "fee recipient paid")
	assert.Equal(t, sellerBefore+9750, QueryTokenBalance(t, ct, seller), "seller paid net")

	AssertEventEmitted(t, logs, "bucket_draw")
	AssertEventEmitted(t, logs, "bucket_purchase")
}

// TestBucketDrawIsWeightedByUnits: an entry stocked with many units must win
// far more often than a 1/1. This is what makes editions behave like commons
// and a single NFT like a chase card.
func TestBucketDrawIsWeightedByUnits(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	seller := ownerAddress

	// 1 rare unit against 19 common units.
	MintNft(t, ct, seller, "rare", 1, 10)
	MintNft(t, ct, seller, "common", 19, 100)
	ApproveNftForMarket(t, ct, seller)
	// Spread the draws across several buyers: an account's RC free tier only
	// covers a handful of draws, and one buyer looping 20 times hits
	// "cost limit exceeded" rather than testing the draw.
	buyers := []string{"hive:buyer1", "hive:buyer2", "hive:buyer3", "hive:buyer4"}
	for _, b := range buyers {
		MintAndApproveToken(t, ct, b, 100000)
	}

	id := listBucket(t, ct, seller, bucketEntriesJSON([][2]string{{"rare", "1"}, {"common", "19"}}), "1000", "0", "[]")

	// Draw the pool down one at a time; every draw must deliver something.
	for _, b := range buyers {
		for i := 0; i < 5; i++ {
			buy := fmt.Sprintf(`{"bucketId":%d,"mode":"single","quantity":1,"maxTotalPrice":""}`, id)
			CallMarket(t, ct, "buyFromBucket", []byte(buy), nil, b, "", true, gas, "")
		}
	}

	rare := uint64(0)
	common := uint64(0)
	for _, b := range buyers {
		rare += QueryNftBalance(t, ct, b, "rare")
		common += QueryNftBalance(t, ct, b, "common")
	}

	// Draining the whole pool must yield exactly the stocked supply — no unit
	// invented, none lost, regardless of the order they came out in.
	assert.Equal(t, uint64(1), rare, "the single rare unit is drawn exactly once")
	assert.Equal(t, uint64(19), common, "all common units are drawn")
	// The whole pool moved: 20 draws delivered exactly the 20 stocked units.
	assert.Equal(t, uint64(0), QueryNftBalance(t, ct, seller, "rare"))
	assert.Equal(t, uint64(0), QueryNftBalance(t, ct, seller, "common"))
}

// TestBucketPackDrawsMultiple: a pack purchase delivers packSize units in one
// transaction and charges the pack price once.
func TestBucketPackDrawsMultiple(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	seller := ownerAddress
	buyer := "hive:buyer"

	MintNft(t, ct, seller, "p1", 10, 100)
	ApproveNftForMarket(t, ct, seller)
	MintAndApproveToken(t, ct, buyer, 100000)

	sellerBefore := QueryTokenBalance(t, ct, seller)
	id := listBucket(t, ct, seller, bucketEntriesJSON([][2]string{{"p1", "10"}}), "0", "10000", "[3]")

	buy := fmt.Sprintf(`{"bucketId":%d,"mode":"pack","quantity":1,"maxTotalPrice":""}`, id)
	CallMarket(t, ct, "buyFromBucket", []byte(buy), nil, buyer, "", true, gas, "")

	assert.Equal(t, uint64(3), QueryNftBalance(t, ct, buyer, "p1"), "a pack of 3 delivers 3 units")
	assert.Equal(t, uint64(7), QueryNftBalance(t, ct, seller, "p1"), "7 units remain with the seller")
	// One pack price, not three single prices.
	assert.Equal(t, sellerBefore+9750, QueryTokenBalance(t, ct, seller), "seller paid one pack price net of fee")
}

// TestBucketRefusesDisabledMode: a bucket that only sells packs must refuse a
// single draw, and vice versa — otherwise a buyer could pay the wrong price.
func TestBucketRefusesDisabledMode(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	seller := ownerAddress
	buyer := "hive:buyer"

	MintNft(t, ct, seller, "d1", 5, 100)
	ApproveNftForMarket(t, ct, seller)
	MintAndApproveToken(t, ct, buyer, 100000)

	// Pack-only bucket.
	packOnly := listBucket(t, ct, seller, bucketEntriesJSON([][2]string{{"d1", "5"}}), "0", "10000", "[2]")
	buy := fmt.Sprintf(`{"bucketId":%d,"mode":"single","quantity":1,"maxTotalPrice":""}`, packOnly)
	// single draw must be refused on a pack-only bucket
	CallMarket(t, ct, "buyFromBucket", []byte(buy), nil, buyer, "", false, gas, "")

	// Single-only bucket.
	singleOnly := listBucket(t, ct, seller, bucketEntriesJSON([][2]string{{"d1", "3"}}), "1000", "0", "[]")
	buy = fmt.Sprintf(`{"bucketId":%d,"mode":"pack","quantity":1,"maxTotalPrice":""}`, singleOnly)
	// pack purchase must be refused on a single-only bucket
	CallMarket(t, ct, "buyFromBucket", []byte(buy), nil, buyer, "", false, gas, "")
}

// TestBucketCannotOverdraw: asking for more units than remain is refused
// outright rather than short-filling, which would make a fixed pack price
// meaningless.
func TestBucketCannotOverdraw(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	seller := ownerAddress
	buyer := "hive:buyer"

	MintNft(t, ct, seller, "o1", 2, 100)
	ApproveNftForMarket(t, ct, seller)
	MintAndApproveToken(t, ct, buyer, 100000)

	id := listBucket(t, ct, seller, bucketEntriesJSON([][2]string{{"o1", "2"}}), "1000", "5000", "[5]")

	// packSize 5 against 2 remaining units.
	buy := fmt.Sprintf(`{"bucketId":%d,"mode":"pack","quantity":1,"maxTotalPrice":""}`, id)
	// a pack larger than the remaining pool must abort
	CallMarket(t, ct, "buyFromBucket", []byte(buy), nil, buyer, "", false, gas, "")

	// Nothing was taken on the failed attempt.
	assert.Equal(t, uint64(0), QueryNftBalance(t, ct, buyer, "o1"), "failed purchase delivers nothing")
	assert.Equal(t, uint64(2), QueryNftBalance(t, ct, seller, "o1"), "failed purchase leaves the pool intact")

	// Draining exactly the remaining units is fine, and then it is sold out.
	for i := 0; i < 2; i++ {
		single := fmt.Sprintf(`{"bucketId":%d,"mode":"single","quantity":1,"maxTotalPrice":""}`, id)
		CallMarket(t, ct, "buyFromBucket", []byte(single), nil, buyer, "", true, gas, "")
	}
	single := fmt.Sprintf(`{"bucketId":%d,"mode":"single","quantity":1,"maxTotalPrice":""}`, id)
	// a drained bucket must refuse further draws
	CallMarket(t, ct, "buyFromBucket", []byte(single), nil, buyer, "", false, gas, "")
}

// TestBucketRejectsDuplicateEntries: the same token id twice would split its
// units across two entries and silently double its draw weight against its real
// supply.
func TestBucketRejectsDuplicateEntries(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	seller := ownerAddress
	MintNft(t, ct, seller, "dup", 4, 100)
	ApproveNftForMarket(t, ct, seller)

	payload := fmt.Sprintf(
		`{"nftContract":"%s","entries":%s,"paymentToken":"%s","pricePerDraw":"1000","pricePerPack":"0","packDraws":[],"expirationBlock":0}`,
		NftContractID, bucketEntriesJSON([][2]string{{"dup", "2"}, {"dup", "2"}}), TokenID)
	// duplicate token ids in one bucket must be refused
	CallMarket(t, ct, "listBucket", []byte(payload), nil, seller, "", false, gas, "")
}

// TestBucketDelistStopsSales: only the seller can delist, and a delisted bucket
// sells nothing.
func TestBucketDelistStopsSales(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	seller := ownerAddress
	buyer := "hive:buyer"

	MintNft(t, ct, seller, "x1", 3, 100)
	ApproveNftForMarket(t, ct, seller)
	MintAndApproveToken(t, ct, buyer, 100000)

	id := listBucket(t, ct, seller, bucketEntriesJSON([][2]string{{"x1", "3"}}), "1000", "0", "[]")

	del := fmt.Sprintf(`{"bucketId":%d}`, id)
	// a non-seller must not be able to delist
	CallMarket(t, ct, "delistBucket", []byte(del), nil, buyer, "", false, gas, "")

	CallMarket(t, ct, "delistBucket", []byte(del), nil, seller, "", true, gas, "")

	buy := fmt.Sprintf(`{"bucketId":%d,"mode":"single","quantity":1,"maxTotalPrice":""}`, id)
	// a delisted bucket must refuse purchases
	CallMarket(t, ct, "buyFromBucket", []byte(buy), nil, buyer, "", false, gas, "")
	assert.Equal(t, uint64(3), QueryNftBalance(t, ct, seller, "x1"), "delisting returns nothing — custody never moved")
}

// TestBucketSellerCannotBuyOwn mirrors the guard every other sale path carries.
func TestBucketSellerCannotBuyOwn(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	seller := ownerAddress
	MintNft(t, ct, seller, "s1", 3, 100)
	ApproveNftForMarket(t, ct, seller)
	MintAndApproveToken(t, ct, seller, 100000)

	id := listBucket(t, ct, seller, bucketEntriesJSON([][2]string{{"s1", "3"}}), "1000", "0", "[]")
	buy := fmt.Sprintf(`{"bucketId":%d,"mode":"single","quantity":1,"maxTotalPrice":""}`, id)
	// the seller must not draw from their own bucket
	CallMarket(t, ct, "buyFromBucket", []byte(buy), nil, seller, "", false, gas, "")
}

// TestBucketPokemonStylePack: pools turn a bucket into a real card pack. Four
// commons, three uncommons and one guaranteed rare per pack — the rare slot can
// only be filled from the rare pool, so it is a promise rather than a
// probability.
func TestBucketPokemonStylePack(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	seller := ownerAddress

	// Enough stock for two packs: 8 commons, 6 uncommons, 2 rares.
	MintNft(t, ct, seller, "common", 8, 100)
	MintNft(t, ct, seller, "uncommon", 6, 100)
	MintNft(t, ct, seller, "holo", 4, 100)
	MintNft(t, ct, seller, "rare", 2, 100)
	ApproveNftForMarket(t, ct, seller)

	entries := bucketPoolEntriesJSON([][3]string{
		{"common", "8", "0"},
		{"uncommon", "6", "1"},
		{"holo", "4", "2"},
		{"rare", "2", "3"},
	})
	// A real 10-card pack: 4 commons + 3 uncommons + 2 holos + 1 rare. This is
	// the shape the feature exists for, and it fits a default-rcLimit purchase.
	id := listBucket(t, ct, seller, entries, "0", "10000", "[4,3,2,1]")

	// Two buyers so neither runs out of RC mid-test.
	for _, b := range []string{"hive:packbuyer1", "hive:packbuyer2"} {
		MintAndApproveToken(t, ct, b, 100000)
		buy := fmt.Sprintf(`{"bucketId":%d,"mode":"pack","quantity":1,"maxTotalPrice":""}`, id)
		// `gas` here is a test-side assertion ceiling, not a chain limit — a
		// 10-draw pack legitimately costs more than a single-item action.
		CallMarket(t, ct, "buyFromBucket", []byte(buy), nil, b, "", true, uint(1_200_000_000), "")

		// Every pack has exactly the promised shape.
		assert.Equal(t, uint64(4), QueryNftBalance(t, ct, b, "common"), "4 commons per pack")
		assert.Equal(t, uint64(3), QueryNftBalance(t, ct, b, "uncommon"), "3 uncommons per pack")
		assert.Equal(t, uint64(2), QueryNftBalance(t, ct, b, "holo"), "2 holos per pack")
		assert.Equal(t, uint64(1), QueryNftBalance(t, ct, b, "rare"), "every pack contains its rare")
	}

	// The whole stock is gone, and the bucket closed itself.
	assert.Equal(t, uint64(0), QueryNftBalance(t, ct, seller, "rare"), "both rares were dealt")
	assert.Equal(t, uint64(0), QueryNftBalance(t, ct, seller, "common"))
}

// TestBucketRefusesPackWithEmptySlotPool: a pack that promises a rare must not
// be listable when the rare pool is empty — otherwise the guarantee is a lie the
// buyer only discovers at purchase time.
func TestBucketRefusesPackWithEmptySlotPool(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	seller := ownerAddress
	buyer := "hive:emptyslotbuyer"
	MintNft(t, ct, seller, "onlycommon", 10, 100)
	ApproveNftForMarket(t, ct, seller)
	MintAndApproveToken(t, ct, buyer, 100000)

	// packDraws asks for a draw from pool 1, but nothing is stocked there.
	//
	// Listing SUCCEEDS: stocking is split across calls now, so a bucket whose
	// rare pool arrives in a later addToBucket is normal, not an error — that is
	// exactly the shape a Pokemon-style pack has. The guarantee is enforced
	// where it can be enforced against live state instead.
	entries := bucketPoolEntriesJSON([][3]string{{"onlycommon", "10", "0"}})
	payload := fmt.Sprintf(
		`{"nftContract":"%s","entries":%s,"paymentToken":"%s","pricePerDraw":"0","pricePerPack":"10000","packDraws":[4,1],"expirationBlock":0}`,
		NftContractID, entries, TokenID)
	res, _, _ := CallMarket(t, ct, "listBucket", []byte(payload), nil, seller, "", true, gas, "")
	id := ParseCreated(res).Id

	// Buying is refused, and refused BEFORE any money moves — a pack that cannot
	// fill its rare slot must never be paid for.
	before := QueryTokenBalance(t, ct, buyer)
	buy := fmt.Sprintf(`{"bucketId":%d,"mode":"pack","quantity":1,"maxTotalPrice":""}`, id)
	CallMarket(t, ct, "buyFromBucket", []byte(buy), nil, buyer, "", false, gas,
		"Not enough units left in a required pool")
	assert.Equal(t, before, QueryTokenBalance(t, ct, buyer), "buyer must not be charged")

	// Once the rare pool is stocked, the same purchase goes through and the
	// guaranteed slot is honoured.
	MintNft(t, ct, seller, "therare", 1, 1)
	add := fmt.Sprintf(`{"bucketId":%d,"entries":%s}`, id,
		bucketPoolEntriesJSON([][3]string{{"therare", "1", "1"}}))
	CallMarket(t, ct, "addToBucket", []byte(add), nil, seller, "", true, gas, "")
	_, _, logs := CallMarket(t, ct, "buyFromBucket", []byte(buy), nil, buyer, "", true, gas, "")
	assert.Equal(t, uint64(1), QueryNftBalance(t, ct, buyer, "therare"),
		"the guaranteed slot must be filled from the rare pool")
	assert.Len(t, drawnTokenIds(logs), 5, "a [4,1] pack delivers five cards")
}

// TestBucketPackStopsWhenRarePoolEmpties: once the guaranteed slot's pool runs
// out, packs must stop rather than quietly delivering a pack without its rare.
func TestBucketPackStopsWhenRarePoolEmpties(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	seller := ownerAddress
	MintNft(t, ct, seller, "c", 20, 100)
	MintNft(t, ct, seller, "r", 1, 100)
	ApproveNftForMarket(t, ct, seller)

	entries := bucketPoolEntriesJSON([][3]string{{"c", "20", "0"}, {"r", "1", "1"}})
	id := listBucket(t, ct, seller, entries, "0", "10000", "[2,1]")

	b1 := "hive:rarebuyer1"
	b2 := "hive:rarebuyer2"
	MintAndApproveToken(t, ct, b1, 100000)
	MintAndApproveToken(t, ct, b2, 100000)

	buy := fmt.Sprintf(`{"bucketId":%d,"mode":"pack","quantity":1,"maxTotalPrice":""}`, id)
	CallMarket(t, ct, "buyFromBucket", []byte(buy), nil, b1, "", true, gas, "")
	assert.Equal(t, uint64(1), QueryNftBalance(t, ct, b1, "r"), "first pack takes the only rare")

	// Commons remain, but the rare pool is empty — the pack can no longer keep
	// its promise, so it must refuse.
	CallMarket(t, ct, "buyFromBucket", []byte(buy), nil, b2, "", false, gas, "")
	assert.Equal(t, uint64(0), QueryNftBalance(t, ct, b2, "c"), "a refused pack delivers nothing")
}

// TestBucketLargePurchaseFitsDefaultRcLimit: buying several packs at once is the
// case that scales worst, so pin it. Four 5-card packs is 20 draws in one
// transaction — this must stay inside the 10000 rcLimit the SDK sends, or the
// cap is a promise the contract cannot keep.
func TestBucketLargePurchaseFitsDefaultRcLimit(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	seller := ownerAddress
	buyer := "hive:bulkbuyer"

	MintNft(t, ct, seller, "bulk-a", 12, 100)
	MintNft(t, ct, seller, "bulk-b", 8, 100)
	ApproveNftForMarket(t, ct, seller)
	MintAndApproveToken(t, ct, buyer, 500000)

	entries := bucketPoolEntriesJSON([][3]string{{"bulk-a", "12", "0"}, {"bulk-b", "8", "1"}})
	// 3 from pool 0 + 2 from pool 1 = a 5-card pack.
	id := listBucket(t, ct, seller, entries, "0", "10000", "[3,2]")

	// Four packs in one call = 20 draws.
	buy := fmt.Sprintf(`{"bucketId":%d,"mode":"pack","quantity":4,"maxTotalPrice":""}`, id)
	CallMarket(t, ct, "buyFromBucket", []byte(buy), nil, buyer, "", true, uint(2_000_000_000), "")

	// Every pack keeps its shape: 4 packs x 3 = 12 from pool 0, 4 x 2 = 8 from pool 1.
	assert.Equal(t, uint64(12), QueryNftBalance(t, ct, buyer, "bulk-a"), "12 units from pool 0")
	assert.Equal(t, uint64(8), QueryNftBalance(t, ct, buyer, "bulk-b"), "8 units from pool 1")
	assert.Equal(t, uint64(0), QueryNftBalance(t, ct, seller, "bulk-a"), "seller drained")
	assert.Equal(t, uint64(0), QueryNftBalance(t, ct, seller, "bulk-b"))
}

// TestBucketWorstCaseRcHeadroom pins the most expensive purchase the contract
// permits: MaxDrawsPerTx draws against a bucket holding MaxBucketEntries
// entries. Marginal cost scales with BOTH, since every draw scans the entry
// table, so the cap is only honest if this fits the 10000 rcLimit the SDK
// sends. If this starts failing, lower MaxDrawsPerTx rather than hoping.
func TestBucketWorstCaseRcHeadroom(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	seller := ownerAddress
	buyer := "hive:worstcase"

	// 5 ids x 6 units across 2 pools: pool 1 gets 12 units, enough for three
	// 6-card packs (9 per pool).
	entries := make([][3]string, 0, MaxBucketEntriesTest)
	for i := 0; i < MaxBucketEntriesTest; i++ {
		id := fmt.Sprintf("w%02d", i)
		MintNft(t, ct, seller, id, 6, 100)
		pool := "0"
		if i%2 == 1 {
			pool = "1"
		}
		entries = append(entries, [3]string{id, "6", pool})
	}
	ApproveNftForMarket(t, ct, seller)
	MintAndApproveToken(t, ct, buyer, 500000)

	// 6-card packs (3 from each pool) against a 5-entry table: work per pack is
	// 6 * (5+4) = 54, so five packs (270) fit and six (324) do not.
	id := listBucket(t, ct, seller, bucketPoolEntriesJSON(entries), "0", "10000", "[3,3]")

	// Six packs = 36 draws: over both MaxDrawsPerTx and the work bound. Refused
	// up front with a message the buyer can act on, NOT an opaque "cost limit
	// exceeded" mid-execution.
	tooBig := fmt.Sprintf(`{"bucketId":%d,"mode":"pack","quantity":6,"maxTotalPrice":""}`, id)
	CallMarket(t, ct, "buyFromBucket", []byte(tooBig), nil, buyer, "", false, uint(4_000_000_000),
		"Too many draws in one transaction")

	// Three packs (18 draws, work 162) must fit the default rcLimit.
	ok := fmt.Sprintf(`{"bucketId":%d,"mode":"pack","quantity":3,"maxTotalPrice":""}`, id)
	CallMarket(t, ct, "buyFromBucket", []byte(ok), nil, buyer, "", true, uint(4_000_000_000), "")

	total := uint64(0)
	for i := 0; i < MaxBucketEntriesTest; i++ {
		total += QueryNftBalance(t, ct, buyer, fmt.Sprintf("w%02d", i))
	}
	assert.Equal(t, uint64(18), total, "18 draws delivered across three packs")
}

// ---------------------------------------------------------------------------
// The promises above are the fun ones. These cover the load-bearing behaviour
// underneath: money correctness, the no-escrow consequences, and the guards
// that only matter when something has already gone wrong.
// ---------------------------------------------------------------------------

// TestBucketRoyaltySplitsArePaid: every earlier test ran with no royalty
// configured, so the split payout path was never exercised for buckets at all.
func TestBucketRoyaltySplitsArePaid(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	seller := ownerAddress
	buyer := "hive:royaltybuyer"

	// 500 bps of royalty, split 300/200 between two recipients.
	splits := fmt.Sprintf(`{"nftContract":"%s","splits":[{"recipient":"hive:artist","bps":300},{"recipient":"hive:studio","bps":200}]}`, NftContractID)
	CallMarket(t, ct, "setRoyaltySplits", []byte(splits), nil, ownerAddress, "", true, gas, "")

	MintNft(t, ct, seller, "roy", 4, 100)
	ApproveNftForMarket(t, ct, seller)
	MintAndApproveToken(t, ct, buyer, 100000)

	artistBefore := QueryTokenBalance(t, ct, "hive:artist")
	studioBefore := QueryTokenBalance(t, ct, "hive:studio")
	feeBefore := QueryTokenBalance(t, ct, feeRecipientAddress)
	sellerBefore := QueryTokenBalance(t, ct, seller)

	id := listBucket(t, ct, seller, bucketEntriesJSON([][2]string{{"roy", "4"}}), "10000", "0", "[]")
	buy := fmt.Sprintf(`{"bucketId":%d,"mode":"single","quantity":1,"maxTotalPrice":""}`, id)
	CallMarket(t, ct, "buyFromBucket", []byte(buy), nil, buyer, "", true, gas, "")

	// 10000 sale: 250 fee (250 bps), 300 + 200 royalty, seller keeps the rest.
	assert.Equal(t, feeBefore+250, QueryTokenBalance(t, ct, feeRecipientAddress), "fee recipient")
	assert.Equal(t, artistBefore+300, QueryTokenBalance(t, ct, "hive:artist"), "artist split")
	assert.Equal(t, studioBefore+200, QueryTokenBalance(t, ct, "hive:studio"), "studio split")
	assert.Equal(t, sellerBefore+9250, QueryTokenBalance(t, ct, seller), "seller nets the remainder")
}

// TestBucketFeeAndRoyaltyAreSnapshotAtListTime: raising the fee or royalty after
// a bucket is listed must not change what an in-flight bucket charges. This is
// the guarantee the snapshot exists for, and nothing tested it.
func TestBucketFeeAndRoyaltyAreSnapshotAtListTime(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	seller := ownerAddress
	buyer := "hive:snapbuyer"

	MintNft(t, ct, seller, "snap", 4, 100)
	ApproveNftForMarket(t, ct, seller)
	MintAndApproveToken(t, ct, buyer, 100000)

	// Listed under the default 250 bps fee and no royalty.
	id := listBucket(t, ct, seller, bucketEntriesJSON([][2]string{{"snap", "4"}}), "10000", "0", "[]")

	// Now move both sharply, AFTER listing.
	CallMarket(t, ct, "setFee", []byte(`{"feeBps":1000}`), nil, ownerAddress, "", true, gas, "")
	splits := fmt.Sprintf(`{"nftContract":"%s","splits":[{"recipient":"hive:artist","bps":2000}]}`, NftContractID)
	CallMarket(t, ct, "setRoyaltySplits", []byte(splits), nil, ownerAddress, "", true, gas, "")

	feeBefore := QueryTokenBalance(t, ct, feeRecipientAddress)
	artistBefore := QueryTokenBalance(t, ct, "hive:artist")
	sellerBefore := QueryTokenBalance(t, ct, seller)

	buy := fmt.Sprintf(`{"bucketId":%d,"mode":"single","quantity":1,"maxTotalPrice":""}`, id)
	CallMarket(t, ct, "buyFromBucket", []byte(buy), nil, buyer, "", true, gas, "")

	// The OLD terms apply: 250 fee, no royalty, seller keeps 9750.
	assert.Equal(t, feeBefore+250, QueryTokenBalance(t, ct, feeRecipientAddress), "fee stays at the listed 250 bps")
	assert.Equal(t, artistBefore, QueryTokenBalance(t, ct, "hive:artist"), "a royalty added after listing must not apply")
	assert.Equal(t, sellerBefore+9750, QueryTokenBalance(t, ct, seller), "seller nets the listed terms")
}

// TestBucketMaxTotalPriceGuard: the slippage guard every other buy path carries.
func TestBucketMaxTotalPriceGuard(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	seller := ownerAddress
	buyer := "hive:slipbuyer"

	MintNft(t, ct, seller, "slip", 6, 100)
	ApproveNftForMarket(t, ct, seller)
	MintAndApproveToken(t, ct, buyer, 100000)

	id := listBucket(t, ct, seller, bucketEntriesJSON([][2]string{{"slip", "6"}}), "1000", "0", "[]")

	// Two draws cost 2000; a 1500 ceiling must refuse.
	tooTight := fmt.Sprintf(`{"bucketId":%d,"mode":"single","quantity":2,"maxTotalPrice":"1500"}`, id)
	CallMarket(t, ct, "buyFromBucket", []byte(tooTight), nil, buyer, "", false, gas, "Total price exceeds maxTotalPrice")
	assert.Equal(t, uint64(0), QueryNftBalance(t, ct, buyer, "slip"), "a refused buy delivers nothing")

	// The exact total is fine.
	ok := fmt.Sprintf(`{"bucketId":%d,"mode":"single","quantity":2,"maxTotalPrice":"2000"}`, id)
	CallMarket(t, ct, "buyFromBucket", []byte(ok), nil, buyer, "", true, gas, "")
	assert.Equal(t, uint64(2), QueryNftBalance(t, ct, buyer, "slip"), "two singles in one call")
}

// TestBucketExpires: an expired bucket sells nothing, even with units left.
func TestBucketExpires(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	seller := ownerAddress
	buyer := "hive:latebuyer"

	MintNft(t, ct, seller, "exp", 4, 100)
	ApproveNftForMarket(t, ct, seller)
	MintAndApproveToken(t, ct, buyer, 100000)

	ct.BlockHeight = 100
	payload := fmt.Sprintf(
		`{"nftContract":"%s","entries":%s,"paymentToken":"%s","pricePerDraw":"1000","pricePerPack":"0","packDraws":[],"expirationBlock":150}`,
		NftContractID, bucketEntriesJSON([][2]string{{"exp", "4"}}), TokenID)
	res, _, _ := CallMarket(t, ct, "listBucket", []byte(payload), nil, seller, "", true, gas, "")
	id := ParseCreated(res).Id

	// Before expiry: fine.
	buy := fmt.Sprintf(`{"bucketId":%d,"mode":"single","quantity":1,"maxTotalPrice":""}`, id)
	CallMarket(t, ct, "buyFromBucket", []byte(buy), nil, buyer, "", true, gas, "")

	// Past it: refused, though three units remain.
	ct.BlockHeight = 200
	CallMarket(t, ct, "buyFromBucket", []byte(buy), nil, buyer, "", false, gas, "Bucket has expired")
	assert.Equal(t, uint64(3), QueryNftBalance(t, ct, seller, "exp"), "unsold units stay with the seller")
}

// TestBucketPrunesStaleEntryAndStillDelivers is the test the no-escrow design
// most needs. The market never takes custody, so a seller can move units out
// from under a listed bucket. A draw that lands on such an entry must drop it,
// say so, and redraw — one stale entry must not fail the whole purchase.
func TestBucketPrunesStaleEntryAndStillDelivers(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	seller := ownerAddress
	buyer := "hive:prunebuyer"

	MintNft(t, ct, seller, "gone", 5, 100)
	MintNft(t, ct, seller, "stays", 5, 100)
	ApproveNftForMarket(t, ct, seller)
	MintAndApproveToken(t, ct, buyer, 100000)

	id := listBucket(t, ct, seller, bucketEntriesJSON([][2]string{{"gone", "5"}, {"stays", "5"}}), "1000", "0", "[]")

	// The seller moves every unit of "gone" elsewhere AFTER listing.
	away := fmt.Sprintf(`{"from":"%s","to":"hive:elsewhere","id":"gone","amount":5,"data":""}`, seller)
	CallNft(t, ct, "safeTransferFrom", []byte(away), nil, seller, true, gas, "")
	assert.Equal(t, uint64(0), QueryNftBalance(t, ct, seller, "gone"), "seller no longer holds it")

	// The purchase still succeeds, delivering from the entry that is left.
	buy := fmt.Sprintf(`{"bucketId":%d,"mode":"single","quantity":1,"maxTotalPrice":""}`, id)
	_, _, logs := CallMarket(t, ct, "buyFromBucket", []byte(buy), nil, buyer, "", true, gas, "")

	assert.Equal(t, uint64(1), QueryNftBalance(t, ct, buyer, "stays"), "delivered from the live entry")
	assert.Equal(t, uint64(0), QueryNftBalance(t, ct, buyer, "gone"), "never delivers what the seller moved away")

	// The units did not vanish silently — the drop is on the record.
	AssertEventEmitted(t, logs, "bucket_entry_dropped")
}

// TestBucketNonOwnerCannotStockSoulbound: soulbound tokens can only be moved by
// the collection owner, so anyone else stocking one would be selling a prize
// that can never be delivered.
func TestBucketNonOwnerCannotStockSoulbound(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	holder := "hive:sbholder"

	// Mint a soulbound token, then hand it to someone who is NOT the collection
	// owner — allowed, because the transfer is FROM the owner.
	CallNft(t, ct, "mint", []byte(`{"to":"hive:tibfox","id":"sb1","amount":2,"maxSupply":2,"soulbound":true}`),
		nil, ownerAddress, true, gas, "")
	away := fmt.Sprintf(`{"from":"%s","to":"%s","id":"sb1","amount":2,"data":""}`, ownerAddress, holder)
	CallNft(t, ct, "safeTransferFrom", []byte(away), nil, ownerAddress, true, gas, "")

	approve := `{"operator":"contract:market","approved":true}`
	CallNft(t, ct, "setApprovalForAll", []byte(approve), nil, holder, true, gas, "")

	payload := fmt.Sprintf(
		`{"nftContract":"%s","entries":%s,"paymentToken":"%s","pricePerDraw":"1000","pricePerPack":"0","packDraws":[],"expirationBlock":0}`,
		NftContractID, bucketEntriesJSON([][2]string{{"sb1", "2"}}), TokenID)
	CallMarket(t, ct, "listBucket", []byte(payload), nil, holder, "", false, gas, "Cannot stock soulbound tokens")
}

// TestBucketRefusesContractCaller proves the guard that makes a random draw
// fair: buyFromBucket must refuse to run when a CONTRACT is calling it.
//
// Without this, the draw is not random in any meaningful sense. A buyer deploys
// a contract that calls buyFromBucket, inspects the token it drew and aborts on
// a bad result — the abort reverts the whole transaction, so the losing draw
// costs them only RC. They retry until the rare comes out, turning odds into a
// choice. The market sees msg.caller = their contract while msg.sender stays
// the human who signed, so caller == sender rejects exactly that shape.
func TestBucketRefusesContractCaller(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	seller := ownerAddress
	attacker := "hive:attacker"

	MintNft(t, ct, seller, "prize", 5, 100)
	ApproveNftForMarket(t, ct, seller)
	MintAndApproveToken(t, ct, attacker, 100000)
	// The mock is deliberately NOT funded: a contract account cannot sign an
	// approve, and it does not need to. The guard fires before any payment is
	// pulled, which is the point — the refusal is about who is calling, not
	// about whether they could pay.

	id := listBucket(t, ct, seller, bucketEntriesJSON([][2]string{{"prize", "5"}}), "1000", "0", "[]")

	// Straight from a user account: allowed.
	direct := fmt.Sprintf(`{"bucketId":%d,"mode":"single","quantity":1,"maxTotalPrice":""}`, id)
	CallMarket(t, ct, "buyFromBucket", []byte(direct), nil, attacker, "", true, gas, "")
	assert.Equal(t, uint64(1), QueryNftBalance(t, ct, attacker, "prize"), "a direct buy works")

	// Through a contract: refused.
	through := fmt.Sprintf(`{"market":"%s","bucketId":%d}`, MarketContractID, id)
	callContract(t, ct, CallerMockID, "buyThrough", []byte(through), nil, attacker,
		defaultTimestamp, false, gas, "")

	// Nothing moved on the refused attempt — not to the mock, not to the human.
	assert.Equal(t, uint64(0), QueryNftBalance(t, ct, "contract:"+CallerMockID, "prize"),
		"the contract must not receive a draw")
	assert.Equal(t, uint64(1), QueryNftBalance(t, ct, attacker, "prize"),
		"and the refused call must not deliver to the signer either")
	assert.Equal(t, uint64(4), QueryNftBalance(t, ct, seller, "prize"), "only the direct buy drew a unit")
}

// ---------------------------------------------------------------------------
// Validation. Table-driven: every one of these is a distinct way to ask the
// contract for something incoherent, and each must be refused with a message
// that says which one it was — a bucket that lists on bad input sells a promise
// it cannot keep.
// ---------------------------------------------------------------------------

func TestBucketListValidation(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	seller := ownerAddress
	MintNft(t, ct, seller, "v1", 4, 100)
	MintNft(t, ct, seller, "v2", 4, 100)
	ApproveNftForMarket(t, ct, seller)

	one := bucketEntriesJSON([][2]string{{"v1", "2"}})

	cases := []struct {
		name    string
		payload string
		expect  string
	}{
		{"no nft contract",
			fmt.Sprintf(`{"nftContract":"","entries":%s,"paymentToken":"%s","pricePerDraw":"1000","pricePerPack":"0","packDraws":[],"expirationBlock":0}`, one, TokenID),
			"NFT contract and payment token required"},
		{"no payment token",
			fmt.Sprintf(`{"nftContract":"%s","entries":%s,"paymentToken":"","pricePerDraw":"1000","pricePerPack":"0","packDraws":[],"expirationBlock":0}`, NftContractID, one),
			"NFT contract and payment token required"},
		{"no entries",
			fmt.Sprintf(`{"nftContract":"%s","entries":[],"paymentToken":"%s","pricePerDraw":"1000","pricePerPack":"0","packDraws":[],"expirationBlock":0}`, NftContractID, TokenID),
			"At least one entry required"},
		{"empty token id",
			fmt.Sprintf(`{"nftContract":"%s","entries":[{"tokenId":"","amount":1,"pool":0}],"paymentToken":"%s","pricePerDraw":"1000","pricePerPack":"0","packDraws":[],"expirationBlock":0}`, NftContractID, TokenID),
			"Token ID required for each bucket entry"},
		{"zero amount",
			fmt.Sprintf(`{"nftContract":"%s","entries":[{"tokenId":"v1","amount":0,"pool":0}],"paymentToken":"%s","pricePerDraw":"1000","pricePerPack":"0","packDraws":[],"expirationBlock":0}`, NftContractID, TokenID),
			"Amount must be greater than zero"},
		{"pool out of range",
			fmt.Sprintf(`{"nftContract":"%s","entries":[{"tokenId":"v1","amount":1,"pool":99}],"paymentToken":"%s","pricePerDraw":"1000","pricePerPack":"0","packDraws":[],"expirationBlock":0}`, NftContractID, TokenID),
			"Entry pool out of range"},
		{"no price at all",
			fmt.Sprintf(`{"nftContract":"%s","entries":%s,"paymentToken":"%s","pricePerDraw":"0","pricePerPack":"0","packDraws":[],"expirationBlock":0}`, NftContractID, one, TokenID),
			"Set a single-draw price, a pack price, or both"},
		{"pack price without packDraws",
			fmt.Sprintf(`{"nftContract":"%s","entries":%s,"paymentToken":"%s","pricePerDraw":"0","pricePerPack":"1000","packDraws":[],"expirationBlock":0}`, NftContractID, one, TokenID),
			"Pack sales need packDraws"},
		{"packDraws all zero",
			fmt.Sprintf(`{"nftContract":"%s","entries":%s,"paymentToken":"%s","pricePerDraw":"0","pricePerPack":"1000","packDraws":[0,0],"expirationBlock":0}`, NftContractID, one, TokenID),
			"A pack must draw at least one card"},
		{"pack bigger than the draw cap",
			fmt.Sprintf(`{"nftContract":"%s","entries":%s,"paymentToken":"%s","pricePerDraw":"0","pricePerPack":"1000","packDraws":[99],"expirationBlock":0}`, NftContractID, one, TokenID),
			"Pack size exceeds the per-transaction draw cap"},
		{"too many pools",
			fmt.Sprintf(`{"nftContract":"%s","entries":%s,"paymentToken":"%s","pricePerDraw":"0","pricePerPack":"1000","packDraws":[1,1,1,1,1,1,1,1,1],"expirationBlock":0}`, NftContractID, one, TokenID),
			"Too many pools in packDraws"},
		{"more units than the seller holds",
			fmt.Sprintf(`{"nftContract":"%s","entries":[{"tokenId":"v1","amount":999,"pool":0}],"paymentToken":"%s","pricePerDraw":"1000","pricePerPack":"0","packDraws":[],"expirationBlock":0}`, NftContractID, TokenID),
			"Insufficient NFT balance to stock bucket"},
		{"token id with invalid characters",
			fmt.Sprintf(`{"nftContract":"%s","entries":[{"tokenId":"bad\",\"from\":\"hive:victim","amount":1,"pool":0}],"paymentToken":"%s","pricePerDraw":"1000","pricePerPack":"0","packDraws":[],"expirationBlock":0}`, NftContractID, TokenID),
			"tokenId contains invalid characters"},
		{"payment token not whitelisted",
			fmt.Sprintf(`{"nftContract":"%s","entries":%s,"paymentToken":"nosuchtoken","pricePerDraw":"1000","pricePerPack":"0","packDraws":[],"expirationBlock":0}`, NftContractID, one),
			"not allowed"},
		{"empty payload", ``, "Payload required"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			CallMarket(t, ct, "listBucket", []byte(c.payload), nil, seller, "", false, gas, c.expect)
		})
	}
}

func TestBucketBuyValidation(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	seller := ownerAddress
	buyer := "hive:valbuyer"
	MintNft(t, ct, seller, "bv", 4, 100)
	ApproveNftForMarket(t, ct, seller)
	MintAndApproveToken(t, ct, buyer, 100000)

	id := listBucket(t, ct, seller, bucketEntriesJSON([][2]string{{"bv", "4"}}), "1000", "0", "[]")

	cases := []struct {
		name    string
		payload string
		expect  string
	}{
		{"zero quantity",
			fmt.Sprintf(`{"bucketId":%d,"mode":"single","quantity":0,"maxTotalPrice":""}`, id),
			"Quantity must be greater than zero"},
		{"unknown mode",
			fmt.Sprintf(`{"bucketId":%d,"mode":"lucky-dip","quantity":1,"maxTotalPrice":""}`, id),
			"Mode must be"},
		{"bucket that never existed",
			`{"bucketId":9999,"mode":"single","quantity":1,"maxTotalPrice":""}`,
			"Bucket not active"},
		{"more units than remain",
			fmt.Sprintf(`{"bucketId":%d,"mode":"single","quantity":9,"maxTotalPrice":""}`, id),
			"Not enough units left"},
		{"empty payload", ``, "Payload required"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			CallMarket(t, ct, "buyFromBucket", []byte(c.payload), nil, buyer, "", false, gas, c.expect)
		})
	}

	// None of the refusals touched the bucket.
	assert.Equal(t, uint64(4), QueryNftBalance(t, ct, seller, "bv"), "refused buys leave the pool intact")
}

func TestBucketDelistValidation(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	seller := ownerAddress
	MintNft(t, ct, seller, "dv", 2, 100)
	ApproveNftForMarket(t, ct, seller)
	id := listBucket(t, ct, seller, bucketEntriesJSON([][2]string{{"dv", "2"}}), "1000", "0", "[]")

	CallMarket(t, ct, "delistBucket", []byte(``), nil, seller, "", false, gas, "Payload required")
	CallMarket(t, ct, "delistBucket", []byte(`{"bucketId":9999}`), nil, seller, "", false, gas, "Bucket not active")

	del := fmt.Sprintf(`{"bucketId":%d}`, id)
	CallMarket(t, ct, "delistBucket", []byte(del), nil, seller, "", true, gas, "")
	// Delisting twice is refused rather than silently succeeding.
	CallMarket(t, ct, "delistBucket", []byte(del), nil, seller, "", false, gas, "Bucket not active")
}

// ---------------------------------------------------------------------------
// Governance and custody paths: the situations where the world changes AFTER a
// bucket is listed.
// ---------------------------------------------------------------------------

// TestBucketPauseStopsTradingButNotDelisting: pausing must stop new listings and
// purchases, while leaving the seller a way out. Delisting deliberately has no
// pause check — a paused market must not trap a seller's NFTs in a bucket.
func TestBucketPauseStopsTradingButNotDelisting(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	seller := ownerAddress
	buyer := "hive:pausebuyer"
	MintNft(t, ct, seller, "pz", 4, 100)
	ApproveNftForMarket(t, ct, seller)
	MintAndApproveToken(t, ct, buyer, 100000)

	id := listBucket(t, ct, seller, bucketEntriesJSON([][2]string{{"pz", "4"}}), "1000", "0", "[]")

	CallMarket(t, ct, "pause", nil, nil, ownerAddress, "", true, gas, "")

	buy := fmt.Sprintf(`{"bucketId":%d,"mode":"single","quantity":1,"maxTotalPrice":""}`, id)
	CallMarket(t, ct, "buyFromBucket", []byte(buy), nil, buyer, "", false, gas, "paused")

	payload := fmt.Sprintf(
		`{"nftContract":"%s","entries":%s,"paymentToken":"%s","pricePerDraw":"1000","pricePerPack":"0","packDraws":[],"expirationBlock":0}`,
		NftContractID, bucketEntriesJSON([][2]string{{"pz", "1"}}), TokenID)
	CallMarket(t, ct, "listBucket", []byte(payload), nil, seller, "", false, gas, "paused")

	// The seller can still withdraw the bucket while paused.
	CallMarket(t, ct, "delistBucket", []byte(fmt.Sprintf(`{"bucketId":%d}`, id)), nil, seller, "", true, gas, "")
	assert.Equal(t, uint64(4), QueryNftBalance(t, ct, seller, "pz"), "custody never moved, so nothing to return")
}

// TestBucketDeniedCollectionBlocked: denylisting a collection must stop both
// listing and buying, including for buckets listed before the denial.
func TestBucketDeniedCollectionBlocked(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	seller := ownerAddress
	buyer := "hive:denybuyer"
	MintNft(t, ct, seller, "dn", 4, 100)
	ApproveNftForMarket(t, ct, seller)
	MintAndApproveToken(t, ct, buyer, 100000)

	id := listBucket(t, ct, seller, bucketEntriesJSON([][2]string{{"dn", "4"}}), "1000", "0", "[]")

	deny := fmt.Sprintf(`{"nftContract":"%s"}`, NftContractID)
	CallMarket(t, ct, "denyCollection", []byte(deny), nil, ownerAddress, "", true, gas, "")

	// Re-validated at buy time, not just at list time.
	buy := fmt.Sprintf(`{"bucketId":%d,"mode":"single","quantity":1,"maxTotalPrice":""}`, id)
	CallMarket(t, ct, "buyFromBucket", []byte(buy), nil, buyer, "", false, gas, "denied")

	payload := fmt.Sprintf(
		`{"nftContract":"%s","entries":%s,"paymentToken":"%s","pricePerDraw":"1000","pricePerPack":"0","packDraws":[],"expirationBlock":0}`,
		NftContractID, bucketEntriesJSON([][2]string{{"dn", "1"}}), TokenID)
	CallMarket(t, ct, "listBucket", []byte(payload), nil, seller, "", false, gas, "denied")
}

// TestBucketPaymentTokenRemovedAfterListing: a payment token de-whitelisted
// after a bucket is listed must halt sales, the same re-validation the offer
// path does.
func TestBucketPaymentTokenRemovedAfterListing(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	seller := ownerAddress
	buyer := "hive:ptbuyer"
	MintNft(t, ct, seller, "pt1", 4, 100)
	ApproveNftForMarket(t, ct, seller)
	MintAndApproveToken(t, ct, buyer, 100000)

	id := listBucket(t, ct, seller, bucketEntriesJSON([][2]string{{"pt1", "4"}}), "1000", "0", "[]")

	CallMarket(t, ct, "removePaymentToken", []byte(fmt.Sprintf(`{"token":"%s"}`, TokenID)),
		nil, ownerAddress, "", true, gas, "")

	buy := fmt.Sprintf(`{"bucketId":%d,"mode":"single","quantity":1,"maxTotalPrice":""}`, id)
	CallMarket(t, ct, "buyFromBucket", []byte(buy), nil, buyer, "", false, gas, "not allowed")
	assert.Equal(t, uint64(4), QueryNftBalance(t, ct, seller, "pt1"), "no units moved")
}

// TestBucketWorksWithPerTokenAllowance: a seller can authorise the market with a
// capped ERC-6909 allowance instead of blanket operator approval. Every other
// bucket test uses setApprovalForAll, so this branch was never exercised.
func TestBucketWorksWithPerTokenAllowance(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	seller := ownerAddress
	buyer := "hive:allowbuyer"

	MintNft(t, ct, seller, "al", 5, 100)
	// Per-token approve ONLY — deliberately no setApprovalForAll.
	CallNft(t, ct, "approve",
		[]byte(fmt.Sprintf(`{"spender":"%s","id":"al","amount":5}`, MarketContractAddress)),
		nil, seller, true, gas, "")
	MintAndApproveToken(t, ct, buyer, 100000)

	id := listBucket(t, ct, seller, bucketEntriesJSON([][2]string{{"al", "5"}}), "1000", "0", "[]")

	buy := fmt.Sprintf(`{"bucketId":%d,"mode":"single","quantity":1,"maxTotalPrice":""}`, id)
	CallMarket(t, ct, "buyFromBucket", []byte(buy), nil, buyer, "", true, gas, "")
	assert.Equal(t, uint64(1), QueryNftBalance(t, ct, buyer, "al"), "allowance authorises the draw")
}

// TestBucketAllEntriesStaleAbortsCleanly: pruning covers a bucket that still has
// something to give. When the seller has emptied it entirely, the purchase must
// abort — and take nothing with it.
func TestBucketAllEntriesStaleAbortsCleanly(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	seller := ownerAddress
	buyer := "hive:stalebuyer"
	MintNft(t, ct, seller, "st", 4, 100)
	ApproveNftForMarket(t, ct, seller)
	MintAndApproveToken(t, ct, buyer, 100000)

	id := listBucket(t, ct, seller, bucketEntriesJSON([][2]string{{"st", "4"}}), "1000", "0", "[]")

	buyerBefore := QueryTokenBalance(t, ct, buyer)

	// Seller empties the wallet after listing.
	away := fmt.Sprintf(`{"from":"%s","to":"hive:elsewhere","id":"st","amount":4,"data":""}`, seller)
	CallNft(t, ct, "safeTransferFrom", []byte(away), nil, seller, true, gas, "")

	buy := fmt.Sprintf(`{"bucketId":%d,"mode":"single","quantity":1,"maxTotalPrice":""}`, id)
	CallMarket(t, ct, "buyFromBucket", []byte(buy), nil, buyer, "", false, gas, "")

	// The buyer was not charged for a draw that could not be delivered.
	assert.Equal(t, buyerBefore, QueryTokenBalance(t, ct, buyer), "a failed purchase costs nothing but RC")
}

// TestBucketFeeOnTransferTokenDistributesReceived: with a payment token that
// taxes every transfer, the market must distribute what it ACTUALLY received,
// not the nominal price. Buckets take payment once per purchase rather than per
// draw, so this needs proving on the pack path too — pricing a pack off the
// nominal amount would quietly overpay the seller out of pool funds.
func TestBucketFeeOnTransferTokenDistributesReceived(t *testing.T) {
	ct := SetupContractTest()
	initFeeTokenSetup(t, ct)

	seller := ownerAddress
	buyer := "hive:fotbuyer"

	MintNft(t, ct, seller, "fot", 6, 100)
	ApproveNftForMarket(t, ct, seller)
	MintFeeToken(t, ct, buyer, 100000)

	payload := fmt.Sprintf(
		`{"nftContract":"%s","entries":%s,"paymentToken":"%s","pricePerDraw":"0","pricePerPack":"1000","packDraws":[3],"expirationBlock":0}`,
		NftContractID, bucketEntriesJSON([][2]string{{"fot", "6"}}), FeeTokenID)
	res, _, _ := CallMarket(t, ct, "listBucket", []byte(payload), nil, seller, "", true, gas, "")
	id := ParseCreated(res).Id

	sellerBefore := QueryFeeTokenBalance(t, ct, seller)
	buyerBefore := QueryFeeTokenBalance(t, ct, buyer)

	buy := fmt.Sprintf(`{"bucketId":%d,"mode":"pack","quantity":1,"maxTotalPrice":""}`, id)
	_, _, logs := CallMarket(t, ct, "buyFromBucket", []byte(buy), nil, buyer, "", true, gas, "")

	// One pack price is charged, not three single prices.
	assert.Equal(t, uint64(1000), buyerBefore-QueryFeeTokenBalance(t, ct, buyer),
		"buyer debited one nominal pack price")

	// escrowIn received 1000 - floor(1000/100) = 990, and the payout hop is
	// taxed again: 990 - floor(990/100) = 981. feeBps is 0 in this setup.
	assert.Equal(t, uint64(981), QueryFeeTokenBalance(t, ct, seller)-sellerBefore,
		"seller credited the post-payout-fee amount, not the nominal 1000")

	// The purchase event records what was received, proving the balance-delta
	// is what gets distributed.
	AssertEventContains(t, logs, "bucket_purchase", `"paid":"990"`)

	// And the pack still delivered its three cards.
	assert.Equal(t, uint64(3), QueryNftBalance(t, ct, buyer, "fot"), "pack delivered regardless")
}

// TestBucketRejectsCombinedFeeAndRoyaltyOver100: fee tops out at 10000 bps and
// royalty at 5000, so the two together can exceed the sale price. Listing must
// refuse rather than create a bucket whose payout arithmetic cannot balance.
func TestBucketRejectsCombinedFeeAndRoyaltyOver100(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	seller := ownerAddress
	MintNft(t, ct, seller, "cf", 2, 100)
	ApproveNftForMarket(t, ct, seller)

	CallMarket(t, ct, "setFee", []byte(`{"feeBps":8000}`), nil, ownerAddress, "", true, gas, "")
	splits := fmt.Sprintf(`{"nftContract":"%s","splits":[{"recipient":"hive:artist","bps":3000}]}`, NftContractID)
	CallMarket(t, ct, "setRoyaltySplits", []byte(splits), nil, ownerAddress, "", true, gas, "")

	payload := fmt.Sprintf(
		`{"nftContract":"%s","entries":%s,"paymentToken":"%s","pricePerDraw":"1000","pricePerPack":"0","packDraws":[],"expirationBlock":0}`,
		NftContractID, bucketEntriesJSON([][2]string{{"cf", "2"}}), TokenID)
	CallMarket(t, ct, "listBucket", []byte(payload), nil, seller, "", false, gas,
		"Combined fee and royalty exceed 100%")
}

// TestBucketRejectsTooManyEntries: the entry cap bounds per-draw work, so it has
// to be enforced. The refusal happens before any balance check, so this needs no
// minting — which is the point: an oversized payload is rejected on sight.
func TestBucketRejectsTooManyEntries(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	entries := make([][2]string, 0, MaxEntriesPerCallContract+1)
	for i := 0; i <= MaxEntriesPerCallContract; i++ {
		entries = append(entries, [2]string{fmt.Sprintf("over%03d", i), "1"})
	}
	payload := fmt.Sprintf(
		`{"nftContract":"%s","entries":%s,"paymentToken":"%s","pricePerDraw":"1000","pricePerPack":"0","packDraws":[],"expirationBlock":0}`,
		NftContractID, bucketEntriesJSON(entries), TokenID)
	CallMarket(t, ct, "listBucket", []byte(payload), nil, ownerAddress, "", false, gas,
		"Too many bucket entries in one call")
}

// ===================================
// addToBucket — stocking a bucket across several transactions
// ===================================

// stockBucket lists a bucket and then restocks it until it holds `total`
// entries, which is the only way to build a large one: no single transaction
// can afford to write hundreds of entries.
func stockBucket(t *testing.T, ct *test_utils.ContractTest, seller string, ids []string, perCall int, packDraws string) uint64 {
	first := perCall
	if first > len(ids) {
		first = len(ids)
	}
	entries := make([][2]string, 0, first)
	for _, id := range ids[:first] {
		entries = append(entries, [2]string{id, "1"})
	}
	payload := fmt.Sprintf(
		`{"nftContract":"%s","entries":%s,"paymentToken":"%s","pricePerDraw":"1000","pricePerPack":"9000","packDraws":%s,"expirationBlock":0}`,
		NftContractID, bucketEntriesJSON(entries), TokenID, packDraws)
	res, _, _ := CallMarket(t, ct, "listBucket", []byte(payload), nil, seller, "", true, bigGas, "")
	id := ParseCreated(res).Id

	for off := first; off < len(ids); off += perCall {
		end := off + perCall
		if end > len(ids) {
			end = len(ids)
		}
		batch := make([][2]string, 0, end-off)
		for _, tid := range ids[off:end] {
			batch = append(batch, [2]string{tid, "1"})
		}
		add := fmt.Sprintf(`{"bucketId":%d,"entries":%s}`, id, bucketEntriesJSON(batch))
		CallMarket(t, ct, "addToBucket", []byte(add), nil, seller, "", true, bigGas, "")
	}
	return id
}

func TestBucketAddToBucketStocksMore(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	seller := ownerAddress
	buyer := "hive:restockbuyer"

	MintNftBatch(t, ct, seller, []string{"r1", "r2", "r3", "r4"}, 1, 1)
	ApproveNftForMarket(t, ct, seller)
	MintAndApproveToken(t, ct, buyer, 100000)

	entries := [][2]string{{"r1", "1"}, {"r2", "1"}}
	payload := fmt.Sprintf(
		`{"nftContract":"%s","entries":%s,"paymentToken":"%s","pricePerDraw":"1000","pricePerPack":"0","packDraws":[],"expirationBlock":0}`,
		NftContractID, bucketEntriesJSON(entries), TokenID)
	res, _, _ := CallMarket(t, ct, "listBucket", []byte(payload), nil, seller, "", true, gas, "")
	id := ParseCreated(res).Id

	add := fmt.Sprintf(`{"bucketId":%d,"entries":%s}`, id,
		bucketEntriesJSON([][2]string{{"r3", "1"}, {"r4", "1"}}))
	_, _, logs := CallMarket(t, ct, "addToBucket", []byte(add), nil, seller, "", true, gas, "")
	AssertEventEmitted(t, logs, "bucket_restocked")
	AssertEventContains(t, logs, "bucket_restocked", `"totalEntries":4`)

	// All four units must be drawable: the restocked entries are not second
	// class, and the draw must see the whole bucket, not just the first batch.
	buy := fmt.Sprintf(`{"bucketId":%d,"mode":"single","quantity":1,"maxTotalPrice":""}`, id)
	for i := 0; i < 4; i++ {
		fmt.Printf(">>>RC buy%d buyer=%d seller=%d\n", i+2, ct.GetAvailableRCs(buyer), ct.GetAvailableRCs(seller))
		CallMarket(t, ct, "buyFromBucket", []byte(buy), nil, buyer, "", true, gas, "")
	}
	for _, tid := range []string{"r1", "r2", "r3", "r4"} {
		assert.Equal(t, uint64(1), QueryNftBalance(t, ct, buyer, tid))
	}
	// Drained, so the bucket deactivates — the restock did not corrupt the count.
	CallMarket(t, ct, "buyFromBucket", []byte(buy), nil, buyer, "", false, gas, "Bucket not active")
}

func TestBucketAddToBucketValidation(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	seller := ownerAddress
	other := "hive:notseller"

	MintNftBatch(t, ct, seller, []string{"v1", "v2", "v3"}, 1, 1)
	ApproveNftForMarket(t, ct, seller)

	entries := [][2]string{{"v1", "1"}}
	payload := fmt.Sprintf(
		`{"nftContract":"%s","entries":%s,"paymentToken":"%s","pricePerDraw":"1000","pricePerPack":"0","packDraws":[],"expirationBlock":0}`,
		NftContractID, bucketEntriesJSON(entries), TokenID)
	res, _, _ := CallMarket(t, ct, "listBucket", []byte(payload), nil, seller, "", true, gas, "")
	id := ParseCreated(res).Id

	cases := []struct {
		name    string
		caller  string
		payload string
		errMsg  string
	}{
		{"empty payload", seller, ``, "Payload required"},
		{"no entries", seller,
			fmt.Sprintf(`{"bucketId":%d,"entries":[]}`, id), "At least one entry required"},
		{"not the seller", other,
			fmt.Sprintf(`{"bucketId":%d,"entries":%s}`, id, bucketEntriesJSON([][2]string{{"v2", "1"}})),
			"Only seller can add to bucket"},
		{"unknown bucket", seller,
			fmt.Sprintf(`{"bucketId":999,"entries":%s}`, bucketEntriesJSON([][2]string{{"v2", "1"}})),
			"Bucket not active"},
		// The presence marker, not a scan: a token already in the bucket would
		// otherwise get a second entry and silently double its own odds.
		{"already stocked", seller,
			fmt.Sprintf(`{"bucketId":%d,"entries":%s}`, id, bucketEntriesJSON([][2]string{{"v1", "1"}})),
			"Token ID already stocked in this bucket"},
		{"duplicate within the batch", seller,
			fmt.Sprintf(`{"bucketId":%d,"entries":%s}`, id,
				bucketEntriesJSON([][2]string{{"v2", "1"}, {"v2", "1"}})),
			"Duplicate token ID in bucket entries"},
		{"more units than held", seller,
			fmt.Sprintf(`{"bucketId":%d,"entries":%s}`, id, bucketEntriesJSON([][2]string{{"v2", "5"}})),
			"Insufficient NFT balance"},
		{"zero amount", seller,
			fmt.Sprintf(`{"bucketId":%d,"entries":%s}`, id, bucketEntriesJSON([][2]string{{"v2", "0"}})),
			"Amount must be greater than zero"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			CallMarket(t, ct, "addToBucket", []byte(c.payload), nil, c.caller, "", false, gas, c.errMsg)
		})
	}

	// Over the per-call cap.
	big := make([][2]string, 0, MaxEntriesPerCallContract+1)
	for i := 0; i <= MaxEntriesPerCallContract; i++ {
		big = append(big, [2]string{fmt.Sprintf("big%03d", i), "1"})
	}
	CallMarket(t, ct, "addToBucket",
		[]byte(fmt.Sprintf(`{"bucketId":%d,"entries":%s}`, id, bucketEntriesJSON(big))),
		nil, seller, "", false, gas, "Too many bucket entries in one call")

	// Pausing halts restocking, exactly as it halts listing.
	CallMarket(t, ct, "pause", []byte(`{}`), nil, ownerAddress, "", true, gas, "")
	CallMarket(t, ct, "addToBucket",
		[]byte(fmt.Sprintf(`{"bucketId":%d,"entries":%s}`, id, bucketEntriesJSON([][2]string{{"v2", "1"}}))),
		nil, seller, "", false, gas, "paused")
	CallMarket(t, ct, "unpause", []byte(`{}`), nil, ownerAddress, "", true, gas, "")

	// A delisted bucket cannot be revived by restocking it.
	CallMarket(t, ct, "delistBucket", []byte(fmt.Sprintf(`{"bucketId":%d}`, id)), nil, seller, "", true, gas, "")
	CallMarket(t, ct, "addToBucket",
		[]byte(fmt.Sprintf(`{"bucketId":%d,"entries":%s}`, id, bucketEntriesJSON([][2]string{{"v2", "1"}}))),
		nil, seller, "", false, gas, "Bucket not active")
}

// drawnTokenIds pulls the token ids out of the bucket_draw events of one call.
func drawnTokenIds(logs map[string]contract_session.LogOutput) []string {
	var ids []string
	for _, e := range FindEventsInLogs(logs, "bucket_draw") {
		const k = `"tokenId":"`
		i := strings.Index(e, k)
		if i < 0 {
			continue
		}
		rest := e[i+len(k):]
		j := strings.Index(rest, `"`)
		if j < 0 {
			continue
		}
		ids = append(ids, rest[:j])
	}
	return ids
}

// TestBucketFiveHundredDistinctCards is the case the chunked layout exists for.
//
// Under the old flat layout a draw read and scanned every entry, so this bucket
// was not merely expensive but IMPOSSIBLE: a single draw over 500 entries needed
// roughly 14k RC against a 10k limit, and listing them needed 77k. Both now fit,
// and the point of the test is that they fit with the whole bucket in play —
// not just the first chunk of it.
func TestBucketFiveHundredDistinctCards(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	seller := ownerAddress
	buyer := "hive:bigbuyer"

	// The seller mints 500 NFTs and makes eight stocking calls here; without
	// funding, the 10k free tier is gone long before the bucket is built.
	FundRc(t, ct, seller, 5_000_000)
	FundRc(t, ct, buyer, 5_000_000)

	const total = 500
	ids := make([]string, 0, total)
	for i := 0; i < total; i++ {
		ids = append(ids, fmt.Sprintf("card%03d", i))
	}
	MintNftBatch(t, ct, seller, ids, 1, 1)
	ApproveNftForMarket(t, ct, seller)
	MintAndApproveToken(t, ct, buyer, 1000000)

	fmt.Printf("\n>>> BIG stocking %d distinct entries\n", total)
	id := stockBucket(t, ct, seller, ids, MaxEntriesPerCallContract, "[10]")

	fmt.Printf("\n>>> BIG single draw over %d entries\n", total)
	single := fmt.Sprintf(`{"bucketId":%d,"mode":"single","quantity":1,"maxTotalPrice":""}`, id)
	_, _, logs := CallMarket(t, ct, "buyFromBucket", []byte(single), nil, buyer, "", true, bigGas, "")
	got := drawnTokenIds(logs)
	assert.Len(t, got, 1, "a single draw delivers exactly one card")

	fmt.Printf("\n>>> BIG 10-card pack over %d entries\n", total)
	pack := fmt.Sprintf(`{"bucketId":%d,"mode":"pack","quantity":1,"maxTotalPrice":""}`, id)
	_, _, packLogs := CallMarket(t, ct, "buyFromBucket", []byte(pack), nil, buyer, "", true, bigGas, "")
	packed := drawnTokenIds(packLogs)
	assert.Len(t, packed, 10, "a [10] pack delivers ten cards")

	// The buyer holds every card drawn, and no card was handed over twice —
	// each entry has exactly one unit, so a double delivery would mean the
	// chunk sums and the slots had drifted apart.
	seen := map[string]bool{}
	for _, tid := range append(got, packed...) {
		assert.False(t, seen[tid], "card %s was drawn twice from single-unit entries", tid)
		seen[tid] = true
		assert.Equal(t, uint64(1), QueryNftBalance(t, ct, buyer, tid))
	}

	// Draws must reach past the first chunk. A chunk-walk that never advanced
	// would still pass every test above while quietly confining the entire
	// bucket's odds to its first 32 entries, so pull enough draws to make that
	// overwhelmingly unlikely to happen by chance.
	beyondFirstChunk := false
	for i := 0; i < 6; i++ {
		_, _, l := CallMarket(t, ct, "buyFromBucket", []byte(pack), nil, buyer, "", true, bigGas, "")
		for _, tid := range drawnTokenIds(l) {
			var n int
			fmt.Sscanf(tid, "card%03d", &n)
			if n >= BucketChunkContract {
				beyondFirstChunk = true
			}
		}
	}
	assert.True(t, beyondFirstChunk,
		"every draw landed in the first chunk — the chunk walk is not advancing")
}

// TestBucketRejectsOverfullBucket proves the total cap is enforced across
// calls, not just within one.
func TestBucketRejectsOverfullBucket(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	seller := ownerAddress

	FundRc(t, ct, seller, 5_000_000)

	// Fill to the cap, then try to add one more.
	ids := make([]string, 0, MaxBucketEntriesContract+1)
	for i := 0; i <= MaxBucketEntriesContract; i++ {
		ids = append(ids, fmt.Sprintf("full%03d", i))
	}
	MintNftBatch(t, ct, seller, ids, 1, 1)
	ApproveNftForMarket(t, ct, seller)

	id := stockBucket(t, ct, seller, ids[:MaxBucketEntriesContract], MaxEntriesPerCallContract, "[1]")
	over := fmt.Sprintf(`{"bucketId":%d,"entries":%s}`, id,
		bucketEntriesJSON([][2]string{{ids[MaxBucketEntriesContract], "1"}}))
	CallMarket(t, ct, "addToBucket", []byte(over), nil, seller, "", false, gas, "Bucket is full")
}

// ===================================
// Chunked-layout regressions
// ===================================
//
// Both of these exist because entries moved from one flat array into per-pool
// slot arrays with chunked unit sums. That change is invisible to every test
// written before it: a small bucket is one chunk, and a bucket that is only
// ever stocked before it is drawn from never exercises appending onto partially
// drained state. Neither path fails loudly if the bookkeeping drifts — the
// symptom is wrong odds or a unit that cannot be delivered.

// TestBucketRestockAfterDraws appends to a bucket that has ALREADY been drawn
// from.
//
// Every other addToBucket test stocks before anyone buys. Here the first two
// draws zero out two slots WITHOUT removing them, so the append has to land at
// the next free index and credit the right chunk sum on top of state that a
// purchase has already decremented. If slot indices or chunk sums drift, the
// restocked cards either never come out or come out twice.
func TestBucketRestockAfterDraws(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	seller := ownerAddress
	// Two buyers, because RC is a PER-ACCOUNT budget and six draws from one
	// account would exhaust the 10k free tier and fail for a reason that has
	// nothing to do with restocking. Two collectors is also the more realistic
	// shape: a restocked bucket is normally drawn from by whoever shows up next.
	early := "hive:restockearly"
	late := "hive:restocklate"

	first := []string{"ra", "rb", "rc"}
	second := []string{"rd", "re", "rf"}
	MintNftBatch(t, ct, seller, append(append([]string{}, first...), second...), 1, 1)
	ApproveNftForMarket(t, ct, seller)
	MintAndApproveToken(t, ct, early, 100000)
	MintAndApproveToken(t, ct, late, 100000)

	entries := make([][2]string, 0, 3)
	for _, id := range first {
		entries = append(entries, [2]string{id, "1"})
	}
	id := listBucket(t, ct, seller, bucketEntriesJSON(entries), "1000", "0", "[]")

	buy := fmt.Sprintf(`{"bucketId":%d,"mode":"single","quantity":1,"maxTotalPrice":""}`, id)
	for i := 0; i < 2; i++ {
		CallMarket(t, ct, "buyFromBucket", []byte(buy), nil, early, "", true, gas, "")
	}

	// Restock ON TOP of the two drained slots.
	more := make([][2]string, 0, 3)
	for _, tid := range second {
		more = append(more, [2]string{tid, "1"})
	}
	add := fmt.Sprintf(`{"bucketId":%d,"entries":%s}`, id, bucketEntriesJSON(more))
	CallMarket(t, ct, "addToBucket", []byte(add), nil, seller, "", true, gas, "")

	// One unit survives from the original batch plus three new ones.
	for i := 0; i < 4; i++ {
		CallMarket(t, ct, "buyFromBucket", []byte(buy), nil, late, "", true, gas, "")
	}

	// Every card reaches a buyer exactly once, and the seller keeps none. A
	// drifted slot index would deliver one twice and strand another.
	for _, tid := range append(append([]string{}, first...), second...) {
		held := QueryNftBalance(t, ct, early, tid) + QueryNftBalance(t, ct, late, tid)
		assert.Equal(t, uint64(1), held, "exactly one %s should have been delivered", tid)
		assert.Equal(t, uint64(0), QueryNftBalance(t, ct, seller, tid),
			"seller should have handed over %s", tid)
	}

	// Drained, so the bucket closes — the restock did not corrupt the count.
	CallMarket(t, ct, "buyFromBucket", []byte(buy), nil, late, "", false, gas, "Bucket not active")
}

// TestBucketPrunesStaleEntryInLaterChunk puts the stale entry BEYOND the first
// chunk.
//
// Pruning decrements `chunks[pool][idx/BucketChunk]`. Every stale-entry test so
// far used a handful of entries, so that division always yielded chunk 0 and the
// indexing was never actually exercised. Here the stale entry sits at index 34 —
// chunk 1 — and carries almost all the bucket's weight, so the draw is
// overwhelmingly likely to select it and be forced to prune.
//
// The second round of draws is the real assertion: if pruning had decremented
// the wrong chunk's sum, the totals would still look right (they are kept
// separately) while the chunk sums silently disagreed with the slots, and later
// draws would misbehave.
func TestBucketPrunesStaleEntryInLaterChunk(t *testing.T) {
	ct := SetupContractTest()

	seller := ownerAddress
	// The seller alone spends ~27k RC here (batch mints, a 24-entry listing, a
	// 12-entry restock), far past the 10k free tier. The buyers below each stay
	// inside the free tier, so only the seller needs topping up.
	FundRc(t, ct, seller, 5_000_000)

	InitFullSetup(t, ct)

	// One buyer per pack: RC is per-account, and two packs over a 36-entry
	// bucket would exhaust the free tier.
	firstBuyer := "hive:chunkbuyer1"
	secondBuyer := "hive:chunkbuyer2"

	// 24 in the listing (the per-call cap) then 12 more, so the bucket spans
	// two chunks of 32 slots.
	firstBatch := make([]string, 0, 24)
	for i := 0; i < 24; i++ {
		firstBatch = append(firstBatch, fmt.Sprintf("ck%02d", i))
	}
	secondBatch := make([]string, 0, 11)
	for i := 24; i < 36; i++ {
		if i == 34 {
			continue // index 34 is the stale entry, minted separately
		}
		secondBatch = append(secondBatch, fmt.Sprintf("ck%02d", i))
	}
	const stale = "ck34"

	MintNftBatch(t, ct, seller, firstBatch, 1, 1)
	MintNftBatch(t, ct, seller, secondBatch, 1, 1)
	// Dominant weight, so a draw almost certainly lands on it.
	MintNft(t, ct, seller, stale, 500, 500)
	ApproveNftForMarket(t, ct, seller)
	MintAndApproveToken(t, ct, firstBuyer, 100000)
	MintAndApproveToken(t, ct, secondBuyer, 100000)

	listed := make([][2]string, 0, 24)
	for _, tid := range firstBatch {
		listed = append(listed, [2]string{tid, "1"})
	}
	// bigGas for the bulk calls: maxGas here is an assertion AFTER the call, not
	// an execution limit, and a 24-entry listing legitimately exceeds the
	// default one.
	listPayload := fmt.Sprintf(
		`{"nftContract":"%s","entries":%s,"paymentToken":"%s","pricePerDraw":"0","pricePerPack":"5000","packDraws":[5],"expirationBlock":0}`,
		NftContractID, bucketEntriesJSON(listed), TokenID)
	listRes, _, _ := CallMarket(t, ct, "listBucket", []byte(listPayload), nil, seller, "", true, bigGas, "")
	id := ParseCreated(listRes).Id

	// Append so the stale entry lands at slot 34 — chunk 1, not chunk 0.
	rest := make([][2]string, 0, 12)
	for i := 24; i < 36; i++ {
		if i == 34 {
			rest = append(rest, [2]string{stale, "500"})
			continue
		}
		rest = append(rest, [2]string{fmt.Sprintf("ck%02d", i), "1"})
	}
	add := fmt.Sprintf(`{"bucketId":%d,"entries":%s}`, id, bucketEntriesJSON(rest))
	CallMarket(t, ct, "addToBucket", []byte(add), nil, seller, "", true, bigGas, "")

	// The seller moves the whole dominant entry away AFTER listing.
	away := fmt.Sprintf(`{"from":"%s","to":"hive:elsewhere","id":"%s","amount":500,"data":""}`, seller, stale)
	CallNft(t, ct, "safeTransferFrom", []byte(away), nil, seller, true, gas, "")
	assert.Equal(t, uint64(0), QueryNftBalance(t, ct, seller, stale), "seller no longer holds it")

	pack := fmt.Sprintf(`{"bucketId":%d,"mode":"pack","quantity":1,"maxTotalPrice":""}`, id)
	_, _, logs := CallMarket(t, ct, "buyFromBucket", []byte(pack), nil, firstBuyer, "", true, bigGas, "")

	// It was pruned rather than silently skipped, and never delivered.
	AssertEventEmitted(t, logs, "bucket_entry_dropped")
	assert.Equal(t, uint64(0), QueryNftBalance(t, ct, firstBuyer, stale),
		"a unit the seller no longer holds must never be delivered")

	delivered := uint64(0)
	for i := 0; i < 36; i++ {
		delivered += QueryNftBalance(t, ct, firstBuyer, fmt.Sprintf("ck%02d", i))
	}
	assert.Equal(t, uint64(5), delivered, "a [5] pack delivers five cards")

	// The chunk sums must still agree with the slots after a prune in chunk 1.
	// A wrong-chunk decrement leaves the totals plausible and the chunk sums
	// corrupt, which only shows up on the NEXT draw.
	_, _, logs2 := CallMarket(t, ct, "buyFromBucket", []byte(pack), nil, secondBuyer, "", true, bigGas, "")
	assert.Empty(t, FindEventsInLogs(logs2, "bucket_entry_dropped"),
		"nothing else was stale — a second drop means the prune corrupted the table")

	delivered2 := uint64(0)
	for i := 0; i < 36; i++ {
		delivered2 += QueryNftBalance(t, ct, secondBuyer, fmt.Sprintf("ck%02d", i))
	}
	assert.Equal(t, uint64(5), delivered2, "the bucket keeps drawing correctly after the prune")
	assert.Equal(t, uint64(0), QueryNftBalance(t, ct, secondBuyer, stale), "still never delivered")
}

// ===================================
// Bounded pruning, and a collection that misbehaves
// ===================================

// TestBucketMaxStaleRetriesBoundsPruning proves the draw gives up rather than
// pruning an unbounded number of stale entries.
//
// The seller keeps custody, so entries CAN go stale, and pruning them is right.
// But each prune costs a cross-contract call, so a bucket whose whole stock has
// moved on would otherwise burn the buyer's entire budget discovering that. The
// bound is what stops one buyer paying to clean up after the seller.
//
// The abort MESSAGE is what pins the bound. With 20 stale entries and a cap of
// 12, the draw prunes 13 and gives up while units still remain — "No deliverable
// entries left in bucket". Without the cap it would prune all 20, drive the pool
// to zero, and abort with "Bucket pool is sold out" instead. So the message
// distinguishes bounded from unbounded pruning, deterministically.
func TestBucketMaxStaleRetriesBoundsPruning(t *testing.T) {
	ct := SetupContractTest()

	seller := ownerAddress
	// The seller mints 20 ids, lists them and then moves every one away.
	FundRc(t, ct, seller, 5_000_000)

	InitFullSetup(t, ct)

	buyer := "hive:staleretrybuyer"
	ids := make([]string, 0, 20)
	entries := make([][2]string, 0, 20)
	for i := 0; i < 20; i++ {
		id := fmt.Sprintf("sr%02d", i)
		ids = append(ids, id)
		entries = append(entries, [2]string{id, "1"})
	}
	MintNftBatch(t, ct, seller, ids, 1, 1)
	ApproveNftForMarket(t, ct, seller)
	MintAndApproveToken(t, ct, buyer, 100000)

	payload := fmt.Sprintf(
		`{"nftContract":"%s","entries":%s,"paymentToken":"%s","pricePerDraw":"1000","pricePerPack":"0","packDraws":[],"expirationBlock":0}`,
		NftContractID, bucketEntriesJSON(entries), TokenID)
	res, _, _ := CallMarket(t, ct, "listBucket", []byte(payload), nil, seller, "", true, bigGas, "")
	id := ParseCreated(res).Id

	// Every single unit moves on AFTER listing.
	for _, tid := range ids {
		away := fmt.Sprintf(`{"from":"%s","to":"hive:elsewhere","id":"%s","amount":1,"data":""}`, seller, tid)
		CallNft(t, ct, "safeTransferFrom", []byte(away), nil, seller, true, gas, "")
	}

	buyerBefore := QueryTokenBalance(t, ct, buyer)
	buy := fmt.Sprintf(`{"bucketId":%d,"mode":"single","quantity":1,"maxTotalPrice":""}`, id)
	CallMarket(t, ct, "buyFromBucket", []byte(buy), nil, buyer, "", false, bigGas,
		"No deliverable entries left in bucket")

	assert.Equal(t, buyerBefore, QueryTokenBalance(t, ct, buyer),
		"a purchase that could not be delivered costs nothing but RC")
}

// TestBucketHostileCollectionFailingTransferAbortsPurchase proves a refused
// delivery takes the whole purchase down with it.
//
// The market has already taken payment and written its state by the time it
// calls the collection. If a failed transfer were swallowed, the buyer would pay
// for a card that never moved and the bucket would count it as sold. Only a
// collection that refuses can test this — the real one always cooperates.
func TestBucketHostileCollectionFailingTransferAbortsPurchase(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	seller := ownerAddress
	buyer := "hive:hostilebuyer"
	MintAndApproveToken(t, ct, buyer, 100000)

	stockHostileCollection(t, ct, seller, "hx", 5)
	id := listHostileBucket(t, ct, seller, "hx", 5)

	// Arm the refusal.
	CallHostileNft(t, ct, "setMode", []byte(`{"mode":"fail"}`), seller, true, "")

	buyerBefore := QueryTokenBalance(t, ct, buyer)
	sellerBefore := QueryTokenBalance(t, ct, seller)

	// The COLLECTION's message comes back, not the market's. A sub-contract
	// that aborts takes the whole transaction down where it stands, so the
	// market's own "safeBatchTransferFrom call failed" guard is never reached —
	// that branch only fires if a callee returns nothing WITHOUT aborting.
	// Either way the property that matters holds: the purchase does not
	// half-complete.
	buy := fmt.Sprintf(`{"bucketId":%d,"mode":"single","quantity":1,"maxTotalPrice":""}`, id)
	CallMarket(t, ct, "buyFromBucket", []byte(buy), nil, buyer, "", false, gas,
		"hostile collection refuses to transfer")

	// Nothing moved: not the buyer's money, not the seller's proceeds.
	assert.Equal(t, buyerBefore, QueryTokenBalance(t, ct, buyer),
		"buyer must not pay for a card the collection refused to move")
	assert.Equal(t, sellerBefore, QueryTokenBalance(t, ct, seller),
		"seller must not be paid for a delivery that failed")
}

// TestBucketWritesStateBeforeDelivering proves the CEI ordering the design
// claims: state is flushed BEFORE the external call, so a collection that
// re-enters cannot see units it has already been promised.
//
// The hostile collection reads the market's own bucket counter from inside
// safeBatchTransferFrom. If the market flushed first, the value visible
// mid-delivery is already the POST-purchase count. If it flushed afterwards, the
// pre-purchase count would leak — and that gap is exactly what a re-entrant
// collection would spend.
func TestBucketWritesStateBeforeDelivering(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	seller := ownerAddress
	buyer := "hive:ceibuyer"
	MintAndApproveToken(t, ct, buyer, 100000)

	stockHostileCollection(t, ct, seller, "cei", 5)
	id := listHostileBucket(t, ct, seller, "cei", 5)

	// Point the spy at this bucket.
	CallHostileNft(t, ct, "setMode",
		[]byte(fmt.Sprintf(`{"mode":"spy","market":"%s","bucketId":%d}`, MarketContractID, id)),
		seller, true, "")

	buy := fmt.Sprintf(`{"bucketId":%d,"mode":"single","quantity":1,"maxTotalPrice":""}`, id)
	CallMarket(t, ct, "buyFromBucket", []byte(buy), nil, buyer, "", true, gas, "")

	// 5 units, one drawn: a collection called mid-purchase must already see 4.
	// Seeing 5 would mean the market had promised a unit it had not yet booked.
	seen := ct.StateGet(HostileNftID, "seen")
	assert.Equal(t, "4", seen,
		"the market must write its state BEFORE calling out; mid-delivery the bucket should already read 4, not 5")
}

// stockHostileCollection gives the hostile mock the shape the market reads:
// an owner, a balance, and blanket approval.
func stockHostileCollection(t *testing.T, ct *test_utils.ContractTest, seller, tokenId string, amount uint64) {
	t.Helper()
	CallHostileNft(t, ct, "setOwner",
		[]byte(fmt.Sprintf(`{"owner":"%s"}`, seller)), seller, true, "")
	CallHostileNft(t, ct, "setBalance",
		[]byte(fmt.Sprintf(`{"account":"%s","id":"%s","amount":%d}`, seller, tokenId, amount)), seller, true, "")
	CallHostileNft(t, ct, "setOperator",
		[]byte(fmt.Sprintf(`{"owner":"%s","operator":"%s"}`, seller, MarketContractAddress)), seller, true, "")
}

func listHostileBucket(t *testing.T, ct *test_utils.ContractTest, seller, tokenId string, amount uint64) uint64 {
	t.Helper()
	payload := fmt.Sprintf(
		`{"nftContract":"%s","entries":[{"tokenId":"%s","amount":%d,"pool":0}],"paymentToken":"%s","pricePerDraw":"1000","pricePerPack":"0","packDraws":[],"expirationBlock":0}`,
		HostileNftID, tokenId, amount, TokenID)
	res, _, _ := CallMarket(t, ct, "listBucket", []byte(payload), nil, seller, "", true, gas, "")
	return ParseCreated(res).Id
}
