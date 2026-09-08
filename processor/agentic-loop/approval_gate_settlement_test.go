package agenticloop

import (
	"errors"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/stretchr/testify/require"
)

func approvalGateSettlementFixture(t *testing.T) (*Component, *agentic.ToolResult) {
	t.Helper()
	handler := NewMessageHandler(DefaultConfig())
	c := releaseTestComponent(t, handler)
	loopID, err := handler.loopManager.CreateLoop("task-approval-settlement", "general", "model", 3)
	require.NoError(t, err)
	requestID := handler.loopManager.GenerateRequestID(loopID)
	_, err = handler.HandleModelResponse(t.Context(), loopID, agentic.AgentResponse{
		RequestID: requestID, Status: agentic.StatusToolCall,
		Message: agentic.ChatMessage{Role: "assistant", ToolCalls: []agentic.ToolCall{{
			ID: "call-approval", Name: "delete_rule", Arguments: map[string]any{"rule_id": "rule-42"},
		}}},
	})
	require.NoError(t, err)
	c.loopsBucket = &settlementBucket{values: make(map[string][]byte)}
	return c, &agentic.ToolResult{
		LoopID: loopID, RequestID: requestID, ExecutionID: deriveToolExecutionID(requestID, "call-approval", 1),
		CallID: "call-approval", CallOrdinal: 1, Name: "delete_rule",
		ErrorKind: agentic.ToolErrorPermission, Error: agentic.ApprovalRequiredPrefix + "human approval required",
	}
}

// spec: agentic-loop / All six loop input classes settle after owner-specific durable done
func TestApprovalGateConstructionFailureDoesNotReportSuccessfulWait(t *testing.T) {
	c, toolResult := approvalGateSettlementFixture(t)
	c.handler.config.Ports.Outputs = nil
	before, err := c.handler.GetLoop(toolResult.LoopID)
	require.NoError(t, err)

	result, err := c.handler.HandleToolResult(t.Context(), toolResult.LoopID, *toolResult)

	require.ErrorContains(t, err, `port name "agent.approval_pending" not found`)
	require.Equal(t, before.State, result.State, "failed gate construction must not report a successful approval wait")
	require.Empty(t, result.PublishedMessages)
}

// spec: agentic-loop / Approval continuation after replacement is exact and evidence-bounded
// spec: agentic-loop / All six loop input classes settle after owner-specific durable done
func TestApprovalGateRetainsRequiredResultAndSuccessfulWait(t *testing.T) {
	c, toolResult := approvalGateSettlementFixture(t)

	result, err := c.handler.HandleToolResult(t.Context(), toolResult.LoopID, *toolResult)

	require.NoError(t, err)
	require.Equal(t, agentic.LoopStateAwaitingApproval, result.State)
	require.Len(t, result.PublishedMessages, 1)
	baseMsg, err := c.decoder.Decode(result.PublishedMessages[0].Data)
	require.NoError(t, err)
	pending, ok := baseMsg.Payload().(*agentic.ApprovalPendingEvent)
	require.True(t, ok)
	require.Equal(t, toolResult.LoopID, pending.LoopID)
	require.Equal(t, toolResult.CallID, pending.CallID)

	entity, err := c.handler.GetLoop(toolResult.LoopID)
	require.NoError(t, err)
	require.NotNil(t, entity.PendingApproval)
	require.Equal(t, toolResult.ExecutionID, entity.PendingApproval.ExecutionID)
	require.Equal(t, *toolResult, entity.PendingToolResults[toolResult.ExecutionID],
		"awaiting approval must retain the result required before source ACK")

	// An already established wait still absorbs later results without advancing.
	result, err = c.handler.HandleToolResult(t.Context(), toolResult.LoopID, *toolResult)
	require.NoError(t, err)
	require.Equal(t, agentic.LoopStateAwaitingApproval, result.State)
	require.Empty(t, result.PublishedMessages)
}

// spec: agentic-loop / All six loop input classes settle after owner-specific durable done
func TestApprovalGateFailureUsesExistingDeliveryClassification(t *testing.T) {
	for _, failure := range []string{"missing output", "invalid arguments"} {
		t.Run(failure, func(t *testing.T) {
			c, toolResult := approvalGateSettlementFixture(t)
			if failure == "missing output" {
				c.handler.config.Ports.Outputs = nil
			} else {
				args := c.handler.loopManager.GetToolArguments(toolResult.ExecutionID)
				args["invalid"] = make(chan struct{})
				c.handler.loopManager.TrackToolArguments(toolResult.ExecutionID, args)
			}
			msg := &loopDeliveryOwnerMsg{data: settlementEnvelope(t, toolResult)}

			result, admitted := consumeAdmittedDelivery(t.Context(), msg,
				task4HeartbeatPolicy(t, "tool.result", c.handleToolResultMessage), newDeliveryLaneAdmission(nil))

			require.True(t, admitted)
			require.Equal(t, natsclient.DeliveryDecisionTerminate, result.Decision())
			require.True(t, errs.IsInvalid(result.Err()), "existing invalid classification was lost: %v", result.Err())
			require.Zero(t, msg.acks.Load())
			require.Equal(t, int32(1), msg.terms.Load())
			require.Zero(t, msg.naks.Load())
			require.Empty(t, c.loopsBucket.(*settlementBucket).values, "failed setup must not persist successful approval state")
			_, err := c.handler.GetLoop(toolResult.LoopID)
			require.Error(t, err, "failed setup must discard speculative process state")
		})
	}
}

// spec: agentic-loop / All six loop input classes settle after owner-specific durable done
func TestApprovalGateDurableFailureCannotAck(t *testing.T) {
	for _, failure := range []string{"loop state", "pending publication"} {
		t.Run(failure, func(t *testing.T) {
			c, toolResult := approvalGateSettlementFixture(t)
			if failure == "loop state" {
				c.loopsBucket = failingLoopBucket{err: errors.New("approval state unavailable")}
			} else {
				c.natsClient = &natsclient.Client{}
			}
			msg := &loopDeliveryOwnerMsg{data: settlementEnvelope(t, toolResult)}

			result, admitted := consumeAdmittedDelivery(t.Context(), msg,
				task4HeartbeatPolicy(t, "tool.result", c.handleToolResultMessage), newDeliveryLaneAdmission(nil))

			require.True(t, admitted)
			require.Equal(t, natsclient.DeliveryDecisionRetry, result.Decision())
			require.Error(t, result.Err())
			require.Zero(t, msg.acks.Load()+msg.terms.Load())
			require.Equal(t, int32(1), msg.naks.Load())
			_, err := c.handler.GetLoop(toolResult.LoopID)
			require.Error(t, err, "failed durability must discard speculative process state")
		})
	}
}

// spec: agentic-loop / All six loop input classes settle after owner-specific durable done
func TestApprovalGateAfterUnsettledCancellationCannotAck(t *testing.T) {
	c, toolResult := approvalGateSettlementFixture(t)
	c.natsClient = &natsclient.Client{}
	signal := &agentic.UserSignal{
		SignalID: "cancel-approval", Type: agentic.SignalCancel, LoopID: toolResult.LoopID,
		UserID: "user-approval", ChannelType: "test", ChannelID: "approval-channel",
	}
	cancelDecision, err := c.handleSignalMessage(t.Context(), settlementEnvelope(t, signal))
	require.ErrorContains(t, err, "cancellation completion has unknown durability")
	require.Equal(t, natsclient.DeliveryDecisionQuarantine, cancelDecision)
	entity, err := c.handler.GetLoop(toolResult.LoopID)
	require.NoError(t, err)
	require.Equal(t, agentic.LoopStateCancelled, entity.State)
	msg := &loopDeliveryOwnerMsg{data: settlementEnvelope(t, toolResult)}

	result, admitted := consumeAdmittedDelivery(t.Context(), msg,
		task4HeartbeatPolicy(t, "tool.result", c.handleToolResultMessage), newDeliveryLaneAdmission(nil))

	require.True(t, admitted)
	require.Zero(t, msg.acks.Load(), "terminal state alone does not prove the approval-required result applied")
	require.ErrorContains(t, result.Err(), "cannot begin awaiting approval from terminal state")
}

// spec: agentic-loop / All six loop input classes settle after owner-specific durable done
func TestToolTimeoutWithoutConstructedFailureCannotAck(t *testing.T) {
	c, toolResult := approvalGateSettlementFixture(t)
	entity, err := c.handler.GetLoop(toolResult.LoopID)
	require.NoError(t, err)
	entity.TimeoutAt = time.Now().Add(-time.Second)
	require.NoError(t, c.handler.UpdateLoop(entity))
	c.handler.config.Ports.Outputs = nil
	msg := &loopDeliveryOwnerMsg{data: settlementEnvelope(t, toolResult)}

	result, admitted := consumeAdmittedDelivery(t.Context(), msg,
		task4HeartbeatPolicy(t, "tool.result", c.handleToolResultMessage), newDeliveryLaneAdmission(nil))

	require.True(t, admitted)
	require.Equal(t, natsclient.DeliveryDecisionQuarantine, result.Decision())
	require.ErrorContains(t, result.Err(), "loop timeout exceeded")
	require.Zero(t, msg.settlement.Load(), "failed terminal construction must not settle its source")
	require.Empty(t, c.loopsBucket.(*settlementBucket).values)
}
