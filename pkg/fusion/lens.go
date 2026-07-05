package fusion

import (
	"context"
	"fmt"
	"slices"

	"github.com/c360studio/semstreams/message"
)

// The Lens SPI is the ONLY domain-specific surface of the deterministic fusion
// engine (ADR-062 increment 3). The engine owns resolve / expand / rank / budget
// / envelope; a product-supplied Lens declares which edges to walk, how to read
// an entity's human-facing fields, and how to hydrate its verbatim body. One
// engine, many lenses (code, docs, …) — the proof that fusion is a general
// primitive, not code in disguise.
//
// Lifted from semsource's validated `source/fusion` Lens (rule-of-three:
// semsource code + docs lenses, plus our research-graph retrieval). The one
// deliberate change from semsource's shape is Hydrate's return type — see the
// hydration contract below.

// Entity is the minimal projection a Lens reads: the entity's ID and its current
// triples. Kept deliberately small (no graph.EntityState, no NATS) so pkg/fusion
// stays a near-leaf — the Lens reads human-facing fields off the triples.
type Entity struct {
	ID      string
	Triples []message.Triple
}

// First returns the first object for predicate rendered as a string, or "". A
// convenience for lenses/ranker so they read triple objects without touching the
// triple shape.
func (e *Entity) First(predicate string) string {
	for i := range e.Triples {
		if e.Triples[i].Predicate == predicate {
			return objectString(e.Triples[i].Object)
		}
	}
	return ""
}

// FirstInt returns the first object for predicate as an int, or 0.
func (e *Entity) FirstInt(predicate string) int {
	for i := range e.Triples {
		if e.Triples[i].Predicate == predicate {
			switch v := e.Triples[i].Object.(type) {
			case int:
				return v
			case int64:
				return int(v)
			case float64:
				return int(v)
			}
		}
	}
	return 0
}

// objectString renders a triple object as a string (objects are string IRIs/
// literals, or numeric literals).
func objectString(o any) string {
	switch v := o.(type) {
	case string:
		return v
	case nil:
		return ""
	default:
		return fmt.Sprint(v)
	}
}

// ResolveMode picks how the engine turns a raw query string into seed entities.
// The Lens classifies the query; the engine maps the mode to a resolve strategy
// (symbol → graph.query.byName exact lookup, prefix → graph.query.prefix, nl →
// semantic search). Provenance follows from the mode: symbol/prefix are
// deterministic, nl is embedding.
type ResolveMode string

const (
	// ResolveModeNL routes the query to semantic (embedding) search. Seeds are
	// embedding-ranked, so the response provenance is "embedding", not
	// "deterministic".
	ResolveModeNL ResolveMode = "nl"

	// ResolveModeSymbol routes to deterministic exact name/title lookup
	// (graph.query.byName). The provenance the byName index makes honest.
	ResolveModeSymbol ResolveMode = "symbol"

	// ResolveModePrefix routes to deterministic prefix lookup
	// (graph.query.prefix) — e.g. a path or namespace stem.
	ResolveModePrefix ResolveMode = "prefix"
)

// Facet names one of the three edge-walk facets the engine derives from a lens's
// EdgeSpecs. The string values match the corresponding Want values so lens authors
// and callers share one vocabulary. (WantBody has no Facet — it is not edge-driven.)
type Facet string

// The three edge-walk facets.
const (
	FacetRelations Facet = "relations" // the per-node relations map (forward + reverse roles)
	FacetPaths     Facet = "paths"     // the outgoing paths DFS
	FacetImpact    Facet = "impact"    // the incoming impact BFS
)

// EdgeSpec declares one relationship predicate the engine should expand around a
// seed, with the role labels for its forward and (optional) reverse directions.
// For code: {Predicate: "code.relationship.calls", OutgoingRole: "callee",
// IncomingRole: "caller"}. An empty IncomingRole skips the reverse direction.
//
// Facets optionally restricts which edge-walk facets this predicate feeds. An empty
// Facets means the edge participates in ALL three facets (relations, paths, impact)
// — the backward-compatible default. A non-empty Facets restricts the edge to
// exactly the named facets, so e.g. a containment edge can populate the relations
// map (Facets: {FacetRelations}) without inflating the impact walk.
type EdgeSpec struct {
	Predicate    string
	OutgoingRole string
	IncomingRole string  // "" to skip the reverse direction
	Facets       []Facet // nil/empty = all facets; else only the named facets
}

// walksFacet reports whether this EdgeSpec participates in facet f. An empty Facets
// participates in every facet (backward-compatible default).
func (s EdgeSpec) walksFacet(f Facet) bool {
	return len(s.Facets) == 0 || slices.Contains(s.Facets, f)
}

// Locator is a domain-general place: a file path or URL, an optional section /
// anchor fragment (docs), and an optional line range (code). One of Lines or
// Fragment is typically set, not both.
type Locator struct {
	Path     string // file path or URL
	Fragment string // section / anchor (docs)
	Lines    [2]int // line range (code); zero value means line-less
}

// Lens supplies the only domain-specific parts of fusion (ADR-062). The engine
// owns resolve / expand / rank / budget / envelope; the lens declares which
// edges to walk, how to read an entity's human-facing fields, and how to hydrate
// its verbatim body.
type Lens interface {
	// Name identifies the lens (e.g. "code", "docs"). Used as the registry key.
	Name() string

	// ResolveMode picks how to turn the raw query into seeds.
	ResolveMode(query string) ResolveMode

	// Edges are the relationship predicates to expand around each seed.
	Edges() []EdgeSpec

	// Label is the entity's human name (e.g. from dc.terms.title).
	Label(e *Entity) string

	// Kind is a short human kind for the entity (e.g. "function", "doc").
	Kind(e *Entity) string

	// Location returns the entity's domain-general place (file path or URL,
	// optional section fragment, optional line range).
	Location(e *Entity) Locator

	// Hydrate returns a HANDLE to the entity's verbatim body — never the bytes
	// themselves and never a filesystem read. The engine resolves the handle's
	// StorageInstance to a registered storage.Store and reads the body with
	// Get(Key) (ADR-062 hydration contract, increment 4). Returning a handle
	// (not a string) is what makes fusion deployment-independent: a remote
	// caller of a standalone fusion service (semsource ADR-0006) cannot read the
	// service's worktree, so bodies must be addressable through a backend-
	// agnostic store. Return (nil, nil) when the entity has no verbatim body.
	//
	// Error policy (engine contract): hydration is best-effort. A non-nil error
	// DEGRADES the response — the engine omits that node's body and continues —
	// it does NOT fail the fuse. This mirrors the engine's degrade-don't-fail
	// posture for sub-query failures (see Fuse). Lenses should return an error
	// only for genuine retrieval faults, not for "no body" (use (nil, nil)).
	Hydrate(ctx context.Context, e *Entity) (*message.StorageReference, error)
}

// ProvenanceForMode maps a ResolveMode to the honesty-envelope provenance tier:
// symbol/prefix seeds are deterministic exact lookups; nl seeds come from
// embedding search. (The research_graph LLM sibling is the only "llm" source —
// the deterministic engine never sets it.) Exported so the engine's assemble
// step and any lens-driven caller stamp provenance consistently.
//
// Deliberate divergence from semsource's provenanceFor (source/fusion/engine.go),
// whose catch-all is "deterministic" and only embedding is named: there,
// ResolveMode is a CLOSED int enum so "unknown" is unreachable. Here ResolveMode
// is an OPEN string type, so the catch-all must be the conservative tier —
// "embedding" — because only "deterministic" permits a caller to treat an empty
// result as an authoritative not-found, and an unclassified seed has not earned
// that. CAUTION: this means any NEW deterministic mode added to the enum MUST be
// added to the deterministic case below, or it silently degrades to embedding
// (no compile error — open string enum).
func ProvenanceForMode(mode ResolveMode) Provenance {
	switch mode {
	case ResolveModeSymbol, ResolveModePrefix:
		return ProvenanceDeterministic
	case ResolveModeNL:
		return ProvenanceEmbedding
	default:
		return ProvenanceEmbedding
	}
}
