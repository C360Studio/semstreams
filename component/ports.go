// Package component provides port configuration and management for component connections.
package component

// PortDefinition represents a port configuration from JSON
type PortDefinition struct {
	Name        string `json:"name"                  schema:"readonly,type:string,description:Port identifier"`
	Type        string `json:"type,omitempty"        schema:"readonly,type:string,description:Port type (nats jetstream kv-watch etc)"`
	Subject     string `json:"subject,omitempty"     schema:"editable,type:string,description:NATS subject pattern or network address"`
	Interface   string `json:"interface,omitempty"   schema:"readonly,type:string,description:Interface contract type"`
	Required    bool   `json:"required,omitempty"    schema:"readonly,type:bool,description:Whether port connection is required"`
	Description string `json:"description,omitempty" schema:"readonly,type:string,description:Human-readable port description"`
	Timeout     string `json:"timeout,omitempty"     schema:"editable,type:string,description:Request timeout for request/reply ports"`
	StreamName  string `json:"stream_name,omitempty" schema:"editable,type:string,description:JetStream stream name"`
}

// PortConfig represents port configuration in component config
type PortConfig struct {
	Inputs  []PortDefinition `json:"inputs,omitempty"`
	Outputs []PortDefinition `json:"outputs,omitempty"`
}

// MergePortConfigs merges default ports with configured overrides
func MergePortConfigs(defaults []Port, overrides []PortDefinition, direction Direction) []Port {
	result := make([]Port, 0)
	overrideMap := make(map[string]PortDefinition)

	// Build override map
	for _, override := range overrides {
		overrideMap[override.Name] = override
	}

	// Apply overrides to defaults
	for _, defaultPort := range defaults {
		if override, found := overrideMap[defaultPort.Name]; found {
			// Override found - use configured values
			result = append(result, BuildPortFromDefinition(override, direction))
			delete(overrideMap, defaultPort.Name)
		} else {
			// No override - use default
			result = append(result, defaultPort)
		}
	}

	// Add any additional ports from config
	for _, override := range overrideMap {
		result = append(result, BuildPortFromDefinition(override, direction))
	}

	return result
}

// BuildPortFromDefinition creates a Port from a PortDefinition
func BuildPortFromDefinition(def PortDefinition, direction Direction) Port {
	port := Port{
		Name:        def.Name,
		Direction:   direction,
		Required:    def.Required,
		Description: def.Description,
	}

	// Create appropriate port type based on config
	switch def.Type {
	case "jetstream":
		port.Config = JetStreamPort{
			StreamName: def.StreamName,
			Subjects:   []string{def.Subject}, // Convert single subject to array
		}
	case "nats-request":
		timeout := def.Timeout
		if timeout == "" {
			timeout = "1s" // Default timeout
		}
		port.Config = NATSRequestPort{
			Subject: def.Subject,
			Timeout: timeout,
		}
	case "kv-watch", "kvwatch":
		// Parse KV watch config - simplified for now
		port.Config = KVWatchPort{
			Bucket: def.Subject, // Subject holds bucket name
		}
	case "kv-write", "kvwrite":
		// Parse KV write config
		var iface *InterfaceContract
		if def.Interface != "" {
			iface = &InterfaceContract{
				Type:    def.Interface,
				Version: "v1",
			}
		}
		port.Config = KVWritePort{
			Bucket:    def.Subject, // Subject holds bucket name
			Interface: iface,
		}
	default: // Default to NATS pub/sub
		var iface *InterfaceContract
		if def.Interface != "" {
			iface = &InterfaceContract{
				Type:    def.Interface,
				Version: "v1",
			}
		}
		port.Config = NATSPort{
			Subject:   def.Subject,
			Interface: iface,
		}
	}

	return port
}
