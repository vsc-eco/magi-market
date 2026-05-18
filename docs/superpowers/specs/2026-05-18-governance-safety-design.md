# magi-market — Governance & Safety Hardening (Sub-project A)

**Date:** 2026-05-18
**Status:** Design approved, pending spec review
**Repo:** `magi-market`, branch `feature/governance-safety` (off `feature/latest-contract-utxo-compat` @ `0ed11b9`)

## Context

First sub-project of a broader functionality program (decomposed into A–F;
A first because it is low-risk, isolated, and hardens the fund-handling
contract before value-moving features are added). The contract is the
post-rework magi-market: approval-custody listings/offers, escrow auctions,
big.Int amounts, balance-delta accounting, raw-state cross-contract getters
(252 tests green at `0ed11b9`).

## Scope (YAGNI-trimmed during brainstorming)

Sub-project A delivers exactly two features. The originally-proposed
keeper-incentivized expired sweeper was **dropped**: post approval-custody,
expired listings lock zero value, and `cancelOffer` already lets anyone
cancel an expired offer — there is no trapped value to justify a bounty or
the added surface. An admin timelock was **dropped**: the user chose
2-step ownership transfer alone (timelocking the wrong actions is a
liability; the 2-step flow already defeats fat-finger/malicious owner
hand-off).

1. **2-step owner transfer.**
2. **Retroactive collection denylist** (default-open; owner can block scam
   collections, which also stops trading of their already-active entries).

## Decisions (locked during brainstorming)

| Topic | Decision |
|---|---|
| Sweeper | Dropped (no trapped value post approval-custody). |
| Timelock | Dropped (2-step owner transfer only). |
| Owner transfer | 2-step: propose (`changeOwner`) → new owner `acceptOwnership`; owner can `cancelOwnershipTransfer`. |
| Denylist mode | Default-open denylist (not allowlist/curation). |
| Denylist enforcement | **Retroactive**: blocks creation AND completion (buy/bid/accept) on already-active entries of a denied collection. |
| Recovery | Cancel/delist/emergency paths never gated → denied collections can never trap a seller's NFT or a buyer's escrow. |
| Surface | Approach B: minimal logic + the read/UX surface (queries + dedicated events + explicit cancel). |

## Section 1 — 2-step owner transfer

**State:** new key `pending_owner` (string; absent/empty = none). `owner`
is unchanged until the transfer is accepted.

**Entrypoints:**

- `changeOwner {newOwner}` — owner-only, init-required. Abort if
  `newOwner == ""`. Sets `pending_owner = newOwner` (does **not** mutate
  `owner`). Emits `OwnerTransferInitiated{currentOwner, pendingOwner}`.
  Re-calling overwrites the pending candidate.
- `acceptOwnership` (empty payload) — init-required. Abort if no
  `pending_owner` set ("No pending ownership transfer"). Caller **must
  equal** `pending_owner` else abort ("Not the pending owner"). Sets
  `owner = pending_owner`, clears `pending_owner`, emits the existing
  `OwnerChange{previousOwner, newOwner}`.
- `cancelOwnershipTransfer` (empty payload) — owner-only, init-required.
  Abort if no pending. Clears `pending_owner`. Emits
  `OwnerTransferCancelled{by}`.
- `getPendingOwner` — query → `{pendingOwner}` (empty string if none).

**Behavior / edge cases:**
- Ownership only moves on the *new* owner's explicit `acceptOwnership` —
  prevents hand-off to a wrong/dead/uncontrolled address.
- All four work regardless of pause (governance must function while paused).
- `getOwner` / `getInfo` continue to report the **current** owner only.
- No timelock.

## Section 2 — Retroactive collection denylist

**State:** `dl|<nftContract>` = `"1"` when denied; key absent = allowed
(default-open). Helpers: `isCollectionDenied(nftContract) bool`;
`assertCollectionAllowed(nftContract)` → abort `"Collection is denied"`
if denied.

**Admin entrypoints (owner-only, init-required, IMMEDIATE — must block a
scam instantly; no timelock):**

- `denyCollection {nftContract}` — abort if `nftContract == ""`. Sets
  `dl|<nftContract>="1"`. Emits `CollectionDenied{nftContract, by}`.
  Idempotent.
- `allowCollection {nftContract}` — deletes the key. Emits
  `CollectionAllowed{nftContract, by}`. Idempotent.
- `isCollectionDenied {nftContract}` — query → `{denied:bool}`.

**Enforcement — `assertCollectionAllowed(nftContract)` is called at:**

- *Creation:* `doList` (covers `list` + `batchList` per item),
  `CreateAuction`, `MakeOffer`.
- *Completion (the retroactive part):* `doBuy` (covers `buy` +
  `batchBuy` per item), `PlaceBid`, `doAcceptOffer` (covers `acceptOffer`
  + `acceptCollectionOffer`).

Once a collection is denied, its already-active listings/auctions/offers
can no longer be bought / bid / accepted.

**Deliberately NOT gated (recovery must always work for a denied
collection, so value is never trapped):** `delist`, `cancelAuction`,
`cancelOffer`, `updateListing`, `emergencyWithdraw`.

**SettleAuction interaction (the one non-trivial edge).** Auctions escrow
the NFT, so a denied+ended auction must still resolve without trapping the
escrowed NFT/bid. `SettleAuction` becomes deny-aware:
- If the auction's collection is **denied**: treat as a no-sale —
  return the escrowed NFT to the seller, refund the high bidder (if any)
  their escrowed bid, mark settled. Do **not** transfer to the winner or
  pay the seller. Emit the existing settled/no-sale events accordingly.
- If allowed: unchanged behavior (winner gets NFT, seller paid; or
  no-bids → NFT back to seller).
- `cancelAuction` (seller, pre-settle, no-bids rule unchanged) remains
  available regardless of denial.

## Section 3 — Testing, build constraints, file layout

**Build/tinyjson (inherited, unchanged):** `GOTOOLCHAIN=go1.25.3`,
gitignored `go.work`, tinygo `wasm-unknown`. tinyjson cannot regenerate
(package-main) — `contract/types_tinyjson.go` is hand-maintained. New
structs (`CollectionPayload{nftContract}`, `PendingOwnerResponse
{pendingOwner}`, `CollectionDeniedResponse{denied}`, and the new event +
attribute structs) get hand-written `MarshalTinyJSON`/`UnmarshalTinyJSON`
mirroring proven in-file patterns (`GetRoyaltyPayload` for a single
string-field payload; `PausedResponse` for a bool response;
`OwnerChangeEvent` for an event). `acceptOwnership`/
`cancelOwnershipTransfer` take an empty payload (no struct). Correctness
is proven by tinygo build + the full wasm suite (the regression oracle).

**File layout:**
- `internal.go` — `pending_owner` get/set/clear helpers; denylist
  `isCollectionDenied` / `assertCollectionAllowed` + `dl|` state helpers.
- `market.go` — rewrite `ChangeOwner` (now propose-only); add
  `AcceptOwnership`, `CancelOwnershipTransfer`, `GetPendingOwner`,
  `DenyCollection`, `AllowCollection`, `IsCollectionDenied`; insert
  `assertCollectionAllowed` in `doList`, `doBuy`, `MakeOffer`,
  `doAcceptOffer`.
- `auction.go` — `assertCollectionAllowed` in `CreateAuction`,
  `PlaceBid`; deny-aware unwind in `SettleAuction`.
- `events.go` — `emitOwnerTransferInitiated`,
  `emitOwnerTransferCancelled`, `emitCollectionDenied`,
  `emitCollectionAllowed`.
- `types.go` / `types_tinyjson.go` — new payload/response/event structs
  (hand-edited per the inherited constraint).

**Tests (new `test/governance_test.go`):**
- *2-step transfer:* `changeOwner` sets pending, `owner` unchanged;
  non-pending caller `acceptOwnership` rejected; pending caller succeeds
  and `owner` moves; `cancelOwnershipTransfer` clears pending;
  `acceptOwnership` with no pending rejected; all four operate while
  paused; `getPendingOwner` reflects state.
- *Denylist:* deny blocks new `list`/`createAuction`/`makeOffer`; deny
  **retroactively** blocks `buy`/`placeBid`/`acceptOffer` on a
  pre-existing active entry; `delist`/`cancelAuction`/`cancelOffer`
  still succeed for a denied collection (recovery, no trapped value);
  `settleAuction` on a denied+ended auction returns NFT to seller +
  refunds the high bidder; `allowCollection` re-enables trading;
  non-owner `denyCollection`/`allowCollection` rejected;
  `isCollectionDenied` query correct.
- The existing 252-test suite stays green (additive guards; existing
  tests use non-denied collections and the immediate-`changeOwner`
  expectation is updated to the 2-step flow where exercised).

## Out of scope

- Keeper sweeper, admin timelock, allowlist/curation, per-collection
  status enum (all explicitly dropped/declined).
- Sub-projects B–F (fee/royalty engine, trading mechanics, swaps,
  rental, cross-chain settlement) — separate spec → plan → execution
  cycles.

## Risks / notes

- Existing tests that call `changeOwner` and expect the owner to change
  immediately must be updated to the 2-step flow (`changeOwner` then
  `acceptOwnership`). This is a deliberate behavior change, not a
  regression — surfaced here so the plan budgets for it.
- No new cross-contract coupling (this sub-project does not touch the
  raw-state-read getters or their accepted internal-storage coupling).
- Fresh-deploy assumption from the prior spec still holds; `pending_owner`
  and `dl|` keys are new and absent ⇒ safe defaults (no pending, allowed).
