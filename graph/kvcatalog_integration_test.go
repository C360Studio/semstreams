//go:build integration

package graph

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIntegration_RetentionPolicyIsPerDescriptor proves a no-lifecycle
// descriptor strips a foreign TTL through the catalog acquisition seam.
func TestIntegration_RetentionPolicyIsPerDescriptor(t *testing.T) {
	ctx := context.Background()
	tc := natsclient.NewTestClient(t, natsclient.WithKV())
	client := tc.Client

	// EMBEDDING_INDEX pre-created dirty with a foreign TTL (declared
	// no-lifecycle in the catalog).
	_, err := client.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{
		Bucket: BucketEmbeddingIndex,
		TTL:    7 * 24 * time.Hour,
	})
	require.NoError(t, err)

	embedding, err := EnsureCatalogBucket(ctx, client, BucketEmbeddingIndex)
	require.NoError(t, err)
	maxAge, _, err := natsclient.BucketRetention(ctx, embedding)
	require.NoError(t, err)
	assert.Equal(t, time.Duration(0), maxAge,
		"EMBEDDING_INDEX's foreign TTL must be STRIPPED (declared no-lifecycle)")
}

// TestIntegration_OpenCatalogReader_AbsentNamesTheCatalogOwner: the reader
// seam's not-ready error carries the catalog Owner so an operator reads "wait
// for / deploy graph-ingest", and the bucket stays absent.
func TestIntegration_OpenCatalogReader_AbsentNamesTheCatalogOwner(t *testing.T) {
	ctx := context.Background()
	tc := natsclient.NewTestClient(t, natsclient.WithKV())
	client := tc.Client

	_, err := OpenCatalogReader(ctx, client, BucketEntityStates)
	require.Error(t, err)

	var classified *errs.ClassifiedError
	require.True(t, errors.As(err, &classified))
	assert.Equal(t, errs.ErrorTransient, classified.Class)
	assert.Equal(t, ErrorCodeIndexNotReady, classified.Code,
		"the not-ready code must be the one classified consumers already handle")
	assert.Nil(t, classified.Detail)
	assert.Contains(t, err.Error(), "graph-ingest",
		"the error must name the catalog owner of ENTITY_STATES")
	assert.EqualError(t, errors.Unwrap(classified),
		`framework bucket "ENTITY_STATES" is not ready: its owner (graph-ingest) has not provisioned it in this deployment`)

	_, gerr := client.GetKeyValueBucket(ctx, BucketEntityStates)
	assert.ErrorIs(t, gerr, jetstream.ErrBucketNotFound,
		"the reader seam must never create the bucket")
}

// TestIntegration_OpenCatalogReader_DelegatesOnlyReadCapabilities proves the
// private wrapper delegates every approved method while its dynamic type
// cannot be asserted back to the write-capable JetStream handle.
func TestIntegration_OpenCatalogReader_DelegatesOnlyReadCapabilities(t *testing.T) {
	ctx := context.Background()
	tc := natsclient.NewTestClient(t, natsclient.WithKV())
	client := tc.Client

	ownerBucket, err := EnsureCatalogBucket(ctx, client, BucketEntityStates)
	require.NoError(t, err)
	_, err = ownerBucket.Put(ctx, "acme.ops.robotics.gcs.drone.001", []byte("value"))
	require.NoError(t, err)

	reader, err := OpenCatalogReader(ctx, client, BucketEntityStates)
	require.NoError(t, err)

	_, isKeyValue := any(reader).(jetstream.KeyValue)
	assert.False(t, isKeyValue, "reader dynamic type must not expose the full write-capable handle")
	_, canPut := any(reader).(interface {
		Put(context.Context, string, []byte) (uint64, error)
	})
	assert.False(t, canPut, "reader dynamic type must not expose a mutation capability")

	entry, err := reader.Get(ctx, "acme.ops.robotics.gcs.drone.001")
	require.NoError(t, err)
	assert.Equal(t, []byte("value"), entry.Value())

	keys, err := reader.Keys(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{"acme.ops.robotics.gcs.drone.001"}, keys)

	lister, err := reader.ListKeys(ctx)
	require.NoError(t, err)
	require.NoError(t, lister.Stop())

	filtered, err := reader.ListKeysFiltered(ctx, "acme.ops.>")
	require.NoError(t, err)
	require.NoError(t, filtered.Stop())

	watcher, err := reader.Watch(ctx, "acme.ops.>")
	require.NoError(t, err)
	require.NoError(t, watcher.Stop())

	watcher, err = reader.WatchAll(ctx)
	require.NoError(t, err)
	require.NoError(t, watcher.Stop())

	status, err := reader.Status(ctx)
	require.NoError(t, err)
	assert.Equal(t, BucketEntityStates, status.Bucket())
}
