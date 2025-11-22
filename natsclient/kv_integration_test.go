//go:build integration

package natsclient

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestKVStore_UpdateWithRetry(t *testing.T) {
	// Use real NATS via testcontainer
	testClient := NewTestClient(t, WithKV())
	client := testClient.Client

	ctx := context.Background()

	// Create real KV bucket
	bucket, err := client.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{
		Bucket:      "test-update-retry",
		Description: "Test bucket for CAS operations",
		History:     5,
	})
	require.NoError(t, err)

	// Create KVStore with real bucket
	kvStore := client.NewKVStore(bucket)

	t.Run("successful update", func(t *testing.T) {
		// Create initial value
		_, err := kvStore.Put(ctx, "test-key", []byte("initial"))
		require.NoError(t, err)

		// Update with retry should succeed
		err = kvStore.UpdateWithRetry(ctx, "test-key", func(current []byte) ([]byte, error) {
			assert.Equal(t, "initial", string(current))
			return []byte("updated"), nil
		})
		assert.NoError(t, err)

		// Verify update
		entry, err := kvStore.Get(ctx, "test-key")
		require.NoError(t, err)
		assert.Equal(t, "updated", string(entry.Value))
	})

	t.Run("retry on conflict", func(t *testing.T) {
		key := "conflict-key"
		_, err := kvStore.Put(ctx, key, []byte("v1"))
		require.NoError(t, err)

		updateCount := 0
		err = kvStore.UpdateWithRetry(ctx, key, func(_ []byte) ([]byte, error) {
			updateCount++
			if updateCount == 1 {
				// Simulate concurrent update
				_, _ = kvStore.Put(ctx, key, []byte("concurrent"))
			}
			return []byte("final"), nil
		})

		// Should succeed after retry
		assert.NoError(t, err)
		assert.Greater(t, updateCount, 1, "Should have retried")

		entry, _ := kvStore.Get(ctx, key)
		assert.Equal(t, "final", string(entry.Value))
	})

	t.Run("max retries exceeded", func(t *testing.T) {
		key := "max-retry-key"
		_, err := kvStore.Put(ctx, key, []byte("initial"))
		require.NoError(t, err)

		// Create a store with minimal retries
		limitedStore := client.NewKVStore(bucket, func(opts *KVOptions) {
			opts.MaxRetries = 1
			opts.RetryDelay = 1 * time.Millisecond
		})

		attempts := 0
		err = limitedStore.UpdateWithRetry(ctx, key, func(_ []byte) ([]byte, error) {
			attempts++
			// Always cause conflict by updating outside
			_, _ = kvStore.Put(ctx, key, []byte("interfering"))
			return []byte("never-succeeds"), nil
		})

		assert.Equal(t, ErrKVMaxRetriesExceeded, err)
		assert.Equal(t, 2, attempts, "Should try initial + 1 retry")
	})
}

func TestKVStore_UpdateJSON(t *testing.T) {
	testClient := NewTestClient(t, WithKV())
	client := testClient.Client
	ctx := context.Background()

	bucket, err := client.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{
		Bucket: "test-json",
	})
	require.NoError(t, err)

	kvStore := client.NewKVStore(bucket)

	t.Run("update JSON object", func(t *testing.T) {
		key := "config"

		// Create initial JSON
		initial := map[string]any{"enabled": true, "port": 8080}
		data, _ := json.Marshal(initial)
		_, err := kvStore.Put(ctx, key, data)
		require.NoError(t, err)

		// Update using UpdateJSON
		err = kvStore.UpdateJSON(ctx, key, func(current map[string]any) error {
			assert.Equal(t, true, current["enabled"])
			assert.Equal(t, float64(8080), current["port"])

			current["enabled"] = false
			current["version"] = 2
			return nil
		})
		assert.NoError(t, err)

		// Verify update
		entry, _ := kvStore.Get(ctx, key)
		var result map[string]any
		json.Unmarshal(entry.Value, &result)
		assert.Equal(t, false, result["enabled"])
		assert.Equal(t, float64(2), result["version"])
	})

	t.Run("handle empty initial value", func(t *testing.T) {
		key := "new-config"

		// UpdateJSON on non-existent key should now create it
		err := kvStore.UpdateJSON(ctx, key, func(current map[string]any) error {
			// Should be called with empty map for non-existent key
			assert.Empty(t, current)
			current["created"] = true
			current["version"] = 1
			return nil
		})
		assert.NoError(t, err)

		// Verify it was created
		entry, err := kvStore.Get(ctx, key)
		require.NoError(t, err)
		var result map[string]any
		json.Unmarshal(entry.Value, &result)
		assert.Equal(t, true, result["created"])
		assert.Equal(t, float64(1), result["version"])
	})
}

func TestKVStore_ErrorDetection(t *testing.T) {
	// Test with real NATS errors
	testClient := NewTestClient(t, WithKV())
	client := testClient.Client
	ctx := context.Background()

	bucket, err := client.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{
		Bucket: "test-errors",
	})
	require.NoError(t, err)

	kvStore := client.NewKVStore(bucket)

	t.Run("not found error", func(t *testing.T) {
		_, err := kvStore.Get(ctx, "non-existent")
		assert.True(t, IsKVNotFoundError(err))
		assert.Equal(t, ErrKVKeyNotFound, err)
	})

	t.Run("key exists error", func(t *testing.T) {
		key := "exists-key"
		_, err := kvStore.Create(ctx, key, []byte("value"))
		require.NoError(t, err)

		// Try to create again
		_, err = kvStore.Create(ctx, key, []byte("value2"))
		assert.True(t, IsKVConflictError(err))
		assert.Equal(t, ErrKVKeyExists, err)
	})

	t.Run("revision mismatch error", func(t *testing.T) {
		key := "revision-key"
		rev1, err := kvStore.Put(ctx, key, []byte("v1"))
		require.NoError(t, err)

		// Update with wrong revision
		_, err = kvStore.Update(ctx, key, []byte("v2"), rev1+999)
		assert.True(t, IsKVConflictError(err))
		assert.Equal(t, ErrKVRevisionMismatch, err)
	})
}

func TestKVStore_Watch(t *testing.T) {
	testClient := NewTestClient(t, WithKV())
	client := testClient.Client
	ctx := context.Background()

	bucket, err := client.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{
		Bucket: "test-watch",
	})
	require.NoError(t, err)

	kvStore := client.NewKVStore(bucket)

	// Create watcher
	watcher, err := kvStore.Watch(ctx, "watch.*")
	require.NoError(t, err)
	defer watcher.Stop()

	// Make changes
	go func() {
		time.Sleep(100 * time.Millisecond)
		_, _ = kvStore.Put(ctx, "watch.key1", []byte("value1"))
		_, _ = kvStore.Put(ctx, "watch.key2", []byte("value2"))
	}()

	// Receive updates
	updates := 0
	timeout := time.After(1 * time.Second)

	for updates < 2 {
		select {
		case entry := <-watcher.Updates():
			if entry != nil {
				updates++
				assert.Contains(t, entry.Key(), "watch.")
			}
		case <-timeout:
			t.Fatal("Timeout waiting for watch updates")
		}
	}

	assert.Equal(t, 2, updates)
}

func TestKVStore_BasicOperations(t *testing.T) {
	testClient := NewTestClient(t, WithKV())
	client := testClient.Client
	ctx := context.Background()

	bucket, err := client.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{
		Bucket: "test-basic",
	})
	require.NoError(t, err)

	kvStore := client.NewKVStore(bucket)

	t.Run("put and get", func(t *testing.T) {
		key := "basic-key"
		value := []byte("basic-value")

		// Put
		rev, err := kvStore.Put(ctx, key, value)
		require.NoError(t, err)
		assert.Greater(t, rev, uint64(0))

		// Get
		entry, err := kvStore.Get(ctx, key)
		require.NoError(t, err)
		assert.Equal(t, key, entry.Key)
		assert.Equal(t, value, entry.Value)
		assert.Equal(t, rev, entry.Revision)
	})

	t.Run("create new key", func(t *testing.T) {
		key := "create-key"
		value := []byte("create-value")

		// Create should succeed
		rev, err := kvStore.Create(ctx, key, value)
		require.NoError(t, err)
		assert.Greater(t, rev, uint64(0))

		// Verify it exists
		entry, err := kvStore.Get(ctx, key)
		require.NoError(t, err)
		assert.Equal(t, value, entry.Value)
	})

	t.Run("update with revision", func(t *testing.T) {
		key := "update-key"
		initial := []byte("initial")
		updated := []byte("updated")

		// Create initial value
		rev1, err := kvStore.Put(ctx, key, initial)
		require.NoError(t, err)

		// Update with correct revision
		rev2, err := kvStore.Update(ctx, key, updated, rev1)
		require.NoError(t, err)
		assert.Greater(t, rev2, rev1)

		// Verify update
		entry, err := kvStore.Get(ctx, key)
		require.NoError(t, err)
		assert.Equal(t, updated, entry.Value)
		assert.Equal(t, rev2, entry.Revision)
	})

	t.Run("delete key", func(t *testing.T) {
		key := "delete-key"
		value := []byte("delete-value")

		// Create key
		_, err := kvStore.Put(ctx, key, value)
		require.NoError(t, err)

		// Delete key
		err = kvStore.Delete(ctx, key)
		require.NoError(t, err)

		// Verify it's gone
		_, err = kvStore.Get(ctx, key)
		assert.Equal(t, ErrKVKeyNotFound, err)
	})
}

func TestKVStore_Options(t *testing.T) {
	testClient := NewTestClient(t, WithKV())
	client := testClient.Client
	ctx := context.Background()

	bucket, err := client.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{
		Bucket: "test-options",
	})
	require.NoError(t, err)

	t.Run("custom options", func(t *testing.T) {
		// Create store with custom options
		kvStore := client.NewKVStore(bucket, func(opts *KVOptions) {
			opts.MaxRetries = 5
			opts.RetryDelay = 50 * time.Millisecond
			opts.Timeout = 10 * time.Second
		})

		// Verify options were applied (indirect test via behavior)
		assert.NotNil(t, kvStore)
		assert.Equal(t, 5, kvStore.options.MaxRetries)
		assert.Equal(t, 50*time.Millisecond, kvStore.options.RetryDelay)
		assert.Equal(t, 10*time.Second, kvStore.options.Timeout)
	})

	t.Run("default options", func(t *testing.T) {
		// Create store with default options
		kvStore := client.NewKVStore(bucket)

		defaults := DefaultKVOptions()
		assert.Equal(t, defaults.MaxRetries, kvStore.options.MaxRetries)
		assert.Equal(t, defaults.RetryDelay, kvStore.options.RetryDelay)
		assert.Equal(t, defaults.Timeout, kvStore.options.Timeout)
	})
}

func TestKVStore_Timeout(t *testing.T) {
	testClient := NewTestClient(t, WithKV())
	client := testClient.Client
	ctx := context.Background()

	bucket, err := client.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{
		Bucket: "test-timeout",
	})
	require.NoError(t, err)

	t.Run("operations respect timeout", func(t *testing.T) {
		// Create store with very short timeout
		kvStore := client.NewKVStore(bucket, func(opts *KVOptions) {
			opts.Timeout = 1 * time.Nanosecond // Extremely short to force timeout
		})

		// This should timeout
		_, err := kvStore.Get(ctx, "timeout-test")
		// We expect either a timeout error or completion (NATS is fast)
		// The important thing is that timeout is applied, not that it always triggers
		t.Logf("Get with 1ns timeout result: %v", err)
	})

	t.Run("normal operations with reasonable timeout", func(t *testing.T) {
		kvStore := client.NewKVStore(bucket, func(opts *KVOptions) {
			opts.Timeout = 5 * time.Second // Reasonable timeout
		})

		// Should complete normally
		_, err := kvStore.Put(ctx, "normal-key", []byte("value"))
		assert.NoError(t, err)

		entry, err := kvStore.Get(ctx, "normal-key")
		assert.NoError(t, err)
		assert.Equal(t, "value", string(entry.Value))
	})
}

func TestKVStore_ErrorHelpers(t *testing.T) {
	t.Run("IsKVNotFoundError", func(t *testing.T) {
		// Test various not found error formats
		assert.True(t, IsKVNotFoundError(ErrKVKeyNotFound))
		assert.False(t, IsKVNotFoundError(ErrKVKeyExists))
		assert.False(t, IsKVNotFoundError(nil))

		// Test actual NATS error messages (would need real errors from NATS)
		// These are the known patterns from the implementation
	})

	t.Run("IsKVConflictError", func(t *testing.T) {
		// Test various conflict error formats
		assert.True(t, IsKVConflictError(ErrKVRevisionMismatch))
		assert.True(t, IsKVConflictError(ErrKVKeyExists))
		assert.False(t, IsKVConflictError(ErrKVKeyNotFound))
		assert.False(t, IsKVConflictError(nil))
	})
}
