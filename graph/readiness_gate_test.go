package graph

import (
	"testing"
)

// The gate is the single home for readiness semantics (ADR-084 D1), so these tests pin
// the properties every adopter depends on. ADR-084 collapsed four consumer MODES into
// two orthogonal questions; this change deleted the second one, leaving exactly one:
//
//   - HEALTH — fresh status ∧ interpretable state ∧ no hard stop ∧ bootstrapped.
//
// So the pins here are about that question and about the things that must NOT be able
// to influence it:
//
//   - each of the four defer reasons fires on its own condition, and only on it;
//   - an unbootstrapped producer defers even when it looks merely "behind" (the gh#474
//     cutover window, which is the reason the bit is on the wire at all);
//   - view age never gates anything, at any magnitude, in either encoding;
//   - coverage (Ready) neither licenses nor withholds — it is absent from the gate.

// healthy is an envelope with nothing wrong: bootstrapped, no hard stop. Individual
// cases vary only the fields they are about.
func healthy(ready bool, stalenessMs uint64) IndexStatusResponse {
	state := IndexStateBuilding
	if ready {
		state = IndexStateReady
	}
	return IndexStatusResponse{
		Ready: ready, State: state, BootstrapComplete: true,
		TargetRevision: 500, StalenessMs: stalenessMs,
	}
}

func TestEvaluateReadinessGate(t *testing.T) {
	tests := []struct {
		name       string
		reading    StatusReading
		proceed    bool
		wantReason DeferReason
	}{
		{
			name:    "caught up proceeds",
			reading: StatusReading{Status: healthy(true, 0), Fresh: true},
			proceed: true,
		},
		{
			// The inverse of the retired "lag defers under exact" row. Lag on a healthy,
			// bootstrapped index is a property of the ANSWER, not a fault: only "still
			// building" and "broken" justify withholding. Community detection — the last
			// consumer that ever declared a view-age tolerance — is the safest possible
			// reader of a stale view (periodic, idempotent, overwritten next cycle).
			name:    "lag proceeds",
			reading: StatusReading{Status: healthy(false, 200), Fresh: true},
			proceed: true,
		},
		{
			// The inverse of the retired "over the bound defers" row: there is no bound.
			name:    "a minute-old view proceeds",
			reading: StatusReading{Status: healthy(false, 60_000), Fresh: true},
			proceed: true,
		},
		{
			// The inverse of the retired "absent staleness never satisfies a bound" row.
			// StalenessMs == 0 on a not-ready envelope is still the PRESENCE encoding —
			// the producer could not compute an age — but an uncomputable age no longer
			// blocks anything, because nothing consults the age. It is reported, and a
			// consumer that surfaces it carries the encoding with it.
			name:    "an uncomputable view age proceeds",
			reading: StatusReading{Status: healthy(false, 0), Fresh: true},
			proceed: true,
		},
		{
			name: "an unbootstrapped index defers",
			// The gh#474 cutover: State=building with a plausible lag, but the keyset
			// is half-materialised. This is the one "not yet" the gate still enforces,
			// and the reason the bit had to reach the wire.
			reading: StatusReading{
				Status: IndexStatusResponse{State: IndexStateBuilding, TargetRevision: 500, StalenessMs: 10},
				Fresh:  true,
			},
			proceed:    false,
			wantReason: DeferBootstrapIncomplete,
		},
		{
			name: "an authoritatively empty graph proceeds",
			// 0/0 after enumeration is a COMPLETED build (gh#474 Codex #5). The
			// producer encodes it as Ready at TargetRevision 0; a gate that deferred
			// on target==0 would reject every query against a fresh empty deployment
			// forever.
			reading: StatusReading{
				Status: IndexStatusResponse{Ready: true, State: IndexStateReady, BootstrapComplete: true},
				Fresh:  true,
			},
			proceed: true,
		},
		{
			name: "degraded defers",
			reading: StatusReading{
				Status: IndexStatusResponse{State: IndexStateDegraded, BootstrapComplete: true},
				Fresh:  true,
			},
			proceed:    false,
			wantReason: DeferHardStop,
		},
		{
			name: "reset_required defers",
			reading: StatusReading{
				Status: IndexStatusResponse{State: IndexStateResetRequired, BootstrapComplete: true},
				Fresh:  true,
			},
			proceed:    false,
			wantReason: DeferHardStop,
		},
		{
			name:       "an unknown status defers",
			reading:    StatusReading{Status: healthy(true, 0), Fresh: false},
			proceed:    false,
			wantReason: DeferStatusUnknown,
		},
		{
			name: "an uninterpretable state defers",
			reading: StatusReading{
				Status: IndexStatusResponse{State: "rebuilding", BootstrapComplete: true},
				Fresh:  true,
			},
			proceed:    false,
			wantReason: DeferUnrecognizedState,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, reason := EvaluateReadinessGate(tt.reading)
			if got != tt.proceed {
				t.Errorf("proceed = %v, want %v", got, tt.proceed)
			}
			if reason != tt.wantReason {
				t.Errorf("reason = %q, want %q", reason, tt.wantReason)
			}
			if got && reason != DeferNone {
				t.Errorf("a proceed carried defer reason %q", reason)
			}
		})
	}
}

// TestEvaluateReadinessGate_ViewAgeNeverGates is the load-bearing pin of this change,
// and it is the inverse of every row the freshness table used to hold. No StalenessMs —
// zero, one, absurd, or the uint64 ceiling that used to wrap NEGATIVE through
// time.Duration and needed an overflow guard — can change a health verdict, in either
// direction. The arithmetic that guard protected is gone because the comparison is
// gone: there is nothing left to fail open.
//
// This is what "gate on health; REPORT freshness" means mechanically. A stale view
// yields a stale ANSWER carrying its own age (IndexStatusResponse.StalenessMs, stamped
// by consumers onto e.g. staleness_at_detection_ms), never a withheld one.
func TestEvaluateReadinessGate_ViewAgeNeverGates(t *testing.T) {
	ages := []uint64{0, 1, 1_000, 60_000, 86_400_000, 1 << 62, 1<<63 - 1, 1<<64 - 1}

	t.Run("a healthy index proceeds at every view age", func(t *testing.T) {
		for _, ms := range ages {
			proceed, reason := EvaluateReadinessGate(
				StatusReading{Status: healthy(false, ms), Fresh: true})
			if !proceed {
				t.Errorf("StalenessMs=%d deferred as %q; view age must never withhold an answer", ms, reason)
			}
		}
	})

	t.Run("an unhealthy index defers at every view age", func(t *testing.T) {
		// The other direction: a tiny (or absent) age cannot buy a broken index a
		// proceed either. Health is answered without consulting the number at all.
		unhealthy := map[DeferReason]IndexStatusResponse{
			DeferHardStop:            {State: IndexStateDegraded, BootstrapComplete: true},
			DeferBootstrapIncomplete: {State: IndexStateBuilding, TargetRevision: 10},
			DeferUnrecognizedState:   {State: "", BootstrapComplete: true},
		}
		for want, status := range unhealthy {
			for _, ms := range ages {
				status.StalenessMs = ms
				proceed, reason := EvaluateReadinessGate(StatusReading{Status: status, Fresh: true})
				if proceed {
					t.Errorf("%q proceeded at StalenessMs=%d", want, ms)
				}
				if reason != want {
					t.Errorf("StalenessMs=%d gave reason %q, want %q", ms, reason, want)
				}
			}
		}
	})
}

// TestEvaluateReadinessGate_UnknownStatusNeverProceeds proves the fail-closed
// transport rule survives the collapse: a status the consumer cannot vouch for is a
// TRANSPORT fact, evaluated before any index state, so no envelope contents however
// healthy-looking license proceeding on it (gh#590 — a status RTT timing out behind the
// firehose logged identically to a genuine not-ready).
func TestEvaluateReadinessGate_UnknownStatusNeverProceeds(t *testing.T) {
	for _, s := range []IndexStatusResponse{
		{Ready: true, State: IndexStateReady, BootstrapComplete: true},
		{Ready: false, State: IndexStateBuilding, BootstrapComplete: true, StalenessMs: 1},
		{},
	} {
		proceed, reason := EvaluateReadinessGate(StatusReading{Status: s, Fresh: false})
		if proceed {
			t.Errorf("proceeded on an unknown status: %+v", s)
		}
		if reason != DeferStatusUnknown {
			t.Errorf("reason = %q, want %q — an unknown feed must not read as index state",
				reason, DeferStatusUnknown)
		}
	}
}

// TestEvaluateReadinessGate_CoverageNeitherLicensesNorWithholds pins Ready's total
// absence from the gate. Before ADR-084 the two directions were fused into one Ready
// bool, and that is what made ordinary lag look like a correctness problem; the
// freshness parameter then kept half of it alive as an opt-in. Now the bit is inert
// here: flipping it changes NO verdict, healthy or not. A caller that genuinely needs
// read-your-writes compares its own revision against IndexedRevision itself.
func TestEvaluateReadinessGate_CoverageNeitherLicensesNorWithholds(t *testing.T) {
	states := []struct {
		name   string
		status IndexStatusResponse
	}{
		{"healthy", IndexStatusResponse{State: IndexStateBuilding, BootstrapComplete: true, StalenessMs: 9_000}},
		{"degraded", IndexStatusResponse{State: IndexStateDegraded, BootstrapComplete: true}},
		{"unbootstrapped", IndexStatusResponse{State: IndexStateBuilding, TargetRevision: 10}},
		{"unrecognized", IndexStatusResponse{State: "future_state", BootstrapComplete: true}},
	}
	for _, tc := range states {
		t.Run(tc.name, func(t *testing.T) {
			notCovered := tc.status
			notCovered.Ready = false
			covered := tc.status
			covered.Ready = true

			gotNo, reasonNo := EvaluateReadinessGate(StatusReading{Status: notCovered, Fresh: true})
			gotYes, reasonYes := EvaluateReadinessGate(StatusReading{Status: covered, Fresh: true})

			if gotNo != gotYes || reasonNo != reasonYes {
				t.Errorf("Ready changed the verdict: false→(%v,%q) true→(%v,%q); coverage must be inert",
					gotNo, reasonNo, gotYes, reasonYes)
			}
		})
	}
}

// TestEvaluateReadinessGate_UnrecognizedStateFailsClosed pins the allow-list. The gate
// used to check only for degraded/reset_required, so a blank or future State read as
// "not a hard stop, therefore healthy" and proceeded — a fail-OPEN on exactly the field
// ADR-084 made load-bearing when health replaced coverage, and now the ONLY thing
// standing between a garbled envelope and a served answer.
//
// A blank State is reachable without any malice: an empty envelope, a partially written
// key, or a consumer built against a newer producer that added a state.
func TestEvaluateReadinessGate_UnrecognizedStateFailsClosed(t *testing.T) {
	for _, state := range []string{"", "ready_ish", "REBUILDING", "unknown", "Ready"} {
		status := IndexStatusResponse{
			State: state, BootstrapComplete: true, TargetRevision: 100, StalenessMs: 1,
		}
		proceed, reason := EvaluateReadinessGate(StatusReading{Status: status, Fresh: true})
		if proceed {
			t.Errorf("State %q proceeded; an uninterpretable state must fail closed", state)
		}
		if reason != DeferUnrecognizedState {
			t.Errorf("State %q gave reason %q, want %q — the operator action is a version "+
				"check, not a config tweak", state, reason, DeferUnrecognizedState)
		}
	}

	// Every state the producer can actually emit stays interpretable, so the allow-list
	// cannot silently reject the healthy path.
	for _, state := range AllIndexStates {
		_, reason := EvaluateReadinessGate(StatusReading{
			Status: IndexStatusResponse{State: state, BootstrapComplete: true, Ready: true},
			Fresh:  true,
		})
		if reason == DeferUnrecognizedState {
			t.Errorf("declared state %q was rejected as unrecognized", state)
		}
	}
}

// TestEvaluateReadinessGate_HealthOrderIsAttributable pins the evaluation ORDER, which
// is what makes a defer reason an operator instruction rather than a label. Each row
// stacks a further fault on top of the previous one; the reason reported must stay the
// OUTERMOST (most fundamental) fact, because an operator handed "bootstrap_incomplete"
// for a dead status feed goes and watches a build that no one is reporting on.
func TestEvaluateReadinessGate_HealthOrderIsAttributable(t *testing.T) {
	// A maximally broken envelope: dead feed, garbage state, degraded, unbootstrapped.
	worst := IndexStatusResponse{State: "gibberish", BootstrapComplete: false, StalenessMs: 5_000}

	tests := []struct {
		name    string
		reading StatusReading
		want    DeferReason
	}{
		{
			name:    "a dead feed outranks everything in the envelope",
			reading: StatusReading{Status: worst, Fresh: false},
			want:    DeferStatusUnknown,
		},
		{
			name:    "an uninterpretable state outranks the hard stop it might be hiding",
			reading: StatusReading{Status: worst, Fresh: true},
			want:    DeferUnrecognizedState,
		},
		{
			name: "a hard stop outranks an incomplete build",
			reading: StatusReading{
				Status: IndexStatusResponse{State: IndexStateDegraded, BootstrapComplete: false},
				Fresh:  true,
			},
			want: DeferHardStop,
		},
		{
			name: "an incomplete build is reported once nothing more fundamental is wrong",
			reading: StatusReading{
				Status: IndexStatusResponse{State: IndexStateBuilding, BootstrapComplete: false},
				Fresh:  true,
			},
			want: DeferBootstrapIncomplete,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			proceed, reason := EvaluateReadinessGate(tt.reading)
			if proceed {
				t.Fatal("an unhealthy index proceeded")
			}
			if reason != tt.want {
				t.Errorf("reason = %q, want %q", reason, tt.want)
			}
		})
	}
}

// TestDeferReasons_AreTheClosedSet guards the metric label vocabulary. Every reason the
// gate can emit must be one of the four declared constants: the graph-clustering
// defer_total{reason} label set is enumerated from them, and countDefer silently DROPS
// an unrecognized value rather than leak cardinality — so a fifth reason introduced
// without updating that list would go uncounted, which is precisely the invisible-defer
// shape gh#590 cost three cycles on.
func TestDeferReasons_AreTheClosedSet(t *testing.T) {
	// Sourced from AllDeferReasons, NOT a literal — this is the test that proves the
	// canonical list and the gate agree, so hand-copying the names here would let both
	// drift together unnoticed. It stays a real assertion because `seen` below is built
	// by EXECUTING the gate: a reason the gate emits but AllDeferReasons omits fails the
	// first check, and a reason AllDeferReasons declares but no reading reaches fails the
	// second.
	//
	// KNOWN LIMIT, verified by mutation: this cannot catch a new reason returned from a
	// branch no reading below reaches — such a mutant passes, because `seen` only ever
	// contains what the table provokes. The readings are therefore load-bearing, not
	// illustrative: a new gate branch needs a new row here, or its reason is unguarded.
	closed := map[DeferReason]bool{}
	for _, reason := range AllDeferReasons {
		closed[reason] = true
	}

	// A spread of envelopes wide enough to reach every branch, including the
	// combinations a real producer emits under load.
	readings := []StatusReading{
		{Status: healthy(true, 0), Fresh: false},
		{Status: healthy(false, 0), Fresh: true},
		{Status: healthy(false, 1<<63), Fresh: true},
		{Status: IndexStatusResponse{}, Fresh: true},
		{Status: IndexStatusResponse{State: IndexStateDegraded, BootstrapComplete: true}, Fresh: true},
		{Status: IndexStatusResponse{State: IndexStateResetRequired, BootstrapComplete: true}, Fresh: true},
		{Status: IndexStatusResponse{State: IndexStateBuilding}, Fresh: true},
		{Status: IndexStatusResponse{State: "someday"}, Fresh: true},
	}
	seen := map[DeferReason]bool{}
	for _, r := range readings {
		proceed, reason := EvaluateReadinessGate(r)
		if proceed {
			if reason != DeferNone {
				t.Errorf("proceed carried reason %q", reason)
			}
			continue
		}
		if !closed[reason] {
			t.Errorf("gate emitted %q, which is outside the closed reason set — "+
				"defer_total would silently drop it", reason)
		}
		seen[reason] = true
	}
	for reason := range closed {
		if !seen[reason] {
			t.Errorf("no reading produced %q; the table no longer covers every branch", reason)
		}
	}
}

// TestStatusReading_ZeroValueFailsClosed pins the struct's fail-direction. With the
// Freshness parameter gone, StatusReading is the gate's ENTIRE input, so an
// uninitialised one reaching it must withhold rather than serve. Fresh's zero value is
// false, which is the safe direction — this test exists so a future field reordering or
// an inverted "Stale bool" rename cannot flip it silently.
func TestStatusReading_ZeroValueFailsClosed(t *testing.T) {
	proceed, reason := EvaluateReadinessGate(StatusReading{})
	if proceed {
		t.Fatal("the zero-value StatusReading proceeded; an uninitialised reading must fail closed")
	}
	if reason != DeferStatusUnknown {
		t.Errorf("reason = %q, want %q — nothing was ever received", reason, DeferStatusUnknown)
	}

	// And the deliberate in-process shape (a producer computing its own envelope, with
	// no transport to lose) is the ONLY thing that opens it: Fresh must be set
	// explicitly, and the envelope must still pass health on its own merits.
	proceed, _ = EvaluateReadinessGate(StatusReading{Status: healthy(false, 250), Fresh: true})
	if !proceed {
		t.Error("an in-process healthy reading deferred; Fresh: true is the whole opt-in")
	}
}
