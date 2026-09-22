//go:build integration

package agenticloop

import (
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/stretchr/testify/require"
)

// TestAReplacementReArmsNoApprovalDeadline is docket OQ2's ruling as a test
// (owner ruling 2026-09-22): an approval deadline is a process-local
// convenience, not a durable fact. `PendingApproval` carries `RequestedAt` and
// `Timeout` in the record, so a deadline is RECOVERABLE — nothing in this
// change makes it recovered. No startup pass reads AGENT_LOOPS, and
// `SnapshotExpiredApprovals` reports only the loops this process holds in
// memory, which for a replacement is none of them.
//
// The zero is a MEASURED DELTA, not a bare absence: the same instrument, at the
// same instant, over the same durable record, reports the deadline on the
// process that armed it and reports nothing on the process that replaced it.
// An assertion that only read the replacement would pass against a component
// whose approval gate never fired at all.
//
// The instant is passed in rather than waited for. `SnapshotExpiredApprovals`
// takes `now` as an argument, so a deadline past its expiry is expressed by
// asking about a later instant — no sleep, no backdated fixture, and the
// configured 12h wait stays the production one.
//
// spec: agentic-loop / The loop record names its outstanding request
func TestAReplacementReArmsNoApprovalDeadline(t *testing.T) {
	client := newLoopNATS(t)
	predecessor, handler := startLoopProcess(t, client, DefaultConfig())
	loopID := gatedLoop(t, client, predecessor, handler, "task-approval-oq2")

	// Any instant past RequestedAt + the configured approval wait. Both
	// processes are asked about this same instant, so the two answers differ
	// only in what each process holds.
	expired := time.Now().UTC().Add(DefaultConfig().ApprovalTimeout() + time.Hour)

	armed := predecessor.handler.loopManager.SnapshotExpiredApprovals(expired)
	require.Len(t, armed, 1, "the process that gated the loop must report its deadline, or the delta below measures nothing")
	require.Equal(t, loopID, armed[0].LoopID)
	require.NotZero(t, armed[0].Timeout, "a candidate with no timeout is not an armed deadline")

	gated := loopRecordOf(t, predecessor, loopID)
	require.Equal(t, agentic.LoopStateAwaitingApproval, gated.entity.State)
	require.NotNil(t, gated.entity.PendingApproval,
		"the deadline's two durable fields are on the record; what is not durable is the timer")
	require.NotZero(t, gated.entity.PendingApproval.Timeout)

	// A replacement: its own handler, its own LoopManager, its own memory,
	// over the same bucket and the same stream.
	replacement, _ := startLoopProcess(t, client, DefaultConfig())

	require.Empty(t, replacement.handler.loopManager.SnapshotExpiredApprovals(expired),
		"a replacement re-armed a deadline its predecessor held; the auto-reject would fire on a loop this process never gated")

	after := loopRecordOf(t, replacement, loopID)
	require.Equal(t, agentic.LoopStateAwaitingApproval, after.entity.State,
		"the loop stays awaiting_approval until the approval is answered or the loop is cancelled")
	require.Equal(t, gated.revision, after.revision,
		"a replacement that re-arms nothing writes nothing")
	require.Equal(t, gated.entity.PendingApproval.RequestID, after.entity.PendingApproval.RequestID)
}

// gatedLoop drives a loop into awaiting_approval the way production does: a
// born loop, a model response dispatching one tool, and a tool result whose
// error carries the approval_required prefix. The gate that fires is the real
// one (handlers.go gateForApproval), so RequestedAt and Timeout are the fields
// a real pending approval carries and the record is written by the real
// carrier.
func gatedLoop(t *testing.T, client *natsclient.Client, c *Component, h *MessageHandler, taskID string) string {
	t.Helper()
	loopID, firstRequest := bornLoop(t, c, h, taskID)

	batch := agentic.AgentResponse{
		RequestID:    firstRequest,
		Status:       agentic.StatusToolCall,
		FinishReason: "tool_calls",
		Message: agentic.ChatMessage{
			Role:      "assistant",
			ToolCalls: []agentic.ToolCall{{ID: "call-gated", Name: "delete_rule"}},
		},
	}
	retainModelResponse(t, client, batch)
	dispatch, err := h.HandleModelResponse(t.Context(), loopID, batch)
	require.NoError(t, err)
	require.NoError(t, c.persistHandlerResult(t.Context(), dispatch, publishThenWrite))
	call, _ := dispatchedToolCall(t, dispatch)

	_, delivered := deliverToolResult(t, c, agentic.ToolResult{
		CallID: call.ID, Name: call.Name, LoopID: loopID,
		Error:       agentic.ApprovalRequiredPrefix + "confirm the deletion",
		RequestID:   call.RequestID,
		ExecutionID: call.ExecutionID,
		CallOrdinal: call.CallOrdinal,
	})
	require.Equal(t, natsclient.DeliveryDecisionAck, delivered.Decision(),
		"the gated result is handled; the loop parks rather than the delivery failing")
	return loopID
}
