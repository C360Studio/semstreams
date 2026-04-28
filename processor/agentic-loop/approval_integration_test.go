//go:build integration

package agenticloop_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/payloadbuiltins"
	agenticloop "github.com/c360studio/semstreams/processor/agentic-loop"
)

// TestIntegration_ApprovalFlow_Approve exercises the full
// human-in-the-loop round trip end-to-end through a running
// agentic-loop component:
//
//  1. Task → first agent.request published
//  2. Model response with tool_call → tool.execute published
//  3. Tool rejection with approval_required: prefix arrives
//  4. Loop pauses; agent.approval_pending event published; entity
//     state in AGENT_LOOPS KV transitions to awaiting_approval with
//     PendingApproval populated
//  5. ApprovalResponse with decision=approve arrives
//  6. Loop re-dispatches the original tool.execute, this time with
//     ApprovedBy stamped on the ToolCall envelope
//
// This is the integration counterpart to the unit-level
// TestHandleToolResult_ApprovalGated and TestHandleApprovalResponse_*
// tests — those exercise the handler logic directly; this exercises
// the NATS subscribe/publish wiring + KV persistence on top.
func TestIntegration_ApprovalFlow_Approve(t *testing.T) {
	natsClient := getSharedNATSClient(t)

	const loopID = "loop_approval_approve"
	const callID = "call_approval_approve"

	config := agenticloop.Config{
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{Name: "agent.task", Type: "jetstream", Subject: "agent.task.*", StreamName: "AGENT", Required: true},
				{Name: "agent.response", Type: "jetstream", Subject: "agent.response.>", StreamName: "AGENT"},
				{Name: "tool.result", Type: "jetstream", Subject: "tool.result.>", StreamName: "AGENT"},
				{Name: "agent.approval_response", Type: "jetstream", Subject: "agent.approval_response.*", StreamName: "AGENT"},
			},
			Outputs: []component.PortDefinition{
				{Name: "agent.request", Type: "jetstream", Subject: "agent.request.*", StreamName: "AGENT"},
				{Name: "tool.execute", Type: "jetstream", Subject: "tool.execute.*", StreamName: "AGENT"},
				{Name: "agent.approval_pending", Type: "jetstream", Subject: "agent.approval_pending.*", StreamName: "AGENT"},
				{Name: "agent.complete", Type: "jetstream", Subject: "agent.complete.*", StreamName: "AGENT"},
			},
		},
		MaxIterations:      10,
		Timeout:            "60s",
		StreamName:         "AGENT",
		ConsumerNameSuffix: "approval-approve-test",
		LoopsBucket:        "AGENT_LOOPS",
	}

	rawConfig, err := json.Marshal(config)
	require.NoError(t, err)

	deps := component.Dependencies{
		NATSClient:      natsClient,
		PayloadRegistry: payloadbuiltins.NewTestRegistry(t),
	}

	comp, err := agenticloop.NewComponent(rawConfig, deps)
	require.NoError(t, err)

	lc := comp.(component.LifecycleComponent)
	require.NoError(t, lc.Initialize())

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	require.NoError(t, lc.Start(ctx))
	defer lc.Stop(5 * time.Second)

	time.Sleep(200 * time.Millisecond)

	dec := payloadbuiltins.NewTestDecoder(t)

	// Subscribe to model requests so we can grab the request_id the
	// loop is expecting a response on.
	var firstRequestID string
	var requestMu sync.Mutex
	_, err = natsClient.Subscribe(ctx, "agent.request.>", func(_ context.Context, msg *nats.Msg) {
		baseMsg, decErr := dec.Decode(msg.Data)
		if decErr != nil {
			return
		}
		req, ok := baseMsg.Payload().(*agentic.AgentRequest)
		if !ok {
			return
		}
		requestMu.Lock()
		if firstRequestID == "" {
			firstRequestID = req.RequestID
		}
		requestMu.Unlock()
	})
	require.NoError(t, err)

	// Subscribe to tool dispatches. We expect two: the initial call
	// (no ApprovedBy) and the re-dispatch (with ApprovedBy set).
	dispatchedCalls := make([]agentic.ToolCall, 0, 2)
	var dispatchMu sync.Mutex
	_, err = natsClient.Subscribe(ctx, "tool.execute.>", func(_ context.Context, msg *nats.Msg) {
		baseMsg, decErr := dec.Decode(msg.Data)
		if decErr != nil {
			return
		}
		call, ok := baseMsg.Payload().(*agentic.ToolCall)
		if !ok {
			return
		}
		dispatchMu.Lock()
		dispatchedCalls = append(dispatchedCalls, *call)
		dispatchMu.Unlock()
	})
	require.NoError(t, err)

	// Subscribe to approval pending events.
	pendingEvents := make([]agentic.ApprovalPendingEvent, 0)
	var pendingMu sync.Mutex
	_, err = natsClient.Subscribe(ctx, "agent.approval_pending.>", func(_ context.Context, msg *nats.Msg) {
		baseMsg, decErr := dec.Decode(msg.Data)
		if decErr != nil {
			return
		}
		ev, ok := baseMsg.Payload().(*agentic.ApprovalPendingEvent)
		if !ok {
			return
		}
		pendingMu.Lock()
		pendingEvents = append(pendingEvents, *ev)
		pendingMu.Unlock()
	})
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	// Step 1: publish task.
	task := &agentic.TaskMessage{
		LoopID: loopID,
		TaskID: "task-approval-approve",
		Role:   "general",
		Model:  "test-model",
		Prompt: "Delete a rule",
	}
	publishTaskMessage(t, natsClient, "agent.task.approval", task)

	require.Eventually(t, func() bool {
		requestMu.Lock()
		defer requestMu.Unlock()
		return firstRequestID != ""
	}, 5*time.Second, 50*time.Millisecond, "loop should publish first agent.request")

	requestMu.Lock()
	reqID := firstRequestID
	requestMu.Unlock()

	// Step 2: model response with tool_call.
	resp := &agentic.AgentResponse{
		RequestID: reqID,
		Status:    "tool_call",
		Message: agentic.ChatMessage{
			Role: "assistant",
			ToolCalls: []agentic.ToolCall{
				{ID: callID, Name: "delete_rule", Arguments: map[string]any{"rule_id": "rule-42"}},
			},
		},
	}
	publishResponseMessage(t, natsClient, "agent.response."+reqID, resp)

	// Step 3: wait for the loop to dispatch the tool call.
	require.Eventually(t, func() bool {
		dispatchMu.Lock()
		defer dispatchMu.Unlock()
		return len(dispatchedCalls) >= 1
	}, 5*time.Second, 50*time.Millisecond, "loop should dispatch the original tool call")

	// Step 4: simulate the agentic-tools approval filter rejecting
	// the call by publishing a tool result with the approval_required
	// prefix.
	gateResult := &agentic.ToolResult{
		CallID:    callID,
		Name:      "delete_rule",
		ErrorKind: agentic.ToolErrorPermission,
		Error:     agentic.ApprovalRequiredPrefix + "Tool 'delete_rule' requires human approval before execution",
	}
	publishToolResultMessage(t, natsClient, "tool.result."+callID, gateResult)

	// Step 5: wait for ApprovalPendingEvent.
	require.Eventually(t, func() bool {
		pendingMu.Lock()
		defer pendingMu.Unlock()
		return len(pendingEvents) >= 1
	}, 5*time.Second, 50*time.Millisecond, "loop should publish agent.approval_pending")

	pendingMu.Lock()
	pe := pendingEvents[0]
	pendingMu.Unlock()
	assert.Equal(t, loopID, pe.LoopID)
	assert.Equal(t, callID, pe.CallID)
	assert.Equal(t, "delete_rule", pe.ToolName)
	assert.Contains(t, pe.Reason, "approval_required:")

	// Step 6: publish ApprovalResponse with decision=approve.
	approval := &agentic.ApprovalResponse{
		LoopID:     loopID,
		CallID:     callID,
		Decision:   agentic.ApprovalDecisionApprove,
		ApprovedBy: "alice@example.com",
		DecidedAt:  time.Now().UTC(),
	}
	envelope := message.NewBaseMessage(approval.Schema(), approval, "integration-test")
	envelopeData, err := json.Marshal(envelope)
	require.NoError(t, err)
	require.NoError(t, natsClient.PublishToStream(ctx, "agent.approval_response."+loopID, envelopeData))

	// Step 7: wait for the re-dispatched tool call carrying
	// ApprovedBy. We need to see TWO dispatched calls (the original
	// + the re-dispatch).
	require.Eventually(t, func() bool {
		dispatchMu.Lock()
		defer dispatchMu.Unlock()
		return len(dispatchedCalls) >= 2
	}, 5*time.Second, 50*time.Millisecond, "loop should re-dispatch the approved tool call")

	dispatchMu.Lock()
	defer dispatchMu.Unlock()
	require.GreaterOrEqual(t, len(dispatchedCalls), 2)
	approved := dispatchedCalls[1]
	assert.Equal(t, callID, approved.ID, "re-dispatch should reuse the original call_id")
	assert.Equal(t, "delete_rule", approved.Name)
	assert.Equal(t, "alice@example.com", approved.ApprovedBy, "re-dispatch must carry the approver token")
	if v, ok := approved.Arguments["rule_id"].(string); !ok || v != "rule-42" {
		t.Errorf("re-dispatch lost original arguments: %v", approved.Arguments)
	}
}
