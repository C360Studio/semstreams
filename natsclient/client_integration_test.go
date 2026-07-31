//go:build integration

package natsclient

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// TestIntegration_KeyValueBuckets_RealServer tests KV operations against a real NATS server.
// Extracted from TestKeyValueBuckets subtest "operations work with real KV server".
func TestIntegration_KeyValueBuckets_RealServer(t *testing.T) {
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
}

// TestIntegration_ContextAwareMethods_RealServer tests context-aware methods against a real NATS server.
// Extracted from TestContextAwareMethods subtest "with real NATS server".
func TestIntegration_ContextAwareMethods_RealServer(t *testing.T) {
	ctx := t.Context()
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
	sub, err := client.Subscribe(ctx, "test.reply", func(_ context.Context, msg *nats.Msg) {
		received <- msg.Data
	})
	require.NoError(t, err)
	defer sub.Unsubscribe()

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
}

// TestIntegration_JetStreamMethods_RealServer tests JetStream methods against a real NATS server.
// Extracted from TestJetStreamMethods subtest "with real JetStream server".
func TestIntegration_JetStreamMethods_RealServer(t *testing.T) {
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
	cfg := jetstream.StreamConfig{
		Name: "UNIT_TEST", Subjects: []string{"unit.test.*"},
		MaxAge: testStreamMaxAge, MaxBytes: testStreamMaxBytes,
	}
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
	err = client.ConsumeStream(ctx, "UNIT_TEST", "unit.test.*", func(msg jetstream.Msg) {
		received <- msg.Data()
		msg.Ack()
	})
	require.NoError(t, err)

	// Verify message received
	select {
	case data := <-received:
		assert.Equal(t, []byte("test message"), data)
	case <-time.After(2 * time.Second):
		t.Fatal("Stream message not received")
	}
}

// Helper function to start NATS container for integration tests.
//
// Wait strategy mirrors test_client.go: combined port + HTTP health on the
// monitoring port with an explicit startup timeout. The bare ForListeningPort
// strategy (without timeout) flakes under Docker API pressure when many
// containers spawn in parallel — testcontainers' container-inspect call
// can hit its default deadline before the port mapping resolves.
func startTestNATSContainer(ctx context.Context, t *testing.T) (testcontainers.Container, string) {
	t.Helper()

	req := testcontainers.ContainerRequest{
		Image:        "nats:2.14-alpine",
		ExposedPorts: []string{"4222/tcp", "8222/tcp"},
		Cmd:          []string{"--http_port", "8222"},
		WaitingFor: wait.ForAll(
			wait.ForListeningPort("4222/tcp"),
			wait.ForHTTP("/").WithPort("8222/tcp").WithStartupTimeout(2*time.Minute),
		),
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

// Helper function to start NATS container with JetStream for integration tests.
// Wait strategy rationale: see startTestNATSContainer above.
func startTestNATSContainerWithJS(ctx context.Context, t *testing.T) (testcontainers.Container, string) {
	t.Helper()

	req := testcontainers.ContainerRequest{
		Image:        "nats:2.14-alpine",
		ExposedPorts: []string{"4222/tcp", "8222/tcp"},
		Cmd:          []string{"--js", "--http_port", "8222"}, // Enable JetStream + monitoring
		WaitingFor: wait.ForAll(
			wait.ForListeningPort("4222/tcp"),
			wait.ForHTTP("/").WithPort("8222/tcp").WithStartupTimeout(2*time.Minute),
		),
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
