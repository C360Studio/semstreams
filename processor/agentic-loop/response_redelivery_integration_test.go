//go:build integration

package agenticloop

import (
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/processor/agentic-loop/internal/looprequest"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

// TestARedeliveredResponseTheLoopAlreadyAppliedIsAcknowledged is the response
// lane's replay guard (owner ruling Q12, 2026-09-23; found by the in-house
// sweep before round 3, recorded as task 8.7.9's traced residual).
//
// Ordering a response against the record answers three of the four cases:
// older is superseded, newer is not yet observable, foreign is quarantined.
// The fourth — a second delivery of the CURRENT request's answer — was
// handled again from the top. Tool EXECUTION survives that, because the
// execution identity is deterministic and TOOL_CALL_OUTCOMES replays the
// outcome, but the loop's CONVERSATION does not: every redelivery appends
// another assistant turn and re-dispatches the batch, and the duplicate turn
// is in the next request the model is asked to answer. On a gated loop the
// re-dispatch also runs while a human is being asked to approve one of those
// very calls.
//
// The fact that separates a first delivery from a replay is not on the
// record, which names the same request either way. It is the outstanding
// mark: HandleModelResponse settles it the moment a response is applied, so a
// response naming the request the record names while the loop is waiting on
// nothing is an answer this process already used. A loop REBUILT for an
// arriving response is not that case — restoreLoopFromRequest marks the
// retained request outstanding from the only evidence it has — so the cold
// arm's first response still applies.
//
// spec: agentic-loop / The loop record names its outstanding request
func TestARedeliveredResponseTheLoopAlreadyAppliedIsAcknowledged(t *testing.T) {
	client := newLoopNATS(t)

	const loopID = "d4f1b607-8c25-4e93-a17b-0f6d3c8e5b29"
	task := agentic.TaskMessage{
		TaskID: "task-response-replay",
		LoopID: loopID,
		Role:   "general",
		Model:  "test-model",
		Prompt: "the prompt whose answer is delivered twice",
	}
	firstRequest := looprequest.ID{LoopID: loopID, Iteration: 1, Retry: 0}.String()
	executeSubject, err := component.ResolveSubject(DefaultConfig().Ports.Outputs, "tool.execute", "replay_tool")
	require.NoError(t, err)

	c, handler := startLoopProcess(t, client, DefaultConfig())
	_, birth := deliverTask(t, c, task)
	require.Equal(t, natsclient.DeliveryDecisionAck, birth.Decision())

	// The counter is a process-wide singleton, so the assertion below is a
	// DELTA taken across the delivery under test.
	drops := func(reason string) float64 {
		return testutil.ToFloat64(c.metrics.modelResponsesDropped.WithLabelValues(reason))
	}

	answer := agentic.AgentResponse{
		RequestID:    firstRequest,
		Status:       agentic.StatusToolCall,
		FinishReason: "tool_calls",
		Message: agentic.ChatMessage{
			Role:      "assistant",
			Content:   "the turn that dispatches one call",
			ToolCalls: []agentic.ToolCall{{ID: "call-replay-a", Name: "replay_tool"}},
		},
	}
	retainModelResponse(t, client, answer)

	_, applied := deliverResponse(t, c, answer)
	require.Equal(t, natsclient.DeliveryDecisionAck, applied.Decision())
	require.Equal(t, uint64(1), messagesOn(t, client, executeSubject),
		"the first delivery is the one that dispatches the batch")
	after := loopRecordOf(t, c, loopID)

	before := drops("already_applied")

	// The same bytes again: an acknowledgement lost between the server and
	// this process, which is ordinary at-least-once delivery.
	_, replayed := deliverResponse(t, c, answer)

	require.Equal(t, natsclient.DeliveryDecisionAck, replayed.Decision(),
		"an answer this loop already used is finished, not retried")
	require.Equal(t, uint64(1), messagesOn(t, client, executeSubject),
		"the replay re-dispatched a batch that is already running")

	assistantTurns := 0
	for _, msg := range handler.loopManager.GetContextManager(loopID).GetContext() {
		if msg.Role == "assistant" {
			assistantTurns++
		}
	}
	require.Equal(t, 1, assistantTurns,
		"the replay appended the assistant turn a second time, so the duplicate rides the next "+
			"request the model is asked to answer")

	replayedRecord := loopRecordOf(t, c, loopID)
	require.Equal(t, after.revision, replayedRecord.revision,
		"a response that changed nothing must not move the record's revision")
	require.Equal(t, before+1, drops("already_applied"),
		"a drop is a declared event: it carries the reason value an operator greps for")
}
