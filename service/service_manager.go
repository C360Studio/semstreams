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
	"sync/atomic"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/config"
	"github.com/c360studio/semstreams/health"
	"github.com/c360studio/semstreams/internal/lifecyclecleanup"
	"github.com/c360studio/semstreams/metric"
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
	serviceOutcomes    map[string]*serviceInvocationOutcome
	startupMetrics     *startupMetricWriter
	bootCommitted      atomic.Bool
	stopping           bool

	// HTTP server infrastructure
	httpServer     *http.Server
	httpMux        *http.ServeMux
	diagnosticMux  *http.ServeMux
	httpRoutes     atomic.Pointer[http.ServeMux]
	httpMiddleware []HTTPMiddleware // applied outermost-first; see ADR-030 Phase 1
	config         ManagerConfig

	// Optional dedicated health-port listener (#100). Operators set
	// cmd/semstreams's -health-port (or SEMSTREAMS_HEALTH_PORT env) to
	// bind a parallel listener serving ONLY /health and /healthz — a
	// lightweight surface for Docker/k8s probes that is independent of
	// the service-manager UI port. Zero means disabled (the default).
	// Both endpoints reuse the Manager's existing handleSystemHealth /
	// handleLiveness handlers; no separate health logic.
	httpListener    net.Listener
	httpCancel      context.CancelFunc
	httpServeDone   chan struct{}
	httpUsed        bool
	httpStopping    bool
	httpTerminal    bool
	healthServer    *http.Server
	healthListener  net.Listener
	healthCancel    context.CancelFunc
	healthServeDone chan struct{}
	healthUsed      bool
	healthStopping  bool
	healthTerminal  bool

	healthPublisherCancel   context.CancelFunc
	healthPublisherDone     chan struct{}
	healthPublisherUsed     bool
	healthPublisherStopping bool
	healthPublisherTerminal bool
	startupMetricsServer    *metric.Server
	startupMetricsService   *Metrics

	// Causal observation points used only by startup-boundary tests.
	testCommitPrepared     chan<- struct{}
	testCommitRelease      <-chan struct{}
	testSharedHTTPBound    chan<- struct{}
	testMetricsBindRelease <-chan struct{}

	// Track if we're the instance managing HTTP
	isHTTPManager bool

	// Config management
	natsClient    *natsclient.Client
	configManager *config.Manager
	dependencies  *Dependencies // Store full dependencies for mandatory services
}

type serviceInvocationOutcome struct {
	service        Service
	startInvoked   bool
	startCompleted bool
	startErr       error
	stopInvoked    bool
	stopCompleted  bool
	stopErr        error
}

type startupUnitCounts struct {
	Admitted              int `json:"admitted"`
	LifecycleParticipants int `json:"lifecycle_participants"`
	StartsInvoked         int `json:"starts_invoked"`
	StartsCompleted       int `json:"starts_completed"`
	StartsFailed          int `json:"starts_failed"`
}

type serviceStartupCounts struct {
	Admitted        int `json:"admitted"`
	StartsInvoked   int `json:"starts_invoked"`
	StartsCompleted int `json:"starts_completed"`
	StartsFailed    int `json:"starts_failed"`
}

type startupSnapshot struct {
	Status     string               `json:"status"`
	Services   serviceStartupCounts `json:"services"`
	Components startupUnitCounts    `json:"components"`
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

func (m *Manager) bindConstructorDependencies(deps *Dependencies) *Dependencies {
	if deps == nil {
		return &Dependencies{ServiceManager: m}
	}
	bound := *deps
	bound.ServiceManager = m
	return &bound
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
		instance, err := constructor(serviceConfig.Config, m.bindConstructorDependencies(deps))
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
	retainedDeps := m.bindConstructorDependencies(deps)
	if deps != nil {
		m.dependencies = retainedDeps
		if retainedDeps.NATSClient != nil {
			m.natsClient = retainedDeps.NATSClient
		}
		if retainedDeps.Manager != nil {
			m.configManager = retainedDeps.Manager
		}
	} else {
		m.dependencies = retainedDeps
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
	m.mu.RLock()
	if m.sealed {
		m.mu.RUnlock()
		return nil, &CompositionSealedError{Operation: "create", Name: name}
	}

	// Check if service already exists
	if _, exists := m.services[name]; exists {
		m.mu.RUnlock()
		return nil, &DuplicateServiceError{Name: name}
	}
	m.mu.RUnlock()

	constructor, exists := m.registry.Constructor(name)
	if !exists {
		return nil, fmt.Errorf("no constructor registered for service %s", name)
	}

	service, err := constructor(rawConfig, m.bindConstructorDependencies(deps))
	if err != nil {
		return nil, fmt.Errorf("failed to create service %s: %w", name, err)
	}

	// Revalidate after construction because other pre-start writers may have
	// sealed composition or committed the same identity while the callback ran.
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sealed {
		return nil, &CompositionSealedError{Operation: "create", Name: name}
	}
	if _, exists := m.services[name]; exists {
		return nil, &DuplicateServiceError{Name: name}
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
	if err := ctx.Err(); err != nil {
		return errs.WrapInvalid(err, "Manager", "StartAll", "context already canceled")
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

	// The sealed identity set is fixed before the startup diagnostic surface is
	// built. A later Start failure changes lifecycle state, not composition.
	logger.Debug("Manager.StartAll: Initializing HTTP infrastructure")
	if err := m.initializeHTTPInfrastructure(); err != nil {
		return fmt.Errorf("initialize HTTP infrastructure: %w", err)
	}
	if err := m.initializeStartupMetricWriter(); err != nil {
		return fmt.Errorf("initialize startup metrics: %w", err)
	}
	if err := m.startHTTPRuntime(ctx); err != nil {
		cleanupErr := m.stopStartupMetricsServer(ctx)
		return stderrors.Join(fmt.Errorf("start HTTP diagnostics: %w", err), cleanupErr)
	}
	if m.testSharedHTTPBound != nil {
		close(m.testSharedHTTPBound)
		<-m.testMetricsBindRelease
	}
	if err := m.startStartupMetricsServer(ctx); err != nil {
		cleanupErr := m.cleanupDiagnosticBindFailure(ctx)
		return stderrors.Join(fmt.Errorf("start metrics diagnostics: %w", err), cleanupErr)
	}

	logger.Debug("Manager.StartAll: Beginning service startup sequence", "service_count", len(services))

	// Start all services (Manager is no longer in this list)
	for _, admitted := range services {
		name, service := admitted.name, admitted.service
		logger.Debug("Manager.StartAll: Starting service", "name", name, "type", fmt.Sprintf("%T", service))
		m.recordServiceStartInvoked(name)
		startErr := service.Start(ctx)
		m.recordServiceStartCompleted(name, startErr)
		if startErr != nil {
			logger.Error("Manager.StartAll: Failed to start service", "name", name, "error", startErr)
			return m.rollbackFailedStart(ctx, fmt.Errorf("failed to start service %s: %w", name, startErr))
		}
		logger.Debug("Manager.StartAll: Service started successfully", "name", name)
	}

	// There is deliberately NO post-start catalog-retention pass here. Its
	// entire justified class — a bucket created dirty during this boot's own
	// startup — is reconciled AT CREATION inside each component's Start by the
	// bucket acquisition seam (natsclient.EnsureFrameworkBucket), earlier and
	// more precisely than a sweep could; a seam failure fails that component's
	// Start, which the component-start barrier turns into a failed boot before
	// boot commitment exposes the complete route set. The one class the seam cannot
	// reach (a catalog bucket unused by this composition) is
	// covered by the pre-start legacy-drift backstop in WireGraphRuntime.

	// Build the complete route set off-path. The dispatcher remains pinned to
	// diagnostics until every later fallible Manager acquisition succeeds.
	logger.Debug("Manager.StartAll: Preparing complete HTTP route set")
	fullMux, err := m.prepareCompleteHTTPMux(ctx)
	if err != nil {
		return m.rollbackFailedStart(ctx, fmt.Errorf("complete HTTP setup: %w", err))
	}

	// Start health publishing loop (publishes to health.service.{name}).
	if err := m.startHealthPublisher(ctx); err != nil {
		return m.rollbackFailedStart(ctx, fmt.Errorf("start health publisher: %w", err))
	}
	m.commitStartup(fullMux)
	logger.Info("Manager HTTP server started", "port", m.config.HTTPPort)

	logger.Info("Manager.StartAll: All services started", "count", len(services))
	return nil
}

func (m *Manager) rollbackFailedStart(ctx context.Context, startErr error) error {
	rollbackErr := lifecyclecleanup.RollbackFailedStart(ctx, m.cleanupFailedStart)
	return stderrors.Join(startErr, rollbackErr)
}

type managerCleanupMode uint8

const (
	managerCleanupTerminal managerCleanupMode = iota
	managerCleanupFailedStart
)

func (m *Manager) cleanupFailedStart(ctx context.Context) error {
	return m.stopAll(ctx, managerCleanupFailedStart)
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

	m.serviceOutcomes = make(map[string]*serviceInvocationOutcome, len(m.order))
	for _, name := range m.order {
		m.serviceOutcomes[name] = &serviceInvocationOutcome{service: m.services[name]}
	}

	services := make([]admittedService, 0, len(m.order))
	for _, name := range m.order {
		service, exists := m.services[name]
		if exists {
			services = append(services, admittedService{name: name, service: service})
		}
	}
	return services, nil
}

func (m *Manager) recordServiceStartInvoked(name string) {
	m.mu.Lock()
	if outcome := m.serviceOutcomes[name]; outcome != nil {
		outcome.startInvoked = true
	}
	writer := m.startupMetrics
	m.mu.Unlock()
	writer.publishServices()
}

func (m *Manager) recordServiceStartCompleted(name string, startErr error) {
	m.mu.Lock()
	if outcome := m.serviceOutcomes[name]; outcome != nil {
		outcome.startCompleted = true
		outcome.startErr = startErr
	}
	writer := m.startupMetrics
	m.mu.Unlock()
	writer.publishServices()
}

func (m *Manager) serviceStartupCountsLocked() serviceStartupCounts {
	counts := serviceStartupCounts{Admitted: len(m.serviceOutcomes)}
	for _, outcome := range m.serviceOutcomes {
		if outcome.startInvoked {
			counts.StartsInvoked++
		}
		if outcome.startCompleted {
			counts.StartsCompleted++
		}
		if outcome.startErr != nil {
			counts.StartsFailed++
		}
	}
	return counts
}

func (m *Manager) serviceStartupCounts() serviceStartupCounts {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.serviceStartupCountsLocked()
}

func (m *Manager) initializeStartupMetricWriter() error {
	m.mu.RLock()
	var registry *metric.MetricsRegistry
	if m.dependencies != nil {
		registry = m.dependencies.MetricsRegistry
	}
	componentManager, _ := m.services["component-manager"].(*ComponentManager)
	metricsService, _ := m.services["metrics"].(*Metrics)
	if registry == nil && metricsService != nil {
		registry = metricsService.registry
	}
	m.mu.RUnlock()

	if registry != nil {
		componentSnapshot := func() startupUnitCounts { return startupUnitCounts{} }
		if componentManager != nil {
			componentSnapshot = componentManager.startupSnapshot
		}
		writer, err := newStartupMetricWriter(registry, m.serviceStartupCounts, componentSnapshot)
		if err != nil {
			return err
		}
		m.mu.Lock()
		m.startupMetrics = writer
		m.mu.Unlock()
		if componentManager != nil {
			componentManager.setStartupMetricWriter(writer)
		}
	}

	if metricsService == nil {
		return nil
	}
	server, err := metricsService.claimManagerServer()
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.startupMetricsServer = server
	m.startupMetricsService = metricsService
	m.mu.Unlock()
	return nil
}

func (m *Manager) startStartupMetricsServer(ctx context.Context) error {
	m.mu.RLock()
	server := m.startupMetricsServer
	service := m.startupMetricsService
	m.mu.RUnlock()
	if server == nil {
		return nil
	}
	if err := server.Start(ctx); err != nil {
		return err
	}
	service.setManagerServerHealthy(true)
	return nil
}

func (m *Manager) stopStartupMetricsServer(ctx context.Context) error {
	m.mu.Lock()
	server := m.startupMetricsServer
	service := m.startupMetricsService
	m.startupMetricsServer = nil
	m.startupMetricsService = nil
	m.mu.Unlock()
	if service != nil {
		service.setManagerServerHealthy(false)
	}
	if server == nil {
		return nil
	}
	return server.Stop(ctx)
}

func (m *Manager) cleanupDiagnosticBindFailure(ctx context.Context) error {
	m.beginStopping("")
	return stderrors.Join(
		m.stopRuntimeServersMode(ctx, managerCleanupFailedStart),
		m.stopStartupMetricsServer(ctx),
	)
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

func (m *Manager) startHealthPublisher(ctx context.Context) error {
	if ctx == nil {
		return errs.WrapInvalid(errs.ErrInvalidData, "Manager", "startHealthPublisher", "nil context")
	}
	if err := ctx.Err(); err != nil {
		return errs.WrapInvalid(err, "Manager", "startHealthPublisher", "context already canceled")
	}
	m.mu.Lock()
	if m.healthPublisherUsed {
		m.mu.Unlock()
		return errs.WrapFatal(errs.ErrAlreadyStarted, "Manager", "startHealthPublisher", "health publisher already used")
	}
	publisherCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	m.healthPublisherUsed = true
	m.healthPublisherCancel = cancel
	m.healthPublisherDone = done
	m.mu.Unlock()

	go func() {
		defer close(done)
		m.publishHealthLoop(publisherCtx)
	}()
	return nil
}

func (m *Manager) stopHealthPublisher(ctx context.Context) error {
	return m.stopHealthPublisherMode(ctx, managerCleanupTerminal)
}

func (m *Manager) stopHealthPublisherMode(ctx context.Context, mode managerCleanupMode) error {
	m.mu.Lock()
	if !m.healthPublisherUsed || m.healthPublisherTerminal {
		m.mu.Unlock()
		return nil
	}
	if m.healthPublisherStopping {
		m.mu.Unlock()
		return errs.WrapTransient(stderrors.New("health publisher stop already in progress"), "Manager", "stopHealthPublisher", "concurrent Stop is unsupported")
	}
	m.healthPublisherStopping = true
	cancel := m.healthPublisherCancel
	done := m.healthPublisherDone
	m.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	var stopErr error
	if done != nil {
		select {
		case <-done:
		default:
			select {
			case <-done:
			case <-ctx.Done():
				stopErr = errs.NewShutdownError("service-manager/health-publisher", errs.PhaseJoinRuntime, ctx.Err())
			}
		}
	}
	if ctxErr := ctx.Err(); ctxErr != nil && !stderrors.Is(stopErr, ctxErr) {
		stopErr = stderrors.Join(stopErr, errs.NewShutdownError("service-manager/health-publisher", errs.PhaseJoinRuntime, ctxErr))
	}
	m.mu.Lock()
	m.healthPublisherStopping = false
	if mode == managerCleanupTerminal || stopErr == nil {
		m.healthPublisherTerminal = true
		m.healthPublisherCancel = nil
		m.healthPublisherDone = nil
	}
	m.mu.Unlock()
	return stopErr
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
	return m.stopAll(ctx, managerCleanupTerminal)
}

func (m *Manager) stopAll(ctx context.Context, mode managerCleanupMode) error {
	// Use injected logger with operation context
	logger := m.logger
	if logger == nil {
		logger = slog.Default()
	}
	logger = logger.With("operation", "services-shutdown")

	m.mu.RLock()
	order := append([]string(nil), m.order...)
	m.mu.RUnlock()
	// Stop in exact reverse registration order.
	reverseOrder := make([]string, len(order))
	for i := len(order) - 1; i >= 0; i-- {
		reverseOrder[len(order)-1-i] = order[i]
	}

	m.beginStopping(firstServiceName(reverseOrder))

	m.mu.RLock()
	// Copy services map for safe access.
	services := make(map[string]Service, len(m.services))
	for name, service := range m.services {
		services[name] = service
	}
	m.mu.RUnlock()

	logger.Debug("Starting service shutdown sequence",
		"count", len(services),
		"order", reverseOrder,
	)
	overallStart := time.Now()

	var errors []error
	// Stop services in reverse order of registration
	for _, name := range reverseOrder {
		if service, exists := services[name]; exists {
			m.recordServiceStopInvoked(name)
			serviceStart := time.Now()
			logger.Debug("Stopping service", "service", name)

			stopErr := service.Stop(ctx)
			m.recordServiceStopCompleted(name, stopErr)
			if stopErr != nil && !stderrors.Is(stopErr, ErrAlreadyStopped) {
				logger.Error("Service stop failed",
					"service", name,
					"duration_ms", time.Since(serviceStart).Milliseconds(),
					"error", stopErr,
				)
				errors = append(errors, fmt.Errorf("failed to stop service %s: %w", name, stopErr))
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
	if err := m.stopHealthPublisherMode(ctx, mode); err != nil {
		errors = append(errors, err)
	}
	if err := m.stopRuntimeServersMode(ctx, mode); err != nil {
		logger.Error("HTTP listeners stop failed", "error", err)
		errors = append(errors, err)
	}
	if err := m.stopStartupMetricsServer(ctx); err != nil {
		logger.Error("Prometheus listener stop failed", "error", err)
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

func (m *Manager) recordServiceStopInvoked(name string) {
	m.mu.Lock()
	if outcome := m.serviceOutcomes[name]; outcome != nil {
		outcome.stopInvoked = true
	}
	m.mu.Unlock()
}

func firstServiceName(order []string) string {
	if len(order) == 0 {
		return ""
	}
	return order[0]
}

func (m *Manager) beginStopping(firstService string) {
	m.mu.Lock()
	m.stopping = true
	m.bootCommitted.Store(false)
	if outcome := m.serviceOutcomes[firstService]; outcome != nil {
		outcome.stopInvoked = true
	}
	m.mu.Unlock()
}

func (m *Manager) recordServiceStopCompleted(name string, stopErr error) {
	m.mu.Lock()
	if outcome := m.serviceOutcomes[name]; outcome != nil {
		outcome.stopCompleted = true
		outcome.stopErr = stopErr
	}
	m.mu.Unlock()
}

// GetHealthyServices returns a list of healthy services
func (m *Manager) GetHealthyServices() []string {
	m.mu.RLock()
	services := make(map[string]Service, len(m.services))
	for name, service := range m.services {
		services[name] = service
	}
	m.mu.RUnlock()

	var healthy []string
	for name, service := range services {
		if service.IsHealthy() {
			healthy = append(healthy, name)
		}
	}
	return healthy
}

// GetUnhealthyServices returns a list of unhealthy services
func (m *Manager) GetUnhealthyServices() []string {
	m.mu.RLock()
	services := make(map[string]Service, len(m.services))
	for name, service := range m.services {
		services[name] = service
	}
	m.mu.RUnlock()

	var unhealthy []string
	for name, service := range services {
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
	services := make(map[string]Service, len(m.services))
	for name, service := range m.services {
		services[name] = service
	}
	m.mu.RUnlock()

	result := make(map[string]any)
	for name, service := range services {
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
	publisherErr := m.stopHealthPublisher(ctx)
	serverErr := m.stopRuntimeServers(ctx)
	return stderrors.Join(baseErr, publisherErr, serverErr)
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
	// completion (startHTTPRuntime takes the same mutex while freezing
	// m.httpMiddleware into the server handler).
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.httpServer != nil || m.httpUsed {
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

// buildHTTPHandler returns the framework's HTTP dispatcher wrapped with
// the product-supplied middleware chain. startHTTPRuntime calls this when
// assigning http.Server.Handler so tests can assert the wired chain without
// booting a real listener. Caller must hold m.mu.
func (m *Manager) buildHTTPHandler() http.Handler {
	dispatch := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mux := m.diagnosticMux
		if m.bootCommitted.Load() {
			mux = m.httpRoutes.Load()
		}
		if mux == nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("NOT READY"))
			return
		}
		mux.ServeHTTP(w, r)
	})
	return chainMiddleware(dispatch, m.httpMiddleware)
}

// initializeHTTPInfrastructure creates the diagnostic mux after composition is
// sealed and before either Manager-owned diagnostic listener binds.
func (m *Manager) initializeHTTPInfrastructure() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.httpMux != nil {
		// The pre-acquisition setup helper is safely repeatable; the Manager's
		// StartAll lifecycle itself remains terminal and one-shot.
		return nil
	}

	startupMux := http.NewServeMux()
	m.registerDiagnosticEndpoints(startupMux)
	if service, exists := m.services["component-manager"]; exists {
		if cm, ok := service.(*ComponentManager); ok {
			cm.registerStartupHTTPHandlers("/components", startupMux)
		}
	}
	startupMux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("NOT READY"))
	})
	m.httpMux = startupMux
	m.diagnosticMux = startupMux

	return nil
}

// startHTTPRuntime synchronously binds the shared listener while the startup
// mux is active. Service and gateway routes remain unreachable until the
// complete mux is atomically promoted.
func (m *Manager) startHTTPRuntime(ctx context.Context) error {
	if ctx == nil {
		return errs.WrapInvalid(errs.ErrInvalidData, "Manager", "startHTTPRuntime", "nil context")
	}
	if err := ctx.Err(); err != nil {
		return errs.WrapInvalid(err, "Manager", "startHTTPRuntime", "context already canceled")
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.httpMux == nil || m.diagnosticMux == nil {
		return fmt.Errorf("HTTP infrastructure not initialized")
	}

	if m.httpUsed {
		return fmt.Errorf("HTTP server instance already used")
	}
	listener, err := net.Listen("tcp", ":"+strconv.Itoa(m.config.HTTPPort))
	if err != nil {
		return fmt.Errorf("bind HTTP listener: %w", err)
	}

	serverCtx, serverCancel := context.WithCancel(ctx)
	serveDone := make(chan struct{})

	// Create HTTP server with lifecycle context as BaseContext.
	// All request contexts (r.Context()) derive from this, so they cancel on shutdown.
	server := &http.Server{
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
	m.httpServer = server
	m.httpListener = listener
	m.httpCancel = serverCancel
	m.httpServeDone = serveDone
	m.httpUsed = true
	go func() {
		defer close(serveDone)
		if err := server.Serve(listener); err != nil && !stderrors.Is(err, http.ErrServerClosed) && !stderrors.Is(err, net.ErrClosed) {
			logger := m.logger
			if logger == nil {
				logger = slog.Default()
			}
			logger.Error("HTTP server error", "error", err)
		}
	}()

	return nil
}

// prepareCompleteHTTPMux constructs the complete route set off-path. The
// caller commits it only after every fallible boot operation succeeds.
func (m *Manager) prepareCompleteHTTPMux(ctx context.Context) (*http.ServeMux, error) {
	if ctx == nil {
		return nil, errs.WrapInvalid(errs.ErrInvalidData, "Manager", "prepareCompleteHTTPMux", "nil context")
	}
	if err := ctx.Err(); err != nil {
		return nil, errs.WrapInvalid(err, "Manager", "prepareCompleteHTTPMux", "context already canceled")
	}
	m.mu.RLock()
	started := m.httpUsed && m.httpServer != nil
	m.mu.RUnlock()
	if !started {
		return nil, fmt.Errorf("HTTP diagnostics not started")
	}

	fullMux := http.NewServeMux()
	m.registerSystemEndpoints(fullMux)
	if err := m.registerServiceHandlers(fullMux); err != nil {
		return nil, fmt.Errorf("failed to register service handlers: %w", err)
	}
	m.registerOpenAPIEndpoints(fullMux)
	return fullMux, nil
}

func (m *Manager) completeHTTPSetup(ctx context.Context) error {
	mux, err := m.prepareCompleteHTTPMux(ctx)
	if err != nil {
		return err
	}
	m.commitStartup(mux)
	return nil
}

func (m *Manager) commitStartup(mux *http.ServeMux) {
	if mux == nil {
		return
	}
	if m.testCommitPrepared != nil {
		close(m.testCommitPrepared)
		<-m.testCommitRelease
	}
	m.httpRoutes.Store(mux)
	m.mu.Lock()
	m.httpMux = mux
	m.mu.Unlock()
	m.bootCommitted.Store(true)
}

// StartHealthListener binds a dedicated /health + /healthz listener on
// the given port. Intended for Docker / Kubernetes probes that want a
// stable port independent of the service-manager UI's HTTPPort (#100).
// Port 0 is a no-op (disabled — the default for the -health-port flag).
//
// The listener serves the SAME handler functions as the main HTTP mux's
// /health and /healthz routes; it reads m.services / m.natsClient under
// the same m.mu locks. Bind failure is returned synchronously so the
// composition boundary can decide whether this convenience-only surface
// is required for boot.
//
// The listener is one-shot: calling twice, including after completed Stop,
// returns an error rather than re-binding this Manager instance.
func (m *Manager) StartHealthListener(ctx context.Context, port int) error {
	if ctx == nil {
		return errs.WrapInvalid(errs.ErrInvalidData, "Manager", "StartHealthListener", "nil context")
	}
	if err := ctx.Err(); err != nil {
		return errs.WrapInvalid(err, "Manager", "StartHealthListener", "context already canceled")
	}
	if port == 0 {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.healthUsed {
		return fmt.Errorf("health listener instance already used")
	}
	listener, err := net.Listen("tcp", ":"+strconv.Itoa(port))
	if err != nil {
		return fmt.Errorf("bind health listener: %w", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", m.handleSystemHealth)
	mux.HandleFunc("/healthz", m.handleLiveness)
	healthCtx, healthCancel := context.WithCancel(ctx)
	serveDone := make(chan struct{})

	server := &http.Server{
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

	m.healthServer = server
	m.healthListener = listener
	m.healthCancel = healthCancel
	m.healthServeDone = serveDone
	m.healthUsed = true
	logger := m.logger
	if logger == nil {
		logger = slog.Default()
	}
	logger.Info("Starting dedicated health listener", "port", port, "routes", []string{"/health", "/healthz"})
	go func() {
		defer close(serveDone)
		if err := server.Serve(listener); err != nil && !stderrors.Is(err, http.ErrServerClosed) && !stderrors.Is(err, net.ErrClosed) {
			logger.Warn("dedicated health listener error", "port", port, "error", err)
		}
	}()
	return nil
}

func (m *Manager) stopRuntimeServers(ctx context.Context) error {
	return m.stopRuntimeServersMode(ctx, managerCleanupTerminal)
}

func (m *Manager) stopRuntimeServersMode(ctx context.Context, mode managerCleanupMode) error {
	healthErr := m.stopHealthRuntimeMode(ctx, mode)
	httpErr := m.stopHTTPRuntimeMode(ctx, mode)
	return stderrors.Join(healthErr, httpErr)
}

// StopHealthListener gracefully shuts down the dedicated health-port
// listener if one was started. No-op when the listener is not running.
func (m *Manager) StopHealthListener(ctx context.Context) error {
	if ctx == nil {
		return errs.WrapInvalid(errs.ErrInvalidData, "Manager", "StopHealthListener", "nil context")
	}
	logger := m.logger
	if logger == nil {
		logger = slog.Default()
	}
	err := m.stopHealthRuntime(ctx)
	if err != nil {
		logger.Warn("dedicated health listener shutdown failed", "error", err)
		return fmt.Errorf("shutdown dedicated health listener: %w", err)
	}
	return nil
}

func (m *Manager) stopHealthRuntime(ctx context.Context) error {
	return m.stopHealthRuntimeMode(ctx, managerCleanupTerminal)
}

func (m *Manager) stopHealthRuntimeMode(ctx context.Context, mode managerCleanupMode) error {
	m.mu.Lock()
	if !m.healthUsed || m.healthTerminal {
		m.mu.Unlock()
		return nil
	}
	if m.healthStopping {
		m.mu.Unlock()
		return errs.WrapTransient(stderrors.New("health listener stop already in progress"), "Manager", "StopHealthListener", "concurrent Stop is unsupported")
	}
	m.healthStopping = true
	server, listener := m.healthServer, m.healthListener
	cancel, serveDone := m.healthCancel, m.healthServeDone
	m.mu.Unlock()

	stopErr := shutdownManagerHTTPRuntime(ctx, "service-manager/health-listener", server, listener, cancel, serveDone, mode)
	m.mu.Lock()
	m.healthStopping = false
	if mode == managerCleanupTerminal || stopErr == nil {
		m.healthTerminal = true
		m.healthServer = nil
		m.healthListener = nil
		m.healthCancel = nil
		m.healthServeDone = nil
	}
	m.mu.Unlock()
	return stopErr
}

func (m *Manager) stopHTTPRuntime(ctx context.Context) error {
	return m.stopHTTPRuntimeMode(ctx, managerCleanupTerminal)
}

func (m *Manager) stopHTTPRuntimeMode(ctx context.Context, mode managerCleanupMode) error {
	m.mu.Lock()
	if !m.httpUsed || m.httpTerminal {
		m.mu.Unlock()
		return nil
	}
	if m.httpStopping {
		m.mu.Unlock()
		return errs.WrapTransient(stderrors.New("HTTP listener stop already in progress"), "Manager", "stopRuntimeServers", "concurrent Stop is unsupported")
	}
	m.httpStopping = true
	server, listener := m.httpServer, m.httpListener
	cancel, serveDone := m.httpCancel, m.httpServeDone
	m.mu.Unlock()

	stopErr := shutdownManagerHTTPRuntime(ctx, "service-manager/http-listener", server, listener, cancel, serveDone, mode)
	m.mu.Lock()
	m.httpStopping = false
	if mode == managerCleanupTerminal || stopErr == nil {
		m.httpTerminal = true
		m.httpServer = nil
		m.httpListener = nil
		m.httpCancel = nil
		m.httpServeDone = nil
		m.httpMux = nil
		m.httpRoutes.Store(nil)
	}
	m.mu.Unlock()
	return stopErr
}

func shutdownManagerHTTPRuntime(
	ctx context.Context,
	owner string,
	server *http.Server,
	listener net.Listener,
	cancel context.CancelFunc,
	serveDone <-chan struct{},
	mode managerCleanupMode,
) error {
	var shutdownErr error
	if server != nil {
		shutdownErr = errs.NewShutdownError(owner, errs.PhaseShutdownListener, server.Shutdown(ctx))
	}
	if mode == managerCleanupFailedStart && shutdownErr != nil {
		if cancel != nil {
			cancel()
		}
		return shutdownErr
	}
	if listener != nil {
		if err := listener.Close(); err != nil && !stderrors.Is(err, net.ErrClosed) {
			shutdownErr = stderrors.Join(shutdownErr, errs.NewShutdownError(owner, errs.PhaseShutdownListener, err))
		}
	}
	if shutdownErr != nil && server != nil {
		if err := server.Close(); err != nil && !stderrors.Is(err, http.ErrServerClosed) {
			shutdownErr = stderrors.Join(shutdownErr, errs.NewShutdownError(owner, errs.PhaseShutdownListener, err))
		}
	}
	if cancel != nil {
		cancel()
	}

	var joinErr error
	if serveDone != nil {
		select {
		case <-serveDone:
		default:
			select {
			case <-serveDone:
			case <-ctx.Done():
				joinErr = errs.NewShutdownError(owner, errs.PhaseJoinRuntime, ctx.Err())
			}
		}
	}
	if ctxErr := ctx.Err(); ctxErr != nil && !stderrors.Is(shutdownErr, ctxErr) && !stderrors.Is(joinErr, ctxErr) {
		joinErr = errs.NewShutdownError(owner, errs.PhaseJoinRuntime, ctxErr)
	}
	return stderrors.Join(shutdownErr, joinErr)
}

func attributeShutdownError(owner string, phase errs.ShutdownPhase, err error) error {
	if err == nil {
		return nil
	}
	var shutdownErr *errs.ShutdownError
	if stderrors.As(err, &shutdownErr) {
		return err
	}
	return errs.NewShutdownError(owner, phase, err)
}

// registerServiceHandlers registers HTTP handlers for all services that implement HTTPHandler
func (m *Manager) registerServiceHandlers(mux *http.ServeMux) error {
	m.mu.RLock()
	identities := append([]string(nil), m.sealedServices...)
	services := make(map[string]Service, len(m.serviceOutcomes))
	for name, outcome := range m.serviceOutcomes {
		if outcome != nil && outcome.service != nil {
			services[name] = outcome.service
		}
	}
	m.mu.RUnlock()
	for _, name := range identities {
		service, exists := services[name]
		if !exists {
			continue
		}
		if handler, ok := service.(HTTPHandler); ok {
			// Convert service name to URL prefix (e.g., "component-manager" -> "/components")
			prefix := "/" + m.serviceNameToPrefix(name)
			handler.RegisterHTTPHandlers(prefix, mux)
		}
	}

	// Also register gateway component handlers
	if err := m.registerComponentHandlers(mux); err != nil {
		return fmt.Errorf("failed to register component handlers: %w", err)
	}

	return nil
}

// registerComponentHandlers registers HTTP handlers for gateway components
func (m *Manager) registerComponentHandlers(mux *http.ServeMux) error {
	// Get ComponentManager from services
	m.mu.RLock()
	cmService, exists := m.services["component-manager"]
	m.mu.RUnlock()
	if !exists {
		// ComponentManager not started yet, skip gateway registration
		return nil
	}

	cm, ok := cmService.(*ComponentManager)
	if !ok {
		// ComponentManager not the expected type (e.g., mock in tests), skip gateway registration
		return nil
	}

	return cm.withComponents(func(components map[string]*component.ManagedComponent) error {
		for name, managed := range components {
			if gateway, ok := managed.Component.(interface {
				RegisterHTTPHandlers(prefix string, mux *http.ServeMux)
			}); ok {
				prefix := "/" + name
				gateway.RegisterHTTPHandlers(prefix, mux)
				m.logger.Debug("Registered gateway component HTTP handlers",
					"component", name,
					"prefix", prefix)
			}
		}
		return nil
	})
}

// registerOpenAPIEndpoints registers OpenAPI documentation endpoints
func (m *Manager) registerOpenAPIEndpoints(mux *http.ServeMux) {
	// Serve OpenAPI JSON specification
	mux.HandleFunc("/openapi.json", m.handleOpenAPISpec)

	// Serve Swagger UI if enabled
	if m.config.SwaggerUI {
		mux.HandleFunc("/docs", m.handleSwaggerUI)
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
func (m *Manager) registerDiagnosticEndpoints(mux *http.ServeMux) {
	// System-wide health endpoints
	mux.HandleFunc("/health", m.handleSystemHealth)
	mux.HandleFunc("/healthz", m.handleLiveness)
	mux.HandleFunc("/readyz", m.handleReadiness)

	// Service discovery endpoints
	mux.HandleFunc("/services", m.handleServiceList)
	mux.HandleFunc("/services/health", m.handleServicesHealth)
}

func (m *Manager) registerSystemEndpoints(mux *http.ServeMux) {
	m.registerDiagnosticEndpoints(mux)

	// Graph query endpoints (operator-facing, read-only)
	mux.HandleFunc("/graph/triples", m.handleGraphTriples)
}

// Removed buildServiceHealthMap and writeHealthResponse - using health.Status directly now

// handleSystemHealth returns aggregated system health
func (m *Manager) handleSystemHealth(w http.ResponseWriter, _ *http.Request) {
	m.mu.RLock()
	services := make([]Service, 0, len(m.services))
	for _, service := range m.services {
		services = append(services, service)
	}
	natsClient := m.natsClient
	m.mu.RUnlock()

	// Collect health status from all services
	var subStatuses []health.Status

	// Add service health statuses
	for _, service := range services {
		subStatuses = append(subStatuses, service.Health())
	}

	// Add NATS health as a sub-status
	if natsClient != nil {
		natsStatus := natsClient.GetStatus()
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

// handleReadiness reports the direct process-local manager observation. The
// exact response bodies are compatibility-frozen for existing probes.
func (m *Manager) handleReadiness(w http.ResponseWriter, _ *http.Request) {
	if m.currentStartupSnapshot().Status == "ready" {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("READY"))
	} else {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("NOT READY"))
	}
}

func (m *Manager) currentStartupSnapshot() startupSnapshot {
	m.mu.RLock()
	sealed := m.sealed
	committed := m.bootCommitted.Load()
	stopping := m.stopping
	serviceCounts := m.serviceStartupCountsLocked()
	services := make(map[string]Service, len(m.serviceOutcomes))
	serviceStopInvoked := false
	for name, outcome := range m.serviceOutcomes {
		if outcome != nil && outcome.service != nil {
			services[name] = outcome.service
		}
		if outcome != nil && outcome.stopInvoked {
			serviceStopInvoked = true
		}
	}
	m.mu.RUnlock()

	var componentCounts startupUnitCounts
	componentManagerService, hasComponentManager := services["component-manager"]
	componentManager, concreteComponentManager := componentManagerService.(*ComponentManager)
	if concreteComponentManager {
		componentCounts = componentManager.startupSnapshot()
	}

	snapshot := startupSnapshot{Services: serviceCounts, Components: componentCounts}
	servicesComplete := sealed && hasComponentManager && serviceCounts.Admitted > 0 &&
		serviceCounts.StartsInvoked == serviceCounts.Admitted &&
		serviceCounts.StartsCompleted == serviceCounts.Admitted
	componentsComplete := !concreteComponentManager ||
		(componentCounts.StartsInvoked == componentCounts.LifecycleParticipants &&
			componentCounts.StartsCompleted == componentCounts.LifecycleParticipants)
	if serviceCounts.StartsFailed > 0 || componentCounts.StartsFailed > 0 {
		snapshot.Status = "failed"
		return snapshot
	}
	if stopping || serviceStopInvoked || (concreteComponentManager && componentManager.startupStopBegun()) {
		snapshot.Status = "stopping"
		return snapshot
	}
	if !servicesComplete || !componentsComplete {
		snapshot.Status = "starting"
		return snapshot
	}
	if !committed {
		snapshot.Status = "not_ready"
		return snapshot
	}

	for _, service := range services {
		if service.Status() != StatusRunning || !service.IsHealthy() {
			snapshot.Status = "not_ready"
			return snapshot
		}
	}
	if concreteComponentManager {
		healthByName := componentManager.GetComponentHealth()
		if len(healthByName) != componentCounts.Admitted {
			snapshot.Status = "not_ready"
			return snapshot
		}
		for _, status := range healthByName {
			if !status.Healthy {
				snapshot.Status = "not_ready"
				return snapshot
			}
		}
	}
	if !m.bootCommitted.Load() {
		snapshot.Status = "stopping"
		return snapshot
	}
	snapshot.Status = "ready"
	return snapshot
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
		"startup":                 m.currentStartupSnapshot(),
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
	services := make([]Service, 0, len(m.services))
	for _, service := range m.services {
		services = append(services, service)
	}
	m.mu.RUnlock()

	// Collect all service health statuses
	var serviceStatuses []health.Status
	for _, service := range services {
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
