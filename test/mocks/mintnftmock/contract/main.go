// Minimal NFT mock for the magi-market mint-spot test harness.
//
// Models the magi_nft-contract post-feature ABI documented in:
//   magi_nft-contract/docs/superpowers/specs/
//     2026-05-18-editioned-nft-define-delegated-mint-design.md
//
// Supported entrypoints:
//   init            {owner}             → {"success":true}
//   define          {id, maxSupply}     → {"success":true}
//   setApprovalForAll {operator,approved} → {"success":true}
//   mint            {to, id, amount}    → {"success":true}
//   balanceOf       {account, id}       → {"balance":"<dec>"}
//   maxSupply       {id}                → {"maxSupply":"<dec>"}
//   getOwner        (ignored)           → {"owner":"<addr>"}
//
// State keys:
//   owner               stored owner address
//   b|<acct>|<id>       balance (decimal string int)
//   ms|<id>             maxSupply of edition (decimal string uint64)
//   mt|<id>             minted count of edition (decimal string uint64)
//   op|<owner>|<oper>   "1" if operator approved
//
// JSON is hand-rolled (substring scan) — NO tinyjson.

package main

import (
	"strconv"

	"mintnftmock/sdk"
)

func main() {}

// ---------------------------------------------------------------------------
// Hand-rolled JSON field extractors (substring scan, mirrors dexmock/utxomock).
// ---------------------------------------------------------------------------

// jsonStringField returns the string value of a JSON string field, or "".
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

// jsonNumberField returns the numeric value of a JSON number field as a string, or "".
// Handles unquoted JSON numbers (e.g. "amount":42).
func jsonNumberField(s, field string) string {
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
	if idx >= len(s) {
		return ""
	}
	// If it's a quoted number, extract as string field
	if s[idx] == '"' {
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
	// Unquoted number
	end := idx
	for end < len(s) && s[end] != ',' && s[end] != '}' && s[end] != ' ' && s[end] != '\t' && s[end] != '\n' {
		end++
	}
	return s[idx:end]
}

// jsonBoolField returns whether a JSON bool field is true.
func jsonBoolField(s, field string) bool {
	key := `"` + field + `":`
	idx := -1
	for i := 0; i+len(key) <= len(s); i++ {
		if s[i:i+len(key)] == key {
			idx = i + len(key)
			break
		}
	}
	if idx < 0 {
		return false
	}
	for idx < len(s) && (s[idx] == ' ' || s[idx] == '\t' || s[idx] == '\n') {
		idx++
	}
	if idx+4 <= len(s) && s[idx:idx+4] == "true" {
		return true
	}
	return false
}

// ---------------------------------------------------------------------------
// State helpers
// ---------------------------------------------------------------------------

func getOwnerAddr() string {
	v := sdk.StateGetObject("owner")
	if v == nil {
		return ""
	}
	return *v
}

func getCaller() string {
	v := sdk.GetEnvKey("msg.caller")
	if v == nil {
		sdk.Abort("no caller")
	}
	return *v
}

// getUint64State returns the stored uint64 decimal string value for key, 0 if unset.
func getUint64State(key string) uint64 {
	v := sdk.StateGetObject(key)
	if v == nil || *v == "" {
		return 0
	}
	n, err := strconv.ParseUint(*v, 10, 64)
	if err != nil {
		return 0
	}
	return n
}

func setUint64State(key string, val uint64) {
	sdk.StateSetObject(key, strconv.FormatUint(val, 10))
}

func balKey(acct, id string) string   { return "b|" + acct + "|" + id }
func maxSupplyKey(id string) string   { return "ms|" + id }
func mintedKey(id string) string      { return "mt|" + id }
func operatorKey(owner, op string) string { return "op|" + owner + "|" + op }

func getBalance(acct, id string) uint64 {
	return getUint64State(balKey(acct, id))
}

func addBalance(acct, id string, amount uint64) {
	setUint64State(balKey(acct, id), getBalance(acct, id)+amount)
}

func success() *string {
	r := `{"success":true}`
	return &r
}

// ---------------------------------------------------------------------------
// Exported entrypoints
// ---------------------------------------------------------------------------

// init stores the owner and marks the contract initialised.
// Payload: {"owner":"<addr>"} — if owner is empty, falls back to msg.caller.
//
//go:wasmexport init
func Init(payload *string) *string {
	p := ""
	if payload != nil {
		p = *payload
	}
	owner := jsonStringField(p, "owner")
	if owner == "" {
		owner = getCaller()
	}
	sdk.StateSetObject("owner", owner)
	sdk.StateSetObject("isInit", "1")
	return success()
}

// define creates a new edition with the given maxSupply.
// Payload: {"id":"<id>","maxSupply":<uint>}
// Only the stored owner may call this.
//
//go:wasmexport define
func Define(payload *string) *string {
	if payload == nil || *payload == "" {
		sdk.Abort("Payload required")
	}
	p := *payload
	caller := getCaller()
	owner := getOwnerAddr()
	if caller != owner {
		sdk.Abort("Must be owner to define")
	}
	id := jsonStringField(p, "id")
	if id == "" {
		sdk.Abort("id required")
	}
	msStr := jsonNumberField(p, "maxSupply")
	if msStr == "" {
		sdk.Abort("maxSupply required")
	}
	ms, err := strconv.ParseUint(msStr, 10, 64)
	if err != nil || ms == 0 {
		sdk.Abort("maxSupply must be > 0")
	}
	if getUint64State(maxSupplyKey(id)) > 0 {
		sdk.Abort("Edition already defined")
	}
	setUint64State(maxSupplyKey(id), ms)
	setUint64State(mintedKey(id), 0)
	return success()
}

// setApprovalForAll approves or revokes an operator for the caller.
// Payload: {"operator":"<addr>","approved":<bool>}
// Caller must be the stored owner.
//
//go:wasmexport setApprovalForAll
func SetApprovalForAll(payload *string) *string {
	if payload == nil || *payload == "" {
		sdk.Abort("Payload required")
	}
	p := *payload
	caller := getCaller()
	owner := getOwnerAddr()
	if caller != owner {
		sdk.Abort("Must be owner to set approval")
	}
	operator := jsonStringField(p, "operator")
	if operator == "" {
		sdk.Abort("operator required")
	}
	approved := jsonBoolField(p, "approved")
	key := operatorKey(owner, operator)
	if approved {
		sdk.StateSetObject(key, "1")
	} else {
		sdk.StateDeleteObject(key)
	}
	return success()
}

// mint mints `amount` tokens of edition `id` to `to`.
// Payload: {"to":"<addr>","id":"<id>","amount":<uint>}
// Caller must be owner OR an approved operator of the owner.
//
//go:wasmexport mint
func Mint(payload *string) *string {
	if payload == nil || *payload == "" {
		sdk.Abort("Payload required")
	}
	p := *payload
	caller := getCaller()
	owner := getOwnerAddr()

	// Auth: caller must be owner or approved operator
	opKey := operatorKey(owner, caller)
	opVal := sdk.StateGetObject(opKey)
	isApproved := opVal != nil && *opVal == "1"
	if caller != owner && !isApproved {
		sdk.Abort("Must be owner or approved operator to mint")
	}

	to := jsonStringField(p, "to")
	if to == "" {
		sdk.Abort("to required")
	}
	id := jsonStringField(p, "id")
	if id == "" {
		sdk.Abort("id required")
	}
	amountStr := jsonNumberField(p, "amount")
	if amountStr == "" {
		sdk.Abort("amount required")
	}
	amount, err := strconv.ParseUint(amountStr, 10, 64)
	if err != nil || amount == 0 {
		sdk.Abort("amount must be > 0")
	}

	ms := getUint64State(maxSupplyKey(id))
	if ms == 0 {
		sdk.Abort("Edition not defined")
	}
	minted := getUint64State(mintedKey(id))
	if minted+amount > ms {
		sdk.Abort("Would exceed max supply")
	}

	addBalance(to, id, amount)
	setUint64State(mintedKey(id), minted+amount)
	return success()
}

// balanceOf returns the balance of `account` for edition `id`.
// Payload: {"account":"<addr>","id":"<id>"}
// Returns: {"balance":"<dec>"}
//
//go:wasmexport balanceOf
func BalanceOf(payload *string) *string {
	if payload == nil || *payload == "" {
		sdk.Abort("Payload required")
	}
	p := *payload
	account := jsonStringField(p, "account")
	id := jsonStringField(p, "id")
	if account == "" || id == "" {
		sdk.Abort("account and id required")
	}
	bal := getBalance(account, id)
	r := `{"balance":"` + strconv.FormatUint(bal, 10) + `"}`
	return &r
}

// maxSupply returns the maxSupply for edition `id` (0 if undefined).
// Payload: {"id":"<id>"}
// Returns: {"maxSupply":"<dec>"}
//
//go:wasmexport maxSupply
func MaxSupply(payload *string) *string {
	if payload == nil || *payload == "" {
		sdk.Abort("Payload required")
	}
	p := *payload
	id := jsonStringField(p, "id")
	if id == "" {
		sdk.Abort("id required")
	}
	ms := getUint64State(maxSupplyKey(id))
	r := `{"maxSupply":"` + strconv.FormatUint(ms, 10) + `"}`
	return &r
}

// getOwner returns the stored owner address.
// Payload: ignored.
// Returns: {"owner":"<addr>"}
//
//go:wasmexport getOwner
func GetOwner(payload *string) *string {
	owner := getOwnerAddr()
	r := `{"owner":"` + owner + `"}`
	return &r
}
