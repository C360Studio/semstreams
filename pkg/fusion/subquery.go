package fusion

import (
	"fmt"
	"strings"
)

// SubQuery is a typed retrieval request. Each variant carries the
// minimum fields its tier executor needs; the Type discriminator
// drives dispatch in the engine's executeSubQuery.
//
// Tier and Source travel with the SubQuery so the per-tier executor
// can stamp them onto every Evidence it produces without re-deriving
// — keeps provenance honest end-to-end.
type SubQuery struct {
	Type   SubQueryType `json:"type"`
	Tier   string       `json:"tier"`   // "0" (predicate) or "1" (BM25)
	Source string       `json:"source"` // e.g. "walk_seeds:drone-001"; surfaces on Evidence.Source

	// Per-type args. Exactly one is populated based on Type — the
	// executor switch enforces. Optional fields allow tests + future
	// templates to omit per-type primitives that don't apply.
	EntityState   *EntityStateArgs   `json:"entity_state,omitempty"`
	PredicateWalk *PredicateWalkArgs `json:"predicate_walk,omitempty"`
	TemporalRange *TemporalRangeArgs `json:"temporal_range,omitempty"`
	BM25          *BM25Args          `json:"bm25,omitempty"`
}

// SubQueryType is the closed set of Tier 0+1 primitives. Phase 2
// adds spatial_polygon + neural; Phase 1 stays minimal.
type SubQueryType string

const (
	// SubQueryTypeEntityState fetches current state of named
	// entities. Tier 0; used for walk_seeds anchoring + decompose's
	// entity_type axis when classifier candidates are present.
	SubQueryTypeEntityState SubQueryType = "entity_state"

	// SubQueryTypePredicateWalk traverses predicate(s) from seed
	// entities. Tier 0; used for walk_seeds neighborhood expansion +
	// decompose's free-form axis fallback.
	SubQueryTypePredicateWalk SubQueryType = "predicate_walk"

	// SubQueryTypeTemporalRange queries entities within a time
	// window. Tier 0; used for decompose's time axis. Phase 1 ships
	// the type but the executor falls through to BM25 on topic when
	// graph-index-temporal isn't wired — operators see the
	// "temporal degrade" hint on the produced Evidence.
	SubQueryTypeTemporalRange SubQueryType = "temporal_range"

	// SubQueryTypeBM25 text-searches via graph-query's existing
	// BM25 surface. Tier 1; always added to widen coverage beyond
	// purely-structural retrieval (intent-shaped routing can miss
	// keyword matches the classifier surfaced).
	SubQueryTypeBM25 SubQueryType = "bm25"
)

// EntityStateArgs carries IDs to fetch.
type EntityStateArgs struct {
	EntityIDs []string `json:"entity_ids"`
}

// PredicateWalkArgs carries seed IDs + optional predicate filter.
// MaxHops 0 resolves to 1 at the executor.
type PredicateWalkArgs struct {
	Seeds      []string `json:"seeds"`
	Predicates []string `json:"predicates,omitempty"`
	MaxHops    int      `json:"max_hops,omitempty"`
}

// TemporalRangeArgs carries start/end + an optional topic filter.
// Empty Topic widens to all entities in the window (may be heavy;
// callers should always pass a topic in practice).
type TemporalRangeArgs struct {
	// Start / End are RFC3339 strings; timezone handling stays in the
	// executor where the upstream graph-index-temporal surface lives.
	Start string `json:"start"`
	End   string `json:"end"`
	Topic string `json:"topic,omitempty"`
}

// BM25Args carries the text query + result cap.
type BM25Args struct {
	Query string `json:"query"`
	Limit int    `json:"limit,omitempty"`
}

// Validate returns a non-nil error when required per-type fields are
// missing. Called by the materializer before fan-out so a malformed
// sub-query surfaces before any retrieval work runs.
func (q SubQuery) Validate() error {
	if q.Tier != "0" && q.Tier != "1" {
		return fmt.Errorf("subquery tier %q is not canonical (want \"0\" or \"1\")", q.Tier)
	}
	if strings.TrimSpace(q.Source) == "" {
		return fmt.Errorf("subquery source required for provenance")
	}
	switch q.Type {
	case SubQueryTypeEntityState:
		if q.EntityState == nil || len(q.EntityState.EntityIDs) == 0 {
			return fmt.Errorf("entity_state subquery: entity_ids required")
		}
	case SubQueryTypePredicateWalk:
		if q.PredicateWalk == nil || len(q.PredicateWalk.Seeds) == 0 {
			return fmt.Errorf("predicate_walk subquery: seeds required")
		}
	case SubQueryTypeTemporalRange:
		if q.TemporalRange == nil || q.TemporalRange.Start == "" || q.TemporalRange.End == "" {
			return fmt.Errorf("temporal_range subquery: start + end required")
		}
	case SubQueryTypeBM25:
		if q.BM25 == nil || strings.TrimSpace(q.BM25.Query) == "" {
			return fmt.Errorf("bm25 subquery: query required")
		}
	default:
		return fmt.Errorf("subquery type %q is not a Phase 1 primitive", q.Type)
	}
	return nil
}
