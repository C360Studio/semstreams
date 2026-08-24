// Package service provides service lifecycle management, HTTP server coordination,
// and component orchestration for the StreamKit platform.
//
// The service package implements a sophisticated service architecture with clearly
// separated responsibilities across multiple service types:
//
// # Core Service Types
//
// BaseService: Foundation for all services with standardized lifecycle management:
//   - Lifecycle states: Stopped → Starting → Running → Stopping
//   - Health monitoring with periodic checks
//   - Metrics integration with CoreMetrics registry
//   - Context-based cancellation and graceful shutdown
//   - Dependency injection through Dependencies
//
// Manager: Central orchestration of HTTP server and service lifecycle:
//   - HTTP server management with graceful shutdown
//   - Service registration and dependency injection
//   - Two-phase HTTP initialization (system endpoints → service endpoints)
//   - Health aggregation across all services
//   - OpenAPI documentation aggregation
//
// ComponentManager: boot-only component lifecycle ownership:
//   - Captures configuration once during construction
//   - Creates the enabled boot set and seals Registry declarations
//   - Retains live handles as the sole lifecycle owner
//   - Exposes read-only health, status, configuration, and flow graph views
//
// FlowService: saved flow-diagram authoring API:
//   - CRUD operations for flow definitions
//   - Validation and compilation through Engine
//   - Explicit sorted, upsert-only publication for the next boot
//   - Best-effort observations keyed by diagram component names
//
// # Service Patterns
//
// All services follow standardized patterns:
//
// Constructor Pattern with Dependency Injection:
//
//	type MyService struct {
//	    *BaseService
//	    // service-specific fields
//	}
//
//	func NewMyService(deps Dependencies, config MyConfig) (*MyService, error) {
//	    base := NewBaseService("my-service", deps)
//	    svc := &MyService{BaseService: base}
//	    // Initialize service-specific fields
//	    return svc, nil
//	}
//
// Lifecycle Implementation:
//
//	func (s *MyService) Initialize(ctx context.Context) error {
//	    // One-time initialization
//	    return s.BaseService.Initialize(ctx)
//	}
//
//	func (s *MyService) Start(ctx context.Context) error {
//	    // Start background operations
//	    return s.BaseService.Start(ctx)
//	}
//
//	func (s *MyService) Stop(ctx context.Context) error {
//	    // Graceful shutdown
//	    return s.BaseService.Stop(ctx)
//	}
//
// HTTP Handler Integration:
//
//	func (s *MyService) RegisterHTTPHandlers(mux *http.ServeMux) {
//	    mux.HandleFunc("/api/v1/myservice/", s.handleRequest)
//	}
//
//	func (s *MyService) OpenAPISpec() map[string]any {
//	    return map[string]any{
//	        "paths": map[string]any{
//	            "/api/v1/myservice/": {
//	                "get": map[string]any{
//	                    "summary": "My service endpoint",
//	                    "responses": map[string]any{
//	                        "200": map[string]any{
//	                            "description": "Success",
//	                        },
//	                    },
//	                },
//	            },
//	        },
//	    }
//	}
//
// # Service Registration
//
// Services are registered with Manager using constructor functions:
//
//	manager := service.NewServiceManager(deps)
//
//	// Register services
//	manager.RegisterConstructor("my-service", func(deps Dependencies) (Service, error) {
//	    return NewMyService(deps, config)
//	})
//
//	// Initialize and start all services
//	if err := manager.InitializeAll(ctx); err != nil {
//	    log.Fatal(err)
//	}
//	if err := manager.StartAll(ctx); err != nil {
//	    log.Fatal(err)
//	}
//
// # HTTP Server Management
//
// Manager coordinates HTTP server lifecycle with startup diagnostics and
// atomic route promotion:
//
//  1. Startup phase, after composition is sealed:
//     - The shared listener binds before any service Start
//     - Only /health, /healthz, /readyz, /services,
//     /services/health, and read-only component diagnostics are served
//     - Other routes return 503 with the exact body NOT READY
//     - Manager binds the configured built-in Prometheus listener without
//     changing service lifecycle order
//
//  2. Commitment phase, after every fallible boot step succeeds:
//     - Service, gateway, graph, and OpenAPI routes are built off-path
//     - Manager starts its remaining runtime owners, stores the complete mux,
//     and commits boot as the final non-failing transition
//
// Requests therefore see either startup diagnostics or the complete route set,
// never a partially registered mux. TCP reachability is not readiness; callers
// use the /readyz status code. Its exact bodies remain READY and NOT READY.
//
// # Health Monitoring
//
// Services implement health checks through BaseService:
//
//	// Override health check logic
//	func (s *MyService) healthCheck() error {
//	    if !s.isHealthy {
//	        return fmt.Errorf("service unhealthy: %v", s.lastError)
//	    }
//	    return nil
//	}
//
// Health status is aggregated by Manager:
//   - /health - Returns 200 if any service is healthy
//   - /readyz - Returns 200 only after boot commitment, successful Starts,
//     no Stop observation, and current health for every admitted unit
//
// # Metrics Integration
//
// CoreMetrics and Manager-owned startup observation expose:
//   - semstreams_service_status - Current service status (gauge)
//   - semstreams_startup_units - Process-local admitted/invoked/completed/failed counts
//   - semstreams_messages_received_total - Message counter
//   - semstreams_messages_processed_total - Processing counter
//   - semstreams_health_checks_total - Health check counter
//
// # Component Management
//
// ComponentManager composes only the configuration snapshot captured by its
// constructor. It does not subscribe to later component or model-registry
// changes. Registry stores defensive declaration values and is sealed after
// boot admission; ComponentManager retains all concrete runtime handles.
//
// Flow CRUD and validation are authoring-only. Explicit
// publish-component-configs writes sorted component candidates and reports exact
// partial progress. Those writes do not change the current process and require
// a later process boot.
//
// # Error Handling
//
// Services follow StreamKit error handling patterns:
//   - Configuration errors: Return during construction
//   - Initialization errors: Return from Initialize()
//   - Runtime errors: Log and update health status
//   - Shutdown errors: Log but continue graceful shutdown
//
// Use project error wrapping for context:
//
//	import "github.com/c360studio/semstreams/pkg/errs"
//
//	if err := validateConfig(cfg); err != nil {
//	    return errs.WrapInvalid(err, "my-service", "NewMyService", "validate config")
//	}
//
// # Graceful Shutdown
//
// Manager coordinates graceful shutdown in reverse order:
//  1. Stop accepting new HTTP requests
//  2. Stop services in reverse registration order
//  3. Shutdown HTTP server with timeout
//  4. Close remaining connections
//
// Example:
//
//	// Main application
//	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
//	defer cancel()
//
//	if err := manager.StopAll(ctx); err != nil {
//	    log.Printf("Graceful shutdown incomplete: %v", err)
//	}
//
// # Testing
//
// The package provides ServiceSuite for integration testing with testcontainers:
//
//	func TestMyService(t *testing.T) {
//	    suite := service.NewServiceSuite(t)
//	    defer suite.Cleanup()
//
//	    // Suite provides NATS client, config manager, etc.
//	    svc, err := NewMyService(suite.Deps(), config)
//	    require.NoError(t, err)
//
//	    // Test service lifecycle
//	    err = svc.Initialize(suite.Context())
//	    require.NoError(t, err)
//	}
//
// # Security Considerations
//
// The service HTTP APIs are designed for internal edge deployment:
//   - No built-in authentication (add reverse proxy for production)
//   - No rate limiting (implement at gateway level)
//   - Path traversal protection on component endpoints
//   - Input validation on all HTTP handlers
//
// For production deployments, add external security layers:
//   - Reverse proxy with authentication (nginx, Traefik)
//   - Network policies to restrict access
//   - TLS termination at gateway
//   - Rate limiting at gateway level
//
// # Example: Complete Service Implementation
//
//	package main
//
//	import (
//	    "context"
//	    "log"
//	    "os"
//	    "os/signal"
//	    "syscall"
//
//	    "github.com/c360studio/semstreams/service"
//	    "github.com/c360studio/semstreams/config"
//	    "github.com/c360studio/semstreams/natsclient"
//	    "github.com/c360studio/semstreams/metric"
//	)
//
//	func main() {
//	    // Load configuration
//	    cfg, err := config.LoadMinimalConfig("config.json")
//	    if err != nil {
//	        log.Fatal(err)
//	    }
//
//	    // Initialize dependencies
//	    natsClient, err := natsclient.NewClient(cfg.NATS)
//	    if err != nil {
//	        log.Fatal(err)
//	    }
//	    defer natsClient.Close()
//
//	    metricsRegistry := metric.NewMetricsRegistry()
//	    configMgr := config.NewConfigManager(natsClient, cfg)
//
//	    deps := service.Dependencies{
//	        NATSClient:      natsClient,
//	        Manager:   configMgr,
//	        MetricsRegistry: metricsRegistry,
//	        Logger:          slog.Default(),
//	        Platform:        cfg.Platform,
//	    }
//
//	    // Create service manager
//	    manager := service.NewServiceManager(deps)
//
//	    // Register services
//	    manager.RegisterConstructor("flow-service", func(d Dependencies) (Service, error) {
//	        return service.NewFlowService(d, flowEngine, flowStore)
//	    })
//
//	    // Initialize and start
//	    ctx := context.Background()
//	    if err := manager.InitializeAll(ctx); err != nil {
//	        log.Fatal(err)
//	    }
//	    if err := manager.StartAll(ctx); err != nil {
//	        log.Fatal(err)
//	    }
//
//	    // Wait for signal
//	    sig := make(chan os.Signal, 1)
//	    signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
//	    <-sig
//
//	    // Graceful shutdown
//	    shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
//	    defer cancel()
//	    if err := manager.StopAll(shutdownCtx); err != nil {
//	        log.Printf("Shutdown error: %v", err)
//	    }
//	}
//
// For more details and examples, see the README.md in this directory.
package service
