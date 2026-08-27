// Package mission implements the demo Mission lifecycle workflow used
// by the lifecycle e2e tier. State is the Participant; Transitions
// declares the phase graph; Register wires the workflow into
// pkg/lifecycle.Manager.
//
// The package intentionally lives under cmd/e2e-semstreams so the
// production semstreams binary does not register this workflow —
// per ADR-049 the framework provides the substrate (Manager,
// lifecycle-gateway component, lifecycle_* rule actions); apps own
// their workflow types.
package mission

import (
	"reflect"

	"github.com/c360studio/semstreams/pkg/lifecycle"
	semtypes "github.com/c360studio/semstreams/pkg/types"
	"github.com/c360studio/semstreams/vocabulary"
)

// Workflow is the registered workflow type name.
const Workflow = "mission"

// EntityIDPattern matches mission entities in the federated graph.
// Six-segment org.platform.system.domain.type.instance shape per
// pkg/types.EntityID; the org + platform + instance segments wildcard
// while system (`gcs`, the source), domain (`lifecycle`, this harness's
// delegated taxonomy), and type (`mission`) pin the canonical mission shape.
const EntityIDPattern = "*.*.gcs.lifecycle.mission.*"

// Producer is the trusted producer name this harness registers its entity
// domain under; the e2e composition root supplies it.
const Producer = "e2e-mission"

// EntityDomainDelegations declares the `lifecycle` domain this harness mints
// under. It is not a framework-reserved domain: lifecycle participation is a
// property of an entity, not a framework family (inventory PF-9).
func EntityDomainDelegations() []semtypes.EntityDomainDelegation {
	return []semtypes.EntityDomainDelegation{{Producer: Producer, Domain: "lifecycle"}}
}

// Predicate names for projection (ADR-049). Manager.Transition writes
// `mission.state.phase`; UpdateFromOperator writes `mission.owner.org-id`
// and `mission.state.note`; the audit predicates carry source attribution.
const (
	PredicatePhase       = "mission.state.phase"
	PredicateOwnerOrgID  = "mission.owner.org-id"
	PredicateNote        = "mission.state.note"
	PredicateAuditSource = "mission.transition.source"
	PredicateAuditAt     = "mission.transition.at"
	PredicateAuditFrom   = "mission.transition.from"
	PredicateAuditNote   = "mission.transition.note"
	PredicateCommand     = "mission.command.requested"
)

func init() {
	for _, predicate := range []string{
		PredicatePhase,
		PredicateOwnerOrgID,
		PredicateNote,
		PredicateAuditSource,
		PredicateAuditAt,
		PredicateAuditFrom,
		PredicateAuditNote,
		PredicateCommand,
	} {
		vocabulary.Register(predicate)
	}
}

// Phases of a mission.
const (
	PhasePlanning  = "planning"
	PhaseFlying    = "flying"
	PhaseCompleted = "completed"
	PhaseAborted   = "aborted"
)

// Transitions is the declared phase graph for a Mission.
//
//	planning ──> flying ──> completed
//	    └──> aborted
//	    flying ──> aborted
//
// completed + aborted are terminal (no out-edges).
var Transitions = lifecycle.Transitions{
	PhasePlanning:  {PhaseFlying, PhaseAborted},
	PhaseFlying:    {PhaseCompleted, PhaseAborted},
	PhaseCompleted: {},
	PhaseAborted:   {},
}

// State is a lifecycle.Participant for the Mission workflow.
//
// Field tags (ADR-049):
//   - lifecycle:"id"                                          — identity
//   - lifecycle:"phase,predicate=mission.state.phase"         — phase triple
//   - lifecycle:"operator_writable,predicate=mission.*"       — patchable via operator API
//
// The projection layer round-trips between the entity's triples in
// ENTITY_STATES and this struct on every Get / Transition; the
// Participant is a typed VIEW over triples, not a private blob.
type State struct {
	EntityIDField string `json:"entity_id" lifecycle:"id"`
	PhaseField    string `json:"phase" lifecycle:"phase,predicate=mission.state.phase"`
	OwnerOrgID    string `json:"owner_org_id,omitempty" lifecycle:"operator_writable,predicate=mission.owner.org-id"`
	Note          string `json:"note,omitempty" lifecycle:"operator_writable,predicate=mission.state.note"`
}

// New returns a freshly-initialized State. Apps don't call this on
// read — Manager.Get projects fresh instances from the registered
// Schema via reflection. New is here for Create-side callers
// (e.g. seedMission in cmd/e2e-semstreams/main.go).
func New() *State {
	return &State{}
}

// WorkflowDeclaration returns the lifecycle.Workflow ready to pass
// to Manager.Register. Centralized here so cmd/e2e-semstreams/main.go
// + the lifecycle-gateway both see the same schema.
func WorkflowDeclaration() lifecycle.Workflow {
	return lifecycle.Workflow{
		Name:            Workflow,
		EntityIDPattern: EntityIDPattern,
		Phases:          []string{PhasePlanning, PhaseFlying, PhaseCompleted, PhaseAborted},
		Transitions:     Transitions,
		PhasePredicate:  PredicatePhase,
		Schema:          reflect.TypeOf(State{}),
		OperatorWritablePredicates: []string{
			PredicateOwnerOrgID,
			PredicateNote,
		},
		AuditPredicates: lifecycle.AuditSpec{
			Source: PredicateAuditSource,
			At:     PredicateAuditAt,
			From:   PredicateAuditFrom,
			Note:   PredicateAuditNote,
		},
	}
}

// EntityID returns the federated 6-part identifier.
func (s *State) EntityID() string { return s.EntityIDField }

// Workflow returns the registered workflow type name.
func (s *State) Workflow() string { return Workflow }

// Phase returns the current phase.
func (s *State) Phase() string { return s.PhaseField }

// IsTerminal returns true when the current phase has no declared
// out-edges in Transitions.
func (s *State) IsTerminal() bool { return Transitions.IsTerminal(s.PhaseField) }

// ParentEntityID returns "" — missions have no parent workflow in
// this demo.
func (s *State) ParentEntityID() string { return "" }
