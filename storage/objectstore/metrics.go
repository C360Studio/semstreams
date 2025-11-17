package objectstore

import (
	"github.com/c360/semstreams/metric"
	"github.com/prometheus/client_golang/prometheus"
)

// storeMetrics holds Prometheus metrics for ObjectStore operations.
type storeMetrics struct {
	// Operation counters
	readOps   *prometheus.CounterVec // By bucket
	writeOps  *prometheus.CounterVec // By bucket
	deleteOps *prometheus.CounterVec // By bucket
	listOps   *prometheus.CounterVec // By bucket

	// Operation latency
	readLatency   *prometheus.HistogramVec // By bucket
	writeLatency  *prometheus.HistogramVec // By bucket
	deleteLatency *prometheus.HistogramVec // By bucket
	listLatency   *prometheus.HistogramVec // By bucket

	// Error counters
	errors *prometheus.CounterVec // By operation and bucket

	// State gauges
	objectCount  *prometheus.GaugeVec // By bucket
	storageBytes *prometheus.GaugeVec // By bucket

	// Cache performance (from embedded cache)
	cacheHits   *prometheus.CounterVec
	cacheMisses *prometheus.CounterVec
}

// newStoreMetrics creates and registers ObjectStore metrics with the provided registry.
func newStoreMetrics(registry *metric.MetricsRegistry, bucket string) (*storeMetrics, error) {
	if registry == nil {
		return nil, nil // Metrics disabled
	}

	m := &storeMetrics{
		// Operation counters
		readOps: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace:   "semstreams",
			Subsystem:   "objectstore",
			Name:        "read_operations_total",
			Help:        "Total number of read operations",
			ConstLabels: prometheus.Labels{"bucket": bucket},
		}, []string{"operation"}), // operation: get, get_metadata

		writeOps: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace:   "semstreams",
			Subsystem:   "objectstore",
			Name:        "write_operations_total",
			Help:        "Total number of write operations",
			ConstLabels: prometheus.Labels{"bucket": bucket},
		}, []string{"operation"}), // operation: put, store

		deleteOps: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace:   "semstreams",
			Subsystem:   "objectstore",
			Name:        "delete_operations_total",
			Help:        "Total number of delete operations",
			ConstLabels: prometheus.Labels{"bucket": bucket},
		}, []string{}),

		listOps: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace:   "semstreams",
			Subsystem:   "objectstore",
			Name:        "list_operations_total",
			Help:        "Total number of list operations",
			ConstLabels: prometheus.Labels{"bucket": bucket},
		}, []string{}),

		// Operation latency histograms
		readLatency: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace:   "semstreams",
			Subsystem:   "objectstore",
			Name:        "read_duration_seconds",
			Help:        "Read operation duration in seconds",
			ConstLabels: prometheus.Labels{"bucket": bucket},
			Buckets:     []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1.0, 2.0},
		}, []string{"operation"}),

		writeLatency: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace:   "semstreams",
			Subsystem:   "objectstore",
			Name:        "write_duration_seconds",
			Help:        "Write operation duration in seconds",
			ConstLabels: prometheus.Labels{"bucket": bucket},
			Buckets:     []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1.0, 2.0},
		}, []string{"operation"}),

		deleteLatency: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace:   "semstreams",
			Subsystem:   "objectstore",
			Name:        "delete_duration_seconds",
			Help:        "Delete operation duration in seconds",
			ConstLabels: prometheus.Labels{"bucket": bucket},
			Buckets:     []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1.0, 2.0},
		}, []string{}),

		listLatency: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace:   "semstreams",
			Subsystem:   "objectstore",
			Name:        "list_duration_seconds",
			Help:        "List operation duration in seconds",
			ConstLabels: prometheus.Labels{"bucket": bucket},
			Buckets:     []float64{0.01, 0.05, 0.1, 0.5, 1.0, 2.0, 5.0},
		}, []string{}),

		// Error counters
		errors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace:   "semstreams",
			Subsystem:   "objectstore",
			Name:        "operation_errors_total",
			Help:        "Total number of operation errors",
			ConstLabels: prometheus.Labels{"bucket": bucket},
		}, []string{"operation"}), // operation: get, put, store, delete, list, get_metadata

		// State gauges
		objectCount: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace:   "semstreams",
			Subsystem:   "objectstore",
			Name:        "object_count",
			Help:        "Current number of objects in store",
			ConstLabels: prometheus.Labels{"bucket": bucket},
		}, []string{}),

		storageBytes: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace:   "semstreams",
			Subsystem:   "objectstore",
			Name:        "storage_bytes",
			Help:        "Storage bytes used",
			ConstLabels: prometheus.Labels{"bucket": bucket},
		}, []string{}),

		// Cache performance
		cacheHits: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace:   "semstreams",
			Subsystem:   "objectstore",
			Name:        "cache_hits_total",
			Help:        "Total number of cache hits",
			ConstLabels: prometheus.Labels{"bucket": bucket},
		}, []string{}),

		cacheMisses: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace:   "semstreams",
			Subsystem:   "objectstore",
			Name:        "cache_misses_total",
			Help:        "Total number of cache misses",
			ConstLabels: prometheus.Labels{"bucket": bucket},
		}, []string{}),
	}

	// Register all metrics
	prefix := "objectstore_" + bucket

	// Counters
	if err := registry.RegisterCounterVec(prefix, "read_ops", m.readOps); err != nil {
		return nil, err
	}
	if err := registry.RegisterCounterVec(prefix, "write_ops", m.writeOps); err != nil {
		return nil, err
	}
	if err := registry.RegisterCounterVec(prefix, "delete_ops", m.deleteOps); err != nil {
		return nil, err
	}
	if err := registry.RegisterCounterVec(prefix, "list_ops", m.listOps); err != nil {
		return nil, err
	}

	// Histograms
	if err := registry.RegisterHistogramVec(prefix, "read_latency", m.readLatency); err != nil {
		return nil, err
	}
	if err := registry.RegisterHistogramVec(prefix, "write_latency", m.writeLatency); err != nil {
		return nil, err
	}
	if err := registry.RegisterHistogramVec(prefix, "delete_latency", m.deleteLatency); err != nil {
		return nil, err
	}
	if err := registry.RegisterHistogramVec(prefix, "list_latency", m.listLatency); err != nil {
		return nil, err
	}

	// Error counters
	if err := registry.RegisterCounterVec(prefix, "errors", m.errors); err != nil {
		return nil, err
	}

	// Gauges
	if err := registry.RegisterGaugeVec(prefix, "object_count", m.objectCount); err != nil {
		return nil, err
	}
	if err := registry.RegisterGaugeVec(prefix, "storage_bytes", m.storageBytes); err != nil {
		return nil, err
	}

	// Cache metrics
	if err := registry.RegisterCounterVec(prefix, "cache_hits", m.cacheHits); err != nil {
		return nil, err
	}
	if err := registry.RegisterCounterVec(prefix, "cache_misses", m.cacheMisses); err != nil {
		return nil, err
	}

	return m, nil
}

// recordReadOp records a read operation metric.
func (m *storeMetrics) recordReadOp(operation string) {
	if m != nil {
		m.readOps.WithLabelValues(operation).Inc()
	}
}

// recordWriteOp records a write operation metric.
func (m *storeMetrics) recordWriteOp(operation string) {
	if m != nil {
		m.writeOps.WithLabelValues(operation).Inc()
	}
}

// recordDeleteOp records a delete operation metric.
func (m *storeMetrics) recordDeleteOp() {
	if m != nil {
		m.deleteOps.WithLabelValues().Inc()
	}
}

// recordListOp records a list operation metric.
func (m *storeMetrics) recordListOp() {
	if m != nil {
		m.listOps.WithLabelValues().Inc()
	}
}

// recordReadLatency records read operation latency.
func (m *storeMetrics) recordReadLatency(operation string, seconds float64) {
	if m != nil {
		m.readLatency.WithLabelValues(operation).Observe(seconds)
	}
}

// recordWriteLatency records write operation latency.
func (m *storeMetrics) recordWriteLatency(operation string, seconds float64) {
	if m != nil {
		m.writeLatency.WithLabelValues(operation).Observe(seconds)
	}
}

// recordDeleteLatency records delete operation latency.
func (m *storeMetrics) recordDeleteLatency(seconds float64) {
	if m != nil {
		m.deleteLatency.WithLabelValues().Observe(seconds)
	}
}

// recordListLatency records list operation latency.
func (m *storeMetrics) recordListLatency(seconds float64) {
	if m != nil {
		m.listLatency.WithLabelValues().Observe(seconds)
	}
}

// recordError records an error metric.
func (m *storeMetrics) recordError(operation string) {
	if m != nil {
		m.errors.WithLabelValues(operation).Inc()
	}
}

// updateObjectCount updates the object count gauge.
func (m *storeMetrics) updateObjectCount(count float64) {
	if m != nil {
		m.objectCount.WithLabelValues().Set(count)
	}
}

// updateStorageBytes updates the storage bytes gauge.
func (m *storeMetrics) updateStorageBytes(bytes float64) {
	if m != nil {
		m.storageBytes.WithLabelValues().Set(bytes)
	}
}

// recordCacheHit records a cache hit.
func (m *storeMetrics) recordCacheHit() {
	if m != nil {
		m.cacheHits.WithLabelValues().Inc()
	}
}

// recordCacheMiss records a cache miss.
func (m *storeMetrics) recordCacheMiss() {
	if m != nil {
		m.cacheMisses.WithLabelValues().Inc()
	}
}
