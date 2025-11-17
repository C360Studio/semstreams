package cache

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

// testBasicOperations tests basic cache operations.
func testBasicOperations(t *testing.T, cache Cache[string]) {
	// Test Get on empty cache
	if value, exists := cache.Get("key1"); exists {
		t.Errorf("Expected cache miss, got value: %s", value)
	}

	// Test Set and Get
	isNew, err := cache.Set("key1", "value1")
	if err != nil {
		t.Fatalf("Unexpected error setting key: %v", err)
	}
	if !isNew {
		t.Error("Expected new entry creation")
	}

	if value, exists := cache.Get("key1"); !exists || value != "value1" {
		t.Errorf("Expected 'value1', got value: %s, exists: %t", value, exists)
	}

	// Test Update
	isNew, err = cache.Set("key1", "value1_updated")
	if err != nil {
		t.Fatalf("Unexpected error updating key: %v", err)
	}
	if isNew {
		t.Error("Expected existing entry update")
	}

	if value, exists := cache.Get("key1"); !exists || value != "value1_updated" {
		t.Errorf("Expected 'value1_updated', got value: %s, exists: %t", value, exists)
	}

	// Test Delete
	deleted, err := cache.Delete("key1")
	if err != nil {
		t.Fatalf("Unexpected error deleting key: %v", err)
	}
	if !deleted {
		t.Error("Expected successful deletion")
	}

	deleted, err = cache.Delete("key1")
	if err != nil {
		t.Fatalf("Unexpected error deleting non-existent key: %v", err)
	}
	if deleted {
		t.Error("Expected deletion failure for non-existent key")
	}

	if value, exists := cache.Get("key1"); exists {
		t.Errorf("Expected cache miss after deletion, got value: %s", value)
	}
}

// testSizeOperations tests cache size tracking.
func testSizeOperations(t *testing.T, cache Cache[string]) {
	if cache.Size() != 0 {
		t.Errorf("Expected size 0, got %d", cache.Size())
	}

	_, _ = cache.Set("key1", "value1")
	_, _ = cache.Set("key2", "value2")

	if cache.Size() != 2 {
		t.Errorf("Expected size 2, got %d", cache.Size())
	}

	_, _ = cache.Delete("key1")

	if cache.Size() != 1 {
		t.Errorf("Expected size 1, got %d", cache.Size())
	}
}

// testKeysOperation tests cache key listing.
func testKeysOperation(t *testing.T, cache Cache[string]) {
	if len(cache.Keys()) != 0 {
		t.Errorf("Expected no keys, got %v", cache.Keys())
	}

	_, _ = cache.Set("key1", "value1")
	_, _ = cache.Set("key2", "value2")

	keys := cache.Keys()
	if len(keys) != 2 {
		t.Errorf("Expected 2 keys, got %d", len(keys))
	}

	keyMap := make(map[string]bool)
	for _, key := range keys {
		keyMap[key] = true
	}

	if !keyMap["key1"] || !keyMap["key2"] {
		t.Errorf("Expected keys 'key1' and 'key2', got %v", keys)
	}
}

// testClearOperation tests cache clearing.
func testClearOperation(t *testing.T, cache Cache[string]) {
	_, _ = cache.Set("key1", "value1")
	_, _ = cache.Set("key2", "value2")

	_ = cache.Clear()

	if cache.Size() != 0 {
		t.Errorf("Expected size 0 after clear, got %d", cache.Size())
	}

	if value, exists := cache.Get("key1"); exists {
		t.Errorf("Expected cache miss after clear, got value: %s", value)
	}
}

// testSuite runs common cache tests across all implementations.
func testSuite(t *testing.T, createCache func() Cache[string]) {
	t.Run("BasicOperations", func(t *testing.T) {
		cache := createCache()
		defer cache.Close()
		testBasicOperations(t, cache)
	})

	t.Run("Size", func(t *testing.T) {
		cache := createCache()
		defer cache.Close()
		testSizeOperations(t, cache)
	})

	t.Run("Keys", func(t *testing.T) {
		cache := createCache()
		defer cache.Close()
		testKeysOperation(t, cache)
	})

	t.Run("Clear", func(t *testing.T) {
		cache := createCache()
		defer cache.Close()
		testClearOperation(t, cache)
	})
}

// TestSimpleCache tests the simple cache implementation.
func TestSimpleCache(t *testing.T) {
	testSuite(t, func() Cache[string] {
		cache, err := NewSimple[string]()
		if err != nil {
			panic(err)
		}
		return cache
	})

	t.Run("NoEviction", func(t *testing.T) {
		cache, err := NewSimple[string]()
		if err != nil {
			t.Fatal(err)
		}
		defer cache.Close()

		// Add many items to ensure no eviction
		for i := 0; i < 1000; i++ {
			_, _ = cache.Set(fmt.Sprintf("key%d", i), fmt.Sprintf("value%d", i))
		}

		if cache.Size() != 1000 {
			t.Errorf("Expected size 1000, got %d", cache.Size())
		}

		// All items should still be present
		for i := 0; i < 1000; i++ {
			if value, exists := cache.Get(fmt.Sprintf("key%d", i)); !exists || value != fmt.Sprintf("value%d", i) {
				t.Errorf("Item %d missing or incorrect", i)
			}
		}
	})
}

// TestLRUCache tests the LRU cache implementation.
func TestLRUCache(t *testing.T) {
	testSuite(t, func() Cache[string] {
		cache, err := NewLRU[string](10)
		if err != nil {
			panic(err)
		}
		return cache
	})

	t.Run("LRUEviction", func(t *testing.T) {
		cache, err := NewLRU[string](3)
		if err != nil {
			t.Fatal(err)
		}
		defer cache.Close()

		// Fill cache to capacity
		_, _ = cache.Set("key1", "value1")
		_, _ = cache.Set("key2", "value2")
		_, _ = cache.Set("key3", "value3")

		if cache.Size() != 3 {
			t.Errorf("Expected size 3, got %d", cache.Size())
		}

		// Access key1 to make it most recently used
		cache.Get("key1")

		// Add key4, which should evict key2 (least recently used)
		_, _ = cache.Set("key4", "value4")

		if cache.Size() != 3 {
			t.Errorf("Expected size 3 after eviction, got %d", cache.Size())
		}

		// key2 should be evicted
		if _, exists := cache.Get("key2"); exists {
			t.Error("Expected key2 to be evicted")
		}

		// key1, key3, key4 should still exist
		if _, exists := cache.Get("key1"); !exists {
			t.Error("Expected key1 to exist")
		}
		if _, exists := cache.Get("key3"); !exists {
			t.Error("Expected key3 to exist")
		}
		if _, exists := cache.Get("key4"); !exists {
			t.Error("Expected key4 to exist")
		}
	})

	t.Run("LRUOrder", func(t *testing.T) {
		cache, err := NewLRU[string](3)
		if err != nil {
			t.Fatal(err)
		}
		defer cache.Close()

		_, _ = cache.Set("key1", "value1")
		_, _ = cache.Set("key2", "value2")
		_, _ = cache.Set("key3", "value3")

		// Access in specific order
		cache.Get("key2")
		cache.Get("key1")
		cache.Get("key3")

		keys := cache.Keys()
		expected := []string{"key3", "key1", "key2"}

		for i, key := range keys {
			if key != expected[i] {
				t.Errorf("Expected key order %v, got %v", expected, keys)
				break
			}
		}
	})
}

// TestTTLCache tests the TTL cache implementation.
func TestTTLCache(t *testing.T) {
	testSuite(t, func() Cache[string] {
		cache, err := NewTTL[string](context.Background(), 100*time.Millisecond, 50*time.Millisecond)
		if err != nil {
			panic(err)
		}
		return cache
	})

	t.Run("TTLExpiration", func(t *testing.T) {
		cache, err := NewTTL[string](context.Background(), 100*time.Millisecond, 50*time.Millisecond)
		if err != nil {
			t.Fatal(err)
		}
		defer cache.Close()

		_, _ = cache.Set("key1", "value1")

		// Should exist immediately
		if value, exists := cache.Get("key1"); !exists || value != "value1" {
			t.Error("Expected key1 to exist immediately after set")
		}

		// Wait for expiration
		time.Sleep(150 * time.Millisecond)

		// Should be expired
		if _, exists := cache.Get("key1"); exists {
			t.Error("Expected key1 to be expired")
		}
	})

	t.Run("BackgroundCleanup", func(t *testing.T) {
		cache, err := NewTTL[string](context.Background(), 50*time.Millisecond, 25*time.Millisecond)
		if err != nil {
			t.Fatal(err)
		}
		defer cache.Close()

		_, _ = cache.Set("key1", "value1")
		_, _ = cache.Set("key2", "value2")

		if cache.Size() != 2 {
			t.Errorf("Expected size 2, got %d", cache.Size())
		}

		// Wait for background cleanup
		time.Sleep(100 * time.Millisecond)

		// Items should be cleaned up
		if cache.Size() != 0 {
			t.Errorf("Expected size 0 after cleanup, got %d", cache.Size())
		}
	})
}

// TestHybridCache tests the hybrid cache implementation.
func TestHybridCache(t *testing.T) {
	testSuite(t, func() Cache[string] {
		cache, err := newHybrid[string](context.Background(), 10, 100*time.Millisecond, 50*time.Millisecond)
		if err != nil {
			panic(err)
		}
		return cache
	})

	t.Run("HybridEviction", func(t *testing.T) {
		cache, err := newHybrid[string](context.Background(), 2, 1*time.Second, 500*time.Millisecond)
		if err != nil {
			t.Fatal(err)
		}
		defer cache.Close()

		_, _ = cache.Set("key1", "value1")
		_, _ = cache.Set("key2", "value2")

		// Should trigger LRU eviction of key1
		_, _ = cache.Set("key3", "value3")

		if cache.Size() != 2 {
			t.Errorf("Expected size 2, got %d", cache.Size())
		}

		if _, exists := cache.Get("key1"); exists {
			t.Error("Expected key1 to be evicted by LRU")
		}
	})

	t.Run("TTLInHybrid", func(t *testing.T) {
		cache, err := newHybrid[string](context.Background(), 10, 100*time.Millisecond, 50*time.Millisecond)
		if err != nil {
			t.Fatal(err)
		}
		defer cache.Close()

		_, _ = cache.Set("key1", "value1")

		// Wait for TTL expiration
		time.Sleep(150 * time.Millisecond)

		if _, exists := cache.Get("key1"); exists {
			t.Error("Expected key1 to be expired by TTL")
		}
	})
}

// runConcurrentOperations performs concurrent cache operations for testing.
func runConcurrentOperations(t *testing.T, cache Cache[string], numGoroutines, numOperations int) {
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	// Concurrent reads and writes
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()

			for j := 0; j < numOperations; j++ {
				key := fmt.Sprintf("key%d-%d", id, j)
				value := fmt.Sprintf("value%d-%d", id, j)

				_, _ = cache.Set(key, value)

				if retrievedValue, exists := cache.Get(key); exists && retrievedValue != value {
					t.Errorf("Expected %s, got %s", value, retrievedValue)
				}

				if j%10 == 0 {
					_, _ = cache.Delete(key)
				}
			}
		}(i)
	}

	wg.Wait()
}

// TestConcurrency tests thread safety of cache implementations.
func TestConcurrency(t *testing.T) {
	// Helper to create caches with error handling
	createCaches := func() []struct {
		name  string
		cache Cache[string]
	} {
		simple, _ := NewSimple[string]()
		lru, _ := NewLRU[string](100)
		ttl, _ := NewTTL[string](context.Background(), 1*time.Second, 500*time.Millisecond)
		hybrid, _ := newHybrid[string](context.Background(), 100, 1*time.Second, 500*time.Millisecond)

		return []struct {
			name  string
			cache Cache[string]
		}{
			{"Simple", simple},
			{"LRU", lru},
			{"TTL", ttl},
			{"Hybrid", hybrid},
		}
	}

	caches := createCaches()

	for _, tc := range caches {
		t.Run(tc.name, func(t *testing.T) {
			cache := tc.cache
			defer cache.Close()

			const numGoroutines = 10
			const numOperations = 100

			runConcurrentOperations(t, cache, numGoroutines, numOperations)
		})
	}
}

// TestEvictCallback tests the eviction callback functionality.
func TestEvictCallback(t *testing.T) {
	t.Run("LRUEvictCallback", func(t *testing.T) {
		var evictedKeys []string
		var mu sync.Mutex

		cache, err := NewLRU[string](2, WithEvictionCallback[string](func(key string, _ string) {
			mu.Lock()
			evictedKeys = append(evictedKeys, key)
			mu.Unlock()
		}))
		if err != nil {
			t.Fatal(err)
		}
		defer cache.Close()

		_, _ = cache.Set("key1", "value1")
		_, _ = cache.Set("key2", "value2")
		_, _ = cache.Set("key3", "value3") // Should evict key1

		time.Sleep(10 * time.Millisecond) // Allow callback to execute

		mu.Lock()
		if len(evictedKeys) != 1 || evictedKeys[0] != "key1" {
			t.Errorf("Expected evicted keys [key1], got %v", evictedKeys)
		}
		mu.Unlock()
	})

	t.Run("TTLEvictCallback", func(t *testing.T) {
		var evictedKeys []string
		var mu sync.Mutex

		cache, err := NewTTL[string](
			context.Background(),
			50*time.Millisecond,
			25*time.Millisecond,
			WithEvictionCallback[string](func(key string, _ string) {
				mu.Lock()
				evictedKeys = append(evictedKeys, key)
				mu.Unlock()
			}),
		)
		if err != nil {
			t.Fatal(err)
		}
		defer cache.Close()

		_, _ = cache.Set("key1", "value1")

		// Wait for expiration and cleanup
		time.Sleep(100 * time.Millisecond)

		mu.Lock()
		if len(evictedKeys) != 1 || evictedKeys[0] != "key1" {
			t.Errorf("Expected evicted keys [key1], got %v", evictedKeys)
		}
		mu.Unlock()
	})
}

// TestStatistics tests the statistics functionality.
func TestStatistics(t *testing.T) {
	// Note: Stats are always enabled now
	cache, err := NewLRU[string](10)
	if err != nil {
		t.Fatal(err)
	}
	defer cache.Close()

	stats := cache.Stats()
	if stats == nil {
		t.Fatal("Expected stats to be enabled")
	}

	// Test basic operations
	_, _ = cache.Set("key1", "value1")
	_, _ = cache.Set("key2", "value2")
	cache.Get("key1") // hit
	cache.Get("key3") // miss
	_, _ = cache.Delete("key2")

	if stats.Sets() != 2 {
		t.Errorf("Expected 2 sets, got %d", stats.Sets())
	}

	if stats.Hits() != 1 {
		t.Errorf("Expected 1 hit, got %d", stats.Hits())
	}

	if stats.Misses() != 1 {
		t.Errorf("Expected 1 miss, got %d", stats.Misses())
	}

	if stats.Deletes() != 1 {
		t.Errorf("Expected 1 delete, got %d", stats.Deletes())
	}

	if stats.HitRatio() != 0.5 {
		t.Errorf("Expected hit ratio 0.5, got %f", stats.HitRatio())
	}

	if stats.CurrentSize() != 1 {
		t.Errorf("Expected current size 1, got %d", stats.CurrentSize())
	}
}

// testValidConfigs tests valid cache configurations.
func testValidConfigs(t *testing.T) {
	configs := []Config{
		{Enabled: true, Strategy: StrategySimple},
		{Enabled: true, Strategy: StrategyLRU, MaxSize: 100},
		{Enabled: true, Strategy: StrategyTTL, TTL: 5 * time.Minute, CleanupInterval: 1 * time.Minute},
		{Enabled: true, Strategy: StrategyHybrid, MaxSize: 100, TTL: 5 * time.Minute, CleanupInterval: 1 * time.Minute},
	}

	for i, config := range configs {
		t.Run(fmt.Sprintf("Config%d", i), func(t *testing.T) {
			cache, err := NewFromConfig[string](context.Background(), config)
			if err != nil {
				t.Fatalf("Expected no error, got %v", err)
			}
			defer cache.Close()

			// Basic functionality test
			_, _ = cache.Set("test", "value")
			if value, exists := cache.Get("test"); !exists || value != "value" {
				t.Error("Cache not working properly")
			}
		})
	}
}

// testDisabledCache tests that disabled caches work correctly.
func testDisabledCache(t *testing.T) {
	config := Config{Enabled: false}
	cache, err := NewFromConfig[string](context.Background(), config)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	defer cache.Close()

	// Should always miss
	_, _ = cache.Set("test", "value")
	if _, exists := cache.Get("test"); exists {
		t.Error("Disabled cache should always miss")
	}
}

// testInvalidConfigs tests that invalid configurations are rejected.
func testInvalidConfigs(t *testing.T) {
	invalidConfigs := []Config{
		{Enabled: true, Strategy: StrategyLRU, MaxSize: 0},
		{Enabled: true, Strategy: StrategyTTL, TTL: 0, CleanupInterval: 1 * time.Minute},
		{Enabled: true, Strategy: Strategy("invalid")},
	}

	for i, config := range invalidConfigs {
		t.Run(fmt.Sprintf("Invalid%d", i), func(t *testing.T) {
			_, err := NewFromConfig[string](context.Background(), config)
			if err == nil {
				t.Error("Expected error for invalid config")
			}
		})
	}
}

// TestConfiguration tests cache creation from configuration.
func TestConfiguration(t *testing.T) {
	t.Run("ValidConfigs", testValidConfigs)
	t.Run("DisabledCache", testDisabledCache)
	t.Run("InvalidConfigs", testInvalidConfigs)
}
