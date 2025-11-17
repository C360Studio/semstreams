// Package health provides health monitoring functionality for StreamKit components and systems
// with thread-safe status tracking and aggregation.
//
// The health package enables tracking the health status of individual components and aggregating
// system-wide health information for monitoring, alerting, and operational visibility.
//
// # Health States
//
// The package supports three health states:
//   - Healthy: component operating normally
//   - Degraded: component operating with reduced functionality
//   - Unhealthy: component not functioning properly
//
// This three-state model enables nuanced health reporting and appropriate operational responses.
// For example, a degraded database might trigger capacity scaling, while an unhealthy database
// triggers immediate incident response.
//
// # Core Components
//
// Status: Individual component health state containing status level, descriptive message,
// timestamp, optional metrics, and hierarchical sub-statuses for complex systems.
//
// Monitor: Thread-safe centralized tracking system for multiple component health statuses
// with concurrent read/write access and automatic timestamp management.
//
// Helpers: Convenience functions for creating status objects and aggregating system health.
//
// # Basic Usage
//
// Creating and tracking component health:
//
//	monitor := health.NewMonitor()
//
//	// Update component health
//	monitor.UpdateHealthy("database", "Database connection stable")
//	monitor.UpdateDegraded("cache", "Cache hit rate below threshold")
//	monitor.UpdateUnhealthy("external-api", "Connection timeout after 5 attempts")
//
//	// Check individual component health
//	if status, exists := monitor.Get("database"); exists {
//	    if status.IsHealthy() {
//	        log.Println("Database is healthy")
//	    }
//	}
//
//	// Get all component statuses
//	allStatuses := monitor.GetAll()
//	for name, status := range allStatuses {
//	    log.Printf("%s: %s - %s", name, status.Status, status.Message)
//	}
//
// # System-Wide Health Aggregation
//
// Combining multiple component health statuses into system-wide indicators:
//
//	// Aggregate all monitored components
//	systemHealth := monitor.AggregateHealth("platform")
//	if systemHealth.IsUnhealthy() {
//	    log.Printf("System unhealthy: %s", systemHealth.Message)
//	    // Trigger alerts, failover, etc.
//	}
//
//	// Aggregation uses hierarchical rules:
//	// - Any unhealthy component → system unhealthy
//	// - Any degraded component (with no unhealthy) → system degraded
//	// - All healthy → system healthy
//
// # Hierarchical Status
//
// Building nested health status for complex systems:
//
//	// Create database cluster health with sub-components
//	primaryStatus := health.NewHealthy("db-primary", "Primary node operational")
//	replicaStatus := health.NewDegraded("db-replica", "Replica lagging by 5s")
//
//	clusterHealth := health.NewHealthy("database-cluster", "Cluster operational").
//	    WithSubStatus(primaryStatus).
//	    WithSubStatus(replicaStatus)
//
//	// Aggregate automatically considers sub-statuses
//	overallHealth := health.Aggregate([]health.Status{clusterHealth})
//
// # Health Metrics
//
// Attaching performance and operational metrics to health status:
//
//	metrics := map[string]any{
//	    "uptime_seconds":     3600,
//	    "error_count":        0,
//	    "messages_processed": 1500,
//	    "last_activity":      time.Now(),
//	    "cpu_percent":        45.2,
//	    "memory_mb":          512,
//	}
//
//	status := health.NewHealthy("processor", "Processing normally").
//	    WithMetrics(metrics)
//
//	// Access metrics
//	if uptime, ok := status.Metrics["uptime_seconds"].(int); ok {
//	    log.Printf("Uptime: %d seconds", uptime)
//	}
//
// # Integration with Components
//
// Converting component.HealthStatus to health.Status:
//
//	// Assuming you have a component that implements component.HealthChecker
//	componentHealth := component.GetHealth() // Returns component.HealthStatus
//
//	// Convert to health.Status with automatic error sanitization
//	healthStatus := health.FromComponentHealth("my-component", componentHealth)
//
//	// Error messages are automatically sanitized to remove:
//	// - URLs (http://, nats://, ws://)
//	// - File paths (Unix and Windows)
//	// - IP addresses and ports
//	// - Credentials (password, token, key, secret)
//
// # Thread Safety
//
// All Monitor operations are thread-safe and can be safely called from multiple goroutines:
//
//	monitor := health.NewMonitor()
//
//	// Safe to call concurrently from multiple goroutines
//	go monitor.UpdateHealthy("service-1", "Running")
//	go monitor.UpdateHealthy("service-2", "Running")
//	go monitor.UpdateHealthy("service-3", "Running")
//
//	// Read operations can happen concurrently with writes
//	go func() {
//	    for {
//	        systemHealth := monitor.AggregateHealth("system")
//	        log.Printf("System health: %s", systemHealth.Status)
//	        time.Sleep(5 * time.Second)
//	    }
//	}()
//
// The Monitor uses an RWMutex internally to allow concurrent reads while protecting writes.
// Status objects are immutable - methods like WithMetrics and WithSubStatus return new copies
// rather than modifying the original.
//
// # Security
//
// Error messages passed through FromComponentHealth are automatically sanitized to remove
// potentially sensitive information:
//
//	// Original error with sensitive data
//	err := "failed to connect to https://api.example.com/v1 with password=secret123"
//
//	// After sanitization via FromComponentHealth
//	// "failed to connect to [URL] with [REDACTED]"
//
// Sanitization patterns:
//   - URLs: http://, https://, nats://, ws://, wss:// → [URL]
//   - File paths: /path/to/file, C:\path\to\file → [PATH]
//   - IP addresses: 192.168.1.100 → [IP]
//   - Ports: :8080 → :[PORT]
//   - Credentials: password=X, token=X, key=X, secret=X → [REDACTED]
//
// This prevents accidental exposure of sensitive data in health dashboards and logs.
//
// # Error Handling Philosophy
//
// The health package does not return errors because it represents the *result* of error
// handling, not part of error propagation. Health status is an observability output.
//
// Components creating Status objects should use the semstreams/errors package for any
// error wrapping before converting to health status messages. The health package then
// sanitizes these error messages for safe display.
//
// # Testing
//
// The package provides comprehensive test coverage (100%) including:
//   - Unit tests for all helper functions and status methods
//   - Concurrency tests for thread-safe Monitor operations
//   - Security tests for error message sanitization
//   - Isolation tests for immutability guarantees
//
// Example test usage:
//
//	func TestMyService_Health(t *testing.T) {
//	    service := NewMyService()
//
//	    status := service.Health()
//
//	    assert.True(t, status.IsHealthy())
//	    assert.Equal(t, "my-service", status.Component)
//	    assert.NotZero(t, status.Timestamp)
//	}
//
// # Performance Considerations
//
// Monitor operations:
//   - Get/Update: O(1) map operations
//   - GetAll: O(n) with defensive copy to prevent external mutation
//   - Aggregate: O(n) for n components, plus recursive traversal of sub-statuses
//
// Memory:
//   - Status objects are small value types (typically <1KB)
//   - Monitor holds one Status per component name
//   - Sub-statuses create nested tree structures
//
// Concurrency:
//   - RWMutex allows unlimited concurrent reads
//   - Writes are serialized but typically infrequent
//   - No lock contention expected for normal usage patterns
//
// # Architecture Integration
//
// The health package integrates with StreamKit components:
//   - service: Services implement Health() returning health.Status
//   - component: Components expose HealthStatus converted via FromComponentHealth
//   - HTTP endpoints: Monitor provides GetAll() for health check endpoints
//   - Metrics systems: Status.Metrics attach operational data
//
// Data flow:
//
//	Component → component.HealthStatus → health.FromComponentHealth → health.Status → Monitor → HTTP /health
//
// # Design Decisions
//
// Three-State Model: Chose healthy/degraded/unhealthy over binary healthy/unhealthy to
// enable nuanced operational responses. Degraded state allows systems to continue operating
// with reduced capacity while triggering scaling rather than immediate failover.
//
// Automatic Sanitization: Error messages are sanitized by default (no opt-out) to prevent
// accidental credential exposure. This "secure by default" design prevents common security
// mistakes even if it occasionally over-redacts during debugging.
//
// Value-Based Status: Status is a struct, not *Status, making it immutable and preventing
// accidental mutation. Methods like WithMetrics return new copies, following functional
// programming patterns for safety.
//
// Conservative Aggregation: System health follows "worst case" rules - a single unhealthy
// component marks the entire system unhealthy. This conservative approach ensures problems
// are not masked by healthy components.
//
// # Examples
//
// Service health monitoring:
//
//	type MyService struct {
//	    monitor *health.Monitor
//	}
//
//	func (s *MyService) Start() error {
//	    s.monitor = health.NewMonitor()
//
//	    // Monitor database health
//	    go func() {
//	        ticker := time.NewTicker(10 * time.Second)
//	        defer ticker.Stop()
//
//	        for range ticker.C {
//	            if err := s.db.Ping(); err != nil {
//	                s.monitor.UpdateUnhealthy("database",
//	                    fmt.Sprintf("Database ping failed: %v", err))
//	            } else {
//	                s.monitor.UpdateHealthy("database", "Database responding")
//	            }
//	        }
//	    }()
//
//	    return nil
//	}
//
//	func (s *MyService) Health() health.Status {
//	    return s.monitor.AggregateHealth("my-service")
//	}
//
// HTTP health endpoint:
//
//	func healthHandler(monitor *health.Monitor) http.HandlerFunc {
//	    return func(w http.ResponseWriter, r *http.Request) {
//	        systemHealth := monitor.AggregateHealth("platform")
//
//	        statusCode := http.StatusOK
//	        if systemHealth.IsUnhealthy() {
//	            statusCode = http.StatusServiceUnavailable
//	        } else if systemHealth.IsDegraded() {
//	            statusCode = http.StatusOK // Still serving traffic
//	        }
//
//	        w.Header().Set("Content-Type", "application/json")
//	        w.WriteHeader(statusCode)
//	        json.NewEncoder(w).Encode(systemHealth)
//	    }
//	}
//
// For more examples and detailed usage, see the README.md in this directory.
package health
