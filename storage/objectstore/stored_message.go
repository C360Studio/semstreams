package objectstore

import (
	"encoding/json"
	"time"

	"github.com/c360/semstreams/component"
	"github.com/c360/semstreams/errors"
	"github.com/c360/semstreams/message"
)

func init() {
	// Register StoredMessage for proper unmarshaling in graph processors
	component.RegisterPayload(&component.PayloadRegistration{
		Factory: func() interface{} {
			return &StoredMessage{}
		},
		Domain:      "storage",
		Category:    "stored",
		Version:     "v1",
		Description: "Message with semantic data and storage reference",
	})
}

// StoredMessage implements message.Storable interface for messages that have been
// persisted to ObjectStore. It combines the semantic data (Graphable) with
// a storage reference pointing to the full message.
//
// This enables the "store once, reference everywhere" pattern where:
//   - ObjectStore stores the full raw message
//   - Graph processor receives semantic data plus storage reference
//   - Components can retrieve full message via ObjectStore API if needed
type StoredMessage struct {
	// Semantic data from the original message
	entityID string
	triples  []message.Triple

	// Reference to the stored full message
	storageRef *message.StorageReference

	// Metadata about storage
	storedAt    time.Time
	messageType string
}

// NewStoredMessage creates a StoredMessage from a Graphable and storage metadata
func NewStoredMessage(graphable message.Graphable, storageRef *message.StorageReference, messageType string) *StoredMessage {
	return &StoredMessage{
		entityID:    graphable.EntityID(),
		triples:     graphable.Triples(),
		storageRef:  storageRef,
		storedAt:    time.Now(),
		messageType: messageType,
	}
}

// EntityID implements message.Graphable interface
func (s *StoredMessage) EntityID() string {
	return s.entityID
}

// Triples implements message.Graphable interface
func (s *StoredMessage) Triples() []message.Triple {
	return s.triples
}

// StorageRef implements message.Storable interface
func (s *StoredMessage) StorageRef() *message.StorageReference {
	return s.storageRef
}

// StoredAt returns when the message was stored
func (s *StoredMessage) StoredAt() time.Time {
	return s.storedAt
}

// MessageType returns the original message type
func (s *StoredMessage) MessageType() string {
	return s.messageType
}

// MarshalJSON provides JSON serialization for StoredMessage
func (s *StoredMessage) MarshalJSON() ([]byte, error) {
	// Create a map for JSON representation
	data := map[string]interface{}{
		"entity_id":    s.entityID,
		"triples":      s.triples,
		"storage_ref":  s.storageRef,
		"stored_at":    s.storedAt,
		"message_type": s.messageType,
	}

	return json.Marshal(data)
}

// UnmarshalJSON provides JSON deserialization for StoredMessage
func (s *StoredMessage) UnmarshalJSON(data []byte) error {
	var tmp struct {
		EntityID    string                   `json:"entity_id"`
		Triples     []message.Triple         `json:"triples"`
		StorageRef  *message.StorageReference `json:"storage_ref"`
		StoredAt    time.Time                `json:"stored_at"`
		MessageType string                   `json:"message_type"`
	}

	if err := json.Unmarshal(data, &tmp); err != nil {
		return err
	}

	s.entityID = tmp.EntityID
	s.triples = tmp.Triples
	s.storageRef = tmp.StorageRef
	s.storedAt = tmp.StoredAt
	s.messageType = tmp.MessageType

	return nil
}

// Schema implements message.Payload interface
func (s *StoredMessage) Schema() message.Type {
	// Return a type indicating this is a stored message
	return message.Type{
		Domain:   "storage",
		Category: "stored",
		Version:  "v1",
	}
}

// Validate implements message.Payload interface
func (s *StoredMessage) Validate() error {
	if s.entityID == "" {
		return errors.WrapInvalid(errors.ErrInvalidData, "StoredMessage", "Validate", "entity_id is required")
	}
	if s.storageRef == nil {
		return errors.WrapInvalid(errors.ErrInvalidData, "StoredMessage", "Validate", "storage_ref is required")
	}
	return nil
}

// Ensure StoredMessage implements both message.Storable and message.Payload
var _ message.Storable = (*StoredMessage)(nil)
var _ message.Payload = (*StoredMessage)(nil)
