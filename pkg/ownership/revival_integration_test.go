//go:build integration

package ownership

import (
	"context"
	"testing"
	"time"

	"github.com/c360studio/semstreams/natsclient"
)

// TestIntegration_WatchRevival_QuiescesOnSupersession proves the ADR-056 PR-4
// Watch-revival quiesce end-to-end through the real NATS KV epoch (testcontainer):
// when a SECOND process (a distinct Registry with a different incarnation)
// re-registers the SAME owner id, the first process's WatchRevival goroutine sees
// the live epoch's incarnation change and QUIESCES that owner.
//
// Supersession is driven deterministically (two RegisterOwner calls, both awaited)
// and detection is awaited via the test-only revivalUpdates signal channel — no
// sleeps gate the assertion (only a bounded safety deadline).
func TestIntegration_WatchRevival_QuiescesOnSupersession(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithKV())
	ctx := context.Background()
	const owner = "watch-revival-owner"

	// regA = "this process". Wire the test signal channel BEFORE starting the watch.
	regA, err := EnsureBuckets(ctx, tc.Client, nil, nil)
	if err != nil {
		t.Fatalf("EnsureBuckets A: %v", err)
	}
	updates := make(chan struct{}, 32)
	regA.setRevivalUpdates(updates)

	if err := regA.RegisterOwner(ctx, systemOwnerRegistration(owner)); err != nil {
		t.Fatalf("regA RegisterOwner: %v", err)
	}

	watchCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() { _ = regA.WatchRevival(watchCtx, nil) }()

	// regB = "a revived second process". A distinct Registry over the SAME buckets
	// has a distinct incarnation; re-registering the SAME owner id overwrites the
	// epoch entry with regB's incarnation — the evicted-then-superseded condition.
	regB, err := EnsureBuckets(ctx, tc.Client, nil, nil)
	if err != nil {
		t.Fatalf("EnsureBuckets B: %v", err)
	}
	if regA.Incarnation() == regB.Incarnation() {
		t.Fatal("regA and regB must have distinct incarnations for the test to be meaningful")
	}
	if err := regB.RegisterOwner(ctx, systemOwnerRegistration(owner)); err != nil {
		t.Fatalf("regB RegisterOwner: %v", err)
	}

	// Await WatchRevival processing the supersession via the signal channel, bounded
	// by a safety deadline. Each receive = one processed epoch update; re-check the
	// latch (which is set BEFORE the signal fires) after each.
	deadline := time.After(15 * time.Second)
	for !regA.IsQuiesced(owner) {
		select {
		case <-updates:
		case <-deadline:
			t.Fatal("timed out waiting for WatchRevival to quiesce regA after supersession by regB")
		}
	}

	if !regA.IsQuiesced(owner) {
		t.Error("regA must be quiesced after regB's incarnation superseded it in the epoch")
	}
	// regB is the rightful live owner; nothing quiesced it (its own watch isn't running).
	if regB.IsQuiesced(owner) {
		t.Error("regB (the rightful owner) must NOT be quiesced")
	}
}

// TestIntegration_WatchRevival_NoQuiesceOnBenignBump proves the negative: an epoch
// bump that does NOT touch our owner (a different owner registering) must NOT
// quiesce us. Locks decideRevival's "present with our own incarnation / unrelated
// owner → none" branch through the real watch.
func TestIntegration_WatchRevival_NoQuiesceOnBenignBump(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithKV())
	ctx := context.Background()
	const mine = "benign-bump-mine"

	regA, err := EnsureBuckets(ctx, tc.Client, nil, nil)
	if err != nil {
		t.Fatalf("EnsureBuckets A: %v", err)
	}
	updates := make(chan struct{}, 32)
	regA.setRevivalUpdates(updates)
	if err := regA.RegisterOwner(ctx, systemOwnerRegistration(mine)); err != nil {
		t.Fatalf("regA RegisterOwner: %v", err)
	}

	// Register the unrelated owner (disjoint pattern, no overlap) BEFORE starting
	// regA's watcher. This is what makes the test deterministic: the watcher's
	// initial replay then delivers an epoch that PROVABLY already contains
	// "benign-bump-other", so the single signal we await below cannot be a
	// regA-only initial state — it necessarily reflects the benign bump.
	regB, err := EnsureBuckets(ctx, tc.Client, nil, nil)
	if err != nil {
		t.Fatalf("EnsureBuckets B: %v", err)
	}
	if err := regB.RegisterOwner(ctx, Registration{
		Owner: "benign-bump-other",
		Claims: []OwnerClaim{{
			Owner: "benign-bump-other", Pattern: "c360.other.*.*.*.*", Mode: ModeReplaceOwned,
			Predicates: []string{"test.value.p"},
		}},
	}); err != nil {
		t.Fatalf("regB RegisterOwner: %v", err)
	}

	watchCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() { _ = regA.WatchRevival(watchCtx, nil) }()

	// Await the watcher processing its initial replay (which contains BOTH owners),
	// then assert regA is STILL not quiesced — a benign bump by an unrelated owner
	// must not quiesce us.
	select {
	case <-updates:
	case <-time.After(15 * time.Second):
		t.Fatal("timed out waiting for WatchRevival to process the initial epoch (with both owners)")
	}
	if regA.IsQuiesced(mine) {
		t.Error("a benign epoch bump by an unrelated owner must NOT quiesce regA")
	}
}
