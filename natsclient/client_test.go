package natsclient

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// Test basic manager creation
func TestNewClient(t *testing.T) {
	manager, err := NewClient("nats://localhost:4222")
	assert.NoError(t, err)

	assert.NotNil(t, manager)
	assert.Equal(t, "nats://localhost:4222", manager.URL())
	assert.Equal(t, StatusDisconnected, manager.Status())
	assert.False(t, manager.IsHealthy())
}

// Test circuit breaker opens after failures
func TestCircuitBreaker_OpensAfterFailures(t *testing.T) {
	manager, err := NewClient("nats://invalid:4222")
	assert.NoError(t, err)

	// Record 4 failures - should not open
	for i := 0; i < 4; i++ {
		manager.recordFailure()
	}
	assert.NotEqual(t, StatusCircuitOpen, manager.Status())

	// 5th failure should open circuit
	manager.recordFailure()
	assert.Equal(t, StatusCircuitOpen, manager.Status())
	assert.Equal(t, int32(5), manager.Failures())
}

// Test circuit breaker reset
func TestCircuitBreaker_Reset(t *testing.T) {
	manager, err := NewClient("nats://localhost:4222")
	assert.NoError(t, err)

	// Record failures to open circuit
	for i := 0; i < 5; i++ {
		manager.recordFailure()
	}
	assert.Equal(t, StatusCircuitOpen, manager.Status())

	// Reset circuit
	manager.resetCircuit()
	assert.Equal(t, int32(0), manager.Failures())
	assert.NotEqual(t, StatusCircuitOpen, manager.Status())
}

// Test exponential backoff
func TestCircuitBreaker_ExponentialBackoff(t *testing.T) {
	manager, err := NewClient("nats://localhost:4222")
	assert.NoError(t, err)

	// Initial backoff should be 1 second
	assert.Equal(t, time.Second, manager.Backoff())

	// Record failures and check backoff increases
	for i := 0; i < 5; i++ {
		manager.recordFailure()
	}
	assert.Equal(t, 2*time.Second, manager.Backoff())

	// Another round of failures
	for i := 0; i < 5; i++ {
		manager.recordFailure()
	}
	assert.Equal(t, 4*time.Second, manager.Backoff())

	// Backoff should cap at max (1 minute)
	for i := 0; i < 20; i++ {
		for j := 0; j < 5; j++ {
			manager.recordFailure()
		}
	}
	assert.LessOrEqual(t, manager.Backoff(), time.Minute)
}

// Test status transitions
func TestStatus_Transitions(t *testing.T) {
	tests := []struct {
		name           string
		initialStatus  ConnectionStatus
		action         func(*Client)
		expectedStatus ConnectionStatus
	}{
		{
			name:          "disconnected to connecting",
			initialStatus: StatusDisconnected,
			action: func(m *Client) {
				m.setStatus(StatusConnecting)
			},
			expectedStatus: StatusConnecting,
		},
		{
			name:          "connecting to connected",
			initialStatus: StatusConnecting,
			action: func(m *Client) {
				m.setStatus(StatusConnected)
			},
			expectedStatus: StatusConnected,
		},
		{
			name:          "connected to reconnecting",
			initialStatus: StatusConnected,
			action: func(m *Client) {
				m.setStatus(StatusReconnecting)
			},
			expectedStatus: StatusReconnecting,
		},
		{
			name:          "any to circuit open",
			initialStatus: StatusConnected,
			action: func(m *Client) {
				for i := 0; i < 5; i++ {
					m.recordFailure()
				}
			},
			expectedStatus: StatusCircuitOpen,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager, err := NewClient("nats://localhost:4222")
			assert.NoError(t, err)
			manager.setStatus(tt.initialStatus)

			tt.action(manager)

			assert.Equal(t, tt.expectedStatus, manager.Status())
		})
	}
}

// Test concurrent safety
func TestConcurrentSafety(t *testing.T) {
	manager, err := NewClient("nats://localhost:4222")
	assert.NoError(t, err)

	var wg sync.WaitGroup
	iterations := 100

	// Concurrent status updates
	wg.Add(3)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			manager.setStatus(StatusConnecting)
		}
	}()

	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			manager.setStatus(StatusConnected)
		}
	}()

	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			_ = manager.Status()
		}
	}()

	// Concurrent failure recording
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			manager.recordFailure()
		}
	}()

	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			manager.resetCircuit()
		}
	}()

	wg.Wait()

	// Should not panic and should have valid state
	status := manager.Status()
	assert.Contains(t, []ConnectionStatus{
		StatusDisconnected,
		StatusConnecting,
		StatusConnected,
		StatusReconnecting,
		StatusCircuitOpen,
	}, status)
}

// Test IsHealthy logic
func TestIsHealthy(t *testing.T) {
	tests := []struct {
		name     string
		status   ConnectionStatus
		expected bool
	}{
		{"connected is healthy", StatusConnected, true},
		{"disconnected is not healthy", StatusDisconnected, false},
		{"connecting is not healthy", StatusConnecting, false},
		{"reconnecting is not healthy", StatusReconnecting, false},
		{"circuit open is not healthy", StatusCircuitOpen, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager, err := NewClient("nats://localhost:4222")
			assert.NoError(t, err)
			manager.setStatus(tt.status)
			assert.Equal(t, tt.expected, manager.IsHealthy())
		})
	}
}

// Test WaitForConnection with timeout
func TestWaitForConnection(t *testing.T) {
	t.Run("times out when not connected", func(t *testing.T) {
		manager, err := NewClient("nats://localhost:4222")
		assert.NoError(t, err)

		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		err = manager.WaitForConnection(ctx)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "timeout")
	})

	t.Run("returns immediately when connected", func(t *testing.T) {
		manager, err := NewClient("nats://localhost:4222")
		assert.NoError(t, err)
		manager.setStatus(StatusConnected)

		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()

		start := time.Now()
		err = manager.WaitForConnection(ctx)
		elapsed := time.Since(start)

		assert.NoError(t, err)
		assert.Less(t, elapsed, 100*time.Millisecond)
	})

	t.Run("returns when becomes connected", func(t *testing.T) {
		manager, err := NewClient("nats://localhost:4222")
		assert.NoError(t, err)

		// Simulate connection after delay
		go func() {
			time.Sleep(50 * time.Millisecond)
			manager.setStatus(StatusConnected)
		}()

		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()

		err = manager.WaitForConnection(ctx)
		assert.NoError(t, err)
		assert.Equal(t, StatusConnected, manager.Status())
	})
}

// Test new KeyValue bucket operations
func TestKeyValueBuckets(t *testing.T) {
	t.Run("operations return error when not connected", func(t *testing.T) {
		client, err := NewClient("nats://localhost:4222")
		assert.NoError(t, err)
		ctx := context.Background()

		// Test all operations return ErrNotConnected
		cfg := jetstream.KeyValueConfig{Bucket: "test"}
		_, err = client.CreateKeyValueBucket(ctx, cfg)
		assert.Equal(t, ErrNotConnected, err)

		_, err = client.GetKeyValueBucket(ctx, "test")
		assert.Equal(t, ErrNotConnected, err)

		err = client.DeleteKeyValueBucket(ctx, "test")
		assert.Equal(t, ErrNotConnected, err)

		_, err = client.ListKeyValueBuckets(ctx)
		assert.Equal(t, ErrNotConnected, err)
	})

	t.Run("operations return error when circuit open", func(t *testing.T) {
		client, err := NewClient("nats://localhost:4222")
		assert.NoError(t, err)

		// Open circuit
		for i := 0; i < 5; i++ {
			client.recordFailure()
		}
		assert.Equal(t, StatusCircuitOpen, client.Status())

		ctx := context.Background()
		cfg := jetstream.KeyValueConfig{Bucket: "test"}

		_, err = client.CreateKeyValueBucket(ctx, cfg)
		assert.Equal(t, ErrCircuitOpen, err)

		_, err = client.GetKeyValueBucket(ctx, "test")
		assert.Equal(t, ErrCircuitOpen, err)

		err = client.DeleteKeyValueBucket(ctx, "test")
		assert.Equal(t, ErrCircuitOpen, err)

		_, err = client.ListKeyValueBuckets(ctx)
		assert.Equal(t, ErrCircuitOpen, err)
	})

	t.Run("operations work with real KV server", func(t *testing.T) {
		if testing.Short() {
			t.Skip("Skipping integration test in short mode")
		}

		ctx := context.Background()
		natsContainer, natsURL := startTestNATSContainerWithJS(ctx, t)
		defer natsContainer.Terminate(ctx)

		// Create and connect client
		client, err := NewClient(natsURL,
			WithMaxReconnects(0), // No reconnects in tests
		)
		require.NoError(t, err)

		err = client.Connect(ctx)
		require.NoError(t, err)
		defer client.Close(ctx)

		// Test KV bucket operations
		cfg := jetstream.KeyValueConfig{Bucket: "unit_test_bucket"}

		// Create bucket
		kv, err := client.CreateKeyValueBucket(ctx, cfg)
		require.NoError(t, err)
		require.NotNil(t, kv)

		// Test put/get operations
		_, err = kv.Put(ctx, "test-key", []byte("test-value"))
		require.NoError(t, err)

		entry, err := kv.Get(ctx, "test-key")
		require.NoError(t, err)
		assert.Equal(t, []byte("test-value"), entry.Value())

		// Get bucket by name
		retrievedKV, err := client.GetKeyValueBucket(ctx, "unit_test_bucket")
		require.NoError(t, err)
		require.NotNil(t, retrievedKV)

		// Verify we can still access data
		entry2, err := retrievedKV.Get(ctx, "test-key")
		require.NoError(t, err)
		assert.Equal(t, []byte("test-value"), entry2.Value())

		// List buckets
		buckets, err := client.ListKeyValueBuckets(ctx)
		require.NoError(t, err)

		// Should have at least our bucket
		found := false
		for _, bucketName := range buckets {
			if bucketName == "unit_test_bucket" {
				found = true
				break
			}
		}
		assert.True(t, found, "Should find our unit_test_bucket in list")

		// Delete bucket
		err = client.DeleteKeyValueBucket(ctx, "unit_test_bucket")
		require.NoError(t, err)

		// Verify bucket is gone
		_, err = client.GetKeyValueBucket(ctx, "unit_test_bucket")
		assert.Error(t, err) // Should fail to get deleted bucket
	})
}

// Test context-aware methods
func TestContextAwareMethods(t *testing.T) {
	t.Run("with invalid host", func(t *testing.T) {
		client, err := NewClient("nats://invalid-host:4222")
		assert.NoError(t, err)

		// Test Connect with context
		ctx := context.Background()

		// These will fail because no actual NATS server, but we can test the API
		err = client.Connect(ctx)
		assert.Error(t, err) // Should fail due to no server

		// Test Close with context
		err = client.Close(ctx)
		assert.NoError(t, err) // Should succeed even when not connected

		// Test Publish with context (will fail due to not connected)
		err = client.Publish(ctx, "test.subject", []byte("data"))
		assert.Equal(t, ErrNotConnected, err)

		// Test Subscribe with context (will fail due to not connected)
		err = client.Subscribe(ctx, "test.subject", func(_ context.Context, _ []byte) {})
		assert.Equal(t, ErrNotConnected, err)
	})

	t.Run("with real NATS server", func(t *testing.T) {
		if testing.Short() {
			t.Skip("Skipping integration test in short mode")
		}

		ctx := context.Background()
		natsContainer, natsURL := startTestNATSContainer(ctx, t)
		defer natsContainer.Terminate(ctx)

		// Create and connect client
		client, err := NewClient(natsURL,
			WithMaxReconnects(0), // No reconnects in tests
		)
		require.NoError(t, err)

		err = client.Connect(ctx)
		require.NoError(t, err)
		defer client.Close(ctx)

		// Test successful operations with real server
		assert.True(t, client.IsHealthy())

		// Test Publish with context (should succeed)
		err = client.Publish(ctx, "test.subject", []byte("data"))
		assert.NoError(t, err)

		// Test Subscribe with context (should succeed)
		received := make(chan []byte, 1)
		err = client.Subscribe(ctx, "test.reply", func(_ context.Context, data []byte) {
			received <- data
		})
		assert.NoError(t, err)

		// Test round-trip message
		err = client.Publish(ctx, "test.reply", []byte("response"))
		assert.NoError(t, err)

		// Verify message received
		select {
		case data := <-received:
			assert.Equal(t, []byte("response"), data)
		case <-time.After(1 * time.Second):
			t.Fatal("Message not received")
		}
	})
}

// Test JetStream methods with context
func TestJetStreamMethods(t *testing.T) {
	t.Run("when not connected", func(t *testing.T) {
		client, err := NewClient("nats://localhost:4222")
		assert.NoError(t, err)
		ctx := context.Background()

		// All should return ErrNotConnected when not connected
		cfg := jetstream.StreamConfig{Name: "test", Subjects: []string{"test.*"}}
		_, err = client.CreateStream(ctx, cfg)
		assert.Equal(t, ErrNotConnected, err)

		_, err = client.GetStream(ctx, "test")
		assert.Equal(t, ErrNotConnected, err)

		err = client.PublishToStream(ctx, "test.subject", []byte("data"))
		assert.Equal(t, ErrNotConnected, err)

		err = client.ConsumeStream(ctx, "test", "test.*", func([]byte) {})
		assert.Equal(t, ErrNotConnected, err)
	})

	t.Run("with real JetStream server", func(t *testing.T) {
		if testing.Short() {
			t.Skip("Skipping integration test in short mode")
		}

		ctx := context.Background()
		natsContainer, natsURL := startTestNATSContainerWithJS(ctx, t)
		defer natsContainer.Terminate(ctx)

		// Create and connect client
		client, err := NewClient(natsURL,
			WithMaxReconnects(0), // No reconnects in tests
		)
		require.NoError(t, err)

		err = client.Connect(ctx)
		require.NoError(t, err)
		defer client.Close(ctx)

		// Test JetStream functionality
		js, err := client.JetStream()
		require.NoError(t, err)
		require.NotNil(t, js)

		// Create a stream
		cfg := jetstream.StreamConfig{Name: "UNIT_TEST", Subjects: []string{"unit.test.*"}}
		stream, err := client.CreateStream(ctx, cfg)
		require.NoError(t, err)
		require.NotNil(t, stream)

		// Get the stream back
		retrievedStream, err := client.GetStream(ctx, "UNIT_TEST")
		require.NoError(t, err)
		assert.Equal(t, "UNIT_TEST", retrievedStream.CachedInfo().Config.Name)

		// Test publish to stream
		err = client.PublishToStream(ctx, "unit.test.data", []byte("test message"))
		require.NoError(t, err)

		// Test consume from stream
		received := make(chan []byte, 1)
		err = client.ConsumeStream(ctx, "UNIT_TEST", "unit.test.*", func(data []byte) {
			received <- data
		})
		require.NoError(t, err)

		// Verify message received
		select {
		case data := <-received:
			assert.Equal(t, []byte("test message"), data)
		case <-time.After(2 * time.Second):
			t.Fatal("Stream message not received")
		}
	})
}

// Test connection options
func TestConnectionOptions(t *testing.T) {
	manager, err := NewClient("nats://localhost:4222",
		WithMaxReconnects(10),
		WithReconnectWait(5*time.Second),
		WithPingInterval(30*time.Second),
	)
	assert.NoError(t, err)

	// Should have default options
	opts := manager.ConnectionOptions()
	assert.NotNil(t, opts)

	// Verify options were set
	assert.Equal(t, 10, manager.MaxReconnects())
	assert.Equal(t, 5*time.Second, manager.ReconnectWait())
	assert.Equal(t, 30*time.Second, manager.PingInterval())
}

// Test metrics collection
func TestMetrics(t *testing.T) {
	manager, err := NewClient("nats://localhost:4222")
	assert.NoError(t, err)

	// Record some failures
	for i := 0; i < 3; i++ {
		manager.recordFailure()
	}

	// Check status
	status := manager.GetStatus()
	assert.NotNil(t, status)
	assert.Equal(t, int32(3), status.FailureCount)
	assert.Equal(t, StatusDisconnected, status.Status)
	assert.NotZero(t, status.LastFailureTime)

	// Reset and check
	manager.resetCircuit()
	status = manager.GetStatus()
	assert.Equal(t, int32(0), status.FailureCount)
}

// Helper function for testing - mock NATS connection
type mockNATSConn struct {
	connected atomic.Bool
	rtt       time.Duration
}

func (m *mockNATSConn) IsConnected() bool {
	return m.connected.Load()
}

func (m *mockNATSConn) RTT() (time.Duration, error) {
	if !m.IsConnected() {
		return 0, ErrNotConnected
	}
	return m.rtt, nil
}

func (m *mockNATSConn) Close() {
	m.connected.Store(false)
}

// Table-driven tests for various scenarios
func TestManagerScenarios(t *testing.T) {
	scenarios := []struct {
		name     string
		setup    func(*Client)
		action   func(*Client)
		validate func(*testing.T, *Client)
	}{
		{
			name: "successful connection flow",
			setup: func(m *Client) {
				m.setStatus(StatusDisconnected)
			},
			action: func(m *Client) {
				m.setStatus(StatusConnecting)
				m.setStatus(StatusConnected)
				m.resetCircuit()
			},
			validate: func(t *testing.T, m *Client) {
				assert.Equal(t, StatusConnected, m.Status())
				assert.True(t, m.IsHealthy())
				assert.Equal(t, int32(0), m.Failures())
			},
		},
		{
			name: "connection failure and circuit break",
			setup: func(m *Client) {
				m.setStatus(StatusConnecting)
			},
			action: func(m *Client) {
				for i := 0; i < 5; i++ {
					m.recordFailure()
				}
			},
			validate: func(t *testing.T, m *Client) {
				assert.Equal(t, StatusCircuitOpen, m.Status())
				assert.False(t, m.IsHealthy())
				assert.Equal(t, int32(5), m.Failures())
			},
		},
		{
			name: "reconnection after disconnect",
			setup: func(m *Client) {
				m.setStatus(StatusConnected)
			},
			action: func(m *Client) {
				m.setStatus(StatusReconnecting)
				time.Sleep(10 * time.Millisecond)
				m.setStatus(StatusConnected)
				m.resetCircuit()
			},
			validate: func(t *testing.T, m *Client) {
				assert.Equal(t, StatusConnected, m.Status())
				assert.True(t, m.IsHealthy())
			},
		},
	}

	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			manager, err := NewClient("nats://localhost:4222")
			assert.NoError(t, err)

			scenario.setup(manager)
			scenario.action(manager)
			scenario.validate(t, manager)
		})
	}
}

// Test graceful KV bucket already exists handling
func TestCreateKeyValueBucket_AlreadyExists(t *testing.T) {
	t.Run("isAlreadyExistsError recognizes error patterns", func(t *testing.T) {
		// Test error patterns that should be recognized as "already exists"
		testCases := []struct {
			name     string
			err      error
			expected bool
		}{
			{"bucket name already in use", errors.New("nats: bucket name already in use"), true},
			{"already exists", errors.New("bucket already exists"), true},
			{"stream name already in use", errors.New("nats: stream name already in use"), true},
			{"other error", errors.New("connection failed"), false},
			{"nil error", nil, false},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				result := isAlreadyExistsError(tc.err)
				assert.Equal(t, tc.expected, result)
			})
		}
	})
}

// Helper function to start NATS container for unit tests
func startTestNATSContainer(ctx context.Context, t *testing.T) (testcontainers.Container, string) {
	t.Helper()

	req := testcontainers.ContainerRequest{
		Image:        "nats:2.11.7-alpine",
		ExposedPorts: []string{"4222/tcp"},
		WaitingFor:   wait.ForListeningPort("4222/tcp"),
	}

	natsContainer, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	require.NoError(t, err)

	host, err := natsContainer.Host(ctx)
	require.NoError(t, err)

	port, err := natsContainer.MappedPort(ctx, "4222")
	require.NoError(t, err)

	natsURL := fmt.Sprintf("nats://%s:%s", host, port.Port())
	return natsContainer, natsURL
}

// Helper function to start NATS container with JetStream for unit tests
func startTestNATSContainerWithJS(ctx context.Context, t *testing.T) (testcontainers.Container, string) {
	t.Helper()

	req := testcontainers.ContainerRequest{
		Image:        "nats:2.11.7-alpine",
		ExposedPorts: []string{"4222/tcp"},
		WaitingFor:   wait.ForListeningPort("4222/tcp"),
		Cmd:          []string{"--js"}, // Enable JetStream
	}

	natsContainer, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	require.NoError(t, err)

	host, err := natsContainer.Host(ctx)
	require.NoError(t, err)

	port, err := natsContainer.MappedPort(ctx, "4222")
	require.NoError(t, err)

	natsURL := fmt.Sprintf("nats://%s:%s", host, port.Port())
	return natsContainer, natsURL
}
