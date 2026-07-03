// Package graph provides index query data types for the graph query system.
package graph

// --- Index Query Data Types ---

// OutgoingEntry represents a single outgoing relationship in the graph.
type OutgoingEntry struct {
	ToEntityID string `json:"to_entity_id"`
	Predicate  string `json:"predicate"`
}

// IncomingEntry represents a single incoming relationship in the graph.
type IncomingEntry struct {
	FromEntityID string `json:"from_entity_id"`
	Predicate    string `json:"predicate"`
}

// ContextEntry represents an entity-predicate pair indexed by context.
type ContextEntry struct {
	EntityID  string `json:"entity_id"`
	Predicate string `json:"predicate"`
}

// --- Index Query Response Payloads ---

// OutgoingRelationshipsData contains outgoing relationships for an entity.
type OutgoingRelationshipsData struct {
	Relationships []OutgoingEntry `json:"relationships"`
}

// IncomingRelationshipsData contains incoming relationships for an entity.
type IncomingRelationshipsData struct {
	Relationships []IncomingEntry `json:"relationships"`
}

// AliasData contains the canonical entity ID for an alias lookup.
type AliasData struct {
	CanonicalID *string `json:"canonical_id"` // nil if not found
}

// PredicateData contains entities that have a specific predicate.
type PredicateData struct {
	Entities []string `json:"entities"`
}
