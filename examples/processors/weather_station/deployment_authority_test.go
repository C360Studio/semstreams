package weatherstation

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
// NewComponent(rawConfig, deps) — and asserts the entity ID the wired
// processor mints carries deps.Platform in positions 1-2, and that the
// identity survives the wire.
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
		"station_id":  "ws-001",
		"temperature": 22.5,
		"humidity":    65.0,
		"condition":   "sunny",
	})
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if !strings.HasPrefix(reading.EntityID(), deploymentAuthorityPrefix) {
		t.Errorf("weather entity ID %q does not mint under the deployment authority %q",
			reading.EntityID(), deploymentAuthorityPrefix)
	}

	wire, err := json.Marshal(reading)
	if err != nil {
		t.Fatalf("marshal reading: %v", err)
	}
	decoded := &WeatherReading{}
	if err := json.Unmarshal(wire, decoded); err != nil {
		t.Fatalf("unmarshal reading: %v", err)
	}
	if decoded.EntityID() != reading.EntityID() {
		t.Errorf("entity ID did not survive the wire: got %q, want %q", decoded.EntityID(), reading.EntityID())
	}
}
