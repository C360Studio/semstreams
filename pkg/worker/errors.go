package worker

import "errors"

// Sentinel errors for worker pool operations
var (
	// ErrPoolNotStarted indicates the pool hasn't been started yet
	ErrPoolNotStarted = errors.New("worker pool not started")

	// ErrPoolStopped indicates the pool has been stopped
	ErrPoolStopped = errors.New("worker pool stopped")

	// ErrPoolAlreadyStarted indicates Start() was called on an already-started pool
	ErrPoolAlreadyStarted = errors.New("worker pool already started")

	// ErrQueueFull indicates the work queue is at capacity
	ErrQueueFull = errors.New("worker pool queue full")

	// ErrNilProcessor indicates a nil processor function was provided
	ErrNilProcessor = errors.New("processor function cannot be nil")

	// ErrStopTimeout indicates the pool didn't stop within the timeout
	ErrStopTimeout = errors.New("timeout waiting for workers to stop")
)
