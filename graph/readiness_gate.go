package graph

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
// published.
//
// This change finished the collapse by deleting the SECOND question too. The freshness
// parameter (exact | within(d) | none) survived ADR-084 only because coverage-derived
// gating had been the mechanism behind the retired ADR-066 "empty means not-found only
// if Ready" absence license; with the license gone, the machinery was serving exactly
// one call site — periodic community detection, which is the safest possible consumer
// of a stale view (idempotent, whole-result re-derivation, overwritten next cycle).
// What remains is ONE question:
//
//   - HEALTH — is this index sound to read from? Fresh status, an interpretable state,
//     no hard stop, initial build complete. Every consumer asks it; no consumer can opt
//     out; nothing can talk past it.
//
// View age is REPORTED, never gating: IndexStatusResponse.StalenessMs rides the answer
// and consumers stamp it on their output (community detection's
// staleness_at_detection_ms gauge). "Only 'still building' and 'broken' justify
// withholding; how far behind belongs on the answer."
//
// Read-your-writes, the one legitimately per-entity question, is not a gate concern at
// all: a caller that knows its own revision compares it against
// IndexStatusResponse.IndexedRevision directly. Ready does not appear in this gate —
// it is coverage, and coverage neither licenses nor withholds.

// DeferReason is the typed cause of a defer. Its string form is the label value of the
// defer_total{reason} counter and of the structured defer log field, so the set is
// CLOSED: four reasons an operator acts on differently — a broken index, an index that
// has not finished building, a dead status feed, and an envelope this consumer cannot
// interpret. The gh#590 investigation cost three comment cycles because the defer line
// was a bare constant.
type DeferReason string

// The closed defer-reason set.
const (
	// DeferNone is the reason attached to a proceed.
	DeferNone DeferReason = ""
	// DeferHardStop is a degraded or reset_required index: broken, not behind.
	DeferHardStop DeferReason = "hard_stop"
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

// AllDeferReasons is the closed iteration domain for the typed defer reasons a gate can
// return. It lives next to the const block so it is the single source of truth, exactly
// as AllIndexStates is for wire states: adding a reason means adding it HERE, once, and
// every consumer that pre-initializes counter series or validates a label set picks it
// up automatically.
//
// The alternative — each consumer hand-maintaining its own copy — fails SILENTLY in the
// direction that matters. A counter keyed off a private list drops any reason missing
// from it (dropping is correct: an unbounded label is a cardinality leak), so a newly
// added reason would defer in production while incrementing nothing, and the dashboard
// would read as "no defers" rather than "unknown defers".
//
// DeferNone is deliberately absent: it is the reason attached to a PROCEED, not a defer,
// and pre-initializing a "" series would publish a permanently-zero label.
var AllDeferReasons = []DeferReason{
	DeferHardStop,
	DeferStatusUnknown,
	DeferUnrecognizedState,
	DeferBootstrapIncomplete,
}

// StatusReading is a consumer's local view of a producer's envelope: the envelope
// itself plus the one fact only the consumer knows — whether it can still vouch for
// the feed. It is a struct rather than positional arguments so a new consumer-local
// fact can be added without breaking every adopter.
//
// graph/readiness.Reading maps onto it directly; an IN-PROCESS producer that computes
// its own envelope passes Fresh: true, having no transport to lose.
type StatusReading struct {
	// Status is the last envelope received from the producer.
	Status IndexStatusResponse
	// Fresh reports that the reading is recent enough to vouch for. False means
	// UNKNOWN, which fails closed — not "not ready".
	Fresh bool
}

// EvaluateReadinessGate is the single home for readiness gate semantics. It answers the
// health question and reports whether to proceed plus the typed defer reason. How a
// caller REACTS to a defer (error, skip a tick, serve an honest empty envelope) stays
// with the caller.
//
// Evaluation order is load-bearing:
//
//  1. Unknown status short-circuits BEFORE anything else. A dead status feed is a
//     transport fact; nothing licenses proceeding on state the consumer cannot vouch
//     for.
//  2. An UNRECOGNIZED State defers. The check is an allow-list over AllIndexStates,
//     not a deny-list for degraded/reset_required: a blank or future state must not
//     read as "not degraded, therefore healthy".
//  3. Hard stops (degraded, reset_required) defer.
//  4. An incomplete initial build defers likewise — this is the wire-observable gh#474
//     guard, and the reason bootstrap_complete had to leave the producer's process.
//  5. Everything else proceeds, however far behind the view is. Lag is not a fault; it
//     is a property of the answer, carried on IndexStatusResponse.StalenessMs for the
//     consumer to stamp on its output.
//
// Ready is deliberately absent. It is COVERAGE, and coverage answers no question this
// gate asks: it cannot license a proceed a health verdict already denied, and it cannot
// withhold one a health verdict already allowed. A caller with a specific revision in
// hand compares it against IndexedRevision itself.
//
// PRODUCER INVARIANT this ordering still relies on: an envelope with Ready == true must
// also carry BootstrapComplete == true. Health is answered before coverage is consulted
// anywhere, so a producer that published a caught-up envelope while reporting an
// unfinished build would see every consumer defer on it forever — pointing an operator
// at a cutover that already finished. It survived the ADR-085 collapse: deleting the
// Ready fast path removed the gate's own READ of the field, not the obligation on
// producers, because graph-index's sticky responder gate and every consumer's
// IndexedRevision comparison still assume the two agree. graph-index maintains it in
// latchBootstrap and on its caged early-boot branch; a new producer must do the same.
func EvaluateReadinessGate(reading StatusReading) (proceed bool, reason DeferReason) {
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
	return true, DeferNone
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
