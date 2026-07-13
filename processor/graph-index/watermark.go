package graphindex

import (
	"context"
	"log/slog"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/natsclient"
)

// The low-water-of-pending watermark mechanism lives in pkg/revlag (shared with
// graph-embedding); the readiness projection lives in graph.ComputeIndexStatus. This
// file holds graph-index's glue: the query-time target read and the §4 stuck detector.

// degradedStuckAfter is how long IndexedRevision may stall while the index is not
// caught up before the status flips to "degraded" — the §4 stuck-watermark detector.
// Wall-clock based, so it is independent of how often a consumer polls status.
const degradedStuckAfter = 30 * time.Second

// computeIndexStatus builds the honest revision-lag readiness envelope (ADR-066):
// IndexedRevision from the low-water-of-pending watermark, TargetRevision from the
// ENTITY_STATES stream LastSeq read at query time, and the §4 stuck-watermark
// detector for the degraded state.
func (c *Component) computeIndexStatus(ctx context.Context) graph.IndexStatusResponse {
	// A status call before Start wired the watcher (early boot / unit tests that do
	// not start it) falls back to the legacy sticky signal rather than panicking.
	// This MUST NOT become reachable in production: setupQueryHandlers (which
	// subscribes this handler) runs after both watermark and entityStatesBucket are
	// set in Start, so a live request always takes the honest revision-lag path. The
	// fallback returns the exact false-ready signal ADR-066 retires — keep it caged.
	if c.watermark == nil || c.entityStatesBucket == nil {
		ready := c.nameIndexIsReady(ctx)
		state := graph.IndexStateBuilding
		if ready {
			state = graph.IndexStateReady
		}
		return graph.IndexStatusResponse{Ready: ready, State: state}
	}

	indexed := c.watermark.Indexed()

	target, err := natsclient.BucketLastSeq(ctx, c.entityStatesBucket)
	if err != nil {
		// Can't read the target → can't honestly confirm caught-up. A backend fault
		// is a degraded signal, not "building" (ADR-066 §4).
		c.logger.Warn("index status: failed to read ENTITY_STATES LastSeq target",
			slog.Any("error", err))
		return graph.IndexStatusResponse{
			Ready:           false,
			State:           graph.IndexStateDegraded,
			IndexedRevision: indexed,
		}
	}

	stuck, lastSynced := c.trackReadinessProgress(indexed, target)
	status := graph.ComputeIndexStatus(indexed, target, stuck, lastSynced)

	// Withhold authoritative readiness while any entity's required index writes are
	// unresolved (gh#474 P1b): the reverse index is known-incomplete even when the
	// revision watermark is caught up, so incoming/byName must not report a smaller
	// graph than exists. Cleared when every failed entity re-indexes cleanly.
	if c.failedCount.Load() > 0 && status.Ready {
		status.Ready = false
		status.State = graph.IndexStateDegraded
	}
	return status
}

// trackReadinessProgress updates the wall-clock stuck-watermark detector and returns
// whether IndexedRevision has stalled while not caught-up (→ degraded), plus the
// last-advance timestamp for LastSynced. Wall-clock based so it is independent of how
// often a consumer polls status (ADR-066 §4).
func (c *Component) trackReadinessProgress(indexed, target uint64) (stuck bool, lastSynced string) {
	now := time.Now()
	c.statusMu.Lock()
	defer c.statusMu.Unlock()
	if indexed > c.lastIndexedSeen || c.lastProgressAt.IsZero() {
		c.lastIndexedSeen = indexed
		c.lastProgressAt = now
	}
	caughtUp := target > 0 && indexed >= target
	stuck = !caughtUp && now.Sub(c.lastProgressAt) > degradedStuckAfter
	lastSynced = c.lastProgressAt.UTC().Format(time.RFC3339)
	return stuck, lastSynced
}
