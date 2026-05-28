// Package mission implements the demo Mission lifecycle workflow used
// by the lifecycle e2e tier. State is the Participant; Transitions
// declares the phase graph; Register wires the workflow into
// pkg/lifecycle.Manager.
//
// The package intentionally lives under cmd/e2e-semstreams so the
// production semstreams binary does not register this workflow —
// per ADR-047 the framework provides the substrate (Manager,
// lifecycle-gateway component, lifecycle_* rule actions); apps own
// their workflow types.
package mission

import (
	"github.com/c360studio/semstreams/pkg/lifecycle"
)

// Workflow is the registered workflow type name.
const Workflow = "mission"

// KVBucket is the NATS KV bucket that stores Mission instances.
const KVBucket = "MISSIONS"

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
// Field tags:
//   - lifecycle:"id"                 — identity, read-only via operator path
//   - lifecycle:"phase"              — phase, owned by Manager.Transition
//   - lifecycle:"operator_writable"  — patchable through POST .../state
//
// All fields carry json tags because Manager round-trips Participants
// through JSON on every Get/Update. The OwnerOrgID field is the only
// operator-writable surface — phase changes go through Transition.
type State struct {
	EntityIDField string `json:"entity_id" lifecycle:"id"`
	PhaseField    string `json:"phase" lifecycle:"phase"`
	OwnerOrgID    string `json:"owner_org_id,omitempty" lifecycle:"operator_writable"`
	Note          string `json:"note,omitempty" lifecycle:"operator_writable"`
}

// New returns a freshly-initialized State. The Manager's factory
// callback uses this to produce target instances for Get/Update.
// EntityID + Phase are zeroed; they are populated by JSON unmarshal
// from KV (Get) or by the caller before Manager.Create.
func New() lifecycle.Participant {
	return &State{}
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

// KVBucket returns the NATS KV bucket name.
func (s *State) KVBucket() string { return KVBucket }

// KVKey returns the KV key for a given entity ID. Pure function of
// entityID per ADR-047's Participant contract — no per-instance
// state is consulted (the Manager caches one sample and calls KVKey
// with resolved entityIDs).
func (s *State) KVKey(entityID string) string { return entityID }

// ParentEntityID returns "" — missions have no parent workflow in
// this demo. Apps wanting parent/child workflows (e.g. plan owns
// requirements) return the parent's EntityID here.
func (s *State) ParentEntityID() string { return "" }
