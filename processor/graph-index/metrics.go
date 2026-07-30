// Package graphindex provides Prometheus metrics for graph-index component.
package graphindex

import (
	"sync"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/graph/readiness"
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
	//
	// Owned by the SHARED set rather than hand-rolled here. This component and
	// graph-embedding each maintained their own copy and both omitted
	// bootstrap_complete; the shared set makes that omission unrepresentable (#763).
	// Emitted metric names are unchanged — readiness.Gauges reproduces them exactly,
	// pinned by a test.
	readiness *readiness.Gauges
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

			// Revision-lag producer: indexed_revision / target_revision are
			// meaningful here and stay exposed.
			readiness: readiness.NewGauges(
				readiness.ProducerNames{Service: "graph-index", Subsystem: "graph_index"},
				readiness.WithRevisionGauges(),
			),
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
			metrics.readiness.Register(registry)
		} else {
			// Fallback to default prometheus registry for testing
			_ = prometheus.DefaultRegisterer.Register(metrics.eventsProcessed)
			_ = prometheus.DefaultRegisterer.Register(metrics.indexUpdates)
			_ = prometheus.DefaultRegisterer.Register(metrics.kvOperations)
			_ = prometheus.DefaultRegisterer.Register(metrics.watchEvents)
			_ = prometheus.DefaultRegisterer.Register(metrics.writeFailures)
			_ = prometheus.DefaultRegisterer.Register(metrics.reindexEvents)
			_ = prometheus.DefaultRegisterer.Register(metrics.reconcileOperations)
			metrics.readiness.Register(nil)
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
	m.readiness.Set(resp)
}

// recordStatusPublishFailure counts one failed GRAPH_STATUS heartbeat write (ADR-083).
func (m *indexMetrics) recordStatusPublishFailure() {
	m.readiness.RecordPublishFailure()
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
