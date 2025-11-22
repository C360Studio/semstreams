//go:build integration

package graphclustering

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
