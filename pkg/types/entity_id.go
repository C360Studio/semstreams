package types

import (
	"errors"
	"fmt"
	"strings"

	"github.com/c360studio/semstreams/pkg/errs"
)

const (
	// MaxEntityIDBytes is the maximum serialized size of a canonical entity ID,
	// including its five separators. There is no independent segment bound.
	MaxEntityIDBytes = 256

	// ErrorCodeEntityIDInvalid classifies an invalid literal entity ID.
	ErrorCodeEntityIDInvalid = "entity_id_invalid"
	// ErrorCodeEntityIDPatternInvalid classifies an invalid declaration pattern.
	ErrorCodeEntityIDPatternInvalid = "entity_id_pattern_invalid"
	// ErrorCodeEntityIDPrefixInvalid classifies an invalid query prefix.
	ErrorCodeEntityIDPrefixInvalid = "entity_id_prefix_invalid"

	// EntityIDReasonEmpty identifies an empty whole input.
	EntityIDReasonEmpty = "empty"
	// EntityIDReasonBytes identifies a serialized byte-limit violation.
	EntityIDReasonBytes = "bytes"
	// EntityIDReasonArity identifies an invalid number of positions.
	EntityIDReasonArity = "arity"
	// EntityIDReasonEmptySegment identifies an empty position.
	EntityIDReasonEmptySegment = "empty_segment"
	// EntityIDReasonFirstByte identifies a non-alphanumeric segment start.
	EntityIDReasonFirstByte = "first_byte"
	// EntityIDReasonAlphabet identifies a forbidden segment byte.
	EntityIDReasonAlphabet = "alphabet"

	// EntityIDDetailReason is the stable reason detail key.
	EntityIDDetailReason = "reason"
	// EntityIDDetailMeasuredBytes reports the rejected serialized byte count.
	EntityIDDetailMeasuredBytes = "measured_bytes"
	// EntityIDDetailAllowedBytes reports the serialized byte limit.
	EntityIDDetailAllowedBytes = "allowed_bytes"
	// EntityIDDetailMeasuredParts reports the rejected position count.
	EntityIDDetailMeasuredParts = "measured_parts"
	// EntityIDDetailAllowedParts reports the position-count limit.
	EntityIDDetailAllowedParts = "allowed_parts"
	// EntityIDDetailSegmentIndex reports the zero-based failing position.
	EntityIDDetailSegmentIndex = "segment_index"

	entityIDLiteralSegmentPattern = `[A-Za-z0-9][A-Za-z0-9_-]*`
	entityIDLiteralBodyPattern    = entityIDLiteralSegmentPattern + `(?:\.` + entityIDLiteralSegmentPattern + `){5}`
	entityIDPatternSegmentPattern = `(?:\*|` + entityIDLiteralSegmentPattern + `)`
	entityIDPatternBodyPattern    = entityIDPatternSegmentPattern + `(?:\.` + entityIDPatternSegmentPattern + `){5}`

	// EntityIDLiteralPattern is the anchored JSON-Schema-compatible regular
	// expression for a canonical six-part literal entity ID.
	EntityIDLiteralPattern = `^` + entityIDLiteralBodyPattern + `$`
	// EntityIDDeclarationPattern is the anchored JSON-Schema-compatible
	// regular expression for an exact six-position entity ID declaration
	// pattern whose positions are canonical literals or the complete token "*".
	EntityIDDeclarationPattern = `^` + entityIDPatternBodyPattern + `$`
	// EntityIDLiteralPrefixPattern is the anchored JSON-Schema-compatible
	// regular expression for a canonical one-to-six-part literal query prefix.
	EntityIDLiteralPrefixPattern = `^` + entityIDLiteralSegmentPattern + `(?:\.` + entityIDLiteralSegmentPattern + `){0,5}$`
	// OptionalEntityIDLiteralPattern is EntityIDLiteralPattern plus the explicit
	// empty-string sentinel used by optional configuration fields.
	OptionalEntityIDLiteralPattern = `^(?:$|` + entityIDLiteralBodyPattern + `)$`
)

const canonicalEntityIDParts = 6

var errInvalidEntityIDContract = errors.New("invalid entity ID contract input")

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

// IsValid reports whether the exact serialized fields form a canonical ID.
func (eid EntityID) IsValid() bool {
	return ValidateEntityID(eid.Key()) == nil
}

// ParseEntityID creates EntityID from dotted string format.
// Expects exactly 6 parts: org.platform.domain.system.type.instance
// Returns an error if the format is invalid.
func ParseEntityID(s string) (EntityID, error) {
	if err := ValidateEntityID(s); err != nil {
		return EntityID{}, err
	}
	parts := strings.Split(s, ".")
	return EntityID{
		Org:      parts[0],
		Platform: parts[1],
		Domain:   parts[2],
		System:   parts[3],
		Type:     parts[4],
		Instance: parts[5],
	}, nil
}

// ValidateEntityID validates one canonical six-part entity identity without
// rewriting, encoding, or normalizing any input byte.
func ValidateEntityID(value string) error {
	return validateEntityIDValue(value, ErrorCodeEntityIDInvalid, canonicalEntityIDParts, canonicalEntityIDParts, false)
}

// IsValidEntityID is the boolean convenience over ValidateEntityID.
func IsValidEntityID(value string) bool {
	return ValidateEntityID(value) == nil
}

// ValidateEntityIDPattern validates an exact six-position declaration
// pattern. Each position is either one canonical literal segment or "*".
func ValidateEntityIDPattern(value string) error {
	return validateEntityIDValue(
		value, ErrorCodeEntityIDPatternInvalid, canonicalEntityIDParts, canonicalEntityIDParts, true,
	)
}

// MatchEntityIDPattern reports whether entityID matches an exact six-position
// declaration pattern. Both inputs are validated by the canonical contract;
// callers never receive a partial or best-effort match for malformed data.
func MatchEntityIDPattern(pattern, entityID string) (bool, error) {
	if err := ValidateEntityIDPattern(pattern); err != nil {
		return false, err
	}
	if err := ValidateEntityID(entityID); err != nil {
		return false, err
	}

	patternParts := strings.Split(pattern, ".")
	entityParts := strings.Split(entityID, ".")
	for index := range patternParts {
		if patternParts[index] != "*" && patternParts[index] != entityParts[index] {
			return false, nil
		}
	}
	return true, nil
}

// ValidateEntityIDPrefix validates a non-empty one-to-six-position literal
// query prefix. Match-all surfaces must handle empty before calling this API.
func ValidateEntityIDPrefix(value string) error {
	return validateEntityIDValue(value, ErrorCodeEntityIDPrefixInvalid, 1, canonicalEntityIDParts, false)
}

func validateEntityIDValue(value, code string, minimumParts, maximumParts int, allowWildcard bool) error {
	if value == "" {
		return newEntityIDContractError(code, EntityIDReasonEmpty, nil)
	}
	if len(value) > MaxEntityIDBytes {
		return newEntityIDContractError(code, EntityIDReasonBytes, map[string]any{
			EntityIDDetailMeasuredBytes: len(value),
			EntityIDDetailAllowedBytes:  MaxEntityIDBytes,
		})
	}

	parts := strings.Split(value, ".")
	if len(parts) < minimumParts || len(parts) > maximumParts {
		return newEntityIDContractError(code, EntityIDReasonArity, map[string]any{
			EntityIDDetailMeasuredParts: len(parts),
			EntityIDDetailAllowedParts:  maximumParts,
		})
	}
	for index, part := range parts {
		if part == "" {
			return newEntityIDContractError(code, EntityIDReasonEmptySegment, map[string]any{
				EntityIDDetailSegmentIndex: index,
			})
		}
	}
	for index, part := range parts {
		if allowWildcard && part == "*" {
			continue
		}
		if !isEntityIDAlphanumeric(part[0]) {
			return newEntityIDContractError(code, EntityIDReasonFirstByte, map[string]any{
				EntityIDDetailSegmentIndex: index,
			})
		}
	}
	for index, part := range parts {
		if allowWildcard && part == "*" {
			continue
		}
		for position := 1; position < len(part); position++ {
			if !isEntityIDRemainingByte(part[position]) {
				return newEntityIDContractError(code, EntityIDReasonAlphabet, map[string]any{
					EntityIDDetailSegmentIndex: index,
				})
			}
		}
	}
	return nil
}

func isEntityIDAlphanumeric(value byte) bool {
	return value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' || value >= '0' && value <= '9'
}

func isEntityIDRemainingByte(value byte) bool {
	return isEntityIDAlphanumeric(value) || value == '_' || value == '-'
}

func newEntityIDContractError(code, reason string, detail map[string]any) error {
	if detail == nil {
		detail = make(map[string]any, 1)
	}
	detail[EntityIDDetailReason] = reason
	return errs.ClassifiedCodeDetail(errs.ErrorInvalid, code, detail, errInvalidEntityIDContract)
}

// TypePrefix returns the 5-part prefix identifying the entity type level.
// Format: org.platform.domain.system.type
// This groups all instances of the same type (siblings).
//
// Example:
//
//	eid := EntityID{Org: "c360", Platform: "logistics", Domain: "environmental",
//	                System: "sensor", Type: "temperature", Instance: "cold-storage-01"}
//	eid.TypePrefix() // Returns "c360.logistics.environmental.sensor.temperature"
func (eid EntityID) TypePrefix() string {
	return fmt.Sprintf("%s.%s.%s.%s.%s",
		eid.Org, eid.Platform, eid.Domain, eid.System, eid.Type)
}

// SystemPrefix returns the 4-part prefix identifying the system level.
// Format: org.platform.domain.system
// This groups all entity types within the same system.
//
// Example:
//
//	eid := EntityID{Org: "c360", Platform: "logistics", Domain: "environmental",
//	                System: "sensor", Type: "temperature", Instance: "cold-storage-01"}
//	eid.SystemPrefix() // Returns "c360.logistics.environmental.sensor"
func (eid EntityID) SystemPrefix() string {
	return fmt.Sprintf("%s.%s.%s.%s",
		eid.Org, eid.Platform, eid.Domain, eid.System)
}

// DomainPrefix returns the 3-part prefix identifying the domain level.
// Format: org.platform.domain
// This groups all systems within the same domain.
//
// Example:
//
//	eid := EntityID{Org: "c360", Platform: "logistics", Domain: "environmental",
//	                System: "sensor", Type: "temperature", Instance: "cold-storage-01"}
//	eid.DomainPrefix() // Returns "c360.logistics.environmental"
func (eid EntityID) DomainPrefix() string {
	return fmt.Sprintf("%s.%s.%s",
		eid.Org, eid.Platform, eid.Domain)
}

// PlatformPrefix returns the 2-part prefix identifying the platform level.
// Format: org.platform
// This groups all domains within the same platform.
//
// Example:
//
//	eid := EntityID{Org: "c360", Platform: "logistics", Domain: "environmental",
//	                System: "sensor", Type: "temperature", Instance: "cold-storage-01"}
//	eid.PlatformPrefix() // Returns "c360.logistics"
func (eid EntityID) PlatformPrefix() string {
	return fmt.Sprintf("%s.%s", eid.Org, eid.Platform)
}

// HasPrefix checks if this EntityID has the given prefix.
// Used for hierarchical grouping and sibling detection.
//
// Example:
//
//	eid := EntityID{Org: "c360", Platform: "logistics", Domain: "environmental",
//	                System: "sensor", Type: "temperature", Instance: "cold-storage-01"}
//	eid.HasPrefix("c360.logistics.environmental.sensor.temperature") // true (same type)
//	eid.HasPrefix("c360.logistics.environmental.sensor")             // true (same system)
//	eid.HasPrefix("c360.logistics.environmental")                    // true (same domain)
//	eid.HasPrefix("c360.logistics.facility")                         // false (different domain)
func (eid EntityID) HasPrefix(prefix string) bool {
	key := eid.Key()
	// Exact match or prefix with dot separator
	return key == prefix || strings.HasPrefix(key, prefix+".")
}

// IsSibling checks if another EntityID is a sibling (same type-level prefix).
// Siblings are entities of the same type within the same system.
//
// Example:
//
//	sensor1 := EntityID{Org: "c360", Platform: "logistics", Domain: "environmental",
//	                    System: "sensor", Type: "temperature", Instance: "cold-storage-01"}
//	sensor2 := EntityID{Org: "c360", Platform: "logistics", Domain: "environmental",
//	                    System: "sensor", Type: "temperature", Instance: "cold-storage-02"}
//	sensor1.IsSibling(sensor2) // true - same type prefix
//
//	humid := EntityID{Org: "c360", Platform: "logistics", Domain: "environmental",
//	                  System: "sensor", Type: "humidity", Instance: "zone-a"}
//	sensor1.IsSibling(humid) // false - different type
func (eid EntityID) IsSibling(other EntityID) bool {
	return eid.TypePrefix() == other.TypePrefix() && eid.Instance != other.Instance
}

// IsSameSystem checks if another EntityID is in the same system.
// Entities in the same system may have different types but share the system-level prefix.
func (eid EntityID) IsSameSystem(other EntityID) bool {
	return eid.SystemPrefix() == other.SystemPrefix()
}

// IsSameDomain checks if another EntityID is in the same domain.
// Entities in the same domain may have different systems but share the domain-level prefix.
func (eid EntityID) IsSameDomain(other EntityID) bool {
	return eid.DomainPrefix() == other.DomainPrefix()
}
