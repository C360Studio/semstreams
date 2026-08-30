//go:build integration

package config

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/c360studio/semstreams/model"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigManager_PatternMatching(t *testing.T) {
	// Create a minimal config
	cfg := &Config{
		Version:    "1.0.0",
		Services:   make(types.ServiceConfigs),
		Components: make(ComponentConfigs),
	}

	// Create a test NATS client
	client := natsclient.NewTestClient(t, natsclient.WithJetStream())
	// TestClient uses t.Cleanup() automatically

	// Create Manager
	cm, err := NewConfigManager(cfg, client.Client, nil)
	require.NoError(t, err)
	require.NotNil(t, cm)

	tests := []struct {
		name     string
		key      string
		pattern  string
		expected bool
	}{
		{"exact match", "services.metrics", "services.metrics", true},
		{"wildcard suffix all services", "services.metrics", "services.*", true},
		{"wildcard suffix all components", "components.udp-sensor", "components.*", true},
		{"prefix wildcard", "components.udp-sensor-1", "components.udp-*", true},
		{"prefix wildcard no match", "components.tcp-sensor", "components.udp-*", false},
		{"no match different section", "services.metrics", "components.*", false},
		{"no match wrong exact", "services.metrics", "services.discovery", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := cm.matchesPattern(tt.key, tt.pattern)
			assert.Equal(t, tt.expected, result, "pattern %s matching key %s", tt.pattern, tt.key)
		})
	}
}

func TestConfigManager_Subscriptions(t *testing.T) {
	// Create a test config
	cfg := &Config{
		Version: "1.0.0",
		Platform: PlatformConfig{
			Org:  "c360",
			ID:   "test-platform",
			Type: "test",
		},
		Services: types.ServiceConfigs{
			"metrics": types.ServiceConfig{Enabled: true, Config: json.RawMessage(`{"port": 9090}`)},
		},
		Components: ComponentConfigs{
			"udp-sensor": types.ComponentConfig{
				Type:    "input",
				Name:    "udp",
				Enabled: true,
				Config:  json.RawMessage(`{"port": 8080}`),
			},
		},
	}

	// Create a test NATS client
	client := natsclient.NewTestClient(t, natsclient.WithJetStream())
	// TestClient uses t.Cleanup() automatically

	// Create Manager
	cm, err := NewConfigManager(cfg, client.Client, nil)
	require.NoError(t, err)

	// Start Manager
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	err = cm.Start(ctx)
	require.NoError(t, err)
	defer cm.Stop(5 * time.Second)

	// Subscribe to service changes
	serviceUpdates := cm.OnChange("services.*")
	require.NotNil(t, serviceUpdates)

	// Subscribe to component changes
	componentUpdates := cm.OnChange("components.*")
	require.NotNil(t, componentUpdates)

	// Should receive initial config immediately
	select {
	case update := <-serviceUpdates:
		assert.Equal(t, "services.*", update.Path)
		assert.NotNil(t, update.Config)
		currentCfg := update.Config.Get()
		assert.NotNil(t, currentCfg.Services["metrics"])
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout waiting for initial service config")
	}

	select {
	case update := <-componentUpdates:
		assert.Equal(t, "components.*", update.Path)
		assert.NotNil(t, update.Config)
		currentCfg := update.Config.Get()
		assert.NotNil(t, currentCfg.Components["udp-sensor"])
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout waiting for initial component config")
	}
}

func TestConfigManager_KVUpdates(t *testing.T) {
	// Skip if not using testcontainers
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Create initial config with required fields
	cfg := &Config{
		Version: "1.0.0",
		Platform: PlatformConfig{
			Org:  "c360",
			ID:   "test-platform",
			Type: "test",
		},
		Services: types.ServiceConfigs{
			"metrics": types.ServiceConfig{Enabled: true, Config: json.RawMessage(`{"port": 9090}`)},
		},
		Components: make(ComponentConfigs),
	}

	// Create a test NATS client with real NATS
	client := natsclient.NewTestClient(t, natsclient.WithJetStream())
	// TestClient uses t.Cleanup() automatically

	// Create Manager
	cm, err := NewConfigManager(cfg, client.Client, nil)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Push initial config to KV before starting watcher
	err = cm.PushToKV(ctx)
	require.NoError(t, err)
	// A bucket that already holds configuration must also hold its identity
	// record, or Start refuses it as predating identity minting (ADR-104).
	seedDeclaredIdentity(t, ctx, cm)

	// Start Manager
	// This will detect existing KV and sync from it
	err = cm.Start(ctx)
	require.NoError(t, err)
	defer cm.Stop(5 * time.Second)

	// Subscribe to service updates AFTER starting
	// OnChange will send current config immediately
	updates := cm.OnChange("services.metrics")

	// Should receive initial config from OnChange
	select {
	case <-updates:
		// Got initial config from OnChange
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout waiting for initial config from OnChange")
	}

	// Update config via KV
	newConfig := json.RawMessage(`{"enabled":false,"config":{"port":9090}}`)
	_, err = cm.kv.Put(ctx, "services.metrics", newConfig)
	require.NoError(t, err)

	// Should receive update
	select {
	case update := <-updates:
		assert.Equal(t, "services.metrics", update.Path)
		currentCfg := update.Config.Get()

		// Verify the config was updated
		metricsService := currentCfg.Services["metrics"]
		assert.Equal(t, false, metricsService.Enabled)

	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for config update")
	}
}

func TestConfigManager_PushToKV(t *testing.T) {
	// Create a config to push
	cfg := &Config{
		Version: "1.0.0",
		Platform: PlatformConfig{
			Org: "test-org",
			ID:  "test-id",
		},
		Services: types.ServiceConfigs{
			"metrics": types.ServiceConfig{Enabled: true, Config: json.RawMessage(`{}`)},

			"discovery": types.ServiceConfig{Enabled: false, Config: json.RawMessage(`{"port": 8080}`)},
		},
		Components: ComponentConfigs{
			"udp-sensor": types.ComponentConfig{
				Type:    "input",
				Name:    "udp",
				Enabled: true,
				Config:  json.RawMessage(`{"port": 8080}`),
			},
		},
		ModelRegistry: &model.Registry{
			Endpoints: map[string]*model.EndpointConfig{
				"ollama-coder": {
					Provider:  "ollama",
					Model:     "qwen2.5-coder:7b",
					URL:       "http://localhost:11434/v1",
					MaxTokens: 32768,
				},
			},
			Capabilities: map[string]*model.CapabilityConfig{
				"agent-work": {
					Preferred:     []string{"ollama-coder"},
					RequiresTools: false,
				},
			},
			Defaults: model.DefaultsConfig{
				Model:      "ollama-coder",
				Capability: "agent-work",
			},
		},
	}

	// Create test NATS client with JetStream enabled
	client := natsclient.NewTestClient(t, natsclient.WithJetStream())
	// TestClient uses t.Cleanup() automatically

	// Create Manager
	cm, err := NewConfigManager(cfg, client.Client, nil)
	require.NoError(t, err)

	ctx := context.Background()

	// Push config to KV
	err = cm.PushToKV(ctx)
	require.NoError(t, err)

	// Verify services were pushed
	entry, err := cm.kv.Get(ctx, "services.metrics")
	require.NoError(t, err)
	var metricsConfig types.ServiceConfig
	err = json.Unmarshal(entry.Value(), &metricsConfig)
	require.NoError(t, err)
	assert.True(t, metricsConfig.Enabled)

	entry, err = cm.kv.Get(ctx, "services.discovery")
	require.NoError(t, err)
	var discoveryConfig types.ServiceConfig
	err = json.Unmarshal(entry.Value(), &discoveryConfig)
	require.NoError(t, err)
	assert.False(t, discoveryConfig.Enabled)

	// Verify discovery config contains port
	var discoveryInnerConfig map[string]any
	err = json.Unmarshal(discoveryConfig.Config, &discoveryInnerConfig)
	require.NoError(t, err)
	assert.Equal(t, float64(8080), discoveryInnerConfig["port"])

	// Verify components were pushed
	entry, err = cm.kv.Get(ctx, "components.udp-sensor")
	require.NoError(t, err)

	var compConfig types.ComponentConfig
	err = json.Unmarshal(entry.Value(), &compConfig)
	require.NoError(t, err)
	assert.Equal(t, types.ComponentType("input"), compConfig.Type)
	assert.Equal(t, "udp", compConfig.Name)
	assert.True(t, compConfig.Enabled)

	// Verify platform was pushed
	entry, err = cm.kv.Get(ctx, "platform")
	require.NoError(t, err)

	var platformConfig PlatformConfig
	err = json.Unmarshal(entry.Value(), &platformConfig)
	require.NoError(t, err)
	assert.Equal(t, "test-org", platformConfig.Org)
	assert.Equal(t, "test-id", platformConfig.ID)

	// Verify model registry was pushed
	entry, err = cm.kv.Get(ctx, "model_registry")
	require.NoError(t, err)

	var registry model.Registry
	err = json.Unmarshal(entry.Value(), &registry)
	require.NoError(t, err)
	assert.Contains(t, registry.Endpoints, "ollama-coder")
	assert.Equal(t, "ollama", registry.Endpoints["ollama-coder"].Provider)
	assert.Equal(t, 32768, registry.Endpoints["ollama-coder"].MaxTokens)
	assert.Contains(t, registry.Capabilities, "agent-work")
	assert.Equal(t, "ollama-coder", registry.Defaults.Model)
}

func TestConfigManager_ModelRegistryKVUpdate(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	cfg := &Config{
		Version: "1.0.0",
		Platform: PlatformConfig{
			Org:  "c360",
			ID:   "test-platform",
			Type: "test",
		},
		Services:   make(types.ServiceConfigs),
		Components: make(ComponentConfigs),
		ModelRegistry: &model.Registry{
			Endpoints: map[string]*model.EndpointConfig{
				"ollama-coder": {
					Provider:  "ollama",
					Model:     "qwen2.5-coder:7b",
					URL:       "http://localhost:11434/v1",
					MaxTokens: 32768,
				},
			},
			Capabilities: map[string]*model.CapabilityConfig{
				"agent-work": {
					Preferred:     []string{"ollama-coder"},
					RequiresTools: false,
				},
			},
			Defaults: model.DefaultsConfig{
				Model:      "ollama-coder",
				Capability: "agent-work",
			},
		},
	}

	client := natsclient.NewTestClient(t, natsclient.WithJetStream())

	cm, err := NewConfigManager(cfg, client.Client, nil)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = cm.PushToKV(ctx)
	require.NoError(t, err)
	// A bucket that already holds configuration must also hold its identity
	// record, or Start refuses it as predating identity minting (ADR-104).
	seedDeclaredIdentity(t, ctx, cm)

	err = cm.Start(ctx)
	require.NoError(t, err)
	defer cm.Stop(5 * time.Second)

	// Subscribe to model_registry updates
	updates := cm.OnChange("model_registry")

	// Drain initial config from OnChange
	select {
	case <-updates:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout waiting for initial config from OnChange")
	}

	// Update model registry via KV (simulating external mutation)
	updatedRegistry := &model.Registry{
		Endpoints: map[string]*model.EndpointConfig{
			"claude-4": {
				Provider:  "anthropic",
				Model:     "claude-sonnet-4-20250514",
				MaxTokens: 200000,
			},
		},
		Capabilities: map[string]*model.CapabilityConfig{
			"agent-work": {
				Preferred:     []string{"claude-4"},
				RequiresTools: false,
			},
		},
		Defaults: model.DefaultsConfig{
			Model:      "claude-4",
			Capability: "agent-work",
		},
	}
	data, err := json.Marshal(updatedRegistry)
	require.NoError(t, err)
	_, err = cm.kv.Put(ctx, "model_registry", data)
	require.NoError(t, err)

	// Should receive update via watcher
	select {
	case update := <-updates:
		assert.Equal(t, "model_registry", update.Path)
		currentCfg := update.Config.Get()
		require.NotNil(t, currentCfg.ModelRegistry)
		assert.Contains(t, currentCfg.ModelRegistry.Endpoints, "claude-4")
		assert.Equal(t, "anthropic", currentCfg.ModelRegistry.Endpoints["claude-4"].Provider)
		assert.Equal(t, "claude-4", currentCfg.ModelRegistry.Defaults.Model)
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for model_registry update")
	}
}

// TestConfigManager_WatchModelRegistry verifies the typed convenience
// channel for external library consumers: WatchModelRegistry emits
// the latest *model.Registry on every model_registry KV update.
// Pairs with model.Watch — together they provide the documented
// "external consumer keeps own registry fresh" pattern.
func TestConfigManager_WatchModelRegistry(t *testing.T) {
	if testing.Short() {
		t.Skip("integration")
	}

	cfg := &Config{
		Version: "1.0.0",
		Platform: PlatformConfig{
			Org:  "c360",
			ID:   "test-platform",
			Type: "test",
		},
		Services:   make(types.ServiceConfigs),
		Components: make(ComponentConfigs),
		ModelRegistry: &model.Registry{
			Endpoints: map[string]*model.EndpointConfig{
				"initial": {
					Provider: "ollama", URL: "http://x/v1",
					Model: "initial-model", MaxTokens: 1024,
				},
			},
			Defaults: model.DefaultsConfig{Model: "initial"},
		},
	}

	client := natsclient.NewTestClient(t, natsclient.WithJetStream())

	cm, err := NewConfigManager(cfg, client.Client, nil)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	require.NoError(t, cm.PushToKV(ctx))
	// A bucket that already holds configuration must also hold its identity
	// record, or Start refuses it as predating identity minting (ADR-104).
	seedDeclaredIdentity(t, ctx, cm)
	require.NoError(t, cm.Start(ctx))
	defer cm.Stop(5 * time.Second)

	// Subscribe BEFORE the KV write so we don't race the watcher setup.
	regCh := cm.WatchModelRegistry()

	// Drain the initial config emit (mirrors OnChange semantics).
	select {
	case <-regCh:
	case <-time.After(200 * time.Millisecond):
		// No initial emit is acceptable too; the channel may simply
		// not have warmed up yet. We care about the KV-driven update.
	}

	// Update KV with a new registry shape.
	updated := &model.Registry{
		Endpoints: map[string]*model.EndpointConfig{
			"swapped": {
				Provider: "anthropic",
				Model:    "claude-test",
			},
		},
		Defaults: model.DefaultsConfig{Model: "swapped"},
	}
	data, err := json.Marshal(updated)
	require.NoError(t, err)
	_, err = cm.kv.Put(ctx, "model_registry", data)
	require.NoError(t, err)

	// WatchModelRegistry must emit the new registry.
	select {
	case got := <-regCh:
		require.NotNil(t, got)
		assert.Equal(t, "swapped", got.GetDefault())
		assert.NotNil(t, got.GetEndpoint("swapped"))
		assert.Nil(t, got.GetEndpoint("initial"))
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for WatchModelRegistry update")
	}
}

// TestConfigManager_WatchModelRegistry_ImplementsModelWatcher proves
// the public surface satisfies model.Watcher at compile time, so
// model.Watch can consume cm directly. Catches accidental signature
// drift between the two packages.
func TestConfigManager_WatchModelRegistry_ImplementsModelWatcher(t *testing.T) {
	cfg := &Config{
		Version:    "1.0.0",
		Platform:   PlatformConfig{Org: "c360", ID: "test", Type: "test"},
		Services:   make(types.ServiceConfigs),
		Components: make(ComponentConfigs),
	}
	client := natsclient.NewTestClient(t, natsclient.WithJetStream())
	cm, err := NewConfigManager(cfg, client.Client, nil)
	require.NoError(t, err)

	var w model.Watcher = cm
	_ = w
}

func TestConfigManager_MultipleSubscribers(t *testing.T) {
	cfg := &Config{
		Version:    "1.0.0",
		Services:   make(types.ServiceConfigs),
		Components: make(ComponentConfigs),
	}

	client := natsclient.NewTestClient(t, natsclient.WithJetStream())
	// TestClient uses t.Cleanup() automatically

	cm, err := NewConfigManager(cfg, client.Client, nil)
	require.NoError(t, err)

	// Create multiple subscribers for the same pattern
	sub1 := cm.OnChange("services.*")
	sub2 := cm.OnChange("services.*")
	sub3 := cm.OnChange("services.metrics") // Exact match

	// All should receive initial config
	for i, sub := range []<-chan Update{sub1, sub2, sub3} {
		select {
		case update := <-sub:
			assert.NotNil(t, update.Config, "subscriber %d", i+1)
		case <-time.After(100 * time.Millisecond):
			t.Fatalf("timeout waiting for initial config on subscriber %d", i+1)
		}
	}
}
