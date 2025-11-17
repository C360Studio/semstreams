package websocket

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/c360/semstreams/metric"
	"github.com/c360/semstreams/natsclient"
	"github.com/c360/semstreams/pkg/security"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// ACK/NACK PROTOCOL TESTS
// =============================================================================

// TestWebSocketInput_SendAck tests ack message sending
func TestWebSocketInput_SendAck(t *testing.T) {
	// Create mock WebSocket server to receive ack
	var receivedMsg MessageEnvelope
	var receivedMu sync.Mutex
	received := make(chan bool, 1)

	upgrader := websocket.Upgrader{
		CheckOrigin: func(_ *http.Request) bool { return true },
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Logf("Upgrade error: %v", err)
			return
		}
		defer conn.Close()

		// Read ack message
		err = conn.ReadJSON(&receivedMsg)
		if err != nil {
			t.Logf("Read error: %v", err)
			return
		}

		receivedMu.Lock()
		received <- true
		receivedMu.Unlock()
	}))
	defer server.Close()

	// Connect to mock server
	wsURL := "ws" + server.URL[4:] // Replace http with ws
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer conn.Close()

	// Create Input component
	natsClient := natsclient.NewTestClient(t)
	config := DefaultConfig()
	config.Mode = ModeServer
	config.ServerConfig.HTTPPort = getAvailablePort(t)
	config.ServerConfig.Path = "/ws"

	registry := metric.NewMetricsRegistry()
	input, err := NewInput("test-input", natsClient.Client, config, registry, security.Config{})
	require.NoError(t, err)

	// Send ack
	messageID := "test-msg-001"
	input.sendAck(conn, messageID)

	// Verify ack received
	select {
	case <-received:
		assert.Equal(t, "ack", receivedMsg.Type)
		assert.Equal(t, messageID, receivedMsg.ID)
		assert.Greater(t, receivedMsg.Timestamp, int64(0))
		assert.Nil(t, receivedMsg.Payload)
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for ack message")
	}
}

// TestWebSocketInput_SendNack tests nack message sending with error details
func TestWebSocketInput_SendNack(t *testing.T) {
	// Create mock WebSocket server to receive nack
	var receivedMsg MessageEnvelope
	var receivedMu sync.Mutex
	received := make(chan bool, 1)

	upgrader := websocket.Upgrader{
		CheckOrigin: func(_ *http.Request) bool { return true },
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Logf("Upgrade error: %v", err)
			return
		}
		defer conn.Close()

		// Read nack message
		err = conn.ReadJSON(&receivedMsg)
		if err != nil {
			t.Logf("Read error: %v", err)
			return
		}

		receivedMu.Lock()
		received <- true
		receivedMu.Unlock()
	}))
	defer server.Close()

	// Connect to mock server
	wsURL := "ws" + server.URL[4:] // Replace http with ws
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer conn.Close()

	// Create Input component
	natsClient := natsclient.NewTestClient(t)
	config := DefaultConfig()
	config.Mode = ModeServer
	config.ServerConfig.HTTPPort = getAvailablePort(t)
	config.ServerConfig.Path = "/ws"

	registry := metric.NewMetricsRegistry()
	input, err := NewInput("test-input", natsClient.Client, config, registry, security.Config{})
	require.NoError(t, err)

	// Send nack with error details
	messageID := "test-msg-002"
	reason := "publish_failed"
	errorMsg := "NATS connection lost"
	input.sendNack(conn, messageID, reason, errorMsg)

	// Verify nack received
	select {
	case <-received:
		assert.Equal(t, "nack", receivedMsg.Type)
		assert.Equal(t, messageID, receivedMsg.ID)
		assert.Greater(t, receivedMsg.Timestamp, int64(0))
		assert.NotNil(t, receivedMsg.Payload)

		// Parse nack payload
		var nackPayload map[string]string
		err := json.Unmarshal(receivedMsg.Payload, &nackPayload)
		require.NoError(t, err)
		assert.Equal(t, reason, nackPayload["reason"])
		assert.Equal(t, errorMsg, nackPayload["error"])
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for nack message")
	}
}

// TestWebSocketInput_HandleMessage_SuccessfulPublish tests ack on successful NATS publish
func TestWebSocketInput_HandleMessage_SuccessfulPublish(t *testing.T) {
	// Create mock WebSocket server to receive ack
	var receivedMsg MessageEnvelope
	received := make(chan bool, 1)

	upgrader := websocket.Upgrader{
		CheckOrigin: func(_ *http.Request) bool { return true },
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		// Read ack message
		err = conn.ReadJSON(&receivedMsg)
		if err != nil {
			return
		}
		received <- true
	}))
	defer server.Close()

	// Connect to mock server
	wsURL := "ws" + server.URL[4:]
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer conn.Close()

	// Create Input component with real NATS
	natsClient := natsclient.NewTestClient(t)
	config := DefaultConfig()
	config.Mode = ModeServer
	config.ServerConfig.HTTPPort = getAvailablePort(t)
	config.ServerConfig.Path = "/ws"

	registry := metric.NewMetricsRegistry()
	input, err := NewInput("test-input", natsClient.Client, config, registry, security.Config{})
	require.NoError(t, err)

	// Create data message envelope
	testData := map[string]interface{}{"sensor": "temp-01", "value": 23.5}
	payload, err := json.Marshal(testData)
	require.NoError(t, err)

	envelope := &MessageEnvelope{
		Type:      "data",
		ID:        "msg-001",
		Timestamp: time.Now().UnixMilli(),
		Payload:   json.RawMessage(payload),
	}

	// Handle message (should publish to NATS and send ack)
	ctx := context.Background()
	input.handleMessage(ctx, envelope, conn)

	// Verify ack received
	select {
	case <-received:
		assert.Equal(t, "ack", receivedMsg.Type)
		assert.Equal(t, "msg-001", receivedMsg.ID)
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for ack after successful publish")
	}
}

// TestWebSocketInput_HandleMessage_FailedPublish tests nack on failed NATS publish
func TestWebSocketInput_HandleMessage_FailedPublish(t *testing.T) {
	// Create mock WebSocket server to receive nack
	var receivedMsg MessageEnvelope
	received := make(chan bool, 1)

	upgrader := websocket.Upgrader{
		CheckOrigin: func(_ *http.Request) bool { return true },
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		// Read nack message
		err = conn.ReadJSON(&receivedMsg)
		if err != nil {
			return
		}
		received <- true
	}))
	defer server.Close()

	// Connect to mock server
	wsURL := "ws" + server.URL[4:]
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer conn.Close()

	// Create Input component with NATS client
	natsClient := natsclient.NewTestClient(t)
	config := DefaultConfig()
	config.Mode = ModeServer
	config.ServerConfig.HTTPPort = getAvailablePort(t)
	config.ServerConfig.Path = "/ws"

	registry := metric.NewMetricsRegistry()
	input, err := NewInput("test-input", natsClient.Client, config, registry, security.Config{})
	require.NoError(t, err)

	// Close the underlying NATS connection to force publish failure
	nativeConn := natsClient.GetNativeConnection()
	nativeConn.Close()

	// Create data message envelope
	testData := map[string]interface{}{"sensor": "temp-01", "value": 23.5}
	payload, err := json.Marshal(testData)
	require.NoError(t, err)

	envelope := &MessageEnvelope{
		Type:      "data",
		ID:        "msg-002",
		Timestamp: time.Now().UnixMilli(),
		Payload:   json.RawMessage(payload),
	}

	// Handle message (should fail to publish and send nack)
	ctx := context.Background()
	input.handleMessage(ctx, envelope, conn)

	// Verify nack received
	select {
	case <-received:
		assert.Equal(t, "nack", receivedMsg.Type)
		assert.Equal(t, "msg-002", receivedMsg.ID)

		// Parse nack payload
		var nackPayload map[string]string
		err := json.Unmarshal(receivedMsg.Payload, &nackPayload)
		require.NoError(t, err)
		assert.Equal(t, "publish_failed", nackPayload["reason"])
		assert.NotEmpty(t, nackPayload["error"])
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for nack after failed publish")
	}
}

// TestWebSocketInput_NilConnectionHandling tests that nil connections don't cause panics
func TestWebSocketInput_NilConnectionHandling(t *testing.T) {
	natsClient := natsclient.NewTestClient(t)
	config := DefaultConfig()
	config.Mode = ModeServer
	config.ServerConfig.HTTPPort = getAvailablePort(t)
	config.ServerConfig.Path = "/ws"

	registry := metric.NewMetricsRegistry()
	input, err := NewInput("test-input", natsClient.Client, config, registry, security.Config{})
	require.NoError(t, err)

	// Test sendAck with nil connection (should not panic)
	assert.NotPanics(t, func() {
		input.sendAck(nil, "test-msg-id")
	})

	// Test sendNack with nil connection (should not panic)
	assert.NotPanics(t, func() {
		input.sendNack(nil, "test-msg-id", "test_reason", "test error")
	})
}

// TestWebSocketInput_MessageEnvelopeTypes tests handling of different message types
func TestWebSocketInput_MessageEnvelopeTypes(t *testing.T) {
	natsClient := natsclient.NewTestClient(t)
	config := DefaultConfig()
	config.Mode = ModeServer
	config.ServerConfig.HTTPPort = getAvailablePort(t)
	config.ServerConfig.Path = "/ws"

	registry := metric.NewMetricsRegistry()
	input, err := NewInput("test-input", natsClient.Client, config, registry, security.Config{})
	require.NoError(t, err)

	ctx := context.Background()

	// Test control message types (ack, nack, slow) - should be ignored
	controlTypes := []string{"ack", "nack", "slow"}
	for _, msgType := range controlTypes {
		t.Run(msgType, func(t *testing.T) {
			envelope := &MessageEnvelope{
				Type:      msgType,
				ID:        "control-001",
				Timestamp: time.Now().UnixMilli(),
			}

			// Should not panic or error
			assert.NotPanics(t, func() {
				input.handleMessage(ctx, envelope, nil)
			})
		})
	}

	// Test unknown message type - should not panic
	t.Run("unknown_type", func(t *testing.T) {
		envelope := &MessageEnvelope{
			Type:      "unknown_message_type",
			ID:        "unknown-001",
			Timestamp: time.Now().UnixMilli(),
		}

		// Should not panic when handling unknown message type
		assert.NotPanics(t, func() {
			input.handleMessage(ctx, envelope, nil)
		})
	})
}

// =============================================================================
// HELPER FUNCTIONS
// =============================================================================

func getAvailablePort(t *testing.T) int {
	t.Helper()

	basePort := 9082
	for i := 0; i < 100; i++ {
		port := basePort + i
		ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
		if err == nil {
			_ = ln.Close()
			return port
		}
	}

	t.Fatal("Could not find available port for testing")
	return 9082 // Never reached
}
