package flowengine

import (
	"encoding/json"
	"io"
	"log/slog"
	"testing"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/flowstore"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/types"
	"github.com/stretchr/testify/require"
)

type validationTestComponent struct {
	value string
}

func (c *validationTestComponent) Meta() component.Metadata {
	return component.Metadata{Name: "validation-test", Type: "processor"}
}
func (c *validationTestComponent) InputPorts() []component.Port  { return nil }
func (c *validationTestComponent) OutputPorts() []component.Port { return nil }
func (c *validationTestComponent) ConfigSchema() component.ConfigSchema {
	return component.ConfigSchema{}
}
func (c *validationTestComponent) Health() component.HealthStatus  { return component.HealthStatus{} }
func (c *validationTestComponent) DataFlow() component.FlowMetrics { return component.FlowMetrics{} }

func TestValidatorUsesCopiedRealRegistrationAndActualNodeConfig(t *testing.T) {
	registry := component.NewRegistry()
	require.NoError(t, registry.RegisterWithConfig(component.RegistrationConfig{
		Name: "real-factory", Type: "processor", Protocol: "nats", Domain: "test",
		Description: "real registration", Version: "1.2.3", Dependencies: []string{"model-registry"},
		Factory: func(raw json.RawMessage, _ component.Dependencies) (component.Discoverable, error) {
			var cfg struct {
				Value string `json:"value"`
			}
			if err := json.Unmarshal(raw, &cfg); err != nil {
				return nil, err
			}
			return &validationTestComponent{value: cfg.Value}, nil
		},
	}))
	validator := NewValidator(registry, &natsclient.Client{},
		slog.New(slog.NewTextHandler(io.Discard, nil)))

	validationRegistry, registrations, err := validator.newValidationRegistry()
	require.NoError(t, err)
	require.Contains(t, registrations, "real-factory")
	require.Equal(t, []string{"real-factory"}, validationRegistry.ListComponentTypes())
	copied := validationRegistry.ListFactories()["real-factory"]
	require.Equal(t, "nats", copied.Protocol)
	require.Equal(t, "test", copied.Domain)
	require.Equal(t, "real registration", copied.Description)
	require.Equal(t, "1.2.3", copied.Version)
	require.Equal(t, []string{"model-registry"}, copied.Dependencies)

	graph, issues := validator.buildFlowGraph(&flowstore.Flow{Nodes: []flowstore.FlowNode{
		{ID: "first", Name: "First", Component: "real-factory", Type: types.ComponentTypeProcessor,
			Config: map[string]any{"value": "one"}},
		{ID: "second", Name: "Second", Component: "real-factory", Type: types.ComponentTypeProcessor,
			Config: map[string]any{"value": "two"}},
	}})
	require.Empty(t, issues)
	nodes := graph.GetNodes()
	require.Contains(t, nodes, "first")
	require.Contains(t, nodes, "second")
}
