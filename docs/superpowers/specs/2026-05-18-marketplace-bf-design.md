# magi-market — Broader-Functionality Expansion (Sub-projects B–F)

**Date:** 2026-05-18
**Status:** Design approved (single consolidated approval), pending implementation
**Repo:** `magi-market`, branch `feature/marketplace-bf` (off `feature/governance-safety` @ `0327bde`)

## Context

Sub-projects B–F of the A–F decomposition. A (governance & safety) shipped
(268 tests green). The contract is the post-rework magi-market:
approval-custody listings/offers, escrow auctions, big.Int decimal-string
amounts, balance-delta accounting, raw-state cross-contract getters,
2-step owner transfer, retroactive collection denylist. User directive:
deliver B–F fast, one consolidated spec/plan, executed back-to-back in
dependency order (B→C→D→E→F), minimal questions — all design decisions
made by the implementer with YAGNI defaults, single approval given.

## Cross-cutting invariants (apply to every sub-project)

- All monetary values are big.Int decimal strings; reuse `parseMoney`/
  `formatMoney`/`mMulU64`/`mMulBpsDiv`/`mCmp`/`mIsZero`/`mAdd`/`mSub`/
  money state helpers and `distributeFeesBig`.
- All inbound payment uses `escrowIn` (balance-delta — distribute the
  actually-received amount).
- NFT movement uses approval-custody (`nftIsApprovedForAll` preflight +
  `nftSafeTransferFrom` on completion); auctions keep escrow.
- Cross-contract reads use the existing raw-state getters; cross-contract
  *writes* / new external calls (F) use the target's **stable method
  ABI**, never internal-state, to avoid deepening the documented
  internal-storage coupling risk.
- `contract/types_tinyjson.go` is HAND-MAINTAINED (generator cannot run
  on a package-main contract). New structs get hand-written
  encode/decode + wrappers mirroring existing in-file patterns.
- The collection denylist (`assertCollectionAllowed`) gates every new
  create/complete value path too.
- Every new feature is **opt-in**; all existing default code paths and
  the prior 268-test suite stay unchanged/green.
- Build/test/git invariants identical to prior sub-projects
  (`GOTOOLCHAIN=go1.25.3`, gitignored `go.work`, on-branch commits,
  never run tinyjson, suite is the regression oracle).

## Sub-project B — Fee/Royalty engine

**B1. Multi-recipient royalty splits.** New collection-owner-only
`setRoyaltySplits {nftContract, splits:[{recipient,bps}…]}`:
- ≤ 10 split entries; each `bps` > 0; Σ`bps` ≤ 5000 (existing royalty
  cap). Σ`bps` IS the collection's effective royalty total.
- Stored under `rsplit|<nftContract>` keys (count + per-index
  recipient/bps). Existing `setRoyalty` remains and is internally
  normalized to a single-entry split (full backward compatibility;
  `getRoyalty` still returns the legacy view = total bps + first/primary
  recipient).
- Every payout path (buy, offer accept, auction settle, Dutch buy,
  bundle, sweep) replaces the single-royalty transfer with a loop:
  for each split, `royaltyPart = mMulBpsDiv(received, split.bps)`,
  `tokenTransferBig(paymentToken, split.recipient, royaltyPart)`. Seller
  payment = received − fee − ΣroyaltyParts (via `mSub`), preserving the
  existing "distribute only what was received" invariant. Royalty bps
  are locked at listing/offer/auction creation exactly as today (read
  the splits snapshot at creation, store the resolved splits with the
  entry so later split changes don't alter in-flight trades).

**B2. Per-collection fee override.** New owner-only
`setCollectionFee {nftContract, feeBps}` (feeBps ≤ 10000) and
`clearCollectionFee {nftContract}`; `getEffectiveFee {nftContract}`
query. Fee resolution at entry-creation time: per-collection override if
set, else the global `fee_bps`. Locked into the listing/offer/auction at
creation like the current `fb` field. Combined fee+royalty ≤ 10000 check
still enforced at creation.

**Dropped (YAGNI):** volume-tiered fees, maker/taker fees (stateful
per-user volume accounting; per-collection override delivers comparable
control without the gas/state surface).

## Sub-project C — Trading mechanics

**C1. Bundles (single-collection, atomic).** New
`listBundle {nftContract, items:[{tokenId,amount}…], paymentToken,
price, expirationBlock}` (approval-custody: seller must have approved
the market operator for `nftContract`; ≤ 20 items). Stored as a bundle
entry. `buyBundle {bundleId}`: balance-delta escrow of `price`;
fee+royalty resolved against `nftContract`; **all** items transferred
seller→buyer atomically (any leg abort reverts the whole tx — no
partial bundle). `delistBundle {bundleId}` (seller). Cross-collection
bundles are out of scope (YAGNI defer).

**C2. Floor sweep.** New `sweep {nftContract, listingIds:[…],
maxTotal}`: iterate the given existing single listings; require each
listing's `nc == nftContract` and active/unexpired; sum their costs;
abort `"Sweep exceeds maxTotal"` if Σ > `maxTotal` (slippage guard);
otherwise execute each as a normal buy (reusing `doBuy`) atomically.
Caller is the buyer.

**C3. Scheduled listings.** Add optional `startBlock` (uint64, 0 =
immediate) to `ListPayload`/listing state (`sb`). `doBuy` aborts
`"Listing not started"` while `startBlock != 0 && block.height <
startBlock`. Creation still escrow-free (approval model). `getListing`
returns `startBlock`.

**Dropped (YAGNI):** sealed-bid / commit-reveal auctions.

## Sub-project D — NFT-for-NFT swap

New entrypoints mirroring the offer lifecycle (approval-custody, no NFT
escrow):
- `proposeSwap {offeredNft, offeredTokenId, offeredAmount, wantedNft,
  wantedTokenId, wantedAmount, topUp, topUpToken, expirationBlock}` —
  proposer must have approved the market operator for `offeredNft` and
  hold `offeredAmount`. `topUp` (big.Int string, may be "0") is paid by
  the **proposer** to the acceptor; if `topUp > 0`, `topUpToken` must be
  an allowed payment token. Stored as a swap entry; emits `swapProposed`.
- `acceptSwap {swapId}` — caller (acceptor) must hold `wantedAmount` of
  `wantedNft` and have approved the market operator for it. Not expired,
  active, `caller != proposer`. Both collections must pass
  `assertCollectionAllowed`. Execution: market `safeTransferFrom`
  proposer→acceptor (offered NFT) and acceptor→proposer (wanted NFT);
  if `topUp > 0`, balance-delta `escrowIn(topUpToken, proposer, topUp)`
  then pay acceptor `received` minus marketplace fee
  (`distributeFeesBig` with royaltyBps=0 — **no royalty on barter**,
  documented). Emits `swapAccepted`. Atomic (any leg abort reverts).
- `cancelSwap {swapId}` — proposer only (or anyone if expired); marks
  inactive; emits `swapCancelled`. No escrow to refund (top-up is only
  pulled at accept time).
- `getSwap {swapId}` query.

## Sub-project E — NFT rental (escrow-backed rights attestation)

A contract cannot force return of an NFT it does not hold, so rental is
modeled as an **escrow-backed on-chain rights record**, never an NFT
hand-over:
- `listRental {nftContract, tokenId, amount, paymentToken,
  pricePerBlock, minBlocks, maxBlocks}` — owner must have approved the
  market operator + hold `amount`. Stored as a rental listing; emits
  `rentalListed`. `delistRental {rentalId}` (owner; only if not
  currently rented).
- `rent {rentalId, blocks}` — `minBlocks ≤ blocks ≤ maxBlocks`,
  collection not denied, rental not already active. Cost =
  `pricePerBlock × blocks`. Balance-delta `escrowIn(paymentToken,
  renter, cost)`; the NFT is escrowed owner→market for the term; pay the
  owner `received` minus marketplace fee + royalty splits
  (`distributeFeesBig` + B1 loop). Create rental record:
  `renter`, `until = block.height + blocks`, `active`. Emits `rented`.
- `endRental {rentalId}` — callable by anyone once `block.height ≥
  until` (or by the owner any time after `until`); returns the escrowed
  NFT owner-ward (to the original lister/owner address), clears the
  active record. Emits `rentalEnded`.
- `endRentalEarly {rentalId}` — renter only; ends the record and returns
  the NFT to the owner immediately; **no refund** of the unused term
  (simplicity; documented). Emits `rentalEnded`.
- Queries: `getRental {rentalId}`; `getActiveRentalOf {account,
  nftContract, tokenId}` → `{active, until}` so games/dapps can grant
  in-app rights to the current renter.
- No deposit (the NFT never leaves escrow, so the renter cannot damage
  it — YAGNI).

## Sub-project F — Cross-chain settlement (opt-in, stable ABIs only)

Both features are per-listing opt-in; default path unchanged. F uses the
**stable method ABIs** of utxo-mapping and the Magi DEX router —
explicitly NOT internal-state reads (avoids compounding the documented
coupling risk). The exact ABIs (utxo `unmap`/`unmapFrom`; DEX router
swap method name + payload + min-out field) are a **planning-time
verification task** against the live contracts — not a redesign; if an
ABI differs from assumption, adjust the call wrapper only.

**F1. Native L1 payout.** Optional listing fields `payoutMode`
("default" | "unmap") and `payoutL1Address`. When `"unmap"` and the
listing's `paymentToken` is a UTXO-mapping contract: on sale, after
computing the seller's net (received − fee − royalty), instead of
`tokenTransferBig(seller, …)` the market calls the utxo-mapping
contract's `unmap`/`unmapFrom` method to send the seller's net as real
L1 coin to `payoutL1Address`. Fee/royalty recipients are still paid in
the mapped token as today. If `unmap` fails the whole tx aborts (buyer
refunded by revert).

**F2. DEX-routed settlement.** Optional listing fields `settleToken`
(contract id, "" = disabled) and `minSettleOut` (big.Int string). When
set and `settleToken != paymentToken`: after balance-delta escrow, the
market swaps the seller's net payment-token amount to `settleToken` via
the Magi DEX router method ABI with `minSettleOut` as the slippage
floor; the resulting `settleToken` amount (balance-delta measured) is
paid to the seller. Swap shortfall / router failure → whole tx aborts.
Fee/royalty stay in `paymentToken`. F1 and F2 are mutually exclusive per
listing (validated at creation).

## Testing

Per sub-project, extend the wasm harness. New mocks as needed:
- B: multi-recipient royalty distribution exactness (Σparts + fee +
  seller == received), per-collection fee override resolution + locking.
- C: bundle atomic all-or-nothing (one bad leg reverts all); sweep
  slippage guard + atomicity; scheduled listing not buyable pre-start.
- D: swap happy path (both legs + top-up), denylist on either side,
  cancel/expire, fee-on-top-up via balance-delta.
- E: rent escrows NFT + pays owner; `getActiveRentalOf` reflects term;
  `endRental` only after `until`; `endRentalEarly` returns NFT no
  refund; double-rent blocked; denylist blocks `rent`.
- F: a `dexmock` router (swap with min-out, fee/slippage) + reuse
  `utxomock` (extend with `unmap`/`unmapFrom`) — prove unmap payout and
  DEX-routed payout end-to-end, slippage abort reverts cleanly.
- The full prior 268-suite stays green throughout (all new behavior is
  opt-in; new entrypoints/fields default to legacy behavior).

## Out of scope / dropped

Volume-tier & maker/taker fees; cross-collection bundles; sealed-bid
auctions; rental deposits; rental as NFT-hand-over (unenforceable);
royalty on pure barter; lazy-mint (user-excluded). Internal-state reads
for F (stable ABI only).

## Risks (carried forward / new)

- Inherited: raw-read getters' internal-storage coupling (re-verify
  before upgrading magi_nft/magi_token/utxo-mapping).
- F depends on utxo-mapping `unmap`/`unmapFrom` and DEX router ABIs
  being stable & as-verified; mitigated by stable-ABI choice + a
  planning verification task + balance-delta measurement of swap output.
- E rental rights are on-chain attestations; in-app enforcement is the
  consuming dapp's responsibility (documented expectation).
- Contract surface grows substantially; mitigated by opt-in defaults,
  reuse of existing primitives, and the suite-green regression oracle.
