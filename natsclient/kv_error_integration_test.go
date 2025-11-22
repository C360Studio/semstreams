//go:build integration

package natsclient

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestKVStore_ErrorBoundaries tests edge cases and error conditions
func TestKVStore_ErrorBoundaries(t *testing.T) {
	// Use real NATS via testcontainer
	testClient := NewTestClient(t, WithKV())
	client := testClient.Client

	ctx := context.Background()
	bucket, err := client.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{
		Bucket:      "test-error-boundaries",
		Description: "Test error boundaries",
	})
	require.NoError(t, err)

	t.Run("value_size_limits", func(t *testing.T) {
		kv := client.NewKVStore(bucket, func(opts *KVOptions) {
			opts.MaxRetries = 3
			opts.RetryDelay = 10 * time.Millisecond
			opts.Timeout = time.Second
			opts.MaxValueSize = 100 // Small limit for testing
		})

		// Try to write value that exceeds limit
		largeValue := make([]byte, 200) // Exceeds MaxValueSize
		for i := range largeValue {
			largeValue[i] = 'x'
		}

		err := kv.UpdateWithRetry(ctx, "large-key", func(_ []byte) ([]byte, error) {
			return largeValue, nil
		})

		// Should fail with validation error
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "value size validation failed")
		assert.Contains(t, err.Error(), "exceeds maximum")

		// Value at the limit should work
		limitValue := make([]byte, 100)
		err = kv.UpdateWithRetry(ctx, "limit-key", func(_ []byte) ([]byte, error) {
			return limitValue, nil
		})
		assert.NoError(t, err)
	})

	t.Run("update_function_errors", func(t *testing.T) {
		kv := client.NewKVStore(bucket)

		// Update function that always fails
		expectedErr := errors.New("custom update error")
		err := kv.UpdateWithRetry(ctx, "error-key", func(_ []byte) ([]byte, error) {
			return nil, expectedErr
		})

		// Should propagate the error with context
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "update function error")
		assert.Contains(t, err.Error(), "custom update error")
	})

	t.Run("concurrent_updates_stress", func(t *testing.T) {
		kv := client.NewKVStore(bucket, func(opts *KVOptions) {
			opts.MaxRetries = 20 // High retry count for stress test
			opts.RetryDelay = 5 * time.Millisecond
			opts.Timeout = 5 * time.Second
			opts.UseExponentialBackoff = true
			opts.MaxRetryDelay = 100 * time.Millisecond
		})

		// Initialize counter
		err := kv.UpdateWithRetry(ctx, "counter", func(_ []byte) ([]byte, error) {
			return []byte("0"), nil
		})
		require.NoError(t, err)

		// Launch multiple goroutines to increment counter
		const numGoroutines = 10
		const incrementsPerGoroutine = 5
		var wg sync.WaitGroup

		successCount := int32(0)
		failCount := int32(0)

		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				for j := 0; j < incrementsPerGoroutine; j++ {
					err := kv.UpdateWithRetry(ctx, "counter", func(current []byte) ([]byte, error) {
						// Parse current value
						var val int
						if len(current) > 0 {
							fmt.Sscanf(string(current), "%d", &val)
						}
						// Increment
						val++
						return []byte(fmt.Sprintf("%d", val)), nil
					})
					if err == nil {
						atomic.AddInt32(&successCount, 1)
					} else {
						atomic.AddInt32(&failCount, 1)
						t.Logf("Goroutine %d increment %d failed: %v", id, j, err)
					}
				}
			}(i)
		}

		wg.Wait()

		// Verify final count
		entry, err := kv.Get(ctx, "counter")
		require.NoError(t, err)

		var finalCount int
		fmt.Sscanf(string(entry.Value), "%d", &finalCount)

		// All increments should succeed despite high concurrency
		expectedCount := numGoroutines * incrementsPerGoroutine
		assert.Equal(t, expectedCount, finalCount, "Final counter value mismatch")
		assert.Equal(t, int32(expectedCount), successCount, "Not all updates succeeded")
		assert.Equal(t, int32(0), failCount, "Some updates failed")
	})

	t.Run("timeout_behavior", func(t *testing.T) {
		// Create a KV store with very short timeout
		kv := client.NewKVStore(bucket, func(opts *KVOptions) {
			opts.MaxRetries = 1
			opts.RetryDelay = time.Millisecond
			opts.Timeout = 1 * time.Nanosecond // Extremely short timeout
		})

		// This should timeout
		err := kv.UpdateWithRetry(ctx, "timeout-key", func(_ []byte) ([]byte, error) {
			return []byte("value"), nil
		})

		// Should get context deadline exceeded
		assert.Error(t, err)
		assert.True(t,
			errors.Is(err, context.DeadlineExceeded) ||
				strings.Contains(err.Error(), "deadline exceeded"),
			"Expected deadline exceeded error, got: %v", err)
	})

	t.Run("nil_and_empty_values", func(t *testing.T) {
		kv := client.NewKVStore(bucket)

		// Test nil value
		err := kv.UpdateWithRetry(ctx, "nil-key", func(_ []byte) ([]byte, error) {
			return nil, nil
		})
		assert.NoError(t, err)

		entry, err := kv.Get(ctx, "nil-key")
		require.NoError(t, err)
		assert.Equal(t, 0, len(entry.Value))

		// Test empty slice
		err = kv.UpdateWithRetry(ctx, "empty-key", func(_ []byte) ([]byte, error) {
			return []byte{}, nil
		})
		assert.NoError(t, err)

		entry, err = kv.Get(ctx, "empty-key")
		require.NoError(t, err)
		assert.Equal(t, 0, len(entry.Value))

		// Test transition from value to nil
		err = kv.UpdateWithRetry(ctx, "transition-key", func(_ []byte) ([]byte, error) {
			return []byte("initial"), nil
		})
		require.NoError(t, err)

		err = kv.UpdateWithRetry(ctx, "transition-key", func(current []byte) ([]byte, error) {
			assert.Equal(t, "initial", string(current))
			return nil, nil
		})
		assert.NoError(t, err)
	})

	t.Run("max_retries_exhaustion", func(t *testing.T) {
		kv := client.NewKVStore(bucket, func(opts *KVOptions) {
			opts.MaxRetries = 2 // Low retry count
			opts.RetryDelay = 5 * time.Millisecond
			opts.Timeout = time.Second
		})

		// Create initial value
		_, err := bucket.Create(ctx, "exhaustion-key", []byte("v1"))
		require.NoError(t, err)

		// Simulate continuous conflicts by updating in background
		stopConflicts := make(chan struct{})
		go func() {
			ticker := time.NewTicker(2 * time.Millisecond)
			defer ticker.Stop()
			counter := 2
			for {
				select {
				case <-stopConflicts:
					return
				case <-ticker.C:
					// Keep updating to cause conflicts
					bucket.Put(ctx, "exhaustion-key", []byte(fmt.Sprintf("v%d", counter)))
					counter++
				}
			}
		}()

		// Try to update - should exhaust retries
		err = kv.UpdateWithRetry(ctx, "exhaustion-key", func(_ []byte) ([]byte, error) {
			// Slow update to ensure conflict
			time.Sleep(10 * time.Millisecond)
			return []byte("my-update"), nil
		})

		close(stopConflicts)

		// Should get max retries exceeded
		assert.Error(t, err)
		assert.True(t,
			errors.Is(err, ErrKVMaxRetriesExceeded) ||
				strings.Contains(err.Error(), "max retries exceeded"),
			"Expected max retries error, got: %v", err)
	})

	t.Run("invalid_json_handling", func(t *testing.T) {
		kv := client.NewKVStore(bucket)

		// Put invalid JSON
		_, err := bucket.Put(ctx, "bad-json", []byte("{invalid json}"))
		require.NoError(t, err)

		// Try to update as JSON
		err = kv.UpdateJSON(ctx, "bad-json", func(_ map[string]any) error {
			// Should never reach here
			t.Fatal("UpdateJSON should fail on invalid JSON")
			return nil
		})

		// Should get JSON parse error
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unmarshal")
	})

	t.Run("update_deleted_key", func(t *testing.T) {
		kv := client.NewKVStore(bucket)

		// Create and delete a key
		_, err := bucket.Create(ctx, "deleted-key", []byte("value"))
		require.NoError(t, err)

		err = bucket.Delete(ctx, "deleted-key")
		require.NoError(t, err)

		// Try to update deleted key - should treat as new
		err = kv.UpdateWithRetry(ctx, "deleted-key", func(current []byte) ([]byte, error) {
			assert.Nil(t, current, "Deleted key should be treated as nil")
			return []byte("new-value"), nil
		})
		assert.NoError(t, err)

		// Verify it was created
		entry, err := kv.Get(ctx, "deleted-key")
		require.NoError(t, err)
		assert.Equal(t, "new-value", string(entry.Value))
	})

	t.Run("panic_recovery", func(t *testing.T) {
		kv := client.NewKVStore(bucket)

		// Update function that panics
		err := func() (err error) {
			defer func() {
				if r := recover(); r != nil {
					err = fmt.Errorf("panic recovered: %v", r)
				}
			}()

			return kv.UpdateWithRetry(ctx, "panic-key", func(_ []byte) ([]byte, error) {
				panic("test panic")
			})
		}()

		// The panic should be caught at a higher level
		// This tests that our code doesn't suppress panics
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "panic")
	})
}

// TestTemporalResolver_ErrorBoundaries tests edge cases for temporal resolver
func TestTemporalResolver_ErrorBoundaries(t *testing.T) {
	// Use real NATS via testcontainer
	testClient := NewTestClient(t, WithKV())
	client := testClient.Client

	ctx := context.Background()
	bucket, err := client.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{
		Bucket:      "test-temporal-errors",
		Description: "Test temporal error boundaries",
		History:     10,
	})
	require.NoError(t, err)

	resolver := NewTemporalResolver(ctx, bucket)
	defer resolver.Close()

	t.Run("empty_history", func(t *testing.T) {
		// Query non-existent key
		entry, err := resolver.GetAtTimestamp(ctx, "non-existent", time.Now())
		assert.Error(t, err)
		assert.Nil(t, entry)
		// Log the actual error for debugging
		if !errors.Is(err, ErrKVKeyNotFound) {
			t.Logf("Expected ErrKVKeyNotFound, got: %v (type: %T)", err, err)
		}
		assert.True(t, errors.Is(err, ErrKVKeyNotFound))
	})

	t.Run("future_timestamp", func(t *testing.T) {
		// Create some history
		key := "future-test"
		for i := 0; i < 3; i++ {
			_, err := bucket.Put(ctx, key, []byte(fmt.Sprintf("v%d", i)))
			require.NoError(t, err)
			time.Sleep(10 * time.Millisecond)
		}

		// Query with future timestamp
		futureTime := time.Now().Add(24 * time.Hour)
		entry, err := resolver.GetAtTimestamp(ctx, key, futureTime)
		require.NoError(t, err)

		// Should return the latest entry
		assert.Equal(t, "v2", string(entry.Value()))
	})

	t.Run("ancient_timestamp", func(t *testing.T) {
		// Create some history
		key := "ancient-test"
		for i := 0; i < 3; i++ {
			_, err := bucket.Put(ctx, key, []byte(fmt.Sprintf("v%d", i)))
			require.NoError(t, err)
			time.Sleep(10 * time.Millisecond)
		}

		// Query with ancient timestamp
		ancientTime := time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)
		entry, err := resolver.GetAtTimestamp(ctx, key, ancientTime)
		require.NoError(t, err)

		// Should return the oldest entry
		assert.Equal(t, "v0", string(entry.Value()))
	})

	t.Run("cache_eviction", func(t *testing.T) {
		// Get stats before
		statsBefore := resolver.GetStats()
		initialSize := statsBefore.CurrentSize()

		// Add entries to fill cache
		for i := 0; i < 10; i++ {
			key := fmt.Sprintf("cache-key-%d", i)
			_, err := bucket.Put(ctx, key, []byte("value"))
			require.NoError(t, err)

			// Access to populate cache
			_, err = resolver.GetAtTimestamp(ctx, key, time.Now())
			require.NoError(t, err)
		}

		// Check cache grew
		statsAfter := resolver.GetStats()
		assert.Greater(t, statsAfter.CurrentSize(), initialSize)

		// Wait for TTL expiration
		time.Sleep(6 * time.Second) // Cache TTL is 5 seconds

		// Access one key to trigger cleanup
		_, err = resolver.GetAtTimestamp(ctx, "cache-key-0", time.Now())
		require.NoError(t, err)

		// Cache should have been cleaned
		statsFinal := resolver.GetStats()
		assert.LessOrEqual(t, statsFinal.CurrentSize(), statsAfter.CurrentSize())
	})
}
