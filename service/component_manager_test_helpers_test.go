package service

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/internal/componentadmission"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/types"
	"github.com/stretchr/testify/require"
)

type baseDiscoverable struct{ name string }

func (b baseDiscoverable) Meta() component.Metadata {
	return component.Metadata{Name: b.name, Type: "processor"}
}
func (b baseDiscoverable) InputPorts() []component.Port         { return nil }
func (b baseDiscoverable) OutputPorts() []component.Port        { return nil }
func (b baseDiscoverable) ConfigSchema() component.ConfigSchema { return component.ConfigSchema{} }
func (b baseDiscoverable) Health() component.HealthStatus {
	return component.HealthStatus{Healthy: true}
}
func (b baseDiscoverable) DataFlow() component.FlowMetrics { return component.FlowMetrics{} }

type mockDiscoverableComponent struct {
	metadata    component.Metadata
	inputPorts  []component.Port
	outputPorts []component.Port
}

func (m *mockDiscoverableComponent) Meta() component.Metadata      { return m.metadata }
func (m *mockDiscoverableComponent) InputPorts() []component.Port  { return m.inputPorts }
func (m *mockDiscoverableComponent) OutputPorts() []component.Port { return m.outputPorts }
func (m *mockDiscoverableComponent) ConfigSchema() component.ConfigSchema {
	return component.ConfigSchema{}
}
func (m *mockDiscoverableComponent) Health() component.HealthStatus {
	return component.HealthStatus{Healthy: true}
}
func (m *mockDiscoverableComponent) DataFlow() component.FlowMetrics { return component.FlowMetrics{} }

func admitTestRegistryComponent(
	t *testing.T, registry *component.Registry, name string, instance component.Discoverable,
) {
	t.Helper()
	require.NoError(t, registry.RegisterWithConfig(component.RegistrationConfig{
		Name: name, Type: "processor",
		Factory: func(json.RawMessage, component.Dependencies) (component.Discoverable, error) {
			return instance, nil
		},
	}))
	_, err := registry.CreateComponent(componentadmission.Access{}, name, types.ComponentConfig{
		Name: name, Type: types.ComponentTypeProcessor, Enabled: true, Config: json.RawMessage(`{}`),
	}, component.Dependencies{NATSClient: new(natsclient.Client)}, nil)
	require.NoError(t, err)
}

func waitForSignal(t *testing.T, signal <-chan struct{}, description string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", description)
	}
}
