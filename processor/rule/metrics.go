package rule

import (
	"github.com/c360/semstreams/metric"
	"github.com/prometheus/client_golang/prometheus"
)

// RuleMetrics holds Prometheus metrics for RuleProcessor component
type RuleMetrics struct {
	messagesReceived      *prometheus.CounterVec
	evaluationsTotal      *prometheus.CounterVec
	triggersTotal         *prometheus.CounterVec
	evaluationDuration    *prometheus.HistogramVec
	bufferSize            *prometheus.GaugeVec
	bufferExpiredTotal    *prometheus.CounterVec
	cooldownActive        *prometheus.GaugeVec
	eventsPublishedTotal  *prometheus.CounterVec
	errorsTotal           *prometheus.CounterVec
	activeRules           prometheus.Gauge
}

// newRuleMetrics creates and registers RuleProcessor metrics
func newRuleMetrics(registry *metric.MetricsRegistry, _ string) *RuleMetrics {
	// Return nil if no registry provided (nil input = nil feature pattern)
	if registry == nil {
		return nil
	}

	metrics := &RuleMetrics{
		messagesReceived: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "semstreams",
			Subsystem: "rule",
			Name:      "messages_received_total",
			Help:      "Total messages received for rule evaluation",
		}, []string{"subject"}),

		evaluationsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "semstreams",
			Subsystem: "rule",
			Name:      "evaluations_total",
			Help:      "Total rule evaluations performed",
		}, []string{"rule_name", "result"}),

		triggersTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "semstreams",
			Subsystem: "rule",
			Name:      "triggers_total",
			Help:      "Total rule triggers (successful evaluations)",
		}, []string{"rule_name", "severity"}),

		evaluationDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "semstreams",
			Subsystem: "rule",
			Name:      "evaluation_duration_seconds",
			Help:      "Time spent evaluating individual rules",
			Buckets:   []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0},
		}, []string{"rule_name"}),

		bufferSize: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: "semstreams",
			Subsystem: "rule",
			Name:      "buffer_size",
			Help:      "Current message buffer size per rule",
		}, []string{"rule_name"}),

		bufferExpiredTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "semstreams",
			Subsystem: "rule",
			Name:      "buffer_expired_total",
			Help:      "Messages expired from time windows",
		}, []string{"rule_name"}),

		cooldownActive: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: "semstreams",
			Subsystem: "rule",
			Name:      "cooldown_active",
			Help:      "Rules currently in cooldown state",
		}, []string{"rule_name"}),

		eventsPublishedTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "semstreams",
			Subsystem: "rule",
			Name:      "events_published_total",
			Help:      "Rule events published to NATS",
		}, []string{"subject", "event_type"}),

		errorsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "semstreams",
			Subsystem: "rule",
			Name:      "errors_total",
			Help:      "Rule processing errors",
		}, []string{"rule_name", "error_type"}),

		activeRules: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "semstreams",
			Subsystem: "rule",
			Name:      "active_rules",
			Help:      "Number of active rules loaded",
		}),
	}

	// Register metrics with Prometheus registry
	registry.PrometheusRegistry().MustRegister(
		metrics.messagesReceived,
		metrics.evaluationsTotal,
		metrics.triggersTotal,
		metrics.evaluationDuration,
		metrics.bufferSize,
		metrics.bufferExpiredTotal,
		metrics.cooldownActive,
		metrics.eventsPublishedTotal,
		metrics.errorsTotal,
		metrics.activeRules,
	)

	return metrics
}
