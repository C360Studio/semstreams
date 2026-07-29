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
// #610/#611 shape end-to-end through the real backstop: a derived owned bucket
// (EMBEDDING_INDEX) is pre-created with a foreign 7-day TTL and a stored key —
// the owner-absent-from-this-composition class the backstop exists for — then
// AssertOwnedBucketsClean strips the TTL in place, WARNs naming the bucket,
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
	assert.Equal(t, time.Duration(0), maxAge, "the backstop must strip the foreign TTL")
	assert.LessOrEqual(t, maxBytes, int64(0), "the backstop must leave MaxBytes non-binding")

	// A WARN was logged naming the bucket.
	assert.True(t, rec.warnMentioning(BucketEmbeddingIndex),
		"the backstop must WARN naming the stripped bucket %s", BucketEmbeddingIndex)

	// No stored key was deleted by the reconciliation.
	entry, err := fresh.Get(ctx, "entity.key.one")
	require.NoError(t, err, "the stored key must survive the strip")
	assert.Equal(t, []byte("survivor"), entry.Value())
}

// TestIntegration_AssertOwnedBucketsClean_IgnoresOrphanedEmbeddingsCache pins
// the adopter-facing consequence of the EMBEDDINGS_CACHE deletion
// (reopen-framework-owned-bucket-guards): an orphaned KV_EMBEDDINGS_CACHE
// bucket left behind by a pre-deletion deployment is inert — no longer in the
// owned set, so the backstop neither strips nor reports it, and operators may
// delete it manually at leisure.
func TestIntegration_AssertOwnedBucketsClean_IgnoresOrphanedEmbeddingsCache(t *testing.T) {
	const orphanedCache = "EMBEDDINGS_CACHE"
	ctx := context.Background()
	testClient := natsclient.NewTestClient(t, natsclient.WithKV())
	client := testClient.Client

	_, err := client.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{
		Bucket: orphanedCache,
		TTL:    24 * time.Hour,
	})
	require.NoError(t, err)

	rec := &recordingHandler{}
	require.NoError(t, AssertOwnedBucketsClean(ctx, client, slog.New(rec)))

	// The orphan is untouched and unreported — it is outside the owned set.
	fresh, err := client.GetKeyValueBucket(ctx, orphanedCache)
	require.NoError(t, err)
	maxAge, _, err := natsclient.BucketRetention(ctx, fresh)
	require.NoError(t, err)
	assert.Equal(t, 24*time.Hour, maxAge, "an orphaned EMBEDDINGS_CACHE bucket must be left untouched")
	assert.False(t, rec.warnMentioning(orphanedCache),
		"the orphaned cache must not be reported by the backstop")
	assert.False(t, IsFrameworkOwnedBucket(orphanedCache),
		"EMBEDDINGS_CACHE must no longer be framework-owned (surface deleted)")
}

// TestIntegration_AssertOwnedBucketsClean_SkipsAbsentBuckets proves the backstop is
// resourceless-deploy safe: with NO framework buckets provisioned, the sweep is
// a clean no-op (skip-if-absent) and never creates a bucket — so a tier-gated
// deploy that omits, e.g., the embedding/community indexes is not forced to
// provision them.
func TestIntegration_AssertOwnedBucketsClean_SkipsAbsentBuckets(t *testing.T) {
	ctx := context.Background()
	testClient := natsclient.NewTestClient(t, natsclient.WithKV())
	client := testClient.Client

	require.NoError(t, AssertOwnedBucketsClean(ctx, client, slog.New(&recordingHandler{})))

	// The backstop must NOT have created any guarded bucket.
	for _, bucket := range FrameworkOwnedBuckets() {
		_, err := client.GetKeyValueBucket(ctx, bucket)
		assert.ErrorIs(t, err, jetstream.ErrBucketNotFound,
			"backstop must not create absent bucket %s", bucket)
	}
}

// TestIntegration_AssertOwnedBucketsClean_StripsGraphStatusTTL_PreservesHistory
// is the F3 coverage test: GRAPH_STATUS is a framework-owned no-lifecycle
// catalog bucket the retention backstop covers. A foreign TTL on it is stripped, but its History
// (MaxMsgsPerSubject) — the readiness replay depth the producer sets to 3 — MUST
// survive, because the reconcile strips ONLY MaxAge/MaxBytes. Clobbering History
// would silently shorten readiness replay.
func TestIntegration_AssertOwnedBucketsClean_StripsGraphStatusTTL_PreservesHistory(t *testing.T) {
	ctx := context.Background()
	testClient := natsclient.NewTestClient(t, natsclient.WithKV())
	client := testClient.Client

	// GRAPH_STATUS as the readiness producer creates it (History=3, per
	// readiness.BucketHistory) but ALSO carrying a foreign TTL (the dirty shape).
	const graphStatusHistory = 3
	dirty, err := client.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{
		Bucket:  BucketGraphStatus,
		History: graphStatusHistory,
		TTL:     7 * 24 * time.Hour,
	})
	require.NoError(t, err)
	_, err = dirty.Put(ctx, "graph-index", []byte("ready-envelope"))
	require.NoError(t, err)

	rec := &recordingHandler{}
	require.NoError(t, AssertOwnedBucketsClean(ctx, client, slog.New(rec)))

	fresh, err := client.GetKeyValueBucket(ctx, BucketGraphStatus)
	require.NoError(t, err)
	maxAge, maxBytes, err := natsclient.BucketRetention(ctx, fresh)
	require.NoError(t, err)
	assert.Equal(t, time.Duration(0), maxAge, "the backstop must strip GRAPH_STATUS's foreign TTL")
	assert.LessOrEqual(t, maxBytes, int64(0), "the backstop must leave MaxBytes non-binding")

	// CRITICAL: History (MaxMsgsPerSubject) is UNTOUCHED — the reconcile mutates
	// only MaxAge/MaxBytes; clobbering History would break readiness replay.
	status, err := fresh.Status(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(graphStatusHistory), status.History(),
		"the backstop must NOT clobber GRAPH_STATUS History (readiness replay depth)")

	assert.True(t, rec.warnMentioning(BucketGraphStatus),
		"the backstop must WARN naming the stripped bucket %s", BucketGraphStatus)
	entry, err := fresh.Get(ctx, "graph-index")
	require.NoError(t, err, "the stored readiness envelope must survive the strip")
	assert.Equal(t, []byte("ready-envelope"), entry.Value())
}

// TestIntegration_OrderedCreateRace_SeamReconcilesAtAcquisition proves the
// create-race class the retired post-start sweep pass existed for is closed by
// the acquisition SEAM, earlier and more precisely: the pre-start backstop
// SKIPS the absent guarded bucket (no create), a competing process then wins
// the create with a foreign TTL, and the owner's seam acquisition reconciles
// the adopted-dirty bucket AT ACQUISITION — TTL stripped in place, WARN naming
// the bucket, stored key preserved — with no second boot pass involved.
func TestIntegration_OrderedCreateRace_SeamReconcilesAtAcquisition(t *testing.T) {
	ctx := context.Background()
	testClient := natsclient.NewTestClient(t, natsclient.WithKV())
	client := testClient.Client

	// The pre-start backstop: the guarded bucket is ABSENT — skipped, not created.
	require.NoError(t, AssertOwnedBucketsClean(ctx, client, slog.New(&recordingHandler{})))
	_, err := client.GetKeyValueBucket(ctx, BucketCommunityIndex)
	require.ErrorIs(t, err, jetstream.ErrBucketNotFound, "the backstop must not create the absent bucket")

	// A competing process wins the create with a foreign TTL (the create-race
	// the pre-start backstop cannot see), and stores a key.
	rival, err := client.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{
		Bucket: BucketCommunityIndex,
		TTL:    7 * 24 * time.Hour,
	})
	require.NoError(t, err)
	_, err = rival.Put(ctx, "community.key", []byte("member"))
	require.NoError(t, err)

	// The OWNER acquires through the seam (as graph-clustering's Start does):
	// the dirty adopt is reconciled right there — no sweep pass runs after.
	// (The seam's WARN-naming-the-bucket behavior is pinned at the seam's own
	// integration tests in natsclient, where the client logger is injectable.)
	adopted, err := EnsureCatalogBucket(ctx, client, BucketCommunityIndex)
	require.NoError(t, err)

	maxAge, _, err := natsclient.BucketRetention(ctx, adopted)
	require.NoError(t, err)
	assert.Equal(t, time.Duration(0), maxAge,
		"the owner's seam acquisition must strip the create-race TTL — no post-start pass exists to catch it later")
	entry, err := adopted.Get(ctx, "community.key")
	require.NoError(t, err, "the stored key must survive the strip")
	assert.Equal(t, []byte("member"), entry.Value())
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

// TestIntegration_AssertOwnedBucketsClean_PreservesBoundedTTL pins the
// backstop's bounded-ttl exclusion: OWNER_PRESENCE's declared TTL IS the
// liveness contract (dead owners must expire, ADR-056), and the backstop
// ranges only the catalog's no-lifecycle descriptors. The historically obvious
// "simplification" — iterating FrameworkOwnedBuckets(), exactly what this
// function did before the catalog — would silently strip the TTL now that the
// derived owned set contains OWNER_PRESENCE: presence keys stop expiring, dead
// owners appear alive forever, with zero error or WARN. This test is the only
// thing that makes that regression loud.
func TestIntegration_AssertOwnedBucketsClean_PreservesBoundedTTL(t *testing.T) {
	ctx := context.Background()
	testClient := natsclient.NewTestClient(t, natsclient.WithKV())
	client := testClient.Client

	spec, ok := SpecFor(BucketOwnerPresence)
	require.True(t, ok, "OWNER_PRESENCE must be a catalog descriptor")
	require.Equal(t, natsclient.RetentionBoundedTTL, spec.Retention.Kind)
	bucket, err := natsclient.EnsureFrameworkBucket(ctx, client, spec)
	require.NoError(t, err)
	_ = bucket

	rec := &recordingHandler{}
	require.NoError(t, AssertOwnedBucketsClean(ctx, client, slog.New(rec)))

	fresh, err := client.GetKeyValueBucket(ctx, BucketOwnerPresence)
	require.NoError(t, err)
	maxAge, _, err := natsclient.BucketRetention(ctx, fresh)
	require.NoError(t, err)
	assert.Equal(t, spec.Retention.TTL, maxAge,
		"the backstop must NOT strip a bounded-ttl descriptor's declared TTL — it is the liveness contract")
	assert.False(t, rec.warnMentioning(BucketOwnerPresence),
		"the backstop must not report the bounded-ttl bucket at all")
}
