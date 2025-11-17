package metric

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewMetricsRegistry(t *testing.T) {
	registry := NewMetricsRegistry()

	assert.NotNil(t, registry)
	assert.NotNil(t, registry.PrometheusRegistry())
}

func TestMetricsRegistry_RegisterCounter(t *testing.T) {
	registry := NewMetricsRegistry()

	counter := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "test_counter",
		Help: "A test counter",
	})

	err := registry.RegisterCounter("test-service", "test_counter", counter)
	require.NoError(t, err)

	// Should be able to increment the counter
	counter.Inc()

	// Verify the counter is registered in the underlying Prometheus registry
	metricFamilies, err := registry.PrometheusRegistry().Gather()
	require.NoError(t, err)

	found := false
	for _, mf := range metricFamilies {
		if mf.GetName() == "test_counter" {
			found = true
			break
		}
	}
	assert.True(t, found, "Counter should be registered in Prometheus registry")
}

func TestMetricsRegistry_RegisterGauge(t *testing.T) {
	registry := NewMetricsRegistry()

	gauge := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "test_gauge",
		Help: "A test gauge",
	})

	err := registry.RegisterGauge("test-service", "test_gauge", gauge)
	require.NoError(t, err)

	// Should be able to set the gauge
	gauge.Set(42.0)

	// Verify the gauge is registered
	metricFamilies, err := registry.PrometheusRegistry().Gather()
	require.NoError(t, err)

	found := false
	for _, mf := range metricFamilies {
		if mf.GetName() == "test_gauge" {
			found = true
			break
		}
	}
	assert.True(t, found, "Gauge should be registered in Prometheus registry")
}

func TestMetricsRegistry_RegisterHistogram(t *testing.T) {
	registry := NewMetricsRegistry()

	histogram := prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "test_histogram",
		Help:    "A test histogram",
		Buckets: prometheus.DefBuckets,
	})

	err := registry.RegisterHistogram("test-service", "test_histogram", histogram)
	require.NoError(t, err)

	// Should be able to observe values
	histogram.Observe(1.5)

	// Verify the histogram is registered
	metricFamilies, err := registry.PrometheusRegistry().Gather()
	require.NoError(t, err)

	found := false
	for _, mf := range metricFamilies {
		if mf.GetName() == "test_histogram" {
			found = true
			break
		}
	}
	assert.True(t, found, "Histogram should be registered in Prometheus registry")
}

func TestMetricsRegistry_PreventDuplicateRegistration(t *testing.T) {
	registry := NewMetricsRegistry()

	counter1 := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "duplicate_counter",
		Help: "First counter",
	})

	counter2 := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "duplicate_counter",
		Help: "First counter", // Same help to avoid Prometheus validation error
	})

	// First registration should succeed
	err := registry.RegisterCounter("service1", "duplicate_counter", counter1)
	require.NoError(t, err)

	// Second registration with same name should fail with our custom tracking
	err = registry.RegisterCounter("service2", "duplicate_counter", counter2)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "prometheus conflict")
}

func TestMetricsRegistry_UnregisterMetric(t *testing.T) {
	registry := NewMetricsRegistry()

	counter := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "unregister_counter",
		Help: "A counter to unregister",
	})

	// Register the counter
	err := registry.RegisterCounter("test-service", "unregister_counter", counter)
	require.NoError(t, err)

	// Verify it's registered
	metricFamilies, err := registry.PrometheusRegistry().Gather()
	require.NoError(t, err)

	found := false
	for _, mf := range metricFamilies {
		if mf.GetName() == "unregister_counter" {
			found = true
			break
		}
	}
	assert.True(t, found)

	// Unregister the counter
	success := registry.Unregister("test-service", "unregister_counter")
	assert.True(t, success)

	// Verify it's no longer registered
	metricFamilies, err = registry.PrometheusRegistry().Gather()
	require.NoError(t, err)

	found = false
	for _, mf := range metricFamilies {
		if mf.GetName() == "unregister_counter" {
			found = true
			break
		}
	}
	assert.False(t, found)
}

func TestMetricsRegistry_ThreadSafety(t *testing.T) {
	registry := NewMetricsRegistry()

	var wg sync.WaitGroup
	numGoroutines := 10

	// Register metrics concurrently
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			counter := prometheus.NewCounter(prometheus.CounterOpts{
				Name: fmt.Sprintf("concurrent_counter_%d", id),
				Help: "A concurrent counter",
			})

			err := registry.RegisterCounter("concurrent-service",
				fmt.Sprintf("concurrent_counter_%d", id), counter)
			assert.NoError(t, err)
		}(i)
	}

	wg.Wait()

	// Verify all metrics were registered
	metricFamilies, err := registry.PrometheusRegistry().Gather()
	require.NoError(t, err)

	counterCount := 0
	for _, mf := range metricFamilies {
		if contains(mf.GetName(), "concurrent_counter_") {
			counterCount++
		}
	}

	assert.Equal(t, numGoroutines, counterCount,
		"All concurrent counters should be registered")
}

func TestMetricsRegistrar_Interface(t *testing.T) {
	registry := NewMetricsRegistry()

	// Verify registry implements MetricsRegistrar interface
	var registrar MetricsRegistrar = registry
	assert.NotNil(t, registrar)

	// Test registering through the interface
	counter := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "interface_counter",
		Help: "Counter registered through interface",
	})

	err := registrar.RegisterCounter("interface-service", "interface_counter", counter)
	require.NoError(t, err)
}

func TestMetricsRegistry_CoreMetricsInitialization(t *testing.T) {
	registry := NewMetricsRegistry()

	// Vector metrics don't appear in Gather() until they have at least one value set
	// So we need to use the core metrics to set some values first
	coreMetrics := registry.CoreMetrics()

	// Set some values so the metrics show up in Gather()
	coreMetrics.RecordServiceStatus("test-service", 2)
	coreMetrics.RecordMessageReceived("test-service", "drifter")
	coreMetrics.RecordMessageProcessed("test-service", "drifter", "success")
	coreMetrics.RecordMessagePublished("test-service", "ocean.data")
	coreMetrics.RecordProcessingDuration("test-service", "read", 100*time.Millisecond)
	coreMetrics.RecordError("test-service", "connection")
	coreMetrics.RecordHealthStatus("test-service", true)

	// Verify core platform metrics are initialized
	metricFamilies, err := registry.PrometheusRegistry().Gather()
	require.NoError(t, err)

	expectedCoreMetrics := []string{
		"semstreams_service_status",
		"semstreams_messages_received_total",
		"semstreams_messages_processed_total",
		"semstreams_messages_published_total",
		"semstreams_processing_duration_seconds",
		"semstreams_errors_total",
		"semstreams_health_status",
		"semstreams_nats_connected",
		"semstreams_nats_rtt_milliseconds",
		"semstreams_nats_reconnects_total",
		"semstreams_nats_circuit_breaker",
	}

	foundMetrics := make(map[string]bool)
	for _, mf := range metricFamilies {
		foundMetrics[mf.GetName()] = true
	}

	for _, expectedMetric := range expectedCoreMetrics {
		assert.True(t, foundMetrics[expectedMetric],
			"core metric %s should be initialized", expectedMetric)
	}
}

func TestMetricsRegistry_NoCoreBusinessMetrics(t *testing.T) {
	registry := NewMetricsRegistry()

	metricFamilies, err := registry.PrometheusRegistry().Gather()
	require.NoError(t, err)

	// These business metrics should NOT be in core registry
	businessMetrics := []string{
		"semstreams_business_drifters_tracked",
		"semstreams_business_convergence_zones_total",
		"semstreams_business_files_processed_total",
		"semstreams_business_catalog_size",
	}

	foundMetrics := make(map[string]bool)
	for _, mf := range metricFamilies {
		foundMetrics[mf.GetName()] = true
	}

	for _, businessMetric := range businessMetrics {
		assert.False(t, foundMetrics[businessMetric],
			"Business metric %s should NOT be in core registry", businessMetric)
	}
}

func TestMetricsRegistry_GetCoreMetrics(t *testing.T) {
	registry := NewMetricsRegistry()

	coreMetrics := registry.CoreMetrics()
	assert.NotNil(t, coreMetrics)

	// Verify core metrics are accessible
	assert.NotNil(t, coreMetrics.ServiceStatus)
	assert.NotNil(t, coreMetrics.MessagesReceived)
	assert.NotNil(t, coreMetrics.MessagesProcessed)
	assert.NotNil(t, coreMetrics.MessagesPublished)
	assert.NotNil(t, coreMetrics.ProcessingDuration)
	assert.NotNil(t, coreMetrics.ErrorsTotal)
	assert.NotNil(t, coreMetrics.HealthCheckStatus)
	assert.NotNil(t, coreMetrics.NATSConnected)
	assert.NotNil(t, coreMetrics.NATSRTT)
	assert.NotNil(t, coreMetrics.NATSReconnects)
	assert.NotNil(t, coreMetrics.NATSCircuitBreaker)
}

func TestCoreMetrics_RecordMethods(t *testing.T) {
	registry := NewMetricsRegistry()
	coreMetrics := registry.CoreMetrics()

	// Test service status recording
	coreMetrics.RecordServiceStatus("test-service", 2)

	// Test message recording
	coreMetrics.RecordMessageReceived("test-service", "drifter")
	coreMetrics.RecordMessageProcessed("test-service", "drifter", "success")
	coreMetrics.RecordMessagePublished("test-service", "ocean.data")

	// Test processing duration
	coreMetrics.RecordProcessingDuration("test-service", "read", 100*time.Millisecond)

	// Test error recording
	coreMetrics.RecordError("test-service", "connection")

	// Test health status
	coreMetrics.RecordHealthStatus("test-service", true)

	// Test NATS metrics
	coreMetrics.RecordNATSStatus(true)
	coreMetrics.RecordNATSRTT(50 * time.Millisecond)
	coreMetrics.RecordNATSReconnect()
	coreMetrics.RecordCircuitBreakerState(0)

	// Verify metrics have values > 0
	metricFamilies, err := registry.PrometheusRegistry().Gather()
	require.NoError(t, err)

	// Check that we have metrics data
	assert.Greater(t, len(metricFamilies), 0, "Should have recorded metrics")
}

// Helper function to check if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && s[:len(substr)] == substr
}
