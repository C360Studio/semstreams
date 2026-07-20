package fusion

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/c360studio/semstreams/message"
)

// The graph projection facet (gh#533). WantGraph adds a lossless, structured
// projection of the response's seed entities — typed property facts, explicit
// directed edges, and per-fact/per-edge evidence — so structured consumers
// (e.g. a knowledge-workbench drill-down) never reconstruct graph structure by
// shape-sniffing six-part strings or fabricating provenance. Classification is
// declaration-driven (lens EdgeSpecs or the explicit "@id" entity-reference
// datatype), never value-shape-driven; the engine owns these generic
// semantics, the lens keeps declaring only domain predicates and human roles.

// Graph-facet caps. Deliberately independent of the v1 node/body budgeter and
// of the relations facet's maxRelationsPerNode: the graph facet does not
// consume the body byte budget, and its truncation is reported through its own
// metadata — never through the top-level Response.Truncated.
const (
	// maxGraphFactsPerNode caps property facts per projected node; overflow is
	// reported via GraphNode.FactsTruncated + FactsDropped.
	maxGraphFactsPerNode = 64
	// maxGraphEdges caps edges per projection; overflow sets
	// GraphProjection.Truncated.
	maxGraphEdges = 256
	// maxGraphEvidencePerEdge caps evidence entries per edge; overflow sets
	// GraphEdge.Truncated.
	maxGraphEvidencePerEdge = 8
	// maxGraphEvidencePerFact caps evidence entries per fact (mirror of the
	// edge cap); overflow sets GraphFact.Truncated and the projection's
	// Truncated — a multi-source assertion re-stamped by many producers must
	// not turn one fact into an unbounded wire payload.
	maxGraphEvidencePerFact = 8
)

// GraphEdge.Direction values, relative to the projected seed node. Source and
// Target always carry the TRUE subject→object orientation regardless of
// direction — Direction only says which side of the edge the seed is on.
const (
	GraphDirectionOutgoing = "outgoing" // the seed is the edge's subject (source)
	GraphDirectionIncoming = "incoming" // the seed is the edge's object (target)
)

// GraphProjection is the optional structured graph facet carried on
// Response.Graph when the request Wants "graph". Nodes are the response's seed
// entities (same opaque handles as Node.Handle); Edges are the directed
// relationships around them, in true subject→object orientation. The
// projection is lossless but bounded: its caps are independent of the v1
// node/body budget, and every cut is reported through FactsTruncated /
// FactsDropped / Truncated — never silently.
//
// ViewRevision carries revision OBSERVATIONS, not a consistency claim: the
// engine assembles a projection from N independent reads with no snapshot and
// no read transaction, so no response field can prove the reads hit one
// indexed revision (ADR-083). A consumer that needs a genuinely coherent
// single-revision view must use pkg/graphview (ADR-081); this facet is
// best-effort ranked evidence.
type GraphProjection struct {
	Nodes        []GraphNode  `json:"nodes"`
	Edges        []GraphEdge  `json:"edges,omitempty"`
	ViewRevision ViewRevision `json:"view_revision"`
	// Truncated reports that ANY node- or edge-level cap below cut content
	// (facts, edges, or evidence), or that an edge walk faulted and left the
	// edge set incomplete — a partial projection never reads as exact.
	Truncated bool `json:"truncated"`
}

// GraphNode is one projected seed entity: the same opaque Handle as
// Node.Handle (never parse or construct it) plus the entity's typed property
// facts. FactsTruncated + FactsDropped report the per-node fact cap, so a
// consumer can always tell a small entity from a capped one.
type GraphNode struct {
	Handle string      `json:"handle"`
	Facts  []GraphFact `json:"facts,omitempty"`
	// FactsTruncated reports the fact list was capped; FactsDropped counts the
	// distinct facts cut. Both are omitted when nothing was dropped.
	FactsTruncated bool `json:"facts_truncated,omitempty"`
	FactsDropped   int  `json:"facts_dropped,omitempty"`
}

// GraphFact is one typed property fact: the ORIGINAL predicate verbatim, the
// value as typed JSON passthrough (no coercion, no stringification), the
// verbatim datatype (absent stays absent), and the contributing evidence.
// Multi-valued predicates produce multiple facts; duplicate assertions of the
// same (predicate, value) merge into one fact carrying one evidence entry per
// contributing triple.
type GraphFact struct {
	Predicate string          `json:"predicate"`
	Value     json.RawMessage `json:"value"`
	Datatype  string          `json:"datatype,omitempty"`
	Evidence  []GraphEvidence `json:"evidence,omitempty"`
	// Truncated reports the per-fact evidence cap was hit; surviving entries
	// are a prefix of the discovered contributions, and the projection-level
	// Truncated flag is set alongside.
	Truncated bool `json:"truncated,omitempty"`
}

// GraphEdge is one directed relationship. Source and Target are node handles
// in TRUE subject→object orientation regardless of which side the seed is on;
// Direction reports the seed's side; when BOTH endpoints are seeds the edge
// is discovered outgoing-first, so Direction reads "outgoing" relative to the
// subject seed (deterministic first-discovery). Edge identity is (source, predicate,
// target) — rendered as the stable ID "<source>|<predicate>|<target>" — so
// parallel predicates between the same pair are distinct edges, opposite
// directions are distinct edges with swapped Source/Target, and multiple
// assertions of the same edge merge into ONE edge with multiple inspectable
// Evidence entries. Truncated reports the per-edge evidence cap.
type GraphEdge struct {
	ID        string          `json:"id"`
	Source    string          `json:"source"`
	Target    string          `json:"target"`
	Predicate string          `json:"predicate"`
	Direction string          `json:"direction"`
	Evidence  []GraphEvidence `json:"evidence,omitempty"`
	Truncated bool            `json:"truncated,omitempty"`
}

// GraphEvidence is one stored assertion's provenance, projected verbatim:
// absent values are OMITTED from the wire, never defaulted or synthesized.
// Confidence is a pointer so an absent confidence is distinguishable from an
// asserted value — the stored float zero value is the unset default and is
// therefore omitted, never fabricated as a literal 0. Timestamp is RFC 3339
// and omitted when the stored time is zero.
type GraphEvidence struct {
	Source     string   `json:"source,omitempty"`
	Timestamp  string   `json:"timestamp,omitempty"`
	Confidence *float64 `json:"confidence,omitempty"`
	Context    string   `json:"context,omitempty"`
}

// ViewRevision is a pair of observations: the graph's indexed revision as
// sampled before seed resolution (Start) and re-sampled after the facet's
// last graph fetch (End). Since ADR-083 distributes status on a heartbeat,
// both samples are heartbeat-grained — equal bounds mean the status feed did
// not visibly advance during assembly, which can never prove the reads in
// between hit one revision. The former Coherent bool claimed exactly that and
// was removed (ADR-083, third break): two samples agreeing cannot establish
// the absence of motion between them, before or after the transport change.
// A failed re-sample reports End=0 — the engine degrades honestly rather
// than guessing a revision.
type ViewRevision struct {
	Start uint64 `json:"start"`
	End   uint64 `json:"end"`
}

// computeGraph builds the graph projection for the ranked seeds. Facts and
// outgoing edges come from the seeds' own triples (already fetched — zero
// extra I/O); incoming edges reuse the relations-style Neighbors walk, whose
// counterpart fetch also supplies the incoming evidence — no new I/O class.
// startRev is the IndexedRevision sampled before resolve; the view revision is
// re-sampled after the facet's last fetch.
func (e *Engine) computeGraph(ctx context.Context, seeds []*Entity, lens Lens, startRev uint64) *GraphProjection {
	preds, declared := allEdgePredicates(lens.Edges())
	proj := &GraphProjection{}
	acc := newGraphEdgeAcc()

	for _, s := range seeds {
		node, outgoing := graphNodeFor(s, declared)
		if node.FactsTruncated {
			proj.Truncated = true
		}
		for i := range node.Facts {
			if node.Facts[i].Truncated {
				proj.Truncated = true
				break
			}
		}
		proj.Nodes = append(proj.Nodes, node)
		for _, a := range outgoing {
			acc.add(s.ID, a.predicate, a.target, GraphDirectionOutgoing, graphEvidence(a.triple))
		}
	}

	// Incoming edges are only visible from the counterpart side, so they go
	// through the graph index walk — restricted to the lens-declared predicate
	// vocabulary (declaration-driven: an unfiltered walk would re-import the
	// index's shape-classified edges). Skipped once the projection edge cap
	// has already dropped content: further discoveries could only be dropped.
	for _, s := range seeds {
		if acc.full() || len(preds) == 0 {
			break
		}
		e.collectIncomingGraphEdges(ctx, s, preds, declared, acc, proj)
	}
	proj.Edges = acc.result()
	if acc.truncated {
		proj.Truncated = true
	}

	// Re-sample AFTER the facet's last graph fetch: the observed bounds of the
	// assembly window, heartbeat-grained — never a coherence proof (ADR-083).
	proj.ViewRevision = e.graphViewRevision(ctx, startRev)
	return proj
}

// graphViewRevision re-samples the index status after the facet's fetch phase.
// A failed re-sample reports End=0 — degrade honestly, never guess a revision.
func (e *Engine) graphViewRevision(ctx context.Context, start uint64) ViewRevision {
	status, err := e.graph.Status(ctx)
	if err != nil {
		return ViewRevision{Start: start}
	}
	return ViewRevision{Start: start, End: status.IndexedRevision}
}

// collectIncomingGraphEdges walks the graph index for edges pointing AT seed,
// restricted to the lens-declared predicates. Each edge's evidence comes from
// the counterpart entity the walk fetches anyway — the counterpart's own
// matching triple(s). Degrade-don't-fail: a Neighbors fault marks the
// projection truncated (its edge set is incomplete); a missing counterpart
// still projects the edge with no evidence (absent, never fabricated).
func (e *Engine) collectIncomingGraphEdges(ctx context.Context, seed *Entity, preds []string, declared map[string]bool, acc *graphEdgeAcc, proj *GraphProjection) {
	edges, ok := e.neighbors(ctx, seed.ID, preds, Incoming)
	if !ok {
		proj.Truncated = true // an incomplete edge set must not read as exact
		return
	}
	for _, ed := range edges {
		// Re-guard by predicate (mirrors computeImpact): declaration-driven
		// classification must hold regardless of whether the RetrievalClient
		// impl filtered.
		if !declared[ed.Predicate] {
			continue
		}
		counterpart, err := e.graph.Entity(ctx, ed.Target)
		if err != nil || counterpart == nil {
			acc.add(ed.Target, ed.Predicate, seed.ID, GraphDirectionIncoming, nil)
			continue
		}
		matched := false
		for i := range counterpart.Triples {
			t := &counterpart.Triples[i]
			if t.Predicate != ed.Predicate {
				continue
			}
			if obj, isString := t.Object.(string); !isString || obj != seed.ID {
				continue
			}
			acc.add(counterpart.ID, ed.Predicate, seed.ID, GraphDirectionIncoming, graphEvidence(t))
			matched = true
		}
		if !matched {
			// The index knows the edge but the counterpart's current triples
			// don't show it (e.g. a raced update) — project the edge anyway,
			// evidence absent.
			acc.add(ed.Target, ed.Predicate, seed.ID, GraphDirectionIncoming, nil)
		}
	}
}

// edgeAssertion is one seed triple classified as an outgoing edge: the
// verbatim predicate, the target handle, and the asserting triple (evidence).
type edgeAssertion struct {
	predicate, target string
	triple            *message.Triple
}

// graphNodeFor splits a seed's triples into the node's property facts and its
// outgoing edge assertions. Facts merge per (predicate, datatype, value) with
// one evidence entry per contributing triple; distinct facts past the cap are
// counted in FactsDropped, never dropped silently.
func graphNodeFor(ent *Entity, declared map[string]bool) (GraphNode, []edgeAssertion) {
	node := GraphNode{Handle: ent.ID}
	var outgoing []edgeAssertion
	factIdx := map[string]int{}  // fact key → index into node.Facts
	dropped := map[string]bool{} // distinct fact keys cut by the cap
	for i := range ent.Triples {
		t := &ent.Triples[i]
		if target, isEdge := edgeTarget(t, declared); isEdge {
			outgoing = append(outgoing, edgeAssertion{predicate: t.Predicate, target: target, triple: t})
			continue
		}
		value, err := json.Marshal(t.Object)
		if err != nil {
			continue // an unserializable object cannot be projected verbatim
		}
		key := t.Predicate + "\x00" + t.Datatype + "\x00" + string(value)
		if idx, seen := factIdx[key]; seen {
			appendFactEvidence(&node.Facts[idx], t)
			continue
		}
		if dropped[key] {
			continue
		}
		if len(node.Facts) >= maxGraphFactsPerNode {
			dropped[key] = true
			continue
		}
		fact := GraphFact{Predicate: t.Predicate, Value: json.RawMessage(value), Datatype: t.Datatype}
		appendFactEvidence(&fact, t)
		factIdx[key] = len(node.Facts)
		node.Facts = append(node.Facts, fact)
	}
	if len(dropped) > 0 {
		node.FactsTruncated = true
		node.FactsDropped = len(dropped)
	}
	return node, outgoing
}

// edgeTarget classifies one seed triple: it projects as a directed edge iff
// its predicate is lens-declared (any facet) or it carries the explicit
// message.EntityReferenceDatatype ("@id") — and its object is a string
// handle. Everything else is a property fact, including a string that merely
// has six-part entity-ID shape with an empty datatype. Deliberately NOT
// Triple.IsRelationship, whose empty-datatype branch classifies by value
// shape — the exact behavior this facet must never exhibit.
func edgeTarget(t *message.Triple, declared map[string]bool) (string, bool) {
	target, isString := t.Object.(string)
	if !isString || target == "" {
		return "", false
	}
	if declared[t.Predicate] || t.Datatype == message.EntityReferenceDatatype {
		return target, true
	}
	return "", false
}

// graphEvidence projects a triple's provenance verbatim: source, RFC 3339
// timestamp (omitted when zero), pointer confidence, and context. The stored
// Confidence is a plain float64 whose zero value means unset, so 0 projects as
// ABSENT — emitting a fabricated literal 0 would violate the never-fabricate
// contract. Returns nil when the triple carries no provenance at all.
func graphEvidence(t *message.Triple) *GraphEvidence {
	ev := GraphEvidence{Source: t.Source, Context: t.Context}
	if !t.Timestamp.IsZero() {
		ev.Timestamp = t.Timestamp.Format(time.RFC3339Nano)
	}
	if t.Confidence != 0 {
		c := t.Confidence
		ev.Confidence = &c
	}
	if ev.Source == "" && ev.Timestamp == "" && ev.Confidence == nil && ev.Context == "" {
		return nil
	}
	return &ev
}

// appendFactEvidence adds t's evidence to a fact, deduplicated by value; a
// triple carrying no provenance contributes nothing (absent stays absent).
func appendFactEvidence(f *GraphFact, t *message.Triple) {
	ev := graphEvidence(t)
	if ev == nil {
		return
	}
	key := evidenceKey(*ev)
	for i := range f.Evidence {
		if evidenceKey(f.Evidence[i]) == key {
			return
		}
	}
	if len(f.Evidence) >= maxGraphEvidencePerFact {
		f.Truncated = true
		return
	}
	f.Evidence = append(f.Evidence, *ev)
}

// evidenceKey renders an evidence entry as a comparable value key, so the same
// stored assertion discovered from both endpoints (outgoing via the seed's
// triple, incoming via the counterpart's) collapses to one entry while
// genuinely different contributions stay distinct.
func evidenceKey(ev GraphEvidence) string {
	conf := "-"
	if ev.Confidence != nil {
		conf = strconv.FormatFloat(*ev.Confidence, 'g', -1, 64)
	}
	return ev.Source + "\x00" + ev.Timestamp + "\x00" + conf + "\x00" + ev.Context
}

// allEdgePredicates flattens a lens's EdgeSpecs into every declared
// relationship predicate REGARDLESS of facet restriction — the graph facet
// projects the lens's whole declared edge vocabulary, not one walk's subset
// (unlike edgePredicates, which filters to a single facet's participants).
func allEdgePredicates(specs []EdgeSpec) (preds []string, set map[string]bool) {
	set = make(map[string]bool, len(specs))
	for _, s := range specs {
		if s.Predicate == "" || set[s.Predicate] {
			continue
		}
		set[s.Predicate] = true
		preds = append(preds, s.Predicate)
	}
	return preds, set
}

// graphEdgeAcc accumulates projection edges in first-seen order, merging
// same-(source, predicate, target) discoveries into one edge with multiple
// evidence entries and enforcing the projection edge cap.
type graphEdgeAcc struct {
	byID      map[string]*GraphEdge
	order     []string
	truncated bool // an edge or evidence entry was dropped
}

func newGraphEdgeAcc() *graphEdgeAcc {
	return &graphEdgeAcc{byID: map[string]*GraphEdge{}}
}

// add records one discovery of the directed edge source→target under
// predicate. A repeat of the same identity merges into the existing edge —
// evidence deduplicated by value and capped per edge; a NEW edge past the
// projection cap is dropped and flagged, never silently.
func (a *graphEdgeAcc) add(source, predicate, target, direction string, ev *GraphEvidence) {
	id := source + "|" + predicate + "|" + target
	edge, exists := a.byID[id]
	if !exists {
		if len(a.order) >= maxGraphEdges {
			a.truncated = true
			return
		}
		edge = &GraphEdge{ID: id, Source: source, Target: target, Predicate: predicate, Direction: direction}
		a.byID[id] = edge
		a.order = append(a.order, id)
	}
	if ev == nil {
		return
	}
	key := evidenceKey(*ev)
	for i := range edge.Evidence {
		if evidenceKey(edge.Evidence[i]) == key {
			return // the same assertion discovered from both endpoints
		}
	}
	if len(edge.Evidence) >= maxGraphEvidencePerEdge {
		edge.Truncated = true
		a.truncated = true
		return
	}
	edge.Evidence = append(edge.Evidence, *ev)
}

// full reports the projection edge cap has been hit AND an overflow was
// dropped — remaining walks are skipped (the projection is already truncated).
func (a *graphEdgeAcc) full() bool {
	return a.truncated && len(a.order) >= maxGraphEdges
}

// result renders the accumulated edges in first-seen order.
func (a *graphEdgeAcc) result() []GraphEdge {
	if len(a.order) == 0 {
		return nil
	}
	out := make([]GraphEdge, 0, len(a.order))
	for _, id := range a.order {
		out = append(out, *a.byID[id])
	}
	return out
}
