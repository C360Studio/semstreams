package message

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/c360/semstreams/component"
	"github.com/c360/semstreams/config"
	"github.com/c360/semstreams/errors"
	"github.com/c360/semstreams/pkg/timestamp"
	"github.com/google/uuid"
)

// BaseMessage provides the standard implementation of the Message interface.
// It combines a typed payload with metadata to create a complete message
// ready for transmission through the semantic event mesh.
//
// BaseMessage is immutable after creation - all fields are set during
// construction and cannot be modified. This ensures message integrity
// throughout the processing pipeline.
//
// Construction using Functional Options:
//
// NewBaseMessage uses the functional options pattern for clean, composable configuration:
//
//	// Simple message (most common)
//	msg := NewBaseMessage(msgType, payload, "my-service")
//
//	// With specific timestamp (testing/historical data)
//	msg := NewBaseMessage(msgType, payload, "my-service", WithTime(pastTime))
//
//	// With federation support (multi-platform)
//	msg := NewBaseMessage(msgType, payload, "my-service", WithFederation(platform))
//
//	// Composable options
//	msg := NewBaseMessage(msgType, payload, "my-service",
//	    WithFederation(platform),
//	    WithTime(pastTime))
type BaseMessage struct {
	id      string
	msgType Type
	payload Payload
	meta    Meta
}

// Option is a functional option for configuring BaseMessage construction.
type Option func(*BaseMessage)

// WithTime sets a specific creation timestamp instead of using time.Now().
// Useful for historical data import or testing.
func WithTime(createdAt time.Time) Option {
	return func(m *BaseMessage) {
		// Replace the default meta with one using the specified time
		if defaultMeta, ok := m.meta.(*DefaultMeta); ok {
			m.meta = NewDefaultMeta(createdAt, defaultMeta.Source())
		}
	}
}

// WithMeta replaces the default metadata with a custom Meta implementation.
func WithMeta(meta Meta) Option {
	return func(m *BaseMessage) {
		m.meta = meta
	}
}

// WithFederation enables federation support by using FederationMeta.
// This adds global UIDs for cross-platform message correlation.
func WithFederation(platform config.PlatformConfig) Option {
	return func(m *BaseMessage) {
		// Replace default meta with federation meta
		if defaultMeta, ok := m.meta.(*DefaultMeta); ok {
			m.meta = NewFederationMeta(defaultMeta.Source(), platform)
		}
	}
}

// WithFederationAndTime combines federation support with a specific timestamp.
func WithFederationAndTime(platform config.PlatformConfig, createdAt time.Time) Option {
	return func(m *BaseMessage) {
		if defaultMeta, ok := m.meta.(*DefaultMeta); ok {
			m.meta = NewFederationMetaWithTime(defaultMeta.Source(), platform, createdAt)
		}
	}
}

// NewBaseMessage creates a new BaseMessage with optional configuration.
//
// Parameters:
//   - msgType: Structured type information (domain, category, version)
//   - payload: The message payload implementing the Payload interface
//   - source: Identifier of the service or component creating this message
//   - opts: Optional configuration functions
//
// Examples:
//
//	// Simple message with current timestamp
//	msg := NewBaseMessage(msgType, payload, "my-service")
//
//	// Message with specific timestamp (for historical data)
//	msg := NewBaseMessage(msgType, payload, "my-service", WithTime(pastTime))
//
//	// Federated message for multi-platform deployment
//	msg := NewBaseMessage(msgType, payload, "my-service", WithFederation(platform))
//
//	// Federated message with specific timestamp
//	msg := NewBaseMessage(msgType, payload, "my-service",
//	    WithFederationAndTime(platform, pastTime))
func NewBaseMessage(msgType Type, payload Payload, source string, opts ...Option) *BaseMessage {
	// Create message with defaults
	m := &BaseMessage{
		id:      uuid.New().String(),
		msgType: msgType,
		payload: payload,
		meta:    NewDefaultMeta(time.Now(), source),
	}

	// Apply functional options
	for _, opt := range opts {
		opt(m)
	}

	return m
}

// ID returns the unique message identifier.
func (m *BaseMessage) ID() string {
	return m.id
}

// Type returns the structured message type.
func (m *BaseMessage) Type() Type {
	return m.msgType
}

// Payload returns the message payload.
func (m *BaseMessage) Payload() Payload {
	return m.payload
}

// Meta returns the message metadata.
func (m *BaseMessage) Meta() Meta {
	return m.meta
}

// Hash returns a SHA256 hash of the message content.
// The hash includes the message type and payload data.
func (m *BaseMessage) Hash() string {
	h := sha256.New()

	// Include message type in hash
	if _, err := h.Write([]byte(m.msgType.String())); err != nil {
		// Hash.Write() implementation in crypto/sha256 never returns an error,
		// but we handle it for interface compliance and future-proofing
		return ""
	}

	// Include payload data
	if data, err := m.payload.MarshalJSON(); err == nil {
		if _, err := h.Write(data); err != nil {
			// Hash.Write() implementation in crypto/sha256 never returns an error,
			// but we handle it for interface compliance and future-proofing
			return ""
		}
	}

	return hex.EncodeToString(h.Sum(nil))
}

// Validate performs comprehensive message validation.
func (m *BaseMessage) Validate() error {
	// Validate message type
	if !m.msgType.IsValid() {
		return errors.WrapInvalid(errors.ErrInvalidData, "BaseMessage", "Validate",
			fmt.Sprintf("invalid message type: %s", m.msgType.String()))
	}

	// Validate payload
	if m.payload == nil {
		return errors.WrapInvalid(errors.ErrInvalidData, "BaseMessage", "Validate", "payload cannot be nil")
	}

	if err := m.payload.Validate(); err != nil {
		return errors.WrapInvalid(err, "BaseMessage", "Validate", "invalid payload")
	}

	// Validate metadata
	if m.meta == nil {
		return errors.WrapInvalid(errors.ErrInvalidData, "BaseMessage", "Validate", "meta cannot be nil")
	}

	return nil
}

// wireFormat represents the JSON wire format for BaseMessage.
// This struct has public fields for JSON marshalling/unmarshalling.
type wireFormat struct {
	ID      string          `json:"id"`
	Type    Type            `json:"type"`
	Payload json.RawMessage `json:"payload"`
	Meta    map[string]any  `json:"meta"`
}

// MarshalJSON implements json.Marshaler for BaseMessage.
// This allows BaseMessage to be serialized to JSON even though
// its fields are private.
func (m *BaseMessage) MarshalJSON() ([]byte, error) {
	// Marshal the payload using its MarshalJSON method
	payloadData, err := m.payload.MarshalJSON()
	if err != nil {
		return nil, errors.WrapInvalid(err, "BaseMessage", "MarshalJSON", "failed to marshal payload")
	}

	// Create metadata map with int64 timestamps for consistency
	metaMap := map[string]interface{}{
		"created_at":  timestamp.ToUnixMs(m.meta.CreatedAt()),
		"received_at": timestamp.ToUnixMs(m.meta.ReceivedAt()),
		"source":      m.meta.Source(),
	}

	// Create the wire format
	wire := wireFormat{
		ID:      m.id,
		Type:    m.msgType,
		Payload: json.RawMessage(payloadData),
		Meta:    metaMap,
	}

	return json.Marshal(wire)
}

// UnmarshalJSON implements json.Unmarshaler for BaseMessage.
// Requires payload types to be registered in the global PayloadRegistry.
// For generic JSON processing, use the well-known type "core.json.v1" (GenericJSONPayload).
func (m *BaseMessage) UnmarshalJSON(data []byte) error {
	var wire wireFormat
	if err := json.Unmarshal(data, &wire); err != nil {
		return errors.WrapInvalid(err, "BaseMessage", "UnmarshalJSON", "failed to unmarshal wire format")
	}

	// Set basic fields
	m.id = wire.ID
	m.msgType = wire.Type

	// Unmarshal metadata - create a DefaultMeta from the wire format
	// Use timestamp.Parse to handle both int64 and string formats
	var createdAt, receivedAt time.Time
	var source string

	createdAtMs := timestamp.Parse(wire.Meta["created_at"])
	if createdAtMs != 0 {
		createdAt = timestamp.ToTime(createdAtMs)
	}

	receivedAtMs := timestamp.Parse(wire.Meta["received_at"])
	if receivedAtMs != 0 {
		receivedAt = timestamp.ToTime(receivedAtMs)
	}

	if sourceStr, ok := wire.Meta["source"].(string); ok {
		source = sourceStr
	}

	m.meta = NewDefaultMetaWithReceivedAt(createdAt, receivedAt, source)

	// Try to create typed payload using registry
	payload := component.CreatePayload(m.msgType.Domain, m.msgType.Category, m.msgType.Version)
	if payload == nil {
		// Unknown type - payload must be registered or use core.json.v1
		return errors.WrapInvalid(
			fmt.Errorf("unregistered payload type: %s", m.msgType.String()),
			"BaseMessage", "UnmarshalJSON", "payload type lookup")
	}

	// Unmarshal JSON into the typed payload
	if msgPayload, ok := payload.(Payload); ok {
		if err := json.Unmarshal(wire.Payload, msgPayload); err != nil {
			return errors.WrapInvalid(err, "BaseMessage", "UnmarshalJSON", "failed to unmarshal payload")
		}
		m.payload = msgPayload
	} else {
		return errors.WrapInvalid(errors.ErrInvalidData, "BaseMessage", "UnmarshalJSON", "payload does not implement message.Payload interface")
	}

	return nil
}
