package fusion

import (
	"bytes"
	"log/slog"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
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

// TestResolveBodyHydrationFailureVec_LoudOnRegistrationFailure pins FINDING-4:
// a registration error that is NOT AlreadyRegisteredError (a same-name /
// different-descriptor collision — impossible in practice, but the only path that
// returns an UNREGISTERED, never-scraped vec) must emit a LOUD operational signal
// rather than silently counting into an invisible collector. It must NOT panic and
// must still return a usable (counting) vec.
//
// NOTE: no t.Parallel — this test swaps the process-global slog default.
func TestResolveBodyHydrationFailureVec_LoudOnRegistrationFailure(t *testing.T) {
	reg := metric.NewMetricsRegistry()

	// Pre-register a DIFFERENT collector under the SAME fully-qualified name
	// (semstreams_fusion_body_hydration_failures_total) but a different descriptor
	// (different label set). Prometheus rejects the subsequent Register with a plain
	// "inconsistent descriptor" error — NOT AlreadyRegisteredError — which is the
	// exact branch under test.
	conflicting := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "semstreams",
		Subsystem: "fusion",
		Name:      "body_hydration_failures_total",
		Help:      "conflicting collector with a different descriptor",
	}, []string{"different_label"})
	require.NoError(t, reg.PrometheusRegistry().Register(conflicting))

	// Capture the loud signal off the process-global slog default.
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelError})))
	defer slog.SetDefault(prev)

	var vec *prometheus.CounterVec
	require.NotPanics(t, func() { vec = resolveBodyHydrationFailureVec(reg) })
	require.NotNil(t, vec, "must still return a usable vec on the failure path")

	// The vec still counts (unregistered, but safe) rather than panicking.
	require.NotPanics(t, func() { vec.WithLabelValues("error").Inc() })

	logged := buf.String()
	assert.Contains(t, logged, "level=ERROR", "the failure must be logged at ERROR")
	assert.Contains(t, logged, "semstreams_fusion_body_hydration_failures_total",
		"the log must name the metric that failed to register")
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
