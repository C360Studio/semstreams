// Package rule - Tests for WindowTracker (sliding-window counter).
//
// These tests use a CAS-capable in-memory KV mock so that the real
// read-modify-write CAS loop in Increment is exercised fully. The
// integration test (TestIntegration_CountInWindow_RealKV) exercises the
// same loop against a real NATS testcontainer.
package rule

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWindowTracker_ConcurrentCAS_AllIncrementsAccountedFor fires 50 goroutines
// × 100 increments and verifies that every increment lands with no lost writes.
// Uses a sync.WaitGroup to exercise concurrent KV access under the race detector.
//
// Each goroutine uses a unique dimension key, so all 50 goroutines run without
// CAS conflicts. This verifies concurrent bucket access (no data races) and
// guarantees every increment lands without exhausting the retry budget. CAS
// conflict resolution is tested separately in TestWindowTracker_ConcurrentCAS_ConflictRetry.
//
// Run with -race to confirm there are no data races in the CAS path.
func TestWindowTracker_ConcurrentCAS_AllIncrementsAccountedFor(t *testing.T) {
	t.Parallel()

	wt, _ := newWindowTrackerForTest()
	ctx := context.Background()
	now := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)

	const goroutines = 50
	const incrementsPerGoroutine = 100
	const expected = goroutines * incrementsPerGoroutine

	var wg sync.WaitGroup
	wg.Add(goroutines)

	// Collect errors without panicking; t.Errorf is called after Wait.
	errCh := make(chan error, goroutines)

	for i := 0; i < goroutines; i++ {
		goroutineIdx := i
		go func() {
			defer wg.Done()
			// Each goroutine owns a unique dimension → no CAS conflicts.
			// This exercises concurrent bucket reads/writes under the race detector.
			dimension := fmt.Sprintf("goroutine-%d", goroutineIdx)
			for j := 0; j < incrementsPerGoroutine; j++ {
				if _, err := wt.Increment(ctx, "rule-concurrent", dimension, now, 5*time.Minute); err != nil {
					select {
					case errCh <- err:
					default:
					}
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("Increment error from goroutine: %v", err)
	}
	if t.Failed() {
		return
	}

	// Tally across all goroutine-owned keys.
	total := 0
	for i := 0; i < goroutines; i++ {
		key := windowKey("rule-concurrent", fmt.Sprintf("goroutine-%d", i))
		entry, err := wt.bucket.Get(ctx, key)
		require.NoError(t, err, "key %q not found", key)
		var rec WindowRecord
		require.NoError(t, json.Unmarshal(entry.Value(), &rec), "unmarshal %q", key)
		for _, b := range rec.Buckets {
			total += b.Count
		}
	}
	assert.Equal(t, expected, total,
		"all %d concurrent increments must be accounted for; no writes may be lost", expected)
}

// TestWindowTracker_ConcurrentCAS_ConflictRetry verifies that when two goroutines
// race on an existing key, the CAS Update retry loop resolves the conflict and
// both increments land. This exercises the read-outside-lock → ErrKeyExists →
// retry path that protects concurrent Increment calls in production.
//
// Design: the key is pre-seeded with one count so both goroutines enter the
// Update path (revision > 0). This is the CAS path with real conflict semantics
// in the casKVBucket mock (Update returns ErrKeyExists on revision mismatch).
// The Put path (revision == 0) is last-writer-wins in the real NATS KV, which
// is an acceptable first-write race in production given the dimMu cardinality
// guard already serialises new-key creation.
func TestWindowTracker_ConcurrentCAS_ConflictRetry(t *testing.T) {
	t.Parallel()

	wt, _ := newWindowTrackerForTest()
	ctx := context.Background()
	now := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)

	// Pre-seed the key so both racing goroutines enter the Update (CAS) path.
	_, err := wt.Increment(ctx, "rule-conflict", "alice", now, 5*time.Minute)
	require.NoError(t, err, "pre-seed increment failed")

	// Use a barrier to maximise the chance that both goroutines read the same
	// revision before either writes, exercising the conflict-then-retry path.
	barrier := make(chan struct{})

	type result struct{ err error }
	results := make(chan result, 2)

	// Goroutine 1: waits at barrier then increments once (enters Update path).
	go func() {
		<-barrier
		_, err := wt.Increment(ctx, "rule-conflict", "alice", now, 5*time.Minute)
		results <- result{err: err}
	}()

	// Goroutine 2: waits at barrier then increments once (enters Update path).
	go func() {
		<-barrier
		_, err := wt.Increment(ctx, "rule-conflict", "alice", now, 5*time.Minute)
		results <- result{err: err}
	}()

	// Release both goroutines simultaneously.
	close(barrier)

	r1, r2 := <-results, <-results
	if r1.err != nil {
		t.Errorf("goroutine 1 Increment error: %v", r1.err)
	}
	if r2.err != nil {
		t.Errorf("goroutine 2 Increment error: %v", r2.err)
	}
	if t.Failed() {
		return
	}

	entry, err := wt.bucket.Get(ctx, windowKey("rule-conflict", "alice"))
	require.NoError(t, err)
	var rec WindowRecord
	require.NoError(t, json.Unmarshal(entry.Value(), &rec))

	total := 0
	for _, b := range rec.Buckets {
		total += b.Count
	}
	// 1 pre-seed + 2 concurrent = 3 total
	assert.Equal(t, 3, total,
		"pre-seed + both goroutines must land all increments; CAS Update retry must not lose writes")
}

// ----- CAS-capable KV mock -----

// casKVBucket is an in-memory KV bucket that implements optimistic concurrency
// (Update checks the expected revision and returns ErrKeyExists on
// mismatch). This is required for WindowTracker's CAS loop to work in tests.
type casKVBucket struct {
	mu       sync.Mutex
	data     map[string][]byte
	revision map[string]uint64
	nextRev  uint64
}

func newCASKVBucket() *casKVBucket {
	return &casKVBucket{
		data:     make(map[string][]byte),
		revision: make(map[string]uint64),
		nextRev:  1,
	}
}

func (c *casKVBucket) Get(_ context.Context, key string) (jetstream.KeyValueEntry, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	val, ok := c.data[key]
	if !ok {
		return nil, jetstream.ErrKeyNotFound
	}
	valCopy := make([]byte, len(val))
	copy(valCopy, val)
	return &mockKVEntry{key: key, value: valCopy, revision: c.revision[key]}, nil
}

func (c *casKVBucket) Put(_ context.Context, key string, value []byte) (uint64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	valCopy := make([]byte, len(value))
	copy(valCopy, value)
	c.data[key] = valCopy
	rev := c.nextRev
	c.revision[key] = rev
	c.nextRev++
	return rev, nil
}

// Update performs an optimistic write: if current revision != lastRevision,
// it returns jetstream.ErrKeyExists (JSErrCodeStreamWrongLastSequence) so the
// caller knows to retry. This mirrors the real NATS KV Update behaviour on a
// revision mismatch — the tracker treats any Update error as a CAS conflict.
func (c *casKVBucket) Update(_ context.Context, key string, value []byte, lastRevision uint64) (uint64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	cur, ok := c.revision[key]
	if !ok || cur != lastRevision {
		return 0, jetstream.ErrKeyExists
	}
	valCopy := make([]byte, len(value))
	copy(valCopy, value)
	c.data[key] = valCopy
	rev := c.nextRev
	c.revision[key] = rev
	c.nextRev++
	return rev, nil
}

func (c *casKVBucket) Delete(_ context.Context, key string, _ ...jetstream.KVDeleteOpt) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.data[key]; !ok {
		return jetstream.ErrKeyNotFound
	}
	delete(c.data, key)
	delete(c.revision, key)
	return nil
}

func (c *casKVBucket) Keys(_ context.Context, _ ...jetstream.WatchOpt) ([]string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.data) == 0 {
		return nil, jetstream.ErrNoKeysFound
	}
	keys := make([]string, 0, len(c.data))
	for k := range c.data {
		keys = append(keys, k)
	}
	return keys, nil
}

// Stub out the remaining jetstream.KeyValue interface methods.
func (c *casKVBucket) GetRevision(_ context.Context, _ string, _ uint64) (jetstream.KeyValueEntry, error) {
	return nil, errors.New("not implemented")
}
func (c *casKVBucket) PutString(ctx context.Context, key string, value string) (uint64, error) {
	return c.Put(ctx, key, []byte(value))
}
func (c *casKVBucket) Create(_ context.Context, _ string, _ []byte, _ ...jetstream.KVCreateOpt) (uint64, error) {
	return 0, errors.New("not implemented")
}
func (c *casKVBucket) Purge(_ context.Context, _ string, _ ...jetstream.KVDeleteOpt) error {
	return errors.New("not implemented")
}
func (c *casKVBucket) Watch(_ context.Context, _ string, _ ...jetstream.WatchOpt) (jetstream.KeyWatcher, error) {
	return nil, errors.New("not implemented")
}
func (c *casKVBucket) WatchAll(_ context.Context, _ ...jetstream.WatchOpt) (jetstream.KeyWatcher, error) {
	return nil, errors.New("not implemented")
}
func (c *casKVBucket) WatchFiltered(_ context.Context, _ []string, _ ...jetstream.WatchOpt) (jetstream.KeyWatcher, error) {
	return nil, errors.New("not implemented")
}
func (c *casKVBucket) History(_ context.Context, _ string, _ ...jetstream.WatchOpt) ([]jetstream.KeyValueEntry, error) {
	return nil, errors.New("not implemented")
}
func (c *casKVBucket) Bucket() string { return "cas-mock" }
func (c *casKVBucket) PurgeDeletes(_ context.Context, _ ...jetstream.KVPurgeOpt) error {
	return errors.New("not implemented")
}
func (c *casKVBucket) Status(_ context.Context) (jetstream.KeyValueStatus, error) {
	return nil, errors.New("not implemented")
}
func (c *casKVBucket) ListKeys(_ context.Context, _ ...jetstream.WatchOpt) (jetstream.KeyLister, error) {
	return nil, errors.New("not implemented")
}
func (c *casKVBucket) ListKeysFiltered(_ context.Context, _ ...string) (jetstream.KeyLister, error) {
	return nil, errors.New("not implemented")
}

// ----- Helper -----

func newWindowTrackerForTest() (*WindowTracker, *casKVBucket) {
	bucket := newCASKVBucket()
	return NewWindowTracker(bucket, nil), bucket
}

// ----- Tests -----

// TestWindowTracker_BucketedCounter_Stores verifies that two increments in
// the same 1-minute bucket produce a single bucket entry with count=2.
func TestWindowTracker_BucketedCounter_Stores(t *testing.T) {
	t.Parallel()
	wt, _ := newWindowTrackerForTest()
	ctx := context.Background()

	now := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	count1, err := wt.Increment(ctx, "rule-a", "alice", now, 5*time.Minute)
	require.NoError(t, err)
	assert.Equal(t, 1, count1)

	// Same minute — should land in the same bucket
	count2, err := wt.Increment(ctx, "rule-a", "alice", now.Add(30*time.Second), 5*time.Minute)
	require.NoError(t, err)
	assert.Equal(t, 2, count2, "two increments in the same bucket window should give count=2")

	// Verify only one bucket entry exists
	entry, err := wt.bucket.Get(ctx, windowKey("rule-a", "alice"))
	require.NoError(t, err)
	var rec WindowRecord
	require.NoError(t, json.Unmarshal(entry.Value(), &rec))
	assert.Len(t, rec.Buckets, 1, "both increments should land in the same 1-minute bucket")
	assert.Equal(t, 2, rec.Buckets[0].Count)
}

// TestWindowTracker_BucketRotation_OnWindowAdvance verifies that increments
// in different 1-minute buckets produce two distinct bucket entries.
func TestWindowTracker_BucketRotation_OnWindowAdvance(t *testing.T) {
	t.Parallel()
	wt, _ := newWindowTrackerForTest()
	ctx := context.Background()

	t0 := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC) // minute 0
	t1 := t0.Add(90 * time.Second)                     // minute 1

	_, err := wt.Increment(ctx, "rule-b", "bob", t0, 5*time.Minute)
	require.NoError(t, err)

	count, err := wt.Increment(ctx, "rule-b", "bob", t1, 5*time.Minute)
	require.NoError(t, err)
	assert.Equal(t, 2, count, "two increments in different buckets, both within window → count=2")

	entry, err := wt.bucket.Get(ctx, windowKey("rule-b", "bob"))
	require.NoError(t, err)
	var rec WindowRecord
	require.NoError(t, json.Unmarshal(entry.Value(), &rec))
	assert.Len(t, rec.Buckets, 2, "increments in different minutes should produce two buckets")
}

// TestWindowTracker_RestartRecovery verifies that counts survive a tracker restart
// (re-read from KV — the bucket holds the persistent state).
func TestWindowTracker_RestartRecovery(t *testing.T) {
	t.Parallel()
	_, bucket := newWindowTrackerForTest()
	ctx := context.Background()
	now := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)

	// First "process lifetime" — write some state
	wt1 := NewWindowTracker(bucket, nil)
	count, err := wt1.Increment(ctx, "rule-c", "carol", now, 10*time.Minute)
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	// Simulate restart — new tracker instance, same bucket
	wt2 := NewWindowTracker(bucket, nil)
	count2, err := wt2.Increment(ctx, "rule-c", "carol", now.Add(30*time.Second), 10*time.Minute)
	require.NoError(t, err)
	assert.Equal(t, 2, count2, "count should persist across tracker restarts via KV")
}

// TestWindowTracker_DimensionCapHit_RejectsNewKeys verifies that when
// DefaultMaxDistinctDimensionsPerRule is reached, new dimensions return -1.
func TestWindowTracker_DimensionCapHit_RejectsNewKeys(t *testing.T) {
	t.Parallel()
	wt, _ := newWindowTrackerForTest()
	ctx := context.Background()
	now := time.Now()
	ruleID := "rule-cap"

	// Fill to DefaultMaxDistinctDimensionsPerRule. Use a small constant to
	// keep the test fast — we directly manipulate the cap via the internal
	// approach: populate the bucket with synthetic entries at the full cap.
	// The test must work regardless of the actual cap value.
	//
	// Strategy: write (DefaultMaxDistinctDimensionsPerRule) entries directly
	// into the bucket, then try to add one more via Increment.
	for i := 0; i < DefaultMaxDistinctDimensionsPerRule; i++ {
		key := windowKey(ruleID, fmt.Sprintf("dim-%d", i))
		data, _ := json.Marshal(WindowRecord{Buckets: []WindowBucket{{WindowStart: now.Unix(), Count: 1}}})
		_, err := wt.bucket.Put(ctx, key, data)
		require.NoError(t, err)
	}

	// Now try to add a brand-new dimension — should hit the cap
	count, err := wt.Increment(ctx, ruleID, "new-dimension", now, 5*time.Minute)
	require.NoError(t, err, "cardinality cap hit should return -1, not an error")
	assert.Equal(t, -1, count, "cardinality cap hit should return -1")
}

// TestWindowTracker_TimestampCapHit_TruncatesOldestBuckets verifies that
// when the per-window count exceeds DefaultMaxCountsPerWindow, the oldest
// buckets are dropped.
func TestWindowTracker_TimestampCapHit_TruncatesOldestBuckets(t *testing.T) {
	t.Parallel()
	wt, bucket := newWindowTrackerForTest()
	ctx := context.Background()
	ruleID := "rule-truncate"
	dimension := "grace"
	now := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)

	// Pre-populate a record whose total count equals DefaultMaxCountsPerWindow,
	// spread across 3 buckets so that the oldest can be dropped.
	thirds := DefaultMaxCountsPerWindow / 3
	rec := WindowRecord{
		Buckets: []WindowBucket{
			{WindowStart: now.Add(-4 * time.Minute).Truncate(time.Minute).Unix(), Count: thirds},
			{WindowStart: now.Add(-3 * time.Minute).Truncate(time.Minute).Unix(), Count: thirds},
			{WindowStart: now.Add(-2 * time.Minute).Truncate(time.Minute).Unix(), Count: DefaultMaxCountsPerWindow - 2*thirds},
		},
	}
	data, _ := json.Marshal(rec)
	_, err := bucket.Put(ctx, windowKey(ruleID, dimension), data)
	require.NoError(t, err)

	// One more increment pushes us over the cap → a bucket should be dropped.
	// Increment adds a 4th bucket (at "now"), then the cap loop drops the
	// oldest, leaving 3 buckets. The assertion verifies truncation happened
	// (we went from 4 → 3, not 3 → 3).
	count, err := wt.Increment(ctx, ruleID, dimension, now, 10*time.Minute)
	require.NoError(t, err)

	entry, err := bucket.Get(ctx, windowKey(ruleID, dimension))
	require.NoError(t, err)
	var stored WindowRecord
	require.NoError(t, json.Unmarshal(entry.Value(), &stored))

	// Before Increment: 3 buckets at cap. After: 4 buckets (new one added), then
	// oldest dropped → 3 remaining. Verify the result is ≤ 3 (truncation occurred).
	assert.LessOrEqual(t, len(stored.Buckets), 3, "oldest bucket should have been dropped when cap exceeded")
	assert.Greater(t, count, 0, "count should be positive")
}

// TestWindowTracker_ExpiredBuckets_PrunedOnWrite verifies that buckets outside
// the window are pruned on the next write, not counted.
func TestWindowTracker_ExpiredBuckets_PrunedOnWrite(t *testing.T) {
	t.Parallel()
	wt, _ := newWindowTrackerForTest()
	ctx := context.Background()
	now := time.Now()

	// Record one event well in the past (beyond a 1-minute window)
	past := now.Add(-90 * time.Second)
	_, err := wt.Increment(ctx, "rule-prune", "heidi", past, time.Minute)
	require.NoError(t, err)

	// Now increment at current time — past event should be pruned
	count, err := wt.Increment(ctx, "rule-prune", "heidi", now, time.Minute)
	require.NoError(t, err)
	assert.Equal(t, 1, count, "expired bucket should be pruned; only the current event counted")
}

// TestWindowTracker_NilBucket_ReturnsError verifies that a tracker constructed
// with a nil bucket returns an error rather than panicking.
func TestWindowTracker_NilBucket_ReturnsError(t *testing.T) {
	t.Parallel()
	wt := NewWindowTracker(nil, nil)
	_, err := wt.Increment(context.Background(), "rule-x", "dim-x", time.Now(), 5*time.Minute)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrWindowBucketNil)
}
