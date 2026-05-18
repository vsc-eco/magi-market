package main

import (
	"magi_market/sdk"

	"github.com/CosmWasm/tinyjson/jwriter"
)

// ==========================================
// MAGI Market - Event Emission (tinyjson)
// ==========================================

func emitInit(owner string, feeBps uint64, feeRecipient string) {
	txID := sdk.GetEnvKey("tx.id")
	event := InitEvent{
		Type:       "init_magi_market",
		Attributes: InitAttributes{Owner: owner, FeeBps: feeBps, FeeRecipient: feeRecipient},
		Tx:         *txID,
	}
	w := jwriter.Writer{}
	event.MarshalTinyJSON(&w)
	sdk.Log(string(w.Buffer.BuildBytes()))
}

func emitListed(listingId uint64, seller, nftContract, tokenId string, amount uint64, pricePerUnit string, paymentToken string, expirationBlock uint64) {
	txID := sdk.GetEnvKey("tx.id")
	event := ListedEvent{
		Type: "listed",
		Attributes: ListedAttributes{
			ListingId: listingId, Seller: seller, NftContract: nftContract,
			TokenId: tokenId, Amount: amount, PricePerUnit: pricePerUnit, PaymentToken: paymentToken,
			ExpirationBlock: expirationBlock,
		},
		Tx: *txID,
	}
	w := jwriter.Writer{}
	event.MarshalTinyJSON(&w)
	sdk.Log(string(w.Buffer.BuildBytes()))
}

func emitDelisted(listingId uint64, seller string) {
	txID := sdk.GetEnvKey("tx.id")
	event := DelistedEvent{
		Type:       "delisted",
		Attributes: DelistedAttributes{ListingId: listingId, Seller: seller},
		Tx:         *txID,
	}
	w := jwriter.Writer{}
	event.MarshalTinyJSON(&w)
	sdk.Log(string(w.Buffer.BuildBytes()))
}

func emitBought(listingId uint64, buyer string, amount uint64, totalPrice, fee, royalty string) {
	txID := sdk.GetEnvKey("tx.id")
	event := BoughtEvent{
		Type: "bought",
		Attributes: BoughtAttributes{
			ListingId: listingId, Buyer: buyer, Amount: amount,
			TotalPrice: totalPrice, Fee: fee, Royalty: royalty,
		},
		Tx: *txID,
	}
	w := jwriter.Writer{}
	event.MarshalTinyJSON(&w)
	sdk.Log(string(w.Buffer.BuildBytes()))
}

func emitListingUpdated(listingId uint64, newPrice string) {
	txID := sdk.GetEnvKey("tx.id")
	event := ListingUpdatedEvent{
		Type:       "listing_updated",
		Attributes: ListingUpdatedAttributes{ListingId: listingId, NewPrice: newPrice},
		Tx:         *txID,
	}
	w := jwriter.Writer{}
	event.MarshalTinyJSON(&w)
	sdk.Log(string(w.Buffer.BuildBytes()))
}

func emitOfferMade(offerId uint64, buyer, nftContract, tokenId string, amount uint64, pricePerUnit string, paymentToken string, expirationBlock uint64, isCollection bool) {
	txID := sdk.GetEnvKey("tx.id")
	event := OfferMadeEvent{
		Type: "offer_made",
		Attributes: OfferMadeAttributes{
			OfferId: offerId, Buyer: buyer, NftContract: nftContract,
			TokenId: tokenId, Amount: amount, PricePerUnit: pricePerUnit, PaymentToken: paymentToken,
			ExpirationBlock: expirationBlock, IsCollection: isCollection,
		},
		Tx: *txID,
	}
	w := jwriter.Writer{}
	event.MarshalTinyJSON(&w)
	sdk.Log(string(w.Buffer.BuildBytes()))
}

func emitOfferCancelled(offerId uint64, buyer string) {
	txID := sdk.GetEnvKey("tx.id")
	event := OfferCancelledEvent{
		Type:       "offer_cancelled",
		Attributes: OfferCancelledAttributes{OfferId: offerId, Buyer: buyer},
		Tx:         *txID,
	}
	w := jwriter.Writer{}
	event.MarshalTinyJSON(&w)
	sdk.Log(string(w.Buffer.BuildBytes()))
}

func emitOfferAccepted(offerId uint64, seller, buyer string, amount uint64, totalPrice, fee, royalty string, tokenId string) {
	txID := sdk.GetEnvKey("tx.id")
	event := OfferAcceptedEvent{
		Type: "offer_accepted",
		Attributes: OfferAcceptedAttributes{
			OfferId: offerId, Seller: seller, Buyer: buyer,
			Amount: amount, TotalPrice: totalPrice, Fee: fee, Royalty: royalty, TokenId: tokenId,
		},
		Tx: *txID,
	}
	w := jwriter.Writer{}
	event.MarshalTinyJSON(&w)
	sdk.Log(string(w.Buffer.BuildBytes()))
}

func emitOwnerChange(previousOwner, newOwner string) {
	txID := sdk.GetEnvKey("tx.id")
	event := OwnerChangeEvent{
		Type:       "ownerChange",
		Attributes: OwnerChangeAttributes{PreviousOwner: previousOwner, NewOwner: newOwner},
		Tx:         *txID,
	}
	w := jwriter.Writer{}
	event.MarshalTinyJSON(&w)
	sdk.Log(string(w.Buffer.BuildBytes()))
}

func emitOwnerTransferInitiated(currentOwner, pendingOwner string) {
	txID := sdk.GetEnvKey("tx.id")
	event := OwnerTransferInitiatedEvent{
		Type:       "ownerTransferInitiated",
		Attributes: OwnerTransferInitiatedAttributes{CurrentOwner: currentOwner, PendingOwner: pendingOwner},
		Tx:         *txID,
	}
	w := jwriter.Writer{}
	event.MarshalTinyJSON(&w)
	sdk.Log(string(w.Buffer.BuildBytes()))
}

func emitOwnerTransferCancelled(by string) {
	txID := sdk.GetEnvKey("tx.id")
	event := OwnerTransferCancelledEvent{
		Type:       "ownerTransferCancelled",
		Attributes: OwnerTransferCancelledAttributes{By: by},
		Tx:         *txID,
	}
	w := jwriter.Writer{}
	event.MarshalTinyJSON(&w)
	sdk.Log(string(w.Buffer.BuildBytes()))
}

func emitPaused(by string) {
	txID := sdk.GetEnvKey("tx.id")
	event := PausedEvent{
		Type:       "paused",
		Attributes: PausedAttributes{By: by},
		Tx:         *txID,
	}
	w := jwriter.Writer{}
	event.MarshalTinyJSON(&w)
	sdk.Log(string(w.Buffer.BuildBytes()))
}

func emitUnpaused(by string) {
	txID := sdk.GetEnvKey("tx.id")
	event := UnpausedEvent{
		Type:       "unpaused",
		Attributes: UnpausedAttributes{By: by},
		Tx:         *txID,
	}
	w := jwriter.Writer{}
	event.MarshalTinyJSON(&w)
	sdk.Log(string(w.Buffer.BuildBytes()))
}

func emitRoyaltySet(nftContract string, royaltyBps uint64, royaltyRecipient string) {
	txID := sdk.GetEnvKey("tx.id")
	event := RoyaltySetEvent{
		Type: "royalty_set",
		Attributes: RoyaltySetAttributes{
			NftContract: nftContract, RoyaltyBps: royaltyBps, RoyaltyRecipient: royaltyRecipient,
		},
		Tx: *txID,
	}
	w := jwriter.Writer{}
	event.MarshalTinyJSON(&w)
	sdk.Log(string(w.Buffer.BuildBytes()))
}

func emitEmergencyWithdraw(tokenType, contract, tokenId, amount, to string) {
	txID := sdk.GetEnvKey("tx.id")
	event := EmergencyWithdrawEvent{
		Type: "emergency_withdraw",
		Attributes: EmergencyWithdrawAttributes{
			TokenType: tokenType, Contract: contract, TokenId: tokenId, Amount: amount, To: to,
		},
		Tx: *txID,
	}
	w := jwriter.Writer{}
	event.MarshalTinyJSON(&w)
	sdk.Log(string(w.Buffer.BuildBytes()))
}

func emitAuctionCreated(auctionId uint64, seller, nftContract, tokenId string, amount uint64, auctionType string, startPrice, endPrice string, startBlock, endBlock uint64) {
	txID := sdk.GetEnvKey("tx.id")
	event := AuctionCreatedEvent{
		Type: "auction_created",
		Attributes: AuctionCreatedAttributes{
			AuctionId: auctionId, Seller: seller, NftContract: nftContract, TokenId: tokenId,
			Amount: amount, AuctionType: auctionType, StartPrice: startPrice, EndPrice: endPrice,
			StartBlock: startBlock, EndBlock: endBlock,
		},
		Tx: *txID,
	}
	w := jwriter.Writer{}
	event.MarshalTinyJSON(&w)
	sdk.Log(string(w.Buffer.BuildBytes()))
}

func emitBidPlaced(auctionId uint64, bidder string, bidAmount string) {
	txID := sdk.GetEnvKey("tx.id")
	event := BidPlacedEvent{
		Type:       "bid_placed",
		Attributes: BidPlacedAttributes{AuctionId: auctionId, Bidder: bidder, BidAmount: bidAmount},
		Tx:         *txID,
	}
	w := jwriter.Writer{}
	event.MarshalTinyJSON(&w)
	sdk.Log(string(w.Buffer.BuildBytes()))
}

func emitAuctionSettled(auctionId uint64, winner string, finalPrice, fee, royalty string) {
	txID := sdk.GetEnvKey("tx.id")
	event := AuctionSettledEvent{
		Type: "auction_settled",
		Attributes: AuctionSettledAttributes{
			AuctionId: auctionId, Winner: winner, FinalPrice: finalPrice, Fee: fee, Royalty: royalty,
		},
		Tx: *txID,
	}
	w := jwriter.Writer{}
	event.MarshalTinyJSON(&w)
	sdk.Log(string(w.Buffer.BuildBytes()))
}

func emitAuctionCancelled(auctionId uint64, seller string) {
	txID := sdk.GetEnvKey("tx.id")
	event := AuctionCancelledEvent{
		Type:       "auction_cancelled",
		Attributes: AuctionCancelledAttributes{AuctionId: auctionId, Seller: seller},
		Tx:         *txID,
	}
	w := jwriter.Writer{}
	event.MarshalTinyJSON(&w)
	sdk.Log(string(w.Buffer.BuildBytes()))
}

func emitCollectionDenied(nftContract, by string) {
	txID := sdk.GetEnvKey("tx.id")
	event := CollectionDeniedEvent{
		Type:       "collectionDenied",
		Attributes: CollectionDeniedAttributes{NftContract: nftContract, By: by},
		Tx:         *txID,
	}
	w := jwriter.Writer{}
	event.MarshalTinyJSON(&w)
	sdk.Log(string(w.Buffer.BuildBytes()))
}

func emitCollectionAllowed(nftContract, by string) {
	txID := sdk.GetEnvKey("tx.id")
	event := CollectionAllowedEvent{
		Type:       "collectionAllowed",
		Attributes: CollectionAllowedAttributes{NftContract: nftContract, By: by},
		Tx:         *txID,
	}
	w := jwriter.Writer{}
	event.MarshalTinyJSON(&w)
	sdk.Log(string(w.Buffer.BuildBytes()))
}

func emitRoyaltySplitsSet(nftContract string, count uint64) {
	txID := sdk.GetEnvKey("tx.id")
	event := RoyaltySplitsSetEvent{
		Type:       "royaltySplitsSet",
		Attributes: RoyaltySplitsSetAttributes{NftContract: nftContract, Count: count},
		Tx:         *txID,
	}
	w := jwriter.Writer{}
	event.MarshalTinyJSON(&w)
	sdk.Log(string(w.Buffer.BuildBytes()))
}

func emitCollectionFeeSet(nftContract string, feeBps uint64) {
	txID := sdk.GetEnvKey("tx.id")
	event := CollectionFeeSetEvent{
		Type:       "collectionFeeSet",
		Attributes: CollectionFeeSetAttributes{NftContract: nftContract, FeeBps: feeBps},
		Tx:         *txID,
	}
	w := jwriter.Writer{}
	event.MarshalTinyJSON(&w)
	sdk.Log(string(w.Buffer.BuildBytes()))
}

func emitCollectionFeeCleared(nftContract string) {
	txID := sdk.GetEnvKey("tx.id")
	event := CollectionFeeClearedEvent{
		Type:       "collectionFeeCleared",
		Attributes: CollectionFeeClearedAttributes{NftContract: nftContract},
		Tx:         *txID,
	}
	w := jwriter.Writer{}
	event.MarshalTinyJSON(&w)
	sdk.Log(string(w.Buffer.BuildBytes()))
}

func emitSwept(buyer string, count uint64, total string) {
	txID := sdk.GetEnvKey("tx.id")
	event := SweptEvent{
		Type:       "swept",
		Attributes: SweptAttributes{Buyer: buyer, Count: count, Total: total},
		Tx:         *txID,
	}
	w := jwriter.Writer{}
	event.MarshalTinyJSON(&w)
	sdk.Log(string(w.Buffer.BuildBytes()))
}

func emitBundleListed(bundleId uint64, seller, nftContract string, count uint64, price string) {
	txID := sdk.GetEnvKey("tx.id")
	event := BundleListedEvent{
		Type: "bundle_listed",
		Attributes: BundleListedAttributes{
			BundleId: bundleId, Seller: seller, NftContract: nftContract, Count: count, Price: price,
		},
		Tx: *txID,
	}
	w := jwriter.Writer{}
	event.MarshalTinyJSON(&w)
	sdk.Log(string(w.Buffer.BuildBytes()))
}

func emitBundleBought(bundleId uint64, buyer, totalPrice, fee, royalty string) {
	txID := sdk.GetEnvKey("tx.id")
	event := BundleBoughtEvent{
		Type: "bundle_bought",
		Attributes: BundleBoughtAttributes{
			BundleId: bundleId, Buyer: buyer, TotalPrice: totalPrice, Fee: fee, Royalty: royalty,
		},
		Tx: *txID,
	}
	w := jwriter.Writer{}
	event.MarshalTinyJSON(&w)
	sdk.Log(string(w.Buffer.BuildBytes()))
}

func emitBundleDelisted(bundleId uint64, seller string) {
	txID := sdk.GetEnvKey("tx.id")
	event := BundleDelistedEvent{
		Type:       "bundle_delisted",
		Attributes: BundleDelistedAttributes{BundleId: bundleId, Seller: seller},
		Tx:         *txID,
	}
	w := jwriter.Writer{}
	event.MarshalTinyJSON(&w)
	sdk.Log(string(w.Buffer.BuildBytes()))
}

// ===================================
// D1: NFT-for-NFT Swap Events
// ===================================

func emitSwapProposed(swapId uint64, proposer, offeredNft, wantedNft string) {
	txID := sdk.GetEnvKey("tx.id")
	event := SwapProposedEvent{
		Type: "swapProposed",
		Attributes: SwapProposedAttributes{
			SwapId: swapId, Proposer: proposer, OfferedNft: offeredNft, WantedNft: wantedNft,
		},
		Tx: *txID,
	}
	w := jwriter.Writer{}
	event.MarshalTinyJSON(&w)
	sdk.Log(string(w.Buffer.BuildBytes()))
}

func emitSwapAccepted(swapId uint64, proposer, acceptor string) {
	txID := sdk.GetEnvKey("tx.id")
	event := SwapAcceptedEvent{
		Type: "swapAccepted",
		Attributes: SwapAcceptedAttributes{
			SwapId: swapId, Proposer: proposer, Acceptor: acceptor,
		},
		Tx: *txID,
	}
	w := jwriter.Writer{}
	event.MarshalTinyJSON(&w)
	sdk.Log(string(w.Buffer.BuildBytes()))
}

func emitSwapCancelled(swapId uint64, by string) {
	txID := sdk.GetEnvKey("tx.id")
	event := SwapCancelledEvent{
		Type:       "swapCancelled",
		Attributes: SwapCancelledAttributes{SwapId: swapId, By: by},
		Tx:         *txID,
	}
	w := jwriter.Writer{}
	event.MarshalTinyJSON(&w)
	sdk.Log(string(w.Buffer.BuildBytes()))
}

// ===================================
// E1: NFT Rental Events
// ===================================

func emitRentalListed(rentalId uint64, owner, nftContract, tokenId string) {
	txID := sdk.GetEnvKey("tx.id")
	event := RentalListedEvent{
		Type: "rentalListed",
		Attributes: RentalListedAttributes{
			RentalId: rentalId, Owner: owner, NftContract: nftContract, TokenId: tokenId,
		},
		Tx: *txID,
	}
	w := jwriter.Writer{}
	event.MarshalTinyJSON(&w)
	sdk.Log(string(w.Buffer.BuildBytes()))
}

func emitRented(rentalId uint64, renter string, until uint64) {
	txID := sdk.GetEnvKey("tx.id")
	event := RentedEvent{
		Type:       "rented",
		Attributes: RentedAttributes{RentalId: rentalId, Renter: renter, Until: until},
		Tx:         *txID,
	}
	w := jwriter.Writer{}
	event.MarshalTinyJSON(&w)
	sdk.Log(string(w.Buffer.BuildBytes()))
}

func emitRentalEnded(rentalId uint64, by string) {
	txID := sdk.GetEnvKey("tx.id")
	event := RentalEndedEvent{
		Type:       "rentalEnded",
		Attributes: RentalEndedAttributes{RentalId: rentalId, By: by},
		Tx:         *txID,
	}
	w := jwriter.Writer{}
	event.MarshalTinyJSON(&w)
	sdk.Log(string(w.Buffer.BuildBytes()))
}

func emitRentalDelisted(rentalId uint64, owner string) {
	txID := sdk.GetEnvKey("tx.id")
	event := RentalDelistedEvent{
		Type:       "rentalDelisted",
		Attributes: RentalDelistedAttributes{RentalId: rentalId, Owner: owner},
		Tx:         *txID,
	}
	w := jwriter.Writer{}
	event.MarshalTinyJSON(&w)
	sdk.Log(string(w.Buffer.BuildBytes()))
}
