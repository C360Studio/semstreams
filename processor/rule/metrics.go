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
	windowDimCapHit       *prometheus.CounterVec // count_in_window cardinality cap rejections
	windowTruncation      *prometheus.CounterVec // count_in_window per-key count cap truncations
	windowKeysSwept       *prometheus.CounterVec // sweep_windows keys deleted per janitor cron-rule fire
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

			// count_in_window cardinality cap — label: rule_id ONLY.
			// Intentionally NO dimension label: logging the dimension value here
			// would itself cause unbounded Prometheus label cardinality — exactly
			// the DoS surface the cap exists to prevent.
			windowDimCapHit: prometheus.NewCounterVec(prometheus.CounterOpts{
				Namespace: "semstreams",
				Subsystem: "rule",
				Name:      "window_dimension_cap_hit_total",
				Help: "New dimension values rejected by the per-rule cardinality cap for " +
					"count_in_window conditions. Alert when non-zero to detect dimension-space DoS " +
					"or misconfigured max_dimensions. Labels: rule_id only (no dimension label — " +
					"adding the dimension value would itself cause Prometheus cardinality blowup).",
			}, []string{"rule_id"}),

			// count_in_window per-key count cap — label: rule_id ONLY.
			// Tracks how often the oldest bucket is dropped to enforce the
			// per-window count cap. Expected to be silent in normal operation;
			// non-zero suggests the threshold or window is too permissive.
			windowTruncation: prometheus.NewCounterVec(prometheus.CounterOpts{
				Namespace: "semstreams",
				Subsystem: "rule",
				Name:      "window_truncation_total",
				Help: "Oldest bucket dropped to enforce the per-window count cap for " +
					"count_in_window conditions. Non-zero suggests max_per_window is too low " +
					"or the rule fires against extremely high-traffic dimensions.",
			}, []string{"rule_id"}),

			// sweep_windows janitor metric — label: rule_id (the cron rule ID).
			// Counts keys deleted per Sweep call. Zero = nothing to clean (healthy).
			// Non-zero = stale silent-dimension keys were found and removed. Alert
			// when this counter grows unboundedly without a corresponding decrease
			// in RULE_WINDOWS key count — that indicates new dimensions are arriving
			// faster than the janitor can sweep, which is a DoS signal.
			windowKeysSwept: prometheus.NewCounterVec(prometheus.CounterOpts{
				Namespace: "semstreams",
				Subsystem: "rule",
				Name:      "window_keys_swept_total",
				Help: "Keys deleted from RULE_WINDOWS by the sweep_windows janitor action. " +
					"Label rule_id is the cron rule that fired the sweep (e.g. " +
					"\"system-window-janitor\"). Zero is normal (no stale keys). " +
					"Rising values indicate silent-dimension cleanup is working as intended.",
			}, []string{"rule_id"}),
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
			ruleMetrics.windowDimCapHit,
			ruleMetrics.windowTruncation,
			ruleMetrics.windowKeysSwept,
		)
	})

	return ruleMetrics
}
