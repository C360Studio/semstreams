package sensorml

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestUnmarshalProcess_PhysicalSystemFixture(t *testing.T) {
	data := loadFixture(t, "drone-001.sensorml.json")
	process, err := UnmarshalProcess(data)
	if err != nil {
		t.Fatalf("UnmarshalProcess: %v", err)
	}
	sys, ok := process.(*PhysicalSystem)
	if !ok {
		t.Fatalf("expected *PhysicalSystem, got %T", process)
	}

	if sys.ID != "drone-001" {
		t.Errorf("id: got %q, want drone-001", sys.ID)
	}
	if sys.Label == "" {
		t.Errorf("label should be non-empty")
	}
	if sys.Definition != "http://www.w3.org/ns/sosa/Platform" {
		t.Errorf("definition: got %q, want sosa:Platform IRI", sys.Definition)
	}
	if len(sys.Identifiers) != 2 {
		t.Errorf("identifiers: got %d, want 2", len(sys.Identifiers))
	}
	if len(sys.Capabilities) != 1 {
		t.Errorf("capabilities: got %d, want 1", len(sys.Capabilities))
	}
	if len(sys.Components) != 2 {
		t.Fatalf("components: got %d, want 2", len(sys.Components))
	}
	if len(sys.Connections) != 1 {
		t.Errorf("connections: got %d, want 1", len(sys.Connections))
	}

	first, ok := sys.Components[0].(*PhysicalComponent)
	if !ok {
		t.Fatalf("components[0]: expected *PhysicalComponent, got %T", sys.Components[0])
	}
	if first.ID != "battery-sensor" {
		t.Errorf("components[0].id: got %q, want battery-sensor", first.ID)
	}
	if first.Method == nil || first.Method.Href != "http://example.org/procedures/voltmeter" {
		t.Errorf("components[0].method: got %+v, want voltmeter procedure href", first.Method)
	}
}

// TestRoundTrip_PhysicalSystemFixture confirms that
// Unmarshal → Marshal → Unmarshal is stable. The second
// unmarshal must produce a value semantically equal to the
// first; we re-marshal the second and assert byte-equality
// against the first marshal (the canonical representation).
func TestRoundTrip_PhysicalSystemFixture(t *testing.T) {
	data := loadFixture(t, "drone-001.sensorml.json")

	first, err := UnmarshalProcess(data)
	if err != nil {
		t.Fatalf("first unmarshal: %v", err)
	}
	marshaled, err := json.Marshal(first)
	if err != nil {
		t.Fatalf("first marshal: %v", err)
	}
	second, err := UnmarshalProcess(marshaled)
	if err != nil {
		t.Fatalf("second unmarshal: %v\nfrom: %s", err, marshaled)
	}
	remarshaled, err := json.Marshal(second)
	if err != nil {
		t.Fatalf("second marshal: %v", err)
	}
	if string(remarshaled) != string(marshaled) {
		t.Errorf("round-trip differs:\n  first:  %s\n  second: %s", marshaled, remarshaled)
	}
}

func TestUnmarshalProcess_RejectsBadInput(t *testing.T) {
	cases := []struct {
		name string
		json string
	}{
		{"empty object", `{}`},
		{"missing type", `{"id": "foo"}`},
		{"unknown type", `{"type": "Specimen", "id": "s1"}`},
		{"malformed json", `{"type":`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := UnmarshalProcess([]byte(c.json))
			if err == nil {
				t.Errorf("expected error for %s, got nil", c.name)
			}
		})
	}
}

func TestUnmarshalProcess_SimpleProcessAndAggregate(t *testing.T) {
	const raw = `{
        "type": "AggregateProcess",
        "id": "ndvi-pipeline",
        "components": [
            {"type": "SimpleProcess", "id": "downsample", "method": {"href": "http://example.org/methods/downsample"}},
            {"type": "SimpleProcess", "id": "compute-ndvi", "method": {"href": "http://example.org/methods/ndvi"}}
        ]
    }`
	process, err := UnmarshalProcess([]byte(raw))
	if err != nil {
		t.Fatalf("UnmarshalProcess: %v", err)
	}
	agg, ok := process.(*AggregateProcess)
	if !ok {
		t.Fatalf("expected *AggregateProcess, got %T", process)
	}
	if len(agg.Components) != 2 {
		t.Fatalf("components: got %d, want 2", len(agg.Components))
	}
	for i, child := range agg.Components {
		if _, ok := child.(*SimpleProcess); !ok {
			t.Errorf("components[%d]: expected *SimpleProcess, got %T", i, child)
		}
	}
}

func loadFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return data
}
