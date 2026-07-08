//go:build integration

package config

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/types"
)

type ManagerIntegrationSuite struct {
	suite.Suite
	testClient    *natsclient.TestClient
	natsClient    *natsclient.Client
	configManager *Manager
	kvStore       *natsclient.KVStore
	ctx           context.Context
	cancel        context.CancelFunc
}

func (s *ManagerIntegrationSuite) SetupSuite() {
	s.testClient = natsclient.NewTestClient(s.T(),
		natsclient.WithJetStream(),
		natsclient.WithKV())
	s.natsClient = s.testClient.Client
}

func (s *ManagerIntegrationSuite) SetupTest() {
	// Create base config with required fields
	baseConfig := &Config{
		Version: "1.0.0",
		Platform: PlatformConfig{
			Org:  "c360",
			ID:   "integration-test",
			Type: "test",
		},
		Services:   make(types.ServiceConfigs),
		Components: make(ComponentConfigs),
	}

	// Create Manager
	var err error
	s.configManager, err = NewConfigManager(baseConfig, s.natsClient, nil)
	s.Require().NoError(err)

	// Create context for test
	s.ctx, s.cancel = context.WithCancel(context.Background())

	// Start watching
	err = s.configManager.Start(s.ctx)
	s.Require().NoError(err)

	// Get KVStore for direct KV operations
	s.kvStore = s.configManager.kvStore // Use the same KVStore instance

	// Give watcher time to initialize
	time.Sleep(50 * time.Millisecond)
}

func (s *ManagerIntegrationSuite) TearDownTest() {
	_ = s.configManager.Stop(5 * time.Second)
	s.cancel()
}

func (s *ManagerIntegrationSuite) TestJSONOnlyUpdates() {
	// Subscribe to service updates
	updates := s.configManager.OnChange("services.*")

	// With UpdatesOnly, we should get initial config from OnChange
	// but no replay from watcher
	select {
	case <-updates:
		// Expected - OnChange sends initial config
	case <-time.After(100 * time.Millisecond):
		s.Fail("No initial config received from OnChange")
	}

	// 1. Write JSON service config - should work
	metricsConfig := types.ServiceConfig{
		Name:    "metrics",
		Enabled: true,
		Config:  json.RawMessage(`{"port": 9090, "path": "/metrics"}`),
	}
	configJSON, _ := json.Marshal(metricsConfig)
	_, err := s.kvStore.Put(s.ctx, "services.metrics", configJSON)
	s.Require().NoError(err)

	// 2. Wait for update via channel
	select {
	case update := <-updates:
		s.Equal("services.metrics", update.Path) // Should be exact key, not pattern
		cfg := update.Config.Get()
		s.NotNil(cfg.Services["metrics"])

		// Verify the config was properly stored
		svcConfig := cfg.Services["metrics"]
		s.T().Logf("Service config: %+v", svcConfig)
		s.T().Logf("Raw config: %s", string(svcConfig.Config))
		s.Equal("metrics", svcConfig.Name)
		s.True(svcConfig.Enabled)
	case <-time.After(500 * time.Millisecond):
		s.Fail("No config update received")
	}

	// 3. Try property-level update - should be ignored
	s.T().Log("Writing property-level key services.metrics.enabled")
	_, err = s.kvStore.Put(s.ctx, "services.metrics.enabled", []byte("false"))
	s.Require().NoError(err)

	// 4. Verify no update received (property-level ignored)
	select {
	case update := <-updates:
		s.T().Logf("Unexpected update received for key: %s", update.Path)
		cfg := update.Config.Get()
		if svc, ok := cfg.Services["metrics"]; ok {
			s.T().Logf("Service state after property write: %+v", svc)
		}
		s.Fail("Should not receive update for property-level key")
	case <-time.After(200 * time.Millisecond):
		// Expected - no update
		s.T().Log("Good: No update received for property-level key")
	}

	// 5. Update with full JSON again - should work
	metricsConfig.Enabled = false
	configJSON, _ = json.Marshal(metricsConfig)
	_, err = s.kvStore.Put(s.ctx, "services.metrics", configJSON)
	s.Require().NoError(err)

	// 6. Should receive update for JSON change
	select {
	case update := <-updates:
		cfg := update.Config.Get()
		s.NotNil(cfg.Services["metrics"])
	case <-time.After(500 * time.Millisecond):
		s.Fail("Should receive update for JSON config change")
	}
}

func (s *ManagerIntegrationSuite) TestChannelSubscriptions() {
	// Subscribe to different patterns
	serviceUpdates := s.configManager.OnChange("services.*")
	componentUpdates := s.configManager.OnChange("components.*")
	specificService := s.configManager.OnChange("services.discovery")

	// OnChange sends initial config, drain those (expecting up to 3)
	timeout := time.After(300 * time.Millisecond)
	drained := 0
	for drained < 3 {
		select {
		case <-serviceUpdates:
			drained++
		case <-componentUpdates:
			drained++
		case <-specificService:
			drained++
		case <-timeout:
			// No more initial configs to drain
			drained = 3
		}
	}

	// Update a service
	config := types.ServiceConfig{
		Name:    "discovery",
		Enabled: true,
		Config:  json.RawMessage(`{"interval": 30}`),
	}
	configJSON, _ := json.Marshal(config)
	_, err := s.kvStore.Put(s.ctx, "services.discovery", configJSON)
	s.Require().NoError(err)

	// Service channels should receive update
	received := 0
	timeout2 := time.After(500 * time.Millisecond)

	for received < 2 {
		select {
		case <-serviceUpdates:
			received++
		case <-specificService:
			received++
		case <-componentUpdates:
			s.Fail("Component channel should not receive service update")
		case <-timeout2:
			s.Fail("Timeout waiting for service updates")
			return
		}
	}

	s.Equal(2, received, "Should receive updates on both service channels")

	// Component channel should NOT have received update
	select {
	case <-componentUpdates:
		s.Fail("Component channel should not receive service update")
	case <-time.After(50 * time.Millisecond):
		// Expected - no update on component channel
	}
}

func (s *ManagerIntegrationSuite) TestConcurrentKVUpdates() {
	// Test that Manager handles concurrent KV updates gracefully
	updates := s.configManager.OnChange("services.*")

	// Write multiple services concurrently
	services := []string{"metrics", "discovery", "message-logger"}
	done := make(chan bool, len(services))

	for _, svcName := range services {
		go func(name string) {
			config := types.ServiceConfig{
				Name:    name,
				Enabled: true,
				Config:  json.RawMessage(`{"test": true}`),
			}
			configJSON, _ := json.Marshal(config)
			_, err := s.kvStore.Put(s.ctx, "services."+name, configJSON)
			s.NoError(err)
			done <- true
		}(svcName)
	}

	// Wait for all writes to complete
	for i := 0; i < len(services); i++ {
		<-done
	}

	// Should receive updates for all services (order may vary)
	receivedServices := make(map[string]bool)
	timeout := time.After(1 * time.Second)

	for len(receivedServices) < len(services) {
		select {
		case update := <-updates:
			cfg := update.Config.Get()
			for svcName := range cfg.Services {
				receivedServices[svcName] = true
			}
		case <-timeout:
			s.Failf("Timeout waiting for all service updates", "Received: %v", receivedServices)
			return
		}
	}

	// Verify all services were received
	for _, svcName := range services {
		s.True(receivedServices[svcName], "Should have received update for "+svcName)
	}
}

func (s *ManagerIntegrationSuite) TestCompleteFlow_KVToService() {
	// Test complete flow: KV → Manager → Config update → Service visibility

	// 1. Subscribe to updates
	updates := s.configManager.OnChange("services.metrics")

	// OnChange sends initial config, drain it
	select {
	case <-updates:
		// Expected - OnChange sends initial config
	case <-time.After(100 * time.Millisecond):
		// May not receive if no existing config
	}

	// 2. Write service config to KV
	metricsConfig := types.ServiceConfig{
		Name:    "metrics",
		Enabled: true,
		Config:  json.RawMessage(`{"port": 9090, "path": "/metrics"}`),
	}
	configJSON, _ := json.Marshal(metricsConfig)
	_, err := s.kvStore.Put(s.ctx, "services.metrics", configJSON)
	s.Require().NoError(err)

	// 3. Verify update received via channel
	select {
	case <-updates:
		// 4. Verify config is accessible via GetConfig()
		currentConfig := s.configManager.GetConfig()
		cfg := currentConfig.Get()

		s.NotNil(cfg.Services["metrics"])
		s.Equal("metrics", cfg.Services["metrics"].Name)
		s.True(cfg.Services["metrics"].Enabled)

		// 5. Verify the raw config is preserved correctly
		var parsedConfig map[string]any
		err := json.Unmarshal(cfg.Services["metrics"].Config, &parsedConfig)
		s.NoError(err)
		s.Equal(float64(9090), parsedConfig["port"])
		s.Equal("/metrics", parsedConfig["path"])

	case <-time.After(500 * time.Millisecond):
		s.Fail("No config update received")
	}

	// 6. Test config deletion
	err = s.kvStore.Delete(s.ctx, "services.metrics")
	s.NoError(err)

	// 7. Should receive update for deletion
	select {
	case <-updates:
		// After deletion, service should be removed from config
		currentConfig := s.configManager.GetConfig()
		cfg := currentConfig.Get()
		_, exists := cfg.Services["metrics"]
		s.False(exists, "Service should be removed after deletion")
	case <-time.After(500 * time.Millisecond):
		s.Fail("No update received for deletion")
	}
}

func (s *ManagerIntegrationSuite) TestKVStore_OptimisticLocking() {
	// Test that KVStore's CAS operations prevent lost updates

	// Create initial config
	config := types.ServiceConfig{
		Name:    "test-service",
		Enabled: true,
		Config:  json.RawMessage(`{"version": 1}`),
	}
	configJSON, _ := json.Marshal(config)
	rev1, err := s.kvStore.Put(s.ctx, "services.test", configJSON)
	s.Require().NoError(err)
	s.Greater(rev1, uint64(0))

	// Get current state
	entry, err := s.kvStore.Get(s.ctx, "services.test")
	s.Require().NoError(err)
	s.Equal(rev1, entry.Revision)

	// Simulate concurrent update (someone else changes it)
	config.Config = json.RawMessage(`{"version": 2}`)
	configJSON, _ = json.Marshal(config)
	rev2, err := s.kvStore.Put(s.ctx, "services.test", configJSON)
	s.Require().NoError(err)
	s.Greater(rev2, rev1)

	// Try to update with old revision (should fail)
	config.Config = json.RawMessage(`{"version": 3}`)
	configJSON, _ = json.Marshal(config)
	_, err = s.kvStore.Update(s.ctx, "services.test", configJSON, rev1)
	s.Error(err)
	s.True(natsclient.IsKVConflictError(err), "Should be a revision mismatch error")

	// Update with correct revision (should succeed)
	_, err = s.kvStore.Update(s.ctx, "services.test", configJSON, rev2)
	s.NoError(err)
}

// TestRuntimeComponentAdd_AppliesAndReconciles (gh#388) proves PutComponentToKV
// applies the component to the in-memory config synchronously AND drives a
// components.* notification, so a runtime add reconciles without PushToKV.
func (s *ManagerIntegrationSuite) TestRuntimeComponentAdd_AppliesAndReconciles() {
	updates := s.configManager.OnChange("components.*")
	select {
	case <-updates: // drain the initial-config send
	case <-time.After(500 * time.Millisecond):
		s.Fail("no initial config from OnChange")
	}

	comp := types.ComponentConfig{Type: "input", Name: "doc-source-003", Enabled: true}
	s.Require().NoError(s.configManager.PutComponentToKV(s.ctx, "doc-source-003", comp))

	// In-memory config reflects the add synchronously (right after the call).
	_, present := s.configManager.config.Get().Components["doc-source-003"]
	s.True(present, "PutComponentToKV must apply the component in memory synchronously")

	// A components.* notification is delivered carrying the added component.
	select {
	case up := <-updates:
		s.Equal("components.doc-source-003", up.Path)
		_, ok := up.Config.Get().Components["doc-source-003"]
		s.True(ok, "notified config must carry the added component")
	case <-time.After(2 * time.Second):
		s.Fail("PutComponentToKV did not deliver a reconcile notification (gh#388)")
	}
}

// TestRuntimeComponentRemove_AppliesAndReconciles (gh#388) proves
// DeleteComponentFromKV removes the component from the in-memory config
// synchronously AND drives a components.* notification, so a runtime remove
// reconciles (teardown) without PushToKV.
func (s *ManagerIntegrationSuite) TestRuntimeComponentRemove_AppliesAndReconciles() {
	// Seed a component.
	comp := types.ComponentConfig{Type: "input", Name: "doc-source-009", Enabled: true}
	s.Require().NoError(s.configManager.PutComponentToKV(s.ctx, "doc-source-009", comp))
	_, present := s.configManager.config.Get().Components["doc-source-009"]
	s.Require().True(present, "precondition: component seeded in memory")

	updates := s.configManager.OnChange("components.*")
	select {
	case <-updates: // drain initial
	case <-time.After(500 * time.Millisecond):
		s.Fail("no initial config from OnChange")
	}

	s.Require().NoError(s.configManager.DeleteComponentFromKV(s.ctx, "doc-source-009"))

	// In-memory config reflects the removal synchronously.
	_, stillPresent := s.configManager.config.Get().Components["doc-source-009"]
	s.False(stillPresent, "DeleteComponentFromKV must remove the component in memory synchronously")

	// A components.* notification is delivered without the removed component.
	select {
	case up := <-updates:
		_, ok := up.Config.Get().Components["doc-source-009"]
		s.False(ok, "notified config must not carry the removed component")
	case <-time.After(2 * time.Second):
		s.Fail("DeleteComponentFromKV did not deliver a reconcile notification (gh#388)")
	}
}

func TestManagerIntegrationSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration tests in short mode")
	}
	suite.Run(t, new(ManagerIntegrationSuite))
}

// TestConfigManager_RefusesForeignPlatformIdentity is a regression test for
// gh#459: two sem* apps sharing one NATS server also share the fixed-name
// semstreams_config bucket. With matching config versions, the second app to
// boot silently adopted the first's components (and could panic creating a
// foreign one). The manager must refuse to adopt config whose platform
// identity (org+id) differs from the local file's, and run on local config.
func TestConfigManager_RefusesForeignPlatformIdentity(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithJetStream(), natsclient.WithKV())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Manager A — the "foreign" app boots first and seeds the shared bucket.
	foreignCfg := &Config{
		Version:  "1.0.0",
		Platform: PlatformConfig{Org: "foreignorg", ID: "foreign-app", Type: "test"},
		Services: make(types.ServiceConfigs),
		Components: ComponentConfigs{
			"foreign-comp": types.ComponentConfig{
				Type: "input", Name: "udp", Enabled: true,
				Config: json.RawMessage(`{"port": 8080}`),
			},
		},
	}
	mgrA, err := NewConfigManager(foreignCfg, tc.Client, nil)
	require.NoError(t, err)
	require.NoError(t, mgrA.Start(ctx)) // first boot → pushes foreign config to KV
	require.NoError(t, mgrA.Stop(5*time.Second))

	// Manager B — the "local" app boots second against the same bucket with a
	// DIFFERENT platform identity but the SAME version. It must NOT adopt the
	// foreign config.
	localCfg := &Config{
		Version:  "1.0.0", // matching version is not matching identity
		Platform: PlatformConfig{Org: "localorg", ID: "local-app", Type: "test"},
		Services: make(types.ServiceConfigs),
		Components: ComponentConfigs{
			"local-comp": types.ComponentConfig{
				Type: "output", Name: "websocket", Enabled: true,
				Config: json.RawMessage(`{"port": 9099}`),
			},
		},
	}
	mgrB, err := NewConfigManager(localCfg, tc.Client, nil)
	require.NoError(t, err)
	require.NoError(t, mgrB.Start(ctx))
	defer mgrB.Stop(5 * time.Second)

	got := mgrB.GetConfig().Get()

	// Kept its own platform identity — did not adopt the foreign one.
	require.Equal(t, "localorg", got.Platform.Org, "manager B must keep its own org")
	require.Equal(t, "local-app", got.Platform.ID, "manager B must keep its own platform id")

	// Kept its own component; did not adopt the foreign app's (the gh#459 bleed).
	_, hasForeign := got.Components["foreign-comp"]
	require.False(t, hasForeign, "manager B must not adopt the foreign app's component")
	_, hasLocal := got.Components["local-comp"]
	require.True(t, hasLocal, "manager B must keep its own component")

	// Reverse-bleed guard: a detached manager must not write the local app's
	// config INTO the foreign bucket either. PushToKV must no-op, leaving the
	// stored platform identity as the foreign one.
	require.NoError(t, mgrB.PushToKV(ctx))
	kvID, found := mgrB.kvPlatformIdentity(ctx)
	require.True(t, found)
	require.Equal(t, "foreign-app", kvID.ID,
		"detached manager must not overwrite the foreign bucket's platform identity")
}
