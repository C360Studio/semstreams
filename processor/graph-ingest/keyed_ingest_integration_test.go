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

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIntegration_KeyedIngest_PublishedEntityIngestsThroughPool drives the
// ASSEMBLED wire end-to-end: a message published to the ENTITY stream is picked
// up by the real graph-ingest consumer, decoded once, submitted to the keyed
// pool, applied by processIngest, and acked — landing in ENTITY_STATES. None of
// the other tests exercise the consume→pool→ingest composition; this pins that
// the pool wiring actually processes published messages (the happy path).
func TestIntegration_KeyedIngest_PublishedEntityIngestsThroughPool(t *testing.T) {
	ctx := context.Background()
	streams := []natsclient.TestStreamConfig{
		{Name: "ENTITY", Subjects: []string{"entity.>"}},
	}
	testClient := natsclient.NewTestClient(t, natsclient.WithKV(), natsclient.WithStreams(streams...))

	cfg := DefaultConfig() // IngestLanes = 8 (concurrent by default)
	cfgJSON, err := json.Marshal(cfg)
	require.NoError(t, err)

	comp, err := CreateGraphIngest(cfgJSON, component.Dependencies{NATSClient: testClient.Client})
	require.NoError(t, err)
	c := comp.(*Component)
	require.NoError(t, c.Initialize())
	// Register the test decoder BEFORE Start — the consumer reads c.decoder on
	// each message, so swapping it after Start would race the consumer goroutine.
	registerMergeTestPayload(t, c)
	require.NoError(t, c.Start(ctx))
	t.Cleanup(func() { _ = c.Stop(5 * time.Second) })

	const entityID = "c360.test.wire.keyed.entity.001"
	now := time.Now()
	payload := &mergeTestGraphable{
		entityID: entityID,
		triples: []message.Triple{
			{Subject: entityID, Predicate: "wire.status", Object: "ok", Timestamp: now, Confidence: 1.0},
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
		if tr.Predicate == "wire.status" && tr.Object == "ok" {
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

	work := ingestWork{entityID: "c360.test.guard.entity.001", stream: "ENTITY", seq: 5}

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
