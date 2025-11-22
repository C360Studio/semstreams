//go:build integration

package service

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/c360/semstreams/component"
	"github.com/c360/semstreams/flowstore"
	"github.com/c360/semstreams/natsclient"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// createTestFlowService creates a test flow service with NATS client
func createTestFlowService(t *testing.T) (*http.ServeMux, *flowstore.Store, *natsclient.Client) {
	t.Helper()

	// Create NATS client using shared test helper
	testClient := natsclient.NewTestClient(t,
		natsclient.WithJetStream(),
		natsclient.WithKV())
	natsClient := testClient.Client

	// Create flow store
	flowStore, err := flowstore.NewStore(natsClient)
	require.NoError(t, err)

	return http.NewServeMux(), flowStore, natsClient
}

func TestHandleRuntimeLogs_BasicStreaming(t *testing.T) {
	// Setup test environment
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, flowStore, nc := createTestFlowService(t)
	baseService := NewBaseServiceWithOptions("test-flow-service", nil, WithNATS(nc), WithLogger(slog.Default()))
	fs := &FlowService{
		BaseService: baseService,
		flowStore:   flowStore,
		config: FlowServiceConfig{
			LogStreamBufferSize: 100,
		},
	}

	// Create a test flow
	flowID := createTestFlowInStore(t, ctx, flowStore)

	// Create a test request with context
	reqCtx, reqCancel := context.WithCancel(ctx)
	defer reqCancel()

	req := httptest.NewRequest("GET", fmt.Sprintf("/flows/%s/runtime/logs", flowID), nil)
	req = req.WithContext(reqCtx)
	req.SetPathValue("id", flowID)

	// Create thread-safe response recorder for concurrent access
	rec := newSafeResponseRecorder()

	// Start SSE handler in background
	done := make(chan struct{})

	go func() {
		defer close(done)
		fs.handleRuntimeLogs(rec, req)
	}()

	// Wait for handler to be ready by checking for the "connected" event
	require.Eventually(t, func() bool {
		body := rec.BodyString()
		return strings.Contains(body, `"connected"`) || strings.Contains(body, "event: connected")
	}, 2*time.Second, 10*time.Millisecond, "Handler should send connected event")

	// Publish some test logs
	testLogs := []component.LogEntry{
		{
			Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
			Level:     component.LogLevelInfo,
			Component: "test-component",
			FlowID:    flowID,
			Message:   "Test log message 1",
		},
		{
			Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
			Level:     component.LogLevelError,
			Component: "test-component",
			FlowID:    flowID,
			Message:   "Test error message",
			Stack:     "error stack trace",
		},
	}

	natsConn := nc.GetConnection()
	for _, logEntry := range testLogs {
		data, err := json.Marshal(logEntry)
		require.NoError(t, err)

		subject := fmt.Sprintf("logs.%s.%s", flowID, logEntry.Component)
		err = natsConn.Publish(subject, data)
		require.NoError(t, err)
	}

	// Flush NATS to ensure messages are sent
	err := natsConn.Flush()
	require.NoError(t, err)

	// Wait for logs to appear in the response body
	require.Eventually(t, func() bool {
		body := rec.BodyString()
		return strings.Contains(body, "Test log message 1") || strings.Contains(body, "Test error message")
	}, 2*time.Second, 50*time.Millisecond, "Should receive log events")

	// Cancel request to stop streaming
	reqCancel()

	// Wait for handler to finish
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Handler did not finish in time")
	}

	// Verify SSE response headers
	assert.Equal(t, "text/event-stream", rec.Header().Get("Content-Type"))
	assert.Equal(t, "no-cache", rec.Header().Get("Cache-Control"))
	assert.Equal(t, "keep-alive", rec.Header().Get("Connection"))

	// Parse SSE events
	body := rec.BodyString()
	events := parseSSEEvents(t, body)

	// Should have at least connected event + log events
	assert.GreaterOrEqual(t, len(events), 2)

	// Verify we received log events
	logEventCount := 0
	for _, event := range events {
		if event.Event == "log" {
			var logEntry component.LogEntry
			err := json.Unmarshal([]byte(event.Data), &logEntry)
			require.NoError(t, err)
			assert.Equal(t, flowID, logEntry.FlowID)
			assert.Equal(t, "test-component", logEntry.Component)
			logEventCount++
		}
	}

	assert.GreaterOrEqual(t, logEventCount, 1, "Should receive at least one log event")
}

func TestHandleRuntimeLogs_LevelFiltering(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, flowStore, nc := createTestFlowService(t)
	baseService := NewBaseServiceWithOptions("test-flow-service", nil, WithNATS(nc), WithLogger(slog.Default()))
	fs := &FlowService{
		BaseService: baseService,
		flowStore:   flowStore,
		config: FlowServiceConfig{
			LogStreamBufferSize: 100,
		},
	}

	flowID := createTestFlowInStore(t, ctx, flowStore)

	// Request with ERROR level filter
	reqCtx, reqCancel := context.WithCancel(ctx)
	defer reqCancel()

	req := httptest.NewRequest("GET", fmt.Sprintf("/flows/%s/runtime/logs?level=ERROR", flowID), nil)
	req = req.WithContext(reqCtx)
	req.SetPathValue("id", flowID)

	rec := newSafeResponseRecorder()

	done := make(chan struct{})
	go func() {
		defer close(done)
		fs.handleRuntimeLogs(rec, req)
	}()

	// Wait for handler to be ready by checking for the "connected" event
	require.Eventually(t, func() bool {
		body := rec.BodyString()
		return strings.Contains(body, `"connected"`) || strings.Contains(body, "event: connected")
	}, 2*time.Second, 10*time.Millisecond, "Handler should send connected event")

	// Publish logs with different levels
	testLogs := []component.LogEntry{
		{
			Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
			Level:     component.LogLevelInfo,
			Component: "test-component",
			FlowID:    flowID,
			Message:   "Info message - should be filtered",
		},
		{
			Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
			Level:     component.LogLevelError,
			Component: "test-component",
			FlowID:    flowID,
			Message:   "Error message - should be received",
		},
		{
			Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
			Level:     component.LogLevelDebug,
			Component: "test-component",
			FlowID:    flowID,
			Message:   "Debug message - should be filtered",
		},
	}

	natsConn := nc.GetConnection()
	for _, logEntry := range testLogs {
		data, err := json.Marshal(logEntry)
		require.NoError(t, err)

		subject := fmt.Sprintf("logs.%s.%s", flowID, logEntry.Component)
		err = natsConn.Publish(subject, data)
		require.NoError(t, err)
	}

	err := natsConn.Flush()
	require.NoError(t, err)

	// Wait for error log to appear
	require.Eventually(t, func() bool {
		body := rec.BodyString()
		return strings.Contains(body, "Error message - should be received")
	}, 2*time.Second, 50*time.Millisecond, "Should receive ERROR log")

	reqCancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Handler did not finish in time")
	}

	// Parse events and verify only ERROR level logs received
	body := rec.BodyString()
	events := parseSSEEvents(t, body)

	logEventCount := 0
	for _, event := range events {
		if event.Event == "log" {
			var logEntry component.LogEntry
			err := json.Unmarshal([]byte(event.Data), &logEntry)
			require.NoError(t, err)
			assert.Equal(t, component.LogLevelError, logEntry.Level, "Should only receive ERROR level logs")
			logEventCount++
		}
	}

	assert.Equal(t, 1, logEventCount, "Should receive exactly one ERROR log")
}

func TestHandleRuntimeLogs_ComponentFiltering(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, flowStore, nc := createTestFlowService(t)
	baseService := NewBaseServiceWithOptions("test-flow-service", nil, WithNATS(nc), WithLogger(slog.Default()))
	fs := &FlowService{
		BaseService: baseService,
		flowStore:   flowStore,
		config: FlowServiceConfig{
			LogStreamBufferSize: 100,
		},
	}

	flowID := createTestFlowInStore(t, ctx, flowStore)

	// Request with component filter
	reqCtx, reqCancel := context.WithCancel(ctx)
	defer reqCancel()

	req := httptest.NewRequest("GET", fmt.Sprintf("/flows/%s/runtime/logs?component=component-a", flowID), nil)
	req = req.WithContext(reqCtx)
	req.SetPathValue("id", flowID)

	rec := newSafeResponseRecorder()

	done := make(chan struct{})
	go func() {
		defer close(done)
		fs.handleRuntimeLogs(rec, req)
	}()

	// Wait for handler to be ready by checking for the "connected" event
	require.Eventually(t, func() bool {
		body := rec.BodyString()
		return strings.Contains(body, `"connected"`) || strings.Contains(body, "event: connected")
	}, 2*time.Second, 10*time.Millisecond, "Handler should send connected event")

	// Publish logs from different components
	testLogs := []component.LogEntry{
		{
			Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
			Level:     component.LogLevelInfo,
			Component: "component-a",
			FlowID:    flowID,
			Message:   "Message from component-a",
		},
		{
			Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
			Level:     component.LogLevelInfo,
			Component: "component-b",
			FlowID:    flowID,
			Message:   "Message from component-b - should be filtered",
		},
	}

	natsConn := nc.GetConnection()
	for _, logEntry := range testLogs {
		data, err := json.Marshal(logEntry)
		require.NoError(t, err)

		subject := fmt.Sprintf("logs.%s.%s", flowID, logEntry.Component)
		err = natsConn.Publish(subject, data)
		require.NoError(t, err)
	}

	err := natsConn.Flush()
	require.NoError(t, err)

	// Wait for component-a log to appear
	require.Eventually(t, func() bool {
		body := rec.BodyString()
		return strings.Contains(body, "Message from component-a")
	}, 2*time.Second, 50*time.Millisecond, "Should receive component-a log")

	reqCancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Handler did not finish in time")
	}

	// Parse events and verify only component-a logs received
	body := rec.BodyString()
	events := parseSSEEvents(t, body)

	logEventCount := 0
	for _, event := range events {
		if event.Event == "log" {
			var logEntry component.LogEntry
			err := json.Unmarshal([]byte(event.Data), &logEntry)
			require.NoError(t, err)
			assert.Equal(t, "component-a", logEntry.Component, "Should only receive logs from component-a")
			logEventCount++
		}
	}

	assert.Equal(t, 1, logEventCount, "Should receive exactly one log from component-a")
}

func TestHandleRuntimeLogs_InvalidLevelFilter(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, flowStore, nc := createTestFlowService(t)
	baseService := NewBaseServiceWithOptions("test-flow-service", nil, WithNATS(nc), WithLogger(slog.Default()))
	fs := &FlowService{
		BaseService: baseService,
		flowStore:   flowStore,
		config: FlowServiceConfig{
			LogStreamBufferSize: 100,
		},
	}

	flowID := createTestFlowInStore(t, ctx, flowStore)

	// Request with invalid level filter
	req := httptest.NewRequest("GET", fmt.Sprintf("/flows/%s/runtime/logs?level=INVALID", flowID), nil)
	req = req.WithContext(ctx)
	req.SetPathValue("id", flowID)

	rec := httptest.NewRecorder()

	fs.handleRuntimeLogs(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "invalid level filter")
}

func TestHandleRuntimeLogs_FlowNotFound(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, flowStore, nc := createTestFlowService(t)
	baseService := NewBaseServiceWithOptions("test-flow-service", nil, WithNATS(nc), WithLogger(slog.Default()))
	fs := &FlowService{
		BaseService: baseService,
		flowStore:   flowStore,
		config: FlowServiceConfig{
			LogStreamBufferSize: 100,
		},
	}

	// Request logs for non-existent flow
	req := httptest.NewRequest("GET", "/flows/non-existent-flow/runtime/logs", nil)
	req = req.WithContext(ctx)
	req.SetPathValue("id", "non-existent-flow")

	rec := httptest.NewRecorder()

	fs.handleRuntimeLogs(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// Helper types and functions

type sseEvent struct {
	Event string
	Data  string
}

// parseSSEEvents parses SSE event stream into structured events
func parseSSEEvents(t *testing.T, body string) []sseEvent {
	t.Helper()

	var events []sseEvent
	scanner := bufio.NewScanner(strings.NewReader(body))

	var currentEvent sseEvent
	for scanner.Scan() {
		line := scanner.Text()

		if line == "" {
			// Empty line marks end of event
			if currentEvent.Event != "" || currentEvent.Data != "" {
				events = append(events, currentEvent)
				currentEvent = sseEvent{}
			}
			continue
		}

		if strings.HasPrefix(line, "event: ") {
			currentEvent.Event = strings.TrimPrefix(line, "event: ")
		} else if strings.HasPrefix(line, "data: ") {
			currentEvent.Data = strings.TrimPrefix(line, "data: ")
		}
	}

	require.NoError(t, scanner.Err())
	return events
}

// createTestFlowInStore creates a test flow in the flow store and returns its ID
func createTestFlowInStore(t *testing.T, ctx context.Context, store *flowstore.Store) string {
	t.Helper()

	flowID := uuid.New().String()
	flow := &flowstore.Flow{
		ID:      flowID,
		Name:    "test-flow",
		Version: 1,
		Nodes: []flowstore.FlowNode{
			{
				ID:   "node-1",
				Name: "test-component",
				Type: "input",
			},
		},
	}

	err := store.Create(ctx, flow)
	require.NoError(t, err)

	return flowID
}
