// DEX pool mock for the magi-market contract-compatibility test harness.
//
// Models a minimal DEX pool that:
//   - Stores balances under "a-<account>" keys as big-endian uint64 (byte-
//     identical to utxo-mapping/utxomock) so magi-market's raw-read
//     tokenBalanceOf path works for escrowIn.
//   - Supports the magi-market escrowIn path via transferFrom + transfer.
//   - Implements the F0-verified DEX swap ABI:
//       swap {"asset_in":"<id>","amount_in":"<dec>","asset_out":"<id>",
//             "min_amount_out":"<dec>","to":"<recipient>"}
//     The mock debits the CALLER's a-<caller> balance by amount_in,
//     applies a fixed 5% mock spread (out = amount_in * 95 / 100),
//     aborts "slippage tolerance exceeded" if out < min_amount_out,
//     and credits a-<to> by out. Returns bare "0".
//   - TEST-ONLY "bal {account}" query → {"balance":"<dec>"} (NOT named
//     balanceOf, so magi-market never calls it directly).
//
// JSON is hand-rolled (substring scan, same pattern as utxomock) — NO tinyjson.
// Amounts are decimal strings in JSON payloads, int64 internally.

package main

import (
	"encoding/binary"
	"math/bits"
	"strconv"

	"dexmock/sdk"
)

func main() {}

// ---------------------------------------------------------------------------
// Hand-rolled JSON string-field extractor (substring scan, mirrors utxomock).
// Returns the value of `"<field>":"<value>"` or "" if absent.
// ---------------------------------------------------------------------------

func jsonStringField(s, field string) string {
	key := `"` + field + `":`
	idx := -1
	for i := 0; i+len(key) <= len(s); i++ {
		if s[i:i+len(key)] == key {
			idx = i + len(key)
			break
		}
	}
	if idx < 0 {
		return ""
	}
	for idx < len(s) && (s[idx] == ' ' || s[idx] == '\t' || s[idx] == '\n') {
		idx++
	}
	if idx >= len(s) || s[idx] != '"' {
		return ""
	}
	idx++
	end := idx
	for end < len(s) && s[end] != '"' {
		end++
	}
	if end > len(s) {
		return ""
	}
	return s[idx:end]
}

// ---------------------------------------------------------------------------
// Account-balance ledger — byte-identical to utxo-mapping getAccBal/setAccBal.
// State key "a-"+acct, value = BE uint64 with leading zero bytes trimmed;
// zero balance deletes the key.
// magi-market's tokenBalanceOf reads "a-<acct>" and decodes as BE-u64.
// ---------------------------------------------------------------------------

func balKey(account string) string { return "a-" + account }

func getAccBal(account string) int64 {
	s := sdk.StateGetObject(balKey(account))
	if s == nil || *s == "" {
		return 0
	}
	var buf [8]byte
	copy(buf[8-len(*s):], *s)
	return int64(binary.BigEndian.Uint64(buf[:]))
}

func setAccBal(account string, newBal int64) {
	if newBal == 0 {
		sdk.StateDeleteObject(balKey(account))
		return
	}
	v := uint64(newBal)
	n := (bits.Len64(v) + 7) / 8
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], v)
	sdk.StateSetObject(balKey(account), string(buf[8-n:]))
}

func credit(account string, amount int64) {
	setAccBal(account, getAccBal(account)+amount)
}

func debit(account string, amount int64) {
	cur := getAccBal(account)
	if cur < amount {
		sdk.Abort("insufficient balance")
	}
	setAccBal(account, cur-amount)
}

func parseAmount(s string) int64 {
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n < 0 {
		sdk.Abort("invalid amount")
	}
	return n
}

func caller() string {
	c := sdk.GetEnvKey("msg.caller")
	if c == nil {
		sdk.Abort("no caller")
	}
	return *c
}

func zero() *string {
	r := "0"
	return &r
}

// ---------------------------------------------------------------------------
// Exported entrypoints
// ---------------------------------------------------------------------------

//go:wasmexport init
func Init(payload *string) *string {
	p := ""
	if payload != nil {
		p = *payload
	}
	owner := jsonStringField(p, "owner")
	if owner != "" {
		sdk.StateSetObject("owner", owner)
	}
	sdk.StateSetObject("isInit", "1")
	return zero()
}

//go:wasmexport mint
func Mint(payload *string) *string {
	if payload == nil || *payload == "" {
		sdk.Abort("Payload required")
	}
	to := jsonStringField(*payload, "to")
	amount := jsonStringField(*payload, "amount")
	if to == "" || amount == "" {
		sdk.Abort("to and amount required")
	}
	credit(to, parseAmount(amount))
	return zero()
}

// transfer: caller debited full amount, recipient credited same (no fee in this mock).
// Called by magi-market's tokenTransferBig when paying the dexmock as paymentToken.
//
//go:wasmexport transfer
func Transfer(payload *string) *string {
	if payload == nil || *payload == "" {
		sdk.Abort("Payload required")
	}
	from := caller()
	to := jsonStringField(*payload, "to")
	amountStr := jsonStringField(*payload, "amount")
	if to == "" || amountStr == "" {
		sdk.Abort("to and amount required")
	}
	amount := parseAmount(amountStr)
	debit(from, amount)
	credit(to, amount)
	return zero()
}

// transferFrom: used by magi-market's escrowIn (calls transferFrom on the
// paymentToken to move funds from buyer to market).
//
//go:wasmexport transferFrom
func TransferFrom(payload *string) *string {
	if payload == nil || *payload == "" {
		sdk.Abort("Payload required")
	}
	from := jsonStringField(*payload, "from")
	to := jsonStringField(*payload, "to")
	amountStr := jsonStringField(*payload, "amount")
	if from == "" || to == "" || amountStr == "" {
		sdk.Abort("from, to and amount required")
	}
	amount := parseAmount(amountStr)
	// No 1% deduct fee in the dexmock — simpler accounting for F2 tests.
	// escrowIn's balance-delta will equal the full nominal amount.
	debit(from, amount)
	credit(to, amount)
	return zero()
}

// swap implements the F0-verified DEX pool swap ABI:
//   {"asset_in":"<id>","amount_in":"<dec>","asset_out":"<id>",
//    "min_amount_out":"<dec>","to":"<recipient>"}
//
// The mock tracks a single balance ledger (the a-<acct> ledger above) for
// both asset_in and asset_out. This is fine because:
//   - The marketplace is the only entity that holds asset_in (it escrowed it).
//   - The seller receives asset_out credits on the same ledger.
//   - asset_in / asset_out are distinct nominal token ids in the test, but
//     the mock models "the DEX has converted and delivered" by debiting
//     the caller's balance by amount_in and crediting `to` by `out`.
//
// Fixed mock rate: out = amount_in * 95 / 100 (5% mock spread).
// Aborts "slippage tolerance exceeded" if out < min_amount_out.
// Returns bare "0".
//
//go:wasmexport swap
func Swap(payload *string) *string {
	if payload == nil || *payload == "" {
		sdk.Abort("Payload required")
	}
	p := *payload
	amountInStr := jsonStringField(p, "amount_in")
	minAmountOutStr := jsonStringField(p, "min_amount_out")
	toAddr := jsonStringField(p, "to")
	if amountInStr == "" || toAddr == "" {
		sdk.Abort("amount_in and to required")
	}
	amountIn := parseAmount(amountInStr)

	// Fixed mock rate: 5% spread → out = amountIn * 95 / 100
	out := amountIn * 95 / 100

	// Check slippage if min_amount_out provided.
	if minAmountOutStr != "" {
		minOut := parseAmount(minAmountOutStr)
		if out < minOut {
			sdk.Abort("slippage tolerance exceeded")
		}
	}

	// Debit the caller (the marketplace holds asset_in after escrowIn).
	c := caller()
	debit(c, amountIn)

	// Credit the recipient with the output amount.
	credit(toAddr, out)

	return zero()
}

// bal is a TEST-ONLY balance query (NOT named balanceOf so magi-market never
// calls it via tokenBalanceOf). Returns {"balance":"<dec>"}.
//
//go:wasmexport bal
func Bal(payload *string) *string {
	if payload == nil || *payload == "" {
		sdk.Abort("Payload required")
	}
	account := jsonStringField(*payload, "account")
	if account == "" {
		sdk.Abort("account required")
	}
	r := `{"balance":"` + strconv.FormatInt(getAccBal(account), 10) + `"}`
	return &r
}
