package agenticloop

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/types"
	"github.com/stretchr/testify/require"
)

// The tool-result lane's handler-error branch used to log and return nil, which
// is ACK (#1343). The worst case was the terminal one: HandleToolResult's
// timeout branch transitions the loop to failed, builds its failure record and
// its failure publications, and returns them WITH the error — so the branch
// acknowledged a terminal failure that was never written and never published,
// and the loop record stayed non-terminal with the input that would have
// settled it gone.
//
// The result decides now. A terminal result is persisted like any other
// terminal result and settles on that write; anything else is commit-unknown
// and quarantines, with cancellation the one ordinary Retry.
//
// spec: agentic-loop / Loop input classes settle after owner-specific durable done
func TestToolResultHandlerFailureSettlesOnTheDurableRecord(t *testing.T) {
	newPolicy := func(t *testing.T, handler inputHandler) natsclient.HeartbeatDeliveryPolicy {
		t.Helper()
		return heartbeatPolicyForTest(t, "tool.result", handler)
	}
	// The result carries both identities because the lane routes on the
	// framework execution id (#1328) while the loop's pending-tool set is still
	// keyed by the provider call id. A fixture that supplied only one of them
	// would never reach the handler, and every assertion below would pass
	// vacuously against a settled-drop ACK.
	toolResultBytes := func(t *testing.T, executionID, callID string) []byte {
		t.Helper()
		toolResult := &agentic.ToolResult{
			ExecutionID: executionID, CallID: callID, Name: "search", Content: "executor ran this"}
		data, err := json.Marshal(message.NewBaseMessage(toolResult.Schema(), toolResult, "test"))
		require.NoError(t, err)
		return data
	}
	timedOutLoop := func(t *testing.T) (*Component, *recordingLoopBucket, string, string, string) {
		t.Helper()
		handler := NewMessageHandler(DefaultConfig())
		loopID, err := handler.loopManager.CreateLoop("task-timeout", "general", "model", 3)
		require.NoError(t, err)
		callID := "call-timeout"
		executionID := "execution-timeout"
		handler.loopManager.TrackToolCall(executionID, loopID)
		require.NoError(t, handler.loopManager.AddPendingTool(loopID, callID))
		// Already past its deadline when the result lands: the handler fails the
		// loop, builds the failure record, and returns it with a fatal error.
		require.NoError(t, handler.loopManager.SetTimeout(loopID, -time.Second))
		c := releaseTestComponent(t, handler)
		bucket := &recordingLoopBucket{}
		c.loopsBucket = bucket
		return c, bucket, loopID, executionID, callID
	}

	t.Run("a terminal handler failure is written before it is acknowledged", func(t *testing.T) {
		c, bucket, loopID, executionID, callID := timedOutLoop(t)
		msg := &loopDeliveryOwnerMsg{data: toolResultBytes(t, executionID, callID)}
		result, admitted := consumeAdmittedDelivery(
			t.Context(), msg, newPolicy(t, c.handleToolResultMessage), newDeliveryLaneAdmission(nil))
		require.True(t, admitted)
		require.Equal(t, natsclient.DeliveryDecisionAck, result.Decision())
		require.Equal(t, int32(1), msg.acks.Load())

		// The ACK is only correct because the terminal state reached KV. Before
		// this, the same delivery ACKed with nothing written at all.
		persisted, ok := bucket.value(loopID)
		require.True(t, ok, "terminal tool-result failure acknowledged without persisting the loop")
		var entity agentic.LoopEntity
		require.NoError(t, json.Unmarshal(persisted, &entity))
		require.Equal(t, agentic.LoopStateFailed, entity.State)

		// And the terminal RECORD, which is the half round 3 found missing: a
		// watcher reads the outcome out of COMPLETE_<loopID>, not out of the
		// loop key, and this branch used to stamp graph triples and ACK
		// without ever writing it.
		record, ok := bucket.value("COMPLETE_" + loopID)
		require.True(t, ok,
			"terminal tool-result failure acknowledged with COMPLETE_<loopID> absent: "+
				"every KV watcher sees a loop that ended with no result")
		var failure agentic.LoopFailedEvent
		require.NoError(t, json.Unmarshal(record, &failure))
		require.Equal(t, agentic.OutcomeFailed, failure.Outcome)
		require.Equal(t, loopID, failure.LoopID)
	})

	t.Run("a terminal handler failure whose write fails quarantines", func(t *testing.T) {
		c, bucket, _, executionID, callID := timedOutLoop(t)
		bucket.fail = errKVUnavailable
		msg := &loopDeliveryOwnerMsg{data: toolResultBytes(t, executionID, callID)}
		result, admitted := consumeAdmittedDelivery(
			t.Context(), msg, newPolicy(t, c.handleToolResultMessage), newDeliveryLaneAdmission(nil))
		require.True(t, admitted)
		require.Equal(t, natsclient.DeliveryDecisionQuarantine, result.Decision())
		require.True(t, result.OwnerStopRequired())
		require.Zero(t, msg.acks.Load()+msg.naks.Load()+msg.terms.Load())
	})

	t.Run("a non-terminal handler failure quarantines rather than discarding the result", func(t *testing.T) {
		handler := NewMessageHandler(DefaultConfig())
		// Routed to a loop this manager never created: HandleToolResult fails at
		// GetLoop with an empty, non-terminal result. Nothing to persist, an
		// executor's work in hand, so the lane stops instead of ACKing it away.
		callID := "call-unrouted"
		executionID := "execution-unrouted"
		handler.loopManager.TrackToolCall(executionID, "2f1a6c9e-9f2d-4b27-8f4a-3c9f0e6d51aa")
		c := releaseTestComponent(t, handler)
		msg := &loopDeliveryOwnerMsg{data: toolResultBytes(t, executionID, callID)}
		result, admitted := consumeAdmittedDelivery(
			t.Context(), msg, newPolicy(t, c.handleToolResultMessage), newDeliveryLaneAdmission(nil))
		require.True(t, admitted)
		require.Equal(t, natsclient.DeliveryDecisionQuarantine, result.Decision())
		require.Zero(t, msg.acks.Load()+msg.naks.Load()+msg.terms.Load())
	})

	t.Run("a cancelled process retries instead of latching a false fatal", func(t *testing.T) {
		c, _, _, executionID, callID := timedOutLoop(t)
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		msg := &loopDeliveryOwnerMsg{data: toolResultBytes(t, executionID, callID)}
		result, admitted := consumeAdmittedDelivery(
			ctx, msg, newPolicy(t, c.handleToolResultMessage), newDeliveryLaneAdmission(nil))
		require.True(t, admitted)
		require.Equal(t, natsclient.DeliveryDecisionRetry, result.Decision(),
			"a clean shutdown must not latch delivery ownership lost")
		require.Equal(t, int32(1), msg.naks.Load())
		require.Zero(t, msg.acks.Load()+msg.terms.Load())
	})
}

// cancellingTodoReader fires a cancellation from inside HandleToolResult, at
// the one point production reads it: prependIterationContext
// (handlers.go:2551) runs after IncrementIteration and GetAndClearToolResults
// have already moved this loop, and the ctx check that observes the
// cancellation is the one two lines later. It is the smallest production seam
// that produces a post-mutation cancel without a fake handler.
type cancellingTodoReader struct {
	cancel context.CancelFunc
	calls  atomic.Int32
}

func (r *cancellingTodoReader) ReadTodos(_ context.Context, _ string) ([]TodoState, error) {
	r.calls.Add(1)
	r.cancel()
	return nil, nil
}

// Cancellation is the one Retry carved out of "otherwise Quarantine", and it is
// only sound where the cancellation preceded every mutation. HandleToolResult
// checks its context three times and the other two run after StoreToolResult,
// RemovePendingTool, IncrementIteration and GetAndClearToolResults — a replay
// of those lands on a loop that has already advanced, which the round-3 probe
// measured: iterations 0 → 1 with zero publications, then a replay that hit the
// budget and returned terminal max_iterations without ever issuing the
// interrupted request.
//
// The cancellation source is not only shutdown: delivery_settlement.go:366-373
// cancels the work context when a heartbeat InProgress fails, in a live
// process.
//
// spec: agentic-loop / Loop input classes settle after owner-specific durable done
func TestToolResultCancellationRetriesOnlyBeforeMutation(t *testing.T) {
	toolResultBytes := func(t *testing.T, executionID, callID string) []byte {
		t.Helper()
		toolResult := &agentic.ToolResult{
			ExecutionID: executionID, CallID: callID, Name: "search", Content: "executor ran this"}
		data, err := json.Marshal(message.NewBaseMessage(toolResult.Schema(), toolResult, "test"))
		require.NoError(t, err)
		return data
	}
	// A loop with one dispatched tool call, so the arriving result completes
	// the batch and drives handleToolsComplete.
	loopAwaitingItsOnlyTool := func(t *testing.T) (*Component, *MessageHandler, string, string, string) {
		t.Helper()
		handler := NewMessageHandler(DefaultConfig())
		handler.SetPlatform(types.PlatformMeta{Org: "acme", Platform: "ops"})
		loopID, err := handler.loopManager.CreateLoop("task-cancel-boundary", "general", "model", 3)
		require.NoError(t, err)
		callID := "call-cancel-boundary"
		_, err = handler.HandleModelResponse(t.Context(), loopID, agentic.AgentResponse{
			RequestID: "request-cancel-boundary", Status: "tool_call",
			Message: agentic.ChatMessage{Role: "assistant", ToolCalls: []agentic.ToolCall{{ID: callID, Name: "search"}}},
		})
		require.NoError(t, err)
		c := releaseTestComponent(t, handler)
		c.loopsBucket = &recordingLoopBucket{}
		return c, handler, loopID, dispatchedExecutionID(t, handler.loopManager, loopID), callID
	}

	t.Run("cancelled after the loop advanced, so the delivery quarantines", func(t *testing.T) {
		c, handler, loopID, executionID, callID := loopAwaitingItsOnlyTool(t)
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		reader := &cancellingTodoReader{cancel: cancel}
		handler.SetTodoReader(reader)

		before := handler.loopManager.GetCurrentIteration(loopID)
		msg := &loopDeliveryOwnerMsg{data: toolResultBytes(t, executionID, callID)}
		result, admitted := consumeAdmittedDelivery(
			ctx, msg, heartbeatPolicyForTest(t, "tool.result", c.handleToolResultMessage),
			newDeliveryLaneAdmission(nil))
		require.True(t, admitted)
		require.Positive(t, reader.calls.Load(), "the fixture must cancel from inside the handler")

		// The mutation this delivery cannot rebuild, measured rather than
		// assumed: the loop advanced and nothing was published for it.
		require.Greater(t, handler.loopManager.GetCurrentIteration(loopID), before,
			"the fixture must cancel AFTER the iteration advanced, or it proves nothing")

		require.Equal(t, natsclient.DeliveryDecisionQuarantine, result.Decision(),
			"a cancellation after the loop advanced is a partial effect, not a clean stop")
		require.True(t, result.OwnerStopRequired())
		require.Zero(t, msg.acks.Load()+msg.naks.Load()+msg.terms.Load())
	})

	t.Run("cancelled before the handler touched anything, so the delivery retries", func(t *testing.T) {
		c, handler, loopID, executionID, callID := loopAwaitingItsOnlyTool(t)
		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		before := handler.loopManager.GetCurrentIteration(loopID)
		msg := &loopDeliveryOwnerMsg{data: toolResultBytes(t, executionID, callID)}
		result, admitted := consumeAdmittedDelivery(
			ctx, msg, heartbeatPolicyForTest(t, "tool.result", c.handleToolResultMessage),
			newDeliveryLaneAdmission(nil))
		require.True(t, admitted)
		require.Equal(t, natsclient.DeliveryDecisionRetry, result.Decision(),
			"a clean stop that mutated nothing must not latch delivery ownership lost")
		require.Equal(t, int32(1), msg.naks.Load())
		require.Equal(t, before, handler.loopManager.GetCurrentIteration(loopID),
			"the pre-mutation case is only retryable because nothing moved")
	})
}

// dispatchedExecutionID reads back the framework execution identity the loop
// minted for its one dispatched call (#1328). It reads the routing map
// production writes rather than re-deriving the id from its inputs: a fixture
// that re-derived it would still route after a change to the derivation, and
// route to nothing after a change to what is tracked.
func dispatchedExecutionID(t *testing.T, m *LoopManager, loopID string) string {
	t.Helper()
	m.mu.RLock()
	defer m.mu.RUnlock()
	var found []string
	for executionID, owner := range m.toolCallToLoop {
		if owner == loopID {
			found = append(found, executionID)
		}
	}
	require.Len(t, found, 1, "fixture expects exactly one dispatched tool call for loop %s", loopID)
	return found[0]
}
