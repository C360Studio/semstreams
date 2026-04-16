package otel

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
)

func TestNewSpanCollector(t *testing.T) {
	sc := NewSpanCollector("test-service", "1.0.0", 1.0)

	if sc == nil {
		t.Fatal("expected span collector, got nil")
	}

	if sc.serviceName != "test-service" {
		t.Errorf("expected service name 'test-service', got %q", sc.serviceName)
	}

	if sc.serviceVersion != "1.0.0" {
		t.Errorf("expected service version '1.0.0', got %q", sc.serviceVersion)
	}

	if sc.samplingRate != 1.0 {
		t.Errorf("expected sampling rate 1.0, got %f", sc.samplingRate)
	}
}

func TestSpanCollectorProcessLoopEvents(t *testing.T) {
	sc := NewSpanCollector("test-service", "1.0.0", 1.0)
	ctx := context.Background()
	now := time.Now()

	// Create loop
	createEvent := AgentEvent{
		Type:      "loop.created",
		LoopID:    "loop-001",
		Timestamp: now,
		EntityID:  "agent.test.001",
		Role:      "architect",
	}
	data, _ := json.Marshal(createEvent)
	if err := sc.ProcessEvent(ctx, data); err != nil {
		t.Fatalf("ProcessEvent failed: %v", err)
	}

	// Verify active span
	stats := sc.Stats()
	if stats["active_spans"] != 1 {
		t.Errorf("expected 1 active span, got %d", stats["active_spans"])
	}
	if stats["spans_created"] != 1 {
		t.Errorf("expected 1 span created, got %d", stats["spans_created"])
	}

	// Complete loop
	completeEvent := AgentEvent{
		Type:      "loop.completed",
		LoopID:    "loop-001",
		Timestamp: now.Add(5 * time.Second),
		Duration:  5 * time.Second,
	}
	data, _ = json.Marshal(completeEvent)
	if err := sc.ProcessEvent(ctx, data); err != nil {
		t.Fatalf("ProcessEvent failed: %v", err)
	}

	// Verify span completed
	stats = sc.Stats()
	if stats["active_spans"] != 0 {
		t.Errorf("expected 0 active spans, got %d", stats["active_spans"])
	}
	if stats["spans_completed"] != 1 {
		t.Errorf("expected 1 span completed, got %d", stats["spans_completed"])
	}
	if stats["pending_spans"] != 1 {
		t.Errorf("expected 1 pending span, got %d", stats["pending_spans"])
	}

	// Flush and verify span data
	spans := sc.FlushCompleted()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}

	span := spans[0]
	if span.Name != "agent.loop" {
		t.Errorf("expected span name 'agent.loop', got %q", span.Name)
	}
	if span.Status.Code != "ok" {
		t.Errorf("expected status 'ok', got %q", span.Status.Code)
	}
	if span.Attributes["agent.loop_id"] != "loop-001" {
		t.Errorf("expected loop_id 'loop-001', got %v", span.Attributes["agent.loop_id"])
	}
}

func TestSpanCollectorProcessTaskEvents(t *testing.T) {
	sc := NewSpanCollector("test-service", "1.0.0", 1.0)
	ctx := context.Background()
	now := time.Now()

	// Create parent loop first
	createLoop := AgentEvent{
		Type:      "loop.created",
		LoopID:    "loop-002",
		Timestamp: now,
	}
	data, _ := json.Marshal(createLoop)
	_ = sc.ProcessEvent(ctx, data)

	// Start task
	startTask := AgentEvent{
		Type:      "task.started",
		LoopID:    "loop-002",
		TaskID:    "task-001",
		Timestamp: now.Add(time.Second),
	}
	data, _ = json.Marshal(startTask)
	if err := sc.ProcessEvent(ctx, data); err != nil {
		t.Fatalf("ProcessEvent failed: %v", err)
	}

	// Verify task span created
	stats := sc.Stats()
	if stats["active_spans"] != 2 {
		t.Errorf("expected 2 active spans, got %d", stats["active_spans"])
	}

	// Complete task
	completeTask := AgentEvent{
		Type:      "task.completed",
		LoopID:    "loop-002",
		TaskID:    "task-001",
		Timestamp: now.Add(2 * time.Second),
		Duration:  time.Second,
	}
	data, _ = json.Marshal(completeTask)
	if err := sc.ProcessEvent(ctx, data); err != nil {
		t.Fatalf("ProcessEvent failed: %v", err)
	}

	// Complete loop
	completeLoop := AgentEvent{
		Type:      "loop.completed",
		LoopID:    "loop-002",
		Timestamp: now.Add(3 * time.Second),
	}
	data, _ = json.Marshal(completeLoop)
	_ = sc.ProcessEvent(ctx, data)

	// Verify spans
	spans := sc.FlushCompleted()
	if len(spans) != 2 {
		t.Fatalf("expected 2 spans, got %d", len(spans))
	}

	// Find task span
	var taskSpan *SpanData
	for _, s := range spans {
		if s.Name == "agent.task" {
			taskSpan = s
			break
		}
	}

	if taskSpan == nil {
		t.Fatal("task span not found")
	}

	if taskSpan.ParentSpanID == "" {
		t.Error("expected task span to have parent")
	}
	if taskSpan.Attributes["agent.task_id"] != "task-001" {
		t.Errorf("expected task_id 'task-001', got %v", taskSpan.Attributes["agent.task_id"])
	}
}

func TestSpanCollectorProcessToolEvents(t *testing.T) {
	sc := NewSpanCollector("test-service", "1.0.0", 1.0)
	ctx := context.Background()
	now := time.Now()

	// Create parent loop
	createLoop := AgentEvent{
		Type:      "loop.created",
		LoopID:    "loop-003",
		Timestamp: now,
	}
	data, _ := json.Marshal(createLoop)
	_ = sc.ProcessEvent(ctx, data)

	// Start tool
	startTool := AgentEvent{
		Type:      "tool.started",
		LoopID:    "loop-003",
		ToolName:  "code_search",
		Timestamp: now.Add(time.Second),
	}
	data, _ = json.Marshal(startTool)
	if err := sc.ProcessEvent(ctx, data); err != nil {
		t.Fatalf("ProcessEvent failed: %v", err)
	}

	// Complete tool
	completeTool := AgentEvent{
		Type:      "tool.completed",
		LoopID:    "loop-003",
		ToolName:  "code_search",
		Timestamp: now.Add(2 * time.Second),
		Duration:  time.Second,
	}
	data, _ = json.Marshal(completeTool)
	if err := sc.ProcessEvent(ctx, data); err != nil {
		t.Fatalf("ProcessEvent failed: %v", err)
	}

	// Complete loop
	completeLoop := AgentEvent{
		Type:      "loop.completed",
		LoopID:    "loop-003",
		Timestamp: now.Add(3 * time.Second),
	}
	data, _ = json.Marshal(completeLoop)
	_ = sc.ProcessEvent(ctx, data)

	// Verify spans
	spans := sc.FlushCompleted()
	if len(spans) != 2 {
		t.Fatalf("expected 2 spans, got %d", len(spans))
	}

	// Find tool span
	var toolSpan *SpanData
	for _, s := range spans {
		if s.Name == "agent.tool.code_search" {
			toolSpan = s
			break
		}
	}

	if toolSpan == nil {
		t.Fatal("tool span not found")
	}

	if toolSpan.Kind != "client" {
		t.Errorf("expected tool span kind 'client', got %q", toolSpan.Kind)
	}
	if toolSpan.Attributes["tool.name"] != "code_search" {
		t.Errorf("expected tool.name 'code_search', got %v", toolSpan.Attributes["tool.name"])
	}
}

func TestSpanCollectorFailedSpans(t *testing.T) {
	sc := NewSpanCollector("test-service", "1.0.0", 1.0)
	ctx := context.Background()
	now := time.Now()

	// Create loop
	createLoop := AgentEvent{
		Type:      "loop.created",
		LoopID:    "loop-004",
		Timestamp: now,
	}
	data, _ := json.Marshal(createLoop)
	_ = sc.ProcessEvent(ctx, data)

	// Fail loop
	failLoop := AgentEvent{
		Type:      "loop.failed",
		LoopID:    "loop-004",
		Timestamp: now.Add(time.Second),
		Error:     "LLM timeout",
	}
	data, _ = json.Marshal(failLoop)
	if err := sc.ProcessEvent(ctx, data); err != nil {
		t.Fatalf("ProcessEvent failed: %v", err)
	}

	spans := sc.FlushCompleted()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}

	span := spans[0]
	if span.Status.Code != "error" {
		t.Errorf("expected status 'error', got %q", span.Status.Code)
	}
	if span.Status.Message != "LLM timeout" {
		t.Errorf("expected status message 'LLM timeout', got %q", span.Status.Message)
	}
}

func TestSpanCollectorAddEventToSpan(t *testing.T) {
	sc := NewSpanCollector("test-service", "1.0.0", 1.0)
	ctx := context.Background()
	now := time.Now()

	// Create loop
	createLoop := AgentEvent{
		Type:      "loop.created",
		LoopID:    "loop-005",
		Timestamp: now,
	}
	data, _ := json.Marshal(createLoop)
	_ = sc.ProcessEvent(ctx, data)

	// Add unknown event (should be added as span event)
	unknownEvent := AgentEvent{
		Type:      "model.response.received",
		LoopID:    "loop-005",
		Timestamp: now.Add(time.Second),
		Metadata: map[string]any{
			"tokens": 150,
		},
	}
	data, _ = json.Marshal(unknownEvent)
	if err := sc.ProcessEvent(ctx, data); err != nil {
		t.Fatalf("ProcessEvent failed: %v", err)
	}

	// Complete loop
	completeLoop := AgentEvent{
		Type:      "loop.completed",
		LoopID:    "loop-005",
		Timestamp: now.Add(2 * time.Second),
	}
	data, _ = json.Marshal(completeLoop)
	_ = sc.ProcessEvent(ctx, data)

	spans := sc.FlushCompleted()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}

	span := spans[0]
	if len(span.Events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(span.Events))
	}

	event := span.Events[0]
	if event.Name != "model.response.received" {
		t.Errorf("expected event name 'model.response.received', got %q", event.Name)
	}
}

func TestSpanCollectorFlushCompleted(t *testing.T) {
	sc := NewSpanCollector("test-service", "1.0.0", 1.0)
	ctx := context.Background()
	now := time.Now()

	// Create and complete multiple loops
	for i := 0; i < 3; i++ {
		loopID := "loop-batch-" + string(rune('a'+i))
		create := AgentEvent{
			Type:      "loop.created",
			LoopID:    loopID,
			Timestamp: now,
		}
		data, _ := json.Marshal(create)
		_ = sc.ProcessEvent(ctx, data)

		complete := AgentEvent{
			Type:      "loop.completed",
			LoopID:    loopID,
			Timestamp: now.Add(time.Second),
		}
		data, _ = json.Marshal(complete)
		_ = sc.ProcessEvent(ctx, data)
	}

	// First flush
	spans := sc.FlushCompleted()
	if len(spans) != 3 {
		t.Errorf("expected 3 spans, got %d", len(spans))
	}

	// Second flush should be empty
	spans = sc.FlushCompleted()
	if len(spans) != 0 {
		t.Errorf("expected 0 spans after second flush, got %d", len(spans))
	}
}

func TestGenerateTraceID(t *testing.T) {
	traceID := generateTraceID("loop-001")

	if len(traceID) != 32 {
		t.Errorf("expected trace ID length 32, got %d", len(traceID))
	}

	// Should be deterministic
	traceID2 := generateTraceID("loop-001")
	if traceID != traceID2 {
		t.Errorf("expected deterministic trace ID, got %q and %q", traceID, traceID2)
	}

	// Different input should give different ID
	traceID3 := generateTraceID("loop-002")
	if traceID == traceID3 {
		t.Error("expected different trace IDs for different inputs")
	}
}

func TestGenerateSpanID(t *testing.T) {
	spanID := generateSpanID("span-001")

	if len(spanID) != 16 {
		t.Errorf("expected span ID length 16, got %d", len(spanID))
	}

	// Should be deterministic
	spanID2 := generateSpanID("span-001")
	if spanID != spanID2 {
		t.Errorf("expected deterministic span ID, got %q and %q", spanID, spanID2)
	}
}

func TestSpanCollectorInvalidJSON(t *testing.T) {
	sc := NewSpanCollector("test-service", "1.0.0", 1.0)
	ctx := context.Background()

	err := sc.ProcessEvent(ctx, []byte("not valid json"))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

// ---- ProcessMessage tests ----

// wrapPayload wraps a typed payload in a minimal BaseMessage envelope JSON.
func wrapPayload(t *testing.T, category string, payload any) []byte {
	t.Helper()
	p, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	env := map[string]any{
		"type": map[string]string{
			"domain":   "agentic",
			"category": category,
		},
		"payload": json.RawMessage(p),
	}
	data, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	return data
}

func TestProcessMessage_LoopCreated(t *testing.T) {
	sc := NewSpanCollector("test-service", "1.0.0", 1.0)
	ctx := context.Background()
	now := time.Now()

	evt := agentic.LoopCreatedEvent{
		LoopID:    "loop-msg-001",
		TaskID:    "task-001",
		Role:      "developer",
		Model:     "gpt-4o",
		CreatedAt: now,
	}
	data := wrapPayload(t, agentic.CategoryLoopCreated, evt)

	if err := sc.ProcessMessage(ctx, "agent.loop.created", data); err != nil {
		t.Fatalf("ProcessMessage failed: %v", err)
	}

	stats := sc.Stats()
	if stats["active_spans"] != 1 {
		t.Errorf("expected 1 active span, got %d", stats["active_spans"])
	}
	if stats["spans_created"] != 1 {
		t.Errorf("expected 1 span created, got %d", stats["spans_created"])
	}

	// Verify span attributes are populated
	sc.mu.RLock()
	span := sc.activeSpans["loop-msg-001"]
	sc.mu.RUnlock()

	if span == nil {
		t.Fatal("expected active span for loop-msg-001")
	}
	if span.Attributes["agent.role"] != "developer" {
		t.Errorf("expected role 'developer', got %v", span.Attributes["agent.role"])
	}
	if span.Attributes["agent.model"] != "gpt-4o" {
		t.Errorf("expected model 'gpt-4o', got %v", span.Attributes["agent.model"])
	}
}

func TestProcessMessage_LoopCompleted(t *testing.T) {
	sc := NewSpanCollector("test-service", "1.0.0", 1.0)
	ctx := context.Background()
	now := time.Now()

	// First create the loop
	created := agentic.LoopCreatedEvent{
		LoopID:    "loop-msg-002",
		TaskID:    "task-002",
		Role:      "architect",
		Model:     "claude-3-5-sonnet",
		CreatedAt: now,
	}
	_ = sc.ProcessMessage(ctx, "agent.loop.created", wrapPayload(t, agentic.CategoryLoopCreated, created))

	// Now complete it with beta.5 fields
	completed := agentic.LoopCompletedEvent{
		LoopID:      "loop-msg-002",
		TaskID:      "task-002",
		Outcome:     agentic.OutcomeSuccess,
		Role:        "architect",
		Result:      "Implementation complete",
		Prompt:      "Design the authentication system",
		Model:       "claude-3-5-sonnet",
		Iterations:  3,
		TokensIn:    1200,
		TokensOut:   800,
		CompletedAt: now.Add(10 * time.Second),
	}
	if err := sc.ProcessMessage(ctx, "agent.loop.completed", wrapPayload(t, agentic.CategoryLoopCompleted, completed)); err != nil {
		t.Fatalf("ProcessMessage failed: %v", err)
	}

	spans := sc.FlushCompleted()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}

	span := spans[0]
	if span.Status.Code != "ok" {
		t.Errorf("expected status 'ok', got %q", span.Status.Code)
	}
	if span.Attributes["agent.model"] != "claude-3-5-sonnet" {
		t.Errorf("expected model 'claude-3-5-sonnet', got %v", span.Attributes["agent.model"])
	}
	if span.Attributes["agent.iterations"] != 3 {
		t.Errorf("expected iterations 3, got %v", span.Attributes["agent.iterations"])
	}
	if span.Attributes["agent.tokens_in"] != 1200 {
		t.Errorf("expected tokens_in 1200, got %v", span.Attributes["agent.tokens_in"])
	}
	if span.Attributes["agent.tokens_out"] != 800 {
		t.Errorf("expected tokens_out 800, got %v", span.Attributes["agent.tokens_out"])
	}
	if span.Attributes["agent.prompt"] != "Design the authentication system" {
		t.Errorf("expected prompt set, got %v", span.Attributes["agent.prompt"])
	}
	if span.Attributes["agent.outcome"] != agentic.OutcomeSuccess {
		t.Errorf("expected outcome 'success', got %v", span.Attributes["agent.outcome"])
	}
}

func TestProcessMessage_LoopFailed(t *testing.T) {
	sc := NewSpanCollector("test-service", "1.0.0", 1.0)
	ctx := context.Background()
	now := time.Now()

	created := agentic.LoopCreatedEvent{
		LoopID: "loop-msg-003", TaskID: "task-003",
		Role: "developer", Model: "gpt-4o", CreatedAt: now,
	}
	_ = sc.ProcessMessage(ctx, "agent.loop.created", wrapPayload(t, agentic.CategoryLoopCreated, created))

	failed := agentic.LoopFailedEvent{
		LoopID:     "loop-msg-003",
		TaskID:     "task-003",
		Outcome:    agentic.OutcomeFailed,
		Reason:     "max_iterations",
		Error:      "exceeded iteration limit",
		Role:       "developer",
		Model:      "gpt-4o",
		Iterations: 10,
		TokensIn:   5000,
		TokensOut:  3000,
		FailedAt:   now.Add(30 * time.Second),
	}
	if err := sc.ProcessMessage(ctx, "agent.loop.failed", wrapPayload(t, agentic.CategoryLoopFailed, failed)); err != nil {
		t.Fatalf("ProcessMessage failed: %v", err)
	}

	spans := sc.FlushCompleted()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}

	span := spans[0]
	if span.Status.Code != "error" {
		t.Errorf("expected status 'error', got %q", span.Status.Code)
	}
	if span.Attributes["agent.error"] != "exceeded iteration limit" {
		t.Errorf("expected agent.error set, got %v", span.Attributes["agent.error"])
	}
	if span.Attributes["agent.reason"] != "max_iterations" {
		t.Errorf("expected agent.reason 'max_iterations', got %v", span.Attributes["agent.reason"])
	}
}

func TestProcessMessage_ToolResult(t *testing.T) {
	sc := NewSpanCollector("test-service", "1.0.0", 1.0)
	ctx := context.Background()
	now := time.Now()

	// Create a parent loop span first
	created := agentic.LoopCreatedEvent{
		LoopID: "loop-msg-004", TaskID: "task-004",
		Role: "developer", Model: "gpt-4o", CreatedAt: now,
	}
	_ = sc.ProcessMessage(ctx, "agent.loop.created", wrapPayload(t, agentic.CategoryLoopCreated, created))

	// Tool result with error classification
	toolResult := agentic.ToolResult{
		CallID:    "call-001",
		Name:      "code_search",
		Content:   "",
		Error:     "connection refused",
		ErrorKind: agentic.ToolErrorNetwork,
		LoopID:    "loop-msg-004",
	}
	if err := sc.ProcessMessage(ctx, "tool.result.code_search", wrapPayload(t, agentic.CategoryToolResult, toolResult)); err != nil {
		t.Fatalf("ProcessMessage failed: %v", err)
	}

	// Tool span is immediately completed; loop span still active
	stats := sc.Stats()
	if stats["active_spans"] != 1 {
		t.Errorf("expected 1 active loop span, got %d", stats["active_spans"])
	}
	if stats["pending_spans"] != 1 {
		t.Errorf("expected 1 pending tool span, got %d", stats["pending_spans"])
	}

	spans := sc.FlushCompleted()
	if len(spans) != 1 {
		t.Fatalf("expected 1 tool span, got %d", len(spans))
	}

	span := spans[0]
	if span.Name != "agent.tool.code_search" {
		t.Errorf("expected span name 'agent.tool.code_search', got %q", span.Name)
	}
	if span.Attributes["tool.name"] != "code_search" {
		t.Errorf("expected tool.name 'code_search', got %v", span.Attributes["tool.name"])
	}
	if span.Attributes["tool.status"] != "error" {
		t.Errorf("expected tool.status 'error', got %v", span.Attributes["tool.status"])
	}
	if span.Attributes["tool.error_kind"] != string(agentic.ToolErrorNetwork) {
		t.Errorf("expected tool.error_kind %q, got %v", agentic.ToolErrorNetwork, span.Attributes["tool.error_kind"])
	}
	if span.Attributes["tool.error"] != "connection refused" {
		t.Errorf("expected tool.error set, got %v", span.Attributes["tool.error"])
	}
	// Tool result span inherits trace ID from parent
	sc.mu.RLock()
	parentSpan := sc.activeSpans["loop-msg-004"]
	sc.mu.RUnlock()
	if parentSpan != nil && span.TraceID != parentSpan.TraceID {
		t.Errorf("expected tool span trace ID %q to match parent trace ID %q", span.TraceID, parentSpan.TraceID)
	}
}

func TestProcessMessage_ToolResultSuccess(t *testing.T) {
	sc := NewSpanCollector("test-service", "1.0.0", 1.0)
	ctx := context.Background()

	toolResult := agentic.ToolResult{
		CallID:  "call-002",
		Name:    "file_write",
		Content: "wrote 42 bytes",
		LoopID:  "loop-orphan",
	}
	if err := sc.ProcessMessage(ctx, "tool.result.file_write", wrapPayload(t, agentic.CategoryToolResult, toolResult)); err != nil {
		t.Fatalf("ProcessMessage failed: %v", err)
	}

	spans := sc.FlushCompleted()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	if spans[0].Attributes["tool.status"] != "success" {
		t.Errorf("expected tool.status 'success', got %v", spans[0].Attributes["tool.status"])
	}
	if spans[0].Status.Code != "ok" {
		t.Errorf("expected status code 'ok', got %q", spans[0].Status.Code)
	}
}

func TestProcessMessage_ContextEvent(t *testing.T) {
	sc := NewSpanCollector("test-service", "1.0.0", 1.0)
	ctx := context.Background()
	now := time.Now()

	// Create parent loop
	created := agentic.LoopCreatedEvent{
		LoopID: "loop-msg-005", TaskID: "task-005",
		Role: "developer", Model: "gpt-4o", CreatedAt: now,
	}
	_ = sc.ProcessMessage(ctx, "agent.loop.created", wrapPayload(t, agentic.CategoryLoopCreated, created))

	ctxEvt := agentic.ContextEvent{
		Type:        agentic.ContextEventCompactionComplete,
		LoopID:      "loop-msg-005",
		Iteration:   4,
		Utilization: 0.62,
		TokensSaved: 1500,
		Summary:     "Compacted 3 iterations",
	}
	if err := sc.ProcessMessage(ctx, "agent.context.event", wrapPayload(t, agentic.CategoryContextEvent, ctxEvt)); err != nil {
		t.Fatalf("ProcessMessage failed: %v", err)
	}

	// Complete the loop to get the span
	completed := agentic.LoopCompletedEvent{
		LoopID: "loop-msg-005", TaskID: "task-005",
		Outcome: agentic.OutcomeSuccess, CompletedAt: now.Add(5 * time.Second),
	}
	_ = sc.ProcessMessage(ctx, "agent.loop.completed", wrapPayload(t, agentic.CategoryLoopCompleted, completed))

	spans := sc.FlushCompleted()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	if len(spans[0].Events) != 1 {
		t.Fatalf("expected 1 span event, got %d", len(spans[0].Events))
	}
	ev := spans[0].Events[0]
	if ev.Name != agentic.ContextEventCompactionComplete {
		t.Errorf("expected event name %q, got %q", agentic.ContextEventCompactionComplete, ev.Name)
	}
	if ev.Attributes["context.tokens_saved"] != 1500 {
		t.Errorf("expected tokens_saved 1500, got %v", ev.Attributes["context.tokens_saved"])
	}
}

func TestProcessMessage_InvalidJSON(t *testing.T) {
	sc := NewSpanCollector("test-service", "1.0.0", 1.0)
	ctx := context.Background()

	err := sc.ProcessMessage(ctx, "agent.loop.created", []byte("not valid json"))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestProcessMessage_UnknownCategory(t *testing.T) {
	sc := NewSpanCollector("test-service", "1.0.0", 1.0)
	ctx := context.Background()

	// Unknown categories are silently ignored (no error, no span created)
	env := map[string]any{
		"type":    map[string]string{"domain": "agentic", "category": "unknown_category"},
		"payload": json.RawMessage(`{}`),
	}
	data, _ := json.Marshal(env)

	if err := sc.ProcessMessage(ctx, "agent.unknown", data); err != nil {
		t.Errorf("unexpected error for unknown category: %v", err)
	}
	if sc.Stats()["spans_created"] != 0 {
		t.Error("expected no spans created for unknown category")
	}
}
