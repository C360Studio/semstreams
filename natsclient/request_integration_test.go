//go:build integration

package natsclient

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIntegration_Request tests the basic request/reply pattern
func TestIntegration_Request(t *testing.T) {
	ctx := context.Background()

	// Start NATS container
	natsContainer, natsURL := startNATSContainer(ctx, t)
	defer natsContainer.Terminate(ctx)

	// Create and connect client
	client, err := NewClient(natsURL)
	require.NoError(t, err)
	err = client.Connect(ctx)
	require.NoError(t, err)
	defer client.Close(ctx)

	// Set up a responder
	subject := "test.request"
	expectedResponse := []byte("pong")

	_, err = client.SubscribeForRequests(ctx, subject, func(_ context.Context, data []byte) ([]byte, error) {
		assert.Equal(t, []byte("ping"), data)
		return expectedResponse, nil
	})
	require.NoError(t, err)

	// Give subscription time to be established
	time.Sleep(50 * time.Millisecond)

	// Send request
	response, err := client.Request(ctx, subject, []byte("ping"), 5*time.Second)
	require.NoError(t, err)
	assert.Equal(t, expectedResponse, response)
}

// TestIntegration_Request_Timeout tests request timeout behavior
func TestIntegration_Request_Timeout(t *testing.T) {
	ctx := context.Background()

	// Start NATS container
	natsContainer, natsURL := startNATSContainer(ctx, t)
	defer natsContainer.Terminate(ctx)

	// Create and connect client
	client, err := NewClient(natsURL)
	require.NoError(t, err)
	err = client.Connect(ctx)
	require.NoError(t, err)
	defer client.Close(ctx)

	// Send request to non-existent subject (no responder)
	// NATS may return "no responders" immediately or timeout depending on server config
	_, err = client.Request(ctx, "nonexistent.subject", []byte("test"), 100*time.Millisecond)

	// Should return an error (either timeout or "no responders")
	assert.Error(t, err)
	// The error should be either a timeout or "no responders" error
	errStr := err.Error()
	assert.True(t, errStr == "nats: no responders available for request" ||
		errStr == "context deadline exceeded" ||
		errStr == "nats: timeout",
		"Expected timeout or no responders error, got: %s", errStr)
}

// TestIntegration_Request_DefaultTimeout tests default timeout is applied
func TestIntegration_Request_DefaultTimeout(t *testing.T) {
	ctx := context.Background()

	// Start NATS container
	natsContainer, natsURL := startNATSContainer(ctx, t)
	defer natsContainer.Terminate(ctx)

	// Create and connect client
	client, err := NewClient(natsURL)
	require.NoError(t, err)
	err = client.Connect(ctx)
	require.NoError(t, err)
	defer client.Close(ctx)

	// Request with 0 timeout should use default (5s)
	// We won't wait for full timeout, just verify it doesn't fail immediately
	ctx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()

	_, err = client.Request(ctx, "nonexistent.subject", []byte("test"), 0)
	assert.Error(t, err) // Will error due to context cancellation
}

// TestIntegration_RequestWithHeaders tests request with custom headers
func TestIntegration_RequestWithHeaders(t *testing.T) {
	ctx := context.Background()

	// Start NATS container
	natsContainer, natsURL := startNATSContainer(ctx, t)
	defer natsContainer.Terminate(ctx)

	// Create and connect client
	client, err := NewClient(natsURL)
	require.NoError(t, err)
	err = client.Connect(ctx)
	require.NoError(t, err)
	defer client.Close(ctx)

	// Set up responder using SubscribeForRequests
	// Note: Headers are preserved in the underlying message
	subject := "test.headers"
	_, err = client.SubscribeForRequests(ctx, subject, func(_ context.Context, _ []byte) ([]byte, error) {
		// Return a fixed response
		return []byte("headers-response"), nil
	})
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)

	// Send request with headers
	headers := map[string]string{
		"X-Custom": "test-value",
	}
	response, err := client.RequestWithHeaders(ctx, subject, []byte("data"), headers, 5*time.Second)
	require.NoError(t, err)
	assert.Equal(t, []byte("headers-response"), response.Data)
}

// TestIntegration_Request_NotConnected tests behavior when not connected
func TestIntegration_Request_NotConnected(t *testing.T) {
	// Create client without connecting
	client, err := NewClient("nats://localhost:4222")
	require.NoError(t, err)

	ctx := context.Background()
	_, err = client.Request(ctx, "test.subject", []byte("data"), time.Second)
	assert.Equal(t, ErrNotConnected, err)
}

// TestIntegration_RequestWithHeaders_NotConnected tests headers request when not connected
func TestIntegration_RequestWithHeaders_NotConnected(t *testing.T) {
	// Create client without connecting
	client, err := NewClient("nats://localhost:4222")
	require.NoError(t, err)

	ctx := context.Background()
	_, err = client.RequestWithHeaders(ctx, "test.subject", []byte("data"), nil, time.Second)
	assert.Equal(t, ErrNotConnected, err)
}

// TestIntegration_Reply tests the Reply function
func TestIntegration_Reply(t *testing.T) {
	ctx := context.Background()

	// Start NATS container
	natsContainer, natsURL := startNATSContainer(ctx, t)
	defer natsContainer.Terminate(ctx)

	// Create and connect client
	client, err := NewClient(natsURL)
	require.NoError(t, err)
	err = client.Connect(ctx)
	require.NoError(t, err)
	defer client.Close(ctx)

	// Reply to empty subject should be no-op
	err = client.Reply(ctx, "", []byte("data"))
	assert.NoError(t, err)

	// Reply to valid subject
	err = client.Reply(ctx, "reply.subject", []byte("data"))
	assert.NoError(t, err)
}

// TestIntegration_ReplyWithHeaders tests ReplyWithHeaders function
func TestIntegration_ReplyWithHeaders(t *testing.T) {
	ctx := context.Background()

	// Start NATS container
	natsContainer, natsURL := startNATSContainer(ctx, t)
	defer natsContainer.Terminate(ctx)

	// Create and connect client
	client, err := NewClient(natsURL)
	require.NoError(t, err)
	err = client.Connect(ctx)
	require.NoError(t, err)
	defer client.Close(ctx)

	// Reply with headers to empty subject should be no-op
	err = client.ReplyWithHeaders(ctx, "", []byte("data"), map[string]string{"X-Test": "value"})
	assert.NoError(t, err)

	// Reply with headers to valid subject
	err = client.ReplyWithHeaders(ctx, "reply.subject", []byte("data"), map[string]string{"X-Test": "value"})
	assert.NoError(t, err)
}

// TestIntegration_SubscribeForRequests tests the request handler subscription
func TestIntegration_SubscribeForRequests(t *testing.T) {
	ctx := context.Background()

	// Start NATS container
	natsContainer, natsURL := startNATSContainer(ctx, t)
	defer natsContainer.Terminate(ctx)

	// Create and connect client
	client, err := NewClient(natsURL)
	require.NoError(t, err)
	err = client.Connect(ctx)
	require.NoError(t, err)
	defer client.Close(ctx)

	// Subscribe to requests
	subject := "test.service"
	handlerCalled := make(chan bool, 1)

	_, err = client.SubscribeForRequests(ctx, subject, func(_ context.Context, _ []byte) ([]byte, error) {
		handlerCalled <- true
		return []byte("response"), nil
	})
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)

	// Send a request
	response, err := client.Request(ctx, subject, []byte("request"), 5*time.Second)
	require.NoError(t, err)
	assert.Equal(t, []byte("response"), response)

	// Verify handler was called
	select {
	case <-handlerCalled:
		// Success
	case <-time.After(time.Second):
		t.Fatal("Handler was not called")
	}
}

// TestIntegration_SubscribeForRequests_HandlerTimeoutConfigurable proves the
// per-message handler context carries the CONFIGURED deadline, not the hardcoded
// 30s. The handler reports the remaining budget on its ctx; with a 5s configured
// timeout the deadline must be ~5s (well under the old 30s default), which is what
// lets a slow-by-design handler (8B answer synthesis) get a >30s budget when the
// deployment raises it.
func TestIntegration_SubscribeForRequests_HandlerTimeoutConfigurable(t *testing.T) {
	ctx := context.Background()

	natsContainer, natsURL := startNATSContainer(ctx, t)
	defer natsContainer.Terminate(ctx)

	const configured = 5 * time.Second
	client, err := NewClient(natsURL, WithRequestHandlerTimeout(configured))
	require.NoError(t, err)
	err = client.Connect(ctx)
	require.NoError(t, err)
	defer client.Close(ctx)

	subject := "test.handler.timeout"
	_, err = client.SubscribeForRequests(ctx, subject, func(hctx context.Context, _ []byte) ([]byte, error) {
		dl, ok := hctx.Deadline()
		if !ok {
			return []byte("no-deadline"), nil
		}
		// Report remaining budget in milliseconds so the test can assert it
		// tracks `configured`, not the old hardcoded 30s.
		return []byte(strconv.FormatInt(int64(time.Until(dl)/time.Millisecond), 10)), nil
	})
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)

	resp, err := client.Request(ctx, subject, []byte("x"), 5*time.Second)
	require.NoError(t, err)

	remainingMs, err := strconv.ParseInt(string(resp), 10, 64)
	require.NoError(t, err, "handler must report a deadline, got %q", string(resp))
	// Budget must reflect the 5s config (allow generous slack for delivery),
	// and must be far below the old 30s default.
	assert.Greater(t, remainingMs, int64(3000), "handler budget should be ~5s, got %dms", remainingMs)
	assert.Less(t, remainingMs, int64(5200), "handler budget must reflect the 5s config, not the old 30s default, got %dms", remainingMs)
}

// TestIntegration_SubscribeForRequests_Error tests error handling in request handler
func TestIntegration_SubscribeForRequests_Error(t *testing.T) {
	ctx := context.Background()

	// Start NATS container
	natsContainer, natsURL := startNATSContainer(ctx, t)
	defer natsContainer.Terminate(ctx)

	// Create and connect client
	client, err := NewClient(natsURL)
	require.NoError(t, err)
	err = client.Connect(ctx)
	require.NoError(t, err)
	defer client.Close(ctx)

	// Subscribe with handler that returns error
	subject := "test.error"
	_, err = client.SubscribeForRequests(ctx, subject, func(_ context.Context, _ []byte) ([]byte, error) {
		return nil, assert.AnError
	})
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)

	// ADR-060: a handler error is the {message} envelope + X-Status header;
	// RequestClassified surfaces it as a classified error carrying the message.
	_, err = client.RequestClassified(ctx, subject, []byte("request"), 5*time.Second)
	require.Error(t, err)
	assert.Contains(t, err.Error(), assert.AnError.Error())
}

// TestIntegration_SubscribeForRequests_NotConnected tests subscription when not connected
func TestIntegration_SubscribeForRequests_NotConnected(t *testing.T) {
	// Create client without connecting
	client, err := NewClient("nats://localhost:4222")
	require.NoError(t, err)

	ctx := context.Background()
	_, err = client.SubscribeForRequests(ctx, "test.subject", func(_ context.Context, _ []byte) ([]byte, error) {
		return nil, nil
	})
	assert.Equal(t, ErrNotConnected, err)
}

// TestIntegration_RequestWithRetry_Success tests retry succeeds when responder starts late
func TestIntegration_RequestWithRetry_Success(t *testing.T) {
	ctx := context.Background()

	// Start NATS container
	natsContainer, natsURL := startNATSContainer(ctx, t)
	defer natsContainer.Terminate(ctx)

	// Create and connect client
	client, err := NewClient(natsURL)
	require.NoError(t, err)
	err = client.Connect(ctx)
	require.NoError(t, err)
	defer client.Close(ctx)

	subject := "test.retry"
	expectedResponse := []byte("delayed-response")

	// Start responder after a delay to simulate "not ready yet" scenario
	go func() {
		time.Sleep(150 * time.Millisecond)
		_, err := client.SubscribeForRequests(ctx, subject, func(_ context.Context, data []byte) ([]byte, error) {
			return expectedResponse, nil
		})
		if err != nil {
			t.Logf("Failed to subscribe: %v", err)
		}
	}()

	// Configure retry with short backoff for faster test
	config := DefaultRetryConfig()
	config.InitialBackoff = 50 * time.Millisecond
	config.MaxRetries = 5
	config.BackoffMultiplier = 1.5

	// Send request that should retry until responder is ready
	response, err := client.RequestWithRetry(
		ctx,
		subject,
		[]byte("request"),
		2*time.Second,
		config,
	)

	require.NoError(t, err, "retry should succeed when responder becomes available")
	assert.Equal(t, expectedResponse, response)
}

// TestIntegration_RequestWithRetry_NoResponder tests all retries exhausted when no responder exists
func TestIntegration_RequestWithRetry_NoResponder(t *testing.T) {
	ctx := context.Background()

	// Start NATS container
	natsContainer, natsURL := startNATSContainer(ctx, t)
	defer natsContainer.Terminate(ctx)

	// Create and connect client
	client, err := NewClient(natsURL)
	require.NoError(t, err)
	err = client.Connect(ctx)
	require.NoError(t, err)
	defer client.Close(ctx)

	// Configure retry with short backoff for faster test
	config := DefaultRetryConfig()
	config.InitialBackoff = 20 * time.Millisecond
	config.MaxRetries = 3
	config.MaxBackoff = 100 * time.Millisecond

	// Send request to subject with no responder
	_, err = client.RequestWithRetry(
		ctx,
		"nonexistent.subject",
		[]byte("request"),
		100*time.Millisecond,
		config,
	)

	// Should fail with "no responders" or timeout error
	require.Error(t, err)
	errStr := err.Error()
	assert.True(t,
		errStr == "nats: no responders available for request" ||
			errStr == "context deadline exceeded" ||
			errStr == "nats: timeout",
		"Expected no responders or timeout error, got: %s", errStr)

	// Note: NATS returns "no responders" immediately (no wait), so with retries
	// and backoff the timing is: retry backoffs (20ms + 40ms + 60ms) ~120ms
	// We don't assert on timing as it varies with server configuration
}
