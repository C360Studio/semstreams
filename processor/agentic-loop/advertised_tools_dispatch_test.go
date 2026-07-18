package agenticloop_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	agenticloop "github.com/c360studio/semstreams/processor/agentic-loop"
)

// advertisedTestTools is the narrow per-spawn tool set the gh#551 acceptance
// case advertises. Names deliberately mirror semdev's routing rules.
func advertisedTestTools() []agentic.ToolDefinition {
	return []agentic.ToolDefinition{
		{Name: "decide", Description: "terminal decision", Parameters: map[string]any{"type": "object"}},
		{Name: "query_entity", Description: "graph read", Parameters: map[string]any{"type": "object"}},
	}
}

// startLoopWithTools drives HandleTask with a per-spawn tool set and returns
// the loop ID. Mirrors gateCallWithMetadata's task shape but seeds task.Tools
// (cached at loop start via LoopManager.CacheTools) — the advertised set the
// dispatch stamp must derive from.
func startLoopWithTools(t *testing.T, handler *agenticloop.MessageHandler, taskID string, tools []agentic.ToolDefinition) string {
	t.Helper()
	taskResult, err := handler.HandleTask(context.Background(), agenticloop.TaskMessage{
		TaskID: taskID,
		Role:   "coordinator",
		Model:  "qwen-32b",
		Prompt: "route the work",
		Tools:  tools,
	})
	if err != nil {
		t.Fatalf("HandleTask: %v", err)
	}
	return taskResult.LoopID
}

// dispatchedToolExecute drives HandleModelResponse with a single tool call and
// returns the raw published tool.execute message (the main dispatch path).
func dispatchedToolExecute(t *testing.T, handler *agenticloop.MessageHandler, loopID string, tc agentic.ToolCall) string {
	t.Helper()
	result, err := handler.HandleModelResponse(context.Background(), loopID, agentic.AgentResponse{
		RequestID: "req-" + tc.ID,
		Status:    "tool_call",
		Message: agentic.ChatMessage{
			Role:      "assistant",
			ToolCalls: []agentic.ToolCall{tc},
		},
	})
	if err != nil {
		t.Fatalf("HandleModelResponse: %v", err)
	}
	for _, msg := range result.PublishedMessages {
		if strings.HasPrefix(msg.Subject, "tool.execute.") {
			return string(msg.Data)
		}
	}
	t.Fatalf("no tool.execute message published; got %d messages", len(result.PublishedMessages))
	return ""
}

// TestDispatch_StampsAdvertisedToolsFromCache is the gh#551 loop-side
// contract: dispatchToolCall stamps the names of the loop's cached tool
// definitions onto ToolCall.Metadata[agent.tools.advertised] so the executor
// can enforce the advertised set per loop.
func TestDispatch_StampsAdvertisedToolsFromCache(t *testing.T) {
	handler := agenticloop.NewMessageHandler(createTestConfig())
	loopID := startLoopWithTools(t, handler, "task-adv-stamp", advertisedTestTools())

	dispatched := dispatchedToolExecute(t, handler, loopID, agentic.ToolCall{
		ID: "call-adv-1", Name: "decide", Arguments: map[string]any{"action": "handoff"},
	})

	if !strings.Contains(dispatched, `"agent.tools.advertised":["decide","query_entity"]`) {
		t.Errorf("dispatched call missing advertised tool set stamp: %s", dispatched)
	}
}

// TestDispatch_OverwritesCallerAdvertisedTools proves the stamp is
// authoritative (OVERWRITE, like the RunID stamp): a caller/model-supplied
// value on the ToolCall must never defeat the control by widening the set.
func TestDispatch_OverwritesCallerAdvertisedTools(t *testing.T) {
	handler := agenticloop.NewMessageHandler(createTestConfig())
	loopID := startLoopWithTools(t, handler, "task-adv-overwrite", advertisedTestTools()[:1]) // only "decide"

	dispatched := dispatchedToolExecute(t, handler, loopID, agentic.ToolCall{
		ID:   "call-adv-2",
		Name: "decide",
		Metadata: map[string]any{
			agentic.MetadataKeyAdvertisedTools: []any{"decide", "create_change"},
		},
	})

	if !strings.Contains(dispatched, `"agent.tools.advertised":["decide"]`) {
		t.Errorf("dispatch did not overwrite caller-supplied advertised set: %s", dispatched)
	}
	if strings.Contains(dispatched, "create_change") {
		t.Errorf("caller-widened advertised set survived dispatch (fail-open): %s", dispatched)
	}
}

// TestDispatch_NoCachedTools_NoAdvertisedKey confirms back-compat: a loop
// spawned without an advertised tool set (no task.Tools, no discovery
// registry) dispatches with the key absent — the executor then applies only
// its global allowlist.
func TestDispatch_NoCachedTools_NoAdvertisedKey(t *testing.T) {
	handler := agenticloop.NewMessageHandler(createTestConfig())
	loopID := startLoopWithTools(t, handler, "task-adv-none", nil)

	dispatched := dispatchedToolExecute(t, handler, loopID, agentic.ToolCall{
		ID: "call-adv-3", Name: "bash", Arguments: map[string]any{"command": "ls"},
	})

	if strings.Contains(dispatched, agentic.MetadataKeyAdvertisedTools) {
		t.Errorf("loop with no cached tools must not stamp advertised set: %s", dispatched)
	}
}

// TestApprovedCall_CarriesAdvertisedTools covers the approval RE-DISPATCH
// path the way exec_policy_dispatch_test.go covers it for the enforced keys:
// the approval path rebuilds a bare ToolCall (no Metadata) and dispatches via
// the shared dispatchToolCall seam — losing the advertised set there would be
// a fail-open exactly when the operator gated the tool (ADR-067 §2 bug class).
func TestApprovedCall_CarriesAdvertisedTools(t *testing.T) {
	handler := agenticloop.NewMessageHandler(createTestConfig())
	loopID := startLoopWithTools(t, handler, "task-adv-approve", advertisedTestTools())

	// Drive to awaiting_approval on the call (mirrors gateCallWithMetadata).
	if _, err := handler.HandleModelResponse(context.Background(), loopID, agentic.AgentResponse{
		RequestID: "req-call-adv-4",
		Status:    "tool_call",
		Message: agentic.ChatMessage{
			Role: "assistant",
			ToolCalls: []agentic.ToolCall{
				{ID: "call-adv-4", Name: "decide", Arguments: map[string]any{"action": "handoff"}},
			},
		},
	}); err != nil {
		t.Fatalf("HandleModelResponse: %v", err)
	}
	gateRes, err := handler.HandleToolResult(context.Background(), loopID, agentic.ToolResult{
		CallID: "call-adv-4",
		Name:   "decide",
		Error:  agentic.ApprovalRequiredPrefix + "needs human review",
	})
	if err != nil {
		t.Fatalf("HandleToolResult (gate): %v", err)
	}
	if gateRes.State != agentic.LoopStateAwaitingApproval {
		t.Fatalf("loop state = %s, want awaiting_approval", gateRes.State)
	}

	result, err := handler.HandleApprovalResponse(context.Background(), agentic.ApprovalResponse{
		LoopID:     loopID,
		CallID:     "call-adv-4",
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
	if !strings.Contains(dispatched, `"agent.tools.advertised":["decide","query_entity"]`) {
		t.Errorf("re-dispatched approved call LOST advertised tool set (gh#551 fail-open): %s", dispatched)
	}
}
