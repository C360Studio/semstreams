package agenticloop

import (
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/internal/deliverylane"
	"github.com/c360studio/semstreams/metric"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/processor/agentic-loop/internal/looprequest"
	"github.com/stretchr/testify/require"
)

// replacementProcessHoldingALoop builds what a process looks like after it has
// recovered a loop it did not start: the loop is in memory with the request
// its RECORD names, and this process has minted nothing for it.
//
// That second half is the whole point. The superseded-response guard used to
// compare against a process-local map of mints, which is empty in exactly this
// state — so every response reaching a replacement passed the guard whatever
// it named, including one the loop had already moved past (#1328's declared
// residual). The durable name is what this process now compares against.
func replacementProcessHoldingALoop(t *testing.T, published string, loopID string) (*Component, *MessageHandler) {
	t.Helper()
	h := NewMessageHandler(DefaultConfig())
	c := releaseTestComponent(t, h)
	c.metrics = getMetrics(metric.NewMetricsRegistry())
	h.SetMetrics(c.metrics)
	c.loopsBucket = &recordingLoopBucket{}
	c.natsClient = unpublishableClient(t)
	c.requestEvidence = stubEvidenceReader{}

	_, err := h.loopManager.CreateLoopWithID(loopID, "task-replacement", "general", "model", 5)
	require.NoError(t, err)
	require.NoError(t, h.loopManager.IncrementIteration(loopID))
	require.NoError(t, h.loopManager.SetPublishedRequest(loopID, published))
	require.Empty(t, h.OutstandingRequestForTest(loopID),
		"a replacement has minted nothing for this loop; that is the state under test")
	seedLoopRecord(t, c, loopID)
	return c, h
}

func deliverResponse(t *testing.T, c *Component, response agentic.AgentResponse) (*loopDeliveryOwnerMsg, natsclient.DeliveryResult) {
	t.Helper()
	msg := &loopDeliveryOwnerMsg{data: baseMessageBytes(t, &response)}
	delivered, admitted := deliverylane.Consume(t.Context(), msg,
		heartbeatPolicyForTest(t, "agent.response", c.handleResponseMessage), deliverylane.NewAdmission(nil, nil))
	require.True(t, admitted)
	return msg, delivered
}

// TestAReplacementClassifiesAResponseAgainstTheRecordNotItsOwnMints is the
// response lane's half of #1330's classification (design § 5.2).
//
// spec: agentic-loop / The loop record names its outstanding request
func TestAReplacementClassifiesAResponseAgainstTheRecordNotItsOwnMints(t *testing.T) {
	const loopID = "8c2a4d1e-6f30-4b52-9d87-0a1b2c3d4e5f"
	published := looprequest.ID{LoopID: loopID, Iteration: 2, Retry: 0}.String()

	t.Run("a completion for a superseded request does not settle the loop", func(t *testing.T) {
		c, h := replacementProcessHoldingALoop(t, published, loopID)
		before, err := h.loopManager.GetLoop(loopID)
		require.NoError(t, err)
		dropped := supersededDrops(c)

		msg, delivered := deliverResponse(t, c, agentic.AgentResponse{
			RequestID: looprequest.ID{LoopID: loopID, Iteration: 1, Retry: 0}.String(),
			Status:    agentic.StatusComplete,
			Message:   agentic.ChatMessage{Role: "assistant", Content: "an answer a move out of date"},
		})

		require.Equal(t, natsclient.DeliveryDecisionAck, delivered.Decision(),
			"the superseded completion published something instead of being dropped")
		require.Equal(t, int32(1), msg.acks.Load())
		require.Equal(t, dropped+1, supersededDrops(c),
			"the drop is deliberate, so it is counted where an operator can see it")

		after, err := h.loopManager.GetLoop(loopID)
		require.NoError(t, err)
		require.Equal(t, before.State, after.State,
			"a response the loop moved past settled the loop it no longer belongs to")
	})

	t.Run("a response the record does not yet name is retried, not failed", func(t *testing.T) {
		c, h := replacementProcessHoldingALoop(t, published, loopID)
		before, err := h.loopManager.GetLoop(loopID)
		require.NoError(t, err)

		msg, delivered := deliverResponse(t, c, agentic.AgentResponse{
			RequestID: looprequest.ID{LoopID: loopID, Iteration: 3, Retry: 0}.String(),
			Status:    agentic.StatusComplete,
			Message:   agentic.ChatMessage{Role: "assistant", Content: "an answer that outran its record"},
		})

		require.Equal(t, natsclient.DeliveryDecisionRetry, delivered.Decision(),
			"an answer whose question the record has not recorded yet is not yet observable")
		require.Equal(t, int32(1), msg.naks.Load())
		require.Zero(t, msg.acks.Load()+msg.terms.Load())

		after, err := h.loopManager.GetLoop(loopID)
		require.NoError(t, err)
		require.Equal(t, before.State, after.State,
			"a not-yet-observable response must not be read as this loop's failure")
		require.False(t, after.State.IsTerminal())
		require.Empty(t, c.loopsBucket.(*recordingLoopBucket).written(),
			"a retried delivery wrote the loop record")
	})

	t.Run("a response for the request the record names is handled", func(t *testing.T) {
		c, h := replacementProcessHoldingALoop(t, published, loopID)

		_, delivered := deliverResponse(t, c, agentic.AgentResponse{
			RequestID: published,
			Status:    agentic.StatusComplete,
			Message:   agentic.ChatMessage{Role: "assistant", Content: "the answer this loop is waiting for"},
		})

		// A completion publishes the loop's terminal events, and this
		// component cannot publish — so the delivery failing at the PUBLISH is
		// the proof the classification let it through to the handler.
		require.Equal(t, natsclient.DeliveryDecisionQuarantine, delivered.Decision(),
			"the classification refused the answer the loop was waiting for")
		entity, err := h.loopManager.GetLoop(loopID)
		require.NoError(t, err)
		require.True(t, entity.State.IsTerminal(),
			"the completion never reached the handler")
	})
}

// TestColdResponseIsClassifiedAgainstTheAdoptedRecord: the cold arm adopts the
// newest retained request and then orders the response against it. A response
// older than the adopted request can be applied by no process at all, so it is
// acknowledged rather than retried to MaxDeliver against every replacement in
// turn.
//
// spec: agentic-loop / The loop record names its outstanding request
func TestColdResponseIsClassifiedAgainstTheAdoptedRecord(t *testing.T) {
	const loopID = "d2f9c304-4e6f-4c7d-a081-3f4e5d6c7082"
	retained := looprequest.ID{LoopID: loopID, Iteration: 3, Retry: 0}.String()

	c := evidenceComponent(t, retained)
	c.metrics = getMetrics(metric.NewMetricsRegistry())
	c.handler.SetMetrics(c.metrics)
	coldRecord(t, c, loopID, func(e *agentic.LoopEntity) {
		e.PublishedRequestID = looprequest.ID{LoopID: loopID, Iteration: 2, Retry: 0}.String()
		e.Iterations = 1
	})
	dropped := supersededDrops(c)

	msg, delivered := deliverResponse(t, c, agentic.AgentResponse{
		RequestID: looprequest.ID{LoopID: loopID, Iteration: 2, Retry: 0}.String(),
		Status:    agentic.StatusComplete,
		Message:   agentic.ChatMessage{Role: "assistant", Content: "an answer the loop moved past"},
	})

	require.Equal(t, natsclient.DeliveryDecisionAck, delivered.Decision())
	require.Equal(t, int32(1), msg.acks.Load())
	require.Equal(t, dropped+1, supersededDrops(c))
	require.Equal(t, retained, decodeRecord(t, c, loopID).PublishedRequestID,
		"the classification ran without adopting first")
}
