package research

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/c360studio/semstreams/message"
)

// RouteDecision is route_search's structured-emit output: one of four
// routing actions, action-specific args, and a free-form rationale for
// trajectory review. Phase 1's PR 3 specializes the LLM prompt around
// these four actions; ADR-045 commits to the closed enum at the
// schema level (Phase 2 may add actions, but Phase 1 ships strict).
//
// Per feedback_llm_authored_predicates_rule_opaque, when this payload
// is stamped onto loop entity triples, the Rationale field's
// triple-predicate is published with WithRuleOpaque(true). Rules
// should not pattern-match on rationale prose — they branch on the
// typed Action field only.
type RouteDecision struct {
	// Action is the routing decision. Must be one of ActionXxx
	// constants. UnmarshalJSON and Validate both enforce the enum.
	Action string `json:"action"`

	// Args carries the action-specific argument map. Schemas per
	// action (PR 3 finalizes the per-action shapes):
	//   - synthesize_directly: empty (uses classifier output)
	//   - retighten:           {"topic": string, "hints": map[string]string}
	//   - walk_seeds:          {"seed_entity_ids": []string}
	//   - decompose:           {"sub_queries": [{...typed...}]}
	// The map is intentionally loose at the wire level — strict
	// per-action shapes are enforced by the consuming component, not
	// at decode time, so Phase 1 ships without a separate per-action
	// payload type per action arg shape.
	Args map[string]any `json:"args,omitempty"`

	// Rationale is the LLM's natural-language justification for the
	// chosen action. Captured for operator trajectory review;
	// rule-opaque per discipline memory.
	Rationale string `json:"rationale,omitempty"`
}

// Schema implements message.Payload.
func (p *RouteDecision) Schema() message.Type {
	return message.Type{
		Domain:   Domain,
		Category: CategoryRouteDecision,
		Version:  SchemaVersion,
	}
}

// IsValidRouteAction reports whether s is one of the four canonical
// router actions. Exported so route_search's structured-emit
// validator can reuse the closed set.
func IsValidRouteAction(s string) bool {
	switch s {
	case ActionSynthesizeDirectly, ActionRetighten, ActionWalkSeeds, ActionDecompose:
		return true
	}
	return false
}

// Validate implements message.Payload. Enforces the closed action
// enum. Args content is not validated here — per-action arg shapes
// are checked by the consuming component (PR 3+).
func (p *RouteDecision) Validate() error {
	if p == nil {
		return fmt.Errorf("route decision is nil")
	}
	if strings.TrimSpace(p.Action) == "" {
		return fmt.Errorf("action required")
	}
	if !IsValidRouteAction(p.Action) {
		return fmt.Errorf("action %q is not a canonical router action (want one of %q, %q, %q, %q)",
			p.Action,
			ActionSynthesizeDirectly, ActionRetighten, ActionWalkSeeds, ActionDecompose)
	}
	return nil
}

// MarshalJSON implements json.Marshaler with the alias-recursion
// guard.
func (p *RouteDecision) MarshalJSON() ([]byte, error) {
	type alias RouteDecision
	return json.Marshal((*alias)(p))
}

// UnmarshalJSON implements json.Unmarshaler with strict action enum
// enforcement. Schema-violating values fail at decode rather than
// surfacing as nil-action downstream.
func (p *RouteDecision) UnmarshalJSON(data []byte) error {
	type alias RouteDecision
	if err := json.Unmarshal(data, (*alias)(p)); err != nil {
		return err
	}
	// Empty payloads (e.g. {}) round-trip clean; reject only when
	// Action is set to an unrecognised value. This keeps decode
	// usable for partial payloads in tests while still catching
	// invalid-action drift on real wire data.
	if p.Action != "" && !IsValidRouteAction(p.Action) {
		return fmt.Errorf("route decision: action %q is not a canonical router action", p.Action)
	}
	return nil
}
