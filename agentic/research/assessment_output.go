package research

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/c360studio/semstreams/message"
)

// AssessmentOutput is the assess_sufficiency component's emit
// (ADR-045 Phase 1 PR 5): the sufficient/refine decision over the
// upstream ExecutionOutput's evidence array. Phase 1's R3 rule
// dispatches synthesize_answer when Sufficient is true and the
// refine loop on R4 when Sufficient is false.
//
// Per feedback_llm_authored_predicates_rule_opaque, when this payload
// is stamped onto loop entity triples the Rationale field's
// triple-predicate is published with WithRuleOpaque(true). Rules
// branch on the typed Sufficient boolean only.
type AssessmentOutput struct {
	// Topic echoes the originating Intent topic so downstream
	// consumers and trajectory viewers don't have to re-fetch the
	// intent payload for logging / display. Required.
	Topic string `json:"topic"`

	// Sufficient is the load-bearing decision. True means the
	// evidence array is enough to synthesize a useful answer; R3 routes
	// to synthesize. False means the chain should refine; R4 routes
	// the RefinedQueries through another execute_subqueries pass.
	Sufficient bool `json:"sufficient"`

	// RefinedQueries is the refine path's payload — short natural-
	// language sub-query strings the assessor believes would close the
	// gap. Empty when Sufficient is true. The Phase 1 refine path
	// concatenates these into a single hint set and re-runs nl_classify;
	// Phase 2 may dispatch them as parallel typed sub-queries.
	//
	// The shape is deliberately stringy (not typed sub-queries) per
	// feedback_tool_signature_intent_not_structure: the assessor model
	// expresses gap intent, the backend constructs typed dispatches.
	RefinedQueries []string `json:"refined_queries,omitempty"`

	// Rationale is the assessor's natural-language justification for
	// the decision. Captured for operator trajectory review; rule-
	// opaque per discipline memory.
	Rationale string `json:"rationale,omitempty"`

	// Confidence is the assessor's confidence in the sufficiency
	// decision (0..1). Optional — the assess prompt asks for it as
	// observability; rules do not branch on it in Phase 1. Operator
	// trajectory review uses it to spot prompts that flip flags
	// with low conviction.
	Confidence float64 `json:"confidence,omitempty"`

	// EvidenceCount echoes len(ExecutionOutput.Evidence) the assessor
	// saw. Observability — lets the operator confirm the assessor
	// actually had evidence to read when it claimed Sufficient (a
	// hallucinated Sufficient=true on EvidenceCount=0 is a prompt
	// bug worth catching).
	EvidenceCount int `json:"evidence_count,omitempty"`

	// Degraded flags an assessment that couldn't run cleanly — e.g.
	// the LLM call failed and the component fell back to a
	// "Sufficient=false, refine with original intent" safety route,
	// or upstream ExecutionOutput was missing. assess_sufficiency
	// surfaces this so R3 / R4 can downgrade their next-stage
	// confidence without stranding the chain on a hard abort.
	Degraded       bool   `json:"degraded,omitempty"`
	DegradedReason string `json:"degraded_reason,omitempty"`
}

// Schema implements message.Payload.
func (p *AssessmentOutput) Schema() message.Type {
	return message.Type{
		Domain:   Domain,
		Category: CategoryAssessmentOutput,
		Version:  SchemaVersion,
	}
}

// Validate implements message.Payload. Required: Topic. RefinedQueries
// items are checked for non-empty strings when present (an empty
// query string is an assessor-prompt bug). When Sufficient is true
// RefinedQueries SHOULD be empty — the assessor shouldn't refine
// what it just declared sufficient — but the check is a Warn at the
// consuming component, not a Validate-time reject, so the chain
// stays moving on imperfect assessor emits.
func (p *AssessmentOutput) Validate() error {
	if p == nil {
		return fmt.Errorf("assessment output is nil")
	}
	if strings.TrimSpace(p.Topic) == "" {
		return fmt.Errorf("topic required")
	}
	for i, q := range p.RefinedQueries {
		if strings.TrimSpace(q) == "" {
			return fmt.Errorf("refined_queries[%d] is empty", i)
		}
	}
	if p.Confidence < 0 || p.Confidence > 1 {
		return fmt.Errorf("confidence must be in [0,1], got %v", p.Confidence)
	}
	if p.EvidenceCount < 0 {
		return fmt.Errorf("evidence_count must be >= 0, got %d", p.EvidenceCount)
	}
	return nil
}

// MarshalJSON implements json.Marshaler with the alias-recursion guard.
func (p *AssessmentOutput) MarshalJSON() ([]byte, error) {
	type alias AssessmentOutput
	return json.Marshal((*alias)(p))
}

// UnmarshalJSON implements json.Unmarshaler with the alias-recursion
// guard.
func (p *AssessmentOutput) UnmarshalJSON(data []byte) error {
	type alias AssessmentOutput
	return json.Unmarshal(data, (*alias)(p))
}
