// Package graphembedding provides Prometheus metrics for graph-embedding component.
package graphembedding

import (
	"sync"

	"github.com/c360studio/semstreams/metric"
	"github.com/prometheus/client_golang/prometheus"
)

// embeddingMetrics holds Prometheus metrics for the graph-embedding component.
type embeddingMetrics struct {
	embedderType        prometheus.Gauge // 0=disabled, 1=bm25, 2=http
	embeddingsGenerated prometheus.Counter
	embeddingErrors     prometheus.Counter
	embeddingDedupHits  prometheus.Counter
	embeddingPending    prometheus.Gauge
	kvOperations        *prometheus.CounterVec
	// contentUnresolved counts entities whose offloaded BODY (a StorageRef)
	// could not be fetched because no content store is wired, so that body was
	// excluded from the embedding (gh#414). The entity may still be embedded from
	// any inline text triples it carries; a rising value means offloaded body
	// text is being dropped from embeddings — wire a store-read port.
	contentUnresolved prometheus.Counter
}

// Package-level metrics (registered once to avoid duplicate registration errors)
var (
	metricsOnce sync.Once
	metrics     *embeddingMetrics
)

// getMetrics returns the singleton metrics instance, creating and registering it if needed.
func getMetrics(registry *metric.MetricsRegistry) *embeddingMetrics {
	metricsOnce.Do(func() {
		metrics = &embeddingMetrics{
			embedderType: prometheus.NewGauge(prometheus.GaugeOpts{
				Namespace: "semstreams",
				Subsystem: "graph_embedding",
				Name:      "embedder_type",
				Help:      "Embedder type: 0=disabled, 1=bm25, 2=http",
			}),

			embeddingsGenerated: prometheus.NewCounter(prometheus.CounterOpts{
				Namespace: "semstreams",
				Subsystem: "graph_embedding",
				Name:      "embeddings_generated_total",
				Help:      "Total embeddings generated",
			}),

			embeddingErrors: prometheus.NewCounter(prometheus.CounterOpts{
				Namespace: "semstreams",
				Subsystem: "graph_embedding",
				Name:      "errors_total",
				Help:      "Total embedding generation errors",
			}),

			kvOperations: prometheus.NewCounterVec(prometheus.CounterOpts{
				Namespace: "semstreams",
				Subsystem: "graph_embedding",
				Name:      "kv_operations_total",
				Help:      "Total KV bucket operations",
			}, []string{"operation", "kv_bucket"}),

			embeddingDedupHits: prometheus.NewCounter(prometheus.CounterOpts{
				Namespace: "semstreams",
				Subsystem: "graph_embedding",
				Name:      "dedup_hits_total",
				Help:      "Total embedding deduplication cache hits",
			}),

			embeddingPending: prometheus.NewGauge(prometheus.GaugeOpts{
				Namespace: "semstreams",
				Subsystem: "graph_embedding",
				Name:      "pending",
				Help:      "Current number of pending embeddings",
			}),

			contentUnresolved: prometheus.NewCounter(prometheus.CounterOpts{
				Namespace: "semstreams",
				Subsystem: "graph_embedding",
				Name:      "content_unresolved_total",
				Help:      "Entities whose offloaded body (StorageRef) was excluded from embedding because no content store is wired; inline text, if any, is still embedded (gh#414)",
			}),
		}

		// Register metrics with the metrics registry if available
		if registry != nil {
			_ = registry.RegisterGauge("graph-embedding", "embedder_type", metrics.embedderType)
			_ = registry.RegisterCounter("graph-embedding", "embeddings_generated_total", metrics.embeddingsGenerated)
			_ = registry.RegisterCounter("graph-embedding", "errors_total", metrics.embeddingErrors)
			_ = registry.RegisterCounterVec("graph-embedding", "kv_operations_total", metrics.kvOperations)
			_ = registry.RegisterCounter("graph-embedding", "dedup_hits_total", metrics.embeddingDedupHits)
			_ = registry.RegisterGauge("graph-embedding", "pending", metrics.embeddingPending)
			_ = registry.RegisterCounter("graph-embedding", "content_unresolved_total", metrics.contentUnresolved)
		} else {
			// Fallback to default prometheus registry for testing
			_ = prometheus.DefaultRegisterer.Register(metrics.embedderType)
			_ = prometheus.DefaultRegisterer.Register(metrics.embeddingsGenerated)
			_ = prometheus.DefaultRegisterer.Register(metrics.embeddingErrors)
			_ = prometheus.DefaultRegisterer.Register(metrics.kvOperations)
			_ = prometheus.DefaultRegisterer.Register(metrics.embeddingDedupHits)
			_ = prometheus.DefaultRegisterer.Register(metrics.embeddingPending)
			_ = prometheus.DefaultRegisterer.Register(metrics.contentUnresolved)
		}
	})
	return metrics
}

// setEmbedderType sets the embedder type gauge.
// 0=disabled, 1=bm25, 2=http
func (m *embeddingMetrics) setEmbedderType(embedderType string) {
	var value float64
	switch embedderType {
	case "http":
		value = 2
	case "bm25":
		value = 1
	default:
		value = 0
	}
	m.embedderType.Set(value)
}

// recordEmbeddingGenerated increments the embeddings generated counter.
func (m *embeddingMetrics) recordEmbeddingGenerated() {
	m.embeddingsGenerated.Inc()
}

// recordEmbeddingError increments the embedding error counter.
func (m *embeddingMetrics) recordEmbeddingError() {
	m.embeddingErrors.Inc()
}

// recordKVOperation records a KV operation for the given operation type and bucket.
func (m *embeddingMetrics) recordKVOperation(operation, bucket string) {
	m.kvOperations.WithLabelValues(operation, bucket).Inc()
}

// recordDedupHit increments the deduplication hits counter.
func (m *embeddingMetrics) recordDedupHit() {
	m.embeddingDedupHits.Inc()
}

// recordContentUnresolved increments the counter for offloaded content that was
// excluded from embedding because no content store is wired (gh#414).
func (m *embeddingMetrics) recordContentUnresolved() {
	m.contentUnresolved.Inc()
}

// setPending sets the pending embeddings gauge.
func (m *embeddingMetrics) setPending(count float64) {
	m.embeddingPending.Set(count)
}

// workerMetricsAdapter adapts embeddingMetrics to the embedding.WorkerMetrics interface.
// This allows the Worker to report metrics without direct dependency on prometheus.
type workerMetricsAdapter struct {
	metrics *embeddingMetrics
}

// IncDedupHits implements embedding.WorkerMetrics.
func (a *workerMetricsAdapter) IncDedupHits() {
	if a.metrics != nil {
		a.metrics.recordDedupHit()
	}
}

// IncFailed implements embedding.WorkerMetrics.
func (a *workerMetricsAdapter) IncFailed() {
	if a.metrics != nil {
		a.metrics.recordEmbeddingError()
	}
}

// SetPending implements embedding.WorkerMetrics.
func (a *workerMetricsAdapter) SetPending(count float64) {
	if a.metrics != nil {
		a.metrics.setPending(count)
	}
}

// newWorkerMetricsAdapter creates an adapter for the embedding.WorkerMetrics interface.
func newWorkerMetricsAdapter(m *embeddingMetrics) *workerMetricsAdapter {
	return &workerMetricsAdapter{metrics: m}
}
