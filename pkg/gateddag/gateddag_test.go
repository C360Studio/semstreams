package gateddag_test

import (
	"reflect"
	"slices"
	"testing"

	"github.com/c360studio/semstreams/pkg/gateddag"
)

// The cases here ARE the ADR-046 correctness requirements the brain owns
// (#1 derived-not-mutated, #2 wait-for-ALL, #3 generic source, #4 reset,
// #7 brain-half of failure-release, #8 stall). #5 fresh-identity and #6
// in-flight dedup are executor concerns, not brain — see the package doc.
// Generalized from semspec's coordinator suite with all Story/owner cases
// dropped (M:N gating stays in the consumer).

func contains(ss []string, s string) bool {
	return slices.Contains(ss, s)
}

// --- #1 derived-not-mutated: status is a pure function of markers ---

func TestDeriveStatus_PureFunctionOfMarkers(t *testing.T) {
	m := gateddag.NewMarkerSet(
		[]string{"u-done"},   // completed
		[]string{"u-failed"}, // failed
		nil,                  // dirtied
	)
	cases := []struct {
		id   string
		want gateddag.Status
	}{
		{"u-done", gateddag.StatusDone},
		{"u-failed", gateddag.StatusBlocked},
		{"u-unknown", gateddag.StatusReady},
	}
	for _, c := range cases {
		if got := gateddag.DeriveStatus(c.id, m); got != c.want {
			t.Errorf("DeriveStatus(%q) = %v, want %v", c.id, got, c.want)
		}
	}
}

// TestDeriveStatus_DirtiedOverridesStaleTerminal proves the reset/re-entry
// idiom: a dirtied unit derives Ready even with a stale completed marker, so a
// reset re-dispatches with no stored status to clear. Teeth: drop the Dirtied
// branch and this fails (would report Done).
func TestDeriveStatus_DirtiedOverridesStaleTerminal(t *testing.T) {
	m := gateddag.NewMarkerSet(
		[]string{"u-reset"}, // stale completed
		nil,
		[]string{"u-reset"}, // recovery-dirtied
	)
	if got := gateddag.DeriveStatus("u-reset", m); got != gateddag.StatusReady {
		t.Errorf("dirtied unit with stale completed marker: got %v, want Ready", got)
	}
}

// TestDeriveStatus_FailedBeatsCompleted pins the precedence: a unit carrying
// both terminal markers reports the actionable Blocked, not a false Done.
func TestDeriveStatus_FailedBeatsCompleted(t *testing.T) {
	m := gateddag.NewMarkerSet([]string{"u"}, []string{"u"}, nil)
	if got := gateddag.DeriveStatus("u", m); got != gateddag.StatusBlocked {
		t.Errorf("completed+failed: got %v, want Blocked", got)
	}
}

// --- #2 wait-for-ALL-prerequisites ---

// TestSelectDispatchable_WaitsForAllPrerequisites is the "ALL not any" teeth
// test: a dependent with two prerequisites stays gated while only one is done,
// and dispatches only once BOTH are done.
func TestSelectDispatchable_WaitsForAllPrerequisites(t *testing.T) {
	units := []string{"p1", "p2", "dep"}
	deps := map[string][]string{"dep": {"p1", "p2"}}

	onlyP1 := gateddag.NewMarkerSet([]string{"p1"}, nil, nil)
	got := gateddag.SelectDispatchable(units, deps, onlyP1)
	if contains(got, "dep") {
		t.Errorf("dep dispatched with only one of two prerequisites complete: %v", got)
	}
	if !contains(got, "p2") {
		t.Errorf("p2 (no prereqs, not done) should be dispatchable: %v", got)
	}

	bothDone := gateddag.NewMarkerSet([]string{"p1", "p2"}, nil, nil)
	got = gateddag.SelectDispatchable(units, deps, bothDone)
	if !contains(got, "dep") {
		t.Errorf("dep should dispatch once ALL prerequisites complete: %v", got)
	}
}

// TestSelectDispatchable_BlockedPrerequisiteHoldsDependent: a FAILED prerequisite
// (not Done) keeps the dependent gated — a blocked prereq is not "complete".
func TestSelectDispatchable_BlockedPrerequisiteHoldsDependent(t *testing.T) {
	units := []string{"p", "dep"}
	deps := map[string][]string{"dep": {"p"}}
	m := gateddag.NewMarkerSet(nil, []string{"p"}, nil) // p failed
	if contains(gateddag.SelectDispatchable(units, deps, m), "dep") {
		t.Error("dependent dispatched while its prerequisite is Blocked")
	}
}

func TestSelectDispatchable_DoneUnitNotRedispatched(t *testing.T) {
	units := []string{"u"}
	m := gateddag.NewMarkerSet([]string{"u"}, nil, nil)
	if contains(gateddag.SelectDispatchable(units, nil, m), "u") {
		t.Error("a completed unit must not be dispatchable")
	}
}

// --- #3 generic dependency source: depends_on is opaque, consumer-resolved ---

// TestSelectDispatchable_HonorsResolvedEdges is the generic of semspec's
// file-overlap test: two units with no intrinsic relationship serialize purely
// because the consumer resolved an edge between them into depends_on. The brain
// does not care where the edge came from (semantic, file-overlap, anything).
func TestSelectDispatchable_HonorsResolvedEdges(t *testing.T) {
	units := []string{"a", "b"}
	deps := map[string][]string{"b": {"a"}} // consumer-resolved: b waits on a

	got := gateddag.SelectDispatchable(units, deps, gateddag.NewMarkerSet(nil, nil, nil))
	if !contains(got, "a") {
		t.Errorf("prerequisite a should dispatch: %v", got)
	}
	if contains(got, "b") {
		t.Errorf("b dispatched despite a resolved depends_on edge: %v", got)
	}
	got = gateddag.SelectDispatchable(units, deps, gateddag.NewMarkerSet([]string{"a"}, nil, nil))
	if !contains(got, "b") {
		t.Errorf("b should dispatch once its resolved prerequisite is done: %v", got)
	}
}

// --- #4 reset / re-dispatch authoritative (brain level) ---

func TestSelectDispatchable_DirtiedUnitRedispatches(t *testing.T) {
	units := []string{"u"}
	// u was completed, then reset (dirtied) — it must become dispatchable again.
	m := gateddag.NewMarkerSet([]string{"u"}, nil, []string{"u"})
	if !contains(gateddag.SelectDispatchable(units, nil, m), "u") {
		t.Error("a reset (dirtied) unit must re-dispatch, not idle-skip on a stale completed marker")
	}
}

// TestSelectDispatchable_DirtiedPrerequisiteHoldsDependent is the teeth test
// that pins "wait for DeriveStatus(dep) == Done", not a weaker "dep not Failed":
// a prerequisite that was completed then reset (Dirtied → Ready, re-running) is
// NOT Done, so its dependent must stay held while the prereq itself re-dispatches
// to re-produce its output. A closure that checked `!= StatusBlocked` would
// wrongly dispatch the dependent over a re-running prereq (a #4-reset wedge).
func TestSelectDispatchable_DirtiedPrerequisiteHoldsDependent(t *testing.T) {
	units := []string{"p", "dep"}
	deps := map[string][]string{"dep": {"p"}}
	// p was completed, now reset → derives Ready (re-running), not Done.
	m := gateddag.NewMarkerSet([]string{"p"}, nil, []string{"p"})

	got := gateddag.SelectDispatchable(units, deps, m)
	if contains(got, "dep") {
		t.Errorf("dep dispatched while its prerequisite p is re-running (Dirtied, not Done): %v", got)
	}
	if !contains(got, "p") {
		t.Errorf("the dirtied prerequisite p must itself re-dispatch: %v", got)
	}
	if s := gateddag.Stalled(units, deps, m); s != nil {
		t.Errorf("not stalled — p is dispatchable: %v", s)
	}
}

// TestSelectDispatchable_Diamond verifies multi-level fan-in (a→{b,c}→d): d
// dispatches only after BOTH mid-nodes complete, and the mid-nodes only after
// the root. Single-level WaitsForAll does not exercise the transitive case.
func TestSelectDispatchable_Diamond(t *testing.T) {
	units := []string{"a", "b", "c", "d"}
	deps := map[string][]string{"b": {"a"}, "c": {"a"}, "d": {"b", "c"}}

	// a done, only b done (c outstanding) → d held.
	abDone := gateddag.NewMarkerSet([]string{"a", "b"}, nil, nil)
	if contains(gateddag.SelectDispatchable(units, deps, abDone), "d") {
		t.Error("d dispatched with only one of its two mid-level prerequisites done")
	}
	// a, b, c done → d dispatchable.
	abcDone := gateddag.NewMarkerSet([]string{"a", "b", "c"}, nil, nil)
	if !contains(gateddag.SelectDispatchable(units, deps, abcDone), "d") {
		t.Error("d should dispatch once both mid-level prerequisites complete")
	}
}

// TestStalled_SelfLoop: a 1-node cycle (a→a) derives nothing dispatchable and
// must surface as stalled (DetectsCycle only covers the 2-node case).
func TestStalled_SelfLoop(t *testing.T) {
	units := []string{"a"}
	deps := map[string][]string{"a": {"a"}}
	m := gateddag.NewMarkerSet(nil, nil, nil)
	if got := gateddag.SelectDispatchable(units, deps, m); len(got) != 0 {
		t.Fatalf("a self-loop leaves nothing dispatchable, got %v", got)
	}
	if !contains(gateddag.Stalled(units, deps, m), "a") {
		t.Error("a self-loop should surface as a stall, not silent idle")
	}
}

// --- determinism + Evaluate report ---

func TestEvaluate_IsDeterministicallySorted(t *testing.T) {
	units := []string{"c", "a", "b"}
	got := gateddag.Evaluate(units, nil, gateddag.NewMarkerSet(nil, nil, nil))
	ids := make([]string, len(got))
	for i, d := range got {
		ids[i] = d.UnitID
	}
	want := []string{"a", "b", "c"}
	if !reflect.DeepEqual(ids, want) {
		t.Errorf("Evaluate order: got %v, want %v (must be unit-ID sorted for determinism)", ids, want)
	}
}

func TestEvaluate_CarriesStatusAndReason(t *testing.T) {
	units := []string{"done", "dep", "blocked"}
	deps := map[string][]string{"dep": {"done"}}
	m := gateddag.NewMarkerSet([]string{"done"}, []string{"blocked"}, nil)
	byID := map[string]gateddag.Decision{}
	for _, d := range gateddag.Evaluate(units, deps, m) {
		byID[d.UnitID] = d
	}
	if d := byID["done"]; d.Status != gateddag.StatusDone || d.Dispatchable {
		t.Errorf("done: %+v", d)
	}
	if d := byID["blocked"]; d.Status != gateddag.StatusBlocked || d.Dispatchable {
		t.Errorf("blocked: %+v", d)
	}
	if d := byID["dep"]; !d.Dispatchable {
		t.Errorf("dep should dispatch (its only prereq is done): %+v", d)
	}
}

func TestEvaluate_ReasonStrings(t *testing.T) {
	units := []string{"p", "dep"}
	deps := map[string][]string{"dep": {"p"}}
	byID := map[string]gateddag.Decision{}
	for _, d := range gateddag.Evaluate(units, deps, gateddag.NewMarkerSet(nil, nil, nil)) {
		byID[d.UnitID] = d
	}
	if r := byID["dep"].Reason; r != "waiting on prerequisite p" {
		t.Errorf("held-dependent reason: got %q", r)
	}
	if r := byID["p"].Reason; r != "ready" {
		t.Errorf("ready reason: got %q", r)
	}
}

// --- #8 stall / cycle detection (and the brain half of #7) ---

// TestStalled_DetectsCycle: a→b, b→a derives nothing dispatchable, nothing
// terminal — a silent-idle deadlock. Stalled must surface the held units.
func TestStalled_DetectsCycle(t *testing.T) {
	units := []string{"a", "b"}
	deps := map[string][]string{"a": {"b"}, "b": {"a"}}
	m := gateddag.NewMarkerSet(nil, nil, nil)

	if got := gateddag.SelectDispatchable(units, deps, m); len(got) != 0 {
		t.Fatalf("a cycle should leave nothing dispatchable, got %v", got)
	}
	stalled := gateddag.Stalled(units, deps, m)
	if !contains(stalled, "a") || !contains(stalled, "b") {
		t.Errorf("Stalled should surface the deadlocked units, got %v", stalled)
	}
}

// TestStalled_AllBlockedWait is also the brain half of #7 (failure-release): a
// dependent gated behind a Blocked prerequisite is surfaced as stalled (the
// prereq will never derive Done without recovery) — never SILENTLY stranded.
func TestStalled_AllBlockedWait(t *testing.T) {
	units := []string{"p", "dep"}
	deps := map[string][]string{"dep": {"p"}}
	m := gateddag.NewMarkerSet(nil, []string{"p"}, nil) // p failed (terminal)
	stalled := gateddag.Stalled(units, deps, m)
	if !contains(stalled, "dep") {
		t.Errorf("dep gated behind a Blocked prereq should be reported stalled: %v", stalled)
	}
	if contains(stalled, "p") {
		t.Errorf("p is Blocked (terminal), not a held-Ready stall: %v", stalled)
	}
}

// TestStalled_IndependentBranchFlows pins #7's "independent branches keep
// flowing": a failed node strands only its own dependents; an unrelated unit
// stays dispatchable, so the plan is NOT stalled.
func TestStalled_IndependentBranchFlows(t *testing.T) {
	units := []string{"p", "dep", "free"}
	deps := map[string][]string{"dep": {"p"}} // free has no prereqs
	m := gateddag.NewMarkerSet(nil, []string{"p"}, nil)
	if got := gateddag.Stalled(units, deps, m); got != nil {
		t.Errorf("an independent dispatchable unit (free) means not stalled: %v", got)
	}
	if !contains(gateddag.SelectDispatchable(units, deps, m), "free") {
		t.Error("free should dispatch — it does not depend on the failed node")
	}
}

func TestStalled_NotStalledWhenProgressing(t *testing.T) {
	units := []string{"a", "b"}
	deps := map[string][]string{"b": {"a"}}
	if got := gateddag.Stalled(units, deps, gateddag.NewMarkerSet(nil, nil, nil)); got != nil {
		t.Errorf("a plan with dispatchable work (a) is not stalled: %v", got)
	}
}

func TestStalled_NotStalledWhenComplete(t *testing.T) {
	units := []string{"a", "b"}
	deps := map[string][]string{"b": {"a"}}
	m := gateddag.NewMarkerSet([]string{"a", "b"}, nil, nil)
	if got := gateddag.Stalled(units, deps, m); got != nil {
		t.Errorf("a completed plan is not stalled: %v", got)
	}
}

func TestStalled_EmptyPlanNotStalled(t *testing.T) {
	if got := gateddag.Stalled(nil, nil, gateddag.NewMarkerSet(nil, nil, nil)); got != nil {
		t.Errorf("an empty plan is not stalled: %v", got)
	}
}
