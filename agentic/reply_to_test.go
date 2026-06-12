package agentic_test

// gh#256 — resumable-reply wire plumbing tests.
// Covers the two fields a reply must carry to re-enter and resume a paused run
// (ADR-053 §4b-2): the run anchor (RunID, already exercised by run_id_test.go)
// and the reply marker (InReplyTo, new here). Asserts:
//  1. TaskMessage.InReplyTo JSON round-trip + omitempty discipline.
//  2. TaskMessage.InReplyTo survives the PRODUCTION wire (BaseMessage +
//     registered decoder) — the path agentic-loop actually decodes a task off,
//     per feedback_production_decoder_round_trip_required.
//  3. UserMessage.RunID + InReplyTo JSON round-trip + omitempty discipline —
//     the dispatch-inbound shape that threads into the TaskMessage.

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

// --- TaskMessage.InReplyTo ---

func TestTaskMessage_InReplyTo_RoundTrip(t *testing.T) {
	t.Parallel()
	task := agentic.TaskMessage{
		TaskID:    "task-001",
		Role:      "coordinator",
		Model:     "model-a",
		Prompt:    "here is my clarification answer",
		RunID:     "paused-run-uuid",
		InReplyTo: "asking-loop-uuid",
	}
	data, err := json.Marshal(&task)
	require.NoError(t, err)

	var got agentic.TaskMessage
	require.NoError(t, json.Unmarshal(data, &got))
	assert.Equal(t, "asking-loop-uuid", got.InReplyTo)
	assert.Equal(t, "paused-run-uuid", got.RunID)
}

func TestTaskMessage_InReplyTo_OmittedWhenEmpty(t *testing.T) {
	t.Parallel()
	task := agentic.TaskMessage{
		TaskID: "task-002",
		Role:   "researcher",
		Model:  "model-b",
		Prompt: "investigate X",
		// InReplyTo deliberately empty
	}
	data, err := json.Marshal(&task)
	require.NoError(t, err)
	assert.NotContains(t, string(data), `"in_reply_to"`)
}

// TestTaskMessage_InReplyTo_ProductionWire round-trips a TaskMessage carrying
// InReplyTo through the production decode path (BaseMessage envelope + the full
// builtin registry decoder) — the exact path agentic-loop's handler uses to
// decode an inbound task. A struct-level round-trip alone would not catch an
// envelope/registration regression.
func TestTaskMessage_InReplyTo_ProductionWire(t *testing.T) {
	t.Parallel()
	task := &agentic.TaskMessage{
		TaskID:    "task-wire-001",
		Role:      "coordinator",
		Model:     "model-a",
		Prompt:    "resume the paused run",
		RunID:     "paused-run-uuid",
		InReplyTo: "asking-loop-uuid",
	}
	envelope := message.NewBaseMessage(task.Schema(), task, "agentic-dispatch-test")
	data, err := json.Marshal(envelope)
	require.NoError(t, err)

	decoder := payloadbuiltins.NewTestDecoder(t)
	decoded, err := decoder.Decode(data)
	require.NoError(t, err)

	got, ok := decoded.Payload().(*agentic.TaskMessage)
	require.True(t, ok, "decoded payload is not *agentic.TaskMessage")
	assert.Equal(t, "asking-loop-uuid", got.InReplyTo)
	assert.Equal(t, "paused-run-uuid", got.RunID)
}

// --- UserMessage.RunID + InReplyTo (dispatch-inbound shape) ---

func TestUserMessage_RunID_InReplyTo_RoundTrip(t *testing.T) {
	t.Parallel()
	msg := agentic.UserMessage{
		MessageID: "msg-001",
		UserID:    "operator-1",
		Content:   "the answer is blue",
		ReplyTo:   "asking-loop-uuid", // routes to the loop to continue
		RunID:     "paused-run-uuid",  // re-attaches the resumed loop to its run
		InReplyTo: "asking-loop-uuid", // marks this message as a reply
		Timestamp: time.Now().UTC(),
	}
	data, err := json.Marshal(&msg)
	require.NoError(t, err)

	var got agentic.UserMessage
	require.NoError(t, json.Unmarshal(data, &got))
	assert.Equal(t, "paused-run-uuid", got.RunID)
	assert.Equal(t, "asking-loop-uuid", got.InReplyTo)
	assert.Equal(t, "asking-loop-uuid", got.ReplyTo)
}

func TestUserMessage_RunID_InReplyTo_OmittedWhenEmpty(t *testing.T) {
	t.Parallel()
	msg := agentic.UserMessage{
		MessageID: "msg-002",
		UserID:    "operator-1",
		Content:   "start a fresh task",
		Timestamp: time.Now().UTC(),
		// RunID + InReplyTo deliberately empty
	}
	data, err := json.Marshal(&msg)
	require.NoError(t, err)
	assert.NotContains(t, string(data), `"run_id"`)
	assert.NotContains(t, string(data), `"in_reply_to"`)
}
