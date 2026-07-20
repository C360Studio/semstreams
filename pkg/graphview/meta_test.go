package graphview

import (
	"context"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
)

// TestDecodeReceivesEntryMeta: the validating decode is handed the entry's
// authoritative metadata — revision and server write time — so decoded
// records can embed the KV write time instead of guessing from decode-time
// clocks (the operator-surface timestamp parity requirement).
func TestDecodeReceivesEntryMeta(t *testing.T) {
	created := time.Date(2026, 7, 1, 12, 30, 0, 0, time.UTC)
	metas := make(chan EntryMeta, 8)

	src := &fakeSource{}
	w := newFakeWatcher()
	src.queue = []*fakeWatcher{w}
	decode := func(_ string, value []byte, meta EntryMeta) (string, bool, error) {
		metas <- meta
		return string(value), true, nil
	}
	view, err := New[string](src, decode)
	require.NoError(t, err)
	require.NoError(t, view.Start(context.Background()))
	t.Cleanup(view.Stop)

	w.updates <- fakeEntry{key: "K", value: []byte("v1"), rev: 7, op: jetstream.KeyValuePut, created: created}
	select {
	case meta := <-metas:
		require.Equal(t, uint64(7), meta.Revision)
		require.True(t, meta.Created.Equal(created), "decode must receive entry.Created(), got %v", meta.Created)
	case <-time.After(testWait):
		t.Fatal("timed out waiting for decode")
	}
}

// TestDeltaCarriesCreated: every delta op carries the KV entry's Created
// timestamp through the coalesced lane — tombstones have no decoded T, so
// Delta.Created is their only channel for the write time (a delete-marker
// consumer must be able to reproduce entry.Created() exactly, per the old
// per-client watcher wire). LWW coalescing keeps the newest op's Created.
func TestDeltaCarriesCreated(t *testing.T) {
	h := newHarness(t, 0)
	h.endReplay()

	_, sub, err := h.view.SnapshotAndSubscribe(context.Background())
	require.NoError(t, err)

	tPut := time.Date(2026, 7, 1, 9, 0, 0, 0, time.UTC)
	tDel := time.Date(2026, 7, 1, 9, 5, 0, 0, time.UTC)
	tSkipPut := time.Date(2026, 7, 1, 9, 10, 0, 0, time.UTC)
	tSkip := time.Date(2026, 7, 1, 9, 15, 0, 0, time.UTC)

	// Upsert carries Created.
	h.send(fakeEntry{key: "K", value: []byte("v1"), rev: 1, op: jetstream.KeyValuePut, created: tPut})
	h.waitApplied()
	h.tickNow()
	batch := recvBatch(t, sub)
	require.Len(t, batch, 1)
	require.Equal(t, DeltaUpsert, batch[0].Op)
	require.True(t, batch[0].Created.Equal(tPut), "upsert delta must carry entry.Created()")

	// Delete tombstone carries the delete marker's Created.
	h.send(fakeEntry{key: "K", rev: 2, op: jetstream.KeyValueDelete, created: tDel})
	h.waitApplied()
	h.tickNow()
	batch = recvBatch(t, sub)
	require.Len(t, batch, 1)
	require.Equal(t, DeltaDelete, batch[0].Op)
	require.True(t, batch[0].Created.Equal(tDel), "tombstone delta must carry the delete marker's Created()")

	// keep=false on a previously-kept key synthesizes a tombstone carrying
	// the skipping write's Created.
	h.send(fakeEntry{key: "S", value: []byte("v1"), rev: 3, op: jetstream.KeyValuePut, created: tSkipPut})
	h.waitApplied()
	h.tickNow()
	batch = recvBatch(t, sub)
	require.Len(t, batch, 1)
	require.Equal(t, DeltaUpsert, batch[0].Op)

	h.send(fakeEntry{key: "S", value: []byte("SKIP"), rev: 4, op: jetstream.KeyValuePut, created: tSkip})
	h.waitApplied()
	h.tickNow()
	batch = recvBatch(t, sub)
	require.Len(t, batch, 1)
	require.Equal(t, DeltaDelete, batch[0].Op)
	require.True(t, batch[0].Created.Equal(tSkip), "synthesized tombstone must carry the skipping write's Created()")

	// LWW within one window keeps the newest op's Created alongside its
	// revision (G4: Created never regresses past a newer op).
	tOld := time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC)
	tNew := time.Date(2026, 7, 1, 10, 1, 0, 0, time.UTC)
	h.send(fakeEntry{key: "L", value: []byte("v1"), rev: 5, op: jetstream.KeyValuePut, created: tOld})
	h.waitApplied()
	h.send(fakeEntry{key: "L", value: []byte("v2"), rev: 6, op: jetstream.KeyValuePut, created: tNew})
	h.waitApplied()
	h.tickNow()
	batch = recvBatch(t, sub)
	require.Len(t, batch, 1)
	require.Equal(t, uint64(6), batch[0].Revision)
	require.True(t, batch[0].Created.Equal(tNew), "coalescing must keep the newest op's Created")

	sub.Unsubscribe()
}
