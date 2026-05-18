# Governance & Safety Hardening (Sub-project A) — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add 2-step owner transfer and a retroactive collection denylist to magi-market, hardening the fund-handling contract without new cross-contract coupling.

**Architecture:** Two isolated, additive features on the post-rework contract. `changeOwner` becomes propose-only (`pending_owner` state); a new `acceptOwnership` finalizes. A `dl|<nftContract>` denylist gates creation AND completion paths (retroactive) while recovery paths (delist/cancel*/emergencyWithdraw) stay ungated so value is never trapped. New JSON structs are hand-added to the generated `types_tinyjson.go` (the generator cannot run on a package-main contract — established constraint).

**Tech Stack:** Go (tinygo `wasm-unknown`), CosmWasm tinyjson (hand-maintained), vsc-node wasm test harness.

**Spec:** `docs/superpowers/specs/2026-05-18-governance-safety-design.md`

**Build/run invariants (every build/test step assumes these):**
- `export GOTOOLCHAIN=go1.25.3` for every go/tinygo command (host go is 1.26; tinygo 0.39 needs ≤1.25).
- Gitignored `go.work` already pins `vsc-node` locally (do NOT commit it).
- WASM build: `tinygo build -gc=custom -scheduler=none -panic=trap -no-debug -target=wasm-unknown -o test/artifacts/main.wasm ./contract`
- Test: `GOTOOLCHAIN=go1.25.3 go test ./test/ -count=1` (full suite ~60s; use `-run` while iterating).
- **NEVER run tinyjson** — `contract/types_tinyjson.go` is hand-maintained. New structs get hand-written encode/decode helpers + wrapper methods, mirroring existing in-file patterns. Correctness is proven by tinygo build + the full wasm suite (the regression oracle).
- Git: work on branch `feature/governance-safety` (already created off `0ed11b9`). Normal `git commit` (never `--amend` across tasks, never detach HEAD). Before every commit verify `git branch --show-current` == `feature/governance-safety`; after, verify `git rev-parse feature/governance-safety` == HEAD. Commit messages multi-line, NO Co-Authored-By / NO Claude attribution. No PRs.
- Baseline at branch start: full suite 252/252 green.

---

## Task 0: Confirm baseline green

**Files:** none (verification only)

- [ ] **Step 1: Verify branch + baseline**

Run:
```bash
cd /home/dockeruser/magi/magi-market
git branch --show-current
export GOTOOLCHAIN=go1.25.3
tinygo build -gc=custom -scheduler=none -panic=trap -no-debug -target=wasm-unknown -o test/artifacts/main.wasm ./contract && echo BUILD_OK
go test ./test/ -count=1 2>&1 | tail -2
```
Expected: branch `feature/governance-safety`; `BUILD_OK`; `ok  	magi_market/test` (252 pass). If not green, STOP and report — do not start feature work on a red baseline.

---

## Task 1: 2-step owner transfer

**Files:**
- Modify: `contract/types.go` (add 2 structs + 2 event/attr struct pairs)
- Modify: `contract/types_tinyjson.go` (hand-add encode/decode + wrappers for the new structs)
- Modify: `contract/internal.go` (pending-owner state helpers)
- Modify: `contract/market.go` (rewrite `ChangeOwner`; add `AcceptOwnership`, `CancelOwnershipTransfer`, `GetPendingOwner`)
- Modify: `contract/events.go` (`emitOwnerTransferInitiated`, `emitOwnerTransferCancelled`)
- Modify: `test/basic_test.go` (update `TestChangeOwner` to 2-step), `test/edge_cases_test.go` (update `TestChangeOwnerThenAdminActions` to 2-step)
- Create: `test/governance_test.go` (new 2-step transfer tests)

- [ ] **Step 1: Add structs to `contract/types.go`**

After the existing `ChangeOwnerPayload` struct, add:
```go
type PendingOwnerResponse struct {
	PendingOwner string `json:"pendingOwner"`
}

type OwnerTransferInitiatedEvent struct {
	Type       string                           `json:"type"`
	Attributes OwnerTransferInitiatedAttributes `json:"attributes"`
	Tx         string                           `json:"tx"`
}

type OwnerTransferInitiatedAttributes struct {
	CurrentOwner string `json:"currentOwner"`
	PendingOwner string `json:"pendingOwner"`
}

type OwnerTransferCancelledEvent struct {
	Type       string                           `json:"type"`
	Attributes OwnerTransferCancelledAttributes `json:"attributes"`
	Tx         string                           `json:"tx"`
}

type OwnerTransferCancelledAttributes struct {
	By string `json:"by"`
}
```

- [ ] **Step 2: Hand-add tinyjson for the new structs in `contract/types_tinyjson.go`**

The generator cannot run. Append the following hand-written marshalers at the END of `contract/types_tinyjson.go` (distinct, clearly hand-named helpers so they never collide with the numbered generated ones; pattern mirrors the in-file `GetRoyaltyPayload` (single string) and `OwnerChangeEvent` (event+attrs) shapes):

```go
// ---- hand-added (sub-project A): governance structs ----

func tinyjsonGovDecodePendingOwnerResponse(in *jlexer.Lexer, out *PendingOwnerResponse) {
	isTopLevel := in.IsStart()
	if in.IsNull() {
		if isTopLevel {
			in.Consumed()
		}
		in.Skip()
		return
	}
	in.Delim('{')
	for !in.IsDelim('}') {
		key := in.UnsafeFieldName(false)
		in.WantColon()
		if in.IsNull() {
			in.Skip()
			in.WantComma()
			continue
		}
		switch key {
		case "pendingOwner":
			out.PendingOwner = string(in.String())
		default:
			in.SkipRecursive()
		}
		in.WantComma()
	}
	in.Delim('}')
	if isTopLevel {
		in.Consumed()
	}
}

func tinyjsonGovEncodePendingOwnerResponse(out *jwriter.Writer, in PendingOwnerResponse) {
	out.RawByte('{')
	{
		const prefix string = ",\"pendingOwner\":"
		out.RawString(prefix[1:])
		out.String(string(in.PendingOwner))
	}
	out.RawByte('}')
}

func (v PendingOwnerResponse) MarshalTinyJSON(w *jwriter.Writer) {
	tinyjsonGovEncodePendingOwnerResponse(w, v)
}
func (v *PendingOwnerResponse) UnmarshalTinyJSON(l *jlexer.Lexer) {
	tinyjsonGovDecodePendingOwnerResponse(l, v)
}

func tinyjsonGovEncodeOwnerTransferInitiated(out *jwriter.Writer, in OwnerTransferInitiatedEvent) {
	out.RawByte('{')
	{
		const prefix string = ",\"type\":"
		out.RawString(prefix[1:])
		out.String(string(in.Type))
	}
	{
		const prefix string = ",\"attributes\":"
		out.RawString(prefix)
		out.RawByte('{')
		{
			const p2 string = ",\"currentOwner\":"
			out.RawString(p2[1:])
			out.String(string(in.Attributes.CurrentOwner))
		}
		{
			const p2 string = ",\"pendingOwner\":"
			out.RawString(p2)
			out.String(string(in.Attributes.PendingOwner))
		}
		out.RawByte('}')
	}
	{
		const prefix string = ",\"tx\":"
		out.RawString(prefix)
		out.String(string(in.Tx))
	}
	out.RawByte('}')
}

func (v OwnerTransferInitiatedEvent) MarshalTinyJSON(w *jwriter.Writer) {
	tinyjsonGovEncodeOwnerTransferInitiated(w, v)
}

func tinyjsonGovEncodeOwnerTransferCancelled(out *jwriter.Writer, in OwnerTransferCancelledEvent) {
	out.RawByte('{')
	{
		const prefix string = ",\"type\":"
		out.RawString(prefix[1:])
		out.String(string(in.Type))
	}
	{
		const prefix string = ",\"attributes\":"
		out.RawString(prefix)
		out.RawByte('{')
		{
			const p2 string = ",\"by\":"
			out.RawString(p2[1:])
			out.String(string(in.Attributes.By))
		}
		out.RawByte('}')
	}
	{
		const prefix string = ",\"tx\":"
		out.RawString(prefix)
		out.String(string(in.Tx))
	}
	out.RawByte('}')
}

func (v OwnerTransferCancelledEvent) MarshalTinyJSON(w *jwriter.Writer) {
	tinyjsonGovEncodeOwnerTransferCancelled(w, v)
}
```

(Only the entrypoint input/`jsonResponse` output structs need `Unmarshal`/`Marshal`; events only need `MarshalTinyJSON` because `events.go` only marshals them. `PendingOwnerResponse` gets both because it is a query response built via `jsonResponse` — which calls `MarshalTinyJSON` — and is decoded by the test's `ParsePendingOwner`.)

- [ ] **Step 3: Add pending-owner state helpers to `contract/internal.go`**

Append near the other state helpers:
```go
// ---- pending-owner (2-step transfer) ----

func getPendingOwner() string {
	v := sdk.StateGetObject("pending_owner")
	if v == nil {
		return ""
	}
	return *v
}

func setPendingOwner(addr string) {
	sdk.StateSetObject("pending_owner", addr)
}

func clearPendingOwner() {
	sdk.StateDeleteObject("pending_owner")
}
```

- [ ] **Step 4: Rewrite `ChangeOwner` + add the 3 new entrypoints in `contract/market.go`**

Replace the entire existing `ChangeOwner` function (the `//go:wasmexport changeOwner` block) with:
```go
//go:wasmexport changeOwner
func ChangeOwner(payload *string) *string {
	assertInit()

	owner, isOwner := getOwner()
	if !isOwner {
		sdk.Abort("Only owner can change owner")
	}

	if payload == nil || *payload == "" {
		sdk.Abort("Payload required")
	}

	var p ChangeOwnerPayload
	r := jlexer.Lexer{Data: []byte(*payload)}
	p.UnmarshalTinyJSON(&r)
	if r.Error() != nil {
		sdk.Abort("Invalid payload")
	}

	if p.NewOwner == "" {
		sdk.Abort("New owner address required")
	}

	// 2-step: propose only. Ownership does not move until the proposed
	// owner calls acceptOwnership. Re-calling overwrites the candidate.
	setPendingOwner(p.NewOwner)
	emitOwnerTransferInitiated(owner, p.NewOwner)
	return jsonResponse(&SuccessResponse{Success: true})
}

//go:wasmexport acceptOwnership
func AcceptOwnership(payload *string) *string {
	assertInit()

	pending := getPendingOwner()
	if pending == "" {
		sdk.Abort("No pending ownership transfer")
	}

	caller := getCaller()
	if caller != pending {
		sdk.Abort("Not the pending owner")
	}

	previous, _ := getOwner()
	sdk.StateSetObject("owner", pending)
	clearPendingOwner()
	emitOwnerChange(previous, pending)
	return jsonResponse(&SuccessResponse{Success: true})
}

//go:wasmexport cancelOwnershipTransfer
func CancelOwnershipTransfer(payload *string) *string {
	assertInit()

	_, isOwner := getOwner()
	if !isOwner {
		sdk.Abort("Only owner can cancel ownership transfer")
	}

	if getPendingOwner() == "" {
		sdk.Abort("No pending ownership transfer")
	}

	caller := getCaller()
	clearPendingOwner()
	emitOwnerTransferCancelled(caller)
	return jsonResponse(&SuccessResponse{Success: true})
}

//go:wasmexport getPendingOwner
func GetPendingOwner(payload *string) *string {
	assertInit()
	return jsonResponse(&PendingOwnerResponse{PendingOwner: getPendingOwner()})
}
```

- [ ] **Step 5: Add emit functions to `contract/events.go`**

After `emitOwnerChange`, add (mirroring its structure exactly):
```go
func emitOwnerTransferInitiated(currentOwner, pendingOwner string) {
	txID := sdk.GetEnvKey("tx.id")
	event := OwnerTransferInitiatedEvent{
		Type:       "ownerTransferInitiated",
		Attributes: OwnerTransferInitiatedAttributes{CurrentOwner: currentOwner, PendingOwner: pendingOwner},
		Tx:         *txID,
	}
	w := jwriter.Writer{}
	event.MarshalTinyJSON(&w)
	sdk.Log(string(w.Buffer.BuildBytes()))
}

func emitOwnerTransferCancelled(by string) {
	txID := sdk.GetEnvKey("tx.id")
	event := OwnerTransferCancelledEvent{
		Type:       "ownerTransferCancelled",
		Attributes: OwnerTransferCancelledAttributes{By: by},
		Tx:         *txID,
	}
	w := jwriter.Writer{}
	event.MarshalTinyJSON(&w)
	sdk.Log(string(w.Buffer.BuildBytes()))
}
```

- [ ] **Step 6: Build green**

Run:
```bash
cd /home/dockeruser/magi/magi-market
GOTOOLCHAIN=go1.25.3 tinygo build -gc=custom -scheduler=none -panic=trap -no-debug -target=wasm-unknown -o test/artifacts/main.wasm ./contract && echo BUILD_OK
```
Expected: `BUILD_OK`. If a hand-written marshaler is malformed, tinygo fails here — fix it to match the in-file pattern; never run tinyjson.

- [ ] **Step 7: Write the new 2-step transfer tests**

Create `test/governance_test.go`. First inspect `test/helpers_test.go` for the real helper names (`CallMarket`, `InitFullSetup`, `SetupContractTest`, `AssertEventEmitted`, `ParseOwner`). Add a `ParsePendingOwner` helper if none exists (mirror `ParseOwner`, reading the `pendingOwner` field). Tests:
```go
package contract_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTwoStepTransferProposeDoesNotMoveOwner(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	_, _, logs := CallMarket(t, ct, "changeOwner", []byte(`{"newOwner":"hive:newowner"}`), nil, ownerAddress, "", true, gas, "")
	AssertEventEmitted(t, logs, "ownerTransferInitiated")

	res, _, _ := CallMarket(t, ct, "getOwner", nil, nil, "hive:anyone", "", true, gas, "")
	assert.Equal(t, ownerAddress, ParseOwner(res), "owner unchanged until accepted")

	pend, _, _ := CallMarket(t, ct, "getPendingOwner", nil, nil, "hive:anyone", "", true, gas, "")
	assert.Equal(t, "hive:newowner", ParsePendingOwner(pend))
}

func TestTwoStepTransferAcceptByNonPendingRejected(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	CallMarket(t, ct, "changeOwner", []byte(`{"newOwner":"hive:newowner"}`), nil, ownerAddress, "", true, gas, "")
	CallMarket(t, ct, "acceptOwnership", nil, nil, "hive:someoneelse", "", false, gas, "Not the pending owner")
}

func TestTwoStepTransferAcceptFinalizes(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	CallMarket(t, ct, "changeOwner", []byte(`{"newOwner":"hive:newowner"}`), nil, ownerAddress, "", true, gas, "")
	_, _, logs := CallMarket(t, ct, "acceptOwnership", nil, nil, "hive:newowner", "", true, gas, "")
	AssertEventEmitted(t, logs, "ownerChange")

	res, _, _ := CallMarket(t, ct, "getOwner", nil, nil, "hive:anyone", "", true, gas, "")
	assert.Equal(t, "hive:newowner", ParseOwner(res))

	pend, _, _ := CallMarket(t, ct, "getPendingOwner", nil, nil, "hive:anyone", "", true, gas, "")
	assert.Equal(t, "", ParsePendingOwner(pend), "pending cleared after accept")

	// new owner can admin; old owner cannot
	CallMarket(t, ct, "setFee", []byte(`{"feeBps":500}`), nil, "hive:newowner", "", true, gas, "")
	CallMarket(t, ct, "setFee", []byte(`{"feeBps":500}`), nil, ownerAddress, "", false, gas, "Only owner can set fee")
}

func TestTwoStepTransferAcceptNoPendingRejected(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	CallMarket(t, ct, "acceptOwnership", nil, nil, "hive:newowner", "", false, gas, "No pending ownership transfer")
}

func TestTwoStepTransferCancel(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	CallMarket(t, ct, "changeOwner", []byte(`{"newOwner":"hive:newowner"}`), nil, ownerAddress, "", true, gas, "")
	_, _, logs := CallMarket(t, ct, "cancelOwnershipTransfer", nil, nil, ownerAddress, "", true, gas, "")
	AssertEventEmitted(t, logs, "ownerTransferCancelled")
	pend, _, _ := CallMarket(t, ct, "getPendingOwner", nil, nil, "hive:anyone", "", true, gas, "")
	assert.Equal(t, "", ParsePendingOwner(pend))
	CallMarket(t, ct, "acceptOwnership", nil, nil, "hive:newowner", "", false, gas, "No pending ownership transfer")
}

func TestTwoStepTransferWorksWhilePaused(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)
	CallMarket(t, ct, "pause", nil, nil, ownerAddress, "", true, gas, "")
	CallMarket(t, ct, "changeOwner", []byte(`{"newOwner":"hive:newowner"}`), nil, ownerAddress, "", true, gas, "")
	CallMarket(t, ct, "acceptOwnership", nil, nil, "hive:newowner", "", true, gas, "")
	res, _, _ := CallMarket(t, ct, "getOwner", nil, nil, "hive:anyone", "", true, gas, "")
	assert.Equal(t, "hive:newowner", ParseOwner(res))
}
```
If `ParsePendingOwner` must be added, put it in `test/governance_test.go` mirroring `ParseOwner`'s implementation (same result type, reading JSON field `pendingOwner`).

- [ ] **Step 8: Update the two existing tests that assumed immediate transfer**

In `test/basic_test.go`, replace the body of `TestChangeOwner` with the 2-step flow:
```go
func TestChangeOwner(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	_, _, logs := CallMarket(t, ct, "changeOwner", []byte(`{"newOwner":"hive:newowner"}`), nil, ownerAddress, "", true, gas, "")
	AssertEventEmitted(t, logs, "ownerTransferInitiated")

	_, _, logs2 := CallMarket(t, ct, "acceptOwnership", nil, nil, "hive:newowner", "", true, gas, "")
	AssertEventEmitted(t, logs2, "ownerChange")

	result, _, _ := CallMarket(t, ct, "getOwner", nil, nil, "hive:anyone", "", true, gas, "")
	assert.Equal(t, "hive:newowner", ParseOwner(result))
}
```
(`TestChangeOwnerNonOwner` and `TestChangeOwnerEmpty` are unchanged — non-owner and empty-newOwner are still rejected by the rewritten `changeOwner`.)

In `test/edge_cases_test.go`, replace the body of `TestChangeOwnerThenAdminActions` with:
```go
func TestChangeOwnerThenAdminActions(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	newOwner := "hive:newowner"
	CallMarket(t, ct, "changeOwner", []byte(fmt.Sprintf(`{"newOwner":"%s"}`, newOwner)), nil, ownerAddress, "", true, gas, "")
	// Until accepted, the OLD owner still administers.
	CallMarket(t, ct, "setFee", []byte(`{"feeBps":500}`), nil, ownerAddress, "", true, gas, "")
	CallMarket(t, ct, "acceptOwnership", nil, nil, newOwner, "", true, gas, "")

	// Old owner can no longer admin
	CallMarket(t, ct, "setFee", []byte(`{"feeBps":500}`), nil, ownerAddress, "", false, gas, "Only owner can set fee")
	CallMarket(t, ct, "pause", nil, nil, ownerAddress, "", false, gas, "Only owner can pause")

	// New owner can admin
	CallMarket(t, ct, "setFee", []byte(`{"feeBps":500}`), nil, newOwner, "", true, gas, "")
	CallMarket(t, ct, "pause", nil, nil, newOwner, "", true, gas, "")
}
```
(The "admin works when paused" test in `edge_cases_test.go` that calls `changeOwner` expecting success needs NO change — `changeOwner` still returns success as a propose; it does not re-read the owner.)

- [ ] **Step 9: Run targeted then full suite**

Run:
```bash
cd /home/dockeruser/magi/magi-market
GOTOOLCHAIN=go1.25.3 go test ./test/ -count=1 -run 'TwoStep|ChangeOwner' -v 2>&1 | tail -25
GOTOOLCHAIN=go1.25.3 go test ./test/ -count=1 2>&1 | tail -3
```
Expected: the 2-step + ChangeOwner tests PASS; full suite `ok` (252 prior + 6 new = 258, 0 fail). If any non-owner-related test regressed, STOP and fix before committing.

- [ ] **Step 10: Commit**

```bash
cd /home/dockeruser/magi/magi-market
git branch --show-current
git add contract/ test/governance_test.go test/basic_test.go test/edge_cases_test.go
git commit -m "feat: 2-step owner transfer

changeOwner now proposes a pending_owner; ownership only moves when the
proposed owner calls acceptOwnership. Adds cancelOwnershipTransfer and
getPendingOwner. Works while paused. Existing immediate-transfer tests
updated to the 2-step flow."
git rev-parse feature/governance-safety   # == HEAD
```

---

## Task 2: Collection denylist — state, admin entrypoints, queries, events (no enforcement yet)

**Why separate from enforcement:** this task is purely additive (new state + entrypoints), so the full suite stays green and the enforcement diff in Task 3 is reviewable in isolation.

**Files:**
- Modify: `contract/types.go` (add `CollectionPayload`, `CollectionDeniedResponse`, denied/allowed event structs)
- Modify: `contract/types_tinyjson.go` (hand-add marshalers)
- Modify: `contract/internal.go` (`isCollectionDenied`, `assertCollectionAllowed`, deny/allow state setters)
- Modify: `contract/market.go` (`DenyCollection`, `AllowCollection`, `IsCollectionDenied`)
- Modify: `contract/events.go` (`emitCollectionDenied`, `emitCollectionAllowed`)
- Modify: `test/governance_test.go` (denylist admin/query tests)

- [ ] **Step 1: Add structs to `contract/types.go`**

```go
type CollectionPayload struct {
	NftContract string `json:"nftContract"`
}

type CollectionDeniedResponse struct {
	Denied bool `json:"denied"`
}

type CollectionDeniedEvent struct {
	Type       string                     `json:"type"`
	Attributes CollectionDeniedAttributes `json:"attributes"`
	Tx         string                     `json:"tx"`
}

type CollectionDeniedAttributes struct {
	NftContract string `json:"nftContract"`
	By          string `json:"by"`
}

type CollectionAllowedEvent struct {
	Type       string                      `json:"type"`
	Attributes CollectionAllowedAttributes `json:"attributes"`
	Tx         string                      `json:"tx"`
}

type CollectionAllowedAttributes struct {
	NftContract string `json:"nftContract"`
	By          string `json:"by"`
}
```

- [ ] **Step 2: Hand-add tinyjson at the END of `contract/types_tinyjson.go`**

```go
// ---- hand-added (sub-project A): denylist structs ----

func tinyjsonGovDecodeCollectionPayload(in *jlexer.Lexer, out *CollectionPayload) {
	isTopLevel := in.IsStart()
	if in.IsNull() {
		if isTopLevel {
			in.Consumed()
		}
		in.Skip()
		return
	}
	in.Delim('{')
	for !in.IsDelim('}') {
		key := in.UnsafeFieldName(false)
		in.WantColon()
		if in.IsNull() {
			in.Skip()
			in.WantComma()
			continue
		}
		switch key {
		case "nftContract":
			out.NftContract = string(in.String())
		default:
			in.SkipRecursive()
		}
		in.WantComma()
	}
	in.Delim('}')
	if isTopLevel {
		in.Consumed()
	}
}

func (v *CollectionPayload) UnmarshalTinyJSON(l *jlexer.Lexer) {
	tinyjsonGovDecodeCollectionPayload(l, v)
}

func tinyjsonGovEncodeCollectionDeniedResponse(out *jwriter.Writer, in CollectionDeniedResponse) {
	out.RawByte('{')
	{
		const prefix string = ",\"denied\":"
		out.RawString(prefix[1:])
		out.Bool(bool(in.Denied))
	}
	out.RawByte('}')
}

func (v CollectionDeniedResponse) MarshalTinyJSON(w *jwriter.Writer) {
	tinyjsonGovEncodeCollectionDeniedResponse(w, v)
}

func tinyjsonGovEncodeCollectionDenied(out *jwriter.Writer, in CollectionDeniedEvent) {
	out.RawByte('{')
	{
		const prefix string = ",\"type\":"
		out.RawString(prefix[1:])
		out.String(string(in.Type))
	}
	{
		const prefix string = ",\"attributes\":"
		out.RawString(prefix)
		out.RawByte('{')
		{
			const p2 string = ",\"nftContract\":"
			out.RawString(p2[1:])
			out.String(string(in.Attributes.NftContract))
		}
		{
			const p2 string = ",\"by\":"
			out.RawString(p2)
			out.String(string(in.Attributes.By))
		}
		out.RawByte('}')
	}
	{
		const prefix string = ",\"tx\":"
		out.RawString(prefix)
		out.String(string(in.Tx))
	}
	out.RawByte('}')
}

func (v CollectionDeniedEvent) MarshalTinyJSON(w *jwriter.Writer) {
	tinyjsonGovEncodeCollectionDenied(w, v)
}

func tinyjsonGovEncodeCollectionAllowed(out *jwriter.Writer, in CollectionAllowedEvent) {
	out.RawByte('{')
	{
		const prefix string = ",\"type\":"
		out.RawString(prefix[1:])
		out.String(string(in.Type))
	}
	{
		const prefix string = ",\"attributes\":"
		out.RawString(prefix)
		out.RawByte('{')
		{
			const p2 string = ",\"nftContract\":"
			out.RawString(p2[1:])
			out.String(string(in.Attributes.NftContract))
		}
		{
			const p2 string = ",\"by\":"
			out.RawString(p2)
			out.String(string(in.Attributes.By))
		}
		out.RawByte('}')
	}
	{
		const prefix string = ",\"tx\":"
		out.RawString(prefix)
		out.String(string(in.Tx))
	}
	out.RawByte('}')
}

func (v CollectionAllowedEvent) MarshalTinyJSON(w *jwriter.Writer) {
	tinyjsonGovEncodeCollectionAllowed(w, v)
}
```

- [ ] **Step 3: Add denylist helpers to `contract/internal.go`**

```go
// ---- collection denylist ----

func denylistKey(nftContract string) string {
	return "dl|" + nftContract
}

func isCollectionDenied(nftContract string) bool {
	v := sdk.StateGetObject(denylistKey(nftContract))
	return v != nil && *v == "1"
}

func setCollectionDenied(nftContract string) {
	sdk.StateSetObject(denylistKey(nftContract), "1")
}

func clearCollectionDenied(nftContract string) {
	sdk.StateDeleteObject(denylistKey(nftContract))
}

// assertCollectionAllowed aborts if the collection is on the denylist.
func assertCollectionAllowed(nftContract string) {
	if isCollectionDenied(nftContract) {
		sdk.Abort("Collection is denied")
	}
}
```

- [ ] **Step 4: Add admin + query entrypoints to `contract/market.go`**

```go
//go:wasmexport denyCollection
func DenyCollection(payload *string) *string {
	assertInit()

	_, isOwner := getOwner()
	if !isOwner {
		sdk.Abort("Only owner can deny collection")
	}

	if payload == nil || *payload == "" {
		sdk.Abort("Payload required")
	}
	var p CollectionPayload
	r := jlexer.Lexer{Data: []byte(*payload)}
	p.UnmarshalTinyJSON(&r)
	if r.Error() != nil {
		sdk.Abort("Invalid payload")
	}
	if p.NftContract == "" {
		sdk.Abort("NFT contract required")
	}

	setCollectionDenied(p.NftContract)
	emitCollectionDenied(p.NftContract, getCaller())
	return jsonResponse(&SuccessResponse{Success: true})
}

//go:wasmexport allowCollection
func AllowCollection(payload *string) *string {
	assertInit()

	_, isOwner := getOwner()
	if !isOwner {
		sdk.Abort("Only owner can allow collection")
	}

	if payload == nil || *payload == "" {
		sdk.Abort("Payload required")
	}
	var p CollectionPayload
	r := jlexer.Lexer{Data: []byte(*payload)}
	p.UnmarshalTinyJSON(&r)
	if r.Error() != nil {
		sdk.Abort("Invalid payload")
	}
	if p.NftContract == "" {
		sdk.Abort("NFT contract required")
	}

	clearCollectionDenied(p.NftContract)
	emitCollectionAllowed(p.NftContract, getCaller())
	return jsonResponse(&SuccessResponse{Success: true})
}

//go:wasmexport isCollectionDenied
func IsCollectionDenied(payload *string) *string {
	assertInit()

	if payload == nil || *payload == "" {
		sdk.Abort("Payload required")
	}
	var p CollectionPayload
	r := jlexer.Lexer{Data: []byte(*payload)}
	p.UnmarshalTinyJSON(&r)
	if r.Error() != nil {
		sdk.Abort("Invalid payload")
	}

	return jsonResponse(&CollectionDeniedResponse{Denied: isCollectionDenied(p.NftContract)})
}
```

- [ ] **Step 5: Add emit functions to `contract/events.go`**

```go
func emitCollectionDenied(nftContract, by string) {
	txID := sdk.GetEnvKey("tx.id")
	event := CollectionDeniedEvent{
		Type:       "collectionDenied",
		Attributes: CollectionDeniedAttributes{NftContract: nftContract, By: by},
		Tx:         *txID,
	}
	w := jwriter.Writer{}
	event.MarshalTinyJSON(&w)
	sdk.Log(string(w.Buffer.BuildBytes()))
}

func emitCollectionAllowed(nftContract, by string) {
	txID := sdk.GetEnvKey("tx.id")
	event := CollectionAllowedEvent{
		Type:       "collectionAllowed",
		Attributes: CollectionAllowedAttributes{NftContract: nftContract, By: by},
		Tx:         *txID,
	}
	w := jwriter.Writer{}
	event.MarshalTinyJSON(&w)
	sdk.Log(string(w.Buffer.BuildBytes()))
}
```

- [ ] **Step 6: Build + add admin/query tests + run**

Build (`tinygo … && echo BUILD_OK`). Append to `test/governance_test.go`:
```go
func TestDenyAllowCollectionAdmin(t *testing.T) {
	ct := SetupContractTest()
	InitFullSetup(t, ct)

	q := `{"nftContract":"` + NftContractID + `"}`
	res, _, _ := CallMarket(t, ct, "isCollectionDenied", []byte(q), nil, "hive:anyone", "", true, gas, "")
	assert.False(t, ParseCollectionDenied(res), "default allowed")

	CallMarket(t, ct, "denyCollection", []byte(q), nil, "hive:alice", "", false, gas, "Only owner can deny collection")
	_, _, logs := CallMarket(t, ct, "denyCollection", []byte(q), nil, ownerAddress, "", true, gas, "")
	AssertEventEmitted(t, logs, "collectionDenied")

	res2, _, _ := CallMarket(t, ct, "isCollectionDenied", []byte(q), nil, "hive:anyone", "", true, gas, "")
	assert.True(t, ParseCollectionDenied(res2))

	CallMarket(t, ct, "allowCollection", []byte(q), nil, "hive:alice", "", false, gas, "Only owner can allow collection")
	_, _, logs2 := CallMarket(t, ct, "allowCollection", []byte(q), nil, ownerAddress, "", true, gas, "")
	AssertEventEmitted(t, logs2, "collectionAllowed")

	res3, _, _ := CallMarket(t, ct, "isCollectionDenied", []byte(q), nil, "hive:anyone", "", true, gas, "")
	assert.False(t, ParseCollectionDenied(res3))
}
```
Add a `ParseCollectionDenied` helper in `test/governance_test.go` mirroring how other bool responses are parsed (read the `denied` JSON field; pattern in `test/helpers_test.go` for e.g. `IsPaused`/`ParsePaused`). Run `-run 'DenyAllow' -v` (PASS) then the full suite (`ok`, 252 + Task-1's 6 + 1 = 259).

- [ ] **Step 7: Commit**

```bash
cd /home/dockeruser/magi/magi-market
git branch --show-current
git add contract/ test/governance_test.go
git commit -m "feat: collection denylist state + admin entrypoints (no enforcement yet)

denyCollection/allowCollection (owner-only) + isCollectionDenied query
+ events. Additive only; enforcement wired in the next task."
git rev-parse feature/governance-safety
```

---

## Task 3: Wire denylist enforcement (retroactive) + deny-aware SettleAuction

**Files:**
- Modify: `contract/market.go` (`assertCollectionAllowed` in `doList`, `doBuy`, `MakeOffer`, `doAcceptOffer`)
- Modify: `contract/auction.go` (`assertCollectionAllowed` in `CreateAuction`, `PlaceBid`; deny-aware unwind in `SettleAuction`)
- Modify: `test/governance_test.go` (retroactive + recovery + settle-on-denied tests)

- [ ] **Step 1: Insert creation-path guards in `contract/market.go`**

- In `doList`, immediately after the existing soulbound check (`if nftIsSoulbound(p.NftContract, p.TokenId) { sdk.Abort("Cannot list soulbound tokens") }`), add:
  ```go
	assertCollectionAllowed(p.NftContract)
  ```
- In `MakeOffer`, immediately after its payload is parsed and `p.NftContract`/required-field validation passes (just before the min-offer / escrow logic), add:
  ```go
	assertCollectionAllowed(p.NftContract)
  ```

- [ ] **Step 2: Insert completion-path guards in `contract/market.go`**

- In `doBuy`, after `nftContract := getListingField(p.ListingId, "nc")` is read (before the `escrowIn` call), add:
  ```go
	assertCollectionAllowed(nftContract)
  ```
- In `doAcceptOffer`, after `nftContract := getOfferField(offerId, "nc")` is read (before the approval/balance preflight), add:
  ```go
	assertCollectionAllowed(nftContract)
  ```
  (covers both `acceptOffer` and `acceptCollectionOffer`, which both call `doAcceptOffer`.)

- [ ] **Step 3: Insert auction guards in `contract/auction.go`**

- In `CreateAuction`, after its NFT/soulbound validation and before the NFT is escrowed, add `assertCollectionAllowed(p.NftContract)`.
- In `PlaceBid`, after the auction's `nc` is read (`nftContract := getAuctionField(p.AuctionId, "nc")` — add this read if not already present in scope) and before any escrow, add `assertCollectionAllowed(nftContract)`.

- [ ] **Step 4: Make `SettleAuction` deny-aware in `contract/auction.go`**

In `SettleAuction`, after the existing reads (`seller`, `nftContract`, `tokenId`, `amount`, `paymentToken`, `highBidder`, `highBid`, `contractAddr`) and before the `if highBidder == "" || mIsZero(highBid)` branch, insert a denial short-circuit that unwinds to seller + refunds bidder rather than completing a denied trade or trapping escrow:
```go
	if isCollectionDenied(nftContract) {
		// Collection denied mid-auction: treat as no-sale. Return the
		// escrowed NFT to the seller and refund the high bidder (if any).
		nftSafeTransferFrom(nftContract, contractAddr, seller, tokenId, amount)
		setAuctionField(p.AuctionId, "stl", "1")
		setAuctionField(p.AuctionId, "act", "0")
		if highBidder != "" && !mIsZero(highBid) {
			tokenTransferBig(paymentToken, highBidder, highBid)
		}
		emitAuctionSettled(p.AuctionId, "", "0", "0", "0")
		return jsonResponse(&SuccessResponse{Success: true})
	}
```
(The existing no-bid / winner branches below remain unchanged for non-denied collections.)

- [ ] **Step 5: Build green**

`cd /home/dockeruser/magi/magi-market && GOTOOLCHAIN=go1.25.3 tinygo build -gc=custom -scheduler=none -panic=trap -no-debug -target=wasm-unknown -o test/artifacts/main.wasm ./contract && echo BUILD_OK`

- [ ] **Step 6: Add retroactive / recovery / settle-on-denied tests**

Append to `test/governance_test.go`. Use the existing harness helpers for minting/approving/listing/offering/auctioning (inspect `test/helpers_test.go` and an existing `listing_test.go`/`auction_test.go` for the exact helper names — e.g. `MintNft`, `ApproveNftForMarket`, `MintAndApproveToken`, `CallMarket`, `ParseListing`, `QueryNftBalance`). Required scenarios (write each as its own `func Test...`):
- **Creation blocked when denied:** deny `NftContractID`; `list` / `createAuction` / `makeOffer` for that collection each abort with `"Collection is denied"`.
- **Retroactive completion block:** create a listing while allowed; then `denyCollection`; a `buy` of that listing aborts `"Collection is denied"`. Same for an active auction `placeBid` and an active offer `acceptOffer`.
- **Recovery still works while denied:** with a denied collection, the seller can `delist` an active listing (succeeds; NFT balance of seller unchanged since approval-custody), the buyer can `cancelOffer` an active offer (succeeds; escrow refunded), the seller can `cancelAuction` a no-bid auction (succeeds; escrowed NFT returned).
- **SettleAuction on denied+ended auction:** create+bid an English auction while allowed; deny the collection; advance past `endBlock`; `settleAuction` succeeds, NFT returns to seller (market NFT balance 0, seller balance restored), high bidder refunded their escrowed bid, auction marked settled — winner does NOT receive the NFT and seller is NOT paid.
- **Allow re-enables:** after `allowCollection`, a fresh `list` + `buy` for that collection succeeds again.

Each assertion must check real on-ledger balances / abort messages / events (no vacuous asserts).

- [ ] **Step 7: Run targeted then full suite**

```bash
cd /home/dockeruser/magi/magi-market
GOTOOLCHAIN=go1.25.3 go test ./test/ -count=1 -run 'Deny|Collection|Governance|TwoStep' -v 2>&1 | tail -30
GOTOOLCHAIN=go1.25.3 go test ./test/ -count=1 2>&1 | tail -3
```
Expected: all new governance tests PASS; full suite `ok` with **0 FAIL** (252 prior + all new governance tests). The prior 252 stay green because they use a never-denied collection and the guards are no-ops unless `dl|<c>` is set. If any prior test regressed, STOP and fix.

- [ ] **Step 8: Commit**

```bash
cd /home/dockeruser/magi/magi-market
git branch --show-current
git add contract/ test/governance_test.go
git commit -m "feat: enforce collection denylist (retroactive) + deny-aware settle

assertCollectionAllowed gates create (list/createAuction/makeOffer) and
complete (buy/placeBid/acceptOffer) paths; recovery paths (delist/
cancel*/emergencyWithdraw) stay ungated so denied collections never
trap NFT or escrow. SettleAuction on a denied collection unwinds to
seller + refunds bidder instead of completing the trade."
git rev-parse feature/governance-safety
```

---

## Self-Review (performed during planning)

**Spec coverage:**
- 2-step owner transfer (changeOwner→pending, acceptOwnership, cancel, getPendingOwner, works-while-paused, getOwner reports current) → Task 1. ✓
- Retroactive denylist (default-open, deny/allow/isCollectionDenied, creation+completion enforcement, recovery ungated, deny-aware SettleAuction) → Tasks 2 (state/admin) + 3 (enforcement). ✓
- Read/UX surface (queries + dedicated events + explicit cancel) → Tasks 1 & 2. ✓
- Existing immediate-transfer tests updated → Task 1 Step 8. ✓
- tinyjson hand-maintained (no generator) → Tasks 1 & 2 Step 2. ✓
- No new cross-contract coupling → confirmed (only local state + existing helpers). ✓
- Dropped scope (sweeper, timelock, allowlist) → absent. ✓

**Placeholder scan:** No TBD/TODO; every code step shows complete code; test steps that reference harness helpers explicitly instruct inspecting `test/helpers_test.go` for exact names and give the full test bodies where the surface is new (Task 1). Task 3 Step 6 enumerates each scenario as its own test with concrete assertions to write against the real helpers (the listing/offer/auction setup helpers already exist and vary; the plan names them and points at existing test files as the pattern source rather than guessing signatures).

**Type consistency:** `getPendingOwner`/`setPendingOwner`/`clearPendingOwner`, `isCollectionDenied`/`setCollectionDenied`/`clearCollectionDenied`/`assertCollectionAllowed`, `denylistKey`, structs (`PendingOwnerResponse`, `CollectionPayload`, `CollectionDeniedResponse`, the 4 event/attr pairs), emit fns, and tinyjson helper names are defined once and used consistently across tasks. `CollectionPayload` is used for deny/allow/isCollectionDenied input; `CollectionDeniedResponse` for the query output. Events only get `MarshalTinyJSON` (events.go only marshals); input structs only `UnmarshalTinyJSON`; the query response gets both (built via `jsonResponse`, parsed by tests).

**Sequencing note:** Task 2 is additive (suite stays green); Task 3 adds guards that are no-ops for never-denied collections, so the prior 252 stay green throughout. The only intentional behavior change to existing tests is the 2-step flow (Task 1 Step 8), explicitly handled.

---

## Execution Handoff

After saving, offer Subagent-Driven (recommended) vs Inline execution.
