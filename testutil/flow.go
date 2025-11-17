package testutil

import (
	"encoding/json"
)

// TestFlow returns a generic core flow definition for testing (no semantic concepts).
// This is a vanilla input -> processor -> output flow with no domain knowledge.
func TestFlow() map[string]any {
	return map[string]any{
		"name": "test-flow",
		"components": map[string]any{
			"test-input": map[string]any{
				"type":     "input",
				"protocol": "udp",
				"config": map[string]any{
					"port": 14550,
					"bind": "0.0.0.0",
				},
			},
			"test-processor": map[string]any{
				"type":     "processor",
				"protocol": "filter",
				"config": map[string]any{
					"field":     "value",
					"condition": "gt",
					"threshold": 10,
				},
			},
			"test-output": map[string]any{
				"type":     "output",
				"protocol": "websocket",
				"config": map[string]any{
					"port": 8080,
					"path": "/ws",
				},
			},
		},
	}
}

// TestFlowJSON returns a test flow as JSON bytes.
func TestFlowJSON() ([]byte, error) {
	flow := TestFlow()
	return json.Marshal(flow)
}

// TestFlowRawMessage returns a test flow as json.RawMessage.
func TestFlowRawMessage() (json.RawMessage, error) {
	return TestFlowJSON()
}

// SimpleFlow returns a minimal flow for basic testing.
func SimpleFlow() map[string]any {
	return map[string]any{
		"name": "simple-flow",
		"components": map[string]any{
			"input1": map[string]any{
				"type":     "input",
				"protocol": "udp",
			},
			"output1": map[string]any{
				"type":     "output",
				"protocol": "websocket",
			},
		},
	}
}

// MultiComponentFlow returns a flow with multiple components.
func MultiComponentFlow() map[string]any {
	return map[string]any{
		"name": "multi-component-flow",
		"components": map[string]any{
			"input1": map[string]any{
				"type":     "input",
				"protocol": "udp",
				"config": map[string]any{
					"port": 14550,
				},
			},
			"input2": map[string]any{
				"type":     "input",
				"protocol": "udp",
				"config": map[string]any{
					"port": 14551,
				},
			},
			"processor1": map[string]any{
				"type":     "processor",
				"protocol": "json",
			},
			"processor2": map[string]any{
				"type":     "processor",
				"protocol": "filter",
			},
			"output1": map[string]any{
				"type":     "output",
				"protocol": "websocket",
				"config": map[string]any{
					"port": 8080,
				},
			},
			"output2": map[string]any{
				"type":     "output",
				"protocol": "websocket",
				"config": map[string]any{
					"port": 8081,
				},
			},
		},
	}
}

// FlowWithStorage returns a flow that includes a storage component.
func FlowWithStorage() map[string]any {
	return map[string]any{
		"name": "storage-flow",
		"components": map[string]any{
			"input1": map[string]any{
				"type":     "input",
				"protocol": "udp",
			},
			"storage1": map[string]any{
				"type":     "storage",
				"protocol": "objectstore",
				"config": map[string]any{
					"bucket": "test-bucket",
				},
			},
			"output1": map[string]any{
				"type":     "output",
				"protocol": "websocket",
			},
		},
	}
}

// InvalidFlow returns an intentionally invalid flow for error testing.
func InvalidFlow() map[string]any {
	return map[string]any{
		"name": "invalid-flow",
		"components": map[string]any{
			"bad-component": map[string]any{
				// Missing required "type" field
				"protocol": "unknown",
			},
		},
	}
}

// EmptyFlow returns a flow with no components.
func EmptyFlow() map[string]any {
	return map[string]any{
		"name":       "empty-flow",
		"components": map[string]any{},
	}
}

// FlowComponentConfig represents a generic component configuration.
type FlowComponentConfig struct {
	Name     string         `json:"name"`
	Type     string         `json:"type"`
	Protocol string         `json:"protocol"`
	Config   map[string]any `json:"config,omitempty"`
	Enabled  bool           `json:"enabled"`
}

// NewFlowComponentConfig creates a new flow component config.
func NewFlowComponentConfig(name, compType, protocol string) *FlowComponentConfig {
	return &FlowComponentConfig{
		Name:     name,
		Type:     compType,
		Protocol: protocol,
		Config:   make(map[string]any),
		Enabled:  true,
	}
}

// ToJSON converts the component config to JSON.
func (c *FlowComponentConfig) ToJSON() ([]byte, error) {
	return json.Marshal(c)
}

// FlowBuilder is a helper for building test flows programmatically.
type FlowBuilder struct {
	name       string
	components map[string]*FlowComponentConfig
}

// NewFlowBuilder creates a new flow builder.
func NewFlowBuilder(name string) *FlowBuilder {
	return &FlowBuilder{
		name:       name,
		components: make(map[string]*FlowComponentConfig),
	}
}

// AddComponent adds a component to the flow.
func (fb *FlowBuilder) AddComponent(comp *FlowComponentConfig) *FlowBuilder {
	fb.components[comp.Name] = comp
	return fb
}

// AddInput adds an input component.
func (fb *FlowBuilder) AddInput(name, protocol string, config map[string]any) *FlowBuilder {
	comp := NewFlowComponentConfig(name, "input", protocol)
	comp.Config = config
	return fb.AddComponent(comp)
}

// AddProcessor adds a processor component.
func (fb *FlowBuilder) AddProcessor(name, protocol string, config map[string]any) *FlowBuilder {
	comp := NewFlowComponentConfig(name, "processor", protocol)
	comp.Config = config
	return fb.AddComponent(comp)
}

// AddOutput adds an output component.
func (fb *FlowBuilder) AddOutput(name, protocol string, config map[string]any) *FlowBuilder {
	comp := NewFlowComponentConfig(name, "output", protocol)
	comp.Config = config
	return fb.AddComponent(comp)
}

// AddStorage adds a storage component.
func (fb *FlowBuilder) AddStorage(name, protocol string, config map[string]any) *FlowBuilder {
	comp := NewFlowComponentConfig(name, "storage", protocol)
	comp.Config = config
	return fb.AddComponent(comp)
}

// Build builds the flow and returns it as a map.
func (fb *FlowBuilder) Build() map[string]any {
	components := make(map[string]any)
	for name, comp := range fb.components {
		components[name] = map[string]any{
			"type":     comp.Type,
			"protocol": comp.Protocol,
			"config":   comp.Config,
			"enabled":  comp.Enabled,
		}
	}

	return map[string]any{
		"name":       fb.name,
		"components": components,
	}
}

// BuildJSON builds the flow and returns it as JSON.
func (fb *FlowBuilder) BuildJSON() ([]byte, error) {
	flow := fb.Build()
	return json.Marshal(flow)
}
