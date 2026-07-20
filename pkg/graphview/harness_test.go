package graphview

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
)

// testWait bounds channel reads on FAILURE paths only — no assertion in this
// suite sleeps for it; every synchronization point is an explicit channel.
const testWait = 5 * time.Second

// fakeEntry implements jetstream.KeyValueEntry deterministically.
type fakeEntry struct {
	key     string
	value   []byte
	rev     uint64
	op      jetstream.KeyValueOp
	created time.Time
}

func (e fakeEntry) Bucket() string                  { return "FAKE" }
func (e fakeEntry) Key() string                     { return e.key }
func (e fakeEntry) Value() []byte                   { return e.value }
func (e fakeEntry) Revision() uint64                { return e.rev }
func (e fakeEntry) Created() time.Time              { return e.created }
func (e fakeEntry) Delta() uint64                   { return 0 }
func (e fakeEntry) Operation() jetstream.KeyValueOp { return e.op }

// fakeWatcher is a deterministic KeyWatcher: tests push entries (nil = the
// end-of-replay marker) and close(updates) to simulate watcher loss.
type fakeWatcher struct {
	updates chan jetstream.KeyValueEntry
}

func newFakeWatcher() *fakeWatcher {
	return &fakeWatcher{updates: make(chan jetstream.KeyValueEntry, 256)}
}

func (w *fakeWatcher) Updates() <-chan jetstream.KeyValueEntry { return w.updates }
func (w *fakeWatcher) Stop() error                             { return nil }

// fakeSource hands out queued fake watchers: one per WatchAll call (Start,
// then each Restart).
type fakeSource struct {
	mu     sync.Mutex
	queue  []*fakeWatcher
	issued []*fakeWatcher
}

func (s *fakeSource) WatchAll(_ context.Context, _ ...jetstream.WatchOpt) (jetstream.KeyWatcher, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.queue) == 0 {
		return nil, errors.New("fake source: no watcher queued")
	}
	w := s.queue[0]
	s.queue = s.queue[1:]
	s.issued = append(s.issued, w)
	return w, nil
}

// lastIssued returns the watcher most recently handed to the view (the
// restart generation's watcher after a Restart).
func (s *fakeSource) lastIssued() *fakeWatcher {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.issued[len(s.issued)-1]
}

// decodeTest is the injected validating decode for unit tests:
// "SKIP" -> keep=false; "POISON..." -> contract error; else the string value.
func decodeTest(_ string, value []byte, _ EntryMeta) (string, bool, error) {
	s := string(value)
	switch {
	case strings.HasPrefix(s, "POISON"):
		return "", false, errors.New("test contract violation: " + s)
	case s == "SKIP":
		return "", false, nil
	default:
		return s, true, nil
	}
}

type tickResult struct {
	changed int
	subs    int
}

// harness wires a View[string] to a fake watcher with a caller-driven tick
// channel and hook channels for explicit synchronization (no sleeps).
type harness struct {
	t    *testing.T
	view *View[string]
	src  *fakeSource
	w    *fakeWatcher
	tick chan time.Time

	applied chan uint64
	ticked  chan tickResult
	lost    chan error
}

// newHarness starts a view over extraWatchers+1 queued fake watchers and
// registers Stop as cleanup. The first watcher is h.w; restarts pop the next.
func newHarness(t *testing.T, extraWatchers int) *harness {
	t.Helper()
	h := &harness{
		t:       t,
		src:     &fakeSource{},
		tick:    make(chan time.Time),
		applied: make(chan uint64, 4096),
		ticked:  make(chan tickResult, 64),
		lost:    make(chan error, 4),
	}
	h.w = newFakeWatcher()
	h.src.queue = []*fakeWatcher{h.w}
	for range extraWatchers {
		h.src.queue = append(h.src.queue, newFakeWatcher())
	}
	hooks := Hooks{
		OnApply:       func(_ string, rev uint64) { h.applied <- rev },
		OnTick:        func(changed, subs int) { h.ticked <- tickResult{changed: changed, subs: subs} },
		OnWatcherLost: func(err error) { h.lost <- err },
	}
	view, err := New[string](h.src, decodeTest, WithHooks(hooks), withTickChannel(h.tick))
	require.NoError(t, err)
	require.NoError(t, view.Start(context.Background()))
	h.view = view
	t.Cleanup(view.Stop)
	return h
}

func (h *harness) send(e jetstream.KeyValueEntry) {
	h.t.Helper()
	select {
	case h.w.updates <- e:
	case <-time.After(testWait):
		h.t.Fatal("send: fake watcher channel full")
	}
}

func (h *harness) waitApplied() uint64 {
	h.t.Helper()
	select {
	case rev := <-h.applied:
		return rev
	case <-time.After(testWait):
		h.t.Fatal("timed out waiting for apply")
		return 0
	}
}

// put sends a Put entry and waits until the view applied it.
func (h *harness) put(key, val string, rev uint64) {
	h.t.Helper()
	h.send(fakeEntry{key: key, value: []byte(val), rev: rev, op: jetstream.KeyValuePut})
	h.waitApplied()
}

// del sends a Delete tombstone and waits until the view applied it.
func (h *harness) del(key string, rev uint64) {
	h.t.Helper()
	h.send(fakeEntry{key: key, rev: rev, op: jetstream.KeyValueDelete})
	h.waitApplied()
}

// purge sends a Purge tombstone and waits until the view applied it.
func (h *harness) purge(key string, rev uint64) {
	h.t.Helper()
	h.send(fakeEntry{key: key, rev: rev, op: jetstream.KeyValuePurge})
	h.waitApplied()
}

// endReplay sends the end-of-replay marker and waits for caught-up.
func (h *harness) endReplay() {
	h.t.Helper()
	h.send(nil)
	ctx, cancel := context.WithTimeout(context.Background(), testWait)
	defer cancel()
	require.NoError(h.t, h.view.WaitCaughtUp(ctx))
}

// tickNow fires one coalescing window and waits until fan-out completed
// (enqueue into every subscriber buffer done — delivery itself is async).
func (h *harness) tickNow() tickResult {
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

// waitLost blocks until the watcher-loss hook fired.
func (h *harness) waitLost() error {
	h.t.Helper()
	select {
	case err := <-h.lost:
		return err
	case <-time.After(testWait):
		h.t.Fatal("timed out waiting for watcher loss")
		return nil
	}
}

// recvBatch reads one delta batch or fails the test.
func recvBatch(t *testing.T, sub *Subscription[string]) []Delta[string] {
	t.Helper()
	select {
	case b, ok := <-sub.Deltas():
		require.True(t, ok, "deltas channel closed while expecting a batch (Err: %v)", sub.Err())
		return b
	case <-time.After(testWait):
		t.Fatal("timed out waiting for delta batch")
		return nil
	}
}

// recvClosed drains the subscription until its channel closes, returning
// every batch received on the way.
func recvClosed(t *testing.T, sub *Subscription[string]) [][]Delta[string] {
	t.Helper()
	var out [][]Delta[string]
	for {
		select {
		case b, ok := <-sub.Deltas():
			if !ok {
				return out
			}
			out = append(out, b)
		case <-time.After(testWait):
			t.Fatal("timed out waiting for terminal close")
			return nil
		}
	}
}

// tracker materializes a subscriber's snapshot+delta stream and asserts the
// per-key revision high-water NEVER regresses — the coherence invariant every
// delivery in this suite is held to (G2/G3/G4).
type tracker struct {
	t      *testing.T
	values map[string]string
	revs   map[string]uint64
}

func newTracker(t *testing.T) *tracker {
	return &tracker{t: t, values: make(map[string]string), revs: make(map[string]uint64)}
}

func (tr *tracker) loadSnapshot(snap Snapshot[string]) {
	tr.t.Helper()
	for k, e := range snap.Entries {
		tr.values[k] = e.Value
		tr.revs[k] = e.Revision
	}
	for k, p := range snap.Poisoned {
		delete(tr.values, k)
		tr.revs[k] = p.Revision
	}
}

func (tr *tracker) apply(batch []Delta[string]) {
	tr.t.Helper()
	for _, d := range batch {
		require.Greater(tr.t, d.Revision, tr.revs[d.Key],
			"delta %s %s@%d regresses the subscriber's high-water %d — stale delivery",
			d.Op, d.Key, d.Revision, tr.revs[d.Key])
		tr.revs[d.Key] = d.Revision
		switch d.Op {
		case DeltaUpsert:
			tr.values[d.Key] = d.Value
		case DeltaDelete, DeltaPoison:
			delete(tr.values, d.Key)
		}
	}
}
