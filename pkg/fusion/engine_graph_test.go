package fusion_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/internal/semantictest"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/pkg/fusion"
)

// Graph projection facet tests (gh#533). Each of the issue's eight acceptance
// criteria maps to a test below:
//
//  1. ID-shaped literal stays a fact        → TestGraph_IDShapedLiteralStaysFact
//  2. parallel predicates are two edges     → TestGraph_ParallelPredicates_TwoEdges
//  3. opposite directions stay distinct     → TestGraph_OppositeDirections_DistinctEdges
//  4. evidence merges on one edge           → TestGraph_SameEdgeEvidence_OneEdgeTwoEntries
//  5. absent evidence stays absent          → TestGraph_AbsentEvidence_OmittedFromWire
//  6. truncation observable + independent   → TestGraph_FactCap_ObservableAndIndependent
//  7. view-revision observations            → TestGraph_ViewRevision
//  8. v1 requests unaffected                → TestGraph_NotRequested_V1ShapeUnchanged

// fuseGraph runs a graph-want fuse over g through lens, failing the test on a
// fuse error or a missing projection.
func fuseGraph(t *testing.T, g *fakeGraph, lens fusion.Lens) fusion.Response {
	t.Helper()
	eng := fusion.NewEngine(g, nil)
	resp, err := eng.Fuse(context.Background(), fusion.Request{Query: "q", Want: []fusion.Want{fusion.WantGraph}}, lens)
	if err != nil {
		t.Fatalf("Fuse: %v", err)
	}
	if resp.Graph == nil {
		t.Fatal("expected a graph projection when the graph facet is wanted")
	}
	return resp
}

// graphSeed builds a fakeGraph whose single seed resolves under query "q".
func graphSeed(seed *fusion.Entity, extra ...*fusion.Entity) *fakeGraph {
	entities := map[string]*fusion.Entity{seed.ID: seed}
	for _, e := range extra {
		entities[e.ID] = e
	}
	return &fakeGraph{
		status:   readyStatus(),
		seeds:    map[string][]string{"q": {seed.ID}},
		entities: entities,
	}
}

// findEdge returns the projection edge with the given source and target under
// predicate, or fails the test.
func findEdge(t *testing.T, proj *fusion.GraphProjection, source, predicate, target string) fusion.GraphEdge {
	t.Helper()
	for _, ed := range proj.Edges {
		if ed.Source == source && ed.Predicate == predicate && ed.Target == target {
			return ed
		}
	}
	t.Fatalf("no edge %s -[%s]-> %s in %+v", source, predicate, target, proj.Edges)
	return fusion.GraphEdge{}
}

// findFact returns the fact with the given predicate on node, or fails.
func findFact(t *testing.T, node fusion.GraphNode, predicate string) fusion.GraphFact {
	t.Helper()
	for _, f := range node.Facts {
		if f.Predicate == predicate {
			return f
		}
	}
	t.Fatalf("no fact %q on node %q: %+v", predicate, node.Handle, node.Facts)
	return fusion.GraphFact{}
}

// TestGraph_IDShapedLiteralStaysFact (acceptance 1): a string value with
// perfect six-part entity-ID shape, an EMPTY datatype, and an undeclared
// predicate is a property fact — never an edge. This is the declaration-driven
// contract: Triple.IsRelationship's empty-datatype branch would shape-sniff
// this value into a relationship, which is exactly what the facet must not do.
func TestGraph_IDShapedLiteralStaysFact(t *testing.T) {
	const idShaped = "acme.ops.robotics.gcs.drone.001"
	tr := message.Triple{Predicate: "acme.related.hint", Object: idShaped, Source: "importer"}
	// Precondition: prove the fixture actually trips the shape-sniffing trap.
	if !tr.IsRelationship() {
		t.Fatalf("fixture must be ID-shaped enough for IsRelationship to shape-sniff it: %+v", tr)
	}

	seed := &fusion.Entity{ID: "S", Triples: []message.Triple{tr}}
	resp := fuseGraph(t, graphSeed(seed), refLens{})

	if len(resp.Graph.Edges) != 0 {
		t.Errorf("edges = %+v, want none (undeclared predicate + empty datatype must not project an edge)", resp.Graph.Edges)
	}
	if len(resp.Graph.Nodes) != 1 {
		t.Fatalf("nodes = %d, want 1 (the seed)", len(resp.Graph.Nodes))
	}
	fact := findFact(t, resp.Graph.Nodes[0], "acme.related.hint")
	if string(fact.Value) != fmt.Sprintf("%q", idShaped) {
		t.Errorf("fact value = %s, want the verbatim JSON string %q", fact.Value, idShaped)
	}
	if fact.Datatype != "" {
		t.Errorf("fact datatype = %q, want absent (verbatim: absent stays absent)", fact.Datatype)
	}
}

// TestGraph_ParallelPredicates_TwoEdges (acceptance 2): two lens-declared
// predicates linking the same pair are two edges, each with its verbatim
// predicate. Uses facetedLens, whose second predicate is restricted to the
// relations facet — proving the graph facet draws on the lens's WHOLE declared
// edge vocabulary, not one walk's facet subset.
func TestGraph_ParallelPredicates_TwoEdges(t *testing.T) {
	seed := &fusion.Entity{ID: "A", Triples: []message.Triple{
		{Predicate: refEdgePred, Object: "B", Source: "walker"},
		{Predicate: containsPred, Object: "B", Source: "walker"},
	}}
	resp := fuseGraph(t, graphSeed(seed), facetedLens{})

	if len(resp.Graph.Edges) != 2 {
		t.Fatalf("edges = %d, want 2 (parallel predicates must not collapse): %+v", len(resp.Graph.Edges), resp.Graph.Edges)
	}
	ref := findEdge(t, resp.Graph, "A", refEdgePred, "B")
	con := findEdge(t, resp.Graph, "A", containsPred, "B")
	if ref.ID == con.ID {
		t.Errorf("edge IDs collide: %q — identity must include the predicate", ref.ID)
	}
	for _, ed := range []fusion.GraphEdge{ref, con} {
		if ed.Direction != fusion.GraphDirectionOutgoing {
			t.Errorf("edge %q direction = %q, want outgoing (seed is the subject)", ed.ID, ed.Direction)
		}
	}
}

// TestGraph_OppositeDirections_DistinctEdges (acceptance 3): a fact A→B and a
// fact B→A are two edges with swapped source/target in true subject→object
// orientation — neither collapses into the other. The incoming edge's evidence
// comes from the counterpart's own triple (the fetch the walk already does).
func TestGraph_OppositeDirections_DistinctEdges(t *testing.T) {
	seed := &fusion.Entity{ID: "A", Triples: []message.Triple{
		{Predicate: refEdgePred, Object: "B", Source: "a-asserts"},
	}}
	counterpart := &fusion.Entity{ID: "B", Triples: []message.Triple{
		{Predicate: refEdgePred, Object: "A", Source: "b-asserts"},
	}}
	g := graphSeed(seed, counterpart)
	g.in = map[string][]fusion.Edge{"A": {{Predicate: refEdgePred, Target: "B"}}} // B points at A
	resp := fuseGraph(t, g, refLens{})

	if len(resp.Graph.Edges) != 2 {
		t.Fatalf("edges = %d, want 2 (opposite directions are distinct): %+v", len(resp.Graph.Edges), resp.Graph.Edges)
	}
	out := findEdge(t, resp.Graph, "A", refEdgePred, "B")
	in := findEdge(t, resp.Graph, "B", refEdgePred, "A")
	if out.Direction != fusion.GraphDirectionOutgoing {
		t.Errorf("A→B direction = %q, want outgoing", out.Direction)
	}
	if in.Direction != fusion.GraphDirectionIncoming {
		t.Errorf("B→A direction = %q, want incoming (seed is the object)", in.Direction)
	}
	if len(out.Evidence) != 1 || out.Evidence[0].Source != "a-asserts" {
		t.Errorf("A→B evidence = %+v, want the seed's own triple (a-asserts)", out.Evidence)
	}
	if len(in.Evidence) != 1 || in.Evidence[0].Source != "b-asserts" {
		t.Errorf("B→A evidence = %+v, want the counterpart's triple (b-asserts)", in.Evidence)
	}
}

// TestGraph_SameEdgeEvidence_OneEdgeTwoEntries (acceptance 4): two triples
// asserting the same (source, predicate, target) with different evidence are
// ONE edge carrying BOTH evidence entries, each inspectable.
func TestGraph_SameEdgeEvidence_OneEdgeTwoEntries(t *testing.T) {
	seed := &fusion.Entity{ID: "A", Triples: []message.Triple{
		{Predicate: refEdgePred, Object: "B", Source: "sensor-1", Confidence: 0.9},
		{Predicate: refEdgePred, Object: "B", Source: "sensor-2", Confidence: 0.7},
	}}
	resp := fuseGraph(t, graphSeed(seed), refLens{})

	if len(resp.Graph.Edges) != 1 {
		t.Fatalf("edges = %d, want 1 (same semantic edge merges): %+v", len(resp.Graph.Edges), resp.Graph.Edges)
	}
	ed := resp.Graph.Edges[0]
	if len(ed.Evidence) != 2 {
		t.Fatalf("evidence = %d entries, want 2 (contributions stay inspectable): %+v", len(ed.Evidence), ed.Evidence)
	}
	byCandidate := map[string]float64{}
	for _, ev := range ed.Evidence {
		if ev.Confidence == nil {
			t.Fatalf("evidence %+v lost its confidence", ev)
		}
		byCandidate[ev.Source] = *ev.Confidence
	}
	if byCandidate["sensor-1"] != 0.9 || byCandidate["sensor-2"] != 0.7 {
		t.Errorf("evidence entries = %v, want sensor-1→0.9 and sensor-2→0.7", byCandidate)
	}
}

// TestGraph_BothEndpointsSeeds_SameAssertionNotDoubled: when both endpoints of
// an edge are seeds, the SAME stored assertion is discovered twice — outgoing
// from the subject seed's triple and incoming via the object seed's walk
// (counterpart fetch returns the same triple). It must stay ONE edge with ONE
// evidence entry, not a doubled entry.
func TestGraph_BothEndpointsSeeds_SameAssertionNotDoubled(t *testing.T) {
	a := &fusion.Entity{ID: "A", Triples: []message.Triple{
		{Predicate: refEdgePred, Object: "B", Source: "a-asserts"},
	}}
	b := &fusion.Entity{ID: "B"}
	g := &fakeGraph{
		status:   readyStatus(),
		seeds:    map[string][]string{"q": {"A", "B"}},
		entities: map[string]*fusion.Entity{"A": a, "B": b},
		in:       map[string][]fusion.Edge{"B": {{Predicate: refEdgePred, Target: "A"}}}, // A points at B
	}
	resp := fuseGraph(t, g, refLens{})

	if len(resp.Graph.Edges) != 1 {
		t.Fatalf("edges = %d, want 1 (one assertion, discovered from both endpoints): %+v", len(resp.Graph.Edges), resp.Graph.Edges)
	}
	if got := resp.Graph.Edges[0].Evidence; len(got) != 1 || got[0].Source != "a-asserts" {
		t.Errorf("evidence = %+v, want exactly one a-asserts entry (no double-count)", got)
	}
}

// TestGraph_AbsentEvidence_OmittedFromWire (acceptance 5): a stored triple with
// no confidence and no context projects evidence that OMITS those keys from the
// marshaled JSON — pointer-confidence means absent is distinguishable from 0
// and nothing is ever zero-filled.
func TestGraph_AbsentEvidence_OmittedFromWire(t *testing.T) {
	when := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

	t.Run("absent stays absent", func(t *testing.T) {
		seed := &fusion.Entity{ID: "S", Triples: []message.Triple{
			{Predicate: "acme.note.text", Object: "hello", Source: "importer", Timestamp: when},
		}}
		resp := fuseGraph(t, graphSeed(seed), refLens{})

		fact := findFact(t, resp.Graph.Nodes[0], "acme.note.text")
		if len(fact.Evidence) != 1 || fact.Evidence[0].Confidence != nil {
			t.Fatalf("evidence = %+v, want one entry with nil Confidence", fact.Evidence)
		}
		raw, err := json.Marshal(resp.Graph)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		for _, key := range []string{`"confidence"`, `"context"`} {
			if strings.Contains(string(raw), key) {
				t.Errorf("marshaled projection carries %s despite the value being absent:\n%s", key, raw)
			}
		}
		if !strings.Contains(string(raw), `"timestamp":"2026-01-02T03:04:05Z"`) {
			t.Errorf("marshaled projection lost the RFC3339 timestamp:\n%s", raw)
		}
	})

	t.Run("present values project verbatim", func(t *testing.T) {
		seed := &fusion.Entity{ID: "S", Triples: []message.Triple{
			{Predicate: "acme.note.text", Object: "hello", Source: "importer", Confidence: 0.75, Context: "batch-7"},
		}}
		resp := fuseGraph(t, graphSeed(seed), refLens{})

		raw, err := json.Marshal(resp.Graph)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if !strings.Contains(string(raw), `"confidence":0.75`) || !strings.Contains(string(raw), `"context":"batch-7"`) {
			t.Errorf("marshaled projection lost present evidence values:\n%s", raw)
		}
	})
}

// TestGraph_FactCap_ObservableAndIndependent (acceptance 6): a seed whose fact
// count exceeds the per-node cap reports FactsTruncated + FactsDropped and the
// projection-level Truncated flag — while the v1 top-level Response.Truncated
// stays untouched (the graph facet never overloads the v1 budget signal).
func TestGraph_FactCap_ObservableAndIndependent(t *testing.T) {
	// 67 distinct facts against the cap of 64 → 3 dropped.
	triples := make([]message.Triple, 0, 67)
	for i := range 67 {
		triples = append(triples, message.Triple{
			Predicate: semantictest.Predicate(t, "acme", "fact", "cap"),
			Object:    i,
		})
	}
	seed := &fusion.Entity{ID: "S", Triples: triples}
	resp := fuseGraph(t, graphSeed(seed), refLens{})

	node := resp.Graph.Nodes[0]
	if len(node.Facts) != 64 {
		t.Errorf("facts = %d, want the cap of 64", len(node.Facts))
	}
	if !node.FactsTruncated || node.FactsDropped != 3 {
		t.Errorf("FactsTruncated=%v FactsDropped=%d, want true/3", node.FactsTruncated, node.FactsDropped)
	}
	if !resp.Graph.Truncated {
		t.Error("projection Truncated must be set on fact truncation")
	}
	if resp.Truncated {
		t.Error("v1 Response.Truncated must be UNAFFECTED by graph-facet truncation")
	}
}

// TestGraph_EdgeCap_Truncates: more distinct edges than the projection cap of
// 256 keeps the first 256 and sets the projection Truncated flag.
func TestGraph_EdgeCap_Truncates(t *testing.T) {
	triples := make([]message.Triple, 0, 260)
	for i := range 260 {
		triples = append(triples, message.Triple{Predicate: refEdgePred, Object: fmt.Sprintf("t%03d", i)})
	}
	seed := &fusion.Entity{ID: "S", Triples: triples}
	resp := fuseGraph(t, graphSeed(seed), refLens{})

	if len(resp.Graph.Edges) != 256 {
		t.Errorf("edges = %d, want the cap of 256", len(resp.Graph.Edges))
	}
	if !resp.Graph.Truncated {
		t.Error("projection Truncated must be set on edge truncation")
	}
	if resp.Truncated {
		t.Error("v1 Response.Truncated must be unaffected by graph-facet truncation")
	}
}

// TestGraph_EvidenceCap_Truncates: more contributions than the per-edge
// evidence cap of 8 keeps 8 entries and flags the EDGE truncated (plus the
// projection flag) — a capped evidence list is never silent.
func TestGraph_EvidenceCap_Truncates(t *testing.T) {
	triples := make([]message.Triple, 0, 10)
	for i := range 10 {
		triples = append(triples, message.Triple{Predicate: refEdgePred, Object: "B", Source: fmt.Sprintf("sensor-%d", i)})
	}
	seed := &fusion.Entity{ID: "S", Triples: triples}
	resp := fuseGraph(t, graphSeed(seed), refLens{})

	if len(resp.Graph.Edges) != 1 {
		t.Fatalf("edges = %d, want 1", len(resp.Graph.Edges))
	}
	ed := resp.Graph.Edges[0]
	if len(ed.Evidence) != 8 {
		t.Errorf("evidence = %d entries, want the cap of 8", len(ed.Evidence))
	}
	if !ed.Truncated {
		t.Error("edge Truncated must be set when its evidence list is capped")
	}
	if !resp.Graph.Truncated {
		t.Error("projection Truncated must be set on evidence truncation")
	}
}

// TestGraph_ViewRevision (acceptance 7): the projection reports the indexed
// revision sampled before resolve (Start) and re-sampled after the facet's
// last graph fetch (End) — OBSERVATIONS of the assembly window, never a
// coherence claim (the former Coherent bool was removed; ADR-083). A failed
// re-sample degrades to End=0 rather than guessing.
func TestGraph_ViewRevision(t *testing.T) {
	seedEntity := func() *fusion.Entity {
		return &fusion.Entity{ID: "S", Triples: []message.Triple{{Predicate: "acme.note.text", Object: "x"}}}
	}
	statusAt := func(rev uint64) fusion.IndexStatus {
		return fusion.IndexStatus{Ready: true, State: fusion.StateReady,
			BootstrapComplete: true, IndexedRevision: rev}
	}

	t.Run("stable bounds when the revision holds", func(t *testing.T) {
		g := graphSeed(seedEntity())
		g.statusFn = func(int) (fusion.IndexStatus, error) { return statusAt(100), nil }
		resp := fuseGraph(t, g, refLens{})

		want := fusion.ViewRevision{Start: 100, End: 100}
		if resp.Graph.ViewRevision != want {
			t.Errorf("view revision = %+v, want %+v", resp.Graph.ViewRevision, want)
		}
		if g.statusCalls != 2 {
			t.Errorf("status calls = %d, want 2 (pre-resolve sample + post-fetch re-sample)", g.statusCalls)
		}
	})

	t.Run("spanning bounds when the revision advances", func(t *testing.T) {
		g := graphSeed(seedEntity())
		g.statusFn = func(call int) (fusion.IndexStatus, error) {
			if call == 1 {
				return statusAt(100), nil
			}
			return statusAt(105), nil // the index advanced during the fetch phase
		}
		resp := fuseGraph(t, g, refLens{})

		want := fusion.ViewRevision{Start: 100, End: 105}
		if resp.Graph.ViewRevision != want {
			t.Errorf("view revision = %+v, want %+v (the observed span must be reported verbatim)", resp.Graph.ViewRevision, want)
		}
	})

	t.Run("failed re-sample degrades honestly", func(t *testing.T) {
		g := graphSeed(seedEntity())
		g.statusFn = func(call int) (fusion.IndexStatus, error) {
			if call == 1 {
				return statusAt(100), nil
			}
			return fusion.IndexStatus{}, errors.New("status backend down")
		}
		resp := fuseGraph(t, g, refLens{})

		want := fusion.ViewRevision{Start: 100, End: 0}
		if resp.Graph.ViewRevision != want {
			t.Errorf("view revision = %+v, want %+v (never guess a revision)", resp.Graph.ViewRevision, want)
		}
	})

	// The wire must carry NO coherence claim: the field was deleted (ADR-083)
	// because two heartbeat-grained samples agreeing cannot prove the reads
	// between them hit one revision — consumers needing a coherent view use
	// pkg/graphview. A resurrected "coherent" key is a contract regression.
	t.Run("wire carries no coherent field", func(t *testing.T) {
		g := graphSeed(seedEntity())
		g.statusFn = func(int) (fusion.IndexStatus, error) { return statusAt(100), nil }
		resp := fuseGraph(t, g, refLens{})

		raw, err := json.Marshal(resp.Graph.ViewRevision)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if strings.Contains(string(raw), "coherent") {
			t.Errorf("view revision JSON must not claim coherence:\n%s", raw)
		}
	})
}

// TestGraph_NotRequested_V1ShapeUnchanged (acceptance 8): a request WITHOUT the
// graph want carries no projection and marshals with no "graph" key at all —
// the v1 wire shape is byte-identical for non-requesting callers, and the
// default want-set does not include the facet.
func TestGraph_NotRequested_V1ShapeUnchanged(t *testing.T) {
	for name, wants := range map[string][]fusion.Want{
		"default want-set": nil,
		"explicit v1 want": {fusion.WantBody, fusion.WantRelations},
	} {
		t.Run(name, func(t *testing.T) {
			seed := &fusion.Entity{ID: "S", Triples: []message.Triple{
				{Predicate: refTitlePredicate, Object: "S"},
			}}
			g := graphSeed(seed)
			eng := fusion.NewEngine(g, nil)
			resp, err := eng.Fuse(context.Background(), fusion.Request{Query: "q", Want: wants}, refLens{})
			if err != nil {
				t.Fatalf("Fuse: %v", err)
			}
			if resp.Graph != nil {
				t.Fatalf("Graph = %+v, want nil (facet not requested)", resp.Graph)
			}
			raw, err := json.Marshal(resp)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if strings.Contains(string(raw), `"graph"`) {
				t.Errorf("v1 response JSON must carry no graph key:\n%s", raw)
			}
			if g.statusCalls != 1 {
				t.Errorf("status calls = %d, want 1 (no re-sample without the graph facet)", g.statusCalls)
			}
		})
	}
}

// TestGraph_IDDatatypedUndeclaredPredicate_Edge: a triple carrying the explicit
// "@id" entity-reference datatype projects as an edge even when its predicate
// has no lens role — the projection is generic — and its target stays a
// handle-only reference (no counterpart fetch required, no Ref semantics).
func TestGraph_IDDatatypedUndeclaredPredicate_Edge(t *testing.T) {
	const target = "acme.ops.robotics.gcs.pad.007" // deliberately absent from the graph
	seed := &fusion.Entity{ID: "S", Triples: []message.Triple{
		{Predicate: "acme.custom.link", Object: target, Datatype: message.EntityReferenceDatatype, Source: "importer"},
	}}
	resp := fuseGraph(t, graphSeed(seed), refLens{})

	if len(resp.Graph.Edges) != 1 {
		t.Fatalf("edges = %d, want 1 (explicit @id datatype projects an edge): %+v", len(resp.Graph.Edges), resp.Graph.Edges)
	}
	ed := findEdge(t, resp.Graph, "S", "acme.custom.link", target)
	if ed.Direction != fusion.GraphDirectionOutgoing {
		t.Errorf("direction = %q, want outgoing", ed.Direction)
	}
	// The target is handle-only: only the seed projects as a node.
	if len(resp.Graph.Nodes) != 1 || resp.Graph.Nodes[0].Handle != "S" {
		t.Errorf("nodes = %+v, want only the seed (targets stay handles)", resp.Graph.Nodes)
	}
	// And the @id triple is an edge, not a fact.
	for _, f := range resp.Graph.Nodes[0].Facts {
		if f.Predicate == "acme.custom.link" {
			t.Errorf("the @id-datatyped triple must not also project as a fact: %+v", f)
		}
	}
}

// TestGraph_IncomingCounterpartAbsent_EdgeWithoutEvidence: an index-known
// incoming edge whose counterpart entity cannot be fetched still projects —
// with EMPTY evidence (absent, never fabricated), per degrade-don't-fail.
func TestGraph_IncomingCounterpartAbsent_EdgeWithoutEvidence(t *testing.T) {
	seed := &fusion.Entity{ID: "S"}
	g := graphSeed(seed)
	g.in = map[string][]fusion.Edge{"S": {{Predicate: refEdgePred, Target: "GHOST"}}} // GHOST not in entities
	resp := fuseGraph(t, g, refLens{})

	ed := findEdge(t, resp.Graph, "GHOST", refEdgePred, "S")
	if len(ed.Evidence) != 0 {
		t.Errorf("evidence = %+v, want none (a missing counterpart must not fabricate provenance)", ed.Evidence)
	}
}

// TestGraph_IncomingWalkFault_MarksTruncated: a Neighbors backend fault leaves
// the incoming edge set incomplete — the projection must say so (Truncated)
// while the seed-triple-derived outgoing edges still project.
func TestGraph_IncomingWalkFault_MarksTruncated(t *testing.T) {
	seed := &fusion.Entity{ID: "S", Triples: []message.Triple{
		{Predicate: refEdgePred, Object: "B", Source: "s-asserts"},
	}}
	g := graphSeed(seed)
	g.neighborsErr = errors.New("backend down")
	resp := fuseGraph(t, g, refLens{})

	if len(resp.Graph.Edges) != 1 {
		t.Fatalf("edges = %d, want 1 (outgoing edges come from the seed's own triples)", len(resp.Graph.Edges))
	}
	if !resp.Graph.Truncated {
		t.Error("an incoming-walk fault must mark the projection Truncated (incomplete, not exact)")
	}
}

// TestGraph_TypedFactsAndMultiValuedPredicates: values are typed JSON
// passthrough (numbers stay numbers, booleans stay booleans), a multi-valued
// predicate produces one fact per value, and duplicate assertions of the same
// (predicate, value) merge into one fact with per-contribution evidence.
func TestGraph_TypedFactsAndMultiValuedPredicates(t *testing.T) {
	seed := &fusion.Entity{ID: "S", Triples: []message.Triple{
		{Predicate: "acme.battery.level", Object: 85.5, Source: "telemetry"},
		{Predicate: "acme.flight.armed", Object: true, Source: "telemetry"},
		{Predicate: "acme.tag.name", Object: "alpha", Source: "importer-1"},
		{Predicate: "acme.tag.name", Object: "beta", Source: "importer-1"},
		{Predicate: "acme.tag.name", Object: "alpha", Source: "importer-2"}, // duplicate value, new evidence
	}}
	resp := fuseGraph(t, graphSeed(seed), refLens{})

	node := resp.Graph.Nodes[0]
	if got := string(findFact(t, node, "acme.battery.level").Value); got != "85.5" {
		t.Errorf("battery value = %s, want the raw number 85.5 (no stringification)", got)
	}
	if got := string(findFact(t, node, "acme.flight.armed").Value); got != "true" {
		t.Errorf("armed value = %s, want the raw boolean true", got)
	}
	var tags []fusion.GraphFact
	for _, f := range node.Facts {
		if f.Predicate == "acme.tag.name" {
			tags = append(tags, f)
		}
	}
	if len(tags) != 2 {
		t.Fatalf("tag facts = %d, want 2 (multi-valued predicate, one fact per value): %+v", len(tags), tags)
	}
	for _, f := range tags {
		switch string(f.Value) {
		case `"alpha"`:
			if len(f.Evidence) != 2 {
				t.Errorf("alpha evidence = %+v, want both contributing importers", f.Evidence)
			}
		case `"beta"`:
			if len(f.Evidence) != 1 {
				t.Errorf("beta evidence = %+v, want the single importer", f.Evidence)
			}
		default:
			t.Errorf("unexpected tag value %s", f.Value)
		}
	}
}

// TestGraphProjection_ResponseJSONRoundTrip: the projection survives an opaque
// Response JSON encode/decode losslessly — the proof that any adapter
// transporting the Response as JSON (the fusionnats deployment shape: the
// RetrievalClient feeds the engine, the Response travels as opaque JSON)
// carries the facet through unmodified.
func TestGraphProjection_ResponseJSONRoundTrip(t *testing.T) {
	conf := 0.85
	resp := fusion.Response{
		Index:      fusion.IndexStatus{Ready: true, State: fusion.StateReady, BootstrapComplete: true, IndexedRevision: 41},
		Provenance: fusion.ProvenanceDeterministic,
		Nodes:      []fusion.Node{{Name: "OnEvent", Handle: "acme.ops.code.repo.symbol.OnEvent"}},
		Graph: &fusion.GraphProjection{
			Nodes: []fusion.GraphNode{{
				Handle: "acme.ops.code.repo.symbol.OnEvent",
				Facts: []fusion.GraphFact{{
					Predicate: "acme.battery.level",
					Value:     json.RawMessage("85.5"),
					Datatype:  "xsd:float",
					Evidence: []fusion.GraphEvidence{{
						Source: "telemetry", Timestamp: "2026-01-02T03:04:05Z", Confidence: &conf, Context: "batch-7",
					}},
				}},
				FactsTruncated: true,
				FactsDropped:   3,
			}},
			Edges: []fusion.GraphEdge{{
				ID:        "a|p|b",
				Source:    "a",
				Target:    "b",
				Predicate: "p", // predicate-audit:invalid {"kind":"stored-predicate","value":"p","reason":"arity"}
				Direction: fusion.GraphDirectionOutgoing,
				Evidence:  []fusion.GraphEvidence{{Source: "walker"}},
				Truncated: true,
			}},
			ViewRevision: fusion.ViewRevision{Start: 41, End: 42},
			Truncated:    true,
		},
		Truncated:       false,
		ContractVersion: fusion.ContractVersion,
	}

	raw, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded fusion.Response
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(resp.Graph, decoded.Graph) {
		t.Errorf("graph projection did not survive the Response JSON round-trip:\n got %+v\nwant %+v", decoded.Graph, resp.Graph)
	}
}

// TestGraph_FactEvidenceCap_Truncates pins the reviewer M1 fix: fact evidence
// is capped like edge evidence (8), so a multi-source assertion re-stamped by
// many producers cannot turn one fact into an unbounded wire payload, and the
// cut is observable on the fact and the projection while v1 truncation stays
// untouched.
func TestGraph_FactEvidenceCap_Truncates(t *testing.T) {
	// 10 distinct-provenance assertions of the same (predicate, value)
	// against the per-fact evidence cap of 8.
	triples := make([]message.Triple, 0, 10)
	for i := range 10 {
		triples = append(triples, message.Triple{
			Subject:   "S",
			Predicate: "acme.fact.multisource",
			Object:    "same-value",
			Source:    fmt.Sprintf("producer-%02d", i),
			Timestamp: time.Date(2026, 7, 19, 0, 0, i, 0, time.UTC),
		})
	}
	seed := &fusion.Entity{ID: "S", Triples: triples}
	resp := fuseGraph(t, graphSeed(seed), refLens{})

	if len(resp.Graph.Nodes) != 1 {
		t.Fatalf("want 1 node, got %d", len(resp.Graph.Nodes))
	}
	fact := findFact(t, resp.Graph.Nodes[0], "acme.fact.multisource")
	if len(fact.Evidence) != 8 {
		t.Errorf("evidence capped at 8, got %d", len(fact.Evidence))
	}
	if !fact.Truncated {
		t.Error("the evidence cut must be observable on the fact")
	}
	if !resp.Graph.Truncated {
		t.Error("the evidence cut must set the projection Truncated flag")
	}
	if resp.Truncated {
		t.Error("v1 top-level truncation must stay untouched")
	}
}
