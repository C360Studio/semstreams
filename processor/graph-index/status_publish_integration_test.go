//go:build integration

package graphindex

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

// TestIntegration_GraphIndexPublishesReadinessHeartbeat drives task 2.1/2.2 through the
// PRODUCTION component lifecycle: Start creates GRAPH_STATUS eagerly, and the status
// tick writes the envelope to this producer's key unconditionally, so the key both
// appears and heartbeats. It deliberately does not call the publish helper — a helper
// test would prove the helper works while Start silently never wired it.
func TestIntegration_GraphIndexPublishesReadinessHeartbeat(t *testing.T) {
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
	created, err := CreateGraphIndex(configJSON, component.Dependencies{NATSClient: nc})
	require.NoError(t, err)
	indexComponent := created.(*Component)
	require.NoError(t, indexComponent.Initialize())

	// Shorten the heartbeat instead of sleeping through the 5s production cadence.
	// Set before Start: the tick goroutine reads it at creation, so the write
	// happens-before the read via goroutine start.
	indexComponent.statusInterval = 250 * time.Millisecond

	require.NoError(t, indexComponent.Start(ctx))
	defer indexComponent.Stop(context.Background())

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

	// 2.2: the key appears AND heartbeats. Two successive entries with an advancing
	// revision prove the write is unconditional (nothing about the index changes
	// between ticks in this test, so a change-gated publisher would write once).
	entries := collectStatusEntries(ctx, t, statusKV, readiness.KeyGraphIndex, 2)
	require.Greater(t, entries[1].Revision(), entries[0].Revision(),
		"second heartbeat did not advance the revision — the publish is not unconditional")

	// The value is plain graph.IndexStatusResponse JSON (a KV value, not a
	// payload-registry BaseMessage publish), decodable by the same struct fusion uses.
	for i, entry := range entries {
		require.Equal(t, jetstream.KeyValuePut, entry.Operation(), "entry %d", i)
		var envelope graph.IndexStatusResponse
		require.NoError(t, json.Unmarshal(entry.Value(), &envelope), "entry %d value: %s", i, entry.Value())
		require.Contains(t, graph.AllIndexStates, envelope.State, "entry %d carried no known state", i)

		var raw map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(entry.Value(), &raw))
		require.NotContains(t, raw, "payload", "entry %d is wrapped in a payload-registry envelope", i)
	}

	// The consumer side reads it as FRESH through the production watcher — the spec
	// scenario "consumers hold last-known readiness without polling".
	watcher := readiness.NewWatcher(nc, readiness.KeyGraphIndex,
		readiness.WithHeartbeat(indexComponent.statusInterval))
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

// TestIntegration_StatusBucketCreationIsIdempotent proves the two-producer case
// directly: EnsureBucket runs repeatedly against a live NATS (as it does when
// graph-index and graph-embedding both Start in cmd/semstreams) and never errors,
// including when the bucket already carries a value.
func TestIntegration_StatusBucketCreationIsIdempotent(t *testing.T) {
	testClient := natsclient.NewTestClient(t, natsclient.WithKV())
	nc := testClient.Client

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	first, err := readiness.EnsureBucket(ctx, nc)
	require.NoError(t, err)
	_, err = first.Put(ctx, readiness.KeyGraphIndex, []byte(`{"state":"building"}`))
	require.NoError(t, err)

	// The sibling producer's Start, after the bucket exists and holds a value.
	second, err := readiness.EnsureBucket(ctx, nc)
	require.NoError(t, err, "second producer's eager create must not error out its Start")

	entry, err := second.Get(ctx, readiness.KeyGraphIndex)
	require.NoError(t, err, "the second handle must address the same bucket, not a fresh one")
	require.JSONEq(t, `{"state":"building"}`, string(entry.Value()))
}
