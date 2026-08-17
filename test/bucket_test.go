package contract_test

import (
	"fmt"
	"testing"

	"vsc-node/lib/test_utils"

	"github.com/stretchr/testify/assert"
)

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
		s += fmt.Sprintf(`{"tokenId":"%s","amount":%s}`, e[0], e[1])
	}
	return s + "]"
}

// listBucket stocks a bucket and returns its id.
func listBucket(t *testing.T, ct *test_utils.ContractTest, seller, entries, priceDraw, pricePack string, packSize uint64) uint64 {
	t.Helper()
	payload := fmt.Sprintf(
		`{"nftContract":"%s","entries":%s,"paymentToken":"%s","pricePerDraw":"%s","pricePerPack":"%s","packSize":%d,"expirationBlock":0}`,
		NftContractID, entries, TokenID, priceDraw, pricePack, packSize)
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

	id := listBucket(t, ct, seller, bucketEntriesJSON([][2]string{{"b1", "1"}, {"b2", "1"}}), "10000", "0", 0)

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

	id := listBucket(t, ct, seller, bucketEntriesJSON([][2]string{{"rare", "1"}, {"common", "19"}}), "1000", "0", 0)

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
	id := listBucket(t, ct, seller, bucketEntriesJSON([][2]string{{"p1", "10"}}), "0", "10000", 3)

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
	packOnly := listBucket(t, ct, seller, bucketEntriesJSON([][2]string{{"d1", "5"}}), "0", "10000", 2)
	buy := fmt.Sprintf(`{"bucketId":%d,"mode":"single","quantity":1,"maxTotalPrice":""}`, packOnly)
	// single draw must be refused on a pack-only bucket
	CallMarket(t, ct, "buyFromBucket", []byte(buy), nil, buyer, "", false, gas, "")

	// Single-only bucket.
	singleOnly := listBucket(t, ct, seller, bucketEntriesJSON([][2]string{{"d1", "3"}}), "1000", "0", 0)
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

	id := listBucket(t, ct, seller, bucketEntriesJSON([][2]string{{"o1", "2"}}), "1000", "5000", 5)

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
		`{"nftContract":"%s","entries":%s,"paymentToken":"%s","pricePerDraw":"1000","pricePerPack":"0","packSize":0,"expirationBlock":0}`,
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

	id := listBucket(t, ct, seller, bucketEntriesJSON([][2]string{{"x1", "3"}}), "1000", "0", 0)

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

	id := listBucket(t, ct, seller, bucketEntriesJSON([][2]string{{"s1", "3"}}), "1000", "0", 0)
	buy := fmt.Sprintf(`{"bucketId":%d,"mode":"single","quantity":1,"maxTotalPrice":""}`, id)
	// the seller must not draw from their own bucket
	CallMarket(t, ct, "buyFromBucket", []byte(buy), nil, seller, "", false, gas, "")
}
