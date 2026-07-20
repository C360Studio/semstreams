package graph

import "time"

// The canonical readiness gate (ADR-083 D4). Before it, four consumers implemented
// four gate semantics — sticky-forever, per-tick exact-unless-configured, per-call
// exact with no knob, and degrade-honest — and a transport failure in one of them
// was indistinguishable in the logs from a genuine not-ready (gh#590). Semantics now
// live HERE, beside the envelope type they interpret (the two change together), and
// a consumer declares a MODE instead of hand-rolling a rule.

// GateMode is a consumer's declared readiness policy. It selects semantics, not
// severity: how a caller REACTS to a defer (error, skip a tick, serve an honest
// empty) stays with the caller.
type GateMode string

// The declared modes. Each names a real adopter; adding a mode means adding a row
// to the ADR-083 table, not a per-consumer special case.
const (
	// GateExact proceeds only on a fresh Ready envelope — the ADR-066
	// authoritative-absence license, unchanged. Adopters: the graph/query client,
	// fusion's top gate.
	GateExact GateMode = "exact"
	// GateBoundedStaleness proceeds when the view is no older than
	// GateConfig.MaxStaleness, hard stops excepted. Adopter: community detection,
	// a periodic whole-result-re-deriving consumer.
	GateBoundedStaleness GateMode = "bounded-staleness"
	// GateStickyBootstrap is exact until the first pass completes, then open —
	// with hard stops still overriding. Adopter: graph-index's own reverse-index
	// query gate.
	GateStickyBootstrap GateMode = "sticky-bootstrap"
	// GateDegradeHonest evaluates identically to GateExact; it exists so a caller
	// that degrades to an honest-empty result instead of erroring DECLARES that at
	// the call site. Adopter: fusion Fuse.
	GateDegradeHonest GateMode = "degrade-honest"
)

// DeferReason is the typed cause of a defer. Its string form is the label value of
// the defer_total{reason} counter and of the structured defer log field, so the set
// is CLOSED: four reasons an operator can act on differently — a broken index, an
// over-stale view, a dead status feed, and an empty graph. The gh#590 investigation
// cost three comment cycles because the defer line was a bare constant.
type DeferReason string

// The closed defer-reason set.
const (
	// DeferNone is the reason attached to a proceed.
	DeferNone DeferReason = ""
	// DeferHardStop is a degraded or reset_required index: broken, not behind. It
	// survives every mode and every tolerance.
	DeferHardStop DeferReason = "hard_stop"
	// DeferOverStaleness is a view older than the declared tolerance — including
	// the exact modes, whose tolerance is zero, so "not caught up" lands here.
	DeferOverStaleness DeferReason = "over_staleness"
	// DeferStatusUnknown is a status feed the consumer cannot vouch for (never
	// received, or older than the freshness window). It fails closed and is never
	// mistaken for index state.
	DeferStatusUnknown DeferReason = "status_unknown"
	// DeferEmpty is an empty / pre-enumeration graph (TargetRevision == 0): Lag is
	// 0 but that does NOT mean caught up.
	DeferEmpty DeferReason = "empty"
)

// GateConfig carries the per-consumer knobs the mode interprets. The zero value is
// the strictest configuration for every mode.
type GateConfig struct {
	// MaxStaleness is the bounded-staleness tolerance in wall time. Zero (the
	// default) makes the gate exactly equivalent to Ready. Ignored by the other
	// modes. Wall time rather than a revision count because the correct revision
	// bound moves 2-4x with coalesce_ms alone and linearly with write rate
	// (gh#590), while a time bound is invariant to both.
	MaxStaleness time.Duration
	// BootstrapDone reports whether the sticky-bootstrap consumer has completed its
	// first successful pass. Ignored by the other modes.
	BootstrapDone bool
}

// EvaluateReadinessGate is the single home for readiness gate semantics. It
// evaluates the held status, whether that status is FRESH (consumer-local arrival
// freshness — see graph/readiness; in-process producers that compute the envelope
// themselves pass true, having no transport to lose), the consumer's declared mode,
// and its config, and reports whether to proceed plus the typed defer reason.
//
// Evaluation order is load-bearing:
//
//  1. Unknown status short-circuits BEFORE any tolerance is considered. A dead
//     status feed is a transport fact; no tolerance, however generous, licenses
//     proceeding on state the consumer cannot vouch for.
//  2. Hard stops (degraded, reset_required) defer under EVERY mode and tolerance.
//  3. Ready proceeds under every mode.
//  4. Sticky-bootstrap opens after its first pass (hard stops already excluded).
//  5. An empty / pre-enumeration graph (TargetRevision == 0) never proceeds by
//     tolerance — Lag == 0 there does not mean caught up (the load-bearing guard
//     inherited from ReadyWithinLag's 5-lens F1 finding).
//  6. Bounded staleness proceeds within tolerance; everything else defers as
//     over_staleness.
//
// INVARIANT the exact-mode parity relies on: degraded and reset_required always
// carry Ready == false (maintained by ComputeIndexStatus and the producers'
// known-incomplete overrides). Because of it, steps 1-3 collapse to "proceed iff
// fresh && Ready" for the exact modes — bit-parity with the pre-ADR-083 checks. If a
// future writer ever set a hard-stop State with Ready == true, that parity would
// diverge; preserve the projection when editing readiness.
func EvaluateReadinessGate(status IndexStatusResponse, fresh bool, mode GateMode, cfg GateConfig) (proceed bool, reason DeferReason) {
	if !fresh {
		return false, DeferStatusUnknown
	}
	if status.State == IndexStateDegraded || status.State == IndexStateResetRequired {
		return false, DeferHardStop
	}
	if status.Ready {
		return true, DeferNone
	}
	if mode == GateStickyBootstrap && cfg.BootstrapDone {
		return true, DeferNone
	}
	if status.TargetRevision == 0 {
		return false, DeferEmpty
	}
	if mode == GateBoundedStaleness && cfg.MaxStaleness > 0 &&
		// StalenessMs > 0 is the PRESENCE bit, not a lower bound on age: a
		// not-ready envelope with staleness 0 could not compute its age, and
		// treating that as "0ms stale" would proceed on an unknown view. Producers
		// clamp any computed staleness to >= 1ms so this test never rejects a real
		// sub-millisecond value.
		status.StalenessMs > 0 &&
		status.StalenessMs <= uint64(cfg.MaxStaleness.Milliseconds()) {
		return true, DeferNone
	}
	return false, DeferOverStaleness
}
