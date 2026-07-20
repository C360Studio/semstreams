package agenticdispatch

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/metric"
	"github.com/c360studio/semstreams/pkg/graphview"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// activityTestWait bounds channel/output waits on FAILURE paths only; every
// synchronization point is an explicit channel or an output-content wait.
const activityTestWait = 5 * time.Second

// ---------------------------------------------------------------------------
// Fake AGENT_LOOPS watcher layer (the graphview.WatcherSource seam)
// ---------------------------------------------------------------------------

type fakeActivityEntry struct {
	key     string
	value   []byte
	rev     uint64
	op      jetstream.KeyValueOp
	created time.Time
}

func (e fakeActivityEntry) Bucket() string                  { return "AGENT_LOOPS" }
func (e fakeActivityEntry) Key() string                     { return e.key }
func (e fakeActivityEntry) Value() []byte                   { return e.value }
func (e fakeActivityEntry) Revision() uint64                { return e.rev }
func (e fakeActivityEntry) Created() time.Time              { return e.created }
func (e fakeActivityEntry) Delta() uint64                   { return 0 }
func (e fakeActivityEntry) Operation() jetstream.KeyValueOp { return e.op }

type fakeActivityWatcher struct {
	updates chan jetstream.KeyValueEntry
}

func (w *fakeActivityWatcher) Updates() <-chan jetstream.KeyValueEntry { return w.updates }
func (w *fakeActivityWatcher) Stop() error                             { return nil }

// fakeActivitySource counts WatchAll calls — the one-watcher-for-N-clients
// proof reads s.calls().
type fakeActivitySource struct {
	mu       sync.Mutex
	watchers []*fakeActivityWatcher
	created  chan struct{}
}

func newFakeActivitySource() *fakeActivitySource {
	return &fakeActivitySource{created: make(chan struct{}, 8)}
}

func (s *fakeActivitySource) WatchAll(_ context.Context, _ ...jetstream.WatchOpt) (jetstream.KeyWatcher, error) {
	s.mu.Lock()
	w := &fakeActivityWatcher{updates: make(chan jetstream.KeyValueEntry, 256)}
	s.watchers = append(s.watchers, w)
	s.mu.Unlock()
	s.created <- struct{}{}
	return w, nil
}

func (s *fakeActivitySource) calls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.watchers)
}

// waitWatcher blocks until the n-th (1-based) WatchAll happened and returns
// that watcher.
func (s *fakeActivitySource) waitWatcher(t *testing.T, n int) *fakeActivityWatcher {
	t.Helper()
	deadline := time.After(activityTestWait)
	for {
		s.mu.Lock()
		if len(s.watchers) >= n {
			w := s.watchers[n-1]
			s.mu.Unlock()
			return w
		}
		s.mu.Unlock()
		select {
		case <-s.created:
		case <-deadline:
			t.Fatalf("timed out waiting for WatchAll call %d", n)
		}
	}
}

// ---------------------------------------------------------------------------
// View hook synchronization channels (explicit sync — no sleeps)
// ---------------------------------------------------------------------------

type activityHookChans struct {
	applied     chan string
	caughtUp    chan struct{}
	subscribers chan int
	poisons     chan string
	lost        chan error
}

func newActivityHookChans() *activityHookChans {
	return &activityHookChans{
		applied:     make(chan string, 4096),
		caughtUp:    make(chan struct{}, 8),
		subscribers: make(chan int, 256),
		poisons:     make(chan string, 64),
		lost:        make(chan error, 4),
	}
}

func (h *activityHookChans) hooks() graphview.Hooks {
	return graphview.Hooks{
		OnApply:       func(key string, _ uint64) { h.applied <- key },
		OnCaughtUp:    func() { h.caughtUp <- struct{}{} },
		OnSubscribers: func(n int) { h.subscribers <- n },
		OnPoison:      func(key string, _ error) { h.poisons <- key },
		OnWatcherLost: func(err error) { h.lost <- err },
	}
}

// waitAppliedKey drains the apply hook until the given key was applied.
func (h *activityHookChans) waitAppliedKey(t *testing.T, key string) {
	t.Helper()
	deadline := time.After(activityTestWait)
	for {
		select {
		case k := <-h.applied:
			if k == key {
				return
			}
		case <-deadline:
			t.Fatalf("timed out waiting for apply of %q", key)
		}
	}
}

func (h *activityHookChans) waitCaughtUp(t *testing.T) {
	t.Helper()
	select {
	case <-h.caughtUp:
	case <-time.After(activityTestWait):
		t.Fatal("timed out waiting for view caught-up")
	}
}

// waitSubscriberCount drains the subscriber hook until the given count is
// observed.
func (h *activityHookChans) waitSubscriberCount(t *testing.T, want int) {
	t.Helper()
	deadline := time.After(activityTestWait)
	for {
		select {
		case n := <-h.subscribers:
			if n == want {
				return
			}
		case <-deadline:
			t.Fatalf("timed out waiting for subscriber count %d", want)
		}
	}
}

func (h *activityHookChans) waitPoisonKey(t *testing.T, key string) {
	t.Helper()
	deadline := time.After(activityTestWait)
	for {
		select {
		case k := <-h.poisons:
			if k == key {
				return
			}
		case <-deadline:
			t.Fatalf("timed out waiting for poison of %q", key)
		}
	}
}

func (h *activityHookChans) waitLost(t *testing.T) {
	t.Helper()
	select {
	case <-h.lost:
	case <-time.After(activityTestWait):
		t.Fatal("timed out waiting for watcher loss")
	}
}

// ---------------------------------------------------------------------------
// SSE recorder: a streaming ResponseWriter with content waits and a write
// gate for the slow-client test
// ---------------------------------------------------------------------------

type sseRecorder struct {
	mu     sync.Mutex
	header http.Header
	buf    bytes.Buffer
	gate   chan struct{}
	signal chan struct{}
}

func newSSERecorder() *sseRecorder {
	return &sseRecorder{header: make(http.Header), signal: make(chan struct{}, 1)}
}

func (r *sseRecorder) Header() http.Header { return r.header }
func (r *sseRecorder) WriteHeader(int)     {}
func (r *sseRecorder) Flush()              {}

func (r *sseRecorder) Write(p []byte) (int, error) {
	r.mu.Lock()
	gate := r.gate
	r.mu.Unlock()
	if gate != nil {
		<-gate // stalled client: the handler goroutine parks right here
	}
	r.mu.Lock()
	n, err := r.buf.Write(p)
	r.mu.Unlock()
	select {
	case r.signal <- struct{}{}:
	default:
	}
	return n, err
}

func (r *sseRecorder) contents() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.buf.String()
}

// stall blocks all subsequent writes until the returned release func runs.
func (r *sseRecorder) stall() func() {
	gate := make(chan struct{})
	r.mu.Lock()
	r.gate = gate
	r.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			r.mu.Lock()
			r.gate = nil
			r.mu.Unlock()
			close(gate)
		})
	}
}

// waitFor blocks until the SSE output contains substr.
func (r *sseRecorder) waitFor(t *testing.T, substr string) {
	t.Helper()
	deadline := time.After(activityTestWait)
	for {
		if strings.Contains(r.contents(), substr) {
			return
		}
		select {
		case <-r.signal:
		case <-deadline:
			t.Fatalf("timed out waiting for %q in SSE output:\n%s", substr, r.contents())
		}
	}
}

// sseFrame is one parsed "event:/data:" block.
type sseFrame struct {
	event string
	data  string
}

func parseSSEFrames(raw string) []sseFrame {
	var frames []sseFrame
	for _, block := range strings.Split(raw, "\n\n") {
		var f sseFrame
		for _, line := range strings.Split(block, "\n") {
			switch {
			case strings.HasPrefix(line, "event: "):
				f.event = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				f.data = strings.TrimPrefix(line, "data: ")
			}
		}
		if f.event != "" {
			frames = append(frames, f)
		}
	}
	return frames
}

// activityFrames returns the decoded ActivityEvent payloads of every
// "activity" frame in order.
func activityFrames(t *testing.T, raw string) []ActivityEvent {
	t.Helper()
	var out []ActivityEvent
	for _, f := range parseSSEFrames(raw) {
		if f.event != "activity" {
			continue
		}
		var ev ActivityEvent
		require.NoError(t, json.Unmarshal([]byte(f.data), &ev), "activity frame data must decode: %s", f.data)
		out = append(out, ev)
	}
	return out
}

// ---------------------------------------------------------------------------
// Client + component harness
// ---------------------------------------------------------------------------

type activityClient struct {
	rec    *sseRecorder
	cancel context.CancelFunc
	done   chan struct{}
}

func startActivityClient(t *testing.T, comp *Component) *activityClient {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/activity", nil).WithContext(ctx)
	cl := &activityClient{rec: newSSERecorder(), cancel: cancel, done: make(chan struct{})}
	go func() {
		defer close(cl.done)
		comp.handleActivityStream(cl.rec, req)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-cl.done:
		case <-time.After(activityTestWait):
			t.Error("activity handler goroutine did not exit on cleanup")
		}
	})
	return cl
}

// close disconnects the client and waits for the handler goroutine to exit.
func (cl *activityClient) close(t *testing.T) {
	t.Helper()
	cl.cancel()
	select {
	case <-cl.done:
	case <-time.After(activityTestWait):
		t.Fatal("handler did not exit after client disconnect")
	}
}

// waitDone waits for the handler goroutine to exit without cancelling.
func (cl *activityClient) waitDone(t *testing.T) {
	t.Helper()
	select {
	case <-cl.done:
	case <-time.After(activityTestWait):
		t.Fatal("handler did not exit")
	}
}

func newActivityTestComponent(t *testing.T, src graphview.WatcherSource, extra graphview.Hooks) *Component {
	t.Helper()
	comp := newTestComponent(t)
	comp.activityViewSource = src
	comp.activityViewOpts = []graphview.Option{graphview.WithTickInterval(2 * time.Millisecond)}
	comp.activityTestHooks = extra
	t.Cleanup(comp.stopActivityView)
	return comp
}

func loopEntityJSON(t *testing.T, id string, state agentic.LoopState, iterations int) []byte {
	t.Helper()
	e := agentic.LoopEntity{
		ID:            id,
		TaskID:        "task-" + id,
		State:         state,
		Role:          "researcher",
		Iterations:    iterations,
		MaxIterations: 10,
	}
	b, err := json.Marshal(&e)
	require.NoError(t, err)
	return b
}

func loopCompletionJSON(t *testing.T, id string) []byte {
	t.Helper()
	ev := &agentic.LoopCompletedEvent{
		LoopID:      id,
		TaskID:      "task-" + id,
		Role:        "researcher",
		Outcome:     agentic.OutcomeSuccess,
		Result:      "42",
		Iterations:  3,
		CompletedAt: time.Date(2026, 7, 19, 10, 0, 0, 0, time.UTC),
	}
	b, err := json.Marshal(ev)
	require.NoError(t, err)
	return b
}

func putEntry(key string, value []byte, rev uint64) fakeActivityEntry {
	return fakeActivityEntry{key: key, value: value, rev: rev, op: jetstream.KeyValuePut}
}

func putEntryAt(key string, value []byte, rev uint64, created time.Time) fakeActivityEntry {
	return fakeActivityEntry{key: key, value: value, rev: rev, op: jetstream.KeyValuePut, created: created}
}

func delEntry(key string, rev uint64) fakeActivityEntry {
	return fakeActivityEntry{key: key, rev: rev, op: jetstream.KeyValueDelete}
}

func delEntryAt(key string, rev uint64, created time.Time) fakeActivityEntry {
	return fakeActivityEntry{key: key, rev: rev, op: jetstream.KeyValueDelete, created: created}
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestActivityStream_OneWatcherManyClients is the #579 proof: N concurrent
// SSE clients are served replay + live events by exactly ONE AGENT_LOOPS
// watcher, and disconnects release their subscriptions.
func TestActivityStream_OneWatcherManyClients(t *testing.T) {
	src := newFakeActivitySource()
	hooks := newActivityHookChans()
	comp := newActivityTestComponent(t, src, hooks.hooks())

	// First client triggers the lazy view; feed its bootstrap replay.
	c1 := startActivityClient(t, comp)
	w := src.waitWatcher(t, 1)
	w.updates <- putEntry("loop_a", loopEntityJSON(t, "loop_a", agentic.LoopStateExecuting, 1), 1)
	w.updates <- nil // end-of-replay marker
	hooks.waitCaughtUp(t)

	c1.rec.waitFor(t, "sync_complete")
	c1.rec.waitFor(t, `"loop_id":"loop_a"`)

	c2 := startActivityClient(t, comp)
	c3 := startActivityClient(t, comp)
	c2.rec.waitFor(t, "sync_complete")
	c3.rec.waitFor(t, "sync_complete")
	// Late attachers replay from the shared snapshot, not a fresh watcher.
	c2.rec.waitFor(t, `"loop_id":"loop_a"`)
	c3.rec.waitFor(t, `"loop_id":"loop_a"`)
	hooks.waitSubscriberCount(t, 3)

	// One live write fans out to every client through the one watcher.
	w.updates <- putEntry("loop_b", loopEntityJSON(t, "loop_b", agentic.LoopStatePlanning, 0), 2)
	for _, cl := range []*activityClient{c1, c2, c3} {
		cl.rec.waitFor(t, `"loop_id":"loop_b"`)
	}

	require.Equal(t, 1, src.calls(), "N SSE clients must share exactly ONE bucket watcher")

	// Disconnect cleanup: every subscription is released.
	c1.close(t)
	c2.close(t)
	c3.close(t)
	hooks.waitSubscriberCount(t, 0)
	require.Equal(t, 1, src.calls(), "disconnects must not churn watchers")
}

// TestActivityStream_ReplayThenLiveOrdering locks the wire contract:
// connected → retry directive → snapshot replay → sync_complete → live
// events, with today's event names, payload shapes, and type derivation
// (revision 1 → loop_created, revision > 1 → loop_updated).
func TestActivityStream_ReplayThenLiveOrdering(t *testing.T) {
	src := newFakeActivitySource()
	hooks := newActivityHookChans()
	comp := newActivityTestComponent(t, src, hooks.hooks())

	cl := startActivityClient(t, comp)
	w := src.waitWatcher(t, 1)
	w.updates <- putEntry("loop_a", loopEntityJSON(t, "loop_a", agentic.LoopStateExecuting, 1), 1)
	w.updates <- nil
	hooks.waitCaughtUp(t)
	cl.rec.waitFor(t, "sync_complete")

	// Live update to the same loop after the seam.
	w.updates <- putEntry("loop_a", loopEntityJSON(t, "loop_a", agentic.LoopStateReviewing, 2), 2)
	cl.rec.waitFor(t, string(agentic.LoopStateReviewing))

	raw := cl.rec.contents()
	idxConnected := strings.Index(raw, "event: connected")
	idxRetry := strings.Index(raw, "retry: 5000")
	idxReplay := strings.Index(raw, `"type":"loop_created"`)
	idxSync := strings.Index(raw, "event: sync_complete")
	idxLive := strings.Index(raw, `"type":"loop_updated"`)
	require.True(t, idxConnected >= 0 && idxRetry >= 0 && idxReplay >= 0 && idxSync >= 0 && idxLive >= 0,
		"missing wire elements in output:\n%s", raw)
	assert.Less(t, idxConnected, idxRetry, "connected precedes the retry directive")
	assert.Less(t, idxRetry, idxReplay, "retry directive precedes replay")
	assert.Less(t, idxReplay, idxSync, "replay precedes sync_complete")
	assert.Less(t, idxSync, idxLive, "live events follow sync_complete")

	events := activityFrames(t, raw)
	require.Len(t, events, 2)
	replay, live := events[0], events[1]
	assert.Equal(t, "loop_created", replay.Type, "revision-1 replay entry maps to loop_created")
	assert.Equal(t, "loop_a", replay.LoopID)
	require.NotNil(t, replay.Data)
	assert.Equal(t, "loop_a", replay.Data.LoopID, "envelope loop_id equals data.loop_id")
	assert.Equal(t, agentic.LoopStateExecuting.String(), replay.Data.State)
	assert.Equal(t, 1, replay.Data.Iterations)

	assert.Equal(t, "loop_updated", live.Type, "revision >1 maps to loop_updated")
	require.NotNil(t, live.Data)
	assert.Equal(t, agentic.LoopStateReviewing.String(), live.Data.State)
	assert.Equal(t, 2, live.Data.Iterations)

	// The connected frame carries the client id, as before.
	frames := parseSSEFrames(raw)
	require.NotEmpty(t, frames)
	assert.Equal(t, "connected", frames[0].event, "first frame is connected")
	assert.Contains(t, frames[0].data, "client_id")
	assert.Contains(t, frames[0].data, "Watching for activity events")
}

// TestActivityStream_CompletionAndDeleteEvents: COMPLETE_<id> keys emit
// loop_completed with the bare loop id and the terminal payload decoded by
// the production projection; KV deletes emit loop_deleted with no data —
// both identical to the per-client watcher wire.
func TestActivityStream_CompletionAndDeleteEvents(t *testing.T) {
	src := newFakeActivitySource()
	hooks := newActivityHookChans()
	comp := newActivityTestComponent(t, src, hooks.hooks())

	cl := startActivityClient(t, comp)
	w := src.waitWatcher(t, 1)
	w.updates <- nil
	hooks.waitCaughtUp(t)
	cl.rec.waitFor(t, "sync_complete")

	w.updates <- putEntry("loop_x", loopEntityJSON(t, "loop_x", agentic.LoopStateExecuting, 1), 1)
	cl.rec.waitFor(t, `"loop_id":"loop_x"`)

	w.updates <- putEntry("COMPLETE_loop_x", loopCompletionJSON(t, "loop_x"), 2)
	cl.rec.waitFor(t, "loop_completed")

	w.updates <- delEntry("loop_x", 3)
	cl.rec.waitFor(t, "loop_deleted")

	events := activityFrames(t, cl.rec.contents())
	require.Len(t, events, 3)

	completed := events[1]
	assert.Equal(t, "loop_completed", completed.Type)
	assert.Equal(t, "loop_x", completed.LoopID, "COMPLETE_ prefix must be stripped from the envelope")
	require.NotNil(t, completed.Data)
	assert.Equal(t, "loop_x", completed.Data.LoopID)
	assert.Equal(t, agentic.OutcomeSuccess, completed.Data.Outcome, "data.outcome carries the verdict")
	assert.Empty(t, completed.Data.State, "terminal events populate outcome, not state")

	deleted := events[2]
	assert.Equal(t, "loop_deleted", deleted.Type)
	assert.Equal(t, "loop_x", deleted.LoopID)
	assert.Nil(t, deleted.Data, "delete events carry no data payload")
}

// TestActivityStream_PoisonIsPerKeyNonTerminal (G6): a malformed AGENT_LOOPS
// value surfaces on the existing SSE error path for the affected key only —
// the connection stays open, later keys keep flowing, and a late attacher
// sees the sticky poison surfaced during its replay.
func TestActivityStream_PoisonIsPerKeyNonTerminal(t *testing.T) {
	src := newFakeActivitySource()
	hooks := newActivityHookChans()
	comp := newActivityTestComponent(t, src, hooks.hooks())

	c1 := startActivityClient(t, comp)
	w := src.waitWatcher(t, 1)
	w.updates <- nil
	hooks.waitCaughtUp(t)
	c1.rec.waitFor(t, "sync_complete")

	w.updates <- putEntry("loop_bad", []byte("{not json"), 1)
	hooks.waitPoisonKey(t, "loop_bad")
	c1.rec.waitFor(t, "Malformed loop entry: loop_bad")

	// The stream survives the poisoned key: a later healthy write flows.
	w.updates <- putEntry("loop_ok", loopEntityJSON(t, "loop_ok", agentic.LoopStateExecuting, 1), 2)
	c1.rec.waitFor(t, `"loop_id":"loop_ok"`)
	select {
	case <-c1.done:
		t.Fatal("one poisoned key must not terminate the SSE connection")
	default:
	}

	// A late attacher sees the sticky poison during replay, before
	// sync_complete, plus the healthy entry.
	c2 := startActivityClient(t, comp)
	c2.rec.waitFor(t, "sync_complete")
	raw := c2.rec.contents()
	idxPoison := strings.Index(raw, "Malformed loop entry: loop_bad")
	idxOK := strings.Index(raw, `"loop_id":"loop_ok"`)
	idxSync := strings.Index(raw, "event: sync_complete")
	require.True(t, idxPoison >= 0 && idxOK >= 0 && idxSync >= 0, "missing replay elements:\n%s", raw)
	assert.Less(t, idxPoison, idxSync, "snapshot poison surfaces during replay")
	assert.Less(t, idxOK, idxSync, "snapshot entries replay before sync_complete")
}

// TestActivityStream_WatcherLossFailsClosedThenRestartsOnAttach (G5): losing
// the shared watcher sends every connected client the existing terminal SSE
// error and ends their connections (never a silently frozen stream); the
// NEXT attach restarts the view, re-bootstraps from a fresh watcher, and
// serves a coherent snapshot.
func TestActivityStream_WatcherLossFailsClosedThenRestartsOnAttach(t *testing.T) {
	src := newFakeActivitySource()
	hooks := newActivityHookChans()
	comp := newActivityTestComponent(t, src, hooks.hooks())

	c1 := startActivityClient(t, comp)
	w := src.waitWatcher(t, 1)
	w.updates <- putEntry("loop_a", loopEntityJSON(t, "loop_a", agentic.LoopStateExecuting, 1), 1)
	w.updates <- nil
	hooks.waitCaughtUp(t)
	c1.rec.waitFor(t, "sync_complete")

	// Watcher dies.
	close(w.updates)
	hooks.waitLost(t)
	c1.rec.waitFor(t, "Watcher closed unexpectedly")
	c1.waitDone(t)

	// Next attach restarts the shared view on a fresh watcher.
	c2 := startActivityClient(t, comp)
	w2 := src.waitWatcher(t, 2)
	w2.updates <- putEntry("loop_a", loopEntityJSON(t, "loop_a", agentic.LoopStateReviewing, 5), 2)
	w2.updates <- nil
	hooks.waitCaughtUp(t)

	c2.rec.waitFor(t, "sync_complete")
	c2.rec.waitFor(t, `"loop_id":"loop_a"`)
	events := activityFrames(t, c2.rec.contents())
	require.Len(t, events, 1)
	require.NotNil(t, events[0].Data)
	assert.Equal(t, agentic.LoopStateReviewing.String(), events[0].Data.State,
		"post-restart snapshot serves the fresh replay's state")
	assert.Equal(t, 2, src.calls(), "restart opens exactly one new watcher")
}

// TestActivityStream_SlowClientDoesNotStallPeers: a stalled HTTP write parks
// only that client's drain loop. The view keeps applying, the fast client
// keeps receiving at view rate, and the slow client converges to current
// state (coalesced) once released — it is never disconnected for slowness.
func TestActivityStream_SlowClientDoesNotStallPeers(t *testing.T) {
	src := newFakeActivitySource()
	hooks := newActivityHookChans()
	comp := newActivityTestComponent(t, src, hooks.hooks())

	slow := startActivityClient(t, comp)
	w := src.waitWatcher(t, 1)
	w.updates <- nil
	hooks.waitCaughtUp(t)
	slow.rec.waitFor(t, "sync_complete")

	fast := startActivityClient(t, comp)
	fast.rec.waitFor(t, "sync_complete")
	hooks.waitSubscriberCount(t, 2)

	release := slow.rec.stall()
	defer release()

	// Five revisions of one loop land while the slow client cannot write.
	for rev := uint64(1); rev <= 5; rev++ {
		w.updates <- putEntry("loop_k", loopEntityJSON(t, "loop_k", agentic.LoopStateExecuting, int(rev)), rev)
		hooks.waitAppliedKey(t, "loop_k")
	}

	// The fast client converges while the slow client is stalled — proof the
	// watcher, the projection, and peers never blocked on the stalled write.
	fast.rec.waitFor(t, `"iterations":5`)
	require.Equal(t, 1, src.calls())

	// Released, the slow client converges to current state without needing
	// every intermediate revision (its pending deltas coalesced LWW).
	release()
	slow.rec.waitFor(t, `"iterations":5`)
	select {
	case <-slow.done:
		t.Fatal("slowness alone must never disconnect a client")
	default:
	}
}

// TestActivityStream_NilNATSClientDegradesPerRequest: without a NATS client
// (and no injected source) the endpoint degrades per request with today's
// bucket-access SSE error — view laziness keeps AGENT_LOOPS absence a
// request-time condition, not a boot failure.
func TestActivityStream_NilNATSClientDegradesPerRequest(t *testing.T) {
	comp := newTestComponent(t) // natsClient nil, no activityViewSource
	t.Cleanup(comp.stopActivityView)

	cl := startActivityClient(t, comp)
	cl.rec.waitFor(t, "Failed to access AGENT_LOOPS bucket")
	cl.waitDone(t)

	frames := parseSSEFrames(cl.rec.contents())
	require.NotEmpty(t, frames)
	assert.Equal(t, "error", frames[0].event, "bucket absence surfaces on the SSE error event")
}

// TestDecodeActivityRecord drives the view's validating decode directly:
// live keys decode agentic.LoopEntity through the production projection,
// COMPLETE_ keys decode terminal payloads, the record carries the entry's
// authoritative KV write time (meta.Created), and only genuinely malformed
// payloads poison (err != nil).
func TestDecodeActivityRecord(t *testing.T) {
	comp := newTestComponent(t)
	created := time.Date(2026, 7, 10, 8, 0, 0, 0, time.UTC)
	meta := graphview.EntryMeta{Revision: 3, Created: created}

	t.Run("live loop entity decodes and projects", func(t *testing.T) {
		rec, keep, err := comp.decodeActivityRecord("loop_a", loopEntityJSON(t, "loop_a", agentic.LoopStateExecuting, 3), meta)
		require.NoError(t, err)
		require.True(t, keep)
		assert.Equal(t, "loop_a", rec.loop.LoopID)
		assert.Equal(t, agentic.LoopStateExecuting.String(), rec.loop.State)
		assert.Equal(t, 3, rec.loop.Iterations)
		assert.True(t, rec.createdAt.Equal(created), "record must carry the KV write time, not decode time")
	})

	t.Run("completion payload decodes with bare loop id", func(t *testing.T) {
		rec, keep, err := comp.decodeActivityRecord("COMPLETE_loop_a", loopCompletionJSON(t, "loop_a"), meta)
		require.NoError(t, err)
		require.True(t, keep)
		assert.Equal(t, "loop_a", rec.loop.LoopID)
		assert.Equal(t, agentic.OutcomeSuccess, rec.loop.Outcome)
		assert.True(t, rec.createdAt.Equal(created))
	})

	t.Run("malformed loop entity poisons", func(t *testing.T) {
		_, _, err := comp.decodeActivityRecord("loop_bad", []byte("{not json"), meta)
		require.Error(t, err)
	})

	t.Run("malformed completion poisons", func(t *testing.T) {
		_, _, err := comp.decodeActivityRecord("COMPLETE_loop_bad", []byte("{not json"), meta)
		require.Error(t, err)
	})

	t.Run("completion without loop_id poisons", func(t *testing.T) {
		_, _, err := comp.decodeActivityRecord("COMPLETE_loop_bad", []byte("{}"), meta)
		require.Error(t, err)
	})
}

// TestActivityStream_TimestampIsKVWriteTime pins the timestamp source to the
// old wire's semantics: ActivityEvent.Timestamp is the KV entry's Created()
// — for snapshot replay, live upserts, AND delete markers — and replayed
// events after a view re-bootstrap still carry the ORIGINAL write times, not
// the bootstrap time.
func TestActivityStream_TimestampIsKVWriteTime(t *testing.T) {
	tReplay := time.Date(2026, 7, 2, 3, 4, 5, 0, time.UTC)
	tLive := time.Date(2026, 7, 2, 6, 7, 8, 0, time.UTC)
	tDelete := time.Date(2026, 7, 2, 9, 10, 11, 0, time.UTC)

	src := newFakeActivitySource()
	hooks := newActivityHookChans()
	comp := newActivityTestComponent(t, src, hooks.hooks())

	// Replay entry carries its original write time.
	c1 := startActivityClient(t, comp)
	w := src.waitWatcher(t, 1)
	w.updates <- putEntryAt("loop_a", loopEntityJSON(t, "loop_a", agentic.LoopStateExecuting, 1), 1, tReplay)
	w.updates <- nil
	hooks.waitCaughtUp(t)
	c1.rec.waitFor(t, "sync_complete")

	// Live upsert and delete carry their entries' write times.
	w.updates <- putEntryAt("loop_b", loopEntityJSON(t, "loop_b", agentic.LoopStatePlanning, 1), 2, tLive)
	c1.rec.waitFor(t, `"loop_id":"loop_b"`)
	w.updates <- delEntryAt("loop_b", 3, tDelete)
	c1.rec.waitFor(t, "loop_deleted")

	events := activityFrames(t, c1.rec.contents())
	require.Len(t, events, 3)
	require.True(t, events[0].Timestamp.Equal(tReplay),
		"replay event must carry entry.Created(), got %v want %v", events[0].Timestamp, tReplay)
	require.True(t, events[1].Timestamp.Equal(tLive),
		"live upsert must carry entry.Created(), got %v want %v", events[1].Timestamp, tLive)
	require.True(t, events[2].Timestamp.Equal(tDelete),
		"delete event must carry the delete marker's Created(), got %v want %v", events[2].Timestamp, tDelete)

	// Watcher loss + restart-on-attach: the re-bootstrapped snapshot still
	// serves the ORIGINAL write time, never the (much later) bootstrap time.
	close(w.updates)
	hooks.waitLost(t)
	c1.rec.waitFor(t, "Watcher closed unexpectedly")
	c1.waitDone(t)

	c2 := startActivityClient(t, comp)
	w2 := src.waitWatcher(t, 2)
	w2.updates <- putEntryAt("loop_a", loopEntityJSON(t, "loop_a", agentic.LoopStateExecuting, 1), 1, tReplay)
	w2.updates <- nil
	hooks.waitCaughtUp(t)
	c2.rec.waitFor(t, "sync_complete")

	events2 := activityFrames(t, c2.rec.contents())
	require.Len(t, events2, 1)
	require.True(t, events2[0].Timestamp.Equal(tReplay),
		"post-restart replay must carry the original write time, got %v want %v", events2[0].Timestamp, tReplay)
}

// TestActivityViewMetrics wires a real metrics registry and asserts the view
// telemetry lands on the component's Prometheus surface: staleness gauge,
// applied-revision watermark, poison counter, subscriber gauge, and the
// watcher-lost counter.
func TestActivityViewMetrics(t *testing.T) {
	registry := metric.NewMetricsRegistry()
	src := newFakeActivitySource()
	hooks := newActivityHookChans()
	comp := newActivityTestComponent(t, src, hooks.hooks())
	comp.metrics = getMetrics(registry)

	cl := startActivityClient(t, comp)
	w := src.waitWatcher(t, 1)
	w.updates <- putEntry("loop_a", loopEntityJSON(t, "loop_a", agentic.LoopStateExecuting, 1), 1)
	w.updates <- putEntry("loop_bad", []byte("{not json"), 2)
	hooks.waitPoisonKey(t, "loop_bad")
	w.updates <- nil
	hooks.waitCaughtUp(t)
	cl.rec.waitFor(t, "sync_complete")
	hooks.waitSubscriberCount(t, 1)

	m := comp.metrics
	assert.Equal(t, float64(1), getGaugeValue(t, m.activityViewCaughtUp), "caught-up gauge is 1 when live")
	assert.Equal(t, float64(2), getGaugeValue(t, m.activityViewAppliedRevision), "watermark reflects the last applied revision")
	assert.Equal(t, float64(1), getSimpleCounterValue(t, m.activityViewPoisonedTotal), "poison counter counts per poisoned write")
	assert.Equal(t, float64(1), getGaugeValue(t, m.activityViewSubscribers))

	cl.close(t)
	hooks.waitSubscriberCount(t, 0)
	assert.Equal(t, float64(0), getGaugeValue(t, m.activityViewSubscribers))

	close(w.updates)
	hooks.waitLost(t)
	assert.Equal(t, float64(0), getGaugeValue(t, m.activityViewCaughtUp), "watcher loss drops the staleness gauge to 0")
	assert.Equal(t, float64(1), getSimpleCounterValue(t, m.activityViewWatcherLostTotal))
}

// TestComponentStopStopsActivityView: the component owns the shared view —
// Stop terminates it, ending live SSE connections with the explicit terminal
// error instead of a silent hang.
func TestComponentStopStopsActivityView(t *testing.T) {
	src := newFakeActivitySource()
	hooks := newActivityHookChans()
	comp := newActivityTestComponent(t, src, hooks.hooks())

	cl := startActivityClient(t, comp)
	w := src.waitWatcher(t, 1)
	w.updates <- nil
	hooks.waitCaughtUp(t)
	cl.rec.waitFor(t, "sync_complete")

	comp.stopActivityView()
	cl.rec.waitFor(t, "Watcher closed unexpectedly")
	cl.waitDone(t)

	// A fresh attach after stop lazily rebuilds the view.
	c2 := startActivityClient(t, comp)
	w2 := src.waitWatcher(t, 2)
	w2.updates <- nil
	hooks.waitCaughtUp(t)
	c2.rec.waitFor(t, "sync_complete")
	require.Equal(t, 2, src.calls())
}

// TestActivityStreamCoalescesToViewRate: multiple revisions of one key
// inside one tick window reach the client as a single event carrying the
// newest state (G4 — current-state stream, not an event log). Proven by
// feeding all writes before the first tick can fire: the client's total
// activity frames for the key stay below the write count.
func TestActivityStreamCoalescesToViewRate(t *testing.T) {
	src := newFakeActivitySource()
	hooks := newActivityHookChans()
	comp := newTestComponent(t)
	comp.activityViewSource = src
	// A long tick guarantees every write lands in ONE window.
	comp.activityViewOpts = []graphview.Option{graphview.WithTickInterval(200 * time.Millisecond)}
	comp.activityTestHooks = hooks.hooks()
	t.Cleanup(comp.stopActivityView)

	cl := startActivityClient(t, comp)
	w := src.waitWatcher(t, 1)
	w.updates <- nil
	hooks.waitCaughtUp(t)
	cl.rec.waitFor(t, "sync_complete")

	for rev := uint64(1); rev <= 5; rev++ {
		w.updates <- putEntry("loop_k", loopEntityJSON(t, "loop_k", agentic.LoopStateExecuting, int(rev)), rev)
		hooks.waitAppliedKey(t, "loop_k")
	}

	cl.rec.waitFor(t, `"iterations":5`)
	events := activityFrames(t, cl.rec.contents())
	var kFrames int
	for _, ev := range events {
		if ev.LoopID == "loop_k" {
			kFrames++
			require.NotNil(t, ev.Data)
		}
	}
	assert.Equal(t, 1, kFrames, "five writes in one window must coalesce to one event carrying the newest state")
	last := events[len(events)-1]
	assert.Equal(t, 5, last.Data.Iterations)
	assert.Equal(t, "loop_updated", last.Type, "coalesced event type derives from the delivered revision")
}
