//go:build integration

// Integration tests for the ADR-072 keyed-ingest redelivery guard's DURABLE
// tier against a real NATS KV bucket. The in-memory tier is unit-tested in
// keyed_ingest_test.go; here we pin the property that makes the guard correct
// across a process restart and cache eviction (round-4 findings B2/B3): the
// durable stamp catches a stale redelivery even when the in-memory tier has
// lost the entry.

package graphingest

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIntegration_IngestGuardBucket_ReconcilesTTLBucketAtAcquisition pins the
// ADR-072 B2/B3 durability guarantee at boot, in its catalog-seam form: the
// durable guard bucket is no-eviction correctness state, so if a stale/foreign
// deploy pre-created GRAPH_INGEST_APPLIED_SEQ with a TTL, graph-ingest's seam
// acquisition STRIPS the retention in place (self-heal, no stored stamp lost)
// and Start proceeds on a clean bucket — a strictly stronger posture than the
// retired at-creation assert, which could only fail boot and leave the dirt.
// (An UNSTRIPPABLE retention still fails Start closed; that arm is pinned at
// the seam's unit level in natsclient.)
func TestIntegration_IngestGuardBucket_ReconcilesTTLBucketAtAcquisition(t *testing.T) {
	ctx := context.Background()
	streams := []natsclient.TestStreamConfig{
		{Name: "ENTITY", Subjects: []string{"entity.>"}},
	}
	testClient := natsclient.NewTestClient(t, natsclient.WithKV(), natsclient.WithStreams(streams...))

	// Pre-create the guard bucket WITH a TTL (a stale/foreign deploy) and a
	// stored sequence stamp that must survive the reconcile.
	dirty, err := testClient.Client.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{
		Bucket: graph.BucketGraphIngestAppliedSeq,
		TTL:    24 * time.Hour,
	})
	require.NoError(t, err)
	_, err = dirty.Put(ctx, "some.entity.id/ENTITY", []byte("41"))
	require.NoError(t, err)

	cfg := DefaultConfig()
	cfgJSON, err := json.Marshal(cfg)
	require.NoError(t, err)
	comp, err := CreateGraphIngest(cfgJSON, testDependencies(t, testClient.Client, withAuthority("c360", "test")))
	require.NoError(t, err)
	c := comp.(*Component)
	require.NoError(t, c.Initialize())
	t.Cleanup(func() { _ = c.Stop(context.Background()) })

	// Start succeeds — and the acquisition reconciled the bucket to its
	// declared no-lifecycle policy.
	require.NoError(t, c.Start(ctx),
		"Start must self-heal a strippable TTL at the seam, not fail on it")

	fresh, err := testClient.Client.GetKeyValueBucket(ctx, graph.BucketGraphIngestAppliedSeq)
	require.NoError(t, err)
	maxAge, maxBytes, err := natsclient.BucketRetention(ctx, fresh)
	require.NoError(t, err)
	assert.Equal(t, time.Duration(0), maxAge, "the seam must strip the foreign TTL")
	assert.LessOrEqual(t, maxBytes, int64(0))

	entry, err := fresh.Get(ctx, "some.entity.id/ENTITY")
	require.NoError(t, err, "the stored sequence stamp must survive the strip")
	assert.Equal(t, []byte("41"), entry.Value())
}

// TestIntegration_KeyedIngest_PublishedEntityIngestsThroughPool drives the
// ASSEMBLED wire end-to-end: a message published to the ENTITY stream is picked
// up by the real graph-ingest consumer, decoded once, submitted to the keyed
// pool, applied by processIngest, and acked — landing in ENTITY_STATES. None of
// the other tests exercise the consume→pool→ingest composition; this pins that
// the pool wiring actually processes published messages (the happy path).
func TestIntegration_KeyedIngest_PublishedEntityIngestsThroughPool(t *testing.T) {
	ctx, c, testClient := startKeyedWireComponent(t)

	const entityID = "c360.test.wire.keyed.entity.001"
	now := time.Now()
	payload := &mergeTestGraphable{
		entityID: entityID,
		triples: []message.Triple{
			{Subject: entityID, Predicate: "wire.state.status", Object: "ok", Timestamp: now, Confidence: 1.0},
		},
	}
	baseMsg := message.NewBaseMessage(payload.Schema(), payload, "test-source")
	data, err := json.Marshal(baseMsg)
	require.NoError(t, err)

	// Publish to the ENTITY stream — the real consumer decodes, submits to the
	// keyed pool, and processIngest applies it (no direct handleMessage call).
	require.NoError(t, testClient.Client.PublishToStream(ctx, "entity."+entityID, data))

	// The entity must land in ENTITY_STATES via the consume→pool→ingest wire.
	require.Eventually(t, func() bool {
		stored, _, ferr := c.fetchEntityState(ctx, entityID)
		return ferr == nil && stored != nil
	}, 5*time.Second, 20*time.Millisecond, "published entity must ingest through the keyed pool")

	stored, _, err := c.fetchEntityState(ctx, entityID)
	require.NoError(t, err)
	require.NotNil(t, stored)
	found := false
	for _, tr := range stored.Triples {
		if tr.Predicate == "wire.state.status" && tr.Object == "ok" {
			found = true
		}
	}
	assert.True(t, found, "the published triple must be present after pool ingestion")
}

// TestIntegration_IngestGuard_DurableSurvivesRestart pins B2/B3: after the
// in-memory tier is wiped (a restart, or a high-cardinality LRU eviction), a
// redelivery of an older sequence is still judged stale via the durable tier,
// so it cannot overwrite the newer write through the arrival-order merge.
func TestIntegration_IngestGuard_DurableSurvivesRestart(t *testing.T) {
	ctx, c := startBatchTestComponent(t)
	require.NotNil(t, c.ingestGuardBucket, "Start must provision the durable guard bucket")

	work := ingestWork{entityID: "c360.test.guard.entity.state.001", stream: "ENTITY", seq: 5}

	// Apply-time stamp: durable first, then in-memory (as processIngest does).
	require.NoError(t, c.ingestGuardStampDurable(ctx, work))
	c.ingestGuardMem[0].set(guardKey(work.entityID, work.stream), work.seq)

	// Simulate a restart / full eviction: the in-memory tier for this lane is
	// empty. Only the durable stamp remains.
	c.ingestGuardMem[0] = newLaneGuard(ingestGuardMemMaxPerLane)

	// A post-restart redelivery of an OLDER sequence must be dropped as stale.
	older := ingestWork{entityID: work.entityID, stream: work.stream, seq: 3}
	stale, err := c.ingestGuardStale(ctx, 0, older)
	require.NoError(t, err)
	assert.True(t, stale, "durable tier catches a post-restart older redelivery (B2/B3)")

	// The durable miss-read warmed the in-memory tier back up.
	v, ok := c.ingestGuardMem[0].get(guardKey(work.entityID, work.stream))
	assert.True(t, ok, "a durable read warms the in-memory cache")
	assert.Equal(t, uint64(5), v)

	// A genuinely newer sequence is applied (not stale).
	newer := ingestWork{entityID: work.entityID, stream: work.stream, seq: 6}
	stale, err = c.ingestGuardStale(ctx, 0, newer)
	require.NoError(t, err)
	assert.False(t, stale, "a newer sequence after restart is a fresh update, not stale")
}

// TestIntegration_IngestGuard_DurablePerStreamIndependence pins the round-2 fix
// at the durable level: the guard compares sequences only WITHIN a stream, so a
// valid low-sequence message from a second stream is not silenced by a high
// sequence already applied from another stream.
func TestIntegration_IngestGuard_DurablePerStreamIndependence(t *testing.T) {
	ctx, c := startBatchTestComponent(t)
	entity := "c360.test.guard.entity.002"

	// Stream A durably applied a high sequence.
	require.NoError(t, c.ingestGuardStampDurable(ctx, ingestWork{entityID: entity, stream: "STREAM_A", seq: 1000}))

	// Wipe the in-memory tier so the check must consult the durable tier.
	for i := range c.ingestGuardMem {
		c.ingestGuardMem[i] = newLaneGuard(ingestGuardMemMaxPerLane)
	}

	// A low-sequence message from stream B is a DIFFERENT durable key → not stale.
	fromB := ingestWork{entityID: entity, stream: "STREAM_B", seq: 5}
	stale, err := c.ingestGuardStale(ctx, 0, fromB)
	require.NoError(t, err)
	assert.False(t, stale, "stream B's low seq is not silenced by stream A's durable high seq")
}

// startKeyedWireComponent stands up a graph-ingest component with a running
// consumer on entity.> and the merge-test decoder registered BEFORE Start (so
// the consumer goroutine never races c.decoder). Returns the component + the
// test client for publishing to the wire.
func startKeyedWireComponent(t *testing.T) (context.Context, *Component, *natsclient.TestClient) {
	t.Helper()
	ctx := context.Background()
	streams := []natsclient.TestStreamConfig{
		{Name: "ENTITY", Subjects: []string{"entity.>"}},
	}
	testClient := natsclient.NewTestClient(t, natsclient.WithKV(), natsclient.WithStreams(streams...))

	cfg := DefaultConfig() // IngestLanes = 8 (concurrent by default)
	cfgJSON, err := json.Marshal(cfg)
	require.NoError(t, err)

	comp, err := CreateGraphIngest(cfgJSON, testDependencies(t, testClient.Client, withAuthority("c360", "test")))
	require.NoError(t, err)
	c := comp.(*Component)
	require.NoError(t, c.Initialize())
	registerMergeTestPayload(t, c) // decoder BEFORE Start (no consumer race)
	require.NoError(t, c.Start(ctx))
	t.Cleanup(func() { _ = c.Stop(context.Background()) })
	return ctx, c, testClient
}

// TestIntegration_KeyedIngest_SameEntityUpdatesStayOrdered drives many rapid
// updates for ONE entity through the assembled wire and asserts the final state
// reflects the LAST update — i.e. same-entity messages serialize (one lane, in
// arrival order) rather than reorder. Each publish gets a strictly higher stream
// sequence, so none is a stale redelivery; the guard never drops one, and keyed
// ordering guarantees the last-submitted write wins the single-valued predicate.
// Without keying, concurrent out-of-order application of the arrival-order merge
// could leave an older value as the winner.
func TestIntegration_KeyedIngest_SameEntityUpdatesStayOrdered(t *testing.T) {
	ctx, c, testClient := startKeyedWireComponent(t)

	const entityID = "c360.test.wire.order.entity.001"
	const updates = 25
	now := time.Now()
	for i := 1; i <= updates; i++ {
		payload := &mergeTestGraphable{
			entityID: entityID,
			triples: []message.Triple{
				{Subject: entityID, Predicate: "order.sequence.value", Object: i, Timestamp: now, Confidence: 1.0},
			},
		}
		baseMsg := message.NewBaseMessage(payload.Schema(), payload, "test-source")
		data, err := json.Marshal(baseMsg)
		require.NoError(t, err)
		require.NoError(t, testClient.Client.PublishToStream(ctx, "entity."+entityID, data))
	}

	// The single-valued order.sequence.value predicate must converge to the LAST update.
	orderSeq := func() (float64, bool) {
		stored, _, err := c.fetchEntityState(ctx, entityID)
		if err != nil || stored == nil {
			return 0, false
		}
		for _, tr := range stored.Triples {
			if tr.Predicate == "order.sequence.value" {
				if f, ok := tr.Object.(float64); ok { // JSON round-trips numbers as float64
					return f, true
				}
			}
		}
		return 0, false
	}
	require.Eventually(t, func() bool {
		v, ok := orderSeq()
		return ok && v == float64(updates)
	}, 5*time.Second, 20*time.Millisecond, "same-entity updates must apply in order; final must be the last write")

	// Belt-and-suspenders: it settled on exactly the last value, not an earlier one.
	v, ok := orderSeq()
	require.True(t, ok)
	assert.Equal(t, float64(updates), v, "final order.sequence.value must be the last-submitted update (no reorder)")
}
