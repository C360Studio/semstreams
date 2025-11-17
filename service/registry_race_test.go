package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/c360/semstreams/health"
	"github.com/c360/semstreams/metric"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockService implements the Service interface for testing
type mockService struct {
	name    string
	started bool
	stopped bool
	healthy bool
	mu      sync.RWMutex
}

func newMockService(name string) *mockService {
	return &mockService{
		name:    name,
		healthy: true,
	}
}

func (m *mockService) Start(ctx context.Context) error {
	// Check for cancellation
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.started {
		return fmt.Errorf("service %s already started", m.name)
	}
	m.started = true
	m.stopped = false
	return nil
}

func (m *mockService) Stop(_ time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.started {
		return fmt.Errorf("service %s not started", m.name)
	}
	m.started = false
	m.stopped = true
	return nil
}

func (m *mockService) IsHealthy() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.healthy && m.started
}

func (m *mockService) Status() Status {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.started {
		return StatusRunning
	}
	return StatusStopped
}

func (m *mockService) GetStatus() Info {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return Info{
		Name:   m.name,
		Status: m.Status(),
	}
}

func (m *mockService) Name() string {
	return m.name
}

func (m *mockService) RegisterMetrics(_ metric.MetricsRegistrar) error {
	return nil
}

func (m *mockService) Health() health.Status {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if !m.healthy {
		return health.NewUnhealthy(m.name, "Mock service is unhealthy")
	}

	if !m.started {
		return health.NewUnhealthy(m.name, "Mock service not started")
	}

	if m.stopped {
		return health.NewUnhealthy(m.name, "Mock service stopped")
	}

	return health.NewHealthy(m.name, "Mock service operating normally")
}

// shutdownTrackingService wraps mockService to track shutdown order
type shutdownTrackingService struct {
	*mockService
	shutdownCallback func()
}

func (s *shutdownTrackingService) Stop(timeout time.Duration) error {
	s.shutdownCallback()
	return s.mockService.Stop(timeout)
}

func (m *mockService) SetHealthy(healthy bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.healthy = healthy
}

func (m *mockService) IsStarted() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.started
}

func (m *mockService) IsStopped() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.stopped
}

// TestServiceManager_ConcurrentOperations tests concurrent registry operations
func TestServiceManager_ConcurrentOperations(t *testing.T) {
	registry := NewServiceRegistry()

	var wg sync.WaitGroup
	const numGoroutines = 50
	const servicesPerGoroutine = 10

	// Register factories concurrently
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < servicesPerGoroutine; j++ {
				factoryName := fmt.Sprintf("factory-%d-%d", id, j)
				constructor := func(_ json.RawMessage, _ *Dependencies) (Service, error) {
					return newMockService(factoryName), nil
				}
				_ = registry.Register(factoryName, constructor)
			}
		}(i)
	}

	wg.Wait()

	// Verify constructors were registered
	constructors := registry.Constructors()
	assert.True(t, len(constructors) > 0, "Expected constructors to be registered")
}

// TestServiceManager_ServiceLifecycle tests service creation, start, and stop
func TestServiceManager_ServiceLifecycle(t *testing.T) {
	registry := NewServiceRegistry()
	manager := NewServiceManager(registry)

	// Register test constructor
	err := registry.Register("test-service", func(_ json.RawMessage, _ *Dependencies) (Service, error) {
		return newMockService("test-service"), nil
	})
	require.NoError(t, err)

	// Create service
	service, err := manager.CreateService("test-service", json.RawMessage(`{}`), &Dependencies{})
	require.NoError(t, err)

	mockSvc := service.(*mockService)
	assert.False(t, mockSvc.IsStarted(), "Service should not be started initially")

	// Start service
	ctx := context.Background()
	err = service.Start(ctx)
	require.NoError(t, err)
	assert.True(t, mockSvc.IsStarted(), "Service should be started")
	assert.True(t, service.IsHealthy(), "Service should be healthy")

	// Stop service
	err = service.Stop(5 * time.Second)
	require.NoError(t, err)
	assert.True(t, mockSvc.IsStopped(), "Service should be stopped")
	assert.False(t, service.IsHealthy(), "Service should not be healthy after stop")
}

// TestServiceManager_StopAllCleanup tests that StopAll properly cleans up manager
func TestServiceManager_StopAllCleanup(t *testing.T) {
	registry := NewServiceRegistry()
	manager := NewServiceManager(registry)

	// Register services
	serviceNames := []string{"service-1", "service-2", "service-3", "service-4", "service-5"}
	for _, name := range serviceNames {
		err := registry.Register(name, func(_ json.RawMessage, _ *Dependencies) (Service, error) {
			return newMockService(name), nil
		})
		require.NoError(t, err)

		// Create and start service using manager
		ctx := context.Background()
		err = manager.StartService(ctx, name, json.RawMessage(`{}`), &Dependencies{})
		require.NoError(t, err)
	}

	// Verify services are running
	allServices := manager.GetAllServices()
	assert.Len(t, allServices, len(serviceNames), "Expected all services to be created")

	for _, service := range allServices {
		assert.True(t, service.IsHealthy(), "Expected all services to be healthy")
	}

	// Stop all services
	err := manager.StopAll(5 * time.Second)
	assert.NoError(t, err)

	// Verify manager is cleaned up
	allServicesAfterStop := manager.GetAllServices()
	assert.Empty(t, allServicesAfterStop, "Expected manager to be empty after StopAll")

	// Verify individual services are stopped
	for _, service := range allServices {
		mockSvc := service.(*mockService)
		assert.True(t, mockSvc.IsStopped(), "Expected service to be stopped")
		assert.False(t, service.IsHealthy(), "Expected service to be unhealthy")
	}
}

// TestServiceManager_ReverseOrderShutdown tests services stop in reverse registration order
func TestServiceManager_ReverseOrderShutdown(t *testing.T) {
	registry := NewServiceRegistry()
	manager := NewServiceManager(registry)

	var shutdownOrder []string
	var mu sync.Mutex

	// Register services with tracking of shutdown order
	serviceNames := []string{"first", "second", "third", "fourth"}
	for _, name := range serviceNames {
		serviceName := name // capture for closure
		err := registry.Register(serviceName, func(_ json.RawMessage, _ *Dependencies) (Service, error) {
			// Create a wrapper service to track shutdown order
			baseService := newMockService(serviceName)
			wrappedService := &shutdownTrackingService{
				mockService: baseService,
				shutdownCallback: func() {
					mu.Lock()
					shutdownOrder = append(shutdownOrder, serviceName)
					mu.Unlock()
				},
			}
			return wrappedService, nil
		})
		require.NoError(t, err)

		// Create and start service using manager
		ctx := context.Background()
		err = manager.StartService(ctx, serviceName, json.RawMessage(`{}`), &Dependencies{})
		require.NoError(t, err)
	}

	// Stop all services
	err := manager.StopAll(5 * time.Second)
	assert.NoError(t, err)

	// Verify shutdown order is reverse of registration order
	expectedOrder := []string{"fourth", "third", "second", "first"}
	assert.Equal(t, expectedOrder, shutdownOrder, "Expected services to stop in reverse registration order")
}

// TestServiceManager_ConcurrentStartStop tests concurrent start/stop operations
func TestServiceManager_ConcurrentStartStop(t *testing.T) {
	registry := NewServiceRegistry()
	manager := NewServiceManager(registry)

	// Register mandatory service constructor in isolated manager
	err := registry.Register("component-manager", func(_ json.RawMessage, _ *Dependencies) (Service, error) {
		return newMockService("component-manager"), nil
	})
	require.NoError(t, err)

	// Register services
	const numServices = 20
	for i := 0; i < numServices; i++ {
		serviceName := fmt.Sprintf("service-%d", i)
		err := registry.Register(serviceName, func(_ json.RawMessage, _ *Dependencies) (Service, error) {
			return newMockService(serviceName), nil
		})
		require.NoError(t, err)

		_, err = manager.CreateService(serviceName, json.RawMessage(`{}`), &Dependencies{})
		require.NoError(t, err)
		// Service is automatically tracked by manager
	}

	var wg sync.WaitGroup
	ctx := context.Background()

	// Start all services concurrently
	wg.Add(1)
	go func() {
		defer wg.Done()
		err := manager.StartAll(ctx)
		assert.NoError(t, err)
	}()

	// Concurrent individual service operations
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			serviceName := fmt.Sprintf("service-%d", id)
			service, exists := manager.GetService(serviceName)
			if exists {
				_ = service.IsHealthy()
				_ = service.Status()
			}
		}(i)
	}

	wg.Wait()

	// Verify all services are healthy
	healthy := manager.GetHealthyServices()
	assert.GreaterOrEqual(t, len(healthy), numServices, "Expected all services to be healthy")

	// Stop all services
	err = manager.StopAll(5 * time.Second)
	assert.NoError(t, err)

	// Verify cleanup
	allServices := manager.GetAllServices()
	assert.Empty(t, allServices, "Expected manager to be empty after stop")
}

// TestServiceManager_RemoveService tests service removal
func TestServiceManager_RemoveService(t *testing.T) {
	registry := NewServiceRegistry()
	manager := NewServiceManager(registry)

	// Register and create service
	err := registry.Register("test-service", func(_ json.RawMessage, _ *Dependencies) (Service, error) {
		return newMockService("test-service"), nil
	})
	require.NoError(t, err)

	// Create and start service using manager
	ctx := context.Background()
	err = manager.StartService(ctx, "test-service", json.RawMessage(`{}`), &Dependencies{})
	require.NoError(t, err)

	// Verify service exists
	service, exists := manager.GetService("test-service")
	assert.True(t, exists, "Expected service to exist")
	require.NotNil(t, service)

	// Stop service before removing
	err = manager.StopService("test-service", 5*time.Second)
	require.NoError(t, err)

	// Remove service
	manager.RemoveService("test-service")

	// Verify service is removed
	_, exists = manager.GetService("test-service")
	assert.False(t, exists, "Expected service to be removed")
}

// TestServiceManager_ErrorHandling tests error scenarios
func TestServiceManager_ErrorHandling(t *testing.T) {
	registry := NewServiceRegistry()
	manager := NewServiceManager(registry)

	// Test duplicate constructor registration
	constructor := func(_ json.RawMessage, _ *Dependencies) (Service, error) {
		return newMockService("test"), nil
	}

	err := registry.Register("test-constructor", constructor)
	assert.NoError(t, err)

	err = registry.Register("test-constructor", constructor)
	assert.Error(t, err, "Expected error for duplicate constructor registration")

	// Test creating service with non-existent constructor
	_, err = manager.CreateService("non-existent", json.RawMessage(`{}`), &Dependencies{})
	assert.Error(t, err, "Expected error for non-existent constructor")

	// Test getting non-existent service
	_, exists := manager.GetService("non-existent")
	assert.False(t, exists, "Expected false for non-existent service")

	// Test status of non-existent service
	_, err = manager.GetServiceStatus("non-existent")
	assert.Error(t, err, "Expected error for non-existent service status")
}
