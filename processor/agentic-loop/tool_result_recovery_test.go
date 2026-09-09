package agenticloop

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// spec: agentic-loop / Loop recovery is lane-specific and read-through
// spec: agentic-loop / Tool execution has stable framework correlation
// spec: agentic-loop / Loop task, request, and tool work use only required correlation
func TestColdToolResultOrderedBatchCheckpoints(t *testing.T) {
	for _, tc := range []struct {
		prefix      int
		taskRestore bool
		priorTool   bool
	}{{prefix: 0}, {prefix: 1}, {prefix: 2}, {prefix: 3}, {prefix: 3, taskRestore: true}, {priorTool: true}} {
		prefix := tc.prefix
		t.Run(fmt.Sprintf("durable_prefix_%d/task_restored_%t/prior_tool_%t", prefix, tc.taskRestore, tc.priorTool), func(t *testing.T) {
			loopID := uuid.NewString()
			requestID := loopID + ":req:" + uuid.NewString()
			calls := []agentic.ToolCall{
				{ID: "repeated-provider-id", Name: "search"},
				{ID: "repeated-provider-id", Name: "search"},
				{ID: "repeated-provider-id", Name: "search"},
			}
			require.NoError(t, stampToolExecutionCorrelation(requestID, calls))
			results := make([]agentic.ToolResult, len(calls))
			entity := agentic.NewLoopEntity(loopID, "ordered-task", "general", "model", 20)
			entity.Iterations = 7
			entity.PendingToolResults = make(map[string]agentic.ToolResult)
			for i, call := range calls {
				results[i] = agentic.ToolResult{LoopID: loopID, RequestID: requestID, ExecutionID: call.ExecutionID,
					CallOrdinal: call.CallOrdinal, CallID: call.ID, Name: call.Name, Content: fmt.Sprintf("answer-%d", i+1)}
				if i < prefix {
					entity.PendingToolResults[call.ExecutionID] = results[i]
				}
			}
			if prefix == len(calls) {
				// The durable next-turn state preceded failed request publication.
				entity.Iterations = 8
			}
			if tc.taskRestore {
				entity.MaxIterations = 8 // Replay must not spuriously fail at the charged budget boundary.
			}
			request := &agentic.AgentRequest{LoopID: loopID, RequestID: requestID, Role: entity.Role, Model: entity.Model,
				Messages: []agentic.ChatMessage{{Role: "user", Content: "first question"}, {Role: "assistant", Content: "first answer"}, {Role: "user", Content: "next question"}}}
			if tc.priorTool {
				priorCalls := []agentic.ToolCall{{ID: "distinct-prior-call", Name: "lookup"}}
				require.NoError(t, stampToolExecutionCorrelation(loopID+":req:"+uuid.NewString(), priorCalls))
				request.Messages = []agentic.ChatMessage{
					{Role: "user", Content: "first question"},
					{Role: "assistant", ToolCalls: priorCalls},
					{Role: "tool", ToolCallID: priorCalls[0].ID, Name: priorCalls[0].Name, Content: "prior tool answer"},
					{Role: "assistant", Content: "first answer"},
					{Role: "user", Content: "next question"},
				}
			}
			response := &agentic.AgentResponse{RequestID: requestID, Status: agentic.StatusToolCall,
				Message: agentic.ChatMessage{Role: "assistant", ToolCalls: calls}}
			bucket := &settlementBucket{values: map[string][]byte{loopID: settlementLoopRecord(t, entity)}}
			c := releaseTestComponent(t, NewMessageHandler(DefaultConfig()))
			c.loopsBucket = bucket
			evidence := &settlementEvidence{
				request: retainedLoopMessage{subject: "agent.request." + loopID, data: settlementEnvelope(t, request)}, requestFound: true,
				response: retainedLoopMessage{subject: "agent.response." + requestID, data: settlementEnvelope(t, response)}, responseFound: true,
			}
			c.settlementEvidence = evidence
			if tc.taskRestore {
				// Independent durable lanes may redeliver in either order. Task
				// read-through restores this loop but no execution routing.
				task := &agentic.TaskMessage{LoopID: loopID, TaskID: entity.TaskID, Role: entity.Role, Model: entity.Model, Prompt: "next question"}
				decision, err := c.handleTaskMessage(t.Context(), settlementEnvelope(t, task))
				require.NoError(t, err)
				require.Equal(t, natsclient.DeliveryDecisionAck, decision)
				_, err = c.handler.GetLoop(loopID)
				require.NoError(t, err)
				_, routed := c.handler.loopManager.GetLoopForToolCall(results[2].ExecutionID)
				require.False(t, routed)
			}
			if prefix == 0 {
				before := append([]byte(nil), bucket.values[loopID]...)
				decision, err := c.handleToolResultMessage(t.Context(), settlementEnvelope(t, &results[1]))
				require.Error(t, err)
				require.Equal(t, natsclient.DeliveryDecisionRetry, decision, "missing preceding result must not skip a serial execution")
				conflict := results[0]
				conflict.Name = "other-tool"
				decision, err = c.handleToolResultMessage(t.Context(), settlementEnvelope(t, &conflict))
				require.Error(t, err)
				require.Equal(t, natsclient.DeliveryDecisionQuarantine, decision)
				evidence.requestFound = false
				decision, err = c.handleToolResultMessage(t.Context(), settlementEnvelope(t, &results[0]))
				require.Error(t, err)
				require.Equal(t, natsclient.DeliveryDecisionRetry, decision, "retention absence is not applied proof")
				evidence.requestFound = true
				require.Equal(t, before, bucket.values[loopID], "unresolved or conflicting recovery must not advance durable state")
				_, err = c.handler.GetLoop(loopID)
				require.Error(t, err, "rejected recovery must not install a speculative loop")
			}
			start := max(0, prefix-1)
			var emittedContext []agentic.ChatMessage
			for i := start; i < len(results); i++ {
				if tc.priorTool && i == len(results)-1 {
					// Observe the actual registered next-request envelope from the
					// normal transition owner, not only its context-manager cache.
					transition, err := c.handler.HandleToolResult(t.Context(), loopID, results[i])
					require.NoError(t, err)
					require.NoError(t, c.persistHandlerResult(t.Context(), transition))
					for _, publication := range transition.PublishedMessages {
						decoded, err := c.decoder.Decode(publication.Data)
						require.NoError(t, err)
						if next, ok := decoded.Payload().(*agentic.AgentRequest); ok {
							emittedContext = next.Messages
						}
					}
				} else {
					decision, err := c.handleToolResultMessage(t.Context(), settlementEnvelope(t, &results[i]))
					require.NoError(t, err)
					require.Equal(t, natsclient.DeliveryDecisionAck, decision)
				}
				var durable agentic.LoopEntity
				require.NoError(t, json.Unmarshal(bucket.values[loopID], &durable))
				require.False(t, durable.State.IsTerminal(), "replaying a charged checkpoint must not exhaust the budget again")
				for completed := 0; completed <= i; completed++ {
					require.Equal(t, results[completed], durable.PendingToolResults[calls[completed].ExecutionID])
				}
				if i+1 < len(results) {
					require.Equal(t, uint32(i+2), c.handler.loopManager.GetToolOrdinal(calls[i+1].ExecutionID))
					_, routed := c.handler.loopManager.GetLoopForToolCall(calls[i+1].ExecutionID)
					require.True(t, routed, "next serial execution must be dispatched, despite repeated provider IDs")
				}
			}
			final, err := c.handler.GetLoop(loopID)
			require.NoError(t, err)
			require.Equal(t, 8, final.Iterations, "a replayed full checkpoint must not spend the iteration twice")
			messages := c.handler.loopManager.GetContextManager(loopID).GetContext()
			require.Len(t, messages, len(request.Messages)+1+len(results))
			require.Equal(t, request.Messages, messages[:len(request.Messages)], "complete conversational history must survive replacement in order")
			for i, result := range results {
				require.Equal(t, result.Content, messages[len(request.Messages)+1+i].Content, "results must follow originating call order")
			}
			if tc.priorTool {
				require.NotEmpty(t, emittedContext)
				start := -1
				for i, message := range emittedContext {
					if message.Role == "user" && message.Content == "first question" {
						start = i
						break
					}
				}
				require.GreaterOrEqual(t, start, 0)
				require.Equal(t, messages, emittedContext[start:], "emitted request must contain intact prior and current exchanges in order")
			}
			// A later committed request proves only the result at this execution's
			// ordinal inside the exact originating batch, not any repeated CallID.
			request.RequestID = loopID + ":req:" + uuid.NewString()
			request.Messages = messages
			evidence.request.data = settlementEnvelope(t, request)
			c.releaseLoopTransientState(loopID)
			decision, err := c.handleToolResultMessage(t.Context(), settlementEnvelope(t, &results[1]))
			require.NoError(t, err)
			require.Equal(t, natsclient.DeliveryDecisionAck, decision)
			_, err = c.handler.GetLoop(loopID)
			require.Error(t, err, "applied replay must not restore or mutate the current loop")
			wrong := results[1]
			wrong.Content = results[0].Content
			decision, err = c.handleToolResultMessage(t.Context(), settlementEnvelope(t, &wrong))
			require.Error(t, err)
			require.Equal(t, natsclient.DeliveryDecisionRetry, decision, "same provider ID cannot borrow a sibling's applied proof")
		})
	}
}
