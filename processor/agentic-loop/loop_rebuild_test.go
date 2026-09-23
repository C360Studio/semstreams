package agenticloop

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/metric"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/processor/agentic-loop/internal/looprequest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const rebuildLoopID = "4b7d2e91-0c3a-4f65-9a8b-7c6d5e4f3a21"

// rebuiltRecord is what a cold process reads out of AGENT_LOOPS: a loop it has
// no memory of, at some iteration, naming the request it last published.
func rebuiltRecord(requestID string, shape func(*agentic.LoopEntity)) agentic.LoopEntity {
	record := agentic.NewLoopEntity(rebuildLoopID, "task-rebuild", "general", "test-model", 10)
	record.State = agentic.LoopStateExecuting
	record.Iterations = 2
	record.PublishedRequestID = requestID
	if shape != nil {
		shape(&record)
	}
	return record
}

// retainedRequest is what agent.request.<loopID> holds for that record: the
// conversation as GetContext() rendered it, plus the settings the loop was
// running with.
func retainedRequest(requestID string, messages ...agentic.ChatMessage) agentic.AgentRequest {
	return agentic.AgentRequest{
		RequestID: requestID,
		LoopID:    rebuildLoopID,
		Role:      "general",
		Model:     "test-model",
		Messages:  messages,
		Tools: []agentic.ToolDefinition{
			{Name: "search", Description: "find things"},
		},
		ToolChoice: &agentic.ToolChoice{Mode: "auto"},
		Timeout:    "45s",
	}
}

func roles(messages []agentic.ChatMessage) []string {
	out := make([]string, 0, len(messages))
	for _, msg := range messages {
		out = append(out, msg.Role)
	}
	return out
}

// TestARebuiltLoopIsTheRecordPlusItsRetainedRequest is task 1.2's rebuild:
// everything a replacement needs to go on running a loop it never started,
// taken from two durable facts and nothing else.
//
// spec: agentic-loop / The loop record names its outstanding request
func TestARebuiltLoopIsTheRecordPlusItsRetainedRequest(t *testing.T) {
	requestID := looprequest.ID{LoopID: rebuildLoopID, Iteration: 3, Retry: 0}.String()

	t.Run("the record supplies the loop and the request supplies its conversation", func(t *testing.T) {
		manager := NewLoopManager()
		record := rebuiltRecord(requestID, func(e *agentic.LoopEntity) {
			e.PendingToolResults = map[string]agentic.ToolResult{
				"tool-exec-v1-already": {ExecutionID: "tool-exec-v1-already", Name: "search", Content: "answered"},
			}
		})
		request := retainedRequest(requestID,
			agentic.ChatMessage{Role: "system", Content: "you are a test agent"},
			agentic.ChatMessage{Role: "user", Content: "the original task"},
			agentic.ChatMessage{Role: "assistant", Content: "thinking"},
		)

		require.NoError(t, manager.restoreLoopFromRequest(t.Context(), record, request))

		rebuilt, err := manager.GetLoop(rebuildLoopID)
		require.NoError(t, err)
		require.Equal(t, record.Iterations, rebuilt.Iterations)
		require.Equal(t, requestID, rebuilt.PublishedRequestID)
		require.Equal(t, record.State, rebuilt.State)
		require.Equal(t, record.MaxIterations, rebuilt.MaxIterations)
		require.Equal(t, record.PendingToolResults, rebuilt.PendingToolResults,
			"the applied set is the record's, and it decides what the next request carries")

		cm := manager.GetContextManager(rebuildLoopID)
		require.NotNil(t, cm)
		require.Equal(t, roles(request.Messages), roles(cm.GetContext()),
			"this fixture carries no per-iteration prefix, so the retained body IS GetContext()'s "+
				"order and the rebuild must render it back the same way")

		require.Equal(t, request.Tools, manager.GetCachedTools(rebuildLoopID),
			"a rebuilt loop with no tools cached would advertise none on its next request")
		require.Equal(t, request.ToolChoice, manager.GetCachedToolChoice(rebuildLoopID))
		require.Equal(t, "45s", manager.GetCachedRequestTimeout(rebuildLoopID))

		routed, ok := manager.GetLoopForRequest(requestID)
		require.True(t, ok, "the rebuilt loop must be reachable from the request it named")
		require.Equal(t, rebuildLoopID, routed)
		require.Equal(t, requestID, manager.OutstandingRequest(rebuildLoopID),
			"the only evidence in hand is that the request went out, so it is outstanding")
	})

	t.Run("an assistant turn whose results never arrived is repaired away", func(t *testing.T) {
		manager := NewLoopManager()
		request := retainedRequest(requestID,
			agentic.ChatMessage{Role: "system", Content: "you are a test agent"},
			agentic.ChatMessage{Role: "user", Content: "the original task"},
			agentic.ChatMessage{Role: "assistant", ToolCalls: []agentic.ToolCall{{ID: "call-orphan", Name: "search"}}},
		)

		require.NoError(t, manager.restoreLoopFromRequest(t.Context(), rebuiltRecord(requestID, nil), request))

		require.Equal(t, []string{"system", "user"}, roles(manager.GetContextManager(rebuildLoopID).GetContext()),
			"a request retained mid-batch carries an assistant tool_call with no tool message, and a "+
				"provider refuses that pair outright")
	})

	t.Run("the retained request must be the one the record names", func(t *testing.T) {
		manager := NewLoopManager()
		older := looprequest.ID{LoopID: rebuildLoopID, Iteration: 2, Retry: 0}.String()

		err := manager.restoreLoopFromRequest(t.Context(), rebuiltRecord(requestID, nil),
			retainedRequest(older, agentic.ChatMessage{Role: "user", Content: "an older turn"}))

		require.Error(t, err)
		require.True(t, errs.IsInvalid(err))
		_, getErr := manager.GetLoop(rebuildLoopID)
		require.Error(t, getErr, "a refused rebuild must leave no loop behind")
	})

	t.Run("the per-iteration prefix belongs to the request, not to the conversation", func(t *testing.T) {
		// The fixture is built by the PRODUCTION prefixer, not by hand: a hand-
		// written approximation of what a mint attaches is the reconstruction
		// this whole change removes, and it would go on passing after the
		// prefix's wording changed.
		handler := &MessageHandler{
			config:   Config{},
			platform: todoTestPlatform(),
			todoReader: &fakeTodoReader{
				todos: []TodoState{{ID: "1", Content: "the predecessor's working list", Status: "in_progress"}},
			},
		}
		handler.logger = todoTestLogger()
		conversation := []agentic.ChatMessage{
			{Role: "system", Content: "you are a test agent"},
			{Role: "user", Content: "the original task"},
			{Role: "assistant", Content: "thinking"},
		}
		minted := handler.prependIterationContext(t.Context(), rebuildLoopID, 3, 20, conversation)
		require.Len(t, minted, len(conversation)+2,
			"the fixture must carry BOTH prefix messages, or this arm proves nothing")

		manager := NewLoopManager()
		require.NoError(t, manager.restoreLoopFromRequest(t.Context(),
			rebuiltRecord(requestID, nil), retainedRequest(requestID, minted...)))

		rebuilt := manager.GetContextManager(rebuildLoopID).GetContext()
		require.Equal(t, conversation, rebuilt,
			"the budget line and the working list are ONE iteration's framing; seating them pins "+
				"a stale budget at the top of the system prompt for the rest of the loop's life, "+
				"while every later request prepends a fresh one")
	})

	t.Run("a message that only looks like the prefix is still the conversation", func(t *testing.T) {
		manager := NewLoopManager()
		// A user is free to type either string, and a leading run is all a mint
		// can produce — so only a leading run is dropped.
		body := []agentic.ChatMessage{
			{Role: "system", Content: "you are a test agent"},
			{Role: "user", Content: "[Iteration Budget] explain what this line means"},
			{Role: "system", Content: "[Working list — quoted back by a tool]"},
		}
		require.NoError(t, manager.restoreLoopFromRequest(t.Context(),
			rebuiltRecord(requestID, nil), retainedRequest(requestID, body...)))

		// Membership, not order: the two-region rebuild renders every system
		// message before the recent history, which is the documented
		// attribution residual, not what this arm is about.
		rebuilt := manager.GetContextManager(rebuildLoopID).GetContext()
		require.Len(t, rebuilt, len(body),
			"the filter reached past the leading run and ate the conversation")
		require.Contains(t, rebuilt, body[1], "a user may type the budget prefix; it is still their message")
		require.Contains(t, rebuilt, body[2], "a prefix-shaped message in the body is the conversation")
	})

	t.Run("a loop this process already holds is not rebuilt over", func(t *testing.T) {
		manager := NewLoopManager()
		_, err := manager.CreateLoopWithID(rebuildLoopID, "task-live", "general", "test-model", 10)
		require.NoError(t, err)
		require.NoError(t, manager.GetContextManager(rebuildLoopID).AddMessage(
			RegionRecentHistory, agentic.ChatMessage{Role: "user", Content: "the live conversation"}))

		err = manager.restoreLoopFromRequest(t.Context(), rebuiltRecord(requestID, nil),
			retainedRequest(requestID, agentic.ChatMessage{Role: "user", Content: "a retained conversation"}))

		require.ErrorIs(t, err, ErrLoopAlreadyExists)
		require.Len(t, manager.GetContextManager(rebuildLoopID).GetContext(), 1,
			"the live conversation was overwritten by a retained one")
	})
}

// TestARestoredToolBatchKnowsWhatIsLeftToRun is the half of the rebuild the
// record alone cannot answer: how many calls the assistant asked for.
//
// spec: agentic-loop / The loop record names its outstanding request
func TestARestoredToolBatchKnowsWhatIsLeftToRun(t *testing.T) {
	requestID := looprequest.ID{LoopID: rebuildLoopID, Iteration: 3, Retry: 0}.String()
	calls := []agentic.ToolCall{
		{ID: "call-done", Name: "search", Arguments: map[string]any{"q": "one"}},
		{ID: "call-inflight", Name: "fetch", Arguments: map[string]any{"q": "two"}},
		{ID: "call-queued", Name: "write", Arguments: map[string]any{"q": "three"}},
	}
	executionOf := func(index int) string {
		return deriveToolExecutionID(requestID, calls[index].ID, uint32(index+1))
	}
	response := agentic.AgentResponse{
		RequestID: requestID,
		Status:    agentic.StatusToolCall,
		Message:   agentic.ChatMessage{Role: "assistant", Content: "three things", ToolCalls: calls},
	}

	manager := NewLoopManager()
	applied := map[string]agentic.ToolResult{
		executionOf(0): {ExecutionID: executionOf(0), CallID: calls[0].ID, Name: "search", Content: "answered"},
	}
	record := rebuiltRecord(requestID, func(e *agentic.LoopEntity) { e.PendingToolResults = applied })
	require.NoError(t, manager.restoreLoopFromRequest(t.Context(), record,
		retainedRequest(requestID, agentic.ChatMessage{Role: "user", Content: "do three things"})))

	require.NoError(t, manager.restoreToolBatch(rebuildLoopID, response, applied, executionOf(1)))

	next, ok := manager.DequeueToolCall(rebuildLoopID)
	require.True(t, ok, "the sibling that never ran must be queued for dispatch")
	require.Equal(t, "call-queued", next.ID)
	require.Equal(t, executionOf(2), next.ExecutionID,
		"the queued call must carry the identity its dispatch derives, not a fresh one")
	_, more := manager.DequeueToolCall(rebuildLoopID)
	require.False(t, more, "an applied call and the arriving one must not be queued for re-execution")

	routed, ok := manager.GetLoopForToolCall(executionOf(1))
	require.True(t, ok, "the arriving result must route to the rebuilt loop")
	require.Equal(t, rebuildLoopID, routed)
	_, appliedRouted := manager.GetLoopForToolCall(executionOf(0))
	require.False(t, appliedRouted,
		"an already-applied execution stays unroutable, as it is after GetAndClearToolResults")

	require.Equal(t, "write", manager.GetToolName(executionOf(2)))
	require.Equal(t, calls[2].Arguments, manager.GetToolArguments(executionOf(2)))

	require.Equal(t, []string{"user", "assistant"}, roles(manager.GetContextManager(rebuildLoopID).GetContext()),
		"the assistant turn the batch belongs to lives in the retained RESPONSE; without it every "+
			"tool message the batch produces is an orphan")
	require.Empty(t, manager.OutstandingRequest(rebuildLoopID),
		"the response for this request is in hand, so the loop is not waiting on a model")
}

// coldRebuildComponent is a replacement process meeting a loop it never
// started: a record in the bucket, the loop's request (and optionally its
// response) retained, and nothing at all in memory.
//
// Its NATS client is unconnected, so every publish genuinely fails on the
// production path. That is deliberate — a unit test can prove the rebuild
// happened and the delivery reached the handler; only a real broker can prove
// what the record and the stream look like afterwards, which is what the
// integration arms of task 1.2 assert.
func coldRebuildComponent(
	t *testing.T,
	requestID string,
	response *agentic.AgentResponse,
	shape func(*agentic.LoopEntity),
) (*Component, *MessageHandler, loopRecord) {
	t.Helper()
	h := NewMessageHandler(DefaultConfig())
	c := releaseTestComponent(t, h)
	c.metrics = getMetrics(metric.NewMetricsRegistry())
	h.SetMetrics(c.metrics)
	c.loopsBucket = &recordingLoopBucket{}
	c.natsClient = unpublishableClient(t)
	c.requestEvidence = stubEvidenceReader{requestID: requestID, response: response}
	record := coldRecord(t, c, rebuildLoopID, func(e *agentic.LoopEntity) {
		e.PublishedRequestID = requestID
		e.Iterations = 3
		if shape != nil {
			shape(e)
		}
	})
	_, err := h.loopManager.GetLoop(rebuildLoopID)
	require.Error(t, err, "the fixture must start with no memory of the loop")
	return c, h, record
}

// TestAColdResponseRebuildsTheLoopItAnswers is the response lane's half of the
// cold rebuild (#1330 task 1.2, design § 5.2 step 2).
//
// Before it, a model response naming the request its loop's record names, met
// by a process that does not hold that loop, was refused with "not held by
// this process" and retried until MaxDeliver stopped redelivering it,
// because after a process replacement no process ever holds it again. The
// model's answer was durably on the stream and structurally unreachable.
//
// spec: agentic-loop / The loop record names its outstanding request
func TestAColdResponseRebuildsTheLoopItAnswers(t *testing.T) {
	requestID := looprequest.ID{LoopID: rebuildLoopID, Iteration: 4, Retry: 0}.String()
	c, h, record := coldRebuildComponent(t, requestID, nil, nil)

	_, delivered := deliverResponse(t, c, agentic.AgentResponse{
		RequestID: requestID,
		Status:    agentic.StatusComplete,
		Message:   agentic.ChatMessage{Role: "assistant", Content: "the answer the predecessor never saw"},
	})

	entity, err := h.loopManager.GetLoop(rebuildLoopID)
	require.NoError(t, err,
		"the response was refused instead of rebuilding the loop it answers")
	require.True(t, entity.State.IsTerminal(),
		"the rebuilt loop was seated but the response never reached the handler")
	require.Equal(t, record.entity.Iterations, entity.Iterations,
		"the rebuilt loop took its iteration count from the record, not from zero")
	require.Equal(t, requestID, entity.PublishedRequestID)

	require.Equal(t, []string{"user", "assistant"},
		roles(h.loopManager.GetContextManager(rebuildLoopID).GetContext()),
		"the conversation is the retained request plus the answer just applied")

	// A completion compare-and-swaps the record against the revision the
	// observer read it at, BEFORE it publishes. The rebuilt process wrote it,
	// which it could only do by taking the record's revision with the loop —
	// without that, its first write is refused and the loop is recovered and
	// then immediately stranded.
	require.Equal(t, agentic.LoopStateComplete, decodeRecord(t, c, rebuildLoopID).State,
		"the rebuilt holder could not write the record it had just read")
	require.Equal(t, natsclient.DeliveryDecisionQuarantine, delivered.Decision(),
		"a completion this component cannot publish is commit-unknown, not retryable")
}

// TestAColdToolResultRebuildsTheBatchItBelongsTo is the tool lane's half, and
// the one that needs the second read: the record says which executions are
// APPLIED and the retained response says how many there were.
//
// spec: agentic-loop / The loop record names its outstanding request
func TestAColdToolResultRebuildsTheBatchItBelongsTo(t *testing.T) {
	requestID := looprequest.ID{LoopID: rebuildLoopID, Iteration: 4, Retry: 0}.String()
	calls := []agentic.ToolCall{
		{ID: "call-applied", Name: "search", Arguments: map[string]any{"q": "one"}},
		{ID: "call-arriving", Name: "fetch", Arguments: map[string]any{"q": "two"}},
		{ID: "call-never-ran", Name: "write", Arguments: map[string]any{"q": "three"}},
	}
	executionOf := func(index int) string {
		return deriveToolExecutionID(requestID, calls[index].ID, uint32(index+1))
	}
	response := &agentic.AgentResponse{
		RequestID: requestID,
		Status:    agentic.StatusToolCall,
		Message:   agentic.ChatMessage{Role: "assistant", Content: "three things", ToolCalls: calls},
	}
	c, h, _ := coldRebuildComponent(t, requestID, response, func(e *agentic.LoopEntity) {
		e.PendingToolResults = map[string]agentic.ToolResult{
			executionOf(0): {
				ExecutionID: executionOf(0), CallID: calls[0].ID,
				Name: "search", Content: "answered before the crash",
			},
		}
	})

	deliverToolResult(t, c, agentic.ToolResult{
		CallID: calls[1].ID, Name: "fetch", Content: "answered after it",
		LoopID: rebuildLoopID, RequestID: requestID,
		ExecutionID: executionOf(1), CallOrdinal: 2,
	})

	entity, err := h.loopManager.GetLoop(rebuildLoopID)
	require.NoError(t, err,
		"the executor's completed work was refused instead of rebuilding the loop it belongs to")
	require.Contains(t, entity.PendingToolResults, executionOf(1),
		"the arriving result never reached the applied set")
	require.Contains(t, entity.PendingToolResults, executionOf(0),
		"the rebuild dropped the results the record already carried")

	require.Equal(t, []string{"user", "assistant"},
		roles(h.loopManager.GetContextManager(rebuildLoopID).GetContext()),
		"without the assistant turn the retained response carries, every tool message "+
			"in this batch is an orphan")

	require.Equal(t, []string{calls[2].ID}, h.loopManager.GetPendingTools(rebuildLoopID),
		"the sibling that never ran must be dispatched next; an applied one must not be re-run")
	_, queued := h.loopManager.DequeueToolCall(rebuildLoopID)
	require.False(t, queued, "the batch's last call is in flight, so nothing is left queued")
}

// TestARebuiltLoopDoesNotReAskForATurnItCannotRecover is the documented
// limitation, asserted.
//
// A continuation admitted while a request was outstanding is durable as a
// MARKER only: PendingContinuation says a turn was admitted, and its TEXT went
// into the predecessor's context manager, which died with it.
// PendingContinuationRequestID is empty precisely because no request ever
// carried it. Seated wholesale by the rebuild, that marker made
// HasPendingContinuation true on a loop with nothing new to say — the next
// completion spent an iteration re-asking the model with a context that had
// gained nothing, and then settled anyway.
//
// So the rebuild clears it with a warning, and the limitation is documented
// where an adopter reads it: the turn must be re-sent. The durable-turn field
// that would recover it is filed as #1365 (owner ruling on #1330 Q2,
// 2026-09-23, answering finding 4 of the owner Codex round on PR #1361).
//
// The completion event's empty Prompt is the SAME limitation, one field over
// (#1330 Q8): taskPrompts is the one per-loop cache the rebuild does not
// restore, because the record has no field to restore it from, so
// LoopCompletedEvent.Prompt, LoopFailedEvent.Prompt and recoverEmptyContext's
// fallback all see it empty after a replacement. It rides #1365 too, and it is
// asserted here so the documented limitation is a tested one.
//
// spec: agentic-loop / The loop record names its outstanding request
func TestARebuiltLoopDoesNotReAskForATurnItCannotRecover(t *testing.T) {
	requestID := looprequest.ID{LoopID: rebuildLoopID, Iteration: 3, Retry: 0}.String()
	// The rebuild's own logger, so the warning the delta's THEN promises has an
	// observer. Without it the clear is silent: a turn an operator was told was
	// accepted is dropped, and the only trace is the field it cleared.
	var logs bytes.Buffer
	handler := NewMessageHandler(DefaultConfig(),
		WithLoopManagerLogger(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn}))))
	record := rebuiltRecord(requestID, func(e *agentic.LoopEntity) {
		e.PendingContinuation = true
		e.PendingContinuationRequestID = ""
	})

	require.NoError(t, handler.loopManager.restoreLoopFromRequest(t.Context(), record,
		retainedRequest(requestID,
			agentic.ChatMessage{Role: "system", Content: "you are a test agent"},
			agentic.ChatMessage{Role: "user", Content: "the original task"})))

	rebuilt, err := handler.loopManager.GetLoop(rebuildLoopID)
	require.NoError(t, err)
	// assert, not require: the marker and the phantom iteration it causes are
	// two independent facts about the same rebuild, and a run that stops at
	// the first reports only half of what broke.
	assert.False(t, rebuilt.PendingContinuation,
		"the rebuilt loop still claims a deferred turn whose text died with the predecessor; the "+
			"next completion will spend an iteration re-asking the model with nothing new")
	// The LINE, not the buffer: the rebuild emits other warnings that carry a
	// loop_id, so a buffer-wide check would report a match this clear never
	// made.
	dropped := logLineContaining(t, logs.String(), "cleared a deferred turn it cannot recover")
	assert.Contains(t, dropped, "loop_id="+rebuildLoopID,
		"the drop is a declared event and must name the loop whose turn it dropped")

	completion, err := handler.HandleModelResponse(t.Context(), rebuildLoopID, agentic.AgentResponse{
		RequestID: requestID,
		Status:    agentic.StatusComplete,
		Message:   agentic.ChatMessage{Role: "assistant", Content: "the task is done"},
	})
	require.NoError(t, err)
	assert.Empty(t, mintedRequestIDsFromResult(t, completion),
		"the rebuilt loop minted another request to re-ask a turn it does not have")
	assert.True(t, completion.State.IsTerminal(),
		"with nothing carryable deferred the completion must settle the loop")
	require.NotNil(t, completion.CompletionState,
		"a settling completion builds its terminal record")
	require.Empty(t, completion.CompletionState.Prompt,
		"a rebuilt loop has no durable task prompt to publish (#1330 Q8, see #1365); an assertion "+
			"that expects one here would be asserting a field the record does not carry")
}

// mintedRequestIDsFromResult returns the RequestID of every agent.request in a
// handler result. The internal-package twin of the external fixture helper: a
// request is minted when one of those messages is appended, whatever the loop
// then does with it.
func mintedRequestIDsFromResult(t *testing.T, result HandlerResult) []string {
	t.Helper()
	var ids []string
	for _, msg := range result.PublishedMessages {
		if msg.MsgID != "" {
			ids = append(ids, msg.MsgID)
		}
	}
	return ids
}

// TestRestoringARecordsDeadlineTouchesOnlyTheTwoFieldsItOwns is the in-place
// half of the cold R1 arm's deadline restore.
//
// The restore used to be a component-level read-modify-write — GetLoop, set the
// two fields, UpdateLoop — and UpdateLoop replaces the WHOLE entity, so
// anything a sibling lane committed between the two calls was discarded. The
// window is reachable: the loop is registered and its first request tracked
// before the restore runs, so the response lane can be applying the
// predecessor's answer to the same loop. It is a lost update, not a data race,
// so -race cannot see it and only the shape can rule it out.
//
// What is asserted is exactly that: the entity the manager holds afterwards is
// the one it held before with two fields changed and nothing else re-rendered.
//
// spec: agentic-loop / The loop record names its outstanding request
func TestRestoringARecordsDeadlineTouchesOnlyTheTwoFieldsItOwns(t *testing.T) {
	manager := NewLoopManager()
	loopID, err := manager.CreateLoop("task-restore-deadline", "general", "test-model", 5)
	require.NoError(t, err)
	require.NoError(t, manager.SetTimeout(loopID, time.Hour))

	// A sibling lane's work, already applied to the held entity.
	require.NoError(t, manager.IncrementIteration(loopID))
	require.NoError(t, manager.SetPublishedRequest(loopID,
		looprequest.ID{LoopID: loopID, Iteration: 2, Retry: 0}.String()))
	before, err := manager.GetLoop(loopID)
	require.NoError(t, err)

	// The record's own timing, as a rebuild reads it: started long ago, with a
	// deadline this loop is already close to.
	recordedStart := time.Now().UTC().Add(-90 * time.Minute)
	recordedTimeout := recordedStart.Add(2 * time.Hour)

	require.NoError(t, manager.restoreDeadline(loopID, recordedStart, recordedTimeout))

	restored, err := manager.GetLoop(loopID)
	require.NoError(t, err)
	want := before
	want.StartedAt = recordedStart
	want.TimeoutAt = recordedTimeout
	assert.Equal(t, want, restored,
		"the restore owns two fields: anything else that differs was re-rendered from a stale read")
}

// logLineContaining returns the one log line carrying substr, or "" with the
// whole buffer reported. Warning-level lines from unrelated paths share the
// buffer, so an assertion made against the buffer as a whole can pass on a
// neighbour's attributes.
func logLineContaining(t *testing.T, logs, substr string) string {
	t.Helper()
	for _, line := range strings.Split(logs, "\n") {
		if strings.Contains(line, substr) {
			return line
		}
	}
	assert.Fail(t, "no log line carries the expected message",
		"expected a line containing %q, logged:\n%s", substr, logs)
	return ""
}
