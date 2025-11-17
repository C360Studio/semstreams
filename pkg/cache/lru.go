package cache

import (
	"container/list"
	"sync"

	"github.com/c360/semstreams/errors"
)

// lruEntry represents an entry in the LRU cache.
type lruEntry[V any] struct {
	key   string
	value V
}

// lruCache is a thread-safe LRU (Least Recently Used) cache implementation.
// It evicts the least recently used items when the maximum size is exceeded.
type lruCache[V any] struct {
	mu      sync.RWMutex
	maxSize int
	items   map[string]*list.Element // key -> list element
	order   *list.List               // doubly-linked list for LRU ordering
	stats   *Statistics              // ALWAYS initialized
	metrics *cacheMetrics            // Optional, if metrics enabled
	evictFn EvictCallback[V]         // Optional callback
}

// newLRUCache creates a new LRU cache with the specified maximum size.
// Returns an error if metrics registration fails when requested.
func newLRUCache[V any](maxSize int, opts *cacheOptions[V]) (*lruCache[V], error) {
	// Stats are ALWAYS initialized - observability is not optional
	stats := NewStatistics()

	var metrics *cacheMetrics
	// Optionally expose stats as Prometheus metrics
	if opts.metricsReg != nil && opts.metricsPrefix != "" {
		var err error
		metrics, err = newCacheMetrics(opts.metricsReg, opts.metricsPrefix)
		if err != nil {
			// Return classified error instead of silently ignoring
			return nil, errors.WrapTransient(err, "cache", "newLRUCache", "metrics registration")
		}
	}

	return &lruCache[V]{
		maxSize: maxSize,
		items:   make(map[string]*list.Element),
		order:   list.New(),
		stats:   stats,   // ALWAYS present
		metrics: metrics, // Optional
		evictFn: opts.evictCallback,
	}, nil
}

// Get retrieves a value by key and marks it as recently used.
func (c *lruCache[V]) Get(key string) (V, bool) {
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

	// Move to front (most recently used)
	c.order.MoveToFront(element)

	entry := element.Value.(*lruEntry[V])
	// ALWAYS track in stats (observability is not optional)
	c.stats.Hit()
	// ALSO track in metrics if enabled
	if c.metrics != nil {
		c.metrics.recordHit()
	}

	return entry.value, true
}

// Set stores a value with the given key and marks it as recently used.
func (c *lruCache[V]) Set(key string, value V) (bool, error) {
	// Validate key using framework pattern
	if err := validateKey(key); err != nil {
		return false, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	// Check if key already exists
	if element, exists := c.items[key]; exists {
		// Update existing entry
		entry := element.Value.(*lruEntry[V])
		entry.value = value
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
	entry := &lruEntry[V]{key: key, value: value}
	element := c.order.PushFront(entry)
	c.items[key] = element

	// Check if we need to evict
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
func (c *lruCache[V]) Delete(key string) (bool, error) {
	// Validate key using framework pattern
	if err := validateKey(key); err != nil {
		return false, err
	}

	var evictKey string
	var evictValue V
	var shouldEvict bool

	c.mu.Lock()
	element, exists := c.items[key]
	if !exists {
		c.mu.Unlock()
		return false, nil
	}

	// Capture eviction data before removing
	if c.evictFn != nil {
		entry := element.Value.(*lruEntry[V])
		evictKey = entry.key
		evictValue = entry.value
		shouldEvict = true
	}

	c.removeElementUnsafe(element)

	// ALWAYS track in stats (observability is not optional)
	c.stats.Delete()
	c.stats.UpdateSize(int64(len(c.items)))

	// ALSO track in metrics if enabled
	if c.metrics != nil {
		c.metrics.recordDelete()
		c.metrics.updateSize(len(c.items))
	}

	c.mu.Unlock()

	// Call eviction callback outside lock to prevent deadlock
	if shouldEvict {
		c.evictFn(evictKey, evictValue)
	}

	return true, nil
}

// Clear removes all entries from the cache.
func (c *lruCache[V]) Clear() error {
	// Collect items to evict before releasing lock
	var evictItems []lruEntry[V]

	c.mu.Lock()
	if c.evictFn != nil {
		evictItems = make([]lruEntry[V], 0, len(c.items))
		for element := c.order.Back(); element != nil; element = element.Prev() {
			entry := element.Value.(*lruEntry[V])
			evictItems = append(evictItems, *entry)
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
	c.mu.Unlock()

	// Call eviction callbacks outside lock to prevent deadlock
	if c.evictFn != nil {
		for _, entry := range evictItems {
			c.evictFn(entry.key, entry.value)
		}
	}

	return nil
}

// Size returns the current number of entries in the cache.
func (c *lruCache[V]) Size() int {
	c.mu.RLock()
	size := len(c.items)
	c.mu.RUnlock()
	return size
}

// Keys returns a slice of all keys currently in the cache.
// Keys are returned in LRU order (most recently used first).
func (c *lruCache[V]) Keys() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	keys := make([]string, 0, len(c.items))
	for element := c.order.Front(); element != nil; element = element.Next() {
		entry := element.Value.(*lruEntry[V])
		keys = append(keys, entry.key)
	}
	return keys
}

// Stats returns cache statistics if enabled.
func (c *lruCache[V]) Stats() *Statistics {
	return c.stats
}

// Close shuts down the cache. For LRU cache, this is a no-op.
func (c *lruCache[V]) Close() error {
	// LRU cache has no background goroutines to clean up
	return nil
}

// evictLRU removes the least recently used item from the cache.
// Must be called with mutex held.
func (c *lruCache[V]) evictLRU() {
	element := c.order.Back()
	if element == nil {
		return
	}

	// Capture eviction data before removing
	var evictKey string
	var evictValue V
	var shouldEvict bool

	if c.evictFn != nil {
		entry := element.Value.(*lruEntry[V])
		evictKey = entry.key
		evictValue = entry.value
		shouldEvict = true
	}

	c.removeElementUnsafe(element)

	// ALWAYS track eviction in stats (observability is not optional)
	c.stats.Eviction()
	// ALSO track in metrics if enabled
	if c.metrics != nil {
		c.metrics.recordEviction()
	}

	// Temporarily release lock to call eviction callback
	c.mu.Unlock()
	if shouldEvict {
		c.evictFn(evictKey, evictValue)
	}
	c.mu.Lock()
}

// removeElementUnsafe removes an element from both the list and map.
// Must be called with mutex held. Does NOT call eviction callback - caller is responsible.
func (c *lruCache[V]) removeElementUnsafe(element *list.Element) {
	entry := element.Value.(*lruEntry[V])
	delete(c.items, entry.key)
	c.order.Remove(element)
}
