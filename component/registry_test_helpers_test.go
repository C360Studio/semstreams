package component

import (
	"encoding/json"
	"testing"

	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/types"
)

type declarationTestComponent struct {
	inputs      []Port
	outputs     []Port
	inputCalls  int
	outputCalls int
}

func (c *declarationTestComponent) Meta() Metadata             { return Metadata{Name: "declaration-test"} }
func (c *declarationTestComponent) ConfigSchema() ConfigSchema { return ConfigSchema{} }
func (c *declarationTestComponent) Health() HealthStatus       { return HealthStatus{} }
func (c *declarationTestComponent) DataFlow() FlowMetrics      { return FlowMetrics{} }
func (c *declarationTestComponent) InputPorts() []Port {
	c.inputCalls++
	return c.inputs
}
func (c *declarationTestComponent) OutputPorts() []Port {
	c.outputCalls++
	return c.outputs
}

func declarationTestPort(subject string) Port {
	return Port{
		Name: "events", Direction: DirectionOutput, Required: true,
		Config: JetStreamPort{Subjects: []string{subject}},
	}
}

func declarationTestConfig(factory, raw string) types.ComponentConfig {
	return types.ComponentConfig{
		Type: types.ComponentTypeProcessor, Name: factory, Enabled: true, Config: json.RawMessage(raw),
	}
}

// declarePorts is the PortDeclarer for a declarationTestComponent: exactly
// the ports the component reports, so admission parity holds by construction.
func (c *declarationTestComponent) declarePorts(json.RawMessage, string) (PortConfig, error) {
	return PortConfigFrom(c.inputs, c.outputs), nil
}

// mockPorts declares a dependency-free test mock's ports by constructing it:
// test mocks take no dependencies, so the declaration is exactly what the
// factory reports.
func mockPorts(factory Factory) PortDeclarer {
	return func(raw json.RawMessage, _ string) (PortConfig, error) {
		built, err := factory(raw, Dependencies{})
		if err != nil {
			return PortConfig{}, err
		}
		return PortConfigFrom(built.InputPorts(), built.OutputPorts()), nil
	}
}

// noPorts is the PortDeclarer for mocks that report no ports.
func noPorts(json.RawMessage, string) (PortConfig, error) { return PortConfig{}, nil }

func declarationTestDeps() Dependencies {
	return Dependencies{NATSClient: new(natsclient.Client)}
}

func requireNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
