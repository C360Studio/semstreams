package graphclustering

import (
	"sync"

	"github.com/c360studio/semstreams/metric"
	"github.com/prometheus/client_golang/prometheus"
)

// clusteringMetrics holds Prometheus metrics for the graph-clustering component.
type clusteringMetrics struct {
	// indexLagAtDetection is the ENTITY_STATES revision lag graph-index reported at
	// the moment the most recent community-detection run was allowed to proceed.
	// Because a non-zero index_lag_tolerance means clustering runs on bounded-stale
	// topology BY DESIGN (ADR-082), this gauge is the operator's answer to "did the
	// last partition run stale, and by how much" — so bounded staleness cannot
	// become silent staleness (#579). 0 = the last run was exactly caught up.
	indexLagAtDetection prometheus.Gauge
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
			indexLagAtDetection: prometheus.NewGauge(prometheus.GaugeOpts{
				Namespace: "semstreams",
				Subsystem: "graph_clustering",
				Name:      "index_lag_at_detection",
				Help:      "ENTITY_STATES revision lag graph-index reported when the last community detection run was allowed to proceed (0 = exactly caught up). Non-zero means clustering ran on bounded-stale topology under index_lag_tolerance.",
			}),
		}
		if registry != nil {
			_ = registry.RegisterGauge("graph-clustering", "index_lag_at_detection", metrics.indexLagAtDetection)
		} else {
			_ = prometheus.DefaultRegisterer.Register(metrics.indexLagAtDetection)
		}
	})
	return metrics
}

// setIndexLagAtDetection records the lag the most recent detection run proceeded
// at (including 0 for an exactly-caught-up run, so the gauge reflects the latest
// run rather than going stale at the last non-zero value).
func (m *clusteringMetrics) setIndexLagAtDetection(lag uint64) {
	m.indexLagAtDetection.Set(float64(lag))
}
