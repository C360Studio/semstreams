# Health

Health monitoring functionality for SemStreams components and systems with thread-safe status tracking and aggregation.

## Overview

The health package provides comprehensive health monitoring capabilities for distributed systems and component-based architectures. It enables tracking the health status of individual components and aggregating system-wide health information for monitoring, alerting, and operational visibility.

The package supports three health states: Healthy (component operating normally), Degraded (component operating with reduced functionality), and Unhealthy (component not functioning properly). This classification enables nuanced health reporting and appropriate response strategies.

The package includes a thread-safe Monitor for tracking multiple component health statuses, helper functions for creating status objects, and aggregation logic for rolling up component health into system-wide health indicators.

## Installation

```go
import "github.com/c360/semstreams/health"
```

## Core Concepts

### Health Status
Individual component health state containing status level, descriptive message, timestamp, optional metrics, and hierarchical sub-statuses for complex systems with nested components.

### Health Monitor
Thread-safe centralized tracking system for multiple component health statuses with concurrent read/write access, automatic timestamp management, and convenient update methods.

### Health Aggregation
Logic for combining multiple component health statuses into system-wide health indicators using hierarchical rules where any unhealthy component makes the system unhealthy.

### Health Metrics
Performance and operational metrics attached to health status including uptime, error counts, message processing statistics, and last activity timestamps.

## Usage

### Basic Example

```go
import "github.com/c360/semstreams/health"

// Create a health monitor
monitor := health.NewMonitor()

// Update component health status
monitor.UpdateHealthy("database", "Database connection stable")
monitor.UpdateDegraded("cache", "Cache hit rate below threshold")
monitor.UpdateUnhealthy("external-api", "Connection timeout after 5 attempts")

// Check individual component health
if status, exists := monitor.Get("database"); exists {
    if status.IsHealthy() {
        log.Printf("Database is healthy: %s", status.Message)
    }
}

// Get system-wide health status
systemHealth := monitor.AggregateHealth("semstreams-platform")
log.Printf("System health: %s - %s", systemHealth.Status, systemHealth.Message)

// List all monitored components
components := monitor.ListComponents()
log.Printf("Monitoring %d components: %v", len(components), components)
```

### Advanced Usage - Custom Status with Metrics

```go
// Create status with detailed metrics
metrics := &health.Metrics{
    Uptime:            24 * time.Hour,
    ErrorCount:        3,
    MessagesProcessed: 15420,
    LastActivity:      time.Now().Add(-5 * time.Minute),
}

status := health.NewHealthy("message-processor", "Processing messages normally").
    WithMetrics(metrics)

monitor.Update("message-processor", status)

// Create hierarchical status with sub-components
webServerStatus := health.NewHealthy("web-server", "HTTP server operational")
apiStatus := health.NewDegraded("api-handler", "High response latency")
authStatus := health.NewHealthy("auth-service", "Authentication working")

systemStatus := health.Aggregate("web-system", []health.Status{
    webServerStatus,
    apiStatus,
    authStatus,
})

// System will be degraded due to API handler
log.Printf("Web system status: %s", systemStatus.Status) // "degraded"

// Access sub-statuses
for _, subStatus := range systemStatus.SubStatuses {
    log.Printf("  %s: %s - %s", subStatus.Component, subStatus.Status, subStatus.Message)
}
```

## API Reference

### Types

#### `Status`
Primary health status structure for components and systems.

```go
type Status struct {
    Component   string    `json:"component"`             // Component name
    Status      string    `json:"status"`                // "healthy", "unhealthy", "degraded"
    Message     string    `json:"message"`               // Descriptive message
    Timestamp   time.Time `json:"timestamp"`             // Status timestamp
    SubStatuses []Status  `json:"sub_statuses,omitempty"` // Hierarchical sub-components
    Metrics     *Metrics  `json:"metrics,omitempty"`      // Optional performance metrics
}

// Status check methods
func (s Status) IsHealthy() bool      // Returns true if status is "healthy"
func (s Status) IsDegraded() bool     // Returns true if status is "degraded"
func (s Status) IsUnhealthy() bool    // Returns true if status is "unhealthy"

// Status builder methods
func (s Status) WithMetrics(metrics *Metrics) Status       // Attach metrics
func (s Status) WithSubStatus(subStatus Status) Status     // Add sub-status
```

#### `Metrics`
Performance and operational metrics for health status.

```go
type Metrics struct {
    Uptime            time.Duration `json:"uptime"`                      // Component uptime
    ErrorCount        int           `json:"error_count"`                 // Total error count
    MessagesProcessed int64         `json:"messages_processed,omitempty"` // Messages processed
    LastActivity      time.Time     `json:"last_activity,omitempty"`     // Last activity time
}
```

#### `Monitor`
Thread-safe health monitor for multiple components.

```go
type Monitor struct {
    // private fields for thread safety
}

// Core operations
func NewMonitor() *Monitor                                      // Create new monitor
func (m *Monitor) Update(name string, status Status)           // Update component status
func (m *Monitor) Get(name string) (Status, bool)              // Get component status
func (m *Monitor) GetAll() map[string]Status                  // Get all statuses
func (m *Monitor) Remove(name string)                          // Remove component
func (m *Monitor) Clear()                                      // Clear all components

// Convenience methods
func (m *Monitor) UpdateHealthy(name, message string)          // Mark component healthy
func (m *Monitor) UpdateUnhealthy(name, message string)        // Mark component unhealthy
func (m *Monitor) UpdateDegraded(name, message string)         // Mark component degraded

// Aggregation and inspection
func (m *Monitor) AggregateHealth(systemName string) Status    // System-wide health
func (m *Monitor) ListComponents() []string                    // List component names
func (m *Monitor) Count() int                                  // Number of components
```

### Functions

#### `NewHealthy(component, message string) Status`
Creates a new healthy status with current timestamp.

#### `NewUnhealthy(component, message string) Status`
Creates a new unhealthy status with current timestamp.

#### `NewDegraded(component, message string) Status`
Creates a new degraded status with current timestamp.

#### `Aggregate(component string, subStatuses []Status) Status`
Aggregates multiple sub-statuses using hierarchical health rules.

#### `FromComponentHealth(name string, ch component.HealthStatus) Status`
Converts component package health status to health package status.

## Architecture

### Design Decisions

**Three-State Health Model**: Chose Healthy/Degraded/Unhealthy over binary healthy/unhealthy to enable nuanced operational states and appropriate response strategies.
- Trade-off: Gained operational flexibility but increased complexity of health logic
- Alternative considered: Binary health model (too simplistic for production systems)

**Thread-Safe Monitor**: Used read-write mutex for concurrent access to health statuses because health monitoring must work safely in multi-goroutine environments.
- Rationale: Health updates and reads happen frequently from multiple goroutines
- Trade-off: Gained thread safety but added mutex overhead

**Hierarchical Aggregation**: Implemented hierarchical health aggregation with worst-case rollup (any unhealthy component makes system unhealthy).
- Chose conservative aggregation because false negatives in health are worse than false positives
- Trade-off: Gained system visibility but may over-report unhealthy states

### Integration Points

- **Dependencies**: component package for HealthStatus conversion, standard library for time and sync
- **Used By**: service package for service health, component package for component monitoring
- **Data Flow**: `Component Status → Monitor Update → Health Aggregation → System Status`

## Configuration

### Health Check Intervals

```yaml
# Example health monitoring configuration
health:
  check_interval: "30s"        # How often to check component health
  timeout: "5s"               # Health check timeout
  unhealthy_threshold: 3      # Failed checks before marking unhealthy
  degraded_threshold: 5       # High latency checks before marking degraded
```

### Component Health Thresholds

```yaml
# Component-specific health thresholds
components:
  database:
    response_time_warning: "100ms"    # Degraded threshold
    response_time_critical: "500ms"   # Unhealthy threshold
    connection_pool_warning: 80       # Pool utilization %

  message_queue:
    queue_depth_warning: 1000         # Messages in queue
    queue_depth_critical: 5000
    consumer_lag_warning: "30s"
```

## Error Handling

### Health Status Validation

```go
// Validate health status
if status.Component == "" {
    return fmt.Errorf("component name is required")
}

if status.Status != "healthy" && status.Status != "degraded" && status.Status != "unhealthy" {
    return fmt.Errorf("invalid status: %s", status.Status)
}

// Check for stale health status
if time.Since(status.Timestamp) > 5*time.Minute {
    log.Printf("Stale health status for %s: %v old", status.Component, time.Since(status.Timestamp))
}
```

### Best Practices

```go
// DO: Use descriptive health messages
monitor.UpdateDegraded("database", "Connection pool at 85% capacity")

// DO: Include relevant metrics
status := health.NewHealthy("processor", "Processing normally").
    WithMetrics(&health.Metrics{
        Uptime:            uptime,
        MessagesProcessed: totalMessages,
        LastActivity:      lastProcessTime,
    })

// DO: Use hierarchical status for complex systems
systemStatus := health.Aggregate("platform", []health.Status{
    dbStatus,
    queueStatus,
    apiStatus,
})

// DON'T: Use vague messages
monitor.UpdateUnhealthy("service", "Something is wrong") // Too vague

// DON'T: Forget to handle missing components
if status, exists := monitor.Get("component"); exists {
    // Process status
} else {
    log.Printf("Component not found in health monitor")
}
```

## Testing

### Test Utilities

```go
// Test health status creation
func TestHealthStatus(t *testing.T) {
    status := health.NewHealthy("test-component", "Test message")

    assert.Equal(t, "test-component", status.Component)
    assert.Equal(t, "healthy", status.Status)
    assert.True(t, status.IsHealthy())
    assert.False(t, status.Timestamp.IsZero())
}

// Test health aggregation
func TestHealthAggregation(t *testing.T) {
    testCases := []struct {
        name       string
        subStatus  []health.Status
        expected   string
    }{
        {
            name: "all healthy",
            subStatus: []health.Status{
                health.NewHealthy("comp1", "ok"),
                health.NewHealthy("comp2", "ok"),
            },
            expected: "healthy",
        },
        {
            name: "one unhealthy",
            subStatus: []health.Status{
                health.NewHealthy("comp1", "ok"),
                health.NewUnhealthy("comp2", "error"),
            },
            expected: "unhealthy",
        },
    }

    for _, tc := range testCases {
        t.Run(tc.name, func(t *testing.T) {
            aggregated := health.Aggregate("system", tc.subStatus)
            assert.Equal(t, tc.expected, aggregated.Status)
        })
    }
}
```

### Testing Patterns

- Use table-driven tests for health aggregation scenarios
- Test concurrent access to health monitor with goroutines
- Verify timestamp handling and automatic updates
- Test edge cases like empty component lists and stale statuses

## Performance Considerations

- **Concurrency**: Read-write mutex provides concurrent read access while protecting writes
- **Memory**: Status objects are value types with minimal heap allocation
- **Aggregation**: O(n) complexity for aggregating n sub-components
- **Monitor Operations**: O(1) for get/set operations on component health

## Examples

### Example 1: Service Health Monitoring

```go
package main

import (
    "context"
    "log"
    "net/http"
    "time"

    "github.com/c360/semstreams/health"
)

type ServiceHealthMonitor struct {
    monitor     *health.Monitor
    services    map[string]HealthChecker
    checkTicker *time.Ticker
}

type HealthChecker interface {
    CheckHealth(ctx context.Context) error
}

func NewServiceHealthMonitor() *ServiceHealthMonitor {
    return &ServiceHealthMonitor{
        monitor:  health.NewMonitor(),
        services: make(map[string]HealthChecker),
    }
}

func (s *ServiceHealthMonitor) RegisterService(name string, checker HealthChecker) {
    s.services[name] = checker
    s.monitor.UpdateHealthy(name, "Service registered")
}

func (s *ServiceHealthMonitor) Start(ctx context.Context) {
    // Check all services every 30 seconds
    s.checkTicker = time.NewTicker(30 * time.Second)

    go func() {
        for {
            select {
            case <-s.checkTicker.C:
                s.checkAllServices(ctx)
            case <-ctx.Done():
                s.checkTicker.Stop()
                return
            }
        }
    }()
}

func (s *ServiceHealthMonitor) checkAllServices(ctx context.Context) {
    for name, checker := range s.services {
        go s.checkService(ctx, name, checker)
    }
}

func (s *ServiceHealthMonitor) checkService(ctx context.Context, name string, checker HealthChecker) {
    checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
    defer cancel()

    start := time.Now()
    err := checker.CheckHealth(checkCtx)
    duration := time.Since(start)

    metrics := &health.Metrics{
        LastActivity: time.Now(),
    }

    if err != nil {
        s.monitor.Update(name, health.NewUnhealthy(name, err.Error()).WithMetrics(metrics))
        log.Printf("Health check failed for %s: %v", name, err)
    } else if duration > 1*time.Second {
        s.monitor.Update(name, health.NewDegraded(name,
            fmt.Sprintf("Slow response: %v", duration)).WithMetrics(metrics))
    } else {
        s.monitor.Update(name, health.NewHealthy(name,
            fmt.Sprintf("Response time: %v", duration)).WithMetrics(metrics))
    }
}

func (s *ServiceHealthMonitor) GetSystemHealth() health.Status {
    return s.monitor.AggregateHealth("platform")
}

func (s *ServiceHealthMonitor) HTTPHandler() http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        systemHealth := s.GetSystemHealth()

        w.Header().Set("Content-Type", "application/json")

        if systemHealth.IsUnhealthy() {
            w.WriteHeader(http.StatusServiceUnavailable)
        } else if systemHealth.IsDegraded() {
            w.WriteHeader(http.StatusAccepted)
        } else {
            w.WriteHeader(http.StatusOK)
        }

        json.NewEncoder(w).Encode(systemHealth)
    }
}

// Mock health checker implementations
type DatabaseHealthChecker struct {
    connectionPool *sql.DB
}

func (d *DatabaseHealthChecker) CheckHealth(ctx context.Context) error {
    return d.connectionPool.PingContext(ctx)
}

type ExternalAPIChecker struct {
    baseURL string
}

func (e *ExternalAPIChecker) CheckHealth(ctx context.Context) error {
    req, err := http.NewRequestWithContext(ctx, "GET", e.baseURL+"/health", nil)
    if err != nil {
        return err
    }

    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        return err
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK {
        return fmt.Errorf("API returned status %d", resp.StatusCode)
    }

    return nil
}

func main() {
    monitor := NewServiceHealthMonitor()

    // Register services
    monitor.RegisterService("database", &DatabaseHealthChecker{})
    monitor.RegisterService("external-api", &ExternalAPIChecker{baseURL: "https://api.example.com"})

    // Start health monitoring
    ctx := context.Background()
    monitor.Start(ctx)

    // Setup HTTP health endpoint
    http.HandleFunc("/health", monitor.HTTPHandler())

    log.Printf("Health monitoring started, listening on :8080/health")
    log.Fatal(http.ListenAndServe(":8080", nil))
}
```

### Example 2: Component Health Dashboard

```go
type HealthDashboard struct {
    monitor     *health.Monitor
    subscribers []chan health.Status
    mu          sync.RWMutex
}

func NewHealthDashboard() *HealthDashboard {
    return &HealthDashboard{
        monitor:     health.NewMonitor(),
        subscribers: make([]chan health.Status, 0),
    }
}

func (d *HealthDashboard) Subscribe() <-chan health.Status {
    d.mu.Lock()
    defer d.mu.Unlock()

    ch := make(chan health.Status, 10)
    d.subscribers = append(d.subscribers, ch)
    return ch
}

func (d *HealthDashboard) UpdateComponentHealth(name string, status health.Status) {
    // Update the monitor
    d.monitor.Update(name, status)

    // Notify all subscribers
    d.mu.RLock()
    defer d.mu.RUnlock()

    for _, subscriber := range d.subscribers {
        select {
        case subscriber <- status:
        default:
            // Subscriber not keeping up, skip
        }
    }
}

func (d *HealthDashboard) GetHealthSummary() map[string]any {
    allStatuses := d.monitor.GetAll()
    systemHealth := d.monitor.AggregateHealth("system")

    healthy := 0
    degraded := 0
    unhealthy := 0

    for _, status := range allStatuses {
        switch {
        case status.IsHealthy():
            healthy++
        case status.IsDegraded():
            degraded++
        case status.IsUnhealthy():
            unhealthy++
        }
    }

    return map[string]any{
        "system_status": systemHealth.Status,
        "system_message": systemHealth.Message,
        "component_count": d.monitor.Count(),
        "healthy_count": healthy,
        "degraded_count": degraded,
        "unhealthy_count": unhealthy,
        "components": allStatuses,
        "last_updated": time.Now(),
    }
}

func (d *HealthDashboard) WebSocketHandler(w http.ResponseWriter, r *http.Request) {
    upgrader := websocket.Upgrader{
        CheckOrigin: func(r *http.Request) bool { return true },
    }

    conn, err := upgrader.Upgrade(w, r, nil)
    if err != nil {
        log.Printf("WebSocket upgrade failed: %v", err)
        return
    }
    defer conn.Close()

    // Send initial health summary
    summary := d.GetHealthSummary()
    conn.WriteJSON(summary)

    // Subscribe to health updates
    updates := d.Subscribe()

    for {
        select {
        case status := <-updates:
            // Send individual component update
            update := map[string]any{
                "type": "component_update",
                "component": status.Component,
                "status": status,
            }
            if err := conn.WriteJSON(update); err != nil {
                return
            }

        case <-r.Context().Done():
            return
        }
    }
}

func main() {
    dashboard := NewHealthDashboard()

    // Simulate component health updates
    go func() {
        components := []string{"database", "cache", "queue", "api"}

        for {
            for _, comp := range components {
                var status health.Status

                // Random health status for demo
                switch rand.Intn(3) {
                case 0:
                    status = health.NewHealthy(comp, "Operating normally")
                case 1:
                    status = health.NewDegraded(comp, "Performance degraded")
                case 2:
                    status = health.NewUnhealthy(comp, "Service unavailable")
                }

                dashboard.UpdateComponentHealth(comp, status)
            }

            time.Sleep(10 * time.Second)
        }
    }()

    // Setup HTTP endpoints
    http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
        summary := dashboard.GetHealthSummary()
        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(summary)
    })

    http.HandleFunc("/health/ws", dashboard.WebSocketHandler)

    log.Printf("Health dashboard running on :8080")
    log.Fatal(http.ListenAndServe(":8080", nil))
}
```

## Related Packages

- [`pkg/component`](../component): Component framework providing HealthStatus integration
- [`pkg/service`](../service): Service framework using health monitoring
- [`pkg/errors`](../errors): Error classification for health status determination
- [`pkg/util`](../util): Utility functions for health check implementations

## License

MIT