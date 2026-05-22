package research

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/c360studio/semstreams/message"
)

// Candidate is one entity hit the classifier surfaced for the topic.
// Mirrors the search_graph EntityDigest shape (entity_id + label +
// type + relevance score + optional snippet) so PR 4
// (execute_subqueries) can consume nl_classify's output and search-
// graph's output through one evidence schema.
//
// Provenance is mandatory: every Candidate carries the Tier ("0"
// keyword / "1" embedding/BM25 / "2" neural — Phase 2) and Source
// (the retrieval method, e.g. "classifier_chain", "search_graph") so
// downstream rules can filter by retrieval pathway.
type Candidate struct {
	// EntityID is the matching graph entity. Required.
	EntityID string `json:"entity_id"`

	// Label is a human-readable name for the entity. Optional; empty
	// when the candidate source doesn't carry a label (some graph
	// queries return only IDs).
	Label string `json:"label,omitempty"`

	// Type is the entity's domain type when known (e.g. "drone",
	// "sensor"). Optional.
	Type string `json:"type,omitempty"`

	// Relevance is the within-tier ranking score (higher = better).
	// Per ADR-045 Phase 1, cross-tier comparison isn't meaningful —
	// PR 4 keeps per-tier ordering + recency tie-break; Phase 2 may
	// add a learned ranker.
	Relevance float64 `json:"relevance,omitempty"`

	// SnippetText is a short inline preview useful for prompt
	// injection. Omit when the candidate has no readable preview.
	SnippetText string `json:"snippet_text,omitempty"`

	// Tier is the retrieval tier that surfaced this hit. Same
	// taxonomy as Evidence.Tier in result.go so the evidence schema
	// stays consistent across PR 2 (this file) and PR 4.
	Tier string `json:"tier"`

	// Source is the retrieval source within the tier — e.g.
	// "classifier_chain", "search_graph". Required so PR 4 can
	// classify candidates by retrieval pathway.
	Source string `json:"source"`
}

// Validate checks the required fields and the closed Tier enum.
func (c *Candidate) Validate() error {
	if strings.TrimSpace(c.EntityID) == "" {
		return fmt.Errorf("entity_id required")
	}
	if strings.TrimSpace(c.Source) == "" {
		return fmt.Errorf("source required")
	}
	switch c.Tier {
	case "0", "1", "2":
		// Accepted set; mirrors Evidence.Tier.
	default:
		return fmt.Errorf("tier %q must be \"0\", \"1\", or \"2\"", c.Tier)
	}
	return nil
}

// ClassifierOutput is the nl_classify component's emit: the
// classifier hints (extracted from ClassificationResult.Options) plus
// the initial candidate set surfaced by executing the resulting
// SearchOptions against the graph. R1 fires on this payload (via the
// classify.complete.<loop_id> trigger key) and route_search (PR 3)
// consumes it to choose one of the four routing actions.
type ClassifierOutput struct {
	// Topic is the topic the classifier ran on. Echoed so the
	// downstream consumer (route_search) doesn't have to read the
	// intent payload separately.
	Topic string `json:"topic"`

	// Tier is which classifier tier produced the hint set:
	// "0" (keyword), "1" (embedding/BM25), or "3" (LLM). The
	// numbering follows ClassifierChain conventions (T0/T1/T2/T3 — T2
	// neural is Phase 2, not currently emitted). Empty/"0" is the
	// default fall-through when no tier matched.
	//
	// IMPORTANT: this is the *classifier* tier and shares no enum
	// with [Candidate.Tier] / [Evidence.Tier], which are *retrieval*
	// tiers ("0"/"1"/"2"). A LLM-tier classifier may surface
	// Tier="3" here while its surfaced candidates carry Tier="0" or
	// "1". Don't copy this string into a Candidate without
	// re-mapping — Candidate.Validate rejects "3".
	Tier string `json:"tier,omitempty"`

	// Intent is the LLM-or-embedding-detected intent classification.
	// Empty when the keyword tier matched (KeywordClassifier doesn't
	// set Intent — its semantics live in the Options map).
	Intent string `json:"intent,omitempty"`

	// Confidence is the classifier's confidence in the hint set.
	// 1.0 for keyword matches, <=1.0 from embedding similarity.
	Confidence float64 `json:"confidence,omitempty"`

	// Hints is the operator-facing flattened classifier output: time
	// range, geo bounds, path intent, aggregation type, etc. Wire
	// shape matches ClassificationResult.Options so consumers can
	// reconstruct a SearchOptions when they need to re-execute.
	Hints map[string]any `json:"hints,omitempty"`

	// Candidates is the initial candidate set: entities the
	// classifier surfaced, with scores + snippets where available.
	// Empty when the topic produced no hits — PR 3's route_search
	// will choose "retighten" or "decompose" in that case.
	Candidates []Candidate `json:"candidates,omitempty"`

	// Degraded is set when the candidate retrieval fell back to a
	// less-rich path (e.g. server-side semantic fallback). Carried
	// through so PR 3's router can treat the candidates as
	// advisory.
	Degraded bool `json:"degraded,omitempty"`

	// DegradedReason is a short operator-readable label describing
	// the fallback path when Degraded is true. Empty otherwise.
	DegradedReason string `json:"degraded_reason,omitempty"`
}

// Schema implements message.Payload.
func (p *ClassifierOutput) Schema() message.Type {
	return message.Type{
		Domain:   Domain,
		Category: CategoryClassifierOutput,
		Version:  SchemaVersion,
	}
}

// Validate implements message.Payload. Topic is required; Candidates
// are individually validated when present.
func (p *ClassifierOutput) Validate() error {
	if p == nil {
		return fmt.Errorf("classifier output is nil")
	}
	if strings.TrimSpace(p.Topic) == "" {
		return fmt.Errorf("topic required")
	}
	for i := range p.Candidates {
		if err := p.Candidates[i].Validate(); err != nil {
			return fmt.Errorf("candidates[%d]: %w", i, err)
		}
	}
	if p.Confidence < 0 {
		return fmt.Errorf("confidence must be >= 0, got %v", p.Confidence)
	}
	return nil
}

// MarshalJSON implements json.Marshaler with the alias-recursion
// guard.
func (p *ClassifierOutput) MarshalJSON() ([]byte, error) {
	type alias ClassifierOutput
	return json.Marshal((*alias)(p))
}

// UnmarshalJSON implements json.Unmarshaler.
func (p *ClassifierOutput) UnmarshalJSON(data []byte) error {
	type alias ClassifierOutput
	return json.Unmarshal(data, (*alias)(p))
}
