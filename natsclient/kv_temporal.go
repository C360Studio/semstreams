package natsclient

import (
	"context"
	"fmt"
	"time"

	"github.com/c360/semstreams/pkg/cache"
	"github.com/nats-io/nats.go/jetstream"
)

// TemporalResolver provides efficient timestamp-based KV queries with caching
type TemporalResolver struct {
	bucket       jetstream.KeyValue
	historyCache cache.Cache[[]jetstream.KeyValueEntry] // TTL cache for history entries
}

// NewTemporalResolver creates a resolver for timestamp-based queries
// The context is used for the cache background cleanup goroutine lifecycle
func NewTemporalResolver(ctx context.Context, bucket jetstream.KeyValue) *TemporalResolver {
	// Use TTL cache with 5-second expiration for history entries
	// This eliminates ~100 lines of custom cache code
	histCache, err := cache.NewTTL[[]jetstream.KeyValueEntry](
		ctx,
		5*time.Second, // 5-second TTL for cached histories
		1*time.Second, // Cleanup expired entries every second
		cache.WithEvictionCallback(func(_ string, _ []jetstream.KeyValueEntry) {
			// Optional: Could log evictions for debugging
			// logger.Debug("Evicted history cache", "key", key, "entries", len(entries))
		}),
	)
	if err != nil {
		// For NewTemporalResolver, we'll panic on cache creation failure since it's a fundamental dependency
		panic(fmt.Sprintf("failed to create temporal resolver cache: %v", err))
	}

	return &TemporalResolver{
		bucket:       bucket,
		historyCache: histCache,
	}
}

// NewTemporalResolverWithCache creates a resolver with custom cache TTL
// The context is used for the cache background cleanup goroutine lifecycle
func NewTemporalResolverWithCache(
	ctx context.Context,
	bucket jetstream.KeyValue,
	cacheTTL time.Duration,
) *TemporalResolver {
	// Use TTL cache with custom expiration
	cleanupInterval := cacheTTL / 5 // Clean up at 1/5 of TTL
	if cleanupInterval < 1*time.Second {
		cleanupInterval = 1 * time.Second
	}

	histCache, err := cache.NewTTL[[]jetstream.KeyValueEntry](
		ctx,
		cacheTTL,
		cleanupInterval,
		cache.WithEvictionCallback(func(_ string, _ []jetstream.KeyValueEntry) {
			// Optional: Could log evictions for debugging
		}),
	)
	if err != nil {
		// For NewTemporalResolverWithCache, we'll panic on cache creation failure since it's a fundamental dependency
		panic(fmt.Sprintf("failed to create temporal resolver cache: %v", err))
	}

	return &TemporalResolver{
		bucket:       bucket,
		historyCache: histCache,
	}
}

// getCachedHistory retrieves history from cache or fetches it
func (tr *TemporalResolver) getCachedHistory(ctx context.Context, key string) ([]jetstream.KeyValueEntry, error) {
	// Check cache first - the cache package handles TTL automatically
	if cached, found := tr.historyCache.Get(key); found {
		// Cache hit - stats are automatically tracked by the cache package
		return cached, nil
	}

	// Cache miss - fetch from bucket
	history, err := tr.bucket.History(ctx, key)
	if err != nil {
		return nil, err
	}

	// Update cache - the cache package handles TTL and eviction automatically
	tr.historyCache.Set(key, history)

	return history, nil
}

// GetAtTimestamp finds the entity state that was current at the given timestamp
// Uses binary search for O(log n) performance with caching
func (tr *TemporalResolver) GetAtTimestamp(
	ctx context.Context,
	key string,
	targetTime time.Time,
) (jetstream.KeyValueEntry, error) {
	// Get history (from cache if available)
	history, err := tr.getCachedHistory(ctx, key)
	if err != nil {
		// Check if it's a key not found error from NATS
		if jetstream.ErrKeyNotFound != nil && err == jetstream.ErrKeyNotFound {
			return nil, ErrKVKeyNotFound
		}
		// Also check for string match in case the error is different
		if err.Error() == "nats: key not found" {
			return nil, ErrKVKeyNotFound
		}
		return nil, fmt.Errorf("get history: %w", err)
	}

	if len(history) == 0 {
		return nil, ErrKVKeyNotFound
	}

	// Quick boundary checks
	if targetTime.Before(history[0].Created()) {
		// Target is before any history - return oldest
		return history[0], nil
	}

	lastIdx := len(history) - 1
	if targetTime.After(history[lastIdx].Created()) || targetTime.Equal(history[lastIdx].Created()) {
		// Target is at or after latest - return newest
		return history[lastIdx], nil
	}

	// Binary search for the right revision
	left, right := 0, lastIdx
	for left < right {
		mid := left + (right-left+1)/2 // Bias toward right for "floor" behavior

		if history[mid].Created().After(targetTime) {
			// This entry is too new
			right = mid - 1
		} else {
			// This entry is valid (created before or at target)
			left = mid
		}
	}

	return history[left], nil
}

// GetRangeAtTimestamp finds multiple entities at a specific timestamp
// Useful for reconstructing entire system state at time T
func (tr *TemporalResolver) GetRangeAtTimestamp(
	ctx context.Context,
	keys []string,
	targetTime time.Time,
) (map[string]jetstream.KeyValueEntry, error) {
	results := make(map[string]jetstream.KeyValueEntry)

	// Process each key
	for _, key := range keys {
		entry, err := tr.GetAtTimestamp(ctx, key, targetTime)
		if err != nil {
			if err == ErrKVKeyNotFound {
				// Key didn't exist at target time - skip
				continue
			}
			// Other error - fail
			return nil, fmt.Errorf("get %s at timestamp: %w", key, err)
		}
		results[key] = entry
	}

	return results, nil
}

// GetInTimeRange finds all entity states within a time range
// Returns states ordered by timestamp
func (tr *TemporalResolver) GetInTimeRange(
	ctx context.Context,
	key string,
	startTime, endTime time.Time,
) ([]jetstream.KeyValueEntry, error) {
	// Get full history (from cache if available)
	history, err := tr.getCachedHistory(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("get history: %w", err)
	}

	// Filter entries within the time range
	var results []jetstream.KeyValueEntry
	for _, entry := range history {
		created := entry.Created()
		if created.After(startTime) && (created.Before(endTime) || created.Equal(endTime)) {
			results = append(results, entry)
		}
	}

	return results, nil
}

// GetRangeInTimeRange finds multiple entities within a time range
// Returns a map of key -> entries within the range
func (tr *TemporalResolver) GetRangeInTimeRange(
	ctx context.Context,
	keys []string,
	startTime, endTime time.Time,
) (map[string][]jetstream.KeyValueEntry, error) {
	results := make(map[string][]jetstream.KeyValueEntry)

	// Process each key
	for _, key := range keys {
		entries, err := tr.GetInTimeRange(ctx, key, startTime, endTime)
		if err != nil {
			// Skip keys that don't exist
			if err == ErrKVKeyNotFound {
				continue
			}
			return nil, fmt.Errorf("get %s in range: %w", key, err)
		}
		if len(entries) > 0 {
			results[key] = entries
		}
	}

	return results, nil
}

// GetStats returns cache statistics for monitoring
func (tr *TemporalResolver) GetStats() *cache.Statistics {
	return tr.historyCache.Stats()
}

// Close shuts down the temporal resolver and its cache
func (tr *TemporalResolver) Close() error {
	return tr.historyCache.Close()
}
