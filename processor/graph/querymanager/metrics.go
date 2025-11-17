package querymanager

import (
	"time"

	"github.com/c360/semstreams/metric"
	"github.com/prometheus/client_golang/prometheus"
)

// Metrics holds Prometheus metrics for the query manager service
type Metrics struct {
	// Entity operations
	entityGetTotal      *prometheus.CounterVec
	entityGetDuration   *prometheus.HistogramVec
	entityBatchTotal    *prometheus.CounterVec
	entityBatchDuration *prometheus.HistogramVec

	// Cache metrics - L1 (Hot - LRU)
	l1CacheHits      prometheus.Counter
	l1CacheMisses    prometheus.Counter
	l1CacheSize      prometheus.Gauge
	l1CacheEvictions prometheus.Counter

	// Cache metrics - L2 (Warm - TTL)
	l2CacheHits      prometheus.Counter
	l2CacheMisses    prometheus.Counter
	l2CacheSize      prometheus.Gauge
	l2CacheEvictions prometheus.Counter
	l2CacheExpired   prometheus.Counter

	// Cache metrics - L3 (Query Results - Hybrid)
	l3CacheHits      prometheus.Counter
	l3CacheMisses    prometheus.Counter
	l3CacheSize      prometheus.Gauge
	l3CacheEvictions prometheus.Counter

	// Overall cache metrics
	cacheHitRatio   prometheus.Gauge
	cachePromotions prometheus.Counter
	kvFetches       prometheus.Counter
	kvFetchDuration prometheus.Histogram

	// Cache invalidation metrics
	invalidationsTotal   prometheus.Counter
	invalidationsBatched prometheus.Counter
	invalidationDuration prometheus.Histogram
	invalidationErrors   prometheus.Counter

	// Query execution metrics
	queryTotal      *prometheus.CounterVec
	queryDuration   *prometheus.HistogramVec
	queryResultSize *prometheus.HistogramVec
	queryErrors     *prometheus.CounterVec
	queryTimeouts   *prometheus.CounterVec

	// Path execution metrics
	pathExecutionTotal    prometheus.Counter
	pathExecutionDuration prometheus.Histogram
	pathLengthHistogram   prometheus.Histogram

	// Snapshot metrics
	snapshotTotal         prometheus.Counter
	snapshotDuration      prometheus.Histogram
	snapshotSizeHistogram prometheus.Histogram

	// KV Watch metrics
	watchEventsTotal    *prometheus.CounterVec
	watchEventsDuration prometheus.Histogram
	watchErrors         prometheus.Counter
	watchLag            prometheus.Gauge
	activeWatchers      prometheus.Gauge

	// Index manager dependency metrics
	indexManagerRequests *prometheus.CounterVec
	indexManagerDuration *prometheus.HistogramVec
	indexManagerErrors   *prometheus.CounterVec

	// Health metrics
	healthCheckDuration prometheus.Histogram
	componentHealth     *prometheus.GaugeVec
	memoryUsage         prometheus.Gauge
}

// NewMetrics creates a new query manager metrics instance using MetricsRegistry
func NewMetrics(registry *metric.MetricsRegistry, _ string) *Metrics {
	if registry == nil {
		return nil
	}

	m := &Metrics{}

	// Initialize metrics by category
	m.initializeEntityMetrics(registry)
	m.initializeL1CacheMetrics(registry)
	m.initializeL2CacheMetrics(registry)
	m.initializeL3CacheMetrics(registry)
	m.initializeOverallCacheMetrics(registry)
	m.initializeInvalidationMetrics(registry)
	m.initializeQueryMetrics(registry)
	m.initializePathMetrics(registry)
	m.initializeSnapshotMetrics(registry)
	m.initializeWatchMetrics(registry)
	m.initializeIndexManagerMetrics(registry)
	m.initializeHealthMetrics(registry)

	return m
}

// initializeEntityMetrics initializes entity operation metrics
func (m *Metrics) initializeEntityMetrics(registry *metric.MetricsRegistry) {
	m.entityGetTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "semstreams_queryengine_entity_get_total",
			Help: "Total number of entity get operations",
		},
		[]string{"component", "status"},
	)
	registry.RegisterCounterVec("queryengine", "entity_get_total", m.entityGetTotal)

	m.entityGetDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "semstreams_queryengine_entity_get_duration_seconds",
			Help:    "Duration of entity get operations",
			Buckets: []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1.0, 2.0, 5.0},
		},
		[]string{"component", "cache_layer"},
	)
	registry.RegisterHistogramVec("queryengine", "entity_get_duration_seconds", m.entityGetDuration)

	m.entityBatchTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "semstreams_queryengine_entity_batch_total",
			Help: "Total number of entity batch operations",
		},
		[]string{"component", "status"},
	)
	registry.RegisterCounterVec("queryengine", "entity_batch_total", m.entityBatchTotal)

	m.entityBatchDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "semstreams_queryengine_entity_batch_duration_seconds",
			Help:    "Duration of entity batch operations",
			Buckets: []float64{0.01, 0.05, 0.1, 0.5, 1.0, 2.0, 5.0, 10.0},
		},
		[]string{"component", "size_range"},
	)
	registry.RegisterHistogramVec("queryengine", "entity_batch_duration_seconds", m.entityBatchDuration)
}

// initializeL1CacheMetrics initializes L1 cache metrics
func (m *Metrics) initializeL1CacheMetrics(registry *metric.MetricsRegistry) {
	m.l1CacheHits = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "semstreams_queryengine_l1_cache_hits_total",
			Help: "Total L1 cache hits",
		},
	)
	registry.RegisterCounter("queryengine", "l1_cache_hits_total", m.l1CacheHits)

	m.l1CacheMisses = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "semstreams_queryengine_l1_cache_misses_total",
			Help: "Total L1 cache misses",
		},
	)
	registry.RegisterCounter("queryengine", "l1_cache_misses_total", m.l1CacheMisses)

	m.l1CacheSize = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "semstreams_queryengine_l1_cache_size",
			Help: "Current L1 cache size",
		},
	)
	registry.RegisterGauge("queryengine", "l1_cache_size", m.l1CacheSize)

	m.l1CacheEvictions = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "semstreams_queryengine_l1_cache_evictions_total",
			Help: "Total L1 cache evictions",
		},
	)
	registry.RegisterCounter("queryengine", "l1_cache_evictions_total", m.l1CacheEvictions)
}

// initializeL2CacheMetrics initializes L2 cache metrics
func (m *Metrics) initializeL2CacheMetrics(registry *metric.MetricsRegistry) {
	m.l2CacheHits = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "semstreams_queryengine_l2_cache_hits_total",
			Help: "Total L2 cache hits",
		},
	)
	registry.RegisterCounter("queryengine", "l2_cache_hits_total", m.l2CacheHits)

	m.l2CacheMisses = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "semstreams_queryengine_l2_cache_misses_total",
			Help: "Total L2 cache misses",
		},
	)
	registry.RegisterCounter("queryengine", "l2_cache_misses_total", m.l2CacheMisses)

	m.l2CacheSize = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "semstreams_queryengine_l2_cache_size",
			Help: "Current L2 cache size",
		},
	)
	registry.RegisterGauge("queryengine", "l2_cache_size", m.l2CacheSize)

	m.l2CacheEvictions = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "semstreams_queryengine_l2_cache_evictions_total",
			Help: "Total L2 cache evictions",
		},
	)
	registry.RegisterCounter("queryengine", "l2_cache_evictions_total", m.l2CacheEvictions)

	m.l2CacheExpired = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "semstreams_queryengine_l2_cache_expired_total",
			Help: "Total L2 cache expirations",
		},
	)
	registry.RegisterCounter("queryengine", "l2_cache_expired_total", m.l2CacheExpired)
}

// initializeL3CacheMetrics initializes L3 cache metrics
func (m *Metrics) initializeL3CacheMetrics(registry *metric.MetricsRegistry) {
	m.l3CacheHits = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "semstreams_queryengine_l3_cache_hits_total",
			Help: "Total L3 cache hits",
		},
	)
	registry.RegisterCounter("queryengine", "l3_cache_hits_total", m.l3CacheHits)

	m.l3CacheMisses = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "semstreams_queryengine_l3_cache_misses_total",
			Help: "Total L3 cache misses",
		},
	)
	registry.RegisterCounter("queryengine", "l3_cache_misses_total", m.l3CacheMisses)

	m.l3CacheSize = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "semstreams_queryengine_l3_cache_size",
			Help: "Current L3 cache size",
		},
	)
	registry.RegisterGauge("queryengine", "l3_cache_size", m.l3CacheSize)

	m.l3CacheEvictions = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "semstreams_queryengine_l3_cache_evictions_total",
			Help: "Total L3 cache evictions",
		},
	)
	registry.RegisterCounter("queryengine", "l3_cache_evictions_total", m.l3CacheEvictions)
}

// initializeOverallCacheMetrics initializes overall cache metrics
func (m *Metrics) initializeOverallCacheMetrics(registry *metric.MetricsRegistry) {
	m.cacheHitRatio = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "semstreams_queryengine_cache_hit_ratio",
			Help: "Overall cache hit ratio",
		},
	)
	registry.RegisterGauge("queryengine", "cache_hit_ratio", m.cacheHitRatio)

	m.cachePromotions = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "semstreams_queryengine_cache_promotions_total",
			Help: "Total cache promotions from L2 to L1",
		},
	)
	registry.RegisterCounter("queryengine", "cache_promotions_total", m.cachePromotions)

	m.kvFetches = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "semstreams_queryengine_kv_fetches_total",
			Help: "Total KV fetches (cache misses)",
		},
	)
	registry.RegisterCounter("queryengine", "kv_fetches_total", m.kvFetches)

	m.kvFetchDuration = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "semstreams_queryengine_kv_fetch_duration_seconds",
			Help:    "Duration of KV fetch operations",
			Buckets: []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1.0, 2.0},
		},
	)
	registry.RegisterHistogram("queryengine", "kv_fetch_duration_seconds", m.kvFetchDuration)
}

// initializeInvalidationMetrics initializes cache invalidation metrics
func (m *Metrics) initializeInvalidationMetrics(registry *metric.MetricsRegistry) {
	m.invalidationsTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "semstreams_queryengine_invalidations_total",
			Help: "Total cache invalidations",
		},
	)
	registry.RegisterCounter("queryengine", "invalidations_total", m.invalidationsTotal)

	m.invalidationsBatched = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "semstreams_queryengine_invalidations_batched_total",
			Help: "Total batched cache invalidations",
		},
	)
	registry.RegisterCounter("queryengine", "invalidations_batched_total", m.invalidationsBatched)

	m.invalidationDuration = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "semstreams_queryengine_invalidation_duration_seconds",
			Help:    "Duration of cache invalidations",
			Buckets: []float64{0.0001, 0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1.0},
		},
	)
	registry.RegisterHistogram("queryengine", "invalidation_duration_seconds", m.invalidationDuration)

	m.invalidationErrors = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "semstreams_queryengine_invalidation_errors_total",
			Help: "Total cache invalidation errors",
		},
	)
	registry.RegisterCounter("queryengine", "invalidation_errors_total", m.invalidationErrors)
}

// initializeQueryMetrics initializes query execution metrics
func (m *Metrics) initializeQueryMetrics(registry *metric.MetricsRegistry) {
	m.queryTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "semstreams_queryengine_query_total",
			Help: "Total query operations",
		},
		[]string{"component", "query_type", "status"},
	)
	registry.RegisterCounterVec("queryengine", "query_total", m.queryTotal)

	m.queryDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "semstreams_queryengine_query_duration_seconds",
			Help:    "Duration of query operations",
			Buckets: []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1.0, 5.0, 10.0, 30.0},
		},
		[]string{"component", "query_type"},
	)
	registry.RegisterHistogramVec("queryengine", "query_duration_seconds", m.queryDuration)

	m.queryResultSize = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "semstreams_queryengine_query_result_size",
			Help:    "Size of query results",
			Buckets: []float64{1, 10, 100, 1000, 10000, 100000},
		},
		[]string{"component", "query_type"},
	)
	registry.RegisterHistogramVec("queryengine", "query_result_size", m.queryResultSize)

	m.queryErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "semstreams_queryengine_query_errors_total",
			Help: "Total query errors",
		},
		[]string{"component", "query_type", "error_type"},
	)
	registry.RegisterCounterVec("queryengine", "query_errors_total", m.queryErrors)

	m.queryTimeouts = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "semstreams_queryengine_query_timeouts_total",
			Help: "Total query timeouts",
		},
		[]string{"component", "query_type"},
	)
	registry.RegisterCounterVec("queryengine", "query_timeouts_total", m.queryTimeouts)
}

// initializePathMetrics initializes path execution metrics
func (m *Metrics) initializePathMetrics(registry *metric.MetricsRegistry) {
	m.pathExecutionTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "semstreams_queryengine_path_execution_total",
			Help: "Total path execution operations",
		},
	)
	registry.RegisterCounter("queryengine", "path_execution_total", m.pathExecutionTotal)

	m.pathExecutionDuration = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "semstreams_queryengine_path_execution_duration_seconds",
			Help:    "Duration of path execution operations",
			Buckets: []float64{0.01, 0.05, 0.1, 0.5, 1.0, 5.0, 10.0},
		},
	)
	registry.RegisterHistogram("queryengine", "path_execution_duration_seconds", m.pathExecutionDuration)

	m.pathLengthHistogram = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "semstreams_queryengine_path_length",
			Help:    "Length of executed paths",
			Buckets: []float64{1, 2, 5, 10, 20, 50, 100},
		},
	)
	registry.RegisterHistogram("queryengine", "path_length", m.pathLengthHistogram)
}

// initializeSnapshotMetrics initializes snapshot metrics
func (m *Metrics) initializeSnapshotMetrics(registry *metric.MetricsRegistry) {
	m.snapshotTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "semstreams_queryengine_snapshot_total",
			Help: "Total snapshot operations",
		},
	)
	registry.RegisterCounter("queryengine", "snapshot_total", m.snapshotTotal)

	m.snapshotDuration = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "semstreams_queryengine_snapshot_duration_seconds",
			Help:    "Duration of snapshot operations",
			Buckets: []float64{0.1, 0.5, 1.0, 5.0, 10.0, 30.0, 60.0},
		},
	)
	registry.RegisterHistogram("queryengine", "snapshot_duration_seconds", m.snapshotDuration)

	m.snapshotSizeHistogram = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "semstreams_queryengine_snapshot_size",
			Help:    "Size of graph snapshots",
			Buckets: []float64{10, 100, 1000, 10000, 50000, 100000},
		},
	)
	registry.RegisterHistogram("queryengine", "snapshot_size", m.snapshotSizeHistogram)
}

// initializeWatchMetrics initializes KV watch metrics
func (m *Metrics) initializeWatchMetrics(registry *metric.MetricsRegistry) {
	m.watchEventsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "semstreams_queryengine_watch_events_total",
			Help: "Total KV watch events processed",
		},
		[]string{"component", "pattern", "operation"},
	)
	registry.RegisterCounterVec("queryengine", "watch_events_total", m.watchEventsTotal)

	m.watchEventsDuration = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "semstreams_queryengine_watch_events_duration_seconds",
			Help:    "Duration of processing KV watch events",
			Buckets: []float64{0.0001, 0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1.0},
		},
	)
	registry.RegisterHistogram("queryengine", "watch_events_duration_seconds", m.watchEventsDuration)

	m.watchErrors = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "semstreams_queryengine_watch_errors_total",
			Help: "Total KV watch errors",
		},
	)
	registry.RegisterCounter("queryengine", "watch_errors_total", m.watchErrors)

	m.watchLag = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "semstreams_queryengine_watch_lag_seconds",
			Help: "Lag between KV event and processing",
		},
	)
	registry.RegisterGauge("queryengine", "watch_lag_seconds", m.watchLag)

	m.activeWatchers = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "semstreams_queryengine_active_watchers",
			Help: "Number of active KV watchers",
		},
	)
	registry.RegisterGauge("queryengine", "active_watchers", m.activeWatchers)
}

// initializeIndexManagerMetrics initializes index manager dependency metrics
func (m *Metrics) initializeIndexManagerMetrics(registry *metric.MetricsRegistry) {
	m.indexManagerRequests = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "semstreams_queryengine_indexmanager_requests_total",
			Help: "Total index manager requests",
		},
		[]string{"component", "operation", "status"},
	)
	registry.RegisterCounterVec("queryengine", "indexmanager_requests_total", m.indexManagerRequests)

	m.indexManagerDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "semstreams_queryengine_indexmanager_duration_seconds",
			Help:    "Duration of index manager requests",
			Buckets: []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1.0, 2.0},
		},
		[]string{"component", "operation"},
	)
	registry.RegisterHistogramVec("queryengine", "indexmanager_duration_seconds", m.indexManagerDuration)

	m.indexManagerErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "semstreams_queryengine_indexmanager_errors_total",
			Help: "Total index manager errors",
		},
		[]string{"component", "operation", "error_type"},
	)
	registry.RegisterCounterVec("queryengine", "indexmanager_errors_total", m.indexManagerErrors)
}

// initializeHealthMetrics initializes health monitoring metrics
func (m *Metrics) initializeHealthMetrics(registry *metric.MetricsRegistry) {
	m.healthCheckDuration = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "semstreams_queryengine_health_check_duration_seconds",
			Help:    "Duration of health checks",
			Buckets: []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1.0},
		},
	)
	registry.RegisterHistogram("queryengine", "health_check_duration_seconds", m.healthCheckDuration)

	m.componentHealth = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "semstreams_queryengine_component_health",
			Help: "Health status of query manager components",
		},
		[]string{"component", "component_type"},
	)
	registry.RegisterGaugeVec("queryengine", "component_health", m.componentHealth)

	m.memoryUsage = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "semstreams_queryengine_memory_usage_bytes",
			Help: "Memory usage of query manager",
		},
	)
	registry.RegisterGauge("queryengine", "memory_usage_bytes", m.memoryUsage)
}

// RecordEntityGet records an entity get operation
func (m *Metrics) RecordEntityGet(component string, duration time.Duration, cacheLayer string, success bool) {
	status := "success"
	if !success {
		status = "error"
	}
	m.entityGetTotal.WithLabelValues(component, status).Inc()
	m.entityGetDuration.WithLabelValues(component, cacheLayer).Observe(duration.Seconds())
}

// RecordCacheHit records a cache hit for the specified layer
func (m *Metrics) RecordCacheHit(layer string) {
	switch layer {
	case "l1":
		m.l1CacheHits.Inc()
	case "l2":
		m.l2CacheHits.Inc()
		m.cachePromotions.Inc() // L2 hit promotes to L1
	case "l3":
		m.l3CacheHits.Inc()
	}
}

// RecordCacheMiss records a cache miss for the specified layer
func (m *Metrics) RecordCacheMiss(layer string) {
	switch layer {
	case "l1":
		m.l1CacheMisses.Inc()
	case "l2":
		m.l2CacheMisses.Inc()
	case "l3":
		m.l3CacheMisses.Inc()
	}
}

// RecordKVFetch records a KV fetch operation (cache miss)
func (m *Metrics) RecordKVFetch(duration time.Duration) {
	m.kvFetches.Inc()
	m.kvFetchDuration.Observe(duration.Seconds())
}

// RecordInvalidation records a cache invalidation
func (m *Metrics) RecordInvalidation(duration time.Duration, batched bool, success bool) {
	m.invalidationsTotal.Inc()
	if batched {
		m.invalidationsBatched.Inc()
	}
	m.invalidationDuration.Observe(duration.Seconds())
	if !success {
		m.invalidationErrors.Inc()
	}
}

// RecordQuery records a query operation
func (m *Metrics) RecordQuery(component, queryType string, duration time.Duration, resultSize int, success bool) {
	status := "success"
	if !success {
		status = "error"
	}
	m.queryTotal.WithLabelValues(component, queryType, status).Inc()
	m.queryDuration.WithLabelValues(component, queryType).Observe(duration.Seconds())
	m.queryResultSize.WithLabelValues(component, queryType).Observe(float64(resultSize))
}

// UpdateCacheStats updates cache statistics
func (m *Metrics) UpdateCacheStats(stats CacheStats) {
	// Update L1 stats
	m.l1CacheSize.Set(float64(stats.L1Size))

	// Update L2 stats
	m.l2CacheSize.Set(float64(stats.L2Size))

	// Update L3 stats
	m.l3CacheSize.Set(float64(stats.L3Size))

	// Update overall stats
	m.cacheHitRatio.Set(stats.OverallHitRatio)
}

// RecordWatchEvent records a KV watch event
func (m *Metrics) RecordWatchEvent(component, pattern, operation string, duration time.Duration) {
	m.watchEventsTotal.WithLabelValues(component, pattern, operation).Inc()
	m.watchEventsDuration.Observe(duration.Seconds())
}

// UpdateWatchHealth updates KV watch health metrics
func (m *Metrics) UpdateWatchHealth(activeWatchers int, lagSeconds float64) {
	m.activeWatchers.Set(float64(activeWatchers))
	m.watchLag.Set(lagSeconds)
}

// RecordIndexManagerRequest records an index manager request
func (m *Metrics) RecordIndexManagerRequest(component, operation string, duration time.Duration, success bool) {
	status := "success"
	if !success {
		status = "error"
	}
	m.indexManagerRequests.WithLabelValues(component, operation, status).Inc()
	m.indexManagerDuration.WithLabelValues(component, operation).Observe(duration.Seconds())
}
