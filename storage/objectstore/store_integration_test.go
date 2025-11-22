//go:build integration

package objectstore_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360/semstreams/metric"
	"github.com/c360/semstreams/natsclient"
	"github.com/c360/semstreams/pkg/cache"
	"github.com/c360/semstreams/storage/objectstore"
	dto "github.com/prometheus/client_model/go"
)

// Package-level shared test client to avoid Docker resource exhaustion
var (
	sharedTestClient *natsclient.TestClient
	sharedNATSClient *natsclient.Client
)

// TestMain sets up a single shared NATS container for all objectstore tests
func TestMain(m *testing.M) {
	if os.Getenv("INTEGRATION_TESTS") != "" {
		// Create a single shared test client for integration tests
		testClient, err := natsclient.NewSharedTestClient(
			natsclient.WithJetStream(),
			natsclient.WithTestTimeout(5*time.Second),
			natsclient.WithStartTimeout(30*time.Second),
		)
		if err != nil {
			panic("Failed to create shared test client: " + err.Error())
		}

		sharedTestClient = testClient
		sharedNATSClient = testClient.Client
	}

	// Run all tests
	exitCode := m.Run()

	// Cleanup integration test resources if they were created
	if sharedTestClient != nil {
		sharedTestClient.Terminate()
	}

	os.Exit(exitCode)
}

// getSharedNATSClient returns the shared NATS client for integration tests
func getSharedNATSClient(t *testing.T) *natsclient.Client {
	if os.Getenv("INTEGRATION_TESTS") == "" {
		t.Skip("Skipping integration test. Set INTEGRATION_TESTS=1 to run.")
	}
	if sharedNATSClient == nil {
		t.Fatal("Shared NATS client not initialized - TestMain should have created it")
	}
	return sharedNATSClient
}

// TestIntegration_StoreAndGet tests basic store and get operations
func TestIntegration_StoreAndGet(t *testing.T) {
	natsClient := getSharedNATSClient(t)

	config := objectstore.Config{
		BucketName: "TEST_MESSAGES",
	}

	ctx := context.Background()
	store, err := objectstore.NewStoreWithConfig(ctx, natsClient, config)
	require.NoError(t, err)
	require.NotNil(t, store)
	defer store.Close()

	// Store a message
	testData := map[string]any{
		"sensor_id":   "temp-001",
		"temperature": 23.5,
		"unit":        "celsius",
	}

	key, err := store.Store(ctx, testData)
	require.NoError(t, err)
	assert.NotEmpty(t, key)

	// Retrieve the message
	data, err := store.Get(ctx, key)
	require.NoError(t, err)
	assert.NotNil(t, data)

	// Unmarshal and verify content
	var retrieved map[string]any
	err = json.Unmarshal(data, &retrieved)
	require.NoError(t, err)
	assert.Equal(t, "temp-001", retrieved["sensor_id"])
	assert.Equal(t, 23.5, retrieved["temperature"])
}

// TestIntegration_PutAndGet tests low-level Put/Get operations
func TestIntegration_PutAndGet(t *testing.T) {
	natsClient := getSharedNATSClient(t)

	config := objectstore.Config{
		BucketName: "TEST_MESSAGES",
	}

	ctx := context.Background()
	store, err := objectstore.NewStoreWithConfig(ctx, natsClient, config)
	require.NoError(t, err)
	require.NotNil(t, store)
	defer store.Close()

	// Put raw data
	key := "test/manual/key"
	testData := []byte(`{"value": "test"}`)

	err = store.Put(ctx, key, testData)
	require.NoError(t, err)

	// Get the data back
	data, err := store.Get(ctx, key)
	require.NoError(t, err)
	assert.Equal(t, testData, data)
}

// TestIntegration_List tests listing keys by prefix
func TestIntegration_List(t *testing.T) {
	natsClient := getSharedNATSClient(t)

	config := objectstore.Config{
		BucketName: "TEST_MESSAGES",
	}

	ctx := context.Background()
	store, err := objectstore.NewStoreWithConfig(ctx, natsClient, config)
	require.NoError(t, err)
	require.NotNil(t, store)
	defer store.Close()

	// Store multiple messages with a common prefix
	prefix := "test/list"
	for i := 0; i < 3; i++ {
		key := prefix + "/" + string(rune('a'+i))
		err = store.Put(ctx, key, []byte(`{"index": `+string(rune('0'+i))+`}`))
		require.NoError(t, err)
	}

	// List keys with the prefix
	keys, err := store.List(ctx, prefix)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(keys), 3, "Should have at least 3 keys with the prefix")
}

// TestIntegration_Delete tests deleting stored objects
func TestIntegration_Delete(t *testing.T) {
	natsClient := getSharedNATSClient(t)

	config := objectstore.Config{
		BucketName: "TEST_MESSAGES",
	}

	ctx := context.Background()
	store, err := objectstore.NewStoreWithConfig(ctx, natsClient, config)
	require.NoError(t, err)
	require.NotNil(t, store)
	defer store.Close()

	// Store a message
	key := "test/delete/key"
	err = store.Put(ctx, key, []byte(`{"temp": "data"}`))
	require.NoError(t, err)

	// Verify it exists
	_, err = store.Get(ctx, key)
	require.NoError(t, err)

	// Delete it
	err = store.Delete(ctx, key)
	require.NoError(t, err)

	// Verify it's gone
	_, err = store.Get(ctx, key)
	assert.Error(t, err, "Should get an error when retrieving deleted object")
}

// TestIntegration_Caching tests that caching works correctly
func TestIntegration_Caching(t *testing.T) {
	natsClient := getSharedNATSClient(t)

	config := objectstore.Config{
		BucketName: "TEST_MESSAGES",
		DataCache: cache.Config{
			Enabled:         true,
			Strategy:        "hybrid",
			MaxSize:         100,
			TTL:             60 * time.Second,
			CleanupInterval: 10 * time.Second,
		},
	}

	ctx := context.Background()
	store, err := objectstore.NewStoreWithConfig(ctx, natsClient, config)
	require.NoError(t, err)
	require.NotNil(t, store)
	defer store.Close()

	// Store a message
	testData := []byte(`{"cached": true}`)
	key, err := store.Store(ctx, testData)
	require.NoError(t, err)

	// First Get - will cache the result
	data1, err := store.Get(ctx, key)
	require.NoError(t, err)

	// Second Get - should come from cache
	data2, err := store.Get(ctx, key)
	require.NoError(t, err)

	// Both should be equal
	assert.Equal(t, data1, data2)
}

// TestIntegration_Metrics verifies that ObjectStore operations are properly instrumented
func TestIntegration_Metrics(t *testing.T) {
	natsClient := getSharedNATSClient(t)

	// Create metrics registry
	metricsRegistry := metric.NewMetricsRegistry()

	config := objectstore.Config{
		BucketName: "TEST_METRICS",
		DataCache: cache.Config{
			Enabled:         true,
			Strategy:        "lru",
			MaxSize:         100,
			TTL:             60 * time.Second,
			CleanupInterval: 10 * time.Second,
		},
	}

	ctx := context.Background()
	store, err := objectstore.NewStoreWithConfigAndMetrics(ctx, natsClient, config, metricsRegistry)
	require.NoError(t, err)
	require.NotNil(t, store)
	defer store.Close()

	// Perform operations to generate metrics

	// 1. Store operation (write)
	testData := map[string]string{"test": "data"}
	key, err := store.Store(ctx, testData)
	require.NoError(t, err)

	// 2. Put operation (write)
	putData := []byte("put test data")
	err = store.Put(ctx, "test-key", putData)
	require.NoError(t, err)

	// 3. Get operation - cache HIT (read) - Store() already cached the data
	_, err = store.Get(ctx, key)
	require.NoError(t, err)

	// 4. Get operation - cache HIT (read) - second access
	_, err = store.Get(ctx, key)
	require.NoError(t, err)

	// 5. Get operation - cache MISS for non-existent key (read + error)
	_, err = store.Get(ctx, "non-existent-key")
	assert.Error(t, err) // Expected error

	// 6. GetMetadata operation (read)
	_, err = store.GetMetadata(ctx, key)
	require.NoError(t, err)

	// 7. List operation
	keys, err := store.List(ctx, "")
	require.NoError(t, err)
	assert.NotEmpty(t, keys)

	// 8. Delete operation
	err = store.Delete(ctx, "test-key")
	require.NoError(t, err)

	// Gather metrics from registry
	metricFamilies, err := metricsRegistry.PrometheusRegistry().Gather()
	require.NoError(t, err)

	// Build metric lookup map
	metricsByName := make(map[string]*dto.MetricFamily)
	for _, mf := range metricFamilies {
		metricsByName[*mf.Name] = mf
	}

	// Verify write operations
	writeOps := metricsByName["semstreams_objectstore_write_operations_total"]
	require.NotNil(t, writeOps, "write operations metric should exist")
	// Should have 2 write ops: 1 store + 1 put
	var totalWrites float64
	for _, m := range writeOps.Metric {
		totalWrites += *m.Counter.Value
	}
	assert.Equal(t, float64(2), totalWrites, "should have 2 write operations")

	// Verify read operations
	readOps := metricsByName["semstreams_objectstore_read_operations_total"]
	require.NotNil(t, readOps, "read operations metric should exist")
	// Should have 4 read ops: 2 get + 1 get error + 1 get_metadata
	var totalReads float64
	for _, m := range readOps.Metric {
		totalReads += *m.Counter.Value
	}
	assert.Equal(t, float64(4), totalReads, "should have 4 read operations")

	// Verify delete operations
	deleteOps := metricsByName["semstreams_objectstore_delete_operations_total"]
	require.NotNil(t, deleteOps, "delete operations metric should exist")
	assert.Equal(t, float64(1), *deleteOps.Metric[0].Counter.Value, "should have 1 delete operation")

	// Verify list operations
	listOps := metricsByName["semstreams_objectstore_list_operations_total"]
	require.NotNil(t, listOps, "list operations metric should exist")
	assert.Equal(t, float64(1), *listOps.Metric[0].Counter.Value, "should have 1 list operation")

	// Verify cache hits
	cacheHits := metricsByName["semstreams_objectstore_cache_hits_total"]
	require.NotNil(t, cacheHits, "cache hits metric should exist")
	// 2 hits: first and second Get() of stored key (Store() already cached it)
	assert.Equal(t, float64(2), *cacheHits.Metric[0].Counter.Value, "should have 2 cache hits")

	// Verify cache misses
	cacheMisses := metricsByName["semstreams_objectstore_cache_misses_total"]
	require.NotNil(t, cacheMisses, "cache misses metric should exist")
	// 1 miss: Get() of non-existent key
	assert.Equal(t, float64(1), *cacheMisses.Metric[0].Counter.Value, "should have 1 cache miss")

	// Verify errors
	errors := metricsByName["semstreams_objectstore_operation_errors_total"]
	require.NotNil(t, errors, "errors metric should exist")
	assert.Equal(t, float64(1), *errors.Metric[0].Counter.Value, "should have 1 error from non-existent key")

	// Verify latency histograms exist
	readLatency := metricsByName["semstreams_objectstore_read_duration_seconds"]
	require.NotNil(t, readLatency, "read latency metric should exist")

	writeLatency := metricsByName["semstreams_objectstore_write_duration_seconds"]
	require.NotNil(t, writeLatency, "write latency metric should exist")

	deleteLatency := metricsByName["semstreams_objectstore_delete_duration_seconds"]
	require.NotNil(t, deleteLatency, "delete latency metric should exist")

	listLatency := metricsByName["semstreams_objectstore_list_duration_seconds"]
	require.NotNil(t, listLatency, "list latency metric should exist")

	// Verify bucket label
	assert.Equal(t, "TEST_METRICS", *writeOps.Metric[0].Label[0].Value, "should have correct bucket label")
}
