//go:build integration

package executors

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/flowstore"
	"github.com/c360studio/semstreams/natsclient"
	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
)

// TestFlowExecutorListFlowsRealManagerEmpty drives list_flows over a REAL
// flowstore.Manager on an empty bucket, through the production registration
// wire. The in-memory fake in flows_test.go returns a typed empty list, so it
// can never reach the failure the tool has over a real Manager; only real NATS
// proves the empty branch is reachable.
func TestFlowExecutorListFlowsRealManagerEmpty(t *testing.T) {
	ctx := context.Background()
	client := natsclient.NewTestClient(t, natsclient.WithJetStream(), natsclient.WithKV()).Client

	mgr, err := flowstore.NewManager(client)
	require.NoError(t, err)

	registry := agentictools.NewExecutorRegistry()
	require.NoError(t, RegisterBuiltins(ctx, registry, ToolDependencies{
		NATSClient:   client,
		FlowManager:  mgr,
		SkipBuiltins: skipAllBut("flows"),
	}))
	require.NotNil(t, registry.GetTool("list_flows"), "list_flows must be registered through RegisterBuiltins")

	result, execErr := registry.Execute(ctx, agentic.ToolCall{ID: "l1", Name: "list_flows"})
	require.NoError(t, execErr, "listing an empty store must not be an execution error")
	assert.Equal(t, "", result.Error, "an empty store must carry no error attachment")
	assert.Equal(t, agentic.ToolErrorKind(""), result.ErrorKind, "an empty store must carry no error kind")
	assert.Equal(t, "No flows configured.", result.Content)
}
