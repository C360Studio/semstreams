// Package component defines the Discoverable interface and related types
package component

import (
	"time"
)

// Discoverable defines the interface for components that can be discovered
// and inspected by the management layer. This interface enables dynamic discovery
// of component capabilities, configuration, and health status.
//
// Components implementing this interface can be:
// - Input components: Accept external data (UDP, TCP, HTTP)
// - Processor components: Transform data (plugins)
// - Output components: Send data to external systems
// - Storage components: Store and retrieve data (ObjectStore, KV)
type Discoverable interface {
	// Meta returns basic component information
	Meta() Metadata

	// InputPorts returns the ports this component accepts data on
	InputPorts() []Port

	// OutputPorts returns the ports this component produces data on
	OutputPorts() []Port

	// ConfigSchema returns the configuration schema for this component
	ConfigSchema() ConfigSchema

	// Health returns current health status
	Health() HealthStatus

	// DataFlow returns current data flow metrics
	DataFlow() FlowMetrics
}

// Metadata describes what a component is
type Metadata struct {
	Name        string `json:"name"`
	Type        string `json:"type"` // "input", "processor", "output", "storage"
	Description string `json:"description"`
	Version     string `json:"version"`
}

// ConfigSchema describes the configuration parameters for a component
type ConfigSchema struct {
	Properties map[string]PropertySchema `json:"properties"`
	Required   []string                  `json:"required"`
}

// PropertySchema describes a single configuration property
type PropertySchema struct {
	Type        string                    `json:"type"` // "string", "int", "bool", "float", "enum", "array", "object", "ports", "cache"
	Description string                    `json:"description"`
	Default     any                       `json:"default,omitempty"`
	Enum        []string                  `json:"enum,omitempty"`        // Valid string values
	Minimum     *int                      `json:"minimum,omitempty"`     // For numeric types
	Maximum     *int                      `json:"maximum,omitempty"`     // For numeric types
	MinLength   *int                      `json:"minLength,omitempty"`   // Minimum string length
	MaxLength   *int                      `json:"maxLength,omitempty"`   // Maximum string length
	Pattern     string                    `json:"pattern,omitempty"`     // Regular expression for string values
	Category    string                    `json:"category,omitempty"`    // "basic" or "advanced" for UI organization
	PortFields  map[string]PortFieldInfo  `json:"portFields,omitempty"`  // Metadata for port fields (when type is "ports")
	CacheFields map[string]CacheFieldInfo `json:"cacheFields,omitempty"` // Metadata for cache fields (when type is "cache")
	Properties  map[string]PropertySchema `json:"properties,omitempty"`  // Nested properties for object types
	// AdditionalProperties controls whether object fields outside Properties
	// are accepted. nil preserves JSON Schema's permissive default.
	AdditionalProperties *bool           `json:"additionalProperties,omitempty"`
	Required             []string        `json:"required,omitempty"` // Required nested fields for object types
	Items                *PropertySchema `json:"items,omitempty"`    // Item schema for array types
}

// HealthStatus describes the current health state of a component
type HealthStatus struct {
	Healthy    bool          `json:"healthy"`
	LastCheck  time.Time     `json:"last_check"`
	ErrorCount int           `json:"error_count"`
	LastError  string        `json:"last_error,omitempty"`
	Uptime     time.Duration `json:"uptime"`
	Status     string        `json:"status"`
}

// DebugStatusProvider is an optional interface for components that can provide
// extended debug information beyond basic health status.
type DebugStatusProvider interface {
	// DebugStatus returns extended debug information for the component.
	// The returned value should be JSON-serializable.
	DebugStatus() any
}

// FlowMetrics describes the current data flow through a component
type FlowMetrics struct {
	MessagesPerSecond float64   `json:"messages_per_second"`
	BytesPerSecond    float64   `json:"bytes_per_second"`
	ErrorRate         float64   `json:"error_rate"`
	LastActivity      time.Time `json:"last_activity"`
}
