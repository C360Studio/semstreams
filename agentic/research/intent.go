package research

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/c360studio/semstreams/message"
)

// Intent is the caller's request: a free-form topic plus
// optional hints, plus per-call budgets. Per ADR-045's v2 amendment,
// the intent carries only what the caller actually knows — no entity
// IDs, no predicate names, no tier mix; those are outputs of the chain.
//
// Hints is intentionally a map[string]string in Phase 1: trajectory
// data from operator use will inform whether typed hint subtypes are
// worth adding in Phase 2 (see plan doc Risks for PR 1). Keys are
// classifier-parseable strings ("entity_kind", "domain", "recency",
// etc.); unknown keys are passed through and ignored downstream rather
// than rejected, so operators can experiment without schema churn.
type Intent struct {
	// Topic is the natural-language research question or subject.
	// Required.
	Topic string `json:"topic"`

	// Hints is an optional, free-form map of classifier hints. Phase 1
	// keeps the shape flexible; Phase 2 may add typed subtypes once
	// operator trajectories show what hints get passed.
	Hints map[string]string `json:"hints,omitempty"`

	// BudgetTokens caps total LLM token spend across the chain. Zero
	// falls back to DefaultBudgetTokens at consumption time so callers
	// can omit it.
	BudgetTokens int `json:"budget_tokens,omitempty"`

	// MaxIterations caps the refine loop in R4. Zero falls back to
	// DefaultMaxIterations.
	MaxIterations int `json:"max_iterations,omitempty"`
}

// Schema implements message.Payload.
func (p *Intent) Schema() message.Type {
	return message.Type{
		Domain:   Domain,
		Category: CategoryIntent,
		Version:  SchemaVersion,
	}
}

// Validate implements message.Payload. Required: Topic. Hints values
// are checked for non-empty strings when present (a hint key mapped to
// "" is a caller bug — empty signals "absent", which should be
// expressed by omitting the key).
func (p *Intent) Validate() error {
	if p == nil {
		return fmt.Errorf("research intent is nil")
	}
	if strings.TrimSpace(p.Topic) == "" {
		return fmt.Errorf("topic required")
	}
	for k, v := range p.Hints {
		if strings.TrimSpace(k) == "" {
			return fmt.Errorf("hints contains an empty key")
		}
		if strings.TrimSpace(v) == "" {
			return fmt.Errorf("hints[%q] is empty; omit the key instead", k)
		}
	}
	if p.BudgetTokens < 0 {
		return fmt.Errorf("budget_tokens must be >= 0, got %d", p.BudgetTokens)
	}
	if p.MaxIterations < 0 {
		return fmt.Errorf("max_iterations must be >= 0, got %d", p.MaxIterations)
	}
	return nil
}

// ResolvedBudgetTokens returns BudgetTokens or DefaultBudgetTokens
// when zero. Use this at consumption time so callers can omit the
// field and the chain still has a budget cap.
func (p *Intent) ResolvedBudgetTokens() int {
	if p.BudgetTokens == 0 {
		return DefaultBudgetTokens
	}
	return p.BudgetTokens
}

// ResolvedMaxIterations returns MaxIterations or DefaultMaxIterations
// when zero.
func (p *Intent) ResolvedMaxIterations() int {
	if p.MaxIterations == 0 {
		return DefaultMaxIterations
	}
	return p.MaxIterations
}

// MarshalJSON implements json.Marshaler. Uses the alias pattern to
// avoid recursion through the Payload interface (the registry decodes
// via this method).
func (p *Intent) MarshalJSON() ([]byte, error) {
	type alias Intent
	return json.Marshal((*alias)(p))
}

// UnmarshalJSON implements json.Unmarshaler. Mirrors MarshalJSON's
// alias pattern for the round-trip through the payload registry.
func (p *Intent) UnmarshalJSON(data []byte) error {
	type alias Intent
	return json.Unmarshal(data, (*alias)(p))
}
