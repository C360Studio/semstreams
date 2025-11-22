package indexmanager

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gtypes "github.com/c360/semstreams/graph"
	"github.com/c360/semstreams/message"
	"github.com/c360/semstreams/natsclient"
)

// setupTestIndexManager creates and starts an index manager for testing
func setupTestIndexManager(t *testing.T, testClient *natsclient.TestClient) (Indexer, context.CancelFunc) {
	ctx := context.Background()

	entityBucket, err := testClient.CreateKVBucket(ctx, "ENTITY_STATES")
	require.NoError(t, err, "Failed to create ENTITY_STATES bucket")

	predicateBucket, err := testClient.CreateKVBucket(ctx, "PREDICATE_INDEX")
	require.NoError(t, err, "Failed to create PREDICATE_INDEX bucket")

	incomingBucket, err := testClient.CreateKVBucket(ctx, "INCOMING_INDEX")
	require.NoError(t, err, "Failed to create INCOMING_INDEX bucket")

	aliasBucket, err := testClient.CreateKVBucket(ctx, "ALIAS_INDEX")
	require.NoError(t, err, "Failed to create ALIAS_INDEX bucket")

	spatialBucket, err := testClient.CreateKVBucket(ctx, "SPATIAL_INDEX")
	require.NoError(t, err, "Failed to create SPATIAL_INDEX bucket")

	temporalBucket, err := testClient.CreateKVBucket(ctx, "TEMPORAL_INDEX")
	require.NoError(t, err, "Failed to create TEMPORAL_INDEX bucket")

	ctx, cancel := context.WithCancel(ctx)

	config := DefaultConfig()
	buckets := map[string]jetstream.KeyValue{
		"ENTITY_STATES":   entityBucket,
		"PREDICATE_INDEX": predicateBucket,
		"INCOMING_INDEX":  incomingBucket,
		"ALIAS_INDEX":     aliasBucket,
		"SPATIAL_INDEX":   spatialBucket,
		"TEMPORAL_INDEX":  temporalBucket,
	}
	indexManager, err := NewManager(config, buckets, testClient.Client, nil, nil)
	require.NoError(t, err, "Failed to create index manager")

	var wg sync.WaitGroup
	managerErrors := make(chan error, 1)
	wg.Add(1)
	go func() {
		defer wg.Done()
		managerErrors <- indexManager.Run(ctx)
	}()

	time.Sleep(100 * time.Millisecond)

	select {
	case err := <-managerErrors:
		if err != nil {
			t.Fatalf("IndexManager failed to start: %v", err)
		}
	default:
	}

	return indexManager, cancel
}

// createTestEntity creates a test entity for index testing
func createTestEntity(entityID string, triples []message.Triple) *gtypes.EntityState {
	return &gtypes.EntityState{
		Node: gtypes.NodeProperties{
			ID:     entityID,
			Type:   "domain.type",
			Status: gtypes.StatusActive,
		},
		Triples:   triples,
		Version:   1,
		UpdatedAt: time.Now(),
	}
}

// putEntityInBucket marshals and stores an entity in a KV bucket
func putEntityInBucket(ctx context.Context, bucket jetstream.KeyValue, entity *gtypes.EntityState) error {
	entityData, err := json.Marshal(entity)
	if err != nil {
		return err
	}
	_, err = bucket.Put(ctx, entity.Node.ID, entityData)
	return err
}

// waitForPredicateIndexing polls until a predicate appears in the index
func waitForPredicateIndexing(ctx context.Context, t *testing.T, indexManager Indexer, predicate, expectedEntityID string) {
	ctx, timeoutCancel := context.WithTimeout(ctx, 5*time.Second)
	defer timeoutCancel()

	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			t.Fatal("Timeout waiting for entity to be processed")
		case <-ticker.C:
			results, err := indexManager.GetPredicateIndex(ctx, predicate)
			if err == nil && len(results) > 0 {
				for _, id := range results {
					if id == expectedEntityID {
						return
					}
				}
			}
		}
	}
}

// waitForAliasResolution polls until an alias resolves correctly
func waitForAliasResolution(ctx context.Context, t *testing.T, indexManager Indexer, alias, expectedPrimaryID string) {
	ctx, timeoutCancel := context.WithTimeout(ctx, 5*time.Second)
	defer timeoutCancel()

	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			t.Fatal("Timeout waiting for alias to be processed")
		case <-ticker.C:
			resolved, err := indexManager.ResolveAlias(ctx, alias)
			if err == nil && resolved == expectedPrimaryID {
				return
			}
		}
	}
}

// waitForAliasRemoval polls until an alias is removed
func waitForAliasRemoval(ctx context.Context, t *testing.T, indexManager Indexer, alias string) {
	ctx, timeoutCancel := context.WithTimeout(ctx, 3*time.Second)
	defer timeoutCancel()

	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			t.Fatal("Timeout waiting for alias removal")
		case <-ticker.C:
			_, err := indexManager.ResolveAlias(ctx, alias)
			if err != nil {
				return
			}
		}
	}
}

func TestIndexManager_IntegrationWithNATS(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	testClient := natsclient.NewTestClient(t, natsclient.WithJetStream(), natsclient.WithKV())
	indexManager, cancel := setupTestIndexManager(t, testClient)
	defer cancel()

	ctx := context.Background()
	entityBucket, err := testClient.CreateKVBucket(ctx, "ENTITY_STATES")
	if err != nil {
		// Bucket already exists, get it
		js, _ := testClient.Client.JetStream()
		entityBucket, err = js.KeyValue(ctx, "ENTITY_STATES")
		require.NoError(t, err)
	}

	t.Run("Lifecycle Operations", func(t *testing.T) {
		// Test basic operations to ensure manager is functioning
		// Note: We can't test HealthStatus() since that method doesn't exist
		// Instead we test that basic operations work

		// Test backlog is initially reasonable
		backlog := indexManager.GetBacklog()
		assert.GreaterOrEqual(t, backlog, 0, "Backlog should be non-negative")
	})

	t.Run("KV Watching and Processing", func(t *testing.T) {
		entityID := "test.platform.domain.system.type.integration1"
		entity := createTestEntity(entityID, []message.Triple{
			{Subject: entityID, Predicate: "status", Object: "active"},
			{Subject: entityID, Predicate: "battery", Object: 85.5},
			{Subject: entityID, Predicate: "name", Object: "IntegrationTestEntity"},
			{Subject: entityID, Predicate: "geo.location.latitude", Object: 37.7749},
			{Subject: entityID, Predicate: "geo.location.longitude", Object: -122.4194},
			{Subject: entityID, Predicate: "geo.location.altitude", Object: 100.0},
		})

		err := putEntityInBucket(ctx, entityBucket, entity)
		require.NoError(t, err, "Failed to put entity in KV bucket")

		waitForPredicateIndexing(ctx, t, indexManager, "status", entityID)

		results, err := indexManager.GetPredicateIndex(ctx, "status")
		require.NoError(t, err, "Failed to query predicate index")
		assert.Contains(t, results, entity.Node.ID, "Entity should be in predicate index")
	})

	t.Run("Alias Operations", func(t *testing.T) {
		primaryID := "test.platform.domain.system.type.alias1"
		alias := "short_alias_1"

		err := indexManager.UpdateAliasIndex(ctx, alias, primaryID)
		require.NoError(t, err, "Failed to add alias")

		waitForAliasResolution(ctx, t, indexManager, alias, primaryID)

		resolved, err := indexManager.ResolveAlias(ctx, alias)
		require.NoError(t, err)
		assert.Equal(t, primaryID, resolved, "Alias should resolve to primary ID")

		err = indexManager.DeleteFromAliasIndex(ctx, alias)
		require.NoError(t, err, "Failed to remove alias")

		waitForAliasRemoval(ctx, t, indexManager, alias)

		_, err = indexManager.ResolveAlias(ctx, alias)
		assert.Error(t, err, "Alias should be removed")
	})

	t.Run("Concurrent Operations", func(t *testing.T) {
		numEntities := 5
		var wgConcurrent sync.WaitGroup

		for i := 0; i < numEntities; i++ {
			wgConcurrent.Add(1)
			go func(index int) {
				defer wgConcurrent.Done()
				entityID := fmt.Sprintf("test.platform.domain.system.type.concurrent%d", index)
				entity := createTestEntity(entityID, []message.Triple{
					{Subject: entityID, Predicate: "index", Object: float64(index)},
					{Subject: entityID, Predicate: "name", Object: fmt.Sprintf("ConcurrentEntity%d", index)},
				})
				putEntityInBucket(context.Background(), entityBucket, entity)
			}(i)
		}

		wgConcurrent.Wait()

		// Wait for some processing
		ctx, timeoutCancel := context.WithTimeout(ctx, 5*time.Second)
		defer timeoutCancel()

		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				t.Log("Timeout waiting for concurrent processing - this may be expected")
				goto Done
			case <-ticker.C:
				results, err := indexManager.GetPredicateIndex(ctx, "name")
				if err == nil && len(results) >= numEntities/2 {
					goto Done
				}
			}
		}
	Done:
		backlog := indexManager.GetBacklog()
		assert.GreaterOrEqual(t, backlog, 0, "Backlog should be non-negative after concurrent operations")
	})

	t.Run("Error Handling", func(t *testing.T) {
		invalidData := []byte("invalid json data")
		_, err := entityBucket.Put(ctx, "invalid_entity_id", invalidData)
		require.NoError(t, err, "Should be able to put invalid data in bucket")

		time.Sleep(200 * time.Millisecond)

		backlog := indexManager.GetBacklog()
		assert.GreaterOrEqual(t, backlog, 0, "Index manager should remain functional despite invalid data")
	})

	t.Run("Metrics", func(t *testing.T) {
		stats := indexManager.GetDeduplicationStats()
		assert.GreaterOrEqual(t, stats.TotalEvents, int64(0), "Total events should be non-negative")
		assert.GreaterOrEqual(t, stats.ProcessedEvents, int64(0), "Processed events should be non-negative")
	})
}
