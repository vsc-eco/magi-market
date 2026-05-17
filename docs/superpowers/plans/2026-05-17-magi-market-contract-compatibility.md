# magi-market Latest NFT/Token + UTXO Payment Compatibility — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `magi-market` correct against the latest `magi_nft-contract` (operator-approval custody for listings/offers), the latest `magi_token-contract` (big.Int decimal-string amounts), and any `utxo-mapping` contract as a payment token, with balance-delta payment accounting.

**Architecture:** Layered-in-place. A new `contract/money.go` holds all `math/big` logic (parse/format/add/sub/mul/bps-split + money state helpers). Monetary JSON fields become quoted decimal strings; NFT quantities stay `uint64`. Listings/offers drop NFT escrow and use `isApprovedForAll` + `safeTransferFrom`-on-sale; auctions keep escrow unchanged except amount typing. Every inbound payment transfer measures actual received via `tokenBalanceOf` before/after and distributes that. Spec: `docs/superpowers/specs/2026-05-17-magi-market-contract-compatibility-design.md`.

**Tech Stack:** Go (tinygo `wasm-unknown`), `github.com/CosmWasm/tinyjson` (codegen via `/root/go/bin/tinyjson`), `math/big`, vsc-node wasm test harness (`vsc-node/lib/test_utils`).

**Test strategy:** Integration via the wasm harness only. The `contract` Go package is built solely by tinygo (its `sdk` uses `//go:wasmimport`), so host-side `go test ./contract/...` is not viable and no pure-unit rig is introduced — matching the existing suite, which is entirely harness-driven. Each behavioral task: add/adjust a harness test → build wasm → run → green → commit.

**Build/run invariants (every build & test step assumes these):**
- Always export `GOTOOLCHAIN=go1.25.3` (installed tinygo 0.39.0 rejects the host go1.26; this re-execs go1.25.3 which is already downloaded).
- A gitignored `go.work` at repo root pins `vsc-node` to the local checkout:
  ```
  go 1.24.0

  use .

  replace vsc-node => /home/dockeruser/magi/testnet/go-vsc-node
  ```
  `go.work` / `go.work.sum` are in `.gitignore` (added in Task 0) and MUST NOT be committed.
- WASM build command (per contract repo):
  ```
  GOTOOLCHAIN=go1.25.3 tinygo build -gc=custom -scheduler=none -panic=trap -no-debug -target=wasm-unknown -o <out> ./contract
  ```
- tinyjson regen command (regenerates `contract/types_tinyjson.go` from struct tags):
  ```
  /root/go/bin/tinyjson -all contract/types.go
  ```
- Test run:
  ```
  GOTOOLCHAIN=go1.25.3 go test ./test/ -count=1
  ```

---

## Task 0: Establish build pipeline + green test baseline

**Why:** The cloned repo cannot build or test as-is: broken `replace` path, missing wasm artifacts, and a `test/helpers_test.go` written against an older `ct.Call` API (3 returns) than the available go-vsc-node (single `ContractTestCallResult` struct). This task makes `go test ./test/` green BEFORE any feature change, so later regressions are attributable.

**Files:**
- Create: `go.work` (gitignored), `test/artifacts/{main,token,nft}.wasm` (gitignored)
- Modify: `.gitignore`, `test/helpers_test.go`

- [ ] **Step 1: Create gitignored `go.work`**

```bash
cd /home/dockeruser/magi/magi-market
printf 'go 1.24.0\n\nuse .\n\nreplace vsc-node => /home/dockeruser/magi/testnet/go-vsc-node\n' > go.work
grep -qxF 'go.work' .gitignore || printf 'go.work\ngo.work.sum\n' >> .gitignore
git check-ignore go.work && echo "go.work ignored OK"
```
Expected: prints `go.work` then `go.work ignored OK`.

- [ ] **Step 2: Build the three wasm artifacts from the latest contracts**

```bash
cd /home/dockeruser/magi/magi-market
export GOTOOLCHAIN=go1.25.3
B='-gc=custom -scheduler=none -panic=trap -no-debug -target=wasm-unknown'
A=/home/dockeruser/magi/magi-market/test/artifacts
mkdir -p "$A"
tinygo build $B -o "$A/main.wasm" ./contract
( cd /mnt/HC_Volume_105012347/magi/magi_nft-contract && tinygo build $B -o "$A/nft.wasm" ./contract )
( cd /mnt/HC_Volume_105012347/magi/testnet/magi_token-contract && tinygo build $B -o "$A/token.wasm" ./contract )
ls -la "$A"
```
Expected: `main.wasm`, `nft.wasm`, `token.wasm` all present and non-zero (≈58–70 KB each).

- [ ] **Step 3: Run the suite to observe the pre-existing harness compile failure**

Run: `GOTOOLCHAIN=go1.25.3 go test ./test/ -count=1 2>&1 | tail -5`
Expected: FAIL — `test/helpers_test.go:125:27: assignment mismatch: 3 variables but ct.Call returns 1 value`.

- [ ] **Step 4: Adapt `test/helpers_test.go` to the current `ContractTestCallResult` API**

The current API (`/home/dockeruser/magi/testnet/go-vsc-node/lib/test_utils/contract_test_utils.go`):
```go
func (ct *ContractTest) Call(tx stateEngine.TxVscCallContract) ContractTestCallResult
type ContractTestCallResult struct {
    Success   bool
    Err       contracts.ContractOutputError
    ErrMsg    string
    Ret       string
    RcUsed    int64
    GasUsed   uint
    Logs      map[string]contract_session.LogOutput
    StateDiff map[string]contract_session.StateDiff
}
```

4a. Add the log-package import. In the import block of `test/helpers_test.go`, add after the `stateEngine "vsc-node/modules/state-processing"` line:
```go
	contract_session "vsc-node/modules/contract/session"
```

4b. Replace the `callContract` body's call + return wiring. Change:
```go
	result, gasUsed, logs := ct.Call(stateEngine.TxVscCallContract{
```
to:
```go
	cr := ct.Call(stateEngine.TxVscCallContract{
```
and inside `callContract`, after the `})` that closes the `ct.Call(...)` argument, replace the subsequent references so the body reads:
```go
	PrintLogs(cr.Logs)
	PrintErrorIfFailed(cr)
	fmt.Printf("return msg: %s\n", cr.Ret)
	fmt.Printf("RC used: %d\n", cr.RcUsed)
	fmt.Printf("gas used: %d\n", cr.GasUsed)

	assert.LessOrEqual(t, cr.GasUsed, maxGas, fmt.Sprintf("Gas %d exceeded limit %d", cr.GasUsed, maxGas))

	if expectedResult {
		assert.True(t, cr.Success, "Contract action failed with "+cr.Ret)
	} else {
		assert.False(t, cr.Success, "Contract action did not fail (as expected)")
	}
	if expectedOutput != "" {
		assert.True(t, strings.Contains(cr.Ret, expectedOutput), fmt.Sprintf("Expected output to contain %q but got %q", expectedOutput, cr.Ret))
	}
	return cr, cr.GasUsed, cr.Logs
}
```

4c. Change the four return signatures `(stateEngine.TxResult, uint, map[string][]string)` → `(test_utils.ContractTestCallResult, uint, map[string]contract_session.LogOutput)`. They are: `callContract` (the definition at the `func callContract(` line) and the three wrappers whose bodies are `return callContract(t, ct, MarketContractID, ...)`, `... TokenID ...`, `... NftContractID ...`.

4d. Replace the two print helpers:
```go
func PrintLogs(logs map[string]contract_session.LogOutput) {
	for key, v := range logs {
		fmt.Printf("[%s] %+v\n", key, v)
	}
}

func PrintErrorIfFailed(result test_utils.ContractTestCallResult) {
	if !result.Success {
		fmt.Println(result.ErrMsg)
	}
}
```

- [ ] **Step 5: Confirm the harness compiles and the baseline is green**

Run: `GOTOOLCHAIN=go1.25.3 go test ./test/ -count=1 2>&1 | tail -15`
Expected: build succeeds; existing suites (basic, listing, offer, auction, royalty, expiration, edge_cases, features, collection_offer, review*, review2, review3*) report `ok  	magi_market/test`. If any pre-existing test fails for a non-harness reason, STOP and report — do not start feature work on a red baseline.

- [ ] **Step 6: Commit the baseline (repo files only; never `go.work`)**

```bash
cd /home/dockeruser/magi/magi-market
git add .gitignore test/helpers_test.go
git commit -m "test: adapt harness to current go-vsc-node ContractTestCallResult API

The cloned repo's helpers_test.go targeted an older ct.Call signature
(3 returns); all available go-vsc-node checkouts return a single
ContractTestCallResult. Rewire the sole call site, four return
signatures, and the two print helpers. Gitignore go.work so the
local vsc-node path override is never committed.

Pre-existing breakage in the clone, unrelated to feature work;
required to obtain a green baseline before changes."
```
Expected: commit succeeds; `git status` shows no tracked `go.work`.

---

## Task 0b: Correct the stale batch-rollback test (pre-existing, folded in)

**Why:** `TestBatchBuyOneInvalidAborts` asserts the VSC runtime does NOT roll back a failed call (expects a listing partially consumed). The current go-vsc-node fully rolls back on abort, so the listing stays whole. The test's premise is outdated AND contradicts the atomicity our approval + balance-delta design relies on (a failed `buy` MUST revert its escrow leg). Fixing the test to assert full-rollback aligns it with both runtime reality and our design. (`TestBuyerCannotAcceptOwnOffer`, the other pre-existing failure, is fixed in Task 3 via a `caller == buyer` guard — see that task.)

**Files:** Modify `test/review_fixes_test.go`

- [ ] **Step 1: Rewrite the assertion and comment in `TestBatchBuyOneInvalidAborts`**

Replace the misleading NOTE block and assertion (currently lines ~381–386) so it reads:
```go
	// The VSC runtime rolls back ALL state mutations of a failed call: when
	// the second batch item aborts, the first item's buy is reverted too.
	// batchBuy is therefore atomic (all-or-nothing), and the marketplace's
	// escrow/approval flows depend on this guarantee.
	listing, _, _ := CallMarket(t, ct, "getListing", []byte(`{"listingId":0}`), nil, "hive:anyone", "", true, gas, "")
	l := ParseListing(listing)
	assert.Equal(t, uint64(5), l.Amount) // full rollback: nothing consumed
```

- [ ] **Step 2: Run the full suite — expect 244/245 (only `TestBuyerCannotAcceptOwnOffer` still red, fixed in Task 3)**

Run: `GOTOOLCHAIN=go1.25.3 go test ./test/ -count=1 2>&1 | tail -8`
Expected: only `TestBuyerCannotAcceptOwnOffer` fails; everything else green. If anything else is red, STOP and report.

- [ ] **Step 3: Commit**

```bash
cd /home/dockeruser/magi/magi-market
git add test/review_fixes_test.go
git commit -m "test: assert batchBuy is atomic (full rollback) per current runtime

TestBatchBuyOneInvalidAborts encoded a stale assumption that the VSC
runtime does not roll back failed calls. It does roll back fully;
batchBuy is all-or-nothing. The approval + balance-delta design relies
on this atomicity. Pre-existing failure, folded into the plan."
```

---

## Task 1: `money.go` primitives + cross-contract helper upgrades (additive, no behavior change)

**Why:** Foundation for every later task. Pure additions — existing flows untouched — so the suite stays green and the diff is reviewable in isolation.

**Files:**
- Create: `contract/money.go`
- Modify: `contract/internal.go` (add helpers; do NOT yet change existing call sites)

- [ ] **Step 1: Create `contract/money.go`**

```go
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
func setOfferMoney(id uint64, field string, v *big.Int)    { setMoneyState(offerKey(id, field), v) }
func getOfferMoney(id uint64, field string) *big.Int       { return getMoneyState(offerKey(id, field)) }
func setAuctionMoney(id uint64, field string, v *big.Int)  { setMoneyState(auctionKey(id, field), v) }
func getAuctionMoney(id uint64, field string) *big.Int     { return getMoneyState(auctionKey(id, field)) }
```

- [ ] **Step 2: Add cross-contract helpers to `contract/internal.go`**

Append these functions to `contract/internal.go` (do not remove or alter existing ones yet):

```go
import "math/big" // add to the existing import block if not present

// jsonBoolField returns true iff "<field>": true appears in s.
func jsonBoolField(s, field string) bool {
	key := `"` + field + `":`
	idx := -1
	for i := 0; i <= len(s)-len(key); i++ {
		if s[i:i+len(key)] == key {
			idx = i + len(key)
			break
		}
	}
	if idx < 0 {
		return false
	}
	for idx < len(s) && (s[idx] == ' ' || s[idx] == '\t' || s[idx] == '\n') {
		idx++
	}
	return idx+4 <= len(s) && s[idx:idx+4] == "true"
}

// nftIsApprovedForAll checks operator approval on the NFT contract.
func nftIsApprovedForAll(nftContract, account, operator string) bool {
	payload := `{"account":"` + account + `","operator":"` + operator + `"}`
	result := sdk.ContractCallSimple(nftContract, "isApprovedForAll", payload)
	if result == nil {
		return false
	}
	return jsonBoolField(*result, "approved")
}

// tokenBalanceOf reads an ERC-20-style balance as big.Int (string or numeric JSON).
func tokenBalanceOf(tokenContract, account string) *big.Int {
	payload := `{"account":"` + account + `"}`
	result := sdk.ContractCallSimple(tokenContract, "balanceOf", payload)
	if result == nil {
		return big.NewInt(0)
	}
	s := *result
	key := `"balance":`
	idx := -1
	for i := 0; i <= len(s)-len(key); i++ {
		if s[i:i+len(key)] == key {
			idx = i + len(key)
			break
		}
	}
	if idx < 0 {
		return big.NewInt(0)
	}
	for idx < len(s) && (s[idx] == ' ' || s[idx] == '\t' || s[idx] == '\n') {
		idx++
	}
	if idx < len(s) && s[idx] == '"' {
		idx++
	}
	end := idx
	for end < len(s) && s[end] >= '0' && s[end] <= '9' {
		end++
	}
	if end == idx {
		return big.NewInt(0)
	}
	v, ok := new(big.Int).SetString(s[idx:end], 10)
	if !ok {
		return big.NewInt(0)
	}
	return v
}

// escrowIn pulls `requested` from payer into the marketplace and returns the
// ACTUAL amount received (balance-delta), robust to fee-on-transfer / utxo deduct_fee.
func escrowIn(paymentToken, payer string, requested *big.Int) *big.Int {
	contractAddr := getContractAddress()
	before := tokenBalanceOf(paymentToken, contractAddr)
	tokenTransferFromBig(paymentToken, payer, contractAddr, requested)
	after := tokenBalanceOf(paymentToken, contractAddr)
	received := mSub(after, before)
	if mIsZero(received) {
		sdk.Abort("no payment received")
	}
	return received
}

// big.Int variants of the token call helpers (added now, swapped in later tasks).
func tokenTransferFromBig(tokenContract, from, to string, amount *big.Int) {
	payload := `{"from":"` + from + `","to":"` + to + `","amount":"` + formatMoney(amount) + `"}`
	if sdk.ContractCallSimple(tokenContract, "transferFrom", payload) == nil {
		sdk.Abort("transferFrom call failed")
	}
}

func tokenTransferBig(tokenContract, to string, amount *big.Int) {
	payload := `{"to":"` + to + `","amount":"` + formatMoney(amount) + `"}`
	if sdk.ContractCallSimple(tokenContract, "transfer", payload) == nil {
		sdk.Abort("transfer call failed")
	}
}
```

Note: the existing crude `nftIsSoulbound` substring scan is replaced in Task 2 (where the listing path is rewritten) with `jsonBoolField(*result, "soulbound")`. Leave it untouched in this task.

- [ ] **Step 3: Build wasm to prove it compiles**

Run:
```bash
cd /home/dockeruser/magi/magi-market && GOTOOLCHAIN=go1.25.3 tinygo build -gc=custom -scheduler=none -panic=trap -no-debug -target=wasm-unknown -o test/artifacts/main.wasm ./contract && echo BUILD_OK
```
Expected: `BUILD_OK` (Go would flag unused funcs only for locals, not package-level — these are fine unused).

- [ ] **Step 4: Run full suite — must still be green (no behavior change)**

Run: `GOTOOLCHAIN=go1.25.3 go test ./test/ -count=1 2>&1 | tail -5`
Expected: `ok  	magi_market/test`.

- [ ] **Step 5: Commit**

```bash
cd /home/dockeruser/magi/magi-market
git add contract/money.go contract/internal.go
git commit -m "feat: add big.Int money primitives and cross-contract helpers

- contract/money.go: parse/format/add/sub/mulU64/mulBpsDiv/cmp/zero
  plus money state + per-entity helpers
- internal.go: jsonBoolField, nftIsApprovedForAll, tokenBalanceOf,
  escrowIn (balance-delta), big.Int token call helpers

Additive only; existing flows and the test suite are unchanged."
```

---

## Task 2: Listings + Buy — approval custody, big.Int price, balance-delta

**Why:** First behavioral migration. NFT stays with seller (operator approval); price is a decimal string; payment distribution uses actually-received funds.

**Files:**
- Modify: `contract/types.go` (string price fields), `contract/internal.go` (`nftIsSoulbound` parse, retire old `distributeFees`/`calculateFee*`), `contract/market.go` (`doList`, `Delist`, `doBuy`, `UpdateListing`, `GetListing`), `contract/events.go` (string amounts in listed/bought/updated), `contract/types_tinyjson.go` (regenerated)
- Modify: `test/listing_test.go`, `test/basic_test.go`, `test/features_test.go`, `test/edge_cases_test.go` (string prices + operator-approval setup) — adjust only the listing/buy/delist/update cases; offer & auction cases are migrated in Tasks 3–4

- [ ] **Step 1: Change listing/buy money fields to string in `contract/types.go`**

Apply these exact field-type changes (uint64 → string), leaving NFT `Amount`, ids, blocks, and `*Bps` as-is:
- `ListPayload.PricePerUnit` → `string`
- `UpdateListingPayload.NewPrice` → `string`
- `ListingResponse.PricePerUnit` → `string`
- `ListedAttributes.PricePerUnit` → `string`
- `BoughtAttributes.TotalPrice`, `.Fee`, `.Royalty` → `string`
- `ListingUpdatedAttributes.NewPrice` → `string`

(JSON tags stay identical; tinyjson emits string fields as quoted JSON, no float/WASI.)

- [ ] **Step 2: Regenerate tinyjson and build (expect contract compile errors — that's the failing state)**

Run:
```bash
cd /home/dockeruser/magi/magi-market
/root/go/bin/tinyjson -all contract/types.go
GOTOOLCHAIN=go1.25.3 tinygo build -gc=custom -scheduler=none -panic=trap -no-debug -target=wasm-unknown -o test/artifacts/main.wasm ./contract 2>&1 | tail -10
```
Expected: tinyjson regenerates `contract/types_tinyjson.go`; tinygo FAILS with type errors in `market.go`/`events.go` where `PricePerUnit`/`TotalPrice` etc. are used as uint64. This is expected — fixed in Steps 3–5.

- [ ] **Step 3: Rewrite the listing path in `contract/market.go`**

Replace `doList` with (operator-approval, no NFT escrow, big.Int price):

```go
func doList(caller string, p *ListPayload) uint64 {
	if p.NftContract == "" || p.TokenId == "" || p.PaymentToken == "" {
		sdk.Abort("NFT contract, token ID, and payment token required")
	}
	if p.Amount == 0 {
		sdk.Abort("Amount must be greater than zero")
	}
	price := parseMoney(p.PricePerUnit)
	if mIsZero(price) {
		sdk.Abort("Price must be greater than zero")
	}

	assertPaymentTokenAllowed(p.PaymentToken)

	if nftIsSoulbound(p.NftContract, p.TokenId) {
		sdk.Abort("Cannot list soulbound tokens")
	}

	currentFeeBps := getFeeBps()
	currentRoyaltyBps := getRoyaltyBps(p.NftContract)
	if currentFeeBps+currentRoyaltyBps > 10000 {
		sdk.Abort("Combined fee and royalty exceed 100%")
	}

	// Approval model: NFT stays with seller; market must be an approved operator
	// and the seller must currently hold enough balance.
	contractAddr := getContractAddress()
	if !nftIsApprovedForAll(p.NftContract, caller, contractAddr) {
		sdk.Abort("Marketplace not approved as operator for this NFT collection")
	}
	if nftBalanceOf(p.NftContract, caller, p.TokenId) < p.Amount {
		sdk.Abort("Insufficient NFT balance to list")
	}

	id := getNextListingId()
	setListingField(id, "s", caller)
	setListingField(id, "nc", p.NftContract)
	setListingField(id, "ti", p.TokenId)
	setListingUint64(id, "a", p.Amount)
	setListingMoney(id, "p", price)
	setListingField(id, "pt", p.PaymentToken)
	setListingField(id, "act", "1")
	setListingUint64(id, "exp", p.ExpirationBlock)
	setListingUint64(id, "fb", currentFeeBps)
	setListingUint64(id, "rb", currentRoyaltyBps)
	setListingField(id, "rr", getRoyaltyRecipient(p.NftContract))
	setNextListingId(id + 1)

	emitListed(id, caller, p.NftContract, p.TokenId, p.Amount, formatMoney(price), p.PaymentToken, p.ExpirationBlock)
	return id
}
```

- [ ] **Step 4: Rewrite `Delist`, `doBuy`, `UpdateListing`, `GetListing` in `contract/market.go`**

`Delist` — remove the escrowed-NFT return (nothing is escrowed now). Delete this block from `Delist`:
```go
	// Return escrowed NFT to seller
	nftContract := getListingField(p.ListingId, "nc")
	tokenId := getListingField(p.ListingId, "ti")
	amount := getListingUint64(p.ListingId, "a")
	contractAddr := getContractAddress()
	nftSafeTransferFrom(nftContract, contractAddr, seller, tokenId, amount)
```
so `Delist` only verifies seller, sets `"act"` to `"0"`, and emits `emitDelisted`.

Replace `doBuy` with:
```go
func doBuy(caller string, p *BuyPayload) {
	if !isListingActive(p.ListingId) {
		sdk.Abort("Listing not active")
	}
	if isExpired(getListingUint64(p.ListingId, "exp")) {
		sdk.Abort("Listing has expired")
	}
	if p.Amount == 0 {
		sdk.Abort("Amount must be greater than zero")
	}
	remaining := getListingUint64(p.ListingId, "a")
	if p.Amount > remaining {
		sdk.Abort("Insufficient listing amount")
	}

	seller := getListingField(p.ListingId, "s")
	if caller == seller {
		sdk.Abort("Seller cannot buy own listing")
	}
	paymentToken := getListingField(p.ListingId, "pt")
	pricePerUnit := getListingMoney(p.ListingId, "p")
	nftContract := getListingField(p.ListingId, "nc")
	tokenId := getListingField(p.ListingId, "ti")
	lockedFeeBps := getListingUint64(p.ListingId, "fb")
	lockedRoyaltyBps := getListingUint64(p.ListingId, "rb")
	royaltyRecipient := getListingField(p.ListingId, "rr")

	totalCost := mMulU64(pricePerUnit, p.Amount)

	// Escrow payment (balance-delta: distribute what we actually received).
	received := escrowIn(paymentToken, caller, totalCost)
	fee, royalty, sellerPayment := distributeFees(received, lockedFeeBps, lockedRoyaltyBps)

	// Transfer NFT from seller -> buyer using operator approval. If the seller
	// moved/burned the NFT or revoked approval, this aborts and the whole tx
	// (including the escrow leg) reverts — no orphaned funds.
	nftSafeTransferFrom(nftContract, seller, caller, tokenId, p.Amount)

	if !mIsZero(fee) {
		tokenTransferBig(paymentToken, getFeeRecipient(), fee)
	}
	if !mIsZero(royalty) && royaltyRecipient != "" {
		tokenTransferBig(paymentToken, royaltyRecipient, royalty)
	}
	if !mIsZero(sellerPayment) {
		tokenTransferBig(paymentToken, seller, sellerPayment)
	}

	newRemaining := safeSub(remaining, p.Amount)
	if newRemaining == 0 {
		setListingField(p.ListingId, "act", "0")
	}
	setListingUint64(p.ListingId, "a", newRemaining)

	emitBought(p.ListingId, caller, p.Amount, formatMoney(received), formatMoney(fee), formatMoney(royalty))
}
```

In `UpdateListing`, replace the price handling:
```go
	newPrice := parseMoney(p.NewPrice)
	if mIsZero(newPrice) {
		sdk.Abort("Price must be greater than zero")
	}
	setListingMoney(p.ListingId, "p", newPrice)
	emitListingUpdated(p.ListingId, formatMoney(newPrice))
```

In `GetListing`, change the response field:
```go
		PricePerUnit:    formatMoney(getListingMoney(p.ListingId, "p")),
```

- [ ] **Step 5: Update `contract/internal.go` and `contract/events.go`**

5a. In `internal.go`, replace `nftIsSoulbound`'s parsing body with `return jsonBoolField(*result, "soulbound")` (drop the for-loop substring scan). Delete the now-unused uint64 fee functions `calculateFeeWithBps`, `calculateFee`, and the old uint64 `distributeFees`; add the big.Int `distributeFees`:
```go
// distributeFees splits totalPrice into (fee, royalty, sellerPayment).
func distributeFees(totalPrice *big.Int, lockedFeeBps, lockedRoyaltyBps uint64) (*big.Int, *big.Int, *big.Int) {
	fee := mMulBpsDiv(totalPrice, lockedFeeBps)
	royalty := mMulBpsDiv(totalPrice, lockedRoyaltyBps)
	sellerPayment := mSub(mSub(totalPrice, fee), royalty)
	return fee, royalty, sellerPayment
}
```
Leave `safeAdd/safeSub/safeMul` (still used for uint64 NFT quantities). The old uint64 `tokenTransfer`/`tokenTransferFrom` remain for now (Tasks 3–4 still reference them until migrated); buy uses the `*Big` variants.

5b. In `events.go`, change `emitListed`, `emitBought`, `emitListingUpdated` signatures so the monetary parameters are `string` and assign directly into the (now string) attribute fields. Example for `emitBought`:
```go
func emitBought(listingId uint64, buyer string, amount uint64, totalPrice, fee, royalty string) {
	// ... build BoughtEvent with BoughtAttributes{ ..., TotalPrice: totalPrice, Fee: fee, Royalty: royalty }
}
```
Mirror for `emitListed` (`pricePerUnit string`) and `emitListingUpdated` (`newPrice string`).

- [ ] **Step 6: Regenerate tinyjson + build green**

Run:
```bash
cd /home/dockeruser/magi/magi-market
/root/go/bin/tinyjson -all contract/types.go
GOTOOLCHAIN=go1.25.3 tinygo build -gc=custom -scheduler=none -panic=trap -no-debug -target=wasm-unknown -o test/artifacts/main.wasm ./contract && echo BUILD_OK
```
Expected: `BUILD_OK`.

- [ ] **Step 7: Update listing/buy tests to string prices + approval setup**

In the listing/buy/delist/update cases of `test/listing_test.go`, `test/basic_test.go`, `test/features_test.go`, `test/edge_cases_test.go`:
- Change every `"pricePerUnit": <n>` / `"newPrice": <n>` in payloads to quoted strings (e.g. `"pricePerUnit":"100"`), and every expected JSON-output numeric `totalPrice`/`fee`/`royalty`/`pricePerUnit` to quoted strings.
- Before each `list`/`batchList`, have the seller approve the market as operator on the NFT contract (mirrors how existing tests already mint NFTs). Insert a `callNft` call:
  ```go
  callNft(t, ct, "setApprovalForAll",
      json.RawMessage(`{"operator":"`+MarketContractAddress+`","approved":true}`),
      nil, sellerAddr, true, gas, "")
  ```
- Remove assertions that the NFT is escrowed in the market after listing; replace with: after `list`, `nft balanceOf(seller,id)` is unchanged; after `buy`, `nft balanceOf(buyer,id)` increased and `balanceOf(seller,id)` decreased.
- For `delist`: assert no NFT movement (seller balance unchanged through list→delist).

- [ ] **Step 8: Run targeted then full suite**

Run: `GOTOOLCHAIN=go1.25.3 go test ./test/ -count=1 -run 'Listing|Basic|Feature|Edge' -v 2>&1 | tail -20`
Expected: listed/buy/delist/update tests PASS.
Then full: `GOTOOLCHAIN=go1.25.3 go test ./test/ -count=1 2>&1 | tail -8`
Expected: offer/auction suites may now fail to build/compile because they share `types.go` structs not yet migrated — that is acceptable ONLY if the failure is confined to offer/auction/collection tests. If listing/buy/basic regressed, STOP and fix before proceeding.

- [ ] **Step 9: Commit**

```bash
cd /home/dockeruser/magi/magi-market
git add contract/ test/listing_test.go test/basic_test.go test/features_test.go test/edge_cases_test.go
git commit -m "feat: listings/buy use operator-approval custody + big.Int price

- NFT no longer escrowed on list; market verifies isApprovedForAll +
  seller balance; safeTransferFrom seller->buyer only on sale
- pricePerUnit/newPrice and bought/listed/updated event amounts are
  big.Int decimal strings (regenerated types_tinyjson.go)
- buy distributes balance-delta received amount, not requested
- delist no longer moves NFTs"
```

---

## Task 3: Offers — big.Int, balance-delta escrow, accept-time approval preflight

**Why:** Offers already escrow payment (not NFT) and `acceptOffer` already does seller→buyer transfer; migrate amounts to big.Int, make escrow balance-delta, and add a clean approval/balance preflight on accept.

**Files:**
- Modify: `contract/types.go` (offer string fields), `contract/market.go` (`MakeOffer`, `CancelOffer`, `doAcceptOffer`, `GetOffer`), `contract/events.go` (offer events string), `contract/types_tinyjson.go` (regen)
- Modify: `test/offer_test.go`, `test/collection_offer_test.go`

- [ ] **Step 1: String-type the offer money fields in `types.go`**

uint64 → string: `MakeOfferPayload.PricePerUnit`, `OfferResponse.PricePerUnit`, `OfferMadeAttributes.PricePerUnit`, `OfferAcceptedAttributes.TotalPrice`, `.Fee`, `.Royalty`. (`SetMinOfferPayload.MinOffer` is migrated in Task 5.)

- [ ] **Step 2: Rewrite `MakeOffer` payment + storage (`contract/market.go`)**

Replace the amount/threshold/escrow section of `MakeOffer` with:
```go
	price := parseMoney(p.PricePerUnit)
	if mIsZero(price) {
		sdk.Abort("Price must be greater than zero")
	}
	if p.Amount == 0 {
		sdk.Abort("Amount must be greater than zero")
	}
	assertPaymentTokenAllowed(p.PaymentToken)

	totalOffer := mMulU64(price, p.Amount)
	minOffer := getMoneyState("min_ofr")
	if !mIsZero(minOffer) && mCmp(totalOffer, minOffer) < 0 {
		sdk.Abort("Offer below minimum threshold")
	}

	currentFeeBps := getFeeBps()
	currentRoyaltyBps := getRoyaltyBps(p.NftContract)
	if currentFeeBps+currentRoyaltyBps > 10000 {
		sdk.Abort("Combined fee and royalty exceed 100%")
	}

	// Escrow payment with balance-delta; store the ACTUAL received total so
	// cancel refunds and accept payouts can never over-distribute.
	received := escrowIn(p.PaymentToken, caller, totalOffer)
```
Then store `setOfferMoney(id, "p", price)` and additionally `setOfferMoney(id, "esc", received)` (escrowed total actually held). Keep all other `setOfferField` calls. Emit `emitOfferMade(... , formatMoney(price), ...)`.

- [ ] **Step 3: Rewrite `CancelOffer` refund (`contract/market.go`)**

Replace the refund computation with the stored escrow:
```go
	buyer := getOfferField(p.OfferId, "b")
	expBlock := getOfferUint64(p.OfferId, "exp")
	if !isExpired(expBlock) && caller != buyer {
		sdk.Abort("Only buyer can cancel offer")
	}
	paymentToken := getOfferField(p.OfferId, "pt")
	refund := getOfferMoney(p.OfferId, "esc")
	if !mIsZero(refund) {
		tokenTransferBig(paymentToken, buyer, refund)
	}
	setOfferField(p.OfferId, "act", "0")
	emitOfferCancelled(p.OfferId, buyer)
```

- [ ] **Step 4: Rewrite `doAcceptOffer` (`contract/market.go`)**

```go
func doAcceptOffer(caller string, offerId uint64, acceptAmount uint64, tokenId string) {
	if !isOfferActive(offerId) {
		sdk.Abort("Offer not active")
	}
	if isExpired(getOfferUint64(offerId, "exp")) {
		sdk.Abort("Offer has expired")
	}

	buyer := getOfferField(offerId, "b")
	if caller == buyer {
		// Mirrors doBuy's "Seller cannot buy own listing": a self-deal is
		// economically meaningless and would be a self-transfer on the NFT.
		sdk.Abort("Buyer cannot accept own offer")
	}
	nftContract := getOfferField(offerId, "nc")
	offerAmount := getOfferUint64(offerId, "a")
	pricePerUnit := getOfferMoney(offerId, "p")
	paymentToken := getOfferField(offerId, "pt")
	lockedFeeBps := getOfferUint64(offerId, "fb")
	lockedRoyaltyBps := getOfferUint64(offerId, "rb")
	royaltyRecipient := getOfferField(offerId, "rr")

	if acceptAmount == 0 {
		acceptAmount = offerAmount
	}
	if acceptAmount > offerAmount {
		sdk.Abort("Accept amount exceeds offer amount")
	}

	// Clean preflight instead of a raw cross-call abort.
	if !nftIsApprovedForAll(nftContract, caller, getContractAddress()) {
		sdk.Abort("Marketplace not approved as operator to fulfill offer")
	}
	if nftBalanceOf(nftContract, caller, tokenId) < acceptAmount {
		sdk.Abort("Insufficient NFT balance to fulfill offer")
	}

	totalPrice := mMulU64(pricePerUnit, acceptAmount)
	escrowed := getOfferMoney(offerId, "esc")
	if mCmp(totalPrice, escrowed) > 0 {
		sdk.Abort("Accept exceeds escrowed funds")
	}
	fee, royalty, sellerPayment := distributeFees(totalPrice, lockedFeeBps, lockedRoyaltyBps)

	nftSafeTransferFrom(nftContract, caller, buyer, tokenId, acceptAmount)

	if !mIsZero(sellerPayment) {
		tokenTransferBig(paymentToken, caller, sellerPayment)
	}
	if !mIsZero(fee) {
		tokenTransferBig(paymentToken, getFeeRecipient(), fee)
	}
	if !mIsZero(royalty) && royaltyRecipient != "" {
		tokenTransferBig(paymentToken, royaltyRecipient, royalty)
	}

	newRemaining := safeSub(offerAmount, acceptAmount)
	setOfferMoney(offerId, "esc", mSub(escrowed, totalPrice))
	if newRemaining == 0 {
		setOfferField(offerId, "act", "0")
	} else {
		setOfferUint64(offerId, "a", newRemaining)
	}

	emitOfferAccepted(offerId, caller, buyer, acceptAmount, formatMoney(totalPrice), formatMoney(fee), formatMoney(royalty), tokenId)
}
```

- [ ] **Step 5: `GetOffer` + `events.go`**

`GetOffer`: `PricePerUnit: formatMoney(getOfferMoney(p.OfferId, "p"))`. In `events.go` change `emitOfferMade` (pricePerUnit string) and `emitOfferAccepted` (totalPrice/fee/royalty string) signatures + assignments.

- [ ] **Step 6: Regenerate + build**

Run:
```bash
cd /home/dockeruser/magi/magi-market
/root/go/bin/tinyjson -all contract/types.go
GOTOOLCHAIN=go1.25.3 tinygo build -gc=custom -scheduler=none -panic=trap -no-debug -target=wasm-unknown -o test/artifacts/main.wasm ./contract && echo BUILD_OK
```
Expected: `BUILD_OK`.

- [ ] **Step 7: Update offer tests**

In `test/offer_test.go` and `test/collection_offer_test.go`: quote all `pricePerUnit`/`minOffer` payload values and expected output amounts; before each `acceptOffer`/`acceptCollectionOffer`, add the seller `setApprovalForAll` `callNft` (as in Task 2 Step 7); assert payment is escrowed at `makeOffer` (token balance of `MarketContractAddress` rose) and refunded on `cancelOffer`.

Also fix the second pre-existing failure, `TestBuyerCannotAcceptOwnOffer` in `test/review2_test.go`: it expected the NFT contract to reject the self-transfer with `"Cannot transfer to self"`, but the marketplace now rejects it earlier via the `caller == buyer` guard in `doAcceptOffer`. Update its `acceptOffer` call's expected-output substring from `"Cannot transfer to self"` to `"Buyer cannot accept own offer"`, quote its `pricePerUnit` to `"1000"`, and change the comment on the failing line to: `// Marketplace rejects self-deal before any NFT transfer`. After this task it must pass.

- [ ] **Step 8: Run offer suites then full**

Run: `GOTOOLCHAIN=go1.25.3 go test ./test/ -count=1 -run 'Offer|Collection' -v 2>&1 | tail -20`
Expected: PASS.
Then `GOTOOLCHAIN=go1.25.3 go test ./test/ -count=1 2>&1 | tail -8` — only auction-related suites may still fail (migrated in Task 4); listing/buy/offer must be green.

- [ ] **Step 9: Commit**

```bash
cd /home/dockeruser/magi/magi-market
git add contract/ test/offer_test.go test/collection_offer_test.go
git commit -m "feat: offers use big.Int + balance-delta escrow + accept preflight

- makeOffer stores actual received escrow ('esc'); cancel/accept pay
  from stored escrow so they cannot over-distribute
- acceptOffer/acceptCollectionOffer preflight isApprovedForAll +
  balance for a clean error
- offer payloads/responses/events use big.Int decimal strings"
```

---

## Task 4: Auctions — big.Int amounts, escrow unchanged, balance-delta bid escrow

**Why:** Auctions intentionally keep NFT escrow (rug-proof). Only monetary typing changes plus balance-delta on bid escrow.

**Files:**
- Modify: `contract/types.go` (auction string fields), `contract/auction.go`, `contract/events.go` (auction events string), `contract/types_tinyjson.go` (regen)
- Modify: `test/auction_test.go`

- [ ] **Step 1: String-type auction money fields in `types.go`**

uint64 → string: `CreateAuctionPayload.StartPrice`, `.EndPrice`; `PlaceBidPayload.BidAmount`; `AuctionResponse.StartPrice`, `.EndPrice`, `.HighBid`; `AuctionCreatedAttributes.StartPrice`, `.EndPrice`; `BidPlacedAttributes.BidAmount`; `AuctionSettledAttributes.FinalPrice`, `.Fee`, `.Royalty`. (`StartBlock`/`EndBlock`/ids stay uint64.)

- [ ] **Step 2: Migrate `contract/auction.go`**

- Add a big.Int Dutch price helper alongside the existing one:
```go
func getDutchAuctionCurrentPriceBig(startPrice, endPrice *big.Int, startBlock, endBlock, currentBlock uint64) *big.Int {
	if currentBlock <= startBlock {
		return startPrice
	}
	if currentBlock >= endBlock {
		return endPrice
	}
	elapsed := new(big.Int).SetUint64(currentBlock - startBlock)
	duration := new(big.Int).SetUint64(endBlock - startBlock)
	drop := new(big.Int).Mul(mSub(startPrice, endPrice), elapsed)
	drop.Quo(drop, duration)
	return mSub(startPrice, drop)
}
```
- `CreateAuction`: `startP := parseMoney(p.StartPrice)`; for dutch `endP := parseMoney(p.EndPrice)` with `mCmp(endP, startP) >= 0` → abort "Dutch auction end price must be less than start price"; for english `endP := mZero()`. Require `!mIsZero(startP)`. Keep the NFT `nftSafeTransferFrom(p.NftContract, caller, contractAddr, ...)` escrow exactly as today. Store `setAuctionMoney(id,"sp",startP)`, `setAuctionMoney(id,"ep",endP)`. Emit with `formatMoney`.
- `PlaceBid` english branch: `bid := parseMoney(p.BidAmount)`; `reserveTotal := mMulU64(getAuctionMoney(id,"sp"), amount)`; `currentHighBid := getAuctionMoney(id,"ha")`; comparisons via `mCmp`; min-increment: `minBid := mAdd(currentHighBid, mMulBpsDiv(currentHighBid, minIncBps))`. Escrow via `received := escrowIn(paymentToken, caller, bid)`; store `setAuctionMoney(id,"ha",received)` and refund previous `getAuctionMoney`-read high bid with `tokenTransferBig`. Anti-snipe block logic unchanged.
- `PlaceBid` dutch branch: `currentPrice := getDutchAuctionCurrentPriceBig(getAuctionMoney(id,"sp"), getAuctionMoney(id,"ep"), startBlock, endBlock, currentBlock)`; `totalPrice := mMulU64(currentPrice, amount)`; `received := escrowIn(...)`; require `mCmp(received, totalPrice) >= 0`; transfer escrowed NFT to buyer (unchanged); `distributeFees(received, ...)`; payouts via `tokenTransferBig`; emit with `formatMoney`.
- `SettleAuction`: read `highBid := getAuctionMoney(id,"ha")`; `if highBidder == "" || mIsZero(highBid)` → return NFT to seller (unchanged escrow return) and `emitAuctionSettled(id,"","0","0","0")`; else NFT to winner (unchanged), `fee,royalty,sellerPayment := distributeFees(highBid, ...)`, payouts via `tokenTransferBig`, emit with `formatMoney`.
- `CancelAuction`: unchanged except it already returns escrowed NFT — keep.
- `GetAuction`: `StartPrice/EndPrice/HighBid` via `formatMoney(getAuctionMoney(...))`.

- [ ] **Step 3: `events.go` auction signatures**

Change `emitAuctionCreated` (startPrice/endPrice string), `emitBidPlaced` (bidAmount string), `emitAuctionSettled` (finalPrice/fee/royalty string) and assign into the now-string attribute fields.

- [ ] **Step 4: Regenerate + build**

Run:
```bash
cd /home/dockeruser/magi/magi-market
/root/go/bin/tinyjson -all contract/types.go
GOTOOLCHAIN=go1.25.3 tinygo build -gc=custom -scheduler=none -panic=trap -no-debug -target=wasm-unknown -o test/artifacts/main.wasm ./contract && echo BUILD_OK
```
Expected: `BUILD_OK`.

- [ ] **Step 5: Update `test/auction_test.go`**

Quote all `startPrice`/`endPrice`/`bidAmount` payload values and expected amount outputs. Keep existing escrow assertions (createAuction moves NFT into `MarketContractAddress`; settle/cancel return it) — these must still hold. The auction seller still needs the NFT and approval is NOT required for auctions (escrow uses caller authority); leave existing mint/escrow setup as-is, only string-ify amounts.

- [ ] **Step 6: Run auction suite then FULL suite green**

Run: `GOTOOLCHAIN=go1.25.3 go test ./test/ -count=1 -run Auction -v 2>&1 | tail -20`
Expected: PASS.
Then: `GOTOOLCHAIN=go1.25.3 go test ./test/ -count=1 2>&1 | tail -8`
Expected: `ok  	magi_market/test` — entire suite green again (Tasks 2–4 complete the migration).

- [ ] **Step 7: Commit**

```bash
cd /home/dockeruser/magi/magi-market
git add contract/ test/auction_test.go
git commit -m "feat: auctions use big.Int amounts + balance-delta bid escrow

NFT escrow model for auctions unchanged (rug-proof). Prices/bids/
settlement amounts are big.Int decimal strings; bid escrow distributes
actual received funds."
```

---

## Task 5: Admin/query money surfaces, emergency-withdraw, retire uint64 token helpers

**Why:** Finish the migration surface (minOffer + token emergency withdraw) and delete now-dead uint64 token call helpers so only the big.Int path remains.

**Files:**
- Modify: `contract/types.go`, `contract/internal.go`, `contract/market.go`, `contract/types_tinyjson.go` (regen)
- Modify: `test/review_fixes_test.go`, `test/review2_test.go`, `test/review3_test.go`, `test/review3_fixes_test.go`, `test/expiration_test.go`, `test/royalty_test.go` (only where they pass/expect `minOffer` or emergency-withdraw `amount` or token output amounts)

- [ ] **Step 1: String-type remaining money fields in `types.go`**

uint64 → string: `SetMinOfferPayload.MinOffer`, `MinOfferResponse.MinOffer`, `InfoResponse.MinOffer`, `EmergencyWithdrawPayload.Amount`, `EmergencyWithdrawAttributes.Amount`.

- [ ] **Step 2: `internal.go` minOffer helpers → money**

Replace:
```go
func getMinOffer() uint64        { return getUint64State("min_ofr") }
func setMinOfferState(v uint64)  { setUint64State("min_ofr", v) }
```
with:
```go
func getMinOfferMoney() *big.Int   { return getMoneyState("min_ofr") }
func setMinOfferMoney(v *big.Int)  { setMoneyState("min_ofr", v) }
```
Update the `MakeOffer` min-offer check (Task 3 already reads `getMoneyState("min_ofr")` — switch it to `getMinOfferMoney()` for consistency). Delete the old uint64 `tokenTransfer` and `tokenTransferFrom` (all call sites now use `tokenTransferBig`/`tokenTransferFromBig`); keep `nftSafeTransferFrom`, `nftBalanceOf`, `nftGetOwner`.

- [ ] **Step 3: `market.go` minOffer + emergencyWithdraw**

`SetMinOffer`:
```go
	setMinOfferMoney(parseMoney(p.MinOffer))
```
`GetMinOffer`: `MinOffer: formatMoney(getMinOfferMoney())`. `GetInfo`: `MinOffer: formatMoney(getMinOfferMoney())`.

`EmergencyWithdraw` — split by token type using a string amount:
```go
	if p.Contract == "" || p.To == "" || p.Amount == "" {
		sdk.Abort("Contract, to, and amount required")
	}
	if p.TokenType == "nft" {
		if p.TokenId == "" {
			sdk.Abort("Token ID required for NFT withdraw")
		}
		qty := parseMoney(p.Amount)
		if !qty.IsUint64() {
			sdk.Abort("NFT amount too large")
		}
		nftSafeTransferFrom(p.Contract, getContractAddress(), p.To, p.TokenId, qty.Uint64())
	} else if p.TokenType == "token" {
		tokenTransferBig(p.Contract, p.To, parseMoney(p.Amount))
	} else {
		sdk.Abort("Token type must be 'nft' or 'token'")
	}
	emitEmergencyWithdraw(p.TokenType, p.Contract, p.TokenId, p.Amount, p.To)
```
Change `emitEmergencyWithdraw` signature so `amount` is `string`.

- [ ] **Step 4: Regenerate + build + run full suite**

```bash
cd /home/dockeruser/magi/magi-market
/root/go/bin/tinyjson -all contract/types.go
GOTOOLCHAIN=go1.25.3 tinygo build -gc=custom -scheduler=none -panic=trap -no-debug -target=wasm-unknown -o test/artifacts/main.wasm ./contract && echo BUILD_OK
```
Expected: `BUILD_OK`. Then update any failing `minOffer`/`emergencyWithdraw`/token-amount assertions in the review/expiration/royalty test files to quoted strings, and:
```bash
GOTOOLCHAIN=go1.25.3 go test ./test/ -count=1 2>&1 | tail -8
```
Expected: `ok  	magi_market/test` (entire suite green).

- [ ] **Step 5: Commit**

```bash
cd /home/dockeruser/magi/magi-market
git add contract/ test/
git commit -m "feat: big.Int minOffer + emergency token withdraw; drop uint64 token helpers

minOffer and emergencyWithdraw token amounts are big.Int strings;
removed the now-unused uint64 tokenTransfer/tokenTransferFrom."
```

---

## Task 6: Fee-on-transfer proof + UTXO payment-token verification

**Why:** Prove balance-delta accounting distributes the received (post-fee) amount, and verify a real `utxo-mapping` contract is wire-compatible as a payment token.

**Files:**
- Create: `test/mocks/feetoken/contract/main.go`, `test/mocks/feetoken/go.mod`, `test/artifacts/feetoken.wasm` (gitignored), `test/feetoken_test.go`
- Modify: `test/helpers_test.go` (register the fee token), `docs/superpowers/specs/2026-05-17-magi-market-contract-compatibility-design.md` (record UTXO verification result)

- [ ] **Step 1: Minimal fee-on-transfer mock token**

Create `test/mocks/feetoken/go.mod`:
```
module feetoken

go 1.24.0

require github.com/CosmWasm/tinyjson v0.9.0
```
Create `test/mocks/feetoken/contract/main.go` — an ERC-20-shaped contract whose `transfer`/`transferFrom` credit the recipient `amount - fee` (fee = 1% floor) while debiting the full `amount`, with `init {supply,owner}`, `mint {to,amount}`, `balanceOf {account}->{ "balance":"<dec>" }`, `transfer {to,amount}`, `transferFrom {from,to,amount}`. Use the same big.Int string convention and `//go:wasmexport` style as `/mnt/HC_Volume_105012347/magi/testnet/magi_token-contract/contract` (copy its structure; change only the transfer crediting to subtract `mMulBpsDiv(amount,100)`). Keep it minimal (no allowance enforcement needed for the test; `transferFrom` ignores allowance).

Build it:
```bash
cd /home/dockeruser/magi/magi-market/test/mocks/feetoken
GOTOOLCHAIN=go1.25.3 GOFLAGS=-mod=mod go mod tidy
GOTOOLCHAIN=go1.25.3 tinygo build -gc=custom -scheduler=none -panic=trap -no-debug -target=wasm-unknown -o /home/dockeruser/magi/magi-market/test/artifacts/feetoken.wasm ./contract && echo FEETOKEN_OK
```
Expected: `FEETOKEN_OK`.

- [ ] **Step 2: Register the fee token in the harness**

In `test/helpers_test.go`, next to the existing artifact embeds, add:
```go
//go:embed artifacts/feetoken.wasm
var FeeTokenWasm []byte
```
and a `const FeeTokenID = "feetoken"`, plus in `SetupContractTest()` add
`ct.RegisterContract(FeeTokenID, ownerAddress, FeeTokenWasm)`.

- [ ] **Step 3: Write the failing fee-on-transfer test**

Create `test/feetoken_test.go`: init market with `feeBps:0` (isolate the transfer fee), `addPaymentToken {"token":"contract:feetoken"}`, init+mint feetoken to buyer, mint NFT to seller, seller `setApprovalForAll` market. Seller `list` `{pricePerUnit:"1000",amount:1,paymentToken:"contract:feetoken"}`. Buyer `buy {listingId:0,amount:1}`. Assert: `feetoken.balanceOf(seller)` increased by `received` (= `1000 - floor(1000*0.01)` = `990`), NOT `1000`; and the `bought` event `totalPrice` equals `"990"`. Run:
```bash
GOTOOLCHAIN=go1.25.3 go test ./test/ -count=1 -run FeeToken -v 2>&1 | tail -20
```
Expected first run: FAIL only if implementation is wrong; with Tasks 1–5 done correctly it should PASS (balance-delta already distributes `received`). If it FAILS because the seller got `1000`, balance-delta is not wired — fix `doBuy`.

- [ ] **Step 4: Commit**

```bash
cd /home/dockeruser/magi/magi-market
git add test/mocks/feetoken test/feetoken_test.go test/helpers_test.go
git commit -m "test: fee-on-transfer mock proves balance-delta distributes received"
```

- [ ] **Step 5: UTXO payment-token wire-compat verification (no code change)**

This is a verification checklist; record findings in the spec's "Open verification items" section.

5a. Build a UTXO mapping wasm to confirm it compiles with the same toolchain:
```bash
cd /mnt/HC_Volume_105012347/magi/testnet/utxo-mapping/btc-mapping-contract
GOTOOLCHAIN=go1.25.3 tinygo build -gc=custom -scheduler=none -panic=trap -no-debug -target=wasm-unknown -o /tmp/btc-mapping.wasm ./contract && echo UTXO_BUILD_OK
```
Expected: `UTXO_BUILD_OK`.

5b. Confirm payload field-name parity by reading the mapping contract source:
```bash
grep -n "json:\"to\"\|json:\"from\"\|json:\"amount\"\|json:\"account\"\|\"balance\"" \
  /mnt/HC_Volume_105012347/magi/testnet/utxo-mapping/btc-mapping-contract/contract/mapping/types.go
```
Expected: `transfer`/`transferFrom` accept `{to,amount}` / `{from,to,amount}` and balance reads `{account}`→`{"balance":...}` — matching the strings magi-market emits in `tokenTransferBig`/`tokenTransferFromBig`/`tokenBalanceOf`.

5c. Verify address-form acceptance: inspect how the mapping contract keys balances/allowances (`grep -n "BalancePrefix\|getAccBal\|allowanceKey" .../contract/mapping/utils.go`) and confirm the recipient/`from` strings are used opaquely (no `did:vsc:`-only validation that would reject `contract:<id>` or `hive:user`). Record the canonical address form actually required.

5d. In the design spec file, replace the three "Open verification items" with the concrete findings (build result, payload parity confirmed, address-form result). If 5c reveals an address-form restriction, that is a NEW finding — STOP and report rather than adding an adapter unilaterally (per the no-external-repo-mods / surface-blockers rule).

- [ ] **Step 6: Commit the verification record**

```bash
cd /home/dockeruser/magi/magi-market
git add docs/superpowers/specs/2026-05-17-magi-market-contract-compatibility-design.md
git commit -m "docs: record UTXO mapping payment-token wire-compat verification"
```

---

## Self-Review (performed during planning)

**Spec coverage:**
- big.Int payment amounts → Tasks 1–5 (money.go + per-surface migration). ✓
- Operator-approval custody for listings/offers → Task 2 (list/buy), Task 3 (accept preflight). ✓
- Auctions keep escrow → Task 4 (escrow lines explicitly preserved). ✓
- Balance-delta accounting → `escrowIn` (Task 1), applied in buy (T2), offers (T3), bids (T4), proven in Task 6. ✓
- UTXO as payment token via whitelist, no special-casing → Task 6 verification + existing `addPaymentToken`. ✓
- Latest magi_nft-contract interface (operator approval, proper isSoulbound, isApprovedForAll) → Tasks 1–2. ✓
- Fresh deploy / no migration assumption → no migration task (correct per spec). ✓
- Out-of-scope (collection metadata/properties, modular split) → not present. ✓

**Placeholder scan:** No TBD/TODO; every code step has complete code or exact field-level instructions; commands have expected output.

**Type consistency:** `escrowIn`/`tokenTransferBig`/`tokenTransferFromBig`/`tokenBalanceOf`/`nftIsApprovedForAll`/`jsonBoolField` defined in Task 1 and used with identical signatures in Tasks 2–6. Money helpers (`parseMoney`/`formatMoney`/`mMulU64`/`mMulBpsDiv`/`mCmp`/`mIsZero`/`mSub`/`mAdd`/`getMoneyState`/per-entity get/set) consistent throughout. `distributeFees` redefined once (Task 2 Step 5a) as `(*big.Int, uint64, uint64) -> (*big.Int,*big.Int,*big.Int)` and used consistently after.

**Known sequencing note:** Between Task 2 and Task 4 the shared `types.go`/`types_tinyjson.go` is partially migrated, so the full suite is intentionally not green until Task 4 Step 6 (each task verifies its own sub-suite; later-surface suites are expected red and explicitly bounded). This is called out in each task's run step.

---

