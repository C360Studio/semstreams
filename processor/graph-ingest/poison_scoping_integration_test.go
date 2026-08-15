//go:build integration

// poison-response-scoping task 7.2 — wire-level proof of snapshot-then-stop
// (design D1): after Start's boot snapshot sweep completes, graph-ingest holds
// NO consumer on the ENTITY_STATES backing stream, and a post-Start entity
// write is delivered to no graph-ingest watcher. This is the retirement of the
// per-mutation guard fan-out semboids measured (gh#562).

package graphingest

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// entityStatesConsumerCount reads the current consumer count on the
// KV_ENTITY_STATES backing stream via the JetStream API — authoritative
// server-side state, not component bookkeeping.
func entityStatesConsumerCount(t *testing.T, ctx context.Context, natsClient *natsclient.Client) int {
	t.Helper()
	js, err := natsClient.JetStream()
	require.NoError(t, err)
	stream, err := js.Stream(ctx, "KV_ENTITY_STATES")
	require.NoError(t, err)
	info, err := stream.Info(ctx)
	require.NoError(t, err)
	return info.State.Consumers
}

func TestIntegration_NoGuardConsumerOnEntityStatesAfterStart(t *testing.T) {
	ctx := context.Background()

	streams := []natsclient.TestStreamConfig{
		{Name: "ENTITY", Subjects: []string{"entity.>"}},
	}
	testClient := natsclient.NewTestClient(t, natsclient.WithKV(), natsclient.WithStreams(streams...))
	natsClient := testClient.Client

	config := DefaultConfig()
	deps := component.Dependencies{NATSClient: natsClient}
	configJSON, err := json.Marshal(config)
	require.NoError(t, err)
	comp, err := CreateGraphIngest(configJSON, deps)
	require.NoError(t, err)
	c := comp.(*Component)
	require.NoError(t, c.Initialize())

	// Seed one resident entity so the snapshot sweep has real pre-marker work.
	const seededID = "c360.test.poison.scope.entity.pre"
	require.NoError(t, c.Start(ctx))
	t.Cleanup(func() { _ = c.Stop(context.Background()) })

	// The sweep watcher is stopped inside Start (snapshot-then-stop). Its
	// ephemeral ordered consumer is deleted asynchronously by the server;
	// poll the authoritative stream info until it settles at zero.
	require.Eventually(t, func() bool {
		return entityStatesConsumerCount(t, ctx, natsClient) == 0
	}, 10*time.Second, 100*time.Millisecond,
		"ENTITY_STATES must carry no graph-ingest guard consumer at steady state")

	// A post-Start write commits and is delivered to NO graph-ingest watcher:
	// the consumer count on the backing stream stays zero across the write.
	require.NoError(t, c.MergeEntity(ctx, &graph.EntityState{
		ID: seededID,
		Triples: []message.Triple{{
			Subject: seededID, Predicate: "test.state.value", Object: "post-start",
			Timestamp: time.Now(), Confidence: 1.0,
		}},
	}))
	assert.Equal(t, 0, entityStatesConsumerCount(t, ctx, natsClient),
		"a steady-state entity write must not be re-delivered to its writer")

	// The write is still fully readable through the query lane (enforcement
	// rides the per-read decode, not a watcher).
	data, err := c.handleQueryEntityNATS(ctx, []byte(`{"id":"`+seededID+`"}`))
	require.NoError(t, err)
	assert.Contains(t, string(data), seededID)

	// Health/readiness are clean: the deliberate stop was not classified as
	// watch loss.
	assert.False(t, c.entityWatchLost.Load(), "deliberate stop misclassified as watch loss")
	require.NoError(t, c.ensureEntityQueriesReady())
	health := c.Health()
	assert.True(t, health.Healthy, "steady state after snapshot-then-stop must be healthy: %+v", health)
}

// TestIntegration_BootSweepInventoriesResidentPoisonRealNATS drives the boot
// sweep against real NATS: resident poison seeded BEFORE Start is inventoried
// while ingest boots and unaffected entities keep serving; delete + recreate
// recovers Health without a restart (spec scenarios on the real wire).
func TestIntegration_BootSweepInventoriesResidentPoisonRealNATS(t *testing.T) {
	ctx := context.Background()

	streams := []natsclient.TestStreamConfig{
		{Name: "ENTITY", Subjects: []string{"entity.>"}},
	}
	testClient := natsclient.NewTestClient(t, natsclient.WithKV(), natsclient.WithStreams(streams...))
	natsClient := testClient.Client

	// Seed resident poison BEFORE the component boots: raw bytes written
	// around the MarshalEntityState write gate — how poison arises in the field.
	const poisonID = "c360.test.poison.scope.entity.dirty"
	const validID = "c360.test.poison.scope.entity.clean"
	kv, err := natsClient.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{Bucket: graph.BucketEntityStates})
	require.NoError(t, err)
	_, err = kv.Put(ctx, poisonID,
		[]byte(`{"id":"`+poisonID+`","triples":[{"subject":"`+poisonID+`","predicate":"legacy.predicate","object":1}]}`)) // predicate-audit:invalid {"kind":"stored-predicate","value":"legacy.predicate","reason":"arity"}
	require.NoError(t, err)
	_, err = kv.Put(ctx, validID, []byte(`{"id":"`+validID+`","triples":[]}`))
	require.NoError(t, err)

	config := DefaultConfig()
	deps := component.Dependencies{NATSClient: natsClient}
	configJSON, err := json.Marshal(config)
	require.NoError(t, err)
	comp, err := CreateGraphIngest(configJSON, deps)
	require.NoError(t, err)
	c := comp.(*Component)
	require.NoError(t, c.Initialize())
	require.NoError(t, c.Start(ctx))
	t.Cleanup(func() { _ = c.Stop(context.Background()) })

	// Boots with the poison inventoried; queries ready.
	require.NoError(t, c.ensureEntityQueriesReady())
	rec, inventoried := poisonInventoryEntry(c, poisonID)
	require.True(t, inventoried, "boot sweep must inventory resident poison")
	assert.NotZero(t, rec.revision, "inventory record must carry the failing KV revision")
	health := c.Health()
	assert.False(t, health.Healthy)
	assert.Equal(t, graph.IndexStateDegraded, health.Status)

	// Per-entity scope on the real wire: poisoned refuses, valid serves.
	_, err = c.handleQueryEntityNATS(ctx, []byte(`{"id":"`+poisonID+`"}`))
	assertIngestResetRequired(t, err)
	data, err := c.handleQueryEntityNATS(ctx, []byte(`{"id":"`+validID+`"}`))
	require.NoError(t, err)
	assert.Contains(t, string(data), validID)

	// Canonical repair: delete + recreate recovers Health without restart.
	require.NoError(t, c.deleteEntityAtRevision(ctx, poisonID, rec.revision))
	_, inventoried = poisonInventoryEntry(c, poisonID)
	assert.False(t, inventoried)
	require.NoError(t, c.CreateEntity(ctx, &graph.EntityState{
		ID: poisonID,
		Triples: []message.Triple{{
			Subject: poisonID, Predicate: "test.state.value", Object: "reborn",
			Timestamp: time.Now(), Confidence: 1.0,
		}},
	}))
	data, err = c.handleQueryEntityNATS(ctx, []byte(`{"id":"`+poisonID+`"}`))
	require.NoError(t, err, "recreated entity must serve without restart")
	assert.Contains(t, string(data), "reborn")
	health = c.Health()
	assert.True(t, health.Healthy, "Health must recover after repair: %+v", health)
}
