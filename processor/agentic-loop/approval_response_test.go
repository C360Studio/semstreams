package agenticloop_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	agenticloop "github.com/c360studio/semstreams/processor/agentic-loop"
)

// gateLoopAtCall sets up a loop, drives it through one tool_call
// response, and feeds an approval_required rejection so it lands in
// LoopStateAwaitingApproval. Returns the loopID for the response
// tests to operate on.
func gateLoopAtCall(t *testing.T, handler *agenticloop.MessageHandler, callID, toolName string, args map[string]any) string {
	t.Helper()
	ctx := context.Background()

	taskResult, err := handler.HandleTask(ctx, agenticloop.TaskMessage{
		TaskID: "task-" + callID,
		Role:   "general",
		Model:  "qwen-32b",
		Prompt: "gate the loop",
	})
	if err != nil {
		t.Fatalf("HandleTask: %v", err)
	}
	loopID := taskResult.LoopID

	_, err = handler.HandleModelResponse(ctx, loopID, agentic.AgentResponse{
		RequestID: "req-" + callID,
		Status:    "tool_call",
		Message: agentic.ChatMessage{
			Role: "assistant",
			ToolCalls: []agentic.ToolCall{
				{ID: callID, Name: toolName, Arguments: args},
			},
		},
	})
	if err != nil {
		t.Fatalf("HandleModelResponse: %v", err)
	}

	gateRes, err := handler.HandleToolResult(ctx, loopID, agentic.ToolResult{
		CallID: callID,
		Name:   toolName,
		Error:  agentic.ApprovalRequiredPrefix + "needs human review",
	})
	if err != nil {
		t.Fatalf("HandleToolResult (gate): %v", err)
	}
	if gateRes.State != agentic.LoopStateAwaitingApproval {
		t.Fatalf("loop state = %s, want awaiting_approval", gateRes.State)
	}
	return loopID
}

func TestHandleApprovalResponse_Approve(t *testing.T) {
	handler := agenticloop.NewMessageHandler(createTestConfig())
	loopID := gateLoopAtCall(t, handler, "call-A", "delete_rule", map[string]any{"rule_id": "rule-42"})

	resp := agentic.ApprovalResponse{
		LoopID:     loopID,
		CallID:     "call-A",
		Decision:   agentic.ApprovalDecisionApprove,
		ApprovedBy: "alice@example.com",
		DecidedAt:  time.Now().UTC(),
	}
	result, err := handler.HandleApprovalResponse(context.Background(), resp)
	if err != nil {
		t.Fatalf("HandleApprovalResponse: %v", err)
	}

	// Loop should have resolved approval and resumed.
	entity, err := handler.GetLoop(loopID)
	if err != nil {
		t.Fatalf("GetLoop: %v", err)
	}
	if entity.State == agentic.LoopStateAwaitingApproval {
		t.Errorf("loop still awaiting approval after approve")
	}
	if entity.PendingApproval != nil {
		t.Errorf("PendingApproval not cleared: %+v", entity.PendingApproval)
	}

	// Re-dispatch should publish a tool.execute message carrying
	// ApprovedBy so the filter can bypass on the second pass.
	var sawDispatch bool
	for _, msg := range result.PublishedMessages {
		if strings.HasPrefix(msg.Subject, "tool.execute.") {
			sawDispatch = true
			if !strings.Contains(string(msg.Data), `"approved_by":"alice@example.com"`) {
				t.Errorf("dispatched ToolCall missing approved_by: %s", msg.Data)
			}
			if !strings.Contains(string(msg.Data), `"rule-42"`) {
				t.Errorf("dispatched ToolCall lost original arguments: %s", msg.Data)
			}
		}
	}
	if !sawDispatch {
		t.Errorf("approve path did not publish tool.execute; subjects: %v", subjectsOf(result.PublishedMessages))
	}
}

func TestHandleApprovalResponse_Modify(t *testing.T) {
	handler := agenticloop.NewMessageHandler(createTestConfig())
	loopID := gateLoopAtCall(t, handler, "call-M", "delete_rule", map[string]any{"rule_id": "rule-42"})

	resp := agentic.ApprovalResponse{
		LoopID:            loopID,
		CallID:            "call-M",
		Decision:          agentic.ApprovalDecisionModify,
		ModifiedArguments: map[string]any{"rule_id": "rule-safe"},
		ApprovedBy:        "alice@example.com",
		DecidedAt:         time.Now().UTC(),
	}
	result, err := handler.HandleApprovalResponse(context.Background(), resp)
	if err != nil {
		t.Fatalf("HandleApprovalResponse: %v", err)
	}

	var dispatched string
	for _, msg := range result.PublishedMessages {
		if strings.HasPrefix(msg.Subject, "tool.execute.") {
			dispatched = string(msg.Data)
		}
	}
	if dispatched == "" {
		t.Fatalf("modify path did not publish tool.execute")
	}
	if !strings.Contains(dispatched, `"rule-safe"`) {
		t.Errorf("modify path did not substitute arguments: %s", dispatched)
	}
	if strings.Contains(dispatched, `"rule-42"`) {
		t.Errorf("modify path leaked original arguments: %s", dispatched)
	}
}

func TestHandleApprovalResponse_Reject(t *testing.T) {
	handler := agenticloop.NewMessageHandler(createTestConfig())
	loopID := gateLoopAtCall(t, handler, "call-R", "delete_rule", nil)

	resp := agentic.ApprovalResponse{
		LoopID:    loopID,
		CallID:    "call-R",
		Decision:  agentic.ApprovalDecisionReject,
		Reason:    "policy violation",
		DecidedAt: time.Now().UTC(),
	}
	result, err := handler.HandleApprovalResponse(context.Background(), resp)
	if err != nil {
		t.Fatalf("HandleApprovalResponse: %v", err)
	}

	// Loop must NOT remain awaiting approval; the synthesised rejection
	// must NOT use the gating prefix (otherwise the gate re-fires);
	// downstream advancement should publish agent.request so the LLM
	// gets one round-trip with the rejection.
	entity, err := handler.GetLoop(loopID)
	if err != nil {
		t.Fatalf("GetLoop: %v", err)
	}
	if entity.State == agentic.LoopStateAwaitingApproval {
		t.Errorf("reject path left loop awaiting approval")
	}
	for _, msg := range result.PublishedMessages {
		if strings.HasPrefix(msg.Subject, "agent.approval_pending.") {
			t.Errorf("reject path re-emitted approval_pending: %s", msg.Subject)
		}
	}
	traj, err := handler.GetTrajectory(loopID)
	if err != nil {
		t.Fatalf("GetTrajectory: %v", err)
	}
	var sawRejection bool
	for _, step := range traj.Steps {
		if strings.HasPrefix(step.ErrorMessage, agentic.ApprovalRejectedPrefix) {
			sawRejection = true
			break
		}
	}
	if !sawRejection {
		t.Errorf("rejection prefix not found in trajectory steps")
	}
}

func TestHandleApprovalResponse_NotAwaiting(t *testing.T) {
	handler := agenticloop.NewMessageHandler(createTestConfig())
	ctx := context.Background()

	taskResult, err := handler.HandleTask(ctx, agenticloop.TaskMessage{
		TaskID: "task-stale",
		Role:   "general",
		Model:  "qwen-32b",
		Prompt: "no gating here",
	})
	if err != nil {
		t.Fatalf("HandleTask: %v", err)
	}

	// Loop is in exploring state, not awaiting approval. Stale or
	// duplicate response must not error or mutate state.
	resp := agentic.ApprovalResponse{
		LoopID:     taskResult.LoopID,
		CallID:     "ghost",
		Decision:   agentic.ApprovalDecisionApprove,
		ApprovedBy: "alice@example.com",
		DecidedAt:  time.Now().UTC(),
	}
	result, err := handler.HandleApprovalResponse(ctx, resp)
	if err != nil {
		t.Fatalf("stale response should not error: %v", err)
	}
	if len(result.PublishedMessages) != 0 {
		t.Errorf("stale response should not publish messages, got %d", len(result.PublishedMessages))
	}
}

func TestHandleApprovalResponse_CallIDMismatch(t *testing.T) {
	handler := agenticloop.NewMessageHandler(createTestConfig())
	loopID := gateLoopAtCall(t, handler, "call-real", "delete_rule", nil)

	resp := agentic.ApprovalResponse{
		LoopID:     loopID,
		CallID:     "call-other", // pinned to a different call
		Decision:   agentic.ApprovalDecisionApprove,
		ApprovedBy: "alice@example.com",
		DecidedAt:  time.Now().UTC(),
	}
	result, err := handler.HandleApprovalResponse(context.Background(), resp)
	if err != nil {
		t.Fatalf("mismatched call_id should not error: %v", err)
	}

	// Loop must remain awaiting approval; no dispatch should fire.
	entity, _ := handler.GetLoop(loopID)
	if entity.State != agentic.LoopStateAwaitingApproval {
		t.Errorf("call_id mismatch resolved approval: state=%s", entity.State)
	}
	for _, msg := range result.PublishedMessages {
		if strings.HasPrefix(msg.Subject, "tool.execute.") {
			t.Errorf("mismatched call_id triggered dispatch: %s", msg.Subject)
		}
	}
}
