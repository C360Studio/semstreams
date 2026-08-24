package service

import (
	"fmt"
	"sync"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/c360studio/semstreams/metric"
	"github.com/c360studio/semstreams/pkg/errs"
)

// startupMetricWriter is the sole production owner of the startup GaugeVec.
// Its mutex serializes observation with publication so an older snapshot can
// never overwrite a newer manager-owned outcome.
type startupMetricWriter struct {
	mu                sync.Mutex
	units             *prometheus.GaugeVec
	serviceSnapshot   func() serviceStartupCounts
	componentSnapshot func() startupUnitCounts
}

func newStartupMetricWriter(
	registry *metric.MetricsRegistry,
	serviceSnapshot func() serviceStartupCounts,
	componentSnapshot func() startupUnitCounts,
) (*startupMetricWriter, error) {
	if registry == nil {
		return nil, errs.WrapFatal(
			fmt.Errorf("metrics registry is nil"),
			"startupMetricWriter", "new", "startup metric registry unavailable",
		)
	}
	if serviceSnapshot == nil || componentSnapshot == nil {
		return nil, errs.WrapFatal(
			fmt.Errorf("startup snapshot function is nil"),
			"startupMetricWriter", "new", "startup metric observation unavailable",
		)
	}

	units := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "semstreams",
			Subsystem: "startup",
			Name:      "units",
			Help:      "Process-local startup unit counts by manager owner and fixed lifecycle stage",
		},
		[]string{"owner", "stage"},
	)
	if err := registry.PrometheusRegistry().Register(units); err != nil {
		return nil, errs.WrapFatal(
			err,
			"startupMetricWriter", "new", "register fresh startup metric collector",
		)
	}

	writer := &startupMetricWriter{
		units:             units,
		serviceSnapshot:   serviceSnapshot,
		componentSnapshot: componentSnapshot,
	}
	writer.publishAll()
	return writer, nil
}

func (w *startupMetricWriter) publishAll() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.writeServices(w.serviceSnapshot())
	w.writeComponents(w.componentSnapshot())
}

func (w *startupMetricWriter) publishServices() {
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.writeServices(w.serviceSnapshot())
}

func (w *startupMetricWriter) publishComponents() {
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.writeComponents(w.componentSnapshot())
}

func (w *startupMetricWriter) writeServices(counts serviceStartupCounts) {
	w.units.WithLabelValues("services", "admitted").Set(float64(counts.Admitted))
	w.units.WithLabelValues("services", "starts_invoked").Set(float64(counts.StartsInvoked))
	w.units.WithLabelValues("services", "starts_completed").Set(float64(counts.StartsCompleted))
	w.units.WithLabelValues("services", "starts_failed").Set(float64(counts.StartsFailed))
}

func (w *startupMetricWriter) writeComponents(counts startupUnitCounts) {
	w.units.WithLabelValues("components", "admitted").Set(float64(counts.Admitted))
	w.units.WithLabelValues("components", "lifecycle_participants").Set(float64(counts.LifecycleParticipants))
	w.units.WithLabelValues("components", "starts_invoked").Set(float64(counts.StartsInvoked))
	w.units.WithLabelValues("components", "starts_completed").Set(float64(counts.StartsCompleted))
	w.units.WithLabelValues("components", "starts_failed").Set(float64(counts.StartsFailed))
}
