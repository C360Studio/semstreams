package agenticloop

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// spec: agentic-model / Model request settlement is bound to a durable response
func TestRequestIDsDistinguishLogicalProviderWork(t *testing.T) {
	manager := NewLoopManager()
	loopID := uuid.NewString()

	first := manager.GenerateRequestID(loopID)
	second := manager.GenerateRequestID(loopID)

	require.NotEqual(t, first, second)
	require.Equal(t, loopID, manager.ExtractLoopIDFromRequest(first))
	require.Equal(t, loopID, manager.ExtractLoopIDFromRequest(second))
	for _, requestID := range []string{first, second} {
		suffix := strings.TrimPrefix(requestID, loopID+":req:")
		_, err := uuid.Parse(suffix)
		require.NoError(t, err, "RequestID must retain a full collision-resistant request token")
	}
}

// spec: agentic-loop / Tool execution has stable framework correlation
func TestToolExecutionIdentitySeparatesRepeatedProviderCallID(t *testing.T) {
	first := []agentic.ToolCall{{ID: "provider-call", Name: "lookup"}}
	firstRedelivery := []agentic.ToolCall{{ID: "provider-call", Name: "lookup"}}
	second := []agentic.ToolCall{{ID: "provider-call", Name: "lookup"}}
	repeatedInRequest := []agentic.ToolCall{
		{ID: "provider-call", Name: "lookup"},
		{ID: "provider-call", Name: "lookup"},
	}

	require.NoError(t, stampToolExecutionCorrelation("request-a", first))
	require.NoError(t, stampToolExecutionCorrelation("request-a", firstRedelivery))
	require.NoError(t, stampToolExecutionCorrelation("request-b", second))
	require.NoError(t, stampToolExecutionCorrelation("request-a", repeatedInRequest))

	require.Equal(t, "provider-call", first[0].ID, "provider CallID remains conversation data")
	require.Equal(t, "provider-call", second[0].ID, "provider CallID remains conversation data")
	require.Equal(t, "request-a", first[0].RequestID)
	require.Equal(t, uint32(1), first[0].CallOrdinal)
	require.NotEmpty(t, first[0].ExecutionID)
	require.Equal(t, first[0].ExecutionID, firstRedelivery[0].ExecutionID, "redelivery retains execution identity")
	require.NotEqual(t, first[0].ExecutionID, second[0].ExecutionID)
	require.Equal(t, uint32(2), repeatedInRequest[1].CallOrdinal)
	require.NotEqual(t, repeatedInRequest[0].ExecutionID, repeatedInRequest[1].ExecutionID)
}

// spec: agentic-loop / Tool execution has stable framework correlation
func TestToolResultRoutingSeparatesRepeatedProviderCallIDAcrossLoops(t *testing.T) {
	handler := NewMessageHandler(DefaultConfig())
	loopA, err := handler.loopManager.CreateLoop("task-a", "general", "model")
	require.NoError(t, err)
	loopB, err := handler.loopManager.CreateLoop("task-b", "general", "model")
	require.NoError(t, err)

	callsA := []agentic.ToolCall{{ID: "provider-call", Name: "lookup"}}
	callsB := []agentic.ToolCall{{ID: "provider-call", Name: "lookup"}}
	require.NoError(t, stampToolExecutionCorrelation("request-a", callsA))
	require.NoError(t, stampToolExecutionCorrelation("request-b", callsB))
	require.NoError(t, handler.dispatchToolCall(&HandlerResult{}, loopA, callsA[0]))
	require.NoError(t, handler.dispatchToolCall(&HandlerResult{}, loopB, callsB[0]))

	component := &Component{handler: handler}
	require.Equal(t, loopA, component.findLoopIDForToolCall(callsA[0].ExecutionID))
	require.Equal(t, loopB, component.findLoopIDForToolCall(callsB[0].ExecutionID))
	require.NotEqual(t, callsA[0].ExecutionID, callsB[0].ExecutionID)
}

// spec: agentic-loop / Tool execution has stable framework correlation
func TestApprovalRedispatchSeparatesRepeatedProviderCallIDAcrossLoops(t *testing.T) {
	handler := NewMessageHandler(DefaultConfig())
	loopA, err := handler.loopManager.CreateLoop("task-a", "general", "model")
	require.NoError(t, err)
	loopB, err := handler.loopManager.CreateLoop("task-b", "general", "model")
	require.NoError(t, err)

	callsA := []agentic.ToolCall{{ID: "provider-call", Name: "delete-a", Arguments: map[string]any{"target": "a"}}}
	callsB := []agentic.ToolCall{{ID: "provider-call", Name: "delete-b", Arguments: map[string]any{"target": "b"}}}
	require.NoError(t, stampToolExecutionCorrelation("request-a", callsA))
	require.NoError(t, stampToolExecutionCorrelation("request-b", callsB))
	require.NoError(t, handler.dispatchToolCall(&HandlerResult{}, loopA, callsA[0]))
	require.NoError(t, handler.dispatchToolCall(&HandlerResult{}, loopB, callsB[0]))

	entityA, err := handler.loopManager.GetLoop(loopA)
	require.NoError(t, err)
	_, err = handler.gateForApproval(loopA, &entityA, agentic.ToolResult{
		CallID: callsA[0].ID, RequestID: callsA[0].RequestID, ExecutionID: callsA[0].ExecutionID,
		CallOrdinal: callsA[0].CallOrdinal, Error: agentic.ApprovalRequiredPrefix + "review",
	})
	require.NoError(t, err)

	entityB, err := handler.loopManager.GetLoop(loopB)
	require.NoError(t, err)
	_, err = handler.gateForApproval(loopB, &entityB, agentic.ToolResult{
		CallID: callsB[0].ID, RequestID: callsB[0].RequestID, ExecutionID: callsB[0].ExecutionID,
		CallOrdinal: callsB[0].CallOrdinal, Error: agentic.ApprovalRequiredPrefix + "review",
	})
	require.NoError(t, err)

	approved, err := handler.HandleApprovalResponse(context.Background(), agentic.ApprovalResponse{
		LoopID: loopA, CallID: callsA[0].ID, Decision: agentic.ApprovalDecisionApprove, ApprovedBy: "reviewer",
	})
	require.NoError(t, err)
	require.Len(t, approved.PublishedMessages, 1)
	var envelope struct {
		Payload agentic.ToolCall `json:"payload"`
	}
	require.NoError(t, json.Unmarshal(approved.PublishedMessages[0].Data, &envelope))
	require.Equal(t, callsA[0].ID, envelope.Payload.ID)
	require.Equal(t, callsA[0].Name, envelope.Payload.Name)
	require.Equal(t, callsA[0].Arguments, envelope.Payload.Arguments)
	require.Equal(t, callsA[0].RequestID, envelope.Payload.RequestID)
	require.Equal(t, callsA[0].ExecutionID, envelope.Payload.ExecutionID)
	require.Equal(t, callsA[0].CallOrdinal, envelope.Payload.CallOrdinal)
	require.NotEqual(t, callsB[0].Name, envelope.Payload.Name)
	require.NotEqual(t, callsB[0].Arguments, envelope.Payload.Arguments)
}

// spec: agentic-loop / Tool execution has stable framework correlation
func TestGovernanceProposalCarriesFrameworkExecutionCorrelation(t *testing.T) {
	publisher := &mockVerdictPublisher{}
	call := agentic.ToolCall{
		ID: "provider-call", Name: "lookup", RequestID: "request-a",
		ExecutionID: "tool-exec-a", CallOrdinal: 1,
	}

	require.NoError(t, publishProposed(context.Background(), publisher, "loop-a", "", call, nil))
	require.Len(t, publisher.published, 1)
	payload := unwrapProposedFromBaseMessage(t, publisher.published[0].data)
	require.Equal(t, call.RequestID, payload.RequestID)
	require.Equal(t, call.ExecutionID, payload.ExecutionID)
	require.Equal(t, call.CallOrdinal, payload.CallOrdinal)
	require.NotEmpty(t, payload.ProposalFingerprint)
}

// spec: agentic-loop / Tool execution has stable framework correlation
func TestGovernanceWaitersSeparateRepeatedProviderCallID(t *testing.T) {
	publisher := &raceTestPublisher{}
	dispatcher := NewGovernanceDispatcher(
		ToolCallGovernanceConfig{Mode: ToolCallGovernanceModeEnforce, Timeout: "1s"},
		publisher, nil, nil,
	)
	calls := []agentic.ToolCall{
		{ID: "provider-call", Name: "lookup", RequestID: "request-a", ExecutionID: "tool-exec-a", CallOrdinal: 1},
		{ID: "provider-call", Name: "lookup", RequestID: "request-b", ExecutionID: "tool-exec-b", CallOrdinal: 1},
	}
	next := 0
	publisher.onPublish = func() {
		executionID := calls[next].ExecutionID
		next++
		payload, err := json.Marshal(VerdictPayload{Decision: "approved", ExecutionID: executionID})
		require.NoError(t, err)
		decision, err := dispatcher.HandleVerdict("approved", executionID, payload)
		require.NoError(t, err)
		require.Equal(t, natsclient.DeliveryDecisionAck, decision)
	}

	result, err := dispatcher.Propose(context.Background(), "loop-a", "", calls)
	require.NoError(t, err)
	require.Len(t, result.Approved, 2)
	require.Equal(t, "provider-call", result.Approved[0].ID)
	require.Equal(t, "provider-call", result.Approved[1].ID)
}
