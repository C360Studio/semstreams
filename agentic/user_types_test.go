package agentic

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUserMessage_Validate(t *testing.T) {
	tests := []struct {
		name    string
		msg     UserMessage
		wantErr string
	}{
		{
			name: "valid message",
			msg: UserMessage{
				MessageID:   "msg-123",
				ChannelType: "cli",
				ChannelID:   "session-1",
				UserID:      "user-1",
				Content:     "hello world",
				Timestamp:   time.Now(),
			},
			wantErr: "",
		},
		{
			name: "valid message with attachment only",
			msg: UserMessage{
				MessageID:   "msg-123",
				ChannelType: "cli",
				ChannelID:   "session-1",
				UserID:      "user-1",
				Attachments: []Attachment{{Type: "file", Name: "test.txt"}},
				Timestamp:   time.Now(),
			},
			wantErr: "",
		},
		{
			name: "missing message_id",
			msg: UserMessage{
				ChannelType: "cli",
				ChannelID:   "session-1",
				UserID:      "user-1",
				Content:     "hello",
			},
			wantErr: "message_id required",
		},
		{
			name: "missing channel_type",
			msg: UserMessage{
				MessageID: "msg-123",
				ChannelID: "session-1",
				UserID:    "user-1",
				Content:   "hello",
			},
			wantErr: "channel_type required",
		},
		{
			name: "missing channel_id",
			msg: UserMessage{
				MessageID:   "msg-123",
				ChannelType: "cli",
				UserID:      "user-1",
				Content:     "hello",
			},
			wantErr: "channel_id required",
		},
		{
			name: "missing user_id",
			msg: UserMessage{
				MessageID:   "msg-123",
				ChannelType: "cli",
				ChannelID:   "session-1",
				Content:     "hello",
			},
			wantErr: "user_id required",
		},
		{
			name: "missing content and attachments",
			msg: UserMessage{
				MessageID:   "msg-123",
				ChannelType: "cli",
				ChannelID:   "session-1",
				UserID:      "user-1",
			},
			wantErr: "either content or attachments must be present",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.msg.Validate()
			if tt.wantErr == "" {
				assert.NoError(t, err)
			} else {
				assert.EqualError(t, err, tt.wantErr)
			}
		})
	}
}

func TestUserMessage_JSONRoundTrip(t *testing.T) {
	original := UserMessage{
		MessageID:   "msg-123",
		ChannelType: "slack",
		ChannelID:   "C12345",
		UserID:      "U67890",
		Content:     "test message",
		ReplyTo:     "loop-abc",
		ThreadID:    "thread-xyz",
		Metadata:    map[string]string{"team_id": "T123"},
		Attachments: []Attachment{
			{Type: "file", Name: "doc.pdf", MimeType: "application/pdf", Size: 1024},
		},
		Timestamp: time.Now().UTC().Truncate(time.Millisecond),
	}

	data, err := json.Marshal(original)
	require.NoError(t, err)

	var decoded UserMessage
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, original.MessageID, decoded.MessageID)
	assert.Equal(t, original.ChannelType, decoded.ChannelType)
	assert.Equal(t, original.Content, decoded.Content)
	assert.Equal(t, original.Metadata, decoded.Metadata)
	assert.Len(t, decoded.Attachments, 1)
	assert.Equal(t, original.Attachments[0].Name, decoded.Attachments[0].Name)
}

func TestUserSignal_Validate(t *testing.T) {
	tests := []struct {
		name    string
		signal  UserSignal
		wantErr string
	}{
		{
			name: "valid cancel signal",
			signal: UserSignal{
				SignalID:    "sig-123",
				Type:        SignalCancel,
				LoopID:      "loop-abc",
				UserID:      "user-1",
				ChannelType: "cli",
				ChannelID:   "session-1",
				Timestamp:   time.Now(),
			},
			wantErr: "",
		},
		{
			name: "valid reject signal with payload",
			signal: UserSignal{
				SignalID:    "sig-123",
				Type:        SignalReject,
				LoopID:      "loop-abc",
				UserID:      "user-1",
				ChannelType: "cli",
				ChannelID:   "session-1",
				Payload:     "needs more tests",
				Timestamp:   time.Now(),
			},
			wantErr: "",
		},
		{
			name: "missing signal_id",
			signal: UserSignal{
				Type:   SignalCancel,
				LoopID: "loop-abc",
				UserID: "user-1",
			},
			wantErr: "signal_id required",
		},
		{
			name: "missing type",
			signal: UserSignal{
				SignalID: "sig-123",
				LoopID:   "loop-abc",
				UserID:   "user-1",
			},
			wantErr: "type required",
		},
		{
			name: "invalid type",
			signal: UserSignal{
				SignalID: "sig-123",
				Type:     "invalid",
				LoopID:   "loop-abc",
				UserID:   "user-1",
			},
			wantErr: "type must be one of: cancel, pause, resume, approve, reject, feedback, retry",
		},
		{
			name: "missing loop_id",
			signal: UserSignal{
				SignalID: "sig-123",
				Type:     SignalCancel,
				UserID:   "user-1",
			},
			wantErr: "loop_id required",
		},
		{
			name: "missing user_id",
			signal: UserSignal{
				SignalID: "sig-123",
				Type:     SignalCancel,
				LoopID:   "loop-abc",
			},
			wantErr: "user_id required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.signal.Validate()
			if tt.wantErr == "" {
				assert.NoError(t, err)
			} else {
				assert.EqualError(t, err, tt.wantErr)
			}
		})
	}
}

func TestUserSignal_JSONRoundTrip(t *testing.T) {
	original := UserSignal{
		SignalID:    "sig-123",
		Type:        SignalReject,
		LoopID:      "loop-abc",
		UserID:      "user-1",
		ChannelType: "slack",
		ChannelID:   "C12345",
		Payload:     "rejection reason",
		Timestamp:   time.Now().UTC().Truncate(time.Millisecond),
	}

	data, err := json.Marshal(original)
	require.NoError(t, err)

	var decoded UserSignal
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, original.SignalID, decoded.SignalID)
	assert.Equal(t, original.Type, decoded.Type)
	assert.Equal(t, original.LoopID, decoded.LoopID)
	assert.Equal(t, original.Payload, decoded.Payload)
}

func TestUserResponse_Validate(t *testing.T) {
	tests := []struct {
		name    string
		resp    UserResponse
		wantErr string
	}{
		{
			name: "valid text response",
			resp: UserResponse{
				ResponseID:  "resp-123",
				ChannelType: "cli",
				ChannelID:   "session-1",
				UserID:      "user-1",
				Type:        ResponseTypeText,
				Content:     "Hello!",
				Timestamp:   time.Now(),
			},
			wantErr: "",
		},
		{
			name: "valid prompt response with actions",
			resp: UserResponse{
				ResponseID:  "resp-123",
				ChannelType: "slack",
				ChannelID:   "C12345",
				UserID:      "U67890",
				Type:        ResponseTypePrompt,
				Content:     "Ready for review",
				Actions: []ResponseAction{
					{ID: "approve", Type: "button", Label: "Approve", Signal: SignalApprove, Style: "primary"},
					{ID: "reject", Type: "button", Label: "Reject", Signal: SignalReject, Style: "danger"},
				},
				Timestamp: time.Now(),
			},
			wantErr: "",
		},
		{
			name: "missing response_id",
			resp: UserResponse{
				ChannelType: "cli",
				ChannelID:   "session-1",
				Type:        ResponseTypeText,
			},
			wantErr: "response_id required",
		},
		{
			name: "missing channel_type",
			resp: UserResponse{
				ResponseID: "resp-123",
				ChannelID:  "session-1",
				Type:       ResponseTypeText,
			},
			wantErr: "channel_type required",
		},
		{
			name: "missing channel_id",
			resp: UserResponse{
				ResponseID:  "resp-123",
				ChannelType: "cli",
				Type:        ResponseTypeText,
			},
			wantErr: "channel_id required",
		},
		{
			name: "missing type",
			resp: UserResponse{
				ResponseID:  "resp-123",
				ChannelType: "cli",
				ChannelID:   "session-1",
			},
			wantErr: "type required",
		},
		{
			name: "invalid type",
			resp: UserResponse{
				ResponseID:  "resp-123",
				ChannelType: "cli",
				ChannelID:   "session-1",
				Type:        "invalid",
			},
			wantErr: "type must be one of: text, status, result, error, prompt, stream",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.resp.Validate()
			if tt.wantErr == "" {
				assert.NoError(t, err)
			} else {
				assert.EqualError(t, err, tt.wantErr)
			}
		})
	}
}

func TestUserResponse_JSONRoundTrip(t *testing.T) {
	original := UserResponse{
		ResponseID:  "resp-123",
		ChannelType: "cli",
		ChannelID:   "session-1",
		UserID:      "user-1",
		InReplyTo:   "loop-abc",
		Type:        ResponseTypeResult,
		Content:     "Task completed successfully",
		Blocks: []ResponseBlock{
			{Type: "code", Content: "fmt.Println(\"hello\")", Lang: "go"},
		},
		Actions: []ResponseAction{
			{ID: "retry", Type: "button", Label: "Retry", Signal: SignalRetry},
		},
		Timestamp: time.Now().UTC().Truncate(time.Millisecond),
	}

	data, err := json.Marshal(original)
	require.NoError(t, err)

	var decoded UserResponse
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, original.ResponseID, decoded.ResponseID)
	assert.Equal(t, original.Type, decoded.Type)
	assert.Equal(t, original.Content, decoded.Content)
	assert.Len(t, decoded.Blocks, 1)
	assert.Equal(t, "go", decoded.Blocks[0].Lang)
	assert.Len(t, decoded.Actions, 1)
	assert.Equal(t, SignalRetry, decoded.Actions[0].Signal)
}

func TestSignalTypeConstants(t *testing.T) {
	// Verify all signal types are valid
	validTypes := []string{
		SignalCancel,
		SignalPause,
		SignalResume,
		SignalApprove,
		SignalReject,
		SignalFeedback,
		SignalRetry,
	}

	for _, sigType := range validTypes {
		assert.True(t, isValidSignalType(sigType), "expected %s to be valid", sigType)
	}

	// Verify invalid types
	assert.False(t, isValidSignalType("invalid"))
	assert.False(t, isValidSignalType(""))
}

func TestResponseTypeConstants(t *testing.T) {
	// Verify all response types are valid
	validTypes := []string{
		ResponseTypeText,
		ResponseTypeStatus,
		ResponseTypeResult,
		ResponseTypeError,
		ResponseTypePrompt,
		ResponseTypeStream,
	}

	for _, respType := range validTypes {
		assert.True(t, isValidResponseType(respType), "expected %s to be valid", respType)
	}

	// Verify invalid types
	assert.False(t, isValidResponseType("invalid"))
	assert.False(t, isValidResponseType(""))
}

func TestTaskMessage_Validate(t *testing.T) {
	tests := []struct {
		name    string
		task    TaskMessage
		wantErr string
	}{
		{
			name: "valid task message",
			task: TaskMessage{
				TaskID: "task-123",
				Role:   "general",
				Model:  "qwen2.5-coder:32b",
				Prompt: "help me write code",
			},
			wantErr: "",
		},
		{
			name: "valid task message with loop_id",
			task: TaskMessage{
				LoopID: "loop-abc",
				TaskID: "task-123",
				Role:   "developer",
				Model:  "gpt-4",
				Prompt: "continue the task",
			},
			wantErr: "",
		},
		{
			name: "missing task_id",
			task: TaskMessage{
				Role:   "general",
				Model:  "qwen2.5-coder:32b",
				Prompt: "help me",
			},
			wantErr: "task_id required",
		},
		{
			name: "missing role",
			task: TaskMessage{
				TaskID: "task-123",
				Model:  "qwen2.5-coder:32b",
				Prompt: "help me",
			},
			wantErr: "role required",
		},
		{
			name: "missing model",
			task: TaskMessage{
				TaskID: "task-123",
				Role:   "general",
				Prompt: "help me",
			},
			wantErr: "model required",
		},
		{
			name: "missing prompt",
			task: TaskMessage{
				TaskID: "task-123",
				Role:   "general",
				Model:  "qwen2.5-coder:32b",
			},
			wantErr: "prompt required",
		},
		{
			name: "valid with tool_choice auto",
			task: TaskMessage{
				TaskID:     "task-123",
				Role:       "general",
				Model:      "gpt-4",
				Prompt:     "test",
				ToolChoice: &ToolChoice{Mode: "auto"},
			},
			wantErr: "",
		},
		{
			name: "valid with tool_choice function",
			task: TaskMessage{
				TaskID:     "task-123",
				Role:       "general",
				Model:      "gpt-4",
				Prompt:     "test",
				ToolChoice: &ToolChoice{Mode: "function", FunctionName: "read_file"},
			},
			wantErr: "",
		},
		{
			name: "invalid tool_choice mode",
			task: TaskMessage{
				TaskID:     "task-123",
				Role:       "general",
				Model:      "gpt-4",
				Prompt:     "test",
				ToolChoice: &ToolChoice{Mode: "always"},
			},
			wantErr: `invalid tool_choice mode: "always" (must be auto, required, none, or function)`,
		},
		{
			name: "function mode without name",
			task: TaskMessage{
				TaskID:     "task-123",
				Role:       "general",
				Model:      "gpt-4",
				Prompt:     "test",
				ToolChoice: &ToolChoice{Mode: "function"},
			},
			wantErr: `function_name required when tool_choice mode is "function"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.task.Validate()
			if tt.wantErr == "" {
				assert.NoError(t, err)
			} else {
				assert.EqualError(t, err, tt.wantErr)
			}
		})
	}
}

func TestTaskMessage_JSONRoundTrip(t *testing.T) {
	original := TaskMessage{
		LoopID: "loop-abc",
		TaskID: "task-123",
		Role:   "developer",
		Model:  "qwen2.5-coder:32b",
		Prompt: "help me write better Go code",
	}

	data, err := json.Marshal(original)
	require.NoError(t, err)

	var decoded TaskMessage
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, original.LoopID, decoded.LoopID)
	assert.Equal(t, original.TaskID, decoded.TaskID)
	assert.Equal(t, original.Role, decoded.Role)
	assert.Equal(t, original.Model, decoded.Model)
	assert.Equal(t, original.Prompt, decoded.Prompt)
}

func TestTaskMessage_Tools_JSONRoundTrip(t *testing.T) {
	original := TaskMessage{
		TaskID: "task-tools",
		Role:   "general",
		Model:  "fast",
		Prompt: "do the thing",
		Tools: []ToolDefinition{
			{
				Name:        "graph_query",
				Description: "Query the knowledge graph",
				Parameters:  map[string]any{"type": "object"},
			},
			{
				Name:        "file_read",
				Description: "Read a file",
				Parameters:  map[string]any{"type": "object"},
			},
		},
		Metadata: map[string]any{
			"tenant_id": "acme",
			"org":       "ops",
		},
	}

	data, err := json.Marshal(original)
	require.NoError(t, err)

	var decoded TaskMessage
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Len(t, decoded.Tools, 2)
	assert.Equal(t, "graph_query", decoded.Tools[0].Name)
	assert.Equal(t, "file_read", decoded.Tools[1].Name)
	assert.Equal(t, "acme", decoded.Metadata["tenant_id"])
	assert.Equal(t, "ops", decoded.Metadata["org"])
}

// TestTaskMessage_ResponseFormat_JSONRoundTrip verifies that
// ResponseFormat threads through JSON serialization on TaskMessage.
// ADR-034. Both helpers (NewJSONSchemaFormat, NewJSONObjectFormat)
// must round-trip with the Type / Schema / Name / Strict fields intact.
func TestTaskMessage_ResponseFormat_JSONRoundTrip(t *testing.T) {
	t.Run("JSONSchema strict mode", func(t *testing.T) {
		rf := NewJSONSchemaFormat("decision", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"action": map[string]any{"type": "string"},
			},
			"required": []any{"action"},
		})
		original := TaskMessage{
			TaskID:         "task-rf-schema",
			Role:           "planner",
			Model:          "qwen3:7b",
			Prompt:         "decide",
			ResponseFormat: rf,
		}

		data, err := json.Marshal(original)
		require.NoError(t, err)

		var decoded TaskMessage
		require.NoError(t, json.Unmarshal(data, &decoded))

		require.NotNil(t, decoded.ResponseFormat)
		assert.Equal(t, ResponseFormatJSONSchema, decoded.ResponseFormat.Type)
		assert.Equal(t, "decision", decoded.ResponseFormat.Name)
		assert.True(t, decoded.ResponseFormat.Strict)
		require.NotNil(t, decoded.ResponseFormat.Schema)
		assert.Equal(t, "object", decoded.ResponseFormat.Schema["type"])
	})

	t.Run("JSONObject bare mode", func(t *testing.T) {
		original := TaskMessage{
			TaskID:         "task-rf-object",
			Role:           "general",
			Model:          "qwen3:7b",
			Prompt:         "respond as JSON",
			ResponseFormat: NewJSONObjectFormat(),
		}

		data, err := json.Marshal(original)
		require.NoError(t, err)

		var decoded TaskMessage
		require.NoError(t, json.Unmarshal(data, &decoded))

		require.NotNil(t, decoded.ResponseFormat)
		assert.Equal(t, ResponseFormatJSONObject, decoded.ResponseFormat.Type)
		assert.Empty(t, decoded.ResponseFormat.Name)
		assert.False(t, decoded.ResponseFormat.Strict)
	})

	t.Run("nil omits field via omitempty", func(t *testing.T) {
		original := TaskMessage{
			TaskID: "task-rf-nil",
			Role:   "general",
			Model:  "fast",
			Prompt: "no constraint",
			// ResponseFormat omitted
		}

		data, err := json.Marshal(original)
		require.NoError(t, err)

		// `omitempty` on a pointer means absent JSON key when nil.
		// The receiver sees a nil ResponseFormat after unmarshal — back-compat
		// for every caller that doesn't opt in.
		assert.NotContains(t, string(data), "response_format",
			"nil ResponseFormat should be omitted from JSON entirely")

		var decoded TaskMessage
		require.NoError(t, json.Unmarshal(data, &decoded))
		assert.Nil(t, decoded.ResponseFormat)
	})
}

// TestTaskMessage_ResponseFormat_ValidatePropagates verifies that an
// invalid ResponseFormat surfaces from TaskMessage.Validate (e.g., a
// json_schema type without a name field). ADR-034 invariant: validation
// at the TaskMessage boundary catches malformed schemas before they hit
// the LLM client and produce confusing 400s.
func TestTaskMessage_ResponseFormat_ValidatePropagates(t *testing.T) {
	task := TaskMessage{
		TaskID: "task-bad-rf",
		Role:   "general",
		Model:  "fast",
		Prompt: "x",
		ResponseFormat: &ResponseFormat{
			Type: ResponseFormatJSONSchema,
			// Missing Name and Schema — both required when Type == JSONSchema
		},
	}
	err := task.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "name required",
		"TaskMessage.Validate should propagate ResponseFormat.Validate failures")
}

// TestTaskMessage_Tools_NilVsEmptyPreserved verifies that Tools serialises
// faithfully for both nil and explicit empty slices. The distinction is
// load-bearing: the spawner uses nil to mean "no override, discover tools"
// and empty to mean "no tools for this role". If either value round-trips
// identically we break product-layer role scoping.
func TestTaskMessage_Tools_NilVsEmptyPreserved(t *testing.T) {
	// Nil → `"tools": null` (field present, value null). Unmarshals back
	// to nil so the loop falls through to discovery.
	nilCase := TaskMessage{TaskID: "t1", Role: "general", Model: "fast", Prompt: "p"}
	data, err := json.Marshal(nilCase)
	require.NoError(t, err)
	var raw map[string]any
	require.NoError(t, json.Unmarshal(data, &raw))
	val, hasTools := raw["tools"]
	assert.True(t, hasTools, "tools field should be present (omitempty removed)")
	assert.Nil(t, val, "nil Tools should serialise as JSON null")

	// Empty non-nil → `"tools": []`. Unmarshals back to empty non-nil
	// which the loop respects as "no tools for this role".
	emptyCase := TaskMessage{TaskID: "t2", Role: "general", Model: "fast", Prompt: "p", Tools: []ToolDefinition{}}
	data, err = json.Marshal(emptyCase)
	require.NoError(t, err)
	var decoded TaskMessage
	require.NoError(t, json.Unmarshal(data, &decoded))
	assert.NotNil(t, decoded.Tools, "empty Tools must round-trip as non-nil")
	assert.Len(t, decoded.Tools, 0)

	// Metadata retains its own omitempty — assert that, to guard against
	// an accidental flip.
	raw = map[string]any{}
	require.NoError(t, json.Unmarshal(data, &raw))
	_, hasMeta := raw["metadata"]
	assert.False(t, hasMeta, "metadata should still be omitted when empty")
}

func TestTaskMessage_BackwardCompat_OldJSON(t *testing.T) {
	// JSON without the new fields — should deserialize cleanly
	oldJSON := `{"task_id":"t1","role":"general","model":"fast","prompt":"hello"}`

	var decoded TaskMessage
	err := json.Unmarshal([]byte(oldJSON), &decoded)
	require.NoError(t, err)

	assert.Equal(t, "t1", decoded.TaskID)
	assert.Nil(t, decoded.Tools)
	assert.Nil(t, decoded.Metadata)
}
