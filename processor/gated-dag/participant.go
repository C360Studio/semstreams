package gateddagexec

import (
	"reflect"

	"github.com/c360studio/semstreams/pkg/lifecycle"
)

// FanOutWorkflow is the framework-default lifecycle workflow name the executor
// watches and self-registers when the consumer does not supply its own.
const FanOutWorkflow = "gateddag-fanout"

// FanOutEntityIDPattern matches FanOut instance entities. Six-segment
// org.platform.system.domain.type.instance shape: org + platform + instance
// wildcard; system (`gated-dag`, the minting component), domain (`agent`, the
// framework-reserved domain this family re-slotted under — ADR-102, ruled
// O-9), type (`fanout`) pin the canonical shape.
const FanOutEntityIDPattern = "*.*.gated-dag.agent.fanout.*"

// PredicateFanOutPhase is the FanOut phase predicate. lifecycle.Watch only
// delivers entries carrying this predicate, so the watched entities MUST carry
// it (the periodic backstop is the safety net for any entity that does not).
const PredicateFanOutPhase = "gateddag.fanout.phase"

// FanOut phases. The instance is `dispatching` while units remain, then a
// terminal phase. Terminal detection is derived from the Transitions table.
const (
	PhaseDispatching = "dispatching"
	PhaseCompleted   = "completed"
	PhaseFailed      = "failed"
)

// Transitions is the FanOut phase graph:
//
//	dispatching ──> completed
//	            └─> failed
//
// completed + failed are terminal (no out-edges).
var Transitions = lifecycle.Transitions{
	PhaseDispatching: {PhaseCompleted, PhaseFailed},
	PhaseCompleted:   {},
	PhaseFailed:      {},
}

// FanOut is the lifecycle.Participant for a gated-DAG fan-out instance
// (ADR-047). It is a thin named instance carrying restart recovery + operator
// gateway visibility; the unit set + edges + markers live on the per-unit
// entities under Config.UnitEntityPrefix, not here.
type FanOut struct {
	EntityIDField string `json:"entity_id" lifecycle:"id"`
	PhaseField    string `json:"phase" lifecycle:"phase,predicate=gateddag.fanout.phase"`
}

// WorkflowDeclaration returns the framework FanOut workflow ready for
// Manager.Register. The executor registers this when FanOutWorkflow is left at
// its default; a consumer that points Config.FanOutWorkflow at its own workflow
// registers that one itself.
func WorkflowDeclaration() lifecycle.Workflow {
	return lifecycle.Workflow{
		Name:            FanOutWorkflow,
		EntityIDPattern: FanOutEntityIDPattern,
		Phases:          []string{PhaseDispatching, PhaseCompleted, PhaseFailed},
		Transitions:     Transitions,
		PhasePredicate:  PredicateFanOutPhase,
		Schema:          reflect.TypeOf(FanOut{}),
	}
}

// EntityID returns the federated 6-part identifier.
func (f *FanOut) EntityID() string { return f.EntityIDField }

// Workflow returns the registered workflow type name.
func (f *FanOut) Workflow() string { return FanOutWorkflow }

// Phase returns the current phase.
func (f *FanOut) Phase() string { return f.PhaseField }

// IsTerminal reports whether the current phase has no declared out-edges.
func (f *FanOut) IsTerminal() bool { return Transitions.IsTerminal(f.PhaseField) }

// ParentEntityID returns "" — fan-out instances have no parent workflow here.
func (f *FanOut) ParentEntityID() string { return "" }
