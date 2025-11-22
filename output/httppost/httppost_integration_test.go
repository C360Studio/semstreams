//go:build integration

package httppost_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360/semstreams/component"
	"github.com/c360/semstreams/natsclient"
	"github.com/c360/semstreams/output/httppost"
)

// Package-level shared test client to avoid Docker resource exhaustion
var (
	sharedTestClient *natsclient.TestClient
	sharedNATSClient *natsclient.Client
)

// TestMain sets up a single shared NATS container for all HTTP POST output tests
func TestMain(m *testing.M) {
	if os.Getenv("INTEGRATION_TESTS") != "" {
		// Create a single shared test client for integration tests
		testClient, err := natsclient.NewSharedTestClient(
			natsclient.WithJetStream(),
			natsclient.WithKV(),
			natsclient.WithTestTimeout(5*time.Second),
			natsclient.WithStartTimeout(30*time.Second),
		)
		if err != nil {
			panic("Failed to create shared test client: " + err.Error())
		}

		sharedTestClient = testClient
		sharedNATSClient = testClient.Client
	}

	// Run all tests
	exitCode := m.Run()

	// Cleanup integration test resources if they were created
	if sharedTestClient != nil {
		sharedTestClient.Terminate()
	}

	os.Exit(exitCode)
}

// getSharedNATSClient returns the shared NATS client for integration tests
func getSharedNATSClient(t *testing.T) *natsclient.Client {
	if os.Getenv("INTEGRATION_TESTS") == "" {
		t.Skip("Skipping integration test. Set INTEGRATION_TESTS=1 to run.")
	}
	if sharedNATSClient == nil {
		t.Fatal("Shared NATS client not initialized - TestMain should have created it")
	}
	return sharedNATSClient
}

// TestIntegration_BasicHTTPPost tests NATS message triggering HTTP POST
func TestIntegration_BasicHTTPPost(t *testing.T) {
	natsClient := getSharedNATSClient(t)

	// Create test HTTP server
	receivedMessages := make([][]byte, 0)
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify method and content type
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		// Read body
		buf := make([]byte, r.ContentLength)
		r.Body.Read(buf)

		mu.Lock()
		receivedMessages = append(receivedMessages, buf)
		mu.Unlock()

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Create HTTP POST output config
	config := httppost.Config{
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{Name: "input", Type: "nats", Subject: "test.httppost.input", Required: true},
			},
		},
		URL:         server.URL,
		Timeout:     5,
		RetryCount:  0,
		ContentType: "application/json",
	}

	rawConfig, err := json.Marshal(config)
	require.NoError(t, err)

	deps := component.Dependencies{
		NATSClient: natsClient,
	}

	// Create HTTP POST output
	httpComp, err := httppost.NewOutput(rawConfig, deps)
	require.NoError(t, err)
	require.NotNil(t, httpComp)

	httpOutput, ok := httpComp.(component.LifecycleComponent)
	require.True(t, ok)

	// Initialize
	err = httpOutput.Initialize()
	require.NoError(t, err)

	// Create context
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Start output
	err = httpOutput.Start(ctx)
	require.NoError(t, err)
	defer httpOutput.Stop(5 * time.Second)

	// Give output time to subscribe
	time.Sleep(100 * time.Millisecond)

	// Publish test messages
	testMessages := []map[string]any{
		{"id": 1, "data": "first message"},
		{"id": 2, "data": "second message"},
		{"id": 3, "data": "third message"},
	}

	for _, msg := range testMessages {
		data, err := json.Marshal(msg)
		require.NoError(t, err)

		err = natsClient.Publish(ctx, "test.httppost.input", data)
		require.NoError(t, err)
	}

	// Wait for HTTP POSTs to be made
	time.Sleep(500 * time.Millisecond)

	// Verify all messages received
	mu.Lock()
	require.Len(t, receivedMessages, 3, "Should have received 3 HTTP POSTs")

	for i, received := range receivedMessages {
		var msg map[string]any
		err := json.Unmarshal(received, &msg)
		require.NoError(t, err)

		assert.Equal(t, float64(i+1), msg["id"])
	}
	mu.Unlock()
}

// TestIntegration_CustomHeaders tests HTTP POST with custom headers
func TestIntegration_CustomHeaders(t *testing.T) {
	natsClient := getSharedNATSClient(t)

	// Create test HTTP server that captures headers
	receivedHeaders := make(map[string]string)
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		receivedHeaders["X-Custom-Header"] = r.Header.Get("X-Custom-Header")
		receivedHeaders["Authorization"] = r.Header.Get("Authorization")
		mu.Unlock()

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Create HTTP POST output with custom headers
	config := httppost.Config{
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{Name: "input", Type: "nats", Subject: "test.httppost.headers", Required: true},
			},
		},
		URL: server.URL,
		Headers: map[string]string{
			"X-Custom-Header": "custom-value",
			"Authorization":   "Bearer token123",
		},
		Timeout:     5,
		RetryCount:  0,
		ContentType: "application/json",
	}

	rawConfig, err := json.Marshal(config)
	require.NoError(t, err)

	deps := component.Dependencies{
		NATSClient: natsClient,
	}

	httpComp, err := httppost.NewOutput(rawConfig, deps)
	require.NoError(t, err)

	httpOutput, ok := httpComp.(component.LifecycleComponent)
	require.True(t, ok)

	err = httpOutput.Initialize()
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err = httpOutput.Start(ctx)
	require.NoError(t, err)
	defer httpOutput.Stop(5 * time.Second)

	time.Sleep(100 * time.Millisecond)

	// Publish test message
	testData := map[string]any{"test": "data"}
	data, err := json.Marshal(testData)
	require.NoError(t, err)

	err = natsClient.Publish(ctx, "test.httppost.headers", data)
	require.NoError(t, err)

	time.Sleep(500 * time.Millisecond)

	// Verify headers were sent
	mu.Lock()
	assert.Equal(t, "custom-value", receivedHeaders["X-Custom-Header"])
	assert.Equal(t, "Bearer token123", receivedHeaders["Authorization"])
	mu.Unlock()
}

// TestIntegration_RetryOnFailure tests retry logic with failing HTTP endpoint
func TestIntegration_RetryOnFailure(t *testing.T) {
	natsClient := getSharedNATSClient(t)

	// Create test HTTP server that fails first 2 attempts
	attemptCount := 0
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		attemptCount++
		currentAttempt := attemptCount
		mu.Unlock()

		if currentAttempt < 3 {
			// Fail first two attempts
			w.WriteHeader(http.StatusInternalServerError)
		} else {
			// Succeed on third attempt
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	// Create HTTP POST output with retry
	config := httppost.Config{
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{Name: "input", Type: "nats", Subject: "test.httppost.retry", Required: true},
			},
		},
		URL:         server.URL,
		Timeout:     5,
		RetryCount:  3,
		ContentType: "application/json",
	}

	rawConfig, err := json.Marshal(config)
	require.NoError(t, err)

	deps := component.Dependencies{
		NATSClient: natsClient,
	}

	httpComp, err := httppost.NewOutput(rawConfig, deps)
	require.NoError(t, err)

	httpOutput, ok := httpComp.(component.LifecycleComponent)
	require.True(t, ok)

	err = httpOutput.Initialize()
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err = httpOutput.Start(ctx)
	require.NoError(t, err)
	defer httpOutput.Stop(5 * time.Second)

	time.Sleep(100 * time.Millisecond)

	// Publish test message
	testData := map[string]any{"test": "retry"}
	data, err := json.Marshal(testData)
	require.NoError(t, err)

	err = natsClient.Publish(ctx, "test.httppost.retry", data)
	require.NoError(t, err)

	// Wait for retries to complete (initial + 2 retries with exponential backoff)
	// First retry: ~100ms, Second retry: ~400ms
	time.Sleep(2 * time.Second)

	// Verify retry attempts
	mu.Lock()
	assert.Equal(t, 3, attemptCount, "Should have attempted 3 times (initial + 2 retries)")
	mu.Unlock()
}

// TestIntegration_StatusCodeValidation tests various HTTP status codes
func TestIntegration_StatusCodeValidation(t *testing.T) {
	natsClient := getSharedNATSClient(t)

	tests := []struct {
		name       string
		subject    string
		statusCode int
		expectOK   bool
	}{
		{"200 OK", "test.httppost.status.200-ok", http.StatusOK, true},
		{"201 Created", "test.httppost.status.201-created", http.StatusCreated, true},
		{"204 No Content", "test.httppost.status.204-nocontent", http.StatusNoContent, true},
		{"400 Bad Request", "test.httppost.status.400-badrequest", http.StatusBadRequest, false},
		{"500 Internal Error", "test.httppost.status.500-error", http.StatusInternalServerError, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			receivedCount := 0
			var mu sync.Mutex

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				mu.Lock()
				receivedCount++
				mu.Unlock()
				w.WriteHeader(tt.statusCode)
			}))
			defer server.Close()

			config := httppost.Config{
				Ports: &component.PortConfig{
					Inputs: []component.PortDefinition{
						{Name: "input", Type: "nats", Subject: tt.subject, Required: true},
					},
				},
				URL:         server.URL,
				Timeout:     5,
				RetryCount:  0, // No retries for this test
				ContentType: "application/json",
			}

			rawConfig, err := json.Marshal(config)
			require.NoError(t, err)

			deps := component.Dependencies{
				NATSClient: natsClient,
			}

			httpComp, err := httppost.NewOutput(rawConfig, deps)
			require.NoError(t, err)

			httpOutput, ok := httpComp.(component.LifecycleComponent)
			require.True(t, ok)

			err = httpOutput.Initialize()
			require.NoError(t, err)

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			err = httpOutput.Start(ctx)
			require.NoError(t, err)
			defer httpOutput.Stop(5 * time.Second)

			time.Sleep(100 * time.Millisecond)

			// Publish test message
			testData := map[string]any{"status": tt.statusCode}
			data, err := json.Marshal(testData)
			require.NoError(t, err)

			err = natsClient.Publish(ctx, tt.subject, data)
			require.NoError(t, err)

			time.Sleep(500 * time.Millisecond)

			// Verify HTTP endpoint was called
			mu.Lock()
			assert.Equal(t, 1, receivedCount, "HTTP endpoint should be called once")
			mu.Unlock()

			// Check health status
			health := httpComp.Health()
			if tt.expectOK {
				assert.Equal(t, 0, health.ErrorCount, "Should have no errors for successful status codes")
			} else {
				assert.Greater(t, health.ErrorCount, 0, "Should have errors for failed status codes")
			}
		})
	}
}

// TestIntegration_MultipleSubjects tests HTTP POST output subscribing to multiple NATS subjects
func TestIntegration_MultipleSubjects(t *testing.T) {
	natsClient := getSharedNATSClient(t)

	// Create test HTTP server
	receivedMessages := make([]map[string]any, 0)
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, r.ContentLength)
		r.Body.Read(buf)

		var msg map[string]any
		json.Unmarshal(buf, &msg)

		mu.Lock()
		receivedMessages = append(receivedMessages, msg)
		mu.Unlock()

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Create HTTP POST output with multiple input subjects
	config := httppost.Config{
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{Name: "input1", Type: "nats", Subject: "test.httppost.multi.1", Required: true},
				{Name: "input2", Type: "nats", Subject: "test.httppost.multi.2", Required: true},
			},
		},
		URL:         server.URL,
		Timeout:     5,
		RetryCount:  0,
		ContentType: "application/json",
	}

	rawConfig, err := json.Marshal(config)
	require.NoError(t, err)

	deps := component.Dependencies{
		NATSClient: natsClient,
	}

	httpComp, err := httppost.NewOutput(rawConfig, deps)
	require.NoError(t, err)

	httpOutput, ok := httpComp.(component.LifecycleComponent)
	require.True(t, ok)

	err = httpOutput.Initialize()
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err = httpOutput.Start(ctx)
	require.NoError(t, err)
	defer httpOutput.Stop(5 * time.Second)

	time.Sleep(100 * time.Millisecond)

	// Publish to both subjects
	msg1 := map[string]any{"source": "subject1", "data": "from subject 1"}
	data1, err := json.Marshal(msg1)
	require.NoError(t, err)

	msg2 := map[string]any{"source": "subject2", "data": "from subject 2"}
	data2, err := json.Marshal(msg2)
	require.NoError(t, err)

	err = natsClient.Publish(ctx, "test.httppost.multi.1", data1)
	require.NoError(t, err)

	err = natsClient.Publish(ctx, "test.httppost.multi.2", data2)
	require.NoError(t, err)

	time.Sleep(500 * time.Millisecond)

	// Verify both messages were sent via HTTP POST
	mu.Lock()
	require.Len(t, receivedMessages, 2, "Should have received 2 HTTP POSTs")

	sources := make([]string, 2)
	for i, msg := range receivedMessages {
		sources[i] = msg["source"].(string)
	}

	assert.Contains(t, sources, "subject1", "Should have received message from subject1")
	assert.Contains(t, sources, "subject2", "Should have received message from subject2")
	mu.Unlock()
}
