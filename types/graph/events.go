// Package graph provides event types for graph mutation requests from rules.
package graph

import (
	"fmt"
	"strings"
	"time"

	"github.com/c360/semstreams/errors"
)

// Event represents a graph mutation request from rules.
// This enables an event-driven architecture where rules emit events
// instead of directly mutating the graph, allowing for better decoupling,
// auditability, and potential event replay functionality.
type Event struct {
	Type       EventType              `json:"type"`       // Event type enum
	EntityID   string                 `json:"entity_id"`  // Primary entity ID
	TargetID   string                 `json:"target_id"`  // Target entity ID (for relationships)
	Properties map[string]any `json:"properties"` // Properties to set/update
	Metadata   EventMetadata          `json:"metadata"`   // Event metadata
	Confidence float64                `json:"confidence"` // Confidence score (0.0-1.0)
}

// EventType defines types of graph events that can be emitted by rules.
type EventType string

const (
	// EventEntityCreate represents a request to create a new entity in the graph.
	EventEntityCreate EventType = "entity_create"

	// EventEntityUpdate represents a request to update an existing entity's properties.
	EventEntityUpdate EventType = "entity_update"

	// EventEntityDelete represents a request to delete an entity from the graph.
	EventEntityDelete EventType = "entity_delete"

	// EventRelationshipCreate represents a request to create a relationship between entities.
	EventRelationshipCreate EventType = "relationship_create"

	// EventRelationshipDelete represents a request to delete a relationship between entities.
	EventRelationshipDelete EventType = "relationship_delete"
)

// EventMetadata contains metadata about the event source and context.
// This information is crucial for debugging, auditing, and understanding
// the decision-making process of the rule system.
type EventMetadata struct {
	RuleName  string    `json:"rule_name"`  // Name of the rule that generated this event
	Timestamp time.Time `json:"timestamp"`  // When the event was generated
	Source    string    `json:"source"`     // Component that generated the event
	Reason    string    `json:"reason"`     // Human-readable reason for the event
	Version   string    `json:"version"`    // Event schema version (default "1.0.0")
}

// Validate checks if the Event is valid and contains all required fields.
// Returns an error describing any validation failures.
func (e *Event) Validate() error {
	if e.Type == "" {
		return errors.WrapInvalid(errors.ErrInvalidData, "Event", "Validate", "event type is required")
	}

	if e.EntityID == "" {
		return errors.WrapInvalid(errors.ErrInvalidData, "Event", "Validate", "entity ID is required")
	}

	// Validate confidence range
	if e.Confidence < 0.0 || e.Confidence > 1.0 {
		return errors.WrapInvalid(errors.ErrInvalidData, "Event", "Validate",
			fmt.Sprintf("confidence must be between 0.0 and 1.0, got %f", e.Confidence))
	}

	// Validate relationship events have target ID
	if (e.Type == EventRelationshipCreate || e.Type == EventRelationshipDelete) && e.TargetID == "" {
		return errors.WrapInvalid(errors.ErrInvalidData, "Event", "Validate", "target ID is required for relationship events")
	}

	// Validate metadata
	if e.Metadata.RuleName == "" {
		return errors.WrapInvalid(errors.ErrInvalidData, "Event", "Validate", "rule name is required in metadata")
	}

	if e.Metadata.Timestamp.IsZero() {
		return errors.WrapInvalid(errors.ErrInvalidData, "Event", "Validate", "timestamp is required in metadata")
	}

	if e.Metadata.Source == "" {
		return errors.WrapInvalid(errors.ErrInvalidData, "Event", "Validate", "source is required in metadata")
	}

	// Set default version if not specified
	if e.Metadata.Version == "" {
		e.Metadata.Version = "1.0.0"
	}

	return nil
}

// Subject returns the NATS subject for this event type.
// This follows a hierarchical naming pattern that allows for selective subscription
// to specific event types or all graph events.
func (e *Event) Subject() string {
	return fmt.Sprintf("graph.events.%s", strings.ReplaceAll(string(e.Type), "_", "."))
}

// EventType returns the event type as a string.
// This method implements the rule.Event interface, allowing graph events
// to be used generically by the rule processor.
func (e *Event) EventType() string {
	return string(e.Type)
}

// Payload returns the event data as a generic map.
// This method implements the rule.Event interface, providing access to
// the event's properties in a generic format.
func (e *Event) Payload() map[string]any {
	// Return a map containing all event fields for generic handling
	payload := map[string]any{
		"entity_id":  e.EntityID,
		"confidence": e.Confidence,
		"metadata":   e.Metadata,
	}

	// Add target ID for relationship events
	if e.TargetID != "" {
		payload["target_id"] = e.TargetID
	}

	// Merge in properties
	for k, v := range e.Properties {
		payload[k] = v
	}

	return payload
}

// NewEntityUpdateEvent creates an entity update event with the specified parameters.
// This is a convenience constructor for the common case of updating entity properties.
func NewEntityUpdateEvent(entityID string, properties map[string]any, metadata EventMetadata) *Event {
	return &Event{
		Type:       EventEntityUpdate,
		EntityID:   entityID,
		Properties: properties,
		Metadata:   metadata,
		Confidence: 1.0, // Default to high confidence for direct updates
	}
}

// NewRelationshipCreateEvent creates a relationship creation event between two entities.
// The relationshipType should be a descriptive string like "POWERED_BY", "NEAR", etc.
func NewRelationshipCreateEvent(fromID, toID string, relationshipType string, metadata EventMetadata) *Event {
	return &Event{
		Type:     EventRelationshipCreate,
		EntityID: fromID,
		TargetID: toID,
		Properties: map[string]any{
			"edge_type": relationshipType,
		},
		Metadata:   metadata,
		Confidence: 1.0, // Default to high confidence for explicit relationships
	}
}

// NewAlertEvent creates an alert entity in the graph.
// This is commonly used by rules to create alert entities when conditions are detected.
// The alertType should be descriptive (e.g., "battery_low", "temperature_high").
func NewAlertEvent(alertType string, entityID string, properties map[string]any, metadata EventMetadata) *Event {
	// Ensure alert-specific properties are set
	if properties == nil {
		properties = make(map[string]any)
	}
	
	properties["alert_type"] = alertType
	properties["source_entity"] = entityID
	properties["status"] = "warning" // Default to warning status
	
	// Generate unique alert ID based on type, entity, and timestamp
	alertID := fmt.Sprintf("alert_%s_%s_%d", alertType, entityID, metadata.Timestamp.Unix())
	
	return &Event{
		Type:       EventEntityCreate,
		EntityID:   alertID,
		Properties: properties,
		Metadata:   metadata,
		Confidence: 0.8, // Slightly lower confidence for derived alert entities
	}
}

// NewEntityCreateEvent creates an entity creation event.
// This is used when rules determine that a new entity should be created in the graph.
func NewEntityCreateEvent(entityID string, entityType string, properties map[string]any, metadata EventMetadata) *Event {
	if properties == nil {
		properties = make(map[string]any)
	}
	
	properties["type"] = entityType
	
	return &Event{
		Type:       EventEntityCreate,
		EntityID:   entityID,
		Properties: properties,
		Metadata:   metadata,
		Confidence: 1.0, // Default to high confidence for explicit creation
	}
}

// NewEntityDeleteEvent creates an entity deletion event.
// This is used when rules determine that an entity should be removed from the graph.
func NewEntityDeleteEvent(entityID string, reason string, metadata EventMetadata) *Event {
	metadata.Reason = reason // Override reason with deletion-specific reason
	
	return &Event{
		Type:       EventEntityDelete,
		EntityID:   entityID,
		Properties: make(map[string]any), // Empty properties for deletion
		Metadata:   metadata,
		Confidence: 1.0, // Default to high confidence for explicit deletion
	}
}

// NewRelationshipDeleteEvent creates a relationship deletion event.
// This removes a relationship between two entities based on the relationship type.
func NewRelationshipDeleteEvent(fromID, toID string, relationshipType string, metadata EventMetadata) *Event {
	return &Event{
		Type:     EventRelationshipDelete,
		EntityID: fromID,
		TargetID: toID,
		Properties: map[string]any{
			"edge_type": relationshipType,
		},
		Metadata:   metadata,
		Confidence: 1.0, // Default to high confidence for explicit deletion
	}
}