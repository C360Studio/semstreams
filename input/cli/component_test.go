package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/payloadbuiltins"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestComponent_Meta(t *testing.T) {
	config := DefaultConfig()
	configJSON, _ := json.Marshal(config)

	comp, err := NewComponent(configJSON, mockDependencies(t))
	require.NoError(t, err)

	meta := comp.Meta()
	assert.Equal(t, "cli-input", meta.Name)
	assert.Equal(t, "input", meta.Type)
	assert.Contains(t, meta.Description, "CLI input")
}

func TestComponent_Ports(t *testing.T) {
	config := DefaultConfig()
	configJSON, _ := json.Marshal(config)

	comp, err := NewComponent(configJSON, mockDependencies(t))
	require.NoError(t, err)

	cliComp := comp.(*Component)

	// Check input ports
	inputs := cliComp.InputPorts()
	assert.Len(t, inputs, 1)

	// Check output ports
	outputs := cliComp.OutputPorts()
	assert.Len(t, outputs, 2)
}

func TestComponent_Health(t *testing.T) {
	config := DefaultConfig()
	configJSON, _ := json.Marshal(config)

	comp, err := NewComponent(configJSON, mockDependencies(t))
	require.NoError(t, err)

	cliComp := comp.(*Component)

	// Before start
	health := cliComp.Health()
	assert.False(t, health.Healthy)
	assert.Equal(t, "stopped", health.Status)
}

func TestComponent_LocalCommands(t *testing.T) {
	config := DefaultConfig()
	configJSON, _ := json.Marshal(config)

	comp, err := NewComponent(configJSON, mockDependencies(t))
	require.NoError(t, err)

	cliComp := comp.(*Component)

	tests := []struct {
		command  string
		isLocal  bool
		setsLoop bool
	}{
		{"/quit", true, false},
		{"/exit", true, false},
		{"/clear", true, true},
		{"/help", false, false},   // Sent to router
		{"/cancel", false, false}, // Sent to router
		{"regular message", false, false},
	}

	for _, tt := range tests {
		t.Run(tt.command, func(t *testing.T) {
			if tt.setsLoop {
				cliComp.SetActiveLoop("test-loop")
			}
			isLocal := cliComp.handleLocalCommand(tt.command)
			assert.Equal(t, tt.isLocal, isLocal)
		})
	}
}

func TestComponent_ActiveLoop(t *testing.T) {
	config := DefaultConfig()
	configJSON, _ := json.Marshal(config)

	comp, err := NewComponent(configJSON, mockDependencies(t))
	require.NoError(t, err)

	cliComp := comp.(*Component)

	// Initially empty
	assert.Empty(t, cliComp.GetActiveLoop())

	// Set loop
	cliComp.SetActiveLoop("loop-123")
	assert.Equal(t, "loop-123", cliComp.GetActiveLoop())

	// Clear via command
	cliComp.handleLocalCommand("/clear")
	assert.Empty(t, cliComp.GetActiveLoop())
}

func TestComponent_DisplayResponse(t *testing.T) {
	config := DefaultConfig()
	configJSON, _ := json.Marshal(config)

	comp, err := NewComponent(configJSON, mockDependencies(t))
	require.NoError(t, err)

	cliComp := comp.(*Component)

	tests := []struct {
		name     string
		resp     agentic.UserResponse
		contains string
	}{
		{
			name: "error response",
			resp: agentic.UserResponse{
				Type:    agentic.ResponseTypeError,
				Content: "Something went wrong",
			},
			contains: "[ERROR]",
		},
		{
			name: "status response",
			resp: agentic.UserResponse{
				Type:    agentic.ResponseTypeStatus,
				Content: "Task started",
			},
			contains: "[STATUS]",
		},
		{
			name: "result response",
			resp: agentic.UserResponse{
				Type:    agentic.ResponseTypeResult,
				Content: "Task completed",
			},
			contains: "[RESULT]",
		},
		{
			name: "prompt response",
			resp: agentic.UserResponse{
				Type:    agentic.ResponseTypePrompt,
				Content: "Approve?",
				Actions: []agentic.ResponseAction{
					{ID: "approve", Label: "Yes"},
					{ID: "reject", Label: "No"},
				},
			},
			contains: "[PROMPT]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			cliComp.SetWriter(&buf)

			cliComp.displayResponse(tt.resp)

			output := buf.String()
			assert.Contains(t, output, tt.contains)
			assert.Contains(t, output, tt.resp.Content)
		})
	}
}

func TestComponent_DisplayResponse_WithActions(t *testing.T) {
	config := DefaultConfig()
	configJSON, _ := json.Marshal(config)

	comp, err := NewComponent(configJSON, mockDependencies(t))
	require.NoError(t, err)

	cliComp := comp.(*Component)

	var buf bytes.Buffer
	cliComp.SetWriter(&buf)

	resp := agentic.UserResponse{
		Type:    agentic.ResponseTypePrompt,
		Content: "Ready for approval",
		Actions: []agentic.ResponseAction{
			{ID: "approve", Label: "Approve Changes"},
			{ID: "reject", Label: "Reject Changes"},
		},
	}

	cliComp.displayResponse(resp)

	output := buf.String()
	assert.Contains(t, output, "[approve]")
	assert.Contains(t, output, "Approve Changes")
	assert.Contains(t, output, "[reject]")
	assert.Contains(t, output, "Reject Changes")
}

func TestComponent_HandleResponse_TracksActiveLoop(t *testing.T) {
	config := DefaultConfig()
	configJSON, _ := json.Marshal(config)

	comp, err := NewComponent(configJSON, mockDependencies(t))
	require.NoError(t, err)

	cliComp := comp.(*Component)
	var buf bytes.Buffer
	cliComp.SetWriter(&buf)

	// Initially no active loop
	assert.Empty(t, cliComp.GetActiveLoop())

	// Simulate receiving a response with InReplyTo (wrapped in BaseMessage)
	resp := &agentic.UserResponse{
		ResponseID:  "resp-test-123",
		ChannelType: "cli",
		ChannelID:   "test-session",
		Type:        agentic.ResponseTypeStatus,
		Content:     "Loop started",
		InReplyTo:   "loop-xyz",
	}
	baseMsg := message.NewBaseMessage(resp.Schema(), resp, "test")
	respData, _ := json.Marshal(baseMsg)

	cliComp.handleResponse(nil, respData)

	// Active loop should be set
	assert.Equal(t, "loop-xyz", cliComp.GetActiveLoop())
}

func TestComponent_ConfigSchema(t *testing.T) {
	config := DefaultConfig()
	configJSON, _ := json.Marshal(config)

	comp, err := NewComponent(configJSON, mockDependencies(t))
	require.NoError(t, err)

	cliComp := comp.(*Component)
	schema := cliComp.ConfigSchema()

	assert.Contains(t, schema.Properties, "user_id")
	assert.Contains(t, schema.Properties, "session_id")
	assert.Contains(t, schema.Properties, "prompt")
}

func TestNewComponent_DefaultsApplied(t *testing.T) {
	// Minimal config
	config := map[string]interface{}{}
	configJSON, _ := json.Marshal(config)

	comp, err := NewComponent(configJSON, mockDependencies(t))
	require.NoError(t, err)

	cliComp := comp.(*Component)

	// Defaults should be applied
	assert.Equal(t, "cli-user", cliComp.config.UserID)
	assert.Equal(t, "cli-session", cliComp.config.SessionID)
	assert.Equal(t, "> ", cliComp.config.Prompt)
}

func TestComponent_SetReaderWriter(t *testing.T) {
	config := DefaultConfig()
	configJSON, _ := json.Marshal(config)

	comp, err := NewComponent(configJSON, mockDependencies(t))
	require.NoError(t, err)

	cliComp := comp.(*Component)

	// Set custom reader/writer
	reader := strings.NewReader("test input")
	var writer bytes.Buffer

	cliComp.SetReader(reader)
	cliComp.SetWriter(&writer)

	// Verify they're set (indirectly through displayResponse)
	cliComp.displayResponse(agentic.UserResponse{
		Type:    agentic.ResponseTypeText,
		Content: "test output",
	})

	assert.Contains(t, writer.String(), "test output")
}

// mockDependencies creates mock dependencies for testing.
// Takes testing.TB so the test-only payload registry constructor is
// callable; production code physically cannot reach this helper.
func mockDependencies(tb testing.TB) component.Dependencies {
	tb.Helper()
	return component.Dependencies{
		NATSClient:      nil, // Tests don't need actual NATS
		Logger:          nil,
		PayloadRegistry: payloadbuiltins.NewTestRegistry(tb),
	}
}
