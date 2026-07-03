//go:build integration

package graphindex

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

// TestIntegration_KVWatchToIndexFlow tests the full KV watch -> index update flow
func TestIntegration_KVWatchToIndexFlow(t *testing.T) {
	// Create test NATS client with KV support
	// Each test gets its own NATS container, so bucket isolation is automatic
	testClient := natsclient.NewTestClient(t, natsclient.WithKV())
	nc := testClient.Client

	// Create component with default config
	config := DefaultConfig()
	configJSON, err := json.Marshal(config)
	require.NoError(t, err)

	deps := component.Dependencies{
		NATSClient: nc,
	}

	comp, err := CreateGraphIndex(configJSON, deps)
	require.NoError(t, err)
	require.NotNil(t, comp)

	graphIndex := comp.(*Component)

	// Initialize component
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	require.NoError(t, graphIndex.Initialize())

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
	require.NoError(t, graphIndex.Start(ctx))
	defer graphIndex.Stop(5 * time.Second)

	// Create test entity with relationships
	entityID := "c360.platform.robotics.mav1.drone.001"
	targetID := "c360.platform.robotics.mav1.mission.001"
	alias := "drone-alpha"

	state := graph.EntityState{
		ID: entityID,
		Triples: []message.Triple{
			{
				Subject:   entityID,
				Predicate: "robotics.assigned.mission",
				Object:    targetID, // Relationship to another entity
				Source:    "test",
				Timestamp: time.Now(),
			},
			{
				Subject:   entityID,
				Predicate: "robotics.status.armed",
				Object:    true, // Literal value
				Source:    "test",
				Timestamp: time.Now(),
			},
			{
				Subject:   entityID,
				Predicate: "core.identity.alias",
				Object:    alias,
				Source:    "test",
				Timestamp: time.Now(),
			},
		},
		MessageType: message.Type{Domain: "test", Category: "entity", Version: "v1"},
		Version:     1,
	}

	// Write entity to ENTITY_STATES bucket
	stateData, err := json.Marshal(state)
	require.NoError(t, err)

	_, err = entityBucket.Put(ctx, entityID, stateData)
	require.NoError(t, err)

	// Wait for component to process the update
	time.Sleep(500 * time.Millisecond)

	// Verify outgoing index was created (array format: [{to_entity_id, predicate}])
	outgoingEntry, err := graphIndex.outgoingBucket.Get(ctx, entityID)
	require.NoError(t, err)
	assert.NotNil(t, outgoingEntry)

	var outgoingData []map[string]interface{}
	err = json.Unmarshal(outgoingEntry.Value, &outgoingData)
	require.NoError(t, err)
	require.Len(t, outgoingData, 1, "should have one relationship")

	assert.Equal(t, targetID, outgoingData[0]["to_entity_id"])
	assert.Equal(t, "robotics.assigned.mission", outgoingData[0]["predicate"])

	// Verify incoming index was created (array format: [{from_entity_id, predicate}])
	incomingEntry, err := graphIndex.incomingBucket.Get(ctx, targetID)
	require.NoError(t, err)
	assert.NotNil(t, incomingEntry)

	var incomingData []map[string]interface{}
	err = json.Unmarshal(incomingEntry.Value, &incomingData)
	require.NoError(t, err)
	require.Len(t, incomingData, 1, "should have one incoming relationship")

	assert.Equal(t, entityID, incomingData[0]["from_entity_id"])
	assert.Equal(t, "robotics.assigned.mission", incomingData[0]["predicate"])

	// Verify alias index was created
	aliasEntry, err := graphIndex.aliasBucket.Get(ctx, alias)
	require.NoError(t, err)
	assert.NotNil(t, aliasEntry)
	assert.Equal(t, entityID, string(aliasEntry.Value))

	// Verify predicate indexes were created: one composite key per
	// (predicate, entity) pair, hash(predicate)+"."+entityID (ADR-065) —
	// not a blob keyed on the raw predicate string.
	predicates := []string{"robotics.assigned.mission", "robotics.status.armed", "core.identity.alias"}
	for _, predicate := range predicates {
		_, err := graphIndex.predicateBucket.Get(ctx, predicateIndexKey(predicate, entityID))
		require.NoError(t, err, "predicate index should have a composite-key entry for %s", predicate)

		_, err = graphIndex.predicateCatalogBucket.Get(ctx, predicate)
		require.NoError(t, err, "predicate catalog should record %s", predicate)
	}
}

// TestIntegration_EntityDeletion tests that entity deletion removes from all indexes
func TestIntegration_EntityDeletion(t *testing.T) {
	// Create test NATS client with KV support
	// Each test gets its own NATS container, so bucket isolation is automatic
	testClient := natsclient.NewTestClient(t, natsclient.WithKV())
	nc := testClient.Client

	// Create component
	config := DefaultConfig()
	configJSON, err := json.Marshal(config)
	require.NoError(t, err)

	deps := component.Dependencies{
		NATSClient: nc,
	}

	comp, err := CreateGraphIndex(configJSON, deps)
	require.NoError(t, err)

	graphIndex := comp.(*Component)

	// Initialize component
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	require.NoError(t, graphIndex.Initialize())

	// Get JetStream context
	js, err := nc.JetStream()
	require.NoError(t, err)

	// Create ENTITY_STATES bucket BEFORE starting component
	entityBucket, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket:      graph.BucketEntityStates,
		Description: "Test entity states",
	})
	require.NoError(t, err)

	// Start component (now that input bucket exists)
	require.NoError(t, graphIndex.Start(ctx))
	defer graphIndex.Stop(5 * time.Second)

	// Create test entity
	entityID := "c360.platform.robotics.mav1.drone.002"
	targetID := "c360.platform.robotics.mav1.mission.002"

	state := graph.EntityState{
		ID: entityID,
		Triples: []message.Triple{
			{
				Subject:   entityID,
				Predicate: "robotics.assigned.mission",
				Object:    targetID,
				Source:    "test",
				Timestamp: time.Now(),
			},
		},
		MessageType: message.Type{Domain: "test", Category: "entity", Version: "v1"},
		Version:     1,
	}

	// Write entity
	stateData, err := json.Marshal(state)
	require.NoError(t, err)

	_, err = entityBucket.Put(ctx, entityID, stateData)
	require.NoError(t, err)

	// Wait for indexing
	time.Sleep(500 * time.Millisecond)

	// Verify indexes exist
	_, err = graphIndex.outgoingBucket.Get(ctx, entityID)
	require.NoError(t, err, "outgoing index should exist before deletion")

	// Delete entity from ENTITY_STATES
	err = entityBucket.Delete(ctx, entityID)
	require.NoError(t, err)

	// Wait for deletion to process
	time.Sleep(500 * time.Millisecond)

	// Verify indexes were removed
	_, err = graphIndex.outgoingBucket.Get(ctx, entityID)
	assert.True(t, natsclient.IsKVNotFoundError(err), "outgoing index should be deleted, got: %v", err)

	_, err = graphIndex.incomingBucket.Get(ctx, entityID)
	assert.True(t, natsclient.IsKVNotFoundError(err), "incoming index should be deleted, got: %v", err)
}

// TestIntegration_MultipleRelationships tests indexing entities with multiple relationships
func TestIntegration_MultipleRelationships(t *testing.T) {
	// Create test NATS client with KV support
	// Each test gets its own NATS container, so bucket isolation is automatic
	testClient := natsclient.NewTestClient(t, natsclient.WithKV())
	nc := testClient.Client

	// Create component
	config := DefaultConfig()
	configJSON, err := json.Marshal(config)
	require.NoError(t, err)

	deps := component.Dependencies{
		NATSClient: nc,
	}

	comp, err := CreateGraphIndex(configJSON, deps)
	require.NoError(t, err)

	graphIndex := comp.(*Component)

	// Initialize component
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	require.NoError(t, graphIndex.Initialize())

	// Get JetStream context
	js, err := nc.JetStream()
	require.NoError(t, err)

	// Create ENTITY_STATES bucket BEFORE starting component
	entityBucket, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket:      graph.BucketEntityStates,
		Description: "Test entity states",
	})
	require.NoError(t, err)

	// Start component (now that input bucket exists)
	require.NoError(t, graphIndex.Start(ctx))
	defer graphIndex.Stop(5 * time.Second)

	// Create entity with multiple relationships
	entityID := "c360.platform.robotics.mav1.drone.003"
	mission1 := "c360.platform.robotics.mav1.mission.001"
	mission2 := "c360.platform.robotics.mav1.mission.002"
	operator := "c360.platform.robotics.mav1.operator.alice"

	state := graph.EntityState{
		ID: entityID,
		Triples: []message.Triple{
			{
				Subject:   entityID,
				Predicate: "robotics.assigned.mission",
				Object:    mission1,
				Source:    "test",
				Timestamp: time.Now(),
			},
			{
				Subject:   entityID,
				Predicate: "robotics.backup.mission",
				Object:    mission2,
				Source:    "test",
				Timestamp: time.Now(),
			},
			{
				Subject:   entityID,
				Predicate: "robotics.assigned.operator",
				Object:    operator,
				Source:    "test",
				Timestamp: time.Now(),
			},
		},
		MessageType: message.Type{Domain: "test", Category: "entity", Version: "v1"},
		Version:     1,
	}

	// Write entity
	stateData, err := json.Marshal(state)
	require.NoError(t, err)

	_, err = entityBucket.Put(ctx, entityID, stateData)
	require.NoError(t, err)

	// Wait for indexing
	time.Sleep(500 * time.Millisecond)

	// Verify outgoing index has all three relationships (array format)
	outgoingEntry, err := graphIndex.outgoingBucket.Get(ctx, entityID)
	require.NoError(t, err)

	var outgoingData []map[string]interface{}
	err = json.Unmarshal(outgoingEntry.Value, &outgoingData)
	require.NoError(t, err)
	require.Len(t, outgoingData, 3, "should have three relationships")

	// Verify each target exists
	targetIDs := make(map[string]bool)
	for _, entry := range outgoingData {
		targetIDs[entry["to_entity_id"].(string)] = true
	}

	assert.True(t, targetIDs[mission1], "should have mission1 relationship")
	assert.True(t, targetIDs[mission2], "should have mission2 relationship")
	assert.True(t, targetIDs[operator], "should have operator relationship")

	// Verify incoming indexes on all targets (array format)
	for _, targetID := range []string{mission1, mission2, operator} {
		incomingEntry, err := graphIndex.incomingBucket.Get(ctx, targetID)
		require.NoError(t, err, "incoming index should exist for %s", targetID)

		var incomingData []map[string]interface{}
		err = json.Unmarshal(incomingEntry.Value, &incomingData)
		require.NoError(t, err)
		require.NotEmpty(t, incomingData, "should have at least one incoming relationship")

		assert.Equal(t, entityID, incomingData[0]["from_entity_id"])
	}
}

// TestIntegration_ConcurrentUpdates tests concurrent entity updates
func TestIntegration_ConcurrentUpdates(t *testing.T) {
	// Create test NATS client with KV support
	// Each test gets its own NATS container, so bucket isolation is automatic
	testClient := natsclient.NewTestClient(t, natsclient.WithKV())
	nc := testClient.Client

	// Create component
	config := DefaultConfig()
	configJSON, err := json.Marshal(config)
	require.NoError(t, err)

	deps := component.Dependencies{
		NATSClient: nc,
	}

	comp, err := CreateGraphIndex(configJSON, deps)
	require.NoError(t, err)

	graphIndex := comp.(*Component)

	// Initialize component
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	require.NoError(t, graphIndex.Initialize())

	// Get JetStream context
	js, err := nc.JetStream()
	require.NoError(t, err)

	// Create ENTITY_STATES bucket BEFORE starting component
	entityBucket, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket:      graph.BucketEntityStates,
		Description: "Test entity states",
	})
	require.NoError(t, err)

	// Start component (now that input bucket exists)
	require.NoError(t, graphIndex.Start(ctx))
	defer graphIndex.Stop(5 * time.Second)

	// Create multiple entities concurrently
	const numEntities = 10
	done := make(chan bool, numEntities)

	for i := 0; i < numEntities; i++ {
		go func(idx int) {
			entityID := "c360.platform.robotics.mav1.drone." + string(rune('0'+idx))
			targetID := "c360.platform.robotics.mav1.mission." + string(rune('0'+idx))

			state := graph.EntityState{
				ID: entityID,
				Triples: []message.Triple{
					{
						Subject:   entityID,
						Predicate: "robotics.assigned.mission",
						Object:    targetID,
						Source:    "test",
						Timestamp: time.Now(),
					},
				},
				MessageType: message.Type{Domain: "test", Category: "entity", Version: "v1"},
				Version:     1,
			}

			stateData, err := json.Marshal(state)
			if err != nil {
				t.Errorf("failed to marshal state: %v", err)
				done <- false
				return
			}

			_, err = entityBucket.Put(ctx, entityID, stateData)
			if err != nil {
				t.Errorf("failed to put entity: %v", err)
				done <- false
				return
			}

			done <- true
		}(i)
	}

	// Wait for all entities to be written
	for i := 0; i < numEntities; i++ {
		success := <-done
		assert.True(t, success, "entity write should succeed")
	}

	// Wait for all indexing to complete
	time.Sleep(1 * time.Second)

	// Verify all entities were indexed
	for i := 0; i < numEntities; i++ {
		entityID := "c360.platform.robotics.mav1.drone." + string(rune('0'+i))

		_, err := graphIndex.outgoingBucket.Get(ctx, entityID)
		assert.NoError(t, err, "outgoing index should exist for entity %d", i)
	}

	// Check component health
	health := graphIndex.Health()
	assert.True(t, health.Healthy, "component should be healthy after concurrent updates")
	assert.Equal(t, 0, health.ErrorCount, "should have no errors")
}

// TestIntegration_HierarchyEdgeIndexing tests that hierarchy edges are properly indexed.
// This is critical: entities with hierarchy triples MUST have those edges indexed
// for community detection to work correctly.
func TestIntegration_HierarchyEdgeIndexing(t *testing.T) {
	// Create test NATS client with KV support
	testClient := natsclient.NewTestClient(t, natsclient.WithKV())
	nc := testClient.Client

	// Create component
	config := DefaultConfig()
	configJSON, err := json.Marshal(config)
	require.NoError(t, err)

	deps := component.Dependencies{
		NATSClient: nc,
	}

	comp, err := CreateGraphIndex(configJSON, deps)
	require.NoError(t, err)

	graphIndex := comp.(*Component)

	// Initialize component
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	require.NoError(t, graphIndex.Initialize())

	// Get JetStream context
	js, err := nc.JetStream()
	require.NoError(t, err)

	// Create ENTITY_STATES bucket BEFORE starting component
	entityBucket, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket:      graph.BucketEntityStates,
		Description: "Test entity states",
	})
	require.NoError(t, err)

	// Start component
	require.NoError(t, graphIndex.Start(ctx))
	defer graphIndex.Stop(5 * time.Second)

	// Create entity with hierarchy edges - mimics what graph-ingest produces
	// Entity ID follows 6-part format: org.platform.domain.system.type.instance
	entityID := "c360.logistics.sensor.document.temperature.sensor-temp-001"

	// Container IDs also follow 6-part format (created by hierarchy inference)
	typeContainer := "c360.logistics.sensor.document.temperature.group"
	systemContainer := "c360.logistics.sensor.document.group.container"
	domainContainer := "c360.logistics.sensor.group.container.level"

	state := graph.EntityState{
		ID: entityID,
		Triples: []message.Triple{
			// Type triple (not a relationship)
			{
				Subject:   entityID,
				Predicate: "entity.type.class",
				Object:    "sensor.temperature",
				Source:    "test",
				Timestamp: time.Now(),
			},
			// Hierarchy edges - these MUST be indexed as relationships
			{
				Subject:   entityID,
				Predicate: "hierarchy.type.member",
				Object:    typeContainer, // 6-part entity ID → IsRelationship() should return true
				Context:   "inference.hierarchy",
				Source:    "hierarchy",
				Timestamp: time.Now(),
			},
			{
				Subject:   entityID,
				Predicate: "hierarchy.system.member",
				Object:    systemContainer,
				Context:   "inference.hierarchy",
				Source:    "hierarchy",
				Timestamp: time.Now(),
			},
			{
				Subject:   entityID,
				Predicate: "hierarchy.domain.member",
				Object:    domainContainer,
				Context:   "inference.hierarchy",
				Source:    "hierarchy",
				Timestamp: time.Now(),
			},
		},
		MessageType: message.Type{Domain: "logistics", Category: "sensor", Version: "v1"},
		Version:     1,
	}

	// Write entity to ENTITY_STATES bucket
	stateData, err := json.Marshal(state)
	require.NoError(t, err)

	_, err = entityBucket.Put(ctx, entityID, stateData)
	require.NoError(t, err)

	// Wait for component to process the update
	time.Sleep(500 * time.Millisecond)

	// CRITICAL TEST: Verify outgoing index contains ALL hierarchy edges
	outgoingEntry, err := graphIndex.outgoingBucket.Get(ctx, entityID)
	require.NoError(t, err, "outgoing index should exist for entity")
	assert.NotNil(t, outgoingEntry)

	var outgoingData []map[string]interface{}
	err = json.Unmarshal(outgoingEntry.Value, &outgoingData)
	require.NoError(t, err)

	// Should have exactly 3 hierarchy relationships
	require.Len(t, outgoingData, 3, "entity should have 3 hierarchy relationships indexed")

	// Verify each container is in outgoing index
	targetIDs := make(map[string]string) // target → predicate
	for _, entry := range outgoingData {
		targetIDs[entry["to_entity_id"].(string)] = entry["predicate"].(string)
	}

	assert.Equal(t, "hierarchy.type.member", targetIDs[typeContainer],
		"type container should be in outgoing index with hierarchy.type.member predicate")
	assert.Equal(t, "hierarchy.system.member", targetIDs[systemContainer],
		"system container should be in outgoing index with hierarchy.system.member predicate")
	assert.Equal(t, "hierarchy.domain.member", targetIDs[domainContainer],
		"domain container should be in outgoing index with hierarchy.domain.member predicate")

	// CRITICAL TEST: Verify incoming indexes on containers
	// This is how community detection finds connections
	for _, containerID := range []string{typeContainer, systemContainer, domainContainer} {
		incomingEntry, err := graphIndex.incomingBucket.Get(ctx, containerID)
		require.NoError(t, err, "incoming index should exist for container %s", containerID)

		var incomingData []map[string]interface{}
		err = json.Unmarshal(incomingEntry.Value, &incomingData)
		require.NoError(t, err)
		require.NotEmpty(t, incomingData, "container %s should have incoming edges", containerID)

		// Verify entity is in incoming index
		found := false
		for _, entry := range incomingData {
			if entry["from_entity_id"].(string) == entityID {
				found = true
				break
			}
		}
		assert.True(t, found, "entity should be in incoming index for container %s", containerID)
	}

	// Verify context index tracks hierarchy inference provenance
	contextEntry, err := graphIndex.contextBucket.Get(ctx, "inference.hierarchy")
	require.NoError(t, err, "context index should exist for inference.hierarchy")

	// Context index stores []ContextEntry (entity_id, predicate pairs)
	var contextData []map[string]interface{}
	err = json.Unmarshal(contextEntry.Value, &contextData)
	require.NoError(t, err)

	// Verify entity is in context index
	foundEntity := false
	for _, entry := range contextData {
		if entry["entity_id"].(string) == entityID {
			foundEntity = true
			break
		}
	}
	assert.True(t, foundEntity, "entity should be in inference.hierarchy context")
}
