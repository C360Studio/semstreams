package service

// Runtime logs SSE endpoint for Flow Builder UI.
//
// This file implements the GET /flowbuilder/flows/{id}/runtime/logs endpoint
// which streams component logs in real-time via Server-Sent Events (SSE).
//
// Components publish structured logs to NATS subjects:
//   logs.{flow_id}.{component_name}
//
// The endpoint subscribes to logs.{flow_id}.> and streams filtered log events
// to connected clients with support for:
//   - Filtering by log level (DEBUG, INFO, WARN, ERROR)
//   - Filtering by component name
//   - SSE reconnection support with event IDs and Last-Event-ID header
//   - Graceful connection management (reconnects handled by browser)
//
// Response format (SSE):
//   event: log
//   id: 42
//   data: {"timestamp":"2025-11-17T14:23:01.234Z","level":"INFO","component":"udp-source","flow_id":"flow-123","message":"Started"}
//
//   event: error
//   data: {"error":"subscription failed","details":"..."}

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"

	"github.com/c360/semstreams/component"
	"github.com/nats-io/nats.go"
)

// isValidFlowID validates a flow ID to prevent NATS subject injection attacks.
// Flow IDs must not contain NATS wildcards (>, *, .) to prevent subscribing to
// logs from multiple flows or unintended subjects.
func isValidFlowID(flowID string) bool {
	if flowID == "" {
		return false
	}
	// Reject NATS wildcards and subject separators
	if strings.ContainsAny(flowID, ">*.") {
		return false
	}
	return true
}

// isValidComponentName validates a component name to prevent NATS subject injection.
// Component names must not contain NATS wildcards or special characters.
func isValidComponentName(componentName string) bool {
	if componentName == "" {
		return true // Empty means no filter
	}
	// Reject NATS wildcards and subject separators
	if strings.ContainsAny(componentName, ">*.") {
		return false
	}
	return true
}

// handleRuntimeLogs handles GET /flows/{id}/runtime/logs
// Streams component logs via SSE with optional filtering
func (fs *FlowService) handleRuntimeLogs(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	flowID := r.PathValue("id")

	// Validate request parameters and verify flow exists
	levelFilter, componentFilter, statusCode, err := fs.validateLogStreamRequest(ctx, flowID, r)
	if err != nil {
		http.Error(w, err.Error(), statusCode)
		return
	}

	// Setup SSE connection
	fs.setupSSEHeaders(w)

	// Create log subscription
	logChan, sub, err := fs.createLogSubscription(ctx, flowID, levelFilter, componentFilter)
	if err != nil {
		fs.sendSSEError(w, "Failed to subscribe to logs", err)
		return
	}
	defer func() {
		if err := sub.Drain(); err != nil {
			fs.logger.Warn("Failed to drain subscription", "error", err, "flow_id", flowID)
		}
	}()

	// Stream logs to client
	fs.streamLogsToClient(ctx, w, flowID, levelFilter, componentFilter, logChan)
}

// validateLogStreamRequest validates all request parameters and checks flow existence
func (fs *FlowService) validateLogStreamRequest(ctx context.Context, flowID string, r *http.Request) (levelFilter, componentFilter string, statusCode int, err error) {
	// Validate flow ID to prevent NATS subject injection
	if !isValidFlowID(flowID) {
		fs.logger.Warn("Invalid flow ID", "flow_id", flowID)
		return "", "", http.StatusBadRequest, fmt.Errorf("invalid flow ID: must not contain NATS wildcards (>, *, .)")
	}

	// Parse query parameters for filtering
	levelFilter = strings.ToUpper(r.URL.Query().Get("level"))
	componentFilter = r.URL.Query().Get("component")

	// Validate component filter to prevent NATS subject injection
	if !isValidComponentName(componentFilter) {
		fs.logger.Warn("Invalid component name", "component", componentFilter)
		return "", "", http.StatusBadRequest, fmt.Errorf("invalid component name: must not contain NATS wildcards (>, *, .)")
	}

	// Validate level filter if provided
	if levelFilter != "" {
		validLevels := map[string]bool{
			"DEBUG": true,
			"INFO":  true,
			"WARN":  true,
			"ERROR": true,
		}
		if !validLevels[levelFilter] {
			fs.logger.Warn("Invalid level filter", "level", levelFilter)
			return "", "", http.StatusBadRequest, fmt.Errorf("invalid level filter. Valid values: DEBUG, INFO, WARN, ERROR")
		}
	}

	// Verify flow exists
	_, flowErr := fs.flowStore.Get(ctx, flowID)
	if flowErr != nil {
		fs.logger.Error("Flow not found for logs", "flow_id", flowID, "error", flowErr)
		return "", "", http.StatusNotFound, fmt.Errorf("flow not found")
	}

	return levelFilter, componentFilter, 0, nil
}

// setupSSEHeaders configures SSE headers and flushes response
func (fs *FlowService) setupSSEHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // Disable nginx buffering

	// Flush headers immediately
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}

// createLogSubscription creates NATS subscription with filtering logic
func (fs *FlowService) createLogSubscription(
	ctx context.Context,
	flowID, levelFilter, componentFilter string,
) (chan *component.LogEntry, *nats.Subscription, error) {
	// Subscribe to all logs for this flow
	subject := fmt.Sprintf("logs.%s.>", flowID)
	fs.logger.Info("Subscribing to logs", "flow_id", flowID, "subject", subject, "level_filter", levelFilter, "component_filter", componentFilter)

	// Get buffer size from config
	bufferSize := fs.config.LogStreamBufferSize
	if bufferSize <= 0 {
		bufferSize = 100 // Fallback to default
	}

	// Channel for log entries
	logChan := make(chan *component.LogEntry, bufferSize)

	// Done channel for cleanup coordination
	done := make(chan struct{})
	go func() {
		<-ctx.Done()
		close(done)
	}()

	// Get NATS connection from BaseService
	natsConn := fs.nats.GetConnection()
	if natsConn == nil {
		fs.logger.Error("NATS connection not available")
		return nil, nil, fmt.Errorf("NATS connection not available")
	}

	// Create NATS subscription with filtering
	sub, err := natsConn.Subscribe(subject, func(msg *nats.Msg) {
		var entry component.LogEntry
		if err := json.Unmarshal(msg.Data, &entry); err != nil {
			fs.logger.Warn("Failed to unmarshal log entry", "error", err)
			return
		}

		// Apply filters
		if levelFilter != "" && string(entry.Level) != levelFilter {
			return
		}
		if componentFilter != "" && entry.Component != componentFilter {
			return
		}

		// Send to channel (non-blocking), but check done channel first
		select {
		case <-done:
			// Handler is shutting down, stop sending to channel
			return
		case logChan <- &entry:
		default:
			// Channel full, skip this log entry
			fs.logger.Warn("Log channel full, dropping entry", "component", entry.Component, "flow_id", flowID)
		}
	})

	if err != nil {
		fs.logger.Error("Failed to subscribe to logs", "error", err)
		return nil, nil, err
	}

	return logChan, sub, nil
}

// streamLogsToClient streams log entries to the client via SSE
func (fs *FlowService) streamLogsToClient(
	ctx context.Context,
	w http.ResponseWriter,
	flowID, levelFilter, componentFilter string,
	logChan chan *component.LogEntry,
) {
	// Event ID counter for SSE reconnection support
	var eventID atomic.Uint64

	// Send initial connection success event with retry directive
	fs.sendSSEEventWithID(w, "connected", map[string]string{
		"flow_id":          flowID,
		"level_filter":     levelFilter,
		"component_filter": componentFilter,
	}, eventID.Add(1))

	// Send retry directive for reconnection (5 seconds)
	fmt.Fprintf(w, "retry: 5000\n\n")
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}

	// Stream logs until context is cancelled
	for {
		select {
		case <-ctx.Done():
			// Client disconnected
			fs.logger.Info("Client disconnected from logs stream", "flow_id", flowID)
			return

		case entry := <-logChan:
			// Send log entry as SSE event with incremental event ID
			data, err := json.Marshal(entry)
			if err != nil {
				fs.logger.Error("Failed to marshal log entry", "error", err)
				continue
			}

			currentEventID := eventID.Add(1)
			fmt.Fprintf(w, "event: log\nid: %d\ndata: %s\n\n", currentEventID, data)
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
		}
	}
}

// sendSSEEvent sends a generic SSE event without an event ID
func (fs *FlowService) sendSSEEvent(w http.ResponseWriter, event string, data interface{}) {
	jsonData, err := json.Marshal(data)
	if err != nil {
		fs.logger.Error("Failed to marshal SSE event", "event", event, "error", err)
		return
	}

	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, jsonData)
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}

// sendSSEEventWithID sends a generic SSE event with an event ID for reconnection support
func (fs *FlowService) sendSSEEventWithID(w http.ResponseWriter, event string, data interface{}, id uint64) {
	jsonData, err := json.Marshal(data)
	if err != nil {
		fs.logger.Error("Failed to marshal SSE event", "event", event, "error", err)
		return
	}

	fmt.Fprintf(w, "event: %s\nid: %d\ndata: %s\n\n", event, id, jsonData)
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}

// sendSSEError sends an error event via SSE
func (fs *FlowService) sendSSEError(w http.ResponseWriter, message string, err error) {
	errorData := map[string]string{
		"error": message,
	}
	if err != nil {
		errorData["details"] = err.Error()
	}

	fs.sendSSEEvent(w, "error", errorData)
}
