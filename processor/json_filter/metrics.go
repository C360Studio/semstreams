package jsonfilter

import (
	"time"

	"github.com/c360/semstreams/metric"
	"github.com/prometheus/client_golang/prometheus"
)

// filterMetrics holds Prometheus metrics for JSON Filter Processor operations.
type filterMetrics struct {
	// Message counters
	messagesTotal *prometheus.CounterVec // By component_name and status (matched/rejected/error)
	matched       *prometheus.CounterVec // By component_name
	rejected      *prometheus.CounterVec // By component_name
	errors        *prometheus.CounterVec // By component_name and error_type

	// Performance metrics
	evaluationDuration *prometheus.HistogramVec // By component_name

	// Effectiveness metrics
	filterEffectiveness prometheus.Gauge // Match rate (matched / total)
}

// newFilterMetrics creates and registers JSON filter metrics with the provided registry.
func newFilterMetrics(registry *metric.MetricsRegistry, componentName string) (*filterMetrics, error) {
	if registry == nil {
		return nil, nil // Metrics disabled
	}

	m := &filterMetrics{
		// Message counters
		messagesTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "semstreams",
			Subsystem: "json_filter",
			Name:      "messages_total",
			Help:      "Total number of messages evaluated by filter",
		}, []string{"component", "status"}), // status: matched, rejected, error

		matched: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "semstreams",
			Subsystem: "json_filter",
			Name:      "matched_total",
			Help:      "Total number of messages that matched filter rules",
		}, []string{"component"}),

		rejected: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "semstreams",
			Subsystem: "json_filter",
			Name:      "rejected_total",
			Help:      "Total number of messages that did not match filter rules",
		}, []string{"component"}),

		errors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "semstreams",
			Subsystem: "json_filter",
			Name:      "errors_total",
			Help:      "Total number of filter evaluation errors",
		}, []string{"component", "error_type"}), // error_type: parse, validation, publish

		// Performance metrics
		evaluationDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "semstreams",
			Subsystem: "json_filter",
			Name:      "evaluation_duration_seconds",
			Help:      "Filter evaluation duration in seconds",
			Buckets:   []float64{0.0001, 0.0005, 0.001, 0.005, 0.01, 0.05, 0.1}, // Sub-millisecond to 100ms
		}, []string{"component"}),

		// Effectiveness metrics
		filterEffectiveness: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "semstreams",
			Subsystem: "json_filter",
			Name:      "match_rate",
			Help:      "Current filter match rate (matched / total messages)",
		}),
	}

	// Register all metrics
	if err := registry.RegisterCounterVec("json_filter", "messages_total", m.messagesTotal); err != nil {
		return nil, err
	}
	if err := registry.RegisterCounterVec("json_filter", "matched", m.matched); err != nil {
		return nil, err
	}
	if err := registry.RegisterCounterVec("json_filter", "rejected", m.rejected); err != nil {
		return nil, err
	}
	if err := registry.RegisterCounterVec("json_filter", "errors", m.errors); err != nil {
		return nil, err
	}
	if err := registry.RegisterHistogramVec("json_filter", "evaluation_duration", m.evaluationDuration); err != nil {
		return nil, err
	}
	if err := registry.RegisterGauge("json_filter", "match_rate", m.filterEffectiveness); err != nil {
		return nil, err
	}

	return m, nil
}

// recordEvaluation records a filter evaluation operation.
func (m *filterMetrics) recordEvaluation(componentName string, matched bool, duration time.Duration) {
	if m == nil {
		return
	}

	status := "rejected"
	if matched {
		status = "matched"
		m.matched.WithLabelValues(componentName).Inc()
	} else {
		m.rejected.WithLabelValues(componentName).Inc()
	}

	m.messagesTotal.WithLabelValues(componentName, status).Inc()
	m.evaluationDuration.WithLabelValues(componentName).Observe(duration.Seconds())
}

// recordError records a filter processing error.
func (m *filterMetrics) recordError(componentName, errorType string) {
	if m == nil {
		return
	}

	m.errors.WithLabelValues(componentName, errorType).Inc()
	m.messagesTotal.WithLabelValues(componentName, "error").Inc()
}

// updateMatchRate updates the filter effectiveness metric.
func (m *filterMetrics) updateMatchRate(matched, total int64) {
	if m == nil || total == 0 {
		return
	}

	matchRate := float64(matched) / float64(total)
	m.filterEffectiveness.Set(matchRate)
}
