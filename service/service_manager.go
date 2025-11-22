package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/c360/semstreams/config"
	"github.com/c360/semstreams/health"
	"github.com/c360/semstreams/natsclient"
	"github.com/c360/semstreams/pkg/retry"
	"github.com/c360/semstreams/types"
)

// Manager manages service lifecycle using a provided registry.
// Services are explicitly registered and created from raw JSON configs.
type Manager struct {
	*BaseService // Embed BaseService to implement Service interface

	registry *Registry
	services map[string]Service
	order    []string // Track registration order for cleanup
	mu       sync.RWMutex

	// HTTP server infrastructure
	httpServer *http.Server
	httpMux    *http.ServeMux
	config     ManagerConfig

	// Track if we're the instance managing HTTP
	isHTTPManager bool

	// Config management
	natsClient    *natsclient.Client
	configManager *config.Manager
	configUpdates <-chan config.Update // Channel for config updates
	dependencies  *Dependencies        // Store full dependencies for mandatory services
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
	// Use the injected logger if available
	logger := slog.Default()
	if deps != nil && deps.Logger != nil {
		logger = deps.Logger
	}

	// Look for service-manager config
	smConfig, hasConfig := services["service-manager"]
	if !hasConfig || !smConfig.Enabled {
		logger.Debug("Manager: No service-manager config or disabled, using defaults")
		// Use defaults
		m.config = ManagerConfig{
			HTTPPort:  8080,
			SwaggerUI: false,
			ServerInfo: InfoSpec{
				Title:       "SemStreams API",
				Description: "Flow-based programming framework API",
				Version:     "0.7.0",
			},
		}
	} else {
		// Parse the config
		var cfg ManagerConfig
		if len(smConfig.Config) > 0 {
			if err := json.Unmarshal(smConfig.Config, &cfg); err != nil {
				return fmt.Errorf("parse service-manager config: %w", err)
			}
		}

		// Apply defaults
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

		// Validate configuration
		if err := cfg.Validate(); err != nil {
			return fmt.Errorf("validate service-manager config: %w", err)
		}

		m.config = cfg
	}

	// Store dependencies
	if deps != nil {
		m.dependencies = deps // Store full dependencies for mandatory services
		if deps.NATSClient != nil {
			m.natsClient = deps.NATSClient
		}
		if deps.Manager != nil {
			m.configManager = deps.Manager
			// Subscribe to service config changes
			m.configUpdates = deps.Manager.OnChange("services.*")
		}
	}

	// Create BaseService for lifecycle management
	if m.BaseService == nil {
		m.BaseService = NewBaseServiceWithOptions(
			"service-manager",
			nil,
			WithLogger(deps.Logger),
			WithMetrics(deps.MetricsRegistry),
		)
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

	// Check if service already exists
	if _, exists := m.services[name]; exists {
		return nil, fmt.Errorf("service %s already created", name)
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
	// Use the injected logger from BaseService if available
	logger := m.logger
	if logger == nil {
		logger = slog.Default()
	}

	// Initialize HTTP infrastructure (but don't start listening yet)
	logger.Debug("Manager.StartAll: Initializing HTTP infrastructure")
	if err := m.initializeHTTPInfrastructure(); err != nil {
		return fmt.Errorf("initialize HTTP infrastructure: %w", err)
	}

	// Create mandatory services if they don't exist
	if err := m.createMandatoryServices(logger); err != nil {
		return fmt.Errorf("create mandatory services: %w", err)
	}

	m.mu.RLock()
	services := make(map[string]Service)
	for name, service := range m.services {
		services[name] = service
	}
	m.mu.RUnlock()

	logger.Debug("Manager.StartAll: Beginning service startup sequence", "service_count", len(services))

	// Start all services (Manager is no longer in this list)
	for name, service := range services {
		logger.Debug("Manager.StartAll: Starting service", "name", name, "type", fmt.Sprintf("%T", service))
		if err := service.Start(ctx); err != nil {
			logger.Error("Manager.StartAll: Failed to start service", "name", name, "error", err)
			return fmt.Errorf("failed to start service %s: %w", name, err)
		}
		logger.Debug("Manager.StartAll: Service started successfully", "name", name)
	}

	// Now that all services are started, register their HTTP handlers and start the server
	logger.Debug("Manager.StartAll: Completing HTTP setup with service handlers")
	if err := m.completeHTTPSetup(); err != nil {
		return fmt.Errorf("complete HTTP setup: %w", err)
	}
	logger.Info("Manager HTTP server started", "port", m.config.HTTPPort)

	logger.Info("Manager.StartAll: All services started", "count", len(services))
	return nil
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
func (m *Manager) StopAll(timeout time.Duration) error {
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
		"timeout", timeout,
		"order", reverseOrder,
	)
	overallStart := time.Now()

	var errors []error
	// Stop services in reverse order of registration
	for _, name := range reverseOrder {
		if service, exists := services[name]; exists {
			serviceStart := time.Now()
			logger.Debug("Stopping service", "service", name)

			if err := service.Stop(timeout); err != nil {
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

	// Clear the registry
	m.mu.Lock()
	m.services = make(map[string]Service)
	m.order = nil
	m.mu.Unlock()

	// Stop the HTTP server if running
	// This was missing and causing containers to not shutdown cleanly!
	if m.isHTTPManager {
		logger.Debug("Stopping HTTP server")
		if err := m.stopHTTPServer(); err != nil {
			logger.Error("HTTP server stop failed", "error", err)
			errors = append(errors, fmt.Errorf("failed to stop HTTP server: %w", err))
		}
	}

	logger.Debug("Service shutdown sequence completed",
		"duration_ms", time.Since(overallStart).Milliseconds(),
		"error_count", len(errors),
	)

	// Return combined errors if any
	if len(errors) > 0 {
		return fmt.Errorf("stop errors: %v", errors)
	}
	return nil
}

// StartService creates and starts a single service if not already running
func (m *Manager) StartService(ctx context.Context, name string, rawConfig json.RawMessage, deps *Dependencies) error {
	// Use the injected logger from BaseService if available
	logger := m.logger
	if logger == nil {
		logger = slog.Default()
	}

	// Check if service already exists
	m.mu.RLock()
	_, exists := m.services[name]
	m.mu.RUnlock()

	if exists {
		// Service already exists - check if it's running
		logger.Debug("Service already exists", "service", name)
		// Note: We can't easily check if a service is "running" without a Status() method
		// For now, assume if it exists, it's running
		return nil
	}

	// Create the service
	logger.Info("Creating service", "service", name)
	service, err := m.CreateService(name, rawConfig, deps)
	if err != nil {
		return fmt.Errorf("failed to create service %s: %w", name, err)
	}

	// Start the service with retry for resilience
	logger.Info("Starting service", "service", name)

	// Use Quick retry config for service startup
	// Services may have dependencies that aren't ready yet
	retryConfig := retry.Quick() // 10 attempts over ~1 second
	startErr := retry.Do(ctx, retryConfig, func() error {
		if err := service.Start(ctx); err != nil {
			logger.Debug("Service start attempt failed, will retry",
				"service", name,
				"error", err)
			return err
		}
		return nil
	})

	if startErr != nil {
		// Remove from registry if start fails after all retries
		m.RemoveService(name)
		return fmt.Errorf("failed to start service %s after retries: %w", name, startErr)
	}

	logger.Info("Service started successfully", "service", name)
	return nil
}

// StopService stops and removes a single service
func (m *Manager) StopService(name string, timeout time.Duration) error {
	// Use the injected logger from BaseService if available
	logger := m.logger
	if logger == nil {
		logger = slog.Default()
	}

	// Check if service exists
	m.mu.RLock()
	service, exists := m.services[name]
	m.mu.RUnlock()

	if !exists {
		logger.Debug("Service not found", "service", name)
		return nil // Not an error - service already stopped
	}

	// Check if it's a mandatory service
	for _, mandatoryName := range mandatoryServices {
		if name == mandatoryName {
			logger.Warn("Cannot stop mandatory service", "service", name)
			return fmt.Errorf("cannot stop mandatory service %s", name)
		}
	}

	// Stop the service
	logger.Info("Stopping service", "service", name)
	if err := service.Stop(timeout); err != nil {
		logger.Error("Failed to stop service", "service", name, "error", err)
		// Continue with removal even if stop fails - service might be stuck
	}

	// Remove from registry
	m.RemoveService(name)
	logger.Info("Service stopped and removed", "service", name)
	return nil
}

// RemoveService removes a service instance
func (m *Manager) RemoveService(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.services[name]; exists {
		delete(m.services, name)

		// Remove from order tracking
		for i, n := range m.order {
			if n == name {
				m.order = append(m.order[:i], m.order[i+1:]...)
				break
			}
		}
	}
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

// watchConfigUpdates monitors for configuration changes from Manager
func (m *Manager) watchConfigUpdates(ctx context.Context) {
	// Keep track of previous configs to detect changes
	var previousConfigs types.ServiceConfigs

	for {
		select {
		case update, ok := <-m.configUpdates:
			if !ok {
				// Channel closed
				return
			}

			// Process the update
			fullConfig := update.Config.Get()
			currentConfigs := fullConfig.Services

			// Compare with previous configs to find changes
			if previousConfigs != nil {
				m.processServiceConfigChanges(previousConfigs, currentConfigs)
			}

			// Update previous configs for next iteration
			previousConfigs = currentConfigs

		case <-ctx.Done():
			return
		}
	}
}

// processServiceConfigChanges compares old and new configs and applies changes
func (m *Manager) processServiceConfigChanges(oldConfigs, newConfigs types.ServiceConfigs) {
	// Check each service for changes
	for serviceName, newConfig := range newConfigs {
		oldConfig, existed := oldConfigs[serviceName]

		if !existed {
			// New service added
			slog.Info("New service configuration detected",
				"service", serviceName)
			// Start the new service if it's enabled
			if newConfig.Enabled {
				// Use stored dependencies if available
				deps := m.dependencies
				if deps == nil {
					deps = &Dependencies{
						NATSClient: m.natsClient,
						Manager:    m.configManager,
						Logger:     m.logger,
					}
				}
				if err := m.StartService(context.Background(), serviceName, newConfig.Config, deps); err != nil {
					slog.Error("Failed to start new service", "service", serviceName, "error", err)
				}
			}
			continue
		}

		// Compare configs - check if Config field changed or Enabled state changed
		if !bytes.Equal(oldConfig.Config, newConfig.Config) || oldConfig.Enabled != newConfig.Enabled {
			// Handle enable/disable
			if oldConfig.Enabled != newConfig.Enabled {
				if newConfig.Enabled {
					slog.Info("Service enabled in config", "service", serviceName)
					// Start service if not running
					// Use stored dependencies if available
					deps := m.dependencies
					if deps == nil {
						deps = &Dependencies{
							NATSClient: m.natsClient,
							Manager:    m.configManager,
							Logger:     m.logger,
						}
					}
					if err := m.StartService(context.Background(), serviceName, newConfig.Config, deps); err != nil {
						slog.Error("Failed to start service", "service", serviceName, "error", err)
					}
				} else {
					slog.Info("Service disabled in config", "service", serviceName)
					// Stop service if running
					if err := m.StopService(serviceName, 5*time.Second); err != nil {
						slog.Error("Failed to stop service", "service", serviceName, "error", err)
					}
				}
			}
			// Apply config changes if Config field changed
			if !bytes.Equal(oldConfig.Config, newConfig.Config) {
				m.applyServiceConfigChange(serviceName, newConfig.Config)
			}
		}
	}

	// Check for removed services
	for serviceName := range oldConfigs {
		if _, exists := newConfigs[serviceName]; !exists {
			slog.Info("Service configuration removed",
				"service", serviceName)
			// Stop the service if it's running
			if err := m.StopService(serviceName, 5*time.Second); err != nil {
				slog.Error("Failed to stop removed service", "service", serviceName, "error", err)
			}
		}
	}
}

// applyServiceConfigChange applies configuration changes to a service
func (m *Manager) applyServiceConfigChange(serviceName string, newConfig json.RawMessage) {
	// Get service instance
	service, exists := m.GetService(serviceName)
	if !exists {
		slog.Warn("Configuration change for unknown service",
			"service", serviceName)
		return
	}

	// Check if service supports runtime configuration
	runtimeConfigurable, ok := service.(RuntimeConfigurable)
	if !ok {
		slog.Info("Service does not support runtime configuration, restart required",
			"service", serviceName)
		return
	}

	// Parse new config to map for validation
	var newConfigMap map[string]any
	if err := json.Unmarshal(newConfig, &newConfigMap); err != nil {
		slog.Error("Failed to parse new service configuration",
			"service", serviceName,
			"error", err)
		return
	}

	// Validate the configuration change
	if err := runtimeConfigurable.ValidateConfigUpdate(newConfigMap); err != nil {
		slog.Error("Invalid service configuration update",
			"service", serviceName,
			"error", err)
		return
	}

	// Apply the validated configuration change
	if err := runtimeConfigurable.ApplyConfigUpdate(newConfigMap); err != nil {
		slog.Error("Failed to apply service configuration update",
			"service", serviceName,
			"error", err)
		return
	}

	slog.Info("Successfully applied service configuration update",
		"service", serviceName)
}

// hasRuntimeConfigSupport checks if a service supports runtime configuration
func (m *Manager) hasRuntimeConfigSupport(serviceName string) bool {
	service, exists := m.GetService(serviceName)
	if !exists {
		return false
	}

	_, ok := service.(RuntimeConfigurable)
	return ok
}

// GetServiceRuntimeConfig returns current runtime configuration for a service
func (m *Manager) GetServiceRuntimeConfig(serviceName string) (map[string]any, error) {
	service, exists := m.GetService(serviceName)
	if !exists {
		return nil, fmt.Errorf("service %s not found", serviceName)
	}

	runtimeConfigurable, ok := service.(RuntimeConfigurable)
	if !ok {
		return nil, fmt.Errorf("service %s does not support runtime configuration", serviceName)
	}

	return runtimeConfigurable.GetRuntimeConfig(), nil
}

// Start starts the Manager HTTP server if configured
func (m *Manager) Start(ctx context.Context) error {
	// First start the base service
	if err := m.BaseService.Start(ctx); err != nil {
		return err
	}

	// Start watching for config updates if channel is available
	// This only applies to HTTP manager instances since they have lifecycle management
	if m.isHTTPManager && m.configUpdates != nil {
		m.waitGroup.Add(1) // Track the goroutine for proper shutdown
		go func() {
			defer m.waitGroup.Done()
			m.watchConfigUpdates(ctx)
		}()
		slog.Info("Config watching enabled for Manager")
	}

	// HTTP server is now started in StartAll(), not here
	// This prevents duplicate startup attempts
	return nil
}

// Stop stops the Manager HTTP server
func (m *Manager) Stop(timeout time.Duration) error {
	// Config watching is now handled by Manager, no need to stop it here

	// Stop HTTP server if running
	if m.isHTTPManager {
		if err := m.stopHTTPServer(); err != nil {
			return err
		}
	}

	// Stop base service
	return m.BaseService.Stop(timeout)
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
func (m *Manager) completeHTTPSetup() error {
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

	// Create HTTP server
	m.httpServer = &http.Server{
		Addr:         ":" + strconv.Itoa(m.config.HTTPPort),
		Handler:      m.httpMux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start server in background
	// Capture server reference before goroutine to avoid race condition
	server := m.httpServer
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			m.logger.Error("HTTP server error", "error", err)
		}
	}()

	return nil
}

// startHTTPServer starts the HTTP server and registers all service handlers
// DEPRECATED: Use initializeHTTPInfrastructure() and completeHTTPSetup() instead
func (m *Manager) startHTTPServer() error {
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

	// Create HTTP server
	m.httpServer = &http.Server{
		Addr:         ":" + strconv.Itoa(m.config.HTTPPort),
		Handler:      m.httpMux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start server in background
	// Capture server reference before goroutine to avoid race condition
	server := m.httpServer
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			m.logger.Error("HTTP server error", "error", err)
		}
	}()

	return nil
}

// stopHTTPServer stops the HTTP server gracefully
func (m *Manager) stopHTTPServer() error {
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

	logger.Debug("Starting HTTP server shutdown", "timeout", "5s")
	start := time.Now()

	// Create context for graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

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
	for name, service := range m.services {
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
			m.logger.Info("Registered gateway component HTTP handlers",
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

	// Collect specs from all services that implement HTTPHandler
	for name, service := range m.services {
		if handler, ok := service.(HTTPHandler); ok {
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

	return doc
}

// serviceNameToPrefix converts service name to URL prefix
func (m *Manager) serviceNameToPrefix(serviceName string) string {
	switch serviceName {
	case "component-manager":
		return "components"
	case "message-logger":
		return "message-logger"
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
	defer m.mu.RUnlock()

	services := make([]map[string]any, 0, len(m.services))
	for name, service := range m.services {
		services = append(services, map[string]any{
			"name":    name,
			"status":  service.Status().String(),
			"healthy": service.IsHealthy(),
		})
	}

	response := map[string]any{
		"services": services,
		"count":    len(services),
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
