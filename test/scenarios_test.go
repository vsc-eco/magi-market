package contract_test

import (
	"fmt"
	"testing"

	"vsc-node/lib/test_utils"

	"github.com/stretchr/testify/assert"
)

// Recognisable bucket scenarios.
//
// The tests elsewhere pin mechanisms — chunk sums, pruning bounds, CEI. These
// pin PRODUCTS: the things a seller would actually try to build, described the
// way they would describe them. If one of these breaks, a real listing breaks,
// and the failure reads as "packs stopped guaranteeing a rare" rather than as
// an assertion about slot indices.
//
// Every one of them runs against the real magi_nft and magi_token contracts.
//
// Note on RC: it is a PER-ACCOUNT budget that accumulates across a test, so
// buyers are split up rather than reused. That is a harness constraint, not a
// product one — on chain each of these buyers is a different person anyway.

// TestScenarioPokemonBoosterBox — "every pack has a rare".
//
// A booster box: many packs off one bucket, each promising 4 commons and 1 rare.
// The promise has to hold on EVERY pack, not just the first one — a guarantee
// that lapses after the first sale is worse than no guarantee, because buyers
// have already priced it in.
func TestScenarioPokemonBoosterBox(t *testing.T) {
	ct := SetupContractTest()
	seller := ownerAddress
	FundRc(t, ct, seller, 5_000_000)
	InitFullSetup(t, ct)

	// One common design and one rare design, stocked deep enough for six packs.
	MintNft(t, ct, seller, "boostercommon", 24, 24)
	MintNft(t, ct, seller, "boosterrare", 6, 6)
	ApproveNftForMarket(t, ct, seller)

	entries := bucketStackEntriesJSON([][3]string{
		{"boostercommon", "24", "0"},
		{"boosterrare", "6", "1"},
	})
	payload := fmt.Sprintf(
		`{"nftContract":"%s","entries":%s,"paymentToken":"%s","pricePerDraw":"0","pricePerPack":"5000","packDraws":[4,1],"expirationBlock":0}`,
		NftContractID, entries, TokenID)
	res, _, _ := CallMarket(t, ct, "listBucket", []byte(payload), nil, seller, "", true, gas, "")
	rcLog(t, "pokemon/seller: list bucket (2 entries, 2 stacks)", res)
	id := ParseCreated(res).Id

	// Six packs across three collectors, two each.
	pack := fmt.Sprintf(`{"bucketId":%d,"mode":"pack","quantity":1,"maxTotalPrice":""}`, id)
	for _, collector := range []string{"hive:ash", "hive:misty", "hive:brock"} {
		MintAndApproveToken(t, ct, collector, 100000)
		for i := 0; i < 2; i++ {
			buyRes, _, _ := CallMarket(t, ct, "buyFromBucket", []byte(pack), nil, collector, "", true, gas, "")
			if i == 0 {
				rcLog(t, "pokemon/buyer:  open a 5-card pack [4,1]", buyRes)
			}
		}
		// Two packs => exactly two rares and eight commons. Not "at least one
		// rare somewhere": the guarantee is per pack.
		assert.Equal(t, uint64(2), QueryNftBalance(t, ct, collector, "boosterrare"),
			"%s opened two packs and must hold exactly two rares", collector)
		assert.Equal(t, uint64(8), QueryNftBalance(t, ct, collector, "boostercommon"),
			"%s opened two packs and must hold exactly eight commons", collector)
	}

	// The box is empty and closes itself.
	assert.Equal(t, uint64(0), QueryNftBalance(t, ct, seller, "boosterrare"), "all rares went out")
	assert.Equal(t, uint64(0), QueryNftBalance(t, ct, seller, "boostercommon"), "all commons went out")
	CallMarket(t, ct, "buyFromBucket", []byte(pack), nil, "hive:ash", "", false, gas, "Bucket not active")
}

// TestScenarioRaffleSingleGrandPrize — "one car, a thousand tickets".
//
// A raffle is a bucket with one jackpot among many blanks, sold as a strip of
// tickets. What matters is that the jackpot is genuinely finite: it can be won,
// but never twice, however many tickets are drawn.
func TestScenarioRaffleSingleGrandPrize(t *testing.T) {
	ct := SetupContractTest()
	seller := ownerAddress
	FundRc(t, ct, seller, 5_000_000)
	InitFullSetup(t, ct)

	MintNft(t, ct, seller, "grandprize", 1, 1)
	MintNft(t, ct, seller, "consolation", 40, 40)
	ApproveNftForMarket(t, ct, seller)

	entries := bucketStackEntriesJSON([][3]string{
		{"grandprize", "1", "0"},
		{"consolation", "40", "0"},
	})
	// A strip of ten tickets, drawn in one go.
	payload := fmt.Sprintf(
		`{"nftContract":"%s","entries":%s,"paymentToken":"%s","pricePerDraw":"100","pricePerPack":"900","packDraws":[10],"expirationBlock":0}`,
		NftContractID, entries, TokenID)
	res, _, _ := CallMarket(t, ct, "listBucket", []byte(payload), nil, seller, "", true, gas, "")
	rcLog(t, "raffle/seller:  list bucket (2 entries, 41 units)", res)
	id := ParseCreated(res).Id

	strip := fmt.Sprintf(`{"bucketId":%d,"mode":"pack","quantity":1,"maxTotalPrice":""}`, id)
	players := []string{"hive:player1", "hive:player2", "hive:player3"}
	jackpots := uint64(0)
	for _, p := range players {
		MintAndApproveToken(t, ct, p, 100000)
		stripRes, _, _ := CallMarket(t, ct, "buyFromBucket", []byte(strip), nil, p, "", true, gas, "")
		rcLog(t, "raffle/buyer:   draw a 10-ticket strip", stripRes)

		won := QueryNftBalance(t, ct, p, "grandprize")
		consolations := QueryNftBalance(t, ct, p, "consolation")
		jackpots += won
		// Ten tickets, ten prizes — nobody gets a short strip.
		assert.Equal(t, uint64(10), won+consolations,
			"%s bought a ten-ticket strip and must receive ten items", p)
		assert.LessOrEqual(t, won, uint64(1), "there is only one grand prize to win")
	}

	// Thirty tickets drawn from a stack holding exactly one jackpot.
	assert.LessOrEqual(t, jackpots, uint64(1), "the grand prize cannot be won twice")
	assert.Equal(t, 1-int(jackpots), int(QueryNftBalance(t, ct, seller, "grandprize")),
		"the jackpot is either won exactly once or still with the house")
}

// TestScenarioPlayingCardDeckDealtWithoutReplacement — "deal the whole deck".
//
// Fifty-two distinct cards, every one of them a 1-of-1. Dealing the bucket dry
// must produce each card EXACTLY once: no card dealt twice, none stranded. That
// is the property a deck has and a random pile does not, and it is the strongest
// statement about the draw's bookkeeping this suite can make.
func TestScenarioPlayingCardDeckDealtWithoutReplacement(t *testing.T) {
	ct := SetupContractTest()
	seller := ownerAddress
	FundRc(t, ct, seller, 5_000_000)
	InitFullSetup(t, ct)

	suits := []string{"S", "H", "D", "C"}
	ranks := []string{"A", "2", "3", "4", "5", "6", "7", "8", "9", "10", "J", "Q", "K"}
	deck := make([]string, 0, 52)
	for _, s := range suits {
		for _, r := range ranks {
			deck = append(deck, s+r)
		}
	}
	MintNftBatch(t, ct, seller, deck, 1, 1)
	ApproveNftForMarket(t, ct, seller)

	// 52 entries needs three stocking calls at the 24-per-call cap.
	id := stockBucket(t, ct, seller, deck, MaxEntriesPerCallContract, "[13]")

	// Four players, thirteen cards each — the whole deck.
	hand := fmt.Sprintf(`{"bucketId":%d,"mode":"pack","quantity":1,"maxTotalPrice":""}`, id)
	players := []string{"hive:north", "hive:east", "hive:south", "hive:west"}
	for _, p := range players {
		MintAndApproveToken(t, ct, p, 100000)
		handRes, _, logs := CallMarket(t, ct, "buyFromBucket", []byte(hand), nil, p, "", true, bigGas, "")
		rcLog(t, "deck/buyer:     deal 13 cards from 52 entries", handRes)
		assert.Len(t, FindEventsInLogs(logs, "bucket_draw"), 13, "%s should be dealt thirteen cards", p)
	}

	// Every card in the deck reached exactly one player, and the house kept none.
	//
	// Read from state rather than via balanceOf: 260 assertions here, and every
	// balanceOf is a contract call billed to one shared account, which would
	// exhaust its RC and fail the test on its own assertions.
	for _, card := range deck {
		total := uint64(0)
		for _, p := range players {
			total += NftBalanceState(ct, p, card)
		}
		assert.Equal(t, uint64(1), total, "%s must be dealt exactly once", card)
		assert.Equal(t, uint64(0), NftBalanceState(ct, seller, card), "%s should have left the house", card)
	}

	// The deck is spent.
	CallMarket(t, ct, "buyFromBucket", []byte(hand), nil, "hive:north", "", false, bigGas, "Bucket not active")
}

// TestScenarioFourTierMysteryPack — "commons, uncommons, holo, and one secret".
//
// A four-tier pack, which is where slot guarantees earn their keep: each tier is
// its own stack and each slot draws from exactly one of them, so the shape of a
// pack is fixed even though its contents are not. Nothing else in the suite uses
// more than two stacks.
func TestScenarioFourTierMysteryPack(t *testing.T) {
	ct := SetupContractTest()
	seller := ownerAddress
	FundRc(t, ct, seller, 5_000_000)
	InitFullSetup(t, ct)

	MintNft(t, ct, seller, "tiercommon", 20, 20)
	MintNft(t, ct, seller, "tieruncommon", 12, 12)
	MintNft(t, ct, seller, "tierholo", 4, 4)
	MintNft(t, ct, seller, "tiersecret", 2, 2)
	ApproveNftForMarket(t, ct, seller)

	entries := bucketStackEntriesJSON([][3]string{
		{"tiercommon", "20", "0"},
		{"tieruncommon", "12", "1"},
		{"tierholo", "4", "2"},
		{"tiersecret", "2", "3"},
	})
	// Five commons, three uncommons, one holo, one secret.
	payload := fmt.Sprintf(
		`{"nftContract":"%s","entries":%s,"paymentToken":"%s","pricePerDraw":"0","pricePerPack":"7000","packDraws":[5,3,1,1],"expirationBlock":0}`,
		NftContractID, entries, TokenID)
	res, _, _ := CallMarket(t, ct, "listBucket", []byte(payload), nil, seller, "", true, gas, "")
	rcLog(t, "4tier/seller:   list bucket (4 entries, 4 stacks)", res)
	id := ParseCreated(res).Id

	pack := fmt.Sprintf(`{"bucketId":%d,"mode":"pack","quantity":1,"maxTotalPrice":""}`, id)
	for _, buyer := range []string{"hive:tiera", "hive:tierb"} {
		MintAndApproveToken(t, ct, buyer, 100000)
		tierRes, _, _ := CallMarket(t, ct, "buyFromBucket", []byte(pack), nil, buyer, "", true, gas, "")
		rcLog(t, "4tier/buyer:    open a 10-card pack [5,3,1,1]", tierRes)

		// The pack's SHAPE is guaranteed even though its contents are drawn.
		assert.Equal(t, uint64(5), QueryNftBalance(t, ct, buyer, "tiercommon"), "five common slots")
		assert.Equal(t, uint64(3), QueryNftBalance(t, ct, buyer, "tieruncommon"), "three uncommon slots")
		assert.Equal(t, uint64(1), QueryNftBalance(t, ct, buyer, "tierholo"), "one holo slot")
		assert.Equal(t, uint64(1), QueryNftBalance(t, ct, buyer, "tiersecret"), "one secret slot")
	}

	// Two packs consumed exactly two of each guaranteed tier.
	assert.Equal(t, uint64(2), QueryNftBalance(t, ct, seller, "tierholo"), "holo stock drawn down by two")
	assert.Equal(t, uint64(0), QueryNftBalance(t, ct, seller, "tiersecret"), "both secrets went out")
}

// rcLog records what one operation actually cost, so docs/rc.md can be
// regenerated from a test run instead of hand-maintained and drifting.
//
//	go test ./test/ -run TestScenario -v | grep RCCOST
func rcLog(t *testing.T, label string, res test_utils.ContractTestCallResult) {
	t.Helper()
	t.Logf("RCCOST %-44s %6d", label, res.RcUsed)
}

// TestScenarioGachaponCapsuleMachine — "keep pulling until you get the chase".
//
// The opposite of a booster pack. One stack, no slot guarantees, no packs: you
// pay per pull and the odds come from how many units of each design are in the
// machine. A 1-of-1 chase figure among 121 capsules is simply 1-in-121 on every
// pull, and nothing promises you will ever see it.
//
// This is the shape to reach for when you want odds rather than guarantees —
// and the cheapest bucket to run, because one stack needs no pack layout at all.
func TestScenarioGachaponCapsuleMachine(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	seller := ownerAddress
	collector := "hive:gacha"
	FundRc(t, ct, collector, 2_000_000) // eight separate pulls, not one pack

	MintNft(t, ct, seller, "capsulecommon", 100, 100)
	MintNft(t, ct, seller, "capsulerare", 20, 20)
	MintNft(t, ct, seller, "capsulechase", 1, 1)
	ApproveNftForMarket(t, ct, seller)
	MintAndApproveToken(t, ct, collector, 100000)

	// Everything in ONE stack: weight comes from unit counts, not slots.
	entries := bucketStackEntriesJSON([][3]string{
		{"capsulecommon", "100", "0"},
		{"capsulerare", "20", "0"},
		{"capsulechase", "1", "0"},
	})
	payload := fmt.Sprintf(
		`{"nftContract":"%s","entries":%s,"paymentToken":"%s","pricePerDraw":"300","pricePerPack":"0","packDraws":[],"expirationBlock":0}`,
		NftContractID, entries, TokenID)
	res, _, _ := CallMarket(t, ct, "listBucket", []byte(payload), nil, seller, "", true, gas, "")
	rcLog(t, "gachapon/seller: list bucket (3 entries, 1 stack)", res)
	id := ParseCreated(res).Id

	pull := fmt.Sprintf(`{"bucketId":%d,"mode":"single","quantity":1,"maxTotalPrice":""}`, id)
	for i := 0; i < 8; i++ {
		pullRes, _, _ := CallMarket(t, ct, "buyFromBucket", []byte(pull), nil, collector, "", true, gas, "")
		if i == 0 {
			rcLog(t, "gachapon/buyer:  one pull", pullRes)
		}
	}

	// Eight pulls, eight capsules — whatever they turned out to be.
	got := QueryNftBalance(t, ct, collector, "capsulecommon") +
		QueryNftBalance(t, ct, collector, "capsulerare") +
		QueryNftBalance(t, ct, collector, "capsulechase")
	assert.Equal(t, uint64(8), got, "eight pulls must yield eight capsules")

	// The chase is a 1-of-1: winnable, but never twice, and never duplicated.
	assert.LessOrEqual(t, QueryNftBalance(t, ct, collector, "capsulechase"), uint64(1),
		"there is exactly one chase figure in the machine")
}

// TestScenarioArtPrintDropWithRoyalty — "one print, or the whole portfolio".
//
// A gallery drop where both sale modes are live at once: a single print, or a
// five-print portfolio at a discount. The artist takes a cut of every sale
// through royalty splits, so this is also the scenario that shows the money
// actually dividing four ways — artist, gallery, marketplace fee, seller.
func TestScenarioArtPrintDropWithRoyalty(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	seller := ownerAddress
	buyer := "hive:artfan"
	// The seller mints three plates, sets royalty splits and lists; the buyer
	// makes two purchases. Both run past the free tier.
	FundRc(t, ct, seller, 2_000_000)
	FundRc(t, ct, buyer, 2_000_000)

	// 5% to the artist, 2.5% to the gallery, on top of the 2.5% market fee.
	splits := fmt.Sprintf(
		`{"nftContract":"%s","splits":[{"recipient":"hive:artist","bps":500},{"recipient":"hive:gallery","bps":250}]}`,
		NftContractID)
	CallMarket(t, ct, "setRoyaltySplits", []byte(splits), nil, ownerAddress, "", true, gas, "")

	// Three plates, ten prints each.
	for _, plate := range []string{"printdawn", "printdusk", "printnoon"} {
		MintNft(t, ct, seller, plate, 10, 10)
	}
	ApproveNftForMarket(t, ct, seller)
	MintAndApproveToken(t, ct, buyer, 500000)

	entries := bucketStackEntriesJSON([][3]string{
		{"printdawn", "10", "0"},
		{"printdusk", "10", "0"},
		{"printnoon", "10", "0"},
	})
	// One print 5000; a portfolio of five 20000 — cheaper than five singles.
	payload := fmt.Sprintf(
		`{"nftContract":"%s","entries":%s,"paymentToken":"%s","pricePerDraw":"5000","pricePerPack":"20000","packDraws":[5],"expirationBlock":0}`,
		NftContractID, entries, TokenID)
	res, _, _ := CallMarket(t, ct, "listBucket", []byte(payload), nil, seller, "", true, gas, "")
	rcLog(t, "artdrop/seller: list bucket (3 entries, both modes)", res)
	id := ParseCreated(res).Id

	artistBefore := QueryTokenBalance(t, ct, "hive:artist")
	galleryBefore := QueryTokenBalance(t, ct, "hive:gallery")
	feeBefore := QueryTokenBalance(t, ct, feeRecipientAddress)
	sellerBefore := QueryTokenBalance(t, ct, seller)

	// A single print first.
	single := fmt.Sprintf(`{"bucketId":%d,"mode":"single","quantity":1,"maxTotalPrice":""}`, id)
	singleRes, _, _ := CallMarket(t, ct, "buyFromBucket", []byte(single), nil, buyer, "", true, gas, "")
	rcLog(t, "artdrop/buyer:  buy one print", singleRes)

	// 5000: 125 fee, 250 artist, 125 gallery, 4500 to the seller.
	assert.Equal(t, feeBefore+125, QueryTokenBalance(t, ct, feeRecipientAddress), "market fee on a single print")
	assert.Equal(t, artistBefore+250, QueryTokenBalance(t, ct, "hive:artist"), "artist royalty on a single print")
	assert.Equal(t, galleryBefore+125, QueryTokenBalance(t, ct, "hive:gallery"), "gallery royalty on a single print")
	assert.Equal(t, sellerBefore+4500, QueryTokenBalance(t, ct, seller), "seller nets the rest")

	// Then the portfolio: five prints for the price of four.
	portfolio := fmt.Sprintf(`{"bucketId":%d,"mode":"pack","quantity":1,"maxTotalPrice":""}`, id)
	portRes, _, _ := CallMarket(t, ct, "buyFromBucket", []byte(portfolio), nil, buyer, "", true, gas, "")
	rcLog(t, "artdrop/buyer:  buy a 5-print portfolio", portRes)

	held := QueryNftBalance(t, ct, buyer, "printdawn") +
		QueryNftBalance(t, ct, buyer, "printdusk") +
		QueryNftBalance(t, ct, buyer, "printnoon")
	assert.Equal(t, uint64(6), held, "one single print plus a portfolio of five")

	// 20000 portfolio: 500 fee, 1000 artist, 500 gallery — the split scales
	// with the sale, and a portfolio pays it ONCE rather than per print.
	assert.Equal(t, artistBefore+250+1000, QueryTokenBalance(t, ct, "hive:artist"), "artist royalty on the portfolio")
	assert.Equal(t, galleryBefore+125+500, QueryTokenBalance(t, ct, "hive:gallery"), "gallery royalty on the portfolio")
}

// TestScenarioFlashDropWithDeadline — "24 hours only".
//
// A drop that closes on a deadline rather than when it sells out. Unsold stock
// simply stays with the seller: expiry stops the sale, it does not burn or
// forfeit anything, which is what makes a timed drop safe to run.
func TestScenarioFlashDropWithDeadline(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	seller := ownerAddress
	early := "hive:earlybird"
	late := "hive:latecomer"

	MintNft(t, ct, seller, "flashtee", 12, 12)
	ApproveNftForMarket(t, ct, seller)
	MintAndApproveToken(t, ct, early, 100000)
	MintAndApproveToken(t, ct, late, 100000)

	// The drop opens at block 100 and closes at 150.
	ct.BlockHeight = 100
	payload := fmt.Sprintf(
		`{"nftContract":"%s","entries":%s,"paymentToken":"%s","pricePerDraw":"1000","pricePerPack":"0","packDraws":[],"expirationBlock":150}`,
		NftContractID, bucketEntriesJSON([][2]string{{"flashtee", "12"}}), TokenID)
	res, _, _ := CallMarket(t, ct, "listBucket", []byte(payload), nil, seller, "", true, gas, "")
	rcLog(t, "flash/seller:   list bucket (1 entry, expiring)", res)
	id := ParseCreated(res).Id

	buy := fmt.Sprintf(`{"bucketId":%d,"mode":"single","quantity":1,"maxTotalPrice":""}`, id)
	for i := 0; i < 3; i++ {
		buyRes, _, _ := CallMarket(t, ct, "buyFromBucket", []byte(buy), nil, early, "", true, gas, "")
		if i == 0 {
			rcLog(t, "flash/buyer:    buy inside the window", buyRes)
		}
	}
	assert.Equal(t, uint64(3), QueryNftBalance(t, ct, early, "flashtee"), "the early bird got three")

	// The window closes.
	ct.BlockHeight = 200
	CallMarket(t, ct, "buyFromBucket", []byte(buy), nil, late, "", false, gas, "Bucket has expired")
	assert.Equal(t, uint64(0), QueryNftBalance(t, ct, late, "flashtee"), "too late is too late")

	// Nine unsold shirts are still the seller's — expiry closes the sale, it
	// does not confiscate stock.
	assert.Equal(t, uint64(9), QueryNftBalance(t, ct, seller, "flashtee"), "unsold stock stays home")
}

// TestScenarioLootCrateBoughtInBulkAndRestocked — "buy three, and the shop
// tops up".
//
// Two things no other scenario shows: buying SEVERAL packs in one transaction,
// and a seller restocking a drop that is already live. Together they are the
// shape of a running shop rather than a one-off drop — stock goes out in
// batches and comes back in between them.
func TestScenarioLootCrateBoughtInBulkAndRestocked(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	seller := ownerAddress
	whale := "hive:whale"
	regular := "hive:regular"
	// The seller mints twice, lists, then mints again and restocks mid-sale;
	// the whale takes twelve draws in one transaction. Both exceed the free tier.
	FundRc(t, ct, seller, 2_000_000)
	FundRc(t, ct, whale, 2_000_000)

	// Enough for exactly three crates, then a restock for one more.
	MintNft(t, ct, seller, "cratecommon", 16, 16)
	MintNft(t, ct, seller, "crategold", 4, 4)
	ApproveNftForMarket(t, ct, seller)
	MintAndApproveToken(t, ct, whale, 200000)
	MintAndApproveToken(t, ct, regular, 200000)

	// A crate is 3 commons and 1 guaranteed gold.
	// 16 commons, deliberately more than the three crates need: after the bulk
	// buy the bucket still holds plenty of commons but NO gold, so the refusal
	// below is about the empty stack rather than the bucket simply being short.
	entries := bucketStackEntriesJSON([][3]string{
		{"cratecommon", "16", "0"},
		{"crategold", "3", "1"},
	})
	payload := fmt.Sprintf(
		`{"nftContract":"%s","entries":%s,"paymentToken":"%s","pricePerDraw":"0","pricePerPack":"4000","packDraws":[3,1],"expirationBlock":0}`,
		NftContractID, entries, TokenID)
	res, _, _ := CallMarket(t, ct, "listBucket", []byte(payload), nil, seller, "", true, gas, "")
	rcLog(t, "lootcrate/seller: list bucket (2 entries, 2 stacks)", res)
	id := ParseCreated(res).Id

	// THREE crates in one transaction — twelve draws.
	bulk := fmt.Sprintf(`{"bucketId":%d,"mode":"pack","quantity":3,"maxTotalPrice":""}`, id)
	bulkRes, _, _ := CallMarket(t, ct, "buyFromBucket", []byte(bulk), nil, whale, "", true, gas, "")
	rcLog(t, "lootcrate/buyer:  buy 3 crates at once (12 draws)", bulkRes)

	assert.Equal(t, uint64(9), QueryNftBalance(t, ct, whale, "cratecommon"), "three crates: nine commons")
	assert.Equal(t, uint64(3), QueryNftBalance(t, ct, whale, "crategold"), "three crates: three guaranteed golds")

	// The shop is now out of gold, so the next crate cannot be filled.
	single := fmt.Sprintf(`{"bucketId":%d,"mode":"pack","quantity":1,"maxTotalPrice":""}`, id)
	CallMarket(t, ct, "buyFromBucket", []byte(single), nil, regular, "", false, gas,
		"Not enough units left in a required stack")

	// The seller tops up a LIVE bucket, mid-sale. Restocking is append-only —
	// a token id already in the bucket cannot be added again — so a top-up
	// brings NEW ids, which is what a real shop does anyway: next week's stock
	// is next week's cards.
	MintNft(t, ct, seller, "cratecommon2", 4, 4)
	MintNft(t, ct, seller, "crategold2", 1, 1)
	restock := fmt.Sprintf(`{"bucketId":%d,"entries":%s}`, id,
		bucketStackEntriesJSON([][3]string{{"cratecommon2", "4", "0"}, {"crategold2", "1", "1"}}))
	restockRes, _, _ := CallMarket(t, ct, "addToBucket", []byte(restock), nil, seller, "", true, gas, "")
	rcLog(t, "lootcrate/seller: restock a live bucket", restockRes)

	// The same purchase now goes through, and the guarantee still holds on the
	// restocked stock.
	regularRes, _, _ := CallMarket(t, ct, "buyFromBucket", []byte(single), nil, regular, "", true, gas, "")
	rcLog(t, "lootcrate/buyer:  buy 1 crate after restock", regularRes)

	commons := QueryNftBalance(t, ct, regular, "cratecommon") + QueryNftBalance(t, ct, regular, "cratecommon2")
	golds := QueryNftBalance(t, ct, regular, "crategold") + QueryNftBalance(t, ct, regular, "crategold2")
	assert.Equal(t, uint64(3), commons, "a crate is three commons")
	assert.Equal(t, uint64(1), golds, "and one guaranteed gold, restocked stock included")
}
