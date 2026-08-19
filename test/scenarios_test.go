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

	entries := bucketPoolEntriesJSON([][3]string{
		{"boostercommon", "24", "0"},
		{"boosterrare", "6", "1"},
	})
	payload := fmt.Sprintf(
		`{"nftContract":"%s","entries":%s,"paymentToken":"%s","pricePerDraw":"0","pricePerPack":"5000","packDraws":[4,1],"expirationBlock":0}`,
		NftContractID, entries, TokenID)
	res, _, _ := CallMarket(t, ct, "listBucket", []byte(payload), nil, seller, "", true, gas, "")
	rcLog(t, "pokemon/seller: list bucket (2 entries, 2 pools)", res)
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

	entries := bucketPoolEntriesJSON([][3]string{
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

	// Thirty tickets drawn from a pool holding exactly one jackpot.
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
// its own pool and each slot draws from exactly one of them, so the shape of a
// pack is fixed even though its contents are not. Nothing else in the suite uses
// more than two pools.
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

	entries := bucketPoolEntriesJSON([][3]string{
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
	rcLog(t, "4tier/seller:   list bucket (4 entries, 4 pools)", res)
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
