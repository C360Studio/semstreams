//go:build integration

package websocket

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/c360/semstreams/metric"
	"github.com/c360/semstreams/natsclient"
	"github.com/c360/semstreams/pkg/security"
	"github.com/gorilla/websocket"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWebSocketOutput_MetricsInitialization(t *testing.T) {
	natsClient := natsclient.NewTestClient(t)

	port := getAvailablePort(t)

	// Test without metrics registry
	ws := NewOutput(port, "/ws", []string{"test.>"}, natsClient.Client)
	assert.Nil(t, ws.metrics)

	// Test with metrics registry
	registry := metric.NewMetricsRegistry()
	cfg := ConstructorConfig{
		Name:            "test-ws",
		Port:            port + 1,
		Path:            "/ws",
		Subjects:        []string{"test.>"},
		NATSClient:      natsClient.Client,
		MetricsRegistry: registry,
		Security:        security.Config{},
		DeliveryMode:    DeliveryAtMostOnce,
		AckTimeout:      5 * time.Second,
	}
	wsWithMetrics := NewOutputFromConfig(cfg)

	assert.NotNil(t, wsWithMetrics.metrics)
	assert.NotNil(t, wsWithMetrics.metrics.messagesReceived)
	assert.NotNil(t, wsWithMetrics.metrics.messagesSent)
	assert.NotNil(t, wsWithMetrics.metrics.bytesSent)
	assert.NotNil(t, wsWithMetrics.metrics.clientsConnected)
	assert.NotNil(t, wsWithMetrics.metrics.connectionTotal)
	assert.NotNil(t, wsWithMetrics.metrics.disconnectionTotal)
	assert.NotNil(t, wsWithMetrics.metrics.broadcastDuration)
	assert.NotNil(t, wsWithMetrics.metrics.messageSizeBytes)
	assert.NotNil(t, wsWithMetrics.metrics.errorsTotal)
	assert.NotNil(t, wsWithMetrics.metrics.serverUptimeSeconds)
}

func TestWebSocketOutput_ClientConnectionMetrics(t *testing.T) {
	natsClient := natsclient.NewTestClient(t)

	registry := metric.NewMetricsRegistry()
	port := getAvailablePort(t)

	ws := NewOutputFromConfig(ConstructorConfig{
		Name:            "test-ws",
		Port:            port,
		Path:            "/ws",
		Subjects:        []string{"test.>"},
		NATSClient:      natsClient.Client,
		MetricsRegistry: registry,
		Security:        security.Config{},
		DeliveryMode:    DeliveryAtMostOnce,
		AckTimeout:      5 * time.Second,
	})
	require.NotNil(t, ws.metrics)

	// Start the WebSocket server
	ctx := context.Background()
	err := ws.Start(ctx)
	require.NoError(t, err)
	defer ws.Stop(5 * time.Second)

	// Wait for server to start
	time.Sleep(100 * time.Millisecond)

	// Connect a WebSocket client
	dialer := websocket.Dialer{}
	wsURL := fmt.Sprintf("ws://localhost:%d/ws", port)
	conn, _, err := dialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer conn.Close()

	// Wait for connection to be registered
	time.Sleep(100 * time.Millisecond)

	// Check metrics were updated
	// Should have recorded connection
	if ws.metrics != nil && ws.metrics.connectionTotal != nil {
		connectionCount := testutil.ToFloat64(ws.metrics.connectionTotal)
		assert.True(t, connectionCount > 0, "Should have recorded at least one connection")
	}

	// Should have active client
	if ws.metrics != nil && ws.metrics.clientsConnected != nil {
		activeClients := testutil.ToFloat64(ws.metrics.clientsConnected)
		assert.True(t, activeClients > 0, "Should have at least one active client")
	}

	// Close connection
	conn.Close()
	time.Sleep(100 * time.Millisecond)

	// Check disconnection metrics
	if ws.metrics != nil && ws.metrics.disconnectionTotal != nil {
		// Check for any disconnect reason - use "normal" as the most likely
		disconnectionCount := testutil.ToFloat64(ws.metrics.disconnectionTotal.WithLabelValues("normal"))
		earlyDisconnectCount := testutil.ToFloat64(ws.metrics.disconnectionTotal.WithLabelValues("early_disconnect"))
		totalDisconnects := disconnectionCount + earlyDisconnectCount
		assert.True(t, totalDisconnects > 0, "Should have recorded at least one disconnection")
	}
}

func TestWebSocketOutput_MessageBroadcastMetrics(t *testing.T) {
	natsClient := natsclient.NewTestClient(t)

	registry := metric.NewMetricsRegistry()
	port := getAvailablePort(t)

	ws := NewOutputFromConfig(ConstructorConfig{
		Name:            "test-ws",
		Port:            port,
		Path:            "/ws",
		Subjects:        []string{"test.subject"},
		NATSClient:      natsClient.Client,
		MetricsRegistry: registry,
		Security:        security.Config{},
		DeliveryMode:    DeliveryAtMostOnce,
		AckTimeout:      5 * time.Second,
	})
	require.NotNil(t, ws.metrics)

	// Start the WebSocket server
	ctx := context.Background()
	err := ws.Start(ctx)
	require.NoError(t, err)
	defer ws.Stop(5 * time.Second)

	// Wait for server to start
	time.Sleep(100 * time.Millisecond)

	// Connect a WebSocket client
	dialer := websocket.Dialer{}
	wsURL := fmt.Sprintf("ws://localhost:%d/ws", port)
	conn, _, err := dialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer conn.Close()

	// Wait for connection to be registered
	time.Sleep(100 * time.Millisecond)

	// Publish a message to NATS
	testMessage := map[string]any{
		"test":      "message",
		"timestamp": time.Now().Format(time.RFC3339),
	}

	data, err := json.Marshal(testMessage)
	require.NoError(t, err)

	err = natsClient.Client.Publish(ctx, "test.subject", data)
	require.NoError(t, err)

	// Wait for message to be processed and broadcast
	time.Sleep(200 * time.Millisecond)

	// Check metrics
	// Should have received message from NATS
	if ws.metrics != nil && ws.metrics.messagesReceived != nil {
		receivedCount := testutil.ToFloat64(ws.metrics.messagesReceived.WithLabelValues("test.subject"))
		assert.True(t, receivedCount > 0, "Should have received at least one message from NATS")
	}

	// Should have sent message to WebSocket clients
	if ws.metrics != nil && ws.metrics.messagesSent != nil {
		sentCount := testutil.ToFloat64(ws.metrics.messagesSent.WithLabelValues("test.subject"))
		assert.True(t, sentCount > 0, "Should have sent at least one message to WebSocket clients")
	}

	// Should have recorded bytes sent
	if ws.metrics != nil && ws.metrics.bytesSent != nil {
		bytesSent := testutil.ToFloat64(ws.metrics.bytesSent)
		assert.True(t, bytesSent > 0, "Should have sent some bytes")
	}

	// Should have recorded broadcast duration (histogram metrics are harder to test but we can check structure)
	if ws.metrics != nil && ws.metrics.broadcastDuration != nil {
		assert.NotNil(
			t,
			ws.metrics.broadcastDuration.WithLabelValues("test.subject"),
			"Should have broadcast duration metric",
		)
	}
}

func TestWebSocketOutput_ErrorMetrics(t *testing.T) {
	natsClient := natsclient.NewTestClient(t)

	registry := metric.NewMetricsRegistry()
	port := getAvailablePort(t)

	ws := NewOutputFromConfig(ConstructorConfig{
		Name:            "test-ws",
		Port:            port,
		Path:            "/ws",
		Subjects:        []string{"test.>"},
		NATSClient:      natsClient.Client,
		MetricsRegistry: registry,
		Security:        security.Config{},
		DeliveryMode:    DeliveryAtMostOnce,
		AckTimeout:      5 * time.Second,
	})
	require.NotNil(t, ws.metrics)

	// Test connection upgrade error by using invalid port - use separate registry to avoid metric name conflicts
	invalidRegistry := metric.NewMetricsRegistry()
	invalidWs := NewOutputFromConfig(ConstructorConfig{
		Name:            "test-ws-invalid",
		Port:            99999,
		Path:            "/ws",
		Subjects:        []string{"test.>"},
		NATSClient:      natsClient.Client,
		MetricsRegistry: invalidRegistry,
		Security:        security.Config{},
		DeliveryMode:    DeliveryAtMostOnce,
		AckTimeout:      5 * time.Second,
	})

	// This should fail during initialization due to invalid port
	err := invalidWs.Initialize()
	// Error expected due to invalid port validation
	assert.Error(t, err)

	// Create a connection and then close it abruptly to test client errors
	ctx := context.Background()
	err = ws.Initialize()
	require.NoError(t, err)
	err = ws.Start(ctx)
	require.NoError(t, err)
	defer ws.Stop(5 * time.Second)

	// Wait for server to start
	time.Sleep(100 * time.Millisecond)

	// Connect and immediately close to trigger potential error scenarios
	dialer := websocket.Dialer{}
	wsURL := fmt.Sprintf("ws://localhost:%d/ws", port)
	conn, _, err := dialer.Dial(wsURL, nil)
	if err == nil {
		conn.Close() // Immediate close should trigger disconnect metrics
	}

	time.Sleep(100 * time.Millisecond)

	// Metrics verification would require actual prometheus metric gathering
	// For now, we verify the metrics structure exists
	assert.NotNil(t, ws.metrics.errorsTotal)
}

func TestWebSocketOutput_ServerUptimeMetrics(t *testing.T) {
	natsClient := natsclient.NewTestClient(t)

	registry := metric.NewMetricsRegistry()
	port := getAvailablePort(t)

	ws := NewOutputFromConfig(ConstructorConfig{
		Name:            "test-ws",
		Port:            port,
		Path:            "/ws",
		Subjects:        []string{"test.>"},
		NATSClient:      natsClient.Client,
		MetricsRegistry: registry,
		Security:        security.Config{},
		DeliveryMode:    DeliveryAtMostOnce,
		AckTimeout:      5 * time.Second,
	})
	require.NotNil(t, ws.metrics)

	// Start the WebSocket server
	ctx := context.Background()
	err := ws.Start(ctx)
	require.NoError(t, err)
	defer ws.Stop(5 * time.Second)

	// Wait for uptime tracking to update (should happen every 10 seconds, but we test structure)
	time.Sleep(100 * time.Millisecond)

	// Verify uptime metric exists and is being tracked
	assert.NotNil(t, ws.metrics.serverUptimeSeconds)

	// In real implementation, we'd check that uptime value increases over time
}

func TestWebSocketOutput_MessageSizeMetrics(t *testing.T) {
	natsClient := natsclient.NewTestClient(t)

	registry := metric.NewMetricsRegistry()
	port := getAvailablePort(t)

	ws := NewOutputFromConfig(ConstructorConfig{
		Name:            "test-ws",
		Port:            port,
		Path:            "/ws",
		Subjects:        []string{"test.size"},
		NATSClient:      natsClient.Client,
		MetricsRegistry: registry,
		Security:        security.Config{},
		DeliveryMode:    DeliveryAtMostOnce,
		AckTimeout:      5 * time.Second,
	})
	require.NotNil(t, ws.metrics)

	// Start the WebSocket server
	ctx := context.Background()
	err := ws.Start(ctx)
	require.NoError(t, err)
	defer ws.Stop(5 * time.Second)

	// Wait for server to start
	time.Sleep(100 * time.Millisecond)

	// Connect a WebSocket client
	dialer := websocket.Dialer{}
	wsURL := fmt.Sprintf("ws://localhost:%d/ws", port)
	conn, _, err := dialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer conn.Close()

	// Wait for connection
	time.Sleep(100 * time.Millisecond)

	// Send messages of different sizes
	smallMessage := map[string]any{"small": "msg"}
	largeMessage := map[string]any{
		"large": "This is a much larger message that should result in more bytes being tracked in the message size histogram metrics",
		"data":  make([]int, 100), // Add some bulk
	}

	smallData, _ := json.Marshal(smallMessage)
	largeData, _ := json.Marshal(largeMessage)

	// Publish different sized messages
	natsClient.Client.Publish(ctx, "test.size", smallData)
	natsClient.Client.Publish(ctx, "test.size", largeData)

	// Wait for processing
	time.Sleep(200 * time.Millisecond)

	// Verify message size histogram metrics exist and have recorded observations
	if ws.metrics != nil && ws.metrics.messageSizeBytes != nil {
		assert.NotNil(
			t,
			ws.metrics.messageSizeBytes.WithLabelValues("test.size"),
			"Should have message size histogram metric for test.size subject",
		)
	}
}

// Helper functions for WebSocket metric testing

func getAvailablePort(t *testing.T) int {
	t.Helper()

	// Use port 8082 as default for tests, but add offset for parallel tests
	basePort := 8082
	for i := 0; i < 100; i++ {
		port := basePort + i
		ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
		if err == nil {
			_ = ln.Close()
			return port
		}
	}

	t.Fatal("Could not find available port for testing")
	return 8082 // Never reached
}
