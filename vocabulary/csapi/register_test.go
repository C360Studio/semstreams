package csapi

import (
	"testing"

	"github.com/c360studio/semstreams/vocabulary"
)

// End-to-end integration with vocabulary/export's prefix compaction
// is covered by the smoke test suite that iterates over registered
// prefixes; this test guards the package-local idempotency contract.
func TestRegisterIsIdempotent(t *testing.T) {
	if err := Register(); err != nil {
		t.Fatalf("Register() after init: %v", err)
	}
	if err := Register(); err != nil {
		t.Fatalf("Register() second call: %v", err)
	}
}

// TestPredicatesRegisteredWithIRIMappings pins the gh#182 contract:
// every dotted predicate constant in this package must be registered
// in the global vocabulary registry with its `*IRI` counterpart as
// StandardIRI metadata. Without this binding, exporters lose the
// dotted→IRI mapping and JSON-LD output drops the spec-rooted IRIs.
func TestPredicatesRegisteredWithIRIMappings(t *testing.T) {
	cases := []struct {
		dotted string
		iri    string
	}{
		{ProducedBy, ProducedByIRI},
		{ResultTimeRange, ResultTimeRangeIRI},
		{PhenomenonTimeRange, PhenomenonTimeRangeIRI},
		{ResultType, ResultTypeIRI},
		{ControlsSystem, ControlsSystemIRI},
		{PartOfControlStream, PartOfControlStreamIRI},
		{EventForSystem, EventForSystemIRI},
		{HasSource, HasSourceIRI},
		{HasResultSchema, HasResultSchemaIRI},
		{HasCommandSchema, HasCommandSchemaIRI},
	}
	for _, c := range cases {
		meta := vocabulary.GetPredicateMetadata(c.dotted)
		if meta == nil {
			t.Errorf("predicate %q not registered in vocabulary registry", c.dotted)
			continue
		}
		if meta.StandardIRI != c.iri {
			t.Errorf("predicate %q registered with IRI %q, want %q",
				c.dotted, meta.StandardIRI, c.iri)
		}
	}
}
