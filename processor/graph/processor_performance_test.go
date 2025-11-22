package graph

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"

	gtypes "github.com/c360/semstreams/graph"
	"github.com/c360/semstreams/message"
	"github.com/c360/semstreams/metric"
	"github.com/c360/semstreams/natsclient"
	"github.com/c360/semstreams/processor/graph/datamanager"
	"github.com/c360/semstreams/processor/graph/querymanager"
	"github.com/c360/semstreams/storage/objectstore"
)

// setupPerformanceTestProcessor creates and starts a processor configured for performance testing
func setupPerformanceTestProcessor(ctx context.Context, t *testing.T, testID string, natsClient *natsclient.Client) (*Processor, jetstream.KeyValue) {
	config := &Config{
		Workers:      5,
		QueueSize:    1000,
		InputSubject: fmt.Sprintf("test.%s.*.events", testID),
		DataManager: func() *datamanager.Config {
			config := datamanager.DefaultConfig()
			return &config
		}(),
		Querier: func() *querymanager.Config {
			config := querymanager.Config{}
			config.SetDefaults()
			return &config
		}(),
	}

	deps := ProcessorDeps{
		Config:          config,
		NATSClient:      natsClient,
		MetricsRegistry: metric.NewMetricsRegistry(),
		Logger:          slog.Default(),
	}

	processor, err := NewProcessor(deps)
	require.NoError(t, err)
	require.NotNil(t, processor)
	require.NoError(t, processor.Initialize())

	// Start processor in goroutine
	startErr := make(chan error, 1)
	go func() {
		startErr <- processor.Start(ctx)
	}()

	// Wait for processor to be ready
	require.Eventually(t, func() bool {
		return processor.IsReady()
	}, 10*time.Second, 100*time.Millisecond, "Processor should be ready")

	time.Sleep(100 * time.Millisecond)

	// Get bucket for verifying results
	entityBucket, err := natsClient.GetKeyValueBucket(ctx, "ENTITY_STATES")
	require.NoError(t, err)

	return processor, entityBucket
}

// publishRapidUpdates sends multiple rapid updates to test write batching
func publishRapidUpdates(ctx context.Context, t *testing.T, natsClient *natsclient.Client, testID, entityID string, count int) {
	for i := 0; i < count; i++ {
		triples := []map[string]interface{}{
			{
				"subject":   entityID,
				"predicate": "system:battery_level",
				"object":    80 + i,
			},
		}

		if i%2 == 0 {
			triples = append(triples, map[string]interface{}{
				"subject":   entityID,
				"predicate": fmt.Sprintf("test.property.%d", i),
				"object":    fmt.Sprintf("value_%d", i),
			})
		}

		testPayload := &TestGraphablePayload{
			ID:         entityID,
			Properties: make(map[string]interface{}),
			TripleData: triples,
		}

		storageRef := &message.StorageReference{
			StorageInstance: "objectstore-primary",
			Key:             fmt.Sprintf("storage/test/msg-%d", i),
			ContentType:     "application/json",
			Size:            1024,
		}

		storedMsg := objectstore.NewStoredMessage(testPayload, storageRef, "test.rapid.update")
		wrappedMsg := message.NewBaseMessage(storedMsg.Schema(), storedMsg, "objectstore-primary")

		data, err := json.Marshal(wrappedMsg)
		require.NoError(t, err)
		err = natsClient.Publish(ctx, fmt.Sprintf("test.%s.robotics.events", testID), data)
		require.NoError(t, err)

		time.Sleep(2 * time.Millisecond)
	}
}

// kvWriteCounter tracks KV writes from a watcher
type kvWriteCounter struct {
	count      int
	stopWatch  jetstream.KeyWatcher
	writeDone  chan struct{}
	countMutex sync.Mutex
}

// startKVWriteCounter starts monitoring KV bucket writes
func startKVWriteCounter(ctx context.Context, t *testing.T, entityBucket jetstream.KeyValue, entityID string) *kvWriteCounter {
	counter := &kvWriteCounter{
		writeDone: make(chan struct{}),
	}

	var err error
	counter.stopWatch, err = entityBucket.Watch(ctx, entityID, jetstream.UpdatesOnly())
	require.NoError(t, err)

	go func() {
		defer close(counter.writeDone)
		for entry := range counter.stopWatch.Updates() {
			if entry != nil {
				counter.countMutex.Lock()
				counter.count++
				count := counter.count
				counter.countMutex.Unlock()
				t.Logf("KV Write #%d: revision=%d", count, entry.Revision())
			}
		}
	}()

	return counter
}

// stop stops the watcher and returns the final count
func (c *kvWriteCounter) stop() int {
	c.stopWatch.Stop()
	<-c.writeDone
	c.countMutex.Lock()
	defer c.countMutex.Unlock()
	return c.count
}

// verifyBatchingReduction validates that writes were coalesced
func verifyBatchingReduction(ctx context.Context, t *testing.T, kvWrites, totalMessages int, entityID string, entityBucket jetstream.KeyValue) {
	t.Logf("Results: %d KV writes for %d updates", kvWrites, totalMessages)

	require.Less(t, kvWrites, 10, "Should coalesce updates (expect <10 writes for 20 updates)")
	require.Greater(t, kvWrites, 0, "Should have at least one KV write")

	// Verify final state
	entry, err := entityBucket.Get(ctx, entityID)
	require.NoError(t, err)

	var entity gtypes.EntityState
	require.NoError(t, json.Unmarshal(entry.Value(), &entity))

	properties := gtypes.GetProperties(&entity)
	t.Logf("Final entity properties: %v", properties)

	batteryLevel, found := gtypes.GetPropertyValue(&entity, "system:battery_level")
	require.True(t, found, "Final entity should have battery level")
	require.Equal(t, float64(99), batteryLevel, "Should have last battery value")

	// Log batching effectiveness
	if kvWrites < totalMessages {
		t.Logf("✅ Batching demonstration: %d messages were coalesced into %d KV writes (%.0f%% reduction)",
			totalMessages, kvWrites, (1-float64(kvWrites)/float64(totalMessages))*100)
	}
}

// createCacheTestMessage creates a test message for cache testing
func createCacheTestMessage(t *testing.T, entityID string, batteryLevel int) []byte {
	testPayload := &TestGraphablePayload{
		ID: entityID,
		Properties: map[string]interface{}{
			"system:battery_level": batteryLevel,
		},
		TripleData: []map[string]interface{}{
			{
				"subject":   entityID,
				"predicate": "system:battery_level",
				"object":    batteryLevel,
			},
		},
	}

	storageRef := &message.StorageReference{
		StorageInstance: "objectstore-primary",
		Key:             "storage/test/cache-msg-123",
		ContentType:     "application/json",
		Size:            1024,
	}

	storedMsg := objectstore.NewStoredMessage(testPayload, storageRef, "test.cache.create")
	wrappedMsg := message.NewBaseMessage(storedMsg.Schema(), storedMsg, "objectstore-primary")

	data, err := json.Marshal(wrappedMsg)
	require.NoError(t, err)
	return data
}

// waitForEntityCreation polls for entity to be created
func waitForEntityCreation(ctx context.Context, t *testing.T, entityStore *datamanager.Manager, entityID string) *gtypes.EntityState {
	var entity *gtypes.EntityState
	var err error
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		entity, err = entityStore.GetEntity(ctx, entityID)
		if err == nil && entity != nil {
			return entity
		}
		if err != nil {
			t.Logf("Polling attempt: %v", err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	require.NoError(t, err, "Final GetEntity should succeed")
	require.NotNil(t, entity, "Entity should exist after message processing")
	return entity
}

// verifyCacheHitRatio tests cache performance with repeated queries
func verifyCacheHitRatio(ctx context.Context, t *testing.T, entityStore *datamanager.Manager, entityID string) {
	stats1 := entityStore.GetCacheStats()
	initialHits := stats1.L1Hits + stats1.L2Hits
	initialMisses := stats1.L1Misses + stats1.L2Misses

	t.Logf("Initial cache stats: L1(h:%d,m:%d) L2(h:%d,m:%d) Total(h:%d,m:%d)",
		stats1.L1Hits, stats1.L1Misses, stats1.L2Hits, stats1.L2Misses,
		stats1.TotalHits, stats1.TotalMisses)

	// Query same entity 50 times
	queryCount := 0
	for i := 0; i < 50; i++ {
		entity, err := entityStore.GetEntity(ctx, entityID)
		if err != nil {
			t.Logf("Query %d failed: %v", i, err)
			continue
		}
		require.NotNil(t, entity, "Entity should not be nil")
		queryCount++

		if i == 0 {
			batteryLevel, _ := gtypes.GetPropertyValue(entity, "system:battery_level")
			t.Logf("First query result: battery level = %v", batteryLevel)
		}
	}

	t.Logf("Successfully completed %d queries out of 50", queryCount)

	stats2 := entityStore.GetCacheStats()
	hits := (stats2.L1Hits + stats2.L2Hits) - initialHits
	misses := (stats2.L1Misses + stats2.L2Misses) - initialMisses
	total := hits + misses

	t.Logf("Final cache stats: L1(h:%d,m:%d) L2(h:%d,m:%d) Total(h:%d,m:%d)",
		stats2.L1Hits, stats2.L1Misses, stats2.L2Hits, stats2.L2Misses,
		stats2.TotalHits, stats2.TotalMisses)
	t.Logf("Cache performance: %d hits, %d misses, %d total queries", hits, misses, total)

	require.Greater(t, total, int64(30), "Should have processed most queries")

	if total > 0 {
		hitRatio := float64(hits) / float64(total)
		t.Logf("Cache hit ratio: %.2f%% (%.0f hits / %.0f total)",
			hitRatio*100, float64(hits), float64(total))
		require.Greater(t, hitRatio, 0.7, "Cache hit ratio should be >70% for repeated entity queries")
		t.Logf("✅ Multi-tier caching working: %.1f%% hit ratio", hitRatio*100)
	}
}

// TestIntegration_GraphProcessorPerformanceFeatures verifies that the sophisticated buffer
// and cache components are actually being leveraged
func TestIntegration_GraphProcessorPerformanceFeatures(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	natsClient := getSharedNATSClient(t)
	testID := fmt.Sprintf("perf%d", time.Now().UnixNano())

	processor, entityBucket := setupPerformanceTestProcessor(ctx, t, testID, natsClient)

	t.Run("Write_Buffer_Batching_Performance", func(t *testing.T) {
		t.Logf("Flushing any pending writes from previous tests...")
		err := processor.dataLifecycle.FlushPendingWrites(ctx)
		require.NoError(t, err, "Failed to flush pending writes")
		time.Sleep(100 * time.Millisecond)

		pendingCount := processor.dataLifecycle.GetPendingWriteCount()
		require.Equal(t, 0, pendingCount, "Buffer should be empty before test starts")

		entityID := fmt.Sprintf("c360.platform.robotics.system.drone.batch.%d", time.Now().UnixNano())

		counter := startKVWriteCounter(ctx, t, entityBucket, entityID)

		t.Logf("Sending 20 rapid updates within 50ms flush interval...")
		publishRapidUpdates(ctx, t, natsClient, testID, entityID, 20)
		t.Logf("Successfully published 20 messages")

		time.Sleep(100 * time.Millisecond)

		t.Logf("Forcing buffer flush to complete batch processing...")
		err = processor.dataLifecycle.FlushPendingWrites(ctx)
		require.NoError(t, err, "Failed to flush buffer after sending messages")

		t.Logf("Waiting for batch processing and index updates...")
		time.Sleep(200 * time.Millisecond)

		kvWrites := counter.stop()
		verifyBatchingReduction(ctx, t, kvWrites, 20, entityID, entityBucket)
	})

	// Ensure clean state between subtests
	t.Logf("Ensuring clean state between subtests...")
	err := processor.dataLifecycle.FlushPendingWrites(ctx)
	require.NoError(t, err, "Failed to flush between subtests")
	time.Sleep(200 * time.Millisecond)

	require.True(t, processor.IsReady(), "Processor should still be ready between subtests")
	t.Logf("Processor is ready, starting second subtest")

	t.Run("Cache_Hit_Ratio_Performance", func(t *testing.T) {
		entityID := fmt.Sprintf("c360.platform.test.cache.perf.%d", time.Now().UnixNano())

		data := createCacheTestMessage(t, entityID, 95)
		subject := fmt.Sprintf("test.%s.robotics.events", testID)
		err := natsClient.Publish(ctx, subject, data)
		require.NoError(t, err)
		t.Logf("Published message for entity %s", entityID)

		entityStore := processor.dataManager
		require.NotNil(t, entityStore, "EntityStore should be available")

		time.Sleep(300 * time.Millisecond)

		_ = waitForEntityCreation(ctx, t, entityStore, entityID)
		verifyCacheHitRatio(ctx, t, entityStore, entityID)
	})

	t.Logf("🎉 Performance features verified: Write batching + Multi-tier caching working!")

	cancel()

	// Wait for Start to return - need to capture startErr from setup
	time.Sleep(1 * time.Second)
}
