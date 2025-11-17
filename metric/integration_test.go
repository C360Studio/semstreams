package metric

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MockService simulates a service that can register its own metrics
type MockService struct {
	name    string
	metrics struct {
		dataProcessed prometheus.Counter
		queueDepth    prometheus.Gauge
	}
}

func NewMockService(name string) *MockService {
	return &MockService{name: name}
}

func (m *MockService) Name() string {
	return m.name
}

// RegisterMetrics registers domain-specific metrics for the mock service
func (m *MockService) RegisterMetrics(registrar MetricsRegistrar) error {
	// Register a custom counter
	m.metrics.dataProcessed = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "semstreams",
		Subsystem: "mock_service",
		Name:      "data_processed_total",
		Help:      "Total number of data items processed",
	})

	err := registrar.RegisterCounter(m.name, "data_processed_total", m.metrics.dataProcessed)
	if err != nil {
		return err
	}

	// Register a custom gauge
	m.metrics.queueDepth = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "semstreams",
		Subsystem: "mock_service",
		Name:      "queue_depth",
		Help:      "Current depth of processing queue",
	})

	return registrar.RegisterGauge(m.name, "queue_depth", m.metrics.queueDepth)
}

// ProcessData simulates data processing and updates metrics
func (m *MockService) ProcessData(items int, queueDepth int) {
	m.metrics.dataProcessed.Add(float64(items))
	m.metrics.queueDepth.Set(float64(queueDepth))
}

func TestMetricsIntegration_ServiceRegistration(t *testing.T) {
	// Create a new metrics registry
	registry := NewMetricsRegistry()

	// Create mock service
	mockService := NewMockService("test-service")

	// Register the service's metrics
	err := mockService.RegisterMetrics(registry)
	require.NoError(t, err)

	// Simulate some service activity
	mockService.ProcessData(10, 5)

	// Verify metrics are registered and have values
	metricFamilies, err := registry.PrometheusRegistry().Gather()
	require.NoError(t, err)

	foundMetrics := make(map[string]bool)
	for _, mf := range metricFamilies {
		foundMetrics[mf.GetName()] = true
	}

	// Verify custom metrics are registered
	assert.True(t, foundMetrics["semstreams_mock_service_data_processed_total"],
		"Custom data_processed metric should be registered")
	assert.True(t, foundMetrics["semstreams_mock_service_queue_depth"],
		"Custom queue_depth metric should be registered")
}

func TestMetricsIntegration_NoDuplicateRegistration(t *testing.T) {
	registry := NewMetricsRegistry()

	// Create two services with the same name (this shouldn't happen in real usage)
	service1 := NewMockService("duplicate-service")
	service2 := NewMockService("duplicate-service")

	// Register first service's metrics
	err := service1.RegisterMetrics(registry)
	require.NoError(t, err)

	// Try to register second service's metrics - should fail
	err = service2.RegisterMetrics(registry)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already registered")
}

func TestMetricsIntegration_CoreAndServiceMetricsSeparate(t *testing.T) {
	registry := NewMetricsRegistry()
	coreMetrics := registry.CoreMetrics()

	mockService := NewMockService("separation-test")
	err := mockService.RegisterMetrics(registry)
	require.NoError(t, err)

	// Use core metrics
	coreMetrics.RecordServiceStatus("separation-test", 2)
	coreMetrics.RecordMessageReceived("separation-test", "test-message")

	// Use service-specific metrics
	mockService.ProcessData(5, 3)

	// Verify both types of metrics are present
	metricFamilies, err := registry.PrometheusRegistry().Gather()
	require.NoError(t, err)

	foundMetrics := make(map[string]bool)
	for _, mf := range metricFamilies {
		foundMetrics[mf.GetName()] = true
	}

	// Verify core metrics
	assert.True(t, foundMetrics["semstreams_service_status"],
		"core service status metric should be present")
	assert.True(t, foundMetrics["semstreams_messages_received_total"],
		"core messages received metric should be present")

	// Verify service-specific metrics
	assert.True(t, foundMetrics["semstreams_mock_service_data_processed_total"],
		"Service-specific data processed metric should be present")
	assert.True(t, foundMetrics["semstreams_mock_service_queue_depth"],
		"Service-specific queue depth metric should be present")

	// Verify business metrics are NOT present (they should be registered by specific services only)
	assert.False(t, foundMetrics["semstreams_business_drifters_tracked"],
		"Business drifters metric should NOT be in core registry")
	assert.False(t, foundMetrics["semstreams_business_convergence_zones_total"],
		"Business convergence zones metric should NOT be in core registry")
}

func TestMetricsIntegration_MetricsUnregistration(t *testing.T) {
	registry := NewMetricsRegistry()

	mockService := NewMockService("unregister-test")

	// Register metrics
	err := mockService.RegisterMetrics(registry)
	require.NoError(t, err)

	// Process some data to make metrics visible
	mockService.ProcessData(1, 1)

	// Verify metrics are present
	metricFamilies, err := registry.PrometheusRegistry().Gather()
	require.NoError(t, err)

	foundBefore := make(map[string]bool)
	for _, mf := range metricFamilies {
		foundBefore[mf.GetName()] = true
	}

	assert.True(t, foundBefore["semstreams_mock_service_data_processed_total"],
		"Metric should be present before unregistration")

	// Unregister one of the metrics
	success := registry.Unregister("unregister-test", "data_processed_total")
	assert.True(t, success, "Unregistration should succeed")

	// Verify metric is no longer present
	metricFamilies, err = registry.PrometheusRegistry().Gather()
	require.NoError(t, err)

	foundAfter := make(map[string]bool)
	for _, mf := range metricFamilies {
		foundAfter[mf.GetName()] = true
	}

	assert.False(t, foundAfter["semstreams_mock_service_data_processed_total"],
		"Metric should be absent after unregistration")
	assert.True(t, foundAfter["semstreams_mock_service_queue_depth"],
		"Other service metrics should remain")
}

func TestMetricsIntegration_MultipleServicesWithUniqueMetrics(t *testing.T) {
	registry := NewMetricsRegistry()

	// Create multiple services - they need different metric names to coexist
	service1 := NewMockService("ocean-analyzer")
	service2 := NewMockService("data-processor")

	// Register first service
	err := service1.RegisterMetrics(registry)
	require.NoError(t, err)

	// The second service will fail because it tries to register the same Prometheus metric names
	// This demonstrates that our registry correctly prevents Prometheus-level conflicts
	err = service2.RegisterMetrics(registry)
	assert.Error(t, err, "Second service should fail due to Prometheus metric name conflict")
	assert.Contains(t, err.Error(), "prometheus conflict")
}

func TestMetricsIntegration_MultipleServicesSameNames(t *testing.T) {
	registry := NewMetricsRegistry()

	// Create services with identical names - this simulates trying to register
	// the same service twice, which should be prevented
	service1 := NewMockService("identical-service")
	service2 := NewMockService("identical-service")

	// Register first service
	err := service1.RegisterMetrics(registry)
	require.NoError(t, err)

	// Second service with same name should fail at our registry level
	err = service2.RegisterMetrics(registry)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already registered")
}
