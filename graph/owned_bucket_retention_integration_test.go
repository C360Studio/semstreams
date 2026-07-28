//go:build integration

package graph

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordingHandler is a minimal slog.Handler that captures emitted records so a
// test can assert a WARN was logged naming a bucket. Concurrency-safe because
// the sweep is single-goroutine here but slog may format lazily.
type recordingHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *recordingHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *recordingHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r.Clone())
	return nil
}
func (h *recordingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *recordingHandler) WithGroup(string) slog.Handler      { return h }

// warnMentioning reports whether a WARN record was emitted whose message or any
// attribute value contains the bucket name.
func (h *recordingHandler) warnMentioning(bucket string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, r := range h.records {
		if r.Level != slog.LevelWarn {
			continue
		}
		if strings.Contains(r.Message, bucket) {
			return true
		}
		found := false
		r.Attrs(func(a slog.Attr) bool {
			if strings.Contains(a.Value.String(), bucket) {
				found = true
				return false
			}
			return true
		})
		if found {
			return true
		}
	}
	return false
}

// TestIntegration_AssertOwnedBucketsClean_StripsForeignTTL reproduces the
// #610/#611 shape end-to-end through the real sweep: a derived owned bucket
// (EMBEDDING_INDEX) is pre-created with a foreign 7-day TTL and a stored key,
// then AssertOwnedBucketsClean strips the TTL in place, WARNs naming the bucket,
// and loses no key — proving boot self-heals a persisted-dirty bucket rather
// than silently honoring its eviction.
func TestIntegration_AssertOwnedBucketsClean_StripsForeignTTL(t *testing.T) {
	ctx := context.Background()
	testClient := natsclient.NewTestClient(t, natsclient.WithKV())
	client := testClient.Client

	// Pre-create the derived bucket WITH a foreign TTL (the persisted-dirty shape
	// a prior/racing process left behind), and store a key in it.
	dirty, err := client.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{
		Bucket: BucketEmbeddingIndex,
		TTL:    7 * 24 * time.Hour,
	})
	require.NoError(t, err)
	_, err = dirty.Put(ctx, "entity.key.one", []byte("survivor"))
	require.NoError(t, err)

	// Precondition: the TTL is really set.
	maxAge, _, err := natsclient.BucketRetention(ctx, dirty)
	require.NoError(t, err)
	require.Equal(t, 7*24*time.Hour, maxAge, "precondition: bucket must carry the foreign TTL")

	rec := &recordingHandler{}
	logger := slog.New(rec)

	// Run the authoritative boot sweep.
	require.NoError(t, AssertOwnedBucketsClean(ctx, client, logger))

	// The TTL is stripped in place.
	fresh, err := client.GetKeyValueBucket(ctx, BucketEmbeddingIndex)
	require.NoError(t, err)
	var maxBytes int64
	maxAge, maxBytes, err = natsclient.BucketRetention(ctx, fresh)
	require.NoError(t, err)
	assert.Equal(t, time.Duration(0), maxAge, "the sweep must strip the foreign TTL")
	assert.LessOrEqual(t, maxBytes, int64(0), "the sweep must leave MaxBytes non-binding")

	// A WARN was logged naming the bucket.
	assert.True(t, rec.warnMentioning(BucketEmbeddingIndex),
		"the sweep must WARN naming the stripped bucket %s", BucketEmbeddingIndex)

	// No stored key was deleted by the reconciliation.
	entry, err := fresh.Get(ctx, "entity.key.one")
	require.NoError(t, err, "the stored key must survive the strip")
	assert.Equal(t, []byte("survivor"), entry.Value())
}

// TestIntegration_AssertOwnedBucketsClean_ExcludesEmbeddingsCache proves the
// rebuildable cache is excluded from the retention sweep: EMBEDDINGS_CACHE with a
// TTL is NOT stripped by AssertOwnedBucketsClean (its capacity policy is owned by
// bounded-storage-operability), while it remains write-ownership-protected.
func TestIntegration_AssertOwnedBucketsClean_ExcludesEmbeddingsCache(t *testing.T) {
	ctx := context.Background()
	testClient := natsclient.NewTestClient(t, natsclient.WithKV())
	client := testClient.Client

	_, err := client.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{
		Bucket: BucketEmbeddingsCache,
		TTL:    24 * time.Hour,
	})
	require.NoError(t, err)

	rec := &recordingHandler{}
	require.NoError(t, AssertOwnedBucketsClean(ctx, client, slog.New(rec)))

	// The cache TTL is UNTOUCHED — it is excluded from the retention guard.
	fresh, err := client.GetKeyValueBucket(ctx, BucketEmbeddingsCache)
	require.NoError(t, err)
	maxAge, _, err := natsclient.BucketRetention(ctx, fresh)
	require.NoError(t, err)
	assert.Equal(t, 24*time.Hour, maxAge, "EMBEDDINGS_CACHE must NOT be stripped by the retention sweep")
	assert.False(t, rec.warnMentioning(BucketEmbeddingsCache),
		"the excluded cache must not be reported as stripped")

	// Still write-ownership-protected.
	assert.True(t, IsFrameworkOwnedBucket(BucketEmbeddingsCache),
		"EMBEDDINGS_CACHE must remain framework-owned (write-protected)")
}

// TestIntegration_AssertOwnedBucketsClean_SkipsAbsentBuckets proves the sweep is
// resourceless-deploy safe: with NO framework buckets provisioned, the sweep is
// a clean no-op (skip-if-absent) and never creates a bucket — so a tier-gated
// deploy that omits, e.g., the embedding/community indexes is not forced to
// provision them.
func TestIntegration_AssertOwnedBucketsClean_SkipsAbsentBuckets(t *testing.T) {
	ctx := context.Background()
	testClient := natsclient.NewTestClient(t, natsclient.WithKV())
	client := testClient.Client

	require.NoError(t, AssertOwnedBucketsClean(ctx, client, slog.New(&recordingHandler{})))

	// The sweep must NOT have created any guarded bucket.
	for _, bucket := range retentionGuardedBuckets() {
		_, err := client.GetKeyValueBucket(ctx, bucket)
		assert.ErrorIs(t, err, jetstream.ErrBucketNotFound,
			"sweep must not create absent bucket %s", bucket)
	}
}

// NOTE on the "unstrippable retention fails boot fast with the bucket named"
// scenario (tasks 4.3): a genuinely un-strippable retention state (a DENIED
// UpdateStream leaving the config binding) is NOT deterministically reachable
// against cooperative real NATS — the strip always takes. That fatal path is
// therefore proven at the atom's unit level
// (natsclient.TestReconcileNoLifecycleRetention_KV / "a denied strip fails closed
// naming the bucket"), which drives the exact function AssertOwnedBucketsClean
// calls. This mirrors the ObjectStore precedent's identical, documented decision
// (storage/objectstore/retention_test.go).
