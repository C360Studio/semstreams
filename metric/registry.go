package metric

import (
	stderrors "errors"
	"fmt"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"

	"github.com/c360/semstreams/errors"
)

// MetricsRegistrar defines the interface for registering service-specific metrics
type MetricsRegistrar interface {
	RegisterCounter(serviceName, metricName string, counter prometheus.Counter) error
	RegisterGauge(serviceName, metricName string, gauge prometheus.Gauge) error
	RegisterHistogram(serviceName, metricName string, histogram prometheus.Histogram) error
	RegisterCounterVec(serviceName, metricName string, counterVec *prometheus.CounterVec) error
	RegisterGaugeVec(serviceName, metricName string, gaugeVec *prometheus.GaugeVec) error
	RegisterHistogramVec(serviceName, metricName string, histogramVec *prometheus.HistogramVec) error
	Unregister(serviceName, metricName string) bool
}

// MetricsRegistry manages the registration and lifecycle of metrics
type MetricsRegistry struct {
	prometheusRegistry *prometheus.Registry
	Metrics            *Metrics
	registeredMetrics  map[string]prometheus.Collector
	mu                 sync.RWMutex
}

// NewMetricsRegistry creates a new metrics registry with core platform metrics
func NewMetricsRegistry() *MetricsRegistry {
	prometheusRegistry := prometheus.NewRegistry()

	registry := &MetricsRegistry{
		prometheusRegistry: prometheusRegistry,
		registeredMetrics:  make(map[string]prometheus.Collector),
	}

	// Initialize and register core metrics
	registry.Metrics = NewMetrics()
	registry.registerMetrics()

	// Add Go runtime metrics
	registry.prometheusRegistry.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)

	return registry
}

// PrometheusRegistry returns the underlying Prometheus registry
func (r *MetricsRegistry) PrometheusRegistry() *prometheus.Registry {
	return r.prometheusRegistry
}

// CoreMetrics returns the core platform metrics
func (r *MetricsRegistry) CoreMetrics() *Metrics {
	return r.Metrics
}

// RegisterCounter registers a counter metric for a service
func (r *MetricsRegistry) RegisterCounter(serviceName, metricName string, counter prometheus.Counter) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	key := fmt.Sprintf("%s.%s", serviceName, metricName)

	if _, exists := r.registeredMetrics[key]; exists {
		return errors.WrapInvalid(
			fmt.Errorf("metric %s already registered for service %s", metricName, serviceName),
			"MetricsRegistry", "RegisterCounter", "duplicate metric registration")
	}

	if err := r.prometheusRegistry.Register(counter); err != nil {
		// Check if it's a duplicate registration error from Prometheus
		var alreadyRegErr prometheus.AlreadyRegisteredError
		if stderrors.As(err, &alreadyRegErr) {
			return errors.WrapInvalid(err, "MetricsRegistry", "RegisterCounter",
				fmt.Sprintf("prometheus conflict for metric %s", metricName))
		}
		return errors.WrapFatal(err, "MetricsRegistry", "RegisterCounter",
			"failed to register counter with prometheus")
	}

	r.registeredMetrics[key] = counter
	return nil
}

// RegisterGauge registers a gauge metric for a service
func (r *MetricsRegistry) RegisterGauge(serviceName, metricName string, gauge prometheus.Gauge) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	key := fmt.Sprintf("%s.%s", serviceName, metricName)

	if _, exists := r.registeredMetrics[key]; exists {
		return errors.WrapInvalid(
			fmt.Errorf("metric %s already registered for service %s", metricName, serviceName),
			"MetricsRegistry", "RegisterGauge", "duplicate metric registration")
	}

	if err := r.prometheusRegistry.Register(gauge); err != nil {
		// Check if it's a duplicate registration error from Prometheus
		var alreadyRegErr prometheus.AlreadyRegisteredError
		if stderrors.As(err, &alreadyRegErr) {
			return errors.WrapInvalid(err, "MetricsRegistry", "RegisterGauge",
				fmt.Sprintf("prometheus conflict for metric %s", metricName))
		}
		return errors.WrapFatal(err, "MetricsRegistry", "RegisterGauge",
			"failed to register gauge with prometheus")
	}

	r.registeredMetrics[key] = gauge
	return nil
}

// RegisterHistogram registers a histogram metric for a service
func (r *MetricsRegistry) RegisterHistogram(serviceName, metricName string, histogram prometheus.Histogram) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	key := fmt.Sprintf("%s.%s", serviceName, metricName)

	if _, exists := r.registeredMetrics[key]; exists {
		return errors.WrapInvalid(
			fmt.Errorf("metric %s already registered for service %s", metricName, serviceName),
			"MetricsRegistry", "RegisterHistogram", "duplicate metric registration")
	}

	if err := r.prometheusRegistry.Register(histogram); err != nil {
		// Check if it's a duplicate registration error from Prometheus
		var alreadyRegErr prometheus.AlreadyRegisteredError
		if stderrors.As(err, &alreadyRegErr) {
			return errors.WrapInvalid(err, "MetricsRegistry", "RegisterHistogram",
				fmt.Sprintf("prometheus conflict for metric %s", metricName))
		}
		return errors.WrapFatal(err, "MetricsRegistry", "RegisterHistogram",
			"failed to register histogram with prometheus")
	}

	r.registeredMetrics[key] = histogram
	return nil
}

// RegisterCounterVec registers a counter vector metric for a service
func (r *MetricsRegistry) RegisterCounterVec(serviceName, metricName string, counterVec *prometheus.CounterVec) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	key := fmt.Sprintf("%s.%s", serviceName, metricName)

	if _, exists := r.registeredMetrics[key]; exists {
		return errors.WrapInvalid(
			fmt.Errorf("metric %s already registered for service %s", metricName, serviceName),
			"MetricsRegistry", "RegisterCounterVec", "duplicate metric registration")
	}

	if err := r.prometheusRegistry.Register(counterVec); err != nil {
		// Check if it's a duplicate registration error from Prometheus
		var alreadyRegErr prometheus.AlreadyRegisteredError
		if stderrors.As(err, &alreadyRegErr) {
			return errors.WrapInvalid(err, "MetricsRegistry", "RegisterCounterVec",
				fmt.Sprintf("prometheus conflict for metric %s", metricName))
		}
		return errors.WrapFatal(err, "MetricsRegistry", "RegisterCounterVec",
			"failed to register counter vector with prometheus")
	}

	r.registeredMetrics[key] = counterVec
	return nil
}

// RegisterGaugeVec registers a gauge vector metric for a service
func (r *MetricsRegistry) RegisterGaugeVec(serviceName, metricName string, gaugeVec *prometheus.GaugeVec) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	key := fmt.Sprintf("%s.%s", serviceName, metricName)

	if _, exists := r.registeredMetrics[key]; exists {
		return errors.WrapInvalid(
			fmt.Errorf("metric %s already registered for service %s", metricName, serviceName),
			"MetricsRegistry", "RegisterGaugeVec", "duplicate metric registration")
	}

	if err := r.prometheusRegistry.Register(gaugeVec); err != nil {
		// Check if it's a duplicate registration error from Prometheus
		var alreadyRegErr prometheus.AlreadyRegisteredError
		if stderrors.As(err, &alreadyRegErr) {
			return errors.WrapInvalid(err, "MetricsRegistry", "RegisterGaugeVec",
				fmt.Sprintf("prometheus conflict for metric %s", metricName))
		}
		return errors.WrapFatal(err, "MetricsRegistry", "RegisterGaugeVec",
			"failed to register gauge vector with prometheus")
	}

	r.registeredMetrics[key] = gaugeVec
	return nil
}

// RegisterHistogramVec registers a histogram vector metric for a service
func (r *MetricsRegistry) RegisterHistogramVec(
	serviceName, metricName string, histogramVec *prometheus.HistogramVec) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	key := fmt.Sprintf("%s.%s", serviceName, metricName)

	if _, exists := r.registeredMetrics[key]; exists {
		return errors.WrapInvalid(
			fmt.Errorf("metric %s already registered for service %s", metricName, serviceName),
			"MetricsRegistry", "RegisterHistogramVec", "duplicate metric registration")
	}

	if err := r.prometheusRegistry.Register(histogramVec); err != nil {
		// Check if it's a duplicate registration error from Prometheus
		var alreadyRegErr prometheus.AlreadyRegisteredError
		if stderrors.As(err, &alreadyRegErr) {
			return errors.WrapInvalid(err, "MetricsRegistry", "RegisterHistogramVec",
				fmt.Sprintf("prometheus conflict for metric %s", metricName))
		}
		return errors.WrapFatal(err, "MetricsRegistry", "RegisterHistogramVec",
			"failed to register histogram vector with prometheus")
	}

	r.registeredMetrics[key] = histogramVec
	return nil
}

// Unregister removes a metric from the registry
func (r *MetricsRegistry) Unregister(serviceName, metricName string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	key := fmt.Sprintf("%s.%s", serviceName, metricName)

	collector, exists := r.registeredMetrics[key]
	if !exists {
		return false
	}

	success := r.prometheusRegistry.Unregister(collector)
	if success {
		delete(r.registeredMetrics, key)
	}

	return success
}

// register Metrics registers all core platform metrics
func (r *MetricsRegistry) registerMetrics() {
	r.prometheusRegistry.MustRegister(
		r.Metrics.ServiceStatus,
		r.Metrics.MessagesReceived,
		r.Metrics.MessagesProcessed,
		r.Metrics.MessagesPublished,
		r.Metrics.ProcessingDuration,
		r.Metrics.ErrorsTotal,
		r.Metrics.HealthCheckStatus,
		r.Metrics.NATSConnected,
		r.Metrics.NATSRTT,
		r.Metrics.NATSReconnects,
		r.Metrics.NATSCircuitBreaker,
	)
}
