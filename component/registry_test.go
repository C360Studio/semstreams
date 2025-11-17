package component

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/c360/semstreams/natsclient"
	"github.com/c360/semstreams/types"
)

// MockComponent implements the Discoverable interface for testing
type MockComponent struct {
	name          string
	componentType string
	inputPorts    []Port
	outputPorts   []Port
	healthy       bool
}

func NewMockComponent(name, componentType string) *MockComponent {
	return &MockComponent{
		name:          name,
		componentType: componentType,
		healthy:       true,
		inputPorts: []Port{
			{
				Name:        "input",
				Direction:   DirectionInput,
				Required:    true,
				Description: "Test input port",
				Config:      NATSPort{Subject: "test.input"},
			},
		},
		outputPorts: []Port{
			{
				Name:        "output",
				Direction:   DirectionOutput,
				Required:    true,
				Description: "Test output port",
				Config:      NATSPort{Subject: "test.output"},
			},
		},
	}
}

func (m *MockComponent) Meta() Metadata {
	return Metadata{
		Name:        m.name,
		Type:        m.componentType,
		Description: "Mock component for testing",
		Version:     "1.0.0",
	}
}

func (m *MockComponent) InputPorts() []Port {
	return m.inputPorts
}

func (m *MockComponent) OutputPorts() []Port {
	return m.outputPorts
}

func (m *MockComponent) ConfigSchema() ConfigSchema {
	return ConfigSchema{
		Properties: map[string]PropertySchema{
			"port": {Type: "int", Description: "Port number", Default: 8080},
		},
		Required: []string{"port"},
	}
}

func (m *MockComponent) Health() HealthStatus {
	return HealthStatus{
		Healthy:   m.healthy,
		LastCheck: time.Now(),
		Uptime:    time.Hour,
	}
}

func (m *MockComponent) DataFlow() FlowMetrics {
	return FlowMetrics{
		MessagesPerSecond: 10.0,
		BytesPerSecond:    1024.0,
		LastActivity:      time.Now(),
	}
}

// Mock factory function
func createMockComponent(rawConfig json.RawMessage, _ Dependencies) (Discoverable, error) {
	// Parse config
	config := make(map[string]any)
	if len(rawConfig) > 0 {
		if err := json.Unmarshal(rawConfig, &config); err != nil {
			return nil, err
		}
	}

	// Use safe config access to prevent panics
	name := getString(config, "name", "")
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}

	componentType := getString(config, "type", "test")

	return NewMockComponent(name, componentType), nil
}

// Local safe getter to avoid import cycle
func getString(cfg map[string]any, key string, defaultVal string) string {
	if val, ok := cfg[key]; ok {
		if str, ok := val.(string); ok {
			return str
		}
	}
	return defaultVal
}

// Factory that always fails
func failingFactory(_ json.RawMessage, _ Dependencies) (Discoverable, error) {
	return nil, fmt.Errorf("factory failure")
}

func TestNewRegistry(t *testing.T) {
	registry := NewRegistry()

	if registry == nil {
		t.Fatal("NewRegistry returned nil")
	}

	if registry.factories == nil {
		t.Error("factories map not initialized")
	}

	if registry.instances == nil {
		t.Error("instances map not initialized")
	}

	// Should start empty
	if len(registry.factories) != 0 {
		t.Error("factories should start empty")
	}

	if len(registry.instances) != 0 {
		t.Error("instances should start empty")
	}
}

func TestRegisterFactory(t *testing.T) {
	registry := NewRegistry()

	registration := &Registration{
		Factory:     createMockComponent,
		Type:        "input",
		Protocol:    "test",
		Description: "Test component",
		Version:     "1.0.0",
	}

	// Successful registration
	err := registry.RegisterFactory("test", registration)
	if err != nil {
		t.Fatalf("Failed to register factory: %v", err)
	}

	// Check that factory was registered
	factories := registry.ListFactories()
	if len(factories) != 1 {
		t.Errorf("Expected 1 factory, got %d", len(factories))
	}

	if factories["test"] == nil {
		t.Error("Factory 'test' not found")
	}

	// Duplicate registration should fail
	err = registry.RegisterFactory("test", registration)
	if err == nil {
		t.Error("Expected error for duplicate factory registration")
	}
}

func TestRegisterFactoryValidation(t *testing.T) {
	registry := NewRegistry()

	tests := []struct {
		name         string
		factoryName  string
		registration *Registration
		expectError  bool
		errorMsg     string
	}{
		{
			name:        "empty name",
			factoryName: "",
			registration: &Registration{
				Factory: createMockComponent,
				Type:    "input",
			},
			expectError: true,
			errorMsg:    "factory name",
		},
		{
			name:         "nil registration",
			factoryName:  "test",
			registration: nil,
			expectError:  true,
			errorMsg:     "registration",
		},
		{
			name:        "nil factory",
			factoryName: "test",
			registration: &Registration{
				Type: "input",
			},
			expectError: true,
			errorMsg:    "factory",
		},
		{
			name:        "empty type",
			factoryName: "test",
			registration: &Registration{
				Factory: createMockComponent,
			},
			expectError: true,
			errorMsg:    "type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := registry.RegisterFactory(tt.factoryName, tt.registration)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				} else if !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("Expected error containing '%s', got '%s'", tt.errorMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
			}
		})
	}
}

func TestCreateComponent(t *testing.T) {
	registry := NewRegistry()

	// Register a factory
	registration := &Registration{
		Factory:     createMockComponent,
		Type:        "input",
		Protocol:    "test",
		Description: "Test component",
		Version:     "1.0.0",
	}

	err := registry.RegisterFactory("test", registration)
	if err != nil {
		t.Fatalf("Failed to register factory: %v", err)
	}

	// Create component
	rawConfig := []byte(`{"name":"test-instance","type":"input"}`)

	testClient := natsclient.NewTestClient(t, natsclient.WithMinimalFeatures())
	deps := Dependencies{
		NATSClient:      testClient.Client,
		MetricsRegistry: nil,
		Platform: PlatformMeta{
			Org:      "test",
			Platform: "test-platform",
		},
	}

	// Create component config
	config := types.ComponentConfig{
		Type:    types.ComponentTypeInput,
		Name:    "test",
		Enabled: true,
		Config:  rawConfig,
	}
	component, err := registry.CreateComponent("test-instance", config, deps)
	if err != nil {
		t.Fatalf("Failed to create component: %v", err)
	}

	if component == nil {
		t.Fatal("Created component is nil")
	}

	// Verify component was registered as instance
	instances := registry.ListComponents()
	if len(instances) != 1 {
		t.Errorf("Expected 1 instance, got %d", len(instances))
	}

	if instances["test-instance"] == nil {
		t.Error("Instance 'test-instance' not found")
	}

	// Verify metadata
	meta := component.Meta()
	if meta.Name != "test-instance" {
		t.Errorf("Expected name 'test-instance', got '%s'", meta.Name)
	}
}

func TestCreateComponentValidation(t *testing.T) {
	registry := NewRegistry()

	// Register a factory
	registration := &Registration{
		Factory: createMockComponent,
		Type:    "input",
	}
	_ = registry.RegisterFactory("test", registration)

	config := map[string]any{"name": "test"}

	tests := []struct {
		name          string
		componentType string // This is actually the factory name in the old API
		instanceName  string
		expectError   bool
		errorContains string
	}{
		{
			name:          "empty factory name",
			componentType: "",
			instanceName:  "test",
			expectError:   true,
			errorContains: "factory name cannot be empty",
		},
		{
			name:          "empty instance name",
			componentType: "test",
			instanceName:  "",
			expectError:   true,
			errorContains: "instance name cannot be empty",
		},
		{
			name:          "unknown factory name",
			componentType: "unknown",
			instanceName:  "test",
			expectError:   true,
			errorContains: "unknown component factory 'unknown'",
		},
	}

	testClient := natsclient.NewTestClient(t, natsclient.WithMinimalFeatures())

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rawConfig, _ := json.Marshal(config)
			deps := Dependencies{
				NATSClient:      testClient.Client,
				MetricsRegistry: nil,
				Platform: PlatformMeta{
					Org:      "test",
					Platform: "test-platform",
				},
			}

			// Create component config
			componentConfig := types.ComponentConfig{
				Type:    types.ComponentTypeInput,
				Name:    tt.componentType, // This is the factory name in the test
				Enabled: true,
				Config:  rawConfig,
			}
			_, err := registry.CreateComponent(tt.instanceName, componentConfig, deps)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				} else if err.Error() == "" {
					t.Error("Expected non-empty error message")
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
			}
		})
	}
}

func TestCreateComponentFactoryFailure(t *testing.T) {
	registry := NewRegistry()

	// Register a failing factory
	registration := &Registration{
		Factory: failingFactory,
		Type:    "input",
	}

	err := registry.RegisterFactory("failing", registration)
	if err != nil {
		t.Fatalf("Failed to register factory: %v", err)
	}

	rawConfig := []byte(`{"name":"test"}`)

	testClient := natsclient.NewTestClient(t, natsclient.WithMinimalFeatures())
	deps := Dependencies{
		NATSClient:      testClient.Client,
		MetricsRegistry: nil,
		Platform: PlatformMeta{
			Org:      "test",
			Platform: "test-platform",
		},
	}
	// Create component config
	config := types.ComponentConfig{
		Type:    types.ComponentTypeInput,
		Name:    "failing",
		Enabled: true,
		Config:  rawConfig,
	}
	_, err = registry.CreateComponent("test-instance", config, deps)
	if err == nil {
		t.Error("Expected error from failing factory")
	}

	// Verify no instance was registered on failure
	instances := registry.ListComponents()
	if len(instances) != 0 {
		t.Errorf("Expected no instances after factory failure, got %d", len(instances))
	}
}

func TestRegisterInstance(t *testing.T) {
	registry := NewRegistry()
	component := NewMockComponent("test", "input")

	// Successful registration
	err := registry.RegisterInstance("test-instance", component)
	if err != nil {
		t.Fatalf("Failed to register instance: %v", err)
	}

	// Verify instance was registered
	retrieved := registry.Component("test-instance")
	if retrieved == nil {
		t.Error("Instance not found after registration")
	}

	if retrieved != component {
		t.Error("Retrieved component is not the same as registered")
	}

	// Duplicate registration should fail
	err = registry.RegisterInstance("test-instance", component)
	if err == nil {
		t.Error("Expected error for duplicate instance registration")
	}
}

func TestRegisterInstanceValidation(t *testing.T) {
	registry := NewRegistry()
	component := NewMockComponent("test", "input")

	tests := []struct {
		name         string
		instanceName string
		component    Discoverable
		expectError  bool
		errorMsg     string
	}{
		{
			name:         "empty name",
			instanceName: "",
			component:    component,
			expectError:  true,
			errorMsg:     "instance name",
		},
		{
			name:         "nil component",
			instanceName: "test",
			component:    nil,
			expectError:  true,
			errorMsg:     "component",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := registry.RegisterInstance(tt.instanceName, tt.component)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				} else if !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("Expected error containing '%s', got '%s'", tt.errorMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
			}
		})
	}
}

func TestUnregisterInstance(t *testing.T) {
	registry := NewRegistry()
	component := NewMockComponent("test", "input")

	// Register instance
	err := registry.RegisterInstance("test-instance", component)
	if err != nil {
		t.Fatalf("Failed to register instance: %v", err)
	}

	// Verify it exists
	if registry.Component("test-instance") == nil {
		t.Error("Instance not found after registration")
	}

	// Unregister
	registry.UnregisterInstance("test-instance")

	// Verify it's gone
	if registry.Component("test-instance") != nil {
		t.Error("Instance still found after unregistration")
	}

	// Unregistering non-existent instance should not panic
	registry.UnregisterInstance("non-existent")

	// Unregistering with empty name should not panic
	registry.UnregisterInstance("")
}

func TestListComponents(t *testing.T) {
	registry := NewRegistry()

	// Start empty
	components := registry.ListComponents()
	if len(components) != 0 {
		t.Errorf("Expected 0 components, got %d", len(components))
	}

	// Add some components
	comp1 := NewMockComponent("comp1", "input")
	comp2 := NewMockComponent("comp2", "output")

	_ = registry.RegisterInstance("instance1", comp1)
	_ = registry.RegisterInstance("instance2", comp2)

	// List components
	components = registry.ListComponents()
	if len(components) != 2 {
		t.Errorf("Expected 2 components, got %d", len(components))
	}

	if components["instance1"] != comp1 {
		t.Error("Component instance1 not found or incorrect")
	}

	if components["instance2"] != comp2 {
		t.Error("Component instance2 not found or incorrect")
	}

	// Verify it's a copy (modifying returned map shouldn't affect registry)
	delete(components, "instance1")

	updatedList := registry.ListComponents()
	if len(updatedList) != 2 {
		t.Error("Modifying returned map affected registry")
	}
}

func TestGetComponent(t *testing.T) {
	registry := NewRegistry()
	component := NewMockComponent("test", "input")

	// Non-existent component
	retrieved := registry.Component("non-existent")
	if retrieved != nil {
		t.Error("Expected nil for non-existent component")
	}

	// Register and retrieve
	_ = registry.RegisterInstance("test-instance", component)
	retrieved = registry.Component("test-instance")

	if retrieved == nil {
		t.Error("Component not found after registration")
	}

	if retrieved != component {
		t.Error("Retrieved component is not the same as registered")
	}
}

func TestListFactories(t *testing.T) {
	registry := NewRegistry()

	// Start empty
	factories := registry.ListFactories()
	if len(factories) != 0 {
		t.Errorf("Expected 0 factories, got %d", len(factories))
	}

	// Add some factories
	reg1 := &Registration{
		Factory:     createMockComponent,
		Type:        "input",
		Protocol:    "tcp",
		Description: "TCP input",
		Version:     "1.0.0",
	}

	reg2 := &Registration{
		Factory:     createMockComponent,
		Type:        "output",
		Protocol:    "websocket",
		Description: "WebSocket output",
		Version:     "2.0.0",
	}

	_ = registry.RegisterFactory("tcp", reg1)
	_ = registry.RegisterFactory("websocket", reg2)

	// List factories
	factories = registry.ListFactories()
	if len(factories) != 2 {
		t.Errorf("Expected 2 factories, got %d", len(factories))
	}

	tcp := factories["tcp"]
	if tcp == nil {
		t.Fatal("TCP factory not found")
	}

	if tcp.Type != "input" {
		t.Errorf("Expected type 'input', got '%s'", tcp.Type)
	}

	if tcp.Protocol != "tcp" {
		t.Errorf("Expected protocol 'tcp', got '%s'", tcp.Protocol)
	}

	// Verify factory function is not copied (for safety)
	if tcp.Factory != nil {
		t.Error("Factory function should not be copied in ListFactories")
	}
}

func TestConcurrentAccess(t *testing.T) {
	registry := NewRegistry()

	// Register a factory for testing
	registration := &Registration{
		Factory: createMockComponent,
		Type:    "input",
	}
	_ = registry.RegisterFactory("test", registration)

	// Create shared test client for concurrent access
	testClient := natsclient.NewTestClient(t, natsclient.WithMinimalFeatures())

	var wg sync.WaitGroup
	errors := make(chan error, 100)

	// Concurrent component creation
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			instanceName := fmt.Sprintf("instance-%d", id)
			config := map[string]any{
				"name": instanceName,
				"type": "input",
			}
			rawConfig, _ := json.Marshal(config)

			deps := Dependencies{
				NATSClient:      testClient.Client,
				MetricsRegistry: nil,
				Platform: PlatformMeta{
					Org:      "test",
					Platform: "test-platform",
				},
			}
			// Create component config
			componentConfig := types.ComponentConfig{
				Type:    types.ComponentTypeInput,
				Name:    "test",
				Enabled: true,
				Config:  rawConfig,
			}
			_, err := registry.CreateComponent(instanceName, componentConfig, deps)
			if err != nil {
				errors <- err
			}
		}(i)
	}

	// Concurrent instance registration
	for i := 10; i < 20; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			instanceName := fmt.Sprintf("manual-%d", id)
			component := NewMockComponent(instanceName, "input")

			err := registry.RegisterInstance(instanceName, component)
			if err != nil {
				errors <- err
			}
		}(i)
	}

	// Concurrent reads
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			_ = registry.ListComponents()
			_ = registry.ListFactories()
			_ = registry.Component("instance-1")
		}()
	}

	wg.Wait()
	close(errors)

	// Check for any errors
	for err := range errors {
		t.Errorf("Concurrent operation failed: %v", err)
	}

	// Verify final state
	components := registry.ListComponents()
	if len(components) != 20 {
		t.Errorf("Expected 20 components after concurrent operations, got %d", len(components))
	}
}
