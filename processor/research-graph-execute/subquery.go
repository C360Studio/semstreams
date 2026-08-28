package researchexecute

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/c360studio/semstreams/agentic/research"
	"github.com/c360studio/semstreams/pkg/fusion"
)

// These aliases let the materialization code below construct sub-query values
// without a fusion. qualifier on every line. The cross-package break is complete
// (no research.SubQuery exists) — convenience aliases, not a compatibility layer.

// SubQuery aliases fusion.SubQuery.
type SubQuery = fusion.SubQuery

// SubQueryType aliases fusion.SubQueryType.
type SubQueryType = fusion.SubQueryType

// EntityStateArgs aliases fusion.EntityStateArgs.
type EntityStateArgs = fusion.EntityStateArgs

// PredicateWalkArgs aliases fusion.PredicateWalkArgs.
type PredicateWalkArgs = fusion.PredicateWalkArgs

// TemporalRangeArgs aliases fusion.TemporalRangeArgs.
type TemporalRangeArgs = fusion.TemporalRangeArgs

// BM25Args aliases fusion.BM25Args.
type BM25Args = fusion.BM25Args

// Re-export the SubQueryType constants from fusion so existing usages
// in this package and its tests compile unchanged.
const (
	SubQueryTypeEntityState   = fusion.SubQueryTypeEntityState
	SubQueryTypePredicateWalk = fusion.SubQueryTypePredicateWalk
	SubQueryTypeTemporalRange = fusion.SubQueryTypeTemporalRange
	SubQueryTypeBM25          = fusion.SubQueryTypeBM25
)

// materializeForWalkSeeds builds the sub-query set for a walk_seeds
// decision. Strategy: (a) entity_state for the resolved seeds so
// the caller sees the seeds themselves in the evidence; (b)
// predicate_walk from those seeds for neighborhood expansion; (c)
// a BM25 sub-query on the topic as Tier 1 coverage.
//
// resolvedSeeds must be the SeedRef-resolved full 6-part IDs from
// resolveSeedRefs (handler.go); empty means resolve produced no
// hits, in which case we ship only the BM25 fallback so the chain
// doesn't return zero evidence.
func materializeForWalkSeeds(intent *research.Intent, resolvedSeeds []string) []SubQuery {
	var queries []SubQuery

	if len(resolvedSeeds) > 0 {
		queries = append(queries, SubQuery{
			Type:        SubQueryTypeEntityState,
			Tier:        "0",
			Source:      "walk_seeds.entity_state",
			EntityState: &EntityStateArgs{EntityIDs: append([]string(nil), resolvedSeeds...)},
		})
		queries = append(queries, SubQuery{
			Type:   SubQueryTypePredicateWalk,
			Tier:   "0",
			Source: "walk_seeds.predicate_walk",
			PredicateWalk: &PredicateWalkArgs{
				Seeds:   append([]string(nil), resolvedSeeds...),
				MaxHops: 1,
			},
		})
	}

	queries = append(queries, SubQuery{
		Type:   SubQueryTypeBM25,
		Tier:   "1",
		Source: "walk_seeds.bm25_coverage",
		BM25:   &BM25Args{Query: intent.Topic},
	})

	return queries
}

// materializeForDecompose builds the sub-query set for a decompose
// decision per the MINIMAL TEMPLATE strategy (PR 4 scope per the
// implementation plan):
//
//   - "entity_type" axis → entity_state on classifier candidates
//     (when present) + BM25 on focus to surface entity-typed hits
//     beyond the classifier's initial set.
//   - "time" axis → temporal_range using a default ±24h window
//     around now. Operators can extend in Phase 2 by parsing the
//     focus string for explicit windows.
//   - "predicate" axis OR any unknown axis → predicate_walk with
//     no Seeds (executor falls through to a topic-anchored predicate
//     query) + BM25 on focus.
//
// spatial axis is deferred to Phase 2 (graph-index-spatial wire);
// surfaces a degraded-coverage log line if seen but ships the
// per-decomposition BM25 anyway so the chain returns evidence.
//
// scope (narrow/medium/broad) caps how many candidate-entity IDs
// flow into the entity_type template — narrow uses top 3, medium
// top 8, broad top 20.
//
// candidates is the upstream ClassifierOutput.Candidates list (may
// be empty if classifier returned nothing); used by the entity_type
// template to anchor.
func materializeForDecompose(intent *research.Intent, args research.DecomposeArgs, candidates []research.Candidate) []SubQuery {
	var queries []SubQuery

	scopeCap := decomposeCandidateCap(args.Scope)
	candidateIDs := topCandidateIDs(candidates, scopeCap)

	// Always include BM25 on the focus — widest-coverage Tier 1.
	bm25Query := args.Focus
	if strings.TrimSpace(bm25Query) == "" {
		bm25Query = intent.Topic
	}
	queries = append(queries, SubQuery{
		Type:   SubQueryTypeBM25,
		Tier:   "1",
		Source: "decompose.bm25_focus",
		BM25:   &BM25Args{Query: bm25Query},
	})

	for _, axis := range args.Axes {
		axisLower := strings.ToLower(strings.TrimSpace(axis))
		switch axisLower {
		case "entity_type", "entity":
			if len(candidateIDs) > 0 {
				queries = append(queries, SubQuery{
					Type:        SubQueryTypeEntityState,
					Tier:        "0",
					Source:      "decompose.entity_type",
					EntityState: &EntityStateArgs{EntityIDs: append([]string(nil), candidateIDs...)},
				})
			}
		case "time", "temporal":
			queries = append(queries, SubQuery{
				Type:   SubQueryTypeTemporalRange,
				Tier:   "0",
				Source: "decompose.time",
				TemporalRange: &TemporalRangeArgs{
					// Phase 1: default ±24h window. Phase 2 parses
					// explicit windows from focus.
					Start: defaultTemporalWindowStart(),
					End:   defaultTemporalWindowEnd(),
					Topic: bm25Query,
				},
			})
		case "predicate", "relationship":
			if len(candidateIDs) > 0 {
				queries = append(queries, SubQuery{
					Type:   SubQueryTypePredicateWalk,
					Tier:   "0",
					Source: "decompose.predicate",
					PredicateWalk: &PredicateWalkArgs{
						Seeds:   append([]string(nil), candidateIDs...),
						MaxHops: 1,
					},
				})
			}
		case "spatial":
			// Phase 2 wire — skip with no template materialized.
			// The component logs a degraded-coverage warning at
			// dispatch; BM25 above keeps the chain productive.
			continue
		default:
			// Fallback for free-form axes: a topic-anchored BM25 with
			// the axis as a hint suffix. Cheap, never errors, gives
			// the agent SOMETHING to work with.
			queries = append(queries, SubQuery{
				Type:   SubQueryTypeBM25,
				Tier:   "1",
				Source: "decompose.axis." + axisLower,
				BM25:   &BM25Args{Query: bm25Query + " " + axisLower},
			})
		}
	}

	return queries
}

// decomposeCandidateCap maps decompose scope → candidate-set cap
// for templates that anchor on classifier candidates. Empty scope
// resolves to medium per the IsValidDecomposeScope contract.
func decomposeCandidateCap(scope string) int {
	switch scope {
	case research.DecomposeScopeNarrow:
		return 3
	case research.DecomposeScopeBroad:
		return 20
	default: // "medium" or empty
		return 8
	}
}

// topCandidateIDs returns the first n candidate entity IDs from the
// classifier output. Stable iteration order: the upstream
// ClassifierOutput.Candidates is already produced in
// relevance-descending order by nl_classify, so taking the prefix
// is the right behaviour.
func topCandidateIDs(candidates []research.Candidate, n int) []string {
	if n <= 0 || len(candidates) == 0 {
		return nil
	}
	out := make([]string, 0, n)
	for _, c := range candidates {
		if n > 0 && len(out) >= n {
			break
		}
		if strings.TrimSpace(c.EntityID) == "" {
			continue
		}
		out = append(out, c.EntityID)
	}
	return out
}

// defaultTemporalWindowStart / End produce an RFC3339-encoded ±24h
// window around the wallclock. Package-level functions (not vars)
// so tests can stub via a build-tag-gated override if needed; Phase 1
// uses the real clock.
func defaultTemporalWindowStart() string {
	return nowRFC3339Offset(-24)
}

func defaultTemporalWindowEnd() string {
	return nowRFC3339Offset(+24)
}

// nowFunc is overridable for tests. Wallclock-driven defaults
// would otherwise make the materializer's output non-deterministic.
var nowFunc = nowDefault

// resolveSeedRefByCandidateIndex parses the ref as an int and
// indexes into classifier candidates. Exported as helper for
// tests; production callers go through resolveSeedRefs.
func resolveSeedRefByCandidateIndex(ref string, candidates []research.Candidate) (string, error) {
	idx, err := strconv.Atoi(strings.TrimSpace(ref))
	if err != nil {
		return "", fmt.Errorf("candidate_index ref %q is not an integer: %w", ref, err)
	}
	if idx < 0 || idx >= len(candidates) {
		return "", fmt.Errorf("candidate_index ref %d out of bounds (have %d candidates)", idx, len(candidates))
	}
	id := strings.TrimSpace(candidates[idx].EntityID)
	if id == "" {
		return "", fmt.Errorf("candidate at index %d has empty entity_id", idx)
	}
	return id, nil
}

// resolveSeedRefByPartialID does suffix-match against the classifier
// candidate IDs. The 6-part federated ID
// org.platform.system.domain.type.instance means a partial
// "robotics.gcs.drone" should match "acme.ops.robotics.gcs.drone.001".
// Returns the FIRST candidate that has the partial as a suffix;
// ties broken by relevance order (which candidates already supply).
func resolveSeedRefByPartialID(ref string, candidates []research.Candidate) (string, error) {
	needle := strings.TrimSpace(ref)
	if needle == "" {
		return "", fmt.Errorf("partial_id ref is empty")
	}
	for _, c := range candidates {
		id := strings.TrimSpace(c.EntityID)
		if id == "" {
			continue
		}
		// Match either exact equality or the partial as a dot-bounded
		// suffix to avoid accidental "drone" matching "octocopter-drone".
		if id == needle || strings.HasSuffix(id, "."+needle) {
			return id, nil
		}
	}
	return "", fmt.Errorf("partial_id ref %q matched no classifier candidate", needle)
}
