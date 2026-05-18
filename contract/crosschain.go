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
//
// --- G1: magi_nft-contract delegated `mint` (subsequent mint into a pre-defined edition) ---
//
//   Contract:   magi_nft-contract (any deployed instance; address supplied by
//               the mint-spot listing).
//   Method:     "mint"  (//go:wasmexport mint)
//   Payload:    {"to":"<addr str>","id":"<token id str>","amount":<uint64 number>}
//               Fields map directly to MintPayload{To, Id, Amount} in
//               magi_nft-contract/contract/types.go. maxSupply is omitted on
//               a subsequent mint into a pre-defined edition (the edition was
//               already defined with maxSupply set; the nft contract ignores
//               maxSupply on the subsequent-mint path).
//   Edition-exists signal: nft `maxSupply({"id":"<id>"})` returns > 0, which
//               is the same as nft `Exists` (getMaxSupply(id) > 0).
//   Auth:       caller must be the collection owner OR an operator approved
//               by the owner via `setApprovalForAll`. The market contract must
//               be approved: `setApprovalForAll({"operator":"<market>","approved":true})`.
//               The nft contract enforces this gate and aborts if not met.
//   Supply cap: the nft contract enforces maxSupply (aborts "Would exceed max
//               supply" → whole tx reverts → buyer refunded).
//   NOTE:       The magi_nft-contract delegated-mint + define-without-mint
//               feature is SPEC-ONLY and unimplemented as of 2026-05-18.
//               ABI per spec 2026-05-18-editioned-nft-define-delegated-mint-design.md;
//               real-nft re-verification is a tracked risk. The market side is
//               proven against mintnftmock (models the documented ABI) and will
//               require a final integration pass once the upstream feature ships.
//               Do NOT modify the nft repo.
// ============================================================================

import (
	"math/big"
	"magi_market/sdk"
	"strconv"
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
