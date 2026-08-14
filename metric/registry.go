package metric

import (
	stderrors "errors"
	"fmt"
	"reflect"
	"sort"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"

	"github.com/c360studio/semstreams/pkg/errs"
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

// RegisterOrGetGaugeVec registers candidate or returns the compatible canonical
// GaugeVec already registered under the same service/metric key.
func (r *MetricsRegistry) RegisterOrGetGaugeVec(
	serviceName, metricName string,
	candidate *prometheus.GaugeVec,
) (*prometheus.GaugeVec, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if candidate == nil {
		return nil, errs.WrapFatal(fmt.Errorf("candidate gauge vector is nil"),
			"MetricsRegistry", "RegisterOrGetGaugeVec", "invalid gauge vector registration")
	}

	key := fmt.Sprintf("%s.%s", serviceName, metricName)
	if existingCollector, exists := r.registeredMetrics[key]; exists {
		existing, ok := existingCollector.(*prometheus.GaugeVec)
		if !ok {
			return nil, errs.WrapFatal(fmt.Errorf("logical metric key %q is registered as %T", key, existingCollector),
				"MetricsRegistry", "RegisterOrGetGaugeVec", "registered collector is not a gauge vector")
		}
		if !sameCollectorDescriptors(existing, candidate) {
			return nil, errs.WrapFatal(fmt.Errorf("logical metric key %q has a different descriptor", key),
				"MetricsRegistry", "RegisterOrGetGaugeVec", "incompatible gauge vector registration")
		}
		return existing, nil
	}
	if err := r.prometheusRegistry.Register(candidate); err != nil {
		var alreadyRegistered prometheus.AlreadyRegisteredError
		if !stderrors.As(err, &alreadyRegistered) {
			return nil, errs.WrapFatal(err, "MetricsRegistry", "RegisterOrGetGaugeVec",
				"incompatible gauge vector registration")
		}
		return nil, errs.WrapFatal(err, "MetricsRegistry", "RegisterOrGetGaugeVec",
			"collector descriptor is already owned by a different logical metric key")
	}
	r.registeredMetrics[key] = candidate
	return candidate, nil
}

func sameCollectorDescriptors(left, right prometheus.Collector) bool {
	return reflect.DeepEqual(collectorDescriptors(left), collectorDescriptors(right))
}

func collectorDescriptors(collector prometheus.Collector) []string {
	if collector == nil {
		return nil
	}
	descriptors := make(chan *prometheus.Desc)
	values := make([]string, 0, 1)
	go func() {
		collector.Describe(descriptors)
		close(descriptors)
	}()
	for descriptor := range descriptors {
		values = append(values, descriptor.String())
	}
	sort.Strings(values)
	return values
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

// RegisterCounter registers a counter metric for a service.
// Idempotent: returns success if metric already registered with same key.
func (r *MetricsRegistry) RegisterCounter(serviceName, metricName string, counter prometheus.Counter) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	key := fmt.Sprintf("%s.%s", serviceName, metricName)

	// Idempotent: if already registered with same key, return success
	if _, exists := r.registeredMetrics[key]; exists {
		return nil
	}

	if err := r.prometheusRegistry.Register(counter); err != nil {
		// Check if it's a duplicate registration error from Prometheus
		var alreadyRegErr prometheus.AlreadyRegisteredError
		if stderrors.As(err, &alreadyRegErr) {
			// Prometheus already has this metric - treat as success for idempotency
			r.registeredMetrics[key] = counter
			return nil
		}
		return errs.WrapFatal(err, "MetricsRegistry", "RegisterCounter",
			"failed to register counter with prometheus")
	}

	r.registeredMetrics[key] = counter
	return nil
}

// RegisterGauge registers a gauge metric for a service.
// Idempotent: returns success if metric already registered with same key.
func (r *MetricsRegistry) RegisterGauge(serviceName, metricName string, gauge prometheus.Gauge) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	key := fmt.Sprintf("%s.%s", serviceName, metricName)

	// Idempotent: if already registered with same key, return success
	if _, exists := r.registeredMetrics[key]; exists {
		return nil
	}

	if err := r.prometheusRegistry.Register(gauge); err != nil {
		// Check if it's a duplicate registration error from Prometheus
		var alreadyRegErr prometheus.AlreadyRegisteredError
		if stderrors.As(err, &alreadyRegErr) {
			// Prometheus already has this metric - treat as success for idempotency
			r.registeredMetrics[key] = gauge
			return nil
		}
		return errs.WrapFatal(err, "MetricsRegistry", "RegisterGauge",
			"failed to register gauge with prometheus")
	}

	r.registeredMetrics[key] = gauge
	return nil
}

// RegisterHistogram registers a histogram metric for a service.
// Idempotent: returns success if metric already registered with same key.
func (r *MetricsRegistry) RegisterHistogram(serviceName, metricName string, histogram prometheus.Histogram) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	key := fmt.Sprintf("%s.%s", serviceName, metricName)

	// Idempotent: if already registered with same key, return success
	if _, exists := r.registeredMetrics[key]; exists {
		return nil
	}

	if err := r.prometheusRegistry.Register(histogram); err != nil {
		// Check if it's a duplicate registration error from Prometheus
		var alreadyRegErr prometheus.AlreadyRegisteredError
		if stderrors.As(err, &alreadyRegErr) {
			// Prometheus already has this metric - treat as success for idempotency
			r.registeredMetrics[key] = histogram
			return nil
		}
		return errs.WrapFatal(err, "MetricsRegistry", "RegisterHistogram",
			"failed to register histogram with prometheus")
	}

	r.registeredMetrics[key] = histogram
	return nil
}

// RegisterCounterVec registers a counter vector metric for a service.
// Idempotent: returns success if metric already registered with same key.
func (r *MetricsRegistry) RegisterCounterVec(serviceName, metricName string, counterVec *prometheus.CounterVec) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	key := fmt.Sprintf("%s.%s", serviceName, metricName)

	// Idempotent: if already registered with same key, return success
	if _, exists := r.registeredMetrics[key]; exists {
		return nil
	}

	if err := r.prometheusRegistry.Register(counterVec); err != nil {
		// Check if it's a duplicate registration error from Prometheus
		var alreadyRegErr prometheus.AlreadyRegisteredError
		if stderrors.As(err, &alreadyRegErr) {
			// Prometheus already has this metric - treat as success for idempotency
			r.registeredMetrics[key] = counterVec
			return nil
		}
		return errs.WrapFatal(err, "MetricsRegistry", "RegisterCounterVec",
			"failed to register counter vector with prometheus")
	}

	r.registeredMetrics[key] = counterVec
	return nil
}

// RegisterGaugeVec registers a gauge vector metric for a service.
// Idempotent: returns success if metric already registered with same key.
func (r *MetricsRegistry) RegisterGaugeVec(serviceName, metricName string, gaugeVec *prometheus.GaugeVec) error {
	_, err := r.RegisterOrGetGaugeVec(serviceName, metricName, gaugeVec)
	return err
}

// RegisterHistogramVec registers a histogram vector metric for a service.
// Idempotent: returns success if metric already registered with same key.
func (r *MetricsRegistry) RegisterHistogramVec(
	serviceName, metricName string, histogramVec *prometheus.HistogramVec) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	key := fmt.Sprintf("%s.%s", serviceName, metricName)

	// Idempotent: if already registered with same key, return success
	if _, exists := r.registeredMetrics[key]; exists {
		return nil
	}

	if err := r.prometheusRegistry.Register(histogramVec); err != nil {
		// Check if it's a duplicate registration error from Prometheus
		var alreadyRegErr prometheus.AlreadyRegisteredError
		if stderrors.As(err, &alreadyRegErr) {
			// Prometheus already has this metric - treat as success for idempotency
			r.registeredMetrics[key] = histogramVec
			return nil
		}
		return errs.WrapFatal(err, "MetricsRegistry", "RegisterHistogramVec",
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
		r.Metrics.LogEntriesTotal,
		r.Metrics.NATSConnected,
		r.Metrics.NATSRTT,
		r.Metrics.NATSReconnects,
		r.Metrics.NATSCircuitBreaker,
	)
}
