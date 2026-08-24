//go:build integration

package service

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/config"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/types"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// bucketSweepRecordingHandler captures emitted slog records so a test can
// assert on boot-time log evidence. onRecord, when set, observes every record
// as it lands (under mu) so a test can react to a specific event.
type bucketSweepRecordingHandler struct {
	mu       sync.Mutex
	records  []slog.Record
	onRecord func(slog.Record)
}

func (h *bucketSweepRecordingHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *bucketSweepRecordingHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r.Clone())
	if h.onRecord != nil {
		h.onRecord(r)
	}
	return nil
}
func (h *bucketSweepRecordingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *bucketSweepRecordingHandler) WithGroup(string) slog.Handler      { return h }

func (h *bucketSweepRecordingHandler) warnMentioning(bucket string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, r := range h.records {
		if r.Level != slog.LevelWarn {
			continue
		}
		if strings.Contains(r.Message, bucket) {
			return true
		}
		found := false
		r.Attrs(func(a slog.Attr) bool {
			if strings.Contains(a.Value.String(), bucket) {
				found = true
				return false
			}
			return true
		})
		if found {
			return true
		}
	}
	return false
}

// bucketAdopterComponent is a real lifecycle component whose Start reproduces
// the create-race shape INSIDE the boot transaction and then closes it the way
// every real owner now does: a rival's dirty create (foreign 7-day TTL + a
// stored key, raw CreateKeyValueBucket — no reconcile) followed by the owner's
// acquisition through the catalog seam, which must reconcile the adopted-dirty
// bucket AT ACQUISITION. observedDirtyTTL records the retention present after
// the rival create, proving the dirty state really existed mid-boot before the
// seam stripped it — there is no post-start sweep left to strip it later.
type bucketAdopterComponent struct {
	client *natsclient.Client
	bucket string

	mu               sync.Mutex
	started          bool
	observedDirtyTTL time.Duration
}

func (c *bucketAdopterComponent) Meta() component.Metadata {
	return component.Metadata{Name: "bucket-adopter", Type: "processor", Version: "1.0.0"}
}
func (c *bucketAdopterComponent) InputPorts() []component.Port  { return nil }
func (c *bucketAdopterComponent) OutputPorts() []component.Port { return nil }
func (c *bucketAdopterComponent) ConfigSchema() component.ConfigSchema {
	return component.ConfigSchema{}
}
func (c *bucketAdopterComponent) Health() component.HealthStatus {
	c.mu.Lock()
	defer c.mu.Unlock()
	return component.HealthStatus{Healthy: c.started, LastCheck: time.Now()}
}
func (c *bucketAdopterComponent) DataFlow() component.FlowMetrics {
	return component.FlowMetrics{LastActivity: time.Now()}
}
func (c *bucketAdopterComponent) Initialize() error { return nil }
func (c *bucketAdopterComponent) Start(ctx context.Context) error {
	// The rival's dirty create: raw, unreconciled, foreign TTL, stored key.
	kv, err := c.client.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{
		Bucket: c.bucket,
		TTL:    7 * 24 * time.Hour,
	})
	if err != nil {
		return err
	}
	if _, err := kv.Put(ctx, "entity.key.one", []byte("survivor")); err != nil {
		return err
	}
	dirtyTTL, _, err := natsclient.BucketRetention(ctx, kv)
	if err != nil {
		return err
	}

	// The owner's acquisition through the catalog seam — the reconcile point.
	if _, err := graph.EnsureCatalogBucket(ctx, c.client, c.bucket); err != nil {
		return err
	}

	c.mu.Lock()
	c.started = true
	c.observedDirtyTTL = dirtyTTL
	c.mu.Unlock()
	return nil
}
func (c *bucketAdopterComponent) Stop(context.Context) error { return nil }

func (c *bucketAdopterComponent) dirtyTTLBeforeSeam() time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.observedDirtyTTL
}

var _ component.LifecycleComponent = (*bucketAdopterComponent)(nil)

// failingStartComponent is a real lifecycle component whose Start always
// fails, for the boot-fails-closed path.
type failingStartComponent struct{}

func (c *failingStartComponent) Meta() component.Metadata {
	return component.Metadata{Name: "failing-start", Type: "processor", Version: "1.0.0"}
}
func (c *failingStartComponent) InputPorts() []component.Port  { return nil }
func (c *failingStartComponent) OutputPorts() []component.Port { return nil }
func (c *failingStartComponent) ConfigSchema() component.ConfigSchema {
	return component.ConfigSchema{}
}
func (c *failingStartComponent) Health() component.HealthStatus {
	return component.HealthStatus{Healthy: false, LastCheck: time.Now()}
}
func (c *failingStartComponent) DataFlow() component.FlowMetrics {
	return component.FlowMetrics{LastActivity: time.Now()}
}
func (c *failingStartComponent) Initialize() error { return nil }
func (c *failingStartComponent) Start(_ context.Context) error {
	return errSimulatedStartFailure
}
func (c *failingStartComponent) Stop(context.Context) error { return nil }

var _ component.LifecycleComponent = (*failingStartComponent)(nil)

var errSimulatedStartFailure = errors.New("simulated component start failure")

// newGuardsTestComponentManager builds a REAL ComponentManager through the
// production constructor: component configs flow from a config.Manager, and
// component instances are created through the component registry's factory
// path — the exact wire cmd/semstreams boots through.
func newGuardsTestComponentManager(
	t *testing.T,
	client *natsclient.Client,
	registry *component.Registry,
	components config.ComponentConfigs,
) *ComponentManager {
	t.Helper()

	initialCfg := &config.Config{
		Platform: config.PlatformConfig{
			Org:         "test",
			ID:          "guards-test",
			InstanceID:  "guards-001",
			Environment: "test",
		},
		Components: components,
	}
	configManager, err := config.NewConfigManager(initialCfg, client, slog.Default())
	require.NoError(t, err)

	deps := &Dependencies{
		NATSClient:        client,
		Manager:           configManager,
		Logger:            slog.Default(),
		ComponentRegistry: registry,
	}
	cmService, err := NewComponentManager(json.RawMessage(`{}`), deps)
	require.NoError(t, err)
	return cmService.(*ComponentManager)
}

// TestIntegration_StartAll_OwnerSeamReconcilesCreateRaceDirtInsideBoot drives
// the real Manager.StartAll wire and proves the retired post-start sweep's
// justified class — a bucket created dirty DURING this boot's own startup — is
// now closed inside the owning component's Start by the acquisition seam: the
// bucket demonstrably carried the foreign TTL mid-boot (after the rival's raw
// create) and is clean the moment StartAll returns, with the stored key
// preserved and NO sweep pass having run (StartAll no longer has one).
func TestIntegration_StartAll_OwnerSeamReconcilesCreateRaceDirtInsideBoot(t *testing.T) {
	ctx := context.Background()
	testClient := natsclient.NewTestClient(t, natsclient.WithKV())
	defer testClient.Terminate()
	client := testClient.Client

	var (
		adopterMu sync.Mutex
		adopter   *bucketAdopterComponent
	)
	compRegistry := component.NewRegistry()
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
	cm := newGuardsTestComponentManager(t, client, compRegistry, config.ComponentConfigs{
		"embedding-index-adopter": types.ComponentConfig{
			Type:    types.ComponentTypeProcessor,
			Name:    "bucket-adopter",
			Enabled: true,
			Config:  json.RawMessage(`{}`),
		},
	})

	manager := NewServiceManager(NewServiceRegistry())
	manager.BaseService = NewBaseServiceWithOptions("service-manager-registry", nil,
		WithLogger(slog.New(&bucketSweepRecordingHandler{})))
	manager.natsClient = client
	manager.RegisterInstance("component-manager", cm)

	// Precondition: the guarded bucket does not exist before boot — it is
	// created dirty during the boot below.
	_, err := client.GetKeyValueBucket(ctx, graph.BucketEmbeddingIndex)
	require.ErrorIs(t, err, jetstream.ErrBucketNotFound,
		"precondition: the guarded bucket must not exist before StartAll")

	require.NoError(t, manager.StartAll(ctx))
	defer func() { _ = manager.StopAll(context.Background()) }()

	// The component started through the real ComponentManager wire, and the
	// bucket really carried the foreign TTL mid-boot before the seam ran.
	status := cm.GetComponentStatus()
	require.Contains(t, status, "embedding-index-adopter")
	require.Equal(t, component.StateStarted, status["embedding-index-adopter"].State,
		"the adopting component must have started through the production launch path")
	adopterMu.Lock()
	started := adopter
	adopterMu.Unlock()
	require.NotNil(t, started, "the factory must have built the adopting component")
	require.Equal(t, 7*24*time.Hour, started.dirtyTTLBeforeSeam(),
		"the bucket must have carried the foreign TTL mid-boot, before the seam acquisition")

	// The seam stripped the create-race TTL inside the owner's Start.
	fresh, err := client.GetKeyValueBucket(ctx, graph.BucketEmbeddingIndex)
	require.NoError(t, err)
	maxAge, maxBytes, err := natsclient.BucketRetention(ctx, fresh)
	require.NoError(t, err)
	assert.Equal(t, time.Duration(0), maxAge,
		"the owner's seam acquisition must strip the create-race TTL before StartAll returns")
	assert.LessOrEqual(t, maxBytes, int64(0), "the seam must leave MaxBytes non-binding")

	// The stored key survived the strip.
	entry, err := fresh.Get(ctx, "entity.key.one")
	require.NoError(t, err, "the stored key must survive the strip")
	assert.Equal(t, []byte("survivor"), entry.Value())
}

// TestIntegration_StartAll_BootFailsClosedOnComponentStartFailure locks the
// framework-composition fail-closed scenario at the composition-root level: a
// registered lifecycle component whose Start returns an error must fail
// Manager.StartAll with an error naming the component. Startup diagnostics bind
// before the barrier, but failure must never promote full routes or leave the
// listener behind.
func TestIntegration_StartAll_BootFailsClosedOnComponentStartFailure(t *testing.T) {
	ctx := context.Background()
	testClient := natsclient.NewTestClient(t, natsclient.WithKV())
	defer testClient.Terminate()
	client := testClient.Client

	compRegistry := component.NewRegistry()
	require.NoError(t, compRegistry.RegisterFactory("failing-start", &component.Registration{
		Name: "failing-start",
		Type: string(types.ComponentTypeProcessor),
		Factory: func(_ json.RawMessage, _ component.Dependencies) (component.Discoverable, error) {
			return &failingStartComponent{}, nil
		},
	}))
	cm := newGuardsTestComponentManager(t, client, compRegistry, config.ComponentConfigs{
		"doomed-component": types.ComponentConfig{
			Type:    types.ComponentTypeProcessor,
			Name:    "failing-start",
			Enabled: true,
			Config:  json.RawMessage(`{}`),
		},
	})

	manager := NewServiceManager(NewServiceRegistry())
	manager.BaseService = NewBaseServiceWithOptions("service-manager-registry", nil,
		WithLogger(slog.New(&bucketSweepRecordingHandler{})))
	manager.natsClient = client
	manager.RegisterInstance("component-manager", cm)

	err := manager.StartAll(ctx)
	defer func() { _ = manager.StopAll(context.Background()) }()

	require.Error(t, err, "a component Start failure must fail Manager.StartAll (boot fails closed)")
	assert.Contains(t, err.Error(), "doomed-component", "the boot error must name the failed component")
	assert.ErrorIs(t, err, errSimulatedStartFailure,
		"the component's own Start error must survive the propagation chain unwrapped-able")
	startup := manager.currentStartupSnapshot()
	assert.Equal(t, "failed", startup.Status)
	assert.Equal(t, 1, startup.Services.StartsFailed)
	assert.Equal(t, 1, startup.Components.StartsFailed)

	// The returned boot error retains the component failure, while successful
	// synchronous rollback leaves the acquired component record stopped.
	status := cm.GetComponentStatus()
	require.Contains(t, status, "doomed-component")
	assert.Equal(t, component.StateStopped, status["doomed-component"].State)
	assert.NoError(t, status["doomed-component"].LastError)

	// The diagnostic server was acquired before the component barrier, then
	// synchronously released without promoting the full route set.
	manager.mu.RLock()
	httpServer := manager.httpServer
	httpUsed := manager.httpUsed
	httpTerminal := manager.httpTerminal
	manager.mu.RUnlock()
	assert.True(t, httpUsed, "startup diagnostics must bind before component Start")
	assert.True(t, httpTerminal, "failed boot must terminally clean up startup diagnostics")
	assert.Nil(t, httpServer, "failed boot must release the shared listener")
}
