package fusion

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/metric"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/storage"
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
	signals RankSignals             // optional; nil → resolve-order + lexical ranking only
	metrics *metric.MetricsRegistry // optional; nil → body-hydration counter falls back to the default registerer

	// bodyFailures is this engine's body-hydration-failure counter, resolved
	// against e.metrics (or the default registerer when nil) and memoized PER
	// ENGINE — no process-global state, so two engines with different registries
	// keep independent series. bodyFailuresOnce guards the lazy default-registerer
	// resolution for an engine built without WithMetrics.
	bodyFailures     *prometheus.CounterVec
	bodyFailuresOnce sync.Once
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

// WithMetrics wires the app MetricsRegistry so this engine's body-hydration-
// failure counter (fusion_body_hydration_failures_total, gh#616) registers into
// the /metrics-scraped registry. Returns the engine for chaining. Deliberately a
// builder rather than a NewEngine parameter so the constructor signature stays
// stable for the library's callers.
//
// The counter is resolved PER ENGINE against the given registry: two engines
// sharing one registry increment the SAME series (register-or-get-existing), two
// engines with different registries each get their own, and a nil registry (or an
// engine built without WithMetrics) counts against the default registerer. There
// is no process-global state pinning the counter to whichever registry was seen
// first.
func (e *Engine) WithMetrics(registry *metric.MetricsRegistry) *Engine {
	e.metrics = registry
	// Resolve eagerly so the vec lands in THIS registry now (construction time),
	// and mark the lazy path done so a later failure does not re-resolve.
	e.bodyFailuresOnce.Do(func() {
		e.bodyFailures = resolveBodyHydrationFailureVec(registry)
	})
	return e
}

// Fuse resolves req against the graph through lens and returns the fused response.
//
// The honesty rule is that an empty result is never ambiguous. An UNHEALTHY graph
// yields an empty envelope the caller must fall back on; a healthy graph that found
// nothing yields a miss with near-matches; seeds that resolved but could not be read
// yield Unhydrated and NO miss; and a backend failure is surfaced as an error rather
// than silently becoming a "not found".
//
// ADR-084 narrowed the first case from coverage to health: a healthy index that is
// merely behind now SERVES, reporting its view age on the envelope, because withholding
// on lag sent callers to fall back on a graph that could have answered.
func (e *Engine) Fuse(ctx context.Context, req Request, lens Lens) (Response, error) {
	status, err := e.graph.Status(ctx)
	if err != nil {
		if errors.Is(err, ErrReadinessUnknown) {
			// A feed we cannot vouch for fails CLOSED as an honest empty envelope
			// (ADR-084 D6). Fusion exposes no ungated escape because it is a shared
			// product surface whose empty answer other people act on.
			//
			// The client returns a ZERO IndexStatus alongside this error, so State is
			// stamped explicitly rather than shipped as "": an empty string is outside
			// the closed state set every consumer switches on, and would read as a new
			// unknown phase rather than as "we could not establish readiness".
			return deferredResponse(unknownReadinessStatus(status),
				string(graph.DeferStatusUnknown)), nil
		}
		// Broken wiring stays LOUD. Masking it as "the graph is busy" would make a
		// permanent operator bug indistinguishable from transient backpressure, forever.
		return Response{}, err
	}
	// Fusion is a read path: it asks the HEALTH question, which is now the only question
	// the gate asks (ADR-084 D1). Adopting the canonical gate is the point — this was
	// hand-rolled as `!status.Ready`, the one exact-coverage check fusion never
	// migrated, so a healthy index under write returned an empty envelope and the
	// caller fell back to grep on a graph that could have answered.
	if proceed, reason := graph.EvaluateReadinessGate(
		graph.StatusReading{Status: status.readinessEnvelope(), Fresh: true}); !proceed {
		return deferredResponse(status, string(reason)), nil
	}

	mode := lens.ResolveMode(req.Query)
	seeds, err := e.graph.Resolve(ctx, ResolveQuery{Query: req.Query, Mode: mode, Scope: req.Scope, Limit: resolveLimit})
	if err != nil {
		if isIndexNotReady(err) {
			return e.notReadyEnvelope(ctx, status), nil
		}
		return Response{}, err
	}
	hydration, err := e.graph.Entities(ctx, SeedIDs(seeds))
	if err != nil {
		if isIndexNotReady(err) {
			return e.notReadyEnvelope(ctx, status), nil
		}
		return Response{}, err
	}
	entities := hydration.Entities
	if len(entities) == 0 {
		// Nothing hydrated. If the seeds resolved but NONE of them loaded, this is a
		// hydration failure, not an absence: reporting it as a miss would claim the
		// graph looked and found nothing, which is exactly the conclusion it cannot
		// draw. Say what failed to hydrate and leave Misses empty.
		if len(hydration.Unhydrated) > 0 {
			// Marked DEFERRED: nothing was read, so this is a withholding, not a
			// zero-result answer. The contract tells consumers to check Deferred to
			// answer "did I get an answer?", and leaving it false here would make the
			// documented rule wrong for the one case that most looks like an answer —
			// an empty node list on a healthy index.
			resp := deferredResponse(status, string(DeferReasonAllSeedsUnhydrated))
			resp.Provenance = ProvenanceForMode(mode)
			resp.Unhydrated = hydration.Unhydrated
			return resp, nil
		}
		return e.miss(ctx, status, mode, req.Query), nil
	}

	names := make([]string, len(entities))
	for i, ent := range entities {
		names[i] = lens.Label(ent)
	}
	ranked := e.rankEntities(entities, names, req.Query)

	wants := wantSet(req.Want)
	resp := Response{Index: status, Provenance: ProvenanceForMode(mode), ContractVersion: ContractVersion}
	resp.Unhydrated = hydration.Unhydrated
	nodes, truncated, err := e.buildNodes(ctx, ranked, lens, wants, newBudget(req.Budget))
	if err != nil {
		if isIndexNotReady(err) {
			return e.notReadyEnvelope(ctx, status), nil
		}
		return Response{}, err
	}
	resp.Nodes, resp.Truncated = nodes, truncated
	if req.IncludeScores {
		applyScores(resp.Nodes, ranked, seeds)
	}
	// Deliberately no `len(resp.Nodes) == 0 -> synthesize a Miss` here. It was
	// unreachable (entities non-empty implies ranked non-empty, and the budgeter always
	// admits the first node), and if it HAD fired it would have manufactured a bare
	// Miss with no near-matches — an absence claim from a budget accident, which is
	// exactly what ADR-084 forbids.
	// Optional facets, computed from the ranked seeds (not the display-budgeted
	// nodes) so they describe the query's structure, not the page.
	if wants[WantImpact] {
		resp.Impact = e.computeImpact(ctx, ranked, lens)
	}
	if wants[WantPaths] {
		resp.Paths = e.computePaths(ctx, ranked, lens)
	}
	// The graph projection is computed LAST so its post-fetch status re-sample
	// (ViewRevision) trails every fetch the facet performed.
	if wants[WantGraph] {
		resp.Graph = e.computeGraph(ctx, ranked, lens, status.IndexedRevision)
	}
	return resp, nil
}

// unknownReadinessStatus is the envelope shipped when readiness could not be
// established. It keeps whatever the client managed to report and forces the two fields
// a consumer branches on into an honest, in-set state: not ready, and building rather
// than empty. Building is the closest existing label for "cannot vouch for this index"
// — the alternative is inventing a state value, which would break the one-hot metric
// and every exhaustive switch downstream.
func unknownReadinessStatus(status IndexStatus) IndexStatus {
	status.Ready = false
	if status.State == "" {
		status.State = StateBuilding
	}
	return status
}

// applyScores attaches each node's RESOLVE rank and resolve similarity (ADR-084 D5),
// opt-in via the request so the default wire shape is unchanged.
//
// Rank is the seed's 1-based position in RESOLVE order — deliberately not the node's
// position in the response, which the caller can already count off the array and which
// therefore carries no information. The whole point of the field is that rankEntities
// REORDERS (lexical affinity, ontology specificity, predicate salience), so the gap
// between where resolve put an entity and where it came out is the diagnostic signal:
// "why did this rank third when the index thought it was the best match?" is
// answerable only from the pair.
//
// Both values are joined BY ENTITY ID. Ranking reorders and the budget truncates, so a
// positional join would silently mislabel — the same class of bug as the cache-order
// scramble this change already fixed. nodes[i] corresponds to ranked[i] by construction
// (buildNodes appends in order and stops at the budget), so ranked supplies the join
// key; the key is what is trusted, not the index.
func applyScores(nodes []Node, ranked []*Entity, seeds []Seed) {
	type score struct {
		rank          int
		similarity    float64
		hasSimilarity bool
	}
	byID := make(map[string]score, len(seeds))
	for i, s := range seeds {
		byID[s.ID] = score{rank: i + 1, similarity: s.Similarity, hasSimilarity: s.HasSimilarity}
	}
	for i := range nodes {
		if i >= len(ranked) {
			// Defensive: buildNodes cannot produce more nodes than ranked entities.
			// Bailing beats indexing past the slice that supplies the join key.
			continue
		}
		sc, ok := byID[ranked[i].ID]
		if !ok {
			// A node whose entity was never a seed (not reachable today — nodes are
			// built from ranked seeds). Leaving rank unset beats inventing one.
			continue
		}
		nodes[i].Rank = sc.rank
		if sc.hasSimilarity {
			similarity := sc.similarity
			nodes[i].Similarity = &similarity
		}
	}
}

// isIndexNotReady reports whether err is the classified readiness transient a
// graph read emits while the index is catching up to ENTITY_STATES. It matches on
// the stable code (graph.ErrorCodeIndexNotReady), NOT errs.IsTransient — a real
// connection timeout is also transient but must propagate as a hard error, not be
// degraded to an empty envelope.
func isIndexNotReady(err error) bool {
	var ce *errs.ClassifiedError
	return errors.As(err, &ce) && ce.Code == graph.ErrorCodeIndexNotReady
}

// notReadyEnvelope is the empty-honest degrade the top-level !Ready gate returns,
// re-sampled so Lag / IndexedRevision reflect the current moment. degrade-consistency
// (this change): every internal read that hits the readiness transient
// (graph.ErrorCodeIndexNotReady) returns THIS — never a Ready=true envelope, never
// a propagated hard error — so a first-catch-up race under load falls back exactly
// like the top gate does. A re-sample failure keeps the passed status but forces
// Ready=false, since the transient is itself proof the index is not ready.
func (e *Engine) notReadyEnvelope(ctx context.Context, status IndexStatus) Response {
	if cur, err := e.graph.Status(ctx); err == nil {
		status = cur
	}
	status.Ready = false
	// A Ready=false envelope must not carry State="ready": the transient is proof the
	// index is not ready, so on a re-sample failure (stale top-gate status) or a raced
	// re-sample back to ready, downgrade the friendly label to building (catching up)
	// so it does not contradict the load-bearing Ready field.
	if status.State == StateReady {
		status.State = StateBuilding
	}
	// Marked deferred EXPLICITLY rather than left for the caller to infer from the
	// envelope. The transient may have come from graph-ingest or graph-embedding while
	// graph-index is healthy, in which case the re-sample above returns a HEALTHY
	// envelope — one that passes the canonical gate — attached to an empty response.
	// Forcing Ready=false is not enough either: ADR-084 consumers gate on health, and
	// health does not read Ready.
	return deferredResponse(status, string(graph.ErrorCodeIndexNotReady))
}

// deferredResponse builds the empty-honest envelope for every path that WITHHELD.
// One constructor so a new defer path cannot forget the flag — the failure mode being
// an empty response that a consumer reads as an answer.
func deferredResponse(status IndexStatus, reason string) Response {
	return Response{
		Index:           status,
		Provenance:      ProvenanceDeterministic,
		Deferred:        true,
		DeferReason:     reason,
		ContractVersion: ContractVersion,
	}
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

// buildNodes materializes ranked entities into Nodes within the budget. It
// surfaces the readiness transient (via nodeFor → relations → collectEdges) so
// Fuse degrades the whole response rather than serving partial relations as ready.
func (e *Engine) buildNodes(ctx context.Context, ranked []*Entity, lens Lens, wants map[Want]bool, bg *budgeter) ([]Node, bool, error) {
	var nodes []Node
	for _, ent := range ranked {
		node, err := e.nodeFor(ctx, ent, lens, wants)
		if err != nil {
			return nil, false, err
		}
		if !bg.admit(len(node.Body)) {
			return nodes, true, nil
		}
		nodes = append(nodes, node)
	}
	return nodes, false, nil
}

// nodeFor builds one Node, including only the requested facets. The body is
// hydrated by dereferencing the lens's Hydrate HANDLE through the BodyResolver
// (ADR-062 increment 4). Hydration is best-effort and degrades-don't-fail: a
// Hydrate or deref failure omits the body and does NOT fail the node — but it is
// no longer SILENT (gh#616, #600). The node ships with a bounded BodyReason and a
// failure counter fires, so an empty body is a reported partial result rather than
// an unexplained absence.
func (e *Engine) nodeFor(ctx context.Context, ent *Entity, lens Lens, wants map[Want]bool) (Node, error) {
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
	if wants[WantBody] {
		// Gate on the WANT alone, then always ask the lens for a handle. A nil
		// BodyResolver (NewEngine(graph, nil)) is documented-valid, but it must not
		// SHORT-CIRCUIT the WantBody path — a node whose lens yields a real handle it
		// cannot resolve is a reportable config gap, exactly the silent failure #632
		// set out to kill (gh#616, #600). The ref==nil case below still keeps genuinely
		// body-less tiers silent, so no resolver never spams BodyError for them.
		ref, err := lens.Hydrate(ctx, ent)
		switch {
		case err != nil:
			// A genuine retrieval fault producing the handle.
			e.reportBodyFailure(&node, BodyError)
		case ref == nil:
			// The entity legitimately has NO verbatim body (lens.go:171,177). This
			// is a normal condition, not a failure: leave Body empty, stamp no
			// reason, and increment no counter. Silent, matching the pre-report
			// body-less semantics.
		case e.body == nil:
			// The lens produced a REAL storage handle but no BodyResolver is wired to
			// dereference it. That is a configuration gap, NOT a body-less tier: the
			// node has a body it simply cannot load, so report it rather than ship an
			// unexplained empty body (gh#616, #600).
			// TODO(when lens Engine is wired): distinct not_configured reason to
			// separate "no resolver wired" from "read faulted" (deferred — the Engine
			// has no production caller yet, so expanding the closed BodyReason
			// vocabulary for an unwired path is premature).
			e.reportBodyFailure(&node, BodyError)
		default:
			body, derr := e.body.ResolveBody(ctx, ref)
			switch {
			case derr == nil:
				node.Body = string(body)
			case errors.Is(derr, storage.ErrObjectNotFound):
				// The ref is present on the entity but resolves to no stored object
				// — the #600 case: content-addressed evidence was reclaimed while the
				// entity still points at it.
				e.reportBodyFailure(&node, BodyNotFound)
			default:
				// The reference resolved to a store but the read faulted.
				e.reportBodyFailure(&node, BodyError)
			}
		}
	}
	if wants[WantRelations] {
		rels, err := e.relations(ctx, ent, lens)
		if err != nil {
			return Node{}, err
		}
		node.Relations = rels
	}
	return node, nil
}

// reportBodyFailure stamps a body-hydration failure onto the node (bounded
// reason, empty body left as-is) and increments this engine's failure counter by
// reason. It never defers the response or synthesizes a Miss — the node is a
// partial result that still ships (gh#616, #600).
func (e *Engine) reportBodyFailure(node *Node, reason BodyReason) {
	node.BodyReason = reason
	e.bodyFailureCounter().WithLabelValues(string(reason)).Inc()
}

// bodyFailureCounter returns this engine's body-hydration-failure counter,
// resolving it lazily against the default registerer for an engine built without
// WithMetrics (memoized per engine via bodyFailuresOnce). WithMetrics resolves it
// eagerly against the wired registry, so this only fires the default-registerer
// path.
func (e *Engine) bodyFailureCounter() *prometheus.CounterVec {
	e.bodyFailuresOnce.Do(func() {
		e.bodyFailures = resolveBodyHydrationFailureVec(e.metrics)
	})
	return e.bodyFailures
}

// relations expands a node's forward and reverse edges into role → refs. It
// returns the readiness transient (if a neighbor read hits it) so Fuse degrades
// consistently; any other neighbor fault is swallowed (best-effort expansion).
func (e *Engine) relations(ctx context.Context, ent *Entity, lens Lens) (map[string][]Ref, error) {
	preds, fwd, rev := edgePredicates(lens.Edges(), FacetRelations)
	if len(preds) == 0 {
		return nil, nil
	}
	rels := map[string][]Ref{}
	if err := e.collectEdges(ctx, ent.ID, preds, Outgoing, fwd, lens, rels); err != nil {
		return nil, err
	}
	if err := e.collectEdges(ctx, ent.ID, preds, Incoming, rev, lens, rels); err != nil {
		return nil, err
	}
	if len(rels) == 0 {
		return nil, nil
	}
	return rels, nil
}

// collectEdges appends refs for one direction's edges, capped per role. It returns
// the readiness transient (graph.ErrorCodeIndexNotReady) so Fuse degrades the whole
// response instead of serving partial-as-ready; any OTHER Neighbors fault is
// swallowed (best-effort expansion), preserving the prior behavior.
func (e *Engine) collectEdges(ctx context.Context, id string, preds []string, dir Direction, roleByPred map[string]string, lens Lens, rels map[string][]Ref) error {
	edges, err := e.graph.Neighbors(ctx, id, preds, dir)
	if err != nil {
		if isIndexNotReady(err) {
			return err
		}
		return nil
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
	return nil
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
