package agenticloop

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/internal/deliverylane"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/stretchr/testify/require"
)

// The model-response lane reaches handleLoopFailure through its own wiring at
// handleResponseMessage, and that wiring is what this test holds. The lane used
// to call the failure path for effect and return nil — ACK — so a loop whose
// terminal failure never reached KV was settled anyway and the response that
// would have driven it was gone.
//
// The sibling test on handleSpawnIdentityFailure covers the failure path
// itself, not this lane's use of it: reverting THIS return to
// `_ = c.handleLoopFailure(...); return nil` leaves that test green, because it
// is the task lane. The assertion here is the return value on the response
// lane, so both subtests drive the production callback through
// deliverylane.Consume rather than calling handleLoopFailure directly.
//
// spec: agentic-loop / Loop input classes settle after owner-specific durable done
func TestResponseHandlerFailureSettlesOnTheDurableRecord(t *testing.T) {
	responseBytes := func(t *testing.T, requestID string) []byte {
		t.Helper()
		response := &agentic.AgentResponse{
			RequestID: requestID,
			Status:    agentic.StatusComplete,
			Message:   agentic.ChatMessage{Role: "assistant", Content: "answer"},
		}
		data, err := json.Marshal(message.NewBaseMessage(response.Schema(), response, "test"))
		require.NoError(t, err)
		return data
	}
	// A loop already past its deadline when the response lands:
	// HandleModelResponse fails it, builds the failure record, and returns that
	// record WITH a fatal error — the terminal business failure this lane must
	// establish before it may acknowledge.
	timedOutLoop := func(t *testing.T) (*Component, *recordingLoopBucket, string, string) {
		t.Helper()
		handler := NewMessageHandler(DefaultConfig())
		loopID, err := handler.loopManager.CreateLoop("task-response-timeout", "general", "model", 3)
		require.NoError(t, err)
		requestID := handler.loopManager.GenerateRequestID(loopID)
		handler.loopManager.TrackRequest(requestID, loopID)
		require.NoError(t, handler.loopManager.SetTimeout(loopID, -time.Second))
		c := releaseTestComponent(t, handler)
		bucket := &recordingLoopBucket{}
		c.loopsBucket = bucket
		seedLoopRecord(t, c, loopID)
		return c, bucket, loopID, requestID
	}

	t.Run("a terminal business failure is written before it is acknowledged", func(t *testing.T) {
		c, bucket, loopID, requestID := timedOutLoop(t)
		msg := &loopDeliveryOwnerMsg{data: responseBytes(t, requestID)}
		result, admitted := deliverylane.Consume(
			t.Context(), msg,
			heartbeatPolicyForTest(t, "agent.response", c.handleResponseMessage),
			deliverylane.NewAdmission(nil, nil))
		require.True(t, admitted)
		require.Equal(t, natsclient.DeliveryDecisionAck, result.Decision())
		require.Equal(t, int32(1), msg.acks.Load())

		persisted, ok := bucket.value(loopID)
		require.True(t, ok, "terminal model-response failure acknowledged without persisting the loop")
		var entity agentic.LoopEntity
		require.NoError(t, json.Unmarshal(persisted, &entity))
		require.Equal(t, agentic.LoopStateFailed, entity.State)
	})

	t.Run("a terminal business failure that cannot be recorded quarantines", func(t *testing.T) {
		c, bucket, _, requestID := timedOutLoop(t)
		bucket.fail = errKVUnavailable
		msg := &loopDeliveryOwnerMsg{data: responseBytes(t, requestID)}
		result, admitted := deliverylane.Consume(
			t.Context(), msg,
			heartbeatPolicyForTest(t, "agent.response", c.handleResponseMessage),
			deliverylane.NewAdmission(nil, nil))
		require.True(t, admitted)
		require.Equal(t, natsclient.DeliveryDecisionQuarantine, result.Decision(),
			"the response lane acknowledged a terminal failure that never reached KV")
		require.True(t, result.OwnerStopRequired())
		require.Zero(t, msg.acks.Load()+msg.naks.Load()+msg.terms.Load())
	})
}
