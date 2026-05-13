package directorybridge

import (
	"encoding/json"
	"testing"

	"github.com/c360studio/semstreams/component"
)

func TestNewComponent(t *testing.T) {
	config := DefaultConfig()
	config.DirectoryURL = "https://directory.example.com"

	rawConfig, err := json.Marshal(config)
	if err != nil {
		t.Fatalf("failed to marshal config: %v", err)
	}

	deps := component.Dependencies{
		NATSClient: nil, // Would need testcontainers for real test
		Logger:     nil,
	}

	disc, err := NewComponent(rawConfig, deps)
	if err != nil {
		t.Fatalf("NewComponent() error = %v", err)
	}

	comp, ok := disc.(*Component)
	if !ok {
		t.Fatal("expected *Component type")
	}

	if comp.name != "directory-bridge" {
		t.Errorf("name = %s, want directory-bridge", comp.name)
	}

	if comp.config.DirectoryURL != "https://directory.example.com" {
		t.Errorf("DirectoryURL = %s, want https://directory.example.com", comp.config.DirectoryURL)
	}
}

func TestNewComponent_InvalidConfig(t *testing.T) {
	tests := []struct {
		name    string
		config  string
		wantErr bool
	}{
		{
			name:    "invalid JSON",
			config:  "{invalid}",
			wantErr: true,
		},
		{
			name:    "invalid heartbeat interval",
			config:  `{"ports":{"inputs":[{"name":"test"}],"outputs":[]},"oasf_kv_bucket":"TEST","heartbeat_interval":"invalid"}`,
			wantErr: true,
		},
	}

	deps := component.Dependencies{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewComponent(json.RawMessage(tt.config), deps)
			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestComponent_Meta(t *testing.T) {
	config := DefaultConfig()
	rawConfig, _ := json.Marshal(config)

	disc, _ := NewComponent(rawConfig, component.Dependencies{})
	comp := disc.(*Component)

	meta := comp.Meta()

	if meta.Name != "directory-bridge" {
		t.Errorf("Name = %s, want directory-bridge", meta.Name)
	}
	if meta.Type != "output" {
		t.Errorf("Type = %s, want output", meta.Type)
	}
	if meta.Version != "1.0.0" {
		t.Errorf("Version = %s, want 1.0.0", meta.Version)
	}
}

func TestComponent_InputPorts(t *testing.T) {
	config := DefaultConfig()
	rawConfig, _ := json.Marshal(config)

	disc, _ := NewComponent(rawConfig, component.Dependencies{})
	comp := disc.(*Component)

	ports := comp.InputPorts()

	if len(ports) != 1 {
		t.Fatalf("expected 1 input port, got %d", len(ports))
	}

	port := ports[0]
	if port.Name != "oasf_records" {
		t.Errorf("port name = %s, want oasf_records", port.Name)
	}
	if port.Direction != component.DirectionInput {
		t.Errorf("port direction = %v, want Input", port.Direction)
	}
	if !port.Required {
		t.Error("expected port to be required")
	}
}

func TestComponent_OutputPorts(t *testing.T) {
	config := DefaultConfig()
	rawConfig, _ := json.Marshal(config)

	disc, _ := NewComponent(rawConfig, component.Dependencies{})
	comp := disc.(*Component)

	ports := comp.OutputPorts()

	if len(ports) != 1 {
		t.Fatalf("expected 1 output port, got %d", len(ports))
	}

	port := ports[0]
	if port.Name != "registration_events" {
		t.Errorf("port name = %s, want registration_events", port.Name)
	}
	if port.Direction != component.DirectionOutput {
		t.Errorf("port direction = %v, want Output", port.Direction)
	}
}

func TestComponent_ConfigSchema(t *testing.T) {
	config := DefaultConfig()
	rawConfig, _ := json.Marshal(config)

	disc, _ := NewComponent(rawConfig, component.Dependencies{})
	comp := disc.(*Component)

	schema := comp.ConfigSchema()

	// Check that expected fields are present
	expectedFields := []string{"ports", "directory_url", "heartbeat_interval", "registration_ttl", "identity_provider", "oasf_kv_bucket"}
	for _, field := range expectedFields {
		if _, ok := schema.Properties[field]; !ok {
			t.Errorf("expected field %s in schema properties", field)
		}
	}
}

func TestComponent_Health_NotRunning(t *testing.T) {
	config := DefaultConfig()
	rawConfig, _ := json.Marshal(config)

	disc, _ := NewComponent(rawConfig, component.Dependencies{})
	comp := disc.(*Component)

	health := comp.Health()

	if health.Healthy {
		t.Error("expected Healthy=false when not running")
	}
	if health.Status != "stopped" {
		t.Errorf("Status = %s, want stopped", health.Status)
	}
}

func TestComponent_DataFlow_Initial(t *testing.T) {
	config := DefaultConfig()
	rawConfig, _ := json.Marshal(config)

	disc, _ := NewComponent(rawConfig, component.Dependencies{})
	comp := disc.(*Component)

	flow := comp.DataFlow()

	if flow.ErrorRate != 0 {
		t.Errorf("ErrorRate = %f, want 0", flow.ErrorRate)
	}
}

func TestComponent_Initialize(t *testing.T) {
	config := DefaultConfig()
	config.DirectoryURL = "https://directory.example.com"
	rawConfig, _ := json.Marshal(config)

	disc, _ := NewComponent(rawConfig, component.Dependencies{})
	comp := disc.(*Component)

	err := comp.Initialize()
	if err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	if comp.backend == nil {
		t.Error("expected backend to be initialized")
	}

	if comp.regManager == nil {
		t.Error("expected regManager to be initialized")
	}

	if comp.idProvider == nil {
		t.Error("expected idProvider to be initialized")
	}
}

func TestComponent_GetRegistrations_BeforeInit(t *testing.T) {
	config := DefaultConfig()
	rawConfig, _ := json.Marshal(config)

	disc, _ := NewComponent(rawConfig, component.Dependencies{})
	comp := disc.(*Component)

	// Should return nil before initialization
	regs := comp.GetRegistrations()
	if regs != nil {
		t.Errorf("expected nil registrations before init, got %v", regs)
	}
}

func TestComponent_NilPortsConfig(t *testing.T) {
	// Test with minimal config that will use defaults
	config := `{"oasf_kv_bucket":"TEST"}`

	disc, err := NewComponent(json.RawMessage(config), component.Dependencies{})
	if err != nil {
		t.Fatalf("NewComponent() error = %v", err)
	}

	comp := disc.(*Component)

	// Should have default ports
	if comp.config.Ports == nil {
		t.Error("expected Ports to be set from defaults")
	}

	ports := comp.InputPorts()
	if len(ports) == 0 {
		t.Error("expected input ports from defaults")
	}
}
