//go:build !gc.custom

// Host-build stubs so tooling that imports this package (e.g. the tinyjson
// generator bootstrap) compiles without the wasm runtime. Never executed:
// real implementations are the //go:wasmimport decls in runtime_imports.go,
// compiled only under the tinygo `gc.custom` build tag.

package sdk

func log(s *string) *string { return nil }

func stateSetObject(key *string, value *string) *string { return nil }

func stateGetObject(key *string) *string { return nil }

func stateDeleteObject(key *string) *string { return nil }

func getEnv(arg *string) *string { return nil }

func getEnvKey(arg *string) *string { return nil }

func getBalance(arg1 *string, arg2 *string) *string { return nil }

func hiveDraw(arg1 *string, arg2 *string) *string { return nil }

func hiveTransfer(arg1 *string, arg2 *string, arg3 *string) *string { return nil }

func hiveWithdraw(arg1 *string, arg2 *string, arg3 *string) *string { return nil }

func contractRead(contractId *string, key *string) *string { return nil }

func contractCall(contractId *string, method *string, payload *string, options *string) *string {
	return nil
}

func tssCreateKey(keyId *string, algo *string) *string { return nil }

func tssSignKey(keyId *string, msgId *string) *string { return nil }

func tssGetKey(keyId *string) *string { return nil }

func abort(msg, file *string, line, column *int32) {}

func revert(msg, symbol *string) {}
