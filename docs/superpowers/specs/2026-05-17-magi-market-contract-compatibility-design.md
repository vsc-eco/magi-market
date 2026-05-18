# magi-market — Latest NFT/Token + UTXO Payment Compatibility

**Date:** 2026-05-17
**Status:** Design approved, pending spec review
**Repo:** `magi-market` (cloned to `/home/dockeruser/magi/magi-market`)

## Goal

A proactive compatibility + capability pass on the `magi-market` NFT marketplace
contract so it works correctly against:

1. The latest `magi_nft-contract` (ERC-1155, now with operator approval).
2. The latest `magi_token-contract` (ERC-20, arbitrary-precision `big.Int`
   string amounts).
3. Any `utxo-mapping` contract (btc/dash/ltc/doge/bch) used as a payment
   token, so users can pay for NFTs with mapped UTXO coins (e.g. BTC → NFT).

Nothing is confirmed broken today; the interfaces are closer than the request
implied. This work modernizes the custody model, removes the `uint64` price
ceiling, and hardens payment accounting so the above are correct and safe.

## Decisions (locked during brainstorming)

| Topic | Decision |
|---|---|
| Driver | Proactive compatibility, not a known break. |
| NFT custody (listings/offers) | **Operator-approval model** — NFT stays with seller, market transfers on sale. |
| NFT custody (auctions) | **Keep escrow** — NFT pulled into market at `createAuction`; rug-proof for time-bound auctions. |
| Payment amount precision | **big.Int decimal strings** end-to-end (no `uint64` ceiling). |
| Payment accounting | **Balance-delta** — distribute what was actually received, robust to fee-on-transfer / UTXO `deduct_fee`. |
| Structure | **Layered in place** — add `money.go` + custody/preflight helpers, keep existing file layout. |

## Reference: target contract interfaces (verified during exploration)

- `magi_nft-contract` @ `/mnt/HC_Volume_105012347/magi/magi_nft-contract`
  (commit `223c728`, 2026-03-11). ERC-1155 with `setApprovalForAll` /
  `isApprovedForAll`. `safeTransferFrom {from,to,id,amount:<uint64>,data}`,
  `balanceOf {account,id}→{balance:<uint64>}`, `isSoulbound {id}→{soulbound:bool}`,
  `getOwner {}→{owner}`. NFT quantities are `uint64`.
- `magi_token-contract` @ `/mnt/HC_Volume_105012347/magi/testnet/magi_token-contract`
  (commit `a819106`, 2026-05-06). ERC-20. `transfer {to,amount:"<dec>"}`,
  `transferFrom {from,to,amount:"<dec>"}`, `balanceOf {account}→{balance:"<dec>"}`,
  allowance-gated `transferFrom`. Amounts are `math/big.Int` decimal **strings**.
- `utxo-mapping` @ `/mnt/HC_Volume_105012347/magi/testnet/utxo-mapping`
  (commit `6039c43`, 2026-05-14). Mapped coin is an **internal per-account
  ledger** inside each mapping contract, but each exposes ERC-20-style
  `transfer {to,amount}` / `transferFrom {from,to,amount}` / `balanceOf` /
  `approve`. Amounts are satoshi `int64` decimal strings. `TransferParams`
  has optional `deduct_fee` / `max_fee`. Asset symbol e.g. `"BTC"`.

## Assumptions

- **Fresh deployment, no state migration.** Monetary state encoding changes
  (`uint64` → decimal string); old listings/offers/auctions would be
  unreadable. A new contract instance is deployed. Draining any populated
  instance (pause + delist/cancel) is out of scope here.
- **NFT quantities stay `uint64`.** Only monetary values become `big.Int`.
  `minBidIncrementBps`, `feeBps`, `royaltyBps`, `antiSnipeBlocks`,
  `expirationBlock`, ids remain `uint64`.
- **No floats, single package, manual JSON** for amount fields — mirrors
  `magi_token-contract` (avoid `tinyjson.Marshal`/WASI; tinygo
  `wasm-unknown`).
- Address strings (`hive:user`, `did:vsc:…`, `contract:<id>`) are passed
  through opaquely. UTXO address-format compatibility is a verification task,
  not a redesign.

## Section 1 — Payment money model (`money.go`)

New file `contract/money.go`, the only place `math/big` is used.

- `parseMoney(s string) *big.Int` — strict non-negative decimal; reject
  empty / sign / non-digit / whitespace; `sdk.Abort` on bad input.
- `formatMoney(*big.Int) string` — canonical decimal string for
  payloads / state / responses.
- `mAdd(a,b)`, `mSub(a,b)` (underflow → Abort), `mMulU64(price, qty uint64)`
  (price × NFT quantity), `mMulBpsDiv(total, bps uint64)` (= total×bps/10000,
  floor; for fee & royalty), `mCmp`, `mIsZero`.
- `getMoneyState(key) *big.Int` / `setMoneyState(key, *big.Int)` storing
  canonical decimal strings.

`distributeFees` is reimplemented over `big.Int`: returns
`(fee, royalty, sellerPayment)` from a total, locked feeBps, locked
royaltyBps, with `sellerPayment = total − fee − royalty` via `mSub`.

### Payload / response / event changes

Every JSON field that is a monetary integer becomes a **quoted decimal
string**: `pricePerUnit`, `newPrice`, `bidAmount`, `startPrice`, `endPrice`,
`minOffer`, and totals/fees/royalties in responses and event attributes
(`totalPrice`, `fee`, `royalty`, `highBid`, `finalPrice`). NFT `amount`
(quantity) stays an **unquoted integer**. Ids, blocks, bps stay unquoted
integers.

Amount fields are read with a small hand-rolled field extractor (generalizing
the existing `balanceOf` parser); non-amount fields continue via tinyjson as
today. Responses/events are emitted with hand-built JSON for amount fields to
avoid pulling float/WASI marshaling.

### Balance-delta accounting

Every **inbound** payment transfer (buy escrow leg, offer escrow, English-bid
escrow, Dutch-buy escrow) becomes:

1. `before = tokenBalanceOf(paymentToken, contractAddr)`
2. `tokenTransferFrom(paymentToken, payer, contractAddr, requested)`
3. `after = tokenBalanceOf(paymentToken, contractAddr)`;
   `received = mSub(after, before)`
4. All downstream splits (fee / royalty / seller, bid refunds) are computed
   from `received`, never from `requested`.

New generic `tokenBalanceOf(token, account) *big.Int` parsing
`{"balance":"…"}` or `{"balance":N}` (string or number) into `big.Int`.
Outbound transfers (payouts, refunds) use computed amounts; escrow only ever
distributes what it actually holds, so it cannot underflow even if an outbound
transfer is itself fee-charging (borne by the recipient — consistent
pay-exact semantics).

## Section 2 — NFT custody

**Listings/offers → operator-approval. Auctions → unchanged escrow.**

- **`list` / `batchList`:** no NFT escrow. Preflight: `isApprovedForAll(seller,
  marketAddr) == true`, `nftBalanceOf(seller, tokenId) >= amount`,
  `isSoulbound == false`. Store listing; NFT stays with seller.
- **`buy` / `batchBuy`:** escrow payment (balance-delta) →
  `safeTransferFrom(seller → buyer, qty)` with market as approved operator →
  distribute `received`. If the NFT transfer aborts (seller moved/burned the
  token or revoked approval), the whole transaction reverts; the buyer's
  escrowed payment is unwound by the abort. No partial state, no orphaned
  escrow.
- **`delist` / `updateListing`:** no NFT movement (nothing escrowed); state
  only. `delist` still works while paused.
- **Offers:** payment escrow unchanged; `acceptOffer` /
  `acceptCollectionOffer` already do seller→buyer `safeTransferFrom`. Add an
  `isApprovedForAll` + `balanceOf` preflight so a missing approval yields a
  clean marketplace error rather than a raw cross-call abort.
- **Auctions:** `createAuction` still escrows the NFT into the market;
  `settleAuction` / `cancelAuction` return it. No custody change — only the
  monetary-amount type migration touches `auction.go`.

## Section 3 — UTXO payment support

A UTXO mapping contract *is* a payment token (`transfer` / `transferFrom` /
`balanceOf`). The owner whitelists each mapping contract ID via the existing
`addPaymentToken`. **No special-casing in market logic.** Correctness rests
on:

- (a) string amounts — Section 1;
- (b) balance-delta accounting absorbing `deduct_fee` — Section 1;
- (c) the mapping contract accepting the market's `contract:<id>` escrow
  address and the caller's address form as ledger keys.

(c) is a **verification task** (not redesign): confirm against `utxo-mapping`
(btc + one alt, e.g. dash) that `transfer` / `transferFrom` / `balanceOf`
accept `contract:<id>` and the caller's address form; document the canonical
address form. If a format adapter proves necessary it is localized to the
token-helper layer only.

## Section 4 — Contract-call interface alignment

- `nftSafeTransferFrom`, `nftBalanceOf`, `nftIsSoulbound`, `nftGetOwner`:
  payloads already match latest `magi_nft-contract` — keep. Replace the crude
  `isSoulbound` substring scan with a real `"soulbound":true` field check.
  Add `nftIsApprovedForAll(account, operator) bool` (payload
  `{"account","operator"}`, response `{"approved":bool}`).
- `tokenTransfer` / `tokenTransferFrom`: payload shape unchanged (string
  amount) — now fed `big.Int` decimal strings. Add
  `tokenBalanceOf(token, account)`.
- Collection metadata / properties: **not consumed** by the marketplace
  (display concern; frontend reads `magi_nft-contract` directly). Explicitly
  out of scope.

## Section 5 — Testing

Extend the existing `test/` suite (mock nft/token contracts):

- big.Int price suite, including prices/totals exceeding `uint64`.
- Fee-on-transfer mock token proving balance-delta splits use `received`.
- Approval-model listing/buy suite, incl. seller-moved-NFT → `buy` aborts
  cleanly with no escrow leak; missing-approval preflight error on `list` and
  `acceptOffer`.
- Unchanged-auction-escrow regression (createAuction escrows; settle/cancel
  return; amounts as strings).
- UTXO-style mock payment token (satoshi string amounts, optional
  `deduct_fee`) exercised end-to-end through buy / offer.
- Existing review / edge / expiration / royalty suites kept green (amount-type
  updates only).

## Out of scope

- In-place state migration / upgrade of a populated contract instance.
- Consuming collection metadata or token properties in market logic.
- Modular package split (rejected: fights single-package wasm build pattern).
- Any changes to `magi_nft-contract`, `magi_token-contract`, or
  `utxo-mapping` themselves.

## UTXO mapping wire-compat verification (2026-05-18, read-only)

Verified against `utxo-mapping/btc-mapping-contract` at its committed state
(no external-repo modification). Findings recorded honestly, including a
surfaced blocker that is documented here as a RISK rather than worked around.

1. **UTXO build result.** `tinygo build -gc=custom -scheduler=none
   -panic=trap -no-debug -target=wasm-unknown ./contract` FAILS under the
   pinned `GOTOOLCHAIN=go1.25.3` because the repo's `go.mod` declares
   `go 1.25.6` (`go: go.mod requires go >= 1.25.6 (running go 1.25.3)`).
   The same build SUCCEEDS with `GOTOOLCHAIN=go1.25.6` (auto-fetched,
   no repo change): a 704 KB wasm is produced. Conclusion: the UTXO
   contract builds; it just requires go ≥ 1.25.6, a newer floor than
   magi-market's. Not a code-compat issue, but a toolchain-version note
   for whoever wires the integration build.

2. **Payload field-name parity.** `transfer` / `transferFrom` both
   unmarshal `mapping.TransferParams` = `{"amount":"<dec-string>",
   "to":"<addr>","from":"<addr>"(omitempty),"deduct_fee":bool(omitempty),
   "max_fee":int(omitempty)}`. magi-market emits exactly:
   `tokenTransferBig` → `{"to":"<addr>","amount":"<dec>"}`,
   `tokenTransferFromBig` → `{"from":"<addr>","to":"<addr>","amount":"<dec>"}`.
   Field names + the quoted-decimal-string `amount` type MATCH (UTXO's
   extra fields are all `omitempty`, so the market's narrower payload
   parses cleanly; `transfer` additionally force-clears `from`).
   **MISMATCH / BLOCKER:** the UTXO contract exposes **no `balanceOf`
   entrypoint** (full wasmexport list: seedBlocks, initPruning,
   setMaxUnmapPerBlock, prune, addBlocks, replaceBlock, replaceBlocks,
   map, unmap, unmapFrom, transfer, transferFrom, approve,
   increaseAllowance, decreaseAllowance, confirmSpend, pause, unpause,
   migrate, getInfo, registerPublicKey, createKey, renewKey,
   registerRouter — no `balanceOf`). magi-market's `escrowIn` /
   `tokenBalanceOf` REQUIRE a `balanceOf` returning
   `{"balance":"<dec>"}`. Without it the balance-delta escrow
   accounting (the entire fee-on-transfer / deduct_fee safety
   mechanism) cannot run against the UTXO token. Separately, UTXO
   `transfer`/`transferFrom` return the bare string `"0"` (not
   `{"success":true}`); magi-market only null-checks the result so
   that is tolerated — the `balanceOf` gap is the hard blocker. This
   is recorded as a RISK; no adapter added and the utxo repo is left
   unmodified per the no-external-repo-mods constraint.

3. **Address-form finding + canonical form.** UTXO `HandleTransfer`
   uses `from`/`to`/account opaquely as ledger keys
   (`constants.BalancePrefix + acct`) — no `did:vsc:`-only ledger
   validation. BUT the recipient is gated by
   `sdk.VerifyAddress(instructions.To) != "unknown"`, backed by host
   `dids.VerifyAddress`. That accepts `hive:<username>` only for a
   3–16-char hive-pattern name, and `contract:<id>` only when the id is
   `vsc1`-prefixed, exactly 38 chars, base58check with version `0x1a`.
   Canonical required form: the marketplace escrow address must be the
   real deployed `contract:vsc1…` (38-char base58check) — which it is
   on a live chain — and users must be valid `hive:` names. Arbitrary
   short ids (e.g. `contract:market` as used in the unit harness) would
   be rejected as `"unknown"`. This is NOT a blocker for production
   (real contract ids are canonical `vsc1…`); it is a constraint to
   honor in integration tests (use canonical addresses, not stub ids).

**Surfaced blocker (carried to report):** the UTXO mapping contract has
no `balanceOf`, so magi-market's balance-delta escrow cannot operate
against it as-is. Fixing this requires a change in the utxo-mapping repo
(add a `balanceOf` entrypoint returning `{"balance":"<dec>"}`), which is
out of scope here and must not be done as a magi-market-side adapter.

(Prior item — tinygo `wasm-unknown` size/gas budget after adding
`math/big` — is already proven: the fee-on-transfer mock and the live
magi-market `main.wasm` both build and the full 252-test suite passes
(245 base + 4 fee-on-transfer + 3 UTXO-payment cases).)

## Resolution: raw cross-contract state reads for ALL getters (2026-05-18, user-decided)

The `balanceOf` blocker is resolved by switching magi-market's cross-contract
reads from method calls (`contracts.call`) to raw state reads
(`sdk.ContractStateGet` = `contracts.read`). The user explicitly chose to
convert **all five** getters (not just balances), accepting the coupling
tradeoff below. Implemented as Task 7.

**Converted getters and the exact target encodings (verified from source):**

| getter | contract | state key | value encoding |
|---|---|---|---|
| `nftIsSoulbound` | magi_nft | `sb\|<id>` | `"1"` ⇒ true; absent/other ⇒ false |
| `nftIsApprovedForAll` | magi_nft | `op\|<owner>\|<operator>` | `"1"` ⇒ true; `"0"`/absent ⇒ false |
| `nftGetOwner` | magi_nft | `owner` | address string (absent ⇒ "") |
| `nftBalanceOf` | magi_nft | `bal\|<acct>\|<id>` | **little-endian** uint64, trailing-zero-trimmed; absent ⇒ 0 |
| `tokenBalanceOf` | magi_token / mock | `bal\|<acct>` | `big.Int.Bytes()` (**big-endian** unsigned magnitude); absent ⇒ 0 |
| `tokenBalanceOf` | utxo-mapping | `a-<acct>` | **big-endian** uint64, leading-zero-trimmed; absent ⇒ 0 |

`tokenBalanceOf` probes `bal|<acct>` first (magi_token / fee mock); if
absent it probes `a-<acct>` (utxo). The two key prefixes are disjoint and
each contract writes only its own, so the probe order is unambiguous
(both-absent ⇒ 0, correct either way).

**Accepted risk (recorded so it is a deliberate, visible tradeoff).**
This hardcodes the *internal* state-key schemes and byte encodings of three
independently-versioned contracts (magi_nft, magi_token, utxo-mapping) into
magi-market. If any of them changes its internal storage layout, magi-market
will silently read wrong values (no compile error) — and for the balance
reads that is a fund-safety bug. This trades the stability of those
contracts' method ABIs for lower RC/gas and UTXO compatibility. Mitigation:
the codecs are centralized in clearly-commented helpers citing the exact
source file/commit they mirror; the full wasm suite (which round-trips real
`magi_token`/`magi_nft` wasm through balance-delta) is the regression oracle
that would catch an encoding drift; a dedicated UTXO-style mock proves the
`a-<acct>` path end-to-end. **Mid-transaction visibility** (balance-delta
reads balance before and after a `transferFrom` within one marketplace call)
is validated by the existing balance-delta suite remaining green after the
switch — if `contracts.read` did not reflect the just-applied cross-contract
write, those tests would fail.
