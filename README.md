# magi-market

An ERC-1155 NFT marketplace smart contract for the VSC (Vite Smart Contract)
ecosystem, written in Go and compiled to `wasm-unknown` with TinyGo. It
supports fixed-price listings, offers, English/Dutch auctions, multi-recipient
royalties, NFT-for-NFT swaps, rentals, primary-sale "mint spots", fixed-price
fungible-token sales, and opt-in cross-chain settlement — all settled in
native HIVE/HBD, a `magi_token`, or a UTXO-mapped coin (e.g. BTC), so an NFT
can be traded for BTC.

- **Network:** VSC L2 (Hive-anchored, ~3.0s/block).
- **Language/build:** Go → TinyGo `wasm-unknown`.
- **External contracts it talks to:** `magi_nft` (ERC-1155 NFTs),
  `magi_token` (ERC-20-style tokens), the UTXO-mapping contracts (e.g. BTC),
  and optionally a DEX pool/router for routed settlement.

## Capabilities

| Area | What it does |
|---|---|
| **Listings & offers** | Fixed-price `list`/`buy`, `makeOffer`/`acceptOffer`, collection-wide offers, `updateListing`, `batchList`/`batchBuy` (≤20 items each). NFTs stay with the seller via operator approval (no escrow). `buy` accepts an optional `maxTotalPrice` slippage cap so a seller can't race `updateListing` to drain a buyer's allowance. |
| **Auctions** | English auctions with anti-snipe extension + a min-bid-increment floor, and Dutch (declining-price) auctions. The NFT is escrowed for the auction's duration. |
| **Royalties & fees** | Single or multi-recipient royalty splits (≤10 recipients, Σ≤50%), snapshotted at listing/auction creation and distributed at every payout. Per-collection fee override on top of the global marketplace fee. |
| **Governance & safety** | Two-step owner transfer (`changeOwner` → `acceptOwnership`), pause/unpause, retroactive collection denylist with deliberately-ungated recovery paths so denied collections never trap assets, payment-token whitelist. |
| **Advanced trading** | Scheduled listings (`startBlock`), slippage-guarded floor `sweep`, single-collection atomic `listBundle`/`buyBundle`. |
| **Swaps** | `proposeSwap`/`acceptSwap` NFT-for-NFT barter with optional **magi_token** top-up. Fee + offered-collection royalty are snapshotted at propose-time. (Native HIVE/HBD as top-up is currently unusable — see Limitations.) |
| **Rentals** | Escrow-backed rental-rights attestation: the NFT is escrowed owner↔market (never to the renter); `endRental` is callable by anyone after the term so nothing strands. |
| **Cross-chain settlement** | Opt-in per-listing native-L1 payout via UTXO `unmap`, or DEX-routed settlement (`dexSwapTo`). Native HIVE/HBD as `paymentToken` is rejected at list time for both modes. |
| **Mint spots (primary sale)** | `listMintSpots`/`buyMintSpot`: the collection owner sells the right to mint editioned NFTs; the market performs a delegated mint directly to the buyer (the market never custodies the NFT). |
| **Token sales (ERC-20)** | `listToken`/`buyToken`: fixed-price sale of a fungible `magi_token` amount at a price per unit. Operator/allowance custody, fee-only (no royalty). |

## Concepts

### Custody models

| Flow | Custody | NFT/asset moves… |
|---|---|---|
| `list` / `buy`, offers, `listBundle`/`buyBundle`, mint spots, token sales | **Operator-approval (no escrow)** | seller → buyer at buy time |
| `createAuction` / `listRental` | **Escrow into the market** | seller → market at create, market → winner/owner at settle/end |

Operator-approval flows require the seller to have approved the market on the
NFT (or token) contract — either blanket `setApprovalForAll(market,true)` or a
per-token ERC-6909 `approve(market, id, amount)` (mint-spot listing requires
operator approval specifically). A single un-escrowed NFT can sit in several
listings at once; the first buy wins and the rest abort at transfer time.

### Money & precision

All amounts are arbitrary-precision `big.Int` decimal strings
(`parseMoney`/`formatMoney`) in the token's smallest unit — never `uint64` —
so high-decimal tokens can't overflow. `escrowIn` measures the **actual
balance delta** received, so fee-on-transfer / UTXO `deduct_fee` tokens settle
correctly; native HIVE/HBD and tokens registered with the `magi_token`
decoder skip the delta read (they transfer exactly the requested amount).

### Payment tokens

Every `paymentToken` must be on the **whitelist** before it can be used.
`init` seeds native `hive` + `hbd`; the owner adds custom tokens with
`addPaymentToken`. The whitelist is enforced once any token is added
(`ptw_on`).

`addPaymentToken` takes an optional **`decoder`** — `"magi_token"` | `"utxo"`
| `"native"` — that binds the token to a known balance-state layout so
`tokenBalanceOf` reads the right key/encoding instead of probing `bal|` then
`a-` and guessing (a non-standard token could otherwise be misread as a 0
balance). Omitting it leaves the token on the legacy auto-probe (back-compat).

| Token kind | How balance is read | How it transfers |
|---|---|---|
| `native` (hive/hbd) | `sdk.GetBalance` | L1 `transfer.allow` intent + `sdk.HiveDraw`/`HiveTransfer` |
| `magi_token` | raw state `bal|<acct>` (big-int bytes, ≤32) | cross-contract `transferFrom`/`transfer` |
| `utxo` (e.g. BTC) | raw state `a-<acct>` (big-endian u64) | cross-contract `transferFrom`/`transfer` |

### Fees & royalties

Basis points throughout (10000 = 100%). A sale of `total = pricePerUnit ×
amount` splits into: **marketplace fee** (`feeBps`, or a per-collection
override) → **royalty** (`royaltyBps`, optionally split among ≤10 recipients,
Σ ≤ 5000 bps) → **seller proceeds** (the remainder). Fee + royalty must be ≤
10000. Each share is floored independently with integer division; the rounding
dust goes to the seller (`seller = total − fee − Σroyalty`), so the contract
never distributes more than it holds. Fee/royalty **snapshots are locked at
listing/auction creation**, so a later config change can't shortchange an
in-flight trade.

### Block & time model

The contract works in **absolute VSC L2 block numbers** (~3.0s/block:
1200/hour, 28800/day), read via `getCurrentBlockHeight`. Clients turn a
wall-clock duration into an absolute block. Relevant fields:

- `startBlock` — listing/auction not active before it (0 = now).
- `expirationBlock` — listing/offer expires at/after it (0 = never).
- `endBlock` — auction end.
- Rentals use a **relative span** (`minBlocks`/`maxBlocks`); `rent` picks a
  duration inside that range.

### Auctions

- **English:** `startPrice` is the reserve; bidders raise; the highest bid at
  `endBlock` wins. A bid must clear the reserve **and** beat the current high
  bid by at least the min increment (defaults to a **1% floor** even when
  unset, so anti-snipe can't be griefed into indefinite extension). Anti-snipe
  extends `endBlock` when a bid lands near the end. `settleAuction` is
  **permissionless** after the end (pays the winner / royalty / fee, or
  returns the NFT to the seller if there were no bids).
- **Dutch:** price declines linearly from `startPrice` (at `startBlock`) to
  `endPrice` (at `endBlock`); the **first bid buys instantly** at the current
  price (settled inside `placeBid`). `settleAuction` does **not** apply to
  Dutch — an unsold/expired Dutch auction is reclaimed by the seller via
  `cancelAuction`.
- `cancelAuction` (seller-only) returns the escrowed NFT; English can only be
  cancelled while it has no bids.

### Rentals

`listRental` escrows the NFT into the market and advertises a price-per-block
and a `[minBlocks, maxBlocks]` span. `rent` pays `pricePerBlock × blocks`
up-front for a term inside the span; the NFT **stays in the market** (the
renter gets a rights attestation, not custody). `endRental` is callable by
**anyone** after the term (returns the NFT to the owner); `endRentalEarly` is
the renter's option; `delistRental` is the owner's pre-rental cancel.
Soulbound tokens can't be rented (escrow would strand them).

### Soulbound tokens

magi_nft permits a soulbound transfer only when `from == collectionOwner` (the
static `owner` set at the NFT contract's init); recipients can never
re-transfer. The marketplace mirrors that rule per custody model:

- **`list` / `listBundle`** — no escrow; the buy-time transfer is
  `seller→buyer`, so the **collection owner may list/bundle their own
  soulbound editions**. A non-owner holder is rejected with
  `Cannot list soulbound tokens`.
- **`createAuction` / `listRental`** — escrow to the market, which is not the
  collection owner and could never transfer the token back out, so **all
  soulbound tokens are blocked** for these (even for the owner).

### Cross-chain settlement (opt-in, per listing)

A listing can route the seller's proceeds off the L2 instead of a plain
token transfer:

- **`payoutMode:"unmap"`** + `payoutL1Address` — the proceeds are `unmap`-ed
  to the seller's native L1 address on the payment token's chain.
- **`settleToken` + `dexPool` + `minSettleOut`** — the proceeds are swapped
  through a DEX pool to a different asset, slippage-guarded, delivered to the
  seller.

Both are mutually exclusive, reject a native `paymentToken`, and validate the
L1-address / pool / token strings against an injection allowlist.

## Limits & constants

| Constant | Value |
|---|---|
| Marketplace fee (`feeBps`) | ≤ 10000 bps |
| Royalty (`royaltyBps`) | ≤ 5000 bps; fee + royalty ≤ 10000 |
| Royalty split recipients | ≤ 10, Σ ≤ 5000 bps |
| Min-bid-increment | ≥ 100 bps when set; **1% floor applied at bid time even when unset** |
| `batchList` / `batchBuy` items | ≤ 20 (sweep is uncapped) |
| Bundle items | ≤ 20 |
| `tokenId` / account strings | allowlist `[a-zA-Z0-9:_.-]` |
| magi_token balance bytes | ≤ 32 |

## Security model

- **Atomicity via abort.** Every multi-leg flow relies on `sdk.Abort`
  reverting the *whole* transaction — a failed NFT transfer/mint or token
  transfer refunds the buyer's escrow leg automatically. A failed
  cross-contract call traps the tx (it can't return a falsey "success").
- **Checks-Effects-Interactions.** Every payable entrypoint flips its
  entity's `active` flag / decrements remaining quantity / claims its next id
  **before** any external call (`escrowIn`, `nftSafeTransferFrom`, delegated
  mint, fee/royalty payouts). A re-entry through a malicious whitelisted
  paymentToken sees the post-write state and aborts; the flip is undone by
  tx-revert on any later abort.
- **Input validation.** `assertValidTokenId` / `assertValidAccount` gate every
  user-supplied token id / account / recipient against the `[a-zA-Z0-9:_.-]`
  allowlist, so a value can never break out of the JSON payload the market
  string-builds for a cross-contract call.
- **Payment-token whitelist** (safe-by-default; seeded with native only).
- **Buy slippage** (`maxTotalPrice` / sweep `maxTotal`).
- **Overflow-safe counters** (e.g. mint-spot cap uses `amount > cap − sold`).

> **⚠️ External-contract coupling risk.** The raw-state decoders hardcode the
> *internal* storage layout of `magi_nft` / `magi_token` / the UTXO-mapping
> contracts. If any of those changes its key format or encoding, magi-market
> will silently misread balances (a fund bug with no compile error). The
> `decoder` registry mitigates this for explicitly-typed tokens; the coupling
> warning lives above the decoders in `contract/internal.go`. **Before
> upgrading any of those contracts, re-verify the decoders and rebuild the
> test wasm artifacts.**
>
> Coupling surfaces last re-verified 2026-07-19, against **vsc-eco `main`** —
> which is what `make ext` builds from (never the sibling repo's checked-out
> branch; see the Makefile's "external contracts" section):
>
> | Contract | vsc-eco `main` | Keys/encodings relied on |
> |---|---|---|
> | `magi_nft` | `ce2ada2` | `bal\|`, `op\|`, `sb\|`, `allow\|`, `owner` — all present, unchanged |
> | `magi_token` | `ee21119` | `bal\|<acct>` + `big.Int.Bytes()` big-endian — unchanged |
> | utxo-mapping | `6039c43` | `a-<acct>` big-endian-u64-trimmed |
>
> Both sibling repos habitually sit on feature branches locally, so `make ext`
> resolves the vsc-eco remote (`origin` in magi_token-contract, `upstream` in
> magi_nft-contract) and builds `<remote>/main` via `git archive` into `.build/`
> — the sibling checkouts are never mutated. Override with
> `make -B ext NFT_REF=<ref>`.
>
> The delegated-mint ABI citation previously named `cebd5a0` on
> `feat/editioned-define-delegated-mint`; that commit is *not* an ancestor of
> the branch that was in use, so the table above supersedes it.

## Entrypoint reference

`auth` legend: **anyone** (no auth) · **owner** (contract owner) ·
**seller/buyer/renter/lister/proposer** (the entity's party) · **collectionOwner**
(the NFT collection's `owner`). All write entrypoints abort when `paused`
(except governance). Read-only `get*`/`is*` entrypoints take the obvious id
payload and never mutate.

### Initialization & governance

| Entrypoint | Auth | Payload | Notes |
|---|---|---|---|
| `init` | owner (once) | `{feeBps, feeRecipient}` | Seeds native hive/hbd whitelist + 1% min-bid floor. `feeBps ≤ 10000`. |
| `setFee` | owner | `{feeBps}` | Global marketplace fee. |
| `setFeeRecipient` | owner | `{feeRecipient}` | |
| `setCollectionFee` | owner | `{nftContract, feeBps}` | Per-collection fee override. |
| `clearCollectionFee` | owner | `{nftContract}` | Revert to global fee. |
| `setMinOffer` | owner | `{minOffer}` | Global minimum offer (smallest units). |
| `setMinBidIncrement` | owner | `{minBidIncrementBps}` | Must be ≥ 100. |
| `setAntiSnipeBlocks` | owner | `{antiSnipeBlocks}` | Extension window for English auctions. |
| `changeOwner` | owner | `{newOwner}` | Step 1 of 2-step transfer. |
| `acceptOwnership` | pending owner | `{}` | Step 2. |
| `cancelOwnershipTransfer` | owner | `{}` | Abort a pending transfer. |
| `pause` / `unpause` | owner | `{}` | Halts all trading entrypoints. |
| `denyCollection` / `allowCollection` | owner | `{nftContract}` | Retroactive denylist (recovery paths stay open). |
| `addPaymentToken` | owner | `{token, decoder?}` | Whitelist + optional decoder (`magi_token`\|`utxo`\|`native`). |
| `removePaymentToken` | owner | `{token}` | |
| `emergencyWithdraw` | owner (paused) | `{tokenType, contract, tokenId, amount, to}` | Rescue stranded assets while paused. |

### Listings & offers

| Entrypoint | Auth | Payload | Notes |
|---|---|---|---|
| `list` | seller | `{nftContract, tokenId, amount, paymentToken, pricePerUnit, expirationBlock?, startBlock?, payoutMode?, payoutL1Address?, dexPool?, settleToken?, minSettleOut?}` | Operator-approval; no escrow. Returns `listingId`. |
| `batchList` | seller | `{items:[List…]}` | ≤ 20 items. |
| `buy` | buyer | `{listingId, amount, maxTotalPrice?}` | `maxTotalPrice` = slippage cap. Transfers seller→buyer. |
| `batchBuy` | buyer | `{items:[Buy…]}` | ≤ 20 items. |
| `delist` | seller | `{listingId}` | |
| `updateListing` | seller | `{listingId, newPrice}` | |
| `makeOffer` | buyer | `{nftContract, tokenId, amount, paymentToken, pricePerUnit, expirationBlock?}` | Escrows payment now. `tokenId:""` = collection-wide offer. |
| `acceptOffer` | NFT holder | `{offerId, amount}` | Partial fills allowed. |
| `acceptCollectionOffer` | NFT holder | `{offerId, tokenId, amount}` | Fulfil a collection-wide offer with a specific token. |
| `cancelOffer` | buyer | `{offerId}` | Refunds escrow. |

### Auctions

| Entrypoint | Auth | Payload | Notes |
|---|---|---|---|
| `createAuction` | seller | `{nftContract, tokenId, amount, paymentToken, auctionType:"english"\|"dutch", startPrice, endPrice, startBlock?, endBlock}` | Escrows the NFT. Soulbound blocked. |
| `placeBid` | bidder | `{auctionId, bidAmount}` | Escrows the bid (refunds prior bidder). Dutch = instant buy. |
| `settleAuction` | anyone (after end) | `{auctionId}` | English only; pays winner or returns to seller. |
| `cancelAuction` | seller | `{auctionId}` | English: only with no bids. Dutch: reclaim unsold. |

### Bundles & sweep

| Entrypoint | Auth | Payload | Notes |
|---|---|---|---|
| `listBundle` | seller | `{nftContract, items:[{tokenId, amount}], paymentToken, price, expirationBlock?}` | Single collection, ≤ 20 items, one price for the lot. |
| `buyBundle` | buyer | `{bundleId}` | Atomic — all items or abort. |
| `delistBundle` | seller | `{bundleId}` | |
| `sweep` | buyer | `{nftContract, listingIds:[…], maxTotal}` | Buy many listings in one collection; `maxTotal` slippage cap. Uncapped count. |

### Rentals

| Entrypoint | Auth | Payload | Notes |
|---|---|---|---|
| `listRental` | owner | `{nftContract, tokenId, amount, paymentToken, pricePerBlock, minBlocks, maxBlocks}` | Escrows the NFT. Soulbound blocked. |
| `rent` | renter | `{rentalId, blocks}` | Pays `pricePerBlock × blocks`; `blocks` within `[min,max]`. |
| `endRental` | anyone (after term) | `{rentalId}` | Returns NFT to owner. |
| `endRentalEarly` | renter | `{rentalId}` | Renter ends early. |
| `delistRental` | owner | `{rentalId}` | Pre-rental cancel. |

### Swaps

| Entrypoint | Auth | Payload | Notes |
|---|---|---|---|
| `proposeSwap` | proposer | `{offeredNft, offeredTokenId, offeredAmount, wantedNft, wantedTokenId, wantedAmount, topUp?, topUpToken?, expirationBlock?}` | NFT-for-NFT + optional magi_token top-up. |
| `acceptSwap` | wanted-NFT holder | `{swapId}` | Atomic two-sided transfer + top-up. |
| `cancelSwap` | proposer | `{swapId}` | |

### Mint spots (primary sale)

| Entrypoint | Auth | Payload | Notes |
|---|---|---|---|
| `listMintSpots` | collection owner | `{nftContract, tokenId, paymentToken, pricePerSpot, maxSpots?, expirationBlock?, startBlock?}` | Requires operator approval on the NFT contract. `maxSpots:0` = uncapped (bounded by `maxSupply`). |
| `buyMintSpot` | buyer | `{listingId, amount}` | Delegated-mints directly to the buyer. |
| `delistMintSpots` | lister | `{listingId}` | |

### Token sales (fungible ERC-20)

| Entrypoint | Auth | Payload | Notes |
|---|---|---|---|
| `listToken` | seller | `{tokenContract, amount, paymentToken, pricePerUnit, expirationBlock?, startBlock?}` | Operator/allowance custody; fee-only (no royalty). |
| `buyToken` | buyer | `{listingId, amount}` | Partial fills allowed. |
| `delistToken` | seller | `{listingId}` | |

### Royalties

| Entrypoint | Auth | Payload | Notes |
|---|---|---|---|
| `setRoyalty` | collection owner | `{nftContract, royaltyBps, royaltyRecipient}` | `royaltyBps ≤ 5000`. |
| `setRoyaltySplits` | collection owner | `{nftContract, splits:[{recipient, bps}]}` | ≤ 10 recipients, Σ ≤ 5000. Overrides single recipient. |

### Read-only queries

`getListing` · `getOffer` · `getAuction` · `getBundle` · `getSwap` ·
`getRental` · `getActiveRentalOf` · `getMintSpotListing` · `getTokenListing`
(take the entity id / nft+token) · `getRoyalty`, `getRoyaltySplits`,
`getEffectiveFee` (`{nftContract}`) · `getMinOffer` · `getInfo` · `getOwner` ·
`getPendingOwner` · `isPaused` · `isCollectionDenied` (`{nftContract}`) ·
`isPaymentTokenAllowed` (`{token}`). These never mutate; most reads for
clients should go through the indexer (see Events).

## Events (for indexers)

Every entrypoint emits a JSON event via `sdk.Log`:
`{"type":"<t>","attributes":{…},"tx":"<id>"}`. The indexer folds these into
the `magi_market_*` views. Emitted `type`s:

```
init_magi_market  listed  delisted  bought  listing_updated
offer_made  offer_cancelled  offer_accepted
auction_created  bid_placed  auction_settled  auction_cancelled
bundle_listed  bundle_bought  bundle_delisted  swept
swapProposed  swapAccepted  swapCancelled
rentalListed  rented  rentalEnded  rentalDelisted
mint_spots_listed  mint_spot_bought  mint_spots_delisted
token_listed  token_bought  token_delisted
royalty_set  royaltySplitsSet  collectionFeeSet  collectionFeeCleared
collectionDenied  collectionAllowed
payment_token_added  payment_token_removed
ownerTransferInitiated  ownerTransferCancelled  ownerChange
paused  unpaused  emergency_withdraw
```

## Build & test

The contract package only compiles under TinyGo (the SDK uses
`go:wasmimport`); there are no host unit tests — all tests are wasm-harness
integration tests.

**On a fresh clone, use the Makefile** — `test/artifacts/*.wasm` and `go.work`
are both gitignored, so nothing builds or tests until they are regenerated:

```bash
make setup      # write go.work pointing at your go-vsc-node checkout
make artifacts  # build all 7 wasm artifacts the harness embeds
make test       # rebuild what's stale, then run the full suite (~80s)
make help       # all targets
```

**Nothing needs to be pre-cloned.** `token.wasm`/`nft.wasm` are always built
from **vsc-eco `main`**, fetched from hardcoded canonical URLs — never from
whatever branch a local checkout happens to sit on. If you do have local
`go-vsc-node` / `magi_nft-contract` / `magi_token-contract` checkouts they are
reused as a fetch cache (and never mutated — the build runs from a throwaway
`git archive` tree under `.build/`); anything missing is cloned into
`.build/src/`. Override with `VSC_NODE=` / `NFT_REPO=` / `TOKEN_REPO=`, or
build a different ref with `make -B ext NFT_URL=<url-or-path> NFT_REF=<ref>`.
The manual equivalents follow.

```bash
# TinyGo 0.39 rejects host go1.26; go-vsc-node's go.mod requires go ≥ 1.25.7.
export GOTOOLCHAIN=go1.25.7

# Build the contract wasm.
tinygo build -gc=custom -scheduler=none -panic=trap -no-debug \
  -target=wasm-unknown -o bin/main.wasm ./contract

# Refresh the embedded artifact, then run the full suite (~80s).
cp bin/main.wasm test/artifacts/main.wasm
GOTOOLCHAIN=go1.25.7 go test ./test/ -count=1
```

The harness also needs `test/artifacts/{token,nft}.wasm` — built from the
`magi_token-contract` / `magi_nft-contract` repos — plus four mock artifacts.
The mocks (`dexmock`, `utxomock`, `feetoken`, `mintnftmock`) are **not** the
production DEX/UTXO contracts: they are minimal stand-ins whose source lives in
this repo under `test/mocks/<name>/contract/`, built per-mock, e.g.:

```bash
cd test/mocks/mintnftmock && GOWORK=off GOTOOLCHAIN=go1.25.7 \
  tinygo build -gc=custom -scheduler=none -panic=trap -no-debug \
  -target=wasm-unknown -o ../../artifacts/mintnftmock.wasm ./contract
```

**After any contract change, refresh `test/artifacts/main.wasm` before running
tests** — the harness embeds the artifact, so a stale artifact gives
false-positive passes. Dedicated suites: `test/audit_fixes_test.go` and
`test/audit_followup_test.go` cover the post-audit input gates (slippage,
account validator, min-bid floor, batch caps, mint-spot overflow guard,
payment-token decoder validation, native-paymentToken rejection, whitelist
defaults).

Notes:

- `go.mod` pins `vsc-node` via a `replace`; locally an **uncommitted,
  gitignored `go.work`** points it at your `go-vsc-node` checkout. `go.work` /
  `go.work.sum` and `test/artifacts/*.wasm` are gitignored — never commit them.
- **`types_tinyjson.go` is hand-maintained and must never be regenerated**
  (the package is `main`; `tinyjson` can't bootstrap it). For a field change,
  edit the marshaler fragment by hand; for a new struct, append hand-written
  encode/decode mirroring the in-file patterns. Correctness is proven by the
  TinyGo build + the full wasm suite.

## Repository layout

```
contract/
  main.go            entrypoint plumbing, owner/pause helpers
  market.go          listings, offers, bundles, sweep, mint spots, governance
  auction.go         English & Dutch auctions
  tokens.go          fixed-price fungible-token sales
  crosschain.go      unmap / DEX settlement, delegated-mint wrapper
  internal.go        money helpers, raw-state decoders, whitelist + decoder registry
  money.go           big.Int money helpers
  events.go          event emitters
  types.go           payload/response structs
  types_tinyjson.go  HAND-MAINTAINED tinyjson marshalers (never regenerate)
test/                wasm-harness integration tests + hand-rolled mocks
docs/superpowers/    design specs and implementation plans
```

## Deployment & upgrade

Built with the `contract-deployer` from go-vsc-node, signed by the owner
account:

```bash
contract-deployer -network testnet -data-dir <deployer-data> \
  -contractId <existing-id> \           # omit to deploy a NEW contract
  -wasmPath bin/main.wasm \
  -name "Magi Market" -description "…" \
  -gqlUrl https://<node>/api/v1/graphql
```

`init` is a separate owner call after a fresh deploy (the deployer leaves
`owner` unset; `init` sets `owner = msg.caller`). Heavy entrypoints need
`rc_limit ≥ 10000`. Verify the live code with `findContract(filterOptions:
{byId:"<id>"}){ id code }`. **Re-verify the raw-state decoders against the
external contracts before/after any of them is upgraded** (see the coupling
warning above).

## Design docs

Full design rationale and implementation plans for every sub-project (compat
rework, governance & safety, advanced trading / swaps / rentals / cross-chain,
mint spots, token sales) are under [`docs/superpowers/`](docs/superpowers/).
