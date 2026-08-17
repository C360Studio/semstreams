package flowengine

import (
	"strings"

	"github.com/c360studio/semstreams/metric"
	"github.com/prometheus/client_golang/prometheus"
)

// engineMetrics holds Prometheus metrics for Flow Engine operations.
type engineMetrics struct {
	validateDuration *prometheus.HistogramVec // By flow_id

	// Validation metrics
	validationErrors *prometheus.CounterVec // By flow_id and error_type
}

// newEngineMetrics creates and registers Flow Engine metrics with the provided registry.
func newEngineMetrics(registry *metric.MetricsRegistry) (*engineMetrics, error) {
	if registry == nil {
		return nil, nil // Metrics disabled
	}

	m := &engineMetrics{
		validateDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "semstreams",
			Subsystem: "flow",
			Name:      "validate_duration_seconds",
			Help:      "Flow validation duration in seconds",
			Buckets:   []float64{0.01, 0.05, 0.1, 0.5, 1.0},
		}, []string{"flow_id"}),

		// Validation errors
		validationErrors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "semstreams",
			Subsystem: "flow",
			Name:      "validation_errors_total",
			Help:      "Total number of flow validation errors",
		}, []string{"flow_id", "error_type"}), // error_type: structural, graph, port, etc.
	}

	// Register all metrics
	if err := registry.RegisterHistogramVec("flow", "validate_duration", m.validateDuration); err != nil {
		return nil, err
	}
	if err := registry.RegisterCounterVec("flow", "validation_errors", m.validationErrors); err != nil {
		return nil, err
	}
	return m, nil
}

// recordValidation records a flow validation operation.
func (m *engineMetrics) recordValidation(flowID string, duration float64, err error) {
	if m == nil {
		return
	}

	m.validateDuration.WithLabelValues(flowID).Observe(duration)

	if err != nil {
		// Determine error type from error message
		errorType := "unknown"
		errMsg := err.Error()
		if strings.Contains(errMsg, "structural") || strings.Contains(errMsg, "basic validation") {
			errorType = "structural"
		} else if strings.Contains(errMsg, "graph") || strings.Contains(errMsg, "connectivity") {
			errorType = "graph"
		} else if strings.Contains(errMsg, "port") || strings.Contains(errMsg, "schema") {
			errorType = "port_mismatch"
		}

		m.validationErrors.WithLabelValues(flowID, errorType).Inc()
	}
}
