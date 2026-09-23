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
	"github.com/prometheus/client_golang/prometheus/testutil"
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

// TestATaskRedeliveredAfterItsFirstIterationRetriedIsNotRepublished is the
// third arm of the task lane's cold fork, and the one the applied set cannot
// answer either (owner Codex round 2 on PR #1361, finding 3).
//
// A length-truncated first response self-heals by re-asking the SAME iteration
// under the next retry ordinal: it publishes `:req:1:1` and deliberately does
// not advance `iterations`, and no tool ran, so the applied set is still
// empty. The record left behind — iteration zero, empty set — is byte-for-byte
// the shape the previous two arms call an untouched birth, and the fork
// republished over it: `HandleTask` on a fresh entity mints `:req:1:0`, the
// publish path finds the NEWER `:req:1:1` retained, and the cold adopt refuses
// the backward name as Fatal. A routine at-least-once redelivery quarantined
// the task lane, and a quarantine latches it for every task behind it.
//
// The durable fact that separates the two is the one the delta's GIVEN already
// names and the code did not read: `published_request_id = R1`. A record
// naming anything else — a within-iteration retry, or a later iteration — is a
// loop that has moved past its task, and the request it does name is answered
// on the lane that owns it. The last arm below is that proof: the retained
// retry's own response reaches the replacement, rebuilds the loop from the
// record and the retained request, and settles it.
//
// The residue is built by running it — a real birth, a real length-truncated
// response through the real carrier — so the record under test is the one
// production writes.
//
// spec: agentic-loop / The loop record names its outstanding request
func TestATaskRedeliveredAfterItsFirstIterationRetriedIsNotRepublished(t *testing.T) {
	client := newLoopNATS(t)

	const loopID = "c7b41e93-2d86-4f05-9a3c-1b2e4d6f8a70"
	task := agentic.TaskMessage{
		TaskID: "task-first-iteration-retried",
		LoopID: loopID,
		Role:   "general",
		Model:  "test-model",
		Prompt: "the prompt whose first answer came back truncated",
	}
	firstRequest := looprequest.ID{LoopID: loopID, Iteration: 1, Retry: 0}.String()
	retryRequest := looprequest.ID{LoopID: loopID, Iteration: 1, Retry: 1}.String()
	requestSubject := "agent.request." + loopID

	predecessor, handler := startLoopProcess(t, client, compactingConfig())
	_, birth := deliverTask(t, predecessor, task)
	require.Equal(t, natsclient.DeliveryDecisionAck, birth.Decision())
	require.Equal(t, uint64(1), messagesOn(t, client, requestSubject))
	fillContextAboveCompactThreshold(t, handler, loopID)

	truncated := agentic.AgentResponse{
		RequestID:    firstRequest,
		Status:       agentic.StatusLengthTruncated,
		FinishReason: agentic.FinishReasonLength,
		Message:      agentic.ChatMessage{Role: "assistant", Content: "partial output"},
		TokenUsage:   agentic.TokenUsage{PromptTokens: 50, CompletionTokens: 4096},
	}
	_, selfHealed := deliverResponse(t, predecessor, truncated)
	require.Equal(t, natsclient.DeliveryDecisionAck, selfHealed.Decision(),
		"the self-heal is an ordinary settled delivery; the residue under test is what it left durable")

	retried := loopRecordOf(t, predecessor, loopID)
	require.Equal(t, retryRequest, retried.entity.PublishedRequestID,
		"the self-heal re-asks the same iteration under the next retry ordinal")
	require.Equal(t, 0, retried.entity.Iterations,
		"a within-iteration retry does not advance the loop; if this ever changes the finding changes with it")
	require.Empty(t, retried.entity.PendingToolResults,
		"no tool ran, so the applied set cannot separate this record from an untouched birth")
	require.Equal(t, uint64(2), messagesOn(t, client, requestSubject),
		"the first request and its retry are both retained")

	// The replacement has no memory of the loop, so the ORIGINAL task meets the
	// cold fork rather than HandleTask's warm dedup.
	replacement, replacementHandler := startLoopProcess(t, client, compactingConfig())

	_, redelivered := deliverTask(t, replacement, task)

	require.Equal(t, natsclient.DeliveryDecisionAck, redelivered.Decision(),
		"republishing over a retried first iteration mints :req:1:0 under a retained :req:1:1, which the "+
			"cold adopt refuses as Fatal — quarantining the task lane over a valid redelivery")
	_, seated := replacementHandler.loopManager.GetLoop(loopID)
	require.Error(t, seated,
		"a loop whose first iteration already retried must not have a fresh one seated over it")
	require.Equal(t, uint64(2), messagesOn(t, client, requestSubject),
		"an acknowledged-without-effect task publishes nothing")

	after := loopRecordOf(t, replacement, loopID)
	require.Equal(t, retried.revision, after.revision,
		"an acknowledged-without-effect task writes nothing")
	require.Equal(t, retryRequest, after.entity.PublishedRequestID,
		"the record must still name the request the stream retains")

	// The proof the loop is not stranded: the request the record DOES name is
	// outstanding on the response lane, and its answer rebuilds the loop from
	// the record and the retained retry, then settles it.
	answer := agentic.AgentResponse{
		RequestID:    retryRequest,
		Status:       agentic.StatusComplete,
		FinishReason: "stop",
		Message:      agentic.ChatMessage{Role: "assistant", Content: "the retry answered in full"},
	}
	retainModelResponse(t, client, answer)
	_, answered := deliverResponse(t, replacement, answer)

	require.Equal(t, natsclient.DeliveryDecisionAck, answered.Decision(),
		"the retained retry is answered on the lane that owns it, cold")
	require.Equal(t, uint64(1), messagesOn(t, client, "agent.complete."+loopID),
		"the loop the task lane declined to rebuild is settled by its own outstanding request")
	settled := loopRecordOf(t, replacement, loopID)
	require.Equal(t, agentic.LoopStateComplete, settled.entity.State)
}

// TestAColdR1ReconstructionKeepsTheRecordsDeadline is the task lane's arm of
// "a rebuild is not a reprieve" (owner Codex round 2 on PR #1361, finding 4).
//
// The cold response and tool arms rebuild through restoreLoopFromRequest,
// which seats the record wholesale — its deadline included. The task lane's R1
// arm does not: it runs the ORDINARY HandleTask, whose configureLoopMetadata
// calls SetTimeout, and SetTimeout stamps `StartedAt = now` and
// `TimeoutAt = now + budget` on the fresh entity. Only the durable revision
// was restored afterwards, so an expired record whose task happened to
// redeliver first resumed on a full fresh budget — a loop outliving the budget
// its caller set, which the owner ruled out explicitly
// (https://github.com/C360Studio/semstreams/issues/1330#issuecomment-5781101792:
// "no refresh on rebuild ... the loop's deadline means what its record says").
//
// Both halves are asserted: the rebuilt loop's own timing fields, and the
// OUTCOME the deadline decides — the R1 response settles the loop on the
// timeout rather than running it. The second is what an operator would see;
// the first is why.
//
// The gap is expressed the way production expresses it — wall clock against
// the record's own TimeoutAt — rather than by rewriting the record.
//
// spec: agentic-loop / The loop record names its outstanding request
func TestAColdR1ReconstructionKeepsTheRecordsDeadline(t *testing.T) {
	client := newLoopNATS(t)

	config := DefaultConfig()
	config.Timeout = shortLoopDeadline.String()

	const loopID = "4f0a2c68-9b17-4e53-8d24-6a5c3b1e7f90"
	task := agentic.TaskMessage{
		TaskID: "task-cold-r1-deadline",
		LoopID: loopID,
		Role:   "general",
		Model:  "test-model",
		Prompt: "the task whose loop ran out of time while nobody held it",
	}
	firstRequest := looprequest.ID{LoopID: loopID, Iteration: 1, Retry: 0}.String()
	requestSubject := "agent.request." + loopID

	predecessor, _ := startLoopProcess(t, client, config)
	_, birth := deliverTask(t, predecessor, task)
	require.Equal(t, natsclient.DeliveryDecisionAck, birth.Decision())
	require.Equal(t, uint64(1), messagesOn(t, client, requestSubject))

	born := loopRecordOf(t, predecessor, loopID)
	require.Equal(t, firstRequest, born.entity.PublishedRequestID,
		"the arm under test is the R1 one: the record still names the loop's first request")
	require.False(t, born.entity.TimeoutAt.IsZero(),
		"the deadline this arm is about must be ON the record, or the rebuild inherits nothing")
	waitPastLoopDeadline(t, born.entity.TimeoutAt)

	// The replacement has no memory of the loop, so the task meets the cold
	// fork and R1 is rebuilt from it.
	replacement, replacementHandler := startLoopProcess(t, client, config)

	_, redelivered := deliverTask(t, replacement, task)

	require.Equal(t, natsclient.DeliveryDecisionAck, redelivered.Decision(),
		"the R1 arm adopts the retained request and acknowledges")
	require.Equal(t, uint64(1), messagesOn(t, client, requestSubject),
		"R1 is already retained, so the rebuild adopts it rather than publishing a second copy")

	// assert, not require, from here down: the deadline and the outcome it
	// decides are one fact seen twice, and a run that reports only the first
	// leaves a reader guessing what a fresh budget actually costs.
	rebuilt, err := replacementHandler.loopManager.GetLoop(loopID)
	require.NoError(t, err, "the replacement must HOLD the loop it rebuilt")
	assert.Equal(t, born.entity.TimeoutAt.UTC(), rebuilt.TimeoutAt.UTC(),
		"a rebuild is not a reprieve: the rebuilt loop carries the record's deadline, not a fresh budget")
	assert.Equal(t, born.entity.StartedAt.UTC(), rebuilt.StartedAt.UTC(),
		"StartedAt is half of the same fact and is stamped by the same call")

	// The outcome the deadline decides. The answer to R1 arrives; a loop
	// carrying its record's expired deadline settles on the timeout, and a loop
	// handed a fresh one would have run.
	answer := agentic.AgentResponse{
		RequestID:    firstRequest,
		Status:       agentic.StatusComplete,
		FinishReason: "stop",
		Message:      agentic.ChatMessage{Role: "assistant", Content: "the answer nobody was still waiting for"},
	}
	retainModelResponse(t, client, answer)
	_, answered := deliverResponse(t, replacement, answer)
	require.Equal(t, natsclient.DeliveryDecisionAck, answered.Decision(),
		"a settled failure owes nobody a redelivery")

	failureSubject := "agent.failed." + loopID
	if assert.Equal(t, uint64(1), messagesOn(t, client, failureSubject),
		"the rebuilt loop was already past its deadline, so its answer settles it on the timeout") {
		// Contains, not Equal: the response lane settles a handler failure
		// through handleLoopFailure, which publishes the WRAPPED error
		// ("agentic-loop.HandleModelResponse: check timeout failed: …"). The
		// claim here is which deadline the loop failed against, not how the
		// lane spells its wrapper.
		assert.Contains(t, failureReasonOn(t, client, failureSubject), "loop timeout exceeded")
		// The prompt is the OTHER half of what this arm proves about the R1
		// rebuild. A loop rebuilt from its record and a retained request
		// publishes its terminal event with an empty Prompt, because the
		// record carries no prompt to restore; this arm is not that rebuild.
		// It runs the ordinary HandleTask, which caches the redelivered task's
		// prompt, so the empty-prompt limitation an adopter is told about
		// stops at the wholesale seat and this event carries the real one.
		assert.Equal(t, task.Prompt, failureEventOn(t, client, failureSubject).Prompt,
			"the cold R1 arm rebuilds through HandleTask, so its terminal event keeps the task's prompt")
	}
	assert.Equal(t, uint64(0), messagesOn(t, client, "agent.complete."+loopID),
		"a loop handed a fresh budget would have completed instead")

	expired := loopRecordOf(t, replacement, loopID)
	assert.Equal(t, agentic.LoopStateFailed, expired.entity.State)
	assert.Equal(t, born.entity.TimeoutAt.UTC(), expired.entity.TimeoutAt.UTC(),
		"the deadline the loop failed against is the one its record carried all along")
}

// TestAColdContinuationForALoopNoProcessHoldsIsRefused is the fourth arm of
// the task lane's cold fork, and the only one that is not a redelivery at all
// (owner Codex round 2 on PR #1361, finding 1; owner ruling
// https://github.com/C360Studio/semstreams/issues/1330#issuecomment-5791390564,
// REFUSE).
//
// Every other arm answers the same task twice. This one answers a NEW task: a
// second turn, admitted by agentic-dispatch against a live record, that names
// a loop no process holds. The classifier read the record alone, so the record
// answered for a task it was never about — advanced, it acknowledged the turn
// as "already applied" and the user's text was never sent to anyone; at
// iteration zero with an empty set it took the republish arm, and `HandleTask`
// on a cold process happily created loop L from the ARRIVING task, seating the
// loop's whole conversation from the new turn's prompt and overwriting the
// record's `task_id` on the next write.
//
// The durable fact that separates them is the record's own `task_id`, which
// birth writes and a warm continuation moves. When it is not the arriving
// task's, this process cannot apply the turn — the loop's context lived in the
// process that is gone — so the turn is refused: acknowledged without effect,
// with a warning naming both tasks and a reason value on
// `task_intake_rejections_total`. That is the same settlement the WARM refusal
// takes when a continuation meets a busy loop, and on a lane that runs at
// `MaxAckPending` 1 it is the only one that does not park every task behind
// it. The turn has to be re-sent once a redelivered input has rebuilt the
// loop.
//
// Both arms are built by running them, so the record under test is the one
// production writes.
//
// spec: agentic-loop / The loop record names its outstanding request
func TestAColdContinuationForALoopNoProcessHoldsIsRefused(t *testing.T) {
	client := newLoopNATS(t)

	// The counter is a process-wide singleton, so every assertion below is a
	// DELTA taken across the delivery under test.
	refusals := func(c *Component) float64 {
		return testutil.ToFloat64(c.metrics.taskIntakeRejections.WithLabelValues(
			taskIntakeColdForkLane, taskIntakeContinuationUnheldReason))
	}

	t.Run("the record is still the untouched birth", func(t *testing.T) {
		const loopID = "8b1e4a07-5c92-4d3f-a610-7e2b9c4d5f81"
		born := agentic.TaskMessage{
			TaskID: "task-continuation-birth",
			LoopID: loopID,
			Role:   "general",
			Model:  "test-model",
			Prompt: "the turn the loop was born from",
		}
		continuation := agentic.TaskMessage{
			TaskID: "task-continuation-second-turn",
			LoopID: loopID,
			Role:   "general",
			Model:  "test-model",
			Prompt: "a second turn, typed while no process held the loop",
		}
		firstRequest := looprequest.ID{LoopID: loopID, Iteration: 1, Retry: 0}.String()
		requestSubject := "agent.request." + loopID

		predecessor, _ := startLoopProcess(t, client, DefaultConfig())
		_, birth := deliverTask(t, predecessor, born)
		require.Equal(t, natsclient.DeliveryDecisionAck, birth.Decision())
		require.Equal(t, uint64(1), messagesOn(t, client, requestSubject))

		record := loopRecordOf(t, predecessor, loopID)
		require.Equal(t, firstRequest, record.entity.PublishedRequestID)
		require.Equal(t, 0, record.entity.Iterations)
		require.Empty(t, record.entity.PendingToolResults,
			"this arm is the one every other field calls an untouched birth")
		require.Equal(t, born.TaskID, record.entity.TaskID,
			"the record carries the task that owns the loop, which is the fact under test")

		replacement, replacementHandler := startLoopProcess(t, client, DefaultConfig())
		before := refusals(replacement)

		_, refused := deliverTask(t, replacement, continuation)

		require.Equal(t, natsclient.DeliveryDecisionAck, refused.Decision(),
			"a refusal this redelivery cannot fix settles; Retry would park the MaxAckPending-1 "+
				"task lane behind it and Quarantine would latch the lane over a valid turn")
		_, seated := replacementHandler.loopManager.GetLoop(loopID)
		require.Error(t, seated,
			"the republish arm created loop L from the ARRIVING task, so the loop's conversation "+
				"was seated from the new turn's prompt and its own first turn was gone")
		require.Equal(t, uint64(1), messagesOn(t, client, requestSubject),
			"a refused turn publishes nothing: the request the record names is already retained")

		after := loopRecordOf(t, replacement, loopID)
		require.Equal(t, record.revision, after.revision,
			"an acknowledged-without-effect turn writes nothing")
		require.Equal(t, born.TaskID, after.entity.TaskID,
			"the record must still belong to the task that created the loop")

		require.Equal(t, before+1, refusals(replacement),
			"a refused turn is a declared event: it carries the reason value an operator greps for")
	})

	t.Run("the record has advanced past its first batch", func(t *testing.T) {
		const loopID = "3c7d2f18-6a04-4b95-8e13-5d9f0a2b6c74"
		born := agentic.TaskMessage{
			TaskID: "task-continuation-advanced",
			LoopID: loopID,
			Role:   "general",
			Model:  "test-model",
			Prompt: "the turn whose batch already ran",
		}
		continuation := agentic.TaskMessage{
			TaskID: "task-continuation-advanced-second-turn",
			LoopID: loopID,
			Role:   "general",
			Model:  "test-model",
			Prompt: "a second turn for a loop that has moved on",
		}
		firstRequest := looprequest.ID{LoopID: loopID, Iteration: 1, Retry: 0}.String()
		secondRequest := looprequest.ID{LoopID: loopID, Iteration: 2, Retry: 0}.String()
		requestSubject := "agent.request." + loopID

		predecessor, handler := startLoopProcess(t, client, DefaultConfig())
		_, birth := deliverTask(t, predecessor, born)
		require.Equal(t, natsclient.DeliveryDecisionAck, birth.Decision())

		// One call, so applying its result completes the batch and the loop
		// advances: the record leaves iteration zero and names R2.
		batch := agentic.AgentResponse{
			RequestID:    firstRequest,
			Status:       agentic.StatusToolCall,
			FinishReason: "tool_calls",
			Message: agentic.ChatMessage{
				Role:      "assistant",
				ToolCalls: []agentic.ToolCall{{ID: "call-advanced", Name: "advanced_tool"}},
			},
		}
		retainModelResponse(t, client, batch)
		dispatch, err := handler.HandleModelResponse(t.Context(), loopID, batch)
		require.NoError(t, err)
		require.NoError(t, predecessor.persistHandlerResult(t.Context(), dispatch, publishThenWrite))
		call, _ := dispatchedToolCall(t, dispatch)

		_, applied := deliverToolResult(t, predecessor, agentic.ToolResult{
			CallID: call.ID, Name: call.Name, Content: "the tool answered", LoopID: loopID,
			RequestID: call.RequestID, ExecutionID: call.ExecutionID, CallOrdinal: call.CallOrdinal,
		})
		require.Equal(t, natsclient.DeliveryDecisionAck, applied.Decision())

		record := loopRecordOf(t, predecessor, loopID)
		require.Equal(t, 1, record.entity.Iterations,
			"the completed batch advanced the loop, which is what this arm is about")
		require.Equal(t, secondRequest, record.entity.PublishedRequestID)
		require.Equal(t, born.TaskID, record.entity.TaskID)
		require.Equal(t, uint64(2), messagesOn(t, client, requestSubject))

		replacement, replacementHandler := startLoopProcess(t, client, DefaultConfig())
		before := refusals(replacement)

		_, refused := deliverTask(t, replacement, continuation)

		require.Equal(t, natsclient.DeliveryDecisionAck, refused.Decision(),
			"the settlement is the same on both arms; what changes is that this one was already "+
				"acknowledged as an applied task, with the turn's text silently dropped")
		_, seated := replacementHandler.loopManager.GetLoop(loopID)
		require.Error(t, seated, "a refused turn seats no loop")
		require.Equal(t, uint64(2), messagesOn(t, client, requestSubject),
			"a refused turn publishes nothing")

		after := loopRecordOf(t, replacement, loopID)
		require.Equal(t, record.revision, after.revision,
			"an acknowledged-without-effect turn writes nothing")
		require.Equal(t, born.TaskID, after.entity.TaskID,
			"the record must still belong to the task that created the loop")

		require.Equal(t, before+1, refusals(replacement),
			"an advanced record acknowledged the turn as 'its loop already moved past it' — a turn "+
				"nobody ever sent is not an applied task, and an operator saw no reason at all")
	})
}
