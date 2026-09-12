package export

import (
	"math"
	"testing"
	"time"

	"github.com/c360studio/semstreams/message"
)

func TestClassifyObject_StringLiteral(t *testing.T) {
	opts := defaultOptions()
	triple := message.Triple{
		Subject:   "acme.ops.robotics.gcs.drone.001",
		Predicate: "robotics.status.mode",
		Object:    "hovering",
	}

	c := classifyObject(triple, &opts)
	if c.kind != objectLiteral {
		t.Fatalf("expected objectLiteral, got %v", c.kind)
	}
	if c.lexical != "hovering" {
		t.Errorf("expected lexical %q, got %q", "hovering", c.lexical)
	}
	if c.datatype != "" {
		t.Errorf("expected empty datatype for plain string, got %q", c.datatype)
	}
}

func TestClassifyObject_EntityReference(t *testing.T) {
	opts := defaultOptions()
	triple := message.Triple{
		Subject:   "acme.ops.robotics.gcs.drone.001",
		Predicate: "graph.rel.contains",
		Object:    "acme.ops.robotics.gcs.battery.001",
	}

	c := classifyObject(triple, &opts)
	if c.kind != objectResource {
		t.Fatalf("expected objectResource, got %v", c.kind)
	}
	want := "https://semstreams.semanticstream.ing/entities/acme/ops/robotics/gcs/battery/001"
	if c.iri != want {
		t.Errorf("expected IRI %q, got %q", want, c.iri)
	}
}

func TestClassifyObject_Bool(t *testing.T) {
	opts := defaultOptions()
	triple := message.Triple{Object: true}

	c := classifyObject(triple, &opts)
	if c.kind != objectLiteral {
		t.Fatalf("expected objectLiteral, got %v", c.kind)
	}
	if c.lexical != "true" {
		t.Errorf("expected lexical %q, got %q", "true", c.lexical)
	}
	if c.datatype != xsdBoolean {
		t.Errorf("expected datatype %q, got %q", xsdBoolean, c.datatype)
	}
	if !c.bare {
		t.Error("expected bare=true for boolean")
	}
}

func TestClassifyObject_Int(t *testing.T) {
	opts := defaultOptions()

	tests := []struct {
		name string
		obj  any
		want string
	}{
		{"int", 42, "42"},
		{"int64", int64(999), "999"},
		{"int32", int32(-10), "-10"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			triple := message.Triple{Object: tt.obj}
			c := classifyObject(triple, &opts)
			if c.kind != objectLiteral {
				t.Fatalf("expected objectLiteral, got %v", c.kind)
			}
			if c.lexical != tt.want {
				t.Errorf("expected lexical %q, got %q", tt.want, c.lexical)
			}
			if c.datatype != xsdInteger {
				t.Errorf("expected datatype %q, got %q", xsdInteger, c.datatype)
			}
			if !c.bare {
				t.Error("expected bare=true for integer")
			}
		})
	}
}

func TestClassifyObject_Float(t *testing.T) {
	opts := defaultOptions()

	tests := []struct {
		name string
		obj  any
		want string
	}{
		{"float64", 85.5, "85.5"},
		{"float32", float32(1.5), "1.5"},
		{"float64_whole", float64(100), "100.0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			triple := message.Triple{Object: tt.obj}
			c := classifyObject(triple, &opts)
			if c.kind != objectLiteral {
				t.Fatalf("expected objectLiteral, got %v", c.kind)
			}
			if c.lexical != tt.want {
				t.Errorf("expected lexical %q, got %q", tt.want, c.lexical)
			}
			if c.datatype != xsdDouble {
				t.Errorf("expected datatype %q, got %q", xsdDouble, c.datatype)
			}
			if c.bare {
				t.Error("expected bare=false for float")
			}
		})
	}
}

func TestClassifyObject_Time(t *testing.T) {
	opts := defaultOptions()
	ts := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
	triple := message.Triple{Object: ts}

	c := classifyObject(triple, &opts)
	if c.kind != objectLiteral {
		t.Fatalf("expected objectLiteral, got %v", c.kind)
	}
	if c.lexical != "2024-01-15T10:30:00Z" {
		t.Errorf("expected lexical %q, got %q", "2024-01-15T10:30:00Z", c.lexical)
	}
	if c.datatype != xsdDateTime {
		t.Errorf("expected datatype %q, got %q", xsdDateTime, c.datatype)
	}
}

func TestClassifyObject_NaNIsInvalid(t *testing.T) {
	opts := defaultOptions()
	triple := message.Triple{Object: math.NaN()}

	c := classifyObject(triple, &opts)
	if c.kind != objectInvalid {
		t.Fatalf("expected objectInvalid for NaN, got %v", c.kind)
	}
}

func TestClassifyObject_InfIsInvalid(t *testing.T) {
	opts := defaultOptions()
	triple := message.Triple{Object: math.Inf(1)}

	c := classifyObject(triple, &opts)
	if c.kind != objectInvalid {
		t.Fatalf("expected objectInvalid for +Inf, got %v", c.kind)
	}
}

func TestClassifyObject_NilIsInvalid(t *testing.T) {
	opts := defaultOptions()
	triple := message.Triple{Object: nil}

	c := classifyObject(triple, &opts)
	if c.kind != objectInvalid {
		t.Fatalf("expected objectInvalid for nil, got %v", c.kind)
	}
}

func TestClassifyObject_ExplicitDatatype(t *testing.T) {
	opts := defaultOptions()
	triple := message.Triple{
		Object:   "85.5",
		Datatype: "xsd:float",
	}

	c := classifyObject(triple, &opts)
	if c.kind != objectLiteral {
		t.Fatalf("expected objectLiteral, got %v", c.kind)
	}
	if c.lexical != "85.5" {
		t.Errorf("expected lexical %q, got %q", "85.5", c.lexical)
	}
	if c.datatype != "http://www.w3.org/2001/XMLSchema#float" {
		t.Errorf("expected expanded datatype, got %q", c.datatype)
	}
}

func TestClassifyObject_ExplicitDatatypeOverridesEntityDetection(t *testing.T) {
	opts := defaultOptions()
	triple := message.Triple{
		Object:   "acme.ops.robotics.gcs.drone.001",
		Datatype: "xsd:string",
	}

	c := classifyObject(triple, &opts)
	if c.kind != objectLiteral {
		t.Fatalf("explicit Datatype should produce literal, got %v", c.kind)
	}
	if c.lexical != "acme.ops.robotics.gcs.drone.001" {
		t.Errorf("expected lexical %q, got %q", "acme.ops.robotics.gcs.drone.001", c.lexical)
	}
}

func TestEscapeTurtleString(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"hello", "hello"},
		{`say "hello"`, `say \"hello\"`},
		{"line1\nline2", `line1\nline2`},
		{"tab\there", `tab\there`},
		{`back\slash`, `back\\slash`},
		{"return\rhere", `return\rhere`},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := escapeTurtleString(tt.input)
			if got != tt.want {
				t.Errorf("escapeTurtleString(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestExpandDatatypePrefix(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"xsd:string", "http://www.w3.org/2001/XMLSchema#string"},
		{"xsd:float", "http://www.w3.org/2001/XMLSchema#float"},
		{"xsd:dateTime", "http://www.w3.org/2001/XMLSchema#dateTime"},
		{"rdf:JSON", "http://www.w3.org/1999/02/22-rdf-syntax-ns#JSON"},
		{"http://example.org/type", "http://example.org/type"},
		// Residual, deliberately pinned: only the two prefixes this repository
		// writes onto triples are expanded (gh#1267 task 5.3). Any other prefix
		// still reaches the output as a relative reference, which is not valid
		// RDF. Pinned so the behaviour is a known residual rather than a
		// surprise; the message.Triple.Datatype doc comment no longer offers
		// "geo:point" as an example (gh#1267 task 10.7), and gh#1272 tracks the
		// same mechanism reached through the framework's own marker.
		{"geo:point", "geo:point"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := expandDatatypePrefix(tt.input)
			if got != tt.want {
				t.Errorf("expandDatatypePrefix(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestFormatFloat(t *testing.T) {
	tests := []struct {
		input float64
		want  string
	}{
		{85.5, "85.5"},
		{100.0, "100.0"},
		{0.001, "0.001"},
		{-42.0, "-42.0"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := formatFloat(tt.input)
			if got != tt.want {
				t.Errorf("formatFloat(%v) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
