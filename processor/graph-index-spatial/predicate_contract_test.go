package graphindexspatial

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
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/nats-io/nats.go/jetstream"
)

func TestExtractGeoCoordinatesIgnoresBarePredicateAliases(t *testing.T) {
	t.Parallel()
	c := &Component{}
	lat, lon, alt := c.extractGeoCoordinates([]message.Triple{
		{Predicate: "geo.location.latitude", Object: 40.0},
		{Predicate: "geo.location.longitude", Object: -100.0},
		{Predicate: "geo.location.altitude", Object: 1200.0},
		{Predicate: "latitude", Object: 1.0},  // predicate-audit:invalid {"kind":"stored-predicate","value":"latitude","reason":"arity"}
		{Predicate: "longitude", Object: 2.0}, // predicate-audit:invalid {"kind":"stored-predicate","value":"longitude","reason":"arity"}
		{Predicate: "altitude", Object: 3.0},  // predicate-audit:invalid {"kind":"stored-predicate","value":"altitude","reason":"arity"}
	})
	if lat == nil || *lat != 40 || lon == nil || *lon != -100 || alt == nil || *alt != 1200 {
		t.Fatalf("coordinates = (%v, %v, %v), want canonical (40, -100, 1200)", lat, lon, alt)
	}
}

func TestPredicatePoisonProducesNoSpatialOutputAndBlocksQueries(t *testing.T) {
	t.Parallel()

	bucket := newMockKVBucket()
	c := &Component{
		logger:            slog.New(slog.NewTextHandler(io.Discard, nil)),
		spatialBucket:     bucket,
		lifecycleReporter: component.NewNoOpLifecycleReporter(),
	}
	poisoned := []byte(`{"id":"acme.ops.robotics.gcs.drone.001","triples":[{"subject":"acme.ops.robotics.gcs.drone.001","predicate":"legacy.predicate","object":1}]}`) // predicate-audit:invalid {"kind":"stored-predicate","value":"legacy.predicate","reason":"arity"}
	c.processEntityUpdate(context.Background(), &mockKVEntry{data: poisoned})

	if c.resetState.Load() == nil {
		t.Fatal("spatial projection did not latch reset-required")
	}
	if len(bucket.data) != 0 {
		t.Fatalf("spatial projection emitted %d rows from poisoned state", len(bucket.data))
	}

	_, err := c.handleQueryBoundsNATS(context.Background(), []byte(`{}`))
	var classified *errs.ClassifiedError
	if !errors.As(err, &classified) {
		t.Fatalf("bounds error = %T %v, want classified reset-required", err, err)
	}
	if classified.Class != errs.ErrorFatal || classified.Code != graph.ErrorCodeGraphStateResetRequired {
		t.Fatalf("classified bounds error = %#v, want fatal %q", classified, graph.ErrorCodeGraphStateResetRequired)
	}
	var contractErr *graph.StateContractError
	if !errors.As(err, &contractErr) || contractErr.Reason != graph.GraphStateReasonNoncanonicalPredicate {
		t.Fatalf("bounds error = %v, want bounded noncanonical-predicate reason", err)
	}
}

func TestSpatialCellBackendFailureIsNotEmptySuccess(t *testing.T) {
	t.Parallel()
	bucket := newMockKVBucket()
	bucket.getFunc = func(context.Context, string) (jetstream.KeyValueEntry, error) {
		return nil, errors.New("spatial backend unavailable")
	}
	c := newBootstrapTestComponent(bucket)

	results, err := c.collectSpatialResults(context.Background(), []string{"geo_7_1_1"}, boundsRequest{Limit: 10})
	if err == nil {
		t.Fatalf("results = %#v, want backend error", results)
	}
}

func TestWatchAllBootstrapRejectsPoisonAtomicallyInEitherOrder(t *testing.T) {
	t.Parallel()

	valid := []byte(`{"id":"acme.ops.robotics.gcs.drone.001","triples":[{"subject":"acme.ops.robotics.gcs.drone.001","predicate":"geo.location.latitude","object":40},{"subject":"acme.ops.robotics.gcs.drone.001","predicate":"geo.location.longitude","object":-100}]}`)
	poison := []byte(`{"id":"acme.ops.robotics.gcs.drone.002","triples":[{"subject":"acme.ops.robotics.gcs.drone.002","predicate":"legacy.predicate","object":1}]}`) // predicate-audit:invalid {"kind":"stored-predicate","value":"legacy.predicate","reason":"arity"}

	for _, tc := range []struct {
		name     string
		entries  [][]byte
		wantRows int
	}{
		{name: "valid then poison", entries: [][]byte{valid, poison}, wantRows: 1},
		{name: "poison then valid", entries: [][]byte{poison, valid}, wantRows: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			output := newMockKVBucket()
			watcher := newBootstrapWatcher(tc.entries...)
			close(watcher.updates)
			bucket := &bootstrapWatchBucket{mockKVBucket: newMockKVBucket(), watcher: watcher}
			c := newBootstrapTestComponent(output)

			c.wg.Add(1)
			c.watchEntityStates(context.Background(), bucket)

			if got := outputSize(output); got != tc.wantRows {
				t.Fatalf("private bootstrap rows = %d, want %d for ordering", got, tc.wantRows)
			}
			if c.bootstrapComplete.Load() {
				t.Fatal("poisoned bootstrap was marked complete")
			}
			assertResetRequiredCode(t, c.ensureBootstrapReady())
		})
	}
}

func TestSpatialQueriesWaitForValidatedBootstrap(t *testing.T) {
	t.Parallel()

	output := newMockKVBucket()
	watcher := &bootstrapWatcher{updates: make(chan jetstream.KeyValueEntry, 2)}
	bucket := &bootstrapWatchBucket{mockKVBucket: newMockKVBucket(), watcher: watcher}
	c := newBootstrapTestComponent(output)
	c.bootstrapStarted.Store(true)
	ctx, cancel := context.WithCancel(context.Background())
	c.wg.Add(1)
	go c.watchEntityStates(ctx, bucket)

	watcher.updates <- &bootstrapEntry{key: "acme.ops.robotics.gcs.drone.001", data: []byte(`{"id":"acme.ops.robotics.gcs.drone.001","triples":[]}`)}
	assertClassifiedCode(t, c.ensureBootstrapReady(), errs.ErrorTransient, graph.ErrorCodeIndexNotReady)
	if _, err := c.handleQueryBoundsNATS(context.Background(), []byte(`{}`)); err == nil {
		t.Fatal("spatial query served before WatchAll bootstrap nil marker")
	} else {
		assertClassifiedCode(t, err, errs.ErrorTransient, graph.ErrorCodeIndexNotReady)
	}

	watcher.updates <- nil
	waitForAtomic(t, &c.bootstrapComplete)
	if _, err := c.handleQueryBoundsNATS(context.Background(), []byte(`{}`)); err != nil {
		t.Fatalf("spatial query remained gated after valid bootstrap: %v", err)
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

func TestSpatialWatcherClosureFailsClosed(t *testing.T) {
	t.Parallel()

	watcher := &bootstrapWatcher{updates: make(chan jetstream.KeyValueEntry)}
	close(watcher.updates)
	c := newBootstrapTestComponent(newMockKVBucket())
	c.running = true
	c.wg.Add(1)
	c.watchEntityStates(context.Background(), &bootstrapWatchBucket{mockKVBucket: newMockKVBucket(), watcher: watcher})
	assertClassifiedCode(t, c.ensureBootstrapReady(), errs.ErrorTransient, graph.ErrorCodeIndexNotReady)
	if state := c.resetState.Load(); state != nil {
		t.Fatalf("watcher transport closure mislabeled graph poison: %v", state)
	}
	health := c.Health()
	if health.Healthy || health.Status != graph.IndexStateDegraded {
		t.Fatalf("closure health = %#v, want unhealthy/degraded", health)
	}
}

func TestSpatialWatcherStartFailureIsTransient(t *testing.T) {
	t.Parallel()

	c := newBootstrapTestComponent(newMockKVBucket())
	c.wg.Add(1)
	c.watchEntityStates(context.Background(), newMockKVBucket())
	assertClassifiedCode(t, c.ensureBootstrapReady(), errs.ErrorTransient, graph.ErrorCodeIndexNotReady)
	if state := c.resetState.Load(); state != nil {
		t.Fatalf("watcher startup failure mislabeled graph poison: %v", state)
	}
}

func TestSpatialBootstrapTreatsDeleteAndPurgeAsTombstones(t *testing.T) {
	t.Parallel()
	for _, op := range []jetstream.KeyValueOp{jetstream.KeyValueDelete, jetstream.KeyValuePurge} {
		t.Run(op.String(), func(t *testing.T) {
			watcher := &bootstrapWatcher{updates: make(chan jetstream.KeyValueEntry, 2)}
			watcher.updates <- &bootstrapEntry{key: "gone", revision: 4, op: op}
			watcher.updates <- nil
			close(watcher.updates)
			c := newBootstrapTestComponent(newMockKVBucket())
			c.wg.Add(1)
			c.watchEntityStates(context.Background(), &bootstrapWatchBucket{mockKVBucket: newMockKVBucket(), watcher: watcher})
			if state := c.resetState.Load(); state != nil {
				t.Fatalf("%s tombstone latched reset-required: %v", op, state)
			}
			if !c.bootstrapComplete.Load() {
				t.Fatalf("%s tombstone prevented clean bootstrap", op)
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

func newBootstrapTestComponent(output jetstream.KeyValue) *Component {
	return &Component{
		config:            DefaultConfig(),
		logger:            slog.New(slog.NewTextHandler(io.Discard, nil)),
		spatialBucket:     output,
		lifecycleReporter: component.NewNoOpLifecycleReporter(),
	}
}

func outputSize(bucket *mockKVBucket) int {
	bucket.mu.Lock()
	defer bucket.mu.Unlock()
	return len(bucket.data)
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

func assertResetRequiredCode(t *testing.T, err error) {
	t.Helper()
	assertClassifiedCode(t, err, errs.ErrorFatal, graph.ErrorCodeGraphStateResetRequired)
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
