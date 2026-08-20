package main

// ===================================
// MAGI Market - JSON Types (tinyjson)
// ===================================

// ===================================
// Payload Types (Input)
// ===================================

type InitPayload struct {
	FeeBps       uint64 `json:"feeBps"`
	FeeRecipient string `json:"feeRecipient"`
}

type ListPayload struct {
	NftContract     string `json:"nftContract"`
	TokenId         string `json:"tokenId"`
	Amount          uint64 `json:"amount"`
	PaymentToken    string `json:"paymentToken"`
	PricePerUnit    string `json:"pricePerUnit"`
	ExpirationBlock uint64 `json:"expirationBlock"`
	StartBlock      uint64 `json:"startBlock"`
	PayoutMode      string `json:"payoutMode"`      // "" | "default" | "unmap" (F1 opt-in)
	PayoutL1Address string `json:"payoutL1Address"` // required when PayoutMode=="unmap"
	DexPool         string `json:"dexPool"`         // F2: DEX pool contract id; "" = disabled
	SettleToken     string `json:"settleToken"`     // F2: asset_out id; "" = disabled
	MinSettleOut    string `json:"minSettleOut"`    // F2: slippage floor (decimal string)
}

type DelistPayload struct {
	ListingId uint64 `json:"listingId"`
}

type BuyPayload struct {
	ListingId uint64 `json:"listingId"`
	Amount    uint64 `json:"amount"`
	// Optional slippage guard. When non-empty, the buy aborts if the
	// computed `pricePerUnit * amount` exceeds this. Mirrors the same
	// field on SweepPayload. Empty/"" disables the check for back-compat.
	MaxTotalPrice string `json:"maxTotalPrice"`
}

type UpdateListingPayload struct {
	ListingId uint64 `json:"listingId"`
	NewPrice  string `json:"newPrice"`
}

type MakeOfferPayload struct {
	NftContract     string `json:"nftContract"`
	TokenId         string `json:"tokenId"`
	Amount          uint64 `json:"amount"`
	PaymentToken    string `json:"paymentToken"`
	PricePerUnit    string `json:"pricePerUnit"`
	ExpirationBlock uint64 `json:"expirationBlock"`
}

type CancelOfferPayload struct {
	OfferId uint64 `json:"offerId"`
}

type AcceptOfferPayload struct {
	OfferId uint64 `json:"offerId"`
	Amount  uint64 `json:"amount"`
}

type AcceptCollectionOfferPayload struct {
	OfferId uint64 `json:"offerId"`
	TokenId string `json:"tokenId"`
	Amount  uint64 `json:"amount"`
}

type FeePayload struct {
	FeeBps uint64 `json:"feeBps"`
}

type FeeRecipientPayload struct {
	FeeRecipient string `json:"feeRecipient"`
}

type ChangeOwnerPayload struct {
	NewOwner string `json:"newOwner"`
}

type PendingOwnerResponse struct {
	PendingOwner string `json:"pendingOwner"`
}

type OwnerTransferInitiatedEvent struct {
	Type       string                           `json:"type"`
	Attributes OwnerTransferInitiatedAttributes `json:"attributes"`
	Tx         string                           `json:"tx"`
}

type OwnerTransferInitiatedAttributes struct {
	CurrentOwner string `json:"currentOwner"`
	PendingOwner string `json:"pendingOwner"`
}

type OwnerTransferCancelledEvent struct {
	Type       string                           `json:"type"`
	Attributes OwnerTransferCancelledAttributes `json:"attributes"`
	Tx         string                           `json:"tx"`
}

type OwnerTransferCancelledAttributes struct {
	By string `json:"by"`
}

type ListingIdPayload struct {
	ListingId uint64 `json:"listingId"`
}

type OfferIdPayload struct {
	OfferId uint64 `json:"offerId"`
}

type SetRoyaltyPayload struct {
	NftContract      string `json:"nftContract"`
	RoyaltyBps       uint64 `json:"royaltyBps"`
	RoyaltyRecipient string `json:"royaltyRecipient"`
}

type GetRoyaltyPayload struct {
	NftContract string `json:"nftContract"`
}

type SetMinOfferPayload struct {
	MinOffer string `json:"minOffer"`
}

type PaymentTokenPayload struct {
	Token string `json:"token"`
	// Optional balance-decoder type: "magi_token" | "utxo" | "native".
	// Binds the token to a known raw-state layout so tokenBalanceOf reads
	// the right key/encoding instead of probing. Omitted = "auto" (legacy
	// probe) for back-compat.
	Decoder string `json:"decoder"`
}

type EmergencyWithdrawPayload struct {
	TokenType string `json:"tokenType"`
	Contract  string `json:"contract"`
	TokenId   string `json:"tokenId"`
	Amount    string `json:"amount"`
	To        string `json:"to"`
}

type BatchListPayload struct {
	Items []ListPayload `json:"items"`
}

type BatchBuyPayload struct {
	Items []BuyPayload `json:"items"`
}

// Auction payloads
type CreateAuctionPayload struct {
	NftContract  string `json:"nftContract"`
	TokenId      string `json:"tokenId"`
	Amount       uint64 `json:"amount"`
	PaymentToken string `json:"paymentToken"`
	AuctionType  string `json:"auctionType"`
	StartPrice   string `json:"startPrice"`
	EndPrice     string `json:"endPrice"`
	StartBlock   uint64 `json:"startBlock"`
	EndBlock     uint64 `json:"endBlock"`
}

type PlaceBidPayload struct {
	AuctionId uint64 `json:"auctionId"`
	BidAmount string `json:"bidAmount"`
}

type SettleAuctionPayload struct {
	AuctionId uint64 `json:"auctionId"`
}

type CancelAuctionPayload struct {
	AuctionId uint64 `json:"auctionId"`
}

type AuctionIdPayload struct {
	AuctionId uint64 `json:"auctionId"`
}

type SetMinBidIncrementPayload struct {
	MinBidIncrementBps uint64 `json:"minBidIncrementBps"`
}

type SetAntiSnipePayload struct {
	AntiSnipeBlocks uint64 `json:"antiSnipeBlocks"`
}

// ===================================
// Response Types (Output)
// ===================================

type SuccessResponse struct {
	Success bool `json:"success"`
}

type CreatedResponse struct {
	Success bool   `json:"success"`
	Id      uint64 `json:"id"`
}

type ListingResponse struct {
	ListingId       uint64 `json:"listingId"`
	Seller          string `json:"seller"`
	NftContract     string `json:"nftContract"`
	TokenId         string `json:"tokenId"`
	Amount          uint64 `json:"amount"`
	PricePerUnit    string `json:"pricePerUnit"`
	PaymentToken    string `json:"paymentToken"`
	Active          bool   `json:"active"`
	ExpirationBlock uint64 `json:"expirationBlock"`
	FeeBps          uint64 `json:"feeBps"`
	RoyaltyBps      uint64 `json:"royaltyBps"`
	StartBlock      uint64 `json:"startBlock"`
}

// ===================================
// Token-contract (ERC-20) fixed-price sale — separate from NFT listings.
// Asset = `Amount` of an ERC-20 `TokenContract` (big.Int decimal strings;
// no tokenId / collection / royalty / soulbound). Custody is
// approval-based: the seller grants the market an ERC-20 allowance
// (`approve(contract:<market>, >= amount)`) and `buyToken` pulls the
// asset via `transferFrom` (atomic; reverts + refunds if short).
// ===================================

type ListTokenPayload struct {
	TokenContract   string `json:"tokenContract"`
	Amount          string `json:"amount"`
	PaymentToken    string `json:"paymentToken"`
	PricePerUnit    string `json:"pricePerUnit"`
	ExpirationBlock uint64 `json:"expirationBlock"`
	StartBlock      uint64 `json:"startBlock"`
}

type BuyTokenPayload struct {
	ListingId uint64 `json:"listingId"`
	Amount    string `json:"amount"`
}

type TokenListingIdPayload struct {
	ListingId uint64 `json:"listingId"`
}

type TokenListingResponse struct {
	ListingId       uint64 `json:"listingId"`
	Seller          string `json:"seller"`
	TokenContract   string `json:"tokenContract"`
	Amount          string `json:"amount"`
	PricePerUnit    string `json:"pricePerUnit"`
	PaymentToken    string `json:"paymentToken"`
	Active          bool   `json:"active"`
	ExpirationBlock uint64 `json:"expirationBlock"`
	StartBlock      uint64 `json:"startBlock"`
	FeeBps          uint64 `json:"feeBps"`
}

type OfferResponse struct {
	OfferId         uint64 `json:"offerId"`
	Buyer           string `json:"buyer"`
	NftContract     string `json:"nftContract"`
	TokenId         string `json:"tokenId"`
	Amount          uint64 `json:"amount"`
	PricePerUnit    string `json:"pricePerUnit"`
	PaymentToken    string `json:"paymentToken"`
	Active          bool   `json:"active"`
	ExpirationBlock uint64 `json:"expirationBlock"`
	FeeBps          uint64 `json:"feeBps"`
	RoyaltyBps      uint64 `json:"royaltyBps"`
	IsCollection    bool   `json:"isCollection"`
}

type InfoResponse struct {
	Owner              string `json:"owner"`
	FeeBps             uint64 `json:"feeBps"`
	FeeRecipient       string `json:"feeRecipient"`
	Paused             bool   `json:"paused"`
	MinOffer           string `json:"minOffer"`
	MinBidIncrementBps uint64 `json:"minBidIncrementBps"`
	AntiSnipeBlocks    uint64 `json:"antiSnipeBlocks"`
}

type OwnerResponse struct {
	Owner string `json:"owner"`
}

type PausedResponse struct {
	Paused bool `json:"paused"`
}

type RoyaltyResponse struct {
	NftContract      string `json:"nftContract"`
	RoyaltyBps       uint64 `json:"royaltyBps"`
	RoyaltyRecipient string `json:"royaltyRecipient"`
}

type MinOfferResponse struct {
	MinOffer string `json:"minOffer"`
}

type PaymentTokenAllowedResponse struct {
	Allowed bool `json:"allowed"`
}

type AuctionResponse struct {
	AuctionId    uint64 `json:"auctionId"`
	Seller       string `json:"seller"`
	NftContract  string `json:"nftContract"`
	TokenId      string `json:"tokenId"`
	Amount       uint64 `json:"amount"`
	PaymentToken string `json:"paymentToken"`
	AuctionType  string `json:"auctionType"`
	StartPrice   string `json:"startPrice"`
	EndPrice     string `json:"endPrice"`
	StartBlock   uint64 `json:"startBlock"`
	EndBlock     uint64 `json:"endBlock"`
	HighBidder   string `json:"highBidder"`
	HighBid      string `json:"highBid"`
	Active       bool   `json:"active"`
	Settled      bool   `json:"settled"`
	FeeBps       uint64 `json:"feeBps"`
	RoyaltyBps   uint64 `json:"royaltyBps"`
}

// ===================================
// Event Types
// ===================================

type InitEvent struct {
	Type       string         `json:"type"`
	Attributes InitAttributes `json:"attributes"`
	Tx         string         `json:"tx"`
}

type InitAttributes struct {
	Owner        string `json:"owner"`
	FeeBps       uint64 `json:"feeBps"`
	FeeRecipient string `json:"feeRecipient"`
}

type ListedEvent struct {
	Type       string           `json:"type"`
	Attributes ListedAttributes `json:"attributes"`
	Tx         string           `json:"tx"`
}

type ListedAttributes struct {
	ListingId       uint64 `json:"listingId"`
	Seller          string `json:"seller"`
	NftContract     string `json:"nftContract"`
	TokenId         string `json:"tokenId"`
	Amount          uint64 `json:"amount"`
	PricePerUnit    string `json:"pricePerUnit"`
	PaymentToken    string `json:"paymentToken"`
	ExpirationBlock uint64 `json:"expirationBlock"`
}

type DelistedEvent struct {
	Type       string             `json:"type"`
	Attributes DelistedAttributes `json:"attributes"`
	Tx         string             `json:"tx"`
}

type DelistedAttributes struct {
	ListingId uint64 `json:"listingId"`
	Seller    string `json:"seller"`
}

// PaymentTokenEvent is emitted by addPaymentToken/removePaymentToken so the
// indexer can fold the current whitelist set (Type is "payment_token_added"
// or "payment_token_removed"). One struct, two type strings.
type PaymentTokenEvent struct {
	Type       string                      `json:"type"`
	Attributes PaymentTokenEventAttributes `json:"attributes"`
	Tx         string                      `json:"tx"`
}

type PaymentTokenEventAttributes struct {
	Token string `json:"token"`
}

type BoughtEvent struct {
	Type       string           `json:"type"`
	Attributes BoughtAttributes `json:"attributes"`
	Tx         string           `json:"tx"`
}

type BoughtAttributes struct {
	ListingId  uint64 `json:"listingId"`
	Buyer      string `json:"buyer"`
	Amount     uint64 `json:"amount"`
	TotalPrice string `json:"totalPrice"`
	Fee        string `json:"fee"`
	Royalty    string `json:"royalty"`
}

type ListingUpdatedEvent struct {
	Type       string                   `json:"type"`
	Attributes ListingUpdatedAttributes `json:"attributes"`
	Tx         string                   `json:"tx"`
}

type ListingUpdatedAttributes struct {
	ListingId uint64 `json:"listingId"`
	NewPrice  string `json:"newPrice"`
}

type OfferMadeEvent struct {
	Type       string              `json:"type"`
	Attributes OfferMadeAttributes `json:"attributes"`
	Tx         string              `json:"tx"`
}

type OfferMadeAttributes struct {
	OfferId         uint64 `json:"offerId"`
	Buyer           string `json:"buyer"`
	NftContract     string `json:"nftContract"`
	TokenId         string `json:"tokenId"`
	Amount          uint64 `json:"amount"`
	PricePerUnit    string `json:"pricePerUnit"`
	PaymentToken    string `json:"paymentToken"`
	ExpirationBlock uint64 `json:"expirationBlock"`
	IsCollection    bool   `json:"isCollection"`
}

type OfferCancelledEvent struct {
	Type       string                   `json:"type"`
	Attributes OfferCancelledAttributes `json:"attributes"`
	Tx         string                   `json:"tx"`
}

type OfferCancelledAttributes struct {
	OfferId uint64 `json:"offerId"`
	Buyer   string `json:"buyer"`
}

type OfferAcceptedEvent struct {
	Type       string                  `json:"type"`
	Attributes OfferAcceptedAttributes `json:"attributes"`
	Tx         string                  `json:"tx"`
}

type OfferAcceptedAttributes struct {
	OfferId    uint64 `json:"offerId"`
	Seller     string `json:"seller"`
	Buyer      string `json:"buyer"`
	Amount     uint64 `json:"amount"`
	TotalPrice string `json:"totalPrice"`
	Fee        string `json:"fee"`
	Royalty    string `json:"royalty"`
	TokenId    string `json:"tokenId"`
}

type OwnerChangeEvent struct {
	Type       string                `json:"type"`
	Attributes OwnerChangeAttributes `json:"attributes"`
	Tx         string                `json:"tx"`
}

type OwnerChangeAttributes struct {
	PreviousOwner string `json:"previousOwner"`
	NewOwner      string `json:"newOwner"`
}

type PausedEvent struct {
	Type       string           `json:"type"`
	Attributes PausedAttributes `json:"attributes"`
	Tx         string           `json:"tx"`
}

type PausedAttributes struct {
	By string `json:"by"`
}

type UnpausedEvent struct {
	Type       string             `json:"type"`
	Attributes UnpausedAttributes `json:"attributes"`
	Tx         string             `json:"tx"`
}

type UnpausedAttributes struct {
	By string `json:"by"`
}

type RoyaltySetEvent struct {
	Type       string               `json:"type"`
	Attributes RoyaltySetAttributes `json:"attributes"`
	Tx         string               `json:"tx"`
}

type RoyaltySetAttributes struct {
	NftContract      string `json:"nftContract"`
	RoyaltyBps       uint64 `json:"royaltyBps"`
	RoyaltyRecipient string `json:"royaltyRecipient"`
}

type EmergencyWithdrawEvent struct {
	Type       string                      `json:"type"`
	Attributes EmergencyWithdrawAttributes `json:"attributes"`
	Tx         string                      `json:"tx"`
}

type EmergencyWithdrawAttributes struct {
	TokenType string `json:"tokenType"`
	Contract  string `json:"contract"`
	TokenId   string `json:"tokenId"`
	Amount    string `json:"amount"`
	To        string `json:"to"`
}

type AuctionCreatedEvent struct {
	Type       string                   `json:"type"`
	Attributes AuctionCreatedAttributes `json:"attributes"`
	Tx         string                   `json:"tx"`
}

type AuctionCreatedAttributes struct {
	AuctionId   uint64 `json:"auctionId"`
	Seller      string `json:"seller"`
	NftContract string `json:"nftContract"`
	TokenId     string `json:"tokenId"`
	Amount      uint64 `json:"amount"`
	AuctionType string `json:"auctionType"`
	StartPrice  string `json:"startPrice"`
	EndPrice    string `json:"endPrice"`
	StartBlock  uint64 `json:"startBlock"`
	EndBlock    uint64 `json:"endBlock"`
}

type BidPlacedEvent struct {
	Type       string              `json:"type"`
	Attributes BidPlacedAttributes `json:"attributes"`
	Tx         string              `json:"tx"`
}

type BidPlacedAttributes struct {
	AuctionId uint64 `json:"auctionId"`
	Bidder    string `json:"bidder"`
	BidAmount string `json:"bidAmount"`
}

type AuctionSettledEvent struct {
	Type       string                   `json:"type"`
	Attributes AuctionSettledAttributes `json:"attributes"`
	Tx         string                   `json:"tx"`
}

type AuctionSettledAttributes struct {
	AuctionId  uint64 `json:"auctionId"`
	Winner     string `json:"winner"`
	FinalPrice string `json:"finalPrice"`
	Fee        string `json:"fee"`
	Royalty    string `json:"royalty"`
}

type AuctionCancelledEvent struct {
	Type       string                     `json:"type"`
	Attributes AuctionCancelledAttributes `json:"attributes"`
	Tx         string                     `json:"tx"`
}

type AuctionCancelledAttributes struct {
	AuctionId uint64 `json:"auctionId"`
	Seller    string `json:"seller"`
}

type CollectionPayload struct {
	NftContract string `json:"nftContract"`
}

type CollectionDeniedResponse struct {
	Denied bool `json:"denied"`
}

type CollectionDeniedEvent struct {
	Type       string                     `json:"type"`
	Attributes CollectionDeniedAttributes `json:"attributes"`
	Tx         string                     `json:"tx"`
}

type CollectionDeniedAttributes struct {
	NftContract string `json:"nftContract"`
	By          string `json:"by"`
}

type CollectionAllowedEvent struct {
	Type       string                      `json:"type"`
	Attributes CollectionAllowedAttributes `json:"attributes"`
	Tx         string                      `json:"tx"`
}

type CollectionAllowedAttributes struct {
	NftContract string `json:"nftContract"`
	By          string `json:"by"`
}

// ===================================
// B1: Royalty Splits Types
// ===================================

type RoyaltySplit struct {
	Recipient string `json:"recipient"`
	Bps       uint64 `json:"bps"`
}

type SetRoyaltySplitsPayload struct {
	NftContract string         `json:"nftContract"`
	Splits      []RoyaltySplit `json:"splits"`
}

type RoyaltySplitsResponse struct {
	NftContract string         `json:"nftContract"`
	Splits      []RoyaltySplit `json:"splits"`
}

type RoyaltySplitsSetEvent struct {
	Type       string                     `json:"type"`
	Attributes RoyaltySplitsSetAttributes `json:"attributes"`
	Tx         string                     `json:"tx"`
}

type RoyaltySplitsSetAttributes struct {
	NftContract string `json:"nftContract"`
	Count       uint64 `json:"count"`
}

// ===================================
// C2: Floor Sweep Types
// ===================================

type SweepPayload struct {
	NftContract string   `json:"nftContract"`
	ListingIds  []uint64 `json:"listingIds"`
	MaxTotal    string   `json:"maxTotal"`
	// The one asset this sweep spends. MaxTotal is a bare integer with no
	// currency of its own, so without pinning the token every listing is
	// paid in, the cap would compare a sum of different currencies against
	// a number that belongs to none of them. Empty is still accepted and
	// means "whatever the first listing is priced in" — old callers that
	// only ever swept a single token keep working, and the ones that mixed
	// tokens now abort, which is the point.
	PaymentToken string `json:"paymentToken"`
}

type SweptEvent struct {
	Type       string          `json:"type"`
	Attributes SweptAttributes `json:"attributes"`
	Tx         string          `json:"tx"`
}

type SweptAttributes struct {
	Buyer string `json:"buyer"`
	Count uint64 `json:"count"`
	Total string `json:"total"`
	// Which asset Total is denominated in. Without it the number is not
	// interpretable by anything downstream — the indexer included.
	PaymentToken string `json:"paymentToken"`
}

// ===================================
// B3: Per-Collection Fee Override Types
// ===================================

type CollectionFeePayload struct {
	NftContract string `json:"nftContract"`
	FeeBps      uint64 `json:"feeBps"`
}

type EffectiveFeeResponse struct {
	FeeBps uint64 `json:"feeBps"`
}

type CollectionFeeSetEvent struct {
	Type       string                     `json:"type"`
	Attributes CollectionFeeSetAttributes `json:"attributes"`
	Tx         string                     `json:"tx"`
}

type CollectionFeeSetAttributes struct {
	NftContract string `json:"nftContract"`
	FeeBps      uint64 `json:"feeBps"`
}

type CollectionFeeClearedEvent struct {
	Type       string                         `json:"type"`
	Attributes CollectionFeeClearedAttributes `json:"attributes"`
	Tx         string                         `json:"tx"`
}

type CollectionFeeClearedAttributes struct {
	NftContract string `json:"nftContract"`
}

// ===================================
// C3: Bundle Types
// ===================================

type BundleItem struct {
	TokenId string `json:"tokenId"`
	Amount  uint64 `json:"amount"`
}

// ===================================
// Buckets (random-draw sales)
// ===================================

// MaxBucketEntries caps how many distinct token ids one bucket holds.
//
// This used to be 20, because the draw re-read and re-scanned EVERY entry on
// every draw: cost was O(entries), so a large bucket could not be drawn from
// inside the default rcLimit at all. Entries now live in per-stack slot arrays
// with chunked unit sums (see internal.go), so a draw touches
// `entries/BucketChunk + BucketChunk` slots instead of all of them, and the
// cap can be set by what the STATE should sensibly hold rather than by what a
// single draw can afford to scan.
const MaxBucketEntries = 512

// MaxEntriesPerCall caps how many entries ONE listBucket or addToBucket call
// may add. A full bucket cannot be stocked in a single transaction at any
// per-entry cost, which is exactly why addToBucket exists — stock a big bucket
// across several calls.
//
// Measured: listing costs ~1570 RC fixed plus ~265 RC per entry (1 entry =
// 1572, 5 entries = 2634), against a default 10000 rcLimit. 24 entries lands
// near 7900 and leaves room for the sellers who cost more per entry — anyone
// authorising with per-token allowances instead of operator approval, or
// stocking a collection they do not own, pays an extra state read each.
//
// A cap the RC budget cannot honour would just move the failure from a clear
// message to an opaque "cost limit exceeded", which is the same mistake
// MaxDrawWork exists to avoid.
const MaxEntriesPerCall = 24

// BucketChunk is how many slots share one cached unit sum. The draw walks chunk
// sums to find the right chunk, then scans inside it, so per-draw work is
// `entries/BucketChunk + BucketChunk` — minimised near sqrt(entries). 32 is the
// sweet spot for a 512-entry bucket (16 + 32) and costs nothing at small sizes,
// where the whole bucket is one chunk.
const BucketChunk = 32

// MaxDrawsPerTx is an absolute ceiling on the draws one transaction performs.
// It is a backstop; MaxDrawWork below is the constraint that actually binds.
const MaxDrawsPerTx = 24

// MaxDrawWork bounds the WORK a purchase may ask for. RC still scales with the
// entry count, just far more slowly than it did, so the bound is kept with a
// formula that tracks the new shape:
//
//	work = draws * (entries/BucketChunk + min(entries, BucketChunk) + 8)
//
// Measured on the chunked layout (RC, on the default 10000 rcLimit):
//
//	fixed                            ~1840 RC   payment pull, fee/royalty/seller
//	per work unit                      ~13 RC
//
//	 1 draw  x 500 entries =  55 work =  2738 RC   (measured)
//	10 draws x 500 entries = 550 work =  9113 RC   (measured)
//	10 draws x   5 entries = 130 work =  3687 RC   (measured)
//
// (10000 - 1840) / 13 ~= 620, so 600 is the honest ceiling: it admits the
// 10-card pack over a full 500-entry bucket that has just been measured, and
// refuses the 11-card one that would not fit.
//
// The point is unchanged: refuse an oversized purchase with a clear message
// BEFORE taking payment, rather than letting it die deep in execution with an
// opaque "cost limit exceeded".
const MaxDrawWork = 600

// MaxBucketStacks caps how many stacks a pack may draw from. Each stack costs a
// pass over the entries per draw, so this bounds the work a single purchase can
// ask for. Four is already a Pokemon-style pack (commons / uncommons / reverse
// holo / rare); eight leaves room without letting a pack become unbounded.
const MaxBucketStacks = 8

// BucketEntry is one already-minted token id and how many units of it are in
// the bucket. Amount > 1 is how editions are stocked: each unit is a separate
// prize, so an entry with 50 units is 50x likelier to be drawn than a 1/1.
//
// Stack groups entries that compete with each other. A bucket with everything in
// stack 0 is one flat pile — the simple case. Splitting entries across stacks is
// what makes a real card pack possible: commons in stack 0, rares in stack 1, and
// a pack that always takes one draw from stack 1 always contains a rare.
type BucketEntry struct {
	TokenId string `json:"tokenId"`
	Amount  uint64 `json:"amount"`
	Stack    uint64 `json:"stack"`
}

// ListBucketPayload creates a bucket. The seller enables single draws, pack
// draws, or both: a zero/empty price switches that mode off, and at least one
// must be on.
//
// PackDraws describes a pack as draws-per-stack, indexed by stack: [5] is five
// draws from one flat pile, and [4,3,1,1] is a card pack — four commons, three
// uncommons, one reverse holo, one rare — where the last slot GUARANTEES a rare
// because it can only be filled from stack 3. One field expresses both the
// simple and the elaborate case, and the pack size is just its sum.
//
// Single draws always come from stack 0.
type ListBucketPayload struct {
	NftContract     string        `json:"nftContract"`
	Entries         []BucketEntry `json:"entries"`
	PaymentToken    string        `json:"paymentToken"`
	PricePerDraw    string        `json:"pricePerDraw"`
	PricePerPack    string        `json:"pricePerPack"`
	PackDraws       []uint64      `json:"packDraws"`
	ExpirationBlock uint64        `json:"expirationBlock"`
}

// BuyFromBucketPayload buys from a bucket. Mode "single" draws Quantity times;
// mode "pack" draws Quantity * packSize times. MaxTotalPrice is the same
// slippage guard `buy` and `sweep` carry — empty disables it.
type BuyFromBucketPayload struct {
	BucketId      uint64 `json:"bucketId"`
	Mode          string `json:"mode"`
	Quantity      uint64 `json:"quantity"`
	MaxTotalPrice string `json:"maxTotalPrice"`
}

type BucketIdPayload struct {
	BucketId uint64 `json:"bucketId"`
}

// AddToBucketPayload stocks MORE entries into a bucket that already exists.
//
// A 500-card bucket cannot be listed in one transaction — the per-entry write
// cost alone exceeds the rcLimit long before that — so stocking is chunked:
// listBucket opens the bucket with a first batch, addToBucket appends the rest.
type AddToBucketPayload struct {
	BucketId uint64        `json:"bucketId"`
	Entries  []BucketEntry `json:"entries"`
}

type BucketRestockedEvent struct {
	Type       string                    `json:"type"`
	Attributes BucketRestockedAttributes `json:"attributes"`
	Tx         string                    `json:"tx"`
}

type BucketRestockedAttributes struct {
	BucketId     uint64        `json:"bucketId"`
	Seller       string        `json:"seller"`
	Entries      []BucketEntry `json:"entries"`
	Added        uint64        `json:"added"`
	TotalEntries uint64        `json:"totalEntries"`
	UnitsAdded   uint64        `json:"unitsAdded"`
}

type BucketListedEvent struct {
	Type       string                 `json:"type"`
	Attributes BucketListedAttributes `json:"attributes"`
	Tx         string                 `json:"tx"`
}

// BucketListedAttributes carries everything an indexer needs to mirror a new
// bucket without reading contract state: the commercial terms, the fee and
// royalty snapshot taken at list time, and the entries themselves.
//
// The entries are safe to inline because listBucket accepts at most
// MaxEntriesPerCall of them, so one event can never carry an unbounded array —
// a large bucket arrives as a listing plus a series of restocks, each bounded
// the same way.
type BucketListedAttributes struct {
	BucketId         uint64        `json:"bucketId"`
	Seller           string        `json:"seller"`
	NftContract      string        `json:"nftContract"`
	PaymentToken     string        `json:"paymentToken"`
	PricePerDraw     string        `json:"pricePerDraw"`
	PricePerPack     string        `json:"pricePerPack"`
	PackDraws        []uint64      `json:"packDraws"`
	ExpirationBlock  uint64        `json:"expirationBlock"`
	FeeBps           uint64        `json:"feeBps"`
	RoyaltyBps       uint64        `json:"royaltyBps"`
	RoyaltyRecipient string        `json:"royaltyRecipient"`
	Entries          []BucketEntry `json:"entries"`
	EntryCount       uint64        `json:"entryCount"`
	Units            uint64        `json:"units"`
}

// BucketDrawEvent fires once per delivered unit rather than carrying an array,
// so the indexer gets one row per NFT and can answer "what did this purchase
// yield" and "who holds it now" without unpacking a list.
type BucketDrawEvent struct {
	Type       string               `json:"type"`
	Attributes BucketDrawAttributes `json:"attributes"`
	Tx         string               `json:"tx"`
}

type BucketDrawAttributes struct {
	BucketId  uint64 `json:"bucketId"`
	Buyer     string `json:"buyer"`
	TokenId   string `json:"tokenId"`
	Stack      uint64 `json:"stack"`
	DrawIndex uint64 `json:"drawIndex"`
}

type BucketPurchaseEvent struct {
	Type       string                   `json:"type"`
	Attributes BucketPurchaseAttributes `json:"attributes"`
	Tx         string                   `json:"tx"`
}

type BucketPurchaseAttributes struct {
	BucketId     uint64 `json:"bucketId"`
	Buyer        string `json:"buyer"`
	Mode         string `json:"mode"`
	Draws        uint64 `json:"draws"`
	PaymentToken string `json:"paymentToken"`
	Paid         string `json:"paid"`
	Fee          string `json:"fee"`
	Royalty      string `json:"royalty"`
	UnitsLeft    uint64 `json:"unitsLeft"`
}

// BucketEntryDroppedEvent records an entry pruned mid-draw because the seller
// no longer holds it or revoked approval. Without this the units simply vanish
// from the bucket with no on-chain explanation.
type BucketEntryDroppedEvent struct {
	Type       string                       `json:"type"`
	Attributes BucketEntryDroppedAttributes `json:"attributes"`
	Tx         string                       `json:"tx"`
}

type BucketEntryDroppedAttributes struct {
	BucketId uint64 `json:"bucketId"`
	TokenId  string `json:"tokenId"`
	Stack     uint64 `json:"stack"`
	Units    uint64 `json:"units"`
	Reason   string `json:"reason"`
}

// BucketSoldOutEvent fires when the last unit leaves a bucket and it closes
// itself. Without it the only way to observe a closed bucket is delisting,
// which is a SELLER action — a bucket that simply sold out would look open
// forever to anything reading the log.
type BucketSoldOutEvent struct {
	Type       string                  `json:"type"`
	Attributes BucketSoldOutAttributes `json:"attributes"`
	Tx         string                  `json:"tx"`
}

type BucketSoldOutAttributes struct {
	BucketId uint64 `json:"bucketId"`
	Seller   string `json:"seller"`
}

type BucketDelistedEvent struct {
	Type       string                   `json:"type"`
	Attributes BucketDelistedAttributes `json:"attributes"`
	Tx         string                   `json:"tx"`
}

type BucketDelistedAttributes struct {
	BucketId uint64 `json:"bucketId"`
	Seller   string `json:"seller"`
}

type ListBundlePayload struct {
	NftContract     string       `json:"nftContract"`
	Items           []BundleItem `json:"items"`
	PaymentToken    string       `json:"paymentToken"`
	Price           string       `json:"price"`
	ExpirationBlock uint64       `json:"expirationBlock"`
}

type BundleIdPayload struct {
	BundleId uint64 `json:"bundleId"`
}

type BundleResponse struct {
	BundleId        uint64       `json:"bundleId"`
	Seller          string       `json:"seller"`
	NftContract     string       `json:"nftContract"`
	Items           []BundleItem `json:"items"`
	PaymentToken    string       `json:"paymentToken"`
	Price           string       `json:"price"`
	Active          bool         `json:"active"`
	ExpirationBlock uint64       `json:"expirationBlock"`
}

type BundleListedEvent struct {
	Type       string                 `json:"type"`
	Attributes BundleListedAttributes `json:"attributes"`
	Tx         string                 `json:"tx"`
}

type BundleListedAttributes struct {
	BundleId    uint64 `json:"bundleId"`
	Seller      string `json:"seller"`
	NftContract string `json:"nftContract"`
	Count       uint64 `json:"count"`
	Price       string `json:"price"`
}

type BundleBoughtEvent struct {
	Type       string                 `json:"type"`
	Attributes BundleBoughtAttributes `json:"attributes"`
	Tx         string                 `json:"tx"`
}

type BundleBoughtAttributes struct {
	BundleId   uint64 `json:"bundleId"`
	Buyer      string `json:"buyer"`
	TotalPrice string `json:"totalPrice"`
	Fee        string `json:"fee"`
	Royalty    string `json:"royalty"`
}

type BundleDelistedEvent struct {
	Type       string                   `json:"type"`
	Attributes BundleDelistedAttributes `json:"attributes"`
	Tx         string                   `json:"tx"`
}

type BundleDelistedAttributes struct {
	BundleId uint64 `json:"bundleId"`
	Seller   string `json:"seller"`
}

// ===================================
// D1: NFT-for-NFT Swap Types
// ===================================

type ProposeSwapPayload struct {
	OfferedNft      string `json:"offeredNft"`
	OfferedTokenId  string `json:"offeredTokenId"`
	OfferedAmount   uint64 `json:"offeredAmount"`
	WantedNft       string `json:"wantedNft"`
	WantedTokenId   string `json:"wantedTokenId"`
	WantedAmount    uint64 `json:"wantedAmount"`
	TopUp           string `json:"topUp"`
	TopUpToken      string `json:"topUpToken"`
	ExpirationBlock uint64 `json:"expirationBlock"`
}

type SwapIdPayload struct {
	SwapId uint64 `json:"swapId"`
}

type SwapResponse struct {
	SwapId          uint64 `json:"swapId"`
	Proposer        string `json:"proposer"`
	OfferedNft      string `json:"offeredNft"`
	OfferedTokenId  string `json:"offeredTokenId"`
	OfferedAmount   uint64 `json:"offeredAmount"`
	WantedNft       string `json:"wantedNft"`
	WantedTokenId   string `json:"wantedTokenId"`
	WantedAmount    uint64 `json:"wantedAmount"`
	TopUp           string `json:"topUp"`
	TopUpToken      string `json:"topUpToken"`
	Active          bool   `json:"active"`
	ExpirationBlock uint64 `json:"expirationBlock"`
}

type SwapProposedEvent struct {
	Type       string                 `json:"type"`
	Attributes SwapProposedAttributes `json:"attributes"`
	Tx         string                 `json:"tx"`
}

type SwapProposedAttributes struct {
	SwapId     uint64 `json:"swapId"`
	Proposer   string `json:"proposer"`
	OfferedNft string `json:"offeredNft"`
	WantedNft  string `json:"wantedNft"`
}

type SwapAcceptedEvent struct {
	Type       string                 `json:"type"`
	Attributes SwapAcceptedAttributes `json:"attributes"`
	Tx         string                 `json:"tx"`
}

type SwapAcceptedAttributes struct {
	SwapId   uint64 `json:"swapId"`
	Proposer string `json:"proposer"`
	Acceptor string `json:"acceptor"`
}

type SwapCancelledEvent struct {
	Type       string                  `json:"type"`
	Attributes SwapCancelledAttributes `json:"attributes"`
	Tx         string                  `json:"tx"`
}

type SwapCancelledAttributes struct {
	SwapId uint64 `json:"swapId"`
	By     string `json:"by"`
}

// ===================================
// E1: NFT Rental Types
// ===================================

type ListRentalPayload struct {
	NftContract   string `json:"nftContract"`
	TokenId       string `json:"tokenId"`
	Amount        uint64 `json:"amount"`
	PaymentToken  string `json:"paymentToken"`
	PricePerBlock string `json:"pricePerBlock"`
	MinBlocks     uint64 `json:"minBlocks"`
	MaxBlocks     uint64 `json:"maxBlocks"`
}

type RentPayload struct {
	RentalId uint64 `json:"rentalId"`
	Blocks   uint64 `json:"blocks"`
}

type RentalIdPayload struct {
	RentalId uint64 `json:"rentalId"`
}

type ActiveRentalQuery struct {
	Account     string `json:"account"`
	NftContract string `json:"nftContract"`
	TokenId     string `json:"tokenId"`
}

type RentalResponse struct {
	RentalId      uint64 `json:"rentalId"`
	Owner         string `json:"owner"`
	NftContract   string `json:"nftContract"`
	TokenId       string `json:"tokenId"`
	Amount        uint64 `json:"amount"`
	PaymentToken  string `json:"paymentToken"`
	PricePerBlock string `json:"pricePerBlock"`
	MinBlocks     uint64 `json:"minBlocks"`
	MaxBlocks     uint64 `json:"maxBlocks"`
	Active        bool   `json:"active"`
	Renter        string `json:"renter"`
	Until         uint64 `json:"until"`
	Rented        bool   `json:"rented"`
}

type ActiveRentalResponse struct {
	Active bool   `json:"active"`
	Until  uint64 `json:"until"`
}

type RentalListedEvent struct {
	Type       string                 `json:"type"`
	Attributes RentalListedAttributes `json:"attributes"`
	Tx         string                 `json:"tx"`
}

type RentalListedAttributes struct {
	RentalId    uint64 `json:"rentalId"`
	Owner       string `json:"owner"`
	NftContract string `json:"nftContract"`
	TokenId     string `json:"tokenId"`
}

type RentedEvent struct {
	Type       string           `json:"type"`
	Attributes RentedAttributes `json:"attributes"`
	Tx         string           `json:"tx"`
}

type RentedAttributes struct {
	RentalId uint64 `json:"rentalId"`
	Renter   string `json:"renter"`
	Until    uint64 `json:"until"`
}

type RentalEndedEvent struct {
	Type       string                `json:"type"`
	Attributes RentalEndedAttributes `json:"attributes"`
	Tx         string                `json:"tx"`
}

type RentalEndedAttributes struct {
	RentalId uint64 `json:"rentalId"`
	By       string `json:"by"`
}

type RentalDelistedEvent struct {
	Type       string                   `json:"type"`
	Attributes RentalDelistedAttributes `json:"attributes"`
	Tx         string                   `json:"tx"`
}

type RentalDelistedAttributes struct {
	RentalId uint64 `json:"rentalId"`
	Owner    string `json:"owner"`
}

// ===================================
// G2: Mint-Spot Primary Sale Types
// ===================================

type ListMintSpotsPayload struct {
	NftContract     string `json:"nftContract"`
	TokenId         string `json:"tokenId"`
	PaymentToken    string `json:"paymentToken"`
	PricePerSpot    string `json:"pricePerSpot"`
	MaxSpots        uint64 `json:"maxSpots"`
	ExpirationBlock uint64 `json:"expirationBlock"`
	StartBlock      uint64 `json:"startBlock"`
}

type BuyMintSpotPayload struct {
	ListingId uint64 `json:"listingId"`
	Amount    uint64 `json:"amount"`
}

type MintSpotIdPayload struct {
	ListingId uint64 `json:"listingId"`
}

type MintSpotListingResponse struct {
	ListingId       uint64 `json:"listingId"`
	Lister          string `json:"lister"`
	NftContract     string `json:"nftContract"`
	TokenId         string `json:"tokenId"`
	PaymentToken    string `json:"paymentToken"`
	PricePerSpot    string `json:"pricePerSpot"`
	MaxSpots        uint64 `json:"maxSpots"`
	Sold            uint64 `json:"sold"`
	Active          bool   `json:"active"`
	ExpirationBlock uint64 `json:"expirationBlock"`
	StartBlock      uint64 `json:"startBlock"`
	FeeBps          uint64 `json:"feeBps"`
}

type MintSpotsListedEvent struct {
	Type       string                    `json:"type"`
	Attributes MintSpotsListedAttributes `json:"attributes"`
	Tx         string                    `json:"tx"`
}

type MintSpotsListedAttributes struct {
	ListingId   uint64 `json:"listingId"`
	Lister      string `json:"lister"`
	NftContract string `json:"nftContract"`
	TokenId     string `json:"tokenId"`
	MaxSpots    uint64 `json:"maxSpots"`
}

type MintSpotBoughtEvent struct {
	Type       string                   `json:"type"`
	Attributes MintSpotBoughtAttributes `json:"attributes"`
	Tx         string                   `json:"tx"`
}

type MintSpotBoughtAttributes struct {
	ListingId uint64 `json:"listingId"`
	Buyer     string `json:"buyer"`
	Amount    uint64 `json:"amount"`
	Received  string `json:"received"`
	Fee       string `json:"fee"`
}

type MintSpotsDelistedEvent struct {
	Type       string                      `json:"type"`
	Attributes MintSpotsDelistedAttributes `json:"attributes"`
	Tx         string                      `json:"tx"`
}

type MintSpotsDelistedAttributes struct {
	ListingId uint64 `json:"listingId"`
	Lister    string `json:"lister"`
}
