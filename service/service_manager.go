package service

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/c360studio/semstreams/config"
	"github.com/c360studio/semstreams/health"
	"github.com/c360studio/semstreams/internal/lifecyclejoin"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/types"
)

// Manager manages service lifecycle using a provided registry.
// Services are explicitly registered and created from raw JSON configs.
type Manager struct {
	*BaseService // Embed BaseService to implement Service interface

	registry *Registry
	services map[string]Service
	order    []string // Track registration order for cleanup
	mu       sync.RWMutex
	sealed   bool

	bootServiceConfigs types.ServiceConfigs
	sealedServices     []string

	// HTTP server infrastructure
	httpServer     *http.Server
	httpMux        *http.ServeMux
	httpMiddleware []HTTPMiddleware // applied outermost-first; see ADR-030 Phase 1
	config         ManagerConfig

	// Optional dedicated health-port listener (#100). Operators set
	// cmd/semstreams's -health-port (or SEMSTREAMS_HEALTH_PORT env) to
	// bind a parallel listener serving ONLY /health and /healthz — a
	// lightweight surface for Docker/k8s probes that is independent of
	// the service-manager UI port. Zero means disabled (the default).
	// Both endpoints reuse the Manager's existing handleSystemHealth /
	// handleLiveness handlers; no separate health logic.
	healthServer     *http.Server
	serverGeneration *lifecyclejoin.Generation
	healthGeneration *lifecyclejoin.Generation
	serverShutdown   *lifecyclejoin.Operation
	healthShutdown   *lifecyclejoin.Operation

	// Track if we're the instance managing HTTP
	isHTTPManager bool

	// Config management
	natsClient    *natsclient.Client
	configManager *config.Manager
	dependencies  *Dependencies // Store full dependencies for mandatory services
}

// NewServiceManager creates a new service manager
func NewServiceManager(registry *Registry) *Manager {
	m := &Manager{
		registry: registry,
		services: make(map[string]Service),
		// config will be set when Manager is created as a service
	}
	// Initialize BaseService for registry/factory functionality
	m.BaseService = NewBaseServiceWithOptions("service-manager-registry", nil)
	return m
}

// ConfigureFromServices configures Manager directly from services config
// This replaces the old pattern where Manager was a service itself
func (m *Manager) ConfigureFromServices(services map[string]types.ServiceConfig, deps *Dependencies) error {
	m.mu.RLock()
	sealed := m.sealed
	m.mu.RUnlock()
	if sealed {
		return &CompositionSealedError{Operation: "configure", Name: "service-manager"}
	}

	resolved, err := ResolveServiceConfigs(services)
	if err != nil {
		return fmt.Errorf("resolve service configs: %w", err)
	}
	for _, name := range []string{"service-manager", "component-manager"} {
		if !resolved[name].Enabled {
			return &MandatoryServiceDisabledError{Name: name}
		}
	}

	// Use the injected logger if available
	logger := slog.Default()
	if deps != nil && deps.Logger != nil {
		logger = deps.Logger
	}

	var cfg ManagerConfig
	if err := decodeStrictServiceJSON(resolved["service-manager"].Config, &cfg); err != nil {
		return fmt.Errorf("parse service-manager config: %w", err)
	}
	if cfg.HTTPPort == 0 {
		cfg.HTTPPort = 8080
	}
	if cfg.ServerInfo.Title == "" {
		cfg.ServerInfo.Title = "SemStreams API"
	}
	if cfg.ServerInfo.Description == "" {
		cfg.ServerInfo.Description = "Flow-based programming framework API"
	}
	if cfg.ServerInfo.Version == "" {
		cfg.ServerInfo.Version = "0.7.0"
	}
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("validate service-manager config: %w", err)
	}

	names := make([]string, 0, len(resolved))
	for name := range resolved {
		names = append(names, name)
	}
	sort.Strings(names)

	// A configured identity may not alias a fixed/prebuilt instance. Perform
	// this check before invoking any constructor so a collision leaves both the
	// manager state and external constructor effects untouched.
	m.mu.RLock()
	if m.sealed {
		m.mu.RUnlock()
		return &CompositionSealedError{Operation: "configure", Name: "service-manager"}
	}
	for _, name := range names {
		if name == "service-manager" {
			continue
		}
		if _, exists := m.services[name]; exists {
			m.mu.RUnlock()
			return &DuplicateServiceError{Name: name}
		}
	}
	m.mu.RUnlock()

	constructed := make([]admittedService, 0, len(names))
	for _, name := range names {
		serviceConfig := resolved[name]
		if name == "service-manager" || !serviceConfig.Enabled {
			continue
		}
		constructor, exists := m.registry.Constructor(name)
		if !exists {
			return fmt.Errorf("create configured service %s: no constructor registered for service %s", name, name)
		}
		instance, err := constructor(serviceConfig.Config, deps)
		if err != nil {
			return fmt.Errorf("create configured service %s: %w", name, err)
		}
		constructed = append(constructed, admittedService{name: name, service: instance})
	}

	// Commit the fully validated composition atomically. Repeat the seal and
	// collision checks because another pre-start writer may have raced the
	// constructor staging above.
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sealed {
		return &CompositionSealedError{Operation: "configure", Name: "service-manager"}
	}
	for _, name := range names {
		if name == "service-manager" {
			continue
		}
		if _, exists := m.services[name]; exists {
			return &DuplicateServiceError{Name: name}
		}
	}

	m.config = cfg
	if deps != nil {
		m.dependencies = deps
		if deps.NATSClient != nil {
			m.natsClient = deps.NATSClient
		}
		if deps.Manager != nil {
			m.configManager = deps.Manager
		}
	}
	m.bootServiceConfigs = cloneResolvedServiceConfigs(resolved)
	for _, admitted := range constructed {
		m.services[admitted.name] = admitted.service
		m.order = append(m.order, admitted.name)
	}

	logger.Debug("Manager configured",
		"http_port", m.config.HTTPPort,
		"swagger_ui", m.config.SwaggerUI)
	return nil
}

// RegisterConstructor registers a service constructor with the given name
// RegisterConstructor removed - use registry.Register() directly

// CreateService creates a service instance using the registered constructor
func (m *Manager) CreateService(name string, rawConfig json.RawMessage, deps *Dependencies) (Service, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sealed {
		return nil, &CompositionSealedError{Operation: "create", Name: name}
	}

	// Check if service already exists
	if _, exists := m.services[name]; exists {
		return nil, &DuplicateServiceError{Name: name}
	}

	constructor, exists := m.registry.Constructor(name)
	if !exists {
		return nil, fmt.Errorf("no constructor registered for service %s", name)
	}

	service, err := constructor(rawConfig, deps)
	if err != nil {
		return nil, fmt.Errorf("failed to create service %s: %w", name, err)
	}

	// Store the service instance and track order
	m.services[name] = service
	m.order = append(m.order, name)

	return service, nil
}

// RegisterInstance admits a pre-built Service to the manager (composition-root
// wiring, as opposed to config-driven CreateService). Same map + order tracking
// CreateService uses, so StartAll/StopAll treat it identically.
func (m *Manager) RegisterInstance(name string, svc Service) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sealed {
		return &CompositionSealedError{Operation: "register", Name: name}
	}
	if _, exists := m.services[name]; exists {
		return &DuplicateServiceError{Name: name}
	}
	m.services[name] = svc
	m.order = append(m.order, name)
	return nil
}

// GetService returns a service instance by name
func (m *Manager) GetService(name string) (Service, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	service, exists := m.services[name]
	return service, exists
}

// GetAllServices returns all registered service instances
func (m *Manager) GetAllServices() map[string]Service {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Return a copy to avoid race conditions
	result := make(map[string]Service)
	for name, service := range m.services {
		result[name] = service
	}
	return result
}

// ListConstructors returns all registered constructor names
func (m *Manager) ListConstructors() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var names []string
	for name := range m.registry.Constructors() {
		names = append(names, name)
	}
	return names
}

// HasConstructor checks if a constructor is registered
func (m *Manager) HasConstructor(name string) bool {
	_, exists := m.registry.Constructor(name)
	return exists
}

// mandatoryServices lists services that must always exist
var mandatoryServices = []string{
	"component-manager", // Always needed to manage components
}

// StartAll starts all registered service instances and the HTTP server
func (m *Manager) StartAll(ctx context.Context) error {
	if ctx == nil {
		return errs.WrapInvalid(errs.ErrInvalidData, "Manager", "StartAll", "nil context")
	}

	// Use the injected logger from BaseService if available
	logger := m.logger
	if logger == nil {
		logger = slog.Default()
	}

	// Create mandatory services if they don't exist
	if err := m.createMandatoryServices(logger); err != nil {
		return fmt.Errorf("create mandatory services: %w", err)
	}

	services, err := m.sealComposition()
	if err != nil {
		return err
	}

	// No route or OpenAPI surface is built until the complete identity set is
	// fixed. A later Start failure changes lifecycle state, not composition.
	logger.Debug("Manager.StartAll: Initializing HTTP infrastructure")
	if err := m.initializeHTTPInfrastructure(); err != nil {
		return fmt.Errorf("initialize HTTP infrastructure: %w", err)
	}

	logger.Debug("Manager.StartAll: Beginning service startup sequence", "service_count", len(services))

	// Start all services (Manager is no longer in this list)
	for _, admitted := range services {
		name, service := admitted.name, admitted.service
		logger.Debug("Manager.StartAll: Starting service", "name", name, "type", fmt.Sprintf("%T", service))
		if err := service.Start(ctx); err != nil {
			logger.Error("Manager.StartAll: Failed to start service", "name", name, "error", err)
			return fmt.Errorf("failed to start service %s: %w", name, err)
		}
		logger.Debug("Manager.StartAll: Service started successfully", "name", name)
	}

	// There is deliberately NO post-start catalog-retention pass here. Its
	// entire justified class — a bucket created dirty during this boot's own
	// startup — is reconciled AT CREATION inside each component's Start by the
	// bucket acquisition seam (natsclient.EnsureFrameworkBucket), earlier and
	// more precisely than a sweep could; a seam failure fails that component's
	// Start, which the component-start barrier turns into a failed boot before
	// completeHTTPSetup brings the surface up. The one class the seam cannot
	// reach (a catalog bucket unused by this composition) is
	// covered by the pre-start legacy-drift backstop in WireGraphRuntime.

	// Now that all services are started, register their HTTP handlers and start the server
	logger.Debug("Manager.StartAll: Completing HTTP setup with service handlers")
	if err := m.completeHTTPSetup(ctx); err != nil {
		return fmt.Errorf("complete HTTP setup: %w", err)
	}
	logger.Info("Manager HTTP server started", "port", m.config.HTTPPort)

	// Start health publishing loop (publishes to health.service.{name})
	go m.publishHealthLoop(ctx)

	logger.Info("Manager.StartAll: All services started", "count", len(services))
	return nil
}

type admittedService struct {
	name    string
	service Service
}

func (m *Manager) sealComposition() ([]admittedService, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.sealed {
		return nil, &CompositionSealedError{Operation: "start", Name: "service-manager"}
	}
	for name, configured := range m.bootServiceConfigs {
		if name == "service-manager" || !configured.Enabled {
			continue
		}
		if _, exists := m.services[name]; !exists {
			return nil, fmt.Errorf("enabled configured service %s was not constructed", name)
		}
	}
	if _, exists := m.services["component-manager"]; !exists {
		return nil, fmt.Errorf("mandatory service component-manager was not constructed")
	}

	identities := make([]string, 0, len(m.services))
	for name := range m.services {
		identities = append(identities, name)
	}
	sort.Strings(identities)
	m.sealedServices = append([]string(nil), identities...)
	m.sealed = true

	services := make([]admittedService, 0, len(m.order))
	for _, name := range m.order {
		service, exists := m.services[name]
		if exists {
			services = append(services, admittedService{name: name, service: service})
		}
	}
	return services, nil
}

// publishHealthLoop publishes service health to JetStream every 5s.
// Each service's health is published to health.service.{name} for granular filtering.
// Gracefully handles NATS being unavailable - skips publish, doesn't block.
//
// Fires an immediate first publish before entering the tick loop so the HEALTH
// JetStream stream is seeded as soon as Manager.StartAll completes. Otherwise
// fresh clients connecting between T+0 and T+5s see an empty stream under
// last_per_subject delivery and time out waiting for `service_health` /
// `component_health` envelopes (e2e dataflow flake — see
// project_websocket_flake_diagnosis).
func (m *Manager) publishHealthLoop(ctx context.Context) {
	m.publishServiceHealth(ctx) // seed stream immediately
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.publishServiceHealth(ctx)
		}
	}
}

// publishServiceHealth publishes health for each service to NATS JetStream.
func (m *Manager) publishServiceHealth(ctx context.Context) {
	// Graceful fallback: skip if NATS unavailable
	if m.natsClient == nil {
		return
	}

	m.mu.RLock()
	services := make(map[string]Service, len(m.services))
	for name, svc := range m.services {
		services[name] = svc
	}
	m.mu.RUnlock()

	timestamp := time.Now().UnixMilli()

	for name, svc := range services {
		data, err := json.Marshal(map[string]any{
			"timestamp": timestamp,
			"name":      name,
			"status":    svc.Status().String(),
			"health":    svc.Health(),
		})
		if err != nil {
			continue
		}

		// Publish to health.service.{name} for granular filtering
		subject := "health.service." + name
		_ = m.natsClient.PublishToStream(ctx, subject, data)
	}
}

// createMandatoryServices creates mandatory services if they don't already exist
func (m *Manager) createMandatoryServices(logger *slog.Logger) error {
	for _, serviceName := range mandatoryServices {
		// Check if service already exists
		m.mu.RLock()
		_, exists := m.services[serviceName]
		m.mu.RUnlock()

		if exists {
			logger.Debug("Mandatory service already exists", "service", serviceName)
			continue
		}

		// Use stored dependencies if available, otherwise create minimal deps
		deps := m.dependencies
		if deps == nil {
			deps = &Dependencies{
				NATSClient: m.natsClient,
				Manager:    m.configManager,
				Logger:     logger,
			}
		}

		// Create the mandatory service with empty config
		logger.Info("Creating mandatory service", "service", serviceName)
		if _, err := m.CreateService(serviceName, json.RawMessage("{}"), deps); err != nil {
			return fmt.Errorf("failed to create mandatory service %s: %w", serviceName, err)
		}

		logger.Info("Mandatory service created successfully", "service", serviceName)
	}

	return nil
}

// StopAll stops all registered service instances in reverse order and the HTTP server
func (m *Manager) StopAll(ctx context.Context) error {
	if ctx == nil {
		return errs.WrapInvalid(errs.ErrInvalidData, "Manager", "StopAll", "nil context")
	}
	// Use injected logger with operation context
	logger := m.logger
	if logger == nil {
		logger = slog.Default()
	}
	logger = logger.With("operation", "services-shutdown")

	m.mu.Lock()
	// Create reverse order slice for shutdown
	reverseOrder := make([]string, len(m.order))
	for i := len(m.order) - 1; i >= 0; i-- {
		reverseOrder[len(m.order)-1-i] = m.order[i]
	}

	// Copy services map for safe access
	services := make(map[string]Service, len(m.services))
	for name, service := range m.services {
		services[name] = service
	}
	m.mu.Unlock()

	logger.Debug("Starting service shutdown sequence",
		"count", len(services),
		"order", reverseOrder,
	)
	overallStart := time.Now()

	var errors []error
	// Stop services in reverse order of registration
	for _, name := range reverseOrder {
		if service, exists := services[name]; exists {
			serviceStart := time.Now()
			logger.Debug("Stopping service", "service", name)

			if err := service.Stop(ctx); err != nil && !stderrors.Is(err, ErrAlreadyStopped) {
				logger.Error("Service stop failed",
					"service", name,
					"duration_ms", time.Since(serviceStart).Milliseconds(),
					"error", err,
				)
				errors = append(errors, fmt.Errorf("failed to stop service %s: %w", name, err))
			} else {
				logger.Debug("Service stopped successfully",
					"service", name,
					"duration_ms", time.Since(serviceStart).Milliseconds(),
				)
			}
		}
	}

	if err := m.BaseService.Stop(ctx); err != nil {
		errors = append(errors, err)
	}
	if err := m.stopRuntimeServers(ctx); err != nil {
		logger.Error("HTTP listeners stop failed", "error", err)
		errors = append(errors, err)
	}

	logger.Debug("Service shutdown sequence completed",
		"duration_ms", time.Since(overallStart).Milliseconds(),
		"error_count", len(errors),
	)

	// Return combined errors if any
	if len(errors) > 0 {
		return fmt.Errorf("stop errors: %w", stderrors.Join(errors...))
	}
	m.mu.Lock()
	m.services = make(map[string]Service)
	m.order = nil
	m.mu.Unlock()
	return nil
}

// GetHealthyServices returns a list of healthy services
func (m *Manager) GetHealthyServices() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var healthy []string
	for name, service := range m.services {
		if service.IsHealthy() {
			healthy = append(healthy, name)
		}
	}
	return healthy
}

// GetUnhealthyServices returns a list of unhealthy services
func (m *Manager) GetUnhealthyServices() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var unhealthy []string
	for name, service := range m.services {
		if !service.IsHealthy() {
			unhealthy = append(unhealthy, name)
		}
	}
	return unhealthy
}

// GetServiceStatus returns the status of a specific service
func (m *Manager) GetServiceStatus(name string) (any, error) {
	m.mu.RLock()
	service, exists := m.services[name]
	m.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("service %s not found", name)
	}

	return service.Status(), nil
}

// GetAllServiceStatus returns the status of all services
func (m *Manager) GetAllServiceStatus() map[string]any {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[string]any)
	for name, service := range m.services {
		result[name] = service.Status()
	}
	return result
}

// hasNATSAccess checks if Manager has access to NATS client
func (m *Manager) hasNATSAccess() bool {
	return m.natsClient != nil && m.natsClient.GetConnection() != nil
}

// Start starts the Manager HTTP server if configured
func (m *Manager) Start(ctx context.Context) error {
	if ctx == nil {
		return errs.WrapInvalid(errs.ErrInvalidData, "Manager", "Start", "nil context")
	}
	// First start the base service
	if err := m.BaseService.Start(ctx); err != nil {
		return err
	}
	// HTTP server is now started in StartAll(), not here
	// This prevents duplicate startup attempts
	return nil
}

// Stop stops the Manager HTTP server
func (m *Manager) Stop(ctx context.Context) error {
	if ctx == nil {
		return errs.WrapInvalid(errs.ErrInvalidData, "Manager", "Stop", "nil context")
	}
	baseErr := m.BaseService.Stop(ctx)
	serverErr := m.stopRuntimeServers(ctx)
	return stderrors.Join(baseErr, serverErr)
}

// UseHTTPMiddleware appends product-supplied middleware to the
// chain wrapped around every HTTP route the framework registers
// (component handlers, gateway-component handlers, system endpoints
// like /openapi.json and /health). Order is outermost-first: the
// first middleware passed in this call (or across calls) is the
// outermost wrapper.
//
// This is the framework's only HTTP middleware seam. The framework
// ships zero default middleware; auth, request logging, panic
// recovery, rate limiting, and CORS are product policy. Products
// pairing identity-aware middleware with the beta.22 helpers should
// call agenticdispatch.WithIdentity from inside the middleware so
// agenticdispatch.IdentityFromRequest picks it up downstream.
//
// Must be called before the HTTP server starts (i.e., before the
// owning service.Manager.Start*HTTP* path runs). Calls after the
// server is already up are ignored with a warning log — late
// registration would be silently dropped at the chain since the
// http.Server's Handler field is set at boot, and a warning is the
// closest we can get to "you didn't get what you asked for" without
// a panic. Multiple calls before boot are concatenated in call
// order, so a product may layer middleware progressively.
func (m *Manager) UseHTTPMiddleware(mws ...HTTPMiddleware) {
	if len(mws) == 0 {
		return
	}
	// Lock protects against late registration racing concurrent boot
	// completion (completeHTTPSetup / startHTTPServer take the same
	// mutex when reading m.httpMiddleware to build the wrapped handler).
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.httpServer != nil {
		logger := m.logger
		if logger == nil {
			logger = slog.Default()
		}
		logger.Warn("UseHTTPMiddleware called after HTTP server started; ignoring",
			"middleware_count", len(mws))
		return
	}
	m.httpMiddleware = append(m.httpMiddleware, mws...)
}

// buildHTTPHandler returns the framework's HTTP mux wrapped with
// the product-supplied middleware chain. Both completeHTTPSetup and
// the deprecated startHTTPServer call this when assigning to
// http.Server.Handler so the wrapping happens in one place — and so
// tests can assert the wired chain runs without booting a real
// listener. Caller must hold m.mu.
func (m *Manager) buildHTTPHandler() http.Handler {
	return chainMiddleware(m.httpMux, m.httpMiddleware)
}

// initializeHTTPInfrastructure creates the HTTP mux and registers system endpoints only
// This is called early in StartAll before services are created
func (m *Manager) initializeHTTPInfrastructure() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.httpMux != nil {
		// Already initialized - this is not an error condition
		// Multiple calls to StartAll should be idempotent
		return nil
	}

	// Create HTTP mux
	m.httpMux = http.NewServeMux()

	// Register system endpoints (health, liveness, readiness)
	// These don't depend on services being created
	m.registerSystemEndpoints()

	return nil
}

// completeHTTPSetup registers service handlers and starts the HTTP server
// This is called after all services have been started
func (m *Manager) completeHTTPSetup(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.httpMux == nil {
		return fmt.Errorf("HTTP infrastructure not initialized")
	}

	if m.httpServer != nil {
		return fmt.Errorf("HTTP server already started")
	}

	// Register service handlers (services now exist and are started!)
	if err := m.registerServiceHandlers(); err != nil {
		return fmt.Errorf("failed to register service handlers: %w", err)
	}

	// Register OpenAPI endpoints
	m.registerOpenAPIEndpoints()

	serverCtx, serverCancel := context.WithCancel(ctx)
	serveDone := make(chan struct{})
	m.serverGeneration = lifecyclejoin.NewGeneration(serverCancel, func() { <-serveDone })
	m.serverShutdown = lifecyclejoin.NewOperation()

	// Create HTTP server with lifecycle context as BaseContext.
	// All request contexts (r.Context()) derive from this, so they cancel on shutdown.
	m.httpServer = &http.Server{
		Addr:    ":" + strconv.Itoa(m.config.HTTPPort),
		Handler: m.buildHTTPHandler(),
		BaseContext: func(_ net.Listener) context.Context {
			return serverCtx
		},
		ReadTimeout:  m.config.ResolvedHTTPReadTimeout(),
		WriteTimeout: m.config.ResolvedHTTPWriteTimeout(),
		IdleTimeout:  60 * time.Second,
	}

	// Start server in background
	// Capture server reference before goroutine to avoid race condition
	server := m.httpServer
	go func() {
		defer close(serveDone)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			m.logger.Error("HTTP server error", "error", err)
		}
	}()

	return nil
}

// startHTTPServer starts the HTTP server and registers all service handlers
// DEPRECATED: Use initializeHTTPInfrastructure() and completeHTTPSetup() instead
func (m *Manager) startHTTPServer(ctx context.Context) error {
	if ctx == nil {
		return errs.WrapInvalid(errs.ErrInvalidData, "Manager", "startHTTPServer", "nil context")
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.httpServer != nil {
		return fmt.Errorf("HTTP server already started")
	}

	// Create HTTP mux
	m.httpMux = http.NewServeMux()

	// Register system endpoints (health, liveness, readiness)
	m.registerSystemEndpoints()

	// Register service handlers
	if err := m.registerServiceHandlers(); err != nil {
		return fmt.Errorf("failed to register service handlers: %w", err)
	}

	// Register OpenAPI endpoints
	m.registerOpenAPIEndpoints()

	serverCtx, serverCancel := context.WithCancel(ctx)
	serveDone := make(chan struct{})
	m.serverGeneration = lifecyclejoin.NewGeneration(serverCancel, func() { <-serveDone })
	m.serverShutdown = lifecyclejoin.NewOperation()

	// Create HTTP server with lifecycle context as BaseContext
	m.httpServer = &http.Server{
		Addr:    ":" + strconv.Itoa(m.config.HTTPPort),
		Handler: m.buildHTTPHandler(),
		BaseContext: func(_ net.Listener) context.Context {
			return serverCtx
		},
		ReadTimeout:  m.config.ResolvedHTTPReadTimeout(),
		WriteTimeout: m.config.ResolvedHTTPWriteTimeout(),
		IdleTimeout:  60 * time.Second,
	}

	// Start server in background
	// Capture server reference before goroutine to avoid race condition
	server := m.httpServer
	go func() {
		defer close(serveDone)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			m.logger.Error("HTTP server error", "error", err)
		}
	}()

	return nil
}

// StartHealthListener binds a dedicated /health + /healthz listener on
// the given port. Intended for Docker / Kubernetes probes that want a
// stable port independent of the service-manager UI's HTTPPort (#100).
// Port 0 is a no-op (disabled — the default for the -health-port flag).
//
// The listener serves the SAME handler functions as the main HTTP mux's
// /health and /healthz routes; it reads m.services / m.natsClient under
// the same m.mu locks. Failure to bind logs at Warn level but does NOT
// fail the boot — the main /health on HTTPPort is the authoritative
// health surface; this dedicated listener is convenience-only.
//
// Idempotent: calling twice with the same non-zero port returns an
// error rather than re-binding; callers should call StopHealthListener
// first if they need to switch ports at runtime.
func (m *Manager) StartHealthListener(ctx context.Context, port int) error {
	if ctx == nil {
		return errs.WrapInvalid(errs.ErrInvalidData, "Manager", "StartHealthListener", "nil context")
	}
	if port == 0 {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.healthServer != nil {
		return fmt.Errorf("health listener already started; call StopHealthListener before re-binding")
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", m.handleSystemHealth)
	mux.HandleFunc("/healthz", m.handleLiveness)
	healthCtx, healthCancel := context.WithCancel(ctx)
	serveDone := make(chan struct{})
	m.healthGeneration = lifecyclejoin.NewGeneration(healthCancel, func() { <-serveDone })
	m.healthShutdown = lifecyclejoin.NewOperation()

	m.healthServer = &http.Server{
		Addr:    ":" + strconv.Itoa(port),
		Handler: mux,
		BaseContext: func(_ net.Listener) context.Context {
			return healthCtx
		},
		// Health probes are short-lived; keep timeouts tight so a
		// misbehaving probe client can't pin a goroutine.
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
		IdleTimeout:  30 * time.Second,
	}

	server := m.healthServer
	logger := m.logger
	if logger == nil {
		logger = slog.Default()
	}
	logger.Info("Starting dedicated health listener", "port", port, "routes", []string{"/health", "/healthz"})
	go func() {
		defer close(serveDone)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Warn("dedicated health listener error", "port", port, "error", err)
		}
	}()
	return nil
}

func (m *Manager) stopRuntimeServers(ctx context.Context) error {
	m.mu.RLock()
	generation := m.serverGeneration
	healthGeneration := m.healthGeneration
	serverShutdown := m.serverShutdown
	healthShutdown := m.healthShutdown
	httpServer := m.httpServer
	healthServer := m.healthServer
	m.mu.RUnlock()
	if generation == nil && healthGeneration == nil {
		return nil
	}
	var stopErr error
	if healthGeneration != nil {
		healthGeneration.Cancel()
		shutdownErr := healthShutdown.Run(ctx, func(ctx context.Context) error {
			if healthServer == nil {
				return nil
			}
			if err := healthServer.Shutdown(ctx); err != nil {
				return fmt.Errorf("shutdown dedicated health listener: %w", err)
			}
			return nil
		})
		joinErr := healthGeneration.Stop(ctx, nil, nil)
		healthErr := stderrors.Join(shutdownErr, joinErr)
		stopErr = stderrors.Join(stopErr, healthErr)
		if healthErr == nil {
			m.mu.Lock()
			if m.healthServer == healthServer {
				m.healthServer = nil
				m.healthGeneration = nil
				m.healthShutdown = nil
			}
			m.mu.Unlock()
		}
	}
	if generation != nil {
		generation.Cancel()
		shutdownErr := serverShutdown.Run(ctx, func(ctx context.Context) error {
			if httpServer == nil {
				return nil
			}
			if err := httpServer.Shutdown(ctx); err != nil {
				return fmt.Errorf("failed to shutdown HTTP server: %w", err)
			}
			return nil
		})
		joinErr := generation.Stop(ctx, nil, nil)
		serverErr := stderrors.Join(shutdownErr, joinErr)
		stopErr = stderrors.Join(stopErr, serverErr)
		if serverErr == nil {
			m.mu.Lock()
			if m.httpServer == httpServer {
				m.httpServer = nil
				m.httpMux = nil
				m.serverGeneration = nil
				m.serverShutdown = nil
			}
			m.mu.Unlock()
		}
	}
	return stopErr
}

// StopHealthListener gracefully shuts down the dedicated health-port
// listener if one was started. No-op when the listener is not running.
func (m *Manager) StopHealthListener(ctx context.Context) error {
	if ctx == nil {
		return errs.WrapInvalid(errs.ErrInvalidData, "Manager", "StopHealthListener", "nil context")
	}
	m.mu.RLock()
	server := m.healthServer
	generation := m.healthGeneration
	shutdown := m.healthShutdown
	m.mu.RUnlock()

	if server == nil || generation == nil {
		return nil
	}

	logger := m.logger
	if logger == nil {
		logger = slog.Default()
	}
	generation.Cancel()
	shutdownErr := shutdown.Run(ctx, func(ctx context.Context) error {
		return server.Shutdown(ctx)
	})
	joinErr := generation.Stop(ctx, nil, nil)
	err := stderrors.Join(shutdownErr, joinErr)
	if err != nil {
		logger.Warn("dedicated health listener shutdown failed", "error", err)
		return fmt.Errorf("shutdown dedicated health listener: %w", err)
	}
	m.mu.Lock()
	if m.healthServer == server {
		m.healthServer = nil
		m.healthGeneration = nil
		m.healthShutdown = nil
	}
	m.mu.Unlock()
	return nil
}

// stopHTTPServer stops the HTTP server gracefully
func (m *Manager) stopHTTPServer(ctx context.Context) error {
	if ctx == nil {
		return errs.WrapInvalid(errs.ErrInvalidData, "Manager", "stopHTTPServer", "nil context")
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.httpServer == nil {
		return nil
	}

	// Use injected logger with operation context
	logger := m.logger
	if logger == nil {
		logger = slog.Default()
	}
	logger = logger.With("operation", "http-shutdown")

	logger.Debug("Starting HTTP server shutdown")
	start := time.Now()

	// Gracefully shutdown the server
	if err := m.httpServer.Shutdown(ctx); err != nil {
		logger.Error("HTTP server shutdown failed",
			"duration_ms", time.Since(start).Milliseconds(),
			"error", err,
		)
		return fmt.Errorf("failed to shutdown HTTP server: %w", err)
	}

	logger.Debug("HTTP server shutdown completed",
		"duration_ms", time.Since(start).Milliseconds(),
	)

	m.httpServer = nil
	m.httpMux = nil
	return nil
}

// registerServiceHandlers registers HTTP handlers for all services that implement HTTPHandler
func (m *Manager) registerServiceHandlers() error {
	for _, name := range m.sealedServices {
		service, exists := m.services[name]
		if !exists {
			continue
		}
		if handler, ok := service.(HTTPHandler); ok {
			// Convert service name to URL prefix (e.g., "component-manager" -> "/components")
			prefix := "/" + m.serviceNameToPrefix(name)
			handler.RegisterHTTPHandlers(prefix, m.httpMux)
		}
	}

	// Also register gateway component handlers
	if err := m.registerComponentHandlers(); err != nil {
		return fmt.Errorf("failed to register component handlers: %w", err)
	}

	return nil
}

// registerComponentHandlers registers HTTP handlers for gateway components
func (m *Manager) registerComponentHandlers() error {
	// Get ComponentManager from services
	cmService, exists := m.services["component-manager"]
	if !exists {
		// ComponentManager not started yet, skip gateway registration
		return nil
	}

	cm, ok := cmService.(*ComponentManager)
	if !ok {
		// ComponentManager not the expected type (e.g., mock in tests), skip gateway registration
		return nil
	}

	// Get all managed components
	components := cm.GetManagedComponents()

	// Register gateway components
	for name, mc := range components {
		// Check if component implements gateway.Gateway interface
		if gateway, ok := mc.Component.(interface {
			RegisterHTTPHandlers(prefix string, mux *http.ServeMux)
		}); ok {
			// Use component instance name as URL prefix
			prefix := "/" + name
			gateway.RegisterHTTPHandlers(prefix, m.httpMux)
			m.logger.Debug("Registered gateway component HTTP handlers",
				"component", name,
				"prefix", prefix)
		}
	}

	return nil
}

// registerOpenAPIEndpoints registers OpenAPI documentation endpoints
func (m *Manager) registerOpenAPIEndpoints() {
	// Serve OpenAPI JSON specification
	m.httpMux.HandleFunc("/openapi.json", m.handleOpenAPISpec)

	// Serve Swagger UI if enabled
	if m.config.SwaggerUI {
		m.httpMux.HandleFunc("/docs", m.handleSwaggerUI)
	}
}

// handleOpenAPISpec serves the combined OpenAPI specification
func (m *Manager) handleOpenAPISpec(w http.ResponseWriter, _ *http.Request) {
	spec := m.generateOpenAPIDocument()

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	if err := json.NewEncoder(w).Encode(spec); err != nil {
		http.Error(w, "Failed to encode OpenAPI spec", http.StatusInternalServerError)
		return
	}
}

// handleSwaggerUI serves a simple Swagger UI
func (m *Manager) handleSwaggerUI(w http.ResponseWriter, _ *http.Request) {
	html := `<!DOCTYPE html>
<html>
<head>
    <title>SemStreams API Documentation</title>
    <link rel="stylesheet" type="text/css" href="https://unpkg.com/swagger-ui-dist@3.52.5/swagger-ui.css" />
</head>
<body>
    <div id="swagger-ui"></div>
    <script src="https://unpkg.com/swagger-ui-dist@3.52.5/swagger-ui-bundle.js"></script>
    <script>
        SwaggerUIBundle({
            url: '/openapi.json',
            dom_id: '#swagger-ui',
            presets: [SwaggerUIBundle.presets.apis, SwaggerUIBundle.presets.standalone],
        });
    </script>
</body>
</html>`

	w.Header().Set("Content-Type", "text/html")
	_, _ = w.Write([]byte(html))
}

// generateOpenAPIDocument creates a combined OpenAPI document from all services
func (m *Manager) generateOpenAPIDocument() *OpenAPIDocument {
	doc := &OpenAPIDocument{
		OpenAPI: "3.0.0",
		Info:    m.config.ServerInfo,
		Servers: []ServerSpec{
			{
				URL:         fmt.Sprintf("http://localhost:%d", m.config.HTTPPort),
				Description: "Development server",
			},
		},
		Paths: make(map[string]PathSpec),
		Tags:  make([]TagSpec, 0),
	}

	// Snapshot services under read lock to avoid data race
	m.mu.RLock()
	services := make([]admittedService, 0, len(m.sealedServices))
	for _, name := range m.sealedServices {
		if svc, exists := m.services[name]; exists {
			services = append(services, admittedService{name: name, service: svc})
		}
	}
	m.mu.RUnlock()

	// Collect specs from all services that implement HTTPHandler
	for _, admitted := range services {
		name, svc := admitted.name, admitted.service
		if handler, ok := svc.(HTTPHandler); ok {
			serviceSpec := handler.OpenAPISpec()
			if serviceSpec != nil {
				// Merge paths with service prefix
				prefix := "/" + m.serviceNameToPrefix(name)
				for path, pathSpec := range serviceSpec.Paths {
					fullPath := prefix + path
					doc.Paths[fullPath] = pathSpec
				}

				// Merge tags
				for _, tag := range serviceSpec.Tags {
					doc.Tags = append(doc.Tags, tag)
				}
			}
		}
	}

	// Generate schemas from all registered specs (superset including component specs)
	schemas := make(map[string]any)
	seen := make(map[reflect.Type]bool)

	for _, spec := range GetAllOpenAPISpecs() {
		for _, t := range spec.ResponseTypes {
			if !seen[t] {
				seen[t] = true
				schemas[TypeNameFromReflect(t)] = SchemaFromType(t)
			}
		}
		for _, t := range spec.RequestBodyTypes {
			if !seen[t] {
				seen[t] = true
				schemas[TypeNameFromReflect(t)] = SchemaFromType(t)
			}
		}
	}

	if len(schemas) > 0 {
		doc.Components = &ComponentsSpec{Schemas: schemas}
	}

	return doc
}

// serviceNameToPrefix converts service name to URL prefix
func (m *Manager) serviceNameToPrefix(serviceName string) string {
	switch serviceName {
	case "component-manager":
		return "components"
	case "message-logger":
		return "message-logger"
	case StorageObservabilityServiceName:
		// Hyphens preserved, following the message-logger precedent: the
		// default collapse would mount the storage report at
		// /storageobservability, which no operator would guess.
		return StorageObservabilityServiceName
	default:
		// Remove hyphens and use as-is
		return strings.ReplaceAll(serviceName, "-", "")
	}
}

// registerSystemEndpoints registers system-wide health endpoints
func (m *Manager) registerSystemEndpoints() {
	// System-wide health endpoints
	m.httpMux.HandleFunc("/health", m.handleSystemHealth)
	m.httpMux.HandleFunc("/healthz", m.handleLiveness)
	m.httpMux.HandleFunc("/readyz", m.handleReadiness)

	// Service discovery endpoints
	m.httpMux.HandleFunc("/services", m.handleServiceList)
	m.httpMux.HandleFunc("/services/health", m.handleServicesHealth)

	// Graph query endpoints (operator-facing, read-only)
	m.httpMux.HandleFunc("/graph/triples", m.handleGraphTriples)
}

// Removed buildServiceHealthMap and writeHealthResponse - using health.Status directly now

// handleSystemHealth returns aggregated system health
func (m *Manager) handleSystemHealth(w http.ResponseWriter, _ *http.Request) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Collect health status from all services
	var subStatuses []health.Status

	// Add service health statuses
	for _, service := range m.services {
		subStatuses = append(subStatuses, service.Health())
	}

	// Add NATS health as a sub-status
	if m.natsClient != nil {
		natsStatus := m.natsClient.GetStatus()
		if natsStatus.Status == natsclient.StatusConnected {
			subStatuses = append(subStatuses, health.NewHealthy("nats",
				fmt.Sprintf("Connected (RTT: %v)", natsStatus.RTT)))
		} else {
			subStatuses = append(subStatuses, health.NewUnhealthy("nats",
				fmt.Sprintf("Disconnected: %s (failures: %d)",
					natsStatus.Status.String(), natsStatus.FailureCount)))
		}
	}

	// Aggregate all health statuses
	systemHealth := health.Aggregate("system", subStatuses)

	// Set HTTP status code based on health
	w.Header().Set("Content-Type", "application/json")
	if systemHealth.IsUnhealthy() {
		w.WriteHeader(http.StatusServiceUnavailable)
	} else if systemHealth.IsDegraded() {
		w.WriteHeader(http.StatusOK) // 200 but degraded in body
	}

	// Write the health status directly as JSON
	if err := json.NewEncoder(w).Encode(systemHealth); err != nil {
		m.logger.Error("Failed to encode system health response", "error", err)
	}
}

// handleLiveness is a simple liveness probe
func (m *Manager) handleLiveness(w http.ResponseWriter, _ *http.Request) {
	// Simple liveness - is server running?
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("OK"))
}

// handleReadiness checks if all critical services are ready
func (m *Manager) handleReadiness(w http.ResponseWriter, _ *http.Request) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Check if all services are running
	ready := true
	for _, service := range m.services {
		if service.Status() != StatusRunning || !service.IsHealthy() {
			ready = false
			break
		}
	}

	if ready {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("READY"))
	} else {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("NOT READY"))
	}
}

// handleServiceList returns a list of all registered services
func (m *Manager) handleServiceList(w http.ResponseWriter, _ *http.Request) {
	m.mu.RLock()
	identities := append([]string(nil), m.sealedServices...)
	if len(identities) == 0 {
		for name := range m.services {
			identities = append(identities, name)
		}
		sort.Strings(identities)
	}
	instances := make(map[string]Service, len(m.services))
	for name, service := range m.services {
		instances[name] = service
	}
	boot := cloneResolvedServiceConfigs(m.bootServiceConfigs)
	configManager := m.configManager
	m.mu.RUnlock()

	services := make([]map[string]any, 0, len(identities))
	for _, name := range identities {
		service, exists := instances[name]
		if !exists {
			continue
		}
		services = append(services, map[string]any{
			"name":    name,
			"status":  service.Status().String(),
			"healthy": service.IsHealthy(),
		})
	}

	desired := boot
	if configManager != nil {
		current := configManager.GetConfig().Get()
		resolved, err := ResolveServiceConfigs(current.Services)
		if err != nil {
			http.Error(w, "Failed to resolve desired service configuration", http.StatusInternalServerError)
			return
		}
		desired = resolved
	}
	pending := pendingServiceChanges(boot, desired)

	response := map[string]any{
		"services":                services,
		"count":                   len(services),
		"restart_required":        len(pending) > 0,
		"pending_service_changes": pending,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		m.logger.Error("Failed to encode services list", "error", err)
	}
}

// handleServicesHealth returns detailed health information for all services
func (m *Manager) handleServicesHealth(w http.ResponseWriter, _ *http.Request) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Collect all service health statuses
	var serviceStatuses []health.Status
	for _, service := range m.services {
		serviceStatuses = append(serviceStatuses, service.Health())
	}

	// Create response with individual service health and overall status
	response := struct {
		Overall  health.Status   `json:"overall"`
		Services []health.Status `json:"services"`
	}{
		Overall:  health.Aggregate("services", serviceStatuses),
		Services: serviceStatuses,
	}

	// Set HTTP status code based on overall health
	w.Header().Set("Content-Type", "application/json")
	if response.Overall.IsUnhealthy() {
		w.WriteHeader(http.StatusServiceUnavailable)
	}

	if err := json.NewEncoder(w).Encode(response); err != nil {
		m.logger.Error("Failed to encode services health response", "error", err)
	}
}
