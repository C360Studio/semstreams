package graph

import (
	"encoding/json"
	"reflect"
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

// TestComputeIndexStatus_FailedCountDegrades is the #613 statement: FailedCount>0
// projects to State=degraded BEFORE "ready wins", so a producer caught up over
// failures (Indexed>=Target) reports degraded while Ready stays coverage-accurate.
// It also pins graph-index parity: FailedCount==0 is byte-identical to the prior
// projection.
func TestComputeIndexStatus_FailedCountDegrades(t *testing.T) {
	t.Run("caught up with failures is degraded, Ready still true", func(t *testing.T) {
		got := ComputeIndexStatus(IndexStatusInputs{Indexed: 100, Target: 100, FailedCount: 3})
		if got.State != IndexStateDegraded {
			t.Errorf("State = %q, want %q: FailedCount>0 must win over the ready branch (#613)", got.State, IndexStateDegraded)
		}
		if !got.Ready {
			t.Error("Ready = false, want true: coverage is complete, only health is degraded (#613)")
		}
		if got.FailedCount != 3 {
			t.Errorf("FailedCount = %d, want 3: the input must echo to the envelope", got.FailedCount)
		}
	})

	t.Run("failed with small lag is degraded, not building", func(t *testing.T) {
		got := ComputeIndexStatus(IndexStatusInputs{Indexed: 99, Target: 100, FailedCount: 1})
		if got.State != IndexStateDegraded {
			t.Errorf("State = %q, want %q: a failed entry defers regardless of lag", got.State, IndexStateDegraded)
		}
		if got.Ready {
			t.Error("Ready = true, want false: not caught up")
		}
	})

	t.Run("FailedCount==0 is byte-identical to today (graph-index parity)", func(t *testing.T) {
		// Every pre-existing input shape, computed with the field left at its zero value,
		// must marshal identically to a run that never knew the field existed.
		for _, in := range []IndexStatusInputs{
			{Indexed: 0, Target: 0},
			{Indexed: 100, Target: 100},
			{Indexed: 40, Target: 100},
			{Indexed: 40, Target: 100, Stuck: true},
			{Indexed: 100, Target: 100, Stuck: true},
		} {
			got := ComputeIndexStatus(in)
			raw, err := json.Marshal(got)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			// failed_count must be OMITTED (not emitted as 0) when there are no failures,
			// so an envelope from a producer that never fails is wire-unchanged.
			var decoded map[string]any
			if err := json.Unmarshal(raw, &decoded); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if _, present := decoded["failed_count"]; present {
				t.Errorf("failed_count present in %s: it must be omitted when zero (graph-index parity)", raw)
			}
			if _, present := decoded["failed_reasons"]; present {
				t.Errorf("failed_reasons present in %s: it must be omitted when empty", raw)
			}
			if _, present := decoded["first_failure_at"]; present {
				t.Errorf("first_failure_at present in %s: it must be omitted when empty", raw)
			}
		}
	})
}

// TestIndexStatusResponse_FailedDetailWireRoundTrip proves the three additive
// failure-detail fields survive the production JSON encode/decode under the exact
// keys consumers read, and are omitted when zero (wire compatibility). The
// graph-status KV value IS this JSON, so this is the production codec.
func TestIndexStatusResponse_FailedDetailWireRoundTrip(t *testing.T) {
	src := IndexStatusResponse{
		Ready: true, State: IndexStateDegraded,
		FailedCount:    5,
		FailedReasons:  map[string]uint64{"connection_refused": 4, "content_error": 1},
		FirstFailureAt: "2026-07-22T10:00:00Z",
	}
	raw, err := json.Marshal(src)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got IndexStatusResponse
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.FailedCount != 5 || got.FirstFailureAt != "2026-07-22T10:00:00Z" {
		t.Errorf("failure detail lost: FailedCount=%d FirstFailureAt=%q", got.FailedCount, got.FirstFailureAt)
	}
	if got.FailedReasons["connection_refused"] != 4 || got.FailedReasons["content_error"] != 1 {
		t.Errorf("FailedReasons lost: %v", got.FailedReasons)
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
	if !reflect.DeepEqual(back, src) {
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

// TestIndexStatusResponse_BootstrapCompleteWireRoundTrip pins the ADR-084 D2 bit on
// the wire. Unlike staleness_ms, this field is deliberately NOT omitempty: it gates
// health, so an explicit `false` must be distinguishable in a `nats kv get` dump from
// a pre-ADR-084 producer that never emits the key at all. Both decode to false — fail
// closed either way — but only one of them is a bug an operator can fix by upgrading.
func TestIndexStatusResponse_BootstrapCompleteWireRoundTrip(t *testing.T) {
	// The shared projection never sets the bit; producers stamp it from their own
	// latch. Pin that, so a future edit that folds a guess into ComputeIndexStatus
	// (where no bootstrap fact is in scope) fails here.
	if ComputeIndexStatus(IndexStatusInputs{Indexed: 100, Target: 100}).BootstrapComplete {
		t.Error("ComputeIndexStatus invented a bootstrap verdict it has no input for")
	}

	for _, bootstrapped := range []bool{false, true} {
		src := ComputeIndexStatus(IndexStatusInputs{Indexed: 40, Target: 100, LastSynced: "ts"})
		src.BootstrapComplete = bootstrapped

		raw, err := json.Marshal(src)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var keyed map[string]any
		if err := json.Unmarshal(raw, &keyed); err != nil {
			t.Fatalf("unmarshal to map: %v", err)
		}
		got, present := keyed["bootstrap_complete"]
		if !present {
			t.Fatalf("bootstrap_complete absent from the wire for %v: %s", bootstrapped, raw)
		}
		if got != bootstrapped {
			t.Errorf("wire bootstrap_complete = %v, want %v", got, bootstrapped)
		}

		var back IndexStatusResponse
		if err := json.Unmarshal(raw, &back); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if !reflect.DeepEqual(back, src) {
			t.Errorf("round trip changed the envelope:\n got %+v\nwant %+v", back, src)
		}
	}

	// The migration contract: an envelope from a producer that predates the field
	// decodes to false, so every health gate fails closed until the lockstep upgrade.
	var legacy IndexStatusResponse
	if err := json.Unmarshal([]byte(`{"ready":true,"state":"ready"}`), &legacy); err != nil {
		t.Fatalf("unmarshal legacy: %v", err)
	}
	if legacy.BootstrapComplete {
		t.Error("absent bootstrap_complete decoded true; the health gate would fail OPEN on an old producer")
	}
}
