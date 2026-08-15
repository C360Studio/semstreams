//go:build integration

package rule

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
)

// TestEntityWatcherHardeningRealNATS exercises the watcher lifecycle against
// real JetStream KV. Deterministic unit tests cover the exact lock seams; this
// proof keeps the transport bootstrap, overlapping subscriptions, live
// coalescing, delete cancellation, dynamic replacement, and shutdown drain in
// one end-to-end watcher lifecycle.
func TestEntityWatcherHardeningRealNATS(t *testing.T) {
	testClient, err := natsclient.NewSharedTestClient(
		natsclient.WithJetStream(),
		natsclient.WithKV(),
		natsclient.WithTestTimeout(10*time.Second),
		natsclient.WithStartTimeout(30*time.Second),
	)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, testClient.Terminate())
	})

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	js, err := testClient.Client.JetStream()
	require.NoError(t, err)
	kv, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: graph.BucketEntityStates})
	require.NoError(t, err)

	bootstrapID := "acme.prod.robotics.lab.sensor.bootstrap"
	putWatcherState(t, ctx, kv, bootstrapID)

	config := mustTestConfig(t, "watcher-hardening-integration")
	config.DebounceDelayMs = 100 * time.Millisecond
	config.EntityWatchBuckets = map[string][]string{
		graph.BucketEntityStates: {
			"acme.prod.robotics.*.*.*",
			"acme.*.robotics.gcs.*.*",
		},
	}
	processor, err := NewProcessorWithMetrics(testClient.Client, &config, nil)
	require.NoError(t, err)
	require.NoError(t, processor.Initialize())

	counter := &watcherIntegrationCounterRule{name: "entity"}
	processor.mu.Lock()
	processor.rules[counter.name] = counter
	processor.ruleDefinitions[counter.name] = Definition{
		ID: counter.name,
		Entity: EntityConfig{
			Pattern: "*.*.*.*.*.*",
		},
	}
	processor.mu.Unlock()

	require.NoError(t, processor.Start(ctx))
	stopped := false
	t.Cleanup(func() {
		if !stopped {
			_ = processor.Stop(context.Background())
		}
	})

	require.Eventually(t, func() bool {
		return counter.evaluated.Load() == 1
	}, 5*time.Second, 10*time.Millisecond, "bootstrap entity must evaluate after watcher bootstrap")

	// This ID matches both active patterns. The two live deliveries carry
	// distinct provenance, but the batch fetches and evaluates the entity once.
	overlapID := "acme.prod.robotics.gcs.drone.overlap"
	putWatcherState(t, ctx, kv, overlapID)
	require.Eventually(t, func() bool {
		return counter.evaluated.Load() == 2
	}, 5*time.Second, 10*time.Millisecond, "overlapping watchers must coalesce one live evaluation")
	require.Never(t, func() bool {
		return counter.evaluated.Load() > 2
	}, 350*time.Millisecond, 10*time.Millisecond, "overlapping watchers must not duplicate evaluation")

	// A delete observed inside the settling window removes both provenance keys
	// before the coalescer can fetch the transient state.
	deletedID := "acme.prod.robotics.gcs.drone.deleted"
	putWatcherState(t, ctx, kv, deletedID)
	require.NoError(t, kv.Delete(ctx, deletedID))
	require.Never(t, func() bool {
		return counter.evaluated.Load() > 2
	}, 500*time.Millisecond, 10*time.Millisecond, "delete must cancel all queued work for the entity")

	// Replace both robotics generations with one logistics generation. The old
	// physical watchers may still be winding down, but they no longer have
	// dispatch authority.
	require.NoError(t, processor.UpdateWatchBuckets(map[string][]string{
		graph.BucketEntityStates: {"acme.prod.logistics.*.*.*"},
	}))
	putWatcherState(t, ctx, kv, "acme.prod.robotics.gcs.drone.retired")
	putWatcherState(t, ctx, kv, "acme.prod.logistics.fleet.truck.current")
	require.Eventually(t, func() bool {
		return counter.evaluated.Load() == 3
	}, 5*time.Second, 10*time.Millisecond, "replacement generation must evaluate its matching entity")
	require.Never(t, func() bool {
		return counter.evaluated.Load() > 3
	}, 350*time.Millisecond, 10*time.Millisecond, "retired generations must not evaluate")

	require.NoError(t, processor.Stop(context.Background()))
	stopped = true
	active, idle := processor.entityEvaluationFence.counts()
	require.Zero(t, active)
	require.Zero(t, idle)
	if processor.entityCoalescer != nil {
		require.Zero(t, processor.entityCoalescer.PendingCount())
	}
}

type watcherIntegrationCounterRule struct {
	name      string
	evaluated atomic.Int64
}

func (r *watcherIntegrationCounterRule) Name() string        { return r.name }
func (r *watcherIntegrationCounterRule) Subscribe() []string { return nil }
func (r *watcherIntegrationCounterRule) Evaluate([]message.Message) bool {
	return false
}
func (r *watcherIntegrationCounterRule) ExecuteEvents([]message.Message) ([]Event, error) {
	return nil, nil
}
func (r *watcherIntegrationCounterRule) EvaluateEntityState(*graph.EntityState) bool {
	r.evaluated.Add(1)
	return false
}

func putWatcherState(t *testing.T, ctx context.Context, kv jetstream.KeyValue, entityID string) {
	t.Helper()
	encoded, err := json.Marshal(graph.EntityState{
		ID:        entityID,
		Version:   1,
		UpdatedAt: time.Now(),
	})
	require.NoError(t, err)
	_, err = kv.Put(ctx, entityID, encoded)
	require.NoError(t, err)
}
