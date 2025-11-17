package jsonmapprocessor

import (
	"encoding/json"
	"testing"

	"github.com/c360/semstreams/component"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJSONMapProcessor_Creation(t *testing.T) {
	config := Config{
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{Name: "input", Type: "nats", Subject: "test.input", Interface: "core .json.v1", Required: true},
			},
			Outputs: []component.PortDefinition{
				{Name: "output", Type: "nats", Subject: "test.output", Interface: "core .json.v1", Required: true},
			},
		},
		Mappings: []FieldMapping{
			{SourceField: "old_name", TargetField: "new_name", Transform: "copy"},
		},
	}

	rawConfig, err := json.Marshal(config)
	require.NoError(t, err)

	deps := component.Dependencies{
		NATSClient: nil,
	}

	processor, err := NewProcessor(rawConfig, deps)
	require.NoError(t, err)
	require.NotNil(t, processor)

	// Check metadata
	meta := processor.Meta()
	assert.Equal(t, "json-map-processor", meta.Name)
	assert.Equal(t, "processor", meta.Type)
	assert.Contains(t, meta.Description, "GenericJSON")
	assert.Contains(t, meta.Description, "core .json.v1")
}

func TestJSONMapProcessor_DefaultConfig(t *testing.T) {
	config := DefaultConfig()

	assert.NotNil(t, config.Ports)
	assert.Len(t, config.Ports.Inputs, 1)
	assert.Len(t, config.Ports.Outputs, 1)
	assert.Equal(t, "raw.>", config.Ports.Inputs[0].Subject)
	assert.Equal(t, "core .json.v1", config.Ports.Inputs[0].Interface)
	assert.Equal(t, "mapped.messages", config.Ports.Outputs[0].Subject)
	assert.Equal(t, "core .json.v1", config.Ports.Outputs[0].Interface)
}

func TestJSONMapProcessor_TransformMessage(t *testing.T) {
	tests := []struct {
		name         string
		input        map[string]any
		mappings     []FieldMapping
		addFields    map[string]any
		removeFields []string
		expected     map[string]any
	}{
		{
			name:  "simple field rename",
			input: map[string]any{"old_field": "value"},
			mappings: []FieldMapping{
				{SourceField: "old_field", TargetField: "new_field", Transform: "copy"},
			},
			addFields:    nil,
			removeFields: nil,
			expected:     map[string]any{"new_field": "value"},
		},
		{
			name:         "add static fields",
			input:        map[string]any{"existing": "value"},
			mappings:     nil,
			addFields:    map[string]any{"added": "static"},
			removeFields: nil,
			expected:     map[string]any{"existing": "value", "added": "static"},
		},
		{
			name:         "remove fields",
			input:        map[string]any{"keep": "yes", "remove": "no"},
			mappings:     nil,
			addFields:    nil,
			removeFields: []string{"remove"},
			expected:     map[string]any{"keep": "yes"},
		},
		{
			name:  "uppercase transform",
			input: map[string]any{"text": "hello"},
			mappings: []FieldMapping{
				{SourceField: "text", TargetField: "TEXT", Transform: "uppercase"},
			},
			addFields:    nil,
			removeFields: nil,
			expected:     map[string]any{"TEXT": "HELLO"},
		},
		{
			name:  "lowercase transform",
			input: map[string]any{"text": "HELLO"},
			mappings: []FieldMapping{
				{SourceField: "text", TargetField: "text_lower", Transform: "lowercase"},
			},
			addFields:    nil,
			removeFields: nil,
			expected:     map[string]any{"text_lower": "hello"},
		},
		{
			name:  "trim transform",
			input: map[string]any{"text": "  hello  "},
			mappings: []FieldMapping{
				{SourceField: "text", TargetField: "text_trimmed", Transform: "trim"},
			},
			addFields:    nil,
			removeFields: nil,
			expected:     map[string]any{"text_trimmed": "hello"},
		},
		{
			name:  "combined transformations",
			input: map[string]any{"a": "value_a", "b": "value_b", "c": "remove_me"},
			mappings: []FieldMapping{
				{SourceField: "a", TargetField: "renamed_a", Transform: "copy"},
			},
			addFields:    map[string]any{"static": "added"},
			removeFields: []string{"c"},
			expected:     map[string]any{"renamed_a": "value_a", "b": "value_b", "static": "added"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			removeFieldsSet := make(map[string]bool)
			for _, field := range tt.removeFields {
				removeFieldsSet[field] = true
			}

			processor := &Processor{
				mappings:     tt.mappings,
				addFields:    tt.addFields,
				removeFields: removeFieldsSet,
			}

			result := processor.transformMessage(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestJSONMapProcessor_ApplyTransform(t *testing.T) {
	processor := &Processor{}

	tests := []struct {
		name      string
		value     any
		transform string
		expected  any
	}{
		{"copy string", "hello", "copy", "hello"},
		{"uppercase", "hello", "uppercase", "HELLO"},
		{"lowercase", "HELLO", "lowercase", "hello"},
		{"trim", "  hello  ", "trim", "hello"},
		{"unknown transform", "hello", "unknown", "hello"},
		{"non-string value", 123, "uppercase", 123},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := processor.applyTransform(tt.value, tt.transform)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestJSONMapProcessor_Lifecycle(t *testing.T) {
	config := Config{
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{Name: "input", Type: "nats", Subject: "test.input", Interface: "core .json.v1", Required: true},
			},
			Outputs: []component.PortDefinition{
				{Name: "output", Type: "nats", Subject: "test.output", Interface: "core .json.v1", Required: true},
			},
		},
		Mappings: []FieldMapping{},
	}

	rawConfig, err := json.Marshal(config)
	require.NoError(t, err)

	deps := component.Dependencies{
		NATSClient: nil,
	}

	processor, err := NewProcessor(rawConfig, deps)
	require.NoError(t, err)

	lifecycleComp, ok := processor.(component.LifecycleComponent)
	require.True(t, ok)

	// Initialize should work without NATS
	err = lifecycleComp.Initialize()
	assert.NoError(t, err)

	// Health check (without starting)
	health := processor.Health()
	assert.False(t, health.Healthy) // Not started yet

	// Check port definitions have Interface contract
	inputPorts := processor.InputPorts()
	require.Len(t, inputPorts, 1)
	natsPort, ok := inputPorts[0].Config.(component.NATSPort)
	require.True(t, ok)
	require.NotNil(t, natsPort.Interface)
	assert.Equal(t, "core .json.v1", natsPort.Interface.Type)

	outputPorts := processor.OutputPorts()
	require.Len(t, outputPorts, 1)
	outPort, ok := outputPorts[0].Config.(component.NATSPort)
	require.True(t, ok)
	require.NotNil(t, outPort.Interface)
	assert.Equal(t, "core .json.v1", outPort.Interface.Type)
}

func TestStringHelpers(t *testing.T) {
	assert.Equal(t, "HELLO", toUpperCase("hello"))
	assert.Equal(t, "HELLO", toUpperCase("HELLO"))
	assert.Equal(t, "HELLO123", toUpperCase("hello123"))

	assert.Equal(t, "hello", toLowerCase("HELLO"))
	assert.Equal(t, "hello", toLowerCase("hello"))
	assert.Equal(t, "hello123", toLowerCase("HELLO123"))

	assert.Equal(t, "hello", trimSpaces("  hello  "))
	assert.Equal(t, "hello", trimSpaces("hello"))
	assert.Equal(t, "hello world", trimSpaces("  hello world  "))
	assert.Equal(t, "", trimSpaces("   "))
}
