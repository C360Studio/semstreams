package graphembedding

import (
	"context"
	"testing"

	"github.com/c360studio/semstreams/graph"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

// assertReadinessGauges asserts every readiness gauge reflects want, including the
// one-hot state gauge (current state == 1, all other states == 0).
func assertReadinessGauges(t *testing.T, m *embeddingMetrics, want graph.IndexStatusResponse) {
	t.Helper()
	wantReady := 0.0
	if want.Ready {
		wantReady = 1
	}
	require.Equal(t, wantReady, testutil.ToFloat64(m.readiness), "readiness")
	require.Equal(t, float64(want.Lag), testutil.ToFloat64(m.lag), "lag")
	require.Equal(t, float64(want.IndexedRevision), testutil.ToFloat64(m.indexedRevision), "indexed_revision")
	require.Equal(t, float64(want.TargetRevision), testutil.ToFloat64(m.targetRevision), "target_revision")
	for _, s := range graph.AllIndexStates {
		exp := 0.0
		if s == want.State {
			exp = 1
		}
		require.Equal(t, exp, testutil.ToFloat64(m.readinessState.WithLabelValues(s)), "state=%s", s)
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
	assertReadinessGauges(t, m, ready)

	building := graph.IndexStatusResponse{
		Ready: false, State: graph.IndexStateBuilding,
		IndexedRevision: 40, TargetRevision: 100, Lag: 60,
	}
	m.setReadinessGauges(building)
	assertReadinessGauges(t, m, building)
	require.Equal(t, 0.0, testutil.ToFloat64(m.readinessState.WithLabelValues(graph.IndexStateReady)),
		"one-hot must clear the previously-set ready state")

	degraded := graph.IndexStatusResponse{
		Ready: false, State: graph.IndexStateDegraded,
		IndexedRevision: 90, TargetRevision: 100, Lag: 10,
	}
	m.setReadinessGauges(degraded)
	assertReadinessGauges(t, m, degraded)
	require.Equal(t, 0.0, testutil.ToFloat64(m.readinessState.WithLabelValues(graph.IndexStateBuilding)),
		"one-hot must clear the previously-set building state")

	reset := graph.IndexStatusResponse{
		Ready: false, State: graph.IndexStateResetRequired,
	}
	m.setReadinessGauges(reset)
	assertReadinessGauges(t, m, reset)
	require.Equal(t, 0.0, testutil.ToFloat64(m.readinessState.WithLabelValues(graph.IndexStateDegraded)),
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

	assertReadinessGauges(t, comp.metrics, want)
	require.Equal(t, graph.IndexStateBuilding, want.State, "nil-watermark fallback should be building")
}
