package graphembedding

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/pkg/revlag"
	"github.com/nats-io/nats.go/jetstream"
)

func TestPredicatePoisonLatchesEmbeddingResetAndBlocksQueries(t *testing.T) {
	t.Parallel()

	c := &Component{
		logger:            slog.New(slog.NewTextHandler(io.Discard, nil)),
		lifecycleReporter: component.NewNoOpLifecycleReporter(),
	}
	poisoned := []byte(`{"id":"acme.ops.robotics.gcs.drone.001","triples":[{"subject":"acme.ops.robotics.gcs.drone.001","predicate":"legacy.predicate","object":"old"}]}`)
	c.queueEntityForEmbedding(context.Background(), "acme.ops.robotics.gcs.drone.001", 7, poisoned)

	status := c.computeEmbeddingStatus(context.Background())
	if status.Ready || status.State != graph.IndexStateResetRequired ||
		status.Code != graph.ErrorCodeGraphStateResetRequired ||
		status.Reason != string(graph.GraphStateReasonNoncanonicalPredicate) {
		t.Fatalf("embedding status = %#v, want noncanonical reset-required", status)
	}

	_, err := c.handleQuerySearchNATS(context.Background(), []byte(`{}`))
	var classified *errs.ClassifiedError
	if !errors.As(err, &classified) {
		t.Fatalf("search error = %T %v, want classified reset-required", err, err)
	}
	if classified.Class != errs.ErrorFatal || classified.Code != graph.ErrorCodeGraphStateResetRequired {
		t.Fatalf("classified search error = %#v, want fatal %q", classified, graph.ErrorCodeGraphStateResetRequired)
	}
}

func TestWatchAllBootstrapPoisonIsNeverQueryVisibleInEitherOrder(t *testing.T) {
	t.Parallel()

	valid := []byte(`{"id":"acme.ops.robotics.gcs.drone.001","triples":[]}`)
	poison := []byte(`{"id":"acme.ops.robotics.gcs.drone.002","triples":[{"subject":"acme.ops.robotics.gcs.drone.002","predicate":"legacy.predicate","object":1}]}`)

	for _, tc := range []struct {
		name          string
		entries       [][]byte
		wantWatermark uint64
	}{
		{name: "valid then poison", entries: [][]byte{valid, poison}, wantWatermark: 1},
		{name: "poison then valid", entries: [][]byte{poison, valid}, wantWatermark: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			watcher := newBootstrapWatcher(tc.entries...)
			close(watcher.updates)
			c := newBootstrapTestComponent()
			c.wg.Add(1)
			c.watchEntityStates(context.Background(), &bootstrapWatchBucket{mockKVBucket: newMockKVBucket(), watcher: watcher})

			if got := c.watermark.Indexed(); got != tc.wantWatermark {
				t.Fatalf("private bootstrap watermark = %d, want %d for ordering", got, tc.wantWatermark)
			}
			if c.bootstrapComplete.Load() {
				t.Fatal("poisoned bootstrap was marked complete")
			}
			assertClassifiedCode(t, c.ensureBootstrapReady(), errs.ErrorFatal, graph.ErrorCodeGraphStateResetRequired)
			status := c.computeEmbeddingStatus(context.Background())
			if status.Ready || status.State != graph.IndexStateResetRequired {
				t.Fatalf("poisoned private projection became visible: %#v", status)
			}
		})
	}
}

func TestEmbeddingQueriesWaitForValidatedBootstrap(t *testing.T) {
	t.Parallel()

	c := newBootstrapTestComponent()
	c.bootstrapStarted.Store(true)
	watcher := &bootstrapWatcher{updates: make(chan jetstream.KeyValueEntry, 2)}
	ctx, cancel := context.WithCancel(context.Background())
	c.wg.Add(1)
	go c.watchEntityStates(ctx, &bootstrapWatchBucket{mockKVBucket: newMockKVBucket(), watcher: watcher})

	watcher.updates <- &bootstrapEntry{key: "acme.ops.robotics.gcs.drone.001", data: []byte(`{"id":"acme.ops.robotics.gcs.drone.001","triples":[]}`), revision: 1}
	assertClassifiedCode(t, c.ensureBootstrapReady(), errs.ErrorTransient, graph.ErrorCodeIndexNotReady)
	if _, err := c.handleQuerySearchNATS(context.Background(), []byte(`{}`)); err == nil {
		t.Fatal("embedding query served before WatchAll bootstrap nil marker")
	} else {
		assertClassifiedCode(t, err, errs.ErrorTransient, graph.ErrorCodeIndexNotReady)
	}
	if status := c.computeEmbeddingStatus(context.Background()); status.Ready || status.State != graph.IndexStateBuilding {
		t.Fatalf("embedding status before bootstrap nil = %#v, want building", status)
	}

	watcher.updates <- nil
	waitForAtomic(t, &c.bootstrapComplete)
	if err := c.ensureBootstrapReady(); err != nil {
		t.Fatalf("embedding query remained gated after valid bootstrap: %v", err)
	}
	cancel()
	c.wg.Wait()
	if state := c.resetState.Load(); state != nil {
		t.Fatalf("normal watcher cancellation latched reset-required: %v", state)
	}
	if c.watchUnavailable.Load() {
		t.Fatal("normal watcher cancellation marked watcher unavailable")
	}
}

func TestEmbeddingWatcherClosureFailsClosed(t *testing.T) {
	t.Parallel()

	c := newBootstrapTestComponent()
	c.running = true
	watcher := &bootstrapWatcher{updates: make(chan jetstream.KeyValueEntry)}
	close(watcher.updates)
	c.wg.Add(1)
	c.watchEntityStates(context.Background(), &bootstrapWatchBucket{mockKVBucket: newMockKVBucket(), watcher: watcher})
	assertClassifiedCode(t, c.ensureBootstrapReady(), errs.ErrorTransient, graph.ErrorCodeIndexNotReady)
	if state := c.resetState.Load(); state != nil {
		t.Fatalf("watcher transport closure mislabeled graph poison: %v", state)
	}
	status := c.computeEmbeddingStatus(context.Background())
	if status.Ready || status.State != graph.IndexStateDegraded || status.Code != graph.ErrorCodeIndexNotReady {
		t.Fatalf("closure status = %#v, want degraded/index-not-ready", status)
	}
	health := c.Health()
	if health.Healthy || health.Status != graph.IndexStateDegraded {
		t.Fatalf("closure health = %#v, want unhealthy/degraded", health)
	}
}

func TestEmbeddingWatcherStartFailureIsTransient(t *testing.T) {
	t.Parallel()

	c := newBootstrapTestComponent()
	c.wg.Add(1)
	c.watchEntityStates(context.Background(), newMockKVBucket())
	assertClassifiedCode(t, c.ensureBootstrapReady(), errs.ErrorTransient, graph.ErrorCodeIndexNotReady)
	if state := c.resetState.Load(); state != nil {
		t.Fatalf("watcher startup failure mislabeled graph poison: %v", state)
	}
}

func TestEmbeddingBootstrapTreatsDeleteAndPurgeAsTerminalTombstones(t *testing.T) {
	t.Parallel()
	for _, op := range []jetstream.KeyValueOp{jetstream.KeyValueDelete, jetstream.KeyValuePurge} {
		t.Run(op.String(), func(t *testing.T) {
			watcher := &bootstrapWatcher{updates: make(chan jetstream.KeyValueEntry, 2)}
			watcher.updates <- &bootstrapEntry{key: "gone", revision: 4, op: op}
			watcher.updates <- nil
			close(watcher.updates)
			c := newBootstrapTestComponent()
			c.wg.Add(1)
			c.watchEntityStates(context.Background(), &bootstrapWatchBucket{mockKVBucket: newMockKVBucket(), watcher: watcher})
			if state := c.resetState.Load(); state != nil {
				t.Fatalf("%s tombstone latched reset-required: %v", op, state)
			}
			if !c.bootstrapComplete.Load() {
				t.Fatalf("%s tombstone prevented clean bootstrap", op)
			}
			if got := c.watermark.Indexed(); got != 4 {
				t.Fatalf("%s watermark = %d, want terminal revision 4", op, got)
			}
		})
	}
}

type bootstrapWatcher struct {
	updates chan jetstream.KeyValueEntry
}

func newBootstrapWatcher(values ...[]byte) *bootstrapWatcher {
	updates := make(chan jetstream.KeyValueEntry, len(values)+1)
	for i, value := range values {
		updates <- &bootstrapEntry{key: "entity." + string(rune('a'+i)), data: value, revision: uint64(i + 1)}
	}
	updates <- nil
	return &bootstrapWatcher{updates: updates}
}

func (w *bootstrapWatcher) Updates() <-chan jetstream.KeyValueEntry { return w.updates }
func (w *bootstrapWatcher) Stop() error                             { return nil }

type bootstrapWatchBucket struct {
	*mockKVBucket
	watcher jetstream.KeyWatcher
}

func (b *bootstrapWatchBucket) WatchAll(context.Context, ...jetstream.WatchOpt) (jetstream.KeyWatcher, error) {
	return b.watcher, nil
}

type bootstrapEntry struct {
	key      string
	data     []byte
	revision uint64
	op       jetstream.KeyValueOp
}

func (e *bootstrapEntry) Key() string        { return e.key }
func (e *bootstrapEntry) Value() []byte      { return e.data }
func (e *bootstrapEntry) Revision() uint64   { return e.revision }
func (e *bootstrapEntry) Created() time.Time { return time.Now() }
func (e *bootstrapEntry) Delta() uint64      { return 0 }
func (e *bootstrapEntry) Operation() jetstream.KeyValueOp {
	if e.op == 0 {
		return jetstream.KeyValuePut
	}
	return e.op
}
func (e *bootstrapEntry) Bucket() string { return graph.BucketEntityStates }

func newBootstrapTestComponent() *Component {
	return &Component{
		config:            DefaultConfig(),
		logger:            slog.New(slog.NewTextHandler(io.Discard, nil)),
		lifecycleReporter: component.NewNoOpLifecycleReporter(),
		watermark:         revlag.New(),
	}
}

func waitForAtomic(t *testing.T, value *atomic.Bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for !value.Load() {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for bootstrap state")
		}
		time.Sleep(time.Millisecond)
	}
}

func assertClassifiedCode(t *testing.T, err error, class errs.ErrorClass, code string) {
	t.Helper()
	var classified *errs.ClassifiedError
	if !errors.As(err, &classified) {
		t.Fatalf("error = %T %v, want classified %s/%s", err, err, class, code)
	}
	if classified.Class != class || classified.Code != code {
		t.Fatalf("classified error = %#v, want %s/%s", classified, class, code)
	}
}
