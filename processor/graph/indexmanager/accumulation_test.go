package indexmanager

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"

	gtypes "github.com/c360/semstreams/graph"
	"github.com/c360/semstreams/message"
	"github.com/c360/semstreams/natsclient"
)

// TestTemporalIndexAccumulation verifies that temporal index
// accumulates ALL events, not just the latest per entity
func TestTemporalIndexAccumulation(t *testing.T) {
	ctx := context.Background()

	// Create a mock KV bucket for temporal index
	mockBucket := NewMockKeyValue()

	// Create temporal index with mock
	index := NewTemporalIndex(mockBucket, nil, nil, nil, slog.Default())

	// Create test entity
	entityID := "test.entity.temporal"
	baseTime := time.Now().Truncate(time.Hour)
	timeKey := fmt.Sprintf("%04d.%02d.%02d.%02d",
		baseTime.Year(), baseTime.Month(), baseTime.Day(), baseTime.Hour())

	// Track the accumulated data across updates
	var accumulatedData []byte
	var lastRevision uint64

	// Setup mock expectations for each update
	for i := 0; i < 5; i++ {
		updateTime := baseTime.Add(time.Duration(i) * time.Minute)

		if i == 0 {
			// First update - bucket returns key not found
			mockBucket.On("Get", ctx, timeKey).Return(nil, jetstream.ErrKeyNotFound).Once()

			// Expect Put for first entry
			firstData := map[string]interface{}{
				"events": []interface{}{
					map[string]interface{}{
						"entity":    entityID,
						"type":      "update",
						"timestamp": updateTime.Format(time.RFC3339),
					},
				},
				"entity_count": 1,
			}
			expectedBytes, _ := json.Marshal(firstData)
			mockBucket.On("Put", ctx, timeKey, expectedBytes).Return(uint64(i+1), nil).Once()
			accumulatedData = expectedBytes
			lastRevision = uint64(i + 1)
		} else {
			// Subsequent updates - return previous accumulated data
			entry := &MockKeyValueEntry{
				key:      timeKey,
				value:    accumulatedData,
				revision: lastRevision,
			}
			mockBucket.On("Get", ctx, timeKey).Return(entry, nil).Once()

			// Parse and update the data to add new event
			var existingData map[string]interface{}
			_ = json.Unmarshal(accumulatedData, &existingData)
			events := existingData["events"].([]interface{})
			events = append(events, map[string]interface{}{
				"entity":    entityID,
				"type":      "update",
				"timestamp": updateTime.Format(time.RFC3339),
			})
			existingData["events"] = events
			existingData["entity_count"] = 1

			newBytes, _ := json.Marshal(existingData)
			mockBucket.On("Put", ctx, timeKey, newBytes).Return(uint64(i+1), nil).Once()
			accumulatedData = newBytes
			lastRevision = uint64(i + 1)
		}
	}

	// Send 5 updates for the same entity in the same time bucket
	for i := 0; i < 5; i++ {
		entityState := &gtypes.EntityState{
			Node: gtypes.NodeProperties{
				ID: entityID,
			},
			UpdatedAt: baseTime.Add(time.Duration(i) * time.Minute),
		}

		err := index.HandleUpdate(ctx, entityID, entityState)
		require.NoError(t, err)
	}

	// Verify the final accumulated state
	var temporalData map[string]interface{}
	err := json.Unmarshal(accumulatedData, &temporalData)
	require.NoError(t, err)

	events, ok := temporalData["events"].([]interface{})
	require.True(t, ok, "events should be an array")

	// CRITICAL TEST: Should have ALL 5 events, not deduplicated
	require.Equal(t, 5, len(events), "Should accumulate all 5 events, not deduplicate")

	// Verify entity_count is 1 (unique entities)
	entityCount, ok := temporalData["entity_count"].(float64)
	require.True(t, ok)
	require.Equal(t, float64(1), entityCount, "Should track 1 unique entity")

	// Verify all expectations were met
	mockBucket.AssertExpectations(t)
}

// TestIntegration_SpatialIndexMerging verifies that spatial index
// properly merges multiple entities sharing the same geohash
func TestIntegration_SpatialIndexMerging(t *testing.T) {
	// Skip in short mode as this requires NATS
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	// Create test NATS client with JetStream and KV
	testClient := natsclient.NewTestClient(t, natsclient.WithJetStream(), natsclient.WithKV())

	// Create spatial bucket
	bucket, err := testClient.CreateKVBucket(ctx, "SPATIAL_INDEX")
	require.NoError(t, err, "Failed to create SPATIAL_INDEX bucket")

	// Create spatial index with proper NATS client for concurrent operations
	index := NewSpatialIndex(bucket, testClient.Client, nil, nil, nil)

	// Create 3 entities at the same location (same geohash)
	lat, lon := 37.7749, -122.4194 // San Francisco

	entities := []string{"drone.1", "drone.2", "drone.3"}

	for _, entityID := range entities {
		entityState := &gtypes.EntityState{
			Node: gtypes.NodeProperties{
				ID: entityID,
			},
			Triples: []message.Triple{
				{
					Subject:   entityID,
					Predicate: "geo.location.latitude",
					Object:    lat,
				},
				{
					Subject:   entityID,
					Predicate: "geo.location.longitude",
					Object:    lon,
				},
				{
					Subject:   entityID,
					Predicate: "geo.location.altitude",
					Object:    100.0,
				},
			},
		}

		err := index.HandleUpdate(ctx, entityID, entityState)
		require.NoError(t, err)
	}

	// Calculate the geohash for verification
	geohash := index.calculateGeohash(lat, lon, 7)

	// Verify all 3 entities are in the same geohash bucket
	entry, err := bucket.Get(ctx, geohash)
	require.NoError(t, err)

	var spatialData map[string]interface{}
	err = json.Unmarshal(entry.Value(), &spatialData)
	require.NoError(t, err)

	entitiesMap, ok := spatialData["entities"].(map[string]interface{})
	require.True(t, ok, "entities should be a map")

	// CRITICAL TEST: Should have ALL 3 entities, not overwritten
	require.Equal(t, 3, len(entitiesMap), "Should have all 3 entities in same geohash")

	// Verify each entity is present
	for _, entityID := range entities {
		entityData, exists := entitiesMap[entityID]
		require.True(t, exists, "Entity %s should exist in geohash", entityID)

		// Verify position data
		posData, ok := entityData.(map[string]interface{})
		require.True(t, ok)
		require.Equal(t, lat, posData["lat"])
		require.Equal(t, lon, posData["lon"])
	}
}

// TestIntegration_ConcurrentSpatialUpdates verifies that concurrent updates
// to the same geohash don't lose data
func TestIntegration_ConcurrentSpatialUpdates(t *testing.T) {
	// Skip in short mode as this requires NATS
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	// Create test NATS client with JetStream and KV
	testClient := natsclient.NewTestClient(t, natsclient.WithJetStream(), natsclient.WithKV())

	// Create spatial bucket
	bucket, err := testClient.CreateKVBucket(ctx, "SPATIAL_INDEX")
	require.NoError(t, err, "Failed to create SPATIAL_INDEX bucket")

	// Create spatial index with proper NATS client for concurrent operations
	index := NewSpatialIndex(bucket, testClient.Client, nil, nil, nil)

	// Same location for all entities
	lat, lon := 40.7128, -74.0060 // New York

	// Launch 10 concurrent updates
	done := make(chan error, 10)

	for i := 0; i < 10; i++ {
		entityID := fmt.Sprintf("concurrent.entity.%d", i)
		go func(id string, altitude int) {
			entityState := &gtypes.EntityState{
				Node: gtypes.NodeProperties{
					ID: id,
				},
				Triples: []message.Triple{
					{
						Subject:   id,
						Predicate: "geo.location.latitude",
						Object:    lat,
					},
					{
						Subject:   id,
						Predicate: "geo.location.longitude",
						Object:    lon,
					},
					{
						Subject:   id,
						Predicate: "geo.location.altitude",
						Object:    float64(altitude) * 10,
					},
				},
			}

			done <- index.HandleUpdate(ctx, id, entityState)
		}(entityID, i)
	}

	// Wait for all updates
	for i := 0; i < 10; i++ {
		err := <-done
		require.NoError(t, err)
	}

	// Verify all 10 entities made it
	geohash := index.calculateGeohash(lat, lon, 7)
	entry, err := bucket.Get(ctx, geohash)
	require.NoError(t, err)

	var spatialData map[string]interface{}
	err = json.Unmarshal(entry.Value(), &spatialData)
	require.NoError(t, err)

	entitiesMap, ok := spatialData["entities"].(map[string]interface{})
	require.True(t, ok)

	// CRITICAL TEST: No entities lost due to concurrent updates
	require.Equal(t, 10, len(entitiesMap), "All 10 concurrent updates should succeed")
}
