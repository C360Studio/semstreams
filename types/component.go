// Package types contains shared domain types used across the semstreams platform
package types

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/c360studio/semstreams/pkg/errs"
)

// ComponentType represents the category of a component
type ComponentType string

// Component type constants
const (
	ComponentTypeInput     ComponentType = "input"
	ComponentTypeProcessor ComponentType = "processor"
	ComponentTypeOutput    ComponentType = "output"
	ComponentTypeStorage   ComponentType = "storage"
	ComponentTypeGateway   ComponentType = "gateway"
)

// ComponentConfig provides configuration for creating a component instance
// The instance name comes from the map key in the components configuration.
// This structure is shared between the config and component packages.
type ComponentConfig struct {
	Type    ComponentType   `json:"type"`    // Component type (input/processor/output/storage/gateway)
	Name    string          `json:"name"`    // Factory/component name (e.g., "udp", "websocket", "mavlink")
	Enabled bool            `json:"enabled"` // Whether component is enabled
	Config  json.RawMessage `json:"config"`  // Component-specific configuration
}

// Equal reports whether two component configs are effectively identical.
//
// Type, Name, and Enabled compare by value. Config is raw JSON, so it is
// compared canonically (order- and whitespace-insensitive): two configs that
// differ only in key ordering or formatting — exactly what a re-marshal during a
// full-config sync produces — are equal. This is what lets the ComponentManager
// treat a no-op config update as a skip instead of a spurious restart. Empty,
// absent, and JSON null Config all compare equal. If either Config is not valid
// JSON, it falls back to a raw-byte compare (a malformed config is not silently
// treated as equal to a differing one).
func (c ComponentConfig) Equal(other ComponentConfig) bool {
	if c.Type != other.Type || c.Name != other.Name || c.Enabled != other.Enabled {
		return false
	}
	return equalJSONConfig(c.Config, other.Config)
}

// equalJSONConfig compares two raw JSON config bodies canonically. Empty/null
// bodies are equivalent; malformed JSON falls back to a byte compare.
func equalJSONConfig(a, b json.RawMessage) bool {
	aCanon, aOK := canonicalJSON(a)
	bCanon, bOK := canonicalJSON(b)
	if !aOK || !bOK {
		// At least one side is not valid JSON — compare bytes exactly rather
		// than risk treating a malformed change as unchanged.
		return string(a) == string(b)
	}
	return aCanon == bCanon
}

// canonicalJSON returns an order-insensitive canonical form of a raw JSON body.
// An empty or JSON-null body canonicalizes to "null" so the two are equal. The
// bool is false when raw is non-empty and not valid JSON.
//
// Numbers are decoded with UseNumber so integer tokens survive as their exact
// source text rather than lossy float64. That keeps a large integer (≥ 2^53)
// distinct from a different large integer that would round to the same float64 —
// the guard must never treat a genuine config change as unchanged. The trade-off
// is that "1" and "1.0" canonicalize distinctly (a harmless spurious restart, the
// safe direction), which is acceptable for a restart-avoidance guard.
func canonicalJSON(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 {
		return "null", true
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return "", false
	}
	// Reject trailing content after the first JSON value (Decode reads only one).
	if dec.More() {
		return "", false
	}
	// Re-marshal: encoding/json sorts object keys, yielding a canonical,
	// whitespace-free form for equality comparison.
	canon, err := json.Marshal(v)
	if err != nil {
		return "", false
	}
	return string(canon), true
}

// Validate ensures the component configuration is valid
func (c ComponentConfig) Validate() error {
	if c.Type == "" {
		return errs.WrapInvalid(
			errs.ErrMissingConfig,
			"ComponentConfig",
			"Validate",
			"component type cannot be empty",
		)
	}
	if c.Name == "" {
		return errs.WrapInvalid(
			errs.ErrMissingConfig,
			"ComponentConfig",
			"Validate",
			"component factory name cannot be empty",
		)
	}

	switch c.Type {
	case ComponentTypeInput, ComponentTypeProcessor, ComponentTypeOutput, ComponentTypeStorage, ComponentTypeGateway:
		return nil
	default:
		return errs.WrapInvalid(errs.ErrInvalidConfig, "ComponentConfig", "Validate",
			fmt.Sprintf("invalid component type: %s", c.Type))
	}
}

// String implements fmt.Stringer for ComponentType
func (ct ComponentType) String() string {
	return string(ct)
}

// PlatformMeta provides platform identity to services and components.
// This structure decouples platform identity from the config package,
// allowing services to access org and platform information without
// creating dependencies on configuration structures.
type PlatformMeta struct {
	Org      string // Organization namespace (e.g., "c360", "noaa")
	Platform string // Platform identifier (e.g., "platform1", "vessel-alpha")
}
