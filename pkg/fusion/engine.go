package fusion

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
)

// Default engine constants. Callers that pass zero-value FuseOptions
// fields fall through to these.
const (
	// DefaultMaxParallelism caps concurrent sub-query execution.
	// Keeps fan-out from saturating the responding gateways under
	// a wide-decompose scenario; operators can raise for staging
	// environments with more capacity.
	DefaultMaxParallelism = 8

	// DefaultMaxResultsPerSubquery caps per-sub-query result count
	// before ranking + budget enforcement run. Prevents a single
	// runaway sub-query from dominating the evidence array.
	DefaultMaxResultsPerSubquery = 50

	// DefaultBudgetTokens is the fallback evidence-budget when the
	// caller doesn't supply one. Should never normally fire when
	// callers supply a domain default, but acts as a safety net.
	DefaultBudgetTokens = 4000

	// DefaultPerSubqueryTimeoutFraction is the fraction of the
	// overall context deadline each sub-query gets as its per-call
	// timeout. 0.5 leaves slack for fan-out overhead and ranking.
	DefaultPerSubqueryTimeoutFraction = 0.5

	// DefaultExecuteTimeout is the fallback per-query timeout when
	// the caller's context has no deadline. Used only to bound
	// per-sub-query execution when ctx has no deadline set.
	DefaultExecuteTimeout = 60 * time.Second
)

// FuseOptions controls the engine's concurrency, per-subquery result
// cap, and evidence budget. Zero values fall through to the
// Default* constants.
type FuseOptions struct {
	// MaxParallelism caps concurrent sub-query execution. 0 uses DefaultMaxParallelism.
	MaxParallelism int

	// MaxResultsPerSubquery caps result count per sub-query before
	// ranking + budget enforcement run. 0 uses DefaultMaxResultsPerSubquery.
	MaxResultsPerSubquery int

	// BudgetTokens is the estimated-token budget for the returned
	// evidence set. 0 uses DefaultBudgetTokens.
	BudgetTokens int
}

// FuseResult is the engine's output: dedup'd + ranked + budget-enforced
// evidence, plus observability fields.
type FuseResult struct {
	// Evidence is the kept evidence set after dedup, sort, and budget
	// enforcement. May be empty (degraded path or zero queries).
	Evidence []Evidence

	// Degraded flags incomplete fan-out — e.g. one or more sub-query
	// errors or a timeout. The caller should surface this to operators
	// or downstream LLM consumers as low-confidence input.
	Degraded bool

	// DegradedReason explains why Degraded is set. Multiple per-sub-query
	// failures are joined with "; ".
	DegradedReason string

	// BudgetTokensUsed is the estimated token cost of the kept evidence
	// array after budget enforcement. Useful for operator observability
	// and tuning budget parameters.
	BudgetTokensUsed int
}

// Fuse runs the materialized sub-query set in parallel, deduplicates
// by EntityID (across tiers), sorts by tier + score + entity-ID
// tie-break, enforces the token budget, and returns a FuseResult.
//
// gq must not be nil. An empty queries slice is not an error — it
// returns FuseResult{Degraded: true, DegradedReason: "..."}.
//
// Per-sub-query failures are degrading, NOT chain-failing: they set
// FuseResult.Degraded and append to DegradedReason without cancelling
// sibling sub-queries. The only hard-fail is a parent-context
// cancellation, which returns a non-nil error.
//
// The logger parameter is optional; nil uses slog.Default().
func Fuse(ctx context.Context, gq GraphQueryClient, queries []SubQuery, opts FuseOptions, logger *slog.Logger) (FuseResult, error) {
	if gq == nil {
		return FuseResult{}, fmt.Errorf("graph-query client is nil")
	}
	if logger == nil {
		logger = slog.Default()
	}

	maxParallelism := opts.MaxParallelism
	if maxParallelism <= 0 {
		maxParallelism = DefaultMaxParallelism
	}
	maxResultsPerSubquery := opts.MaxResultsPerSubquery
	if maxResultsPerSubquery <= 0 {
		maxResultsPerSubquery = DefaultMaxResultsPerSubquery
	}
	budget := opts.BudgetTokens
	if budget <= 0 {
		budget = DefaultBudgetTokens
	}

	if len(queries) == 0 {
		return FuseResult{
			Degraded:       true,
			DegradedReason: "materializer produced zero sub-queries",
		}, nil
	}

	// Per-sub-query timeout = ctx's remaining deadline * DefaultPerSubqueryTimeoutFraction.
	perQueryTimeout := derivePerQueryTimeout(ctx)

	var (
		mu          sync.Mutex
		allEvidence []Evidence
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
				// Return nil so errgroup doesn't cancel sibling sub-queries.
				return nil
			}
			mu.Lock()
			allEvidence = append(allEvidence, evidence...)
			mu.Unlock()
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		// errgroup returns the first non-nil error from a goroutine.
		// We return nil from per-sub-query failures, so this only fires
		// on a hard cancellation (parent ctx done).
		return FuseResult{}, fmt.Errorf("fan-out: %w", err)
	}

	// Dedup by EntityID (cross-tier collapses), preserve highest-tier
	// + highest-score Evidence for the duplicate set.
	deduped := dedupEvidence(allEvidence)

	// Per-tier ordering + recency tie-break.
	sortEvidence(deduped)

	// Budget enforcement. Drop lowest-scoring until under budget.
	enforced, usedTokens := enforceBudget(deduped, budget)

	result := FuseResult{
		Evidence:         enforced,
		BudgetTokensUsed: usedTokens,
	}
	if degraded {
		result.Degraded = true
		result.DegradedReason = strings.Join(reasons, "; ")
	}

	return result, nil
}

// executeSubQuery dispatches one sub-query to the right GraphQueryClient
// method. Returns Evidence with Tier + Source already stamped by the
// implementing executor (interface contract).
func executeSubQuery(ctx context.Context, gq GraphQueryClient, q SubQuery, limit int) ([]Evidence, error) {
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

// dedupEvidence collapses duplicates by EntityID. When the same entity
// surfaces in multiple sub-queries, the dedup picks the "best" Evidence:
// lower Tier wins (Tier 0 > Tier 1), and within the same tier higher
// Score wins.
//
// Provenance is preserved by stamping the WINNING evidence's Source.
// Phase 2 may extend Evidence with a multi-source list so the agent
// can quote any path that surfaced the entity; Phase 1 ships
// single-source-per-entity.
func dedupEvidence(in []Evidence) []Evidence {
	if len(in) == 0 {
		return nil
	}
	best := make(map[string]Evidence, len(in))
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
	out := make([]Evidence, 0, len(best))
	for _, e := range best {
		out = append(out, e)
	}
	return out
}

// evidenceBeats returns true when a should replace b in dedup.
// Tier 0 beats Tier 1 unconditionally; within the same tier, higher
// Score wins; ties broken by EntityID ascending (deterministic
// across map iteration).
func evidenceBeats(a, b Evidence) bool {
	if a.Tier != b.Tier {
		return a.Tier < b.Tier // "0" < "1" lexically — Tier 0 wins
	}
	if a.Score != b.Score {
		return a.Score > b.Score
	}
	return a.EntityID < b.EntityID
}

// sortEvidence applies Phase 1's ordering rule: lower-tier first
// (Tier 0 before Tier 1), then higher Score, then EntityID ascending
// for tie-break. Stable sort preserves input order within ties for
// replay determinism.
func sortEvidence(ev []Evidence) {
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
// Returns the kept slice and the estimated token cost actually retained.
//
// Token estimation: ~4 chars/token (industry rule of thumb) across the
// rendered evidence — EntityID + SnippetText + Source + ObjectStoreRef.
// Crude but stable; Phase 2 may swap a tokenizer.
func enforceBudget(in []Evidence, budget int) ([]Evidence, int) {
	if len(in) == 0 || budget <= 0 {
		return in, 0
	}
	out := make([]Evidence, 0, len(in))
	used := 0
	for _, e := range in {
		cost := estimateEvidenceTokens(e)
		if used+cost > budget {
			// Drop this and everything after (in sorted order, they're
			// lower-ranked). Phase 1 strategy: sharp cutoff.
			break
		}
		out = append(out, e)
		used += cost
	}
	return out, used
}

// estimateEvidenceTokens approximates the prompt-cost of one Evidence
// entry. Sums character counts of the rendered fields and divides by 4.
// Always returns at least 1 to prevent empty Evidence from being "free".
func estimateEvidenceTokens(e Evidence) int {
	chars := len(e.EntityID) + len(e.SnippetText) + len(e.Source) + len(e.ObjectStoreRef)
	tokens := chars / 4
	if tokens < 1 {
		tokens = 1
	}
	return tokens
}

// derivePerQueryTimeout returns the per-sub-query deadline as a
// fraction of the parent ctx's remaining time. When the parent has no
// deadline (test ctx.Background, or operator-disabled component
// timeout), falls back to a fraction of DefaultExecuteTimeout so a
// wedged sub-query can't hang the fan-out indefinitely.
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
