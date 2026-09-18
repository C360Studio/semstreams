package agenticdispatch

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/payloadbuiltins"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/prometheus/client_golang/prometheus/testutil"
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
	require.Zero(t, testutil.ToFloat64(c.metrics.tasksSubmitted), "unreadable evidence must not publish newly minted work")

	response, err := c.processTaskSubmissionSync(t.Context(), msg)
	require.NoError(t, err)
	require.Equal(t, agentic.ResponseTypeError, response.Type)
	require.Contains(t, response.Content, "retained read unavailable")
	require.Empty(t, sink.all(), "HTTP read refusal is synchronous")
	require.Zero(t, testutil.ToFloat64(c.metrics.tasksSubmitted))
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

// spec: agentic-dispatch / Every dispatch durable input settles through its owner
// spec: agentic-dispatch / Dispatch task redelivery recovers the committed LoopID
func TestRetainedTaskReusePreservesCallerResponseSemantics(t *testing.T) {
	for _, lane := range []string{"channel", "http"} {
		for _, failResponse := range []bool{false, true} {
			name := lane + "/response_commits"
			if failResponse {
				name = lane + "/response_fails"
			}
			t.Run(name, func(t *testing.T) {
				c, sink, _ := newSeamTestComponent(t)
				c.decoder = payloadbuiltins.NewTestDecoder(t)
				msg := seamUserMessage("user")
				_, slot, found, err := c.findRetainedDispatchTask(t.Context(), msg)
				require.NoError(t, err)
				require.False(t, found)
				prepared, err := c.prepareNewDispatchTask(t.Context(), msg, slot)
				require.NoError(t, err)
				c.taskEvidence = priorTaskEvidence{data: prepared.data}
				if failResponse {
					c.sendResponseFn = nil // The disconnected real response publication must fail.
				}

				// The disconnected real client would refuse any task publication.
				// Exact retained commitment must still reach the response path.
				if lane == "channel" {
					data, marshalErr := json.Marshal(message.NewBaseMessage(msg.Schema(), &msg, "test"))
					require.NoError(t, marshalErr)
					decision, cause := c.handleUserMessage(t.Context(), data)
					if failResponse {
						require.Equal(t, natsclient.DeliveryDecisionRetry, decision)
						require.ErrorContains(t, cause, "publish user response")
					} else {
						require.Equal(t, natsclient.DeliveryDecisionAck, decision)
						require.NoError(t, cause)
					}
				} else {
					response, submitErr := c.processTaskSubmissionSync(t.Context(), msg)
					require.NoError(t, submitErr, "the optional stream mirror does not replace the synchronous response")
					require.Equal(t, agentic.ResponseTypeStatus, response.Type)
					require.Equal(t, prepared.task.LoopID, response.InReplyTo)
				}
				responses := sink.all()
				if failResponse {
					require.Empty(t, responses)
				} else {
					require.Len(t, responses, 1)
					require.Equal(t, agentic.ResponseTypeStatus, responses[0].Type)
					require.Equal(t, prepared.task.LoopID, responses[0].InReplyTo)
				}
				require.Equal(t, float64(1), testutil.ToFloat64(c.metrics.tasksSubmitted))
			})
		}
	}
}
