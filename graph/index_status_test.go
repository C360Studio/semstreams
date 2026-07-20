package graph

import "testing"

// ReadyWithinLag is the single home for the bounded-lag hard-stop rule (ADR-082).
// These cases pin every branch the 5-lens review flagged: the degraded/reset hard
// stops, the empty-graph (target==0) guard, exact n=0 == Ready parity across ALL
// states, and the Lag boundary at exactly n.
func TestReadyWithinLag(t *testing.T) {
	tests := []struct {
		name   string
		status IndexStatusResponse
		n      uint64
		want   bool
	}{
		{
			name:   "building within tolerance is ready",
			status: IndexStatusResponse{State: IndexStateBuilding, TargetRevision: 500, Lag: 5},
			n:      100,
			want:   true,
		},
		{
			name:   "building at exactly the tolerance boundary is ready",
			status: IndexStatusResponse{State: IndexStateBuilding, TargetRevision: 500, Lag: 100},
			n:      100,
			want:   true,
		},
		{
			name:   "building one past the tolerance boundary defers",
			status: IndexStatusResponse{State: IndexStateBuilding, TargetRevision: 500, Lag: 101},
			n:      100,
			want:   false,
		},
		{
			name:   "degraded is a hard stop under any tolerance",
			status: IndexStatusResponse{State: IndexStateDegraded, TargetRevision: 500, Lag: 5},
			n:      1000,
			want:   false,
		},
		{
			name:   "reset_required is a hard stop under any tolerance",
			status: IndexStatusResponse{State: IndexStateResetRequired, TargetRevision: 500, Lag: 5},
			n:      1000,
			want:   false,
		},
		{
			// The load-bearing empty-graph guard (5-lens F1): Lag==0 here does NOT
			// mean caught-up because Ready==false and target==0.
			name:   "empty graph (target 0) is never ready by tolerance",
			status: IndexStatusResponse{State: IndexStateBuilding, TargetRevision: 0, Lag: 0},
			n:      100,
			want:   false,
		},
		{
			name:   "empty graph is not ready even at n=0",
			status: IndexStatusResponse{State: IndexStateBuilding, TargetRevision: 0, Lag: 0},
			n:      0,
			want:   false,
		},
		{
			name:   "ready index is ready regardless of tolerance",
			status: IndexStatusResponse{Ready: true, State: IndexStateReady, TargetRevision: 500, Lag: 0},
			n:      0,
			want:   true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.status.ReadyWithinLag(tt.n); got != tt.want {
				t.Errorf("ReadyWithinLag(%d) = %v, want %v", tt.n, got, tt.want)
			}
		})
	}
}

// TestReadyWithinLag_ZeroToleranceEqualsReady proves n=0 is exact parity with the
// Ready gate for every state/target/lag combination — INCLUDING the target==0 row,
// where Ready is false and ReadyWithinLag(0) must also be false.
func TestReadyWithinLag_ZeroToleranceEqualsReady(t *testing.T) {
	statuses := []IndexStatusResponse{
		{Ready: true, State: IndexStateReady, TargetRevision: 100, Lag: 0},
		{Ready: false, State: IndexStateBuilding, TargetRevision: 100, Lag: 40},
		{Ready: false, State: IndexStateBuilding, TargetRevision: 0, Lag: 0}, // empty graph
		{Ready: false, State: IndexStateDegraded, TargetRevision: 100, Lag: 10},
		{Ready: false, State: IndexStateResetRequired, TargetRevision: 100, Lag: 10},
		{Ready: true, State: IndexStateReady, TargetRevision: 100, Lag: 0}, // caught up
	}
	for i, s := range statuses {
		if got := s.ReadyWithinLag(0); got != s.Ready {
			t.Errorf("case %d: ReadyWithinLag(0) = %v, want Ready = %v (%+v)", i, got, s.Ready, s)
		}
	}
}
