package graphembedding

import (
	"context"
	"testing"

	"github.com/c360studio/semstreams/graph"
)

// TestComputeEmbeddingStatus_BootstrapCompleteStamped pins the ADR-084 D2 bit on this
// producer's envelope. The latch is the APPLIED build (buildApplied) — the initial
// snapshot carried to a terminal embedding outcome — NOT the WatchAll sentinel, which
// only proves delivery. See TestBuildApplied_RequiresAppliedNotDelivered.
//
// The stamp must survive the hard-stop early returns as well as the normal path, or a
// consumer would read "never bootstrapped" from an index that bootstrapped and then
// broke — two states with different operator responses.
func TestComputeEmbeddingStatus_BootstrapCompleteStamped(t *testing.T) {
	t.Parallel()

	t.Run("watcher-unavailable hard stop carries the latch", func(t *testing.T) {
		c := newBootstrapTestComponent()
		c.buildApplied.Store(true)
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
		c.buildApplied.Store(true)
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

// TestBuildApplied_RequiresAppliedNotDelivered is the review's blocking finding for this
// producer. The public bootstrap_complete bit was set at the WatchAll nil sentinel —
// the moment the initial entries had been DELIVERED and validated. But embedding
// generation is asynchronous, so the watermark can sit far behind the enumeration-time
// target while the envelope already claims the build finished. An unbounded health
// consumer such as fusion would then serve a partially built cold index, which is
// exactly what bootstrap_complete exists to prevent.
//
// Delivery is not application. The bit now waits for the applied floor to reach a
// target captured when enumeration ended — fixed, so it stays reachable under load.
func TestBuildApplied_RequiresAppliedNotDelivered(t *testing.T) {
	t.Parallel()

	t.Run("delivered but not applied reports false", func(t *testing.T) {
		c := newBootstrapTestComponent()
		c.bootstrapTarget.Store(500) // enumeration ended at revision 500
		c.bootstrapComplete.Store(true)

		c.latchBuildApplied(499) // one revision short of applied
		if c.buildApplied.Load() {
			t.Error("claimed bootstrap_complete while the initial snapshot was still embedding")
		}
	})

	t.Run("applied to the enumeration-time target latches", func(t *testing.T) {
		c := newBootstrapTestComponent()
		c.bootstrapTarget.Store(500)
		c.bootstrapComplete.Store(true)

		c.latchBuildApplied(500)
		if !c.buildApplied.Load() {
			t.Error("the initial build reached its target but the bit stayed false")
		}
	})

	t.Run("latches under continuous write", func(t *testing.T) {
		// The reachability property: the target is the ENUMERATION-TIME one, so a
		// stream that keeps advancing past it cannot hold the latch shut.
		c := newBootstrapTestComponent()
		c.bootstrapTarget.Store(500)
		c.bootstrapComplete.Store(true)

		c.latchBuildApplied(900) // floor well past enumeration; live target higher still
		if !c.buildApplied.Load() {
			t.Error("a built pipeline under continuous write never reports bootstrap_complete")
		}
	})

	t.Run("an empty graph latches immediately", func(t *testing.T) {
		c := newBootstrapTestComponent()
		c.bootstrapTarget.Store(0) // sentinel fired with nothing delivered
		c.bootstrapComplete.Store(true)

		c.latchBuildApplied(0)
		if !c.buildApplied.Load() {
			t.Error("an authoritatively empty graph never reports bootstrap_complete")
		}
	})

	t.Run("no sentinel means no latch", func(t *testing.T) {
		c := newBootstrapTestComponent()
		c.latchBuildApplied(1000)
		if c.buildApplied.Load() {
			t.Error("latched without the snapshot ever being delivered")
		}
	})
}
