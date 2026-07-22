package graphembedding

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/graph/embedding"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
)

// The low-water-of-pending watermark mechanism lives in pkg/revlag (shared with
// graph-index); the readiness projection lives in graph.ComputeIndexStatus. This file
// holds graph-embedding's glue: the terminal-completion counter, the query-time
// target read, and the COMPLETIONS-based stuck detector (ADR-066 §3).

// embeddingDegradedAfter is how long the pipeline may go with ZERO terminal
// completions (while not caught up) before the status flips to "degraded". It is
// generous relative to graph-index's because embeddings add a slow external-LLM hop;
// and the detector keys off completions, not IndexedRevision, so a single slow call
// pinning Indexed while other workers finish is correctly NOT degraded.
const embeddingDegradedAfter = 120 * time.Second

// completeEmbedding drains the readiness watermark for a terminal outcome, counts the
// completion (feeds the stuck detector), and routes the current-failed map by outcome
// (#613). Called from every hop-1 immediate terminal AND the hop-2 onTerminal callback.
// sourceRevision==0 (a legacy record with no revision) makes Complete a no-op — hop-1's
// own bootstrap re-observe carries the real revision, so there is nothing to strand;
// ^uint64(0) is the max-rev drain for an unrecoverable (corrupt) record.
//
// The map update runs UNCONDITIONALLY, even on the nil-watermark early-boot / unit-test
// path, so a Failed/Skipped outcome is never silently dropped. The watermark advance is
// deliberately unchanged: it drains on ALL outcomes (deadlock avoidance).
func (c *Component) completeEmbedding(entityID string, sourceRevision uint64, outcome embedding.TerminalOutcome, reason string) {
	c.applyTerminalOutcome(entityID, sourceRevision, outcome, reason)
	if c.watermark == nil {
		return
	}
	c.watermark.Complete(entityID, sourceRevision)
	// A liveness counter for the stuck detector, not an exact terminal count: a
	// delete-branch completion whose revision hop-2 later re-completes (a no-op
	// Complete) bumps it twice. Harmless — it only ever over-reports progress, which
	// fails toward "healthy", never toward a false-degraded.
	c.embeddingCompletions.Add(1)
}

// handleEmbeddingStatusNATS serves graph.embedding.query.status (ADR-066 §3): the
// honest revision-lag readiness of the embedding pipeline. Ready means every eligible
// ENTITY_STATES revision <= the query-time target reached a TERMINAL embedding outcome
// (generated / failed / deliberately-skipped), not merely "embedding started". Takes
// no request body; the response JSON shape matches graph.IndexStatusResponse.
func (c *Component) handleEmbeddingStatusNATS(ctx context.Context, _ []byte) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	data, err := json.Marshal(c.computeEmbeddingStatus(ctx))
	if err != nil {
		// Unreachable (all-scalar struct), but classify like the sibling handlers so
		// a caller cannot mistake an error body for a zero-value success status.
		return nil, errs.ClassifiedCode(errs.ErrorTransient, graph.ErrorCodeInternal, errors.New("internal error"))
	}
	return data, nil
}

// computeEmbeddingStatus builds the embedding readiness envelope (ADR-066 §3):
// IndexedRevision from the low-water-of-pending watermark (advanced on every terminal
// outcome, so telemetry-only/no-text entities do not deadlock it), TargetRevision
// from the ENTITY_STATES stream LastSeq at query time, and the completions-based stuck
// detector for the degraded state.
func (c *Component) computeEmbeddingStatus(ctx context.Context) graph.IndexStatusResponse {
	// TWO distinct facts, deliberately not merged (the merge deadlocks — see below):
	//
	//   snapshotValidated — the WatchAll sentinel fired: the initial entries were
	//     DELIVERED and validated. It gates the early return that keeps queries and
	//     status honest while the first snapshot is still arriving.
	//   buildApplied — the public ADR-084 D2 bit: that snapshot reached a TERMINAL
	//     embedding outcome up to the enumeration-time target. Delivery is not
	//     application, and embedding is asynchronous, so publishing the sentinel as
	//     bootstrap_complete would let an unbounded health consumer serve a partially
	//     built cold index — the failure graph-index had, from the other end.
	//
	// Gating the early return on buildApplied instead would be a DEADLOCK: the latch
	// below is only reachable past that return, so the bit could never flip and the
	// component would report building forever. TestIntegration_EmbeddingReadiness_
	// DeadlockAvoidance caught that regression — incidentally, via its poll for Ready
	// timing out, since it predates this split and asserts the ADR-066 watermark
	// property rather than this guard. Treat it as a tripwire, not a specification.
	//
	// buildApplied is stamped on EVERY envelope below, hard stops included, so a
	// consumer can tell "still building" from "built, then broke".
	snapshotValidated := c.bootstrapComplete.Load()
	bootstrapped := c.buildApplied.Load()
	if c.resetState.Load() != nil {
		return graph.IndexStatusResponse{
			Ready: false, State: graph.IndexStateResetRequired,
			Code: graph.ErrorCodeGraphStateResetRequired, Reason: c.graphStateResetReason(),
			BootstrapComplete: bootstrapped,
		}
	}
	if c.watchUnavailable.Load() {
		return graph.IndexStatusResponse{
			Ready: false, State: graph.IndexStateDegraded,
			Code: graph.ErrorCodeIndexNotReady, Reason: "entity_state_watcher_unavailable",
			BootstrapComplete: bootstrapped,
		}
	}
	// Snapshot-validation gate, read from the snapshot above rather than re-Loading:
	// a second read could see the sentinel fire between the two and fall through to
	// stamp a stale value onto an envelope ComputeIndexStatus may mark Ready.
	if c.bootstrapStarted.Load() && !snapshotValidated {
		return graph.IndexStatusResponse{Ready: false, State: graph.IndexStateBuilding,
			BootstrapComplete: false}
	}
	// A status call before Start wired the watcher (early boot / unit tests) reports
	// building rather than panicking. Unreachable in production: setupQueryHandlers
	// runs after both watermark and entityStatesBucket are set in Start.
	if c.watermark == nil || c.entityStatesBucket == nil {
		return graph.IndexStatusResponse{Ready: false, State: graph.IndexStateBuilding,
			BootstrapComplete: bootstrapped}
	}

	indexed, indexedAt := c.watermark.IndexedAt()

	target, err := natsclient.BucketLastSeq(ctx, c.entityStatesBucket)
	if err != nil {
		c.logger.Warn("embedding status: failed to read ENTITY_STATES LastSeq target",
			slog.Any("error", err))
		return graph.IndexStatusResponse{
			Ready:             false,
			State:             graph.IndexStateDegraded,
			IndexedRevision:   indexed,
			BootstrapComplete: bootstrapped,
		}
	}

	// Latch the applied build here, where the watermark floor is in hand. Unlike
	// graph-index's latchBootstrap there is deliberately NO Ready term: this producer
	// has the enumeration-time target in hand on every call, so the fixed comparison is
	// always available and a live-target shortcut would only add a way to latch early.
	// The authoritatively-empty case latches via bootstrapTarget == 0.
	c.latchBuildApplied(indexed)

	// Current-failed detail (#613): FailedCount drives State=degraded in the shared
	// projection BEFORE "ready wins" (a producer caught up over failures is degraded, not
	// ready), and the bounded reason histogram + first-failure time ride the envelope so
	// an operator can tell an outage from a few poison entities. Read once, before the
	// projection, so the count that sets the state and the detail on the wire agree.
	failedCount, failedReasons, firstFailureAt := c.failedSnapshot()
	stuck, lastSynced := c.trackEmbeddingProgress(indexed, target)
	status := graph.ComputeIndexStatus(graph.IndexStatusInputs{
		Indexed:    indexed,
		Target:     target,
		Stuck:      stuck,
		LastSynced: lastSynced,
		// Commit time of the newest ENTITY_STATES revision that reached a terminal
		// embedding outcome — the age-of-view input to staleness_ms (ADR-083).
		IndexedAt:   indexedAt,
		FailedCount: failedCount,
	})
	// ComputeIndexStatus already echoed FailedCount and set State=degraded; attach the
	// bounded breakdown here (never a per-entity list on the watched key). Omitted when
	// there are no failures, so a healthy envelope is wire-unchanged.
	if failedCount > 0 {
		status.FailedReasons = failedReasons
		if !firstFailureAt.IsZero() {
			status.FirstFailureAt = firstFailureAt.UTC().Format(time.RFC3339)
		}
	}
	// Re-read: latchBuildApplied above may have flipped it on this very call, and an
	// envelope that reports Ready while denying its build finished is the one shape
	// the collapsed gate cannot tolerate (health is answered before coverage).
	status.BootstrapComplete = c.buildApplied.Load()
	return status
}

// latchBuildApplied flips the public bootstrap bit once the embedding pipeline has
// carried the initial snapshot to a terminal outcome — the applied floor reaching the
// enumeration-time target. Latching only; a later hard stop is carried by State.
func (c *Component) latchBuildApplied(indexed uint64) {
	if c.buildApplied.Load() {
		return
	}
	if c.bootstrapComplete.Load() && indexed >= c.bootstrapTarget.Load() {
		c.buildApplied.Store(true)
	}
}

// trackEmbeddingProgress is the COMPLETIONS-based stuck detector (ADR-066 §3). Unlike
// graph-index's IndexedRevision-based detector, it flips degraded only when NO
// terminal completion has fired for embeddingDegradedAfter while not caught up — so a
// slow single LLM call that pins IndexedRevision (its low revision runs while higher
// revisions finish out of order) is correctly healthy, not degraded. Returns the
// stuck flag and the last-completion timestamp for LastSynced.
func (c *Component) trackEmbeddingProgress(indexed, target uint64) (stuck bool, lastSynced string) {
	now := time.Now()
	completions := c.embeddingCompletions.Load()
	c.statusMu.Lock()
	defer c.statusMu.Unlock()
	if completions > c.lastCompletionsSeen || c.lastProgressAt.IsZero() {
		c.lastCompletionsSeen = completions
		c.lastProgressAt = now
	}
	caughtUp := target > 0 && indexed >= target
	stuck = !caughtUp && now.Sub(c.lastProgressAt) > embeddingDegradedAfter
	lastSynced = c.lastProgressAt.UTC().Format(time.RFC3339)
	return stuck, lastSynced
}
