package main

// ============================================================================
// Cross-chain settlement helpers — verified ABIs (F0 verification).
//
// --- F1: utxo-mapping `unmap` ---
//
//   Contract:   utxo-mapping/btc-mapping-contract (or equivalent per chain)
//   Method:     "unmap"
//   Payload:    {"to":"<l1address>","amount":"<decimal>"}
//   Semantics:  draws from CALLER's balance (the `from` field is ignored;
//               caller is always the source).  The marketplace already holds
//               the mapped payment token in its own balance after escrowIn,
//               so no allowance or `from` field is needed.
//   Returns:    bare string "0" on success (NOT {"success":true}).
//               Abort only if sdk.ContractCallSimple returns nil.
//
//   Verified:   F0 task — ABI confirmed against
//               /mnt/HC_Volume_105012347/magi/testnet/utxo-mapping/
//               btc-mapping-contract/contract (commit 6039c43).
//
// --- F2: Magi DEX pool `swap` ---
//
//   Contract:   Magi DEX pool (address stored per-listing as `dp`)
//   Method:     "swap"  (pool entrypoint, not a global router)
//   Payload:    {"asset_in":"<pt>","amount_in":"<decimal>","asset_out":"<st>",
//               "min_amount_out":"<mso>","to":"<seller>"}
//   Semantics:  market calls the pool's swap with `to:seller` and
//               `min_amount_out:<mso>`; the pool delivers `asset_out`
//               directly to the seller and enforces slippage itself (pool
//               aborts if output < min_amount_out → whole tx reverts).
//               The market does NOT balance-delta-measure the output.
// ============================================================================

import (
	"math/big"
	"magi_market/sdk"
)

// unmapTo sends `amount` of the (utxo-mapped) token from THIS contract's
// balance to L1 address `l1` via the utxo-mapping contract's `unmap`
// entrypoint (caller = source; verified F0). Aborts on failure (revert).
func unmapTo(utxoContract, l1 string, amount *big.Int) {
	payload := `{"to":"` + l1 + `","amount":"` + formatMoney(amount) + `"}`
	if sdk.ContractCallSimple(utxoContract, "unmap", payload) == nil {
		sdk.Abort("unmap call failed")
	}
}

// dexSwapTo swaps `amountIn` of `assetIn` (held by THIS contract from escrow)
// into `assetOut` via the DEX `pool` contract, delivered directly to `to`,
// with the pool enforcing `minOut` (it aborts on slippage → whole tx
// reverts). Aborts if the call fails. F0-verified ABI.
func dexSwapTo(pool, assetIn, assetOut, to, amountIn, minOut string) {
	payload := `{"asset_in":"` + assetIn + `","amount_in":"` + amountIn + `","asset_out":"` + assetOut + `","min_amount_out":"` + minOut + `","to":"` + to + `"}`
	if sdk.ContractCallSimple(pool, "swap", payload) == nil {
		sdk.Abort("dex swap call failed")
	}
}
