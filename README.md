# magi-market

An ERC-1155 NFT marketplace smart contract for the VSC (Vite Smart Contract)
ecosystem, compiled to `wasm-unknown` with TinyGo. It supports fixed-price
listings, offers, auctions, multi-recipient royalties, NFT-for-NFT swaps,
rentals, cross-chain settlement, and primary-sale "mint spots" — all settled
in `magi_token` or UTXO-mapped coins (e.g. BTC), so an NFT can be traded for
BTC.

## Capabilities

| Area | What it does |
|---|---|
| **Listings & offers** | Fixed-price `list`/`buy`, `makeOffer`/`acceptOffer`, collection-wide offers, `updateListing`, `batchList`/`batchBuy`. NFTs stay with the seller via operator approval (no escrow). |
| **Auctions** | English auctions with anti-snipe extension and Dutch (declining-price) auctions. NFTs are escrowed for the auction's duration. |
| **Royalties & fees** | Single or multi-recipient royalty splits (≤10 recipients, Σ≤50%), snapshotted at listing and distributed at every payout. Per-collection fee override on top of the global marketplace fee. |
| **Governance & safety** | Two-step owner transfer (`changeOwner` → `acceptOwnership`), pause/unpause, retroactive collection denylist with deliberately-ungated recovery paths so denied collections never trap assets. |
| **Advanced trading** | Scheduled listings (`startBlock`), slippage-guarded floor `sweep`, single-collection atomic `listBundle`/`buyBundle`. |
| **Swaps** | `proposeSwap`/`acceptSwap` NFT-for-NFT barter with optional token top-up (fee only, no royalty on barter). |
| **Rentals** | Escrow-backed rental-rights attestation: NFT is escrowed owner↔market (never to the renter); `endRental` is callable by anyone after the term so nothing strands. |
| **Cross-chain settlement** | Opt-in per-listing native-L1 payout via UTXO `unmap`, or DEX-routed settlement (`dexSwapTo`) where the pool enforces slippage and delivers to the seller. |
| **Mint spots (primary sale)** | `listMintSpots`/`buyMintSpot`: the collection owner sells the right to mint editioned NFTs; the market performs a delegated mint directly to the buyer (the market never custodies the NFT). |

## Architecture

- **Money is arbitrary-precision.** All amounts are `big.Int` decimal strings
  (`parseMoney`/`formatMoney`), never `uint64`, so high-decimal tokens cannot
  overflow. Payouts use balance-delta accounting (`escrowIn` measures what was
  actually received) so fee-on-transfer tokens settle correctly.
- **Custody model.** Listings/offers/mint-spots use magi_nft operator approval
  (no NFT escrow). Auctions and rentals escrow the NFT for their duration.
- **Cross-contract reads are raw-state.** Balances/approvals/ownership of
  `magi_nft`, `magi_token`, and `utxo-mapping` are read with raw
  `sdk.ContractStateGet`, with decoders that mirror each contract's internal
  storage layout (magi_nft little-endian-u64, magi_token big-int bytes,
  utxo-mapping big-endian-u64). `maxSupply` is read via the stable `maxSupply`
  ABI call (unquoted JSON number).
- **Atomicity via abort.** Every multi-leg flow relies on `sdk.Abort`
  reverting the whole transaction, so a failed NFT transfer/mint refunds the
  buyer's escrow leg automatically. NFT transfer/mint always happens *before*
  payouts.

> **⚠️ External-contract coupling risk.** The raw-state decoders hardcode the
> *internal* storage layout of `magi_nft` / `magi_token` / `utxo-mapping`. If
> any of those contracts changes its internal key format or encoding,
> magi-market will silently misread balances (a fund bug with no compile
> error). The coupling warning lives above the decoders in
> `contract/internal.go`. **Before upgrading any of those three contracts,
> re-verify the decoders and regenerate the test wasm artifacts.** The
> delegated-mint ABI was last verified against
> `tibfox/magi_nft-contract@feat/editioned-define-delegated-mint` (commit
> `cebd5a0`).

## Repository layout

```
contract/
  main.go            entrypoint plumbing, owner/pause helpers
  market.go          listings, offers, mint spots, governance
  auction.go         English & Dutch auctions
  crosschain.go      unmap / DEX settlement, delegated mint wrapper
  internal.go        money helpers, raw-state cross-contract decoders
  money.go           per-entity money accessors
  events.go          event emitters
  types.go           payload/response structs
  types_tinyjson.go  HAND-MAINTAINED CosmWasm marshalers (see below)
test/                wasm-harness integration tests + hand-rolled mocks
docs/superpowers/    design specs and implementation plans (specs/, plans/)
```

## Build & test

The contract package only compiles under TinyGo (the SDK uses
`go:wasmimport`); there are no host unit tests — all tests are wasm-harness
integration tests.

```bash
# TinyGo 0.39 rejects host go1.26 — pin the toolchain for every command.
export GOTOOLCHAIN=go1.25.3

# Build the contract wasm.
tinygo build -gc=custom -scheduler=none -panic=trap -no-debug \
  -target=wasm-unknown -o test/artifacts/main.wasm ./contract

# Run the full suite.
GOTOOLCHAIN=go1.25.3 go test ./test/ -count=1
```

The harness also needs `test/artifacts/{token,nft}.wasm` and the mock
artifacts. Mocks live under `test/mocks/<name>/` and are built per-mock, e.g.:

```bash
cd test/mocks/mintnftmock && GOWORK=off GOTOOLCHAIN=go1.25.3 \
  tinygo build -gc=custom -scheduler=none -panic=trap -no-debug \
  -target=wasm-unknown -o ../../artifacts/mintnftmock.wasm ./contract
```

Notes:

- `go.mod` pins `vsc-node` via a `replace`; locally an **uncommitted,
  gitignored `go.work`** points it at your `go-vsc-node` checkout. `go.work`
  / `go.work.sum` and `test/artifacts/*.wasm` are gitignored — never commit
  them.
- **`types_tinyjson.go` is hand-maintained and must never be regenerated**
  (the package is `main`; `tinyjson` cannot bootstrap it). For a type change,
  edit the field's marshaler fragment by hand; for a new struct, append
  hand-written encode/decode mirroring the existing in-file patterns.
  Correctness is proven by the TinyGo build plus the full wasm suite.

## Initialization

```jsonc
// init payload
{ "feeBps": 250, "feeRecipient": "hive:treasury" }   // feeBps ≤ 10000 (2.5% here)
```

Only the contract owner (deploying account) may call `init`, once.

## Entrypoints

Core trading: `list` `buy` `delist` `updateListing` `batchList` `batchBuy`
`makeOffer` `acceptOffer` `cancelOffer` `getListing` `getOffer` `getMinOffer`
`setMinOffer` `acceptCollectionOffer`

Auctions: `createAuction` `placeBid` `settleAuction` `cancelAuction`
`getAuction` `setAntiSnipeBlocks` `setMinBidIncrement`

Royalties & fees: `setRoyalty` `getRoyalty` `setRoyaltySplits`
`getRoyaltySplits` `setFee` `setFeeRecipient` `setCollectionFee`
`clearCollectionFee` `getEffectiveFee`

Bundles & sweep: `listBundle` `buyBundle` `delistBundle` `sweep`

Swaps: `proposeSwap` `acceptSwap` `cancelSwap` `getSwap`

Rentals: `listRental` `rent` `endRental` `endRentalEarly` `delistRental`
`getRental` `getActiveRentalOf`

Mint spots: `listMintSpots` `buyMintSpot` `delistMintSpots`
`getMintSpotListing`

Governance & payment tokens: `changeOwner` `acceptOwnership`
`cancelOwnershipTransfer` `getPendingOwner` `pause` `unpause`
`denyCollection` `allowCollection` `isCollectionDenied`
`addPaymentToken` `removePaymentToken` `isPaymentTokenAllowed`
`emergencyWithdraw` `getInfo` `getOwner` `isPaused`

## Mint-spot authorization

`listMintSpots` requires the collection owner to have approved the
marketplace as an **operator** on the NFT contract
(`setApprovalForAll(market, true)`). A narrower per-token ERC-6909
`approve(spender=market, id, amount=N)` allowance is *not* sufficient to
create a mint-spot listing — the listing gate checks operator approval only.
`buyMintSpot` then delegated-mints directly to the buyer; the magi_nft
contract still enforces `maxSupply`.

## Design docs

Full design rationale and implementation plans for every sub-project (compat
rework, governance & safety, advanced trading/swaps/rentals/cross-chain, mint
spots) are under [`docs/superpowers/`](docs/superpowers/).
