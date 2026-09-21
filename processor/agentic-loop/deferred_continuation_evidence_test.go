package agenticloop

import (
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/stretchr/testify/require"
)

// A deferred continuation publishes nothing, and for one round it recorded
// nothing either: the turn entered the loop's context, the task Acked, and the
// only later correlation was the carried request's own RequestID. "Which task
// contributed this turn" was then answerable from a log line and nowhere else,
// against a capability whose purpose makes agent execution evidence
// first-class and an ADR (098) that routes execution signals to graph
// conditions rather than logs.
//
// spec: agentic-loop / A logical model request has one deterministic identity
func TestDeferredContinuationRecordsTheTaskThatContributedTheTurn(t *testing.T) {
	h := NewMessageHandler(DefaultConfig())
	ctx := t.Context()

	birth, err := h.HandleTask(ctx, TaskMessage{
		TaskID: "task-evidence-1", Role: "general", Model: "model", Prompt: "the first thing",
	})
	require.NoError(t, err)
	loopID := birth.LoopID
	outstanding := h.loopManager.OutstandingRequest(loopID)
	require.NotEmpty(t, outstanding)

	deferred, err := h.HandleTask(ctx, TaskMessage{
		TaskID: "task-evidence-2", LoopID: loopID, Role: "general", Model: "model",
		Prompt: "and also the second thing",
	})
	require.NoError(t, err)
	require.True(t, deferred.Deferred)
	require.Empty(t, deferred.PublishedMessages, "the deferral must still publish nothing")

	require.Len(t, deferred.trajectoryObservations, 1,
		"the admitted turn left no observation, so only a log names the task that contributed it")
	observation := deferred.trajectoryObservations[0]
	require.Equal(t, agentic.TrajectorySourceTask, observation.SourceKind)
	require.Equal(t, "task-evidence-2", observation.SourceCorrelation,
		"the observation must correlate on the task, which is the identity the carried request drops")
	require.Equal(t, agentic.TrajectoryStatusRequested, observation.Status,
		"the turn is admitted and waiting for a carrier; completed would claim it was sent")

	evidence, ok := observation.Evidence.(trajectoryDeferredContinuationEvidence)
	require.True(t, ok, "evidence = %#v", observation.Evidence)
	require.Equal(t, outstanding, evidence.OutstandingRequest,
		"the evidence must name the request the turn is waiting behind")
}
