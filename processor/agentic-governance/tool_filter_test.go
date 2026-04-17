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
