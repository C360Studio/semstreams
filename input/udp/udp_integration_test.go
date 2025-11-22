//go:build integration

package udp

import (
	"testing"
)

// TestUDPSchemaRegistration tests UDP component schema registration (T016)
// Given: UDP component initialized
// When: ConfigSchema() called
// Then: Returns schema with port (int, default 14550), bind (string, default "0.0.0.0"), subject (string)
func TestUDPSchemaRegistration(t *testing.T) {
	// Create UDP component
	// Note: We don't need a real NATS client to test the schema
	deps := InputDeps{
		Config:          DefaultConfig(),
		NATSClient:      nil, // Schema doesn't require runtime deps
		MetricsRegistry: nil,
		Logger:          nil,
	}
	udp := NewInput(deps)

	// Get schema
	schema := udp.ConfigSchema()

	// Verify schema structure
	if schema.Properties == nil {
		t.Fatal("Schema should have properties")
	}

	// Verify ports field (Architecture Decision: Ports in Schema)
	portsProp, exists := schema.Properties["ports"]
	if !exists {
		t.Fatal("Schema should have ports property")
	}
	if portsProp.Type != "ports" {
		t.Errorf("Ports should be ports type (first-class), got %s", portsProp.Type)
	}
	if portsProp.Category != "basic" {
		t.Errorf("Ports should be basic category, got %s", portsProp.Category)
	}

	// Verify no required fields (ports are optional, use defaults)
	if len(schema.Required) != 0 {
		t.Errorf("Schema should have no required fields (ports has defaults), got %v", schema.Required)
	}
}
