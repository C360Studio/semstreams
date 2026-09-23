//go:build integration

package agenticloop

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/metric"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/processor/agentic-loop/internal/looprequest"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
)

// The crash windows of #1330 are about what a REAL broker retained when a
// process stopped. The unit tests drive every arm of the classification
// against fakes; these two files drive the two lanes against a real NATS
// server, where the retained request is read with GetLastMsgForSubject, the
// record moves under a real compare-and-swap, and "was a second copy
// published?" is answered by counting the messages the stream holds on one
// subject rather than by trusting a recording double.
//
// Assertions read the record's fields and per-subject message counts only. No
// message BODY is compared: recovery here orders request identities, and a
// decision made by comparing rendered conversation content is the shape this
// whole change removes.

// loopStreamName is the stream DefaultConfig declares for every agentic-loop
// port. The tests create it with the subjects the component publishes to, so
// the publish path and the retained-request read address the same place they
// do in production.
const loopStreamName = "AGENT"

func newLoopNATS(t *testing.T) *natsclient.Client {
	t.Helper()
	testClient := natsclient.NewTestClient(t,
		natsclient.WithJetStream(),
		natsclient.WithKV(),
		natsclient.WithStreams(natsclient.TestStreamConfig{
			Name:     loopStreamName,
			Subjects: []string{"agent.>", "tool.>"},
		}))
	return testClient.Client
}

// startLoopProcess is one agentic-loop process: its own handler, its own
// component, its own memory — over durable state it shares with every other
// process that ever held these loops.
//
// A replacement is built by calling this a second time. Nothing of the
// predecessor is carried over, which is the whole point: the routing maps, the
// context managers and the minted-request map are all process-local, and the
// record plus the stream are all a replacement has to recover from.
func startLoopProcess(t *testing.T, client *natsclient.Client, config Config) (*Component, *MessageHandler) {
	t.Helper()
	h := NewMessageHandler(config)
	c := releaseTestComponent(t, h)
	c.config = config
	c.natsClient = client
	c.metrics = getMetrics(metric.NewMetricsRegistry())
	h.SetMetrics(c.metrics)
	require.NoError(t, c.initializeKVBuckets(t.Context()))
	return c, h
}

// bornLoop drives a real birth: the handler mints the loop and its first
// request, the component writes the record and then publishes, which is the
// order birth keeps (#1330 task 2.1). It returns the loop and the name of the
// request now retained for it.
// bornLoopPrompt is the task every bornLoop starts from, named so an assertion
// about where the rebuilt conversation opens cannot drift from the fixture.
const bornLoopPrompt = "recover from a crash between the publish and the record write"

func bornLoop(t *testing.T, c *Component, h *MessageHandler, taskID string) (loopID, requestID string) {
	t.Helper()
	result, err := h.HandleTask(t.Context(), TaskMessage{
		TaskID: taskID,
		Role:   "general",
		Model:  "test-model",
		Prompt: bornLoopPrompt,
	})
	require.NoError(t, err)
	require.NoError(t, c.createLoopState(t.Context(), result.LoopID))
	require.NoError(t, c.publishResults(t.Context(), result))
	return result.LoopID, mintedRequest(t, result)
}

// mintedRequest returns the identity of the request a handler result carries,
// which is also the Nats-Msg-Id it publishes under.
func mintedRequest(t *testing.T, result HandlerResult) string {
	t.Helper()
	for _, published := range result.PublishedMessages {
		if strings.HasPrefix(published.Subject, "agent.request.") {
			require.NotEmpty(t, published.MsgID, "a minted request publishes under its own identity")
			return published.MsgID
		}
	}
	t.Fatal("handler result published no agent.request")
	return ""
}

// dispatchedToolCall returns the call a handler result put on tool.execute,
// with the correlation agentic-tools echoes back on its result.
func dispatchedToolCall(t *testing.T, result HandlerResult) (agentic.ToolCall, string) {
	t.Helper()
	for _, published := range result.PublishedMessages {
		if !strings.HasPrefix(published.Subject, "tool.execute.") {
			continue
		}
		var envelope struct {
			Payload agentic.ToolCall `json:"payload"`
		}
		require.NoError(t, json.Unmarshal(published.Data, &envelope))
		return envelope.Payload, published.Subject
	}
	t.Fatal("handler result published no tool.execute")
	return agentic.ToolCall{}, ""
}

// crashedBeforeRecordUpdate is a process that dies between its publish and its
// record write.
//
// Only Update is replaced: everything the component reads still reads the real
// bucket, and the publish that runs first has really PubAck'd by the time the
// write is attempted. What is left on the server afterwards is exactly the W4
// residue — the next request retained, the record still naming the previous
// one — with no fixture writing it by hand.
type crashedBeforeRecordUpdate struct {
	jetstream.KeyValue
}

func (crashedBeforeRecordUpdate) Update(context.Context, string, []byte, uint64) (uint64, error) {
	return 0, errors.New("the process died before its record update reached the server")
}

// messagesOn counts what the stream retains on exactly one subject.
func messagesOn(t *testing.T, client *natsclient.Client, subject string) uint64 {
	t.Helper()
	stream, err := client.GetStream(t.Context(), loopStreamName)
	require.NoError(t, err)
	info, err := stream.Info(t.Context(), jetstream.WithSubjectFilter(subject))
	require.NoError(t, err)
	return info.State.Subjects[subject]
}

// retainModelResponse publishes a model response exactly as agentic-model
// does, so the loop's answer is on the stream where a replacement's rebuild
// reads it (#1330 task 1.2).
//
// A test that calls HandleModelResponse directly skips the delivery that would
// have put it there; the durable fact is not optional, so the test supplies it
// rather than pretending the rebuild can work without it.
func retainModelResponse(t *testing.T, client *natsclient.Client, response agentic.AgentResponse) {
	t.Helper()
	// Resolved from the same port declaration the recovery reader resolves
	// (responseAddress), never formatted here: a port change must move the
	// fixture and the reader under test together, or the test would go on
	// publishing where the reader no longer looks and prove nothing.
	subject, _, err := responseAddress(DefaultConfig().Ports.Inputs, response.RequestID)
	require.NoError(t, err)
	data, err := json.Marshal(message.NewBaseMessage(response.Schema(), &response, "agentic-model"))
	require.NoError(t, err)
	require.NoError(t, client.PublishToStream(t.Context(), subject, data))
}

// loopRecordOf reads the loop's record through the production reader, so the
// revision the assertions compare is the one a writer would compare-and-swap
// against.
func loopRecordOf(t *testing.T, c *Component, loopID string) loopRecord {
	t.Helper()
	record := c.readLoopRecord(t.Context(), loopID)
	require.NotEqual(t, loopPresenceUnknown, record.presence, "the loop record could not be read")
	return record
}

// TestToolResultRedeliveredToAReplacementProcess is task 4.2's tool lane: the
// two crash windows a tool result can be redelivered into, over a real broker.
//
// Both subtests build their residue the same way — a real loop, a real
// dispatch, and a process that dies at its record write — and then hand the
// same delivery to a REPLACEMENT process, which is the state the cold arms
// exist for. What separates them is what the predecessor had already put on
// the stream: in W4 the loop's next request, in W2 nothing at all. The first
// is acknowledged after the record is brought forward; the second is still
// owed to somebody and must not be acknowledged away.
//
// spec: agentic-loop / The loop record names its outstanding request
func TestToolResultRedeliveredToAReplacementProcess(t *testing.T) {
	client := newLoopNATS(t)

	t.Run("W4: the next request is retained and the record never learned its name", func(t *testing.T) {
		predecessor, handler := startLoopProcess(t, client, DefaultConfig())
		loopID, firstRequest := bornLoop(t, predecessor, handler, "task-tool-w4")
		requestSubject := "agent.request." + loopID
		require.Equal(t, uint64(1), messagesOn(t, client, requestSubject))

		// One tool call, so the batch completes on its only result — and that
		// completion is what mints the loop's next request.
		dispatch, err := handler.HandleModelResponse(t.Context(), loopID, agentic.AgentResponse{
			RequestID:    firstRequest,
			Status:       agentic.StatusToolCall,
			FinishReason: "tool_calls",
			Message: agentic.ChatMessage{
				Role:      "assistant",
				ToolCalls: []agentic.ToolCall{{ID: "call-w4", Name: "w4_tool"}},
			},
		})
		require.NoError(t, err)
		require.NoError(t, predecessor.persistHandlerResult(t.Context(), dispatch, publishThenWrite))
		call, executeSubject := dispatchedToolCall(t, dispatch)

		// From here the predecessor is dying: it will publish and never record.
		predecessor.loopsBucket = crashedBeforeRecordUpdate{KeyValue: predecessor.loopsBucket}
		result := agentic.ToolResult{
			CallID: call.ID, Name: call.Name, Content: "the tool answered", LoopID: loopID,
			RequestID: call.RequestID, ExecutionID: call.ExecutionID, CallOrdinal: call.CallOrdinal,
		}
		_, died := deliverToolResult(t, predecessor, result)
		require.Equal(t, natsclient.DeliveryDecisionQuarantine, died.Decision(),
			"a record write of unknown durability is not an acknowledgement")

		// The residue, read off the server: the loop's SECOND request is on the
		// stream and the record still names the first. That gap is W4.
		secondRequest := looprequest.ID{LoopID: loopID, Iteration: 2, Retry: 0}.String()
		require.Equal(t, uint64(2), messagesOn(t, client, requestSubject))
		require.Equal(t, firstRequest, loopRecordOf(t, predecessor, loopID).entity.PublishedRequestID)

		// The replacement has no memory of the loop, so the result arrives
		// through the cold arm: step 0 adopts what the stream retains, and only
		// then is the delivery classified.
		replacement, _ := startLoopProcess(t, client, DefaultConfig())
		superseded := toolDropDelta(replacement, "older_request")

		msg, delivered := deliverToolResult(t, replacement, result)

		require.Equal(t, natsclient.DeliveryDecisionAck, delivered.Decision(),
			"after adoption the result names an older request than the record, and nobody is owed it")
		require.Equal(t, int32(1), msg.acks.Load())
		require.Equal(t, float64(1), superseded(), "a drop nobody can see is a drop an operator cannot act on")

		adopted := loopRecordOf(t, replacement, loopID)
		require.Equal(t, secondRequest, adopted.entity.PublishedRequestID,
			"the record must name the request the stream retains before anything is classified against it")
		require.Equal(t, 1, adopted.entity.Iterations,
			"the adopted iteration is the one its request was minted at")
		require.Empty(t, adopted.entity.PendingToolResults,
			"the advance that minted the next request drained the applied set")
		require.False(t, adopted.entity.State.IsTerminal())

		require.Equal(t, uint64(2), messagesOn(t, client, requestSubject),
			"adoption must not put a second copy of the loop's request on the stream")
		require.Equal(t, uint64(1), messagesOn(t, client, executeSubject),
			"an acknowledged-without-effect result dispatches nothing")
	})

	t.Run("W2: the result was applied in memory and the record never learned it", func(t *testing.T) {
		predecessor, handler := startLoopProcess(t, client, DefaultConfig())
		loopID, firstRequest := bornLoop(t, predecessor, handler, "task-tool-w2")
		requestSubject := "agent.request." + loopID

		// Two tool calls, so the first result leaves the batch incomplete: the
		// loop mints no new request, and the only durable fact the predecessor
		// owed was the applied set.
		batch := agentic.AgentResponse{
			RequestID:    firstRequest,
			Status:       agentic.StatusToolCall,
			FinishReason: "tool_calls",
			Message: agentic.ChatMessage{
				Role: "assistant",
				ToolCalls: []agentic.ToolCall{
					{ID: "call-w2-a", Name: "w2_tool"},
					{ID: "call-w2-b", Name: "w2_tool"},
				},
			},
		}
		retainModelResponse(t, client, batch)
		dispatch, err := handler.HandleModelResponse(t.Context(), loopID, batch)
		require.NoError(t, err)
		require.NoError(t, predecessor.persistHandlerResult(t.Context(), dispatch, publishThenWrite))
		call, executeSubject := dispatchedToolCall(t, dispatch)
		require.Equal(t, uint64(1), messagesOn(t, client, executeSubject),
			"an assistant batch dispatches serially: one call out, the sibling queued")

		predecessor.loopsBucket = crashedBeforeRecordUpdate{KeyValue: predecessor.loopsBucket}
		result := agentic.ToolResult{
			CallID: call.ID, Name: call.Name, Content: "the first tool answered", LoopID: loopID,
			RequestID: call.RequestID, ExecutionID: call.ExecutionID, CallOrdinal: call.CallOrdinal,
		}
		_, died := deliverToolResult(t, predecessor, result)
		require.Equal(t, natsclient.DeliveryDecisionQuarantine, died.Decision())

		// The W2 residue: the effect landed — the first result was applied in
		// memory and the queued sibling went out on the real stream — and the
		// record write that would have made either fact durable never did.
		dispatched := messagesOn(t, client, executeSubject)
		require.Equal(t, uint64(2), dispatched,
			"the applied result released its sibling before the record write was attempted")

		crashed := loopRecordOf(t, predecessor, loopID)
		require.Equal(t, firstRequest, crashed.entity.PublishedRequestID)
		require.Equal(t, uint64(1), messagesOn(t, client, requestSubject),
			"an incomplete batch mints no request")

		// The replacement finds nothing newer than the record, so the result is
		// still the current request's. No process holds the loop, and before
		// L4a that meant the delivery was refused with "not held by this
		// process" and retried until MaxDeliver stopped redelivering it — an
		// executor's completed work, durably on the stream and structurally
		// unreachable. It now rebuilds the loop from the record and the two
		// retained messages, and applies.
		replacement, replacementHandler := startLoopProcess(t, client, DefaultConfig())
		superseded := toolDropDelta(replacement, "older_request")
		stale := toolDropDelta(replacement, "stale_execution")
		unproven := toolDropDelta(replacement, "terminal_unproven")

		msg, delivered := deliverToolResult(t, replacement, result)

		require.Equal(t, natsclient.DeliveryDecisionAck, delivered.Decision(),
			"a result the rebuilt loop applied is settled, not owed to a process that will never exist")
		require.Equal(t, int32(1), msg.acks.Load())
		require.Zero(t, superseded()+stale()+unproven(), "an applied result is not a dropped one")

		rebuilt, err := replacementHandler.loopManager.GetLoop(loopID)
		require.NoError(t, err, "the replacement must HOLD the loop it rebuilt")
		require.Contains(t, rebuilt.PendingToolResults, result.ExecutionID)

		// The retained request is prependIterationContext's OUTPUT, so on the
		// wire it opens with that iteration's budget line and, when the loop
		// has one, its working list. Both are Role "system". A rebuild that
		// seated them would pin ONE iteration's framing at the top of
		// RegionSystemPrompt for the rest of the loop's life while every later
		// request prepends a fresh one. This loop was born through the real
		// task lane, so the prefix is on the retained request for free.
		rebuiltContext := replacementHandler.loopManager.GetContextManager(loopID).GetContext()
		require.NotEmpty(t, rebuiltContext)
		require.Equal(t, bornLoopPrompt, rebuiltContext[0].Content,
			"the rebuilt conversation must open where the loop's own conversation opens; "+
				"before the prefix filter it opened with an iteration-budget line")
		for _, msg := range rebuiltContext {
			require.NotContains(t, msg.Content, iterationBudgetPrefix,
				"the request's per-iteration framing was replayed into the rebuilt conversation")
			require.NotContains(t, msg.Content, workingListPrefix,
				"the request's per-iteration framing was replayed into the rebuilt conversation")
		}

		after := loopRecordOf(t, replacement, loopID)
		require.Greater(t, after.revision, crashed.revision,
			"the applied fact the predecessor lost is durable only if the rebuilt holder can write the record")
		require.Contains(t, after.entity.PendingToolResults, result.ExecutionID,
			"the applied set the crash lost must now be on the record")
		require.Equal(t, firstRequest, after.entity.PublishedRequestID,
			"one result of a two-call batch completes nothing, so no new request is minted")
		require.False(t, after.entity.State.IsTerminal())
		require.Equal(t, uint64(1), messagesOn(t, client, requestSubject))

		// The record says which executions are APPLIED, never which were
		// DISPATCHED. So the rebuild queues the sibling the predecessor had
		// already sent, and the apply dispatches it again — under the SAME
		// execution identity, because that identity is derived from the
		// request and the call, not minted (L2). agentic-tools keys
		// TOOL_CALL_OUTCOMES by it and replays the recorded outcome rather
		// than running the tool twice, which is what makes the re-dispatch a
		// replay instead of a duplicate execution.
		require.Equal(t, dispatched+1, messagesOn(t, client, executeSubject),
			"the rebuilt loop must go on running the batch it recovered")
		sibling := deriveToolExecutionID(firstRequest, "call-w2-b", 2)
		routed, held := replacementHandler.loopManager.GetLoopForToolCall(sibling)
		require.True(t, held,
			"the re-dispatched sibling must carry the identity its first dispatch derived")
		require.Equal(t, loopID, routed)
	})

	// The qualification the two arms above do not carry, and the one the
	// published layers used to omit: a rebuild is not a reprieve.
	//
	// `TimeoutAt` is written at birth and lives on the record, so the rebuild
	// seats it along with everything else (`state.go`, restoreLoopFromRequest).
	// The warm apply then checks it (`handlers.go` HandleToolResult) and a
	// replacement whose gap outran the loop's own deadline rebuilds the loop and
	// immediately fails it. That is the DOCUMENTED behaviour, not a defect: the
	// alternative — refreshing the deadline on rebuild, or excluding downtime
	// from it — would let a loop outlive the budget its caller set, which is an
	// owner ruling and not a recovery decision.
	//
	// The gap is expressed the way production expresses it — wall clock against
	// the record's own `TimeoutAt` — rather than by rewriting the record, so
	// what the test proves is what an operator would see. The deadline is short
	// because a test cannot wait out a 120s one; the predecessor's whole run
	// above it takes milliseconds.
	//
	// spec: agentic-loop / The loop record names its outstanding request
	t.Run("the replacement gap outran the loop's deadline: rebuilt, then failed", func(t *testing.T) {
		config := DefaultConfig()
		config.Timeout = shortLoopDeadline.String()

		predecessor, handler := startLoopProcess(t, client, config)
		loopID, firstRequest := bornLoop(t, predecessor, handler, "task-tool-deadline")
		requestSubject := "agent.request." + loopID

		batch := agentic.AgentResponse{
			RequestID:    firstRequest,
			Status:       agentic.StatusToolCall,
			FinishReason: "tool_calls",
			Message: agentic.ChatMessage{
				Role: "assistant",
				ToolCalls: []agentic.ToolCall{
					{ID: "call-deadline-a", Name: "deadline_tool"},
					{ID: "call-deadline-b", Name: "deadline_tool"},
				},
			},
		}
		retainModelResponse(t, client, batch)
		dispatch, err := handler.HandleModelResponse(t.Context(), loopID, batch)
		require.NoError(t, err)
		require.NoError(t, predecessor.persistHandlerResult(t.Context(), dispatch, publishThenWrite))
		call, _ := dispatchedToolCall(t, dispatch)

		predecessor.loopsBucket = crashedBeforeRecordUpdate{KeyValue: predecessor.loopsBucket}
		result := agentic.ToolResult{
			CallID: call.ID, Name: call.Name, Content: "the tool answered", LoopID: loopID,
			RequestID: call.RequestID, ExecutionID: call.ExecutionID, CallOrdinal: call.CallOrdinal,
		}
		_, died := deliverToolResult(t, predecessor, result)
		require.Equal(t, natsclient.DeliveryDecisionQuarantine, died.Decision())

		crashed := loopRecordOf(t, predecessor, loopID)
		require.False(t, crashed.entity.TimeoutAt.IsZero(),
			"the deadline this arm is about must be ON the record, or the rebuild inherits nothing")
		require.False(t, crashed.entity.State.IsTerminal())
		waitPastLoopDeadline(t, crashed.entity.TimeoutAt)

		replacement, replacementHandler := startLoopProcess(t, client, config)
		_, delivered := deliverToolResult(t, replacement, result)

		// Rebuilt, then failed, and BOTH halves are read off durable state. The
		// in-process loop is deliberately not the witness: the terminal
		// transition releases it, so by the time the delivery returns, GetLoop
		// reports "not found" for a loop this process really did hold.
		//
		// The failure event is the proof the rebuild ran. It is built by
		// HandleToolResult, which only runs for a loop this process HOLDS —
		// before L4a the delivery was refused for a loop nobody holds and no
		// deadline was ever consulted.
		failureSubject := "agent.failed." + loopID
		require.Equal(t, uint64(1), messagesOn(t, client, failureSubject),
			"a rebuilt-then-expired loop settles on agent.failed, where a reactive consumer can see it; "+
				"no event here means the delivery was refused instead of rebuilt")
		require.Equal(t, "loop timeout exceeded", failureReasonOn(t, client, failureSubject))
		_, held := replacementHandler.loopManager.GetLoop(loopID)
		require.Error(t, held, "a settled loop releases its process state")

		expired := loopRecordOf(t, replacement, loopID)
		require.Equal(t, crashed.entity.TimeoutAt.UTC(), expired.entity.TimeoutAt.UTC(),
			"the rebuilt loop carries the record's ORIGINAL deadline; nothing refreshes it on rebuild")
		require.Equal(t, agentic.LoopStateFailed, expired.entity.State)
		require.Greater(t, expired.revision, crashed.revision,
			"the failure is durable, not just published")

		// The delivery is ACKNOWLEDGED, and that is the sharp edge: the loop
		// settled terminally, its failure is published and durable, and nothing
		// is owed to anyone — so this shape is INVISIBLE to a consumer-health
		// or settlement check, which sees a clean ack. The only place it shows
		// up is agent.failed, which is why that is what this arm asserts on.
		require.Equal(t, natsclient.DeliveryDecisionAck, delivered.Decision(),
			"a settled failure owes nobody a redelivery; this is why the shape hides from settlement checks")
		require.Equal(t, uint64(1), messagesOn(t, client, requestSubject),
			"an expired loop mints no further request")
	})
}

// shortLoopDeadline is the loop timeout the deadline arm births under. It has
// to be long enough that the predecessor's whole run — birth, model response,
// dispatch, the crashed delivery — finishes inside it on a loaded machine, and
// short enough that a test can wait it out. The predecessor's run is
// sub-millisecond work against a local broker; a second is three orders of
// magnitude of headroom.
const shortLoopDeadline = time.Second

// waitPastLoopDeadline blocks until the loop's own recorded deadline is in the
// past by a small margin, so the comparison cannot land on the boundary. It
// waits on the RECORD's timestamp rather than on a duration the test picked, so
// the arm stays correct if the deadline above ever changes. The wait is a
// polled condition with a bound rather than a sleep: a bare time.Sleep in an
// integration test is the shape test/testinfra's policy guard ratchets out, and
// the bound doubles as the premise that the deadline sits inside this arm's
// budget.
func waitPastLoopDeadline(t *testing.T, deadline time.Time) {
	t.Helper()
	const boundaryMargin = 50 * time.Millisecond
	require.Eventually(t, func() bool { return time.Now().After(deadline.Add(boundaryMargin)) },
		5*time.Second, 10*time.Millisecond,
		"the recorded deadline %s is further out than this arm budgets for", deadline.UTC().Format(time.RFC3339Nano))
}

// failureEventOn reads the loop's failure event back through the same envelope
// the component published it in.
func failureEventOn(t *testing.T, client *natsclient.Client, subject string) agentic.LoopFailedEvent {
	t.Helper()
	stream, err := client.GetStream(t.Context(), loopStreamName)
	require.NoError(t, err)
	stored, err := stream.GetLastMsgForSubject(t.Context(), subject)
	require.NoError(t, err)
	var envelope struct {
		Payload agentic.LoopFailedEvent `json:"payload"`
	}
	require.NoError(t, json.Unmarshal(stored.Data, &envelope))
	return envelope.Payload
}

// failureReasonOn reads the error that event carries.
func failureReasonOn(t *testing.T, client *natsclient.Client, subject string) string {
	t.Helper()
	return failureEventOn(t, client, subject).Error
}

// TestAReplayedAppliedToolResultDoesNotQuarantineItsLane is the lost-ACK arm
// of the cold tool lane (owner Codex round on PR #1361, finding 5).
//
// A tool result is applied, the record learns it, its sibling is still
// running, and then the delivery's acknowledgement is lost — ordinary
// at-least-once behaviour. The redelivery cannot be ordered away: the batch
// belongs to the request the record still names, so ordering says "current".
// The cold arm therefore rebuilt the loop, and the rebuild deliberately leaves
// applied executions unrouted — so the lane immediately found no route for the
// arriving execution and Terminated it. A routine redelivery quarantined the
// tool lane, and the batch's unfinished sibling was left behind a loop this
// process now holds and cannot rebuild over.
//
// Membership in the record's applied set is the only fact that decides, and it
// is read BEFORE anything is rebuilt: the replay settles, nothing is touched,
// and the sibling's own arrival is what recovers the batch.
//
// spec: agentic-loop / The loop record names its outstanding request
func TestAReplayedAppliedToolResultDoesNotQuarantineItsLane(t *testing.T) {
	client := newLoopNATS(t)

	const loopID = "5f1e3a82-7b64-4c09-8d25-1a3b4c5d6e70"
	task := agentic.TaskMessage{
		TaskID: "task-replayed-applied-result",
		LoopID: loopID,
		Role:   "general",
		Model:  "test-model",
		Prompt: "the task whose first tool result loses its acknowledgement",
	}
	firstRequest := looprequest.ID{LoopID: loopID, Iteration: 1, Retry: 0}.String()
	secondRequest := looprequest.ID{LoopID: loopID, Iteration: 2, Retry: 0}.String()
	requestSubject := "agent.request." + loopID

	predecessor, handler := startLoopProcess(t, client, DefaultConfig())
	_, birth := deliverTask(t, predecessor, task)
	require.Equal(t, natsclient.DeliveryDecisionAck, birth.Decision())

	// Two calls, so applying the first leaves the sibling unfinished — the
	// state that makes this a replay rather than a late result for a loop that
	// has already moved on.
	batch := agentic.AgentResponse{
		RequestID:    firstRequest,
		Status:       agentic.StatusToolCall,
		FinishReason: "tool_calls",
		Message: agentic.ChatMessage{
			Role: "assistant",
			ToolCalls: []agentic.ToolCall{
				{ID: "call-replay-a", Name: "replay_tool"},
				{ID: "call-replay-b", Name: "replay_tool"},
			},
		},
	}
	retainModelResponse(t, client, batch)
	dispatch, err := handler.HandleModelResponse(t.Context(), loopID, batch)
	require.NoError(t, err)
	require.NoError(t, predecessor.persistHandlerResult(t.Context(), dispatch, publishThenWrite))
	callA, _ := dispatchedToolCall(t, dispatch)

	resultA := agentic.ToolResult{
		CallID: callA.ID, Name: callA.Name, Content: "the first tool answered", LoopID: loopID,
		RequestID: callA.RequestID, ExecutionID: callA.ExecutionID, CallOrdinal: callA.CallOrdinal,
	}
	_, appliedA := deliverToolResult(t, predecessor, resultA)
	require.Equal(t, natsclient.DeliveryDecisionAck, appliedA.Decision())

	applied := loopRecordOf(t, predecessor, loopID)
	require.Contains(t, applied.entity.PendingToolResults, resultA.ExecutionID,
		"the replay under test is of a result the RECORD carries; without it this is a different arm")
	require.Equal(t, firstRequest, applied.entity.PublishedRequestID,
		"the batch still belongs to the request the record names, so ordering cannot settle this")

	// A's acknowledgement was lost and the process was replaced. The same
	// bytes arrive again at a process with no memory of the loop.
	replacement, replacementHandler := startLoopProcess(t, client, DefaultConfig())
	replays := toolDropDelta(replacement, "already_applied")
	stale := toolDropDelta(replacement, "stale_execution")
	superseded := toolDropDelta(replacement, "older_request")

	msg, redelivered := deliverToolResult(t, replacement, resultA)

	require.Equal(t, natsclient.DeliveryDecisionAck, redelivered.Decision(),
		"a lost ACK is ordinary at-least-once delivery; terminating it quarantines the tool lane")
	require.Zero(t, msg.terms.Load(), "the replay was quarantined")
	require.Equal(t, float64(1), replays(),
		"a drop nobody can see is a drop an operator cannot act on")
	require.Zero(t, stale()+superseded(), "a replay is neither a stale execution nor an older request")

	_, seated := replacementHandler.loopManager.GetLoop(loopID)
	require.Error(t, seated,
		"a replay settles without touching the loop; rebuilding here would seat a loop whose own "+
			"arriving execution has no route, which is what Terminated the lane")
	after := loopRecordOf(t, replacement, loopID)
	require.Equal(t, applied.revision, after.revision, "an acknowledged-without-effect result writes nothing")
	require.Equal(t, applied.entity.PendingToolResults, after.entity.PendingToolResults,
		"the applied set the unfinished sibling will be rebuilt against must survive the replay")

	// The sibling proves the siblings' progress was preserved: it arrives,
	// rebuilds the batch against the untouched record, completes it, and mints
	// the loop's next request.
	_, deliveredSibling := deliverToolResult(t, replacement, agentic.ToolResult{
		CallID: "call-replay-b", Name: "replay_tool", Content: "the sibling answered",
		LoopID: loopID, RequestID: firstRequest,
		ExecutionID: deriveToolExecutionID(firstRequest, "call-replay-b", 2), CallOrdinal: 2,
	})
	require.Equal(t, natsclient.DeliveryDecisionAck, deliveredSibling.Decision())

	completed := loopRecordOf(t, replacement, loopID)
	require.Equal(t, 1, completed.entity.Iterations, "the completed batch advances its loop")
	require.Equal(t, secondRequest, completed.entity.PublishedRequestID)
	require.Equal(t, uint64(2), messagesOn(t, client, requestSubject),
		"a recovered batch's completion is what puts the loop's second request on the stream")
}
