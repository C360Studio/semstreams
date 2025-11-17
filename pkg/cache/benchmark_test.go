package cache

import (
	"context"
	"fmt"
	"math/rand"
	"testing"
	"time"
)

// Helper function to create caches with error handling
func mustCreateCaches() (Cache[string], Cache[string], Cache[string], Cache[string]) {
	simple, err := NewSimple[string]()
	if err != nil {
		panic(err)
	}
	lru, err := NewLRU[string](1000)
	if err != nil {
		panic(err)
	}
	ttl, err := NewTTL[string](context.Background(), 5*time.Minute, 1*time.Minute)
	if err != nil {
		panic(err)
	}
	hybrid, err := newHybrid[string](context.Background(), 1000, 5*time.Minute, 1*time.Minute)
	if err != nil {
		panic(err)
	}
	return simple, lru, ttl, hybrid
}

// BenchmarkCacheGet benchmarks cache Get operations across different implementations.
func BenchmarkCacheGet(b *testing.B) {
	simple, lru, ttl, hybrid := mustCreateCaches()

	benchmarks := []struct {
		name  string
		cache Cache[string]
	}{
		{"Simple", simple},
		{"LRU_1000", lru},
		{"TTL_5m", ttl},
		{"Hybrid_1000_5m", hybrid},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			cache := bm.cache
			defer cache.Close()

			// Pre-populate cache
			for i := 0; i < 1000; i++ {
				_, _ = cache.Set(fmt.Sprintf("key%d", i), fmt.Sprintf("value%d", i))
			}

			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					key := fmt.Sprintf("key%d", rand.Intn(1000))
					cache.Get(key)
				}
			})
		})
	}
}

// BenchmarkCacheSet benchmarks cache Set operations across different implementations.
func BenchmarkCacheSet(b *testing.B) {
	simple, lru, ttl, hybrid := mustCreateCaches()

	benchmarks := []struct {
		name  string
		cache Cache[string]
	}{
		{"Simple", simple},
		{"LRU_1000", lru},
		{"TTL_5m", ttl},
		{"Hybrid_1000_5m", hybrid},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			cache := bm.cache
			defer cache.Close()

			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				i := 0
				for pb.Next() {
					key := fmt.Sprintf("key%d", i)
					value := fmt.Sprintf("value%d", i)
					_, _ = cache.Set(key, value)
					i++
				}
			})
		})
	}
}

// BenchmarkCacheMixed benchmarks mixed cache operations (Get/Set/Delete).
func BenchmarkCacheMixed(b *testing.B) {
	simple, lru, ttl, hybrid := mustCreateCaches()

	benchmarks := []struct {
		name  string
		cache Cache[string]
	}{
		{"Simple", simple},
		{"LRU_1000", lru},
		{"TTL_5m", ttl},
		{"Hybrid_1000_5m", hybrid},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			cache := bm.cache
			defer cache.Close()

			// Pre-populate cache
			for i := 0; i < 500; i++ {
				_, _ = cache.Set(fmt.Sprintf("key%d", i), fmt.Sprintf("value%d", i))
			}

			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				i := 500
				for pb.Next() {
					switch rand.Intn(5) {
					case 0, 1: // 40% reads
						key := fmt.Sprintf("key%d", rand.Intn(1000))
						cache.Get(key)
					case 2, 3: // 40% writes
						key := fmt.Sprintf("key%d", i)
						value := fmt.Sprintf("value%d", i)
						_, _ = cache.Set(key, value)
						i++
					case 4: // 20% deletes
						key := fmt.Sprintf("key%d", rand.Intn(1000))
						_, _ = cache.Delete(key)
					}
				}
			})
		})
	}
}

// BenchmarkLRUEviction benchmarks LRU eviction performance.
func BenchmarkLRUEviction(b *testing.B) {
	sizes := []int{100, 500, 1000, 5000}

	for _, size := range sizes {
		b.Run(fmt.Sprintf("Size_%d", size), func(b *testing.B) {
			cache, err := NewLRU[string](size)
			if err != nil {
				b.Fatal(err)
			}
			defer cache.Close()

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				key := fmt.Sprintf("key%d", i)
				value := fmt.Sprintf("value%d", i)
				_, _ = cache.Set(key, value)
			}
		})
	}
}

// BenchmarkTTLCleanup benchmarks TTL cleanup performance.
func BenchmarkTTLCleanup(b *testing.B) {
	cache, err := NewTTL[string](context.Background(), 1*time.Millisecond, 10*time.Millisecond)
	if err != nil {
		b.Fatal(err)
	}
	defer cache.Close()

	// Pre-populate with items that will expire
	for i := 0; i < 1000; i++ {
		_, _ = cache.Set(fmt.Sprintf("key%d", i), fmt.Sprintf("value%d", i))
	}

	// Wait for items to expire
	time.Sleep(20 * time.Millisecond)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Access cache to trigger cleanup of expired items
		cache.Get(fmt.Sprintf("key%d", i%1000))
	}
}

// BenchmarkCacheWithStats benchmarks cache performance with statistics enabled.
func BenchmarkCacheWithStats(b *testing.B) {
	configs := []struct {
		name      string
		withStats bool
	}{
		{"WithoutStats", false},
		{"WithStats", true},
	}

	for _, config := range configs {
		b.Run(config.name, func(b *testing.B) {
			// Note: Stats are always enabled now, regardless of config.withStats
			cache, err := NewLRU[string](1000)
			if err != nil {
				b.Fatal(err)
			}
			defer cache.Close()

			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				i := 0
				for pb.Next() {
					if i%2 == 0 {
						key := fmt.Sprintf("key%d", i)
						value := fmt.Sprintf("value%d", i)
						_, _ = cache.Set(key, value)
					} else {
						key := fmt.Sprintf("key%d", rand.Intn(i+1))
						cache.Get(key)
					}
					i++
				}
			})
		})
	}
}

// BenchmarkMemoryUsage measures relative memory usage of different cache types.
func BenchmarkMemoryUsage(b *testing.B) {
	const numItems = 10000

	simple, err := NewSimple[string]()
	if err != nil {
		b.Fatal(err)
	}
	lru, err := NewLRU[string](numItems)
	if err != nil {
		b.Fatal(err)
	}
	ttl, err := NewTTL[string](context.Background(), 5*time.Minute, 1*time.Minute)
	if err != nil {
		b.Fatal(err)
	}
	hybrid, err := newHybrid[string](context.Background(), numItems, 5*time.Minute, 1*time.Minute)
	if err != nil {
		b.Fatal(err)
	}

	benchmarks := []struct {
		name  string
		cache Cache[string]
	}{
		{"Simple", simple},
		{"LRU", lru},
		{"TTL", ttl},
		{"Hybrid", hybrid},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			cache := bm.cache
			defer cache.Close()

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				for j := 0; j < numItems; j++ {
					key := fmt.Sprintf("key%d_%d", i, j)
					value := fmt.Sprintf("value%d_%d", i, j)
					_, _ = cache.Set(key, value)
				}
				_ = cache.Clear()
			}
		})
	}
}

// BenchmarkConcurrentAccess benchmarks concurrent access patterns.
func BenchmarkConcurrentAccess(b *testing.B) {
	cache, err := NewLRU[string](1000)
	if err != nil {
		b.Fatal(err)
	}
	defer cache.Close()

	// Pre-populate
	for i := 0; i < 1000; i++ {
		_, _ = cache.Set(fmt.Sprintf("key%d", i), fmt.Sprintf("value%d", i))
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			// Mix of operations that would typically happen concurrently
			go func() {
				cache.Get(fmt.Sprintf("key%d", rand.Intn(1000)))
			}()

			go func() {
				_, _ = cache.Set(fmt.Sprintf("key%d", rand.Intn(2000)), "new_value")
			}()

			// Occasionally check size (read operation)
			if rand.Intn(100) == 0 {
				cache.Size()
			}
		}
	})
}

// BenchmarkConfigCreation benchmarks cache creation from configuration.
func BenchmarkConfigCreation(b *testing.B) {
	configs := []Config{
		{Enabled: true, Strategy: StrategySimple},
		{Enabled: true, Strategy: StrategyLRU, MaxSize: 1000},
		{Enabled: true, Strategy: StrategyTTL, TTL: 5 * time.Minute, CleanupInterval: 1 * time.Minute},
		{
			Enabled:         true,
			Strategy:        StrategyHybrid,
			MaxSize:         1000,
			TTL:             5 * time.Minute,
			CleanupInterval: 1 * time.Minute,
		},
	}

	for _, config := range configs {
		b.Run(string(config.Strategy), func(b *testing.B) {
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				cache, err := NewFromConfig[string](context.Background(), config)
				if err != nil {
					b.Fatal(err)
				}
				cache.Close()
			}
		})
	}
}

// Example benchmarks to demonstrate usage patterns.

// BenchmarkExample_ReadHeavy simulates a read-heavy workload (90% reads, 10% writes).
func BenchmarkExample_ReadHeavy(b *testing.B) {
	cache, err := NewLRU[string](1000)
	if err != nil {
		b.Fatal(err)
	}
	defer cache.Close()

	// Pre-populate
	for i := 0; i < 1000; i++ {
		_, _ = cache.Set(fmt.Sprintf("key%d", i), fmt.Sprintf("value%d", i))
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if rand.Intn(10) == 0 { // 10% writes
				key := fmt.Sprintf("key%d", rand.Intn(2000))
				_, _ = cache.Set(key, "updated_value")
			} else { // 90% reads
				key := fmt.Sprintf("key%d", rand.Intn(1000))
				cache.Get(key)
			}
		}
	})
}

// BenchmarkExample_WriteHeavy simulates a write-heavy workload (70% writes, 30% reads).
func BenchmarkExample_WriteHeavy(b *testing.B) {
	cache, err := NewLRU[string](1000)
	if err != nil {
		b.Fatal(err)
	}
	defer cache.Close()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			if rand.Intn(10) < 7 { // 70% writes
				key := fmt.Sprintf("key%d", i)
				_, _ = cache.Set(key, fmt.Sprintf("value%d", i))
				i++
			} else { // 30% reads
				key := fmt.Sprintf("key%d", rand.Intn(i+1))
				cache.Get(key)
			}
		}
	})
}
