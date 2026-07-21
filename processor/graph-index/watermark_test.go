package graphindex

import (
	"context"
	"testing"
	"time"

	"github.com/c360studio/semstreams/graph"
)

// The low-water-of-pending watermark mechanism is tested in pkg/revlag and the
// Ready/Lag/State projection in graph.ComputeIndexStatus's own package. This test
// pins graph-index's use of the projection plus the §4 wall-clock stuck detector.

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
			got := graph.ComputeIndexStatus(graph.IndexStatusInputs{
				Indexed: tt.indexed, Target: tt.target, Stuck: tt.stuck, LastSynced: "ts",
			})
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

// TestApplyKnownIncompleteOverrides pins the two graph-index-local readiness
// invariants layered onto the shared projection: the empty-graph ready case and
// the UNCONDITIONAL failedCount→degraded hard stop (ADR-082, task 2.1b). The
// unconditional projection is the blocking fix — a known-incomplete index with a
// small lag must read degraded, not building, or a bounded-lag consumer would run
// clustering on partial topology.
//
// Propagation latency is part of the contract, not a defect (ADR-084 D7): the override
// lands in-process immediately, but an ENVELOPE consumer learns about it only on the
// next GRAPH_STATUS heartbeat, so a hard stop binds remote gates up to one heartbeat
// late. In-process gates (the reverse-index responder's own failedCount check) are
// stronger and are checked per query. Both are deliberate; the accepted window is
// recorded here so a future reader does not "fix" it by shortening the heartbeat.
func TestApplyKnownIncompleteOverrides(t *testing.T) {
	tests := []struct {
		name         string
		indexed      uint64
		target       uint64
		stuck        bool
		enumComplete bool
		failedCount  int64
		wantReady    bool
		wantState    string
	}{
		{
			name:    "building with lag and no failures is unchanged",
			indexed: 50, target: 100, wantReady: false, wantState: graph.IndexStateBuilding,
		},
		{
			name:    "caught up with no failures is ready",
			indexed: 100, target: 100, wantReady: true, wantState: graph.IndexStateReady,
		},
		{
			// The blocking case: failed required write + small lag under continuous
			// write. Ready is already false via Lag>0; the OLD &&Ready-gated override
			// left State=building here, which bounded-lag would treat as runnable.
			name:    "failedCount with building+lag is degraded (unconditional)",
			indexed: 99, target: 100, failedCount: 1,
			wantReady: false, wantState: graph.IndexStateDegraded,
		},
		{
			name:    "failedCount when caught up is degraded (was already)",
			indexed: 100, target: 100, failedCount: 1,
			wantReady: false, wantState: graph.IndexStateDegraded,
		},
		{
			name:    "empty graph after enumeration is ready",
			indexed: 0, target: 0, enumComplete: true,
			wantReady: true, wantState: graph.IndexStateReady,
		},
		{
			// failedCount override runs AFTER the empty-graph override, so a
			// known-incomplete empty-after-enumeration index is degraded, not ready.
			name:    "failedCount overrides the empty-graph-ready case",
			indexed: 0, target: 0, enumComplete: true, failedCount: 1,
			wantReady: false, wantState: graph.IndexStateDegraded,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := graph.ComputeIndexStatus(graph.IndexStatusInputs{
				Indexed: tt.indexed, Target: tt.target, Stuck: tt.stuck, LastSynced: "ts",
			})
			got := applyKnownIncompleteOverrides(base, tt.target, tt.enumComplete, tt.failedCount)
			if got.Ready != tt.wantReady {
				t.Errorf("Ready = %v, want %v", got.Ready, tt.wantReady)
			}
			if got.State != tt.wantState {
				t.Errorf("State = %q, want %q", got.State, tt.wantState)
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

// TestLatchBootstrap pins the ADR-084 D2 latch: it flips on the first caught-up
// envelope, never clears, and stamps every envelope thereafter. The predicate is
// deliberately the same Ready the reverse-index query gate reduces to, so the wire bit
// and that gate can never disagree.
func TestLatchBootstrap(t *testing.T) {
	t.Run("building does not latch", func(t *testing.T) {
		c := &Component{}
		got := c.latchBootstrap(graph.ComputeIndexStatus(
			graph.IndexStatusInputs{Indexed: 40, Target: 100}))
		if got.BootstrapComplete {
			t.Error("an index still catching up reported bootstrap_complete")
		}
	})

	t.Run("caught up latches", func(t *testing.T) {
		c := &Component{}
		got := c.latchBootstrap(graph.ComputeIndexStatus(
			graph.IndexStatusInputs{Indexed: 100, Target: 100}))
		if !got.BootstrapComplete || !c.indexBootstrapped.Load() {
			t.Errorf("caught-up envelope did not latch: wire=%v latch=%v",
				got.BootstrapComplete, c.indexBootstrapped.Load())
		}
	})

	t.Run("authoritatively empty graph latches", func(t *testing.T) {
		// The 0/0 outcome is a COMPLETED build, not a missing one (gh#474 Codex #5).
		// It reaches the latch as Ready only through the empty-graph override, so this
		// row is what keeps a fresh empty deployment from fail-closing forever.
		c := &Component{}
		base := graph.ComputeIndexStatus(graph.IndexStatusInputs{Indexed: 0, Target: 0})
		got := c.latchBootstrap(applyKnownIncompleteOverrides(base, 0, true, 0))
		if !got.BootstrapComplete {
			t.Error("authoritatively empty graph never reports bootstrap_complete; health gates would defer forever")
		}
	})

	t.Run("latch survives a later hard stop", func(t *testing.T) {
		// "Built, then broke" must stay distinguishable from "never built": State
		// carries the hard stop, the latch carries the history. Clearing it here would
		// conflate two states that want different operator responses.
		c := &Component{}
		c.latchBootstrap(graph.ComputeIndexStatus(graph.IndexStatusInputs{Indexed: 100, Target: 100}))
		base := graph.ComputeIndexStatus(graph.IndexStatusInputs{Indexed: 100, Target: 100})
		degraded := c.latchBootstrap(applyKnownIncompleteOverrides(base, 100, true, 1))
		if degraded.State != graph.IndexStateDegraded {
			t.Fatalf("precondition: State = %q, want degraded", degraded.State)
		}
		if !degraded.BootstrapComplete {
			t.Error("a degraded-after-bootstrap index reported bootstrap_complete=false")
		}
	})
}

// TestComputeIndexStatus_ReadyImpliesBootstrapComplete pins the producer invariant the
// collapsed gate depends on. The gate asks health BEFORE coverage, so an envelope that
// claims Ready while reporting an unfinished build is not merely odd — it defers
// forever, and it does so with a reason (bootstrap_incomplete) that sends an operator
// looking for a cutover that already finished.
//
// Every return path of computeIndexStatus is exercised, including the caged early-boot
// fallback, which is exactly where the invariant broke once: it reported the retired
// sticky NAME_INDEX Ready while stamping the process latch, which was still false.
func TestComputeIndexStatus_ReadyImpliesBootstrapComplete(t *testing.T) {
	assertInvariant := func(t *testing.T, name string, status graph.IndexStatusResponse) {
		t.Helper()
		if status.Ready && !status.BootstrapComplete {
			t.Errorf("%s: Ready with BootstrapComplete=false — the health gate would defer on a caught-up index", name)
		}
	}

	t.Run("caged early-boot fallback", func(t *testing.T) {
		// watermark wired but no entity bucket: the fallback path. Whatever the legacy
		// signal answers, the two bits must agree.
		c := createTestComponentWithMockKV(t)
		c.entityStatesBucket = nil
		assertInvariant(t, "fallback", c.computeIndexStatus(context.Background()))
	})

	t.Run("projected envelopes", func(t *testing.T) {
		for _, tc := range []struct {
			name                     string
			indexed, target          uint64
			enumComplete, hasFailure bool
		}{
			{name: "caught up", indexed: 100, target: 100},
			{name: "building", indexed: 40, target: 100},
			{name: "empty pre-enumeration", indexed: 0, target: 0},
			{name: "empty after enumeration", indexed: 0, target: 0, enumComplete: true},
			{name: "caught up with failures", indexed: 100, target: 100, hasFailure: true},
		} {
			t.Run(tc.name, func(t *testing.T) {
				c := &Component{}
				var failed int64
				if tc.hasFailure {
					failed = 1
				}
				base := graph.ComputeIndexStatus(graph.IndexStatusInputs{Indexed: tc.indexed, Target: tc.target})
				status := c.latchBootstrap(applyKnownIncompleteOverrides(base, tc.target, tc.enumComplete, failed))
				assertInvariant(t, tc.name, status)
			})
		}
	})
}
