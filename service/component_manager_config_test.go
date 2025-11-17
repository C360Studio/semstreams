package service_test

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/c360/semstreams/component"
	"github.com/c360/semstreams/config"
	"github.com/c360/semstreams/natsclient"
	"github.com/c360/semstreams/service"
	"github.com/c360/semstreams/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMockComponent is a simple test component for integration testing
type TestMockComponent struct {
	id        string
	started   bool
	stopped   bool
	config    json.RawMessage
	startErr  error
	stopErr   error
	startTime time.Time
}

// Implement Discoverable interface
func (t *TestMockComponent) Meta() component.Metadata {
	return component.Metadata{
		Name:        "test-mock",
		Type:        string(types.ComponentTypeProcessor),
		Description: "Test mock component",
		Version:     "1.0.0",
	}
}

func (t *TestMockComponent) InputPorts() []component.Port {
	return []component.Port{}
}

func (t *TestMockComponent) OutputPorts() []component.Port {
	return []component.Port{}
}

func (t *TestMockComponent) ConfigSchema() component.ConfigSchema {
	return component.ConfigSchema{
		Properties: map[string]component.PropertySchema{
			"setting": {
				Type:        "string",
				Description: "Test setting",
			},
		},
	}
}

func (t *TestMockComponent) Health() component.HealthStatus {
	return component.HealthStatus{
		Healthy:   !t.stopped,
		LastCheck: time.Now(),
		Uptime:    time.Since(t.startTime),
	}
}

func (t *TestMockComponent) DataFlow() component.FlowMetrics {
	return component.FlowMetrics{
		MessagesPerSecond: 0,
		BytesPerSecond:    0,
		ErrorRate:         0,
		LastActivity:      time.Now(),
	}
}

// Implement Service-like methods for lifecycle
func (t *TestMockComponent) Start(_ context.Context) error {
	t.started = true
	t.startTime = time.Now()
	return t.startErr
}

func (t *TestMockComponent) Stop(_ time.Duration) error {
	t.stopped = true
	return t.stopErr
}

// TestComponentManagerConfigUpdates tests that config updates actually create/update/remove components
func TestComponentManagerConfigUpdates(t *testing.T) {
	ctx := context.Background()

	// Create real NATS test client with KV support
	testClient := natsclient.NewTestClient(t, natsclient.WithKV())
	defer testClient.Terminate()

	// Register a test component factory
	testFactoryCalled := 0
	var lastConfig json.RawMessage
	testFactory := func(config json.RawMessage, _ component.Dependencies) (component.Discoverable, error) {
		testFactoryCalled++
		lastConfig = config
		return &TestMockComponent{
			id:     fmt.Sprintf("test-mock-%d", testFactoryCalled),
			config: config,
		}, nil
	}

	// Get the default registry and register the factory
	registry := component.NewRegistry()
	err := registry.RegisterFactory("test-mock", &component.Registration{
		Name:        "test-mock",
		Type:        string(types.ComponentTypeProcessor),
		Protocol:    "test",
		Description: "Test mock component",
		Version:     "1.0.0",
		Factory:     testFactory,
	})
	require.NoError(t, err)
	defer func() {
		// Cleanup: unregister factory after test
		// Note: We can't directly unregister, so we rely on test isolation
	}()

	// Create initial configuration without the test component
	initialConfig := &config.Config{
		Platform: config.PlatformConfig{
			Org:         "test",
			ID:          "test-platform",
			InstanceID:  "test-001",
			Environment: "test",
		},
		Components: config.ComponentConfigs{
			// Start with no components
		},
	}

	// Create Manager
	configManager, err := config.NewConfigManager(initialConfig, testClient.Client, slog.Default())
	require.NoError(t, err)

	// Push initial config to KV before starting (required for watchers to work)
	err = configManager.PushToKV(ctx)
	require.NoError(t, err)

	// Now start Manager
	require.NoError(t, configManager.Start(ctx))
	defer configManager.Stop(5 * time.Second)

	// Get direct access to the KV bucket for config updates
	kv, err := testClient.Client.GetKeyValueBucket(ctx, "semstreams_config")
	require.NoError(t, err)

	// Create service dependencies with the registry we registered our test factory to
	deps := &service.Dependencies{
		NATSClient:        testClient.Client,
		Manager:           configManager,
		Logger:            slog.Default(),
		ComponentRegistry: registry, // Pass the registry with our test factory
	}

	// Create ComponentManager with config watching enabled
	cmConfig := json.RawMessage(`{"watch_config": true}`)
	cmService, err := service.NewComponentManager(cmConfig, deps)
	require.NoError(t, err)

	cm := cmService.(*service.ComponentManager)

	// Initialize and start ComponentManager
	err = cm.Initialize()
	require.NoError(t, err)

	err = cm.Start(ctx)
	require.NoError(t, err)
	defer cm.Stop(5 * time.Second)

	// Give the config watcher time to start and receive initial update
	time.Sleep(500 * time.Millisecond)
	t.Logf("ComponentManager started with config watching enabled")

	// Test 1: Add a new component via config update
	t.Run("Add new component via config", func(t *testing.T) {
		// Update config to add a new component
		newConfig := &config.Config{
			Platform: initialConfig.Platform,
			Components: config.ComponentConfigs{
				"test-component-1": types.ComponentConfig{
					Type:    types.ComponentTypeProcessor,
					Name:    "test-mock",
					Enabled: true,
					Config:  json.RawMessage(`{"setting":"value1"}`),
				},
			},
		}

		// Push config update via KV store
		for name, compConfig := range newConfig.Components {
			data, err := json.Marshal(compConfig)
			require.NoError(t, err)
			t.Logf("Pushing component config to KV: components.%s = %s", name, string(data))
			_, err = kv.Put(ctx, "components."+name, data)
			require.NoError(t, err)
		}

		// Wait for component to be created
		time.Sleep(2 * time.Second) // Increase wait time more

		// Debug logging
		t.Logf("After config update, components: %v", cm.ListComponents())
		t.Logf("Factory called %d times", testFactoryCalled)

		// Verify component was created
		components := cm.ListComponents()
		assert.Contains(t, components, "test-component-1", "Component should be created from config update")

		// Verify factory was called
		assert.Equal(t, 1, testFactoryCalled, "Factory should be called once")
		assert.JSONEq(t, `{"setting":"value1"}`, string(lastConfig), "Factory should receive correct config")
	})

	// Test 2: Update existing component's config
	t.Run("Update existing component config", func(t *testing.T) {
		// Reset factory call counter
		testFactoryCalled = 0

		// Update config with different settings
		updatedConfig := &config.Config{
			Platform: initialConfig.Platform,
			Components: config.ComponentConfigs{
				"test-component-1": types.ComponentConfig{
					Type:    types.ComponentTypeProcessor,
					Name:    "test-mock",
					Enabled: true,
					Config:  json.RawMessage(`{"setting":"value2"}`),
				},
			},
		}

		// Push config update via KV store
		for name, compConfig := range updatedConfig.Components {
			data, err := json.Marshal(compConfig)
			require.NoError(t, err)
			_, err = kv.Put(ctx, "components."+name, data)
			require.NoError(t, err)
		}

		// Wait for component to be restarted
		time.Sleep(500 * time.Millisecond)

		// Verify factory was called again (component restarted)
		assert.Equal(t, 1, testFactoryCalled, "Factory should be called for restart")
		assert.JSONEq(t, `{"setting":"value2"}`, string(lastConfig), "Factory should receive updated config")

		// Component should still exist
		components := cm.ListComponents()
		assert.Contains(t, components, "test-component-1", "Component should still exist after update")
	})

	// Test 3: Disable a component
	t.Run("Disable component via config", func(t *testing.T) {
		// Update config to disable the component
		disabledConfig := &config.Config{
			Platform: initialConfig.Platform,
			Components: config.ComponentConfigs{
				"test-component-1": types.ComponentConfig{
					Type:    types.ComponentTypeProcessor,
					Name:    "test-mock",
					Enabled: false, // Disabled
					Config:  json.RawMessage(`{"setting":"value2"}`),
				},
			},
		}

		// Push config update via KV store
		for name, compConfig := range disabledConfig.Components {
			data, err := json.Marshal(compConfig)
			require.NoError(t, err)
			_, err = kv.Put(ctx, "components."+name, data)
			require.NoError(t, err)
		}

		// Wait for component to be stopped
		time.Sleep(500 * time.Millisecond)

		// Verify component was removed
		components := cm.ListComponents()
		assert.NotContains(t, components, "test-component-1", "Disabled component should be removed")
	})

	// Test 4: Remove component from config entirely
	t.Run("Remove component from config", func(t *testing.T) {
		// First, add the component back
		enabledConfig := &config.Config{
			Platform: initialConfig.Platform,
			Components: config.ComponentConfigs{
				"test-component-2": types.ComponentConfig{
					Type:    types.ComponentTypeProcessor,
					Name:    "test-mock",
					Enabled: true,
					Config:  json.RawMessage(`{"setting":"value3"}`),
				},
			},
		}

		// Push config update via KV store
		for name, compConfig := range enabledConfig.Components {
			data, err := json.Marshal(compConfig)
			require.NoError(t, err)
			_, err = kv.Put(ctx, "components."+name, data)
			require.NoError(t, err)
		}
		time.Sleep(500 * time.Millisecond)

		// Verify it was created
		components := cm.ListComponents()
		assert.Contains(t, components, "test-component-2", "Component should be created")

		// Now remove it from config entirely
		// Clear all components from KV
		// Since it's empty, we need to delete the existing ones
		err = kv.Delete(ctx, "components.test-component-2")
		// Ignore not found errors
		if err != nil && err.Error() != "nats: key not found" {
			require.NoError(t, err)
		}
		time.Sleep(500 * time.Millisecond)

		// Verify component was removed
		components = cm.ListComponents()
		assert.NotContains(t, components, "test-component-2", "Component should be removed when not in config")
	})

	// Test 5: Add multiple components simultaneously
	t.Run("Add multiple components at once", func(t *testing.T) {
		multiConfig := &config.Config{
			Platform: initialConfig.Platform,
			Components: config.ComponentConfigs{
				"multi-comp-1": types.ComponentConfig{
					Type:    types.ComponentTypeProcessor,
					Name:    "test-mock",
					Enabled: true,
					Config:  json.RawMessage(`{"id":1}`),
				},
				"multi-comp-2": types.ComponentConfig{
					Type:    types.ComponentTypeProcessor,
					Name:    "test-mock",
					Enabled: true,
					Config:  json.RawMessage(`{"id":2}`),
				},
				"multi-comp-3": types.ComponentConfig{
					Type:    types.ComponentTypeProcessor,
					Name:    "test-mock",
					Enabled: false, // This one is disabled
					Config:  json.RawMessage(`{"id":3}`),
				},
			},
		}

		// Push config update via KV store
		for name, compConfig := range multiConfig.Components {
			data, err := json.Marshal(compConfig)
			require.NoError(t, err)
			_, err = kv.Put(ctx, "components."+name, data)
			require.NoError(t, err)
		}
		time.Sleep(500 * time.Millisecond)

		// Verify only enabled components were created
		components := cm.ListComponents()
		assert.Contains(t, components, "multi-comp-1", "Enabled component 1 should be created")
		assert.Contains(t, components, "multi-comp-2", "Enabled component 2 should be created")
		assert.NotContains(t, components, "multi-comp-3", "Disabled component should not be created")
	})
}

// TestComponentManagerConfigResilience tests error handling in config updates
func TestComponentManagerConfigResilience(t *testing.T) {
	ctx := context.Background()

	// Create real NATS test client
	testClient := natsclient.NewTestClient(t, natsclient.WithKV())
	defer testClient.Terminate()

	// Register a factory that can fail
	failOnCreate := false
	testFactory := func(config json.RawMessage, _ component.Dependencies) (component.Discoverable, error) {
		if failOnCreate {
			return nil, assert.AnError
		}
		return &TestMockComponent{
			id:     "test-resilient",
			config: config,
		}, nil
	}

	// Get the default registry and register the factory
	registry := component.NewRegistry()
	err := registry.RegisterFactory("test-resilient", &component.Registration{
		Name:        "test-resilient",
		Type:        string(types.ComponentTypeProcessor),
		Protocol:    "test",
		Description: "Test resilient component",
		Version:     "1.0.0",
		Factory:     testFactory,
	})
	require.NoError(t, err)

	// Create Manager
	configManager, err := config.NewConfigManager(&config.Config{
		Platform: config.PlatformConfig{
			Org:         "test",
			ID:          "test-platform",
			InstanceID:  "test-001",
			Environment: "test",
		},
	}, testClient.Client, slog.Default())
	require.NoError(t, err)

	// Push initial config to KV before starting (required for watchers to work)
	err = configManager.PushToKV(ctx)
	require.NoError(t, err)

	// Now start Manager
	require.NoError(t, configManager.Start(ctx))
	defer configManager.Stop(5 * time.Second)

	// Get direct access to the KV bucket for config updates
	kv, err := testClient.Client.GetKeyValueBucket(ctx, "semstreams_config")
	require.NoError(t, err)

	// Create ComponentManager with the registry we registered our test factory to
	deps := &service.Dependencies{
		NATSClient:        testClient.Client,
		Manager:           configManager,
		Logger:            slog.Default(),
		ComponentRegistry: registry, // Pass the registry with our test factory
	}

	// Create ComponentManager with config watching enabled
	cmConfig := json.RawMessage(`{"watch_config": true}`)
	cmService, err := service.NewComponentManager(cmConfig, deps)
	require.NoError(t, err)

	cm := cmService.(*service.ComponentManager)
	err = cm.Initialize()
	require.NoError(t, err)
	err = cm.Start(ctx)
	require.NoError(t, err)
	defer cm.Stop(5 * time.Second)

	// Give the config watcher time to start
	time.Sleep(500 * time.Millisecond)
	t.Logf("ComponentManager started with config watching enabled")

	t.Run("Component creation failure doesn't crash system", func(t *testing.T) {
		// Make factory fail
		failOnCreate = true

		// Try to add component that will fail
		failConfig := &config.Config{
			Platform: config.PlatformConfig{
				Org:         "test",
				ID:          "test-platform",
				InstanceID:  "test-001",
				Environment: "test",
			},
			Components: config.ComponentConfigs{
				"fail-component": types.ComponentConfig{
					Type:    types.ComponentTypeProcessor,
					Name:    "test-resilient",
					Enabled: true,
					Config:  json.RawMessage(`{}`),
				},
			},
		}

		// This should not panic or crash
		// Push config update via KV store
		for name, compConfig := range failConfig.Components {
			data, err := json.Marshal(compConfig)
			require.NoError(t, err)
			_, err = kv.Put(ctx, "components."+name, data)
			require.NoError(t, err)
		}

		time.Sleep(500 * time.Millisecond)

		// System should still be operational
		components := cm.ListComponents()
		assert.NotNil(t, components, "System should still be operational after component creation failure")

		// Component should not exist
		assert.NotContains(t, components, "fail-component", "Failed component should not be in list")

		// Now fix the factory and retry
		failOnCreate = false

		// Push same config again via KV store
		for name, compConfig := range failConfig.Components {
			data, err := json.Marshal(compConfig)
			require.NoError(t, err)
			_, err = kv.Put(ctx, "components."+name, data)
			require.NoError(t, err)
		}

		time.Sleep(500 * time.Millisecond)

		// Component should now be created
		components = cm.ListComponents()
		assert.Contains(t, components, "fail-component", "Component should be created after retry")
	})
}
