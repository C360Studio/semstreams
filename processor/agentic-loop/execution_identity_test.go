package agenticloop

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/stretchr/testify/require"
)

// spec: agentic-loop / A logical model request has one deterministic identity
//
// (a) of the #1328 scope amendment: the same logical request minted twice
// yields the same ID, and a request at a different iteration does not.
func TestRequestIDIsDeterministicPerIteration(t *testing.T) {
	manager := NewLoopManager()
	loopID, err := manager.CreateLoop("task-determinism", "role", "model", 5)
	require.NoError(t, err)

	first := manager.GenerateRequestID(loopID)
	redelivered := manager.GenerateRequestID(loopID)
	require.Equal(t, first, redelivered,
		"a redelivered task republishes the same logical request, so its ID must not move")
	require.Equal(t, loopID+":req:1:0", first,
		"the first request of a loop is iteration 1, retry 0")

	require.NoError(t, manager.IncrementIteration(loopID))
	second := manager.GenerateRequestID(loopID)
	require.Equal(t, loopID+":req:2:0", second)
	require.NotEqual(t, first, second,
		"a different iteration is different logical work and must not reuse an identity")
}

// spec: agentic-loop / A logical model request has one deterministic identity
//
// (b) of the #1328 scope amendment: a truncation retry of iteration N is :N:1,
// and forward progress clears the ordinal back to 0.
func TestRequestIDCarriesTheTruncationRetryOrdinal(t *testing.T) {
	manager := NewLoopManager()
	loopID, err := manager.CreateLoop("task-retry", "role", "model", 5)
	require.NoError(t, err)
	require.NoError(t, manager.IncrementIteration(loopID))
	require.NoError(t, manager.IncrementIteration(loopID))

	require.Equal(t, loopID+":req:3:0", manager.GenerateRequestID(loopID))

	require.Equal(t, 1, manager.IncrementTruncationRetry(loopID))
	require.Equal(t, loopID+":req:3:1", manager.GenerateRequestID(loopID),
		"the compaction retry is within-iteration recovery: same iteration, next retry ordinal")

	manager.ResetTruncationRetry(loopID)
	require.Equal(t, loopID+":req:3:0", manager.GenerateRequestID(loopID),
		"forward progress clears the retry ordinal")
}

// spec: agentic-loop / A logical model request has one deterministic identity
//
// (c) of the #1328 scope amendment: the loopID:req: prefix is unchanged, so
// loop extraction and the agent.response.<requestID> subject both still
// resolve. semspec splits a RequestID on its FIRST colon; the suffix shape is
// free, the prefix is the contract.
func TestRequestIDKeepsTheLoopPrefixAndSubjectGrammar(t *testing.T) {
	manager := NewLoopManager()
	loopID, err := manager.CreateLoop("task-grammar", "role", "model", 5)
	require.NoError(t, err)
	requestID := manager.GenerateRequestID(loopID)

	require.True(t, strings.HasPrefix(requestID, loopID+":req:"))
	require.Equal(t, loopID, manager.ExtractLoopIDFromRequest(requestID))
	require.Equal(t, loopID, strings.SplitN(requestID, ":", 2)[0],
		"a consumer that splits on the first colon still recovers the loop token")
	require.NotContains(t, requestID, ".",
		"a RequestID is one NATS subject token, so it may not contain a dot")

	subject, err := component.ResolveSubject(
		[]component.PortDefinition{{
			Name:   "agent.response",
			Config: component.JetStreamPort{Subjects: []string{"agent.response.*"}, StreamName: "AGENT"},
		}},
		"agent.response", requestID)
	require.NoError(t, err)
	require.Equal(t, "agent.response."+requestID, subject)
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
		LoopID: loopA, CallID: callsA[0].ID, ExecutionID: callsA[0].ExecutionID,
		Decision: agentic.ApprovalDecisionApprove, ApprovedBy: "reviewer",
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
		decision, err := dispatcher.HandleVerdict("approved", executionID,
			VerdictPayload{Decision: "approved", ExecutionID: executionID})
		require.NoError(t, err)
		require.Equal(t, natsclient.DeliveryDecisionAck, decision)
	}

	result, err := dispatcher.Propose(context.Background(), "loop-a", "", calls)
	require.NoError(t, err)
	require.Len(t, result.Approved, 2)
	require.Equal(t, "provider-call", result.Approved[0].ID)
	require.Equal(t, "provider-call", result.Approved[1].ID)
}
