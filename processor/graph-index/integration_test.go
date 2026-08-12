//go:build integration

package graphindex

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// readIncomingEntries reconstructs every incoming edge for a target from the
// sharded INCOMING_INDEX (gh#474): one empty-value key "targetID.sourceID.predicate"
// per edge. Mirrors the production reader handleQueryIncomingNATS — prefix-scan
// "targetID.>" then reconstruct via incomingEntryFromKey. Replaces the pre-sharding
// bare-key Get(targetID) of a JSON list.
func readIncomingEntries(ctx context.Context, t *testing.T, kv *natsclient.KVStore, targetID string) []graph.IncomingEntry {
	t.Helper()
	keys, err := kv.KeysByPrefix(ctx, incomingIndexPrefix(targetID))
	require.NoError(t, err)
	entries := make([]graph.IncomingEntry, 0, len(keys))
	for _, key := range keys {
		e, ok := incomingEntryFromKey(key, targetID)
		require.True(t, ok, "incoming composite key should reconstruct: %s", key)
		entries = append(entries, e)
	}
	return entries
}

// TestIntegration_PreexistingPredicatePoisonIsSticky proves the beta cutover
// contract at the real KV watch boundary. A noncanonical value that exists
// before Start must poison readiness during replay, and a later canonical
// update must not hide that process-lifetime reset requirement.
func TestIntegration_PreexistingPredicatePoisonIsSticky(t *testing.T) {
	testClient := natsclient.NewTestClient(t, natsclient.WithKV())
	nc := testClient.Client

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	js, err := nc.JetStream()
	require.NoError(t, err)
	entityBucket, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket:      graph.BucketEntityStates,
		Description: "Test incompatible entity state replay",
	})
	require.NoError(t, err)

	entityID := "acme.ops.robotics.gcs.drone.001"
	targetID := "acme.ops.robotics.gcs.mission.001"
	poisoned := []byte(`{"id":"acme.ops.robotics.gcs.drone.001","triples":[{"subject":"acme.ops.robotics.gcs.drone.001","predicate":"legacy.predicate","object":"old"}]}`) // predicate-audit:invalid {"kind":"stored-predicate","value":"legacy.predicate","reason":"arity"}
	_, err = entityBucket.Put(ctx, entityID, poisoned)
	require.NoError(t, err)

	configJSON, err := json.Marshal(DefaultConfig())
	require.NoError(t, err)
	created, err := CreateGraphIndex(configJSON, component.Dependencies{NATSClient: nc})
	require.NoError(t, err)
	indexComponent := created.(*Component)
	require.NoError(t, indexComponent.Initialize())
	require.NoError(t, indexComponent.Start(ctx))
	defer indexComponent.Stop(5 * time.Second)

	require.Eventually(t, func() bool {
		status := indexComponent.computeIndexStatus(ctx)
		return !status.Ready &&
			status.State == graph.IndexStateResetRequired &&
			status.Code == graph.ErrorCodeGraphStateResetRequired &&
			status.Reason == string(graph.GraphStateReasonNoncanonicalPredicate)
	}, 5*time.Second, 25*time.Millisecond, "preexisting poison never latched reset-required")

	canonical := graph.EntityState{
		ID: entityID,
		Triples: []message.Triple{{
			Subject:   entityID,
			Predicate: "robotics.assigned.mission",
			Object:    targetID,
			Source:    "test",
			Timestamp: time.Now(),
		}},
	}
	canonicalData, err := graph.MarshalEntityState(&canonical)
	require.NoError(t, err)
	_, err = entityBucket.Put(ctx, entityID, canonicalData)
	require.NoError(t, err)

	// Prove the later revision was actually delivered and processed; merely
	// checking the sticky flag without this synchronization could pass before
	// the update reaches the component.
	require.Eventually(t, func() bool {
		_, getErr := indexComponent.outgoingBucket.Get(ctx, entityID)
		return getErr == nil
	}, 5*time.Second, 25*time.Millisecond, "later canonical update was not indexed")

	status := indexComponent.computeIndexStatus(ctx)
	require.False(t, status.Ready)
	require.Equal(t, graph.IndexStateResetRequired, status.State)
	require.Equal(t, graph.ErrorCodeGraphStateResetRequired, status.Code)
	require.Equal(t, string(graph.GraphStateReasonNoncanonicalPredicate), status.Reason)

	queryErr := indexComponent.ensureQueryReady(ctx)
	var classified *errs.ClassifiedError
	require.ErrorAs(t, queryErr, &classified)
	require.Equal(t, errs.ErrorFatal, classified.Class)
	require.Equal(t, graph.ErrorCodeGraphStateResetRequired, classified.Code)
}

// TestIntegration_PredicateCleanWipeReseedRestoresQueryParity proves typed
// stored-state poison recovery against real NATS. A poisoned process cannot be
// repaired in place; after stop + complete incompatible-bucket reset, canonical
// repopulation restores exact and namespace queries, and a second clean restart
// replays to the same results.
func TestIntegration_PredicateCleanWipeReseedRestoresQueryParity(t *testing.T) {
	testClient := natsclient.NewTestClient(t, natsclient.WithKV())
	nc := testClient.Client
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	js, err := nc.JetStream()
	require.NoError(t, err)
	entityBucket, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket: graph.BucketEntityStates, Description: "Predicate clean-wipe cutover proof",
	})
	require.NoError(t, err)
	entityID := "acme.ops.robotics.gcs.drone.001"
	poisoned := []byte(`{"id":"acme.ops.robotics.gcs.drone.001","triples":[{"subject":"acme.ops.robotics.gcs.drone.001","predicate":"legacy.predicate","object":"old"}]}`) // predicate-audit:invalid {"kind":"stored-predicate","value":"legacy.predicate","reason":"arity"}
	_, err = entityBucket.Put(ctx, entityID, poisoned)
	require.NoError(t, err)

	newIndex := func() *Component {
		t.Helper()
		configJSON, marshalErr := json.Marshal(DefaultConfig())
		require.NoError(t, marshalErr)
		created, createErr := CreateGraphIndex(configJSON, component.Dependencies{NATSClient: nc})
		require.NoError(t, createErr)
		indexComponent := created.(*Component)
		require.NoError(t, indexComponent.Initialize())
		return indexComponent
	}
	waitReady := func(indexComponent *Component) {
		t.Helper()
		require.Eventually(t, func() bool {
			return indexComponent.computeIndexStatus(ctx).Ready
		}, 5*time.Second, 25*time.Millisecond, "canonical replay never became query-ready")
	}
	assertPredicateParity := func(indexComponent *Component) {
		t.Helper()
		exactData, queryErr := indexComponent.handleQueryPredicateNATS(
			ctx, []byte(`{"predicate":"robotics.assigned.mission"}`))
		require.NoError(t, queryErr)
		var exact graph.PredicateQueryResponse
		require.NoError(t, json.Unmarshal(exactData, &exact))
		require.Equal(t, []string{entityID}, exact.Data.Entities)

		namespaceRequest, marshalErr := json.Marshal(graph.PredicateListQuery{Namespace: "robotics.assigned"})
		require.NoError(t, marshalErr)
		namespaceData, queryErr := indexComponent.handleQueryPredicateListNATS(ctx, namespaceRequest)
		require.NoError(t, queryErr)
		var namespace graph.PredicateListQueryResponse
		require.NoError(t, json.Unmarshal(namespaceData, &namespace))
		require.Equal(t, []graph.PredicateSummary{
			{Predicate: "robotics.assigned.mission", EntityCount: 1},
			{Predicate: "robotics.assigned.team", EntityCount: 1},
		}, namespace.Data.Predicates)
	}

	poisonedIndex := newIndex()
	require.NoError(t, poisonedIndex.Start(ctx))
	require.Eventually(t, func() bool {
		status := poisonedIndex.computeIndexStatus(ctx)
		return !status.Ready && status.State == graph.IndexStateResetRequired &&
			status.Reason == string(graph.GraphStateReasonNoncanonicalPredicate)
	}, 5*time.Second, 25*time.Millisecond, "poisoned replay never latched reset-required")
	require.NoError(t, poisonedIndex.Stop(5*time.Second))

	// This is the graph-index-owned subset of the operator runbook's complete
	// resource set. Every bucket exists because Start created it before replay.
	for _, bucket := range []string{
		graph.BucketEntityStates,
		graph.BucketOutgoingIndex,
		graph.BucketIncomingIndex,
		graph.BucketAliasIndex,
		graph.BucketPredicateIndex,
		graph.BucketNameIndex,
	} {
		require.NoError(t, js.DeleteKeyValue(ctx, bucket), "wipe bucket %s", bucket)
	}

	entityBucket, err = js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket: graph.BucketEntityStates, Description: "Canonical predicate reseed",
	})
	require.NoError(t, err)
	canonical := graph.EntityState{
		ID: entityID,
		Triples: []message.Triple{
			{Subject: entityID, Predicate: "robotics.assigned.mission", Object: "acme.ops.robotics.gcs.mission.001", Source: "test", Timestamp: time.Now()},
			{Subject: entityID, Predicate: "robotics.assigned.team", Object: "alpha", Source: "test", Timestamp: time.Now()},
		},
	}
	canonicalData, err := graph.MarshalEntityState(&canonical)
	require.NoError(t, err)
	_, err = entityBucket.Put(ctx, entityID, canonicalData)
	require.NoError(t, err)

	reseededIndex := newIndex()
	require.NoError(t, reseededIndex.Start(ctx))
	waitReady(reseededIndex)
	assertPredicateParity(reseededIndex)
	require.NoError(t, reseededIndex.Stop(5*time.Second))

	replayedIndex := newIndex()
	require.NoError(t, replayedIndex.Start(ctx))
	waitReady(replayedIndex)
	assertPredicateParity(replayedIndex)
	require.NoError(t, replayedIndex.Stop(5*time.Second))
}

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

	// Verify incoming index was created (composite-key format after gh#474:
	// one empty-value key "targetID.sourceID.predicate" per edge).
	incomingEntries := readIncomingEntries(ctx, t, graphIndex.incomingBucket, targetID)
	require.Len(t, incomingEntries, 1, "should have one incoming relationship")
	assert.Equal(t, entityID, incomingEntries[0].FromEntityID)
	assert.Equal(t, "robotics.assigned.mission", incomingEntries[0].Predicate)

	// Verify alias index was created
	aliasEntry, err := graphIndex.aliasBucket.Get(ctx, alias)
	require.NoError(t, err)
	assert.NotNil(t, aliasEntry)
	assert.Equal(t, entityID, string(aliasEntry.Value))

	// Verify predicate indexes were created: one composite key per
	// (predicate, entity) pair in the self-describing predicate3.entity6 layout.
	predicates := []string{"robotics.assigned.mission", "robotics.status.armed", "core.identity.alias"}
	for _, predicate := range predicates {
		_, err := graphIndex.predicateBucket.Get(ctx, predicateIndexKey(predicate, entityID))
		require.NoError(t, err, "predicate index should have a composite-key entry for %s", predicate)
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

	// Synchronize on the concrete projection rather than a fixed sleep.
	require.Eventually(t, func() bool {
		_, getErr := graphIndex.outgoingBucket.Get(ctx, entityID)
		return getErr == nil
	}, 3*time.Second, 25*time.Millisecond, "outgoing index should exist before deletion")

	// Seed a populated INCOMING row that is physically target-prefixed by entityID
	// but semantically owned by another live source. Target retirement must preserve
	// this assertion while retracting entityID's own assertion against targetID.
	otherSourceID := "c360.platform.robotics.mav1.sensor.002"
	require.NoError(t, graphIndex.UpdateIncomingIndex(ctx, entityID, otherSourceID, "core.relationship.related"))
	require.Eventually(t, func() bool {
		return len(readIncomingEntries(ctx, t, graphIndex.incomingBucket, entityID)) == 1
	}, 3*time.Second, 25*time.Millisecond, "incoming-as-target row should exist before deletion")

	// Delete entity from ENTITY_STATES
	err = entityBucket.Delete(ctx, entityID)
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		_, outgoingErr := graphIndex.outgoingBucket.Get(ctx, entityID)
		return natsclient.IsKVNotFoundError(outgoingErr) &&
			len(readIncomingEntries(ctx, t, graphIndex.incomingBucket, targetID)) == 0 &&
			len(readIncomingEntries(ctx, t, graphIndex.incomingBucket, entityID)) == 1
	}, 3*time.Second, 25*time.Millisecond, "source-owned rows were not retracted cleanly")

	// Verify indexes were removed
	_, err = graphIndex.outgoingBucket.Get(ctx, entityID)
	assert.True(t, natsclient.IsKVNotFoundError(err), "outgoing index should be deleted, got: %v", err)

	// The target-prefixed row belongs to the still-live source and must survive.
	targetPrefixedIncoming := readIncomingEntries(ctx, t, graphIndex.incomingBucket, entityID)
	assert.Equal(t, []graph.IncomingEntry{{
		FromEntityID: otherSourceID, Predicate: "core.relationship.related",
	}}, targetPrefixedIncoming)
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

	// Verify incoming indexes on all targets (composite-key format after gh#474).
	for _, targetID := range []string{mission1, mission2, operator} {
		incomingEntries := readIncomingEntries(ctx, t, graphIndex.incomingBucket, targetID)
		require.NotEmpty(t, incomingEntries, "incoming index should exist for %s", targetID)
		assert.Equal(t, entityID, incomingEntries[0].FromEntityID)
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

	// CRITICAL TEST: Verify incoming indexes on containers (composite-key format
	// after gh#474). This is how community detection finds connections.
	for _, containerID := range []string{typeContainer, systemContainer, domainContainer} {
		incomingEntries := readIncomingEntries(ctx, t, graphIndex.incomingBucket, containerID)
		require.NotEmpty(t, incomingEntries, "container %s should have incoming edges", containerID)

		// Verify entity is in incoming index
		found := false
		for _, entry := range incomingEntries {
			if entry.FromEntityID == entityID {
				found = true
				break
			}
		}
		assert.True(t, found, "entity should be in incoming index for container %s", containerID)
	}

	// Provenance remains on the authoritative triples; graph-index must not strip it
	// while deriving the consumed relationship views.
	authorityEntry, err := entityBucket.Get(ctx, entityID)
	require.NoError(t, err)
	var authoritative graph.EntityState
	require.NoError(t, json.Unmarshal(authorityEntry.Value(), &authoritative))
	hierarchyTriples := 0
	for _, triple := range authoritative.Triples {
		if strings.HasPrefix(triple.Predicate, "hierarchy.") {
			hierarchyTriples++
			assert.Equal(t, "inference.hierarchy", triple.Context)
		}
	}
	assert.Equal(t, 3, hierarchyTriples, "all authoritative hierarchy triples retain provenance")
}
