package graphview

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestNewValidatesArguments: the view is explicitly constructed; nil
// injections are refused up front, not discovered at Start.
func TestNewValidatesArguments(t *testing.T) {
	_, err := New[string](nil, decodeTest)
	require.Error(t, err)
	_, err = New[string](&fakeSource{}, nil)
	require.Error(t, err)
}

// TestStartLifecycleGuards: double Start is refused; Start after Stop is
// refused; Stop is idempotent.
func TestStartLifecycleGuards(t *testing.T) {
	h := newHarness(t, 0)
	require.ErrorIs(t, h.view.Start(context.Background()), ErrAlreadyStarted)
	h.view.Stop()
	h.view.Stop() // idempotent
	require.ErrorIs(t, h.view.Start(context.Background()), ErrViewStopped)
	_, _, err := h.view.Get("k")
	require.ErrorIs(t, err, ErrViewStopped)
	require.ErrorIs(t, h.view.WaitCaughtUp(context.Background()), ErrViewStopped)
}

// TestUnsubscribeReleasesState: detach removes the subscriber from the
// fan-out set and releases its buffered deltas; the channel closes cleanly
// (Err() == nil) and subsequent ticks retain nothing for it.
func TestUnsubscribeReleasesState(t *testing.T) {
	h := newHarness(t, 0)
	h.endReplay()

	_, sub, err := h.view.SnapshotAndSubscribe(context.Background())
	require.NoError(t, err)

	// Leave an undrained pending delta behind, then detach.
	h.put("K", "v1", 1)
	h.tickNow()
	sub.Unsubscribe()
	recvClosed(t, sub)
	require.NoError(t, sub.Err(), "clean detach reports no terminal error")

	sub.mu.Lock()
	require.Nil(t, sub.pending, "detach must release the subscriber's buffered state")
	sub.mu.Unlock()

	h.view.mu.Lock()
	require.Empty(t, h.view.subs, "detached subscriber must leave the fan-out set")
	h.view.mu.Unlock()

	// Later windows fan out to zero subscribers without retaining state.
	h.put("K", "v2", 2)
	r := h.tickNow()
	require.Equal(t, 0, r.subs)
	h.view.mu.Lock()
	require.Empty(t, h.view.pending, "detached window is dropped, not retained")
	h.view.mu.Unlock()

	// Unsubscribe is idempotent.
	sub.Unsubscribe()
}

// TestSubscriberContextCancelDetaches: cancelling the subscription context
// detaches exactly like Unsubscribe, with the context error as the reason.
func TestSubscriberContextCancelDetaches(t *testing.T) {
	h := newHarness(t, 0)
	h.endReplay()

	ctx, cancel := context.WithCancel(context.Background())
	sub, err := h.view.Subscribe(ctx)
	require.NoError(t, err)
	cancel()
	recvClosed(t, sub)
	require.ErrorIs(t, sub.Err(), context.Canceled)

	h.view.mu.Lock()
	require.Empty(t, h.view.subs)
	h.view.mu.Unlock()
}

// TestStopDeliversTerminalClose: view shutdown terminates every subscription
// with an explicit terminal signal — never a silent hang — including a
// subscriber that is not draining.
func TestStopDeliversTerminalClose(t *testing.T) {
	h := newHarness(t, 0)
	h.endReplay()

	_, subA, err := h.view.SnapshotAndSubscribe(context.Background())
	require.NoError(t, err)
	subB, err := h.view.Subscribe(context.Background())
	require.NoError(t, err)

	// subA has an undrained window in flight when Stop lands.
	h.put("K", "v1", 1)
	h.tickNow()

	h.view.Stop()
	recvClosed(t, subA)
	require.ErrorIs(t, subA.Err(), ErrViewStopped)
	recvClosed(t, subB)
	require.ErrorIs(t, subB.Err(), ErrViewStopped)
}

// TestGetAndListSurface: coherent point reads and deterministic bounded
// listing over the same projection the delta stream serves.
func TestGetAndListSurface(t *testing.T) {
	h := newHarness(t, 0)
	h.put("a.1", "v1", 1)
	h.put("a.2", "v2", 2)
	h.put("b.1", "v3", 3)
	h.endReplay()

	val, rev, err := h.view.Get("a.2")
	require.NoError(t, err)
	require.Equal(t, "v2", val)
	require.Equal(t, uint64(2), rev)

	_, _, err = h.view.Get("missing")
	require.ErrorIs(t, err, ErrKeyNotFound)

	all, err := h.view.List("", 0)
	require.NoError(t, err)
	require.Len(t, all, 3)
	require.Equal(t, []string{"a.1", "a.2", "b.1"}, []string{all[0].Key, all[1].Key, all[2].Key},
		"complete result set is sorted deterministically")

	prefixed, err := h.view.List("a.", 0)
	require.NoError(t, err)
	require.Len(t, prefixed, 2)

	limited, err := h.view.List("", 2)
	require.NoError(t, err)
	require.Len(t, limited, 2)
	require.Equal(t, "a.1", limited[0].Key, "sort happens before the limit")
}

// TestSkippedKeyMapsToAbsence: keep=false decodes are not part of the view —
// a skipped write on a previously-kept key converges subscribers to absence
// via a tombstone at that revision; a skipped write on an unknown key only
// advances the watermark.
func TestSkippedKeyMapsToAbsence(t *testing.T) {
	h := newHarness(t, 0)
	h.put("K", "v1", 1)
	h.endReplay()

	snap, sub, err := h.view.SnapshotAndSubscribe(context.Background())
	require.NoError(t, err)
	tr := newTracker(t)
	tr.loadSnapshot(snap)

	// Unknown key skipped: watermark advances, nothing delivered.
	h.put("meta", "SKIP", 2)
	require.Equal(t, uint64(2), h.view.AppliedRevision())
	r := h.tickNow()
	require.Equal(t, 0, r.changed)

	// Previously-kept key turns not-ready: subscribers converge to absence.
	h.put("K", "SKIP", 3)
	h.tickNow()
	batch := recvBatch(t, sub)
	tr.apply(batch)
	require.Len(t, batch, 1)
	require.Equal(t, DeltaDelete, batch[0].Op)
	require.Equal(t, uint64(3), batch[0].Revision)
	_, _, err = h.view.Get("K")
	require.ErrorIs(t, err, ErrKeyNotFound)
	sub.Unsubscribe()
}

// TestDeltaOnlySubscribeIsTriggerShaped: a delta-only attach carries no
// snapshot and receives exactly the changes after its attach sequence.
func TestDeltaOnlySubscribeIsTriggerShaped(t *testing.T) {
	h := newHarness(t, 0)
	h.put("pre", "v", 1)
	h.endReplay()

	sub, err := h.view.Subscribe(context.Background())
	require.NoError(t, err)

	// A pre-attach change is not replayed to a delta-only subscriber.
	h.put("post", "v", 2)
	h.tickNow()
	batch := recvBatch(t, sub)
	require.Len(t, batch, 1)
	require.Equal(t, "post", batch[0].Key)
	sub.Unsubscribe()
}
