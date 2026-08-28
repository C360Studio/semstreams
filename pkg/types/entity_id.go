package types

import (
	"errors"
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

// EntityID is one complete entity identity in the canonical order
// org.platform.system.domain.type.instance (ADR-102). Each position has one
// meaning and one owner:
//
//	pos  name      meaning                                   supplied by
//	1    org       organization namespace                    platform.org (config)
//	2    platform  minting deployment authority              platform.id (config, via deps.Platform)
//	3    system    the source that produced the entity       the producer (feed, repo, world, framework component)
//	4    domain    delegated taxonomy                        a registered EntityDomainDelegation, or a framework-reserved domain
//	5    type      entity type within the domain             the same delegation as domain
//	6    instance  the producer's leaf identifier            the producer; the only unbounded-cardinality position, always last
//
// Positions 1-2 are never a payload value, a constant, or a product name; a
// product is provenance (Triple.Source, the envelope source), not identity.
//
// Examples:
//   - EntityID{Org: "acme", Platform: "dep1", System: "src", Domain: "git", Type: "commit", Instance: "a1"} -> "acme.dep1.src.git.commit.a1"
//   - EntityID{Org: "acme", Platform: "dep1", System: "agentic-loop", Domain: "agent", Type: "execution", Instance: "<uuid>"}
type EntityID struct {
	// Deployment authority (2 parts): the composition root's own identity.
	Org      string // Organization namespace (e.g., "acme")
	Platform string // Minting deployment authority (e.g., "dep1"), from platform.id

	// Source (1 part): who produced the entity; never the product name.
	System string // Source (e.g., "src", "gcs1", "agentic-loop")

	// Taxonomy (2 parts): a delegated domain and its type.
	Domain string // Delegated taxonomy (e.g., "git", "agent")
	Type   string // Entity type within the domain (e.g., "commit", "execution")

	// Leaf (1 part): unbounded cardinality, always last.
	Instance string // Leaf identifier (e.g., "a1", a UUID, a digest)
}

// Key returns the canonical six-part dotted key in
// org.platform.system.domain.type.instance order. It implements the Keyable
// interface for unified semantic keys.
func (eid EntityID) Key() string {
	return eid.Org + "." + eid.Platform + "." + eid.System + "." + eid.Domain + "." + eid.Type + "." + eid.Instance
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
// Expects exactly 6 parts in the canonical order
// org.platform.system.domain.type.instance and assigns every field from its
// named position. Returns a coded error if the format is invalid.
func ParseEntityID(s string) (EntityID, error) {
	if err := ValidateEntityID(s); err != nil {
		return EntityID{}, err
	}
	parts := strings.Split(s, ".")
	return EntityID{
		Org:      parts[0],
		Platform: parts[1],
		System:   parts[2],
		Domain:   parts[3],
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

// Prefix levels are named methods, not a level vocabulary: a prefix of length
// n means exactly the level named for n (ADR-102 d6), and grouping by a
// non-prefix combination — a taxonomy across sources — is an exact-arity
// wildcard pattern or KV filter, never a prefix. The exported level constants
// and EntityID.PrefixLevel(n) that once accompanied these were deleted by the
// owner ruling of 2026-08-28: they had no consumer. ADR-099/#606 re-adds a
// level vocabulary when it has one.

// DeploymentPrefix returns the two-position prefix org.platform.
func (eid EntityID) DeploymentPrefix() string {
	return eid.Org + "." + eid.Platform
}

// SourcePrefix returns the three-position prefix org.platform.system — the
// federation triple.
func (eid EntityID) SourcePrefix() string {
	return eid.DeploymentPrefix() + "." + eid.System
}

// TaxonomyPrefix returns the four-position prefix org.platform.system.domain.
func (eid EntityID) TaxonomyPrefix() string {
	return eid.SourcePrefix() + "." + eid.Domain
}

// TypePrefix returns the five-position prefix org.platform.system.domain.type
// shared by every instance of one type (siblings).
func (eid EntityID) TypePrefix() string {
	return eid.TaxonomyPrefix() + "." + eid.Type
}

// HasPrefix reports whether this EntityID lies under the given literal prefix
// on a position boundary.
//
// Example:
//
//	eid := EntityID{Org: "acme", Platform: "dep1", System: "src", Domain: "git", Type: "commit", Instance: "a1"}
//	eid.HasPrefix("acme.dep1.src.git.commit") // true (same type)
//	eid.HasPrefix("acme.dep1.src")            // true (same source)
//	eid.HasPrefix("acme.dep1.other")          // false (different source)
func (eid EntityID) HasPrefix(prefix string) bool {
	key := eid.Key()
	// Exact match or prefix with dot separator
	return key == prefix || strings.HasPrefix(key, prefix+".")
}

// IsSibling reports whether other shares this EntityID's type prefix and is
// not the same instance.
func (eid EntityID) IsSibling(other EntityID) bool {
	return eid.TypePrefix() == other.TypePrefix() && eid.Instance != other.Instance
}

// IsSameSource reports whether other shares this EntityID's source prefix
// (org.platform.system).
func (eid EntityID) IsSameSource(other EntityID) bool {
	return eid.SourcePrefix() == other.SourcePrefix()
}
