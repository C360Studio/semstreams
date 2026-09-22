//go:build integration

package agenticloop

import (
	"context"
	"errors"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/processor/agentic-loop/internal/looprequest"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
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

// TestATaskRedeliveredOverAProgressedFirstBatchIsNotRepublished is the other
// half of the task lane's cold fork, and the one `iterations` alone cannot
// answer (owner Codex round on PR #1361, finding 6).
//
// A loop advances its iteration only when a whole tool batch is in, so the
// entire FIRST batch sits at `iterations = 0` while its applied set fills. A
// classification that reads the ordinal alone therefore cannot tell an
// untouched birth from a first batch half applied, and answered both with
// "republish R1" — seating a fresh loop with no batch over a record that
// carries one. The sibling result then had no execution to route to, the
// rebuild was refused over the loop the republish had just seated, and an
// executor's completed work retried to MaxDeliver.
//
// The durable fact that separates them is the applied set, which the delta
// scenario already names. The state below is built by running it: a real
// birth, a real two-call batch, and a real apply of the first result, so the
// record under test is the one production writes rather than a fixture's idea
// of it.
//
// spec: agentic-loop / The loop record names its outstanding request
func TestATaskRedeliveredOverAProgressedFirstBatchIsNotRepublished(t *testing.T) {
	client := newLoopNATS(t)

	const loopID = "9d3f7c21-4a58-4b6e-8f01-2c3d4e5f6a70"
	task := agentic.TaskMessage{
		TaskID: "task-progressed-first-batch",
		LoopID: loopID,
		Role:   "general",
		Model:  "test-model",
		Prompt: "the prompt whose first batch is already half applied",
	}
	firstRequest := looprequest.ID{LoopID: loopID, Iteration: 1, Retry: 0}.String()
	secondRequest := looprequest.ID{LoopID: loopID, Iteration: 2, Retry: 0}.String()
	requestSubject := "agent.request." + loopID

	predecessor, handler := startLoopProcess(t, client, DefaultConfig())
	_, birth := deliverTask(t, predecessor, task)
	require.Equal(t, natsclient.DeliveryDecisionAck, birth.Decision())
	require.Equal(t, uint64(1), messagesOn(t, client, requestSubject))

	// Two calls, so the first result completes nothing: the loop stays at
	// iteration zero with one execution applied, which is exactly the state
	// the ordinal cannot tell from a birth that has done nothing at all.
	batch := agentic.AgentResponse{
		RequestID:    firstRequest,
		Status:       agentic.StatusToolCall,
		FinishReason: "tool_calls",
		Message: agentic.ChatMessage{
			Role: "assistant",
			ToolCalls: []agentic.ToolCall{
				{ID: "call-progressed-a", Name: "progressed_tool"},
				{ID: "call-progressed-b", Name: "progressed_tool"},
			},
		},
	}
	retainModelResponse(t, client, batch)
	dispatch, err := handler.HandleModelResponse(t.Context(), loopID, batch)
	require.NoError(t, err)
	require.NoError(t, predecessor.persistHandlerResult(t.Context(), dispatch, publishThenWrite))
	callA, executeSubject := dispatchedToolCall(t, dispatch)

	resultA := agentic.ToolResult{
		CallID: callA.ID, Name: callA.Name, Content: "the first tool answered", LoopID: loopID,
		RequestID: callA.RequestID, ExecutionID: callA.ExecutionID, CallOrdinal: callA.CallOrdinal,
	}
	_, appliedA := deliverToolResult(t, predecessor, resultA)
	require.Equal(t, natsclient.DeliveryDecisionAck, appliedA.Decision())

	progressed := loopRecordOf(t, predecessor, loopID)
	require.Equal(t, 0, progressed.entity.Iterations,
		"the whole first batch runs at iteration zero; if this ever changes the finding changes with it")
	require.Contains(t, progressed.entity.PendingToolResults, resultA.ExecutionID,
		"the residue under test is a record at iteration zero that already carries an applied result")
	require.Equal(t, firstRequest, progressed.entity.PublishedRequestID)
	require.Equal(t, uint64(2), messagesOn(t, client, executeSubject),
		"the applied result released its sibling, so the batch is half run")

	// The replacement has no memory of the loop, so the original task meets
	// the cold fork rather than HandleTask's warm dedup.
	replacement, replacementHandler := startLoopProcess(t, client, DefaultConfig())

	_, redelivered := deliverTask(t, replacement, task)

	require.Equal(t, natsclient.DeliveryDecisionAck, redelivered.Decision(),
		"a task its loop has already moved past is settled, not re-run")
	_, seated := replacementHandler.loopManager.GetLoop(loopID)
	require.Error(t, seated,
		"the redelivery seated a fresh loop over a record that carries a running batch; the "+
			"sibling result now has no execution to route to and the rebuild is refused over the seat")
	require.Equal(t, uint64(1), messagesOn(t, client, requestSubject),
		"a loop whose batch is running must not have its first request published a second time")

	after := loopRecordOf(t, replacement, loopID)
	require.Equal(t, progressed.revision, after.revision,
		"an acknowledged-without-effect task writes nothing")
	require.Contains(t, after.entity.PendingToolResults, resultA.ExecutionID,
		"the applied set the batch is still running against must survive the redelivery")

	// The sibling is the proof the batch is still runnable: it arrives cold,
	// rebuilds the loop from the record and the two retained messages,
	// completes the batch and mints the loop's next request.
	siblingResult := agentic.ToolResult{
		CallID: "call-progressed-b", Name: "progressed_tool", Content: "the sibling answered",
		LoopID: loopID, RequestID: firstRequest,
		ExecutionID: deriveToolExecutionID(firstRequest, "call-progressed-b", 2), CallOrdinal: 2,
	}
	_, deliveredSibling := deliverToolResult(t, replacement, siblingResult)

	require.Equal(t, natsclient.DeliveryDecisionAck, deliveredSibling.Decision(),
		"the sibling belongs to the batch the record names; a replacement must be able to apply it")
	completed := loopRecordOf(t, replacement, loopID)
	require.Equal(t, 1, completed.entity.Iterations,
		"the completed batch advances the loop it belongs to")
	require.Equal(t, secondRequest, completed.entity.PublishedRequestID)
	require.Equal(t, uint64(2), messagesOn(t, client, requestSubject),
		"the loop's second request is what a recovered batch produces")
}

// refusesFirstCreate is a record store that refuses the first birth write with
// an ordinary failure — not the key-exists conflict, which the lane already
// has its own arm for. Everything else reads and writes the real bucket, so
// after the refusal is spent the redelivery meets a healthy store.
type refusesFirstCreate struct {
	jetstream.KeyValue
	remaining int
}

func (b *refusesFirstCreate) Create(
	ctx context.Context, key string, value []byte, opts ...jetstream.KVCreateOpt,
) (uint64, error) {
	if b.remaining > 0 {
		b.remaining--
		return 0, errors.New("the record store refused the birth write")
	}
	return b.KeyValue.Create(ctx, key, value, opts...)
}

// TestABirthWhoseRecordWriteFailedIsFinishedByItsRetry is the failed-birth arm
// of I1 (owner Codex round on PR #1361, finding 2).
//
// Birth builds the loop in memory, writes its record, then publishes R1. When
// the record write failed with anything other than a conflict the delivery
// went back transient — correctly — but the loop it had just built stayed in
// this process's memory. The redelivery then found a warm loop, so the cold
// classification never ran, HandleTask answered with its task-ID dedup, and
// the lane acknowledged a task that had never issued a request. Nothing was
// retained, nothing was recorded, and nobody was owed it any more: silent task
// loss.
//
// The key-exists arm and the publish-failure arm both release; this one did
// not. The assertion is the pair I1 names, read off a real server: after the
// store is healthy again the retry finishes the birth, or it is not
// acknowledged.
//
// spec: agentic-loop / The loop record names its outstanding request
func TestABirthWhoseRecordWriteFailedIsFinishedByItsRetry(t *testing.T) {
	client := newLoopNATS(t)

	const loopID = "0e5a9b14-3c72-4d68-9a05-7f8e1d2c3b40"
	task := agentic.TaskMessage{
		TaskID: "task-failed-birth-write",
		LoopID: loopID,
		Role:   "general",
		Model:  "test-model",
		Prompt: "the task whose first record write is refused",
	}
	firstRequest := looprequest.ID{LoopID: loopID, Iteration: 1, Retry: 0}.String()
	requestSubject := "agent.request." + loopID

	c, handler := startLoopProcess(t, client, DefaultConfig())
	healthy := c.loopsBucket
	c.loopsBucket = &refusesFirstCreate{KeyValue: healthy, remaining: 1}

	_, refused := deliverTask(t, c, task)

	require.Equal(t, natsclient.DeliveryDecisionRetry, refused.Decision(),
		"a birth whose record never landed is still owed to somebody")
	require.Equal(t, uint64(0), messagesOn(t, client, requestSubject),
		"the record is written before the request is published; a refused write publishes nothing")
	require.Equal(t, loopPresenceStale, c.readLoopRecord(t.Context(), loopID).presence,
		"the refused write must leave no record behind")
	// The mechanism, asserted without stopping the run: the harm below is what
	// the finding is about, and seeing both reds at once is what tells a reader
	// the released loop and the finished birth are the same fact.
	_, warm := handler.loopManager.GetLoop(loopID)
	assert.Error(t, warm,
		"the loop built for a birth that did not happen is still held; the redelivery will meet "+
			"HandleTask's task-id dedup instead of the record, and acknowledge without publishing")

	// The store is healthy again and the SAME process takes the redelivery —
	// which is the case that makes this a loss rather than a retry: a
	// replacement would have met the cold fork regardless.
	c.loopsBucket = healthy

	msg, retried := deliverTask(t, c, task)

	require.Equal(t, natsclient.DeliveryDecisionAck, retried.Decision(),
		"the retry found a healthy store and finished the birth")
	require.Equal(t, int32(1), msg.acks.Load())
	require.Equal(t, uint64(1), messagesOn(t, client, requestSubject),
		"the acknowledged birth must have put its first request on the stream")
	require.Equal(t, firstRequest, retainedRequestIdentity(t, client, requestSubject))

	record := loopRecordOf(t, c, loopID)
	require.Equal(t, loopPresenceLive, record.presence)
	require.Equal(t, firstRequest, record.entity.PublishedRequestID,
		"the record and the stream must name the same request, or I1 does not hold for this loop")
}
