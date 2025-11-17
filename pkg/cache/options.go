package cache

import (
	"time"

	"github.com/c360/semstreams/metric"
)

// Option configures cache behavior using the functional options pattern.
// This provides a clean, extensible API for configuring caches.
type Option[V any] func(*cacheOptions[V])

// cacheOptions holds internal configuration for cache instances.
// Stats are ALWAYS collected - they are not optional.
// Metrics are optional and exposed via WithMetrics().
type cacheOptions[V any] struct {
	// metricsReg is optional - if provided, cache stats are also exposed as Prometheus metrics
	metricsReg *metric.MetricsRegistry

	// metricsPrefix is used as the component label for Prometheus metrics
	metricsPrefix string

	// evictCallback is called when items are evicted from the cache
	evictCallback EvictCallback[V]

	// statsInterval is how often to update aggregate statistics (for TTL/Hybrid caches)
	statsInterval time.Duration
}

// WithMetrics enables Prometheus metrics export for cache statistics.
// If registry is nil, this option is ignored.
// Registry should not be nil in normal usage - this handles edge cases gracefully.
func WithMetrics[V any](registry *metric.MetricsRegistry, prefix string) Option[V] {
	return func(opts *cacheOptions[V]) {
		if registry != nil && prefix != "" {
			opts.metricsReg = registry
			opts.metricsPrefix = prefix
		}
	}
}

// WithEvictionCallback sets a callback function that is called when items are evicted.
// The callback receives the key and value of the evicted entry.
func WithEvictionCallback[V any](callback EvictCallback[V]) Option[V] {
	return func(opts *cacheOptions[V]) {
		opts.evictCallback = callback
	}
}

// WithStatsInterval sets how often aggregate statistics are updated.
// This is only relevant for TTL and Hybrid caches with background cleanup.
// If interval is <= 0, this option is ignored.
func WithStatsInterval[V any](interval time.Duration) Option[V] {
	return func(opts *cacheOptions[V]) {
		if interval > 0 {
			opts.statsInterval = interval
		}
	}
}

// applyOptions applies functional options to create final cache configuration.
// This is an internal helper used by cache constructors.
func applyOptions[V any](options ...Option[V]) *cacheOptions[V] {
	opts := &cacheOptions[V]{
		// Default values
		statsInterval: 30 * time.Second,
	}

	for _, opt := range options {
		if opt != nil {
			opt(opts)
		}
	}

	return opts
}
