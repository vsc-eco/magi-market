// Fee-on-transfer mock token (1% fee, integer floor) for the magi-market
// contract-compatibility test harness.
//
// Wire-compatible with what magi-market emits in tokenTransferBig /
// tokenTransferFromBig / tokenBalanceOf:
//
//   transfer     {"to":"<addr>","amount":"<dec>"}
//   transferFrom {"from":"<addr>","to":"<addr>","amount":"<dec>"}
//   balanceOf    {"account":"<addr>"}  ->  {"balance":"<dec>"}
//
// On every transfer/transferFrom the sender (caller / `from`) is debited the
// FULL `amount`, while the recipient is credited `amount - floor(amount/100)`
// (a 1% fee that is simply burned). This makes the balance-delta the
// marketplace measures via escrowIn smaller than the nominal amount, which is
// exactly the fee-on-transfer / utxo deduct_fee behaviour under test.
//
// JSON is hand-rolled (substring scan, same pattern as magi-market
// contract/internal.go) — NO tinyjson. Balances are stored as decimal
// strings under `bal|<account>` state keys; amounts use math/big.

package main

import (
	"math/big"

	"feetoken/sdk"
)

func main() {}

// ---------------------------------------------------------------------------
// Hand-rolled JSON string-field extractor (substring scan, mirrors the
// pattern in magi-market contract/internal.go tokenBalanceOf).
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
// Balance ledger helpers
// ---------------------------------------------------------------------------

func balKey(account string) string { return "bal|" + account }

// bigIntToBytes / getBal / setBal mirror magi_token-contract's byte storage:
// big.Int.Bytes() == big-endian unsigned magnitude. magi-market's new raw-read
// tokenBalanceOf decodes the `bal|<acct>` value via new(big.Int).SetBytes.
func bigIntToBytes(v *big.Int) []byte {
	b := v.Bytes()
	if len(b) == 0 {
		return []byte{0}
	}
	return b
}

func getBal(account string) *big.Int {
	v := sdk.StateGetObject(balKey(account))
	if v == nil || *v == "" {
		return big.NewInt(0)
	}
	return new(big.Int).SetBytes([]byte(*v))
}

func setBal(account string, amount *big.Int) {
	sdk.StateSetObject(balKey(account), string(bigIntToBytes(amount)))
}

func credit(account string, amount *big.Int) {
	setBal(account, new(big.Int).Add(getBal(account), amount))
}

func debit(account string, amount *big.Int) {
	cur := getBal(account)
	if cur.Cmp(amount) < 0 {
		sdk.Abort("insufficient balance")
	}
	setBal(account, new(big.Int).Sub(cur, amount))
}

func parseAmount(s string) *big.Int {
	n, ok := new(big.Int).SetString(s, 10)
	if !ok || n.Sign() < 0 {
		sdk.Abort("invalid amount")
	}
	return n
}

// fee = floor(amount / 100) (1%).
func feeOf(amount *big.Int) *big.Int {
	return new(big.Int).Div(amount, big.NewInt(100))
}

func caller() string {
	c := sdk.GetEnvKey("msg.caller")
	if c == nil {
		sdk.Abort("no caller")
	}
	return *c
}

func okBalance(amount *big.Int) *string {
	r := `{"balance":"` + amount.String() + `"}`
	return &r
}

func okSuccess() *string {
	r := `{"success":true}`
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
	supply := jsonStringField(p, "supply")
	if supply != "" && owner != "" {
		credit(owner, parseAmount(supply))
	}
	sdk.StateSetObject("isInit", "1")
	return okSuccess()
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
	return okSuccess()
}

//go:wasmexport balanceOf
func BalanceOf(payload *string) *string {
	if payload == nil || *payload == "" {
		sdk.Abort("Payload required")
	}
	account := jsonStringField(*payload, "account")
	if account == "" {
		sdk.Abort("account required")
	}
	return okBalance(getBal(account))
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
	// Debit sender the FULL amount, credit recipient amount-fee (1% burned).
	debit(from, amount)
	credit(to, new(big.Int).Sub(amount, feeOf(amount)))
	return okSuccess()
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
	// amount, credit `to` amount-fee (1% burned).
	debit(from, amount)
	credit(to, new(big.Int).Sub(amount, feeOf(amount)))
	return okSuccess()
}
