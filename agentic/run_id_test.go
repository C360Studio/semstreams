package agentic_test

// ADR-053 Pass A — RunID plumbing tests.
// Covers:
//  1. TaskMessage RunID JSON round-trip (field present with omitempty).
//  2. LoopEntity RunID JSON round-trip.
//  3. All four loop events (Created/Completed/Failed/Cancelled) marshal and
//     unmarshal RunID + RunEntityID through the production wire (BaseMessage +
//     registered decoder) per feedback_production_decoder_round_trip_required
//     and feedback_polymorphic_config_needs_json_roundtrip_test.
//  4. LoopCancelledEvent ParentLoopID parity with LoopFailedEvent.
//  5. omitempty discipline — empty RunID must not appear in serialised JSON.

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/payloadbuiltins"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newRunIDTestDecoder builds a production decoder from the full builtin registry,
// satisfying the feedback_production_decoder_round_trip_required discipline.
func newRunIDTestDecoder(t *testing.T) *message.Decoder {
	t.Helper()
	return payloadbuiltins.NewTestDecoder(t)
}

// --- TaskMessage RunID ---

func TestTaskMessage_RunID_RoundTrip(t *testing.T) {
	t.Parallel()
	task := agentic.TaskMessage{
		TaskID: "task-001",
		Role:   "coordinator",
		Model:  "model-a",
		Prompt: "run the research arc",
		RunID:  "root-loop-uuid",
	}
	data, err := json.Marshal(&task)
	require.NoError(t, err)

	var got agentic.TaskMessage
	require.NoError(t, json.Unmarshal(data, &got))
	assert.Equal(t, "root-loop-uuid", got.RunID)
}

func TestTaskMessage_RunID_OmittedWhenEmpty(t *testing.T) {
	t.Parallel()
	task := agentic.TaskMessage{
		TaskID: "task-002",
		Role:   "researcher",
		Model:  "model-b",
		Prompt: "investigate X",
		// RunID deliberately empty
	}
	data, err := json.Marshal(&task)
	require.NoError(t, err)
	assert.NotContains(t, string(data), `"run_id"`,
		"empty RunID must be omitted from JSON (omitempty)")
}

func TestTaskMessage_RunID_PreservesOtherFields(t *testing.T) {
	t.Parallel()
	task := agentic.TaskMessage{
		TaskID:       "task-003",
		Role:         "architect",
		Model:        "model-c",
		Prompt:       "design the system",
		ParentLoopID: "parent-loop-uuid",
		RunID:        "run-anchor-uuid",
	}
	data, err := json.Marshal(&task)
	require.NoError(t, err)

	var got agentic.TaskMessage
	require.NoError(t, json.Unmarshal(data, &got))
	assert.Equal(t, "parent-loop-uuid", got.ParentLoopID)
	assert.Equal(t, "run-anchor-uuid", got.RunID)
}

// --- LoopEntity RunID ---

func TestLoopEntity_RunID_RoundTrip(t *testing.T) {
	t.Parallel()
	entity := agentic.LoopEntity{
		ID:            "loop-123",
		TaskID:        "task-001",
		State:         agentic.LoopStateExploring,
		Role:          "coordinator",
		Model:         "model-a",
		MaxIterations: 10,
		ParentLoopID:  "parent-loop-uuid",
		RunID:         "root-loop-uuid",
	}
	data, err := json.Marshal(&entity)
	require.NoError(t, err)

	var got agentic.LoopEntity
	require.NoError(t, json.Unmarshal(data, &got))
	assert.Equal(t, "root-loop-uuid", got.RunID)
	assert.Equal(t, "parent-loop-uuid", got.ParentLoopID)
}

func TestLoopEntity_RunID_OmittedWhenEmpty(t *testing.T) {
	t.Parallel()
	entity := agentic.LoopEntity{
		ID:            "loop-124",
		TaskID:        "task-002",
		State:         agentic.LoopStateExploring,
		Role:          "researcher",
		Model:         "model-b",
		MaxIterations: 5,
		// RunID deliberately empty
	}
	data, err := json.Marshal(&entity)
	require.NoError(t, err)
	assert.NotContains(t, string(data), `"run_id"`,
		"empty RunID must be omitted from JSON (omitempty)")
}

// --- LoopCreatedEvent RunID+RunEntityID production wire round-trip ---

func TestLoopCreatedEvent_RunID_ProductionWireRoundTrip(t *testing.T) {
	t.Parallel()
	ev := &agentic.LoopCreatedEvent{
		LoopID:        "loop-created-001",
		TaskID:        "task-001",
		Role:          "coordinator",
		Model:         "model-x",
		MaxIterations: 20,
		CreatedAt:     time.Now().UTC().Truncate(time.Second),
		RunID:         "root-loop-uuid",
		RunEntityID:   "acme.ops.chain.agent.execution.root-loop-uuid",
	}
	baseMsg := message.NewBaseMessage(ev.Schema(), ev, "test")
	data, err := json.Marshal(baseMsg)
	require.NoError(t, err)

	decoded, err := newRunIDTestDecoder(t).Decode(data)
	require.NoError(t, err)
	got, ok := decoded.Payload().(*agentic.LoopCreatedEvent)
	require.True(t, ok, "expected *agentic.LoopCreatedEvent payload, got %T", decoded.Payload())
	assert.Equal(t, "root-loop-uuid", got.RunID)
	assert.Equal(t, "acme.ops.chain.agent.execution.root-loop-uuid", got.RunEntityID)
	assert.Equal(t, "loop-created-001", got.LoopID)
}

func TestLoopCreatedEvent_RunID_OmittedWhenEmpty(t *testing.T) {
	t.Parallel()
	ev := &agentic.LoopCreatedEvent{
		LoopID:        "loop-created-002",
		TaskID:        "task-002",
		Role:          "researcher",
		Model:         "model-x",
		MaxIterations: 10,
		CreatedAt:     time.Now().UTC(),
		// RunID empty
	}
	data, err := json.Marshal(ev)
	require.NoError(t, err)
	assert.NotContains(t, string(data), `"run_id"`)
	assert.NotContains(t, string(data), `"run_entity_id"`)
}

// --- LoopCompletedEvent RunID+RunEntityID production wire round-trip ---

func TestLoopCompletedEvent_RunID_ProductionWireRoundTrip(t *testing.T) {
	t.Parallel()
	ev := &agentic.LoopCompletedEvent{
		LoopID:      "loop-completed-001",
		TaskID:      "task-001",
		Outcome:     agentic.OutcomeSuccess,
		Role:        "coordinator",
		Result:      "done",
		Model:       "model-x",
		CompletedAt: time.Now().UTC().Truncate(time.Second),
		RunID:       "root-loop-uuid",
		RunEntityID: "acme.ops.chain.agent.execution.root-loop-uuid",
	}
	baseMsg := message.NewBaseMessage(ev.Schema(), ev, "test")
	data, err := json.Marshal(baseMsg)
	require.NoError(t, err)

	decoded, err := newRunIDTestDecoder(t).Decode(data)
	require.NoError(t, err)
	got, ok := decoded.Payload().(*agentic.LoopCompletedEvent)
	require.True(t, ok, "expected *agentic.LoopCompletedEvent payload, got %T", decoded.Payload())
	assert.Equal(t, "root-loop-uuid", got.RunID)
	assert.Equal(t, "acme.ops.chain.agent.execution.root-loop-uuid", got.RunEntityID)
}

func TestLoopCompletedEvent_RunID_OmittedWhenEmpty(t *testing.T) {
	t.Parallel()
	ev := &agentic.LoopCompletedEvent{
		LoopID:      "loop-completed-002",
		TaskID:      "task-002",
		Outcome:     agentic.OutcomeSuccess,
		Role:        "researcher",
		Result:      "done",
		Model:       "model-x",
		CompletedAt: time.Now().UTC(),
	}
	data, err := json.Marshal(ev)
	require.NoError(t, err)
	assert.NotContains(t, string(data), `"run_id"`)
	assert.NotContains(t, string(data), `"run_entity_id"`)
}

// --- LoopFailedEvent RunID+RunEntityID production wire round-trip ---

func TestLoopFailedEvent_RunID_ProductionWireRoundTrip(t *testing.T) {
	t.Parallel()
	ev := &agentic.LoopFailedEvent{
		LoopID:      "loop-failed-001",
		TaskID:      "task-001",
		Outcome:     agentic.OutcomeFailed,
		Reason:      "context exhausted",
		Error:       "max iterations",
		Role:        "researcher",
		Model:       "model-x",
		FailedAt:    time.Now().UTC().Truncate(time.Second),
		RunID:       "root-loop-uuid",
		RunEntityID: "acme.ops.chain.agent.execution.root-loop-uuid",
	}
	baseMsg := message.NewBaseMessage(ev.Schema(), ev, "test")
	data, err := json.Marshal(baseMsg)
	require.NoError(t, err)

	decoded, err := newRunIDTestDecoder(t).Decode(data)
	require.NoError(t, err)
	got, ok := decoded.Payload().(*agentic.LoopFailedEvent)
	require.True(t, ok, "expected *agentic.LoopFailedEvent payload, got %T", decoded.Payload())
	assert.Equal(t, "root-loop-uuid", got.RunID)
	assert.Equal(t, "acme.ops.chain.agent.execution.root-loop-uuid", got.RunEntityID)
}

func TestLoopFailedEvent_RunID_OmittedWhenEmpty(t *testing.T) {
	t.Parallel()
	ev := &agentic.LoopFailedEvent{
		LoopID:   "loop-failed-002",
		TaskID:   "task-002",
		Outcome:  agentic.OutcomeFailed,
		Reason:   "timeout",
		Error:    "deadline exceeded",
		Role:     "researcher",
		Model:    "model-x",
		FailedAt: time.Now().UTC(),
	}
	data, err := json.Marshal(ev)
	require.NoError(t, err)
	assert.NotContains(t, string(data), `"run_id"`)
	assert.NotContains(t, string(data), `"run_entity_id"`)
}

// --- LoopCancelledEvent RunID+RunEntityID+ParentLoopID production wire round-trip ---

func TestLoopCancelledEvent_RunID_ProductionWireRoundTrip(t *testing.T) {
	t.Parallel()
	ev := &agentic.LoopCancelledEvent{
		LoopID:       "loop-cancelled-001",
		TaskID:       "task-001",
		Outcome:      agentic.OutcomeCancelled,
		CancelledBy:  "user-xyz",
		ParentLoopID: "parent-loop-uuid",
		CancelledAt:  time.Now().UTC().Truncate(time.Second),
		RunID:        "root-loop-uuid",
		RunEntityID:  "acme.ops.chain.agent.execution.root-loop-uuid",
	}
	baseMsg := message.NewBaseMessage(ev.Schema(), ev, "test")
	data, err := json.Marshal(baseMsg)
	require.NoError(t, err)

	decoded, err := newRunIDTestDecoder(t).Decode(data)
	require.NoError(t, err)
	got, ok := decoded.Payload().(*agentic.LoopCancelledEvent)
	require.True(t, ok, "expected *agentic.LoopCancelledEvent payload, got %T", decoded.Payload())
	assert.Equal(t, "root-loop-uuid", got.RunID)
	assert.Equal(t, "acme.ops.chain.agent.execution.root-loop-uuid", got.RunEntityID)
	assert.Equal(t, "parent-loop-uuid", got.ParentLoopID,
		"LoopCancelledEvent.ParentLoopID must round-trip (parity with LoopFailedEvent)")
}

func TestLoopCancelledEvent_ParentLoopID_JSONTagMatchesFailedEvent(t *testing.T) {
	t.Parallel()
	// Verify JSON tag is "parent_loop" (matching LoopFailedEvent, not "parent_loop_id").
	// This pins the wire shape so a future refactor doesn't silently change it.
	ev := &agentic.LoopCancelledEvent{
		LoopID:       "loop-cancelled-tag",
		TaskID:       "task-tag",
		Outcome:      agentic.OutcomeCancelled,
		CancelledBy:  "user",
		ParentLoopID: "parent-loop-abc",
		CancelledAt:  time.Now().UTC(),
	}
	data, err := json.Marshal(ev)
	require.NoError(t, err)

	var m map[string]any
	require.NoError(t, json.Unmarshal(data, &m))
	_, hasParentLoop := m["parent_loop"]
	_, hasParentLoopID := m["parent_loop_id"]
	assert.True(t, hasParentLoop,
		`LoopCancelledEvent.ParentLoopID must serialise as "parent_loop" (matching LoopFailedEvent wire tag)`)
	assert.False(t, hasParentLoopID,
		`LoopCancelledEvent.ParentLoopID must NOT serialise as "parent_loop_id"`)
}

func TestLoopCancelledEvent_RunID_OmittedWhenEmpty(t *testing.T) {
	t.Parallel()
	ev := &agentic.LoopCancelledEvent{
		LoopID:      "loop-cancelled-002",
		TaskID:      "task-002",
		Outcome:     agentic.OutcomeCancelled,
		CancelledBy: "user",
		CancelledAt: time.Now().UTC(),
	}
	data, err := json.Marshal(ev)
	require.NoError(t, err)
	assert.NotContains(t, string(data), `"run_id"`)
	assert.NotContains(t, string(data), `"run_entity_id"`)
	assert.NotContains(t, string(data), `"parent_loop"`)
}
