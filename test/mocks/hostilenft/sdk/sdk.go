// Minimal SDK for the caller mock. This package is ONLY ever compiled by
// tinygo for the test harness (never host-compiled), so there is no
// host-stub split. The //go:wasmimport decls below are always linked.
//
// The wasmimport signatures mirror magi-market's sdk/runtime_imports.go
// exactly (string-pointer in/out, env.abort with msg/file/line/column).

package sdk

// Blank-import the local custom-GC runtime so the `-gc=custom` tinygo build
// links its allocator (same pattern as magi-market sdk/runtime_imports.go).
import (
	_ "hostilenft/runtime"
)

//go:wasmimport sdk db.set_object
func stateSetObject(key *string, value *string) *string

//go:wasmimport sdk db.get_object
func stateGetObject(key *string) *string

//go:wasmimport sdk db.rm_object
func stateDeleteObject(key *string) *string

//go:wasmimport sdk system.get_env_key
func getEnvKey(arg *string) *string

//go:wasmimport env abort
func abort(msg, file *string, line, column *int32)

// StateSetObject sets a value by key in the contract state.
func StateSetObject(key string, value string) {
	stateSetObject(&key, &value)
}

// StateGetObject gets a value by key from the contract state (nil if unset).
func StateGetObject(key string) *string {
	return stateGetObject(&key)
}

// StateDeleteObject removes a value by key from the contract state.
func StateDeleteObject(key string) {
	stateDeleteObject(&key)
}

// GetEnvKey returns an execution-environment value by key (e.g. msg.caller).
func GetEnvKey(key string) *string {
	return getEnvKey(&key)
}

// Abort aborts the contract execution with msg.
func Abort(msg string) {
	ln := int32(0)
	abort(&msg, nil, &ln, &ln)
	panic(msg)
}

//go:wasmimport sdk contracts.read
func contractRead(contractId *string, key *string) *string

// ContractStateGet reads another contract's state. The hostile mock uses it to
// look at the market's bucket counter from inside a delivery call, which is how
// the CEI ordering is observed.
func ContractStateGet(contractId string, key string) *string {
	return contractRead(&contractId, &key)
}

//go:wasmimport sdk contracts.call
func contractCall(contractId *string, method *string, payload *string, options *string) *string

// ContractCallSimple mirrors magi-market's own helper: call another contract
// with no options. This mock exists to prove the market REFUSES such a call.
func ContractCallSimple(contractId string, method string, payload string) *string {
	optStr := ""
	return contractCall(&contractId, &method, &payload, &optStr)
}
