package main

// ============================================================================
// Token-contract (ERC-20) fixed-price sale.
//
// Separate entrypoints from the NFT listing flow (zero risk to NFT paths).
// The listed asset is `Amount` of an ERC-20 `TokenContract` (big.Int decimal
// strings; no tokenId / collection / royalty / soulbound). Custody is
// approval-based, exactly like the NFT per-token-allowance path: the seller
// grants the market an ERC-20 allowance via
// `approve(spender=contract:<market>, amount >= listed)` on the token
// contract; `buyToken` pulls the asset with `transferFrom` (atomic — if the
// seller revoked/short, the whole tx reverts and the buyer is refunded, the
// same trust model `escrowIn` already relies on). Fee only (the global
// marketplace fee, snapshotted at list time); tokens have no royalty.
//
// State keys: `tl|<id>|<field>` (s, tc, a, p, pt, act, exp, sb, fb).
// Id counter: `nxt_tl`. Events: hand-built JSON via sdk.Log (no tinyjson).
// ============================================================================

import (
	"strconv"

	"magi_market/sdk"

	"github.com/CosmWasm/tinyjson/jlexer"
)

func tokenListingKey(id uint64, field string) string {
	return "tl|" + strconv.FormatUint(id, 10) + "|" + field
}
func setTokenListingField(id uint64, field, value string) {
	sdk.StateSetObject(tokenListingKey(id, field), value)
}
func getTokenListingField(id uint64, field string) string {
	return getStringState(tokenListingKey(id, field))
}
func setTokenListingUint64(id uint64, field string, val uint64) {
	setUint64State(tokenListingKey(id, field), val)
}
func getTokenListingUint64(id uint64, field string) uint64 {
	return getUint64State(tokenListingKey(id, field))
}
func isTokenListingActive(id uint64) bool {
	return getTokenListingField(id, "act") == "1"
}
func getNextTokenListingId() uint64 {
	return getUint64State("nxt_tl")
}
func setNextTokenListingId(id uint64) {
	setUint64State("nxt_tl", id)
}

func emitTokenEvent(kind, attrs string) {
	txID := sdk.GetEnvKey("tx.id")
	sdk.Log(`{"type":"` + kind + `","attributes":{` + attrs + `},"tx":"` + *txID + `"}`)
}

// doListToken validates + stores a token sale, returning the new id.
func doListToken(caller string, p *ListTokenPayload) uint64 {
	if p.TokenContract == "" || p.PaymentToken == "" {
		sdk.Abort("Token contract and payment token required")
	}
	if p.TokenContract == p.PaymentToken {
		sdk.Abort("Asset token must differ from payment token")
	}
	amount := parseMoney(p.Amount)
	if mIsZero(amount) {
		sdk.Abort("Amount must be greater than zero")
	}
	price := parseMoney(p.PricePerUnit)
	if mIsZero(price) {
		sdk.Abort("Price must be greater than zero")
	}

	assertPaymentTokenAllowed(p.PaymentToken)
	// Token-sale listings must respect the same collection denylist as NFT
	// listings — without this, an admin denying a misbehaving token can
	// still see fresh listings created against it.
	assertCollectionAllowed(p.TokenContract)

	// Preflight: seller must currently hold at least the listed amount.
	// The market's allowance is enforced atomically by transferFrom at buy
	// time (mirrors how escrowIn trusts the payment-token transferFrom).
	if mCmp(tokenBalanceOf(p.TokenContract, caller), amount) < 0 {
		sdk.Abort("Insufficient token balance to list")
	}

	feeBps := getFeeBps()
	if feeBps > 10000 {
		sdk.Abort("Fee must be <= 10000 basis points")
	}

	id := getNextTokenListingId()
	setTokenListingField(id, "s", caller)
	setTokenListingField(id, "tc", p.TokenContract)
	setTokenListingMoney(id, "a", amount)
	setTokenListingMoney(id, "p", price)
	setTokenListingField(id, "pt", p.PaymentToken)
	setTokenListingField(id, "act", "1")
	setTokenListingUint64(id, "exp", p.ExpirationBlock)
	setTokenListingUint64(id, "sb", p.StartBlock)
	setTokenListingUint64(id, "fb", feeBps)
	setNextTokenListingId(id + 1)

	emitTokenEvent("token_listed",
		`"listingId":`+strconv.FormatUint(id, 10)+
			`,"seller":"`+caller+`","tokenContract":"`+p.TokenContract+
			`","amount":"`+formatMoney(amount)+`","pricePerUnit":"`+formatMoney(price)+
			`","paymentToken":"`+p.PaymentToken+
			`","expirationBlock":`+strconv.FormatUint(p.ExpirationBlock, 10))
	return id
}

//go:wasmexport listToken
func ListToken(payload *string) *string {
	assertInit()
	assertNotPaused()
	caller := getCaller()
	if payload == nil || *payload == "" {
		sdk.Abort("Payload required")
	}
	var p ListTokenPayload
	r := jlexer.Lexer{Data: []byte(*payload)}
	p.UnmarshalTinyJSON(&r)
	if r.Error() != nil {
		sdk.Abort("Invalid payload")
	}
	id := doListToken(caller, &p)
	return jsonResponse(&CreatedResponse{Success: true, Id: id})
}

//go:wasmexport delistToken
func DelistToken(payload *string) *string {
	assertInit()
	// Allowed while paused (recovery path — no asset is escrowed).
	caller := getCaller()
	if payload == nil || *payload == "" {
		sdk.Abort("Payload required")
	}
	var p TokenListingIdPayload
	r := jlexer.Lexer{Data: []byte(*payload)}
	p.UnmarshalTinyJSON(&r)
	if r.Error() != nil {
		sdk.Abort("Invalid payload")
	}
	if !isTokenListingActive(p.ListingId) {
		sdk.Abort("Token listing not active")
	}
	seller := getTokenListingField(p.ListingId, "s")
	if caller != seller {
		sdk.Abort("Only seller can delist")
	}
	setTokenListingField(p.ListingId, "act", "0")
	emitTokenEvent("token_delisted",
		`"listingId":`+strconv.FormatUint(p.ListingId, 10)+`,"seller":"`+seller+`"`)
	return jsonResponse(&SuccessResponse{Success: true})
}

// doBuyToken: atomic token-for-token swap. Mirrors doBuy ordering — pull
// payment, then move the ASSET before paying the seller, so any failure
// reverts the whole tx (buyer fully refunded, no orphaned funds).
func doBuyToken(caller string, p *BuyTokenPayload) {
	if !isTokenListingActive(p.ListingId) {
		sdk.Abort("Token listing not active")
	}
	if isExpired(getTokenListingUint64(p.ListingId, "exp")) {
		sdk.Abort("Token listing has expired")
	}
	if sb := getTokenListingUint64(p.ListingId, "sb"); sb != 0 && getCurrentBlockHeight() < sb {
		sdk.Abort("Token listing not started")
	}
	buyAmt := parseMoney(p.Amount)
	if mIsZero(buyAmt) {
		sdk.Abort("Amount must be greater than zero")
	}
	remaining := getTokenListingMoney(p.ListingId, "a")
	if mCmp(buyAmt, remaining) > 0 {
		sdk.Abort("Insufficient listing amount")
	}

	seller := getTokenListingField(p.ListingId, "s")
	if caller == seller {
		sdk.Abort("Seller cannot buy own listing")
	}
	tokenContract := getTokenListingField(p.ListingId, "tc")
	paymentToken := getTokenListingField(p.ListingId, "pt")
	// Re-validate the payment token at buy-time (de-whitelisted-after-list halt).
	assertPaymentTokenAllowed(paymentToken)
	// Re-validate the listed token's collection allowlist (mirror NFT path).
	assertCollectionAllowed(tokenContract)
	price := getTokenListingMoney(p.ListingId, "p")
	feeBps := getTokenListingUint64(p.ListingId, "fb")

	total := mMul(price, buyAmt)

	// CEI: decrement remaining + flip active BEFORE the external calls.
	// Mirrors doBuy's fix — a malicious paymentToken's transferFrom
	// callback or a malicious tokenContract's transferFrom callback can
	// re-enter doBuyToken; without this ordering the outer's stale local
	// `remaining` would overwrite the inner's decrement and the listing's
	// `a` would silently drift, allowing over-sell / phantom inventory.
	newRemaining := mSub(remaining, buyAmt)
	setTokenListingMoney(p.ListingId, "a", newRemaining)
	if mIsZero(newRemaining) {
		setTokenListingField(p.ListingId, "act", "0")
	}

	received := escrowIn(paymentToken, caller, total)

	// Fee only — tokens have no collection/royalty.
	fee, _, sellerPayment := feeAndRoyaltyOf(received, feeBps, nil, nil)

	// Move the ASSET first (seller -> buyer) via the ERC-20 allowance the
	// seller granted the market. If short/revoked this aborts and the
	// whole tx (incl. the escrow leg) reverts.
	tokenTransferFromBig(tokenContract, seller, caller, buyAmt)

	if !mIsZero(fee) {
		tokenTransferBig(paymentToken, getFeeRecipient(), fee)
	}
	if !mIsZero(sellerPayment) {
		tokenTransferBig(paymentToken, seller, sellerPayment)
	}

	emitTokenEvent("token_bought",
		`"listingId":`+strconv.FormatUint(p.ListingId, 10)+
			`,"buyer":"`+caller+`","amount":"`+formatMoney(buyAmt)+
			`","totalPrice":"`+formatMoney(received)+`","fee":"`+formatMoney(fee)+`"`)
}

//go:wasmexport buyToken
func BuyToken(payload *string) *string {
	assertInit()
	assertNotPaused()
	caller := getCaller()
	if payload == nil || *payload == "" {
		sdk.Abort("Payload required")
	}
	var p BuyTokenPayload
	r := jlexer.Lexer{Data: []byte(*payload)}
	p.UnmarshalTinyJSON(&r)
	if r.Error() != nil {
		sdk.Abort("Invalid payload")
	}
	doBuyToken(caller, &p)
	return jsonResponse(&SuccessResponse{Success: true})
}

//go:wasmexport getTokenListing
func GetTokenListing(payload *string) *string {
	assertInit()
	if payload == nil || *payload == "" {
		sdk.Abort("Payload required")
	}
	var p TokenListingIdPayload
	r := jlexer.Lexer{Data: []byte(*payload)}
	p.UnmarshalTinyJSON(&r)
	if r.Error() != nil {
		sdk.Abort("Invalid payload")
	}
	seller := getTokenListingField(p.ListingId, "s")
	if seller == "" {
		sdk.Abort("Token listing not found")
	}
	return jsonResponse(&TokenListingResponse{
		ListingId:       p.ListingId,
		Seller:          seller,
		TokenContract:   getTokenListingField(p.ListingId, "tc"),
		Amount:          formatMoney(getTokenListingMoney(p.ListingId, "a")),
		PricePerUnit:    formatMoney(getTokenListingMoney(p.ListingId, "p")),
		PaymentToken:    getTokenListingField(p.ListingId, "pt"),
		Active:          isTokenListingActive(p.ListingId),
		ExpirationBlock: getTokenListingUint64(p.ListingId, "exp"),
		StartBlock:      getTokenListingUint64(p.ListingId, "sb"),
		FeeBps:          getTokenListingUint64(p.ListingId, "fb"),
	})
}
