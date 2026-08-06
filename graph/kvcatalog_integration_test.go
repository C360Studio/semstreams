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

// TestIntegration_OpenCatalogBucket_AbsentNamesTheCatalogOwner: the reader
// seam's not-ready error carries the catalog Owner so an operator reads "wait
// for / deploy graph-ingest", and the bucket stays absent.
func TestIntegration_OpenCatalogBucket_AbsentNamesTheCatalogOwner(t *testing.T) {
	ctx := context.Background()
	tc := natsclient.NewTestClient(t, natsclient.WithKV())
	client := tc.Client

	_, err := OpenCatalogBucket(ctx, client, BucketEntityStates)
	require.Error(t, err)

	var classified *errs.ClassifiedError
	require.True(t, errors.As(err, &classified))
	assert.Equal(t, ErrorCodeIndexNotReady, classified.Code,
		"the not-ready code must be the one classified consumers already handle")
	assert.Contains(t, err.Error(), "graph-ingest",
		"the error must name the catalog owner of ENTITY_STATES")

	_, gerr := client.GetKeyValueBucket(ctx, BucketEntityStates)
	assert.ErrorIs(t, gerr, jetstream.ErrBucketNotFound,
		"the reader seam must never create the bucket")
}
