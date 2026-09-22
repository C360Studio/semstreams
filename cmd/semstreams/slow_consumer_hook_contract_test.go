package main

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestSlowConsumerHookRunsBetweenConnectionAndConfigArbitration pins where the
// probe runs in boot order: after the NATS connection exists and before config
// arbitration, which is the only window in which it can observe the callback.
// Which binary carries it, under which tag, is pinned by
// test/contract/e2e_tier_binary_contract_test.go.
func TestSlowConsumerHookRunsBetweenConnectionAndConfigArbitration(t *testing.T) {
	mainSource, err := os.ReadFile("main.go")
	require.NoError(t, err)
	source := string(mainSource)
	connect := strings.LastIndex(source, "bootstrapobservability.ConnectClient(")
	hook := strings.Index(source, "runSlowConsumerProbe(ctx, natsClient)")
	completion := strings.LastIndex(source, "spinner.Stop()")
	require.GreaterOrEqual(t, connect, 0)
	require.Greater(t, hook, connect)
	require.Greater(t, completion, hook)
}
