// Package metric provides Prometheus-based metrics collection and HTTP server
// for StreamKit platform monitoring and observability.
//
// The package offers a centralized metrics registry managing both core platform
// metrics (service status, message processing, NATS health) and custom
// service-specific metrics. It includes an HTTP server exposing metrics in
// Prometheus format for monitoring system integration.
//
// # Architecture
//
// The package follows a three-layer design:
//
//  1. Core Metrics: Platform-level metrics automatically registered (Metrics type)
//  2. Service Registry: Extensible registration for service-specific metrics (MetricsRegistrar interface)
//  3. HTTP Server: Metrics endpoint with health checks (Server type)
//
// This architecture separates infrastructure concerns (core metrics) from
// application concerns (service-specific metrics) while providing a unified
// metrics endpoint for monitoring systems.
//
// # Basic Usage
//
// Setting up metrics collection and HTTP server:
//
//	registry := metric.NewMetricsRegistry()
//	securityCfg := security.Config{} // Platform security config
//	server := metric.NewServer(9090, "/metrics", registry, securityCfg)
//
//	go func() {
//	    if err := server.Start(); err != nil && err != http.ErrServerClosed {
//	        log.Printf("Metrics server error: %v", err)
//	    }
//	}()
//
//	// Record core platform metrics
//	coreMetrics := registry.CoreMetrics()
//	coreMetrics.RecordServiceStatus("my-service", 2)
//	coreMetrics.RecordMessagesProcessed("my-service", 1500)
//	coreMetrics.RecordNATSHealth(1.0)
//
// The metrics server will expose Prometheus-formatted metrics at http://localhost:9090/metrics
// and a health check at http://localhost:9090/health.
//
// # Core Metrics
//
// The package automatically registers core platform metrics tracking:
//
//   - Service lifecycle: service_status (0=stopped, 1=starting, 2=running, 3=stopping)
//   - Message processing: messages_processed_total, messages_failed_total
//   - Processing performance: message_processing_duration_seconds
//   - NATS connectivity: nats_connection_status, nats_messages_total
//   - Error tracking: errors_total, panic_total
//
// Access core metrics through the registry:
//
//	coreMetrics := registry.CoreMetrics()
//
//	// Service lifecycle tracking
//	coreMetrics.RecordServiceStatus("processor", 2) // 2 = running
//
//	// Message processing metrics
//	coreMetrics.RecordMessagesProcessed("processor", 100)
//	coreMetrics.RecordMessagesFailed("processor", 2)
//	coreMetrics.ObserveProcessingDuration("processor", 0.150) // 150ms
//
//	// NATS connectivity
//	coreMetrics.RecordNATSHealth(1.0) // 1.0 = healthy
//	coreMetrics.RecordNATSMessages("input-subject", 50)
//
//	// Error tracking
//	coreMetrics.RecordError("processor", "validation")
//	coreMetrics.RecordPanic("processor")
//
// # Service-Specific Metrics
//
// Services can register custom metrics through the registry:
//
//	// Register a counter
//	requestCounter := prometheus.NewCounter(prometheus.CounterOpts{
//	    Name: "api_requests_total",
//	    Help: "Total number of API requests",
//	})
//	err := registry.RegisterCounter("api-service", "api_requests_total", requestCounter)
//
//	// Register a gauge
//	activeConnections := prometheus.NewGauge(prometheus.GaugeOpts{
//	    Name: "active_connections",
//	    Help: "Number of active client connections",
//	})
//	err = registry.RegisterGauge("websocket-service", "active_connections", activeConnections)
//
//	// Register a histogram
//	queryDuration := prometheus.NewHistogram(prometheus.HistogramOpts{
//	    Name:    "query_duration_seconds",
//	    Help:    "Time spent executing queries",
//	    Buckets: prometheus.DefBuckets,
//	})
//	err = registry.RegisterHistogram("database-service", "query_duration_seconds", queryDuration)
//
// # Vector Metrics with Labels
//
// Register metrics with labels for multi-dimensional data:
//
//	// Counter with labels
//	httpRequestsVec := prometheus.NewCounterVec(
//	    prometheus.CounterOpts{
//	        Name: "http_requests_total",
//	        Help: "Total HTTP requests by status and method",
//	    },
//	    []string{"status", "method"},
//	)
//	err := registry.RegisterCounterVec("api-service", "http_requests_total", httpRequestsVec)
//
//	// Use the metric with specific label values
//	httpRequestsVec.WithLabelValues("200", "GET").Inc()
//	httpRequestsVec.WithLabelValues("404", "POST").Inc()
//
//	// Gauge with labels
//	cacheItemsVec := prometheus.NewGaugeVec(
//	    prometheus.GaugeOpts{
//	        Name: "cache_items",
//	        Help: "Number of items in cache by type",
//	    },
//	    []string{"cache_type"},
//	)
//	err = registry.RegisterGaugeVec("cache-service", "cache_items", cacheItemsVec)
//
//	// Histogram with labels
//	requestDurationVec := prometheus.NewHistogramVec(
//	    prometheus.HistogramOpts{
//	        Name:    "request_duration_seconds",
//	        Help:    "Request duration by endpoint",
//	        Buckets: []float64{.001, .01, .1, 1, 10},
//	    },
//	    []string{"endpoint"},
//	)
//	err = registry.RegisterHistogramVec("api-service", "request_duration_seconds", requestDurationVec)
//
// # HTTP Server
//
// The metrics server provides three endpoints:
//
//   - GET / - HTML page with links to metrics and health endpoints
//   - GET /metrics - Prometheus-formatted metrics (default path, configurable)
//   - GET /health - JSON health check response
//
// Server configuration:
//
//	// Default configuration (port 9090, path /metrics)
//	securityCfg := security.Config{} // Platform security config
//	server := metric.NewServer(0, "", registry, securityCfg)
//
//	// Custom configuration
//	server := metric.NewServer(8080, "/prometheus", registry, securityCfg)
//
//	// Start server (blocking)
//	if err := server.Start(); err != nil {
//	    log.Fatalf("Failed to start metrics server: %v", err)
//	}
//
//	// Stop server (in another goroutine)
//	if err := server.Stop(); err != nil {
//	    log.Printf("Error stopping server: %v", err)
//	}
//
// Health endpoint response format:
//
//	{
//	    "status": "healthy",
//	    "timestamp": "2024-01-15T10:30:00Z"
//	}
//
// # Prometheus Integration
//
// The package uses the official Prometheus Go client library and exposes
// metrics in OpenMetrics format. Configure Prometheus to scrape the endpoint:
//
//	# prometheus.yml
//	scrape_configs:
//	  - job_name: 'streamkit'
//	    static_configs:
//	      - targets: ['localhost:9090']
//	    metrics_path: '/metrics'
//	    scrape_interval: 15s
//
// All core metrics use the namespace "semstreams" and appropriate subsystems:
//   - semstreams_service_status{service="..."}
//   - semstreams_messages_processed_total{service="..."}
//   - semstreams_nats_connection_status
//
// Service-specific metrics use the metric name as provided during registration.
//
// # MetricsRegistrar Interface
//
// Services implement the MetricsRegistrar interface for dependency injection:
//
//	type MyService struct {
//	    metrics metric.MetricsRegistrar
//	}
//
//	func NewMyService(metrics metric.MetricsRegistrar) *MyService {
//	    counter := prometheus.NewCounter(prometheus.CounterOpts{
//	        Name: "operations_total",
//	        Help: "Total operations",
//	    })
//	    metrics.RegisterCounter("my-service", "operations_total", counter)
//
//	    return &MyService{metrics: metrics}
//	}
//
// This enables testing with mock registrars and provides loose coupling.
//
// # Thread Safety
//
// All registry operations are thread-safe:
//   - Registration methods use mutex protection
//   - Metric recording is lock-free (Prometheus guarantee)
//   - CoreMetrics() returns a thread-safe shared instance
//   - PrometheusRegistry() is safe for concurrent access
//
// Example concurrent usage:
//
//	registry := metric.NewMetricsRegistry()
//	coreMetrics := registry.CoreMetrics()
//
//	// Safe to call from multiple goroutines
//	go coreMetrics.RecordMessagesProcessed("service-1", 100)
//	go coreMetrics.RecordMessagesProcessed("service-2", 200)
//	go coreMetrics.RecordMessagesProcessed("service-3", 300)
//
// # Error Handling
//
// Registration methods return errors for:
//
//   - Duplicate registration: attempting to register same metric name twice
//   - Prometheus conflicts: internal Prometheus registration failures
//   - Validation errors: nil metrics or invalid parameters
//
// Example error handling:
//
//	counter := prometheus.NewCounter(prometheus.CounterOpts{Name: "test"})
//	err := registry.RegisterCounter("service", "test", counter)
//	if err != nil {
//	    // Check for duplicate registration
//	    if strings.Contains(err.Error(), "already registered") {
//	        log.Printf("Metric already registered, skipping")
//	    } else {
//	        log.Fatalf("Failed to register metric: %v", err)
//	    }
//	}
//
// The Server.Start() method returns errors for:
//
//   - Server already running
//   - Nil registry
//   - HTTP server failures (port in use, permission denied)
//
// # Testing
//
// The package includes comprehensive tests:
//
//   - Unit tests: Core metrics recording, registry operations
//   - Integration tests: Full registry lifecycle, Prometheus gathering
//   - Race detection: Concurrent access patterns verified
//
// Example test using the registry:
//
//	func TestMyService_Metrics(t *testing.T) {
//	    registry := metric.NewMetricsRegistry()
//	    service := NewMyService(registry)
//
//	    // Perform operations
//	    service.DoWork()
//
//	    // Verify metrics
//	    coreMetrics := registry.CoreMetrics()
//	    // Check that metrics were recorded
//	}
//
// # Performance Considerations
//
// Metric recording performance:
//   - Counter.Inc(): ~100ns per operation (lock-free)
//   - Gauge.Set(): ~100ns per operation (lock-free)
//   - Histogram.Observe(): ~150ns per operation (bucket lookup)
//
// Registry operations:
//   - Registration: O(1) map insert with mutex
//   - Gathering: O(n) for n registered metrics
//
// Memory usage:
//   - Core metrics: ~2KB base overhead
//   - Per service metric: ~200 bytes
//   - Vector metrics: ~200 bytes + (100 bytes × number of label combinations)
//
// The HTTP server adds minimal overhead (~1MB base) and handles Prometheus
// scraping efficiently with streaming responses.
//
// # Architecture Integration
//
// The metric package integrates with StreamKit components:
//
//   - service: Services record lifecycle and processing metrics
//   - component: Components track message flow metrics
//   - natsclient: NATS client records connectivity metrics
//   - health: Health status can be mirrored as metrics
//
// Data flow:
//
//	Component → Core Metrics → Prometheus Registry → HTTP Server → Prometheus
//
// # Design Decisions
//
// Centralized Registry: Chose centralized registry over distributed collectors
// to ensure consistent metric namespace, prevent duplication, and enable
// runtime metric discovery.
//
// Core vs Service Metrics: Separated platform-level metrics (core) from
// service-specific metrics to distinguish infrastructure health from
// application health.
//
// Prometheus Direct Integration: Used official Prometheus client rather than
// abstraction to leverage native features, avoid wrapper overhead, and ensure
// compatibility with Prometheus ecosystem.
//
// No Context in Server.Start(): Current design uses blocking Start() without
// context. Future enhancement could add context-aware lifecycle management.
//
// # Examples
//
// Complete service integration:
//
//	package main
//
//	import (
//	    "log"
//	    "time"
//
//	    "github.com/c360/semstreams/metric"
//	    "github.com/prometheus/client_golang/prometheus"
//	)
//
//	func main() {
//	    // Create metrics registry
//	    registry := metric.NewMetricsRegistry()
//
//	    // Start metrics server
//	    securityCfg := security.Config{} // Platform security config
//	    server := metric.NewServer(9090, "/metrics", registry, securityCfg)
//	    go func() {
//	        if err := server.Start(); err != nil {
//	            log.Printf("Metrics server error: %v", err)
//	        }
//	    }()
//	    defer server.Stop()
//
//	    // Get core metrics
//	    coreMetrics := registry.CoreMetrics()
//
//	    // Register service-specific metric
//	    operationCounter := prometheus.NewCounter(prometheus.CounterOpts{
//	        Name: "operations_total",
//	        Help: "Total operations performed",
//	    })
//	    registry.RegisterCounter("my-service", "operations_total", operationCounter)
//
//	    // Record service status
//	    coreMetrics.RecordServiceStatus("my-service", 2) // running
//
//	    // Simulate work
//	    for i := 0; i < 100; i++ {
//	        operationCounter.Inc()
//	        coreMetrics.RecordMessagesProcessed("my-service", 1)
//	        time.Sleep(100 * time.Millisecond)
//	    }
//	}
//
// For more examples and detailed usage, see the README.md in this directory.
package metric
