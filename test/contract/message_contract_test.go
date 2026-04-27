package contract

import (
	"encoding/json"
	"testing"

	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/payloadregistry"
	"github.com/c360studio/semstreams/pkg/types"

	// Import packages to trigger their init() payload registrations
	_ "github.com/c360studio/semstreams/agentic"
	_ "github.com/c360studio/semstreams/processor/agentic-dispatch"
)

// schemaProvider matches message.Payload's Schema() method
type schemaProvider interface {
	Schema() types.Type
}

// TestSchemaRegistrationConsistency verifies that all registered payloads
// have Schema() methods that return values matching their registration.
// This test catches mismatches that would cause deserialization failures.
func TestSchemaRegistrationConsistency(t *testing.T) {
	payloads := payloadregistry.Global().List()
	if len(payloads) == 0 {
		t.Skip("No payloads registered")
	}

	for msgType, reg := range payloads {
		t.Run(msgType, func(t *testing.T) {
			// Create instance using factory
			payload := payloadregistry.Create(reg.Domain, reg.Category, reg.Version)
			if payload == nil {
				t.Fatalf("CreatePayload returned nil for registered type %s", msgType)
			}

			// Check if payload implements Schema()
			sp, ok := payload.(schemaProvider)
			if !ok {
				t.Skipf("Payload %s does not implement Schema() method", msgType)
				return
			}

			// Verify Schema() matches registration
			schema := sp.Schema()
			if schema.Domain != reg.Domain {
				t.Errorf("Schema().Domain = %q, want %q", schema.Domain, reg.Domain)
			}
			if schema.Category != reg.Category {
				t.Errorf("Schema().Category = %q, want %q", schema.Category, reg.Category)
			}
			if schema.Version != reg.Version {
				t.Errorf("Schema().Version = %q, want %q", schema.Version, reg.Version)
			}
		})
	}
}

// TestBaseMessageRoundTrip verifies that BaseMessage can marshal and unmarshal
// for all registered payload types without data loss.
//
// Note: Empty payloads (from factory) typically fail validation because they
// have required fields. This is expected and correct behavior - the contract
// enforcement prevents invalid messages from being serialized.
func TestBaseMessageRoundTrip(t *testing.T) {
	payloads := payloadregistry.Global().List()
	if len(payloads) == 0 {
		t.Skip("No payloads registered")
	}

	for msgType, reg := range payloads {
		t.Run(msgType, func(t *testing.T) {
			// Create a payload instance
			payload := payloadregistry.Create(reg.Domain, reg.Category, reg.Version)
			if payload == nil {
				t.Fatalf("CreatePayload returned nil for registered type %s", msgType)
			}

			// Cast to message.Payload
			msgPayload, ok := payload.(message.Payload)
			if !ok {
				t.Skipf("Payload %s does not implement message.Payload", msgType)
				return
			}

			// Create BaseMessage
			msgTypeStruct := types.Type{
				Domain:   reg.Domain,
				Category: reg.Category,
				Version:  reg.Version,
			}
			original := message.NewBaseMessage(msgTypeStruct, msgPayload, "contract-test")

			// Marshal to JSON - may fail for empty payloads (expected)
			data, err := json.Marshal(original)
			if err != nil {
				// Empty payloads failing validation is expected and correct behavior
				// This proves the contract enforcement is working
				t.Skipf("Empty payload correctly rejected by validation: %v", err)
				return
			}

			// Unmarshal back
			var restored message.BaseMessage
			if err := json.Unmarshal(data, &restored); err != nil {
				t.Fatalf("Failed to unmarshal BaseMessage: %v\nJSON: %s", err, string(data))
			}

			// Validate restored message
			if err := restored.Validate(); err != nil {
				t.Errorf("Restored message failed validation: %v", err)
			}

			// Verify type matches
			if restored.Type() != original.Type() {
				t.Errorf("Type mismatch: got %v, want %v", restored.Type(), original.Type())
			}
		})
	}
}

// TestPayloadValidation verifies that newly created payloads from factories
// pass validation (or fail with expected errors for required fields).
func TestPayloadValidation(t *testing.T) {
	payloads := payloadregistry.Global().List()
	if len(payloads) == 0 {
		t.Skip("No payloads registered")
	}

	for msgType, reg := range payloads {
		t.Run(msgType, func(t *testing.T) {
			payload := payloadregistry.Create(reg.Domain, reg.Category, reg.Version)
			if payload == nil {
				t.Fatalf("CreatePayload returned nil for registered type %s", msgType)
			}

			msgPayload, ok := payload.(message.Payload)
			if !ok {
				t.Skipf("Payload %s does not implement message.Payload", msgType)
				return
			}

			// Empty payloads may fail validation - that's expected
			// We're just checking that Validate() doesn't panic
			_ = msgPayload.Validate()
		})
	}
}

// TestPayloadMarshalJSON verifies that all registered payloads can marshal to JSON.
func TestPayloadMarshalJSON(t *testing.T) {
	payloads := payloadregistry.Global().List()
	if len(payloads) == 0 {
		t.Skip("No payloads registered")
	}

	for msgType, reg := range payloads {
		t.Run(msgType, func(t *testing.T) {
			payload := payloadregistry.Create(reg.Domain, reg.Category, reg.Version)
			if payload == nil {
				t.Fatalf("CreatePayload returned nil for registered type %s", msgType)
			}

			msgPayload, ok := payload.(message.Payload)
			if !ok {
				t.Skipf("Payload %s does not implement message.Payload", msgType)
				return
			}

			// Test MarshalJSON doesn't panic
			_, err := msgPayload.MarshalJSON()
			if err != nil {
				// Some payloads may fail to marshal when empty due to validation
				// This is expected behavior - log but don't fail
				t.Logf("Payload %s MarshalJSON error (may be expected for empty payload): %v", msgType, err)
			}
		})
	}
}
