package lifecycle

import "time"

// Participant is the contract a lifecycle-tracked entity satisfies.
// Apps implement this on their domain state structs to get framework
// infrastructure (graph projection, restart recovery, rule integration,
// operator API).
//
// The interface is small by design — four required methods plus one
// optional parent-link method. All state and behavior beyond the
// declared phases lives in the implementing struct's own fields and
// methods; the harness only reads what it needs to operate
// (identity, current phase, terminal-detection).
//
// Per ADR-049 the harness lives over ENTITY_STATES — there is no
// private KV bucket. State changes emit through graph-ingest via
// the standard write path; the Participant is materialized via
// reflection-driven projection from the entity's triples (see
// Workflow.Schema). KVBucket() and KVKey() are no longer part of
// the contract.
//
// Implementations are expected to be plain data structs with these
// methods. The projection layer round-trips Participant values
// through triples, NOT through JSON of the whole struct, so
// non-serializable runtime state on the Participant is fine — it
// just won't survive a Get/round-trip if not also projected from
// a declared predicate.
type Participant interface {
	// EntityID returns the 6-part federated graph entity ID
	// (org.platform.domain.system.type.instance). Used as the
	// canonical identifier across the graph, KV, and operator API.
	EntityID() string

	// Workflow returns the workflow type identifier (e.g.
	// "mission", "csapi-system"). Must match a Workflow.Name
	// passed to Manager.Register at startup. Stable per
	// implementation type — never varies per instance.
	Workflow() string

	// Phase returns the current lifecycle phase (e.g. "planning",
	// "flying", "completed"). Must be a key in the Workflow's
	// Transitions table.
	Phase() string

	// IsTerminal returns true when the entity is in a phase with no
	// declared out-edges (i.e. completed, failed, aborted). Used by
	// Manager.List with ListOptions.Active to filter active vs
	// finished instances. By convention, derived from Phase() against
	// the registered Transitions table — see Transitions.IsTerminal.
	IsTerminal() bool

	// ParentEntityID returns the parent workflow instance's EntityID,
	// or empty string for root workflows. Enables explicit parent/child
	// workflow relationships in workflows that prefer a struct field
	// over a reference predicate. Implementations with no parent-child
	// relationships return "" unconditionally.
	//
	// For Tier-B workflows that prefer to declare children via the
	// schema's ChildWorkflows + ReferencePredicates, this can stay
	// empty; the harness uses the predicate-declared graph instead.
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
// History is derived from ENTITY_STATES revision replay filtered to
// phase-changing writes; source attribution is reconstructed from
// the AuditPredicates triples Manager.Transition stamped at write
// time. No parallel audit bucket is required.
type TransitionEvent struct {
	// From is the phase the entity was in before the transition.
	// Empty string for the Create event (entity didn't exist before).
	From string `json:"from"`

	// To is the phase the entity entered. Equal to the entity's
	// PhasePredicate value at the time of the transition.
	To string `json:"to"`

	// At is the wallclock time the transition was committed.
	// Sourced from the KV revision's metadata, not from app code
	// — so it's authoritative across restarts and clock skew.
	At time.Time `json:"at"`

	// Triggered identifies what caused the transition. Closed set
	// of values defined by TransitionSource. Read from the audit
	// triple stamped at write time; defaults to
	// TransitionSourceFramework when the workflow has no
	// AuditPredicates.Source declared.
	Triggered TransitionSource `json:"triggered"`

	// Note is an optional free-text annotation, stamped by
	// Manager.Transition if AuditPredicates.Note is declared.
	// Empty when omitted at write time or undeclared.
	Note string `json:"note,omitempty"`
}

// WorkflowDef describes a registered workflow type. Returned by
// Manager.GetWorkflowDefinition and Manager.ListWorkflows; intended
// primarily for operator dashboard introspection (e.g. rendering a
// state-machine diagram derivable directly from the Transitions table).
//
// Apps don't construct WorkflowDef directly — the framework synthesizes
// it from the Workflow declaration passed to Manager.Register.
type WorkflowDef struct {
	// Workflow is the workflow type identifier (matches the
	// Workflow.Name passed to Manager.Register and what
	// Participant.Workflow returns).
	Workflow string `json:"workflow"`

	// Transitions is the declared phase-transition table registered
	// for this workflow. The state-machine diagram is derivable
	// directly from this map.
	Transitions Transitions `json:"transitions"`

	// EntityIDPattern is the 6-part federated-ID glob this workflow
	// matches. Operator dashboards use this to scope discovery.
	EntityIDPattern string `json:"entity_id_pattern"`

	// PhasePredicate is the triple predicate that carries the
	// entity's phase.
	PhasePredicate string `json:"phase_predicate"`

	// OperatorWritableFields lists the JSON field paths that are
	// tagged `lifecycle:"operator_writable"` on the registered Schema
	// struct. Used by UpdateFromOperator to enforce default-deny;
	// also exposed in the operator API for clients to know which
	// fields are patchable.
	OperatorWritableFields []string `json:"operator_writable_fields"`

	// OperatorWritablePredicates lists the underlying predicate names
	// for the patchable fields. Operator clients that want to write
	// triples directly (via graph-gateway) can match on these.
	OperatorWritablePredicates []string `json:"operator_writable_predicates"`
}
