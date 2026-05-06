package agentic_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/agentic"
)

func TestNewJSONSchemaFormat_StrictTrueByDefault(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{"type": "string"},
		},
		"required":             []any{"action"},
		"additionalProperties": false,
	}
	rf := agentic.NewJSONSchemaFormat("decide_action_args", schema)

	if rf == nil {
		t.Fatal("NewJSONSchemaFormat returned nil")
	}
	if rf.Type != agentic.ResponseFormatJSONSchema {
		t.Errorf("Type = %q, want %q", rf.Type, agentic.ResponseFormatJSONSchema)
	}
	if rf.Name != "decide_action_args" {
		t.Errorf("Name = %q, want %q", rf.Name, "decide_action_args")
	}
	if !rf.Strict {
		t.Error("Strict = false, want true (the OpenAI Structured Outputs guarantee)")
	}
	if len(rf.Schema) == 0 {
		t.Error("Schema is empty, want copy of input schema")
	}
}

func TestNewJSONObjectFormat_NoSchemaOrName(t *testing.T) {
	rf := agentic.NewJSONObjectFormat()

	if rf == nil {
		t.Fatal("NewJSONObjectFormat returned nil")
	}
	if rf.Type != agentic.ResponseFormatJSONObject {
		t.Errorf("Type = %q, want %q", rf.Type, agentic.ResponseFormatJSONObject)
	}
	if rf.Name != "" {
		t.Errorf("Name = %q, want empty (json_object mode does not require a name)", rf.Name)
	}
	if len(rf.Schema) != 0 {
		t.Errorf("Schema has %d keys, want empty (json_object mode does not use a schema)", len(rf.Schema))
	}
	if rf.Strict {
		t.Error("Strict = true on json_object mode; strict only applies to json_schema")
	}
}

func TestResponseFormat_Validate(t *testing.T) {
	validSchema := map[string]any{
		"type":                 "object",
		"properties":           map[string]any{"x": map[string]any{"type": "string"}},
		"required":             []any{"x"},
		"additionalProperties": false,
	}

	tests := []struct {
		name    string
		rf      *agentic.ResponseFormat
		wantErr bool
		errSub  string
	}{
		{
			name:    "valid json_object",
			rf:      &agentic.ResponseFormat{Type: agentic.ResponseFormatJSONObject},
			wantErr: false,
		},
		{
			name:    "valid json_schema via constructor",
			rf:      agentic.NewJSONSchemaFormat("my_output", validSchema),
			wantErr: false,
		},
		{
			name: "valid json_schema explicit",
			rf: &agentic.ResponseFormat{
				Type:   agentic.ResponseFormatJSONSchema,
				Name:   "my_output",
				Schema: validSchema,
				Strict: true,
			},
			wantErr: false,
		},
		{
			name: "valid json_schema with strict=false (permissive opt-in)",
			rf: &agentic.ResponseFormat{
				Type:   agentic.ResponseFormatJSONSchema,
				Name:   "my_output",
				Schema: validSchema,
				Strict: false,
			},
			wantErr: false,
		},
		{
			name:    "empty type",
			rf:      &agentic.ResponseFormat{},
			wantErr: true,
			errSub:  "invalid response_format type",
		},
		{
			name:    "unknown type",
			rf:      &agentic.ResponseFormat{Type: "yaml"},
			wantErr: true,
			errSub:  "invalid response_format type",
		},
		{
			name: "json_schema without name",
			rf: &agentic.ResponseFormat{
				Type:   agentic.ResponseFormatJSONSchema,
				Schema: validSchema,
			},
			wantErr: true,
			errSub:  "name required",
		},
		{
			name: "json_schema without schema",
			rf: &agentic.ResponseFormat{
				Type: agentic.ResponseFormatJSONSchema,
				Name: "my_output",
			},
			wantErr: true,
			errSub:  "schema required",
		},
		{
			name: "json_schema with empty schema map",
			rf: &agentic.ResponseFormat{
				Type:   agentic.ResponseFormatJSONSchema,
				Name:   "my_output",
				Schema: map[string]any{},
			},
			wantErr: true,
			errSub:  "schema required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.rf.Validate()
			if tt.wantErr {
				if err == nil {
					t.Errorf("Validate() expected error containing %q, got nil", tt.errSub)
					return
				}
				if tt.errSub != "" && !strings.Contains(err.Error(), tt.errSub) {
					t.Errorf("Validate() error = %q, want substring %q", err.Error(), tt.errSub)
				}
			} else if err != nil {
				t.Errorf("Validate() unexpected error = %v", err)
			}
		})
	}
}

func TestResponseFormat_JSONRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		rf   *agentic.ResponseFormat
	}{
		{
			name: "json_schema via constructor",
			rf: agentic.NewJSONSchemaFormat("decide_action_args", map[string]any{
				"type":                 "object",
				"properties":           map[string]any{"action": map[string]any{"type": "string"}},
				"required":             []any{"action"},
				"additionalProperties": false,
			}),
		},
		{
			name: "json_object via constructor",
			rf:   agentic.NewJSONObjectFormat(),
		},
		{
			name: "json_schema with strict=false preserved on round-trip",
			rf: &agentic.ResponseFormat{
				Type:   agentic.ResponseFormatJSONSchema,
				Name:   "compat_test",
				Schema: map[string]any{"type": "object"},
				Strict: false,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.rf)
			if err != nil {
				t.Fatalf("Marshal failed: %v", err)
			}

			var decoded agentic.ResponseFormat
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("Unmarshal failed: %v", err)
			}

			if decoded.Type != tt.rf.Type {
				t.Errorf("Type = %q, want %q", decoded.Type, tt.rf.Type)
			}
			if decoded.Name != tt.rf.Name {
				t.Errorf("Name = %q, want %q", decoded.Name, tt.rf.Name)
			}
			if decoded.Strict != tt.rf.Strict {
				t.Errorf("Strict = %v, want %v", decoded.Strict, tt.rf.Strict)
			}
			if len(decoded.Schema) != len(tt.rf.Schema) {
				t.Errorf("Schema key count = %d, want %d", len(decoded.Schema), len(tt.rf.Schema))
			}
		})
	}
}

func TestAgentRequest_WithResponseFormat_Validate(t *testing.T) {
	validSchema := map[string]any{
		"type":                 "object",
		"properties":           map[string]any{"x": map[string]any{"type": "string"}},
		"required":             []any{"x"},
		"additionalProperties": false,
	}

	tests := []struct {
		name    string
		request agentic.AgentRequest
		wantErr bool
		errSub  string
	}{
		{
			name: "request with valid json_schema response_format",
			request: agentic.AgentRequest{
				RequestID: "req-1",
				Messages: []agentic.ChatMessage{
					{Role: "user", Content: "hi"},
				},
				ResponseFormat: agentic.NewJSONSchemaFormat("my_output", validSchema),
			},
			wantErr: false,
		},
		{
			name: "request with valid json_object response_format",
			request: agentic.AgentRequest{
				RequestID: "req-1",
				Messages: []agentic.ChatMessage{
					{Role: "user", Content: "hi"},
				},
				ResponseFormat: agentic.NewJSONObjectFormat(),
			},
			wantErr: false,
		},
		{
			name: "request with nil response_format (default)",
			request: agentic.AgentRequest{
				RequestID: "req-1",
				Messages: []agentic.ChatMessage{
					{Role: "user", Content: "hi"},
				},
				ResponseFormat: nil,
			},
			wantErr: false,
		},
		{
			name: "request with invalid response_format propagates error",
			request: agentic.AgentRequest{
				RequestID: "req-1",
				Messages: []agentic.ChatMessage{
					{Role: "user", Content: "hi"},
				},
				ResponseFormat: &agentic.ResponseFormat{Type: "yaml"},
			},
			wantErr: true,
			errSub:  "invalid response_format type",
		},
		{
			name: "request with json_schema missing name propagates error",
			request: agentic.AgentRequest{
				RequestID: "req-1",
				Messages: []agentic.ChatMessage{
					{Role: "user", Content: "hi"},
				},
				ResponseFormat: &agentic.ResponseFormat{
					Type:   agentic.ResponseFormatJSONSchema,
					Schema: validSchema,
				},
			},
			wantErr: true,
			errSub:  "name required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.request.Validate()
			if tt.wantErr {
				if err == nil {
					t.Errorf("Validate() expected error containing %q, got nil", tt.errSub)
					return
				}
				if tt.errSub != "" && !strings.Contains(err.Error(), tt.errSub) {
					t.Errorf("Validate() error = %q, want substring %q", err.Error(), tt.errSub)
				}
			} else if err != nil {
				t.Errorf("Validate() unexpected error = %v", err)
			}
		})
	}
}

func TestAgentRequest_ResponseFormat_JSONOmitEmpty(t *testing.T) {
	req := agentic.AgentRequest{
		RequestID: "req-1",
		Messages: []agentic.ChatMessage{
			{Role: "user", Content: "hi"},
		},
	}
	data, err := json.Marshal(&req)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	if strings.Contains(string(data), `"response_format"`) {
		t.Errorf("nil ResponseFormat should be omitted from JSON; got: %s", string(data))
	}
}

func TestAgentRequest_ResponseFormat_JSONIncludesWhenSet(t *testing.T) {
	req := agentic.AgentRequest{
		RequestID: "req-1",
		Messages: []agentic.ChatMessage{
			{Role: "user", Content: "hi"},
		},
		ResponseFormat: agentic.NewJSONSchemaFormat("my_output", map[string]any{
			"type":                 "object",
			"properties":           map[string]any{"x": map[string]any{"type": "string"}},
			"required":             []any{"x"},
			"additionalProperties": false,
		}),
	}
	data, err := json.Marshal(&req)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	if !strings.Contains(string(data), `"response_format"`) {
		t.Errorf("non-nil ResponseFormat should appear in JSON; got: %s", string(data))
	}
	if !strings.Contains(string(data), `"json_schema"`) {
		t.Errorf("ResponseFormat type json_schema should appear in JSON; got: %s", string(data))
	}

	var decoded agentic.AgentRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if decoded.ResponseFormat == nil {
		t.Fatal("decoded ResponseFormat is nil after round-trip")
	}
	if decoded.ResponseFormat.Type != agentic.ResponseFormatJSONSchema {
		t.Errorf("decoded.ResponseFormat.Type = %q, want %q", decoded.ResponseFormat.Type, agentic.ResponseFormatJSONSchema)
	}
	if decoded.ResponseFormat.Name != "my_output" {
		t.Errorf("decoded.ResponseFormat.Name = %q, want %q", decoded.ResponseFormat.Name, "my_output")
	}
	if !decoded.ResponseFormat.Strict {
		t.Error("decoded.ResponseFormat.Strict = false, want true")
	}
}
