package export

import (
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/vocabulary"
)

// This file pins the three consequences of #1267 that motivate honoring the
// registry's declared predicate DataType. Every assertion here describes
// CURRENT behavior; each one is flipped to the target assertion by the same
// change that fixes it, so the fixture cannot have been written to fit the fix.
//
// The three, in the order the issue and its design record them:
//
//  1. a predicate declared "int" emits xsd:double after the authoritative
//     ENTITY_STATES JSON round trip (design M-P1: encoding/json decodes every
//     JSON number into float64 when the destination is `any`);
//  2. a triple carrying message.EntityReferenceDatatype emits a LITERAL whose
//     datatype IRI is the relative reference "@id" — invalid RDF (design M6);
//  3. a triple carrying "rdf:JSON" emits an unexpanded prefix, because
//     expandDatatypePrefix knows only "xsd:" (design M7).

const (
	roundTripEntityID    = "acme.ops.gcs.robotics.drone.001"
	roundTripPeerID      = "acme.ops.gcs.robotics.drone.002"
	roundTripIntPred     = "robotics.battery.cycles"
	roundTripRefPred     = "robotics.component.has"
	roundTripJSONPred    = "agent.todo.record"
	xsdDoubleIRI         = "http://www.w3.org/2001/XMLSchema#double"
	entityReferenceMarks = message.EntityReferenceDatatype
)

// roundTripThroughEntityStates writes triples through the authoritative
// ENTITY_STATES persistence seam and reads them back through the authoritative
// decoder, returning exactly what a graph reader would hand the serializer.
func roundTripThroughEntityStates(t *testing.T, triples []message.Triple) []message.Triple {
	t.Helper()

	entity := &graph.EntityState{
		ID:        roundTripEntityID,
		Triples:   triples,
		UpdatedAt: time.Unix(0, 0).UTC(),
	}
	data, err := graph.MarshalEntityState(entity)
	if err != nil {
		t.Fatalf("MarshalEntityState: %v", err)
	}
	var decoded graph.EntityState
	if err := graph.UnmarshalEntityState(data, &decoded); err != nil {
		t.Fatalf("UnmarshalEntityState: %v", err)
	}
	return decoded.Triples
}

// TestDeclaredIntSurvivesEntityStateRoundTrip pins consequence 1: the declared
// datatype is not consulted, so a whole number that entered the store as an int
// leaves it as a float64 and serializes as xsd:double.
func TestDeclaredIntSurvivesEntityStateRoundTrip(t *testing.T) {
	defer vocabulary.SnapshotRegistry()()
	vocabulary.Register(roundTripIntPred,
		vocabulary.WithDescription("battery charge cycles"),
		vocabulary.WithDataType("int"))

	decoded := roundTripThroughEntityStates(t, []message.Triple{{
		Subject:   roundTripEntityID,
		Predicate: roundTripIntPred,
		Object:    5,
	}})

	if got, ok := decoded[0].Object.(float64); !ok || got != 5 {
		t.Fatalf("ENTITY_STATES round trip: got Object %#v, want float64(5)", decoded[0].Object)
	}

	out, err := SerializeToString(decoded, NTriples)
	if err != nil {
		t.Fatalf("SerializeToString: %v", err)
	}

	// CURRENT behavior (#1267): the declaration says "int" and the exporter
	// emits xsd:double, because it classifies by observed Go type only.
	want := `"5.0"^^<` + xsdDoubleIRI + `>`
	if !strings.Contains(out, want) {
		t.Errorf("N-Triples output does not carry %s\n%s", want, out)
	}
	if strings.Contains(out, "XMLSchema#integer") {
		t.Errorf("N-Triples output unexpectedly carries xsd:integer\n%s", out)
	}
}

// TestEntityReferenceDatatypeSerializesAsLiteral pins consequence 2 (#1272):
// the framework's own per-triple entity-reference marker is emitted as a
// literal whose datatype IRI is the relative reference "@id".
//
// Assert on `^^<@id>`, not `^^@id`: both Turtle and N-Triples wrap a datatype
// that no prefix compacts in angle brackets, so a probe asserting the unwrapped
// form passes while the defect is present.
func TestEntityReferenceDatatypeSerializesAsLiteral(t *testing.T) {
	defer vocabulary.SnapshotRegistry()()
	vocabulary.Register(roundTripRefPred,
		vocabulary.WithDescription("component containment"),
		vocabulary.WithDataType("entity_ref"))

	decoded := roundTripThroughEntityStates(t, []message.Triple{{
		Subject:   roundTripEntityID,
		Predicate: roundTripRefPred,
		Object:    roundTripPeerID,
		Datatype:  entityReferenceMarks,
	}})

	turtle, err := SerializeToString(decoded, Turtle)
	if err != nil {
		t.Fatalf("SerializeToString(Turtle): %v", err)
	}
	ntriples, err := SerializeToString(decoded, NTriples)
	if err != nil {
		t.Fatalf("SerializeToString(NTriples): %v", err)
	}

	// CURRENT behavior (#1272): a literal carrying a relative-reference
	// datatype IRI. Not an IRI node, and not valid RDF.
	want := `"` + roundTripPeerID + `"^^<@id>`
	if !strings.Contains(turtle, want) {
		t.Errorf("Turtle output does not carry %s\n%s", want, turtle)
	}
	if !strings.Contains(ntriples, want) {
		t.Errorf("N-Triples output does not carry %s\n%s", want, ntriples)
	}
	// And the object is NOT emitted as the entity IRI it denotes.
	entityIRI := "/entities/acme/ops/gcs/robotics/drone/002"
	if strings.Contains(ntriples, entityIRI) {
		t.Errorf("N-Triples output unexpectedly carries the entity IRI %s\n%s", entityIRI, ntriples)
	}
}

// TestRDFJSONDatatypeIsNotExpanded pins consequence 3: expandDatatypePrefix
// knows only "xsd:", so the agentic vocabulary's own "rdf:JSON" marker
// (vocabulary/agentic/predicates.go) is emitted verbatim as a relative
// reference, with no rdf: prefix declaration to bind it.
func TestRDFJSONDatatypeIsNotExpanded(t *testing.T) {
	defer vocabulary.SnapshotRegistry()()
	vocabulary.Register(roundTripJSONPred,
		vocabulary.WithDescription("todo record document"),
		vocabulary.WithDataType("string"))

	decoded := roundTripThroughEntityStates(t, []message.Triple{{
		Subject:   roundTripEntityID,
		Predicate: roundTripJSONPred,
		Object:    `{"done":true}`,
		Datatype:  "rdf:JSON",
	}})

	turtle, err := SerializeToString(decoded, Turtle)
	if err != nil {
		t.Fatalf("SerializeToString(Turtle): %v", err)
	}
	ntriples, err := SerializeToString(decoded, NTriples)
	if err != nil {
		t.Fatalf("SerializeToString(NTriples): %v", err)
	}

	// CURRENT behavior (#1267): the prefix is never expanded, so Turtle wraps
	// the raw CURIE text in angle brackets and declares no rdf: prefix, and
	// N-Triples emits the same relative reference where an IRI is required.
	if !strings.Contains(turtle, `^^<rdf:JSON>`) {
		t.Errorf("Turtle output does not carry ^^<rdf:JSON>\n%s", turtle)
	}
	if strings.Contains(turtle, "@prefix rdf:") {
		t.Errorf("Turtle output unexpectedly declares the rdf: prefix\n%s", turtle)
	}
	if !strings.Contains(ntriples, `^^<rdf:JSON>`) {
		t.Errorf("N-Triples output does not carry ^^<rdf:JSON>\n%s", ntriples)
	}
}
