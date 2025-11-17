package cache

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/c360/semstreams/errors"
)

// ttlEntry represents an entry in the TTL cache.
type ttlEntry[V any] struct {
	key       string
	value     V
	expiresAt time.Time
}

// isExpired checks if the entry has expired.
func (e *ttlEntry[V]) isExpired() bool {
	return time.Now().After(e.expiresAt)
}

// ttlCache is a thread-safe TTL (Time-To-Live) cache implementation.
// It automatically evicts items when they expire based on their TTL.
type ttlCache[V any] struct {
	mu              sync.RWMutex
	ttl             time.Duration
	cleanupInterval time.Duration
	items           map[string]*ttlEntry[V]
	stats           *Statistics      // ALWAYS initialized
	metrics         *cacheMetrics    // Optional, if metrics enabled
	evictFn         EvictCallback[V] // Optional callback
	statsInterval   time.Duration    // Stats update interval

	// Background cleanup coordination
	shutdown chan struct{}
	done     chan struct{}
}

// newTTLCache creates a new TTL cache with the specified TTL and cleanup interval.
// Returns an error if metrics registration fails when requested.
func newTTLCache[V any](
	ctx context.Context, ttl, cleanupInterval time.Duration, opts *cacheOptions[V],
) (*ttlCache[V], error) {
	// Stats are ALWAYS initialized - observability is not optional
	stats := NewStatistics()

	var metrics *cacheMetrics
	// Optionally expose stats as Prometheus metrics
	if opts.metricsReg != nil && opts.metricsPrefix != "" {
		var err error
		metrics, err = newCacheMetrics(opts.metricsReg, opts.metricsPrefix)
		if err != nil {
			// Return classified error instead of silently ignoring
			return nil, errors.WrapTransient(err, "cache", "newTTLCache", "metrics registration")
		}
	}

	c := &ttlCache[V]{
		ttl:             ttl,
		cleanupInterval: cleanupInterval,
		items:           make(map[string]*ttlEntry[V]),
		stats:           stats,   // ALWAYS present
		metrics:         metrics, // Optional
		evictFn:         opts.evictCallback,
		statsInterval:   opts.statsInterval,
		shutdown:        make(chan struct{}),
		done:            make(chan struct{}),
	}

	// Start background cleanup goroutine with caller's context
	go c.cleanup(ctx)

	return c, nil
}

// Get retrieves a value by key, checking for expiration.
func (c *ttlCache[V]) Get(key string) (V, bool) {
	c.mu.RLock()
	entry, exists := c.items[key]
	c.mu.RUnlock()

	if !exists {
		var zero V
		// ALWAYS track in stats (observability is not optional)
		c.stats.Miss()
		// ALSO track in metrics if enabled
		if c.metrics != nil {
			c.metrics.recordMiss()
		}
		return zero, false
	}

	// Check if expired
	if entry.isExpired() {
		// Remove expired entry
		c.mu.Lock()
		// Double-check it's still there and still expired
		if currentEntry, stillExists := c.items[key]; stillExists && currentEntry.isExpired() {
			delete(c.items, key)
			if c.evictFn != nil {
				defer c.evictFn(key, currentEntry.value)
			}
			// ALWAYS track eviction in stats (observability is not optional)
			c.stats.Eviction()
			c.stats.UpdateSize(int64(len(c.items)))
			// ALSO track in metrics if enabled
			if c.metrics != nil {
				c.metrics.recordEviction()
				c.metrics.updateSize(len(c.items))
			}
		}
		c.mu.Unlock()

		var zero V
		// ALWAYS track in stats (observability is not optional)
		c.stats.Miss()
		// ALSO track in metrics if enabled
		if c.metrics != nil {
			c.metrics.recordMiss()
		}
		return zero, false
	}

	// ALWAYS track in stats (observability is not optional)
	c.stats.Hit()
	// ALSO track in metrics if enabled
	if c.metrics != nil {
		c.metrics.recordHit()
	}

	return entry.value, true
}

// Set stores a value with the given key and sets its expiration time.
func (c *ttlCache[V]) Set(key string, value V) (bool, error) {
	// Validate key using framework pattern
	if err := validateKey(key); err != nil {
		return false, err
	}
	expiresAt := time.Now().Add(c.ttl)

	c.mu.Lock()
	_, exists := c.items[key]
	c.items[key] = &ttlEntry[V]{
		key:       key,
		value:     value,
		expiresAt: expiresAt,
	}
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
func (c *ttlCache[V]) Delete(key string) (bool, error) {
	// Validate key using framework pattern
	if err := validateKey(key); err != nil {
		return false, err
	}
	c.mu.Lock()
	entry, exists := c.items[key]
	if exists {
		delete(c.items, key)
		if c.evictFn != nil {
			defer c.evictFn(key, entry.value)
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
func (c *ttlCache[V]) Clear() error {
	c.mu.Lock()
	if c.evictFn != nil {
		// Call OnEvict for all items
		for _, entry := range c.items {
			c.evictFn(entry.key, entry.value)
		}
	}
	c.items = make(map[string]*ttlEntry[V])
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
func (c *ttlCache[V]) Size() int {
	c.mu.RLock()
	size := len(c.items)
	c.mu.RUnlock()
	return size
}

// Keys returns a slice of all keys currently in the cache.
// Note: Some keys may be expired but not yet cleaned up.
func (c *ttlCache[V]) Keys() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	keys := make([]string, 0, len(c.items))
	now := time.Now()
	for key, entry := range c.items {
		if now.Before(entry.expiresAt) {
			keys = append(keys, key)
		}
	}
	return keys
}

// Stats returns cache statistics if enabled.
func (c *ttlCache[V]) Stats() *Statistics {
	return c.stats
}

// Close shuts down the cache and stops the background cleanup goroutine.
func (c *ttlCache[V]) Close() error {
	// Signal shutdown via channel
	select {
	case <-c.shutdown:
		// Already shutting down
	default:
		close(c.shutdown)
	}

	// Wait for cleanup goroutine to finish with timeout
	select {
	case <-c.done:
		return nil
	case <-time.After(5 * time.Second):
		return fmt.Errorf("timeout waiting for cleanup goroutine to finish")
	}
}

// cleanup runs in a background goroutine and periodically removes expired entries.
func (c *ttlCache[V]) cleanup(ctx context.Context) {
	defer close(c.done)

	ticker := time.NewTicker(c.cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.shutdown:
			return
		case <-ticker.C:
			c.removeExpired()
		}
	}
}

// removeExpired removes all expired entries from the cache.
func (c *ttlCache[V]) removeExpired() {
	now := time.Now()
	var expiredEntries []*ttlEntry[V]

	c.mu.Lock()
	for key, entry := range c.items {
		if now.After(entry.expiresAt) {
			expiredEntries = append(expiredEntries, entry)
			delete(c.items, key)
		}
	}
	size := len(c.items)
	c.mu.Unlock()

	// Call OnEvict callbacks outside the lock
	if c.evictFn != nil {
		for _, entry := range expiredEntries {
			c.evictFn(entry.key, entry.value)
		}
	}

	// Update statistics
	if len(expiredEntries) > 0 {
		// ALWAYS track evictions in stats
		for range expiredEntries {
			c.stats.Eviction()
		}
		c.stats.UpdateSize(int64(size))
		// ALSO track in metrics if enabled
		if c.metrics != nil {
			for range expiredEntries {
				c.metrics.recordEviction()
			}
			c.metrics.updateSize(size)
		}
	}
}
