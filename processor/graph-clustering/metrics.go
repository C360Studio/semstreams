package graphclustering

import (
	"sync"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/metric"
	"github.com/prometheus/client_golang/prometheus"
)

// clusteringMetrics holds Prometheus metrics for the graph-clustering component.
type clusteringMetrics struct {
	// stalenessAtDetection is the AGE OF THE VIEW, in milliseconds, that graph-index
	// reported at the moment the most recent community-detection run was allowed to
	// proceed. Because a non-zero max_staleness means clustering runs on bounded-stale
	// topology BY DESIGN (ADR-083), this gauge is the operator's answer to "did the
	// last partition run stale, and by how much" — so bounded staleness cannot become
	// silent staleness (#579). 0 = the last run was exactly caught up.
	//
	// It replaces the ADR-082 index_lag_at_detection gauge, whose unit (revisions)
	// moved 2-4x with coalesce_ms alone and again with write rate, making it
	// uncomparable across two deployments or two loads (gh#590).
	stalenessAtDetection prometheus.Gauge

	// detectionDuration is how long a community-detection run took, in seconds.
	//
	// It is the companion `max_staleness` needs, and it exists because a semboids
	// adoption report had to derive this coupling by hand from logs: detection time
	// scales with community SIZE, so as a graph consolidates (many small communities →
	// few large ones) a run can grow several-fold — 4.4s to 23.7s across one observed
	// 90s window. A long run competes with the indexer for the same box, so the view is
	// staler at the next tick, and a tolerance that was ample while the graph was
	// fragmented starts tripping over_staleness for a reason unrelated to index health.
	//
	// Paired with stalenessAtDetection on a dashboard, the two make that loop visible
	// instead of inferable: rising duration alongside rising staleness against a fixed
	// max_staleness is the signature.
	detectionDuration prometheus.Histogram

	// deferTotal counts deferred detection ticks by typed reason. The label set is
	// CLOSED — it is graph.DeferReason, the same value the structured defer log
	// carries — so an operator can separate a broken index (hard_stop) from an
	// over-stale view (over_staleness), a dead status feed (status_unknown), an index
	// still doing its initial build (bootstrap_incomplete), and an envelope this
	// consumer cannot interpret (unrecognized_state) without correlating log lines.
	// All series are
	// pre-initialized at zero so a reason that has never fired is still scrapeable
	// (absent series break rate() alerting).
	deferTotal *prometheus.CounterVec
}

// deferReasons is the closed label set for deferTotal. Sourcing it from the typed
// graph.DeferReason constants is what keeps the metric labels and the gate's own
// vocabulary from drifting apart.
var deferReasons = []graph.DeferReason{
	graph.DeferHardStop,
	graph.DeferOverStaleness,
	graph.DeferStatusUnknown,
	graph.DeferBootstrapIncomplete,
	graph.DeferUnrecognizedState,
	graph.DeferStalenessUnknown,
}

// Package-level metrics (registered once to avoid duplicate registration errors).
var (
	metricsOnce sync.Once
	metrics     *clusteringMetrics
)

// getMetrics returns the singleton metrics instance, creating and registering it
// if needed. Mirrors graph-query's getMetrics: registers with the shared registry
// when present, else falls back to the default registerer for tests.
func getMetrics(registry *metric.MetricsRegistry) *clusteringMetrics {
	metricsOnce.Do(func() {
		metrics = &clusteringMetrics{
			stalenessAtDetection: prometheus.NewGauge(prometheus.GaugeOpts{
				Namespace: "semstreams",
				Subsystem: "graph_clustering",
				Name:      "staleness_at_detection_ms",
				Help:      "Age of the graph-index view, in milliseconds, when the last community detection run was allowed to proceed (0 = exactly caught up). Non-zero means clustering ran on bounded-stale topology under max_staleness.",
			}),
			detectionDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
				Namespace: "semstreams",
				Subsystem: "graph_clustering",
				Name:      "detection_duration_seconds",
				Help:      "Wall time of a community-detection run. Scales with community size, so it grows as a graph consolidates; a run longer than max_staleness will start tripping over_staleness defers (the tolerance must exceed the consumer's own worst-case cycle).",
				// Detection observed between ~4s and ~24s on a 200-entity flock; the
				// buckets bracket that with room either side rather than using the
				// default second-scale set, which would pile everything into +Inf.
				Buckets: []float64{0.5, 1, 2, 5, 10, 20, 30, 60, 120},
			}),
			deferTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
				Namespace: "semstreams",
				Subsystem: "graph_clustering",
				Name:      "defer_total",
				Help:      "Community detection ticks deferred by the readiness gate, by reason: hard_stop (degraded/reset_required index), over_staleness (view older than max_staleness, including the exact gate's zero tolerance), status_unknown (no fresh readiness envelope — the feed died or graph-index is absent), bootstrap_incomplete (producer has not finished its initial build this process lifetime), unrecognized_state (envelope State is blank or outside the known set — version skew), staleness_unknown (the producer could not compute a view age, so no tolerance applies).",
			}, []string{"reason"}),
		}
		for _, reason := range deferReasons {
			metrics.deferTotal.WithLabelValues(string(reason))
		}
		if registry != nil {
			_ = registry.RegisterGauge("graph-clustering", "staleness_at_detection_ms", metrics.stalenessAtDetection)
			_ = registry.RegisterCounterVec("graph-clustering", "defer_total", metrics.deferTotal)
			_ = registry.RegisterHistogram("graph-clustering", "detection_duration_seconds", metrics.detectionDuration)
		} else {
			_ = prometheus.DefaultRegisterer.Register(metrics.stalenessAtDetection)
			_ = prometheus.DefaultRegisterer.Register(metrics.deferTotal)
			_ = prometheus.DefaultRegisterer.Register(metrics.detectionDuration)
		}
	})
	return metrics
}

// setStalenessAtDetection records the view age the most recent detection run
// proceeded at (including 0 for an exactly-caught-up run, so the gauge reflects the
// latest run rather than going stale at the last non-zero value).
func (m *clusteringMetrics) setStalenessAtDetection(stalenessMs uint64) {
	m.stalenessAtDetection.Set(float64(stalenessMs))
}

// observeDetectionDuration records a completed detection run's wall time. Recorded for
// EVERY completed run, not only slow ones — the finding it exists for is a TREND
// (duration climbing as communities consolidate), which a threshold-triggered
// observation would hide.
func (m *clusteringMetrics) observeDetectionDuration(d time.Duration) {
	m.detectionDuration.Observe(d.Seconds())
}

// countDefer increments the typed defer counter. An unrecognized reason would create
// an unbounded label, so only the closed set counts — the gate cannot produce another
// value, and if it ever did, dropping it is better than a cardinality leak.
func (m *clusteringMetrics) countDefer(reason graph.DeferReason) {
	for _, known := range deferReasons {
		if reason == known {
			m.deferTotal.WithLabelValues(string(reason)).Inc()
			return
		}
	}
}
