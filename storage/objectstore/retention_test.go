package objectstore

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeStream is a minimal jetstream.Stream. Only Info is exercised by the guard;
// the embedded interface satisfies the rest (nil — a panic if any is called, which
// signals a test touching an unmodeled path).
type fakeStream struct {
	jetstream.Stream
	info jetstream.StreamInfo
}

func (f *fakeStream) Info(context.Context, ...jetstream.StreamInfoOpt) (*jetstream.StreamInfo, error) {
	// Return a copy so callers mutating Config for UpdateStream do not alias ours.
	cp := f.info
	return &cp, nil
}

// fakeJS is a minimal jetstream.JetStream exercising only Stream + UpdateStream —
// the two calls reconcileNoLifecycleRetention makes. UpdateStream, unless denied,
// writes the new config back onto the backing stream so the guard's fresh re-read
// (the fail-closed assert) observes the strip, exactly as real NATS would.
type fakeJS struct {
	jetstream.JetStream
	stream    *fakeStream
	updated   *jetstream.StreamConfig // last config passed to UpdateStream
	updateErr error                   // when set, UpdateStream is denied and the config is left unchanged
	streamErr error                   // when set, Stream() fails
}

func (f *fakeJS) Stream(context.Context, string) (jetstream.Stream, error) {
	if f.streamErr != nil {
		return nil, f.streamErr
	}
	return f.stream, nil
}

func (f *fakeJS) UpdateStream(_ context.Context, cfg jetstream.StreamConfig) (jetstream.Stream, error) {
	cp := cfg
	f.updated = &cp
	if f.updateErr != nil {
		return nil, f.updateErr
	}
	f.stream.info.Config = cfg // reflect the strip so the re-read assert sees it
	return f.stream, nil
}

func newFakeJS(bucket string, maxAge time.Duration, maxBytes int64) *fakeJS {
	return &fakeJS{
		stream: &fakeStream{
			info: jetstream.StreamInfo{
				Config: jetstream.StreamConfig{
					Name:     objectStoreStreamPrefix + bucket,
					MaxAge:   maxAge,
					MaxBytes: maxBytes,
				},
			},
		},
	}
}

// TestCheckNoLifecycleRetention_ObjectStoreShapedValues pins the pure classifier
// (reused verbatim from natsclient) against the exact value shapes the ObjectStore
// backing stream produces: a clean store carries MaxAge 0 and the NATS "unlimited"
// MaxBytes sentinel -1; a legacy TTL carries a positive MaxAge; a size cap carries
// a positive MaxBytes.
func TestCheckNoLifecycleRetention_ObjectStoreShapedValues(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		maxAge   time.Duration
		maxBytes int64
		binding  bool
	}{
		"clean object store (unlimited sentinel)": {0, -1, false},
		"clean object store (zero max bytes)":     {0, 0, false},
		"legacy 24h TTL":                          {24 * time.Hour, -1, true},
		"size cap":                                {0, 1 << 20, true},
		"both":                                    {time.Hour, 1 << 20, true},
	}
	for name, tc := range tests {
		tc := tc
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			err := natsclient.CheckNoLifecycleRetention("CONTENT", tc.maxAge, tc.maxBytes)
			if tc.binding {
				require.Error(t, err)
				assert.ErrorIs(t, err, natsclient.ErrGraphBucketRetention)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestBackingStreamRetention_ReadsConfig proves the reader returns the backing
// stream's MaxAge/MaxBytes verbatim — the values the guard classifies.
func TestBackingStreamRetention_ReadsConfig(t *testing.T) {
	t.Parallel()
	js := newFakeJS("CONTENT", 24*time.Hour, 1<<20)

	maxAge, maxBytes, err := backingStreamRetention(context.Background(), js, "CONTENT")
	require.NoError(t, err)
	assert.Equal(t, 24*time.Hour, maxAge)
	assert.Equal(t, int64(1<<20), maxBytes)
}

// TestReconcileNoLifecycleRetention drives every branch of the reconcile-then-
// assert guard against a fake backing stream: a clean store boots untouched; a
// legacy MaxAge / a size cap is stripped in place then boots; and a strip that is
// DENIED (UpdateStream fails and the config stays binding) fails closed with the
// ErrGraphBucketRetention sentinel rather than proceeding.
func TestReconcileNoLifecycleRetention(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	t.Run("clean store boots without an update", func(t *testing.T) {
		t.Parallel()
		js := newFakeJS("CONTENT", 0, -1)
		require.NoError(t, reconcileNoLifecycleRetention(context.Background(), js, "CONTENT", logger))
		assert.Nil(t, js.updated, "a clean store must not be updated")
	})

	t.Run("legacy MaxAge is stripped then boots", func(t *testing.T) {
		t.Parallel()
		js := newFakeJS("CONTENT", 24*time.Hour, -1)
		require.NoError(t, reconcileNoLifecycleRetention(context.Background(), js, "CONTENT", logger))
		require.NotNil(t, js.updated, "a binding MaxAge must trigger an UpdateStream")
		assert.Equal(t, time.Duration(0), js.updated.MaxAge, "the update must clear MaxAge")
		assert.Equal(t, int64(-1), js.updated.MaxBytes, "the update must leave MaxBytes unlimited")
	})

	t.Run("binding MaxBytes is stripped then boots", func(t *testing.T) {
		t.Parallel()
		js := newFakeJS("CONTENT", 0, 1<<20)
		require.NoError(t, reconcileNoLifecycleRetention(context.Background(), js, "CONTENT", logger))
		require.NotNil(t, js.updated)
		assert.Equal(t, int64(-1), js.updated.MaxBytes, "the update must clear the size cap")
	})

	t.Run("a denied strip fails closed", func(t *testing.T) {
		t.Parallel()
		js := newFakeJS("CONTENT", 24*time.Hour, -1)
		js.updateErr = errors.New("update denied") // strip cannot take; config stays binding

		err := reconcileNoLifecycleRetention(context.Background(), js, "CONTENT", logger)
		require.Error(t, err)
		assert.ErrorIs(t, err, natsclient.ErrGraphBucketRetention,
			"an un-strippable retention config must fail boot closed")
	})
}

// TestStartStoreError_PreservesClass drives FINDING-3 through the PRODUCTION
// decision seam: Component.Start funnels its store-constructor error through
// startStoreError, which must fail CLOSED for a fatal retention violation and stay
// transient (retryable) for anything else. errs.IsFatal inspects the OUTERMOST
// classification, so the pre-fix unconditional WrapTransient silently downgraded the
// fatal — the guard "failed closed" but the consumer reopened it (#632). This test
// invokes the real seam (not a reconstructed wrap chain) so a mutation restoring the
// unconditional WrapTransient turns it red.
//
// The fatal input is the REAL guard error: reconcileNoLifecycleRetention with a
// denied strip returns the exact WrapFatal(ErrGraphBucketRetention) the store
// constructor hands to Start. A true denied-UpdateStream is not deterministically
// reachable against cooperative real NATS (the strip always takes), so the graceful
// (strippable) path is exercised end-to-end through the real Component.Start in the
// integration tier.
func TestStartStoreError_PreservesClass(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	t.Run("a REAL fatal retention error fails closed and stays fatal", func(t *testing.T) {
		t.Parallel()
		js := newFakeJS("CONTENT", 24*time.Hour, -1)
		js.updateErr = errors.New("update denied") // strip denied → guard fails closed
		ctorErr := reconcileNoLifecycleRetention(context.Background(), js, "CONTENT", logger)
		require.Error(t, ctorErr)
		require.True(t, errs.IsFatal(ctorErr), "precondition: the guard error is fatal")

		got := startStoreError(ctorErr)
		require.Error(t, got, "the seam must not swallow the fatal to nil")
		assert.True(t, errs.IsFatal(got), "Start's seam must keep the retention violation fatal")
		assert.ErrorIs(t, got, natsclient.ErrGraphBucketRetention,
			"the retention sentinel must survive the seam's wrap")
	})

	t.Run("a transient constructor error stays transient (retryable)", func(t *testing.T) {
		t.Parallel()
		transient := errs.WrapTransient(errors.New("nats unavailable"),
			"Store", "NewStoreWithConfigAndMetrics", "get JetStream context")
		require.True(t, errs.IsTransient(transient))

		got := startStoreError(transient)
		assert.False(t, errs.IsFatal(got), "a non-retention error must not be promoted to fatal")
		assert.True(t, errs.IsTransient(got), "a transient constructor error stays retryable")
	})

	t.Run("nil is nil", func(t *testing.T) {
		t.Parallel()
		assert.NoError(t, startStoreError(nil))
	})
}
