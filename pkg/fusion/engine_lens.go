package fusion

import (
	"context"
	"sort"
	"strings"
)

// The lens-driven engine entry (ADR-062). Resolves a query against the graph
// through a Lens and returns a fused Response — the deterministic counterpart to
// research_graph's chain (same resolve→expand→assemble shape, no LLM). Ported
// from semsource's validated source/fusion engine, generalized: a string
// ResolveMode, verbatim bodies hydrated via the handle-deref BodyResolver
// (ADR-062 increment 4) rather than a lens filesystem read, and (for now)
// resolve-order + lexical ranking with the ontology-coherence signal and the
// paths/impact facets deferred (gh#376 increment 5 + a facets follow-on).
//
// DISTINCT from the package-level Fuse(queries) — that runs a pre-built
// sub-query set over GraphQueryClient; this resolves a raw query through a Lens
// over RetrievalClient.

// Engine bounds keep the multi-hop traversal cheap and predictable.
const (
	resolveLimit        = 40
	maxRelationsPerNode = 12
)

// Ranking weights. Resolve order (the graph's own index/semantic ranking) is the
// base signal; lexical name match reorders locally. Ontology specificity and
// predicate salience (gh#376 increment 5) are SECONDARY reorderings folded in
// only when RankSignals is attached — deliberately small next to resolveScale
// (up to resolveScale×N) and lexExact so they break ties / nudge within a
// resolve-lexical tier rather than dominating the graph's own ranking.
const (
	lexExact      = 40.0
	lexPrefix     = 20.0
	lexContains   = 8.0
	resolveScale  = 4.0 // base score per resolve-rank position
	ontologyScale = 1.5 // per ontology-depth level (class specificity)
	salienceScale = 3.0 // per unit of an entity's net predicate salience (signed: boost − demotion, gh#441)
)

// ontologyClassPredicate is the stamped BFO/CCO class triple (semsource ADR-0005).
// Read for Node.Class when present; ID-derived class fallback is increment 5.
// Keep in sync with semsource's ontology.ClassPredicate — increment 5 (#396),
// which lifts the ontology helpers framework-side, is where these unify.
const ontologyClassPredicate = "entity.ontology.class"

// Engine runs the lens-driven deterministic fusion pipeline over a
// RetrievalClient, hydrating verbatim bodies through a BodyResolver.
type Engine struct {
	graph   RetrievalClient
	body    *BodyResolver
	signals RankSignals // optional; nil → resolve-order + lexical ranking only
}

// NewEngine builds a lens-driven engine. body may be nil — node bodies are then
// omitted (the response degrades gracefully rather than panicking).
func NewEngine(graph RetrievalClient, body *BodyResolver) *Engine {
	return &Engine{graph: graph, body: body}
}

// WithSignals attaches the framework ranking signals — ontology specificity +
// predicate salience (ADR-062 increment 5, gh#396) — folded into ranking on top
// of resolve-order + lexical. Returns the engine for chaining. Passing nil (the
// default) keeps ranking at resolve-order + lexical.
func (e *Engine) WithSignals(s RankSignals) *Engine {
	e.signals = s
	return e
}

// Fuse resolves req against the graph through lens and returns the fused
// response. Readiness is load-bearing: a not-ready graph yields an empty
// envelope (the caller must fall back); ready+absent yields a miss with
// near-matches — never an ambiguous empty. A backend failure fetching seeds is
// surfaced as an error, NOT silently turned into a "not found" (that would
// violate the ready≠not-found contract).
func (e *Engine) Fuse(ctx context.Context, req Request, lens Lens) (Response, error) {
	status, err := e.graph.Status(ctx)
	if err != nil {
		return Response{}, err
	}
	if !status.Ready {
		return Response{Index: status, Provenance: ProvenanceDeterministic, ContractVersion: ContractVersion}, nil
	}

	mode := lens.ResolveMode(req.Query)
	seeds, err := e.graph.Resolve(ctx, req.Query, mode, resolveLimit)
	if err != nil {
		return Response{}, err
	}
	entities, err := e.graph.Entities(ctx, seeds)
	if err != nil {
		return Response{}, err
	}
	if len(entities) == 0 {
		return e.miss(ctx, status, mode, req.Query), nil
	}

	names := make([]string, len(entities))
	for i, ent := range entities {
		names[i] = lens.Label(ent)
	}
	ranked := e.rankEntities(entities, names, req.Query)

	wants := wantSet(req.Want)
	resp := Response{Index: status, Provenance: ProvenanceForMode(mode), ContractVersion: ContractVersion}
	resp.Nodes, resp.Truncated = e.buildNodes(ctx, ranked, lens, wants, newBudget(req.Budget))
	if len(resp.Nodes) == 0 {
		resp.Misses = []Miss{{Query: req.Query}}
	}
	// Optional facets, computed from the ranked seeds (not the display-budgeted
	// nodes) so they describe the query's structure, not the page.
	if wants[WantImpact] {
		resp.Impact = e.computeImpact(ctx, ranked, lens)
	}
	if wants[WantPaths] {
		resp.Paths = e.computePaths(ctx, ranked, lens)
	}
	return resp, nil
}

// miss builds a ready+absent response with near-matches.
func (e *Engine) miss(ctx context.Context, status IndexStatus, mode ResolveMode, query string) Response {
	names, _ := e.graph.Names(ctx, query, 5)
	return Response{
		Index:           status,
		Provenance:      ProvenanceForMode(mode),
		Misses:          []Miss{{Query: query, DidYouMean: names}},
		ContractVersion: ContractVersion,
	}
}

// buildNodes materializes ranked entities into Nodes within the budget.
func (e *Engine) buildNodes(ctx context.Context, ranked []*Entity, lens Lens, wants map[Want]bool, bg *budgeter) ([]Node, bool) {
	var nodes []Node
	for _, ent := range ranked {
		node := e.nodeFor(ctx, ent, lens, wants)
		if !bg.admit(len(node.Body)) {
			return nodes, true
		}
		nodes = append(nodes, node)
	}
	return nodes, false
}

// nodeFor builds one Node, including only the requested facets. The body is
// hydrated by dereferencing the lens's Hydrate HANDLE through the BodyResolver
// (ADR-062 increment 4). Hydration is best-effort: a Hydrate or deref error omits
// the body and does not fail the node (degrade-don't-fail).
func (e *Engine) nodeFor(ctx context.Context, ent *Entity, lens Lens, wants map[Want]bool) Node {
	loc := lens.Location(ent)
	node := Node{
		Name:     lens.Label(ent),
		Kind:     lens.Kind(ent),
		Path:     loc.Path,
		Fragment: loc.Fragment,
		Lines:    lineRange(loc.Lines),
		Class:    classOf(ent),
		Handle:   ent.ID,
	}
	if wants[WantBody] && e.body != nil {
		if ref, err := lens.Hydrate(ctx, ent); err == nil && ref != nil {
			if body, derr := e.body.ResolveBody(ctx, ref); derr == nil {
				node.Body = string(body)
			}
		}
	}
	if wants[WantRelations] {
		node.Relations = e.relations(ctx, ent, lens)
	}
	return node
}

// relations expands a node's forward and reverse edges into role → refs.
func (e *Engine) relations(ctx context.Context, ent *Entity, lens Lens) map[string][]Ref {
	preds, fwd, rev := edgePredicates(lens.Edges(), FacetRelations)
	if len(preds) == 0 {
		return nil
	}
	rels := map[string][]Ref{}
	e.collectEdges(ctx, ent.ID, preds, Outgoing, fwd, lens, rels)
	e.collectEdges(ctx, ent.ID, preds, Incoming, rev, lens, rels)
	if len(rels) == 0 {
		return nil
	}
	return rels
}

// collectEdges appends refs for one direction's edges, capped per role.
func (e *Engine) collectEdges(ctx context.Context, id string, preds []string, dir Direction, roleByPred map[string]string, lens Lens, rels map[string][]Ref) {
	edges, err := e.graph.Neighbors(ctx, id, preds, dir)
	if err != nil {
		return
	}
	for _, ed := range edges {
		role := roleByPred[ed.Predicate]
		if role == "" || len(rels[role]) >= maxRelationsPerNode {
			continue
		}
		if ref, ok := e.refFor(ctx, ed.Target, lens); ok {
			rels[role] = append(rels[role], ref)
		}
	}
}

// refFor resolves a target entity ID to a human Ref.
func (e *Engine) refFor(ctx context.Context, id string, lens Lens) (Ref, bool) {
	te, err := e.graph.Entity(ctx, id)
	if err != nil || te == nil {
		return Ref{}, false
	}
	loc := lens.Location(te)
	return Ref{Name: lens.Label(te), Path: loc.Path, Fragment: loc.Fragment, Line: loc.Lines[0]}, true
}

// edgePredicates flattens a lens's EdgeSpecs into the predicate list and the
// forward/reverse role lookups used during expansion, restricted to the specs that
// participate in facet f. An EdgeSpec with no Facets participates in every facet
// (backward-compatible default); one with a non-empty Facets is included only for
// the facets it names (gh#475).
func edgePredicates(specs []EdgeSpec, f Facet) (preds []string, fwd, rev map[string]string) {
	fwd = make(map[string]string, len(specs))
	rev = make(map[string]string, len(specs))
	for _, s := range specs {
		if !s.walksFacet(f) {
			continue
		}
		preds = append(preds, s.Predicate)
		if s.OutgoingRole != "" {
			fwd[s.Predicate] = s.OutgoingRole
		}
		if s.IncomingRole != "" {
			rev[s.Predicate] = s.IncomingRole
		}
	}
	return preds, fwd, rev
}

// classOf returns an entity's stamped BFO/CCO class IRI (entity.ontology.class),
// or "". ID-derived class fallback + ontology-distance ranking are gh#376 increment 5.
func classOf(e *Entity) string {
	return e.First(ontologyClassPredicate)
}

// lineRange renders a [start,end] pair as a wire slice, or nil for line-less
// domains so the field is omitted.
func lineRange(l [2]int) []int {
	if l[0] == 0 && l[1] == 0 {
		return nil
	}
	return []int{l[0], l[1]}
}

// rankEntities orders entities (given in resolve order) by resolve-rank base +
// lexical name match, plus — when RankSignals is attached (increment 5) —
// ontology specificity and predicate salience as secondary reorderings.
// names[i] is entity[i]'s display name. With no signals the result is exactly
// resolve order + lexical (increments 1–4).
func (e *Engine) rankEntities(entities []*Entity, names []string, query string) []*Entity {
	q := strings.ToLower(strings.TrimSpace(query))
	type scored struct {
		e   *Entity
		s   float64
		pos int
	}
	arr := make([]scored, len(entities))
	for i, ent := range entities {
		s := resolveScale * float64(len(entities)-i)
		s += lexicalScore(q, strings.ToLower(names[i]))
		if e.signals != nil {
			s += ontologyScale * e.signals.ClassSpecificity(classOf(ent))
			s += salienceScale * entitySalience(ent, e.signals)
		}
		arr[i] = scored{ent, s, i}
	}
	sort.SliceStable(arr, func(a, b int) bool {
		if arr[a].s != arr[b].s {
			return arr[a].s > arr[b].s
		}
		return arr[a].pos < arr[b].pos
	})
	out := make([]*Entity, len(arr))
	for i := range arr {
		out[i] = arr[i].e
	}
	return out
}

// entitySalience is an entity's ranking salience: the entity's single most
// salient (max positive) weight PLUS its single strongest demotion (min
// negative) weight over its predicates — one presence-based boost and one
// presence-based penalty, summed (gh#441). Extremes, not a sum over all
// predicates, so a densely-annotated entity does not outrank a sharply-
// identified one purely on fact count; and the two extremes come from different
// predicates (a predicate's weight is one sign), so a boost and a demotion
// compose without either masking the other.
//
// Signed weights are what let a ranker DEMOTE, not just boost: structurally-
// identifiable noise (tests, generated code, mocks) that carries the SAME
// salient predicates as the real thing — a *_test.go with the same doc-comment
// salience as its impl — also carries a negatively-weighted presence predicate
// (e.g. code.artifact.test → WithWeight(-1.5)), so it sinks below the impl the
// additive-only ranker could never separate it from. With no negative weights
// (every config before gh#441) min-negative stays 0 and this is exactly the old
// max — backward-compatible.
func entitySalience(ent *Entity, sig RankSignals) float64 {
	var maxPos, minNeg float64 // strongest boost (≥0), strongest demotion (≤0)
	for i := range ent.Triples {
		w := sig.PredicateSalience(ent.Triples[i].Predicate)
		if w > maxPos {
			maxPos = w
		}
		if w < minNeg {
			minNeg = w
		}
	}
	return maxPos + minNeg
}

// lexicalScore scores a name against a lowercased query.
func lexicalScore(q, name string) float64 {
	switch {
	case name == q:
		return lexExact
	case strings.HasPrefix(name, q):
		return lexPrefix
	case strings.Contains(name, q):
		return lexContains
	default:
		return 0
	}
}
