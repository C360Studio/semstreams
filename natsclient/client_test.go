package natsclient

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
)

// Test basic manager creation
func TestNewClient(t *testing.T) {
	manager, err := NewClient("nats://localhost:4222")
	assert.NoError(t, err)

	assert.NotNil(t, manager)
	assert.Equal(t, "nats://localhost:4222", manager.URLs())
	assert.Equal(t, StatusDisconnected, manager.Status())
	assert.False(t, manager.IsHealthy())
}

// Test circuit breaker opens after failures
func TestCircuitBreaker_OpensAfterFailures(t *testing.T) {
	manager, err := NewClient("nats://invalid:4222")
	assert.NoError(t, err)

	// Record 14 failures - should not open
	for i := 0; i < 14; i++ {
		manager.recordFailure()
	}
	assert.NotEqual(t, StatusCircuitOpen, manager.Status())

	// 15th failure should open circuit
	manager.recordFailure()
	assert.Equal(t, StatusCircuitOpen, manager.Status())
	assert.Equal(t, int32(15), manager.Failures())
}

// Test circuit breaker reset
func TestCircuitBreaker_Reset(t *testing.T) {
	manager, err := NewClient("nats://localhost:4222")
	assert.NoError(t, err)

	// Record failures to open circuit (threshold is 15)
	for i := 0; i < 15; i++ {
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

	// Record failures and check backoff increases (threshold is 15)
	for i := 0; i < 15; i++ {
		manager.recordFailure()
	}
	assert.Equal(t, 2*time.Second, manager.Backoff())

	// Another round of failures
	for i := 0; i < 15; i++ {
		manager.recordFailure()
	}
	assert.Equal(t, 4*time.Second, manager.Backoff())

	// Backoff should cap at max (1 minute)
	for i := 0; i < 20; i++ {
		for j := 0; j < 15; j++ {
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
				for i := 0; i < 15; i++ {
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

		// Open circuit (threshold is 15)
		for i := 0; i < 15; i++ {
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
		_, err = client.Subscribe(ctx, "test.subject", func(_ context.Context, _ *nats.Msg) {})
		assert.Equal(t, ErrNotConnected, err)
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

		err = client.ConsumeStream(ctx, "test", "test.*", func(jetstream.Msg) {})
		assert.Equal(t, ErrNotConnected, err)
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
				for i := 0; i < 15; i++ {
					m.recordFailure()
				}
			},
			validate: func(t *testing.T, m *Client) {
				assert.Equal(t, StatusCircuitOpen, m.Status())
				assert.False(t, m.IsHealthy())
				assert.Equal(t, int32(15), m.Failures())
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

// TestConnectionLossTimeout_FiresAfterGrace verifies the watchdog fires
// onConnectionLost once the broker has been continuously absent for grace.
func TestConnectionLossTimeout_FiresAfterGrace(t *testing.T) {
	var fired atomic.Int32
	gotErr := make(chan error, 1)

	manager, err := NewClient("nats://localhost:4222",
		WithConnectionLossTimeout(50*time.Millisecond),
		WithConnectionLostCallback(func(e error) {
			fired.Add(1)
			select {
			case gotErr <- e:
			default:
			}
		}),
	)
	assert.NoError(t, err)

	disconnectErr := errors.New("broker gone")
	manager.handleDisconnect(nil, disconnectErr)

	select {
	case e := <-gotErr:
		assert.Equal(t, disconnectErr, e)
	case <-time.After(time.Second):
		t.Fatal("connection-loss callback did not fire within 1s")
	}
	assert.Equal(t, int32(1), fired.Load())
}

// TestConnectionLossTimeout_CancelledByReconnect verifies that a reconnect
// arriving before grace expires cancels the watchdog.
func TestConnectionLossTimeout_CancelledByReconnect(t *testing.T) {
	var fired atomic.Int32

	manager, err := NewClient("nats://localhost:4222",
		WithConnectionLossTimeout(200*time.Millisecond),
		WithConnectionLostCallback(func(_ error) {
			fired.Add(1)
		}),
	)
	assert.NoError(t, err)

	manager.handleDisconnect(nil, errors.New("blip"))
	time.Sleep(50 * time.Millisecond)
	manager.handleReconnect(nil)

	// Wait past the original grace window — callback must not fire.
	time.Sleep(300 * time.Millisecond)
	assert.Equal(t, int32(0), fired.Load())
}

// TestConnectionLossTimeout_RepeatDisconnectKeepsOriginalDeadline verifies
// that a second disconnect without an intervening reconnect does not reset
// the grace window — total elapsed measures from the *first* loss. We
// record the wallclock at fire time and assert it lands close to the
// original deadline, not the second-disconnect deadline; a buggy reset
// would extend by another ~75ms and the test would catch it.
func TestConnectionLossTimeout_RepeatDisconnectKeepsOriginalDeadline(t *testing.T) {
	const grace = 200 * time.Millisecond
	const midGrace = 100 * time.Millisecond
	const slack = 75 * time.Millisecond // < midGrace so a buggy reset (fire ~midGrace+grace) lies outside grace+slack

	firedAt := make(chan time.Time, 1)
	manager, err := NewClient("nats://localhost:4222",
		WithConnectionLossTimeout(grace),
		WithConnectionLostCallback(func(_ error) {
			select {
			case firedAt <- time.Now():
			default:
			}
		}),
	)
	assert.NoError(t, err)

	start := time.Now()
	manager.handleDisconnect(nil, errors.New("first"))
	time.Sleep(midGrace)
	// Second disconnect mid-grace must NOT extend the deadline.
	manager.handleDisconnect(nil, errors.New("second"))

	select {
	case fireTime := <-firedAt:
		elapsed := fireTime.Sub(start)
		// Deadline is `grace` from the first disconnect. A buggy reset
		// would put fire at ~midGrace + grace = 225ms; correct behavior
		// is ~grace = 150ms. Cap at grace + slack to catch the bug.
		assert.LessOrEqual(t, elapsed, grace+slack,
			"watchdog fired %v after first disconnect — deadline appears to have been extended", elapsed)
		assert.GreaterOrEqual(t, elapsed, grace-slack,
			"watchdog fired too early (%v before grace=%v)", elapsed, grace)
	case <-time.After(500 * time.Millisecond):
		t.Fatal("watchdog did not fire within 500ms")
	}
}

// TestConnectionLossTimeout_DisabledByDefault verifies the legacy behavior:
// without WithConnectionLossTimeout, onConnectionLost never fires
// automatically on disconnect.
func TestConnectionLossTimeout_DisabledByDefault(t *testing.T) {
	var fired atomic.Int32

	manager, err := NewClient("nats://localhost:4222",
		WithConnectionLostCallback(func(_ error) {
			fired.Add(1)
		}),
	)
	assert.NoError(t, err)

	manager.handleDisconnect(nil, errors.New("boom"))
	time.Sleep(100 * time.Millisecond)
	assert.Equal(t, int32(0), fired.Load())
	// Structural check: no timer should have armed at all.
	manager.lossTimerMu.Lock()
	assert.Nil(t, manager.lossTimer)
	manager.lossTimerMu.Unlock()
}

// TestConnectionLossTimeout_NoCallbackNoFire verifies the watchdog stays
// idle when only the timeout is configured but no callback is registered.
func TestConnectionLossTimeout_NoCallbackNoFire(t *testing.T) {
	manager, err := NewClient("nats://localhost:4222",
		WithConnectionLossTimeout(20*time.Millisecond),
	)
	assert.NoError(t, err)

	manager.handleDisconnect(nil, errors.New("boom"))
	time.Sleep(100 * time.Millisecond)
	// Mostly we're confirming this doesn't panic; the timer should not have
	// armed at all since onConnectionLost is nil.
	manager.lossTimerMu.Lock()
	assert.Nil(t, manager.lossTimer)
	manager.lossTimerMu.Unlock()
}

// TestConnectionLossTimeout_CloseCancels verifies that closing the client
// cancels a pending watchdog so the callback never fires after Close.
func TestConnectionLossTimeout_CloseCancels(t *testing.T) {
	var fired atomic.Int32

	manager, err := NewClient("nats://localhost:4222",
		WithConnectionLossTimeout(100*time.Millisecond),
		WithConnectionLostCallback(func(_ error) {
			fired.Add(1)
		}),
	)
	assert.NoError(t, err)

	manager.handleDisconnect(nil, errors.New("gone"))
	// Close before the grace window expires.
	_ = manager.Close(context.Background())

	time.Sleep(200 * time.Millisecond)
	assert.Equal(t, int32(0), fired.Load())
}
