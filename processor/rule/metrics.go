package rule

import (
	"sync"

	"github.com/c360studio/semstreams/metric"
	"github.com/prometheus/client_golang/prometheus"
)

// Metrics holds Prometheus metrics for RuleProcessor component
type Metrics struct {
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
	stateTransitionsTotal *prometheus.CounterVec // OnEnter/OnExit transitions
	debounceDelaysTotal   prometheus.Counter     // Coalesced updates due to debouncing
	shadowFires           *prometheus.CounterVec // Shadow-mode action suppression counter
}

// Package-level singleton (registered once to avoid duplicate registration panics)
var (
	ruleMetricsOnce sync.Once
	ruleMetrics     *Metrics
)

// newRuleMetrics returns the singleton RuleProcessor metrics, creating and
// registering them on first call. Multiple rule-processor instances share
// the same collectors since Prometheus metric names are global.
func newRuleMetrics(registry *metric.MetricsRegistry, _ string) *Metrics {
	if registry == nil {
		return nil
	}

	ruleMetricsOnce.Do(func() {
		ruleMetrics = &Metrics{
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

			stateTransitionsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
				Namespace: "semstreams",
				Subsystem: "rule",
				Name:      "state_transitions_total",
				Help:      "Total rule state transitions (OnEnter/OnExit)",
			}, []string{"rule_name", "transition"}),

			debounceDelaysTotal: prometheus.NewCounter(prometheus.CounterOpts{
				Namespace: "semstreams",
				Subsystem: "rule",
				Name:      "debounce_delays_total",
				Help:      "Total number of debounced rule evaluations (coalesced updates)",
			}),

			shadowFires: prometheus.NewCounterVec(prometheus.CounterOpts{
				Namespace: "semstreams",
				Subsystem: "rule",
				Name:      "shadow_fires_total",
				Help: "Actions suppressed by shadow mode per rule and action type. " +
					"Use this to validate a shadow rule against production traffic before enabling it.",
			}, []string{"rule_id", "action_type"}),
		}

		registry.PrometheusRegistry().MustRegister(
			ruleMetrics.messagesReceived,
			ruleMetrics.evaluationsTotal,
			ruleMetrics.triggersTotal,
			ruleMetrics.evaluationDuration,
			ruleMetrics.bufferSize,
			ruleMetrics.bufferExpiredTotal,
			ruleMetrics.cooldownActive,
			ruleMetrics.eventsPublishedTotal,
			ruleMetrics.errorsTotal,
			ruleMetrics.activeRules,
			ruleMetrics.stateTransitionsTotal,
			ruleMetrics.debounceDelaysTotal,
			ruleMetrics.shadowFires,
		)
	})

	return ruleMetrics
}
