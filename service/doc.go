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
// ComponentManager: Dynamic component lifecycle management:
//   - Component instantiation from registry factories
//   - Flow-based component deployment
//   - Runtime configuration updates via NATS KV
//   - Flow graph validation with connectivity analysis
//   - Health monitoring of managed components
//
// FlowService: Visual flow builder HTTP API:
//   - CRUD operations for flow definitions
//   - Flow deployment via Engine integration
//   - Real-time flow status monitoring
//   - Validation feedback with detailed errors
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
// Manager coordinates HTTP server lifecycle with two-phase initialization:
//
//  1. Early Phase (initializeHTTPInfrastructure):
//     - System endpoints registered: /health, /readyz, /metrics
//     - HTTP server created but not started
//
//  2. Late Phase (completeHTTPSetup):
//     - Service endpoints registered after services start
//     - OpenAPI documentation aggregated
//     - HTTP server starts listening
//
// This prevents race conditions and ensures system endpoints are available
// before service-specific endpoints.
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
//   - /readyz - Returns 200 if all services are healthy
//
// # Metrics Integration
//
// Services automatically register metrics with CoreMetrics:
//   - semstreams_service_status - Current service status (gauge)
//   - semstreams_messages_received_total - Message counter
//   - semstreams_messages_processed_total - Processing counter
//   - semstreams_health_checks_total - Health check counter
//
// # Component Management
//
// ComponentManager integrates with the component registry and flow engine:
//
//	cm := service.NewComponentManager(deps, flowEngine, configMgr)
//
//	// Deploy flow with components
//	err := cm.DeployFlow(ctx, flowID, flowDef)
//
//	// Runtime configuration updates
//	err := cm.UpdateComponentConfig(ctx, componentName, newConfig)
//
//	// Health monitoring
//	status := cm.GetComponentStatus(componentName)
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
//	import "github.com/c360/semstreams/errors"
//
//	if err := validateConfig(cfg); err != nil {
//	    return errors.WrapInvalid(err, "my-service", "NewMyService", "validate config")
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
//	    "github.com/c360/semstreams/service"
//	    "github.com/c360/semstreams/config"
//	    "github.com/c360/semstreams/natsclient"
//	    "github.com/c360/semstreams/metric"
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
