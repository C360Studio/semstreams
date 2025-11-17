package message

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/c360/semstreams/component"
)

// init registers the Entity payload type with the global PayloadRegistry.
// This enables BaseMessage.UnmarshalJSON to recreate Entity payloads
// from JSON when the message type is "graph.Entity.v1".
func init() {
	// Register Entity payload factory
	err := component.RegisterPayload(&component.PayloadRegistration{
		Domain:      "graph",
		Category:    "Entity",
		Version:     "v1",
		Description: "Generic entity payload for semantic graph processing",
		Factory: func() any {
			return &EntityPayload{}
		},
		Example: map[string]any{
			"entity_id":   "c360.platform1.robotics.sensor1.device.001",
			"entity_type": "robotics.sensor",
			"properties": map[string]any{
				"temperature": 23.5,
				"status":      "active",
			},
			"class":      "Object",
			"role":       "primary",
			"confidence": 1.0,
		},
	})
	if err != nil {
		panic("failed to register Entity payload: " + err.Error())
	}
}

// EntityPayload represents a generic entity message that implements the Graphable interface.
// This payload type enables conversion from GenericJSON to graph-processable entities.
//
// EntityPayload is designed to bridge the gap between generic JSON processing and
// semantic graph processing, allowing the system to support both lightweight generic
// flows and full semantic entity extraction without forcing all processors to be
// entity-aware.
//
// Design Principles:
//   - Simple entity representation with ID, Type, and Properties
//   - Implements Graphable for automatic graph processor compatibility
//   - Generates semantic triples from properties
//   - Supports metadata for provenance and confidence tracking
type EntityPayload struct {
	// ID is the canonical entity identifier (6-part format: org.platform.domain.system.type.instance)
	ID string `json:"entity_id"`

	// Type is the entity type in dotted notation (domain.type)
	Type string `json:"entity_type"`

	// Properties contains all entity properties as key-value pairs
	Properties map[string]any `json:"properties"`

	// Class provides semantic classification (Object, Event, Agent, Place, Process, Thing)
	Class EntityClass `json:"class,omitempty"`

	// Role indicates how this entity relates to the message (primary, observed, component, etc.)
	Role EntityRole `json:"role,omitempty"`

	// Source identifies where this entity data came from
	Source string `json:"source,omitempty"`

	// Timestamp indicates when this entity data was captured
	Timestamp time.Time `json:"timestamp,omitempty"`

	// Confidence indicates the reliability of this entity data (0.0 to 1.0)
	Confidence float64 `json:"confidence,omitempty"`
}

// Schema implements the Payload interface, returning the message type identifier
func (e *EntityPayload) Schema() Type {
	return Type{
		Domain:   "graph",
		Category: "Entity",
		Version:  "v1",
	}
}

// Validate implements the Payload interface, checking required fields
func (e *EntityPayload) Validate() error {
	if e.ID == "" {
		return fmt.Errorf("entity_id is required")
	}
	if e.Type == "" {
		return fmt.Errorf("entity_type is required")
	}
	if e.Properties == nil {
		return fmt.Errorf("properties cannot be nil")
	}
	return nil
}

// EntityID implements the Graphable interface, returning the entity identifier
func (e *EntityPayload) EntityID() string {
	return e.ID
}

// Triples implements the Graphable interface, converting properties to semantic triples
func (e *EntityPayload) Triples() []Triple {
	triples := make([]Triple, 0, len(e.Properties)+2)

	// Use current time if not specified
	ts := e.Timestamp
	if ts.IsZero() {
		ts = time.Now()
	}

	// Default confidence to 1.0 if not specified
	confidence := e.Confidence
	if confidence == 0.0 {
		confidence = 1.0
	}

	// Add rdf:type triple for entity classification
	triples = append(triples, Triple{
		Subject:    e.ID,
		Predicate:  "rdf:type",
		Object:     e.Type,
		Source:     e.Source,
		Timestamp:  ts,
		Confidence: confidence,
	})

	// Add entity class if specified
	if e.Class != "" && e.Class.IsValid() {
		triples = append(triples, Triple{
			Subject:    e.ID,
			Predicate:  "rdf:class",
			Object:     e.Class.String(),
			Source:     e.Source,
			Timestamp:  ts,
			Confidence: confidence,
		})
	}

	// Convert all properties to triples
	for key, value := range e.Properties {
		// Create predicate from entity type and property key
		// Format: domain.type.property (e.g., "robotics.drone.battery_level")
		predicate := fmt.Sprintf("%s.%s", e.Type, key)

		triples = append(triples, Triple{
			Subject:    e.ID,
			Predicate:  predicate,
			Object:     value,
			Source:     e.Source,
			Timestamp:  ts,
			Confidence: confidence,
		})
	}

	return triples
}

// MarshalJSON serializes the Entity payload to JSON format
func (e *EntityPayload) MarshalJSON() ([]byte, error) {
	// Use alias to avoid infinite recursion
	type Alias EntityPayload
	return json.Marshal((*Alias)(e))
}

// UnmarshalJSON deserializes JSON data into the Entity payload
func (e *EntityPayload) UnmarshalJSON(data []byte) error {
	// Use alias to avoid infinite recursion
	type Alias EntityPayload
	return json.Unmarshal(data, (*Alias)(e))
}

// NewEntityPayload creates a new EntityPayload with sensible defaults
func NewEntityPayload(id, entityType string, properties map[string]any) *EntityPayload {
	return &EntityPayload{
		ID:         id,
		Type:       entityType,
		Properties: properties,
		Confidence: 1.0,
		Timestamp:  time.Now(),
	}
}
