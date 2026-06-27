package gateddagexec

import (
	"github.com/c360studio/semstreams/metric"
	"github.com/prometheus/client_golang/prometheus"
)

const metricsService = "gated-dag"

// metrics holds the executor's prometheus collectors. The collectors are always
// constructed (they operate standalone); they are only registered with the
// shared registry when one is provided, so increments are always nil-safe.
type metrics struct {
	stall       prometheus.Gauge
	claimed     prometheus.Counter
	dispatched  prometheus.Counter
	dispatchErr prometheus.Counter
	evals       prometheus.Counter
}

// newMetrics builds the collectors and registers them with reg when non-nil.
func newMetrics(reg *metric.MetricsRegistry) *metrics {
	m := &metrics{
		stall: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "gated_dag_stalled_units",
			Help: "Number of units held-ready with no forward progress and nothing in-flight (a depends_on cycle or all-blocked-behind-failure). 0 = healthy.",
		}),
		claimed: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "gated_dag_units_claimed_total",
			Help: "Durable claim markers committed (before dispatch).",
		}),
		dispatched: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "gated_dag_units_dispatched_total",
			Help: "Dispatch references published after a successful claim.",
		}),
		dispatchErr: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "gated_dag_dispatch_errors_total",
			Help: "Dispatch publishes that failed after the claim was committed (the unit may be stranded until reset).",
		}),
		evals: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "gated_dag_evaluations_total",
			Help: "Re-evaluation passes (watch-triggered + backstop ticks).",
		}),
	}
	if reg != nil {
		// Best-effort registration: a duplicate (e.g. two flows) is logged by the
		// registry and does not abort the executor.
		_ = reg.RegisterGauge(metricsService, "stalled_units", m.stall)
		_ = reg.RegisterCounter(metricsService, "units_claimed_total", m.claimed)
		_ = reg.RegisterCounter(metricsService, "units_dispatched_total", m.dispatched)
		_ = reg.RegisterCounter(metricsService, "dispatch_errors_total", m.dispatchErr)
		_ = reg.RegisterCounter(metricsService, "evaluations_total", m.evals)
	}
	return m
}
