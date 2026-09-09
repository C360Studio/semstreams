package export

import (
	"math"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/vocabulary"
	"pgregory.net/rapid"
)

// Property harness for the never-fabricate rule. The invariant is the spec
// clause, not a list of examples: the emitted lexical form always round-trips
// to the observed Object, so a declaration the value contradicts is ignored for
// that triple rather than applied.
//
// The generators are written from the value grammar the declaration accepts,
// and they hug the boundaries the clause names — the whole/fractional boundary,
// the exact-integer bound where a float64 stops naming a unique integer, and
// the non-finite values that have no lexical form at all. A generator that
// merely strides a bound catches an off-by-one only probabilistically.

const propIntPred = "prop.declared.int"

// wholeFloats generates float64 values that ARE whole numbers, hugging the
// exact-integer bound on both sides: past 2^53 a float64 no longer names a
// unique integer, so the declaration must fall back to observation there.
// spec: predicate-contract / RDF export honors the declared datatype and never fabricates a value
var wholeFloats = rapid.OneOf(
	rapid.Just(0.0),
	rapid.Just(-0.0),
	rapid.Just(1.0),
	rapid.Just(-1.0),
	rapid.Just(float64(wholeFloatBound-1)),
	rapid.Just(float64(wholeFloatBound)),
	rapid.Just(float64(wholeFloatBound+2)), // first whole float past the bound
	rapid.Just(-float64(wholeFloatBound)),
	rapid.Just(-float64(wholeFloatBound+2)),
	rapid.Just(math.MaxFloat64),
	rapid.Custom(func(rt *rapid.T) float64 {
		return float64(rapid.Int64Range(-1<<40, 1<<40).Draw(rt, "whole"))
	}),
)

// fractionalFloats generates float64 values that are NOT whole, hugging the
// smallest departures from a whole number in both directions.
// spec: predicate-contract / RDF export honors the declared datatype and never fabricates a value
var fractionalFloats = rapid.OneOf(
	rapid.Just(0.5),
	rapid.Just(-0.5),
	rapid.Just(math.Nextafter(1, 2)),  // 1 + 1ulp
	rapid.Just(math.Nextafter(1, 0)),  // 1 - 1ulp
	rapid.Just(math.Nextafter(0, 1)),  // the smallest positive subnormal
	rapid.Just(math.Nextafter(-1, 0)), // -1 + 1ulp
	rapid.Float64Range(-1e12, 1e12).Filter(func(f float64) bool { return f != math.Trunc(f) }),
)

// TestPropDeclaredIntegerNeverFabricates asserts the spec clause directly: the
// emitted lexical form parses back to the observed value. It cannot be
// satisfied by recomputing the implementation's own algorithm — the oracle is
// strconv, parsing the emitted text, compared against the input.
//
// spec: predicate-contract / RDF export honors the declared datatype and never fabricates a value
func TestPropDeclaredIntegerNeverFabricates(t *testing.T) {
	defer vocabulary.SnapshotRegistry()()
	vocabulary.Register(propIntPred, vocabulary.WithDataType(vocabulary.DataTypeInt))
	opts := defaultOptions()

	rapid.Check(t, func(rt *rapid.T) {
		observed := rapid.OneOf(wholeFloats, fractionalFloats).Draw(rt, "observed")

		classified := classifyObject(message.Triple{
			Subject:   roundTripEntityID,
			Predicate: propIntPred,
			Object:    observed,
		}, &opts)

		if classified.kind != objectLiteral {
			rt.Fatalf("observed %v classified as kind %v, want a literal", observed, classified.kind)
		}

		switch classified.datatype {
		case xsdInteger:
			back, err := strconv.ParseInt(classified.lexical, 10, 64)
			if err != nil {
				rt.Fatalf("observed %v emitted %q as xsd:integer, which does not parse: %v",
					observed, classified.lexical, err)
			}
			if float64(back) != observed {
				rt.Fatalf("observed %v emitted %q as xsd:integer, which round-trips to %d",
					observed, classified.lexical, back)
			}
		case xsdDouble:
			back, err := strconv.ParseFloat(classified.lexical, 64)
			if err != nil {
				rt.Fatalf("observed %v emitted %q as xsd:double, which does not parse: %v",
					observed, classified.lexical, err)
			}
			if back != observed {
				rt.Fatalf("observed %v emitted %q as xsd:double, which round-trips to %v",
					observed, classified.lexical, back)
			}
		default:
			rt.Fatalf("observed %v emitted datatype %q, want xsd:integer or xsd:double",
				observed, classified.datatype)
		}
	})
}

// TestPropDeclaredIntegerRefusesNonFiniteValues pins the boundary the lexical
// grammar has no form for at all: NaN and the infinities are not serializable,
// and a declaration must not manufacture a lexical form for them.
//
// spec: predicate-contract / RDF export honors the declared datatype and never fabricates a value
func TestPropDeclaredIntegerRefusesNonFiniteValues(t *testing.T) {
	defer vocabulary.SnapshotRegistry()()
	vocabulary.Register(propIntPred, vocabulary.WithDataType(vocabulary.DataTypeInt))
	opts := defaultOptions()

	for _, observed := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		classified := classifyObject(message.Triple{
			Subject: roundTripEntityID, Predicate: propIntPred, Object: observed,
		}, &opts)
		if classified.kind != objectInvalid {
			t.Errorf("observed %v classified as kind %v with lexical %q; a non-finite value has no lexical form",
				observed, classified.kind, classified.lexical)
		}
	}
}

// TestPropDeclaredEntityIDNeverFabricatesAnIRI asserts that the entity-reference
// declaration promotes an object to an IRI only when the observed value IS a
// canonical entity ID, and that the emitted IRI carries every segment of it.
//
// spec: predicate-contract / RDF export honors the declared datatype and never fabricates a value
func TestPropDeclaredEntityIDNeverFabricatesAnIRI(t *testing.T) {
	defer vocabulary.SnapshotRegistry()()
	const pred = "prop.declared.entityid"
	vocabulary.Register(pred, vocabulary.WithDataType(vocabulary.DataTypeEntityID))
	opts := defaultOptions()

	segment := rapid.StringMatching(`[a-zA-Z0-9][a-zA-Z0-9_-]{0,12}`)

	rapid.Check(t, func(rt *rapid.T) {
		segments := make([]string, 6)
		for i := range segments {
			segments[i] = segment.Draw(rt, "segment")
		}
		// Draw both canonical six-part IDs and near-misses of every other arity,
		// so the property sees the accept AND the fall-back path.
		arity := rapid.IntRange(1, 6).Draw(rt, "arity")
		observed := strings.Join(segments[:arity], ".")

		classified := classifyObject(message.Triple{
			Subject: roundTripEntityID, Predicate: pred, Object: observed,
		}, &opts)

		if !message.IsValidEntityID(observed) {
			if classified.kind == objectResource && !isAbsoluteIRI(observed) {
				rt.Fatalf("observed %q is not a canonical entity ID but was promoted to IRI %q",
					observed, classified.iri)
			}
			return
		}

		if classified.kind != objectResource {
			rt.Fatalf("observed canonical entity ID %q classified as kind %v, want a resource",
				observed, classified.kind)
		}
		for _, part := range segments[:arity] {
			if !strings.Contains(classified.iri, "/"+part) {
				rt.Fatalf("IRI %q for %q drops segment %q", classified.iri, observed, part)
			}
		}
	})
}

// TestPropDeclaredDateTimeNeverFabricatesAnInstant asserts that the datetime
// declaration is applied only to values that ARE instants, and that the emitted
// lexical form parses back to the same instant.
//
// spec: predicate-contract / RDF export honors the declared datatype and never fabricates a value
func TestPropDeclaredDateTimeNeverFabricatesAnInstant(t *testing.T) {
	defer vocabulary.SnapshotRegistry()()
	const pred = "prop.declared.datetime"
	vocabulary.Register(pred, vocabulary.WithDataType(vocabulary.DataTypeDateTime))
	opts := defaultOptions()

	rapid.Check(t, func(rt *rapid.T) {
		unix := rapid.Int64Range(-62135596800, 253402300799).Draw(rt, "unix")
		instant := time.Unix(unix, 0).UTC()

		asString := rapid.Bool().Draw(rt, "asString")
		var object any = instant
		if asString {
			object = instant.Format(time.RFC3339)
		}

		classified := classifyObject(message.Triple{
			Subject: roundTripEntityID, Predicate: pred, Object: object,
		}, &opts)

		if classified.datatype != xsdDateTime {
			rt.Fatalf("observed %v (string=%v) emitted datatype %q, want xsd:dateTime",
				object, asString, classified.datatype)
		}
		back, err := time.Parse(time.RFC3339, classified.lexical)
		if err != nil {
			rt.Fatalf("emitted %q does not parse as RFC 3339: %v", classified.lexical, err)
		}
		if !back.Equal(instant) {
			rt.Fatalf("observed %v emitted %q, which round-trips to %v", instant, classified.lexical, back)
		}
	})
}

// TestPropNonInstantStringUnderDateTimeDeclarationFallsBack is the refusal half
// of the datetime property: the accept-path generator above never draws a
// non-instant, so nothing there could see the declaration being over-applied.
//
// spec: predicate-contract / RDF export honors the declared datatype and never fabricates a value
func TestPropNonInstantStringUnderDateTimeDeclarationFallsBack(t *testing.T) {
	defer vocabulary.SnapshotRegistry()()
	const pred = "prop.declared.datetime"
	vocabulary.Register(pred, vocabulary.WithDataType(vocabulary.DataTypeDateTime))
	opts := defaultOptions()

	for _, observed := range []string{
		"", "hovering", "2026-09-09", "2026-09-09 12:00:00", "12:00:00Z", "not a time",
	} {
		classified := classifyObject(message.Triple{
			Subject: roundTripEntityID, Predicate: pred, Object: observed,
		}, &opts)
		if classified.datatype == xsdDateTime {
			t.Errorf("observed %q was emitted as xsd:dateTime %q; it is not an instant",
				observed, classified.lexical)
		}
		if classified.lexical != observed {
			t.Errorf("observed %q was emitted as %q; observation renders what is there",
				observed, classified.lexical)
		}
	}
}
