package agenticmodel_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	agenticmodel "github.com/c360studio/semstreams/processor/agentic-model"
)

func TestNewEndpointThrottle_ZeroLimits(t *testing.T) {
	// Zero values should create a valid throttle that imposes no limits.
	th := agenticmodel.NewEndpointThrottle(0, 0)
	if th == nil {
		t.Fatal("NewEndpointThrottle() returned nil")
	}

	ctx := context.Background()
	if err := th.Acquire(ctx); err != nil {
		t.Fatalf("Acquire() with no limits returned error: %v", err)
	}
	th.Release() // should be a no-op
}

func TestEndpointThrottle_ConcurrencyLimit(t *testing.T) {
	const maxConcurrent = 3
	th := agenticmodel.NewEndpointThrottle(0, maxConcurrent)

	// Acquire all slots.
	ctx := context.Background()
	for i := range maxConcurrent {
		if err := th.Acquire(ctx); err != nil {
			t.Fatalf("Acquire() slot %d failed: %v", i, err)
		}
	}

	// The next Acquire should block — verify it respects context cancellation.
	cancelCtx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := th.Acquire(cancelCtx)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("Acquire() should have blocked and returned context error")
	}
	if elapsed < 40*time.Millisecond {
		t.Errorf("Acquire() returned too quickly (%v); expected to block ~50ms", elapsed)
	}

	// Release all slots, then verify Acquire succeeds again.
	for range maxConcurrent {
		th.Release()
	}
	if err := th.Acquire(ctx); err != nil {
		t.Fatalf("Acquire() after Release() failed: %v", err)
	}
	th.Release()
}

func TestEndpointThrottle_ConcurrencyLimitEnforced(t *testing.T) {
	const maxConcurrent = 2
	th := agenticmodel.NewEndpointThrottle(0, maxConcurrent)
	ctx := context.Background()

	var inFlight atomic.Int32
	var maxObserved atomic.Int32
	var wg sync.WaitGroup

	const goroutines = 10
	for range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := th.Acquire(ctx); err != nil {
				t.Errorf("Acquire() failed: %v", err)
				return
			}
			defer th.Release()

			current := inFlight.Add(1)
			// Track the high-water mark.
			for {
				prev := maxObserved.Load()
				if current <= prev || maxObserved.CompareAndSwap(prev, current) {
					break
				}
			}
			time.Sleep(5 * time.Millisecond)
			inFlight.Add(-1)
		}()
	}

	wg.Wait()

	if observed := int(maxObserved.Load()); observed > maxConcurrent {
		t.Errorf("max concurrent = %d, want <= %d", observed, maxConcurrent)
	}
}

func TestEndpointThrottle_RateLimitContextCancellation(t *testing.T) {
	// Set an extremely low rate (1 req/min) so the second Acquire blocks.
	th := agenticmodel.NewEndpointThrottle(1, 0)
	ctx := context.Background()

	// First request should go through immediately (full bucket).
	if err := th.Acquire(ctx); err != nil {
		t.Fatalf("first Acquire() failed: %v", err)
	}
	th.Release()

	// Second request should block because the bucket is empty.
	cancelCtx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := th.Acquire(cancelCtx)
	if err == nil {
		t.Fatal("Acquire() should have been blocked and returned context error")
	}
}

func TestEndpointThrottle_Release_IsNoOpWithNoConcurrencyLimit(_ *testing.T) {
	// Release on a throttle with no concurrency limit must not panic or block.
	th := agenticmodel.NewEndpointThrottle(0, 0)
	// Should not block or panic.
	th.Release()
	th.Release()
}
