//go:build integration

package natsclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIntegration_NewTestClient_BasicConnection(t *testing.T) {
	testClient := NewTestClient(t, WithMonitoring())
	require.NotNil(t, testClient)
	require.NotNil(t, testClient.Client)
	assert.True(t, testClient.IsReady())
	assert.NotEmpty(t, testClient.URL)
	assert.NotEmpty(t, testClient.MonitoringURL)

	httpClient := &http.Client{Timeout: 5 * time.Second}
	response, err := httpClient.Get(testClient.MonitoringURL + "/varz") //nolint:gosec // testcontainers URL
	require.NoError(t, err)
	defer response.Body.Close()
	require.Equal(t, http.StatusOK, response.StatusCode)
	var server struct {
		Port int `json:"port"`
	}
	require.NoError(t, json.NewDecoder(response.Body).Decode(&server))
	require.Equal(t, 4222, server.Port, "monitoring URL must address the started test container")
}

func TestIntegration_NewTestClient_WithFastStartup(t *testing.T) {
	start := time.Now()
	testClient := NewTestClient(t, WithFastStartup())
	elapsed := time.Since(start)

	require.NotNil(t, testClient)
	assert.True(t, testClient.IsReady())

	// Should startup faster than default
	assert.Less(t, elapsed, 15*time.Second, "Fast startup should complete quickly")
}

func TestIntegration_NewTestClient_WithJetStream(t *testing.T) {
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
		MaxAge:   testStreamMaxAge,
		MaxBytes: testStreamMaxBytes,
	}

	stream, err := testClient.Client.CreateStream(ctx, streamCfg)
	require.NoError(t, err)
	require.NotNil(t, stream)
}

func TestIntegration_NewTestClient_WithKV(t *testing.T) {
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

func TestIntegration_NewTestClient_WithKVBuckets(t *testing.T) {
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

func TestIntegration_NewTestClient_WithStreams(t *testing.T) {
	streams := []TestStreamConfig{
		{Name: "STREAM1", Subjects: []string{"stream1.>"}},
		{Name: "STREAM2", Subjects: []string{"stream2.>"}},
	}
	testClient := NewTestClient(t, WithStreams(streams...))
	require.NotNil(t, testClient)
	assert.True(t, testClient.IsReady())

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Verify all streams were created
	for _, streamCfg := range streams {
		stream, err := testClient.GetStream(ctx, streamCfg.Name)
		require.NoError(t, err, "Stream %s should exist", streamCfg.Name)
		require.NotNil(t, stream)

		// Verify stream info
		info, err := stream.Info(ctx)
		require.NoError(t, err)
		assert.Equal(t, streamCfg.Name, info.Config.Name)
	}
}

func TestIntegration_NewTestClient_CreateStreamHelper(t *testing.T) {
	testClient := NewTestClient(t, WithJetStream())
	require.NotNil(t, testClient)
	assert.True(t, testClient.IsReady())

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Create stream using helper
	stream, err := testClient.CreateStream(ctx, "HELPER_STREAM", []string{"helper.>"})
	require.NoError(t, err)
	require.NotNil(t, stream)

	// Verify it exists
	info, err := stream.Info(ctx)
	require.NoError(t, err)
	assert.Equal(t, "HELPER_STREAM", info.Config.Name)

	// Get stream using helper
	retrieved, err := testClient.GetStream(ctx, "HELPER_STREAM")
	require.NoError(t, err)
	require.NotNil(t, retrieved)
}

func TestIntegration_NewTestClient_PubSub(t *testing.T) {
	testClient := NewTestClient(t, WithMinimalFeatures())
	require.NotNil(t, testClient)
	assert.True(t, testClient.IsReady())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Setup subscription
	var received []byte
	var receivedMu sync.Mutex
	receiveCh := make(chan struct{})

	_, err := testClient.Client.Subscribe(ctx, "test.subject", func(_ context.Context, msg *nats.Msg) {
		receivedMu.Lock()
		received = msg.Data
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

func TestIntegration_NewTestClient_ParallelExecution(t *testing.T) {
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

func TestIntegration_NewTestClient_CleanupOnFailure(t *testing.T) {
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

func TestIntegration_NewTestClient_GetNativeConnection(t *testing.T) {
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

func TestIntegration_NewTestClient_IntegrationDefaults(t *testing.T) {
	testClient := NewTestClient(t, WithIntegrationDefaults())
	require.NotNil(t, testClient)
	assert.True(t, testClient.IsReady())

	// Should have JetStream enabled
	js, err := testClient.Client.JetStream()
	require.NoError(t, err)
	require.NotNil(t, js)
}

func TestIntegration_NewTestClient_E2EDefaults(t *testing.T) {
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
func BenchmarkIntegration_NewTestClient_Minimal(b *testing.B) {
	for i := 0; i < b.N; i++ {
		testClient, err := NewSharedTestClient(WithMinimalFeatures())
		if err != nil {
			b.Fatal(err)
		}
		if err := testClient.Terminate(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkIntegration_NewTestClient_WithJetStream(b *testing.B) {
	for i := 0; i < b.N; i++ {
		testClient, err := NewSharedTestClient(WithJetStream())
		if err != nil {
			b.Fatal(err)
		}
		if err := testClient.Terminate(); err != nil {
			b.Fatal(err)
		}
	}
}
