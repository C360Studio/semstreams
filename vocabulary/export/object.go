package export

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/vocabulary"
)

// XSD datatype IRIs
const (
	xsdString   = "http://www.w3.org/2001/XMLSchema#string"
	xsdInteger  = "http://www.w3.org/2001/XMLSchema#integer"
	xsdDouble   = "http://www.w3.org/2001/XMLSchema#double"
	xsdBoolean  = "http://www.w3.org/2001/XMLSchema#boolean"
	xsdDateTime = "http://www.w3.org/2001/XMLSchema#dateTime"
)

// RDF namespace and the one datatype this package renders from it.
const (
	rdfNamespace = "http://www.w3.org/1999/02/22-rdf-syntax-ns#"
	rdfJSON      = rdfNamespace + "JSON"
)

// wholeFloatBound is the largest magnitude at which a float64 still names a
// unique integer. Beyond it the conversion to int64 is not a rendering of the
// observed value but an invention, so a declared integer falls back to
// observation rather than fabricating a lexical form.
const wholeFloatBound = 1 << 53

// The RDF rendering of each canonical declared datatype.
// This table is the ENTIRE semantic-web surface of the declaration vocabulary
// (ADR-107): the registry declares pragmatic values, and the serializer — and
// only the serializer — knows what they mean in RDF.
//
//	vocabulary.DataTypeString   xsd:string, omitted from output exactly as today
//	vocabulary.DataTypeInt      xsd:integer
//	vocabulary.DataTypeFloat    xsd:double
//	vocabulary.DataTypeBool     xsd:boolean
//	vocabulary.DataTypeDateTime xsd:dateTime
//	vocabulary.DataTypeJSON     rdf:JSON
//	vocabulary.DataTypeEntityID an IRI resource, never a literal
//
// The table is prose rather than a map because each row also decides whether
// the observed value can carry the declaration at all; classifyByDeclaredDataType
// is where that lives.

// objectKind classifies how an object value should be serialized.
type objectKind int

const (
	objectLiteral  objectKind = iota // Quoted literal with optional datatype
	objectResource                   // IRI reference (entity or external)
	objectInvalid                    // Cannot be serialized
)

// classifiedObject holds the result of classifying a Triple's Object.
type classifiedObject struct {
	kind     objectKind
	iri      string // For objectResource: the resolved IRI
	lexical  string // For objectLiteral: the lexical form (unescaped)
	datatype string // For objectLiteral: XSD datatype IRI (empty for plain string)
	bare     bool   // True if Turtle can emit without quotes (int, bool)
}

// classifyObject inspects a Triple's Object, its Datatype hint, and the
// datatype its predicate declares in the vocabulary registry, and returns a
// classified representation suitable for serialization.
//
// Classification is a total order, most specific first (gh#1267):
//
//  1. the triple's own Datatype hint — a statement about THIS value;
//  2. the predicate's declared DataType — a statement about the PREDICATE,
//     and the only place a fact the JSON round trip erased can come from;
//  3. observation of the Go type — what the serializer can see for itself.
//
// Observation is strictly the fallback, never a competing authority — but a
// declaration the observed value cannot carry is ignored for that triple
// rather than applied. Export is a serializer: it renders what is there, and
// emitting a predicted type over a contradicting observation would be lying
// about data it can see.
func classifyObject(t message.Triple, opts *options) classifiedObject {
	if t.Object == nil {
		return classifiedObject{kind: objectInvalid}
	}

	// 1. The triple's own datatype hint overrides everything.
	if t.Datatype != "" {
		return classifyWithExplicitDatatype(t, opts)
	}

	// 2. The predicate's declared datatype, when the observed value can carry it.
	if meta := vocabulary.GetPredicateMetadata(t.Predicate); meta != nil && meta.DataType != "" {
		if classified, ok := classifyByDeclaredDataType(meta.DataType, t.Object, opts); ok {
			return classified
		}
	}

	// 3. Observation.
	return classifyByGoType(t.Object, opts)
}

// classifyWithExplicitDatatype handles triples where Datatype is explicitly set.
// When the caller sets Datatype, it is respected over the predicate's
// declaration and over the Go type — even for strings that look like entity IDs.
//
// The one datatype that is not a literal marker is the framework's own
// entity-reference marker: message.EntityReferenceDatatype is "@id", a
// relative reference, so emitting it as a literal's datatype produced
// `"acme.ops.gcs.robotics.drone.001"^^<@id>` — invalid RDF for the only
// per-triple marker the authoritative persistence seam validates (gh#1272).
// It denotes an IRI node, and it is serialized as one.
//
// A marked object that is NOT a canonical entity ID cannot be rendered as an
// IRI, so it falls through to observation rather than emitting a non-IRI
// datatype. That state is refused at graph.MarshalEntityState, so it cannot
// arrive from ENTITY_STATES; the fallback exists because Serialize accepts
// arbitrary triples.
func classifyWithExplicitDatatype(t message.Triple, opts *options) classifiedObject {
	if t.Datatype == message.EntityReferenceDatatype {
		if s, ok := t.Object.(string); ok && message.IsValidEntityID(s) {
			return classifiedObject{kind: objectResource, iri: resolveSubjectIRI(s, opts)}
		}
		return classifyByGoType(t.Object, opts)
	}

	return classifiedObject{
		kind:     objectLiteral,
		lexical:  fmt.Sprintf("%v", t.Object),
		datatype: expandDatatypePrefix(t.Datatype),
	}
}

// classifyByDeclaredDataType renders an object according to the datatype its
// predicate declares. It reports false when the declaration says nothing the
// serializer can act on for this value — either because the declared value is
// not one the exporter renders, or because the observed value cannot carry it
// — and the caller then falls back to observation.
//
// vocabulary.DataTypeString is deliberately absent. Its RDF rendering is
// xsd:string, which this package already omits from output, so the
// declaration confirms observation rather than overriding it. Forcing a plain
// literal instead would flip seven framework predicates that declare "string"
// and carry entity IDs — hierarchy.{domain,system,type}.{member,contains} and
// hierarchy.type.sibling (vocabulary/hierarchy.go), whose StandardIRIs are
// skos:broader/narrower/related — from IRI objects to string literals.
func classifyByDeclaredDataType(declared string, obj any, opts *options) (classifiedObject, bool) {
	switch declared {
	case vocabulary.DataTypeEntityID:
		if s, ok := obj.(string); ok && message.IsValidEntityID(s) {
			return classifiedObject{kind: objectResource, iri: resolveSubjectIRI(s, opts)}, true
		}
	case vocabulary.DataTypeInt:
		if lexical, ok := integerLexical(obj); ok {
			return classifiedObject{kind: objectLiteral, lexical: lexical, datatype: xsdInteger, bare: true}, true
		}
	case vocabulary.DataTypeFloat:
		if f, ok := floatValue(obj); ok {
			if classified := classifyFloat(f); classified.kind == objectLiteral {
				return classified, true
			}
		}
	case vocabulary.DataTypeBool:
		if b, ok := obj.(bool); ok {
			return classifiedObject{
				kind: objectLiteral, lexical: fmt.Sprintf("%t", b), datatype: xsdBoolean, bare: true,
			}, true
		}
	case vocabulary.DataTypeDateTime:
		if lexical, ok := dateTimeLexical(obj); ok {
			return classifiedObject{kind: objectLiteral, lexical: lexical, datatype: xsdDateTime}, true
		}
	case vocabulary.DataTypeJSON:
		if s, ok := obj.(string); ok {
			return classifiedObject{kind: objectLiteral, lexical: s, datatype: rdfJSON}, true
		}
	}
	return classifiedObject{}, false
}

// integerLexical renders obj as an xsd:integer lexical form, reporting false
// when the observed value has no exact integer rendering. A float64 with a
// fractional part, a NaN, an infinity, or a magnitude past the exact-integer
// bound all fall back to observation: the declaration is a statement about the
// predicate, not a licence to round, truncate, or invent.
func integerLexical(obj any) (string, bool) {
	switch v := obj.(type) {
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return fmt.Sprintf("%d", v), true
	case float32:
		return wholeFloatLexical(float64(v))
	case float64:
		return wholeFloatLexical(v)
	default:
		return "", false
	}
}

func wholeFloatLexical(f float64) (string, bool) {
	if math.IsNaN(f) || math.IsInf(f, 0) || f != math.Trunc(f) || math.Abs(f) > wholeFloatBound {
		return "", false
	}
	return strconv.FormatInt(int64(f), 10), true
}

// floatValue reports obj as a float64 when it is a Go numeric value.
func floatValue(obj any) (float64, bool) {
	switch v := obj.(type) {
	case int:
		return float64(v), true
	case int8:
		return float64(v), true
	case int16:
		return float64(v), true
	case int32:
		return float64(v), true
	case int64:
		return float64(v), true
	case uint:
		return float64(v), true
	case uint8:
		return float64(v), true
	case uint16:
		return float64(v), true
	case uint32:
		return float64(v), true
	case uint64:
		return float64(v), true
	case float32:
		return float64(v), true
	case float64:
		return v, true
	default:
		return 0, false
	}
}

// dateTimeLexical renders obj as an xsd:dateTime lexical form. A declared
// instant travels either as a time.Time or, once it has been through the
// authoritative JSON round trip, as an RFC 3339 string; a string that is not
// an RFC 3339 instant falls back to observation.
func dateTimeLexical(obj any) (string, bool) {
	switch v := obj.(type) {
	case time.Time:
		return v.Format(time.RFC3339), true
	case string:
		parsed, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return "", false
		}
		return parsed.Format(time.RFC3339), true
	default:
		return "", false
	}
}

// classifyByGoType infers the serialization from the Go type of the object.
func classifyByGoType(obj any, opts *options) classifiedObject {
	switch v := obj.(type) {
	case string:
		return classifyString(v, opts)
	case bool:
		return classifiedObject{
			kind:     objectLiteral,
			lexical:  fmt.Sprintf("%t", v),
			datatype: xsdBoolean,
			bare:     true,
		}
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return integerLiteral(v)
	case float32:
		return classifyFloat(float64(v))
	case float64:
		return classifyFloat(v)
	case time.Time:
		return classifiedObject{
			kind:     objectLiteral,
			lexical:  v.Format(time.RFC3339),
			datatype: xsdDateTime,
		}
	default:
		return classifiedObject{
			kind:    objectLiteral,
			lexical: fmt.Sprintf("%v", v),
		}
	}
}

func integerLiteral(v any) classifiedObject {
	return classifiedObject{
		kind:     objectLiteral,
		lexical:  fmt.Sprintf("%d", v),
		datatype: xsdInteger,
		bare:     true,
	}
}

func classifyFloat(f float64) classifiedObject {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return classifiedObject{kind: objectInvalid}
	}
	return classifiedObject{
		kind:     objectLiteral,
		lexical:  formatFloat(f),
		datatype: xsdDouble,
	}
}

// classifyString determines if a string is an entity reference, an absolute
// IRI, or a literal.
//
// The absolute-IRI branch is gh#1142: message.IsValidEntityID is false for
// anything carrying a scheme, so an IRI object fell to the literal branch BY
// CONSTRUCTION, and `rdf:type "http://schema.org/Thing"` is not valid RDF for
// a consumer that expects a class. The two branches cannot both match — a
// canonical 6-part entity ID carries no scheme, and a scheme's ":" is not a
// legal entity-ID character.
func classifyString(s string, opts *options) classifiedObject {
	if message.IsValidEntityID(s) {
		return classifiedObject{
			kind: objectResource,
			iri:  resolveSubjectIRI(s, opts),
		}
	}
	if isAbsoluteIRI(s) {
		return classifiedObject{
			kind: objectResource,
			iri:  s,
		}
	}
	return classifiedObject{
		kind:    objectLiteral,
		lexical: s,
		// xsd:string is the default and omitted in output
	}
}

// isAbsoluteIRI reports whether s carries one of the schemes this serializer
// emits as an IRI node rather than as a literal.
func isAbsoluteIRI(s string) bool {
	return strings.HasPrefix(s, "http://") ||
		strings.HasPrefix(s, "https://") ||
		strings.HasPrefix(s, "urn:")
}

// formatFloat renders a float64 for RDF output.
// It avoids scientific notation for common values and ensures a decimal point.
func formatFloat(f float64) string {
	s := fmt.Sprintf("%g", f)
	// Ensure it has a decimal point for clarity
	if !strings.Contains(s, ".") && !strings.Contains(s, "e") && !strings.Contains(s, "E") {
		s += ".0"
	}
	return s
}

// escapeTurtleString escapes a string for Turtle/N-Triples string literals.
func escapeTurtleString(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// expandDatatypePrefix expands the xsd: and rdf: prefixed datatypes this
// repository writes onto triples into full IRIs. A prefix left unexpanded is
// emitted as a relative reference, which is not a datatype IRI: the agentic
// vocabulary's own "rdf:JSON" marker (vocabulary/agentic/predicates.go,
// written at processor/agentic-tools/write_todos.go) produced `^^<rdf:JSON>`
// with no rdf: prefix declaration to bind it.
func expandDatatypePrefix(dt string) string {
	if strings.HasPrefix(dt, "xsd:") {
		return "http://www.w3.org/2001/XMLSchema#" + dt[4:]
	}
	if strings.HasPrefix(dt, "rdf:") {
		return rdfNamespace + dt[4:]
	}
	// Already a full IRI or other prefix
	if strings.Contains(dt, "://") {
		return dt
	}
	// Unknown prefix — return as-is for now
	return dt
}
