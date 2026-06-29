package fusion

import (
	"fmt"
	"strings"
)

// Evidence is one item in a fused evidence set — a single entity hit
// or evidence snippet the retrieval fan-out surfaced. Provenance is
// mandatory: every evidence item carries enough metadata (EntityID +
// Tier + Source) for the caller to verify it back against the graph
// and for downstream consumers (e.g. synthesize_answer's quote-back
// validation) to reject fabricated refs.
//
// ObjectStoreRef is set when the body of the evidence (long text,
// document, etc.) lives in ObjectStore; consumers read it via that
// ref. SnippetText carries a short inline preview for prompt-injection
// readability without triggering ObjectStore round-trips on every hit.
type Evidence struct {
	// EntityID is the graph entity this evidence refers to. Required.
	EntityID string `json:"entity_id"`

	// Tier is the retrieval tier that surfaced this hit: "0" (rules /
	// predicate queries), "1" (BM25), or "2" (neural — deferred to
	// Phase 2). Operators may use it to filter evidence by retrieval
	// method.
	Tier string `json:"tier"`

	// Source is the retrieval source within the tier — e.g.
	// "classifier", "predicate_walk", "bm25_index". Required.
	Source string `json:"source"`

	// Score is the within-tier ranking score (higher = better).
	// Cross-tier comparison is not meaningful in Phase 1 (per-tier
	// ordering + recency tie-break); Phase 2 may add a learned ranker.
	Score float64 `json:"score,omitempty"`

	// SnippetText is a short inline preview (prompt-injection
	// friendly). Omit when the evidence has no readable preview.
	SnippetText string `json:"snippet_text,omitempty"`

	// ObjectStoreRef is the ObjectStore key for the full body when
	// the evidence has bulk content. Empty when the evidence is fully
	// expressed in the EntityID + triples on the graph.
	ObjectStoreRef string `json:"objectstore_ref,omitempty"`
}

// Validate checks the required fields and rejects unknown tier values.
func (e *Evidence) Validate() error {
	if strings.TrimSpace(e.EntityID) == "" {
		return fmt.Errorf("entity_id required")
	}
	if strings.TrimSpace(e.Source) == "" {
		return fmt.Errorf("source required")
	}
	switch e.Tier {
	case "0", "1", "2":
		// Accepted set. "2" is reserved for Phase 2 but accepted at
		// the schema level so Phase 1 fixtures can probe forward-compat.
	default:
		return fmt.Errorf("tier %q must be \"0\", \"1\", or \"2\"", e.Tier)
	}
	return nil
}
