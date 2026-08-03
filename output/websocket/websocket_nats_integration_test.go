//go:build integration

package websocket

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

// TestWebSocketOutput_Creation_ValidConfig tests component creation with valid ComponentConfig
func TestIntegration_WebSocketOutput_Creation_ValidConfig(t *testing.T) {
	// Use testcontainer for real NATS
	testClient := natsclient.NewTestClient(t, natsclient.WithJetStream())

	// Create WebSocket config using PortConfig format
	wsConfig := testWebSocketConfig(8082, "/ws", []string{"test.entity.>", "test.rule.>"})
	configJSON, err := json.Marshal(wsConfig)
	require.NoError(t, err)

	// Create component dependencies
	deps := component.Dependencies{
		NATSClient: testClient.Client,
		Platform: component.PlatformMeta{
			Org:      "test",
			Platform: "test-platform",
		},
	}

	// Create REAL component
	wsOutput, err := CreateOutput(configJSON, deps)
	require.NoError(t, err)
	require.NotNil(t, wsOutput)

	// Verify real behavior - component metadata
	meta := wsOutput.Meta()
	require.Equal(t, "output", meta.Type)
	require.Contains(t, meta.Description, ":8082")
	require.Contains(t, meta.Description, "/ws")

	// Verify real behavior - WebSocket port configuration
	outputPorts := wsOutput.OutputPorts()
	require.Len(t, outputPorts, 1)
	wsPort := outputPorts[0].Config.(component.NetworkPort)
	require.Equal(t, 8082, wsPort.Port)
	require.Equal(t, "websocket", wsPort.Protocol)
}

// TestWebSocketOutput_Creation_InvalidPort tests component creation with invalid port
func TestIntegration_WebSocketOutput_Creation_InvalidPort(t *testing.T) {
	testClient := natsclient.NewTestClient(t, natsclient.WithJetStream())

	testCases := []struct {
		name          string
		port          int
		expectedError string
	}{
		{"port too low", 500, "port 500 out of range"},
		{"port too high", 99999, "port 99999 out of range"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create WebSocket config with test port using PortConfig format
			wsConfig := testWebSocketConfig(tc.port, "/ws", []string{"test.>"})
			configJSON, err := json.Marshal(wsConfig)
			require.NoError(t, err)

			// Create component dependencies
			deps := component.Dependencies{
				NATSClient: testClient.Client,
				Platform: component.PlatformMeta{
					Org:      "test",
					Platform: "test-platform",
				},
			}

			_, err = CreateOutput(configJSON, deps)
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.expectedError)
		})
	}
}

// TestWebSocketOutput_Integration_NATSToWebSocket tests complete NATS → WebSocket message flow
func TestIntegration_WebSocketOutput_Integration_NATSToWebSocket(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	testClient := natsclient.NewTestClient(t, natsclient.WithJetStream())

	// Find available port for test
	port := findAvailablePort(t)

	// Create WebSocket config using PortConfig format
	wsConfig := testWebSocketConfig(port, "/test", []string{"test.integration.ws"})
	configJSON, err := json.Marshal(wsConfig)
	require.NoError(t, err)

	// Create component dependencies
	deps := component.Dependencies{
		NATSClient: testClient.Client,
		Platform: component.PlatformMeta{
			Org:      "test",
			Platform: "test-platform",
		},
	}

	// Create and start WebSocket output
	wsOutput, err := CreateOutput(configJSON, deps)
	require.NoError(t, err)

	wsLifecycle := wsOutput.(component.LifecycleComponent)
	err = wsLifecycle.Initialize()
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err = wsLifecycle.Start(ctx)
	require.NoError(t, err)
	defer wsLifecycle.Stop(5 * time.Second)

	// Give server time to start
	time.Sleep(200 * time.Millisecond)

	// Connect WebSocket client
	wsURL := fmt.Sprintf("ws://127.0.0.1:%d/test", port)
	wsConn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer wsConn.Close()

	// Set up message receiver
	received := make(chan map[string]any, 1)
	go func() {
		for {
			var msg map[string]any
			err := wsConn.ReadJSON(&msg)
			if err != nil {
				return
			}
			received <- msg
		}
	}()

	// Give WebSocket connection time to be registered
	time.Sleep(100 * time.Millisecond)

	// Publish NATS message
	testMessage := map[string]any{
		"type": "test_entity",
		"id":   "test-123",
		"data": "integration test data",
	}

	msgBytes, _ := json.Marshal(testMessage)
	nativeConn := testClient.GetNativeConnection()
	err = nativeConn.Publish("test.integration.ws", msgBytes)
	require.NoError(t, err)

	// Verify message received via WebSocket
	select {
	case receivedMsg := <-received:
		// Messages are wrapped in MessageEnvelope protocol
		require.Equal(t, "data", receivedMsg["type"], "Envelope type should be 'data'")
		require.NotEmpty(t, receivedMsg["id"], "Envelope should have message ID")
		require.NotEmpty(t, receivedMsg["timestamp"], "Envelope should have timestamp")

		// Extract the actual message payload
		payload, ok := receivedMsg["payload"].(map[string]any)
		require.True(t, ok, "Payload should be a map")

		// Verify the actual message content within payload
		require.Equal(t, "test_entity", payload["type"])
		require.Equal(t, "test-123", payload["id"])
		require.Equal(t, "test.integration.ws", payload["subject"])
		require.NotEmpty(t, payload["timestamp"])
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for WebSocket message")
	}
}

// TestWebSocketOutput_Lifecycle_StartStop tests complete lifecycle behavior
func TestIntegration_WebSocketOutput_Lifecycle_StartStop(t *testing.T) {
	testClient := natsclient.NewTestClient(t, natsclient.WithJetStream())

	// Create WebSocket config using PortConfig format
	port := findAvailablePort(t)
	wsConfig := testWebSocketConfig(port, "/test", []string{"test.lifecycle"})
	configJSON, err := json.Marshal(wsConfig)
	require.NoError(t, err)

	// Create component dependencies
	deps := component.Dependencies{
		NATSClient: testClient.Client,
		Platform: component.PlatformMeta{
			Org:      "test",
			Platform: "test-platform",
		},
	}

	wsOutput, err := CreateOutput(configJSON, deps)
	require.NoError(t, err)

	wsLifecycle := wsOutput.(component.LifecycleComponent)

	// Test Initialize
	err = wsLifecycle.Initialize()
	require.NoError(t, err)

	// Test Start
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = wsLifecycle.Start(ctx)
	require.NoError(t, err)

	// Verify actually running
	health := wsLifecycle.Health()
	require.True(t, health.Healthy, "Component should be healthy after start")

	// Test Stop
	err = wsLifecycle.Stop(5 * time.Second)
	require.NoError(t, err)

	// Verify actually stopped
	health = wsLifecycle.Health()
	require.False(t, health.Healthy, "Component should be unhealthy after stop")
}

// TestWebSocketOutput_Integration_MultipleClients tests multiple WebSocket clients receiving messages
func TestIntegration_WebSocketOutput_Integration_MultipleClients(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	testClient := natsclient.NewTestClient(t, natsclient.WithJetStream())
	port := findAvailablePort(t)

	// Create WebSocket config using PortConfig format
	wsConfig := testWebSocketConfig(port, "/multi", []string{"test.multi.broadcast"})
	configJSON, err := json.Marshal(wsConfig)
	require.NoError(t, err)

	// Create component dependencies
	deps := component.Dependencies{
		NATSClient: testClient.Client,
		Platform: component.PlatformMeta{
			Org:      "test",
			Platform: "test-platform",
		},
	}

	wsOutput, err := CreateOutput(configJSON, deps)
	require.NoError(t, err)

	wsLifecycle := wsOutput.(component.LifecycleComponent)
	require.NoError(t, wsLifecycle.Initialize())

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	require.NoError(t, wsLifecycle.Start(ctx))
	defer wsLifecycle.Stop(5 * time.Second)

	time.Sleep(200 * time.Millisecond) // Allow server to start

	// Connect multiple WebSocket clients
	const numClients = 3
	clients := make([]*websocket.Conn, numClients)
	receivers := make([]chan map[string]any, numClients)

	wsURL := fmt.Sprintf("ws://127.0.0.1:%d/multi", port)

	for i := 0; i < numClients; i++ {
		conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		require.NoError(t, err)
		clients[i] = conn
		receivers[i] = make(chan map[string]any, 1)

		// Start message receiver for this client
		go func(clientIdx int) {
			for {
				var msg map[string]any
				err := clients[clientIdx].ReadJSON(&msg)
				if err != nil {
					return
				}
				receivers[clientIdx] <- msg
			}
		}(i)
	}

	// Cleanup all clients
	defer func() {
		for _, conn := range clients {
			if conn != nil {
				conn.Close()
			}
		}
	}()

	time.Sleep(100 * time.Millisecond) // Allow clients to connect

	// Publish NATS message
	testMessage := map[string]any{
		"type":    "broadcast_test",
		"id":      "multi-123",
		"content": "message to all clients",
	}

	msgBytes, _ := json.Marshal(testMessage)
	nativeConn := testClient.GetNativeConnection()
	err = nativeConn.Publish("test.multi.broadcast", msgBytes)
	require.NoError(t, err)

	// Verify all clients received the message
	for i := 0; i < numClients; i++ {
		select {
		case receivedMsg := <-receivers[i]:
			// Messages are wrapped in MessageEnvelope protocol
			require.Equal(t, "data", receivedMsg["type"], "Envelope type should be 'data'")

			// Extract the actual message payload
			payload, ok := receivedMsg["payload"].(map[string]any)
			require.True(t, ok, "Payload should be a map")

			// Verify the actual message content within payload
			require.Equal(t, "broadcast_test", payload["type"])
			require.Equal(t, "multi-123", payload["id"])
			require.Equal(t, "test.multi.broadcast", payload["subject"])
			t.Logf("Client %d successfully received message", i)
		case <-time.After(5 * time.Second):
			t.Fatalf("Timeout waiting for message on client %d", i)
		}
	}
}

// TestWebSocketOutput_Configuration_SubjectParsing tests different subject configuration formats
func TestIntegration_WebSocketOutput_Configuration_SubjectParsing(t *testing.T) {
	testClient := natsclient.NewTestClient(t, natsclient.WithJetStream())

	testCases := []struct {
		name          string
		subjectConfig any
		expectedCount int
		expectedFirst string
	}{
		{
			name:          "string_subject",
			subjectConfig: "single.subject",
			expectedCount: 1,
			expectedFirst: "single.subject",
		},
		{
			name:          "string_slice_subjects",
			subjectConfig: []string{"first.subject", "second.subject"},
			expectedCount: 2,
			expectedFirst: "first.subject",
		},
		{
			name:          "default_subjects",
			subjectConfig: nil, // Will use defaults
			expectedCount: 2,
			expectedFirst: "process.robotics.>",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Determine subjects to use
			var subjects []string
			switch v := tc.subjectConfig.(type) {
			case string:
				subjects = []string{v}
			case []string:
				subjects = v
			case nil:
				// Use defaults
				subjects = []string{"process.robotics.>", "process.graph.entity.>"}
			}

			// Create WebSocket config using proper Ports structure
			wsConfig := testWebSocketConfig(findAvailablePort(t), "/test", subjects)
			configJSON, err := json.Marshal(wsConfig)
			require.NoError(t, err)

			// Create component dependencies
			deps := component.Dependencies{
				NATSClient: testClient.Client,
				Platform: component.PlatformMeta{
					Org:      "test",
					Platform: "test-platform",
				},
			}

			wsOutput, err := CreateOutput(configJSON, deps)
			require.NoError(t, err)

			// Verify input ports match expected subjects
			inputPorts := wsOutput.InputPorts()
			require.Len(t, inputPorts, tc.expectedCount)

			natsPort := inputPorts[0].Config.(component.NATSPort)
			require.Equal(t, tc.expectedFirst, natsPort.Subject)
		})
	}
}
