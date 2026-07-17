package cache

import (
	"context"
	"strings"
	"sync"
	"time"
)

// CoalescingSet collects keys over a time window and fires a callback with the batch.
// It deduplicates keys automatically using a map-based set structure.
// Follows the ticker-based background goroutine pattern from ttl.go.
type CoalescingSet struct {
	pending   map[string]struct{}
	mu        sync.Mutex
	window    time.Duration
	callback  func(keys []string)
	ticker    *time.Ticker
	shutdown  chan struct{}
	done      chan struct{}
	closeOnce sync.Once
}

// NewCoalescingSet creates a new CoalescingSet that fires the callback every window duration
// with the collected (deduplicated) keys. The background goroutine stops when ctx is cancelled
// or when Close() is called.
func NewCoalescingSet(ctx context.Context, window time.Duration, callback func([]string)) *CoalescingSet {
	c := &CoalescingSet{
		pending:  make(map[string]struct{}),
		window:   window,
		callback: callback,
		shutdown: make(chan struct{}),
		done:     make(chan struct{}),
	}

	// Handle zero or negative window by using minimum ticker duration
	tickerDuration := window
	if tickerDuration <= 0 {
		tickerDuration = 1 * time.Nanosecond
	}

	c.ticker = time.NewTicker(tickerDuration)

	// Start background goroutine
	go c.run(ctx)

	return c
}

// Add adds a key to the pending set and reports whether it was newly inserted.
// If the key already exists, it is deduplicated. Thread-safe.
func (c *CoalescingSet) Add(key string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.pending[key]; exists {
		return false
	}
	c.pending[key] = struct{}{}
	return true
}

// Remove removes a key from the pending set and reports whether it existed.
// This is useful when an entity is deleted before the window expires.
// Thread-safe.
func (c *CoalescingSet) Remove(key string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.pending[key]; !exists {
		return false
	}
	delete(c.pending, key)
	return true
}

// RemovePrefix removes every pending key with the supplied prefix and returns
// the number removed.
// This is useful when several provenance-bearing work items belong to the
// same logical entity and a delete must retire all of them atomically.
func (c *CoalescingSet) RemovePrefix(prefix string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	removed := 0
	for key := range c.pending {
		if strings.HasPrefix(key, prefix) {
			delete(c.pending, key)
			removed++
		}
	}
	return removed
}

// PendingCount returns the number of keys currently pending in the set.
// Thread-safe.
func (c *CoalescingSet) PendingCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.pending)
}

// Drain removes and returns every pending key without invoking the callback.
// Callers use this after Close when queued work owns external resources that
// must be released during shutdown.
func (c *CoalescingSet) Drain() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	keys := make([]string, 0, len(c.pending))
	for key := range c.pending {
		keys = append(keys, key)
	}
	c.pending = make(map[string]struct{})
	return keys
}

// Close stops the background ticker and waits for cleanup to complete.
// It is idempotent - multiple calls are safe.
func (c *CoalescingSet) Close() error {
	// Use sync.Once to make Close() safe for concurrent calls
	c.closeOnce.Do(func() {
		close(c.shutdown)
	})

	// Wait for background goroutine to finish
	<-c.done

	return nil
}

// run is the background goroutine that fires the callback on each tick.
// Follows the pattern from ttl.go cleanup method.
func (c *CoalescingSet) run(ctx context.Context) {
	defer close(c.done)
	defer c.ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.shutdown:
			return
		case <-c.ticker.C:
			c.fireBatch()
		}
	}
}

// fireBatch collects the current pending keys, clears the set, and calls the callback.
// The callback is invoked OUTSIDE the lock to prevent deadlocks.
func (c *CoalescingSet) fireBatch() {
	// Lock to read and clear pending keys
	c.mu.Lock()
	if len(c.pending) == 0 {
		c.mu.Unlock()
		// Skip callback if no keys pending
		return
	}

	// Copy keys to slice
	keys := make([]string, 0, len(c.pending))
	for k := range c.pending {
		keys = append(keys, k)
	}

	// Clear the pending set for next window
	c.pending = make(map[string]struct{})
	c.mu.Unlock()

	// Call callback OUTSIDE the lock to prevent deadlock
	// Wrap in defer/recover to handle panics gracefully
	if c.callback != nil {
		func() {
			defer func() {
				if r := recover(); r != nil {
					// Callback panicked - recover gracefully and continue
					// In production, this could log the panic
					_ = r
				}
			}()
			c.callback(keys)
		}()
	}
}
