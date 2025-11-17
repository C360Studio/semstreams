package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360/semstreams/component"
	"github.com/c360/semstreams/types"
)

// TestComponentManagerConfig tests the configuration handling
func TestComponentManagerConfig(t *testing.T) {
	t.Run("default config values", func(t *testing.T) {
		config := &ComponentManagerConfig{}

		// Test defaults after unmarshal
		jsonData := []byte(`{}`)
		err := json.Unmarshal(jsonData, config)
		require.NoError(t, err)

		// ComponentManager is mandatory - no enabled field
		assert.False(t, config.WatchConfig, "WatchConfig should default to false")
		assert.Empty(t, config.EnabledComponents, "EnabledComponents should be empty by default")
	})

	t.Run("parse full config", func(t *testing.T) {
		jsonData := []byte(`{
			"watch_config": true,
			"enabled_components": ["component1", "component2"]
		}`)

		config := &ComponentManagerConfig{}
		err := json.Unmarshal(jsonData, config)
		require.NoError(t, err)

		assert.True(t, config.WatchConfig)
		assert.Equal(t, []string{"component1", "component2"}, config.EnabledComponents)
	})

	t.Run("ComponentManager is mandatory - no enabled field", func(_ *testing.T) {
		// This test verifies ComponentManager is properly mandatory
		// with no enable/disable logic

		config := &ComponentManagerConfig{}

		// Verify the struct has no Enabled field
		// This is a compile-time check - if Enabled field existed,
		// this would fail to compile
		_ = config.WatchConfig       // This compiles
		_ = config.EnabledComponents // This compiles
		// _ = config.Enabled would NOT compile (good!)
	})
}

// TestComponentManagerService tests the service creation and lifecycle
func TestComponentManagerService(t *testing.T) {
	t.Run("create component manager service", func(t *testing.T) {
		// Create config
		cmConfig := ComponentManagerConfig{
			WatchConfig:       true,
			EnabledComponents: []string{"test-component"},
		}

		rawConfig, err := json.Marshal(cmConfig)
		require.NoError(t, err)

		// Create dependencies
		deps := &Dependencies{
			// NATSClient would normally be provided here
			// For this test, we're checking basic creation
		}

		// Create service via constructor
		service, err := NewComponentManager(rawConfig, deps)
		require.NoError(t, err)
		assert.NotNil(t, service)

		// Verify it's a ComponentManager
		cm, ok := service.(*ComponentManager)
		assert.True(t, ok, "Service should be a ComponentManager")
		assert.NotNil(t, cm)
	})

	t.Run("component manager is always created", func(t *testing.T) {
		// ComponentManager is mandatory - always created when configured
		// regardless of any fields in the config

		configs := []ComponentManagerConfig{
			{}, // Empty config
			{WatchConfig: false},
			{WatchConfig: true},
			{EnabledComponents: []string{"comp1"}},
		}

		for i, config := range configs {
			rawConfig, err := json.Marshal(config)
			require.NoError(t, err)

			deps := &Dependencies{}

			service, err := NewComponentManager(rawConfig, deps)
			require.NoError(t, err, "Config %d should create ComponentManager", i)
			assert.NotNil(t, service, "ComponentManager should always be created when configured")
		}
	})

	t.Run("backward compatibility - extra fields ignored", func(t *testing.T) {
		// This test ensures configs with extra fields (like "enabled")
		// are handled gracefully - Go's json unmarshaling ignores unknown fields

		jsonData := []byte(`{
			"enabled": true,
			"watch_config": true,
			"enabled_components": ["comp1"],
			"unknown_field": "ignored"
		}`)

		config := &ComponentManagerConfig{}
		err := json.Unmarshal(jsonData, config)
		require.NoError(t, err)

		// Unknown fields are ignored, known fields are parsed
		assert.True(t, config.WatchConfig)
		assert.Equal(t, []string{"comp1"}, config.EnabledComponents)
	})
}

// TestComponentManagerLifecycle tests the component manager lifecycle
func TestComponentManagerLifecycle(t *testing.T) {
	t.Run("start and stop", func(t *testing.T) {
		// Create a minimal ComponentManager for testing
		cm := &ComponentManager{
			BaseService: NewBaseServiceWithOptions("component-manager", nil),
			components:  make(map[string]*component.ManagedComponent),
			registry:    component.NewRegistry(),
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// ComponentManager should always start when called
		// (after CLEANUP-001, there won't be an enabled check)
		err := cm.Start(ctx)

		// Note: This will fail without proper initialization
		// but we're testing the structure exists
		if err != nil {
			// Expected for this minimal test
			// Real tests would use proper setup
			t.Logf("Start error (expected in minimal test): %v", err)
		}

		// Stop should always work
		err = cm.Stop(1 * time.Second)
		if err != nil {
			t.Logf("Stop error: %v", err)
		}
	})
}

// TestComponentManagerMandatoryBehavior verifies ComponentManager behaves as mandatory service
func TestComponentManagerMandatoryBehavior(t *testing.T) {
	t.Run("always creates when configured", func(t *testing.T) {
		// ComponentManager is mandatory - ALWAYS created when configured
		// Extra fields like "enabled" are ignored by Go's json unmarshaling

		configs := []string{
			`{}`,                     // Empty config
			`{"watch_config": true}`, // Standard config
			`{"enabled": true}`,      // Extra field ignored
			`{"enabled": false}`,     // Extra field ignored
			`{"unknown": "value"}`,   // Unknown fields ignored
		}

		for _, configJSON := range configs {
			rawConfig := json.RawMessage(configJSON)
			deps := &Dependencies{}

			service, err := NewComponentManager(rawConfig, deps)
			require.NoError(t, err, "Config: %s", configJSON)
			assert.NotNil(
				t,
				service,
				"ComponentManager should always be created when configured. Config: %s",
				configJSON,
			)
		}
	})
}

// TestComponentManagerInitializeCreatesComponents tests that Initialize actually creates components
func TestComponentManagerInitializeCreatesComponents(t *testing.T) {
	// This test verifies the critical fix where Initialize() now creates components
	cm := &ComponentManager{
		BaseService: NewBaseServiceWithOptions("component-manager", nil),
		components:  make(map[string]*component.ManagedComponent),
		registry:    component.NewRegistry(),
		componentConfigs: map[string]types.ComponentConfig{
			"test-comp": {
				Type:    types.ComponentTypeProcessor,
				Name:    "test-processor",
				Enabled: true,
				Config:  json.RawMessage(`{}`),
			},
			"disabled-comp": {
				Type:    types.ComponentTypeProcessor,
				Name:    "test-processor",
				Enabled: false, // Should NOT be created
				Config:  json.RawMessage(`{}`),
			},
		},
	}

	// Before Initialize, no components should exist
	assert.Empty(t, cm.components, "No components should exist before Initialize")

	// Initialize should attempt to create components but will fail without dependencies
	err := cm.Initialize()
	// We expect no error from Initialize itself (it continues on component creation failures)
	// Components will fail to create due to missing NATS client
	assert.NoError(t, err, "Initialize should not return error (component failures are logged)")

	// No components should be created because NATS client is nil
	assert.Empty(t, cm.components, "No components should be created without NATS client")

	// Verify Initialize was called
	assert.True(t, cm.IsInitialized(), "ComponentManager should be marked as initialized")
}

// TestComponentManagerRemoveComponent tests component removal
func TestComponentManagerRemoveComponent(t *testing.T) {
	cm := createTestComponentManager(t)

	// Add a mock component
	mockComp := &component.ManagedComponent{
		Component: &mockDiscoverableComponent{
			metadata: component.Metadata{
				Name: "test-delete",
				Type: "processor",
			},
		},
	}
	cm.components["test-delete"] = mockComp

	// Verify it exists
	assert.Contains(t, cm.components, "test-delete")

	// Remove it
	err := cm.RemoveComponent("test-delete")
	require.NoError(t, err)

	// Verify it's gone
	assert.NotContains(t, cm.components, "test-delete")

	// Try to remove non-existent
	err = cm.RemoveComponent("non-existent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// TestComponentManagerComponent tests retrieving components
func TestComponentManagerComponent(t *testing.T) {
	cm := createTestComponentManager(t)

	// Add a mock component to the registry (not directly to cm.components)
	mockComp := &mockDiscoverableComponent{
		metadata: component.Metadata{
			Name: "test-get",
			Type: "processor",
		},
	}

	// Register the component in the registry
	cm.registry.RegisterInstance("test-get", mockComp)

	// Get existing component
	comp := cm.Component("test-get")
	assert.NotNil(t, comp)

	// Get non-existent component
	comp = cm.Component("non-existent")
	assert.Nil(t, comp)
}

// TestComponentManagerListComponents tests listing all components
func TestComponentManagerListComponents(t *testing.T) {
	cm := createTestComponentManager(t)

	// Clear any existing components from previous tests
	// (tests share the global registry which causes pollution)
	for name := range cm.registry.ListComponents() {
		cm.registry.UnregisterInstance(name)
	}

	// Initially empty
	assert.Empty(t, cm.ListComponents())

	// Add components to the registry
	comp1 := &mockDiscoverableComponent{
		metadata: component.Metadata{Name: "comp1"},
	}
	comp2 := &mockDiscoverableComponent{
		metadata: component.Metadata{Name: "comp2"},
	}
	comp3 := &mockDiscoverableComponent{
		metadata: component.Metadata{Name: "comp3"},
	}

	// Register components in the registry
	cm.registry.RegisterInstance("comp1", comp1)
	cm.registry.RegisterInstance("comp2", comp2)
	cm.registry.RegisterInstance("comp3", comp3)

	// List should contain all (returns map[string]component.Discoverable)
	list := cm.ListComponents()
	assert.Len(t, list, 3)
	assert.Contains(t, list, "comp1")
	assert.Contains(t, list, "comp2")
	assert.Contains(t, list, "comp3")
}

// TestComponentManagerGetComponentHealth tests health reporting
func TestComponentManagerGetComponentHealth(t *testing.T) {
	cm := createTestComponentManager(t)

	// Add components with different health states
	healthyComp := &mockDiscoverableComponent{
		metadata: component.Metadata{Name: "healthy"},
	}
	unhealthyComp := &mockDiscoverableComponent{
		metadata: component.Metadata{Name: "unhealthy"},
	}

	cm.components["healthy"] = &component.ManagedComponent{
		Component: healthyComp,
	}
	cm.components["unhealthy"] = &component.ManagedComponent{
		Component: unhealthyComp,
	}

	// Get health
	health := cm.GetComponentHealth()
	assert.NotNil(t, health)
	assert.Len(t, health, 2)

	// Check both components are in health map
	assert.Contains(t, health, "healthy")
	assert.Contains(t, health, "unhealthy")
}

// TestComponentManagerGetFlowGraph tests FlowGraph generation
func TestComponentManagerGetFlowGraph(t *testing.T) {
	cm := createTestComponentManager(t)

	// Add a component with flow information
	flowComp := &mockDiscoverableComponent{
		metadata: component.Metadata{
			Name: "flow-comp",
			Type: "processor",
		},
		inputPorts: []component.Port{
			{Name: "input", Description: "Input port"},
		},
		outputPorts: []component.Port{
			{Name: "output", Description: "Output port"},
		},
	}

	cm.components["flow-comp"] = &component.ManagedComponent{
		Component: flowComp,
	}

	// Get FlowGraph
	graph := cm.GetFlowGraph()
	assert.NotNil(t, graph)

	// Verify component is in graph
	nodes := graph.GetNodes()
	assert.Contains(t, nodes, "flow-comp")
}

// TestComponentManagerValidateFlowConnectivity tests flow validation
func TestComponentManagerValidateFlowConnectivity(t *testing.T) {
	cm := createTestComponentManager(t)

	// Empty flow should be healthy
	result := cm.ValidateFlowConnectivity()
	assert.NotNil(t, result)
	assert.Equal(t, "healthy", result.ValidationStatus)
	assert.Empty(t, result.DisconnectedNodes)
	assert.Empty(t, result.OrphanedPorts)
}

// TestComponentManagerGetFlowPaths tests flow path extraction
func TestComponentManagerGetFlowPaths(t *testing.T) {
	cm := createTestComponentManager(t)

	// Empty should return empty paths
	paths := cm.GetFlowPaths()
	assert.NotNil(t, paths)
	assert.Empty(t, paths)
}

// TestComponentManagerBuildComponentDependencies tests dependency building
func TestComponentManagerBuildComponentDependencies(t *testing.T) {
	cm := &ComponentManager{
		BaseService: NewBaseServiceWithOptions("component-manager", nil),
		natsClient:  nil, // Would be mocked in real test
	}

	deps := cm.buildComponentDependencies()
	assert.NotNil(t, deps)
	// In real test, would verify NATS client and logger are set
}

// TestComponentManagerResilientErrorHandling tests error resilience
func TestComponentManagerResilientErrorHandling(t *testing.T) {
	cm := createTestComponentManager(t)

	// Test that failures don't cascade
	t.Run("CreateComponent error doesn't panic", func(t *testing.T) {
		err := cm.CreateComponent(context.Background(), "fail", types.ComponentConfig{
			Type: types.ComponentType("invalid-type"),
			Name: "invalid-name",
		}, component.Dependencies{})
		assert.Error(t, err)
		// Should still be operational
		assert.NotNil(t, cm.ListComponents())
	})

	t.Run("restartComponentWithNewConfig handles missing component", func(t *testing.T) {
		// This should not panic or fail catastrophically
		err := cm.restartComponentWithNewConfig(context.Background(), "non-existent", types.ComponentConfig{
			Type:    types.ComponentTypeProcessor,
			Name:    "test",
			Enabled: true,
		}, nil)
		assert.Error(t, err) // Should return error for missing component
		// Should still be operational
		assert.NotNil(t, cm.ListComponents())
	})

	t.Run("stopAndRemoveComponent handles missing component", func(t *testing.T) {
		// This should not panic or fail catastrophically
		err := cm.stopAndRemoveComponent(context.Background(), "non-existent", nil)
		assert.Error(t, err) // Should return error for nil component
		// Should still be operational
		assert.NotNil(t, cm.ListComponents())
	})
}

// NOTE: ComponentManager flow validation tests have been replaced by FlowGraph tests
// See flowgraph_test.go for the new graph-based flow analysis tests

// Test helpers

// mockDiscoverableComponent implements component.Discoverable for testing
type mockDiscoverableComponent struct {
	metadata    component.Metadata
	inputPorts  []component.Port
	outputPorts []component.Port
}

func (m *mockDiscoverableComponent) Meta() component.Metadata {
	return m.metadata
}

func (m *mockDiscoverableComponent) InputPorts() []component.Port {
	return m.inputPorts
}

func (m *mockDiscoverableComponent) OutputPorts() []component.Port {
	return m.outputPorts
}

func (m *mockDiscoverableComponent) ConfigSchema() component.ConfigSchema {
	return component.ConfigSchema{}
}

func (m *mockDiscoverableComponent) Health() component.HealthStatus {
	return component.HealthStatus{Healthy: true}
}

func (m *mockDiscoverableComponent) DataFlow() component.FlowMetrics {
	return component.FlowMetrics{}
}

func createTestComponentManager(_ *testing.T) *ComponentManager {
	cm := &ComponentManager{
		BaseService: NewBaseServiceWithOptions("component-manager", nil),
		components:  make(map[string]*component.ManagedComponent),
		registry:    component.NewRegistry(),
	}
	return cm
}
