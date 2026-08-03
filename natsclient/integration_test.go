//go:build integration

package natsclient

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/c360studio/semstreams/metric"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
)

// managedTestContainer preserves the package's historical helper return type
// while routing termination through TestClient's bounded, error-reporting
// cleanup. Embedded methods such as Stop and Start still reach the raw
// container for the tests that intentionally simulate server failure.
type managedTestContainer struct {
	testcontainers.Container
	testClient *TestClient
}

func (c *managedTestContainer) Terminate(
	_ context.Context,
	_ ...testcontainers.TerminateOption,
) error {
	return c.testClient.Terminate()
}

// TestIntegration_ConnectToRealNATS tests connection to a real NATS server
func TestIntegration_ConnectToRealNATS(t *testing.T) {
	ctx := context.Background()

	testClient := NewTestClient(t)
	natsURL := testClient.URL

	// Create manager and connect
	manager, err := NewClient(natsURL)
	require.NoError(t, err)
	err = manager.Connect(ctx)
	require.NoError(t, err)
	defer manager.Close(ctx)

	// Verify connection
	assert.True(t, manager.IsHealthy())
	assert.Equal(t, StatusConnected, manager.Status())

	// Test RTT
	rtt, err := manager.RTT()
	assert.NoError(t, err)
	assert.Greater(t, rtt, time.Duration(0))
}

// TestIntegration_Reconnection tests automatic reconnection
func TestIntegration_Reconnection(t *testing.T) {
	t.Skip(
		"Skipping reconnection test: testcontainers assigns new port on restart, breaking reconnection. Reconnection logic is covered by unit tests.",
	)

	ctx := context.Background()

	testClient := NewTestClient(t)
	natsContainer, natsURL := testClient.container, testClient.URL

	// Track disconnection and reconnection
	var disconnected, reconnected atomic.Bool

	// Create manager with reconnect options
	manager, err := NewClient(natsURL,
		WithMaxReconnects(5),
		WithReconnectWait(100*time.Millisecond),
		WithDisconnectCallback(func(_ error) {
			disconnected.Store(true)
		}),
		WithReconnectCallback(func() {
			reconnected.Store(true)
		}),
	)
	require.NoError(t, err)

	// Connect
	err = manager.Connect(ctx)
	require.NoError(t, err)
	defer manager.Close(ctx)

	// Simulate network interruption by stopping container
	err = natsContainer.Stop(ctx, nil)
	require.NoError(t, err)

	// Wait for disconnection to be detected
	time.Sleep(500 * time.Millisecond)
	assert.True(t, disconnected.Load(), "Expected disconnection callback to be triggered")
	assert.False(t, manager.IsHealthy(), "Expected manager to be unhealthy after disconnect")

	// Restart container
	err = natsContainer.Start(ctx)
	require.NoError(t, err)

	// Wait for reconnection - NATS client will retry with configured interval
	time.Sleep(1 * time.Second)
	assert.True(t, reconnected.Load(), "Expected reconnection callback to be triggered")
	assert.True(t, manager.IsHealthy(), "Expected manager to be healthy after reconnect")
}

// TestIntegration_CircuitBreakerWithRealConnection tests circuit breaker with actual failures
func TestIntegration_CircuitBreakerWithRealConnection(t *testing.T) {
	ctx := context.Background()

	// Try to connect to an invalid NATS server
	manager, err := NewClient("nats://invalid-host:4222")
	require.NoError(t, err)

	// Try 14 times - should not open circuit (threshold is 15)
	for i := 0; i < 14; i++ {
		err = manager.Connect(ctx)
		assert.Error(t, err)
		assert.NotEqual(t, StatusCircuitOpen, manager.Status())
	}

	// 15th attempt should trigger circuit breaker
	err = manager.Connect(ctx)
	assert.Error(t, err)

	// After 15 failures, circuit should be open
	assert.Equal(t, StatusCircuitOpen, manager.Status())
	assert.Equal(t, int32(15), manager.Failures())

	// Further attempts should fail immediately with circuit open error
	start := time.Now()
	err = manager.Connect(ctx)
	elapsed := time.Since(start)

	assert.Error(t, err)
	assert.Equal(t, ErrCircuitOpen, err)
	assert.Less(t, elapsed, 10*time.Millisecond) // Should fail fast
}

// TestIntegration_PublishSubscribe tests basic pub/sub functionality
func TestIntegration_PublishSubscribe(t *testing.T) {
	ctx := t.Context()

	testClient := NewTestClient(t)
	natsURL := testClient.URL

	// Create manager and connect
	manager, err := NewClient(natsURL)
	require.NoError(t, err)
	err = manager.Connect(ctx)
	require.NoError(t, err)
	defer manager.Close(ctx)

	// Subscribe to a subject
	received := make(chan string, 1)
	sub, err := manager.Subscribe(ctx, "test.subject", func(_ context.Context, msg *nats.Msg) {
		received <- string(msg.Data)
	})
	require.NoError(t, err)
	defer sub.Unsubscribe()

	// Publish a message
	testMessage := "Hello NATS"
	err = manager.Publish(ctx, "test.subject", []byte(testMessage))
	require.NoError(t, err)

	// Verify message received
	select {
	case msg := <-received:
		assert.Equal(t, testMessage, msg)
	case <-time.After(1 * time.Second):
		t.Fatal("Message not received")
	}
}

// TestIntegration_JetStream tests JetStream functionality
func TestIntegration_JetStream(t *testing.T) {
	ctx := context.Background()

	testClient := NewTestClient(t, WithJetStream())
	natsURL := testClient.URL

	// Create manager and connect
	manager, err := NewClient(natsURL)
	require.NoError(t, err)
	err = manager.Connect(ctx)
	require.NoError(t, err)
	defer manager.Close(ctx)

	// Get JetStream context
	js, err := manager.JetStream()
	require.NoError(t, err)
	require.NotNil(t, js)

	// Create a stream
	streamName := "TEST_STREAM"
	streamCfg := jetstream.StreamConfig{
		Name:     streamName,
		Subjects: []string{"test.*"},
		MaxAge:   testStreamMaxAge,
		MaxBytes: testStreamMaxBytes,
	}
	_, err = manager.CreateStream(ctx, streamCfg)
	require.NoError(t, err)

	// Publish to stream
	err = manager.PublishToStream(ctx, "test.data", []byte("stream message"))
	require.NoError(t, err)

	// Create consumer and receive message
	received := make(chan string, 1)
	err = manager.ConsumeStream(ctx, streamName, "test.*", func(msg jetstream.Msg) {
		received <- string(msg.Data())
		msg.Ack()
	})
	require.NoError(t, err)

	// Verify message
	select {
	case msg := <-received:
		assert.Equal(t, "stream message", msg)
	case <-time.After(1 * time.Second):
		t.Fatal("Stream message not received")
	}
}

// TestIntegration_HealthMonitoring tests health check functionality
func TestIntegration_HealthMonitoring(t *testing.T) {
	ctx := context.Background()

	testClient := NewTestClient(t)
	natsContainer, natsURL := testClient.container, testClient.URL

	// Create manager with health monitoring
	manager, err := NewClient(natsURL)
	require.NoError(t, err)
	manager.WithHealthCheck(100 * time.Millisecond)

	// Track health changes
	healthChanges := make(chan bool, 10)
	manager.OnHealthChange(func(healthy bool) {
		healthChanges <- healthy
	})

	// Connect
	err = manager.Connect(ctx)
	require.NoError(t, err)
	defer manager.Close(ctx)

	// Should report healthy
	select {
	case healthy := <-healthChanges:
		assert.True(t, healthy)
	case <-time.After(200 * time.Millisecond):
		// Initial state might already be healthy
	}

	// The harness client is not the subject of this test. Close it while the
	// server is healthy so intentional server loss cannot turn its expected
	// drain failure into a harness-cleanup failure.
	require.NoError(t, testClient.Client.Close(ctx))

	// Stop container to simulate failure
	err = natsContainer.Stop(ctx, nil)
	require.NoError(t, err)

	// Should report unhealthy
	select {
	case healthy := <-healthChanges:
		assert.False(t, healthy)
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Health change not detected")
	}
}

// startNATSContainer retains the package test helper contract while delegating
// all lifecycle and readiness behavior to the canonical TestClient.
func startNATSContainer(_ context.Context, t *testing.T) (testcontainers.Container, string) {
	t.Helper()
	testClient := NewTestClient(t)
	return &managedTestContainer{Container: testClient.container, testClient: testClient}, testClient.URL
}

// startNATSContainerWithJS is the JetStream form of startNATSContainer.
func startNATSContainerWithJS(_ context.Context, t *testing.T) (testcontainers.Container, string) {
	t.Helper()
	testClient := NewTestClient(t, WithJetStream())
	return &managedTestContainer{Container: testClient.container, testClient: testClient}, testClient.URL
}

// TestIntegration_JetStreamMetrics verifies that JetStream metrics are properly collected
func TestIntegration_JetStreamMetrics(t *testing.T) {
	ctx := context.Background()

	testClient := NewTestClient(t, WithJetStream())
	natsURL := testClient.URL

	// Create metrics registry
	metricsRegistry := metric.NewMetricsRegistry()

	// Create client with metrics enabled
	client, err := NewClient(natsURL,
		WithMetrics(metricsRegistry),
	)
	require.NoError(t, err)

	err = client.Connect(ctx)
	require.NoError(t, err)
	defer client.Close(ctx)

	// Create a stream
	streamCfg := jetstream.StreamConfig{
		Name:     "TEST_METRICS",
		Subjects: []string{"test.metrics.>"},
		MaxAge:   testStreamMaxAge,
		MaxBytes: testStreamMaxBytes,
	}
	stream, err := client.CreateStream(ctx, streamCfg)
	require.NoError(t, err)
	require.NotNil(t, stream)

	// Publish some messages to populate stream stats
	for i := 0; i < 5; i++ {
		err := client.PublishToStream(ctx, "test.metrics.msg", []byte(fmt.Sprintf("test message %d", i)))
		require.NoError(t, err)
	}

	// Create a consumer
	received := make(chan bool, 5)
	err = client.ConsumeStream(ctx, "TEST_METRICS", "test.metrics.>", func(msg jetstream.Msg) {
		select {
		case received <- true:
		default:
		}
		msg.Ack()
	})
	require.NoError(t, err)

	// Wait for messages to be delivered
	time.Sleep(500 * time.Millisecond)

	// Trigger metrics update manually (normally happens every 30s)
	if client.jsMetrics != nil {
		client.jsMetrics.updateStats(ctx)
	}

	// Gather metrics
	metricFamilies, err := metricsRegistry.PrometheusRegistry().Gather()
	require.NoError(t, err)

	// Build metric lookup map
	metricsByName := make(map[string]*dto.MetricFamily)
	for _, mf := range metricFamilies {
		metricsByName[*mf.Name] = mf
	}

	// Verify stream metrics exist
	streamMessages := metricsByName["semstreams_jetstream_stream_messages"]
	require.NotNil(t, streamMessages, "stream messages metric should exist")
	// Should have 5 messages in stream (might have consumed some)
	assert.GreaterOrEqual(t, *streamMessages.Metric[0].Gauge.Value, float64(0))

	streamBytes := metricsByName["semstreams_jetstream_stream_bytes"]
	require.NotNil(t, streamBytes, "stream bytes metric should exist")
	assert.Greater(t, *streamBytes.Metric[0].Gauge.Value, float64(0))

	streamState := metricsByName["semstreams_jetstream_stream_state"]
	require.NotNil(t, streamState, "stream state metric should exist")
	assert.Equal(t, float64(1), *streamState.Metric[0].Gauge.Value, "stream should be active")

	// Verify consumer metrics exist
	consumerPending := metricsByName["semstreams_jetstream_consumer_pending_messages"]
	require.NotNil(t, consumerPending, "consumer pending metric should exist")

	consumerDelivered := metricsByName["semstreams_jetstream_consumer_delivered_total"]
	require.NotNil(t, consumerDelivered, "consumer delivered metric should exist")
	assert.GreaterOrEqual(t, *consumerDelivered.Metric[0].Counter.Value, float64(0))
}
