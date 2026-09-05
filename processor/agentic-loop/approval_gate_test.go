package agenticloop_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	agenticloop "github.com/c360studio/semstreams/processor/agentic-loop"
)

// TestHandleToolResult_ApprovalGated verifies that when a tool result
// arrives with an "approval_required: ..." error, the loop transitions
// to LoopStateAwaitingApproval, emits ApprovalPendingEvent, and stops
// short of issuing the next agent.request — the gap that beta.18 left
// open and beta.19 closes.
func TestHandleToolResult_ApprovalGated(t *testing.T) {
	handler := agenticloop.NewMessageHandler(createTestConfig())
	ctx := context.Background()

	taskResult, err := handler.HandleTask(ctx, agenticloop.TaskMessage{
		TaskID: "task-approval",
		Role:   "general",
		Model:  "qwen-32b",
		Prompt: "Delete a rule",
	})
	if err != nil {
		t.Fatalf("HandleTask: %v", err)
	}
	loopID := taskResult.LoopID

	// Drive the loop to dispatch a tool that the filter will reject.
	toolResponse := agentic.AgentResponse{
		RequestID: "req-001",
		Status:    "tool_call",
		Message: agentic.ChatMessage{
			Role: "assistant",
			ToolCalls: []agentic.ToolCall{
				{
					ID:        "call-001",
					Name:      "delete_rule",
					Arguments: map[string]any{"rule_id": "rule-42"},
				},
			},
		},
	}
	dispatchResult, err := handler.HandleModelResponse(ctx, loopID, toolResponse)
	if err != nil {
		t.Fatalf("HandleModelResponse: %v", err)
	}
	dispatched := dispatchedToolCallFromResult(t, dispatchResult)

	// Simulate the filter-side rejection: tool result arrives with the
	// approval_required prefix.
	toolResult := agentic.ToolResult{
		CallID:      dispatched.ID,
		Name:        dispatched.Name,
		ErrorKind:   agentic.ToolErrorPermission,
		Error:       agentic.ApprovalRequiredPrefix + "Tool 'delete_rule' requires human approval before execution",
		RequestID:   dispatched.RequestID,
		ExecutionID: dispatched.ExecutionID,
		CallOrdinal: dispatched.CallOrdinal,
	}
	result, err := handler.HandleToolResult(ctx, loopID, toolResult)
	if err != nil {
		t.Fatalf("HandleToolResult: %v", err)
	}

	if result.State != agentic.LoopStateAwaitingApproval {
		t.Errorf("State = %s, want %s", result.State, agentic.LoopStateAwaitingApproval)
	}

	// Persisted state must agree with HandlerResult.State.
	entity, err := handler.GetLoop(loopID)
	if err != nil {
		t.Fatalf("GetLoop: %v", err)
	}
	if entity.State != agentic.LoopStateAwaitingApproval {
		t.Errorf("entity.State = %s, want awaiting_approval", entity.State)
	}
	if entity.PendingApproval == nil {
		t.Fatalf("PendingApproval not set on entity")
	}
	if entity.PendingApproval.CallID != "call-001" || entity.PendingApproval.ToolName != "delete_rule" {
		t.Errorf("PendingApproval mismatch: %+v", entity.PendingApproval)
	}
	if v, ok := entity.PendingApproval.Arguments["rule_id"].(string); !ok || v != "rule-42" {
		t.Errorf("Arguments[rule_id] = %v, want rule-42", entity.PendingApproval.Arguments["rule_id"])
	}

	// An ApprovalPendingEvent must have been published; no agent.request
	// should be emitted while we wait.
	var sawPending bool
	for _, msg := range result.PublishedMessages {
		if strings.HasPrefix(msg.Subject, "agent.approval_pending.") {
			sawPending = true
			var envelope struct {
				Type struct {
					Domain   string `json:"domain"`
					Category string `json:"category"`
					Version  string `json:"version"`
				} `json:"type"`
			}
			if err := json.Unmarshal(msg.Data, &envelope); err != nil {
				t.Fatalf("decode envelope: %v", err)
			}
			if envelope.Type.Category != agentic.CategoryApprovalPending {
				t.Errorf("envelope type.category = %q, want %q", envelope.Type.Category, agentic.CategoryApprovalPending)
			}
		}
		if strings.HasPrefix(msg.Subject, "agent.request.") {
			t.Errorf("loop emitted agent.request while awaiting approval: %s", msg.Subject)
		}
	}
	if !sawPending {
		t.Errorf("no agent.approval_pending message published; got subjects: %v", subjectsOf(result.PublishedMessages))
	}

	// Trajectory must still capture the rejection so audits see the gating.
	traj, err := handler.GetTrajectory(loopID)
	if err != nil {
		t.Fatalf("GetTrajectory: %v", err)
	}
	var sawToolStep bool
	for _, s := range traj.Steps {
		if s.StepType == "tool_call" {
			sawToolStep = true
			break
		}
	}
	if !sawToolStep {
		t.Errorf("trajectory missing tool_call step for gated rejection")
	}
}

// TestHandleToolResult_AwaitingApprovalAbsorbsSiblings verifies that
// once the loop is awaiting approval, additional tool results from
// the same batch land in storage but do not advance the loop or fire
// the next agent.request — the resume path will drain them later.
func TestHandleToolResult_AwaitingApprovalAbsorbsSiblings(t *testing.T) {
	handler := agenticloop.NewMessageHandler(createTestConfig())
	ctx := context.Background()

	taskResult, err := handler.HandleTask(ctx, agenticloop.TaskMessage{
		TaskID: "task-approval-multi",
		Role:   "general",
		Model:  "qwen-32b",
		Prompt: "Two-tool batch",
	})
	if err != nil {
		t.Fatalf("HandleTask: %v", err)
	}
	loopID := taskResult.LoopID

	// Two tool calls in one batch.
	toolResponse := agentic.AgentResponse{
		RequestID: "req-002",
		Status:    "tool_call",
		Message: agentic.ChatMessage{
			Role: "assistant",
			ToolCalls: []agentic.ToolCall{
				{ID: "call-A", Name: "delete_rule"},
				{ID: "call-B", Name: "graph_query"},
			},
		},
	}
	if _, err := handler.HandleModelResponse(ctx, loopID, toolResponse); err != nil {
		t.Fatalf("HandleModelResponse: %v", err)
	}

	// First arrival: gated rejection.
	gateRes, err := handler.HandleToolResult(ctx, loopID, agentic.ToolResult{
		CallID: "call-A",
		Name:   "delete_rule",
		Error:  agentic.ApprovalRequiredPrefix + "needs approval",
	})
	if err != nil {
		t.Fatalf("first HandleToolResult: %v", err)
	}
	if gateRes.State != agentic.LoopStateAwaitingApproval {
		t.Fatalf("first call state = %s, want awaiting_approval", gateRes.State)
	}

	// Second arrival: a normal result for the sibling. Must NOT trigger
	// the next agent.request even though the model would otherwise
	// advance.
	siblingRes, err := handler.HandleToolResult(ctx, loopID, agentic.ToolResult{
		CallID:  "call-B",
		Name:    "graph_query",
		Content: "graph data",
	})
	if err != nil {
		t.Fatalf("sibling HandleToolResult: %v", err)
	}
	for _, msg := range siblingRes.PublishedMessages {
		if strings.HasPrefix(msg.Subject, "agent.request.") {
			t.Errorf("sibling tool result triggered agent.request while awaiting approval: %s", msg.Subject)
		}
	}

	// Loop entity remains in awaiting_approval; PendingApproval still
	// pinned to call-A.
	entity, err := handler.GetLoop(loopID)
	if err != nil {
		t.Fatalf("GetLoop: %v", err)
	}
	if entity.State != agentic.LoopStateAwaitingApproval {
		t.Errorf("entity.State = %s, want awaiting_approval", entity.State)
	}
	if entity.PendingApproval == nil || entity.PendingApproval.CallID != "call-A" {
		t.Errorf("PendingApproval drifted: %+v", entity.PendingApproval)
	}
}

func subjectsOf(msgs []agenticloop.PublishedMessage) []string {
	out := make([]string, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, m.Subject)
	}
	return out
}
