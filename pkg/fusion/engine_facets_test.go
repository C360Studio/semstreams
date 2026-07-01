package fusion_test

import (
	"context"
	"errors"
	"testing"

	"github.com/c360studio/semstreams/pkg/fusion"
)

const refEdgePred = "ref.relationship.references"

func edge(target string) fusion.Edge {
	return fusion.Edge{Predicate: refEdgePred, Target: target}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// facetGraph builds a fakeGraph whose single seed "S" resolves under query "q",
// with caller-supplied outgoing/incoming adjacency and entity set.
func facetGraph(out, in map[string][]fusion.Edge, entities map[string]*fusion.Entity) *fakeGraph {
	return &fakeGraph{
		status:   readyStatus(),
		seeds:    map[string][]string{"q": {"S"}},
		entities: entities,
		out:      out,
		in:       in,
	}
}

func fuseFacet(t *testing.T, g *fakeGraph, want fusion.Want) fusion.Response {
	t.Helper()
	eng := fusion.NewEngine(g, nil)
	resp, err := eng.Fuse(context.Background(), fusion.Request{Query: "q", Want: []fusion.Want{want}}, refLens{})
	if err != nil {
		t.Fatalf("Fuse: %v", err)
	}
	return resp
}

// --- Impact: transitive reverse-relation closure ---

func TestImpact_TransitiveClosureCountsNodesAndFiles(t *testing.T) {
	// S ← A, S ← B, A ← C. Reverse closure of S = {A,B,C}; seed S excluded.
	entities := map[string]*fusion.Entity{
		"S": entity("S", "S", "s.go"),
		"A": entity("A", "A", "a.go"),
		"B": entity("B", "B", "b.go"),
		"C": entity("C", "C", "c.go"),
	}
	in := map[string][]fusion.Edge{
		"S": {edge("A"), edge("B")}, // A and B point at S
		"A": {edge("C")},            // C points at A
	}
	g := facetGraph(nil, in, entities)

	resp := fuseFacet(t, g, fusion.WantImpact)
	if resp.Impact == nil {
		t.Fatal("Impact facet missing")
	}
	if resp.Impact.Nodes != 3 {
		t.Errorf("Impact.Nodes = %d, want 3 (A,B,C; seed excluded)", resp.Impact.Nodes)
	}
	if resp.Impact.Files != 3 {
		t.Errorf("Impact.Files = %d, want 3 (a.go,b.go,c.go)", resp.Impact.Files)
	}
	if resp.Impact.Truncated {
		t.Error("Impact.Truncated = true, want false (well under cap)")
	}
}

func TestImpact_ExcludesSeedAndDedupes(t *testing.T) {
	// Diamond: S ← A, S ← B, A ← D, B ← D (D reaches S two ways) + a self-ref
	// S ← S that must not count the seed.
	entities := map[string]*fusion.Entity{
		"S": entity("S", "S", "s.go"),
		"A": entity("A", "A", "a.go"),
		"B": entity("B", "B", "b.go"),
		"D": entity("D", "D", "d.go"),
	}
	in := map[string][]fusion.Edge{
		"S": {edge("A"), edge("B"), edge("S")}, // self-edge must be ignored
		"A": {edge("D")},
		"B": {edge("D")}, // D discovered once (dedup)
	}
	g := facetGraph(nil, in, entities)

	resp := fuseFacet(t, g, fusion.WantImpact)
	if resp.Impact.Nodes != 3 {
		t.Errorf("Impact.Nodes = %d, want 3 (A,B,D; seed + dup excluded)", resp.Impact.Nodes)
	}
}

func TestImpact_NotRequestedIsNil(t *testing.T) {
	entities := map[string]*fusion.Entity{"S": entity("S", "S", "s.go")}
	in := map[string][]fusion.Edge{"S": {edge("A")}}
	g := facetGraph(nil, in, entities)

	resp := fuseFacet(t, g, fusion.WantRelations) // not WantImpact
	if resp.Impact != nil {
		t.Errorf("Impact = %+v, want nil (not requested)", resp.Impact)
	}
}

// --- Paths: bounded outgoing DFS ---

func TestPaths_LinearChain(t *testing.T) {
	// S → A → B (leaf).
	entities := map[string]*fusion.Entity{
		"S": entity("S", "S", "s.go"),
		"A": entity("A", "A", "a.go"),
		"B": entity("B", "B", "b.go"),
	}
	out := map[string][]fusion.Edge{
		"S": {edge("A")},
		"A": {edge("B")},
	}
	g := facetGraph(out, nil, entities)

	resp := fuseFacet(t, g, fusion.WantPaths)
	if len(resp.Paths) != 1 {
		t.Fatalf("got %d paths, want 1", len(resp.Paths))
	}
	if want := []string{"S", "A", "B"}; !equalStrings(resp.Paths[0].Names, want) {
		t.Errorf("path = %v, want %v", resp.Paths[0].Names, want)
	}
	if resp.Paths[0].Truncated {
		t.Error("linear chain path must not be truncated")
	}
}

func TestPaths_BranchesEmitOnePerLeaf(t *testing.T) {
	// S → {A, B}, both leaves.
	entities := map[string]*fusion.Entity{
		"S": entity("S", "S", "s.go"),
		"A": entity("A", "A", "a.go"),
		"B": entity("B", "B", "b.go"),
	}
	out := map[string][]fusion.Edge{"S": {edge("A"), edge("B")}}
	g := facetGraph(out, nil, entities)

	resp := fuseFacet(t, g, fusion.WantPaths)
	if len(resp.Paths) != 2 {
		t.Fatalf("got %d paths, want 2", len(resp.Paths))
	}
	got := map[string]bool{}
	for _, p := range resp.Paths {
		got[p.Names[len(p.Names)-1]] = true
	}
	if !got["A"] || !got["B"] {
		t.Errorf("paths should end at A and B, got %v", resp.Paths)
	}
}

func TestPaths_CycleTruncatedNotDropped(t *testing.T) {
	// S → A → S (cycle back to seed). A's only forward edge loops → truncate.
	entities := map[string]*fusion.Entity{
		"S": entity("S", "S", "s.go"),
		"A": entity("A", "A", "a.go"),
	}
	out := map[string][]fusion.Edge{
		"S": {edge("A")},
		"A": {edge("S")}, // back to seed (on path)
	}
	g := facetGraph(out, nil, entities)

	resp := fuseFacet(t, g, fusion.WantPaths)
	if len(resp.Paths) != 1 {
		t.Fatalf("got %d paths, want 1 (cycle kept, not dropped)", len(resp.Paths))
	}
	if want := []string{"S", "A"}; !equalStrings(resp.Paths[0].Names, want) {
		t.Errorf("path = %v, want %v", resp.Paths[0].Names, want)
	}
	if !resp.Paths[0].Truncated {
		t.Error("cycle path must be marked Truncated")
	}
}

func TestPaths_DepthCapTruncates(t *testing.T) {
	// Chain longer than maxPathDepth (4 hops): S→A→B→C→D→E. Path cut at 4 hops.
	entities := map[string]*fusion.Entity{}
	out := map[string][]fusion.Edge{}
	chain := []string{"S", "A", "B", "C", "D", "E"}
	for i, id := range chain {
		entities[id] = entity(id, id, id+".go")
		if i+1 < len(chain) {
			out[id] = []fusion.Edge{edge(chain[i+1])}
		}
	}
	g := facetGraph(out, nil, entities)

	resp := fuseFacet(t, g, fusion.WantPaths)
	if len(resp.Paths) != 1 {
		t.Fatalf("got %d paths, want 1", len(resp.Paths))
	}
	// 4 hops → 5 names (S,A,B,C,D), truncated before E.
	if want := []string{"S", "A", "B", "C", "D"}; !equalStrings(resp.Paths[0].Names, want) {
		t.Errorf("path = %v, want %v (depth-capped)", resp.Paths[0].Names, want)
	}
	if !resp.Paths[0].Truncated {
		t.Error("depth-capped path must be marked Truncated")
	}
}

func TestPaths_ForwardAndCycleFollowsForward(t *testing.T) {
	// A has both a forward edge (→B, leaf) and a cycle edge (→S, on path). The
	// forward path is followed and emitted UNtruncated; no spurious truncated
	// emit for the cycle branch.
	entities := map[string]*fusion.Entity{
		"S": entity("S", "S", "s.go"),
		"A": entity("A", "A", "a.go"),
		"B": entity("B", "B", "b.go"),
	}
	out := map[string][]fusion.Edge{
		"S": {edge("A")},
		"A": {edge("B"), edge("S")}, // forward to B, cycle back to S
	}
	g := facetGraph(out, nil, entities)

	resp := fuseFacet(t, g, fusion.WantPaths)
	if len(resp.Paths) != 1 {
		t.Fatalf("got %d paths, want 1 (forward followed, cycle not a separate emit)", len(resp.Paths))
	}
	if want := []string{"S", "A", "B"}; !equalStrings(resp.Paths[0].Names, want) {
		t.Errorf("path = %v, want %v", resp.Paths[0].Names, want)
	}
	if resp.Paths[0].Truncated {
		t.Error("a branch with a forward continuation must not be marked Truncated")
	}
}

func TestPaths_MaxPathsCap(t *testing.T) {
	// A star with many leaves (> maxPaths) must emit exactly maxPaths(10) paths.
	entities := map[string]*fusion.Entity{"S": entity("S", "S", "s.go")}
	var star []fusion.Edge
	for i := range 25 {
		id := "leaf" + string(rune('A'+i))
		entities[id] = entity(id, id, id+".go")
		star = append(star, edge(id))
	}
	g := facetGraph(map[string][]fusion.Edge{"S": star}, nil, entities)

	resp := fuseFacet(t, g, fusion.WantPaths)
	if len(resp.Paths) != 10 {
		t.Errorf("got %d paths, want exactly maxPaths=10", len(resp.Paths))
	}
}

func TestImpact_NodeCapTruncates(t *testing.T) {
	// A fan of > maxImpactNodes(200) dependents all pointing at S. Impact caps at
	// 200 with Truncated set.
	entities := map[string]*fusion.Entity{"S": entity("S", "S", "s.go")}
	var deps []fusion.Edge
	for i := range 250 {
		id := "dep" + string(rune(i))
		entities[id] = entity(id, id, id+".go")
		deps = append(deps, edge(id))
	}
	g := facetGraph(nil, map[string][]fusion.Edge{"S": deps}, entities)

	resp := fuseFacet(t, g, fusion.WantImpact)
	if resp.Impact.Nodes != 200 {
		t.Errorf("Impact.Nodes = %d, want 200 (capped)", resp.Impact.Nodes)
	}
	if !resp.Impact.Truncated {
		t.Error("Impact.Truncated must be set when the node cap is hit")
	}
}

func TestImpact_WalkFaultIsTruncatedNotExact(t *testing.T) {
	// A Neighbors backend fault must surface as Truncated (counts are a lower
	// bound), NOT a silent exact-looking zero — the honesty-envelope contract.
	g := facetGraph(nil, nil, map[string]*fusion.Entity{"S": entity("S", "S", "s.go")})
	g.neighborsErr = errors.New("backend down")

	resp := fuseFacet(t, g, fusion.WantImpact)
	if resp.Impact == nil {
		t.Fatal("Impact facet missing")
	}
	if !resp.Impact.Truncated {
		t.Error("a Neighbors fault must set Impact.Truncated (partial result, not exact)")
	}
}

func TestPaths_WalkFaultTruncatesPartialPath(t *testing.T) {
	// S → A, but Neighbors faults: the seed's own walk fails, so no path is
	// emitted (nothing beyond the seed is known); a fault deeper would truncate.
	entities := map[string]*fusion.Entity{
		"S": entity("S", "S", "s.go"),
		"A": entity("A", "A", "a.go"),
	}
	g := facetGraph(map[string][]fusion.Edge{"S": {edge("A")}}, nil, entities)
	g.neighborsErr = errors.New("backend down")

	resp := fuseFacet(t, g, fusion.WantPaths)
	// The seed's Neighbors fails → len(path)==1 → no emit (a single-name path is
	// never emitted). The point: no PANIC and no false complete path.
	if len(resp.Paths) != 0 {
		t.Errorf("got %d paths, want 0 (seed walk faulted before any hop)", len(resp.Paths))
	}
}

func TestPaths_NotRequestedIsNil(t *testing.T) {
	entities := map[string]*fusion.Entity{
		"S": entity("S", "S", "s.go"),
		"A": entity("A", "A", "a.go"),
	}
	out := map[string][]fusion.Edge{"S": {edge("A")}}
	g := facetGraph(out, nil, entities)

	resp := fuseFacet(t, g, fusion.WantRelations) // not WantPaths
	if resp.Paths != nil {
		t.Errorf("Paths = %v, want nil (not requested)", resp.Paths)
	}
}
