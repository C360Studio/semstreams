package graphindex

import (
	"sync"
	"testing"
	"time"

	"github.com/c360studio/semstreams/graph"
)

// The watermark's whole reason to exist is honesty under SPARSE, out-of-order,
// multi-path completion (ADR-066 §1). These tests feed sparse ascending revision
// streams (e.g. {50, 72, 100}) — NOT dense 1..N — because History=1 +
// DeliverLastPerSubject never delivers a dense set, and the rejected
// "advance-from-indexed+1" algorithm would stall on the very first gap.

func TestRevisionWatermark_ColdStart(t *testing.T) {
	w := newRevisionWatermark()
	if got := w.indexed(); got != 0 {
		t.Fatalf("cold start indexed = %d, want 0", got)
	}
}

func TestRevisionWatermark_SparseMidBuild_LowWaterOfPending(t *testing.T) {
	w := newRevisionWatermark()
	// Sparse ascending delivery: distinct keys at purged-gap revisions.
	w.observe(50, "A")
	w.observe(72, "B")
	w.observe(100, "C")

	// Nothing completed: indexed is minPending-1, NOT stuck at 0 waiting for rev 1.
	if got := w.indexed(); got != 49 {
		t.Fatalf("all pending: indexed = %d, want 49 (minPending-1)", got)
	}

	// Out-of-order completion: a fast LATE worker (rev 72) cannot report the gap done.
	w.complete("B", 72)
	if got := w.indexed(); got != 49 {
		t.Fatalf("after completing 72 (50 still pending): indexed = %d, want 49", got)
	}

	// Complete the low revision: watermark advances past it to the next gap.
	w.complete("A", 50)
	if got := w.indexed(); got != 99 {
		t.Fatalf("after completing 50 (100 still pending): indexed = %d, want 99", got)
	}

	// Complete the last: pending empties → indexed jumps to observedHigh (=100),
	// honestly skipping the purged 51..71, 73..99 that were never delivered.
	w.complete("C", 100)
	if got := w.indexed(); got != 100 {
		t.Fatalf("caught up: indexed = %d, want 100 (observedHigh)", got)
	}
}

func TestRevisionWatermark_CoalescerCollapse_KeyScopedDrainsLowerRevisions(t *testing.T) {
	w := newRevisionWatermark()
	// One key delivered three times (the coalescer will re-Get only the latest, 30).
	w.observe(10, "K")
	w.observe(20, "K")
	w.observe(30, "K")
	if got := w.indexed(); got != 9 {
		t.Fatalf("three pending revs of K: indexed = %d, want 9", got)
	}

	// The single completion at the re-Got latest revision drains ALL collapsed lower
	// revisions of K that no worker ever sees individually. Exact-revision completion
	// would strand 10 and 20 forever.
	w.complete("K", 30)
	if got := w.indexed(); got != 30 {
		t.Fatalf("after key-scoped complete(K,30): indexed = %d, want 30", got)
	}
}

func TestRevisionWatermark_DeleteSupersedesPendingUpdate(t *testing.T) {
	w := newRevisionWatermark()
	// Update K@10 is delivered and in flight...
	w.observe(10, "K")
	// ...then a delete K@11 arrives. The delete's key-scoped completion drains the
	// tombstone AND the earlier still-pending update it supersedes (Defect #1 fix).
	w.observe(11, "K")
	w.complete("K", 11)
	if got := w.indexed(); got != 11 {
		t.Fatalf("after delete supersedes pending update: indexed = %d, want 11", got)
	}
}

func TestRevisionWatermark_CompletionIsKeyScoped(t *testing.T) {
	w := newRevisionWatermark()
	w.observe(10, "A")
	w.observe(11, "B") // a delete of a DIFFERENT key
	// Completing B must NOT drain A's pending revision even though 10 <= 11.
	w.complete("B", 11)
	if got := w.indexed(); got != 9 {
		t.Fatalf("key-scoped: completing B left A@10 pending, indexed = %d, want 9", got)
	}
	w.complete("A", 10)
	if got := w.indexed(); got != 11 {
		t.Fatalf("after A completes too: indexed = %d, want 11 (observedHigh)", got)
	}
}

func TestRevisionWatermark_TrailingDeleteOnlyTail(t *testing.T) {
	w := newRevisionWatermark()
	w.observe(10, "A")
	w.complete("A", 10)
	if got := w.indexed(); got != 10 {
		t.Fatalf("after A: indexed = %d, want 10", got)
	}
	// A tail of pure deletes must still advance the watermark — their sequences
	// participate, or lag wedges forever.
	w.observe(11, "B")
	w.complete("B", 11)
	if got := w.indexed(); got != 11 {
		t.Fatalf("trailing delete: indexed = %d, want 11", got)
	}
}

func TestRevisionWatermark_RevisionZeroIgnored(t *testing.T) {
	w := newRevisionWatermark()
	w.observe(0, "X") // "no revision" — stream sequences start at 1
	if got := w.indexed(); got != 0 {
		t.Fatalf("observe(0) should be a no-op, indexed = %d, want 0", got)
	}
	w.observe(5, "A")
	if got := w.indexed(); got != 4 {
		t.Fatalf("after observe(5): indexed = %d, want 4", got)
	}
}

func TestRevisionWatermark_CompleteUnobservedRevisionDrainsLower(t *testing.T) {
	// The coalescer re-Get can return a revision NEWER than any yet delivered to the
	// watch (a write between snapshot and Get). Completing at that not-yet-observed
	// revision must still drain the key's lower pending, and must NOT advance
	// observedHigh (only the watch does that).
	w := newRevisionWatermark()
	w.observe(10, "K")
	w.complete("K", 40) // 40 never observed via watch
	if got := w.indexed(); got != 10 {
		t.Fatalf("indexed = %d, want 10 (observedHigh unchanged; 10 drained)", got)
	}
}

func TestIndexReadiness(t *testing.T) {
	tests := []struct {
		name             string
		indexed, target  uint64
		stuck            bool
		wantReady        bool
		wantState        string
		wantLag          uint64
		wantRevisionText string
	}{
		{"cold start", 0, 100, false, false, graph.IndexStateBuilding, 100, ""},
		{"mid build", 50, 100, false, false, graph.IndexStateBuilding, 50, "50"},
		{"caught up", 100, 100, false, true, graph.IndexStateReady, 0, "100"},
		{"caught up past", 105, 100, false, true, graph.IndexStateReady, 0, "105"},
		{"stuck while lagging", 50, 100, true, false, graph.IndexStateDegraded, 50, "50"},
		{"ready overrides stuck", 100, 100, true, true, graph.IndexStateReady, 0, "100"},
		{"empty stream not ready", 0, 0, false, false, graph.IndexStateBuilding, 0, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := indexReadiness(tt.indexed, tt.target, tt.stuck, "ts")
			if got.Ready != tt.wantReady {
				t.Errorf("Ready = %v, want %v", got.Ready, tt.wantReady)
			}
			if got.State != tt.wantState {
				t.Errorf("State = %q, want %q", got.State, tt.wantState)
			}
			if got.Lag != tt.wantLag {
				t.Errorf("Lag = %d, want %d", got.Lag, tt.wantLag)
			}
			if got.IndexedRevision != tt.indexed || got.TargetRevision != tt.target {
				t.Errorf("revisions = (%d,%d), want (%d,%d)", got.IndexedRevision, got.TargetRevision, tt.indexed, tt.target)
			}
			if got.Revision != tt.wantRevisionText {
				t.Errorf("Revision text = %q, want %q", got.Revision, tt.wantRevisionText)
			}
		})
	}
}

func TestTrackReadinessProgress_StuckDetector(t *testing.T) {
	c := &Component{}

	// First observation initializes progress; never stuck on the first call.
	if stuck, _ := c.trackReadinessProgress(10, 100); stuck {
		t.Fatal("first call must not be stuck")
	}

	// Simulate a stall: no advance and the last progress was long ago.
	c.lastProgressAt = time.Now().Add(-degradedStuckAfter - time.Second)
	if stuck, _ := c.trackReadinessProgress(10, 100); !stuck {
		t.Fatal("stalled watermark while lagging must be stuck (degraded)")
	}

	// A real advance clears the stall.
	if stuck, _ := c.trackReadinessProgress(20, 100); stuck {
		t.Fatal("advancing watermark must not be stuck")
	}

	// Caught up is never stuck, even if the timestamp is old.
	c.lastProgressAt = time.Now().Add(-degradedStuckAfter - time.Second)
	if stuck, _ := c.trackReadinessProgress(100, 100); stuck {
		t.Fatal("caught-up index must never be degraded")
	}
}

// TestRevisionWatermark_Concurrent runs observe/complete across many goroutines to
// prove the mutex holds and that draining every observed revision leaves indexed at
// observedHigh (run under -race). Mirrors the real wiring: the watch goroutine
// observes ascending; N workers complete out of order.
func TestRevisionWatermark_Concurrent(t *testing.T) {
	w := newRevisionWatermark()
	const n = 2000

	// Observe ascending (as OrderedConsumer delivery guarantees), one key each.
	keys := make([]string, n)
	for i := 0; i < n; i++ {
		keys[i] = string(rune('a'+i%26)) + "-" + time.Duration(i).String()
		w.observe(uint64(i+1), keys[i])
	}

	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(rev int) {
			defer wg.Done()
			w.complete(keys[rev], uint64(rev+1))
		}(i)
	}
	wg.Wait()

	if got := w.indexed(); got != uint64(n) {
		t.Fatalf("after draining all: indexed = %d, want %d", got, n)
	}
}
