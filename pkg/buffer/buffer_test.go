package buffer

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"testing"
	"time"

	cerrors "github.com/c360/semstreams/errors"
	"github.com/stretchr/testify/require"
)

// TestBufferInterface verifies all buffer implementations satisfy the interface
func TestBufferInterface(t *testing.T) {
	// Test with different types to ensure generics work
	testCases := []struct {
		name string
		buf  Buffer[int]
	}{
		{"CircularBuffer", func() Buffer[int] {
			buf, err := NewCircularBuffer[int](5)
			if err != nil {
				panic(err)
			}
			return buf
		}()},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			buf := tc.buf
			defer buf.Close()

			// Test initial state
			if buf.Size() != 0 {
				t.Errorf("Expected initial size 0, got %d", buf.Size())
			}
			if buf.Capacity() != 5 {
				t.Errorf("Expected capacity 5, got %d", buf.Capacity())
			}
			if !buf.IsEmpty() {
				t.Error("Expected buffer to be empty initially")
			}
			if buf.IsFull() {
				t.Error("Expected buffer not to be full initially")
			}
		})
	}
}

func TestCircularBufferBasicOperations(t *testing.T) {
	buf, err := NewCircularBuffer[string](3)
	require.NoError(t, err, "Failed to create buffer")
	defer buf.Close()

	// Test Write operations
	if err := buf.Write("first"); err != nil {
		t.Fatalf("Failed to write: %v", err)
	}
	if buf.Size() != 1 {
		t.Errorf("Expected size 1, got %d", buf.Size())
	}

	if err := buf.Write("second"); err != nil {
		t.Fatalf("Failed to write: %v", err)
	}
	if err := buf.Write("third"); err != nil {
		t.Fatalf("Failed to write: %v", err)
	}

	if !buf.IsFull() {
		t.Error("Expected buffer to be full")
	}
	if buf.IsEmpty() {
		t.Error("Expected buffer not to be empty")
	}

	// Test Peek operation
	value, ok := buf.Peek()
	if !ok {
		t.Error("Expected peek to succeed")
	}
	if value != "first" {
		t.Errorf("Expected peek to return 'first', got %s", value)
	}
	if buf.Size() != 3 {
		t.Error("Peek should not change size")
	}

	// Test Read operations
	value, ok = buf.Read()
	if !ok {
		t.Error("Expected read to succeed")
	}
	if value != "first" {
		t.Errorf("Expected read to return 'first', got %s", value)
	}
	if buf.Size() != 2 {
		t.Errorf("Expected size 2 after read, got %d", buf.Size())
	}

	// Test ReadBatch
	batch := buf.ReadBatch(2)
	if len(batch) != 2 {
		t.Errorf("Expected batch size 2, got %d", len(batch))
	}
	if batch[0] != "second" || batch[1] != "third" {
		t.Errorf("Expected ['second', 'third'], got %v", batch)
	}
	if buf.Size() != 0 {
		t.Errorf("Expected size 0 after batch read, got %d", buf.Size())
	}
}

func TestCircularBufferOverflowPolicies(t *testing.T) {
	testCases := []struct {
		name     string
		policy   OverflowPolicy
		expected []int
	}{
		{
			name:     "DropOldest",
			policy:   DropOldest,
			expected: []int{3, 4, 5}, // 1,2 dropped
		},
		{
			name:     "DropNewest",
			policy:   DropNewest,
			expected: []int{1, 2, 3}, // 4,5 not added
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			buf, err := NewCircularBuffer[int](3, WithOverflowPolicy[int](tc.policy))
			if err != nil {
				t.Fatalf("Failed to create buffer: %v", err)
			}
			defer buf.Close()

			// Fill buffer and overflow
			for i := 1; i <= 5; i++ {
				buf.Write(i)
			}

			// Read all and verify
			var result []int
			for !buf.IsEmpty() {
				value, ok := buf.Read()
				if ok {
					result = append(result, value)
				}
			}

			if len(result) != len(tc.expected) {
				t.Errorf("Expected %d items, got %d", len(tc.expected), len(result))
			}

			for i, expected := range tc.expected {
				if i < len(result) && result[i] != expected {
					t.Errorf("Position %d: expected %d, got %d", i, expected, result[i])
				}
			}
		})
	}
}

func TestCircularBufferWithStatistics(t *testing.T) {
	buf, err := NewCircularBuffer[int](5) // Stats are always enabled
	if err != nil {
		t.Fatalf("Failed to create buffer: %v", err)
	}
	defer buf.Close()

	stats := buf.Stats()
	if stats == nil {
		t.Fatal("Expected stats to be enabled")
	}

	// Test write stats
	_ = buf.Write(1)
	_ = buf.Write(2)

	if stats.Writes() != 2 {
		t.Errorf("Expected 2 writes, got %d", stats.Writes())
	}

	// Test read stats
	buf.Read()

	if stats.Reads() != 1 {
		t.Errorf("Expected 1 read, got %d", stats.Reads())
	}

	// Test overflow stats
	overflowBuf, err := NewCircularBuffer[int](2, WithOverflowPolicy[int](DropOldest)) // Stats always enabled
	require.NoError(t, err, "Failed to create overflow buffer")
	defer overflowBuf.Close()

	_ = overflowBuf.Write(1)
	_ = overflowBuf.Write(2)
	_ = overflowBuf.Write(3) // Should cause overflow

	overflowStats := overflowBuf.Stats()
	if overflowStats.Overflows() != 1 {
		t.Errorf("Expected 1 overflow, got %d", overflowStats.Overflows())
	}
}

func TestCircularBufferThreadSafety(t *testing.T) {
	buf, err := NewCircularBuffer[int](1000)
	if err != nil {
		t.Fatalf("Failed to create buffer: %v", err)
	}
	defer buf.Close()

	var wg sync.WaitGroup
	numWorkers := 10
	itemsPerWorker := 100

	// Writers
	wg.Add(numWorkers)
	for w := 0; w < numWorkers; w++ {
		go func(worker int) {
			defer wg.Done()
			for i := 0; i < itemsPerWorker; i++ {
				_ = buf.Write(worker*itemsPerWorker + i)
			}
		}(w)
	}

	// Readers
	wg.Add(numWorkers)
	readCount := 0
	var readMutex sync.Mutex
	for w := 0; w < numWorkers; w++ {
		go func() {
			defer wg.Done()
			for i := 0; i < itemsPerWorker; i++ {
				if _, ok := buf.Read(); ok {
					readMutex.Lock()
					readCount++
					readMutex.Unlock()
				}
			}
		}()
	}

	wg.Wait()

	// Verify no data races occurred
	finalSize := buf.Size()
	totalWritten := numWorkers * itemsPerWorker

	readMutex.Lock()
	totalRead := readCount
	readMutex.Unlock()

	if totalRead+finalSize != totalWritten {
		t.Errorf("Data integrity issue: written=%d, read=%d, remaining=%d",
			totalWritten, totalRead, finalSize)
	}
}

func TestCircularBufferClear(t *testing.T) {
	buf, err := NewCircularBuffer[string](5)
	if err != nil {
		t.Fatalf("Failed to create buffer: %v", err)
	}
	defer buf.Close()

	_ = buf.Write("a")
	_ = buf.Write("b")
	_ = buf.Write("c")

	if buf.Size() != 3 {
		t.Errorf("Expected size 3, got %d", buf.Size())
	}

	buf.Clear()

	if buf.Size() != 0 {
		t.Errorf("Expected size 0 after clear, got %d", buf.Size())
	}
	if !buf.IsEmpty() {
		t.Error("Expected buffer to be empty after clear")
	}
}

func TestCircularBufferOnDrop(t *testing.T) {
	var droppedItems []int
	var mu sync.Mutex

	buf, err := NewCircularBuffer[int](2,
		WithOverflowPolicy[int](DropOldest),
		WithDropCallback(func(item int) {
			mu.Lock()
			droppedItems = append(droppedItems, item)
			mu.Unlock()
		}),
	)
	if err != nil {
		t.Fatalf("Failed to create buffer: %v", err)
	}
	defer buf.Close()

	// Fill and overflow
	_ = buf.Write(1)
	_ = buf.Write(2)
	_ = buf.Write(3) // Should drop 1
	_ = buf.Write(4) // Should drop 2

	mu.Lock()
	if len(droppedItems) != 2 {
		t.Errorf("Expected 2 dropped items, got %d", len(droppedItems))
	}
	if len(droppedItems) >= 2 && (droppedItems[0] != 1 || droppedItems[1] != 2) {
		t.Errorf("Expected dropped items [1, 2], got %v", droppedItems)
	}
	mu.Unlock()
}

func TestCircularBufferGenericTypes(t *testing.T) {
	// Test with different types to ensure generics work properly

	// String buffer
	stringBuf, err := NewCircularBuffer[string](3)
	if err != nil {
		t.Fatalf("Failed to create string buffer: %v", err)
	}
	defer stringBuf.Close()

	_ = stringBuf.Write("hello")
	_ = stringBuf.Write("world")

	value, ok := stringBuf.Read()
	if !ok || value != "hello" {
		t.Errorf("String buffer failed: expected 'hello', got %s (ok=%v)", value, ok)
	}

	// Struct buffer
	type TestStruct struct {
		ID   int
		Name string
	}

	structBuf, err := NewCircularBuffer[TestStruct](2)
	if err != nil {
		t.Fatalf("Failed to create struct buffer: %v", err)
	}
	defer structBuf.Close()

	_ = structBuf.Write(TestStruct{ID: 1, Name: "first"})
	_ = structBuf.Write(TestStruct{ID: 2, Name: "second"})

	result, ok := structBuf.Read()
	if !ok || result.ID != 1 || result.Name != "first" {
		t.Errorf("Struct buffer failed: expected {1, 'first'}, got %+v (ok=%v)", result, ok)
	}
}

func TestCircularBufferEdgeCases(t *testing.T) {
	// Test with capacity 1
	buf, err := NewCircularBuffer[int](1)
	if err != nil {
		t.Fatalf("Failed to create buffer: %v", err)
	}
	defer buf.Close()

	_ = buf.Write(1)
	if !buf.IsFull() {
		t.Error("Buffer with capacity 1 should be full after one write")
	}

	value, ok := buf.Read()
	if !ok || value != 1 {
		t.Errorf("Expected to read 1, got %d (ok=%v)", value, ok)
	}

	// Test reading from empty buffer
	_, ok = buf.Read()
	if ok {
		t.Error("Reading from empty buffer should return false")
	}

	// Test peeking empty buffer
	_, ok = buf.Peek()
	if ok {
		t.Error("Peeking empty buffer should return false")
	}

	// Test ReadBatch on empty buffer
	batch := buf.ReadBatch(5)
	if len(batch) != 0 {
		t.Errorf("ReadBatch on empty buffer should return empty slice, got %v", batch)
	}
}

func TestBufferBlockingPolicy(t *testing.T) {
	// Note: Block policy will be challenging to test without timeouts
	// This test verifies the policy is set correctly
	buf, err := NewCircularBuffer[int](2, WithOverflowPolicy[int](Block))
	if err != nil {
		t.Fatalf("Failed to create buffer: %v", err)
	}
	defer buf.Close()

	// Fill buffer
	_ = buf.Write(1)
	_ = buf.Write(2)

	// For now, just verify it doesn't panic
	// In full implementation, Write with Block policy should block
	// until space is available
	if !buf.IsFull() {
		t.Error("Buffer should be full")
	}
}

func TestBlockingPolicyWithTimeout(t *testing.T) {
	buf, err := NewCircularBuffer[int](2, WithOverflowPolicy[int](Block))
	if err != nil {
		t.Fatalf("Failed to create buffer: %v", err)
	}
	defer buf.Close()

	// Fill buffer
	if err := buf.Write(1); err != nil {
		t.Fatalf("Failed to write first item: %v", err)
	}
	if err := buf.Write(2); err != nil {
		t.Fatalf("Failed to write second item: %v", err)
	}

	// Test WriteWithTimeout when buffer is full
	start := time.Now()
	err = buf.(*circularBuffer[int]).WriteWithTimeout(3, 100*time.Millisecond)
	elapsed := time.Since(start)

	if err == nil {
		t.Error("Expected timeout error when buffer is full")
	}
	if err != context.DeadlineExceeded {
		t.Errorf("Expected context.DeadlineExceeded, got %v", err)
	}
	if elapsed < 90*time.Millisecond || elapsed > 200*time.Millisecond {
		t.Errorf("Expected ~100ms timeout, got %v", elapsed)
	}
}

func TestBlockingPolicyWithContextCancellation(t *testing.T) {
	buf, err := NewCircularBuffer[int](2, WithOverflowPolicy[int](Block))
	require.NoError(t, err, "Failed to create blocking buffer")
	defer buf.Close()

	// Fill buffer
	_ = buf.Write(1)
	_ = buf.Write(2)

	// Test WriteWithContext cancellation
	ctx, cancel := context.WithCancel(context.Background())

	// Start a goroutine to cancel context after a short delay
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	err = buf.(*circularBuffer[int]).WriteWithContext(ctx, 3)
	elapsed := time.Since(start)

	if err == nil {
		t.Error("Expected cancellation error")
	}
	if err != context.Canceled {
		t.Errorf("Expected context.Canceled, got %v", err)
	}
	if elapsed < 40*time.Millisecond || elapsed > 100*time.Millisecond {
		t.Errorf("Expected ~50ms cancellation, got %v", elapsed)
	}
}

func TestBlockingPolicyUnblocksOnRead(t *testing.T) {
	buf, err := NewCircularBuffer[int](2, WithOverflowPolicy[int](Block))
	if err != nil {
		t.Fatalf("Failed to create buffer: %v", err)
	}
	defer buf.Close()

	// Fill buffer
	_ = buf.Write(1)
	_ = buf.Write(2)

	var wg sync.WaitGroup
	var writeErr error

	// Start blocking write in goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()
		writeErr = buf.Write(3)
	}()

	// Wait a bit to ensure write is blocked
	time.Sleep(50 * time.Millisecond)

	// Read to unblock write
	value, ok := buf.Read()
	if !ok || value != 1 {
		t.Errorf("Expected to read 1, got %d (ok=%v)", value, ok)
	}

	// Wait for write to complete
	wg.Wait()

	if writeErr != nil {
		t.Errorf("Write should have succeeded after read, got error: %v", writeErr)
	}

	// Verify the write succeeded
	if buf.Size() != 2 {
		t.Errorf("Expected size 2 after unblocking write, got %d", buf.Size())
	}
}

func TestErrorFrameworkIntegration(t *testing.T) {
	buf, err := NewCircularBuffer[int](2)
	if err != nil {
		t.Fatalf("Failed to create buffer: %v", err)
	}
	defer buf.Close()

	// Close buffer first
	_ = buf.Close()

	// Test Write returns framework error
	err = buf.Write(1)
	if err == nil {
		t.Fatal("Expected error when writing to closed buffer")
	}

	// Verify it's a classified error
	var classifiedErr *cerrors.ClassifiedError
	if !errors.As(err, &classifiedErr) {
		t.Error("Expected error to be classified")
	} else {
		if classifiedErr.Class != cerrors.ErrorInvalid {
			t.Errorf("Expected ErrorInvalid class, got %v", classifiedErr.Class)
		}
		if classifiedErr.Component != "Buffer" {
			t.Errorf("Expected component 'Buffer', got %s", classifiedErr.Component)
		}
		if classifiedErr.Operation != "Write" {
			t.Errorf("Expected operation 'Write', got %s", classifiedErr.Operation)
		}
	}

	// Verify it wraps ErrAlreadyStopped
	if !errors.Is(err, cerrors.ErrAlreadyStopped) {
		t.Error("Expected error to wrap ErrAlreadyStopped")
	}
}

func TestWriteWithContextClosedBuffer(t *testing.T) {
	buf, err := NewCircularBuffer[int](2, WithOverflowPolicy[int](Block))
	if err != nil {
		t.Fatalf("Failed to create buffer: %v", err)
	}
	defer buf.Close()

	// Close buffer
	_ = buf.Close()

	// Test WriteWithContext returns framework error
	ctx := context.Background()
	err = buf.(*circularBuffer[int]).WriteWithContext(ctx, 1)

	if err == nil {
		t.Fatal("Expected error when writing to closed buffer")
	}

	// Verify it's the correct framework error
	if !errors.Is(err, cerrors.ErrAlreadyStopped) {
		t.Error("Expected error to wrap ErrAlreadyStopped")
	}
}

func TestConcurrentContextCancellations(t *testing.T) {
	buf, err := NewCircularBuffer[int](1, WithOverflowPolicy[int](Block))
	if err != nil {
		t.Fatalf("Failed to create buffer: %v", err)
	}
	defer buf.Close()

	// Fill buffer
	_ = buf.Write(1)

	var wg sync.WaitGroup
	var errs []error
	var errorsMutex sync.Mutex

	numGoroutines := 10

	// Start multiple goroutines trying to write with context cancellation
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
			defer cancel()

			err := buf.(*circularBuffer[int]).WriteWithContext(ctx, id)

			errorsMutex.Lock()
			errs = append(errs, err)
			errorsMutex.Unlock()
		}(i)
	}

	wg.Wait()

	// All should have failed with context errors
	errorsMutex.Lock()
	defer errorsMutex.Unlock()

	if len(errs) != numGoroutines {
		t.Errorf("Expected %d errors, got %d", numGoroutines, len(errs))
	}

	for i, err := range errs {
		if err == nil {
			t.Errorf("Goroutine %d should have failed with timeout", i)
		} else if err != context.DeadlineExceeded {
			t.Errorf("Goroutine %d: expected DeadlineExceeded, got %v", i, err)
		}
	}
}

func TestBlockingPolicyNoGoroutineLeaks(t *testing.T) {
	initialGoroutines := countGoroutines()

	buf, err := NewCircularBuffer[int](1, WithOverflowPolicy[int](Block))
	if err != nil {
		t.Fatalf("Failed to create buffer: %v", err)
	}
	defer buf.Close()

	// Fill buffer
	_ = buf.Write(1)

	// Test multiple cancelled context operations
	for i := 0; i < 10; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		_ = buf.(*circularBuffer[int]).WriteWithContext(ctx, i)
		cancel()
	}

	// Give time for goroutines to cleanup
	time.Sleep(100 * time.Millisecond)

	finalGoroutines := countGoroutines()

	// Allow for some variance in goroutine count but no significant leak
	if finalGoroutines > initialGoroutines+2 {
		t.Errorf("Potential goroutine leak: started with %d, ended with %d", initialGoroutines, finalGoroutines)
	}
}

func TestWriteWithContextNoLeaksOnSuccess(t *testing.T) {
	initialGoroutines := countGoroutines()

	buf, err := NewCircularBuffer[int](2, WithOverflowPolicy[int](Block))
	if err != nil {
		t.Fatalf("Failed to create buffer: %v", err)
	}
	defer buf.Close()

	// Fill buffer leaving one space
	_ = buf.Write(1)

	// Test successful WriteWithContext operations (should not leak goroutines)
	for i := 0; i < 10; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		err := buf.(*circularBuffer[int]).WriteWithContext(ctx, i)
		if err != nil {
			t.Errorf("WriteWithContext failed: %v", err)
		}

		// Read immediately to make space
		buf.Read()
		cancel()
	}

	// Give time for goroutines to cleanup
	time.Sleep(50 * time.Millisecond)

	finalGoroutines := countGoroutines()

	// Should not leak goroutines even on successful writes
	if finalGoroutines > initialGoroutines+1 {
		t.Errorf(
			"Goroutine leak on successful writes: started with %d, ended with %d",
			initialGoroutines,
			finalGoroutines,
		)
	}
}

// Helper function to count goroutines for leak detection
func countGoroutines() int {
	return runtime.NumGoroutine()
}
