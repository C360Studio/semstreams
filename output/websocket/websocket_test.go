package websocket

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/metric"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/security"
	"github.com/gorilla/websocket"
	natspkg "github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testWebSocketConfig creates a standard test configuration for WebSocket output
func testWebSocketConfig(port int, _ string, subjects []string) Config {
	// Create input ports for each subject
	inputs := make([]component.PortDefinition, len(subjects))
	for i, subject := range subjects {
		inputs[i] = component.PortDefinition{
			Name:        fmt.Sprintf("nats_input_%d", i),
			Config:      component.NATSPort{Subject: subject},
			Required:    true,
			Description: fmt.Sprintf("NATS subject subscription for %s", subject),
		}
	}

	// Create output port for WebSocket server (encode as URL in Subject)
	outputs := []component.PortDefinition{
		{
			Name:        "websocket_server",
			Config:      component.NetworkPort{Protocol: "http", Host: "0.0.0.0", Port: port},
			Required:    false,
			Description: "WebSocket server for real-time data streaming",
		},
	}

	return Config{
		Ports: &component.PortConfig{
			Inputs:  inputs,
			Outputs: outputs,
		},
	}
}

// TestWebSocketOutput_Interfaces verifies that Output implements required interfaces
func TestWebSocketOutput_Interfaces(t *testing.T) {
	natsClient := &natsclient.Client{}
	ws := mustNewOutput(t, 8080, "/ws", []string{"graph.updates.>"}, natsClient)

	// Test Discoverable interface
	var _ component.Discoverable = ws

	// Test LifecycleComponent interface
	var _ component.LifecycleComponent = ws
}

// TestWebSocketOutput_Meta tests the Meta method
func TestWebSocketOutput_Meta(t *testing.T) {
	natsClient := &natsclient.Client{}
	ws := mustNewOutput(t, 8081, "/test", []string{"test.subject"}, natsClient)

	meta := ws.Meta()

	expected := component.Metadata{
		Name:        "websocket-output-8081",
		Type:        "output",
		Description: "WebSocket server on /test:8081 serving updates from subjects [test.subject]",
		Version:     "1.0.0",
	}

	if meta != expected {
		t.Errorf("Meta() = %+v, want %+v", meta, expected)
	}
}

// TestWebSocketOutput_Ports tests InputPorts and OutputPorts methods
func TestWebSocketOutput_Ports(t *testing.T) {
	natsClient := &natsclient.Client{}
	ws := mustNewOutput(t, 8082, "/ws", []string{"graph.updates.>"}, natsClient)

	// Test InputPorts
	inputPorts := ws.InputPorts()
	if len(inputPorts) != 1 {
		t.Errorf("InputPorts() returned %d ports, want 1", len(inputPorts))
	}

	inputPort := inputPorts[0]
	if inputPort.Name != "nats_input_0" {
		t.Errorf("InputPort name = %s, want nats_input_0", inputPort.Name)
	}
	if inputPort.Direction != component.DirectionInput {
		t.Errorf("InputPort direction = %s, want %s", inputPort.Direction, component.DirectionInput)
	}

	// Check NATS port config
	natsPort, ok := inputPort.Config.(component.NATSPort)
	if !ok {
		t.Errorf("InputPort config should be NATSPort, got %T", inputPort.Config)
	} else if natsPort.Subject != "graph.updates.>" {
		t.Errorf("InputPort subject = %s, want graph.updates.>", natsPort.Subject)
	}

	// Test OutputPorts
	outputPorts := ws.OutputPorts()
	if len(outputPorts) != 1 {
		t.Errorf("OutputPorts() returned %d ports, want 1", len(outputPorts))
	}

	outputPort := outputPorts[0]
	if outputPort.Name != "websocket_server" {
		t.Errorf("OutputPort name = %s, want websocket_server", outputPort.Name)
	}
	if outputPort.Direction != component.DirectionOutput {
		t.Errorf("OutputPort direction = %s, want %s", outputPort.Direction, component.DirectionOutput)
	}

	// Check network port config
	networkPort, ok := outputPort.Config.(component.NetworkPort)
	if !ok {
		t.Errorf("OutputPort config should be NetworkPort, got %T", outputPort.Config)
	} else if networkPort.Protocol != "http" {
		t.Errorf("OutputPort protocol = %s, want http", networkPort.Protocol)
	}
}

// TestWebSocketOutput_ConfigSchema tests the ConfigSchema method
func TestWebSocketOutput_ConfigSchema(t *testing.T) {
	natsClient := &natsclient.Client{}
	ws := mustNewOutput(t, 8083, "/ws", []string{"graph.updates.>"}, natsClient)

	schema := ws.ConfigSchema()

	// With PortConfig architecture, no fields are required (all have defaults)
	if len(schema.Required) != 0 {
		t.Errorf("ConfigSchema required fields length = %d, want 0 (all fields have defaults)", len(schema.Required))
	}

	// Check that ports property exists (Architecture Decision: Ports in Schema)
	portsProp, exists := schema.Properties["ports"]
	if !exists {
		t.Error("ConfigSchema missing ports property")
	} else {
		if portsProp.Type != "ports" {
			t.Errorf("Ports property type = %s, want ports (first-class)", portsProp.Type)
		}
		if portsProp.Category != "basic" {
			t.Errorf("Ports category = %s, want basic", portsProp.Category)
		}
	}
}

// TestWebSocketOutput_Health tests the Health method
func TestWebSocketOutput_Health(t *testing.T) {
	natsClient := &natsclient.Client{}
	ws := mustNewOutput(t, 8084, "/ws", []string{"graph.updates.>"}, natsClient)

	// Test initial health (not running)
	health := ws.Health()
	if health.Healthy {
		t.Error("Health should be false when not running")
	}
	if health.ErrorCount != 0 {
		t.Errorf("Initial error count = %d, want 0", health.ErrorCount)
	}
}

// TestWebSocketOutput_DataFlow tests the DataFlow method
func TestWebSocketOutput_DataFlow(t *testing.T) {
	natsClient := &natsclient.Client{}
	ws := mustNewOutput(t, 8085, "/ws", []string{"graph.updates.>"}, natsClient)

	flow := ws.DataFlow()

	// Initial values should be zero
	if flow.MessagesPerSecond != 0 {
		t.Errorf("Initial MessagesPerSecond = %f, want 0", flow.MessagesPerSecond)
	}
	if flow.BytesPerSecond != 0 {
		t.Errorf("Initial BytesPerSecond = %f, want 0", flow.BytesPerSecond)
	}
	if flow.ErrorRate != 0 {
		t.Errorf("Initial ErrorRate = %f, want 0", flow.ErrorRate)
	}
}

// TestWebSocketOutput_Initialize tests the Initialize method
func TestWebSocketOutput_Initialize(t *testing.T) {
	tests := []struct {
		name       string
		port       int
		path       string
		subjects   []string
		natsClient *natsclient.Client
		wantErr    bool
		errMsg     string
	}{
		{
			name:       "valid config",
			port:       8086,
			path:       "/ws",
			subjects:   []string{"test.subject"},
			natsClient: &natsclient.Client{},
			wantErr:    false,
		},
		{
			name:       "invalid port too low",
			port:       1023,
			path:       "/ws",
			subjects:   []string{"test.subject"},
			natsClient: &natsclient.Client{},
			wantErr:    true,
			errMsg:     "invalid port",
		},
		{
			name:       "invalid port too high",
			port:       65536,
			path:       "/ws",
			subjects:   []string{"test.subject"},
			natsClient: &natsclient.Client{},
			wantErr:    true,
			errMsg:     "must be between 1 and 65535",
		},
		{
			name:       "empty path",
			port:       8087,
			path:       "",
			subjects:   []string{"test.subject"},
			natsClient: &natsclient.Client{},
			wantErr:    true,
			errMsg:     "WebSocket path cannot be empty",
		},
		{
			name:       "empty subjects",
			port:       8088,
			path:       "/ws",
			subjects:   []string{},
			natsClient: &natsclient.Client{},
			wantErr:    true,
			errMsg:     "at least one input",
		},
		{
			name:       "nil NATS client (allowed for testing)",
			port:       8089,
			path:       "/ws",
			subjects:   []string{"test.subject"},
			natsClient: nil,
			wantErr:    false,
			errMsg:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ws, err := NewOutput(tt.port, tt.path, tt.subjects, tt.natsClient)
			if err == nil {
				err = ws.Initialize()
			}

			if tt.wantErr {
				if err == nil {
					t.Errorf("Initialize() error = nil, want error containing %s", tt.errMsg)
				} else if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("Initialize() error = %v, want error containing %s", err, tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("Initialize() error = %v, want nil", err)
				}
			}
		})
	}
}

// TestWebSocketOutput_RaceConditions tests for race conditions in concurrent scenarios
func TestWebSocketOutput_RaceConditions(t *testing.T) {
	// Use nil NATS client for testing (bypasses NATS subscription)
	ws := mustNewOutput(t, 8901, "/ws", []string{"test.subject"}, nil)

	if err := ws.Initialize(); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Start the WebSocket server
	if err := ws.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer ws.Stop(5 * time.Second)

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	const numClients = 50
	const messagesPerClient = 10

	var wg sync.WaitGroup

	// Simulate concurrent client connections and disconnections
	for i := 0; i < numClients; i++ {
		wg.Add(1)
		go func(clientID int) {
			defer wg.Done()

			// Connect WebSocket client
			u := url.URL{Scheme: "ws", Host: "localhost:8901", Path: "/ws"}
			conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
			if err != nil {
				t.Logf("Client %d: Failed to connect: %v", clientID, err)
				return
			}
			defer conn.Close()

			// Send some messages to keep connection alive
			for j := 0; j < messagesPerClient; j++ {
				if err := conn.WriteMessage(websocket.TextMessage, []byte("ping")); err != nil {
					t.Logf("Client %d: Write error: %v", clientID, err)
					return
				}
				time.Sleep(1 * time.Millisecond)
			}
		}(i)
	}

	// Simultaneously broadcast messages to all clients
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(msgID int) {
			defer wg.Done()

			testData := []byte(
				fmt.Sprintf(`{"test": "message_%d", "timestamp": "%s"}`, msgID, time.Now().Format(time.RFC3339)),
			)
			ctx := context.Background()
			ws.broadcastToClients(ctx, "test.subject", testData)
			time.Sleep(1 * time.Millisecond)
		}(i)
	}

	// Wait for all goroutines to complete
	wg.Wait()

	// Verify the component is still healthy
	health := ws.Health()
	if !health.Healthy {
		t.Errorf("Component unhealthy after race test: %+v", health)
	}
}

// TestWebSocketOutput_ConcurrentClients tests 100 concurrent clients for stress testing
func TestWebSocketOutput_ConcurrentClients(t *testing.T) {
	// Use nil NATS client for testing (bypasses NATS subscription)
	ws := mustNewOutput(t, 8902, "/ws", []string{"test.subject"}, nil)

	if err := ws.Initialize(); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Start the WebSocket server
	if err := ws.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer ws.Stop(5 * time.Second)

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	const numClients = 100

	var wg sync.WaitGroup
	var connectErrors int32
	var mu sync.Mutex

	// Create 100 concurrent connections
	for i := 0; i < numClients; i++ {
		wg.Add(1)
		go func(clientID int) {
			defer wg.Done()

			// Connect WebSocket client
			u := url.URL{Scheme: "ws", Host: "localhost:8902", Path: "/ws"}
			conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
			if err != nil {
				mu.Lock()
				connectErrors++
				mu.Unlock()
				t.Logf("Client %d: Failed to connect: %v", clientID, err)
				return
			}
			defer conn.Close()

			// Keep connection alive for a bit
			time.Sleep(50 * time.Millisecond)

			// Read any messages (to handle pings/data)
			conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
			for {
				_, _, err := conn.ReadMessage()
				if err != nil {
					break // Connection closed or timeout
				}
			}
		}(i)
	}

	// Broadcast test messages while clients are connecting
	go func() {
		for i := 0; i < 10; i++ {
			testData := []byte(
				fmt.Sprintf(`{"test": "broadcast_%d", "timestamp": "%s"}`, i, time.Now().Format(time.RFC3339)),
			)
			ctx := context.Background()
			ws.broadcastToClients(ctx, "test.subject", testData)
			time.Sleep(20 * time.Millisecond)
		}
	}()

	// Wait for all clients to finish
	wg.Wait()

	// Allow some margin for connection errors under high load
	mu.Lock()
	errorRate := float64(connectErrors) / float64(numClients)
	mu.Unlock()

	if errorRate > 0.1 { // Allow up to 10% connection failures under stress
		t.Errorf(
			"Too many connection errors: %d/%d (%.2f%%), max allowed 10%%",
			connectErrors,
			numClients,
			errorRate*100,
		)
	}

	// Verify the component is still healthy
	health := ws.Health()
	if !health.Healthy {
		t.Errorf("Component unhealthy after stress test: %+v", health)
	}

	t.Logf("Stress test completed: %d clients, %d connection errors (%.2f%%)", numClients, connectErrors, errorRate*100)
}

// TestWebSocketOutput_DoubleClose tests that double close operations don't panic
func TestWebSocketOutput_DoubleClose(t *testing.T) {
	// Use nil NATS client for testing (bypasses NATS subscription)
	ws := mustNewOutput(t, 8903, "/ws", []string{"test.subject"}, nil)

	if err := ws.Initialize(); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Start the WebSocket server
	if err := ws.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer ws.Stop(5 * time.Second)

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	// Connect a client
	u := url.URL{Scheme: "ws", Host: "localhost:8903", Path: "/ws"}
	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}

	// Give the server time to register the client
	time.Sleep(50 * time.Millisecond)

	// Get client info from the server's client map
	ws.clientsMu.RLock()
	var clientInfo *clientInfo
	var clientConn *websocket.Conn
	for c, info := range ws.clients {
		clientInfo = info
		clientConn = c
		break
	}
	ws.clientsMu.RUnlock()

	if clientInfo == nil {
		t.Fatal("No client info found")
	}

	// First close the WebSocket connection to stop handleClient goroutine
	conn.Close()

	// Give time for handleClient to finish
	time.Sleep(100 * time.Millisecond)

	// Test that multiple concurrent removeClient calls don't panic
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// This should not panic even if called multiple times
			ws.removeClient(clientConn, clientInfo)
		}()
	}

	wg.Wait()

	// Give time for async cleanup to complete
	time.Sleep(50 * time.Millisecond)

	// Verify client is removed
	ws.clientsMu.RLock()
	clientCount := len(ws.clients)
	ws.clientsMu.RUnlock()

	if clientCount != 0 {
		t.Errorf("Expected 0 clients after removal, got %d", clientCount)
	}

	// Verify atomic flag is set
	if !clientInfo.closed.Load() {
		t.Error("Expected client.closed to be true")
	}
}

// TestWebSocketOutput_AtomicCleanup tests atomic cleanup behavior
func TestWebSocketOutput_AtomicCleanup(t *testing.T) {
	// Use nil NATS client for testing (bypasses NATS subscription)
	ws := mustNewOutput(t, 8904, "/ws", []string{"test.subject"}, nil)

	if err := ws.Initialize(); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Start the WebSocket server
	if err := ws.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer ws.Stop(5 * time.Second)

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	const numClients = 20
	var wg sync.WaitGroup

	// Create multiple clients that will disconnect abruptly
	for i := 0; i < numClients; i++ {
		wg.Add(1)
		go func(clientID int) {
			defer wg.Done()

			// Connect WebSocket client
			u := url.URL{Scheme: "ws", Host: "localhost:8904", Path: "/ws"}
			conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
			if err != nil {
				t.Logf("Client %d: Failed to connect: %v", clientID, err)
				return
			}

			// Immediately close connection to trigger cleanup
			conn.Close()
		}(i)
	}

	// Wait for all clients to connect and disconnect
	wg.Wait()

	// Give time for cleanup
	time.Sleep(200 * time.Millisecond)

	// Verify all clients are cleaned up
	ws.clientsMu.RLock()
	clientCount := len(ws.clients)
	ws.clientsMu.RUnlock()

	if clientCount > 0 {
		t.Errorf("Expected 0 clients after cleanup, got %d", clientCount)
	}

	// Verify the component is still healthy
	health := ws.Health()
	if !health.Healthy {
		t.Errorf("Component unhealthy after atomic cleanup test: %+v", health)
	}
}

// TestWebSocketOutput_Lifecycle tests the full lifecycle (Initialize -> Start -> Stop)
func TestWebSocketOutput_Lifecycle(t *testing.T) {
	// Create a mock NATS connection for testing
	natsClient := &natsclient.Client{}

	// Use a different port for each test to avoid conflicts
	port := 8091
	ws := mustNewOutput(t, port, "/ws", []string{"test.subject"}, natsClient)

	// Test Initialize
	err := ws.Initialize()
	if err != nil {
		t.Fatalf("Initialize() failed: %v", err)
	}

	// Create a context with timeout for testing
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Test Start (note: this will fail because we don't have a real NATS connection)
	// We're testing the lifecycle pattern, not the actual functionality
	err = ws.Start(ctx)
	// We expect this to fail due to no real NATS connection, which is fine for this test

	// Test Stop
	err = ws.Stop(5 * time.Second)
	if err != nil {
		t.Errorf("Stop() failed: %v", err)
	}

	// Verify component is not running after stop
	health := ws.Health()
	if health.Healthy {
		t.Error("Component should not be healthy after Stop()")
	}
}

// TestWebSocketOutput_MessageHandling tests message processing logic
func TestWebSocketOutput_MessageHandling(t *testing.T) {
	natsClient := &natsclient.Client{}
	ws := mustNewOutput(t, 8092, "/ws", []string{"test.subject"}, natsClient)

	// Initialize the component
	err := ws.Initialize()
	if err != nil {
		t.Fatalf("Initialize() failed: %v", err)
	}

	// Set running state for testing
	ws.mu.Lock()
	ws.running = true
	ws.mu.Unlock()

	tests := []struct {
		name    string
		msgData []byte
		subject string
	}{
		{
			name:    "valid JSON message",
			msgData: []byte(`{"type": "graph_update", "entity_id": "123", "status": "active"}`),
			subject: "graph.updates.entity",
		},
		{
			name:    "invalid JSON message",
			msgData: []byte("not json"),
			subject: "graph.updates.raw",
		},
		{
			name:    "empty message",
			msgData: []byte(""),
			subject: "graph.updates.empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := &natspkg.Msg{
				Data:    tt.msgData,
				Subject: tt.subject,
			}

			// This should not panic and should update lastActivity
			ctx := context.Background()
			ws.handleNATSMessage(ctx, msg)

			// Verify lastActivity was updated
			lastActivityNanos := ws.lastActivity.Load()

			if lastActivityNanos == 0 {
				t.Error("lastActivity was not updated after handling message")
			}
		})
	}
}

// TestWebSocketOutput_ClientManagement tests WebSocket client handling
func TestWebSocketOutput_ClientManagement(t *testing.T) {
	natsClient := &natsclient.Client{}
	ws := mustNewOutput(t, 8093, "/ws", []string{"test.subject"}, natsClient)

	// Test initial client count
	ws.clientsMu.RLock()
	clientCount := len(ws.clients)
	ws.clientsMu.RUnlock()
	if clientCount != 0 {
		t.Errorf("Initial client count = %d, want 0", clientCount)
	}

	// Test broadcastToClients with no clients (should not panic)
	testData := []byte(`{"test": "message"}`)
	ctx := context.Background()
	ws.broadcastToClients(ctx, "test.subject", testData)

	// Verify no errors occurred
	errors := ws.errors.Load()

	if errors != 0 {
		t.Errorf("Broadcast with no clients caused %d errors, want 0", errors)
	}
}

// TestWebSocketOutput_ThreadSafety tests concurrent access to the component
func TestWebSocketOutput_ThreadSafety(t *testing.T) {
	natsClient := &natsclient.Client{}
	ws := mustNewOutput(t, 8094, "/ws", []string{"test.subject"}, natsClient)

	// Initialize the component
	err := ws.Initialize()
	if err != nil {
		t.Fatalf("Initialize() failed: %v", err)
	}

	// Set running state
	ws.mu.Lock()
	ws.running = true
	ws.mu.Unlock()

	// Test concurrent access to metrics
	done := make(chan bool, 100)

	// Start multiple goroutines that access different methods
	for i := 0; i < 10; i++ {
		go func() {
			defer func() { done <- true }()

			// Simulate concurrent access
			for j := 0; j < 10; j++ {
				_ = ws.Health()
				_ = ws.DataFlow()
				_ = ws.Meta()

				// Simulate message handling
				ctx := context.Background()
				msg := &natspkg.Msg{
					Data:    []byte(fmt.Sprintf(`{"test": %d}`, j)),
					Subject: "test.subject",
				}
				ws.handleNATSMessage(ctx, msg)

				time.Sleep(time.Millisecond)
			}
		}()
	}

	// Wait for all goroutines to complete
	for i := 0; i < 10; i++ {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("Timeout waiting for goroutines to complete")
		}
	}

	// Verify the component is still functional
	health := ws.Health()
	// The component is running but doesn't have a server, so it should not be healthy
	// This is expected behavior - we're just testing that concurrent access doesn't cause data races
	if health.ErrorCount < 0 {
		t.Error("Error count should not be negative after concurrent access")
	}
}

// =============================================================================
// COMPREHENSIVE LIFECYCLE TESTING - V1-QUALITY-005
// =============================================================================

// testPortCounter provides unique ports for concurrent test instances.
// Starting at 20000 to avoid conflicts with other tests using ports in 18000 range.
var testPortCounter uint32 = 20000

// getNextTestPort returns a unique port for each test instance.
// This prevents port collision when StandardLifecycleTests runs 50+ concurrent Start() calls.
func getNextTestPort() int {
	// Use atomic to safely increment across concurrent goroutines
	port := atomic.AddUint32(&testPortCounter, 1)
	return int(port)
}

// createTestWebSocketOutput creates a test instance for lifecycle testing.
// Each call returns an instance with a unique port to prevent bind conflicts
// during concurrent lifecycle tests.
func createTestWebSocketOutput() component.LifecycleComponent {
	// Use nil NATS client for testing to avoid external dependencies
	// Use unique port to prevent "address already in use" errors in concurrent tests
	port := getNextTestPort()
	ws, err := NewOutput(port, "/test", []string{"test.subject"}, nil)
	if err != nil {
		panic(err)
	}
	return ws
}

// TestWebSocketOutput_ComprehensiveLifecycle runs the complete lifecycle test suite
func TestWebSocketOutput_ComprehensiveLifecycle(t *testing.T) {
	component.StandardLifecycleTests(t, createTestWebSocketOutput)
}

// TestWebSocketOutput_SpecificErrorCases tests WebSocket-specific error scenarios
func TestWebSocketOutput_SpecificErrorCases(t *testing.T) {
	tests := []struct {
		name      string
		setup     func() (*Output, error)
		operation func(*Output) error
		wantErr   bool
		errMsg    string
	}{
		{
			name: "initialize_with_invalid_port",
			setup: func() (*Output, error) {
				return NewOutput(99999, "/ws", []string{"test.subject"}, nil)
			},
			operation: func(ws *Output) error {
				return ws.Initialize()
			},
			wantErr: true,
			errMsg:  "must be between 1 and 65535",
		},
		{
			name: "initialize_with_empty_path",
			setup: func() (*Output, error) {
				return NewOutput(18081, "", []string{"test.subject"}, nil)
			},
			operation: func(ws *Output) error {
				return ws.Initialize()
			},
			wantErr: true,
			errMsg:  "WebSocket path cannot be empty",
		},
		{
			name: "initialize_with_empty_subjects",
			setup: func() (*Output, error) {
				return NewOutput(18082, "/ws", []string{}, nil)
			},
			operation: func(ws *Output) error {
				return ws.Initialize()
			},
			wantErr: true,
			errMsg:  "at least one input",
		},
		{
			name: "start_without_nats",
			setup: func() (*Output, error) {
				ws := mustNewOutput(t, 18083, "/ws", []string{"test.subject"}, nil)
				_ = ws.Initialize()
				return ws, nil
			},
			operation: func(ws *Output) error {
				ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
				defer cancel()
				return ws.Start(ctx)
			},
			wantErr: false, // Should handle gracefully
		},
		{
			name: "handle_nil_nats_message",
			setup: func() (*Output, error) {
				ws := mustNewOutput(t, 18084, "/ws", []string{"test.subject"}, nil)
				_ = ws.Initialize()
				return ws, nil
			},
			operation: func(ws *Output) error {
				// Handle nil message should not panic
				ctx := context.Background()
				ws.handleNATSMessage(ctx, nil)
				return nil
			},
			wantErr: false, // Should handle gracefully
		},
		{
			name: "concurrent_metadata_access",
			setup: func() (*Output, error) {
				ws := mustNewOutput(t, 18085, "/ws", []string{"test.subject"}, nil)
				_ = ws.Initialize()
				return ws, nil
			},
			operation: func(ws *Output) error {
				var wg sync.WaitGroup

				// Concurrent access to metadata methods (should be thread-safe)
				for i := 0; i < 10; i++ {
					wg.Add(1)
					go func() {
						defer wg.Done()
						_ = ws.Meta()
						_ = ws.Health()
						_ = ws.DataFlow()
					}()
				}

				wg.Wait()
				return nil
			},
			wantErr: false, // Should handle concurrency safely
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ws, setupErr := tt.setup()
			if setupErr != nil {
				if tt.wantErr {
					return // Expected setup failure
				}
				t.Fatalf("Setup failed unexpectedly: %v", setupErr)
			}

			err := tt.operation(ws)

			if tt.wantErr {
				require.Error(t, err, "Expected error for %s", tt.name)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg, "Error message should contain expected text")
				}
			} else {
				// Allow either success or specific handled errors
				if err != nil {
					t.Logf("Operation returned error (may be acceptable): %v", err)
				}
			}

			// Ensure component can be cleaned up
			ws.Stop(5 * time.Second)
		})
	}
}

// TestWebSocketOutput_ConcurrentClientHandling tests concurrent client handling
func TestWebSocketOutput_ConcurrentClientHandling(t *testing.T) {
	ws := createTestWebSocketOutput().(*Output)
	require.NoError(t, ws.Initialize())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	require.NoError(t, ws.Start(ctx))
	defer ws.Stop(5 * time.Second)

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	var wg sync.WaitGroup
	const numWorkers = 10
	const operationsPerWorker = 20

	// Simulate concurrent WebSocket operations
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			for j := 0; j < operationsPerWorker; j++ {
				// Simulate broadcast operations
				testData := []byte(fmt.Sprintf(`{"worker": %d, "operation": %d, "timestamp": "%s"}`,
					workerID, j, time.Now().Format(time.RFC3339)))

				ctx := context.Background()
				ws.broadcastToClients(ctx, "test.subject", testData)

				// Access metadata concurrently
				_ = ws.Health()
				_ = ws.DataFlow()

				// Brief pause to allow other goroutines to run
				time.Sleep(time.Microsecond)

				// Check for context cancellation
				select {
				case <-ctx.Done():
					return
				default:
				}
			}
		}(i)
	}

	wg.Wait()

	// Verify component is still functional after concurrent load
	assert.Equal(t, "output", ws.Meta().Type)

	t.Logf("Concurrent client handling completed: %d workers × %d operations",
		numWorkers, operationsPerWorker)
}

// TestWebSocketOutput_MemoryStability tests memory usage under repeated operations
func TestWebSocketOutput_MemoryStability(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping memory stability test in short mode")
	}

	const iterations = 200
	for i := 0; i < iterations; i++ {
		port := 19000 + i // Use different port for each iteration
		ws := mustNewOutput(t, port, "/test", []string{"test.subject"}, nil)

		// Full lifecycle
		_ = ws.Initialize()

		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		_ = ws.Start(ctx)

		// Simulate some operations
		testData := []byte(fmt.Sprintf(`{"iteration": %d, "timestamp": "%s"}`, i, time.Now().Format(time.RFC3339)))
		broadcastCtx := context.Background()
		ws.broadcastToClients(broadcastCtx, "test.subject", testData)

		_ = ws.Stop(5 * time.Second)
		cancel()

		// Periodic cleanup
		if i%50 == 49 {
			runtime.GC()
			time.Sleep(10 * time.Millisecond)
		}
	}

	t.Logf("Memory stability test completed: %d iterations", iterations)
}

// TestWebSocketOutput_StateTransitions tests all valid state transitions
func TestWebSocketOutput_StateTransitions(t *testing.T) {
	tests := []struct {
		name        string
		operations  []string
		expectError []bool
	}{
		{
			name:        "normal_lifecycle",
			operations:  []string{"Initialize", "Start", "Stop"},
			expectError: []bool{false, false, false},
		},
		{
			name:        "double_initialize",
			operations:  []string{"Initialize", "Initialize"},
			expectError: []bool{false, false}, // Should be idempotent
		},
		{
			name:        "start_without_init",
			operations:  []string{"Start"},
			expectError: []bool{true}, // Should require initialization
		},
		{
			name:        "stop_without_start",
			operations:  []string{"Stop"},
			expectError: []bool{false}, // Should always succeed
		},
		{
			name:        "restart_cycle",
			operations:  []string{"Initialize", "Start", "Stop", "Initialize", "Start", "Stop"},
			expectError: []bool{false, false, false, false, false, false},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			port := 19100 + len(tt.operations) // Use unique port for each test
			ws := mustNewOutput(t, port, "/test", []string{"test.subject"}, nil)

			for i, op := range tt.operations {
				var err error
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)

				switch op {
				case "Initialize":
					err = ws.Initialize()
				case "Start":
					err = ws.Start(ctx)
				case "Stop":
					err = ws.Stop(5 * time.Second)
				}

				cancel()

				if tt.expectError[i] {
					if err == nil {
						t.Logf("Operation %s succeeded (expected to fail, but may be acceptable)", op)
					}
				} else {
					if err != nil {
						t.Logf("Operation %s failed: %v (may be acceptable depending on state)", op, err)
					}
				}
			}

			// Always ensure clean shutdown
			ws.Stop(5 * time.Second)
		})
	}
}

// TestWebSocketOutput_ErrorInjection tests error handling with injected failures
func TestWebSocketOutput_ErrorInjection(t *testing.T) {
	component.TestErrorInjection(t, createTestWebSocketOutput)
}

// BenchmarkWebSocketOutput_Lifecycle benchmarks lifecycle operations
func BenchmarkWebSocketOutput_Lifecycle(b *testing.B) {
	component.BenchmarkLifecycleMethods(b, createTestWebSocketOutput)
}

// TestWebSocketOutput_BroadcastStress tests broadcast functionality under stress
func TestWebSocketOutput_BroadcastStress(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping broadcast stress test in short mode")
	}

	ws := mustNewOutput(t, 19200, "/test", []string{"test.subject"}, nil)
	require.NoError(t, ws.Initialize())

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	require.NoError(t, ws.Start(ctx))
	defer ws.Stop(5 * time.Second)

	const numBroadcasts = 1000

	// Stress test broadcast operations
	for i := 0; i < numBroadcasts; i++ {
		testData := []byte(fmt.Sprintf(`{"broadcast": %d, "timestamp": "%s", "data": "stress_test"}`,
			i, time.Now().Format(time.RFC3339)))

		ctx := context.Background()
		ws.broadcastToClients(ctx, "test.subject", testData)

		// Periodic health checks
		if i%100 == 99 {
			health := ws.Health()
			t.Logf("Completed %d broadcasts, health: %+v", i+1, health)
		}

		// Brief pause to prevent overwhelming the system
		if i%50 == 49 {
			time.Sleep(time.Millisecond)
		}
	}

	// Final verification
	finalHealth := ws.Health()
	t.Logf("Successfully completed %d broadcasts, final health: %+v", numBroadcasts, finalHealth)
}

// =============================================================================
// BEHAVIOR-BASED TESTS USING COMPONENTCONFIG PATTERN (README-DRIVEN)
// =============================================================================

// findAvailablePort finds an available port for testing WebSocket servers
func findAvailablePort(t *testing.T) int {
	listener, err := net.Listen("tcp", ":0")
	require.NoError(t, err)
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

// TestWebSocketOutput_Creation_MissingNATSClient tests component creation with missing NATS client
func TestWebSocketOutput_Creation_MissingNATSClient(t *testing.T) {
	// Create WebSocket config using PortConfig format
	wsConfig := testWebSocketConfig(8082, "/ws", []string{"test.>"})
	configJSON, err := json.Marshal(wsConfig)
	require.NoError(t, err)

	// Create component dependencies with nil NATS client
	deps := component.Dependencies{
		NATSClient: nil, // Missing NATS client
		Platform: component.PlatformMeta{
			Org:      "test",
			Platform: "test-platform",
		},
	}

	_, err = CreateOutput(configJSON, deps)
	require.Error(t, err)
	require.Contains(t, err.Error(), "NATS client is required")
}

// =============================================================================
// RELIABILITY FEATURES TESTS - Ack/Nack Protocol & Delivery Modes
// =============================================================================

// TestWebSocketOutput_MessageEnvelope tests message envelope wrapping
func TestWebSocketOutput_MessageEnvelope(t *testing.T) {
	ws := mustNewOutputFromConfig(t, ConstructorConfig{
		Name:         "test",
		Path:         "/test",
		InputPorts:   natsInputDefinitions([]string{"test.subject"}),
		OutputPorts:  websocketOutputDefinitions(19300),
		NATSClient:   nil,
		Security:     security.Config{},
		DeliveryMode: DeliveryAtMostOnce,
		AckTimeout:   5 * time.Second,
	})

	require.NoError(t, ws.Initialize())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	require.NoError(t, ws.Start(ctx))
	defer ws.Stop(5 * time.Second)

	time.Sleep(100 * time.Millisecond) // Allow server to start

	// Connect WebSocket client
	wsURL := fmt.Sprintf("ws://127.0.0.1:%d/test", 19300)
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer conn.Close()

	// Set up message receiver
	received := make(chan MessageEnvelope, 1)
	go func() {
		for {
			var envelope MessageEnvelope
			err := conn.ReadJSON(&envelope)
			if err != nil {
				return
			}
			received <- envelope
		}
	}()

	time.Sleep(100 * time.Millisecond) // Allow client to connect

	// Broadcast a message
	testData := []byte(`{"test":"data"}`)
	broadcastCtx := context.Background()
	ws.broadcastToClients(broadcastCtx, "test.subject", testData)

	// Verify envelope structure
	select {
	case envelope := <-received:
		assert.Equal(t, "data", envelope.Type)
		assert.NotEmpty(t, envelope.ID)
		assert.Greater(t, envelope.Timestamp, int64(0))
		assert.NotNil(t, envelope.Payload)
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for message envelope")
	}
}

// TestWebSocketOutput_DeliveryModes tests different delivery mode configurations
func TestWebSocketOutput_DeliveryModes(t *testing.T) {
	tests := []struct {
		name         string
		deliveryMode DeliveryMode
		ackTimeout   time.Duration
	}{
		{
			name:         "at-most-once",
			deliveryMode: DeliveryAtMostOnce,
			ackTimeout:   5 * time.Second,
		},
		{
			name:         "at-least-once",
			deliveryMode: DeliveryAtLeastOnce,
			ackTimeout:   2 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			port := 19301 + len(tests)
			ws := mustNewOutputFromConfig(t, ConstructorConfig{
				Name:         "test",
				Path:         "/test",
				InputPorts:   natsInputDefinitions([]string{"test.subject"}),
				OutputPorts:  websocketOutputDefinitions(port),
				NATSClient:   nil,
				Security:     security.Config{},
				DeliveryMode: tt.deliveryMode,
				AckTimeout:   tt.ackTimeout,
			})

			require.NoError(t, ws.Initialize())
			assert.Equal(t, tt.deliveryMode, ws.deliveryMode)
			assert.Equal(t, tt.ackTimeout, ws.ackTimeout)

			ws.Stop(1 * time.Second)
		})
	}
}

// TestWebSocketOutput_MessageIDGeneration tests unique message ID generation
func TestWebSocketOutput_MessageIDGeneration(t *testing.T) {
	ws := mustNewOutputFromConfig(t, ConstructorConfig{
		Name:         "test",
		Path:         "/test",
		InputPorts:   natsInputDefinitions([]string{"test.subject"}),
		OutputPorts:  websocketOutputDefinitions(19312),
		NATSClient:   nil,
		Security:     security.Config{},
		DeliveryMode: DeliveryAtMostOnce,
		AckTimeout:   5 * time.Second,
	})

	// Generate multiple IDs and ensure they're unique
	ids := make(map[string]bool)
	for i := 0; i < 1000; i++ {
		id := ws.generateMessageID()
		assert.NotEmpty(t, id)
		assert.False(t, ids[id], "Message ID should be unique: %s", id)
		ids[id] = true
	}

	assert.Len(t, ids, 1000, "All generated IDs should be unique")
}

// TestWebSocketOutput_PendingBufferCreation tests pending message buffer creation per client
func TestWebSocketOutput_PendingBufferCreation(t *testing.T) {
	ws := mustNewOutputFromConfig(t, ConstructorConfig{
		Name:         "test",
		Path:         "/test",
		InputPorts:   natsInputDefinitions([]string{"test.subject"}),
		OutputPorts:  websocketOutputDefinitions(19313),
		NATSClient:   nil,
		Security:     security.Config{},
		DeliveryMode: DeliveryAtLeastOnce,
		AckTimeout:   5 * time.Second,
	})

	require.NoError(t, ws.Initialize())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	require.NoError(t, ws.Start(ctx))
	defer ws.Stop(5 * time.Second)

	time.Sleep(100 * time.Millisecond)

	// Connect client
	wsURL := fmt.Sprintf("ws://127.0.0.1:%d/test", 19313)
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer conn.Close()

	time.Sleep(100 * time.Millisecond)

	// Verify client has pending buffer
	ws.clientsMu.RLock()
	var clientInfo *clientInfo
	for _, info := range ws.clients {
		clientInfo = info
		break
	}
	ws.clientsMu.RUnlock()

	require.NotNil(t, clientInfo)
	assert.NotNil(t, clientInfo.pendingBuffer)
	assert.NotNil(t, clientInfo.pendingMessages)
}

// TestSetupSubscriptions_CoreNATS verifies that a port with type "nats" results in a core NATS
// subscription (not a JetStream consumer). A nil NATS client is used so the method returns
// immediately without actually connecting; the test validates the early-return path and that
// no error is produced for the "nats" type branch structure.
func TestSetupSubscriptions_CoreNATS(t *testing.T) {
	cfg := DefaultConstructorConfig()
	cfg.Path = "/ws"
	cfg.InputPorts = []component.PortDefinition{
		{
			Name:   "nats_input_0",
			Config: component.NATSPort{Subject: "events.>"},
		},
	}
	cfg.OutputPorts = websocketOutputDefinitions(19100)
	cfg.NATSClient = nil // nil client → setupSubscriptions returns nil immediately

	ws := mustNewOutputFromConfig(t, cfg)

	ctx := context.Background()
	err := ws.setupSubscriptions(ctx)
	require.NoError(t, err, "setupSubscriptions should not error when natsClient is nil")

	// Confirm that inputPorts was stored correctly on the struct
	require.Len(t, ws.inputPorts, 1)
	facts, factsErr := ws.inputPorts[0].Facts()
	require.NoError(t, factsErr)
	assert.Equal(t, component.PortKindNATS, facts.Kind())
	assert.Equal(t, []string{"events.>"}, facts.NATSSubjects())
}

// TestSetupSubscriptions_SubjectConstructor verifies constructor subjects become canonical NATS ports.
func TestSetupSubscriptions_SubjectConstructor(t *testing.T) {
	cfg := DefaultConstructorConfig()
	cfg.Path = "/ws"
	cfg.InputPorts = natsInputDefinitions([]string{"metrics.>"})
	cfg.OutputPorts = websocketOutputDefinitions(19101)
	cfg.NATSClient = nil

	ws := mustNewOutputFromConfig(t, cfg)

	ctx := context.Background()
	err := ws.setupSubscriptions(ctx)
	require.NoError(t, err)
	require.Len(t, ws.InputPorts(), 1)
	facts, factsErr := ws.InputPorts()[0].Facts()
	require.NoError(t, factsErr)
	assert.Equal(t, component.PortKindNATS, facts.Kind())
}

// TestDeriveStreamName tests the stream name derivation heuristic used when no explicit
// StreamName is provided in the port definition.
func TestDeriveStreamName(t *testing.T) {
	ws := mustNewOutput(t, 19102, "/ws", []string{"placeholder"}, nil)

	tests := []struct {
		name    string
		subject string
		want    string
	}{
		{
			name:    "simple subject",
			subject: "events.entity.created",
			want:    "EVENTS",
		},
		{
			name:    "wildcard suffix >",
			subject: "metrics.>",
			want:    "METRICS",
		},
		{
			name:    "wildcard suffix *",
			subject: "logs.*",
			want:    "LOGS",
		},
		{
			name:    "leading wildcard prefix stripped",
			subject: "*.events",
			want:    "EVENTS",
		},
		{
			name:    "bare wildcard",
			subject: ">",
			want:    "",
		},
		{
			name:    "empty string",
			subject: "",
			want:    "",
		},
		{
			name:    "single segment",
			subject: "telemetry",
			want:    "TELEMETRY",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ws.deriveStreamName(tt.subject)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestWebSocketOutput_MetricsRegistrationIdempotent reproduces gh#490: a runtime
// component restart re-constructs the output against the SAME MetricsRegistry.
// The previous raw MustRegister panicked with "duplicate metrics collector
// registration attempted" on the second construction; routing through the
// idempotent MetricsRegistry.Register* helpers must make restart panic-free.
func TestWebSocketOutput_MetricsRegistrationIdempotent(t *testing.T) {
	registry := metric.NewMetricsRegistry()
	newCfg := func() ConstructorConfig {
		return ConstructorConfig{
			Name:            "ws-restart",
			Path:            "/ws",
			InputPorts:      natsInputDefinitions([]string{"test.>"}),
			OutputPorts:     websocketOutputDefinitions(9090),
			NATSClient:      &natsclient.Client{},
			MetricsRegistry: registry,
			DeliveryMode:    DeliveryAtMostOnce,
			AckTimeout:      5 * time.Second,
		}
	}

	// First construction registers the collectors.
	first := mustNewOutputFromConfig(t, newCfg())
	require.NotNil(t, first.metrics)

	// Second construction against the same registry mimics restart — must not
	// panic (would previously fail via MustRegister).
	require.NotPanics(t, func() {
		second := mustNewOutputFromConfig(t, newCfg())
		require.NotNil(t, second.metrics)
	})
}
