//go:build integration

package natsclient

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// kvspecRecordingHandler captures slog records so a test can assert a WARN was
// emitted naming a bucket and both History values.
type kvspecRecordingHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *kvspecRecordingHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *kvspecRecordingHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r.Clone())
	return nil
}
func (h *kvspecRecordingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *kvspecRecordingHandler) WithGroup(string) slog.Handler      { return h }

// warnContainingAll reports whether a WARN record exists whose message or
// attributes contain every one of the given substrings.
func (h *kvspecRecordingHandler) warnContainingAll(subs ...string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, r := range h.records {
		if r.Level != slog.LevelWarn {
			continue
		}
		joined := r.Message
		r.Attrs(func(a slog.Attr) bool {
			joined += " " + a.Key + "=" + a.Value.String()
			return true
		})
		all := true
		for _, s := range subs {
			if !strings.Contains(joined, s) {
				all = false
				break
			}
		}
		if all {
			return true
		}
	}
	return false
}

func fixtureSpec(name string) BucketSpec {
	return BucketSpec{
		Name:      name,
		Owner:     "kvspec-fixture-owner",
		Class:     ClassDerived,
		Retention: RetentionPolicy{Kind: RetentionNoLifecycle},
		Write:     WriteOwnerOnly,
		Posture:   PostureOwnerCreates,
		History:   1,
		Replicas:  1,
	}
}

// TestIntegration_EnsureFrameworkBucket_ReconcilesAdoptedHistory is the F1
// mechanism test at the seam: a bucket created earlier by another path with a
// divergent History is adopted by the owner's Ensure and reconciled to the
// declared value, WARNing with both values — so bucket configuration is no
// longer decided by boot order.
func TestIntegration_EnsureFrameworkBucket_ReconcilesAdoptedHistory(t *testing.T) {
	ctx := context.Background()
	tc := NewTestClient(t, WithKV())
	rec := &kvspecRecordingHandler{}
	tc.Client.logger = slog.New(rec)

	// A rival path creates the bucket with History 3 and stores a key.
	rival, err := tc.Client.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{
		Bucket:  "KVSPEC_HISTORY_RACE",
		History: 3,
	})
	require.NoError(t, err)
	_, err = rival.Put(ctx, "entity.one", []byte("survivor"))
	require.NoError(t, err)

	// The owner acquires through the seam declaring History 1.
	bucket, err := EnsureFrameworkBucket(ctx, tc.Client, fixtureSpec("KVSPEC_HISTORY_RACE"))
	require.NoError(t, err)

	status, err := bucket.Status(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(1), status.History(),
		"the seam must reconcile the adopted History to the catalog declaration")

	assert.True(t, rec.warnContainingAll("KVSPEC_HISTORY_RACE", "3", "1"),
		"the History reconcile must WARN naming the bucket and both values")

	entry, err := bucket.Get(ctx, "entity.one")
	require.NoError(t, err, "the stored key's current revision must survive the History reconcile")
	assert.Equal(t, []byte("survivor"), entry.Value())
}

// TestIntegration_EnsureFrameworkBucket_StripsForeignTTLAtAcquisition proves
// the seam holds the retention guarantee AT acquisition, with no sweep
// involved: an adopted bucket carrying a foreign TTL is stripped in place
// (WARN naming the bucket) before the owner proceeds, and no stored key is
// deleted.
func TestIntegration_EnsureFrameworkBucket_StripsForeignTTLAtAcquisition(t *testing.T) {
	ctx := context.Background()
	tc := NewTestClient(t, WithKV())
	rec := &kvspecRecordingHandler{}
	tc.Client.logger = slog.New(rec)

	dirty, err := tc.Client.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{
		Bucket: "KVSPEC_DIRTY_ADOPT",
		TTL:    7 * 24 * time.Hour,
	})
	require.NoError(t, err)
	_, err = dirty.Put(ctx, "entity.one", []byte("survivor"))
	require.NoError(t, err)

	bucket, err := EnsureFrameworkBucket(ctx, tc.Client, fixtureSpec("KVSPEC_DIRTY_ADOPT"))
	require.NoError(t, err)

	maxAge, maxBytes, err := BucketRetention(ctx, bucket)
	require.NoError(t, err)
	assert.Equal(t, time.Duration(0), maxAge, "the seam must strip the foreign TTL at acquisition")
	assert.LessOrEqual(t, maxBytes, int64(0))
	assert.True(t, rec.warnContainingAll("KVSPEC_DIRTY_ADOPT"),
		"the strip must WARN naming the bucket")

	entry, err := bucket.Get(ctx, "entity.one")
	require.NoError(t, err, "no stored key may be deleted by the reconciliation")
	assert.Equal(t, []byte("survivor"), entry.Value())
}

// TestIntegration_EnsureFrameworkBucket_BoundedTTLConvergesToDeclared is the
// bounded-ttl arm: the declared TTL is the contract. A clean create carries
// it; an adopted bucket with a divergent MaxAge is converged TO it — the same
// seam that strips a no-lifecycle bucket's TTL preserves this one.
func TestIntegration_EnsureFrameworkBucket_BoundedTTLConvergesToDeclared(t *testing.T) {
	ctx := context.Background()
	tc := NewTestClient(t, WithKV())

	spec := fixtureSpec("KVSPEC_BOUNDED_TTL")
	spec.Retention = RetentionPolicy{Kind: RetentionBoundedTTL, TTL: 120 * time.Second}

	// Clean create: the declared TTL is applied.
	bucket, err := EnsureFrameworkBucket(ctx, tc.Client, spec)
	require.NoError(t, err)
	maxAge, _, err := BucketRetention(ctx, bucket)
	require.NoError(t, err)
	require.Equal(t, 120*time.Second, maxAge, "a bounded-ttl create must carry the declared TTL")

	// Re-acquisition preserves it (idempotent, no strip).
	bucket, err = EnsureFrameworkBucket(ctx, tc.Client, spec)
	require.NoError(t, err)
	maxAge, _, err = BucketRetention(ctx, bucket)
	require.NoError(t, err)
	assert.Equal(t, 120*time.Second, maxAge,
		"re-acquisition must PRESERVE the declared TTL, never strip it")

	// A divergent out-of-band MaxAge is converged back to the declaration.
	js, err := tc.Client.JetStream()
	require.NoError(t, err)
	stream, err := js.Stream(ctx, "KV_KVSPEC_BOUNDED_TTL")
	require.NoError(t, err)
	info, err := stream.Info(ctx)
	require.NoError(t, err)
	cfg := info.Config
	cfg.MaxAge = time.Hour
	_, err = js.UpdateStream(ctx, cfg)
	require.NoError(t, err)

	bucket, err = EnsureFrameworkBucket(ctx, tc.Client, spec)
	require.NoError(t, err)
	maxAge, _, err = BucketRetention(ctx, bucket)
	require.NoError(t, err)
	assert.Equal(t, 120*time.Second, maxAge,
		"a divergent MaxAge must be converged TO the declared bounded TTL")
}

// TestIntegration_OpenFrameworkBucket_AbsentIsNotReadyAndNeverCreates is the
// #714-closing seam assertion: Open on an absent bucket returns a classified
// not-ready error naming the catalog Owner, and the bucket is STILL ABSENT
// afterwards — a reader can never become an emitter of divergent
// configuration.
func TestIntegration_OpenFrameworkBucket_AbsentIsNotReadyAndNeverCreates(t *testing.T) {
	ctx := context.Background()
	tc := NewTestClient(t, WithKV())

	spec := fixtureSpec("KVSPEC_NEVER_CREATED")
	_, err := OpenFrameworkBucket(ctx, tc.Client, spec)
	require.Error(t, err, "opening an absent must-exist bucket must fail")

	var classified *errs.ClassifiedError
	require.True(t, errors.As(err, &classified), "the error must be classified")
	assert.Equal(t, ErrorCodeBucketNotReady, classified.Code, "the error must carry the not-ready code")
	assert.Equal(t, errs.ErrorTransient, classified.Class, "not-ready is transient: the owner may still boot")
	assert.Contains(t, err.Error(), "kvspec-fixture-owner",
		"the error must name the catalog Owner so the operator knows who provisions it")

	// The #714 closure: the bucket must STILL be absent (Open never creates).
	_, gerr := tc.Client.GetKeyValueBucket(ctx, "KVSPEC_NEVER_CREATED")
	assert.ErrorIs(t, gerr, jetstream.ErrBucketNotFound,
		"OpenFrameworkBucket must NEVER create the bucket")

	// And once the owner provisions it, Open binds.
	_, err = EnsureFrameworkBucket(ctx, tc.Client, spec)
	require.NoError(t, err)
	bucket, err := OpenFrameworkBucket(ctx, tc.Client, spec)
	require.NoError(t, err)
	require.NotNil(t, bucket)
}

// TestIntegration_EnsureFrameworkBucket_CleanCreateIsQuiet: a clean create
// against an empty NATS carries the declared config and needs no reconcile.
func TestIntegration_EnsureFrameworkBucket_CleanCreateIsQuiet(t *testing.T) {
	ctx := context.Background()
	tc := NewTestClient(t, WithKV())
	rec := &kvspecRecordingHandler{}
	tc.Client.logger = slog.New(rec)

	spec := fixtureSpec("KVSPEC_CLEAN_CREATE")
	spec.History = 3
	spec.Description = "kvspec clean create fixture"

	bucket, err := EnsureFrameworkBucket(ctx, tc.Client, spec)
	require.NoError(t, err)

	status, err := bucket.Status(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(3), status.History(), "a clean create must carry the declared History")
	maxAge, maxBytes, err := BucketRetention(ctx, bucket)
	require.NoError(t, err)
	assert.Equal(t, time.Duration(0), maxAge)
	assert.LessOrEqual(t, maxBytes, int64(0))
	assert.False(t, rec.warnContainingAll("KVSPEC_CLEAN_CREATE"),
		"a clean create must not WARN — nothing was reconciled")
}
