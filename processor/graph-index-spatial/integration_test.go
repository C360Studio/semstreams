//go:build integration

package graphindexspatial

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

// TestIntegration_SpatialIndexFlow tests the full entity -> spatial index flow
// Verifies that the component produces data in the format expected by QuerySpatial
func TestIntegration_SpatialIndexFlow(t *testing.T) {
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

	comp, err := CreateGraphIndexSpatial(configJSON, deps)
	require.NoError(t, err)
	require.NotNil(t, comp)

	spatialIndex := comp.(*Component)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	require.NoError(t, spatialIndex.Initialize())

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
	require.NoError(t, spatialIndex.Start(ctx))
	defer spatialIndex.Stop(context.Background())

	// Wait for SPATIAL_INDEX bucket to be created by component
	var spatialBucket jetstream.KeyValue
	require.Eventually(t, func() bool {
		spatialBucket, err = js.KeyValue(ctx, graph.BucketSpatialIndex)
		return err == nil
	}, 5*time.Second, 100*time.Millisecond, "SPATIAL_INDEX bucket should be created")

	// Create test entity with spatial coordinates
	now := time.Now().UTC()
	entityID := "c360.platform.robotics.mav1.drone.001"
	lat := 37.7749
	lon := -122.4194
	alt := 10.0

	state := graph.EntityState{
		ID: entityID,
		Triples: []message.Triple{
			{
				Subject:   entityID,
				Predicate: "geo.location.latitude",
				Object:    lat,
				Source:    "test",
				Timestamp: now,
			},
			{
				Subject:   entityID,
				Predicate: "geo.location.longitude",
				Object:    lon,
				Source:    "test",
				Timestamp: now,
			},
			{
				Subject:   entityID,
				Predicate: "geo.location.altitude",
				Object:    alt,
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

	// Calculate expected geohash key (precision=6, multiplier=100.0)
	// latInt = floor((37.7749 + 90.0) * 100.0) = floor(12777.49) = 12777
	// lonInt = floor((-122.4194 + 180.0) * 100.0) = floor(5758.06) = 5758
	expectedGeohashKey := "geo_6_12777_5758"

	// Wait for entity to be indexed
	var indexEntry jetstream.KeyValueEntry
	require.Eventually(t, func() bool {
		indexEntry, err = spatialBucket.Get(ctx, expectedGeohashKey)
		return err == nil
	}, 5*time.Second, 100*time.Millisecond, "Entity should be indexed in spatial bucket")

	// Verify data format matches QuerySpatial expectations
	var spatialData map[string]interface{}
	err = json.Unmarshal(indexEntry.Value(), &spatialData)
	require.NoError(t, err)

	// Check "entities" map exists
	entities, ok := spatialData["entities"].(map[string]interface{})
	require.True(t, ok, "spatialData should have 'entities' map, got: %+v", spatialData)
	require.GreaterOrEqual(t, len(entities), 1, "should have at least 1 entity")

	// Verify entity data structure
	entityData, ok := entities[entityID].(map[string]interface{})
	require.True(t, ok, "entity should be in entities map")
	assert.Equal(t, lat, entityData["lat"], "entity should have correct latitude")
	assert.Equal(t, lon, entityData["lon"], "entity should have correct longitude")
	assert.Equal(t, alt, entityData["alt"], "entity should have correct altitude")
	assert.NotEmpty(t, entityData["updated"], "entity should have 'updated' timestamp")

	// Verify last_update exists
	lastUpdate, ok := spatialData["last_update"].(float64)
	require.True(t, ok, "spatialData should have 'last_update'")
	assert.Greater(t, lastUpdate, float64(0), "last_update should be positive timestamp")

	t.Logf("Spatial index entry for %s: %s", expectedGeohashKey, string(indexEntry.Value()))
}

// TestIntegration_SpatialIndexKeyFormat verifies the geohash key calculation
func TestIntegration_SpatialIndexKeyFormat(t *testing.T) {
	testClient := natsclient.NewTestClient(t, natsclient.WithKV())
	nc := testClient.Client

	config := DefaultConfig()
	configJSON, err := json.Marshal(config)
	require.NoError(t, err)

	deps := component.Dependencies{
		NATSClient: nc,
	}

	comp, err := CreateGraphIndexSpatial(configJSON, deps)
	require.NoError(t, err)

	spatialIndex := comp.(*Component)

	// Test various coordinates produce expected keys
	tests := []struct {
		name      string
		lat       float64
		lon       float64
		precision int
		wantKey   string
	}{
		{
			name:      "precision 6 - San Francisco",
			lat:       37.7749,
			lon:       -122.4194,
			precision: 6,
			// latInt = floor((37.7749 + 90.0) * 100.0) = 12777
			// lonInt = floor((-122.4194 + 180.0) * 100.0) = 5758
			wantKey: "geo_6_12777_5758",
		},
		{
			name:      "precision 6 - New York",
			lat:       40.7128,
			lon:       -74.0060,
			precision: 6,
			// latInt = floor((40.7128 + 90.0) * 100.0) = 13071
			// lonInt = floor((-74.0060 + 180.0) * 100.0) = 10599
			wantKey: "geo_6_13071_10599",
		},
		{
			name:      "precision 6 - London",
			lat:       51.5074,
			lon:       -0.1278,
			precision: 6,
			// latInt = floor((51.5074 + 90.0) * 100.0) = 14150
			// lonInt = floor((-0.1278 + 180.0) * 100.0) = 17987
			wantKey: "geo_6_14150_17987",
		},
		{
			name:      "precision 6 - origin (0,0)",
			lat:       0.0,
			lon:       0.0,
			precision: 6,
			// latInt = floor((0.0 + 90.0) * 100.0) = 9000
			// lonInt = floor((0.0 + 180.0) * 100.0) = 18000
			wantKey: "geo_6_9000_18000",
		},
		{
			name:      "precision 6 - south pole",
			lat:       -90.0,
			lon:       0.0,
			precision: 6,
			// latInt = floor((-90.0 + 90.0) * 100.0) = 0
			// lonInt = floor((0.0 + 180.0) * 100.0) = 18000
			wantKey: "geo_6_0_18000",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spatialIndex.config.GeohashPrecision = tt.precision
			got := spatialIndex.calculateGeohash(tt.lat, tt.lon, tt.precision)
			assert.Equal(t, tt.wantKey, got, "geohash key should match expected format")
		})
	}
}

// TestIntegration_SpatialIndexAccumulation verifies multiple entities in same cell accumulate
func TestIntegration_SpatialIndexAccumulation(t *testing.T) {
	testClient := natsclient.NewTestClient(t, natsclient.WithKV())
	nc := testClient.Client

	config := DefaultConfig()
	configJSON, err := json.Marshal(config)
	require.NoError(t, err)

	deps := component.Dependencies{
		NATSClient: nc,
	}

	comp, err := CreateGraphIndexSpatial(configJSON, deps)
	require.NoError(t, err)

	spatialIndex := comp.(*Component)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	require.NoError(t, spatialIndex.Initialize())

	js, err := nc.JetStream()
	require.NoError(t, err)

	entityBucket, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket:      graph.BucketEntityStates,
		Description: "Test entity states",
	})
	require.NoError(t, err)

	require.NoError(t, spatialIndex.Start(ctx))
	defer spatialIndex.Stop(context.Background())

	var spatialBucket jetstream.KeyValue
	require.Eventually(t, func() bool {
		spatialBucket, err = js.KeyValue(ctx, graph.BucketSpatialIndex)
		return err == nil
	}, 5*time.Second, 100*time.Millisecond)

	// Write 3 different entities with similar coordinates (same geohash cell)
	// All within San Francisco area that maps to same precision-6 cell
	now := time.Now().UTC()
	entities := []struct {
		id  string
		lat float64
		lon float64
		alt float64
	}{
		{
			id:  "c360.platform.robotics.mav1.drone.001",
			lat: 37.7749,
			lon: -122.4194,
			alt: 10.0,
		},
		{
			id:  "c360.platform.robotics.mav1.drone.002",
			lat: 37.7750, // Very close, same geohash cell
			lon: -122.4195,
			alt: 15.0,
		},
		{
			id:  "c360.platform.robotics.mav1.drone.003",
			lat: 37.7751, // Very close, same geohash cell
			lon: -122.4196,
			alt: 20.0,
		},
	}

	for i, entity := range entities {
		state := graph.EntityState{
			ID: entity.id,
			Triples: []message.Triple{
				{Subject: entity.id, Predicate: "geo.location.latitude", Object: entity.lat, Source: "test", Timestamp: now},
				{Subject: entity.id, Predicate: "geo.location.longitude", Object: entity.lon, Source: "test", Timestamp: now},
				{Subject: entity.id, Predicate: "geo.location.altitude", Object: entity.alt, Source: "test", Timestamp: now},
				{Subject: entity.id, Predicate: "test.spatial.index", Object: i, Source: "test", Timestamp: now},
			},
			MessageType: message.Type{Domain: "test", Category: "entity", Version: "v1"},
			Version:     1,
			UpdatedAt:   now.Add(time.Duration(i) * time.Minute),
		}
		stateData, err := json.Marshal(state)
		require.NoError(t, err)

		_, err = entityBucket.Put(ctx, entity.id, stateData)
		require.NoError(t, err)

		// Small delay to ensure ordering
		time.Sleep(100 * time.Millisecond)
	}

	// Calculate expected geohash key (all 3 entities should map to same cell)
	expectedGeohashKey := "geo_6_12777_5758"

	// Wait for all entities to be indexed
	var spatialData map[string]interface{}
	require.Eventually(t, func() bool {
		entry, err := spatialBucket.Get(ctx, expectedGeohashKey)
		if err != nil {
			return false
		}
		if err := json.Unmarshal(entry.Value(), &spatialData); err != nil {
			return false
		}
		entitiesMap, ok := spatialData["entities"].(map[string]interface{})
		if !ok {
			return false
		}
		return len(entitiesMap) >= 3
	}, 10*time.Second, 200*time.Millisecond, "All 3 entities should be indexed in same cell")

	// Verify all 3 entities accumulated
	entitiesMap := spatialData["entities"].(map[string]interface{})
	assert.GreaterOrEqual(t, len(entitiesMap), 3, "should have at least 3 entities in same geohash cell")

	// Verify all entity IDs present with correct data
	for _, entity := range entities {
		entityData, ok := entitiesMap[entity.id].(map[string]interface{})
		require.True(t, ok, "entity %s should be in entities map", entity.id)
		assert.Equal(t, entity.lat, entityData["lat"], "entity %s should have correct lat", entity.id)
		assert.Equal(t, entity.lon, entityData["lon"], "entity %s should have correct lon", entity.id)
		assert.Equal(t, entity.alt, entityData["alt"], "entity %s should have correct alt", entity.id)
		assert.NotEmpty(t, entityData["updated"], "entity %s should have updated timestamp", entity.id)
	}

	// Verify last_update exists and is valid
	lastUpdate, ok := spatialData["last_update"].(float64)
	require.True(t, ok, "spatialData should have 'last_update'")
	assert.Greater(t, lastUpdate, float64(0), "last_update should be positive timestamp")

	t.Logf("Spatial index accumulated %d entities in geohash cell %s", len(entitiesMap), expectedGeohashKey)
}
