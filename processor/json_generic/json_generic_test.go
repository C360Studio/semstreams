package jsongeneric

import (
	"encoding/json"
	"testing"

	"github.com/c360studio/semstreams/component"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJSONGenericProcessor_Creation(t *testing.T) {
	config := Config{
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{Name: "input", Config: component.NATSPort{Subject: "raw.input"}, Required: true},
			},
			Outputs: []component.PortDefinition{
				{Name: "output", Config: component.NATSPort{Subject: "wrapped.output", Interface: &component.InterfaceContract{Type: "core .json.v1"}}, Required: true},
			},
		},
	}

	rawConfig, err := json.Marshal(config)
	require.NoError(t, err)

	deps := component.Dependencies{
		NATSClient: nil, // Will be nil for creation test
	}

	processor, err := NewProcessor(rawConfig, deps)
	require.NoError(t, err)
	require.NotNil(t, processor)

	// Check metadata
	meta := processor.Meta()
	assert.Equal(t, "json-generic-processor", meta.Name)
	assert.Equal(t, "processor", meta.Type)
	assert.Contains(t, meta.Description, "GenericJSON")
	assert.Contains(t, meta.Description, "core .json.v1")
}

func TestJSONGenericProcessor_DefaultConfig(t *testing.T) {
	config := DefaultConfig()

	assert.NotNil(t, config.Ports)
	assert.Len(t, config.Ports.Inputs, 1)
	assert.Len(t, config.Ports.Outputs, 1)
	input, ok := config.Ports.Inputs[0].Config.(component.NATSPort)
	require.True(t, ok)
	output, ok := config.Ports.Outputs[0].Config.(component.NATSPort)
	require.True(t, ok)
	assert.Equal(t, "raw.>", input.Subject)
	assert.Equal(t, "generic.messages", output.Subject)
	require.NotNil(t, output.Interface)
	assert.Equal(t, "core .json.v1", output.Interface.Type)
}

func TestJSONGenericProcessor_InvalidConfig(t *testing.T) {
	invalidJSON := []byte(`{invalid json}`)

	deps := component.Dependencies{
		NATSClient: nil,
	}

	processor, err := NewProcessor(invalidJSON, deps)
	assert.Error(t, err)
	assert.Nil(t, processor)
	assert.Contains(t, err.Error(), "config unmarshal")
}

func TestJSONGenericProcessor_ConfigSchema(t *testing.T) {
	config := DefaultConfig()
	rawConfig, err := json.Marshal(config)
	require.NoError(t, err)

	deps := component.Dependencies{
		NATSClient: nil,
	}

	processor, err := NewProcessor(rawConfig, deps)
	require.NoError(t, err)

	schema := processor.ConfigSchema()
	assert.NotNil(t, schema)
	assert.NotEmpty(t, schema)
}

func TestJSONGenericProcessor_InputPorts(t *testing.T) {
	config := Config{
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{Name: "input1", Config: component.NATSPort{Subject: "raw.input1"}, Required: true},
				{Name: "input2", Config: component.NATSPort{Subject: "raw.input2"}, Required: false},
			},
			Outputs: []component.PortDefinition{
				{Name: "output", Config: component.NATSPort{Subject: "wrapped.output", Interface: &component.InterfaceContract{Type: "core .json.v1"}}, Required: true},
			},
		},
	}

	rawConfig, err := json.Marshal(config)
	require.NoError(t, err)

	deps := component.Dependencies{
		NATSClient: nil,
	}

	processor, err := NewProcessor(rawConfig, deps)
	require.NoError(t, err)

	inputPorts := processor.InputPorts()
	assert.Len(t, inputPorts, 2)

	// Check first input port
	natsPort1, ok := inputPorts[0].Config.(component.NATSPort)
	assert.True(t, ok, "First input should be NATS port")
	assert.Equal(t, "raw.input1", natsPort1.Subject)

	// Check second input port
	natsPort2, ok := inputPorts[1].Config.(component.NATSPort)
	assert.True(t, ok, "Second input should be NATS port")
	assert.Equal(t, "raw.input2", natsPort2.Subject)
}

func TestJSONGenericProcessor_OutputPorts(t *testing.T) {
	config := Config{
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{Name: "input", Config: component.NATSPort{Subject: "raw.input"}, Required: true},
			},
			Outputs: []component.PortDefinition{
				{Name: "output", Config: component.NATSPort{Subject: "wrapped.output", Interface: &component.InterfaceContract{Type: "core .json.v1"}}, Required: true},
			},
		},
	}

	rawConfig, err := json.Marshal(config)
	require.NoError(t, err)

	deps := component.Dependencies{
		NATSClient: nil,
	}

	processor, err := NewProcessor(rawConfig, deps)
	require.NoError(t, err)

	outputPorts := processor.OutputPorts()
	assert.Len(t, outputPorts, 1)

	natsPort, ok := outputPorts[0].Config.(component.NATSPort)
	assert.True(t, ok, "Output should be NATS port")
	assert.Equal(t, "wrapped.output", natsPort.Subject)
	assert.NotNil(t, natsPort.Interface)
	if natsPort.Interface != nil {
		assert.Equal(t, "core .json.v1", natsPort.Interface.Type)
	}
}

func TestJSONGenericProcessor_Metadata(t *testing.T) {
	config := DefaultConfig()
	rawConfig, err := json.Marshal(config)
	require.NoError(t, err)

	deps := component.Dependencies{
		NATSClient: nil,
	}

	processor, err := NewProcessor(rawConfig, deps)
	require.NoError(t, err)

	meta := processor.Meta()
	assert.Equal(t, "json-generic-processor", meta.Name)
	assert.Equal(t, "processor", meta.Type)
	assert.Equal(t, "0.1.0", meta.Version)
	assert.NotEmpty(t, meta.Description)
}

func TestJSONGenericProcessor_DataFlow(t *testing.T) {
	config := DefaultConfig()
	rawConfig, err := json.Marshal(config)
	require.NoError(t, err)

	deps := component.Dependencies{
		NATSClient: nil,
	}

	processor, err := NewProcessor(rawConfig, deps)
	require.NoError(t, err)

	// DataFlow returns FlowMetrics - just verify it doesn't panic
	_ = processor.DataFlow()
}
