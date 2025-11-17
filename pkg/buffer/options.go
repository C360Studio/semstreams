package buffer

import (
	"github.com/c360/semstreams/metric"
)

// Option configures buffer behavior using the functional options pattern.
// This provides a clean, extensible API for configuring buffers.
type Option[T any] func(*bufferOptions[T])

// bufferOptions holds internal configuration for buffer instances.
// Stats are ALWAYS collected - they are not optional.
// Metrics are optional and exposed via WithMetrics().
type bufferOptions[T any] struct {
	// Stats are ALWAYS collected - not an option
	overflowPolicy OverflowPolicy
	dropCallback   DropCallback[T]

	// metricsReg is optional - if provided, buffer stats are also exposed as Prometheus metrics
	metricsReg *metric.MetricsRegistry

	// metricsPrefix is used as the component label for Prometheus metrics
	metricsPrefix string
}

// WithOverflowPolicy sets the overflow behavior for the buffer.
// Defaults to DropOldest if not specified.
func WithOverflowPolicy[T any](policy OverflowPolicy) Option[T] {
	return func(opts *bufferOptions[T]) {
		opts.overflowPolicy = policy
	}
}

// WithMetrics enables Prometheus metrics export for buffer statistics.
// If registry is nil, this option is ignored.
// Registry should not be nil in normal usage - this handles edge cases gracefully.
func WithMetrics[T any](registry *metric.MetricsRegistry, prefix string) Option[T] {
	return func(opts *bufferOptions[T]) {
		if registry != nil && prefix != "" {
			opts.metricsReg = registry
			opts.metricsPrefix = prefix
		}
	}
}

// WithDropCallback sets a callback function that is called when items are dropped.
// The callback receives the item that was dropped.
func WithDropCallback[T any](callback DropCallback[T]) Option[T] {
	return func(opts *bufferOptions[T]) {
		opts.dropCallback = callback
	}
}

// applyOptions applies functional options to create final buffer configuration.
// This is an internal helper used by buffer constructors.
func applyOptions[T any](options ...Option[T]) *bufferOptions[T] {
	opts := &bufferOptions[T]{
		// Default values
		overflowPolicy: DropOldest, // Sensible default
	}

	for _, opt := range options {
		if opt != nil {
			opt(opts)
		}
	}

	return opts
}
