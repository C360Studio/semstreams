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

	// Each factory publishes the exact instance it creates. The test waits for
	// that instance's Start call to return instead of treating factory creation
	// as a proxy for lifecycle completion.
	depInstances := make(chan *registryTestComponent, 2)
	nodepInstances := make(chan *registryTestComponent, 2)

	depFactory := func(_ json.RawMessage, _ component.Dependencies) (component.Discoverable, error) {
		instance := newRegistryTestComponent()
		depInstances <- instance
		return instance, nil
	}
	nodepFactory := func(_ json.RawMessage, _ component.Dependencies) (component.Discoverable, error) {
		instance := newRegistryTestComponent()
		nodepInstances <- instance
		return instance, nil
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
	defer cm.Stop(context.Background())

	// The boot barrier has returned, so both initial instance Starts must also
	// have returned. Assert that contract explicitly with bounded channel waits.
	initialDep := waitForRegistryTestInstance(t, depInstances, "initial dependent instance")
	waitForRegistryTestStart(t, initialDep, "initial dependent instance")
	initialNodep := waitForRegistryTestInstance(t, nodepInstances, "initial non-dependent instance")
	waitForRegistryTestStart(t, initialNodep, "initial non-dependent instance")

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

	// Wait for the replacement's Start call itself to return. Factory creation
	// alone is too early: ComponentManager launches dynamic Start asynchronously.
	replacement := waitForRegistryTestInstance(t, depInstances, "replacement dependent instance")
	waitForRegistryTestStart(t, replacement, "replacement dependent instance")

	select {
	case <-nodepInstances:
		t.Fatal("non-dependent component was rebuilt after model_registry update")
	default:
	}
}

func waitForRegistryTestInstance(
	t *testing.T, instances <-chan *registryTestComponent, description string,
) *registryTestComponent {
	t.Helper()
	select {
	case instance := <-instances:
		return instance
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s to be created", description)
		return nil
	}
}

func waitForRegistryTestStart(t *testing.T, instance *registryTestComponent, description string) {
	t.Helper()
	select {
	case <-instance.startReturned:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s Start to return", description)
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
	startMu       sync.RWMutex
	startTime     time.Time
	startReturned chan struct{}
}

func newRegistryTestComponent() *registryTestComponent {
	return &registryTestComponent{startReturned: make(chan struct{})}
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
	defer close(c.startReturned)
	c.startMu.Lock()
	c.startTime = time.Now()
	c.startMu.Unlock()
	return nil
}
func (c *registryTestComponent) Stop(context.Context) error { return nil }

var _ component.Discoverable = (*registryTestComponent)(nil)
var _ component.LifecycleComponent = (*registryTestComponent)(nil)
