package graph

import (
	"math"
	"time"
)

// The canonical readiness gate. Before it, four consumers implemented four gate
// semantics — sticky-forever, per-tick exact-unless-configured, per-call exact with no
// knob, and degrade-honest — and a transport failure in one of them was
// indistinguishable in the logs from a genuine not-ready (gh#590). Semantics live HERE,
// beside the envelope type they interpret (the two change together).
//
// ADR-084 then collapsed those four MODES into two orthogonal questions, because the
// modes were four dressings of one conflation: readiness had been asked to answer
// health, freshness, read-your-writes, AND authoritative absence at once, and the
// fourth is not answerable at all — coverage says nothing about whether a source ever
// published. What remains:
//
//   - HEALTH — is this index sound to read from? Fresh status, no hard stop, initial
//     build complete. Every consumer asks it, and no consumer can opt out.
//   - FRESHNESS — how current does THIS consumer need the view to be? A parameter
//     (exact | within(d) | none), not a policy, and never able to talk past health.
//
// Read-your-writes, the one legitimately per-entity question, is not a gate concern at
// all: a caller that knows its own revision compares it against
// IndexStatusResponse.IndexedRevision directly.

// Freshness is a consumer's declared view-currency requirement. Construct it with
// FreshnessExact, FreshnessWithin, or FreshnessNone — the fields are unexported so
// "exact" and "unbounded" cannot be confused by a partially-filled literal, and so the
// ZERO VALUE is the strictest setting rather than the loosest.
type Freshness struct {
	// unbounded declares no freshness requirement at all (read paths).
	unbounded bool
	// bound is the tolerated view age; 0 with !unbounded means exact catch-up.
	bound time.Duration
}

// FreshnessExact requires a caught-up view: the consumer proceeds only when the index
// has applied every committed revision up to its target. It is the view-rate contract
// for periodic whole-result re-derivation (community detection) and the zero value.
func FreshnessExact() Freshness { return Freshness{} }

// FreshnessWithin tolerates a view up to d old. A NON-POSITIVE d means exact — the
// shipped operator contract is "empty or 0 max_staleness = require exact index
// catch-up", and quietly reinterpreting an unset config field as "unbounded" would
// loosen the strictest gate in the system by changing nothing an operator can see.
func FreshnessWithin(d time.Duration) Freshness {
	if d <= 0 {
		return FreshnessExact()
	}
	return Freshness{bound: d}
}

// FreshnessNone declares no freshness requirement: the consumer wants the best
// available evidence from a HEALTHY index and will report how stale it was rather than
// withhold it. This is the read-path declaration (fusion, the graph/query client) and
// the deliberate ADR-084 reversal of #592's read-path close-out — ordinary lag is a
// property to report, not a fault to fail on.
func FreshnessNone() Freshness { return Freshness{unbounded: true} }

// DeferReason is the typed cause of a defer. Its string form is the label value of the
// defer_total{reason} counter and of the structured defer log field, so the set is
// CLOSED: five reasons an operator acts on differently — a broken index, an index that
// has not finished building, an over-stale view, a dead status feed, and an envelope
// this consumer cannot interpret. The gh#590 investigation cost three comment cycles
// because the defer line was a bare constant.
type DeferReason string

// The closed defer-reason set.
const (
	// DeferNone is the reason attached to a proceed.
	DeferNone DeferReason = ""
	// DeferHardStop is a degraded or reset_required index: broken, not behind. It
	// survives every freshness declaration.
	DeferHardStop DeferReason = "hard_stop"
	// DeferOverStaleness is a view older than the declared requirement — including
	// exact, whose tolerance is zero, so "not caught up" lands here. It is the ONE
	// reason an operator answers by tuning a tolerance, which is why no health
	// verdict is ever reported under it.
	DeferOverStaleness DeferReason = "over_staleness"
	// DeferStatusUnknown is a status feed the consumer cannot vouch for (never
	// received, or older than the freshness window). It fails closed and is never
	// mistaken for index state.
	DeferStatusUnknown DeferReason = "status_unknown"
	// DeferUnrecognizedState is an envelope whose State is blank or outside
	// AllIndexStates. It is NOT the same operator problem as a dead feed: the value
	// arrived and decoded, so the producer is talking — it is saying something this
	// consumer does not understand, which means version skew (a newer producer, a
	// partially-written envelope, a hand-edited key). Failing closed on it is the
	// whole posture of ADR-084: State became load-bearing when health replaced
	// coverage, so an uninterpretable State cannot be waved through as "not degraded".
	DeferUnrecognizedState DeferReason = "unrecognized_state"
	// DeferBootstrapIncomplete is a producer that has not finished its initial build
	// in this process lifetime — the gh#474 cutover window, where the keyset is
	// half-materialised and a plausible small Lag is actively misleading. It replaces
	// the former `empty` reason, whose TargetRevision==0 proxy was wrong in both
	// directions: false during a cutover, and true for the authoritatively empty
	// graph it then wrongly deferred.
	DeferBootstrapIncomplete DeferReason = "bootstrap_incomplete"
)

// StatusReading is a consumer's local view of a producer's envelope: the envelope
// itself plus the two facts only the consumer knows — whether it can still vouch for
// the feed, and how long ago the value arrived. It is a struct rather than positional
// arguments because Fresh and Age are otherwise an adjacent bool/duration pair in a
// call that gates authoritative reads.
//
// graph/readiness.Reading maps onto it directly; an IN-PROCESS producer that computes
// its own envelope passes Fresh: true and Age: 0, having no transport to lose.
type StatusReading struct {
	// Status is the last envelope received from the producer.
	Status IndexStatusResponse
	// Fresh reports that the reading is recent enough to vouch for. False means
	// UNKNOWN, which fails closed — not "not ready".
	Fresh bool
	// Age is how long ago this value arrived, consumer-local. It is added to the
	// envelope's own staleness when judging a bound, closing the heartbeat window: a
	// 2.5s-stale envelope received 2s ago describes a ~4.5s-old view.
	Age time.Duration
}

// EvaluateReadinessGate is the single home for readiness gate semantics. It answers the
// health question, then the consumer's declared freshness question, and reports whether
// to proceed plus the typed defer reason. How a caller REACTS to a defer (error, skip a
// tick, serve an honest empty envelope) stays with the caller.
//
// Evaluation order is load-bearing:
//
//  1. Unknown status short-circuits BEFORE anything else. A dead status feed is a
//     transport fact; no freshness declaration, not even "none", licenses proceeding on
//     state the consumer cannot vouch for.
//  2. An UNRECOGNIZED State defers. The check is an allow-list over AllIndexStates,
//     not a deny-list for degraded/reset_required: a blank or future state must not
//     read as "not degraded, therefore healthy".
//  3. Hard stops (degraded, reset_required) defer under every freshness declaration.
//  4. An incomplete initial build defers likewise — this is the wire-observable gh#474
//     guard, and the reason bootstrap_complete had to leave the producer's process.
//  5. Ready proceeds. Coverage LICENSES a proceed but never causes a defer: a caught-up
//     view answers every freshness requirement with zero age, which is also what keeps
//     the staleness presence encoding sound (a caught-up envelope reports no staleness,
//     and without this fast path every bounded consumer would wedge exactly when the
//     index caught up).
//  6. No freshness requirement proceeds on a healthy index, however far behind.
//  7. A bound proceeds when the envelope's staleness PLUS the reading's own age fits
//     inside it, computed overflow-safely — an out-of-range staleness or a backwards
//     local clock defers rather than wrapping into a false "fresh". Everything else is
//     over_staleness.
//
// Health verdicts are never reported as over_staleness. That separation is the point of
// the reason set: over_staleness is the one defer an operator answers by tuning a
// tolerance, and a broken or half-built index answered that way sends them to the wrong
// dial (gh#590 again, in a new costume).
//
// PRODUCER INVARIANT this ordering relies on: an envelope with Ready == true must also
// carry BootstrapComplete == true. Health is answered BEFORE coverage, so a producer
// that published a caught-up envelope while reporting an unfinished build would see
// every consumer defer on it forever — pointing an operator at a cutover that already
// finished. graph-index maintains it in latchBootstrap and on its caged early-boot
// branch; a new producer must do the same.
func EvaluateReadinessGate(reading StatusReading, want Freshness) (proceed bool, reason DeferReason) {
	status := reading.Status
	if !reading.Fresh {
		return false, DeferStatusUnknown
	}
	if !recognizedState(status.State) {
		// Allow-list, not deny-list. Checking only for degraded/reset_required would
		// let a blank or future state proceed as healthy — fail-OPEN on exactly the
		// field ADR-084 made load-bearing.
		return false, DeferUnrecognizedState
	}
	if status.State == IndexStateDegraded || status.State == IndexStateResetRequired {
		return false, DeferHardStop
	}
	if !status.BootstrapComplete {
		return false, DeferBootstrapIncomplete
	}
	if status.Ready {
		return true, DeferNone
	}
	if want.unbounded {
		return true, DeferNone
	}
	if want.bound > 0 &&
		// StalenessMs > 0 is the PRESENCE bit, not a lower bound on age: a not-ready
		// envelope with staleness 0 could not compute its age, and treating that as
		// "0ms stale" would proceed on an unknown view. Producers clamp any computed
		// staleness to >= 1ms so this test never rejects a real sub-millisecond value.
		status.StalenessMs > 0 {
		if age, ok := viewAge(status.StalenessMs, reading.Age); ok && age <= want.bound {
			return true, DeferNone
		}
	}
	return false, DeferOverStaleness
}

// recognizedState reports whether a wire State is one this consumer understands.
// Sourced from AllIndexStates so adding a state means adding it there, once.
func recognizedState(state string) bool {
	for _, known := range AllIndexStates {
		if state == known {
			return true
		}
	}
	return false
}

// maxStalenessMs is the largest StalenessMs that survives conversion to a
// time.Duration without wrapping (Duration is int64 NANOseconds). Anything above it is
// not a longer age — it is a corrupt or hostile value, and converting it yields a
// NEGATIVE duration that would sail through any positive bound.
const maxStalenessMs = uint64(math.MaxInt64 / int64(time.Millisecond))

// viewAge is the total age a bound is judged against: the envelope's own staleness plus
// how long ago the consumer received it. It reports ok=false when the inputs cannot be
// combined into a trustworthy age, which the caller must treat as a DEFER.
//
// Both guards are fail-closed against arithmetic that would otherwise fail open:
//   - a StalenessMs above maxStalenessMs wraps negative through time.Duration;
//   - a negative Age (local clock stepping backwards between receipt and evaluation)
//     would SUBTRACT from the age and make a stale view look fresh.
func viewAge(stalenessMs uint64, age time.Duration) (time.Duration, bool) {
	if stalenessMs > maxStalenessMs {
		return 0, false
	}
	if age < 0 {
		// A clock that ran backwards tells us nothing about how old the view is. Drop
		// the local contribution rather than let it credit freshness; the envelope's
		// own staleness still applies.
		age = 0
	}
	total := time.Duration(stalenessMs) * time.Millisecond
	if total > math.MaxInt64-age {
		return 0, false
	}
	return total + age, true
}
