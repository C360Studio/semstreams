package cache

import (
	"github.com/c360/semstreams/metric"
	"github.com/prometheus/client_golang/prometheus"
)

// cacheMetrics holds Prometheus metrics for cache operations.
type cacheMetrics struct {
	// Counter metrics - directly incremented without stats duplication
	hits      prometheus.Counter
	misses    prometheus.Counter
	sets      prometheus.Counter
	deletes   prometheus.Counter
	evictions prometheus.Counter

	// Gauge metrics - updated on operations
	size prometheus.Gauge
}

// newCacheMetrics creates and registers cache metrics with the provided registry.
func newCacheMetrics(registry *metric.MetricsRegistry, prefix string) (*cacheMetrics, error) {
	m := &cacheMetrics{
		hits: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace:   "semstreams",
			Subsystem:   "cache",
			Name:        "hits_total",
			ConstLabels: prometheus.Labels{"component": prefix},
			Help:        "Total number of cache hits",
		}),
		misses: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace:   "semstreams",
			Subsystem:   "cache",
			Name:        "misses_total",
			ConstLabels: prometheus.Labels{"component": prefix},
			Help:        "Total number of cache misses",
		}),
		sets: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace:   "semstreams",
			Subsystem:   "cache",
			Name:        "sets_total",
			ConstLabels: prometheus.Labels{"component": prefix},
			Help:        "Total number of cache set operations",
		}),
		deletes: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace:   "semstreams",
			Subsystem:   "cache",
			Name:        "deletes_total",
			ConstLabels: prometheus.Labels{"component": prefix},
			Help:        "Total number of cache delete operations",
		}),
		evictions: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace:   "semstreams",
			Subsystem:   "cache",
			Name:        "evictions_total",
			ConstLabels: prometheus.Labels{"component": prefix},
			Help:        "Total number of cache evictions",
		}),
		size: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace:   "semstreams",
			Subsystem:   "cache",
			Name:        "size",
			ConstLabels: prometheus.Labels{"component": prefix},
			Help:        "Current number of entries in cache",
		}),
	}

	// Register all metrics with the registry
	if err := registry.RegisterCounter(prefix, "cache_hits", m.hits); err != nil {
		return nil, err
	}
	if err := registry.RegisterCounter(prefix, "cache_misses", m.misses); err != nil {
		return nil, err
	}
	if err := registry.RegisterCounter(prefix, "cache_sets", m.sets); err != nil {
		return nil, err
	}
	if err := registry.RegisterCounter(prefix, "cache_deletes", m.deletes); err != nil {
		return nil, err
	}
	if err := registry.RegisterCounter(prefix, "cache_evictions", m.evictions); err != nil {
		return nil, err
	}
	if err := registry.RegisterGauge(prefix, "cache_size", m.size); err != nil {
		return nil, err
	}

	return m, nil
}

// recordHit increments the hit counter.
func (m *cacheMetrics) recordHit() {
	m.hits.Inc()
}

// recordMiss increments the miss counter.
func (m *cacheMetrics) recordMiss() {
	m.misses.Inc()
}

// recordSet increments the set counter.
func (m *cacheMetrics) recordSet() {
	m.sets.Inc()
}

// recordDelete increments the delete counter.
func (m *cacheMetrics) recordDelete() {
	m.deletes.Inc()
}

// recordEviction increments the eviction counter.
func (m *cacheMetrics) recordEviction() {
	m.evictions.Inc()
}

// updateSize sets the current cache size.
func (m *cacheMetrics) updateSize(size int) {
	m.size.Set(float64(size))
}
