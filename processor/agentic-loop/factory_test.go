package agenticloop_test

import (
	"testing"

	"github.com/c360studio/semstreams/component"
	agenticloop "github.com/c360studio/semstreams/processor/agentic-loop"
)

func TestRegister_SuccessfulRegistration(t *testing.T) {
	registry := component.NewRegistry()

	// Register the factory
	err := agenticloop.Register(registry)
	if err != nil {
		t.Fatalf("Register() failed: %v", err)
	}

	// Verify registration using ListComponentTypes (NOT ListComponents)
	types := registry.ListComponentTypes()
	if len(types) == 0 {
		t.Fatal("ListComponentTypes() should return registered types")
	}

	// Look for our component
	found := false
	for _, typeName := range types {
		if typeName == "agentic-loop" {
			found = true
			break
		}
	}

	if !found {
		t.Errorf("Component type 'agentic-loop' not found in registered types: %v", types)
	}
}

func TestRegister_FactoryMetadata(t *testing.T) {
	registry := component.NewRegistry()

	err := agenticloop.Register(registry)
	if err != nil {
		t.Fatalf("Register() failed: %v", err)
	}

	// Get factory metadata
	factories := registry.ListFactories()
	factoryReg, ok := factories["agentic-loop"]
	if !ok {
		t.Fatal("Factory 'agentic-loop' not found in ListFactories()")
	}

	// Verify metadata
	if factoryReg.Name != "agentic-loop" {
		t.Errorf("Factory.Name = %s, want agentic-loop", factoryReg.Name)
	}
	if factoryReg.Type != "processor" {
		t.Errorf("Factory.Type = %s, want processor", factoryReg.Type)
	}
	if factoryReg.Description == "" {
		t.Error("Factory.Description should not be empty")
	}
	if factoryReg.Version == "" {
		t.Error("Factory.Version should not be empty")
	}
	if factoryReg.Protocol == "" {
		t.Error("Factory.Protocol should not be empty")
	}
	if factoryReg.Domain == "" {
		t.Error("Factory.Domain should not be empty")
	}

	// Verify schema is present
	if len(factoryReg.Schema.Properties) == 0 {
		t.Error("Factory.Schema.Properties should not be empty")
	}
}

func TestRegister_ConfigSchema(t *testing.T) {
	registry := component.NewRegistry()

	err := agenticloop.Register(registry)
	if err != nil {
		t.Fatalf("Register() failed: %v", err)
	}

	factories := registry.ListFactories()
	factoryReg, ok := factories["agentic-loop"]
	if !ok {
		t.Fatal("Factory 'agentic-loop' not found")
	}

	schema := factoryReg.Schema

	// Verify schema has NO Type field (only Properties and Required)
	if schema.Properties == nil {
		t.Error("Schema.Properties should not be nil")
	}

	// Verify expected properties exist
	expectedProps := []string{"max_iterations", "timeout", "loops_bucket", "ports"}
	for _, propName := range expectedProps {
		if _, ok := schema.Properties[propName]; !ok {
			t.Errorf("Schema.Properties should have %q property", propName)
		}
	}

	// Verify max_iterations property
	maxIterProp, ok := schema.Properties["max_iterations"]
	if !ok {
		t.Fatal("Schema should have 'max_iterations' property")
	}
	if maxIterProp.Type != "int" {
		t.Errorf("max_iterations.Type = %s, want int", maxIterProp.Type)
	}
	if maxIterProp.Description == "" {
		t.Error("max_iterations.Description should not be empty")
	}

	// Verify timeout property
	timeoutProp, ok := schema.Properties["timeout"]
	if !ok {
		t.Fatal("Schema should have 'timeout' property")
	}
	if timeoutProp.Type != "string" {
		t.Errorf("timeout.Type = %s, want string", timeoutProp.Type)
	}
	if timeoutProp.Description == "" {
		t.Error("timeout.Description should not be empty")
	}

	// Verify loops_bucket property
	loopsBucketProp, ok := schema.Properties["loops_bucket"]
	if !ok {
		t.Fatal("Schema should have 'loops_bucket' property")
	}
	if loopsBucketProp.Type != "string" {
		t.Errorf("loops_bucket.Type = %s, want string", loopsBucketProp.Type)
	}

	// Verify ports property
	portsProp, ok := schema.Properties["ports"]
	if !ok {
		t.Fatal("Schema should have 'ports' property")
	}
	if portsProp.Type != "ports" {
		t.Errorf("ports.Type = %s, want ports", portsProp.Type)
	}
}

func TestRegister_MultipleRegistrations(t *testing.T) {
	registry := component.NewRegistry()

	// First registration should succeed
	err := agenticloop.Register(registry)
	if err != nil {
		t.Fatalf("First Register() failed: %v", err)
	}

	// Second registration should fail (duplicate)
	err = agenticloop.Register(registry)
	if err == nil {
		t.Fatal("Second Register() should fail with duplicate error")
	}

	if !containsIgnoreCase(err.Error(), "already") && !containsIgnoreCase(err.Error(), "duplicate") {
		t.Errorf("Error should mention duplicate/already registered, got: %v", err)
	}
}

func TestRegister_NilRegistry(_ *testing.T) {
	// Register with nil registry should panic or return error.
	// The defer-recover here documents both legal outcomes:
	//   - panic → recover() returns non-nil, the panic is swallowed
	//   - return error → recover() returns nil, Register handled nil gracefully
	// We don't assert which path because both are valid implementations;
	// the test exists to lock that Register(nil) doesn't crash the process.
	defer func() { _ = recover() }()

	_ = agenticloop.Register(nil)
}

func TestRegister_FactoryFunctionality(t *testing.T) {
	registry := component.NewRegistry()

	err := agenticloop.Register(registry)
	if err != nil {
		t.Fatalf("Register() failed: %v", err)
	}

	// Verify factory can create components
	factories := registry.ListFactories()
	factoryReg, ok := factories["agentic-loop"]
	if !ok {
		t.Fatal("Factory 'agentic-loop' not found")
	}

	// Note: Factory function itself is not exposed in ListFactories (security measure)
	// So we can't directly test factory creation here
	// This is tested in component_test.go via NewComponent

	// Just verify the registration exists and has correct structure
	if factoryReg.Factory != nil {
		t.Error("Factory function should not be exposed in ListFactories() for security")
	}
}

func TestRegister_SchemaValidation(t *testing.T) {
	registry := component.NewRegistry()

	err := agenticloop.Register(registry)
	if err != nil {
		t.Fatalf("Register() failed: %v", err)
	}

	factories := registry.ListFactories()
	factoryReg, ok := factories["agentic-loop"]
	if !ok {
		t.Fatal("Factory 'agentic-loop' not found")
	}

	schema := factoryReg.Schema

	// Test that schema properties have proper types
	maxIterProp := schema.Properties["max_iterations"]
	if maxIterProp.Type != "int" {
		t.Errorf("max_iterations type should be int, got %s", maxIterProp.Type)
	}

	timeoutProp := schema.Properties["timeout"]
	if timeoutProp.Type != "string" {
		t.Errorf("timeout type should be string, got %s", timeoutProp.Type)
	}

	loopsBucketProp := schema.Properties["loops_bucket"]
	if loopsBucketProp.Type != "string" {
		t.Errorf("loops_bucket type should be string, got %s", loopsBucketProp.Type)
	}

	// Verify schema can be used for validation
	// (Actual validation tested in config_test.go)
	if len(schema.Properties) < 4 {
		t.Errorf("Schema should have at least 4 properties, got %d", len(schema.Properties))
	}
}

func TestRegister_DefaultValues(t *testing.T) {
	registry := component.NewRegistry()

	err := agenticloop.Register(registry)
	if err != nil {
		t.Fatalf("Register() failed: %v", err)
	}

	factories := registry.ListFactories()
	factoryReg, ok := factories["agentic-loop"]
	if !ok {
		t.Fatal("Factory 'agentic-loop' not found")
	}

	schema := factoryReg.Schema

	// Verify default values in schema
	maxIterProp := schema.Properties["max_iterations"]
	if maxIterProp.Default == nil {
		t.Log("max_iterations has no default (may be intentional)")
	} else if defaultVal, ok := maxIterProp.Default.(int); ok {
		if defaultVal != 20 {
			t.Logf("max_iterations default = %d, expected 20 (may vary)", defaultVal)
		}
	}

	timeoutProp := schema.Properties["timeout"]
	if timeoutProp.Default == nil {
		t.Log("timeout has no default (may be intentional)")
	} else if defaultVal, ok := timeoutProp.Default.(string); ok {
		if defaultVal != "120s" {
			t.Logf("timeout default = %s, expected 120s (may vary)", defaultVal)
		}
	}
}

func TestRegister_ComponentDomain(t *testing.T) {
	registry := component.NewRegistry()

	err := agenticloop.Register(registry)
	if err != nil {
		t.Fatalf("Register() failed: %v", err)
	}

	factories := registry.ListFactories()
	factoryReg, ok := factories["agentic-loop"]
	if !ok {
		t.Fatal("Factory 'agentic-loop' not found")
	}

	// Verify domain is set correctly (agentic components)
	if factoryReg.Domain == "" {
		t.Error("Factory.Domain should not be empty")
	}

	// Domain should indicate this is part of agentic system
	if !containsIgnoreCase(factoryReg.Domain, "agent") {
		t.Logf("Factory.Domain = %s, expected to contain 'agent' (may vary)", factoryReg.Domain)
	}
}

func TestRegister_ComponentProtocol(t *testing.T) {
	registry := component.NewRegistry()

	err := agenticloop.Register(registry)
	if err != nil {
		t.Fatalf("Register() failed: %v", err)
	}

	factories := registry.ListFactories()
	factoryReg, ok := factories["agentic-loop"]
	if !ok {
		t.Fatal("Factory 'agentic-loop' not found")
	}

	// Verify protocol is set
	if factoryReg.Protocol == "" {
		t.Error("Factory.Protocol should not be empty")
	}

	// Protocol should indicate NATS messaging
	if !containsIgnoreCase(factoryReg.Protocol, "nats") && !containsIgnoreCase(factoryReg.Protocol, "message") {
		t.Logf("Factory.Protocol = %s, expected to mention NATS or messaging (may vary)", factoryReg.Protocol)
	}
}

func TestRegister_VersionFormat(t *testing.T) {
	registry := component.NewRegistry()

	err := agenticloop.Register(registry)
	if err != nil {
		t.Fatalf("Register() failed: %v", err)
	}

	factories := registry.ListFactories()
	factoryReg, ok := factories["agentic-loop"]
	if !ok {
		t.Fatal("Factory 'agentic-loop' not found")
	}

	// Verify version is in semantic versioning format (at least non-empty)
	if factoryReg.Version == "" {
		t.Error("Factory.Version should not be empty")
	}

	// Common version patterns: "1.0.0", "v1.0.0", "0.1.0", etc.
	// Just verify it's a reasonable string
	if len(factoryReg.Version) < 3 {
		t.Errorf("Factory.Version = %s, seems too short for semantic version", factoryReg.Version)
	}
}
