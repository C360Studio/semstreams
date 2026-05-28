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

	// ErrEntityNotFound is returned by Manager.Get when no entity exists
	// at the given EntityID in ENTITY_STATES. Distinct from
	// ErrEntityNotLifecycleManaged — that one fires when the entity
	// exists but has no phase triple (lifecycle never attached).
	ErrEntityNotFound = errors.New("lifecycle: entity not found")

	// ErrEntityNotLifecycleManaged is returned by Manager.Get / Transition
	// / Complete / Fail / UpdateFromOperator when the entity exists in
	// ENTITY_STATES but has no triple for the workflow's PhasePredicate
	// — i.e. Manager.Create was never called for it. Distinct from
	// ErrEntityNotFound so callers can distinguish "no such entity" from
	// "exists but not lifecycle-managed yet" (forward-reference case
	// per ADR-049 Q5).
	ErrEntityNotLifecycleManaged = errors.New("lifecycle: entity not lifecycle-managed (no phase triple)")

	// ErrAlreadyExists is returned by Manager.Create when the entity
	// already has a triple for the workflow's PhasePredicate (the
	// entity is already lifecycle-managed in this workflow). The
	// entity itself MAY exist with non-lifecycle triples; ADR-049's
	// Create semantics is "add lifecycle dimension," not "create
	// fresh entity."
	ErrAlreadyExists = errors.New("lifecycle: entity already lifecycle-managed")

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
	// registered Schema struct has no field tagged `lifecycle:"id"`.
	// Validates app-side wiring at Register time so the bug surfaces
	// during startup.
	ErrMissingIDField = errors.New("lifecycle: Schema struct missing field with lifecycle:\"id\" tag")

	// ErrMissingPhaseField mirrors ErrMissingIDField for the phase field.
	ErrMissingPhaseField = errors.New("lifecycle: Schema struct missing field with lifecycle:\"phase\" tag")

	// ErrFieldNotOperatorWritable is returned by Manager.UpdateFromOperator
	// when the patch attempts to mutate a field NOT tagged
	// `lifecycle:"operator_writable"`. Default-deny: unflagged fields are
	// not operator-writable.
	ErrFieldNotOperatorWritable = errors.New("lifecycle: field is not operator_writable")

	// ErrInvalidTransitionsTable is returned by Manager.Register when the
	// Transitions table is internally inconsistent (e.g. an out-edge
	// references a phase not declared as a key). Catches typos at startup.
	ErrInvalidTransitionsTable = errors.New("lifecycle: invalid transitions table")

	// ErrUpdateRetriesExhausted is returned by Manager.Update,
	// Transition, UpdateFromOperator, Complete, and Fail when the
	// per-call CAS-conflict retry budget is consumed under persistent
	// contention.
	//
	// Operationally this signals one of:
	//   - A stuck rule writing to the same entity in a tight loop
	//   - An upstream system hammering the same key past framework
	//     capacity
	//   - Misconfigured per-entity fan-in (multiple coordinators
	//     racing on one state)
	//
	// Callers wanting application-layer retry semantics distinct
	// from the framework's bounded retry can branch on
	// errors.Is(err, lifecycle.ErrUpdateRetriesExhausted) and apply
	// their own backoff + retry policy.
	ErrUpdateRetriesExhausted = errors.New("lifecycle: Update retry budget exhausted (persistent CAS contention)")

	// ErrEmitFailed is returned by Manager state-change operations
	// when the graph-ingest emit (NATS request/reply on the
	// UpdateEntityWithTriples subject) fails — typically because
	// graph-ingest is down, the request handler returns a non-CAS
	// error, or the NATS transport itself errors. Wraps the
	// underlying transport / handler error so callers can branch
	// on transient-vs-permanent.
	ErrEmitFailed = errors.New("lifecycle: emit to graph-ingest failed")
)
