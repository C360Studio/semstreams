// Package rule - Prometheus metrics for the cron scheduler.
//
// The cron scheduler exposes seven metrics under the
// `semstreams_cron_*` namespace. They mirror what an operator typically
// wants to see for a periodically-firing component:
//
//   - fires_total{rule_id, status} — counter; status is success / error
//     / panic / cooldown_skipped. Lets ops see fire rates per rule and
//     spot rules whose cron expression ticks faster than actions
//     complete (cooldown_skipped grows).
//   - fire_duration_seconds{rule_id} — histogram of action-dispatch
//     latency. Surfaces slow `publish_agent` rules that risk
//     overlapping with the next tick before the cooldown gate triggers.
//   - registered — gauge; total registered cron rules. Flips on
//     hot-reload; useful for confirming a config change took effect.
//   - missed_fires_total{rule_id} — counter; incremented once per
//     missed fire detected on Start. Per ADR-031 the policy is log-only,
//     so the metric tracks cumulative missed fires rather than
//     auto-replay attempts.
//   - missed_fires_capped_total{rule_id} — counter; incremented when
//     a rule's missed-fire count hits the detection cap (currently 100).
//     Lets operators alert on long downtimes without grepping logs.
//   - next_fire_timestamp_seconds{rule_id} — gauge; Unix epoch seconds
//     of the rule's next scheduled fire. Enables alerts like "no fire
//     in last 2× period" by comparing scrape time against the gauge.
//   - scheduler_running — gauge; 1 when Start has been called and Stop
//     hasn't, 0 otherwise. Catches the "processor still alive but
//     scheduler crashed" pathology.
//
// The constructor follows the routerMetrics pattern in
// agentic-dispatch: per-registry instances cached so multiple
// processors sharing a registry get the same collectors, plus a
// nil-registry singleton fallback for production deployments using
// the default Prometheus registerer. Both paths support nil-tolerant
// recordX helpers so callers don't have to nil-check before every
// observation.
package rule

import (
	"sync"

	"github.com/c360studio/semstreams/metric"
	"github.com/prometheus/client_golang/prometheus"
)

// Cron fire status labels — exported as constants so call sites and
// downstream alerting rules use the same strings. Adding a new status
// here MUST be paired with a dashboard panel update; otherwise the new
// label silently increases cardinality without operator visibility.
const (
	cronFireStatusSuccess         = "success"
	cronFireStatusError           = "error"
	cronFireStatusPanic           = "panic"
	cronFireStatusCooldownSkipped = "cooldown_skipped"
	// cronFireStatusInflightSkipped distinguishes the operator-error
	// case "actions are slower than schedule, no cooldown configured"
	// from the configured-and-working cooldown_skipped path. Operators
	// alerting on inflight_skipped > 0 know to either tune the schedule,
	// configure a cooldown explicitly, or speed up the actions.
	cronFireStatusInflightSkipped = "inflight_skipped"
	// cronFireStatusDenied is emitted when a deny action short-circuits the
	// fire cycle. Semantically distinct from cronFireStatusError: "a rule said
	// no" vs "things are broken". Dashboards that filter on status="error" for
	// "fire failed" views must also include status="denied" if they want a
	// unified "rule did not complete normally" view.
	cronFireStatusDenied = "denied"
)

// cronMetrics holds the Prometheus collectors for cron-scheduler
// observability. All recordX methods are safe to call on a nil
// receiver — production paths can take the nil-metrics branch on
// startup misconfiguration without crashing.
type cronMetrics struct {
	firesTotal             *prometheus.CounterVec
	fireDurationSeconds    *prometheus.HistogramVec
	registered             prometheus.Gauge
	missedFiresTotal       *prometheus.CounterVec
	missedFiresCappedTotal *prometheus.CounterVec
	nextFireTimestampSecs  *prometheus.GaugeVec
	schedulerRunning       prometheus.Gauge
}

// Per-registry caching mirrors routerMetrics in agentic-dispatch.
// Tests pass a fresh MetricsRegistry per test for isolation; production
// uses nil and gets a process-singleton.
var (
	cronMetricsMu    sync.Mutex
	cronMetricsCache = make(map[*metric.MetricsRegistry]*cronMetrics)
	cronNilMetrics   *cronMetrics
	cronNilOnce      sync.Once
)

// getCronMetrics returns a *cronMetrics bound to registry, constructing
// and registering the collectors on first use. A nil registry returns a
// process-singleton bound to the default Prometheus registerer.
func getCronMetrics(registry *metric.MetricsRegistry) *cronMetrics {
	if registry == nil {
		cronNilOnce.Do(func() {
			cronNilMetrics = createAndRegisterCronMetrics(nil)
		})
		return cronNilMetrics
	}
	cronMetricsMu.Lock()
	defer cronMetricsMu.Unlock()
	if m, ok := cronMetricsCache[registry]; ok {
		return m
	}
	m := createAndRegisterCronMetrics(registry)
	cronMetricsCache[registry] = m
	return m
}

func createAndRegisterCronMetrics(registry *metric.MetricsRegistry) *cronMetrics {
	const (
		namespace = "semstreams"
		subsystem = "cron"
	)
	m := &cronMetrics{
		firesTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "rule_fires_total",
			Help:      "Cron rule fire attempts by rule and outcome (success/error/panic/cooldown_skipped)",
		}, []string{"rule_id", "status"}),

		fireDurationSeconds: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "rule_fire_duration_seconds",
			Help:      "Action-dispatch latency per cron rule fire",
			// 1ms to ~16s — covers fast publish actions through slow
			// publish_agent dispatches that include LLM round-trips.
			Buckets: prometheus.ExponentialBuckets(0.001, 2, 14),
		}, []string{"rule_id"}),

		registered: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "rule_registered",
			Help:      "Number of cron rules currently registered with the scheduler",
		}),

		missedFiresTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "rule_missed_fires_total",
			Help:      "Cron rule missed fires detected on scheduler restart (log-only policy; not replayed)",
		}, []string{"rule_id"}),

		missedFiresCappedTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "rule_missed_fires_capped_total",
			Help:      "Cron rules whose missed-fire detection hit the bounded cap (100) at restart — long downtime indicator",
		}, []string{"rule_id"}),

		nextFireTimestampSecs: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "rule_next_fire_timestamp_seconds",
			Help:      "Unix epoch (seconds) of the next scheduled fire per cron rule",
		}, []string{"rule_id"}),

		schedulerRunning: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "scheduler_running",
			Help:      "1 when the cron scheduler is started and ticking, 0 otherwise",
		}),
	}

	if registry != nil {
		_ = registry.RegisterCounterVec(subsystem, "rule_fires_total", m.firesTotal)
		_ = registry.RegisterHistogramVec(subsystem, "rule_fire_duration_seconds", m.fireDurationSeconds)
		_ = registry.RegisterGauge(subsystem, "rule_registered", m.registered)
		_ = registry.RegisterCounterVec(subsystem, "rule_missed_fires_total", m.missedFiresTotal)
		_ = registry.RegisterCounterVec(subsystem, "rule_missed_fires_capped_total", m.missedFiresCappedTotal)
		_ = registry.RegisterGaugeVec(subsystem, "rule_next_fire_timestamp_seconds", m.nextFireTimestampSecs)
		_ = registry.RegisterGauge(subsystem, "scheduler_running", m.schedulerRunning)
	} else {
		_ = prometheus.DefaultRegisterer.Register(m.firesTotal)
		_ = prometheus.DefaultRegisterer.Register(m.fireDurationSeconds)
		_ = prometheus.DefaultRegisterer.Register(m.registered)
		_ = prometheus.DefaultRegisterer.Register(m.missedFiresTotal)
		_ = prometheus.DefaultRegisterer.Register(m.missedFiresCappedTotal)
		_ = prometheus.DefaultRegisterer.Register(m.nextFireTimestampSecs)
		_ = prometheus.DefaultRegisterer.Register(m.schedulerRunning)
	}
	return m
}

// recordFire is the single chokepoint for fires_total + fire_duration.
// It increments fires_total{rule_id, status} unconditionally and
// observes the duration histogram only for statuses that represent a
// completed dispatch (success / error / denied). Skipped statuses
// (cooldown, inflight) and the panic backstop intentionally skip the
// histogram:
//
//   - cooldown / inflight skipped fires never dispatched; their
//     "duration" is just the gate-check time and would pull the
//     histogram artificially low.
//   - panic IS a dispatch outcome but its duration measures partial
//     work + recovery overhead, which would distort success/error
//     percentiles. Panics are paged on via the counter, not the
//     histogram.
//   - denied IS a completed dispatch (the deny action ran); its
//     duration is meaningful for latency analysis alongside success/error.
//
// Callers always go through this method — there is no sibling helper
// per status — so this is the only place a future contributor needs to
// edit when the status taxonomy changes.
func (m *cronMetrics) recordFire(ruleID, status string, durationSeconds float64) {
	if m == nil {
		return
	}
	m.firesTotal.WithLabelValues(ruleID, status).Inc()
	if status == cronFireStatusSuccess || status == cronFireStatusError || status == cronFireStatusDenied {
		m.fireDurationSeconds.WithLabelValues(ruleID).Observe(durationSeconds)
	}
}

// recordRuleRegistered increments the registered gauge. Called from
// CronScheduler.Register on a successful (re-)registration.
func (m *cronMetrics) recordRuleRegistered() {
	if m == nil {
		return
	}
	m.registered.Inc()
}

// recordRuleDeregistered decrements the registered gauge. Called from
// CronScheduler.Deregister on a successful removal.
func (m *cronMetrics) recordRuleDeregistered() {
	if m == nil {
		return
	}
	m.registered.Dec()
}

// recordMissedFire increments missed_fires_total by the given count for
// a single rule. The detector reports the total in one call rather than
// invoking N times so consumers see the increment as one logical event.
func (m *cronMetrics) recordMissedFire(ruleID string, count int) {
	if m == nil || count <= 0 {
		return
	}
	m.missedFiresTotal.WithLabelValues(ruleID).Add(float64(count))
}

// recordMissedFireCapped increments missed_fires_capped_total when a
// rule's restart-detection hit the bounded cap. One call per rule per
// restart.
func (m *cronMetrics) recordMissedFireCapped(ruleID string) {
	if m == nil {
		return
	}
	m.missedFiresCappedTotal.WithLabelValues(ruleID).Inc()
}

// recordNextFire updates the next-fire-timestamp gauge for a rule.
// Called after Register and after each successful fire so the gauge
// always reflects the freshest schedule.Next() value.
func (m *cronMetrics) recordNextFire(ruleID string, unixSeconds float64) {
	if m == nil {
		return
	}
	m.nextFireTimestampSecs.WithLabelValues(ruleID).Set(unixSeconds)
}

// clearNextFire removes the next-fire gauge for a rule. Called on
// Deregister so a removed rule's gauge doesn't go stale and produce
// false-positive alerts.
func (m *cronMetrics) clearNextFire(ruleID string) {
	if m == nil {
		return
	}
	m.nextFireTimestampSecs.DeleteLabelValues(ruleID)
}

// recordSchedulerRunning sets the running gauge. running=true on
// Start, false on Stop. The gauge surfaces the "scheduler crashed but
// processor still alive" pathology that a process-level liveness check
// would miss.
func (m *cronMetrics) recordSchedulerRunning(running bool) {
	if m == nil {
		return
	}
	if running {
		m.schedulerRunning.Set(1)
	} else {
		m.schedulerRunning.Set(0)
	}
}
