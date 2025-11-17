package message

import (
	"fmt"
	"strings"

	"github.com/c360/semstreams/errors"
)

// Keyable interface represents types that can be converted to semantic keys
// using dotted notation. This is the foundation of the unified semantic
// architecture enabling NATS wildcard queries and consistent storage patterns.
type Keyable interface {
	// Key returns the dotted notation representation of this semantic type.
	// Examples: "robotics.drone", "telemetry.robotics.drone.1", "robotics.battery.v1"
	Key() string
}

// EntityType represents a structured entity type identifier using dotted notation.
//
// Format: EntityType{Domain: "domain", Type: "type"} -> Key() returns "domain.type"
//
// Examples:
//   - EntityType{Domain: "robotics", Type: "drone"} -> "robotics.drone"
//   - EntityType{Domain: "robotics", Type: "battery"} -> "robotics.battery"
//   - EntityType{Domain: "sensors", Type: "gps"} -> "sensors.gps"
//
// EntityType is used by Identifiable payloads to specify their entity type,
// enabling entity extraction and property graph construction with consistent dotted keys.
type EntityType struct {
	// Domain identifies the business or system domain (lowercase)
	Domain string
	// Type identifies the specific entity type within the domain (lowercase)
	Type string
}

// Key returns the dotted notation representation: "domain.type"
// This implements the Keyable interface for unified semantic keys.
func (et EntityType) Key() string {
	return fmt.Sprintf("%s.%s", et.Domain, et.Type)
}

// String returns the same as Key() for backwards compatibility
func (et EntityType) String() string {
	return et.Key()
}

// IsValid checks if the EntityType has required fields populated
func (et EntityType) IsValid() bool {
	return et.Domain != "" && et.Type != ""
}

// Type provides structured type information for messages.
// It enables type-safe routing and processing by clearly identifying
// the domain, category, and version of each message.
//
// Type constants should be defined in domain packages to maintain
// clear ownership and avoid coupling. This package only provides the
// type definition itself.
//
// Example definition in a domain package:
//
//	var GPSMessage = message.Type{
//	    Domain:   "sensors",
//	    Category: "gps",
//	    Version:  "v1",
//	}
type Type struct {
	// Domain identifies the business or system domain.
	// Examples: "sensors", "robotics", "finance"
	Domain string

	// Category identifies the specific message type within the domain.
	// Examples: "gps", "temperature", "heartbeat", "trade"
	Category string

	// Version identifies the schema version.
	// Format: "v1", "v2", etc. Enables schema evolution.
	Version string
}

// Key returns the dotted notation representation: "domain.category.version"
// This implements the Keyable interface for unified semantic keys.
func (mt Type) Key() string {
	return fmt.Sprintf("%s.%s.%s", mt.Domain, mt.Category, mt.Version)
}

// String returns the same as Key() for backwards compatibility
func (mt Type) String() string {
	return mt.Key()
}

// IsValid checks if the Type has all required fields populated
// with non-empty values.
func (mt Type) IsValid() bool {
	return mt.Domain != "" && mt.Category != "" && mt.Version != ""
}

// Equal compares two Type instances for equality.
// Returns true if all fields (Domain, Category, Version) are identical.
func (mt Type) Equal(other Type) bool {
	return mt.Domain == other.Domain &&
		mt.Category == other.Category &&
		mt.Version == other.Version
}

// EntityID represents a complete entity identifier with semantic structure.
// Follows the pattern: org.platform.domain.system.type.instance for federated entity management.
//
// Examples:
//   - EntityID{Org: "c360", Platform: "platform1", Domain: "robotics", System: "gcs1", Type: "drone", Instance: "1"} -> "c360.platform1.robotics.gcs1.drone.1"
//   - EntityID{Org: "c360", Platform: "platform1", Domain: "robotics", System: "mav1", Type: "battery", Instance: "0"} -> "c360.platform1.robotics.mav1.battery.0"
//
// EntityID enables federated entity identification where multiple sources may have
// entities with the same local ID but different canonical identities.
type EntityID struct {
	// Federation hierarchy (3 parts)
	Org      string // Organization namespace (e.g., "c360")
	Platform string // Platform/instance ID (e.g., "platform1")
	System   string // System/source ID - RUNTIME from message (e.g., "mav1", "gcs255")

	// Domain hierarchy (2 parts)
	Domain string // Data domain (e.g., "robotics")
	Type   string // Entity type (e.g., "drone", "battery")

	// Instance identifier (1 part)
	Instance string // Simple instance ID (e.g., "1", "42")
}

// Key returns the full 6-part dotted notation in domain-first format
// This implements the Keyable interface for unified semantic keys.
func (eid EntityID) Key() string {
	return fmt.Sprintf("%s.%s.%s.%s.%s.%s",
		eid.Org, eid.Platform, eid.Domain, eid.System, eid.Type, eid.Instance)
}

// String returns the same as Key() for backwards compatibility
func (eid EntityID) String() string {
	return eid.Key()
}

// EntityType returns the EntityType component of this EntityID
func (eid EntityID) EntityType() EntityType {
	return EntityType{Domain: eid.Domain, Type: eid.Type}
}

// IsValid checks if the EntityID has all required fields populated
func (eid EntityID) IsValid() bool {
	return eid.Org != "" && eid.Platform != "" && eid.System != "" && eid.Domain != "" && eid.Type != "" &&
		eid.Instance != ""
}

// ParseEntityID creates EntityID from dotted string format.
// Expects exactly 6 parts: org.platform.domain.system.type.instance
// Returns an error if the format is invalid.
func ParseEntityID(s string) (EntityID, error) {
	parts := strings.Split(s, ".")
	if len(parts) != 6 {
		return EntityID{}, errors.WrapInvalid(errors.ErrInvalidData, "EntityID", "ParseEntityID",
			fmt.Sprintf("expected 6 parts, got %d", len(parts)))
	}

	// Check that no part is empty
	for i, part := range parts {
		if part == "" {
			return EntityID{}, errors.WrapInvalid(errors.ErrInvalidData, "EntityID", "ParseEntityID",
				fmt.Sprintf("part %d is empty", i+1))
		}
	}

	return EntityID{
		Org:      parts[0],
		Platform: parts[1],
		Domain:   parts[2],
		System:   parts[3],
		Type:     parts[4],
		Instance: parts[5],
	}, nil
}
