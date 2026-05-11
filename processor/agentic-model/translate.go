package agenticmodel

import (
	"encoding/json"

	"github.com/c360studio/semstreams/model/wire"
	openai "github.com/sashabaranov/go-openai"
)

// SDK ⇆ wire translation helpers used at adapter call boundaries during
// the ADR-037 dual-backend phase. The SDK still owns the production
// request/response shape; the wire types are what the new ProviderAdapter
// interface speaks. When the wire-native client path lands (chunk 7),
// these helpers are no longer on the hot path — adapters call wire types
// directly. Until then, every adapter call site round-trips through here.
//
// Translation rules:
//   - SDK Content (Go string) ↔ wire Content (json.RawMessage holding a
//     JSON string). The SDK path never produces multi-content arrays,
//     so wire.Message.ContentParts is not exercised here.
//   - SDK Tool.Type / openai.ToolType is a string-based alias — cast
//     directly.
//   - SDK ResponseFormat.JSONSchema.Schema is json.Marshaler; serialize
//     once to capture the canonical bytes in wire.JSONSchema.Schema
//     (json.RawMessage).
//   - Extras on wire types is empty by construction here — SDK shape
//     doesn't have an Extras concept, so back-propagation of Extras to
//     SDK is a no-op (and would be silently lost if an adapter wrote
//     into wire Extras during NormalizeRequest/Response in chunk 6).
//     Chunk 8 adds the Extras-aware path for Gemini thought_signature.

// sdkMessagesToWire translates SDK chat messages to the wire shape.
func sdkMessagesToWire(in []openai.ChatCompletionMessage) []wire.Message {
	if in == nil {
		return nil
	}
	out := make([]wire.Message, len(in))
	for i, m := range in {
		out[i] = sdkMessageToWire(m)
	}
	return out
}

// wireMessagesToSDK is the inverse translation. Non-string wire content is
// dropped (the SDK path doesn't model multi-content; chunk 7 wire-native
// path owns that case).
func wireMessagesToSDK(in []wire.Message) []openai.ChatCompletionMessage {
	if in == nil {
		return nil
	}
	out := make([]openai.ChatCompletionMessage, len(in))
	for i, m := range in {
		out[i] = wireMessageToSDK(m)
	}
	return out
}

func sdkMessageToWire(m openai.ChatCompletionMessage) wire.Message {
	out := wire.Message{
		Role:             m.Role,
		Name:             m.Name,
		ReasoningContent: m.ReasoningContent,
		ToolCallID:       m.ToolCallID,
	}
	if m.Content != "" {
		_ = out.SetContentString(m.Content)
	}
	if len(m.ToolCalls) > 0 {
		out.ToolCalls = sdkToolCallsToWire(m.ToolCalls)
	}
	return out
}

func wireMessageToSDK(m wire.Message) openai.ChatCompletionMessage {
	out := openai.ChatCompletionMessage{
		Role:             m.Role,
		Name:             m.Name,
		ReasoningContent: m.ReasoningContent,
		ToolCallID:       m.ToolCallID,
	}
	if s, ok := m.ContentString(); ok {
		out.Content = s
	}
	if len(m.ToolCalls) > 0 {
		out.ToolCalls = wireToolCallsToSDK(m.ToolCalls)
	}
	return out
}

func sdkToolCallsToWire(in []openai.ToolCall) []wire.ToolCall {
	if in == nil {
		return nil
	}
	out := make([]wire.ToolCall, len(in))
	for i, tc := range in {
		out[i] = sdkToolCallToWire(tc)
	}
	return out
}

func wireToolCallsToSDK(in []wire.ToolCall) []openai.ToolCall {
	if in == nil {
		return nil
	}
	out := make([]openai.ToolCall, len(in))
	for i, tc := range in {
		out[i] = wireToolCallToSDK(tc)
	}
	return out
}

func sdkToolCallToWire(tc openai.ToolCall) wire.ToolCall {
	out := wire.ToolCall{
		ID:   tc.ID,
		Type: string(tc.Type),
		Function: wire.Function{
			Name:      tc.Function.Name,
			Arguments: tc.Function.Arguments,
		},
	}
	if tc.Index != nil {
		idx := *tc.Index
		out.Index = &idx
	}
	return out
}

func wireToolCallToSDK(tc wire.ToolCall) openai.ToolCall {
	out := openai.ToolCall{
		ID:   tc.ID,
		Type: openai.ToolType(tc.Type),
		Function: openai.FunctionCall{
			Name:      tc.Function.Name,
			Arguments: tc.Function.Arguments,
		},
	}
	if tc.Index != nil {
		idx := *tc.Index
		out.Index = &idx
	}
	return out
}

// sdkRequestToWire builds the wire-shape view of an SDK request body for
// adapter NormalizeRequest inspection. Carries only the fields adapters
// might inspect today: Model, Messages, Tools, ResponseFormat. Other
// SDK-side fields (Temperature, MaxTokens, Stop, ...) are intentionally
// omitted — no current adapter touches them, and adding them here is a
// chunk 7 concern when the wire-native client builds the full request
// directly.
func sdkRequestToWire(req *openai.ChatCompletionRequest) wire.ChatCompletionRequest {
	if req == nil {
		return wire.ChatCompletionRequest{}
	}
	out := wire.ChatCompletionRequest{
		Model:    req.Model,
		Messages: sdkMessagesToWire(req.Messages),
	}
	if len(req.Tools) > 0 {
		out.Tools = sdkToolsToWire(req.Tools)
	}
	if req.ResponseFormat != nil {
		out.ResponseFormat = sdkResponseFormatToWire(req.ResponseFormat)
	}
	return out
}

// applyWireRequestToSDK propagates fields an adapter may have mutated
// in NormalizeRequest back onto the SDK request. Only the fields the
// chunk 6 adapter surface can plausibly touch are written back; Extras
// is not re-encoded into the SDK request because SDK lacks an Extras
// carrier (chunk 8 wires the wire-native path for Extras).
func applyWireRequestToSDK(wreq wire.ChatCompletionRequest, req *openai.ChatCompletionRequest) {
	if req == nil {
		return
	}
	req.Messages = wireMessagesToSDK(wreq.Messages)
	if wreq.ResponseFormat == nil {
		req.ResponseFormat = nil
	} else {
		req.ResponseFormat = wireResponseFormatToSDK(wreq.ResponseFormat)
	}
}

func sdkToolsToWire(in []openai.Tool) []wire.Tool {
	if in == nil {
		return nil
	}
	out := make([]wire.Tool, len(in))
	for i, t := range in {
		out[i] = wire.Tool{Type: string(t.Type)}
		if t.Function != nil {
			out[i].Function = wire.FunctionDefinition{
				Name:        t.Function.Name,
				Description: t.Function.Description,
				Strict:      t.Function.Strict,
			}
			if t.Function.Parameters != nil {
				if b, err := json.Marshal(t.Function.Parameters); err == nil {
					out[i].Function.Parameters = b
				}
			}
		}
	}
	return out
}

func sdkResponseFormatToWire(rf *openai.ChatCompletionResponseFormat) *wire.ResponseFormat {
	if rf == nil {
		return nil
	}
	out := &wire.ResponseFormat{Type: string(rf.Type)}
	if rf.JSONSchema != nil {
		out.JSONSchema = &wire.JSONSchema{
			Name:   rf.JSONSchema.Name,
			Strict: rf.JSONSchema.Strict,
		}
		if rf.JSONSchema.Schema != nil {
			if b, err := json.Marshal(rf.JSONSchema.Schema); err == nil {
				out.JSONSchema.Schema = b
			}
		}
	}
	return out
}

func wireResponseFormatToSDK(rf *wire.ResponseFormat) *openai.ChatCompletionResponseFormat {
	if rf == nil {
		return nil
	}
	out := &openai.ChatCompletionResponseFormat{
		Type: openai.ChatCompletionResponseFormatType(rf.Type),
	}
	if rf.JSONSchema != nil {
		out.JSONSchema = &openai.ChatCompletionResponseFormatJSONSchema{
			Name:   rf.JSONSchema.Name,
			Strict: rf.JSONSchema.Strict,
		}
		if len(rf.JSONSchema.Schema) > 0 {
			out.JSONSchema.Schema = json.RawMessage(rf.JSONSchema.Schema)
		}
	}
	return out
}

// sdkResponseToWire builds the wire-shape view of an SDK response for
// adapter NormalizeResponse inspection. Mirrors sdkRequestToWire's
// scoping: ID, Model, Choices (with Message + FinishReason), Usage.
func sdkResponseToWire(resp *openai.ChatCompletionResponse) wire.ChatCompletionResponse {
	if resp == nil {
		return wire.ChatCompletionResponse{}
	}
	out := wire.ChatCompletionResponse{
		ID:    resp.ID,
		Model: resp.Model,
		Usage: &wire.Usage{
			PromptTokens:     resp.Usage.PromptTokens,
			CompletionTokens: resp.Usage.CompletionTokens,
			TotalTokens:      resp.Usage.TotalTokens,
		},
	}
	if len(resp.Choices) > 0 {
		out.Choices = make([]wire.Choice, len(resp.Choices))
		for i, c := range resp.Choices {
			msg := sdkMessageToWire(c.Message)
			out.Choices[i] = wire.Choice{
				Index:        c.Index,
				Message:      &msg,
				FinishReason: string(c.FinishReason),
			}
		}
	}
	return out
}

// applyWireResponseToSDK propagates fields an adapter may have mutated
// in NormalizeResponse back onto the SDK response. Only Choices[].Message
// and FinishReason are written back today — those are the realistic
// adapter mutation points (e.g. provider-specific blob extraction into
// domain metadata).
func applyWireResponseToSDK(wresp wire.ChatCompletionResponse, resp *openai.ChatCompletionResponse) {
	if resp == nil {
		return
	}
	if len(wresp.Choices) != len(resp.Choices) {
		return
	}
	for i, wc := range wresp.Choices {
		if wc.Message != nil {
			resp.Choices[i].Message = wireMessageToSDK(*wc.Message)
		}
		if wc.FinishReason != "" {
			resp.Choices[i].FinishReason = openai.FinishReason(wc.FinishReason)
		}
	}
}
