package objectstore

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/c360studio/semstreams/message"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

// TestDefaultKeyGenerator_ByteSlice tests key generation with []byte input
// Note: []byte doesn't implement message.Message, so defaults are used
func TestDefaultKeyGenerator_ByteSlice(t *testing.T) {
	gen := &DefaultKeyGenerator{}

	// JSON bytes that might come from NATS
	jsonData := []byte(`{"type":"document","title":"Safety Manual"}`)

	key := gen.GenerateKey(jsonData)

	// Should use defaults since []byte doesn't implement message.Message
	assert.Contains(t, key, "message/")
	assert.Contains(t, key, "/unknown_")
}

// TestDefaultKeyGenerator_RawBytesSameSecondDistinct pins the sub-second
// entropy lane of #741: raw []byte inputs implement neither leg of
// message.Message, so every such write shares the "unknown" identifier —
// with a seconds-granularity timestamp suffix, two DISTINCT messages in the
// same wall-clock second generated the IDENTICAL key, and ObjectStore Put
// replace semantics silently discarded the first (no error, no log).
//
// The retry loop is a same-second window guard: the test only discriminates
// when both GenerateKey calls land inside one wall-clock second, so a crossed
// boundary retries rather than passing vacuously.
func TestDefaultKeyGenerator_RawBytesSameSecondDistinct(t *testing.T) {
	gen := &DefaultKeyGenerator{}

	var k1, k2 string
	sameSecond := false
	for i := 0; i < 50 && !sameSecond; i++ {
		before := time.Now().Unix()
		k1 = gen.GenerateKey([]byte(`{"seq":"a"}`))
		k2 = gen.GenerateKey([]byte(`{"seq":"b"}`))
		sameSecond = time.Now().Unix() == before
	}
	require.True(t, sameSecond,
		"could not land two GenerateKey calls inside one wall-clock second")

	assert.NotEqual(t, k1, k2,
		"two distinct raw-path messages in the same second must never share a key: "+
			"ObjectStore Put replaces and the first object is silently lost (#741)")
}

// TestDefaultKeyGenerator_RawMessage tests key generation with json.RawMessage input
func TestDefaultKeyGenerator_RawMessage(t *testing.T) {
	gen := &DefaultKeyGenerator{}

	// json.RawMessage - raw JSON bytes
	rawMsg := json.RawMessage(`{"entity_id":"entity-123","properties":{}}`)

	key := gen.GenerateKey(rawMsg)

	// Should use defaults since json.RawMessage doesn't implement message.Message
	assert.Contains(t, key, "message/")
	assert.Contains(t, key, "/unknown_")
}
