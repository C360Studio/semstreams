package graphview

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestBootstrapGatesAttach (G5): attach and point reads before the initial
// replay completes fail with the typed not-ready error — a partial bootstrap
// snapshot is never served unmarked. Readiness is caught-up, not started.
func TestBootstrapGatesAttach(t *testing.T) {
	h := newHarness(t, 0)

	// Mid-replay: entries applied, no end-of-replay marker yet.
	h.put("k1", "a", 1)
	require.False(t, h.view.CaughtUp())

	_, _, err := h.view.SnapshotAndSubscribe(context.Background())
	require.ErrorIs(t, err, ErrNotReady, "attach mid-bootstrap must be gated")
	_, err = h.view.Subscribe(context.Background())
	require.ErrorIs(t, err, ErrNotReady, "delta-only attach is gated the same way")
	_, _, err = h.view.Get("k1")
	require.ErrorIs(t, err, ErrNotReady, "point reads honor the readiness gate")
	_, err = h.view.List("", 0)
	require.ErrorIs(t, err, ErrNotReady)

	// WaitCaughtUp observes the gate (bounded by the caller's context).
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	err = h.view.WaitCaughtUp(cancelled)
	require.ErrorIs(t, err, context.Canceled)

	// The watermark advances during replay even though the view is gated.
	require.Equal(t, uint64(1), h.view.AppliedRevision())

	// End of replay: the gate lifts and the snapshot is complete.
	h.endReplay()
	require.True(t, h.view.CaughtUp())
	snap, sub, err := h.view.SnapshotAndSubscribe(context.Background())
	require.NoError(t, err)
	require.Len(t, snap.Entries, 1)
	require.Equal(t, uint64(1), snap.Entries["k1"].Revision)
	sub.Unsubscribe()
}

// TestWatcherLossFailsClosed (G5): when the shared watcher dies, every
// attached subscriber receives the explicit staleness signal (terminal close
// with Err() = wrapped ErrWatcherLost), point reads refuse to serve the
// frozen projection as live, and new attaches observe not-ready semantics
// that also identify the loss.
func TestWatcherLossFailsClosed(t *testing.T) {
	h := newHarness(t, 0)
	h.put("k1", "a", 1)
	h.endReplay()

	_, subX, err := h.view.SnapshotAndSubscribe(context.Background())
	require.NoError(t, err)
	subY, err := h.view.Subscribe(context.Background())
	require.NoError(t, err)

	// Watcher loss: the updates channel closes.
	close(h.w.updates)
	require.Error(t, h.waitLost())

	// Every subscriber observes the explicit terminal staleness signal.
	recvClosed(t, subX)
	require.ErrorIs(t, subX.Err(), ErrWatcherLost)
	recvClosed(t, subY)
	require.ErrorIs(t, subY.Err(), ErrWatcherLost)

	// The frozen projection is not served as live.
	require.False(t, h.view.CaughtUp())
	_, _, err = h.view.Get("k1")
	require.ErrorIs(t, err, ErrWatcherLost)
	require.ErrorIs(t, err, ErrNotReady, "fail-closed is a not-ready state")

	// New attaches observe not-ready semantics carrying the loss.
	_, _, err = h.view.SnapshotAndSubscribe(context.Background())
	require.ErrorIs(t, err, ErrNotReady)
	require.ErrorIs(t, err, ErrWatcherLost)

	// WaitCaughtUp reports the loss instead of blocking forever.
	err = h.view.WaitCaughtUp(context.Background())
	require.ErrorIs(t, err, ErrWatcherLost)
}

// TestRestartReconcilesGhostKeys (G5): after a watcher loss, re-bootstrap
// reconciles the retained projection against the fresh replay — a key whose
// delete (and purge-cleaned tombstone) happened while the watcher was down
// is REMOVED before caught-up is reported again, and never appears in
// post-recovery snapshots or point reads.
func TestRestartReconcilesGhostKeys(t *testing.T) {
	h := newHarness(t, 1) // one spare watcher queued for the restart

	h.put("ghost", "g1", 1)
	h.put("kept", "k1", 2)
	h.endReplay()

	// Loss. "ghost" is deleted AND purge-cleaned while the watcher is down,
	// so the fresh replay simply never mentions it.
	close(h.w.updates)
	require.Error(t, h.waitLost())

	require.NoError(t, h.view.Restart())
	require.False(t, h.view.CaughtUp(), "restart re-enters bootstrap; caught-up only after reconcile")

	// Fresh replay delivers only the surviving key (updated) and completes.
	h.w = h.src.lastIssued() // the restart consumed the queued spare
	h.put("kept", "k2", 3)
	h.endReplay()

	// Ghost removed BEFORE caught-up was reported: absent from snapshots
	// and point reads; the survivor carries the fresh replay's value.
	snap, sub, err := h.view.SnapshotAndSubscribe(context.Background())
	require.NoError(t, err)
	require.NotContains(t, snap.Entries, "ghost", "ghost key must be reconciled away on re-bootstrap")
	require.Equal(t, "k2", snap.Entries["kept"].Value)
	_, _, err = h.view.Get("ghost")
	require.ErrorIs(t, err, ErrKeyNotFound)

	// The recovered view serves live deltas again.
	h.put("new", "n1", 4)
	h.tickNow()
	batch := recvBatch(t, sub)
	require.Len(t, batch, 1)
	require.Equal(t, "new", batch[0].Key)
	sub.Unsubscribe()
}

// TestRestartRequiresFailedView: restart is only meaningful from the
// fail-closed state; a live or stopped view refuses it.
func TestRestartRequiresFailedView(t *testing.T) {
	h := newHarness(t, 0)
	h.endReplay()
	require.Error(t, h.view.Restart(), "restart of a live view must be refused")

	h.view.Stop()
	require.ErrorIs(t, h.view.Restart(), ErrViewStopped)
}
