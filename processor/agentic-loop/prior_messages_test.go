package agenticloop

import (
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/payloadbuiltins"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func historyTask() agentic.TaskMessage {
	return agentic.TaskMessage{
		LoopID: uuid.NewString(), TaskID: "history-task", Role: "general", Model: "model", Prompt: "What number?",
		PriorMessages: []agentic.ChatMessage{{Role: "user", Content: "Remember 42."}, {Role: "assistant", Content: "I will remember 42."}},
		Context:       &agentic.ConstructedContext{Content: "current embedded context"},
	}
}

func historyRequest(t *testing.T, result HandlerResult) agentic.AgentRequest {
	t.Helper()
	decoder := payloadbuiltins.NewTestDecoder(t)
	for _, out := range result.PublishedMessages {
		decoded, err := decoder.Decode(out.Data)
		require.NoError(t, err)
		if request, ok := decoded.Payload().(*agentic.AgentRequest); ok {
			require.NoError(t, request.Validate())
			return *request
		}
	}
	t.Fatal("missing registered agent request")
	return agentic.AgentRequest{}
}

func requireHistoryOnce(t *testing.T, messages []agentic.ChatMessage, task agentic.TaskMessage) {
	t.Helper()
	expected := append(append([]agentic.ChatMessage(nil), task.PriorMessages...), agentic.ChatMessage{Role: "user", Content: task.Prompt})
	var conversational []agentic.ChatMessage
	for _, msg := range messages {
		if msg.Role == "user" || (msg.Role == "assistant" && len(msg.ToolCalls) == 0) {
			conversational = append(conversational, msg)
		}
	}
	require.Equal(t, expected, conversational)
}

// spec: agentic-loop / A task carries its prior conversational input
func TestPriorMessagesInitialAndToolIteration(t *testing.T) {
	h := fenceHandler(t)
	task := historyTask()
	result, err := h.HandleTask(t.Context(), task)
	require.NoError(t, err)
	request := historyRequest(t, result)
	requireHistoryOnce(t, request.Messages, task)
	require.Contains(t, request.Messages[0].Content, "Iteration 1 of")
	require.Less(t, indexOfContent(request.Messages, fenceSystemFragment), indexOfContent(request.Messages, task.PriorMessages[0].Content))
	require.Less(t, indexOfContent(request.Messages, "[Context]\n"+task.Context.Content), indexOfContent(request.Messages, task.PriorMessages[0].Content))
	requireHistoryOnce(t, h.loopManager.GetContextManager(task.LoopID).GetContext(), task)

	_, err = h.HandleModelResponse(t.Context(), task.LoopID, agentic.AgentResponse{
		RequestID: request.RequestID, Status: agentic.StatusToolCall,
		Message: agentic.ChatMessage{Role: "assistant", ToolCalls: []agentic.ToolCall{{ID: "call", Name: "lookup"}}},
	})
	require.NoError(t, err)
	result, err = h.HandleToolResult(t.Context(), task.LoopID, agentic.ToolResult{
		LoopID: task.LoopID, RequestID: request.RequestID, ExecutionID: deriveToolExecutionID(request.RequestID, "call", 1),
		CallID: "call", CallOrdinal: 1, Name: "lookup", Content: "tool result",
	})
	require.NoError(t, err)
	next := historyRequest(t, result)
	requireHistoryOnce(t, next.Messages, task)
	require.Contains(t, next.Messages[0].Content, "[Iteration Budget]")
	require.Contains(t, next.Messages[0].Content, "of 20")
	require.NotEqual(t, request.RequestID, next.RequestID)
}

// spec: agentic-loop / A task carries its prior conversational input
func TestPriorMessagesColdReconstructionAndRestoration(t *testing.T) {
	for _, retained := range []bool{false, true} {
		t.Run(map[bool]string{false: "absent request", true: "retained request"}[retained], func(t *testing.T) {
			task := historyTask()
			entity := agentic.NewLoopEntity(task.LoopID, task.TaskID, task.Role, task.Model, 4)
			c := releaseTestComponent(t, fenceHandler(t))
			c.loopsBucket = &settlementBucket{values: map[string][]byte{task.LoopID: settlementLoopRecord(t, entity)}}
			evidence := &settlementEvidence{}
			c.settlementEvidence = evidence
			var original agentic.AgentRequest
			if retained {
				first, err := fenceHandler(t).HandleTask(t.Context(), task)
				require.NoError(t, err)
				original = historyRequest(t, first)
				evidence.request = retainedLoopMessage{subject: "agent.request." + task.LoopID, data: settlementEnvelope(t, &original)}
				evidence.requestFound = true
			}
			result, err := c.recoverTaskDelivery(t.Context(), task)
			require.NoError(t, err)
			request := historyRequest(t, result)
			requireHistoryOnce(t, request.Messages, task)
			requireHistoryOnce(t, c.handler.loopManager.GetContextManager(task.LoopID).GetContext(), task)
			if retained {
				require.Equal(t, original, request, "matching request restoration must reuse rather than reseed")
			}
		})
	}
}

// spec: agentic-loop / A task carries its prior conversational input
func TestPriorMessagesCannotAttachToDifferentTask(t *testing.T) {
	h := fenceHandler(t)
	original := historyTask()
	_, err := h.HandleTask(t.Context(), original)
	require.NoError(t, err)
	before, err := h.GetLoop(original.LoopID)
	require.NoError(t, err)
	contextBefore := h.loopManager.GetContextManager(original.LoopID).GetContext()
	next := original
	next.TaskID = "different-task"
	next.Prompt = "new question"
	result, err := h.HandleTask(t.Context(), next)
	require.True(t, errs.IsInvalid(err), "history-bearing attachment must be invalid: %v", err)
	require.ErrorContains(t, err, "prior_messages")
	require.Empty(t, result.PublishedMessages)
	after, err := h.GetLoop(original.LoopID)
	require.NoError(t, err)
	require.Equal(t, before, after)
	require.Equal(t, contextBefore, h.loopManager.GetContextManager(original.LoopID).GetContext())
}
