package researchexecute

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/c360studio/semstreams/agentic/research"
	"github.com/c360studio/semstreams/pkg/fusion"
)

// GraphQueryClient is the narrow surface this component consumes
// from graph-query / graph-index. Production wraps NATS-direct
// subjects (graph.query.entitiesByPrefix, graph.query.searchGraph,
// etc.); tests substitute an in-memory fake so the matrix doesn't
// need a live graph stack.
//
// This is a type alias for fusion.GraphQueryClient — the interface
// is defined in pkg/fusion; this alias keeps the component's imports
// tidy without an extra indirection for callers already working
// inside this package.
type GraphQueryClient = fusion.GraphQueryClient

// LoopStore is the AGENT_LOOPS read/write surface this component
// consumes. Production wraps natsclient.KVStore; tests substitute
// an in-memory map.
type LoopStore interface {
	// GetIntent loads the research_intent payload from the
	// research.request.received.<loopID> key.
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
	queries []fusion.SubQuery,
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

	budget := intent.ResolvedBudgetTokens()
	if budget <= 0 {
		budget = fusion.DefaultBudgetTokens
	}

	opts := fusion.FuseOptions{
		MaxParallelism:        maxParallelism,
		MaxResultsPerSubquery: maxResultsPerSubquery,
		BudgetTokens:          budget,
	}

	result, err := fusion.Fuse(ctx, gq, queries, opts, logger)
	if err != nil {
		return nil, err
	}

	output := &research.ExecutionOutput{
		Topic:            intent.Topic,
		Action:           decisionAction,
		SubQueryCount:    len(queries),
		Evidence:         result.Evidence,
		BudgetTokensUsed: result.BudgetTokensUsed,
	}
	if result.Degraded {
		output.Degraded = true
		output.DegradedReason = result.DegradedReason
	}
	return output, nil
}
