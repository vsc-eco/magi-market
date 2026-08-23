package contract_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// A seller's name for the sale reaches the event the indexer reads, and is
// kept in state — the contract never acts on it, so the event is the whole
// point of storing it.
func TestBucketNameIsStoredAndEmitted(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	seller := ownerAddress

	MintNft(t, ct, seller, "70", 4, 10)
	ApproveNftForMarket(t, ct, seller)

	entries := bucketStackEntriesJSON([][3]string{{"70", "4", "0"}})
	payload := fmt.Sprintf(
		`{"name":"Base Set Booster","nftContract":"%s","entries":%s,"paymentToken":"%s","pricePerDraw":"100","pricePerPack":"0","packDraws":[],"expirationBlock":0}`,
		NftContractID, entries, TokenID)
	_, _, logs := CallMarket(t, ct, "listBucket", []byte(payload), nil, seller, "", true, gas, "")

	assert.Contains(t, fmt.Sprint(logs), `"name":"Base Set Booster"`,
		"bucket_listed must carry the seller's name")
}

// Omitting it is fine — every sale listed before this field existed had none.
func TestBucketNameIsOptional(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	seller := ownerAddress

	MintNft(t, ct, seller, "71", 4, 10)
	ApproveNftForMarket(t, ct, seller)

	id := listBucket(t, ct, seller,
		bucketStackEntriesJSON([][3]string{{"71", "4", "0"}}), "100", "0", "[]")
	assert.Equal(t, uint64(0), id)
}

// The name is echoed into an event and held in state, so it is bounded and
// must not carry control bytes that would corrupt a log line.
func TestBucketNameLimits(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	seller := ownerAddress

	MintNft(t, ct, seller, "72", 4, 10)
	ApproveNftForMarket(t, ct, seller)
	entries := bucketStackEntriesJSON([][3]string{{"72", "4", "0"}})

	long := strings.Repeat("x", 65)
	CallMarket(t, ct, "listBucket", []byte(fmt.Sprintf(
		`{"name":"%s","nftContract":"%s","entries":%s,"paymentToken":"%s","pricePerDraw":"100","pricePerPack":"0","packDraws":[],"expirationBlock":0}`,
		long, NftContractID, entries, TokenID)),
		nil, seller, "", false, gas, "Name too long")

	// A tab is a control byte; the JSON encodes it, the contract rejects it.
	CallMarket(t, ct, "listBucket", []byte(fmt.Sprintf(
		`{"name":"bad\tname","nftContract":"%s","entries":%s,"paymentToken":"%s","pricePerDraw":"100","pricePerPack":"0","packDraws":[],"expirationBlock":0}`,
		NftContractID, entries, TokenID)),
		nil, seller, "", false, gas, "Name contains control characters")

	// Exactly at the cap is fine.
	ok := strings.Repeat("y", 64)
	CallMarket(t, ct, "listBucket", []byte(fmt.Sprintf(
		`{"name":"%s","nftContract":"%s","entries":%s,"paymentToken":"%s","pricePerDraw":"100","pricePerPack":"0","packDraws":[],"expirationBlock":0}`,
		ok, NftContractID, entries, TokenID)),
		nil, seller, "", true, gas, "")
}
