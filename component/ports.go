// Package component provides port configuration and management for component connections.
package component

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/c360studio/semstreams/pkg/errs"
)

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

// UnmarshalJSON populates PortDefinition.Config with the typed struct
// matching p.Type, instead of leaving it as a generic map[string]any from
// the default decoder. Without this, every type assertion against
// def.Config (e.g. def.Config.(JetStreamPort) at BuildPortFromDefinition
// and applyJetStreamConsumerConfig) silently fails on JSON-loaded port
// configs — meaning per-port consumer fields (DeliverPolicy, AckPolicy,
// MaxDeliver, AckWait, HeartbeatInterval, ConsumerName) never reach the
// consumer at runtime even though they were declared in the YAML/JSON.
//
// Symmetric with Port.UnmarshalJSON's type-discriminated path
// (component/port.go:78). The wire shape differs: Port wraps its config
// in a `{"type":"...","data":{...}}` envelope, whereas PortDefinition
// uses the top-level `type` field as the discriminator and the `config`
// object holds the typed payload directly:
//
//	{"name":"agent.request","type":"jetstream","config":{"ack_wait":"5m"}}
//
// Unknown types fall through to map[string]any (matches pre-fix
// behaviour for forward-compat with custom port types).
func (p *PortDefinition) UnmarshalJSON(data []byte) error {
	type Alias PortDefinition
	aux := &struct {
		Config json.RawMessage `json:"config,omitempty"`
		*Alias
	}{
		Alias: (*Alias)(p),
	}
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}

	if len(aux.Config) == 0 || string(aux.Config) == "null" {
		return nil
	}

	switch p.Type {
	case "jetstream":
		var js JetStreamPort
		if err := json.Unmarshal(aux.Config, &js); err != nil {
			return errs.Wrap(err, "PortDefinition", "UnmarshalJSON", "jetstream config")
		}
		p.Config = js
	case "nats":
		var n NATSPort
		if err := json.Unmarshal(aux.Config, &n); err != nil {
			return errs.Wrap(err, "PortDefinition", "UnmarshalJSON", "nats config")
		}
		p.Config = n
	case "nats-request":
		var nr NATSRequestPort
		if err := json.Unmarshal(aux.Config, &nr); err != nil {
			return errs.Wrap(err, "PortDefinition", "UnmarshalJSON", "nats-request config")
		}
		p.Config = nr
	case "kvwatch", "kv-watch":
		var kv KVWatchPort
		if err := json.Unmarshal(aux.Config, &kv); err != nil {
			return errs.Wrap(err, "PortDefinition", "UnmarshalJSON", "kvwatch config")
		}
		p.Config = kv
	case "kv", "kv-write", "kvwrite":
		var kv KVWritePort
		if err := json.Unmarshal(aux.Config, &kv); err != nil {
			return errs.Wrap(err, "PortDefinition", "UnmarshalJSON", "kvwrite config")
		}
		p.Config = kv
	case "timer":
		var t TimerPort
		if err := json.Unmarshal(aux.Config, &t); err != nil {
			return errs.Wrap(err, "PortDefinition", "UnmarshalJSON", "timer config")
		}
		p.Config = t
	case "network", "http", "grpc", "websocket-server":
		var n NetworkPort
		if err := json.Unmarshal(aux.Config, &n); err != nil {
			return errs.Wrap(err, "PortDefinition", "UnmarshalJSON", "network config")
		}
		p.Config = n
	case "file":
		var f FilePort
		if err := json.Unmarshal(aux.Config, &f); err != nil {
			return errs.Wrap(err, "PortDefinition", "UnmarshalJSON", "file config")
		}
		p.Config = f
	case "http-client":
		var hc HTTPClientPort
		if err := json.Unmarshal(aux.Config, &hc); err != nil {
			return errs.Wrap(err, "PortDefinition", "UnmarshalJSON", "http-client config")
		}
		p.Config = hc
	default:
		// Unknown port type — preserve pre-fix behaviour by decoding the
		// raw config into a generic map. Forward-compat for custom port
		// types not yet known to the framework.
		var m map[string]any
		if err := json.Unmarshal(aux.Config, &m); err != nil {
			return errs.Wrap(err, "PortDefinition", "UnmarshalJSON",
				fmt.Sprintf("unknown port type %q config", p.Type))
		}
		p.Config = m
	}
	return nil
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
		// Merge additional config if provided. PortDefinition.UnmarshalJSON
		// guarantees def.Config is a JetStreamPort struct (not a generic
		// map[string]any) when the raw JSON had `"type":"jetstream"`, so
		// the type assertion succeeds for both Go-constructed and JSON-
		// loaded definitions.
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
			if configPort.AckWait != "" {
				jsPort.AckWait = configPort.AckWait
			}
			if configPort.HeartbeatInterval != "" {
				jsPort.HeartbeatInterval = configPort.HeartbeatInterval
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
	case "http-client":
		// Outbound HTTP-client / polling input dependency.
		// Precedence: typed Config wins over def.Subject for URLPattern.
		// def.Subject is the default URL pattern (scalar field); a typed
		// def.Config.(HTTPClientPort) overrides each non-empty field so
		// an operator JSON config block takes effect over the scalar.
		hcPort := HTTPClientPort{
			URLPattern: def.Subject, // scalar default
		}
		// Seed Interface from the flat scalar field (like the timer/kv/nats
		// cases) so a config-less declaration that only sets `interface:` does
		// not silently lose its contract; a typed Config.Interface still wins below.
		if def.Interface != "" {
			hcPort.Interface = &InterfaceContract{Type: def.Interface, Version: "v1"}
		}
		if configPort, ok := def.Config.(HTTPClientPort); ok {
			if configPort.Method != "" {
				hcPort.Method = configPort.Method
			}
			if configPort.URLPattern != "" {
				hcPort.URLPattern = configPort.URLPattern // typed Config wins
			}
			if configPort.TriggerPort != "" {
				hcPort.TriggerPort = configPort.TriggerPort
			}
			if configPort.AuthRef != "" {
				hcPort.AuthRef = configPort.AuthRef
			}
			if configPort.ContactPolicy != "" {
				hcPort.ContactPolicy = configPort.ContactPolicy
			}
			if configPort.Interface != nil {
				hcPort.Interface = configPort.Interface
			}
		}
		port.Config = hcPort
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
