package component

import (
	"encoding/json"
	"fmt"

	"github.com/c360studio/semstreams/pkg/errs"
)

// Direction for data flow
type Direction string

// Direction constants for port data flow
const (
	DirectionInput  Direction = "input"
	DirectionOutput Direction = "output"
)

// Port describes any I/O interface
type Port struct {
	Name        string    `json:"name"`
	Direction   Direction `json:"direction"`
	Required    bool      `json:"required"`
	Description string    `json:"description"`
	Config      Portable  `json:"config"`
}

// Portable interface - minimal, no Get prefix (Go idiomatic)
type Portable interface {
	ResourceID() string // Unique identifier for conflict detection
	IsExclusive() bool  // Whether multiple components can share
	Type() string       // Port type identifier
}

// InterfaceContract defines expected message interface
type InterfaceContract struct {
	Type       string   `json:"type"`                 // e.g., "message.Storable"
	Version    string   `json:"version,omitempty"`    // e.g., "v1"
	Compatible []string `json:"compatible,omitempty"` // Also accepts these
}

// MarshalJSON provides custom JSON marshaling for Port struct
// This handles the Portable interface by creating a wrapper with type information
func (p Port) MarshalJSON() ([]byte, error) {
	// Create a struct that matches Port but with the config properly typed
	type PortAlias Port // Prevent infinite recursion

	// Create the wrapper struct with json.RawMessage for config
	wrapper := struct {
		PortAlias
		Config json.RawMessage `json:"config"`
	}{
		PortAlias: (PortAlias)(p),
	}

	// Marshal the config with type information
	if p.Config != nil {
		configWithType := struct {
			Type string `json:"type"`
			Data any    `json:"data"`
		}{
			Type: p.Config.Type(),
			Data: p.Config,
		}

		configBytes, err := json.Marshal(configWithType)
		if err != nil {
			return nil, errs.Wrap(err, "Port", "MarshalJSON", "config marshaling")
		}
		wrapper.Config = configBytes
	}

	return json.Marshal(wrapper)
}

// UnmarshalJSON provides custom JSON unmarshaling for Port struct
// This handles reconstruction of the Portable interface from JSON
func (p *Port) UnmarshalJSON(data []byte) error {
	// Use an alias to prevent infinite recursion
	type PortAlias Port

	// Temporary struct to handle unmarshaling
	temp := struct {
		*PortAlias
		Config json.RawMessage `json:"config"`
	}{
		PortAlias: (*PortAlias)(p),
	}

	if err := json.Unmarshal(data, &temp); err != nil {
		return err
	}

	// Parse the config if present
	if len(temp.Config) > 0 {
		var configWrapper struct {
			Type string          `json:"type"`
			Data json.RawMessage `json:"data"`
		}

		if err := json.Unmarshal(temp.Config, &configWrapper); err != nil {
			return errs.Wrap(err, "Port", "UnmarshalJSON", "config wrapper unmarshaling")
		}

		// Create the appropriate config type based on the type field
		switch configWrapper.Type {
		case "timer":
			var timerConfig TimerPort
			if err := json.Unmarshal(configWrapper.Data, &timerConfig); err != nil {
				return errs.Wrap(err, "Port", "UnmarshalJSON", "timer config unmarshaling")
			}
			p.Config = timerConfig
		case "network":
			var netConfig NetworkPort
			if err := json.Unmarshal(configWrapper.Data, &netConfig); err != nil {
				return errs.Wrap(err, "Port", "UnmarshalJSON", "network config unmarshaling")
			}
			p.Config = netConfig
		case "nats":
			var natsConfig NATSPort
			if err := json.Unmarshal(configWrapper.Data, &natsConfig); err != nil {
				return errs.Wrap(err, "Port", "UnmarshalJSON", "nats config unmarshaling")
			}
			p.Config = natsConfig
		case "nats-request":
			var requestConfig NATSRequestPort
			if err := json.Unmarshal(configWrapper.Data, &requestConfig); err != nil {
				return errs.Wrap(err, "Port", "UnmarshalJSON", "nats-request config unmarshaling")
			}
			p.Config = requestConfig
		case "file":
			var fileConfig FilePort
			if err := json.Unmarshal(configWrapper.Data, &fileConfig); err != nil {
				return errs.Wrap(err, "Port", "UnmarshalJSON", "file config unmarshaling")
			}
			p.Config = fileConfig
		case "jetstream":
			var jsConfig JetStreamPort
			if err := json.Unmarshal(configWrapper.Data, &jsConfig); err != nil {
				return errs.Wrap(err, "Port", "UnmarshalJSON", "jetstream config unmarshaling")
			}
			p.Config = jsConfig
		// Accept both concatenated ("kvwatch") and dashed ("kv-watch")
		// spellings to match PortDefinition.UnmarshalJSON
		// (component/ports.go). Pre-2026-05-13 only the concatenated
		// form parsed here while PortDefinition accepted both —
		// operator configs that used the dashed spelling worked
		// through the PortDefinition path but failed loudly if the
		// same Port was round-tripped through Port.UnmarshalJSON
		// (e.g. KV-stored Port descriptors or serialized flow
		// definitions). Audit finding 2026-05-08 (Finding 5).
		case "kvwatch", "kv-watch":
			var kvConfig KVWatchPort
			if err := json.Unmarshal(configWrapper.Data, &kvConfig); err != nil {
				return errs.Wrap(err, "Port", "UnmarshalJSON", "kvwatch config unmarshaling")
			}
			p.Config = kvConfig
		case "kvwrite", "kv-write", "kv":
			// "kv" is the shorthand PortDefinition.UnmarshalJSON
			// accepts as well; keep parity.
			var kvConfig KVWritePort
			if err := json.Unmarshal(configWrapper.Data, &kvConfig); err != nil {
				return errs.Wrap(err, "Port", "UnmarshalJSON", "kvwrite config unmarshaling")
			}
			p.Config = kvConfig
		case "http-client":
			var hcConfig HTTPClientPort
			if err := json.Unmarshal(configWrapper.Data, &hcConfig); err != nil {
				return errs.Wrap(err, "Port", "UnmarshalJSON", "http-client config unmarshaling")
			}
			p.Config = hcConfig
		default:
			return errs.WrapInvalid(
				fmt.Errorf("unknown config type: %s", configWrapper.Type),
				"Port",
				"UnmarshalJSON",
				"config type validation",
			)
		}
	}

	return nil
}
