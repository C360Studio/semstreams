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

	// Args carries the action-specific argument map. Per-action shapes
	// (intent-not-structure per ADR-045 Amendment 2026-05-23 — model
	// emits intent, backend constructs typed structures):
	//   - synthesize_directly: empty (uses classifier output as-is)
	//   - retighten:           RetightenArgs   {topic, hints}
	//   - walk_seeds:          WalkSeedsArgs   {seeds: [{ref, ref_type}]}
	//                          ref_type ∈ {"name","partial_id","candidate_index"};
	//                          backend resolves to full 6-part entity IDs
	//   - decompose:           DecomposeArgs   {axes, focus, scope}
	//                          backend constructs typed sub-queries
	// The map is intentionally loose at the wire level so the payload
	// stays uniform; strict per-action shape checks live in the
	// Parse*Args helpers below, called by the consuming component
	// after a successful Validate() of the action enum.
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

// SeedRefType constants for WalkSeedsArgs.Seeds[*].RefType. Closed
// enum enforced by ParseWalkSeedsArgs. The backend
// (execute_subqueries) interprets the ref string differently per
// type: a "name" matches the entity's display name, a "partial_id"
// matches any suffix of the 6-part federated ID, a
// "candidate_index" indexes into the upstream ClassifierOutput
// candidate list.
const (
	SeedRefTypeName           = "name"
	SeedRefTypePartialID      = "partial_id"
	SeedRefTypeCandidateIndex = "candidate_index"
)

// DecomposeScope constants for DecomposeArgs.Scope. Closed enum;
// empty resolves to ScopeMedium at the backend so a model that
// omits the field still routes deterministically.
const (
	DecomposeScopeNarrow = "narrow"
	DecomposeScopeMedium = "medium"
	DecomposeScopeBroad  = "broad"
)

// DecomposeArgs is the typed view of RouteDecision.Args when
// Action == ActionDecompose. Intent-shaped per ADR-045 Amendment
// 2026-05-23: the model emits decomposition INTENT (what to
// decompose along, the central concept, the breadth); the backend
// (execute_subqueries) constructs the typed sub-queries from
// templates keyed on Axes + Focus. The model isn't asked to spell
// sub-query types it can't reliably produce.
type DecomposeArgs struct {
	// Axes lists the dimensions of decomposition (e.g.
	// "entity_type", "time", "spatial", "relationship"). Free-form
	// strings; backend matches against its known decomposition
	// templates.
	Axes []string `json:"axes"`

	// Focus is the central concept to decompose around. Anchors the
	// backend's template selection — e.g. "sensor maintenance
	// events" or "drone telemetry anomalies".
	Focus string `json:"focus"`

	// Scope is the breadth of decomposition. One of the
	// DecomposeScope* constants. Empty resolves to "medium" at the
	// backend.
	Scope string `json:"scope,omitempty"`
}

// SeedRef carries one seed reference for WalkSeedsArgs. RefType is
// one of SeedRefType*.
type SeedRef struct {
	// Ref is the reference itself — a display name, a partial
	// federated ID, or a stringified index into the upstream
	// classifier candidate list. Interpretation depends on RefType.
	Ref string `json:"ref"`

	// RefType disambiguates how the backend resolves Ref. Closed
	// enum.
	RefType string `json:"ref_type"`
}

// WalkSeedsArgs is the typed view of RouteDecision.Args when
// Action == ActionWalkSeeds. Intent-shaped per ADR-045 Amendment
// 2026-05-23: the model emits SEED REFERENCES (names, partial IDs,
// or indices into the classifier candidate list); the backend
// resolves to full 6-part entity IDs via the entity index. The
// model isn't asked to spell full IDs it can't reliably produce.
type WalkSeedsArgs struct {
	Seeds []SeedRef `json:"seeds"`
}

// RetightenArgs is the typed view of RouteDecision.Args when
// Action == ActionRetighten. The refined topic + hints feed back
// into a second nl_classify run; R2's retighten branch caps the
// loop at MaxIterations=2.
type RetightenArgs struct {
	// Topic is the refined topic string. Required.
	Topic string `json:"topic"`

	// Hints is the refined hint set. Optional; empty map and absent
	// field both treated as no hints.
	Hints map[string]string `json:"hints,omitempty"`
}

// IsValidSeedRefType reports whether s is one of the closed seed
// reference types. Exported so component validators can reuse the
// set.
func IsValidSeedRefType(s string) bool {
	switch s {
	case SeedRefTypeName, SeedRefTypePartialID, SeedRefTypeCandidateIndex:
		return true
	}
	return false
}

// IsValidDecomposeScope reports whether s is one of the closed
// decompose-scope values. Empty string is treated as valid (it
// resolves to "medium" at the backend); callers that want to
// reject empty should test before calling.
func IsValidDecomposeScope(s string) bool {
	switch s {
	case "", DecomposeScopeNarrow, DecomposeScopeMedium, DecomposeScopeBroad:
		return true
	}
	return false
}

// ParseDecomposeArgs decodes p.Args into DecomposeArgs and
// validates shape. Returns an error if p.Action != ActionDecompose,
// if required fields are missing, or if Scope is non-empty and
// outside the closed enum.
func (p *RouteDecision) ParseDecomposeArgs() (DecomposeArgs, error) {
	var zero DecomposeArgs
	if p == nil {
		return zero, fmt.Errorf("route decision is nil")
	}
	if p.Action != ActionDecompose {
		return zero, fmt.Errorf("ParseDecomposeArgs: action is %q, want %q", p.Action, ActionDecompose)
	}
	var args DecomposeArgs
	if err := remarshalArgs(p.Args, &args); err != nil {
		return zero, fmt.Errorf("decompose args: %w", err)
	}
	if len(args.Axes) == 0 {
		return zero, fmt.Errorf("decompose args: axes required (at least one decomposition dimension)")
	}
	for i, a := range args.Axes {
		if strings.TrimSpace(a) == "" {
			return zero, fmt.Errorf("decompose args: axes[%d] is empty", i)
		}
	}
	if strings.TrimSpace(args.Focus) == "" {
		return zero, fmt.Errorf("decompose args: focus required")
	}
	if !IsValidDecomposeScope(args.Scope) {
		return zero, fmt.Errorf("decompose args: scope %q is not a canonical value (want one of %q, %q, %q, or empty)",
			args.Scope, DecomposeScopeNarrow, DecomposeScopeMedium, DecomposeScopeBroad)
	}
	return args, nil
}

// ParseWalkSeedsArgs decodes p.Args into WalkSeedsArgs and
// validates shape. Returns an error if p.Action != ActionWalkSeeds,
// if Seeds is empty, or if any seed has an empty Ref or an
// out-of-enum RefType.
func (p *RouteDecision) ParseWalkSeedsArgs() (WalkSeedsArgs, error) {
	var zero WalkSeedsArgs
	if p == nil {
		return zero, fmt.Errorf("route decision is nil")
	}
	if p.Action != ActionWalkSeeds {
		return zero, fmt.Errorf("ParseWalkSeedsArgs: action is %q, want %q", p.Action, ActionWalkSeeds)
	}
	var args WalkSeedsArgs
	if err := remarshalArgs(p.Args, &args); err != nil {
		return zero, fmt.Errorf("walk_seeds args: %w", err)
	}
	if len(args.Seeds) == 0 {
		return zero, fmt.Errorf("walk_seeds args: seeds required (at least one)")
	}
	for i, s := range args.Seeds {
		if strings.TrimSpace(s.Ref) == "" {
			return zero, fmt.Errorf("walk_seeds args: seeds[%d].ref is empty", i)
		}
		if !IsValidSeedRefType(s.RefType) {
			return zero, fmt.Errorf("walk_seeds args: seeds[%d].ref_type %q is not a canonical value (want one of %q, %q, %q)",
				i, s.RefType, SeedRefTypeName, SeedRefTypePartialID, SeedRefTypeCandidateIndex)
		}
	}
	return args, nil
}

// ParseRetightenArgs decodes p.Args into RetightenArgs and
// validates shape. Returns an error if p.Action != ActionRetighten,
// if Topic is empty, or if any Hints value is empty.
func (p *RouteDecision) ParseRetightenArgs() (RetightenArgs, error) {
	var zero RetightenArgs
	if p == nil {
		return zero, fmt.Errorf("route decision is nil")
	}
	if p.Action != ActionRetighten {
		return zero, fmt.Errorf("ParseRetightenArgs: action is %q, want %q", p.Action, ActionRetighten)
	}
	var args RetightenArgs
	if err := remarshalArgs(p.Args, &args); err != nil {
		return zero, fmt.Errorf("retighten args: %w", err)
	}
	if strings.TrimSpace(args.Topic) == "" {
		return zero, fmt.Errorf("retighten args: topic required")
	}
	for k, v := range args.Hints {
		if strings.TrimSpace(v) == "" {
			return zero, fmt.Errorf("retighten args: hints[%q] is empty", k)
		}
	}
	return args, nil
}

// remarshalArgs converts a loose map[string]any to a typed struct
// via JSON round-trip. Keeps the Parse*Args helpers from depending
// on a reflection library and matches the wire shape exactly.
func remarshalArgs(in map[string]any, out any) error {
	if in == nil {
		// Decode the empty case as zero-value of out — caller's
		// downstream "required field" checks will surface a missing
		// field with a typed error rather than a marshal failure.
		return nil
	}
	buf, err := json.Marshal(in)
	if err != nil {
		return fmt.Errorf("remarshal: %w", err)
	}
	if err := json.Unmarshal(buf, out); err != nil {
		return fmt.Errorf("decode: %w", err)
	}
	return nil
}
