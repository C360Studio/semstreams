package agenticloop

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
)

// TestContentStoreInitOutcome drives FINDING-1 through the PRODUCTION decision seam
// contentStoreInitOutcome — the function initializeKVBuckets calls to classify the
// trajectory content-store constructor error. The content store is legitimately
// OPTIONAL, so a genuinely unavailable store is disabled and Start boots; but a FATAL
// D2 retention violation (#600/#616) must fail Start CLOSED. Invoking the real seam
// (not a reconstructed wrap chain) makes a mutation that folds the fatal into the
// graceful path — re-introducing the #632 swallowed-fatal defect — turn this red.
//
// Internal (package agenticloop) so it can call the unexported seam. The fatal input
// is the real D1 classifier (natsclient.CheckNoLifecycleRetention) wrapped fatal
// exactly as reconcileNoLifecycleRetention emits it (retention.go:119-121) — the
// shape initializeKVBuckets receives from the shared store constructor; the
// objectstore seam test drives that guard end-to-end via a denied fake JS.
func TestContentStoreInitOutcome(t *testing.T) {
	const bucket = "AGENT_CONTENT"
	guardFatal := errs.WrapFatal(
		natsclient.CheckNoLifecycleRetention(bucket, 24*time.Hour, -1),
		"Store", "reconcileNoLifecycleRetention",
		"content store retains lifecycle eviction after reconcile")
	require.True(t, errs.IsFatal(guardFatal), "precondition: the guard error is fatal")

	t.Run("a fatal retention error fails Start closed", func(t *testing.T) {
		got := contentStoreInitOutcome(guardFatal)
		require.Error(t, got, "a fatal must NOT resolve to the graceful (nil/disable) path")
		assert.True(t, errs.IsFatal(got), "the retention violation stays fatal")
		assert.ErrorIs(t, got, natsclient.ErrGraphBucketRetention, "the sentinel survives")
	})

	t.Run("a non-fatal error degrades gracefully (nil ⇒ disable)", func(t *testing.T) {
		transient := errs.WrapTransient(errors.New("nats unavailable"),
			"Store", "NewStoreWithConfigAndMetrics", "get JetStream context")
		assert.NoError(t, contentStoreInitOutcome(transient),
			"non-fatal ⇒ nil so the caller disables content storage and boots")
	})

	t.Run("no constructor error ⇒ nil (wire the store)", func(t *testing.T) {
		assert.NoError(t, contentStoreInitOutcome(nil))
	})
}
