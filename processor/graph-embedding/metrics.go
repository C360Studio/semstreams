// Package graphembedding provides Prometheus metrics for graph-embedding component.
package graphembedding

import (
	"errors"
	"log/slog"
	"sync"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/graph/readiness"
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
	// Owned by the SHARED set rather than hand-rolled here. This component and
	// graph-index each maintained their own copy and both omitted
	// bootstrap_complete; the shared set makes that omission unrepresentable
	// (#763). Emitted metric names are unchanged, pinned by a test.
	readiness *readiness.Gauges
	// contentUnresolved counts entities whose offloaded BODY (a StorageRef) could not
	// be fetched because no store in this process serves the StorageInstance the
	// reference NAMES (gh#414, gh#875). Post-gh#875 the dominant cause is not "no store
	// wired" but "a store is wired for a DIFFERENT instance" — an operator pointed at
	// the old text checked whether a store-read port existed, which is the wrong check.
	// The entity is still embedded from any inline text it carries, and its record
	// carries the content_excluded qualifier, so the affected entities are enumerable.
	// The one-shot warning names the instance and the remedy for it.
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
	// dedupSkipped counts embeddings generated on a condition where the durable dedup
	// bucket was NOT consulted (currently: an embedder whose vector width is
	// unresolved, so no content-addressed key exists). Labelled by reason so the
	// avoided-reuse cost is visible rather than inferred (#623) — the offloaded-lane
	// re-embed cost Track 0 measured, and, post-fix, its recovery toward zero.
	dedupSkipped *prometheus.CounterVec
	// textTruncated counts source-text truncations at the effective cap before
	// embedding. The cap is part of what the vector depends on, so truncation is
	// reported rather than silent, making the bytes actually embedded discoverable (#602).
	textTruncated prometheus.Counter
	// offloadedIdentityIncluded / offloadedIdentityAbsent are the paired observable for
	// the offloaded (StorageRef) lane's identity embedding (D5/#601). Both count STORED
	// vectors, not attempts: they fire on the successful-persistence path, so a dropped
	// save (superseded/tombstoned/failed embed) counts neither (#635 retro F3). included
	// rises each time an offloaded entity's STORED vector embedded its inline identity text
	// (title/.signature/.comment, per text_suffixes) ahead of its body; absent rises when a
	// STORED vector came from the body alone. A producer tuning text_suffixes on offloaded
	// entities reads the effect from these rather than inferring it from silence — the
	// Epic A "make the config-effect observable" discipline.
	offloadedIdentityIncluded prometheus.Counter
	offloadedIdentityAbsent   prometheus.Counter
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
				Help:      "Entities whose offloaded body (StorageRef) was excluded from embedding because no store in this process serves the StorageInstance the reference names — most often a store IS wired but for a different instance, not no store at all. Inline text, if any, is still embedded and the record is marked content_excluded. See the one-shot warning for the instance and its remedy (gh#414, gh#875)",
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

			// Revision-lag producer: indexed_revision / target_revision are
			// meaningful here and stay exposed.
			readiness: readiness.NewGauges(
				readiness.ProducerNames{Service: "graph-embedding", Subsystem: "graph_embedding"},
				readiness.WithRevisionGauges(),
			),

			dedupSkipped: prometheus.NewCounterVec(prometheus.CounterOpts{
				Namespace: "semstreams",
				Subsystem: "graph_embedding",
				Name:      "dedup_skipped_total",
				Help:      "Embeddings generated without consulting the dedup bucket (e.g. embedder vector width unresolved), by reason; makes the avoided-reuse cost visible rather than inferred (#623)",
			}, []string{"reason"}),

			textTruncated: prometheus.NewCounter(prometheus.CounterOpts{
				Namespace: "semstreams",
				Subsystem: "graph_embedding",
				Name:      "text_truncated_total",
				Help:      "Source texts truncated at the effective cap before embedding; the cap is part of the vector's identity, so truncation is reported, not silent (#602)",
			}),

			offloadedIdentityIncluded: prometheus.NewCounter(prometheus.CounterOpts{
				Namespace: "semstreams",
				Subsystem: "graph_embedding",
				Name:      "offloaded_identity_included_total",
				Help:      "Offloaded (StorageRef) entities whose STORED vector included inline identity text (title/.signature, per text_suffixes) ahead of the body; counted on successful persistence, so a dropped save does not count. A rising value confirms text_suffixes took effect on the offloaded lane (#601)",
			}),

			offloadedIdentityAbsent: prometheus.NewCounter(prometheus.CounterOpts{
				Namespace: "semstreams",
				Subsystem: "graph_embedding",
				Name:      "offloaded_identity_absent_total",
				Help:      "Offloaded (StorageRef) entities whose STORED vector came from the body alone (no inline identity text); counted on successful persistence. The symmetric half of offloaded_identity_included so a config-effect is observable, not inferred from silence (#601)",
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
			_ = registry.RegisterCounter("graph-embedding", "content_resolve_error_total", metrics.contentResolveError)
			_ = registry.RegisterCounter("graph-embedding", "content_resolved_total", metrics.contentResolved)
			metrics.readiness.Register(registry)
			_ = registry.RegisterCounterVec("graph-embedding", "dedup_skipped_total", metrics.dedupSkipped)
			_ = registry.RegisterCounter("graph-embedding", "text_truncated_total", metrics.textTruncated)
			_ = registry.RegisterCounter("graph-embedding", "offloaded_identity_included_total", metrics.offloadedIdentityIncluded)
			_ = registry.RegisterCounter("graph-embedding", "offloaded_identity_absent_total", metrics.offloadedIdentityAbsent)
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
			metrics.readiness.Register(nil)
			_ = prometheus.DefaultRegisterer.Register(metrics.dedupSkipped)
			_ = prometheus.DefaultRegisterer.Register(metrics.textTruncated)
			_ = prometheus.DefaultRegisterer.Register(metrics.offloadedIdentityIncluded)
			_ = prometheus.DefaultRegisterer.Register(metrics.offloadedIdentityAbsent)
		}
	})
	return metrics
}

// The current-failed metrics (#613) are resolved PER-REGISTRY (register-or-get), not
// through the process-global getMetrics singleton, mirroring inc 2's fusion
// body_hydration_failures_total{reason}. The singleton returns the FIRST registry's
// collectors to every component; two components with different registries would then
// share one registry's series and leave the other's invisible. Register-or-get gives
// each registry its own series and reuses the existing one when two components share a
// registry.

// newEmbeddingFailedGauge builds the current-failed gauge
// (semstreams_graph_embedding_failed): the number of entities CURRENTLY in a failed
// embedding state. It drives State=degraded while >0 and drops to 0 as failures resolve
// on re-delivery (#613).
func newEmbeddingFailedGauge() prometheus.Gauge {
	return prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "semstreams",
		Subsystem: "graph_embedding",
		Name:      "failed",
		Help:      "Entities currently in a failed embedding state (#613): drives State=degraded while >0, drops to 0 as failures resolve on re-delivery. NOT cumulative — this is the live count, not total failures ever.",
	})
}

// newEmbeddingFailuresVec builds the reason-labelled failures counter
// (semstreams_graph_embedding_failures_total{reason}). The label is the BOUNDED reason
// enum — never the raw error message (unbounded → cardinality blowup) (#613).
func newEmbeddingFailuresVec() *prometheus.CounterVec {
	return prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "semstreams",
		Subsystem: "graph_embedding",
		Name:      "failures_total",
		Help:      "Cumulative embedding failures by BOUNDED reason (#613): connection_refused | timeout | dimension_mismatch | embedder_error | content_error | internal. The raw error message is never a label.",
	}, []string{"reason"})
}

// resolveEmbeddingFailedGauge resolves the current-failed gauge against a SPECIFIC
// registry (nil → the default registerer), register-or-get-existing. See
// pkg/fusion.resolveBodyHydrationFailureVec for the rationale — a direct Register on the
// underlying Registerer (not metric.MetricsRegistry.RegisterGauge, which reports success
// without handing back the existing collector).
func resolveEmbeddingFailedGauge(registry *metric.MetricsRegistry) prometheus.Gauge {
	g := newEmbeddingFailedGauge()
	reg := registererFor(registry)
	if err := reg.Register(g); err != nil {
		var already prometheus.AlreadyRegisteredError
		if errors.As(err, &already) {
			if existing, ok := already.ExistingCollector.(prometheus.Gauge); ok {
				return existing
			}
		}
		slog.Default().Error(
			"graph-embedding failed gauge registration failed; it will still update but is NOT scraped",
			slog.String("metric", "semstreams_graph_embedding_failed"), slog.Any("error", err))
	}
	return g
}

// resolveEmbeddingFailuresVec resolves the reason-labelled failures counter against a
// SPECIFIC registry, register-or-get-existing (see resolveEmbeddingFailedGauge).
func resolveEmbeddingFailuresVec(registry *metric.MetricsRegistry) *prometheus.CounterVec {
	vec := newEmbeddingFailuresVec()
	reg := registererFor(registry)
	if err := reg.Register(vec); err != nil {
		var already prometheus.AlreadyRegisteredError
		if errors.As(err, &already) {
			if existing, ok := already.ExistingCollector.(*prometheus.CounterVec); ok {
				return existing
			}
		}
		slog.Default().Error(
			"graph-embedding failures counter registration failed; it will still increment but is NOT scraped",
			slog.String("metric", "semstreams_graph_embedding_failures_total"), slog.Any("error", err))
	}
	return vec
}

// registererFor returns the prometheus.Registerer backing a MetricsRegistry, or the
// default registerer when none is wired (the test path).
func registererFor(registry *metric.MetricsRegistry) prometheus.Registerer {
	if registry != nil {
		return registry.PrometheusRegistry()
	}
	return prometheus.DefaultRegisterer
}

// setReadinessGauges publishes the ADR-066 readiness envelope as Prometheus gauges.
// It is pure over resp (no compute, no NATS) so it is unit-testable and cheap to call
// on a tick. The state gauge is one-hot: the current state is set to 1 and every other
// state to 0, so a stale state can never linger at 1 — a "ready" pipeline that later
// degrades reads degraded=1, ready=0, not both.
func (m *embeddingMetrics) setReadinessGauges(resp graph.IndexStatusResponse) {
	m.readiness.Set(resp)
}

// recordStatusPublishFailure counts one failed GRAPH_STATUS heartbeat write (ADR-083).
func (m *embeddingMetrics) recordStatusPublishFailure() {
	m.readiness.RecordPublishFailure()
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

// recordContentUnresolved increments the counter for offloaded content excluded from
// embedding because no store in this process serves the instance the reference names
// (gh#414, gh#875 — usually a store wired for a DIFFERENT instance, not none at all).
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

// recordDedupSkipped counts one embedding generated without consulting the dedup
// bucket, tagged with the reason (#623).
func (m *embeddingMetrics) recordDedupSkipped(reason string) {
	m.dedupSkipped.WithLabelValues(reason).Inc()
}

// recordTextTruncated counts one source-text truncation at the effective cap (#602).
func (m *embeddingMetrics) recordTextTruncated() {
	m.textTruncated.Inc()
}

// recordOffloadedIdentityIncluded counts one offloaded entity whose STORED vector
// embedded inline identity text ahead of its body (#601; counted on successful
// persistence, #635 retro F3).
func (m *embeddingMetrics) recordOffloadedIdentityIncluded() {
	m.offloadedIdentityIncluded.Inc()
}

// recordOffloadedIdentityAbsent counts one offloaded entity whose STORED vector came
// from the body alone because it carried no inline identity text (#601; counted on
// successful persistence, #635 retro F3).
func (m *embeddingMetrics) recordOffloadedIdentityAbsent() {
	m.offloadedIdentityAbsent.Inc()
}

// setPending sets the pending embeddings gauge.
func (m *embeddingMetrics) setPending(count float64) {
	m.embeddingPending.Set(count)
}

// workerMetricsAdapter adapts embeddingMetrics to the embedding.WorkerMetrics interface.
// This allows the Worker to report metrics without direct dependency on prometheus.
//
// failuresVec is the PER-REGISTRY reason-labelled failures counter (#613), threaded in
// separately from the process-global embeddingMetrics singleton so it resolves against
// the component's own registry (register-or-get). It is the ONE metric the worker reports
// that must not go through the singleton.
//
// reportExcluded is the component's own reportOffloadedContentExcluded (gh#414). Hop 2
// reaches the SAME reporter through here rather than growing a second one (gh#875), so
// the counter and the one-shot operator warning have exactly one home regardless of
// which hop observed the unresolvable instance.
type workerMetricsAdapter struct {
	metrics        *embeddingMetrics
	failuresVec    *prometheus.CounterVec
	reportExcluded func(entityID, storageInstance string)
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

// IncFailedReason implements embedding.WorkerMetrics: it increments the per-registry
// failures_total{reason} counter (#613). reason is a value from the bounded enum; the
// worker guarantees the raw error message never reaches here.
func (a *workerMetricsAdapter) IncFailedReason(reason string) {
	if a.failuresVec != nil {
		a.failuresVec.WithLabelValues(reason).Inc()
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

// ReportContentExcluded implements embedding.WorkerMetrics by routing hop 2's
// unresolvable-instance observation into the component's single exclusion reporter
// (gh#875) — the same content_unresolved_total counter and one-shot warning hop 1's
// gate uses. A nil reporter (a directly-constructed adapter in a test) is a no-op.
func (a *workerMetricsAdapter) ReportContentExcluded(entityID, storageInstance string) {
	if a.reportExcluded != nil {
		a.reportExcluded(entityID, storageInstance)
	}
}

// IncDedupSkipped implements embedding.WorkerMetrics.
func (a *workerMetricsAdapter) IncDedupSkipped(reason string) {
	if a.metrics != nil {
		a.metrics.recordDedupSkipped(reason)
	}
}

// IncTruncated implements embedding.WorkerMetrics.
func (a *workerMetricsAdapter) IncTruncated() {
	if a.metrics != nil {
		a.metrics.recordTextTruncated()
	}
}

// IncOffloadedIdentityIncluded implements embedding.WorkerMetrics.
func (a *workerMetricsAdapter) IncOffloadedIdentityIncluded() {
	if a.metrics != nil {
		a.metrics.recordOffloadedIdentityIncluded()
	}
}

// IncOffloadedIdentityAbsent implements embedding.WorkerMetrics.
func (a *workerMetricsAdapter) IncOffloadedIdentityAbsent() {
	if a.metrics != nil {
		a.metrics.recordOffloadedIdentityAbsent()
	}
}

// newWorkerMetricsAdapter creates an adapter for the embedding.WorkerMetrics interface.
// failuresVec is the per-registry failures_total{reason} counter (#613);
// reportExcluded is the component's reportOffloadedContentExcluded (gh#414/gh#875).
func newWorkerMetricsAdapter(
	m *embeddingMetrics,
	failuresVec *prometheus.CounterVec,
	reportExcluded func(entityID, storageInstance string),
) *workerMetricsAdapter {
	return &workerMetricsAdapter{metrics: m, failuresVec: failuresVec, reportExcluded: reportExcluded}
}
