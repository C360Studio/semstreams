// Package graph provides types for entity state storage in the graph system.
package graph

import (
	"context"
	"time"

	"github.com/c360studio/semstreams/message"
)

// EntityState represents complete local graph state for an entity.
// Triples are the single source of truth for all semantic properties.
//
// The ID field is the 6-part entity identifier (org.platform.domain.system.type.instance)
// which serves as the NATS KV key for storage and retrieval.
//
// To extract type information from the ID, use message.ParseEntityID():
//
//	eid, err := message.ParseEntityID(state.ID)
//	if err != nil {
//	    return fmt.Errorf("invalid entity ID: %w", err)
//	}
//	entityType := eid.Type
type EntityState struct {
	// ID is the 6-part entity identifier: org.platform.domain.system.type.instance
	// Used as NATS KV key for storage and retrieval.
	ID string `json:"id"`

	// Triples contains all semantic facts about this entity.
	// Properties, relationships, and domain-specific data are all stored as triples.
	Triples []message.Triple `json:"triples"`

	// StorageRef optionally points to where the full original message is stored.
	// Supports "store once, reference anywhere" pattern for large payloads.
	// Nil if message was not stored or storage reference not available.
	StorageRef *message.StorageReference `json:"storage_ref,omitempty"`

	// MessageType records the original message type that created/updated this entity.
	// Provides provenance and enables filtering by message source.
	MessageType message.Type `json:"message_type"`

	// Version is incremented on each update for optimistic concurrency control.
	Version uint64 `json:"version"`

	// UpdatedAt records when this entity state was last modified.
	UpdatedAt time.Time `json:"updated_at"`
}

// GetTriple returns the first triple matching the given predicate.
// Returns nil if no matching triple is found.
// This helper method simplifies accessing triple-based properties.
func (es *EntityState) GetTriple(predicate string) *message.Triple {
	if es == nil {
		return nil
	}
	for i := range es.Triples {
		if es.Triples[i].Predicate == predicate {
			return &es.Triples[i]
		}
	}
	return nil
}

// GetPropertyValue returns the value for a property by predicate.
// It checks Triples for a matching predicate and returns the Object value.
// Returns (value, true) if found, (nil, false) if not found.
func (es *EntityState) GetPropertyValue(predicate string) (any, bool) {
	if es == nil {
		return nil, false
	}

	triple := es.GetTriple(predicate)
	if triple != nil {
		return triple.Object, true
	}

	return nil, false
}

// Provider abstracts the graph data source for algorithms.
// Used by clustering, structural indexing, and other graph operations.
type Provider interface {
	// GetAllEntityIDs returns all entity IDs in the graph.
	GetAllEntityIDs(ctx context.Context) ([]string, error)

	// GetNeighbors returns the entity IDs connected to the given entity.
	// direction: "outgoing", "incoming", or "both"
	GetNeighbors(ctx context.Context, entityID string, direction string) ([]string, error)

	// GetEdgeWeight returns the weight of the edge between two entities.
	// Returns 1.0 if edge exists but has no weight, 0.0 if no edge exists.
	GetEdgeWeight(ctx context.Context, fromID, toID string) (float64, error)
}

// Clone returns a deep copy of the EntityState.
// This is used to avoid race conditions when multiple goroutines
// process the same entity concurrently.
func (es *EntityState) Clone() *EntityState {
	if es == nil {
		return nil
	}

	clone := &EntityState{
		ID:          es.ID,
		MessageType: es.MessageType,
		Version:     es.Version,
		UpdatedAt:   es.UpdatedAt,
	}

	// Deep copy triples slice
	if es.Triples != nil {
		clone.Triples = make([]message.Triple, len(es.Triples))
		copy(clone.Triples, es.Triples)
	}

	// Deep copy storage reference
	if es.StorageRef != nil {
		clone.StorageRef = &message.StorageReference{
			StorageInstance: es.StorageRef.StorageInstance,
			Key:             es.StorageRef.Key,
			ContentType:     es.StorageRef.ContentType,
			Size:            es.StorageRef.Size,
		}
	}

	return clone
}
