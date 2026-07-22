//go:build integration

package objectstore_test

import (
	"context"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/storage"
	"github.com/c360studio/semstreams/storage/objectstore"
)

// TestIntegration_StoreGetMapsObjectNotFound proves the FIX-2 seam end-to-end
// against real NATS: a Get of an absent object surfaces the backend-agnostic
// storage.ErrObjectNotFound sentinel (via errors.Is), NOT an opaque transient
// error — which is what lets fusion classify the #600 case as not_found rather
// than a fault.
func TestIntegration_StoreGetMapsObjectNotFound(t *testing.T) {
	natsClient := getSharedNATSClient(t)
	ctx := context.Background()

	store, err := objectstore.NewStoreWithConfig(ctx, natsClient,
		objectstore.Config{BucketName: "TEST_NOT_FOUND_MAPPING"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	_, err = store.Get(ctx, "content/never/put/this/key")
	require.Error(t, err)
	assert.ErrorIs(t, err, storage.ErrObjectNotFound,
		"an absent object must surface the backend-agnostic not-found sentinel")
}

// backingStreamMaxAge reads a content ObjectStore's backing-stream MaxAge
// (OBJ_<bucket>) straight from JetStream — the ground truth the D2 guard
// reconciles against.
func backingStreamMaxAge(t *testing.T, ctx context.Context, bucket string) time.Duration {
	t.Helper()
	js, err := sharedNATSClient.JetStream()
	require.NoError(t, err)
	stream, err := js.Stream(ctx, "OBJ_"+bucket)
	require.NoError(t, err)
	info, err := stream.Info(ctx)
	require.NoError(t, err)
	return info.Config.MaxAge
}

// TestIntegration_ContentStoreRetentionReconcile proves the D2 boot guard
// (ADR-068; #600) against real NATS: a backing stream carrying the legacy 24h TTL
// is stripped in place at construction and the store boots, and a store created
// fresh through the constructor carries no retention.
func TestIntegration_ContentStoreRetentionReconcile(t *testing.T) {
	natsClient := getSharedNATSClient(t)
	ctx := context.Background()

	t.Run("a legacy 24h MaxAge is stripped in place and the store boots", func(t *testing.T) {
		const bucket = "TEST_LEGACY_RETENTION"

		// Simulate a pre-contract persistent bucket: create the backing ObjectStore
		// directly with the historical 24h TTL the old constructor stamped.
		js, err := natsClient.JetStream()
		require.NoError(t, err)
		_, err = js.CreateObjectStore(ctx, jetstream.ObjectStoreConfig{
			Bucket:      bucket,
			Description: "legacy retention fixture",
			TTL:         24 * time.Hour,
		})
		require.NoError(t, err)
		require.Equal(t, 24*time.Hour, backingStreamMaxAge(t, ctx, bucket),
			"fixture precondition: the backing stream carries the legacy TTL")

		// Constructing through the shared ctor must reconcile-then-assert: strip the
		// TTL and boot cleanly (no fail-closed).
		store, err := objectstore.NewStoreWithConfig(ctx, natsClient, objectstore.Config{BucketName: bucket})
		require.NoError(t, err, "the store must boot after stripping the legacy retention")
		require.NotNil(t, store)
		t.Cleanup(func() { _ = store.Close() })

		assert.Equal(t, time.Duration(0), backingStreamMaxAge(t, ctx, bucket),
			"the boot guard must have stripped the backing stream's MaxAge in place")
	})

	t.Run("stripping deletes no stored object", func(t *testing.T) {
		const bucket = "TEST_LEGACY_RETENTION_KEEP"

		js, err := natsClient.JetStream()
		require.NoError(t, err)
		os, err := js.CreateObjectStore(ctx, jetstream.ObjectStoreConfig{
			Bucket: bucket,
			TTL:    24 * time.Hour,
		})
		require.NoError(t, err)
		_, err = os.PutBytes(ctx, "evidence/key", []byte("verbatim body"))
		require.NoError(t, err)

		store, err := objectstore.NewStoreWithConfig(ctx, natsClient, objectstore.Config{BucketName: bucket})
		require.NoError(t, err)
		t.Cleanup(func() { _ = store.Close() })

		got, err := store.Get(ctx, "evidence/key")
		require.NoError(t, err, "reconciliation must not delete stored content")
		assert.Equal(t, []byte("verbatim body"), got)
		assert.Equal(t, time.Duration(0), backingStreamMaxAge(t, ctx, bucket))
	})

	t.Run("a freshly constructed content store carries no retention", func(t *testing.T) {
		const bucket = "TEST_CLEAN_RETENTION"

		store, err := objectstore.NewStoreWithConfig(ctx, natsClient, objectstore.Config{BucketName: bucket})
		require.NoError(t, err, "a clean content store must boot")
		require.NotNil(t, store)
		t.Cleanup(func() { _ = store.Close() })

		assert.Equal(t, time.Duration(0), backingStreamMaxAge(t, ctx, bucket),
			"the constructor must stamp no MaxAge on the backing stream")
	})
}
