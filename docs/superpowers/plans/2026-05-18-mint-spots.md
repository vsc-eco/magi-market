# magi-market Mint-Spot Primary Sales (Sub-project G) — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement task-by-task. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Add opt-in "mint-spot" listings: a creator who defined an editioned NFT sells mint spots; buyers pay and the market delegated-mints a fresh token to the buyer, fee→recipient / remainder→creator, the nft contract enforcing `maxSupply`.

**Architecture:** New additive entrypoints + `msp|` state on `feature/mint-spots` (off `feature/marketplace-bf` @ `52277f0`). Reuses `escrowIn` (balance-delta), `feeAndRoyaltyOf(received,fee,nil,nil)`, `getEffectiveFeeBps`, `assertCollectionAllowed`, the F-style JSON-injection allowlist, hand-maintained tinyjson. The nft delegated-mint feature is spec-only/unimplemented, so the market side is proven against a `mintnftmock` modeling the documented ABI; real-nft integration is a recorded verification task.

**Tech Stack:** Go (tinygo `wasm-unknown`), hand-maintained tinyjson, vsc-node wasm harness.

**Spec:** `docs/superpowers/specs/2026-05-18-mint-spots-design.md`

## Invariants (every build/test/commit step)
- `export GOTOOLCHAIN=go1.25.3`; gitignored `go.work` (never commit); WASM `tinygo build -gc=custom -scheduler=none -panic=trap -no-debug -target=wasm-unknown -o test/artifacts/main.wasm ./contract`; mock builds `GOWORK=off`; test `GOTOOLCHAIN=go1.25.3 go test ./test/ -count=1`.
- NEVER run tinyjson; hand-append new structs' marshalers mirroring the existing `tinyjsonGov*`/F-task hand-written blocks (single-string payload → mirror `tinyjsonGovDecodeCollectionPayload`; multi-field payload/response/event → mirror an existing one e.g. `BundleResponse`/`SwapResponse` + their event encoders).
- Git: stay ON `feature/mint-spots`; normal `git commit` (never `--amend` across tasks / never detach); before each commit `git branch --show-current`==`feature/mint-spots`, after `git rev-parse feature/mint-spots`==HEAD; if detached STOP+report. Commit msgs multi-line, NO Co-Authored-By/Claude attribution. No PR.
- Additive only: new entrypoints/state; every existing path + the prior **306**-test suite stays byte-identical/green (only add tests).
- Reuse, do not reinvent: `escrowIn`, `feeAndRoyaltyOf`, `tokenTransferBig`, `getEffectiveFeeBps`, `assertCollectionAllowed`, `assertPaymentTokenAllowed`, `nftGetOwner`, `nftIsApprovedForAll`, `getContractAddress`, `getFeeRecipient`, money helpers, the F1/F2 `[a-zA-Z0-9:-]` allowlist loop. Read `contract/market.go` `doBuy`/`doList`, `contract/crosschain.go` (unmapTo/dexSwapTo + F0 comment), `contract/internal.go`, an existing mock (`test/mocks/dexmock`) before writing.

## Task G0: baseline
- [ ] Verify `git branch --show-current`==`feature/mint-spots`; build BUILD_OK; `go test ./test/` → 306 pass / 0 fail. If not green STOP.

---

## Task G1: ABI verification + `mintnftmock` + `nftDelegatedMint` wrapper

**Files:** read `/home/dockeruser/magi/magi_nft-contract` (branch `feat/editioned-define-delegated-mint`) `contract/token.go` `Mint` + `contract/types.go` `MintPayload` + the spec doc there; create `test/mocks/mintnftmock/{go.mod,contract/main.go,runtime/gc_leaking_exported.go,sdk/sdk.go}`, `test/artifacts/mintnftmock.wasm` (gitignored); modify `contract/crosschain.go`, `test/helpers_test.go`.

- [ ] **Step 1: Record the documented ABI** in a comment at the top of the new section in `contract/crosschain.go`. From the nft spec + `MintPayload` struct, record the exact `mint` `//go:wasmexport` name and JSON payload field names/types for a *subsequent* mint into a pre-defined edition (`{to,id,amount}`; `amount` is `uint64` JSON number per existing `MintPayload`; `maxSupply` omitted/ignored on subsequent mint), and the existing-edition signal the market uses (`maxSupply(id)>0` / `exists`). If the real `Mint` code is still unimplemented (only the spec exists today), record "ABI per spec `2026-05-18-editioned-nft-define-delegated-mint-design.md`; real-nft re-verification is a tracked risk" — do NOT modify the nft repo.

- [ ] **Step 2: `nftDelegatedMint` in `contract/crosschain.go`**
```go
// nftDelegatedMint mints `amount` of a pre-defined edition `tokenId` to `to`
// on `nftContract` via the documented delegated-mint ABI. The market must be
// an approved operator (nft enforces isApprovedOrOwner) and the nft enforces
// maxSupply (aborts → whole tx reverts). ABI: see comment above / spec
// 2026-05-18-editioned-nft-define-delegated-mint-design.md.
func nftDelegatedMint(nftContract, to, tokenId string, amount uint64) {
	payload := `{"to":"` + to + `","id":"` + tokenId + `","amount":` + strconv.FormatUint(amount, 10) + `}`
	if sdk.ContractCallSimple(nftContract, "mint", payload) == nil {
		sdk.Abort("delegated mint call failed")
	}
}
```
Add `"strconv"` to `crosschain.go` imports if absent. (`to`=authenticated buyer addr; `tokenId` is lister-supplied — it is allowlist-validated at listMintSpots in G2, so no injection here.)

- [ ] **Step 3: Create `test/mocks/mintnftmock/`** mirroring `test/mocks/dexmock/` scaffold (copy its `go.mod` shape→`module mintnftmock`, `runtime/gc_leaking_exported.go` verbatim, `sdk/sdk.go` with `mintnftmock/runtime` import + the wasmimports it needs incl. `system.get_env_key`). `contract/main.go` package main, hand-rolled JSON, NO tinyjson. Models the post-feature nft minimally:
  - balance ledger `b|<acct>|<id>` (decimal-string int) ; maxSupply `ms|<id>` ; minted `mt|<id>` ; operator approval `op|<owner>|<operator>`="1" ; owner stored at `init`.
  - `//go:wasmexport init {owner}` → store owner.
  - `//go:wasmexport define {id, maxSupply}` → owner-only; set `ms|<id>=maxSupply`, `mt|<id>=0`; abort "Edition already defined" if `ms|<id>`>0; return `{"success":true}`.
  - `//go:wasmexport setApprovalForAll {operator, approved}` → caller is owner; set/clear `op|<owner>|<operator>`.
  - `//go:wasmexport mint {to,id,amount}` → require caller==owner OR `op|<owner>|<caller>`=="1" else abort "Must be owner or approved operator to mint"; require `ms|<id>`>0 else "Edition not defined"; require `mt|<id>+amount <= ms|<id>` else abort "Would exceed max supply"; `b|<to>|<id> += amount`; `mt|<id> += amount`; return `{"success":true}`.
  - `//go:wasmexport balanceOf {account,id}` → `{"balance":"<dec>"}` (lets the test assert mints).
  - `//go:wasmexport maxSupply {id}` → `{"maxSupply":"<dec>"}` and `//go:wasmexport getOwner {}` → `{"owner":"<addr>"}` so magi-market's existing `nftGetOwner` + the G2 existence check resolve against the mock.
  Build: `cd test/mocks/mintnftmock && GOWORK=off GOTOOLCHAIN=go1.25.3 tinygo build -gc=custom -scheduler=none -panic=trap -no-debug -target=wasm-unknown -o /home/dockeruser/magi/magi-market/test/artifacts/mintnftmock.wasm ./contract && echo MINTNFTMOCK_OK`.
  Register in `test/helpers_test.go`: `//go:embed artifacts/mintnftmock.wasm` / `var MintNftMockWasm []byte`, `const MintNftMockID = "mintnftmock"`, `ct.RegisterContract(MintNftMockID, ownerAddress, MintNftMockWasm)` in `SetupContractTest()`.

- [ ] **Step 4:** `tinygo build … ./contract && echo BUILD_OK` (nftDelegatedMint compiles); `MINTNFTMOCK_OK`; `go test ./test/ -count=1` → still 306/0 (additive, no entrypoint yet calls nftDelegatedMint — Go allows the unused package-level func; if the linter/compiler complains it's referenced in G2, that's expected then). Commit `feat(G1): mintnftmock + nftDelegatedMint wrapper + ABI note`.

---

## Task G2: mint-spot listing/buy/delist/query (+ tests)  — FULL two-stage review (fund path)

**Files:** `contract/types.go`, `contract/types_tinyjson.go`, `contract/internal.go`, `contract/market.go`, `contract/events.go`, `test/mintspots_test.go` (new).

- [ ] **Step 1 (failing tests)** `test/mintspots_test.go` (package contract_test). Read `test/helpers_test.go`, `test/trading_test.go` (bundle/listing patterns), `test/crosschain_test.go` (mock setup), and how `MintNftMockID` is registered. Helper to define+approve on the mock (mirror existing mock-call helpers). Tests (write all, run `-run MintSpot` → FAIL: entrypoints missing):
  - `TestMintSpotHappyPath`: mock `init`+`define id="1" maxSupply=5`; owner `setApprovalForAll(market,true)`; market `addPaymentToken contract:<pt>` (use an existing token mock as paymentToken — e.g. the feetoken/utxomock used elsewhere, or `TokenID`); buyer funded; owner `listMintSpots {nftContract:MintNftMockID,tokenId:"1",paymentToken,pricePerSpot:"1000",maxSpots:0,expirationBlock:0}`; buyer `buyMintSpot {listingId:0,amount:2}` → assert mintnftmock `balanceOf(buyer,"1")==2`, creator(owner) received `received−fee`, feeRecipient received fee (feeBps from InitFullSetup), market token residual 0, `mint_spot_bought` event amount/received.
  - `TestMintSpotMaxSupplyEnforcedByNft`: define maxSupply=3; list; buy amount=2 (ok); buy amount=2 → aborts "Would exceed max supply"; assert buyer NOT minted the 2nd batch, buyer payment refunded (escrow reverted), creator not paid for the failed buy.
  - `TestMintSpotListingCapEnforced`: define maxSupply=10; list with `maxSpots:3`; buy amount=2 ok; buy amount=2 → aborts "Exceeds listing mint-spot cap"; listing auto-inactive once sold==maxSpots (buy exactly 3 then listing act=0).
  - `TestMintSpotOnlyOwnerLists`: non-owner `listMintSpots` → "Only collection owner can list mint spots".
  - `TestMintSpotMarketNotApproved`: owner did NOT setApprovalForAll → at `buyMintSpot` the mock `mint` aborts "Must be owner or approved operator to mint", whole buy reverts (refund). (Optionally also gate at list: list requires `nftIsApprovedForAll` → "Marketplace not approved as operator for this NFT collection".)
  - `TestMintSpotEditionNotDefined`: `listMintSpots` for an id with no `define` → "Edition not defined".
  - `TestMintSpotDeniedCollectionBlocked`: `denyCollection(MintNftMockID)` → listMintSpots aborts "Collection is denied"; and a pre-listed then denied → buyMintSpot aborts.
  - `TestMintSpotDelistByLister`: list → delistMintSpots (lister) → act=0 → buyMintSpot aborts "Mint spot listing not active"; non-lister delist rejected.
  - `TestMintSpotTokenIdInjectionRejected`: `listMintSpots` with `tokenId` containing `"` → "tokenId contains invalid characters".
  - `TestMintSpotStartBlock`: list with future `startBlock` → buy aborts "Listing not started"; advance blocks → succeeds.

- [ ] **Step 2: types.go** — `ListMintSpotsPayload{NftContract,TokenId string; PaymentToken string; PricePerSpot string json:"pricePerSpot"; MaxSpots uint64 json:"maxSpots"; ExpirationBlock uint64 json:"expirationBlock"; StartBlock uint64 json:"startBlock"}` (json: nftContract/tokenId/paymentToken/pricePerSpot/maxSpots/expirationBlock/startBlock); `BuyMintSpotPayload{ListingId uint64 json:"listingId"; Amount uint64 json:"amount"}`; `MintSpotIdPayload{ListingId uint64 json:"listingId"}`; `MintSpotListingResponse{ListingId uint64; Lister string; NftContract string; TokenId string; PaymentToken string; PricePerSpot string; MaxSpots uint64; Sold uint64; Active bool; ExpirationBlock uint64; StartBlock uint64}`; events `MintSpotsListedEvent`+attrs `{ListingId uint64; Lister string; NftContract string; TokenId string; MaxSpots uint64}`, `MintSpotBoughtEvent`+attrs `{ListingId uint64; Buyer string; Amount uint64; Received string; Fee string}`, `MintSpotsDelistedEvent`+attrs `{ListingId uint64; Lister string}`.

- [ ] **Step 3: types_tinyjson.go** — hand-append decode for the 3 payloads + encode for the response + 3 events, mirroring existing hand-written multi-field patterns (`BundleResponse`/`SwapResponse` encoders, `tinyjsonGovEncodeCollectionDenied` events; uint64 fields like an existing `ExpirationBlock` fragment; string fields like `PaymentToken`). Touch no existing struct's marshaler.

- [ ] **Step 4: internal.go** — `mintSpotKey(id,field)` (prefix `msp|`), `set/getMintSpotField`, `set/getMintSpotUint64`, `set/getMintSpotMoney` (mirror listing money helper), `isMintSpotActive`, `getNextMintSpotId`/`setNextMintSpotId` (counter `nxt_msp`). Add an nft existence reader `nftMaxSupplyOf(nftContract, tokenId string) uint64` calling the nft `maxSupply` ABI (`{"id":...}` → `{"maxSupply":"<dec>"}`; mirror `tokenBalanceOf`'s parse style; 0 if absent/nil) — used only by listMintSpots' existence check; document it's a stable-ABI call (not internal-state).

- [ ] **Step 5: market.go entrypoints** (mirror `doList`/`doBuy`/`Delist`/`GetListing` structure):
  - `//go:wasmexport listMintSpots` `ListMintSpots`: assertInit; assertNotPaused; caller; parse; `NftContract/TokenId/PaymentToken!=""`; `price:=parseMoney(PricePerSpot)` !mIsZero else "Price must be greater than zero"; `assertPaymentTokenAllowed(pt)`; `assertCollectionAllowed(nc)`; apply the F1/F2 `[a-zA-Z0-9:-]` allowlist loop to `TokenId` → abort "tokenId contains invalid characters"; `nftGetOwner(nc)==caller` else "Only collection owner can list mint spots"; `nftIsApprovedForAll(nc,caller,getContractAddress())` else "Marketplace not approved as operator for this NFT collection"; `nftMaxSupplyOf(nc,ti)>0` else "Edition not defined"; `feeBps:=getEffectiveFeeBps(nc)`; `feeBps>10000`→abort "Fee must be <= 10000 basis points"; store s/nc/ti/pt, money p, ms=MaxSpots, sold=0, act=1, exp, sb=StartBlock, fb=feeBps; `nxt_msp++`; `emitMintSpotsListed`; `CreatedResponse{Success:true,Id:id}`.
  - `//go:wasmexport buyMintSpot` `BuyMintSpot`: assertInit; assertNotPaused; buyer=getCaller; parse; `isMintSpotActive(id)` else "Mint spot listing not active"; `isExpired(getMintSpotUint64(id,"exp"))`→"Mint spot listing has expired"; `sb:=getMintSpotUint64(id,"sb"); if sb!=0 && getCurrentBlockHeight()<sb` → "Listing not started"; `assertCollectionAllowed(getMintSpotField(id,"nc"))`; `p.Amount>0` else "Amount must be greater than zero"; lister=`s`; `buyer!=lister` else "Lister cannot buy own mint spots"; `ms:=getMintSpotUint64(id,"ms"); sold:=getMintSpotUint64(id,"sold"); if ms!=0 && sold+p.Amount>ms` → "Exceeds listing mint-spot cap"; `pt:=getMintSpotField(id,"pt")`; `price:=getMintSpotMoney(id,"p")`; `total:=mMulU64(price,p.Amount)`; `received:=escrowIn(pt,buyer,total)`; `fee,_,creatorPay:=feeAndRoyaltyOf(received,getMintSpotUint64(id,"fb"),nil,nil)`; `nftDelegatedMint(getMintSpotField(id,"nc"),buyer,getMintSpotField(id,"ti"),p.Amount)` **before** payouts (abort ⇒ whole tx reverts ⇒ buyer refunded); if `!mIsZero(fee)` `tokenTransferBig(pt,getFeeRecipient(),fee)`; if `!mIsZero(creatorPay)` `tokenTransferBig(pt,lister,creatorPay)`; `setMintSpotUint64(id,"sold",sold+p.Amount)`; if `ms!=0 && sold+p.Amount==ms` `setMintSpotField(id,"act","0")`; `emitMintSpotBought(id,buyer,p.Amount,formatMoney(received),formatMoney(fee))`; SuccessResponse.
  - `//go:wasmexport delistMintSpots` `DelistMintSpots`: assertInit (works while paused, like Delist); caller; parse; active; `caller==s` else "Only lister can delist mint spots"; act=0; `emitMintSpotsDelisted`; SuccessResponse.
  - `//go:wasmexport getMintSpotListing` `GetMintSpotListing`: assertInit; parse; build `MintSpotListingResponse`.

- [ ] **Step 6: events.go** — `emitMintSpotsListed`, `emitMintSpotBought`, `emitMintSpotsDelisted` mirroring existing emit fns.

- [ ] **Step 7:** `tinygo build … && echo BUILD_OK`; `go test ./test/ -count=1 -run MintSpot -v` (all G2 tests PASS); full `go test ./test/ -count=1` → **306 + new, 0 fail**. If a prior test regresses STOP+report (must be additive). Commit `feat(G2): sell mint spots (delegated primary mint)`.

---

## Self-Review (performed during planning)
**Spec coverage:** decision 1 single-id (G2 single tokenId) ✓; 2 lister==owner+approved (ListMintSpots checks) ✓; 3 nft supply truth + soft maxSpots (BuyMintSpot maxSpots check + nft `mint` cap test) ✓; 4 primary payout fee+remainder, no royalty (`feeAndRoyaltyOf(received,fb,nil,nil)`) ✓; 5 soulbound sellable (no soulbound guard added) ✓; denylist parity (list+buy) ✓; mint-before-payout atomicity ✓; injection allowlist on tokenId ✓; dependency = mintnftmock + recorded ABI note + risk ✓; additive/306-green ✓.
**Placeholder scan:** no TBD; entrypoint algorithms fully specified with exact fields/aborts/order + the mirror functions; mock fully specified; tests concrete. The nft `mint`/`maxSupply` ABI field exactness is an explicit G1-Step-1 verification (sanctioned, like F0) — the documented `{to,id,amount}` is the design contract; only the wrapper adjusts if the real ABI differs.
**Type consistency:** `nftDelegatedMint`/`nftMaxSupplyOf` defined G1/G2-Step4 used in G2-Step5; `feeAndRoyaltyOf(received,fee,nil,nil)` matches the B2 equal-length guard (0==0); state prefix `msp|`/`nxt_msp` unique vs existing (ls|of|au|bnd|sw|rnt|ract|rsplit|cfee|...); reuses escrowIn/tokenTransferBig/getEffectiveFeeBps/assertCollectionAllowed/nftGetOwner/nftIsApprovedForAll consistently.
**Review compression:** G1 inline-verified (mock+wrapper, additive); **G2 full two-stage** (fund-flow buy/mint path); one final whole-G review before finishing.

## Execution Handoff
Subagent-Driven; compressed review as above.
