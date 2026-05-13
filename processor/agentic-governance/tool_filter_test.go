package agenticgovernance

import (
	"context"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToolCallFilter_BlocksBashMetadataEndpoint(t *testing.T) {
	f := NewToolCallFilter(nil)

	msg := ToolCallToMessage(agentic.ToolCall{
		ID:        "call-1",
		Name:      "bash",
		Arguments: map[string]any{"command": "curl http://169.254.169.254/latest/meta-data/"},
	}, "user-1", "ch-1")

	result, err := f.Process(context.Background(), msg)
	require.NoError(t, err)
	assert.False(t, result.Allowed)
	assert.Equal(t, SeverityHigh, result.Violation.Severity)
}

func TestToolCallFilter_AllowsSafeBash(t *testing.T) {
	f := NewToolCallFilter(nil)

	msg := ToolCallToMessage(agentic.ToolCall{
		ID:        "call-2",
		Name:      "bash",
		Arguments: map[string]any{"command": "go test ./..."},
	}, "user-1", "ch-1")

	result, err := f.Process(context.Background(), msg)
	require.NoError(t, err)
	assert.True(t, result.Allowed)
}

func TestToolCallFilter_BlocksHTTPMetadataURL(t *testing.T) {
	f := NewToolCallFilter(nil)

	msg := ToolCallToMessage(agentic.ToolCall{
		ID:        "call-3",
		Name:      "http_request",
		Arguments: map[string]any{"url": "http://169.254.169.254/latest/meta-data/"},
	}, "user-1", "ch-1")

	result, err := f.Process(context.Background(), msg)
	require.NoError(t, err)
	assert.False(t, result.Allowed)
}

func TestToolCallFilter_AllowsSafeHTTP(t *testing.T) {
	f := NewToolCallFilter(nil)

	msg := ToolCallToMessage(agentic.ToolCall{
		ID:        "call-4",
		Name:      "http_request",
		Arguments: map[string]any{"url": "https://pkg.go.dev/net/http"},
	}, "user-1", "ch-1")

	result, err := f.Process(context.Background(), msg)
	require.NoError(t, err)
	assert.True(t, result.Allowed)
}

func TestToolCallFilter_SkipsNonToolCallMessages(t *testing.T) {
	f := NewToolCallFilter(nil)

	msg := &Message{
		Type:    MessageTypeTask,
		Content: Content{Text: "normal task"},
	}

	result, err := f.Process(context.Background(), msg)
	require.NoError(t, err)
	assert.True(t, result.Allowed)
}

func TestToolCallFilter_BlocksForkBomb(t *testing.T) {
	f := NewToolCallFilter(nil)

	msg := ToolCallToMessage(agentic.ToolCall{
		ID:        "call-5",
		Name:      "bash",
		Arguments: map[string]any{"command": ":(){ :|:& };:"},
	}, "user-1", "ch-1")

	result, err := f.Process(context.Background(), msg)
	require.NoError(t, err)
	assert.False(t, result.Allowed)
}

func TestToolCallFilter_CheckPII_WithEmailInArgs(t *testing.T) {
	piiFilter, err := NewPIIFilter(&PIIFilterConfig{
		Types:               []PIIType{PIITypeEmail},
		Strategy:            RedactionLabel,
		ConfidenceThreshold: 0.9,
	})
	require.NoError(t, err)

	f := NewToolCallFilter(piiFilter)

	// Use a tool name that routes to checkPII (not bash or http_request)
	msg := ToolCallToMessage(agentic.ToolCall{
		ID:        "call-pii-1",
		Name:      "graph_query",
		Arguments: map[string]any{"query": "find user admin@example.com"},
	}, "user-1", "ch-1")

	result, err := f.Process(context.Background(), msg)
	require.NoError(t, err)

	// PII filter redacts but does not block
	assert.True(t, result.Allowed)
	assert.NotNil(t, result.Modified, "Modified message expected when PII is detected")
	assert.NotContains(t, result.Modified.Content.Text, "admin@example.com")
}

func TestToolCallFilter_CheckPII_WithCleanArgs(t *testing.T) {
	piiFilter, err := NewPIIFilter(&PIIFilterConfig{
		Types:               []PIIType{PIITypeEmail},
		Strategy:            RedactionLabel,
		ConfidenceThreshold: 0.9,
	})
	require.NoError(t, err)

	f := NewToolCallFilter(piiFilter)

	msg := ToolCallToMessage(agentic.ToolCall{
		ID:        "call-pii-2",
		Name:      "graph_query",
		Arguments: map[string]any{"query": "count all entities"},
	}, "user-1", "ch-1")

	result, err := f.Process(context.Background(), msg)
	require.NoError(t, err)

	assert.True(t, result.Allowed)
	assert.Nil(t, result.Modified, "No modification expected when no PII detected")
}

func TestToolCallFilter_CheckPII_NilPIIFilter(t *testing.T) {
	// NewToolCallFilter(nil) leaves piiFilter nil — checkPII must return allowed immediately.
	f := NewToolCallFilter(nil)

	msg := ToolCallToMessage(agentic.ToolCall{
		ID:        "call-pii-3",
		Name:      "graph_query",
		Arguments: map[string]any{"query": "find user admin@example.com"},
	}, "user-1", "ch-1")

	result, err := f.Process(context.Background(), msg)
	require.NoError(t, err)

	assert.True(t, result.Allowed)
	assert.Nil(t, result.Modified)
}

func TestToolCallToMessage(t *testing.T) {
	call := agentic.ToolCall{
		ID:        "call-x",
		Name:      "bash",
		Arguments: map[string]any{"command": "echo hi"},
		LoopID:    "loop-1",
	}

	msg := ToolCallToMessage(call, "user-42", "channel-7")
	assert.Equal(t, "call-x", msg.ID)
	assert.Equal(t, MessageTypeToolCall, msg.Type)
	assert.Equal(t, "user-42", msg.UserID)
	assert.Equal(t, "bash", msg.Content.Metadata["tool_name"])
	assert.Equal(t, "loop-1", msg.Content.Metadata["loop_id"])
}

// TestViolationReason covers the rejection-reason fallback path used
// when the chain reports a policy violation. Subject-mode rule actions
// emit rejection reasons from this same shape, so the test pins the
// fallback hierarchy for future authors.
func TestViolationReason(t *testing.T) {
	assert.Equal(t, "policy violation", violationReason(nil))
	assert.Equal(t, "policy violation (pii_redaction)", violationReason(&Violation{FilterName: "pii_redaction"}))
	v := &Violation{FilterName: "tool_call_governance", Details: map[string]any{"message": "blocked: rm -rf /"}}
	assert.Equal(t, "blocked: rm -rf /", violationReason(v))
}
