package agenticloop

import (
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/types"
)

// TestDispatchToolCall_StampsAgentRole is the production-wire proof for
// ADR-080's "attribution is derived, not supplied": dispatch stamps the
// emitting loop's role onto the outgoing ToolCall.Metadata under
// MetadataKeyAgentRole so a tool executor (emit_lesson) can DERIVE
// agent.lesson.observed-role without the model supplying an identity argument.
// The stamp is AUTHORITATIVE — it OVERWRITES any pre-injected (spoof) value —
// exactly like the run anchor.
func TestDispatchToolCall_StampsAgentRole(t *testing.T) {
	handler := NewMessageHandler(DefaultConfig())
	handler.SetPlatform(types.PlatformMeta{Org: "acme", Platform: "ops"})

	loopID, err := handler.loopManager.CreateLoop("task-role", "ops", "m", 20)
	if err != nil {
		t.Fatalf("CreateLoop: %v", err)
	}

	// A model that tries to SPOOF a role via injected tool metadata must be
	// overwritten by the framework fact.
	call := agentic.ToolCall{
		ID:       "call-role",
		Name:     "test_tool",
		Metadata: map[string]any{agentic.MetadataKeyAgentRole: "smuggled-superuser"},
	}

	result := &HandlerResult{}
	if _, storeErr := handler.tryDispatchOrSynthesize(result, loopID, call); storeErr != nil {
		t.Fatalf("storeErr = %v, want nil", storeErr)
	}

	payload := findPublishedToolCallPayload(t, result, "test_tool")
	metadata, ok := payload["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("payload.metadata not a map: %T", payload["metadata"])
	}
	if got := metadata[agentic.MetadataKeyAgentRole]; got != "ops" {
		t.Errorf("payload.metadata[%q] = %v, want %q (dispatch must stamp the loop's real role, overwriting any spoof)",
			agentic.MetadataKeyAgentRole, got, "ops")
	}
}

// TestDispatchToolCall_RolelessLoopNoRoleKey pins the back-compat contract: a
// loop with no role leaves the role key ABSENT — and dispatch DELETES any
// pre-injected value, so a roleless loop can never carry a caller-spoofed role
// into a tool executor.
func TestDispatchToolCall_RolelessLoopNoRoleKey(t *testing.T) {
	handler := NewMessageHandler(DefaultConfig())
	handler.SetPlatform(types.PlatformMeta{Org: "acme", Platform: "ops"})

	loopID, err := handler.loopManager.CreateLoop("task-roleless", "", "m", 20)
	if err != nil {
		t.Fatalf("CreateLoop: %v", err)
	}

	call := agentic.ToolCall{
		ID:       "call-roleless",
		Name:     "test_tool",
		Metadata: map[string]any{agentic.MetadataKeyAgentRole: "smuggled-role"},
	}

	result := &HandlerResult{}
	if _, storeErr := handler.tryDispatchOrSynthesize(result, loopID, call); storeErr != nil {
		t.Fatalf("storeErr = %v, want nil", storeErr)
	}

	payload := findPublishedToolCallPayload(t, result, "test_tool")
	metadata, _ := payload["metadata"].(map[string]any)
	if got, present := metadata[agentic.MetadataKeyAgentRole]; present {
		t.Errorf("payload.metadata[%q] = %v, want absent (roleless loop must not carry any role, spoofed or otherwise)",
			agentic.MetadataKeyAgentRole, got)
	}
}
