//go:build integration

// Integration test for pkg/graphview covering the production wire path: a
// real NATS KV bucket and a real WatchAll (ordered consumer, end-of-replay
// nil marker, tombstone delivery) through a testcontainer. The deterministic
// coherence burden lives in the unit suite (fake watcher, scripted
// interleavings); this single focused test proves the composed path:
// bootstrap -> caught-up -> snapshot+subscribe -> live deltas -> delete ->
// convergence and point-read coherence.
//
// Run with `go test -tags=integration -race ./pkg/graphview/...`.
package graphview

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/natsclient"
)

type itRecord struct {
	Name string `json:"name"`
}

func decodeITRecord(key string, value []byte, _ EntryMeta) (*itRecord, bool, error) {
	var rec itRecord
	if err := json.Unmarshal(value, &rec); err != nil {
		return nil, false, fmt.Errorf("decode record %s: %w", key, err)
	}
	if rec.Name == "" {
		return nil, false, nil // not-ready record maps to absent
	}
	return &rec, true, nil
}

func TestIntegration_GraphViewOverRealBucket(t *testing.T) {
	const bucket = "GRAPHVIEW_IT"
	tc := natsclient.NewTestClient(t, natsclient.WithKVBuckets(bucket))
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	kv, err := tc.Client.GetKeyValueBucket(ctx, bucket)
	require.NoError(t, err)

	// Pre-view state: the bootstrap replay must deliver it.
	_, err = kv.Put(ctx, "alpha", []byte(`{"name":"a1"}`))
	require.NoError(t, err)
	_, err = kv.Put(ctx, "beta", []byte(`{"name":"b1"}`))
	require.NoError(t, err)

	view, err := New[*itRecord](kv, decodeITRecord, WithTickInterval(50*time.Millisecond))
	require.NoError(t, err)
	require.NoError(t, view.Start(ctx))
	t.Cleanup(view.Stop)

	// Readiness = caught-up, not started: the real end-of-replay marker.
	require.NoError(t, view.WaitCaughtUp(ctx))
	require.True(t, view.CaughtUp())

	snap, sub, err := view.SnapshotAndSubscribe(ctx)
	require.NoError(t, err)
	require.Len(t, snap.Entries, 2)
	require.Equal(t, "a1", snap.Entries["alpha"].Value.Name)

	values := make(map[string]string, len(snap.Entries))
	revs := make(map[string]uint64, len(snap.Entries))
	for k, e := range snap.Entries {
		values[k] = e.Value.Name
		revs[k] = e.Revision
	}

	// Live phase: create, update, delete — all through the real bucket.
	_, err = kv.Put(ctx, "gamma", []byte(`{"name":"g1"}`))
	require.NoError(t, err)
	_, err = kv.Put(ctx, "alpha", []byte(`{"name":"a2"}`))
	require.NoError(t, err)
	require.NoError(t, kv.Delete(ctx, "beta"))

	// Converge by reading batches (event-driven; the deadline is the
	// context, not a sleep).
	want := map[string]string{"alpha": "a2", "gamma": "g1"}
	for !mapsEqualIT(values, want) {
		select {
		case batch, ok := <-sub.Deltas():
			require.True(t, ok, "subscription closed early: %v", sub.Err())
			for _, d := range batch {
				require.Greater(t, d.Revision, revs[d.Key], "stale delivery over the real wire")
				revs[d.Key] = d.Revision
				switch d.Op {
				case DeltaUpsert:
					values[d.Key] = d.Value.Name
				case DeltaDelete, DeltaPoison:
					delete(values, d.Key)
				}
			}
		case <-ctx.Done():
			t.Fatalf("did not converge to %v; have %v", want, values)
		}
	}

	// Point reads are coherent with the stream the subscriber saw.
	val, rev, err := view.Get("alpha")
	require.NoError(t, err)
	require.Equal(t, "a2", val.Name)
	require.GreaterOrEqual(t, rev, revs["alpha"])
	_, _, err = view.Get("beta")
	require.ErrorIs(t, err, ErrKeyNotFound)

	// A fresh attach snapshots the converged state (snapshot+delta seam over
	// the real wire).
	snap2, sub2, err := view.SnapshotAndSubscribe(ctx)
	require.NoError(t, err)
	got := make(map[string]string, len(snap2.Entries))
	for k, e := range snap2.Entries {
		got[k] = e.Value.Name
	}
	require.Equal(t, want, got)
	sub2.Unsubscribe()
	sub.Unsubscribe()
}

func mapsEqualIT(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}
