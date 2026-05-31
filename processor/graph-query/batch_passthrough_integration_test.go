//go:build integration

// Integration test for the graph.query.batch passthrough (gh#172
// Phase 1). The internal graph.ingest.query.batch handler in
// graph-ingest already exists with bounded concurrency + cache-aware
// reads + partial-success semantics; this test confirms the public
// passthrough at graph-query lands a request on it and forwards the
// reply verbatim.

package graphquery

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIntegration_BatchQueryPassthrough_ForwardsToGraphIngest pins
// the load-bearing wire: a client requesting graph.query.batch must
// arrive at graph.ingest.query.batch with the same request body, and
// the responder's reply must come back unmodified. semconnect's
// collection-hydration path depends on this passthrough; the test
// exists so the convention can't silently drift (e.g. a future
// refactor swapping the subject mapping in the StaticRouter).
func TestIntegration_BatchQueryPassthrough_ForwardsToGraphIngest(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	natsClient, cleanup := setupTestNATS(t)
	defer cleanup()

	ctx := context.Background()

	// Stub graph.ingest.query.batch responder. Echoes the IDs back
	// as minimal entity records so the test can verify both the
	// forwarded request shape AND that the reply passes through
	// unmodified.
	var stubHits atomic.Int32
	_, err := natsClient.SubscribeForRequests(ctx, "graph.ingest.query.batch",
		func(_ context.Context, data []byte) ([]byte, error) {
			stubHits.Add(1)
			var req struct {
				IDs []string `json:"ids"`
			}
			require.NoError(t, json.Unmarshal(data, &req))
			entities := make([]map[string]any, 0, len(req.IDs))
			for _, id := range req.IDs {
				entities = append(entities, map[string]any{
					"id":      id,
					"triples": []map[string]any{},
				})
			}
			return json.Marshal(map[string]any{"entities": entities})
		})
	require.NoError(t, err)

	// Start the graph-query component.
	config := DefaultConfig()
	configJSON, err := json.Marshal(config)
	require.NoError(t, err)
	comp, err := CreateGraphQuery(configJSON, component.Dependencies{NATSClient: natsClient})
	require.NoError(t, err)
	gq := comp.(*Component)
	require.NoError(t, gq.Initialize())
	require.NoError(t, gq.Start(ctx))
	t.Cleanup(func() { _ = gq.Stop(5 * time.Second) })

	// Fire a request at the PUBLIC graph.query.batch subject — exactly
	// what semconnect's collection-hydration path would do.
	ids := []string{
		"acme.ops.robotics.gcs.drone.001",
		"acme.ops.robotics.gcs.drone.002",
		"acme.ops.robotics.gcs.drone.003",
	}
	reqBody, err := json.Marshal(map[string]any{"ids": ids})
	require.NoError(t, err)

	respBody, err := natsClient.Request(ctx, "graph.query.batch", reqBody, 5*time.Second)
	require.NoError(t, err)

	// Responder must have been hit exactly once.
	assert.Equal(t, int32(1), stubHits.Load(), "stub responder should have received the forwarded request once")

	// Response must contain all three echoed entities — proves the
	// stub's reply passed back through the passthrough unmodified.
	var resp struct {
		Entities []map[string]any `json:"entities"`
	}
	require.NoError(t, json.Unmarshal(respBody, &resp))
	require.Len(t, resp.Entities, 3)
	for i, id := range ids {
		assert.Equal(t, id, resp.Entities[i]["id"])
	}
}

// TestIntegration_BatchQueryPassthrough_RejectsMalformedRequest pins
// the early-validation behavior — malformed JSON must NOT reach
// graph-ingest. The handler validates the request shape before
// forwarding; this test asserts on the stub-responder hit count to
// catch the validation regression (rather than asserting on the
// natsclient.Request error path, which is the gh#93 classified-error
// surface — a separate concern).
func TestIntegration_BatchQueryPassthrough_RejectsMalformedRequest(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	natsClient, cleanup := setupTestNATS(t)
	defer cleanup()

	ctx := context.Background()

	// Stub graph.ingest.query.batch — should NOT be invoked. If it
	// is, the passthrough is forwarding malformed bodies, which we
	// want to catch.
	var stubHits atomic.Int32
	_, err := natsClient.SubscribeForRequests(ctx, "graph.ingest.query.batch",
		func(_ context.Context, _ []byte) ([]byte, error) {
			stubHits.Add(1)
			return []byte(`{}`), nil
		})
	require.NoError(t, err)

	config := DefaultConfig()
	configJSON, err := json.Marshal(config)
	require.NoError(t, err)
	comp, err := CreateGraphQuery(configJSON, component.Dependencies{NATSClient: natsClient})
	require.NoError(t, err)
	gq := comp.(*Component)
	require.NoError(t, gq.Initialize())
	require.NoError(t, gq.Start(ctx))
	t.Cleanup(func() { _ = gq.Stop(5 * time.Second) })

	// Fire a malformed JSON request. The natsclient.Request call
	// itself may not surface an error (gh#93 dual-encoding window
	// returns the error body as a successful NATS reply); the
	// load-bearing assertion is that the stub was not hit.
	_, _ = natsClient.Request(ctx, "graph.query.batch", []byte("not json"), 2*time.Second)

	// Stub responder must NOT have been hit — passthrough rejected
	// the malformed body at validation, before any forward.
	assert.Equal(t, int32(0), stubHits.Load(),
		"malformed requests must not reach graph-ingest")
}
