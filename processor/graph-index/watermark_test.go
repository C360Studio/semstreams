package graphindex

import (
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
			got := graph.ComputeIndexStatus(tt.indexed, tt.target, tt.stuck, "ts")
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
