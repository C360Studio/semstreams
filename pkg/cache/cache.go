// Package cache provides generic, thread-safe cache implementations with various eviction policies.
//
// This package offers multiple cache types:
//   - SimpleCache: No eviction policy (stores items indefinitely)
//   - LRUCache: Least Recently Used eviction based on size
//   - TTLCache: Time-To-Live eviction based on expiry
//   - HybridCache: Combined LRU and TTL eviction
//
// All cache implementations are thread-safe with built-in statistics (always enabled for observability)
// and optional Prometheus metrics integration via functional options.
package cache

import (
	"context"
	"time"

	"github.com/c360/semstreams/errors"
)

// Cache represents a generic cache interface that all cache implementations must satisfy.
// The cache is parameterized by value type V for type safety.
type Cache[V any] interface {
	// Get retrieves a value by key. Returns the value and true if found, zero value and false otherwise.
	Get(key string) (V, bool)

	// Set stores a value with the given key. Returns true if a new entry was created, false if updated.
	// Returns an error if the operation fails (e.g., invalid key).
	Set(key string, value V) (bool, error)

	// Delete removes an entry by key. Returns true if the key existed and was deleted.
	// Returns an error if the operation fails.
	Delete(key string) (bool, error)

	// Clear removes all entries from the cache.
	// Returns an error if the operation fails.
	Clear() error

	// Size returns the current number of entries in the cache.
	Size() int

	// Keys returns a slice of all keys currently in the cache.
	Keys() []string

	// Stats returns cache statistics if enabled, nil otherwise.
	Stats() *Statistics

	// Close shuts down the cache and releases any resources (e.g., background goroutines).
	Close() error
}

// EvictCallback is called when an entry is evicted from the cache.
// It receives the key and value of the evicted entry.
type EvictCallback[V any] func(key string, value V)

// Entry represents an entry in the cache with metadata.
type Entry[V any] struct {
	Key        string
	Value      V // Stored value
	CreatedAt  time.Time
	ExpiresAt  *time.Time // nil means no expiration
	AccessedAt time.Time
}

// IsExpired checks if the entry has expired based on the current time.
func (e *Entry[V]) IsExpired() bool {
	if e.ExpiresAt == nil {
		return false
	}
	return time.Now().After(*e.ExpiresAt)
}

// Touch updates the last accessed time of the entry.
func (e *Entry[V]) Touch() {
	e.AccessedAt = time.Now()
}

// contextKey is used for context values in this package.
type contextKey string

const (
	// ContextKeyStats can be used to pass statistics through context.
	ContextKeyStats contextKey = "cache-stats"
)

// WithStats adds statistics to the context.
func WithStats(ctx context.Context, stats *Statistics) context.Context {
	return context.WithValue(ctx, ContextKeyStats, stats)
}

// StatsFromContext retrieves statistics from the context.
func StatsFromContext(ctx context.Context) (*Statistics, bool) {
	stats, ok := ctx.Value(ContextKeyStats).(*Statistics)
	return stats, ok
}

// validateKey validates a cache key for basic requirements.
// Returns a classified error if the key is invalid.
func validateKey(key string) error {
	if key == "" {
		return errors.WrapInvalid(errors.ErrInvalidData, "cache", "validateKey", "key cannot be empty")
	}
	// Additional validations can be added here as needed
	return nil
}
