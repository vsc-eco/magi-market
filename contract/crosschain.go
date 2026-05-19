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
//               "min_amount_out":"<mso>","from":"<this contract>",
//               "to":"<seller>"}
//               SwapParams.AmountIn/MinAmountOut are STRINGS (the pool's
//               main.go doc-comment showing unquoted numbers is stale; the
//               struct + tinyjson decode via in.String()). The recipient key
//               is `to` (not `recipient`).
//   from:       MANDATORY. The pool aborts if VerifyAddress(from)=="unknown"
//               and DrawAssetFrom requires from == caller (no allowance
//               pull). The market is the caller and holds asset_in after
//               escrowIn, so `from` = this contract's own address.
//   Semantics:  market calls the pool's swap with `to:seller` and
//               `min_amount_out:<mso>`; the pool delivers `asset_out`
//               directly to the seller and enforces slippage itself (pool
//               aborts if output < min_amount_out → whole tx reverts).
//               The market does NOT balance-delta-measure the output.
//   Verified:   Against testnet dex-contracts contracts/dex/main.go +
//               contracts/types/types.go + contracts/asset/asset.go @
//               09af7ee (2026-05-18). The asset_in/asset_out↔pool-asset-name
//               mapping is testnet-config-specific (lister-supplied) and is
//               an on-chain smoke-test item, not a contract concern.
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
//   Auth:       caller must be the collection owner, an operator the owner
//               approved via `setApprovalForAll`, OR a per-token ERC-6909
//               allowance the owner granted via `approve(spender=<market>,
//               id, amount=N)` (capped at N, decremented per mint). The nft
//               contract enforces this gate and aborts if unmet. magi-market
//               accepts either operator approval or a per-token allowance to
//               authorize a mint-spot listing (see listMintSpots /
//               nftAllowanceOf).
//   maxSupply:  read via the `maxSupply` ABI, which returns an UNQUOTED JSON
//               number {"maxSupply":<uint64>} (out.Uint64) — nftMaxSupplyOf
//               parses that (and tolerates the quoted form).
//   Supply cap: the nft contract enforces maxSupply (aborts "Would exceed max
//               supply" → whole tx reverts → buyer refunded).
//   Verified:   IMPLEMENTED and verified field-by-field against upstream
//               tibfox/magi_nft-contract@feat/editioned-define-delegated-mint
//               commit cebd5a0 (2026-05-18). nftDelegatedMint payload,
//               nftGetOwner (`owner`), nftIsApprovedForAll (`op|o|op`="1"),
//               nftAllowanceOf (`allow|o|s|id` LE-u64) and the maxSupply
//               number format all confirmed against that source. Re-verify
//               before any magi_nft internal-storage/ABI upgrade. Do NOT
//               modify the nft repo.
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
// reverts). Aborts if the call fails.
//
// The pool's DrawAssetFrom requires `from == caller` and hard-aborts if
// `from` is absent/invalid (VerifyAddress == "unknown"). Since the
// marketplace is the caller and already holds `assetIn` after escrowIn, we
// pass `from` = this contract's own address (== the caller the pool sees).
// Verified against testnet dex-contracts contracts/dex/main.go +
// contracts/asset/asset.go @ 09af7ee (2026-05-18).
func dexSwapTo(pool, assetIn, assetOut, to, amountIn, minOut string) {
	from := getContractAddress()
	payload := `{"asset_in":"` + assetIn + `","amount_in":"` + amountIn + `","asset_out":"` + assetOut + `","min_amount_out":"` + minOut + `","from":"` + from + `","to":"` + to + `"}`
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
