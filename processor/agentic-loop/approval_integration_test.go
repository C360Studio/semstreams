//go:build integration

package agenticloop_test

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
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
	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
)

type approvalFlowExecutor struct{ calls atomic.Int32 }

func (e *approvalFlowExecutor) Execute(_ context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	e.calls.Add(1)
	return agentic.ToolResult{CallID: call.ID, Name: call.Name, Content: "approved terminal result"}, nil
}

func (e *approvalFlowExecutor) ListTools() []agentic.ToolDefinition {
	return []agentic.ToolDefinition{{Name: "delete_rule", Parameters: map[string]any{"type": "object"}}}
}

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
				{Name: "agent.task", Config: component.JetStreamPort{StreamName: "AGENT", Subjects: []string{"agent.task.*"}}, Required: true},
				{Name: "agent.response", Config: component.JetStreamPort{StreamName: "AGENT", Subjects: []string{"agent.response.>"}}},
				{Name: "tool.result", Config: component.JetStreamPort{StreamName: "AGENT", Subjects: []string{"tool.result.>"}}},
				{Name: "agent.approval_response", Config: component.JetStreamPort{StreamName: "AGENT", Subjects: []string{"agent.approval_response.*"}}},
			},
			Outputs: []component.PortDefinition{
				{Name: "agent.request", Config: component.JetStreamPort{StreamName: "AGENT", Subjects: []string{"agent.request.*"}}},
				{Name: "tool.execute", Config: component.JetStreamPort{StreamName: "AGENT", Subjects: []string{"tool.execute.*"}}},
				{Name: "agent.approval_pending", Config: component.JetStreamPort{StreamName: "AGENT", Subjects: []string{"agent.approval_pending.*"}}},
				{Name: "agent.complete", Config: component.JetStreamPort{StreamName: "AGENT", Subjects: []string{"agent.complete.*"}}},
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
	defer lc.Stop(context.Background())

	toolsConfig := agentictools.DefaultConfig()
	toolsConfig.ApprovalRequired = []string{"delete_rule"}
	toolsConfig.ConsumerNameSuffix = "approval-tools-test"
	for index := range toolsConfig.Ports.Inputs {
		if toolsConfig.Ports.Inputs[index].Name == "tool.execute" {
			toolsConfig.Ports.Inputs[index].Config = component.JetStreamPort{
				StreamName: "AGENT", Subjects: []string{"tool.execute.>"},
			}
		}
	}
	for index := range toolsConfig.Ports.Outputs {
		if toolsConfig.Ports.Outputs[index].Name == "tool.result" {
			toolsConfig.Ports.Outputs[index].Config = component.JetStreamPort{
				StreamName: "AGENT", Subjects: []string{"tool.result.*"},
			}
		}
	}
	toolsRaw, err := json.Marshal(toolsConfig)
	require.NoError(t, err)
	toolsDiscoverable, err := agentictools.NewComponent(toolsRaw, deps)
	require.NoError(t, err)
	toolsComponent := toolsDiscoverable.(*agentictools.Component)
	toolExecutor := &approvalFlowExecutor{}
	require.NoError(t, toolsComponent.RegisterToolExecutor(toolExecutor))
	require.NoError(t, toolsComponent.Start(ctx))
	defer toolsComponent.Stop(context.Background())

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
	toolResults := make([]agentic.ToolResult, 0, 2)
	var resultsMu sync.Mutex
	_, err = natsClient.Subscribe(ctx, "tool.result.>", func(_ context.Context, msg *nats.Msg) {
		baseMsg, decErr := dec.Decode(msg.Data)
		if decErr != nil {
			return
		}
		toolResult, ok := baseMsg.Payload().(*agentic.ToolResult)
		if !ok || toolResult.CallID != callID {
			return
		}
		resultsMu.Lock()
		toolResults = append(toolResults, *toolResult)
		resultsMu.Unlock()
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

	// Step 4/5: the running agentic-tools component publishes the nonterminal
	// approval gate; the running loop consumes it and emits ApprovalPending.
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
	assert.Equal(t, int32(0), toolExecutor.calls.Load(), "initial gate must not execute")

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
	require.GreaterOrEqual(t, len(dispatchedCalls), 2)
	approved := dispatchedCalls[1]
	assert.Equal(t, callID, approved.ID, "re-dispatch should reuse the original call_id")
	assert.Equal(t, "delete_rule", approved.Name)
	assert.Equal(t, "alice@example.com", approved.ApprovedBy, "re-dispatch must carry the approver token")
	if v, ok := approved.Arguments["rule_id"].(string); !ok || v != "rule-42" {
		t.Errorf("re-dispatch lost original arguments: %v", approved.Arguments)
	}
	dispatchMu.Unlock()

	require.Eventually(t, func() bool {
		resultsMu.Lock()
		defer resultsMu.Unlock()
		return len(toolResults) >= 2 && toolResults[len(toolResults)-1].Content == "approved terminal result"
	}, 5*time.Second, 50*time.Millisecond, "approved same-ID re-dispatch should publish terminal result")
	assert.Equal(t, int32(1), toolExecutor.calls.Load(), "approved same-ID flow must execute exactly once")
	resultsMu.Lock()
	terminal := toolResults[len(toolResults)-1]
	resultsMu.Unlock()
	assert.Equal(t, callID, terminal.CallID)
	assert.Equal(t, "delete_rule", terminal.Name)
	assert.Equal(t, loopID, terminal.LoopID)
}

// TestIntegration_ApprovalTimeoutSweeper_PublishesWireResponse verifies that
// when a loop's approval timeout elapses the sweeper publishes an
// ApprovalResponse to agent.approval_response.<loopID> on the wire, so that
// external observers (dashboards, audit consumers, sister repos) see timeout
// auto-rejects symmetrically with human approvals.
//
// The test drives the loop to LoopStateAwaitingApproval with a very short
// approval timeout (500 ms), then subscribes to agent.approval_response.*
// and waits for the sweeper to fire and publish. Explicit channel
// synchronisation — no time.Sleep blocking on the critical assert path.
func TestIntegration_ApprovalTimeoutSweeper_PublishesWireResponse(t *testing.T) {
	natsClient := getSharedNATSClient(t)

	const loopID = "loop_approval_timeout_wire"
	const callID = "call_approval_timeout_wire"

	// Very short approval timeout so the sweeper fires quickly.
	const approvalTimeout = "500ms"

	config := agenticloop.Config{
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{Name: "agent.task", Config: component.JetStreamPort{StreamName: "AGENT", Subjects: []string{"agent.task.*"}}, Required: true},
				{Name: "agent.response", Config: component.JetStreamPort{StreamName: "AGENT", Subjects: []string{"agent.response.>"}}},
				{Name: "tool.result", Config: component.JetStreamPort{StreamName: "AGENT", Subjects: []string{"tool.result.>"}}},
				{Name: "agent.approval_response", Config: component.JetStreamPort{StreamName: "AGENT", Subjects: []string{"agent.approval_response.*"}}},
			},
			Outputs: []component.PortDefinition{
				{Name: "agent.request", Config: component.JetStreamPort{StreamName: "AGENT", Subjects: []string{"agent.request.*"}}},
				{Name: "tool.execute", Config: component.JetStreamPort{StreamName: "AGENT", Subjects: []string{"tool.execute.*"}}},
				{Name: "agent.approval_pending", Config: component.JetStreamPort{StreamName: "AGENT", Subjects: []string{"agent.approval_pending.*"}}},
				{Name: "agent.complete", Config: component.JetStreamPort{StreamName: "AGENT", Subjects: []string{"agent.complete.*"}}},
				{Name: "agent.failed", Config: component.JetStreamPort{StreamName: "AGENT", Subjects: []string{"agent.failed.*"}}},
			},
		},
		MaxIterations:      10,
		Timeout:            "60s",
		ApprovalTimeoutStr: approvalTimeout,
		StreamName:         "AGENT",
		ConsumerNameSuffix: "approval-timeout-wire-test",
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
	defer lc.Stop(context.Background())

	time.Sleep(200 * time.Millisecond)

	dec := payloadbuiltins.NewTestDecoder(t)

	// Subscribe to model requests so we can feed a response.
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

	// Subscribe to tool dispatches so we can synchronise before sending
	// the tool.result. Without this gate, tool.result arrives on its own
	// JetStream consumer concurrently with the agent.response consumer;
	// under CI load the result can be processed before the loop has
	// registered the tool call (TrackToolCall), causing the component to
	// log "No loop found for tool call" and drop the result — the loop
	// never reaches awaiting_approval and the sweeper has nothing to fire.
	var dispatchedCall bool
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
		if call.ID != callID {
			return
		}
		dispatchMu.Lock()
		dispatchedCall = true
		dispatchMu.Unlock()
	})
	require.NoError(t, err)

	// Subscribe to agent.approval_response.* to capture wire publishes.
	approvalRespCh := make(chan *agentic.ApprovalResponse, 4)
	_, err = natsClient.Subscribe(ctx, "agent.approval_response.>", func(_ context.Context, msg *nats.Msg) {
		baseMsg, decErr := dec.Decode(msg.Data)
		if decErr != nil {
			return
		}
		resp, ok := baseMsg.Payload().(*agentic.ApprovalResponse)
		if !ok {
			return
		}
		// Filter to only the loop under test.
		if resp.LoopID != loopID {
			return
		}
		approvalRespCh <- resp
	})
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	// Step 1: publish task.
	task := &agentic.TaskMessage{
		LoopID: loopID,
		TaskID: "task-approval-timeout-wire",
		Role:   "general",
		Model:  "test-model",
		Prompt: "Delete a rule",
	}
	publishTaskMessage(t, natsClient, "agent.task.approval-timeout", task)

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

	// Wait for the loop to dispatch the tool call before publishing the
	// tool result. The loop registers the call-to-loop routing table entry
	// (TrackToolCall) before emitting tool.execute; without this gate the
	// tool.result can arrive on a parallel JetStream consumer before
	// registration completes, yielding "No loop found for tool call" and
	// a dropped result that prevents the loop from ever entering
	// awaiting_approval.
	require.Eventually(t, func() bool {
		dispatchMu.Lock()
		defer dispatchMu.Unlock()
		return dispatchedCall
	}, 5*time.Second, 50*time.Millisecond, "loop should dispatch tool call to tool.execute before tool.result is sent")

	// Step 3: simulate agentic-tools approval filter rejecting the call.
	gateResult := &agentic.ToolResult{
		CallID:    callID,
		Name:      "delete_rule",
		ErrorKind: agentic.ToolErrorPermission,
		Error:     agentic.ApprovalRequiredPrefix + "Tool 'delete_rule' requires human approval",
	}
	publishToolResultMessage(t, natsClient, "tool.result."+callID, gateResult)

	// Step 4: the sweeper fires after the approval timeout (~500ms) and publishes
	// the ApprovalResponse to agent.approval_response.<loopID>. Wait up to 15s
	// to give the sweeper (5s interval) plus timeout (500ms) plus startup margin.
	select {
	case wireResp := <-approvalRespCh:
		assert.Equal(t, loopID, wireResp.LoopID, "wire response must carry correct loop_id")
		assert.Equal(t, callID, wireResp.CallID, "wire response must carry correct call_id")
		assert.Equal(t, agentic.ApprovalDecisionReject, wireResp.Decision, "timeout auto-reject must use reject decision")
		assert.Contains(t, wireResp.Reason, "timed out", "wire response reason must mention timeout")
		assert.Equal(t, "system:approval-timeout", wireResp.ApprovedBy, "wire response must carry timeout sentinel in approved_by")
	case <-ctx.Done():
		t.Fatal("timed out waiting for sweeper to publish agent.approval_response wire message")
	}
}
