package agenticdispatch

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/stretchr/testify/assert"
)

// newBuildTaskTestComponent builds the minimal Component buildTaskMessage needs:
// a config (DefaultRole + nil DefaultTools so scopeTaskTools is a no-op) and a
// model registry (resolveModel). No NATS — buildTaskMessage is pure assembly.
func newBuildTaskTestComponent() *Component {
	return &Component{
		config:        DefaultConfig(),
		modelRegistry: newTestRegistry(),
		logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

// TestBuildTaskMessage_PropagatesResumableReplyAnchors is the gh#256 regression
// guard: both the bus path and the HTTP sync path now go through buildTaskMessage,
// so a reply's run anchor (RunID) and reply marker (InReplyTo) must reach the
// TaskMessage. Before the fix the reply branch dropped both, leaving the resumed
// loop run-orphaned and unrecognisable as a reply.
func TestBuildTaskMessage_PropagatesResumableReplyAnchors(t *testing.T) {
	t.Parallel()
	c := newBuildTaskTestComponent()

	msg := agentic.UserMessage{
		MessageID: "msg-1",
		UserID:    "operator-1",
		Content:   "the answer is blue",
		ReplyTo:   "asking-loop-uuid", // routes to the loop to continue
		RunID:     "paused-run-uuid",  // re-attaches the resumed loop to its run
		InReplyTo: "asking-loop-uuid", // marks this message as a reply
	}

	task := c.buildTaskMessage(context.Background(), msg, "asking-loop-uuid", "task-1")

	assert.Equal(t, "paused-run-uuid", task.RunID, "RunID must propagate UserMessage → TaskMessage")
	assert.Equal(t, "asking-loop-uuid", task.InReplyTo, "InReplyTo must propagate UserMessage → TaskMessage")
	// The rest of the contract still holds.
	assert.Equal(t, "asking-loop-uuid", task.LoopID)
	assert.Equal(t, "task-1", task.TaskID)
	assert.Equal(t, "the answer is blue", task.Prompt)
	assert.Equal(t, c.config.DefaultRole, task.Role)
}

// TestBuildTaskMessage_OrdinarySubmissionCarriesNoAnchors pins the omitempty
// contract: a fresh (non-reply) submission must not carry a run anchor or a
// reply marker, so an ordinary continuation is never mis-stamped as a reply.
func TestBuildTaskMessage_OrdinarySubmissionCarriesNoAnchors(t *testing.T) {
	t.Parallel()
	c := newBuildTaskTestComponent()

	msg := agentic.UserMessage{
		MessageID: "msg-2",
		UserID:    "operator-1",
		Content:   "start a fresh task",
		// RunID + InReplyTo deliberately empty
	}

	task := c.buildTaskMessage(context.Background(), msg, "loop-new", "task-2")

	assert.Empty(t, task.RunID, "fresh submission must not carry a run anchor")
	assert.Empty(t, task.InReplyTo, "fresh submission must not be marked as a reply")
}
