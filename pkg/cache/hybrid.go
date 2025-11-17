package cache

import (
	"container/list"
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/c360/semstreams/errors"
)

// hybridEntry represents an entry in the Hybrid cache.
type hybridEntry[V any] struct {
	key       string
	value     V
	expiresAt time.Time
}

// isExpired checks if the entry has expired.
func (e *hybridEntry[V]) isExpired() bool {
	return time.Now().After(e.expiresAt)
}

// hybridCache combines LRU and TTL eviction policies.
// Items are evicted either when the cache reaches maximum size (LRU)
// or when items expire (TTL), whichever comes first.
type hybridCache[V any] struct {
	mu              sync.RWMutex
	maxSize         int
	ttl             time.Duration
	cleanupInterval time.Duration
	items           map[string]*list.Element // key -> list element
	order           *list.List               // doubly-linked list for LRU ordering
	stats           *Statistics              // ALWAYS initialized
	metrics         *cacheMetrics            // Optional, if metrics enabled
	evictFn         EvictCallback[V]         // Optional callback
	statsInterval   time.Duration            // Stats update interval

	// Background cleanup coordination
	shutdown chan struct{}
	done     chan struct{}
}

// newHybridCache creates a new hybrid cache with LRU and TTL policies.
// Returns an error if metrics registration fails when requested.
func newHybridCache[V any](
	ctx context.Context, maxSize int, ttl, cleanupInterval time.Duration, opts *cacheOptions[V],
) (*hybridCache[V], error) {
	// Stats are ALWAYS initialized - observability is not optional
	stats := NewStatistics()

	var metrics *cacheMetrics
	// Optionally expose stats as Prometheus metrics
	if opts.metricsReg != nil && opts.metricsPrefix != "" {
		var err error
		metrics, err = newCacheMetrics(opts.metricsReg, opts.metricsPrefix)
		if err != nil {
			// Return classified error instead of silently ignoring
			return nil, errors.WrapTransient(err, "cache", "newHybridCache", "metrics registration")
		}
	}

	c := &hybridCache[V]{
		maxSize:         maxSize,
		ttl:             ttl,
		cleanupInterval: cleanupInterval,
		items:           make(map[string]*list.Element),
		order:           list.New(),
		stats:           stats,   // ALWAYS present
		metrics:         metrics, // Optional
		evictFn:         opts.evictCallback,
		statsInterval:   opts.statsInterval,
		shutdown:        make(chan struct{}),
		done:            make(chan struct{}),
	}

	// Start background cleanup goroutine for TTL with caller's context
	go c.cleanup(ctx)

	return c, nil
}

// Get retrieves a value by key, checking for expiration and updating LRU order.
func (c *hybridCache[V]) Get(key string) (V, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	element, exists := c.items[key]
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

	entry := element.Value.(*hybridEntry[V])

	// Check if expired
	if entry.isExpired() {
		// Remove expired entry
		c.removeElement(element)
		// ALWAYS track eviction and miss in stats (observability is not optional)
		c.stats.Eviction()
		c.stats.Miss()
		c.stats.UpdateSize(int64(len(c.items)))
		// ALSO track in metrics if enabled
		if c.metrics != nil {
			c.metrics.recordEviction()
			c.metrics.recordMiss()
			c.metrics.updateSize(len(c.items))
		}

		var zero V
		return zero, false
	}

	// Move to front (most recently used)
	c.order.MoveToFront(element)

	// ALWAYS track hit in stats (observability is not optional)
	c.stats.Hit()
	// ALSO track in metrics if enabled
	if c.metrics != nil {
		c.metrics.recordHit()
	}

	return entry.value, true
}

// Set stores a value with the given key, setting TTL and updating LRU order.
func (c *hybridCache[V]) Set(key string, value V) (bool, error) {
	// Validate key using framework pattern
	if err := validateKey(key); err != nil {
		return false, err
	}
	expiresAt := time.Now().Add(c.ttl)

	c.mu.Lock()
	defer c.mu.Unlock()

	// Check if key already exists
	if element, exists := c.items[key]; exists {
		// Update existing entry
		entry := element.Value.(*hybridEntry[V])
		entry.value = value
		entry.expiresAt = expiresAt
		c.order.MoveToFront(element)

		// ALWAYS track in stats (observability is not optional)
		c.stats.Set()
		// ALSO track in metrics if enabled
		if c.metrics != nil {
			c.metrics.recordSet()
		}
		return false, nil // existing entry was updated
	}

	// Create new entry
	entry := &hybridEntry[V]{
		key:       key,
		value:     value,
		expiresAt: expiresAt,
	}
	element := c.order.PushFront(entry)
	c.items[key] = element

	// Check if we need to evict for size (LRU policy)
	if len(c.items) > c.maxSize {
		c.evictLRU()
	}

	// ALWAYS track in stats (observability is not optional)
	c.stats.Set()
	c.stats.UpdateSize(int64(len(c.items)))
	// ALSO track in metrics if enabled
	if c.metrics != nil {
		c.metrics.recordSet()
		c.metrics.updateSize(len(c.items))
	}

	return true, nil // new entry was created
}

// Delete removes an entry by key.
func (c *hybridCache[V]) Delete(key string) (bool, error) {
	// Validate key using framework pattern
	if err := validateKey(key); err != nil {
		return false, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	element, exists := c.items[key]
	if !exists {
		return false, nil
	}

	c.removeElement(element)

	// ALWAYS track in stats (observability is not optional)
	c.stats.Delete()
	c.stats.UpdateSize(int64(len(c.items)))
	// ALSO track in metrics if enabled
	if c.metrics != nil {
		c.metrics.recordDelete()
		c.metrics.updateSize(len(c.items))
	}

	return true, nil
}

// Clear removes all entries from the cache.
func (c *hybridCache[V]) Clear() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.evictFn != nil {
		// Call OnEvict for all items
		for element := c.order.Back(); element != nil; element = element.Prev() {
			entry := element.Value.(*hybridEntry[V])
			c.evictFn(entry.key, entry.value)
		}
	}

	c.items = make(map[string]*list.Element)
	c.order.Init()

	// ALWAYS track size update in stats
	c.stats.UpdateSize(0)
	// ALSO track in metrics if enabled
	if c.metrics != nil {
		c.metrics.updateSize(0)
	}

	return nil
}

// Size returns the current number of entries in the cache.
func (c *hybridCache[V]) Size() int {
	c.mu.RLock()
	size := len(c.items)
	c.mu.RUnlock()
	return size
}

// Keys returns a slice of all keys currently in the cache.
// Keys are returned in LRU order (most recently used first).
// Note: Some keys may be expired but not yet cleaned up.
func (c *hybridCache[V]) Keys() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	keys := make([]string, 0, len(c.items))
	now := time.Now()

	for element := c.order.Front(); element != nil; element = element.Next() {
		entry := element.Value.(*hybridEntry[V])
		if now.Before(entry.expiresAt) {
			keys = append(keys, entry.key)
		}
	}
	return keys
}

// Stats returns cache statistics if enabled.
func (c *hybridCache[V]) Stats() *Statistics {
	return c.stats
}

// Close shuts down the cache and stops the background cleanup goroutine.
func (c *hybridCache[V]) Close() error {
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

// evictLRU removes the least recently used item from the cache.
// Must be called with mutex held.
func (c *hybridCache[V]) evictLRU() {
	element := c.order.Back()
	if element != nil {
		c.removeElement(element)
		// ALWAYS track eviction in stats (observability is not optional)
		c.stats.Eviction()
		// ALSO track in metrics if enabled
		if c.metrics != nil {
			c.metrics.recordEviction()
		}
	}
}

// removeElement removes an element from both the list and map.
// Must be called with mutex held.
func (c *hybridCache[V]) removeElement(element *list.Element) {
	entry := element.Value.(*hybridEntry[V])
	delete(c.items, entry.key)
	c.order.Remove(element)

	if c.evictFn != nil {
		// Call OnEvict callback outside of critical section
		defer c.evictFn(entry.key, entry.value)
	}
}

// cleanup runs in a background goroutine and periodically removes expired entries.
func (c *hybridCache[V]) cleanup(ctx context.Context) {
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
func (c *hybridCache[V]) removeExpired() {
	now := time.Now()
	var expiredElements []*list.Element

	c.mu.Lock()

	// Walk through the list and find expired elements
	for element := c.order.Front(); element != nil; {
		next := element.Next()
		entry := element.Value.(*hybridEntry[V])

		if now.After(entry.expiresAt) {
			expiredElements = append(expiredElements, element)
			delete(c.items, entry.key)
			c.order.Remove(element)
		}

		element = next
	}

	size := len(c.items)
	c.mu.Unlock()

	// Call OnEvict callbacks outside the lock
	if c.evictFn != nil {
		for _, element := range expiredElements {
			entry := element.Value.(*hybridEntry[V])
			c.evictFn(entry.key, entry.value)
		}
	}

	// Update statistics
	if len(expiredElements) > 0 {
		// ALWAYS track evictions in stats
		for range expiredElements {
			c.stats.Eviction()
		}
		c.stats.UpdateSize(int64(size))
		// ALSO track in metrics if enabled
		if c.metrics != nil {
			for range expiredElements {
				c.metrics.recordEviction()
			}
			c.metrics.updateSize(size)
		}
	}
}
