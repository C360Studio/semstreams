package iotsensor

import (
	"encoding/json"
	"strings"
	"testing"
)

// deploymentAuthorityPrefix is positions 1-2 of every entity this processor
// mints when the composition root's platform.org / platform.id are the
// testDependencies() values. ADR-102 d2: the authority is the composition
// root's own identity field and nothing else.
const deploymentAuthorityPrefix = "c360.semstreams-e2e-structural."

// TestComponentMintsUnderDeploymentAuthority drives the production factory —
// NewComponent(rawConfig, deps) — and asserts the entity IDs the wired
// processor mints carry deps.Platform in positions 1-2. It fails if minting
// falls back to any other source (an operator config key, a default, a
// literal, or a wire field).
func TestComponentMintsUnderDeploymentAuthority(t *testing.T) {
	encoded, err := json.Marshal(DefaultConfig())
	if err != nil {
		t.Fatalf("marshal default config: %v", err)
	}
	discoverable, err := NewComponent(encoded, testDependencies())
	if err != nil {
		t.Fatalf("NewComponent: %v", err)
	}
	comp, ok := discoverable.(*Component)
	if !ok {
		t.Fatalf("NewComponent returned %T, want *Component", discoverable)
	}

	reading, err := comp.processor.Process(map[string]any{
		"device_id": "temp-sensor-001",
		"type":      "temperature",
		"reading":   36.5,
		"unit":      "fahrenheit",
		"location":  "cold-storage-1",
	})
	if err != nil {
		t.Fatalf("Process: %v", err)
	}

	if !strings.HasPrefix(reading.EntityID(), deploymentAuthorityPrefix) {
		t.Errorf("sensor entity ID %q does not mint under the deployment authority %q",
			reading.EntityID(), deploymentAuthorityPrefix)
	}
	if !strings.HasPrefix(reading.ZoneEntityID, deploymentAuthorityPrefix) {
		t.Errorf("zone entity ID %q does not mint under the deployment authority %q",
			reading.ZoneEntityID, deploymentAuthorityPrefix)
	}

	// The identity must survive the wire: graph-ingest reconstructs the
	// payload from JSON and calls EntityID() on the decoded value.
	wire, err := json.Marshal(reading)
	if err != nil {
		t.Fatalf("marshal reading: %v", err)
	}
	decoded := &SensorReading{}
	if err := json.Unmarshal(wire, decoded); err != nil {
		t.Fatalf("unmarshal reading: %v", err)
	}
	if decoded.EntityID() != reading.EntityID() {
		t.Errorf("entity ID did not survive the wire: got %q, want %q", decoded.EntityID(), reading.EntityID())
	}
}
