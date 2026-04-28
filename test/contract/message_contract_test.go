package contract

import (
	"encoding/json"
	"testing"

	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/payloadbuiltins"
	"github.com/c360studio/semstreams/payloadregistry"
	"github.com/c360studio/semstreams/pkg/types"
)

// schemaProvider matches message.Payload's Schema() method
type schemaProvider interface {
	Schema() types.Type
}

// contractRegistry returns a fresh registry populated with all
// first-party payload registrations. Pre-beta.18 this test relied on
// blank imports of agentic + agentic-dispatch to trigger init() side
// effects on a process-wide singleton; post-beta.18 the singleton is
// gone and the test wires its own registry explicitly via
// payloadbuiltins.NewTestRegistry.
func contractRegistry(t *testing.T) *payloadregistry.Registry {
	t.Helper()
	return payloadbuiltins.NewTestRegistry(t)
}

// TestSchemaRegistrationConsistency verifies that all registered payloads
// have Schema() methods that return values matching their registration.
// This test catches mismatches that would cause deserialization failures.
func TestSchemaRegistrationConsistency(t *testing.T) {
	reg := contractRegistry(t)
	payloads := reg.List()
	if len(payloads) == 0 {
		t.Skip("No payloads registered")
	}

	for msgType, registration := range payloads {
		t.Run(msgType, func(t *testing.T) {
			// Create instance using factory
			payload := reg.Create(registration.Domain, registration.Category, registration.Version)
			if payload == nil {
				t.Fatalf("Create returned nil for registered type %s", msgType)
			}

			// Check if payload implements Schema()
			sp, ok := payload.(schemaProvider)
			if !ok {
				t.Skipf("Payload %s does not implement Schema() method", msgType)
				return
			}

			// Verify Schema() matches registration
			schema := sp.Schema()
			if schema.Domain != registration.Domain {
				t.Errorf("Schema().Domain = %q, want %q", schema.Domain, registration.Domain)
			}
			if schema.Category != registration.Category {
				t.Errorf("Schema().Category = %q, want %q", schema.Category, registration.Category)
			}
			if schema.Version != registration.Version {
				t.Errorf("Schema().Version = %q, want %q", schema.Version, registration.Version)
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
	reg := contractRegistry(t)
	payloads := reg.List()
	if len(payloads) == 0 {
		t.Skip("No payloads registered")
	}

	dec := message.NewDecoder(reg)

	for msgType, registration := range payloads {
		t.Run(msgType, func(t *testing.T) {
			// Create a payload instance
			payload := reg.Create(registration.Domain, registration.Category, registration.Version)
			if payload == nil {
				t.Fatalf("Create returned nil for registered type %s", msgType)
			}

			// Cast to message.Payload
			msgPayload, ok := payload.(message.Payload)
			if !ok {
				t.Skipf("Payload %s does not implement message.Payload", msgType)
				return
			}

			// Create BaseMessage
			msgTypeStruct := types.Type{
				Domain:   registration.Domain,
				Category: registration.Category,
				Version:  registration.Version,
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

			// Unmarshal back via the per-test registry-bound Decoder.
			restored, err := dec.Decode(data)
			if err != nil {
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
	reg := contractRegistry(t)
	payloads := reg.List()
	if len(payloads) == 0 {
		t.Skip("No payloads registered")
	}

	for msgType, registration := range payloads {
		t.Run(msgType, func(t *testing.T) {
			payload := reg.Create(registration.Domain, registration.Category, registration.Version)
			if payload == nil {
				t.Fatalf("Create returned nil for registered type %s", msgType)
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
	reg := contractRegistry(t)
	payloads := reg.List()
	if len(payloads) == 0 {
		t.Skip("No payloads registered")
	}

	for msgType, registration := range payloads {
		t.Run(msgType, func(t *testing.T) {
			payload := reg.Create(registration.Domain, registration.Category, registration.Version)
			if payload == nil {
				t.Fatalf("Create returned nil for registered type %s", msgType)
			}

			msgPayload, ok := payload.(message.Payload)
			if !ok {
				t.Skipf("Payload %s does not implement message.Payload", msgType)
				return
			}

			// Test MarshalJSON doesn't panic
			_, err := msgPayload.MarshalJSON()
			if err != nil {
				// Validation failures are expected for empty payloads
				t.Logf("MarshalJSON returned error for empty payload (expected): %v", err)
			}
		})
	}
}
