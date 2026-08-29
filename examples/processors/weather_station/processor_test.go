package weatherstation

import (
	"encoding/json"
	"testing"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
)

func TestProcessor_Process_JSONTransformation(t *testing.T) {
	p := NewProcessor(testAuthority)

	inputJSON := `{
		"station_id": "ws-001",
		"temperature": 22.5,
		"humidity": 65.0,
		"condition": "sunny",
		"city": "San Francisco"
	}`

	var input map[string]any
	if err := json.Unmarshal([]byte(inputJSON), &input); err != nil {
		t.Fatalf("failed to unmarshal test input: %v", err)
	}

	result, err := p.Process(input)
	if err != nil {
		t.Fatalf("Process() unexpected error: %v", err)
	}

	// Verify result implements Graphable
	var _ graph.Graphable = result

	// Verify EntityID is valid 6-part format
	entityID := result.EntityID()
	if !message.IsValidEntityID(entityID) {
		t.Errorf("EntityID() = %q is not valid 6-part format", entityID)
	}

	// Verify Triples returns meaningful data
	triples := result.Triples()
	if len(triples) < 3 {
		t.Errorf("Triples() returned %d triples, want at least 3", len(triples))
	}

	// Verify specific values
	if result.Temperature != 22.5 {
		t.Errorf("Temperature = %v, want 22.5", result.Temperature)
	}
	if result.Condition != "sunny" {
		t.Errorf("Condition = %q, want sunny", result.Condition)
	}
}

func TestProcessor_Process_MissingField(t *testing.T) {
	p := NewProcessor(testAuthority)

	// Missing condition
	input := map[string]any{
		"station_id":  "ws-001",
		"temperature": 22.5,
		"humidity":    65.0,
	}

	_, err := p.Process(input)
	if err == nil {
		t.Error("Process() expected error for missing condition, got nil")
	}
}

// The processor Config struct and its OrgID/Platform validation were deleted
// with the operator keys (ADR-102 d2): the processor now takes the deployment
// authority from component.Dependencies.Platform, so there is no
// processor-local configuration left to validate. What replaced this test is
// TestComponentMintsUnderDeploymentAuthority in deployment_authority_test.go.
