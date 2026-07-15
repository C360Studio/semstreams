package message

import "github.com/c360studio/semstreams/pkg/types"

// Type aliases for backwards compatibility.
// The canonical definitions are now in pkg/types.
// New code should import pkg/types directly.

// Keyable interface represents types that can be converted to semantic keys
// using dotted notation.
type Keyable = types.Keyable

// Type provides structured type information for messages.
// It enables type-safe routing and processing by clearly identifying
// the domain, category, and version of each message.
type Type = types.Type

// EntityType represents a structured entity type identifier using dotted notation.
// Format: EntityType{Domain: "domain", Type: "type"} -> Key() returns "domain.type"
type EntityType = types.EntityType

// EntityID represents a complete entity identifier with semantic structure.
// Follows the pattern: org.platform.domain.system.type.instance for federated entity management.
type EntityID = types.EntityID

// ParseEntityID delegates canonical parsing to pkg/types.
func ParseEntityID(value string) (EntityID, error) {
	return types.ParseEntityID(value)
}
