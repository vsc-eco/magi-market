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
}

type DelistPayload struct {
	ListingId uint64 `json:"listingId"`
}

type BuyPayload struct {
	ListingId uint64 `json:"listingId"`
	Amount    uint64 `json:"amount"`
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
	Type       string                      `json:"type"`
	Attributes RoyaltySplitsSetAttributes  `json:"attributes"`
	Tx         string                      `json:"tx"`
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
}

type SweptEvent struct {
	Type       string           `json:"type"`
	Attributes SweptAttributes  `json:"attributes"`
	Tx         string           `json:"tx"`
}

type SweptAttributes struct {
	Buyer string `json:"buyer"`
	Count uint64 `json:"count"`
	Total string `json:"total"`
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
	Type       string                       `json:"type"`
	Attributes CollectionFeeSetAttributes   `json:"attributes"`
	Tx         string                       `json:"tx"`
}

type CollectionFeeSetAttributes struct {
	NftContract string `json:"nftContract"`
	FeeBps      uint64 `json:"feeBps"`
}

type CollectionFeeClearedEvent struct {
	Type       string                          `json:"type"`
	Attributes CollectionFeeClearedAttributes  `json:"attributes"`
	Tx         string                          `json:"tx"`
}

type CollectionFeeClearedAttributes struct {
	NftContract string `json:"nftContract"`
}
