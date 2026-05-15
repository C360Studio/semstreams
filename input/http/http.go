// Package http implements the REST polling input component.
// See doc.go for the full operator guide.
package http

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	nethttp "net/http"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/metric"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
)

// maxResponseBytes caps the response body we will buffer to keep a
// misbehaving endpoint from exhausting memory. Set high enough to
// support typical telemetry payloads while still bounded.
const maxResponseBytes = 32 * 1024 * 1024 // 32 MiB

// publisher abstracts the NATS publish path so tests can inject a
// stub. Production wiring uses natsPublisher which composes core
// NATS / JetStream selection via the resolved port type.
type publisher interface {
	Publish(ctx context.Context, subject string, data []byte) error
}

// natsPublisher is the production publisher. It chooses between
// JetStream and core NATS based on the resolved port type.
type natsPublisher struct {
	client       *natsclient.Client
	useJetStream bool
}

// Publish routes through JetStream or core NATS.
func (p *natsPublisher) Publish(ctx context.Context, subject string, data []byte) error {
	if p.useJetStream {
		if err := p.client.PublishToStream(ctx, subject, data); err != nil {
			return errs.WrapTransient(err, "http-input", "Publish", "JetStream publish")
		}
		return nil
	}
	nc := p.client.GetConnection()
	if nc == nil {
		return errs.WrapTransient(errs.ErrNoConnection, "http-input", "Publish", "no NATS connection")
	}
	if err := nc.Publish(subject, data); err != nil {
		return errs.WrapTransient(err, "http-input", "Publish", "NATS publish")
	}
	return nil
}

// Input implements an HTTP polling input.
type Input struct {
	name     string
	config   Config
	resolved resolved
	client   *nethttp.Client

	publisher publisher
	logger    *slog.Logger
	metrics   *metrics

	// Lifecycle
	lifecycleMu sync.Mutex
	shutdown    chan struct{}
	done        chan struct{}
	running     atomic.Bool
	startTime   time.Time
	wg          sync.WaitGroup

	// Counters (atomic for thread safety; exposed via Metric()).
	requestsTotal     atomic.Int64
	requestsFailed    atomic.Int64
	messagesPublished atomic.Int64
	errCount          atomic.Int64
}

// Compile-time interface assertions.
var (
	_ component.Discoverable       = (*Input)(nil)
	_ component.LifecycleComponent = (*Input)(nil)
)

// httpInputSchema is the registry-visible schema generated from
// Config. Built once at package load.
var httpInputSchema = component.GenerateConfigSchema(reflect.TypeOf(Config{}))

// InputDeps holds runtime dependencies for constructing an Input.
type InputDeps struct {
	Name            string
	Config          Config
	NATSClient      *natsclient.Client
	MetricsRegistry *metric.MetricsRegistry
	Logger          *slog.Logger
}

// NewInput constructs a new HTTP polling input from the given
// dependencies. Caller is expected to have called Config.Validate
// already; the constructor does not re-validate.
func NewInput(deps InputDeps) *Input {
	resolved := deps.Config.resolve()
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default().With("component", "http-input", "url", resolved.url)
	}

	return &Input{
		name:     deps.Name,
		config:   deps.Config,
		resolved: resolved,
		client:   &nethttp.Client{Timeout: resolved.timeout},
		publisher: &natsPublisher{
			client:       deps.NATSClient,
			useJetStream: resolved.useJetStream,
		},
		logger:    logger,
		metrics:   newMetrics(deps.MetricsRegistry, deps.Name),
		startTime: time.Now(),
	}
}

// Meta returns the component metadata.
func (h *Input) Meta() component.Metadata {
	name := h.name
	if name == "" {
		name = "http-input"
	}
	return component.Metadata{
		Name:        name,
		Type:        "input",
		Description: fmt.Sprintf("HTTP REST polling from %s publishing to %s", h.resolved.url, h.resolved.subject),
		Version:     "1.0.0",
	}
}

// InputPorts returns this component's input ports.
func (h *Input) InputPorts() []component.Port {
	return []component.Port{
		{
			Name:        "http_source",
			Direction:   component.DirectionInput,
			Required:    true,
			Description: fmt.Sprintf("HTTP endpoint: %s", h.resolved.url),
		},
	}
}

// OutputPorts returns this component's output ports.
func (h *Input) OutputPorts() []component.Port {
	portType := "nats"
	if h.resolved.useJetStream {
		portType = "jetstream"
	}
	return []component.Port{
		{
			Name:        portType + "_output",
			Direction:   component.DirectionOutput,
			Required:    true,
			Description: "NATS subject for publishing decoded HTTP responses",
			Config: component.NATSPort{
				Subject: h.resolved.subject,
			},
		},
	}
}

// ConfigSchema returns the component config schema.
func (h *Input) ConfigSchema() component.ConfigSchema {
	return httpInputSchema
}

// Health returns the current health status.
func (h *Input) Health() component.HealthStatus {
	return component.HealthStatus{
		Healthy:    h.running.Load(),
		LastCheck:  time.Now(),
		ErrorCount: int(h.errCount.Load()),
		Uptime:     time.Since(h.startTime),
	}
}

// DataFlow returns current data-flow metrics for the input.
func (h *Input) DataFlow() component.FlowMetrics {
	published := h.messagesPublished.Load()
	errCount := h.errCount.Load()
	uptime := time.Since(h.startTime).Seconds()
	var msgsPerSec float64
	if uptime > 0 {
		msgsPerSec = float64(published) / uptime
	}
	var errorRate float64
	if published > 0 {
		errorRate = float64(errCount) / float64(published)
	}
	return component.FlowMetrics{
		MessagesPerSecond: msgsPerSec,
		ErrorRate:         errorRate,
		LastActivity:      time.Now(),
	}
}

// Initialize prepares the http input component. Currently a no-op
// past Validate; reserved for future DNS pre-warm or auth probe
// hooks.
func (h *Input) Initialize() error {
	return nil
}

// Start launches the polling loop. Safe to call multiple times;
// subsequent calls are no-ops until Stop has been called.
func (h *Input) Start(ctx context.Context) error {
	if ctx == nil {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "Input", "Start", "context cannot be nil")
	}
	if err := ctx.Err(); err != nil {
		return errs.WrapInvalid(err, "Input", "Start", "context already cancelled")
	}

	h.lifecycleMu.Lock()
	defer h.lifecycleMu.Unlock()

	if h.running.Load() {
		return nil
	}

	h.shutdown = make(chan struct{})
	h.done = make(chan struct{})
	h.running.Store(true)
	h.wg.Add(1)

	go h.pollLoop(ctx)

	h.logger.Info("HTTP input started",
		slog.String("url", h.resolved.url),
		slog.String("subject", h.resolved.subject),
		slog.String("interval", h.resolved.interval.String()),
		slog.String("decoder", h.resolved.decoderMode))
	return nil
}

// Stop signals shutdown and waits up to timeout for the polling
// loop to exit.
func (h *Input) Stop(timeout time.Duration) error {
	h.lifecycleMu.Lock()
	if !h.running.Load() {
		h.lifecycleMu.Unlock()
		return nil
	}
	h.running.Store(false)
	close(h.shutdown)
	h.lifecycleMu.Unlock()

	done := make(chan struct{})
	go func() {
		h.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		h.logger.Info("HTTP input stopped")
	case <-time.After(timeout):
		h.logger.Warn("HTTP input stop timed out")
	}
	return nil
}

// pollLoop runs the request-decode-publish cycle on the configured
// interval until shutdown.
func (h *Input) pollLoop(ctx context.Context) {
	defer h.wg.Done()
	defer close(h.done)

	// Fire one cycle immediately so operators don't wait
	// `interval` for the first observation.
	h.runCycle(ctx)

	ticker := time.NewTicker(h.resolved.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-h.shutdown:
			return
		case <-ticker.C:
			h.runCycle(ctx)
		}
	}
}

// runCycle issues a single poll: doRequest with retry, decode the
// response, publish each decoded message. Errors are logged and
// counted; the cycle does not propagate them up because the
// polling loop is the durable owner of recovery semantics.
func (h *Input) runCycle(ctx context.Context) {
	body, err := h.doRequest(ctx)
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			h.logger.Warn("HTTP request failed", slog.Any("error", err))
		}
		h.errCount.Add(1)
		if h.metrics != nil {
			h.metrics.requestsFailed.Inc()
		}
		return
	}
	if len(body) == 0 {
		return
	}
	if err := h.decodeAndPublish(ctx, body); err != nil {
		h.logger.Warn("decode or publish failed", slog.Any("error", err))
		h.errCount.Add(1)
		if h.metrics != nil {
			h.metrics.parseErrors.Inc()
		}
	}
}

// doRequest performs the configured HTTP request with exponential
// backoff retry. Returns the response body bytes on success.
// Network errors and 5xx responses are retried; 4xx responses are
// returned as errors without retry.
func (h *Input) doRequest(ctx context.Context) ([]byte, error) {
	var (
		body    []byte
		lastErr error
	)
	backoff := h.resolved.initialBackoff
	for attempt := 1; attempt <= h.resolved.maxAttempts; attempt++ {
		if attempt > 1 {
			if h.metrics != nil {
				h.metrics.requestsRetried.Inc()
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-h.shutdown:
				return nil, errors.New("http-input: shutdown during retry backoff")
			case <-time.After(backoff):
			}
			backoff = nextBackoff(backoff, h.resolved.maxBackoff, h.resolved.multiplier)
		}

		h.requestsTotal.Add(1)
		if h.metrics != nil {
			h.metrics.requestsTotal.Inc()
		}

		body, lastErr = h.attempt(ctx)
		if lastErr == nil {
			return body, nil
		}
		// Distinguish retryable vs terminal failures.
		if !isRetryable(lastErr) {
			return nil, lastErr
		}
	}
	return nil, fmt.Errorf("http-input: %d attempts exhausted: %w", h.resolved.maxAttempts, lastErr)
}

// attempt issues a single HTTP request, reads the body up to
// maxResponseBytes, and classifies the outcome.
func (h *Input) attempt(ctx context.Context) ([]byte, error) {
	var bodyReader io.Reader
	if len(h.resolved.body) > 0 {
		bodyReader = bytes.NewReader(h.resolved.body)
	}
	req, err := nethttp.NewRequestWithContext(ctx, h.resolved.method, h.resolved.url, bodyReader)
	if err != nil {
		return nil, &terminalError{err: fmt.Errorf("build request: %w", err)}
	}
	for k, v := range h.resolved.headers {
		req.Header.Set(k, v)
	}
	switch h.resolved.auth.Type {
	case AuthBearer:
		req.Header.Set("Authorization", "Bearer "+h.resolved.auth.Token)
	case AuthBasic:
		creds := h.resolved.auth.Username + ":" + h.resolved.auth.Password
		req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(creds)))
	}

	resp, err := h.client.Do(req)
	if err != nil {
		// Network-level error — retryable.
		return nil, &retryableError{err: err}
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 500 && resp.StatusCode < 600 {
		return nil, &retryableError{err: fmt.Errorf("server returned %d", resp.StatusCode)}
	}
	if resp.StatusCode >= 400 {
		return nil, &terminalError{err: fmt.Errorf("client error %d (%s)", resp.StatusCode, resp.Status)}
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return nil, &retryableError{err: fmt.Errorf("read body: %w", err)}
	}
	if len(body) > maxResponseBytes {
		// Drain the remainder so keep-alive can reuse the
		// connection on the next poll rather than tearing down
		// and reopening on every oversized response.
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil, &terminalError{err: fmt.Errorf("response body exceeds %d bytes", maxResponseBytes)}
	}
	return body, nil
}

// decodeAndPublish dispatches on the configured decoder mode and
// publishes each resulting message as a BaseMessage envelope.
func (h *Input) decodeAndPublish(ctx context.Context, body []byte) error {
	switch h.resolved.decoderMode {
	case DecoderJSONL:
		return h.publishJSONL(ctx, body)
	default: // DecoderJSON
		return h.publishJSON(ctx, body)
	}
}

// publishJSON treats the response body as one JSON object,
// publishes one envelope.
func (h *Input) publishJSON(ctx context.Context, body []byte) error {
	var data map[string]any
	if err := json.Unmarshal(body, &data); err != nil {
		return fmt.Errorf("json decode: %w", err)
	}
	if h.metrics != nil {
		h.metrics.messagesParsed.Inc()
	}
	return h.publishEnvelope(ctx, data)
}

// publishJSONL splits the body by newline and publishes one
// envelope per non-empty line. Parse errors on individual lines
// are counted but do not abort the cycle.
func (h *Input) publishJSONL(ctx context.Context, body []byte) error {
	scanner := bufio.NewScanner(bytes.NewReader(body))
	scanner.Buffer(make([]byte, 64*1024), maxResponseBytes)
	var firstErr error
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var data map[string]any
		if err := json.Unmarshal(line, &data); err != nil {
			h.logger.Debug("jsonl: skipping malformed line", slog.Any("error", err))
			if h.metrics != nil {
				h.metrics.parseErrors.Inc()
			}
			if firstErr == nil {
				firstErr = fmt.Errorf("jsonl decode: %w", err)
			}
			continue
		}
		if h.metrics != nil {
			h.metrics.messagesParsed.Inc()
		}
		if err := h.publishEnvelope(ctx, data); err != nil {
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("jsonl scan: %w", err)
	}
	// firstErr is non-nil only if every line failed *or* an early
	// line failed before a later success. The cycle still
	// published valid lines; return firstErr so the caller logs it
	// without surfacing as a complete-cycle failure.
	return firstErr
}

// publishEnvelope wraps the decoded data in a GenericJSONPayload
// BaseMessage and publishes it via the configured NATS path.
// Required by the payload-registry discipline: every NATS publish
// is decodeable through message.NewDecoder(reg).
func (h *Input) publishEnvelope(ctx context.Context, data map[string]any) error {
	payload := message.NewGenericJSON(data)
	envelope := message.NewBaseMessage(payload.Schema(), payload, h.envelopeSource())
	wire, err := json.Marshal(envelope)
	if err != nil {
		return fmt.Errorf("marshal envelope: %w", err)
	}
	return h.publishBytes(ctx, wire)
}

// envelopeSource returns the BaseMessage Source string. Encodes
// component-name + URL so downstream consumers can attribute
// observations to a specific input instance.
func (h *Input) envelopeSource() string {
	if h.name == "" {
		return "http-input"
	}
	return "http-input:" + h.name
}

// publishBytes hands off the marshaled envelope to the configured
// publisher. The publisher's implementation owns the JetStream vs
// core NATS dispatch.
func (h *Input) publishBytes(ctx context.Context, data []byte) error {
	if err := h.publisher.Publish(ctx, h.resolved.subject, data); err != nil {
		return err
	}
	h.messagesPublished.Add(1)
	if h.metrics != nil {
		h.metrics.messagesPublished.Inc()
	}
	return nil
}

// retryableError marks a failure mode worth retrying (network
// error, 5xx). Unwrapped error preserves the cause for callers
// that want to inspect via errors.Is / errors.As.
type retryableError struct{ err error }

func (e *retryableError) Error() string { return e.err.Error() }
func (e *retryableError) Unwrap() error { return e.err }

// terminalError marks a failure that should not be retried (4xx,
// build-request failure, response-too-large).
type terminalError struct{ err error }

func (e *terminalError) Error() string { return e.err.Error() }
func (e *terminalError) Unwrap() error { return e.err }

func isRetryable(err error) bool {
	var r *retryableError
	return errors.As(err, &r)
}

// nextBackoff applies the exponential multiplier and caps at max.
func nextBackoff(current, maxBackoff time.Duration, multiplier float64) time.Duration {
	next := time.Duration(float64(current) * multiplier)
	if next > maxBackoff {
		return maxBackoff
	}
	return next
}
