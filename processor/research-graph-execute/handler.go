package researchexecute

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/c360studio/semstreams/agentic/research"
)

// GraphQueryClient is the narrow surface this component consumes
// from graph-query / graph-index. Production wraps NATS-direct
// subjects (graph.query.entitiesByPrefix, graph.query.searchGraph,
// etc.); tests substitute an in-memory fake so the matrix doesn't
// need a live graph stack.
//
// Per-method semantics:
//
//   - EntityState: returns the current Triples for the named
//     entities, projected into Evidence (one per entity).
//   - PredicateWalk: from each seed, returns Evidence for entities
//     reachable within MaxHops via Predicates (empty Predicates =
//     all).
//   - TemporalRange: returns Evidence for entities within the time
//     window, optionally filtered by topic.
//   - BM25: text search via graph-query's existing BM25 surface.
//
// All methods MUST stamp Tier + Source on every returned Evidence
// (taken from the SubQuery they were dispatched for) so provenance
// stays honest end-to-end. The orchestrator does NOT re-stamp.
type GraphQueryClient interface {
	EntityState(ctx context.Context, args EntityStateArgs, tier, source string, limit int) ([]research.Evidence, error)
	PredicateWalk(ctx context.Context, args PredicateWalkArgs, tier, source string, limit int) ([]research.Evidence, error)
	TemporalRange(ctx context.Context, args TemporalRangeArgs, tier, source string, limit int) ([]research.Evidence, error)
	BM25(ctx context.Context, args BM25Args, tier, source string, limit int) ([]research.Evidence, error)
}

// LoopStore is the AGENT_LOOPS read/write surface this component
// consumes. Production wraps natsclient.KVStore; tests substitute
// an in-memory map.
type LoopStore interface {
	// GetIntent loads the research_intent payload from the
	// research.requested.<loopID> key.
	GetIntent(ctx context.Context, loopID string) (*research.Intent, error)

	// GetClassifierOutput loads the upstream ClassifierOutput from
	// the classify.complete.<loopID> trigger key. Needed for
	// walk_seeds candidate_index resolution + decompose entity_type
	// anchoring.
	GetClassifierOutput(ctx context.Context, loopID string) (*research.ClassifierOutput, error)

	// GetRouteDecision loads the upstream RouteDecision from the
	// route.complete.<loopID> trigger key. Drives sub-query
	// materialization.
	GetRouteDecision(ctx context.Context, loopID string) (*research.RouteDecision, error)

	// PutExecutionOutput writes the ExecutionOutput envelope at
	// R3's trigger key execute.complete.<loopID>.
	PutExecutionOutput(ctx context.Context, loopID string, envelope []byte) error

	// PutSnapshot writes the envelope at the stable non-trigger
	// key execute.snapshot.<loopID> so operators / downstream
	// queryability can read without racing R3's wildcard watcher.
	PutSnapshot(ctx context.Context, loopID string, envelope []byte) error
}

// Sentinel errors for diagnostic clarity in handler logs + so tests
// can assert via errors.Is.
var (
	errIntentNotFound          = fmt.Errorf("research intent not found")
	errClassifierOutputMissing = fmt.Errorf("classifier output not found")
	errRouteDecisionMissing    = fmt.Errorf("route decision not found")
)

// extractLoopIDFromSubject pulls the loop_id off
// component.execute_subqueries.<loop_id>. Returns empty on shape
// mismatch — handler treats as routing-config bug.
func extractLoopIDFromSubject(subject string) string {
	const prefix = "component.execute_subqueries."
	if !strings.HasPrefix(subject, prefix) {
		return ""
	}
	return subject[len(prefix):]
}

// resolveSeedRefs maps SeedRef entries to full 6-part entity IDs
// using the upstream ClassifierOutput candidate list. The three
// ref_types per agentic/research:
//
//   - "name":            display-name lookup against candidates
//     (Phase 1 scope; entity-index lookup is a
//     Phase 2 extension if candidates don't have
//     a hit on the name).
//   - "partial_id":      dot-bounded suffix match on candidate IDs.
//   - "candidate_index": integer index into candidates list.
//
// Unresolved seeds are SKIPPED rather than erroring — a single bad
// reference shouldn't gate the whole execution; the handler logs
// the drop and degraded-flags the ExecutionOutput when ANY seed
// was dropped.
func resolveSeedRefs(seeds []research.SeedRef, candidates []research.Candidate, logger *slog.Logger) (resolved []string, dropped int) {
	if logger == nil {
		logger = slog.Default()
	}
	for i, seed := range seeds {
		id, err := resolveOneSeedRef(seed, candidates)
		if err != nil {
			logger.Warn("seed reference did not resolve; dropping",
				slog.Int("index", i),
				slog.String("ref", seed.Ref),
				slog.String("ref_type", seed.RefType),
				slog.Any("error", err))
			dropped++
			continue
		}
		resolved = append(resolved, id)
	}
	return resolved, dropped
}

// resolveOneSeedRef dispatches a single SeedRef per its RefType.
// Separated from resolveSeedRefs so unit tests can drive each
// branch without log assertions.
func resolveOneSeedRef(seed research.SeedRef, candidates []research.Candidate) (string, error) {
	switch seed.RefType {
	case research.SeedRefTypeCandidateIndex:
		return resolveSeedRefByCandidateIndex(seed.Ref, candidates)
	case research.SeedRefTypePartialID:
		return resolveSeedRefByPartialID(seed.Ref, candidates)
	case research.SeedRefTypeName:
		return resolveSeedRefByName(seed.Ref, candidates)
	}
	return "", fmt.Errorf("ref_type %q is not a canonical seed reference type", seed.RefType)
}

// resolveSeedRefByName does case-insensitive label match against
// classifier candidates. Phase 1 scope is candidate-anchored
// (Phase 2 may extend to entity-index lookup when the classifier
// missed a known entity).
func resolveSeedRefByName(ref string, candidates []research.Candidate) (string, error) {
	needle := strings.ToLower(strings.TrimSpace(ref))
	if needle == "" {
		return "", fmt.Errorf("name ref is empty")
	}
	for _, c := range candidates {
		label := strings.ToLower(strings.TrimSpace(c.Label))
		if label != "" && label == needle {
			id := strings.TrimSpace(c.EntityID)
			if id == "" {
				return "", fmt.Errorf("candidate with label %q has empty entity_id", needle)
			}
			return id, nil
		}
	}
	return "", fmt.Errorf("name ref %q matched no classifier candidate label", needle)
}

// executeAll runs the materialized sub-query set in parallel,
// dedups by entity_id, applies per-tier ordering + recency
// tie-break, enforces the intent's token budget, and returns the
// finalised ExecutionOutput. Pure function over GraphQueryClient
// (production wraps NATS, tests inject an in-memory fake) — no
// NATS plumbing leaks here so the test matrix can exercise the
// orchestration logic with deterministic inputs.
//
// Degraded flagging: ANY of (sub-query error, sub-query timeout,
// dropped seed reference, materialization yielded zero queries)
// flips Degraded=true with a per-cause DegradedReason. The chain
// doesn't error on degraded — assess_sufficiency surfaces it to
// the LLM as low-confidence input.
func executeAll(
	ctx context.Context,
	gq GraphQueryClient,
	queries []SubQuery,
	intent *research.Intent,
	decisionAction string,
	maxParallelism int,
	maxResultsPerSubquery int,
	logger *slog.Logger,
) (*research.ExecutionOutput, error) {
	if intent == nil {
		return nil, fmt.Errorf("intent is nil")
	}
	if gq == nil {
		return nil, fmt.Errorf("graph-query client is nil")
	}
	if logger == nil {
		logger = slog.Default()
	}
	if maxParallelism <= 0 {
		maxParallelism = DefaultMaxParallelism
	}
	if maxResultsPerSubquery <= 0 {
		maxResultsPerSubquery = DefaultMaxResultsPerSubquery
	}

	output := &research.ExecutionOutput{
		Topic:         intent.Topic,
		Action:        decisionAction,
		SubQueryCount: len(queries),
	}

	if len(queries) == 0 {
		output.Degraded = true
		output.DegradedReason = "materializer produced zero sub-queries"
		return output, nil
	}

	// Per-sub-query timeout = ExecuteTimeout * DefaultPerSubqueryTimeoutFraction.
	// Derived from the parent ctx's remaining deadline so the cap is
	// the MIN of operator config + caller's remaining budget.
	perQueryTimeout := derivePerQueryTimeout(ctx)

	var (
		mu          sync.Mutex
		allEvidence []research.Evidence
		degraded    bool
		reasons     []string
	)

	// Bounded-concurrency errgroup. SetLimit clamps how many
	// sub-queries dispatch in parallel; remaining queries queue.
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(maxParallelism)

	for i := range queries {
		q := queries[i]
		g.Go(func() error {
			qctx := gctx
			if perQueryTimeout > 0 {
				var cancel context.CancelFunc
				qctx, cancel = context.WithTimeout(gctx, perQueryTimeout)
				defer cancel()
			}
			evidence, err := executeSubQuery(qctx, gq, q, maxResultsPerSubquery)
			if err != nil {
				logger.Warn("sub-query failed; degrading",
					slog.String("type", string(q.Type)),
					slog.String("source", q.Source),
					slog.Any("error", err))
				mu.Lock()
				degraded = true
				reasons = append(reasons, fmt.Sprintf("%s:%s failed: %v", q.Type, q.Source, err))
				mu.Unlock()
				// Per-sub-query failure is degrading, NOT chain-failing.
				// Return nil so errgroup doesn't cancel sibling
				// sub-queries that may still succeed.
				return nil
			}
			mu.Lock()
			allEvidence = append(allEvidence, evidence...)
			mu.Unlock()
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		// errgroup returns the first non-nil error from a Go-routine.
		// We return nil from per-sub-query failures, so this only
		// fires on a hard cancellation (parent ctx done).
		return nil, fmt.Errorf("fan-out: %w", err)
	}

	// Dedup by EntityID (cross-tier collapses), preserve highest-tier
	// + highest-score Evidence for the duplicate set.
	deduped := dedupEvidence(allEvidence)

	// Per-tier ordering + recency tie-break.
	sortEvidence(deduped)

	// Budget enforcement. Drop lowest-scoring until under budget.
	budget := intent.ResolvedBudgetTokens()
	if budget <= 0 {
		budget = DefaultBudgetTokens
	}
	enforced, usedTokens := enforceBudget(deduped, budget)

	output.Evidence = enforced
	output.BudgetTokensUsed = usedTokens
	if degraded {
		output.Degraded = true
		output.DegradedReason = strings.Join(reasons, "; ")
	}

	return output, nil
}

// executeSubQuery dispatches one sub-query to the right GraphQueryClient
// method. Returns Evidence with Tier + Source already stamped by the
// implementing executor (interface contract).
func executeSubQuery(ctx context.Context, gq GraphQueryClient, q SubQuery, limit int) ([]research.Evidence, error) {
	switch q.Type {
	case SubQueryTypeEntityState:
		return gq.EntityState(ctx, *q.EntityState, q.Tier, q.Source, limit)
	case SubQueryTypePredicateWalk:
		return gq.PredicateWalk(ctx, *q.PredicateWalk, q.Tier, q.Source, limit)
	case SubQueryTypeTemporalRange:
		return gq.TemporalRange(ctx, *q.TemporalRange, q.Tier, q.Source, limit)
	case SubQueryTypeBM25:
		return gq.BM25(ctx, *q.BM25, q.Tier, q.Source, limit)
	}
	return nil, fmt.Errorf("unknown sub-query type %q", q.Type)
}

// dedupEvidence collapses duplicates by EntityID. When the same
// entity surfaces in multiple sub-queries, the dedup picks the
// "best" Evidence: lower Tier wins (Tier 0 > Tier 1), and within
// the same tier higher Score wins.
//
// Provenance is preserved by stamping the WINNING evidence's
// Source. Phase 2 may extend Evidence with a multi-source list so
// the agent can quote any path that surfaced the entity; Phase 1
// ships single-source-per-entity.
func dedupEvidence(in []research.Evidence) []research.Evidence {
	if len(in) == 0 {
		return nil
	}
	best := make(map[string]research.Evidence, len(in))
	for _, e := range in {
		if e.EntityID == "" {
			continue
		}
		existing, ok := best[e.EntityID]
		if !ok {
			best[e.EntityID] = e
			continue
		}
		if evidenceBeats(e, existing) {
			best[e.EntityID] = e
		}
	}
	out := make([]research.Evidence, 0, len(best))
	for _, e := range best {
		out = append(out, e)
	}
	return out
}

// evidenceBeats returns true when a should replace b in dedup.
// Tier 0 beats Tier 1 unconditionally; within the same tier, higher
// Score wins; ties broken by EntityID ascending (deterministic
// across map iteration).
func evidenceBeats(a, b research.Evidence) bool {
	if a.Tier != b.Tier {
		return a.Tier < b.Tier // "0" < "1" lexically — Tier 0 wins
	}
	if a.Score != b.Score {
		return a.Score > b.Score
	}
	return a.EntityID < b.EntityID
}

// sortEvidence applies Phase 1's ordering rule: lower-tier first
// (Tier 0 before Tier 1), then higher Score, then EntityID
// ascending for tie-break. Stable sort preserves input order
// within ties for replay determinism.
func sortEvidence(ev []research.Evidence) {
	sort.SliceStable(ev, func(i, j int) bool {
		if ev[i].Tier != ev[j].Tier {
			return ev[i].Tier < ev[j].Tier
		}
		if ev[i].Score != ev[j].Score {
			return ev[i].Score > ev[j].Score
		}
		return ev[i].EntityID < ev[j].EntityID
	})
}

// enforceBudget drops lowest-ranked evidence (from the tail of the
// sorted slice) until the estimated token cost fits within budget.
// Returns the kept slice and the estimated token cost actually
// retained.
//
// Token estimation: ~4 chars/token (industry rule of thumb) across
// the rendered evidence — EntityID + Label + Type + SnippetText +
// Source. Crude but stable; Phase 2 may swap a tokenizer.
func enforceBudget(in []research.Evidence, budget int) ([]research.Evidence, int) {
	if len(in) == 0 || budget <= 0 {
		return in, 0
	}
	out := make([]research.Evidence, 0, len(in))
	used := 0
	for _, e := range in {
		cost := estimateEvidenceTokens(e)
		if used+cost > budget {
			// Drop this and everything after (in sorted order, they're
			// lower-ranked). Phase 1 strategy: sharp cutoff. Phase 2
			// may try greedier per-item drop.
			break
		}
		out = append(out, e)
		used += cost
	}
	return out, used
}

// estimateEvidenceTokens approximates the prompt-cost of one
// Evidence entry. Sums character counts of the rendered fields
// (matches the rough shape a downstream synthesizer would inline
// in its prompt) and divides by 4. Always returns at least 1 to
// prevent empty Evidence from being "free" (still costs a JSON
// boundary in the array).
func estimateEvidenceTokens(e research.Evidence) int {
	chars := len(e.EntityID) + len(e.SnippetText) + len(e.Source) + len(e.ObjectStoreRef)
	tokens := chars / 4
	if tokens < 1 {
		tokens = 1
	}
	return tokens
}

// derivePerQueryTimeout returns the per-sub-query deadline as a
// fraction of the parent ctx's remaining time. When the parent
// has no deadline (test ctx.Background, or operator-disabled
// component timeout), falls back to a fraction of
// DefaultExecuteTimeout so a wedged sub-query can't hang the
// fan-out indefinitely.
func derivePerQueryTimeout(ctx context.Context) time.Duration {
	dl, ok := ctx.Deadline()
	if !ok {
		return time.Duration(float64(DefaultExecuteTimeout) * DefaultPerSubqueryTimeoutFraction)
	}
	remaining := time.Until(dl)
	if remaining <= 0 {
		return 0
	}
	return time.Duration(float64(remaining) * DefaultPerSubqueryTimeoutFraction)
}
