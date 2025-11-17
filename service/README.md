# Service Package

Framework-level service infrastructure for SemStreams, providing service lifecycle management, HTTP server coordination, and configuration management.

## Overview

The service package defines the core service architecture for SemStreams, providing explicit service registration with standardized lifecycle management, dependency injection, and HTTP endpoint coordination. This package follows clean architecture principles with dependency injection through Dependencies and configuration-driven service instantiation.

Services in SemStreams are self-contained units that are explicitly registered via the RegisterAll() function, receive structured dependencies, and can optionally expose HTTP endpoints through a shared server. The Manager coordinates all service lifecycle operations while maintaining clean separation of concerns.

The package supports both mandatory services (always running) and optional services (config-driven), with built-in health monitoring, graceful shutdown, and OpenAPI documentation aggregation.

## Installation

```go
import "github.com/c360/semstreams/service"
```

## Core Concepts

### Service Interface

Every service must implement the Service interface, providing lifecycle methods (Start/Stop) and health monitoring. Services handle their own configuration parsing and business logic.

### Explicit Registration Pattern

Services export Register() functions that are called by RegisterAll() in register.go, enabling clear dependency graphs and testable service registration without global state modification.

### Dependencies

All external dependencies (NATS client, metrics registry, logger, platform identity, config manager) are injected through Dependencies struct, following clean dependency injection patterns.

### Manager

Central coordinator that manages service lifecycle, owns the shared HTTP server, and aggregates OpenAPI documentation from all services. Acts as both a framework component and a service itself.

### ComponentManager Service

Special service that manages component lifecycle (inputs, processors, outputs, storage). Provides HTTP APIs for component health, status, and configuration management. See [component package](../component) for component architecture and [flowgraph](../component/flowgraph) for connectivity validation.

## Usage

### Basic Example

```go
// Exported Register function for explicit registration
func Register(registry *service.Registry) error {
    return registry.Register("my-service", NewMyService)
}

// Constructor following service pattern
func NewMyService(rawConfig json.RawMessage, deps *Dependencies) (Service, error) {
    cfg := &MyServiceConfig{
        Enabled: true,  // default values
        Port:    8080,
    }
    
    // Parse raw JSON configuration
    if len(rawConfig) > 0 {
        if err := json.Unmarshal(rawConfig, cfg); err != nil {
            return nil, fmt.Errorf("invalid my-service config: %w", err)
        }
    }
    
    return &MyService{
        config: cfg,
        nats:   deps.NATSClient,
        logger: deps.Logger,
        platform: deps.Platform,
    }, nil
}

// Service implementation
type MyService struct {
    config *MyServiceConfig
    nats   *natsclient.Client
    logger *slog.Logger
    platform types.PlatformMeta
}

func (s *MyService) Start(ctx context.Context) error {
    s.logger.Info("Starting my-service", "org", s.platform.Org, "platform", s.platform.Platform)
    // Service-specific startup logic
    return nil
}

func (s *MyService) Stop(timeout time.Duration) error {
    s.logger.Info("Stopping my-service")
    // Graceful shutdown logic
    return nil
}

func (s *MyService) IsHealthy() bool {
    return true // Service-specific health check
}

func (s *MyService) GetStatus() ServiceStatus {
    return ServiceStatus{
        Name:    "my-service",
        Healthy: s.IsHealthy(),
        Started: time.Now(), // Track actual start time
    }
}
```

### Advanced Usage

```go
// Service with HTTP endpoints
type MyService struct {
    // ... fields
}

// Implement HTTPHandler interface for HTTP endpoints
func (s *MyService) RegisterHTTPHandlers(prefix string, mux *http.ServeMux) {
    mux.HandleFunc(prefix+"/status", s.handleStatus)
    mux.HandleFunc(prefix+"/data", s.handleData)
}

func (s *MyService) OpenAPISpec() *OpenAPISpec {
    return &OpenAPISpec{
        Paths: map[string]PathItem{
            "/status": {
                Get: &Operation{
                    Summary:     "Get service status",
                    Description: "Returns current service status",
                    Responses: map[string]Response{
                        "200": {Description: "Service status"},
                    },
                },
            },
        },
    }
}

// Service with runtime configuration support
func (s *MyService) GetRuntimeConfig() map[string]any {
    return map[string]any{
        "enabled": s.config.Enabled,
        "port":    s.config.Port,
    }
}

func (s *MyService) ValidateConfigUpdate(newConfig map[string]any) error {
    // Validate proposed configuration changes
    if port, ok := newConfig["port"].(float64); ok {
        if port != float64(s.config.Port) {
            return fmt.Errorf("port changes require service restart")
        }
    }
    return nil
}

func (s *MyService) ApplyConfigUpdate(newConfig map[string]any) error {
    // Apply runtime configuration changes
    if enabled, ok := newConfig["enabled"].(bool); ok {
        s.config.Enabled = enabled
        s.logger.Info("Updated enabled setting", "enabled", enabled)
    }
    return nil
}
```

### ComponentManager HTTP APIs

ComponentManager service exposes HTTP endpoints for component management:

```go
// GET /api/v1/components - List all managed components
// GET /api/v1/components/{name} - Get specific component details
// GET /api/v1/components/{name}/health - Component health status
// POST /api/v1/components/{name}/start - Start a component
// POST /api/v1/components/{name}/stop - Stop a component

// Connectivity validation endpoints (uses flowgraph internally)
// GET /api/v1/flowgraph - Component connectivity graph
// GET /api/v1/validate/connectivity - Connectivity analysis
```

For component architecture and connectivity validation details, see [component package](../component) and [flowgraph](../component/flowgraph).

### FlowService Runtime Endpoints

FlowService exposes runtime observability endpoints for debugging and monitoring running flows:

```go
// GET /flowbuilder/flows/{id}/runtime/metrics - Component metrics (JSON polling)
// Returns throughput, error rates, queue depth per component
// Poll interval: 2-5s recommended
// Response time: <100ms (90ms timeout)

// GET /flowbuilder/flows/{id}/runtime/health - Component health (JSON polling)
// Returns component status, uptime, last activity
// Poll interval: 5s recommended
// Response time: <200ms (180ms timeout)

// GET /flowbuilder/flows/{id}/runtime/messages - NATS message flow (JSON polling)
// Returns filtered message logger entries for flow components
// Poll interval: 1-2s recommended
// Response time: <100ms (90ms timeout)

// GET /flowbuilder/flows/{id}/runtime/logs - Component logs (SSE streaming)
// Streams real-time logs from all components in the flow
// Connection: Server-Sent Events (text/event-stream)
```

**Runtime Metrics Response** (`GET /runtime/metrics`):

```json
{
  "timestamp": "2025-11-17T14:23:05.123456789Z",
  "components": [
    {
      "name": "udp-source",
      "throughput": 1234.5,
      "error_rate": 0.0,
      "queue_depth": 0,
      "status": "healthy"
    }
  ],
  "prometheus_available": true
}
```

**Runtime Health Response** (`GET /runtime/health`):

```json
{
  "timestamp": "2025-11-17T14:23:05.123456789Z",
  "overall": {
    "status": "healthy",
    "running_count": 3,
    "degraded_count": 0,
    "error_count": 0
  },
  "components": [
    {
      "name": "udp-source",
      "type": "udp",
      "status": "running",
      "healthy": true,
      "message": "Processing messages",
      "start_time": "2025-11-17T14:07:33.123Z",
      "last_activity": "2025-11-17T14:23:04.567Z",
      "uptime_seconds": 932,
      "details": null
    }
  ]
}
```

**Runtime Messages Response** (`GET /runtime/messages?limit=100`):

```json
{
  "timestamp": "2025-11-17T14:23:01.123456789Z",
  "messages": [
    {
      "timestamp": "2025-11-17T14:23:01.234567890Z",
      "subject": "process.json-processor.data",
      "message_id": "msg-12345",
      "component": "json-processor",
      "direction": "published",
      "summary": "JSON filter applied",
      "metadata": {"size_bytes": 256},
      "message_type": "ProcessedData"
    }
  ],
  "total": 1,
  "limit": 100
}
```

**Runtime Logs Stream** (`GET /runtime/logs` - SSE):

```
event: log
data: {"timestamp":"2025-11-17T14:23:01.123Z","level":"INFO","component":"udp-source","message":"Listening on :5000"}

event: log
data: {"timestamp":"2025-11-17T14:23:02.456Z","level":"ERROR","component":"processor","message":"Failed to parse JSON"}

event: ping
data: {"timestamp":"2025-11-17T14:23:05.000Z"}
```

**Architecture**:

- **Metrics**: Three-tier fallback (Prometheus API → raw metrics → health only)
- **Health**: Uses ComponentManager health status with timing enhancements
- **Messages**: Filters MessageLogger circular buffer by flow component subjects
- **Logs**: Aggregates component logs and streams via SSE with reconnection support

**Performance**:

- All endpoints enforce strict timeouts (<100ms or <200ms)
- Graceful degradation when optional services unavailable
- UTC timestamps for consistency across distributed systems
- Pre-allocated buffers and efficient filtering

**Security**:

- Input validation on all query parameters
- DoS protection via limit enforcement (max 1000 messages)
- Subject pattern sanitization prevents injection attacks
- Error messages safe for client exposure

For implementation details, see:

- `flow_runtime_metrics.go` - Prometheus integration with fallback tiers
- `flow_runtime_health.go` - Component health aggregation with timing
- `flow_runtime_messages.go` - NATS message flow filtering
- `flow_runtime_logs.go` - SSE log streaming (if implemented)

## API Reference

### Types

#### `Service`

Primary interface that all services must implement.

```go
type Service interface {
    Start(ctx context.Context) error    // Start service with context
    Stop(timeout time.Duration) error   // Stop service with timeout
    IsHealthy() bool                    // Health check
    GetStatus() ServiceStatus           // Service status for monitoring
}
```

#### `Dependencies`

Dependency injection structure for service construction.

```go  
type Dependencies struct {
    NATSClient      *natsclient.Client        // Required: NATS messaging client
    MetricsRegistry *metric.MetricsRegistry   // Optional: Prometheus metrics
    Logger          *slog.Logger              // Optional: structured logger (defaults to slog.Default())
    Platform        types.PlatformMeta        // Required: platform identity (org + platform)
    Manager   *config.Manager     // Optional: centralized configuration management
}
```

#### `Manager`

Central service coordinator and HTTP server owner.

```go
type Manager struct {
    // Thread-safe service lifecycle management and HTTP server coordination
}
```

### Functions

#### `RegisterConstructor(name string, constructor Constructor)`

Registers a service constructor with the ServiceRegistry. Called by RegisterAll() during service initialization.

#### `(m *Manager) CreateService(name string, rawConfig json.RawMessage, deps *Dependencies) (Service, error)`

Creates a service instance using the registered constructor with proper dependency injection.

#### `(m *Manager) StartAll(ctx context.Context) error`

Starts all created services in registration order with proper error handling.

#### `(m *Manager) StopAll(timeout time.Duration) error`

Stops all services in reverse order with graceful shutdown and timeout handling.

#### `(cm *ComponentManager) ListComponents() []component.Discoverable`

Returns all managed components for introspection and monitoring.

#### `(cm *ComponentManager) GetComponent(name string) (component.Discoverable, bool)`

Retrieves a specific managed component by name. Returns false if component not found.

### Interfaces

#### `HTTPHandler`

```go
type HTTPHandler interface {
    RegisterHTTPHandlers(prefix string, mux *http.ServeMux)
    OpenAPISpec() *OpenAPISpec
}
```

Optional interface for services that want to expose HTTP endpoints. Manager automatically registers handlers and aggregates OpenAPI documentation.

#### `RuntimeConfigurable`

```go
type RuntimeConfigurable interface {
    GetRuntimeConfig() map[string]any
    ValidateConfigUpdate(newConfig map[string]any) error
    ApplyConfigUpdate(newConfig map[string]any) error
}
```

Optional interface for services that support runtime configuration changes without restart.

## Architecture

### Design Decisions

**Explicit Service Registration**: Chose RegisterAll() orchestration over init() self-registration

- Automatic discovery without configuration complexity
- Clean dependency management through imports
- Explicit control over service availability

**Centralized HTTP Server**: Manager owns single HTTP server shared by all services

- Eliminates port conflicts and resource waste
- Unified OpenAPI documentation and routing
- Consistent URL patterns and middleware

**Constructor Pattern**: Standardized service constructor signature matching dependency injection

- Services handle their own configuration parsing and validation
- Enables flexible per-service configuration schemas
- Clean separation between framework and service logic

**Configuration-Driven Instantiation**: Services created only if registered AND configured

- Clear distinction between available (registered) and active (configured)
- Supports optional services with graceful degradation
- Environment-specific service composition

**ComponentManager Integration**: Special service for managing component lifecycle

- Manages component startup/shutdown and health monitoring
- Provides HTTP APIs for component introspection and control
- Integrates with component package for connectivity validation
- Enables runtime component management and debugging

### Integration Points

- **Dependencies**: NATS client (required), MetricsRegistry (optional), Logger (optional), Manager (optional)
- **Used By**: Main application for service orchestration, individual services for HTTP endpoints
- **Component Integration**: ComponentManager service integrates with [component package](../component) for lifecycle management
- **Data Flow**: `Configuration → Constructor → Service Instance → Manager → HTTP Endpoints`

## Configuration

### Required Configuration

```json
{
  "services": {
    "http_port": 8080,
    "swagger_ui": true,
    "component-manager": {
      "enabled": true
    },
    "metrics": {
      "enabled": true,
      "port": 9090,
      "path": "/metrics"
    }
  }
}
```

### Optional Configuration

```json
{
  "services": {
    "discovery": {
      "enabled": false
    },
    "message-logger": {
      "enabled": false,
      "max_messages": 1000
    },
    "service-manager": {
      "read_timeout": "10s",
      "write_timeout": "10s",
      "shutdown_timeout": "30s"
    }
  }
}
```

## Error Handling

### Error Types

This package defines the following error patterns:

```go
// Service registration errors
ErrServiceAlreadyExists = errors.New("service: constructor already registered")
ErrInvalidConstructor  = errors.New("service: invalid constructor function")

// Service lifecycle errors  
ErrServiceNotFound     = errors.New("service: service not found")
ErrServiceStartup      = errors.New("service: failed to start")
ErrServiceShutdown     = errors.New("service: failed to stop gracefully")

// HTTP server errors
ErrHTTPServerStartup   = errors.New("service: failed to start HTTP server")
ErrPortInUse          = errors.New("service: HTTP port already in use")
```

### Error Detection

```go
svc, err := manager.CreateService("my-service", config, deps)
if errors.Is(err, service.ErrServiceNotFound) {
    // Handle missing service constructor
}

err = manager.StartAll(ctx)
if errors.Is(err, service.ErrServiceStartup) {
    // Handle service startup failure
}
```

## Testing

### Test Utilities

This package provides comprehensive test utilities for service testing:

```go
// ServiceSuite provides NATS testcontainer and common setup
type ServiceSuite struct {
    natsClient *natsclient.TestClient
    manager    *Manager
    deps       *Dependencies
}

// Use in service tests
func (s *MyServiceSuite) SetupTest() {
    s.ServiceSuite.SetupTest()
    
    // Register and create your service
    service.RegisterConstructor("my-service", NewMyService)
    svc, err := s.manager.CreateService("my-service", config, s.deps)
    s.Require().NoError(err)
}

// Test service lifecycle
func (s *MyServiceSuite) TestMyService_Lifecycle() {
    err := s.service.Start(context.Background())
    s.Assert().NoError(err)
    s.Assert().True(s.service.IsHealthy())
    
    err = s.service.Stop(5 * time.Second)
    s.Assert().NoError(err)
}
```

### Testing Patterns

- Use ServiceSuite for integration tests with real NATS via testcontainers
- Test service behavior through Service interface methods
- Verify HTTP endpoints using httptest.ResponseRecorder
- Test configuration parsing with various JSON inputs
- Validate graceful shutdown and resource cleanup

For component-specific testing (including connectivity validation), see [component package](../component).

## Performance Considerations

- **Concurrency**: All Manager operations are thread-safe using read-write mutex
- **Memory**: Services maintain references until explicitly stopped and removed
- **HTTP Performance**: Single shared server eliminates overhead of multiple HTTP listeners
- **Startup Time**: Services start in parallel where possible, sequentially where dependencies exist
- **Component Lifecycle**: ComponentManager caches connectivity analysis for efficient repeated access

## Examples

### Example 1: Simple Monitoring Service

```go
package main

import (
    "context"
    "encoding/json"
    "log"
    "net/http"
    "time"
    
    "github.com/c360/semstreams/service"
    "github.com/c360/semstreams/types"
)

// MonitoringService tracks system metrics
type MonitoringService struct {
    config   *MonitoringConfig
    platform types.PlatformMeta
    logger   *slog.Logger
    ticker   *time.Ticker
}

type MonitoringConfig struct {
    Enabled  bool          `json:"enabled"`
    Interval time.Duration `json:"interval"`
}

func NewMonitoringService(rawConfig json.RawMessage, deps *service.Dependencies) (service.Service, error) {
    cfg := &MonitoringConfig{
        Enabled:  true,
        Interval: 30 * time.Second,
    }
    
    if len(rawConfig) > 0 {
        if err := json.Unmarshal(rawConfig, cfg); err != nil {
            return nil, err
        }
    }
    
    return &MonitoringService{
        config:   cfg,
        platform: deps.Platform,
        logger:   deps.Logger,
    }, nil
}

func (m *MonitoringService) Start(ctx context.Context) error {
    if !m.config.Enabled {
        m.logger.Info("Monitoring service disabled")
        return nil
    }
    
    m.ticker = time.NewTicker(m.config.Interval)
    go m.monitoringLoop(ctx)
    
    m.logger.Info("Started monitoring service",
        "interval", m.config.Interval,
        "platform", m.platform.Platform)
    return nil
}

func (m *MonitoringService) Stop(timeout time.Duration) error {
    if m.ticker != nil {
        m.ticker.Stop()
    }
    m.logger.Info("Stopped monitoring service")
    return nil
}

func (m *MonitoringService) IsHealthy() bool {
    return m.config.Enabled && m.ticker != nil
}

func (m *MonitoringService) GetStatus() service.ServiceStatus {
    return service.ServiceStatus{
        Name:    "monitoring",
        Healthy: m.IsHealthy(),
        Details: map[string]any{
            "enabled":  m.config.Enabled,
            "interval": m.config.Interval.String(),
            "platform": m.platform.Platform,
        },
    }
}

func (m *MonitoringService) monitoringLoop(ctx context.Context) {
    for {
        select {
        case <-ctx.Done():
            return
        case <-m.ticker.C:
            m.logger.Debug("Monitoring tick", "platform", m.platform.Platform)
            // Monitoring logic here
        }
    }
}

// HTTP endpoints
func (m *MonitoringService) RegisterHTTPHandlers(prefix string, mux *http.ServeMux) {
    mux.HandleFunc(prefix+"/status", m.handleStatus)
    mux.HandleFunc(prefix+"/metrics", m.handleMetrics)
}

func (m *MonitoringService) OpenAPISpec() *service.OpenAPISpec {
    return &service.OpenAPISpec{
        Paths: map[string]service.PathItem{
            "/status": {
                Get: &service.Operation{
                    Summary: "Get monitoring status",
                    Responses: map[string]service.Response{
                        "200": {Description: "Monitoring status"},
                    },
                },
            },
        },
    }
}

func (m *MonitoringService) handleStatus(w http.ResponseWriter, r *http.Request) {
    status := m.GetStatus()
    json.NewEncoder(w).Encode(status)
}

func (m *MonitoringService) handleMetrics(w http.ResponseWriter, r *http.Request) {
    metrics := map[string]any{
        "platform": m.platform.Platform,
        "uptime":   time.Since(time.Now()), // Would track actual uptime
    }
    json.NewEncoder(w).Encode(metrics)
}

// Explicit registration via exported function
func Register(registry *service.Registry) error {
    return registry.Register("monitoring", NewMonitoringService)
}

func main() {
    // Service is automatically available to Manager
    log.Println("Monitoring service registered and ready")
}
```

### Example 2: Service Coordination and Management

```go
package main

import (
    "context"
    "encoding/json"
    "log"
    "time"
    
    "github.com/c360/semstreams/service"
    "github.com/c360/semstreams/types"
    "github.com/c360/semstreams/natsclient"
    "github.com/c360/semstreams/metric"
)

func main() {
    // Create dependencies
    natsClient, _ := natsclient.NewClient("nats://localhost:4222")
    metricsRegistry := metric.NewMetricsRegistry()
    platform := types.PlatformMeta{
        Org:      "example",
        Platform: "demo-platform",
    }
    
    deps := &service.Dependencies{
        NATSClient:      natsClient,
        MetricsRegistry: metricsRegistry,
        Logger:          slog.Default(),
        Platform:        platform,
    }
    
    // Get the default Manager
    manager := service.DefaultManager
    
    // Configure HTTP server
    manager.SetHTTPConfig(8080, true, service.InfoSpec{
        Title:   "Demo Services",
        Version: "1.0.0",
    })
    
    // Services are registered via RegisterAll()
    // Create services from configuration
    serviceConfigs := map[string]json.RawMessage{
        "monitoring": json.RawMessage(`{"enabled": true, "interval": "10s"}`),
        "metrics":    json.RawMessage(`{"enabled": true, "port": 9090}`),
    }
    
    // Create all configured services
    for name, config := range serviceConfigs {
        svc, err := manager.CreateService(name, config, deps)
        if err != nil {
            log.Printf("Failed to create service %s: %v", name, err)
            continue
        }
        log.Printf("Created service: %s", name)
    }
    
    // Start all services
    ctx := context.Background()
    if err := manager.StartAll(ctx); err != nil {
        log.Fatalf("Failed to start services: %v", err)
    }
    
    log.Println("All services started")
    log.Println("HTTP server available at http://localhost:8080")
    log.Println("API documentation at http://localhost:8080/docs")
    
    // Check service health
    for name, svc := range manager.GetAllServices() {
        if svc.IsHealthy() {
            log.Printf("Service %s: healthy", name)
        } else {
            log.Printf("Service %s: unhealthy", name)
        }
    }
    
    // Simulate running for a while
    time.Sleep(30 * time.Second)
    
    // Graceful shutdown
    log.Println("Shutting down services...")
    if err := manager.StopAll(10 * time.Second); err != nil {
        log.Printf("Error during shutdown: %v", err)
    }
    
    log.Println("All services stopped")
}
```

## Known Limitations

- HTTP server configuration cannot be changed at runtime (requires restart)
- Service dependencies must be acyclic (enforced through import structure)
- OpenAPI spec aggregation assumes unique operation IDs across services
- Graceful shutdown timeout applies to all services equally (no per-service timeouts)

## Related Packages

- [`pkg/component`](../component): ComponentManager service uses component Registry for lifecycle management
- [`pkg/types`](../types): Provides PlatformMeta and other shared types
- [`pkg/natsclient`](../natsclient): NATS client dependency for service messaging
- [`pkg/metric`](../metric): Optional metrics collection for services
- [`pkg/config`](../config): Configuration management and Manager integration
