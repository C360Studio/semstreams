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
	if c.resetState.Load() != nil {
		return graph.IndexStatusResponse{
			Ready:             false,
			State:             graph.IndexStateResetRequired,
			Code:              graph.ErrorCodeGraphStateResetRequired,
			Reason:            c.graphStateResetReason(),
			BootstrapComplete: c.indexBootstrapped.Load(),
		}
	}
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
		// BootstrapComplete tracks this branch's own verdict rather than the process
		// latch, preserving the producer invariant every consumer of the collapsed gate
		// relies on: Ready implies BootstrapComplete. An envelope claiming to be caught
		// up while reporting an unfinished build is self-contradictory, and the gate —
		// which asks health before coverage — would defer on it forever.
		//
		// It deliberately does NOT set the process latch: this Ready is the retired
		// sticky NAME_INDEX signal, which fires before the index is populated (gh#431),
		// so a later honest compute must be free to report an incomplete build.
		return graph.IndexStatusResponse{Ready: ready, State: state, BootstrapComplete: ready}
	}

	indexed, indexedAt := c.watermark.IndexedAt()

	statusBucket := c.entityStatesStatusBucket
	if statusBucket == nil {
		// Unit-test/early-construction fallback. Production Start always creates a
		// separate status handle before installing query subscriptions.
		statusBucket = c.entityStatesBucket
	}
	c.statusTargetMu.Lock()
	target, err := natsclient.BucketLastSeq(ctx, statusBucket)
	c.statusTargetMu.Unlock()
	if err != nil {
		// Can't read the target → can't honestly confirm caught-up. A backend fault
		// is a degraded signal, not "building" (ADR-066 §4).
		c.logger.Warn("index status: failed to read ENTITY_STATES LastSeq target",
			slog.Any("error", err))
		return graph.IndexStatusResponse{
			Ready:             false,
			State:             graph.IndexStateDegraded,
			IndexedRevision:   indexed,
			BootstrapComplete: c.indexBootstrapped.Load(),
		}
	}

	stuck, lastSynced := c.trackReadinessProgress(indexed, target)
	status := graph.ComputeIndexStatus(graph.IndexStatusInputs{
		Indexed:    indexed,
		Target:     target,
		Stuck:      stuck,
		LastSynced: lastSynced,
		// Commit time of the newest ENTITY_STATES revision this index covers — the
		// age-of-view input to staleness_ms (ADR-083). Zero until the watermark
		// covers a delivered revision, which the projection reports as "not
		// computable" rather than as a fresh view.
		IndexedAt: indexedAt,
	})
	status = applyKnownIncompleteOverrides(status, target,
		c.initialEnumerationComplete.Load(), c.failedCount.Load())
	return c.latchBootstrap(status)
}

// latchBootstrap sets the process-lifetime bootstrap latch from a computed envelope and
// stamps the wire bit (ADR-084 D2).
//
// The latch flips on the FIRST envelope whose post-override Ready is true, which is the
// same predicate the reverse-index query gate uses (EvaluateReadinessGate under
// sticky-bootstrap with BootstrapDone=false proceeds exactly on Ready, because hard
// stops carry Ready==false). Driving it from the status projection rather than only
// from an arriving query is what makes the bit honest on the wire: a deployment that
// never issues a direct reverse-index query would otherwise heartbeat
// bootstrap_complete=false forever while its index had been caught up for hours.
//
// Latching only — never cleared. A later hard stop (failedCount, stuck watermark, reset)
// is carried by State, which every gate treats as unconditional; clearing the latch
// there would conflate "never built" with "built, then broke", and the two want
// different operator responses.
func (c *Component) latchBootstrap(status graph.IndexStatusResponse) graph.IndexStatusResponse {
	if status.Ready {
		c.indexBootstrapped.Store(true)
	}
	status.BootstrapComplete = c.indexBootstrapped.Load()
	return status
}

// applyKnownIncompleteOverrides layers the two graph-index-local readiness
// invariants the shared ComputeIndexStatus projection cannot see onto its output.
//
// Empty-graph ready: an authoritatively empty graph (0/0 once initial enumeration
// is DELIVERED) is ready even though the revision-lag check requires target>0 —
// otherwise a fresh empty graph never becomes ready and every reverse-index query
// is rejected forever (gh#474 Codex #5). Gated on target==0 AND enumeration
// delivered — NOT on a non-empty replay whose workers may still be writing (#1).
//
// failedCount→degraded is UNCONDITIONAL (ADR-082, not gated on Ready): a failed
// required write/delete can make the reverse index report a smaller graph than
// exists even when the revision watermark is caught up, so this hard stop must
// hold at any lag. Under continuous write Ready is already false (Lag>0), so the
// old &&Ready guard SKIPPED the projection and left State=building — which a
// bounded-lag consumer would treat as runnable and cluster on partial topology.
// Applied AFTER the empty-graph override so a known-incomplete empty-after-
// enumeration index reads degraded, not ready.
func applyKnownIncompleteOverrides(status graph.IndexStatusResponse, target uint64, initialEnumerationComplete bool, failedCount int64) graph.IndexStatusResponse {
	if !status.Ready && target == 0 && initialEnumerationComplete {
		status.Ready = true
		status.State = graph.IndexStateReady
	}
	if failedCount > 0 {
		status.Ready = false
		status.State = graph.IndexStateDegraded
	}
	return status
}

func (c *Component) markGraphStateResetRequired(reason string) {
	c.resetState.CompareAndSwap(nil, &graph.StateContractError{Reason: graph.StateResetReason(reason)})
}

func (c *Component) graphStateResetReason() string {
	if state := c.resetState.Load(); state != nil {
		return string(state.Reason)
	}
	return "incompatible_entity_state"
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
