//go:build integration

package indexmanager

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gtypes "github.com/c360/semstreams/graph"
	"github.com/c360/semstreams/message"
)

func TestIntegration_SpatialIndex_ProcessesTriples(t *testing.T) {
	// Use real NATS with testcontainers
	natsClient := getSharedNATSClient(t)
	ctx := context.Background()

	// Create real KV bucket for spatial index
	spatialBucket, err := natsClient.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{
		Bucket:      "TEST_SPATIAL_INDEX",
		Description: "Test spatial index bucket",
		History:     10,
	})
	require.NoError(t, err)

	// Create SpatialIndex with real dependencies
	metrics := &InternalMetrics{}
	spatialIndex := NewSpatialIndex(spatialBucket, natsClient, metrics, nil, nil)

	// Test entity with PROPER triples (not properties)
	entity := &gtypes.EntityState{
		Node: gtypes.NodeProperties{
			ID:         "c360.platform1.robotics.test.drone.0",
			Type:       "drone",
			Properties: map[string]any{}, // Empty - data should be in triples
		},
		Triples: []message.Triple{
			{
				Subject:   "c360.platform1.robotics.test.drone.0",
				Predicate: "geo.location.latitude",
				Object:    37.7749,
				Source:    "test_position",
				Timestamp: time.Now(),
			},
			{
				Subject:   "c360.platform1.robotics.test.drone.0",
				Predicate: "geo.location.longitude",
				Object:    -122.4194,
				Source:    "test_position",
				Timestamp: time.Now(),
			},
			{
				Subject:   "c360.platform1.robotics.test.drone.0",
				Predicate: "geo.location.altitude",
				Object:    100.0,
				Source:    "test_position",
				Timestamp: time.Now(),
			},
		},
	}

	// Process through spatial index
	err = spatialIndex.HandleCreate(ctx, "c360.platform1.robotics.test.drone.0", entity)
	require.NoError(t, err)

	// Verify real KV bucket contents
	entries := getAllKVEntries(t, spatialBucket, "*")
	assert.Equal(t, 1, len(entries), "Should have exactly one geohash entry")

	// Verify geohash structure
	if len(entries) > 0 {
		var spatialData map[string]interface{}
		err = json.Unmarshal(entries[0].Value(), &spatialData)
		require.NoError(t, err)

		// Check entities map
		entitiesRaw, ok := spatialData["entities"]
		require.True(t, ok, "Spatial data should have entities map")

		entities, ok := entitiesRaw.(map[string]interface{})
		require.True(t, ok, "Entities should be a map")

		// Verify our drone is in the spatial index
		droneData, ok := entities["c360.platform1.robotics.test.drone.0"]
		require.True(t, ok, "Drone should be in spatial index")

		droneMap, ok := droneData.(map[string]interface{})
		require.True(t, ok, "Drone data should be a map")

		// Verify coordinates
		assert.Equal(t, 37.7749, droneMap["lat"])
		assert.Equal(t, -122.4194, droneMap["lon"])
		assert.Equal(t, 100.0, droneMap["alt"])
	}
}

func TestIntegration_SpatialIndex_SkipsWithoutTriples(t *testing.T) {
	// Use real NATS with testcontainers
	natsClient := getSharedNATSClient(t)
	ctx := context.Background()

	// Create real KV bucket for spatial index
	spatialBucket, err := natsClient.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{
		Bucket:      "TEST_SPATIAL_INDEX_EMPTY",
		Description: "Test spatial index bucket for empty case",
		History:     10,
	})
	require.NoError(t, err)

	// Create SpatialIndex with real dependencies
	metrics := &InternalMetrics{}
	spatialIndex := NewSpatialIndex(spatialBucket, natsClient, metrics, nil, nil)

	// Test entity with NO triples (current E2E failure case)
	entity := &gtypes.EntityState{
		Node: gtypes.NodeProperties{
			ID:   "c360.platform1.robotics.test.drone.1",
			Type: "drone",
			Properties: map[string]any{
				// Location data in properties (anti-pattern)
				"geo.location.latitude":  37.7749,
				"geo.location.longitude": -122.4194,
				"geo.location.altitude":  100.0,
			},
		},
		Triples: nil, // This is what E2E currently has - broken!
	}

	// Process through spatial index
	err = spatialIndex.HandleCreate(ctx, "c360.platform1.robotics.test.drone.1", entity)
	require.NoError(t, err)

	// Verify KV bucket is EMPTY (spatial index should skip entities without triples)
	entries := getAllKVEntries(t, spatialBucket, "*")
	assert.Equal(t, 0, len(entries), "Spatial index should be empty when entity has no geo triples")
}

func TestIntegration_SpatialIndex_ConfigurablePrecision(t *testing.T) {
	// Test different precision levels generate different geohashes
	testCases := []struct {
		precision int
		lat, lon  float64
		expected  string // Expected geohash pattern
	}{
		{4, 37.7749, -122.4194, "geo_4_"}, // ~2.5km bins
		{7, 37.7749, -122.4194, "geo_7_"}, // ~30m bins (default)
		{8, 37.7749, -122.4194, "geo_8_"}, // ~5m bins
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("precision_%d", tc.precision), func(t *testing.T) {
			spatialIndex := &SpatialIndex{precision: tc.precision}
			geohash := spatialIndex.calculateGeohash(tc.lat, tc.lon, tc.precision)
			assert.Contains(t, geohash, tc.expected, "Geohash should include precision level")
		})
	}
}

// Helper function to get all KV entries for testing
func getAllKVEntries(t *testing.T, bucket jetstream.KeyValue, _ string) []jetstream.KeyValueEntry {
	ctx := context.Background()
	watcher, err := bucket.WatchAll(ctx)
	require.NoError(t, err)
	defer watcher.Stop()

	var entries []jetstream.KeyValueEntry

	// Collect initial entries
	for entry := range watcher.Updates() {
		if entry == nil {
			break // End of initial entries
		}
		entries = append(entries, entry)
	}

	return entries
}
