package graph

import "strconv"

// IndexStatusResponse is the wire shape of graph.index.query.status (gh#397,
// enriched by ADR-066): the deterministic-fusion honesty envelope's readiness
// signal.
//
// Ready is load-bearing — only Ready permits a caller to treat an empty result
// as an authoritative not-found; when not Ready the caller must fall back (e.g.
// to grep) rather than conclude absence. Ready now means the index is CAUGHT UP:
// every committed ENTITY_STATES revision <= the query-time target has been
// applied (revision-lag, ADR-066), not merely "indexing started" (the old sticky
// NAME_INDEX-non-empty signal that fired minutes before the index was populated,
// gh#431). Ready guarantees revision COVERAGE, not last-writer-wins freshness
// under same-key churn (ADR-066 §1 Scope boundary).
//
// The JSON field names match pkg/fusion.IndexStatus so the fusion
// RetrievalClient decodes this response directly into its honesty envelope; the
// two structs change together.
type IndexStatusResponse struct {
	Ready bool   `json:"ready"` // target>0 && indexed_revision>=target_revision
	State string `json:"state"` // "building" | "ready" | "degraded" | "reset_required"
	// Code and Reason carry bounded operator-actionable fatal readiness state.
	// They are empty during ordinary building/ready/degraded operation.
	Code   string `json:"code,omitempty"`
	Reason string `json:"reason,omitempty"`
	// IndexedRevision is the low-water-of-pending watermark: every delivered
	// ENTITY_STATES revision <= this has been applied and nothing <= it is still
	// in flight. A consumer that knows its own target revision can gate on
	// IndexedRevision >= myRev instead of the coarse global Ready bool.
	IndexedRevision uint64 `json:"indexed_revision,omitempty"`
	// TargetRevision is the ENTITY_STATES stream LastSeq read at query time — the
	// latest committed write the index must catch up to.
	TargetRevision uint64 `json:"target_revision,omitempty"`
	// Lag is TargetRevision - IndexedRevision; 0 means caught up.
	Lag uint64 `json:"lag,omitempty"`
	// Phase is the optional friendly projection (ingesting | indexing | ready);
	// deferred — the numeric fields are load-bearing (ADR-066 open question).
	Phase      string `json:"phase,omitempty"`
	Revision   string `json:"revision,omitempty"`    // string IndexedRevision (now populated)
	LastSynced string `json:"last_synced,omitempty"` // last watermark advance (now populated)
}

// Index readiness states. Mirrors pkg/fusion.IndexState string values.
const (
	IndexStateBuilding      = "building"
	IndexStateReady         = "ready"
	IndexStateDegraded      = "degraded"
	IndexStateResetRequired = "reset_required"
)

// AllIndexStates is the closed iteration domain for the one-hot readiness `state`
// metric published by every producer of the envelope (graph-index, graph-embedding).
// It lives next to the const block so it is the single source of truth: adding a new
// readiness state means adding it HERE, and every one-hot state gauge picks it up
// automatically — otherwise a new state would render as all-zeros (silent "no data",
// not an alertable signal).
var AllIndexStates = []string{
	IndexStateBuilding,
	IndexStateReady,
	IndexStateDegraded,
	IndexStateResetRequired,
}

// ReadyWithinLag is the canonical bounded-lag readiness interpretation for
// periodic, whole-result-re-deriving consumers (ADR-082) — community detection
// today. It returns true iff the index is not a hard stop (degraded and
// reset_required survive NO tolerance) AND either it is exactly caught up (Ready)
// or its revision lag is within n.
//
// n is a per-CONSUMER tolerance, not a producer signal: exact/point-query
// consumers (the fusion honesty envelope, direct reverse-index reads) MUST keep
// gating on Ready and never call this — bounded lag would return a symbol written
// in the last n revisions as an authoritative miss. n=0 is exact parity with Ready
// for every state, target, and lag.
//
// The TargetRevision>0 guard is load-bearing (5-lens F1): an empty / pre-
// enumeration graph has Lag==0 but Ready==false, so Lag==0 here does NOT mean
// caught-up. Without the guard ReadyWithinLag(0) would report an empty graph ready
// while Ready is false.
//
// INVARIANT the n=0≡Ready parity relies on: degraded and reset_required always
// carry Ready==false (maintained by ComputeIndexStatus and the known-incomplete
// overrides). If a future writer ever sets a hard-stop State with Ready==true this
// short-circuit would diverge — preserve that projection when editing readiness.
func (r IndexStatusResponse) ReadyWithinLag(n uint64) bool {
	if r.State == IndexStateDegraded || r.State == IndexStateResetRequired {
		return false
	}
	return r.Ready || (r.TargetRevision > 0 && r.Lag <= n)
}

// ComputeIndexStatus builds the honest revision-lag readiness envelope (ADR-066)
// from an indexed watermark, the query-time target (a stream LastSeq), a stuck flag
// (the caller's stuck-watermark detector), and a last-synced timestamp. It is the
// shared PROJECTION over pkg/revlag.Watermark used by every revision-lag consumer
// (graph-index, graph-embedding); the watermark mechanism and the per-consumer
// stuck-detector live elsewhere.
//
//   - Ready = target > 0 && indexed >= target (no max(0,…) clamp — indexed <= target
//     is structural in the watermark, so Lag cannot underflow).
//   - State = ready ? "ready" : (stuck ? "degraded" : "building"); ready wins.
func ComputeIndexStatus(indexed, target uint64, stuck bool, lastSynced string) IndexStatusResponse {
	ready := target > 0 && indexed >= target
	var lag uint64
	if target > indexed {
		lag = target - indexed
	}
	state := IndexStateBuilding
	switch {
	case ready:
		state = IndexStateReady
	case stuck:
		state = IndexStateDegraded
	}
	resp := IndexStatusResponse{
		Ready:           ready,
		State:           state,
		IndexedRevision: indexed,
		TargetRevision:  target,
		Lag:             lag,
		LastSynced:      lastSynced,
	}
	if indexed > 0 {
		resp.Revision = strconv.FormatUint(indexed, 10)
	}
	return resp
}
