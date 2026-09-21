package agenticloop

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/agentic"
)

// gateCallForApproval dispatches one tool call on a loop and gates it, returning
// the stamped call. Mirrors the production order: stamp execution identity on
// the model's calls, dispatch, then gate on the approval-required result.
func gateCallForApproval(t *testing.T, h *MessageHandler, loopID, requestID, callID, toolName string, args map[string]any) agentic.ToolCall {
	t.Helper()
	calls := []agentic.ToolCall{{ID: callID, Name: toolName, Arguments: args}}
	require.NoError(t, stampToolExecutionCorrelation(requestID, calls))
	require.NoError(t, h.dispatchToolCall(&HandlerResult{}, loopID, calls[0]))

	entity, err := h.loopManager.GetLoop(loopID)
	require.NoError(t, err)
	_, err = h.gateForApproval(loopID, &entity, agentic.ToolResult{
		CallID:      calls[0].ID,
		RequestID:   calls[0].RequestID,
		ExecutionID: calls[0].ExecutionID,
		CallOrdinal: calls[0].CallOrdinal,
		Error:       agentic.ApprovalRequiredPrefix + "review",
	})
	require.NoError(t, err)
	return calls[0]
}

func dispatchedToolNames(t *testing.T, result HandlerResult) []string {
	t.Helper()
	var names []string
	for _, msg := range result.PublishedMessages {
		var envelope struct {
			Payload agentic.ToolCall `json:"payload"`
		}
		if err := json.Unmarshal(msg.Data, &envelope); err != nil {
			continue
		}
		if envelope.Payload.Name != "" {
			names = append(names, envelope.Payload.Name)
		}
	}
	return names
}

// The same-loop, later-turn case the across-loops test did not cover. A
// provider may reuse a CallID on its next turn of the SAME conversation; the
// framework's execution identity does not, because it is derived from the
// RequestID. Matching an approval on provider CallID alone therefore lets an
// approval a human gave for tool A authorise tool B — a different invocation,
// with different arguments, that nobody approved.
//
// spec: agentic-loop / Tool execution has stable framework correlation
func TestReplayedApprovalDoesNotAuthoriseALaterCallWithTheSameProviderCallID(t *testing.T) {
	h := NewMessageHandler(DefaultConfig())
	loopID, err := h.loopManager.CreateLoop("task-replay", "general", "model")
	require.NoError(t, err)

	const providerCallID = "provider-call"

	// Turn 1: delete-a is gated and approved by a human who saw "target: a".
	callA := gateCallForApproval(t, h, loopID, loopID+":req:1:0", providerCallID,
		"delete-a", map[string]any{"target": "a"})
	approvalA := agentic.ApprovalResponse{
		LoopID:      loopID,
		CallID:      callA.ID,
		ExecutionID: callA.ExecutionID,
		Decision:    agentic.ApprovalDecisionApprove,
		ApprovedBy:  "reviewer",
	}
	approved, err := h.HandleApprovalResponse(context.Background(), approvalA)
	require.NoError(t, err)
	require.Equal(t, []string{"delete-a"}, dispatchedToolNames(t, approved),
		"the approval a human actually gave must dispatch the call they saw")

	// Turn 2: the provider reuses the same CallID for a different tool.
	callB := gateCallForApproval(t, h, loopID, loopID+":req:2:0", providerCallID,
		"delete-b", map[string]any{"target": "b"})
	require.Equal(t, callA.ID, callB.ID,
		"the fixture is only meaningful while the provider CallID repeats")
	require.NotEqual(t, callA.ExecutionID, callB.ExecutionID,
		"execution identity must not repeat across turns, or there is nothing to match on")

	// The earlier approval is replayed — a duplicate delivery, a UI retry, or a
	// replayed message. It names the same loop and the same provider CallID.
	replayed, err := h.HandleApprovalResponse(context.Background(), approvalA)
	require.NoError(t, err, "a replayed approval is a stale drop, never an error")
	require.True(t, replayed.staleDrop, "the replayed approval was not refused as stale")
	require.Empty(t, dispatchedToolNames(t, replayed),
		"the replayed approval dispatched a call nobody approved")

	entity, err := h.loopManager.GetLoop(loopID)
	require.NoError(t, err)
	require.Equal(t, agentic.LoopStateAwaitingApproval, entity.State,
		"the loop left awaiting_approval on a response that did not match its pending call")
	require.NotNil(t, entity.PendingApproval)
	require.Equal(t, callB.ExecutionID, entity.PendingApproval.ExecutionID,
		"the pending approval is still B's, so a real decision on B can still arrive")
}

// No CallID fallback. A response that carries no execution identity at all
// against a pending approval that has one is refused rather than matched on
// CallID — the fallback is the hole, so it must not exist.
//
// spec: agentic-loop / Tool execution has stable framework correlation
func TestApprovalWithoutExecutionIdentityIsRefusedAgainstAGatedCall(t *testing.T) {
	h := NewMessageHandler(DefaultConfig())
	loopID, err := h.loopManager.CreateLoop("task-no-exec", "general", "model")
	require.NoError(t, err)

	call := gateCallForApproval(t, h, loopID, loopID+":req:1:0", "provider-call",
		"delete-a", map[string]any{"target": "a"})

	result, err := h.HandleApprovalResponse(context.Background(), agentic.ApprovalResponse{
		LoopID:     loopID,
		CallID:     call.ID,
		Decision:   agentic.ApprovalDecisionApprove,
		ApprovedBy: "reviewer",
	})
	require.NoError(t, err)
	require.True(t, result.staleDrop, "a response with no execution identity was accepted")
	require.Empty(t, dispatchedToolNames(t, result))

	entity, err := h.loopManager.GetLoop(loopID)
	require.NoError(t, err)
	require.Equal(t, agentic.LoopStateAwaitingApproval, entity.State)
}

// The gated call's identity must reach whoever answers, or the matcher above
// refuses every response and a gated loop can never be resolved. Asserted on
// the published event rather than on the entity, because the event is what a
// product approval UI reads.
//
// spec: agentic-loop / Tool execution has stable framework correlation
func TestApprovalPendingEventCarriesTheGatedExecutionIdentity(t *testing.T) {
	h := NewMessageHandler(DefaultConfig())
	loopID, err := h.loopManager.CreateLoop("task-event", "general", "model")
	require.NoError(t, err)

	calls := []agentic.ToolCall{{ID: "provider-call", Name: "delete-a"}}
	require.NoError(t, stampToolExecutionCorrelation(loopID+":req:1:0", calls))
	require.NoError(t, h.dispatchToolCall(&HandlerResult{}, loopID, calls[0]))

	entity, err := h.loopManager.GetLoop(loopID)
	require.NoError(t, err)
	published, err := h.gateForApproval(loopID, &entity, agentic.ToolResult{
		CallID:      calls[0].ID,
		RequestID:   calls[0].RequestID,
		ExecutionID: calls[0].ExecutionID,
		CallOrdinal: calls[0].CallOrdinal,
		Error:       agentic.ApprovalRequiredPrefix + "review",
	})
	require.NoError(t, err)
	require.NotNil(t, published)

	var envelope struct {
		Payload agentic.ApprovalPendingEvent `json:"payload"`
	}
	require.NoError(t, json.Unmarshal(published.Data, &envelope))
	require.Equal(t, calls[0].ExecutionID, envelope.Payload.ExecutionID,
		"the approval event omits the identity its response must echo")
	require.Equal(t, calls[0].RequestID, envelope.Payload.RequestID)
	require.Equal(t, calls[0].ID, envelope.Payload.CallID,
		"provider CallID stays on the event: conversation semantics still need it")
}

// The timeout sweeper is the one responder that is not a human UI. It must echo
// the execution identity too, or its auto-reject is refused by the matcher and
// the gated loop waits forever — the exact wedge the timeout exists to prevent.
//
// spec: agentic-loop / Tool execution has stable framework correlation
func TestExpiredApprovalCandidateCarriesTheGatedExecutionIdentity(t *testing.T) {
	cfg := DefaultConfig()
	// A zero timeout is the wait-indefinitely policy and is skipped by the
	// sweeper, so the fixture has to declare one for there to be a candidate.
	cfg.ApprovalTimeoutStr = "1m"
	h := NewMessageHandler(cfg)
	loopID, err := h.loopManager.CreateLoop("task-timeout", "general", "model")
	require.NoError(t, err)

	call := gateCallForApproval(t, h, loopID, loopID+":req:1:0", "provider-call",
		"delete-a", map[string]any{"target": "a"})

	entity, err := h.loopManager.GetLoop(loopID)
	require.NoError(t, err)
	require.NotNil(t, entity.PendingApproval)
	deadline := entity.PendingApproval.RequestedAt.Add(entity.PendingApproval.Timeout)

	candidates := h.loopManager.SnapshotExpiredApprovals(deadline)
	require.Len(t, candidates, 1)
	require.Equal(t, call.ExecutionID, candidates[0].ExecutionID,
		"the sweeper cannot echo an identity the snapshot does not carry")
	require.Equal(t, call.RequestID, candidates[0].RequestID)

	// And the response it builds from that candidate resolves the approval.
	resolved, err := h.HandleApprovalResponse(context.Background(), agentic.ApprovalResponse{
		LoopID:      candidates[0].LoopID,
		CallID:      candidates[0].CallID,
		ExecutionID: candidates[0].ExecutionID,
		RequestID:   candidates[0].RequestID,
		Decision:    agentic.ApprovalDecisionReject,
		Reason:      "approval timed out",
		ApprovedBy:  approvalTimeoutSystemApprover,
	})
	require.NoError(t, err)
	require.False(t, resolved.staleDrop, "the sweeper's own auto-reject was refused as stale")
}
