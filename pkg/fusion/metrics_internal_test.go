package fusion

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/metric"
)

// TestReportBodyFailure_IncrementsThisEngineCounter proves reportBodyFailure — the
// exact call nodeFor makes — stamps the reason on the node and increments THIS
// engine's counter, resolved against the engine's own registry (gh#616). No
// process-global state: the counter is memoized per engine.
func TestReportBodyFailure_IncrementsThisEngineCounter(t *testing.T) {
	reg := metric.NewMetricsRegistry()
	e := (&Engine{}).WithMetrics(reg)

	var n1, n2, n3 Node
	e.reportBodyFailure(&n1, BodyNotFound)
	e.reportBodyFailure(&n2, BodyError)
	e.reportBodyFailure(&n3, BodyError)

	assert.Equal(t, BodyNotFound, n1.BodyReason)
	assert.Equal(t, BodyError, n2.BodyReason)

	assert.Equal(t, float64(1), testutil.ToFloat64(e.bodyFailures.WithLabelValues("not_found")))
	assert.Equal(t, float64(2), testutil.ToFloat64(e.bodyFailures.WithLabelValues("error")))

	// It registered into the PROVIDED registry (the /metrics-scraped one).
	count, err := testutil.GatherAndCount(reg.PrometheusRegistry(),
		"semstreams_fusion_body_hydration_failures_total")
	require.NoError(t, err)
	assert.Equal(t, 2, count, "one series per reason present")
}

// TestResolveBodyHydrationFailureVec_PerRegistry pins the FIX-4 invariant: the
// counter is resolved PER registry with no process-global pinning. Two engines
// sharing one registry increment the SAME series (register-or-get-existing); two
// engines with DIFFERENT registries keep independent series; and neither loses the
// counter to whichever registered first.
func TestResolveBodyHydrationFailureVec_PerRegistry(t *testing.T) {
	t.Run("two engines sharing a registry share the series", func(t *testing.T) {
		reg := metric.NewMetricsRegistry()
		a := (&Engine{}).WithMetrics(reg)
		b := (&Engine{}).WithMetrics(reg)

		var n Node
		a.reportBodyFailure(&n, BodyNotFound)
		b.reportBodyFailure(&n, BodyNotFound)

		// Both increments land on the ONE series in this registry.
		got, err := testutil.GatherAndCount(reg.PrometheusRegistry(),
			"semstreams_fusion_body_hydration_failures_total")
		require.NoError(t, err)
		assert.Equal(t, 1, got, "one shared series, not two")
		assert.Equal(t, float64(2), testutil.ToFloat64(a.bodyFailures.WithLabelValues("not_found")))
		assert.Equal(t, float64(2), testutil.ToFloat64(b.bodyFailures.WithLabelValues("not_found")),
			"the second engine resolved to the same registered vec")
	})

	t.Run("two registries keep independent series", func(t *testing.T) {
		regA := metric.NewMetricsRegistry()
		regB := metric.NewMetricsRegistry()
		a := (&Engine{}).WithMetrics(regA)
		b := (&Engine{}).WithMetrics(regB)

		var n Node
		a.reportBodyFailure(&n, BodyError)

		assert.Equal(t, float64(1), testutil.ToFloat64(a.bodyFailures.WithLabelValues("error")))
		assert.Equal(t, float64(0), testutil.ToFloat64(b.bodyFailures.WithLabelValues("error")),
			"the second registry's counter is independent and untouched")
	})
}

// TestBodyFailureCounter_NilRegistryFallsBackToDefault proves an engine built
// WITHOUT WithMetrics (nil registry — the library default) still counts, resolving
// lazily against the default registerer rather than panicking.
func TestBodyFailureCounter_NilRegistryFallsBackToDefault(t *testing.T) {
	e := &Engine{} // no WithMetrics
	var n Node
	e.reportBodyFailure(&n, BodyNotFound)

	require.NotNil(t, e.bodyFailures)
	assert.GreaterOrEqual(t, testutil.ToFloat64(e.bodyFailures.WithLabelValues("not_found")), float64(1))
}
