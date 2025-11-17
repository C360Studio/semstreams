package objectstore

import (
	"testing"

	"github.com/c360/semstreams/message"
	"github.com/stretchr/testify/assert"
)

// TestDefaultKeyGenerator_WithMessage tests key generation with proper message.Message interface
func TestDefaultKeyGenerator_WithMessage(t *testing.T) {
	gen := &DefaultKeyGenerator{}

	// Create a proper message.Message with known type and ID
	payload := message.NewGenericJSON(map[string]any{
		"device_id": "device-123",
		"data":      "test",
	})
	msg := message.NewBaseMessage(payload.Schema(), payload, "test-source")

	key := gen.GenerateKey(msg)

	// Key should have format: type/year/month/day/hour/identifier_timestamp
	// Type should be "core.json.v1" from GenericJSONPayload
	assert.Contains(t, key, "core.json.v1/")
	// Identifier should be the message ID (UUID)
	assert.Contains(t, key, msg.ID()+"_")
}

// TestDefaultKeyGenerator_WithFallbackStruct tests fallback behavior with plain struct
func TestDefaultKeyGenerator_WithFallbackStruct(t *testing.T) {
	gen := &DefaultKeyGenerator{}

	// Plain struct that doesn't implement message.Message
	type PlainStruct struct {
		ID   string `json:"id"`
		Data string `json:"data"`
	}

	payload := &PlainStruct{
		ID:   "device-123",
		Data: "test",
	}

	key := gen.GenerateKey(payload)

	// Should use defaults since struct doesn't implement message.Message
	assert.Contains(t, key, "message/")
	assert.Contains(t, key, "/unknown_")
}

// TestDefaultKeyGenerator_WithFallbackMap tests fallback behavior with plain map
func TestDefaultKeyGenerator_WithFallbackMap(t *testing.T) {
	gen := &DefaultKeyGenerator{}

	// Plain map - doesn't implement message.Message
	payload := map[string]any{
		"device_id": "sensor-456",
		"value":     23.5,
	}

	key := gen.GenerateKey(payload)

	// Should use defaults since map doesn't implement message.Message
	assert.Contains(t, key, "message/")
	assert.Contains(t, key, "/unknown_")
}

// TestDefaultKeyGenerator_KeyFormat tests the overall key format structure
func TestDefaultKeyGenerator_KeyFormat(t *testing.T) {
	gen := &DefaultKeyGenerator{}

	payload := message.NewGenericJSON(map[string]any{"value": 25.0})
	msg := message.NewBaseMessage(payload.Schema(), payload, "test-source")

	key := gen.GenerateKey(msg)

	// Verify key contains expected components
	assert.Contains(t, key, "core.json.v1/") // Type (GenericJSONPayload)
	assert.Contains(t, key, "/")             // Path separators
	assert.Contains(t, key, "_")             // Identifier_timestamp separator

	// Key should match format: type/YYYY/MM/DD/HH/identifier_timestamp
	// We can't test exact values due to timestamps, but we can verify structure
	assert.Regexp(t, `^[^/]+/\d{4}/\d{2}/\d{2}/\d{2}/[^_]+_\d+$`, key)
}

// TestDefaultKeyGenerator_MultipleCalls tests that each call generates unique keys
func TestDefaultKeyGenerator_MultipleCalls(t *testing.T) {
	gen := &DefaultKeyGenerator{}

	payload := message.NewGenericJSON(map[string]any{})

	// Generate multiple keys
	keys := make([]string, 5)
	for i := 0; i < 5; i++ {
		msg := message.NewBaseMessage(payload.Schema(), payload, "test")
		keys[i] = gen.GenerateKey(msg)
	}

	// All keys should be unique (different message IDs)
	seen := make(map[string]bool)
	for _, key := range keys {
		assert.False(t, seen[key], "Generated duplicate key: %s", key)
		seen[key] = true
	}
}
