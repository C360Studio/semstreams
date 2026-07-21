package scenarios

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/test/e2e/client"
)

// Regression coverage for gh#615.
//
// executeValidateZeroClusters gated on semstreams_clustering_runs_total, a name
// no production code exports. SumMetricsByName's "metric not found" error was
// discarded into _, the count read 0, and `0 <= ExpectedClusters` was therefore
// true on every run the structural tier has ever performed. The stage that
// exists to prove clustering did NOT happen was structurally incapable of
// failing.
//
// These tests exercise the real HTTP path: a live prometheus registry rendered
// through promhttp, scraped by a real MetricsClient over httptest. Nothing is
// stubbed between the assertion and the exposition format, so the `_count`
// suffix derivation the fix depends on is exercised by Prometheus itself rather
// than asserted from a hand-written string.

// clusteringHistogramOpts mirrors the HistogramOpts in
// processor/graph-clustering/metrics.go. Those metrics are package-private, so
// this test cannot import them; it reproduces the naming triple and lets the
// real Prometheus renderer derive the exposed series names. That is what proves
// clusteringRunsMetric ("..._detection_duration_seconds_count") is the name a
// scrape actually carries.
var clusteringHistogramOpts = prometheus.HistogramOpts{
	Namespace: "semstreams",
	Subsystem: "graph_clustering",
	Name:      "detection_duration_seconds",
	Help:      "Wall time of a community-detection run.",
	Buckets:   []float64{0.5, 1, 2, 5, 10, 20, 30, 60, 120},
}

// metricsFixture spins a real /metrics endpoint over the supplied registry and
// returns a scenario wired to scrape it.
func metricsFixture(t *testing.T, reg *prometheus.Registry) *TieredScenario {
	t.Helper()

	mux := httptest.NewServer(promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	t.Cleanup(mux.Close)

	return &TieredScenario{
		metrics: client.NewMetricsClient(mux.URL),
		config:  &TieredConfig{ExpectedClusters: 0, ExpectedEmbeddings: 0},
	}
}

func newResult() *Result {
	return &Result{
		Metrics: map[string]any{},
		Details: map[string]any{},
	}
}

// registerUnrelated puts at least one series on the endpoint so that "clustering
// subsystem absent" is distinguishable from "scrape returned nothing at all".
func registerUnrelated(t *testing.T, reg *prometheus.Registry) {
	t.Helper()
	g := prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "semstreams",
		Subsystem: "graph_index",
		Name:      "lag",
		Help:      "unrelated series",
	})
	g.Set(0)
	require.NoError(t, reg.Register(g))
}

// TestExecuteValidateZeroClusters_FailsWhenClusteringRan is THE regression test:
// under the old phantom-metric gate this case passed.
func TestExecuteValidateZeroClusters_FailsWhenClusteringRan(t *testing.T) {
	reg := prometheus.NewRegistry()
	registerUnrelated(t, reg)

	h := prometheus.NewHistogram(clusteringHistogramOpts)
	require.NoError(t, reg.Register(h))

	// Three completed detection runs. graph-clustering observes this histogram
	// exactly once per completed run, so _count == 3 means clustering ran 3x.
	h.Observe(4.4)
	h.Observe(11.2)
	h.Observe(23.7)

	s := metricsFixture(t, reg)
	result := newResult()

	err := s.executeValidateZeroClusters(context.Background(), result)

	require.Error(t, err, "clustering ran 3 times in a tier that forbids it; the gate must fail")
	require.Contains(t, err.Error(), "structural tier constraint violated")
	require.Contains(t, err.Error(), "clustering_runs=3")

	require.Equal(t, 3, result.Metrics["clustering_runs"])
	details, ok := result.Details["zero_clusters_validation"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, false, details["constraint_met"])
	require.Equal(t, true, details["component_scraped"])
}

// TestExecuteValidateZeroClusters_PassesWhenComponentNotDeployed covers the real
// structural tier: configs/structural.json deploys no graph-clustering, so the
// entire subsystem is absent from the scrape. Absence is proof the constraint
// holds, not an error — a naive "assert the metric was found" would invert the
// bug and fail the tier permanently.
func TestExecuteValidateZeroClusters_PassesWhenComponentNotDeployed(t *testing.T) {
	reg := prometheus.NewRegistry()
	registerUnrelated(t, reg)

	s := metricsFixture(t, reg)
	result := newResult()

	require.NoError(t, s.executeValidateZeroClusters(context.Background(), result))

	details, ok := result.Details["zero_clusters_validation"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, true, details["constraint_met"])
	require.Equal(t, false, details["component_scraped"], "graph-clustering must be reported as not deployed")
	require.Contains(t, details["message"], "not deployed")
}

// TestExecuteValidateZeroClusters_PassesWhenDeployedButIdle covers a tier that
// runs graph-clustering without a completed detection cycle. The histogram is
// registered in the component constructor, so _count is scrapeable at 0.
func TestExecuteValidateZeroClusters_PassesWhenDeployedButIdle(t *testing.T) {
	reg := prometheus.NewRegistry()
	registerUnrelated(t, reg)
	require.NoError(t, reg.Register(prometheus.NewHistogram(clusteringHistogramOpts)))

	s := metricsFixture(t, reg)
	result := newResult()

	require.NoError(t, s.executeValidateZeroClusters(context.Background(), result))
	require.Equal(t, 0, result.Metrics["clustering_runs"])

	details, ok := result.Details["zero_clusters_validation"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, true, details["component_scraped"])
}

// TestExecuteValidateZeroClusters_FailsOnMetricNameDrift reproduces gh#615's
// exact shape: the component is deployed and exporting, but the specific name
// the gate polls is not among its series. That must fail loudly instead of
// reading as a compliant 0.
func TestExecuteValidateZeroClusters_FailsOnMetricNameDrift(t *testing.T) {
	reg := prometheus.NewRegistry()
	registerUnrelated(t, reg)

	// graph-clustering is present and exporting defer_total, but the histogram
	// the gate depends on is missing — a rename in production code.
	deferTotal := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "semstreams",
		Subsystem: "graph_clustering",
		Name:      "defer_total",
		Help:      "deferred detection ticks",
	}, []string{"reason"})
	deferTotal.WithLabelValues("hard_stop")
	require.NoError(t, reg.Register(deferTotal))

	s := metricsFixture(t, reg)
	result := newResult()

	err := s.executeValidateZeroClusters(context.Background(), result)

	require.Error(t, err, "a deployed component that does not export the gated metric means the gate measures nothing")
	require.Contains(t, err.Error(), "unverifiable")
	require.Contains(t, err.Error(), "detection_duration_seconds_count")

	details, ok := result.Details["zero_clusters_validation"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, false, details["verifiable"])
	require.Equal(t, false, details["constraint_met"])
}

// TestExecuteValidateZeroEmbeddings_FailsWhenEmbeddingsGenerated confirms the
// sibling validator got the same treatment. Its metric name was always real, but
// its error discard had the identical hole.
func TestExecuteValidateZeroEmbeddings_FailsWhenEmbeddingsGenerated(t *testing.T) {
	reg := prometheus.NewRegistry()
	registerUnrelated(t, reg)

	generated := prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "semstreams",
		Subsystem: "graph_embedding",
		Name:      "embeddings_generated_total",
		Help:      "embeddings generated",
	})
	generated.Add(17)
	require.NoError(t, reg.Register(generated))

	s := metricsFixture(t, reg)
	result := newResult()

	err := s.executeValidateZeroEmbeddings(context.Background(), result)

	require.Error(t, err)
	require.Contains(t, err.Error(), "embeddings_generated=17")
	require.Equal(t, 17, result.Metrics["embeddings_generated"])
}

// TestExecuteValidateZeroEmbeddings_PassesWhenComponentNotDeployed mirrors the
// real structural tier, which deploys no graph-embedding.
func TestExecuteValidateZeroEmbeddings_PassesWhenComponentNotDeployed(t *testing.T) {
	reg := prometheus.NewRegistry()
	registerUnrelated(t, reg)

	s := metricsFixture(t, reg)
	result := newResult()

	require.NoError(t, s.executeValidateZeroEmbeddings(context.Background(), result))

	details, ok := result.Details["zero_embeddings_validation"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, false, details["component_scraped"])
}

// TestPhantomMetricNameIsRejected pins the specific failure gh#615 describes: if
// anyone re-points a gate at the old invented name, the subsystem-presence check
// rejects it rather than silently returning 0.
func TestPhantomMetricNameIsRejected(t *testing.T) {
	reg := prometheus.NewRegistry()
	registerUnrelated(t, reg)
	require.NoError(t, reg.Register(prometheus.NewHistogram(clusteringHistogramOpts)))

	s := metricsFixture(t, reg)

	// The retired name is not even inside the real subsystem prefix, which is
	// its own tell: "semstreams_clustering_" was never a subsystem this
	// repository exports.
	_, err := s.metrics.SumMetricInSubsystem(
		context.Background(), clusteringSubsystem, "semstreams_clustering_runs_total")
	require.Error(t, err)
	require.Contains(t, err.Error(), "does not belong to subsystem")

	// And within the correct prefix, an unexported name is a hard error rather
	// than a zero reading.
	_, err = s.metrics.SumMetricInSubsystem(
		context.Background(), clusteringSubsystem, clusteringSubsystem+"runs_total")
	require.Error(t, err)
	require.Contains(t, err.Error(), "is not exported")
}

// TestSumMetricInSubsystem_SumsAcrossLabels guards the aggregation itself.
func TestSumMetricInSubsystem_SumsAcrossLabels(t *testing.T) {
	reg := prometheus.NewRegistry()
	deferTotal := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "semstreams",
		Subsystem: "graph_clustering",
		Name:      "defer_total",
		Help:      "deferred detection ticks",
	}, []string{"reason"})
	deferTotal.WithLabelValues("hard_stop").Add(2)
	deferTotal.WithLabelValues("status_unknown").Add(5)
	require.NoError(t, reg.Register(deferTotal))

	s := metricsFixture(t, reg)

	reading, err := s.metrics.SumMetricInSubsystem(
		context.Background(), clusteringSubsystem, clusteringSubsystem+"defer_total")
	require.NoError(t, err)
	require.True(t, reading.Found)
	require.True(t, reading.SubsystemPresent)
	require.InDelta(t, 7.0, reading.Sum, 0.001)
}

// TestValidateEmbeddingQueueHealth_FailsWhenPipelineDidNothing covers the wider
// pattern called out in gh#615: this validator fetched `generated` — the only
// value proving work happened — recorded it, asserted nothing, and printed
// "Health check passed" for a pipeline that had produced zero embeddings.
func TestValidateEmbeddingQueueHealth_FailsWhenPipelineDidNothing(t *testing.T) {
	reg := prometheus.NewRegistry()
	for _, name := range []string{"pending", "errors_total", "dedup_hits_total", "embeddings_generated_total"} {
		c := prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "semstreams",
			Subsystem: "graph_embedding",
			Name:      name,
			Help:      "zero",
		})
		c.Set(0)
		require.NoError(t, reg.Register(c))
	}

	s := metricsFixture(t, reg)
	result := newResult()

	err := s.validateEmbeddingQueueHealth(context.Background(), result)

	require.Error(t, err, "a drained empty queue is not a healthy pipeline")
	require.Contains(t, err.Error(), "embedding pipeline did nothing")
}

// TestValidateEmbeddingQueueHealth_NoFabricatedQueuedTotal confirms the phantom
// queued_total no longer reaches operator-facing results.
func TestValidateEmbeddingQueueHealth_NoFabricatedQueuedTotal(t *testing.T) {
	reg := prometheus.NewRegistry()
	values := map[string]float64{
		"pending":                    0,
		"errors_total":               0,
		"dedup_hits_total":           4,
		"embeddings_generated_total": 12,
	}
	for name, v := range values {
		g := prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "semstreams",
			Subsystem: "graph_embedding",
			Name:      name,
			Help:      "fixture",
		})
		g.Set(v)
		require.NoError(t, reg.Register(g))
	}

	s := metricsFixture(t, reg)
	result := newResult()

	require.NoError(t, s.validateEmbeddingQueueHealth(context.Background(), result))

	_, present := result.Metrics["embedding_queued_total"]
	require.False(t, present, "queued_total was never a real metric and must not reappear")
	require.Equal(t, int64(16), result.Metrics["embedding_resolved_total"], "12 generated + 4 dedup hits")

	details, ok := result.Details["embedding_queue_health"].(map[string]any)
	require.True(t, ok)
	_, hasQueued := details["queued_total"]
	require.False(t, hasQueued)

	// The results schema must carry no queued_total field either.
	tr := &TieredResults{}
	buildEmbeddingMetrics(tr, result)
	require.NotNil(t, tr.Embeddings)
	require.Equal(t, int64(16), tr.Embeddings.ResolvedTotal)
	require.InDelta(t, 0.25, tr.Embeddings.DedupRate, 0.001, "4 dedup hits / 16 resolved")
}

// TestEmbeddingResultsJSONHasNoQueuedTotal asserts on the serialized operator
// surface, not just the struct.
func TestEmbeddingResultsJSONHasNoQueuedTotal(t *testing.T) {
	em := EmbeddingMetrics{ResolvedTotal: 16, GeneratedTotal: 12, DedupHits: 4}
	blob, err := json.Marshal(em)
	require.NoError(t, err)
	require.NotContains(t, string(blob), "queued_total",
		"result JSON must not report a metric that production code never exports")
	require.Contains(t, string(blob), "resolved_total")
}
