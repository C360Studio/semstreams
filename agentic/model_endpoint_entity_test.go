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

func TestModelEndpointMessageType_Distinct(t *testing.T) {
	// Must not collide with any other agentic category constant — a collision
	// would let two entity kinds share an origin envelope and silently break
	// typed-origin ownership arbitration.
	mt := agentic.ModelEndpointMessageType()
	others := []string{
		agentic.CategoryLoopExecution,
		agentic.CategoryLoopCreated,
		agentic.CategoryLoopCompleted,
		agentic.CategoryLoopFailed,
		agentic.CategoryLoopCancelled,
		agentic.CategoryTask,
	}
	for _, cat := range others {
		if mt.Category == cat {
			t.Errorf("CategoryModelEndpoint collides with existing category %q", cat)
		}
	}
}

func TestModelEndpointMessageType_KeyFormat(t *testing.T) {
	key := agentic.ModelEndpointMessageType().Key()
	want := "agentic.model_endpoint.v1"
	if key != want {
		t.Errorf("MessageType.Key() = %q, want %q", key, want)
	}
}
