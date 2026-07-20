package graph

import (
	"testing"
	"time"
)

// The gate is the single home for readiness semantics (ADR-083 D4), so these tests
// pin the properties every adopter depends on: exact-mode bit-parity with Ready,
// zero tolerance == Ready, hard stops that survive any tolerance, an empty graph
// that is never ready by tolerance, and an unknown status that short-circuits BEFORE
// tolerance is considered.

func TestEvaluateReadinessGate(t *testing.T) {
	tests := []struct {
		name       string
		status     IndexStatusResponse
		fresh      bool
		mode       GateMode
		cfg        GateConfig
		want       bool
		wantReason DeferReason
	}{
		{
			name:   "exact proceeds on a fresh ready envelope",
			status: IndexStatusResponse{Ready: true, State: IndexStateReady, TargetRevision: 500},
			fresh:  true, mode: GateExact,
			want: true,
		},
		{
			name:   "exact defers while building",
			status: IndexStatusResponse{State: IndexStateBuilding, TargetRevision: 500, Lag: 60, StalenessMs: 20},
			fresh:  true, mode: GateExact,
			wantReason: DeferOverStaleness,
		},
		{
			name:   "exact ignores a tolerance it was never given",
			status: IndexStatusResponse{State: IndexStateBuilding, TargetRevision: 500, StalenessMs: 100},
			fresh:  true, mode: GateExact, cfg: GateConfig{MaxStaleness: time.Minute},
			wantReason: DeferOverStaleness,
		},
		{
			name:   "degrade-honest evaluates identically to exact",
			status: IndexStatusResponse{State: IndexStateBuilding, TargetRevision: 500, StalenessMs: 100},
			fresh:  true, mode: GateDegradeHonest, cfg: GateConfig{MaxStaleness: time.Minute},
			wantReason: DeferOverStaleness,
		},
		{
			name:   "bounded staleness proceeds within tolerance",
			status: IndexStatusResponse{State: IndexStateBuilding, TargetRevision: 500, StalenessMs: 1200},
			fresh:  true, mode: GateBoundedStaleness, cfg: GateConfig{MaxStaleness: 3 * time.Second},
			want: true,
		},
		{
			name:   "bounded staleness proceeds exactly at the boundary",
			status: IndexStatusResponse{State: IndexStateBuilding, TargetRevision: 500, StalenessMs: 3000},
			fresh:  true, mode: GateBoundedStaleness, cfg: GateConfig{MaxStaleness: 3 * time.Second},
			want: true,
		},
		{
			name:   "bounded staleness defers one millisecond past the boundary",
			status: IndexStatusResponse{State: IndexStateBuilding, TargetRevision: 500, StalenessMs: 3001},
			fresh:  true, mode: GateBoundedStaleness, cfg: GateConfig{MaxStaleness: 3 * time.Second},
			wantReason: DeferOverStaleness,
		},
		{
			// The presence encoding: a not-ready envelope whose producer could not
			// compute staleness carries 0, which must NOT read as "0ms stale".
			name:   "bounded staleness defers on an absent staleness value",
			status: IndexStatusResponse{State: IndexStateBuilding, TargetRevision: 500, StalenessMs: 0},
			fresh:  true, mode: GateBoundedStaleness, cfg: GateConfig{MaxStaleness: time.Hour},
			wantReason: DeferOverStaleness,
		},
		{
			name:   "unknown status defers even with a generous tolerance",
			status: IndexStatusResponse{Ready: true, State: IndexStateReady, TargetRevision: 500},
			fresh:  false, mode: GateBoundedStaleness, cfg: GateConfig{MaxStaleness: time.Hour},
			wantReason: DeferStatusUnknown,
		},
		{
			// Freshness is judged before state: a dead feed holding a degraded
			// envelope is attributed to the feed, not to the index.
			name:   "unknown status outranks a stale hard stop",
			status: IndexStatusResponse{State: IndexStateDegraded, TargetRevision: 500},
			fresh:  false, mode: GateExact,
			wantReason: DeferStatusUnknown,
		},
		{
			name:   "unknown status defers sticky-bootstrap after its first pass",
			status: IndexStatusResponse{Ready: true, State: IndexStateReady, TargetRevision: 500},
			fresh:  false, mode: GateStickyBootstrap, cfg: GateConfig{BootstrapDone: true},
			wantReason: DeferStatusUnknown,
		},
		{
			name:   "degraded is a hard stop under any tolerance",
			status: IndexStatusResponse{State: IndexStateDegraded, TargetRevision: 500, StalenessMs: 5},
			fresh:  true, mode: GateBoundedStaleness, cfg: GateConfig{MaxStaleness: time.Hour},
			wantReason: DeferHardStop,
		},
		{
			name:   "reset_required is a hard stop under any tolerance",
			status: IndexStatusResponse{State: IndexStateResetRequired, TargetRevision: 500, StalenessMs: 5},
			fresh:  true, mode: GateBoundedStaleness, cfg: GateConfig{MaxStaleness: time.Hour},
			wantReason: DeferHardStop,
		},
		{
			name:   "a hard stop overrides sticky-bootstrap's open gate",
			status: IndexStatusResponse{State: IndexStateDegraded, TargetRevision: 500},
			fresh:  true, mode: GateStickyBootstrap, cfg: GateConfig{BootstrapDone: true},
			wantReason: DeferHardStop,
		},
		{
			// The load-bearing empty guard: Lag == 0 here does NOT mean caught up.
			name:   "empty graph is never ready by tolerance",
			status: IndexStatusResponse{State: IndexStateBuilding, TargetRevision: 0, Lag: 0, StalenessMs: 1},
			fresh:  true, mode: GateBoundedStaleness, cfg: GateConfig{MaxStaleness: time.Hour},
			wantReason: DeferEmpty,
		},
		{
			name:   "empty graph defers in exact mode as empty, not over_staleness",
			status: IndexStatusResponse{State: IndexStateBuilding, TargetRevision: 0},
			fresh:  true, mode: GateExact,
			wantReason: DeferEmpty,
		},
		{
			// graph-index's authoritatively-empty override sets Ready with target 0;
			// that must still proceed, or a fresh empty graph never serves.
			name:   "authoritatively empty but ready proceeds",
			status: IndexStatusResponse{Ready: true, State: IndexStateReady, TargetRevision: 0},
			fresh:  true, mode: GateExact,
			want: true,
		},
		{
			name:   "sticky-bootstrap is exact before its first pass",
			status: IndexStatusResponse{State: IndexStateBuilding, TargetRevision: 500, StalenessMs: 5},
			fresh:  true, mode: GateStickyBootstrap,
			wantReason: DeferOverStaleness,
		},
		{
			name:   "sticky-bootstrap is open after its first pass",
			status: IndexStatusResponse{State: IndexStateBuilding, TargetRevision: 500, Lag: 9000},
			fresh:  true, mode: GateStickyBootstrap, cfg: GateConfig{BootstrapDone: true},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, reason := EvaluateReadinessGate(tt.status, tt.fresh, tt.mode, tt.cfg)
			if got != tt.want {
				t.Errorf("proceed = %v, want %v", got, tt.want)
			}
			wantReason := tt.wantReason
			if tt.want {
				wantReason = DeferNone
			}
			if reason != wantReason {
				t.Errorf("reason = %q, want %q", reason, wantReason)
			}
		})
	}
}

// gateMatrix is the cross-product of every envelope shape the parity properties must
// hold over: each state, empty and non-empty targets, ready and not, and staleness
// values on both sides of any tolerance.
func gateMatrix() []IndexStatusResponse {
	var out []IndexStatusResponse
	for _, state := range AllIndexStates {
		for _, target := range []uint64{0, 100} {
			for _, staleness := range []uint64{0, 1, 5000} {
				ready := state == IndexStateReady
				lag := uint64(0)
				if !ready && target > 0 {
					lag = 60
				}
				out = append(out, IndexStatusResponse{
					Ready: ready, State: state, TargetRevision: target,
					Lag: lag, StalenessMs: staleness,
				})
			}
		}
	}
	// The authoritatively-empty override: Ready with target 0.
	out = append(out, IndexStatusResponse{Ready: true, State: IndexStateReady, TargetRevision: 0})
	return out
}

// TestEvaluateReadinessGate_ExactIsBitParityWithReady proves the exact modes proceed
// exactly when the pre-ADR-083 `if !status.Ready { defer }` check would have, for
// every envelope shape — the property the fusion honesty envelope and the
// reverse-index reads rest on (ADR-066's authoritative-absence license).
func TestEvaluateReadinessGate_ExactIsBitParityWithReady(t *testing.T) {
	for _, mode := range []GateMode{GateExact, GateDegradeHonest} {
		for _, s := range gateMatrix() {
			// Fresh status is the precondition: an unknown feed is a transport fact
			// the old per-call request path expressed as its own error branch.
			got, reason := EvaluateReadinessGate(s, true, mode, GateConfig{})
			if got != s.Ready {
				t.Errorf("%s: proceed = %v, want Ready = %v (%+v)", mode, got, s.Ready, s)
			}
			if got && reason != DeferNone {
				t.Errorf("%s: proceed carried reason %q (%+v)", mode, reason, s)
			}
			if !got && reason == DeferNone {
				t.Errorf("%s: defer carried no reason (%+v)", mode, s)
			}
		}
	}
}

// TestEvaluateReadinessGate_ZeroToleranceEqualsReady proves MaxStaleness = 0 is exact
// parity with Ready in bounded-staleness mode for every state, target, and staleness
// — including the rows where staleness is 0 (absent) or non-zero while not ready, so
// the default config can never open the gate a hair wider than the exact one.
func TestEvaluateReadinessGate_ZeroToleranceEqualsReady(t *testing.T) {
	for _, s := range gateMatrix() {
		got, _ := EvaluateReadinessGate(s, true, GateBoundedStaleness, GateConfig{MaxStaleness: 0})
		if got != s.Ready {
			t.Errorf("bounded-staleness(0): proceed = %v, want Ready = %v (%+v)", got, s.Ready, s)
		}
	}
}

// TestEvaluateReadinessGate_HardStopsSurviveEveryToleranceAndMode is the ADR-082
// invariant carried forward: a broken index is not a stale one, and no mode or
// tolerance may license work on it.
func TestEvaluateReadinessGate_HardStopsSurviveEveryToleranceAndMode(t *testing.T) {
	modes := []GateMode{GateExact, GateBoundedStaleness, GateStickyBootstrap, GateDegradeHonest}
	tolerances := []time.Duration{0, time.Millisecond, time.Hour, 24 * 365 * time.Hour}
	for _, state := range []string{IndexStateDegraded, IndexStateResetRequired} {
		for _, mode := range modes {
			for _, tol := range tolerances {
				for _, bootstrapped := range []bool{false, true} {
					s := IndexStatusResponse{State: state, TargetRevision: 500, Lag: 1, StalenessMs: 1}
					cfg := GateConfig{MaxStaleness: tol, BootstrapDone: bootstrapped}
					got, reason := EvaluateReadinessGate(s, true, mode, cfg)
					if got {
						t.Errorf("%s/%s tol=%v bootstrapped=%v: proceeded on a hard stop",
							state, mode, tol, bootstrapped)
					}
					if reason != DeferHardStop {
						t.Errorf("%s/%s tol=%v: reason = %q, want %q",
							state, mode, tol, reason, DeferHardStop)
					}
				}
			}
		}
	}
}

// TestEvaluateReadinessGate_UnknownStatusNeverProceeds proves the fail-closed
// short-circuit holds for every mode, tolerance, and envelope — the gh#590 lesson
// that a transport failure must never be served as index state.
func TestEvaluateReadinessGate_UnknownStatusNeverProceeds(t *testing.T) {
	modes := []GateMode{GateExact, GateBoundedStaleness, GateStickyBootstrap, GateDegradeHonest}
	for _, mode := range modes {
		for _, s := range gateMatrix() {
			cfg := GateConfig{MaxStaleness: time.Hour, BootstrapDone: true}
			got, reason := EvaluateReadinessGate(s, false, mode, cfg)
			if got {
				t.Errorf("%s: proceeded on an unknown status (%+v)", mode, s)
			}
			if reason != DeferStatusUnknown {
				t.Errorf("%s: reason = %q, want %q (%+v)", mode, reason, DeferStatusUnknown, s)
			}
		}
	}
}
