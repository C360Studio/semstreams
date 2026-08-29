package iotsensor

import (
	"encoding/json"
	"testing"

	"github.com/c360studio/semstreams/component"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewComponent_ValidConfig(t *testing.T) {
	config := ComponentConfig{
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{
					Name:        "nats_input",
					Config:      component.NATSPort{Subject: "raw.sensor.>"},
					Required:    true,
					Description: "NATS subjects with sensor JSON data",
				},
			},
			Outputs: []component.PortDefinition{
				{
					Name:        "nats_output",
					Config:      component.NATSPort{Subject: "events.graph.entity.sensor", Interface: &component.InterfaceContract{Type: "domain.iot.sensor.v1"}},
					Required:    true,
					Description: "NATS subject for Graphable sensor readings",
				},
			},
		},
	}

	rawConfig, err := json.Marshal(config)
	require.NoError(t, err)

	deps := component.Dependencies{
		NATSClient: nil, // Will be nil for creation test
	}

	comp, err := NewComponent(rawConfig, deps)
	require.NoError(t, err)
	require.NotNil(t, comp)

	// Verify metadata (NewComponent returns component.Discoverable)
	meta := comp.Meta()
	assert.Equal(t, "iot-sensor-processor", meta.Name)
	assert.Equal(t, "processor", meta.Type)
	assert.Contains(t, meta.Description, "sensor")
	assert.Equal(t, "0.1.0", meta.Version)
}

// The org_id / platform required-key tests that used to live here were
// deleted with the keys themselves (ADR-102 d2). Their replacement is
// TestRetiredAuthorityKeysAreRefused in retired_authority_keys_test.go, which
// proves the stronger fact: the keys are not merely optional now, they are
// refused, so an operator carrying them forward cannot mint under a silently
// different authority.

func TestComponent_InputPorts(t *testing.T) {
	config := ComponentConfig{
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{Name: "input1", Config: component.NATSPort{Subject: "raw.sensor.1"}, Required: true},
				{Name: "input2", Config: component.NATSPort{Subject: "raw.sensor.2"}, Required: false},
			},
			Outputs: []component.PortDefinition{
				{Name: "output", Config: component.NATSPort{Subject: "events.sensor", Interface: &component.InterfaceContract{Type: "domain.iot.sensor.v1"}}, Required: true},
			},
		},
	}

	rawConfig, err := json.Marshal(config)
	require.NoError(t, err)

	deps := component.Dependencies{
		NATSClient: nil,
	}

	comp, err := NewComponent(rawConfig, deps)
	require.NoError(t, err)

	inputPorts := comp.InputPorts()
	assert.Len(t, inputPorts, 2)

	// Check first input port
	natsPort1, ok := inputPorts[0].Config.(component.NATSPort)
	assert.True(t, ok, "First input should be NATS port")
	assert.Equal(t, "raw.sensor.1", natsPort1.Subject)

	// Check second input port
	natsPort2, ok := inputPorts[1].Config.(component.NATSPort)
	assert.True(t, ok, "Second input should be NATS port")
	assert.Equal(t, "raw.sensor.2", natsPort2.Subject)
}

func TestComponent_OutputPorts(t *testing.T) {
	config := ComponentConfig{
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{Name: "input", Config: component.NATSPort{Subject: "raw.sensor.>"}, Required: true},
			},
			Outputs: []component.PortDefinition{
				{Name: "output", Config: component.NATSPort{Subject: "events.sensor", Interface: &component.InterfaceContract{Type: "domain.iot.sensor.v1"}}, Required: true},
			},
		},
	}

	rawConfig, err := json.Marshal(config)
	require.NoError(t, err)

	deps := component.Dependencies{
		NATSClient: nil,
	}

	comp, err := NewComponent(rawConfig, deps)
	require.NoError(t, err)

	outputPorts := comp.OutputPorts()
	assert.Len(t, outputPorts, 1)

	natsPort, ok := outputPorts[0].Config.(component.NATSPort)
	assert.True(t, ok, "Output should be NATS port")
	assert.Equal(t, "events.sensor", natsPort.Subject)
	assert.NotNil(t, natsPort.Interface)
	if natsPort.Interface != nil {
		assert.Equal(t, "domain.iot.sensor.v1", natsPort.Interface.Type)
	}
}

func TestRegister(t *testing.T) {
	registry := component.NewRegistry()

	err := Register(registry)
	require.NoError(t, err)

	// Verify component was registered
	factory, ok := registry.GetFactory("iot_sensor")
	require.True(t, ok, "Expected component to be registered")
	require.NotNil(t, factory, "Expected component factory to be registered")
}
