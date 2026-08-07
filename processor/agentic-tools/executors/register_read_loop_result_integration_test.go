//go:build integration

package executors

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/natsclient"
	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
)

func TestIntegration_ReadLoopResultLazyMustExistSurvivesCleanBoot(t *testing.T) {
	ctx := context.Background()
	testClient := natsclient.NewTestClient(t, natsclient.WithKV())
	client := testClient.Client

	_, err := client.GetKeyValueBucket(ctx, "AGENT_LOOPS")
	require.ErrorIs(t, err, jetstream.ErrBucketNotFound)

	registry := agentictools.NewExecutorRegistry()
	err = RegisterBuiltins(ctx, registry, ToolDependencies{
		NATSClient:   client,
		LoopsBucket:  "AGENT_LOOPS",
		SkipBuiltins: skipAllBut("read_loop_result"),
	})
	require.NoError(t, err)
	require.NotNil(t, registry.GetTool(agentictools.ReadLoopResultToolName))
	_, err = client.GetKeyValueBucket(ctx, "AGENT_LOOPS")
	require.ErrorIs(t, err, jetstream.ErrBucketNotFound, "registration must not create the owner's bucket")

	result, execErr := registry.Execute(ctx, agentic.ToolCall{
		ID: "before-owner", Name: agentictools.ReadLoopResultToolName,
		Arguments: map[string]any{"loop_id": "loop-1"},
	})
	require.Error(t, execErr)
	assert.Equal(t, agentic.ToolErrorNetwork, result.ErrorKind)
	_, err = client.GetKeyValueBucket(ctx, "AGENT_LOOPS")
	require.ErrorIs(t, err, jetstream.ErrBucketNotFound, "failed execution must not create the owner's bucket")

	bucket, err := client.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{
		Bucket: "AGENT_LOOPS", History: 10, TTL: 24 * time.Hour,
	})
	require.NoError(t, err)
	event, err := json.Marshal(agentic.LoopCompletedEvent{
		LoopID: "loop-1", TaskID: "task-1", Outcome: agentic.OutcomeSuccess,
		Role: "worker", Result: "owner is ready", CompletedAt: time.Now().UTC(),
	})
	require.NoError(t, err)
	_, err = bucket.Put(ctx, "COMPLETE_loop-1", event)
	require.NoError(t, err)

	result, execErr = registry.Execute(ctx, agentic.ToolCall{
		ID: "after-owner", Name: agentictools.ReadLoopResultToolName,
		Arguments: map[string]any{"loop_id": "loop-1"},
	})
	require.NoError(t, execErr)
	assert.Equal(t, "owner is ready", result.Content)
}
