package research

import (
	"encoding/json"
	"fmt"

	"github.com/c360studio/semstreams/message"
)

// ExecutionOutput is the execute_subqueries component's emit:
// evidence array + provenance metadata. R3 fires on the matching
// trigger key; assess_sufficiency (PR 5) consumes the payload to
// pick sufficient / refine.
//
// Action carries the routing action that produced this execution
// (synthesize_directly / retighten / walk_seeds / decompose). The
// rule chain in PR 6 doesn't pattern-match on it — that's R2's
// job — but downstream operator observability (trajectory viewers,
// ops dashboards) wants to know which routing branch the evidence
// came from.
type ExecutionOutput struct {
	// Topic echoes the originating Intent topic so downstream
	// consumers don't have to re-fetch the intent payload for
	// logging / display.
	Topic string `json:"topic"`

	// Action is the routing action that drove this execution. One
	// of the research.ActionXxx constants. Required.
	Action string `json:"action"`

	// Evidence is the dedup'd + ranked + budget-enforced evidence
	// set the fan-out produced. May be empty (the routing decision
	// may have produced no hits — assess_sufficiency surfaces this
	// to the LLM as "you have nothing to synthesize from"; the
	// chain doesn't error on empty).
	Evidence []Evidence `json:"evidence,omitempty"`

	// SubQueryCount is the number of sub-queries the materializer
	// produced for this execution. Operator observability: tracks
	// how aggressively decompose templates fanned out. Surfaces
	// drift in scope-cap behaviour (narrow shouldn't produce many).
	SubQueryCount int `json:"subquery_count,omitempty"`

	// Degraded flags incomplete fan-out — e.g. spatial axis in
	// decompose args (Phase 2 wire), a sub-query timeout, or a
	// retrieval-tier error. Sets DegradedReason to the per-cause
	// explanation. assess_sufficiency may downgrade confidence
	// when degraded.
	Degraded       bool   `json:"degraded,omitempty"`
	DegradedReason string `json:"degraded_reason,omitempty"`

	// BudgetTokensUsed reports the estimated token cost of the
	// emitted evidence array after budget enforcement. Operator-
	// facing observability for tuning Intent.BudgetTokens defaults.
	BudgetTokensUsed int `json:"budget_tokens_used,omitempty"`
}

// Schema implements message.Payload.
func (p *ExecutionOutput) Schema() message.Type {
	return message.Type{
		Domain:   Domain,
		Category: CategoryExecutionOutput,
		Version:  SchemaVersion,
	}
}

// Validate implements message.Payload. Enforces required fields
// (Topic always; Action when not Degraded) and per-Evidence shape
// via Evidence.Validate. Empty Evidence is valid (degraded path).
//
// Action contract: required + must be a canonical RouteAction
// EXCEPT on the Degraded path where execute_subqueries didn't have
// a RouteDecision to consult (e.g. upstream classify.complete or
// route.complete never landed). Those envelopes ship with empty
// Action + Degraded=true + DegradedReason explaining the gap so
// assess_sufficiency (PR 5) can route to low-confidence handling
// without R3 stranding the loop on a hard chain-abort.
func (p *ExecutionOutput) Validate() error {
	if p == nil {
		return fmt.Errorf("execution output is nil")
	}
	if p.Topic == "" {
		return fmt.Errorf("topic required")
	}
	if p.Action != "" && !IsValidRouteAction(p.Action) {
		return fmt.Errorf("action %q is not a canonical router action", p.Action)
	}
	if p.Action == "" && !p.Degraded {
		return fmt.Errorf("action required when not Degraded")
	}
	for i, e := range p.Evidence {
		if err := e.Validate(); err != nil {
			return fmt.Errorf("evidence[%d]: %w", i, err)
		}
	}
	if p.SubQueryCount < 0 {
		return fmt.Errorf("subquery_count must be >= 0")
	}
	if p.BudgetTokensUsed < 0 {
		return fmt.Errorf("budget_tokens_used must be >= 0")
	}
	return nil
}

// MarshalJSON implements json.Marshaler with the alias-recursion guard.
func (p *ExecutionOutput) MarshalJSON() ([]byte, error) {
	type alias ExecutionOutput
	return json.Marshal((*alias)(p))
}

// UnmarshalJSON implements json.Unmarshaler with the alias-recursion
// guard. Partial payloads (e.g. {}) round-trip clean; strict
// validation lives in Validate, not at decode time, so test +
// partial-update flows can decode incrementally without tripping
// shape checks the producer hasn't completed yet.
func (p *ExecutionOutput) UnmarshalJSON(data []byte) error {
	type alias ExecutionOutput
	return json.Unmarshal(data, (*alias)(p))
}
