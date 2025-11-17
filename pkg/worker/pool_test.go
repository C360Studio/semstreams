package worker

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Test data structure for worker pool tests
type testWork struct {
	id    int
	delay time.Duration
	fail  bool
}

func TestNewPool(t *testing.T) {
	processor := func(ctx context.Context, _ testWork) error {
		// Check for cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			return nil
		}
	}

	// Test with valid parameters
	pool := NewPool(5, 100, processor)
	if pool.workers != 5 {
		t.Errorf("Expected 5 workers, got %d", pool.workers)
	}
	if pool.queueSize != 100 {
		t.Errorf("Expected queue size 100, got %d", pool.queueSize)
	}

	// Test with zero workers (should default)
	pool = NewPool(0, 100, processor)
	if pool.workers != 10 {
		t.Errorf("Expected default 10 workers, got %d", pool.workers)
	}

	// Test with zero queue size (should default)
	pool = NewPool(5, 0, processor)
	if pool.queueSize != 1000 {
		t.Errorf("Expected default queue size 1000, got %d", pool.queueSize)
	}
}

func TestNewPool_NilProcessor(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("Expected panic for nil processor")
		}
	}()
	NewPool[testWork](5, 100, nil)
}

func TestPool_StartStop(t *testing.T) {
	var processedCount int64
	processor := func(_ context.Context, _ testWork) error {
		atomic.AddInt64(&processedCount, 1)
		return nil
	}

	pool := NewPool(2, 10, processor)

	// Test starting the pool
	ctx := context.Background()
	err := pool.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start pool: %v", err)
	}

	// Test that we can't start twice
	err = pool.Start(ctx)
	if err == nil {
		t.Error("Expected error when starting pool twice")
	}

	// Submit some work
	for i := 0; i < 5; i++ {
		err := pool.Submit(testWork{id: i})
		if err != nil {
			t.Errorf("Failed to submit work %d: %v", i, err)
		}
	}

	// Give workers time to process
	time.Sleep(100 * time.Millisecond)

	// Stop the pool
	err = pool.Stop(5 * time.Second)
	if err != nil {
		t.Fatalf("Failed to stop pool: %v", err)
	}

	// Verify work was processed
	processed := atomic.LoadInt64(&processedCount)
	if processed != 5 {
		t.Errorf("Expected 5 processed items, got %d", processed)
	}

	// Test that we can't submit after stopping
	err = pool.Submit(testWork{id: 999})
	if err == nil {
		t.Error("Expected error when submitting to stopped pool")
	}
}

func TestPool_QueueFull(t *testing.T) {
	processor := func(_ context.Context, work testWork) error {
		// Slow processor to fill queue
		time.Sleep(work.delay)
		return nil
	}

	pool := NewPool(1, 2, processor) // Small queue

	ctx := context.Background()
	err := pool.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start pool: %v", err)
	}
	defer pool.Stop(5 * time.Second)

	// Submit work that will fill the queue
	submitted := 0
	dropped := 0

	// Submit slow work first
	for i := 0; i < 5; i++ {
		err := pool.Submit(testWork{
			id:    i,
			delay: 200 * time.Millisecond, // Slow processing
		})
		if err != nil {
			dropped++
		} else {
			submitted++
		}
	}

	if dropped == 0 {
		t.Error("Expected some work to be dropped due to full queue")
	}

	if submitted == 0 {
		t.Error("Expected some work to be submitted successfully")
	}

	stats := pool.Stats()
	if stats.Dropped == 0 {
		t.Error("Stats should show dropped work items")
	}
}

func TestPool_ProcessingErrors(t *testing.T) {
	var successCount, errorCount int64

	processor := func(_ context.Context, work testWork) error {
		if work.fail {
			atomic.AddInt64(&errorCount, 1)
			return errors.New("simulated error")
		}
		atomic.AddInt64(&successCount, 1)
		return nil
	}

	pool := NewPool(2, 10, processor)

	ctx := context.Background()
	err := pool.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start pool: %v", err)
	}
	defer pool.Stop(5 * time.Second)

	// Submit mix of successful and failing work
	for i := 0; i < 10; i++ {
		work := testWork{
			id:   i,
			fail: i%2 == 0, // Half will fail
		}
		err := pool.Submit(work)
		if err != nil {
			t.Errorf("Failed to submit work %d: %v", i, err)
		}
	}

	// Give workers time to process
	time.Sleep(100 * time.Millisecond)

	// Verify processing counts
	success := atomic.LoadInt64(&successCount)
	errCount := atomic.LoadInt64(&errorCount)

	if success != 5 {
		t.Errorf("Expected 5 successful processes, got %d", success)
	}
	if errCount != 5 {
		t.Errorf("Expected 5 failed processes, got %d", errCount)
	}

	stats := pool.Stats()
	if stats.Processed != 10 {
		t.Errorf("Expected 10 processed items in stats, got %d", stats.Processed)
	}
	if stats.Failed != 5 {
		t.Errorf("Expected 5 failed items in stats, got %d", stats.Failed)
	}
}

func TestPool_ContextCancellation(t *testing.T) {
	var processedCount int64

	processor := func(ctx context.Context, work testWork) error {
		// Check if context is cancelled
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			time.Sleep(work.delay)
			atomic.AddInt64(&processedCount, 1)
			return nil
		}
	}

	pool := NewPool(2, 10, processor)

	ctx, cancel := context.WithCancel(context.Background())
	err := pool.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start pool: %v", err)
	}

	// Submit work
	for i := 0; i < 5; i++ {
		err := pool.Submit(testWork{
			id:    i,
			delay: 50 * time.Millisecond,
		})
		if err != nil {
			t.Errorf("Failed to submit work %d: %v", i, err)
		}
	}

	// Cancel context quickly
	time.Sleep(10 * time.Millisecond)
	cancel()

	// Stop the pool
	err = pool.Stop(5 * time.Second)
	if err != nil {
		t.Fatalf("Failed to stop pool: %v", err)
	}

	// Some work might be processed before cancellation
	processed := atomic.LoadInt64(&processedCount)
	t.Logf("Processed %d items before cancellation", processed)
}

func TestPool_ConcurrentSubmissions(t *testing.T) {
	var processedCount int64

	processor := func(_ context.Context, _ testWork) error {
		atomic.AddInt64(&processedCount, 1)
		return nil
	}

	pool := NewPool(5, 100, processor)

	ctx := context.Background()
	err := pool.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start pool: %v", err)
	}
	defer pool.Stop(5 * time.Second)

	// Concurrent submissions
	var wg sync.WaitGroup
	submitters := 10
	workPerSubmitter := 10

	for i := 0; i < submitters; i++ {
		wg.Add(1)
		go func(submitterID int) {
			defer wg.Done()
			for j := 0; j < workPerSubmitter; j++ {
				work := testWork{
					id: submitterID*workPerSubmitter + j,
				}
				err := pool.Submit(work)
				if err != nil {
					t.Errorf("Submitter %d failed to submit work %d: %v", submitterID, j, err)
				}
			}
		}(i)
	}

	wg.Wait()

	// Give workers time to process
	time.Sleep(200 * time.Millisecond)

	// Verify all work was processed
	processed := atomic.LoadInt64(&processedCount)
	expected := int64(submitters * workPerSubmitter)
	if processed != expected {
		t.Errorf("Expected %d processed items, got %d", expected, processed)
	}
}

func TestPool_Stats(t *testing.T) {
	processor := func(ctx context.Context, _ testWork) error {
		// Check for cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Millisecond):
			return nil
		}
	}

	pool := NewPool(3, 50, processor)

	// Check initial stats
	stats := pool.Stats()
	if stats.Workers != 3 {
		t.Errorf("Expected 3 workers in stats, got %d", stats.Workers)
	}
	if stats.QueueSize != 50 {
		t.Errorf("Expected queue size 50 in stats, got %d", stats.QueueSize)
	}
	if stats.Submitted != 0 {
		t.Errorf("Expected 0 submitted initially, got %d", stats.Submitted)
	}

	ctx := context.Background()
	err := pool.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start pool: %v", err)
	}
	defer pool.Stop(5 * time.Second)

	// Submit some work
	for i := 0; i < 10; i++ {
		_ = pool.Submit(testWork{id: i})
	}

	// Check updated stats
	time.Sleep(50 * time.Millisecond) // Give some time for processing
	stats = pool.Stats()

	if stats.Submitted != 10 {
		t.Errorf("Expected 10 submitted in stats, got %d", stats.Submitted)
	}

	// Processed count should be > 0 and <= submitted
	if stats.Processed <= 0 || stats.Processed > stats.Submitted {
		t.Errorf("Invalid processed count in stats: %d (submitted: %d)", stats.Processed, stats.Submitted)
	}
}
