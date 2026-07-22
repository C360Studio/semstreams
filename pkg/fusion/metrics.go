package fusion

import (
	"errors"
	"log/slog"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/c360studio/semstreams/metric"
)

// newBodyHydrationFailureVec builds the fusion body-hydration-failure counter.
// Fully-qualified name: semstreams_fusion_body_hydration_failures_total, labelled
// by reason (the closed BodyReason set — not_found | error). It is what makes the
// previously-silent empty body observable (gh#616, #600).
func newBodyHydrationFailureVec() *prometheus.CounterVec {
	return prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "semstreams",
		Subsystem: "fusion",
		Name:      "body_hydration_failures_total",
		Help:      "Count of fusion nodes whose requested verbatim body (WantBody) could not be loaded, by reason (gh#616, #600). Label: reason (not_found | error).",
	}, []string{"reason"})
}

// resolveBodyHydrationFailureVec resolves the body-hydration-failure counter
// against a SPECIFIC registry (nil → the default Prometheus registerer). It holds
// no process-global state: each Engine memoizes its own result, so two engines
// with DIFFERENT registries each get their own series and never lose the counter
// to whichever registered first.
//
// Registration is register-or-get-existing: a fresh registry receives a new vec; a
// registry that already carries this metric (two engines sharing one registry)
// yields the ALREADY-registered vec via prometheus.AlreadyRegisteredError, so both
// engines increment the same series. This is deliberately a direct Register on the
// underlying Registerer rather than metric.MetricsRegistry.RegisterCounterVec —
// that wrapper reports success without handing back the existing collector, which
// would leave a second engine holding an unregistered (invisible) vec.
func resolveBodyHydrationFailureVec(registry *metric.MetricsRegistry) *prometheus.CounterVec {
	vec := newBodyHydrationFailureVec()

	var reg prometheus.Registerer = prometheus.DefaultRegisterer
	if registry != nil {
		reg = registry.PrometheusRegistry()
	}

	if err := reg.Register(vec); err != nil {
		var already prometheus.AlreadyRegisteredError
		if errors.As(err, &already) {
			if existing, ok := already.ExistingCollector.(*prometheus.CounterVec); ok {
				return existing
			}
		}
		// Any other registration error (should not happen for a well-formed,
		// uniquely-named vec — it takes a same-name/different-descriptor collision):
		// emit a LOUD operational signal so the invisible-counter outcome is not
		// itself silent, then fall back to the unregistered vec. A CounterVec still
		// counts when unregistered — the samples simply aren't scraped — so
		// increments stay safe rather than panicking (gh#616, #600).
		slog.Default().Error(
			"fusion body-hydration-failure metric registration failed; "+
				"the counter will still increment but is NOT scraped",
			slog.String("metric", "semstreams_fusion_body_hydration_failures_total"),
			slog.Any("error", err))
	}
	return vec
}
