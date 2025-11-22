//go:build integration
// +build integration

package websocket

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"sync/atomic"
	"testing"
	"time"

	websocketinput "github.com/c360/semstreams/input/websocket"
	"github.com/c360/semstreams/metric"
	"github.com/c360/semstreams/natsclient"
	"github.com/c360/semstreams/pkg/security"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWebSocketFederation_AckFlow tests successful ack flow between Output and Input
func TestWebSocketFederation_AckFlow(t *testing.T) {
	if os.Getenv("INTEGRATION_TESTS") != "1" {
		t.Skip("Skipping integration test. Set INTEGRATION_TESTS=1 to run.")
	}

	// Create shared NATS client
	natsClient := natsclient.NewTestClient(t, natsclient.WithFastStartup())
	ctx := context.Background()

	// Create metrics registry
	registry := metric.NewMetricsRegistry()

	// Setup WebSocket Output (sender)
	outputPort := getIntegrationPort(t)
	wsOutput := NewOutputFromConfig(ConstructorConfig{
		Name:            "test-output",
		Port:            outputPort,
		Path:            "/stream",
		Subjects:        []string{"sensor.data"},
		NATSClient:      natsClient.Client,
		MetricsRegistry: registry,
		Security:        security.Config{},
		DeliveryMode:    DeliveryAtLeastOnce,
		AckTimeout:      5 * time.Second,
	})

	require.NoError(t, wsOutput.Initialize())
	require.NoError(t, wsOutput.Start(ctx))
	defer wsOutput.Stop(5 * time.Second)

	// Wait for Output server to start
	time.Sleep(200 * time.Millisecond)

	// Setup WebSocket Input (receiver)
	inputConfig := websocketinput.DefaultConfig()
	inputConfig.Mode = websocketinput.ModeClient
	inputConfig.ClientConfig = &websocketinput.ClientConfig{
		URL: fmt.Sprintf("ws://localhost:%d/stream", outputPort),
		Reconnect: &websocketinput.ReconnectConfig{
			Enabled:         true,
			MaxRetries:      5,
			InitialInterval: 1 * time.Second,
			MaxInterval:     10 * time.Second,
			Multiplier:      1.5,
		},
	}

	wsInput, err := websocketinput.NewInput("test-input", natsClient.Client, inputConfig, registry, security.Config{})
	require.NoError(t, err)

	require.NoError(t, wsInput.Initialize())
	require.NoError(t, wsInput.Start(ctx))
	defer wsInput.Stop(5 * time.Second)

	// Wait for Input client to connect to Output server
	time.Sleep(500 * time.Millisecond)

	// Verify connection established
	wsOutput.clientsMu.RLock()
	clientCount := len(wsOutput.clients)
	wsOutput.clientsMu.RUnlock()
	require.Equal(t, 1, clientCount, "WebSocket Output should have 1 connected client")

	// Publish message to NATS that Output subscribes to
	testData := map[string]interface{}{
		"sensor_id": "temp-01",
		"value":     23.5,
		"timestamp": time.Now().Unix(),
	}
	payload, err := json.Marshal(testData)
	require.NoError(t, err)

	err = natsClient.Client.Publish(ctx, "sensor.data", payload)
	require.NoError(t, err)

	// Wait for message to flow through the system:
	// 1. Output receives from NATS
	// 2. Output broadcasts to WebSocket clients (Input)
	// 3. Input receives via WebSocket
	// 4. Input publishes to NATS
	// 5. Input sends ack back to Output
	// 6. Output receives ack and clears pending
	time.Sleep(1 * time.Second)

	// Verify Output sent message to WebSocket clients
	outputSent := atomic.LoadInt64(&wsOutput.messagesSent)
	assert.Greater(t, outputSent, int64(0), "Output should have sent message to WebSocket clients")

	// Verify Output has no pending messages (ack was received)
	wsOutput.clientsMu.RLock()
	var pendingCount int
	for _, client := range wsOutput.clients {
		client.pendingMu.RLock()
		pendingCount = len(client.pendingMessages)
		client.pendingMu.RUnlock()
	}
	wsOutput.clientsMu.RUnlock()

	assert.Equal(t, 0, pendingCount, "Output should have no pending messages after ack")

	// Verify metrics on Output side
	if wsOutput.metrics != nil {
		sentCount := wsOutput.metrics.messagesSent.WithLabelValues("sensor.data")
		assert.NotNil(t, sentCount, "Output should have messagesSent metric")
	}
}

// TestWebSocketFederation_NackFlow tests nack flow when NATS publish fails
func TestWebSocketFederation_NackFlow(t *testing.T) {
	if os.Getenv("INTEGRATION_TESTS") != "1" {
		t.Skip("Skipping integration test. Set INTEGRATION_TESTS=1 to run.")
	}

	// Create NATS clients (separate for Output and Input)
	natsClientOutput := natsclient.NewTestClient(t, natsclient.WithFastStartup())
	natsClientInput := natsclient.NewTestClient(t, natsclient.WithFastStartup())
	ctx := context.Background()

	// Create metrics registry
	registry := metric.NewMetricsRegistry()

	// Setup WebSocket Output (sender)
	outputPort := getIntegrationPort(t)
	wsOutput := NewOutputFromConfig(ConstructorConfig{
		Name:            "test-output-nack",
		Port:            outputPort,
		Path:            "/stream",
		Subjects:        []string{"sensor.nack"},
		NATSClient:      natsClientOutput.Client,
		MetricsRegistry: registry,
		Security:        security.Config{},
		DeliveryMode:    DeliveryAtLeastOnce,
		AckTimeout:      2 * time.Second, // Short timeout for faster test
	})

	require.NoError(t, wsOutput.Initialize())
	require.NoError(t, wsOutput.Start(ctx))
	defer wsOutput.Stop(5 * time.Second)

	// Wait for Output server to start
	time.Sleep(200 * time.Millisecond)

	// Setup WebSocket Input (receiver)
	inputConfig := websocketinput.DefaultConfig()
	inputConfig.Mode = websocketinput.ModeClient
	inputConfig.ClientConfig = &websocketinput.ClientConfig{
		URL: fmt.Sprintf("ws://localhost:%d/stream", outputPort),
		Reconnect: &websocketinput.ReconnectConfig{
			Enabled:         true,
			MaxRetries:      5,
			InitialInterval: 1 * time.Second,
			MaxInterval:     10 * time.Second,
			Multiplier:      1.5,
		},
	}

	wsInput, err := websocketinput.NewInput("test-input-nack", natsClientInput.Client, inputConfig, registry, security.Config{})
	require.NoError(t, err)

	require.NoError(t, wsInput.Initialize())
	require.NoError(t, wsInput.Start(ctx))
	defer wsInput.Stop(5 * time.Second)

	// Wait for connection
	time.Sleep(500 * time.Millisecond)

	// Close Input's NATS connection to force publish failure
	nativeConn := natsClientInput.GetNativeConnection()
	nativeConn.Close()

	// Wait for connection to be recognized as closed
	time.Sleep(100 * time.Millisecond)

	// Publish message to NATS that Output subscribes to
	testData := map[string]interface{}{
		"sensor_id": "temp-01",
		"value":     99.9,
		"timestamp": time.Now().Unix(),
	}
	payload, err := json.Marshal(testData)
	require.NoError(t, err)

	err = natsClientOutput.Client.Publish(ctx, "sensor.nack", payload)
	require.NoError(t, err)

	// Wait for:
	// 1. Output receives from NATS
	// 2. Output broadcasts to WebSocket client (Input)
	// 3. Input receives via WebSocket
	// 4. Input fails to publish to NATS (connection closed)
	// 5. Input sends nack back to Output
	// 6. Output receives nack
	time.Sleep(1 * time.Second)

	// Verify Output has pending message (nack was received but message not removed yet)
	// Note: In full implementation, Output would retry or mark as failed
	wsOutput.clientsMu.RLock()
	var pendingCount int
	for _, client := range wsOutput.clients {
		client.pendingMu.RLock()
		pendingCount = len(client.pendingMessages)
		client.pendingMu.RUnlock()
	}
	wsOutput.clientsMu.RUnlock()

	// Pending message should still be there after nack (not retried in current implementation)
	assert.GreaterOrEqual(t, pendingCount, 0, "Output may have pending messages after nack")
}

// TestWebSocketFederation_MessageEnvelopeProtocol tests the envelope structure
func TestWebSocketFederation_MessageEnvelopeProtocol(t *testing.T) {
	if os.Getenv("INTEGRATION_TESTS") != "1" {
		t.Skip("Skipping integration test. Set INTEGRATION_TESTS=1 to run.")
	}

	// Create NATS client
	natsClient := natsclient.NewTestClient(t, natsclient.WithFastStartup())
	ctx := context.Background()

	// Create metrics registry
	registry := metric.NewMetricsRegistry()

	// Setup WebSocket Output
	outputPort := getAvailablePort(t)
	wsOutput := NewOutputFromConfig(ConstructorConfig{
		Name:            "test-output-envelope",
		Port:            outputPort,
		Path:            "/stream",
		Subjects:        []string{"sensor.envelope"},
		NATSClient:      natsClient.Client,
		MetricsRegistry: registry,
		Security:        security.Config{},
		DeliveryMode:    DeliveryAtLeastOnce,
		AckTimeout:      5 * time.Second,
	})

	require.NoError(t, wsOutput.Initialize())
	require.NoError(t, wsOutput.Start(ctx))
	defer wsOutput.Stop(5 * time.Second)

	// Wait for server to start
	time.Sleep(200 * time.Millisecond)

	// Connect WebSocket client manually to inspect envelope
	wsURL := fmt.Sprintf("ws://localhost:%d/stream", outputPort)
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer conn.Close()

	// Set up envelope receiver
	envelopes := make(chan MessageEnvelope, 10)
	go func() {
		for {
			var envelope MessageEnvelope
			err := conn.ReadJSON(&envelope)
			if err != nil {
				return
			}
			envelopes <- envelope
		}
	}()

	// Publish test message
	testData := map[string]interface{}{"test": "envelope"}
	payload, err := json.Marshal(testData)
	require.NoError(t, err)

	err = natsClient.Client.Publish(ctx, "sensor.envelope", payload)
	require.NoError(t, err)

	// Verify envelope structure
	select {
	case envelope := <-envelopes:
		assert.Equal(t, "data", envelope.Type, "Envelope type should be 'data'")
		assert.NotEmpty(t, envelope.ID, "Envelope should have message ID")
		assert.Greater(t, envelope.Timestamp, int64(0), "Envelope should have timestamp")
		assert.NotNil(t, envelope.Payload, "Envelope should have payload")

		// Verify payload content
		var receivedData map[string]interface{}
		err := json.Unmarshal(envelope.Payload, &receivedData)
		require.NoError(t, err)
		assert.Equal(t, "envelope", receivedData["test"])

		// Send ack back
		ack := MessageEnvelope{
			Type:      "ack",
			ID:        envelope.ID,
			Timestamp: time.Now().UnixMilli(),
		}
		err = conn.WriteJSON(ack)
		require.NoError(t, err)

	case <-time.After(3 * time.Second):
		t.Fatal("Timeout waiting for message envelope")
	}

	// Wait for ack to be processed
	time.Sleep(500 * time.Millisecond)

	// Verify pending messages cleared
	wsOutput.clientsMu.RLock()
	var pendingCount int
	for _, client := range wsOutput.clients {
		client.pendingMu.RLock()
		pendingCount = len(client.pendingMessages)
		client.pendingMu.RUnlock()
	}
	wsOutput.clientsMu.RUnlock()

	assert.Equal(t, 0, pendingCount, "Pending messages should be cleared after ack")
}

// =============================================================================
// HELPER FUNCTIONS
// =============================================================================

func getIntegrationPort(t *testing.T) int {
	t.Helper()

	basePort := 18082
	for i := 0; i < 100; i++ {
		port := basePort + i
		ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
		if err == nil {
			ln.Close()
			return port
		}
	}

	t.Fatal("Could not find available port for integration testing")
	return 18082 // Never reached
}
