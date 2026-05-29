package contract_test

import (
	"fmt"
	"testing"
)

// TestPaymentTokenEventsEmitted verifies addPaymentToken / removePaymentToken
// emit the whitelist events the indexer folds into magi_market_payment_tokens.
func TestPaymentTokenEventsEmitted(t *testing.T) {
	ct := SetupContractTest()
	InitMarket(t, ct)

	tok := "vsc1BYBwMvsSFwqvwzio352VWp6fGkjVs7t3Dp"

	_, _, addLogs := CallMarket(t, ct, "addPaymentToken",
		[]byte(fmt.Sprintf(`{"token":"%s"}`, tok)), nil, ownerAddress, "", true, gas, "")
	AssertEventEmitted(t, addLogs, "payment_token_added")
	AssertEventContains(t, addLogs, "payment_token_added", fmt.Sprintf(`"token":"%s"`, tok))

	_, _, rmLogs := CallMarket(t, ct, "removePaymentToken",
		[]byte(fmt.Sprintf(`{"token":"%s"}`, tok)), nil, ownerAddress, "", true, gas, "")
	AssertEventEmitted(t, rmLogs, "payment_token_removed")
	AssertEventContains(t, rmLogs, "payment_token_removed", fmt.Sprintf(`"token":"%s"`, tok))
}
