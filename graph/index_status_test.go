package graph

import (
	"encoding/json"
	"testing"
	"time"
)

// computeBase is a fixed compute instant so the staleness projection is asserted
// exactly rather than against the wall clock.
var computeBase = time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)

// TestComputeIndexStatus_Staleness pins the age-of-view projection (ADR-083 D3),
// including the presence encoding that keeps an ABSENT staleness from reading as a
// perfectly fresh view in a bounded-staleness gate.
func TestComputeIndexStatus_Staleness(t *testing.T) {
	tests := []struct {
		name string
		in   IndexStatusInputs
		want uint64
	}{
		{
			name: "caught up reports zero staleness",
			in: IndexStatusInputs{
				Indexed: 100, Target: 100,
				IndexedAt: computeBase.Add(-90 * time.Second), Now: computeBase,
			},
			want: 0, // Ready: there is no staleness to report, however old the floor is
		},
		{
			name: "building reports the age of the covered floor",
			in: IndexStatusInputs{
				Indexed: 40, Target: 100,
				IndexedAt: computeBase.Add(-1500 * time.Millisecond), Now: computeBase,
			},
			want: 1500,
		},
		{
			name: "unknown floor commit time stays absent, never zero-as-fresh",
			in: IndexStatusInputs{
				Indexed: 40, Target: 100,
				IndexedAt: time.Time{}, Now: computeBase,
			},
			want: 0,
		},
		{
			name: "sub-millisecond age is clamped to the 1ms presence floor",
			in: IndexStatusInputs{
				Indexed: 40, Target: 100,
				IndexedAt: computeBase.Add(-200 * time.Microsecond), Now: computeBase,
			},
			want: 1,
		},
		{
			name: "a floor committed in the future (clock skew) still reports 1ms, not absent",
			in: IndexStatusInputs{
				Indexed: 40, Target: 100,
				IndexedAt: computeBase.Add(2 * time.Second), Now: computeBase,
			},
			want: 1,
		},
		{
			name: "degraded still carries the age of what it has covered",
			in: IndexStatusInputs{
				Indexed: 40, Target: 100, Stuck: true,
				IndexedAt: computeBase.Add(-45 * time.Second), Now: computeBase,
			},
			want: 45000,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ComputeIndexStatus(tt.in).StalenessMs; got != tt.want {
				t.Errorf("StalenessMs = %d, want %d", got, tt.want)
			}
		})
	}
}

// TestComputeIndexStatus_PreExistingFieldsUnchanged is the value-compatibility guard
// from the spec: adding staleness must not move Ready/State/Lag/Revision for any
// pre-existing input.
func TestComputeIndexStatus_PreExistingFieldsUnchanged(t *testing.T) {
	tests := []struct {
		name              string
		in                IndexStatusInputs
		wantReady         bool
		wantState         string
		wantLag           uint64
		wantRevisionField string
	}{
		{
			name:      "empty graph is not ready",
			in:        IndexStatusInputs{Indexed: 0, Target: 0},
			wantState: IndexStateBuilding,
		},
		{
			name:              "caught up is ready",
			in:                IndexStatusInputs{Indexed: 100, Target: 100},
			wantReady:         true,
			wantState:         IndexStateReady,
			wantRevisionField: "100",
		},
		{
			name:              "behind target is building with lag",
			in:                IndexStatusInputs{Indexed: 40, Target: 100},
			wantState:         IndexStateBuilding,
			wantLag:           60,
			wantRevisionField: "40",
		},
		{
			name:              "stuck and behind is degraded",
			in:                IndexStatusInputs{Indexed: 40, Target: 100, Stuck: true},
			wantState:         IndexStateDegraded,
			wantLag:           60,
			wantRevisionField: "40",
		},
		{
			name:              "ready wins over stuck",
			in:                IndexStatusInputs{Indexed: 100, Target: 100, Stuck: true},
			wantReady:         true,
			wantState:         IndexStateReady,
			wantRevisionField: "100",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ComputeIndexStatus(tt.in)
			if got.Ready != tt.wantReady || got.State != tt.wantState ||
				got.Lag != tt.wantLag || got.Revision != tt.wantRevisionField {
				t.Errorf("got Ready=%v State=%q Lag=%d Revision=%q; want %v/%q/%d/%q",
					got.Ready, got.State, got.Lag, got.Revision,
					tt.wantReady, tt.wantState, tt.wantLag, tt.wantRevisionField)
			}
		})
	}
}

// TestIndexStatusResponse_StalenessWireRoundTrip proves the additive field survives
// the wire under the exact key consumers decode (`staleness_ms`), and that it is
// omitted — not emitted as 0 — when absent. The graph-status KV value IS this JSON,
// so the encoder/decoder pair here is the production one (plain encoding/json, no
// payload-registry envelope: readiness is operational KV state, not a published
// message payload).
func TestIndexStatusResponse_StalenessWireRoundTrip(t *testing.T) {
	src := ComputeIndexStatus(IndexStatusInputs{
		Indexed: 40, Target: 100,
		IndexedAt: computeBase.Add(-2500 * time.Millisecond), Now: computeBase,
		LastSynced: "2026-07-20T12:00:00Z",
	})
	raw, err := json.Marshal(src)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var keyed map[string]any
	if err := json.Unmarshal(raw, &keyed); err != nil {
		t.Fatalf("unmarshal to map: %v", err)
	}
	if got, ok := keyed["staleness_ms"]; !ok || got != float64(2500) {
		t.Fatalf("wire key staleness_ms = %v (present=%v), want 2500", got, ok)
	}

	var back IndexStatusResponse
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back != src {
		t.Errorf("round trip changed the envelope:\n got %+v\nwant %+v", back, src)
	}

	// Ready envelopes omit the key entirely, so an old decoder sees exactly what it
	// saw before and a new one cannot mistake an absent value for a present zero.
	readyRaw, err := json.Marshal(ComputeIndexStatus(IndexStatusInputs{Indexed: 100, Target: 100}))
	if err != nil {
		t.Fatalf("marshal ready: %v", err)
	}
	var readyKeyed map[string]any
	if err := json.Unmarshal(readyRaw, &readyKeyed); err != nil {
		t.Fatalf("unmarshal ready: %v", err)
	}
	if _, present := readyKeyed["staleness_ms"]; present {
		t.Errorf("ready envelope emitted staleness_ms: %s", readyRaw)
	}
}
