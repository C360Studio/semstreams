// Package rule - Schedule tracking for cron rule firing.
//
// ScheduleTracker persists per-rule last-fired timestamps to the
// RULE_SCHEDULES KV bucket so that:
//
//  1. Restart-recovery can detect missed fires by comparing the persisted
//     last-fired timestamp against the cron schedule's expected next fire.
//  2. Operators (and downstream components like governance startup hooks)
//     can read last-fire state without coupling to scheduler internals.
//
// The bucket is intentionally simple: one JSON record per rule, keyed by
// rule ID. Records are upserted on every successful fire; they have no
// TTL (retention is the rule's own enable/disable lifecycle).
//
// # Key shape (retrofit-safe)
//
// MVP keys are the bare rule ID — `{ruleID}` — because every cron rule
// fires once globally per tick. When the deferred per-entity fan-out
// extension lands (Definition.ForEach), keys will gain a `.{entityID}`
// suffix to track last-fired per fanout target. Existing single-fire
// rules keep the bare-rule-ID shape; the suffix is purely additive and
// requires no migration. See ADR-031 retrofit seams for context.
//
// # External read access
//
// Governance startup hooks (kill-switch sweeps, chain-hash audits) need
// to read last-fired timestamps to issue catch-up sweeps when a missed
// fire crosses a meaningful interval. Two access patterns are supported:
//
//  1. In-process callers hold a *ScheduleTracker reference and call
//     LastFiredAt(ctx, ruleID).
//  2. Out-of-process callers read RULE_SCHEDULES KV directly using the
//     documented record shape (LastFireRecord). Bucket name and JSON
//     shape are part of the framework's public contract.
package rule

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/c360studio/semstreams/natsclient"
)

// ScheduleBucketName is the canonical NATS KV bucket name for per-rule
// last-fired timestamps. Exported so out-of-process readers (governance
// startup hooks, ops dashboards) can address the bucket without
// stringly-typed duplication.
const ScheduleBucketName = "RULE_SCHEDULES"

// LastFireRecord is the on-disk shape of a per-rule last-fired record.
// JSON field names are part of the framework's public contract — out-of-
// process readers depend on them; renames require a migration.
type LastFireRecord struct {
	// RuleID is the cron rule's stable identifier. Duplicates the KV key
	// so that List() consumers can decode a record without re-threading
	// the key alongside it.
	RuleID string `json:"rule_id"`

	// ScheduleSpec is the rule's cron expression at the time of the
	// recorded fire. Useful for operators reading the bucket directly
	// and for missed-fire detection that wants to compute the expected
	// fire interval without re-loading rule definitions.
	ScheduleSpec string `json:"schedule_spec"`

	// LastFiredAt is the wallclock at which the most recent fire began
	// dispatching actions. UTC; serialised RFC3339Nano via Go's default
	// time.Time JSON encoder. Nanosecond precision is preserved best-
	// effort across platforms — callers that depend on exact equality
	// (e.g. round-trip tests) should truncate to a known resolution
	// before write rather than relying on the encoder.
	LastFiredAt time.Time `json:"last_fired_at"`
}

// ErrScheduleRecordNotFound is returned by LastFiredAt when no fire has
// been recorded for the requested rule. Callers should treat this as
// "never fired" rather than an error condition — it is the expected
// state for a freshly-deployed rule.
var ErrScheduleRecordNotFound = errors.New("schedule record not found")

// ScheduleTracker persists last-fired timestamps for cron rules to KV.
// Methods are safe for concurrent use; the underlying NATS KV client
// already serialises writes.
type ScheduleTracker struct {
	bucket natsclient.KVBucket
	logger *slog.Logger
}

// NewScheduleTracker builds a tracker bound to the supplied bucket. A
// nil bucket is permitted so the rule processor can degrade gracefully
// when bucket creation fails on startup — every method returns an error
// when bucket is nil rather than panicking, mirroring StateTracker's
// nil-tolerant behaviour.
func NewScheduleTracker(bucket natsclient.KVBucket, logger *slog.Logger) *ScheduleTracker {
	if logger == nil {
		logger = slog.Default()
	}
	return &ScheduleTracker{
		bucket: bucket,
		logger: logger,
	}
}

// LastFiredAt returns the most recent fire timestamp for the given rule.
// Returns ErrScheduleRecordNotFound when no fire has been recorded — the
// caller should treat that as "never fired", not as an error to surface.
//
// The schedule spec stored alongside the timestamp is returned too so
// missed-fire detection can compute the expected interval without
// re-loading the live rule definition (which may have been hot-reloaded
// to a different spec since the last fire).
func (st *ScheduleTracker) LastFiredAt(ctx context.Context, ruleID string) (LastFireRecord, error) {
	if ruleID == "" {
		return LastFireRecord{}, fmt.Errorf("rule ID cannot be empty")
	}
	if st.bucket == nil {
		return LastFireRecord{}, fmt.Errorf("schedule tracker bucket is nil")
	}

	entry, err := st.bucket.Get(ctx, scheduleKey(ruleID))
	if err != nil {
		if errors.Is(err, natsclient.ErrKeyNotFound) {
			return LastFireRecord{}, ErrScheduleRecordNotFound
		}
		return LastFireRecord{}, fmt.Errorf("get schedule record: %w", err)
	}

	var rec LastFireRecord
	if err := json.Unmarshal(entry.Value, &rec); err != nil {
		return LastFireRecord{}, fmt.Errorf("unmarshal schedule record: %w", err)
	}
	return rec, nil
}

// RecordFire upserts the last-fired record for the given rule. Called by
// CronScheduler.fire after a successful action dispatch. The schedule
// spec is captured at the moment of the fire so a hot-reload that
// changes the cron expression doesn't retroactively rewrite history.
//
// Errors are surfaced to the caller; the scheduler logs them as warnings
// (a transient KV write failure must not crash the scheduler goroutine —
// the next fire will overwrite the record).
func (st *ScheduleTracker) RecordFire(ctx context.Context, ruleID, scheduleSpec string, firedAt time.Time) error {
	if ruleID == "" {
		return fmt.Errorf("rule ID cannot be empty")
	}
	if st.bucket == nil {
		return fmt.Errorf("schedule tracker bucket is nil")
	}

	rec := LastFireRecord{
		RuleID:       ruleID,
		ScheduleSpec: scheduleSpec,
		LastFiredAt:  firedAt.UTC(),
	}
	data, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("marshal schedule record: %w", err)
	}
	if _, err := st.bucket.Put(ctx, scheduleKey(ruleID), data); err != nil {
		return fmt.Errorf("put schedule record: %w", err)
	}
	return nil
}

// Bucket returns the underlying KVBucket handle. Exposed so in-process
// callers (governance startup hooks, ops dashboards in the same binary)
// can access the bucket for Watch or Keys without re-resolving it via
// js.KeyValue. The returned handle may be nil if the tracker was
// constructed in nil-bucket mode (degraded startup); callers must
// nil-check.
func (st *ScheduleTracker) Bucket() natsclient.KVBucket {
	return st.bucket
}

// Delete removes the persisted record for a rule. Called when a rule is
// removed from configuration so stale records don't linger. Missing
// records are treated as success — Delete is idempotent.
func (st *ScheduleTracker) Delete(ctx context.Context, ruleID string) error {
	if st.bucket == nil {
		return fmt.Errorf("schedule tracker bucket is nil")
	}
	err := st.bucket.Delete(ctx, scheduleKey(ruleID))
	if err != nil && !errors.Is(err, natsclient.ErrKeyNotFound) {
		return fmt.Errorf("delete schedule record: %w", err)
	}
	return nil
}

// List returns every persisted last-fire record. Used by the scheduler
// on Start to detect missed fires and by operators inspecting bucket
// state. Records are returned in undefined order; callers that need a
// stable order should sort by RuleID.
//
// A bucket with no records returns an empty slice and no error.
func (st *ScheduleTracker) List(ctx context.Context) ([]LastFireRecord, error) {
	if st.bucket == nil {
		return nil, fmt.Errorf("schedule tracker bucket is nil")
	}

	keys, err := st.bucket.Keys(ctx)
	if err != nil {
		// NATS KV returns ErrNoKeysFound for an empty bucket; treat as
		// empty rather than an error to keep the caller's branch count
		// low (the on-startup walk runs against a clean bucket on first
		// deploy).
		if errors.Is(err, natsclient.ErrNoKeysFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("list schedule keys: %w", err)
	}

	records := make([]LastFireRecord, 0, len(keys))
	for _, k := range keys {
		entry, err := st.bucket.Get(ctx, k)
		if err != nil {
			// Skip records that disappeared between Keys and Get — the
			// scheduler treats missing records as "never fired" anyway.
			if errors.Is(err, natsclient.ErrKeyNotFound) {
				continue
			}
			st.logger.Warn("Failed to read schedule record",
				"key", k,
				"error", err)
			continue
		}
		var rec LastFireRecord
		if err := json.Unmarshal(entry.Value, &rec); err != nil {
			st.logger.Warn("Skipping malformed schedule record",
				"key", k,
				"error", err)
			continue
		}
		records = append(records, rec)
	}
	return records, nil
}

// scheduleKey returns the KV key for a rule's last-fire record. Kept as
// a single chokepoint so the future per-entity fan-out extension can
// add a suffix here without rewriting every call site.
func scheduleKey(ruleID string) string {
	return ruleID
}
