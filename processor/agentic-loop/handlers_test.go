package agenticloop_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	agenticloop "github.com/c360studio/semstreams/processor/agentic-loop"
	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
)

// testToolExecutor is a mock tool executor for testing tool injection
type testToolExecutor struct {
	tools []agentic.ToolDefinition
}

func (e *testToolExecutor) Execute(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	return agentic.ToolResult{CallID: call.ID, Content: "test result"}, nil
}

func (e *testToolExecutor) ListTools() []agentic.ToolDefinition {
	return e.tools
}

// newTestToolRegistry builds a per-test ExecutorRegistry preloaded with
// the canonical "test_tool" stub. Each test that exercises tool discovery
// calls this and wires the registry via handler.SetToolRegistry.
func newTestToolRegistry(t *testing.T) *agentictools.ExecutorRegistry {
	t.Helper()
	executor := &testToolExecutor{
		tools: []agentic.ToolDefinition{
			{
				Name:        "test_tool",
				Description: "A test tool for unit tests",
				Parameters:  map[string]any{"type": "object", "properties": map[string]any{}},
			},
		},
	}
	reg := agentictools.NewExecutorRegistry()
	if err := reg.RegisterTool("test_tool", executor); err != nil {
		t.Fatalf("register test_tool: %v", err)
	}
	return reg
}

func TestHandleTask_CreatesLoop(t *testing.T) {
	handler := agenticloop.NewMessageHandler(createTestConfig())

	taskMsg := agenticloop.TaskMessage{
		TaskID: "task-001",
		Role:   "general",
		Model:  "qwen-32b",
		Prompt: "Analyze this system",
	}

	ctx := context.Background()
	result, err := handler.HandleTask(ctx, taskMsg)
	if err != nil {
		t.Fatalf("HandleTask() error = %v", err)
	}

	if result.LoopID == "" {
		t.Error("HandleTask() should return loop ID")
	}

	// Verify loop was created with correct initial state
	if result.State != agentic.LoopStateExploring {
		t.Errorf("Initial state = %s, want exploring", result.State)
	}

	// Verify agent request was published
	if len(result.PublishedMessages) == 0 {
		t.Error("HandleTask() should publish initial agent.request message")
	}

	found := false
	for _, msg := range result.PublishedMessages {
		if msg.Subject == "agent.request."+result.LoopID {
			found = true

			// Extract request from BaseMessage envelope
			var envelope map[string]any
			if err := json.Unmarshal(msg.Data, &envelope); err != nil {
				t.Fatalf("Failed to unmarshal envelope: %v", err)
			}
			payload, ok := envelope["payload"].(map[string]any)
			if !ok {
				t.Fatalf("Expected payload in BaseMessage envelope")
			}

			// Verify request content
			if payload["loop_id"] != result.LoopID {
				t.Errorf("Request.LoopID = %v, want %s", payload["loop_id"], result.LoopID)
			}
			if payload["role"] != taskMsg.Role {
				t.Errorf("Request.Role = %v, want %s", payload["role"], taskMsg.Role)
			}
			if payload["model"] != taskMsg.Model {
				t.Errorf("Request.Model = %v, want %s", payload["model"], taskMsg.Model)
			}
			messages, ok := payload["messages"].([]any)
			if !ok || len(messages) == 0 {
				t.Error("Request.Messages should not be empty")
			}
			break
		}
	}

	if !found {
		t.Error("HandleTask() should publish to agent.request subject")
	}

	// Verify trajectory step was recorded
	if len(result.TrajectorySteps) == 0 {
		t.Error("HandleTask() should record trajectory step")
	}
}

func TestHandleTask_MultipleRoles(t *testing.T) {
	handler := agenticloop.NewMessageHandler(createTestConfig())

	tests := []struct {
		name  string
		role  string
		model string
	}{
		{
			name:  "general role",
			role:  "general",
			model: "qwen-32b",
		},
		{
			name:  "architect role",
			role:  "architect",
			model: "deepseek-16b",
		},
		{
			name:  "editor role",
			role:  "editor",
			model: "qwen-32b",
		},
	}

	ctx := context.Background()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			taskMsg := agenticloop.TaskMessage{
				TaskID: "task-" + tt.role,
				Role:   tt.role,
				Model:  tt.model,
				Prompt: "Test prompt",
			}

			result, err := handler.HandleTask(ctx, taskMsg)
			if err != nil {
				t.Fatalf("HandleTask() error = %v", err)
			}

			if result.LoopID == "" {
				t.Error("HandleTask() should return loop ID")
			}

			// Each role should create a valid loop
			if result.State != agentic.LoopStateExploring {
				t.Errorf("Initial state = %s, want exploring", result.State)
			}
		})
	}
}

func TestHandleModelResponse_ToolCall(t *testing.T) {
	handler := agenticloop.NewMessageHandler(createTestConfig())

	// Create a loop first
	ctx := context.Background()
	taskResult, err := handler.HandleTask(ctx, agenticloop.TaskMessage{
		TaskID: "task-001",
		Role:   "general",
		Model:  "qwen-32b",
		Prompt: "Test",
	})
	if err != nil {
		t.Fatalf("HandleTask() error = %v", err)
	}

	loopID := taskResult.LoopID

	// Model response with tool calls
	response := agentic.AgentResponse{
		RequestID: "req-001",
		Status:    "tool_call",
		Message: agentic.ChatMessage{
			Role: "assistant",
			ToolCalls: []agentic.ToolCall{
				{
					ID:   "call-001",
					Name: "graph_query",
					Arguments: map[string]any{
						"query": "SELECT * FROM entities",
					},
				},
				{
					ID:   "call-002",
					Name: "file_read",
					Arguments: map[string]any{
						"path": "/tmp/test.txt",
					},
				},
			},
		},
		TokenUsage: agentic.TokenUsage{
			PromptTokens:     100,
			CompletionTokens: 50,
		},
	}

	result, err := handler.HandleModelResponse(ctx, loopID, response)
	if err != nil {
		t.Fatalf("HandleModelResponse() error = %v", err)
	}

	// Serial dispatch: only the first tool call should be published
	toolExecuteCount := 0
	for _, msg := range result.PublishedMessages {
		if containsIgnoreCase(msg.Subject, "tool.execute") {
			toolExecuteCount++
		}
	}

	if toolExecuteCount != 1 {
		t.Errorf("Should publish 1 tool.execute message (serial dispatch), got %d", toolExecuteCount)
	}

	// Only the first tool is pending; second is queued
	if len(result.PendingTools) != 1 {
		t.Errorf("PendingTools count = %d, want 1", len(result.PendingTools))
	}

	// Should record trajectory step
	if len(result.TrajectorySteps) == 0 {
		t.Error("Should record trajectory step for tool_call")
	}
}

func TestHandleModelResponse_Complete_General(t *testing.T) {
	handler := agenticloop.NewMessageHandler(createTestConfig())

	// Create a general role loop
	ctx := context.Background()
	taskResult, err := handler.HandleTask(ctx, agenticloop.TaskMessage{
		TaskID: "task-001",
		Role:   "general",
		Model:  "qwen-32b",
		Prompt: "Test",
	})
	if err != nil {
		t.Fatalf("HandleTask() error = %v", err)
	}

	loopID := taskResult.LoopID

	// Model response with completion
	response := agentic.AgentResponse{
		RequestID: "req-001",
		Status:    "complete",
		Message: agentic.ChatMessage{
			Role:    "assistant",
			Content: "Task completed successfully",
		},
		TokenUsage: agentic.TokenUsage{
			PromptTokens:     100,
			CompletionTokens: 50,
		},
	}

	result, err := handler.HandleModelResponse(ctx, loopID, response)
	if err != nil {
		t.Fatalf("HandleModelResponse() error = %v", err)
	}

	// Should mark loop as complete
	if result.State != agentic.LoopStateComplete {
		t.Errorf("State = %s, want complete", result.State)
	}

	// Should publish agent.complete
	found := false
	for _, msg := range result.PublishedMessages {
		if containsIgnoreCase(msg.Subject, "agent.complete") {
			found = true
			break
		}
	}
	if !found {
		t.Error("Should publish agent.complete message")
	}

	// Should record trajectory completion
	if len(result.TrajectorySteps) == 0 {
		t.Error("Should record trajectory step for completion")
	}

	// Should have completion state with general role
	if result.CompletionState == nil {
		t.Error("CompletionState should not be nil on completion")
	}
	if result.CompletionState.Role != "general" {
		t.Errorf("CompletionState.Role = %v, want general", result.CompletionState.Role)
	}
}

func TestHandleModelResponse_Complete_Architect(t *testing.T) {
	handler := agenticloop.NewMessageHandler(createTestConfig())

	// Create an architect role loop
	ctx := context.Background()
	taskResult, err := handler.HandleTask(ctx, agenticloop.TaskMessage{
		TaskID: "task-001",
		Role:   "architect",
		Model:  "qwen-32b",
		Prompt: "Design system",
	})
	if err != nil {
		t.Fatalf("HandleTask() error = %v", err)
	}

	loopID := taskResult.LoopID
	architectOutput := "Architecture design complete"

	// Architect completion response
	response := agentic.AgentResponse{
		RequestID: "req-001",
		Status:    "complete",
		Message: agentic.ChatMessage{
			Role:    "assistant",
			Content: architectOutput,
		},
		TokenUsage: agentic.TokenUsage{
			PromptTokens:     200,
			CompletionTokens: 100,
		},
	}

	result, err := handler.HandleModelResponse(ctx, loopID, response)
	if err != nil {
		t.Fatalf("HandleModelResponse() error = %v", err)
	}

	// Should mark architect loop as complete
	if result.State != agentic.LoopStateComplete {
		t.Errorf("Architect state = %s, want complete", result.State)
	}

	// Should have enriched completion state for rules engine
	// (Rules engine handles spawning editor - not the handler directly)
	if result.CompletionState == nil {
		t.Fatal("CompletionState should not be nil")
	}
	if result.CompletionState.Role != "architect" {
		t.Errorf("CompletionState.Role = %v, want architect", result.CompletionState.Role)
	}
	if result.CompletionState.Outcome != agentic.OutcomeSuccess {
		t.Errorf("CompletionState.Outcome = %v, want %s", result.CompletionState.Outcome, agentic.OutcomeSuccess)
	}
	if result.CompletionState.Result != architectOutput {
		t.Errorf("CompletionState.Result = %v, want %s", result.CompletionState.Result, architectOutput)
	}
	if result.CompletionState.TaskID != "task-001" {
		t.Errorf("CompletionState.TaskID = %v, want task-001", result.CompletionState.TaskID)
	}
	if result.CompletionState.Model != "qwen-32b" {
		t.Errorf("CompletionState.Model = %v, want qwen-32b", result.CompletionState.Model)
	}

	// Should publish agent.complete (rules engine watches this)
	foundComplete := false
	for _, msg := range result.PublishedMessages {
		if containsIgnoreCase(msg.Subject, "agent.complete") {
			foundComplete = true
			break
		}
	}
	if !foundComplete {
		t.Error("Should publish agent.complete message")
	}
}

func TestHandleModelResponse_Error(t *testing.T) {
	handler := agenticloop.NewMessageHandler(createTestConfig())

	ctx := context.Background()
	taskResult, err := handler.HandleTask(ctx, agenticloop.TaskMessage{
		TaskID: "task-001",
		Role:   "general",
		Model:  "qwen-32b",
		Prompt: "Test",
	})
	if err != nil {
		t.Fatalf("HandleTask() error = %v", err)
	}

	loopID := taskResult.LoopID

	// Model error response
	response := agentic.AgentResponse{
		RequestID: "req-001",
		Status:    "error",
		Error:     "Model timeout",
		TokenUsage: agentic.TokenUsage{
			PromptTokens:     50,
			CompletionTokens: 0,
		},
	}

	result, err := handler.HandleModelResponse(ctx, loopID, response)
	if err != nil {
		t.Fatalf("HandleModelResponse() error = %v", err)
	}

	// Should mark loop as failed or retry
	if result.State != agentic.LoopStateFailed && result.RetryScheduled == false {
		t.Error("Error response should mark loop as failed or schedule retry")
	}

	// Should record error in trajectory
	if len(result.TrajectorySteps) == 0 {
		t.Error("Should record trajectory step for error")
	}
}

func TestHandleModelResponse_LengthTruncated(t *testing.T) {
	handler := agenticloop.NewMessageHandler(createTestConfig())

	ctx := context.Background()
	taskResult, err := handler.HandleTask(ctx, agenticloop.TaskMessage{
		TaskID: "task-truncated",
		Role:   "general",
		Model:  "qwen-32b",
		Prompt: "Test",
	})
	if err != nil {
		t.Fatalf("HandleTask() error = %v", err)
	}

	loopID := taskResult.LoopID

	// Model response with finish_reason=length (truncated output)
	response := agentic.AgentResponse{
		RequestID:    "req-truncated",
		Status:       agentic.StatusLengthTruncated,
		FinishReason: agentic.FinishReasonLength,
		Message: agentic.ChatMessage{
			Role:    "assistant",
			Content: "This response was cut off before it cou",
		},
		TokenUsage: agentic.TokenUsage{
			PromptTokens:     100,
			CompletionTokens: 4096,
		},
	}

	result, err := handler.HandleModelResponse(ctx, loopID, response)
	if err != nil {
		t.Fatalf("HandleModelResponse() error = %v", err)
	}

	// Should fail the loop, not succeed
	if result.State != agentic.LoopStateFailed {
		t.Errorf("State = %s, want %s (truncated response should fail)", result.State, agentic.LoopStateFailed)
	}

	// Should publish agent.failed event
	found := false
	for _, msg := range result.PublishedMessages {
		if containsIgnoreCase(msg.Subject, "agent.failed") {
			found = true

			// Verify the failure event contains truncation details
			var failedEvent struct {
				Payload struct {
					Reason string `json:"reason"`
					Error  string `json:"error"`
				} `json:"payload"`
			}
			if jsonErr := json.Unmarshal(msg.Data, &failedEvent); jsonErr == nil {
				if failedEvent.Payload.Reason != "length_truncated" {
					t.Errorf("FailedEvent.Reason = %q, want %q", failedEvent.Payload.Reason, "length_truncated")
				}
			}
			break
		}
	}
	if !found {
		t.Error("Should publish agent.failed message for truncated response")
	}

	// Should record trajectory step
	if len(result.TrajectorySteps) == 0 {
		t.Error("Should record trajectory step for truncated response")
	}

	// FailureState should be populated
	if result.FailureState == nil {
		t.Error("FailureState should not be nil on truncation")
	}
}

func TestHandleToolResult_SingleTool(t *testing.T) {
	handler := agenticloop.NewMessageHandler(createTestConfig())

	// Create loop and trigger tool call
	ctx := context.Background()
	taskResult, err := handler.HandleTask(ctx, agenticloop.TaskMessage{
		TaskID: "task-001",
		Role:   "general",
		Model:  "qwen-32b",
		Prompt: "Test",
	})
	if err != nil {
		t.Fatalf("HandleTask() error = %v", err)
	}

	loopID := taskResult.LoopID

	// Model response with single tool call
	toolResponse := agentic.AgentResponse{
		RequestID: "req-001",
		Status:    "tool_call",
		Message: agentic.ChatMessage{
			Role: "assistant",
			ToolCalls: []agentic.ToolCall{
				{
					ID:   "call-001",
					Name: "graph_query",
				},
			},
		},
	}

	_, err = handler.HandleModelResponse(ctx, loopID, toolResponse)
	if err != nil {
		t.Fatalf("HandleModelResponse() error = %v", err)
	}

	// Tool result
	toolResult := agentic.ToolResult{
		CallID:  "call-001",
		Content: "Query result data",
	}

	result, err := handler.HandleToolResult(ctx, loopID, toolResult)
	if err != nil {
		t.Fatalf("HandleToolResult() error = %v", err)
	}

	// Should remove from pending tools
	if len(result.PendingTools) != 0 {
		t.Errorf("PendingTools should be empty after result, got %d", len(result.PendingTools))
	}

	// Should publish next agent.request
	found := false
	for _, msg := range result.PublishedMessages {
		if containsIgnoreCase(msg.Subject, "agent.request") {
			found = true

			// Verify request includes tool result (wrapped in BaseMessage envelope)
			var envelope map[string]any
			if err := json.Unmarshal(msg.Data, &envelope); err != nil {
				t.Fatalf("Failed to parse envelope: %v", err)
			}
			payload, ok := envelope["payload"].(map[string]any)
			if !ok {
				t.Fatalf("Expected payload in BaseMessage envelope")
			}
			messages, ok := payload["messages"].([]any)
			if !ok || len(messages) == 0 {
				t.Error("Request should include messages with tool result")
			}
			break
		}
	}
	if !found {
		t.Error("Should publish next agent.request after tool completion")
	}

	// Should record trajectory step
	if len(result.TrajectorySteps) == 0 {
		t.Error("Should record trajectory step for tool result")
	}

	// Verify tool_call step was persisted in trajectory manager
	traj, trajErr := handler.GetTrajectory(loopID)
	if trajErr != nil {
		t.Fatalf("GetTrajectory() error = %v", trajErr)
	}
	foundToolCall := false
	for _, s := range traj.Steps {
		if s.StepType == "tool_call" {
			foundToolCall = true
			break
		}
	}
	if !foundToolCall {
		t.Error("Trajectory manager should contain a tool_call step")
	}
}

func TestHandleToolResult_MultipleTool_SerialDispatch(t *testing.T) {
	handler := agenticloop.NewMessageHandler(createTestConfig())

	ctx := context.Background()
	taskResult, err := handler.HandleTask(ctx, agenticloop.TaskMessage{
		TaskID: "task-001",
		Role:   "general",
		Model:  "qwen-32b",
		Prompt: "Test",
	})
	if err != nil {
		t.Fatalf("HandleTask() error = %v", err)
	}

	loopID := taskResult.LoopID

	// Model response with 3 tool calls — only the first should be dispatched
	toolResponse := agentic.AgentResponse{
		RequestID: "req-001",
		Status:    "tool_call",
		Message: agentic.ChatMessage{
			Role: "assistant",
			ToolCalls: []agentic.ToolCall{
				{ID: "call-001", Name: "tool1"},
				{ID: "call-002", Name: "tool2"},
				{ID: "call-003", Name: "tool3"},
			},
		},
	}

	modelResult, err := handler.HandleModelResponse(ctx, loopID, toolResponse)
	if err != nil {
		t.Fatalf("HandleModelResponse() error = %v", err)
	}

	// Only 1 tool.execute should be published (serial dispatch)
	toolExecCount := 0
	for _, msg := range modelResult.PublishedMessages {
		if containsIgnoreCase(msg.Subject, "tool.execute") {
			toolExecCount++
		}
	}
	if toolExecCount != 1 {
		t.Errorf("HandleModelResponse should dispatch 1 tool, got %d", toolExecCount)
	}

	// First tool result → should dispatch tool2
	result1, err := handler.HandleToolResult(ctx, loopID, agentic.ToolResult{
		CallID:  "call-001",
		Content: "Result 1",
	})
	if err != nil {
		t.Fatalf("HandleToolResult() #1 error = %v", err)
	}

	// Should have dispatched tool2 (1 pending)
	if len(result1.PendingTools) != 1 {
		t.Errorf("After first result, pending = %d, want 1", len(result1.PendingTools))
	}
	foundToolExec := false
	for _, msg := range result1.PublishedMessages {
		if containsIgnoreCase(msg.Subject, "tool.execute.tool2") {
			foundToolExec = true
		}
		if containsIgnoreCase(msg.Subject, "agent.request") {
			t.Error("Should not publish agent.request until all tools complete")
		}
	}
	if !foundToolExec {
		t.Error("Should dispatch tool2 after tool1 completes")
	}

	// Second tool result → should dispatch tool3
	result2, err := handler.HandleToolResult(ctx, loopID, agentic.ToolResult{
		CallID:  "call-002",
		Content: "Result 2",
	})
	if err != nil {
		t.Fatalf("HandleToolResult() #2 error = %v", err)
	}

	if len(result2.PendingTools) != 1 {
		t.Errorf("After second result, pending = %d, want 1", len(result2.PendingTools))
	}
	foundToolExec = false
	for _, msg := range result2.PublishedMessages {
		if containsIgnoreCase(msg.Subject, "tool.execute.tool3") {
			foundToolExec = true
		}
		if containsIgnoreCase(msg.Subject, "agent.request") {
			t.Error("Should not publish agent.request until all tools complete")
		}
	}
	if !foundToolExec {
		t.Error("Should dispatch tool3 after tool2 completes")
	}

	// Third tool result → queue drained, should publish agent.request
	result3, err := handler.HandleToolResult(ctx, loopID, agentic.ToolResult{
		CallID:  "call-003",
		Content: "Result 3",
	})
	if err != nil {
		t.Fatalf("HandleToolResult() #3 error = %v", err)
	}

	if len(result3.PendingTools) != 0 {
		t.Errorf("After all results, pending = %d, want 0", len(result3.PendingTools))
	}

	foundRequest := false
	for _, msg := range result3.PublishedMessages {
		if containsIgnoreCase(msg.Subject, "agent.request") {
			foundRequest = true
			break
		}
	}
	if !foundRequest {
		t.Error("Should publish agent.request after all tools complete")
	}
}

func TestHandleToolResult_WithError(t *testing.T) {
	handler := agenticloop.NewMessageHandler(createTestConfig())

	ctx := context.Background()
	taskResult, err := handler.HandleTask(ctx, agenticloop.TaskMessage{
		TaskID: "task-001",
		Role:   "general",
		Model:  "qwen-32b",
		Prompt: "Test",
	})
	if err != nil {
		t.Fatalf("HandleTask() error = %v", err)
	}

	loopID := taskResult.LoopID

	// Trigger tool call
	toolResponse := agentic.AgentResponse{
		RequestID: "req-001",
		Status:    "tool_call",
		Message: agentic.ChatMessage{
			Role: "assistant",
			ToolCalls: []agentic.ToolCall{
				{ID: "call-001", Name: "graph_query"},
			},
		},
	}

	_, err = handler.HandleModelResponse(ctx, loopID, toolResponse)
	if err != nil {
		t.Fatalf("HandleModelResponse() error = %v", err)
	}

	// Tool result with error
	toolResult := agentic.ToolResult{
		CallID: "call-001",
		Error:  "Query execution failed",
	}

	result, err := handler.HandleToolResult(ctx, loopID, toolResult)
	if err != nil {
		t.Fatalf("HandleToolResult() error = %v", err)
	}

	// Should still process (model can handle tool errors)
	if len(result.PendingTools) != 0 {
		t.Error("Should remove from pending even with error result")
	}

	// Should publish next agent.request with error included
	found := false
	for _, msg := range result.PublishedMessages {
		if containsIgnoreCase(msg.Subject, "agent.request") {
			found = true
			break
		}
	}
	if !found {
		t.Error("Should publish agent.request even with tool error")
	}
}

// TestHandleToolResult_ErrorCategoryFallsBackToUnknown verifies that when a
// ToolResult carries an error message but no structured ErrorKind (e.g. from
// an older executor that predates the classification refactor), the
// TrajectoryStep still emits ToolStatus="failed" and ErrorCategory="unknown"
// so graph queries can bucket unclassified failures.
func TestHandleToolResult_ErrorCategoryFallsBackToUnknown(t *testing.T) {
	handler := agenticloop.NewMessageHandler(createTestConfig())

	ctx := context.Background()
	taskResult, err := handler.HandleTask(ctx, agenticloop.TaskMessage{
		TaskID: "task-fallback",
		Role:   "general",
		Model:  "qwen-32b",
		Prompt: "Test",
	})
	if err != nil {
		t.Fatalf("HandleTask() error = %v", err)
	}
	loopID := taskResult.LoopID

	// Register a tool call so HandleToolResult has a known call to resolve.
	toolResponse := agentic.AgentResponse{
		RequestID: "req-fallback",
		Status:    "tool_call",
		Message: agentic.ChatMessage{
			Role: "assistant",
			ToolCalls: []agentic.ToolCall{
				{ID: "call-fallback", Name: "graph_query"},
			},
		},
	}
	if _, err := handler.HandleModelResponse(ctx, loopID, toolResponse); err != nil {
		t.Fatalf("HandleModelResponse() error = %v", err)
	}

	// ToolResult has Error set but NO ErrorKind — simulates an older
	// producer path or an unclassified failure.
	toolResult := agentic.ToolResult{
		CallID: "call-fallback",
		Error:  "something went wrong",
	}
	result, err := handler.HandleToolResult(ctx, loopID, toolResult)
	if err != nil {
		t.Fatalf("HandleToolResult() error = %v", err)
	}
	if len(result.TrajectorySteps) == 0 {
		t.Fatal("expected a TrajectoryStep for the tool_call")
	}
	step := result.TrajectorySteps[0]
	if step.ToolStatus != "failed" {
		t.Errorf("ToolStatus = %q, want %q", step.ToolStatus, "failed")
	}
	if step.ErrorMessage != "something went wrong" {
		t.Errorf("ErrorMessage = %q, want copy of the raw error", step.ErrorMessage)
	}
	if step.ErrorCategory != string(agentic.ToolErrorUnknown) {
		t.Errorf("ErrorCategory = %q, want %q (fallback)", step.ErrorCategory, agentic.ToolErrorUnknown)
	}
}

// TestHandleToolResult_ErrorCategoryPreservesExecutorKind verifies that when
// an executor classifies its own failure (e.g. InvalidArgs), the handler
// preserves that kind instead of overwriting with "unknown".
func TestHandleToolResult_ErrorCategoryPreservesExecutorKind(t *testing.T) {
	handler := agenticloop.NewMessageHandler(createTestConfig())

	ctx := context.Background()
	taskResult, err := handler.HandleTask(ctx, agenticloop.TaskMessage{
		TaskID: "task-preserve",
		Role:   "general",
		Model:  "qwen-32b",
		Prompt: "Test",
	})
	if err != nil {
		t.Fatalf("HandleTask() error = %v", err)
	}
	loopID := taskResult.LoopID

	toolResponse := agentic.AgentResponse{
		RequestID: "req-preserve",
		Status:    "tool_call",
		Message: agentic.ChatMessage{
			Role: "assistant",
			ToolCalls: []agentic.ToolCall{
				{ID: "call-preserve", Name: "graph_query"},
			},
		},
	}
	if _, err := handler.HandleModelResponse(ctx, loopID, toolResponse); err != nil {
		t.Fatalf("HandleModelResponse() error = %v", err)
	}

	toolResult := agentic.ToolResult{
		CallID:    "call-preserve",
		Error:     "entity_id is required",
		ErrorKind: agentic.ToolErrorInvalidArgs,
	}
	result, err := handler.HandleToolResult(ctx, loopID, toolResult)
	if err != nil {
		t.Fatalf("HandleToolResult() error = %v", err)
	}
	step := result.TrajectorySteps[0]
	if step.ErrorCategory != string(agentic.ToolErrorInvalidArgs) {
		t.Errorf("ErrorCategory = %q, want %q", step.ErrorCategory, agentic.ToolErrorInvalidArgs)
	}
}

func TestHandleToolResult_StopLoop(t *testing.T) {
	handler := agenticloop.NewMessageHandler(createTestConfig())

	ctx := context.Background()
	taskResult, err := handler.HandleTask(ctx, agenticloop.TaskMessage{
		TaskID: "task-001",
		Role:   "general",
		Model:  "qwen-32b",
		Prompt: "Test",
	})
	if err != nil {
		t.Fatalf("HandleTask() error = %v", err)
	}

	loopID := taskResult.LoopID

	// Trigger tool call
	toolResponse := agentic.AgentResponse{
		RequestID: "req-001",
		Status:    "tool_call",
		Message: agentic.ChatMessage{
			Role: "assistant",
			ToolCalls: []agentic.ToolCall{
				{ID: "call-001", Name: "decompose_quest"},
			},
		},
	}

	_, err = handler.HandleModelResponse(ctx, loopID, toolResponse)
	if err != nil {
		t.Fatalf("HandleModelResponse() error = %v", err)
	}

	// Tool result with StopLoop
	toolResult := agentic.ToolResult{
		CallID:   "call-001",
		Content:  `{"dag": "quest-decomposition-result"}`,
		StopLoop: true,
	}

	result, err := handler.HandleToolResult(ctx, loopID, toolResult)
	if err != nil {
		t.Fatalf("HandleToolResult() error = %v", err)
	}

	// Should be in complete state
	if result.State != agentic.LoopStateComplete {
		t.Errorf("State = %q, want %q", result.State, agentic.LoopStateComplete)
	}

	// Should publish agent.complete (not agent.request)
	foundComplete := false
	foundRequest := false
	for _, msg := range result.PublishedMessages {
		if containsIgnoreCase(msg.Subject, "agent.complete") {
			foundComplete = true

			// Verify the completion result contains the tool's content
			var envelope map[string]any
			if err := json.Unmarshal(msg.Data, &envelope); err != nil {
				t.Fatalf("Failed to parse envelope: %v", err)
			}
			payload, ok := envelope["payload"].(map[string]any)
			if !ok {
				t.Fatalf("Expected payload in BaseMessage envelope")
			}
			if got, ok := payload["result"].(string); !ok || got != toolResult.Content {
				t.Errorf("Completion result = %q, want %q", got, toolResult.Content)
			}
		}
		if containsIgnoreCase(msg.Subject, "agent.request") {
			foundRequest = true
		}
	}
	if !foundComplete {
		t.Error("Should publish agent.complete when StopLoop is set")
	}
	if foundRequest {
		t.Error("Should NOT publish agent.request when StopLoop is set")
	}

	// Should have completion state set
	if result.CompletionState == nil {
		t.Error("CompletionState should be set for StopLoop")
	}

	// Verify tool_call step was persisted in trajectory manager (regression test:
	// prior to fix, tool_call steps were only on result.TrajectorySteps but never
	// added to the trajectory manager, so they were missing from query responses).
	traj, trajErr := handler.GetTrajectory(loopID)
	if trajErr != nil {
		t.Fatalf("GetTrajectory() error = %v", trajErr)
	}
	foundToolCall := false
	for _, s := range traj.Steps {
		if s.StepType == "tool_call" {
			foundToolCall = true
			if s.ToolName != "decompose_quest" {
				t.Errorf("tool_call step ToolName = %q, want %q", s.ToolName, "decompose_quest")
			}
			break
		}
	}
	if !foundToolCall {
		t.Error("Trajectory manager should contain a tool_call step after HandleToolResult")
	}
}

// TestHandleToolResult_StopLoopClearsQueue verifies that when the model emits
// multiple tool calls and the first one returns StopLoop, the remaining queued
// calls are never dispatched.
func TestHandleToolResult_StopLoopClearsQueue(t *testing.T) {
	handler := agenticloop.NewMessageHandler(createTestConfig())

	ctx := context.Background()
	taskResult, err := handler.HandleTask(ctx, agenticloop.TaskMessage{
		TaskID:     "task-001",
		Role:       "general",
		Model:      "qwen-32b",
		Prompt:     "Test StopLoop clears queue",
		ToolChoice: &agentic.ToolChoice{Mode: "required"},
	})
	if err != nil {
		t.Fatalf("HandleTask() error = %v", err)
	}

	loopID := taskResult.LoopID

	// Model emits two tool calls: submit_work (first, will StopLoop) and bash (queued)
	toolResponse := agentic.AgentResponse{
		RequestID: "req-001",
		Status:    "tool_call",
		Message: agentic.ChatMessage{
			Role: "assistant",
			ToolCalls: []agentic.ToolCall{
				{ID: "call-submit", Name: "submit_work"},
				{ID: "call-bash", Name: "bash"},
			},
		},
	}

	modelResult, err := handler.HandleModelResponse(ctx, loopID, toolResponse)
	if err != nil {
		t.Fatalf("HandleModelResponse() error = %v", err)
	}

	// Only submit_work should be dispatched (bash is queued)
	toolExecCount := 0
	for _, msg := range modelResult.PublishedMessages {
		if containsIgnoreCase(msg.Subject, "tool.execute") {
			toolExecCount++
			if !containsIgnoreCase(msg.Subject, "submit_work") {
				t.Errorf("Expected tool.execute.submit_work, got %s", msg.Subject)
			}
		}
	}
	if toolExecCount != 1 {
		t.Errorf("Should dispatch exactly 1 tool, got %d", toolExecCount)
	}

	// submit_work returns StopLoop → loop completes, bash never dispatched
	submitResult, err := handler.HandleToolResult(ctx, loopID, agentic.ToolResult{
		CallID:   "call-submit",
		Content:  `{"output": "final answer"}`,
		StopLoop: true,
	})
	if err != nil {
		t.Fatalf("HandleToolResult(submit) error = %v", err)
	}

	if submitResult.State != agentic.LoopStateComplete {
		t.Errorf("After StopLoop, state = %q, want %q", submitResult.State, agentic.LoopStateComplete)
	}

	// Verify agent.complete published, no tool.execute for bash
	foundComplete := false
	for _, msg := range submitResult.PublishedMessages {
		if containsIgnoreCase(msg.Subject, "agent.complete") {
			foundComplete = true
		}
		if containsIgnoreCase(msg.Subject, "tool.execute") {
			t.Fatal("StopLoop must not dispatch queued tools")
		}
		if containsIgnoreCase(msg.Subject, "agent.request") {
			t.Fatal("StopLoop must not publish agent.request")
		}
	}
	if !foundComplete {
		t.Error("StopLoop tool should publish agent.complete")
	}
}

// TestHandleModelResponse_TerminalLoop verifies that model responses for loops
// already in terminal state are rejected (defense-in-depth against stale agent.request).
func TestHandleModelResponse_TerminalLoop(t *testing.T) {
	handler := agenticloop.NewMessageHandler(createTestConfig())

	ctx := context.Background()
	taskResult, err := handler.HandleTask(ctx, agenticloop.TaskMessage{
		TaskID: "task-001",
		Role:   "general",
		Model:  "qwen-32b",
		Prompt: "Test terminal rejection",
	})
	if err != nil {
		t.Fatalf("HandleTask() error = %v", err)
	}

	loopID := taskResult.LoopID

	// Complete the loop via StopLoop
	toolResponse := agentic.AgentResponse{
		RequestID: "req-001",
		Status:    "tool_call",
		Message: agentic.ChatMessage{
			Role:      "assistant",
			ToolCalls: []agentic.ToolCall{{ID: "call-001", Name: "submit_work"}},
		},
	}
	_, err = handler.HandleModelResponse(ctx, loopID, toolResponse)
	if err != nil {
		t.Fatalf("HandleModelResponse() error = %v", err)
	}

	_, err = handler.HandleToolResult(ctx, loopID, agentic.ToolResult{
		CallID:   "call-001",
		Content:  "done",
		StopLoop: true,
	})
	if err != nil {
		t.Fatalf("HandleToolResult(StopLoop) error = %v", err)
	}

	// Now send a model response to the terminal loop (simulates stale agent.request)
	staleResponse := agentic.AgentResponse{
		RequestID: "req-stale",
		Status:    "tool_call",
		Message: agentic.ChatMessage{
			Role:      "assistant",
			ToolCalls: []agentic.ToolCall{{ID: "call-stale", Name: "bash"}},
		},
	}
	result, err := handler.HandleModelResponse(ctx, loopID, staleResponse)
	if err != nil {
		t.Fatalf("HandleModelResponse(terminal) error = %v", err)
	}

	// Should return terminal state with no published messages
	if result.State != agentic.LoopStateComplete {
		t.Errorf("State = %q, want %q", result.State, agentic.LoopStateComplete)
	}
	if len(result.PublishedMessages) != 0 {
		t.Errorf("Terminal loop should not publish any messages, got %d", len(result.PublishedMessages))
	}
}

func TestBuildIterationBudgetMessage_Tiers(t *testing.T) {
	tests := []struct {
		name      string
		iteration int
		max       int
		wantTier  string // substring that identifies the tier
	}{
		{"neutral_early", 1, 20, "[Iteration Budget] Iteration 1 of 20 (5% used)."},
		{"neutral_half", 10, 20, "[Iteration Budget] Iteration 10 of 20 (50% used)."},
		{"warning", 15, 20, "Consider wrapping up"},
		{"urgent", 18, 20, "Budget nearly exhausted"},
		{"urgent_last", 20, 20, "Budget nearly exhausted"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := agenticloop.BuildIterationBudgetMessage(tt.iteration, tt.max)
			if msg.Role != "system" {
				t.Errorf("Role = %q, want system", msg.Role)
			}
			if !containsIgnoreCase(msg.Content, tt.wantTier) {
				t.Errorf("Content = %q, want substring %q", msg.Content, tt.wantTier)
			}
		})
	}
}

func TestHandleTask_IncludesBudgetMessage(t *testing.T) {
	handler := agenticloop.NewMessageHandler(createTestConfig())

	ctx := context.Background()
	result, err := handler.HandleTask(ctx, agenticloop.TaskMessage{
		TaskID: "task-budget",
		Role:   "general",
		Model:  "qwen-32b",
		Prompt: "Test budget injection",
	})
	if err != nil {
		t.Fatalf("HandleTask() error = %v", err)
	}

	// Find the agent.request and check for budget message
	for _, msg := range result.PublishedMessages {
		if !containsIgnoreCase(msg.Subject, "agent.request") {
			continue
		}

		var envelope map[string]any
		if err := json.Unmarshal(msg.Data, &envelope); err != nil {
			t.Fatalf("Failed to parse envelope: %v", err)
		}
		payload, ok := envelope["payload"].(map[string]any)
		if !ok {
			t.Fatal("Expected payload in BaseMessage envelope")
		}
		messages, ok := payload["messages"].([]any)
		if !ok || len(messages) == 0 {
			t.Fatal("Expected messages in request")
		}

		// First message should be the budget system message
		first, ok := messages[0].(map[string]any)
		if !ok {
			t.Fatal("Expected message object")
		}
		content, _ := first["content"].(string)
		if !containsIgnoreCase(content, "[Iteration Budget]") {
			t.Errorf("First message should be iteration budget, got: %s", content)
		}
		if !containsIgnoreCase(content, "Iteration 1 of") {
			t.Errorf("Budget should show iteration 1, got: %s", content)
		}
		return
	}
	t.Error("No agent.request found in published messages")
}

func TestHandleToolResult_NonExistentLoop(t *testing.T) {
	handler := agenticloop.NewMessageHandler(createTestConfig())

	ctx := context.Background()
	toolResult := agentic.ToolResult{
		CallID:  "call-001",
		Content: "Result",
	}

	_, err := handler.HandleToolResult(ctx, "loop-does-not-exist", toolResult)
	if err == nil {
		t.Error("HandleToolResult() with non-existent loop should return error")
	}
}

func TestMessageHandler_MaxIterationsGuard(t *testing.T) {
	// Create config with max 2 iterations
	config := createTestConfig()
	config.MaxIterations = 2

	handler := agenticloop.NewMessageHandler(config)

	ctx := context.Background()
	taskResult, err := handler.HandleTask(ctx, agenticloop.TaskMessage{
		TaskID: "task-001",
		Role:   "general",
		Model:  "qwen-32b",
		Prompt: "Test",
	})
	if err != nil {
		t.Fatalf("HandleTask() error = %v", err)
	}

	loopID := taskResult.LoopID

	// Iteration 1: tool call and result
	_, err = handler.HandleModelResponse(ctx, loopID, agentic.AgentResponse{
		RequestID: "req-001",
		Status:    "tool_call",
		Message: agentic.ChatMessage{
			Role:      "assistant",
			ToolCalls: []agentic.ToolCall{{ID: "call-001", Name: "tool1"}},
		},
	})
	if err != nil {
		t.Fatalf("HandleModelResponse() iteration 1 error = %v", err)
	}

	_, err = handler.HandleToolResult(ctx, loopID, agentic.ToolResult{
		CallID:  "call-001",
		Content: "Result 1",
	})
	if err != nil {
		t.Fatalf("HandleToolResult() iteration 1 error = %v", err)
	}

	// Iteration 2: tool call and result
	_, err = handler.HandleModelResponse(ctx, loopID, agentic.AgentResponse{
		RequestID: "req-002",
		Status:    "tool_call",
		Message: agentic.ChatMessage{
			Role:      "assistant",
			ToolCalls: []agentic.ToolCall{{ID: "call-002", Name: "tool2"}},
		},
	})
	if err != nil {
		t.Fatalf("HandleModelResponse() iteration 2 error = %v", err)
	}

	result, err := handler.HandleToolResult(ctx, loopID, agentic.ToolResult{
		CallID:  "call-002",
		Content: "Result 2",
	})
	if err != nil {
		t.Fatalf("HandleToolResult() iteration 2 error = %v", err)
	}

	// After 2 iterations, should reach max and mark as failed or complete
	// Depends on implementation, but should not allow iteration 3
	if result.State == agentic.LoopStateFailed || result.MaxIterationsReached {
		// Expected behavior - max iterations enforced
	} else {
		// Attempt iteration 3 should fail
		_, err = handler.HandleModelResponse(ctx, loopID, agentic.AgentResponse{
			RequestID: "req-003",
			Status:    "tool_call",
			Message: agentic.ChatMessage{
				Role:      "assistant",
				ToolCalls: []agentic.ToolCall{{ID: "call-003", Name: "tool3"}},
			},
		})

		if err == nil {
			t.Error("Should not allow iteration beyond max_iterations")
		}
	}
}

// Test helper functions

type testConfig struct {
	MaxIterations int
}

func createTestConfig() agenticloop.Config {
	return agenticloop.DefaultConfig()
}

type PublishedMessage struct {
	Subject string
	Data    []byte
}

type HandlerResult struct {
	LoopID               string
	State                agentic.LoopState
	PublishedMessages    []PublishedMessage
	PendingTools         []string
	TrajectorySteps      []agentic.TrajectoryStep
	RetryScheduled       bool
	MaxIterationsReached bool
	CompletionState      map[string]any
}

// TestHandleTask_PopulatesToolsInRequest verifies that AgentRequest.Tools
// is populated with tool definitions from the wired registry.
func TestHandleTask_PopulatesToolsInRequest(t *testing.T) {
	handler := agenticloop.NewMessageHandler(createTestConfig())
	handler.SetToolRegistry(newTestToolRegistry(t))

	task := agenticloop.TaskMessage{
		TaskID: "task-tools",
		Role:   "general",
		Model:  "test-model",
		Prompt: "Test with tools",
	}

	ctx := context.Background()
	result, err := handler.HandleTask(ctx, task)
	if err != nil {
		t.Fatalf("HandleTask() error = %v", err)
	}

	// Find the agent.request message
	var foundRequest bool
	for _, msg := range result.PublishedMessages {
		if !containsIgnoreCase(msg.Subject, "agent.request") {
			continue
		}
		foundRequest = true

		// Extract request from BaseMessage envelope
		var envelope map[string]any
		if err := json.Unmarshal(msg.Data, &envelope); err != nil {
			t.Fatalf("Failed to unmarshal envelope: %v", err)
		}
		payload, ok := envelope["payload"].(map[string]any)
		if !ok {
			t.Fatalf("Expected payload in BaseMessage envelope")
		}

		// CRITICAL ASSERTION: Tools field must be populated
		tools, hasTools := payload["tools"]
		if !hasTools {
			t.Error("AgentRequest.Tools should be present in payload")
			break
		}
		toolsSlice, ok := tools.([]any)
		if !ok {
			t.Errorf("AgentRequest.Tools should be a slice, got %T", tools)
			break
		}
		if len(toolsSlice) == 0 {
			t.Error("AgentRequest.Tools should not be empty - tools should be discovered from registry")
		}
		break
	}

	if !foundRequest {
		t.Error("HandleTask() should publish agent.request message")
	}
}

// TestHandleToolResult_NextRequestHasTools verifies that subsequent AgentRequest
// messages (after tool completion) also include tool definitions.
func TestHandleToolResult_NextRequestHasTools(t *testing.T) {
	handler := agenticloop.NewMessageHandler(createTestConfig())
	handler.SetToolRegistry(newTestToolRegistry(t))

	// Create loop first
	ctx := context.Background()
	taskResult, err := handler.HandleTask(ctx, agenticloop.TaskMessage{
		TaskID: "task-tools-2",
		Role:   "general",
		Model:  "test-model",
		Prompt: "Test with tools",
	})
	if err != nil {
		t.Fatalf("HandleTask() error = %v", err)
	}

	loopID := taskResult.LoopID

	// Trigger a tool call
	toolResponse := agentic.AgentResponse{
		RequestID: "req-001",
		Status:    "tool_call",
		Message: agentic.ChatMessage{
			Role: "assistant",
			ToolCalls: []agentic.ToolCall{
				{ID: "call-001", Name: "test_tool"},
			},
		},
	}

	_, err = handler.HandleModelResponse(ctx, loopID, toolResponse)
	if err != nil {
		t.Fatalf("HandleModelResponse() error = %v", err)
	}

	// Complete the tool
	toolResult := agentic.ToolResult{
		CallID:  "call-001",
		Content: "Tool result",
	}

	result, err := handler.HandleToolResult(ctx, loopID, toolResult)
	if err != nil {
		t.Fatalf("HandleToolResult() error = %v", err)
	}

	// Find the follow-up agent.request message
	var foundRequest bool
	for _, msg := range result.PublishedMessages {
		if !containsIgnoreCase(msg.Subject, "agent.request") {
			continue
		}
		foundRequest = true

		// Extract request from BaseMessage envelope
		var envelope map[string]any
		if err := json.Unmarshal(msg.Data, &envelope); err != nil {
			t.Fatalf("Failed to unmarshal envelope: %v", err)
		}
		payload, ok := envelope["payload"].(map[string]any)
		if !ok {
			t.Fatalf("Expected payload in BaseMessage envelope")
		}

		// CRITICAL ASSERTION: Tools field must be populated in subsequent requests too
		tools, hasTools := payload["tools"]
		if !hasTools {
			t.Error("AgentRequest.Tools should be present in subsequent requests")
			break
		}
		toolsSlice, ok := tools.([]any)
		if !ok {
			t.Errorf("AgentRequest.Tools should be a slice, got %T", tools)
			break
		}
		if len(toolsSlice) == 0 {
			t.Error("AgentRequest.Tools should not be empty in subsequent requests")
		}
		break
	}

	if !foundRequest {
		t.Error("HandleToolResult() should publish next agent.request")
	}
}

// TestHandleModelResponse_Complete_PopulatesTokenFields verifies that
// LoopCompletedEvent includes token totals from the trajectory.
func TestHandleModelResponse_Complete_PopulatesTokenFields(t *testing.T) {
	handler := agenticloop.NewMessageHandler(createTestConfig())

	ctx := context.Background()
	taskResult, err := handler.HandleTask(ctx, agenticloop.TaskMessage{
		TaskID: "task-tokens",
		Role:   "general",
		Model:  "test-model",
		Prompt: "Test token tracking",
	})
	if err != nil {
		t.Fatalf("HandleTask() error = %v", err)
	}

	loopID := taskResult.LoopID

	// Model response with token usage
	response := agentic.AgentResponse{
		RequestID: "req-tokens",
		Status:    "complete",
		Message: agentic.ChatMessage{
			Role:    "assistant",
			Content: "Done",
		},
		TokenUsage: agentic.TokenUsage{
			PromptTokens:     1500,
			CompletionTokens: 750,
		},
	}

	result, err := handler.HandleModelResponse(ctx, loopID, response)
	if err != nil {
		t.Fatalf("HandleModelResponse() error = %v", err)
	}

	if result.CompletionState == nil {
		t.Fatal("CompletionState should not be nil")
	}

	// The trajectory should have accumulated tokens from this model call.
	// The HandleTask creates an initial trajectory step (no tokens),
	// then HandleModelResponse adds a step with the response tokens.
	if result.CompletionState.TokensIn != 1500 {
		t.Errorf("CompletionState.TokensIn = %d, want 1500", result.CompletionState.TokensIn)
	}
	if result.CompletionState.TokensOut != 750 {
		t.Errorf("CompletionState.TokensOut = %d, want 750", result.CompletionState.TokensOut)
	}
}

// --- Per-task tools tests ---

func TestHandleTask_PerTaskTools(t *testing.T) {
	handler := agenticloop.NewMessageHandler(createTestConfig())
	handler.SetToolRegistry(newTestToolRegistry(t))

	customTools := []agentic.ToolDefinition{
		{
			Name:        "custom_tool_a",
			Description: "Custom A",
			Parameters:  map[string]any{"type": "object"},
		},
		{
			Name:        "custom_tool_b",
			Description: "Custom B",
			Parameters:  map[string]any{"type": "object"},
		},
	}

	task := agenticloop.TaskMessage{
		TaskID: "task-per-task-tools",
		Role:   "general",
		Model:  "test-model",
		Prompt: "Test with per-task tools",
		Tools:  customTools,
	}

	ctx := context.Background()
	result, err := handler.HandleTask(ctx, task)
	if err != nil {
		t.Fatalf("HandleTask() error = %v", err)
	}

	// Find agent.request and verify it contains per-task tools, not global ones
	for _, msg := range result.PublishedMessages {
		if !containsIgnoreCase(msg.Subject, "agent.request") {
			continue
		}
		var envelope map[string]any
		if err := json.Unmarshal(msg.Data, &envelope); err != nil {
			t.Fatalf("Failed to unmarshal envelope: %v", err)
		}
		payload, ok := envelope["payload"].(map[string]any)
		if !ok {
			t.Fatal("Expected payload in BaseMessage envelope")
		}
		tools, ok := payload["tools"].([]any)
		if !ok {
			t.Fatal("tools should be a slice")
		}
		if len(tools) != 2 {
			t.Errorf("Expected 2 per-task tools, got %d", len(tools))
		}
		// Verify tool names are the custom ones
		tool0, _ := tools[0].(map[string]any)
		if tool0["name"] != "custom_tool_a" {
			t.Errorf("First tool name = %v, want custom_tool_a", tool0["name"])
		}
		return
	}
	t.Error("HandleTask() should publish agent.request message")
}

// --- Metadata propagation tests ---

func TestHandleTask_MetadataCachedAndPropagated(t *testing.T) {
	handler := agenticloop.NewMessageHandler(createTestConfig())

	task := agenticloop.TaskMessage{
		TaskID: "task-meta",
		Role:   "general",
		Model:  "test-model",
		Prompt: "Test with metadata",
		Metadata: map[string]any{
			"tenant_id": "acme",
			"domain":    "robotics",
		},
	}

	ctx := context.Background()
	taskResult, err := handler.HandleTask(ctx, task)
	if err != nil {
		t.Fatalf("HandleTask() error = %v", err)
	}
	loopID := taskResult.LoopID

	// Trigger a tool call — metadata should flow to published tool calls
	toolResponse := agentic.AgentResponse{
		RequestID: "req-meta",
		Status:    "tool_call",
		Message: agentic.ChatMessage{
			Role: "assistant",
			ToolCalls: []agentic.ToolCall{
				{ID: "call-meta-001", Name: "graph_query"},
			},
		},
	}

	result, err := handler.HandleModelResponse(ctx, loopID, toolResponse)
	if err != nil {
		t.Fatalf("HandleModelResponse() error = %v", err)
	}

	// Find tool.execute message and check metadata propagation
	for _, msg := range result.PublishedMessages {
		if !containsIgnoreCase(msg.Subject, "tool.execute") {
			continue
		}
		var envelope map[string]any
		if err := json.Unmarshal(msg.Data, &envelope); err != nil {
			t.Fatalf("Failed to unmarshal: %v", err)
		}
		payload, ok := envelope["payload"].(map[string]any)
		if !ok {
			t.Fatal("Expected payload")
		}
		meta, ok := payload["metadata"].(map[string]any)
		if !ok {
			t.Fatal("Expected metadata in tool call payload")
		}
		if meta["tenant_id"] != "acme" {
			t.Errorf("metadata.tenant_id = %v, want acme", meta["tenant_id"])
		}
		if meta["domain"] != "robotics" {
			t.Errorf("metadata.domain = %v, want robotics", meta["domain"])
		}
		return
	}
	t.Error("Should publish tool.execute with metadata")
}

func TestHandleTask_PropagatesTimeoutToAgentRequest(t *testing.T) {
	handler := agenticloop.NewMessageHandler(createTestConfig())

	task := agenticloop.TaskMessage{
		TaskID:  "task-timeout",
		Role:    "general",
		Model:   "test-model",
		Prompt:  "Test with timeout",
		Timeout: "30s",
	}

	ctx := context.Background()
	result, err := handler.HandleTask(ctx, task)
	if err != nil {
		t.Fatalf("HandleTask() error = %v", err)
	}

	for _, msg := range result.PublishedMessages {
		if !containsIgnoreCase(msg.Subject, "agent.request") {
			continue
		}
		var envelope map[string]any
		if err := json.Unmarshal(msg.Data, &envelope); err != nil {
			t.Fatalf("Failed to unmarshal envelope: %v", err)
		}
		payload, ok := envelope["payload"].(map[string]any)
		if !ok {
			t.Fatal("Expected payload in BaseMessage envelope")
		}
		got, ok := payload["timeout"].(string)
		if !ok {
			t.Fatalf("AgentRequest.timeout missing or wrong type: %T", payload["timeout"])
		}
		if got != "30s" {
			t.Errorf("AgentRequest.timeout = %q, want %q", got, "30s")
		}
		return
	}
	t.Error("HandleTask() should publish agent.request message")
}

// TestHandleTask_PropagatesResponseFormatToAgentRequest verifies the
// ADR-034 threading path: TaskMessage.ResponseFormat → cached on
// LoopManager → set on the AgentRequest published to agent.request.*.
// Continuation iterations re-use the cached value (covered separately
// by integration tests that exercise multi-iteration loops).
func TestHandleTask_PropagatesResponseFormatToAgentRequest(t *testing.T) {
	handler := agenticloop.NewMessageHandler(createTestConfig())

	rf := agentic.NewJSONSchemaFormat("decision", map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"action": map[string]any{"type": "string"},
		},
		"required": []any{"action"},
	})

	task := agenticloop.TaskMessage{
		TaskID:         "task-response-format",
		Role:           "planner",
		Model:          "test-model",
		Prompt:         "Decide",
		ResponseFormat: rf,
	}

	ctx := context.Background()
	result, err := handler.HandleTask(ctx, task)
	if err != nil {
		t.Fatalf("HandleTask() error = %v", err)
	}

	for _, msg := range result.PublishedMessages {
		if !containsIgnoreCase(msg.Subject, "agent.request") {
			continue
		}
		var envelope map[string]any
		if err := json.Unmarshal(msg.Data, &envelope); err != nil {
			t.Fatalf("Failed to unmarshal envelope: %v", err)
		}
		payload, ok := envelope["payload"].(map[string]any)
		if !ok {
			t.Fatal("Expected payload in BaseMessage envelope")
		}
		got, ok := payload["response_format"].(map[string]any)
		if !ok {
			t.Fatalf("AgentRequest.response_format missing or wrong type: %T", payload["response_format"])
		}
		if got["type"] != agentic.ResponseFormatJSONSchema {
			t.Errorf("AgentRequest.response_format.type = %q, want %q", got["type"], agentic.ResponseFormatJSONSchema)
		}
		if got["name"] != "decision" {
			t.Errorf("AgentRequest.response_format.name = %q, want %q", got["name"], "decision")
		}
		if got["strict"] != true {
			t.Errorf("AgentRequest.response_format.strict = %v, want true", got["strict"])
		}
		return
	}
	t.Error("HandleTask() should publish agent.request message")
}

// TestHandleTask_NoResponseFormatPreservesNil verifies that omitting
// ResponseFormat on TaskMessage leaves AgentRequest.response_format
// absent — back-compat for every caller that doesn't opt into ADR-034.
func TestHandleTask_NoResponseFormatPreservesNil(t *testing.T) {
	handler := agenticloop.NewMessageHandler(createTestConfig())

	task := agenticloop.TaskMessage{
		TaskID: "task-no-rf",
		Role:   "general",
		Model:  "test-model",
		Prompt: "no constraint",
	}

	ctx := context.Background()
	result, err := handler.HandleTask(context.Background(), task)
	if err != nil {
		t.Fatalf("HandleTask() error = %v", err)
	}
	_ = ctx

	for _, msg := range result.PublishedMessages {
		if !containsIgnoreCase(msg.Subject, "agent.request") {
			continue
		}
		var envelope map[string]any
		if err := json.Unmarshal(msg.Data, &envelope); err != nil {
			t.Fatalf("Failed to unmarshal envelope: %v", err)
		}
		payload, ok := envelope["payload"].(map[string]any)
		if !ok {
			t.Fatal("Expected payload in BaseMessage envelope")
		}
		if _, present := payload["response_format"]; present {
			t.Errorf("response_format should be absent when not set on TaskMessage; got %v", payload["response_format"])
		}
		return
	}
	t.Error("HandleTask() should publish agent.request message")
}

// TestHandleTask_DuplicateTaskID_DedupReturnsCreatedFalse asserts the
// dedup short-circuit reports Created=false when a TaskMessage arrives
// for a task_id that already has an active loop. This is the load-bearing
// signal that gates recordLoopCreated() in handleTaskMessage so the
// active_loops gauge does not drift on JetStream redelivery — without
// the gate, every redelivered TaskMessage adds +1 to the gauge while
// the original loop's eventual completion only fires one matching Dec().
func TestHandleTask_DuplicateTaskID_DedupReturnsCreatedFalse(t *testing.T) {
	handler := agenticloop.NewMessageHandler(createTestConfig())

	task := agenticloop.TaskMessage{
		TaskID: "task-dedup-001",
		Role:   "general",
		Model:  "test-model",
		Prompt: "first dispatch",
	}

	ctx := context.Background()
	first, err := handler.HandleTask(ctx, task)
	if err != nil {
		t.Fatalf("first HandleTask() error = %v", err)
	}
	if !first.Created {
		t.Fatal("first HandleTask() Created = false, want true")
	}
	if first.LoopID == "" {
		t.Fatal("first HandleTask() LoopID empty")
	}

	// Second dispatch with same task_id simulates a JetStream redelivery
	// (transient heartbeat failure, consumer ack delay, etc.).
	second, err := handler.HandleTask(ctx, task)
	if err != nil {
		t.Fatalf("second HandleTask() error = %v", err)
	}
	if second.Created {
		t.Error("second HandleTask() Created = true, want false (dedup short-circuit)")
	}
	if second.LoopID != first.LoopID {
		t.Errorf("second HandleTask() LoopID = %s, want %s (existing loop_id should be returned)", second.LoopID, first.LoopID)
	}
	if len(second.PublishedMessages) != 0 {
		t.Errorf("dedup result should not publish messages, got %d", len(second.PublishedMessages))
	}
}

// TestEmptyNameToolCalls_Rejected verifies that tool calls with empty names are
// dropped before dispatch. Gemini sometimes emits these as acknowledgment non-responses.
// The loop should store error results with a nudge and trigger tools-complete.
func TestEmptyNameToolCalls_Rejected(t *testing.T) {
	handler := agenticloop.NewMessageHandler(createTestConfig())
	// No filter — empty-name rejection is unconditional

	ctx := context.Background()
	taskResult, err := handler.HandleTask(ctx, agenticloop.TaskMessage{
		TaskID: "task-empty-name",
		Role:   "general",
		Model:  "test-model",
		Prompt: "Test empty names",
	})
	if err != nil {
		t.Fatalf("HandleTask() error = %v", err)
	}
	loopID := taskResult.LoopID

	// Model response with one valid and one empty-name tool call
	response := agentic.AgentResponse{
		RequestID: "req-empty-name",
		Status:    "tool_call",
		Message: agentic.ChatMessage{
			Role: "assistant",
			ToolCalls: []agentic.ToolCall{
				{ID: "call-en-1", Name: "real_tool"},
				{ID: "call-en-2", Name: ""},
			},
		},
	}

	result, err := handler.HandleModelResponse(ctx, loopID, response)
	if err != nil {
		t.Fatalf("HandleModelResponse() error = %v", err)
	}

	// Only real_tool should be dispatched
	toolCount := 0
	for _, msg := range result.PublishedMessages {
		if containsIgnoreCase(msg.Subject, "tool.execute") {
			toolCount++
		}
	}
	if toolCount != 1 {
		t.Errorf("Expected 1 tool.execute message, got %d", toolCount)
	}

	// Pending should only contain the valid tool
	if len(result.PendingTools) != 1 {
		t.Errorf("Expected 1 pending tool, got %d", len(result.PendingTools))
	}
}

// TestEmptyNameToolCalls_AllEmpty verifies that when ALL tool calls have empty names,
// the loop triggers tools-complete immediately with nudge error results, causing a
// retry with the model.
func TestEmptyNameToolCalls_AllEmpty(t *testing.T) {
	handler := agenticloop.NewMessageHandler(createTestConfig())

	ctx := context.Background()
	taskResult, err := handler.HandleTask(ctx, agenticloop.TaskMessage{
		TaskID: "task-all-empty",
		Role:   "general",
		Model:  "test-model",
		Prompt: "Test all empty names",
	})
	if err != nil {
		t.Fatalf("HandleTask() error = %v", err)
	}
	loopID := taskResult.LoopID

	response := agentic.AgentResponse{
		RequestID: "req-all-empty",
		Status:    "tool_call",
		Message: agentic.ChatMessage{
			Role: "assistant",
			ToolCalls: []agentic.ToolCall{
				{ID: "call-ae-1", Name: ""},
				{ID: "call-ae-2", Name: ""},
			},
		},
	}

	result, err := handler.HandleModelResponse(ctx, loopID, response)
	if err != nil {
		t.Fatalf("HandleModelResponse() error = %v", err)
	}

	// No tool.execute messages should be published
	toolCount := 0
	for _, msg := range result.PublishedMessages {
		if containsIgnoreCase(msg.Subject, "tool.execute") {
			toolCount++
		}
	}
	if toolCount != 0 {
		t.Errorf("Expected 0 tool.execute messages, got %d", toolCount)
	}

	// All empty → handleToolsComplete should fire → agent.request published
	requestCount := 0
	for _, msg := range result.PublishedMessages {
		if containsIgnoreCase(msg.Subject, "agent.request") {
			requestCount++
		}
	}
	if requestCount == 0 {
		t.Error("All-empty-name tool calls should trigger handleToolsComplete and publish agent.request")
	}
}

// --- Conversation context regression tests ---

// TestHandleToolsComplete_FullConversationHistory verifies that the next
// agent.request after tool completion includes the full conversation history
// (user prompt, assistant tool_call message, and tool results) — not just
// tool results. Regression test for Gemini INVALID_ARGUMENT 400 errors.
func TestHandleToolsComplete_FullConversationHistory(t *testing.T) {
	handler := agenticloop.NewMessageHandler(createTestConfig())

	ctx := context.Background()
	taskResult, err := handler.HandleTask(ctx, agenticloop.TaskMessage{
		TaskID: "task-full-ctx",
		Role:   "general",
		Model:  "test-model",
		Prompt: "Analyze the system",
	})
	if err != nil {
		t.Fatalf("HandleTask() error = %v", err)
	}
	loopID := taskResult.LoopID

	// Model response with tool calls (empty content — typical for tool_call responses)
	toolResponse := agentic.AgentResponse{
		RequestID: "req-ctx-001",
		Status:    "tool_call",
		Message: agentic.ChatMessage{
			Role:    "assistant",
			Content: "", // Empty content — this is the common case
			ToolCalls: []agentic.ToolCall{
				{ID: "call-ctx-1", Name: "get_weather"},
			},
		},
	}

	_, err = handler.HandleModelResponse(ctx, loopID, toolResponse)
	if err != nil {
		t.Fatalf("HandleModelResponse() error = %v", err)
	}

	// Tool result
	result, err := handler.HandleToolResult(ctx, loopID, agentic.ToolResult{
		CallID:  "call-ctx-1",
		Content: `{"temp": 20}`,
	})
	if err != nil {
		t.Fatalf("HandleToolResult() error = %v", err)
	}

	// Find the follow-up agent.request and validate conversation structure
	for _, msg := range result.PublishedMessages {
		if !containsIgnoreCase(msg.Subject, "agent.request") {
			continue
		}

		var envelope map[string]any
		if err := json.Unmarshal(msg.Data, &envelope); err != nil {
			t.Fatalf("Failed to unmarshal envelope: %v", err)
		}
		payload, ok := envelope["payload"].(map[string]any)
		if !ok {
			t.Fatal("Expected payload in BaseMessage envelope")
		}
		messages, ok := payload["messages"].([]any)
		if !ok {
			t.Fatal("Expected messages array")
		}

		// Must have at least: user message + assistant tool_call + tool result
		if len(messages) < 3 {
			t.Errorf("Expected at least 3 messages (user + assistant + tool), got %d", len(messages))
			for i, m := range messages {
				msg, _ := m.(map[string]any)
				t.Logf("  message[%d]: role=%v", i, msg["role"])
			}
			return
		}

		// Verify conversation structure and chronological ordering
		var hasUser, hasAssistant, hasTool bool
		var assistantIdx, toolIdx int
		for i, m := range messages {
			msg, _ := m.(map[string]any)
			role, _ := msg["role"].(string)
			switch role {
			case "user":
				hasUser = true
			case "assistant":
				hasAssistant = true
				assistantIdx = i
				// The assistant message should have tool_calls
				if tc, ok := msg["tool_calls"]; ok {
					tcs, _ := tc.([]any)
					if len(tcs) == 0 {
						t.Error("Assistant message should have tool_calls")
					}
				}
			case "tool":
				hasTool = true
				toolIdx = i
			}
		}

		if !hasUser {
			t.Error("Conversation must include user message")
		}
		if !hasAssistant {
			t.Error("Conversation must include assistant tool_call message")
		}
		if !hasTool {
			t.Error("Conversation must include tool result message")
		}
		// Tool results must follow their assistant tool_call message (chronological)
		if hasTool && hasAssistant && toolIdx <= assistantIdx {
			t.Errorf("Tool result (index %d) must come after assistant tool_call (index %d)", toolIdx, assistantIdx)
		}
		return
	}
	t.Error("Should publish agent.request after tool completion")
}

// TestHandleToolResult_PopulatesToolNameAndArguments verifies that trajectory
// steps from HandleToolResult include ToolName and ToolArguments.
func TestHandleToolResult_PopulatesToolNameAndArguments(t *testing.T) {
	handler := agenticloop.NewMessageHandler(createTestConfig())

	ctx := context.Background()
	taskResult, err := handler.HandleTask(ctx, agenticloop.TaskMessage{
		TaskID: "task-tool-args",
		Role:   "general",
		Model:  "qwen-32b",
		Prompt: "Test tool args",
	})
	if err != nil {
		t.Fatalf("HandleTask() error = %v", err)
	}
	loopID := taskResult.LoopID

	// Model response with a tool call that has arguments
	toolResponse := agentic.AgentResponse{
		RequestID: "req-args-001",
		Status:    "tool_call",
		Message: agentic.ChatMessage{
			Role: "assistant",
			ToolCalls: []agentic.ToolCall{
				{
					ID:        "call-args-001",
					Name:      "graph_query",
					Arguments: map[string]any{"query": "SELECT *", "limit": float64(10)},
				},
			},
		},
	}

	_, err = handler.HandleModelResponse(ctx, loopID, toolResponse)
	if err != nil {
		t.Fatalf("HandleModelResponse() error = %v", err)
	}

	// Tool result
	result, err := handler.HandleToolResult(ctx, loopID, agentic.ToolResult{
		CallID:  "call-args-001",
		Content: "42 results",
	})
	if err != nil {
		t.Fatalf("HandleToolResult() error = %v", err)
	}

	// Verify trajectory step has ToolName and ToolArguments populated
	if len(result.TrajectorySteps) == 0 {
		t.Fatal("Expected trajectory steps")
	}
	step := result.TrajectorySteps[0]
	if step.ToolName != "graph_query" {
		t.Errorf("TrajectoryStep.ToolName = %q, want %q", step.ToolName, "graph_query")
	}
	if step.ToolArguments == nil {
		t.Fatal("TrajectoryStep.ToolArguments should not be nil")
	}
	if step.ToolArguments["query"] != "SELECT *" {
		t.Errorf("TrajectoryStep.ToolArguments[query] = %v, want %q", step.ToolArguments["query"], "SELECT *")
	}
}

// TestHandleTask_TrajectoryDetail_Full verifies that when TrajectoryDetail is "full",
// the trajectory step from HandleTask includes Messages and Model.
func TestHandleTask_TrajectoryDetail_Full(t *testing.T) {
	config := createTestConfig()
	config.TrajectoryDetail = "full"
	handler := agenticloop.NewMessageHandler(config)

	ctx := context.Background()
	result, err := handler.HandleTask(ctx, agenticloop.TaskMessage{
		TaskID: "task-detail",
		Role:   "general",
		Model:  "qwen-32b",
		Prompt: "Full detail test",
	})
	if err != nil {
		t.Fatalf("HandleTask() error = %v", err)
	}

	if len(result.TrajectorySteps) == 0 {
		t.Fatal("Expected trajectory steps")
	}
	step := result.TrajectorySteps[0]

	// Messages should be populated
	if len(step.Messages) == 0 {
		t.Error("TrajectoryStep.Messages should be populated in full detail mode")
	}
	// Model should be populated
	if step.Model != "qwen-32b" {
		t.Errorf("TrajectoryStep.Model = %q, want %q", step.Model, "qwen-32b")
	}
}

// TestHandleTask_TrajectoryDetail_Default verifies that with default config,
// Messages and Model are NOT populated on trajectory steps.
func TestHandleTask_TrajectoryDetail_Default(t *testing.T) {
	handler := agenticloop.NewMessageHandler(createTestConfig())

	ctx := context.Background()
	result, err := handler.HandleTask(ctx, agenticloop.TaskMessage{
		TaskID: "task-summary",
		Role:   "general",
		Model:  "qwen-32b",
		Prompt: "Summary mode test",
	})
	if err != nil {
		t.Fatalf("HandleTask() error = %v", err)
	}

	if len(result.TrajectorySteps) == 0 {
		t.Fatal("Expected trajectory steps")
	}
	step := result.TrajectorySteps[0]

	// Messages should NOT be populated in summary mode
	if len(step.Messages) != 0 {
		t.Errorf("TrajectoryStep.Messages should be nil/empty in summary mode, got %d", len(step.Messages))
	}
	// Model should NOT be populated in summary mode
	if step.Model != "" {
		t.Errorf("TrajectoryStep.Model should be empty in summary mode, got %q", step.Model)
	}
}

// TestHandleModelResponse_TrajectoryDetail_Full verifies that when TrajectoryDetail
// is "full", the trajectory step from HandleModelResponse includes ToolCalls and Model.
func TestHandleModelResponse_TrajectoryDetail_Full(t *testing.T) {
	config := createTestConfig()
	config.TrajectoryDetail = "full"
	handler := agenticloop.NewMessageHandler(config)

	ctx := context.Background()
	taskResult, err := handler.HandleTask(ctx, agenticloop.TaskMessage{
		TaskID: "task-detail-resp",
		Role:   "general",
		Model:  "qwen-32b",
		Prompt: "Detail response test",
	})
	if err != nil {
		t.Fatalf("HandleTask() error = %v", err)
	}
	loopID := taskResult.LoopID

	// Model response with tool calls
	response := agentic.AgentResponse{
		RequestID: "req-detail-001",
		Status:    "tool_call",
		Message: agentic.ChatMessage{
			Role: "assistant",
			ToolCalls: []agentic.ToolCall{
				{ID: "call-detail-1", Name: "test_tool"},
			},
		},
		TokenUsage: agentic.TokenUsage{PromptTokens: 100, CompletionTokens: 50},
	}

	result, err := handler.HandleModelResponse(ctx, loopID, response)
	if err != nil {
		t.Fatalf("HandleModelResponse() error = %v", err)
	}

	if len(result.TrajectorySteps) == 0 {
		t.Fatal("Expected trajectory steps")
	}
	step := result.TrajectorySteps[0]

	// ToolCalls should be populated in full mode
	if len(step.ToolCalls) == 0 {
		t.Error("TrajectoryStep.ToolCalls should be populated in full detail mode")
	}
	if step.ToolCalls[0].Name != "test_tool" {
		t.Errorf("TrajectoryStep.ToolCalls[0].Name = %q, want %q", step.ToolCalls[0].Name, "test_tool")
	}
	// Model should be populated
	if step.Model != "qwen-32b" {
		t.Errorf("TrajectoryStep.Model = %q, want %q", step.Model, "qwen-32b")
	}
}
