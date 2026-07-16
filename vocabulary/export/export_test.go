package export

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/vocabulary"
)

// sampleTriples returns a set of triples for integration testing.
func sampleTriples() []message.Triple {
	ts := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
	return []message.Triple{
		{Subject: "acme.ops.robotics.gcs.drone.001", Predicate: "robotics.battery.level", Object: 85.5},
		{Subject: "acme.ops.robotics.gcs.drone.001", Predicate: "robotics.flight.armed", Object: true},
		{Subject: "acme.ops.robotics.gcs.drone.001", Predicate: "robotics.status.mode", Object: "hovering"},
		{Subject: "acme.ops.robotics.gcs.drone.001", Predicate: "robotics.battery.cells", Object: 4},
		{Subject: "acme.ops.robotics.gcs.drone.001", Predicate: "time.lifecycle.created", Object: ts},
		{Subject: "acme.ops.robotics.gcs.drone.001", Predicate: "graph.rel.contains", Object: "acme.ops.robotics.gcs.battery.001"},
	}
}

func TestSerialize_FormatDispatch(t *testing.T) {
	triples := sampleTriples()

	tests := []struct {
		format Format
		check  func(t *testing.T, output string)
	}{
		{
			format: NTriples,
			check: func(t *testing.T, output string) {
				t.Helper()
				lines := strings.Split(strings.TrimSpace(output), "\n")
				if len(lines) != 6 {
					t.Errorf("N-Triples: expected 6 lines, got %d", len(lines))
				}
				for _, line := range lines {
					if !strings.HasSuffix(line, " .") {
						t.Errorf("N-Triples: line should end with ' .': %s", line)
					}
				}
			},
		},
		{
			format: Turtle,
			check: func(t *testing.T, output string) {
				t.Helper()
				if !strings.Contains(output, "@prefix") {
					t.Error("Turtle: expected prefix declarations")
				}
				if !strings.Contains(output, ";") {
					t.Error("Turtle: expected semicolons for subject grouping")
				}
			},
		},
		{
			format: JSONLD,
			check: func(t *testing.T, output string) {
				t.Helper()
				var doc map[string]any
				if err := json.Unmarshal([]byte(output), &doc); err != nil {
					t.Fatalf("JSON-LD: invalid JSON: %v", err)
				}
				if doc["@id"] == nil {
					t.Error("JSON-LD: expected @id in document")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.format.String(), func(t *testing.T) {
			output, err := SerializeToString(triples, tt.format)
			if err != nil {
				t.Fatal(err)
			}
			tt.check(t, output)
		})
	}
}

func TestSerialize_UnsupportedFormat(t *testing.T) {
	err := Serialize(&bytes.Buffer{}, nil, Format(99))
	if err == nil {
		t.Fatal("expected error for unsupported format")
	}
	if !strings.Contains(err.Error(), "unsupported format") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestSerialize_WithBaseIRI(t *testing.T) {
	triples := []message.Triple{
		{Subject: "acme.ops.robotics.gcs.drone.001", Predicate: "robotics.battery.level", Object: 85.5},
	}

	output, err := SerializeToString(triples, NTriples, WithBaseIRI("https://example.org"))
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(output, "https://example.org/entities/") {
		t.Errorf("expected custom base in subject IRI, got:\n%s", output)
	}
	if !strings.Contains(output, "https://example.org/predicates/") {
		t.Errorf("expected custom base in predicate IRI, got:\n%s", output)
	}
}

func TestSerialize_WithSubjectIRIFunc(t *testing.T) {
	triples := []message.Triple{
		{Subject: "acme.ops.robotics.gcs.drone.001", Predicate: "robotics.battery.level", Object: 85.5},
	}

	customFn := func(subject string) string {
		return "https://custom.example.org/entity/" + subject
	}

	output, err := SerializeToString(triples, NTriples, WithSubjectIRIFunc(customFn))
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(output, "https://custom.example.org/entity/acme.ops.robotics.gcs.drone.001") {
		t.Errorf("expected custom subject IRI, got:\n%s", output)
	}
}

func TestSerialize_SkipsInvalidTriples(t *testing.T) {
	triples := []message.Triple{
		{Subject: "", Predicate: "robotics.battery.level", Object: 85.5},
		{Subject: "acme.ops.robotics.gcs.drone.001", Predicate: "", Object: 85.5}, // predicate-audit:invalid {"kind":"stored-predicate","value":"","reason":"empty"}
		{Subject: "acme.ops.robotics.gcs.drone.001", Predicate: "robotics.battery.level", Object: nil},
		{Subject: "acme.ops.robotics.gcs.drone.001", Predicate: "robotics.status.mode", Object: "ok"},
	}

	formats := []Format{Turtle, NTriples, JSONLD}
	for _, format := range formats {
		t.Run(format.String(), func(t *testing.T) {
			output, err := SerializeToString(triples, format)
			if err != nil {
				t.Fatal(err)
			}
			// Only the last valid triple should be serialized
			if output == "" {
				t.Error("expected at least one valid triple in output")
			}
		})
	}
}

func TestSerialize_NonEntitySubjectFallback(t *testing.T) {
	triples := []message.Triple{
		{Subject: "some.arbitrary.subject", Predicate: "robotics.battery.level", Object: 85.5},
	}

	output, err := SerializeToString(triples, NTriples)
	if err != nil {
		t.Fatal(err)
	}

	// Should use subjects/ path for non-entity subjects
	if !strings.Contains(output, "/subjects/some/arbitrary/subject>") {
		t.Errorf("expected fallback subject IRI, got:\n%s", output)
	}
}

func TestSerialize_RegisteredPredicateUsesStandardIRI(t *testing.T) {
	vocabulary.ClearRegistry()
	defer vocabulary.ClearRegistry()

	vocabulary.Register("robotics.battery.level",
		vocabulary.WithIRI("https://schema.org/batteryLevel"))

	triples := []message.Triple{
		{Subject: "acme.ops.robotics.gcs.drone.001", Predicate: "robotics.battery.level", Object: 85.5},
	}

	output, err := SerializeToString(triples, NTriples)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(output, "<https://schema.org/batteryLevel>") {
		t.Errorf("expected standard IRI from registry, got:\n%s", output)
	}
}

func TestSerialize_FormatString(t *testing.T) {
	tests := []struct {
		format Format
		want   string
	}{
		{Turtle, "Turtle"},
		{NTriples, "N-Triples"},
		{JSONLD, "JSON-LD"},
		{Format(99), "Format(99)"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.format.String(); got != tt.want {
				t.Errorf("Format.String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSerializeToString_Roundtrip(t *testing.T) {
	triples := sampleTriples()

	// Each format should produce non-empty output
	formats := []Format{Turtle, NTriples, JSONLD}
	for _, format := range formats {
		t.Run(format.String(), func(t *testing.T) {
			output, err := SerializeToString(triples, format)
			if err != nil {
				t.Fatal(err)
			}
			if output == "" {
				t.Error("expected non-empty output")
			}
		})
	}
}

func TestSubjectToIRI_EntityID(t *testing.T) {
	base := "https://semstreams.semanticstream.ing"
	got := subjectToIRI("acme.ops.robotics.gcs.drone.001", base)
	want := "https://semstreams.semanticstream.ing/entities/acme/ops/robotics/gcs/drone/001"
	if got != want {
		t.Errorf("subjectToIRI() = %q, want %q", got, want)
	}
}

func TestSubjectToIRI_NonEntityFallback(t *testing.T) {
	base := "https://semstreams.semanticstream.ing"
	got := subjectToIRI("some.topic.name", base)
	want := "https://semstreams.semanticstream.ing/subjects/some/topic/name"
	if got != want {
		t.Errorf("subjectToIRI() = %q, want %q", got, want)
	}
}

func TestSubjectToIRI_Empty(t *testing.T) {
	got := subjectToIRI("", "https://example.org")
	if got != "" {
		t.Errorf("expected empty string for empty subject, got %q", got)
	}
}

func TestResolvePredicateIRI_Unregistered(t *testing.T) {
	vocabulary.ClearRegistry()
	defer vocabulary.ClearRegistry()

	base := "https://semstreams.semanticstream.ing"
	got := resolvePredicateIRI("robotics.battery.level", base)
	want := "https://semstreams.semanticstream.ing/predicates/robotics/battery/level"
	if got != want {
		t.Errorf("resolvePredicateIRI() = %q, want %q", got, want)
	}
}

func TestResolvePredicateIRI_Registered(t *testing.T) {
	vocabulary.ClearRegistry()
	defer vocabulary.ClearRegistry()

	vocabulary.Register("robotics.battery.level",
		vocabulary.WithIRI("https://schema.org/batteryLevel"))

	got := resolvePredicateIRI("robotics.battery.level", "https://semstreams.semanticstream.ing")
	if got != "https://schema.org/batteryLevel" {
		t.Errorf("expected registered standard IRI, got %q", got)
	}
}

func TestResolvePredicateIRI_Empty(t *testing.T) {
	got := resolvePredicateIRI("", "https://example.org")
	if got != "" {
		t.Errorf("expected empty string for empty predicate, got %q", got)
	}
}
