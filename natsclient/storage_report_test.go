package natsclient

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- fakes -------------------------------------------------------------------

// fakeEntry is one revision of one key, as the KV history returns it.
type fakeEntry struct {
	key      string
	value    []byte
	revision uint64
	created  time.Time
	op       jetstream.KeyValueOp
}

func (e fakeEntry) Bucket() string                  { return "STORAGE_REPORT_FAKE" }
func (e fakeEntry) Key() string                     { return e.key }
func (e fakeEntry) Value() []byte                   { return e.value }
func (e fakeEntry) Revision() uint64                { return e.revision }
func (e fakeEntry) Created() time.Time              { return e.created }
func (e fakeEntry) Delta() uint64                   { return 0 }
func (e fakeEntry) Operation() jetstream.KeyValueOp { return e.op }

type fakeKeyLister struct {
	keys []string
	stop chan struct{}
}

func (l *fakeKeyLister) Keys() <-chan string {
	ch := make(chan string, len(l.keys))
	for _, k := range l.keys {
		ch <- k
	}
	close(ch)
	return ch
}

func (l *fakeKeyLister) Stop() error {
	if l.stop != nil {
		close(l.stop)
	}
	return nil
}

// fakeReportStore is a KV bucket with a BOUNDED per-key history, because the
// bound is the point: the growth series lives in that history, and a fake with
// unlimited depth would hide a depth that is too shallow to carry it.
type fakeReportStore struct {
	mu      sync.Mutex
	history map[string][]fakeEntry
	order   []string
	rev     uint64
	depth   int

	putErr    error
	deleteErr error
	listErr   error
	// putErrForKey fails ONE key's write while the rest of the publication
	// succeeds, which is the shape a transient per-message NATS failure takes.
	putErrForKey map[string]error

	deletes []string
}

func newFakeReportStore(depth int) *fakeReportStore {
	return &fakeReportStore{history: map[string][]fakeEntry{}, depth: depth}
}

func (s *fakeReportStore) Put(_ context.Context, key string, value []byte) (uint64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.putErr != nil {
		return 0, s.putErr
	}
	if err, ok := s.putErrForKey[key]; ok {
		return 0, err
	}
	// A real bucket rejects an illegal key. A fake that accepts anything would
	// let a publisher that skipped key encoding pass every test here and fail
	// against a server.
	if err := ValidateKVLiteralKey(key); err != nil {
		return 0, err
	}
	if _, ok := s.history[key]; !ok {
		s.order = append(s.order, key)
	}
	s.rev++
	s.append(key, fakeEntry{key: key, value: value, revision: s.rev, created: time.Now(), op: jetstream.KeyValuePut})
	return s.rev, nil
}

func (s *fakeReportStore) Delete(_ context.Context, key string, _ ...jetstream.KVDeleteOpt) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.deleteErr != nil {
		return s.deleteErr
	}
	s.rev++
	s.append(key, fakeEntry{key: key, revision: s.rev, created: time.Now(), op: jetstream.KeyValueDelete})
	s.deletes = append(s.deletes, key)
	return nil
}

// append enforces the bucket's declared History depth.
func (s *fakeReportStore) append(key string, entry fakeEntry) {
	entries := append(s.history[key], entry)
	if s.depth > 0 && len(entries) > s.depth {
		entries = entries[len(entries)-s.depth:]
	}
	s.history[key] = entries
}

func (s *fakeReportStore) History(_ context.Context, key string, _ ...jetstream.WatchOpt) ([]jetstream.KeyValueEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, ok := s.history[key]
	if !ok || len(entries) == 0 {
		return nil, jetstream.ErrKeyNotFound
	}
	out := make([]jetstream.KeyValueEntry, 0, len(entries))
	for _, e := range entries {
		out = append(out, e)
	}
	return out, nil
}

func (s *fakeReportStore) ListKeys(_ context.Context, _ ...jetstream.WatchOpt) (jetstream.KeyLister, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listErr != nil {
		return nil, s.listErr
	}
	live := make([]string, 0, len(s.order))
	for _, key := range s.order {
		entries := s.history[key]
		if len(entries) == 0 || entries[len(entries)-1].op != jetstream.KeyValuePut {
			continue
		}
		live = append(live, key)
	}
	return &fakeKeyLister{keys: live}, nil
}

// liveRow decodes the current value of a key, as an operator surface would.
func (s *fakeReportStore) liveRow(t *testing.T, key string) ResourceReport {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	entries := s.history[key]
	require.NotEmpty(t, entries, "key %q has no revisions", key)
	last := entries[len(entries)-1]
	require.Equal(t, jetstream.KeyValuePut, last.op, "key %q is deleted", key)
	var row ResourceReport
	require.NoError(t, json.Unmarshal(last.value, &row))
	return row
}

func (s *fakeReportStore) liveKeys() []string {
	lister, _ := s.ListKeys(context.Background())
	var keys []string
	for key := range lister.Keys() {
		keys = append(keys, key)
	}
	return keys
}

// liveResourceKeys is liveKeys minus the reserved per-tier account row, for the
// assertions that mean "one key per RESOURCE". The exclusion is explicit rather
// than folded into liveKeys so a test asserting the bucket is entirely empty
// still sees the account row if one was wrongly written.
func (s *fakeReportStore) liveResourceKeys() []string {
	var keys []string
	for _, key := range s.liveKeys() {
		if key == StorageAccountReportKey {
			continue
		}
		keys = append(keys, key)
	}
	return keys
}

// --- helpers -----------------------------------------------------------------

func boundedResource(name string, limit, used int64) StorageResource {
	return StorageResource{
		Name:        name,
		Kind:        ResourceOrdinaryStream,
		Attribution: AttributionNotApplicable,
		Tier:        TierFile,
		Bytes:       NewCapacity(limit, used, true),
		Messages:    NewCapacity(0, 10, true),
	}
}

func inventoryAt(at time.Time, resources ...StorageResource) StorageInventory {
	return StorageInventory{
		ProducedBy:  "unit-test",
		CollectedAt: at,
		Resources:   resources,
	}
}

func newTestPublisher(t *testing.T, store ReportStore, source ThresholdSource) *StorageReportPublisher {
	t.Helper()
	publisher, err := NewStorageReportPublisher(store, StorageReportConfig{Thresholds: source})
	require.NoError(t, err)
	return publisher
}

func defaultSource() ThresholdSource {
	return func() StoragePressureThresholds { return DefaultStoragePressureThresholds() }
}

// --- tests -------------------------------------------------------------------

// TestStorageReportPublisher_OneKeyPerResource is the publication contract:
// ranging the bucket reconstructs the whole report, and each row carries its
// own collection timestamp because consumers may observe a mix of revisions.
//
// One key per resource rather than one report blob: a blob would be bounded by
// the NATS max payload on a large account, and it would make a disappeared
// resource unreclaimable except by rewriting the whole document.
func TestStorageReportPublisher_OneKeyPerResource(t *testing.T) {
	store := newFakeReportStore(10)
	publisher := newTestPublisher(t, store, defaultSource())
	collectedAt := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)

	result, err := publisher.Publish(context.Background(), inventoryAt(collectedAt,
		boundedResource("EVENTS", mib(1000), mib(100)),
		boundedResource("LOGS", mib(1000), mib(900)),
		StorageResource{
			Name:        KVStreamPrefix + "ENTITY_STATES_FAKE",
			Kind:        ResourceKeyValue,
			Bucket:      "ENTITY_STATES_FAKE",
			Attribution: AttributionAttributed,
			Owner:       "graph-ingest",
			Tier:        TierFile,
			Bytes:       NewCapacity(0, mib(50), true),
			Messages:    NewCapacity(0, 5, true),
		},
	))
	require.NoError(t, err)
	assert.Equal(t, 3, result.Published)
	assert.Equal(t, 0, result.Deleted)
	assert.False(t, result.Skipped)

	assert.ElementsMatch(t,
		[]string{"EVENTS", "LOGS", KVStreamPrefix + "ENTITY_STATES_FAKE"},
		store.liveResourceKeys(),
		"ranging the bucket reconstructs the whole report")

	row := store.liveRow(t, "LOGS")
	assert.Equal(t, "LOGS", row.Resource.Name)
	assert.Equal(t, collectedAt.UTC(), row.CollectedAt.UTC(),
		"each key carries the timestamp its data was collected at")
	assert.Equal(t, "unit-test", row.ProducedBy,
		"a fleet of processes each polling account-wide is unreconcilable without the producer")
	require.NotNil(t, row.Projection.HeadroomBytes)
	assert.Equal(t, mib(100), *row.Projection.HeadroomBytes)
	assert.True(t, row.Pressure.Evaluated)

	// The attribution the inventory resolved is carried through verbatim: the
	// report is a view of the inventory, not a second opinion about ownership.
	kvRow := store.liveRow(t, KVStreamPrefix+"ENTITY_STATES_FAKE")
	assert.Equal(t, "graph-ingest", kvRow.Resource.Owner)
	assert.Equal(t, AttributionAttributed, kvRow.Resource.Attribution)
	assert.False(t, kvRow.Pressure.Evaluated, "an unbounded resource has no pressure state")
	assert.Equal(t, PressureUnavailableUnbounded, kvRow.Pressure.Unavailable)
}

// TestStorageReportPublisher_DisappearedResourceIsDeletedNotExpired is the
// spec's reclamation scenario. The collector DECIDES a resource is gone and
// deletes its key; nothing ages it out. That is the same principle that governs
// the rest of the graph, and it is why the report bucket's catalog row declares
// no lifecycle retention — an expiry would make "this resource is gone" and
// "this row aged out" indistinguishable to every consumer.
func TestStorageReportPublisher_DisappearedResourceIsDeletedNotExpired(t *testing.T) {
	store := newFakeReportStore(10)
	publisher := newTestPublisher(t, store, defaultSource())
	first := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)

	_, err := publisher.Publish(context.Background(), inventoryAt(first,
		boundedResource("STAYS", mib(1000), mib(100)),
		boundedResource("GOES", mib(1000), mib(100)),
	))
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"STAYS", "GOES"}, store.liveResourceKeys())

	result, err := publisher.Publish(context.Background(), inventoryAt(first.Add(time.Minute),
		boundedResource("STAYS", mib(1000), mib(110)),
	))
	require.NoError(t, err)
	assert.Equal(t, 1, result.Published)
	assert.Equal(t, 1, result.Deleted)
	assert.Equal(t, []string{"GOES"}, store.deletes,
		"the collector deletes the key of a resource it no longer sees")
	assert.Equal(t, []string{"STAYS"}, store.liveResourceKeys())
}

// TestStorageReportPublisher_GrowthComesFromSuccessivePublications is task 3.1
// end to end: the rate is Δbytes over Δt across published observations, so the
// FIRST publication of a resource can only report unknown.
func TestStorageReportPublisher_GrowthComesFromSuccessivePublications(t *testing.T) {
	store := newFakeReportStore(10)
	publisher := newTestPublisher(t, store, defaultSource())
	first := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)

	_, err := publisher.Publish(context.Background(),
		inventoryAt(first, boundedResource("EVENTS", mib(1000), mib(100))))
	require.NoError(t, err)

	initial := store.liveRow(t, "EVENTS")
	assert.Equal(t, GrowthUnknown, initial.Growth.State,
		"one observation extrapolates nothing")
	assert.Equal(t, GrowthUnavailableNoPriorObservation, initial.Growth.Unavailable)
	assert.Nil(t, initial.Projection.TimeToThreshold,
		"an unknown rate projects no exhaustion")
	assert.NotNil(t, initial.Projection.HeadroomBytes,
		"headroom is knowable from one observation and is still reported")

	_, err = publisher.Publish(context.Background(),
		inventoryAt(first.Add(time.Minute), boundedResource("EVENTS", mib(1000), mib(160))))
	require.NoError(t, err)

	grown := store.liveRow(t, "EVENTS")
	require.Equal(t, GrowthKnown, grown.Growth.State)
	rate, ok := grown.Growth.Rate()
	require.True(t, ok)
	assert.InDelta(t, float64(mib(60))/60, rate, 0.001)
	assert.Equal(t, time.Minute, grown.Growth.ObservedOver)
	assert.NotNil(t, grown.Projection.TimeToThreshold)
}

// TestStorageReportPublisher_ChurningStableResourceReportsZeroGrowth is the
// spec scenario, at the publication layer: contents continuously replaced,
// total size unchanged between successive observations, rate exactly zero and
// no exhaustion projected.
//
// It passes for the right reason structurally as well as numerically: the
// publisher's only inputs are the two observations, so there is no span of
// retained timestamps available to divide by even if someone wanted to.
func TestStorageReportPublisher_ChurningStableResourceReportsZeroGrowth(t *testing.T) {
	store := newFakeReportStore(10)
	publisher := newTestPublisher(t, store, defaultSource())
	first := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	const stable = int64(512 << 20)

	for i := 0; i < 3; i++ {
		_, err := publisher.Publish(context.Background(), inventoryAt(
			first.Add(time.Duration(i)*time.Minute),
			boundedResource("CHURNING", mib(1000), stable),
		))
		require.NoError(t, err)
	}

	row := store.liveRow(t, "CHURNING")
	require.Equal(t, GrowthKnown, row.Growth.State)
	rate, ok := row.Growth.Rate()
	require.True(t, ok)
	assert.Zero(t, rate, "equal successive observations are zero growth")
	assert.Nil(t, row.Projection.TimeToThreshold, "a resource that is not growing projects no exhaustion")
	assert.Equal(t, ProjectionUnavailableNotGrowing, row.Projection.TimeToThresholdUnavailable)
	assert.Equal(t, PressureNormal, row.Pressure.State)
}

// TestStorageReportPublisher_ProjectionSurvivesRestart is the spec's
// restart-survival scenario and the reason the report bucket sequences ahead of
// the growth work: the observations live in the bucket, so a FRESH publisher —
// a restarted, redeployed, or crash-looped process, with nothing in memory —
// has a rate on its first publication rather than after a full window.
func TestStorageReportPublisher_ProjectionSurvivesRestart(t *testing.T) {
	store := newFakeReportStore(10)
	first := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)

	before := newTestPublisher(t, store, defaultSource())
	_, err := before.Publish(context.Background(),
		inventoryAt(first, boundedResource("EVENTS", mib(1000), mib(100))))
	require.NoError(t, err)

	// The process dies here. A new one comes up holding no samples at all.
	after := newTestPublisher(t, store, defaultSource())
	_, err = after.Publish(context.Background(),
		inventoryAt(first.Add(time.Minute), boundedResource("EVENTS", mib(1000), mib(160))))
	require.NoError(t, err)

	row := store.liveRow(t, "EVENTS")
	require.Equal(t, GrowthKnown, row.Growth.State,
		"the restarted process reads its baseline from the published report")
	rate, ok := row.Growth.Rate()
	require.True(t, ok)
	assert.InDelta(t, float64(mib(60))/60, rate, 0.001)
	require.NotNil(t, row.Growth.ObservedFrom)
	assert.Equal(t, first, row.Growth.ObservedFrom.UTC(),
		"the baseline is the observation the previous process published")
	require.NotNil(t, row.Projection.TimeToThreshold)
}

// TestStorageReportPublisher_BaselineIsNotAdvancedByAnUnusableObservation
// pins the fixed-target rule. When two publications land too close together to
// measure, the baseline must STAY where it was: advancing it to "now" every
// time would keep resetting the target and a rapidly publishing process would
// report an unknown rate forever, which is the worst possible time for the
// projection to go blank.
func TestStorageReportPublisher_BaselineIsNotAdvancedByAnUnusableObservation(t *testing.T) {
	store := newFakeReportStore(10)
	publisher := newTestPublisher(t, store, defaultSource())
	first := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)

	_, err := publisher.Publish(context.Background(),
		inventoryAt(first, boundedResource("EVENTS", mib(1000), mib(100))))
	require.NoError(t, err)

	// Three publications inside MinGrowthSampleInterval of the baseline.
	for i := 1; i <= 3; i++ {
		_, err = publisher.Publish(context.Background(), inventoryAt(
			first.Add(time.Duration(i)*time.Second),
			boundedResource("EVENTS", mib(1000), mib(100)+int64(i)),
		))
		require.NoError(t, err)
		row := store.liveRow(t, "EVENTS")
		require.Equal(t, GrowthUnknown, row.Growth.State)
		require.Equal(t, GrowthUnavailableObservationsTooClose, row.Growth.Unavailable)
	}

	// One past the interval: measured against the ORIGINAL baseline, not the
	// most recent unusable observation.
	_, err = publisher.Publish(context.Background(),
		inventoryAt(first.Add(time.Minute), boundedResource("EVENTS", mib(1000), mib(160))))
	require.NoError(t, err)

	row := store.liveRow(t, "EVENTS")
	require.Equal(t, GrowthKnown, row.Growth.State)
	assert.Equal(t, time.Minute, row.Growth.ObservedOver,
		"the interval runs from the retained baseline, not from the last unusable sample")
	require.NotNil(t, row.Growth.ObservedFrom)
	assert.Equal(t, first, row.Growth.ObservedFrom.UTC())
}

// TestStorageReportPublisher_ThresholdsResolveAtEvaluationTime is task 3.4.
//
// SemStreams is flow-based and its components are runtime-reconfigurable —
// watchConfigUpdates is launched after the boot barrier precisely so post-boot
// component edits reach running components. A threshold captured at
// construction would apply the stale number successfully and silently, which is
// the failure class that is hardest to notice: nothing errors, the report just
// answers the wrong question. The SAME publisher instance is used across the
// edit here, with no reconstruction and no restart.
func TestStorageReportPublisher_ThresholdsResolveAtEvaluationTime(t *testing.T) {
	store := newFakeReportStore(10)

	var mu sync.Mutex
	live := DefaultStoragePressureThresholds()
	source := func() StoragePressureThresholds {
		mu.Lock()
		defer mu.Unlock()
		return live
	}

	publisher := newTestPublisher(t, store, source)
	first := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)

	// 30% free: outside the default 25% warning band.
	_, err := publisher.Publish(context.Background(),
		inventoryAt(first, boundedResource("EVENTS", 1000, 700)))
	require.NoError(t, err)
	assert.Equal(t, PressureNormal, store.liveRow(t, "EVENTS").Pressure.State)

	// The operator edits the configuration of a RUNNING process.
	mu.Lock()
	live.WarningHeadroom = 0.40
	mu.Unlock()

	_, err = publisher.Publish(context.Background(),
		inventoryAt(first.Add(time.Minute), boundedResource("EVENTS", 1000, 700)))
	require.NoError(t, err)

	row := store.liveRow(t, "EVENTS")
	assert.Equal(t, PressureWarning, row.Pressure.State,
		"the next evaluation must use the edited threshold, with no restart")
	assert.Equal(t, PressureInputHeadroom, row.Pressure.RaisedBy)
}

// TestStorageReportPublisher_IncoherentThresholdsSuppressPressureNotTheReport:
// an unusable threshold edit must not silently fall back to defaults (that
// would apply a number the operator did not choose, successfully and silently)
// and must not blank the whole report either — capacity, usage, and growth are
// still true and still worth publishing.
func TestStorageReportPublisher_IncoherentThresholdsSuppressPressureNotTheReport(t *testing.T) {
	store := newFakeReportStore(10)
	broken := DefaultStoragePressureThresholds()
	broken.WarningHorizon = "not-a-duration"

	publisher := newTestPublisher(t, store, func() StoragePressureThresholds { return broken })
	first := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)

	result, err := publisher.Publish(context.Background(),
		inventoryAt(first, boundedResource("EVENTS", 1000, 990)))
	require.NoError(t, err, "a bad threshold edit must not stop the report")
	assert.Equal(t, 1, result.Published)

	row := store.liveRow(t, "EVENTS")
	assert.False(t, row.Pressure.Evaluated,
		"no state at all rather than a state derived from thresholds nobody chose")
	assert.Contains(t, row.Pressure.Unavailable, "threshold")
	require.NotNil(t, row.Resource.Bytes.ConfiguredLimit)
	assert.Equal(t, int64(1000), *row.Resource.Bytes.ConfiguredLimit,
		"the capacity truth is published regardless")
	assert.Nil(t, row.Projection.HeadroomBytes,
		"a projection needs a threshold to target, so it is suppressed with the pressure state")
}

// TestStorageReportPublisher_CriticalPressureRejectsNothing is the spec's
// report-only scenario at this layer: a resource evaluated at critical is
// published like any other and nothing about the publication fails, degrades,
// or refuses. The publisher holds no reference to anything that could reject a
// write — it returns values.
func TestStorageReportPublisher_CriticalPressureRejectsNothing(t *testing.T) {
	store := newFakeReportStore(10)
	publisher := newTestPublisher(t, store, defaultSource())
	first := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)

	// 1% free, and filling fast.
	_, err := publisher.Publish(context.Background(),
		inventoryAt(first, boundedResource("FULL", 1000, 950)))
	require.NoError(t, err)
	result, err := publisher.Publish(context.Background(),
		inventoryAt(first.Add(time.Minute), boundedResource("FULL", 1000, 990)))
	require.NoError(t, err, "critical pressure fails no publication")
	assert.Equal(t, 1, result.Published)

	row := store.liveRow(t, "FULL")
	require.True(t, row.Pressure.Evaluated)
	require.Equal(t, PressureCritical, row.Pressure.State)

	// And the next collection for the same resource still publishes.
	_, err = publisher.Publish(context.Background(),
		inventoryAt(first.Add(2*time.Minute), boundedResource("FULL", 1000, 995)))
	require.NoError(t, err)
	assert.Equal(t, PressureCritical, store.liveRow(t, "FULL").Pressure.State)
}

// TestStorageReportPublisher_UnknownCapacityIsPublishedWithoutPressure: the
// resources the server declines to describe are exactly the ones an inventory
// must not drop, so they get a key like everything else — named, with unknown
// capacity, no projection, and NO pressure state.
func TestStorageReportPublisher_UnknownCapacityIsPublishedWithoutPressure(t *testing.T) {
	store := newFakeReportStore(10)
	publisher := newTestPublisher(t, store, defaultSource())
	first := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)

	undescribable := StorageResource{
		Name:        "OFFLINE_STREAM",
		Kind:        ResourceOrdinaryStream,
		Attribution: AttributionNotApplicable,
		Tier:        TierUnknown,
		Bytes:       UnknownCapacity(),
		Messages:    UnknownCapacity(),
	}
	_, err := publisher.Publish(context.Background(), inventoryAt(first, undescribable))
	require.NoError(t, err)

	row := store.liveRow(t, "OFFLINE_STREAM")
	assert.Equal(t, CapacityUnknown, row.Resource.Bytes.State)
	assert.False(t, row.Pressure.Evaluated)
	assert.Equal(t, PressureUnavailableUnknownCapacity, row.Pressure.Unavailable)
	assert.Nil(t, row.Projection.HeadroomBytes)
	assert.Nil(t, row.Projection.TimeToThreshold)
	assert.Equal(t, GrowthUnknown, row.Growth.State)
	assert.Equal(t, GrowthUnavailableUnknownUsage, row.Growth.Unavailable,
		"there is no usage to difference, which is a different fact from having no prior sample")
}

// TestStorageReportPublisher_StaleInventoryIsNotAnObservation: a failed
// collection serves last-good, and republishing that as a fresh observation
// would inject a duplicate sample into the growth series and move every row's
// collection timestamp forward onto data that is not new.
func TestStorageReportPublisher_StaleInventoryIsNotAnObservation(t *testing.T) {
	store := newFakeReportStore(10)
	publisher := newTestPublisher(t, store, defaultSource())
	first := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)

	stale := inventoryAt(first, boundedResource("EVENTS", mib(1000), mib(100)))
	stale.Stale = true
	stale.StaleReason = "the server went away"

	result, err := publisher.Publish(context.Background(), stale)
	require.NoError(t, err)
	assert.True(t, result.Skipped)
	assert.Equal(t, 0, result.Published)
	assert.Empty(t, store.liveKeys(), "a stale inventory writes nothing")

	// A never-collected inventory is the same case: empty is not "the account
	// holds nothing".
	result, err = publisher.Publish(context.Background(), StorageInventory{ProducedBy: "unit-test", Stale: true})
	require.NoError(t, err)
	assert.True(t, result.Skipped)
	assert.Empty(t, store.deletes, "and it reclaims nothing either")
}

// TestStorageReportKey_AddressesEveryLegalResourceName: JetStream accepts
// stream names that are NOT legal KV keys (`$` and `+` among others), and the
// resources most likely to be a growth problem are the ones nobody in this
// process created. Dropping them from the report would rebuild the silent
// omission the inventory exists to end, so an illegal name is addressed through
// the repository's opaque key codec instead.
func TestStorageReportKey_AddressesEveryLegalResourceName(t *testing.T) {
	plain, err := StorageReportKey("KV_ENTITY_STATES")
	require.NoError(t, err)
	assert.Equal(t, "KV_ENTITY_STATES", plain,
		"an ordinary name is its own key, so `nats kv get` reads naturally")
	require.NoError(t, ValidateKVLiteralKey(plain))

	odd, err := StorageReportKey("weird$stream+name")
	require.NoError(t, err)
	require.NoError(t, ValidateKVLiteralKey(odd), "the encoded form must be a legal key")
	decoded, err := DecodeKVOpaqueToken(odd)
	require.NoError(t, err)
	assert.Equal(t, "weird$stream+name", string(decoded))

	// A name that IS itself a canonical opaque token must not collide with the
	// encoding of the name it decodes to.
	collision, err := StorageReportKey(odd)
	require.NoError(t, err)
	assert.NotEqual(t, odd, collision)
}

// TestStorageReportPublisher_IllegalNameIsPublishedNotDropped drives that
// through a publication: the row is addressable and carries the REAL name.
func TestStorageReportPublisher_IllegalNameIsPublishedNotDropped(t *testing.T) {
	store := newFakeReportStore(10)
	publisher := newTestPublisher(t, store, defaultSource())
	first := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)

	result, err := publisher.Publish(context.Background(),
		inventoryAt(first, boundedResource("legacy$stream", mib(1000), mib(100))))
	require.NoError(t, err)
	assert.Equal(t, 1, result.Published)

	keys := store.liveResourceKeys()
	require.Len(t, keys, 1)
	row := store.liveRow(t, keys[0])
	assert.Equal(t, "legacy$stream", row.Resource.Name,
		"the key is an address; the row still names the resource")
}

// TestStorageReportPublisher_PublicationFailureIsReportedNotSwallowed: one
// resource failing to publish must not silently shrink the report, and must not
// stop the rest of it either.
func TestStorageReportPublisher_PublicationFailureIsReportedNotSwallowed(t *testing.T) {
	store := newFakeReportStore(10)
	store.putErr = errors.New("nats: connection closed")
	publisher := newTestPublisher(t, store, defaultSource())

	result, err := publisher.Publish(context.Background(), inventoryAt(
		time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC),
		boundedResource("EVENTS", mib(1000), mib(100)),
	))
	require.Error(t, err)
	assert.Equal(t, 0, result.Published)
	assert.Contains(t, err.Error(), "EVENTS", "the failure names the resource it lost")
}

// TestNewStorageReportPublisher_FailsClosedOnWiring: a publisher with no
// threshold source would evaluate every resource against numbers no operator
// chose. Failing at construction beats defaulting silently, and the error names
// the one-line way to take the defaults deliberately.
func TestNewStorageReportPublisher_FailsClosedOnWiring(t *testing.T) {
	_, err := NewStorageReportPublisher(nil, StorageReportConfig{Thresholds: defaultSource()})
	require.Error(t, err)

	_, err = NewStorageReportPublisher(newFakeReportStore(10), StorageReportConfig{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "DefaultStoragePressureThresholds")
}

// TestStorageReportPublisher_HistoryDepthSurvivesACrashLoop is what the report
// bucket's declared History depth is FOR, and it is not what it first looks
// like. A single restart works at ANY depth, because a publisher seeds from the
// history BEFORE it writes: the row it is about to compact is the one it just
// read.
//
// The depth becomes load-bearing exactly where the design says the projection
// matters most — a crash loop or a deploy loop. A process restarting faster
// than MinGrowthSampleInterval publishes a row that is too close to the newest
// retained one to measure against, and at depth 1 that row replaces the only
// baseline that existed. Every restart repeats it, and the rate is unknown
// forever. Retained depth is what lets the walk reach back past the too-close
// rows to an observation that can still be measured against.
func TestStorageReportPublisher_HistoryDepthSurvivesACrashLoop(t *testing.T) {
	first := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)

	// Six lives of a process that dies every two seconds. Each one is a FRESH
	// publisher holding nothing, exactly like a restarted binary.
	crashLoop := func(depth int) ResourceReport {
		store := newFakeReportStore(depth)
		for i := 0; i <= 5; i++ {
			publisher := newTestPublisher(t, store, defaultSource())
			_, err := publisher.Publish(context.Background(), inventoryAt(
				first.Add(time.Duration(2*i)*time.Second),
				boundedResource("EVENTS", mib(1000), mib(100)+int64(i)),
			))
			require.NoError(t, err)
		}
		return store.liveRow(t, "EVENTS")
	}

	deep := crashLoop(10)
	assert.Equal(t, GrowthKnown, deep.Growth.State,
		"the declared depth keeps an older observation reachable through the loop")
	assert.GreaterOrEqual(t, deep.Growth.ObservedOver, MinGrowthSampleInterval)

	shallow := crashLoop(1)
	assert.Equal(t, GrowthUnknown, shallow.Growth.State,
		"at depth 1 every restart overwrites the only baseline with a too-close row")
	assert.Equal(t, GrowthUnavailableObservationsTooClose, shallow.Growth.Unavailable)
}

// recordingPublisher counts publications and remembers what it was handed.
type recordingPublisher struct {
	mu    sync.Mutex
	calls int
	last  StorageInventory
	err   error
}

func (p *recordingPublisher) Publish(_ context.Context, inv StorageInventory) (PublishResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	p.last = inv
	if p.err != nil {
		return PublishResult{}, p.err
	}
	return PublishResult{Published: len(inv.Resources)}, nil
}

func (p *recordingPublisher) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

// TestStorageInventory_RunPublishesEveryCollection is what makes "one key per
// resource EACH COLLECTION" a property of the code rather than an intention:
// the collector's own interval drives the publication, so the growth series
// advances at the cadence the operator configured and there is no second timer
// that could drift from it.
func TestStorageInventory_RunPublishesEveryCollection(t *testing.T) {
	publisher := &recordingPublisher{}
	lister := consistentAccount(streamInfo("LOGS", jetstream.FileStorage, 0, 1))
	collector, err := NewStorageInventoryCollector(lister.source(), StorageInventoryConfig{
		OwnerResolver: resolverFrom(nil),
		Interval:      20 * time.Millisecond,
		Timeout:       time.Second,
		Publisher:     publisher,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() { defer close(stopped); collector.Run(ctx) }()

	// 3s budget for two 20ms ticks: only a loop that never publishes fails.
	require.Eventually(t, func() bool { return publisher.count() >= 2 }, 3*time.Second, 5*time.Millisecond,
		"every collection must publish the report")

	cancel()
	select {
	case <-stopped:
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return when its context was cancelled")
	}

	publisher.mu.Lock()
	defer publisher.mu.Unlock()
	assert.False(t, publisher.last.Stale, "only a successful collection is published")
	assert.NotEmpty(t, publisher.last.Resources)
}

// TestStorageInventory_PublicationFailureDoesNotStopCollection: the report is
// observability. A publication that cannot land must not take the collection
// loop down with it — the next tick is the recovery, and the in-process
// last-good inventory stays readable throughout.
func TestStorageInventory_PublicationFailureDoesNotStopCollection(t *testing.T) {
	publisher := &recordingPublisher{err: errors.New("nats: no responders")}
	lister := consistentAccount(streamInfo("LOGS", jetstream.FileStorage, 0, 1))
	collector, err := NewStorageInventoryCollector(lister.source(), StorageInventoryConfig{
		OwnerResolver: resolverFrom(nil),
		Interval:      20 * time.Millisecond,
		Timeout:       time.Second,
		Publisher:     publisher,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() { defer close(stopped); collector.Run(ctx) }()

	require.Eventually(t, func() bool { return publisher.count() >= 2 }, 3*time.Second, 5*time.Millisecond,
		"a failed publication must not stop the loop")
	assert.NotEmpty(t, collector.Latest().Resources, "the in-process inventory stays readable")

	cancel()
	select {
	case <-stopped:
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return when its context was cancelled")
	}
}

// TestStorageInventory_FailedCollectionPublishesNothing: a failed collection
// serves last-good, and last-good is not a new observation. The collector does
// not hand it to the publisher at all — the publisher would decline it anyway,
// but a call per tick would log the same failure twice and make a failing
// server look like a failing report.
func TestStorageInventory_FailedCollectionPublishesNothing(t *testing.T) {
	publisher := &recordingPublisher{}
	broken := errors.New("name listing unavailable")
	// Counted here rather than through fakeLister.calls: the collector lists
	// NAMES first and never reaches the info listing when that fails, so the
	// info-call counter would stay at zero and the assertion below would pass
	// against a loop that never ran.
	var attempts atomic.Int64
	lister := &fakeLister{
		nextInfos: func() *fakeStreamInfoLister {
			return &fakeStreamInfoLister{infos: []*jetstream.StreamInfo{
				streamInfo("LOGS", jetstream.FileStorage, 0, 1),
			}}
		},
		nextNames: func() *fakeStreamNameLister {
			attempts.Add(1)
			return &fakeStreamNameLister{failWith: broken}
		},
	}
	collector, err := NewStorageInventoryCollector(lister.source(), StorageInventoryConfig{
		OwnerResolver: resolverFrom(nil),
		Interval:      20 * time.Millisecond,
		Timeout:       time.Second,
		Publisher:     publisher,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() { defer close(stopped); collector.Run(ctx) }()

	// Wait for real collection ATTEMPTS, so "nothing published" cannot pass by
	// the loop simply never having run.
	require.Eventually(t, func() bool { return attempts.Load() >= 2 }, 3*time.Second, 5*time.Millisecond,
		"the loop must keep attempting collections")
	assert.Zero(t, publisher.count(), "a failed collection is not an observation")

	cancel()
	select {
	case <-stopped:
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return when its context was cancelled")
	}
}

// TestStorageReportPublisher_ReclamationFailureIsReportedNotSwallowed: a
// reclamation that cannot run leaves rows in the bucket for resources that no
// longer exist, which is a report that lies about the account. It must surface
// as an error rather than as a quietly smaller Deleted count.
func TestStorageReportPublisher_ReclamationFailureIsReportedNotSwallowed(t *testing.T) {
	first := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)

	t.Run("the key listing fails", func(t *testing.T) {
		store := newFakeReportStore(10)
		publisher := newTestPublisher(t, store, defaultSource())
		store.listErr = errors.New("nats: connection closed")

		result, err := publisher.Publish(context.Background(),
			inventoryAt(first, boundedResource("EVENTS", mib(1000), mib(100))))
		require.Error(t, err)
		assert.Equal(t, 1, result.Published, "the rows that landed are still reported")
		assert.Equal(t, 0, result.Deleted)
	})

	t.Run("a delete fails", func(t *testing.T) {
		store := newFakeReportStore(10)
		publisher := newTestPublisher(t, store, defaultSource())
		_, err := publisher.Publish(context.Background(), inventoryAt(first,
			boundedResource("STAYS", mib(1000), mib(100)),
			boundedResource("GOES", mib(1000), mib(100)),
		))
		require.NoError(t, err)

		store.deleteErr = errors.New("nats: connection closed")
		result, err := publisher.Publish(context.Background(),
			inventoryAt(first.Add(time.Minute), boundedResource("STAYS", mib(1000), mib(110))))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "GOES", "the failure names the row that could not be reclaimed")
		assert.Equal(t, 0, result.Deleted)
	})
}

// TestStorageReportPublisher_AFailedRowWriteKeepsItsLastGoodRow closes the gap
// between what reclamation is FOR and what it could be made to do.
//
// Reclamation deletes the key of a resource the COLLECTION did not name — the
// collector deciding a resource is gone. A resource whose row merely failed to
// WRITE is a completely different fact: the collection named it, it still
// exists, and its previously published row is still the best truth available.
// Deleting it there would make a transient NATS error indistinguishable from a
// deleted stream, blank a real resource out of the report that section 4's HTTP
// route and alert rule read, and drop a tombstone into the middle of that
// resource's growth series.
func TestStorageReportPublisher_AFailedRowWriteKeepsItsLastGoodRow(t *testing.T) {
	store := newFakeReportStore(10)
	publisher := newTestPublisher(t, store, defaultSource())
	first := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)

	inventory := func(at time.Time, used int64) StorageInventory {
		return inventoryAt(at,
			boundedResource("ALPHA", mib(1000), used),
			boundedResource("BRAVO", mib(1000), used),
			boundedResource("CHARLIE", mib(1000), used),
		)
	}

	_, err := publisher.Publish(context.Background(), inventory(first, mib(100)))
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"ALPHA", "BRAVO", "CHARLIE"}, store.liveResourceKeys())
	goodRow := store.liveRow(t, "BRAVO")

	// One row's write fails; the collection still names all three resources and
	// all three still exist.
	store.putErrForKey = map[string]error{"BRAVO": errors.New("nats: timeout")}

	result, err := publisher.Publish(context.Background(), inventory(first.Add(time.Minute), mib(110)))
	require.Error(t, err, "the failed row is reported")
	assert.Contains(t, err.Error(), "BRAVO")
	assert.Equal(t, 2, result.Published)
	assert.Equal(t, 0, result.Deleted,
		"a write failure is not the collector deciding a resource is gone")

	assert.ElementsMatch(t, []string{"ALPHA", "BRAVO", "CHARLIE"}, store.liveResourceKeys(),
		"an operator ranging the bucket must still see every resource the account holds")
	assert.Empty(t, store.deletes)

	stale := store.liveRow(t, "BRAVO")
	assert.Equal(t, goodRow.CollectedAt.UTC(), stale.CollectedAt.UTC(),
		"the retained row is the last good one, and its own timestamp says how stale it is")

	// And the series is intact: once the write recovers, the rate measures
	// against the retained observation rather than restarting from nothing.
	store.putErrForKey = nil
	_, err = publisher.Publish(context.Background(), inventory(first.Add(2*time.Minute), mib(160)))
	require.NoError(t, err)
	recovered := store.liveRow(t, "BRAVO")
	assert.Equal(t, GrowthKnown, recovered.Growth.State,
		"no tombstone was written, so the growth series was never truncated")
}

// TestStorageReportPublisher_GrowthDoesNotDifferenceAcrossATombstone pins the
// delete-marker guard in the history walk.
//
// A resource that disappeared and came back is not the same series: the bytes
// on either side of the tombstone belong to different lifetimes of the
// resource, and differencing across the gap reports a rate that never happened
// — usually an enormous one, since the gap can be arbitrarily long. Reading
// unknown until two fresh observations exist is the honest answer, and it is
// the same rule that makes a brand-new resource report unknown.
func TestStorageReportPublisher_GrowthDoesNotDifferenceAcrossATombstone(t *testing.T) {
	store := newFakeReportStore(10)
	first := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	resource := func(used int64) StorageResource { return boundedResource("COMES_BACK", mib(1000), used) }

	// Present, then gone (reclaimed), then present again.
	publisher := newTestPublisher(t, store, defaultSource())
	_, err := publisher.Publish(context.Background(), inventoryAt(first, resource(mib(100))))
	require.NoError(t, err)
	_, err = publisher.Publish(context.Background(),
		inventoryAt(first.Add(time.Minute), boundedResource("SOMETHING_ELSE", mib(1000), mib(1))))
	require.NoError(t, err)
	require.NotContains(t, store.liveKeys(), "COMES_BACK", "test premise: the row was reclaimed")

	// A fresh process, so the baseline can only come from the retained history —
	// which now holds an observation followed by a delete marker.
	reborn := newTestPublisher(t, store, defaultSource())
	_, err = reborn.Publish(context.Background(), inventoryAt(first.Add(2*time.Minute), resource(mib(900))))
	require.NoError(t, err)

	row := store.liveRow(t, "COMES_BACK")
	assert.Equal(t, GrowthUnknown, row.Growth.State,
		"observations from before the resource disappeared are a different series")
	assert.Equal(t, GrowthUnavailableNoPriorObservation, row.Growth.Unavailable)
	assert.Nil(t, row.Projection.TimeToThreshold,
		"and no exhaustion is projected from a rate that never happened")
}

// TestStorageReportPublisher_RetainedBaselineIsNeverAFutureObservation covers
// clock skew between producers. Several processes may publish this report, and
// one with a fast clock leaves an observation stamped in the future.
//
// Such an observation can never be a baseline — differencing against it yields
// a negative interval — so it must not be retained as the fixed target either.
// Retaining it would wedge the resource on an unknown rate until wall-clock
// time caught up with the skew, which is precisely when nobody would think to
// look at the clocks.
func TestStorageReportPublisher_RetainedBaselineIsNeverAFutureObservation(t *testing.T) {
	store := newFakeReportStore(10)
	first := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)

	// A producer with a fast clock publishes ten minutes ahead.
	skewed := newTestPublisher(t, store, defaultSource())
	_, err := skewed.Publish(context.Background(),
		inventoryAt(first.Add(10*time.Minute), boundedResource("EVENTS", mib(1000), mib(500))))
	require.NoError(t, err)

	// This process, on a correct clock, seeds from that row and cannot use it.
	local := newTestPublisher(t, store, defaultSource())
	_, err = local.Publish(context.Background(),
		inventoryAt(first, boundedResource("EVENTS", mib(1000), mib(100))))
	require.NoError(t, err)
	require.Equal(t, GrowthUnknown, store.liveRow(t, "EVENTS").Growth.State,
		"test premise: an observation from the future is not a usable baseline")

	// The very next collection must measure normally, against THIS process's
	// own previous observation.
	_, err = local.Publish(context.Background(),
		inventoryAt(first.Add(time.Minute), boundedResource("EVENTS", mib(1000), mib(160))))
	require.NoError(t, err)

	row := store.liveRow(t, "EVENTS")
	require.Equal(t, GrowthKnown, row.Growth.State,
		"a skewed row must not wedge the series until wall-clock time catches up")
	assert.Equal(t, time.Minute, row.Growth.ObservedOver)
}

// TestOldestUsableTarget_NeverRetainsAFutureObservation pins the fixed-target
// selection directly, including the clock-skew invariant that the loop's single
// "only move earlier" condition is responsible for.
func TestOldestUsableTarget_NeverRetainsAFutureObservation(t *testing.T) {
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	ago := func(d time.Duration) time.Time { return now.Add(-d) }
	current := Observation{At: now, Bytes: 500}

	tests := []struct {
		name   string
		priors []Observation
		want   Observation
	}{
		{
			name:   "no priors keeps the current observation",
			priors: nil,
			want:   current,
		},
		{
			name:   "the oldest prior wins",
			priors: []Observation{{At: ago(time.Second), Bytes: 400}, {At: ago(time.Minute), Bytes: 100}},
			want:   Observation{At: ago(time.Minute), Bytes: 100},
		},
		{
			name:   "a future observation is never retained",
			priors: []Observation{{At: now.Add(time.Hour), Bytes: 900}},
			want:   current,
		},
		{
			name: "a future observation cannot outrank a real one",
			priors: []Observation{
				{At: now.Add(time.Hour), Bytes: 900},
				{At: ago(time.Minute), Bytes: 100},
			},
			want: Observation{At: ago(time.Minute), Bytes: 100},
		},
		{
			name:   "an identically stamped prior does not displace the fresher read",
			priors: []Observation{{At: now, Bytes: 111}},
			want:   current,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := oldestUsableTarget(current, tc.priors)
			assert.Equal(t, tc.want, got)
			assert.False(t, got.At.After(current.At),
				"a retained baseline is never in the future of the observation it will be measured against")
		})
	}
}
