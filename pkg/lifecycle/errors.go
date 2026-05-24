package lifecycle

import "errors"

// Package error sentinels. Callers compare with errors.Is.
var (
	// ErrWorkflowNotRegistered is returned by Manager operations when the
	// referenced workflow type was never passed to Manager.Register. This
	// is always a programming error — registration belongs at startup,
	// before any Get/Create/Transition calls land.
	ErrWorkflowNotRegistered = errors.New("lifecycle: workflow not registered")

	// ErrWorkflowAlreadyRegistered is returned by Manager.Register when a
	// workflow type is registered twice. Registration must be idempotent
	// at startup, not a fire-and-forget — duplicate registration likely
	// indicates a wire-up bug, not a benign re-init.
	ErrWorkflowAlreadyRegistered = errors.New("lifecycle: workflow already registered")

	// ErrEntityNotFound is returned by Manager.Get when no instance exists
	// at the given EntityID. Distinct from a KV-layer error so callers can
	// branch cleanly on "doesn't exist yet" vs "KV is broken."
	ErrEntityNotFound = errors.New("lifecycle: entity not found")

	// ErrInvalidTransition is returned by Manager.Transition when the
	// requested (from → to) edge is not declared in the registered
	// Transitions table for the workflow. Surfaces misconfigured rules
	// at runtime rather than letting the state machine drift.
	ErrInvalidTransition = errors.New("lifecycle: invalid transition")

	// ErrTerminalPhase is returned by Manager.Transition when the current
	// phase has no declared out-edges (i.e. is terminal). Distinguishes
	// "you tried to transition from completed/failed" from a generic
	// invalid-edge error so operator dashboards can show the right hint.
	ErrTerminalPhase = errors.New("lifecycle: entity is in terminal phase")

	// ErrMissingIDField is returned by struct-tag parsing when a
	// registered factory produces a Participant struct with no field
	// tagged `lifecycle:"id"`. Validates app-side wiring at Register
	// time, not at first use, so the bug surfaces during startup.
	ErrMissingIDField = errors.New("lifecycle: state struct missing field with lifecycle:\"id\" tag")

	// ErrMissingPhaseField mirrors ErrMissingIDField for the phase field.
	ErrMissingPhaseField = errors.New("lifecycle: state struct missing field with lifecycle:\"phase\" tag")

	// ErrFieldNotOperatorWritable is returned by Manager.UpdateFromOperator
	// when the patch attempts to mutate a field NOT tagged
	// `lifecycle:"operator_writable"`. Default-deny: unflagged fields are
	// not operator-writable.
	ErrFieldNotOperatorWritable = errors.New("lifecycle: field is not operator_writable")

	// ErrInvalidTransitionsTable is returned by Manager.Register when the
	// Transitions table is internally inconsistent (e.g. an out-edge
	// references a phase not declared as a key). Catches typos at startup.
	ErrInvalidTransitionsTable = errors.New("lifecycle: invalid transitions table")
)
