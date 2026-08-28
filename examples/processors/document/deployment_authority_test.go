package document

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
// NewComponent(rawConfig, deps) — and asserts every payload shape this
// processor produces mints under deps.Platform, and that the identity
// survives the wire (graph-ingest decodes the payload and calls EntityID()
// on the decoded value).
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

	for _, shape := range []struct {
		name    string
		input   map[string]any
		decoded func() Payload
	}{
		{
			name:    "document",
			input:   map[string]any{"type": "document", "id": "doc-ops-001", "title": "Ops Manual", "category": "operations"},
			decoded: func() Payload { return &Document{} },
		},
		{
			name:    "maintenance",
			input:   map[string]any{"type": "maintenance", "id": "maint-001", "title": "Compressor service", "status": "completed"},
			decoded: func() Payload { return &Maintenance{} },
		},
		{
			name:    "observation",
			input:   map[string]any{"type": "observation", "id": "obs-001", "title": "Temperature excursion", "severity": "high"},
			decoded: func() Payload { return &Observation{} },
		},
		{
			name:    "sensor_doc",
			input:   map[string]any{"type": "sensor_doc", "id": "sensor-temp-001", "title": "Temp sensor", "category": "temperature"},
			decoded: func() Payload { return &SensorDocument{} },
		},
	} {
		t.Run(shape.name, func(t *testing.T) {
			payload, err := comp.processor.Process(shape.input)
			if err != nil {
				t.Fatalf("Process: %v", err)
			}
			if !strings.HasPrefix(payload.EntityID(), deploymentAuthorityPrefix) {
				t.Errorf("entity ID %q does not mint under the deployment authority %q",
					payload.EntityID(), deploymentAuthorityPrefix)
			}
			wire, err := json.Marshal(payload)
			if err != nil {
				t.Fatalf("marshal payload: %v", err)
			}
			decoded := shape.decoded()
			if err := json.Unmarshal(wire, decoded); err != nil {
				t.Fatalf("unmarshal payload: %v", err)
			}
			if decoded.EntityID() != payload.EntityID() {
				t.Errorf("entity ID did not survive the wire: got %q, want %q", decoded.EntityID(), payload.EntityID())
			}
		})
	}
}
