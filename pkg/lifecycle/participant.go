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
	//
	// TODO(manager): Manager.Get / Manager.List should validate that
	// the returned Phase is actually declared in the registered
	// Transitions table for this workflow type, and surface drift
	// loudly (degraded-wrapper or error per the Degraded-bool
	// precedent from PR #137 / GH #120). Without that check, an
	// entity whose Phase() returns "completed-with-typo" silently
	// reads as terminal forever (Transitions.IsTerminal defaults
	// unknown phases to terminal as a defensive fallback) and never
	// surfaces in Active=true lists where an operator would notice.
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

	// KVKey returns the KV key shape for the given entity ID within
	// the bucket. Apps choose the convention — common shapes include
	// the bare entityID, a workflow-prefixed key (e.g. "mission."+id),
	// or a multi-segment key for partitioned access.
	//
	// IMPORTANT: KVKey is a pure function of entityID. Implementations
	// MUST NOT read other fields of the Participant struct — Manager
	// caches one sample instance per registration and calls KVKey on
	// it with the resolved entityID, so per-instance struct state
	// (Phase, OwnerOrgID, etc.) is not populated at the call site.
	// Apps needing multi-field key derivation should encode the
	// extra context into the entityID itself (e.g. "<slug>.<id>")
	// or capture immutable workflow-level state in package vars at
	// Register time. This signature shape was chosen to keep
	// Manager.Get/Create/Update reflect-free in the per-message
	// hotpath — see ADR-047's "Participation is per-entity" section.
	KVKey(entityID string) string

	// ParentEntityID returns the parent workflow instance's EntityID,
	// or empty string for root workflows. Enables parent/child
	// workflow relationships (e.g. semspec's Plan-owns-Requirements
	// pattern, or a research-pack-spawns-investigators pattern).
	// Implementations with no parent-child relationships return ""
	// unconditionally.
	ParentEntityID() string
}

// TransitionSource identifies what caused a phase transition. Closed
// set of typed constants — Manager.Transition takes a TransitionSource
// parameter, and rule actions / operator API / component direct calls
// each pass the appropriate constant rather than authoring a string
// literal at the call site (typos like "oprator" silently break
// dashboard filtering otherwise).
//
// Wire-compatible with string serialization: string(TransitionSourceRule)
// is "rule", round-trips through JSON / KV as-is.
type TransitionSource string

// Defined TransitionSource values. The set is closed; adding a new
// kind requires both a constant here and a Manager call site that
// produces it.
const (
	// TransitionSourceRule is set when a rule action invoked
	// Manager.Transition (the common case for state-machine
	// progression driven by the rule engine).
	TransitionSourceRule TransitionSource = "rule"

	// TransitionSourceOperator is set when the operator API
	// (POST /workflows/{type}/{id}/transition) invoked
	// Manager.Transition. Audit trails distinguish operator-
	// initiated transitions from automated ones via this value.
	TransitionSourceOperator TransitionSource = "operator"

	// TransitionSourceComponent is set when a component invoked
	// Manager.Transition directly (rare; usually rules orchestrate
	// transitions and components only call Update / Complete /
	// Fail). Reserved for cases where the component's work IS the
	// transition (e.g. landing-executor → landed phase).
	TransitionSourceComponent TransitionSource = "component"

	// TransitionSourceFramework is set by Manager.Create,
	// Manager.Complete, and Manager.Fail — i.e. the harness's own
	// transition-emitting operations rather than caller-driven ones.
	TransitionSourceFramework TransitionSource = "framework"
)

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

	// Triggered identifies what caused the transition. Closed set
	// of values defined by TransitionSource. Operator dashboards
	// can filter / color-code by this field; audit trails
	// distinguish operator-initiated from automated transitions.
	Triggered TransitionSource

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
