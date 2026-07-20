package graphview

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestCoalescingLWWWithinWindow: five writes to one key inside one window
// deliver exactly ONE delta carrying the newest value; intermediates are
// coalesced away (G4 allows losing intermediates, never reordering — the
// tracker enforces the no-reorder half on every delivery).
func TestCoalescingLWWWithinWindow(t *testing.T) {
	h := newHarness(t, 0)
	h.endReplay()

	snap, sub, err := h.view.SnapshotAndSubscribe(context.Background())
	require.NoError(t, err)
	tr := newTracker(t)
	tr.loadSnapshot(snap)

	for rev := uint64(1); rev <= 5; rev++ {
		h.put("K", "v"+string(rune('0'+rev)), rev)
	}
	r := h.tickNow()
	require.Equal(t, 1, r.changed, "five writes to one key coalesce to one changed key")

	batch := recvBatch(t, sub)
	tr.apply(batch)
	require.Len(t, batch, 1, "exactly one delta for K")
	require.Equal(t, DeltaUpsert, batch[0].Op)
	require.Equal(t, uint64(5), batch[0].Revision)
	require.Equal(t, "v5", batch[0].Value)

	// An empty window delivers nothing.
	r = h.tickNow()
	require.Equal(t, 0, r.changed)
	sub.Unsubscribe()
}

// TestDeleteDeliveryToAttachedSubscriber: a subscriber holding K from its
// snapshot receives the tombstone and converges to absence; a later
// delete->recreate inside one window delivers only the recreate; a window
// ending after a bare delete delivers the tombstone (never a pre-delete
// value); purge rides the same lane as delete.
func TestDeleteDeliveryToAttachedSubscriber(t *testing.T) {
	h := newHarness(t, 0)
	h.put("K", "v1", 1)
	h.put("P", "p1", 2)
	h.endReplay()

	snap, sub, err := h.view.SnapshotAndSubscribe(context.Background())
	require.NoError(t, err)
	tr := newTracker(t)
	tr.loadSnapshot(snap)
	require.Equal(t, "v1", tr.values["K"])

	// Bare delete: the window delivers the tombstone.
	h.del("K", 3)
	h.tickNow()
	batch := recvBatch(t, sub)
	tr.apply(batch)
	require.Len(t, batch, 1)
	require.Equal(t, DeltaDelete, batch[0].Op)
	require.Equal(t, uint64(3), batch[0].Revision)
	_, ok := tr.values["K"]
	require.False(t, ok, "subscriber state must converge to key-absence after the tombstone")
	_, _, err = h.view.Get("K")
	require.ErrorIs(t, err, ErrKeyNotFound, "point read is coherent with the delete")

	// Delete -> recreate within ONE window: only the recreate is delivered.
	h.del("P", 4)
	h.put("P", "p2", 5)
	h.tickNow()
	batch = recvBatch(t, sub)
	tr.apply(batch)
	require.Len(t, batch, 1, "intermediate tombstone coalesced away")
	require.Equal(t, DeltaUpsert, batch[0].Op)
	require.Equal(t, uint64(5), batch[0].Revision)
	require.Equal(t, "p2", tr.values["P"])

	// Purge is a tombstone through the same coalescing lane.
	h.purge("P", 6)
	h.tickNow()
	batch = recvBatch(t, sub)
	tr.apply(batch)
	require.Len(t, batch, 1)
	require.Equal(t, DeltaDelete, batch[0].Op)
	require.Equal(t, uint64(6), batch[0].Revision)
	sub.Unsubscribe()
}

// TestSlowSubscriberBackpressure: A stops draining while B and C keep
// receiving at view rate; A's pending coalesces LWW per key (at-most-once,
// bounded by changed-key cardinality); on resume A converges to current
// state WITHOUT replaying every intermediate revision; the watcher, the
// apply, and peers are never blocked (proven by the ticks and applies
// completing while A is stalled). Slowness never disconnects A.
func TestSlowSubscriberBackpressure(t *testing.T) {
	h := newHarness(t, 0)
	h.endReplay()

	ctx := context.Background()
	_, subA, err := h.view.SnapshotAndSubscribe(ctx)
	require.NoError(t, err)
	_, subB, err := h.view.SnapshotAndSubscribe(ctx)
	require.NoError(t, err)
	_, subC, err := h.view.SnapshotAndSubscribe(ctx)
	require.NoError(t, err)

	// Five windows land while A does not drain. Every apply and every
	// fan-out completes (h.put and tickNow block on their hooks), so the
	// watcher and apply demonstrably never blocked on A.
	const windows = 5
	trB, trC := newTracker(t), newTracker(t)
	for i := uint64(1); i <= windows; i++ {
		h.put("K", "v"+string(rune('0'+i)), i)
		r := h.tickNow()
		require.Equal(t, 3, r.subs)
		bb := recvBatch(t, subB)
		trB.apply(bb)
		require.Equal(t, i, bb[0].Revision, "B receives every window at view rate")
		cc := recvBatch(t, subC)
		trC.apply(cc)
		require.Equal(t, i, cc[0].Revision, "C receives every window at view rate")
	}

	// A resumes: it converges to K@5 in at most 3 batches (one possibly
	// buffered in the channel, one possibly mid-send, one coalesced LWW
	// swap) — strictly fewer than the 5 windows, and never out of order.
	trA := newTracker(t)
	batches := 0
	for trA.revs["K"] < windows {
		batch := recvBatch(t, subA)
		trA.apply(batch)
		batches++
		require.LessOrEqual(t, batches, 3, "A's catch-up must be coalesced, not a full replay")
	}
	require.Equal(t, "v5", trA.values["K"])
	require.NoError(t, subA.Err(), "slowness alone must never disconnect a subscriber")

	subA.Unsubscribe()
	subB.Unsubscribe()
	subC.Unsubscribe()
}

// TestPoisonSurfacedNotDelivered (G6): a decode failure surfaces as a typed
// per-key poison signal in the delta lane and on point reads; the value is
// never delivered as an upsert; other keys keep flowing; a later canonical
// write heals the key (per-entity poison, ADR-079 semantics); snapshots
// carry the poison record so late attachers can drive their latches.
func TestPoisonSurfacedNotDelivered(t *testing.T) {
	h := newHarness(t, 0)
	h.endReplay()

	snap, sub, err := h.view.SnapshotAndSubscribe(context.Background())
	require.NoError(t, err)
	require.Empty(t, snap.Poisoned)

	h.put("P", "POISON-bad-contract", 1)
	h.put("G", "good", 2)
	h.tickNow()

	batch := recvBatch(t, sub)
	require.Len(t, batch, 2, "poison for P AND the upsert for G — unrelated keys keep flowing")

	require.Equal(t, DeltaPoison, batch[0].Op)
	require.Equal(t, "P", batch[0].Key)
	var pe *PoisonError
	require.ErrorAs(t, batch[0].Err, &pe, "poison signal must be the typed *PoisonError")
	require.Equal(t, "P", pe.Key)
	require.Equal(t, uint64(1), pe.Revision)
	require.Contains(t, pe.Err.Error(), "test contract violation",
		"the decode/contract error must be preserved for consumer latches")

	require.Equal(t, DeltaUpsert, batch[1].Op)
	require.Equal(t, "G", batch[1].Key)

	// Point read surfaces the poison, never a stale or zero value.
	_, rev, err := h.view.Get("P")
	require.ErrorAs(t, err, &pe)
	require.Equal(t, uint64(1), rev)

	// A late attacher's snapshot carries the poison record (latch survives
	// migration); the poisoned key has no servable entry.
	snap2, sub2, err := h.view.SnapshotAndSubscribe(context.Background())
	require.NoError(t, err)
	require.Contains(t, snap2.Poisoned, "P")
	require.NotContains(t, snap2.Entries, "P")
	require.Contains(t, snap2.Entries, "G")
	sub2.Unsubscribe()

	// A newer canonical write heals the key.
	h.put("P", "healed", 3)
	h.tickNow()
	batch = recvBatch(t, sub)
	require.Len(t, batch, 1)
	require.Equal(t, DeltaUpsert, batch[0].Op)
	require.Equal(t, "healed", batch[0].Value)
	val, rev, err := h.view.Get("P")
	require.NoError(t, err)
	require.Equal(t, "healed", val)
	require.Equal(t, uint64(3), rev)
	require.Empty(t, h.view.Poisoned())
	sub.Unsubscribe()
}
