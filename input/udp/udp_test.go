package udp

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/pkg/retry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testUDPConfig creates a standard test configuration for UDP input
func testUDPConfig(port int, bind, subject string) InputConfig {
	return InputConfig{
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{
					Name:        "udp_socket",
					Config:      component.NetworkPort{Protocol: "udp", Host: bind, Port: port},
					Required:    true,
					Description: "UDP socket for incoming data",
				},
			},
			Outputs: []component.PortDefinition{
				{
					Name:        "data_output",
					Config:      component.NATSPort{Subject: subject},
					Required:    true,
					Description: "NATS output for received data",
				},
			},
		},
	}
}

func TestNewUDPInput(t *testing.T) {
	mockClient := &natsclient.Client{} // Mock client for testing

	// Use new Ports configuration pattern
	deps := InputDeps{
		Config:          testUDPConfig(14550, "127.0.0.1", "test.subject"),
		NATSClient:      mockClient,
		MetricsRegistry: nil,
		Logger:          nil,
	}
	udp, err := NewInput(deps)
	require.NoError(t, err)

	// Test that component extracts configuration from Ports
	assert.Equal(t, 14550, udp.port)
	assert.Equal(t, "127.0.0.1", udp.bind)
	assert.Equal(t, "test.subject", udp.subject)
	assert.Equal(t, mockClient, udp.natsClient)
	assert.NotNil(t, udp.buffer, "should have buffer initialized")
}

func TestUDPInput_Meta(t *testing.T) {
	mockClient := &natsclient.Client{}
	// Use new idiomatic constructor pattern
	deps := InputDeps{
		Config:          testUDPConfig(14550, "127.0.0.1", "test.subject"),
		NATSClient:      mockClient,
		MetricsRegistry: nil,
		Logger:          nil,
	}
	udp, err := NewInput(deps)
	require.NoError(t, err)

	meta := udp.Meta()

	assert.Equal(t, "udp-input-14550", meta.Name)
	assert.Equal(t, "input", meta.Type)
	assert.Contains(t, meta.Description, "UDP input listener")
	assert.Equal(t, "1.0.0", meta.Version)
}

func TestUDPInput_Ports(t *testing.T) {
	mockClient := &natsclient.Client{}
	// Use new idiomatic constructor pattern
	deps := InputDeps{
		Config:          testUDPConfig(14550, "127.0.0.1", "test.subject"),
		NATSClient:      mockClient,
		MetricsRegistry: nil,
		Logger:          nil,
	}
	udp, err := NewInput(deps)
	require.NoError(t, err)

	inputPorts := udp.InputPorts()
	assert.Len(t, inputPorts, 1)
	assert.Equal(t, "udp_socket", inputPorts[0].Name)
	assert.Equal(t, component.DirectionInput, inputPorts[0].Direction)
	assert.True(t, inputPorts[0].Required)

	// Check NetworkPort config
	networkConfig, ok := inputPorts[0].Config.(component.NetworkPort)
	assert.True(t, ok, "Input port config should be NetworkPort")
	assert.Equal(t, "udp", networkConfig.Protocol)
	assert.Equal(t, "127.0.0.1", networkConfig.Host)
	assert.Equal(t, 14550, networkConfig.Port)

	outputPorts := udp.OutputPorts()
	assert.Len(t, outputPorts, 1)
	assert.Equal(t, "data_output", outputPorts[0].Name)
	assert.Equal(t, component.DirectionOutput, outputPorts[0].Direction)
	assert.True(t, outputPorts[0].Required)

	// Check NATSPort config
	natsConfig, ok := outputPorts[0].Config.(component.NATSPort)
	assert.True(t, ok, "Output port config should be NATSPort")
	assert.Equal(t, "test.subject", natsConfig.Subject)
}

func TestUDPInput_ConfigSchema(t *testing.T) {
	mockClient := &natsclient.Client{}
	deps := InputDeps{
		Config:          testUDPConfig(14550, "127.0.0.1", "test.subject"),
		NATSClient:      mockClient,
		MetricsRegistry: nil,
		Logger:          nil,
	}
	udp, err := NewInput(deps)
	require.NoError(t, err)

	schema := udp.ConfigSchema()

	// Architecture Decision: Ports in Schema
	assert.Contains(t, schema.Properties, "ports", "Schema should have ports property")
	assert.Equal(t, "ports", schema.Properties["ports"].Type, "Ports should be ports type (first-class)")
	assert.Equal(t, "basic", schema.Properties["ports"].Category, "Ports should be basic category")
	assert.Empty(t, schema.Required, "Ports should not be required (uses defaults)")
}

func TestUDPInput_Initialize(t *testing.T) {
	tests := []struct {
		name          string
		port          int
		subject       string
		natsClient    *natsclient.Client
		expectedError bool
		errorClass    errs.ErrorClass
	}{
		{
			name:          "valid configuration",
			port:          14550,
			subject:       "test.subject",
			natsClient:    &natsclient.Client{},
			expectedError: false,
		},
		{
			name:          "invalid port - negative",
			port:          -1,
			subject:       "test.subject",
			natsClient:    &natsclient.Client{},
			expectedError: true,
			errorClass:    errs.ErrorInvalid,
		},
		{
			name:          "invalid port - too high",
			port:          70000,
			subject:       "test.subject",
			natsClient:    &natsclient.Client{},
			expectedError: true,
			errorClass:    errs.ErrorInvalid,
		},
		{
			name:          "empty subject",
			port:          14550,
			subject:       "",
			natsClient:    &natsclient.Client{},
			expectedError: true,
			errorClass:    errs.ErrorInvalid,
		},
		{
			name:          "nil NATS client",
			port:          14550,
			subject:       "test.subject",
			natsClient:    nil,
			expectedError: true,
			errorClass:    errs.ErrorInvalid,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deps := InputDeps{
				Config: testUDPConfig(tt.port, "127.0.0.1", tt.subject), NATSClient: tt.natsClient,
				MetricsRegistry: nil,
				Logger:          nil,
			}
			udp, err := NewInput(deps)
			if err == nil {
				err = udp.Initialize()
			}

			if tt.expectedError {
				require.Error(t, err)
				assert.Equal(t, tt.errorClass, errs.Classify(err), "error should have correct classification")
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestUDPInput_Health(t *testing.T) {
	mockClient := &natsclient.Client{}
	deps := InputDeps{
		Config:          testUDPConfig(14550, "127.0.0.1", "test.subject"),
		NATSClient:      mockClient,
		MetricsRegistry: nil,
		Logger:          nil,
	}
	udp, err := NewInput(deps)
	require.NoError(t, err)

	health := udp.Health()

	assert.IsType(t, component.HealthStatus{}, health)
	assert.False(t, health.Healthy) // Should be false before starting
	assert.Equal(t, 0, health.ErrorCount)
}

func TestUDPInput_DataFlow(t *testing.T) {
	mockClient := &natsclient.Client{}
	deps := InputDeps{
		Config:          testUDPConfig(14550, "127.0.0.1", "test.subject"),
		NATSClient:      mockClient,
		MetricsRegistry: nil,
		Logger:          nil,
	}
	udp, err := NewInput(deps)
	require.NoError(t, err)

	flow := udp.DataFlow()

	assert.IsType(t, component.FlowMetrics{}, flow)
	assert.Equal(t, float64(0), flow.MessagesPerSecond)
	assert.Equal(t, float64(0), flow.BytesPerSecond)
	assert.Equal(t, float64(0), flow.ErrorRate)
}

func TestUDPInput_StartStop(t *testing.T) {
	// Find an available port for testing
	port := findAvailablePort(t)
	mockClient := &natsclient.Client{}
	deps := InputDeps{
		Config:          testUDPConfig(port, "127.0.0.1", "test.subject"),
		NATSClient:      mockClient,
		MetricsRegistry: nil,
		Logger:          nil,
	}
	udp, err := NewInput(deps)
	require.NoError(t, err)

	// Initialize first
	err = udp.Initialize()
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	// Ensure cleanup happens even on test failure
	t.Cleanup(func() {
		_ = udp.Stop(5 * time.Second)
	})

	// Start should succeed
	err = udp.Start(ctx)
	require.NoError(t, err)

	// Should be running now
	assert.True(t, udp.running.Load())
	assert.NotNil(t, udp.conn)

	// Health should be good
	health := udp.Health()
	assert.True(t, health.Healthy)

	// Stop should succeed
	err = udp.Stop(5 * time.Second)
	require.NoError(t, err)

	// Should be stopped now
	assert.False(t, udp.running.Load())
	assert.Nil(t, udp.conn)
}

func TestUDPInput_RetryOnBindFailure(t *testing.T) {
	// Bind to a port first to force retry logic
	port := findAvailablePort(t)
	conflictConn, err := net.ListenUDP("udp", &net.UDPAddr{Port: port})
	require.NoError(t, err)

	// Ensure conflict connection is closed even on test failure
	t.Cleanup(func() {
		_ = conflictConn.Close()
	})

	mockClient := &natsclient.Client{}
	deps := InputDeps{
		Config:          testUDPConfig(port, "127.0.0.1", "test.subject"),
		NATSClient:      mockClient,
		MetricsRegistry: nil,
		Logger:          nil,
	}
	udp, err := NewInput(deps)
	require.NoError(t, err)

	// Ensure UDP input is cleaned up even on test failure
	t.Cleanup(func() {
		_ = udp.Stop(5 * time.Second)
	})

	err = udp.Initialize()
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	// Should retry and eventually fail due to port conflict
	err = udp.Start(ctx)
	assert.Error(t, err, "should fail due to port conflict")
	assert.True(t, strings.Contains(strings.ToLower(err.Error()), "bind") ||
		strings.Contains(strings.ToLower(err.Error()), "address already in use"))
}

func TestUDPInput_BufferIntegration(t *testing.T) {
	port := findAvailablePort(t)
	mockClient := &natsclient.Client{}
	deps := InputDeps{
		Config:          testUDPConfig(port, "127.0.0.1", "test.subject"),
		NATSClient:      mockClient,
		MetricsRegistry: nil,
		Logger:          nil,
	}
	udp, err := NewInput(deps)
	require.NoError(t, err)

	// Check buffer is initialized
	assert.NotNil(t, udp.buffer)
	assert.False(t, udp.buffer.IsFull())
	assert.True(t, udp.buffer.IsEmpty())
	assert.Greater(t, udp.buffer.Capacity(), 0)

	// Test buffer write functionality
	testData := []byte("test message")
	err = udp.buffer.Write(testData)
	assert.NoError(t, err)
	assert.Equal(t, 1, udp.buffer.Size())

	// Test buffer read functionality
	data, ok := udp.buffer.Read()
	assert.True(t, ok)
	assert.Equal(t, testData, data)
	assert.Equal(t, 0, udp.buffer.Size())
}

func TestUDPInput_Creation_MissingNATS(t *testing.T) {
	// Create UDP config
	udpConfig := testUDPConfig(14550, "127.0.0.1", "test.udp")
	configJSON, err := json.Marshal(udpConfig)
	require.NoError(t, err)

	// Create component dependencies with nil NATS client
	deps := component.Dependencies{
		NATSClient: nil, // Missing NATS client
		Platform: component.PlatformMeta{
			Org:      "test",
			Platform: "test-platform",
		},
	}

	_, err = CreateInput(configJSON, deps)
	require.Error(t, err)
	require.True(t, errs.IsInvalid(err), "Missing NATS client should be classified as invalid")
	require.Contains(t, err.Error(), "NATS client")
}

func TestUDPInput_Interfaces(t *testing.T) {
	mockClient := &natsclient.Client{}
	deps := InputDeps{
		Config:          testUDPConfig(14550, "127.0.0.1", "test.subject"),
		NATSClient:      mockClient,
		MetricsRegistry: nil,
		Logger:          nil,
	}
	udp, err := NewInput(deps)
	require.NoError(t, err)

	// Verify interface implementations
	var _ component.Discoverable = udp
	var _ component.LifecycleComponent = udp
}

// Helper function to check if a port is available
func isPortAvailable(port int) bool {
	addr, err := net.ResolveUDPAddr("udp", fmt.Sprintf(":%d", port))
	if err != nil {
		return false
	}

	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return false
	}

	_ = conn.Close()
	return true
}

// Helper function to send test UDP data
func sendTestUDPData(t *testing.T, port int, data []byte) {
	conn, err := net.Dial("udp", fmt.Sprintf("127.0.0.1:%d", port))
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	_, err = conn.Write(data)
	require.NoError(t, err)
}

// Helper function to find an available port
func findAvailablePort(t *testing.T) int {
	addr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	require.NoError(t, err)

	conn, err := net.ListenUDP("udp", addr)
	require.NoError(t, err)
	defer conn.Close()

	return conn.LocalAddr().(*net.UDPAddr).Port
}

// P0-CRITICAL TESTS FOR RACE CONDITIONS, GOROUTINE LEAKS, AND PANIC PREVENTION

// TestUDPInput_NoRaceCondition tests that metrics can be accessed concurrently without race conditions
func TestUDPInput_NoRaceCondition(t *testing.T) {
	mockClient := &natsclient.Client{}
	port := findAvailablePort(t)
	deps := InputDeps{
		Config:          testUDPConfig(port, "127.0.0.1", "test.subject"),
		NATSClient:      mockClient,
		MetricsRegistry: nil,
		Logger:          nil,
	}
	input, err := NewInput(deps)
	require.NoError(t, err)

	// Test concurrent access to metrics
	var wg sync.WaitGroup
	const numGoroutines = 100
	const opsPerGoroutine = 100

	// Start input to enable metrics updates
	require.NoError(t, input.Initialize())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, input.Start(ctx))
	defer input.Stop(5 * time.Second)

	// Concurrent metric updates and reads
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < opsPerGoroutine; j++ {
				// Simulate metric updates (using atomic operations)
				input.messagesReceived.Add(1)
				input.bytesReceived.Add(64)
				input.errors.Add(0)
				input.lastActivity.Store(time.Now())

				// Concurrent reads (like in Health() and DataFlow())
				_ = input.Health()
				_ = input.DataFlow()
			}
		}()
	}

	wg.Wait()

	// Verify metrics are reasonable (no corruption)
	messages := input.messagesReceived.Load()
	bytes := input.bytesReceived.Load()
	errCount := input.errors.Load()

	expectedMessages := int64(numGoroutines * opsPerGoroutine)
	expectedBytes := int64(numGoroutines * opsPerGoroutine * 64)

	assert.Equal(t, expectedMessages, messages, "messages counter should be exact")
	assert.Equal(t, expectedBytes, bytes, "bytes counter should be exact")
	assert.GreaterOrEqual(t, errCount, int64(0), "errors counter should not be negative")
}

// TestUDPInput_NoGoroutineLeak tests that stopping UDP input doesn't leak goroutines
func TestUDPInput_NoGoroutineLeak(t *testing.T) {
	before := runtime.NumGoroutine()

	// Create and start/stop multiple UDP inputs
	const numIterations = 5 // Reduce iterations to speed up test
	for i := 0; i < numIterations; i++ {
		mockClient := &natsclient.Client{}
		port := findAvailablePort(t)
		deps := InputDeps{
			Config:          testUDPConfig(port, "127.0.0.1", "test.subject"),
			NATSClient:      mockClient,
			MetricsRegistry: nil,
			Logger:          nil,
		}
		input, err := NewInput(deps)
		require.NoError(t, err)

		require.NoError(t, input.Initialize())

		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		require.NoError(t, input.Start(ctx))

		// Proper cleanup with t.Cleanup to ensure resources are cleaned even on test failure
		t.Cleanup(func() {
			_ = input.Stop(5 * time.Second)
			cancel()
		})

		// Send some data to exercise the read loop
		go func(testPort int) {
			time.Sleep(5 * time.Millisecond)
			sendTestUDPData(t, testPort, []byte("test data"))
		}(port)

		// Let it run briefly
		time.Sleep(20 * time.Millisecond)

		// Stop should clean up goroutines
		require.NoError(t, input.Stop(5*time.Second))
		cancel()
	}

	// Wait for goroutines to clean up
	time.Sleep(50 * time.Millisecond)

	after := runtime.NumGoroutine()

	// Allow for some variance in test environment goroutines
	assert.LessOrEqual(t, after, before+2,
		"goroutine leak detected: before=%d, after=%d, diff=%d",
		before, after, after-before)
}

// TestUDPInput_NoPanic tests that UDP input handles various error conditions without panicking
func TestUDPInput_NoPanic(t *testing.T) {
	mockClient := &natsclient.Client{}
	port := findAvailablePort(t)

	// Test normal operation doesn't panic
	assert.NotPanics(t, func() {
		deps := InputDeps{
			Config:          testUDPConfig(port, "127.0.0.1", "test.subject"),
			NATSClient:      mockClient,
			MetricsRegistry: nil,
			Logger:          nil,
		}
		input, err := NewInput(deps)
		require.NoError(t, err)
		require.NoError(t, input.Initialize())

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		require.NoError(t, input.Start(ctx))
		time.Sleep(10 * time.Millisecond)
		require.NoError(t, input.Stop(5*time.Second))
	}, "normal UDP input operation should not panic")

	// Test force-close connection doesn't panic
	assert.NotPanics(t, func() {
		deps := InputDeps{
			Config:          testUDPConfig(port, "127.0.0.1", "test.subject"),
			NATSClient:      mockClient,
			MetricsRegistry: nil,
			Logger:          nil,
		}
		input, err := NewInput(deps)
		require.NoError(t, err)
		require.NoError(t, input.Initialize())

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		require.NoError(t, input.Start(ctx))

		// Force close the connection while reading
		if input.conn != nil {
			input.conn.Close()
		}

		time.Sleep(20 * time.Millisecond)
		require.NoError(t, input.Stop(5*time.Second))
	}, "force connection close should not panic")

	// Test context cancellation doesn't panic
	assert.NotPanics(t, func() {
		deps := InputDeps{
			Config:          testUDPConfig(port, "127.0.0.1", "test.subject"),
			NATSClient:      mockClient,
			MetricsRegistry: nil,
			Logger:          nil,
		}
		input, err := NewInput(deps)
		require.NoError(t, err)
		require.NoError(t, input.Initialize())

		ctx, cancel := context.WithCancel(context.Background())
		require.NoError(t, input.Start(ctx))

		// Cancel context immediately
		cancel()
		time.Sleep(10 * time.Millisecond)
		require.NoError(t, input.Stop(5*time.Second))
	}, "context cancellation should not panic")
}

// TestUDPInput_CleanShutdown tests that Stop() completes within timeout
func TestUDPInput_CleanShutdown(t *testing.T) {
	mockClient := &natsclient.Client{}
	port := findAvailablePort(t)
	deps := InputDeps{
		Config:          testUDPConfig(port, "127.0.0.1", "test.subject"),
		NATSClient:      mockClient,
		MetricsRegistry: nil,
		Logger:          nil,
	}
	input, err := NewInput(deps)
	require.NoError(t, err)

	require.NoError(t, input.Initialize())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, input.Start(ctx))

	// Ensure cleanup even on test failure
	t.Cleanup(func() {
		_ = input.Stop(5 * time.Second)
	})

	// Send some data to ensure read loop is active
	go func() {
		for i := 0; i < 3; i++ {
			sendTestUDPData(t, port, []byte(fmt.Sprintf("test data %d", i)))
			time.Sleep(5 * time.Millisecond)
		}
	}()

	time.Sleep(30 * time.Millisecond)

	// Stop should complete within reasonable time (much less than 5 second timeout)
	start := time.Now()
	err = input.Stop(5 * time.Second)
	duration := time.Since(start)

	require.NoError(t, err, "Stop should not return error")
	assert.Less(t, duration, 1*time.Second, "Stop should complete quickly")

	// Verify clean state
	assert.False(t, input.running.Load(), "should not be running after stop")
	assert.Nil(t, input.conn, "connection should be nil after stop")
}

// TestUDPInput_StopTimeout tests the 5-second timeout in Stop()
func TestUDPInput_StopTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping timeout test in short mode")
	}

	mockClient := &natsclient.Client{}
	port := findAvailablePort(t)
	deps := InputDeps{
		Config:          testUDPConfig(port, "127.0.0.1", "test.subject"),
		NATSClient:      mockClient,
		MetricsRegistry: nil,
		Logger:          nil,
	}
	input, err := NewInput(deps)
	require.NoError(t, err)

	require.NoError(t, input.Initialize())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, input.Start(ctx))

	// Let the real goroutine exit first by cancelling context and waiting briefly
	cancel()
	time.Sleep(200 * time.Millisecond) // Give time for goroutine to notice cancellation and exit

	// Now simulate a stuck goroutine by replacing the done channel
	// This simulates the case where a goroutine exists but doesn't signal completion
	input.mu.Lock()
	// Replace with new done channel that will never be closed - simulates stuck goroutine
	input.done = make(chan struct{}) // New channel that will never be closed
	// Need to set running back to true so Stop() doesn't early return
	input.running.Store(true)
	input.mu.Unlock()

	start := time.Now()
	err = input.Stop(5 * time.Second)
	duration := time.Since(start)

	// Should timeout and return error
	if err == nil {
		t.Fatal("Stop should return timeout error but got nil")
	}
	assert.Error(t, err, "Stop should return timeout error")
	// Error should be properly classified
	assert.True(t, errs.IsTransient(err), "timeout errors should be transient")
	assert.GreaterOrEqual(t, duration, 4500*time.Millisecond, "should wait at least ~5 seconds")
	assert.Less(t, duration, 6*time.Second, "should not wait much longer than 5 seconds")
}

// TestUDPInput_MetricsThreadSafety tests that metrics operations are thread-safe
func TestUDPInput_MetricsThreadSafety(t *testing.T) {
	mockClient := &natsclient.Client{}
	port := findAvailablePort(t)
	deps := InputDeps{
		Config:          testUDPConfig(port, "127.0.0.1", "test.subject"),
		NATSClient:      mockClient,
		MetricsRegistry: nil,
		Logger:          nil,
	}
	input, err := NewInput(deps)
	require.NoError(t, err)

	// Test atomic operations work correctly
	const numGoroutines = 50
	const incrementsPerGoroutine = 1000

	var wg sync.WaitGroup

	// Concurrent increments
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < incrementsPerGoroutine; j++ {
				input.messagesReceived.Add(1)
				input.bytesReceived.Add(10)
				input.errors.Add(1)
			}
		}()
	}

	// Concurrent reads
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				messages := input.messagesReceived.Load()
				bytes := input.bytesReceived.Load()
				errors := input.errors.Load()

				// Verify consistency
				assert.GreaterOrEqual(t, messages, int64(0))
				assert.GreaterOrEqual(t, bytes, int64(0))
				assert.GreaterOrEqual(t, errors, int64(0))

				time.Sleep(time.Microsecond)
			}
		}()
	}

	wg.Wait()

	// Verify final counts
	expectedMessages := int64(numGoroutines * incrementsPerGoroutine)
	expectedBytes := int64(numGoroutines * incrementsPerGoroutine * 10)
	expectedErrors := int64(numGoroutines * incrementsPerGoroutine)

	assert.Equal(t, expectedMessages, input.messagesReceived.Load())
	assert.Equal(t, expectedBytes, input.bytesReceived.Load())
	assert.Equal(t, expectedErrors, input.errors.Load())
}

// TestUDPInput_ErrorHandling tests proper error handling using pkg/errors
func TestUDPInput_ErrorHandling(t *testing.T) {
	mockClient := &natsclient.Client{}

	// Test invalid port - should return properly classified error
	deps := InputDeps{
		Config:          testUDPConfig(-1, "127.0.0.1", "test.subject"),
		NATSClient:      mockClient,
		MetricsRegistry: nil,
		Logger:          nil,
	}
	input, err := NewInput(deps)
	if err == nil {
		err = input.Initialize()
	}
	require.Error(t, err, "should error on invalid port")
	assert.True(t, errs.IsInvalid(err), "invalid port should be classified as invalid")

	// Test empty subject - should return properly classified error
	deps = InputDeps{
		Config:          testUDPConfig(14550, "127.0.0.1", ""),
		NATSClient:      mockClient,
		MetricsRegistry: nil,
		Logger:          nil,
	}
	input, err = NewInput(deps)
	if err == nil {
		err = input.Initialize()
	}
	require.Error(t, err, "should error on empty subject")
	assert.True(t, errs.IsInvalid(err), "empty subject should be classified as invalid")

	// Test nil NATS client - should return properly classified error
	deps = InputDeps{
		Config:          testUDPConfig(14550, "127.0.0.1", "test.subject"),
		NATSClient:      nil,
		MetricsRegistry: nil,
		Logger:          nil,
	}
	input, err = NewInput(deps)
	require.NoError(t, err)
	err = input.Initialize()
	require.Error(t, err, "should error on nil NATS client")
	assert.True(t, errs.IsInvalid(err), "nil NATS client should be classified as invalid")

	// Test already running
	port := findAvailablePort(t)
	deps = InputDeps{
		Config:          testUDPConfig(port, "127.0.0.1", "test.subject"),
		NATSClient:      mockClient,
		MetricsRegistry: nil,
		Logger:          nil,
	}
	input, err = NewInput(deps)
	require.NoError(t, err)
	require.NoError(t, input.Initialize())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	require.NoError(t, input.Start(ctx))
	defer input.Stop(5 * time.Second)

	// Starting again should not error (idempotent)
	err = input.Start(ctx)
	assert.NoError(t, err, "starting already running input should be idempotent")

	// Stopping not running should not error
	deps2 := InputDeps{
		Config:          testUDPConfig(port+1, "127.0.0.1", "test.subject"),
		NATSClient:      mockClient,
		MetricsRegistry: nil,
		Logger:          nil,
	}
	input2, err := NewInput(deps2)
	require.NoError(t, err)
	err = input2.Stop(5 * time.Second)
	assert.NoError(t, err, "stopping not running input should not error")
}

// TestUDPInput_BufferOverflow tests buffer overflow handling
func TestUDPInput_BufferOverflow(t *testing.T) {
	mockClient := &natsclient.Client{}
	deps := InputDeps{
		Config:          testUDPConfig(14550, "127.0.0.1", "test.subject"),
		NATSClient:      mockClient,
		MetricsRegistry: nil,
		Logger:          nil,
	}
	input, err := NewInput(deps)
	require.NoError(t, err)

	// Fill buffer to capacity
	capacity := input.buffer.Capacity()
	testData := []byte("test message")

	// Fill buffer
	for i := 0; i < capacity; i++ {
		err := input.buffer.Write(testData)
		assert.NoError(t, err, "should be able to write to buffer until full")
	}

	assert.True(t, input.buffer.IsFull(), "buffer should be full")

	// Additional writes should handle overflow according to policy
	err = input.buffer.Write(testData)
	// Behavior depends on overflow policy - could drop oldest/newest or block
	// The test verifies the operation doesn't panic and buffer handles it gracefully
	_ = err // May or may not error depending on policy
}

// TestUDPInput_RetryIntegration tests retry integration for transient errors
func TestUDPInput_RetryIntegration(t *testing.T) {
	mockClient := &natsclient.Client{}
	deps := InputDeps{
		Config:          testUDPConfig(14550, "127.0.0.1", "test.subject"),
		NATSClient:      mockClient,
		MetricsRegistry: nil,
		Logger:          nil,
	}
	input, err := NewInput(deps)
	require.NoError(t, err)

	// Test that retry configuration is properly initialized
	assert.NotNil(t, input.retryConfig, "retry config should be initialized")
	assert.Greater(t, input.retryConfig.MaxAttempts, 0, "should have retry attempts configured")
	assert.Greater(t, input.retryConfig.InitialDelay, time.Duration(0), "should have retry delay configured")

	// Test retry logic with a transient error scenario
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	testOperation := func() error {
		// Simulate a transient error
		return errs.WrapTransient(fmt.Errorf("network timeout"), "udp-input", "test", "simulated timeout")
	}

	err = retry.Do(ctx, input.retryConfig, testOperation)
	assert.Error(t, err, "should fail after retries")
	assert.True(t, errs.IsTransient(err) || strings.Contains(err.Error(), "failed after"),
		"should be transient error or retry exhausted message")
}
