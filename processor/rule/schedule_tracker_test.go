package rule

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sort"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

func newScheduleTrackerForTest() (*ScheduleTracker, *mockKVBucket) {
	bucket := newMockKVBucket()
	return NewScheduleTracker(bucket, slog.Default()), bucket
}

func TestScheduleTracker_RecordFire_AndRead(t *testing.T) {
	t.Parallel()

	tracker, _ := newScheduleTrackerForTest()
	ctx := context.Background()
	fired := time.Date(2026, 4, 30, 9, 0, 0, 0, time.UTC)

	if err := tracker.RecordFire(ctx, "weekly-planning", "0 9 * * MON", fired); err != nil {
		t.Fatalf("RecordFire = %v, want nil", err)
	}

	rec, err := tracker.LastFiredAt(ctx, "weekly-planning")
	if err != nil {
		t.Fatalf("LastFiredAt = %v, want nil", err)
	}
	if rec.RuleID != "weekly-planning" {
		t.Errorf("RuleID = %q, want weekly-planning", rec.RuleID)
	}
	if rec.ScheduleSpec != "0 9 * * MON" {
		t.Errorf("ScheduleSpec = %q, want 0 9 * * MON", rec.ScheduleSpec)
	}
	if !rec.LastFiredAt.Equal(fired) {
		t.Errorf("LastFiredAt = %v, want %v", rec.LastFiredAt, fired)
	}
}

func TestScheduleTracker_RecordFire_NormalizesToUTC(t *testing.T) {
	t.Parallel()

	tracker, _ := newScheduleTrackerForTest()
	ctx := context.Background()

	loc, _ := time.LoadLocation("America/New_York")
	local := time.Date(2026, 4, 30, 5, 0, 0, 0, loc) // 09:00 UTC

	if err := tracker.RecordFire(ctx, "weekly", "@hourly", local); err != nil {
		t.Fatalf("RecordFire = %v", err)
	}

	rec, err := tracker.LastFiredAt(ctx, "weekly")
	if err != nil {
		t.Fatalf("LastFiredAt = %v", err)
	}
	if rec.LastFiredAt.Location() != time.UTC {
		t.Errorf("LastFiredAt.Location = %v, want UTC", rec.LastFiredAt.Location())
	}
	if !rec.LastFiredAt.Equal(local) {
		t.Errorf("LastFiredAt = %v, want %v", rec.LastFiredAt, local)
	}
}

func TestScheduleTracker_LastFiredAt_NotFound(t *testing.T) {
	t.Parallel()

	tracker, _ := newScheduleTrackerForTest()
	_, err := tracker.LastFiredAt(context.Background(), "never-fired")
	if !errors.Is(err, ErrScheduleRecordNotFound) {
		t.Errorf("err = %v, want ErrScheduleRecordNotFound", err)
	}
}

func TestScheduleTracker_LastFiredAt_RejectsEmptyRuleID(t *testing.T) {
	t.Parallel()

	tracker, _ := newScheduleTrackerForTest()
	if _, err := tracker.LastFiredAt(context.Background(), ""); err == nil {
		t.Error("err = nil, want non-nil for empty ruleID")
	}
}

func TestScheduleTracker_RecordFire_RejectsEmptyRuleID(t *testing.T) {
	t.Parallel()

	tracker, _ := newScheduleTrackerForTest()
	if err := tracker.RecordFire(context.Background(), "", "@hourly", time.Now()); err == nil {
		t.Error("err = nil, want non-nil for empty ruleID")
	}
}

func TestScheduleTracker_NilBucket_AllMethodsError(t *testing.T) {
	t.Parallel()

	tracker := NewScheduleTracker(nil, slog.Default())
	ctx := context.Background()

	if _, err := tracker.LastFiredAt(ctx, "any"); err == nil {
		t.Error("LastFiredAt err = nil, want non-nil for nil bucket")
	}
	if err := tracker.RecordFire(ctx, "any", "@hourly", time.Now()); err == nil {
		t.Error("RecordFire err = nil, want non-nil for nil bucket")
	}
	if err := tracker.Delete(ctx, "any"); err == nil {
		t.Error("Delete err = nil, want non-nil for nil bucket")
	}
	if _, err := tracker.List(ctx); err == nil {
		t.Error("List err = nil, want non-nil for nil bucket")
	}
}

func TestScheduleTracker_Delete_Idempotent(t *testing.T) {
	t.Parallel()

	tracker, _ := newScheduleTrackerForTest()
	ctx := context.Background()

	// Delete on missing key must not error.
	if err := tracker.Delete(ctx, "never-existed"); err != nil {
		t.Errorf("Delete on missing key = %v, want nil", err)
	}

	// Delete after Record removes the record.
	if err := tracker.RecordFire(ctx, "rule", "@hourly", time.Now()); err != nil {
		t.Fatalf("RecordFire = %v", err)
	}
	if err := tracker.Delete(ctx, "rule"); err != nil {
		t.Errorf("Delete = %v, want nil", err)
	}
	if _, err := tracker.LastFiredAt(ctx, "rule"); !errors.Is(err, ErrScheduleRecordNotFound) {
		t.Errorf("LastFiredAt after Delete err = %v, want ErrScheduleRecordNotFound", err)
	}
}

func TestScheduleTracker_List_EmptyBucket(t *testing.T) {
	t.Parallel()

	tracker, _ := newScheduleTrackerForTest()
	records, err := tracker.List(context.Background())
	if err != nil {
		t.Fatalf("List = %v, want nil", err)
	}
	if len(records) != 0 {
		t.Errorf("len(records) = %d, want 0", len(records))
	}
}

func TestScheduleTracker_List_ReturnsAllRecords(t *testing.T) {
	t.Parallel()

	tracker, _ := newScheduleTrackerForTest()
	ctx := context.Background()

	t0 := time.Date(2026, 4, 30, 9, 0, 0, 0, time.UTC)
	if err := tracker.RecordFire(ctx, "alpha", "@hourly", t0); err != nil {
		t.Fatalf("RecordFire alpha = %v", err)
	}
	if err := tracker.RecordFire(ctx, "bravo", "@daily", t0.Add(time.Hour)); err != nil {
		t.Fatalf("RecordFire bravo = %v", err)
	}

	records, err := tracker.List(ctx)
	if err != nil {
		t.Fatalf("List = %v, want nil", err)
	}
	if len(records) != 2 {
		t.Fatalf("len(records) = %d, want 2", len(records))
	}

	sort.Slice(records, func(i, j int) bool {
		return records[i].RuleID < records[j].RuleID
	})
	if records[0].RuleID != "alpha" || records[1].RuleID != "bravo" {
		t.Errorf("records = [%q, %q], want [alpha, bravo]", records[0].RuleID, records[1].RuleID)
	}
}

func TestScheduleTracker_List_SkipsMalformedRecord(t *testing.T) {
	t.Parallel()

	tracker, bucket := newScheduleTrackerForTest()
	ctx := context.Background()

	// Write a malformed record directly. List must skip it instead of
	// failing the whole call (one bad record shouldn't blind the
	// scheduler to all sibling rules' missed-fire data).
	if _, err := bucket.Put(ctx, "broken", []byte("not-json")); err != nil {
		t.Fatalf("seed broken record = %v", err)
	}

	good := LastFireRecord{
		RuleID:       "good",
		ScheduleSpec: "@hourly",
		LastFiredAt:  time.Now().UTC().Truncate(time.Second),
	}
	data, _ := json.Marshal(good)
	if _, err := bucket.Put(ctx, "good", data); err != nil {
		t.Fatalf("seed good record = %v", err)
	}

	records, err := tracker.List(ctx)
	if err != nil {
		t.Fatalf("List = %v, want nil", err)
	}
	if len(records) != 1 || records[0].RuleID != "good" {
		t.Errorf("records = %v, want one entry with RuleID=good", records)
	}
}

func TestScheduleTracker_LastFiredAt_MalformedRecord(t *testing.T) {
	t.Parallel()

	tracker, bucket := newScheduleTrackerForTest()
	ctx := context.Background()

	if _, err := bucket.Put(ctx, "rule", []byte("not-json")); err != nil {
		t.Fatalf("seed broken record = %v", err)
	}

	if _, err := tracker.LastFiredAt(ctx, "rule"); err == nil {
		t.Error("LastFiredAt err = nil, want non-nil for malformed record")
	}
}

// Sanity: scheduleKey is the single chokepoint future per-entity fan-out
// extends. Locking the MVP shape ({ruleID} bare) prevents accidental
// drift before the deferred extension actually lands.
func TestScheduleKey_MVPShapeIsBareRuleID(t *testing.T) {
	t.Parallel()
	if scheduleKey("weekly-planning") != "weekly-planning" {
		t.Errorf("scheduleKey = %q, want %q (MVP key shape)",
			scheduleKey("weekly-planning"), "weekly-planning")
	}
}

// Compile-time guard against drift in the JSON wire shape — out-of-process
// readers (governance startup hooks) depend on these field names.
func TestLastFireRecord_JSONFieldNames(t *testing.T) {
	t.Parallel()

	rec := LastFireRecord{
		RuleID:       "weekly-planning",
		ScheduleSpec: "0 9 * * MON",
		LastFiredAt:  time.Date(2026, 4, 30, 9, 0, 0, 0, time.UTC),
	}
	data, err := json.Marshal(rec)
	if err != nil {
		t.Fatalf("Marshal = %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal = %v", err)
	}
	for _, field := range []string{"rule_id", "schedule_spec", "last_fired_at"} {
		if _, ok := raw[field]; !ok {
			t.Errorf("missing public field %q in JSON: %s", field, data)
		}
	}
}

// Tests that the LastFireRecord round-trips cleanly through the bucket —
// guards against a future change to the on-disk encoding that would
// silently corrupt records cross-deploy.
func TestScheduleTracker_RoundTripPreservesRecord(t *testing.T) {
	t.Parallel()

	tracker, _ := newScheduleTrackerForTest()
	ctx := context.Background()

	original := LastFireRecord{
		RuleID:       "rule-1",
		ScheduleSpec: "@every 5m",
		// Truncate to second resolution to avoid round-trip mismatch
		// from JSON's RFC3339Nano default (sub-microsecond drift on
		// some platforms).
		LastFiredAt: time.Now().UTC().Truncate(time.Second),
	}

	if err := tracker.RecordFire(ctx, original.RuleID, original.ScheduleSpec, original.LastFiredAt); err != nil {
		t.Fatalf("RecordFire = %v", err)
	}

	got, err := tracker.LastFiredAt(ctx, original.RuleID)
	if err != nil {
		t.Fatalf("LastFiredAt = %v", err)
	}
	if got != original {
		t.Errorf("round-trip mismatch:\n  got  %+v\n  want %+v", got, original)
	}
}

// Bucket() exposes the underlying KV handle for in-process governance
// reads. Returns the live bucket reference (not a copy) and nil when
// the tracker is in degraded mode.
func TestScheduleTracker_BucketAccessor(t *testing.T) {
	t.Parallel()

	tracker, bucket := newScheduleTrackerForTest()
	if got := tracker.Bucket(); got == nil {
		t.Fatal("Bucket() = nil for live tracker, want non-nil")
	}
	// The accessor returns the same handle the tracker writes through —
	// confirms in-process callers operate on the same KV state.
	if any(tracker.Bucket()) != any(bucket) {
		t.Error("Bucket() returned a different handle than the underlying mock")
	}

	nilTracker := NewScheduleTracker(nil, slog.Default())
	if got := nilTracker.Bucket(); got != nil {
		t.Errorf("Bucket() = %v on nil-bucket tracker, want nil", got)
	}
}

// Static guard: ErrScheduleRecordNotFound must compose with errors.Is
// (consumer code branches on it).
func TestScheduleTracker_ErrIsCompatible(t *testing.T) {
	t.Parallel()

	tracker, _ := newScheduleTrackerForTest()
	_, err := tracker.LastFiredAt(context.Background(), "absent")
	if err == nil {
		t.Fatal("err = nil, want ErrScheduleRecordNotFound")
	}
	if !errors.Is(err, ErrScheduleRecordNotFound) {
		t.Errorf("errors.Is = false, want true (sentinel must be wrapped or returned bare)")
	}
}

// Defence-in-depth: an unexpected error from the underlying bucket must
// not be silently coerced to ErrScheduleRecordNotFound (which would mask
// outages as "rule never fired"). This test wires a flaky mock that
// returns a custom error, then asserts the tracker surfaces it.
func TestScheduleTracker_LastFiredAt_PassesThroughUnexpectedErr(t *testing.T) {
	t.Parallel()

	bucket := &flakyKVBucket{
		mockKVBucket: newMockKVBucket(),
		err:          errors.New("nats: connection lost"),
	}
	tracker := NewScheduleTracker(bucket, slog.Default())

	_, err := tracker.LastFiredAt(context.Background(), "rule")
	if err == nil {
		t.Fatal("err = nil, want non-nil")
	}
	if errors.Is(err, ErrScheduleRecordNotFound) {
		t.Error("err matched ErrScheduleRecordNotFound, want pass-through of underlying error")
	}
}

// flakyKVBucket overrides Get on a real mockKVBucket to return a custom
// error for every key. Embedding the real mock keeps the rest of the
// jetstream.KeyValue surface satisfied without re-stubbing each method.
type flakyKVBucket struct {
	*mockKVBucket
	err error
}

func (f *flakyKVBucket) Get(_ context.Context, _ string) (jetstream.KeyValueEntry, error) {
	return nil, f.err
}

// Regression: rule removal must purge the schedule record. The reviewer
// flagged this as a must-fix in Chunk 3 because a wired-up-but-not-called
// Delete API would let RULE_SCHEDULES grow unboundedly across hot-reload
// rule churn.
func TestProcessor_RemoveRule_DeletesScheduleRecord(t *testing.T) {
	t.Parallel()

	tracker, _ := newScheduleTrackerForTest()
	ctx := context.Background()
	const ruleID = "removed-rule"

	// Seed a record as if the rule had previously fired.
	if err := tracker.RecordFire(ctx, ruleID, "@hourly", time.Now()); err != nil {
		t.Fatalf("seed RecordFire = %v", err)
	}

	// Build a minimal Processor that includes the cron registry and
	// schedule tracker; removeRule expects these.
	rp := &Processor{
		logger:          slog.Default(),
		rules:           make(map[string]Rule),
		ruleDefinitions: make(map[string]Definition),
		ruleConfigs:     make(map[string]map[string]any),
		matchCounters:   make(map[string]*atomic.Int64),
		cronRules:       map[string]*CronRule{ruleID: {id: ruleID}},
		scheduleTracker: tracker,
	}

	rp.removeRule(ruleID)

	if _, err := tracker.LastFiredAt(ctx, ruleID); !errors.Is(err, ErrScheduleRecordNotFound) {
		t.Errorf("LastFiredAt after removeRule = %v, want ErrScheduleRecordNotFound", err)
	}
}

// Regression: a cron→expression type swap on hot-reload must purge the
// cron rule's schedule record. Mirror of the removeRule test for the
// applyExpressionRuleChange code path.
func TestProcessor_TypeSwap_DeletesScheduleRecord(t *testing.T) {
	t.Parallel()

	tracker, _ := newScheduleTrackerForTest()
	ctx := context.Background()
	const ruleID = "swapping-rule"

	if err := tracker.RecordFire(ctx, ruleID, "@hourly", time.Now()); err != nil {
		t.Fatalf("seed RecordFire = %v", err)
	}

	// Direct exercise of the type-swap helper code path: simulate the
	// "had cron, now removing" branch by populating cronRules and then
	// calling deleteScheduleRecord. The applyExpressionRuleChange wraps
	// that branch with rule-creation work irrelevant to record deletion.
	rp := &Processor{
		logger:          slog.Default(),
		cronRules:       map[string]*CronRule{ruleID: {id: ruleID}},
		scheduleTracker: tracker,
	}

	rp.deleteScheduleRecord(ruleID)

	if _, err := tracker.LastFiredAt(ctx, ruleID); !errors.Is(err, ErrScheduleRecordNotFound) {
		t.Errorf("LastFiredAt after type-swap delete = %v, want ErrScheduleRecordNotFound", err)
	}
}

// deleteScheduleRecord with no tracker must be a clean no-op (degraded-
// mode startups must not crash hot-reload paths).
func TestProcessor_DeleteScheduleRecord_NilTrackerNoop(t *testing.T) {
	t.Parallel()

	rp := &Processor{
		logger: slog.Default(),
	}
	rp.deleteScheduleRecord("any-rule") // must not panic
}
