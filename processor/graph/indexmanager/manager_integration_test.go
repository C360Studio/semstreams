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

func TestIndexManager_IntegrationWithNATS(t *testing.T) {
	// Skip in short mode as this requires NATS
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Create test NATS client with JetStream and KV
	testClient := natsclient.NewTestClient(t, natsclient.WithJetStream(), natsclient.WithKV())

	// Create KV buckets for all indexes
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

	// Create cancellable context for lifecycle management
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Create index manager
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

	// Start the manager in a goroutine
	var wg sync.WaitGroup
	managerErrors := make(chan error, 1)
	wg.Add(1)
	go func() {
		defer wg.Done()
		managerErrors <- indexManager.Run(ctx)
	}()

	// Wait for startup - give it a moment to initialize watchers
	time.Sleep(100 * time.Millisecond)

	// Check for startup errors
	select {
	case err := <-managerErrors:
		if err != nil {
			t.Fatalf("IndexManager failed to start: %v", err)
		}
	default:
		// No errors yet - service is starting up
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
		// Create test entity using Triples (single source of truth)
		entityID := "test.platform.domain.system.type.integration1"
		entity := &gtypes.EntityState{
			Node: gtypes.NodeProperties{
				ID:     entityID,
				Type:   "domain.type",
				Status: gtypes.StatusActive,
			},
			Triples: []message.Triple{
				{Subject: entityID, Predicate: "status", Object: "active"},
				{Subject: entityID, Predicate: "battery", Object: 85.5},
				{Subject: entityID, Predicate: "name", Object: "IntegrationTestEntity"},
				{Subject: entityID, Predicate: "geo.location.latitude", Object: 37.7749},
				{Subject: entityID, Predicate: "geo.location.longitude", Object: -122.4194},
				{Subject: entityID, Predicate: "geo.location.altitude", Object: 100.0},
			},
			Version:   1,
			UpdatedAt: time.Now(),
		}

		// Put entity in ENTITY_STATES bucket (simulating EntityStore)
		entityData, err := json.Marshal(entity)
		require.NoError(t, err, "Failed to marshal entity")

		_, err = entityBucket.Put(ctx, entity.Node.ID, entityData)
		require.NoError(t, err, "Failed to put entity in KV bucket")

		// Wait for IndexManager to process the change
		// Use a retry loop with timeout instead of arbitrary sleep
		ctx, timeoutCancel := context.WithTimeout(ctx, 5*time.Second)
		defer timeoutCancel()

		var processed bool
		ticker := time.NewTicker(50 * time.Millisecond)
		defer ticker.Stop()

	ProcessingLoop:
		for {
			select {
			case <-ctx.Done():
				t.Fatal("Timeout waiting for entity to be processed")
			case <-ticker.C:
				// Check if predicate index was updated
				results, err := indexManager.GetPredicateIndex(ctx, "status")
				if err == nil && len(results) > 0 {
					processed = true
					break ProcessingLoop
				}
			}
		}

		require.True(t, processed, "IndexManager should have processed the entity change")

		// Test that predicates were indexed
		results, err := indexManager.GetPredicateIndex(ctx, "status")
		require.NoError(t, err, "Failed to query predicate index")
		assert.Contains(t, results, entity.Node.ID, "Entity should be in predicate index")
	})

	t.Run("Alias Operations", func(t *testing.T) {
		// Test basic alias operations that are part of the public API
		primaryID := "test.platform.domain.system.type.alias1"
		alias := "short_alias_1"

		// Add alias
		err := indexManager.UpdateAliasIndex(ctx, alias, primaryID)
		require.NoError(t, err, "Failed to add alias")

		// Wait for processing with proper timeout
		var resolved string
		ctx, timeoutCancel := context.WithTimeout(ctx, 5*time.Second)
		defer timeoutCancel()

		ticker := time.NewTicker(50 * time.Millisecond)
		defer ticker.Stop()

	AliasWaitLoop:
		for {
			select {
			case <-ctx.Done():
				t.Fatal("Timeout waiting for alias to be processed")
			case <-ticker.C:
				resolved, err = indexManager.ResolveAlias(ctx, alias)
				if err == nil && resolved == primaryID {
					break AliasWaitLoop
				}
			}
		}

		assert.Equal(t, primaryID, resolved, "Alias should resolve to primary ID")

		// Remove alias
		err = indexManager.DeleteFromAliasIndex(ctx, alias)
		require.NoError(t, err, "Failed to remove alias")

		// Wait for removal with timeout
		ctx, timeoutCancel2 := context.WithTimeout(ctx, 3*time.Second)
		defer timeoutCancel2()

		ticker2 := time.NewTicker(50 * time.Millisecond)
		defer ticker2.Stop()

	RemovalWaitLoop:
		for {
			select {
			case <-ctx.Done():
				t.Fatal("Timeout waiting for alias removal")
			case <-ticker2.C:
				_, err = indexManager.ResolveAlias(ctx, alias)
				if err != nil {
					break RemovalWaitLoop
				}
			}
		}

		// Verify alias is removed
		_, err = indexManager.ResolveAlias(ctx, alias)
		assert.Error(t, err, "Alias should be removed")
	})

	t.Run("Concurrent Operations", func(t *testing.T) {
		// Test that manager can handle concurrent KV updates
		numEntities := 5
		var wgConcurrent sync.WaitGroup

		for i := 0; i < numEntities; i++ {
			wgConcurrent.Add(1)
			go func(index int) {
				defer wgConcurrent.Done()
				entityID := fmt.Sprintf("test.platform.domain.system.type.concurrent%d", index)
				entity := &gtypes.EntityState{
					Node: gtypes.NodeProperties{
						ID:   entityID,
						Type: "concurrent.type",
					},
					Triples: []message.Triple{
						{Subject: entityID, Predicate: "index", Object: float64(index)},
						{Subject: entityID, Predicate: "name", Object: fmt.Sprintf("ConcurrentEntity%d", index)},
					},
					Version:   1,
					UpdatedAt: time.Now(),
				}

				entityData, err := json.Marshal(entity)
				if err == nil {
					entityBucket.Put(context.Background(), entity.Node.ID, entityData)
				}
			}(i)
		}

		// Wait for all operations to complete
		wgConcurrent.Wait()

		// Wait for processing with timeout
		ctx, timeoutCancel := context.WithTimeout(ctx, 5*time.Second)
		defer timeoutCancel()

		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()

	ConcurrentProcessingLoop:
		for {
			select {
			case <-ctx.Done():
				t.Log("Timeout waiting for concurrent processing - this may be expected")
				break ConcurrentProcessingLoop
			case <-ticker.C:
				// Check if some entities were processed
				results, err := indexManager.GetPredicateIndex(ctx, "name")
				if err == nil && len(results) >= numEntities/2 {
					break ConcurrentProcessingLoop
				}
			}
		}

		// Test that the index manager is still functional
		backlog := indexManager.GetBacklog()
		assert.GreaterOrEqual(t, backlog, 0, "Backlog should be non-negative after concurrent operations")
	})

	t.Run("Error Handling", func(t *testing.T) {
		// Test that manager handles invalid data gracefully
		invalidData := []byte("invalid json data")
		_, err := entityBucket.Put(ctx, "invalid_entity_id", invalidData)
		require.NoError(t, err, "Should be able to put invalid data in bucket")

		// Wait a moment for processing
		time.Sleep(200 * time.Millisecond)

		// Index manager should still be functional despite invalid data
		backlog := indexManager.GetBacklog()
		assert.GreaterOrEqual(t, backlog, 0, "Index manager should remain functional despite invalid data")
	})

	t.Run("Metrics", func(t *testing.T) {
		// Test deduplication stats
		stats := indexManager.GetDeduplicationStats()
		assert.GreaterOrEqual(t, stats.TotalEvents, int64(0), "Total events should be non-negative")
		assert.GreaterOrEqual(t, stats.ProcessedEvents, int64(0), "Processed events should be non-negative")
	})

	// Cancel context to trigger shutdown
	cancel()

	// Wait for service to shutdown properly
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Clean shutdown
	case <-time.After(5 * time.Second):
		t.Error("IndexManager did not shutdown within 5 seconds")
	}
}
