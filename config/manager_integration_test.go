//go:build integration

package config

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
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
	// Create the test lifetime before constructing any context-taking owner.
	s.ctx, s.cancel = context.WithCancel(context.Background())

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
	s.configManager, err = NewConfigManager(s.ctx, baseConfig, s.natsClient, nil)
	s.Require().NoError(err)

	// Start watching
	err = s.configManager.Start(s.ctx)
	s.Require().NoError(err)

	// Get KVStore for direct KV operations
	s.kvStore = s.configManager.kvStore // Use the same KVStore instance

}

func (s *ManagerIntegrationSuite) TearDownTest() {
	if s.configManager != nil {
		_ = s.configManager.Stop(5 * time.Second)
	}
	if s.cancel != nil {
		s.cancel()
	}
}

func (s *ManagerIntegrationSuite) TestJSONOnlyUpdates() {
	metricsConfig := types.ServiceConfig{Enabled: true, Config: json.RawMessage(`{"port": 9090, "path": "/metrics"}`)}
	configJSON, _ := json.Marshal(metricsConfig)
	_, err := s.kvStore.Put(s.ctx, "services.metrics", configJSON)
	s.Require().NoError(err)
	s.Require().Eventually(func() bool {
		return s.configManager.GetConfig().Get().Services["metrics"].Enabled
	}, time.Second, 10*time.Millisecond)

	metricsConfig.Enabled = false
	configJSON, _ = json.Marshal(metricsConfig)
	_, err = s.kvStore.Put(s.ctx, "services.metrics", configJSON)
	s.Require().NoError(err)
	s.Require().Eventually(func() bool {
		return !s.configManager.GetConfig().Get().Services["metrics"].Enabled
	}, time.Second, 10*time.Millisecond)
}

func (s *ManagerIntegrationSuite) TestConcurrentKVUpdates() {
	// Test that Manager handles concurrent KV updates gracefully
	// Write multiple services concurrently
	services := []string{"metrics", "discovery", "message-logger"}
	done := make(chan bool, len(services))

	for _, svcName := range services {
		go func(name string) {
			config := types.ServiceConfig{Enabled: true, Config: json.RawMessage(`{"test": true}`)}

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

	s.Require().Eventually(func() bool {
		cfg := s.configManager.GetConfig().Get()
		for _, serviceName := range services {
			if _, ok := cfg.Services[serviceName]; !ok {
				return false
			}
		}
		return true
	}, time.Second, 10*time.Millisecond)
}

func (s *ManagerIntegrationSuite) TestCompleteFlow_KVToService() {
	// Test complete flow: KV → Manager → Config update → Service visibility

	metricsConfig := types.ServiceConfig{Enabled: true, Config: json.RawMessage(`{"port": 9090, "path": "/metrics"}`)}

	configJSON, _ := json.Marshal(metricsConfig)
	_, err := s.kvStore.Put(s.ctx, "services.metrics", configJSON)
	s.Require().NoError(err)

	s.Require().Eventually(func() bool {
		currentConfig := s.configManager.GetConfig()
		cfg := currentConfig.Get()
		return cfg.Services["metrics"].Enabled
	}, time.Second, 10*time.Millisecond)
	cfg := s.configManager.GetConfig().Get()
	{
		var parsedConfig map[string]any
		err := json.Unmarshal(cfg.Services["metrics"].Config, &parsedConfig)
		s.NoError(err)
		s.Equal(float64(9090), parsedConfig["port"])
		s.Equal("/metrics", parsedConfig["path"])
	}
	err = s.kvStore.Delete(s.ctx, "services.metrics")
	s.NoError(err)
	s.Require().Eventually(func() bool {
		cfg := s.configManager.GetConfig().Get()
		_, exists := cfg.Services["metrics"]
		return !exists
	}, time.Second, 10*time.Millisecond)
}

func (s *ManagerIntegrationSuite) TestKVStore_OptimisticLocking() {
	// Test that KVStore's CAS operations prevent lost updates

	// Create initial config
	config := types.ServiceConfig{Enabled: true, Config: json.RawMessage(`{"version": 1}`)}

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

// TestComponentAddPersistsDesiredConfig (gh#388) proves PutComponentToKV
// persists desired next-boot configuration and updates the author's local view
// synchronously. Runtime composition remains sealed until restart.
func (s *ManagerIntegrationSuite) TestComponentAddPersistsDesiredConfig() {
	comp := types.ComponentConfig{Type: "input", Name: "doc-source-003", Enabled: true}
	s.Require().NoError(s.configManager.PutComponentToKV(s.ctx, "doc-source-003", comp))

	// In-memory config reflects the add synchronously (right after the call).
	_, present := s.configManager.config.Get().Components["doc-source-003"]
	s.True(present, "PutComponentToKV must apply the component in memory synchronously")

}

// TestSixRapidComponentPutsConverge pins the explicit flow-publish write path:
// each acknowledged Put is durable, immediately visible through SafeConfig,
// and its watcher echo eventually clears the per-key pending classifier.
func (s *ManagerIntegrationSuite) TestSixRapidComponentPutsConverge() {
	s.configManager.pendingMu.Lock()
	startupPending := len(s.configManager.pendingLocal)
	s.configManager.pendingMu.Unlock()
	s.Zero(startupPending, "startup writes precede UpdatesOnly watcher ownership and can never have echoes")

	want := make(ComponentConfigs, 6)
	for i := 0; i < 6; i++ {
		name := fmt.Sprintf("rapid-%d", i)
		componentConfig := types.ComponentConfig{
			Type: types.ComponentTypeProcessor, Name: "rapid-factory", Enabled: true,
			Config: json.RawMessage(fmt.Sprintf(`{"index":%d}`, i)),
		}
		want[name] = componentConfig
		s.Require().NoError(s.configManager.PutComponentToKV(s.ctx, name, componentConfig))
	}

	current := s.configManager.GetConfig().Get()
	for name, expected := range want {
		actual, ok := current.Components[name]
		s.True(ok, "SafeConfig missing %s", name)
		s.True(actual.Equal(expected), "SafeConfig[%s] = %#v", name, actual)
		entry, err := s.kvStore.Get(s.ctx, "components."+name)
		s.Require().NoError(err)
		var persisted types.ComponentConfig
		s.Require().NoError(json.Unmarshal(entry.Value, &persisted))
		s.True(persisted.Equal(expected), "KV[%s] = %#v", name, persisted)
	}
	s.Require().Eventually(func() bool {
		s.configManager.pendingMu.Lock()
		defer s.configManager.pendingMu.Unlock()
		return len(s.configManager.pendingLocal) == 0
	}, time.Second, 10*time.Millisecond)
}

// TestComponentRemovePersistsDesiredConfig (gh#388) proves
// DeleteComponentFromKV removes desired next-boot configuration and updates the
// author's local view synchronously. Runtime composition remains sealed.
func (s *ManagerIntegrationSuite) TestComponentRemovePersistsDesiredConfig() {
	// Seed a component.
	comp := types.ComponentConfig{Type: "input", Name: "doc-source-009", Enabled: true}
	s.Require().NoError(s.configManager.PutComponentToKV(s.ctx, "doc-source-009", comp))
	_, present := s.configManager.config.Get().Components["doc-source-009"]
	s.Require().True(present, "precondition: component seeded in memory")

	s.Require().NoError(s.configManager.DeleteComponentFromKV(s.ctx, "doc-source-009"))

	// In-memory config reflects the removal synchronously.
	_, stillPresent := s.configManager.config.Get().Components["doc-source-009"]
	s.False(stillPresent, "DeleteComponentFromKV must remove the component in memory synchronously")
}

// TestDesiredComponentAddRemoveConcurrentNoLostUpdate is the gh#515 regression at
// the Manager level: many concurrent PutComponentToKV (adds) interleaved with
// DeleteComponentFromKV (removes) must not drop one another's in-memory change.
// Both paths funnel through updateConfig → SafeConfig.Mutate, which serializes the
// read-modify-write. The old lock-free Get→mutate→Update would lose writes here
// (and -race would not flag it — the atomicity violation is compound-level), so
// this asserts on the surviving set, not via -race.
func (s *ManagerIntegrationSuite) TestDesiredComponentAddRemoveConcurrentNoLostUpdate() {
	const n = 40

	// Seed n "remove-target" components that concurrent deletes will remove.
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("del-%03d", i)
		s.Require().NoError(s.configManager.PutComponentToKV(s.ctx,
			name, types.ComponentConfig{Type: "input", Name: name, Enabled: true}))
	}

	var wg sync.WaitGroup
	wg.Add(2 * n)
	for i := 0; i < n; i++ {
		addName := fmt.Sprintf("add-%03d", i)
		delName := fmt.Sprintf("del-%03d", i)
		go func() {
			defer wg.Done()
			_ = s.configManager.PutComponentToKV(s.ctx,
				addName, types.ComponentConfig{Type: "input", Name: addName, Enabled: true})
		}()
		go func() {
			defer wg.Done()
			_ = s.configManager.DeleteComponentFromKV(s.ctx, delName)
		}()
	}
	wg.Wait()

	comps := s.configManager.config.Get().Components
	for i := 0; i < n; i++ {
		addName := fmt.Sprintf("add-%03d", i)
		delName := fmt.Sprintf("del-%03d", i)
		_, added := comps[addName]
		s.True(added, "concurrent add %s must survive (no lost update)", addName)
		_, removed := comps[delName]
		s.False(removed, "concurrent remove %s must take effect (no lost update)", delName)
	}
}

func TestManagerIntegrationSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration tests in short mode")
	}
	suite.Run(t, new(ManagerIntegrationSuite))
}

func TestConfigManagerStartupVersionArbitration(t *testing.T) {
	tests := []struct {
		name        string
		fileVersion string
		kvVersion   string
		wantSource  string
	}{
		{name: "newer file pushes file state", fileVersion: "2.0.0", kvVersion: "1.0.0", wantSource: "file"},
		{name: "older file selects KV", fileVersion: "1.0.0", kvVersion: "2.0.0", wantSource: "kv"},
		{name: "equal version content edit does not apply", fileVersion: "1.0.0", kvVersion: "1.0.0", wantSource: "kv"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc := natsclient.NewTestClient(t, natsclient.WithJetStream(), natsclient.WithKV())
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			fileConfig := &Config{
				Version: tt.fileVersion,
				Platform: PlatformConfig{
					Org: "c360", ID: "arbitration-test", Type: "test",
				},
				Services: types.ServiceConfigs{
					"common": {Enabled: true, Config: json.RawMessage(`{"source":"file"}`)},
				},
				Components: make(ComponentConfigs),
			}
			manager, err := NewConfigManager(context.Background(), fileConfig, tc.Client, nil)
			require.NoError(t, err)

			putConfigValue(t, ctx, manager, "version", tt.kvVersion)
			putConfigValue(t, ctx, manager, "services.common", types.ServiceConfig{
				Enabled: true,
				Config:  json.RawMessage(`{"source":"kv"}`),
			})

			require.NoError(t, manager.Start(ctx))
			defer manager.Stop(5 * time.Second)

			got := manager.GetConfig().Get().Services["common"]
			var inner map[string]string
			require.NoError(t, json.Unmarshal(got.Config, &inner))
			require.Equal(t, tt.wantSource, inner["source"])

			entry, err := manager.kv.Get(ctx, "services.common")
			require.NoError(t, err)
			var stored types.ServiceConfig
			require.NoError(t, json.Unmarshal(entry.Value(), &stored))
			require.NoError(t, json.Unmarshal(stored.Config, &inner))
			require.Equal(t, tt.wantSource, inner["source"])
		})
	}
}

func TestConfigManagerKVSelectionReplacesOnlyServices(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithJetStream(), natsclient.WithKV())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fileConfig := &Config{
		Version: "1.0.0",
		Platform: PlatformConfig{
			Org: "c360", ID: "services-replacement-test", Type: "test",
		},
		Services: types.ServiceConfigs{
			"file-only": {Enabled: true, Config: json.RawMessage(`{}`)},
		},
		Components: ComponentConfigs{
			"file-component": {Type: "input", Name: "file-component", Enabled: true},
		},
	}
	manager, err := NewConfigManager(context.Background(), fileConfig, tc.Client, nil)
	require.NoError(t, err)

	putConfigValue(t, ctx, manager, "version", "1.0.0")
	putConfigValue(t, ctx, manager, "services.kv-only", types.ServiceConfig{
		Enabled: true,
		Config:  json.RawMessage(`{}`),
	})
	putConfigValue(t, ctx, manager, "components.kv-component", types.ComponentConfig{
		Type: "output", Name: "kv-component", Enabled: true,
	})

	require.NoError(t, manager.Start(ctx))
	defer manager.Stop(5 * time.Second)

	got := manager.GetConfig().Get()
	require.NotContains(t, got.Services, "file-only")
	require.Contains(t, got.Services, "kv-only")
	require.Contains(t, got.Components, "file-component")
	require.Contains(t, got.Components, "kv-component")
}

func putConfigValue(t *testing.T, ctx context.Context, manager *Manager, key string, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	require.NoError(t, err)
	_, err = manager.kvStore.Put(ctx, key, data)
	require.NoError(t, err)
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
	mgrA, err := NewConfigManager(context.Background(), foreignCfg, tc.Client, nil)
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
	mgrB, err := NewConfigManager(context.Background(), localCfg, tc.Client, nil)
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

	// Reverse-bleed guard: every detached write path must refuse with the same
	// fatal classified error and leave the foreign bucket byte-for-byte owned by
	// manager A.
	beforePlatform, err := mgrB.kvStore.Get(ctx, "platform")
	require.NoError(t, err)
	beforeComponents, err := mgrB.kvStore.Get(ctx, "components.foreign-comp")
	require.NoError(t, err)

	writeErrors := []error{
		mgrB.PushToKV(ctx),
		mgrB.PutComponentToKV(ctx, "local-extra", types.ComponentConfig{
			Type: types.ComponentTypeOutput, Name: "websocket", Enabled: true,
		}),
		mgrB.DeleteComponentFromKV(ctx, "foreign-comp"),
	}
	for _, writeErr := range writeErrors {
		require.Error(t, writeErr)
		require.True(t, errs.IsFatal(writeErr), "detached write error class = %T: %v", writeErr, writeErr)
		var classified *errs.ClassifiedError
		require.ErrorAs(t, writeErr, &classified)
		require.Contains(t, classified.Error(), "detached from foreign KV bucket")
	}

	afterPlatform, err := mgrB.kvStore.Get(ctx, "platform")
	require.NoError(t, err)
	afterComponents, err := mgrB.kvStore.Get(ctx, "components.foreign-comp")
	require.NoError(t, err)
	require.Equal(t, beforePlatform.Value, afterPlatform.Value)
	require.Equal(t, beforeComponents.Value, afterComponents.Value)
	_, err = mgrB.kvStore.Get(ctx, "components.local-extra")
	require.ErrorIs(t, err, natsclient.ErrKVKeyNotFound)

	kvID, found := mgrB.kvPlatformIdentity(ctx)
	require.True(t, found)
	require.Equal(t, "foreign-app", kvID.ID,
		"detached manager must not overwrite the foreign bucket's platform identity")
}
