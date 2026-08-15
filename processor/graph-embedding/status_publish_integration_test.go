//go:build integration

package graphembedding

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/graph/readiness"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
)

// collectStatusEntries watches a GRAPH_STATUS key on the REAL bucket and returns the
// first `want` non-nil entries. Synchronization is the watch itself — no sleeping on
// the heartbeat, no polling loop: the watch delivers when the producer writes, and the
// only clock involved is the overall failure deadline.
func collectStatusEntries(ctx context.Context, t *testing.T, kv jetstream.KeyValue, key string, want int) []jetstream.KeyValueEntry {
	t.Helper()
	watcher, err := kv.Watch(ctx, key)
	require.NoError(t, err)
	defer func() { require.NoError(t, watcher.Stop()) }()

	entries := make([]jetstream.KeyValueEntry, 0, want)
	deadline := time.After(30 * time.Second)
	for len(entries) < want {
		select {
		case entry, ok := <-watcher.Updates():
			require.True(t, ok, "status watch closed after %d of %d entries", len(entries), want)
			if entry == nil {
				// End-of-initial-values marker, not a value.
				continue
			}
			entries = append(entries, entry)
		case <-deadline:
			t.Fatalf("only %d of %d status writes arrived on %s/%s",
				len(entries), want, readiness.BucketGraphStatus, key)
		case <-ctx.Done():
			t.Fatalf("context ended waiting for status writes: %v", ctx.Err())
		}
	}
	return entries
}

// TestIntegration_GraphEmbeddingPublishesReadinessHeartbeat drives task 2.1/2.2 through
// the PRODUCTION component lifecycle: Start creates GRAPH_STATUS eagerly, and the
// status tick writes the envelope to graph-embedding's OWN key (one key per producer)
// unconditionally. It deliberately does not call the publish helper — a helper test
// would prove the helper works while Start silently never wired it.
func TestIntegration_GraphEmbeddingPublishesReadinessHeartbeat(t *testing.T) {
	testClient := natsclient.NewTestClient(t, natsclient.WithKV())
	nc := testClient.Client

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	js, err := nc.JetStream()
	require.NoError(t, err)
	_, err = js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket:      graph.BucketEntityStates,
		Description: "Test entity states",
	})
	require.NoError(t, err)

	configJSON, err := json.Marshal(DefaultConfig())
	require.NoError(t, err)
	created, err := CreateGraphEmbedding(configJSON, component.Dependencies{NATSClient: nc})
	require.NoError(t, err)
	embeddingComponent := created.(*Component)
	require.NoError(t, embeddingComponent.Initialize())

	// Shorten the heartbeat instead of sleeping through the 5s production cadence.
	// Set before Start: the tick goroutine reads it at creation, so the write
	// happens-before the read via goroutine start.
	embeddingComponent.statusInterval = 250 * time.Millisecond

	require.NoError(t, embeddingComponent.Start(ctx))
	defer embeddingComponent.Stop(context.Background())

	// 2.1: the bucket exists because Start created it eagerly, not because a
	// consumer's watch lazily provoked it.
	statusKV, err := js.KeyValue(ctx, readiness.BucketGraphStatus)
	require.NoError(t, err, "Start must create %s eagerly", readiness.BucketGraphStatus)

	status, err := statusKV.Status(ctx)
	require.NoError(t, err)
	statusSpec, ok := graph.SpecFor(readiness.BucketGraphStatus)
	require.True(t, ok, "GRAPH_STATUS must be a catalog bucket")
	require.Equal(t, int64(statusSpec.History), status.History(),
		"bucket history must be the shared catalog value")

	// 2.2: graph-embedding writes its OWN key and heartbeats on it. Two successive
	// entries with an advancing revision prove the write is unconditional.
	entries := collectStatusEntries(ctx, t, statusKV, readiness.KeyGraphEmbedding, 2)
	require.Greater(t, entries[1].Revision(), entries[0].Revision(),
		"second heartbeat did not advance the revision — the publish is not unconditional")

	for i, entry := range entries {
		require.Equal(t, jetstream.KeyValuePut, entry.Operation(), "entry %d", i)
		var envelope graph.IndexStatusResponse
		require.NoError(t, json.Unmarshal(entry.Value(), &envelope), "entry %d value: %s", i, entry.Value())
		require.Contains(t, graph.AllIndexStates, envelope.State, "entry %d carried no known state", i)
	}

	// One key per producer: graph-embedding must not have written graph-index's key.
	_, err = statusKV.Get(ctx, readiness.KeyGraphIndex)
	require.Error(t, err, "graph-embedding wrote the graph-index key — producers must not share a key")

	// The consumer side reads it as FRESH through the production watcher.
	watcher := readiness.NewWatcher(nc, readiness.KeyGraphEmbedding,
		readiness.WithHeartbeat(embeddingComponent.statusInterval))
	require.NoError(t, watcher.Start(ctx))
	defer watcher.Stop()

	require.Eventually(t, func() bool {
		return watcher.Read().Fresh
	}, 10*time.Second, 50*time.Millisecond, "published status never read fresh by the shared watcher")

	reading := watcher.Read()
	require.True(t, reading.Known)
	require.NoError(t, reading.Err)
	require.Contains(t, graph.AllIndexStates, reading.Status.State)
}
