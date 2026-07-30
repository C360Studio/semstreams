package graph

import (
	"encoding/json"
	"testing"
	"time"
)

// TestComputeBacklogStatus_ReadyAndState pins the backlog projection's two verdicts.
//
// The 0/0 row is the reason this projection exists at all: ComputeIndexStatus computes
// Ready = target > 0 && indexed >= target, which is FALSE with nothing to do — the
// steady state of an idle backlog producer.
func TestComputeBacklogStatus_ReadyAndState(t *testing.T) {
	tests := []struct {
		name      string
		in        BacklogStatusInputs
		wantReady bool
		wantState string
	}{
		{
			name:      "nothing outstanding and bootstrapped is ready",
			in:        BacklogStatusInputs{Outstanding: 0, BootstrapComplete: true},
			wantReady: true,
			wantState: IndexStateReady,
		},
		{
			name:      "outstanding work is not ready",
			in:        BacklogStatusInputs{Outstanding: 7, BootstrapComplete: true},
			wantReady: false,
			wantState: IndexStateBuilding,
		},
		{
			name:      "drained but not bootstrapped is not ready",
			in:        BacklogStatusInputs{Outstanding: 0, BootstrapComplete: false},
			wantReady: false,
			wantState: IndexStateBuilding,
		},
		{
			name: "observation failure degrades even when drained and bootstrapped",
			in: BacklogStatusInputs{
				Outstanding: 0, BootstrapComplete: true, ObservationFailed: true,
			},
			wantReady: false,
			wantState: IndexStateDegraded,
		},
		{
			name: "observation failure degrades rather than reporting building",
			in: BacklogStatusInputs{
				Outstanding: 42, BootstrapComplete: false, ObservationFailed: true,
			},
			wantReady: false,
			wantState: IndexStateDegraded,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ComputeBacklogStatus(tt.in)
			if got.Ready != tt.wantReady {
				t.Errorf("Ready = %v, want %v", got.Ready, tt.wantReady)
			}
			if got.State != tt.wantState {
				t.Errorf("State = %q, want %q", got.State, tt.wantState)
			}
		})
	}
}

// TestComputeBacklogStatus_ObservationFailureCannotReportReady is the fail-closed
// guard stated separately from the table because it is the one combination where a
// plausible reordering (ready wins, then check faults) silently reports a caught-up
// producer that cannot see its own backlog.
func TestComputeBacklogStatus_ObservationFailureCannotReportReady(t *testing.T) {
	got := ComputeBacklogStatus(BacklogStatusInputs{
		Outstanding: 0, BootstrapComplete: true, ObservationFailed: true,
	})
	if got.State != IndexStateDegraded {
		t.Fatalf("State = %q, want %q — a producer that cannot read its own backlog is faulted",
			got.State, IndexStateDegraded)
	}
	proceed, reason := EvaluateReadinessGate(StatusReading{Status: got, Fresh: true})
	if proceed {
		t.Fatalf("gate proceeded on a degraded backlog envelope (reason=%v)", reason)
	}
}

// TestComputeBacklogStatus_LagIsOutstandingMessages pins Lag's unit. It is MESSAGES
// here, not revisions — a different unit on the same field from the revision-lag
// producers, which the spec states explicitly.
func TestComputeBacklogStatus_LagIsOutstandingMessages(t *testing.T) {
	// 2048 is the in-process lane-queue ceiling (8 lanes x 256 depth). Those messages
	// are delivered-but-unacked, so they live in NumAckPending and are invisible to
	// NumPending alone — the under-report gh#712 tripped over.
	got := ComputeBacklogStatus(BacklogStatusInputs{Outstanding: 2048, BootstrapComplete: true})
	if got.Lag != 2048 {
		t.Errorf("Lag = %d, want 2048", got.Lag)
	}
	if got.Ready {
		t.Error("Ready = true with 2048 messages outstanding")
	}
}

// TestComputeBacklogStatus_RevisionFieldsAbsentOnWire is task 3.6's guard, asserted
// at the projection.
//
// IndexedRevision/TargetRevision are contractually in the ENTITY_STATES KV revision
// space (ADR-084 D3 pins them comparable to a caller's kv_revision by a test). A
// backlog producer consumes multiple streams with independent sequence spaces, so it
// has no such scalar; writing a stream sequence there would silently corrupt every
// read-your-writes check in the system. Asserted on the WIRE, not just the struct,
// because omitempty is what keeps them off it.
func TestComputeBacklogStatus_RevisionFieldsAbsentOnWire(t *testing.T) {
	got := ComputeBacklogStatus(BacklogStatusInputs{
		Outstanding: 5, BootstrapComplete: true, BootstrapScope: 900,
		OldestOutstandingAt: computeBase.Add(-3 * time.Second), Now: computeBase,
	})
	if got.IndexedRevision != 0 || got.TargetRevision != 0 {
		t.Fatalf("projection set revision fields: indexed=%d target=%d",
			got.IndexedRevision, got.TargetRevision)
	}

	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var wire map[string]any
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, banned := range []string{"indexed_revision", "target_revision", "revision"} {
		if _, present := wire[banned]; present {
			t.Errorf("%q present on a backlog envelope: %s", banned, raw)
		}
	}
}

// TestComputeBacklogStatus_Staleness pins the age-of-oldest-outstanding projection,
// including the presence encoding shared with the revision-lag producers: 0 means "no
// information", never "zero staleness".
func TestComputeBacklogStatus_Staleness(t *testing.T) {
	tests := []struct {
		name string
		in   BacklogStatusInputs
		want uint64
	}{
		{
			name: "caught up reports no staleness",
			in: BacklogStatusInputs{
				Outstanding: 0, BootstrapComplete: true,
				OldestOutstandingAt: computeBase.Add(-90 * time.Second), Now: computeBase,
			},
			want: 0,
		},
		{
			name: "backlog reports the age of the oldest outstanding message",
			in: BacklogStatusInputs{
				Outstanding: 3, BootstrapComplete: true,
				OldestOutstandingAt: computeBase.Add(-2 * time.Second), Now: computeBase,
			},
			want: 2000,
		},
		{
			name: "unknown oldest-outstanding reports no information, not fresh",
			in: BacklogStatusInputs{
				Outstanding: 3, BootstrapComplete: true, Now: computeBase,
			},
			want: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ComputeBacklogStatus(tt.in).StalenessMs; got != tt.want {
				t.Errorf("StalenessMs = %d, want %d", got, tt.want)
			}
		})
	}
}

// TestComputeBacklogStatus_ReadyImpliesBootstrapComplete pins the gate's PRODUCER
// INVARIANT (documented on EvaluateReadinessGate) for this producer shape. A producer
// publishing Ready while reporting an unfinished build makes every consumer defer
// forever, pointing operators at a cutover that already finished.
func TestComputeBacklogStatus_ReadyImpliesBootstrapComplete(t *testing.T) {
	for _, outstanding := range []uint64{0, 1, 2048} {
		for _, bootstrapped := range []bool{false, true} {
			got := ComputeBacklogStatus(BacklogStatusInputs{
				Outstanding: outstanding, BootstrapComplete: bootstrapped,
			})
			if got.Ready && !got.BootstrapComplete {
				t.Errorf("outstanding=%d bootstrapped=%v: Ready without BootstrapComplete",
					outstanding, bootstrapped)
			}
		}
	}
}

// TestBootstrapScope_GateVerdictInvariant is task 2.3 — the guard that stops
// BootstrapScope from becoming a threshold knob.
//
// The field exists so a consumer can distinguish "finished an EMPTY initial build"
// from "finished a build that had work in it". That distinction is for the consumer's
// own logic; the moment the readiness GATE branches on the magnitude, readiness stops
// being a health question and becomes a tunable bound — which is exactly what ADR-085
// deleted max_staleness to prevent. Two envelopes differing ONLY in this field must
// produce byte-identical verdicts.
func TestBootstrapScope_GateVerdictInvariant(t *testing.T) {
	scopes := []uint64{0, 1, 42, 2048, 1 << 40}

	// Sweep the states the gate actually branches on, so the invariant is proven
	// across every verdict rather than only on the happy path.
	bases := []struct {
		name   string
		status IndexStatusResponse
		fresh  bool
	}{
		{
			name:   "ready",
			status: IndexStatusResponse{State: IndexStateReady, Ready: true, BootstrapComplete: true},
			fresh:  true,
		},
		{
			name:   "building",
			status: IndexStatusResponse{State: IndexStateBuilding, BootstrapComplete: true},
			fresh:  true,
		},
		{
			name:   "degraded",
			status: IndexStatusResponse{State: IndexStateDegraded, BootstrapComplete: true},
			fresh:  true,
		},
		{
			name:   "bootstrap incomplete",
			status: IndexStatusResponse{State: IndexStateReady, BootstrapComplete: false},
			fresh:  true,
		},
		{
			name:   "stale reading",
			status: IndexStatusResponse{State: IndexStateReady, BootstrapComplete: true},
			fresh:  false,
		},
	}

	for _, base := range bases {
		t.Run(base.name, func(t *testing.T) {
			baseline := base.status
			baseline.BootstrapScope = 0
			wantProceed, wantReason := EvaluateReadinessGate(
				StatusReading{Status: baseline, Fresh: base.fresh})

			for _, scope := range scopes {
				variant := base.status
				variant.BootstrapScope = scope
				proceed, reason := EvaluateReadinessGate(
					StatusReading{Status: variant, Fresh: base.fresh})
				if proceed != wantProceed || reason != wantReason {
					t.Errorf("BootstrapScope=%d changed the verdict: got (%v, %v), want (%v, %v)"+
						" — the gate must never read this field",
						scope, proceed, reason, wantProceed, wantReason)
				}
			}
		})
	}
}

// TestBootstrapScope_WireRoundTrip pins the encoding, including the omitempty that
// keeps the field off every existing producer's envelope (this change is additive and
// must not move a byte of graph-index's output).
func TestBootstrapScope_WireRoundTrip(t *testing.T) {
	t.Run("absent when zero", func(t *testing.T) {
		raw, err := json.Marshal(IndexStatusResponse{State: IndexStateReady})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var wire map[string]any
		if err := json.Unmarshal(raw, &wire); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if _, present := wire["bootstrap_scope"]; present {
			t.Errorf("bootstrap_scope present at zero: %s", raw)
		}
	})

	t.Run("survives a round trip", func(t *testing.T) {
		want := IndexStatusResponse{State: IndexStateReady, BootstrapScope: 12345}
		raw, err := json.Marshal(want)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var got IndexStatusResponse
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if got.BootstrapScope != want.BootstrapScope {
			t.Errorf("BootstrapScope = %d, want %d", got.BootstrapScope, want.BootstrapScope)
		}
	})

	t.Run("authoritatively-nothing-to-do is distinguishable", func(t *testing.T) {
		// complete && scope == 0 is the distinction the field exists for. Because
		// scope is omitempty, an ABSENT scope on a complete envelope reads as 0 —
		// which is the same claim. That is intended: the pre-existing producers
		// (graph-index, graph-embedding) do not report scope, and a consumer must not
		// read their silence as "there was work".
		var complete IndexStatusResponse
		raw := []byte(`{"state":"ready","bootstrap_complete":true}`)
		if err := json.Unmarshal(raw, &complete); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if !complete.BootstrapComplete || complete.BootstrapScope != 0 {
			t.Fatalf("got complete=%v scope=%d, want true/0",
				complete.BootstrapComplete, complete.BootstrapScope)
		}
	})
}
