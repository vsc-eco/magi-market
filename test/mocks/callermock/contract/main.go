// Caller mock: a contract that calls magi-market on a user's behalf.
//
// It exists to prove ONE thing — that `buyFromBucket` refuses to be called by a
// contract. That guard is what closes the retry-on-loss attack: without it a
// buyer wraps the purchase in their own contract, inspects the drawn token and
// aborts on a bad result, so the whole transaction reverts and the draw costs
// them only RC. They then retry until they win, turning a fair draw into a
// pick.
//
// The market sees msg.caller = this contract while msg.sender stays the human
// who signed, so requiring caller == sender rejects exactly this shape.
//
// JSON is hand-rolled — no tinyjson.

package main

import (
	"callermock/sdk"
)

//go:wasmexport init
func Init(payload *string) *string {
	out := `{"success":true}`
	return &out
}

// buyThrough forwards a buyFromBucket call to the market. Payload:
//
//	{"market":"<contract id>","bucketId":<n>}
//
// A successful return here would mean the guard is missing.
//
//go:wasmexport buyThrough
func BuyThrough(payload *string) *string {
	if payload == nil || *payload == "" {
		sdk.Abort("payload required")
	}
	market := jsonStr(*payload, "market")
	bucketId := jsonRaw(*payload, "bucketId")
	if market == "" || bucketId == "" {
		sdk.Abort("market and bucketId required")
	}

	inner := `{"bucketId":` + bucketId + `,"mode":"single","quantity":1,"maxTotalPrice":""}`
	if sdk.ContractCallSimple(market, "buyFromBucket", inner) == nil {
		sdk.Abort("market refused the call")
	}
	out := `{"success":true}`
	return &out
}

// ---- tiny hand-rolled JSON readers ----

// jsonStr reads a "key":"value" string field.
func jsonStr(s, key string) string {
	needle := `"` + key + `":"`
	i := indexOf(s, needle)
	if i < 0 {
		return ""
	}
	rest := s[i+len(needle):]
	for j := 0; j < len(rest); j++ {
		if rest[j] == '"' {
			return rest[:j]
		}
	}
	return ""
}

// jsonRaw reads a "key":<literal> field, returning it unquoted.
func jsonRaw(s, key string) string {
	needle := `"` + key + `":`
	i := indexOf(s, needle)
	if i < 0 {
		return ""
	}
	rest := s[i+len(needle):]
	for j := 0; j < len(rest); j++ {
		c := rest[j]
		if c == ',' || c == '}' {
			return rest[:j]
		}
	}
	return ""
}

func indexOf(s, sub string) int {
	if len(sub) == 0 || len(sub) > len(s) {
		return -1
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
