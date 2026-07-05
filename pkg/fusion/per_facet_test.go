package fusion_test

import (
	"context"
	"testing"

	"github.com/c360studio/semstreams/pkg/fusion"
)

// facetedLens is refLens plus a second, containment-style edge restricted to the
// relations facet — the gh#475 motivating shape (a file→symbol "contains" edge
// that must populate relations without inflating the impact/paths walks).
type facetedLens struct{ refLens }

const containsPred = "ref.relationship.contains"

func (facetedLens) Edges() []fusion.EdgeSpec {
	return []fusion.EdgeSpec{
		// Participates in all three facets (no Facets set).
		{Predicate: refEdgePred, OutgoingRole: "referent", IncomingRole: "referrer"},
		// Relations-only: feeds the relations map, excluded from impact + paths.
		{
			Predicate:    containsPred,
			OutgoingRole: "contained",
			IncomingRole: "container",
			Facets:       []fusion.Facet{fusion.FacetRelations},
		},
	}
}

func containsEdge(target string) fusion.Edge {
	return fusion.Edge{Predicate: containsPred, Target: target}
}

func fuseFaceted(t *testing.T, g *fakeGraph, want fusion.Want) fusion.Response {
	t.Helper()
	eng := fusion.NewEngine(g, nil)
	resp, err := eng.Fuse(context.Background(), fusion.Request{Query: "q", Want: []fusion.Want{want}}, facetedLens{})
	if err != nil {
		t.Fatalf("Fuse: %v", err)
	}
	return resp
}

// TestPerFacet_ImpactExcludesRelationsOnlyEdge: the reverse-impact walk must skip
// the relations-only containment edge, so a symbol's impact is its real
// reverse-dependents (A via references) and NOT its structural container (F via
// contains). Without the facet mask, impact would be {A, F} = 2. gh#475.
func TestPerFacet_ImpactExcludesRelationsOnlyEdge(t *testing.T) {
	entities := map[string]*fusion.Entity{
		"S": entity("S", "S", "s.go"),
		"A": entity("A", "A", "a.go"), // real reverse-dependent (references)
		"F": entity("F", "F", "f.go"), // structural container (contains)
	}
	in := map[string][]fusion.Edge{
		"S": {edge("A"), containsEdge("F")}, // S ← A (references), S ← F (contains)
	}
	resp := fuseFaceted(t, facetGraph(nil, in, entities), fusion.WantImpact)

	if resp.Impact == nil {
		t.Fatal("expected an Impact facet")
	}
	if resp.Impact.Nodes != 1 {
		t.Fatalf("impact nodes = %d, want 1 (only the real reverse-dependent A; the "+
			"relations-only containment edge to F must be excluded)", resp.Impact.Nodes)
	}
}

// TestPerFacet_RelationsIncludesRelationsOnlyEdge: the same containment edge MUST
// still populate the relations map (that is why the lens declares it).
func TestPerFacet_RelationsIncludesRelationsOnlyEdge(t *testing.T) {
	entities := map[string]*fusion.Entity{
		"S": entity("S", "S", "s.go"),
		"A": entity("A", "A", "a.go"),
		"F": entity("F", "F", "f.go"),
	}
	in := map[string][]fusion.Edge{
		"S": {edge("A"), containsEdge("F")},
	}
	resp := fuseFaceted(t, facetGraph(nil, in, entities), fusion.WantRelations)

	if len(resp.Nodes) == 0 {
		t.Fatal("expected at least the seed node")
	}
	rels := resp.Nodes[0].Relations
	if len(rels["referrer"]) != 1 {
		t.Errorf("relations[referrer] = %v, want the references dependent A", rels["referrer"])
	}
	if len(rels["container"]) != 1 {
		t.Errorf("relations[container] = %v, want the containment edge F (relations-only "+
			"edges must still populate the relations map)", rels["container"])
	}
}

// TestPerFacet_PathsExcludesRelationsOnlyEdge: the outgoing paths walk must skip
// the relations-only edge, following only the all-facets references edge.
func TestPerFacet_PathsExcludesRelationsOnlyEdge(t *testing.T) {
	entities := map[string]*fusion.Entity{
		"S": entity("S", "S", "s.go"),
		"X": entity("X", "X", "x.go"), // reachable via references (walked)
		"Y": entity("Y", "Y", "y.go"), // reachable via contains (excluded)
	}
	out := map[string][]fusion.Edge{
		"S": {edge("X"), containsEdge("Y")},
	}
	resp := fuseFaceted(t, facetGraph(out, nil, entities), fusion.WantPaths)

	sawX, sawY := false, false
	for _, p := range resp.Paths {
		for _, name := range p.Names {
			switch name {
			case "X":
				sawX = true
			case "Y":
				sawY = true
			}
		}
	}
	if sawY {
		t.Fatal("paths reached Y via the relations-only contains edge; it must be excluded from the paths walk")
	}
	if !sawX {
		t.Error("expected paths to reach X via the all-facets references edge")
	}
}
