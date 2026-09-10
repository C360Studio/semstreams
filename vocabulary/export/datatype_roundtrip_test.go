package export

import (
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/vocabulary"
)

// This file owns the three consequences of #1267 end to end, through the
// authoritative ENTITY_STATES persistence seam rather than a helper that
// reconstructs the round trip. Each assertion was committed first as the
// CURRENT (broken) output and flipped here by the change that fixed it:
//
//  1. a predicate declared "int" emitted xsd:double after the JSON round trip
//     (encoding/json decodes every JSON number into float64 into an `any`);
//  2. a triple carrying message.EntityReferenceDatatype emitted a LITERAL whose
//     datatype IRI was the relative reference "@id" — invalid RDF (#1272);
//  3. a triple carrying "rdf:JSON" emitted an unexpanded prefix, because
//     expandDatatypePrefix knew only "xsd:".

const (
	roundTripEntityID = "acme.ops.gcs.robotics.drone.001"
	roundTripPeerID   = "acme.ops.gcs.robotics.drone.002"
	roundTripPeerIRI  = "https://semstreams.semanticstream.ing/entities/acme/ops/gcs/robotics/drone/002"

	roundTripIntPred      = "robotics.battery.cycles"
	roundTripRefPred      = "robotics.component.has"
	roundTripDeclaredRef  = "robotics.component.mirrors"
	roundTripJSONPred     = "agent.todo.record"
	roundTripUndeclared   = "robotics.status.mode"
	roundTripTypePred     = "graph.type.rdf"
	roundTripDateTimePred = "robotics.flight.startedat"

	xsdIntegerIRI  = "http://www.w3.org/2001/XMLSchema#integer"
	xsdDoubleIRI   = "http://www.w3.org/2001/XMLSchema#double"
	xsdDateTimeIRI = "http://www.w3.org/2001/XMLSchema#dateTime"
	rdfJSONIRI     = "http://www.w3.org/1999/02/22-rdf-syntax-ns#JSON"
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

func serializeBoth(t *testing.T, triples []message.Triple) (turtle, ntriples string) {
	t.Helper()

	turtle, err := SerializeToString(triples, Turtle)
	if err != nil {
		t.Fatalf("SerializeToString(Turtle): %v", err)
	}
	ntriples, err = SerializeToString(triples, NTriples)
	if err != nil {
		t.Fatalf("SerializeToString(NTriples): %v", err)
	}
	return turtle, ntriples
}

func mustContain(t *testing.T, format, out, want string) {
	t.Helper()
	if !strings.Contains(out, want) {
		t.Errorf("%s output does not carry %s\n%s", format, want, out)
	}
}

func mustNotContain(t *testing.T, format, out, unwanted string) {
	t.Helper()
	if strings.Contains(out, unwanted) {
		t.Errorf("%s output unexpectedly carries %s\n%s", format, unwanted, out)
	}
}

// TestDeclaredIntSurvivesEntityStateRoundTrip is the whole issue in one
// fixture. The Go value that reaches the serializer is float64 no matter what
// the author declared, so the declaration is the only place the fact survives.
//
// spec: predicate-contract / RDF export honors the declared datatype and never fabricates a value
func TestDeclaredIntSurvivesEntityStateRoundTrip(t *testing.T) {
	defer vocabulary.SnapshotRegistry()()
	vocabulary.Register(roundTripIntPred,
		vocabulary.WithDescription("battery charge cycles"),
		vocabulary.WithDataType(vocabulary.DataTypeInt))

	decoded := roundTripThroughEntityStates(t, []message.Triple{{
		Subject:   roundTripEntityID,
		Predicate: roundTripIntPred,
		Object:    5,
	}})

	if got, ok := decoded[0].Object.(float64); !ok || got != 5 {
		t.Fatalf("ENTITY_STATES round trip: got Object %#v, want float64(5) — "+
			"the premise of this fixture is that the Go type does NOT survive", decoded[0].Object)
	}

	_, ntriples := serializeBoth(t, decoded)
	mustContain(t, "N-Triples", ntriples, `"5"^^<`+xsdIntegerIRI+`>`)
	mustNotContain(t, "N-Triples", ntriples, xsdDoubleIRI)
}

// TestDeclarationContradictedByValueFallsBackToObservation is the
// never-fabricate rule at its most concrete: a fractional value under an
// integer declaration serializes as what it is, not as a rounded lexical form.
//
// spec: predicate-contract / RDF export honors the declared datatype and never fabricates a value
func TestDeclarationContradictedByValueFallsBackToObservation(t *testing.T) {
	defer vocabulary.SnapshotRegistry()()
	vocabulary.Register(roundTripIntPred,
		vocabulary.WithDescription("battery charge cycles"),
		vocabulary.WithDataType(vocabulary.DataTypeInt))

	decoded := roundTripThroughEntityStates(t, []message.Triple{{
		Subject:   roundTripEntityID,
		Predicate: roundTripIntPred,
		Object:    5.5,
	}})

	_, ntriples := serializeBoth(t, decoded)
	mustContain(t, "N-Triples", ntriples, `"5.5"^^<`+xsdDoubleIRI+`>`)
	mustNotContain(t, "N-Triples", ntriples, xsdIntegerIRI)
	for _, altered := range []string{`"5"^^`, `"6"^^`} {
		mustNotContain(t, "N-Triples", ntriples, altered)
	}
}

// TestEntityReferenceDatatypeSerializesAsResource closes #1272. The framework's
// own per-triple entity-reference marker denotes an IRI node; it emitted a
// literal whose datatype IRI was the relative reference "@id".
//
// The negative assertion is on `^^<@id>`, not `^^@id`: both serializers wrap an
// uncompactable datatype in angle brackets, so a probe asserting the unwrapped
// form passes while the defect is present.
//
// spec: predicate-contract / RDF export honors the declared datatype and never fabricates a value
func TestEntityReferenceDatatypeSerializesAsResource(t *testing.T) {
	defer vocabulary.SnapshotRegistry()()
	vocabulary.Register(roundTripRefPred,
		vocabulary.WithDescription("component containment"),
		vocabulary.WithDataType(vocabulary.DataTypeEntityID))

	decoded := roundTripThroughEntityStates(t, []message.Triple{{
		Subject:   roundTripEntityID,
		Predicate: roundTripRefPred,
		Object:    roundTripPeerID,
		Datatype:  message.EntityReferenceDatatype,
	}})

	turtle, ntriples := serializeBoth(t, decoded)
	for format, out := range map[string]string{"Turtle": turtle, "N-Triples": ntriples} {
		mustContain(t, format, out, "<"+roundTripPeerIRI+">")
		mustNotContain(t, format, out, `^^<@id>`)
		mustNotContain(t, format, out, `"`+roundTripPeerID+`"`)
	}
}

// TestDeclaredEntityIDAndPerTripleMarkerAgree pins that the two spellings of
// the entity-reference fact — the per-predicate declaration `entity_id` and the
// per-triple marker `@id` — produce identical output. ADR-107 deliberately
// keeps them as two strings; this is where they converge.
//
// spec: predicate-contract / RDF export honors the declared datatype and never fabricates a value
func TestDeclaredEntityIDAndPerTripleMarkerAgree(t *testing.T) {
	defer vocabulary.SnapshotRegistry()()
	vocabulary.Register(roundTripRefPred, vocabulary.WithDataType(vocabulary.DataTypeEntityID))
	vocabulary.Register(roundTripDeclaredRef, vocabulary.WithDataType(vocabulary.DataTypeEntityID))

	marked, err := SerializeToString([]message.Triple{{
		Subject: roundTripEntityID, Predicate: roundTripRefPred,
		Object: roundTripPeerID, Datatype: message.EntityReferenceDatatype,
	}}, NTriples)
	if err != nil {
		t.Fatalf("SerializeToString(marked): %v", err)
	}
	declaredOnly, err := SerializeToString([]message.Triple{{
		Subject: roundTripEntityID, Predicate: roundTripDeclaredRef,
		Object: roundTripPeerID,
	}}, NTriples)
	if err != nil {
		t.Fatalf("SerializeToString(declared): %v", err)
	}

	markedObject := objectTerm(t, marked)
	declaredObject := objectTerm(t, declaredOnly)
	if markedObject != declaredObject {
		t.Errorf("the two spellings of an entity reference disagree:\n  per-triple @id: %s\n  declared entity_id: %s",
			markedObject, declaredObject)
	}
	if markedObject != "<"+roundTripPeerIRI+">" {
		t.Errorf("object term = %s, want the entity IRI <%s>", markedObject, roundTripPeerIRI)
	}
}

// objectTerm returns the object term of a single-line N-Triples document.
func objectTerm(t *testing.T, ntriples string) string {
	t.Helper()
	line := strings.TrimSpace(ntriples)
	fields := strings.SplitN(line, " ", 3)
	if len(fields) != 3 {
		t.Fatalf("not a single N-Triples statement: %q", ntriples)
	}
	return strings.TrimSuffix(strings.TrimSpace(fields[2]), " .")
}

// TestRDFJSONDatatypeExpandsToAnIRI pins that a prefixed datatype this
// repository actually writes reaches the output as an IRI. Turtle compacts it
// back to rdf:JSON, but only with an @prefix declaration binding the prefix —
// which is exactly what was missing.
//
// spec: predicate-contract / RDF export honors the declared datatype and never fabricates a value
func TestRDFJSONDatatypeExpandsToAnIRI(t *testing.T) {
	defer vocabulary.SnapshotRegistry()()
	vocabulary.Register(roundTripJSONPred,
		vocabulary.WithDescription("todo record document"),
		vocabulary.WithDataType(vocabulary.DataTypeString))

	decoded := roundTripThroughEntityStates(t, []message.Triple{{
		Subject:   roundTripEntityID,
		Predicate: roundTripJSONPred,
		Object:    `{"done":true}`,
		Datatype:  "rdf:JSON",
	}})

	turtle, ntriples := serializeBoth(t, decoded)
	mustContain(t, "Turtle", turtle, `^^rdf:JSON`)
	mustContain(t, "Turtle", turtle, `@prefix rdf: <http://www.w3.org/1999/02/22-rdf-syntax-ns#> .`)
	mustNotContain(t, "Turtle", turtle, `^^<rdf:JSON>`)
	mustContain(t, "N-Triples", ntriples, `^^<`+rdfJSONIRI+`>`)
}

// TestPerTripleDatatypeBeatsTheDeclaration pins the top of the precedence
// order: a statement about THIS value wins over a statement about the predicate.
//
// spec: predicate-contract / RDF export honors the declared datatype and never fabricates a value
func TestPerTripleDatatypeBeatsTheDeclaration(t *testing.T) {
	defer vocabulary.SnapshotRegistry()()
	vocabulary.Register(roundTripIntPred, vocabulary.WithDataType(vocabulary.DataTypeInt))

	out, err := SerializeToString([]message.Triple{{
		Subject:   roundTripEntityID,
		Predicate: roundTripIntPred,
		Object:    5,
		Datatype:  "xsd:decimal",
	}}, NTriples)
	if err != nil {
		t.Fatalf("SerializeToString: %v", err)
	}
	mustContain(t, "N-Triples", out, `"5"^^<http://www.w3.org/2001/XMLSchema#decimal>`)
	mustNotContain(t, "N-Triples", out, xsdIntegerIRI)
}

// TestDeclaredDateTimeSurvivesTheRoundTripAsAString pins the one declaration
// that recovers a fact observation lost to serialization: a time.Time comes
// back from the authoritative store as an RFC 3339 string.
//
// spec: predicate-contract / RDF export honors the declared datatype and never fabricates a value
func TestDeclaredDateTimeSurvivesTheRoundTripAsAString(t *testing.T) {
	defer vocabulary.SnapshotRegistry()()
	vocabulary.Register(roundTripDateTimePred, vocabulary.WithDataType(vocabulary.DataTypeDateTime))

	decoded := roundTripThroughEntityStates(t, []message.Triple{{
		Subject:   roundTripEntityID,
		Predicate: roundTripDateTimePred,
		Object:    time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC),
	}})

	if _, ok := decoded[0].Object.(string); !ok {
		t.Fatalf("ENTITY_STATES round trip: got Object %#v, want a string", decoded[0].Object)
	}

	_, ntriples := serializeBoth(t, decoded)
	mustContain(t, "N-Triples", ntriples, `"2026-09-09T12:00:00Z"^^<`+xsdDateTimeIRI+`>`)
}

// TestDeclaredJSONRefusesAValueThatIsNotJSON pins that the json declaration is
// checked against the observed value like every other one. Each declared branch
// verifies the value can carry the type — entity_id checks IsValidEntityID, int
// checks the integer lexical form, datetime parses RFC 3339, bool type-asserts —
// and json was the one branch that applied to any string at all, emitting
// "hovering"^^rdf:JSON, an ill-typed literal the requirement forbids.
//
// spec: predicate-contract / RDF export honors the declared datatype and never fabricates a value
func TestDeclaredJSONRefusesAValueThatIsNotJSON(t *testing.T) {
	defer vocabulary.SnapshotRegistry()()
	vocabulary.Register(roundTripJSONPred, vocabulary.WithDataType(vocabulary.DataTypeJSON))

	decoded := roundTripThroughEntityStates(t, []message.Triple{{
		Subject:   roundTripEntityID,
		Predicate: roundTripJSONPred,
		Object:    "hovering",
	}})

	_, ntriples := serializeBoth(t, decoded)
	if strings.Contains(ntriples, rdfJSONIRI) {
		t.Errorf("a non-JSON string under a json declaration emitted an rdf:JSON literal: %s", ntriples)
	}
	mustContain(t, "N-Triples", ntriples, `"hovering"`)
}

// TestDeclaredJSONAppliesToAValueThatIsJSON is the other half: the guard above
// must not have disabled the declaration for values that do carry JSON.
//
// spec: predicate-contract / RDF export honors the declared datatype and never fabricates a value
func TestDeclaredJSONAppliesToAValueThatIsJSON(t *testing.T) {
	defer vocabulary.SnapshotRegistry()()
	vocabulary.Register(roundTripJSONPred, vocabulary.WithDataType(vocabulary.DataTypeJSON))

	decoded := roundTripThroughEntityStates(t, []message.Triple{{
		Subject:   roundTripEntityID,
		Predicate: roundTripJSONPred,
		Object:    `{"done":true}`,
	}})

	_, ntriples := serializeBoth(t, decoded)
	mustContain(t, "N-Triples", ntriples, "^^<"+rdfJSONIRI+">")
}

// TestUndeclaredPredicateSerializesByObservation pins that a predicate
// declaring nothing is untouched by this change.
//
// spec: predicate-contract / RDF export honors the declared datatype and never fabricates a value
func TestUndeclaredPredicateSerializesByObservation(t *testing.T) {
	defer vocabulary.SnapshotRegistry()()
	vocabulary.ClearRegistry()

	out, err := SerializeToString([]message.Triple{
		{Subject: roundTripEntityID, Predicate: roundTripUndeclared, Object: "hovering"},
		{Subject: roundTripEntityID, Predicate: roundTripIntPred, Object: 5.0},
		{Subject: roundTripEntityID, Predicate: roundTripRefPred, Object: roundTripPeerID},
	}, NTriples)
	if err != nil {
		t.Fatalf("SerializeToString: %v", err)
	}

	mustContain(t, "N-Triples", out, `"hovering"`)
	mustContain(t, "N-Triples", out, `"5.0"^^<`+xsdDoubleIRI+`>`)
	mustContain(t, "N-Triples", out, "<"+roundTripPeerIRI+">")
	mustNotContain(t, "N-Triples", out, xsdIntegerIRI)
}

// TestAbsoluteIRIObjectSerializesAsResource closes #1142.
// message.IsValidEntityID is false for anything carrying a scheme, so an IRI
// object fell to the literal branch by construction.
//
// spec: predicate-contract / RDF export honors the declared datatype and never fabricates a value
func TestAbsoluteIRIObjectSerializesAsResource(t *testing.T) {
	defer vocabulary.SnapshotRegistry()()
	vocabulary.ClearRegistry()

	iris := []string{
		"http://www.w3.org/2002/07/owl#Thing",
		"https://schema.org/Vehicle",
		"urn:uuid:3f2504e0-4f89-11d3-9a0c-0305e82c3301",
	}
	triples := make([]message.Triple, 0, len(iris))
	for _, iri := range iris {
		triples = append(triples, message.Triple{
			Subject: roundTripEntityID, Predicate: roundTripTypePred, Object: iri,
		})
	}

	out, err := SerializeToString(triples, NTriples)
	if err != nil {
		t.Fatalf("SerializeToString: %v", err)
	}
	for _, iri := range iris {
		mustContain(t, "N-Triples", out, "<"+iri+">")
		mustNotContain(t, "N-Triples", out, `"`+iri+`"`)
	}
}

// TestCanonicalEntityIDIsNotMistakenForAnIRI pins task 6.3: the entity-ID
// branch and the absolute-IRI branch cannot both match, because a canonical
// 6-part ID carries no scheme and ":" is not a legal entity-ID character.
//
// spec: predicate-contract / RDF export honors the declared datatype and never fabricates a value
func TestCanonicalEntityIDIsNotMistakenForAnIRI(t *testing.T) {
	if isAbsoluteIRI(roundTripPeerID) {
		t.Errorf("isAbsoluteIRI(%q) = true; a canonical entity ID carries no scheme", roundTripPeerID)
	}
	for _, iri := range []string{"http://x/y", "https://x/y", "urn:uuid:1"} {
		if message.IsValidEntityID(iri) {
			t.Errorf("IsValidEntityID(%q) = true; a scheme is not a legal entity ID", iri)
		}
	}
}
