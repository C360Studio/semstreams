// Package agenticloop tests for rules-based workflow orchestration.
// ADR-018: Architect completion now produces enriched completion events
// for the rules engine to process, rather than directly spawning editor loops.

package agenticloop_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	agenticloop "github.com/c360studio/semstreams/processor/agentic-loop"
	"github.com/google/uuid"
)

func TestArchitectCompletion_ProducesEnrichedCompletionState(t *testing.T) {
	handler := agenticloop.NewMessageHandler(createTestConfig())

	// Create architect loop
	ctx := context.Background()
	taskResult, err := handler.HandleTask(ctx, agenticloop.TaskMessage{
		LoopID: uuid.NewString(),
		TaskID: "task-001",
		Role:   "architect",
		Model:  "qwen-32b",
		Prompt: "Design a distributed caching system",
	})
	if err != nil {
		t.Fatalf("HandleTask() error = %v", err)
	}

	architectLoopID := taskResult.LoopID

	// Architect completes
	architectOutput := "Architecture design: Use Redis cluster with consistent hashing..."
	response := agentic.AgentResponse{
		RequestID: "req-001",
		Status:    "complete",
		Message: agentic.ChatMessage{
			Role:    "assistant",
			Content: architectOutput,
		},
	}

	result, err := handler.HandleModelResponse(ctx, architectLoopID, response)
	if err != nil {
		t.Fatalf("HandleModelResponse() error = %v", err)
	}

	// Should mark architect loop as complete
	if result.State != agentic.LoopStateComplete {
		t.Errorf("Architect state = %s, want complete", result.State)
	}

	// Should have enriched completion state for rules engine
	if result.CompletionState == nil {
		t.Fatal("CompletionState should not be nil for completed loop")
	}

	// Verify completion state fields
	if result.CompletionState.Role != "architect" {
		t.Errorf("CompletionState.Role = %v, want architect", result.CompletionState.Role)
	}
	if result.CompletionState.Outcome != agentic.OutcomeSuccess {
		t.Errorf("CompletionState.Outcome = %v, want %s", result.CompletionState.Outcome, agentic.OutcomeSuccess)
	}
	if result.CompletionState.TaskID != "task-001" {
		t.Errorf("CompletionState.TaskID = %v, want task-001", result.CompletionState.TaskID)
	}
	if result.CompletionState.LoopID != architectLoopID {
		t.Errorf("CompletionState.LoopID = %v, want %s", result.CompletionState.LoopID, architectLoopID)
	}
	if result.CompletionState.Model != "qwen-32b" {
		t.Errorf("CompletionState.Model = %v, want qwen-32b", result.CompletionState.Model)
	}

	// Result should include the architect's output for rules engine
	if result.CompletionState.Result != architectOutput {
		t.Errorf("CompletionState.Result = %v, want %s", result.CompletionState.Result, architectOutput)
	}
}

func TestArchitectCompletion_PublishesAgentComplete(t *testing.T) {
	handler := agenticloop.NewMessageHandler(createTestConfig())

	ctx := context.Background()
	taskResult, err := handler.HandleTask(ctx, agenticloop.TaskMessage{
		LoopID: uuid.NewString(),
		TaskID: "task-001",
		Role:   "architect",
		Model:  "qwen-32b",
		Prompt: "Design system",
	})
	if err != nil {
		t.Fatalf("HandleTask() error = %v", err)
	}

	architectLoopID := taskResult.LoopID

	architectOutput := "Architecture: Microservices with event sourcing..."

	response := agentic.AgentResponse{
		RequestID: "req-001",
		Status:    "complete",
		Message: agentic.ChatMessage{
			Role:    "assistant",
			Content: architectOutput,
		},
	}

	result, err := handler.HandleModelResponse(ctx, architectLoopID, response)
	if err != nil {
		t.Fatalf("HandleModelResponse() error = %v", err)
	}

	// Should publish agent.complete with enriched data
	foundComplete := false
	for _, msg := range result.PublishedMessages {
		if containsIgnoreCase(msg.Subject, "agent.complete") {
			foundComplete = true

			// Parse the BaseMessage envelope
			var envelope map[string]any
			if err := json.Unmarshal(msg.Data, &envelope); err != nil {
				t.Fatalf("Failed to parse envelope message: %v", err)
			}

			// Extract payload from BaseMessage envelope
			completion, ok := envelope["payload"].(map[string]any)
			if !ok {
				t.Fatalf("Expected payload in BaseMessage envelope, got: %v", envelope)
			}

			// Verify enriched fields for rules engine
			if completion["role"] != "architect" {
				t.Errorf("completion[role] = %v, want architect", completion["role"])
			}
			if completion["outcome"] != "success" {
				t.Errorf("completion[outcome] = %v, want success", completion["outcome"])
			}
			if completion["result"] != architectOutput {
				t.Errorf("completion[result] = %v, want %s", completion["result"], architectOutput)
			}
			break
		}
	}

	if !foundComplete {
		t.Error("Should publish agent.complete message for architect completion")
	}
}

func TestEditorCompletion_DoesNotChain(t *testing.T) {
	handler := agenticloop.NewMessageHandler(createTestConfig())

	// Directly create an editor role loop
	ctx := context.Background()
	taskResult, err := handler.HandleTask(ctx, agenticloop.TaskMessage{
		LoopID: uuid.NewString(),
		TaskID: "task-001",
		Role:   "editor",
		Model:  "qwen-32b",
		Prompt: "Implement features",
	})
	if err != nil {
		t.Fatalf("HandleTask() error = %v", err)
	}

	editorLoopID := taskResult.LoopID

	response := agentic.AgentResponse{
		RequestID: "req-001",
		Status:    "complete",
		Message: agentic.ChatMessage{
			Role:    "assistant",
			Content: "Implementation done",
		},
	}

	result, err := handler.HandleModelResponse(ctx, editorLoopID, response)
	if err != nil {
		t.Fatalf("HandleModelResponse() error = %v", err)
	}

	// Editor should complete normally
	if result.State != agentic.LoopStateComplete {
		t.Errorf("Editor state = %s, want complete", result.State)
	}

	// Verify completion state has editor role
	if result.CompletionState == nil {
		t.Fatal("CompletionState should not be nil")
	}
	if result.CompletionState.Role != "editor" {
		t.Errorf("CompletionState.Role = %v, want editor", result.CompletionState.Role)
	}

	// Should publish agent.complete
	foundComplete := false
	for _, msg := range result.PublishedMessages {
		if containsIgnoreCase(msg.Subject, "agent.complete") {
			foundComplete = true
			break
		}
	}
	if !foundComplete {
		t.Error("Editor completion should publish agent.complete")
	}
}

func TestGeneralRoleCompletion_ProducesCompletionState(t *testing.T) {
	handler := agenticloop.NewMessageHandler(createTestConfig())

	ctx := context.Background()
	taskResult, err := handler.HandleTask(ctx, agenticloop.TaskMessage{
		LoopID: uuid.NewString(),
		TaskID: "task-001",
		Role:   "general",
		Model:  "qwen-32b",
		Prompt: "Answer question",
	})
	if err != nil {
		t.Fatalf("HandleTask() error = %v", err)
	}

	generalLoopID := taskResult.LoopID

	response := agentic.AgentResponse{
		RequestID: "req-001",
		Status:    "complete",
		Message: agentic.ChatMessage{
			Role:    "assistant",
			Content: "Here is the answer",
		},
	}

	result, err := handler.HandleModelResponse(ctx, generalLoopID, response)
	if err != nil {
		t.Fatalf("HandleModelResponse() error = %v", err)
	}

	// Should mark as complete
	if result.State != agentic.LoopStateComplete {
		t.Errorf("General loop state = %s, want complete", result.State)
	}

	// Should have completion state
	if result.CompletionState == nil {
		t.Fatal("CompletionState should not be nil")
	}
	if result.CompletionState.Role != "general" {
		t.Errorf("CompletionState.Role = %v, want general", result.CompletionState.Role)
	}
}

func TestArchitectWithToolCalls_CompletionAfterTools(t *testing.T) {
	handler := agenticloop.NewMessageHandler(createTestConfig())

	ctx := context.Background()
	taskResult, err := handler.HandleTask(ctx, agenticloop.TaskMessage{
		LoopID: uuid.NewString(),
		TaskID: "task-001",
		Role:   "architect",
		Model:  "qwen-32b",
		Prompt: "Design system",
	})
	if err != nil {
		t.Fatalf("HandleTask() error = %v", err)
	}

	architectLoopID := taskResult.LoopID

	// Architect makes tool calls (should work normally)
	toolResponse := agentic.AgentResponse{
		RequestID: "req-001",
		Status:    "tool_call",
		Message: agentic.ChatMessage{
			Role: "assistant",
			ToolCalls: []agentic.ToolCall{
				{
					ID:   "call-001",
					Name: "graph_query",
					Arguments: map[string]any{
						"query": "Find existing patterns",
					},
				},
			},
		},
	}

	toolResult, err := handler.HandleModelResponse(ctx, architectLoopID, toolResponse)
	if err != nil {
		t.Fatalf("HandleModelResponse() with tool call error = %v", err)
	}

	// Should handle tool call normally (no completion state yet)
	if toolResult.CompletionState != nil {
		t.Error("CompletionState should be nil on tool_call status")
	}

	// Complete tool execution
	_, err = handler.HandleToolResult(ctx, architectLoopID, agentic.ToolResult{
		CallID:  "call-001",
		Content: "Found patterns: ...",
	})
	if err != nil {
		t.Fatalf("HandleToolResult() error = %v", err)
	}

	// Now architect completes
	completeResponse := agentic.AgentResponse{
		RequestID: "req-002",
		Status:    "complete",
		Message: agentic.ChatMessage{
			Role:    "assistant",
			Content: "Architecture design complete",
		},
	}

	completeResult, err := handler.HandleModelResponse(ctx, architectLoopID, completeResponse)
	if err != nil {
		t.Fatalf("HandleModelResponse() complete error = %v", err)
	}

	// NOW should have completion state with enriched data
	if completeResult.CompletionState == nil {
		t.Fatal("CompletionState should not be nil after completion")
	}
	if completeResult.CompletionState.Role != "architect" {
		t.Errorf("CompletionState.Role = %v, want architect", completeResult.CompletionState.Role)
	}
	if completeResult.CompletionState.Outcome != agentic.OutcomeSuccess {
		t.Errorf("CompletionState.Outcome = %v, want %s", completeResult.CompletionState.Outcome, agentic.OutcomeSuccess)
	}
}

func TestArchitectFailure_NoCompletionState(t *testing.T) {
	handler := agenticloop.NewMessageHandler(createTestConfig())

	ctx := context.Background()
	architectTask, err := handler.HandleTask(ctx, agenticloop.TaskMessage{
		LoopID: uuid.NewString(),
		TaskID: "task-001",
		Role:   "architect",
		Model:  "qwen-32b",
		Prompt: "Design system",
	})
	if err != nil {
		t.Fatalf("HandleTask() error = %v", err)
	}

	// Architect fails
	response := agentic.AgentResponse{
		RequestID: "req-001",
		Status:    "error",
		Error:     "Model timeout",
	}

	result, err := handler.HandleModelResponse(ctx, architectTask.LoopID, response)
	if err != nil {
		t.Fatalf("HandleModelResponse() error = %v", err)
	}

	// Should not have completion state on error (rules shouldn't fire for failed completions)
	// The completion state is only set on successful completion
	if result.CompletionState != nil {
		t.Error("CompletionState should be nil when architect fails")
	}

	// Architect should be marked as failed
	if result.State != agentic.LoopStateFailed {
		t.Errorf("Architect state = %s, want failed", result.State)
	}
}

func TestCompletionState_IncludesIterations(t *testing.T) {
	handler := agenticloop.NewMessageHandler(createTestConfig())

	ctx := context.Background()
	taskResult, err := handler.HandleTask(ctx, agenticloop.TaskMessage{
		LoopID: uuid.NewString(),
		TaskID: "task-001",
		Role:   "architect",
		Model:  "qwen-32b",
		Prompt: "Design system",
	})
	if err != nil {
		t.Fatalf("HandleTask() error = %v", err)
	}

	loopID := taskResult.LoopID

	// Do one iteration with tool call
	_, err = handler.HandleModelResponse(ctx, loopID, agentic.AgentResponse{
		RequestID: "req-001",
		Status:    "tool_call",
		Message: agentic.ChatMessage{
			Role:      "assistant",
			ToolCalls: []agentic.ToolCall{{ID: "call-001", Name: "tool1"}},
		},
	})
	if err != nil {
		t.Fatalf("HandleModelResponse() error = %v", err)
	}

	_, err = handler.HandleToolResult(ctx, loopID, agentic.ToolResult{
		CallID:  "call-001",
		Content: "Result 1",
	})
	if err != nil {
		t.Fatalf("HandleToolResult() error = %v", err)
	}

	// Complete
	result, err := handler.HandleModelResponse(ctx, loopID, agentic.AgentResponse{
		RequestID: "req-002",
		Status:    "complete",
		Message: agentic.ChatMessage{
			Role:    "assistant",
			Content: "Done",
		},
	})
	if err != nil {
		t.Fatalf("HandleModelResponse() error = %v", err)
	}

	// Verify iterations are tracked
	if result.CompletionState == nil {
		t.Fatal("CompletionState should not be nil")
	}

	if result.CompletionState.Iterations < 1 {
		t.Errorf("iterations = %d, want >= 1", result.CompletionState.Iterations)
	}
}
