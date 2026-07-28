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

// bucketSweepRecordingHandler captures emitted slog records so a test can assert
// the post-start owned-bucket sweep WARNed naming a stripped bucket. onRecord,
// when set, observes every record as it lands (under mu) so a test can react to
// a specific sweep event.
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

// skipProbeMentioning reports whether the sweep's skip-if-absent Debug probe
// fired naming the bucket. Its message match MUST stay aligned with the
// sweepPassedAbsent gate's — both mirror the Debug line in
// graph.AssertOwnedBucketsClean.
func (h *bucketSweepRecordingHandler) skipProbeMentioning(bucket string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, r := range h.records {
		if r.Level != slog.LevelDebug || !strings.Contains(r.Message, "bucket absent, skipping") {
			continue
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

// bucketAdopterComponent is a real lifecycle component whose Start brings a
// framework-owned bucket into existence DURING this boot's startup carrying a
// foreign 7-day TTL and a stored key — the racing process's dirty create and
// the owner's unchanged adoption (CreateKeyValueBucket never reconciles)
// compressed into one boot-time step. It runs through the production
// ComponentManager launch path — the asynchronous concurrency shape the
// create-race guarantee must hold under. observedTTL records the retention
// actually bound at create time, so the test can prove the dirty state
// existed mid-boot before the sweep stripped it.
//
// sweepPassedAbsent forces the bad interleaving whenever the ordering permits
// it: when the post-start sweep can run concurrently with this Start
// (fire-and-forget), the channel fires on the sweep's skip-if-absent probe of
// the still-absent bucket, and only THEN does Start create it dirty — the
// sweep has already passed the bucket over and never reconciles it. Under the
// barrier that probe cannot happen before Start returns, so the bounded
// fallback fires instead; correctness there is guaranteed by the barrier's
// synchronization, not timing, so the wait costs boot delay only, never flake.
type bucketAdopterComponent struct {
	client            *natsclient.Client
	bucket            string
	sweepPassedAbsent <-chan struct{}

	mu          sync.Mutex
	started     bool
	observedTTL time.Duration
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
	// Wait for the sweep's skip-if-absent probe if concurrency permits one
	// (fire-and-forget), else time out and proceed (barrier — the probe cannot
	// precede this Start's return). Wall-clock rationale: 500ms is orders of
	// magnitude past the sweep's few-ms arrival at the bucket, and in the
	// barrier direction the timeout can only delay boot, never flip an
	// assertion — ordering there is synchronization-guaranteed.
	if c.sweepPassedAbsent != nil {
		select {
		case <-c.sweepPassedAbsent:
		case <-time.After(500 * time.Millisecond):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
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
	// Record the retention actually bound at create time: proof the bucket
	// carried the foreign TTL mid-boot, before the post-start sweep ran.
	maxAge, _, err := natsclient.BucketRetention(ctx, kv)
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.started = true
	c.observedTTL = maxAge
	c.mu.Unlock()
	return nil
}
func (c *bucketAdopterComponent) Stop(_ time.Duration) error { return nil }

func (c *bucketAdopterComponent) createTimeTTL() time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.observedTTL
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
func (c *failingStartComponent) Stop(_ time.Duration) error { return nil }

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

// TestIntegration_StartAll_PostStartSweepStripsCreateRaceTTL is the F1
// production-wire test: it drives the real Manager.StartAll seam with the REAL
// asynchronous ComponentManager (not a synchronous stand-in — a sync mock
// proves an ordering production does not have; that is how the original P0
// shipped) and proves the post-start owned-bucket sweep closes the create-race
// coverage hole the pre-start belt cannot reach.
//
// The guarded bucket does NOT exist before StartAll (the spec's GIVEN): the
// adopting component's Start brings it into existence DURING this boot's own
// startup, carrying the foreign 7-day TTL and a stored key. The component-start
// barrier orders the post-start sweep after every component Start has returned,
// so StartAll must strip the TTL in place, preserve the stored key, and WARN
// naming the bucket — all BEFORE the HTTP surface comes up.
//
// This shape is DISCRIMINATING for the barrier: revert ComponentManager.Start
// to fire-and-forget and this test goes red. The component's dirty create is
// gated on the sweep's own skip-if-absent probe of the bucket, so whenever the
// sweep CAN run concurrently with component Start (fire-and-forget), the bad
// interleaving is forced — the sweep passes the not-yet-created bucket over,
// the component then creates it dirty, and the TTL survives to fail the strip
// assertion. A pre-boot dirty create (or an ungated mid-boot create, which
// wins the race against the sweep's earlier bucket probes) would pass under
// either ordering and prove nothing.
func TestIntegration_StartAll_PostStartSweepStripsCreateRaceTTL(t *testing.T) {
	ctx := context.Background()
	testClient := natsclient.NewTestClient(t, natsclient.WithKV())
	defer testClient.Terminate()
	client := testClient.Client

	// sweepPassedAbsent fires when the post-start sweep logs its skip-if-absent
	// probe of the guarded bucket — the observable event proving the sweep
	// already passed the bucket over (only possible under fire-and-forget).
	sweepPassedAbsent := make(chan struct{})
	var sweepOnce sync.Once
	rec := &bucketSweepRecordingHandler{onRecord: func(r slog.Record) {
		if !strings.Contains(r.Message, "bucket absent, skipping") {
			return
		}
		matched := false
		r.Attrs(func(a slog.Attr) bool {
			if strings.Contains(a.Value.String(), graph.BucketEmbeddingIndex) {
				matched = true
				return false
			}
			return true
		})
		if matched {
			sweepOnce.Do(func() { close(sweepPassedAbsent) })
		}
	}}

	// The adopting component, registered through the production factory path.
	// The factory captures the instance so the test can read the retention it
	// observed at create time.
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
				client:            deps.NATSClient,
				bucket:            graph.BucketEmbeddingIndex,
				sweepPassedAbsent: sweepPassedAbsent,
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
	// Inject the recording logger so the sweep's WARN (and the skip-if-absent
	// probe the adopter gates on) are observable, and the NATS client the
	// post-start sweep reads. Both are in-package private fields.
	manager.BaseService = NewBaseServiceWithOptions("service-manager-registry", nil, WithLogger(slog.New(rec)))
	manager.natsClient = client
	manager.RegisterInstance("component-manager", cm)

	// Precondition (the discriminating GIVEN): the guarded bucket must not
	// exist yet — it is created dirty during the boot below.
	_, err := client.GetKeyValueBucket(ctx, graph.BucketEmbeddingIndex)
	require.ErrorIs(t, err, jetstream.ErrBucketNotFound,
		"precondition: the guarded bucket must not exist before StartAll")

	require.NoError(t, manager.StartAll(ctx))
	defer func() { _ = manager.StopAll(2 * time.Second) }()

	// The adopting component started through the real ComponentManager wire,
	// and the bucket really carried the foreign TTL when it was created
	// mid-boot — so a clean post-StartAll state below is the sweep's doing.
	status := cm.GetComponentStatus()
	require.Contains(t, status, "embedding-index-adopter")
	require.Equal(t, component.StateStarted, status["embedding-index-adopter"].State,
		"the adopting component must have started through the production launch path")
	adopterMu.Lock()
	started := adopter
	adopterMu.Unlock()
	require.NotNil(t, started, "the factory must have built the adopting component")
	require.Equal(t, 7*24*time.Hour, started.createTimeTTL(),
		"the bucket must have carried the foreign TTL at its mid-boot creation")

	// The post-start sweep stripped the create-race TTL in place.
	fresh, err := client.GetKeyValueBucket(ctx, graph.BucketEmbeddingIndex)
	require.NoError(t, err)
	maxAge, maxBytes, err := natsclient.BucketRetention(ctx, fresh)
	require.NoError(t, err)
	assert.Equal(t, time.Duration(0), maxAge, "StartAll's post-start sweep must strip the create-race TTL")
	assert.LessOrEqual(t, maxBytes, int64(0), "the sweep must leave MaxBytes non-binding")

	// The stored key survived the strip.
	entry, err := fresh.Get(ctx, "entity.key.one")
	require.NoError(t, err, "the stored key must survive the strip")
	assert.Equal(t, []byte("survivor"), entry.Value())

	// A WARN naming the stripped bucket fired.
	assert.True(t, rec.warnMentioning(graph.BucketEmbeddingIndex),
		"the post-start sweep must WARN naming the stripped bucket %s", graph.BucketEmbeddingIndex)

	// Log-drift canary: the sweepPassedAbsent gate substring-matches the sweep's
	// skip-if-absent Debug line. If that line is ever reworded, the gate never
	// fires and — under the barrier — nothing visibly changes (the bounded
	// fallback keeps this test green) while the test silently degrades to the
	// non-discriminating ungated shape. ENTITY_STATES is absent in this boot
	// (no graph-ingest), so the sweep must emit its skip probe for it on every
	// run under either ordering; a reword fails here immediately, forcing the
	// gate's match string to be re-aligned in the same PR.
	require.True(t, rec.skipProbeMentioning(graph.BucketEntityStates),
		"log-drift canary: the sweep's skip-if-absent Debug probe for %s was not captured — "+
			"if its message changed, update the sweepPassedAbsent gate's match string to keep "+
			"this test discriminating", graph.BucketEntityStates)
}

// TestIntegration_StartAll_BootFailsClosedOnComponentStartFailure locks the
// framework-composition fail-closed scenario at the composition-root level: a
// registered lifecycle component whose Start returns an error must fail
// Manager.StartAll with an error naming the component, and the HTTP surface
// must never be brought up.
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
	defer func() { _ = manager.StopAll(2 * time.Second) }()

	require.Error(t, err, "a component Start failure must fail Manager.StartAll (boot fails closed)")
	assert.Contains(t, err.Error(), "doomed-component", "the boot error must name the failed component")
	assert.ErrorIs(t, err, errSimulatedStartFailure,
		"the component's own Start error must survive the propagation chain unwrapped-able")

	// The failed component is recorded with its failure.
	status := cm.GetComponentStatus()
	require.Contains(t, status, "doomed-component")
	assert.Equal(t, component.StateFailed, status["doomed-component"].State)
	assert.Error(t, status["doomed-component"].LastError)

	// HTTP was never brought up: completeHTTPSetup (which creates the server)
	// is only reached after every service Start succeeds.
	manager.mu.RLock()
	httpServer := manager.httpServer
	manager.mu.RUnlock()
	assert.Nil(t, httpServer, "the HTTP surface must never come up when boot fails closed")
}
