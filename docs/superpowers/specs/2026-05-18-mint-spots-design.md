# magi-market — Sell "Mint Spots" (Primary-Sale / Mint-on-Buy) — Sub-project G

**Date:** 2026-05-18
**Status:** Design approved (single consolidated approval), pending implementation
**Repo:** `magi-market`, branch `feature/mint-spots` (off `feature/marketplace-bf` @ `52277f0`)

## Context

A creator defines an editioned NFT on `magi_nft-contract` (fixed `maxSupply`,
0 minted) and wants to sell "mint spots" through the marketplace: buyers pay,
the market mints a fresh token from the edition directly to the buyer, up to
`maxSupply`. This is the primary-sale / mint-on-buy capability previously
deferred ("lazy-mint"), now unblocked by the nft-contract
**define-without-mint + delegated (market) mint** feature
(`/home/dockeruser/magi/magi_nft-contract/docs/superpowers/specs/2026-05-18-editioned-nft-define-delegated-mint-design.md`).

**Hard dependency status (must be read first):** that nft feature is
**spec-only — NOT implemented**. Branch `feat/editioned-define-delegated-mint`
contains only the design doc commit; `magi_nft-contract/contract/token.go`
still has the owner-only mint gate and the blanket
`Amount must be greater than 0`. magi-market's test harness builds `nft.wasm`
from that repo, so this feature **cannot be end-to-end integration-tested
against the real nft contract until the upstream feature is implemented**.
Per the user-decided approach, the market side is built and proven now against
the **documented delegated-mint ABI** via a `mintnftmock` (same precedent as
F's `utxomock`/`dexmock`), with the real-nft integration recorded as a
verification task + risk.

## Cross-cutting invariants (inherited, unchanged)

- big.Int decimal-string money; reuse `parseMoney`/`formatMoney`/`mMulU64`/
  `mIsZero`/money state helpers; balance-delta `escrowIn`; `tokenTransferBig`;
  `feeAndRoyaltyOf`.
- Cross-contract reads = existing raw-state getters; cross-contract *writes*/
  new calls use the target's stable method ABI (here: the nft `mint` ABI) —
  NOT internal state.
- `contract/types_tinyjson.go` hand-maintained; new structs get hand-written
  encode/decode mirroring existing patterns.
- Denylist `assertCollectionAllowed` gates the new create + complete paths.
- Opt-in & additive: new entrypoints only; every existing default path and
  the prior 306-test suite stay byte-identical/green.
- Build/test/git invariants identical to prior sub-projects
  (`GOTOOLCHAIN=go1.25.3`, gitignored `go.work`, on-branch commits, never run
  tinyjson, full wasm suite is the regression oracle).

## Locked design decisions

| # | Decision |
|---|---|
| 1 | **Single editioned id only.** One `tokenId` with the edition's `maxSupply`; each `buyMintSpot` mints `amount` of that id. Selling a `MintSeries` range of distinct ids is out of scope (YAGNI defer). |
| 2 | **Lister authorization.** `listMintSpots` requires `nftGetOwner(nftContract) == caller` AND `nftIsApprovedForAll(nftContract, caller, market)` (so the market's delegated `mint` is authorized per the nft `isApprovedOrOwner` gate). The market never *defines* the edition (owner-only, done off-market on the nft contract). The edition must already exist (`maxSupply > 0`). |
| 3 | **Supply truth = the nft contract.** The market enforces only an optional lister-set `maxSpots` (cumulative spots this listing may sell, for listing lifecycle/UX); the *hard* cap is the nft contract's `maxSupply` enforcement, which aborts an over-mint and reverts the whole tx (buyer refunded). No coupled internal-state supply pre-read (avoids deepening the documented internal-storage coupling risk). |
| 4 | **Primary-sale payout.** Marketplace fee → `getFeeRecipient()`; remainder → the creator/lister. **No royalty-split on primary mint** (the creator is the seller; royalty would pay them twice). Uses `escrowIn` (balance-delta `received`) then `feeAndRoyaltyOf(received, lockedFeeBps, nil, nil)` (nil royalty — identical pattern to D swap top-up). Effective fee is `getEffectiveFeeBps(nftContract)` locked at listing creation (consistent with B3). |
| 5 | **Soulbound editions sellable.** Primary issuance of a soulbound edition is allowed; the existing "cannot list soulbound" guard is a *resale* protection and does not apply to primary mint-spot listings. |

## Section 1 — State & entrypoints

State `msp|<id>|...`: `s`(lister/creator),`nc`,`ti`(tokenId),`pt`(paymentToken),
`p`(pricePerSpot, money),`ms`(maxSpots, uint64, 0 = unlimited up to edition
cap),`sold`(uint64, cumulative minted via this listing),`act`("1"/"0"),
`exp`(uint64),`sb`(startBlock uint64, optional),`fb`(locked effective fee bps).
Counter `nxt_msp`.

- `//go:wasmexport listMintSpots` `ListMintSpots`: assertInit; assertNotPaused;
  caller; parse; require `nc/ti/pt != ""`; `price := parseMoney(pricePerSpot)`
  non-zero; `assertPaymentTokenAllowed(pt)`; `assertCollectionAllowed(nc)`;
  `nftGetOwner(nc) == caller` else abort
  `"Only collection owner can list mint spots"`;
  `nftIsApprovedForAll(nc, caller, getContractAddress())` else abort
  `"Marketplace not approved as operator for this NFT collection"`; edition
  must exist — `nftMaxSupplyOf(nc, ti) > 0` else abort
  `"Edition not defined"` (read via the existing nft raw-state max-supply
  getter pattern used elsewhere, or the nft `exists`/`maxSupply` ABI — see
  Section 3 verification); `feeBps := getEffectiveFeeBps(nc)`;
  `feeBps > 10000` guarded; store fields incl `fb=feeBps`, `sold=0`,
  `act=1`; `nxt_msp++`; `emitMintSpotsListed`; return
  `CreatedResponse{Success:true, Id:id}`.
- `//go:wasmexport buyMintSpot` `BuyMintSpot`: assertInit; assertNotPaused;
  buyer = getCaller; parse `{listingId, amount}`; require listing
  `act=="1"` else `"Mint spot listing not active"`; not expired (`exp`);
  if `sb != 0 && block.height < sb` abort `"Listing not started"`;
  `assertCollectionAllowed(nc)`; `amount > 0`; lister = `s`;
  `buyer != lister` else `"Lister cannot buy own mint spots"`; if
  `ms != 0` require `sold + amount <= ms` else abort
  `"Exceeds listing mint-spot cap"`; `price := getMintSpotMoney(id,"p")`;
  `total := mMulU64(price, amount)`; `received := escrowIn(pt, buyer,
  total)`; `fee, _, creatorPay := feeAndRoyaltyOf(received,
  getMintSpotUint64(id,"fb"), nil, nil)`; **delegated mint**:
  `nftDelegatedMint(nc, buyer, ti, amount)` (calls the nft `mint` ABI;
  the nft contract enforces `maxSupply` and aborts → whole tx reverts);
  then if `!mIsZero(fee)` `tokenTransferBig(pt, getFeeRecipient(), fee)`;
  if `!mIsZero(creatorPay)` `tokenTransferBig(pt, lister, creatorPay)`;
  `sold += amount` (`setMintSpotUint64(id,"sold",...)`); if
  `ms != 0 && sold == ms` set `act=0`; `emitMintSpotBought(id, buyer,
  amount, formatMoney(received), formatMoney(fee))`; return
  `SuccessResponse{Success:true}`. (Mint BEFORE payouts so any
  mint/cap/approval-revoked abort reverts the escrow leg — atomic,
  mirrors `doBuy`'s NFT-before-payout ordering.)
- `//go:wasmexport delistMintSpots` `DelistMintSpots`: assertInit (works
  while paused — recovery, like `Delist`); caller; parse; listing active;
  `caller == s` else `"Only lister can delist mint spots"`; `act=0`; no
  external call; `emitMintSpotsDelisted`; SuccessResponse.
- `//go:wasmexport getMintSpotListing` `GetMintSpotListing`: assertInit;
  parse; build `MintSpotListingResponse`.

## Section 2 — Cross-contract delegated-mint wrapper

`contract/crosschain.go` gains:
```go
// nftDelegatedMint mints `amount` of a pre-defined edition (id) to `to`
// on the nft contract via the documented delegated-mint ABI. The market
// must be an approved operator of the collection owner (nft contract
// enforces isApprovedOrOwner) and the nft contract enforces maxSupply
// (aborts → whole tx reverts). Verified ABI: see Section 3.
func nftDelegatedMint(nftContract, to, tokenId string, amount uint64) {
	payload := `{"to":"` + to + `","id":"` + tokenId + `","amount":` + <uint64 decimal> + `}`
	if sdk.ContractCallSimple(nftContract, "mint", payload) == nil {
		sdk.Abort("delegated mint call failed")
	}
}
```
Exact `mint` payload field names/types are pinned by a planning-time
verification task against `magi_nft-contract` `MintPayload` (analogous to
F0). For a *pre-defined* edition the nft "subsequent-mint" path needs only
`{to,id,amount}` (`maxSupply` already set at define; per the nft spec it is
ignored on subsequent mint). The injection-allowlist lesson from F applies:
`to` is the authenticated buyer address (runtime-set, not freeform);
`tokenId` is lister-supplied and concatenated into JSON, so it is
`[a-zA-Z0-9:_-]`-allowlisted at `listMintSpots` (reuse the F1/F2 allowlist
pattern; abort `"tokenId contains invalid characters"`).

## Section 3 — Dependency verification (planning-time, no external-repo change)

A planning task records, from `/home/dockeruser/magi/magi_nft-contract`
(branch `feat/editioned-define-delegated-mint` once implemented; today the
spec): the exact `mint` `//go:wasmexport` name + `MintPayload` JSON field
names/types, and the nft state-key or ABI for "edition exists / maxSupply"
that `listMintSpots` uses for the `"Edition not defined"` check. If the real
ABI differs from the documented assumption, only the `nftDelegatedMint`
wrapper + the existence-check call adjust (design unchanged). **The market
side does not modify the nft repo.**

## Section 4 — Testing

- New `test/mocks/mintnftmock/` (same scaffold as utxomock/dexmock; package
  main, hand-rolled JSON, no tinyjson): models the *post-feature* nft
  contract minimally — `init`, owner-only `define {id,maxSupply,...}`
  (sets maxSupply, 0 supply), `setApprovalForAll {operator,approved}`,
  `mint {to,id,amount}` enforcing operator-or-owner auth + the `maxSupply`
  cap (abort "Would exceed max supply" past cap), balance/`exists`/
  `maxSupply` query (mirroring the real getters magi-market reads). NOT
  named to collide; registered in `helpers_test.go`.
- New `test/mintspots_test.go`: define an edition on the mock (maxSupply 5);
  owner `setApprovalForAll(market,true)`; `listMintSpots`; buyers
  `buyMintSpot` — assert tokens minted to buyers (mock balances), creator
  paid `received − fee`, fee→feeRecipient, market residual 0; minting past
  `maxSupply` aborts and the whole buy reverts (buyer refunded, no mint);
  optional `maxSpots` cap enforced; non-owner `listMintSpots` rejected;
  market-not-approved rejected; denied collection blocks list + buy;
  delist by lister stops further buys; soulbound edition still sellable;
  start-block gating.
- The prior 306-test suite stays green (purely additive).

## Out of scope / dropped (YAGNI)

`MintSeries`-range mint-spot listings; market-side define (owner-only,
off-market); royalty on primary mint; per-buyer purchase limits; allowlist/
phased drops; modifying `magi_nft-contract`.

## Risks

- **Hard dependency (primary risk):** the nft delegated-mint/define feature
  is unimplemented. The market side is mock-proven against the documented
  ABI; **real integration is blocked until the upstream feature ships**, at
  which point a verification task must rebuild the harness `nft.wasm` from
  the implemented `feat/editioned-define-delegated-mint` and re-run the
  mint-spot suite against it. ABI drift before merge → only the
  `nftDelegatedMint` wrapper + existence check change.
- Inherited cross-contract coupling (raw-state decoders / F ABIs) unchanged
  by this sub-project; the nft `mint` call uses the stable method ABI.
- Supply correctness delegated to the nft contract's `maxSupply`
  enforcement (atomic-revert); the market's `maxSpots` is a soft
  listing-level cap only.

### Implemented coupling surface (supersedes the earlier hedge)

The edition-existence check is implemented as the **stable `maxSupply` ABI
call** `nftMaxSupplyOf` (`ContractCallSimple(nftContract,"maxSupply",…)` →
`{"maxSupply":"<dec>"}`), NOT a raw-state read — the safer choice; the
earlier "raw-state getter OR ABI" wording in Section 3 is resolved to the
ABI path. The exact coupling surface to the (still unimplemented) real
magi_nft is therefore:

1. `mint` ABI — payload `{"to","id","amount":<uint64>}`; the real nft
   MUST `sdk.Abort` (so `ContractCallSimple` returns nil) on every failure
   (cap exceeded / not approved / undefined). `nftDelegatedMint` treats any
   non-nil result as success — a real nft that soft-errors with a non-nil
   envelope instead of aborting would let the market pay out against an
   un-minted edition.
2. `maxSupply` ABI — response envelope EXACTLY `{"maxSupply":"<dec>"}`;
   any other shape parses as 0 ⇒ fail-safe `"Edition not defined"` at list
   time (no fund risk, list-only).
3. **Raw-state keys (the one surface a mock cannot falsify):** the lister
   gate reuses the inherited `nftGetOwner` (raw key `owner`) and
   `nftIsApprovedForAll` (raw key `op|<owner>|<operator>`="1"). If the real
   magi_nft stores owner/operator-approval under different keys, the mock
   suite still passes but `listMintSpots` silently mis-authorizes.

### Mandatory real-nft integration verification (production gate — not a branch blocker)

Before any production reliance on mint-spot selling, once
`feat/editioned-define-delegated-mint` is actually implemented upstream:
(a) rebuild the harness `nft.wasm` from the implemented feature and re-run
the mint-spot suite against the real contract; (b) **inspect the real
magi_nft state schema** to confirm `owner` and `op|<owner>|<operator>`
exactly match what `nftGetOwner`/`nftIsApprovedForAll` read (surface #3 —
not covered by ABI stability, not falsifiable by the mock); (c) confirm the
real `mint` **aborts** (not soft-errors) on every failure path (surface
#1); (d) confirm the `maxSupply` response envelope (surface #2). Only the
`nftDelegatedMint` wrapper + `nftMaxSupplyOf` parser change if an ABI
drifts; a raw-state-key mismatch (surface #3) is the highest-attention
item.
