package agentic_test

import (
	"encoding/json"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/stretchr/testify/require"
)

// The owner ruling of 2026-09-03 (#1239) removed the paused state outright:
// the framework supports cancellation, durable human approval, safe
// retry/restart and operational quiescing, not arbitrary execution
// pause/resume, so no value in the vocabulary may advertise one. Deleting the
// constant is not enough on its own — LoopState is a string type, so
// LoopState("paused") remains constructible by any caller, and before this the
// exported transition took whatever it was given. These are the two boundaries
// a paused value can still arrive through.
func TestPausedIsRefusedAtTheExportedTransition(t *testing.T) {
	t.Parallel()

	entity := &agentic.LoopEntity{ID: "loop-paused", State: agentic.LoopStateExecuting, MaxIterations: 20}
	err := entity.TransitionTo(agentic.LoopState("paused"))

	require.Error(t, err, "an exported transition must refuse a state the vocabulary does not define")
	require.Contains(t, err.Error(), "invalid state: paused")
	require.Equal(t, agentic.LoopStateExecuting, entity.State,
		"a refused transition must leave the entity on its previous state")
	require.NoError(t, entity.Validate())
}

// The same refusal is not special-cased to one string: any value outside the
// vocabulary is refused, which is why no reserved enum or alias survives.
func TestUndefinedStatesAreRefusedAtTheExportedTransition(t *testing.T) {
	t.Parallel()

	for _, undefined := range []string{"paused", "suspended", "resumed", "", "PAUSED"} {
		entity := &agentic.LoopEntity{ID: "loop-x", State: agentic.LoopStateExploring, MaxIterations: 3}
		require.Error(t, entity.TransitionTo(agentic.LoopState(undefined)),
			"state %q is not in the vocabulary and must be refused", undefined)
		require.Equal(t, agentic.LoopStateExploring, entity.State)
	}

	// And the vocabulary that remains still transitions.
	for _, defined := range []agentic.LoopState{
		agentic.LoopStateExploring, agentic.LoopStatePlanning, agentic.LoopStateArchitecting,
		agentic.LoopStateExecuting, agentic.LoopStateReviewing, agentic.LoopStateAwaitingApproval,
		agentic.LoopStateComplete, agentic.LoopStateFailed, agentic.LoopStateCancelled,
	} {
		entity := &agentic.LoopEntity{ID: "loop-y", State: agentic.LoopStateExploring, MaxIterations: 3}
		require.NoError(t, entity.TransitionTo(defined), "state %q must remain reachable", defined)
	}
}

// A record written before the removal decodes — JSON cannot refuse a string —
// so validation is where it is refused. No shim, alias or legacy-valid
// exception carries it through.
func TestPersistedPausedRecordFailsValidation(t *testing.T) {
	t.Parallel()

	var entity agentic.LoopEntity
	require.NoError(t, json.Unmarshal(
		[]byte(`{"id":"loop-review","state":"paused","max_iterations":20}`), &entity),
		"decoding is not the refusal boundary; validation is")
	require.Equal(t, agentic.LoopState("paused"), entity.State)

	err := entity.Validate()
	require.Error(t, err, "a persisted paused record must be refused, not accepted as legacy-valid")
	require.Contains(t, err.Error(), "invalid state: paused")
}
