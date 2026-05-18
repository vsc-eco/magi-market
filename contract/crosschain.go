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
// --- F2 (planned, NOT implemented here): Magi DEX router `swap` ---
//
//   Contract:   Magi DEX router (pool address for the token pair)
//   Method:     "swap"  (pool entrypoint, not a global router)
//   Payload:    SwapParams JSON with fields including `amount_in`,
//               `min_amount_out`, and `to` (output recipient).
//   Semantics:  market must hold `amountIn` of fromToken; swap delivers
//               at least `minAmountOut` of toToken to `to`; aborts if
//               slippage floor not met.
//   Measurement: balance-delta of toToken before/after to get actual `out`.
//   Note:       F2 implementation is in a separate task; this file will be
//               extended with a dexSwap helper at that time.
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
