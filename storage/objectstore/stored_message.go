package objectstore

import (
	"encoding/json"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/payloadregistry"
	"github.com/c360studio/semstreams/pkg/errs"
	semtypes "github.com/c360studio/semstreams/pkg/types"
)

func buildStoredMessage(fields map[string]any) (any, error) {
	msg := &StoredMessage{}

	if v, ok := fields["entity_id"].(string); ok {
		msg.entityID = v
	}
	if v, ok := fields["message_type"].(string); ok {
		msg.messageType = v
	}

	// Handle triples slice
	if v, ok := fields["triples"].([]any); ok {
		msg.triples = make([]message.Triple, len(v))
		for i, item := range v {
			if tripleMap, ok := item.(map[string]any); ok {
				if subj, ok := tripleMap["subject"].(string); ok {
					msg.triples[i].Subject = subj
				}
				if pred, ok := tripleMap["predicate"].(string); ok {
					msg.triples[i].Predicate = pred
				}
				if obj, ok := tripleMap["object"]; ok {
					msg.triples[i].Object = obj
				}
				if source, ok := tripleMap["source"].(string); ok {
					msg.triples[i].Source = source
				}
				// Handle timestamp
				if ts, ok := tripleMap["timestamp"].(string); ok {
					if t, err := time.Parse(time.RFC3339, ts); err == nil {
						msg.triples[i].Timestamp = t
					}
				}
				// Handle confidence
				if conf, ok := tripleMap["confidence"].(float64); ok {
					msg.triples[i].Confidence = conf
				}
			}
		}
	}

	// Handle storage_ref
	if v, ok := fields["storage_ref"].(map[string]any); ok {
		ref := &message.StorageReference{}
		if instance, ok := v["storage_instance"].(string); ok {
			ref.StorageInstance = instance
		}
		if key, ok := v["key"].(string); ok {
			ref.Key = key
		}
		if contentType, ok := v["content_type"].(string); ok {
			ref.ContentType = contentType
		}
		if size, ok := v["size"].(float64); ok {
			ref.Size = int64(size)
		} else if size, ok := v["size"].(int64); ok {
			ref.Size = size
		}
		msg.storageRef = ref
	}

	// Handle stored_at
	if v, ok := fields["stored_at"].(string); ok {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			msg.storedAt = t
		}
	}

	if err := msg.Validate(); err != nil {
		return nil, errs.Wrap(err, "StoredMessage", "buildStoredMessage", "validation failed")
	}

	return msg, nil
}

// RegisterPayloads registers the StoredMessage payload type with the
// supplied registry. Called from payloadbuiltins.Register at process
// bootstrap.
func RegisterPayloads(reg *payloadregistry.Registry) error {
	return reg.Register(&payloadregistry.Registration{
		Factory: func() any {
			return &StoredMessage{}
		},
		Domain:      "storage",
		Category:    "stored",
		Version:     "v1",
		Description: "Message with semantic data and storage reference",
		Builder:     buildStoredMessage,
	})
}

// StoredMessage implements message.Storable interface for messages that have been
// persisted to ObjectStore. It combines the semantic data (Graphable) with
// a storage reference pointing to the full message.
//
// This enables the "store once, reference everywhere" pattern where:
//   - ObjectStore stores the full raw message
//   - Graph processor receives semantic data plus storage reference
//   - Authorized components resolve the reference through StoreRegistry
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
func NewStoredMessage(graphable graph.Graphable, storageRef *message.StorageReference, messageType string) *StoredMessage {
	return &StoredMessage{
		entityID:    graphable.EntityID(),
		triples:     graphable.Triples(),
		storageRef:  storageRef,
		storedAt:    time.Now(),
		messageType: messageType,
	}
}

// EntityID implements graph.Graphable interface
func (s *StoredMessage) EntityID() string {
	return s.entityID
}

// Triples implements graph.Graphable interface
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
		EntityID    string                    `json:"entity_id"`
		Triples     []message.Triple          `json:"triples"`
		StorageRef  *message.StorageReference `json:"storage_ref"`
		StoredAt    time.Time                 `json:"stored_at"`
		MessageType string                    `json:"message_type"`
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
	if err := semtypes.ValidateEntityID(s.entityID); err != nil {
		return err
	}
	if s.storageRef == nil {
		return errs.WrapInvalid(errs.ErrInvalidData, "StoredMessage", "Validate", "storage_ref is required")
	}
	return nil
}

// Ensure StoredMessage implements both message.Storable and message.Payload
var _ message.Storable = (*StoredMessage)(nil)
var _ message.Payload = (*StoredMessage)(nil)
