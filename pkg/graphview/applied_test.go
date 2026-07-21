package graphview

import (
	"context"
	"fmt"
	"runtime"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// putAt sends a Put carrying an explicit KV server write time and waits until
// the view applied it, so a test can assert the (revision, appliedAt) pair.
// The shared harness.put deliberately leaves Created zero — these tests are
// the ones that care about it.
func (h *harness) putAt(key, val string, rev uint64, created time.Time) {
	h.t.Helper()
	h.send(fakeEntry{key: key, value: []byte(val), rev: rev, op: jetstream.KeyValuePut, created: created})
	h.waitApplied()
}

// TestAppliedIsZeroBeforeFirstApply: currency is UNKNOWN, not fresh, before
// anything has been applied. The accessor reports the zero time rather than
// fabricating time.Now() — the same "not computable" contract as
// IndexStatusInputs.IndexedAt, whose projection declines to make an
// uncomputable view look current.
func TestAppliedIsZeroBeforeFirstApply(t *testing.T) {
	h := newHarness(t, 0)

	rev, at := h.view.Applied()
	require.Equal(t, uint64(0), rev)
	require.True(t, at.IsZero(), "no applied write means no timestamp, never a fabricated now")

	// Still zero after the bootstrap gate lifts on an empty bucket: caught-up
	// is not evidence of currency.
	h.endReplay()
	rev, at = h.view.Applied()
	require.Equal(t, uint64(0), rev)
	require.True(t, at.IsZero())
}

// TestAppliedAdvancesWithTheRevisionWatermark: appliedAt moves in exactly the
// places and under exactly the condition the revision watermark moves — every
// op kind that advances the watermark carries its timestamp forward, and an
// op that does not advance the watermark does not move the timestamp either.
// The pair therefore always names ONE write.
func TestAppliedAdvancesWithTheRevisionWatermark(t *testing.T) {
	h := newHarness(t, 0)
	h.endReplay()

	t1 := time.Date(2026, 7, 1, 9, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 7, 1, 9, 1, 0, 0, time.UTC)
	t3 := time.Date(2026, 7, 1, 9, 2, 0, 0, time.UTC)
	t4 := time.Date(2026, 7, 1, 9, 3, 0, 0, time.UTC)

	// Upsert advances both.
	h.putAt("K", "v1", 5, t1)
	rev, at := h.view.Applied()
	require.Equal(t, uint64(5), rev)
	require.True(t, at.Equal(t1), "upsert must carry its write time onto the watermark")

	// A poison write is applied like any other op: the value is unservable,
	// but the revision was still applied, so currency advances.
	h.putAt("P", "POISON-bad", 6, t2)
	rev, at = h.view.Applied()
	require.Equal(t, uint64(6), rev)
	require.True(t, at.Equal(t2), "a poisoned write still advances the applied watermark")

	// keep=false on an unknown key advances the watermark only (applySkip's
	// early return sits BELOW the advance) — the timestamp must follow.
	h.putAt("meta", "SKIP", 7, t3)
	rev, at = h.view.Applied()
	require.Equal(t, uint64(7), rev)
	require.True(t, at.Equal(t3), "a skipped write advances currency like any other applied revision")

	// Tombstones advance both.
	h.send(fakeEntry{key: "K", rev: 8, op: jetstream.KeyValueDelete, created: t4})
	h.waitApplied()
	rev, at = h.view.Applied()
	require.Equal(t, uint64(8), rev)
	require.True(t, at.Equal(t4), "a tombstone advances currency")

	// AND ONLY WITH IT. A non-advancing revision must not drag the timestamp
	// with it — neither backwards nor (worse) forwards onto a stale revision.
	// Ordered WatchAll delivery makes these deliveries impossible in
	// production; the assertion pins the coupling, not the delivery order.
	stale := time.Date(2026, 7, 1, 23, 0, 0, 0, time.UTC)
	h.putAt("K", "v-old", 3, stale)
	rev, at = h.view.Applied()
	require.Equal(t, uint64(8), rev, "a lower revision does not advance the watermark")
	require.True(t, at.Equal(t4), "a lower revision must not move the timestamp either")

	h.putAt("K", "v-same", 8, stale)
	rev, at = h.view.Applied()
	require.Equal(t, uint64(8), rev)
	require.True(t, at.Equal(t4), "an equal revision is not an advance; the timestamp holds")
}

// TestAppliedSurvivesLifecycleTransitions: appliedAt has NO reset semantics of
// its own — it survives exactly the transitions the revision watermark
// survives (bootstrap gating, fail-closed watcher loss, re-bootstrap, Stop).
// Every checkpoint asserts the timestamp against the same-instant revision, so
// the two can never drift apart in lifecycle handling.
func TestAppliedSurvivesLifecycleTransitions(t *testing.T) {
	h := newHarness(t, 1) // one spare watcher queued for the restart

	t1 := time.Date(2026, 7, 1, 9, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 7, 1, 9, 5, 0, 0, time.UTC)

	// Mid-bootstrap: the view is GATED but the watermark advances, and so does
	// its timestamp — currency is a report, not a gate.
	h.putAt("k1", "a", 1, t1)
	require.False(t, h.view.CaughtUp())
	rev, at := h.view.Applied()
	require.Equal(t, h.view.AppliedRevision(), rev)
	require.Equal(t, uint64(1), rev)
	require.True(t, at.Equal(t1), "the timestamp advances during gated replay, like the revision")

	// Caught-up changes neither.
	h.endReplay()
	rev, at = h.view.Applied()
	require.Equal(t, uint64(1), rev)
	require.True(t, at.Equal(t1))

	// Watcher loss (fail closed): the projection freezes and neither half of
	// the pair is reset — the frozen view's currency is exactly what it was.
	close(h.w.updates)
	require.Error(t, h.waitLost())
	rev, at = h.view.Applied()
	require.Equal(t, h.view.AppliedRevision(), rev)
	require.Equal(t, uint64(1), rev, "fail-closed does not reset the watermark")
	require.True(t, at.Equal(t1), "fail-closed does not reset the timestamp either")

	// Re-bootstrap: still retained while the fresh replay runs.
	require.NoError(t, h.view.Restart())
	rev, at = h.view.Applied()
	require.Equal(t, uint64(1), rev)
	require.True(t, at.Equal(t1))

	// The fresh replay advances both again.
	h.w = h.src.lastIssued()
	h.putAt("k1", "b", 2, t2)
	h.endReplay()
	rev, at = h.view.Applied()
	require.Equal(t, h.view.AppliedRevision(), rev)
	require.Equal(t, uint64(2), rev)
	require.True(t, at.Equal(t2))

	// Stop keeps the pair readable and unreset: it is a diagnostic surface
	// served in every state, exactly like AppliedRevision.
	h.view.Stop()
	rev, at = h.view.Applied()
	require.Equal(t, h.view.AppliedRevision(), rev)
	require.Equal(t, uint64(2), rev, "Stop does not reset the watermark")
	require.True(t, at.Equal(t2), "Stop does not reset the timestamp")
}

// TestSnapshotCarriesAppliedAt: a subscriber is handed how current the
// snapshot it just received was. The value is captured in the same critical
// section as Entries/Sequence/Revision, so it matches the view's own pair at
// attach time and does not move when later writes land.
func TestSnapshotCarriesAppliedAt(t *testing.T) {
	h := newHarness(t, 0)

	tAttach := time.Date(2026, 7, 1, 9, 0, 0, 0, time.UTC)
	tLater := time.Date(2026, 7, 1, 9, 30, 0, 0, time.UTC)

	h.putAt("k1", "a", 4, tAttach)
	h.endReplay()

	wantRev, wantAt := h.view.Applied()
	snap, sub, err := h.view.SnapshotAndSubscribe(context.Background())
	require.NoError(t, err)
	require.Equal(t, wantRev, snap.Revision)
	require.True(t, snap.AppliedAt.Equal(wantAt), "snapshot must carry the view's applied timestamp at capture")
	require.True(t, snap.AppliedAt.Equal(tAttach))

	// A later write advances the view; the already-captured snapshot keeps the
	// currency it was actually taken at.
	h.putAt("k2", "b", 9, tLater)
	rev, at := h.view.Applied()
	require.Equal(t, uint64(9), rev)
	require.True(t, at.Equal(tLater))
	require.Equal(t, uint64(4), snap.Revision, "the captured snapshot does not move")
	require.True(t, snap.AppliedAt.Equal(tAttach), "the captured snapshot's currency does not move")

	sub.Unsubscribe()
}

// TestSnapshotAppliedAtIsZeroOnEmptyView: the snapshot inherits the "not
// computable" encoding rather than a fabricated capture-time clock.
func TestSnapshotAppliedAtIsZeroOnEmptyView(t *testing.T) {
	h := newHarness(t, 0)
	h.endReplay()

	snap, sub, err := h.view.SnapshotAndSubscribe(context.Background())
	require.NoError(t, err)
	require.Equal(t, uint64(0), snap.Revision)
	require.True(t, snap.AppliedAt.IsZero(), "an empty view's snapshot reports unknown currency, not now()")
	sub.Unsubscribe()
}

// TestAppliedPairIsNeverTorn: the revision and its timestamp come out of ONE
// critical section, so a reader concurrent with a write storm can never
// observe a revision from one apply paired with a timestamp from another.
//
// The test makes a torn pair DETECTABLE rather than merely improbable: each
// write's server timestamp is a pure function of its revision, so any
// mismatch is a tear. Run under -race this also proves the read is
// synchronized rather than merely lucky.
func TestAppliedPairIsNeverTorn(t *testing.T) {
	h := newHarness(t, 0)
	h.endReplay()

	base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	createdFor := func(rev uint64) time.Time { return base.Add(time.Duration(rev) * time.Millisecond) }

	const writes = 200
	stop := make(chan struct{})
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		for {
			rev, at := h.view.Applied()
			if rev == 0 {
				assert.True(t, at.IsZero(), "zero watermark must carry no timestamp")
			} else {
				assert.Truef(t, at.Equal(createdFor(rev)),
					"torn pair: revision %d paired with %v, want %v", rev, at, createdFor(rev))
			}
			select {
			case <-stop:
				return
			default:
			}
			// Yield so the watcher goroutine is not starved by the read loop.
			runtime.Gosched()
		}
	}()

	for rev := uint64(1); rev <= writes; rev++ {
		h.send(fakeEntry{
			key:     fmt.Sprintf("k%d", rev),
			value:   []byte("v"),
			rev:     rev,
			op:      jetstream.KeyValuePut,
			created: createdFor(rev),
		})
	}
	for range writes {
		h.waitApplied()
	}
	close(stop)
	<-readerDone

	rev, at := h.view.Applied()
	require.Equal(t, uint64(writes), rev)
	require.True(t, at.Equal(createdFor(writes)))
}
