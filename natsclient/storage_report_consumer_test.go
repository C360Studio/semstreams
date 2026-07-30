package natsclient

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- fakes -------------------------------------------------------------------

// fakeKeyWatcher is one KV watch. The nil entry is reproduced faithfully
// because it is load-bearing: nats.go sends exactly one nil through Updates()
// when the initial values are done, and a consumer that treats it as a row
// would panic on the first sync.
type fakeKeyWatcher struct {
	updates chan jetstream.KeyValueEntry
	stopped chan struct{}
	once    sync.Once
}

func newFakeKeyWatcher() *fakeKeyWatcher {
	return &fakeKeyWatcher{
		updates: make(chan jetstream.KeyValueEntry, 32),
		stopped: make(chan struct{}),
	}
}

func (w *fakeKeyWatcher) Updates() <-chan jetstream.KeyValueEntry { return w.updates }

func (w *fakeKeyWatcher) Stop() error {
	w.once.Do(func() { close(w.stopped) })
	return nil
}

func (w *fakeKeyWatcher) put(key string, value any) {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	w.updates <- fakeEntry{key: key, value: encoded, op: jetstream.KeyValuePut, created: time.Now()}
}

func (w *fakeKeyWatcher) raw(key string, value []byte) {
	w.updates <- fakeEntry{key: key, value: value, op: jetstream.KeyValuePut, created: time.Now()}
}

func (w *fakeKeyWatcher) del(key string) {
	w.updates <- fakeEntry{key: key, op: jetstream.KeyValueDelete, created: time.Now()}
}

func (w *fakeKeyWatcher) synced() { w.updates <- nil }

// fakeWatchStore hands out watchers under test control, recording how many were
// requested so re-establishment after a dropped watch is provable.
type fakeWatchStore struct {
	mu       sync.Mutex
	watchers []*fakeKeyWatcher
	err      error
	next     func() *fakeKeyWatcher
}

func (s *fakeWatchStore) WatchAll(_ context.Context, _ ...jetstream.WatchOpt) (jetstream.KeyWatcher, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return nil, s.err
	}
	watcher := newFakeKeyWatcher()
	if s.next != nil {
		watcher = s.next()
	}
	s.watchers = append(s.watchers, watcher)
	return watcher, nil
}

func (s *fakeWatchStore) watchCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.watchers)
}

// recordingObserver captures the fan-out the metrics surface consumes.
type recordingObserver struct {
	mu       sync.Mutex
	observed []string
	forgot   []string
	accounts []AccountReport
}

func (o *recordingObserver) ObserveResource(row ResourceReport) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.observed = append(o.observed, row.Resource.Name)
}

func (o *recordingObserver) ForgetResource(row ResourceReport) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.forgot = append(o.forgot, row.Resource.Name)
}

func (o *recordingObserver) ObserveAccount(row AccountReport) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.accounts = append(o.accounts, row)
}

func (o *recordingObserver) snapshot() ([]string, []string, []AccountReport) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]string(nil), o.observed...),
		append([]string(nil), o.forgot...),
		append([]AccountReport(nil), o.accounts...)
}

// --- helpers -----------------------------------------------------------------

func reportRow(name string, tier StorageTier, pressure PressureState) ResourceReport {
	row := ResourceReport{
		Resource:    tieredResource(name, tier, 1<<30, 1<<20),
		CollectedAt: time.Now().UTC(),
		ProducedBy:  "unit-test",
		Growth:      UnknownGrowth(GrowthUnavailableNoPriorObservation),
	}
	if pressure != "" {
		row.Pressure = Pressure{Evaluated: true, State: pressure, RaisedBy: PressureInputHeadroom}
	}
	return row
}

func runConsumer(t *testing.T, store *fakeWatchStore, observer StorageReportObserver) *StorageReportConsumer {
	t.Helper()
	consumer, err := NewStorageReportConsumer(store, StorageReportConsumerConfig{
		Observer:     observer,
		RetryBackoff: time.Millisecond,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		consumer.Run(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("consumer did not stop when its context ended")
		}
	})
	return consumer
}

func eventually(t *testing.T, condition func() bool, msg string) {
	t.Helper()
	require.Eventually(t, condition, 3*time.Second, time.Millisecond, msg)
}

// --- tests -------------------------------------------------------------------

// TestReportConsumer_SnapshotIsBuiltFromTheBucket is the load-bearing property
// of this whole file: the operator surfaces read the PUBLISHED report rather
// than recomputing an inventory, so no two of them can disagree.
func TestReportConsumer_SnapshotIsBuiltFromTheBucket(t *testing.T) {
	store := &fakeWatchStore{}
	observer := &recordingObserver{}
	consumer := runConsumer(t, store, observer)

	eventually(t, func() bool { return store.watchCount() == 1 }, "the watch is established")
	watcher := store.watchers[0]

	watcher.put("LOGS", reportRow("LOGS", TierFile, PressureWarning))
	watcher.put("EVENTS", reportRow("EVENTS", TierFile, PressureNormal))
	watcher.put(StorageAccountReportKey, AccountReport{
		ProducedBy: "unit-test",
		Tiers:      []TierComparison{{Tier: TierFile, State: OvercommitmentOver}},
	})
	watcher.synced()

	eventually(t, func() bool { return consumer.Snapshot().Synced }, "the initial values complete")

	snapshot := consumer.Snapshot()
	require.Len(t, snapshot.Resources, 2)
	assert.Equal(t, "EVENTS", snapshot.Resources[0].Resource.Name, "rows are sorted by name")
	assert.Equal(t, "LOGS", snapshot.Resources[1].Resource.Name)
	assert.Equal(t, PressureWarning, snapshot.Resources[1].Pressure.State)

	require.True(t, snapshot.AccountKnown)
	file, ok := snapshot.Account.TierFor(TierFile)
	require.True(t, ok)
	assert.Equal(t, OvercommitmentOver, file.State)

	observed, _, accounts := observer.snapshot()
	assert.ElementsMatch(t, []string{"LOGS", "EVENTS"}, observed)
	require.Len(t, accounts, 1)
}

// TestReportConsumer_AccountRowIsNeverAResourceRow is the key discrimination.
// Decoding the reserved row as a resource would add a nameless row to every
// operator surface and, worse, publish metrics under an empty resource label.
func TestReportConsumer_AccountRowIsNeverAResourceRow(t *testing.T) {
	store := &fakeWatchStore{}
	observer := &recordingObserver{}
	consumer := runConsumer(t, store, observer)

	eventually(t, func() bool { return store.watchCount() == 1 }, "the watch is established")
	store.watchers[0].put(StorageAccountReportKey, AccountReport{ProducedBy: "unit-test"})
	store.watchers[0].synced()

	eventually(t, func() bool { return consumer.Snapshot().AccountKnown }, "the account row arrives")
	assert.Empty(t, consumer.Snapshot().Resources)

	observed, _, _ := observer.snapshot()
	assert.Empty(t, observed, "the account row is not fanned out as a resource")
}

// TestReportConsumer_DeleteForgetsTheRowWithItsIdentity covers series
// reclamation. The observer must learn WHICH row went away — the key may be an
// opaque token, so a bare key cannot identify the labels a metrics surface
// registered under.
func TestReportConsumer_DeleteForgetsTheRowWithItsIdentity(t *testing.T) {
	store := &fakeWatchStore{}
	observer := &recordingObserver{}
	consumer := runConsumer(t, store, observer)

	eventually(t, func() bool { return store.watchCount() == 1 }, "the watch is established")
	watcher := store.watchers[0]

	key, err := StorageReportKey("weird$stream")
	require.NoError(t, err)
	watcher.put(key, reportRow("weird$stream", TierFile, PressureNormal))
	watcher.synced()
	eventually(t, func() bool { return len(consumer.Snapshot().Resources) == 1 }, "the row arrives")

	watcher.del(key)
	eventually(t, func() bool { return len(consumer.Snapshot().Resources) == 0 }, "the row is dropped")

	_, forgot, _ := observer.snapshot()
	require.Len(t, forgot, 1)
	assert.Equal(t, "weird$stream", forgot[0],
		"the forgotten row carries the real resource name, not its opaque key")
}

// TestReportConsumer_DeleteOfAnUnknownKeyIsNotFannedOut keeps a tombstone for a
// row this process never saw from inventing a removal event.
func TestReportConsumer_DeleteOfAnUnknownKeyIsNotFannedOut(t *testing.T) {
	store := &fakeWatchStore{}
	observer := &recordingObserver{}
	consumer := runConsumer(t, store, observer)

	eventually(t, func() bool { return store.watchCount() == 1 }, "the watch is established")
	store.watchers[0].del("NEVER_SEEN")
	store.watchers[0].synced()

	eventually(t, func() bool { return consumer.Snapshot().Synced }, "the sync completes")
	_, forgot, _ := observer.snapshot()
	assert.Empty(t, forgot)
}

// TestReportConsumer_UndecodableRowIsSkippedNotFatal keeps one bad value from
// blanking a whole account's view.
func TestReportConsumer_UndecodableRowIsSkippedNotFatal(t *testing.T) {
	store := &fakeWatchStore{}
	observer := &recordingObserver{}
	consumer := runConsumer(t, store, observer)

	eventually(t, func() bool { return store.watchCount() == 1 }, "the watch is established")
	watcher := store.watchers[0]

	watcher.raw("CORRUPT", []byte("{not json"))
	watcher.put("GOOD", reportRow("GOOD", TierFile, PressureNormal))
	watcher.synced()

	eventually(t, func() bool { return len(consumer.Snapshot().Resources) == 1 }, "the good row survives")
	assert.Equal(t, "GOOD", consumer.Snapshot().Resources[0].Resource.Name)
}

// TestReportConsumer_ReestablishesADroppedWatch is why the consumer owns a loop
// rather than a single watch call: a watch that ends takes every operator
// surface silent with it, and the silence looks exactly like an account with
// nothing in it.
func TestReportConsumer_ReestablishesADroppedWatch(t *testing.T) {
	store := &fakeWatchStore{}
	consumer := runConsumer(t, store, nil)

	eventually(t, func() bool { return store.watchCount() == 1 }, "the first watch is established")
	close(store.watchers[0].updates)

	eventually(t, func() bool { return store.watchCount() >= 2 }, "a dropped watch is re-established")

	store.mu.Lock()
	second := store.watchers[1]
	store.mu.Unlock()
	second.put("LOGS", reportRow("LOGS", TierFile, PressureNormal))
	second.synced()

	eventually(t, func() bool { return len(consumer.Snapshot().Resources) == 1 },
		"the re-established watch repopulates the snapshot")
}

// TestReportConsumer_RetriesWhenTheWatchCannotBeEstablished covers the cold
// start where the bucket is not reachable yet.
func TestReportConsumer_RetriesWhenTheWatchCannotBeEstablished(t *testing.T) {
	store := &fakeWatchStore{err: errors.New("bucket not found")}
	consumer := runConsumer(t, store, nil)

	eventually(t, func() bool { return store.watchCount() == 0 && consumer.Snapshot().Synced == false },
		"nothing is claimed while the watch cannot be established")

	store.mu.Lock()
	store.err = nil
	store.mu.Unlock()

	eventually(t, func() bool { return store.watchCount() >= 1 }, "the retry succeeds once the bucket exists")
}

// TestReportConsumer_SnapshotIsACopy stops a caller ranging the snapshot from
// mutating the consumer's state under the watch goroutine.
func TestReportConsumer_SnapshotIsACopy(t *testing.T) {
	store := &fakeWatchStore{}
	consumer := runConsumer(t, store, nil)

	eventually(t, func() bool { return store.watchCount() == 1 }, "the watch is established")
	store.watchers[0].put("LOGS", reportRow("LOGS", TierFile, PressureNormal))
	store.watchers[0].synced()
	eventually(t, func() bool { return len(consumer.Snapshot().Resources) == 1 }, "the row arrives")

	snapshot := consumer.Snapshot()
	snapshot.Resources[0].Resource.Name = "TAMPERED"

	assert.Equal(t, "LOGS", consumer.Snapshot().Resources[0].Resource.Name)
}

// TestReportConsumer_PressureCountsComeFromThePublishedRows gives the health
// surface its summary without a second derivation: the counts are a tally of
// what the bucket says, never a re-evaluation of pressure.
func TestReportConsumer_PressureCountsComeFromThePublishedRows(t *testing.T) {
	store := &fakeWatchStore{}
	consumer := runConsumer(t, store, nil)

	eventually(t, func() bool { return store.watchCount() == 1 }, "the watch is established")
	watcher := store.watchers[0]

	watcher.put("A", reportRow("A", TierFile, PressureCritical))
	watcher.put("B", reportRow("B", TierFile, PressureWarning))
	watcher.put("C", reportRow("C", TierFile, PressureNormal))

	unbounded := reportRow("D", TierFile, "")
	unbounded.Resource.Bytes = NewCapacity(0, 1<<20, true)
	unbounded.Pressure = Pressure{Unavailable: PressureUnavailableUnbounded}
	watcher.put("D", unbounded)
	watcher.synced()

	eventually(t, func() bool { return len(consumer.Snapshot().Resources) == 4 }, "all rows arrive")

	snapshot := consumer.Snapshot()
	assert.Equal(t, 1, snapshot.PressureCounts[PressureCritical])
	assert.Equal(t, 1, snapshot.PressureCounts[PressureWarning])
	assert.Equal(t, 1, snapshot.PressureCounts[PressureNormal])
	assert.Equal(t, 0, snapshot.PressureCounts[PressureHigh])
	assert.Equal(t, 1, snapshot.NotEvaluated,
		"an unbounded resource carries no pressure state and must stay countable")
	assert.Equal(t, PressureCritical, snapshot.WorstPressure)

	// Every row lands in EXACTLY ONE bucket, and the state tally contains only
	// real states. Checking the named states alone would miss an unevaluated row
	// being tallied under the empty state as well as counted as unevaluated —
	// a phantom bucket that double-counts a resource and quietly gives the
	// no-state rows a state after all.
	tallied := 0
	for state, count := range snapshot.PressureCounts {
		assert.NotEqual(t, PressureState(""), state,
			"a row with no pressure state must not produce a count under an empty state")
		tallied += count
	}
	assert.Equal(t, len(snapshot.Resources), tallied+snapshot.NotEvaluated,
		"each row is counted once: as a state, or as not-evaluated, never both")
}

// TestReportConsumer_WorstPressureIsEmptyWhenNothingIsEvaluated keeps the
// summary from asserting `normal` over an account whose rows all declined to
// evaluate — the exact phantom this capability exists to remove.
func TestReportConsumer_WorstPressureIsEmptyWhenNothingIsEvaluated(t *testing.T) {
	store := &fakeWatchStore{}
	consumer := runConsumer(t, store, nil)

	eventually(t, func() bool { return store.watchCount() == 1 }, "the watch is established")

	unbounded := reportRow("D", TierFile, "")
	unbounded.Resource.Bytes = NewCapacity(0, 1<<20, true)
	unbounded.Pressure = Pressure{Unavailable: PressureUnavailableUnbounded}
	store.watchers[0].put("D", unbounded)
	store.watchers[0].synced()

	eventually(t, func() bool { return len(consumer.Snapshot().Resources) == 1 }, "the row arrives")
	assert.Equal(t, PressureState(""), consumer.Snapshot().WorstPressure)
	assert.Equal(t, 1, consumer.Snapshot().NotEvaluated)
}
