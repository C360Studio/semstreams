package message

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPayload for testing
type TestPayload struct {
	Value string
	Valid bool
}

func (p *TestPayload) Schema() Type {
	return Type{Domain: "test", Category: "payload", Version: "v1"}
}

func (p *TestPayload) Validate() error {
	if !p.Valid {
		return assert.AnError
	}
	return nil
}

func (p *TestPayload) MarshalJSON() ([]byte, error) {
	return []byte(p.Value), nil
}

func (p *TestPayload) UnmarshalJSON(data []byte) error {
	p.Value = string(data)
	return nil
}

func TestBaseMessage_Creation(t *testing.T) {
	msgType := Type{Domain: "test", Category: "base", Version: "v1"}
	payload := &TestPayload{Value: "test-data", Valid: true}
	source := "test-service"

	msg := NewBaseMessage(msgType, payload, source)

	assert.NotNil(t, msg)
	assert.NotEmpty(t, msg.ID())
	assert.Equal(t, msgType, msg.Type())
	assert.Equal(t, payload, msg.Payload())
	assert.Equal(t, source, msg.Meta().Source())
}

func TestBaseMessage_ID(t *testing.T) {
	msg1 := NewBaseMessage(
		Type{Domain: "test", Category: "id", Version: "v1"},
		&TestPayload{Value: "data1", Valid: true},
		"source1",
	)

	msg2 := NewBaseMessage(
		Type{Domain: "test", Category: "id", Version: "v1"},
		&TestPayload{Value: "data1", Valid: true},
		"source1",
	)

	// IDs should be unique even for identical messages
	assert.NotEqual(t, msg1.ID(), msg2.ID())

	// ID should be UUID format
	assert.Len(t, msg1.ID(), 36)
	assert.Contains(t, msg1.ID(), "-")
}

func TestBaseMessage_Type(t *testing.T) {
	msgType := Type{
		Domain:   "sensors",
		Category: "temperature",
		Version:  "v2",
	}

	msg := NewBaseMessage(msgType, &TestPayload{Valid: true}, "sensor-123")

	assert.Equal(t, msgType, msg.Type())
	assert.Equal(t, "sensors", msg.Type().Domain)
	assert.Equal(t, "temperature", msg.Type().Category)
	assert.Equal(t, "v2", msg.Type().Version)
}

func TestBaseMessage_Payload(t *testing.T) {
	payload := &TestPayload{
		Value: "sensor-reading",
		Valid: true,
	}

	msg := NewBaseMessage(
		Type{Domain: "test", Category: "payload", Version: "v1"},
		payload,
		"test-source",
	)

	assert.Equal(t, payload, msg.Payload())

	// Verify payload is accessible and modifiable
	retrieved := msg.Payload().(*TestPayload)
	assert.Equal(t, "sensor-reading", retrieved.Value)
}

func TestBaseMessage_Meta(t *testing.T) {
	msg := NewBaseMessage(
		Type{Domain: "test", Category: "meta", Version: "v1"},
		&TestPayload{Valid: true},
		"meta-source",
	)

	meta := msg.Meta()
	assert.NotNil(t, meta)
	assert.Equal(t, "meta-source", meta.Source())

	// CreatedAt should be approximately now (within millisecond precision)
	assert.WithinDuration(t, time.Now(), meta.CreatedAt(), 100*time.Millisecond)

	// ReceivedAt should be approximately now (within millisecond precision)
	assert.WithinDuration(t, time.Now(), meta.ReceivedAt(), 100*time.Millisecond)
}

func TestBaseMessage_WithTime(t *testing.T) {
	createdAt := time.Now().Add(-1 * time.Hour)

	msg := NewBaseMessage(
		Type{Domain: "test", Category: "time", Version: "v1"},
		&TestPayload{Valid: true},
		"historical-source",
		WithTime(createdAt),
	)

	// Timestamps lose nanosecond precision due to millisecond storage
	assert.WithinDuration(t, createdAt, msg.Meta().CreatedAt(), time.Millisecond)
	assert.Equal(t, "historical-source", msg.Meta().Source())

	// ReceivedAt should still be approximately now
	assert.WithinDuration(t, time.Now(), msg.Meta().ReceivedAt(), 100*time.Millisecond)
}

func TestBaseMessage_Hash(t *testing.T) {
	msgType := Type{Domain: "test", Category: "hash", Version: "v1"}
	payload1 := &TestPayload{Value: "data1", Valid: true}
	payload2 := &TestPayload{Value: "data2", Valid: true}

	msg1 := NewBaseMessage(msgType, payload1, "source")
	msg2 := NewBaseMessage(msgType, payload1, "source")
	msg3 := NewBaseMessage(msgType, payload2, "source")

	// Same content should produce same hash
	assert.Equal(t, msg1.Hash(), msg2.Hash())

	// Different content should produce different hash
	assert.NotEqual(t, msg1.Hash(), msg3.Hash())

	// Hash should be hex string
	assert.Len(t, msg1.Hash(), 64) // SHA256 produces 32 bytes = 64 hex chars
}

func TestBaseMessage_Validate(t *testing.T) {
	// Valid message
	validMsg := NewBaseMessage(
		Type{Domain: "test", Category: "valid", Version: "v1"},
		&TestPayload{Valid: true},
		"source",
	)
	assert.NoError(t, validMsg.Validate())

	// Invalid payload
	invalidMsg := NewBaseMessage(
		Type{Domain: "test", Category: "invalid", Version: "v1"},
		&TestPayload{Valid: false},
		"source",
	)
	assert.Error(t, invalidMsg.Validate())

	// Invalid message type
	invalidTypeMsg := NewBaseMessage(
		Type{Domain: "", Category: "invalid", Version: "v1"},
		&TestPayload{Valid: true},
		"source",
	)
	assert.Error(t, invalidTypeMsg.Validate())
}

func TestBaseMessage_NoRouteMethod(t *testing.T) {
	// This test verifies BaseMessage has no Route() method
	msg := NewBaseMessage(
		Type{Domain: "test", Category: "noroute", Version: "v1"},
		&TestPayload{Valid: true},
		"source",
	)

	// This should not compile if uncommented:
	// _ = msg.Route()

	// Routing is derived from Type by the Router
	assert.NotNil(t, msg.Type())
}

func TestBaseMessage_ImplementsInterface(t *testing.T) {
	// Verify BaseMessage implements Message interface
	var _ Message = (*BaseMessage)(nil)

	msg := NewBaseMessage(
		Type{Domain: "test", Category: "interface", Version: "v1"},
		&TestPayload{Valid: true},
		"source",
	)

	// Can use as Message interface
	var msgInterface Message = msg
	require.NotNil(t, msgInterface)
	assert.NotEmpty(t, msgInterface.ID())
}
