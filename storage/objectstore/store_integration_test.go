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

	"github.com/c360studio/semstreams/examples/processors/document"
	"github.com/c360studio/semstreams/internal/semantictest"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/metric"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/payloadbuiltins"
	"github.com/c360studio/semstreams/pkg/cache"
	"github.com/c360studio/semstreams/storage/objectstore"
	"github.com/c360studio/semstreams/types"
	dto "github.com/prometheus/client_model/go"
)

// Package-level shared test client to avoid Docker resource exhaustion
var (
	sharedTestClient *natsclient.TestClient
	sharedNATSClient *natsclient.Client
)

// TestMain sets up a single shared NATS container for all objectstore tests
func TestMain(m *testing.M) {
	// Create a single shared test client for integration tests
	// Build tag ensures this only runs with -tags=integration
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

	// Run all tests
	exitCode := m.Run()

	// Cleanup integration test resources
	sharedTestClient.Terminate()

	os.Exit(exitCode)
}

// getSharedNATSClient returns the shared NATS client for integration tests
func getSharedNATSClient(t *testing.T) *natsclient.Client {
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

// TestIntegration_ByteSliceNotDoubleEncoded verifies []byte inputs are stored directly
// without being re-marshaled to base64. This prevents data corruption when JSON bytes
// from NATS messages are stored.
func TestIntegration_ByteSliceNotDoubleEncoded(t *testing.T) {
	natsClient := getSharedNATSClient(t)

	config := objectstore.Config{
		BucketName: "TEST_BYTE_SLICE",
	}

	ctx := context.Background()
	store, err := objectstore.NewStoreWithConfig(ctx, natsClient, config)
	require.NoError(t, err)
	defer store.Close()

	// Given: valid JSON as []byte (simulating NATS message data)
	input := []byte(`{"type":"document","title":"Safety Manual","entity_id":"doc-123"}`)

	// When: stored and retrieved
	key, err := store.Store(ctx, input)
	require.NoError(t, err)
	assert.NotEmpty(t, key)

	retrieved, err := store.Get(ctx, key)
	require.NoError(t, err)

	// Then: data is identical (not base64 encoded)
	assert.Equal(t, input, retrieved, "stored bytes should match input exactly - no base64 encoding")

	// Verify it's still valid JSON that can be unmarshaled
	var parsed map[string]any
	err = json.Unmarshal(retrieved, &parsed)
	require.NoError(t, err)
	assert.Equal(t, "Safety Manual", parsed["title"])
	assert.Equal(t, "doc-123", parsed["entity_id"])
}

// TestIntegration_RawMessageNotDoubleEncoded verifies json.RawMessage is stored directly
func TestIntegration_RawMessageNotDoubleEncoded(t *testing.T) {
	natsClient := getSharedNATSClient(t)

	config := objectstore.Config{
		BucketName: "TEST_RAW_MESSAGE",
	}

	ctx := context.Background()
	store, err := objectstore.NewStoreWithConfig(ctx, natsClient, config)
	require.NoError(t, err)
	defer store.Close()

	// Given: valid JSON as json.RawMessage
	input := json.RawMessage(`{"entity_id":"entity-456","properties":{"name":"Test"}}`)

	// When: stored and retrieved
	key, err := store.Store(ctx, input)
	require.NoError(t, err)
	assert.NotEmpty(t, key)

	retrieved, err := store.Get(ctx, key)
	require.NoError(t, err)

	// Then: data is identical (not base64 encoded)
	assert.Equal(t, []byte(input), retrieved, "stored json.RawMessage should match input exactly")

	// Verify it's still valid JSON
	var parsed map[string]any
	err = json.Unmarshal(retrieved, &parsed)
	require.NoError(t, err)
	assert.Equal(t, "entity-456", parsed["entity_id"])
}

// TestIntegration_BaseMessageRoundTrip verifies the full flow:
// 1. Create BaseMessage with Document payload
// 2. Marshal to []byte (simulating NATS transport)
// 3. Store via Store(ctx, []byte)
// 4. Fetch via FetchContent()
// 5. Verify BaseMessage can be parsed and payload extracted
//
// This test validates the fix for the double-encoding bug that was preventing
// embeddings from being generated.
func TestIntegration_BaseMessageRoundTrip(t *testing.T) {
	natsClient := getSharedNATSClient(t)

	config := objectstore.Config{
		BucketName: "TEST_BASE_MESSAGE",
	}

	ctx := context.Background()
	store, err := objectstore.NewStoreWithConfig(ctx, natsClient, config)
	require.NoError(t, err)
	defer store.Close()

	// 1. Create a Document payload (implements ContentStorable)
	doc := &document.Document{
		ID:          "doc-test-001",
		Title:       "Safety Manual",
		Description: "A comprehensive guide to workplace safety",
		Body:        "This document covers all safety procedures...",
		Category:    "safety",
		EntityIDValue: document.MintDocumentEntityID(
			types.PlatformMeta{Org: "test-org", Platform: "test-dep"}, "safety", "doc-test-001"),
	}

	// 2. Create BaseMessage wrapping the Document
	baseMsg := message.NewBaseMessage(doc.Schema(), doc, "test-source")

	// 3. Marshal to []byte (simulating what happens in NATS transport)
	msgBytes, err := baseMsg.MarshalJSON()
	require.NoError(t, err)
	t.Logf("Marshaled BaseMessage: %s", string(msgBytes))

	// 4. Store the bytes (this is what objectstore component does)
	key, err := store.Store(ctx, msgBytes)
	require.NoError(t, err)
	assert.NotEmpty(t, key)

	// 5. Retrieve raw bytes and verify they're not corrupted
	retrieved, err := store.Get(ctx, key)
	require.NoError(t, err)
	assert.Equal(t, msgBytes, retrieved, "stored bytes should match original - no base64 encoding")

	// 6. Verify we can unmarshal the retrieved data back to BaseMessage.
	// The test stores a content.document.v1 payload, which lives in the
	// example processors and is NOT in payloadbuiltins.Register —
	// register it explicitly into a per-test registry.
	reg := payloadbuiltins.NewTestRegistry(t)
	require.NoError(t, document.RegisterPayloads(reg))
	dec := message.NewDecoder(reg)
	parsedMsg, err := dec.Decode(retrieved)
	require.NoError(t, err, "should be able to unmarshal stored data as BaseMessage")

	// 7. Verify payload was preserved correctly
	payload := parsedMsg.Payload()
	require.NotNil(t, payload, "payload should not be nil")

	// 8. Verify it's a ContentStorable with the expected content fields
	contentStorable, ok := payload.(message.ContentStorable)
	require.True(t, ok, "payload should implement ContentStorable")

	contentFields := contentStorable.ContentFields()
	assert.Contains(t, contentFields, message.ContentRoleTitle)
	assert.Contains(t, contentFields, message.ContentRoleBody)
	assert.Contains(t, contentFields, message.ContentRoleAbstract)

	// 9. Verify we can use FetchContent to get the stored content
	// First store using StoreContent to get a proper StorageReference
	storageRef, err := store.StoreContent(ctx, contentStorable)
	require.NoError(t, err)

	// Fetch the content back
	storedContent, err := store.FetchContent(ctx, storageRef)
	require.NoError(t, err)
	assert.Equal(t, contentStorable.EntityID(), storedContent.EntityID)
	assert.NotEmpty(t, storedContent.Fields)
}

// TestIntegration_StructStillMarshaledCorrectly verifies structs are still properly marshaled
func TestIntegration_StructStillMarshaledCorrectly(t *testing.T) {
	natsClient := getSharedNATSClient(t)

	config := objectstore.Config{
		BucketName: "TEST_STRUCT_MARSHAL",
	}

	ctx := context.Background()
	store, err := objectstore.NewStoreWithConfig(ctx, natsClient, config)
	require.NoError(t, err)
	defer store.Close()

	// Given: a struct (not []byte)
	input := struct {
		Type     string `json:"type"`
		EntityID string `json:"entity_id"`
		Value    int    `json:"value"`
	}{
		Type:     "sensor",
		EntityID: semantictest.EntityID(t, "test", "objectstore", "opaque", "json", "sensor", "789"),
		Value:    42,
	}

	// When: stored and retrieved
	key, err := store.Store(ctx, input)
	require.NoError(t, err)

	retrieved, err := store.Get(ctx, key)
	require.NoError(t, err)

	// Then: struct was properly marshaled to JSON
	var parsed map[string]any
	err = json.Unmarshal(retrieved, &parsed)
	require.NoError(t, err)
	assert.Equal(t, "sensor", parsed["type"])
	assert.Equal(t, input.EntityID, parsed["entity_id"])
	assert.Equal(t, float64(42), parsed["value"]) // JSON numbers are float64
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

// TestIntegration_BinaryStorable tests storing content with binary data
func TestIntegration_BinaryStorable(t *testing.T) {
	natsClient := getSharedNATSClient(t)

	config := objectstore.Config{
		BucketName: "TEST_BINARY",
	}

	ctx := context.Background()
	store, err := objectstore.NewStoreWithConfig(ctx, natsClient, config)
	require.NoError(t, err)
	defer store.Close()

	// Create a BinaryStorable implementation
	entityID := semantictest.EntityID(t, "acme", "ops", "docs", "objectstore", "binary", "001")
	binaryDoc := &testBinaryDocument{
		id:          entityID,
		title:       "Video Tutorial",
		description: "How to use the system",
		videoData:   []byte("fake video data - would be MP4 bytes"),
		imageData:   []byte{0x89, 0x50, 0x4E, 0x47}, // PNG magic bytes
		triples: []message.Triple{{
			Subject: entityID, Predicate: semantictest.Predicate(t, "document", "metadata", "title"), Object: "Video Tutorial",
		}},
	}

	// Store content with binary
	ref, err := store.StoreContent(ctx, binaryDoc)
	require.NoError(t, err)
	assert.NotEmpty(t, ref.Key)

	// Fetch the content metadata
	storedContent, err := store.FetchContent(ctx, ref)
	require.NoError(t, err)

	// Verify text fields
	assert.Equal(t, entityID, storedContent.EntityID)
	assert.Equal(t, "Video Tutorial", storedContent.Fields["title"])
	assert.Equal(t, "How to use the system", storedContent.Fields["description"])

	// Verify binary references exist
	assert.True(t, storedContent.HasBinaryContent(), "should have binary content")
	assert.Len(t, storedContent.BinaryRefs, 2, "should have 2 binary refs")

	// Verify video reference (media role maps to "video" field)
	videoRef := storedContent.GetBinaryRefByRole(message.ContentRoleMedia)
	require.NotNil(t, videoRef, "should have video ref via media role")
	assert.Equal(t, "video/mp4", videoRef.ContentType)
	assert.Equal(t, int64(len(binaryDoc.videoData)), videoRef.Size)
	assert.Contains(t, videoRef.Key, "binary/")

	// Verify thumbnail reference (thumbnail role maps to "thumbnail" field)
	thumbRef := storedContent.GetBinaryRefByRole(message.ContentRoleThumbnail)
	require.NotNil(t, thumbRef, "should have thumbnail ref via thumbnail role")
	assert.Equal(t, "image/png", thumbRef.ContentType)
	assert.Equal(t, int64(len(binaryDoc.imageData)), thumbRef.Size)

	// Fetch actual binary data
	videoBytes, err := store.FetchBinary(ctx, *videoRef)
	require.NoError(t, err)
	assert.Equal(t, binaryDoc.videoData, videoBytes, "video data should match")

	thumbBytes, err := store.FetchBinary(ctx, *thumbRef)
	require.NoError(t, err)
	assert.Equal(t, binaryDoc.imageData, thumbBytes, "thumbnail data should match")
}

// TestIntegration_BinaryStorable_LargeContent tests storing larger binary content
func TestIntegration_BinaryStorable_LargeContent(t *testing.T) {
	natsClient := getSharedNATSClient(t)

	config := objectstore.Config{
		BucketName: "TEST_BINARY_LARGE",
	}

	ctx := context.Background()
	store, err := objectstore.NewStoreWithConfig(ctx, natsClient, config)
	require.NoError(t, err)
	defer store.Close()

	// Create 1MB of binary data
	largeData := make([]byte, 1024*1024)
	for i := range largeData {
		largeData[i] = byte(i % 256)
	}

	entityID := semantictest.EntityID(t, "acme", "ops", "docs", "objectstore", "binary", "large-001")
	binaryDoc := &testBinaryDocument{
		id:          entityID,
		title:       "Large File",
		description: "Testing large binary storage",
		videoData:   largeData,
		imageData:   nil, // No thumbnail
		triples: []message.Triple{{
			Subject: entityID, Predicate: semantictest.Predicate(t, "document", "metadata", "title"), Object: "Large File",
		}},
	}

	// Store content with large binary
	ref, err := store.StoreContent(ctx, binaryDoc)
	require.NoError(t, err)

	// Fetch and verify
	storedContent, err := store.FetchContent(ctx, ref)
	require.NoError(t, err)

	videoRef := storedContent.GetBinaryRefByRole(message.ContentRoleMedia)
	require.NotNil(t, videoRef, "should have video ref via media role")
	assert.Equal(t, int64(1024*1024), videoRef.Size)

	// Fetch and verify data integrity
	retrieved, err := store.FetchBinary(ctx, *videoRef)
	require.NoError(t, err)
	assert.Equal(t, largeData, retrieved, "large binary data should match exactly")
}

// TestIntegration_ContentStorable_NoBinary verifies backward compatibility
// when ContentStorable doesn't implement BinaryStorable
func TestIntegration_ContentStorable_NoBinary(t *testing.T) {
	natsClient := getSharedNATSClient(t)

	config := objectstore.Config{
		BucketName: "TEST_NO_BINARY",
	}

	ctx := context.Background()
	store, err := objectstore.NewStoreWithConfig(ctx, natsClient, config)
	require.NoError(t, err)
	defer store.Close()

	// Use Document which implements ContentStorable but NOT BinaryStorable
	doc := &document.Document{
		ID:          "doc-no-binary-001",
		Title:       "Text Only Document",
		Description: "This document has no binary content",
		Body:        "Just text content here",
		Category:    "text",
		EntityIDValue: document.MintDocumentEntityID(
			types.PlatformMeta{Org: "test-org", Platform: "test-dep"}, "text", "doc-no-binary-001"),
	}

	// Store content - should work without binary
	ref, err := store.StoreContent(ctx, doc)
	require.NoError(t, err)

	// Fetch and verify
	storedContent, err := store.FetchContent(ctx, ref)
	require.NoError(t, err)

	assert.Equal(t, doc.EntityID(), storedContent.EntityID)
	assert.False(t, storedContent.HasBinaryContent(), "should not have binary content")
	assert.Empty(t, storedContent.BinaryRefs, "binary refs should be empty")

	// Verify text fields still work
	assert.Equal(t, "Text Only Document", storedContent.Fields["title"])
	assert.Equal(t, "This document has no binary content", storedContent.Fields["description"])
}

// TestIntegration_FetchBinary_EmptyKey tests error handling for empty key
func TestIntegration_FetchBinary_EmptyKey(t *testing.T) {
	natsClient := getSharedNATSClient(t)

	config := objectstore.Config{
		BucketName: "TEST_BINARY_ERROR",
	}

	ctx := context.Background()
	store, err := objectstore.NewStoreWithConfig(ctx, natsClient, config)
	require.NoError(t, err)
	defer store.Close()

	// Try to fetch with empty key
	emptyRef := objectstore.BinaryRef{
		ContentType: "image/jpeg",
		Size:        100,
		Key:         "", // Empty key
	}

	_, err = store.FetchBinary(ctx, emptyRef)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "empty key")
}

// testBinaryDocument implements BinaryStorable for testing
type testBinaryDocument struct {
	id          string
	title       string
	description string
	videoData   []byte
	imageData   []byte
	storageRef  *message.StorageReference
	triples     []message.Triple
}

func (d *testBinaryDocument) EntityID() string {
	return d.id
}

func (d *testBinaryDocument) Triples() []message.Triple {
	return d.triples
}

func (d *testBinaryDocument) StorageRef() *message.StorageReference {
	return d.storageRef
}

func (d *testBinaryDocument) RawContent() map[string]string {
	return map[string]string{
		"title":       d.title,
		"description": d.description,
	}
}

func (d *testBinaryDocument) ContentFields() map[string]string {
	fields := map[string]string{
		message.ContentRoleTitle:    "title",
		message.ContentRoleAbstract: "description",
	}
	// Map binary roles to binary field names
	if len(d.videoData) > 0 {
		fields[message.ContentRoleMedia] = "video"
	}
	if len(d.imageData) > 0 {
		fields[message.ContentRoleThumbnail] = "thumbnail"
	}
	return fields
}

func (d *testBinaryDocument) BinaryFields() map[string]message.BinaryContent {
	fields := make(map[string]message.BinaryContent)
	if len(d.videoData) > 0 {
		fields["video"] = message.BinaryContent{
			ContentType: "video/mp4",
			Data:        d.videoData,
		}
	}
	if len(d.imageData) > 0 {
		fields["thumbnail"] = message.BinaryContent{
			ContentType: "image/png",
			Data:        d.imageData,
		}
	}
	return fields
}

// TestIntegration_ExtractTextFields_KeyMapping verifies that when raw BaseMessage bytes
// are stored and then fetched via FetchContent, the extractTextFields function correctly
// uses fieldName (not role) as the key in StoredContent.Fields.
//
// This test validates the fix for a bug where extractTextFields was using role as key:
//
//	fields[role] = val  // BUG: stored {"abstract": "desc text"}
//
// Instead of:
//
//	fields[fieldName] = val  // CORRECT: stored {"description": "desc text"}
//
// The worker expects Fields to be keyed by fieldName so it can look up:
//
//	fieldName := contentFields["abstract"]  // = "description"
//	content := stored.Fields["description"] // Must find it here!
func TestIntegration_ExtractTextFields_KeyMapping(t *testing.T) {
	natsClient := getSharedNATSClient(t)

	config := objectstore.Config{
		BucketName: "TEST_EXTRACT_FIELDS",
	}

	ctx := context.Background()
	store, err := objectstore.NewStoreWithConfig(ctx, natsClient, config)
	require.NoError(t, err)
	defer store.Close()

	// FetchContent's BaseMessage-extraction fallback needs a decoder
	// that knows about content.document.v1 — register the document
	// example explicitly into a per-test registry.
	reg := payloadbuiltins.NewTestRegistry(t)
	require.NoError(t, document.RegisterPayloads(reg))
	store.SetDecoder(message.NewDecoder(reg))

	// Create a Document with description field
	// Document.ContentFields() returns {"abstract": "description", "body": "body", "title": "title"}
	// Note: "abstract" role maps to "description" fieldName
	doc := &document.Document{
		ID:          "test-doc-001",
		Title:       "Test Document Title",
		Description: "This is the abstract/description text", // Maps to "abstract" role
		Body:        "This is the body text",
		Category:    "test",
		EntityIDValue: document.MintDocumentEntityID(
			types.PlatformMeta{Org: "test-org", Platform: "test-dep"}, "test", "test-doc-001"),
	}

	// Create BaseMessage with Document payload
	baseMsg := message.NewBaseMessage(doc.Schema(), doc, "test-source")

	// Marshal to bytes (simulating what ObjectStore component receives from NATS)
	msgBytes, err := baseMsg.MarshalJSON()
	require.NoError(t, err)

	// Store raw bytes (this is what ObjectStore component does via handleWriteRequest)
	key, err := store.Store(ctx, msgBytes)
	require.NoError(t, err)

	// Create a StorageReference pointing to the stored bytes
	ref := &message.StorageReference{
		StorageInstance: config.BucketName,
		Key:             key,
	}

	// FetchContent should fall back to extractContentFromBaseMessage
	// which uses extractTextFields internally
	storedContent, err := store.FetchContent(ctx, ref)
	require.NoError(t, err)
	require.NotNil(t, storedContent)

	// CRITICAL ASSERTION: Fields should be keyed by fieldName, NOT role
	// The worker does: stored.Fields[contentFields["abstract"]] = stored.Fields["description"]
	assert.Equal(t, "This is the abstract/description text", storedContent.Fields["description"],
		"Fields should be keyed by fieldName ('description'), not role ('abstract')")

	assert.Equal(t, "This is the body text", storedContent.Fields["body"],
		"Fields should contain body content keyed by 'body'")

	assert.Equal(t, "Test Document Title", storedContent.Fields["title"],
		"Fields should contain title content keyed by 'title'")

	// Verify that role is NOT used as key (would indicate the bug is present)
	_, hasAbstractKey := storedContent.Fields["abstract"]
	assert.False(t, hasAbstractKey,
		"Fields should NOT have 'abstract' as key - that's the role, not the fieldName")

	// Verify ContentFields mapping is preserved correctly
	assert.Equal(t, "description", storedContent.ContentFields[message.ContentRoleAbstract],
		"ContentFields should map abstract role to description fieldName")
	assert.Equal(t, "body", storedContent.ContentFields[message.ContentRoleBody],
		"ContentFields should map body role to body fieldName")
	assert.Equal(t, "title", storedContent.ContentFields[message.ContentRoleTitle],
		"ContentFields should map title role to title fieldName")
}

// gh#400: StorageReference.StorageInstance must be stamped with the canonical
// component instance name, consistently across both write paths. Before the
// fix, the component's StoredMessage emit path stamped c.instanceName while the
// Store's StoreContent path stamped the bucket name — a resolver (the fusion
// hydration deref helper, ADR-062 #399, the first reader) keyed on one could
// not resolve refs stamped with the other.

// TestIntegration_GH400_StoreContentStampsInstanceName locks the fix: when the
// component threads its instance name into the store config (InstanceName set,
// distinct from the bucket), StoreContent stamps the INSTANCE name — the same
// value the component's emitStoredMessage path uses — not the bucket name.
func TestIntegration_GH400_StoreContentStampsInstanceName(t *testing.T) {
	natsClient := getSharedNATSClient(t)
	ctx := context.Background()

	const instanceName = "objectstore" // what the component sets as c.instanceName
	config := objectstore.Config{
		BucketName:   "TEST_GH400_INSTANCE", // deliberately != instanceName
		InstanceName: instanceName,
	}
	store, err := objectstore.NewStoreWithConfig(ctx, natsClient, config)
	require.NoError(t, err)
	defer store.Close()

	doc := &document.Document{
		ID: "gh400-doc-001", Title: "Canonical Instance", Description: "ref stamping",
		Body: "verbatim body", Category: "test",
		EntityIDValue: document.MintDocumentEntityID(
			types.PlatformMeta{Org: "org", Platform: "dep1"}, "test", "gh400-doc-001"),
	}

	ref, err := store.StoreContent(ctx, doc)
	require.NoError(t, err)
	assert.Equal(t, instanceName, ref.StorageInstance,
		"StoreContent must stamp the component instance name (gh#400), matching the StoredMessage emit path")
	assert.NotEqual(t, config.BucketName, ref.StorageInstance,
		"StoreContent must NOT stamp the bucket name (the gh#400 bug)")
}

// TestIntegration_GH400_StoreContentDefaultsToBucketName locks the standalone
// fallback: a Store constructed without an InstanceName (direct callers like
// agentic-loop / graph-embedding that have no component instance name) stamps
// the bucket name, preserving prior behavior so the instance is never empty.
func TestIntegration_GH400_StoreContentDefaultsToBucketName(t *testing.T) {
	natsClient := getSharedNATSClient(t)
	ctx := context.Background()

	config := objectstore.Config{BucketName: "TEST_GH400_DEFAULT"} // no InstanceName
	store, err := objectstore.NewStoreWithConfig(ctx, natsClient, config)
	require.NoError(t, err)
	defer store.Close()

	doc := &document.Document{
		ID: "gh400-doc-002", Title: "Default Bucket", Description: "fallback",
		Body: "verbatim body", Category: "test",
		EntityIDValue: document.MintDocumentEntityID(
			types.PlatformMeta{Org: "org", Platform: "dep1"}, "test", "gh400-doc-002"),
	}

	ref, err := store.StoreContent(ctx, doc)
	require.NoError(t, err)
	assert.Equal(t, config.BucketName, ref.StorageInstance,
		"with no InstanceName, StoreContent defaults StorageInstance to the bucket name")
}
