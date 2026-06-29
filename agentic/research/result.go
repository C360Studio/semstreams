package research

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/pkg/fusion"
)

// DecompTrace records what decomposition the chain actually performed,
// for operator review of router quality. The shape is deliberately
// loose in Phase 1 — operator trajectories will dictate whether
// stricter typing is worth the churn (see plan doc PR 5
// open-questions).
type DecompTrace struct {
	// RouterAction is the value emitted by route_search at the chain's
	// first LLM judgment. One of the four ActionXxx constants.
	RouterAction string `json:"router_action,omitempty"`

	// SubQueries records the decomposed sub-queries the router
	// generated when RouterAction == ActionDecompose. Free-form per
	// Phase 1; typed sub-query schemas land with PR 3's router action
	// arg schemas.
	SubQueries []map[string]any `json:"sub_queries,omitempty"`

	// SeedEntities records the walk_seeds entity set when
	// RouterAction == ActionWalkSeeds.
	SeedEntities []string `json:"seed_entities,omitempty"`

	// RetightenRounds records how many times R2's retighten branch
	// fired before terminating (success, cap, or downstream branch).
	RetightenRounds int `json:"retighten_rounds,omitempty"`
}

// SearchResult is the chain's terminal output, returned to the parent
// loop via the continuation rule. Carries synthesis text plus the
// evidence refs synthesize_answer quoted back, plus the decomp trace
// for trajectory review.
type SearchResult struct {
	// Evidence is the set of hits that informed Synthesis. Every
	// evidence item synthesize_answer references must appear here;
	// quote-back validation enforces this in PR 5.
	Evidence []fusion.Evidence `json:"evidence,omitempty"`

	// Synthesis is the natural-language answer produced by
	// synthesize_answer. Refs quoted in the prose must resolve to
	// Evidence items above.
	Synthesis string `json:"synthesis"`

	// DecompTrace is the per-call audit of the chain's routing
	// choices and decompositions. Optional — present when the chain
	// took a non-trivial path (anything beyond synthesize_directly).
	DecompTrace *DecompTrace `json:"decomp_trace,omitempty"`

	// TokensUsed is the total LLM-token spend across the chain's
	// LLM calls (route_search + assess_sufficiency + synthesize_answer
	// + any retighten/refine repetitions).
	TokensUsed int `json:"tokens_used,omitempty"`

	// Iterations is the total refine-loop iterations the chain
	// executed before terminating. Distinct from RetightenRounds in
	// DecompTrace — Iterations counts R4's refine loop (capped by
	// ResearchIntent.MaxIterations); RetightenRounds counts R2's
	// retighten branch (capped at 2).
	Iterations int `json:"iterations,omitempty"`
}

// Schema implements message.Payload.
func (p *SearchResult) Schema() message.Type {
	return message.Type{
		Domain:   Domain,
		Category: CategoryResult,
		Version:  SchemaVersion,
	}
}

// Validate implements message.Payload. Synthesis is required (the
// chain's terminal emit always carries at least the synthesized
// answer; failure paths emit a degraded result with an error string
// rather than an empty Synthesis). Evidence items are individually
// validated when present.
func (p *SearchResult) Validate() error {
	if p == nil {
		return fmt.Errorf("search result is nil")
	}
	if strings.TrimSpace(p.Synthesis) == "" {
		return fmt.Errorf("synthesis required")
	}
	for i := range p.Evidence {
		if err := p.Evidence[i].Validate(); err != nil {
			return fmt.Errorf("evidence[%d]: %w", i, err)
		}
	}
	if p.TokensUsed < 0 {
		return fmt.Errorf("tokens_used must be >= 0, got %d", p.TokensUsed)
	}
	if p.Iterations < 0 {
		return fmt.Errorf("iterations must be >= 0, got %d", p.Iterations)
	}
	return nil
}

// MarshalJSON implements json.Marshaler with the alias-recursion
// guard.
func (p *SearchResult) MarshalJSON() ([]byte, error) {
	type alias SearchResult
	return json.Marshal((*alias)(p))
}

// UnmarshalJSON implements json.Unmarshaler.
func (p *SearchResult) UnmarshalJSON(data []byte) error {
	type alias SearchResult
	return json.Unmarshal(data, (*alias)(p))
}
