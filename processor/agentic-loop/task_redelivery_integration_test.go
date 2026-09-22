//go:build integration

package agenticloop

import (
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/processor/agentic-loop/internal/looprequest"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
)

// retainedRequestIdentity reports the Nats-Msg-Id the newest retained request
// on a subject was published under. The identity, not the body: a second copy
// of R1 under the same name is what the duplicate window and identity adoption
// both exist to prevent, and it is only observable on a real server.
func retainedRequestIdentity(t *testing.T, client *natsclient.Client, subject string) string {
	t.Helper()
	stream, err := client.GetStream(t.Context(), loopStreamName)
	require.NoError(t, err)
	raw, err := stream.GetLastMsgForSubject(t.Context(), subject)
	require.NoError(t, err)
	return raw.Header.Get(jetstream.MsgIDHeader)
}

// TestTaskRedeliveredToAReplacementLeavesOneFirstRequest is the task lane's
// cold fork over a real broker (#1330 task 3.4, owner ruling Q1).
//
// The unit arm of that fork asserts what the delivery does NOT do — it does
// not acknowledge and does not write the record — and both of those are also
// true of the failure it replaced: a replacement that tries to birth the loop
// again is refused by the record's Create, which neither acknowledges nor
// writes. Only a real server can tell the two apart, because only there does
// the republish have somewhere to land: the fork ACKNOWLEDGES, and
// agent.request.<loopID> still holds exactly one message, under the identity
// the record already names.
//
// Assertions read the record's fields, the per-subject message count and the
// retained message's Nats-Msg-Id. No message body is compared.
//
// spec: agentic-loop / The loop record names its outstanding request
func TestTaskRedeliveredToAReplacementLeavesOneFirstRequest(t *testing.T) {
	client := newLoopNATS(t)

	const loopID = "2f6c1d40-7a3b-4e58-9c1f-8b0d2e3a4c57"
	task := agentic.TaskMessage{
		TaskID: "task-cold-republish",
		LoopID: loopID,
		Role:   "general",
		Model:  "test-model",
		Prompt: "the prompt the replacement rebuilds R1 from",
	}
	firstRequest := looprequest.ID{LoopID: loopID, Iteration: 1, Retry: 0}.String()
	requestSubject := "agent.request." + loopID

	// A real birth through the real task lane: record by Create, then publish.
	predecessor, _ := startLoopProcess(t, client, DefaultConfig())
	born, birthDelivery := deliverTask(t, predecessor, task)
	require.Equal(t, natsclient.DeliveryDecisionAck, birthDelivery.Decision())
	require.Equal(t, int32(1), born.acks.Load())
	require.Equal(t, uint64(1), messagesOn(t, client, requestSubject))
	require.Equal(t, firstRequest, retainedRequestIdentity(t, client, requestSubject))

	birthRecord := loopRecordOf(t, predecessor, loopID)
	require.Equal(t, firstRequest, birthRecord.entity.PublishedRequestID)
	require.Equal(t, 0, birthRecord.entity.Iterations,
		"the fork under test is the one that runs while the record is still at iteration zero")

	// The replacement has no memory of the loop, so the same task bytes meet
	// the cold fork rather than HandleTask's warm dedup.
	replacement, handler := startLoopProcess(t, client, DefaultConfig())

	redelivered, delivery := deliverTask(t, replacement, task)

	require.Equal(t, natsclient.DeliveryDecisionAck, delivery.Decision(),
		"a replacement that births the loop again is refused by the record's Create and retries to "+
			"MaxDeliver; the fork republishes R1 and acknowledges")
	require.Equal(t, int32(1), redelivered.acks.Load())

	require.Equal(t, uint64(1), messagesOn(t, client, requestSubject),
		"the redelivery put a second copy of the loop's first request on the stream")
	require.Equal(t, firstRequest, retainedRequestIdentity(t, client, requestSubject),
		"the one retained request must still be the one the record names")

	after := loopRecordOf(t, replacement, loopID)
	require.Equal(t, birthRecord.revision, after.revision,
		"the record at iteration zero is already the truth; the fork must not write it again")
	require.Equal(t, firstRequest, after.entity.PublishedRequestID)

	entity, err := handler.loopManager.GetLoop(loopID)
	require.NoError(t, err, "the replacement must HOLD the loop it recovered")
	require.Equal(t, firstRequest, entity.PublishedRequestID,
		"the rebuilt loop mints the same first request its record already names")
	revision, held := replacement.observedLoopRevision(loopID)
	require.True(t, held, "the holder took no revision, so its next compare-and-swap cannot run")
	require.Equal(t, birthRecord.revision, revision)
}
