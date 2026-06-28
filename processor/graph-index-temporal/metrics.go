// Package graphindextemporal provides Prometheus metrics for the graph-index-temporal component.
package graphindextemporal

import (
	"sync"

	"github.com/c360studio/semstreams/metric"
	"github.com/prometheus/client_golang/prometheus"
)

// Index timestamp-source labels for temporalMetrics.indexed.
const (
	// indexSourceObserved means the entity was indexed by its observation
	// timestamp (time.observation.recorded) — the correct event-time key.
	indexSourceObserved = "observed"
	// indexSourceWriteFallback means the entity had no observation predicate
	// and was indexed by its UpdatedAt (last-write) timestamp instead. A high
	// or rising ratio of this label means producers have not yet adopted the
	// observation predicate — it is the visible ledger for retiring the fallback.
	indexSourceWriteFallback = "write_fallback"
)

// temporalMetrics holds Prometheus metrics for the graph-index-temporal component.
type temporalMetrics struct {
	// indexed counts entities indexed, labelled by the timestamp source that
	// keyed them (observed | write_fallback). Surfaces the event-time vs
	// processing-time split so the managed UpdatedAt fallback is observable,
	// not silent (gh#370).
	indexed *prometheus.CounterVec
	// staleRemovals counts entity events removed from a prior time bucket on
	// re-index or delete — the cleanup that keeps range queries from returning
	// an entity out of a bucket it has since left.
	staleRemovals prometheus.Counter
	// reverseErrors counts failures writing or deleting the reverse map. A
	// nonzero value means the forward index and the reverse map may have drifted
	// (e.g. an entity could survive deletion in range queries) — paired with a
	// Warn log at the call site so the drift is observable, not silent.
	reverseErrors prometheus.Counter
}

// Package-level metrics (registered once to avoid duplicate registration errors).
var (
	metricsOnce sync.Once
	metrics     *temporalMetrics
)

// getMetrics returns the singleton metrics instance, creating and registering it if needed.
func getMetrics(registry *metric.MetricsRegistry) *temporalMetrics {
	metricsOnce.Do(func() {
		metrics = &temporalMetrics{
			indexed: prometheus.NewCounterVec(prometheus.CounterOpts{
				Namespace: "semstreams",
				Subsystem: "graph_index_temporal",
				Name:      "entities_indexed_total",
				Help:      "Entities indexed by timestamp source: observed (time.observation.recorded) or write_fallback (UpdatedAt). A rising write_fallback ratio means producers have not adopted the observation predicate.",
			}, []string{"source"}),

			staleRemovals: prometheus.NewCounter(prometheus.CounterOpts{
				Namespace: "semstreams",
				Subsystem: "graph_index_temporal",
				Name:      "stale_bucket_removals_total",
				Help:      "Entity events removed from a prior time bucket on re-index or delete (stale-entry cleanup).",
			}),

			reverseErrors: prometheus.NewCounter(prometheus.CounterOpts{
				Namespace: "semstreams",
				Subsystem: "graph_index_temporal",
				Name:      "reverse_index_errors_total",
				Help:      "Failures writing/deleting the reverse map (entity -> bucket). Nonzero means the forward index and reverse map may have drifted.",
			}),
		}

		if registry != nil {
			_ = registry.RegisterCounterVec("graph-index-temporal", "entities_indexed_total", metrics.indexed)
			_ = registry.RegisterCounter("graph-index-temporal", "stale_bucket_removals_total", metrics.staleRemovals)
			_ = registry.RegisterCounter("graph-index-temporal", "reverse_index_errors_total", metrics.reverseErrors)
		} else {
			// Fallback to default prometheus registry for testing.
			_ = prometheus.DefaultRegisterer.Register(metrics.indexed)
			_ = prometheus.DefaultRegisterer.Register(metrics.staleRemovals)
			_ = prometheus.DefaultRegisterer.Register(metrics.reverseErrors)
		}
	})
	return metrics
}

// recordIndexed records an entity indexed under the given timestamp source.
func (m *temporalMetrics) recordIndexed(source string) {
	if m == nil {
		return
	}
	m.indexed.WithLabelValues(source).Inc()
}

// recordStaleRemoval records an entity event removed from a prior time bucket.
func (m *temporalMetrics) recordStaleRemoval() {
	if m == nil {
		return
	}
	m.staleRemovals.Inc()
}

// recordReverseError records a reverse-map write/delete failure (index drift).
func (m *temporalMetrics) recordReverseError() {
	if m == nil {
		return
	}
	m.reverseErrors.Inc()
}
