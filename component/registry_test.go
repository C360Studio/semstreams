package component

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/c360studio/semstreams/internal/componentadmission"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/types"
)

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

// TestRegisterWithConfig_DependenciesRoundTrip verifies that Dependencies
// declared via RegistrationConfig flow through to the stored Registration
// and are queryable via InstanceDependencies once an instance is tracked.
// This is the load-bearing guarantee behind ComponentManager's ability to
// route model_registry updates to the components that opted in.
func TestRegisterWithConfig_DependenciesRoundTrip(t *testing.T) {
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

	_, err = registry.CreateComponent(componentadmission.Access{}, "my-instance", types.ComponentConfig{
		Name: "reg-with-deps", Type: types.ComponentTypeProcessor, Enabled: true, Config: json.RawMessage(`{}`),
	}, Dependencies{NATSClient: new(natsclient.Client)}, nil)
	if err != nil {
		t.Fatalf("CreateComponent: %v", err)
	}

	deps := registry.InstanceDependencies("my-instance")
	if len(deps) != 1 || deps[0] != DepModelRegistry {
		t.Errorf("InstanceDependencies: got %v, want [%q]", deps, DepModelRegistry)
	}

	// Untracked instances return nil (not an empty slice — matches the
	// "no registration found" contract).
	if got := registry.InstanceDependencies("unknown-instance"); got != nil {
		t.Errorf("InstanceDependencies for unknown instance: got %v, want nil", got)
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

// TestNewRegistry_WithLogger verifies that the logger can be customized.
func TestNewRegistry_WithLogger(t *testing.T) {
	// Create a buffer to capture log output
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	// Create registry with custom logger
	registry := NewRegistry(WithLogger(logger))

	// Verify logger was set
	if registry.logger == nil {
		t.Fatal("Logger should not be nil")
	}

	// Verify it's not the default logger
	if registry.logger == slog.Default() {
		t.Error("Expected custom logger, got default logger")
	}
}

// TestNewRegistry_DefaultLogger verifies that the default logger is used when none provided.
func TestNewRegistry_DefaultLogger(t *testing.T) {
	registry := NewRegistry()

	// Verify logger was set to default
	if registry.logger == nil {
		t.Fatal("Logger should not be nil")
	}
}

// TestWithLogger_NilLogger verifies that nil logger is handled safely.
func TestWithLogger_NilLogger(t *testing.T) {
	// Should not panic and should fall back to default
	registry := NewRegistry(WithLogger(nil))

	if registry.logger == nil {
		t.Fatal("Logger should not be nil even when WithLogger(nil) is called")
	}
}
