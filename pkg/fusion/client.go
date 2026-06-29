package fusion

import "context"

// GraphQueryClient is the narrow retrieval surface the fusion engine
// consumes. Production implementations wrap NATS-direct subjects;
// tests substitute in-memory fakes so the test matrix doesn't need a
// live graph stack.
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
// stays honest end-to-end. The engine does NOT re-stamp.
type GraphQueryClient interface {
	EntityState(ctx context.Context, args EntityStateArgs, tier, source string, limit int) ([]Evidence, error)
	PredicateWalk(ctx context.Context, args PredicateWalkArgs, tier, source string, limit int) ([]Evidence, error)
	TemporalRange(ctx context.Context, args TemporalRangeArgs, tier, source string, limit int) ([]Evidence, error)
	BM25(ctx context.Context, args BM25Args, tier, source string, limit int) ([]Evidence, error)
}
