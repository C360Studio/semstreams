package agenticmodel

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/model"
	"github.com/c360studio/semstreams/model/wire/responses"
)

// newResponsesTestClient builds a minimal Client with a Responses
// adapter wired so translate calls have something to invoke.
func newResponsesTestClient(t *testing.T) *Client {
	t.Helper()
	c := &Client{
		endpoint: &model.EndpointConfig{
			Provider:    "openai",
			Model:       "gpt-5.5",
			WireBackend: "responses",
		},
		responsesAdapter: &OpenAIResponsesAdapter{},
	}
	return c
}

// TestBuildResponsesRequest_RoleTranslation pins ADR-051 open
// question 2: system messages translate to role:developer InputItems.
func TestBuildResponsesRequest_RoleTranslation(t *testing.T) {
	c := newResponsesTestClient(t)
	req := agentic.AgentRequest{
		Messages: []agentic.ChatMessage{
			{Role: "system", Content: "you are X"},
			{Role: "user", Content: "do Y"},
		},
	}
	got := c.buildResponsesRequest(req)
	if len(got.Input) != 2 {
		t.Fatalf("Input count = %d, want 2", len(got.Input))
	}
	if got.Input[0].Role != responses.RoleDeveloper {
		t.Errorf("system → role = %q, want %q", got.Input[0].Role, responses.RoleDeveloper)
	}
	if got.Input[1].Role != responses.RoleUser {
		t.Errorf("user → role = %q, want %q", got.Input[1].Role, responses.RoleUser)
	}
}

// TestBuildResponsesRequest_StatelessByDefault pins ADR-051 D2:
// the translator always sets Store=false.
func TestBuildResponsesRequest_StatelessByDefault(t *testing.T) {
	c := newResponsesTestClient(t)
	got := c.buildResponsesRequest(agentic.AgentRequest{
		Messages: []agentic.ChatMessage{{Role: "user", Content: "hi"}},
	})
	if got.Store == nil || *got.Store != false {
		t.Errorf("Store = %v, want explicit false", got.Store)
	}
}

// TestBuildResponsesRequest_AssistantToolCalls pins that an
// assistant message with tool_calls produces a function_call
// InputItem per call, in order.
func TestBuildResponsesRequest_AssistantToolCalls(t *testing.T) {
	c := newResponsesTestClient(t)
	req := agentic.AgentRequest{
		Messages: []agentic.ChatMessage{
			{
				Role: "assistant",
				ToolCalls: []agentic.ToolCall{
					{ID: "call_a", Name: "fn_a", Arguments: map[string]any{"x": 1.0}},
					{ID: "call_b", Name: "fn_b", Arguments: map[string]any{"y": 2.0}},
				},
			},
		},
	}
	got := c.buildResponsesRequest(req)
	if len(got.Input) != 2 {
		t.Fatalf("Input count = %d, want 2 (one per tool_call, no content message)", len(got.Input))
	}
	for i, want := range []struct{ callID, name string }{
		{"call_a", "fn_a"},
		{"call_b", "fn_b"},
	} {
		if !got.Input[i].IsFunctionCall() {
			t.Errorf("Input[%d].Type = %q, want function_call", i, got.Input[i].Type)
		}
		if got.Input[i].CallID != want.callID {
			t.Errorf("Input[%d].CallID = %q, want %q", i, got.Input[i].CallID, want.callID)
		}
		if got.Input[i].Name != want.name {
			t.Errorf("Input[%d].Name = %q, want %q", i, got.Input[i].Name, want.name)
		}
	}
}

// TestBuildResponsesRequest_ToolMessage pins that role:tool messages
// translate to function_call_output InputItems with the tool result
// in the Output field.
func TestBuildResponsesRequest_ToolMessage(t *testing.T) {
	c := newResponsesTestClient(t)
	req := agentic.AgentRequest{
		Messages: []agentic.ChatMessage{
			{
				Role:       "tool",
				ToolCallID: "call_a",
				Name:       "fn_a",
				Content:    `{"result":42}`,
			},
		},
	}
	got := c.buildResponsesRequest(req)
	if len(got.Input) != 1 {
		t.Fatalf("Input count = %d, want 1", len(got.Input))
	}
	if !got.Input[0].IsFunctionCallOutput() {
		t.Errorf("Type = %q, want function_call_output", got.Input[0].Type)
	}
	if got.Input[0].CallID != "call_a" {
		t.Errorf("CallID = %q, want call_a", got.Input[0].CallID)
	}
	if got.Input[0].Output != `{"result":42}` {
		t.Errorf("Output = %q, want %q", got.Input[0].Output, `{"result":42}`)
	}
}

// TestBuildResponsesRequest_ReasoningRecordEcho pins ADR-051 D3
// echo: ReasoningRecords on an assistant message become reasoning
// InputItems emitted right after the message's items.
func TestBuildResponsesRequest_ReasoningRecordEcho(t *testing.T) {
	c := newResponsesTestClient(t)
	req := agentic.AgentRequest{
		Messages: []agentic.ChatMessage{
			{
				Role: "assistant",
				ToolCalls: []agentic.ToolCall{
					{ID: "call_a", Name: "fn_a"},
				},
				ReasoningRecords: []agentic.ReasoningRecord{
					{
						Provider:    "openai",
						CarrierKind: agentic.ReasoningCarrierStandaloneItem,
						ItemID:      "rs_echo",
						Opaque:      []byte("echo-blob"),
						SummaryText: "echo summary",
					},
				},
			},
		},
	}
	got := c.buildResponsesRequest(req)
	if len(got.Input) != 2 {
		t.Fatalf("Input count = %d, want 2 (one function_call + one reasoning echo)", len(got.Input))
	}
	if !got.Input[0].IsFunctionCall() {
		t.Errorf("Input[0].Type = %q, want function_call", got.Input[0].Type)
	}
	if !got.Input[1].IsReasoning() {
		t.Errorf("Input[1].Type = %q, want reasoning", got.Input[1].Type)
	}
	if got.Input[1].ID != "rs_echo" {
		t.Errorf("Input[1].ID = %q, want rs_echo", got.Input[1].ID)
	}
	if got.Input[1].EncryptedContent != "echo-blob" {
		t.Errorf("Input[1].EncryptedContent = %q, want echo-blob", got.Input[1].EncryptedContent)
	}
}

// TestBuildResponsesRequest_NonOpenAIRecordsSkipped pins that
// records from other providers (e.g. Gemini ToolCall-carrier) are
// not echoed onto Responses input — they belong to a different wire
// path.
func TestBuildResponsesRequest_NonOpenAIRecordsSkipped(t *testing.T) {
	c := newResponsesTestClient(t)
	req := agentic.AgentRequest{
		Messages: []agentic.ChatMessage{
			{
				Role:    "assistant",
				Content: "hi",
				ReasoningRecords: []agentic.ReasoningRecord{
					{
						Provider:    "google",
						CarrierKind: agentic.ReasoningCarrierToolCall,
						ToolCallID:  "call-1",
						Opaque:      []byte("gemini-sig"),
					},
				},
			},
		},
	}
	got := c.buildResponsesRequest(req)
	for _, item := range got.Input {
		if item.IsReasoning() {
			t.Errorf("unexpected reasoning InputItem from Gemini record: %+v", item)
		}
	}
}

// TestBuildResponsesRequest_ToolDefinitionsFlatShape pins that
// tool definitions translate to the Responses flat shape, not the
// ChatCompletion nested {function:{...}} shape.
func TestBuildResponsesRequest_ToolDefinitionsFlatShape(t *testing.T) {
	c := newResponsesTestClient(t)
	req := agentic.AgentRequest{
		Messages: []agentic.ChatMessage{{Role: "user", Content: "hi"}},
		Tools: []agentic.ToolDefinition{
			{
				Name:        "multiply",
				Description: "Multiplies",
				Parameters:  map[string]any{"type": "object"},
				Strict:      true,
			},
		},
	}
	got := c.buildResponsesRequest(req)
	if len(got.Tools) != 1 {
		t.Fatalf("Tools count = %d, want 1", len(got.Tools))
	}
	tool := got.Tools[0]
	if tool.Type != "function" || tool.Name != "multiply" || tool.Description != "Multiplies" || !tool.Strict {
		t.Errorf("Tools[0] mismatch: %+v", tool)
	}
}

// TestBuildResponsesRequest_ToolChoiceForced pins forced-function
// shape: {type:"function", name:"..."} (no nested function object).
func TestBuildResponsesRequest_ToolChoiceForced(t *testing.T) {
	c := newResponsesTestClient(t)
	req := agentic.AgentRequest{
		Messages:   []agentic.ChatMessage{{Role: "user", Content: "hi"}},
		ToolChoice: &agentic.ToolChoice{Mode: "function", FunctionName: "go"},
	}
	got := c.buildResponsesRequest(req)
	var envelope map[string]any
	if err := json.Unmarshal(got.ToolChoice, &envelope); err != nil {
		t.Fatalf("decode ToolChoice: %v", err)
	}
	if envelope["type"] != "function" || envelope["name"] != "go" {
		t.Errorf("ToolChoice envelope = %v, want {type:function, name:go}", envelope)
	}
}

// TestBuildResponsesRequest_ReasoningEffortFromEndpoint pins that
// the endpoint's reasoning_effort flows into the Responses
// reasoning.effort field AND that include is set to opt into
// encrypted_content emission (without it the API silently omits
// the blob, breaking cross-turn echo). Both invariants caught by
// the ADR-051 PR 4 live reasoning-echo test.
func TestBuildResponsesRequest_ReasoningEffortFromEndpoint(t *testing.T) {
	c := newResponsesTestClient(t)
	c.endpoint.ReasoningEffort = "medium"
	got := c.buildResponsesRequest(agentic.AgentRequest{
		Messages: []agentic.ChatMessage{{Role: "user", Content: "hi"}},
	})
	if got.Reasoning == nil || got.Reasoning.Effort != "medium" {
		t.Errorf("Reasoning = %+v, want Effort=medium", got.Reasoning)
	}
	wantInclude := "reasoning.encrypted_content"
	found := false
	for _, s := range got.Include {
		if s == wantInclude {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Include = %v, want to contain %q (silent encrypted_content omission risk)",
			got.Include, wantInclude)
	}
}

// TestBuildResponsesRequest_ResponseFormatJSONSchema pins that
// JSON-schema response_format translates to the Responses text.format
// envelope.
func TestBuildResponsesRequest_ResponseFormatJSONSchema(t *testing.T) {
	c := newResponsesTestClient(t)
	got := c.buildResponsesRequest(agentic.AgentRequest{
		Messages: []agentic.ChatMessage{{Role: "user", Content: "hi"}},
		ResponseFormat: agentic.NewJSONSchemaFormat("my_schema", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"a": map[string]any{"type": "integer"},
			},
		}),
	})
	if got.Text == nil {
		t.Fatal("Text was nil; expected json_schema envelope")
	}
	var envelope map[string]any
	if err := json.Unmarshal(got.Text.Format, &envelope); err != nil {
		t.Fatalf("decode Text.Format: %v", err)
	}
	if envelope["type"] != "json_schema" {
		t.Errorf("type = %v, want json_schema", envelope["type"])
	}
	if envelope["name"] != "my_schema" {
		t.Errorf("name = %v, want my_schema", envelope["name"])
	}
	if envelope["strict"] != true {
		t.Errorf("strict = %v, want true", envelope["strict"])
	}
}

// TestBuildResponsesRequest_MaxOutputTokensFieldName pins that we
// emit the Responses-API field name max_output_tokens, NOT
// ChatCompletion's max_tokens. Silent wrong field name = the
// provider ignores the cap.
func TestBuildResponsesRequest_MaxOutputTokensFieldName(t *testing.T) {
	c := newResponsesTestClient(t)
	got := c.buildResponsesRequest(agentic.AgentRequest{
		Messages:  []agentic.ChatMessage{{Role: "user", Content: "hi"}},
		MaxTokens: 1024,
	})
	if got.MaxOutputTokens != 1024 {
		t.Errorf("MaxOutputTokens = %d, want 1024", got.MaxOutputTokens)
	}
	b, _ := json.Marshal(got)
	if !strings.Contains(string(b), `"max_output_tokens":1024`) {
		t.Errorf("expected max_output_tokens in wire body; got %s", string(b))
	}
	if strings.Contains(string(b), `"max_tokens"`) {
		t.Errorf("wire body must NOT contain max_tokens (ChatCompletion name); got %s", string(b))
	}
}
