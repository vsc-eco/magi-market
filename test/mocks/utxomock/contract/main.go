// UTXO-mapping-style mock token for the magi-market contract-compatibility
// test harness.
//
// This mock mirrors utxo-mapping/btc-mapping-contract account-balance storage
// EXACTLY (commit 6039c43, contract/mapping/utils.go getAccBal/setAccBal):
//
//   - state key:   "a-" + account            (BalancePrefix = "a" + "-")
//   - state value: big-endian uint64 of the int64 satoshi balance, with
//                   leading zero bytes trimmed; a zero balance DELETES the key
//
// CRUCIAL: this mock has NO `balanceOf` entrypoint and NO `bal|<acct>` key.
// magi-market's new raw-read tokenBalanceOf first probes `bal|<acct>`
// (magi_token form, absent here) then falls back to raw-reading `a-<acct>`
// and decoding it as BE-u64 via decodeUtxoU64. So every balance magi-market
// observes for this token is proof the `a-<acct>` BE-u64 raw-read path works.
//
// transfer / transferFrom apply a 1% (floor) deduct fee exactly like
// utxo-mapping's unmap deduct_fee: the payer is debited the FULL amount, the
// recipient credited amount - amount/100. Entrypoints return the bare string
// "0" like real utxo-mapping handlers. A TEST-ONLY `tbal` query (distinct
// name, NOT balanceOf) lets the test read balances; magi-market never calls
// it.
//
// JSON is hand-rolled (substring scan, same pattern as magi-market
// contract/internal.go) — NO tinyjson. Amounts are decimal strings in JSON
// payloads, int64 satoshis internally.

package main

import (
	"encoding/binary"
	"math/bits"
	"strconv"

	"utxomock/sdk"
)

func main() {}

// ---------------------------------------------------------------------------
// Hand-rolled JSON string-field extractor (substring scan).
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
// ---------------------------------------------------------------------------

func balKey(account string) string { return "a-" + account }

// getAccBal mirrors utxo-mapping getAccBal byte logic verbatim.
func getAccBal(account string) int64 {
	s := sdk.StateGetObject(balKey(account))
	if s == nil || *s == "" {
		return 0
	}
	var buf [8]byte
	copy(buf[8-len(*s):], *s)
	return int64(binary.BigEndian.Uint64(buf[:]))
}

// setAccBal mirrors utxo-mapping setAccBal byte logic verbatim.
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

// feeOf = floor(amount / 100) (1% deduct fee, like utxo-mapping unmap).
func feeOf(amount int64) int64 { return amount / 100 }

func caller() string {
	c := sdk.GetEnvKey("msg.caller")
	if c == nil {
		sdk.Abort("no caller")
	}
	return *c
}

// zero is the bare success return real utxo-mapping handlers emit.
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
	// Debit sender the FULL amount, credit recipient amount-fee (1% deduct).
	debit(from, amount)
	credit(to, amount-feeOf(amount))
	return zero()
}

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
	// Allowance intentionally ignored (minimal mock). Debit `from` the FULL
	// amount, credit `to` amount-fee (1% deduct).
	debit(from, amount)
	credit(to, amount-feeOf(amount))
	return zero()
}

// tbal is a TEST-ONLY balance query (distinct name, NOT balanceOf, so
// magi-market never calls it — the raw-read `a-<acct>` path stays the only
// way the marketplace sees balances). Returns {"balance":"<dec>"}.
//
//go:wasmexport tbal
func Tbal(payload *string) *string {
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
