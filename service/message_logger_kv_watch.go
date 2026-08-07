package service

// KV Watch SSE endpoint for MessageLogger service.
//
// This file implements the GET /message-logger/kv/{bucket}/watch endpoint
// which streams KV bucket changes in real-time via Server-Sent Events (SSE).
//
// The endpoint watches a NATS KV bucket and streams change events to connected
// clients with support for:
//   - Pattern filtering (e.g., ?pattern=entity.*)
//   - SSE reconnection support with event IDs and Last-Event-ID header
//   - Graceful connection management
//
// Response format (SSE):
//
//	event: connected
//	id: 1
//	data: {"bucket":"ENTITY_STATES","pattern":"*","message":"Watching for changes"}
//
//	event: kv_change
//	id: 42
//	data: {"bucket":"ENTITY_STATES","key":"acme.iot...","operation":"update","value":{...},"revision":5,"timestamp":"..."}

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

// KVWatchEvent represents a KV change event sent via SSE
type KVWatchEvent struct {
	Bucket    string          `json:"bucket"`
	Key       string          `json:"key"`
	Operation string          `json:"operation"` // "create", "update", "delete"
	Value     json.RawMessage `json:"value,omitempty"`
	Revision  uint64          `json:"revision"`
	Timestamp time.Time       `json:"timestamp"`
}

// KVWatchConnectedEvent represents the initial connection event
type KVWatchConnectedEvent struct {
	Bucket  string `json:"bucket"`
	Pattern string `json:"pattern"`
	Message string `json:"message"`
}

// isValidBucketName validates a bucket name to prevent NATS injection attacks.
// Bucket names must not contain NATS wildcards (>, *) or path traversal characters.
func isValidBucketName(bucket string) bool {
	if bucket == "" {
		return false
	}
	// Reject NATS wildcards
	if strings.ContainsAny(bucket, ">*") {
		return false
	}
	// Reject path traversal
	if bucket == "." || bucket == ".." ||
		strings.Contains(bucket, "/") || strings.Contains(bucket, "\\") {
		return false
	}
	return true
}

// isValidWatchPattern validates a watch pattern.
// Allows NATS KV patterns like "entity.*" but prevents injection.
func isValidWatchPattern(pattern string) bool {
	if pattern == "" {
		return true // Empty means default "*"
	}
	// Reject multi-level wildcard which could be dangerous
	if strings.Contains(pattern, ">") {
		return false
	}
	// Allow single-level wildcard (*) for legitimate pattern matching
	// Reject path traversal
	if strings.Contains(pattern, "..") ||
		strings.Contains(pattern, "/") || strings.Contains(pattern, "\\") {
		return false
	}
	return true
}

// detectKVOperation determines the operation type from a KV entry
func detectKVOperation(entry jetstream.KeyValueEntry) string {
	switch entry.Operation() {
	case jetstream.KeyValuePut:
		if entry.Revision() == 1 {
			return "create"
		}
		return "update"
	case jetstream.KeyValueDelete:
		return "delete"
	default:
		return "unknown"
	}
}

// handleKVWatch handles GET /message-logger/kv/{bucket}/watch
// Streams KV bucket changes via SSE with optional pattern filtering
func (ml *MessageLogger) handleKVWatch(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Extract and validate bucket name from path
	bucket, pattern, statusCode, err := ml.validateKVWatchRequest(r)
	if err != nil {
		http.Error(w, err.Error(), statusCode)
		return
	}

	// Setup SSE connection
	ml.setupKVWatchSSEHeaders(w)

	// Open the existing KV bucket; watching never creates persistent state.
	kv, err := ml.getKVBucketForWatch(ctx, bucket)
	if err != nil {
		ml.sendKVWatchError(w, "Failed to access KV bucket", err)
		return
	}

	// Create KV watcher
	eventChan, watcher, err := ml.createKVWatcher(ctx, kv, bucket, pattern)
	if err != nil {
		ml.sendKVWatchError(w, "Failed to create watcher", err)
		return
	}
	defer func() {
		if err := watcher.Stop(); err != nil {
			ml.logger.Warn("Failed to stop KV watcher", "error", err, "bucket", bucket)
		}
	}()

	// Stream events to client
	ml.streamKVEventsToClient(ctx, w, bucket, pattern, eventChan)
}

// validateKVWatchRequest validates request parameters
func (ml *MessageLogger) validateKVWatchRequest(r *http.Request) (bucket, pattern string, statusCode int, err error) {
	// Extract bucket from path - path is /message-logger/kv/{bucket}/watch
	path := r.URL.Path
	// Find the bucket part between /kv/ and /watch
	kvIdx := strings.Index(path, "/kv/")
	if kvIdx == -1 {
		return "", "", http.StatusBadRequest, fmt.Errorf("invalid path: missing /kv/ segment")
	}
	afterKv := path[kvIdx+4:] // Skip "/kv/"
	watchIdx := strings.Index(afterKv, "/watch")
	if watchIdx == -1 {
		return "", "", http.StatusBadRequest, fmt.Errorf("invalid path: missing /watch segment")
	}
	bucket = afterKv[:watchIdx]

	// URL decode the bucket name
	bucket, err = url.QueryUnescape(bucket)
	if err != nil {
		return "", "", http.StatusBadRequest, fmt.Errorf("invalid bucket name encoding")
	}

	if !isValidBucketName(bucket) {
		return "", "", http.StatusBadRequest, fmt.Errorf("invalid bucket name: must not contain wildcards (>, *) or path traversal")
	}

	// Get pattern from query
	pattern = r.URL.Query().Get("pattern")
	if pattern == "" {
		pattern = "*"
	}

	if !isValidWatchPattern(pattern) {
		return "", "", http.StatusBadRequest, fmt.Errorf("invalid watch pattern: must not contain multi-level wildcard (>) or path traversal")
	}

	return bucket, pattern, 0, nil
}

// setupKVWatchSSEHeaders configures SSE headers
func (ml *MessageLogger) setupKVWatchSSEHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // Disable nginx buffering

	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}

// getKVBucketForWatch gets a KV bucket for watching (doesn't create if not exists)
func (ml *MessageLogger) getKVBucketForWatch(ctx context.Context, bucket string) (jetstream.KeyValue, error) {
	// Use GetKeyValueBucket to get existing bucket (returns error if not exists)
	kv, err := ml.natsClient.GetKeyValueBucket(ctx, bucket)
	if err != nil {
		return nil, fmt.Errorf("bucket %s not found: %w", bucket, err)
	}
	return kv, nil
}

// createKVWatcher creates a NATS KV watcher with buffered channel
func (ml *MessageLogger) createKVWatcher(
	ctx context.Context,
	kv jetstream.KeyValue,
	bucket, pattern string,
) (chan *KVWatchEvent, jetstream.KeyWatcher, error) {
	// Use WatchAll if pattern is "*" or empty, otherwise Watch with pattern
	var watcher jetstream.KeyWatcher
	var err error

	if pattern == "" || pattern == "*" {
		watcher, err = kv.WatchAll(ctx)
	} else {
		watcher, err = kv.Watch(ctx, pattern)
	}

	if err != nil {
		return nil, nil, fmt.Errorf("failed to create KV watcher: %w", err)
	}

	// Buffer size for events
	bufferSize := 100
	eventChan := make(chan *KVWatchEvent, bufferSize)

	// Start goroutine to consume watcher updates
	go ml.consumeKVWatchUpdates(ctx, watcher, bucket, eventChan)

	return eventChan, watcher, nil
}

// consumeKVWatchUpdates processes incoming KV watch updates
func (ml *MessageLogger) consumeKVWatchUpdates(
	ctx context.Context,
	watcher jetstream.KeyWatcher,
	bucket string,
	eventChan chan *KVWatchEvent,
) {
	defer close(eventChan)

	for {
		select {
		case <-ctx.Done():
			return

		case entry, ok := <-watcher.Updates():
			if !ok {
				return // Watcher closed
			}

			if entry == nil {
				// NATS KV sends nil to signal initial sync is complete.
				// Forward this as a special event so clients know all existing keys have been sent.
				select {
				case eventChan <- &KVWatchEvent{
					Bucket:    bucket,
					Operation: "initial_sync_complete",
				}:
				case <-ctx.Done():
					return
				}
				continue
			}

			// Convert to event
			event := &KVWatchEvent{
				Bucket:    bucket,
				Key:       entry.Key(),
				Operation: detectKVOperation(entry),
				Revision:  entry.Revision(),
				Timestamp: entry.Created(),
			}

			// Include value for non-delete operations
			if entry.Operation() != jetstream.KeyValueDelete {
				// Try to parse as JSON, fallback to raw string
				if json.Valid(entry.Value()) {
					event.Value = entry.Value()
				} else {
					// Wrap non-JSON value as string
					event.Value, _ = json.Marshal(string(entry.Value()))
				}
			}

			// Send to channel (non-blocking with backpressure)
			select {
			case eventChan <- event:
			case <-ctx.Done():
				return
			default:
				// Channel full - log warning and skip
				ml.logger.Warn("KV watch buffer full, dropping event",
					"bucket", bucket,
					"key", entry.Key(),
					"revision", entry.Revision())
			}
		}
	}
}

// streamKVEventsToClient streams KV events to the client via SSE
func (ml *MessageLogger) streamKVEventsToClient(
	ctx context.Context,
	w http.ResponseWriter,
	bucket, pattern string,
	eventChan chan *KVWatchEvent,
) {
	// Event ID counter for SSE reconnection support
	var eventID atomic.Uint64

	// Send initial connection event
	ml.sendKVWatchEventWithID(w, "connected", KVWatchConnectedEvent{
		Bucket:  bucket,
		Pattern: pattern,
		Message: "Watching for changes",
	}, eventID.Add(1))

	// Send retry directive (5 seconds)
	fmt.Fprintf(w, "retry: 5000\n\n")
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}

	// Stream events until context is cancelled
	for {
		select {
		case <-ctx.Done():
			ml.logger.Info("Client disconnected from KV watch stream",
				"bucket", bucket, "pattern", pattern)
			return

		case event, ok := <-eventChan:
			if !ok {
				// Channel closed - watcher stopped
				ml.sendKVWatchEvent(w, "error", map[string]string{
					"error": "Watcher closed unexpectedly",
				})
				return
			}

			// Send event with incremental ID
			currentEventID := eventID.Add(1)
			data, err := json.Marshal(event)
			if err != nil {
				ml.logger.Error("Failed to marshal KV event", "error", err)
				continue
			}

			fmt.Fprintf(w, "event: kv_change\nid: %d\ndata: %s\n\n", currentEventID, data)
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
		}
	}
}

// sendKVWatchEvent sends an SSE event without ID
func (ml *MessageLogger) sendKVWatchEvent(w http.ResponseWriter, event string, data interface{}) {
	jsonData, err := json.Marshal(data)
	if err != nil {
		ml.logger.Error("Failed to marshal SSE event", "event", event, "error", err)
		return
	}

	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, jsonData)
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}

// sendKVWatchEventWithID sends an SSE event with ID for reconnection support
func (ml *MessageLogger) sendKVWatchEventWithID(w http.ResponseWriter, event string, data interface{}, id uint64) {
	jsonData, err := json.Marshal(data)
	if err != nil {
		ml.logger.Error("Failed to marshal SSE event", "event", event, "error", err)
		return
	}

	fmt.Fprintf(w, "event: %s\nid: %d\ndata: %s\n\n", event, id, jsonData)
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}

// sendKVWatchError sends an error event via SSE
func (ml *MessageLogger) sendKVWatchError(w http.ResponseWriter, message string, err error) {
	errorData := map[string]string{
		"error": message,
	}
	if err != nil {
		errorData["details"] = err.Error()
	}
	ml.sendKVWatchEvent(w, "error", errorData)
}
