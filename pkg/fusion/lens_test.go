package fusion_test

import (
	"context"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/pkg/fusion"
)

// refLens is a minimal but realistic reference Lens proving the SPI is
// implementable end-to-end (ADR-062 increment 3). It reads human-facing fields
// off an entity's triples and hydrates via a StorageReference handle — never a
// filesystem read, never returning bytes (the headless-safe hydration contract).
// Concrete product lenses (semsource code/docs, a research-graph lens) follow
// the same shape with domain-specific predicates.
type refLens struct{}

const (
	refTitlePredicate    = "dc.terms.title"
	refKindPredicate     = "entity.kind"
	refPathPredicate     = "source.path"
	refStorageInstancePr = "content.storage_instance"
	refStorageKeyPr      = "content.storage_key"
)

func (refLens) Name() string { return "ref" }

func (refLens) ResolveMode(query string) fusion.ResolveMode {
	switch {
	case strings.HasPrefix(query, "/"):
		return fusion.ResolveModePrefix
	case !strings.ContainsAny(query, " \t"): // a bare token reads as a symbol
		return fusion.ResolveModeSymbol
	default:
		return fusion.ResolveModeNL
	}
}

func (refLens) Edges() []fusion.EdgeSpec {
	return []fusion.EdgeSpec{
		{Predicate: "ref.relationship.references", OutgoingRole: "referent", IncomingRole: "referrer"},
	}
}

func (refLens) Label(e *fusion.Entity) string { return stringObject(e, refTitlePredicate) }

func (refLens) Kind(e *fusion.Entity) string {
	if k := stringObject(e, refKindPredicate); k != "" {
		return k
	}
	return "entity"
}

func (refLens) Location(e *fusion.Entity) fusion.Locator {
	return fusion.Locator{Path: stringObject(e, refPathPredicate)}
}

func (refLens) Hydrate(_ context.Context, e *fusion.Entity) (*message.StorageReference, error) {
	inst := stringObject(e, refStorageInstancePr)
	key := stringObject(e, refStorageKeyPr)
	if inst == "" || key == "" {
		return nil, nil // no verbatim body for this entity
	}
	return &message.StorageReference{StorageInstance: inst, Key: key}, nil
}

// stringObject returns the first string Object for predicate, or "".
func stringObject(e *fusion.Entity, predicate string) string {
	if e == nil {
		return ""
	}
	for _, t := range e.Triples {
		if t.Predicate == predicate {
			if s, ok := t.Object.(string); ok {
				return s
			}
		}
	}
	return ""
}

// compile-time conformance: refLens must satisfy the Lens SPI.
var _ fusion.Lens = refLens{}

func TestLens_ReferenceImplementation_ReadsFields(t *testing.T) {
	e := &fusion.Entity{
		ID: "acme.ops.code.repo.symbol.OnEvent",
		Triples: []message.Triple{
			{Predicate: refTitlePredicate, Object: "OnEvent"},
			{Predicate: refKindPredicate, Object: "function"},
			{Predicate: refPathPredicate, Object: "handlers/on_event.go"},
		},
	}
	var l fusion.Lens = refLens{}
	if got := l.Label(e); got != "OnEvent" {
		t.Errorf("Label = %q, want OnEvent", got)
	}
	if got := l.Kind(e); got != "function" {
		t.Errorf("Kind = %q, want function", got)
	}
	if got := l.Location(e); got.Path != "handlers/on_event.go" {
		t.Errorf("Location.Path = %q, want handlers/on_event.go", got.Path)
	}
}

func TestLens_Hydrate_ReturnsHandleNotBytes(t *testing.T) {
	l := refLens{}

	// Entity with storage triples → a StorageReference handle the engine derefs.
	withBody := &fusion.Entity{
		ID: "acme.ops.code.repo.symbol.OnEvent",
		Triples: []message.Triple{
			{Predicate: refStorageInstancePr, Object: "objectstore-primary"},
			{Predicate: refStorageKeyPr, Object: "code/on_event.go"},
		},
	}
	ref, err := l.Hydrate(context.Background(), withBody)
	if err != nil {
		t.Fatalf("Hydrate: %v", err)
	}
	if ref == nil {
		t.Fatal("expected a StorageReference handle, got nil")
	}
	if ref.StorageInstance != "objectstore-primary" || ref.Key != "code/on_event.go" {
		t.Errorf("handle = %+v, want {objectstore-primary, code/on_event.go}", ref)
	}

	// Entity without storage triples → (nil, nil): no verbatim body, not an error.
	none := &fusion.Entity{ID: "acme.ops.code.repo.symbol.Bare"}
	ref, err = l.Hydrate(context.Background(), none)
	if err != nil || ref != nil {
		t.Errorf("Hydrate(no body) = (%v, %v), want (nil, nil)", ref, err)
	}
}

func TestResolveMode_Classification(t *testing.T) {
	l := refLens{}
	cases := map[string]fusion.ResolveMode{
		"OnEvent":             fusion.ResolveModeSymbol,
		"/handlers":           fusion.ResolveModePrefix,
		"how does auth work?": fusion.ResolveModeNL,
	}
	for query, want := range cases {
		if got := l.ResolveMode(query); got != want {
			t.Errorf("ResolveMode(%q) = %q, want %q", query, got, want)
		}
	}
}

func TestProvenanceForMode(t *testing.T) {
	cases := map[fusion.ResolveMode]fusion.Provenance{
		fusion.ResolveModeSymbol: fusion.ProvenanceDeterministic,
		fusion.ResolveModePrefix: fusion.ProvenanceDeterministic,
		fusion.ResolveModeNL:     fusion.ProvenanceEmbedding,
		fusion.ResolveMode("?"):  fusion.ProvenanceEmbedding, // unknown is conservatively non-deterministic
	}
	for mode, want := range cases {
		if got := fusion.ProvenanceForMode(mode); got != want {
			t.Errorf("ProvenanceForMode(%q) = %q, want %q", mode, got, want)
		}
	}
}
