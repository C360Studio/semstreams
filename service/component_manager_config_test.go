//go:build integration

package service_test

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/config"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/service"
	"github.com/c360studio/semstreams/types"
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
	// Build tag ensures this only runs with -tags=integration
	ctx := context.Background()

	// Create real NATS test client with KV support
	testClient := natsclient.NewTestClient(t, natsclient.WithKV())
	defer testClient.Terminate()

	// Register a test component factory
	var mu sync.RWMutex
	testFactoryCalled := 0
	var lastConfig json.RawMessage
	testFactory := func(config json.RawMessage, _ component.Dependencies) (component.Discoverable, error) {
		mu.Lock()
		defer mu.Unlock()
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
		mu.RLock()
		factoryCalls := testFactoryCalled
		configCopy := lastConfig
		mu.RUnlock()
		t.Logf("Factory called %d times", factoryCalls)

		// Verify component was created
		components := cm.ListComponents()
		assert.Contains(t, components, "test-component-1", "Component should be created from config update")

		// Verify factory was called
		assert.Equal(t, 1, factoryCalls, "Factory should be called once")
		assert.JSONEq(t, `{"setting":"value1"}`, string(configCopy), "Factory should receive correct config")
	})

	// Test 2: Update existing component's config
	t.Run("Update existing component config", func(t *testing.T) {
		// Reset factory call counter
		mu.Lock()
		testFactoryCalled = 0
		mu.Unlock()

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
		mu.RLock()
		factoryCalls := testFactoryCalled
		configCopy := lastConfig
		mu.RUnlock()
		assert.Equal(t, 1, factoryCalls, "Factory should be called for restart")
		assert.JSONEq(t, `{"setting":"value2"}`, string(configCopy), "Factory should receive updated config")

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
		// Add delay between puts to ensure KV watcher processes each update.
		// Rapid puts can cause the watcher to miss updates in CI.
		for name, compConfig := range multiConfig.Components {
			data, err := json.Marshal(compConfig)
			require.NoError(t, err)
			_, err = kv.Put(ctx, "components."+name, data)
			require.NoError(t, err)
			time.Sleep(50 * time.Millisecond)
		}

		// Wait for components to be created (replace arbitrary sleep with proper wait)
		require.Eventually(t, func() bool {
			components := cm.ListComponents()
			return len(components) >= 2 && // Expect 2 enabled components
				components["multi-comp-1"] != nil &&
				components["multi-comp-2"] != nil
		}, 5*time.Second, 100*time.Millisecond, "Components should be created within timeout")

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
	var failOnCreate atomic.Bool
	var factoryAttempts atomic.Uint64
	testFactory := func(config json.RawMessage, _ component.Dependencies) (component.Discoverable, error) {
		factoryAttempts.Add(1)
		if failOnCreate.Load() {
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

	t.Logf("ComponentManager started with config watching enabled")

	t.Run("Component creation failure doesn't crash system", func(t *testing.T) {
		// Make factory fail
		failOnCreate.Store(true)
		failedAttemptBaseline := factoryAttempts.Load()

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

		require.Eventually(t, func() bool {
			_, present := cm.ListComponents()["fail-component"]
			return factoryAttempts.Load() > failedAttemptBaseline && !present
		}, 5*time.Second, 20*time.Millisecond,
			"factory failure should be observed without admitting the component")

		// System should still be operational
		components := cm.ListComponents()
		assert.NotNil(t, components, "System should still be operational after component creation failure")

		// Component should not exist
		assert.NotContains(t, components, "fail-component", "Failed component should not be in list")

		// Now fix the factory and retry
		failOnCreate.Store(false)
		successAttemptBaseline := factoryAttempts.Load()

		// Push same config again via KV store
		for name, compConfig := range failConfig.Components {
			data, err := json.Marshal(compConfig)
			require.NoError(t, err)
			_, err = kv.Put(ctx, "components."+name, data)
			require.NoError(t, err)
		}

		require.Eventually(t, func() bool {
			_, present := cm.ListComponents()["fail-component"]
			return factoryAttempts.Load() > successAttemptBaseline && present
		}, 5*time.Second, 20*time.Millisecond, "component should be created after retry")
	})
}

// TestComponentManagerReconcileOnBulkPush tests that PushToKV triggers reconciliation
// so all components are created even when individual KV watcher notifications are dropped.
func TestComponentManagerReconcileOnBulkPush(t *testing.T) {
	ctx := context.Background()

	testClient := natsclient.NewTestClient(t, natsclient.WithKV())
	defer testClient.Terminate()

	// Register test factory
	testFactory := func(cfg json.RawMessage, _ component.Dependencies) (component.Discoverable, error) {
		return &TestMockComponent{
			id:     "test-bulk",
			config: cfg,
		}, nil
	}

	registry := component.NewRegistry()
	err := registry.RegisterFactory("test-bulk", &component.Registration{
		Name:        "test-bulk",
		Type:        string(types.ComponentTypeProcessor),
		Protocol:    "test",
		Description: "Test bulk component",
		Version:     "1.0.0",
		Factory:     testFactory,
	})
	require.NoError(t, err)

	// Build a config with 20 enabled components — enough to overflow buffer=1
	components := make(config.ComponentConfigs, 20)
	for i := range 20 {
		components[fmt.Sprintf("bulk-comp-%02d", i)] = types.ComponentConfig{
			Type:    types.ComponentTypeProcessor,
			Name:    "test-bulk",
			Enabled: true,
			Config:  json.RawMessage(fmt.Sprintf(`{"id":%d}`, i)),
		}
	}
	// Add 2 disabled components to verify they're NOT created
	components["bulk-disabled-1"] = types.ComponentConfig{
		Type:    types.ComponentTypeProcessor,
		Name:    "test-bulk",
		Enabled: false,
		Config:  json.RawMessage(`{"id":"disabled-1"}`),
	}
	components["bulk-disabled-2"] = types.ComponentConfig{
		Type:    types.ComponentTypeProcessor,
		Name:    "test-bulk",
		Enabled: false,
		Config:  json.RawMessage(`{"id":"disabled-2"}`),
	}

	initialConfig := &config.Config{
		Version: "1.0.0",
		Platform: config.PlatformConfig{
			Org:         "test",
			ID:          "test-platform",
			InstanceID:  "test-001",
			Environment: "test",
		},
		// Start empty — components will arrive via PushToKV
		Components: config.ComponentConfigs{},
	}

	// Create and start config Manager
	configManager, err := config.NewConfigManager(initialConfig, testClient.Client, slog.Default())
	require.NoError(t, err)

	// Push empty config first so watchers can be created
	require.NoError(t, configManager.PushToKV(ctx))
	require.NoError(t, configManager.Start(ctx))
	defer configManager.Stop(5 * time.Second)

	// Create ComponentManager with config watching enabled
	deps := &service.Dependencies{
		NATSClient:        testClient.Client,
		Manager:           configManager,
		Logger:            slog.Default(),
		ComponentRegistry: registry,
	}

	cmService, err := service.NewComponentManager(json.RawMessage(`{"watch_config": true}`), deps)
	require.NoError(t, err)

	cm := cmService.(*service.ComponentManager)
	require.NoError(t, cm.Initialize())
	require.NoError(t, cm.Start(ctx))
	defer cm.Stop(5 * time.Second)

	// Give watcher goroutine time to start
	time.Sleep(200 * time.Millisecond)

	// Now update the SafeConfig with all 22 components and bulk-push to KV.
	// This is the codepath that was dropping notifications.
	fullConfig := configManager.GetConfig()
	cfg := fullConfig.Get()
	cfg.Components = components
	require.NoError(t, fullConfig.Update(cfg))
	require.NoError(t, configManager.PushToKV(ctx))

	// All 20 enabled components should eventually be created via reconciliation
	require.Eventually(t, func() bool {
		comps := cm.ListComponents()
		return len(comps) >= 20
	}, 10*time.Second, 200*time.Millisecond,
		"All 20 enabled components should be created via reconciliation")

	// Verify disabled components were NOT created
	comps := cm.ListComponents()
	assert.NotContains(t, comps, "bulk-disabled-1", "Disabled component 1 should not be created")
	assert.NotContains(t, comps, "bulk-disabled-2", "Disabled component 2 should not be created")

	// Verify all 20 enabled components exist
	for i := range 20 {
		name := fmt.Sprintf("bulk-comp-%02d", i)
		assert.Contains(t, comps, name, "Component %s should exist", name)
	}

	t.Logf("Reconciliation created %d components from bulk push of 22 configs (20 enabled, 2 disabled)", len(comps))
}

// TestComponentManagerNoOpConfigUpdateSkipsRestart verifies the gh#520
// idempotency guard: a per-component config update whose effective config is
// unchanged does NOT restart the running component, while a genuinely changed
// config restarts it exactly once. Restart is observed via factory re-invocation
// (a restart tears down the old instance and builds a new one via the factory).
func TestComponentManagerNoOpConfigUpdateSkipsRestart(t *testing.T) {
	ctx := context.Background()

	testClient := natsclient.NewTestClient(t, natsclient.WithKV())
	defer testClient.Terminate()

	var mu sync.Mutex
	factoryCalls := 0
	testFactory := func(cfg json.RawMessage, _ component.Dependencies) (component.Discoverable, error) {
		mu.Lock()
		factoryCalls++
		id := factoryCalls
		mu.Unlock()
		return &TestMockComponent{id: fmt.Sprintf("noop-%d", id), config: cfg}, nil
	}
	calls := func() int { mu.Lock(); defer mu.Unlock(); return factoryCalls }

	registry := component.NewRegistry()
	require.NoError(t, registry.RegisterFactory("test-noop", &component.Registration{
		Name:        "test-noop",
		Type:        string(types.ComponentTypeProcessor),
		Protocol:    "test",
		Description: "Test no-op component",
		Version:     "1.0.0",
		Factory:     testFactory,
	}))

	initialConfig := &config.Config{
		Version:    "1.0.0",
		Platform:   config.PlatformConfig{Org: "test", ID: "test-platform", InstanceID: "test-001", Environment: "test"},
		Components: config.ComponentConfigs{},
	}

	configManager, err := config.NewConfigManager(initialConfig, testClient.Client, slog.Default())
	require.NoError(t, err)
	require.NoError(t, configManager.PushToKV(ctx))
	require.NoError(t, configManager.Start(ctx))
	defer configManager.Stop(5 * time.Second)

	kv, err := testClient.Client.GetKeyValueBucket(ctx, "semstreams_config")
	require.NoError(t, err)

	deps := &service.Dependencies{
		NATSClient:        testClient.Client,
		Manager:           configManager,
		Logger:            slog.Default(),
		ComponentRegistry: registry,
	}
	cmService, err := service.NewComponentManager(json.RawMessage(`{"watch_config": true}`), deps)
	require.NoError(t, err)
	cm := cmService.(*service.ComponentManager)
	require.NoError(t, cm.Initialize())
	require.NoError(t, cm.Start(ctx))
	defer cm.Stop(5 * time.Second)
	time.Sleep(300 * time.Millisecond)

	compCfg := types.ComponentConfig{
		Type:    types.ComponentTypeProcessor,
		Name:    "test-noop",
		Enabled: true,
		Config:  json.RawMessage(`{"setting":"a"}`),
	}
	put := func(c types.ComponentConfig) {
		data, err := json.Marshal(c)
		require.NoError(t, err)
		_, err = kv.Put(ctx, "components.noop-1", data)
		require.NoError(t, err)
	}

	// Create the component.
	put(compCfg)
	require.Eventually(t, func() bool {
		_, ok := cm.ListComponents()["noop-1"]
		return ok
	}, 5*time.Second, 100*time.Millisecond, "component should be created")
	require.Equal(t, 1, calls(), "factory called once for initial create")

	// Push the IDENTICAL config (re-marshaled — exercises canonical compare).
	put(compCfg)
	// And a key-reordered/whitespaced form of the same config.
	reordered := compCfg
	reordered.Config = json.RawMessage("{ \"setting\" : \"a\" }")
	put(reordered)
	// Negative assertion (absence of a restart): sleep well past the
	// put→watcher→handleComponentConfigUpdate latency (the 300ms warm-up above
	// shows that path completes in well under a second) so a restart, if the
	// guard were broken, would have incremented the factory count by now.
	time.Sleep(1 * time.Second)
	assert.Equal(t, 1, calls(), "no-op config updates must NOT restart (factory count unchanged)")
	assert.Contains(t, cm.ListComponents(), "noop-1", "component still present after no-op updates")

	// Push a CHANGED config — exactly one restart.
	changed := compCfg
	changed.Config = json.RawMessage(`{"setting":"b"}`)
	put(changed)
	require.Eventually(t, func() bool { return calls() == 2 }, 5*time.Second, 100*time.Millisecond,
		"changed config must restart exactly once (factory called a 2nd time)")
	time.Sleep(500 * time.Millisecond)
	assert.Equal(t, 2, calls(), "changed config restarts exactly once, not repeatedly")
}

// TestComponentManagerRepushUnchangedRestartsNothing verifies that re-pushing an
// unchanged full config (which fans out per-component notifications AND a bulk
// reconcile) restarts none of the running components (gh#520).
func TestComponentManagerRepushUnchangedRestartsNothing(t *testing.T) {
	ctx := context.Background()

	testClient := natsclient.NewTestClient(t, natsclient.WithKV())
	defer testClient.Terminate()

	var mu sync.Mutex
	factoryCalls := 0
	testFactory := func(cfg json.RawMessage, _ component.Dependencies) (component.Discoverable, error) {
		mu.Lock()
		factoryCalls++
		id := factoryCalls
		mu.Unlock()
		return &TestMockComponent{id: fmt.Sprintf("repush-%d", id), config: cfg}, nil
	}
	calls := func() int { mu.Lock(); defer mu.Unlock(); return factoryCalls }

	registry := component.NewRegistry()
	require.NoError(t, registry.RegisterFactory("test-repush", &component.Registration{
		Name:        "test-repush",
		Type:        string(types.ComponentTypeProcessor),
		Protocol:    "test",
		Description: "Test repush component",
		Version:     "1.0.0",
		Factory:     testFactory,
	}))

	components := make(config.ComponentConfigs, 5)
	for i := range 5 {
		components[fmt.Sprintf("repush-%02d", i)] = types.ComponentConfig{
			Type:    types.ComponentTypeProcessor,
			Name:    "test-repush",
			Enabled: true,
			Config:  json.RawMessage(fmt.Sprintf(`{"id":%d}`, i)),
		}
	}

	initialConfig := &config.Config{
		Version:    "1.0.0",
		Platform:   config.PlatformConfig{Org: "test", ID: "test-platform", InstanceID: "test-001", Environment: "test"},
		Components: config.ComponentConfigs{},
	}
	configManager, err := config.NewConfigManager(initialConfig, testClient.Client, slog.Default())
	require.NoError(t, err)
	require.NoError(t, configManager.PushToKV(ctx))
	require.NoError(t, configManager.Start(ctx))
	defer configManager.Stop(5 * time.Second)

	deps := &service.Dependencies{
		NATSClient:        testClient.Client,
		Manager:           configManager,
		Logger:            slog.Default(),
		ComponentRegistry: registry,
	}
	cmService, err := service.NewComponentManager(json.RawMessage(`{"watch_config": true}`), deps)
	require.NoError(t, err)
	cm := cmService.(*service.ComponentManager)
	require.NoError(t, cm.Initialize())
	require.NoError(t, cm.Start(ctx))
	defer cm.Stop(5 * time.Second)
	time.Sleep(200 * time.Millisecond)

	// Create the 5 components via the full config.
	fullConfig := configManager.GetConfig()
	cfg := fullConfig.Get()
	cfg.Components = components
	require.NoError(t, fullConfig.Update(cfg))
	require.NoError(t, configManager.PushToKV(ctx))
	require.Eventually(t, func() bool { return len(cm.ListComponents()) >= 5 }, 10*time.Second, 200*time.Millisecond,
		"all 5 components created")
	require.Equal(t, 5, calls(), "5 factory calls for the 5 creates")

	// Re-push the SAME full config. This exercises the guard via the individual
	// per-component notifications (components.<name>), which without the guard
	// would each restart; the coalesced components.* bulk that may also fire is
	// reconcile-only (never restarts running components), so it can't mask a
	// broken guard — the sibling TestComponentManagerNoOpConfigUpdateSkipsRestart
	// pins the individual-notification path with explicit single-key puts.
	require.NoError(t, configManager.PushToKV(ctx))
	// Negative assertion: sleep past the notification-handling latency so any
	// spurious restart would have re-invoked the factory by the time we assert.
	time.Sleep(2 * time.Second)
	assert.Equal(t, 5, calls(), "re-pushing unchanged config must restart nothing (no extra factory calls)")
	assert.Len(t, cm.ListComponents(), 5, "still 5 components after unchanged re-push")
}
