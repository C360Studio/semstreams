package graph

import (
	"math"
	"testing"
	"time"
)

// The gate is the single home for readiness semantics (ADR-084 D1), so these tests pin
// the properties every adopter depends on. ADR-084 collapsed four consumer MODES into
// two orthogonal questions — health (fresh ∧ no hard stop ∧ bootstrapped) and a
// declared freshness requirement — so the pins here are about the questions, not about
// per-consumer policies:
//
//   - health defers regardless of what freshness the consumer declared;
//   - an unbootstrapped producer defers even when it looks merely "behind" (the gh#474
//     cutover window, which is the reason the bit is on the wire at all);
//   - coverage (Ready) may license a proceed but never causes a defer on its own;
//   - the staleness presence bit is honored, and the reading's own age counts against
//     the bound.

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
		want       Freshness
		proceed    bool
		wantReason DeferReason
	}{
		{
			name:    "caught up proceeds under exact",
			reading: StatusReading{Status: healthy(true, 0), Fresh: true},
			want:    FreshnessExact(),
			proceed: true,
		},
		{
			name: "lag defers under exact",
			// Exact is the view-rate contract (clustering's unset/0 max_staleness),
			// unchanged by ADR-084: this consumer asked for caught-up and is not.
			reading:    StatusReading{Status: healthy(false, 200), Fresh: true},
			want:       FreshnessExact(),
			proceed:    false,
			wantReason: DeferOverStaleness,
		},
		{
			name: "lag proceeds when no freshness is required",
			// The ADR-084 reversal: a read path asks a HEALTH question. Ordinary lag
			// on a healthy, bootstrapped index is not a reason to withhold evidence.
			reading: StatusReading{Status: healthy(false, 60_000), Fresh: true},
			want:    FreshnessNone(),
			proceed: true,
		},
		{
			name:    "within the bound proceeds",
			reading: StatusReading{Status: healthy(false, 2_000), Fresh: true},
			want:    FreshnessWithin(3 * time.Second),
			proceed: true,
		},
		{
			name:       "over the bound defers",
			reading:    StatusReading{Status: healthy(false, 9_000), Fresh: true},
			want:       FreshnessWithin(3 * time.Second),
			proceed:    false,
			wantReason: DeferOverStaleness,
		},
		{
			name: "the reading's own age counts against the bound",
			// The heartbeat-window leak: a 2.5s-stale envelope received 2s ago
			// describes a view that is now ~4.5s old. Judging the envelope's number
			// alone would let a 3s bound pass a 4.5s-old view once per heartbeat.
			reading: StatusReading{Status: healthy(false, 2_500), Fresh: true, Age: 2 * time.Second},
			want:    FreshnessWithin(3 * time.Second),
			proceed: false, wantReason: DeferOverStaleness,
		},
		{
			name: "absent staleness never satisfies a bound",
			// StalenessMs == 0 on a not-ready envelope is the PRESENCE encoding: the
			// producer could not compute an age. Reading it as "0ms stale" would
			// proceed on an unknown view — the inverse of what the bound is for.
			reading:    StatusReading{Status: healthy(false, 0), Fresh: true},
			want:       FreshnessWithin(time.Hour),
			proceed:    false,
			wantReason: DeferOverStaleness,
		},
		{
			name: "an unbootstrapped index defers even under no freshness",
			// The gh#474 cutover: State=building with a plausible lag, but the keyset
			// is half-materialised. Health is the one question freshness cannot
			// override, which is why this bit had to reach the wire.
			reading: StatusReading{
				Status: IndexStatusResponse{State: IndexStateBuilding, TargetRevision: 500, StalenessMs: 10},
				Fresh:  true,
			},
			want:       FreshnessNone(),
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
			want:    FreshnessExact(),
			proceed: true,
		},
		{
			name: "degraded defers under no freshness",
			reading: StatusReading{
				Status: IndexStatusResponse{State: IndexStateDegraded, BootstrapComplete: true},
				Fresh:  true,
			},
			want:       FreshnessNone(),
			proceed:    false,
			wantReason: DeferHardStop,
		},
		{
			name: "reset_required defers under no freshness",
			reading: StatusReading{
				Status: IndexStatusResponse{State: IndexStateResetRequired, BootstrapComplete: true},
				Fresh:  true,
			},
			want:       FreshnessNone(),
			proceed:    false,
			wantReason: DeferHardStop,
		},
		{
			name:       "an unknown status defers under no freshness",
			reading:    StatusReading{Status: healthy(true, 0), Fresh: false},
			want:       FreshnessNone(),
			proceed:    false,
			wantReason: DeferStatusUnknown,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, reason := EvaluateReadinessGate(tt.reading, tt.want)
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

// TestEvaluateReadinessGate_HealthDefersUnderEveryFreshness is the ADR-084 D1
// invariant that keeps freshness from being a severity dial: the health question is
// answered first, and no freshness declaration — not even "none" — can talk past it.
func TestEvaluateReadinessGate_HealthDefersUnderEveryFreshness(t *testing.T) {
	unhealthy := map[string]IndexStatusResponse{
		"degraded":          {State: IndexStateDegraded, BootstrapComplete: true, Ready: false},
		"reset_required":    {State: IndexStateResetRequired, BootstrapComplete: true},
		"not bootstrapped":  {State: IndexStateBuilding, TargetRevision: 10, StalenessMs: 1},
		"degraded pre-boot": {State: IndexStateDegraded},
	}
	freshnesses := map[string]Freshness{
		"exact":          FreshnessExact(),
		"bounded":        FreshnessWithin(time.Hour),
		"none":           FreshnessNone(),
		"zero value":     {},
		"nonpositive":    FreshnessWithin(-time.Second),
		"huge tolerance": FreshnessWithin(1000 * time.Hour),
	}
	for statusName, status := range unhealthy {
		for freshName, want := range freshnesses {
			t.Run(statusName+"/"+freshName, func(t *testing.T) {
				proceed, reason := EvaluateReadinessGate(
					StatusReading{Status: status, Fresh: true}, want)
				if proceed {
					t.Errorf("an unhealthy index proceeded under %s freshness", freshName)
				}
				if reason == DeferOverStaleness {
					t.Errorf("health defer misattributed as %q — an operator would tune a "+
						"tolerance instead of fixing the index", reason)
				}
			})
		}
	}
}

// TestEvaluateReadinessGate_UnknownStatusNeverProceeds proves the fail-closed
// transport rule survives the collapse: a status the consumer cannot vouch for is a
// TRANSPORT fact, evaluated before any index state, so no tolerance however generous
// licenses proceeding on it (gh#590 — a status RTT timing out behind the firehose
// logged identically to a genuine not-ready).
func TestEvaluateReadinessGate_UnknownStatusNeverProceeds(t *testing.T) {
	freshnesses := []Freshness{FreshnessExact(), FreshnessWithin(time.Hour), FreshnessNone()}
	for _, want := range freshnesses {
		for _, s := range []IndexStatusResponse{
			{Ready: true, State: IndexStateReady, BootstrapComplete: true},
			{Ready: false, State: IndexStateBuilding, BootstrapComplete: true, StalenessMs: 1},
			{},
		} {
			proceed, reason := EvaluateReadinessGate(StatusReading{Status: s, Fresh: false}, want)
			if proceed {
				t.Errorf("proceeded on an unknown status: %+v", s)
			}
			if reason != DeferStatusUnknown {
				t.Errorf("reason = %q, want %q — an unknown feed must not read as index state",
					reason, DeferStatusUnknown)
			}
		}
	}
}

// TestEvaluateReadinessGate_CoverageNeverDefers pins the review-driven half of D1:
// coverage may LICENSE a proceed (a caught-up view answers any freshness requirement
// with zero age, which is what keeps the staleness presence encoding sound) but is
// never itself a reason to withhold. Before ADR-084 the two directions were fused into
// one Ready bool, and that is what made ordinary lag look like a correctness problem.
func TestEvaluateReadinessGate_CoverageNeverDefers(t *testing.T) {
	for _, want := range []Freshness{FreshnessExact(), FreshnessWithin(time.Millisecond), FreshnessNone()} {
		// Caught up: no staleness is reported (presence encoding), and every
		// freshness declaration is satisfied — including a bound so tight that the
		// staleness comparison alone would have rejected it.
		proceed, reason := EvaluateReadinessGate(
			StatusReading{Status: healthy(true, 0), Fresh: true, Age: time.Hour}, want)
		if !proceed {
			t.Errorf("a caught-up healthy index deferred as %q", reason)
		}
	}
}

// TestFreshness_ZeroValueIsStrictest pins the constructor contract. The zero value must
// be EXACT, never "none": a Freshness reaching the gate through an uninitialised struct
// field must fail toward withholding, and the shipped clustering operator contract
// ("empty or 0 max_staleness = require exact index catch-up") depends on it — a silent
// inversion there would loosen the strictest gate in the system by doing nothing.
func TestFreshness_ZeroValueIsStrictest(t *testing.T) {
	behind := StatusReading{Status: healthy(false, 5), Fresh: true}

	if proceed, _ := EvaluateReadinessGate(behind, Freshness{}); proceed {
		t.Error("the zero-value Freshness proceeded on a lagging index; it must mean exact")
	}
	if proceed, _ := EvaluateReadinessGate(behind, FreshnessExact()); proceed {
		t.Error("FreshnessExact proceeded on a lagging index")
	}
	// An operator who writes 0 (or omits the field) gets exact, not unbounded.
	for _, d := range []time.Duration{0, -time.Second} {
		if proceed, _ := EvaluateReadinessGate(behind, FreshnessWithin(d)); proceed {
			t.Errorf("FreshnessWithin(%s) proceeded; a non-positive bound must mean exact", d)
		}
	}
	if proceed, _ := EvaluateReadinessGate(behind, FreshnessNone()); !proceed {
		t.Error("FreshnessNone deferred on a healthy lagging index; none must mean no freshness requirement")
	}
}

// TestEvaluateReadinessGate_UnrecognizedStateFailsClosed pins the allow-list. The gate
// used to check only for degraded/reset_required, so a blank or future State read as
// "not a hard stop, therefore healthy" and proceeded — a fail-OPEN on exactly the field
// ADR-084 made load-bearing when health replaced coverage.
//
// A blank State is reachable without any malice: an empty envelope, a partially written
// key, or a consumer built against a newer producer that added a state.
func TestEvaluateReadinessGate_UnrecognizedStateFailsClosed(t *testing.T) {
	for _, state := range []string{"", "ready_ish", "REBUILDING", "unknown", "Ready"} {
		status := IndexStatusResponse{
			State: state, BootstrapComplete: true, TargetRevision: 100, StalenessMs: 1,
		}
		for _, want := range []Freshness{FreshnessExact(), FreshnessWithin(time.Hour), FreshnessNone()} {
			proceed, reason := EvaluateReadinessGate(
				StatusReading{Status: status, Fresh: true}, want)
			if proceed {
				t.Errorf("State %q proceeded; an uninterpretable state must fail closed", state)
			}
			if reason != DeferUnrecognizedState {
				t.Errorf("State %q gave reason %q, want %q — the operator action is a version "+
					"check, not a tolerance tweak", state, reason, DeferUnrecognizedState)
			}
		}
	}

	// Every state the producer can actually emit stays interpretable, so the allow-list
	// cannot silently reject the healthy path.
	for _, state := range AllIndexStates {
		_, reason := EvaluateReadinessGate(StatusReading{
			Status: IndexStatusResponse{State: state, BootstrapComplete: true, Ready: true},
			Fresh:  true,
		}, FreshnessNone())
		if reason == DeferUnrecognizedState {
			t.Errorf("declared state %q was rejected as unrecognized", state)
		}
	}
}

// TestEvaluateReadinessGate_StalenessArithmeticCannotFailOpen pins the overflow guards.
// StalenessMs is a uint64 off the wire and time.Duration is int64 NANOseconds, so a
// large value wraps NEGATIVE and sails under any positive bound — MaxUint64 converts to
// -1ms. A backwards local clock does the same thing from the other side, subtracting
// from the age. Both directions are fail-open, which is the one direction this gate is
// not allowed to fail in.
func TestEvaluateReadinessGate_StalenessArithmeticCannotFailOpen(t *testing.T) {
	bound := FreshnessWithin(3 * time.Second)

	t.Run("an out-of-range staleness defers", func(t *testing.T) {
		for _, ms := range []uint64{math.MaxUint64, math.MaxUint64 - 1, math.MaxInt64} {
			proceed, reason := EvaluateReadinessGate(StatusReading{
				Status: healthy(false, ms), Fresh: true,
			}, bound)
			if proceed {
				t.Errorf("StalenessMs=%d proceeded under a 3s bound — the conversion wrapped negative", ms)
			}
			if reason != DeferOverStaleness {
				t.Errorf("StalenessMs=%d gave %q, want over_staleness", ms, reason)
			}
		}
	})

	t.Run("a backwards local clock cannot credit freshness", func(t *testing.T) {
		// The envelope says the view is 10s old; the local clock claims the reading
		// arrived "in the future". Crediting that would make a 10s-old view pass a 3s
		// bound.
		proceed, _ := EvaluateReadinessGate(StatusReading{
			Status: healthy(false, 10_000), Fresh: true, Age: -30 * time.Second,
		}, bound)
		if proceed {
			t.Error("a negative reading age subtracted from the view age and passed the bound")
		}
	})

	t.Run("a negative age does not reject an otherwise fresh view", func(t *testing.T) {
		// Dropping the local contribution is the conservative reading, not a blanket
		// defer: the envelope's own staleness still decides.
		proceed, _ := EvaluateReadinessGate(StatusReading{
			Status: healthy(false, 500), Fresh: true, Age: -time.Second,
		}, bound)
		if !proceed {
			t.Error("a 500ms-stale view was rejected because the local clock stepped back")
		}
	})

	t.Run("the largest safe staleness still evaluates", func(t *testing.T) {
		// Just under the conversion ceiling: enormous, so it must defer on the BOUND
		// rather than on the range guard — proving the guard is not swallowing the
		// normal path.
		proceed, reason := EvaluateReadinessGate(StatusReading{
			Status: healthy(false, maxStalenessMs), Fresh: true,
		}, bound)
		if proceed || reason != DeferOverStaleness {
			t.Errorf("proceed=%v reason=%q, want a plain over_staleness defer", proceed, reason)
		}
	})
}
