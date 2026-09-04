package agenticdispatch

import (
	"context"
	"errors"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/stretchr/testify/require"
)

type failingRetainedTaskEvidenceReader struct {
	err error
}

func (r failingRetainedTaskEvidenceReader) ReadRetainedTask(
	context.Context,
	string,
	string,
) ([]byte, bool, error) {
	return nil, false, r.err
}

// spec: agentic-dispatch / Dispatch task redelivery recovers the committed LoopID
func TestUnreadableRetainedTaskEvidenceDoesNotMintOrRefuse(t *testing.T) {
	c, sink, _ := newSeamTestComponent(t)
	c.taskEvidence = failingRetainedTaskEvidenceReader{err: errors.New("retained read unavailable")}
	msg := agentic.UserMessage{
		MessageID:   "source-message",
		ChannelType: "http",
		ChannelID:   "channel",
		UserID:      "user",
		Content:     "perform one task",
	}

	prepared, _, found, err := c.findRetainedDispatchTask(t.Context(), msg)

	require.ErrorContains(t, err, "retained read unavailable")
	require.True(t, errs.IsTransient(err), "unreadable evidence must retry instead of pretending this is new work")
	require.False(t, found)
	require.Empty(t, prepared.task.LoopID, "a random LoopID requires an exact retained-absence result")

	err = c.handleTaskSubmission(t.Context(), msg)
	require.True(t, errs.IsTransient(err), "the durable source owner must retry an unreadable evidence check")
	require.Empty(t, sink.all(), "a retryable evidence outage is not a permanent user refusal")
	require.Empty(t, c.loopTracker.GetAllLoops(), "unreadable evidence must not track newly minted work")
}

// spec: agentic-dispatch / Dispatch task redelivery recovers the committed LoopID
func TestInvalidUserMessageIdentityIsRejectedBeforeTaskIdentity(t *testing.T) {
	c, _, _ := newSeamTestComponent(t)
	msg := agentic.UserMessage{
		ChannelType: "http",
		ChannelID:   "channel",
		UserID:      "user",
		Content:     "perform one task",
	}

	prepared, vacant, found, err := c.findRetainedDispatchTask(t.Context(), msg)

	require.ErrorContains(t, err, "message_id required")
	require.False(t, found)
	require.Empty(t, vacant.taskID, "invalid source identity must not derive a TaskID")
	require.Empty(t, prepared.task.LoopID, "invalid source identity must not mint a LoopID")
}
