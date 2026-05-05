//go:build integration

package natsclient

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestKVBucketAdapter_SentinelTranslation verifies that the kvBucketAdapter
// correctly re-exposes jetstream sentinel errors via the natsclient sentinel
// variables (ErrKeyNotFound, ErrKeyExists, ErrNoKeysFound). This is the
// primary integration concern: if NATS changes sentinel identity across
// versions the adapter must catch the regression.
func TestKVBucketAdapter_SentinelTranslation(t *testing.T) {
	tc := NewTestClient(t, WithKV())
	ctx := context.Background()

	js, err := tc.Client.JetStream()
	require.NoError(t, err)

	rawKV, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket:  "TEST_SENTINEL_TRANS",
		History: 1,
	})
	require.NoError(t, err)

	bucket := WrapKV(rawKV)

	t.Run("Get missing key returns ErrKeyNotFound", func(t *testing.T) {
		_, err := bucket.Get(ctx, "absent")
		assert.True(t, errors.Is(err, ErrKeyNotFound), "got %v", err)
	})

	t.Run("Keys on empty bucket returns ErrNoKeysFound", func(t *testing.T) {
		_, err := bucket.Keys(ctx)
		assert.True(t, errors.Is(err, ErrNoKeysFound), "got %v", err)
	})

	t.Run("Update stale revision returns ErrKeyExists", func(t *testing.T) {
		rev, err := bucket.Put(ctx, "k", []byte("v1"))
		require.NoError(t, err)

		// Write a new revision so rev is now stale.
		_, err = bucket.Put(ctx, "k", []byte("v2"))
		require.NoError(t, err)

		_, err = bucket.Update(ctx, "k", []byte("v3"), rev)
		assert.True(t, errors.Is(err, ErrKeyExists), "got %v", err)
	})
}

// TestKVBucketAdapter_GetPutUpdate verifies the basic CRUD path of the adapter.
func TestKVBucketAdapter_GetPutUpdate(t *testing.T) {
	tc := NewTestClient(t, WithKV())
	ctx := context.Background()

	js, err := tc.Client.JetStream()
	require.NoError(t, err)

	rawKV, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket:  "TEST_BUCKET_CRUD",
		History: 2,
	})
	require.NoError(t, err)

	bucket := WrapKV(rawKV)

	// Put.
	rev1, err := bucket.Put(ctx, "foo", []byte("bar"))
	require.NoError(t, err)
	assert.Greater(t, rev1, uint64(0))

	// Get.
	entry, err := bucket.Get(ctx, "foo")
	require.NoError(t, err)
	assert.Equal(t, "foo", entry.Key)
	assert.Equal(t, []byte("bar"), entry.Value)
	assert.Equal(t, rev1, entry.Revision)

	// Update with correct revision.
	rev2, err := bucket.Update(ctx, "foo", []byte("baz"), rev1)
	require.NoError(t, err)
	assert.Greater(t, rev2, rev1)

	// Verify new value.
	entry2, err := bucket.Get(ctx, "foo")
	require.NoError(t, err)
	assert.Equal(t, []byte("baz"), entry2.Value)

	// Delete.
	require.NoError(t, bucket.Delete(ctx, "foo"))
	_, err = bucket.Get(ctx, "foo")
	assert.True(t, errors.Is(err, ErrKeyNotFound), "got %v after delete", err)
}

// TestKVBucketAdapter_Keys verifies Keys() returns populated keys and
// ErrNoKeysFound on an empty bucket.
func TestKVBucketAdapter_Keys(t *testing.T) {
	tc := NewTestClient(t, WithKV())
	ctx := context.Background()

	js, err := tc.Client.JetStream()
	require.NoError(t, err)

	rawKV, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket:  "TEST_BUCKET_KEYS",
		History: 1,
	})
	require.NoError(t, err)

	bucket := WrapKV(rawKV)

	// Empty.
	_, err = bucket.Keys(ctx)
	assert.True(t, errors.Is(err, ErrNoKeysFound), "empty bucket: got %v", err)

	// Populated.
	_, _ = bucket.Put(ctx, "a", []byte("1"))
	_, _ = bucket.Put(ctx, "b", []byte("2"))

	keys, err := bucket.Keys(ctx)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"a", "b"}, keys)
}

// TestKVBucketAdapter_Bucket verifies the Bucket() name accessor round-trips.
func TestKVBucketAdapter_Bucket(t *testing.T) {
	tc := NewTestClient(t, WithKV())
	ctx := context.Background()

	js, err := tc.Client.JetStream()
	require.NoError(t, err)

	const bucketName = "TEST_BUCKET_NAME"
	rawKV, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket:  bucketName,
		History: 1,
	})
	require.NoError(t, err)

	bucket := WrapKV(rawKV)
	assert.Equal(t, bucketName, bucket.Bucket())
}

// TestKVBucketAdapter_Watch_KVTwoferBootstrapDelimiter verifies that the Watch
// adapter preserves the KV-twofer end-of-bootstrap nil delimiter.
//
// The adapter wraps jetstream's nil KeyValueEntry as a zero-value KVEntry
// (Value==nil). This test confirms the delimiter arrives after all current
// values and before any live updates.
func TestKVBucketAdapter_Watch_KVTwoferBootstrapDelimiter(t *testing.T) {
	tc := NewTestClient(t, WithKV())
	ctx := context.Background()

	js, err := tc.Client.JetStream()
	require.NoError(t, err)

	rawKV, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket:  "TEST_BUCKET_WATCH_BOOT",
		History: 1,
	})
	require.NoError(t, err)

	// Pre-populate two entries.
	_, err = rawKV.Put(ctx, "x", []byte("hello"))
	require.NoError(t, err)
	_, err = rawKV.Put(ctx, "y", []byte("world"))
	require.NoError(t, err)

	bucket := WrapKV(rawKV)
	w, err := bucket.Watch(ctx, ">")
	require.NoError(t, err)
	defer w.Stop()

	ch := w.Updates()

	// Drain the bootstrap snapshot + delimiter.
	seenKeys := map[string]bool{}
	delimiterSeen := false
	timeout := time.After(5 * time.Second)

	for i := 0; i < 3; i++ { // 2 initial entries + 1 delimiter
		select {
		case entry := <-ch:
			if entry.Value == nil {
				delimiterSeen = true
			} else {
				seenKeys[entry.Key] = true
			}
		case <-timeout:
			t.Fatal("timed out waiting for bootstrap entries")
		}
	}

	assert.True(t, delimiterSeen, "end-of-bootstrap nil delimiter not received")
	assert.True(t, seenKeys["x"], "snapshot missing key x")
	assert.True(t, seenKeys["y"], "snapshot missing key y")
}

// TestKVBucketAdapter_Watch_LiveUpdates verifies that writes after the
// bootstrap delimiter arrive on the watcher channel.
func TestKVBucketAdapter_Watch_LiveUpdates(t *testing.T) {
	tc := NewTestClient(t, WithKV())
	ctx := context.Background()

	js, err := tc.Client.JetStream()
	require.NoError(t, err)

	rawKV, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket:  "TEST_BUCKET_WATCH_LIVE",
		History: 1,
	})
	require.NoError(t, err)

	bucket := WrapKV(rawKV)
	w, err := bucket.Watch(ctx, ">")
	require.NoError(t, err)
	defer w.Stop()

	ch := w.Updates()
	timeout := time.After(5 * time.Second)

	// Drain bootstrap delimiter (empty bucket).
	select {
	case delim := <-ch:
		require.Nil(t, delim.Value, "expected delimiter, got key=%q", delim.Key)
	case <-timeout:
		t.Fatal("timed out waiting for bootstrap delimiter")
	}

	// Publish a live update.
	_, err = bucket.Put(ctx, "live", []byte("update"))
	require.NoError(t, err)

	select {
	case entry := <-ch:
		assert.Equal(t, "live", entry.Key)
		assert.Equal(t, []byte("update"), entry.Value)
	case <-timeout:
		t.Fatal("timed out waiting for live update")
	}
}

// TestWrapKV_NilInput verifies the nil-idempotency guarantee: WrapKV(nil)
// returns nil so degraded-startup code that passes nil continues to work.
func TestWrapKV_NilInput(t *testing.T) {
	result := WrapKV(nil)
	if result != nil {
		t.Errorf("WrapKV(nil) = %v, want nil", result)
	}
}
