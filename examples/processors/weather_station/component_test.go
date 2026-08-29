package weatherstation

import (
	"encoding/json"
	"testing"

	"github.com/c360studio/semstreams/component"
)

func TestNewComponent_ValidConfig(t *testing.T) {
	config := ComponentConfig{
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{Name: "input", Config: component.NATSPort{Subject: "raw.weather.>"}},
			},
			Outputs: []component.PortDefinition{
				{Name: "output", Config: component.NATSPort{Subject: "events.graph.entity.weather"}},
			},
		},
	}

	rawConfig, err := json.Marshal(config)
	if err != nil {
		t.Fatalf("failed to marshal config: %v", err)
	}

	deps := component.Dependencies{
		NATSClient: nil, // Not needed for this test
		Logger:     nil,
	}

	comp, err := NewComponent(rawConfig, deps)
	if err != nil {
		t.Fatalf("NewComponent() unexpected error: %v", err)
	}

	if comp == nil {
		t.Fatal("NewComponent() returned nil")
	}

	// Verify it implements Discoverable
	discoverable, ok := comp.(component.Discoverable)
	if !ok {
		t.Fatal("Component does not implement Discoverable")
	}

	meta := discoverable.Meta()
	if meta.Type != "processor" {
		t.Errorf("Meta().Type = %q, want processor", meta.Type)
	}
}

// The org_id / platform required-key tests that used to live here were
// deleted with the keys themselves (ADR-102 d2). Their replacement is
// TestRetiredAuthorityKeysAreRefused in retired_authority_keys_test.go, which
// proves the stronger fact: the keys are not merely optional now, they are
// refused, so an operator carrying them forward cannot mint under a silently
// different authority.

func TestComponent_InputPorts(t *testing.T) {
	config := ComponentConfig{
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{Name: "input", Config: component.NATSPort{Subject: "raw.weather.>"}},
			},
			Outputs: []component.PortDefinition{
				{Name: "output", Config: component.NATSPort{Subject: "events.graph.entity.weather"}},
			},
		},
	}

	rawConfig, _ := json.Marshal(config)
	deps := component.Dependencies{}

	comp, err := NewComponent(rawConfig, deps)
	if err != nil {
		t.Fatalf("NewComponent() error: %v", err)
	}

	discoverable := comp.(component.Discoverable)
	ports := discoverable.InputPorts()

	if len(ports) != 1 {
		t.Errorf("InputPorts() returned %d ports, want 1", len(ports))
	}
}

func TestComponent_OutputPorts(t *testing.T) {
	config := ComponentConfig{
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{Name: "input", Config: component.NATSPort{Subject: "raw.weather.>"}},
			},
			Outputs: []component.PortDefinition{
				{Name: "output", Config: component.NATSPort{Subject: "events.graph.entity.weather"}},
			},
		},
	}

	rawConfig, _ := json.Marshal(config)
	deps := component.Dependencies{}

	comp, err := NewComponent(rawConfig, deps)
	if err != nil {
		t.Fatalf("NewComponent() error: %v", err)
	}

	discoverable := comp.(component.Discoverable)
	ports := discoverable.OutputPorts()

	if len(ports) != 1 {
		t.Errorf("OutputPorts() returned %d ports, want 1", len(ports))
	}
}

func TestRegister(t *testing.T) {
	registry := component.NewRegistry()

	err := Register(registry)
	if err != nil {
		t.Fatalf("Register() error: %v", err)
	}

	// Verify it was registered
	factory, ok := registry.GetFactory("weather_station")
	if !ok {
		t.Error("Expected component to be registered")
	}
	if factory == nil {
		t.Error("Expected factory to be non-nil")
	}
}
