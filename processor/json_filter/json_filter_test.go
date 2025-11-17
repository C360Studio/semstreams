package jsonfilter

import (
	"encoding/json"
	"testing"

	"github.com/c360/semstreams/component"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJSONFilterProcessor_Creation(t *testing.T) {
	config := Config{
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{Name: "input", Type: "nats", Subject: "test.input", Interface: "core .json.v1", Required: true},
			},
			Outputs: []component.PortDefinition{
				{Name: "output", Type: "nats", Subject: "test.output", Interface: "core .json.v1", Required: true},
			},
		},
		Rules: []FilterRule{
			{Field: "value", Operator: "gt", Value: 100},
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
	assert.Equal(t, "json-filter-processor", meta.Name)
	assert.Equal(t, "processor", meta.Type)
	assert.Contains(t, meta.Description, "GenericJSON")
	assert.Contains(t, meta.Description, "core .json.v1")
}

func TestJSONFilterProcessor_DefaultConfig(t *testing.T) {
	config := DefaultConfig()

	assert.NotNil(t, config.Ports)
	assert.Len(t, config.Ports.Inputs, 1)
	assert.Len(t, config.Ports.Outputs, 1)
	assert.Equal(t, "raw.>", config.Ports.Inputs[0].Subject)
	assert.Equal(t, "core .json.v1", config.Ports.Inputs[0].Interface)
	assert.Equal(t, "filtered.messages", config.Ports.Outputs[0].Subject)
	assert.Equal(t, "core .json.v1", config.Ports.Outputs[0].Interface)
}

func TestJSONFilterProcessor_MatchesRule(t *testing.T) {
	processor := &Processor{}

	tests := []struct {
		name     string
		data     map[string]any
		rule     FilterRule
		expected bool
	}{
		{
			name:     "eq operator matches",
			data:     map[string]any{"status": "active"},
			rule:     FilterRule{Field: "status", Operator: "eq", Value: "active"},
			expected: true,
		},
		{
			name:     "eq operator does not match",
			data:     map[string]any{"status": "inactive"},
			rule:     FilterRule{Field: "status", Operator: "eq", Value: "active"},
			expected: false,
		},
		{
			name:     "ne operator matches",
			data:     map[string]any{"status": "inactive"},
			rule:     FilterRule{Field: "status", Operator: "ne", Value: "active"},
			expected: true,
		},
		{
			name:     "gt operator matches",
			data:     map[string]any{"value": 150},
			rule:     FilterRule{Field: "value", Operator: "gt", Value: 100},
			expected: true,
		},
		{
			name:     "gt operator does not match",
			data:     map[string]any{"value": 50},
			rule:     FilterRule{Field: "value", Operator: "gt", Value: 100},
			expected: false,
		},
		{
			name:     "gte operator matches equal",
			data:     map[string]any{"value": 100},
			rule:     FilterRule{Field: "value", Operator: "gte", Value: 100},
			expected: true,
		},
		{
			name:     "lt operator matches",
			data:     map[string]any{"value": 50},
			rule:     FilterRule{Field: "value", Operator: "lt", Value: 100},
			expected: true,
		},
		{
			name:     "lte operator matches equal",
			data:     map[string]any{"value": 100},
			rule:     FilterRule{Field: "value", Operator: "lte", Value: 100},
			expected: true,
		},
		{
			name:     "contains operator matches",
			data:     map[string]any{"message": "hello world"},
			rule:     FilterRule{Field: "message", Operator: "contains", Value: "world"},
			expected: true,
		},
		{
			name:     "contains operator does not match",
			data:     map[string]any{"message": "hello there"},
			rule:     FilterRule{Field: "message", Operator: "contains", Value: "world"},
			expected: false,
		},
		{
			name:     "missing field does not match",
			data:     map[string]any{"other": "value"},
			rule:     FilterRule{Field: "status", Operator: "eq", Value: "active"},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := processor.matchesRule(tt.data, tt.rule)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestJSONFilterProcessor_MatchesRules(t *testing.T) {
	tests := []struct {
		name     string
		data     map[string]any
		rules    []FilterRule
		expected bool
	}{
		{
			name:     "no rules passes everything",
			data:     map[string]any{"value": 50},
			rules:    []FilterRule{},
			expected: true,
		},
		{
			name: "single rule matches",
			data: map[string]any{"value": 150},
			rules: []FilterRule{
				{Field: "value", Operator: "gt", Value: 100},
			},
			expected: true,
		},
		{
			name: "multiple rules all match",
			data: map[string]any{"value": 150, "status": "active"},
			rules: []FilterRule{
				{Field: "value", Operator: "gt", Value: 100},
				{Field: "status", Operator: "eq", Value: "active"},
			},
			expected: true,
		},
		{
			name: "multiple rules one fails",
			data: map[string]any{"value": 150, "status": "inactive"},
			rules: []FilterRule{
				{Field: "value", Operator: "gt", Value: 100},
				{Field: "status", Operator: "eq", Value: "active"},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			processor := &Processor{rules: tt.rules}
			result := processor.matchesRules(tt.data)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestJSONFilterProcessor_Lifecycle(t *testing.T) {
	config := Config{
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{Name: "input", Type: "nats", Subject: "test.input", Interface: "core .json.v1", Required: true},
			},
			Outputs: []component.PortDefinition{
				{Name: "output", Type: "nats", Subject: "test.output", Interface: "core .json.v1", Required: true},
			},
		},
		Rules: []FilterRule{},
	}

	rawConfig, err := json.Marshal(config)
	require.NoError(t, err)

	// For unit testing, we'll just test creation and interfaces
	// Integration tests will test with real NATS
	deps := component.Dependencies{
		NATSClient: nil, // Will fail if Start() is called, which is expected
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

	// Note: Start() would fail without a real NATS client, tested in integration tests
}

func TestCompareNumbers(t *testing.T) {
	tests := []struct {
		name     string
		a        any
		b        any
		expected int
	}{
		{"int less than", 5, 10, -1},
		{"int greater than", 10, 5, 1},
		{"int equal", 5, 5, 0},
		{"float less than", 5.5, 10.5, -1},
		{"float greater than", 10.5, 5.5, 1},
		{"float equal", 5.5, 5.5, 0},
		{"mixed int and float", 5, 5.0, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := compareNumbers(tt.a, tt.b)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestContains(t *testing.T) {
	tests := []struct {
		name     string
		s        string
		substr   string
		expected bool
	}{
		{"contains substring", "hello world", "world", true},
		{"does not contain", "hello there", "world", false},
		{"empty substring", "hello", "", true},
		{"exact match", "hello", "hello", true},
		{"case sensitive", "Hello", "hello", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := contains(tt.s, tt.substr)
			assert.Equal(t, tt.expected, result)
		})
	}
}
