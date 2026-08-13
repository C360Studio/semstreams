package otel

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/internal/agentterminal"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/payloadregistry"
)

// SpanData represents collected span information.
type SpanData struct {
	// TraceID is the trace identifier.
	TraceID string `json:"trace_id"`

	// SpanID is the span identifier.
	SpanID string `json:"span_id"`

	// ParentSpanID is the parent span identifier.
	ParentSpanID string `json:"parent_span_id,omitempty"`

	// Name is the span name.
	Name string `json:"name"`

	// Kind is the span kind (client, server, internal, producer, consumer).
	Kind string `json:"kind"`

	// StartTime is when the span started.
	StartTime time.Time `json:"start_time"`

	// EndTime is when the span ended.
	EndTime time.Time `json:"end_time,omitempty"`

	// Status indicates the span status.
	Status SpanStatus `json:"status"`

	// Attributes are span attributes.
	Attributes map[string]any `json:"attributes,omitempty"`

	// Events are span events.
	Events []SpanEvent `json:"events,omitempty"`

	// Links are span links.
	Links []SpanLink `json:"links,omitempty"`
}

// SpanStatus represents the status of a span.
type SpanStatus struct {
	// Code is the status code (unset, ok, error).
	Code string `json:"code"`

	// Message is an optional status message.
	Message string `json:"message,omitempty"`
}

// SpanEvent represents an event within a span.
type SpanEvent struct {
	// Name is the event name.
	Name string `json:"name"`

	// Timestamp is when the event occurred.
	Timestamp time.Time `json:"timestamp"`

	// Attributes are event attributes.
	Attributes map[string]any `json:"attributes,omitempty"`
}

// SpanLink represents a link to another span.
type SpanLink struct {
	// TraceID is the linked trace ID.
	TraceID string `json:"trace_id"`

	// SpanID is the linked span ID.
	SpanID string `json:"span_id"`

	// Attributes are link attributes.
	Attributes map[string]any `json:"attributes,omitempty"`
}

// AgentEvent represents an agent lifecycle event from NATS.
type AgentEvent struct {
	// Type is the event type (loop.created, loop.completed, loop.failed, etc.)
	Type string `json:"type"`

	// LoopID is the agent loop identifier.
	LoopID string `json:"loop_id"`

	// TaskID is the task identifier (for task events).
	TaskID string `json:"task_id,omitempty"`

	// ToolName is the tool name (for tool events).
	ToolName string `json:"tool_name,omitempty"`

	// Timestamp is when the event occurred.
	Timestamp time.Time `json:"timestamp"`

	// EntityID is the agent's entity ID.
	EntityID string `json:"entity_id,omitempty"`

	// Role is the agent's role.
	Role string `json:"role,omitempty"`

	// Error is the error message for failure events.
	Error string `json:"error,omitempty"`

	// Duration is the operation duration (for completion events).
	Duration time.Duration `json:"duration,omitempty"`

	// Metadata contains additional event metadata.
	Metadata map[string]any `json:"metadata,omitempty"`
}

// SpanCollector collects spans from agent events.
type SpanCollector struct {
	mu sync.RWMutex

	// Active spans indexed by loop/task ID
	activeSpans map[string]*SpanData

	// Completed spans ready for export
	completedSpans []*SpanData

	// Service information
	serviceName    string
	serviceVersion string

	// Sampling
	samplingRate float64
	decoder      *message.Decoder

	// Counters
	spansCreated   int64
	spansCompleted int64
	spansDropped   int64
}

// NewSpanCollector creates a new span collector.
func NewSpanCollector(serviceName, serviceVersion string, samplingRate float64) *SpanCollector {
	reg := payloadregistry.New()
	if err := agentic.RegisterPayloads(reg); err != nil {
		panic(fmt.Sprintf("otel: register agentic payloads: %v", err))
	}
	return newSpanCollector(serviceName, serviceVersion, samplingRate, message.NewDecoder(reg))
}

func newSpanCollector(serviceName, serviceVersion string, samplingRate float64, decoder *message.Decoder) *SpanCollector {
	return &SpanCollector{
		activeSpans:    make(map[string]*SpanData),
		completedSpans: make([]*SpanData, 0),
		serviceName:    serviceName,
		serviceVersion: serviceVersion,
		samplingRate:   samplingRate,
		decoder:        decoder,
	}
}

// ProcessEvent processes an agent event and creates/updates spans.
func (sc *SpanCollector) ProcessEvent(_ context.Context, data []byte) error {
	var event AgentEvent
	if err := json.Unmarshal(data, &event); err != nil {
		return err
	}

	switch event.Type {
	case "loop.created":
		sc.startLoopSpan(&event)
	case "loop.completed":
		sc.endLoopSpan(&event, "ok", "")
	case "loop.failed":
		sc.endLoopSpan(&event, "error", event.Error)
	case "task.started":
		sc.startTaskSpan(&event)
	case "task.completed":
		sc.endTaskSpan(&event, "ok", "")
	case "task.failed":
		sc.endTaskSpan(&event, "error", event.Error)
	case "tool.started":
		sc.startToolSpan(&event)
	case "tool.completed":
		sc.endToolSpan(&event, "ok", "")
	case "tool.failed":
		sc.endToolSpan(&event, "error", event.Error)
	default:
		// Add as event to parent span
		sc.addEventToSpan(&event)
	}

	return nil
}

// ProcessMessage processes a BaseMessage envelope published by the agentic loop.
// It dispatches on the message category to create or update spans.
func (sc *SpanCollector) ProcessMessage(_ context.Context, subject string, data []byte) error {
	var envelope struct {
		Type struct {
			Category string `json:"category"`
		} `json:"type"`
		Category string          `json:"category"`
		Payload  json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return err
	}

	// Classification only decides whether this broad input must cross the
	// shared terminal boundary. Subject and flat discriminators never decide
	// semantics; Decode remains the sole interpreter and rejects bad wire shapes.
	if isTerminalCategory(envelope.Type.Category) || isTerminalCategory(envelope.Category) ||
		strings.HasPrefix(subject, "agent.complete.") || strings.HasPrefix(subject, "agent.failed.") {
		terminal, err := agentterminal.Decode(sc.decoder, data)
		if err != nil {
			return err
		}
		sc.endLoopSpanTerminal(terminal)
		return nil
	}

	switch envelope.Type.Category {
	case agentic.CategoryLoopCreated:
		var evt agentic.LoopCreatedEvent
		if err := json.Unmarshal(envelope.Payload, &evt); err != nil {
			return err
		}
		sc.startLoopSpanFromEvent(&evt)
	case agentic.CategoryToolResult:
		var evt agentic.ToolResult
		if err := json.Unmarshal(envelope.Payload, &evt); err != nil {
			return err
		}
		sc.createToolSpanFromResult(&evt)
	case agentic.CategoryContextEvent:
		var evt agentic.ContextEvent
		if err := json.Unmarshal(envelope.Payload, &evt); err != nil {
			return err
		}
		sc.addContextEventToSpan(&evt)
	}

	return nil
}

func isTerminalCategory(category string) bool {
	switch category {
	case agentic.CategoryLoopCompleted, agentic.CategoryLoopFailed, agentic.CategoryLoopCancelled:
		return true
	default:
		return false
	}
}

func (sc *SpanCollector) endLoopSpanTerminal(evt agentterminal.Event) {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	span, ok := sc.activeSpans[evt.LoopID]
	if !ok {
		return
	}
	span.EndTime = evt.TerminalAt
	span.Attributes["agent.outcome"] = evt.Outcome
	if evt.Model != "" {
		span.Attributes["agent.model"] = evt.Model
	}
	if evt.Role != "" {
		span.Attributes["agent.role"] = evt.Role
	}
	if evt.Iterations != 0 {
		span.Attributes["agent.iterations"] = evt.Iterations
	}
	if evt.TokensIn != 0 {
		span.Attributes["agent.tokens_in"] = evt.TokensIn
	}
	if evt.TokensOut != 0 {
		span.Attributes["agent.tokens_out"] = evt.TokensOut
	}
	if evt.Prompt != "" {
		span.Attributes["agent.prompt"] = evt.Prompt
	}
	if evt.WorkflowSlug != "" {
		span.Attributes["agent.workflow_slug"] = evt.WorkflowSlug
	}
	if evt.WorkflowStep != "" {
		span.Attributes["agent.workflow_step"] = evt.WorkflowStep
	}

	switch evt.Class {
	case agentterminal.ClassSucceeded:
		span.Status = SpanStatus{Code: "ok"}
	case agentterminal.ClassFailed:
		span.Status = SpanStatus{Code: "error", Message: evt.Error}
		span.Attributes["agent.error"] = evt.Error
		span.Attributes["agent.reason"] = evt.Reason
	case agentterminal.ClassCancelled:
		span.Status = SpanStatus{Code: "error", Message: "cancelled"}
		span.Attributes["agent.cancelled_by"] = evt.CancelledBy
	}

	delete(sc.activeSpans, evt.LoopID)
	sc.completedSpans = append(sc.completedSpans, span)
	sc.spansCompleted++
}

// startLoopSpanFromEvent creates a root span from a typed LoopCreatedEvent.
func (sc *SpanCollector) startLoopSpanFromEvent(evt *agentic.LoopCreatedEvent) {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	span := &SpanData{
		TraceID:   generateTraceID(evt.LoopID),
		SpanID:    generateSpanID(evt.LoopID),
		Name:      "agent.loop",
		Kind:      "server",
		StartTime: evt.CreatedAt,
		Status:    SpanStatus{Code: "unset"},
		Attributes: map[string]any{
			"agent.loop_id":   evt.LoopID,
			"agent.task_id":   evt.TaskID,
			"agent.role":      evt.Role,
			"agent.model":     evt.Model,
			"service.name":    sc.serviceName,
			"service.version": sc.serviceVersion,
		},
	}

	if evt.WorkflowSlug != "" {
		span.Attributes["agent.workflow_slug"] = evt.WorkflowSlug
	}
	if evt.WorkflowStep != "" {
		span.Attributes["agent.workflow_step"] = evt.WorkflowStep
	}

	for k, v := range evt.Metadata {
		span.Attributes["agent."+k] = v
	}

	sc.activeSpans[evt.LoopID] = span
	sc.spansCreated++
}

// createToolSpanFromResult creates a self-contained tool span from a ToolResult.
// Tool results are point-in-time events; the span start and end both use current time.
func (sc *SpanCollector) createToolSpanFromResult(evt *agentic.ToolResult) {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	// Look up parent loop span to inherit trace context.
	parentSpan := sc.activeSpans[evt.LoopID]

	now := time.Now()
	span := &SpanData{
		Name:      "agent.tool." + evt.Name,
		Kind:      "client",
		StartTime: now,
		EndTime:   now,
		Attributes: map[string]any{
			"tool.name":   evt.Name,
			"tool.status": toolStatus(evt.Error),
		},
	}

	if parentSpan != nil {
		span.TraceID = parentSpan.TraceID
		span.ParentSpanID = parentSpan.SpanID
	} else {
		span.TraceID = generateTraceID(evt.LoopID)
	}
	span.SpanID = generateSpanID(evt.LoopID + ":tool:" + evt.Name + ":" + evt.CallID)

	if evt.ErrorKind != "" {
		span.Attributes["tool.error_kind"] = string(evt.ErrorKind)
	}
	if evt.Error != "" {
		span.Attributes["tool.error"] = evt.Error
		span.Status = SpanStatus{Code: "error", Message: evt.Error}
	} else {
		span.Status = SpanStatus{Code: "ok"}
	}
	if evt.LoopID != "" {
		span.Attributes["agent.loop_id"] = evt.LoopID
	}

	sc.completedSpans = append(sc.completedSpans, span)
	sc.spansCreated++
	sc.spansCompleted++
}

// addContextEventToSpan records a ContextEvent as a span event on the active loop span.
func (sc *SpanCollector) addContextEventToSpan(evt *agentic.ContextEvent) {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	span := sc.activeSpans[evt.LoopID]
	if span == nil {
		return
	}

	attrs := map[string]any{
		"context.type":      evt.Type,
		"context.iteration": evt.Iteration,
	}
	if evt.Utilization > 0 {
		attrs["context.utilization"] = evt.Utilization
	}
	if evt.TokensSaved > 0 {
		attrs["context.tokens_saved"] = evt.TokensSaved
	}
	if evt.Summary != "" {
		attrs["context.summary"] = evt.Summary
	}

	span.Events = append(span.Events, SpanEvent{
		Name:       evt.Type,
		Timestamp:  time.Now(),
		Attributes: attrs,
	})
}

// toolStatus returns "success" or "error" based on whether an error string is set.
func toolStatus(errStr string) string {
	if errStr == "" {
		return "success"
	}
	return "error"
}

// startLoopSpan creates a new root span for an agent loop.
func (sc *SpanCollector) startLoopSpan(event *AgentEvent) {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	span := &SpanData{
		TraceID:   generateTraceID(event.LoopID),
		SpanID:    generateSpanID(event.LoopID),
		Name:      "agent.loop",
		Kind:      "server",
		StartTime: event.Timestamp,
		Status:    SpanStatus{Code: "unset"},
		Attributes: map[string]any{
			"agent.loop_id":   event.LoopID,
			"agent.entity_id": event.EntityID,
			"agent.role":      event.Role,
			"service.name":    sc.serviceName,
			"service.version": sc.serviceVersion,
		},
	}

	// Add metadata as attributes
	for k, v := range event.Metadata {
		span.Attributes["agent."+k] = v
	}

	sc.activeSpans[event.LoopID] = span
	sc.spansCreated++
}

// endLoopSpan completes a loop span.
func (sc *SpanCollector) endLoopSpan(event *AgentEvent, statusCode, statusMsg string) {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	span, ok := sc.activeSpans[event.LoopID]
	if !ok {
		return
	}

	span.EndTime = event.Timestamp
	span.Status = SpanStatus{Code: statusCode, Message: statusMsg}

	if event.Duration > 0 {
		span.Attributes["agent.duration_ms"] = event.Duration.Milliseconds()
	}

	delete(sc.activeSpans, event.LoopID)
	sc.completedSpans = append(sc.completedSpans, span)
	sc.spansCompleted++
}

// startTaskSpan creates a child span for a task.
func (sc *SpanCollector) startTaskSpan(event *AgentEvent) {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	// Find parent loop span
	parentSpan, ok := sc.activeSpans[event.LoopID]
	if !ok {
		return
	}

	spanKey := event.LoopID + ":" + event.TaskID
	span := &SpanData{
		TraceID:      parentSpan.TraceID,
		SpanID:       generateSpanID(spanKey),
		ParentSpanID: parentSpan.SpanID,
		Name:         "agent.task",
		Kind:         "internal",
		StartTime:    event.Timestamp,
		Status:       SpanStatus{Code: "unset"},
		Attributes: map[string]any{
			"agent.loop_id": event.LoopID,
			"agent.task_id": event.TaskID,
		},
	}

	for k, v := range event.Metadata {
		span.Attributes["task."+k] = v
	}

	sc.activeSpans[spanKey] = span
	sc.spansCreated++
}

// endTaskSpan completes a task span.
func (sc *SpanCollector) endTaskSpan(event *AgentEvent, statusCode, statusMsg string) {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	spanKey := event.LoopID + ":" + event.TaskID
	span, ok := sc.activeSpans[spanKey]
	if !ok {
		return
	}

	span.EndTime = event.Timestamp
	span.Status = SpanStatus{Code: statusCode, Message: statusMsg}

	if event.Duration > 0 {
		span.Attributes["task.duration_ms"] = event.Duration.Milliseconds()
	}

	delete(sc.activeSpans, spanKey)
	sc.completedSpans = append(sc.completedSpans, span)
	sc.spansCompleted++
}

// startToolSpan creates a child span for a tool execution.
func (sc *SpanCollector) startToolSpan(event *AgentEvent) {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	// Find parent task or loop span
	parentKey := event.LoopID
	if event.TaskID != "" {
		parentKey = event.LoopID + ":" + event.TaskID
	}

	parentSpan, ok := sc.activeSpans[parentKey]
	if !ok {
		// Try loop span as parent
		parentSpan, ok = sc.activeSpans[event.LoopID]
		if !ok {
			return
		}
	}

	spanKey := event.LoopID + ":tool:" + event.ToolName
	span := &SpanData{
		TraceID:      parentSpan.TraceID,
		SpanID:       generateSpanID(spanKey),
		ParentSpanID: parentSpan.SpanID,
		Name:         "agent.tool." + event.ToolName,
		Kind:         "client",
		StartTime:    event.Timestamp,
		Status:       SpanStatus{Code: "unset"},
		Attributes: map[string]any{
			"agent.loop_id":  event.LoopID,
			"agent.task_id":  event.TaskID,
			"tool.name":      event.ToolName,
			"tool.timestamp": event.Timestamp.Format(time.RFC3339),
		},
	}

	for k, v := range event.Metadata {
		span.Attributes["tool."+k] = v
	}

	sc.activeSpans[spanKey] = span
	sc.spansCreated++
}

// endToolSpan completes a tool span.
func (sc *SpanCollector) endToolSpan(event *AgentEvent, statusCode, statusMsg string) {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	spanKey := event.LoopID + ":tool:" + event.ToolName
	span, ok := sc.activeSpans[spanKey]
	if !ok {
		return
	}

	span.EndTime = event.Timestamp
	span.Status = SpanStatus{Code: statusCode, Message: statusMsg}

	if event.Duration > 0 {
		span.Attributes["tool.duration_ms"] = event.Duration.Milliseconds()
	}

	delete(sc.activeSpans, spanKey)
	sc.completedSpans = append(sc.completedSpans, span)
	sc.spansCompleted++
}

// addEventToSpan adds an event to an active span.
func (sc *SpanCollector) addEventToSpan(event *AgentEvent) {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	// Find parent span
	var span *SpanData
	if event.TaskID != "" {
		spanKey := event.LoopID + ":" + event.TaskID
		span = sc.activeSpans[spanKey]
	}
	if span == nil {
		span = sc.activeSpans[event.LoopID]
	}
	if span == nil {
		return
	}

	spanEvent := SpanEvent{
		Name:       event.Type,
		Timestamp:  event.Timestamp,
		Attributes: event.Metadata,
	}
	span.Events = append(span.Events, spanEvent)
}

// FlushCompleted returns and clears completed spans.
func (sc *SpanCollector) FlushCompleted() []*SpanData {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	spans := sc.completedSpans
	sc.completedSpans = make([]*SpanData, 0)
	return spans
}

// Stats returns collector statistics.
func (sc *SpanCollector) Stats() map[string]int64 {
	sc.mu.RLock()
	defer sc.mu.RUnlock()

	return map[string]int64{
		"spans_created":   sc.spansCreated,
		"spans_completed": sc.spansCompleted,
		"spans_dropped":   sc.spansDropped,
		"active_spans":    int64(len(sc.activeSpans)),
		"pending_spans":   int64(len(sc.completedSpans)),
	}
}

// generateTraceID generates a trace ID from a loop ID.
func generateTraceID(loopID string) string {
	// Use a deterministic hash for trace ID based on loop ID
	// In production, this would use proper trace ID generation
	return hashToHex(loopID, 32)
}

// generateSpanID generates a span ID from a key.
func generateSpanID(key string) string {
	// Use a deterministic hash for span ID
	return hashToHex(key, 16)
}

// hashToHex creates a hex string of specified length from a key.
func hashToHex(key string, length int) string {
	// Simple deterministic hash for testing
	// In production, use crypto/rand or proper OTEL SDK
	h := uint64(0)
	for _, c := range key {
		h = h*31 + uint64(c)
	}

	hex := make([]byte, length)
	for i := 0; i < length; i++ {
		nibble := (h >> (uint(i) * 4)) & 0xf
		if nibble < 10 {
			hex[length-1-i] = byte('0' + nibble)
		} else {
			hex[length-1-i] = byte('a' + nibble - 10)
		}
	}
	return string(hex)
}
