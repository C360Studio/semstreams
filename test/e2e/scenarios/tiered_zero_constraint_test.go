package scenarios

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/graph/clustering"
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

// componentsFixture serves a /components/list inventory, the authoritative
// deployment signal the zero-work gates cross-check absence against.
func componentsFixture(t *testing.T, comps []client.ComponentInfo) *client.ObservabilityClient {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc("/components/list", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(comps))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return client.NewObservabilityClient(srv.URL)
}

// metricsFixture spins a real /metrics endpoint over the supplied registry and
// returns a scenario wired to scrape it.
//
// The component inventory it serves mirrors the real structural tier: a udp
// input and nothing else, so neither graph-embedding nor graph-clustering is
// deployed and absence of their subsystems is genuinely proof they did not run.
func metricsFixture(t *testing.T, reg *prometheus.Registry) *TieredScenario {
	t.Helper()
	return metricsFixtureWithComponents(t, reg, []client.ComponentInfo{
		{Name: "udp-sensor", Component: "udp", Type: "input", Enabled: true, State: "running", Healthy: true},
	})
}

// metricsFixtureWithComponents is metricsFixture with an explicit inventory.
func metricsFixtureWithComponents(
	t *testing.T,
	reg *prometheus.Registry,
	comps []client.ComponentInfo,
) *TieredScenario {
	t.Helper()

	mux := httptest.NewServer(promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	t.Cleanup(mux.Close)

	return &TieredScenario{
		metrics: client.NewMetricsClient(mux.URL),
		client:  componentsFixture(t, comps),
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

// embeddingRegistry builds a graph_embedding subsystem with the four series
// validateEmbeddingQueueHealth reads.
func embeddingRegistry(t *testing.T, values map[string]float64) *prometheus.Registry {
	t.Helper()
	reg := prometheus.NewRegistry()
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
	return reg
}

// TestValidateEmbeddingQueueHealth_NoFabricatedQueuedTotal confirms the phantom
// queued_total no longer reaches operator-facing results, and pins the corrected
// resolution arithmetic.
//
// embeddings_generated_total increments on the dedup-hit path too
// (graph/embedding/worker.go:394 calls saveAndNotify for both branches of
// getOrGenerateEmbedding), so with 12 resolutions of which 4 were cache hits,
// only 8 vectors were actually computed. The former `generated + dedupHits`
// reported 16 resolutions, inventing 4 units of work that never happened.
func TestValidateEmbeddingQueueHealth_NoFabricatedQueuedTotal(t *testing.T) {
	reg := embeddingRegistry(t, map[string]float64{
		"pending":                    0,
		"errors_total":               0,
		"dedup_hits_total":           4,
		"embeddings_generated_total": 12,
	})

	s := metricsFixture(t, reg)
	result := newResult()

	require.NoError(t, s.validateEmbeddingQueueHealth(context.Background(), result))

	_, present := result.Metrics["embedding_queued_total"]
	require.False(t, present, "queued_total was never a real metric and must not reappear")
	require.Equal(t, int64(12), result.Metrics["embedding_resolved_total"],
		"embeddings_generated_total IS the resolution count; adding dedup hits double-counts reuse")
	require.Equal(t, int64(8), result.Metrics["embedding_fresh_generated_total"],
		"12 resolutions - 4 cache hits = 8 vectors actually computed")

	details, ok := result.Details["embedding_queue_health"].(map[string]any)
	require.True(t, ok)
	_, hasQueued := details["queued_total"]
	require.False(t, hasQueued)
	require.InDelta(t, 8.0, details["fresh_generated_total"], 0.001)

	// The results schema must carry no queued_total field either.
	tr := &TieredResults{}
	buildEmbeddingMetrics(tr, result)
	require.NotNil(t, tr.Embeddings)
	require.Equal(t, int64(12), tr.Embeddings.ResolvedTotal)
	require.Equal(t, int64(8), tr.Embeddings.FreshGeneratedTotal)
	require.InDelta(t, 1.0/3.0, tr.Embeddings.DedupRate, 0.001,
		"dedup rate is the share of resolutions served from cache: 4/12")
}

// TestValidateEmbeddingQueueHealth_FreshGenerationsTrackEmbeddingCost is the
// regression that the double-count actually hid.
//
// Two runs resolve the same number of embeddings but with a very different
// fresh/reused split. Under `resolved = generated + dedupHits` the reported
// totals moved in the WRONG direction — the run doing less real work reported a
// bigger number — so a 2.8x change in remote embedder calls read as "unchanged".
// fresh_generated_total must separate them.
func TestValidateEmbeddingQueueHealth_FreshGenerationsTrackEmbeddingCost(t *testing.T) {
	readFresh := func(t *testing.T, generated, dedup float64) int64 {
		t.Helper()
		reg := embeddingRegistry(t, map[string]float64{
			"pending":                    0,
			"errors_total":               0,
			"dedup_hits_total":           dedup,
			"embeddings_generated_total": generated,
		})
		s := metricsFixture(t, reg)
		result := newResult()
		require.NoError(t, s.validateEmbeddingQueueHealth(context.Background(), result))
		return result.Metrics["embedding_fresh_generated_total"].(int64)
	}

	// Mostly cache hits: 250 resolutions, 182 of them reused.
	cheap := readFresh(t, 250, 182)
	// Same resolution count, far less reuse.
	expensive := readFresh(t, 250, 59)

	require.Equal(t, int64(68), cheap)
	require.Equal(t, int64(191), expensive)
	require.Greater(t, expensive, cheap,
		"fresh generations must expose the change in real embedding work that a resolution total hides")
}

// TestValidateEmbeddingQueueHealth_ReportsImpossibleDedupRatio guards the
// documented invariant itself. Dedup hits are a subset of resolutions, so within
// one scrape dedup_hits > embeddings_generated_total means the worker's metric
// wiring changed and every derived number here is wrong.
func TestValidateEmbeddingQueueHealth_ReportsImpossibleDedupRatio(t *testing.T) {
	reg := embeddingRegistry(t, map[string]float64{
		"pending":                    0,
		"errors_total":               0,
		"dedup_hits_total":           9,
		"embeddings_generated_total": 5,
	})

	s := metricsFixture(t, reg)
	result := newResult()

	err := s.validateEmbeddingQueueHealth(context.Background(), result)
	require.Error(t, err, "an impossible subset relation must not be silently clamped away")
	require.Contains(t, err.Error(), "exceeds")
	require.Equal(t, int64(0), result.Metrics["embedding_fresh_generated_total"])
}

// TestValidateEmbeddingQueueHealth_FailsOnUndrainedQueue is a Finding-3
// regression: pending > 0 recorded a warning and returned nil, so
// validate-embedding-queue-health completed successfully with work still queued.
func TestValidateEmbeddingQueueHealth_FailsOnUndrainedQueue(t *testing.T) {
	reg := embeddingRegistry(t, map[string]float64{
		"pending":                    7,
		"errors_total":               0,
		"dedup_hits_total":           2,
		"embeddings_generated_total": 40,
	})

	s := metricsFixture(t, reg)
	result := newResult()

	err := s.validateEmbeddingQueueHealth(context.Background(), result)

	require.Error(t, err, "7 items still queued is the condition this stage exists to detect")
	require.Contains(t, err.Error(), "embedding queue is unhealthy")
	require.Contains(t, err.Error(), "not drained: 7 pending")
	require.NotEmpty(t, result.Warnings, "the operator report keeps the warning too")

	details, ok := result.Details["embedding_queue_health"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, false, details["queue_drained"])
}

// TestValidateEmbeddingQueueHealth_FailsOnEmbeddingFailures is the other half of
// Finding 3: failed embeddings also returned nil.
func TestValidateEmbeddingQueueHealth_FailsOnEmbeddingFailures(t *testing.T) {
	reg := embeddingRegistry(t, map[string]float64{
		"pending":                    0,
		"errors_total":               3,
		"dedup_hits_total":           2,
		"embeddings_generated_total": 40,
	})

	s := metricsFixture(t, reg)
	result := newResult()

	err := s.validateEmbeddingQueueHealth(context.Background(), result)

	require.Error(t, err, "a drained queue with failed embeddings is not a healthy pipeline")
	require.Contains(t, err.Error(), "embedding queue is unhealthy")
	require.Contains(t, err.Error(), "failures detected: 3 failed")

	details, ok := result.Details["embedding_queue_health"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, false, details["no_failures"])
}

// TestValidateEmbeddingQueueHealth_PassesWhenDrainedAndClean pins the positive
// case, so the Finding-3 fix cannot be satisfied by a gate that always fails.
func TestValidateEmbeddingQueueHealth_PassesWhenDrainedAndClean(t *testing.T) {
	reg := embeddingRegistry(t, map[string]float64{
		"pending":                    0,
		"errors_total":               0,
		"dedup_hits_total":           2,
		"embeddings_generated_total": 40,
	})

	s := metricsFixture(t, reg)
	result := newResult()

	require.NoError(t, s.validateEmbeddingQueueHealth(context.Background(), result))
	require.Empty(t, result.Warnings)
	require.Equal(t, int64(38), result.Metrics["embedding_fresh_generated_total"])
}

// TestEmbeddingResultsJSONHasNoQueuedTotal asserts on the serialized operator
// surface, not just the struct.
func TestEmbeddingResultsJSONHasNoQueuedTotal(t *testing.T) {
	em := EmbeddingMetrics{ResolvedTotal: 12, FreshGeneratedTotal: 8, DedupHits: 4}
	blob, err := json.Marshal(em)
	require.NoError(t, err)
	require.NotContains(t, string(blob), "queued_total",
		"result JSON must not report a metric that production code never exports")
	require.Contains(t, string(blob), "resolved_total")
	require.Contains(t, string(blob), "fresh_generated_total",
		"embedding cost must be reported, not left to be re-derived")
}

// --- Finding 7: absence must be corroborated by the component inventory ------
//
// The zero-work gates treat a missing Prometheus subsystem as proof the
// component did not run. That inference is sound for the real structural tier —
// configs/e2e-structural.json deploys neither graph-embedding nor
// graph-clustering, and demanding the metric be present would fail that tier
// permanently. What was missing is corroboration: on the metrics endpoint's word
// alone, "never deployed" and "the custom registry is empty or broken" produce
// the identical reading, and the gate called the second one a pass.

// TestValidateTierMustNotRun_FailsWhenDeployedButSubsystemMissing is THE
// Finding-7 regression: the component IS in the service's inventory, but its
// metrics subsystem is entirely absent from a 200 scrape. Before the fix this
// was SubsystemPresent=false, verifiable=true, constraint met.
func TestValidateTierMustNotRun_FailsWhenDeployedButSubsystemMissing(t *testing.T) {
	// A healthy scrape that simply carries nothing from graph-clustering — the
	// shape a broken or empty custom registry produces.
	reg := prometheus.NewRegistry()
	registerUnrelated(t, reg)

	s := metricsFixtureWithComponents(t, reg, []client.ComponentInfo{
		{Name: "udp-sensor", Component: "udp", Type: "input", Enabled: true, State: "running", Healthy: true},
		{Name: "graph-clustering", Component: "graph-clustering", Type: "processor",
			Enabled: true, State: "running", Healthy: true},
	})
	result := newResult()

	err := s.executeValidateZeroClusters(context.Background(), result)

	require.Error(t, err, "a deployed component whose metrics are missing makes the constraint unverifiable, not satisfied")
	require.Contains(t, err.Error(), "unverifiable")
	require.Contains(t, err.Error(), "graph-clustering")

	details, ok := result.Details["zero_clusters_validation"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, false, details["verifiable"],
		"an unobservable deployed component must never be recorded as verified")
	require.Equal(t, false, details["constraint_met"])
}

// TestValidateTierMustNotRun_EmptyScrapeWithDeployedComponentFails is the
// explicitly requested regression for the degenerate scrape: 200 OK, zero
// series. Nothing at all is exported, yet the component is deployed.
func TestValidateTierMustNotRun_EmptyScrapeWithDeployedComponentFails(t *testing.T) {
	reg := prometheus.NewRegistry() // completely empty custom registry

	s := metricsFixtureWithComponents(t, reg, []client.ComponentInfo{
		{Name: "graph-embedding", Component: "graph-embedding", Type: "processor",
			Enabled: true, State: "running", Healthy: true},
	})
	result := newResult()

	err := s.executeValidateZeroEmbeddings(context.Background(), result)

	require.Error(t, err, "an empty registry is not evidence that graph-embedding is absent")
	require.Contains(t, err.Error(), "unverifiable")

	details, ok := result.Details["zero_embeddings_validation"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, false, details["verifiable"])
}

// TestValidateTierMustNotRun_PassesWhenInventoryConfirmsAbsence is the real
// structural tier and must keep passing: the component is in neither the scrape
// nor the inventory.
func TestValidateTierMustNotRun_PassesWhenInventoryConfirmsAbsence(t *testing.T) {
	reg := prometheus.NewRegistry()
	registerUnrelated(t, reg)

	s := metricsFixtureWithComponents(t, reg, []client.ComponentInfo{
		{Name: "udp-sensor", Component: "udp", Type: "input", Enabled: true, State: "running", Healthy: true},
		{Name: "graph-processor", Component: "graph", Type: "processor", Enabled: true, State: "running", Healthy: true},
	})
	result := newResult()

	require.NoError(t, s.executeValidateZeroClusters(context.Background(), result))

	details, ok := result.Details["zero_clusters_validation"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, true, details["constraint_met"])
	require.Equal(t, true, details["verifiable"])
	require.Contains(t, details["message"], "absent from the component inventory")
}

// TestValidateTierMustNotRun_DisabledComponentCountsAsAbsent covers the entry
// ComponentManager keeps in its map with enabled=false. It runs nothing, so it
// can perform no forbidden work and exports no series.
func TestValidateTierMustNotRun_DisabledComponentCountsAsAbsent(t *testing.T) {
	reg := prometheus.NewRegistry()
	registerUnrelated(t, reg)

	s := metricsFixtureWithComponents(t, reg, []client.ComponentInfo{
		{Name: "graph-clustering", Component: "graph-clustering", Type: "processor",
			Enabled: false, State: "stopped", Healthy: true},
	})
	result := newResult()

	require.NoError(t, s.executeValidateZeroClusters(context.Background(), result))
}

// TestValidateTierMustNotRun_FailsWhenInventoryUnreachable pins the remaining
// hole: if the second source cannot be consulted, the absence claim has no
// corroboration and the gate must not pass on the metrics endpoint alone.
func TestValidateTierMustNotRun_FailsWhenInventoryUnreachable(t *testing.T) {
	reg := prometheus.NewRegistry()
	registerUnrelated(t, reg)

	s := metricsFixture(t, reg)
	s.client = client.NewObservabilityClient("http://127.0.0.1:1") // nothing listening
	result := newResult()

	err := s.executeValidateZeroClusters(context.Background(), result)

	require.Error(t, err)
	require.Contains(t, err.Error(), "unverifiable")

	details, ok := result.Details["zero_clusters_validation"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, false, details["verifiable"])
}

// --- Finding 8: community waits must prove a CURRENT detection cycle ---------

// fakeCommunitySource stands in for NATSValidationClient. onFetch lets a test
// simulate graph-clustering completing a cycle while the wait is in progress.
type fakeCommunitySource struct {
	communities []*clustering.Community
	onFetch     func()
}

func (f *fakeCommunitySource) GetAllCommunities(context.Context) ([]*clustering.Community, error) {
	if f.onFetch != nil {
		f.onFetch()
	}
	return f.communities, nil
}

func staleCommunities() []*clustering.Community {
	return []*clustering.Community{
		{ID: "c-1", Level: 0, Members: []string{"a", "b"}, StatisticalSummary: "from a previous run"},
		{ID: "c-2", Level: 0, Members: []string{"c"}, StatisticalSummary: "also from a previous run"},
	}
}

// TestWaitForCommunities_RejectsPreseededStaleCommunities is THE Finding-8
// regression. COMMUNITY_INDEX is durable NATS state; if the JetStream volume
// survives an earlier run, the first poll returns that run's communities. The
// rewritten wait returned them immediately, so every downstream quality check
// validated stale output. graph-clustering here is deployed and idle: its run
// counter is scrapeable at 0 and never advances.
func TestWaitForCommunities_RejectsPreseededStaleCommunities(t *testing.T) {
	reg := prometheus.NewRegistry()
	registerUnrelated(t, reg)
	require.NoError(t, reg.Register(prometheus.NewHistogram(clusteringHistogramOpts)))

	s := metricsFixture(t, reg)
	source := &fakeCommunitySource{communities: staleCommunities()}

	communities, err := s.waitForCommunitiesFrom(context.Background(), source, 200*time.Millisecond)

	require.Error(t, err, "communities with no detection run behind them are a previous run's output")
	require.Nil(t, communities)
	require.Contains(t, err.Error(), "no community-detection run completed")
	require.Contains(t, err.Error(), "stale")
}

// TestWaitForCommunities_AcceptsCommunitiesFromCurrentCycle is the positive
// case: a detection cycle completes during the wait, so the communities are
// this run's. Without it the Finding-8 fix could be a gate that never passes.
func TestWaitForCommunities_AcceptsCommunitiesFromCurrentCycle(t *testing.T) {
	reg := prometheus.NewRegistry()
	registerUnrelated(t, reg)
	h := prometheus.NewHistogram(clusteringHistogramOpts)
	require.NoError(t, reg.Register(h))

	s := metricsFixture(t, reg)
	// graph-clustering completes a cycle while the wait is polling.
	source := &fakeCommunitySource{
		communities: staleCommunities(),
		onFetch:     func() { h.Observe(3.2) },
	}

	communities, err := s.waitForCommunitiesFrom(context.Background(), source, 5*time.Second)

	require.NoError(t, err)
	require.Len(t, communities, 2)
}

// TestWaitForCommunities_FailsWithoutARunBaseline covers the case where the run
// counter cannot be read at all. Freshness is then undecidable, and undecidable
// is a failure — falling back to "nonempty is good enough" would restore exactly
// the behavior this fix removes.
func TestWaitForCommunities_FailsWithoutARunBaseline(t *testing.T) {
	reg := prometheus.NewRegistry()
	registerUnrelated(t, reg) // no graph_clustering subsystem at all

	s := metricsFixture(t, reg)
	source := &fakeCommunitySource{communities: staleCommunities()}

	_, err := s.waitForCommunitiesFrom(context.Background(), source, 200*time.Millisecond)

	require.Error(t, err)
	require.Contains(t, err.Error(), "community freshness is unverifiable")
}

// TestWaitForCommunities_StillFailsOnEmptyIndex keeps the original behavior:
// no communities at all is a distinct, differently worded failure.
func TestWaitForCommunities_StillFailsOnEmptyIndex(t *testing.T) {
	reg := prometheus.NewRegistry()
	registerUnrelated(t, reg)
	require.NoError(t, reg.Register(prometheus.NewHistogram(clusteringHistogramOpts)))

	s := metricsFixture(t, reg)
	source := &fakeCommunitySource{}

	_, err := s.waitForCommunitiesFrom(context.Background(), source, 200*time.Millisecond)

	require.Error(t, err)
	require.Contains(t, err.Error(), "no communities after")
}
