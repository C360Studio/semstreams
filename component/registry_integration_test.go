//go:build integration

package component

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/c360studio/semstreams/internal/componentadmission"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/types"
)

// MockComponent implements the Discoverable interface for testing
type MockComponent struct {
	name          string
	componentType string
	inputPorts    []Port
	outputPorts   []Port
	healthy       bool
}

func newCapabilityRegistry(t *testing.T, client *natsclient.Client, nodeID string) *Registry {
	t.Helper()
	registry := NewRegistry()
	if err := registry.InitNATS(context.Background(), client, nodeID); err != nil {
		t.Fatalf("InitNATS: %v", err)
	}
	if err := registry.RegisterFactory("test-component", &Registration{
		Name: "test-component", Factory: createMockComponent, Type: "processor",
		Protocol: "test", Description: "Test component", Version: "1.0.0",
	}); err != nil {
		t.Fatalf("RegisterFactory: %v", err)
	}
	return registry
}

func admitCapabilityComponent(t *testing.T, registry *Registry, client *natsclient.Client, name string) {
	t.Helper()
	_, err := registry.CreateComponent(componentadmission.Access{}, name, types.ComponentConfig{
		Type: types.ComponentTypeProcessor, Name: "test-component", Enabled: true,
		Config: json.RawMessage(fmt.Sprintf(`{"name":%q}`, name)),
	}, Dependencies{NATSClient: client, Platform: PlatformMeta{Org: "test", Platform: "test-platform"}}, nil)
	if err != nil {
		t.Fatalf("CreateComponent(%s): %v", name, err)
	}
}

// NATS integration coverage for the preserved capability-discovery surface.
func TestRegistry_InitNATS(t *testing.T) {
	ctx := context.Background()
	testClient := natsclient.NewTestClient(t, natsclient.WithJetStream())
	defer testClient.Terminate()
	registry := newCapabilityRegistry(t, testClient.Client, "test-node-1")
	if registry.nodeID != "test-node-1" || registry.remoteCapabilities == nil {
		t.Fatalf("NATS state not initialized: node=%q capabilities=%v", registry.nodeID, registry.remoteCapabilities)
	}
	stream, err := testClient.Client.GetStream(ctx, "COMPONENT_CAPABILITIES")
	if err != nil {
		t.Fatal(err)
	}
	info, err := stream.Info(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if info.Config.Storage != jetstream.MemoryStorage {
		t.Fatalf("storage = %v, want memory", info.Config.Storage)
	}
}

func TestRegistry_PublishCapabilities(t *testing.T) {
	ctx := context.Background()
	testClient := natsclient.NewTestClient(t, natsclient.WithJetStream())
	defer testClient.Terminate()
	registry := newCapabilityRegistry(t, testClient.Client, "test-node-1")
	admitCapabilityComponent(t, registry, testClient.Client, "test-instance")

	stream, err := testClient.Client.GetStream(ctx, "COMPONENT_CAPABILITIES")
	if err != nil {
		t.Fatal(err)
	}
	consumer, err := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		FilterSubjects: []string{"processor.capabilities.test-instance"},
		DeliverPolicy:  jetstream.DeliverLastPolicy,
	})
	if err != nil {
		t.Fatal(err)
	}
	messages, err := consumer.Messages()
	if err != nil {
		t.Fatal(err)
	}
	message, err := messages.Next()
	if err != nil {
		t.Fatal(err)
	}
	var announcement CapabilityAnnouncement
	if err := json.Unmarshal(message.Data(), &announcement); err != nil {
		t.Fatal(err)
	}
	if announcement.InstanceName != "test-instance" || announcement.Component != "test-component" ||
		announcement.Type != "processor" || announcement.Version != "1.0.0" || announcement.NodeID != "test-node-1" ||
		announcement.TTL != 60*time.Second {
		t.Fatalf("unexpected announcement: %+v", announcement)
	}
}

func TestRegistry_Heartbeat(t *testing.T) {
	ctx := context.Background()
	testClient := natsclient.NewTestClient(t, natsclient.WithJetStream())
	defer testClient.Terminate()
	registry := newCapabilityRegistry(t, testClient.Client, "test-node-1")
	admitCapabilityComponent(t, registry, testClient.Client, "test-instance")
	registry.StartHeartbeat(ctx, 100*time.Millisecond)
	defer registry.StopHeartbeat()
	time.Sleep(250 * time.Millisecond)
	stream, err := testClient.Client.GetStream(ctx, "COMPONENT_CAPABILITIES")
	if err != nil {
		t.Fatal(err)
	}
	info, err := stream.Info(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if info.State.Msgs < 1 {
		t.Fatalf("capability message count = %d, want at least one", info.State.Msgs)
	}
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

func admitMockComponent(t *testing.T, registry *Registry, instanceName string, mock *MockComponent) {
	t.Helper()
	factoryName := "factory-" + instanceName
	if err := registry.RegisterFactory(factoryName, &Registration{
		Name: factoryName, Type: mock.componentType,
		Factory: func(json.RawMessage, Dependencies) (Discoverable, error) { return mock, nil },
	}); err != nil {
		t.Fatalf("register %s: %v", factoryName, err)
	}
	_, err := registry.CreateComponent(componentadmission.Access{}, instanceName, types.ComponentConfig{
		Name: factoryName, Type: types.ComponentType(mock.componentType), Enabled: true, Config: json.RawMessage(`{}`),
	}, Dependencies{NATSClient: new(natsclient.Client)}, nil)
	if err != nil {
		t.Fatalf("admit %s: %v", instanceName, err)
	}
}

func TestNewRegistry(t *testing.T) {
	registry := NewRegistry()

	if registry == nil {
		t.Fatal("NewRegistry returned nil")
	}

	if registry.factories == nil {
		t.Error("factories map not initialized")
	}

	if registry.declarations == nil {
		t.Error("declarations map not initialized")
	}

	// Should start empty
	if len(registry.factories) != 0 {
		t.Error("factories should start empty")
	}

	if len(registry.declarations) != 0 {
		t.Error("declarations should start empty")
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
	component, err := registry.CreateComponent(componentadmission.Access{}, "test-instance", config, deps, nil)
	if err != nil {
		t.Fatalf("Failed to create component: %v", err)
	}

	if component == nil {
		t.Fatal("Created component is nil")
	}

	// Verify component was registered as instance
	instances := registry.declarationsSnapshot()
	if len(instances) != 1 {
		t.Errorf("Expected 1 instance, got %d", len(instances))
	}

	if _, ok := registry.declaration("test-instance"); !ok {
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
			_, err := registry.CreateComponent(componentadmission.Access{}, tt.instanceName, componentConfig, deps, nil)

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
	_, err = registry.CreateComponent(componentadmission.Access{}, "test-instance", config, deps, nil)
	if err == nil {
		t.Error("Expected error from failing factory")
	}

	// Verify no instance was registered on failure
	instances := registry.declarationsSnapshot()
	if len(instances) != 0 {
		t.Errorf("Expected no instances after factory failure, got %d", len(instances))
	}
}
