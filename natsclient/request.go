// Package natsclient provides request/reply pattern support for NATS.
package natsclient

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/nats-io/nats.go"
)

// DefaultRequestTimeout is the default timeout for request/reply operations.
const DefaultRequestTimeout = 5 * time.Second

// DefaultRequestHandlerTimeout bounds a single inbound request-handler
// invocation (SubscribeForRequests). It caps how long a handler may run
// before its context is cancelled, so a wedged or pathologically slow
// handler cannot pin a NATS delivery goroutine indefinitely. This is the
// CI/default value and MUST NOT change without weighing every request
// handler in the tree; slow-by-design handlers (e.g. an LLM answer-synthesis
// path that legitimately needs >30s) raise it per deployment via
// WithRequestHandlerTimeout or the SEMSTREAMS_NATS_REQUEST_HANDLER_TIMEOUT
// environment variable rather than editing this constant.
const DefaultRequestHandlerTimeout = 30 * time.Second

// requestHandlerTimeoutEnv is the deployment-level override for the
// per-message request-handler timeout. Parsed as a Go duration (e.g.
// "150s"); an unset or unparseable value leaves DefaultRequestHandlerTimeout
// in force.
const requestHandlerTimeoutEnv = "SEMSTREAMS_NATS_REQUEST_HANDLER_TIMEOUT"

// resolveRequestHandlerTimeoutFromEnv returns the request-handler timeout
// implied by requestHandlerTimeoutEnv, or DefaultRequestHandlerTimeout when
// the variable is unset or malformed. A malformed value is logged and
// ignored (fail-safe to the default) rather than failing construction.
func resolveRequestHandlerTimeoutFromEnv() time.Duration {
	raw := os.Getenv(requestHandlerTimeoutEnv)
	if raw == "" {
		return DefaultRequestHandlerTimeout
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		slog.Default().Warn("natsclient: ignoring invalid request-handler timeout override",
			slog.String("env", requestHandlerTimeoutEnv),
			slog.String("value", raw),
			slog.Duration("using", DefaultRequestHandlerTimeout))
		return DefaultRequestHandlerTimeout
	}
	return d
}

// Readiness-gated read defaults (ADR-060 sibling doctrine, third bucket — see
// docs/operations/07-nats-request-retry.md). A readiness-gated read tolerates a
// not-yet-subscribed responder at cold start / after reconnect: it retries with
// a SHORT per-attempt timeout up to a bounded TOTAL budget, returning the first
// reply. Because the per-attempt timeout is short and the total is bounded, a
// genuinely hung responder fails within the budget rather than a full-timeout×N
// storm — so it does NOT mask a hung responder the way retrying a full-timeout
// query would. Distinct from Request (steady-state query: timeout = real signal,
// no retry) and RequestWithRetry (mutation: retry-any at full timeout).
const (
	// DefaultReadinessProbeTimeout bounds each attempt. Short so a
	// not-yet-subscribed responder fails fast and retries instead of consuming a
	// full query timeout per attempt.
	DefaultReadinessProbeTimeout = 2 * time.Second
	// DefaultReadinessBudget bounds the TOTAL wall-clock spent waiting for the
	// responder to come up before the error surfaces.
	DefaultReadinessBudget = 30 * time.Second

	readinessInitialBackoff = 100 * time.Millisecond
	readinessMaxBackoff     = 1 * time.Second
)

// RequestReady performs a readiness-gated read: a QUERY that tolerates a
// not-yet-subscribed responder at cold start / after reconnect. It retries with
// probeTimeout per attempt up to a total budget, returning the first reply's
// data. Zero probeTimeout/budget use the Default* values above.
//
// Use for the FIRST read on boot / initial reconcile / after a reconnect, where
// "no responder" means "not ready yet." For steady-state reads use Request
// (timeout = real signal). See docs/operations/07-nats-request-retry.md.
func (c *Client) RequestReady(
	ctx context.Context, subject string, data []byte, probeTimeout, budget time.Duration,
) ([]byte, error) {
	msg, err := c.requestMsgReady(ctx, subject, data, probeTimeout, budget)
	if err != nil {
		return nil, err
	}
	return msg.Data, nil
}

// requestMsgReady is the shared readiness-retry loop. Returns the raw *nats.Msg
// so callers extract .Data (RequestReady) or run ClassifyReply
// (RequestReadyClassified in errors.go) — kept in lockstep with the other
// request families so a future tweak doesn't drift between them.
//
// It retries only on TRANSPORT failure (no reply received: no-responders or
// probe timeout). Any received reply — including a handler-error reply — stops
// the loop, because a reply means the responder is UP; the readiness contract is
// satisfied and the (possibly-error) reply is returned for classification. Only
// a truly-silent (never-replies) responder is retried, and only until the budget
// is spent.
func (c *Client) requestMsgReady(
	ctx context.Context, subject string, data []byte, probeTimeout, budget time.Duration,
) (*nats.Msg, error) {
	c.mu.RLock()
	conn := c.conn
	c.mu.RUnlock()

	if conn == nil || !conn.IsConnected() {
		return nil, ErrNotConnected
	}
	if c.Status() == StatusCircuitOpen {
		return nil, ErrCircuitOpen
	}

	if probeTimeout <= 0 {
		probeTimeout = DefaultReadinessProbeTimeout
	}
	if budget <= 0 {
		budget = DefaultReadinessBudget
	}

	// Auto-generate trace once for all attempts.
	if _, ok := TraceContextFromContext(ctx); !ok {
		ctx = ContextWithTrace(ctx, NewTraceContext())
	}

	overallCtx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()

	backoff := readinessInitialBackoff
	var lastErr error
	for {
		attemptCtx, attemptCancel := context.WithTimeout(overallCtx, probeTimeout)
		msg := nats.NewMsg(subject)
		msg.Data = data
		InjectTrace(ctx, msg)
		reply, err := conn.RequestMsgWithContext(attemptCtx, msg)
		attemptCancel()

		if err == nil {
			c.resetCircuit()
			return reply, nil
		}
		lastErr = err
		// Deliberately do NOT recordFailure() here. A readiness miss (no-responder
		// / probe timeout) is the EXPECTED condition this primitive tolerates — the
		// responder isn't up yet, not a connection fault. Counting it would let a
		// single boot-time RequestReady burst (against briefly-absent responders)
		// trip the shared per-client circuit breaker and fast-fail UNRELATED calls
		// with ErrCircuitOpen. Genuine connection loss still surfaces via the
		// up-front IsConnected check and per-attempt errors, and stays bounded by
		// the budget.

		// Wait before the next probe, bounded by the overall readiness budget.
		select {
		case <-overallCtx.Done():
			// %w preserves errors.Is(…, nats.ErrNoResponders) for callers that
			// want to distinguish a never-appeared responder from a hung one.
			return nil, fmt.Errorf("readiness budget %s exhausted: %w", budget, lastErr)
		case <-time.After(backoff):
		}
		if backoff *= 2; backoff > readinessMaxBackoff {
			backoff = readinessMaxBackoff
		}
	}
}

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

		// Create per-message context with timeout. Configurable (default
		// DefaultRequestHandlerTimeout=30s) so slow-by-design handlers such as
		// the LLM answer-synthesis path can raise it per deployment without
		// changing the framework default. See resolveRequestHandlerTimeoutFromEnv.
		msgCtx, cancel := context.WithTimeout(msgCtx, c.requestHandlerTimeout)
		defer cancel()

		// Call the handler
		response, err := handler(msgCtx, msg.Data)
		if err != nil {
			// Send a header-classified error reply (ADR-060). The reply carries
			// X-Status / X-Error-Class / X-Error-Code headers and the
			// {message, detail} envelope body; a RequestClassified caller
			// reconstructs the typed error. RespondError returns
			// errMissingReplySubject when the inbound message had no reply
			// subject — that's expected for fire-and-forget patterns, so
			// swallow. Any other publish failure is operator-visible (queue
			// full, connection torn down mid-reply, etc.) and needs to surface,
			// not silently swallow.
			if respErr := RespondError(msg, err); respErr != nil && !errors.Is(respErr, errMissingReplySubject) {
				c.logger.Error("natsclient: failed to publish handler-error reply",
					"subject", subject,
					"handler_err", err.Error(),
					"publish_err", respErr.Error())
			}
			return
		}

		// Send successful response
		if msg.Reply != "" {
			if respErr := msg.Respond(response); respErr != nil {
				c.logger.Error("natsclient: failed to publish handler success reply",
					"subject", subject,
					"err", respErr.Error())
			}
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
//
// Footgun: when the responder returns a Go error, SubscribeForRequests
// wire-encodes the failure as a legacy "error: <msg>" text body with
// nil err. This method returns reply.Data on transport success without
// running it through ClassifyReply, so callers that json.Unmarshal the
// body silently corrupt on handler errors. For mutation paths that
// want both retry AND classified error handling, use
// RequestWithRetryClassified (closes the matrix gap filed as gh#192).
func (c *Client) RequestWithRetry(
	ctx context.Context,
	subject string,
	data []byte,
	timeout time.Duration,
	retry RetryConfig,
) ([]byte, error) {
	msg, err := c.requestMsgWithRetry(ctx, subject, data, timeout, retry)
	if err != nil {
		return nil, err
	}
	return msg.Data, nil
}

// requestMsgWithRetry is the shared retry-loop helper. Returns the
// raw *nats.Msg so callers can either extract .Data (RequestWithRetry)
// or run the message through ClassifyReply (RequestWithRetryClassified
// in errors.go). Keeps the two retry-aware request methods in lockstep
// so a future retry-logic tweak doesn't silently drift between them.
func (c *Client) requestMsgWithRetry(
	ctx context.Context,
	subject string,
	data []byte,
	timeout time.Duration,
	retry RetryConfig,
) (*nats.Msg, error) {
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
			return reply, nil
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
