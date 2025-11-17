package cache

import (
	"sync"

	"github.com/c360/semstreams/errors"
)

// simpleCache is a thread-safe cache with no eviction policy.
// It stores items indefinitely until explicitly deleted or cleared.
type simpleCache[V any] struct {
	mu      sync.RWMutex
	items   map[string]V
	stats   *Statistics      // ALWAYS initialized
	metrics *cacheMetrics    // Optional, if metrics enabled
	evictFn EvictCallback[V] // Optional callback
}

// newSimpleCache creates a new simple cache instance.
// Returns an error if metrics registration fails when requested.
func newSimpleCache[V any](opts *cacheOptions[V]) (*simpleCache[V], error) {
	// Stats are ALWAYS initialized - observability is not optional
	stats := NewStatistics()

	var metrics *cacheMetrics
	// Optionally expose stats as Prometheus metrics
	if opts.metricsReg != nil && opts.metricsPrefix != "" {
		var err error
		metrics, err = newCacheMetrics(opts.metricsReg, opts.metricsPrefix)
		if err != nil {
			// Return classified error instead of silently ignoring
			return nil, errors.WrapTransient(err, "cache", "newSimpleCache", "metrics registration")
		}
	}

	return &simpleCache[V]{
		items:   make(map[string]V),
		stats:   stats,   // ALWAYS present
		metrics: metrics, // Optional
		evictFn: opts.evictCallback,
	}, nil
}

// Get retrieves a value by key.
func (c *simpleCache[V]) Get(key string) (V, bool) {
	c.mu.RLock()
	value, exists := c.items[key]
	c.mu.RUnlock()

	// ALWAYS track in stats (observability is not optional)
	if exists {
		c.stats.Hit()
		// ALSO track in metrics if enabled
		if c.metrics != nil {
			c.metrics.recordHit()
		}
	} else {
		c.stats.Miss()
		// ALSO track in metrics if enabled
		if c.metrics != nil {
			c.metrics.recordMiss()
		}
	}

	return value, exists
}

// Set stores a value with the given key.
func (c *simpleCache[V]) Set(key string, value V) (bool, error) {
	// Validate key using framework pattern
	if err := validateKey(key); err != nil {
		return false, err
	}
	c.mu.Lock()
	_, exists := c.items[key]
	c.items[key] = value
	size := len(c.items)
	c.mu.Unlock()

	// ALWAYS track in stats (observability is not optional)
	c.stats.Set()
	c.stats.UpdateSize(int64(size))

	// ALSO track in metrics if enabled
	if c.metrics != nil {
		c.metrics.recordSet()
		c.metrics.updateSize(size)
	}

	return !exists, nil // true if new entry was created
}

// Delete removes an entry by key.
func (c *simpleCache[V]) Delete(key string) (bool, error) {
	// Validate key using framework pattern
	if err := validateKey(key); err != nil {
		return false, err
	}
	c.mu.Lock()
	value, exists := c.items[key]
	if exists {
		delete(c.items, key)
		if c.evictFn != nil {
			// Call eviction callback with the actual value that was stored
			defer c.evictFn(key, value)
		}
	}
	size := len(c.items)
	c.mu.Unlock()

	// ALWAYS track in stats if item was deleted
	if exists {
		c.stats.Delete()
		c.stats.UpdateSize(int64(size))

		// ALSO track in metrics if enabled
		if c.metrics != nil {
			c.metrics.recordDelete()
			c.metrics.updateSize(size)
		}
	}

	return exists, nil
}

// Clear removes all entries from the cache.
func (c *simpleCache[V]) Clear() error {
	c.mu.Lock()
	if c.evictFn != nil {
		// Call eviction callback for all items before clearing
		for key, value := range c.items {
			c.evictFn(key, value)
		}
	}
	c.items = make(map[string]V)
	c.mu.Unlock()

	// ALWAYS track size update in stats
	c.stats.UpdateSize(0)

	// ALSO track in metrics if enabled
	if c.metrics != nil {
		c.metrics.updateSize(0)
	}

	return nil
}

// Size returns the current number of entries in the cache.
func (c *simpleCache[V]) Size() int {
	c.mu.RLock()
	size := len(c.items)
	c.mu.RUnlock()
	return size
}

// Keys returns a slice of all keys currently in the cache.
func (c *simpleCache[V]) Keys() []string {
	c.mu.RLock()
	keys := make([]string, 0, len(c.items))
	for key := range c.items {
		keys = append(keys, key)
	}
	c.mu.RUnlock()
	return keys
}

// Stats returns cache statistics if enabled.
func (c *simpleCache[V]) Stats() *Statistics {
	return c.stats
}

// Close shuts down the cache. For simple cache, this is a no-op.
func (c *simpleCache[V]) Close() error {
	// Simple cache has no background goroutines to clean up
	return nil
}
