// Package agenticmodel provides Prometheus metrics for agentic-model component.
package agenticmodel

import (
	"sync"

	"github.com/c360studio/semstreams/metric"
	"github.com/prometheus/client_golang/prometheus"
)

// modelMetrics holds Prometheus metrics for the agentic-model component.
type modelMetrics struct {
	// Requests
	requestsTotal    *prometheus.CounterVec
	requestDuration  *prometheus.HistogramVec
	requestsInFlight *prometheus.GaugeVec

	// Errors
	errorsTotal *prometheus.CounterVec

	// Response characteristics
	toolCallsReturned *prometheus.HistogramVec

	// Token usage
	tokensTotal *prometheus.CounterVec

	// Streaming
	streamChunksTotal *prometheus.CounterVec
	streamTTFT        *prometheus.HistogramVec

	// Rate limiting
	rateLimitHits    *prometheus.CounterVec
	rateLimitRetries *prometheus.CounterVec

	// Truncation
	lengthTruncationsTotal *prometheus.CounterVec

	// Endpoint health (circuit-breaker observability)
	endpointHealthState *prometheus.GaugeVec
}

// Package-level metrics (registered once to avoid duplicate registration errors)
var (
	metricsOnce sync.Once
	metrics     *modelMetrics
)

// getMetrics returns the singleton metrics instance, creating and registering it if needed.
func getMetrics(registry *metric.MetricsRegistry) *modelMetrics {
	metricsOnce.Do(func() {
		metrics = &modelMetrics{
			requestsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
				Namespace: "semstreams",
				Subsystem: "agentic_model",
				Name:      "requests_total",
				Help:      "Total model requests by model and status",
			}, []string{"model", "status"}),

			requestDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
				Namespace: "semstreams",
				Subsystem: "agentic_model",
				Name:      "request_duration_seconds",
				Help:      "Model request latency in seconds",
				Buckets:   prometheus.ExponentialBuckets(0.1, 2, 10), // 0.1s to ~100s
			}, []string{"model"}),

			requestsInFlight: prometheus.NewGaugeVec(prometheus.GaugeOpts{
				Namespace: "semstreams",
				Subsystem: "agentic_model",
				Name:      "requests_in_flight",
				Help:      "Number of model requests currently in flight",
			}, []string{"model"}),

			errorsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
				Namespace: "semstreams",
				Subsystem: "agentic_model",
				Name:      "errors_total",
				Help:      "Total model errors by model and error type",
			}, []string{"model", "error_type"}),

			toolCallsReturned: prometheus.NewHistogramVec(prometheus.HistogramOpts{
				Namespace: "semstreams",
				Subsystem: "agentic_model",
				Name:      "tool_calls_returned",
				Help:      "Distribution of tool calls per response",
				Buckets:   []float64{0, 1, 2, 3, 5, 10},
			}, []string{"model"}),

			tokensTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
				Namespace: "semstreams",
				Subsystem: "agentic_model",
				Name:      "tokens_total",
				Help:      "Total tokens used by model and type (prompt/completion)",
			}, []string{"model", "type"}),

			streamChunksTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
				Namespace: "semstreams",
				Subsystem: "agentic_model",
				Name:      "stream_chunks_total",
				Help:      "Total streaming chunks received by model",
			}, []string{"model"}),

			streamTTFT: prometheus.NewHistogramVec(prometheus.HistogramOpts{
				Namespace: "semstreams",
				Subsystem: "agentic_model",
				Name:      "stream_ttft_seconds",
				Help:      "Time-to-first-token for streaming requests",
				Buckets:   prometheus.ExponentialBuckets(0.01, 2, 12), // 10ms to ~40s
			}, []string{"model"}),

			rateLimitHits: prometheus.NewCounterVec(prometheus.CounterOpts{
				Namespace: "semstreams",
				Subsystem: "agentic_model",
				Name:      "rate_limit_hits_total",
				Help:      "Total HTTP 429 rate-limit responses received by model",
			}, []string{"model"}),

			rateLimitRetries: prometheus.NewCounterVec(prometheus.CounterOpts{
				Namespace: "semstreams",
				Subsystem: "agentic_model",
				Name:      "rate_limit_retries_total",
				Help:      "Total retry attempts after 429 rate-limit responses by model",
			}, []string{"model"}),

			lengthTruncationsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
				Namespace: "semstreams",
				Subsystem: "agentic_model",
				Name:      "length_truncations_total",
				Help:      "Total responses truncated due to finish_reason=length (max_tokens hit)",
			}, []string{"model"}),

			endpointHealthState: prometheus.NewGaugeVec(prometheus.GaugeOpts{
				Namespace: "semstreams",
				Subsystem: "agentic_model",
				Name:      "endpoint_health_state",
				Help:      "Circuit-breaker state for each endpoint (1 if endpoint is in this state, 0 otherwise). state label: closed, open, half_open.",
			}, []string{"endpoint", "state"}),
		}

		// Register metrics with the metrics registry if available
		if registry != nil {
			_ = registry.RegisterCounterVec("agentic-model", "requests_total", metrics.requestsTotal)
			_ = registry.RegisterHistogramVec("agentic-model", "request_duration_seconds", metrics.requestDuration)
			_ = registry.RegisterGaugeVec("agentic-model", "requests_in_flight", metrics.requestsInFlight)
			_ = registry.RegisterCounterVec("agentic-model", "errors_total", metrics.errorsTotal)
			_ = registry.RegisterHistogramVec("agentic-model", "tool_calls_returned", metrics.toolCallsReturned)
			_ = registry.RegisterCounterVec("agentic-model", "tokens_total", metrics.tokensTotal)
			_ = registry.RegisterCounterVec("agentic-model", "stream_chunks_total", metrics.streamChunksTotal)
			_ = registry.RegisterHistogramVec("agentic-model", "stream_ttft_seconds", metrics.streamTTFT)
			_ = registry.RegisterCounterVec("agentic-model", "rate_limit_hits_total", metrics.rateLimitHits)
			_ = registry.RegisterCounterVec("agentic-model", "rate_limit_retries_total", metrics.rateLimitRetries)
			_ = registry.RegisterCounterVec("agentic-model", "length_truncations_total", metrics.lengthTruncationsTotal)
			_ = registry.RegisterGaugeVec("agentic-model", "endpoint_health_state", metrics.endpointHealthState)
		} else {
			// Fallback to default prometheus registry for testing
			_ = prometheus.DefaultRegisterer.Register(metrics.requestsTotal)
			_ = prometheus.DefaultRegisterer.Register(metrics.requestDuration)
			_ = prometheus.DefaultRegisterer.Register(metrics.requestsInFlight)
			_ = prometheus.DefaultRegisterer.Register(metrics.errorsTotal)
			_ = prometheus.DefaultRegisterer.Register(metrics.toolCallsReturned)
			_ = prometheus.DefaultRegisterer.Register(metrics.tokensTotal)
			_ = prometheus.DefaultRegisterer.Register(metrics.streamChunksTotal)
			_ = prometheus.DefaultRegisterer.Register(metrics.streamTTFT)
			_ = prometheus.DefaultRegisterer.Register(metrics.rateLimitHits)
			_ = prometheus.DefaultRegisterer.Register(metrics.rateLimitRetries)
			_ = prometheus.DefaultRegisterer.Register(metrics.lengthTruncationsTotal)
			_ = prometheus.DefaultRegisterer.Register(metrics.endpointHealthState)
		}
	})
	return metrics
}

// recordEndpointHealthState sets the gauge for one endpoint to indicate
// its current circuit-breaker state. Sets the matching label to 1 and
// the other two states to 0 so dashboards can sum across the state
// label and rely on the result equaling 1.
//
// Called from the request path each time recordHealthResult fires so
// the gauge converges on the latest state without a separate poller.
func (m *modelMetrics) recordEndpointHealthState(endpoint, state string) {
	for _, s := range []string{"closed", "open", "half_open"} {
		v := 0.0
		if s == state {
			v = 1.0
		}
		m.endpointHealthState.WithLabelValues(endpoint, s).Set(v)
	}
}

// recordRequestStart records the start of a model request.
func (m *modelMetrics) recordRequestStart(model string) {
	m.requestsInFlight.WithLabelValues(model).Inc()
}

// recordRequestComplete records a successful model request completion.
func (m *modelMetrics) recordRequestComplete(model string, durationSeconds float64, toolCalls int) {
	m.requestsInFlight.WithLabelValues(model).Dec()
	m.requestsTotal.WithLabelValues(model, "success").Inc()
	m.requestDuration.WithLabelValues(model).Observe(durationSeconds)
	m.toolCallsReturned.WithLabelValues(model).Observe(float64(toolCalls))
}

// recordRequestError records a failed model request.
func (m *modelMetrics) recordRequestError(model, errorType string, durationSeconds float64) {
	m.requestsInFlight.WithLabelValues(model).Dec()
	m.requestsTotal.WithLabelValues(model, "error").Inc()
	m.requestDuration.WithLabelValues(model).Observe(durationSeconds)
	m.errorsTotal.WithLabelValues(model, errorType).Inc()
}

// recordTokenUsage records token usage for a request.
func (m *modelMetrics) recordTokenUsage(model string, promptTokens, completionTokens int) {
	if promptTokens > 0 {
		m.tokensTotal.WithLabelValues(model, "prompt").Add(float64(promptTokens))
	}
	if completionTokens > 0 {
		m.tokensTotal.WithLabelValues(model, "completion").Add(float64(completionTokens))
	}
}

// recordStreamChunk increments the streaming chunk counter.
func (m *modelMetrics) recordStreamChunk(model string) {
	m.streamChunksTotal.WithLabelValues(model).Inc()
}

// recordStreamTTFT records time-to-first-token for a streaming request.
func (m *modelMetrics) recordStreamTTFT(model string, seconds float64) {
	m.streamTTFT.WithLabelValues(model).Observe(seconds)
}

// recordRateLimitHit increments the rate-limit hit counter for the given model.
func (m *modelMetrics) recordRateLimitHit(model string) {
	m.rateLimitHits.WithLabelValues(model).Inc()
}

// recordRateLimitRetry increments the rate-limit retry counter for the given model.
func (m *modelMetrics) recordRateLimitRetry(model string) {
	m.rateLimitRetries.WithLabelValues(model).Inc()
}

// recordLengthTruncation increments the length truncation counter for the given model.
func (m *modelMetrics) recordLengthTruncation(model string) {
	m.lengthTruncationsTotal.WithLabelValues(model).Inc()
}
