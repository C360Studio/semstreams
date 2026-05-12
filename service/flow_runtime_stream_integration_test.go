//go:build integration

package service

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/flowstore"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/logging"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWebSocketStatusStream_ReceivesMetrics verifies metrics flow through WebSocket
func TestWebSocketStatusStream_ReceivesMetrics(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Get shared NATS client
	natsClient := getSharedNATSClient(t)

	// Create flow store and test flow
	flowStore, err := flowstore.NewManager(natsClient)
	require.NoError(t, err)

	flowID := createTestFlowForStream(t, ctx, flowStore)
	defer func() { _ = flowStore.Delete(ctx, flowID) }()

	// Create FlowService
	fs := createTestFlowServiceForStream(t, natsClient, flowStore)

	// Create test server
	mux := http.NewServeMux()
	mux.HandleFunc("/flowbuilder/status/stream", fs.handleStatusWebSocket)
	server := httptest.NewServer(mux)
	defer server.Close()

	// Connect WebSocket
	conn := connectTestWebSocket(t, server, flowID)
	defer conn.Close()

	// Give the server-side goroutines time to set up NATS subscriptions
	time.Sleep(100 * time.Millisecond)

	// Set read deadline
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))

	// Publish a metric to JetStream METRICS stream
	metricData := map[string]interface{}{
		"timestamp": time.Now().UnixMilli(),
		"name":      "test_counter",
		"component": "test-component",
		"type":      "counter",
		"value":     42.0,
		"labels":    map[string]string{"status": "success"},
	}
	metricBytes, err := json.Marshal(metricData)
	require.NoError(t, err)

	err = natsClient.PublishToStream(ctx, "metrics.test-component.test_counter", metricBytes)
	require.NoError(t, err)

	// Wait for metrics envelope
	envelope := waitForEnvelopeType(t, conn, "component_metrics", 5*time.Second)
	require.NotNil(t, envelope, "Should receive component_metrics envelope")

	assert.Equal(t, "component_metrics", envelope.Type)
	assert.Equal(t, flowID, envelope.FlowID)
	assert.NotEmpty(t, envelope.ID)
	assert.Greater(t, envelope.Timestamp, int64(0))
}

// TestWebSocketStatusStream_ReceivesLogs verifies logs flow through WebSocket via NATSLogHandler
func TestWebSocketStatusStream_ReceivesLogs(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Get shared NATS client
	natsClient := getSharedNATSClient(t)

	// Create flow store and test flow
	flowStore, err := flowstore.NewManager(natsClient)
	require.NoError(t, err)

	flowID := createTestFlowForStream(t, ctx, flowStore)
	defer func() { _ = flowStore.Delete(ctx, flowID) }()

	// Create NATSLogHandler for publishing logs to NATS
	natsHandler := logging.NewNATSLogHandler(natsClient, logging.NATSLogHandlerConfig{
		MinLevel:       slog.LevelDebug,
		ExcludeSources: nil,
	})

	// Create FlowService
	fs := createTestFlowServiceForStream(t, natsClient, flowStore)

	// Create test server
	mux := http.NewServeMux()
	mux.HandleFunc("/flowbuilder/status/stream", fs.handleStatusWebSocket)
	server := httptest.NewServer(mux)
	defer server.Close()

	// Connect WebSocket
	conn := connectTestWebSocket(t, server, flowID)
	defer conn.Close()

	// Give the server-side goroutines time to set up JetStream consumers
	time.Sleep(100 * time.Millisecond)

	// Set read deadline
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))

	// Generate a log record and send it directly through NATSLogHandler
	// This simulates what happens when slog is called with NATSLogHandler in the handler chain
	testRecord := slog.NewRecord(time.Now(), slog.LevelInfo, "Integration test log message", 0)
	testRecord.AddAttrs(
		slog.String("component", "integration-test"),
		slog.String("test_id", "log-flow-test"),
	)
	err = natsHandler.Handle(ctx, testRecord)
	require.NoError(t, err)

	// Wait for our specific log message
	envelope := waitForLogMessage(t, conn, "Integration test log message", 5*time.Second)
	require.NotNil(t, envelope, "Should receive log_entry envelope with our message")

	assert.Equal(t, "log_entry", envelope.Type)
	assert.Equal(t, flowID, envelope.FlowID)

	// Verify payload contains our log message
	var payload map[string]interface{}
	err = json.Unmarshal(envelope.Payload, &payload)
	require.NoError(t, err)

	assert.Equal(t, "INFO", payload["level"])
	assert.Contains(t, payload["message"], "Integration test log message")
}

// TestWebSocketStatusStream_ReceivesFlowStatus verifies flow state changes are sent via NATS
func TestWebSocketStatusStream_ReceivesFlowStatus(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Get shared NATS client
	natsClient := getSharedNATSClient(t)

	// Create flow store
	flowStore, err := flowstore.NewManager(natsClient)
	require.NoError(t, err)

	// Create test flow in initial state
	flowID := createTestFlowForStream(t, ctx, flowStore)
	defer func() { _ = flowStore.Delete(ctx, flowID) }()

	// Create FlowService
	fs := createTestFlowServiceForStream(t, natsClient, flowStore)

	// Create test server
	mux := http.NewServeMux()
	mux.HandleFunc("/flowbuilder/status/stream", fs.handleStatusWebSocket)
	server := httptest.NewServer(mux)
	defer server.Close()

	// Connect WebSocket
	conn := connectTestWebSocket(t, server, flowID)
	defer conn.Close()

	// Give WebSocket time to establish subscriptions
	time.Sleep(100 * time.Millisecond)

	// Publish flow status to JetStream FLOWS stream
	statusData := map[string]interface{}{
		"timestamp": time.Now().UnixMilli(),
		"flow_id":   flowID,
		"state":     string(flowstore.StateRunning),
	}
	statusBytes, err := json.Marshal(statusData)
	require.NoError(t, err)
	err = natsClient.PublishToStream(ctx, "flows."+flowID+".status", statusBytes)
	require.NoError(t, err)

	// Wait for flow_status envelope
	envelope := waitForEnvelopeType(t, conn, "flow_status", 5*time.Second)
	require.NotNil(t, envelope, "Should receive flow_status envelope")

	assert.Equal(t, "flow_status", envelope.Type)
	assert.Equal(t, flowID, envelope.FlowID)

	// Verify payload
	var payload map[string]interface{}
	err = json.Unmarshal(envelope.Payload, &payload)
	require.NoError(t, err)

	assert.Equal(t, string(flowstore.StateRunning), payload["state"])
}

// TestWebSocketStatusStream_ReceivesComponentHealth verifies component health flows through WebSocket via NATS
func TestWebSocketStatusStream_ReceivesComponentHealth(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Get shared NATS client
	natsClient := getSharedNATSClient(t)

	// Create flow store and test flow
	flowStore, err := flowstore.NewManager(natsClient)
	require.NoError(t, err)

	flowID := createTestFlowForStream(t, ctx, flowStore)
	defer func() { _ = flowStore.Delete(ctx, flowID) }()

	// Create FlowService
	fs := createTestFlowServiceForStream(t, natsClient, flowStore)

	// Create test server
	mux := http.NewServeMux()
	mux.HandleFunc("/flowbuilder/status/stream", fs.handleStatusWebSocket)
	server := httptest.NewServer(mux)
	defer server.Close()

	// Connect WebSocket
	conn := connectTestWebSocket(t, server, flowID)
	defer conn.Close()

	// Give WebSocket time to establish subscriptions
	time.Sleep(100 * time.Millisecond)

	// Publish health data to JetStream HEALTH stream
	healthData := map[string]interface{}{
		"timestamp": time.Now().UnixMilli(),
		"name":      "test-input",
		"health": map[string]interface{}{
			"healthy":     true,
			"status":      "running",
			"error_count": 0,
		},
	}
	healthBytes, err := json.Marshal(healthData)
	require.NoError(t, err)
	err = natsClient.PublishToStream(ctx, "health.component.test-input", healthBytes)
	require.NoError(t, err)

	// Wait for component_health envelope
	envelope := waitForEnvelopeType(t, conn, "component_health", 5*time.Second)
	require.NotNil(t, envelope, "Should receive component_health envelope")

	assert.Equal(t, "component_health", envelope.Type)
	assert.Equal(t, flowID, envelope.FlowID)
	assert.NotEmpty(t, envelope.ID)
	assert.Greater(t, envelope.Timestamp, int64(0))

	// Verify payload contains the published health data
	var payload map[string]interface{}
	err = json.Unmarshal(envelope.Payload, &payload)
	require.NoError(t, err)

	assert.Equal(t, "test-input", payload["name"])
	health, ok := payload["health"].(map[string]interface{})
	require.True(t, ok, "Should have health object in payload")
	assert.Equal(t, true, health["healthy"])
	assert.Equal(t, "running", health["status"])
}

// TestWebSocketStatusStream_LogsNotReceivedWhenLogForwarderDisabled verifies no logs without LogForwarder
func TestWebSocketStatusStream_LogsNotReceivedWhenLogForwarderDisabled(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Get shared NATS client
	natsClient := getSharedNATSClient(t)

	// Create flow store and test flow
	flowStore, err := flowstore.NewManager(natsClient)
	require.NoError(t, err)

	flowID := createTestFlowForStream(t, ctx, flowStore)
	defer func() { _ = flowStore.Delete(ctx, flowID) }()

	// Create FlowService WITHOUT LogForwarder wired to slog
	fs := createTestFlowServiceForStream(t, natsClient, flowStore)

	// Create test server
	mux := http.NewServeMux()
	mux.HandleFunc("/flowbuilder/status/stream", fs.handleStatusWebSocket)
	server := httptest.NewServer(mux)
	defer server.Close()

	// Connect WebSocket
	conn := connectTestWebSocket(t, server, flowID)
	defer conn.Close()

	// Generate a log message - without LogForwarder, this won't reach NATS
	slog.Info("This log should NOT reach WebSocket", "component", "test")

	// Try to receive - should timeout because no LogForwarder is publishing to NATS
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	envelope := waitForEnvelopeType(t, conn, "log_entry", 2*time.Second)

	// We should NOT receive a log_entry (might receive metrics though)
	if envelope != nil && envelope.Type == "log_entry" {
		// Check if it's our message
		var payload map[string]interface{}
		_ = json.Unmarshal(envelope.Payload, &payload)
		if msg, ok := payload["message"].(string); ok {
			assert.NotContains(t, msg, "This log should NOT reach WebSocket",
				"Log should not reach WebSocket without LogForwarder")
		}
	}
}

// TestWebSocketStatusStream_ExcludeSourcesFiltering verifies excluded sources don't publish
func TestWebSocketStatusStream_ExcludeSourcesFiltering(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Get shared NATS client
	natsClient := getSharedNATSClient(t)

	// Create flow store and test flow
	flowStore, err := flowstore.NewManager(natsClient)
	require.NoError(t, err)

	flowID := createTestFlowForStream(t, ctx, flowStore)
	defer func() { _ = flowStore.Delete(ctx, flowID) }()

	// Create NATSLogHandler with exclude_sources
	natsHandler := logging.NewNATSLogHandler(natsClient, logging.NATSLogHandlerConfig{
		MinLevel:       slog.LevelDebug,
		ExcludeSources: []string{"excluded-source"},
	})

	// Create FlowService
	fs := createTestFlowServiceForStream(t, natsClient, flowStore)

	// Create test server
	mux := http.NewServeMux()
	mux.HandleFunc("/flowbuilder/status/stream", fs.handleStatusWebSocket)
	server := httptest.NewServer(mux)
	defer server.Close()

	// Connect WebSocket
	conn := connectTestWebSocket(t, server, flowID)
	defer conn.Close()

	// Give the server-side goroutines time to set up JetStream consumers
	time.Sleep(100 * time.Millisecond)

	conn.SetReadDeadline(time.Now().Add(5 * time.Second))

	// Send log records directly through NATSLogHandler
	// Log from excluded source - should NOT be forwarded to NATS
	excludedRecord := slog.NewRecord(time.Now(), slog.LevelInfo, "Excluded log message", 0)
	excludedRecord.AddAttrs(slog.String("source", "excluded-source"))
	_ = natsHandler.Handle(ctx, excludedRecord)

	// Log from allowed source - should be forwarded
	allowedRecord := slog.NewRecord(time.Now(), slog.LevelInfo, "Allowed log message", 0)
	allowedRecord.AddAttrs(slog.String("source", "allowed-source"))
	_ = natsHandler.Handle(ctx, allowedRecord)

	// Collect envelopes for a short time
	receivedLogs := collectEnvelopesOfType(t, conn, "log_entry", 3*time.Second)

	// Should receive the allowed log but NOT the excluded one
	var foundAllowed, foundExcluded bool
	for _, env := range receivedLogs {
		var payload map[string]interface{}
		if err := json.Unmarshal(env.Payload, &payload); err != nil {
			continue
		}
		if msg, ok := payload["message"].(string); ok {
			if strings.Contains(msg, "Allowed log message") {
				foundAllowed = true
			}
			if strings.Contains(msg, "Excluded log message") {
				foundExcluded = true
			}
		}
	}

	assert.True(t, foundAllowed, "Should receive log from allowed source")
	assert.False(t, foundExcluded, "Should NOT receive log from excluded source")
}

// --- Test Helpers ---

// createTestFlowForStream creates a test flow in the flow store
func createTestFlowForStream(t *testing.T, ctx context.Context, store *flowstore.Manager) string {
	t.Helper()

	flowID := "test-flow-" + time.Now().Format("20060102150405")
	flow := &flowstore.Flow{
		ID:           flowID,
		Name:         "Test Flow",
		Version:      1,
		RuntimeState: flowstore.StateDeployedStopped,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	err := store.Create(ctx, flow)
	require.NoError(t, err)

	return flowID
}

// createTestFlowServiceForStream creates a FlowService for integration tests
func createTestFlowServiceForStream(t *testing.T, natsClient *natsclient.Client, flowStore *flowstore.Manager) *FlowService {
	t.Helper()

	baseService := NewBaseServiceWithOptions(
		"test-flow-service",
		nil,
		WithNATS(natsClient),
		WithLogger(slog.Default()),
	)

	return &FlowService{
		BaseService: baseService,
		flowStore:   flowStore,
		natsClient:  natsClient,
		config: FlowServiceConfig{
			LogStreamBufferSize: 100,
		},
	}
}

// connectTestWebSocket connects to the WebSocket status stream endpoint
func connectTestWebSocket(t *testing.T, server *httptest.Server, flowID string) *websocket.Conn {
	t.Helper()

	// Convert http:// to ws://
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/flowbuilder/status/stream?flowId=" + flowID

	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err, "WebSocket connection should succeed")
	require.Equal(t, http.StatusSwitchingProtocols, resp.StatusCode)

	return conn
}

// waitForEnvelopeType waits for an envelope of a specific type
func waitForEnvelopeType(t *testing.T, conn *websocket.Conn, msgType string, timeout time.Duration) *StatusStreamEnvelope {
	t.Helper()

	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		conn.SetReadDeadline(deadline)

		_, data, err := conn.ReadMessage()
		if err != nil {
			// Timeout or connection closed
			return nil
		}

		var envelope StatusStreamEnvelope
		if err := json.Unmarshal(data, &envelope); err != nil {
			continue
		}

		if envelope.Type == msgType {
			return &envelope
		}
	}

	return nil
}

// collectEnvelopesOfType collects all envelopes of a specific type within timeout
func collectEnvelopesOfType(t *testing.T, conn *websocket.Conn, msgType string, timeout time.Duration) (envelopes []StatusStreamEnvelope) {
	t.Helper()

	// Recover from panics caused by reading from failed websocket connections
	defer func() {
		if r := recover(); r != nil {
			// Connection was in a failed state, return what we have
			t.Logf("WebSocket read recovered from panic: %v", r)
		}
	}()

	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		if err := conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond)); err != nil {
			// Connection already closed
			break
		}

		_, data, err := conn.ReadMessage()
		if err != nil {
			// Timeout = no data yet, retry until deadline. This is
			// the only safe continue path. Every other error class
			// puts the gorilla/websocket connection into a state
			// where a subsequent ReadMessage call panics with
			// "repeated read on failed websocket connection" — the
			// library considers retry after error a programmer bug.
			// Pre-2026-05-13 the loop had a default "continue on
			// transient error" path that hit exactly that panic on
			// CI runners (network-shape sensitive); the defer
			// recover() below caught it but left envelopes empty,
			// flaking the downstream assertion. Audit-cleanup
			// 2026-05-13.
			if netErr, ok := err.(interface{ Timeout() bool }); ok && netErr.Timeout() {
				continue
			}
			// Any non-timeout error (close, EOF, "use of closed
			// network connection", abnormal close, etc.) is
			// terminal for this connection. Stop reading.
			break
		}

		var envelope StatusStreamEnvelope
		if err := json.Unmarshal(data, &envelope); err != nil {
			continue
		}

		if envelope.Type == msgType {
			envelopes = append(envelopes, envelope)
		}
	}

	return envelopes
}

// waitForLogMessage waits for a log_entry envelope containing a specific message
func waitForLogMessage(t *testing.T, conn *websocket.Conn, messageSubstr string, timeout time.Duration) *StatusStreamEnvelope {
	t.Helper()

	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		conn.SetReadDeadline(deadline)

		_, data, err := conn.ReadMessage()
		if err != nil {
			// Timeout or connection closed
			return nil
		}

		var envelope StatusStreamEnvelope
		if err := json.Unmarshal(data, &envelope); err != nil {
			continue
		}

		if envelope.Type == "log_entry" {
			var payload map[string]interface{}
			if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
				continue
			}
			if msg, ok := payload["message"].(string); ok {
				if strings.Contains(msg, messageSubstr) {
					return &envelope
				}
			}
		}
	}

	return nil
}
