package sdk

import (
	"encoding/hex"
	"strconv"

	"github.com/CosmWasm/tinyjson"
)

// Low-level host bindings (`log`, `stateSetObject`, `contractCall`, `abort`,
// ...) live in runtime_imports.go (//go:build gc.custom, real wasmimports)
// and sdk_stub.go (//go:build !gc.custom, host build stubs). This file holds
// the build-tag-agnostic wrappers.

func Log(s string) {
	log(&s)
}

// Aborts the contract execution
func Abort(msg string) {
	ln := int32(0)
	abort(&msg, nil, &ln, &ln)
	panic(msg)
}

// Reverts the transaction and abort execution in the same way as Abort().
func Revert(msg string, symbol string) {
	revert(&msg, &symbol)
}

// Set a value by key in the contract state
func StateSetObject(key string, value string) {
	stateSetObject(&key, &value)
}

// Get a value by key from the contract state
func StateGetObject(key string) *string {
	return stateGetObject(&key)
}

// Delete or unset a value by key in the contract state
func StateDeleteObject(key string) {
	stateDeleteObject(&key)
}

// Get current execution environment variables
func GetEnv() Env {
	envStr := *getEnv(nil)
	env := Env{}
	tinyjson.Unmarshal([]byte(envStr), &env)
	env2 := Env2{}
	tinyjson.Unmarshal([]byte(envStr), &env2)

	requiredAuths := make([]Address, 0)
	for _, addr := range env2.Auths {
		requiredAuths = append(requiredAuths, Address(addr))
	}
	requiredPostingAuths := make([]Address, 0)
	for _, addr := range env2.PostingAuths {
		requiredPostingAuths = append(requiredPostingAuths, Address(addr))
	}

	env.Sender = Sender{
		Address:              Address(env2.Sender),
		RequiredAuths:        requiredAuths,
		RequiredPostingAuths: requiredPostingAuths,
	}

	return env
}

// Get current execution environment variables as json string
func GetEnvStr() string {
	return *getEnv(nil)
}

// Get current execution environment variable by a key
func GetEnvKey(key string) *string {
	return getEnvKey(&key)
}

// Get balance of an account
func GetBalance(address Address, asset Asset) int64 {
	addr := address.String()
	as := asset.String()
	balStr := *getBalance(&addr, &as)
	bal, err := strconv.ParseInt(balStr, 10, 64)
	if err != nil {
		panic(err)
	}
	return bal
}

// Transfer assets from caller account to the contract up to the limit specified in `intents`. The transaction must be signed using active authority for Hive accounts.
func HiveDraw(amount int64, asset Asset) {
	amt := strconv.FormatInt(amount, 10)
	as := asset.String()
	hiveDraw(&amt, &as)
}

// Transfer assets from the contract to another account.
func HiveTransfer(to Address, amount int64, asset Asset) {
	toaddr := to.String()
	amt := strconv.FormatInt(amount, 10)
	as := asset.String()
	hiveTransfer(&toaddr, &amt, &as)
}

// Unmap assets from the contract to a specified Hive account.
func HiveWithdraw(to Address, amount int64, asset Asset) {
	toaddr := to.String()
	amt := strconv.FormatInt(amount, 10)
	as := asset.String()
	hiveWithdraw(&toaddr, &amt, &as)
}

// Get a value by key from the contract state of another contract
func ContractStateGet(contractId string, key string) *string {
	return contractRead(&contractId, &key)
}

// Call another contract
func ContractCall(contractId string, method string, payload string, options *ContractCallOptions) *string {
	optStr := ""
	if options != nil {
		optByte, err := tinyjson.Marshal(options)
		if err != nil {
			Revert("could not serialize options", "sdk_error")
		}
		optStr = string(optByte)
	}
	return contractCall(&contractId, &method, &payload, &optStr)
}

// ContractCallSimple calls another contract without options.
// This avoids pulling in tinyjson.Marshal and its WASI dependencies.
func ContractCallSimple(contractId string, method string, payload string) *string {
	optStr := ""
	return contractCall(&contractId, &method, &payload, &optStr)
}

// Request a TSS key to be created. Algo must be either ecdsa or eddsa.
func TssCreateKey(keyId string, algo string) string {
	if algo != "ecdsa" && algo != "eddsa" {
		Abort("algo must be ecdsa or eddsa")
	}

	return *tssCreateKey(&keyId, &algo)
}

// Get details of a TSS key. Returns a comma-separated string consists of status, public key and algo.
func TssGetKey(keyId string) string {
	return *tssGetKey(&keyId)
}

// Request a digest to be signed by the TSS key.
func TssSignKey(keyId string, bytes []byte) {
	byteStr := hex.EncodeToString(bytes)

	tssSignKey(&keyId, &byteStr)
}
