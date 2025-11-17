package natsclient

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewTestClient_BasicConnection(t *testing.T) {
	testClient := NewTestClient(t)
	require.NotNil(t, testClient)
	require.NotNil(t, testClient.Client)
	assert.True(t, testClient.IsReady())
	assert.NotEmpty(t, testClient.URL)
}

func TestNewTestClient_WithFastStartup(t *testing.T) {
	start := time.Now()
	testClient := NewTestClient(t, WithFastStartup())
	elapsed := time.Since(start)

	require.NotNil(t, testClient)
	assert.True(t, testClient.IsReady())

	// Should startup faster than default
	assert.Less(t, elapsed, 15*time.Second, "Fast startup should complete quickly")
}

func TestNewTestClient_WithJetStream(t *testing.T) {
	testClient := NewTestClient(t, WithJetStream())
	require.NotNil(t, testClient)
	assert.True(t, testClient.IsReady())

	// Test JetStream functionality
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	js, err := testClient.Client.JetStream()
	require.NoError(t, err)
	require.NotNil(t, js)

	// Create a test stream
	streamCfg := jetstream.StreamConfig{
		Name:     "TEST_STREAM",
		Subjects: []string{"test.>"},
	}

	stream, err := testClient.Client.CreateStream(ctx, streamCfg)
	require.NoError(t, err)
	require.NotNil(t, stream)
}

func TestNewTestClient_WithKV(t *testing.T) {
	testClient := NewTestClient(t, WithKV())
	require.NotNil(t, testClient)
	assert.True(t, testClient.IsReady())

	// Test KV functionality
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	bucket, err := testClient.CreateKVBucket(ctx, "test-bucket")
	require.NoError(t, err)
	require.NotNil(t, bucket)

	// Test put/get
	_, err = bucket.Put(ctx, "test-key", []byte("test-value"))
	require.NoError(t, err)

	entry, err := bucket.Get(ctx, "test-key")
	require.NoError(t, err)
	assert.Equal(t, []byte("test-value"), entry.Value())
}

func TestNewTestClient_WithKVBuckets(t *testing.T) {
	buckets := []string{"bucket1", "bucket2", "bucket3"}
	testClient := NewTestClient(t, WithKVBuckets(buckets...))
	require.NotNil(t, testClient)
	assert.True(t, testClient.IsReady())

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Verify all buckets were created
	for _, bucketName := range buckets {
		bucket, err := testClient.GetKVBucket(ctx, bucketName)
		require.NoError(t, err, "Bucket %s should exist", bucketName)
		require.NotNil(t, bucket)

		// Test basic functionality
		_, err = bucket.Put(ctx, "test", []byte("value"))
		assert.NoError(t, err, "Should be able to put to bucket %s", bucketName)
	}
}

func TestNewTestClient_PubSub(t *testing.T) {
	testClient := NewTestClient(t, WithMinimalFeatures())
	require.NotNil(t, testClient)
	assert.True(t, testClient.IsReady())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Setup subscription
	var received []byte
	var receivedMu sync.Mutex
	receiveCh := make(chan struct{})

	err := testClient.Client.Subscribe(ctx, "test.subject", func(_ context.Context, data []byte) {
		receivedMu.Lock()
		received = data
		receivedMu.Unlock()
		close(receiveCh)
	})
	require.NoError(t, err)

	// Give subscription time to register
	time.Sleep(100 * time.Millisecond)

	// Publish message
	testData := []byte("hello world")
	err = testClient.Client.Publish(ctx, "test.subject", testData)
	require.NoError(t, err)

	// Wait for message
	select {
	case <-receiveCh:
		receivedMu.Lock()
		assert.Equal(t, testData, received)
		receivedMu.Unlock()
	case <-ctx.Done():
		t.Fatal("Timeout waiting for message")
	}
}

func TestNewTestClient_ParallelExecution(t *testing.T) {
	// Test that multiple test clients can run in parallel
	const numClients = 3
	var wg sync.WaitGroup
	results := make(chan bool, numClients)

	for i := 0; i < numClients; i++ {
		wg.Add(1)
		go func(clientID int) {
			defer wg.Done()

			// Each goroutine creates its own test client
			testClient := NewTestClient(t, WithFastStartup(), WithKV())

			// Verify it's working
			if !testClient.IsReady() {
				results <- false
				return
			}

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			// Create a unique bucket for this client
			bucketName := fmt.Sprintf("parallel-test-%d", clientID)
			bucket, err := testClient.CreateKVBucket(ctx, bucketName)
			if err != nil {
				results <- false
				return
			}

			// Test basic KV operations
			key := fmt.Sprintf("key-%d", clientID)
			value := fmt.Sprintf("value-%d", clientID)

			_, err = bucket.Put(ctx, key, []byte(value))
			if err != nil {
				results <- false
				return
			}

			entry, err := bucket.Get(ctx, key)
			if err != nil || string(entry.Value()) != value {
				results <- false
				return
			}

			results <- true
		}(i)
	}

	wg.Wait()
	close(results)

	// Check all clients succeeded
	successCount := 0
	for result := range results {
		if result {
			successCount++
		}
	}

	assert.Equal(t, numClients, successCount, "All parallel clients should succeed")
}

func TestNewTestClient_CleanupOnFailure(t *testing.T) {
	// This test verifies that resources are cleaned up even if test setup fails
	// We can't easily trigger a real failure, so we test the cleanup path directly
	testClient := NewTestClient(t, WithFastStartup())
	require.NotNil(t, testClient)

	// Manually call cleanup to verify it doesn't panic
	assert.NotPanics(t, func() {
		testClient.Terminate()
	})

	// Second call should also not panic
	assert.NotPanics(t, func() {
		testClient.Terminate()
	})
}

func TestNewTestClient_GetNativeConnection(t *testing.T) {
	testClient := NewTestClient(t, WithFastStartup())
	require.NotNil(t, testClient)

	conn := testClient.GetNativeConnection()
	require.NotNil(t, conn)
	assert.True(t, conn.IsConnected())

	// Test that we can use the native connection directly
	// Test RTT using native connection
	rtt, err := conn.RTT()
	require.NoError(t, err)
	assert.Greater(t, rtt, time.Duration(0))
}

func TestNewTestClient_IntegrationDefaults(t *testing.T) {
	testClient := NewTestClient(t, WithIntegrationDefaults())
	require.NotNil(t, testClient)
	assert.True(t, testClient.IsReady())

	// Should have JetStream enabled
	js, err := testClient.Client.JetStream()
	require.NoError(t, err)
	require.NotNil(t, js)
}

func TestNewTestClient_E2EDefaults(t *testing.T) {
	testClient := NewTestClient(t, WithE2EDefaults())
	require.NotNil(t, testClient)
	assert.True(t, testClient.IsReady())

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Should have both JetStream and KV
	js, err := testClient.Client.JetStream()
	require.NoError(t, err)
	require.NotNil(t, js)

	// Should be able to create KV buckets
	bucket, err := testClient.CreateKVBucket(ctx, "e2e-test")
	require.NoError(t, err)
	require.NotNil(t, bucket)
}

// Benchmark tests for performance analysis
func BenchmarkNewTestClient_Minimal(b *testing.B) {
	for i := 0; i < b.N; i++ {
		testClient := NewTestClient(&testing.T{}, WithMinimalFeatures())
		_ = testClient.Terminate()
	}
}

func BenchmarkNewTestClient_WithJetStream(b *testing.B) {
	for i := 0; i < b.N; i++ {
		testClient := NewTestClient(&testing.T{}, WithJetStream())
		_ = testClient.Terminate()
	}
}
