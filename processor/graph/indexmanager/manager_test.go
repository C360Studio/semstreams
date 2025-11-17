package indexmanager

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	gtypes "github.com/c360/semstreams/graph"
	"github.com/c360/semstreams/metric"
)

// MockKeyValue implements jetstream.KeyValue for testing
type MockKeyValue struct {
	mock.Mock
	data map[string][]byte
}

func NewMockKeyValue() *MockKeyValue {
	return &MockKeyValue{
		data: make(map[string][]byte),
	}
}

func (m *MockKeyValue) Get(ctx context.Context, key string) (jetstream.KeyValueEntry, error) {
	args := m.Called(ctx, key)
	if entry := args.Get(0); entry != nil {
		return entry.(jetstream.KeyValueEntry), args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *MockKeyValue) Put(ctx context.Context, key string, value []byte) (uint64, error) {
	args := m.Called(ctx, key, value)
	m.data[key] = value
	if rev := args.Get(0); rev != nil {
		return rev.(uint64), args.Error(1)
	}
	return 1, args.Error(1)
}

func (m *MockKeyValue) Delete(ctx context.Context, key string, _ ...jetstream.KVDeleteOpt) error {
	args := m.Called(ctx, key)
	delete(m.data, key)
	return args.Error(0)
}

func (m *MockKeyValue) WatchAll(ctx context.Context, _ ...jetstream.WatchOpt) (jetstream.KeyWatcher, error) {
	args := m.Called(ctx)
	if watcher := args.Get(0); watcher != nil {
		return watcher.(jetstream.KeyWatcher), args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *MockKeyValue) Watch(
	ctx context.Context,
	pattern string,
	_ ...jetstream.WatchOpt,
) (jetstream.KeyWatcher, error) {
	args := m.Called(ctx, pattern)
	if watcher := args.Get(0); watcher != nil {
		return watcher.(jetstream.KeyWatcher), args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *MockKeyValue) Bucket() string {
	return "test-bucket"
}

// Additional required methods for jetstream.KeyValue interface
func (m *MockKeyValue) GetRevision(_ context.Context, _ string, _ uint64) (jetstream.KeyValueEntry, error) {
	return nil, nil
}

func (m *MockKeyValue) PutString(ctx context.Context, _ string, _ string) (uint64, error) {
	// Check for cancellation
	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	default:
	}
	return 0, nil
}

func (m *MockKeyValue) Create(
	ctx context.Context,
	key string,
	value []byte,
	_ ...jetstream.KVCreateOpt,
) (uint64, error) {
	args := m.Called(ctx, key, value)
	m.data[key] = value
	if rev := args.Get(0); rev != nil {
		return rev.(uint64), args.Error(1)
	}
	return 1, args.Error(1)
}

func (m *MockKeyValue) Update(_ context.Context, _ string, _ []byte, _ uint64) (uint64, error) {
	return 0, nil
}

func (m *MockKeyValue) Purge(_ context.Context, _ string, _ ...jetstream.KVDeleteOpt) error {
	return nil
}

func (m *MockKeyValue) Keys(_ context.Context, _ ...jetstream.WatchOpt) ([]string, error) {
	return nil, nil
}

func (m *MockKeyValue) ListKeys(_ context.Context, _ ...jetstream.WatchOpt) (jetstream.KeyLister, error) {
	return nil, nil
}

func (m *MockKeyValue) ListKeysFiltered(_ context.Context, _ ...string) (jetstream.KeyLister, error) {
	return nil, nil
}

func (m *MockKeyValue) History(
	_ context.Context,
	_ string,
	_ ...jetstream.WatchOpt,
) ([]jetstream.KeyValueEntry, error) {
	return nil, nil
}

func (m *MockKeyValue) WatchFiltered(
	_ context.Context,
	_ []string,
	_ ...jetstream.WatchOpt,
) (jetstream.KeyWatcher, error) {
	return nil, nil
}

func (m *MockKeyValue) PurgeDeletes(_ context.Context, _ ...jetstream.KVPurgeOpt) error {
	return nil
}

func (m *MockKeyValue) Status(ctx context.Context) (jetstream.KeyValueStatus, error) {
	// Check for cancellation
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	return nil, nil
}

// MockKeyWatcher implements jetstream.KeyWatcher for testing
type MockKeyWatcher struct {
	mock.Mock
	updates chan jetstream.KeyValueEntry
}

func NewMockKeyWatcher() *MockKeyWatcher {
	return &MockKeyWatcher{
		updates: make(chan jetstream.KeyValueEntry, 100),
	}
}

func (m *MockKeyWatcher) Updates() <-chan jetstream.KeyValueEntry {
	// Simply return the channel directly - it's already the correct type
	return m.updates
}

func (m *MockKeyWatcher) Stop() error {
	args := m.Called()
	close(m.updates)
	return args.Error(0)
}

// MockKeyValueEntry implements jetstream.KeyValueEntry for testing
type MockKeyValueEntry struct {
	key       string
	value     []byte
	revision  uint64
	operation jetstream.KeyValueOp
}

func (m *MockKeyValueEntry) Key() string                     { return m.key }
func (m *MockKeyValueEntry) Value() []byte                   { return m.value }
func (m *MockKeyValueEntry) Revision() uint64                { return m.revision }
func (m *MockKeyValueEntry) Operation() jetstream.KeyValueOp { return m.operation }
func (m *MockKeyValueEntry) Created() time.Time              { return time.Now() }
func (m *MockKeyValueEntry) Delta() uint64                   { return 0 }
func (m *MockKeyValueEntry) Bucket() string                  { return "test-bucket" }

// Test helper functions

func setupTestManager(t *testing.T) (*Manager, map[string]*MockKeyValue) {
	config := DefaultConfig()
	config.Workers = 2 // Reduce workers for testing
	config.EventBuffer.Capacity = 100
	config.EventBuffer.Metrics = true // Keep metrics enabled to test full behavior
	config.BatchProcessing.Size = 10
	config.BatchProcessing.Interval = 10 * time.Millisecond

	// Create mock buckets
	mockBuckets := map[string]*MockKeyValue{
		"ENTITY_STATES":   NewMockKeyValue(),
		"PREDICATE_INDEX": NewMockKeyValue(),
		"INCOMING_INDEX":  NewMockKeyValue(),
		"ALIAS_INDEX":     NewMockKeyValue(),
		"SPATIAL_INDEX":   NewMockKeyValue(),
		"TEMPORAL_INDEX":  NewMockKeyValue(),
	}

	// Convert to interface map
	buckets := make(map[string]jetstream.KeyValue)
	for name, mockBucket := range mockBuckets {
		buckets[name] = mockBucket
	}

	// Setup mock expectations for WatchAll
	mockWatcher := NewMockKeyWatcher()
	mockBuckets["ENTITY_STATES"].On("WatchAll", mock.Anything).Return(mockWatcher, nil)
	mockWatcher.On("Updates").Return(mockWatcher.updates)
	mockWatcher.On("Stop").Return(nil)

	// Use a test-specific metrics registry to avoid conflicts
	testRegistry := metric.NewMetricsRegistry()
	engine, err := NewManager(config, buckets, nil, testRegistry, nil)
	require.NoError(t, err)

	// Type assert since NewManager returns interface
	engineImpl, ok := engine.(*Manager)
	require.True(t, ok, "Failed to cast to *Manager")
	return engineImpl, mockBuckets
}

func TestNewManager(t *testing.T) {
	t.Run("valid config", func(t *testing.T) {
		engine, _ := setupTestManager(t)
		assert.NotNil(t, engine)
		assert.Equal(t, 5, len(engine.indexes)) // All indexes enabled by default
	})

	t.Run("invalid config", func(t *testing.T) {
		config := DefaultConfig()
		config.Workers = 0                // Invalid
		config.EventBuffer.Metrics = true // Keep metrics enabled to test full behavior

		// Use a test-specific registry instead of the default one
		testRegistry := metric.NewMetricsRegistry()
		_, err := NewManager(config, make(map[string]jetstream.KeyValue), nil, testRegistry, nil)
		assert.Error(t, err)
	})
}

func TestIndexManager_Lifecycle(t *testing.T) {
	t.Run("run and cancel", func(t *testing.T) {
		engine, _ := setupTestManager(t)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Start manager in goroutine
		errorChan := make(chan error, 1)
		go func() {
			errorChan <- engine.Run(ctx)
		}()

		// Wait for initialization
		time.Sleep(50 * time.Millisecond)

		// Check for startup errors
		select {
		case err := <-errorChan:
			if err != nil {
				t.Fatalf("Manager failed to start: %v", err)
			}
		default:
			// No errors yet - manager is running
		}

		// Cancel context to trigger shutdown
		cancel()

		// Wait for clean shutdown
		select {
		case err := <-errorChan:
			assert.NoError(t, err, "Manager should shut down cleanly")
		case <-time.After(5 * time.Second):
			t.Error("Manager did not shutdown within 5 seconds")
		}
	})

	t.Run("context already cancelled", func(t *testing.T) {
		engine, _ := setupTestManager(t)
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		// Run should return immediately without error
		err := engine.Run(ctx)
		assert.NoError(t, err, "Run should return cleanly when context is cancelled")
	})
}

func TestIndexManager_GetPredicateIndex(t *testing.T) {
	engine, mockBuckets := setupTestManager(t)
	ctx := context.Background()

	t.Run("index disabled", func(t *testing.T) {
		engine.config.Indexes.Predicate = false
		_, err := engine.GetPredicateIndex(ctx, "test.predicate")
		assert.Equal(t, ErrIndexDisabled, err)
	})

	t.Run("predicate not found", func(t *testing.T) {
		engine.config.Indexes.Predicate = true
		mockBuckets["PREDICATE_INDEX"].On("Get", mock.Anything, "test.predicate.missing").
			Return(nil, jetstream.ErrKeyNotFound)

		result, err := engine.GetPredicateIndex(ctx, "test.predicate.missing")
		assert.NoError(t, err)
		assert.Empty(t, result)
	})

	t.Run("predicate found", func(t *testing.T) {
		engine.config.Indexes.Predicate = true

		// Mock successful response
		mockEntry := &MockKeyValueEntry{
			key:   "test.predicate.found",
			value: []byte(`["entity1","entity2"]`),
		}
		mockBuckets["PREDICATE_INDEX"].On("Get", mock.Anything, "test.predicate.found").Return(mockEntry, nil)

		result, err := engine.GetPredicateIndex(ctx, "test.predicate.found")
		assert.NoError(t, err)
		assert.Equal(t, []string{"entity1", "entity2"}, result)
	})
}

func TestIndexManager_QuerySpatial(t *testing.T) {
	engine, _ := setupTestManager(t)
	ctx := context.Background()

	t.Run("index disabled", func(t *testing.T) {
		engine.config.Indexes.Spatial = false
		_, err := engine.QuerySpatial(ctx, Bounds{North: 1, South: 0, East: 1, West: 0})
		assert.Equal(t, ErrIndexDisabled, err)
	})

	t.Run("invalid bounds", func(t *testing.T) {
		engine.config.Indexes.Spatial = true
		// North < South
		_, err := engine.QuerySpatial(ctx, Bounds{North: 0, South: 1, East: 1, West: 0})
		assert.Equal(t, ErrInvalidBounds, err)
	})
}

func TestIndexManager_QueryTemporal(t *testing.T) {
	engine, _ := setupTestManager(t)
	ctx := context.Background()

	t.Run("index disabled", func(t *testing.T) {
		engine.config.Indexes.Temporal = false
		_, err := engine.QueryTemporal(ctx, time.Now(), time.Now().Add(time.Hour))
		assert.Equal(t, ErrIndexDisabled, err)
	})

	t.Run("invalid time range", func(t *testing.T) {
		engine.config.Indexes.Temporal = true
		now := time.Now()
		// Start after end
		_, err := engine.QueryTemporal(ctx, now.Add(time.Hour), now)
		assert.Equal(t, ErrInvalidTimeRange, err)
	})
}

func TestIndexManager_ResolveAlias(t *testing.T) {
	engine, mockBuckets := setupTestManager(t)
	ctx := context.Background()

	t.Run("index disabled", func(t *testing.T) {
		engine.config.Indexes.Alias = false
		_, err := engine.ResolveAlias(ctx, "test-alias")
		assert.Equal(t, ErrIndexDisabled, err)
	})

	t.Run("alias not found", func(t *testing.T) {
		engine.config.Indexes.Alias = true
		mockBuckets["ALIAS_INDEX"].On("Get", mock.Anything, "alias--test-alias-missing").Return(nil, jetstream.ErrKeyNotFound)

		_, err := engine.ResolveAlias(ctx, "test-alias-missing")
		assert.Equal(t, gtypes.ErrAliasNotFound, err)
	})

	t.Run("alias found", func(t *testing.T) {
		engine.config.Indexes.Alias = true

		// Mock successful response
		mockEntry := &MockKeyValueEntry{
			key:   "alias--test-alias-found",
			value: []byte("entity123"),
		}
		mockBuckets["ALIAS_INDEX"].On("Get", mock.Anything, "alias--test-alias-found").Return(mockEntry, nil)

		result, err := engine.ResolveAlias(ctx, "test-alias-found")
		assert.NoError(t, err)
		assert.Equal(t, "entity123", result)
	})
}

func TestIndexManager_Health(t *testing.T) {
	engine, _ := setupTestManager(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start manager in goroutine
	errorChan := make(chan error, 1)
	go func() {
		errorChan <- engine.Run(ctx)
	}()

	// Wait for initialization
	time.Sleep(50 * time.Millisecond)

	// Check for startup errors
	select {
	case err := <-errorChan:
		if err != nil {
			t.Fatalf("Manager failed to start: %v", err)
		}
	default:
		// No errors yet - manager is running
	}

	// Note: Health monitoring methods (IsReady, GetHealthStatus) are not yet implemented
	// Test the metrics methods that do exist

	// Check backlog
	backlog := engine.GetBacklog()
	assert.GreaterOrEqual(t, backlog, 0, "Backlog should be non-negative")

	// Check deduplication stats
	stats := engine.GetDeduplicationStats()
	assert.GreaterOrEqual(t, stats.TotalEvents, int64(0), "Total events should be non-negative")
	assert.GreaterOrEqual(t, stats.ProcessedEvents, int64(0), "Processed events should be non-negative")

	// Cancel context and wait for shutdown
	cancel()
	select {
	case err := <-errorChan:
		assert.NoError(t, err, "Manager should shut down cleanly")
	case <-time.After(5 * time.Second):
		t.Error("Manager did not shutdown within 5 seconds")
	}
}

func TestIndexManager_BatchQueries(t *testing.T) {
	engine, mockBuckets := setupTestManager(t)
	ctx := context.Background()

	t.Run("batch predicate queries", func(t *testing.T) {
		// Setup mocks for multiple predicates
		mockEntry1 := &MockKeyValueEntry{
			key:   "pred1",
			value: []byte(`["entity1"]`),
		}
		mockEntry2 := &MockKeyValueEntry{
			key:   "pred2",
			value: []byte(`["entity2"]`),
		}

		mockBuckets["PREDICATE_INDEX"].On("Get", mock.Anything, "pred1").Return(mockEntry1, nil)
		mockBuckets["PREDICATE_INDEX"].On("Get", mock.Anything, "pred2").Return(mockEntry2, nil)

		result, err := engine.GetPredicateIndexes(ctx, []string{"pred1", "pred2"})
		assert.NoError(t, err)
		assert.Equal(t, map[string][]string{
			"pred1": {"entity1"},
			"pred2": {"entity2"},
		}, result)
	})

	t.Run("batch alias resolution", func(t *testing.T) {
		mockEntry1 := &MockKeyValueEntry{
			key:   "alias--alias1",
			value: []byte("entity1"),
		}
		mockBuckets["ALIAS_INDEX"].On("Get", mock.Anything, "alias--alias1").Return(mockEntry1, nil)
		mockBuckets["ALIAS_INDEX"].On("Get", mock.Anything, "alias--alias2").Return(nil, jetstream.ErrKeyNotFound)

		result, err := engine.ResolveAliases(ctx, []string{"alias1", "alias2"})
		assert.NoError(t, err)
		assert.Equal(t, map[string]string{
			"alias1": "entity1",
			// alias2 not found, so not in result
		}, result)
	})
}

// Benchmark tests
func BenchmarkIndexManager_GetPredicateIndex(b *testing.B) {
	// Convert *testing.B to *testing.T for setupTestManager
	t := &testing.T{}
	engine, mockBuckets := setupTestManager(t)
	ctx := context.Background()

	// Setup mock response
	mockEntry := &MockKeyValueEntry{
		key:   "test.predicate",
		value: []byte(`["entity1","entity2","entity3"]`),
	}
	mockBuckets["PREDICATE_INDEX"].On("Get", mock.Anything, "test.predicate").Return(mockEntry, nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := engine.GetPredicateIndex(ctx, "test.predicate")
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkIndexManager_ResolveAlias_Cold benchmarks cold alias resolution (no cache)
func BenchmarkIndexManager_ResolveAlias_Cold(b *testing.B) {
	t := &testing.T{}
	engine, mockBuckets := setupTestManager(t)
	ctx := context.Background()

	// Setup mock response for alias lookup
	mockEntry := &MockKeyValueEntry{
		key:   "alias--test-alias",
		value: []byte("resolved-entity-id"),
	}
	mockBuckets["ALIAS_INDEX"].On("Get", mock.Anything, "alias--test-alias").Return(mockEntry, nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := engine.ResolveAlias(ctx, "test-alias")
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkIndexManager_ResolveAlias_Parallel benchmarks concurrent alias resolution
func BenchmarkIndexManager_ResolveAlias_Parallel(b *testing.B) {
	t := &testing.T{}
	engine, mockBuckets := setupTestManager(t)
	ctx := context.Background()

	// Setup mock response
	mockEntry := &MockKeyValueEntry{
		key:   "alias--test-alias",
		value: []byte("resolved-entity-id"),
	}
	mockBuckets["ALIAS_INDEX"].On("Get", mock.Anything, "alias--test-alias").Return(mockEntry, nil)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, err := engine.ResolveAlias(ctx, "test-alias")
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}

// BenchmarkIndexManager_ResolveAliases_Batch benchmarks batch alias resolution
func BenchmarkIndexManager_ResolveAliases_Batch(b *testing.B) {
	t := &testing.T{}
	engine, mockBuckets := setupTestManager(t)
	ctx := context.Background()

	// Setup mock responses for multiple aliases
	aliases := []string{"alias1", "alias2", "alias3", "alias4", "alias5"}
	for i, alias := range aliases {
		entityID := fmt.Sprintf("entity-%d", i)
		mockEntry := &MockKeyValueEntry{
			key:   fmt.Sprintf("alias--%s", alias),
			value: []byte(entityID),
		}
		mockBuckets["ALIAS_INDEX"].On("Get", mock.Anything, fmt.Sprintf("alias--%s", alias)).Return(mockEntry, nil)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := engine.ResolveAliases(ctx, aliases)
		if err != nil {
			b.Fatal(err)
		}
	}
}
