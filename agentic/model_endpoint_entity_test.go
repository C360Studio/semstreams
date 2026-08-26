package agentic_test

import (
	"testing"

	"github.com/c360studio/semstreams/agentic"
)

// --- ModelEndpointMessageType (gh#390 typed-origin envelope) ---

func TestModelEndpointMessageType_Valid(t *testing.T) {
	mt := agentic.ModelEndpointMessageType()
	if mt.Domain == "" {
		t.Error("MessageType.Domain is empty")
	}
	if mt.Category == "" {
		t.Error("MessageType.Category is empty")
	}
	if mt.Version == "" {
		t.Error("MessageType.Version is empty")
	}
	if !mt.IsValid() {
		t.Errorf("MessageType %v is not valid (IsValid() returned false)", mt)
	}
}

func TestModelEndpointMessageType_KeyFormat(t *testing.T) {
	key := agentic.ModelEndpointMessageType().Key()
	want := "agentic.model_endpoint.v1"
	if key != want {
		t.Errorf("MessageType.Key() = %q, want %q", key, want)
	}
}
