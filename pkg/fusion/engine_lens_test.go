package fusion_test

import (
	"context"
	"errors"
	"testing"

	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/pkg/fusion"
)

// fakeGraph is an in-memory RetrievalClient for lens-driven engine tests — no
// NATS, deterministic inputs. Error fields inject backend failures.
type fakeGraph struct {
	status       fusion.IndexStatus
	seeds        map[string][]string       // query → seed IDs
	entities     map[string]*fusion.Entity // id → entity
	out          map[string][]fusion.Edge  // id → outgoing edges
	in           map[string][]fusion.Edge  // id → incoming edges
	names        []string                  // did_you_mean suggestions
	statusErr    error
	resolveErr   error
	entErr       error
	neighborsErr error
}

func (g *fakeGraph) Status(context.Context) (fusion.IndexStatus, error) {
	return g.status, g.statusErr
}
func (g *fakeGraph) Resolve(_ context.Context, query string, _ fusion.ResolveMode, _ int) ([]string, error) {
	return g.seeds[query], g.resolveErr
}
func (g *fakeGraph) Entity(_ context.Context, id string) (*fusion.Entity, error) {
	return g.entities[id], nil
}
func (g *fakeGraph) Entities(_ context.Context, ids []string) ([]*fusion.Entity, error) {
	if g.entErr != nil {
		return nil, g.entErr
	}
	var out []*fusion.Entity
	for _, id := range ids {
		if e, ok := g.entities[id]; ok {
			out = append(out, e)
		}
	}
	return out, nil
}
func (g *fakeGraph) Neighbors(_ context.Context, id string, _ []string, dir fusion.Direction) ([]fusion.Edge, error) {
	if g.neighborsErr != nil {
		return nil, g.neighborsErr
	}
	if dir == fusion.Outgoing {
		return g.out[id], nil
	}
	return g.in[id], nil
}
func (g *fakeGraph) Names(_ context.Context, _ string, _ int) ([]string, error) {
	return g.names, nil
}

func readyStatus() fusion.IndexStatus {
	return fusion.IndexStatus{Ready: true, State: fusion.StateReady}
}

func entity(id, title, path string, extra ...message.Triple) *fusion.Entity {
	tr := []message.Triple{
		{Predicate: refTitlePredicate, Object: title},
		{Predicate: refPathPredicate, Object: path},
	}
	return &fusion.Entity{ID: id, Triples: append(tr, extra...)}
}

// TestEngine_NotReady_EmptyEnvelope: a not-ready graph yields an empty envelope
// the caller must fall back on — NEVER an authoritative empty result.
func TestEngine_NotReady_EmptyEnvelope(t *testing.T) {
	g := &fakeGraph{status: fusion.IndexStatus{Ready: false, State: fusion.StateBuilding}}
	eng := fusion.NewEngine(g, fusion.NewBodyResolver(fusion.MapStoreResolver{}))

	resp, err := eng.Fuse(context.Background(), fusion.Request{Query: "OnEvent"}, refLens{})
	if err != nil {
		t.Fatalf("Fuse: %v", err)
	}
	if resp.Index.Ready {
		t.Error("expected not-ready envelope")
	}
	if len(resp.Nodes) != 0 || len(resp.Misses) != 0 {
		t.Errorf("not-ready must yield no nodes and no misses, got %d nodes / %d misses", len(resp.Nodes), len(resp.Misses))
	}
}

// TestEngine_ResolveBuildsNodes: the happy path — resolve → entity → Node with
// lens fields + a hydrated body (Hydrate handle → BodyResolver → bytes).
func TestEngine_ResolveBuildsNodes(t *testing.T) {
	store := &fakeStore{data: map[string][]byte{"k/on_event": []byte("package handlers // body")}}
	ent := entity("acme.ops.code.repo.symbol.OnEvent", "OnEvent", "handlers/on_event.go",
		message.Triple{Predicate: refKindPredicate, Object: "function"},
		message.Triple{Predicate: "entity.ontology.class", Object: "http://x/Algorithm"},
		message.Triple{Predicate: refStorageInstancePr, Object: "objectstore"},
		message.Triple{Predicate: refStorageKeyPr, Object: "k/on_event"},
	)
	g := &fakeGraph{
		status:   readyStatus(),
		seeds:    map[string][]string{"OnEvent": {ent.ID}},
		entities: map[string]*fusion.Entity{ent.ID: ent},
	}
	eng := fusion.NewEngine(g, fusion.NewBodyResolver(fusion.MapStoreResolver{"objectstore": store}))

	resp, err := eng.Fuse(context.Background(), fusion.Request{Query: "OnEvent", Want: []fusion.Want{fusion.WantBody}}, refLens{})
	if err != nil {
		t.Fatalf("Fuse: %v", err)
	}
	if resp.Provenance != fusion.ProvenanceDeterministic {
		t.Errorf("provenance = %q, want deterministic (symbol resolve)", resp.Provenance)
	}
	if len(resp.Nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(resp.Nodes))
	}
	n := resp.Nodes[0]
	if n.Name != "OnEvent" || n.Kind != "function" || n.Path != "handlers/on_event.go" {
		t.Errorf("node fields wrong: %+v", n)
	}
	if n.Handle != ent.ID {
		t.Errorf("Handle = %q, want the entity ID %q", n.Handle, ent.ID)
	}
	if n.Class != "http://x/Algorithm" {
		t.Errorf("Class = %q, want the stamped ontology class", n.Class)
	}
	if n.Body != "package handlers // body" {
		t.Errorf("Body = %q, want the hydrated bytes", n.Body)
	}
}

// TestEngine_Relations: WantRelations expands a node's outgoing edges into
// role→refs via the lens's EdgeSpecs.
func TestEngine_Relations(t *testing.T) {
	target := entity("acme.ops.code.repo.symbol.Helper", "Helper", "handlers/helper.go")
	seed := entity("acme.ops.code.repo.symbol.OnEvent", "OnEvent", "handlers/on_event.go")
	g := &fakeGraph{
		status:   readyStatus(),
		seeds:    map[string][]string{"OnEvent": {seed.ID}},
		entities: map[string]*fusion.Entity{seed.ID: seed, target.ID: target},
		out: map[string][]fusion.Edge{
			seed.ID: {{Predicate: "ref.relationship.references", Target: target.ID}},
		},
	}
	eng := fusion.NewEngine(g, fusion.NewBodyResolver(fusion.MapStoreResolver{}))

	resp, err := eng.Fuse(context.Background(), fusion.Request{Query: "OnEvent", Want: []fusion.Want{fusion.WantRelations}}, refLens{})
	if err != nil {
		t.Fatalf("Fuse: %v", err)
	}
	if len(resp.Nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(resp.Nodes))
	}
	refs := resp.Nodes[0].Relations["referent"] // refLens's OutgoingRole
	if len(refs) != 1 || refs[0].Name != "Helper" {
		t.Errorf("expected one 'referent' ref to Helper, got %+v", resp.Nodes[0].Relations)
	}
}

// TestEngine_Miss: ready + resolved-to-nothing yields a Miss with did_you_mean —
// never an ambiguous empty.
func TestEngine_Miss(t *testing.T) {
	g := &fakeGraph{
		status:   readyStatus(),
		seeds:    map[string][]string{"Ghost": {"acme.ops.code.repo.symbol.Ghost"}}, // resolves an id…
		entities: map[string]*fusion.Entity{},                                       // …but it doesn't exist
		names:    []string{"OnEvent", "OnError"},
	}
	eng := fusion.NewEngine(g, fusion.NewBodyResolver(fusion.MapStoreResolver{}))

	resp, err := eng.Fuse(context.Background(), fusion.Request{Query: "Ghost"}, refLens{})
	if err != nil {
		t.Fatalf("Fuse: %v", err)
	}
	if len(resp.Nodes) != 0 {
		t.Errorf("expected no nodes on a miss, got %d", len(resp.Nodes))
	}
	if len(resp.Misses) != 1 || resp.Misses[0].Query != "Ghost" {
		t.Fatalf("expected one Miss for Ghost, got %+v", resp.Misses)
	}
	if len(resp.Misses[0].DidYouMean) != 2 {
		t.Errorf("expected 2 did_you_mean suggestions, got %v", resp.Misses[0].DidYouMean)
	}
}

// TestEngine_BudgetTruncation: a MaxNodes cap admits the budgeted prefix and
// flags truncation.
func TestEngine_BudgetTruncation(t *testing.T) {
	a := entity("acme.ops.code.repo.symbol.A", "A", "a.go")
	b := entity("acme.ops.code.repo.symbol.B", "B", "b.go")
	g := &fakeGraph{
		status:   readyStatus(),
		seeds:    map[string][]string{"q": {a.ID, b.ID}},
		entities: map[string]*fusion.Entity{a.ID: a, b.ID: b},
	}
	eng := fusion.NewEngine(g, fusion.NewBodyResolver(fusion.MapStoreResolver{}))

	resp, err := eng.Fuse(context.Background(), fusion.Request{Query: "q", Budget: fusion.Budget{MaxNodes: 1}}, refLens{})
	if err != nil {
		t.Fatalf("Fuse: %v", err)
	}
	if len(resp.Nodes) != 1 || !resp.Truncated {
		t.Errorf("expected 1 node + Truncated=true, got %d nodes / Truncated=%v", len(resp.Nodes), resp.Truncated)
	}
}

// TestEngine_BackendErrorSurfaced: a backend failure fetching seeds OR entities
// is an ERROR, not silently a not-found (that would violate ready≠not-found).
func TestEngine_BackendErrorSurfaced(t *testing.T) {
	cases := map[string]*fakeGraph{
		"resolve error":  {status: readyStatus(), resolveErr: errors.New("graph down")},
		"entities error": {status: readyStatus(), seeds: map[string][]string{"q": {"id"}}, entErr: errors.New("batch down")},
	}
	for name, g := range cases {
		t.Run(name, func(t *testing.T) {
			eng := fusion.NewEngine(g, fusion.NewBodyResolver(fusion.MapStoreResolver{}))
			if _, err := eng.Fuse(context.Background(), fusion.Request{Query: "q"}, refLens{}); err == nil {
				t.Error("expected a backend error to surface, not a silent miss")
			}
		})
	}
}

// TestEngine_HydrateError_DegradesBody: a hydrate/deref failure omits the body
// and does NOT fail the node or the fuse (degrade-don't-fail). Here the entity
// points at a store key that doesn't exist, so ResolveBody errors.
func TestEngine_HydrateError_DegradesBody(t *testing.T) {
	store := &fakeStore{getErr: errors.New("backend down")}
	ent := entity("acme.ops.code.repo.symbol.OnEvent", "OnEvent", "h/on_event.go",
		message.Triple{Predicate: refStorageInstancePr, Object: "objectstore"},
		message.Triple{Predicate: refStorageKeyPr, Object: "missing"},
	)
	g := &fakeGraph{status: readyStatus(), seeds: map[string][]string{"OnEvent": {ent.ID}}, entities: map[string]*fusion.Entity{ent.ID: ent}}
	eng := fusion.NewEngine(g, fusion.NewBodyResolver(fusion.MapStoreResolver{"objectstore": store}))

	resp, err := eng.Fuse(context.Background(), fusion.Request{Query: "OnEvent", Want: []fusion.Want{fusion.WantBody}}, refLens{})
	if err != nil {
		t.Fatalf("a hydrate fault must NOT fail the fuse, got %v", err)
	}
	if len(resp.Nodes) != 1 {
		t.Fatalf("expected the node to still ship, got %d nodes", len(resp.Nodes))
	}
	if resp.Nodes[0].Body != "" {
		t.Errorf("expected an empty body on hydrate fault, got %q", resp.Nodes[0].Body)
	}
}

// TestEngine_IncomingRelations: reverse edges populate the lens's IncomingRole —
// the reverse-direction path the port added over the seed walk.
func TestEngine_IncomingRelations(t *testing.T) {
	caller := entity("acme.ops.code.repo.symbol.Caller", "Caller", "caller.go")
	seed := entity("acme.ops.code.repo.symbol.OnEvent", "OnEvent", "on_event.go")
	g := &fakeGraph{
		status:   readyStatus(),
		seeds:    map[string][]string{"OnEvent": {seed.ID}},
		entities: map[string]*fusion.Entity{seed.ID: seed, caller.ID: caller},
		in: map[string][]fusion.Edge{
			seed.ID: {{Predicate: "ref.relationship.references", Target: caller.ID}},
		},
	}
	eng := fusion.NewEngine(g, fusion.NewBodyResolver(fusion.MapStoreResolver{}))

	resp, err := eng.Fuse(context.Background(), fusion.Request{Query: "OnEvent", Want: []fusion.Want{fusion.WantRelations}}, refLens{})
	if err != nil {
		t.Fatalf("Fuse: %v", err)
	}
	refs := resp.Nodes[0].Relations["referrer"] // refLens's IncomingRole
	if len(refs) != 1 || refs[0].Name != "Caller" {
		t.Errorf("expected one 'referrer' ref to Caller, got %+v", resp.Nodes[0].Relations)
	}
}

// TestEngine_NilBodyResolver: a nil BodyResolver omits bodies without panicking
// (the e.body != nil guard) — a deployment with no verbatim-body store still works.
func TestEngine_NilBodyResolver(t *testing.T) {
	ent := entity("acme.ops.code.repo.symbol.OnEvent", "OnEvent", "h/on_event.go",
		message.Triple{Predicate: refStorageInstancePr, Object: "objectstore"},
		message.Triple{Predicate: refStorageKeyPr, Object: "k"},
	)
	g := &fakeGraph{status: readyStatus(), seeds: map[string][]string{"OnEvent": {ent.ID}}, entities: map[string]*fusion.Entity{ent.ID: ent}}
	eng := fusion.NewEngine(g, nil) // no body resolver

	resp, err := eng.Fuse(context.Background(), fusion.Request{Query: "OnEvent", Want: []fusion.Want{fusion.WantBody}}, refLens{})
	if err != nil {
		t.Fatalf("Fuse with nil BodyResolver must not error, got %v", err)
	}
	if len(resp.Nodes) != 1 || resp.Nodes[0].Body != "" {
		t.Errorf("expected one node with empty body, got %+v", resp.Nodes)
	}
}

// fakeSignals is an in-memory RankSignals for ranking tests.
type fakeSignals struct {
	depth    map[string]float64 // class IRI → specificity
	salience map[string]float64 // predicate → weight
}

func (f fakeSignals) ClassSpecificity(iri string) float64 { return f.depth[iri] }
func (f fakeSignals) PredicateSalience(p string) float64  { return f.salience[p] }

// rankingPair sets up two entities A,B resolved in that order under a query with
// NO lexical match, so ranking is pure resolve-order (A ahead of B by
// resolveScale) unless a signal overcomes the gap.
func rankingPair(aClass, bClass string, bExtra ...message.Triple) *fakeGraph {
	a := entity("acme.ops.code.repo.symbol.Apple", "Apple", "a.go",
		message.Triple{Predicate: "entity.ontology.class", Object: aClass})
	bTriples := append([]message.Triple{{Predicate: "entity.ontology.class", Object: bClass}}, bExtra...)
	b := entity("acme.ops.code.repo.symbol.Banana", "Banana", "b.go", bTriples...)
	return &fakeGraph{
		status:   readyStatus(),
		seeds:    map[string][]string{"zzz": {a.ID, b.ID}},
		entities: map[string]*fusion.Entity{a.ID: a, b.ID: b},
	}
}

func topName(t *testing.T, eng *fusion.Engine) string {
	t.Helper()
	resp, err := eng.Fuse(context.Background(), fusion.Request{Query: "zzz"}, refLens{})
	if err != nil {
		t.Fatalf("Fuse: %v", err)
	}
	if len(resp.Nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(resp.Nodes))
	}
	return resp.Nodes[0].Name
}

// TestEngine_NilSignals_ResolveOrderUnchanged: with no RankSignals, ranking is
// exactly resolve-order — the increments 1–4 behavior (A resolved first wins).
func TestEngine_NilSignals_ResolveOrderUnchanged(t *testing.T) {
	g := rankingPair("http://x/Vague", "http://x/Specific")
	eng := fusion.NewEngine(g, nil) // no signals
	if got := topName(t, eng); got != "Apple" {
		t.Errorf("top = %q, want Apple (resolve order, no signals)", got)
	}
}

// TestEngine_Signals_OntologySpecificityReorders: B's deeper ontology class
// overcomes the one-position resolve gap, so B reorders ahead of A.
func TestEngine_Signals_OntologySpecificityReorders(t *testing.T) {
	g := rankingPair("http://x/Vague", "http://x/Specific")
	// resolve gap = resolveScale (4.0). ontologyScale*depth for B = 1.5*4 = 6 > 4.
	sig := fakeSignals{depth: map[string]float64{"http://x/Vague": 0, "http://x/Specific": 4}}
	eng := fusion.NewEngine(g, nil).WithSignals(sig)
	if got := topName(t, eng); got != "Banana" {
		t.Errorf("top = %q, want Banana (deeper ontology class reorders ahead)", got)
	}
}

// TestEngine_Signals_PredicateSalienceReorders: B carries a high-salience
// predicate that overcomes the resolve gap.
func TestEngine_Signals_PredicateSalienceReorders(t *testing.T) {
	g := rankingPair("http://x/Same", "http://x/Same",
		message.Triple{Predicate: "acme.identity.serial", Object: "SN-1"})
	// resolve gap = 4.0. salienceScale*weight for B = 3.0*2.0 = 6 > 4.
	sig := fakeSignals{salience: map[string]float64{"acme.identity.serial": 2.0}}
	eng := fusion.NewEngine(g, nil).WithSignals(sig)
	if got := topName(t, eng); got != "Banana" {
		t.Errorf("top = %q, want Banana (salient predicate reorders ahead)", got)
	}
}

// TestEngine_Signals_TooSmallToReorder: a signal smaller than the resolve gap
// does NOT flip order — signals are secondary reorderings, not overrides.
func TestEngine_Signals_TooSmallToReorder(t *testing.T) {
	g := rankingPair("http://x/Vague", "http://x/Specific")
	// ontologyScale*1 = 1.5 < resolve gap 4.0 → A (resolved first) stays on top.
	sig := fakeSignals{depth: map[string]float64{"http://x/Vague": 0, "http://x/Specific": 1}}
	eng := fusion.NewEngine(g, nil).WithSignals(sig)
	if got := topName(t, eng); got != "Apple" {
		t.Errorf("top = %q, want Apple (sub-gap signal must not override resolve order)", got)
	}
}

// TestEngine_Signals_NegativeSalienceDemotes: A (resolved first) carries a
// negatively-weighted predicate — the tests/generated/mocks case (gh#441). The
// demotion overcomes A's resolve-order lead, so B reorders ahead. Additive-only
// salience could never do this: a max over non-negative weights can only boost.
func TestEngine_Signals_NegativeSalienceDemotes(t *testing.T) {
	// A carries the demotion predicate; B is clean. Both same class (no ontology
	// signal). A leads by resolveScale (4.0); demotion on A = salienceScale*1.5 =
	// 4.5 > 4.0, so B wins.
	a := entity("acme.ops.code.repo.symbol.Apple", "Apple", "a_test.go",
		message.Triple{Predicate: "entity.ontology.class", Object: "http://x/Same"},
		message.Triple{Predicate: "code.artifact.test", Object: "true"})
	b := entity("acme.ops.code.repo.symbol.Banana", "Banana", "b.go",
		message.Triple{Predicate: "entity.ontology.class", Object: "http://x/Same"})
	g := &fakeGraph{
		status:   readyStatus(),
		seeds:    map[string][]string{"zzz": {a.ID, b.ID}},
		entities: map[string]*fusion.Entity{a.ID: a, b.ID: b},
	}
	sig := fakeSignals{salience: map[string]float64{"code.artifact.test": -1.5}}
	eng := fusion.NewEngine(g, nil).WithSignals(sig)
	if got := topName(t, eng); got != "Banana" {
		t.Errorf("top = %q, want Banana (negatively-weighted predicate demotes A below B)", got)
	}
}

// TestEngine_Signals_BoostAndDemotionCompose: the issue's core case (gh#441) —
// impl and test carry the SAME salient (doc-comment) predicate, so additive
// salience alone cannot separate them; the test ALSO carries a demotion
// predicate, and boost+demotion compose so the impl sorts ahead of its test.
func TestEngine_Signals_BoostAndDemotionCompose(t *testing.T) {
	// Impl (A) resolves first; both carry the same +1.0 doc-comment salience.
	// Test (B) additionally carries a -1.5 demotion. Net: A salience = +1.0,
	// B salience = 1.0 - 1.5 = -0.5. Salience gap = salienceScale*(1.0-(-0.5)) =
	// 3.0*1.5 = 4.5 > resolve gap 4.0, so the impl stays ahead of the test even
	// though the test resolved... second here, so also assert order explicitly.
	impl := entity("acme.ops.code.repo.symbol.retry", "retry", "retry.go",
		message.Triple{Predicate: "entity.ontology.class", Object: "http://x/Same"},
		message.Triple{Predicate: "code.symbol.doc", Object: "retries the op"})
	test := entity("acme.ops.code.repo.symbol.retryTest", "retry", "retry_test.go",
		message.Triple{Predicate: "entity.ontology.class", Object: "http://x/Same"},
		message.Triple{Predicate: "code.symbol.doc", Object: "tests retry"},
		message.Triple{Predicate: "code.artifact.test", Object: "true"})
	// Resolve the TEST first (adversarial: it has the resolve-order lead), so the
	// demotion must overcome resolve order to put the impl on top.
	g := &fakeGraph{
		status:   readyStatus(),
		seeds:    map[string][]string{"zzz": {test.ID, impl.ID}},
		entities: map[string]*fusion.Entity{test.ID: test, impl.ID: impl},
	}
	sig := fakeSignals{salience: map[string]float64{
		"code.symbol.doc":    1.0,
		"code.artifact.test": -1.5,
	}}
	eng := fusion.NewEngine(g, nil).WithSignals(sig)
	resp, err := eng.Fuse(context.Background(), fusion.Request{Query: "zzz"}, refLens{})
	if err != nil {
		t.Fatalf("Fuse: %v", err)
	}
	if len(resp.Nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(resp.Nodes))
	}
	if resp.Nodes[0].Path != "retry.go" || resp.Nodes[1].Path != "retry_test.go" {
		t.Errorf("order = [%s, %s], want [retry.go, retry_test.go] (impl ahead of its test)",
			resp.Nodes[0].Path, resp.Nodes[1].Path)
	}
}

// TestEngine_Signals_MaxNotSum_NoFactCountInflation: the anti-inflation
// property the signed aggregation must preserve — a densely-annotated entity
// must NOT outrank a sharply-identified one purely on fact count (the reason
// entitySalience takes the positive EXTREME, not a sum). This is also the
// backward-compat guard for every all-positive config from before gh#441.
func TestEngine_Signals_MaxNotSum_NoFactCountInflation(t *testing.T) {
	// A (resolved first, sharp): one +2.0 predicate → salience 2.0 either way.
	// B (resolved second, dense): four +1.0 predicates → max 1.0, sum 4.0.
	// Under the correct max/extreme aggregation, A's resolve lead (4.0) holds and
	// A stays on top. Under a (wrong) sum aggregation, B's salience 4.0 vs A's 2.0
	// gives salienceScale*(4-2)=6 > 4 and B would flip ahead. Assert A wins.
	a := entity("acme.ops.code.repo.symbol.Apple", "Apple", "a.go",
		message.Triple{Predicate: "entity.ontology.class", Object: "http://x/Same"},
		message.Triple{Predicate: "acme.identity.serial", Object: "SN-1"})
	b := entity("acme.ops.code.repo.symbol.Banana", "Banana", "b.go",
		message.Triple{Predicate: "entity.ontology.class", Object: "http://x/Same"},
		message.Triple{Predicate: "acme.label.one", Object: "1"},
		message.Triple{Predicate: "acme.label.two", Object: "2"},
		message.Triple{Predicate: "acme.label.three", Object: "3"},
		message.Triple{Predicate: "acme.label.four", Object: "4"})
	g := &fakeGraph{
		status:   readyStatus(),
		seeds:    map[string][]string{"zzz": {a.ID, b.ID}},
		entities: map[string]*fusion.Entity{a.ID: a, b.ID: b},
	}
	sig := fakeSignals{salience: map[string]float64{
		"acme.identity.serial": 2.0,
		"acme.label.one":       1.0,
		"acme.label.two":       1.0,
		"acme.label.three":     1.0,
		"acme.label.four":      1.0,
	}}
	eng := fusion.NewEngine(g, nil).WithSignals(sig)
	if got := topName(t, eng); got != "Apple" {
		t.Errorf("top = %q, want Apple (sharp entity holds; dense fact count must not inflate via sum)", got)
	}
}
