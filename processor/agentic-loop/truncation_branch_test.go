package agenticloop_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	agenticloop "github.com/c360studio/semstreams/processor/agentic-loop"
)

// fillContextToHighUtilization adds messages to the loop's context
// manager until utilization is at or above the supplied threshold.
// The default agentic-loop config has CompactThreshold=0.60 and
// effective limit ~121600 tokens (128K limit minus 6400 headroom),
// so reaching 60% utilization requires ~73000 tokens. We append a
// few large user/assistant turns that approximate that load.
func fillContextToHighUtilization(t *testing.T, handler *agenticloop.MessageHandler, loopID string, atLeastTokens int) {
	t.Helper()
	cm := handler.GetContextManager(loopID)
	if cm == nil {
		t.Fatalf("no context manager for loop %s", loopID)
	}

	// Roughly 4 chars/token. Build big repeating-letter blobs and
	// add them as alternating user/assistant turns until target hit.
	const charsPerToken = 4
	chunkChars := 16000 * charsPerToken // ~16K tokens per message
	chunk := make([]byte, chunkChars)
	for i := range chunk {
		chunk[i] = 'a' + byte(i%26)
	}
	chunkStr := string(chunk)

	role := "user"
	for cm.TotalTokens() < atLeastTokens {
		_ = cm.AddMessage(agenticloop.RegionRecentHistory, agentic.ChatMessage{
			Role:    role,
			Content: chunkStr,
		})
		if role == "user" {
			role = "assistant"
		} else {
			role = "user"
		}
	}
}

// TestHandleLengthTruncation_FailFast_LowUtilization verifies the
// "structurally can't recover" branch: when utilization is below the
// compaction threshold and the response truncates, fail loud with a
// diagnostic message naming the actual numbers and
// compaction_attempted=false. No agent.request retry is emitted.
func TestHandleLengthTruncation_FailFast_LowUtilization(t *testing.T) {
	handler := agenticloop.NewMessageHandler(createTestConfig())
	ctx := context.Background()

	taskResult, err := handler.HandleTask(ctx, agenticloop.TaskMessage{
		TaskID: "task-failfast",
		Role:   "general",
		Model:  "qwen-32b",
		Prompt: "Test",
	})
	if err != nil {
		t.Fatalf("HandleTask: %v", err)
	}
	loopID := taskResult.LoopID

	// Default scenario after HandleTask: utilization is low (just the
	// task prompt). No additional context fill.

	resp := agentic.AgentResponse{
		RequestID:    "req-failfast",
		Status:       agentic.StatusLengthTruncated,
		FinishReason: agentic.FinishReasonLength,
		Message: agentic.ChatMessage{
			Role:    "assistant",
			Content: "partial output",
		},
		TokenUsage: agentic.TokenUsage{
			PromptTokens:     50,
			CompletionTokens: 4096,
		},
	}

	result, err := handler.HandleModelResponse(ctx, loopID, resp)
	if err != nil {
		t.Fatalf("HandleModelResponse: %v", err)
	}

	if result.State != agentic.LoopStateFailed {
		t.Errorf("State = %s, want %s", result.State, agentic.LoopStateFailed)
	}

	// No agent.request must be emitted on the fail-fast branch — the
	// loop should be terminating, not retrying.
	for _, msg := range result.PublishedMessages {
		if strings.HasPrefix(msg.Subject, "agent.request.") {
			t.Errorf("fail-fast path emitted agent.request: %s", msg.Subject)
		}
	}

	// Diagnostic message must name the actual numbers and indicate
	// compaction was NOT attempted.
	failed := failedEventFor(t, result)
	wantFragments := []string{
		"output budget too small for task",
		"without compaction attempted",
		"completion_tokens=4096",
		"model_limit=",
	}
	for _, frag := range wantFragments {
		if !strings.Contains(failed, frag) {
			t.Errorf("failure message missing %q; got: %s", frag, failed)
		}
	}
	if strings.Contains(failed, "after compaction") {
		t.Errorf("fail-fast message should not say 'after compaction'; got: %s", failed)
	}
	// Pin the low-utilization claim so a regression that inverted the
	// branch condition (sending a high-util case down the fail-fast
	// path) would still get caught.
	if !strings.Contains(failed, "truncated at 0%") {
		t.Errorf("fail-fast message should report low utilization (~0%%); got: %s", failed)
	}
}

// TestHandleLengthTruncation_ResetAfterForwardProgress verifies the
// retry counter is cleared on a normal advancement (StatusComplete /
// StatusToolCall) so a future truncation can self-heal once again.
// Sequence: high-util truncation → retry (counter=1) → tool_call
// response → counter resets → high-util truncation again → retry
// fires (not fails as second-strike). Catches a regression that
// dropped the reset from one of the forward-progress arms.
func TestHandleLengthTruncation_ResetAfterForwardProgress(t *testing.T) {
	handler := agenticloop.NewMessageHandler(createTestConfig())
	ctx := context.Background()

	taskResult, err := handler.HandleTask(ctx, agenticloop.TaskMessage{
		TaskID: "task-reset",
		Role:   "general",
		Model:  "qwen-32b",
		Prompt: "Test",
	})
	if err != nil {
		t.Fatalf("HandleTask: %v", err)
	}
	loopID := taskResult.LoopID

	// First truncation at high utilization → retry (counter=1).
	fillContextToHighUtilization(t, handler, loopID, 80000)
	first := agentic.AgentResponse{
		RequestID:    "req-reset-1",
		Status:       agentic.StatusLengthTruncated,
		FinishReason: agentic.FinishReasonLength,
		Message:      agentic.ChatMessage{Role: "assistant", Content: "p1"},
		TokenUsage:   agentic.TokenUsage{PromptTokens: 50, CompletionTokens: 4096},
	}
	if firstResult, err := handler.HandleModelResponse(ctx, loopID, first); err != nil {
		t.Fatalf("first HandleModelResponse: %v", err)
	} else if firstResult.State == agentic.LoopStateFailed {
		t.Fatalf("first call failed unexpectedly; state=%s", firstResult.State)
	}

	// Forward progress: a normal tool_call response. This must reset
	// the truncation retry counter.
	progress := agentic.AgentResponse{
		RequestID:    "req-reset-progress",
		Status:       agentic.StatusToolCall,
		FinishReason: "tool_calls",
		Message: agentic.ChatMessage{
			Role: "assistant",
			ToolCalls: []agentic.ToolCall{
				{ID: "call-1", Name: "noop", Arguments: map[string]any{}},
			},
		},
		TokenUsage: agentic.TokenUsage{PromptTokens: 100, CompletionTokens: 50},
	}
	if _, err := handler.HandleModelResponse(ctx, loopID, progress); err != nil {
		t.Fatalf("progress HandleModelResponse: %v", err)
	}

	// Re-fill context (compaction freed room earlier) and feed
	// another truncation. The counter should have been reset, so this
	// is "first truncation since progress" — retry must fire, not
	// second-strike-fail.
	fillContextToHighUtilization(t, handler, loopID, 80000)
	second := agentic.AgentResponse{
		RequestID:    "req-reset-2",
		Status:       agentic.StatusLengthTruncated,
		FinishReason: agentic.FinishReasonLength,
		Message:      agentic.ChatMessage{Role: "assistant", Content: "p2"},
		TokenUsage:   agentic.TokenUsage{PromptTokens: 50, CompletionTokens: 4096},
	}
	secondResult, err := handler.HandleModelResponse(ctx, loopID, second)
	if err != nil {
		t.Fatalf("second HandleModelResponse: %v", err)
	}

	if secondResult.State == agentic.LoopStateFailed {
		t.Errorf("post-reset truncation incorrectly hit second-strike fail; state=%s",
			secondResult.State)
	}
	var sawRequest bool
	for _, msg := range secondResult.PublishedMessages {
		if strings.HasPrefix(msg.Subject, "agent.request.") {
			sawRequest = true
		}
	}
	if !sawRequest {
		t.Errorf("post-reset truncation should fire a retry agent.request; got: %v",
			subjectsOf(secondResult.PublishedMessages))
	}
}

// TestHandleLengthTruncation_Retry_HighUtilization verifies the
// self-heal branch: when utilization is at/above the compaction
// threshold and the response truncates, the handler runs compaction
// inline, emits a compaction_retry ContextEvent + trajectory step,
// and re-emits agent.request from the now-smaller context. No
// failLoop should fire.
func TestHandleLengthTruncation_Retry_HighUtilization(t *testing.T) {
	handler := agenticloop.NewMessageHandler(createTestConfig())
	ctx := context.Background()

	taskResult, err := handler.HandleTask(ctx, agenticloop.TaskMessage{
		TaskID: "task-retry",
		Role:   "general",
		Model:  "qwen-32b",
		Prompt: "Test",
	})
	if err != nil {
		t.Fatalf("HandleTask: %v", err)
	}
	loopID := taskResult.LoopID

	// Push utilization above the 0.60 default threshold.
	fillContextToHighUtilization(t, handler, loopID, 80000)

	resp := agentic.AgentResponse{
		RequestID:    "req-retry",
		Status:       agentic.StatusLengthTruncated,
		FinishReason: agentic.FinishReasonLength,
		Message: agentic.ChatMessage{
			Role:    "assistant",
			Content: "partial output",
		},
		TokenUsage: agentic.TokenUsage{
			PromptTokens:     50,
			CompletionTokens: 4096,
		},
	}

	result, err := handler.HandleModelResponse(ctx, loopID, resp)
	if err != nil {
		t.Fatalf("HandleModelResponse: %v", err)
	}

	if result.State == agentic.LoopStateFailed {
		t.Fatalf("retry path failed the loop: result.State = %s", result.State)
	}

	// Must emit exactly one agent.request (the retry).
	var requestCount int
	for _, msg := range result.PublishedMessages {
		if strings.HasPrefix(msg.Subject, "agent.request.") {
			requestCount++
		}
	}
	if requestCount != 1 {
		t.Errorf("retry agent.request count = %d, want 1; subjects: %v",
			requestCount, subjectsOf(result.PublishedMessages))
	}

	// Must record a compaction_retry ContextEvent.
	var sawRetryEvent bool
	for _, ev := range result.ContextEvents {
		if ev.Type == agentic.ContextEventCompactionRetry {
			sawRetryEvent = true
			if ev.LoopID != loopID {
				t.Errorf("compaction_retry event LoopID = %q, want %q", ev.LoopID, loopID)
			}
			if ev.Utilization < 0.60 {
				t.Errorf("compaction_retry event Utilization = %.2f, want >= 0.60", ev.Utilization)
			}
		}
	}
	if !sawRetryEvent {
		t.Errorf("no compaction_retry ContextEvent recorded; got events: %+v", result.ContextEvents)
	}

	// Must record a context_compaction_retry trajectory step (distinct
	// from the routine pre-iteration "context_compaction" step type).
	var sawRetryStep bool
	for _, step := range result.TrajectorySteps {
		if step.StepType == "context_compaction_retry" {
			sawRetryStep = true
		}
	}
	if !sawRetryStep {
		t.Errorf("no context_compaction_retry trajectory step recorded")
	}
}

// TestHandleLengthTruncation_SecondTruncation_FailWithCompactionAttempted
// verifies the second-strike branch: when the loop already retried
// once (truncationRetryAttempts == 1 going in) and another truncation
// arrives, fail with the post-compaction diagnostic message saying
// compaction_attempted=true so the operator knows compaction did
// its job and the response still doesn't fit.
func TestHandleLengthTruncation_SecondTruncation_FailWithCompactionAttempted(t *testing.T) {
	handler := agenticloop.NewMessageHandler(createTestConfig())
	ctx := context.Background()

	taskResult, err := handler.HandleTask(ctx, agenticloop.TaskMessage{
		TaskID: "task-twice",
		Role:   "general",
		Model:  "qwen-32b",
		Prompt: "Test",
	})
	if err != nil {
		t.Fatalf("HandleTask: %v", err)
	}
	loopID := taskResult.LoopID

	// First truncation at high utilization → retry.
	fillContextToHighUtilization(t, handler, loopID, 80000)
	first := agentic.AgentResponse{
		RequestID:    "req-twice-1",
		Status:       agentic.StatusLengthTruncated,
		FinishReason: agentic.FinishReasonLength,
		Message:      agentic.ChatMessage{Role: "assistant", Content: "partial 1"},
		TokenUsage:   agentic.TokenUsage{PromptTokens: 50, CompletionTokens: 4096},
	}
	firstResult, err := handler.HandleModelResponse(ctx, loopID, first)
	if err != nil {
		t.Fatalf("first HandleModelResponse: %v", err)
	}
	if firstResult.State == agentic.LoopStateFailed {
		t.Fatalf("first call failed unexpectedly; state=%s", firstResult.State)
	}

	// Second truncation arrives — same iteration, retry counter is now 1.
	second := agentic.AgentResponse{
		RequestID:    "req-twice-2",
		Status:       agentic.StatusLengthTruncated,
		FinishReason: agentic.FinishReasonLength,
		Message:      agentic.ChatMessage{Role: "assistant", Content: "partial 2"},
		TokenUsage:   agentic.TokenUsage{PromptTokens: 50, CompletionTokens: 4096},
	}
	secondResult, err := handler.HandleModelResponse(ctx, loopID, second)
	if err != nil {
		t.Fatalf("second HandleModelResponse: %v", err)
	}

	if secondResult.State != agentic.LoopStateFailed {
		t.Errorf("second call state = %s, want %s (second truncation must fail)",
			secondResult.State, agentic.LoopStateFailed)
	}

	failed := failedEventFor(t, secondResult)
	if !strings.Contains(failed, "after compaction") {
		t.Errorf("second-truncation message should say 'after compaction'; got: %s", failed)
	}
	if !strings.Contains(failed, "completion_tokens=4096") {
		t.Errorf("second-truncation message missing completion_tokens; got: %s", failed)
	}
	// Must NOT confuse with the fail-fast message variant.
	if strings.Contains(failed, "without compaction attempted") {
		t.Errorf("second-truncation message wrongly says 'without compaction attempted'; got: %s", failed)
	}
}

// failedEventFor extracts the failure error message from a
// HandlerResult's published agent.failed envelope. Returns the
// payload.error string for substring assertions.
func failedEventFor(t *testing.T, result agenticloop.HandlerResult) string {
	t.Helper()
	for _, msg := range result.PublishedMessages {
		if !strings.HasPrefix(msg.Subject, "agent.failed.") {
			continue
		}
		var failed struct {
			Payload struct {
				Reason string `json:"reason"`
				Error  string `json:"error"`
			} `json:"payload"`
		}
		if err := json.Unmarshal(msg.Data, &failed); err != nil {
			t.Fatalf("decode failed envelope: %v", err)
		}
		return failed.Payload.Error
	}
	t.Fatalf("no agent.failed message found in result")
	return ""
}
