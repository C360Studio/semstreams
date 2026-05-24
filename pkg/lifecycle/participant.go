package lifecycle

import "time"

// Participant is the contract a lifecycle-tracked entity satisfies.
// Apps implement this on their domain state structs to get framework
// infrastructure (KV storage, restart recovery, rule integration,
// operator API).
//
// The interface is small by design — six required methods plus one
// optional parent-link method. All state and behavior beyond the
// declared phases lives in the implementing struct's own fields and
// methods; the harness only reads what it needs to operate
// (identity, current phase, terminal-detection, KV placement).
//
// Implementations are expected to be plain data structs with these
// methods — NOT runtime objects with mutable internal state. The
// Manager round-trips Participant values through JSON (or another
// codec) on every Get/Update, so any non-serializable fields will
// be silently dropped on restart. Apps that need transient runtime
// state should keep it OUTSIDE the Participant struct.
type Participant interface {
	// EntityID returns the 6-part federated graph entity ID
	// (org.platform.domain.system.type.instance). Used as the
	// canonical identifier across the graph, KV, and operator API.
	EntityID() string

	// Workflow returns the workflow type identifier (e.g.
	// "drone-survey", "csapi-system"). Must match a string passed
	// to Manager.Register at startup. Stable per implementation
	// type — never varies per instance.
	Workflow() string

	// Phase returns the current lifecycle phase (e.g. "planning",
	// "flying", "completed"). Must be a key in the Transitions
	// table registered for this Workflow.
	Phase() string

	// IsTerminal returns true when the entity is in a phase with no
	// declared out-edges (i.e. completed, failed, aborted). Used by
	// Manager.List with ListOptions.Active to filter active vs
	// finished instances. By convention, derived from Phase() against
	// the registered Transitions table — see DefaultIsTerminal helper.
	IsTerminal() bool

	// KVBucket returns the NATS KV bucket this entity lives in
	// (e.g. "MISSIONS", "CSAPI_SYSTEMS"). Stable per implementation
	// type. The framework does NOT create the bucket — apps
	// provision their own buckets per their deployment topology.
	KVBucket() string

	// KVKey returns the KV key shape for this instance within the
	// bucket. Apps choose the convention — common shapes include
	// the bare EntityID, a slug-prefixed key (e.g. "mission.<id>"),
	// or a multi-segment key for partitioned access. Whatever the
	// app picks, it must be stable for the instance's lifetime
	// (the harness uses it for both reads and writes).
	KVKey() string

	// ParentEntityID returns the parent workflow instance's EntityID,
	// or empty string for root workflows. Enables parent/child
	// workflow relationships (e.g. semspec's Plan-owns-Requirements
	// pattern, or a research-pack-spawns-investigators pattern).
	// Implementations with no parent-child relationships return ""
	// unconditionally.
	ParentEntityID() string
}

// TransitionEvent is one entry in an entity's phase-transition
// history. Manager.History returns these in chronological order.
//
// History is derived from KV revision replay (every write to the
// entity's KV key is one revision; Manager.Update synthesizes a
// TransitionEvent when From != To). No separate history bucket is
// required — the KV bucket's own revision log IS the history.
type TransitionEvent struct {
	// From is the phase the entity was in before the transition.
	// Empty string for the Create event (entity didn't exist before).
	From string

	// To is the phase the entity entered. Equal to the entity's
	// Phase() at the time of the transition.
	To string

	// At is the wallclock time the transition was committed to KV.
	// Sourced from the KV revision's metadata, not from app code
	// — so it's authoritative across restarts and clock skew.
	At time.Time

	// Triggered identifies what caused the transition. One of:
	//   "rule"      — a rule action invoked Manager.Transition
	//   "operator"  — an operator API call invoked Manager.Transition
	//   "component" — a component invoked Manager.Transition directly
	//   "framework" — Create / Complete / Fail (auto-classified)
	//
	// Operator dashboards can filter / color-code by this field;
	// audit trails distinguish operator-initiated from automated
	// transitions.
	Triggered string

	// Note is an optional free-text annotation. Manager.Fail uses
	// this to carry the failure reason; Manager.Transition can pass
	// arbitrary context. Empty by default.
	Note string
}

// WorkflowDef describes a registered workflow type. Returned by
// Manager.GetWorkflowDefinition and Manager.ListWorkflows; intended
// primarily for operator dashboard introspection (e.g. rendering a
// state-machine diagram derivable directly from the Transitions table).
//
// Apps don't construct WorkflowDef directly — the framework synthesizes
// it from the data passed to Manager.Register.
type WorkflowDef struct {
	// Workflow is the workflow type identifier (matches what was
	// passed to Manager.Register and what Participant.Workflow returns).
	Workflow string

	// Transitions is the declared phase-transition table registered
	// for this workflow. The state-machine diagram is derivable
	// directly from this map.
	Transitions Transitions

	// KVBucket is the NATS KV bucket this workflow's instances live
	// in. Operator API uses this to route List/Watch operations.
	KVBucket string

	// OperatorWritableFields lists the JSON field paths that are
	// tagged `lifecycle:"operator_writable"` on the registered state
	// struct. Used by UpdateFromOperator to enforce default-deny;
	// also exposed in the operator API for clients to know which
	// fields are patchable.
	OperatorWritableFields []string
}
