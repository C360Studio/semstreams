// Package agentictools provides Prometheus metrics for agentic-tools component.
package agentictools

import (
	"sync"

	"github.com/c360studio/semstreams/metric"
	"github.com/prometheus/client_golang/prometheus"
)

type outcomePath string
type outcomeStoreOperation string
type outcomeStoreFailureReason string
type resultPublishFailureReason string
type ambiguousRedeliveryCause string

const (
	outcomePathNew       outcomePath = "new"
	outcomePathReplay    outcomePath = "replay"
	outcomePathRejection outcomePath = "rejection"
	outcomePathCompact   outcomePath = "compact"

	storeOperationGet        outcomeStoreOperation = "get"
	storeOperationCreate     outcomeStoreOperation = "create"
	storeOperationReadWinner outcomeStoreOperation = "read_winner"

	storeReasonTransport outcomeStoreFailureReason = "transport"
	storeReasonOversize  outcomeStoreFailureReason = "oversize"
	storeReasonCorrupt   outcomeStoreFailureReason = "corrupt"

	publishReasonTransport resultPublishFailureReason = "transport"
	publishReasonOversize  resultPublishFailureReason = "oversize"
	publishReasonMarshal   resultPublishFailureReason = "marshal"

	ambiguousCauseStoreFailure ambiguousRedeliveryCause = "store_failure"
	ambiguousCauseShutdown     ambiguousRedeliveryCause = "shutdown"
	ambiguousCauseHeartbeat    ambiguousRedeliveryCause = "heartbeat"
	ambiguousCausePanic        ambiguousRedeliveryCause = "panic"
)

// toolsMetrics holds Prometheus metrics for agentic-tools component.
type toolsMetrics struct {
	executionsTotal   *prometheus.CounterVec
	executionDuration *prometheus.HistogramVec
	errorsTotal       *prometheus.CounterVec
	timeoutTotal      *prometheus.CounterVec
	filteredTotal     *prometheus.CounterVec
	rejectionsTotal   *prometheus.CounterVec
	retriesTotal      *prometheus.CounterVec
	retriesExhausted  *prometheus.CounterVec
	toolsRegistered   prometheus.Gauge

	outcomeTotal          *prometheus.CounterVec
	outcomeStoreFailures  *prometheus.CounterVec
	outcomeCollisions     prometheus.Counter
	resultPublishFailures *prometheus.CounterVec
	ambiguousRedeliveries *prometheus.CounterVec
}

var (
	metricsOnce sync.Once
	metrics     *toolsMetrics
)

func getMetrics(registry *metric.MetricsRegistry) *toolsMetrics {
	metricsOnce.Do(func() {
		metrics = newToolsMetrics()
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
			_ = registry.RegisterCounterVec("agentic-tools", "outcome_total", metrics.outcomeTotal)
			_ = registry.RegisterCounterVec("agentic-tools", "outcome_store_failures_total", metrics.outcomeStoreFailures)
			_ = registry.RegisterCounter("agentic-tools", "outcome_collisions_total", metrics.outcomeCollisions)
			_ = registry.RegisterCounterVec("agentic-tools", "result_publish_failures_total", metrics.resultPublishFailures)
			_ = registry.RegisterCounterVec("agentic-tools", "ambiguous_redeliveries_total", metrics.ambiguousRedeliveries)
			return
		}
		for _, collector := range []prometheus.Collector{
			metrics.executionsTotal, metrics.executionDuration, metrics.errorsTotal, metrics.timeoutTotal,
			metrics.filteredTotal, metrics.rejectionsTotal, metrics.retriesTotal, metrics.retriesExhausted,
			metrics.toolsRegistered, metrics.outcomeTotal, metrics.outcomeStoreFailures, metrics.outcomeCollisions,
			metrics.resultPublishFailures, metrics.ambiguousRedeliveries,
		} {
			_ = prometheus.DefaultRegisterer.Register(collector)
		}
	})
	return metrics
}

func newToolsMetrics() *toolsMetrics {
	return &toolsMetrics{
		executionsTotal:       prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: "semstreams", Subsystem: "agentic_tools", Name: "executions_total", Help: "Total tool executions by tool name and status"}, []string{"tool_name", "status"}),
		executionDuration:     prometheus.NewHistogramVec(prometheus.HistogramOpts{Namespace: "semstreams", Subsystem: "agentic_tools", Name: "execution_duration_seconds", Help: "Tool execution latency in seconds", Buckets: prometheus.ExponentialBuckets(0.001, 2, 12)}, []string{"tool_name"}),
		errorsTotal:           prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: "semstreams", Subsystem: "agentic_tools", Name: "errors_total", Help: "Total tool errors by tool name and error type"}, []string{"tool_name", "error_type"}),
		timeoutTotal:          prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: "semstreams", Subsystem: "agentic_tools", Name: "timeout_total", Help: "Total tool execution timeouts by tool name"}, []string{"tool_name"}),
		filteredTotal:         prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: "semstreams", Subsystem: "agentic_tools", Name: "filtered_total", Help: "Total tool calls filtered by reason"}, []string{"tool_name", "reason"}),
		rejectionsTotal:       prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: "semstreams", Subsystem: "agentic_tools", Name: "rejections_total", Help: "Total writer-gate rejections"}, []string{"tool_name", "reason"}),
		retriesTotal:          prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: "semstreams", Subsystem: "agentic_tools", Name: "retries_total", Help: "Total tool-call retries"}, []string{"tool_name", "error_kind"}),
		retriesExhausted:      prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: "semstreams", Subsystem: "agentic_tools", Name: "retries_exhausted_total", Help: "Total exhausted tool-call retry budgets"}, []string{"tool_name"}),
		toolsRegistered:       prometheus.NewGauge(prometheus.GaugeOpts{Namespace: "semstreams", Subsystem: "agentic_tools", Name: "registered", Help: "Number of registered tools"}),
		outcomeTotal:          prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: "semstreams", Subsystem: "agentic_tools", Name: "outcome_total", Help: "Durable outcome paths"}, []string{"path"}),
		outcomeStoreFailures:  prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: "semstreams", Subsystem: "agentic_tools", Name: "outcome_store_failures_total", Help: "Outcome store failures"}, []string{"operation", "reason"}),
		outcomeCollisions:     prometheus.NewCounter(prometheus.CounterOpts{Namespace: "semstreams", Subsystem: "agentic_tools", Name: "outcome_collisions_total", Help: "Immutable outcome collisions"}),
		resultPublishFailures: prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: "semstreams", Subsystem: "agentic_tools", Name: "result_publish_failures_total", Help: "Tool result publication failures"}, []string{"reason"}),
		ambiguousRedeliveries: prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: "semstreams", Subsystem: "agentic_tools", Name: "ambiguous_redeliveries_total", Help: "Redeliveries with potentially completed external effects"}, []string{"cause"}),
	}
}

func (m *toolsMetrics) recordOutcome(path outcomePath) {
	m.outcomeTotal.WithLabelValues(string(path)).Inc()
}
func (m *toolsMetrics) recordStoreFailure(operation outcomeStoreOperation, reason outcomeStoreFailureReason) {
	m.outcomeStoreFailures.WithLabelValues(string(operation), string(reason)).Inc()
}
func (m *toolsMetrics) recordCollision() { m.outcomeCollisions.Inc() }
func (m *toolsMetrics) recordPublishFailure(reason resultPublishFailureReason) {
	m.resultPublishFailures.WithLabelValues(string(reason)).Inc()
}
func (m *toolsMetrics) recordAmbiguous(cause ambiguousRedeliveryCause) {
	m.ambiguousRedeliveries.WithLabelValues(string(cause)).Inc()
}

func (m *toolsMetrics) recordToolsRegistered(count int) { m.toolsRegistered.Set(float64(count)) }
func (m *toolsMetrics) recordExecutionStart(toolName string) func(bool) {
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
func (m *toolsMetrics) recordExecutionSuccess(toolName string, seconds float64) {
	m.executionsTotal.WithLabelValues(toolName, "success").Inc()
	m.executionDuration.WithLabelValues(toolName).Observe(seconds)
}
func (m *toolsMetrics) recordExecutionError(toolName, errorType string, seconds float64) {
	m.executionsTotal.WithLabelValues(toolName, "error").Inc()
	m.executionDuration.WithLabelValues(toolName).Observe(seconds)
	m.errorsTotal.WithLabelValues(toolName, errorType).Inc()
}
func (m *toolsMetrics) recordExecutionTimeout(toolName string, seconds float64) {
	m.executionsTotal.WithLabelValues(toolName, "timeout").Inc()
	m.executionDuration.WithLabelValues(toolName).Observe(seconds)
	m.timeoutTotal.WithLabelValues(toolName).Inc()
}
func (m *toolsMetrics) recordToolFiltered(toolName, reason string) {
	m.filteredTotal.WithLabelValues(toolName, reason).Inc()
}
func (m *toolsMetrics) recordToolRejection(toolName, reason string) {
	m.rejectionsTotal.WithLabelValues(toolName, reason).Inc()
}
func (m *toolsMetrics) recordToolRetry(toolName, kind string) {
	m.retriesTotal.WithLabelValues(toolName, kind).Inc()
}
func (m *toolsMetrics) recordToolRetryExhausted(toolName string) {
	m.retriesExhausted.WithLabelValues(toolName).Inc()
}
