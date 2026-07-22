package graph

import (
	"strconv"
	"time"
)

// IndexStatusResponse is the wire shape of the readiness envelope (gh#397,
// enriched by ADR-066, distributed as GRAPH_STATUS KV state by ADR-083): the
// deterministic-fusion honesty envelope's readiness signal.
//
// Ready reports COVERAGE: every committed ENTITY_STATES revision <= the query-time
// target has been applied (revision-lag, ADR-066), not merely "indexing started" (the
// old sticky NAME_INDEX-non-empty signal that fired minutes before the index was
// populated, gh#431). It guarantees revision coverage, not last-writer-wins freshness
// under same-key churn (ADR-066 §1 Scope boundary).
//
// Ready LICENSES NOTHING ABOUT ABSENCE (ADR-084). It once carried an "only Ready
// permits an authoritative not-found" contract; that is retired, because coverage
// cannot answer the question — an index caught up to every revision ever committed
// still knows nothing about a source that never published. No consumer may treat an
// empty result as an authoritative not-found under ANY envelope state.
//
// Nor does Ready == false withhold a response any more: reads gate on HEALTH (State
// plus BootstrapComplete), and a healthy index that is merely behind serves while
// reporting StalenessMs. Under continuous write Ready is false essentially always,
// which is what made it a poor proxy for soundness. Its remaining jobs are a caught-up
// fast path for the freshness question, and being the value a caller compares its own
// revision against via IndexedRevision — the one sound per-entity check.
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
	// BootstrapComplete reports whether this producer has finished its INITIAL BUILD
	// in the current process lifetime: enumeration of ENTITY_STATES plus replay up to
	// the enumeration-time target, including the authoritatively-empty 0/0 outcome. It
	// latches true once and resets to false on restart, so a restart into a format
	// cutover re-gates (ADR-084 D2).
	//
	// It exists because health is otherwise not evaluable from the wire: an index
	// halfway through a gh#474 cutover reports State=building with a plausible Lag,
	// indistinguishable from an index that is merely behind. Ready cannot answer it
	// (a caught-up index is bootstrapped, but a bootstrapped index under write is not
	// caught up) and TargetRevision==0 cannot either (it is false during a cutover and
	// wrongly deferred the empty graph).
	//
	// NOT omitempty, and absent reads FALSE — fail closed. An envelope from a
	// pre-ADR-084 producer therefore defers every health gate until the lockstep
	// upgrade lands; that is the accepted migration cost, and the explicit `false` on
	// the wire keeps "old producer" distinguishable from "not yet bootstrapped" in a
	// `nats kv get GRAPH_STATUS <producer>` dump.
	BootstrapComplete bool `json:"bootstrap_complete"`
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
	// FailedCount / FailedReasons / FirstFailureAt are the bounded failure detail a
	// producer that tracks per-entity failures carries on a DEGRADED envelope so an
	// operator can tell a whole-dependency outage from a few persistently-failing
	// entities WITHOUT any unbounded per-entity list on the watched key (#613).
	//
	// All three are additive and omitempty: a producer with no failures — including
	// graph-index, which enforces the same rule caller-side via its watermark hole —
	// emits none, so the wire is byte-unchanged. FailedReasons is bounded to a fixed
	// reason enum (a handful of keys), keeping the watched key compact on the hot KV
	// path. FirstFailureAt is RFC3339. FailedCount is echoed from the projection input
	// (it also drives State=degraded, see ComputeIndexStatus); the other two are set by
	// the producer after the projection.
	FailedCount    uint64            `json:"failed_count,omitempty"`
	FailedReasons  map[string]uint64 `json:"failed_reasons,omitempty"`
	FirstFailureAt string            `json:"first_failure_at,omitempty"`
	// StalenessMs is the AGE OF THE VIEW in milliseconds (ADR-083): now minus the
	// KV commit time of the newest ENTITY_STATES revision the index has fully
	// covered. It is the view-rate consumer's tolerance unit because it is
	// invariant to write rate and coalesce_ms, where a revision count is not
	// (gh#590: the correct revision bound shifted 2-4x with the coalesce dial
	// alone).
	//
	// PRESENCE ENCODING — 0 does NOT mean "zero staleness". It means the value
	// carries no information: either Ready is true (no staleness to report) or the
	// producer could not compute it (nothing covered yet / an early-return hard
	// stop). Any COMPUTED staleness is reported as at least 1ms, so a consumer can
	// treat `StalenessMs > 0` as the presence bit and never read an absent age as
	// "0ms fresh".
	//
	// It is REPORTED, never gating. EvaluateReadinessGate does not look at this field:
	// readiness withholds an answer only for index health, and how far behind the view
	// is rides on the answer instead (community detection stamps it on
	// staleness_at_detection_ms). A consumer that surfaces it must carry the presence
	// encoding with it — publishing a bare 0 as "caught up" is the one way to turn an
	// unknown age back into a false claim.
	//
	// It is a FLOOR, not an oracle: a revision still undelivered server-side cannot
	// age it. Total stalls surface through the wall-clock stuck detector
	// (State=degraded), not through this field.
	StalenessMs uint64 `json:"staleness_ms,omitempty"`
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

// IndexStatusInputs are the observations ComputeIndexStatus projects into the
// readiness envelope. It is a struct rather than a positional argument list
// because two of the inputs are time.Time (IndexedAt, Now) and adjacent
// same-typed parameters are a silent-swap footgun in a projection whose output
// gates authoritative-absence claims.
type IndexStatusInputs struct {
	// Indexed is the low-water-of-pending watermark (revlag.Watermark.Indexed).
	Indexed uint64
	// Target is the query-time ENTITY_STATES stream LastSeq the index must reach.
	Target uint64
	// Stuck is the caller's own stuck-watermark detector verdict (-> degraded).
	Stuck bool
	// LastSynced is the last-advance timestamp, RFC3339, for the envelope field.
	LastSynced string
	// IndexedAt is the KV COMMIT time of the newest revision Indexed covers
	// (revlag.Watermark.IndexedAt). ZERO means "not computable" — the projection
	// then leaves StalenessMs at 0 rather than fabricating a fresh-looking view.
	IndexedAt time.Time
	// Now is the compute instant; the zero value means time.Now(). Tests set it to
	// keep the staleness projection deterministic instead of asserting on the wall
	// clock.
	Now time.Time
	// FailedCount is the number of index entries the producer CURRENTLY holds in a
	// failed terminal state. When > 0 it projects to State=degraded BEFORE the "ready
	// wins" branch, UNCONDITIONALLY (not gated on Ready): a producer whose watermark
	// has reached its target while holding failures is COVERED (Ready stays accurate)
	// but NOT healthy (a failed record is not a usable index entry). This makes the
	// shared projection finally enforce the graph-index-readiness `FailedCount > 0 →
	// degraded` rule for a producer (graph-embedding) whose watermark advances past
	// failures. graph-index leaves this 0 — it enforces the same rule caller-side via
	// its watermark hole + applyKnownIncompleteOverrides — so its projection output is
	// byte-unchanged (ADR-085, #613).
	FailedCount uint64
}

// ComputeIndexStatus builds the honest revision-lag readiness envelope (ADR-066,
// extended with age-of-view staleness by ADR-083) from an indexed watermark, the
// query-time target (a stream LastSeq), a stuck flag (the caller's stuck-watermark
// detector), a last-synced timestamp, and the commit time of the indexed floor. It
// is the shared PROJECTION over pkg/revlag.Watermark used by every revision-lag
// producer (graph-index, graph-embedding); the watermark mechanism and the
// per-producer stuck-detector live elsewhere.
//
//   - Ready = target > 0 && indexed >= target (no max(0,…) clamp — indexed <= target
//     is structural in the watermark, so Lag cannot underflow).
//   - State = failedCount>0 ? "degraded" : (ready ? "ready" : (stuck ? "degraded" :
//     "building")). The failure check is FIRST and unconditional, so a producer
//     caught up over failures reports degraded, not ready — Ready still reports the
//     (accurate) coverage, but health lives in State (#613, ADR-085). With
//     failedCount==0 the switch is identical to the prior "ready wins" behavior, so
//     graph-index (which passes 0) is byte-unchanged.
//   - StalenessMs = 0 when Ready or when IndexedAt is unknown, else now-IndexedAt
//     clamped to a 1ms minimum (see the presence encoding on the field).
//
// The staleness subtraction is the ONE place a NATS server commit timestamp meets
// a local clock; under skew it is off by the skew. That is accepted (ADR-083 D3)
// because the alternative — a revision count — is wrong by 2-4x under a coalesce
// change alone. Consumer-side FRESHNESS deliberately does not compare clocks
// (graph/readiness judges arrival locally).
func ComputeIndexStatus(in IndexStatusInputs) IndexStatusResponse {
	ready := in.Target > 0 && in.Indexed >= in.Target
	var lag uint64
	if in.Target > in.Indexed {
		lag = in.Target - in.Indexed
	}
	state := IndexStateBuilding
	switch {
	case in.FailedCount > 0:
		// Unconditional, BEFORE "ready wins": a known-incomplete index (a producer
		// holding failed entries) defers on HEALTH regardless of coverage. Ready stays
		// coverage-accurate below; the health verdict is here (#613, ADR-085).
		state = IndexStateDegraded
	case ready:
		state = IndexStateReady
	case in.Stuck:
		state = IndexStateDegraded
	}
	resp := IndexStatusResponse{
		Ready:           ready,
		State:           state,
		IndexedRevision: in.Indexed,
		TargetRevision:  in.Target,
		Lag:             lag,
		LastSynced:      in.LastSynced,
		StalenessMs:     stalenessMs(ready, in.IndexedAt, in.Now),
		FailedCount:     in.FailedCount,
	}
	if in.Indexed > 0 {
		resp.Revision = strconv.FormatUint(in.Indexed, 10)
	}
	return resp
}

// stalenessMs projects the age of the view. It returns 0 — the "no information"
// encoding — when the view is caught up (nothing is stale) or when the floor's
// commit time is unknown, and otherwise at least 1ms so that a computed staleness
// is always distinguishable from an absent one.
func stalenessMs(ready bool, indexedAt, now time.Time) uint64 {
	if ready || indexedAt.IsZero() {
		return 0
	}
	if now.IsZero() {
		now = time.Now()
	}
	ms := now.Sub(indexedAt).Milliseconds()
	if ms < 1 {
		// Sub-millisecond age, or a local clock behind the server's. Report the
		// minimum COMPUTED value rather than 0, which would read as "absent".
		ms = 1
	}
	return uint64(ms)
}
