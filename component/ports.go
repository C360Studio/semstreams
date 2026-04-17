// Package component provides port configuration and management for component connections.
package component

import "strings"

// ResolveSubject returns the configured subject for the named port,
// replacing the trailing wildcard (`*` or `>`) with suffix. This lets
// components publish/subscribe using port-configured subjects instead
// of hardcoding subject prefixes.
//
// Falls back to portName + "." + suffix when no matching port is found,
// preserving backward compatibility for configs that omit the port.
func ResolveSubject(ports []PortDefinition, portName, suffix string) string {
	for _, p := range ports {
		if p.Name == portName {
			switch {
			case strings.HasSuffix(p.Subject, ".*"):
				return strings.TrimSuffix(p.Subject, "*") + suffix
			case strings.HasSuffix(p.Subject, ".>"):
				return strings.TrimSuffix(p.Subject, ">") + suffix
			default:
				return p.Subject + "." + suffix
			}
		}
	}
	return portName + "." + suffix
}

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
	Bucket      string `json:"bucket,omitempty"      schema:"editable,type:string,description:KV bucket name for KV ports"`

	// Config holds type-specific port configuration (e.g., JetStreamPort for consumer settings)
	Config any `json:"config,omitempty" schema:"editable,type:object,description:Type-specific port configuration"`
}

// PortConfig represents port configuration in component config
type PortConfig struct {
	Inputs  []PortDefinition `json:"inputs,omitempty"`
	Outputs []PortDefinition `json:"outputs,omitempty"`
	KVWrite []PortDefinition `json:"kv_write,omitempty"`
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
	case "timer":
		// Timer port for periodic triggers
		var iface *InterfaceContract
		if def.Interface != "" {
			iface = &InterfaceContract{
				Type:    def.Interface,
				Version: "v1",
			}
		}
		port.Config = TimerPort{
			Interval:  def.Subject, // Subject holds interval duration
			Interface: iface,
		}
	case "jetstream":
		jsPort := JetStreamPort{
			StreamName: def.StreamName,
			Subjects:   []string{def.Subject}, // Convert single subject to array
		}
		// Merge additional config if provided
		if configPort, ok := def.Config.(JetStreamPort); ok {
			if configPort.DeliverPolicy != "" {
				jsPort.DeliverPolicy = configPort.DeliverPolicy
			}
			if configPort.AckPolicy != "" {
				jsPort.AckPolicy = configPort.AckPolicy
			}
			if configPort.MaxDeliver > 0 {
				jsPort.MaxDeliver = configPort.MaxDeliver
			}
			if configPort.ConsumerName != "" {
				jsPort.ConsumerName = configPort.ConsumerName
			}
		}
		port.Config = jsPort
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
		// Parse KV watch config
		bucket := def.Bucket
		if bucket == "" {
			bucket = def.Subject // Fallback to Subject for backward compatibility
		}
		port.Config = KVWatchPort{
			Bucket: bucket,
		}
	case "kv", "kv-write", "kvwrite":
		// Parse KV write config
		bucket := def.Bucket
		if bucket == "" {
			bucket = def.Subject // Fallback to Subject for backward compatibility
		}
		var iface *InterfaceContract
		if def.Interface != "" {
			iface = &InterfaceContract{
				Type:    def.Interface,
				Version: "v1",
			}
		}
		port.Config = KVWritePort{
			Bucket:    bucket,
			Interface: iface,
		}
	case "store-read":
		// Store read ports declare streaming read access to content storage
		bucket := def.Bucket
		if bucket == "" {
			bucket = def.Subject
		}
		var iface *InterfaceContract
		if def.Interface != "" {
			iface = &InterfaceContract{
				Type:    def.Interface,
				Version: "v1",
			}
		}
		port.Config = StoreReadPort{
			Bucket:    bucket,
			Interface: iface,
		}
	case "http", "grpc", "websocket-server":
		// HTTP/gRPC/WebSocket server ports are network boundary ports
		port.Config = NetworkPort{
			Protocol: def.Type,
			Host:     "0.0.0.0",
			Port:     parsePortFromSubject(def.Subject),
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

// parsePortFromSubject extracts a port number from a subject string like ":8084" or "localhost:8084".
func parsePortFromSubject(subject string) int {
	// Handle ":8084" or "localhost:8084" formats
	if idx := len(subject) - 1; idx >= 0 {
		// Find the last colon
		for i := len(subject) - 1; i >= 0; i-- {
			if subject[i] == ':' {
				portStr := subject[i+1:]
				port := 0
				for _, c := range portStr {
					if c >= '0' && c <= '9' {
						port = port*10 + int(c-'0')
					} else {
						return 0
					}
				}
				return port
			}
		}
	}
	return 0
}
