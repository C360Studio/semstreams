package jsonmapprocessor

import (
	"time"

	"github.com/c360/semstreams/metric"
	"github.com/prometheus/client_golang/prometheus"
)

// mapMetrics holds Prometheus metrics for JSON Map Processor operations.
type mapMetrics struct {
	// Transformation counters
	transformationsTotal  *prometheus.CounterVec // By component_name
	fieldExtractions      *prometheus.CounterVec // By component_name
	extractionErrors      *prometheus.CounterVec // By component_name and error_type

	// Operation errors
	errors *prometheus.CounterVec // By component_name and error_type

	// Performance metrics
	transformationDuration *prometheus.HistogramVec // By component_name
	outputSize             *prometheus.HistogramVec // By component_name - output message size

	// Processing metrics
	fieldsAdded   *prometheus.CounterVec // By component_name
	fieldsRemoved *prometheus.CounterVec // By component_name
	fieldsMapped  *prometheus.CounterVec // By component_name
}

// newMapMetrics creates and registers JSON map metrics with the provided registry.
func newMapMetrics(registry *metric.MetricsRegistry, componentName string) (*mapMetrics, error) {
	if registry == nil {
		return nil, nil // Metrics disabled
	}

	m := &mapMetrics{
		// Transformation counters
		transformationsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "semstreams",
			Subsystem: "json_map",
			Name:      "transformations_total",
			Help:      "Total number of message transformations performed",
		}, []string{"component"}),

		fieldExtractions: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "semstreams",
			Subsystem: "json_map",
			Name:      "field_extractions_total",
			Help:      "Total number of field extractions performed",
		}, []string{"component"}),

		extractionErrors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "semstreams",
			Subsystem: "json_map",
			Name:      "extraction_errors_total",
			Help:      "Total number of field extraction failures",
		}, []string{"component", "error_type"}), // error_type: missing_field, type_mismatch

		errors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "semstreams",
			Subsystem: "json_map",
			Name:      "errors_total",
			Help:      "Total number of transformation errors",
		}, []string{"component", "error_type"}), // error_type: parse, validation, marshal, publish

		// Performance metrics
		transformationDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "semstreams",
			Subsystem: "json_map",
			Name:      "transformation_duration_seconds",
			Help:      "Message transformation duration in seconds",
			Buckets:   []float64{0.0001, 0.0005, 0.001, 0.005, 0.01, 0.05, 0.1}, // Sub-millisecond to 100ms
		}, []string{"component"}),

		outputSize: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "semstreams",
			Subsystem: "json_map",
			Name:      "output_size_bytes",
			Help:      "Distribution of output message sizes in bytes",
			Buckets:   prometheus.ExponentialBuckets(100, 2, 10), // 100B to ~100KB
		}, []string{"component"}),

		// Processing metrics
		fieldsAdded: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "semstreams",
			Subsystem: "json_map",
			Name:      "fields_added_total",
			Help:      "Total number of fields added to messages",
		}, []string{"component"}),

		fieldsRemoved: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "semstreams",
			Subsystem: "json_map",
			Name:      "fields_removed_total",
			Help:      "Total number of fields removed from messages",
		}, []string{"component"}),

		fieldsMapped: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "semstreams",
			Subsystem: "json_map",
			Name:      "fields_mapped_total",
			Help:      "Total number of fields mapped/renamed",
		}, []string{"component"}),
	}

	// Register all metrics
	if err := registry.RegisterCounterVec("json_map", "transformations_total", m.transformationsTotal); err != nil {
		return nil, err
	}
	if err := registry.RegisterCounterVec("json_map", "field_extractions", m.fieldExtractions); err != nil {
		return nil, err
	}
	if err := registry.RegisterCounterVec("json_map", "extraction_errors", m.extractionErrors); err != nil {
		return nil, err
	}
	if err := registry.RegisterCounterVec("json_map", "errors", m.errors); err != nil {
		return nil, err
	}
	if err := registry.RegisterHistogramVec("json_map", "transformation_duration", m.transformationDuration); err != nil {
		return nil, err
	}
	if err := registry.RegisterHistogramVec("json_map", "output_size", m.outputSize); err != nil {
		return nil, err
	}
	if err := registry.RegisterCounterVec("json_map", "fields_added", m.fieldsAdded); err != nil {
		return nil, err
	}
	if err := registry.RegisterCounterVec("json_map", "fields_removed", m.fieldsRemoved); err != nil {
		return nil, err
	}
	if err := registry.RegisterCounterVec("json_map", "fields_mapped", m.fieldsMapped); err != nil {
		return nil, err
	}

	return m, nil
}

// recordTransformation records a successful message transformation.
func (m *mapMetrics) recordTransformation(componentName string, duration time.Duration, outputSizeBytes int) {
	if m == nil {
		return
	}

	m.transformationsTotal.WithLabelValues(componentName).Inc()
	m.transformationDuration.WithLabelValues(componentName).Observe(duration.Seconds())
	m.outputSize.WithLabelValues(componentName).Observe(float64(outputSizeBytes))
}

// recordFieldExtraction records a successful field extraction.
func (m *mapMetrics) recordFieldExtraction(componentName string) {
	if m == nil {
		return
	}

	m.fieldExtractions.WithLabelValues(componentName).Inc()
}

// recordExtractionError records a field extraction error.
func (m *mapMetrics) recordExtractionError(componentName, errorType string) {
	if m == nil {
		return
	}

	m.extractionErrors.WithLabelValues(componentName, errorType).Inc()
}

// recordError records a transformation error.
func (m *mapMetrics) recordError(componentName, errorType string) {
	if m == nil {
		return
	}

	m.errors.WithLabelValues(componentName, errorType).Inc()
}

// recordFieldOperations records field add/remove/map operations.
func (m *mapMetrics) recordFieldOperations(componentName string, added, removed, mapped int) {
	if m == nil {
		return
	}

	if added > 0 {
		m.fieldsAdded.WithLabelValues(componentName).Add(float64(added))
	}
	if removed > 0 {
		m.fieldsRemoved.WithLabelValues(componentName).Add(float64(removed))
	}
	if mapped > 0 {
		m.fieldsMapped.WithLabelValues(componentName).Add(float64(mapped))
	}
}
