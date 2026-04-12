// Package message provides core message infrastructure for SemStreams.
// See doc.go for complete package documentation.
package message

import (
	"strings"
	"time"
)

// Triple represents a semantic statement about an entity following the Subject-Predicate-Object pattern.
// This enables RDF-like knowledge graphs while maintaining simplicity for Go developers.
//
// Triple design follows these principles:
//   - Subject: Always an entity ID using EntityID.Key() format (e.g., "telemetry.robotics.drone.1")
//   - Predicate: Semantic property using three-level dotted notation (e.g., "robotics.battery.level")
//   - Object: Typed value (literals) or entity references (other entity IDs)
//   - Metadata: Source, timestamp, and confidence for provenance tracking
//
// Example triples from a drone heartbeat message:
//   - ("telemetry.robotics.drone.1", "robotics.battery.level", 85.5)
//   - ("telemetry.robotics.drone.1", "robotics.flight.armed", true)
//   - ("telemetry.robotics.drone.1", "robotics.component.has", "telemetry.robotics.battery.1")
//
// This structure enables:
//   - NATS wildcard queries: "robotics.battery.*" finds all battery predicates
//   - Entity relationship modeling: Objects can reference other entities
//   - Temporal tracking: Each triple has timestamp and confidence
//   - Provenance: Source field tracks where each assertion came from
//   - Federation: Works with federated entity IDs from multiple sources
type Triple struct {
	// Subject identifies the entity this triple describes.
	// Must use EntityID.Key() format for consistency with federated entity management.
	// Examples: "telemetry.robotics.drone.1", "gcs-alpha.robotics.drone.001"
	Subject string `json:"subject"`

	// Predicate identifies the semantic property using three-level dotted notation.
	// Format: domain.category.property (e.g., "robotics.battery.level")
	// This maintains consistency with the unified dotted notation from Alpha Week 1.
	// Predicates should be defined in the vocabulary package for consistency.
	Predicate string `json:"predicate"`

	// Object contains the property value or entity reference.
	// For literals: primitive types (float64, bool, string, int)
	// For entity references: entity ID strings using EntityID.Key() format
	// Complex objects should be flattened into multiple triples.
	Object any `json:"object"`

	// Source identifies where this assertion came from.
	// Examples: "mavlink_heartbeat", "gps_fix", "operator_input", "ai_inference"
	// Enables traceability and conflict resolution during entity merging.
	Source string `json:"source"`

	// Timestamp indicates when this assertion was made.
	// Should typically be the message timestamp or processing time.
	// Enables temporal queries and helps with entity state evolution.
	Timestamp time.Time `json:"timestamp"`

	// Confidence indicates the reliability of this assertion (0.0 to 1.0).
	// Higher values indicate greater certainty about the triple.
	//
	// Typical confidence levels:
	//   - 1.0: Direct telemetry or explicit data
	//   - 0.9: High-confidence sensor readings
	//   - 0.7: Calculated or derived values
	//   - 0.5: Inferred relationships
	//   - 0.0: Uncertain or placeholder data
	Confidence float64 `json:"confidence"`

	// Context provides correlation ID for message batches or request tracking.
	// This enables grouping related triples from the same processing batch
	// or correlating triples across distributed systems.
	// Examples: message ID, batch ID, correlation ID, request ID
	Context string `json:"context,omitempty"`

	// Datatype provides optional RDF datatype hint for the Object value.
	// This helps with type interpretation and validation in downstream systems.
	// Examples: "xsd:float", "xsd:dateTime", "geo:point", "xsd:boolean"
	// If omitted, the type is inferred from the Go type of Object.
	Datatype string `json:"datatype,omitempty"`

	// ExpiresAt indicates when this triple should be considered expired.
	// When nil, the triple never expires and remains valid indefinitely.
	// When set, the triple is considered expired after this timestamp.
	// This enables TTL-based triple expiration for temporal data management.
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

// TripleGenerator enables messages to produce semantic triples for graph storage.
// This interface replaces the use of untyped Properties maps with structured
// semantic assertions that enable reasoning and complex queries.
//
// Implementations should:
//   - Generate triples for all meaningful properties from the message
//   - Use vocabulary predicate constants for consistency
//   - Include entity relationships as triples with entity reference objects
//   - Set appropriate confidence levels based on data source quality
//   - Use EntityID.Key() format for consistent subject identification
//
// Example implementation for a drone battery payload:
//
//	func (b *BatteryPayload) Triples() []Triple {
//	    entityID := EntityID{
//	        Source: "telemetry", Domain: "robotics", Type: "drone",
//	        Instance: fmt.Sprintf("%d", b.SystemID),
//	    }.Key()
//
//	    return []Triple{{
//	        Subject: entityID,
//	        Predicate: vocabulary.ROBOTICS_BATTERY_LEVEL, // "robotics.battery.level"
//	        Object: float64(b.BatteryRemaining),
//	        Source: "mavlink_battery",
//	        Timestamp: time.Now(),
//	        Confidence: 1.0,
//	    }}
//	}
type TripleGenerator interface {
	// Triples returns semantic triples extracted from this message.
	// Each triple represents a meaningful assertion about an entity,
	// using structured predicates and proper confidence scoring.
	Triples() []Triple
}

// IsRelationship checks if this triple represents a relationship between entities
// rather than a property with a literal value.
// Returns true if Object is a valid EntityID (4-part dotted notation).
func (t Triple) IsRelationship() bool {
	if str, ok := t.Object.(string); ok {
		return IsValidEntityID(str)
	}
	return false
}

// IsValidEntityID checks if a string conforms to the canonical 6-part EntityID format.
// Valid format: organization.platform.domain.system.type.instance (e.g., "c360.platform1.robotics.mav1.drone.0")
// Each part must contain only alphanumeric characters, hyphens, or underscores.
// This prevents false positives from strings that happen to have 5 dots (e.g., JSON
// with file extensions like ".go", ".md") and ensures the ID is safe as a NATS KV key.
func IsValidEntityID(s string) bool {
	if s == "" {
		return false
	}

	parts := strings.Split(s, ".")
	// Require exactly 6 parts for canonical format
	if len(parts) != 6 {
		return false
	}

	// Each part must be non-empty and contain only valid identifier characters.
	// Valid: a-z, A-Z, 0-9, hyphen, underscore
	for _, part := range parts {
		if part == "" {
			return false
		}
		for _, c := range part {
			if !isEntityIDChar(c) {
				return false
			}
		}
	}

	return true
}

// isEntityIDChar returns true if c is valid in an entity ID part:
// alphanumeric, hyphen, or underscore.
func isEntityIDChar(c rune) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_'
}

// IsExpired returns true if the triple has an expiration time that has passed.
// Returns false if ExpiresAt is nil (never expires) or if the expiration time
// has not yet been reached (including exact equality with current time).
// A triple is only considered expired when time.Now() is strictly after ExpiresAt.
func (t Triple) IsExpired() bool {
	if t.ExpiresAt == nil {
		return false
	}
	return time.Now().After(*t.ExpiresAt)
}
