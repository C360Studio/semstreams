package graphembedding

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/c360studio/semstreams/graph"
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

// completeEmbedding drains the readiness watermark for a terminal outcome and counts
// the completion (feeds the stuck detector). Called from every hop-1 immediate
// terminal AND the hop-2 onTerminal callback. sourceRevision==0 (a legacy record with
// no revision) makes Complete a no-op — hop-1's own bootstrap re-observe carries the
// real revision, so there is nothing to strand; ^uint64(0) is the max-rev drain for
// an unrecoverable (corrupt) record.
func (c *Component) completeEmbedding(entityID string, sourceRevision uint64) {
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
	// bootstrapComplete is this producer's ADR-084 D2 latch: the ENTITY_STATES
	// WatchAll initial-sync sentinel, which fires once the complete snapshot has been
	// delivered and validated (including the empty-graph case, where the sentinel is
	// the first update). It is stamped on EVERY envelope below, hard stops included,
	// so a consumer can tell "still building its first snapshot" from "built, then
	// broke" without correlating fields.
	bootstrapped := c.bootstrapComplete.Load()
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
	if c.bootstrapStarted.Load() && !c.bootstrapComplete.Load() {
		return graph.IndexStatusResponse{Ready: false, State: graph.IndexStateBuilding}
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

	stuck, lastSynced := c.trackEmbeddingProgress(indexed, target)
	status := graph.ComputeIndexStatus(graph.IndexStatusInputs{
		Indexed:    indexed,
		Target:     target,
		Stuck:      stuck,
		LastSynced: lastSynced,
		// Commit time of the newest ENTITY_STATES revision that reached a terminal
		// embedding outcome — the age-of-view input to staleness_ms (ADR-083).
		IndexedAt: indexedAt,
	})
	status.BootstrapComplete = bootstrapped
	return status
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
