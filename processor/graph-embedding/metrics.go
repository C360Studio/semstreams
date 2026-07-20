// Package graphembedding provides Prometheus metrics for graph-embedding component.
package graphembedding

import (
	"sync"

	"github.com/c360studio/semstreams/graph"
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
	// Readiness envelope gauges (ADR-066 §3): the honest Ready/lag/watermark numbers
	// computeEmbeddingStatus already answers over NATS, now scrapeable so an operator
	// can dashboard/alert without a per-sample status request (#579 at the source).
	readiness       prometheus.Gauge     // 1 when Ready (pipeline caught up to target), else 0
	lag             prometheus.Gauge     // revisions behind target (0 = caught up)
	indexedRevision prometheus.Gauge     // low-water-of-pending watermark
	targetRevision  prometheus.Gauge     // ENTITY_STATES stream LastSeq target
	readinessState  *prometheus.GaugeVec // one-hot over building|ready|degraded|reset_required
	// contentUnresolved counts entities whose offloaded BODY (a StorageRef)
	// could not be fetched because no content store is wired, so that body was
	// excluded from the embedding (gh#414). The entity may still be embedded from
	// any inline text triples it carries; a rising value means offloaded body
	// text is being dropped from embeddings — wire a store-read port.
	contentUnresolved prometheus.Counter
	// contentResolveError counts body fetches that FAILED after a store was
	// resolved (ADR-063 M1): the StorageInstance resolved to a live store, but the
	// Open/read errored (network fault, deleted bucket, closed handle). Distinct
	// from contentUnresolved (no store at all) so operators can tell a wiring gap
	// from a failing backend.
	contentResolveError prometheus.Counter
	// contentResolved counts offloaded bodies successfully fetched from a resolved
	// store — the POSITIVE observable for the ADR-063 H2 behavior change (configs
	// without a store-read port now embed bodies they previously excluded). Rising
	// value = the inclusion happening, not merely content_unresolved falling.
	contentResolved prometheus.Counter
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

			contentResolveError: prometheus.NewCounter(prometheus.CounterOpts{
				Namespace: "semstreams",
				Subsystem: "graph_embedding",
				Name:      "content_resolve_error_total",
				Help:      "Offloaded body fetches that failed after a store was resolved (infra fault: read error, deleted bucket); distinct from content_unresolved which is a missing wiring (ADR-063)",
			}),

			contentResolved: prometheus.NewCounter(prometheus.CounterOpts{
				Namespace: "semstreams",
				Subsystem: "graph_embedding",
				Name:      "content_resolved_total",
				Help:      "Offloaded bodies successfully fetched from a resolved store — the positive signal that ADR-063 federated resolution is including bodies previously excluded",
			}),

			readiness: prometheus.NewGauge(prometheus.GaugeOpts{
				Namespace: "semstreams",
				Subsystem: "graph_embedding",
				Name:      "readiness",
				Help:      "1 when the readiness envelope is Ready (embedding pipeline caught up to target revision), else 0 (ADR-066)",
			}),

			lag: prometheus.NewGauge(prometheus.GaugeOpts{
				Namespace: "semstreams",
				Subsystem: "graph_embedding",
				Name:      "lag",
				Help:      "Revisions the embedding pipeline is behind the ENTITY_STATES target (target_revision - indexed_revision; 0 = caught up)",
			}),

			indexedRevision: prometheus.NewGauge(prometheus.GaugeOpts{
				Namespace: "semstreams",
				Subsystem: "graph_embedding",
				Name:      "indexed_revision",
				Help:      "Low-water-of-pending watermark: every ENTITY_STATES revision <= this reached a terminal embedding outcome (ADR-066)",
			}),

			targetRevision: prometheus.NewGauge(prometheus.GaugeOpts{
				Namespace: "semstreams",
				Subsystem: "graph_embedding",
				Name:      "target_revision",
				Help:      "ENTITY_STATES stream LastSeq the embedding pipeline must catch up to (ADR-066)",
			}),

			readinessState: prometheus.NewGaugeVec(prometheus.GaugeOpts{
				Namespace: "semstreams",
				Subsystem: "graph_embedding",
				Name:      "readiness_state",
				Help:      "Readiness state one-hot (building|ready|degraded|reset_required): current state=1, others=0, so catching-up is distinguishable from broken",
			}, []string{"state"}),
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
			_ = registry.RegisterCounter("graph-embedding", "content_resolve_error_total", metrics.contentResolveError)
			_ = registry.RegisterCounter("graph-embedding", "content_resolved_total", metrics.contentResolved)
			_ = registry.RegisterGauge("graph-embedding", "readiness", metrics.readiness)
			_ = registry.RegisterGauge("graph-embedding", "lag", metrics.lag)
			_ = registry.RegisterGauge("graph-embedding", "indexed_revision", metrics.indexedRevision)
			_ = registry.RegisterGauge("graph-embedding", "target_revision", metrics.targetRevision)
			_ = registry.RegisterGaugeVec("graph-embedding", "readiness_state", metrics.readinessState)
		} else {
			// Fallback to default prometheus registry for testing
			_ = prometheus.DefaultRegisterer.Register(metrics.embedderType)
			_ = prometheus.DefaultRegisterer.Register(metrics.embeddingsGenerated)
			_ = prometheus.DefaultRegisterer.Register(metrics.embeddingErrors)
			_ = prometheus.DefaultRegisterer.Register(metrics.kvOperations)
			_ = prometheus.DefaultRegisterer.Register(metrics.embeddingDedupHits)
			_ = prometheus.DefaultRegisterer.Register(metrics.embeddingPending)
			_ = prometheus.DefaultRegisterer.Register(metrics.contentUnresolved)
			_ = prometheus.DefaultRegisterer.Register(metrics.contentResolveError)
			_ = prometheus.DefaultRegisterer.Register(metrics.contentResolved)
			_ = prometheus.DefaultRegisterer.Register(metrics.readiness)
			_ = prometheus.DefaultRegisterer.Register(metrics.lag)
			_ = prometheus.DefaultRegisterer.Register(metrics.indexedRevision)
			_ = prometheus.DefaultRegisterer.Register(metrics.targetRevision)
			_ = prometheus.DefaultRegisterer.Register(metrics.readinessState)
		}
	})
	return metrics
}

// setReadinessGauges publishes the ADR-066 readiness envelope as Prometheus gauges.
// It is pure over resp (no compute, no NATS) so it is unit-testable and cheap to call
// on a tick. The state gauge is one-hot: the current state is set to 1 and every other
// state to 0, so a stale state can never linger at 1 — a "ready" pipeline that later
// degrades reads degraded=1, ready=0, not both.
func (m *embeddingMetrics) setReadinessGauges(resp graph.IndexStatusResponse) {
	var ready float64
	if resp.Ready {
		ready = 1
	}
	m.readiness.Set(ready)
	m.lag.Set(float64(resp.Lag))
	m.indexedRevision.Set(float64(resp.IndexedRevision))
	m.targetRevision.Set(float64(resp.TargetRevision))
	for _, s := range graph.AllIndexStates {
		v := 0.0
		if s == resp.State {
			v = 1
		}
		m.readinessState.WithLabelValues(s).Set(v)
	}
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

// recordContentResolveError increments the counter for offloaded content that
// resolved to a store but failed to fetch (ADR-063 M1).
func (m *embeddingMetrics) recordContentResolveError() {
	m.contentResolveError.Inc()
}

// recordContentResolved increments the counter for offloaded content
// successfully fetched from a resolved store (ADR-063 H2 positive observable).
func (m *embeddingMetrics) recordContentResolved() {
	m.contentResolved.Inc()
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

// IncContentResolveError implements embedding.WorkerMetrics.
func (a *workerMetricsAdapter) IncContentResolveError() {
	if a.metrics != nil {
		a.metrics.recordContentResolveError()
	}
}

// IncContentResolved implements embedding.WorkerMetrics.
func (a *workerMetricsAdapter) IncContentResolved() {
	if a.metrics != nil {
		a.metrics.recordContentResolved()
	}
}

// newWorkerMetricsAdapter creates an adapter for the embedding.WorkerMetrics interface.
func newWorkerMetricsAdapter(m *embeddingMetrics) *workerMetricsAdapter {
	return &workerMetricsAdapter{metrics: m}
}
