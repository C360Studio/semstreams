package agenticloop

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
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
	toolResultBytes := func(t *testing.T, callID string) []byte {
		t.Helper()
		toolResult := &agentic.ToolResult{CallID: callID, Name: "search", Content: "executor ran this"}
		data, err := json.Marshal(message.NewBaseMessage(toolResult.Schema(), toolResult, "test"))
		require.NoError(t, err)
		return data
	}
	timedOutLoop := func(t *testing.T) (*Component, *recordingLoopBucket, string, string) {
		t.Helper()
		handler := NewMessageHandler(DefaultConfig())
		loopID, err := handler.loopManager.CreateLoop("task-timeout", "general", "model", 3)
		require.NoError(t, err)
		callID := "call-timeout"
		handler.loopManager.TrackToolCall(callID, loopID)
		require.NoError(t, handler.loopManager.AddPendingTool(loopID, callID))
		// Already past its deadline when the result lands: the handler fails the
		// loop, builds the failure record, and returns it with a fatal error.
		require.NoError(t, handler.loopManager.SetTimeout(loopID, -time.Second))
		c := releaseTestComponent(t, handler)
		bucket := &recordingLoopBucket{}
		c.loopsBucket = bucket
		return c, bucket, loopID, callID
	}

	t.Run("a terminal handler failure is written before it is acknowledged", func(t *testing.T) {
		c, bucket, loopID, callID := timedOutLoop(t)
		msg := &loopDeliveryOwnerMsg{data: toolResultBytes(t, callID)}
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
		c, bucket, _, callID := timedOutLoop(t)
		bucket.fail = errKVUnavailable
		msg := &loopDeliveryOwnerMsg{data: toolResultBytes(t, callID)}
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
		handler.loopManager.TrackToolCall(callID, "2f1a6c9e-9f2d-4b27-8f4a-3c9f0e6d51aa")
		c := releaseTestComponent(t, handler)
		msg := &loopDeliveryOwnerMsg{data: toolResultBytes(t, callID)}
		result, admitted := consumeAdmittedDelivery(
			t.Context(), msg, newPolicy(t, c.handleToolResultMessage), newDeliveryLaneAdmission(nil))
		require.True(t, admitted)
		require.Equal(t, natsclient.DeliveryDecisionQuarantine, result.Decision())
		require.Zero(t, msg.acks.Load()+msg.naks.Load()+msg.terms.Load())
	})

	t.Run("a cancelled process retries instead of latching a false fatal", func(t *testing.T) {
		c, _, _, callID := timedOutLoop(t)
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		msg := &loopDeliveryOwnerMsg{data: toolResultBytes(t, callID)}
		result, admitted := consumeAdmittedDelivery(
			ctx, msg, newPolicy(t, c.handleToolResultMessage), newDeliveryLaneAdmission(nil))
		require.True(t, admitted)
		require.Equal(t, natsclient.DeliveryDecisionRetry, result.Decision(),
			"a clean shutdown must not latch delivery ownership lost")
		require.Equal(t, int32(1), msg.naks.Load())
		require.Zero(t, msg.acks.Load()+msg.terms.Load())
	})
}
