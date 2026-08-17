package component

import (
	"encoding/json"
	"testing"

	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/types"
)

type generationTestComponent struct {
	inputs      []Port
	outputs     []Port
	inputCalls  int
	outputCalls int
}

func (c *generationTestComponent) Meta() Metadata             { return Metadata{Name: "generation-test"} }
func (c *generationTestComponent) ConfigSchema() ConfigSchema { return ConfigSchema{} }
func (c *generationTestComponent) Health() HealthStatus       { return HealthStatus{} }
func (c *generationTestComponent) DataFlow() FlowMetrics      { return FlowMetrics{} }
func (c *generationTestComponent) InputPorts() []Port {
	c.inputCalls++
	return c.inputs
}
func (c *generationTestComponent) OutputPorts() []Port {
	c.outputCalls++
	return c.outputs
}

func generationTestPort(subject string) Port {
	return Port{
		Name: "events", Direction: DirectionOutput, Required: true,
		Config: JetStreamPort{Subjects: []string{subject}},
	}
}

func generationTestConfig(factory, raw string) types.ComponentConfig {
	return types.ComponentConfig{
		Type: types.ComponentTypeProcessor, Name: factory, Enabled: true, Config: json.RawMessage(raw),
	}
}

func generationTestDeps() Dependencies {
	return Dependencies{NATSClient: new(natsclient.Client)}
}

func requireNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
