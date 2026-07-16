// Package graph provides event types for graph mutation requests from rules.
package graph

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/c360studio/semstreams/pkg/errs"
	semtypes "github.com/c360studio/semstreams/pkg/types"
)

const (
	eventMetadataVersion = "1.0.0"
	alertDigestDomain    = "semstreams.graph.alert.v1"
	alertEntityPrefix    = "semstreams.framework.graph.rules.alert."
)

var envelopePropertyKeys = map[string]struct{}{
	"entity_id":  {},
	"target_id":  {},
	"confidence": {},
	"metadata":   {},
}

// Event represents a graph mutation request from rules.
type Event struct {
	Type       EventType      `json:"type"`
	EntityID   string         `json:"entity_id"`
	TargetID   string         `json:"target_id"`
	Properties map[string]any `json:"properties"`
	Metadata   EventMetadata  `json:"metadata"`
	Confidence float64        `json:"confidence"`
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
type EventMetadata struct {
	RuleName  string    `json:"rule_name"`
	Timestamp time.Time `json:"timestamp"`
	Source    string    `json:"source"`
	Reason    string    `json:"reason"`
	Version   string    `json:"version"`
}

// Validate checks the complete event contract without mutating the receiver.
func (e *Event) Validate() error {
	if e == nil {
		return invalidEvent("event is nil")
	}
	if !knownEventType(e.Type) {
		return invalidEvent(fmt.Sprintf("unknown event type %q", e.Type))
	}
	if err := semtypes.ValidateEntityID(e.EntityID); err != nil {
		return errs.WrapInvalid(err, "Event", "Validate", "validate entity ID")
	}

	switch e.Type {
	case EventRelationshipCreate, EventRelationshipDelete:
		if err := semtypes.ValidateEntityID(e.TargetID); err != nil {
			return errs.WrapInvalid(err, "Event", "Validate", "validate relationship target ID")
		}
	case EventEntityCreate, EventEntityUpdate, EventEntityDelete:
		if e.TargetID != "" {
			return invalidEvent("target ID is forbidden for entity events")
		}
	}

	if math.IsNaN(e.Confidence) || math.IsInf(e.Confidence, 0) || e.Confidence < 0 || e.Confidence > 1 {
		return invalidEvent(fmt.Sprintf("confidence must be finite and between 0.0 and 1.0, got %v", e.Confidence))
	}
	if e.Metadata.RuleName == "" {
		return invalidEvent("rule name is required in metadata")
	}
	if e.Metadata.Timestamp.IsZero() {
		return invalidEvent("timestamp is required in metadata")
	}
	if e.Metadata.Source == "" {
		return invalidEvent("source is required in metadata")
	}
	if e.Metadata.Reason == "" {
		return invalidEvent("reason is required in metadata")
	}
	if e.Metadata.Version != eventMetadataVersion {
		return invalidEvent(fmt.Sprintf("metadata version must be %q, got %q", eventMetadataVersion, e.Metadata.Version))
	}
	for key := range e.Properties {
		if _, reserved := envelopePropertyKeys[key]; reserved {
			return invalidEvent(fmt.Sprintf("property key %q is reserved for the event envelope", key))
		}
	}
	return nil
}

func knownEventType(eventType EventType) bool {
	switch eventType {
	case EventEntityCreate, EventEntityUpdate, EventEntityDelete, EventRelationshipCreate, EventRelationshipDelete:
		return true
	default:
		return false
	}
}

func invalidEvent(detail string) error {
	return errs.WrapInvalid(errs.ErrInvalidData, "Event", "Validate", detail)
}

// Subject returns the NATS subject for this event type.
func (e *Event) Subject() string {
	return fmt.Sprintf("graph.events.%s", strings.ReplaceAll(string(e.Type), "_", "."))
}

// EventType returns the event type as a string.
func (e *Event) EventType() string {
	return string(e.Type)
}

// Payload returns the event data as a generic map.
func (e *Event) Payload() map[string]any {
	payload := map[string]any{
		"entity_id":  e.EntityID,
		"confidence": e.Confidence,
		"metadata":   e.Metadata,
	}
	if e.TargetID != "" {
		payload["target_id"] = e.TargetID
	}
	for key, value := range e.Properties {
		payload[key] = value
	}
	return payload
}

// NewEntityUpdateEvent creates and validates an entity update event.
func NewEntityUpdateEvent(entityID string, properties map[string]any, metadata EventMetadata) (*Event, error) {
	return newEvent(EventEntityUpdate, entityID, "", properties, metadata, 1)
}

// NewRelationshipCreateEvent creates and validates a relationship creation event.
func NewRelationshipCreateEvent(
	fromID, toID, relationshipType string,
	metadata EventMetadata,
) (*Event, error) {
	properties, err := propertiesWithOwned(nil, "edge_type", relationshipType)
	if err != nil {
		return nil, err
	}
	return newEvent(EventRelationshipCreate, fromID, toID, properties, metadata, 1)
}

// NewAlertEvent creates and validates a deterministic framework-owned alert entity.
func NewAlertEvent(
	alertType, sourceEntityID string,
	properties map[string]any,
	metadata EventMetadata,
) (*Event, error) {
	if err := semtypes.ValidateEntityID(sourceEntityID); err != nil {
		return nil, errs.WrapInvalid(err, "Event", "NewAlertEvent", "validate source entity ID")
	}
	if alertType == "" {
		return nil, invalidEvent("alert type is required")
	}
	var err error
	properties, err = propertiesWithOwned(properties,
		"alert_type", alertType,
		"source_entity", sourceEntityID,
		"status", "warning",
	)
	if err != nil {
		return nil, err
	}
	metadata = defaultMetadataVersion(metadata)
	alertID := alertEntityPrefix + alertInstance(sourceEntityID, alertType, metadata)
	return newEvent(EventEntityCreate, alertID, "", properties, metadata, 0.8)
}

// NewEntityCreateEvent creates and validates an entity creation event.
func NewEntityCreateEvent(
	entityID, entityType string,
	properties map[string]any,
	metadata EventMetadata,
) (*Event, error) {
	properties, err := propertiesWithOwned(properties, "type", entityType)
	if err != nil {
		return nil, err
	}
	return newEvent(EventEntityCreate, entityID, "", properties, metadata, 1)
}

// NewEntityDeleteEvent creates and validates an entity deletion event.
func NewEntityDeleteEvent(entityID, reason string, metadata EventMetadata) (*Event, error) {
	metadata.Reason = reason
	return newEvent(EventEntityDelete, entityID, "", nil, metadata, 1)
}

// NewRelationshipDeleteEvent creates and validates a relationship deletion event.
func NewRelationshipDeleteEvent(
	fromID, toID, relationshipType string,
	metadata EventMetadata,
) (*Event, error) {
	properties, err := propertiesWithOwned(nil, "edge_type", relationshipType)
	if err != nil {
		return nil, err
	}
	return newEvent(EventRelationshipDelete, fromID, toID, properties, metadata, 1)
}

func newEvent(
	eventType EventType,
	entityID, targetID string,
	properties map[string]any,
	metadata EventMetadata,
	confidence float64,
) (*Event, error) {
	metadata = defaultMetadataVersion(metadata)
	properties, err := copyProperties(properties)
	if err != nil {
		return nil, err
	}
	event := &Event{
		Type:       eventType,
		EntityID:   entityID,
		TargetID:   targetID,
		Properties: properties,
		Metadata:   metadata,
		Confidence: confidence,
	}
	if err := event.Validate(); err != nil {
		return nil, err
	}
	return event, nil
}

func defaultMetadataVersion(metadata EventMetadata) EventMetadata {
	if metadata.Version == "" {
		metadata.Version = eventMetadataVersion
	}
	return metadata
}

func copyProperties(properties map[string]any) (map[string]any, error) {
	result := make(map[string]any, len(properties))
	for key, value := range properties {
		if _, reserved := envelopePropertyKeys[key]; reserved {
			return nil, invalidEvent(fmt.Sprintf("property key %q is reserved for the event envelope", key))
		}
		result[key] = value
	}
	return result, nil
}

func propertiesWithOwned(properties map[string]any, owned ...any) (map[string]any, error) {
	result, err := copyProperties(properties)
	if err != nil {
		return nil, err
	}
	for index := 0; index < len(owned); index += 2 {
		key := owned[index].(string)
		if _, exists := result[key]; exists {
			return nil, invalidEvent(fmt.Sprintf("property key %q is owned by the constructor", key))
		}
		value := owned[index+1]
		if text, ok := value.(string); ok && text == "" {
			return nil, invalidEvent(fmt.Sprintf("constructor property %q is required", key))
		}
		result[key] = value
	}
	return result, nil
}

func alertInstance(sourceEntityID, alertType string, metadata EventMetadata) string {
	digest := sha256.New()
	writeFramedString(digest, alertDigestDomain)
	writeFramedString(digest, sourceEntityID)
	writeFramedString(digest, alertType)
	writeFramedString(digest, metadata.RuleName)
	writeFramedString(digest, metadata.Source)
	var timestamp [12]byte
	binary.BigEndian.PutUint64(timestamp[:8], uint64(metadata.Timestamp.Unix()))
	binary.BigEndian.PutUint32(timestamp[8:], uint32(metadata.Timestamp.Nanosecond()))
	_, _ = digest.Write(timestamp[:])
	return hex.EncodeToString(digest.Sum(nil))
}

type byteWriter interface {
	Write([]byte) (int, error)
}

func writeFramedString(destination byteWriter, value string) {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(value)))
	_, _ = destination.Write(length[:])
	_, _ = destination.Write([]byte(value))
}
