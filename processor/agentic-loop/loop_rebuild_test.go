package agenticloop

import (
	"testing"

	"github.com/c360studio/semstreams/agentic"
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
			"the retained body IS GetContext()'s order; the rebuild must render it back the same way")

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
