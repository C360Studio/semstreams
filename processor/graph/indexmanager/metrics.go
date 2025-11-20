package indexmanager

import (
	"sync"
	"time"

	"github.com/c360/semstreams/metric"
	"github.com/prometheus/client_golang/prometheus"
)

// PrometheusMetrics provides Prometheus metrics for IndexEngine
type PrometheusMetrics struct {
	// Event processing metrics
	eventsTotal     prometheus.Counter
	eventsProcessed prometheus.Counter
	eventsFailed    prometheus.Counter
	eventsDropped   prometheus.Counter
	processLatency  prometheus.Histogram

	// Deduplication metrics
	duplicateEvents    prometheus.Counter
	deduplicationCache prometheus.Gauge

	// Index operation metrics
	indexUpdatesTotal  prometheus.CounterVec
	indexUpdatesFailed prometheus.CounterVec
	indexUpdateLatency prometheus.HistogramVec

	// Query metrics
	queriesTotal  prometheus.CounterVec
	queriesFailed prometheus.CounterVec
	queryLatency  prometheus.HistogramVec

	// Buffer metrics
	bufferSize        prometheus.Gauge
	bufferUtilization prometheus.Gauge
	backlogSize       prometheus.Gauge

	// Health metrics
	healthStatus  prometheus.Gauge
	processingLag prometheus.Gauge
	errorCount    prometheus.Counter

	// KV Watch metrics
	watchEventsTotal   prometheus.Counter
	watchEventsFailed  prometheus.Counter
	watchReconnections prometheus.Counter
	watchersActive     prometheus.Gauge

	// Embedding metrics
	embeddingProvider        prometheus.Gauge   // 0=disabled, 1=bm25, 2=http
	embeddingsGenerated      prometheus.Counter // Total embeddings generated
	embeddingCacheHits       prometheus.Counter // L2 NATS cache hits
	embeddingCacheMisses     prometheus.Counter // L2 NATS cache misses
	embeddingLatency         prometheus.Histogram
	embeddingFallbacks       prometheus.Counter // HTTP → BM25 fallback events
	embeddingsActive         prometheus.Gauge   // Current count in L1 cache
	embeddingTextExtractions prometheus.Counter // Total text extractions
}

// NewPrometheusMetrics creates a new PrometheusMetrics instance using MetricsRegistry
func NewPrometheusMetrics(component string, registry *metric.MetricsRegistry) *PrometheusMetrics {
	if registry == nil {
		return nil
	}

	// Create simple metrics and register them
	eventsTotal := prometheus.NewCounter(prometheus.CounterOpts{
		Name:        "indexengine_events_total",
		Help:        "Total number of KV change events received",
		ConstLabels: prometheus.Labels{"component": component},
	})
	registry.RegisterCounter("indexengine", "events_total", eventsTotal)

	eventsProcessed := prometheus.NewCounter(prometheus.CounterOpts{
		Name:        "indexengine_events_processed_total",
		Help:        "Total number of events successfully processed",
		ConstLabels: prometheus.Labels{"component": component},
	})
	registry.RegisterCounter("indexengine", "events_processed_total", eventsProcessed)

	eventsFailed := prometheus.NewCounter(prometheus.CounterOpts{
		Name:        "indexengine_events_failed_total",
		Help:        "Total number of events that failed processing",
		ConstLabels: prometheus.Labels{"component": component},
	})
	registry.RegisterCounter("indexengine", "events_failed_total", eventsFailed)

	eventsDropped := prometheus.NewCounter(prometheus.CounterOpts{
		Name:        "indexengine_events_dropped_total",
		Help:        "Total number of events dropped due to buffer overflow",
		ConstLabels: prometheus.Labels{"component": component},
	})
	registry.RegisterCounter("indexengine", "events_dropped_total", eventsDropped)

	processLatency := prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:        "indexengine_process_latency_seconds",
		Help:        "Latency of event processing in seconds",
		ConstLabels: prometheus.Labels{"component": component},
		Buckets:     []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1.0, 2.0, 5.0},
	})
	registry.RegisterHistogram("indexengine", "process_latency_seconds", processLatency)

	duplicateEvents := prometheus.NewCounter(prometheus.CounterOpts{
		Name:        "indexengine_duplicate_events_total",
		Help:        "Total number of duplicate events detected and skipped",
		ConstLabels: prometheus.Labels{"component": component},
	})
	registry.RegisterCounter("indexengine", "duplicate_events_total", duplicateEvents)

	deduplicationCache := prometheus.NewGauge(prometheus.GaugeOpts{
		Name:        "indexengine_deduplication_cache_size",
		Help:        "Current size of the deduplication cache",
		ConstLabels: prometheus.Labels{"component": component},
	})
	registry.RegisterGauge("indexengine", "deduplication_cache_size", deduplicationCache)

	// Create vectorized metrics and register them
	indexUpdatesTotal := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name:        "indexengine_index_updates_total",
		Help:        "Total number of index updates by type",
		ConstLabels: prometheus.Labels{"component": component},
	}, []string{"index_type", "operation"})
	registry.RegisterCounterVec("indexengine", "index_updates_total", indexUpdatesTotal)

	indexUpdatesFailed := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name:        "indexengine_index_updates_failed_total",
		Help:        "Total number of failed index updates by type",
		ConstLabels: prometheus.Labels{"component": component},
	}, []string{"index_type", "operation"})
	registry.RegisterCounterVec("indexengine", "index_updates_failed_total", indexUpdatesFailed)

	indexUpdateLatency := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:        "indexengine_index_update_latency_seconds",
		Help:        "Latency of index updates in seconds by type",
		ConstLabels: prometheus.Labels{"component": component},
		Buckets:     []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1.0},
	}, []string{"index_type"})
	registry.RegisterHistogramVec("indexengine", "index_update_latency_seconds", indexUpdateLatency)

	queriesTotal := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name:        "indexengine_queries_total",
		Help:        "Total number of queries by type",
		ConstLabels: prometheus.Labels{"component": component},
	}, []string{"query_type"})
	registry.RegisterCounterVec("indexengine", "queries_total", queriesTotal)

	queriesFailed := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name:        "indexengine_queries_failed_total",
		Help:        "Total number of failed queries by type",
		ConstLabels: prometheus.Labels{"component": component},
	}, []string{"query_type"})
	registry.RegisterCounterVec("indexengine", "queries_failed_total", queriesFailed)

	queryLatency := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:        "indexengine_query_latency_seconds",
		Help:        "Latency of queries in seconds by type",
		ConstLabels: prometheus.Labels{"component": component},
		Buckets:     []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1.0},
	}, []string{"query_type"})
	registry.RegisterHistogramVec("indexengine", "query_latency_seconds", queryLatency)

	bufferSize := prometheus.NewGauge(prometheus.GaugeOpts{
		Name:        "indexengine_buffer_size",
		Help:        "Current number of events in the buffer",
		ConstLabels: prometheus.Labels{"component": component},
	})
	registry.RegisterGauge("indexengine", "buffer_size", bufferSize)

	bufferUtilization := prometheus.NewGauge(prometheus.GaugeOpts{
		Name:        "indexengine_buffer_utilization",
		Help:        "Buffer utilization as a ratio (0-1)",
		ConstLabels: prometheus.Labels{"component": component},
	})
	registry.RegisterGauge("indexengine", "buffer_utilization", bufferUtilization)

	backlogSize := prometheus.NewGauge(prometheus.GaugeOpts{
		Name:        "indexengine_backlog_size",
		Help:        "Current number of events waiting to be processed",
		ConstLabels: prometheus.Labels{"component": component},
	})
	registry.RegisterGauge("indexengine", "backlog_size", backlogSize)

	healthStatus := prometheus.NewGauge(prometheus.GaugeOpts{
		Name:        "indexengine_health_status",
		Help:        "Health status of the IndexEngine (1=healthy, 0=unhealthy)",
		ConstLabels: prometheus.Labels{"component": component},
	})
	registry.RegisterGauge("indexengine", "health_status", healthStatus)

	processingLag := prometheus.NewGauge(prometheus.GaugeOpts{
		Name:        "indexengine_processing_lag_seconds",
		Help:        "Current processing lag in seconds",
		ConstLabels: prometheus.Labels{"component": component},
	})
	registry.RegisterGauge("indexengine", "processing_lag_seconds", processingLag)

	errorCount := prometheus.NewCounter(prometheus.CounterOpts{
		Name:        "indexengine_errors_total",
		Help:        "Total number of errors encountered",
		ConstLabels: prometheus.Labels{"component": component},
	})
	registry.RegisterCounter("indexengine", "errors_total", errorCount)

	watchEventsTotal := prometheus.NewCounter(prometheus.CounterOpts{
		Name:        "indexengine_watch_events_total",
		Help:        "Total number of KV watch events received",
		ConstLabels: prometheus.Labels{"component": component},
	})
	registry.RegisterCounter("indexengine", "watch_events_total", watchEventsTotal)

	watchEventsFailed := prometheus.NewCounter(prometheus.CounterOpts{
		Name:        "indexengine_watch_events_failed_total",
		Help:        "Total number of KV watch events that failed",
		ConstLabels: prometheus.Labels{"component": component},
	})
	registry.RegisterCounter("indexengine", "watch_events_failed_total", watchEventsFailed)

	watchReconnections := prometheus.NewCounter(prometheus.CounterOpts{
		Name:        "indexengine_watch_reconnections_total",
		Help:        "Total number of KV watch reconnections",
		ConstLabels: prometheus.Labels{"component": component},
	})
	registry.RegisterCounter("indexengine", "watch_reconnections_total", watchReconnections)

	watchersActive := prometheus.NewGauge(prometheus.GaugeOpts{
		Name:        "indexengine_watchers_active",
		Help:        "Number of active KV watchers",
		ConstLabels: prometheus.Labels{"component": component},
	})
	registry.RegisterGauge("indexengine", "watchers_active", watchersActive)

	// Embedding metrics
	embeddingProvider := prometheus.NewGauge(prometheus.GaugeOpts{
		Name:        "indexengine_embedding_provider",
		Help:        "Active embedding provider (0=disabled, 1=bm25, 2=http)",
		ConstLabels: prometheus.Labels{"component": component},
	})
	registry.RegisterGauge("indexengine", "embedding_provider", embeddingProvider)

	embeddingsGenerated := prometheus.NewCounter(prometheus.CounterOpts{
		Name:        "indexengine_embeddings_generated_total",
		Help:        "Total number of embeddings generated",
		ConstLabels: prometheus.Labels{"component": component},
	})
	registry.RegisterCounter("indexengine", "embeddings_generated_total", embeddingsGenerated)

	embeddingCacheHits := prometheus.NewCounter(prometheus.CounterOpts{
		Name:        "indexengine_embedding_cache_hits_total",
		Help:        "Total number of L2 NATS cache hits for embeddings",
		ConstLabels: prometheus.Labels{"component": component},
	})
	registry.RegisterCounter("indexengine", "embedding_cache_hits_total", embeddingCacheHits)

	embeddingCacheMisses := prometheus.NewCounter(prometheus.CounterOpts{
		Name:        "indexengine_embedding_cache_misses_total",
		Help:        "Total number of L2 NATS cache misses for embeddings",
		ConstLabels: prometheus.Labels{"component": component},
	})
	registry.RegisterCounter("indexengine", "embedding_cache_misses_total", embeddingCacheMisses)

	embeddingLatency := prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:        "indexengine_embedding_generation_latency_seconds",
		Help:        "Latency of embedding generation in seconds",
		ConstLabels: prometheus.Labels{"component": component},
		Buckets:     []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1.0, 2.0, 5.0},
	})
	registry.RegisterHistogram("indexengine", "embedding_generation_latency_seconds", embeddingLatency)

	embeddingFallbacks := prometheus.NewCounter(prometheus.CounterOpts{
		Name:        "indexengine_embedding_fallbacks_total",
		Help:        "Total number of HTTP → BM25 fallback events",
		ConstLabels: prometheus.Labels{"component": component},
	})
	registry.RegisterCounter("indexengine", "embedding_fallbacks_total", embeddingFallbacks)

	embeddingsActive := prometheus.NewGauge(prometheus.GaugeOpts{
		Name:        "indexengine_embeddings_active",
		Help:        "Current number of embeddings in L1 memory cache",
		ConstLabels: prometheus.Labels{"component": component},
	})
	registry.RegisterGauge("indexengine", "embeddings_active", embeddingsActive)

	embeddingTextExtractions := prometheus.NewCounter(prometheus.CounterOpts{
		Name:        "indexengine_embedding_text_extractions_total",
		Help:        "Total number of text extractions from entities for embedding",
		ConstLabels: prometheus.Labels{"component": component},
	})
	registry.RegisterCounter("indexengine", "embedding_text_extractions_total", embeddingTextExtractions)

	return &PrometheusMetrics{
		eventsTotal:              eventsTotal,
		eventsProcessed:          eventsProcessed,
		eventsFailed:             eventsFailed,
		eventsDropped:            eventsDropped,
		processLatency:           processLatency,
		duplicateEvents:          duplicateEvents,
		deduplicationCache:       deduplicationCache,
		indexUpdatesTotal:        *indexUpdatesTotal,
		indexUpdatesFailed:       *indexUpdatesFailed,
		indexUpdateLatency:       *indexUpdateLatency,
		queriesTotal:             *queriesTotal,
		queriesFailed:            *queriesFailed,
		queryLatency:             *queryLatency,
		bufferSize:               bufferSize,
		bufferUtilization:        bufferUtilization,
		backlogSize:              backlogSize,
		healthStatus:             healthStatus,
		processingLag:            processingLag,
		errorCount:               errorCount,
		watchEventsTotal:         watchEventsTotal,
		watchEventsFailed:        watchEventsFailed,
		watchReconnections:       watchReconnections,
		watchersActive:           watchersActive,
		embeddingProvider:        embeddingProvider,
		embeddingsGenerated:      embeddingsGenerated,
		embeddingCacheHits:       embeddingCacheHits,
		embeddingCacheMisses:     embeddingCacheMisses,
		embeddingLatency:         embeddingLatency,
		embeddingFallbacks:       embeddingFallbacks,
		embeddingsActive:         embeddingsActive,
		embeddingTextExtractions: embeddingTextExtractions,
	}
}

// InternalMetrics tracks metrics that are not exposed via Prometheus
type InternalMetrics struct {
	mu sync.RWMutex

	// Counters
	eventsTotal     int64
	eventsProcessed int64
	eventsFailed    int64
	eventsDropped   int64
	duplicateEvents int64
	queriesTotal    int64
	queriesFailed   int64
	errorCount      int64

	// Timing
	lastSuccess   time.Time
	lastError     time.Time
	lastProcessed time.Time
	processingLag time.Duration

	// Status
	isHealthy      bool
	lastErrorMsg   string
	bufferSize     int
	backlogSize    int
	watchersActive int
}

// NewInternalMetrics creates a new InternalMetrics instance
func NewInternalMetrics() *InternalMetrics {
	return &InternalMetrics{
		isHealthy:   true,
		lastSuccess: time.Now(),
	}
}

// RecordEventReceived increments the events received counter
func (m *InternalMetrics) RecordEventReceived() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.eventsTotal++
}

// RecordEventProcessed increments the events processed counter
func (m *InternalMetrics) RecordEventProcessed() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.eventsProcessed++
	m.lastProcessed = time.Now()
	m.lastSuccess = time.Now()
}

// RecordEventFailed increments the events failed counter
func (m *InternalMetrics) RecordEventFailed(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.eventsFailed++
	m.errorCount++
	m.lastError = time.Now()
	if err != nil {
		m.lastErrorMsg = err.Error()
	}
}

// RecordEventDropped increments the events dropped counter
func (m *InternalMetrics) RecordEventDropped() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.eventsDropped++
}

// RecordDuplicateEvent increments the duplicate events counter
func (m *InternalMetrics) RecordDuplicateEvent() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.duplicateEvents++
}

// RecordQuery increments the query counters
func (m *InternalMetrics) RecordQuery(failed bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.queriesTotal++
	if failed {
		m.queriesFailed++
		m.errorCount++
		m.lastError = time.Now()
	} else {
		m.lastSuccess = time.Now()
	}
}

// UpdateBufferSize updates the current buffer size
func (m *InternalMetrics) UpdateBufferSize(size int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.bufferSize = size
}

// UpdateBacklogSize updates the current backlog size
func (m *InternalMetrics) UpdateBacklogSize(size int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.backlogSize = size
}

// UpdateProcessingLag updates the current processing lag
func (m *InternalMetrics) UpdateProcessingLag(lag time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.processingLag = lag
}

// UpdateHealthStatus updates the health status
func (m *InternalMetrics) UpdateHealthStatus(healthy bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.isHealthy = healthy
}

// UpdateWatchersActive updates the number of active watchers
func (m *InternalMetrics) UpdateWatchersActive(count int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.watchersActive = count
}

// GetSnapshot returns a snapshot of current metrics
func (m *InternalMetrics) GetSnapshot() Metrics {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return Metrics{
		EventsTotal:       m.eventsTotal,
		EventsProcessed:   m.eventsProcessed,
		EventsFailed:      m.eventsFailed,
		EventsDropped:     m.eventsDropped,
		DuplicateEvents:   m.duplicateEvents,
		QueriesTotal:      m.queriesTotal,
		QueriesFailed:     m.queriesFailed,
		BufferSize:        m.bufferSize,
		BacklogSize:       m.backlogSize,
		IsHealthy:         m.isHealthy,
		LastError:         m.lastErrorMsg,
		LastSuccess:       m.lastSuccess,
		ProcessingLag:     m.processingLag,
		DeduplicationRate: m.calculateDeduplicationRate(),
	}
}

// calculateDeduplicationRate calculates the deduplication rate
func (m *InternalMetrics) calculateDeduplicationRate() float64 {
	if m.eventsTotal == 0 {
		return 0.0
	}
	return float64(m.duplicateEvents) / float64(m.eventsTotal)
}

// GetDeduplicationStats returns deduplication statistics
func (m *InternalMetrics) GetDeduplicationStats() DeduplicationStats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return DeduplicationStats{
		TotalEvents:       m.eventsTotal,
		DuplicateEvents:   m.duplicateEvents,
		ProcessedEvents:   m.eventsProcessed,
		DeduplicationRate: m.calculateDeduplicationRate(),
		// CacheSize and CacheHitRate will be filled by the cache implementation
	}
}

// Reset resets all metrics to zero (used for testing)
func (m *InternalMetrics) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.eventsTotal = 0
	m.eventsProcessed = 0
	m.eventsFailed = 0
	m.eventsDropped = 0
	m.duplicateEvents = 0
	m.queriesTotal = 0
	m.queriesFailed = 0
	m.errorCount = 0
	m.bufferSize = 0
	m.backlogSize = 0
	m.watchersActive = 0
	m.processingLag = 0
	m.isHealthy = true
	m.lastErrorMsg = ""
	m.lastSuccess = time.Now()
	m.lastError = time.Time{}
	m.lastProcessed = time.Time{}
}
