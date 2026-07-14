package oasfgenerator

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/nats-io/nats.go/jetstream"
)

func TestContractGuardRejectsPoisonOutsideSelectionPattern(t *testing.T) {
	t.Parallel()

	guard := newOASFTestWatcher(4)
	guard.updates <- &oasfTestEntry{
		key:  "acme.ops.robotics.gcs.sensor.001",
		data: []byte(`{"id":"acme.ops.robotics.gcs.sensor.001","triples":[{"subject":"acme.ops.robotics.gcs.sensor.001","predicate":"legacy.predicate","object":1}]}`),
	}
	guard.updates <- nil

	selection := newOASFTestWatcher(1)
	selection.updates <- nil
	bucket := &oasfWatchBucket{guard: guard, selection: selection}
	c := newOASFGuardTestComponent()

	if err := c.startEntityWatches(c.ctx, bucket, bucket, true); err != nil {
		t.Fatalf("startEntityWatches() error = %v", err)
	}

	assertOASFResetRequired(t, c.outputReadinessError())
	if got := bucket.watchAllCalls.Load(); got != 1 {
		t.Fatalf("WatchAll calls = %d, want 1", got)
	}
	if got := bucket.watchCalls.Load(); got != 0 {
		t.Fatalf("pattern Watch calls = %d, want 0 after poisoned bootstrap", got)
	}
	if health := c.Health(); health.Healthy || health.Status != graph.IndexStateResetRequired {
		t.Fatalf("Health() = %#v, want unhealthy/reset_required", health)
	}
	c.stopWatchers()
}

func TestContractGuardStaysLiveAndSuppressesQueuedGenerationAfterPoison(t *testing.T) {
	t.Parallel()

	guard := newOASFTestWatcher(4)
	guard.updates <- nil
	selection := newOASFTestWatcher(4)
	selection.updates <- nil
	bucket := &oasfWatchBucket{guard: guard, selection: selection}
	c := newOASFGuardTestComponent()

	if err := c.startEntityWatches(c.ctx, bucket, bucket, true); err != nil {
		t.Fatalf("startEntityWatches() error = %v", err)
	}

	guard.updates <- &oasfTestEntry{
		key:  "unrelated.entity",
		data: []byte(`{"id":"acme.ops.robotics.gcs.sensor.002","triples":[{"subject":"acme.ops.robotics.gcs.sensor.002","predicate":"bad","object":1}]}`),
	}
	waitForOASFCondition(t, func() bool { return c.graphStatePoison.Load() != nil })

	generator := &Generator{readiness: c.outputReadinessError}
	assertOASFResetRequired(t, generator.GenerateForEntity(context.Background(), "selected.entity"))
	c.stopWatchers()
}

func TestContractWatcherLossIsTransientNotResetRequired(t *testing.T) {
	t.Parallel()

	guard := newOASFTestWatcher(2)
	guard.updates <- nil
	selection := newOASFTestWatcher(2)
	selection.updates <- nil
	bucket := &oasfWatchBucket{guard: guard, selection: selection}
	c := newOASFGuardTestComponent()

	if err := c.startEntityWatches(c.ctx, bucket, bucket, true); err != nil {
		t.Fatalf("startEntityWatches() error = %v", err)
	}
	close(guard.updates)
	waitForOASFCondition(t, c.entityWatchLost.Load)

	err := c.outputReadinessError()
	var classified *errs.ClassifiedError
	if !errors.As(err, &classified) || classified.Class != errs.ErrorTransient || classified.Code != graph.ErrorCodeIndexNotReady {
		t.Fatalf("readiness error = %T %v, want transient/%s", err, err, graph.ErrorCodeIndexNotReady)
	}
	var contractErr *graph.StateContractError
	if errors.As(err, &contractErr) {
		t.Fatalf("watch transport loss mislabeled as state poison: %v", contractErr)
	}
	c.stopWatchers()
}

func TestSelectionRevisionWaitsForEarlierUnrelatedGuardPoison(t *testing.T) {
	t.Parallel()

	guard := newOASFTestWatcher(4)
	guard.updates <- nil
	selection := newOASFTestWatcher(4)
	selection.updates <- nil
	bucket := &oasfWatchBucket{guard: guard, selection: selection}
	c := newOASFGuardTestComponent()
	queued := make(chan string, 1)
	c.queueGeneration = func(entityID string) { queued <- entityID }
	barrierReached := make(chan struct{})
	c.beforeSelectionBarrier = func(revision uint64) {
		if revision == 2 {
			close(barrierReached)
		}
	}

	if err := c.startEntityWatches(c.ctx, bucket, bucket, true); err != nil {
		t.Fatalf("startEntityWatches() error = %v", err)
	}

	selection.updates <- &oasfTestEntry{
		key:      "selected.agent.entity",
		revision: 2,
		data:     []byte(`{"id":"acme.ops.robotics.gcs.agent.002","triples":[]}`),
	}
	<-barrierReached
	select {
	case entityID := <-queued:
		t.Fatalf("selected revision queued before authoritative guard caught up: %s", entityID)
	default:
	}

	// The graph-wide guard then observes an earlier poison outside the selection
	// pattern. Revision 2 must not pass merely because its selected value is valid.
	guard.updates <- &oasfTestEntry{
		key:      "unrelated.entity",
		revision: 1,
		data:     []byte(`{"id":"acme.ops.robotics.gcs.sensor.001","triples":[{"subject":"acme.ops.robotics.gcs.sensor.001","predicate":"legacy.predicate","object":1}]}`),
	}
	waitForOASFCondition(t, func() bool { return c.graphStatePoison.Load() != nil })
	select {
	case entityID := <-queued:
		t.Fatalf("selected revision queued after earlier graph poison: %s", entityID)
	default:
	}
	c.cancel()
	c.stopWatchers()
}

func TestCustomSelectionBucketUsesBootstrapOnlyBarrier(t *testing.T) {
	t.Parallel()

	guard := newOASFTestWatcher(2)
	guard.updates <- nil
	selection := newOASFTestWatcher(3)
	selection.updates <- nil
	contractBucket := &oasfWatchBucket{guard: guard}
	selectionBucket := &oasfWatchBucket{selection: selection}
	c := newOASFGuardTestComponent()
	queued := make(chan string, 1)
	c.queueGeneration = func(entityID string) { queued <- entityID }

	if err := c.startEntityWatches(c.ctx, contractBucket, selectionBucket, false); err != nil {
		t.Fatalf("startEntityWatches() error = %v", err)
	}
	selection.updates <- &oasfTestEntry{
		key:      "custom.agent.entity",
		revision: 900,
		data:     []byte(`{"id":"acme.ops.robotics.gcs.agent.900","triples":[]}`),
	}
	select {
	case entityID := <-queued:
		if entityID != "custom.agent.entity" {
			t.Fatalf("queued entity = %q", entityID)
		}
	case <-time.After(time.Second):
		t.Fatal("custom-bucket selection incorrectly waited on incomparable authoritative revision")
	}
	c.cancel()
	c.stopWatchers()
}

func TestContractGuardAdvancesRevisionForDeleteAndPurgeWithoutPoison(t *testing.T) {
	t.Parallel()
	for i, op := range []jetstream.KeyValueOp{jetstream.KeyValueDelete, jetstream.KeyValuePurge} {
		t.Run(op.String(), func(t *testing.T) {
			c := newOASFGuardTestComponent()
			revision := uint64(i + 7)
			c.observeContractEntry(&oasfTestEntry{key: "gone", revision: revision, op: op})
			if state := c.graphStatePoison.Load(); state != nil {
				t.Fatalf("%s tombstone latched reset-required: %v", op, state)
			}
			if got := c.contractRevision.Load(); got != revision {
				t.Fatalf("%s watermark = %d, want %d", op, got, revision)
			}
			c.cancel()
		})
	}
}

func newOASFGuardTestComponent() *Component {
	ctx, cancel := context.WithCancel(context.Background())
	c := &Component{
		config:  Config{WatchPattern: "*.agent.*"},
		logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		ctx:     ctx,
		cancel:  cancel,
		running: true,
	}
	return c
}

type oasfTestWatcher struct {
	updates chan jetstream.KeyValueEntry
}

func newOASFTestWatcher(capacity int) *oasfTestWatcher {
	return &oasfTestWatcher{updates: make(chan jetstream.KeyValueEntry, capacity)}
}

func (w *oasfTestWatcher) Updates() <-chan jetstream.KeyValueEntry { return w.updates }
func (w *oasfTestWatcher) Stop() error                             { return nil }

type oasfTestEntry struct {
	key      string
	data     []byte
	revision uint64
	op       jetstream.KeyValueOp
}

func (e *oasfTestEntry) Bucket() string { return graph.BucketEntityStates }
func (e *oasfTestEntry) Key() string    { return e.key }
func (e *oasfTestEntry) Value() []byte  { return e.data }
func (e *oasfTestEntry) Revision() uint64 {
	if e.revision == 0 {
		return 1
	}
	return e.revision
}
func (e *oasfTestEntry) Created() time.Time { return time.Now() }
func (e *oasfTestEntry) Delta() uint64      { return 0 }
func (e *oasfTestEntry) Operation() jetstream.KeyValueOp {
	if e.op == 0 {
		return jetstream.KeyValuePut
	}
	return e.op
}

type oasfWatchBucket struct {
	jetstream.KeyValue
	name          string
	guard         jetstream.KeyWatcher
	selection     jetstream.KeyWatcher
	watchAllCalls atomic.Int64
	watchCalls    atomic.Int64
}

func (b *oasfWatchBucket) Bucket() string {
	if b.name != "" {
		return b.name
	}
	return graph.BucketEntityStates
}

func (b *oasfWatchBucket) WatchAll(context.Context, ...jetstream.WatchOpt) (jetstream.KeyWatcher, error) {
	b.watchAllCalls.Add(1)
	return b.guard, nil
}

func (b *oasfWatchBucket) Watch(context.Context, string, ...jetstream.WatchOpt) (jetstream.KeyWatcher, error) {
	b.watchCalls.Add(1)
	return b.selection, nil
}

func assertOASFResetRequired(t *testing.T, err error) {
	t.Helper()
	var classified *errs.ClassifiedError
	if !errors.As(err, &classified) || classified.Class != errs.ErrorFatal || classified.Code != graph.ErrorCodeGraphStateResetRequired {
		t.Fatalf("error = %T %v, want fatal/%s", err, err, graph.ErrorCodeGraphStateResetRequired)
	}
}

func waitForOASFCondition(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for !condition() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !condition() {
		t.Fatal("condition was not met")
	}
}
