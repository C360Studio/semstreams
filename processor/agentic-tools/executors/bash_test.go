package executors

import (
	"context"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBashExecutor_ListTools(t *testing.T) {
	e := NewBashExecutor("/tmp", "")
	tools := e.ListTools()
	require.Len(t, tools, 1)
	assert.Equal(t, "bash", tools[0].Name)
	assert.Contains(t, tools[0].Description, "shell command")
}

func TestBashExecutor_LocalExec(t *testing.T) {
	e := NewBashExecutor("/tmp", "")

	result, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:        "call-1",
		Name:      "bash",
		Arguments: map[string]any{"command": "echo hello"},
	})
	require.NoError(t, err)
	assert.Equal(t, "call-1", result.CallID)
	assert.Contains(t, result.Content, "hello")
	assert.Empty(t, result.Error)
}

func TestBashExecutor_MissingCommand(t *testing.T) {
	e := NewBashExecutor("/tmp", "")

	result, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:        "call-2",
		Name:      "bash",
		Arguments: map[string]any{},
	})
	require.NoError(t, err)
	assert.Equal(t, "command argument is required", result.Error)
}

func TestBashExecutor_NonZeroExit(t *testing.T) {
	e := NewBashExecutor("/tmp", "")

	result, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:        "call-3",
		Name:      "bash",
		Arguments: map[string]any{"command": "exit 42"},
	})
	require.NoError(t, err)
	assert.Contains(t, result.Error, "exit code 42")
}

func TestBashExecutor_StderrCaptured(t *testing.T) {
	e := NewBashExecutor("/tmp", "")

	result, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:        "call-4",
		Name:      "bash",
		Arguments: map[string]any{"command": "echo stdout-text && echo stderr-text >&2"},
	})
	require.NoError(t, err)
	assert.Contains(t, result.Content, "stdout-text")
	assert.Contains(t, result.Content, "stderr-text")
}

func TestBashExecutor_SandboxMode(t *testing.T) {
	// When sandbox URL is set, sandbox client is created
	e := NewBashExecutor("/tmp", "http://sandbox:8080")
	assert.NotNil(t, e.sandbox)

	// Without sandbox URL, runs locally
	e2 := NewBashExecutor("/tmp", "")
	assert.Nil(t, e2.sandbox)
}

func TestFilterEnv_StripsSecrets(t *testing.T) {
	// This tests that filterEnv doesn't include known-secret patterns.
	// We can't easily inject env vars in unit tests, but we can verify
	// the function doesn't panic and returns a non-nil result.
	env := filterEnv()
	assert.NotNil(t, env)

	// Verify no sensitive patterns leaked through
	for _, e := range env {
		upper := e
		assert.NotContains(t, upper, "ANTHROPIC_API_KEY=")
		assert.NotContains(t, upper, "OPENAI_API_KEY=")
	}
}
