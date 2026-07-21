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
	// reported at the moment the most recent community-detection run proceeded. It is
	// THE "report, don't gate" mechanism: readiness withholds a run only for index
	// health, so a lagging view no longer defers anything, and this gauge is the only
	// place the age of the topology behind the published partition surfaces. Stamp the
	// age on the output rather than refusing to produce one. 0 = exactly caught up.
	//
	// It records on EVERY verified run — not, as under the retired max_staleness
	// tolerance, only on the runs a tolerance admitted — so "how stale are our
	// communities" is answerable continuously instead of only while a knob was set.
	//
	// It replaces the ADR-082 index_lag_at_detection gauge, whose unit (revisions)
	// moved 2-4x with coalesce_ms alone and again with write rate, making it
	// uncomparable across two deployments or two loads (gh#590).
	stalenessAtDetection prometheus.Gauge

	// detectionDuration is how long a community-detection run took, in seconds.
	//
	// It exists because a semboids adoption report had to derive this coupling by hand
	// from logs: detection time scales with community SIZE, so as a graph consolidates
	// (many small communities → few large ones) a run can grow several-fold — 4.4s to
	// 23.7s across one observed 90s window. A long run competes with the indexer for
	// the same box, so the view is staler at the next tick.
	//
	// Paired with stalenessAtDetection on a dashboard, the two make that loop visible
	// instead of inferable: rising duration alongside rising staleness is the
	// signature. It is now a pure observation — with no view-age tolerance left, a slow
	// run degrades the FRESHNESS of the published partition rather than stopping
	// clustering outright.
	detectionDuration prometheus.Histogram

	// deferTotal counts deferred detection ticks by typed reason. The label set is
	// CLOSED — it is graph.DeferReason, the same value the structured defer log
	// carries — so an operator can separate a broken index (hard_stop) from a dead
	// status feed (status_unknown), an index still doing its initial build
	// (bootstrap_incomplete), and an envelope this consumer cannot interpret
	// (unrecognized_state) without correlating log lines. Every surviving reason is an
	// INDEX HEALTH fact; none is answered by tuning a tolerance, because there is no
	// longer one to tune. All series are pre-initialized at zero so a reason that has
	// never fired is still scrapeable (absent series break rate() alerting).
	deferTotal *prometheus.CounterVec
}

// deferReasons is the closed label set for deferTotal, sourced from graph.AllDeferReasons
// so this file holds NO second copy of the vocabulary. A hand-maintained list here would
// drift silently: countDefer drops any reason outside it, so a reason added to the gate
// but not mirrored here would defer in production while incrementing nothing.
var deferReasons = graph.AllDeferReasons

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
				Help:      "Age of the graph-index view, in milliseconds, when the last community detection run proceeded (0 = exactly caught up). Recorded on every verified run: readiness gates on index health alone, so a lagging view never withholds a run and this gauge is where that lag is reported instead.",
			}),
			detectionDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
				Namespace: "semstreams",
				Subsystem: "graph_clustering",
				Name:      "detection_duration_seconds",
				Help:      "Wall time of a community-detection run. Scales with community size, so it grows as a graph consolidates; a long run competes with the indexer for the same box, so watch it alongside staleness_at_detection_ms — rising together means the published partition is getting older, not that clustering is being withheld.",
				// Detection observed between ~4s and ~24s on a 200-entity flock; the
				// buckets bracket that with room either side rather than using the
				// default second-scale set, which would pile everything into +Inf.
				Buckets: []float64{0.5, 1, 2, 5, 10, 20, 30, 60, 120},
			}),
			deferTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
				Namespace: "semstreams",
				Subsystem: "graph_clustering",
				Name:      "defer_total",
				Help:      "Community detection ticks deferred by the readiness gate, by reason: hard_stop (degraded/reset_required index), status_unknown (no fresh readiness envelope — the feed died or graph-index is absent), bootstrap_incomplete (producer has not finished its initial build this process lifetime), unrecognized_state (envelope State is blank or outside the known set — version skew). All four are index-health faults; a merely lagging view is never deferred — its age is reported on staleness_at_detection_ms.",
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
