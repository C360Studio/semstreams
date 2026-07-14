package graphindextemporal

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/nats-io/nats.go/jetstream"
)

func TestPredicatePoisonProducesNoTemporalOutputAndBlocksQueries(t *testing.T) {
	t.Parallel()

	c, temporalBucket, reverseBucket := newEventTimeTestComponent()
	poisoned := []byte(`{"id":"acme.ops.robotics.gcs.drone.001","updated_at":"2026-07-14T00:00:00Z","triples":[{"subject":"acme.ops.robotics.gcs.drone.001","predicate":"legacy.predicate","object":"old"}]}`)
	c.processEntityUpdate(context.Background(), &mockKVEntry{data: poisoned})

	if c.resetState.Load() == nil {
		t.Fatal("temporal projection did not latch reset-required")
	}
	if len(temporalBucket.data) != 0 || len(reverseBucket.data) != 0 {
		t.Fatalf("temporal projection emitted rows from poisoned state: temporal=%d reverse=%d",
			len(temporalBucket.data), len(reverseBucket.data))
	}

	_, err := c.handleQueryRangeNATS(context.Background(), []byte(`{}`))
	var classified *errs.ClassifiedError
	if !errors.As(err, &classified) {
		t.Fatalf("range error = %T %v, want classified reset-required", err, err)
	}
	if classified.Class != errs.ErrorFatal || classified.Code != graph.ErrorCodeGraphStateResetRequired {
		t.Fatalf("classified range error = %#v, want fatal %q", classified, graph.ErrorCodeGraphStateResetRequired)
	}
	var contractErr *graph.StateContractError
	if !errors.As(err, &contractErr) || contractErr.Reason != graph.GraphStateReasonNoncanonicalPredicate {
		t.Fatalf("range error = %v, want bounded noncanonical-predicate reason", err)
	}
}

func TestTemporalBucketBackendFailureIsNotEmptySuccess(t *testing.T) {
	t.Parallel()
	c, temporalBucket, _ := newEventTimeTestComponent()
	temporalBucket.getFunc = func(context.Context, string) (jetstream.KeyValueEntry, error) {
		return nil, errors.New("temporal backend unavailable")
	}
	start := time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC)

	results, err := c.collectTemporalResults(context.Background(), []string{"2026.07.14.00"}, start, start.Add(time.Hour), 10)
	if err == nil {
		t.Fatalf("results = %#v, want backend error", results)
	}
}

func TestWatchAllBootstrapRejectsPoisonAtomicallyInEitherOrder(t *testing.T) {
	t.Parallel()

	valid := []byte(`{"id":"acme.ops.robotics.gcs.drone.001","updated_at":"2026-07-14T00:00:00Z","triples":[]}`)
	poison := []byte(`{"id":"acme.ops.robotics.gcs.drone.002","updated_at":"2026-07-14T00:00:00Z","triples":[{"subject":"acme.ops.robotics.gcs.drone.002","predicate":"legacy.predicate","object":1}]}`)

	for _, tc := range []struct {
		name     string
		entries  [][]byte
		wantRows int
	}{
		{name: "valid then poison", entries: [][]byte{valid, poison}, wantRows: 2},
		{name: "poison then valid", entries: [][]byte{poison, valid}, wantRows: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, temporalBucket, reverseBucket := newEventTimeTestComponent()
			watcher := newBootstrapWatcher(tc.entries...)
			close(watcher.updates)
			bucket := &bootstrapWatchBucket{mockKVBucket: newMockKVBucket(), watcher: watcher}

			c.wg.Add(1)
			c.watchEntityStates(context.Background(), bucket)

			if got := temporalOutputSize(temporalBucket) + temporalOutputSize(reverseBucket); got != tc.wantRows {
				t.Fatalf("private bootstrap rows = %d, want %d for ordering", got, tc.wantRows)
			}
			if c.bootstrapComplete.Load() {
				t.Fatal("poisoned bootstrap was marked complete")
			}
			assertResetRequiredCode(t, c.ensureBootstrapReady())
		})
	}
}

func TestTemporalQueriesWaitForValidatedBootstrap(t *testing.T) {
	t.Parallel()

	c, _, _ := newEventTimeTestComponent()
	c.bootstrapStarted.Store(true)
	watcher := &bootstrapWatcher{updates: make(chan jetstream.KeyValueEntry, 2)}
	bucket := &bootstrapWatchBucket{mockKVBucket: newMockKVBucket(), watcher: watcher}
	ctx, cancel := context.WithCancel(context.Background())
	c.wg.Add(1)
	go c.watchEntityStates(ctx, bucket)

	watcher.updates <- &bootstrapEntry{key: "acme.ops.robotics.gcs.drone.001", data: []byte(`{"id":"acme.ops.robotics.gcs.drone.001","updated_at":"2026-07-14T00:00:00Z","triples":[]}`), revision: 1}
	assertClassifiedCode(t, c.ensureBootstrapReady(), errs.ErrorTransient, graph.ErrorCodeIndexNotReady)
	query := []byte(`{"startTime":"2026-07-13T00:00:00Z","endTime":"2026-07-15T00:00:00Z"}`)
	if _, err := c.handleQueryRangeNATS(context.Background(), query); err == nil {
		t.Fatal("temporal query served before WatchAll bootstrap nil marker")
	} else {
		assertClassifiedCode(t, err, errs.ErrorTransient, graph.ErrorCodeIndexNotReady)
	}

	watcher.updates <- nil
	waitForAtomic(t, &c.bootstrapComplete)
	if _, err := c.handleQueryRangeNATS(context.Background(), query); err != nil {
		t.Fatalf("temporal query remained gated after valid bootstrap: %v", err)
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

func TestTemporalWatcherClosureFailsClosed(t *testing.T) {
	t.Parallel()

	c, _, _ := newEventTimeTestComponent()
	c.running = true
	watcher := &bootstrapWatcher{updates: make(chan jetstream.KeyValueEntry)}
	close(watcher.updates)
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

func TestTemporalWatcherStartFailureIsTransient(t *testing.T) {
	t.Parallel()

	c, _, _ := newEventTimeTestComponent()
	c.wg.Add(1)
	c.watchEntityStates(context.Background(), newMockKVBucket())
	assertClassifiedCode(t, c.ensureBootstrapReady(), errs.ErrorTransient, graph.ErrorCodeIndexNotReady)
	if state := c.resetState.Load(); state != nil {
		t.Fatalf("watcher startup failure mislabeled graph poison: %v", state)
	}
}

func TestTemporalBootstrapTreatsDeleteAndPurgeAsTombstones(t *testing.T) {
	t.Parallel()
	for _, op := range []jetstream.KeyValueOp{jetstream.KeyValueDelete, jetstream.KeyValuePurge} {
		t.Run(op.String(), func(t *testing.T) {
			watcher := &bootstrapWatcher{updates: make(chan jetstream.KeyValueEntry, 2)}
			watcher.updates <- &bootstrapEntry{key: "gone", revision: 4, op: op}
			watcher.updates <- nil
			close(watcher.updates)
			c, _, _ := newEventTimeTestComponent()
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

func temporalOutputSize(bucket *mockKVBucket) int {
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
