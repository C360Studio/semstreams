package agenticloop_test

import (
	"testing"

	"github.com/c360studio/semstreams/agentic"
	agenticloop "github.com/c360studio/semstreams/processor/agentic-loop"
	"github.com/stretchr/testify/require"
)

// TransitionLoop is the manager-level exported transition and delegates to
// LoopEntity.TransitionTo. It is tested separately because delegation is an
// implementation detail a later refactor could drop, and the owner ruling of
// 2026-09-03 (#1239) binds the boundary, not the call chain: a caller reaching
// the manager must be refused "paused" exactly as a caller reaching the entity
// is.
func TestTransitionLoopRefusesPaused(t *testing.T) {
	t.Parallel()

	manager := agenticloop.NewLoopManager()
	loopID, err := manager.CreateLoop("task-paused", "general", "model", 5)
	require.NoError(t, err)

	err = manager.TransitionLoop(loopID, agentic.LoopState("paused"))
	require.Error(t, err, "the manager transition must refuse a state the vocabulary does not define")
	require.Contains(t, err.Error(), "invalid state: paused")

	entity, getErr := manager.GetLoop(loopID)
	require.NoError(t, getErr)
	require.NotEqual(t, agentic.LoopState("paused"), entity.State,
		"a refused transition must not have reached the stored entity")
	require.NoError(t, entity.Validate())

	// The vocabulary that remains still moves through the manager.
	require.NoError(t, manager.TransitionLoop(loopID, agentic.LoopStateAwaitingApproval))
}
