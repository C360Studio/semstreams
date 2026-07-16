package export

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/message"
)

func TestWriteNTriples_BasicTriple(t *testing.T) {
	triples := []message.Triple{
		{
			Subject:   "acme.ops.robotics.gcs.drone.001",
			Predicate: "robotics.battery.level",
			Object:    85.5,
		},
	}

	var buf bytes.Buffer
	err := writeNTriples(&buf, triples, &options{baseIRI: "https://semstreams.semanticstream.ing", subjectIRIFn: defaultSubjectIRI})
	if err != nil {
		t.Fatal(err)
	}

	got := buf.String()
	if !strings.Contains(got, "<https://semstreams.semanticstream.ing/entities/acme/ops/robotics/gcs/drone/001>") {
		t.Errorf("expected subject IRI in output, got:\n%s", got)
	}
	if !strings.Contains(got, `"85.5"^^<http://www.w3.org/2001/XMLSchema#double>`) {
		t.Errorf("expected typed literal in output, got:\n%s", got)
	}
	if !strings.HasSuffix(strings.TrimSpace(got), ".") {
		t.Errorf("expected line to end with dot, got:\n%s", got)
	}
}

func TestWriteNTriples_StringLiteral(t *testing.T) {
	triples := []message.Triple{
		{
			Subject:   "acme.ops.robotics.gcs.drone.001",
			Predicate: "robotics.status.mode",
			Object:    "hovering",
		},
	}

	var buf bytes.Buffer
	opts := defaultOptions()
	err := writeNTriples(&buf, triples, &opts)
	if err != nil {
		t.Fatal(err)
	}

	got := buf.String()
	if !strings.Contains(got, `"hovering"`) {
		t.Errorf("expected string literal, got:\n%s", got)
	}
	// Plain strings should NOT have ^^xsd:string
	if strings.Contains(got, "XMLSchema#string") {
		t.Errorf("plain strings should not have explicit xsd:string datatype, got:\n%s", got)
	}
}

func TestWriteNTriples_BooleanLiteral(t *testing.T) {
	triples := []message.Triple{
		{
			Subject:   "acme.ops.robotics.gcs.drone.001",
			Predicate: "robotics.flight.armed",
			Object:    true,
		},
	}

	var buf bytes.Buffer
	opts := defaultOptions()
	err := writeNTriples(&buf, triples, &opts)
	if err != nil {
		t.Fatal(err)
	}

	got := buf.String()
	if !strings.Contains(got, `"true"^^<http://www.w3.org/2001/XMLSchema#boolean>`) {
		t.Errorf("expected typed boolean in N-Triples, got:\n%s", got)
	}
}

func TestWriteNTriples_IntegerLiteral(t *testing.T) {
	triples := []message.Triple{
		{
			Subject:   "acme.ops.robotics.gcs.drone.001",
			Predicate: "robotics.battery.cells",
			Object:    4,
		},
	}

	var buf bytes.Buffer
	opts := defaultOptions()
	err := writeNTriples(&buf, triples, &opts)
	if err != nil {
		t.Fatal(err)
	}

	got := buf.String()
	if !strings.Contains(got, `"4"^^<http://www.w3.org/2001/XMLSchema#integer>`) {
		t.Errorf("expected typed integer in N-Triples, got:\n%s", got)
	}
}

func TestWriteNTriples_EntityReference(t *testing.T) {
	triples := []message.Triple{
		{
			Subject:   "acme.ops.robotics.gcs.drone.001",
			Predicate: "graph.rel.contains",
			Object:    "acme.ops.robotics.gcs.battery.001",
		},
	}

	var buf bytes.Buffer
	opts := defaultOptions()
	err := writeNTriples(&buf, triples, &opts)
	if err != nil {
		t.Fatal(err)
	}

	got := buf.String()
	if !strings.Contains(got, "<https://semstreams.semanticstream.ing/entities/acme/ops/robotics/gcs/battery/001>") {
		t.Errorf("expected entity reference IRI in output, got:\n%s", got)
	}
	// Should not be quoted
	if strings.Contains(got, `"acme.ops.robotics.gcs.battery.001"`) {
		t.Errorf("entity reference should be an IRI, not a string literal, got:\n%s", got)
	}
}

func TestWriteNTriples_DateTimeLiteral(t *testing.T) {
	ts := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
	triples := []message.Triple{
		{
			Subject:   "acme.ops.robotics.gcs.drone.001",
			Predicate: "time.lifecycle.created",
			Object:    ts,
		},
	}

	var buf bytes.Buffer
	opts := defaultOptions()
	err := writeNTriples(&buf, triples, &opts)
	if err != nil {
		t.Fatal(err)
	}

	got := buf.String()
	if !strings.Contains(got, `"2024-01-15T10:30:00Z"^^<http://www.w3.org/2001/XMLSchema#dateTime>`) {
		t.Errorf("expected typed dateTime, got:\n%s", got)
	}
}

func TestWriteNTriples_SkipsInvalid(t *testing.T) {
	triples := []message.Triple{
		{Subject: "", Predicate: "robotics.battery.level", Object: 85.5},
		{Subject: "acme.ops.robotics.gcs.drone.001", Predicate: "", Object: 85.5}, // predicate-audit:invalid {"kind":"stored-predicate","value":"","reason":"empty"}
		{Subject: "acme.ops.robotics.gcs.drone.001", Predicate: "robotics.battery.level", Object: nil},
		{Subject: "acme.ops.robotics.gcs.drone.001", Predicate: "robotics.status.mode", Object: "ok"},
	}

	var buf bytes.Buffer
	opts := defaultOptions()
	err := writeNTriples(&buf, triples, &opts)
	if err != nil {
		t.Fatal(err)
	}

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 1 {
		t.Errorf("expected 1 valid line, got %d:\n%s", len(lines), buf.String())
	}
}

func TestWriteNTriples_MultipleTriples(t *testing.T) {
	triples := []message.Triple{
		{Subject: "acme.ops.robotics.gcs.drone.001", Predicate: "robotics.battery.level", Object: 85.5},
		{Subject: "acme.ops.robotics.gcs.drone.001", Predicate: "robotics.flight.armed", Object: true},
		{Subject: "acme.ops.robotics.gcs.drone.002", Predicate: "robotics.battery.level", Object: 50.0},
	}

	var buf bytes.Buffer
	opts := defaultOptions()
	err := writeNTriples(&buf, triples, &opts)
	if err != nil {
		t.Fatal(err)
	}

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 3 {
		t.Errorf("expected 3 lines, got %d:\n%s", len(lines), buf.String())
	}

	for i, line := range lines {
		if !strings.HasSuffix(line, " .") {
			t.Errorf("line %d should end with ' .': %s", i, line)
		}
	}
}

func TestWriteNTriples_SpecialCharactersInString(t *testing.T) {
	triples := []message.Triple{
		{
			Subject:   "acme.ops.robotics.gcs.drone.001",
			Predicate: "robotics.status.message",
			Object:    "line1\nline2\twith \"quotes\"",
		},
	}

	var buf bytes.Buffer
	opts := defaultOptions()
	err := writeNTriples(&buf, triples, &opts)
	if err != nil {
		t.Fatal(err)
	}

	got := buf.String()
	if !strings.Contains(got, `line1\nline2\twith \"quotes\"`) {
		t.Errorf("expected escaped special chars, got:\n%s", got)
	}
}
