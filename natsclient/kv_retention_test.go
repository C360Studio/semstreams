package natsclient

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCheckNoLifecycleRetention covers the pure D1 guardrail (ADR-068): any TTL
// (MaxAge) or binding MaxBytes on a live graph bucket is forbidden; the NATS
// default MaxBytes of -1 (unlimited) and a zero TTL are the clean case.
func TestCheckNoLifecycleRetention(t *testing.T) {
	tests := []struct {
		name     string
		maxAge   time.Duration
		maxBytes int64
		wantErr  bool
	}{
		{"clean (zero TTL, unlimited bytes)", 0, -1, false},
		{"clean (zero TTL, zero bytes)", 0, 0, false},
		{"ttl set is a violation", 24 * time.Hour, -1, true},
		{"maxbytes cap is a violation", 0, 1 << 20, true},
		{"both set", time.Hour, 100, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := CheckNoLifecycleRetention("ENTITY_STATES", tt.maxAge, tt.maxBytes)
			switch {
			case tt.wantErr && err == nil:
				t.Fatal("want ErrGraphBucketRetention, got nil")
			case tt.wantErr && !errors.Is(err, ErrGraphBucketRetention):
				t.Errorf("want ErrGraphBucketRetention, got %v", err)
			case !tt.wantErr && err != nil:
				t.Errorf("want nil, got %v", err)
			}
		})
	}
}

// fakeKVStream is a minimal jetstream.Stream. Only Info is exercised by the KV
// reconcile guard; the embedded nil interface panics on any other method, which
// signals a test touching an unmodeled path.
type fakeKVStream struct {
	jetstream.Stream
	info jetstream.StreamInfo
}

func (f *fakeKVStream) Info(context.Context, ...jetstream.StreamInfoOpt) (*jetstream.StreamInfo, error) {
	// Return a copy so a caller mutating Config for UpdateStream does not alias ours.
	cp := f.info
	return &cp, nil
}

// fakeKVJS is a minimal jetstream.JetStream exercising only Stream + UpdateStream
// — the two calls ReconcileNoLifecycleRetention makes. UpdateStream, unless
// denied, writes the new config back so the guard's fresh re-read (the
// fail-closed assert) observes the strip, exactly as real NATS would.
type fakeKVJS struct {
	jetstream.JetStream
	stream      *fakeKVStream
	updated     *jetstream.StreamConfig // last config passed to UpdateStream
	updateErr   error                   // when set, UpdateStream is denied and config is left unchanged
	noopSuccess bool                    // when set, UpdateStream REPORTS success but does not persist the strip (a lying/no-op update)
	streamErr   error                   // when set, Stream() fails
}

func (f *fakeKVJS) Stream(context.Context, string) (jetstream.Stream, error) {
	if f.streamErr != nil {
		return nil, f.streamErr
	}
	return f.stream, nil
}

func (f *fakeKVJS) UpdateStream(_ context.Context, cfg jetstream.StreamConfig) (jetstream.Stream, error) {
	cp := cfg
	f.updated = &cp
	if f.updateErr != nil {
		return nil, f.updateErr
	}
	if f.noopSuccess {
		// Report success but DO NOT persist the strip — the stream's config stays
		// binding, so the fresh re-read still sees the retention. Models a NATS
		// that accepts the update yet leaves the value in place.
		return f.stream, nil
	}
	f.stream.info.Config = cfg // reflect the strip so the re-read assert sees it
	return f.stream, nil
}

func newFakeKVJS(bucket string, maxAge time.Duration, maxBytes int64) *fakeKVJS {
	return &fakeKVJS{
		stream: &fakeKVStream{
			info: jetstream.StreamInfo{
				Config: jetstream.StreamConfig{
					Name:     KVStreamPrefix + bucket,
					MaxAge:   maxAge,
					MaxBytes: maxBytes,
				},
			},
		},
	}
}

// TestReconcileNoLifecycleRetention_KV drives every branch of the KV
// reconcile-then-assert atom against a fake KV backing stream (KV_<bucket>): a
// clean bucket boots untouched; a legacy MaxAge / a size cap is stripped in
// place then boots; and a strip that is DENIED (UpdateStream fails, config stays
// binding) fails closed with the ErrGraphBucketRetention sentinel. Symmetric
// with storage/objectstore/retention_test.go so KV and ObjectStore can never
// diverge on what "binding" means.
func TestReconcileNoLifecycleRetention_KV(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	t.Run("clean bucket boots without an update", func(t *testing.T) {
		t.Parallel()
		js := newFakeKVJS("EMBEDDING_INDEX", 0, -1)
		require.NoError(t, ReconcileNoLifecycleRetention(context.Background(), js, "EMBEDDING_INDEX", logger))
		assert.Nil(t, js.updated, "a clean bucket must not be updated")
	})

	t.Run("clean bucket (zero MaxBytes) boots without an update", func(t *testing.T) {
		t.Parallel()
		js := newFakeKVJS("EMBEDDING_INDEX", 0, 0)
		require.NoError(t, ReconcileNoLifecycleRetention(context.Background(), js, "EMBEDDING_INDEX", logger))
		assert.Nil(t, js.updated, "a clean bucket must not be updated")
	})

	t.Run("foreign MaxAge is stripped then boots", func(t *testing.T) {
		t.Parallel()
		js := newFakeKVJS("COMMUNITY_INDEX", 7*24*time.Hour, -1)
		require.NoError(t, ReconcileNoLifecycleRetention(context.Background(), js, "COMMUNITY_INDEX", logger))
		require.NotNil(t, js.updated, "a binding MaxAge must trigger an UpdateStream")
		assert.Equal(t, time.Duration(0), js.updated.MaxAge, "the update must clear MaxAge")
	})

	t.Run("binding MaxBytes is stripped then boots", func(t *testing.T) {
		t.Parallel()
		js := newFakeKVJS("COMMUNITY_INDEX", 0, 1<<20)
		require.NoError(t, ReconcileNoLifecycleRetention(context.Background(), js, "COMMUNITY_INDEX", logger))
		require.NotNil(t, js.updated)
		assert.Equal(t, int64(-1), js.updated.MaxBytes, "the update must clear the size cap")
	})

	t.Run("a denied strip fails closed naming the bucket", func(t *testing.T) {
		t.Parallel()
		js := newFakeKVJS("SPATIAL_INDEX", 24*time.Hour, -1)
		js.updateErr = errors.New("update denied") // strip cannot take; config stays binding

		err := ReconcileNoLifecycleRetention(context.Background(), js, "SPATIAL_INDEX", logger)
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrGraphBucketRetention, "an un-strippable retention config must fail boot closed")
		assert.Contains(t, err.Error(), "SPATIAL_INDEX", "the fatal error must name the offending bucket")
	})

	t.Run("a no-op UpdateStream that leaves retention binding fails closed", func(t *testing.T) {
		t.Parallel()
		// UpdateStream REPORTS success but does not persist the strip, so the fresh
		// re-read still observes the binding TTL. The fail-closed path must trip on
		// the authoritative re-read, not on the update's reported success — this
		// covers the "cannot strip" spec scenario deterministically without relying
		// on a denied update.
		js := newFakeKVJS("COMMUNITY_INDEX", 24*time.Hour, -1)
		js.noopSuccess = true

		err := ReconcileNoLifecycleRetention(context.Background(), js, "COMMUNITY_INDEX", logger)
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrGraphBucketRetention, "a lying no-op update must still fail boot closed on the re-read")
		assert.Contains(t, err.Error(), "COMMUNITY_INDEX", "the fatal error must name the offending bucket")
		require.NotNil(t, js.updated, "the reconcile must have ATTEMPTED an UpdateStream")
	})
}
