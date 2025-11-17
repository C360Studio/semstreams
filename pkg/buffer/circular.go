package buffer

import (
	"context"
	"sync"
	"time"

	"github.com/c360/semstreams/errors"
)

// circularBuffer is a thread-safe circular buffer with configurable overflow policies.
type circularBuffer[T any] struct {
	mu       sync.RWMutex
	items    []T
	capacity int
	size     int
	head     int            // Points to the next write position
	tail     int            // Points to the next read position
	stats    *Statistics    // ALWAYS initialized for observability
	metrics  *bufferMetrics // Optional Prometheus metrics
	opts     *bufferOptions[T]

	// For Block policy
	notEmpty *sync.Cond
	notFull  *sync.Cond
	closed   bool
}

// newCircularBuffer creates a new circular buffer instance.
// Returns an error if metrics registration fails when requested.
func newCircularBuffer[T any](capacity int, opts *bufferOptions[T]) (*circularBuffer[T], error) {
	if capacity <= 0 {
		capacity = 1 // Minimum capacity
	}

	// Stats are ALWAYS initialized - observability is not optional
	stats := NewStatistics()

	var metrics *bufferMetrics
	// Optionally expose stats as Prometheus metrics
	if opts.metricsReg != nil && opts.metricsPrefix != "" {
		var err error
		metrics, err = newBufferMetrics(opts.metricsReg, opts.metricsPrefix)
		if err != nil {
			// Return classified error instead of silently ignoring
			return nil, errors.WrapTransient(err, "buffer", "newCircularBuffer", "metrics registration")
		}
	}

	cb := &circularBuffer[T]{
		items:    make([]T, capacity),
		capacity: capacity,
		stats:    stats,   // ALWAYS present
		metrics:  metrics, // Optional
		opts:     opts,
	}

	// Set up condition variables for Block policy
	cb.notEmpty = sync.NewCond(&cb.mu)
	cb.notFull = sync.NewCond(&cb.mu)

	return cb, nil
}

// Write adds an item to the buffer according to the overflow policy.
func (cb *circularBuffer[T]) Write(item T) error {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if cb.closed {
		return errors.WrapInvalid(errors.ErrAlreadyStopped, "Buffer", "Write", "buffer closed")
	}

	// Handle different overflow policies when buffer is full
	if cb.size == cb.capacity {
		switch cb.opts.overflowPolicy {
		case DropOldest:
			// Remove oldest item to make room
			droppedItem := cb.items[cb.tail]
			cb.tail = (cb.tail + 1) % cb.capacity
			cb.size--

			// ALWAYS track in stats
			cb.stats.Overflow()
			cb.stats.Drop()

			// ALSO track in metrics if enabled
			if cb.metrics != nil {
				cb.metrics.recordOverflow()
				cb.metrics.recordDrop()
			}

			if cb.opts.dropCallback != nil {
				// Call dropCallback outside the lock to avoid deadlock
				defer cb.opts.dropCallback(droppedItem)
			}

		case DropNewest:
			// Drop the new item
			// ALWAYS track in stats
			cb.stats.Overflow()
			cb.stats.Drop()

			// ALSO track in metrics if enabled
			if cb.metrics != nil {
				cb.metrics.recordOverflow()
				cb.metrics.recordDrop()
			}

			if cb.opts.dropCallback != nil {
				defer cb.opts.dropCallback(item)
			}
			return nil

		case Block:
			// Wait for space to become available
			for cb.size == cb.capacity && !cb.closed {
				cb.notFull.Wait()
			}

			if cb.closed {
				return errors.WrapInvalid(errors.ErrAlreadyStopped, "Buffer", "Write",
					"buffer closed during blocking wait")
			}
		}
	}

	// Add the item
	cb.items[cb.head] = item
	cb.head = (cb.head + 1) % cb.capacity
	cb.size++

	// ALWAYS track in stats
	cb.stats.Write()
	cb.stats.UpdateSize(int64(cb.size))

	// ALSO track in metrics if enabled
	if cb.metrics != nil {
		cb.metrics.recordWrite(cb.size, cb.capacity)
	}

	// Signal waiting readers
	cb.notEmpty.Signal()

	return nil
}

// Read retrieves and removes one item from the buffer.
func (cb *circularBuffer[T]) Read() (T, bool) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	var zero T

	if cb.size == 0 {
		// Don't record a miss for empty reads, just return
		return zero, false
	}

	// Get the item
	item := cb.items[cb.tail]
	cb.items[cb.tail] = zero // Clear for GC
	cb.tail = (cb.tail + 1) % cb.capacity
	cb.size--

	// ALWAYS track in stats
	cb.stats.Read()
	cb.stats.UpdateSize(int64(cb.size))

	// ALSO track in metrics if enabled
	if cb.metrics != nil {
		cb.metrics.recordRead(cb.size, cb.capacity)
	}

	// Signal waiting writers
	cb.notFull.Signal()

	return item, true
}

// ReadBatch retrieves and removes up to max items from the buffer.
func (cb *circularBuffer[T]) ReadBatch(max int) []T {
	if max <= 0 {
		return nil
	}

	cb.mu.Lock()
	defer cb.mu.Unlock()

	if cb.size == 0 {
		return nil
	}

	// Determine how many items to read
	readCount := max
	if readCount > cb.size {
		readCount = cb.size
	}

	result := make([]T, readCount)
	var zero T

	for i := 0; i < readCount; i++ {
		result[i] = cb.items[cb.tail]
		cb.items[cb.tail] = zero // Clear for GC
		cb.tail = (cb.tail + 1) % cb.capacity
		cb.size--

		// ALWAYS track in stats
		cb.stats.Read()
	}

	// ALWAYS track in stats
	cb.stats.UpdateSize(int64(cb.size))

	// ALSO track in metrics if enabled - use final state after all reads
	if cb.metrics != nil {
		cb.metrics.updateSize(cb.size, cb.capacity)
	}

	// Signal waiting writers if we freed up space
	if readCount > 0 {
		for i := 0; i < readCount; i++ {
			cb.notFull.Signal()
		}
	}

	return result
}

// Peek retrieves one item without removing it from the buffer.
func (cb *circularBuffer[T]) Peek() (T, bool) {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	var zero T

	if cb.size == 0 {
		return zero, false
	}

	item := cb.items[cb.tail]

	// ALWAYS track in stats
	cb.stats.Peek()

	// ALSO track in metrics if enabled
	if cb.metrics != nil {
		cb.metrics.recordPeek()
	}

	return item, true
}

// Size returns the current number of items in the buffer.
func (cb *circularBuffer[T]) Size() int {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.size
}

// Capacity returns the maximum number of items the buffer can hold.
func (cb *circularBuffer[T]) Capacity() int {
	return cb.capacity // This is immutable, so no lock needed
}

// IsFull returns true if the buffer is at maximum capacity.
func (cb *circularBuffer[T]) IsFull() bool {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.size == cb.capacity
}

// IsEmpty returns true if the buffer contains no items.
func (cb *circularBuffer[T]) IsEmpty() bool {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.size == 0
}

// Clear removes all items from the buffer.
func (cb *circularBuffer[T]) Clear() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	var zero T

	// Call dropCallback for all items if callback is set
	if cb.opts.dropCallback != nil {
		itemsToDrop := make([]T, cb.size)
		for i := 0; i < cb.size; i++ {
			idx := (cb.tail + i) % cb.capacity
			itemsToDrop[i] = cb.items[idx]
		}
		// Call callbacks outside the lock to avoid deadlock
		defer func() {
			for _, item := range itemsToDrop {
				cb.opts.dropCallback(item)
			}
		}()
	}

	// Clear all items
	for i := 0; i < cb.capacity; i++ {
		cb.items[i] = zero
	}

	cb.head = 0
	cb.tail = 0
	cb.size = 0

	// ALWAYS track in stats
	cb.stats.UpdateSize(0)

	// ALSO track in metrics if enabled
	if cb.metrics != nil {
		cb.metrics.updateSize(0, cb.capacity)
	}

	// Signal all waiting writers
	cb.notFull.Broadcast()
}

// Stats returns buffer statistics (always available for observability).
func (cb *circularBuffer[T]) Stats() *Statistics {
	return cb.stats
}

// Close shuts down the buffer and releases resources.
func (cb *circularBuffer[T]) Close() error {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if cb.closed {
		return nil
	}

	cb.closed = true

	// Wake up all waiting goroutines
	cb.notEmpty.Broadcast()
	cb.notFull.Broadcast()

	return nil
}

// WriteWithTimeout attempts to write an item with a timeout when using Block policy.
func (cb *circularBuffer[T]) WriteWithTimeout(item T, timeout time.Duration) error {
	if cb.opts.overflowPolicy != Block {
		return cb.Write(item)
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return cb.WriteWithContext(ctx, item)
}

// WriteWithContext attempts to write an item with context cancellation when using Block policy.
func (cb *circularBuffer[T]) WriteWithContext(ctx context.Context, item T) error {
	if cb.opts.overflowPolicy != Block {
		return cb.Write(item)
	}

	cb.mu.Lock()
	defer cb.mu.Unlock()

	if cb.closed {
		return errors.WrapInvalid(errors.ErrAlreadyStopped, "Buffer", "WriteWithContext", "buffer closed")
	}

	// Check if context is already cancelled
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Create a done channel to signal when we're done waiting
	done := make(chan struct{})
	defer close(done)

	// Track goroutine exit with WaitGroup to ensure cleanup
	var ctxWg sync.WaitGroup
	ctxWg.Add(1)

	// Set up context cancellation handler without holding the lock
	go func() {
		defer ctxWg.Done()
		select {
		case <-ctx.Done():
			// Wake up waiting goroutines when context is cancelled
			// This is safe because Broadcast can be called without holding the mutex
			cb.notFull.Broadcast()
		case <-done:
			// Function completed successfully, exit goroutine
		}
	}()

	// Wait for space or context cancellation
	for cb.size == cb.capacity && !cb.closed {
		// Check context before waiting
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Wait for space to become available
		cb.notFull.Wait()

		// Check context after being woken up
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}

	if cb.closed {
		return errors.WrapInvalid(errors.ErrAlreadyStopped, "Buffer", "WriteWithContext", "buffer closed during wait")
	}

	// Add the item
	cb.items[cb.head] = item
	cb.head = (cb.head + 1) % cb.capacity
	cb.size++

	// ALWAYS track in stats
	cb.stats.Write()
	cb.stats.UpdateSize(int64(cb.size))

	// ALSO track in metrics if enabled
	if cb.metrics != nil {
		cb.metrics.recordWrite(cb.size, cb.capacity)
	}

	// Signal waiting readers
	cb.notEmpty.Signal()

	return nil
}
