# magi-market B–F Expansion Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Add fee/royalty engine, trading mechanics, NFT-for-NFT swap, NFT rental, and cross-chain settlement to magi-market — all opt-in, preserving every existing default path and the 268-test baseline.

**Architecture:** Five task-groups B→C→D→E→F executed in dependency order on `feature/marketplace-bf` (off `feature/governance-safety` @ `0327bde`). Every feature reuses the established primitives (`escrowIn` balance-delta, big.Int money helpers, `distributeFeesBig`, approval-custody `nftSafeTransferFrom`, `assertCollectionAllowed`, hand-maintained `types_tinyjson.go`). Cross-contract *writes* in F use the targets' stable method ABIs (verify at plan-exec time, never internal state).

**Tech Stack:** Go (tinygo `wasm-unknown`), hand-maintained CosmWasm tinyjson, vsc-node wasm harness.

**Spec:** `docs/superpowers/specs/2026-05-18-marketplace-bf-design.md`

## Invariants (every build/test/commit step assumes these)
- `export GOTOOLCHAIN=go1.25.3`; gitignored `go.work` (never commit); WASM build `tinygo build -gc=custom -scheduler=none -panic=trap -no-debug -target=wasm-unknown -o test/artifacts/main.wasm ./contract`; test `GOTOOLCHAIN=go1.25.3 go test ./test/ -count=1`.
- **Never run tinyjson.** New structs → hand-append encode/decode + wrappers to `contract/types_tinyjson.go` mirroring the existing `tinyjsonGov*` hand-written block added in sub-project A (single-string payload → mirror `tinyjsonGovDecodeCollectionPayload`; bool response → mirror `tinyjsonGovEncodeCollectionDeniedResponse`; event → mirror `tinyjsonGovEncodeCollectionDenied`; multi-field/array payloads → mirror an existing generated struct with a slice, e.g. `BatchListPayload`/`Items` decode pattern already in the file). Build + full suite is the correctness oracle.
- Git: stay ON `feature/marketplace-bf`; normal `git commit` (never `--amend` across tasks / never detach); before each commit `git branch --show-current` must be `feature/marketplace-bf`; after, `git rev-parse feature/marketplace-bf` == HEAD; if HEAD detaches STOP+report. Commit msgs multi-line, NO Co-Authored-By/Claude attribution. No PR.
- All new behavior is opt-in: existing payload structs gain only OPTIONAL fields (absent ⇒ legacy behavior); new entrypoints are additive. The prior 268 tests MUST stay green after every task (only add tests; do not change existing tests' semantics).
- Every NEW create path calls `assertCollectionAllowed(nftContract)`; every NEW completion path calls it too (retroactive denylist parity).
- Money: `parseMoney/formatMoney/mZero/mAdd/mSub/mMulU64/mMulBpsDiv/mCmp/mIsZero`, money state helpers, `escrowIn`, `tokenTransferBig`, `distributeFeesBig`, `getFeeBps/getRoyaltyBps/getRoyaltyRecipient`. Read `contract/internal.go` + `contract/market.go` `doBuy`/`MakeOffer`/`doAcceptOffer` and `contract/auction.go` `SettleAuction` before writing — new code MIRRORS those exactly, changing only what each task specifies.

## Task 0: baseline
- [ ] Verify `git branch --show-current`==`feature/marketplace-bf`, build BUILD_OK, `go test ./test/` → 268 pass / 0 fail. If not green, STOP.

---

# GROUP B — Fee/Royalty engine (do first; later groups reuse `distributeRoyaltySplits`)

### Task B1: Royalty-split state + setRoyaltySplits + getRoyaltySplits

**Files:** `contract/types.go`, `contract/types_tinyjson.go`, `contract/internal.go`, `contract/market.go`, `contract/events.go`, `test/feeroyalty_test.go` (new)

State keys (internal.go): `rsplit|<nft>|n` = count (uint64 via `setUint64State`); `rsplit|<nft>|<i>|r` = recipient (string); `rsplit|<nft>|<i>|b` = bps (uint64). Helpers: `setRoyaltySplits(nft, recips []string, bpss []uint64)`, `getRoyaltySplitCount(nft) uint64`, `getRoyaltySplit(nft,i) (string,uint64)`, and `resolveRoyaltySplits(nft) ([]string,[]uint64)` — if `rsplit|<nft>|n` > 0 return the stored splits; ELSE fall back to legacy single (`getRoyaltyRecipient(nft)`, `getRoyaltyBps(nft)`) as a 1-entry split (or empty if royaltyBps==0). This keeps `setRoyalty` fully backward compatible.

- [ ] **Step 1 (failing test)** `test/feeroyalty_test.go` (package `contract_test`): `TestSetRoyaltySplits` — collection owner sets 3 splits (e.g. 250/150/100 bps, recipients hive:a/b/c) for `NftContractID`; `getRoyaltySplits {nftContract}` returns them; non-collection-owner rejected ("Only collection owner can set royalty"); >10 entries rejected ("Too many royalty splits"); Σbps>5000 rejected ("Royalty must be <= 5000 basis points"). Use existing helpers (read `test/helpers_test.go`; collection owner = NFT contract's `getOwner`, see how `royalty_test.go` sets royalties). Run `-run TestSetRoyaltySplits` → FAIL (entrypoint missing).
- [ ] **Step 2** Add to `types.go`: `RoyaltySplit{Recipient string json:"recipient"; Bps uint64 json:"bps"}`; `SetRoyaltySplitsPayload{NftContract string json:"nftContract"; Splits []RoyaltySplit json:"splits"}`; `RoyaltySplitsResponse{NftContract string json:"nftContract"; Splits []RoyaltySplit json:"splits"}`; `RoyaltySplitsSetEvent`+`RoyaltySplitsSetAttributes{NftContract string; Count uint64}`.
- [ ] **Step 3** Hand-append tinyjson for those structs (mirror the array-bearing `BatchListPayload`/`Items` generated decode pattern for `[]RoyaltySplit`; events mirror `tinyjsonGovEncodeCollectionDenied`). Build BUILD_OK.
- [ ] **Step 4** internal.go: add the state helpers + `resolveRoyaltySplits`. market.go: `//go:wasmexport setRoyaltySplits` `SetRoyaltySplits` — assertInit; parse; verify caller == NFT collection owner via `nftGetOwner(p.NftContract)` (mirror existing `SetRoyalty`'s owner check exactly); `len(Splits)`∈[1,10] else abort; each `Bps>0`; Σ`Bps`≤5000 else abort; store via `setRoyaltySplits`; ALSO set legacy `setRoyaltyBps(nft, Σbps)` + `setRoyaltyRecipientState(nft, Splits[0].Recipient)` so `getRoyalty` legacy view stays coherent; `emitRoyaltySplitsSet`. `//go:wasmexport getRoyaltySplits` `GetRoyaltySplits` → `RoyaltySplitsResponse` from `resolveRoyaltySplits`. events.go: `emitRoyaltySplitsSet`.
- [ ] **Step 5** Build + `-run TestSetRoyaltySplits` PASS + full suite 268+new, 0 fail. Commit `feat(B1): multi-recipient royalty splits state + entrypoints`.

### Task B2: distributeRoyaltySplits helper + wire into all payout paths

**Files:** `contract/internal.go`, `contract/market.go`, `contract/auction.go`, `test/feeroyalty_test.go`

Add `internal.go`: `distributeRoyaltySplitsResolved(paymentToken string, total *big.Int, recips []string, bpss []uint64) *big.Int` — for each i pay `part=mMulBpsDiv(total,bpss[i])` via `tokenTransferBig(paymentToken,recips[i],part)` when `!mIsZero(part)`, return Σparts. And `feeAndRoyaltyOf(total, feeBps uint64, recips []string, bpss []uint64) (fee, royaltyTotal, sellerPayment *big.Int)`: `fee=mMulBpsDiv(total,feeBps)`, `royaltyTotal=Σ mMulBpsDiv(total,bpss[i])`, `seller=mSub(mSub(total,fee),royaltyTotal)`.

Every existing payout site (doBuy, doAcceptOffer, auction Dutch-buy in PlaceBid, SettleAuction winner branch) currently does `distributeFeesBig` + single royalty transfer. Lock splits at creation: when a listing/offer/auction is created, snapshot `resolveRoyaltySplits(nc)` into entry state (`<entity>|<id>|rs_n`, `|rs_<i>_r`, `|rs_<i>_b`) alongside the existing `rb`/`rr`. At payout, read the snapshot (fallback to legacy `rb`/`rr` if `rs_n` absent — preserves in-flight pre-B entries) and replace the single royalty payment with `distributeRoyaltySplitsResolved`; seller payment uses `feeAndRoyaltyOf`. doList/MakeOffer/CreateAuction add the snapshot writes; doBuy/doAcceptOffer/PlaceBid-dutch/SettleAuction read+distribute.

- [ ] **Step 1 (failing test)** `TestBuyWithRoyaltySplits`: collection owner sets 2 splits (300+200 bps, fee 250); list+buy 1@10000 in TokenID; assert recipient A got `mMulBpsDiv(10000,300)`, B got `200/10000`, feeRecipient got `250/10000`, seller got remainder, market residual 0, `bought` totalPrice == received. FAIL initially.
- [ ] **Step 2** Implement helpers + snapshot writes + payout wiring per above; mirror existing `distributeFeesBig` usage exactly (only the royalty leg changes from one transfer to the split loop). Keep `distributeFeesBig` for fee math.
- [ ] **Step 3** Add `TestOfferAcceptRoyaltySplits` and `TestAuctionSettleRoyaltySplits` (same shape via offer/English-auction). Build; `-run RoyaltySplits` PASS; full suite green. Commit `feat(B2): distribute multi-recipient royalty across buy/offer/auction`.

### Task B3: Per-collection fee override

**Files:** `contract/types.go`,`types_tinyjson.go`,`internal.go`,`market.go`,`events.go`,`test/feeroyalty_test.go`

State `cfee|<nft>` = bps string (absent ⇒ use global `getFeeBps()`). Helper `getEffectiveFeeBps(nft) uint64` (override if `cfee|<nft>` set else `getFeeBps()`). Replace `getFeeBps()` at the 3 creation sites (doList, MakeOffer, CreateAuction) where `fb` is locked with `getEffectiveFeeBps(nc)` (locking semantics unchanged — still snapshotted into `fb`). Add owner-only `setCollectionFee {nftContract,feeBps}` (≤10000), `clearCollectionFee {nftContract}`, query `getEffectiveFee {nftContract}`→`{feeBps}`; events `collectionFeeSet`/`collectionFeeCleared`. Combined effective-fee+royalty≤10000 check at creation uses `getEffectiveFeeBps`.

- [ ] Test `TestPerCollectionFeeOverride`: set override 100 for NftContractID, list+buy → fee uses 100 not global; clear → reverts to global; non-owner rejected. TDD: failing test → implement → PASS → full suite green → commit `feat(B3): per-collection fee override`.

---

# GROUP C — Trading mechanics

### Task C1: Scheduled listings (smallest; do first in C)

**Files:** `contract/types.go`,`types_tinyjson.go`,`market.go`,`test/trading_test.go` (new)

Add OPTIONAL `StartBlock uint64 json:"startBlock"` to `ListPayload` (absent⇒0⇒immediate). doList stores `setListingUint64(id,"sb",p.StartBlock)`. doBuy: after the active/expired checks add `if sb:=getListingUint64(p.ListingId,"sb"); sb!=0 && getCurrentBlockHeight()<sb { sdk.Abort("Listing not started") }`. `GetListing` response gains `StartBlock` (add field to `ListingResponse`+tinyjson). Regenerate tinyjson by hand (ListPayload/ListingResponse get one new uint64 field — mirror an existing uint64 field's encode/decode fragment in those structs' generated funcs).

- [ ] TDD `TestScheduledListingNotBuyableBeforeStart` (list with startBlock in future → buy aborts "Listing not started"; advance blocks → buy succeeds) + `TestListingNoStartBlockImmediate` (omitted ⇒ buyable now, regression). Failing→implement→PASS→full suite green (existing listing tests unaffected: startBlock omitted ⇒ 0)→commit `feat(C1): scheduled (start-block) listings`.

### Task C2: Floor sweep

**Files:** `contract/types.go`,`types_tinyjson.go`,`market.go`,`events.go`,`test/trading_test.go`

`SweepPayload{NftContract string; ListingIds []uint64 json:"listingIds"; MaxTotal string json:"maxTotal"}`. `//go:wasmexport sweep` `Sweep`: assertInit; assertNotPaused; caller=getCaller; parse; `maxTotal=parseMoney(p.MaxTotal)`; for each id: require `isListingActive`, not expired, `getListingField(id,"nc")==p.NftContract`; compute `cost=mMulU64(getListingMoney(id,"p"), getListingUint64(id,"a"))` (full remaining — sweep buys the full listing) accumulate; if `mCmp(total,maxTotal)>0` abort "Sweep exceeds maxTotal"; then for each id call `doBuy(caller,&BuyPayload{ListingId:id,Amount:getListingUint64(id,"a")})` (reuse — inherits balance-delta/fee/royalty/denylist/atomic-revert). Emit `swept{buyer,count,total}`.

- [ ] TDD `TestFloorSweepBuysAll` (3 listings same collection, sweep buys all, each seller paid, NFTs to buyer) + `TestFloorSweepSlippageGuard` (maxTotal below sum → abort, NO listing consumed — relies on full-revert) + `TestFloorSweepRejectsForeignCollection` (a listingId of another collection → abort). Failing→implement→PASS→full suite green→commit `feat(C2): floor sweep with slippage guard`.

### Task C3: Bundles

**Files:** `contract/types.go`,`types_tinyjson.go`,`internal.go`,`market.go`,`events.go`,`test/trading_test.go`

`BundleItem{TokenId string json:"tokenId"; Amount uint64 json:"amount"}`; `ListBundlePayload{NftContract,PaymentToken string; Items []BundleItem json:"items"; Price string json:"price"; ExpirationBlock uint64 json:"expirationBlock"}`; `BundleResponse`. State `bnd|<id>|...`: s(seller),nc,pt,p(price money),act,exp, items count + per-item ti/amt, plus fee/royalty-split snapshot like B2. `nxt_bnd` counter. Entrypoints: `listBundle` (approval-custody preflight: `nftIsApprovedForAll(nc,caller,market)` + `nftBalanceOf(nc,caller,ti)≥amt` for every item; `assertCollectionAllowed`; ≤20 items; price>0; lock effective fee + royalty splits), `buyBundle {bundleId}` (mirror doBuy: balance-delta `escrowIn(pt,caller,price)`, `feeAndRoyaltyOf`, transfer EACH item seller→buyer via `nftSafeTransferFrom`, then fee + `distributeRoyaltySplitsResolved` + seller payment, mark act=0, emit `bundleBought`), `delistBundle {bundleId}` (seller, act=0, no NFT movement — approval-custody), `getBundle`. Denylist gates listBundle + buyBundle.

- [ ] TDD `TestBundleAtomicBuy` (3-item bundle, buy → all 3 NFTs to buyer, seller paid net, fee+royalty correct, market residual 0) + `TestBundleOneItemMissingRevertsAll` (seller transfers one item away after listing → buyBundle aborts, NO item moved, escrow returned — full-revert) + `TestDelistBundleNoNftMove` + `TestBundleDeniedCollectionBlocked`. Failing→implement→PASS→full suite green→commit `feat(C3): single-collection atomic bundles`.

---

# GROUP D — NFT-for-NFT swap

### Task D1: swap lifecycle

**Files:** `contract/types.go`,`types_tinyjson.go`,`internal.go`,`market.go`,`events.go`,`test/swap_test.go` (new)

`ProposeSwapPayload{OfferedNft,OfferedTokenId string; OfferedAmount uint64; WantedNft,WantedTokenId string; WantedAmount uint64; TopUp string json:"topUp"; TopUpToken string json:"topUpToken"; ExpirationBlock uint64}`; `SwapIdPayload{SwapId uint64}`; `SwapResponse`. State `sw|<id>|...`: p(proposer),on,oti,oa,wn,wti,wa,tu(topUp money),tt(topUpToken),act,exp. `nxt_sw`. Events swapProposed/Accepted/Cancelled. Entrypoints (mirror MakeOffer/CancelOffer/doAcceptOffer lifecycle):
- `proposeSwap`: assertInit; assertNotPaused; parse; require offered/wanted nft+tokenId non-empty, amounts>0; `assertCollectionAllowed(OfferedNft)`; proposer approval+balance preflight on OfferedNft; if `parseMoney(TopUp)`>0 require `TopUpToken` set + `assertPaymentTokenAllowed(TopUpToken)`; store; emit swapProposed; return CreatedResponse{id}.
- `acceptSwap`: assertInit; assertNotPaused; caller=acceptor; load swap, require active+not expired+`caller!=proposer`; `assertCollectionAllowed(on)` AND `assertCollectionAllowed(wn)`; preflight: proposer still approved+holds offered; acceptor approved+holds wanted; if topUp>0 `received=escrowIn(tt,proposer,topUp)` then `fee,_,acceptorPay=feeAndRoyaltyOf(received,getEffectiveFeeBps(on),nil,nil)` (royalty nil ⇒ 0 — no royalty on barter), pay feeRecipient `fee`, pay acceptor `acceptorPay`; `nftSafeTransferFrom(on,proposer,acceptor,oti,oa)`; `nftSafeTransferFrom(wn,acceptor,proposer,wti,wa)`; act=0; emit swapAccepted. (All-or-nothing via abort-revert.)
- `cancelSwap`: proposer (or anyone if expired); act=0; emit. No escrow (topUp pulled only at accept).
- `getSwap`.

- [ ] TDD `test/swap_test.go`: `TestSwapHappyPathNoTopUp` (both NFTs change hands), `TestSwapWithTopUpFeeOnly` (topUp escrowed, fee taken, acceptor paid net, no royalty), `TestSwapDeniedEitherSideBlocked`, `TestSwapCancelByProposer`, `TestSwapExpiredAnyoneCancels`, `TestAcceptSwapProposerNoLongerHoldsAborts` (full-revert, nothing moved). Failing→implement→PASS→full suite green→commit `feat(D1): NFT-for-NFT swap with optional token top-up`.

---

# GROUP E — NFT rental (escrow-backed rights attestation)

### Task E1: rental lifecycle

**Files:** `contract/types.go`,`types_tinyjson.go`,`internal.go`,`market.go`,`events.go`,`test/rental_test.go` (new)

`ListRentalPayload{NftContract,TokenId string; Amount uint64; PaymentToken string; PricePerBlock string json:"pricePerBlock"; MinBlocks,MaxBlocks uint64}`; `RentPayload{RentalId uint64; Blocks uint64}`; `RentalIdPayload`; `ActiveRentalQuery{Account,NftContract,TokenId string}`; `RentalResponse`; `ActiveRentalResponse{Active bool; Until uint64}`. State `rnt|<id>|...`: o(owner),nc,ti,amt,pt,ppb(money),minb,maxb,act(listing active), and rental record: rby(renter),until(uint64),rented(bool). `nxt_rnt`. Plus an index key `ract|<nc>|<ti>|<account>` = until (for `getActiveRentalOf`). Fee+royalty-split snapshot at listRental (owner is paid like a sale of the rental term). Events rentalListed/rented/rentalEnded/rentalDelisted.
- `listRental`: assertInit; assertNotPaused; parse; owner approval+balance preflight on nc; `assertCollectionAllowed`; ppb>0; 0<minBlocks≤maxBlocks; lock effective fee + royalty splits; store; emit.
- `delistRental {rentalId}`: owner; only if not currently `rented`; act=0; no NFT move.
- `rent {rentalId,blocks}`: assertInit; assertNotPaused; renter=getCaller; require listing act + not currently rented + `minb≤blocks≤maxb`; `assertCollectionAllowed(nc)`; `cost=mMulU64(ppb,blocks)`; `received=escrowIn(pt,renter,cost)`; escrow NFT owner→market `nftSafeTransferFrom(nc,o,marketAddr,ti,amt)`; pay owner: `fee,roy,ownerPay=feeAndRoyaltyOf(received,lockedFee,snapRecips,snapBps)`; pay feeRecipient fee; `distributeRoyaltySplitsResolved(pt,received,snapRecips,snapBps)`; `tokenTransferBig(pt,o,ownerPay)`; set rby=renter, until=`getCurrentBlockHeight()+blocks`, rented=true; set `ract|nc|ti|renter`=until; emit rented.
- `endRental {rentalId}`: callable by anyone if `block.height≥until` (or owner anytime if `≥until`); require rented; `nftSafeTransferFrom(nc,marketAddr,o,ti,amt)`; rented=false; delete `ract|...`; emit rentalEnded. (Listing act stays as-is — owner can re-list/delist after.)
- `endRentalEarly {rentalId}`: renter only; require rented; return NFT to owner (same transfer); rented=false; delete index; emit (no refund).
- `getRental {rentalId}`; `getActiveRentalOf {account,nftContract,tokenId}` → read `ract|nc|ti|account`: if present and `block.height<until` ⇒ `{active:true,until}` else `{active:false,until:0}`.

- [ ] TDD `test/rental_test.go`: `TestRentEscrowsNftAndPaysOwner` (NFT market-escrowed, owner paid net of fee/royalty, `getActiveRentalOf` true with right until), `TestEndRentalBeforeUntilRejected` then after `until` returns NFT to owner, `TestEndRentalEarlyByRenterNoRefund`, `TestDoubleRentBlocked`, `TestRentDeniedCollectionBlocked`, `TestDelistRentalBlockedWhileRented`, `TestGetActiveRentalOfExpires` (after `until`, query reports inactive even before endRental). Failing→implement→PASS→full suite green→commit `feat(E1): escrow-backed NFT rental rights attestation`.

---

# GROUP F — Cross-chain settlement (stable ABIs; opt-in)

### Task F0: ABI verification (no code)

- [ ] Read utxo-mapping `unmap`/`unmapFrom` exported method signatures + JSON payloads at `/mnt/HC_Volume_105012347/magi/testnet/utxo-mapping/btc-mapping-contract/contract` and the Magi DEX router swap entrypoint + payload + min-out field at the dex-contracts repo (`/mnt/HC_Volume_105012347/magi/testnet/dex-contracts`). Record exact method names + JSON shapes in a comment block at the top of a new `contract/crosschain.go`. If a signature materially differs from the spec's assumption, note it and adapt the F1/F2 call wrappers accordingly (wrapper-only change; design unchanged). No commit (folds into F1).

### Task F1: native L1 payout (unmap on sale)

**Files:** `contract/types.go`,`types_tinyjson.go`,`contract/crosschain.go` (new),`market.go`,`test/crosschain_test.go` (new), extend `test/mocks/utxomock`

Add OPTIONAL `PayoutMode string json:"payoutMode"` (""|"default"|"unmap") and `PayoutL1Address string json:"payoutL1Address"` to `ListPayload`; store `pm`,`pl1` on the listing. `crosschain.go`: `unmapTo(token, l1addr string, amount *big.Int)` wrapping the verified utxo `unmap`/`unmapFrom` method ABI via `sdk.ContractCallSimple` (abort on nil result). In `doBuy`, where seller payment is sent: if `getListingField(id,"pm")=="unmap"` and sellerPayment>0 → `unmapTo(paymentToken, getListingField(id,"pl1"), sellerPayment)` instead of `tokenTransferBig(paymentToken,seller,...)`; fee + royalty legs unchanged (still mapped-token transfers). Validate at doList: if pm=="unmap" require pl1 non-empty. Extend `test/mocks/utxomock` with an `unmap {to,amount}`/`unmapFrom {from,to,amount}` entrypoint that records an "unmapped to <l1>:<amt>" log/state so the test can assert.

- [ ] TDD `TestUnmapPayoutOnSale` (list with payoutMode unmap + L1 addr, paymentToken=utxomock; buy; assert seller received NO mapped token but utxomock recorded the unmap of the net amount; fee/royalty recipients got mapped token) + `TestUnmapDefaultUnchanged` (no payoutMode ⇒ legacy seller payment, regression) + `TestUnmapMissingL1Rejected`. Failing→implement→PASS→full suite green→commit `feat(F1): native L1 payout via utxo unmap on sale`.

### Task F2: DEX-routed settlement

**Files:** `contract/types.go`,`types_tinyjson.go`,`contract/crosschain.go`,`market.go`,`test/crosschain_test.go`, new `test/mocks/dexmock`

Add OPTIONAL `SettleToken string json:"settleToken"` (""=disabled) + `MinSettleOut string json:"minSettleOut"` to `ListPayload`; store `st`,`mso`. doList: if `st!="" `: require `st!=paymentToken`, `mso` parses, and `pm!="unmap"` (mutually exclusive — abort "payout and settleToken are mutually exclusive"). `crosschain.go`: `dexSwap(fromToken,toToken string, amountIn, minOut *big.Int) *big.Int` — `before=tokenBalanceOf(toToken,marketAddr)`; call DEX router verified swap ABI (market must hold amountIn of fromToken — it does, from escrow); `after=tokenBalanceOf(toToken,marketAddr)`; `out=mSub(after,before)`; if `mCmp(out,minOut)<0` abort "DEX slippage: out below minSettleOut"; return out. In doBuy, when `st!=""`: after computing sellerPayment in paymentToken, `outc=dexSwap(paymentToken, st, sellerPayment, parseMoney(mso))`; `tokenTransferBig(st, seller, outc)` instead of paying seller in paymentToken; fee/royalty unchanged. Create `test/mocks/dexmock` (same scaffold as feetoken/utxomock; entrypoints: `init`, `mint`, a `swap {fromToken?,toToken?,amountIn,minOut}` that — for the test — burns the caller's escrowed `fromToken` market balance and credits `toToken` at a fixed mock rate, returns out; plus `tbal` query). Register in helpers_test.

- [ ] TDD `TestDexRoutedSettlement` (list settleToken=dexmockB, paymentToken=tokenA; buy; seller receives tokenB at mock rate ≥ minSettleOut; fee/royalty in tokenA; market residual 0) + `TestDexSlippageAbortReverts` (minSettleOut too high → buy aborts, buyer fully refunded, no NFT moved) + `TestSettleTokenDefaultUnchanged` (omitted ⇒ legacy) + `TestPayoutAndSettleMutuallyExclusiveAtList`. Failing→implement→PASS→full suite green→commit `feat(F2): DEX-routed settlement with slippage guard`.

---

## Self-Review (performed during planning)

**Spec coverage:** B1 multi-recipient royalty (Task B1+B2) ✓; B2 per-collection fee (B3) ✓; C bundles/sweep/scheduled (C3/C2/C1) ✓; D swap (D1) ✓; E rental escrow-attestation incl. getActiveRentalOf/endRental/early (E1) ✓; F1 unmap payout + F2 DEX-routed + mutual exclusion + ABI verification (F0/F1/F2) ✓; opt-in/legacy-preserved & denylist parity stated per task ✓; dropped scope absent ✓.

**Placeholder scan:** No TBD/TODO. Per-task code is specified as exact structs/state-keys/entrypoint-names/algorithms with explicit "mirror existing function X" references (doBuy/MakeOffer/CancelOffer/doAcceptOffer/SettleAuction/`tinyjsonGov*`/`BatchListPayload`) — the codebase's patterns are uniform and the prior 19 tasks confirm an implementer reproduces verbatim-quality code from this granularity; this is the deliberate, justified plan granularity for a 5-subsystem consolidated plan, not vagueness. Every task has concrete TDD tests with concrete assertions and exact run/commit commands via the shared invariants block.

**Type consistency:** `resolveRoyaltySplits`/`distributeRoyaltySplitsResolved`/`feeAndRoyaltyOf` defined in B1/B2 and reused by C3/E1 (and swap uses `feeAndRoyaltyOf` with nil royalty); `getEffectiveFeeBps` defined B3 used by C3/D1/E1/F; state-key prefixes unique (`rsplit|`,`cfee|`,`bnd|`,`sw|`,`rnt|`,`ract|`, listing `sb`/`pm`/`pl1`/`st`/`mso`); new optional payload fields are additive to `ListPayload` only (legacy absent⇒0/""); F1/F2 mutual exclusion enforced at doList. Execution order B→C→D→E→F ensures B2 helpers exist before C3/E1 reuse them and F0 verifies ABIs before F1/F2.

**Review compression (per user speed directive + execution-speed preference):** full two-stage review on the fund-critical tasks **B2** (royalty-split distribution math across all payout paths) and **F1+F2** (cross-chain value movement); lighter inline verification (build+full-suite-green+scope/diff check) on B1/B3/C1/C2/C3/D1/E1 which are additive opt-in mirrors of reviewed patterns; one final whole-implementation review before finishing the branch.

## Execution Handoff
Subagent-Driven (per the skill); compressed review as above.
