package agentictools

import (
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/stretchr/testify/require"
)

// spec: agentic-tools / Completed tool outcome identity is globally unambiguous
func TestCompletedOutcomeIdentitySeparatesRepeatedProviderCallID(t *testing.T) {
	first := agentic.ToolCall{
		ID: "provider-call", Name: "lookup", RequestID: "request-a",
		ExecutionID: "tool-exec-a", CallOrdinal: 1,
	}
	second := first
	second.RequestID = "request-b"
	second.ExecutionID = "tool-exec-b"

	require.NotEqual(t, toolCallOutcomeKey(first.ExecutionID), toolCallOutcomeKey(second.ExecutionID))

	firstResult := agentic.ToolResult{
		CallID: first.ID, RequestID: first.RequestID, ExecutionID: first.ExecutionID,
		CallOrdinal: first.CallOrdinal, Content: "first",
	}
	record, err := newCompletedOutcome(first, firstResult)
	require.NoError(t, err)
	wire, err := marshalCompletedOutcome(record)
	require.NoError(t, err)

	decoded, err := decodeCompletedOutcome(wire, first)
	require.NoError(t, err)
	require.Equal(t, first.ExecutionID, decoded.ExecutionID)
	_, err = decodeCompletedOutcome(wire, second)
	require.Error(t, err, "a different logical request cannot replay this outcome")
}

// spec: agentic-tools / Tool outcomes preserve framework execution correlation
func TestHostedToolResultCorrelationIsFrameworkStamped(t *testing.T) {
	call := agentic.ToolCall{
		ID: "provider-call", Name: "lookup", LoopID: "loop-a", TraceID: "trace-a",
		RequestID: "request-a", ExecutionID: "tool-exec-a", CallOrdinal: 2,
	}
	result := correlateToolResult(call, agentic.ToolResult{
		CallID: "executor-value", RequestID: "executor-value", ExecutionID: "executor-value", CallOrdinal: 99,
		Content: "domain result",
	})

	require.Equal(t, call.ID, result.CallID)
	require.Equal(t, call.RequestID, result.RequestID)
	require.Equal(t, call.ExecutionID, result.ExecutionID)
	require.Equal(t, call.CallOrdinal, result.CallOrdinal)
	require.Equal(t, call.LoopID, result.LoopID)
	require.Equal(t, call.TraceID, result.TraceID)
	require.Equal(t, "domain result", result.Content)
}
