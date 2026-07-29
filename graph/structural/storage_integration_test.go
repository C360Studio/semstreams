//go:build integration

package structural

import (
	"context"
	"testing"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIntegration_SaveAndGetKCoreIndex tests k-core index save and retrieve operations.
func TestIntegration_SaveAndGetKCoreIndex(t *testing.T) {
	natsClient := getSharedNATSClient(t)
	ctx := context.Background()

	// Create or get KV bucket
	kv, err := natsClient.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{
		Bucket: graph.BucketStructuralIndex,
	})
	require.NoError(t, err)

	// Clean up after test
	defer cleanupBucket(ctx, kv)

	storage := NewNATSStructuralIndexStorage(kv)

	// Create test index
	computedAt := time.Now().Truncate(time.Second)
	index := &KCoreIndex{
		CoreNumbers: map[string]int{
			"entity-A": 3,
			"entity-B": 3,
			"entity-C": 3,
			"entity-D": 2,
			"entity-E": 1,
		},
		MaxCore: 3,
		CoreBuckets: map[int][]string{
			1: {"entity-E"},
			2: {"entity-D"},
			3: {"entity-A", "entity-B", "entity-C"},
		},
		ComputedAt:  computedAt,
		EntityCount: 5,
	}

	// Save index
	err = storage.SaveKCoreIndex(ctx, index)
	require.NoError(t, err)

	// Retrieve index
	retrieved, err := storage.GetKCoreIndex(ctx)
	require.NoError(t, err)
	require.NotNil(t, retrieved)

	// Verify contents
	assert.Equal(t, index.MaxCore, retrieved.MaxCore)
	assert.Equal(t, index.EntityCount, retrieved.EntityCount)
	assert.Equal(t, index.CoreNumbers, retrieved.CoreNumbers)

	// Verify core buckets contain same entities (order may differ)
	for core, entities := range index.CoreBuckets {
		assert.ElementsMatch(t, entities, retrieved.CoreBuckets[core])
	}

	// Verify time (within a second due to RFC3339 format)
	assert.WithinDuration(t, computedAt, retrieved.ComputedAt, time.Second)
}

// TestIntegration_SaveAndGetPivotIndex tests pivot index save and retrieve operations.
func TestIntegration_SaveAndGetPivotIndex(t *testing.T) {
	natsClient := getSharedNATSClient(t)
	ctx := context.Background()

	kv, err := natsClient.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{
		Bucket: graph.BucketStructuralIndex,
	})
	require.NoError(t, err)

	defer cleanupBucket(ctx, kv)

	storage := NewNATSStructuralIndexStorage(kv)

	// Create test index with realistic data
	computedAt := time.Now().Truncate(time.Second)
	index := &PivotIndex{
		Pivots: []string{"pivot-1", "pivot-2", "pivot-3"},
		DistanceVectors: map[string][]int{
			"pivot-1":  {0, 2, 3},
			"pivot-2":  {2, 0, 1},
			"pivot-3":  {3, 1, 0},
			"entity-A": {1, 1, 2},
			"entity-B": {2, 2, 1},
			"entity-C": {MaxHopDistance, MaxHopDistance, MaxHopDistance}, // Disconnected
		},
		ComputedAt:  computedAt,
		EntityCount: 6,
	}

	// Save index
	err = storage.SavePivotIndex(ctx, index)
	require.NoError(t, err)

	// Retrieve index
	retrieved, err := storage.GetPivotIndex(ctx)
	require.NoError(t, err)
	require.NotNil(t, retrieved)

	// Verify contents
	assert.Equal(t, index.Pivots, retrieved.Pivots)
	assert.Equal(t, index.EntityCount, retrieved.EntityCount)
	assert.Equal(t, index.DistanceVectors, retrieved.DistanceVectors)
	assert.WithinDuration(t, computedAt, retrieved.ComputedAt, time.Second)

	// Verify MaxHopDistance preserved for disconnected entity
	assert.Equal(t, []int{MaxHopDistance, MaxHopDistance, MaxHopDistance}, retrieved.DistanceVectors["entity-C"])
}

// TestIntegration_ClearStructuralIndices tests clearing all structural index data.
func TestIntegration_ClearStructuralIndices(t *testing.T) {
	natsClient := getSharedNATSClient(t)
	ctx := context.Background()

	kv, err := natsClient.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{
		Bucket: graph.BucketStructuralIndex,
	})
	require.NoError(t, err)

	defer cleanupBucket(ctx, kv)

	storage := NewNATSStructuralIndexStorage(kv)

	// Save both indices
	kcoreIndex := &KCoreIndex{
		CoreNumbers: map[string]int{"A": 1, "B": 1},
		MaxCore:     1,
		CoreBuckets: map[int][]string{1: {"A", "B"}},
		ComputedAt:  time.Now(),
		EntityCount: 2,
	}
	pivotIndex := &PivotIndex{
		Pivots:          []string{"P"},
		DistanceVectors: map[string][]int{"P": {0}, "A": {1}},
		ComputedAt:      time.Now(),
		EntityCount:     2,
	}

	err = storage.SaveKCoreIndex(ctx, kcoreIndex)
	require.NoError(t, err)
	err = storage.SavePivotIndex(ctx, pivotIndex)
	require.NoError(t, err)

	// Verify indices exist
	retrieved, err := storage.GetKCoreIndex(ctx)
	require.NoError(t, err)
	require.NotNil(t, retrieved)

	retrievedPivot, err := storage.GetPivotIndex(ctx)
	require.NoError(t, err)
	require.NotNil(t, retrievedPivot)

	// Clear all data
	err = storage.Clear(ctx)
	require.NoError(t, err)

	// Verify indices are gone
	retrieved, err = storage.GetKCoreIndex(ctx)
	require.NoError(t, err)
	assert.Nil(t, retrieved)

	retrievedPivot, err = storage.GetPivotIndex(ctx)
	require.NoError(t, err)
	assert.Nil(t, retrievedPivot)
}

// TestIntegration_OverwriteIndices tests overwriting existing indices.
func TestIntegration_OverwriteIndices(t *testing.T) {
	natsClient := getSharedNATSClient(t)
	ctx := context.Background()

	kv, err := natsClient.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{
		Bucket: graph.BucketStructuralIndex,
	})
	require.NoError(t, err)

	defer cleanupBucket(ctx, kv)

	storage := NewNATSStructuralIndexStorage(kv)

	// Save initial k-core index
	index1 := &KCoreIndex{
		CoreNumbers: map[string]int{"A": 1},
		MaxCore:     1,
		CoreBuckets: map[int][]string{1: {"A"}},
		ComputedAt:  time.Now(),
		EntityCount: 1,
	}

	err = storage.SaveKCoreIndex(ctx, index1)
	require.NoError(t, err)

	// Overwrite with new index (different structure)
	index2 := &KCoreIndex{
		CoreNumbers: map[string]int{
			"X": 5,
			"Y": 5,
			"Z": 3,
		},
		MaxCore: 5,
		CoreBuckets: map[int][]string{
			3: {"Z"},
			5: {"X", "Y"},
		},
		ComputedAt:  time.Now(),
		EntityCount: 3,
	}

	err = storage.SaveKCoreIndex(ctx, index2)
	require.NoError(t, err)

	// Retrieve and verify new index
	retrieved, err := storage.GetKCoreIndex(ctx)
	require.NoError(t, err)
	require.NotNil(t, retrieved)

	assert.Equal(t, 5, retrieved.MaxCore)
	assert.Equal(t, 3, retrieved.EntityCount)
	assert.Equal(t, 5, retrieved.CoreNumbers["X"])
	assert.Equal(t, 5, retrieved.CoreNumbers["Y"])
	assert.Equal(t, 3, retrieved.CoreNumbers["Z"])
}

// TestIntegration_LargeIndex tests storage with a larger index.
func TestIntegration_LargeIndex(t *testing.T) {
	natsClient := getSharedNATSClient(t)
	ctx := context.Background()

	kv, err := natsClient.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{
		Bucket: graph.BucketStructuralIndex,
	})
	require.NoError(t, err)

	defer cleanupBucket(ctx, kv)

	storage := NewNATSStructuralIndexStorage(kv)

	// Create index with 100 entities
	entityCount := 100
	coreNumbers := make(map[string]int, entityCount)
	coreBuckets := make(map[int][]string)

	for i := 0; i < entityCount; i++ {
		entityID := entityIDForIndex(i)
		core := i % 5 // Distribute across 5 core levels
		coreNumbers[entityID] = core
		coreBuckets[core] = append(coreBuckets[core], entityID)
	}

	index := &KCoreIndex{
		CoreNumbers: coreNumbers,
		MaxCore:     4,
		CoreBuckets: coreBuckets,
		ComputedAt:  time.Now(),
		EntityCount: entityCount,
	}

	// Save
	err = storage.SaveKCoreIndex(ctx, index)
	require.NoError(t, err)

	// Retrieve
	retrieved, err := storage.GetKCoreIndex(ctx)
	require.NoError(t, err)
	require.NotNil(t, retrieved)

	// Verify
	assert.Equal(t, entityCount, retrieved.EntityCount)
	assert.Equal(t, 4, retrieved.MaxCore)
	assert.Len(t, retrieved.CoreNumbers, entityCount)

	// Spot check a few entities
	assert.Equal(t, 0, retrieved.CoreNumbers["entity-0"])
	assert.Equal(t, 1, retrieved.CoreNumbers["entity-1"])
	assert.Equal(t, 4, retrieved.CoreNumbers["entity-99"])
}

// TestIntegration_EmptyBucketGet tests getting from empty bucket.
func TestIntegration_EmptyBucketGet(t *testing.T) {
	natsClient := getSharedNATSClient(t)
	ctx := context.Background()

	kv, err := natsClient.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{
		Bucket: graph.BucketStructuralIndex,
	})
	require.NoError(t, err)

	defer cleanupBucket(ctx, kv)

	storage := NewNATSStructuralIndexStorage(kv)

	// Get from empty bucket should return nil without error
	kcore, err := storage.GetKCoreIndex(ctx)
	require.NoError(t, err)
	assert.Nil(t, kcore)

	pivot, err := storage.GetPivotIndex(ctx)
	require.NoError(t, err)
	assert.Nil(t, pivot)
}

// Helper functions

func cleanupBucket(ctx context.Context, kv jetstream.KeyValue) {
	keys, _ := kv.Keys(ctx)
	for _, key := range keys {
		kv.Delete(ctx, key)
	}
}

func entityIDForIndex(i int) string {
	return "entity-" + itoa(i)
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	result := ""
	for i > 0 {
		result = string('0'+byte(i%10)) + result
		i /= 10
	}
	return result
}
