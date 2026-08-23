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

Special service that composes the constructor-captured boot component set and remains the sole lifecycle owner. It admits immutable declarations to Registry, seals composition, and exposes read-only component health, status, and configuration views. Later configuration writes do not mutate the running set.

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
        Port: 8080, // service-specific default
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

func (s *MyService) Stop(ctx context.Context) error {
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

// Service configuration is restart-only. The containing services map owns
// identity and outer enabled; MyServiceConfig contains only inner settings.
```

### ComponentManager HTTP APIs

ComponentManager exposes read-only endpoints for boot-composition observation:

```go
// GET {prefix}/health - Aggregate component health
// GET {prefix}/list - List the boot-composed components
// GET {prefix}/types and {prefix}/types/{id} - Factory metadata and schemas
// GET {prefix}/status/{name} - Observe one component's status
// GET {prefix}/config/{name} - Observe its captured boot configuration

// Connectivity validation endpoints (uses flowgraph internally)
// GET {prefix}/flowgraph - Component connectivity graph
// GET {prefix}/validate - Connectivity analysis
```

There is no HTTP start, stop, or component-configuration write operation.

For component architecture and connectivity validation details, see [component package](../component) and [flowgraph](../component/flowgraph).

### Saved Flow Authoring Endpoints

FlowService treats flows as saved diagrams, not runtime lifecycle owners:

```text
GET|POST /flowbuilder/flows
GET|PUT|DELETE /flowbuilder/flows/{id}
POST /flowbuilder/flows/{id}/validate
POST /flowbuilder/flows/{id}/publish-component-configs
GET /flowbuilder/flows/{id}/observations/{metrics,health,messages}
```

CRUD and validation do not publish configuration. Explicit publication validates
and compiles the diagram, sorts component names, and performs upsert-only writes.
It reports exact partial progress, leaves the current process unchanged, and
requires a restart before published candidates can be composed. Diagram
omissions never delete component configuration.

Flow deploy/start/stop/undeploy routes, the flow status WebSocket, associated
runtime log stream, runtime ownership state, and lifecycle tools are absent.

## API Reference

### Types

#### `Service`

Primary interface that all services must implement.

```go
type Service interface {
    Start(ctx context.Context) error    // Start service with context
    Stop(ctx context.Context) error     // Cancel Start lifetime and bound join/cleanup
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

#### `(m *Manager) StopAll(ctx context.Context) error`

Stops all services in reverse order, passing the exact caller-owned shutdown context to each service.

#### `(cm *ComponentManager) GetComponentHealth() map[string]component.HealthStatus`

Returns health values for the sealed boot composition.

#### `(cm *ComponentManager) GetComponentStatus() map[string]ComponentStatus`

Returns defensive status values without exposing live component handles.

### Interfaces

#### `HTTPHandler`

```go
type HTTPHandler interface {
    RegisterHTTPHandlers(prefix string, mux *http.ServeMux)
    OpenAPISpec() *OpenAPISpec
}
```

Optional interface for services that want to expose HTTP endpoints. Manager automatically registers handlers and aggregates OpenAPI documentation.

Service configuration is restart-only. KV updates change desired next-boot
state; `GET /services` reports whether those changes require restart. Component
post-boot reconfiguration is not a ComponentManager concern; published changes become eligible at the next boot.

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
- Enables boot-composition observation and debugging without runtime mutation

**Startup observability**: After composition seals, Manager binds the shared
HTTP diagnostics and the configured built-in Prometheus listener before service
startup without changing registration-order lifecycle. `/readyz` returns exact
`NOT READY` until all fallible boot work succeeds, Manager commits the complete
route set, and current service/component health is ready. The existing
`/services` response includes additive `startup` counts; Prometheus exposes the
same progress as `semstreams_startup_units{owner,stage}`. Stop clears commitment
before child cleanup. Treat TCP reachability as liveness only, not readiness.

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
    "service-manager": {
      "enabled": true,
      "config": {
        "http_port": 8080,
        "swagger_ui": true
      }
    },
    "component-manager": {
      "enabled": true,
      "config": {}
    },
    "metrics": {
      "enabled": true,
      "config": {
        "port": 9090,
        "path": "/metrics"
      }
    }
  }
}
```

### Optional Configuration

```json
{
  "services": {
    "discovery": {
      "enabled": false,
      "config": {}
    },
    "message-logger": {
      "enabled": false,
      "config": {
        "max_entries": 1000
      }
    },
    "service-manager": {
      "enabled": true,
      "config": {
        "read_timeout": "10s",
        "write_timeout": "10s",
        "shutdown_timeout": "30s"
      }
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
    Interval time.Duration `json:"interval"`
}

func NewMonitoringService(rawConfig json.RawMessage, deps *service.Dependencies) (service.Service, error) {
    cfg := &MonitoringConfig{
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
    m.ticker = time.NewTicker(m.config.Interval)
    go m.monitoringLoop(ctx)
    
    m.logger.Info("Started monitoring service",
        "interval", m.config.Interval,
        "platform", m.platform.Platform)
    return nil
}

func (m *MonitoringService) Stop(ctx context.Context) error {
    if m.ticker != nil {
        m.ticker.Stop()
    }
    m.logger.Info("Stopped monitoring service")
    return nil
}

func (m *MonitoringService) IsHealthy() bool {
    return m.ticker != nil
}

func (m *MonitoringService) GetStatus() service.ServiceStatus {
    return service.ServiceStatus{
        Name:    "monitoring",
        Healthy: m.IsHealthy(),
        Details: map[string]any{
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
    serviceConfigs := types.ServiceConfigs{
        "monitoring": {Enabled: true, Config: json.RawMessage(`{"interval":"10s"}`)},
        "metrics":    {Enabled: true, Config: json.RawMessage(`{"port":9090}`)},
    }
    
    // Resolve and construct the complete pre-start composition.
    if err := manager.ConfigureFromServices(serviceConfigs, deps); err != nil {
        log.Fatalf("Failed to configure services: %v", err)
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
