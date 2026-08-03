//go:build integration

package graphindex

import (
	"context"
	"encoding/json"
	"runtime"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/internal/semantictest"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIntegration_Attack_NoGoroutineLeakOnStart(t *testing.T) {
	// Skip if not in integration mode (needs real NATS)
	if testing.Short() {
		t.Skip("requires NATS - run with integration tests")
	}
	// Skip with race detector - triggers race in nats.go v1.47.0 WatchFiltered()
	// during rapid start/stop cycles. The race is internal to the NATS library.
	if raceEnabled {
		t.Skip("skipping: triggers NATS library internal race in WatchFiltered()")
	}

	before := runtime.NumGoroutine()

	// Create and start/stop component multiple times
	for i := 0; i < 10; i++ {
		testClient := natsclient.NewTestClient(t, natsclient.WithKV())
		nc := testClient.Client

		config := DefaultConfig()
		configJSON, _ := json.Marshal(config)

		deps := component.Dependencies{NATSClient: nc}
		comp, err := CreateGraphIndex(configJSON, deps)
		require.NoError(t, err)

		graphIndex := comp.(*Component)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		require.NoError(t, graphIndex.Initialize())

		// Create input bucket
		js, _ := nc.JetStream()
		_, _ = js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
			Bucket:      graph.BucketEntityStates,
			Description: "Test",
		})

		require.NoError(t, graphIndex.Start(ctx))
		time.Sleep(50 * time.Millisecond)
		require.NoError(t, graphIndex.Stop(1*time.Second))

		cancel()
		// Cleanup NATS client between iterations to prevent goroutine accumulation
		// t.Cleanup() only runs at test end, not between iterations
		testClient.Terminate()
	}

	// Allow goroutines to clean up
	time.Sleep(200 * time.Millisecond)
	runtime.GC()

	after := runtime.NumGoroutine()

	// Allow some tolerance for background goroutines
	assert.LessOrEqual(t, after, before+5,
		"goroutine leak detected: before=%d after=%d (delta=%d)",
		before, after, after-before)
}

func TestIntegration_Attack_ManyTriplesSingleEntity(t *testing.T) {
	// Skip if not in integration mode
	if testing.Short() {
		t.Skip("requires NATS - run with integration tests")
	}

	testClient := natsclient.NewTestClient(t, natsclient.WithKV())
	nc := testClient.Client

	config := DefaultConfig()
	configJSON, _ := json.Marshal(config)
	deps := component.Dependencies{NATSClient: nc}

	comp, err := CreateGraphIndex(configJSON, deps)
	require.NoError(t, err)

	graphIndex := comp.(*Component)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	require.NoError(t, graphIndex.Initialize())

	js, _ := nc.JetStream()
	entityBucket, _ := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket:      graph.BucketEntityStates,
		Description: "Test",
	})

	require.NoError(t, graphIndex.Start(ctx))
	defer graphIndex.Stop(5 * time.Second)

	// Create entity with 1000 relationships
	entityID := "c360.test.stress.entity.massive.001"
	triples := make([]message.Triple, 1000)
	for i := 0; i < 1000; i++ {
		triples[i] = message.Triple{
			Subject:   entityID,
			Predicate: "test.rel.target",
			Object:    "c360.test.stress.entity.target." + string(rune('0'+i%10)),
			Source:    "attack-test",
			Timestamp: time.Now(),
		}
	}

	state := graph.EntityState{
		ID:          entityID,
		Triples:     triples,
		MessageType: message.Type{Domain: "attack", Category: "stress", Version: "v1"},
		Version:     1,
	}

	stateData, _ := json.Marshal(state)
	_, err = entityBucket.Put(ctx, entityID, stateData)
	require.NoError(t, err)

	// Wait for processing
	time.Sleep(2 * time.Second)

	// Component should still be healthy
	health := graphIndex.Health()
	assert.True(t, health.Healthy, "component should survive large input")
	assert.Equal(t, "running", health.Status)
}

func TestIntegration_Attack_MultipleEntitiesSamePredicate(t *testing.T) {
	// This test validates the Concern 3 from code review
	// Skip if not in integration mode
	if testing.Short() {
		t.Skip("requires NATS - run with integration tests")
	}

	testClient := natsclient.NewTestClient(t, natsclient.WithKV())
	nc := testClient.Client

	config := DefaultConfig()
	configJSON, _ := json.Marshal(config)
	deps := component.Dependencies{NATSClient: nc}

	comp, err := CreateGraphIndex(configJSON, deps)
	require.NoError(t, err)

	graphIndex := comp.(*Component)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	require.NoError(t, graphIndex.Initialize())

	js, _ := nc.JetStream()
	entityBucket, _ := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket:      graph.BucketEntityStates,
		Description: "Test",
	})

	require.NoError(t, graphIndex.Start(ctx))
	defer graphIndex.Stop(5 * time.Second)

	// Create 3 entities with the same predicate
	entities := []string{
		"c360.platform.robotics.mav1.drone.001",
		"c360.platform.robotics.mav1.drone.002",
		"c360.platform.robotics.mav1.drone.003",
	}

	for _, entityID := range entities {
		state := graph.EntityState{
			ID: entityID,
			Triples: []message.Triple{
				{
					Subject:   entityID,
					Predicate: semantictest.Predicate(t, "robotics", "status", "armed"),
					Object:    true,
					Source:    "attack-test",
					Timestamp: time.Now(),
				},
			},
			MessageType: message.Type{Domain: "attack", Category: "test", Version: "v1"},
			Version:     1,
		}

		stateData, _ := json.Marshal(state)
		_, err = entityBucket.Put(ctx, entityID, stateData)
		require.NoError(t, err)
		time.Sleep(100 * time.Millisecond) // Stagger writes
	}

	time.Sleep(500 * time.Millisecond)

	// Query predicate index via the production wire (NATS query API, not
	// the raw bucket — see ADR-065). Composite per-(predicate,entity) keys
	// mean there is no longer a "last writer wins" collision: every entity
	// gets its own key, so all 3 must be present, not just one.
	request := map[string]string{"predicate": semantictest.Predicate(t, "robotics", "status", "armed")}
	requestJSON, err := json.Marshal(request)
	require.NoError(t, err)

	respData, err := nc.RequestClassified(ctx, "graph.index.query.predicate", requestJSON, 2*time.Second)
	require.NoError(t, err, "predicate query should succeed")

	var response graph.PredicateQueryResponse
	require.NoError(t, json.Unmarshal(respData, &response))

	assert.ElementsMatch(t, entities, response.Data.Entities,
		"predicate index must contain ALL entities sharing the predicate, not just the last writer")
}

func TestIntegration_Attack_MultipleSourcesSameTarget(t *testing.T) {
	// This test validates Concern 2 from code review
	// Skip if not in integration mode
	if testing.Short() {
		t.Skip("requires NATS - run with integration tests")
	}

	testClient := natsclient.NewTestClient(t, natsclient.WithKV())
	nc := testClient.Client

	config := DefaultConfig()
	configJSON, _ := json.Marshal(config)
	deps := component.Dependencies{NATSClient: nc}

	comp, err := CreateGraphIndex(configJSON, deps)
	require.NoError(t, err)

	graphIndex := comp.(*Component)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	require.NoError(t, graphIndex.Initialize())

	js, _ := nc.JetStream()
	entityBucket, _ := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket:      graph.BucketEntityStates,
		Description: "Test",
	})

	require.NoError(t, graphIndex.Start(ctx))
	defer graphIndex.Stop(5 * time.Second)

	// 3 drones all reference the same mission
	targetID := "c360.platform.robotics.mav1.mission.alpha"
	sources := []string{
		"c360.platform.robotics.mav1.drone.001",
		"c360.platform.robotics.mav1.drone.002",
		"c360.platform.robotics.mav1.drone.003",
	}

	for _, sourceID := range sources {
		state := graph.EntityState{
			ID: sourceID,
			Triples: []message.Triple{
				{
					Subject:   sourceID,
					Predicate: "robotics.assigned.mission",
					Object:    targetID,
					Source:    "attack-test",
					Timestamp: time.Now(),
				},
			},
			MessageType: message.Type{Domain: "attack", Category: "test", Version: "v1"},
			Version:     1,
		}

		stateData, _ := json.Marshal(state)
		_, err = entityBucket.Put(ctx, sourceID, stateData)
		require.NoError(t, err)
		time.Sleep(100 * time.Millisecond) // Stagger writes
	}

	time.Sleep(500 * time.Millisecond)

	// Query incoming index for the target using the composite-key sharded format (gh#474).
	// One key per edge: targetID.sourceID.predicate — scan the target prefix.
	incomingKeys, err := graphIndex.incomingBucket.KeysByPrefix(ctx, incomingIndexPrefix(targetID))
	require.NoError(t, err, "incoming index key scan should succeed")
	require.NotEmpty(t, incomingKeys, "incoming index should have composite-key entries")

	// Reconstruct IncomingEntry for each key; collect distinct source IDs.
	var storedSourceIDs []string
	for _, key := range incomingKeys {
		entry, ok := incomingEntryFromKey(key, targetID)
		if ok {
			storedSourceIDs = append(storedSourceIDs, entry.FromEntityID)
		}
	}

	// ATTACK TEST: Verify multiple sources are properly indexed.
	// With composite-key sharding every edge gets its own key — no CAS
	// contention, all sources must be present.
	assert.Len(t, storedSourceIDs, len(sources),
		"incoming index should contain all sources with composite-key format")
	for _, source := range sources {
		assert.Contains(t, storedSourceIDs, source,
			"incoming index should contain source %s", source)
	}
}
