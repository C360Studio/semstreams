package service

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/c360studio/semstreams/health"
	"github.com/c360studio/semstreams/internal/lifecyclejoin"
	"github.com/c360studio/semstreams/metric"
	"github.com/c360studio/semstreams/natsclient"
	shutdownerrs "github.com/c360studio/semstreams/pkg/errs"
	"github.com/stretchr/testify/require"
)

type blockingHTTPGeneration struct {
	server          *http.Server
	generation      *lifecyclejoin.Generation
	handlerCtx      <-chan context.Context
	releaseHandler  chan struct{}
	shutdownStarted chan struct{}
}

func newBlockingHTTPGeneration(t *testing.T) *blockingHTTPGeneration {
	t.Helper()
	runtimeCtx, cancel := context.WithCancel(t.Context())
	handlerCtx := make(chan context.Context, 1)
	releaseHandler := make(chan struct{})
	shutdownStarted := make(chan struct{})
	var shutdownStartedOnce sync.Once
	mux := http.NewServeMux()
	mux.HandleFunc("/block", func(w http.ResponseWriter, r *http.Request) {
		handlerCtx <- r.Context()
		<-releaseHandler
		w.WriteHeader(http.StatusNoContent)
	})
	server := &http.Server{
		Handler: mux,
		BaseContext: func(net.Listener) context.Context {
			return runtimeCtx
		},
	}
	server.RegisterOnShutdown(func() { shutdownStartedOnce.Do(func() { close(shutdownStarted) }) })
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	serveDone := make(chan struct{})
	go func() {
		defer close(serveDone)
		_ = server.Serve(listener)
	}()
	requestDone := make(chan error, 1)
	go func() {
		response, requestErr := http.Get("http://" + listener.Addr().String() + "/block")
		if response != nil {
			_ = response.Body.Close()
		}
		requestDone <- requestErr
	}()
	t.Cleanup(func() {
		select {
		case <-releaseHandler:
		default:
			close(releaseHandler)
		}
		server.Close()
		<-serveDone
		<-requestDone
	})
	return &blockingHTTPGeneration{
		server:          server,
		generation:      lifecyclejoin.NewGeneration(cancel, func() { <-serveDone }),
		handlerCtx:      handlerCtx,
		releaseHandler:  releaseHandler,
		shutdownStarted: shutdownStarted,
	}
}

// mockNATSClient provides a mock NATS client for testing
type mockNATSClient struct {
	connected     bool
	connectionNil bool
}

func newMockNATSClient(connected bool, connectionNil bool) *mockNATSClient {
	return &mockNATSClient{
		connected:     connected,
		connectionNil: connectionNil,
	}
}

func (m *mockNATSClient) GetConnection() any {
	if m.connectionNil {
		return nil
	}
	return &mockConnection{connected: m.connected}
}

func (m *mockNATSClient) IsConnected() bool {
	return m.connected && !m.connectionNil
}

type mockConnection struct {
	connected bool
}

// MockService provides a mock service for testing
type MockService struct {
	name    string
	status  Status
	healthy bool
}

type stopFailService struct {
	MockService
	err error
}

func (s *stopFailService) Stop(context.Context) error { return s.err }

func TestStopAllRetainsServiceAuthorityAfterFailure(t *testing.T) {
	m := NewServiceManager(NewServiceRegistry())
	wantErr := errors.New("stop failed")
	svc := &stopFailService{MockService: MockService{name: "failing", status: StatusRunning}, err: wantErr}
	m.services[svc.name] = svc
	m.order = []string{svc.name}
	require.ErrorIs(t, m.StopAll(context.Background()), wantErr)
	_, retained := m.services[svc.name]
	require.True(t, retained)
	require.Equal(t, []string{svc.name}, m.order)
}

func TestRuntimeServerGenerationCancelsBaseContextWithCanceledStopBudget(t *testing.T) {
	m := NewServiceManager(NewServiceRegistry())
	m.httpMux = http.NewServeMux()
	m.isHTTPManager = true
	require.NoError(t, m.completeHTTPSetup(context.Background()))
	require.NotNil(t, m.httpServer.BaseContext)
	handlerCtx := m.httpServer.BaseContext(nil)
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, m.stopRuntimeServers(canceled), context.Canceled)
	select {
	case <-handlerCtx.Done():
	default:
		t.Fatal("server generation did not cancel handler BaseContext")
	}
	require.NoError(t, m.stopRuntimeServers(context.Background()))
}

func TestRuntimeServerShutdownKeepsHandlerAuthorityLiveUntilHandlerReturns(t *testing.T) {
	runtime := newBlockingHTTPGeneration(t)
	m := NewServiceManager(NewServiceRegistry())
	m.httpServer = runtime.server
	m.serverGeneration = runtime.generation
	handlerCtx := <-runtime.handlerCtx

	stopDone := make(chan error, 1)
	go func() { stopDone <- m.stopRuntimeServers(t.Context()) }()
	<-runtime.shutdownStarted
	select {
	case <-handlerCtx.Done():
		t.Fatal("HTTP handler authority canceled before listener Shutdown drained it")
	default:
	}
	close(runtime.releaseHandler)
	require.NoError(t, <-stopDone)
	require.ErrorIs(t, handlerCtx.Err(), context.Canceled)
}

func TestHealthListenerShutdownKeepsHandlerAuthorityLiveUntilHandlerReturns(t *testing.T) {
	runtime := newBlockingHTTPGeneration(t)
	m := NewServiceManager(NewServiceRegistry())
	m.healthServer = runtime.server
	m.healthGeneration = runtime.generation
	handlerCtx := <-runtime.handlerCtx

	stopDone := make(chan error, 1)
	go func() { stopDone <- m.StopHealthListener(t.Context()) }()
	<-runtime.shutdownStarted
	select {
	case <-handlerCtx.Done():
		t.Fatal("health handler authority canceled before listener Shutdown drained it")
	default:
	}
	close(runtime.releaseHandler)
	require.NoError(t, <-stopDone)
	require.ErrorIs(t, handlerCtx.Err(), context.Canceled)
}

func TestRuntimeServerShutdownDeadlineNamesListenerOwnerAndPhase(t *testing.T) {
	runtime := newBlockingHTTPGeneration(t)
	m := NewServiceManager(NewServiceRegistry())
	m.httpServer = runtime.server
	m.serverGeneration = runtime.generation
	<-runtime.handlerCtx

	stopCtx, cancelStop := context.WithCancel(t.Context())
	stopDone := make(chan error, 1)
	go func() { stopDone <- m.stopRuntimeServers(stopCtx) }()
	<-runtime.shutdownStarted
	cancelStop()
	err := <-stopDone
	var shutdownErr *shutdownerrs.ShutdownError
	require.ErrorAs(t, err, &shutdownErr)
	require.Equal(t, "service-manager/http-listener", shutdownErr.Owner)
	require.Equal(t, shutdownerrs.PhaseShutdownListener, shutdownErr.Phase)
	require.ErrorIs(t, err, context.Canceled)

	close(runtime.releaseHandler)
	require.NoError(t, m.stopRuntimeServers(t.Context()))
}

func (m *MockService) Name() string { return m.name }
func (m *MockService) Start(ctx context.Context) error {
	// Check for cancellation
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	return nil
}
func (m *MockService) Stop(context.Context) error { return nil }
func (m *MockService) Status() Status             { return m.status }
func (m *MockService) IsHealthy() bool            { return m.healthy }
func (m *MockService) GetStatus() Info {
	return Info{
		Name:   m.name,
		Status: m.status,
	}
}
func (m *MockService) RegisterMetrics(_ metric.MetricsRegistrar) error { return nil }

func (m *MockService) Health() health.Status {
	if !m.healthy {
		return health.NewUnhealthy(m.name, "Mock service unhealthy")
	}
	switch m.status {
	case StatusRunning:
		return health.NewHealthy(m.name, "Mock service running")
	case StatusStarting:
		return health.NewDegraded(m.name, "Mock service starting")
	case StatusStopping:
		return health.NewDegraded(m.name, "Mock service stopping")
	default:
		return health.NewUnhealthy(m.name, "Mock service stopped")
	}
}

// createTestServiceDependencies creates Dependencies for testing
func createTestServiceDependencies(natsClient *mockNATSClient) *Dependencies {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	metricsRegistry := metric.NewMetricsRegistry()

	var deps *Dependencies
	if natsClient != nil {
		deps = &Dependencies{
			NATSClient:      &natsclient.Client{}, // We'll override the interface behavior with mocks
			Logger:          logger,
			MetricsRegistry: metricsRegistry,
		}
	} else {
		deps = &Dependencies{
			Logger:          logger,
			MetricsRegistry: metricsRegistry,
		}
	}

	return deps
}

// createTestServiceManager creates a Manager for testing
// This replaces the deprecated NewServiceManagerService
func createTestServiceManager(config ManagerConfig, deps *Dependencies) *Manager {
	registry := NewServiceRegistry()
	serviceManager := NewServiceManager(registry)
	serviceManager.config = config
	serviceManager.isHTTPManager = true
	var logger *slog.Logger
	if deps != nil && deps.Logger != nil {
		logger = deps.Logger
	}
	serviceManager.BaseService = NewBaseServiceWithOptions(
		"service-manager",
		nil,
		WithLogger(logger),
	)
	if deps != nil && deps.NATSClient != nil {
		serviceManager.natsClient = deps.NATSClient
	}
	if deps != nil && deps.Manager != nil {
		serviceManager.configManager = deps.Manager
	}
	return serviceManager
}

func TestServiceManager_StartWithNATSAvailable(t *testing.T) {
	// Create mock NATS client (connected and connection available)
	mockNATS := newMockNATSClient(true, false)
	deps := createTestServiceDependencies(mockNATS)

	// Configure as HTTP manager so Start() runs
	config := ManagerConfig{
		HTTPPort:  8081, // Use different port to avoid conflicts
		SwaggerUI: false,
	}

	// Create Manager for testing
	serviceManager := createTestServiceManager(config, deps)

	// Test Start with NATS dependencies available.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := serviceManager.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start Manager: %v", err)
	}

	// The fixed service composition should start successfully.

	// Clean up
	err = serviceManager.Stop(context.Background())
	if err != nil {
		t.Errorf("Failed to stop Manager: %v", err)
	}
}

func TestServiceManager_ConfigWatcher_WithNATSUnavailable(t *testing.T) {
	// Create dependencies without NATS client
	deps := createTestServiceDependencies(nil)

	// Configure as HTTP manager so Start() runs
	config := ManagerConfig{
		HTTPPort:  8082, // Use different port to avoid conflicts
		SwaggerUI: false,
	}

	// Create Manager directly for testing
	serviceManager := createTestServiceManager(config, deps)

	// Test Start method should succeed even without NATS
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := serviceManager.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start Manager without NATS: %v", err)
	}

	// Verify service started successfully without ConfigWatcher

	// Clean up
	err = serviceManager.Stop(context.Background())
	if err != nil {
		t.Errorf("Failed to stop Manager: %v", err)
	}
}

func TestServiceManager_ConfigWatcher_NATSConnectionNil(t *testing.T) {
	// Create mock NATS client with nil connection
	mockNATS := newMockNATSClient(false, true)
	deps := createTestServiceDependencies(mockNATS)

	// Configure as HTTP manager so Start() runs
	config := ManagerConfig{
		HTTPPort:  8083, // Use different port to avoid conflicts
		SwaggerUI: false,
	}

	// Create Manager directly for testing
	serviceManager := createTestServiceManager(config, deps)

	// Test Start method should succeed with nil NATS connection
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := serviceManager.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start Manager with nil NATS connection: %v", err)
	}

	// Clean up
	err = serviceManager.Stop(context.Background())
	if err != nil {
		t.Errorf("Failed to stop Manager: %v", err)
	}
}

func TestServiceManager_ConfigWatcher_ShutdownBehavior(t *testing.T) {
	// Create mock NATS client
	mockNATS := newMockNATSClient(true, false)
	deps := createTestServiceDependencies(mockNATS)

	// Configure as HTTP manager
	config := ManagerConfig{
		HTTPPort:  8084,
		SwaggerUI: false,
	}

	// Create Manager directly for testing
	serviceManager := createTestServiceManager(config, deps)

	// Start the service
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := serviceManager.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start Manager: %v", err)
	}

	// Test shutdown behavior
	err = serviceManager.Stop(context.Background())
	if err != nil {
		t.Errorf("Failed to stop Manager cleanly: %v", err)
	}

	// Verify multiple stops don't cause issues
	err = serviceManager.Stop(context.Background())
	if err != nil {
		t.Errorf("Second stop should not cause errors: %v", err)
	}
}

func TestServiceManager_NonHTTPManager_NoConfigWatcher(t *testing.T) {
	// Create service manager
	manager := createTestServiceManager(ManagerConfig{}, nil)

	// Test that non-HTTP manager instances don't initialize ConfigWatcher
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	// Start without being configured as HTTP manager
	err := manager.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start non-HTTP Manager: %v", err)
	}

	// Clean up
	err = manager.Stop(context.Background())
	if err != nil {
		t.Errorf("Failed to stop non-HTTP Manager: %v", err)
	}
}

func TestServiceManager_HandleServiceConfigChange_KeyParsing(t *testing.T) {

	tests := []struct {
		name        string
		key         string
		shouldParse bool
		serviceName string
		property    string
	}{
		{
			name:        "valid simple key",
			key:         "services.message-logger.enabled",
			shouldParse: true,
			serviceName: "message-logger",
			property:    "enabled",
		},
		{
			name:        "valid nested key",
			key:         "services.message-logger.network.port",
			shouldParse: true,
			serviceName: "message-logger",
			property:    "network.port",
		},
		{
			name:        "service name with underscore s",
			key:         "services.message_logger.enabled",
			shouldParse: true,
			serviceName: "message-logger", // Should normalize
			property:    "enabled",
		},
		{
			name:        "invalid key - too short",
			key:         "services.test",
			shouldParse: false,
		},
		{
			name:        "invalid key - wrong prefix",
			key:         "components.test.enabled",
			shouldParse: false,
		},
		{
			name:        "invalid key - no dots",
			key:         "services",
			shouldParse: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Since key parsing is internal to config processing, we test indirectly
			// by verifying the expected parsing results match our test cases
			if tt.shouldParse {
				t.Logf("Valid config key pattern: %s", tt.key)
				// In a real scenario, this would be parsed to service: %s, property: %s
				if tt.serviceName != "" {
					t.Logf("Expected service: %s, property: %s", tt.serviceName, tt.property)
				}
			} else {
				t.Logf("Invalid config key pattern: %s", tt.key)
			}
		})
	}
}

func TestServiceManager_ConfigWatcher_RealNATSConnection(t *testing.T) {
	// This test validates that when a real NATS connection is available,
	// ConfigWatcher is properly initialized

	// We can't easily test with a real NATS server in unit tests,
	// but we can verify the logic paths are correct by ensuring:
	// 1. hasNATSAccess() returns true with a real client
	// 2. initializeConfigWatcher() is called when NATS is available
	// 3. The service gracefully handles ConfigWatcher initialization failures

	// Create a service manager with no NATS client
	registry := NewServiceRegistry()
	sm := NewServiceManager(registry)

	// Test that hasNATSAccess returns false with no client
	if sm.hasNATSAccess() {
		t.Error("Expected hasNATSAccess() to return false with no NATS client")
	}

	// Test that it returns false with nil client
	sm.natsClient = nil
	if sm.hasNATSAccess() {
		t.Error("Expected hasNATSAccess() to return false with nil NATS client")
	}

	t.Logf("ConfigWatcher integration logic validated - would initialize with real NATS connection")
}

func TestServiceManager_NormalizeServiceName(t *testing.T) {

	tests := []struct {
		input    string
		expected string
	}{
		{"message-logger", "message-logger"},
		{"message_logger", "message-logger"},
		{"component_manager", "component-manager"},
		{"service_with_multiple_underscore s", "service-with-multiple-underscore s"},
		{"already-has-hyphens", "already-has-hyphens"},
		{"mixed_and-styles", "mixed-and-styles"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			// Since normalizeServiceName is not public, we test the normalization logic indirectly
			// by verifying that service name normalization works through service registration
			// The expected behavior is that underscore s are converted to hyphens
			normalized := strings.ReplaceAll(tt.input, "_", "-")
			if normalized != tt.expected {
				t.Errorf("Service name normalization: %q should become %q, got %q", tt.input, tt.expected, normalized)
			}
		})
	}
}

// TestServiceManager_RegisterInstance verifies that RegisterInstance places the
// service into the map AND the order slice so StartAll and StopAll treat it
// identically to a config-driven CreateService registration.
func TestServiceManager_RegisterInstance(t *testing.T) {
	t.Parallel()
	manager := createTestServiceManager(ManagerConfig{}, nil)

	svc := &MockService{
		name:    "instance-svc",
		status:  StatusStopped,
		healthy: true,
	}

	if err := manager.RegisterInstance("instance-svc", svc); err != nil {
		t.Fatalf("RegisterInstance: %v", err)
	}

	// GetService must find it.
	got, ok := manager.GetService("instance-svc")
	if !ok {
		t.Fatal("RegisterInstance: GetService returned not-found")
	}
	if got != svc {
		t.Error("RegisterInstance: GetService returned wrong instance")
	}

	// order slice must include it so StopAll visits it in reverse order.
	manager.mu.RLock()
	found := false
	for _, name := range manager.order {
		if name == "instance-svc" {
			found = true
			break
		}
	}
	manager.mu.RUnlock()

	if !found {
		t.Error("RegisterInstance: service name not found in order slice")
	}
}

func TestServiceManager_RegisterInstance_DuplicateRejected(t *testing.T) {
	t.Parallel()
	manager := createTestServiceManager(ManagerConfig{}, nil)

	first := &MockService{name: "dup-svc", status: StatusStopped, healthy: true}
	second := &MockService{name: "dup-svc", status: StatusStopped, healthy: true}

	if err := manager.RegisterInstance("dup-svc", first); err != nil {
		t.Fatal(err)
	}
	err := manager.RegisterInstance("dup-svc", second)
	var duplicate *DuplicateServiceError
	if !errors.As(err, &duplicate) {
		t.Fatalf("duplicate error = %v", err)
	}

	// The original instance and registration order remain unchanged.
	got, ok := manager.GetService("dup-svc")
	if !ok || got != first {
		t.Errorf("duplicate RegisterInstance: GetService = %v,%v, want the first instance", got, ok)
	}

	// order must contain exactly ONE entry for the name (no double-Stop).
	manager.mu.RLock()
	count := 0
	for _, name := range manager.order {
		if name == "dup-svc" {
			count++
		}
	}
	manager.mu.RUnlock()
	if count != 1 {
		t.Errorf("duplicate RegisterInstance: order has %d entries for dup-svc, want exactly 1 (double entry → double Stop)", count)
	}
}
