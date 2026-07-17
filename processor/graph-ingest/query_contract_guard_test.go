package graphingest

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/nats-io/nats.go/jetstream"
)

func TestEntityStateGuardRejectsUnrelatedPoisonAndAllQueries(t *testing.T) {
	t.Parallel()

	watcher := newIngestGuardWatcher(4)
	watcher.updates <- &mockKVEntry{key: "unrelated", data: []byte(`{"id":"acme.ops.robotics.gcs.sensor.001","triples":[{"subject":"acme.ops.robotics.gcs.sensor.001","predicate":"legacy.predicate","object":1}]}`)} // predicate-audit:invalid {"kind":"stored-predicate","value":"legacy.predicate","reason":"arity"}
	watcher.updates <- nil
	bucket := newMockKVBucket()
	bucket.watchAllFactory = func() (jetstream.KeyWatcher, error) { return watcher, nil }
	c := newIngestGuardTestComponent(bucket)

	c.startEntityStateGuard(context.Background(), bucket)

	for name, query := range map[string]func() ([]byte, error){
		"entity": func() ([]byte, error) {
			return c.handleQueryEntityNATS(context.Background(), []byte(`{"id":"acme.ops.robotics.gcs.sensor.999"}`))
		},
		"batch-empty": func() ([]byte, error) {
			return c.handleQueryBatchNATS(context.Background(), []byte(`{"ids":[]}`))
		},
		"prefix": func() ([]byte, error) {
			return c.handleQueryPrefixNATS(context.Background(), []byte(`{"prefix":"acme"}`))
		},
		"suffix": func() ([]byte, error) {
			return c.handleQuerySuffixNATS(context.Background(), []byte(`{"suffix":"999"}`))
		},
	} {
		t.Run(name, func(t *testing.T) {
			result, err := query()
			if result != nil {
				t.Fatalf("result = %s, want nil", result)
			}
			assertIngestResetRequired(t, err)
		})
	}
}

func TestEntityStateGuardBootstrapAndTransportFailureAreTransient(t *testing.T) {
	t.Parallel()

	watcher := newIngestGuardWatcher(2)
	bucket := newMockKVBucket()
	bucket.watchAllFactory = func() (jetstream.KeyWatcher, error) { return watcher, nil }
	c := newIngestGuardTestComponent(bucket)

	done := make(chan struct{})
	go func() {
		c.startEntityStateGuard(context.Background(), bucket)
		close(done)
	}()
	waitForIngestCondition(t, c.entityBootstrapStarted.Load)
	assertIngestNotReady(t, c.ensureEntityQueriesReady())
	watcher.updates <- nil
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("guard did not finish synchronous bootstrap")
	}
	if err := c.ensureEntityQueriesReady(); err != nil {
		t.Fatalf("valid bootstrap remained gated: %v", err)
	}
	close(watcher.updates)
	waitForIngestCondition(t, c.entityWatchLost.Load)
	assertIngestNotReady(t, c.ensureEntityQueriesReady())
	if c.entityStatePoison.Load() != nil {
		t.Fatal("transport failure mislabeled reset-required")
	}
}

func TestEntityStateGuardLivePoisonIsStickyAndNoPartialBatchEscapes(t *testing.T) {
	t.Parallel()

	watcher := newIngestGuardWatcher(4)
	watcher.updates <- nil
	bucket := newMockKVBucket()
	bucket.watchAllFactory = func() (jetstream.KeyWatcher, error) { return watcher, nil }
	bucket.data["acme.ops.robotics.gcs.sensor.001"] = mockKVData{value: []byte(`{"id":"acme.ops.robotics.gcs.sensor.001","triples":[]}`), revision: 1}
	c := newIngestGuardTestComponent(bucket)

	c.startEntityStateGuard(context.Background(), bucket)
	watcher.updates <- &mockKVEntry{key: "unrelated", data: []byte(`not-json`)}
	waitForIngestCondition(t, func() bool { return c.entityStatePoison.Load() != nil })

	result, err := c.handleQueryBatchNATS(context.Background(), []byte(`{"ids":["acme.ops.robotics.gcs.sensor.001"]}`))
	if result != nil {
		t.Fatalf("partial result = %s, want nil", result)
	}
	assertIngestResetRequired(t, err)

	// A later valid entry cannot clear process-lifetime poison.
	watcher.updates <- &mockKVEntry{key: "unrelated", data: []byte(`{"id":"acme.ops.robotics.gcs.sensor.002","triples":[]}`)}
	assertIngestResetRequired(t, c.ensureEntityQueriesReady())
}

func TestEntityStateGuardTreatsDeleteAndPurgeAsCleanTombstones(t *testing.T) {
	t.Parallel()
	for _, op := range []jetstream.KeyValueOp{jetstream.KeyValueDelete, jetstream.KeyValuePurge} {
		t.Run(op.String(), func(t *testing.T) {
			bucket := newMockKVBucket()
			c := newIngestGuardTestComponent(bucket)
			c.entityBootstrapStarted.Store(true)
			c.entityBootstrapComplete.Store(true)
			c.validateEntityStateGuardEntry(&mockKVEntry{key: "gone", revision: 9, op: op})
			if state := c.entityStatePoison.Load(); state != nil {
				t.Fatalf("%s tombstone latched reset-required: %v", op, state)
			}
			if err := c.ensureEntityQueriesReady(); err != nil {
				t.Fatalf("%s tombstone degraded readiness: %v", op, err)
			}
		})
	}
}

func TestBatchBackendFailureReturnsNoPartialEntities(t *testing.T) {
	t.Parallel()

	valid := []byte(`{"id":"acme.ops.robotics.gcs.sensor.001","triples":[]}`)
	bucket := newMockKVBucket()
	bucket.getFunc = func(_ context.Context, key string) (jetstream.KeyValueEntry, error) {
		if key == "acme.ops.robotics.gcs.sensor.001" {
			return &mockKVEntry{key: key, data: valid}, nil
		}
		return nil, errors.New("backend unavailable")
	}
	c := newIngestGuardTestComponent(bucket)

	result, err := c.handleQueryBatchNATS(context.Background(), []byte(
		`{"ids":["acme.ops.robotics.gcs.sensor.001","acme.ops.robotics.gcs.sensor.002"]}`,
	))
	if result != nil {
		t.Fatalf("partial result = %s, want nil", result)
	}
	var classified *errs.ClassifiedError
	if !errors.As(err, &classified) || classified.Class != errs.ErrorTransient {
		t.Fatalf("error = %T %v, want transient backend failure", err, err)
	}
}

func TestQueryDiscoveredPoisonBlocksConcurrentReadyResponse(t *testing.T) {
	t.Parallel()

	validID := "acme.ops.robotics.gcs.sensor.valid"
	poisonID := "acme.ops.robotics.gcs.sensor.poison"
	valid := []byte(`{"id":"acme.ops.robotics.gcs.sensor.valid","triples":[]}`)
	poison := []byte(`{"id":"acme.ops.robotics.gcs.sensor.poison","triples":[{"subject":"acme.ops.robotics.gcs.sensor.poison","predicate":"legacy.predicate","object":1}]}`) // predicate-audit:invalid {"kind":"stored-predicate","value":"legacy.predicate","reason":"arity"}
	bucket := newMockKVBucket()
	bucket.getFunc = func(_ context.Context, key string) (jetstream.KeyValueEntry, error) {
		switch key {
		case validID:
			return &mockKVEntry{key: key, data: valid}, nil
		case poisonID:
			return &mockKVEntry{key: key, data: poison}, nil
		default:
			return nil, jetstream.ErrKeyNotFound
		}
	}
	c := newIngestGuardTestComponent(bucket)

	validAtResponseBarrier := make(chan struct{})
	releaseValid := make(chan struct{})
	c.beforeEntityQueryResponse = func(data []byte) {
		if string(data) != string(valid) {
			return
		}
		close(validAtResponseBarrier)
		<-releaseValid
	}

	type result struct {
		data []byte
		err  error
	}
	validResult := make(chan result, 1)
	go func() {
		data, err := c.handleQueryEntityNATS(context.Background(), []byte(`{"id":"`+validID+`"}`))
		validResult <- result{data: data, err: err}
	}()
	<-validAtResponseBarrier

	poisonResult := make(chan result, 1)
	go func() {
		data, err := c.handleQueryEntityNATS(context.Background(), []byte(`{"id":"`+poisonID+`"}`))
		poisonResult <- result{data: data, err: err}
	}()
	poisoned := <-poisonResult
	if poisoned.data != nil {
		t.Fatalf("poison query result = %s, want nil", poisoned.data)
	}
	assertIngestResetRequired(t, poisoned.err)

	close(releaseValid)
	concurrent := <-validResult
	if concurrent.data != nil {
		t.Fatalf("concurrent valid result escaped after poison latch: %s", concurrent.data)
	}
	assertIngestResetRequired(t, concurrent.err)
}

func newIngestGuardTestComponent(bucket *mockKVBucket) *Component {
	c := &Component{
		entityBucket: bucketStoreForTest(bucket),
		logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		running:      true,
	}
	c.lastActivity.Store(time.Now())
	return c
}

// bucketStoreForTest uses the package's existing test client helper.
func bucketStoreForTest(bucket jetstream.KeyValue) *natsclient.KVStore {
	client, err := natsclient.NewClient("nats://localhost:4222")
	if err != nil {
		panic(err)
	}
	return client.NewKVStore(bucket)
}

type ingestGuardWatcher struct {
	updates chan jetstream.KeyValueEntry
}

func newIngestGuardWatcher(capacity int) *ingestGuardWatcher {
	return &ingestGuardWatcher{updates: make(chan jetstream.KeyValueEntry, capacity)}
}

func (w *ingestGuardWatcher) Updates() <-chan jetstream.KeyValueEntry { return w.updates }
func (w *ingestGuardWatcher) Stop() error                             { return nil }

func assertIngestResetRequired(t *testing.T, err error) {
	t.Helper()
	var classified *errs.ClassifiedError
	if !errors.As(err, &classified) || classified.Class != errs.ErrorFatal || classified.Code != graph.ErrorCodeGraphStateResetRequired {
		t.Fatalf("error = %T %v, want fatal/%s", err, err, graph.ErrorCodeGraphStateResetRequired)
	}
}

func assertIngestNotReady(t *testing.T, err error) {
	t.Helper()
	var classified *errs.ClassifiedError
	if !errors.As(err, &classified) || classified.Class != errs.ErrorTransient || classified.Code != graph.ErrorCodeIndexNotReady {
		t.Fatalf("error = %T %v, want transient/%s", err, err, graph.ErrorCodeIndexNotReady)
	}
}

func waitForIngestCondition(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for !condition() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !condition() {
		t.Fatal("condition was not met")
	}
}
