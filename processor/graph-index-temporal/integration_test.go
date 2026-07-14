//go:build integration

package graphindextemporal

import (
	"context"
	"encoding/json"
	"fmt"
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

// TestIntegration_TemporalIndexFlow tests the full entity -> temporal index flow
// Verifies that the component produces data in the format expected by QueryTemporal
func TestIntegration_TemporalIndexFlow(t *testing.T) {
	// Create test NATS client with KV support
	testClient := natsclient.NewTestClient(t, natsclient.WithKV())
	nc := testClient.Client

	// Create component with default config
	config := DefaultConfig()
	configJSON, err := json.Marshal(config)
	require.NoError(t, err)

	deps := component.Dependencies{
		NATSClient: nc,
	}

	comp, err := CreateGraphIndexTemporal(configJSON, deps)
	require.NoError(t, err)
	require.NotNil(t, comp)

	temporalIndex := comp.(*Component)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	require.NoError(t, temporalIndex.Initialize())

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
	require.NoError(t, temporalIndex.Start(ctx))
	defer temporalIndex.Stop(5 * time.Second)

	// Wait for TEMPORAL_INDEX bucket to be created by component
	var temporalBucket jetstream.KeyValue
	require.Eventually(t, func() bool {
		temporalBucket, err = js.KeyValue(ctx, graph.BucketTemporalIndex)
		return err == nil
	}, 5*time.Second, 100*time.Millisecond, "TEMPORAL_INDEX bucket should be created")

	// Create test entity with UpdatedAt set (this is what temporal index uses)
	now := time.Now().UTC()
	entityID := "c360.platform.robotics.mav1.drone.001"

	state := graph.EntityState{
		ID: entityID,
		Triples: []message.Triple{
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
		UpdatedAt:   now, // This is the timestamp used for temporal indexing
	}

	// Write entity to ENTITY_STATES bucket
	stateData, err := json.Marshal(state)
	require.NoError(t, err)

	_, err = entityBucket.Put(ctx, entityID, stateData)
	require.NoError(t, err)

	// Wait for entity to be indexed
	expectedTimeKey := fmt.Sprintf("%04d.%02d.%02d.%02d",
		now.Year(), now.Month(), now.Day(), now.Hour())

	var indexEntry jetstream.KeyValueEntry
	require.Eventually(t, func() bool {
		indexEntry, err = temporalBucket.Get(ctx, expectedTimeKey)
		return err == nil
	}, 5*time.Second, 100*time.Millisecond, "Entity should be indexed in temporal bucket")

	// Verify data format matches QueryTemporal expectations
	var temporalData map[string]interface{}
	err = json.Unmarshal(indexEntry.Value(), &temporalData)
	require.NoError(t, err)

	// Check "events" array exists (not "entities" map)
	events, ok := temporalData["events"].([]interface{})
	require.True(t, ok, "temporalData should have 'events' array, got: %+v", temporalData)
	require.GreaterOrEqual(t, len(events), 1, "should have at least 1 event")

	// Verify event structure matches QueryTemporal parser (manager.go:1538-1542)
	event := events[0].(map[string]interface{})
	assert.Equal(t, entityID, event["entity"], "event should have 'entity' field")
	assert.Equal(t, "update", event["type"], "event should have 'type' field")
	assert.NotEmpty(t, event["timestamp"], "event should have 'timestamp' field")

	// Verify entity_count exists
	entityCount, ok := temporalData["entity_count"].(float64)
	require.True(t, ok, "temporalData should have 'entity_count'")
	assert.Equal(t, float64(1), entityCount, "entity_count should be 1")

	t.Logf("Temporal index entry for %s: %s", expectedTimeKey, string(indexEntry.Value()))
}

// TestIntegration_TemporalIndexKeyFormat verifies the time bucket key format
func TestIntegration_TemporalIndexKeyFormat(t *testing.T) {
	testClient := natsclient.NewTestClient(t, natsclient.WithKV())
	nc := testClient.Client

	config := DefaultConfig()
	configJSON, err := json.Marshal(config)
	require.NoError(t, err)

	deps := component.Dependencies{
		NATSClient: nc,
	}

	comp, err := CreateGraphIndexTemporal(configJSON, deps)
	require.NoError(t, err)

	temporalIndex := comp.(*Component)

	// Test various timestamps
	tests := []struct {
		name       string
		timestamp  time.Time
		resolution string
		wantKey    string
	}{
		{
			name:       "hour resolution",
			timestamp:  time.Date(2026, 1, 7, 14, 30, 45, 0, time.UTC),
			resolution: "hour",
			wantKey:    "2026.01.07.14",
		},
		{
			name:       "minute resolution",
			timestamp:  time.Date(2026, 1, 7, 14, 30, 45, 0, time.UTC),
			resolution: "minute",
			wantKey:    "2026.01.07.14.30",
		},
		{
			name:       "day resolution",
			timestamp:  time.Date(2026, 1, 7, 14, 30, 45, 0, time.UTC),
			resolution: "day",
			wantKey:    "2026.01.07",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			temporalIndex.config.TimeResolution = tt.resolution
			got := temporalIndex.calculateTimeBucket(tt.timestamp)
			assert.Equal(t, tt.wantKey, got, "time bucket key should use dot-separated format")
		})
	}
}

// TestIntegration_TemporalIndexAccumulation verifies events accumulate (not overwrite)
func TestIntegration_TemporalIndexAccumulation(t *testing.T) {
	testClient := natsclient.NewTestClient(t, natsclient.WithKV())
	nc := testClient.Client

	config := DefaultConfig()
	configJSON, err := json.Marshal(config)
	require.NoError(t, err)

	deps := component.Dependencies{
		NATSClient: nc,
	}

	comp, err := CreateGraphIndexTemporal(configJSON, deps)
	require.NoError(t, err)

	temporalIndex := comp.(*Component)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	require.NoError(t, temporalIndex.Initialize())

	js, err := nc.JetStream()
	require.NoError(t, err)

	entityBucket, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket:      graph.BucketEntityStates,
		Description: "Test entity states",
	})
	require.NoError(t, err)

	require.NoError(t, temporalIndex.Start(ctx))
	defer temporalIndex.Stop(5 * time.Second)

	var temporalBucket jetstream.KeyValue
	require.Eventually(t, func() bool {
		temporalBucket, err = js.KeyValue(ctx, graph.BucketTemporalIndex)
		return err == nil
	}, 5*time.Second, 100*time.Millisecond)

	// Write 3 different entities with same hour timestamp
	now := time.Now().UTC().Truncate(time.Hour)
	entityIDs := []string{
		"c360.platform.robotics.mav1.drone.001",
		"c360.platform.robotics.mav1.drone.002",
		"c360.platform.robotics.mav1.drone.003",
	}

	for i, entityID := range entityIDs {
		state := graph.EntityState{
			ID: entityID,
			Triples: []message.Triple{
				{Subject: entityID, Predicate: "test.temporal.value", Object: i, Source: "test", Timestamp: now},
			},
			MessageType: message.Type{Domain: "test", Category: "entity", Version: "v1"},
			Version:     1,
			UpdatedAt:   now.Add(time.Duration(i) * time.Minute), // Slightly different times within same hour
		}
		stateData, err := json.Marshal(state)
		require.NoError(t, err)

		_, err = entityBucket.Put(ctx, entityID, stateData)
		require.NoError(t, err)

		// Small delay to ensure ordering
		time.Sleep(100 * time.Millisecond)
	}

	// Wait for all entities to be indexed
	expectedTimeKey := fmt.Sprintf("%04d.%02d.%02d.%02d",
		now.Year(), now.Month(), now.Day(), now.Hour())

	var temporalData map[string]interface{}
	require.Eventually(t, func() bool {
		entry, err := temporalBucket.Get(ctx, expectedTimeKey)
		if err != nil {
			return false
		}
		if err := json.Unmarshal(entry.Value(), &temporalData); err != nil {
			return false
		}
		events, ok := temporalData["events"].([]interface{})
		if !ok {
			return false
		}
		return len(events) >= 3
	}, 10*time.Second, 200*time.Millisecond, "All 3 entities should be indexed")

	// Verify all 3 events accumulated
	events := temporalData["events"].([]interface{})
	assert.GreaterOrEqual(t, len(events), 3, "should have at least 3 events")

	// Verify entity_count tracks unique entities
	entityCount := int(temporalData["entity_count"].(float64))
	assert.Equal(t, 3, entityCount, "entity_count should be 3 unique entities")

	// Verify all entity IDs present
	foundEntities := make(map[string]bool)
	for _, evt := range events {
		eventMap := evt.(map[string]interface{})
		if entity, ok := eventMap["entity"].(string); ok {
			foundEntities[entity] = true
		}
	}
	for _, entityID := range entityIDs {
		assert.True(t, foundEntities[entityID], "entity %s should be in events", entityID)
	}

	t.Logf("Temporal index accumulated %d events for %d unique entities", len(events), entityCount)
}
