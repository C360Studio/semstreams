// Package natsclient provides request/reply pattern support for NATS.
package natsclient

import (
	"context"
	"time"

	"github.com/nats-io/nats.go"
)

// DefaultRequestTimeout is the default timeout for request/reply operations.
const DefaultRequestTimeout = 5 * time.Second

// Request performs a synchronous request/reply operation.
// It publishes a message to the subject and waits for a response.
// The timeout parameter controls how long to wait for a response.
// If timeout is 0, DefaultRequestTimeout is used.
//
// Request is the right call for QUERIES (read paths) — failures
// surface to the caller, who decides whether to retry, fall back, or
// raise an error. For MUTATIONS use RequestWithRetry instead;
// without retry, transient "no responders" errors during startup
// races / responder restarts cause silent data loss. See
// docs/operations/07-nats-request-retry.md for the full rule.
func (c *Client) Request(ctx context.Context, subject string, data []byte, timeout time.Duration) ([]byte, error) {
	c.mu.RLock()
	conn := c.conn
	c.mu.RUnlock()

	if conn == nil || !conn.IsConnected() {
		return nil, ErrNotConnected
	}

	// Check circuit breaker
	if c.Status() == StatusCircuitOpen {
		return nil, ErrCircuitOpen
	}

	// Use default timeout if not specified
	if timeout == 0 {
		timeout = DefaultRequestTimeout
	}

	// Auto-generate trace if none exists
	if _, ok := TraceContextFromContext(ctx); !ok {
		ctx = ContextWithTrace(ctx, NewTraceContext())
	}

	// Create a context with timeout if not already set
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Build message with trace headers
	msg := nats.NewMsg(subject)
	msg.Data = data
	InjectTrace(ctx, msg)

	// Perform the request using NATS request/reply
	reply, err := conn.RequestMsgWithContext(reqCtx, msg)
	if err != nil {
		c.recordFailure()
		return nil, err
	}

	c.resetCircuit()
	return reply.Data, nil
}

// RequestWithHeaders performs a request/reply operation with custom headers.
// Headers are passed as a map and converted to NATS message headers.
// Returns the full NATS message to allow access to response headers.
func (c *Client) RequestWithHeaders(
	ctx context.Context,
	subject string,
	data []byte,
	headers map[string]string,
	timeout time.Duration,
) (*nats.Msg, error) {
	c.mu.RLock()
	conn := c.conn
	c.mu.RUnlock()

	if conn == nil || !conn.IsConnected() {
		return nil, ErrNotConnected
	}

	// Check circuit breaker
	if c.Status() == StatusCircuitOpen {
		return nil, ErrCircuitOpen
	}

	// Use default timeout if not specified
	if timeout == 0 {
		timeout = DefaultRequestTimeout
	}

	// Auto-generate trace if none exists
	if _, ok := TraceContextFromContext(ctx); !ok {
		ctx = ContextWithTrace(ctx, NewTraceContext())
	}

	// Create a context with timeout
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Build the message with headers
	msg := nats.NewMsg(subject)
	msg.Data = data

	// Add headers if provided
	if len(headers) > 0 {
		msg.Header = make(nats.Header)
		for key, value := range headers {
			msg.Header.Set(key, value)
		}
	}

	// Inject trace headers (in addition to user headers)
	InjectTrace(ctx, msg)

	// Perform the request
	reply, err := conn.RequestMsgWithContext(reqCtx, msg)
	if err != nil {
		c.recordFailure()
		return nil, err
	}

	c.resetCircuit()
	return reply, nil
}

// Reply sends a reply to a request message.
// This is typically used by service handlers to respond to requests.
func (c *Client) Reply(ctx context.Context, replyTo string, data []byte) error {
	if replyTo == "" {
		return nil // No reply requested
	}

	return c.Publish(ctx, replyTo, data)
}

// ReplyWithHeaders sends a reply with custom headers.
func (c *Client) ReplyWithHeaders(ctx context.Context, replyTo string, data []byte, headers map[string]string) error {
	if replyTo == "" {
		return nil // No reply requested
	}

	c.mu.RLock()
	conn := c.conn
	c.mu.RUnlock()

	if conn == nil || !conn.IsConnected() {
		return ErrNotConnected
	}

	// Auto-generate trace if none exists
	if _, ok := TraceContextFromContext(ctx); !ok {
		ctx = ContextWithTrace(ctx, NewTraceContext())
	}

	// Build the reply message with headers
	msg := nats.NewMsg(replyTo)
	msg.Data = data

	if len(headers) > 0 {
		msg.Header = make(nats.Header)
		for key, value := range headers {
			msg.Header.Set(key, value)
		}
	}

	// Inject trace headers
	InjectTrace(ctx, msg)

	return conn.PublishMsg(msg)
}

// SubscribeForRequests subscribes to a subject and handles request/reply patterns.
// The handler receives the message data and reply subject, and should return
// the response data or an error.
// Returns the Subscription so the caller can unsubscribe when done.
// This is a convenience method for implementing request/reply services.
func (c *Client) SubscribeForRequests(
	ctx context.Context,
	subject string,
	handler func(ctx context.Context, data []byte) ([]byte, error),
) (*Subscription, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn == nil || !c.conn.IsConnected() {
		return nil, ErrNotConnected
	}

	sub, err := c.conn.Subscribe(subject, func(msg *nats.Msg) {
		// Extract trace from incoming request
		msgCtx := ctx
		if tc := ExtractTrace(msg); tc != nil {
			msgCtx = ContextWithTrace(ctx, tc)
		}

		// Create per-message context with timeout
		msgCtx, cancel := context.WithTimeout(msgCtx, 30*time.Second)
		defer cancel()

		// Call the handler
		response, err := handler(msgCtx, msg.Data)
		if err != nil {
			// Send error response if there's a reply subject
			if msg.Reply != "" {
				// Use a simple error format - could be enhanced with structured errors
				_ = msg.Respond([]byte("error: " + err.Error()))
			}
			return
		}

		// Send successful response
		if msg.Reply != "" {
			_ = msg.Respond(response)
		}
	})
	if err != nil {
		return nil, err
	}

	wrappedSub := &Subscription{sub: sub}
	c.subs = append(c.subs, sub)
	return wrappedSub, nil
}

// RetryConfig configures retry behavior for requests.
type RetryConfig struct {
	MaxRetries        int           // Number of retry attempts (default: 3)
	InitialBackoff    time.Duration // First retry delay (default: 100ms)
	MaxBackoff        time.Duration // Cap on backoff growth (default: 2s)
	BackoffMultiplier float64       // Exponential growth factor (default: 2.0)
}

// DefaultRetryConfig returns sensible defaults for retry configuration.
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxRetries:        3,
		InitialBackoff:    100 * time.Millisecond,
		MaxBackoff:        2 * time.Second,
		BackoffMultiplier: 2.0,
	}
}

// RequestWithRetry performs a request with configurable retry on failure.
// This is useful for handling transient "no responders" errors in NATS
// where the subscriber may not be ready when the request arrives.
//
// Use this for MUTATIONS (write paths) where the responder is
// idempotent — the retry path may re-deliver the same request if the
// first attempt's response gets lost, so the responder must converge
// to the same state on duplicate receives. For QUERIES use Request
// instead; retrying on timeout masks hung responders as latency. See
// docs/operations/07-nats-request-retry.md for the full rule.
func (c *Client) RequestWithRetry(
	ctx context.Context,
	subject string,
	data []byte,
	timeout time.Duration,
	retry RetryConfig,
) ([]byte, error) {
	c.mu.RLock()
	conn := c.conn
	c.mu.RUnlock()

	if conn == nil || !conn.IsConnected() {
		return nil, ErrNotConnected
	}

	if c.Status() == StatusCircuitOpen {
		return nil, ErrCircuitOpen
	}

	if timeout == 0 {
		timeout = DefaultRequestTimeout
	}

	// Auto-generate trace if none exists (once for all retries)
	if _, ok := TraceContextFromContext(ctx); !ok {
		ctx = ContextWithTrace(ctx, NewTraceContext())
	}

	var lastErr error
	for attempt := 0; attempt <= retry.MaxRetries; attempt++ {
		reqCtx, cancel := context.WithTimeout(ctx, timeout)

		// Build message with trace headers
		msg := nats.NewMsg(subject)
		msg.Data = data
		InjectTrace(ctx, msg)

		reply, err := conn.RequestMsgWithContext(reqCtx, msg)
		cancel()

		if err == nil {
			c.resetCircuit()
			return reply.Data, nil
		}

		lastErr = err
		c.recordFailure()

		// Wait before retry (if more retries remain)
		if attempt < retry.MaxRetries {
			// Calculate exponential backoff
			backoff := retry.InitialBackoff
			for i := 0; i < attempt; i++ {
				backoff = time.Duration(float64(backoff) * retry.BackoffMultiplier)
			}
			if backoff > retry.MaxBackoff {
				backoff = retry.MaxBackoff
			}

			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
				// Continue to next retry
			}
		}
	}

	return nil, lastErr
}
