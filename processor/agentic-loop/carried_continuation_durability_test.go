package agenticloop

import (
	"testing"

	"github.com/c360studio/semstreams/natsclient"
	"github.com/stretchr/testify/require"
)

// The ordering that decides the marker's shape. persistHandlerResult stamps the
// loop entity (persistResultState) and only THEN emits the results
// (publishResults), and a publish-phase failure is commit-unknown: the delivery
// quarantines with the request's durability unknown. A marker cleared when the
// carrying request was BUILT is therefore durably clear about a send that may
// never have happened, and the one fact that could re-carry the user's turn —
// that a turn is waiting — is gone from the only record recovery will read.
//
// Recording the carrier instead of clearing the marker keeps both halves: the
// persisted record still says "a turn is deferred, carried by <requestID>", and
// the loop does not carry it a second time because a request already names it.
// The clear moves to SettleRequest, where a response for that request proves
// the send happened.
//
// Asserted on the durable record rather than on the in-memory manager: what
// survives a quarantine is what was written to KV.
//
// spec: agentic-loop / A logical model request has one deterministic identity
func TestQuarantinedCarryLeavesTheDeferredTurnInTheDurableRecord(t *testing.T) {
	c, bucket, loopID, first := loopWithADeferredTurn(t)

	// Constructed, never connected: publishResults fails on the carrying
	// agent.request, after persistResultState has already stamped the entity.
	client, err := natsclient.NewClient("nats://127.0.0.1:1")
	require.NoError(t, err)
	c.natsClient = client

	msg := &loopDeliveryOwnerMsg{data: completionResponseBytes(t, first, "the first thing is done")}
	result, admitted := consumeAdmittedDelivery(
		t.Context(), msg,
		heartbeatPolicyForTest(t, "agent.response", c.handleResponseMessage),
		newDeliveryLaneAdmission(nil))
	require.True(t, admitted)
	require.Equal(t, natsclient.DeliveryDecisionQuarantine, result.Decision(),
		"a publish of unknown durability must quarantine, not retry or terminate")

	entity := persistedLoop(t, bucket, loopID)
	require.True(t, entity.PendingContinuation,
		"the quarantined record forgot that a turn is still waiting; nothing can re-carry it")
	require.Equal(t, loopID+":req:2:0", entity.PendingContinuationRequestID,
		"the record must name the request that was supposed to carry the turn")
	require.False(t, entity.State.IsTerminal(),
		"the loop must not be settled with an unanswered turn")
}
