// Package rule - Sliding-window counter tracking for count_in_window rules.
//
// WindowTracker persists per-{ruleID}:{dimension} bucketed event counters to
// the RULE_WINDOWS KV bucket. Each KV value holds a bounded list of time-bucketed
// counts — a bucketed counter, NOT a timestamp array — so the record size is
// O(N_buckets) regardless of event rate.
//
// # Storage shape (public contract)
//
// Key:   {ruleID}.{dimension}
// Value: JSON-encoded WindowRecord
//
//	{
//	  "buckets": [
//	    {"window_start": 1700000000, "count": 47},
//	    {"window_start": 1700000060, "count": 53}
//	  ]
//	}
//
// The key shape uses "." as the separator between ruleID and dimension. "."
// is in the valid NATS KV key character set ([-/_=\.a-zA-Z0-9]+). Both
// components are operator-supplied strings; the tracker does NOT validate or
// sanitise them — callers (config validation, the rule expression evaluator)
// are responsible for ensuring neither component contains characters that
// would cause NATS KV key rejection (i.e. only [-/_=\.a-zA-Z0-9] are safe).
//
// # Bucket granularity
//
// Bucket size is fixed at 1 minute for all windows. A 5-minute window therefore
// has at most 5 entries per key; a 30-minute window has at most 30. The 1-minute
// granularity is a deliberate simplicity trade-off: count-in-window rules are
// typically used for rate-limiting (seconds to minutes), not precision metering
// (sub-second). Callers that need sub-minute precision should use a smaller
// window or a dedicated metrics system.
//
// # Cardinality safety
//
// The RULE_WINDOWS bucket can become arbitrarily large if rules fire against an
// unbounded set of dimension values (e.g. unique user IDs). Two caps bound this:
//
//  1. DefaultMaxDistinctDimensionsPerRule — max distinct {ruleID}:{dim} keys
//     per rule. Enforced by counting Keys() with the ruleID prefix before
//     creating a new key. When the cap is hit, Increment returns -1.
//  2. DefaultMaxCountsPerWindow — max total summed count across buckets per key.
//     Enforced after pruning; when exceeded, oldest buckets are dropped.
//
// See chunk 3b for the janitor cron rule that sweeps stale keys from the bucket.
//
// # Thread safety
//
// WindowTracker is safe for concurrent use; each Increment call performs a CAS
// read-modify-write loop using NATS KV Update (optimistic concurrency). A bounded
// retry loop handles the case where a concurrent Increment to the same key races;
// after maxCASRetries the operation returns an error rather than looping forever.
package rule

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

// WindowsBucketName is the canonical NATS KV bucket name for per-rule
// sliding-window counters. Exported so out-of-process readers (janitor cron
// rules, ops dashboards, governance startup hooks) can address the bucket
// without stringly-typed duplication. The janitor config in
// configs/rules/system/window-janitor.json references this constant.
//
// See chunk 3b for the opt-in janitor cron rule that sweeps stale keys.
const WindowsBucketName = "RULE_WINDOWS"

// Framework-level cardinality caps. These bounds exist to prevent a runaway
// set of distinct callers (or other dimension values) from writing unbounded
// data into RULE_WINDOWS — a DoS surface that an attacker could exploit by
// sending requests with fresh, unique caller IDs.
//
// Rule-level overrides via value.max_dimensions / value.max_per_window are
// validated at config load and may only LOWER these caps, never raise them.
const (
	// DefaultMaxDistinctDimensionsPerRule is the maximum number of distinct
	// dimension values tracked per rule. A "dimension" is the evaluated
	// field value (e.g. caller ID, IP address). When this cap is hit, new
	// dimensions are rejected; existing dimensions continue to be counted.
	DefaultMaxDistinctDimensionsPerRule = 10000

	// DefaultMaxCountsPerWindow is the maximum total event count summed
	// across all buckets for a single {ruleID}:{dimension} key. When this
	// cap is hit, the oldest bucket is dropped to make room, bounding the
	// per-key byte size.
	DefaultMaxCountsPerWindow = 10000
)

// bucketGranularity is the fixed time granularity for buckets. All windows use
// 1-minute buckets regardless of window size.
const bucketGranularity = time.Minute

// maxCASRetries is the maximum number of optimistic-concurrency retry
// attempts before Increment gives up and returns an error.
const maxCASRetries = 5

// WindowBucket is one time-bucketed count within a WindowRecord.
// WindowStart is the Unix timestamp (seconds) of the bucket's opening edge.
// JSON field names are part of the public contract — the janitor cron rule
// and out-of-process readers depend on them.
type WindowBucket struct {
	WindowStart int64 `json:"window_start"` // Unix seconds
	Count       int   `json:"count"`
}

// WindowRecord is the on-disk shape stored under each {ruleID}:{dimension} key.
// Exported so janitor rules and ops dashboards can decode it without coupling to
// tracker internals.
type WindowRecord struct {
	Buckets []WindowBucket `json:"buckets"`
}

// sumCounts returns the total event count across all buckets.
func (r WindowRecord) sumCounts() int {
	total := 0
	for _, b := range r.Buckets {
		total += b.Count
	}
	return total
}

// ErrWindowBucketNil is returned when a WindowTracker method is called on a
// tracker constructed without a valid bucket (degraded startup path).
var ErrWindowBucketNil = errors.New("window tracker bucket is nil")

// WindowTracker implements expression.WindowStateReader using a real NATS KV
// bucket. It satisfies the WindowStateReader interface so the expression
// evaluator can call Increment without knowing about KV details.
//
// Thread safety: safe for concurrent use. The KV CAS loop serialises concurrent
// Increments to the same key; the dimension-count check uses a package-level
// mutex to avoid TOCTOU races on the cardinality cap.
type WindowTracker struct {
	bucket  jetstream.KeyValue
	logger  *slog.Logger
	metrics *Metrics // nil-safe; incremented when non-nil
	// dimMu guards the cardinality cap check-then-write path.
	// Held only for the brief period between counting existing keys and
	// deciding whether to create a new one; released before the KV Put.
	dimMu sync.Mutex
}

// NewWindowTracker builds a tracker bound to the supplied KV bucket. A nil
// bucket is permitted — every method returns ErrWindowBucketNil so the rule
// processor can degrade gracefully (no window counting) without refusing to
// start, matching the ScheduleTracker nil-bucket posture.
func NewWindowTracker(bucket jetstream.KeyValue, logger *slog.Logger) *WindowTracker {
	if logger == nil {
		logger = slog.Default()
	}
	return &WindowTracker{bucket: bucket, logger: logger}
}

// setMetrics wires in the rule-processor metrics singleton. Called by
// Processor after NewWindowTracker so the tracker can record cap-hit and
// truncation counters without requiring metrics at construction time.
func (wt *WindowTracker) setMetrics(m *Metrics) { wt.metrics = m }

// Increment records one occurrence of dimension under ruleID at now and returns
// the rolling count within window. Implements expression.WindowStateReader.
//
// Returns (-1, nil) when the dimension would exceed DefaultMaxDistinctDimensionsPerRule
// for the given rule. The rule expression evaluator treats -1 as a non-match.
//
// Returns (count, nil) on success, where count includes the current occurrence.
// Returns (0, err) on KV or serialisation failures.
func (wt *WindowTracker) Increment(ctx context.Context, ruleID, dimension string, now time.Time, window time.Duration) (int, error) {
	if wt.bucket == nil {
		return 0, ErrWindowBucketNil
	}
	if ruleID == "" {
		return 0, fmt.Errorf("window tracker: ruleID cannot be empty")
	}
	if dimension == "" {
		return 0, fmt.Errorf("window tracker: dimension cannot be empty")
	}

	key := windowKey(ruleID, dimension)
	prefix := ruleID + "."

	// Check cardinality cap before creating a new key.
	// dimMu serialises the check-then-first-write path to prevent concurrent
	// Increments from racing past the cap check.
	wt.dimMu.Lock()
	needsCreate, err := wt.needsNewKey(ctx, key, prefix)
	if err != nil {
		wt.dimMu.Unlock()
		return 0, fmt.Errorf("window tracker: cardinality check failed: %w", err)
	}
	if needsCreate < 0 {
		// Cap hit — reject without creating the key; increment metric
		wt.dimMu.Unlock()
		if wt.metrics != nil && wt.metrics.windowDimCapHit != nil {
			wt.metrics.windowDimCapHit.WithLabelValues(ruleID).Inc()
		}
		return -1, nil
	}
	wt.dimMu.Unlock()

	// CAS read-modify-write loop for concurrent safety. Only retry when the KV
	// Update call returns jetstream.ErrKeyExists (optimistic-concurrency conflict
	// from a concurrent Increment to the same key). Any other error from
	// incrementOnce is a real failure (network, serialization, bucket gone) and
	// bubbles immediately — burning retries on non-conflict errors would mask
	// them and waste maxCASRetries × round-trip latency before surfacing.
	for attempt := 0; attempt < maxCASRetries; attempt++ {
		count, casConflict, err := wt.incrementOnce(ctx, key, now, window)
		if err != nil {
			return 0, err
		}
		if !casConflict {
			return count, nil
		}
	}

	return 0, fmt.Errorf("window tracker: CAS retry limit exceeded for key %q after %d attempts", key, maxCASRetries)
}

// needsNewKey checks whether adding 'key' to the bucket would exceed the
// cardinality cap for the rule (identified by prefix). Returns:
//
//	1  → key already exists (no new key needed)
//	0  → key is new and within cap
//	-1 → cap hit, reject new key
func (wt *WindowTracker) needsNewKey(ctx context.Context, key, prefix string) (int, error) {
	// Fast path: key already exists
	if _, err := wt.bucket.Get(ctx, key); err == nil {
		return 1, nil
	} else if !errors.Is(err, jetstream.ErrKeyNotFound) {
		return 0, err
	}

	// Count existing keys for this rule
	keys, err := wt.bucket.Keys(ctx)
	if err != nil {
		if errors.Is(err, jetstream.ErrNoKeysFound) {
			return 0, nil // empty bucket, safe to create
		}
		return 0, err
	}

	count := 0
	for _, k := range keys {
		if strings.HasPrefix(k, prefix) {
			count++
		}
	}

	if count >= DefaultMaxDistinctDimensionsPerRule {
		wt.logger.Warn("Window tracker: cardinality cap hit, rejecting new dimension",
			"rule_id", strings.TrimSuffix(prefix, "."),
			"cap", DefaultMaxDistinctDimensionsPerRule)
		return -1, nil
	}
	return 0, nil
}

// incrementOnce performs a single optimistic read-modify-write. Returns:
//   - (count, false, nil) on success
//   - (0, true, nil) when a CAS conflict was detected (jetstream.ErrKeyExists — caller should retry)
//   - (0, false, err) on any other error (network, marshal, bucket gone — caller must not retry)
func (wt *WindowTracker) incrementOnce(ctx context.Context, key string, now time.Time, window time.Duration) (int, bool, error) {
	var rec WindowRecord
	var revision uint64

	// Read current record (or start from empty if absent)
	entry, err := wt.bucket.Get(ctx, key)
	if err != nil {
		if !errors.Is(err, jetstream.ErrKeyNotFound) {
			return 0, false, fmt.Errorf("window tracker: get %q: %w", key, err)
		}
		// Key absent — create fresh record
		rec = WindowRecord{}
		revision = 0
	} else {
		revision = entry.Revision()
		if err := json.Unmarshal(entry.Value(), &rec); err != nil {
			return 0, false, fmt.Errorf("window tracker: unmarshal %q: %w", key, err)
		}
	}

	// Prune expired buckets and add the current occurrence to the appropriate bucket
	rec = wt.applyIncrement(rec, now, window)

	// Enforce per-window count cap: drop oldest bucket when total exceeds cap
	for rec.sumCounts() > DefaultMaxCountsPerWindow && len(rec.Buckets) > 1 {
		rec.Buckets = rec.Buckets[1:]
		wt.logger.Warn("Window tracker: per-window count cap hit, dropped oldest bucket",
			"key", key)
		// Extract ruleID from key for metric labelling (key is "ruleID:dimension")
		if wt.metrics != nil && wt.metrics.windowTruncation != nil {
			if idx := strings.Index(key, "."); idx > 0 {
				wt.metrics.windowTruncation.WithLabelValues(key[:idx]).Inc()
			}
		}
	}

	data, err := json.Marshal(rec)
	if err != nil {
		return 0, false, fmt.Errorf("window tracker: marshal %q: %w", key, err)
	}

	// Write back using optimistic concurrency.
	// Only signal casConflict=true when the KV layer returns jetstream.ErrKeyExists —
	// the documented CAS-conflict sentinel for both Put (key already exists from a
	// concurrent creator) and Update (revision mismatch). Any other error is a real
	// failure that must not be retried.
	if revision == 0 {
		// Key was absent — use Put (NATS KV Put is idempotent for first write)
		if _, err := wt.bucket.Put(ctx, key, data); err != nil {
			if errors.Is(err, jetstream.ErrKeyExists) {
				// Another goroutine created the key between our Get and Put — retry
				return 0, true, nil
			}
			return 0, false, fmt.Errorf("window tracker: put %q: %w", key, err)
		}
	} else {
		if _, err := wt.bucket.Update(ctx, key, data, revision); err != nil {
			if errors.Is(err, jetstream.ErrKeyExists) {
				// Optimistic concurrency conflict — another writer changed the revision
				return 0, true, nil
			}
			return 0, false, fmt.Errorf("window tracker: update %q: %w", key, err)
		}
	}

	return rec.sumCounts(), false, nil
}

// applyIncrement prunes buckets older than window and increments the count
// in the current 1-minute bucket, creating it if necessary.
func (wt *WindowTracker) applyIncrement(rec WindowRecord, now time.Time, window time.Duration) WindowRecord {
	// Determine cutoff: events older than this are expired
	cutoff := now.Add(-window)
	cutoffUnix := cutoff.Unix()

	// Current bucket start (truncate to 1-minute boundary)
	currentBucketStart := now.Truncate(bucketGranularity).Unix()

	// Filter expired buckets and carry forward live ones
	live := make([]WindowBucket, 0, len(rec.Buckets)+1)
	for _, b := range rec.Buckets {
		if b.WindowStart >= cutoffUnix {
			live = append(live, b)
		}
	}

	// Update or append the current bucket
	if len(live) > 0 && live[len(live)-1].WindowStart == currentBucketStart {
		live[len(live)-1].Count++
	} else {
		live = append(live, WindowBucket{WindowStart: currentBucketStart, Count: 1})
	}

	return WindowRecord{Buckets: live}
}

// Bucket returns the underlying jetstream.KeyValue handle for in-process
// readers that need full KV API access (Watch, ListKeys). May be nil when
// the tracker was constructed in degraded mode.
func (wt *WindowTracker) Bucket() jetstream.KeyValue {
	return wt.bucket
}

// windowKey constructs the KV key for a {ruleID}.{dimension} pair.
// The "." separator is valid under the NATS KV key character set
// ([-/_=\.a-zA-Z0-9]+) and is used as a hierarchy separator throughout
// the NATS ecosystem. The cardinality cap prefix scan uses "{ruleID}."
// as the prefix to match all dimension keys for a given rule.
//
// Retrofit note: keys are bare "{ruleID}.{dimension}" in the MVP. When
// the deferred per-entity fan-out extension lands, keys may gain a
// secondary component — any change requires a migration because RULE_WINDOWS
// is a public-contract bucket that external readers may query directly.
func windowKey(ruleID, dimension string) string {
	return ruleID + "." + dimension
}
