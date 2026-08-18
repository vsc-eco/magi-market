package contract_test

import (
	"fmt"
	"testing"

	"vsc-node/lib/test_utils"

	"github.com/stretchr/testify/assert"
)

// MaxBucketEntriesTest mirrors the contract constant; the harness cannot import it.
// Deliberately below the contract's MaxBucketEntries. A larger table cannot be
// exercised here yet: the seller is also the account that runs InitFullSetup
// and every mint, and accounts share one cumulative RC pool, so more entries
// exhausts the SELLER before it tests the contract. Raising this needs mintBatch
// so the setup costs one call instead of one per id.
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
	MintNft(t, ct, seller, "onlycommon", 10, 100)
	ApproveNftForMarket(t, ct, seller)

	// packDraws asks for a draw from pool 1, but nothing is stocked there.
	entries := bucketPoolEntriesJSON([][3]string{{"onlycommon", "10", "0"}})
	payload := fmt.Sprintf(
		`{"nftContract":"%s","entries":%s,"paymentToken":"%s","pricePerDraw":"0","pricePerPack":"10000","packDraws":[4,1],"expirationBlock":0}`,
		NftContractID, entries, TokenID)
	CallMarket(t, ct, "listBucket", []byte(payload), nil, seller, "", false, gas, "")
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
