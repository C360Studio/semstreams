package agenticmodel

import (
	"encoding/json"
	"testing"

	"github.com/c360studio/semstreams/model/wire"
	openai "github.com/sashabaranov/go-openai"
)

// TestSDKMessageToWire_RoundTrip verifies that a representative SDK
// message survives a sdkMessageToWire → wireMessageToSDK round-trip
// with its observable fields preserved.
func TestSDKMessageToWire_RoundTrip(t *testing.T) {
	in := openai.ChatCompletionMessage{
		Role:             "assistant",
		Content:          "hello world",
		Name:             "assistant_bot",
		ReasoningContent: "internal monologue",
		ToolCallID:       "",
		ToolCalls: []openai.ToolCall{
			{
				ID:   "call-1",
				Type: openai.ToolTypeFunction,
				Function: openai.FunctionCall{
					Name:      "read_file",
					Arguments: `{"path":"x"}`,
				},
			},
		},
	}
	out := wireMessageToSDK(sdkMessageToWire(in))

	if out.Role != in.Role {
		t.Errorf("Role: got %q want %q", out.Role, in.Role)
	}
	if out.Content != in.Content {
		t.Errorf("Content: got %q want %q", out.Content, in.Content)
	}
	if out.Name != in.Name {
		t.Errorf("Name: got %q want %q", out.Name, in.Name)
	}
	if out.ReasoningContent != in.ReasoningContent {
		t.Errorf("ReasoningContent: got %q want %q", out.ReasoningContent, in.ReasoningContent)
	}
	if len(out.ToolCalls) != 1 {
		t.Fatalf("ToolCalls: got %d want 1", len(out.ToolCalls))
	}
	if out.ToolCalls[0].ID != "call-1" {
		t.Errorf("ToolCall ID: got %q want call-1", out.ToolCalls[0].ID)
	}
	if out.ToolCalls[0].Function.Name != "read_file" {
		t.Errorf("ToolCall Function.Name: got %q want read_file", out.ToolCalls[0].Function.Name)
	}
	if out.ToolCalls[0].Function.Arguments != `{"path":"x"}` {
		t.Errorf("ToolCall Function.Arguments: got %q", out.ToolCalls[0].Function.Arguments)
	}
}

// TestSDKToolCallToWire_PreservesIndex verifies that an explicit Index
// pointer survives translation in both directions.
func TestSDKToolCallToWire_PreservesIndex(t *testing.T) {
	idx := 3
	in := openai.ToolCall{Index: &idx, ID: "call-x"}
	w := sdkToolCallToWire(in)
	if w.Index == nil || *w.Index != 3 {
		t.Fatalf("sdk→wire Index: got %v want 3", w.Index)
	}
	out := wireToolCallToSDK(w)
	if out.Index == nil || *out.Index != 3 {
		t.Fatalf("wire→sdk Index: got %v want 3", out.Index)
	}
	// Independent pointer storage — mutating the round-tripped value
	// must not alias back to the original.
	*out.Index = 99
	if *in.Index == 99 {
		t.Error("Index pointer aliased through round-trip (should be a copy)")
	}
}

// TestSDKToolCallToWire_NoIndex verifies that a missing Index pointer
// stays nil through translation.
func TestSDKToolCallToWire_NoIndex(t *testing.T) {
	in := openai.ToolCall{ID: "call-x"}
	w := sdkToolCallToWire(in)
	if w.Index != nil {
		t.Errorf("expected nil Index, got %v", w.Index)
	}
}

// TestSDKRequestToWire_CarriesAdapterFields verifies that the chunk-6
// shallow request translation carries the fields adapters can plausibly
// inspect: Model, Messages, Tools, ResponseFormat. Other fields
// (Temperature, MaxTokens) are intentionally omitted at this layer.
func TestSDKRequestToWire_CarriesAdapterFields(t *testing.T) {
	in := &openai.ChatCompletionRequest{
		Model: "test-model",
		Messages: []openai.ChatCompletionMessage{
			{Role: "user", Content: "hi"},
		},
		Tools: []openai.Tool{
			{
				Type: openai.ToolTypeFunction,
				Function: &openai.FunctionDefinition{
					Name:        "f",
					Description: "do thing",
					Strict:      true,
					Parameters: map[string]any{
						"type": "object",
					},
				},
			},
		},
		ResponseFormat: &openai.ChatCompletionResponseFormat{
			Type: openai.ChatCompletionResponseFormatTypeJSONSchema,
			JSONSchema: &openai.ChatCompletionResponseFormatJSONSchema{
				Name:   "x",
				Strict: true,
			},
		},
	}
	w := sdkRequestToWire(in)

	if w.Model != "test-model" {
		t.Errorf("Model: got %q", w.Model)
	}
	if len(w.Messages) != 1 || w.Messages[0].Role != "user" {
		t.Errorf("Messages not carried")
	}
	if len(w.Tools) != 1 || w.Tools[0].Function.Name != "f" {
		t.Errorf("Tools not carried: %+v", w.Tools)
	}
	if w.Tools[0].Function.Description != "do thing" {
		t.Errorf("Tool Description not carried")
	}
	if !w.Tools[0].Function.Strict {
		t.Errorf("Tool Strict not carried")
	}
	if len(w.Tools[0].Function.Parameters) == 0 {
		t.Error("Tool Parameters not carried")
	}
	if w.ResponseFormat == nil || w.ResponseFormat.Type != "json_schema" {
		t.Errorf("ResponseFormat not carried: %+v", w.ResponseFormat)
	}
}

// TestApplyWireRequestToSDK_PropagatesMessages verifies that adapter
// mutations to wire Messages flow back to the SDK request.
func TestApplyWireRequestToSDK_PropagatesMessages(t *testing.T) {
	sdk := &openai.ChatCompletionRequest{
		Model: "m",
		Messages: []openai.ChatCompletionMessage{
			{Role: "user", Content: "original"},
		},
	}
	w := sdkRequestToWire(sdk)
	// Simulate an adapter rewriting the message.
	_ = w.Messages[0].SetContentString("mutated")

	applyWireRequestToSDK(w, sdk)

	if len(sdk.Messages) != 1 || sdk.Messages[0].Content != "mutated" {
		t.Errorf("expected sdk Messages[0].Content = mutated, got %+v", sdk.Messages)
	}
}

// TestSDKResponseToWire_CarriesMessage verifies that response Choices
// (Message + FinishReason) are translated for adapter inspection.
func TestSDKResponseToWire_CarriesMessage(t *testing.T) {
	in := &openai.ChatCompletionResponse{
		ID:    "resp-1",
		Model: "test-model",
		Choices: []openai.ChatCompletionChoice{
			{
				Index: 0,
				Message: openai.ChatCompletionMessage{
					Role:    "assistant",
					Content: "answer",
				},
				FinishReason: openai.FinishReasonStop,
			},
		},
		Usage: openai.Usage{PromptTokens: 5, CompletionTokens: 7, TotalTokens: 12},
	}
	w := sdkResponseToWire(in)

	if w.ID != "resp-1" || w.Model != "test-model" {
		t.Errorf("identity fields lost: id=%q model=%q", w.ID, w.Model)
	}
	if len(w.Choices) != 1 || w.Choices[0].Message == nil {
		t.Fatalf("choices not carried")
	}
	s, _ := w.Choices[0].Message.ContentString()
	if s != "answer" {
		t.Errorf("Message.Content: got %q want answer", s)
	}
	if w.Choices[0].FinishReason != "stop" {
		t.Errorf("FinishReason: got %q want stop", w.Choices[0].FinishReason)
	}
	if w.Usage == nil || w.Usage.PromptTokens != 5 || w.Usage.CompletionTokens != 7 {
		t.Errorf("Usage not carried: %+v", w.Usage)
	}
}

// TestApplyWireResponseToSDK_PropagatesMessage verifies that adapter
// mutations to wire Choices[].Message flow back to the SDK response.
func TestApplyWireResponseToSDK_PropagatesMessage(t *testing.T) {
	sdk := &openai.ChatCompletionResponse{
		Choices: []openai.ChatCompletionChoice{
			{
				Message:      openai.ChatCompletionMessage{Role: "assistant", Content: "original"},
				FinishReason: openai.FinishReasonStop,
			},
		},
	}
	w := sdkResponseToWire(sdk)
	_ = w.Choices[0].Message.SetContentString("mutated")
	w.Choices[0].FinishReason = "tool_calls"

	applyWireResponseToSDK(w, sdk)

	if sdk.Choices[0].Message.Content != "mutated" {
		t.Errorf("expected Choices[0].Message.Content = mutated, got %q", sdk.Choices[0].Message.Content)
	}
	if sdk.Choices[0].FinishReason != openai.FinishReasonToolCalls {
		t.Errorf("expected FinishReason tool_calls, got %q", sdk.Choices[0].FinishReason)
	}
}

// TestResponseFormat_RoundTrip verifies that response_format with a JSON
// schema survives sdk → wire → sdk translation.
func TestResponseFormat_RoundTrip(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"x": map[string]any{"type": "string"},
		},
	}
	in := &openai.ChatCompletionResponseFormat{
		Type: openai.ChatCompletionResponseFormatTypeJSONSchema,
		JSONSchema: &openai.ChatCompletionResponseFormatJSONSchema{
			Name:   "thing",
			Strict: true,
			Schema: jsonRawMessageFromMap(schema),
		},
	}
	w := sdkResponseFormatToWire(in)
	if w == nil {
		t.Fatal("nil wire ResponseFormat")
	}
	if string(w.JSONSchema.Schema) == "" {
		t.Error("schema bytes lost")
	}
	out := wireResponseFormatToSDK(w)
	if out == nil || out.JSONSchema == nil {
		t.Fatal("nil sdk after round-trip")
	}
	if out.JSONSchema.Name != "thing" {
		t.Errorf("Name: got %q", out.JSONSchema.Name)
	}
	if !out.JSONSchema.Strict {
		t.Error("Strict lost")
	}
}

// jsonRawMessageFromMap is a tiny helper to provide a json.Marshaler
// schema value matching the SDK's wire shape.
func jsonRawMessageFromMap(m map[string]any) json.RawMessage {
	b, err := json.Marshal(m)
	if err != nil {
		panic(err)
	}
	return b
}

// TestWireMessagesToSDK_DropsArrayContent verifies that wire content
// arrays (typed parts) are dropped at the SDK boundary — the chunk-6
// SDK path doesn't model multi-content, and silent drop matches the
// existing behavior (rather than producing malformed JSON).
func TestWireMessagesToSDK_DropsArrayContent(t *testing.T) {
	m := wire.Message{Role: "user"}
	m.Content = json.RawMessage(`[{"type":"text","text":"hi"}]`)
	out := wireMessagesToSDK([]wire.Message{m})
	if out[0].Content != "" {
		t.Errorf("expected empty Content for array, got %q", out[0].Content)
	}
}
