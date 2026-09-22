package agenticloop

import (
	"testing"

	"github.com/c360studio/semstreams/internal/deliverylane"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/stretchr/testify/require"
)

// The ordering that decides what a failed publish leaves behind.
//
// Before #1330 the carrier stamped the loop entity and only THEN emitted the
// results, so a publish-phase failure left a record that had already recorded
// the carrying request — and the marker had to name that request, because a
// marker cleared when the request was BUILT would be durably clear about a send
// that may never have happened.
//
// On the model-response and tool-result lanes the order is now publish, then
// compare-and-swap. A publish of unknown durability therefore writes NOTHING:
// the durable record still says "a turn is deferred and uncarried", which is
// the state that can be recovered from, and no record claims a carrier for a
// request whose PubAck never came back. The half that genuinely landed — a
// request that DID PubAck before the process died — is recovered by identity
// instead: the redelivery re-mints the same request name, finds it retained,
// adopts it rather than publishing a second copy, and writes the record then.
//
// Asserted on the durable record rather than on the in-memory manager: what
// survives a quarantine is what was written to KV.
//
// spec: agentic-loop / A logical model request has one deterministic identity
func TestQuarantinedCarryWritesNoRecordAndLeavesTheTurnUncarried(t *testing.T) {
	c, bucket, loopID, first := loopWithADeferredTurn(t)

	// Constructed, never connected: publishResults fails on the carrying
	// agent.request, before the record write it now precedes.
	client, err := natsclient.NewClient("nats://127.0.0.1:1")
	require.NoError(t, err)
	c.natsClient = client

	msg := &loopDeliveryOwnerMsg{data: completionResponseBytes(t, first, "the first thing is done")}
	result, admitted := deliverylane.Consume(
		t.Context(), msg,
		heartbeatPolicyForTest(t, "agent.response", c.handleResponseMessage),
		deliverylane.NewAdmission(nil, nil))
	require.True(t, admitted)
	require.Equal(t, natsclient.DeliveryDecisionQuarantine, result.Decision(),
		"a publish of unknown durability must quarantine, not retry or terminate")

	require.Empty(t, bucket.written(),
		"the publish ran first, so a publish that did not commit must have written no record at all")

	entity := persistedLoop(t, bucket, loopID)
	require.True(t, entity.PendingContinuation,
		"the durable record forgot that a turn is still waiting; nothing can re-carry it")
	require.Empty(t, entity.PendingContinuationRequestID,
		"no record may name a carrier for a request whose PubAck never came back")
	require.False(t, entity.State.IsTerminal(),
		"the loop must not be settled with an unanswered turn")
}
