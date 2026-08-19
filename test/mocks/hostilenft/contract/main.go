// Hostile NFT mock: a collection that misbehaves during delivery.
//
// The market never escrows. It writes its own state, then calls the NFT
// contract to move a unit from seller to buyer. Two claims rest on that
// ordering and neither can be tested against a well-behaved collection:
//
//   1. A FAILED transfer must abort the whole purchase, so a buyer is never
//      charged for a card that did not move.
//   2. CEI — the market's state is written BEFORE the external call, so a
//      collection that re-enters cannot observe units it has already been
//      promised.
//
// Claim 2 is checked by having this contract read the market's bucket counter
// from inside safeBatchTransferFrom and record what it saw. If the market
// flushed first, the value observed mid-transfer is already the POST-purchase
// count. If it flushed afterwards, the pre-purchase count would leak out here —
// which is exactly the window a re-entrant collection would exploit.
//
// Balances are stored the way magi_nft stores them, as raw little-endian bytes,
// because that is what the market's decodeNftU64 expects.
//
// JSON is hand-rolled — no tinyjson.

package main

import (
	"hostilenft/sdk"
)

//go:wasmexport init
func Init(payload *string) *string {
	out := `{"success":true}`
	return &out
}

// setOwner writes the collection owner the market reads for its soulbound
// check. Payload: {"owner":"hive:..."}
//
//go:wasmexport setOwner
func SetOwner(payload *string) *string {
	sdk.StateSetObject("owner", jsonStr(str(payload), "owner"))
	out := `{"success":true}`
	return &out
}

// setBalance credits a holder. Payload: {"account":"hive:..","id":"..","amount":N}
//
//go:wasmexport setBalance
func SetBalance(payload *string) *string {
	p := str(payload)
	acct := jsonStr(p, "account")
	id := jsonStr(p, "id")
	amt := jsonUint(p, "amount")
	if acct == "" || id == "" {
		sdk.Abort("account and id required")
	}
	sdk.StateSetObject("bal|"+acct+"|"+id, leBytes(amt))
	out := `{"success":true}`
	return &out
}

// setOperator grants blanket approval. Payload: {"owner":"..","operator":".."}
//
//go:wasmexport setOperator
func SetOperator(payload *string) *string {
	p := str(payload)
	sdk.StateSetObject("op|"+jsonStr(p, "owner")+"|"+jsonStr(p, "operator"), "1")
	out := `{"success":true}`
	return &out
}

// setMode arms the misbehaviour.
//
//	{"mode":"fail"}                                  -> abort every transfer
//	{"mode":"spy","market":"<id>","bucketId":<n>}    -> read the market's units
//	{"mode":"ok"}                                    -> behave
//
//go:wasmexport setMode
func SetMode(payload *string) *string {
	p := str(payload)
	sdk.StateSetObject("mode", jsonStr(p, "mode"))
	sdk.StateSetObject("market", jsonStr(p, "market"))
	sdk.StateSetObject("bkt", jsonRaw(p, "bucketId"))
	out := `{"success":true}`
	return &out
}

// safeBatchTransferFrom is what the market calls to deliver a draw.
//
//go:wasmexport safeBatchTransferFrom
func SafeBatchTransferFrom(payload *string) *string {
	mode := get("mode")

	if mode == "fail" {
		// The collection refuses. The market must treat this as fatal.
		sdk.Abort("hostile collection refuses to transfer")
	}

	if mode == "spy" {
		// Read the market's own bucket counter DURING the external call. This
		// is the value a re-entrant collection would act on.
		market := get("market")
		bkt := get("bkt")
		seen := sdk.ContractStateGet(market, "bkt|"+bkt+"|u")
		if seen == nil {
			sdk.StateSetObject("seen", "<absent>")
		} else {
			sdk.StateSetObject("seen", *seen)
		}
	}

	// Deliberately does NOT move balances: these tests assert on the market's
	// behaviour, not on this mock's bookkeeping.
	out := `{"success":true}`
	return &out
}

// ---- helpers ----

func str(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func get(key string) string {
	v := sdk.StateGetObject(key)
	if v == nil {
		return ""
	}
	return *v
}

// leBytes encodes a uint64 the way magi_nft does — least-significant byte
// first — so the market's decodeNftU64 reads it back correctly.
func leBytes(n uint64) string {
	b := make([]byte, 8)
	for i := 0; i < 8; i++ {
		b[i] = byte(n >> (8 * uint(i)))
	}
	return string(b)
}

func jsonStr(s, key string) string {
	needle := `"` + key + `":"`
	i := indexOf(s, needle)
	if i < 0 {
		return ""
	}
	rest := s[i+len(needle):]
	j := indexOf(rest, `"`)
	if j < 0 {
		return ""
	}
	return rest[:j]
}

// jsonRaw reads an unquoted value (a number) as text.
func jsonRaw(s, key string) string {
	needle := `"` + key + `":`
	i := indexOf(s, needle)
	if i < 0 {
		return ""
	}
	rest := s[i+len(needle):]
	end := 0
	for end < len(rest) {
		c := rest[end]
		if c == ',' || c == '}' || c == ' ' {
			break
		}
		end++
	}
	return rest[:end]
}

func jsonUint(s, key string) uint64 {
	raw := jsonRaw(s, key)
	var n uint64
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + uint64(c-'0')
	}
	return n
}

func indexOf(s, sub string) int {
	if len(sub) == 0 || len(sub) > len(s) {
		return -1
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		match := true
		for j := 0; j < len(sub); j++ {
			if s[i+j] != sub[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}
