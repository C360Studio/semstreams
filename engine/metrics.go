package flowengine

import (
	"strings"

	"github.com/c360/semstreams/metric"
	"github.com/prometheus/client_golang/prometheus"
)

// engineMetrics holds Prometheus metrics for Flow Engine operations.
type engineMetrics struct {
	// Flow lifecycle operations
	deploys *prometheus.CounterVec // By flow_id and status (success/failure)
	starts  *prometheus.CounterVec // By flow_id and status
	stops   *prometheus.CounterVec // By flow_id and status

	// Operation latency
	deployDuration   *prometheus.HistogramVec // By flow_id
	startDuration    *prometheus.HistogramVec // By flow_id
	stopDuration     *prometheus.HistogramVec // By flow_id
	validateDuration *prometheus.HistogramVec // By flow_id

	// Validation metrics
	validationErrors *prometheus.CounterVec // By flow_id and error_type

	// State metrics
	activeFlows prometheus.Gauge // Current number of running flows
}

// newEngineMetrics creates and registers Flow Engine metrics with the provided registry.
func newEngineMetrics(registry *metric.MetricsRegistry) (*engineMetrics, error) {
	if registry == nil {
		return nil, nil // Metrics disabled
	}

	m := &engineMetrics{
		// Lifecycle operations
		deploys: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "semstreams",
			Subsystem: "flow",
			Name:      "deploys_total",
			Help:      "Total number of flow deploy operations",
		}, []string{"flow_id", "status"}), // status: success, failure

		starts: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "semstreams",
			Subsystem: "flow",
			Name:      "starts_total",
			Help:      "Total number of flow start operations",
		}, []string{"flow_id", "status"}),

		stops: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "semstreams",
			Subsystem: "flow",
			Name:      "stops_total",
			Help:      "Total number of flow stop operations",
		}, []string{"flow_id", "status"}),

		// Operation durations
		deployDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "semstreams",
			Subsystem: "flow",
			Name:      "deploy_duration_seconds",
			Help:      "Flow deploy operation duration in seconds",
			Buckets:   []float64{0.1, 0.5, 1.0, 2.0, 5.0, 10.0},
		}, []string{"flow_id"}),

		startDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "semstreams",
			Subsystem: "flow",
			Name:      "start_duration_seconds",
			Help:      "Flow start operation duration in seconds",
			Buckets:   []float64{0.1, 0.5, 1.0, 2.0, 5.0, 10.0},
		}, []string{"flow_id"}),

		stopDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "semstreams",
			Subsystem: "flow",
			Name:      "stop_duration_seconds",
			Help:      "Flow stop operation duration in seconds",
			Buckets:   []float64{0.1, 0.5, 1.0, 2.0, 5.0},
		}, []string{"flow_id"}),

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

		// State metrics
		activeFlows: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "semstreams",
			Subsystem: "flow",
			Name:      "active_flows",
			Help:      "Current number of active (running) flows",
		}),
	}

	// Register all metrics
	if err := registry.RegisterCounterVec("flow", "deploys", m.deploys); err != nil {
		return nil, err
	}
	if err := registry.RegisterCounterVec("flow", "starts", m.starts); err != nil {
		return nil, err
	}
	if err := registry.RegisterCounterVec("flow", "stops", m.stops); err != nil {
		return nil, err
	}
	if err := registry.RegisterHistogramVec("flow", "deploy_duration", m.deployDuration); err != nil {
		return nil, err
	}
	if err := registry.RegisterHistogramVec("flow", "start_duration", m.startDuration); err != nil {
		return nil, err
	}
	if err := registry.RegisterHistogramVec("flow", "stop_duration", m.stopDuration); err != nil {
		return nil, err
	}
	if err := registry.RegisterHistogramVec("flow", "validate_duration", m.validateDuration); err != nil {
		return nil, err
	}
	if err := registry.RegisterCounterVec("flow", "validation_errors", m.validationErrors); err != nil {
		return nil, err
	}
	if err := registry.RegisterGauge("flow", "active_flows", m.activeFlows); err != nil {
		return nil, err
	}

	return m, nil
}

// recordDeploy records a flow deploy operation.
func (m *engineMetrics) recordDeploy(flowID string, success bool, duration float64) {
	if m == nil {
		return
	}

	status := "success"
	if !success {
		status = "failure"
	}

	m.deploys.WithLabelValues(flowID, status).Inc()
	m.deployDuration.WithLabelValues(flowID).Observe(duration)
}

// recordStart records a flow start operation.
func (m *engineMetrics) recordStart(flowID string, success bool, duration float64) {
	if m == nil {
		return
	}

	status := "success"
	if !success {
		status = "failure"
	}

	m.starts.WithLabelValues(flowID, status).Inc()
	m.startDuration.WithLabelValues(flowID).Observe(duration)

	// Update active flows count on successful start
	if success {
		m.activeFlows.Inc()
	}
}

// recordStop records a flow stop operation.
func (m *engineMetrics) recordStop(flowID string, success bool, duration float64) {
	if m == nil {
		return
	}

	status := "success"
	if !success {
		status = "failure"
	}

	m.stops.WithLabelValues(flowID, status).Inc()
	m.stopDuration.WithLabelValues(flowID).Observe(duration)

	// Update active flows count on successful stop
	if success {
		m.activeFlows.Dec()
	}
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

// setActiveFlows sets the active flows count directly (for initialization/sync).
func (m *engineMetrics) setActiveFlows(count float64) {
	if m != nil {
		m.activeFlows.Set(count)
	}
}
