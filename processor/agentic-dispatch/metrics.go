// Package agenticdispatch provides Prometheus metrics for agentic-dispatch component.
package agenticdispatch

import (
	"sync"

	"github.com/c360studio/semstreams/metric"
	"github.com/prometheus/client_golang/prometheus"
)

// routerMetrics holds Prometheus metrics for the router component.
type routerMetrics struct {
	messagesReceived    *prometheus.CounterVec
	commandsExecuted    *prometheus.CounterVec
	tasksSubmitted      prometheus.Counter
	activeLoops         prometheus.Gauge
	routingDuration     prometheus.Histogram
	completionsReceived *prometheus.CounterVec

	// HTTP endpoint metrics
	httpRequestsTotal   *prometheus.CounterVec
	httpRequestDuration *prometheus.HistogramVec

	// Loop signal metrics
	loopSignalsSent *prometheus.CounterVec

	// Loop approval metrics — counts approval submissions through the
	// HTTP /loops/{id}/approval endpoint by decision and outcome.
	loopApprovalsSubmitted *prometheus.CounterVec

	// SSE metrics
	sseConnectionsActive prometheus.Gauge
	sseEventsTotal       *prometheus.CounterVec
	sseErrorsTotal       *prometheus.CounterVec
}

// Package-level metrics cache (keyed by registry to allow test isolation)
var (
	metricsMu    sync.Mutex
	metricsCache = make(map[*metric.MetricsRegistry]*routerMetrics)
	nilMetrics   *routerMetrics
	nilOnce      sync.Once
)

// getMetrics returns the metrics instance for the given registry, creating and registering it if needed.
func getMetrics(registry *metric.MetricsRegistry) *routerMetrics {
	// Special handling for nil registry (production use with default Prometheus registry)
	if registry == nil {
		nilOnce.Do(func() {
			nilMetrics = createAndRegisterMetrics(nil)
		})
		return nilMetrics
	}

	// For non-nil registries, create per-registry instances (test isolation)
	metricsMu.Lock()
	defer metricsMu.Unlock()

	if m, exists := metricsCache[registry]; exists {
		return m
	}

	m := createAndRegisterMetrics(registry)
	metricsCache[registry] = m
	return m
}

// createAndRegisterMetrics creates a new metrics instance and registers it.
func createAndRegisterMetrics(registry *metric.MetricsRegistry) *routerMetrics {
	m := &routerMetrics{
		messagesReceived: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "semstreams",
			Subsystem: "router",
			Name:      "messages_received_total",
			Help:      "Total number of user messages received by channel type",
		}, []string{"channel_type"}),

		commandsExecuted: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "semstreams",
			Subsystem: "router",
			Name:      "commands_executed_total",
			Help:      "Total number of commands executed",
		}, []string{"command"}),

		tasksSubmitted: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "semstreams",
			Subsystem: "router",
			Name:      "tasks_submitted_total",
			Help:      "Total number of tasks submitted to agentic loops",
		}),

		activeLoops: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "semstreams",
			Subsystem: "router",
			Name:      "active_loops",
			Help:      "Number of currently active agentic loops",
		}),

		routingDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: "semstreams",
			Subsystem: "router",
			Name:      "routing_duration_seconds",
			Help:      "Duration of message routing operations in seconds",
			Buckets:   prometheus.ExponentialBuckets(0.001, 2, 10), // 1ms to ~1s
		}),

		completionsReceived: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "semstreams",
			Subsystem: "router",
			Name:      "completions_received_total",
			Help:      "Total number of agent completions received by status",
		}, []string{"status"}),

		// HTTP endpoint metrics
		httpRequestsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "semstreams",
			Subsystem: "router",
			Name:      "http_requests_total",
			Help:      "Total number of HTTP requests by endpoint and status",
		}, []string{"endpoint", "method", "status"}),

		httpRequestDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "semstreams",
			Subsystem: "router",
			Name:      "http_request_duration_seconds",
			Help:      "Duration of HTTP requests in seconds",
			Buckets:   prometheus.ExponentialBuckets(0.001, 2, 10), // 1ms to ~1s
		}, []string{"endpoint", "method"}),

		// Loop signal metrics
		loopSignalsSent: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "semstreams",
			Subsystem: "router",
			Name:      "loop_signals_sent_total",
			Help:      "Total number of loop control signals sent",
		}, []string{"signal_type", "accepted"}),

		// Loop approval metrics
		loopApprovalsSubmitted: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "semstreams",
			Subsystem: "router",
			Name:      "loop_approvals_submitted_total",
			Help:      "Total number of approval responses submitted via HTTP, labelled by decision and submission outcome (status: success/error)",
		}, []string{"decision", "status"}),

		// SSE metrics
		sseConnectionsActive: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "semstreams",
			Subsystem: "router",
			Name:      "sse_connections_active",
			Help:      "Number of active SSE connections",
		}),

		sseEventsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "semstreams",
			Subsystem: "router",
			Name:      "sse_events_total",
			Help:      "Total number of SSE events sent by type",
		}, []string{"event_type"}),

		sseErrorsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "semstreams",
			Subsystem: "router",
			Name:      "sse_errors_total",
			Help:      "Total number of SSE errors by type",
		}, []string{"error_type"}),
	}

	// Register metrics with the metrics registry if available
	if registry != nil {
		_ = registry.RegisterCounterVec("router", "messages_received_total", m.messagesReceived)
		_ = registry.RegisterCounterVec("router", "commands_executed_total", m.commandsExecuted)
		_ = registry.RegisterCounter("router", "tasks_submitted_total", m.tasksSubmitted)
		_ = registry.RegisterGauge("router", "active_loops", m.activeLoops)
		_ = registry.RegisterHistogram("router", "routing_duration_seconds", m.routingDuration)
		_ = registry.RegisterCounterVec("router", "completions_received_total", m.completionsReceived)
		_ = registry.RegisterCounterVec("router", "http_requests_total", m.httpRequestsTotal)
		_ = registry.RegisterHistogramVec("router", "http_request_duration_seconds", m.httpRequestDuration)
		_ = registry.RegisterCounterVec("router", "loop_signals_sent_total", m.loopSignalsSent)
		_ = registry.RegisterCounterVec("router", "loop_approvals_submitted_total", m.loopApprovalsSubmitted)
		_ = registry.RegisterGauge("router", "sse_connections_active", m.sseConnectionsActive)
		_ = registry.RegisterCounterVec("router", "sse_events_total", m.sseEventsTotal)
		_ = registry.RegisterCounterVec("router", "sse_errors_total", m.sseErrorsTotal)
	} else {
		// Fallback to default prometheus registry for production
		_ = prometheus.DefaultRegisterer.Register(m.messagesReceived)
		_ = prometheus.DefaultRegisterer.Register(m.commandsExecuted)
		_ = prometheus.DefaultRegisterer.Register(m.tasksSubmitted)
		_ = prometheus.DefaultRegisterer.Register(m.activeLoops)
		_ = prometheus.DefaultRegisterer.Register(m.routingDuration)
		_ = prometheus.DefaultRegisterer.Register(m.completionsReceived)
		_ = prometheus.DefaultRegisterer.Register(m.httpRequestsTotal)
		_ = prometheus.DefaultRegisterer.Register(m.httpRequestDuration)
		_ = prometheus.DefaultRegisterer.Register(m.loopSignalsSent)
		_ = prometheus.DefaultRegisterer.Register(m.loopApprovalsSubmitted)
		_ = prometheus.DefaultRegisterer.Register(m.sseConnectionsActive)
		_ = prometheus.DefaultRegisterer.Register(m.sseEventsTotal)
		_ = prometheus.DefaultRegisterer.Register(m.sseErrorsTotal)
	}

	return m
}

// recordMessageReceived increments the messages received counter for a channel type.
func (m *routerMetrics) recordMessageReceived(channelType string) {
	m.messagesReceived.WithLabelValues(channelType).Inc()
}

// recordCommandExecuted increments the commands executed counter for a command.
func (m *routerMetrics) recordCommandExecuted(command string) {
	m.commandsExecuted.WithLabelValues(command).Inc()
}

// recordTaskSubmitted increments the tasks submitted counter.
func (m *routerMetrics) recordTaskSubmitted() {
	m.tasksSubmitted.Inc()
}

// recordLoopStarted increments the active loops gauge.
func (m *routerMetrics) recordLoopStarted() {
	m.activeLoops.Inc()
}

// recordLoopEnded decrements the active loops gauge.
func (m *routerMetrics) recordLoopEnded() {
	m.activeLoops.Dec()
}

// recordRoutingDuration records the duration of a routing operation.
func (m *routerMetrics) recordRoutingDuration(seconds float64) {
	m.routingDuration.Observe(seconds)
}

// recordCompletionReceived increments the completions received counter for a status.
func (m *routerMetrics) recordCompletionReceived(status string) {
	m.completionsReceived.WithLabelValues(status).Inc()
}

// recordHTTPRequest records an HTTP request with endpoint, method, and status.
func (m *routerMetrics) recordHTTPRequest(endpoint, method, status string) {
	m.httpRequestsTotal.WithLabelValues(endpoint, method, status).Inc()
}

// recordHTTPDuration records the duration of an HTTP request.
func (m *routerMetrics) recordHTTPDuration(endpoint, method string, seconds float64) {
	m.httpRequestDuration.WithLabelValues(endpoint, method).Observe(seconds)
}

// recordLoopSignal records a loop signal attempt.
func (m *routerMetrics) recordLoopSignal(signalType string, accepted bool) {
	acceptedStr := "false"
	if accepted {
		acceptedStr = "true"
	}
	m.loopSignalsSent.WithLabelValues(signalType, acceptedStr).Inc()
}

// recordLoopApproval records an HTTP approval submission attempt.
// success=true means the response was published successfully on
// agent.approval_response.<loop_id>; success=false means the publish
// failed or the request was rejected at the dispatch boundary
// (validation, missing pending state, etc.). The status label uses
// "success"/"error" rather than a boolean so it lines up with the
// agentic-tools executions_total convention.
func (m *routerMetrics) recordLoopApproval(decision string, success bool) {
	status := "error"
	if success {
		status = "success"
	}
	m.loopApprovalsSubmitted.WithLabelValues(decision, status).Inc()
}

// recordSSEConnect increments the active SSE connections gauge.
func (m *routerMetrics) recordSSEConnect() {
	m.sseConnectionsActive.Inc()
}

// recordSSEDisconnect decrements the active SSE connections gauge.
func (m *routerMetrics) recordSSEDisconnect() {
	m.sseConnectionsActive.Dec()
}

// recordSSEEvent records an SSE event by type.
func (m *routerMetrics) recordSSEEvent(eventType string) {
	m.sseEventsTotal.WithLabelValues(eventType).Inc()
}

// recordSSEError records an SSE error by type.
func (m *routerMetrics) recordSSEError(errorType string) {
	m.sseErrorsTotal.WithLabelValues(errorType).Inc()
}
