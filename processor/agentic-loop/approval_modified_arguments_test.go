package agenticloop

import (
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/stretchr/testify/require"
)

// An approved call's dispatch arguments are recorded only while this process
// knows them (PR #1366 post-archive review P2; owner ruling "omit, do not
// falsify", #1362 issuecomment-5827720719). A human's modify replaces the
// proposed arguments at dispatch, and nothing retained that a replacement can
// read carries the replacement set. The retained model response carries the
// PROPOSAL, so a loop rebuilt from it for the approved call's result records
// no dispatch arguments rather than the proposal. The tool.requested fact
// recorded at dispatch is the record of what ran.

// modifiedApprovalResult answers the cold gate with a modify, then delivers the
// approved call's real result. lose drops the loop between the two, so the
// result recovers the loop cold from its record and retained evidence.
func modifiedApprovalResult(t *testing.T, lose bool) (map[string]any, HandlerResult) {
	t.Helper()
	a := newColdApproval(t, nil, nil)
	response := a.answer(agentic.ApprovalDecisionModify)
	response.ModifiedArguments = map[string]any{"rule_id": "rule-human-authorized"}
	decision, err := a.deliver(t, response)
	require.NoError(t, err)
	require.Equal(t, natsclient.DeliveryDecisionAck, decision)
	require.Equal(t, response.ModifiedArguments, a.c.handler.loopManager.GetToolArguments(a.gate.ExecutionID),
		"the cold approval re-seats the human's arguments at dispatch")

	realResult := agentic.ToolResult{
		LoopID: coldApprovalLoopID, RequestID: a.request, ExecutionID: a.gate.ExecutionID,
		CallID: a.gate.CallID, CallOrdinal: a.gate.CallOrdinal, Name: a.gate.ToolName,
		Content: "rule-human-authorized deleted",
	}
	if lose {
		a.c.releaseLoopTransientState(coldApprovalLoopID)
		rebuilt, err := a.c.settleToolResultWithoutLoop(t.Context(), realResult)
		require.NoError(t, err)
		require.True(t, rebuilt, "fixture: the result rebuilds the loop cold")
	}
	result, err := a.c.handler.HandleToolResult(t.Context(), coldApprovalLoopID, realResult)
	require.NoError(t, err)
	return response.ModifiedArguments, result
}

// recordedArguments reads the two places a completed call records its
// dispatch arguments: the completion evidence and the tool_call step.
func recordedArguments(t *testing.T, result HandlerResult) (evidence, step map[string]any) {
	t.Helper()
	foundEvidence, foundStep := false, false
	for _, observation := range result.trajectoryObservations {
		if observation.Kind != agentic.TrajectoryKindToolCompleted {
			continue
		}
		completion, ok := observation.Evidence.(trajectoryToolCompletionEvidence)
		require.True(t, ok)
		evidence, foundEvidence = completion.DispatchArguments, true
	}
	for _, s := range result.TrajectorySteps {
		if s.StepType == "tool_call" {
			step, foundStep = s.ToolArguments, true
		}
	}
	require.True(t, foundEvidence, "no tool completion evidence was emitted")
	require.True(t, foundStep, "no tool_call trajectory step was emitted")
	return evidence, step
}

// TestAColdRecoveredApprovedCallRecordsNoDispatchArguments is the review's
// modify → approval persisted → transient state lost → cold result recovery.
//
// spec: agentic-loop / Full trajectory evidence uses the registered Store
func TestAColdRecoveredApprovedCallRecordsNoDispatchArguments(t *testing.T) {
	_, result := modifiedApprovalResult(t, true)

	evidence, step := recordedArguments(t, result)

	require.Empty(t, evidence,
		"a replacement cannot know what the human dispatched; the proposal must not be recorded as dispatched")
	require.Empty(t, step, "the trajectory step must not record the proposal as the call's arguments")
}

// TestAColdApprovedCallStillRecordsItsModifiedArguments is the positive half:
// the process that dispatched the modified call — here one that rebuilt the
// loop cold to apply the answer — still knows and records what it sent.
//
// spec: agentic-loop / Full trajectory evidence uses the registered Store
func TestAColdApprovedCallStillRecordsItsModifiedArguments(t *testing.T) {
	modified, result := modifiedApprovalResult(t, false)

	evidence, step := recordedArguments(t, result)

	require.Equal(t, modified, evidence)
	require.Equal(t, modified, step)
}
