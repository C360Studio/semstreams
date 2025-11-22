package service

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360/semstreams/config"
	"github.com/c360/semstreams/metric"
	"github.com/c360/semstreams/natsclient"
	"github.com/c360/semstreams/types"
)

// Helper function to get the shared test NATS client
func createTestNATSClientForBase(t *testing.T) *natsclient.Client {
	// Skip if running unit tests without INTEGRATION_TESTS set
	if os.Getenv("INTEGRATION_TESTS") == "" {
		t.Skip("Skipping integration test - set INTEGRATION_TESTS=1 to run")
	}
	// Use the shared client created in TestMain
	return getSharedNATSClient(t)
}

// waitForHealthy waits for a service to become healthy with timeout
func waitForHealthy(service Service, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if service.IsHealthy() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

// waitForHealthCheckCalled waits for an atomic counter to become non-zero
func waitForHealthCheckCalled(counter *int64, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if atomic.LoadInt64(counter) > 0 {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

// Test service creation and initialization
func TestService_Creation(t *testing.T) {
	cfg := &config.Config{
		Platform: config.PlatformConfig{
			ID:   "test-platform",
			Type: "vessel",
		},
		Services: types.ServiceConfigs{
			"test": types.ServiceConfig{
				Name:    "test",
				Enabled: true,
				Config:  json.RawMessage(`{"default_timeout": "30s", "health_interval": "10s"}`),
			},
		},
	}

	natsClient := createTestNATSClientForBase(t)

	service := NewBaseServiceWithOptions("test-service", cfg,
		WithNATS(natsClient),
		WithMetrics(metric.NewMetricsRegistry()))

	assert.NotNil(t, service)
	assert.Equal(t, "test-service", service.Name())
	assert.Equal(t, StatusStopped, service.Status())
	assert.False(t, service.IsHealthy())
}

// Test service lifecycle
func TestService_Lifecycle(t *testing.T) {
	cfg := &config.Config{
		Platform: config.PlatformConfig{
			ID:   "test-platform",
			Type: "vessel",
		},
		Services: types.ServiceConfigs{
			"test": types.ServiceConfig{
				Name:    "test",
				Enabled: true,
				Config:  json.RawMessage(`{"default_timeout": "100ms", "health_interval": "50ms"}`),
			},
		},
	}

	natsClient := createTestNATSClientForBase(t)
	service := NewBaseServiceWithOptions("test-service", cfg,
		WithNATS(natsClient),
		WithMetrics(metric.NewMetricsRegistry()))

	// Start service
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := service.Start(ctx)
	require.NoError(t, err)
	assert.Equal(t, StatusRunning, service.Status())

	// Wait briefly for initialization
	time.Sleep(10 * time.Millisecond)

	// Stop service
	err = service.Stop(5 * time.Second)
	require.NoError(t, err)
	assert.Equal(t, StatusStopped, service.Status())
}

// Test health monitoring
func TestService_HealthMonitoring(t *testing.T) {
	cfg := &config.Config{
		Services: types.ServiceConfigs{
			"test": types.ServiceConfig{
				Name:    "test",
				Enabled: true,
				Config:  json.RawMessage(`{"health_interval": "50ms"}`),
			},
		},
	}

	natsClient := createTestNATSClientForBase(t)
	service := NewBaseServiceWithOptions("test-service", cfg,
		WithNATS(natsClient),
		WithMetrics(metric.NewMetricsRegistry()))

	// Health callback tracking
	healthChanges := make(chan bool, 10)
	service.OnHealthChange(func(healthy bool) {
		select {
		case healthChanges <- healthy:
		default:
		}
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start service
	err := service.Start(ctx)
	require.NoError(t, err)

	// Wait for health check to initialize with proper synchronization
	assert.True(t, waitForHealthy(service, 500*time.Millisecond), "service should become healthy")

	// Stop service
	err = service.Stop(5 * time.Second)
	require.NoError(t, err)
}

// Test graceful shutdown
func TestService_GracefulShutdown(t *testing.T) {
	cfg := &config.Config{
		Services: types.ServiceConfigs{
			"test": types.ServiceConfig{
				Name:    "test",
				Enabled: true,
				Config:  json.RawMessage(`{"default_timeout": "200ms"}`),
			},
		},
	}

	natsClient := createTestNATSClientForBase(t)
	service := NewBaseServiceWithOptions("test-service", cfg,
		WithNATS(natsClient),
		WithMetrics(metric.NewMetricsRegistry()))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start service
	err := service.Start(ctx)
	require.NoError(t, err)

	// Stop with timeout
	err = service.Stop(100 * time.Millisecond)
	require.NoError(t, err)
	assert.Equal(t, StatusStopped, service.Status())
}

// Test context cancellation
func TestService_ContextCancellation(t *testing.T) {
	cfg := &config.Config{
		Services: types.ServiceConfigs{
			"test": types.ServiceConfig{
				Name:    "test",
				Enabled: true,
				Config:  json.RawMessage(`{"default_timeout": "100ms"}`),
			},
		},
	}

	natsClient := createTestNATSClientForBase(t)
	service := NewBaseServiceWithOptions("test-service", cfg,
		WithNATS(natsClient),
		WithMetrics(metric.NewMetricsRegistry()))

	ctx, cancel := context.WithCancel(context.Background())

	// Start service
	err := service.Start(ctx)
	require.NoError(t, err)

	// Cancel context
	cancel()

	// Service should stop gracefully
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, StatusStopped, service.Status())
}

// Test status collection
func TestService_GetStatus(t *testing.T) {
	cfg := &config.Config{
		Services: types.ServiceConfigs{
			"test": types.ServiceConfig{
				Name:    "test",
				Enabled: true,
				Config:  json.RawMessage(`{"default_timeout": "30s"}`),
			},
		},
	}

	natsClient := createTestNATSClientForBase(t)
	service := NewBaseServiceWithOptions("test-service", cfg,
		WithNATS(natsClient),
		WithMetrics(metric.NewMetricsRegistry()))

	info := service.GetStatus()
	assert.Equal(t, "test-service", info.Name)
	assert.Equal(t, StatusStopped, info.Status)
	assert.Equal(t, int64(0), info.Uptime.Milliseconds())
	assert.Equal(t, int64(0), info.MessagesProcessed)
}

// Test custom health check
func TestService_CustomHealthCheck(t *testing.T) {
	cfg := &config.Config{
		Services: types.ServiceConfigs{
			"test": types.ServiceConfig{
				Name:    "test",
				Enabled: true,
				Config:  json.RawMessage(`{"health_interval": "50ms"}`),
			},
		},
	}

	natsClient := createTestNATSClientForBase(t)
	service := NewBaseServiceWithOptions("test-service", cfg,
		WithNATS(natsClient),
		WithMetrics(metric.NewMetricsRegistry()))

	// Set custom health check with atomic counter to avoid race condition
	var healthCheckCalled int64
	service.SetHealthCheck(func() error {
		atomic.StoreInt64(&healthCheckCalled, 1)
		return nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := service.Start(ctx)
	require.NoError(t, err)

	// Wait for health check to be called with proper synchronization
	assert.True(
		t,
		waitForHealthCheckCalled(&healthCheckCalled, 500*time.Millisecond),
		"custom health check should be called",
	)
	assert.Equal(t, int64(1), atomic.LoadInt64(&healthCheckCalled))

	err = service.Stop(5 * time.Second)
	require.NoError(t, err)
}

// Test failing health check
func TestService_FailingHealthCheck(t *testing.T) {
	cfg := &config.Config{
		Services: types.ServiceConfigs{
			"test": types.ServiceConfig{
				Name:    "test",
				Enabled: true,
				Config:  json.RawMessage(`{"health_interval": "50ms"}`),
			},
		},
	}

	natsClient := createTestNATSClientForBase(t)
	service := NewBaseServiceWithOptions("test-service", cfg,
		WithNATS(natsClient),
		WithMetrics(metric.NewMetricsRegistry()))

	// Set failing health check
	service.SetHealthCheck(func() error {
		return errors.New("health check failed")
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := service.Start(ctx)
	require.NoError(t, err)

	// Wait for health check
	time.Sleep(100 * time.Millisecond)
	assert.False(t, service.IsHealthy())

	err = service.Stop(5 * time.Second)
	require.NoError(t, err)
}

// Test concurrent starts and stops
func TestService_ConcurrentOperations(t *testing.T) {
	cfg := &config.Config{
		Services: types.ServiceConfigs{
			"test": types.ServiceConfig{
				Name:    "test",
				Enabled: true,
				Config:  json.RawMessage(`{"default_timeout": "100ms"}`),
			},
		},
	}

	natsClient := createTestNATSClientForBase(t)
	service := NewBaseServiceWithOptions("test-service", cfg,
		WithNATS(natsClient),
		WithMetrics(metric.NewMetricsRegistry()))

	ctx := context.Background()

	// Start service multiple times concurrently
	for i := 0; i < 10; i++ {
		go func() {
			_ = service.Start(ctx)
		}()
	}

	time.Sleep(50 * time.Millisecond)

	// Stop service multiple times concurrently
	for i := 0; i < 10; i++ {
		go func() {
			_ = service.Stop(5 * time.Second)
		}()
	}

	time.Sleep(50 * time.Millisecond)

	// Should be in a consistent state
	status := service.Status()
	assert.True(t, status == StatusRunning || status == StatusStopped)
}

// Test service restart
func TestService_Restart(t *testing.T) {
	cfg := &config.Config{
		Services: types.ServiceConfigs{
			"test": types.ServiceConfig{
				Name:    "test",
				Enabled: true,
				Config:  json.RawMessage(`{"default_timeout": "100ms"}`),
			},
		},
	}

	natsClient := createTestNATSClientForBase(t)
	service := NewBaseServiceWithOptions("test-service", cfg,
		WithNATS(natsClient),
		WithMetrics(metric.NewMetricsRegistry()))

	ctx := context.Background()

	// Start service
	err := service.Start(ctx)
	require.NoError(t, err)
	assert.Equal(t, StatusRunning, service.Status())

	// Stop service
	err = service.Stop(5 * time.Second)
	require.NoError(t, err)
	assert.Equal(t, StatusStopped, service.Status())

	// Restart service
	err = service.Start(ctx)
	require.NoError(t, err)
	assert.Equal(t, StatusRunning, service.Status())

	err = service.Stop(5 * time.Second)
	require.NoError(t, err)
}

// Test status transitions
func TestService_StatusTransitions(t *testing.T) {
	tests := []struct {
		name         string
		initial      Status
		action       func(*BaseService, context.Context) error
		expectedNext Status
	}{
		{
			name:         "stopped to running",
			initial:      StatusStopped,
			action:       func(s *BaseService, ctx context.Context) error { return s.Start(ctx) },
			expectedNext: StatusRunning,
		},
		{
			name:         "running to stopped",
			initial:      StatusRunning,
			action:       func(s *BaseService, _ context.Context) error { return s.Stop(5 * time.Second) },
			expectedNext: StatusStopped,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				Services: types.ServiceConfigs{
					"test": types.ServiceConfig{
						Name:    "test",
						Enabled: true,
						Config:  json.RawMessage(`{"default_timeout": "100ms"}`),
					},
				},
			}

			natsClient := createTestNATSClientForBase(t)
			service := NewBaseServiceWithOptions("test-service", cfg,
				WithNATS(natsClient),
				WithMetrics(metric.NewMetricsRegistry()))

			ctx := context.Background()

			// Set initial state if needed
			var err error
			if tt.initial == StatusRunning {
				err = service.Start(ctx)
				require.NoError(t, err)
			}

			// Perform action
			err = tt.action(service, ctx)
			require.NoError(t, err)
			assert.Equal(t, tt.expectedNext, service.Status())

			// Cleanup
			service.Stop(5 * time.Second)
		})
	}
}

// Test functional options pattern
func TestService_FunctionalOptions(t *testing.T) {
	cfg := &config.Config{
		Platform: config.PlatformConfig{
			ID:   "test-platform",
			Type: "vessel",
		},
		Services: types.ServiceConfigs{
			"test": types.ServiceConfig{
				Name:    "test",
				Enabled: true,
				Config:  json.RawMessage(`{"health_interval": "10s"}`),
			},
		},
	}

	t.Run("service with no dependencies", func(t *testing.T) {
		service := NewBaseServiceWithOptions("test-service", cfg)

		assert.NotNil(t, service)
		assert.Equal(t, "test-service", service.Name())
		assert.Equal(t, StatusStopped, service.Status())
		assert.Nil(t, service.nats)
		assert.Nil(t, service.metricsRegistry)
	})

	t.Run("service with NATS only", func(t *testing.T) {
		natsClient := createTestNATSClientForBase(t)

		service := NewBaseServiceWithOptions("test-service", cfg, WithNATS(natsClient))

		assert.NotNil(t, service)
		assert.Equal(t, natsClient, service.nats)
		assert.Nil(t, service.metricsRegistry)
	})

	t.Run("service with metrics only", func(t *testing.T) {
		metricsRegistry := metric.NewMetricsRegistry()

		service := NewBaseServiceWithOptions("test-service", cfg, WithMetrics(metricsRegistry))

		assert.NotNil(t, service)
		assert.Nil(t, service.nats)
		assert.Equal(t, metricsRegistry, service.metricsRegistry)
	})

	t.Run("service with both NATS and metrics", func(t *testing.T) {
		natsClient := createTestNATSClientForBase(t)
		metricsRegistry := metric.NewMetricsRegistry()

		service := NewBaseServiceWithOptions("test-service", cfg,
			WithNATS(natsClient),
			WithMetrics(metricsRegistry))

		assert.NotNil(t, service)
		assert.Equal(t, natsClient, service.nats)
		assert.Equal(t, metricsRegistry, service.metricsRegistry)
	})

	t.Run("service with custom health check", func(t *testing.T) {
		healthCheckCalled := false
		healthCheck := func() error {
			healthCheckCalled = true
			return nil
		}

		service := NewBaseServiceWithOptions("test-service", cfg, WithHealthCheck(healthCheck))

		assert.NotNil(t, service)
		assert.NotNil(t, service.healthCheckFunc)

		// Test health check function
		err := service.healthCheckFunc()
		assert.NoError(t, err)
		assert.True(t, healthCheckCalled)
	})

	t.Run("service with custom health interval", func(t *testing.T) {
		customInterval := 5 * time.Second

		service := NewBaseServiceWithOptions("test-service", cfg, WithHealthInterval(customInterval))

		assert.NotNil(t, service)
		assert.Equal(t, customInterval, service.healthInterval)
	})

	t.Run("service with health change callback", func(t *testing.T) {
		var healthStatus bool
		healthCallback := func(healthy bool) {
			healthStatus = healthy
		}

		service := NewBaseServiceWithOptions("test-service", cfg, OnHealthChange(healthCallback))

		assert.NotNil(t, service)
		assert.NotNil(t, service.onHealthChange)

		// Test callback
		service.onHealthChange(true)
		assert.True(t, healthStatus)

		service.onHealthChange(false)
		assert.False(t, healthStatus)
	})

	t.Run("service with multiple options", func(t *testing.T) {
		natsClient := createTestNATSClientForBase(t)
		metricsRegistry := metric.NewMetricsRegistry()
		customInterval := 5 * time.Second

		var healthStatus bool
		healthCallback := func(healthy bool) {
			healthStatus = healthy
		}

		healthCheckCalled := false
		healthCheck := func() error {
			healthCheckCalled = true
			return nil
		}

		service := NewBaseServiceWithOptions("test-service", cfg,
			WithNATS(natsClient),
			WithMetrics(metricsRegistry),
			WithHealthCheck(healthCheck),
			WithHealthInterval(customInterval),
			OnHealthChange(healthCallback))

		assert.NotNil(t, service)
		assert.Equal(t, natsClient, service.nats)
		assert.Equal(t, metricsRegistry, service.metricsRegistry)
		assert.Equal(t, customInterval, service.healthInterval)
		assert.NotNil(t, service.healthCheckFunc)
		assert.NotNil(t, service.onHealthChange)

		// Test health check
		err := service.healthCheckFunc()
		assert.NoError(t, err)
		assert.True(t, healthCheckCalled)

		// Test health callback
		service.onHealthChange(true)
		assert.True(t, healthStatus)
	})
}

// MessageLogger tests removed - MessageLogger has been moved to processor components
// See pkg/processor/logger for the MessageLoggerProcessor component
