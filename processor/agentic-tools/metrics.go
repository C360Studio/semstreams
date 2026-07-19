// Package agentictools provides Prometheus metrics for agentic-tools component.
package agentictools

import (
	"sync"

	"github.com/c360studio/semstreams/metric"
	"github.com/prometheus/client_golang/prometheus"
)

// toolsMetrics holds Prometheus metrics for the agentic-tools component.
type toolsMetrics struct {
	// Executions
	executionsTotal   *prometheus.CounterVec
	executionDuration *prometheus.HistogramVec

	// Errors
	errorsTotal  *prometheus.CounterVec
	timeoutTotal *prometheus.CounterVec

	// Filtering
	filteredTotal *prometheus.CounterVec

	// Writer-gate rejections (a tool's own contract gate bounced the call
	// before any graph mutation), labelled by tool and the gate reason. Kept
	// distinct from filteredTotal (governance/approval filtering upstream of
	// the executor) and errorsTotal (execution failures): a rejection is a
	// deliberate, instructive refusal that names the violated contract.
	rejectionsTotal *prometheus.CounterVec

	// Retries (opt-in via Config.ToolRetries policies)
	retriesTotal     *prometheus.CounterVec
	retriesExhausted *prometheus.CounterVec

	// Registry
	toolsRegistered prometheus.Gauge
}

// Package-level metrics (registered once to avoid duplicate registration errors)
var (
	metricsOnce sync.Once
	metrics     *toolsMetrics
)

// getMetrics returns the singleton metrics instance, creating and registering it if needed.
func getMetrics(registry *metric.MetricsRegistry) *toolsMetrics {
	metricsOnce.Do(func() {
		metrics = &toolsMetrics{
			executionsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
				Namespace: "semstreams",
				Subsystem: "agentic_tools",
				Name:      "executions_total",
				Help:      "Total tool executions by tool name and status",
			}, []string{"tool_name", "status"}),

			executionDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
				Namespace: "semstreams",
				Subsystem: "agentic_tools",
				Name:      "execution_duration_seconds",
				Help:      "Tool execution latency in seconds",
				Buckets:   prometheus.ExponentialBuckets(0.001, 2, 12), // 1ms to ~4s
			}, []string{"tool_name"}),

			errorsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
				Namespace: "semstreams",
				Subsystem: "agentic_tools",
				Name:      "errors_total",
				Help:      "Total tool errors by tool name and error type",
			}, []string{"tool_name", "error_type"}),

			timeoutTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
				Namespace: "semstreams",
				Subsystem: "agentic_tools",
				Name:      "timeout_total",
				Help:      "Total tool execution timeouts by tool name",
			}, []string{"tool_name"}),

			filteredTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
				Namespace: "semstreams",
				Subsystem: "agentic_tools",
				Name:      "filtered_total",
				Help:      "Total tool calls filtered by reason",
			}, []string{"tool_name", "reason"}),

			rejectionsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
				Namespace: "semstreams",
				Subsystem: "agentic_tools",
				Name:      "rejections_total",
				Help:      "Total tool-call rejections by a tool's own writer-gate, labelled by tool name and gate reason",
			}, []string{"tool_name", "reason"}),

			retriesTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
				Namespace: "semstreams",
				Subsystem: "agentic_tools",
				Name:      "retries_total",
				Help:      "Total tool-call retries triggered by a retry policy, labelled by tool and the error kind that triggered the retry",
			}, []string{"tool_name", "error_kind"}),

			retriesExhausted: prometheus.NewCounterVec(prometheus.CounterOpts{
				Namespace: "semstreams",
				Subsystem: "agentic_tools",
				Name:      "retries_exhausted_total",
				Help:      "Total tool calls whose retry budget was exhausted without success",
			}, []string{"tool_name"}),

			toolsRegistered: prometheus.NewGauge(prometheus.GaugeOpts{
				Namespace: "semstreams",
				Subsystem: "agentic_tools",
				Name:      "registered",
				Help:      "Number of registered tools",
			}),
		}

		// Register metrics with the metrics registry if available
		if registry != nil {
			_ = registry.RegisterCounterVec("agentic-tools", "executions_total", metrics.executionsTotal)
			_ = registry.RegisterHistogramVec("agentic-tools", "execution_duration_seconds", metrics.executionDuration)
			_ = registry.RegisterCounterVec("agentic-tools", "errors_total", metrics.errorsTotal)
			_ = registry.RegisterCounterVec("agentic-tools", "timeout_total", metrics.timeoutTotal)
			_ = registry.RegisterCounterVec("agentic-tools", "filtered_total", metrics.filteredTotal)
			_ = registry.RegisterCounterVec("agentic-tools", "rejections_total", metrics.rejectionsTotal)
			_ = registry.RegisterCounterVec("agentic-tools", "retries_total", metrics.retriesTotal)
			_ = registry.RegisterCounterVec("agentic-tools", "retries_exhausted_total", metrics.retriesExhausted)
			_ = registry.RegisterGauge("agentic-tools", "registered", metrics.toolsRegistered)
		} else {
			// Fallback to default prometheus registry for testing
			_ = prometheus.DefaultRegisterer.Register(metrics.executionsTotal)
			_ = prometheus.DefaultRegisterer.Register(metrics.executionDuration)
			_ = prometheus.DefaultRegisterer.Register(metrics.errorsTotal)
			_ = prometheus.DefaultRegisterer.Register(metrics.timeoutTotal)
			_ = prometheus.DefaultRegisterer.Register(metrics.filteredTotal)
			_ = prometheus.DefaultRegisterer.Register(metrics.rejectionsTotal)
			_ = prometheus.DefaultRegisterer.Register(metrics.retriesTotal)
			_ = prometheus.DefaultRegisterer.Register(metrics.retriesExhausted)
			_ = prometheus.DefaultRegisterer.Register(metrics.toolsRegistered)
		}
	})
	return metrics
}

// recordToolsRegistered sets the number of registered tools.
func (m *toolsMetrics) recordToolsRegistered(count int) {
	m.toolsRegistered.Set(float64(count))
}

// recordExecutionStart is called when a tool execution starts.
// Returns a function to call when the execution completes.
func (m *toolsMetrics) recordExecutionStart(toolName string) func(success bool) {
	start := prometheus.NewTimer(m.executionDuration.WithLabelValues(toolName))
	return func(success bool) {
		start.ObserveDuration()
		status := "success"
		if !success {
			status = "error"
		}
		m.executionsTotal.WithLabelValues(toolName, status).Inc()
	}
}

// recordExecutionSuccess records a successful tool execution.
func (m *toolsMetrics) recordExecutionSuccess(toolName string, durationSeconds float64) {
	m.executionsTotal.WithLabelValues(toolName, "success").Inc()
	m.executionDuration.WithLabelValues(toolName).Observe(durationSeconds)
}

// recordExecutionError records a failed tool execution.
func (m *toolsMetrics) recordExecutionError(toolName, errorType string, durationSeconds float64) {
	m.executionsTotal.WithLabelValues(toolName, "error").Inc()
	m.executionDuration.WithLabelValues(toolName).Observe(durationSeconds)
	m.errorsTotal.WithLabelValues(toolName, errorType).Inc()
}

// recordExecutionTimeout records a tool execution timeout.
func (m *toolsMetrics) recordExecutionTimeout(toolName string, durationSeconds float64) {
	m.executionsTotal.WithLabelValues(toolName, "timeout").Inc()
	m.executionDuration.WithLabelValues(toolName).Observe(durationSeconds)
	m.timeoutTotal.WithLabelValues(toolName).Inc()
}

// recordToolFiltered records a filtered tool call.
func (m *toolsMetrics) recordToolFiltered(toolName, reason string) {
	m.filteredTotal.WithLabelValues(toolName, reason).Inc()
}

// recordToolRejection records that a tool's own writer-gate bounced a call
// before any graph mutation, labelled by the gate reason (e.g. evidence,
// bound, grammar, cap for emit_lesson).
func (m *toolsMetrics) recordToolRejection(toolName, reason string) {
	m.rejectionsTotal.WithLabelValues(toolName, reason).Inc()
}

// recordToolRetry records that a retry was triggered for a tool by a
// specific error kind (the kind of the preceding failed attempt).
func (m *toolsMetrics) recordToolRetry(toolName, errorKind string) {
	m.retriesTotal.WithLabelValues(toolName, errorKind).Inc()
}

// recordToolRetryExhausted records that a tool's retry budget was exhausted
// without the call succeeding.
func (m *toolsMetrics) recordToolRetryExhausted(toolName string) {
	m.retriesExhausted.WithLabelValues(toolName).Inc()
}
