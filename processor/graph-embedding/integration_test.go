//go:build integration

package graphembedding

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/graph/embedding"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIntegration_EmbeddingFlow tests the full entity → embedding flow
func TestIntegration_EmbeddingFlow(t *testing.T) {
	// Create test NATS client with KV support
	testClient := natsclient.NewTestClient(t, natsclient.WithKV())
	nc := testClient.Client

	// Create component with default config (uses BM25 embedder)
	config := DefaultConfig()
	configJSON, err := json.Marshal(config)
	require.NoError(t, err)

	deps := component.Dependencies{
		NATSClient: nc,
	}

	comp, err := CreateGraphEmbedding(configJSON, deps)
	require.NoError(t, err)
	require.NotNil(t, comp)

	embeddingComp := comp.(*Component)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	require.NoError(t, embeddingComp.Initialize())

	// Get JetStream context
	js, err := nc.JetStream()
	require.NoError(t, err)

	// Create ENTITY_STATES bucket (input) BEFORE starting component
	entityBucket, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket:      graph.BucketEntityStates,
		Description: "Test entity states",
	})
	require.NoError(t, err)

	// Start component (now that input bucket exists)
	require.NoError(t, embeddingComp.Start(ctx))
	defer embeddingComp.Stop(5 * time.Second)

	// Wait for EMBEDDING_INDEX bucket to be created by component
	var embeddingBucket jetstream.KeyValue
	require.Eventually(t, func() bool {
		embeddingBucket, err = js.KeyValue(ctx, graph.BucketEmbeddingIndex)
		return err == nil
	}, 5*time.Second, 100*time.Millisecond, "EMBEDDING_INDEX bucket should be created")

	// Create test entity with text predicates
	now := time.Now().UTC()
	entityID := "c360.platform.robotics.mav1.drone.001"

	state := graph.EntityState{
		ID: entityID,
		Triples: []message.Triple{
			{
				Subject:   entityID,
				Predicate: "dc.terms.title",
				Object:    "Autonomous Reconnaissance Drone",
				Source:    "test",
				Timestamp: now,
			},
			{
				Subject:   entityID,
				Predicate: "schema.description",
				Object:    "Multi-rotor aerial vehicle for automated surveillance missions",
				Source:    "test",
				Timestamp: now,
			},
			{
				Subject:   entityID,
				Predicate: "robotics.status.armed",
				Object:    true,
				Source:    "test",
				Timestamp: now,
			},
		},
		MessageType: message.Type{Domain: "test", Category: "entity", Version: "v1"},
		Version:     1,
		UpdatedAt:   now,
	}

	// Write entity to ENTITY_STATES bucket
	stateData, err := json.Marshal(state)
	require.NoError(t, err)

	_, err = entityBucket.Put(ctx, entityID, stateData)
	require.NoError(t, err)

	// Wait for entity to appear in EMBEDDING_INDEX with status=generated
	var embeddingEntry jetstream.KeyValueEntry
	require.Eventually(t, func() bool {
		embeddingEntry, err = embeddingBucket.Get(ctx, entityID)
		if err != nil {
			return false
		}

		var rec embedding.Record
		if err := json.Unmarshal(embeddingEntry.Value(), &rec); err != nil {
			return false
		}

		return rec.Status == embedding.StatusGenerated
	}, 10*time.Second, 200*time.Millisecond, "Entity should have generated embedding")

	// Verify record structure
	var record embedding.Record
	err = json.Unmarshal(embeddingEntry.Value(), &record)
	require.NoError(t, err)

	assert.Equal(t, entityID, record.EntityID, "entity_id should match")
	assert.Equal(t, embedding.StatusGenerated, record.Status, "status should be generated")
	assert.NotNil(t, record.Vector, "vector should be present")
	assert.Equal(t, 384, len(record.Vector), "vector should have 384 dimensions")
	assert.NotEmpty(t, record.Model, "model should be set")
	assert.Contains(t, record.Model, "bm25", "model should be BM25")
	assert.Equal(t, 384, record.Dimensions, "dimensions field should be 384")
	assert.False(t, record.GeneratedAt.IsZero(), "generated_at should be set")
	assert.NotEmpty(t, record.ContentHash, "content_hash should be set")

	t.Logf("Generated embedding for %s: model=%s, dimensions=%d, content_hash=%s",
		entityID, record.Model, record.Dimensions, record.ContentHash)
}

// TestIntegration_EmbeddingReadiness_DeadlockAvoidance proves the ADR-066 §3
// terminal-outcome watermark end to end: a MIX of text-bearing entities (which reach
// the hop-2 SaveGenerated terminal) and telemetry-only entities (which terminate at
// the hop-1 no-text skip) BOTH advance the readiness watermark, so
// graph.embedding.query.status catches up to Ready. The deadlock the ADR warns about
// — only text-bearing entities embed, so Target=LastSeq is never reached — does NOT
// occur precisely because the no-text terminal completes the watermark too.
func TestIntegration_EmbeddingReadiness_DeadlockAvoidance(t *testing.T) {
	testClient := natsclient.NewTestClient(t, natsclient.WithKV())
	nc := testClient.Client

	config := DefaultConfig()
	configJSON, err := json.Marshal(config)
	require.NoError(t, err)

	comp, err := CreateGraphEmbedding(configJSON, component.Dependencies{NATSClient: nc})
	require.NoError(t, err)
	embeddingComp := comp.(*Component)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	require.NoError(t, embeddingComp.Initialize())

	js, err := nc.JetStream()
	require.NoError(t, err)
	entityBucket, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket: graph.BucketEntityStates, Description: "Test entity states",
	})
	require.NoError(t, err)

	require.NoError(t, embeddingComp.Start(ctx))
	defer embeddingComp.Stop(5 * time.Second)

	conn := nc.GetConnection()
	// Raw Request is fine here — the status handler's only error path is an
	// unreachable scalar-marshal failure. A production consumer that gates on this
	// signal must use RequestClassified (ADR-066 §3) to distinguish a classified
	// error body from a zero-value success status.
	statusNow := func() graph.IndexStatusResponse {
		msg, reqErr := conn.Request("graph.embedding.query.status", []byte(`{}`), 2*time.Second)
		require.NoError(t, reqErr)
		var st graph.IndexStatusResponse
		require.NoError(t, json.Unmarshal(msg.Data, &st))
		return st
	}

	// Empty ENTITY_STATES → not ready, zero target.
	st := statusNow()
	assert.False(t, st.Ready, "empty bucket must not read ready")
	assert.Zero(t, st.TargetRevision)

	now := time.Now().UTC()
	writeEntity := func(id string, triples []message.Triple) {
		state := graph.EntityState{
			ID: id, Triples: triples,
			MessageType: message.Type{Domain: "test", Category: "entity", Version: "v1"},
			Version:     1, UpdatedAt: now,
		}
		data, mErr := json.Marshal(state)
		require.NoError(t, mErr)
		_, pErr := entityBucket.Put(ctx, id, data)
		require.NoError(t, pErr)
	}

	// Text-bearing entities → hop-2 SaveGenerated terminal.
	const textN = 4
	for i := 0; i < textN; i++ {
		id := fmt.Sprintf("c360.p.d.s.drone.%03d", i)
		writeEntity(id, []message.Triple{
			{Subject: id, Predicate: "dc.terms.title", Object: fmt.Sprintf("Drone %d", i), Source: "test", Timestamp: now},
		})
	}
	// Telemetry-only entities (no text predicate) → hop-1 no-text terminal. If these
	// did NOT complete the watermark, Ready would deadlock forever (the ADR's warning).
	const teleN = 3
	for i := 0; i < teleN; i++ {
		id := fmt.Sprintf("c360.p.d.s.sensor.%03d", i)
		writeEntity(id, []message.Triple{
			{Subject: id, Predicate: "robotics.status.armed", Object: true, Source: "test", Timestamp: now},
		})
	}

	// Poll until caught up. If any telemetry-only revision stranded, this never fires.
	var final graph.IndexStatusResponse
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		final = statusNow()
		if final.Ready {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	require.True(t, final.Ready, "embedding readiness must catch up — text AND telemetry-only both terminal")
	assert.Equal(t, graph.IndexStateReady, final.State)
	assert.Zero(t, final.Lag, "caught up means zero lag")
	assert.GreaterOrEqual(t, final.IndexedRevision, uint64(textN+teleN), "indexed reflects every write")
	assert.Equal(t, final.TargetRevision, final.IndexedRevision, "indexed == target when caught up")
}

// TestIntegration_EmbeddingDeduplication verifies deduplication works
func TestIntegration_EmbeddingDeduplication(t *testing.T) {
	testClient := natsclient.NewTestClient(t, natsclient.WithKV())
	nc := testClient.Client

	config := DefaultConfig()
	configJSON, err := json.Marshal(config)
	require.NoError(t, err)

	deps := component.Dependencies{
		NATSClient: nc,
	}

	comp, err := CreateGraphEmbedding(configJSON, deps)
	require.NoError(t, err)

	embeddingComp := comp.(*Component)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	require.NoError(t, embeddingComp.Initialize())

	js, err := nc.JetStream()
	require.NoError(t, err)

	entityBucket, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket:      graph.BucketEntityStates,
		Description: "Test entity states",
	})
	require.NoError(t, err)

	require.NoError(t, embeddingComp.Start(ctx))
	defer embeddingComp.Stop(5 * time.Second)

	var embeddingBucket jetstream.KeyValue
	require.Eventually(t, func() bool {
		embeddingBucket, err = js.KeyValue(ctx, graph.BucketEmbeddingIndex)
		return err == nil
	}, 5*time.Second, 100*time.Millisecond)

	var dedupBucket jetstream.KeyValue
	require.Eventually(t, func() bool {
		dedupBucket, err = js.KeyValue(ctx, graph.BucketEmbeddingDedup)
		return err == nil
	}, 5*time.Second, 100*time.Millisecond)

	// Create two different entities with SAME text content
	now := time.Now().UTC()
	sameText := "Autonomous Navigation System for Robotic Platforms"

	entity1ID := "c360.platform.robotics.mav1.drone.001"
	entity2ID := "c360.platform.robotics.ugv1.rover.002"

	state1 := graph.EntityState{
		ID: entity1ID,
		Triples: []message.Triple{
			{
				Subject:   entity1ID,
				Predicate: "dc.terms.title",
				Object:    sameText,
				Source:    "test",
				Timestamp: now,
			},
		},
		MessageType: message.Type{Domain: "test", Category: "entity", Version: "v1"},
		Version:     1,
		UpdatedAt:   now,
	}

	state2 := graph.EntityState{
		ID: entity2ID,
		Triples: []message.Triple{
			{
				Subject:   entity2ID,
				Predicate: "schema.description",
				Object:    sameText,
				Source:    "test",
				Timestamp: now,
			},
		},
		MessageType: message.Type{Domain: "test", Category: "entity", Version: "v1"},
		Version:     1,
		UpdatedAt:   now,
	}

	// Write first entity
	stateData1, err := json.Marshal(state1)
	require.NoError(t, err)
	_, err = entityBucket.Put(ctx, entity1ID, stateData1)
	require.NoError(t, err)

	// Wait for first embedding to be generated
	var record1 embedding.Record
	require.Eventually(t, func() bool {
		entry, err := embeddingBucket.Get(ctx, entity1ID)
		if err != nil {
			return false
		}
		if err := json.Unmarshal(entry.Value(), &record1); err != nil {
			return false
		}
		return record1.Status == embedding.StatusGenerated
	}, 10*time.Second, 200*time.Millisecond, "First entity should have generated embedding")

	// Small delay to ensure first entity is fully processed
	time.Sleep(500 * time.Millisecond)

	// Write second entity with same text
	stateData2, err := json.Marshal(state2)
	require.NoError(t, err)
	_, err = entityBucket.Put(ctx, entity2ID, stateData2)
	require.NoError(t, err)

	// Wait for second embedding to be generated
	var record2 embedding.Record
	require.Eventually(t, func() bool {
		entry, err := embeddingBucket.Get(ctx, entity2ID)
		if err != nil {
			return false
		}
		if err := json.Unmarshal(entry.Value(), &record2); err != nil {
			return false
		}
		return record2.Status == embedding.StatusGenerated
	}, 10*time.Second, 200*time.Millisecond, "Second entity should have generated embedding")

	// Verify both entities have the same content hash
	assert.Equal(t, record1.ContentHash, record2.ContentHash, "Both entities should have same content hash")

	// Verify deduplication bucket contains the shared embedding
	dedupEntry, err := dedupBucket.Get(ctx, record1.ContentHash)
	require.NoError(t, err, "Dedup record should exist")

	var dedupRecord embedding.DedupRecord
	err = json.Unmarshal(dedupEntry.Value(), &dedupRecord)
	require.NoError(t, err)

	assert.NotNil(t, dedupRecord.Vector, "Dedup record should have vector")
	assert.Equal(t, 384, len(dedupRecord.Vector), "Dedup vector should have 384 dimensions")
	assert.GreaterOrEqual(t, len(dedupRecord.EntityIDs), 1, "Dedup record should track entity IDs")
	assert.False(t, dedupRecord.FirstGenerated.IsZero(), "First generated timestamp should be set")

	t.Logf("Deduplication verified: content_hash=%s, entities=%v",
		record1.ContentHash, dedupRecord.EntityIDs)
}

// TestIntegration_EmbeddingTextExtraction verifies text extraction from triples
func TestIntegration_EmbeddingTextExtraction(t *testing.T) {
	testClient := natsclient.NewTestClient(t, natsclient.WithKV())
	nc := testClient.Client

	config := DefaultConfig()
	configJSON, err := json.Marshal(config)
	require.NoError(t, err)

	deps := component.Dependencies{
		NATSClient: nc,
	}

	comp, err := CreateGraphEmbedding(configJSON, deps)
	require.NoError(t, err)

	embeddingComp := comp.(*Component)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	require.NoError(t, embeddingComp.Initialize())

	js, err := nc.JetStream()
	require.NoError(t, err)

	entityBucket, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket:      graph.BucketEntityStates,
		Description: "Test entity states",
	})
	require.NoError(t, err)

	require.NoError(t, embeddingComp.Start(ctx))
	defer embeddingComp.Stop(5 * time.Second)

	var embeddingBucket jetstream.KeyValue
	require.Eventually(t, func() bool {
		embeddingBucket, err = js.KeyValue(ctx, graph.BucketEmbeddingIndex)
		return err == nil
	}, 5*time.Second, 100*time.Millisecond)

	now := time.Now().UTC()

	// Test 1: Entity WITH text predicates - should be queued for embedding
	entityWithTextID := "c360.platform.robotics.mav1.drone.001"
	stateWithText := graph.EntityState{
		ID: entityWithTextID,
		Triples: []message.Triple{
			{
				Subject:   entityWithTextID,
				Predicate: "dc.terms.title",
				Object:    "Test Drone",
				Source:    "test",
				Timestamp: now,
			},
			{
				Subject:   entityWithTextID,
				Predicate: "schema.content",
				Object:    "This is test content",
				Source:    "test",
				Timestamp: now,
			},
			{
				Subject:   entityWithTextID,
				Predicate: "robotics.status.armed",
				Object:    true,
				Source:    "test",
				Timestamp: now,
			},
		},
		MessageType: message.Type{Domain: "test", Category: "entity", Version: "v1"},
		Version:     1,
		UpdatedAt:   now,
	}

	stateData, err := json.Marshal(stateWithText)
	require.NoError(t, err)
	_, err = entityBucket.Put(ctx, entityWithTextID, stateData)
	require.NoError(t, err)

	// Wait for embedding to be generated
	require.Eventually(t, func() bool {
		entry, err := embeddingBucket.Get(ctx, entityWithTextID)
		if err != nil {
			return false
		}
		var rec embedding.Record
		if err := json.Unmarshal(entry.Value(), &rec); err != nil {
			return false
		}
		return rec.Status == embedding.StatusGenerated
	}, 10*time.Second, 200*time.Millisecond, "Entity with text should have generated embedding")

	// Test 2: Entity WITHOUT text predicates - should NOT be queued
	entityWithoutTextID := "c360.platform.robotics.mav1.drone.002"
	stateWithoutText := graph.EntityState{
		ID: entityWithoutTextID,
		Triples: []message.Triple{
			{
				Subject:   entityWithoutTextID,
				Predicate: "robotics.status.armed",
				Object:    false,
				Source:    "test",
				Timestamp: now,
			},
			{
				Subject:   entityWithoutTextID,
				Predicate: "robotics.battery.level",
				Object:    85.5,
				Source:    "test",
				Timestamp: now,
			},
		},
		MessageType: message.Type{Domain: "test", Category: "entity", Version: "v1"},
		Version:     1,
		UpdatedAt:   now,
	}

	stateData2, err := json.Marshal(stateWithoutText)
	require.NoError(t, err)
	_, err = entityBucket.Put(ctx, entityWithoutTextID, stateData2)
	require.NoError(t, err)

	// Wait a bit to ensure processing would have happened
	time.Sleep(2 * time.Second)

	// Verify entity without text predicates is NOT in embedding index
	_, err = embeddingBucket.Get(ctx, entityWithoutTextID)
	assert.Error(t, err, "Entity without text predicates should not have embedding")
	assert.Equal(t, jetstream.ErrKeyNotFound, err, "Should get key not found error")

	t.Logf("Text extraction verified: entity with text=%s (embedded), entity without text=%s (not embedded)",
		entityWithTextID, entityWithoutTextID)
}
