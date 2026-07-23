package graphview

import (
	"context"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
)

// hookHarness is a purpose-built setup for the observability-hook tests
// (build task 2.5): it installs OnSubscribers/OnFanOut channels alongside
// the apply/tick synchronization channels, leaving the shared harness (and
// every existing test that uses it) untouched.
type hookHarness struct {
	t    *testing.T
	view *View[string]
	w    *fakeWatcher
	tick chan time.Time

	applied     chan uint64
	ticked      chan tickResult
	subscribers chan int
	fanouts     chan [2]int // {overwritten, maxPending}
	lost        chan error
}

func newHookHarness(t *testing.T) *hookHarness {
	t.Helper()
	h := &hookHarness{
		t:           t,
		tick:        make(chan time.Time),
		applied:     make(chan uint64, 4096),
		ticked:      make(chan tickResult, 64),
		subscribers: make(chan int, 64),
		fanouts:     make(chan [2]int, 64),
		lost:        make(chan error, 4),
	}
	h.w = newFakeWatcher()
	src := &fakeSource{queue: []*fakeWatcher{h.w}}
	hooks := Hooks{
		OnApply:       func(_ string, rev uint64) { h.applied <- rev },
		OnTick:        func(changed, subs int) { h.ticked <- tickResult{changed: changed, subs: subs} },
		OnSubscribers: func(n int) { h.subscribers <- n },
		OnFanOut:      func(overwritten, maxPending int) { h.fanouts <- [2]int{overwritten, maxPending} },
		OnWatcherLost: func(err error) { h.lost <- err },
	}
	view, err := New[string](src, decodeTest, WithHooks(hooks), withTickChannel(h.tick))
	require.NoError(t, err)
	require.NoError(t, view.Start(context.Background()))
	h.view = view
	t.Cleanup(view.Stop)
	return h
}

func (h *hookHarness) put(key, val string, rev uint64) {
	h.t.Helper()
	select {
	case h.w.updates <- fakeEntry{key: key, value: []byte(val), rev: rev, op: jetstream.KeyValuePut}:
	case <-time.After(testWait):
		h.t.Fatal("put: fake watcher channel full")
	}
	select {
	case <-h.applied:
	case <-time.After(testWait):
		h.t.Fatal("timed out waiting for apply")
	}
}

func (h *hookHarness) endReplay() {
	h.t.Helper()
	select {
	case h.w.updates <- nil:
	case <-time.After(testWait):
		h.t.Fatal("endReplay: fake watcher channel full")
	}
	ctx, cancel := context.WithTimeout(context.Background(), testWait)
	defer cancel()
	require.NoError(h.t, h.view.WaitCaughtUp(ctx))
}

func (h *hookHarness) tickNow() tickResult {
	h.t.Helper()
	select {
	case h.tick <- time.Time{}:
	case <-time.After(testWait):
		h.t.Fatal("timed out delivering tick")
	}
	select {
	case r := <-h.ticked:
		return r
	case <-time.After(testWait):
		h.t.Fatal("timed out waiting for tick fan-out")
		return tickResult{}
	}
}

func (h *hookHarness) waitSubscribers() int {
	h.t.Helper()
	select {
	case n := <-h.subscribers:
		return n
	case <-time.After(testWait):
		h.t.Fatal("timed out waiting for OnSubscribers")
		return -1
	}
}

// takeFanOut returns the OnFanOut report for a window that had changes and
// subscribers, or nil if none fired (empty/zero-subscriber windows).
func (h *hookHarness) takeFanOut() *[2]int {
	select {
	case f := <-h.fanouts:
		return &f
	default:
		return nil
	}
}

// waitFanOut blocks for the OnFanOut report of a window the caller KNOWS
// fired one (changed keys AND at least one subscriber). Unlike takeFanOut it
// does not race the hook: OnFanOut fires immediately after OnTick on the
// ticker goroutine, so a non-blocking read taken the instant tickNow returns
// (having only consumed OnTick) can miss a report that is still in flight.
func (h *hookHarness) waitFanOut() [2]int {
	h.t.Helper()
	select {
	case f := <-h.fanouts:
		return f
	case <-time.After(testWait):
		h.t.Fatal("timed out waiting for OnFanOut")
		return [2]int{}
	}
}

// TestOnSubscribersFiresOnAttachAndDetach: the subscriber-count hook fires
// with the new count on every attach (both modes) and every detach path —
// Unsubscribe, subscription-context cancel — so a gauge wired to it tracks
// the live fan-out set.
func TestOnSubscribersFiresOnAttachAndDetach(t *testing.T) {
	h := newHookHarness(t)
	h.endReplay()

	_, subA, err := h.view.SnapshotAndSubscribe(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, h.waitSubscribers())

	subB, err := h.view.Subscribe(context.Background())
	require.NoError(t, err)
	require.Equal(t, 2, h.waitSubscribers())

	ctxC, cancelC := context.WithCancel(context.Background())
	subC, err := h.view.Subscribe(ctxC)
	require.NoError(t, err)
	require.Equal(t, 3, h.waitSubscribers())

	subA.Unsubscribe()
	require.Equal(t, 2, h.waitSubscribers())

	// Context cancellation detaches like Unsubscribe and reports the count.
	cancelC()
	recvClosed(t, subC)
	require.Equal(t, 1, h.waitSubscribers())

	// Idempotent detach does not re-fire.
	subA.Unsubscribe()
	require.Len(t, h.subscribers, 0, "idempotent Unsubscribe must not re-fire OnSubscribers")

	subB.Unsubscribe()
	require.Equal(t, 0, h.waitSubscribers())
}

// TestOnSubscribersReportsZeroOnStop: view shutdown clears the fan-out set
// and reports zero — the terminal gauge value matches the terminal state.
func TestOnSubscribersReportsZeroOnStop(t *testing.T) {
	h := newHookHarness(t)
	h.endReplay()

	sub, err := h.view.Subscribe(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, h.waitSubscribers())

	h.view.Stop()
	recvClosed(t, sub)
	require.ErrorIs(t, sub.Err(), ErrViewStopped)
	require.Equal(t, 0, h.waitSubscribers())
}

// TestOnSubscribersReportsZeroOnWatcherLoss: fail-closed terminates every
// subscription; the count hook reports zero alongside OnWatcherLost so the
// staleness gauge and the subscriber gauge cannot disagree.
func TestOnSubscribersReportsZeroOnWatcherLoss(t *testing.T) {
	h := newHookHarness(t)
	h.endReplay()

	sub, err := h.view.Subscribe(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, h.waitSubscribers())

	close(h.w.updates)
	select {
	case err := <-h.lost:
		require.Error(t, err)
	case <-time.After(testWait):
		t.Fatal("timed out waiting for watcher loss")
	}
	recvClosed(t, sub)
	require.ErrorIs(t, sub.Err(), ErrWatcherLost)
	require.Equal(t, 0, h.waitSubscribers())
}

// TestOnFanOutReportsPendingSize: a fresh subscriber's first window enqueues
// into an empty buffer — maxPending is exactly the changed-key cardinality
// and nothing was overwritten. Empty windows and zero-subscriber windows
// fire no OnFanOut.
func TestOnFanOutReportsPendingSize(t *testing.T) {
	h := newHookHarness(t)
	h.endReplay()

	// Zero-subscriber window with changes: no OnFanOut.
	h.put("k0", "v", 1)
	r := h.tickNow()
	require.Equal(t, 1, r.changed)
	require.Equal(t, 0, r.subs)
	require.Nil(t, h.takeFanOut(), "zero-subscriber window must not report fan-out stats")

	_, sub, err := h.view.SnapshotAndSubscribe(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, h.waitSubscribers())

	// Empty window: no OnFanOut.
	r = h.tickNow()
	require.Equal(t, 0, r.changed)
	require.Nil(t, h.takeFanOut(), "empty window must not report fan-out stats")

	// Three keys in one window into an empty (never-notified) buffer:
	// deterministic maxPending == 3, overwritten == 0.
	h.put("k1", "a", 2)
	h.put("k2", "b", 3)
	h.put("k3", "c", 4)
	r = h.tickNow()
	require.Equal(t, 3, r.changed)
	f := h.takeFanOut()
	require.NotNil(t, f, "window with changes and a subscriber must report fan-out stats")
	require.Equal(t, 0, f[0], "no undrained delta existed to overwrite")
	require.Equal(t, 3, f[1], "maxPending is the subscriber's buffer size after enqueue")
	sub.Unsubscribe()
}

// TestOnFanOutCountsOverwrites: an undrained subscriber's pending delta being
// replaced by a newer one for the same key is the at-most-once drop — the
// slow-client staleness signal. Exact interleaving of the delivery
// goroutine's takes is unobservable, so the assertion is cumulative: with
// four single-key windows and a consumer that never reads, at least one
// overwrite MUST be reported (buffer + in-flight capacity is three), and
// after draining the subscriber converges to the newest revision.
func TestOnFanOutCountsOverwrites(t *testing.T) {
	h := newHookHarness(t)
	h.endReplay()

	_, sub, err := h.view.SnapshotAndSubscribe(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, h.waitSubscribers())

	overwritten := 0
	for rev := uint64(1); rev <= 4; rev++ {
		h.put("K", "v"+string(rune('0'+rev)), rev)
		r := h.tickNow()
		require.Equal(t, 1, r.changed)
		// Every window here has a changed key and a subscriber, so OnFanOut
		// is guaranteed to fire — block for it rather than racing the hook
		// with a non-blocking read (OnFanOut fires just after OnTick, which
		// is what tickNow already consumed).
		f := h.waitFanOut()
		overwritten += f[0]
		require.LessOrEqual(t, f[1], 1, "single-key windows can never buffer more than one pending delta")
	}
	require.GreaterOrEqual(t, overwritten, 1,
		"four undrained single-key windows must overwrite at least once (capacity is three)")

	// Drain: the subscriber converges to the newest revision, never regresses.
	tr := newTracker(t)
	for tr.revs["K"] < 4 {
		tr.apply(recvBatch(t, sub))
	}
	require.Equal(t, "v4", tr.values["K"])
	sub.Unsubscribe()
}
