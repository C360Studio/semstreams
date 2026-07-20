// Package graphindex provides Prometheus metrics for graph-index component.
package graphindex

import (
	"sync"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/metric"
	"github.com/prometheus/client_golang/prometheus"
)

// indexMetrics holds Prometheus metrics for the graph-index component.
type indexMetrics struct {
	eventsProcessed     prometheus.Counter
	indexUpdates        *prometheus.CounterVec
	kvOperations        *prometheus.CounterVec
	watchEvents         *prometheus.CounterVec
	writeFailures       prometheus.Counter     // gh#474 P1b: required index write ultimately failed
	reindexEvents       *prometheus.CounterVec // gh#474 P2b: re-index events by result (changed|unchanged)
	reconcileOperations *prometheus.CounterVec // owner reconciliation I/O by index, operation, and outcome
	// Readiness envelope gauges (ADR-066): the honest Ready/lag/watermark numbers
	// computeIndexStatus already answers over NATS, now scrapeable so an operator can
	// dashboard/alert without a per-sample status request (#579 at the source).
	readiness       prometheus.Gauge     // 1 when Ready (index caught up to target), else 0
	lag             prometheus.Gauge     // revisions behind target (0 = caught up)
	indexedRevision prometheus.Gauge     // low-water-of-pending watermark
	targetRevision  prometheus.Gauge     // ENTITY_STATES stream LastSeq target
	readinessState  *prometheus.GaugeVec // one-hot over building|ready|degraded|reset_required
}

// Package-level metrics (registered once to avoid duplicate registration errors)
var (
	metricsOnce sync.Once
	metrics     *indexMetrics
)

// getMetrics returns the singleton metrics instance, creating and registering it if needed.
func getMetrics(registry *metric.MetricsRegistry) *indexMetrics {
	metricsOnce.Do(func() {
		metrics = &indexMetrics{
			eventsProcessed: prometheus.NewCounter(prometheus.CounterOpts{
				Namespace: "semstreams",
				Subsystem: "graph_index",
				Name:      "events_processed_total",
				Help:      "Total events processed by graph index",
			}),

			indexUpdates: prometheus.NewCounterVec(prometheus.CounterOpts{
				Namespace: "semstreams",
				Subsystem: "graph_index",
				Name:      "updates_total",
				Help:      "Total index update operations by index type",
			}, []string{"index_type"}),

			kvOperations: prometheus.NewCounterVec(prometheus.CounterOpts{
				Namespace: "semstreams",
				Subsystem: "graph_index",
				Name:      "kv_operations_total",
				Help:      "Total KV bucket operations",
			}, []string{"operation", "kv_bucket"}),

			watchEvents: prometheus.NewCounterVec(prometheus.CounterOpts{
				Namespace: "semstreams",
				Subsystem: "graph_index",
				Name:      "watch_events_total",
				Help:      "Total watch events received",
			}, []string{"event_type"}),

			writeFailures: prometheus.NewCounter(prometheus.CounterOpts{
				Namespace: "semstreams",
				Subsystem: "graph_index",
				Name:      "write_failures_total",
				Help:      "Entities whose required index writes ultimately failed after retry (readiness withheld until re-index)",
			}),

			reindexEvents: prometheus.NewCounterVec(prometheus.CounterOpts{
				Namespace: "semstreams",
				Subsystem: "graph_index",
				Name:      "reindex_events_total",
				Help:      "Re-index events by whether the index-input projection changed (the L2 change-detection data gate)",
			}, []string{"result"}),

			reconcileOperations: prometheus.NewCounterVec(prometheus.CounterOpts{
				Namespace: "semstreams",
				Subsystem: "graph_index",
				Name:      "reconcile_operations_total",
				Help:      "Owner reconciliation KV operations by index type, operation, and outcome",
			}, []string{"index_type", "operation", "result"}),

			readiness: prometheus.NewGauge(prometheus.GaugeOpts{
				Namespace: "semstreams",
				Subsystem: "graph_index",
				Name:      "readiness",
				Help:      "1 when the readiness envelope is Ready (index caught up to target revision), else 0 (ADR-066)",
			}),

			lag: prometheus.NewGauge(prometheus.GaugeOpts{
				Namespace: "semstreams",
				Subsystem: "graph_index",
				Name:      "lag",
				Help:      "Revisions the index is behind the ENTITY_STATES target (target_revision - indexed_revision; 0 = caught up)",
			}),

			indexedRevision: prometheus.NewGauge(prometheus.GaugeOpts{
				Namespace: "semstreams",
				Subsystem: "graph_index",
				Name:      "indexed_revision",
				Help:      "Low-water-of-pending watermark: every ENTITY_STATES revision <= this has been applied (ADR-066)",
			}),

			targetRevision: prometheus.NewGauge(prometheus.GaugeOpts{
				Namespace: "semstreams",
				Subsystem: "graph_index",
				Name:      "target_revision",
				Help:      "ENTITY_STATES stream LastSeq the index must catch up to (ADR-066)",
			}),

			readinessState: prometheus.NewGaugeVec(prometheus.GaugeOpts{
				Namespace: "semstreams",
				Subsystem: "graph_index",
				Name:      "readiness_state",
				Help:      "Readiness state one-hot (building|ready|degraded|reset_required): current state=1, others=0, so catching-up is distinguishable from broken",
			}, []string{"state"}),
		}

		// Register metrics with the metrics registry if available
		if registry != nil {
			_ = registry.RegisterCounter("graph-index", "events_processed_total", metrics.eventsProcessed)
			_ = registry.RegisterCounterVec("graph-index", "updates_total", metrics.indexUpdates)
			_ = registry.RegisterCounterVec("graph-index", "kv_operations_total", metrics.kvOperations)
			_ = registry.RegisterCounterVec("graph-index", "watch_events_total", metrics.watchEvents)
			_ = registry.RegisterCounter("graph-index", "write_failures_total", metrics.writeFailures)
			_ = registry.RegisterCounterVec("graph-index", "reindex_events_total", metrics.reindexEvents)
			_ = registry.RegisterCounterVec("graph-index", "reconcile_operations_total", metrics.reconcileOperations)
			_ = registry.RegisterGauge("graph-index", "readiness", metrics.readiness)
			_ = registry.RegisterGauge("graph-index", "lag", metrics.lag)
			_ = registry.RegisterGauge("graph-index", "indexed_revision", metrics.indexedRevision)
			_ = registry.RegisterGauge("graph-index", "target_revision", metrics.targetRevision)
			_ = registry.RegisterGaugeVec("graph-index", "readiness_state", metrics.readinessState)
		} else {
			// Fallback to default prometheus registry for testing
			_ = prometheus.DefaultRegisterer.Register(metrics.eventsProcessed)
			_ = prometheus.DefaultRegisterer.Register(metrics.indexUpdates)
			_ = prometheus.DefaultRegisterer.Register(metrics.kvOperations)
			_ = prometheus.DefaultRegisterer.Register(metrics.watchEvents)
			_ = prometheus.DefaultRegisterer.Register(metrics.writeFailures)
			_ = prometheus.DefaultRegisterer.Register(metrics.reindexEvents)
			_ = prometheus.DefaultRegisterer.Register(metrics.reconcileOperations)
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
// state to 0, so a stale state can never linger at 1 — a "ready" index that later
// degrades reads degraded=1, ready=0, not both.
func (m *indexMetrics) setReadinessGauges(resp graph.IndexStatusResponse) {
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

func (m *indexMetrics) recordReconcileOperation(indexType, operation string, err error) {
	result := "success"
	if err != nil {
		result = "failure"
	}
	m.reconcileOperations.WithLabelValues(indexType, operation, result).Inc()
}

// recordEventProcessed increments the events processed counter.
func (m *indexMetrics) recordEventProcessed() {
	m.eventsProcessed.Inc()
}

// recordIndexUpdate increments the index update counter for the given index type.
func (m *indexMetrics) recordIndexUpdate(indexType string) {
	m.indexUpdates.WithLabelValues(indexType).Inc()
}

// recordKVOperation records a KV operation for the given operation type and bucket.
func (m *indexMetrics) recordKVOperation(operation, bucket string) {
	m.kvOperations.WithLabelValues(operation, bucket).Inc()
}

// recordWatchEvent records a watch event of the given type.
func (m *indexMetrics) recordWatchEvent(eventType string) {
	m.watchEvents.WithLabelValues(eventType).Inc()
}

// recordIndexWriteFailure increments the count of entities whose required index
// writes ultimately failed after retry (gh#474 P1b).
func (m *indexMetrics) recordIndexWriteFailure() {
	m.writeFailures.Inc()
}

// recordReindex records a re-index event, labeled by whether the index-input
// projection was unchanged from the last indexed one (gh#474 P2b).
func (m *indexMetrics) recordReindex(unchanged bool) {
	result := "changed"
	if unchanged {
		result = "unchanged"
	}
	m.reindexEvents.WithLabelValues(result).Inc()
}
