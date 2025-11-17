package httppost

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/c360/semstreams/component"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHTTPPostOutput_Creation(t *testing.T) {
	config := Config{
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{Name: "input", Type: "nats", Subject: "test.input", Required: true},
			},
		},
		URL:         "http://localhost:8080/webhook",
		Headers:     map[string]string{"X-Custom": "value"},
		Timeout:     30,
		RetryCount:  3,
		ContentType: "application/json",
	}

	rawConfig, err := json.Marshal(config)
	require.NoError(t, err)

	deps := component.Dependencies{
		NATSClient: nil,
	}

	output, err := NewOutput(rawConfig, deps)
	require.NoError(t, err)
	require.NotNil(t, output)

	meta := output.Meta()
	assert.Equal(t, "httppost-output", meta.Name)
	assert.Equal(t, "output", meta.Type)
}

func TestHTTPPostOutput_DefaultConfig(t *testing.T) {
	config := DefaultConfig()

	assert.NotNil(t, config.Ports)
	assert.Len(t, config.Ports.Inputs, 1)
	assert.Equal(t, "output.>", config.Ports.Inputs[0].Subject)
	assert.Equal(t, "http://localhost:8080/webhook", config.URL)
	assert.Equal(t, 30, config.Timeout)
	assert.Equal(t, 3, config.RetryCount)
	assert.Equal(t, "application/json", config.ContentType)
}

func TestHTTPPostOutput_Lifecycle(t *testing.T) {
	config := Config{
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{Name: "input", Type: "nats", Subject: "test.input", Required: true},
			},
		},
		URL:         "http://localhost:8080/test",
		Timeout:     5,
		RetryCount:  1,
		ContentType: "application/json",
	}

	rawConfig, err := json.Marshal(config)
	require.NoError(t, err)

	deps := component.Dependencies{
		NATSClient: nil,
	}

	output, err := NewOutput(rawConfig, deps)
	require.NoError(t, err)

	lifecycleComp, ok := output.(component.LifecycleComponent)
	require.True(t, ok)

	// Initialize should work without error (no-op for HTTP POST)
	err = lifecycleComp.Initialize()
	assert.NoError(t, err)

	// Health check (without starting)
	health := output.Health()
	assert.False(t, health.Healthy) // Not started yet
}

func TestHTTPPostOutput_SendHTTPPost(t *testing.T) {
	// Create a test HTTP server
	receivedData := make([][]byte, 0)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify method
		assert.Equal(t, "POST", r.Method)

		// Verify content type
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		// Read body
		var data []byte
		data = make([]byte, r.ContentLength)
		_, _ = r.Body.Read(data)
		receivedData = append(receivedData, data)

		// Return success
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	config := Config{
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{Name: "input", Type: "nats", Subject: "test.input", Required: true},
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
		NATSClient: nil,
	}

	output, err := NewOutput(rawConfig, deps)
	require.NoError(t, err)

	httpOutput := output.(*Output)

	// Test sending a message
	testData := []byte(`{"test":"data"}`)
	err = httpOutput.sendHTTPPost(context.Background(), testData)
	assert.NoError(t, err)

	// Verify data was received
	require.Len(t, receivedData, 1)
	assert.Equal(t, testData, receivedData[0])
}

func TestHTTPPostOutput_SendWithCustomHeaders(t *testing.T) {
	// Create a test HTTP server
	receivedHeaders := make(map[string]string)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Capture custom headers
		receivedHeaders["X-Custom-Header"] = r.Header.Get("X-Custom-Header")
		receivedHeaders["Authorization"] = r.Header.Get("Authorization")

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	config := Config{
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{Name: "input", Type: "nats", Subject: "test.input", Required: true},
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
		NATSClient: nil,
	}

	output, err := NewOutput(rawConfig, deps)
	require.NoError(t, err)

	httpOutput := output.(*Output)

	// Test sending with custom headers
	testData := []byte(`{"test":"data"}`)
	err = httpOutput.sendHTTPPost(context.Background(), testData)
	assert.NoError(t, err)

	// Verify headers were sent
	assert.Equal(t, "custom-value", receivedHeaders["X-Custom-Header"])
	assert.Equal(t, "Bearer token123", receivedHeaders["Authorization"])
}

func TestHTTPPostOutput_RetryOnFailure(t *testing.T) {
	attemptCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attemptCount++
		if attemptCount < 3 {
			// Fail first two attempts
			w.WriteHeader(http.StatusInternalServerError)
		} else {
			// Succeed on third attempt
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	config := Config{
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{Name: "input", Type: "nats", Subject: "test.input", Required: true},
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
		NATSClient: nil,
	}

	output, err := NewOutput(rawConfig, deps)
	require.NoError(t, err)

	httpOutput := output.(*Output)

	// Test retry logic via handleMessage
	testData := []byte(`{"test":"data"}`)
	httpOutput.handleMessage(context.Background(), testData)

	// Should have attempted 3 times (initial + 2 retries)
	assert.Equal(t, 3, attemptCount)

	// Check metrics
	assert.Equal(t, int64(1), httpOutput.messagesSent)
	assert.Equal(t, int64(2), httpOutput.messagesRetried) // 2 retries before success
}

func TestHTTPPostOutput_ExponentialBackoff(t *testing.T) {
	attemptTimes := make([]time.Time, 0)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attemptTimes = append(attemptTimes, time.Now())
		// Always fail to test all retries
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	config := Config{
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{Name: "input", Type: "nats", Subject: "test.input", Required: true},
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
		NATSClient: nil,
	}

	output, err := NewOutput(rawConfig, deps)
	require.NoError(t, err)

	httpOutput := output.(*Output)

	// Test retry logic with backoff
	testData := []byte(`{"test":"data"}`)
	httpOutput.handleMessage(context.Background(), testData)

	// Should have 4 attempts (initial + 3 retries)
	require.Len(t, attemptTimes, 4)

	// Check that delays increased (exponential backoff)
	// First retry: ~100ms (1*1*100)
	// Second retry: ~400ms (2*2*100)
	// Third retry: ~900ms (3*3*100)
	delay1 := attemptTimes[1].Sub(attemptTimes[0])
	delay2 := attemptTimes[2].Sub(attemptTimes[1])
	delay3 := attemptTimes[3].Sub(attemptTimes[2])

	// Verify delays are roughly exponential (with some tolerance)
	assert.Greater(t, delay1, 50*time.Millisecond)  // At least 50ms
	assert.Greater(t, delay2, 200*time.Millisecond) // At least 200ms
	assert.Greater(t, delay3, 400*time.Millisecond) // At least 400ms

	// Check metrics - all attempts failed
	assert.Equal(t, int64(0), httpOutput.messagesSent)
	assert.Equal(t, int64(1), httpOutput.errors)
}

func TestHTTPPostOutput_StatusCodeValidation(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		expectErr  bool
	}{
		{"200 OK", http.StatusOK, false},
		{"201 Created", http.StatusCreated, false},
		{"204 No Content", http.StatusNoContent, false},
		{"400 Bad Request", http.StatusBadRequest, true},
		{"404 Not Found", http.StatusNotFound, true},
		{"500 Internal Error", http.StatusInternalServerError, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.statusCode)
			}))
			defer server.Close()

			config := Config{
				Ports: &component.PortConfig{
					Inputs: []component.PortDefinition{
						{Name: "input", Type: "nats", Subject: "test.input", Required: true},
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
				NATSClient: nil,
			}

			output, err := NewOutput(rawConfig, deps)
			require.NoError(t, err)

			httpOutput := output.(*Output)

			err = httpOutput.sendHTTPPost(context.Background(), []byte(`{"test":"data"}`))
			if tt.expectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
