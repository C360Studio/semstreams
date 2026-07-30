package graphembedding

import (
	"context"
	"testing"

	"github.com/c360studio/semstreams/graph"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
)

// assertReadinessGauges asserts every readiness gauge reflects want, including the
// one-hot state gauge (current state == 1, all other states == 0).
// seriesValue reads one EMITTED Prometheus series by fully-qualified name, optionally
// filtered to a label value.
//
// The readiness gauges moved into the shared readiness.Gauges set (#763), so their
// fields are no longer reachable from here. Asserting on the emitted series is the
// better test anyway: metric NAMES are the external contract dashboards consume, and a
// field-level assertion would still pass if the namespace or subsystem were renamed.
func seriesValue(t *testing.T, name, labelValue string) float64 {
	t.Helper()
	families, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err, "gather")
	for _, f := range families {
		if f.GetName() != name {
			continue
		}
		for _, m := range f.GetMetric() {
			if labelValue == "" {
				if g := m.GetGauge(); g != nil {
					return g.GetValue()
				}
				return m.GetCounter().GetValue()
			}
			for _, lp := range m.GetLabel() {
				if lp.GetValue() == labelValue {
					return m.GetGauge().GetValue()
				}
			}
		}
	}
	t.Fatalf("series %q (label %q) not emitted — a dashboard querying it would go dark", name, labelValue)
	return 0
}

func assertReadinessGauges(t *testing.T, want graph.IndexStatusResponse) {
	t.Helper()
	wantReady := 0.0
	if want.Ready {
		wantReady = 1
	}
	require.Equal(t, wantReady, seriesValue(t, "semstreams_graph_embedding_readiness", ""), "readiness")
	require.Equal(t, float64(want.Lag), seriesValue(t, "semstreams_graph_embedding_lag", ""), "lag")
	require.Equal(t, float64(want.IndexedRevision), seriesValue(t, "semstreams_graph_embedding_indexed_revision", ""), "indexed_revision")
	require.Equal(t, float64(want.TargetRevision), seriesValue(t, "semstreams_graph_embedding_target_revision", ""), "target_revision")
	// bootstrap_complete is the field BOTH hand-rolled implementations omitted, and
	// the reason the shared set exists (#763). Asserted here so this producer's own
	// suite would fail if it went missing again.
	wantBootstrap := 0.0
	if want.BootstrapComplete {
		wantBootstrap = 1
	}
	require.Equal(t, wantBootstrap,
		seriesValue(t, "semstreams_graph_embedding_bootstrap_complete", ""), "bootstrap_complete")

	for _, s := range graph.AllIndexStates {
		exp := 0.0
		if s == want.State {
			exp = 1
		}
		require.Equal(t, exp, seriesValue(t, "semstreams_graph_embedding_readiness_state", s), "state=%s", s)
	}
}

// TestSetReadinessGauges_MapsEnvelope proves setReadinessGauges maps a fabricated
// IndexStatusResponse onto the gauges, and that the state gauge is one-hot: a later
// state clears the prior state's 1 rather than leaving it lingering.
func TestSetReadinessGauges_MapsEnvelope(t *testing.T) {
	m := getMetrics(nil)

	ready := graph.IndexStatusResponse{
		Ready: true, State: graph.IndexStateReady,
		IndexedRevision: 100, TargetRevision: 100, Lag: 0,
	}
	m.setReadinessGauges(ready)
	assertReadinessGauges(t, ready)

	building := graph.IndexStatusResponse{
		Ready: false, State: graph.IndexStateBuilding,
		IndexedRevision: 40, TargetRevision: 100, Lag: 60,
	}
	m.setReadinessGauges(building)
	assertReadinessGauges(t, building)
	require.Equal(t, 0.0, seriesValue(t, "semstreams_graph_embedding_readiness_state", graph.IndexStateReady),
		"one-hot must clear the previously-set ready state")

	degraded := graph.IndexStatusResponse{
		Ready: false, State: graph.IndexStateDegraded,
		IndexedRevision: 90, TargetRevision: 100, Lag: 10,
	}
	m.setReadinessGauges(degraded)
	assertReadinessGauges(t, degraded)
	require.Equal(t, 0.0, seriesValue(t, "semstreams_graph_embedding_readiness_state", graph.IndexStateBuilding),
		"one-hot must clear the previously-set building state")

	reset := graph.IndexStatusResponse{
		Ready: false, State: graph.IndexStateResetRequired,
	}
	m.setReadinessGauges(reset)
	assertReadinessGauges(t, reset)
	require.Equal(t, 0.0, seriesValue(t, "semstreams_graph_embedding_readiness_state", graph.IndexStateDegraded),
		"one-hot must clear the previously-set degraded state")
}

// TestRefreshReadinessMetrics_DrivesRealComputePath proves the tick body publishes
// the gauges from the REAL computeEmbeddingStatus output (not a reimplementation)
// with no NATS status query. The component has a nil watermark, so
// computeEmbeddingStatus returns the honest early-boot fallback
// ({Ready:false, State:building}) without a BucketLastSeq NATS call.
func TestRefreshReadinessMetrics_DrivesRealComputePath(t *testing.T) {
	comp := &Component{metrics: getMetrics(nil)}
	ctx := context.Background()

	want := comp.computeEmbeddingStatus(ctx)
	comp.refreshReadinessMetrics(ctx)

	assertReadinessGauges(t, want)
	require.Equal(t, graph.IndexStateBuilding, want.State, "nil-watermark fallback should be building")
}
