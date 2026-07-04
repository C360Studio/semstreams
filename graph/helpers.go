// Package graph provides helper functions for semantic triples-based property access.
// These helpers enable clean migration from dual Properties/Triples representation
// to pure semantic triples as the single source of truth.
package graph

import (
	"github.com/c360studio/semstreams/message"
)

// GetPropertyValue retrieves a property value from entity triples by predicate.
// Returns the value and true if found, nil and false if not found.
// Only searches non-relationship triples (property triples).
func GetPropertyValue(entity *EntityState, predicate string) (any, bool) {
	if entity == nil {
		return nil, false
	}

	for _, triple := range entity.Triples {
		if triple.Predicate == predicate && !triple.IsRelationship() {
			return triple.Object, true
		}
	}
	return nil, false
}

// GetPropertyValueTyped retrieves a property value with type assertion.
// Returns the typed value and true if found and type matches, zero value and false otherwise.
func GetPropertyValueTyped[T any](entity *EntityState, predicate string) (T, bool) {
	var zero T
	value, found := GetPropertyValue(entity, predicate)
	if !found {
		return zero, false
	}

	typedValue, ok := value.(T)
	if !ok {
		return zero, false
	}

	return typedValue, true
}

// GetProperties computes a properties map from entity triples.
// Only includes non-relationship triples (property triples).
// This enables backward compatibility during migration.
func GetProperties(entity *EntityState) map[string]any {
	if entity == nil {
		return make(map[string]any)
	}

	props := make(map[string]any)
	for _, triple := range entity.Triples {
		if !triple.IsRelationship() {
			props[triple.Predicate] = triple.Object
		}
	}
	return props
}

// GetRelationshipTriples returns only the relationship triples from entity state.
// These represent edges/connections to other entities.
func GetRelationshipTriples(entity *EntityState) []message.Triple {
	if entity == nil {
		return nil
	}

	var relationships []message.Triple
	for _, triple := range entity.Triples {
		if triple.IsRelationship() {
			relationships = append(relationships, triple)
		}
	}
	return relationships
}

// GetPropertyTriples returns only the property triples from entity state.
// These represent entity attributes/properties.
func GetPropertyTriples(entity *EntityState) []message.Triple {
	if entity == nil {
		return nil
	}

	var properties []message.Triple
	for _, triple := range entity.Triples {
		if !triple.IsRelationship() {
			properties = append(properties, triple)
		}
	}
	return properties
}

// HasProperty checks if entity has a property with the given predicate.
func HasProperty(entity *EntityState, predicate string) bool {
	_, found := GetPropertyValue(entity, predicate)
	return found
}

// MergeTriples merges two triple slices by REPLACING per (subject, predicate):
// every existing triple whose (subject, predicate) also appears in newer is
// dropped, and all of newer is kept. So for a single-valued property the newer
// value overrides the older one, and for a MULTI-valued predicate (a relationship
// such as flock.neighbor, where one subject holds several triples with the same
// predicate) the entire prior set is replaced by newer's set — this is
// full-set-replace, NOT preserve-all-unique-relationships. A predicate absent
// from newer is preserved untouched. Callers that want to accumulate values over
// time must publish the complete set per write (or use a different mechanism); a
// partial write drops the omitted objects for that predicate. (gh#466)
func MergeTriples(existing, newer []message.Triple) []message.Triple {
	if len(existing) == 0 {
		return newer
	}
	if len(newer) == 0 {
		return existing
	}

	// Start with newer triples (they take precedence)
	merged := make([]message.Triple, 0, len(existing)+len(newer))
	merged = append(merged, newer...)

	// Add existing triples that don't conflict
	for _, existingTriple := range existing {
		hasConflict := false

		// Check if this triple conflicts with any newer triple
		for _, newerTriple := range newer {
			if existingTriple.Subject == newerTriple.Subject &&
				existingTriple.Predicate == newerTriple.Predicate {
				// Conflict found - newer triple wins
				hasConflict = true
				break
			}
		}

		// Only add if no conflict
		if !hasConflict {
			merged = append(merged, existingTriple)
		}
	}

	return merged
}
