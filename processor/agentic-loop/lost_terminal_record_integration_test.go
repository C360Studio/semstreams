//go:build integration

package agenticloop

import (
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/internal/deliverylane"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/processor/agentic-loop/internal/looprequest"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

// W1 of #1377, the documented bound (OQ1 (a)): a terminal whose record write
// lost its compare-and-swap after COMPLETE_<loopID> and its event landed. The
// test asserts the bound, not a fix — there is no mutation, because nothing
// here is new code (design § 4 T1).
//
// Two processes over one broker, the predecessor/replacement pattern: A holds
// the loop at R1 with the model request outstanding; B, with no memory of the
// loop, applies a tool result cold and advances the record to R2; A's
// completion for R1 then commits the durable terminal and loses its record
// write. The loop runs on in B under a durable terminal until its own.
//
// Arm (c) of the design — the in-process source, this process's own step-0
// adopt on a loop it still holds (loop_evidence.go) — is not forced here: no
// seam reaches it without a production hook. It stays unproven (design § 10
// item 5).

// lostTerminalRecord builds the W1 state and returns B, its handler, the loop
// and the durable terminal's bytes.
func lostTerminalRecord(t *testing.T) (*Component, *MessageHandler, *natsclient.Client, string, []byte) {
	t.Helper()
	client := newLoopNATS(t)
	a, handlerA := startLoopProcess(t, client, DefaultConfig())
	loopID, firstRequest := bornLoop(t, a, handlerA, "task-w1")
	secondRequest := looprequest.ID{LoopID: loopID, Iteration: 2, Retry: 0}.String()

	batch := agentic.AgentResponse{
		RequestID: firstRequest, Status: agentic.StatusToolCall, FinishReason: "tool_calls",
		Message: agentic.ChatMessage{Role: "assistant", ToolCalls: []agentic.ToolCall{{ID: "call-w1", Name: "search"}}},
	}
	retainModelResponse(t, client, batch)

	b, handlerB := startLoopProcess(t, client, DefaultConfig())
	_, applied := deliverToolResult(t, b, agentic.ToolResult{
		CallID: "call-w1", Name: "search", Content: "found", LoopID: loopID, RequestID: firstRequest,
		ExecutionID: deriveToolExecutionID(firstRequest, "call-w1", 1), CallOrdinal: 1,
	})
	require.Equal(t, natsclient.DeliveryDecisionAck, applied.Decision())
	advanced := loopRecordOf(t, b, loopID)
	require.Equal(t, secondRequest, advanced.entity.PublishedRequestID, "fixture: B advanced the record to R2")
	_, err := handlerB.GetLoop(loopID)
	require.NoError(t, err, "fixture: B holds the loop at R2")

	// A still holds the loop at R1, and its completion for R1 arrives. The
	// loop metrics are a process-wide singleton, so counts are deltas.
	completedBefore := testutil.ToFloat64(a.metrics.loopsCompleted)
	completion, err := handlerA.HandleModelResponse(t.Context(), loopID, agentic.AgentResponse{
		RequestID: firstRequest, Status: agentic.StatusComplete, FinishReason: "stop",
		Message: agentic.ChatMessage{Role: "assistant", Content: "done in A"},
	})
	require.NoError(t, err)
	require.Equal(t, agentic.LoopStateComplete, completion.State)
	lost := a.persistHandlerResult(t.Context(), completion)

	require.ErrorIs(t, lost, natsclient.ErrKVRevisionMismatch, "A's record write loses its compare-and-swap")
	require.False(t, errs.IsFatal(lost), "a lost compare-and-swap is retried, never quarantined")
	_, heldErr := handlerA.GetLoop(loopID)
	require.ErrorIs(t, heldErr, ErrLoopNotFound, "the lost write released A's loop")
	entry, err := b.loopsBucket.Get(t.Context(), terminalMarkerKey(loopID))
	require.NoError(t, err, "the durable terminal landed")
	require.Equal(t, uint64(1), messagesOn(t, client, "agent.complete."+loopID), "and so did its event")
	live := loopRecordOf(t, b, loopID)
	require.Equal(t, loopPresenceLive, live.presence, "the bound: the record stays live under a durable terminal")
	require.Equal(t, advanced.revision, live.revision)
	require.Equal(t, completedBefore, testutil.ToFloat64(a.metrics.loopsCompleted),
		"a commit that did not land counts nothing")

	// A's input, redelivered to B, is older than the record.
	msg := &loopDeliveryOwnerMsg{data: baseMessageBytes(t, &agentic.AgentResponse{
		RequestID: firstRequest, Status: agentic.StatusComplete, FinishReason: "stop",
		Message: agentic.ChatMessage{Role: "assistant", Content: "done in A"},
	})}
	redelivered, admitted := deliverylane.Consume(t.Context(), msg,
		heartbeatPolicyForTest(t, "agent.response", b.handleResponseMessage), deliverylane.NewAdmission(nil, nil))
	require.True(t, admitted)
	require.Equal(t, natsclient.DeliveryDecisionAck, redelivered.Decision(),
		"the redelivered terminal input is acknowledged as older")
	require.Equal(t, live.revision, loopRecordOf(t, b, loopID).revision, "and writes nothing")
	_, err = handlerB.GetLoop(loopID)
	require.NoError(t, err, "B still runs the loop under the durable terminal")
	return b, handlerB, client, loopID, entry.Value()
}

// spec: agentic-loop / The loop record names its outstanding request
func TestALostTerminalRecordConvergesAtTheLoopsNextTerminal(t *testing.T) {
	t.Run("a terminal of the same kind adopts the durable terminal", func(t *testing.T) {
		b, handlerB, client, loopID, marker := lostTerminalRecord(t)
		secondRequest := looprequest.ID{LoopID: loopID, Iteration: 2, Retry: 0}.String()
		completedBefore := testutil.ToFloat64(b.metrics.loopsCompleted)

		completion, err := handlerB.HandleModelResponse(t.Context(), loopID, agentic.AgentResponse{
			RequestID: secondRequest, Status: agentic.StatusComplete, FinishReason: "stop",
			Message: agentic.ChatMessage{Role: "assistant", Content: "done in B"},
		})
		require.NoError(t, err)
		require.NoError(t, b.persistHandlerResult(t.Context(), completion))

		record := loopRecordOf(t, b, loopID)
		require.Equal(t, agentic.LoopStateComplete, record.entity.State, "the record converges terminal")
		entry, err := b.loopsBucket.Get(t.Context(), terminalMarkerKey(loopID))
		require.NoError(t, err)
		require.Equal(t, string(marker), string(entry.Value()), "the durable terminal is never overwritten")
		require.Equal(t, uint64(2), messagesOn(t, client, "agent.complete."+loopID),
			"A's event, and the saved terminal republished by the adopting commit")
		require.Equal(t, completedBefore+1, testutil.ToFloat64(b.metrics.loopsCompleted),
			"the terminal is counted once")
		_, heldErr := handlerB.GetLoop(loopID)
		require.ErrorIs(t, heldErr, ErrLoopNotFound)
	})

	t.Run("a terminal of a different kind is refused", func(t *testing.T) {
		b, handlerB, client, loopID, marker := lostTerminalRecord(t)
		secondRequest := looprequest.ID{LoopID: loopID, Iteration: 2, Retry: 0}.String()
		before := loopRecordOf(t, b, loopID)
		failedBefore := testutil.ToFloat64(b.metrics.loopsFailed.WithLabelValues("model_error"))

		failure, err := handlerB.HandleModelResponse(t.Context(), loopID, agentic.AgentResponse{
			RequestID: secondRequest, Status: agentic.StatusError, Error: "the provider failed",
		})
		require.NoError(t, err)
		require.Equal(t, agentic.LoopStateFailed, failure.State)
		err = b.persistHandlerResult(t.Context(), failure)

		require.Error(t, err)
		require.True(t, errs.IsFatal(err), "a different kind is refused and quarantined: the first terminal wins")
		entry, getErr := b.loopsBucket.Get(t.Context(), terminalMarkerKey(loopID))
		require.NoError(t, getErr)
		require.Equal(t, string(marker), string(entry.Value()), "the marker is still the completion")
		require.Zero(t, messagesOn(t, client, "agent.failed."+loopID), "no failure event is published")
		require.Equal(t, uint64(1), messagesOn(t, client, "agent.complete."+loopID),
			"and the durable completion's event is not republished by a refused terminal")
		after := loopRecordOf(t, b, loopID)
		require.Equal(t, before.revision, after.revision, "the record is not written")
		require.Equal(t, failedBefore, testutil.ToFloat64(b.metrics.loopsFailed.WithLabelValues("model_error")),
			"a refused terminal is not counted")
	})
}
