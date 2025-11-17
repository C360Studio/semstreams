package worker

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestPool_SentinelErrors verifies that the correct sentinel errors are returned
func TestPool_SentinelErrors(t *testing.T) {
	tests := []struct {
		name string
		test func(t *testing.T)
	}{
		{
			name: "ErrPoolNotStarted when submitting before start",
			test: func(t *testing.T) {
				processor := func(_ context.Context, _ testWork) error {
					return nil
				}
				pool := NewPool(2, 10, processor)

				// Try to submit before starting
				err := pool.Submit(testWork{id: 1})
				if !errors.Is(err, ErrPoolNotStarted) {
					t.Errorf("Expected ErrPoolNotStarted, got %v", err)
				}
			},
		},
		{
			name: "ErrPoolAlreadyStarted when starting twice",
			test: func(t *testing.T) {
				processor := func(_ context.Context, _ testWork) error {
					return nil
				}
				pool := NewPool(2, 10, processor)

				ctx := context.Background()
				err := pool.Start(ctx)
				if err != nil {
					t.Fatalf("Failed to start pool: %v", err)
				}
				defer pool.Stop(5 * time.Second)

				// Try to start again
				err = pool.Start(ctx)
				if !errors.Is(err, ErrPoolAlreadyStarted) {
					t.Errorf("Expected ErrPoolAlreadyStarted, got %v", err)
				}
			},
		},
		{
			name: "ErrPoolStopped when submitting after stop",
			test: func(t *testing.T) {
				processor := func(_ context.Context, _ testWork) error {
					return nil
				}
				pool := NewPool(2, 10, processor)

				ctx := context.Background()
				err := pool.Start(ctx)
				if err != nil {
					t.Fatalf("Failed to start pool: %v", err)
				}

				// Stop the pool
				err = pool.Stop(5 * time.Second)
				if err != nil {
					t.Fatalf("Failed to stop pool: %v", err)
				}

				// Try to submit after stopping
				err = pool.Submit(testWork{id: 1})
				if !errors.Is(err, ErrPoolStopped) {
					t.Errorf("Expected ErrPoolStopped, got %v", err)
				}
			},
		},
		{
			name: "ErrQueueFull when queue is at capacity",
			test: func(t *testing.T) {
				// Processor that blocks to fill the queue
				processor := func(_ context.Context, _ testWork) error {
					time.Sleep(1 * time.Second)
					return nil
				}

				// Very small pool and queue
				pool := NewPool(1, 2, processor)

				ctx := context.Background()
				err := pool.Start(ctx)
				if err != nil {
					t.Fatalf("Failed to start pool: %v", err)
				}
				defer pool.Stop(5 * time.Second)

				// Fill the queue
				var queueFullErr error
				for i := 0; i < 10; i++ {
					err := pool.Submit(testWork{id: i})
					if err != nil {
						queueFullErr = err
						break
					}
				}

				// Should get ErrQueueFull at some point
				if !errors.Is(queueFullErr, ErrQueueFull) {
					t.Errorf("Expected ErrQueueFull, got %v", queueFullErr)
				}
			},
		},
		{
			name: "ErrStopTimeout when workers don't finish in time",
			test: func(t *testing.T) {
				// Processor that takes a very long time
				processor := func(ctx context.Context, _ testWork) error {
					select {
					case <-time.After(10 * time.Second):
						return nil
					case <-ctx.Done():
						return ctx.Err()
					}
				}

				pool := NewPool(1, 10, processor)

				ctx := context.Background()
				err := pool.Start(ctx)
				if err != nil {
					t.Fatalf("Failed to start pool: %v", err)
				}

				// Submit work that will take too long
				_ = pool.Submit(testWork{id: 1})

				// Give worker time to pick up the work
				time.Sleep(10 * time.Millisecond)

				// Try to stop with a short timeout
				err = pool.Stop(50 * time.Millisecond)
				if !errors.Is(err, ErrStopTimeout) {
					t.Errorf("Expected ErrStopTimeout, got %v", err)
				}
			},
		},
		{
			name: "ErrNilProcessor when creating pool with nil processor",
			test: func(t *testing.T) {
				defer func() {
					r := recover()
					if r == nil {
						t.Error("Expected panic for nil processor")
						return
					}
					// Check that the panic value is our sentinel error
					if !errors.Is(r.(error), ErrNilProcessor) {
						t.Errorf("Expected panic with ErrNilProcessor, got %v", r)
					}
				}()
				NewPool[testWork](5, 100, nil)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.test(t)
		})
	}
}

// TestPool_ErrorsAreNotWrapped verifies errors can be checked with errors.Is
func TestPool_ErrorsAreNotWrapped(t *testing.T) {
	processor := func(ctx context.Context, _ testWork) error {
		// Check for cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			return nil
		}
	}

	pool := NewPool(2, 10, processor)

	// Submit before start
	err := pool.Submit(testWork{id: 1})

	// Should be able to check with errors.Is
	if !errors.Is(err, ErrPoolNotStarted) {
		t.Errorf("errors.Is failed for ErrPoolNotStarted: %v", err)
	}

	// Should be the exact sentinel error (not wrapped)
	if err != ErrPoolNotStarted {
		t.Errorf("Expected exact sentinel error ErrPoolNotStarted, got %v", err)
	}
}
