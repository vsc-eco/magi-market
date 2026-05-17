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
	PricePerUnit    uint64 `json:"pricePerUnit"`
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
	MinOffer uint64 `json:"minOffer"`
}

type PaymentTokenPayload struct {
	Token string `json:"token"`
}

type EmergencyWithdrawPayload struct {
	TokenType string `json:"tokenType"`
	Contract  string `json:"contract"`
	TokenId   string `json:"tokenId"`
	Amount    uint64 `json:"amount"`
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
	StartPrice   uint64 `json:"startPrice"`
	EndPrice     uint64 `json:"endPrice"`
	StartBlock   uint64 `json:"startBlock"`
	EndBlock     uint64 `json:"endBlock"`
}

type PlaceBidPayload struct {
	AuctionId uint64 `json:"auctionId"`
	BidAmount uint64 `json:"bidAmount"`
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
}

type OfferResponse struct {
	OfferId         uint64 `json:"offerId"`
	Buyer           string `json:"buyer"`
	NftContract     string `json:"nftContract"`
	TokenId         string `json:"tokenId"`
	Amount          uint64 `json:"amount"`
	PricePerUnit    uint64 `json:"pricePerUnit"`
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
	MinOffer           uint64 `json:"minOffer"`
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
	MinOffer uint64 `json:"minOffer"`
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
	StartPrice   uint64 `json:"startPrice"`
	EndPrice     uint64 `json:"endPrice"`
	StartBlock   uint64 `json:"startBlock"`
	EndBlock     uint64 `json:"endBlock"`
	HighBidder   string `json:"highBidder"`
	HighBid      uint64 `json:"highBid"`
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
	PricePerUnit    uint64 `json:"pricePerUnit"`
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
	TotalPrice uint64 `json:"totalPrice"`
	Fee        uint64 `json:"fee"`
	Royalty    uint64 `json:"royalty"`
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
	Amount    uint64 `json:"amount"`
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
	StartPrice  uint64 `json:"startPrice"`
	EndPrice    uint64 `json:"endPrice"`
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
	BidAmount uint64 `json:"bidAmount"`
}

type AuctionSettledEvent struct {
	Type       string                   `json:"type"`
	Attributes AuctionSettledAttributes `json:"attributes"`
	Tx         string                   `json:"tx"`
}

type AuctionSettledAttributes struct {
	AuctionId  uint64 `json:"auctionId"`
	Winner     string `json:"winner"`
	FinalPrice uint64 `json:"finalPrice"`
	Fee        uint64 `json:"fee"`
	Royalty    uint64 `json:"royalty"`
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
