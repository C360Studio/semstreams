package graphembedding

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/storage/objectstore"
)

// TestCreateContentStore_NoStoreReadPort_GracefulNil drives the REAL
// createContentStore optional/graceful path (FINDING-1): the content store is
// legitimately OPTIONAL, so a config with no store-read port resolves to (nil, nil)
// — no store, no error — and Start proceeds. This is the degradation the fatal
// branch must NOT disturb (BM25 / no-store tiers must still boot). No NATS is
// touched because the bucket-name scan returns before any constructor call.
func TestCreateContentStore_NoStoreReadPort_GracefulNil(t *testing.T) {
	c := &Component{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		config: Config{
			Ports: &component.PortConfig{
				Inputs: []component.PortDefinition{
					{Name: "entity_watch", Type: "kv-watch", Subject: "ENTITY_STATES"},
				},
			},
		},
	}

	store, err := c.createContentStore(context.Background())
	require.NoError(t, err, "a no-store config must degrade gracefully, not error")
	assert.Nil(t, store, "no store-read port ⇒ no content store wired")
}

// TestContentStoreOutcome_FailsClosedOnFatal drives FINDING-1 through the PRODUCTION
// decision seam contentStoreOutcome — the function createContentStore calls to map a
// constructor result to its return. A fatal D2 retention violation (#600/#616) must
// fail Start CLOSED (nil store, fatal error); the content store is otherwise OPTIONAL,
// so a non-fatal error resolves to a disabled store (nil, nil) and the component boots.
// Invoking the real seam (not a reconstructed wrap chain) makes a mutation that folds
// the fatal into the graceful nil path turn this red.
//
// The fatal input is the real D1 classifier (natsclient.CheckNoLifecycleRetention)
// wrapped fatal exactly as reconcileNoLifecycleRetention emits it (retention.go:119-121)
// — the objectstore seam test drives that guard end-to-end via a denied fake JS;
// createContentStore receives this shape from the shared store constructor.
func TestContentStoreOutcome_FailsClosedOnFatal(t *testing.T) {
	const bucket = "GE_CONTENT"
	guardFatal := errs.WrapFatal(
		natsclient.CheckNoLifecycleRetention(bucket, 24*time.Hour, -1),
		"Store", "reconcileNoLifecycleRetention",
		"content store retains lifecycle eviction after reconcile")
	require.True(t, errs.IsFatal(guardFatal), "precondition: the guard error is fatal")

	t.Run("a fatal retention error fails closed", func(t *testing.T) {
		store, err := contentStoreOutcome(nil, guardFatal, bucket)
		assert.Nil(t, store, "a fatal must NOT resolve to a wired store")
		require.Error(t, err, "a fatal must NOT resolve to the graceful (nil, nil) disable path")
		assert.True(t, errs.IsFatal(err), "the retention violation stays fatal out of the seam")
		assert.ErrorIs(t, err, natsclient.ErrGraphBucketRetention, "the sentinel survives")
	})

	t.Run("a non-fatal unavailable error degrades to a disabled store", func(t *testing.T) {
		transient := errs.WrapTransient(errors.New("nats unavailable"),
			"Store", "NewStoreWithConfigAndMetrics", "get JetStream context")
		store, err := contentStoreOutcome(nil, transient, bucket)
		assert.Nil(t, store)
		assert.NoError(t, err,
			"the content store is optional — a non-fatal error disables it, does not fail Start")
	})

	t.Run("no constructor error wires the store", func(t *testing.T) {
		s := &objectstore.Store{}
		store, err := contentStoreOutcome(s, nil, bucket)
		assert.NoError(t, err)
		assert.Same(t, s, store, "a clean construction returns the store unchanged")
	})
}
