package agenticloop

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/internal/deliverylane"
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

	_, deferred, err := handler.loopManager.attachContinuation(loopID, "task-superseded-2", "a second turn")
	require.NoError(t, err)
	require.True(t, deferred, "the fixture must leave a turn deferred behind the outstanding request")

	c := releaseTestComponent(t, handler)
	bucket := &recordingLoopBucket{}
	c.loopsBucket = bucket
	seedLoopRecord(t, c, loopID)
	return c, bucket, loopID, requestID
}

// supersededDrops reads the superseded-drop counter. getMetrics is a package
// singleton (metrics.go:77, metricsOnce), so this counter accumulates across
// every test in the binary: the observation each test owns is its DELTA, never
// the absolute value.
func supersededDrops(c *Component) float64 {
	return testutil.ToFloat64(c.handler.metrics.modelResponsesDropped.WithLabelValues("superseded_request"))
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
		result, admitted := deliverylane.Consume(
			t.Context(), msg,
			heartbeatPolicyForTest(t, "agent.response", c.handleResponseMessage),
			deliverylane.NewAdmission(nil, nil))
		require.True(t, admitted)
		require.Equal(t, natsclient.DeliveryDecisionAck, result.Decision())
		return msg
	}

	deliver(t)
	carried := c.handler.loopManager.OutstandingRequest(loopID)
	droppedBefore := supersededDrops(c)
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
	require.Equal(t, droppedBefore+1, supersededDrops(c),
		"the drop is deliberate, so it is counted where an operator can see it")
}

// timedOutLoopWithASupersededRequest builds the state the guard has to survive:
// a loop whose deadline has already passed, waiting on its SECOND request,
// with the first still able to answer. Returns the component, its durable
// bucket, the loop, and the superseded request.
func timedOutLoopWithASupersededRequest(t *testing.T) (*Component, *recordingLoopBucket, string, string) {
	t.Helper()
	handler := NewMessageHandler(DefaultConfig())
	handler.SetMetrics(getMetrics(metric.NewMetricsRegistry()))
	loopID, err := handler.loopManager.CreateLoop("task-superseded-timeout", "general", "model", 5)
	require.NoError(t, err)

	superseded := handler.loopManager.GenerateRequestID(loopID)
	handler.loopManager.TrackRequest(superseded, loopID)
	// SetPublishedRequest beside TrackRequest is what every production mint
	// site does (#1330), and it is the identity the superseded guard compares
	// against. A fixture that tracked without naming would leave the record
	// naming nothing and let the stale response through.
	require.NoError(t, handler.loopManager.SetPublishedRequest(loopID, superseded))
	require.NoError(t, handler.loopManager.IncrementIteration(loopID))
	current := handler.loopManager.GenerateRequestID(loopID)
	handler.loopManager.TrackRequest(current, loopID)
	require.NoError(t, handler.loopManager.SetPublishedRequest(loopID, current))
	require.NotEqual(t, superseded, current, "the fixture must leave the first request superseded")
	require.Equal(t, current, handler.loopManager.OutstandingRequest(loopID))

	// A deadline in the past, so IsTimedOut answers true for every delivery
	// from here on without the test waiting on a clock.
	require.NoError(t, handler.loopManager.SetTimeout(loopID, -time.Second))
	require.True(t, handler.loopManager.IsTimedOut(loopID))

	c := releaseTestComponent(t, handler)
	bucket := &recordingLoopBucket{}
	c.loopsBucket = bucket
	seedLoopRecord(t, c, loopID)
	return c, bucket, loopID, superseded
}

// The guard's POSITION, not just its existence. The timeout arm below it
// (handlers.go:1303-1317) is the one branch that turns reading a response into
// a terminal write — it transitions the loop to failed, builds a failure record
// and publishes failure events — and the terminal guard after it cannot stand
// in, because a timed-out loop is not yet in a terminal state. So if identity
// were checked anywhere after the timeout, a response two moves out of date
// would be what finally failed the loop, and the request the loop IS waiting on
// would have its own answer dropped as terminal.
//
// The loop is timed out either way; this delivery is simply not evidence about
// it. It answers a request nobody is waiting on, while the outstanding request
// is still in flight and may yet answer. Whatever settles a timed-out loop has
// to be a delivery that addresses the loop as it stands.
//
// The natsClient is constructed and never connected, so any publication this
// delivery attempts fails and shows up as a Quarantine decision: the Ack below
// is the assertion that nothing was published.
//
// spec: agentic-loop / A logical model request has one deterministic identity
func TestSupersededResponseDoesNotSettleATimedOutLoop(t *testing.T) {
	c, bucket, loopID, superseded := timedOutLoopWithASupersededRequest(t)

	client, err := natsclient.NewClient("nats://127.0.0.1:1")
	require.NoError(t, err)
	c.natsClient = client

	before, err := c.handler.loopManager.GetLoop(loopID)
	require.NoError(t, err)
	require.False(t, before.State.IsTerminal(), "the fixture must start the loop live")
	droppedBefore := supersededDrops(c)

	msg := &loopDeliveryOwnerMsg{data: completionResponseBytes(t, superseded, "an answer two moves out of date")}
	result, admitted := deliverylane.Consume(
		t.Context(), msg,
		heartbeatPolicyForTest(t, "agent.response", c.handleResponseMessage),
		deliverylane.NewAdmission(nil, nil))
	require.True(t, admitted)
	require.Equal(t, natsclient.DeliveryDecisionAck, result.Decision(),
		"the stale response published something or failed the delivery instead of being dropped")

	require.Equal(t, droppedBefore+1, supersededDrops(c),
		"the drop must be counted as superseded, which is why it happened")

	entity := persistedLoop(t, bucket, loopID)
	require.Equal(t, before.State, entity.State,
		"the stale response moved the state of a loop it does not address")
	require.Equal(t, before.Iterations, entity.Iterations,
		"the stale response advanced the loop")
	require.Empty(t, entity.Outcome, "the stale response settled the timed-out loop")
	require.Empty(t, entity.CompletedAt)
	require.Empty(t, bucket.written(),
		"a superseded response must write nothing at all: an empty HandlerResult still reaches the "+
			"record's compare-and-swap, and a delivery that changed nothing must not move its revision")
	require.Equal(t, loopID+":req:2:0", c.handler.loopManager.OutstandingRequest(loopID),
		"the stale response settled the request the loop is actually waiting on")
}
