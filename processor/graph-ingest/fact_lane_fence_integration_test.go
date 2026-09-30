//go:build integration

package graphingest

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/natsclient"
)

// TestIntegration_FactLane_PoisonLoopExecutionEntityIsAckDropped drives the
// REAL consume closure (#1112): a LoopExecutionEntity whose loop_id cannot form
// an entity ID is published on the Graphable-lane stream. Before the fix its
// EntityID panicked, the stream handler's recover Nak'd, and the message was
// redelivered without end. Now the lane's Validate refuses it: counted, WARN,
// acked once, never redelivered, nothing persisted.
func TestIntegration_FactLane_PoisonLoopExecutionEntityIsAckDropped(t *testing.T) {
	ctx, cancelOperation := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancelOperation()
	c, testClient, owner := startKeyedWireComponent(ctx, t)
	defer owner.finish(ctx, t)

	typ := agentic.LoopExecutionMessageType()
	wire := map[string]any{
		"id":   "fence-loop-integration-001",
		"type": map[string]string{"domain": typ.Domain, "category": typ.Category, "version": typ.Version},
		"payload": map[string]any{
			"org": "c360", "platform": "test", "loop_id": "loop.dotted",
			"task": map[string]string{"task_id": "t", "role": "r"},
		},
		"meta": map[string]any{"created_at": 1, "received_at": 1, "source": "fence-test"},
	}
	data, err := json.Marshal(wire)
	require.NoError(t, err)
	before := atomic.LoadInt64(&c.errors)

	require.NoError(t, testClient.Client.PublishToStream(ctx, "entity.fence.poison", data))

	require.Eventually(t, func() bool {
		return atomic.LoadInt64(&c.errors) == before+1
	}, 10*time.Second, 20*time.Millisecond, "the poison message must be counted on the lane's error path")

	// Server-side consumer state is the authority on delivery: the message was
	// delivered once and acknowledged, not Nak'd back into the stream.
	require.Eventually(t, func() bool {
		pending, ackPending, _ := entityStreamConsumerState(t, ctx, testClient)
		return pending == 0 && ackPending == 0
	}, 10*time.Second, 20*time.Millisecond, "the poison message must be acknowledged")
	_, _, redelivered := entityStreamConsumerState(t, ctx, testClient)
	assert.Zero(t, redelivered, "the poison message must not be redelivered")
	assert.Equal(t, before+1, atomic.LoadInt64(&c.errors), "counted exactly once")

	bucket, err := testClient.GetKVBucket(ctx, "ENTITY_STATES")
	require.NoError(t, err)
	lister, err := bucket.ListKeys(ctx)
	require.NoError(t, err)
	var keys []string
	for key := range lister.Keys() {
		keys = append(keys, key)
	}
	assert.Empty(t, keys, "nothing is persisted for the poison entity")
}

// entityStreamConsumerState sums the ENTITY stream consumers' pending,
// ack-pending and redelivered counts, read from the server.
func entityStreamConsumerState(
	t *testing.T, ctx context.Context, testClient *natsclient.TestClient,
) (pending uint64, ackPending int, redelivered int) {
	t.Helper()
	stream, err := testClient.GetStream(ctx, "ENTITY")
	require.NoError(t, err)
	consumers := stream.ListConsumers(ctx)
	for info := range consumers.Info() {
		pending += info.NumPending
		ackPending += info.NumAckPending
		redelivered += info.NumRedelivered
	}
	require.NoError(t, consumers.Err())
	return pending, ackPending, redelivered
}
