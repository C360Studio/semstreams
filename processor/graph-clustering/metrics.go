package graphclustering

import (
	"sync"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/graph/clustering"
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

	// semanticEdgesApplied is the #618 "is this partition semantically blind?"
	// signal: 1 when the most recent detection cycle ran with the semantic-edge
	// tier ACTIVE (graph-embedding ready, mutual-kNN edges in the vote), 0 when it
	// ran STRUCTURAL-ONLY because the embedding index was not ready. It is the axis
	// that distinguishes "semantics ran and found nothing" (gauge 1, a full cycle
	// with no mutual-kNN pairs) from "semantics never ran" (gauge 0, a cold-index
	// structural-only cycle) — the exact confusion that let a semantically-blind
	// partition commit silently. Only updated on deployments that ENABLE the tier;
	// an unopted deployment never touches it (its value is meaningless there).
	semanticEdgesApplied prometheus.Gauge

	// semanticEdgeBuildMs is the wall-clock duration, in milliseconds, of a single
	// mutual-kNN cache refresh (B2 §7.2). It is observed on every ACTIVE cycle,
	// including the near-zero all-reused cycles: the distribution makes the reuse
	// win visible (a corpus that has not changed refreshes in ~0ms because it
	// issues no similarity queries) and surfaces the O(N)-query rebuild cost when
	// the embedding watermark advances. Only meaningful where enable_semantic_edges
	// is true.
	semanticEdgeBuildMs prometheus.Histogram

	// semanticEdgeSimilarQueries counts the FindSimilar (graph.embedding.query.similar)
	// calls the mutual-kNN refresh issues (B2 §7.2). It is the load the revision-keyed
	// cache (§7.1) bounds: on an unchanged corpus a refresh reuses every directed set
	// and this counter does not move, so increase() over a scrape interval reads ~0;
	// it steps up by the re-queried entity count when the embedding watermark advances
	// or a previously-errored entity is retried. Watching it is how an operator
	// confirms the cache is actually bounding query load rather than re-issuing ~N
	// queries per cycle. Only meaningful where enable_semantic_edges is true.
	semanticEdgeSimilarQueries prometheus.Counter
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
			semanticEdgesApplied: prometheus.NewGauge(prometheus.GaugeOpts{
				Namespace: "semstreams",
				Subsystem: "graph_clustering",
				Name:      "semantic_edges_applied",
				Help:      "Whether the most recent community-detection cycle applied the semantic-edge tier: 1 = active (graph-embedding ready, mutual-kNN edges in the vote), 0 = structural-only (embedding index not ready). Distinguishes a full cycle that found no mutual-kNN neighbors (1) from a semantically-blind cycle that never ran semantics (0) — the #618 signal. Only meaningful where enable_semantic_edges is true.",
			}),
			semanticEdgeBuildMs: prometheus.NewHistogram(prometheus.HistogramOpts{
				Namespace: "semstreams",
				Subsystem: "graph_clustering",
				Name:      "semantic_edge_build_ms",
				Help:      "Wall time of one mutual-kNN cache refresh, in milliseconds (B2 §7). Observed every active cycle: an unchanged corpus refreshes in ~0ms (it reuses every cached directed set and issues no similarity queries), while an embedding-watermark advance triggers an O(N)-query rebuild. Read alongside semantic_edge_similar_queries_total to see the revision-keyed cache bounding query load. Only meaningful where enable_semantic_edges is true.",
				// A reuse cycle is sub-millisecond; a full rebuild spans one query per
				// entity, each up to the 30s similarity timeout. Bracket both ends.
				Buckets: []float64{0.1, 0.5, 1, 5, 10, 50, 100, 500, 1000, 5000, 30000},
			}),
			semanticEdgeSimilarQueries: prometheus.NewCounter(prometheus.CounterOpts{
				Namespace: "semstreams",
				Subsystem: "graph_clustering",
				Name:      "semantic_edge_similar_queries_total",
				Help:      "FindSimilar (graph.embedding.query.similar) calls issued by the mutual-kNN cache refresh (B2 §7). The revision-keyed cache (§7.1) bounds this: on an unchanged corpus a refresh reuses every directed set and this counter does not advance, so increase() over a scrape interval reads ~0; it steps up by the re-queried entity count when the embedding watermark advances or an errored entity is retried. Only meaningful where enable_semantic_edges is true.",
			}),
		}
		for _, reason := range deferReasons {
			metrics.deferTotal.WithLabelValues(string(reason))
		}
		// The THREE semantic-tier series (semanticEdgesApplied, semanticEdgeBuildMs,
		// semanticEdgeSimilarQueries) are created above (so the fields are never nil and
		// stamping never panics) but are deliberately NOT registered here: they are
		// exposed ONLY for a deployment that ENABLES the tier, via registerSemanticMetrics
		// called from the factory. Registering them unconditionally would change the
		// exported metric surface of a DISABLED (default-off) deployment — a registered
		// semantic_edges_applied scrapes a default 0 indistinguishable from an
		// enabled-but-cold cycle (#618), and the §7 refresh-cost series appear on a
		// deployment that never runs a refresh — contradicting default-off-identical
		// (Codex P2#4/P2#5).
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

// registerSemanticMetrics exposes ALL THREE semantic-tier series —
// semantic_edges_applied (#618), semantic_edge_build_ms and
// semantic_edge_similar_queries_total (B2 §7.2) — and is called ONLY for a deployment
// that ENABLES the semantic-edge tier (from the factory). A disabled (default-off)
// deployment therefore never exports any of them, so its metric surface is byte-identical
// to a pre-tier build: no misleading semantic_edges_applied=0 that reads as "enabled but
// embeddings cold" (#618), and no §7 refresh-cost series on a deployment that never runs a
// refresh (Codex P2#4/P2#5). Idempotent: the registry dedups by key, and the
// default-registerer path swallows the AlreadyRegistered error like the getMetrics
// registrations do, so repeat enabled instances in one process are safe.
func (m *clusteringMetrics) registerSemanticMetrics(registry *metric.MetricsRegistry) {
	if m == nil || m.semanticEdgesApplied == nil {
		return
	}
	if registry != nil {
		_ = registry.RegisterGauge("graph-clustering", "semantic_edges_applied", m.semanticEdgesApplied)
		_ = registry.RegisterHistogram("graph-clustering", "semantic_edge_build_ms", m.semanticEdgeBuildMs)
		_ = registry.RegisterCounter("graph-clustering", "semantic_edge_similar_queries_total", m.semanticEdgeSimilarQueries)
		return
	}
	_ = prometheus.DefaultRegisterer.Register(m.semanticEdgesApplied)
	_ = prometheus.DefaultRegisterer.Register(m.semanticEdgeBuildMs)
	_ = prometheus.DefaultRegisterer.Register(m.semanticEdgeSimilarQueries)
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

// setSemanticEdgesApplied records whether the most recent detection cycle ran with
// the semantic-edge tier active (1) or structural-only (0). Set every enabled-tier
// cycle so the gauge reflects the latest verdict, not the last time it flipped.
func (m *clusteringMetrics) setSemanticEdgesApplied(active bool) {
	if active {
		m.semanticEdgesApplied.Set(1)
		return
	}
	m.semanticEdgesApplied.Set(0)
}

// observeSemanticEdgeBuildMs records one mutual-kNN cache refresh's duration
// (B2 §7.2). Recorded on every active-cycle refresh, including the ~0ms reuse
// cycles, so the distribution shows how often an actual rebuild happens.
func (m *clusteringMetrics) observeSemanticEdgeBuildMs(ms float64) {
	m.semanticEdgeBuildMs.Observe(ms)
}

// addSemanticEdgeQueries adds the FindSimilar calls a refresh issued (B2 §7.2).
// A no-op add of 0 (a fully-reused cycle) leaves the counter flat, which is the
// observable that the revision-keyed cache is bounding query load.
func (m *clusteringMetrics) addSemanticEdgeQueries(n int) {
	if n <= 0 {
		return
	}
	m.semanticEdgeSimilarQueries.Add(float64(n))
}

// semanticEdgeMetricsAdapter is the clustering.SemanticEdgeMetrics sink the
// SemanticEdgeProvider records through, backed by this component's Prometheus
// metrics. It keeps the leaf clustering package free of a prometheus import
// while the actual registration lives here beside semantic_edges_applied.
type semanticEdgeMetricsAdapter struct{ m *clusteringMetrics }

// Verify the adapter satisfies the clustering-side sink.
var _ clustering.SemanticEdgeMetrics = semanticEdgeMetricsAdapter{}

func (a semanticEdgeMetricsAdapter) ObserveBuildMs(ms float64) {
	if a.m == nil {
		return
	}
	a.m.observeSemanticEdgeBuildMs(ms)
}

func (a semanticEdgeMetricsAdapter) AddSimilarQueries(n int) {
	if a.m == nil {
		return
	}
	a.m.addSemanticEdgeQueries(n)
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
