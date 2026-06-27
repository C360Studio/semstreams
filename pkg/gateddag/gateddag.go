// Package gateddag is the pure decision brain of gated-DAG dispatch
// (ADR-046 Phase 2). Given a set of work units with depends_on edges and a
// completion/failure/reset marker snapshot, it computes — as a side-effect-free
// function of marker membership — each unit's DERIVED status and the set of
// units dispatchable right now.
//
// It owns no state, performs no I/O, and knows nothing of NATS, the graph, or
// any consumer domain. There is no Story/owner/file-overlap concept here: a
// consumer that needs those (e.g. semspec's M:N owner gating, or its
// file-overlap serialization edges) resolves them into the depends_on map and
// applies any domain gate on top before/after calling this brain. The executor
// component (a separate package, ADR-046 Phase 2) reads marker sets from
// ENTITY_STATES authoritatively, calls this brain, and acts on its verdicts via
// the dispatch substrate.
//
// # Why a pure function (the wedge it kills)
//
// Status is DERIVED, never stored or mutated: every evaluation recomputes from
// the current marker membership, so there is no status field to go stale and
// nothing to race the markers. A gated dispatcher that maintains a
// separately-mutated status field reintroduces the wedge family — a reset
// strands a unit; a re-dispatch idempotent-skips into idle (ADR-046 correctness
// requirement #1, "the root of the whole wedge family"). The brain re-derives
// from authoritative marker membership every evaluation; the executor supplies
// that membership by reading the whole unit set from KV (it cannot be projected
// onto a single entity, which is why this is a component, not a rule).
//
// # Coverage boundary
//
// The brain covers the requirements that are a pure function of markers + edges:
// derived status (#1), wait-for-ALL-prerequisites (#2), generic dependency
// source (#3, depends_on is opaque), reset/re-derive (#4, Dirtied precedence),
// the brain half of failure-release (#7, a failed prereq's dependent surfaces
// via Stalled rather than dispatching), and stall/cycle detection (#8).
// Fresh-per-run identity (#5) and in-flight dedup (#6) are executor concerns —
// the brain returns Ready for an in-flight unit (it carries no terminal marker),
// so the executor MUST exclude already-dispatched units via its durable claim
// marker. See ADR-046 "Load-bearing invariants".
package gateddag

import "sort"

// Status is the DERIVED execution status of a unit — a pure function of marker
// membership. It is never stored or mutated.
type Status int

const (
	// StatusReady — no live terminal marker: the unit is eligible to dispatch
	// once its prerequisites clear. A reset/recovery-dirtied unit also derives
	// as Ready (its stale terminal markers are ignored — it must re-run).
	StatusReady Status = iota
	// StatusDone — the unit appears in the Completed marker set.
	StatusDone
	// StatusBlocked — the unit appears in the Failed marker set (and is not
	// dirtied): terminal failure awaiting recovery.
	StatusBlocked
)

// String renders a Status for logs/observability.
func (s Status) String() string {
	switch s {
	case StatusDone:
		return "done"
	case StatusBlocked:
		return "blocked"
	default:
		return "ready"
	}
}

// MarkerSet is the multi-valued completion/failure/reset marker membership read
// from the graph at one instant. Membership is the ONLY input to derived
// status — the brain never reads a mutated status field.
type MarkerSet struct {
	// Completed holds unit IDs present in the completion marker.
	Completed map[string]bool
	// Failed holds unit IDs present in the failure marker.
	Failed map[string]bool
	// Dirtied holds unit IDs that are recovery-dirtied or reset. A dirtied unit
	// overrides any stale Completed/Failed membership and derives as Ready (it
	// must re-run), which is the presence-marker re-entry idiom that lets a
	// reset re-dispatch without a stored status to clear.
	Dirtied map[string]bool
}

// NewMarkerSet builds a MarkerSet from the multi-valued marker slices read off
// the graph (nil slices are fine — they become empty sets).
func NewMarkerSet(completed, failed, dirtied []string) MarkerSet {
	return MarkerSet{
		Completed: toSet(completed),
		Failed:    toSet(failed),
		Dirtied:   toSet(dirtied),
	}
}

func toSet(ids []string) map[string]bool {
	m := make(map[string]bool, len(ids))
	for _, id := range ids {
		m[id] = true
	}
	return m
}

// DeriveStatus computes a unit's status from marker membership alone.
//
// Precedence (documented because it is load-bearing):
//  1. Dirtied wins — a reset/recovery-dirtied unit is Ready regardless of any
//     stale Completed/Failed marker left from a prior run; it must re-execute.
//  2. Failed beats Completed — if a unit somehow carries both terminal markers
//     (a re-failure after a prior completion), the more actionable Blocked
//     verdict is reported so recovery, not a false "done", is surfaced.
//  3. Completed → Done.
//  4. Otherwise → Ready.
func DeriveStatus(id string, m MarkerSet) Status {
	if m.Dirtied[id] {
		return StatusReady
	}
	if m.Failed[id] {
		return StatusBlocked
	}
	if m.Completed[id] {
		return StatusDone
	}
	return StatusReady
}

// Decision is the brain's verdict for one unit, with a reason for observability.
type Decision struct {
	UnitID       string
	Status       Status
	Dispatchable bool
	Reason       string
}

// Evaluate returns a deterministic, unit-ID-sorted decision for every unit: its
// derived status and whether it is dispatchable right now.
//
// A unit is dispatchable when ALL of:
//   - its derived status is Ready (not Done, not Blocked; a dirtied unit is
//     Ready and so re-dispatchable);
//   - every entry in dependsOn[unit] derives as Done — the DAG closure is
//     "wait for ALL prerequisites", never "any" (a Blocked or still-Ready or
//     dirtied prerequisite holds the dependent).
//
// dependsOn is the resolved edge set: the caller unions whatever sources it
// needs (semspec unions semantic prerequisites with file-overlap serialization
// edges) before calling here. A unit with no entry in dependsOn has no
// prerequisites.
func Evaluate(unitIDs []string, dependsOn map[string][]string, m MarkerSet) []Decision {
	decisions := make([]Decision, 0, len(unitIDs))
	for _, id := range unitIDs {
		decisions = append(decisions, decideUnit(id, dependsOn, m))
	}
	sort.Slice(decisions, func(i, j int) bool {
		return decisions[i].UnitID < decisions[j].UnitID
	})
	return decisions
}

func decideUnit(id string, dependsOn map[string][]string, m MarkerSet) Decision {
	d := Decision{UnitID: id, Status: DeriveStatus(id, m)}
	switch d.Status {
	case StatusDone:
		d.Reason = "already complete"
		return d
	case StatusBlocked:
		d.Reason = "failed — awaiting recovery"
		return d
	}
	// Ready: the DAG closure — ALL prerequisites must derive Done.
	for _, dep := range dependsOn[id] {
		if DeriveStatus(dep, m) != StatusDone {
			d.Reason = "waiting on prerequisite " + dep
			return d
		}
	}
	d.Dispatchable = true
	d.Reason = "ready"
	return d
}

// SelectDispatchable is the convenience projection of Evaluate: the sorted set
// of unit IDs dispatchable right now.
func SelectDispatchable(unitIDs []string, dependsOn map[string][]string, m MarkerSet) []string {
	var ready []string
	for _, d := range Evaluate(unitIDs, dependsOn, m) {
		if d.Dispatchable {
			ready = append(ready, d.UnitID)
		}
	}
	return ready
}

// Stalled reports the units that are gated with no forward progress possible:
// there is no dispatchable unit, yet ≥1 unit is non-terminal (Ready but held).
// That is the silent-idle signature of a depends_on cycle, or of every
// non-terminal unit waiting behind a Blocked prerequisite — a stuck plan that
// looks merely idle. The executor surfaces a non-empty result as an ALERT
// instead of letting it read as benign idleness (ADR-046 requirement #8).
// Returns nil when work is dispatchable OR everything is terminal (a genuinely
// complete or genuinely empty plan is not stalled).
//
// Pure observability — it does not change dispatch. A non-empty result means
// nothing can move without recovery (reset a failed prerequisite) or an
// authoring fix (break a cycle).
func Stalled(unitIDs []string, dependsOn map[string][]string, m MarkerSet) []string {
	var heldReady []string
	dispatchable := false
	for _, d := range Evaluate(unitIDs, dependsOn, m) {
		if d.Dispatchable {
			dispatchable = true
		}
		if d.Status == StatusReady && !d.Dispatchable {
			heldReady = append(heldReady, d.UnitID)
		}
	}
	if dispatchable || len(heldReady) == 0 {
		return nil
	}
	return heldReady // already sorted (Evaluate sorts by unit ID)
}
