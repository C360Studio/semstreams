package agenticloop

import (
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/metric"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/processor/agentic-loop/internal/looprequest"
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

		require.NoError(t, manager.restoreLoopFromRequest(record, request))

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

		require.NoError(t, manager.restoreLoopFromRequest(rebuiltRecord(requestID, nil), request))

		require.Equal(t, []string{"system", "user"}, roles(manager.GetContextManager(rebuildLoopID).GetContext()),
			"a request retained mid-batch carries an assistant tool_call with no tool message, and a "+
				"provider refuses that pair outright")
	})

	t.Run("the retained request must be the one the record names", func(t *testing.T) {
		manager := NewLoopManager()
		older := looprequest.ID{LoopID: rebuildLoopID, Iteration: 2, Retry: 0}.String()

		err := manager.restoreLoopFromRequest(rebuiltRecord(requestID, nil),
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
		require.NoError(t, manager.restoreLoopFromRequest(
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
		require.NoError(t, manager.restoreLoopFromRequest(
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

		err = manager.restoreLoopFromRequest(rebuiltRecord(requestID, nil),
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
	require.NoError(t, manager.restoreLoopFromRequest(record,
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
// this process" and retried — to MaxDeliver and then to the dead-letter,
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
