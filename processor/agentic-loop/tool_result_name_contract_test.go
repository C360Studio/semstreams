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

// spec: agentic-tools / Tool-result bounds SHALL be observed rather than predicted
// spec: agentic-loop / Loop recovery is lane-specific and read-through
// spec: agentic-loop / Loop task, request, and tool work use only required correlation
func TestToolResultOptionalNameUsesDispatchedCall(t *testing.T) {
	for _, warm := range []bool{true, false} {
		for _, tc := range []struct {
			name       string
			resultName string
			resultErr  string
			errorKind  agentic.ToolErrorKind
			conflict   bool
		}{
			{name: "compact_too_large", resultErr: "too_large", errorKind: agentic.ToolErrorInternal},
			{name: "terminal_admission_rejection", resultErr: "tool rejected", errorKind: agentic.ToolErrorPermission},
			{name: "matching_name", resultName: "search"},
			{name: "conflicting_name", resultName: "other-tool", conflict: true},
		} {
			t.Run(fmt.Sprintf("warm_%t/%s", warm, tc.name), func(t *testing.T) {
				loopID := uuid.NewString()
				requestID := loopID + ":req:" + uuid.NewString()
				calls := []agentic.ToolCall{{ID: "provider-call", Name: "search"}}
				require.NoError(t, stampToolExecutionCorrelation(requestID, calls))
				call := calls[0]
				entity := agentic.NewLoopEntity(loopID, "name-contract-task", "general", "model", 20)
				entity.Iterations = 7
				request := agentic.AgentRequest{
					LoopID: loopID, RequestID: requestID, Role: entity.Role, Model: entity.Model,
					Messages: []agentic.ChatMessage{{Role: "user", Content: "look it up"}},
				}
				response := agentic.AgentResponse{RequestID: requestID, Status: agentic.StatusToolCall,
					Message: agentic.ChatMessage{Role: "assistant", ToolCalls: calls}}
				// These are the registered wire shapes emitted by compactTooLargeResult
				// and terminal tool admission rejection, not calls to those private
				// producer functions. Both preserve execution correlation and omit Name.
				incoming := agentic.ToolResult{
					LoopID: loopID, RequestID: requestID, ExecutionID: call.ExecutionID,
					CallOrdinal: call.CallOrdinal, CallID: call.ID, Name: tc.resultName,
					Error: tc.resultErr, ErrorKind: tc.errorKind,
				}
				require.NoError(t, incoming.Validate(), "the payload contract admits omitted Name")
				data := settlementEnvelope(t, &incoming)
				bucket := &settlementBucket{values: map[string][]byte{loopID: settlementLoopRecord(t, entity)}}
				evidence := &settlementEvidence{
					request: retainedLoopMessage{subject: "agent.request." + loopID,
						data: settlementEnvelope(t, &request)}, requestFound: true,
					response: retainedLoopMessage{subject: "agent.response." + requestID,
						data: settlementEnvelope(t, &response)}, responseFound: true,
				}
				c := releaseTestComponent(t, NewMessageHandler(DefaultConfig()))
				c.loopsBucket, c.settlementEvidence = bucket, evidence
				decoded, err := c.decoder.Decode(data)
				require.NoError(t, err)
				require.Equal(t, tc.resultName, decoded.Payload().(*agentic.ToolResult).Name)
				if warm {
					// Seed the same existing process owner that cold read-through
					// reconstructs; the delivered result has not been applied.
					require.NoError(t, c.handler.loopManager.restoreToolBatch(entity, request, response, incoming))
					_, err := c.handler.trajectoryManager.startTrajectory(loopID)
					require.NoError(t, err)
				}
				before := append([]byte(nil), bucket.values[loopID]...)
				decision, err := c.handleToolResultMessage(t.Context(), data)
				if tc.conflict {
					require.Error(t, err)
					require.Equal(t, natsclient.DeliveryDecisionQuarantine, decision)
					require.Equal(t, before, bucket.values[loopID], "a present conflicting name must not advance durable state")
					if warm {
						current, err := c.handler.GetLoop(loopID)
						require.NoError(t, err)
						require.Empty(t, current.PendingToolResults)
						require.Equal(t, entity.Iterations, current.Iterations)
					} else {
						_, err := c.handler.GetLoop(loopID)
						require.Error(t, err, "conflicting cold input must not install process state")
					}
					return
				}
				require.NoError(t, err)
				require.Equal(t, natsclient.DeliveryDecisionAck, decision)
				var durable agentic.LoopEntity
				require.NoError(t, json.Unmarshal(bucket.values[loopID], &durable))
				want := incoming
				want.Name = call.Name
				require.Equal(t, want, durable.PendingToolResults[call.ExecutionID], "durable batch must keep the authoritative call name")
				require.Equal(t, 8, durable.Iterations)
				messages := c.handler.loopManager.GetContextManager(loopID).GetContext()
				require.Len(t, messages, 3)
				require.Equal(t, request.Messages[0], messages[0])
				require.Equal(t, response.Message, messages[1])
				require.Equal(t, "tool", messages[2].Role)
				require.Equal(t, call.ID, messages[2].ToolCallID)
				require.Equal(t, call.Name, messages[2].Name, "the next model exchange must not depend on an optional result name")
				require.Equal(t, tc.resultErr != "", messages[2].IsError)

				// Unit owner proof: the result checkpoint exists but the current
				// retained request has not advanced. No native PubAck is claimed.
				c = releaseTestComponent(t, NewMessageHandler(DefaultConfig()))
				c.loopsBucket, c.settlementEvidence = bucket, evidence
				decision, err = c.handleToolResultMessage(t.Context(), data)
				require.NoError(t, err)
				require.Equal(t, natsclient.DeliveryDecisionAck, decision)
				require.NoError(t, json.Unmarshal(bucket.values[loopID], &durable))
				require.Equal(t, want, durable.PendingToolResults[call.ExecutionID])
				require.Equal(t, 8, durable.Iterations, "replacement must reuse the saved result without charging the iteration twice")
				require.Equal(t, messages, c.handler.loopManager.GetContextManager(loopID).GetContext())

				// A seeded later request supplies the existing applied-proof seam.
				// Name omission in the original bytes must not invalidate that proof.
				request.RequestID = loopID + ":req:" + uuid.NewString()
				request.Messages = messages
				evidence.request.data = settlementEnvelope(t, &request)
				before = append([]byte(nil), bucket.values[loopID]...)
				c = releaseTestComponent(t, NewMessageHandler(DefaultConfig()))
				c.loopsBucket, c.settlementEvidence = bucket, evidence
				decision, err = c.handleToolResultMessage(t.Context(), data)
				require.NoError(t, err)
				require.Equal(t, natsclient.DeliveryDecisionAck, decision)
				require.Equal(t, before, bucket.values[loopID])
				_, err = c.handler.GetLoop(loopID)
				require.Error(t, err, "already-applied input must not install speculative process state")
			})
		}
	}
}
