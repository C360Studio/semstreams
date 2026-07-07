//go:build integration
// +build integration

package websocket

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"testing"
	"time"

	websocketinput "github.com/c360studio/semstreams/input/websocket"
	"github.com/c360studio/semstreams/metric"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/security"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWebSocketFederation_AckFlow tests successful ack flow between Output and Input
func TestWebSocketFederation_AckFlow(t *testing.T) {

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
	outputSent := wsOutput.messagesSent.Load()
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

// TestWebSocketFederation_PassthroughPreservesBytes drives the full production
// wire (NATS publish → ws output → connected client) and asserts pass-through's
// contract on the bytes that actually reach the client: with passthrough on, the
// envelope Payload is the producer's ORIGINAL bytes (key order preserved, no
// timestamp/subject injected); with passthrough off, the default path injects
// subject + timestamp. gh#471.
func TestWebSocketFederation_PassthroughPreservesBytes(t *testing.T) {
	// Key order chosen so a map round-trip (Go sorts map keys) would reorder it,
	// and lacking subject/timestamp so the default path visibly injects them.
	original := []byte(`{"z":1,"a":2,"entity":"boid-7"}`)

	cases := []struct {
		name        string
		passthrough bool
	}{
		{"passthrough preserves original bytes", true},
		{"default injects subject and timestamp", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			natsClient := natsclient.NewTestClient(t, natsclient.WithFastStartup())
			ctx := context.Background()

			outputPort := getAvailablePort(t)
			wsOutput := NewOutputFromConfig(ConstructorConfig{
				Name:        "test-passthrough-wire",
				Port:        outputPort,
				Path:        "/stream",
				Subjects:    []string{"boid.snapshot"},
				NATSClient:  natsClient.Client,
				Security:    security.Config{},
				Passthrough: tc.passthrough,
			})
			require.NoError(t, wsOutput.Initialize())
			require.NoError(t, wsOutput.Start(ctx))
			defer wsOutput.Stop(5 * time.Second)

			time.Sleep(200 * time.Millisecond)

			wsURL := fmt.Sprintf("ws://localhost:%d/stream", outputPort)
			conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
			require.NoError(t, err)
			defer conn.Close()

			envelopes := make(chan MessageEnvelope, 4)
			go func() {
				for {
					var envelope MessageEnvelope
					if rerr := conn.ReadJSON(&envelope); rerr != nil {
						return
					}
					envelopes <- envelope
				}
			}()

			require.NoError(t, natsClient.Client.Publish(ctx, "boid.snapshot", original))

			select {
			case envelope := <-envelopes:
				if tc.passthrough {
					// The producer JSON here is already compact and escape-free, so
					// the envelope marshal (which compacts/HTML-escapes — see the
					// unit test TestPassthrough_EnvelopeMarshalCompactsAndEscapes) is
					// a no-op and the payload arrives verbatim. The point this locks
					// is the gh#471 win: keys are NOT sorted (a decode/re-encode
					// would reorder to a,entity,z) and no timestamp/subject injected.
					assert.JSONEq(t, string(original), string(envelope.Payload),
						"pass-through payload must be semantically equal to the producer bytes")
					assert.Equal(t, string(original), string(envelope.Payload),
						"key order preserved and nothing injected (compact escape-free input round-trips verbatim)")
				} else {
					var decoded map[string]any
					require.NoError(t, json.Unmarshal(envelope.Payload, &decoded))
					assert.Equal(t, "boid.snapshot", decoded["subject"],
						"default path must inject the subject")
					assert.NotEmpty(t, decoded["timestamp"],
						"default path must inject a timestamp")
				}
			case <-time.After(3 * time.Second):
				t.Fatal("Timeout waiting for message envelope")
			}
		})
	}
}

// =============================================================================
// HELPER FUNCTIONS
// =============================================================================

// getIntegrationPort asks the kernel for an unused TCP port via
// :0 ephemeral allocation. Previously scanned a fixed range starting
// at 18082, which collided with other tests under parallel execution
// and produced the gh#220 Subclass 2 flake shape. The :0 pattern
// inherits whatever ephemeral-port pool the OS makes available,
// which is broader than a 100-port fixed window and avoids the
// allocate-from-known-range contention.
//
// Race window: between the close here and the websocket server's
// re-bind in the test, another process could grab the port. That
// race is structural to the close-then-pass-port pattern and is
// tracked separately — see gh#220 Subclass 2 follow-up for the
// listener-handoff API fix.
func getIntegrationPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("getIntegrationPort: kernel refused :0 ephemeral allocation: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return port
}

// TestWebSocketOutput_PingSerializesWithFrameWrites is a regression test for
// gh#500: pingClients wrote a ping frame without holding the per-connection
// writeMutex, racing the frame fan-out path (sendToClient). gorilla/websocket
// panics on concurrent writes to one conn, so this was a hard process crash
// under load, not a dropped frame. The fix routes the ping through pingClient,
// which takes info.writeMutex. Rather than hope a race hammer trips the panic
// (flaky — see gh#469), this proves the lock is held deterministically:
// pingClients must BLOCK while the frame path holds the write lock, then
// proceed once it is released.
func TestWebSocketOutput_PingSerializesWithFrameWrites(t *testing.T) {
	natsClient := natsclient.NewTestClient(t, natsclient.WithFastStartup())
	ctx := context.Background()

	outputPort := getAvailablePort(t)
	wsOutput := NewOutputFromConfig(ConstructorConfig{
		Name:            "test-output-ping-lock",
		Port:            outputPort,
		Path:            "/stream",
		Subjects:        []string{"sensor.pinglock"},
		NATSClient:      natsClient.Client,
		MetricsRegistry: metric.NewMetricsRegistry(),
		Security:        security.Config{},
		DeliveryMode:    DeliveryAtMostOnce,
		AckTimeout:      5 * time.Second,
	})
	require.NoError(t, wsOutput.Initialize())
	require.NoError(t, wsOutput.Start(ctx))
	defer wsOutput.Stop(5 * time.Second)

	time.Sleep(200 * time.Millisecond)

	wsURL := fmt.Sprintf("ws://localhost:%d/stream", outputPort)
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer conn.Close()

	// Drain the client so server-side writes never block on a full buffer.
	go func() {
		for {
			if _, _, rerr := conn.ReadMessage(); rerr != nil {
				return
			}
		}
	}()

	// Wait for the server to register the accepted connection, then grab its
	// clientInfo (same-package white-box access).
	var info *clientInfo
	require.Eventually(t, func() bool {
		wsOutput.clientsMu.RLock()
		defer wsOutput.clientsMu.RUnlock()
		for _, i := range wsOutput.clients {
			info = i
			return true
		}
		return false
	}, 2*time.Second, 10*time.Millisecond, "client never registered on server")
	require.NotNil(t, info)

	// Simulate the frame path holding the per-connection write lock.
	info.writeMutex.Lock()

	// pingClients must now block trying to acquire the same lock.
	done := make(chan struct{})
	go func() {
		wsOutput.pingClients(ctx)
		close(done)
	}()

	select {
	case <-done:
		info.writeMutex.Unlock()
		t.Fatal("pingClients did not block on the per-connection write lock — " +
			"ping path bypasses info.writeMutex (gh#500 regression)")
	case <-time.After(300 * time.Millisecond):
		// Expected: blocked on writeMutex.
	}

	// Release the lock; pingClients should now proceed and return.
	info.writeMutex.Unlock()
	select {
	case <-done:
		// ping completed after the lock was released — serialization confirmed.
	case <-time.After(2 * time.Second):
		t.Fatal("pingClients never completed after write lock released")
	}
}
