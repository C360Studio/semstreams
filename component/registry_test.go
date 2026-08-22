package component

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestRegistryDoesNotExposeRemoteCapabilityDiscovery(t *testing.T) {
	t.Parallel()

	registryType := reflect.TypeOf((*Registry)(nil))
	for _, methodName := range []string{
		"InitNATS",
		"GetCapabilities",
		"WaitForCapabilities",
		"StartHeartbeat",
		"StopHeartbeat",
		"SubscribeCapabilities",
	} {
		if _, exists := registryType.MethodByName(methodName); exists {
			t.Errorf("Registry still exports retired remote capability method %s", methodName)
		}
	}

	if got := reflect.TypeOf(NewRegistry).NumIn(); got != 0 {
		t.Errorf("NewRegistry accepts %d options, want a no-option constructor", got)
	}
}

// TestListFactories_PreservesSchemaAndName verifies that ListFactories copies
// the Name and Schema fields. This is critical for schema generation tooling.
// Regression test for bug where ListFactories omitted these fields.
func TestListFactories_PreservesSchemaAndName(t *testing.T) {
	registry := NewRegistry()

	// Create a schema with properties
	schema := ConfigSchema{
		Properties: map[string]PropertySchema{
			"port": {
				Type:        "int",
				Description: "Listen port",
			},
			"host": {
				Type:        "string",
				Description: "Hostname",
			},
		},
		Required: []string{"port"},
	}

	// Mock factory function
	mockFactory := func(_ json.RawMessage, _ Dependencies) (Discoverable, error) {
		return &SimpleMockComponent{name: "my-instance", config: map[string]any{"type": "processor"}}, nil
	}

	reg := &Registration{
		Name:        "test-component",
		Factory:     mockFactory,
		Type:        "input",
		Protocol:    "tcp",
		Domain:      "network",
		Description: "Test component",
		Version:     "1.0.0",
		Schema:      schema,
	}

	err := registry.RegisterFactory("test-component", reg)
	if err != nil {
		t.Fatalf("Failed to register: %v", err)
	}

	// Retrieve via ListFactories
	factories := registry.ListFactories()
	retrieved := factories["test-component"]
	if retrieved == nil {
		t.Fatal("Factory not found")
	}

	// Verify Name is preserved
	if retrieved.Name != "test-component" {
		t.Errorf("Name not preserved: got %q, want %q", retrieved.Name, "test-component")
	}

	// Verify Schema.Properties is preserved (critical for schema generation)
	if retrieved.Schema.Properties == nil {
		t.Fatal("Schema.Properties is nil - ListFactories must copy Schema field")
	}

	if len(retrieved.Schema.Properties) != 2 {
		t.Errorf("Schema.Properties length: got %d, want 2", len(retrieved.Schema.Properties))
	}

	// Verify specific properties exist
	if _, ok := retrieved.Schema.Properties["port"]; !ok {
		t.Error("Schema.Properties missing 'port' field")
	}
	if _, ok := retrieved.Schema.Properties["host"]; !ok {
		t.Error("Schema.Properties missing 'host' field")
	}

	// Verify property details
	portProp := retrieved.Schema.Properties["port"]
	if portProp.Type != "int" {
		t.Errorf("port.Type: got %q, want %q", portProp.Type, "int")
	}
	if portProp.Description != "Listen port" {
		t.Errorf("port.Description: got %q, want %q", portProp.Description, "Listen port")
	}

	// Verify Required is preserved
	if len(retrieved.Schema.Required) != 1 || retrieved.Schema.Required[0] != "port" {
		t.Errorf("Schema.Required: got %v, want [port]", retrieved.Schema.Required)
	}

	// Verify Factory is NOT copied (security measure)
	if retrieved.Factory != nil {
		t.Error("Factory should not be copied in ListFactories for safety")
	}
}

func TestListFactories_ReturnsDefensiveMetadataClones(t *testing.T) {
	registry := NewRegistry()
	if err := registry.RegisterFactory("test-component", &Registration{
		Name: "test-component", Type: "processor",
		Factory: func(_ json.RawMessage, _ Dependencies) (Discoverable, error) {
			return &SimpleMockComponent{name: "instance"}, nil
		},
		Dependencies: []string{DepModelRegistry},
		Schema: ConfigSchema{
			Properties: map[string]PropertySchema{"port": {Type: "int"}},
			Required:   []string{"port"},
		},
	}); err != nil {
		t.Fatal(err)
	}

	first := registry.ListFactories()["test-component"]
	first.Dependencies[0] = "mutated"
	first.Schema.Required[0] = "mutated"
	delete(first.Schema.Properties, "port")

	second := registry.ListFactories()["test-component"]
	if got := second.Dependencies; len(got) != 1 || got[0] != DepModelRegistry {
		t.Fatalf("dependencies leaked caller mutation: %v", got)
	}
	if got := second.Schema.Required; len(got) != 1 || got[0] != "port" {
		t.Fatalf("required fields leaked caller mutation: %v", got)
	}
	if _, ok := second.Schema.Properties["port"]; !ok {
		t.Fatal("schema properties leaked caller mutation")
	}
}

// TestRegisterWithConfig_DependenciesMetadata verifies that declared
// dependencies remain discoverable as factory metadata.
func TestRegisterWithConfig_DependenciesMetadata(t *testing.T) {
	registry := NewRegistry()

	mockFactory := func(_ json.RawMessage, _ Dependencies) (Discoverable, error) {
		return &SimpleMockComponent{name: "my-instance", config: map[string]any{"type": "processor"}}, nil
	}

	err := registry.RegisterWithConfig(RegistrationConfig{
		Name:         "reg-with-deps",
		Factory:      mockFactory,
		Type:         "processor",
		Protocol:     "test",
		Domain:       "test",
		Description:  "Test component with a declared dep",
		Version:      "1.0.0",
		Dependencies: []string{DepModelRegistry},
	})
	if err != nil {
		t.Fatalf("RegisterWithConfig: %v", err)
	}

	// Registration metadata reflects the declared dependency.
	factories := registry.ListFactories()
	reg, ok := factories["reg-with-deps"]
	if !ok {
		t.Fatal("factory not found in ListFactories")
	}
	if len(reg.Dependencies) != 1 || reg.Dependencies[0] != DepModelRegistry {
		t.Errorf("Registration.Dependencies: got %v, want [%q]", reg.Dependencies, DepModelRegistry)
	}

}

// TestRegisterWithConfig_NoDependencies confirms the default (zero-value)
// case. A component that doesn't declare any deps must not receive
// spurious entries — ComponentManager would mis-route updates otherwise.
func TestRegisterWithConfig_NoDependencies(t *testing.T) {
	registry := NewRegistry()

	mockFactory := func(_ json.RawMessage, _ Dependencies) (Discoverable, error) {
		return nil, nil
	}

	err := registry.RegisterWithConfig(RegistrationConfig{
		Name:    "reg-no-deps",
		Factory: mockFactory,
		Type:    "processor",
	})
	if err != nil {
		t.Fatalf("RegisterWithConfig: %v", err)
	}

	factories := registry.ListFactories()
	reg := factories["reg-no-deps"]
	if len(reg.Dependencies) != 0 {
		t.Errorf("Registration.Dependencies for no-deps component: got %v, want empty", reg.Dependencies)
	}
}
