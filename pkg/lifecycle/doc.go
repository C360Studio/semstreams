// Package lifecycle provides a substrate convention layer for
// workflow-shaped entities — named instances with declared phases,
// restart recovery, operator visibility, and rule integration.
//
// # What this is (and is not)
//
// This package is a SUBSTRATE CONVENTION LAYER, not a workflow engine.
// It provides:
//   - A Participant interface apps implement on their domain state structs
//   - A Manager harness that handles KV storage, restart recovery,
//     and operator API integration
//   - A Transitions table that declares valid phase transitions per workflow
//   - Struct-tag-based operator-writability declaration (default-deny)
//
// It does NOT provide:
//   - A runtime, DSL, or state-machine interpreter (apps own work logic)
//   - A separate event bus (uses existing NATS KV primitives)
//   - A process orchestrator (orchestration stays in the rule engine)
//   - A replacement for components (components remain the execution layer)
//
// See ADR-047 for the full rationale.
//
// # Participation is per-entity, not per-deployment
//
// The lifecycle harness has ZERO dependencies on agentic-loop or any
// other consumer class. Apps that ship no agentic features get the
// full harness benefit. This package MUST NOT import any
// processor/agentic-* package — verified by import lint.
//
// Participant is opt-in per ENTITY-TYPE within a single app:
//   - Drone missions, sensor lifecycles, manufacturing batches,
//     scenario executions implement Participant
//   - Raw telemetry, log entries, agent loops, and transient inputs
//     stay outside the harness and pay zero cost
//
// # Usage
//
// Apps annotate state-struct fields with the lifecycle struct tag:
//
//	type SystemState struct {
//	    EntityID_   string `json:"entity_id"    lifecycle:"id"`
//	    Phase_      string `json:"phase"        lifecycle:"phase,readonly"`
//	    OwnerOrgID  string `json:"owner_org_id" lifecycle:"operator_writable"`
//	    InternalState string `json:"internal_state,omitempty"`
//	    // no tag = not operator-writable (default-deny)
//	}
//
// Tag values:
//   - id                — marks the EntityID field (one per struct)
//   - phase             — marks the Phase field (one per struct)
//   - readonly          — never operator-writable
//   - operator_writable — opt-in operator-writability via Manager.UpdateFromOperator
//   - indexable         — flagged for v2 secondary indexing (v1 ignores)
//
// Apps register a Workflow type at startup with its valid transitions:
//
//	transitions := lifecycle.Transitions{
//	    "planning":   {"flying", "aborted"},
//	    "flying":     {"capturing", "landing", "aborted"},
//	    "capturing":  {"flying"},
//	    "landing":    {"completed", "failed"},
//	    "completed":  {},  // terminal — no out-edges
//	    "failed":     {},
//	    "aborted":    {},
//	}
//	mgr.Register("drone-survey", func() Participant {
//	    return &MissionState{}
//	}, transitions)
//
// See ADR-047 for the complete worked example.
package lifecycle
