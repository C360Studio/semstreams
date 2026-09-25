//go:build integration

package agenticloop

import (
	"context"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/internal/deliverylane"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/processor/agentic-loop/internal/looprequest"
	"github.com/stretchr/testify/require"
)

// midAdvance is one loop held open in the interval between a tool batch's
// completion and the PubAck of the request that completion minted.
//
// It is built by running production: a real birth, a real two-call batch, a
// real apply of the first result, and the second result delivered through the
// real tool lane on a goroutine that parks inside publishResults' identity
// read. At that instant the tool lane has already incremented Iterations,
// drained PendingToolResults and tracked R2 as outstanding IN MEMORY, and none
// of it is durable yet: the record still says {R1, 0, {A}} and the stream
// still retains only R1.
type midAdvance struct {
	component      *Component
	loopID         string
	firstRequest   string
	secondRequest  string
	executeSubject string
	appliedA       string
	resultB        agentic.ToolResult
	gate           *gatedEvidenceReader
	release        func()
	carrier        <-chan natsclient.DeliveryResult
}

func startMidAdvance(t *testing.T, client *natsclient.Client, loopID, taskID, tool string) midAdvance {
	t.Helper()
	task := agentic.TaskMessage{
		TaskID: taskID,
		LoopID: loopID,
		Role:   "general",
		Model:  "test-model",
		Prompt: "the prompt whose batch completes while a turn is typed",
	}
	firstRequest := looprequest.ID{LoopID: loopID, Iteration: 1, Retry: 0}.String()
	secondRequest := looprequest.ID{LoopID: loopID, Iteration: 2, Retry: 0}.String()

	c, handler := startLoopProcess(t, client, DefaultConfig())
	_, birth := deliverTask(t, c, task)
	require.Equal(t, natsclient.DeliveryDecisionAck, birth.Decision())

	batch := agentic.AgentResponse{
		RequestID:    firstRequest,
		Status:       agentic.StatusToolCall,
		FinishReason: "tool_calls",
		Message: agentic.ChatMessage{
			Role: "assistant",
			ToolCalls: []agentic.ToolCall{
				{ID: "call-advance-a", Name: tool},
				{ID: "call-advance-b", Name: tool},
			},
		},
	}
	retainModelResponse(t, client, batch)
	dispatch, err := handler.HandleModelResponse(t.Context(), loopID, batch)
	require.NoError(t, err)
	require.NoError(t, c.persistHandlerResult(t.Context(), dispatch))
	callA, executeSubject := dispatchedToolCall(t, dispatch)

	resultA := agentic.ToolResult{
		CallID: callA.ID, Name: callA.Name, Content: "the first tool answered", LoopID: loopID,
		RequestID: callA.RequestID, ExecutionID: callA.ExecutionID, CallOrdinal: callA.CallOrdinal,
	}
	_, appliedA := deliverToolResult(t, c, resultA)
	require.Equal(t, natsclient.DeliveryDecisionAck, appliedA.Decision())
	require.Equal(t, uint64(2), messagesOn(t, client, executeSubject),
		"the applied result released its sibling, so the batch's second call is outstanding")

	applied := loopRecordOf(t, c, loopID)
	require.Equal(t, firstRequest, applied.entity.PublishedRequestID)
	require.Equal(t, 0, applied.entity.Iterations)
	require.Contains(t, applied.entity.PendingToolResults, resultA.ExecutionID,
		"the durable state this interval starts from is a half-applied first batch")

	// The gate IS the interval: publishResults consults the retained request
	// before it sends the request the completion minted, and held there the
	// mint is in memory while the stream still retains only R1.
	gate := &gatedEvidenceReader{entered: make(chan struct{}, 1), release: make(chan struct{})}
	gate.retained.Store(firstRequest)
	c.requestEvidence = gate
	released := make(chan struct{})
	release := func() {
		select {
		case <-released:
		default:
			close(released)
			close(gate.release)
		}
	}
	t.Cleanup(release)

	resultB := agentic.ToolResult{
		CallID: "call-advance-b", Name: tool, Content: "the sibling answered",
		LoopID: loopID, RequestID: firstRequest,
		ExecutionID: deriveToolExecutionID(firstRequest, "call-advance-b", 2), CallOrdinal: 2,
	}
	// Built on the test goroutine: both helpers assert, and an assertion from
	// the delivery goroutine would not stop the test.
	msg := &loopDeliveryOwnerMsg{data: baseMessageBytes(t, &resultB)}
	policy := heartbeatPolicyForTest(t, "tool.result", c.handleToolResultMessage)
	carrier := make(chan natsclient.DeliveryResult, 1)
	go func() {
		delivered, _ := deliverylane.Consume(
			context.Background(), msg, policy, deliverylane.NewAdmission(nil, nil))
		carrier <- delivered
	}()
	<-gate.entered

	return midAdvance{
		component:      c,
		loopID:         loopID,
		firstRequest:   firstRequest,
		secondRequest:  secondRequest,
		executeSubject: executeSubject,
		appliedA:       resultA.ExecutionID,
		resultB:        resultB,
		gate:           gate,
		release:        release,
		carrier:        carrier,
	}
}

// deferContinuation admits a new turn to the loop while its request is
// unpublished, which is the one sibling writer that can reach the record in
// this interval, and returns the record that write left behind.
func deferContinuation(t *testing.T, c *Component, loopID, taskID string) loopRecord {
	t.Helper()
	_, deferred := deliverTask(t, c, agentic.TaskMessage{
		TaskID: taskID,
		LoopID: loopID,
		Role:   "general",
		Model:  "test-model",
		Prompt: "a turn typed while the agent was thinking",
	})
	require.Equal(t, natsclient.DeliveryDecisionAck, deferred.Decision(),
		"a deferred continuation is admitted, not refused: the turn is in the loop's context")
	record := loopRecordOf(t, c, loopID)
	require.True(t, record.entity.PendingContinuation,
		"the marker is the whole durable effect this delivery owns")
	require.Empty(t, record.entity.PendingContinuationRequestID,
		"a turn admitted now is in no retained request, so the marker names no carrier")
	return record
}

// TestADeferredContinuationWritesOnlyTheMarkerItOwns is invariant I3 held
// against the one writer that is not the lane doing the advancing (owner Codex
// round 3 on PR #1361, finding 2; #1330 docket option (b), Q10 unanswered).
//
// Moving the request-ID stamp to the carrier closed half of the window and
// left the other half open. By the time the carrier publishes R2,
// handleToolsComplete has already incremented Iterations and drained
// PendingToolResults in the shared entity, and every record write in this
// process RENDERS that entity. A continuation admitted in that interval
// defers, persists, and commits {R1, 1, empty} — iterations moved in an update
// whose published_request_id did not, which is exactly what I3 forbids, and a
// crash there leaves a record that claims a whole batch was consumed by a
// request the stream never received.
//
// A lane writes only the fields it OWNS. The deferred turn owns its marker, so
// it overlays the marker onto the record it READ and compare-and-swaps that,
// and the tool lane's unpublished advance rides its own write, once R2 is
// really retained.
//
// spec: agentic-loop / The loop record names its outstanding request
func TestADeferredContinuationWritesOnlyTheMarkerItOwns(t *testing.T) {
	client := newLoopNATS(t)

	t.Run("the advance reaches the record on the carrier's own write", func(t *testing.T) {
		const loopID = "6e2a94d1-3f57-4b08-9c6d-1a5e8f30b742"
		stage := startMidAdvance(t, client, loopID, "task-deferred-advance", "advance_tool")

		deferred := deferContinuation(t, stage.component, loopID, "task-deferred-second-turn")

		require.Equal(t, stage.firstRequest, deferred.entity.PublishedRequestID,
			"the sibling must not name a request whose PubAck has not landed")
		require.Equal(t, 0, deferred.entity.Iterations,
			"iterations moved in an update whose published_request_id did not: I3, and the record "+
				"now claims a batch was consumed by a request the stream does not retain")
		require.Contains(t, deferred.entity.PendingToolResults, stage.appliedA,
			"the sibling drained an applied set the advance had not committed, so a replay of the "+
				"batch's results reads them as never applied")

		stage.release()
		require.Equal(t, natsclient.DeliveryDecisionAck, (<-stage.carrier).Decision())

		committed := loopRecordOf(t, stage.component, loopID)
		require.Equal(t, stage.secondRequest, committed.entity.PublishedRequestID,
			"the advance rides the carrier's own write, after the PubAck")
		require.Equal(t, 1, committed.entity.Iterations)
		require.Empty(t, committed.entity.PendingToolResults,
			"the completed batch's applied set is drained by the write that names the next request")
		require.True(t, committed.entity.PendingContinuation,
			"the marker the sibling wrote must survive the advance that follows it")
		require.Equal(t, uint64(2), messagesOn(t, client, "agent.request."+loopID),
			"the request the record now names is really on the stream: R1 and R2, one each")
	})

	t.Run("a crash in the interval leaves a record the batch can be replayed against", func(t *testing.T) {
		const loopID = "0b9c5e83-7d41-4a26-8f50-2c3e6a1b9d74"
		stage := startMidAdvance(t, client, loopID, "task-deferred-crash", "crash_tool")

		deferContinuation(t, stage.component, loopID, "task-deferred-crash-turn")

		// The carrier is never released: R2's PubAck never lands, and the
		// record the replacement meets is whatever the sibling left.
		replacement, _ := startLoopProcess(t, client, DefaultConfig())
		_, replayed := deliverToolResult(t, replacement, stage.resultB)

		require.Equal(t, natsclient.DeliveryDecisionAck, replayed.Decision())
		recovered := loopRecordOf(t, replacement, loopID)
		require.Equal(t, stage.secondRequest, recovered.entity.PublishedRequestID,
			"the replacement completes the batch the record describes and mints the loop's NEXT "+
				"ordinal; a record that had already counted the iteration skips one and spends the "+
				"budget twice")
		require.Equal(t, 1, recovered.entity.Iterations)
		require.Equal(t, uint64(2), messagesOn(t, client, stage.executeSubject),
			"the record named the batch's applied result, so recovery re-dispatched nothing: a "+
				"drained applied set re-runs a tool that already ran")
	})
}
