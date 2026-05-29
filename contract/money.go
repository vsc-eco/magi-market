package main

import (
	"math/big"

	"magi_market/sdk"
)

// parseMoney parses a non-negative decimal integer string into a big.Int.
// Aborts on empty / sign / non-digit input.
func parseMoney(s string) *big.Int {
	if s == "" {
		sdk.Abort("amount required")
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			sdk.Abort("amount must be a non-negative integer string")
		}
	}
	v, ok := new(big.Int).SetString(s, 10)
	if !ok {
		sdk.Abort("invalid amount")
	}
	return v
}

func formatMoney(v *big.Int) string {
	if v == nil {
		return "0"
	}
	return v.String()
}

func mZero() *big.Int { return big.NewInt(0) }

func mAdd(a, b *big.Int) *big.Int { return new(big.Int).Add(a, b) }

// mSub aborts on underflow (mirrors safeSub semantics for money).
func mSub(a, b *big.Int) *big.Int {
	if a.Cmp(b) < 0 {
		sdk.Abort("money underflow")
	}
	return new(big.Int).Sub(a, b)
}

// mMulU64 multiplies a money value by an NFT quantity (uint64).
func mMulU64(price *big.Int, qty uint64) *big.Int {
	return new(big.Int).Mul(price, new(big.Int).SetUint64(qty))
}

// mMul is an exact big*big product — used for token-sale totals
// (pricePerUnit * buyAmount) where both operands are arbitrary-precision
// integer unit counts/prices (no rounding).
func mMul(a, b *big.Int) *big.Int {
	return new(big.Int).Mul(a, b)
}

// mMulBpsDiv returns floor(total * bps / 10000).
func mMulBpsDiv(total *big.Int, bps uint64) *big.Int {
	if bps == 0 {
		return big.NewInt(0)
	}
	r := new(big.Int).Mul(total, new(big.Int).SetUint64(bps))
	return r.Quo(r, big.NewInt(10000))
}

func mCmp(a, b *big.Int) int { return a.Cmp(b) }

func mIsZero(a *big.Int) bool { return a.Sign() == 0 }

func getMoneyState(key string) *big.Int {
	v := sdk.StateGetObject(key)
	if v == nil || *v == "" {
		return big.NewInt(0)
	}
	return parseMoney(*v)
}

func setMoneyState(key string, v *big.Int) {
	sdk.StateSetObject(key, formatMoney(v))
}

// Per-entity money helpers (keys match existing listingKey/offerKey/auctionKey).
func setListingMoney(id uint64, field string, v *big.Int) { setMoneyState(listingKey(id, field), v) }
func getListingMoney(id uint64, field string) *big.Int    { return getMoneyState(listingKey(id, field)) }
func setTokenListingMoney(id uint64, field string, v *big.Int) {
	setMoneyState(tokenListingKey(id, field), v)
}
func getTokenListingMoney(id uint64, field string) *big.Int {
	return getMoneyState(tokenListingKey(id, field))
}
func setOfferMoney(id uint64, field string, v *big.Int)   { setMoneyState(offerKey(id, field), v) }
func getOfferMoney(id uint64, field string) *big.Int      { return getMoneyState(offerKey(id, field)) }
func setAuctionMoney(id uint64, field string, v *big.Int) { setMoneyState(auctionKey(id, field), v) }
func getAuctionMoney(id uint64, field string) *big.Int    { return getMoneyState(auctionKey(id, field)) }
func setBundleMoney(id uint64, field string, v *big.Int)  { setMoneyState(bundleKey(id, field), v) }
func getBundleMoney(id uint64, field string) *big.Int     { return getMoneyState(bundleKey(id, field)) }
func setRentalMoney(id uint64, field string, v *big.Int)  { setMoneyState(rentalKey(id, field), v) }
func getRentalMoney(id uint64, field string) *big.Int     { return getMoneyState(rentalKey(id, field)) }
