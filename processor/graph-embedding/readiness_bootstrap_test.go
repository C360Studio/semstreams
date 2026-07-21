package graphembedding

import (
	"context"
	"testing"

	"github.com/c360studio/semstreams/graph"
)

// TestComputeEmbeddingStatus_BootstrapCompleteStamped pins the ADR-084 D2 bit on this
// producer's envelope. The latch is the ENTITY_STATES WatchAll initial-sync sentinel
// (bootstrapComplete) — the same fact ensureBootstrapReady already gates queries on,
// now carried to the wire so a remote consumer can ask the health question the local
// gate has always been able to ask.
//
// The stamp must survive the hard-stop early returns as well as the normal path, or a
// consumer would read "never bootstrapped" from an index that bootstrapped and then
// broke — two states with different operator responses.
func TestComputeEmbeddingStatus_BootstrapCompleteStamped(t *testing.T) {
	t.Parallel()

	t.Run("watcher-unavailable hard stop carries the latch", func(t *testing.T) {
		c := newBootstrapTestComponent()
		c.bootstrapComplete.Store(true)
		c.watchUnavailable.Store(true)

		status := c.computeEmbeddingStatus(context.Background())
		if status.State != graph.IndexStateDegraded {
			t.Fatalf("precondition: State = %q, want degraded", status.State)
		}
		if !status.BootstrapComplete {
			t.Error("a bootstrapped-then-degraded pipeline reported bootstrap_complete=false")
		}
	})

	t.Run("mid-bootstrap reports false", func(t *testing.T) {
		c := newBootstrapTestComponent()
		c.bootstrapStarted.Store(true)

		status := c.computeEmbeddingStatus(context.Background())
		if status.BootstrapComplete {
			t.Error("a pipeline still validating its first snapshot reported bootstrap_complete")
		}
	})

	t.Run("reset-required hard stop carries the latch", func(t *testing.T) {
		c := newBootstrapTestComponent()
		c.bootstrapComplete.Store(true)
		c.latchGraphStateReset(graph.GraphStateReasonNoncanonicalPredicate)

		status := c.computeEmbeddingStatus(context.Background())
		if status.State != graph.IndexStateResetRequired {
			t.Fatalf("precondition: State = %q, want reset_required", status.State)
		}
		if !status.BootstrapComplete {
			t.Error("a bootstrapped-then-poisoned pipeline reported bootstrap_complete=false")
		}
	})
}
