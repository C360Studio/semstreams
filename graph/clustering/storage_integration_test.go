//go:build integration

package clustering

import (
	"context"
	"testing"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIntegration_SaveAndGetCommunity tests basic save and retrieve operations
func TestIntegration_SaveAndGetCommunity(t *testing.T) {
	natsClient := getSharedNATSClient(t)
	ctx := context.Background()

	// Create or get KV bucket
	kv, err := natsClient.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{
		Bucket: CommunityBucket,
	})
	require.NoError(t, err)

	// Clean up after test
	defer func() {
		keys, _ := kv.Keys(ctx)
		for _, key := range keys {
			kv.Delete(ctx, key)
		}
	}()

	storage := NewNATSCommunityStorage(kv)

	// Create a test community
	community := &Community{
		ID:      "comm-0-test",
		Level:   0,
		Members: []string{"entity1", "entity2", "entity3"},
		Metadata: map[string]interface{}{
			"size": 3,
		},
	}

	// Save community
	err = storage.SaveCommunity(ctx, community)
	require.NoError(t, err)

	// Retrieve community
	retrieved, err := storage.GetCommunity(ctx, "comm-0-test")
	require.NoError(t, err)
	require.NotNil(t, retrieved)

	// Verify contents
	assert.Equal(t, community.ID, retrieved.ID)
	assert.Equal(t, community.Level, retrieved.Level)
	assert.ElementsMatch(t, community.Members, retrieved.Members)
	// JSON unmarshaling converts numbers to float64
	assert.Equal(t, float64(3), retrieved.Metadata["size"])
}

// TestIntegration_GetEntityCommunity tests entity -> community mapping
func TestIntegration_GetEntityCommunity(t *testing.T) {
	natsClient := getSharedNATSClient(t)
	ctx := context.Background()

	kv, err := natsClient.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{
		Bucket: CommunityBucket,
	})
	require.NoError(t, err)

	defer func() {
		keys, _ := kv.Keys(ctx)
		for _, key := range keys {
			kv.Delete(ctx, key)
		}
	}()

	storage := NewNATSCommunityStorage(kv)

	// Create communities with different entities
	comm1 := &Community{
		ID:      "comm-0-A",
		Level:   0,
		Members: []string{"entity1", "entity2"},
	}
	comm2 := &Community{
		ID:      "comm-0-B",
		Level:   0,
		Members: []string{"entity3", "entity4"},
	}

	err = storage.SaveCommunity(ctx, comm1)
	require.NoError(t, err)
	err = storage.SaveCommunity(ctx, comm2)
	require.NoError(t, err)

	// Test entity -> community lookup
	result, err := storage.GetEntityCommunity(ctx, "entity1", 0)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "comm-0-A", result.ID)

	result, err = storage.GetEntityCommunity(ctx, "entity3", 0)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "comm-0-B", result.ID)

	// Test non-existent entity
	result, err = storage.GetEntityCommunity(ctx, "nonexistent", 0)
	require.NoError(t, err)
	assert.Nil(t, result)
}

// TestIntegration_GetCommunitiesByLevel tests level filtering
func TestIntegration_GetCommunitiesByLevel(t *testing.T) {
	natsClient := getSharedNATSClient(t)
	ctx := context.Background()

	kv, err := natsClient.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{
		Bucket: CommunityBucket,
	})
	require.NoError(t, err)

	defer func() {
		keys, _ := kv.Keys(ctx)
		for _, key := range keys {
			kv.Delete(ctx, key)
		}
	}()

	storage := NewNATSCommunityStorage(kv)

	// Create communities at different levels
	level0Communities := []*Community{
		{ID: "comm-0-A", Level: 0, Members: []string{"e1", "e2"}},
		{ID: "comm-0-B", Level: 0, Members: []string{"e3", "e4"}},
	}
	level1Communities := []*Community{
		{ID: "comm-1-X", Level: 1, Members: []string{"e1", "e2", "e3"}},
	}

	for _, comm := range level0Communities {
		err = storage.SaveCommunity(ctx, comm)
		require.NoError(t, err)
	}
	for _, comm := range level1Communities {
		err = storage.SaveCommunity(ctx, comm)
		require.NoError(t, err)
	}

	// Get level 0 communities
	results, err := storage.GetCommunitiesByLevel(ctx, 0)
	require.NoError(t, err)
	assert.Len(t, results, 2)

	// Verify IDs
	ids := make([]string, len(results))
	for i, comm := range results {
		ids[i] = comm.ID
	}
	assert.ElementsMatch(t, []string{"comm-0-A", "comm-0-B"}, ids)

	// Get level 1 communities
	results, err = storage.GetCommunitiesByLevel(ctx, 1)
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, "comm-1-X", results[0].ID)
}

// TestIntegration_DeleteCommunity tests deletion with cleanup
func TestIntegration_DeleteCommunity(t *testing.T) {
	natsClient := getSharedNATSClient(t)
	ctx := context.Background()

	kv, err := natsClient.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{
		Bucket: CommunityBucket,
	})
	require.NoError(t, err)

	defer func() {
		keys, _ := kv.Keys(ctx)
		for _, key := range keys {
			kv.Delete(ctx, key)
		}
	}()

	storage := NewNATSCommunityStorage(kv)

	// Create community
	community := &Community{
		ID:      "comm-0-test",
		Level:   0,
		Members: []string{"entity1", "entity2"},
	}
	err = storage.SaveCommunity(ctx, community)
	require.NoError(t, err)

	// Verify it exists
	retrieved, err := storage.GetCommunity(ctx, "comm-0-test")
	require.NoError(t, err)
	require.NotNil(t, retrieved)

	// Delete community
	err = storage.DeleteCommunity(ctx, "comm-0-test")
	require.NoError(t, err)

	// Verify community is gone
	retrieved, err = storage.GetCommunity(ctx, "comm-0-test")
	require.NoError(t, err)
	assert.Nil(t, retrieved)

	// Verify entity mappings are cleaned up
	result, err := storage.GetEntityCommunity(ctx, "entity1", 0)
	require.NoError(t, err)
	assert.Nil(t, result)
}

// TestIntegration_Clear tests bulk deletion
func TestIntegration_Clear(t *testing.T) {
	natsClient := getSharedNATSClient(t)
	ctx := context.Background()

	kv, err := natsClient.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{
		Bucket: CommunityBucket,
	})
	require.NoError(t, err)

	defer func() {
		keys, _ := kv.Keys(ctx)
		for _, key := range keys {
			kv.Delete(ctx, key)
		}
	}()

	storage := NewNATSCommunityStorage(kv)

	// Create multiple communities
	communities := []*Community{
		{ID: "comm-0-A", Level: 0, Members: []string{"e1", "e2"}},
		{ID: "comm-0-B", Level: 0, Members: []string{"e3", "e4"}},
		{ID: "comm-1-X", Level: 1, Members: []string{"e5", "e6"}},
	}

	for _, comm := range communities {
		err = storage.SaveCommunity(ctx, comm)
		require.NoError(t, err)
	}

	// Verify communities exist
	results, err := storage.GetCommunitiesByLevel(ctx, 0)
	require.NoError(t, err)
	assert.Len(t, results, 2)

	// Clear all communities
	err = storage.Clear(ctx)
	require.NoError(t, err)

	// Verify all levels are empty
	results, err = storage.GetCommunitiesByLevel(ctx, 0)
	require.NoError(t, err)
	assert.Len(t, results, 0)

	results, err = storage.GetCommunitiesByLevel(ctx, 1)
	require.NoError(t, err)
	assert.Len(t, results, 0)
}

// TestIntegration_Prune pins Prune's key derivation against real NATS KV: the
// keep set is expressed as communities, and Prune must reconstruct BOTH the
// community keys ({level}.{id}) and the entity mapping keys
// (entity.{level}.{entity_id}) they imply. A prune that forgets the mapping keys
// still answers queries correctly but leaks a key per churned entity forever, so
// this asserts on the raw key set rather than on query results.
func TestIntegration_Prune(t *testing.T) {
	natsClient := getSharedNATSClient(t)
	ctx := context.Background()

	kv, err := natsClient.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{
		Bucket: CommunityBucket,
	})
	require.NoError(t, err)

	defer func() {
		keys, _ := kv.Keys(ctx)
		for _, key := range keys {
			kv.Delete(ctx, key)
		}
	}()

	storage := NewNATSCommunityStorage(kv)

	// Prior partition.
	prior := []*Community{
		{ID: "A", Level: 0, Members: []string{"e1", "e2"}},
		{ID: "B", Level: 0, Members: []string{"e3", "e4"}},
		{ID: "X", Level: 1, Members: []string{"e1", "e2", "e3", "e4"}},
	}
	for _, comm := range prior {
		require.NoError(t, storage.SaveCommunity(ctx, comm))
	}

	// New partition: B is gone entirely, A keeps only e1, level 1 is unchanged.
	next := []*Community{
		{ID: "A", Level: 0, Members: []string{"e1"}},
		{ID: "X", Level: 1, Members: []string{"e1", "e2", "e3", "e4"}},
	}
	for _, comm := range next {
		require.NoError(t, storage.SaveCommunity(ctx, comm))
	}

	require.NoError(t, storage.Prune(ctx, next))

	keys, err := kv.Keys(ctx)
	require.NoError(t, err)
	keySet := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		keySet[k] = struct{}{}
	}

	retained := []string{"0.A", "1.X", "entity.0.e1", "entity.1.e1", "entity.1.e4"}
	for _, k := range retained {
		assert.Contains(t, keySet, k, "key %s belongs to the new partition and must be retained", k)
	}

	removed := []string{"0.B", "entity.0.e2", "entity.0.e3", "entity.0.e4"}
	for _, k := range removed {
		assert.NotContains(t, keySet, k, "key %s belongs only to the prior partition and must be pruned", k)
	}

	// Query surfaces agree with the pruned key set.
	level0, err := storage.GetCommunitiesByLevel(ctx, 0)
	require.NoError(t, err)
	require.Len(t, level0, 1)
	assert.Equal(t, "A", level0[0].ID)

	level1, err := storage.GetCommunitiesByLevel(ctx, 1)
	require.NoError(t, err)
	assert.Len(t, level1, 1)
}

// TestIntegration_PruneEmptyKeepRemovesEverything covers the empty-graph case:
// no communities means no keys.
func TestIntegration_PruneEmptyKeepRemovesEverything(t *testing.T) {
	natsClient := getSharedNATSClient(t)
	ctx := context.Background()

	kv, err := natsClient.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{
		Bucket: CommunityBucket,
	})
	require.NoError(t, err)

	defer func() {
		keys, _ := kv.Keys(ctx)
		for _, key := range keys {
			kv.Delete(ctx, key)
		}
	}()

	storage := NewNATSCommunityStorage(kv)
	require.NoError(t, storage.SaveCommunity(ctx, &Community{
		ID: "A", Level: 0, Members: []string{"e1", "e2"},
	}))

	require.NoError(t, storage.Prune(ctx, nil))

	results, err := storage.GetCommunitiesByLevel(ctx, 0)
	require.NoError(t, err)
	assert.Empty(t, results)

	mapping, err := storage.GetEntityCommunity(ctx, "e1", 0)
	require.NoError(t, err)
	assert.Nil(t, mapping)
}

// TestIntegration_CommunityIDParsing tests robust ID parsing with edge cases
func TestIntegration_CommunityIDParsing(t *testing.T) {
	natsClient := getSharedNATSClient(t)
	ctx := context.Background()

	kv, err := natsClient.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{
		Bucket: CommunityBucket,
	})
	require.NoError(t, err)

	defer func() {
		keys, _ := kv.Keys(ctx)
		for _, key := range keys {
			kv.Delete(ctx, key)
		}
	}()

	storage := NewNATSCommunityStorage(kv)

	// Test community ID with hyphens in label
	community := &Community{
		ID:      "comm-0-label-with-hyphens",
		Level:   0,
		Members: []string{"entity1"},
	}

	err = storage.SaveCommunity(ctx, community)
	require.NoError(t, err)

	// Retrieve and verify
	retrieved, err := storage.GetCommunity(ctx, "comm-0-label-with-hyphens")
	require.NoError(t, err)
	require.NotNil(t, retrieved)
	assert.Equal(t, "comm-0-label-with-hyphens", retrieved.ID)
	assert.Equal(t, 0, retrieved.Level)
}

// TestIntegration_EmptyMembersCommunity tests edge case of empty community
func TestIntegration_EmptyMembersCommunity(t *testing.T) {
	natsClient := getSharedNATSClient(t)
	ctx := context.Background()

	kv, err := natsClient.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{
		Bucket: CommunityBucket,
	})
	require.NoError(t, err)

	defer func() {
		keys, _ := kv.Keys(ctx)
		for _, key := range keys {
			kv.Delete(ctx, key)
		}
	}()

	storage := NewNATSCommunityStorage(kv)

	// Create community with no members
	community := &Community{
		ID:      "comm-0-empty",
		Level:   0,
		Members: []string{},
	}

	err = storage.SaveCommunity(ctx, community)
	require.NoError(t, err)

	// Retrieve and verify
	retrieved, err := storage.GetCommunity(ctx, "comm-0-empty")
	require.NoError(t, err)
	require.NotNil(t, retrieved)
	assert.Equal(t, "comm-0-empty", retrieved.ID)
	assert.Empty(t, retrieved.Members)

	// Delete should work even with no entity mappings
	err = storage.DeleteCommunity(ctx, "comm-0-empty")
	require.NoError(t, err)
}

// TestIntegration_KeyFormatConsumerCompatibility verifies that storage writes keys
// in the format that consumers (enhancement_worker, community_cache) expect.
// This test catches key format drift bugs where storage is refactored but consumers
// still use stale format (e.g., the "graph.community." prefix removal bug).
func TestIntegration_KeyFormatConsumerCompatibility(t *testing.T) {
	natsClient := getSharedNATSClient(t)
	ctx := context.Background()

	kv, err := natsClient.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{
		Bucket: CommunityBucket,
	})
	require.NoError(t, err)

	defer func() {
		keys, _ := kv.Keys(ctx)
		for _, key := range keys {
			kv.Delete(ctx, key)
		}
	}()

	storage := NewNATSCommunityStorage(kv)

	// Create test communities
	community := &Community{
		ID:      "test-community-123",
		Level:   0,
		Members: []string{"entity-a", "entity-b", "entity-c"},
	}
	err = storage.SaveCommunity(ctx, community)
	require.NoError(t, err)

	// Get all keys from bucket
	keys, err := kv.Keys(ctx)
	require.NoError(t, err)

	// Verify key formats match what consumers expect:
	// - Community data key: "{level}.{communityID}" (e.g., "0.test-community-123")
	// - Entity mapping key: "entity.{level}.{entityID}" (e.g., "entity.0.entity-a")
	foundCommunityKey := false
	foundEntityMappingKeys := 0

	expectedCommunityKey := "0.test-community-123"
	expectedEntityKeyPrefix := "entity.0."

	for _, key := range keys {
		t.Logf("Found key: %s", key)

		// Check community data key format
		if key == expectedCommunityKey {
			foundCommunityKey = true
		}

		// Check entity mapping key format
		// enhancement_worker.go and community_cache.go use: strings.HasPrefix(key, "entity.")
		if len(key) > 9 && key[:9] == expectedEntityKeyPrefix {
			foundEntityMappingKeys++
		}
	}

	// Verify community key format: "{level}.{communityID}" (NOT "graph.community.{level}.{id}")
	assert.True(t, foundCommunityKey,
		"Community key should have format '{level}.{communityID}', expected '%s' in keys %v",
		expectedCommunityKey, keys)

	// Verify entity mapping key format: "entity.{level}.{entityID}" (NOT "graph.community.entity.{entityID}")
	assert.Equal(t, 3, foundEntityMappingKeys,
		"Should have 3 entity mapping keys with format 'entity.{level}.{entityID}', found %d in keys %v",
		foundEntityMappingKeys, keys)

	// Verify consumers can use the same filter patterns that storage writes:
	// enhancement_worker.go line 272: strings.HasPrefix(key, "entity.")
	// community_cache.go line 158: strings.HasPrefix(key, "entity.")
	for _, key := range keys {
		isEntityMapping := len(key) > 7 && key[:7] == "entity."
		isCommunityData := !isEntityMapping && len(key) > 0 && key[0] >= '0' && key[0] <= '9'

		// Every key should be classified as one or the other
		assert.True(t, isEntityMapping || isCommunityData,
			"Key '%s' should match either entity mapping or community data pattern", key)
	}
}

// TestIntegration_CommunitySummaryFields tests the new summary fields
func TestIntegration_CommunitySummaryFields(t *testing.T) {
	natsClient := getSharedNATSClient(t)
	ctx := context.Background()

	kv, err := natsClient.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{
		Bucket: CommunityBucket,
	})
	require.NoError(t, err)

	defer func() {
		keys, _ := kv.Keys(ctx)
		for _, key := range keys {
			kv.Delete(ctx, key)
		}
	}()

	storage := NewNATSCommunityStorage(kv)

	// Create community with summary fields
	community := &Community{
		ID:                 "comm-0-robotics",
		Level:              0,
		Members:            []string{"drone1", "sensor2", "controller3"},
		StatisticalSummary: "Community focused on autonomous navigation and sensor fusion",
		Keywords: []string{
			"autonomous",
			"navigation",
			"sensor-fusion",
			"path-planning",
		},
		RepEntities:   []string{"drone1", "sensor2"},
		SummaryStatus: "statistical",
		Metadata: map[string]interface{}{
			"domain": "robotics",
		},
	}

	// Save community with summary fields
	err = storage.SaveCommunity(ctx, community)
	require.NoError(t, err)

	// Retrieve and verify all fields
	retrieved, err := storage.GetCommunity(ctx, "comm-0-robotics")
	require.NoError(t, err)
	require.NotNil(t, retrieved)

	assert.Equal(t, community.ID, retrieved.ID)
	assert.Equal(t, community.Level, retrieved.Level)
	assert.ElementsMatch(t, community.Members, retrieved.Members)
	assert.Equal(t, community.StatisticalSummary, retrieved.StatisticalSummary)
	assert.ElementsMatch(t, community.Keywords, retrieved.Keywords)
	assert.ElementsMatch(t, community.RepEntities, retrieved.RepEntities)
	assert.Equal(t, community.SummaryStatus, retrieved.SummaryStatus)
	assert.Equal(t, "robotics", retrieved.Metadata["domain"])
}
