//go:build integration

package service_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/config"
	"github.com/c360studio/semstreams/model"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/service"
	"github.com/c360studio/semstreams/types"
	"github.com/stretchr/testify/require"
)

// TestComponentManager_RestartsDependentsOnModelRegistryChange verifies the
// load-bearing contract behind declarative registration dependencies:
// when the model_registry KV key changes, ComponentManager restarts
// exactly the components whose factories declared
// component.DepModelRegistry, and leaves other components untouched.
//
// This replaces the runtime Handle/Subscribe machinery from Phase 3.
// Components that cache registry-derived state at Start() (LLM clients,
// summarizers, embedders) get a clean rebuild; components that don't
// care about model_registry see no disruption.
func TestComponentManager_RestartsDependentsOnModelRegistryChange(t *testing.T) {
	ctx := context.Background()

	testClient := natsclient.NewTestClient(t, natsclient.WithKV())
	defer testClient.Terminate()

	// Two test factories sharing one set of counters. The "dep" factory
	// declares DepModelRegistry; the "nodep" factory declares nothing.
	// Each call to the factory increments its counter so we can assert
	// who was restarted.
	var mu sync.Mutex
	depCalls := 0
	nodepCalls := 0

	depFactory := func(_ json.RawMessage, _ component.Dependencies) (component.Discoverable, error) {
		mu.Lock()
		defer mu.Unlock()
		depCalls++
		return &registryTestComponent{}, nil
	}
	nodepFactory := func(_ json.RawMessage, _ component.Dependencies) (component.Discoverable, error) {
		mu.Lock()
		defer mu.Unlock()
		nodepCalls++
		return &registryTestComponent{}, nil
	}

	registry := component.NewRegistry()
	require.NoError(t, registry.RegisterFactory("dep-factory", &component.Registration{
		Name:         "dep-factory",
		Type:         string(types.ComponentTypeProcessor),
		Factory:      depFactory,
		Dependencies: []string{component.DepModelRegistry},
	}))
	require.NoError(t, registry.RegisterFactory("nodep-factory", &component.Registration{
		Name:    "nodep-factory",
		Type:    string(types.ComponentTypeProcessor),
		Factory: nodepFactory,
		// No Dependencies — must not be restarted on model_registry change.
	}))

	initialCfg := &config.Config{
		Platform: config.PlatformConfig{
			Org:         "test",
			ID:          "test-platform",
			InstanceID:  "test-001",
			Environment: "test",
		},
		ModelRegistry: &model.Registry{
			Endpoints: map[string]*model.EndpointConfig{
				"ep-a": {Provider: "ollama", URL: "http://a/v1", Model: "a", MaxTokens: 1024},
			},
			Defaults: model.DefaultsConfig{Model: "ep-a"},
		},
		Components: config.ComponentConfigs{
			"dep-instance": types.ComponentConfig{
				Type:    types.ComponentTypeProcessor,
				Name:    "dep-factory",
				Enabled: true,
				Config:  json.RawMessage(`{}`),
			},
			"nodep-instance": types.ComponentConfig{
				Type:    types.ComponentTypeProcessor,
				Name:    "nodep-factory",
				Enabled: true,
				Config:  json.RawMessage(`{}`),
			},
		},
	}

	configManager, err := config.NewConfigManager(initialCfg, testClient.Client, slog.Default())
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

	// Confirm both components were built once at Initialize.
	time.Sleep(200 * time.Millisecond)
	mu.Lock()
	initialDep := depCalls
	initialNodep := nodepCalls
	mu.Unlock()
	require.Equal(t, 1, initialDep, "dep-instance factory should be called once at Initialize")
	require.Equal(t, 1, initialNodep, "nodep-instance factory should be called once at Initialize")

	// Write a new model_registry. ComponentManager's watcher must see
	// it and restart only the dependent component.
	updatedRegistry := &model.Registry{
		Endpoints: map[string]*model.EndpointConfig{
			"ep-b": {Provider: "ollama", URL: "http://b/v1", Model: "b", MaxTokens: 2048},
		},
		Defaults: model.DefaultsConfig{Model: "ep-b"},
	}
	data, err := json.Marshal(updatedRegistry)
	require.NoError(t, err)
	_, err = kv.Put(ctx, "model_registry", data)
	require.NoError(t, err)

	// Poll up to 2s for the restart to land. Going directly to an
	// explicit expected count avoids flake on slow CI.
	deadline := time.Now().Add(2 * time.Second)
	for {
		mu.Lock()
		currentDep := depCalls
		currentNodep := nodepCalls
		mu.Unlock()

		if currentDep == 2 && currentNodep == 1 {
			return // success
		}
		if time.Now().After(deadline) {
			t.Fatalf("after model_registry update, got depCalls=%d (want 2), nodepCalls=%d (want 1)",
				currentDep, currentNodep)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// registryTestComponent is a minimal component that satisfies the
// Discoverable + LifecycleComponent interfaces so ComponentManager can
// Start and Stop it during the test.
//
// startMu serializes access to startTime — Start runs on a
// component-manager launch goroutine (startComponent), while Health is
// invoked from the publishHealthLoop goroutine. Without the mutex,
// the race detector flags the unsynchronized read/write pair under
// -race + -tags=integration.
type registryTestComponent struct {
	startMu   sync.RWMutex
	startTime time.Time
}

func (c *registryTestComponent) Meta() component.Metadata {
	return component.Metadata{
		Name:        "registry-test",
		Type:        string(types.ComponentTypeProcessor),
		Description: "test component for registry dep routing",
		Version:     "1.0.0",
	}
}
func (c *registryTestComponent) InputPorts() []component.Port  { return nil }
func (c *registryTestComponent) OutputPorts() []component.Port { return nil }
func (c *registryTestComponent) ConfigSchema() component.ConfigSchema {
	return component.ConfigSchema{}
}
func (c *registryTestComponent) Health() component.HealthStatus {
	c.startMu.RLock()
	uptime := time.Since(c.startTime)
	c.startMu.RUnlock()
	return component.HealthStatus{Healthy: true, LastCheck: time.Now(), Uptime: uptime}
}
func (c *registryTestComponent) DataFlow() component.FlowMetrics {
	return component.FlowMetrics{LastActivity: time.Now()}
}
func (c *registryTestComponent) Initialize() error { return nil }
func (c *registryTestComponent) Start(_ context.Context) error {
	c.startMu.Lock()
	c.startTime = time.Now()
	c.startMu.Unlock()
	return nil
}
func (c *registryTestComponent) Stop(_ time.Duration) error { return nil }

var _ component.Discoverable = (*registryTestComponent)(nil)
var _ component.LifecycleComponent = (*registryTestComponent)(nil)
