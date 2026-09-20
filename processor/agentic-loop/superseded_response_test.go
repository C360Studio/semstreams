package agenticloop

import (
	"encoding/json"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/metric"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

// completionResponseBytes encodes one StatusComplete response on the wire.
func completionResponseBytes(t *testing.T, requestID, content string) []byte {
	t.Helper()
	response := &agentic.AgentResponse{
		RequestID: requestID,
		Status:    agentic.StatusComplete,
		Message:   agentic.ChatMessage{Role: "assistant", Content: content},
	}
	data, err := json.Marshal(message.NewBaseMessage(response.Schema(), response, "test"))
	require.NoError(t, err)
	return data
}

// loopWithADeferredTurn births a loop, leaves its first request outstanding,
// and admits a second turn against it — the "someone typed while the agent was
// thinking" state. Returns the component, its durable bucket, the loop and the
// outstanding request.
func loopWithADeferredTurn(t *testing.T) (*Component, *recordingLoopBucket, string, string) {
	t.Helper()
	handler := NewMessageHandler(DefaultConfig())
	handler.SetMetrics(getMetrics(metric.NewMetricsRegistry()))
	loopID, err := handler.loopManager.CreateLoop("task-superseded-1", "general", "model", 5)
	require.NoError(t, err)
	requestID := handler.loopManager.GenerateRequestID(loopID)
	handler.loopManager.TrackRequest(requestID, loopID)

	_, deferred, err := handler.loopManager.attachContinuation(loopID, "task-superseded-2")
	require.NoError(t, err)
	require.True(t, deferred, "the fixture must leave a turn deferred behind the outstanding request")

	c := releaseTestComponent(t, handler)
	bucket := &recordingLoopBucket{}
	c.loopsBucket = bucket
	return c, bucket, loopID, requestID
}

// persistedLoop decodes the entity the delivery wrote to KV.
func persistedLoop(t *testing.T, bucket *recordingLoopBucket, loopID string) agentic.LoopEntity {
	t.Helper()
	raw, ok := bucket.value(loopID)
	require.True(t, ok, "the delivery persisted no loop record")
	var entity agentic.LoopEntity
	require.NoError(t, json.Unmarshal(raw, &entity))
	return entity
}

// A carried completion leaves the loop NON-TERMINAL, which is exactly what
// takes its redelivery out of reach of the terminal guard. The guard that used
// to absorb a redelivered completion — "ignoring model response for terminal
// loop" — is the premise persistHandlerResult's own classification rationale is
// written on (component.go:1889-1892), and carrying a deferred turn falsifies
// it: the loop advanced to the next iteration instead of completing, so the
// redelivery meets a live loop with nothing left to carry and would settle it
// while the request holding the user's turn is still in flight. That request's
// answer would then be dropped as terminal, losing the turn the deferral exists
// to save.
//
// Request identity is what tells the two apart, and the assertion is on the
// loop's state and its durable record rather than on the guard being called:
// the obligation is "the redelivery changes nothing", not "some branch ran".
//
// spec: agentic-loop / A logical model request has one deterministic identity
func TestRedeliveredCarriedCompletionDoesNotCompleteTheLoop(t *testing.T) {
	c, bucket, loopID, first := loopWithADeferredTurn(t)
	wire := completionResponseBytes(t, first, "the first thing is done")

	deliver := func(t *testing.T) *loopDeliveryOwnerMsg {
		t.Helper()
		msg := &loopDeliveryOwnerMsg{data: wire}
		result, admitted := consumeAdmittedDelivery(
			t.Context(), msg,
			heartbeatPolicyForTest(t, "agent.response", c.handleResponseMessage),
			newDeliveryLaneAdmission(nil))
		require.True(t, admitted)
		require.Equal(t, natsclient.DeliveryDecisionAck, result.Decision())
		return msg
	}

	deliver(t)
	carried := c.handler.loopManager.OutstandingRequest(loopID)
	require.Equal(t, loopID+":req:2:0", carried,
		"the completion must have carried the deferred turn into the next iteration")
	require.False(t, persistedLoop(t, bucket, loopID).State.IsTerminal())

	// The same bytes again, which is what AckWait plus a slow handler produces.
	deliver(t)

	entity := persistedLoop(t, bucket, loopID)
	require.False(t, entity.State.IsTerminal(),
		"the redelivery completed a loop whose carried request is still in flight")
	require.Empty(t, entity.Outcome, "the redelivery built a completion record")
	require.Empty(t, entity.CompletedAt)
	require.Equal(t, carried, c.handler.loopManager.OutstandingRequest(loopID),
		"the redelivery must leave the carried request outstanding")
	require.Equal(t, 1.0, testutil.ToFloat64(
		c.handler.metrics.modelResponsesDropped.WithLabelValues("superseded_request")),
		"the drop is deliberate, so it is counted where an operator can see it")
}
