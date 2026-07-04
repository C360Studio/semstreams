package agenticloop_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	agenticloop "github.com/c360studio/semstreams/processor/agentic-loop"
)

// gateCallWithMetadata drives a loop carrying task Metadata to
// awaiting_approval on a tool call, so an approval-response test can prove what
// the RE-DISPATCH (bare-ToolCall) path stamps. Mirrors gateLoopAtCall but seeds
// TaskMessage.Metadata (cached at loop start) — the enforcement policy lives there.
func gateCallWithMetadata(t *testing.T, handler *agenticloop.MessageHandler, callID, toolName string, args, meta map[string]any) string {
	t.Helper()
	ctx := context.Background()

	taskResult, err := handler.HandleTask(ctx, agenticloop.TaskMessage{
		TaskID:   "task-" + callID,
		Role:     "planner",
		Model:    "qwen-32b",
		Prompt:   "inspect the repo",
		Metadata: meta,
	})
	if err != nil {
		t.Fatalf("HandleTask: %v", err)
	}
	loopID := taskResult.LoopID

	if _, err = handler.HandleModelResponse(ctx, loopID, agentic.AgentResponse{
		RequestID: "req-" + callID,
		Status:    "tool_call",
		Message: agentic.ChatMessage{
			Role: "assistant",
			ToolCalls: []agentic.ToolCall{
				{ID: callID, Name: toolName, Arguments: args},
			},
		},
	}); err != nil {
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

// TestApprovedBashCall_CarriesFilesystemPolicy is the ADR-067 BLOCKING
// regression guard: an APPROVED (re-dispatched) bash call must still carry the
// read-only policy. The approval path rebuilds a bare ToolCall with no Metadata
// and dispatches via dispatchToolCall — before ADR-067 that path dropped
// filesystem_policy, silently disabling read-only enforcement exactly when the
// operator was most cautious (bash in the approval set). The fix stamps the
// policy at the shared dispatchToolCall seam, so it survives re-dispatch.
func TestApprovedBashCall_CarriesFilesystemPolicy(t *testing.T) {
	handler := agenticloop.NewMessageHandler(createTestConfig())
	loopID := gateCallWithMetadata(t, handler, "call-ro", "bash", map[string]any{"command": "ls"}, map[string]any{
		agentic.MetadataKeyFilesystemPolicy: agentic.FilesystemPolicyReadOnly,
		agentic.MetadataKeyScratchPaths:     []string{".probe"},
	})

	result, err := handler.HandleApprovalResponse(context.Background(), agentic.ApprovalResponse{
		LoopID:     loopID,
		CallID:     "call-ro",
		Decision:   agentic.ApprovalDecisionApprove,
		ApprovedBy: "alice@example.com",
		DecidedAt:  time.Now().UTC(),
	})
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
		t.Fatalf("approve path published no tool.execute message")
	}
	if !strings.Contains(dispatched, `"agent.exec.filesystem_policy":"read_only"`) {
		t.Errorf("re-dispatched bash call LOST filesystem_policy (ADR-067 BLOCKING regression): %s", dispatched)
	}
	if !strings.Contains(dispatched, `".probe"`) {
		t.Errorf("re-dispatched bash call lost scratch_paths: %s", dispatched)
	}
}

// TestApprovedCall_NoPolicyStampsNothing confirms back-compat: a loop with no
// read-only policy re-dispatches an approved call with no filesystem_policy key
// (the stamp is opt-in; absent keys stay absent).
func TestApprovedCall_NoPolicyStampsNothing(t *testing.T) {
	handler := agenticloop.NewMessageHandler(createTestConfig())
	loopID := gateCallWithMetadata(t, handler, "call-plain", "bash", map[string]any{"command": "ls"}, nil)

	result, err := handler.HandleApprovalResponse(context.Background(), agentic.ApprovalResponse{
		LoopID:     loopID,
		CallID:     "call-plain",
		Decision:   agentic.ApprovalDecisionApprove,
		ApprovedBy: "bob@example.com",
		DecidedAt:  time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("HandleApprovalResponse: %v", err)
	}

	for _, msg := range result.PublishedMessages {
		if strings.HasPrefix(msg.Subject, "tool.execute.") && strings.Contains(string(msg.Data), "filesystem_policy") {
			t.Errorf("no-policy loop must not stamp filesystem_policy: %s", msg.Data)
		}
	}
}

// TestApprovedDecideCall_CarriesActionAllowlist guards the latent bug ADR-067
// fixes as a bonus (same seam): before the fix, an APPROVED decide call lost its
// action_allowlist on re-dispatch, silently reopening the closed action
// vocabulary the allowlist exists to enforce. The dispatchToolCall stamp closes
// it for the whole DispatchEnforcedMetadataKeys set, decide included.
func TestApprovedDecideCall_CarriesActionAllowlist(t *testing.T) {
	handler := agenticloop.NewMessageHandler(createTestConfig())
	loopID := gateCallWithMetadata(t, handler, "call-dec", "decide",
		map[string]any{"action": "handoff"},
		map[string]any{agentic.MetadataKeyDecideActionAllowlist: []string{"handoff", "escalate"}})

	result, err := handler.HandleApprovalResponse(context.Background(), agentic.ApprovalResponse{
		LoopID:     loopID,
		CallID:     "call-dec",
		Decision:   agentic.ApprovalDecisionApprove,
		ApprovedBy: "carol@example.com",
		DecidedAt:  time.Now().UTC(),
	})
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
		t.Fatalf("approve path published no tool.execute message")
	}
	if !strings.Contains(dispatched, agentic.MetadataKeyDecideActionAllowlist) {
		t.Errorf("re-dispatched decide call LOST action_allowlist (latent gap ADR-067 fixes): %s", dispatched)
	}
}
