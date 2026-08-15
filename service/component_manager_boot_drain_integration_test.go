//go:build integration

package service

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/config"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/model"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/types"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// bootDrainHarness wires the full production boot stack for the boot-boundary
// drain tests: a real config.Manager (KV-backed, watching), a real
// ComponentManager built through NewComponentManager with watch_config
// enabled, and a service Manager driving Manager.StartAll.
type bootDrainHarness struct {
	client        *natsclient.Client
	configManager *config.Manager
	cm            *ComponentManager
	manager       *Manager
	rec           *bucketSweepRecordingHandler
}

func newBootDrainHarness(
	t *testing.T,
	client *natsclient.Client,
	registry *component.Registry,
	initialCfg *config.Config,
	rec *bucketSweepRecordingHandler,
) *bootDrainHarness {
	t.Helper()
	ctx := context.Background()

	configManager, err := config.NewConfigManager(initialCfg, client, slog.Default())
	require.NoError(t, err)
	require.NoError(t, configManager.PushToKV(ctx))
	require.NoError(t, configManager.Start(ctx))
	t.Cleanup(func() { _ = configManager.Stop(5 * time.Second) })

	deps := &Dependencies{
		NATSClient:        client,
		Manager:           configManager,
		Logger:            slog.Default(),
		ComponentRegistry: registry,
	}
	// watch_config: true wires the OnChange channels — the production shape
	// whose cap-1 buffers the drain must defeat.
	cmService, err := NewComponentManager(json.RawMessage(`{"watch_config": true}`), deps)
	require.NoError(t, err)
	cm := cmService.(*ComponentManager)

	manager := NewServiceManager(NewServiceRegistry())
	manager.BaseService = NewBaseServiceWithOptions("service-manager-registry", nil, WithLogger(slog.New(rec)))
	manager.natsClient = client
	manager.RegisterInstance("component-manager", cm)

	return &bootDrainHarness{
		client:        client,
		configManager: configManager,
		cm:            cm,
		manager:       manager,
		rec:           rec,
	}
}

// awaitConfigVisible blocks until a config notification whose applied state
// satisfies check arrives on ch — explicit synchronization on the KV write
// having been applied to the LOCAL SafeConfig (notification follows state
// application in config.Manager). Callers must have consumed the channel's
// initial OnChange snapshot first.
func awaitConfigVisible(t *testing.T, ch <-chan config.Update, what string, check func(*config.Config) bool) {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case u := <-ch:
			if check(u.Config.Get()) {
				return
			}
		case <-deadline:
			t.Fatalf("%s never became locally visible", what)
		}
	}
}

// putComponentConfigKV writes a component config through the production KV
// path (the semstreams_config bucket key config.Manager's watcher consumes).
func putComponentConfigKV(t *testing.T, client *natsclient.Client, name string, cfg types.ComponentConfig) {
	t.Helper()
	ctx := context.Background()
	kv, err := client.GetKeyValueBucket(ctx, "semstreams_config")
	require.NoError(t, err)
	data, err := json.Marshal(cfg)
	require.NoError(t, err)
	_, err = kv.Put(ctx, "components."+name, data)
	require.NoError(t, err)
}

func bootDrainPlatform() config.PlatformConfig {
	return config.PlatformConfig{
		Org:         "test",
		ID:          "boot-drain",
		InstanceID:  "boot-001",
		Environment: "test",
	}
}

// TestIntegration_StartAll_MidBootComponentJoinsBootTransaction locks the
// boot-boundary drain's core guarantee end-to-end through Manager.StartAll: a
// component ADDED while a cold-boot component's Start is still in flight is
// created and started INSIDE the boot transaction — StartAll does not return
// until the late owner's Start has run. Its Start reproduces the create-race
// (a rival's raw dirty create with a foreign TTL) and closes it at the
// acquisition seam, so by the time StartAll returns the guarded bucket is
// clean with its stored key preserved — the post-cutoff class the retired
// post-start sweep once covered, now held by the seam at each acquisition.
//
// Discriminating for the drain: without it, the update-created adopter is
// handled by the watcher's detached dynamic path AFTER StartAll returns, and
// the started-state assertion at StartAll-return goes red. (The
// deterministically-forced interleaving variants live in the Edit/Registry
// drain tests below, whose cap-1-buffer drop is revert-provable without
// timing.)
func TestIntegration_StartAll_MidBootComponentJoinsBootTransaction(t *testing.T) {
	ctx := context.Background()
	testClient := natsclient.NewTestClient(t, natsclient.WithKV())
	defer testClient.Terminate()
	client := testClient.Client

	rec := &bucketSweepRecordingHandler{}

	gated := newBarrierTestComponent("gate-comp")
	gated.entered = make(chan struct{})
	gated.release = make(chan struct{})

	var (
		adopterMu sync.Mutex
		adopter   *bucketAdopterComponent
	)
	compRegistry := component.NewRegistry()
	require.NoError(t, compRegistry.RegisterFactory("gate-factory", &component.Registration{
		Name: "gate-factory",
		Type: string(types.ComponentTypeProcessor),
		Factory: func(_ json.RawMessage, _ component.Dependencies) (component.Discoverable, error) {
			return gated, nil
		},
	}))
	require.NoError(t, compRegistry.RegisterFactory("bucket-adopter", &component.Registration{
		Name: "bucket-adopter",
		Type: string(types.ComponentTypeProcessor),
		Factory: func(_ json.RawMessage, deps component.Dependencies) (component.Discoverable, error) {
			c := &bucketAdopterComponent{
				client: deps.NATSClient,
				bucket: graph.BucketEmbeddingIndex,
			}
			adopterMu.Lock()
			adopter = c
			adopterMu.Unlock()
			return c, nil
		},
	}))

	h := newBootDrainHarness(t, client, compRegistry, &config.Config{
		Platform: bootDrainPlatform(),
		Components: config.ComponentConfigs{
			"gate-comp": types.ComponentConfig{
				Type:    types.ComponentTypeProcessor,
				Name:    "gate-factory",
				Enabled: true,
				Config:  json.RawMessage(`{}`),
			},
		},
	}, rec)

	// Visibility subscriber: consume its initial snapshot so the mid-boot
	// write's notification is not dropped by the cap-1 buffer.
	vis := h.configManager.OnChange("components.*")
	select {
	case <-vis:
	case <-time.After(10 * time.Second):
		t.Fatal("visibility subscriber never received the initial snapshot")
	}

	// Precondition: the guarded bucket must not exist before boot.
	_, err := client.GetKeyValueBucket(ctx, graph.BucketEmbeddingIndex)
	require.ErrorIs(t, err, jetstream.ErrBucketNotFound,
		"precondition: the guarded bucket must not exist before StartAll")

	done := make(chan error, 1)
	go func() { done <- h.manager.StartAll(ctx) }()

	// Boot is provably mid-barrier.
	<-gated.entered

	// Add the late owner through the production KV write path and wait until
	// the change is applied to the LOCAL SafeConfig — the drain reconciles
	// live local state, so local visibility before gate release makes the
	// boot-transaction claim deterministic.
	putComponentConfigKV(t, client, "late-owner", types.ComponentConfig{
		Type:    types.ComponentTypeProcessor,
		Name:    "bucket-adopter",
		Enabled: true,
		Config:  json.RawMessage(`{}`),
	})
	awaitConfigVisible(t, vis, "late-owner component add", func(c *config.Config) bool {
		_, ok := c.Components["late-owner"]
		return ok
	})

	close(gated.release)
	select {
	case err := <-done:
		require.NoError(t, err, "boot must succeed with the mid-boot component joined")
	case <-time.After(30 * time.Second):
		t.Fatal("Manager.StartAll did not return")
	}
	defer func() { _ = h.manager.StopAll(context.Background()) }()

	// The mid-boot component started inside the boot transaction...
	status := h.cm.GetComponentStatus()
	require.Contains(t, status, "late-owner")
	require.Equal(t, component.StateStarted, status["late-owner"].State,
		"the mid-boot-added component must be started by the time StartAll returns")
	adopterMu.Lock()
	started := adopter
	adopterMu.Unlock()
	require.NotNil(t, started, "the drain must have built the late owner through the factory path")
	require.Equal(t, 7*24*time.Hour, started.dirtyTTLBeforeSeam(),
		"the bucket must have carried the foreign TTL at its mid-boot creation, before the seam ran")

	// ...and its seam acquisition reconciled the dirty bucket inside that
	// Start: TTL stripped, key preserved, before StartAll returned — no boot
	// sweep involved (StartAll no longer has one).
	fresh, err := client.GetKeyValueBucket(ctx, graph.BucketEmbeddingIndex)
	require.NoError(t, err)
	maxAge, maxBytes, err := natsclient.BucketRetention(ctx, fresh)
	require.NoError(t, err)
	assert.Equal(t, time.Duration(0), maxAge,
		"the mid-boot owner's seam acquisition must strip its dirty TTL inside the boot transaction")
	assert.LessOrEqual(t, maxBytes, int64(0), "the seam must leave MaxBytes non-binding")
	entry, err := fresh.Get(ctx, "entity.key.one")
	require.NoError(t, err, "the stored key must survive the strip")
	assert.Equal(t, []byte("survivor"), entry.Value())
}

// valuedComponent records the config value its instance was built from and
// started with, so a test can assert WHICH configuration generation is live.
type valuedComponent struct {
	name    string
	value   string
	onStart func(value string)
}

func (c *valuedComponent) Meta() component.Metadata {
	return component.Metadata{Name: c.name, Type: "processor", Version: "1.0.0"}
}
func (c *valuedComponent) InputPorts() []component.Port         { return nil }
func (c *valuedComponent) OutputPorts() []component.Port        { return nil }
func (c *valuedComponent) ConfigSchema() component.ConfigSchema { return component.ConfigSchema{} }
func (c *valuedComponent) Health() component.HealthStatus {
	return component.HealthStatus{Healthy: true}
}
func (c *valuedComponent) DataFlow() component.FlowMetrics { return component.FlowMetrics{} }
func (c *valuedComponent) Initialize() error               { return nil }
func (c *valuedComponent) Stop(context.Context) error      { return nil }
func (c *valuedComponent) Start(_ context.Context) error {
	if c.onStart != nil {
		c.onStart(c.value)
	}
	return nil
}

var _ component.LifecycleComponent = (*valuedComponent)(nil)

// TestIntegration_StartAll_MidBootEditJoinsBootTransaction locks the
// edit-aware half of the boot drain: an EDIT to an existing component that
// becomes locally visible mid-barrier is applied — the component observably
// rebuilt and restarted with the new config — by the time StartAll returns.
//
// REVERT-PROVEN discriminating (deterministically): without the drain, the
// edit's per-key notification is dropped by the cap-1 buffer (the initial
// snapshot occupies it), and the watcher's bulk reconcile of that snapshot
// SKIPS existing components — the edit is never applied at all, and the
// started-value assertion below fails.
func TestIntegration_StartAll_MidBootEditJoinsBootTransaction(t *testing.T) {
	ctx := context.Background()
	testClient := natsclient.NewTestClient(t, natsclient.WithKV())
	defer testClient.Terminate()
	client := testClient.Client

	gated := newBarrierTestComponent("gate-comp")
	gated.entered = make(chan struct{})
	gated.release = make(chan struct{})

	// startedValues records, in order, the config value of every valued-comp
	// instance whose Start ran.
	var (
		valuesMu      sync.Mutex
		startedValues []string
	)
	recordStart := func(v string) {
		valuesMu.Lock()
		startedValues = append(startedValues, v)
		valuesMu.Unlock()
	}

	compRegistry := component.NewRegistry()
	require.NoError(t, compRegistry.RegisterFactory("gate-factory", &component.Registration{
		Name: "gate-factory",
		Type: string(types.ComponentTypeProcessor),
		Factory: func(_ json.RawMessage, _ component.Dependencies) (component.Discoverable, error) {
			return gated, nil
		},
	}))
	require.NoError(t, compRegistry.RegisterFactory("valued-factory", &component.Registration{
		Name: "valued-factory",
		Type: string(types.ComponentTypeProcessor),
		Factory: func(raw json.RawMessage, _ component.Dependencies) (component.Discoverable, error) {
			var cfg struct {
				Value string `json:"value"`
			}
			if err := json.Unmarshal(raw, &cfg); err != nil {
				return nil, err
			}
			return &valuedComponent{name: "valued-comp", value: cfg.Value, onStart: recordStart}, nil
		},
	}))

	h := newBootDrainHarness(t, client, compRegistry, &config.Config{
		Platform: bootDrainPlatform(),
		Components: config.ComponentConfigs{
			"gate-comp": types.ComponentConfig{
				Type:    types.ComponentTypeProcessor,
				Name:    "gate-factory",
				Enabled: true,
				Config:  json.RawMessage(`{}`),
			},
			"valued-comp": types.ComponentConfig{
				Type:    types.ComponentTypeProcessor,
				Name:    "valued-factory",
				Enabled: true,
				Config:  json.RawMessage(`{"value":"A"}`),
			},
		},
	}, &bucketSweepRecordingHandler{})

	vis := h.configManager.OnChange("components.*")
	select {
	case <-vis:
	case <-time.After(10 * time.Second):
		t.Fatal("visibility subscriber never received the initial snapshot")
	}

	done := make(chan error, 1)
	go func() { done <- h.manager.StartAll(ctx) }()
	<-gated.entered

	// Edit valued-comp mid-barrier through the production KV path.
	putComponentConfigKV(t, client, "valued-comp", types.ComponentConfig{
		Type:    types.ComponentTypeProcessor,
		Name:    "valued-factory",
		Enabled: true,
		Config:  json.RawMessage(`{"value":"B"}`),
	})
	awaitConfigVisible(t, vis, "valued-comp edit", func(c *config.Config) bool {
		cfg, ok := c.Components["valued-comp"]
		return ok && bytes.Contains(cfg.Config, []byte(`"B"`))
	})

	close(gated.release)
	select {
	case err := <-done:
		require.NoError(t, err, "boot must succeed with the mid-boot edit applied")
	case <-time.After(30 * time.Second):
		t.Fatal("Manager.StartAll did not return")
	}
	defer func() { _ = h.manager.StopAll(context.Background()) }()

	// The edit joined the boot transaction: the live instance was rebuilt and
	// started with the NEW config before StartAll returned.
	valuesMu.Lock()
	values := append([]string(nil), startedValues...)
	valuesMu.Unlock()
	require.NotEmpty(t, values)
	assert.Equal(t, "B", values[len(values)-1],
		"the mid-boot edit must be applied by the time StartAll returns (started values: %v)", values)

	status := h.cm.GetComponentStatus()
	require.Contains(t, status, "valued-comp")
	assert.Equal(t, component.StateStarted, status["valued-comp"].State)
	for name, st := range status {
		assert.NotEqual(t, component.StateInitialized, st.State,
			"no component may be left StateInitialized after boot (got %s)", name)
	}
}

// registryProbeComponent records which model-registry endpoint set it was
// built against, so a test can assert which registry generation a
// DepModelRegistry dependent is bound to.
type registryProbeComponent struct {
	name string
}

func (c *registryProbeComponent) Meta() component.Metadata {
	return component.Metadata{Name: c.name, Type: "processor", Version: "1.0.0"}
}
func (c *registryProbeComponent) InputPorts() []component.Port  { return nil }
func (c *registryProbeComponent) OutputPorts() []component.Port { return nil }
func (c *registryProbeComponent) ConfigSchema() component.ConfigSchema {
	return component.ConfigSchema{}
}
func (c *registryProbeComponent) Health() component.HealthStatus {
	return component.HealthStatus{Healthy: true}
}
func (c *registryProbeComponent) DataFlow() component.FlowMetrics { return component.FlowMetrics{} }
func (c *registryProbeComponent) Initialize() error               { return nil }
func (c *registryProbeComponent) Start(_ context.Context) error   { return nil }
func (c *registryProbeComponent) Stop(context.Context) error      { return nil }

var _ component.LifecycleComponent = (*registryProbeComponent)(nil)

// TestIntegration_StartAll_MidBootRegistryChangeRebindsDependents locks the
// registry half of the boot drain: a model_registry change that becomes
// locally visible mid-barrier rebinds DepModelRegistry dependents — rebuilt
// against the NEW registry — by the time StartAll returns. Content drift
// against the built-with baseline is what triggers the rebuild, so the cap-1
// buffer dropping the change's notification cannot lose it.
//
// REVERT-PROVEN discriminating (deterministically): without the drain, the
// change's notification is dropped (the initial snapshot occupies the cap-1
// buffer) and the watcher's entry drain blind-DISCARDS that snapshot — the
// change is lost until the NEXT registry change, the dependent stays bound to
// the old registry, and the bound-endpoint assertion below fails.
func TestIntegration_StartAll_MidBootRegistryChangeRebindsDependents(t *testing.T) {
	ctx := context.Background()
	testClient := natsclient.NewTestClient(t, natsclient.WithKV())
	defer testClient.Terminate()
	client := testClient.Client

	gated := newBarrierTestComponent("gate-comp")
	gated.entered = make(chan struct{})
	gated.release = make(chan struct{})

	// boundEndpoints records, in build order, which endpoint set each
	// dep-comp instance was built against ("ep-a" or "ep-b").
	var (
		boundMu        sync.Mutex
		boundEndpoints []string
	)

	compRegistry := component.NewRegistry()
	require.NoError(t, compRegistry.RegisterFactory("gate-factory", &component.Registration{
		Name: "gate-factory",
		Type: string(types.ComponentTypeProcessor),
		Factory: func(_ json.RawMessage, _ component.Dependencies) (component.Discoverable, error) {
			return gated, nil
		},
	}))
	require.NoError(t, compRegistry.RegisterFactory("registry-probe", &component.Registration{
		Name:         "registry-probe",
		Type:         string(types.ComponentTypeProcessor),
		Dependencies: []string{component.DepModelRegistry},
		Factory: func(_ json.RawMessage, deps component.Dependencies) (component.Discoverable, error) {
			bound := "none"
			if deps.ModelRegistry != nil {
				if deps.ModelRegistry.GetEndpoint("ep-b") != nil {
					bound = "ep-b"
				} else if deps.ModelRegistry.GetEndpoint("ep-a") != nil {
					bound = "ep-a"
				}
			}
			boundMu.Lock()
			boundEndpoints = append(boundEndpoints, bound)
			boundMu.Unlock()
			return &registryProbeComponent{name: "dep-comp"}, nil
		},
	}))

	h := newBootDrainHarness(t, client, compRegistry, &config.Config{
		Platform: bootDrainPlatform(),
		ModelRegistry: &model.Registry{
			Endpoints: map[string]*model.EndpointConfig{
				"ep-a": {Provider: "ollama", URL: "http://a/v1", Model: "a", MaxTokens: 1024},
			},
			Defaults: model.DefaultsConfig{Model: "ep-a"},
		},
		Components: config.ComponentConfigs{
			"gate-comp": types.ComponentConfig{
				Type:    types.ComponentTypeProcessor,
				Name:    "gate-factory",
				Enabled: true,
				Config:  json.RawMessage(`{}`),
			},
			"dep-comp": types.ComponentConfig{
				Type:    types.ComponentTypeProcessor,
				Name:    "registry-probe",
				Enabled: true,
				Config:  json.RawMessage(`{}`),
			},
		},
	}, &bucketSweepRecordingHandler{})

	visReg := h.configManager.OnChange("model_registry")
	select {
	case <-visReg:
	case <-time.After(10 * time.Second):
		t.Fatal("registry visibility subscriber never received the initial snapshot")
	}

	done := make(chan error, 1)
	go func() { done <- h.manager.StartAll(ctx) }()
	<-gated.entered

	// Change the model registry mid-barrier through the production KV path.
	newRegistry := &model.Registry{
		Endpoints: map[string]*model.EndpointConfig{
			"ep-b": {Provider: "ollama", URL: "http://b/v1", Model: "b", MaxTokens: 2048},
		},
		Defaults: model.DefaultsConfig{Model: "ep-b"},
	}
	data, err := json.Marshal(newRegistry)
	require.NoError(t, err)
	kv, err := client.GetKeyValueBucket(ctx, "semstreams_config")
	require.NoError(t, err)
	_, err = kv.Put(ctx, "model_registry", data)
	require.NoError(t, err)
	awaitConfigVisible(t, visReg, "model_registry change", func(c *config.Config) bool {
		return c.ModelRegistry != nil && c.ModelRegistry.GetEndpoint("ep-b") != nil
	})

	close(gated.release)
	select {
	case err := <-done:
		require.NoError(t, err, "boot must succeed with the mid-boot registry change applied")
	case <-time.After(30 * time.Second):
		t.Fatal("Manager.StartAll did not return")
	}
	defer func() { _ = h.manager.StopAll(context.Background()) }()

	// The dependent joined the boot transaction: rebuilt against the NEW
	// registry before StartAll returned.
	boundMu.Lock()
	bound := append([]string(nil), boundEndpoints...)
	boundMu.Unlock()
	require.NotEmpty(t, bound)
	assert.Equal(t, "ep-b", bound[len(bound)-1],
		"the DepModelRegistry dependent must be bound to the mid-boot registry by the time StartAll returns (builds: %v)", bound)

	status := h.cm.GetComponentStatus()
	require.Contains(t, status, "dep-comp")
	assert.Equal(t, component.StateStarted, status["dep-comp"].State)
}
