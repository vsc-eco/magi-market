# magi-market — Testnet Smoke-Test Plan

Purpose: empirically validate the cross-contract couplings that the wasm
harness + mocks **cannot** prove, against real deployed contracts. Treat the
first testnet phase as *integration verification, not production*.

## Why this matters

magi-market reads other contracts' **internal state** raw and calls their
ABIs by hand. Mocks reproduce assumed behaviour; they have twice masked real
bugs that only surfaced when checked against the actual source:

- `nftMaxSupplyOf` parsed a quoted `maxSupply` but real magi_nft emits an
  unquoted number — would have aborted *every* `listMintSpots`.
- `dexSwapTo` omitted `from`; the real DEX pool hard-aborts a `from`-less
  swap — would have reverted *every* DEX-routed settlement.

Both are fixed. The static re-verification below is done; the on-chain pass
is the final oracle.

## Static verification status (completed 2026-05-18)

| Coupling | Surface | Verified against | Result |
|---|---|---|---|
| magi_nft balance/owner/operator/allowance | raw `bal\|`,`owner`,`op\|`,`allow\|` + LE-u64 | `tibfox/magi_nft-contract@cebd5a0` | ✅ match |
| magi_nft `mint` / `maxSupply` ABI | payload + unquoted-number response | `@cebd5a0` | ✅ match (parser fixed) |
| magi_token balance | raw `bal\|<acct>` + BE big.Int.Bytes() | testnet `magi_token-contract@a819106` | ✅ match |
| utxo-mapping balance | raw `a-<acct>` + BE-u64-trimmed | testnet `utxo-mapping@6039c43` (btc/dash/ltc/bch/doge) | ✅ match |
| utxo-mapping `unmap` ABI | `{to,amount(str)}`, caller=source, ret `"0"` | testnet `utxo-mapping@6039c43` | ✅ match |
| DEX pool `swap` ABI | `{asset_in,amount_in(str),asset_out,min_amount_out(str),from,to}` | testnet `dex-contracts@09af7ee` | ✅ match (from fix applied) |

**Residual coupling risk:** all of the above hardcode external internal
layouts. Re-run this verification before any upgrade of magi_nft /
magi_token / utxo-mapping / dex-contracts. See the COUPLING WARNING in
`contract/internal.go` and the ABI block in `contract/crosschain.go`.

## Deployment order

1. Ensure deployed instances exist for: magi_token, the relevant
   utxo-mapping contract(s), magi_nft, and (if using F2) a funded DEX pool.
2. Deploy magi-market; `init {feeBps, feeRecipient}` as the owner account.
3. `addPaymentToken` for each payment asset to be used (magi_token id, the
   utxo-mapping contract id, and/or the DEX pool's input-asset id).

## On-chain smoke checks

Each check is "ready" only when observed on testnet with real contracts.

### S1 — magi_token fixed-price buy
- Seller `setApprovalForAll(market)` on magi_nft; `list` priced in magi_token.
- Buyer `approve`s market on magi_token; `buy`.
- Assert: NFT → buyer; fee → feeRecipient; royalty (if set) → recipient;
  remainder → seller; market residual balance == 0.

### S2 — UTXO-mapped buy (trade NFT for BTC-class coin)
- Use a real `hive:`/`contract:` canonical address (utxo-mapping
  `VerifyAddress` rejects stub ids).
- Buyer holds a utxo-mapped balance (`a-<acct>`); `list` priced in the
  utxo-mapping contract id; `buy`.
- Assert: market raw-reads the buyer's mapped balance correctly (no
  under/over-read); payout splits exact; residual 0.

### S3 — Mint-spot delegated primary sale vs real magi_nft
- Creator defines an edition on real magi_nft (`mint` amount=0).
- 3a (operator): creator `setApprovalForAll(market,true)`; `listMintSpots`
  (maxSpots may be 0); buyer `buyMintSpot`; assert NFT minted **to buyer**,
  market never holds it, magi_nft `maxSupply` cap still enforced.
- 3b (per-token allowance): creator `approve(spender=market,id,N)` only;
  `listMintSpots` must require `0 < maxSpots ≤ N`; buy N; assert magi_nft
  allowance decrements N→0 and the listing sells out; the N+1 buy reverts.

### S4 — English auction lifecycle
- Seller `createAuction` (NFT escrowed to market); two bidders `placeBid`
  (prior bidder auto-refunded; anti-snipe extends near end).
- After `endBlock`, a **third party** calls `settleAuction`: assert NFT →
  winner, payout splits exact, and that no seller action was required.
- Also: no-bid auction settle returns NFT to seller; `cancelAuction` with a
  live bid is rejected.

### S5 — F1 native-L1 unmap payout
- `list` with `unmapTo` set to a real L1 address; buy.
- Assert seller receives the native L1 payout via utxo-mapping `unmap`
  (drawn from the market's post-escrow balance).
- Edge: `unmap` amount is parsed as **int64**. Confirm the priced amount in
  base units is < 2^63. An overflow aborts+refunds atomically (not silent),
  but should be avoided by pricing.

### S6 — F2 DEX-routed settlement
- Pre-req: the listing's `settleToken`/`paymentToken` strings MUST be the
  **pool's configured asset names** (testnet-config-specific; the pool
  rejects an unknown asset pair). Confirm the exact names on the chosen pool
  first.
- `list` with `dexPool` + `settleToken` + `minSettleOut > 0`; buy.
- Assert: pool draws `amount_in` from the **market** (from==caller), the
  seller receives `asset_out` directly, slippage (`min_amount_out`) is
  enforced by the pool (set `minSettleOut` above achievable to confirm the
  whole tx reverts), market residual 0.

### S7 — Governance / safety sanity
- `denyCollection` then confirm create-paths blocked and recovery paths
  (`delist`, `cancel*`, `emergencyWithdraw`, deny-aware `settleAuction`)
  still free the NFT/escrow.
- 2-step owner transfer: `changeOwner` → `acceptOwnership`; confirm it works
  while paused.

## Sign-off

The contract is "ready to trust with value on testnet" once S1–S7 pass on
real contracts. Until then it is "ready to deploy to testnet for
verification". Record observed results against each S-item.
