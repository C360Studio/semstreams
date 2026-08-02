//go:build integration

package agenticloop_test

// Integration test for payload-size-chokepoints task 3.4 (gh#857 D4):
// an oversized completion result flows through the PRODUCTION loop path
// (task → agent.request → agent.response → persistHandlerResult), offloads
// to the AGENT_CONTENT ObjectStore, lands in AGENT_LOOPS as a ref-bearing
// COMPLETE_ value, and reads back WHOLE through read_loop_result's existing
// max_bytes/offset paging contract against the real stores.

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/payloadbuiltins"
	agenticloop "github.com/c360studio/semstreams/processor/agentic-loop"
	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
	"github.com/c360studio/semstreams/storage/objectstore"
)

func TestIntegration_OversizedCompletionOffloadsAndPagesBack(t *testing.T) {
	natsClient := getSharedNATSClient(t)

	config := agenticloop.Config{
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{Name: "tasks", Type: "jetstream", Subject: "agent.task.*", StreamName: "AGENT", Required: true},
				{Name: "responses", Type: "jetstream", Subject: "agent.response.>", StreamName: "AGENT"},
			},
			Outputs: []component.PortDefinition{
				{Name: "agent.request", Type: "jetstream", Subject: "agent.request.*", StreamName: "AGENT"},
				{Name: "agent.complete", Type: "jetstream", Subject: "agent.complete.*", StreamName: "AGENT"},
			},
		},
		MaxIterations:      10,
		Timeout:            "60s",
		StreamName:         "AGENT",
		ConsumerNameSuffix: "offload-test",
		LoopsBucket:        "AGENT_LOOPS",
		ContentBucket:      "AGENT_CONTENT",
	}
	rawConfig, err := json.Marshal(config)
	require.NoError(t, err)

	deps := component.Dependencies{
		NATSClient:      natsClient,
		PayloadRegistry: payloadbuiltins.NewTestRegistry(t),
		// Platform identity makes the offload entity ID constructible; without
		// it the offload path degrades to the seam guard.
		Platform: component.PlatformMeta{Org: "c360", Platform: "inttest"},
	}
	comp, err := agenticloop.NewComponent(rawConfig, deps)
	require.NoError(t, err)
	lc, ok := comp.(component.LifecycleComponent)
	require.True(t, ok)
	require.NoError(t, lc.Initialize())

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Platform identity is set, so the graph writer will attempt spawn/
	// completion writes; answer them with the package's stub responders
	// (graph_writer_integration_test.go) so birth succeeds without a full
	// graph-ingest.
	tc := &tripleCollector{}
	if _, serr := natsClient.SubscribeForRequests(ctx, "graph.mutation.triple.add", tc.handler); serr != nil {
		t.Fatalf("subscribe triple.add: %v", serr)
	}
	if _, serr := natsClient.SubscribeForRequests(ctx, "graph.mutation.triple.add_batch", tc.batchHandler); serr != nil {
		t.Fatalf("subscribe triple.add_batch: %v", serr)
	}
	cwt := &createWithTriplesResponder{}
	cwt.subscribe(t, ctx, natsClient)

	require.NoError(t, lc.Start(ctx))
	defer lc.Stop(5 * time.Second)

	time.Sleep(200 * time.Millisecond)

	// Capture the loop's model request so we can answer it.
	var requestMu sync.Mutex
	var requests []agentic.AgentRequest
	dec := payloadbuiltins.NewTestDecoder(t)
	_, err = natsClient.Subscribe(ctx, "agent.request.>", func(_ context.Context, msg *nats.Msg) {
		if baseMsg, decErr := dec.Decode(msg.Data); decErr == nil {
			if req, reqOK := baseMsg.Payload().(*agentic.AgentRequest); reqOK {
				requestMu.Lock()
				requests = append(requests, *req)
				requestMu.Unlock()
			}
		}
	})
	require.NoError(t, err)
	time.Sleep(100 * time.Millisecond)

	const loopID = "offload-e2e-1"
	task := &agentic.TaskMessage{
		LoopID: loopID,
		TaskID: "task-offload-1",
		Role:   "general",
		Model:  "test-model",
		Prompt: "Produce a very large result",
	}
	publishTaskMessage(t, natsClient, "agent.task.offload", task)

	require.Eventually(t, func() bool {
		requestMu.Lock()
		defer requestMu.Unlock()
		return len(requests) > 0
	}, 10*time.Second, 100*time.Millisecond, "loop must publish a model request")

	requestMu.Lock()
	requestID := requests[0].RequestID
	requestMu.Unlock()

	// The oversized result: over half the server's 1MB default payload limit
	// (the derived offload threshold) but under the limit itself, so the
	// agent.response delivering it still fits the wire.
	body := strings.Repeat("0123456789abcdef", 45_000) // 720,000 bytes > 524,288 threshold
	response := &agentic.AgentResponse{
		RequestID: requestID,
		Status:    agentic.StatusComplete,
		Message:   agentic.ChatMessage{Role: "assistant", Content: body},
	}
	publishResponseMessage(t, natsClient, "agent.response."+requestID, response)

	// The COMPLETE_ value must appear ref-bearing: {storage_ref, preview,
	// size}, inline result shed.
	js, err := natsClient.JetStream()
	require.NoError(t, err)
	bucket, err := js.KeyValue(ctx, "AGENT_LOOPS")
	require.NoError(t, err)

	var event agentic.LoopCompletedEvent
	require.Eventually(t, func() bool {
		entry, gerr := bucket.Get(ctx, "COMPLETE_"+loopID)
		if gerr != nil {
			return false
		}
		return json.Unmarshal(entry.Value(), &event) == nil
	}, 15*time.Second, 200*time.Millisecond, "COMPLETE_ value must land")

	require.NotNil(t, event.ResultRef, "oversized completion must carry storage_ref")
	assert.Empty(t, event.Result, "inline result must be shed after offload")
	assert.Equal(t, len(body), event.ResultSize, "size must record the original byte count")
	assert.NotEmpty(t, event.ResultPreview, "preview must be present")
	assert.True(t, strings.HasPrefix(body, event.ResultPreview), "preview must be a prefix of the body")

	// The loop entity value must be slim too — the offload mirrors onto it.
	entityEntry, err := bucket.Get(ctx, loopID)
	require.NoError(t, err)
	var entity agentic.LoopEntity
	require.NoError(t, json.Unmarshal(entityEntry.Value(), &entity))
	assert.NotNil(t, entity.ResultRef, "loop entity must mirror the storage ref")
	assert.Less(t, len(entity.Result), len(body), "loop entity must carry the preview, not the body")
	assert.False(t, entity.ResultNotDurable, "a stored-by-reference result is durable")

	// Read back WHOLE through read_loop_result's paging contract against the
	// REAL stores (guarded KV lane + AGENT_CONTENT ObjectStore).
	kvStore := natsClient.NewKVStore(bucket)
	contentStore, err := objectstore.NewStoreWithConfig(ctx, natsClient, objectstore.Config{
		BucketName: "AGENT_CONTENT",
	})
	require.NoError(t, err)
	executor := agentictools.NewReadLoopResultExecutor(kvStore, contentStore)

	var rebuilt strings.Builder
	offset := 0
	pages := 0
	for {
		res, execErr := executor.Execute(ctx, agentic.ToolCall{
			ID:   "call-1",
			Name: agentictools.ReadLoopResultToolName,
			Arguments: map[string]any{
				"loop_id":   loopID,
				"max_bytes": 200_000,
				"offset":    offset,
			},
		})
		require.NoError(t, execErr, "paged read at offset %d", offset)
		require.Empty(t, res.Error, "tool error at offset %d", offset)
		rebuilt.WriteString(res.Content)
		pages++
		require.Less(t, pages, 20, "paging must terminate")

		hasMore, _ := res.Metadata[agentic.MetadataKeyHasMore].(bool)
		if !hasMore {
			break
		}
		next, nOK := res.Metadata[agentic.MetadataKeyNextOffset].(int)
		require.True(t, nOK, "next_offset must be an int, got %T", res.Metadata[agentic.MetadataKeyNextOffset])
		offset = next
	}
	assert.GreaterOrEqual(t, pages, 4, "720KB at 200KB/page must take multiple pages")
	require.Equal(t, len(body), rebuilt.Len(), "reassembled length must match the original")
	require.Equal(t, body, rebuilt.String(), "paged reads must reassemble to the exact original result")
}
