package weatherstation

import (
	"encoding/json"
	"testing"

	"github.com/c360studio/semstreams/component"
)

func TestNewComponent_ValidConfig(t *testing.T) {
	config := ComponentConfig{
		OrgID:    "acme",
		Platform: "weather",
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

func TestNewComponent_MissingOrgID(t *testing.T) {
	config := ComponentConfig{
		Platform: "weather",
		Ports: &component.PortConfig{
			Inputs:  []component.PortDefinition{{Name: "input", Config: component.NATSPort{Subject: "raw.>"}}},
			Outputs: []component.PortDefinition{{Name: "output", Config: component.NATSPort{Subject: "out.>"}}},
		},
	}

	rawConfig, _ := json.Marshal(config)
	deps := component.Dependencies{}

	_, err := NewComponent(rawConfig, deps)
	if err == nil {
		t.Error("NewComponent() expected error for missing OrgID, got nil")
	}
}

func TestNewComponent_MissingPlatform(t *testing.T) {
	config := ComponentConfig{
		OrgID: "acme",
		Ports: &component.PortConfig{
			Inputs:  []component.PortDefinition{{Name: "input", Config: component.NATSPort{Subject: "raw.>"}}},
			Outputs: []component.PortDefinition{{Name: "output", Config: component.NATSPort{Subject: "out.>"}}},
		},
	}

	rawConfig, _ := json.Marshal(config)
	deps := component.Dependencies{}

	_, err := NewComponent(rawConfig, deps)
	if err == nil {
		t.Error("NewComponent() expected error for missing Platform, got nil")
	}
}

func TestComponent_InputPorts(t *testing.T) {
	config := ComponentConfig{
		OrgID:    "acme",
		Platform: "weather",
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
		OrgID:    "acme",
		Platform: "weather",
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
