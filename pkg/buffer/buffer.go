// Package buffer provides generic, thread-safe buffer implementations with various overflow policies.
//
// This package offers flexible buffer types:
//   - CircularBuffer: Fixed-size buffer with configurable overflow policies
//   - Support for DropOldest, DropNewest, and Block overflow policies
//   - Statistics always enabled for observability
//   - Optional Prometheus metrics integration via functional options
//
// All buffer implementations are thread-safe and always collect statistics for observability.
// Prometheus metrics can be optionally enabled via WithMetrics() functional option.
package buffer

import (
	"context"
)

// Buffer represents a generic buffer interface that all buffer implementations must satisfy.
// The buffer is parameterized by item type T for type safety.
type Buffer[T any] interface {
	// Write adds an item to the buffer. Returns an error if the operation fails.
	// Behavior depends on the overflow policy when buffer is full.
	Write(item T) error

	// Read retrieves and removes one item from the buffer.
	// Returns the item and true if successful, zero value and false if buffer is empty.
	Read() (T, bool)

	// ReadBatch retrieves and removes up to max items from the buffer.
	// Returns a slice containing the retrieved items (may be shorter than max).
	ReadBatch(max int) []T

	// Peek retrieves one item without removing it from the buffer.
	// Returns the item and true if successful, zero value and false if buffer is empty.
	Peek() (T, bool)

	// Size returns the current number of items in the buffer.
	Size() int

	// Capacity returns the maximum number of items the buffer can hold.
	Capacity() int

	// IsFull returns true if the buffer is at maximum capacity.
	IsFull() bool

	// IsEmpty returns true if the buffer contains no items.
	IsEmpty() bool

	// Clear removes all items from the buffer.
	Clear()

	// Stats returns buffer statistics (always available for observability).
	Stats() *Statistics

	// Close shuts down the buffer and releases any resources.
	Close() error
}

// OverflowPolicy defines how the buffer behaves when it reaches capacity.
type OverflowPolicy int

const (
	// DropOldest removes the oldest item to make room for new items.
	DropOldest OverflowPolicy = iota

	// DropNewest drops new items when the buffer is full.
	DropNewest

	// Block causes Write operations to block until space is available.
	Block
)

// String returns a human-readable representation of the overflow policy.
func (p OverflowPolicy) String() string {
	switch p {
	case DropOldest:
		return "DropOldest"
	case DropNewest:
		return "DropNewest"
	case Block:
		return "Block"
	default:
		return "Unknown"
	}
}

// DropCallback is called when an item is dropped due to overflow policy.
// It receives the item that was dropped.
type DropCallback[T any] func(item T)

// contextKey is used for context values in this package.
type contextKey string

const (
	// ContextKeyStats can be used to pass statistics through context.
	ContextKeyStats contextKey = "buffer-stats"
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

// NewCircularBuffer creates a new circular buffer with the specified capacity and options.
// Stats are ALWAYS collected for observability. Metrics are optional via WithMetrics().
// Returns an error if metrics registration fails when metrics are requested.
// Capacity is required - all other configuration is via functional options.
func NewCircularBuffer[T any](capacity int, options ...Option[T]) (Buffer[T], error) {
	opts := applyOptions(options...)
	return newCircularBuffer(capacity, opts)
}
