package config

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/c360/semstreams/natsclient"
	"github.com/c360/semstreams/types"
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

func TestManagerIntegrationSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration tests in short mode")
	}
	suite.Run(t, new(ManagerIntegrationSuite))
}
