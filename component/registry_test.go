package component

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/internal/componentadmission"
	"github.com/c360studio/semstreams/pkg/errs"
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
		Ports:       mockPorts(mockFactory),
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
		Ports:        noPorts,
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
		Ports:        noPorts,
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
		Ports:   noPorts,
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

// TestRegisterFactoryRejectsNilPortDeclarer — a registration without a Ports
// declarer is refused exactly as a registration without a Factory is: a
// classified invalid error naming the factory, and no factory left behind.
func TestRegisterFactoryRejectsNilPortDeclarer(t *testing.T) {
	registry := NewRegistry()
	err := registry.RegisterWithConfig(RegistrationConfig{
		Name: "undeclared", Type: "processor",
		Factory: func(json.RawMessage, Dependencies) (Discoverable, error) {
			return &SimpleMockComponent{name: "undeclared"}, nil
		},
		Schema: ConfigSchema{Properties: map[string]PropertySchema{"port": {Type: "int"}}},
	})
	if err == nil {
		t.Fatal("RegisterWithConfig admitted a registration with a nil Ports declarer")
	}
	if !errs.IsInvalid(err) {
		t.Fatalf("nil declarer error is not classified invalid: %v", err)
	}
	if !strings.Contains(err.Error(), "undeclared") {
		t.Fatalf("nil declarer error does not name the factory: %v", err)
	}
	if _, exists := registry.ListFactories()["undeclared"]; exists {
		t.Fatal("registry retained a factory whose registration was refused")
	}
}

// TestAdmissionRefusesPortDeclarationMismatch — a declarer that returns one
// output while the constructed component reports two fails admission with a
// classified invalid error naming the factory, the instance, and the first
// differing port; the Registry retains no declaration for the instance.
func TestAdmissionRefusesPortDeclarationMismatch(t *testing.T) {
	registry := NewRegistry()
	built := &declarationTestComponent{outputs: []Port{
		declarationTestPort("events.created"),
		{
			Name: "more", Direction: DirectionOutput, Required: true,
			Config: JetStreamPort{Subjects: []string{"events.more"}},
		},
	}}
	requireNoError(t, registry.RegisterWithConfig(RegistrationConfig{
		Name: "liar", Type: "processor",
		Factory: func(json.RawMessage, Dependencies) (Discoverable, error) { return built, nil },
		Ports: func(json.RawMessage, string) (PortConfig, error) {
			return PortConfig{Outputs: []PortDefinition{{
				Name: "events", Required: true, Config: JetStreamPort{Subjects: []string{"events.created"}},
			}}}, nil
		},
	}))

	_, err := registry.CreateComponent(
		componentadmission.Access{}, "worker", declarationTestConfig("liar", `{}`), declarationTestDeps(), nil)
	if err == nil {
		t.Fatal("CreateComponent admitted a component whose constructed ports differ from its declaration")
	}
	if !errs.IsInvalid(err) {
		t.Fatalf("parity error is not classified invalid: %v", err)
	}
	for _, want := range []string{"liar", "worker", "more"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("parity error %q does not name %q", err.Error(), want)
		}
	}
	if _, ok := registry.Snapshot(componentadmission.Access{}, "worker"); ok {
		t.Fatal("Registry retained a declaration for an instance that failed the parity check")
	}
}
