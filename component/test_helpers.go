package component

import "encoding/json"

// Test helpers shared across test files

// createSimpleMockComponent is a factory function for creating mock components in tests
func createSimpleMockComponent(rawConfig json.RawMessage, _ Dependencies) (Discoverable, error) {
	// Parse config if provided
	config := make(map[string]any)
	if len(rawConfig) > 0 {
		if err := json.Unmarshal(rawConfig, &config); err != nil {
			return nil, err
		}
	}

	name := safeGetString(config, "name", "test")
	return &SimpleMockComponent{
		name:   name,
		config: config,
	}, nil
}

// SimpleMockComponent is a simple component implementation for testing
type SimpleMockComponent struct {
	name   string
	config map[string]any
}

// Meta returns the component metadata
func (m *SimpleMockComponent) Meta() Metadata {
	compType := safeGetString(m.config, "type", "processor") // default

	return Metadata{
		Name: m.name,
		Type: compType,
	}
}

// safeGetString safely extracts a string value from a config map
func safeGetString(cfg map[string]any, key string, defaultVal string) string {
	if val, ok := cfg[key]; ok {
		if str, ok := val.(string); ok {
			return str
		}
	}
	return defaultVal
}

// InputPorts returns the component's input ports (none for mock)
func (m *SimpleMockComponent) InputPorts() []Port { return nil }

// OutputPorts returns the component's output ports (none for mock)
func (m *SimpleMockComponent) OutputPorts() []Port { return nil }

// ConfigSchema returns the component's configuration schema
func (m *SimpleMockComponent) ConfigSchema() ConfigSchema {
	return ConfigSchema{
		Properties: map[string]PropertySchema{
			"name": {
				Type:        "string",
				Description: "Component name",
			},
		},
	}
}

// Health returns the component's health status (always healthy for mock)
func (m *SimpleMockComponent) Health() HealthStatus {
	return HealthStatus{
		Healthy:    true,
		LastError:  "",
		ErrorCount: 0,
	}
}

// DataFlow returns the component's data flow metrics (zeros for mock)
func (m *SimpleMockComponent) DataFlow() FlowMetrics {
	return FlowMetrics{
		MessagesPerSecond: 0,
		BytesPerSecond:    0,
	}
}

// intPtr returns a pointer to an int value.
// Used for optional schema fields like Minimum and Maximum.
func intPtr(i int) *int {
	return &i
}
