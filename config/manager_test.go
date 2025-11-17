package config

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/c360/semstreams/natsclient"
	"github.com/c360/semstreams/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigManager_PatternMatching(t *testing.T) {
	// Create a minimal config
	cfg := &Config{
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
		Services: types.ServiceConfigs{
			"metrics": types.ServiceConfig{
				Name:    "metrics",
				Enabled: true,
				Config:  json.RawMessage(`{"port": 9090}`),
			},
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
		Platform: PlatformConfig{
			Org:  "c360",
			ID:   "test-platform",
			Type: "test",
		},
		Services: types.ServiceConfigs{
			"metrics": types.ServiceConfig{
				Name:    "metrics",
				Enabled: true,
				Config:  json.RawMessage(`{"port": 9090}`),
			},
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
	newConfig := json.RawMessage(`{"enabled": false, "port": 9090}`)
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
		Platform: PlatformConfig{
			Org: "test-org",
			ID:  "test-id",
		},
		Services: types.ServiceConfigs{
			"metrics": types.ServiceConfig{
				Name:    "metrics",
				Enabled: true,
				Config:  json.RawMessage(`{}`),
			},
			"discovery": types.ServiceConfig{
				Name:    "discovery",
				Enabled: false,
				Config:  json.RawMessage(`{"port": 8080}`),
			},
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
	assert.Equal(t, "metrics", metricsConfig.Name)
	assert.True(t, metricsConfig.Enabled)

	entry, err = cm.kv.Get(ctx, "services.discovery")
	require.NoError(t, err)
	var discoveryConfig types.ServiceConfig
	err = json.Unmarshal(entry.Value(), &discoveryConfig)
	require.NoError(t, err)
	assert.Equal(t, "discovery", discoveryConfig.Name)
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
}

func TestConfigManager_MultipleSubscribers(t *testing.T) {
	cfg := &Config{
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
