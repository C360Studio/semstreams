package readiness

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/nats-io/nats.go/jetstream"
)

// Synchronization here is explicit, never timing-based: the fake watcher's Updates
// channel is UNBUFFERED (a send returns only once the watch goroutine has received
// it) and the applied hook fires once the entry has been folded into held state. The
// freshness clock is injected, so the expiry tests advance time instead of sleeping.

// fakeEntry is a KV entry. Only the accessors the watcher uses carry real values.
//
// created is the server-side commit stamp. The zero value means "unset", which the
// watcher treats as no commit-time information and therefore never stale-on-arrival —
// so tests that are not about that rule can leave it alone.
type fakeEntry struct {
	key     string
	value   []byte
	rev     uint64
	op      jetstream.KeyValueOp
	created time.Time
}

func (e fakeEntry) Bucket() string                  { return BucketGraphStatus }
func (e fakeEntry) Key() string                     { return e.key }
func (e fakeEntry) Value() []byte                   { return e.value }
func (e fakeEntry) Revision() uint64                { return e.rev }
func (e fakeEntry) Created() time.Time              { return e.created }
func (e fakeEntry) Delta() uint64                   { return 0 }
func (e fakeEntry) Operation() jetstream.KeyValueOp { return e.op }

// fakeWatcher is a jetstream.KeyWatcher over a test-driven channel.
type fakeWatcher struct {
	updates chan jetstream.KeyValueEntry
	stopped atomic.Bool
}

func (w *fakeWatcher) Updates() <-chan jetstream.KeyValueEntry { return w.updates }
func (w *fakeWatcher) Stop() error {
	w.stopped.Store(true)
	return nil
}

// fakeBucket embeds jetstream.KeyValue so it satisfies the interface without
// implementing 20 unused methods. ONLY Watch is ever called by the watcher — any
// other call would nil-panic, which is the intended loud failure if that changes.
type fakeBucket struct {
	jetstream.KeyValue
	watch func(ctx context.Context) (jetstream.KeyWatcher, error)
}

func (b *fakeBucket) Watch(ctx context.Context, _ string, _ ...jetstream.WatchOpt) (jetstream.KeyWatcher, error) {
	return b.watch(ctx)
}

// fakeSource hands out buckets and signals each bind attempt.
type fakeSource struct {
	open func(ctx context.Context) (jetstream.KeyValue, error)
	// opened is signalled (non-blocking) after every open attempt so a test can
	// wait for the bind rather than poll for it.
	opened chan struct{}
}

func (s *fakeSource) GetKeyValueBucket(ctx context.Context, _ string) (jetstream.KeyValue, error) {
	kv, err := s.open(ctx)
	select {
	case s.opened <- struct{}{}:
	default:
	}
	return kv, err
}

// testClock is a manually advanced clock for the freshness window.
type testClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *testClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *testClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func discardLogger() *slog.Logger { return slog.New(slog.DiscardHandler) }

func statusJSON(t *testing.T, s graph.IndexStatusResponse) []byte {
	t.Helper()
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal status: %v", err)
	}
	return b
}

// harness wires a watcher over a single fake bucket whose watch channel the test
// drives directly.
type harness struct {
	t       *testing.T
	watcher *Watcher
	clock   *testClock
	updates chan jetstream.KeyValueEntry
	applied chan struct{}
	source  *fakeSource
}

func newHarness(t *testing.T, opts ...Option) *harness {
	t.Helper()
	h := &harness{
		t:       t,
		clock:   &testClock{t: time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)},
		updates: make(chan jetstream.KeyValueEntry), // unbuffered: send == received
		applied: make(chan struct{}, 8),
	}
	h.source = &fakeSource{opened: make(chan struct{}, 8)}
	h.source.open = func(context.Context) (jetstream.KeyValue, error) {
		return &fakeBucket{watch: func(context.Context) (jetstream.KeyWatcher, error) {
			return &fakeWatcher{updates: h.updates}, nil
		}}, nil
	}

	base := []Option{
		WithLogger(discardLogger()),
		withClock(h.clock.now),
		withRebindDelay(time.Millisecond),
		withAppliedHook(h.applied),
	}
	h.watcher = NewWatcher(h.source, KeyGraphIndex, append(base, opts...)...)
	if err := h.watcher.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(h.watcher.Stop)
	return h
}

// deliver sends an entry and waits until the watcher has applied it.
func (h *harness) deliver(entry jetstream.KeyValueEntry) {
	h.t.Helper()
	select {
	case h.updates <- entry:
	case <-time.After(5 * time.Second):
		h.t.Fatal("watcher never received the entry")
	}
	select {
	case <-h.applied:
	case <-time.After(5 * time.Second):
		h.t.Fatal("watcher never applied the entry")
	}
}

func readyEntry(t *testing.T) fakeEntry {
	t.Helper()
	return fakeEntry{
		key: KeyGraphIndex,
		value: statusJSON(t, graph.IndexStatusResponse{
			Ready: true, State: graph.IndexStateReady,
			IndexedRevision: 500, TargetRevision: 500,
		}),
		rev: 1,
		op:  jetstream.KeyValuePut,
	}
}

// TestWatcher_CurrentValueIsFreshOnBind covers the spec scenario "a restarted
// consumer is fresh within one delivery": the watch is bound without UpdatesOnly, so
// the key's current value arrives immediately and the consumer is fresh without
// waiting a heartbeat.
func TestWatcher_CurrentValueIsFreshOnBind(t *testing.T) {
	h := newHarness(t)

	// Before any delivery the answer is UNKNOWN, never a fabricated envelope.
	if r := h.watcher.Read(); r.Known || r.Fresh || r.Status.Ready {
		t.Fatalf("pre-delivery read = %+v, want unknown with a zero envelope", r)
	}

	h.deliver(readyEntry(t))

	r := h.watcher.Read()
	if !r.Known || !r.Fresh {
		t.Fatalf("read after bind delivery = %+v, want known and fresh", r)
	}
	if !r.Status.Ready || r.Status.TargetRevision != 500 {
		t.Errorf("held envelope = %+v, want the delivered ready envelope", r.Status)
	}
	if r.Err != nil {
		t.Errorf("Err = %v, want nil after a good update", r.Err)
	}
}

// TestWatcher_FreshnessExpiresAtThreeHeartbeats pins the D2 window. The clock is
// injected, so this is exact rather than a timing race.
func TestWatcher_FreshnessExpiresAtThreeHeartbeats(t *testing.T) {
	const heartbeat = 5 * time.Second
	tests := []struct {
		name      string
		advance   time.Duration
		wantFresh bool
	}{
		{"immediately after arrival", 0, true},
		{"one heartbeat later", heartbeat, true},
		{"just inside three heartbeats", 3*heartbeat - time.Millisecond, true},
		{"exactly three heartbeats", 3 * heartbeat, true},
		{"one millisecond past three heartbeats", 3*heartbeat + time.Millisecond, false},
		{"long dead feed", time.Hour, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t, WithHeartbeat(heartbeat))
			h.deliver(readyEntry(t))
			h.clock.advance(tt.advance)

			r := h.watcher.Read()
			if r.Fresh != tt.wantFresh {
				t.Errorf("Fresh = %v after %v, want %v", r.Fresh, tt.advance, tt.wantFresh)
			}
			// Knownness survives expiry: "the feed went quiet holding Ready" is a
			// different operator problem from "no producer was ever seen", and the
			// defer log distinguishes them. Either way the gate fails closed.
			if !r.Known {
				t.Error("Known = false; an expired status is still last-known")
			}
			if r.Age != tt.advance {
				t.Errorf("Age = %v, want %v", r.Age, tt.advance)
			}
		})
	}
}

// TestWatcher_AlreadyStaleOnArrivalIsUnknown pins the ONE commit-time comparison in
// the package, and it is the sharpest fail-closed rule here: a watch delivers the
// key's CURRENT value the instant it binds, so a consumer starting against a DEAD
// producer receives that producer's final envelope — possibly Ready=true, written an
// hour ago. Pure arrival-time freshness would stamp it brand new and serve it as live
// for a whole window, a fail-open the request/reply never had (it got no responders).
//
// The control row is the point: a value committed just inside the window is accepted,
// so the rule cannot be satisfied by rejecting everything.
func TestWatcher_AlreadyStaleOnArrivalIsUnknown(t *testing.T) {
	const heartbeat = 5 * time.Second
	window := FreshnessWindow(heartbeat)
	// The harness clock is fixed, so commit stamps are expressed relative to it.
	base := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name      string
		committed time.Duration // age at arrival
		wantFresh bool
	}{
		{"committed just now", 0, true},
		{"committed one heartbeat ago", heartbeat, true},
		{"committed just inside the window", window - time.Millisecond, true},
		{"committed one millisecond past the window", window + time.Millisecond, false},
		{"dead producer's hour-old final envelope", time.Hour, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t, WithHeartbeat(heartbeat))
			entry := readyEntry(t)
			entry.created = base.Add(-tt.committed)
			h.deliver(entry)

			r := h.watcher.Read()
			if r.Fresh != tt.wantFresh {
				t.Errorf("Fresh = %v for a value committed %v before arrival, want %v",
					r.Fresh, tt.committed, tt.wantFresh)
			}
			if !r.Known {
				t.Error("Known = false; the envelope did arrive and is last-known")
			}
			if tt.wantFresh {
				return
			}
			// A stale-on-arrival rejection is otherwise invisible — knownness is true
			// and Age is ~zero — so it MUST carry its own attribution, or the defer
			// log reads "status_known=true status_age=0s reason=status_unknown" with
			// nothing explaining it.
			if r.Err == nil {
				t.Error("Err = nil; a stale-on-arrival rejection must be attributable")
			}
		})
	}
}

// TestWatcher_MissingBucketIsUnknown covers the standalone deployment (a consumer
// running without its producer) and a producer that has not created the bucket yet:
// unknown, fail closed, with the bind error carried for the defer log — never a
// fabricated envelope, and never an error out of Start.
func TestWatcher_MissingBucketIsUnknown(t *testing.T) {
	bucketErr := errors.New("nats: bucket not found")
	source := &fakeSource{opened: make(chan struct{}, 8)}
	source.open = func(context.Context) (jetstream.KeyValue, error) { return nil, bucketErr }

	w := NewWatcher(source, KeyGraphIndex,
		WithLogger(discardLogger()), withRebindDelay(time.Millisecond))
	if err := w.Start(context.Background()); err != nil {
		t.Fatalf("Start must not fail on an absent bucket: %v", err)
	}
	defer w.Stop()

	// Wait for a real bind ATTEMPT so the assertion is not vacuously true.
	select {
	case <-source.opened:
	case <-time.After(5 * time.Second):
		t.Fatal("watcher never attempted to open the bucket")
	}
	// The error is recorded after the failed open returns; wait for the retry, which
	// proves the first attempt completed.
	select {
	case <-source.opened:
	case <-time.After(5 * time.Second):
		t.Fatal("watcher never retried the bucket open")
	}

	r := w.Read()
	if r.Known || r.Fresh {
		t.Fatalf("read with an absent bucket = %+v, want unknown", r)
	}
	if !reflect.DeepEqual(r.Status, graph.IndexStatusResponse{}) {
		t.Errorf("held envelope = %+v, want the zero value (nothing fabricated)", r.Status)
	}
	if !errors.Is(r.Err, bucketErr) {
		t.Errorf("Err = %v, want it to wrap %v for the structured defer log", r.Err, bucketErr)
	}
}

// TestWatcher_MissingKeyIsUnknown covers a live bucket with no envelope yet: the
// watch delivers only the end-of-initial-values marker, which is not a value.
func TestWatcher_MissingKeyIsUnknown(t *testing.T) {
	h := newHarness(t)

	// An unbuffered send returns only once the watch goroutine has taken the marker,
	// so the assertion below runs after the watcher has genuinely processed it.
	select {
	case h.updates <- nil:
	case <-time.After(5 * time.Second):
		t.Fatal("watcher never received the end-of-initial-values marker")
	}

	if r := h.watcher.Read(); r.Known || r.Fresh {
		t.Fatalf("read with no published key = %+v, want unknown", r)
	}

	// And the key appearing later is picked up on the same watch.
	h.deliver(readyEntry(t))
	if r := h.watcher.Read(); !r.Known || !r.Fresh {
		t.Fatalf("read after the key appears = %+v, want known and fresh", r)
	}
}

// TestWatcher_TombstoneClearsKnownness: a deleted status key is not evidence of a
// live producer, so it must revoke knownness rather than leave the last good
// envelope standing as fresh.
func TestWatcher_TombstoneClearsKnownness(t *testing.T) {
	h := newHarness(t)
	h.deliver(readyEntry(t))

	h.deliver(fakeEntry{key: KeyGraphIndex, rev: 2, op: jetstream.KeyValueDelete})

	r := h.watcher.Read()
	if r.Known || r.Fresh {
		t.Fatalf("read after tombstone = %+v, want unknown", r)
	}
	if r.Err == nil {
		t.Error("Err = nil after a tombstone; the defer log needs the cause")
	}
}

// TestWatcher_UndecodableValueDoesNotRefreshFreshness: garbage on the key must not
// stamp an arrival time, or a broken producer would read as perfectly fresh.
func TestWatcher_UndecodableValueDoesNotRefreshFreshness(t *testing.T) {
	const heartbeat = 5 * time.Second
	h := newHarness(t, WithHeartbeat(heartbeat))
	h.deliver(readyEntry(t))

	// Age the good value out of the window, then deliver garbage.
	h.clock.advance(4 * heartbeat)
	h.deliver(fakeEntry{key: KeyGraphIndex, value: []byte("{not json"), rev: 2, op: jetstream.KeyValuePut})

	r := h.watcher.Read()
	if r.Fresh {
		t.Fatalf("read after an undecodable update = %+v, want stale (arrival not refreshed)", r)
	}
	if r.Err == nil {
		t.Error("Err = nil after a decode failure")
	}
	if !r.Status.Ready {
		t.Error("the last GOOD envelope must survive for diagnostics")
	}
}

// TestWatcher_RebindsAfterWatchLoss proves the feed recovers on its own: when the
// watch closes (connection loss, server restart), the watcher re-opens and the next
// delivery makes the consumer fresh again — the recovery path that keeps a transient
// blip from becoming a permanent status_unknown.
func TestWatcher_RebindsAfterWatchLoss(t *testing.T) {
	h := newHarness(t)
	h.deliver(readyEntry(t))

	// Close the current watch channel and hand out a fresh one on the next open.
	second := make(chan jetstream.KeyValueEntry)
	h.source.open = func(context.Context) (jetstream.KeyValue, error) {
		return &fakeBucket{watch: func(context.Context) (jetstream.KeyWatcher, error) {
			return &fakeWatcher{updates: second}, nil
		}}, nil
	}
	close(h.updates)

	building := fakeEntry{
		key: KeyGraphIndex,
		value: statusJSON(t, graph.IndexStatusResponse{
			State: graph.IndexStateBuilding, TargetRevision: 600, Lag: 100, StalenessMs: 1200,
		}),
		rev: 2,
		op:  jetstream.KeyValuePut,
	}
	select {
	case second <- building:
	case <-time.After(5 * time.Second):
		t.Fatal("watcher never rebound after the watch closed")
	}
	select {
	case <-h.applied:
	case <-time.After(5 * time.Second):
		t.Fatal("watcher never applied the post-rebind entry")
	}

	r := h.watcher.Read()
	if !r.Known || !r.Fresh {
		t.Fatalf("read after rebind = %+v, want known and fresh", r)
	}
	if r.Status.StalenessMs != 1200 {
		t.Errorf("StalenessMs = %d, want 1200 (the post-rebind envelope)", r.Status.StalenessMs)
	}
}

// TestWatcher_StartValidatesWiring: misconfiguration fails loudly at Start, unlike an
// absent bucket (a legitimate deployment shape that must stay a runtime unknown).
func TestWatcher_StartValidatesWiring(t *testing.T) {
	if err := NewWatcher(nil, KeyGraphIndex).Start(context.Background()); err == nil {
		t.Error("Start with a nil source must fail")
	}
	src := &fakeSource{opened: make(chan struct{}, 1)}
	src.open = func(context.Context) (jetstream.KeyValue, error) { return nil, errors.New("unused") }
	if err := NewWatcher(src, "").Start(context.Background()); err == nil {
		t.Error("Start with an empty key must fail")
	}
}

// TestFreshnessWindow pins the ENVELOPE-liveness window, which is the concept that
// survived the view-age deletion and is easy to confuse with it. It answers "can this
// consumer still vouch for the status reading it holds", not "how far behind is the
// index" — the former gates (StatusReading.Fresh, fail closed), the latter never does.
//
// The satisfiability-floor rules that used to live beside it (MinBoundedStaleness,
// ValidateStalenessBound) are gone with the tolerance they constrained: with no
// view-age bound to declare, there is no bound that can be unsatisfiable.
func TestFreshnessWindow(t *testing.T) {
	if got, want := FreshnessWindow(2*time.Second), 6*time.Second; got != want {
		t.Errorf("FreshnessWindow(2s) = %s, want %s (%dx heartbeat)", got, want, FreshnessMultiplier)
	}

	// A consumer that never declared a heartbeat gets the real default window rather
	// than an accidental zero, which would mark every reading stale on arrival.
	for _, heartbeat := range []time.Duration{0, -time.Second} {
		if got, want := FreshnessWindow(heartbeat), FreshnessMultiplier*DefaultHeartbeat; got != want {
			t.Errorf("FreshnessWindow(%s) = %s, want the default window %s", heartbeat, got, want)
		}
	}
}
