package executors

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- StubWebSearchExecutor tests ---

func TestStubWebSearchExecutor_ListTools(t *testing.T) {
	executor := NewStubWebSearchExecutor()
	tools := executor.ListTools()

	require.Len(t, tools, 1)
	assert.Equal(t, "web_search", tools[0].Name)
}

func TestStubWebSearchExecutor_Execute_ValidQuery(t *testing.T) {
	executor := NewStubWebSearchExecutor()

	call := agentic.ToolCall{
		ID:   "call-stub-valid",
		Name: "web_search",
		Arguments: map[string]any{
			"query": "NATS JetStream vs Kafka",
		},
	}

	result, err := executor.Execute(context.Background(), call)

	require.NoError(t, err)
	assert.Empty(t, result.Error, "valid query should not produce an error result")
	assert.NotEmpty(t, result.Content, "valid query should return content")
	assert.Contains(t, result.Content, "NATS JetStream vs Kafka", "content should mention the query")
	assert.Equal(t, call.ID, result.CallID)
}

func TestStubWebSearchExecutor_Execute_MissingQuery(t *testing.T) {
	executor := NewStubWebSearchExecutor()

	call := agentic.ToolCall{
		ID:        "call-stub-missing-query",
		Name:      "web_search",
		Arguments: map[string]any{},
	}

	result, err := executor.Execute(context.Background(), call)

	// Missing query is a caller error: no Go error returned, but result.Error is set.
	require.NoError(t, err, "missing query should not return a Go error")
	assert.NotEmpty(t, result.Error, "missing query should set result.Error")
	assert.Empty(t, result.Content, "missing query should not produce content")
	assert.Equal(t, call.ID, result.CallID)
}

// --- WebSearchExecutor tests (no real API calls) ---

func TestWebSearchExecutor_ListTools(t *testing.T) {
	executor := NewWebSearchExecutor("test-api-key")
	tools := executor.ListTools()

	require.Len(t, tools, 1)

	tool := tools[0]
	assert.Equal(t, "web_search", tool.Name)
	assert.NotEmpty(t, tool.Description)

	// Parameters must satisfy agentic validation (non-nil, non-empty).
	require.NotNil(t, tool.Parameters)
	assert.NotEmpty(t, tool.Parameters)

	props, ok := tool.Parameters["properties"].(map[string]any)
	require.True(t, ok, "parameters should have a 'properties' map")
	_, hasQuery := props["query"]
	assert.True(t, hasQuery, "parameters should include 'query'")
}

func TestWebSearchExecutor_New(t *testing.T) {
	const key = "brave-test-key-xyz"
	executor := NewWebSearchExecutor(key)

	require.NotNil(t, executor)
	assert.Equal(t, key, executor.apiKey)

	require.NotNil(t, executor.httpClient, "http client should be initialised")

	// Constructor sets a 10-second timeout; confirm it is non-zero and sane.
	assert.Equal(t, 10*time.Second, executor.httpClient.Timeout)

	// The client type should be *http.Client (not a nil interface).
	_, ok := any(executor.httpClient).(*http.Client)
	assert.True(t, ok, "httpClient should be a *http.Client")
}
