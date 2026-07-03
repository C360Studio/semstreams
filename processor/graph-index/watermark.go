package graphindex

import (
	"context"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/natsclient"
)

// degradedStuckAfter is how long IndexedRevision may stall while the index is not
// caught up before the status flips to "degraded" — the §4 stuck-watermark detector.
// Wall-clock based, so it is independent of how often a consumer polls status.
const degradedStuckAfter = 30 * time.Second

// revisionWatermark is the low-water-of-pending "caught up" tracker for the async,
// write-driven, lagging index pipeline (ADR-066 §1): a kv-watch over ENTITY_STATES
// feeds an unordered N-worker pool (plus an optional coalescer and an inline delete
// path). It answers "every DELIVERED ENTITY_STATES revision <= R has been applied
// and nothing <= R is still in flight."
//
// Why not "advance from indexed+1 past every contiguous completion": ENTITY_STATES
// runs at NATS KV default History=1 and WatchAll delivers via OrderedConsumer +
// DeliverLastPerSubject, so the delivered revision set is SPARSE (superseded
// revisions are purged, never delivered). Absolute-contiguity would wait forever on
// a purged revision. This tracker is defined only over delivered revisions:
//
//   - observedHigh: the highest revision ever delivered (monotonic).
//   - pending:      delivered-but-not-yet-completed revisions, keyed by revision.
//   - indexed():    pending.empty() ? observedHigh : (minPending - 1).
//
// Its correctness rests on delivery being monotonic ascending in Revision() — which
// OrderedConsumer guarantees at the nats-server storage layer for both the bootstrap
// replay and live updates — so any un-observed revision below observedHigh is purged
// and correctly skipped. All methods are safe for concurrent use (the watch
// goroutine observes; N pool workers, the coalescer callback, and the inline delete
// handler complete; the status handler reads).
type revisionWatermark struct {
	mu           sync.Mutex
	observedHigh uint64            // highest revision ever delivered to the watcher
	pending      map[uint64]string // in-flight revision -> its key
}

func newRevisionWatermark() *revisionWatermark {
	return &revisionWatermark{pending: make(map[uint64]string)}
}

// observe records revision r (for key) as delivered-and-in-flight. Called from the
// watch goroutine for EVERY delivered entry — updates AND deletes — before dispatch.
// observedHigh is monotonic; delivery is ascending so r exceeds any prior, but max()
// is kept for defensiveness. Revision 0 ("no revision") is ignored — stream
// sequences start at 1.
func (w *revisionWatermark) observe(r uint64, key string) {
	if r == 0 {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if r > w.observedHigh {
		w.observedHigh = r
	}
	w.pending[r] = key
}

// complete drains every in-flight revision for key with revision <= rev — the single
// key-scoped completion rule (ADR-066 §1). A pool worker calls it with the entry it
// processed, which drains the coalescer-collapsed lower revisions of that key that no
// worker sees individually; the inline delete handler calls it with the tombstone's
// revision, which drains an earlier pending update the delete supersedes. Key-scoped
// (never global-<=-rev) so one key's completion cannot drop a different key's pending
// revision.
func (w *revisionWatermark) complete(key string, rev uint64) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for r, k := range w.pending {
		if k == key && r <= rev {
			delete(w.pending, r)
		}
	}
}

// indexed returns the low-water-of-pending watermark: observedHigh when nothing is in
// flight, else minPending-1. Every delivered revision <= the result is applied and
// nothing <= it is still pending.
func (w *revisionWatermark) indexed() uint64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.pending) == 0 {
		return w.observedHigh
	}
	minRev := ^uint64(0)
	for r := range w.pending {
		if r < minRev {
			minRev = r
		}
	}
	return minRev - 1
}

// indexReadiness computes the honest readiness envelope (ADR-066) from the indexed
// watermark, the query-time target (ENTITY_STATES LastSeq), a stuck flag (the §4
// stuck-watermark detector), and the last-synced timestamp. It is pure so the subtle
// Ready/Lag/State logic is unit-testable without a live NATS.
//
//   - Ready  = target > 0 && indexed >= target (no max(0,…) clamp — indexed <= target
//     is structural, so Lag cannot underflow).
//   - State  = ready ? "ready" : (stuck ? "degraded" : "building"); ready wins.
func indexReadiness(indexed, target uint64, stuck bool, lastSynced string) graph.IndexStatusResponse {
	ready := target > 0 && indexed >= target
	var lag uint64
	if target > indexed {
		lag = target - indexed
	}
	state := graph.IndexStateBuilding
	switch {
	case ready:
		state = graph.IndexStateReady
	case stuck:
		state = graph.IndexStateDegraded
	}
	resp := graph.IndexStatusResponse{
		Ready:           ready,
		State:           state,
		IndexedRevision: indexed,
		TargetRevision:  target,
		Lag:             lag,
		LastSynced:      lastSynced,
	}
	if indexed > 0 {
		resp.Revision = strconv.FormatUint(indexed, 10)
	}
	return resp
}

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

	indexed := c.watermark.indexed()

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
	return indexReadiness(indexed, target, stuck, lastSynced)
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
