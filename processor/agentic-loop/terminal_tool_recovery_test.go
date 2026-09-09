package agenticloop

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// spec: agentic-loop / Loop recovery is lane-specific and read-through
// spec: agentic-loop / Loop task, request, and tool work use only required correlation
// spec: agentic-loop / All six loop input classes settle after owner-specific durable done
func TestColdTerminalToolResultRequiresExactAppliedEvidence(t *testing.T) {
	for _, stopLoop := range []bool{true, false} {
		name := "max_iterations"
		if stopLoop {
			name = "stop_loop"
		}
		t.Run(name, func(t *testing.T) {
			loopID := uuid.NewString()
			requestID := loopID + ":req:" + uuid.NewString()
			calls := []agentic.ToolCall{{ID: "repeated", Name: "search"}, {ID: "repeated", Name: "search"}}
			require.NoError(t, stampToolExecutionCorrelation(requestID, calls))
			results := make([]agentic.ToolResult, len(calls))
			for i, call := range calls {
				results[i] = agentic.ToolResult{LoopID: loopID, RequestID: requestID, ExecutionID: call.ExecutionID,
					CallID: call.ID, CallOrdinal: call.CallOrdinal, Name: call.Name, TraceID: "trace", Content: "answer"}
			}
			results[1].StopLoop = stopLoop
			entity := agentic.NewLoopEntity(loopID, "terminal-tool-task", "general", "model", 1)
			entity.State, entity.Iterations = agentic.LoopStateExecuting, entity.MaxIterations
			bucket := &settlementBucket{values: map[string][]byte{loopID: settlementLoopRecord(t, entity)}}
			response := &agentic.AgentResponse{RequestID: requestID, Status: agentic.StatusToolCall,
				Message: agentic.ChatMessage{Role: "assistant", ToolCalls: calls}}
			evidence := &settlementEvidence{request: retainedRequest(t, loopID, requestID), requestFound: true,
				response: retainedLoopMessage{subject: "agent.response." + requestID, data: settlementEnvelope(t, response)}, responseFound: true}
			c := releaseTestComponent(t, NewMessageHandler(DefaultConfig()))
			c.loopsBucket, c.settlementEvidence = bucket, evidence
			decision, err := c.handleToolResultMessage(t.Context(), settlementEnvelope(t, &results[0]))
			require.NoError(t, err)
			require.Equal(t, natsclient.DeliveryDecisionAck, decision)
			before := append([]byte(nil), bucket.values[loopID]...)
			// The terminal input must retry from the prior durable prefix if the
			// final marker fails, even though COMPLETE_ already committed.
			bucket.failPutKey, bucket.failPutLeft = loopID, 1
			wire := settlementEnvelope(t, &results[1])
			decision, err = c.handleToolResultMessage(t.Context(), wire)
			require.Error(t, err)
			require.Equal(t, natsclient.DeliveryDecisionRetry, decision)
			require.Equal(t, before, bucket.values[loopID])
			require.Contains(t, bucket.values, "COMPLETE_"+loopID)
			_, err = c.handler.GetLoop(loopID)
			require.Error(t, err, "failed marker must discard speculative terminal state")
			decision, err = c.handleToolResultMessage(t.Context(), wire)
			require.NoError(t, err)
			require.Equal(t, natsclient.DeliveryDecisionAck, decision)
			var terminal agentic.LoopEntity
			require.NoError(t, json.Unmarshal(bucket.values[loopID], &terminal))
			require.Equal(t, entity.MaxIterations, terminal.Iterations)
			require.Equal(t, results[1], terminal.PendingToolResults[results[1].ExecutionID])
			if stopLoop {
				require.Equal(t, agentic.LoopStateComplete, terminal.State)
				require.Equal(t, agentic.OutcomeSuccess, terminal.Outcome)
				require.Equal(t, results[1].Content, terminal.Result)
			} else {
				require.Equal(t, agentic.LoopStateFailed, terminal.State)
				require.Equal(t, agentic.OutcomeFailed, terminal.Outcome)
				require.Len(t, terminal.PendingToolResults, len(calls))
				var failure agentic.LoopFailedEvent
				require.NoError(t, json.Unmarshal(bucket.values["COMPLETE_"+loopID], &failure))
				require.Equal(t, "max_iterations", failure.Reason)
			}

			final := append([]byte(nil), bucket.values[loopID]...)
			replacement := releaseTestComponent(t, NewMessageHandler(DefaultConfig()))
			replacement.loopsBucket, replacement.settlementEvidence = bucket, evidence
			// Any attempted publication now fails; applied replay must perform
			// neither publication nor another terminal marker write.
			replacement.natsClient = &natsclient.Client{}
			decision, err = replacement.handleToolResultMessage(t.Context(), wire)
			require.NoError(t, err)
			require.Equal(t, natsclient.DeliveryDecisionAck, decision)
			require.Equal(t, final, bucket.values[loopID])
			_, err = replacement.handler.GetLoop(loopID)
			require.Error(t, err, "proven terminal replay must not reinstall process state")

			if !stopLoop {
				t.Run("timeout at cap is not max iteration proof", func(t *testing.T) {
					// Response-lane recovery can see a full pre-publication tool
					// checkpoint at the cap, then time out before handling its
					// response. That terminal is not the tool-drain consequence.
					var checkpoint agentic.LoopEntity
					require.NoError(t, json.Unmarshal(final, &checkpoint))
					checkpoint.State, checkpoint.Outcome, checkpoint.Error = agentic.LoopStateExecuting, "", ""
					checkpoint.CompletedAt = time.Time{}
					checkpoint.TimeoutAt = time.Now().Add(-time.Second)
					bucket.values[loopID] = settlementLoopRecord(t, checkpoint)
					timeoutOwner := releaseTestComponent(t, NewMessageHandler(DefaultConfig()))
					timeoutOwner.loopsBucket, timeoutOwner.settlementEvidence = bucket, evidence
					decision, err := timeoutOwner.handleResponseMessage(t.Context(), settlementEnvelope(t, response))
					require.NoError(t, err)
					require.Equal(t, natsclient.DeliveryDecisionAck, decision)
					var timedOut agentic.LoopEntity
					require.NoError(t, json.Unmarshal(bucket.values[loopID], &timedOut))
					require.Equal(t, agentic.LoopStateFailed, timedOut.State)
					require.Equal(t, "loop timeout exceeded", timedOut.Error)
					require.Equal(t, checkpoint.MaxIterations, timedOut.Iterations)
					require.Len(t, timedOut.PendingToolResults, len(calls))
					decision, err = replacement.handleToolResultMessage(t.Context(), wire)
					require.Equal(t, natsclient.DeliveryDecisionRetry, decision, "a timeout-at-cap marker is not the max-iteration consequence")
					require.Error(t, err)
				})
			}

			for _, tc := range []struct {
				name   string
				change func(*agentic.LoopEntity)
				want   natsclient.DeliveryDecision
			}{
				{name: "missing exact result", change: func(e *agentic.LoopEntity) { delete(e.PendingToolResults, results[1].ExecutionID) }, want: natsclient.DeliveryDecisionRetry},
				{name: "missing batch prefix", change: func(e *agentic.LoopEntity) { delete(e.PendingToolResults, results[0].ExecutionID) }, want: natsclient.DeliveryDecisionRetry},
				{name: "missing terminal consequence", change: func(e *agentic.LoopEntity) {
					if stopLoop {
						e.Result = "another terminal result"
					} else {
						e.Iterations--
					}
				}, want: natsclient.DeliveryDecisionRetry},
				{name: "approval remains unresolved", change: func(e *agentic.LoopEntity) {
					e.PendingApproval = &agentic.PendingApprovalState{ExecutionID: results[1].ExecutionID}
				}, want: natsclient.DeliveryDecisionRetry},
				{name: "conflicting exact result", change: func(e *agentic.LoopEntity) {
					r := e.PendingToolResults[results[1].ExecutionID]
					r.Content = "different"
					e.PendingToolResults[results[1].ExecutionID] = r
				}, want: natsclient.DeliveryDecisionQuarantine},
				{name: "conflicting ordinal", change: func(e *agentic.LoopEntity) {
					r := e.PendingToolResults[results[1].ExecutionID]
					r.CallOrdinal = 1
					e.PendingToolResults[results[1].ExecutionID] = r
				}, want: natsclient.DeliveryDecisionQuarantine},
			} {
				t.Run(tc.name, func(t *testing.T) {
					var changed agentic.LoopEntity
					require.NoError(t, json.Unmarshal(final, &changed))
					tc.change(&changed)
					bucket.values[loopID] = settlementLoopRecord(t, changed)
					decision, err := replacement.handleToolResultMessage(t.Context(), wire)
					require.Error(t, err)
					require.Equal(t, tc.want, decision)
					require.Equal(t, settlementLoopRecord(t, changed), bucket.values[loopID])
				})
			}
		})
	}
}
