package agenticmodel

import (
	"encoding/json"
	"log/slog"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/model/wire/responses"
)

// buildResponsesRequest converts an AgentRequest into a responses.Request,
// translating the ChatCompletion-shaped message conversation into the
// Responses typed-array input shape and reshaping reasoning records
// for echo. Adapter NormalizeRequest runs after translation; the
// echo step lives at this seam so adapter hooks operate on a fully-
// formed request.
//
// Translation table:
//
//   - role=system → role=developer InputItem (explicit translation
//     per ADR-051 open question 2; avoids relying on silent provider
//     compat).
//   - role=user → role=user InputItem with input_text content.
//   - role=assistant with content → role=assistant InputItem with
//     output_text content.
//   - role=assistant with tool_calls → emit each tool_call as a
//     standalone function_call InputItem (echoes the OpenAI
//     Responses shape where assistant tool calls are top-level items,
//     not nested inside a message).
//   - role=tool → function_call_output InputItem keyed by
//     ToolCallID.
//   - ReasoningRecords from the agentic message → reasoning
//     InputItems via the ResponsesAdapter's EchoReasoningRecords
//     hook. Echoed AFTER the message they were captured from, in
//     the order the API recommends.
func (c *Client) buildResponsesRequest(req agentic.AgentRequest) responses.Request {
	if len(req.Messages) == 0 {
		if c.logger != nil {
			c.logger.Warn("buildResponsesRequest called with empty messages",
				slog.String("request_id", req.RequestID),
				slog.String("loop_id", req.LoopID))
		}
		return responses.Request{Model: c.endpoint.Model}
	}

	adapter := c.getResponsesAdapter()
	out := responses.Request{
		Model: c.endpoint.Model,
	}

	// Stateless mode (ADR-051 D2): always set Store=false so the
	// caller owns full history and echoes reasoning items per turn.
	storeFalse := false
	out.Store = &storeFalse

	for _, msg := range req.Messages {
		input := agenticMessageToResponses(msg)
		out.Input = append(out.Input, input...)
		echoed := adapter.EchoReasoningRecords(msg.ReasoningRecords)
		out.Input = append(out.Input, echoed...)
	}

	c.applyResponsesRequestParams(&out, req)
	if req.ResponseFormat != nil {
		out.Text = agenticResponseFormatToResponsesText(req.ResponseFormat, req.RequestID, c.logger)
	}
	if len(req.Tools) > 0 {
		out.Tools = agenticToolsToResponses(req.Tools)
	}
	if req.ToolChoice != nil {
		out.ToolChoice = agenticToolChoiceToResponses(req.ToolChoice)
	}

	adapter.NormalizeRequest(&out)
	return out
}

// agenticMessageToResponses translates one agentic ChatMessage into
// zero or more Responses InputItems per the table above. Tool
// messages produce a single function_call_output; assistant
// messages with tool_calls produce one function_call per call (and
// optionally a message InputItem if there's also content).
func agenticMessageToResponses(msg agentic.ChatMessage) []responses.InputItem {
	switch msg.Role {
	case "system":
		// Explicit translation: avoid silent provider compat.
		return []responses.InputItem{responses.NewInputDeveloperMessage(msg.Content)}

	case "user":
		return []responses.InputItem{responses.NewInputUserMessage(msg.Content)}

	case "assistant":
		var out []responses.InputItem
		if msg.Content != "" {
			out = append(out, responses.InputItem{
				Type: responses.ItemTypeMessage,
				Role: responses.RoleAssistant,
				Content: []responses.ContentPart{
					{Type: responses.ContentTypeOutputText, Text: msg.Content},
				},
			})
		}
		for _, tc := range msg.ToolCalls {
			argsJSON := ""
			if len(tc.Arguments) > 0 {
				if b, err := json.Marshal(tc.Arguments); err == nil {
					argsJSON = string(b)
				}
			}
			out = append(out, responses.NewInputFunctionCall(tc.ID, tc.Name, argsJSON))
		}
		return out

	case "tool":
		return []responses.InputItem{responses.NewInputFunctionCallOutput(msg.ToolCallID, msg.Content)}

	default:
		// Forward-compat: emit a developer message so the model sees
		// the content rather than dropping it silently.
		return []responses.InputItem{responses.NewInputDeveloperMessage(msg.Content)}
	}
}

// applyResponsesRequestParams writes the scalar sampling controls
// from AgentRequest + endpoint config onto the responses.Request.
// Mirrors applyWireRequestParams. Field names are the Responses-API
// names ("max_output_tokens", not "max_tokens").
func (c *Client) applyResponsesRequestParams(out *responses.Request, req agentic.AgentRequest) {
	switch {
	case req.MaxTokens > 0:
		out.MaxOutputTokens = req.MaxTokens
	case c.endpoint.MaxOutputTokens > 0:
		out.MaxOutputTokens = c.endpoint.MaxOutputTokens
	}
	if req.Temperature > 0 {
		t := req.Temperature
		out.Temperature = &t
	}
	if c.endpoint.ReasoningEffort != "" {
		out.Reasoning = &responses.ReasoningParams{Effort: c.endpoint.ReasoningEffort}
		// Opt into encrypted_content emission on reasoning output
		// items. Without this, the API omits encrypted_content and
		// cross-turn echo loses the opaque blob — a silent break
		// caught by the ADR-051 PR 4 live reasoning-echo test.
		out.Include = appendUnique(out.Include, "reasoning.encrypted_content")
	}
}

// appendUnique appends v to dst if not already present.
func appendUnique(dst []string, v string) []string {
	for _, s := range dst {
		if s == v {
			return dst
		}
	}
	return append(dst, v)
}

// agenticResponseFormatToResponsesText translates agentic.ResponseFormat
// into the Responses-shape text.format envelope. Distinct from
// ChatCompletion's top-level response_format — Responses nests
// format under text.
//
// Phase 1 supports json_schema and json_object via a minimal hand-
// shaped envelope; the schema bytes flow through json.RawMessage.
// A marshal failure on rf.Schema is surfaced via Warn — silent
// drop would surface as an upstream generic 400 with "schema must
// be object" instead of the real root cause.
func agenticResponseFormatToResponsesText(rf *agentic.ResponseFormat, requestID string, logger *slog.Logger) *responses.TextParams {
	if rf == nil {
		return nil
	}
	switch rf.Type {
	case agentic.ResponseFormatJSONObject:
		return &responses.TextParams{
			Format: json.RawMessage(`{"type":"json_object"}`),
		}
	case agentic.ResponseFormatJSONSchema:
		envelope := map[string]any{
			"type":   "json_schema",
			"name":   rf.Name,
			"strict": rf.Strict,
		}
		if len(rf.Schema) > 0 {
			envelope["schema"] = rf.Schema
		}
		b, err := json.Marshal(envelope)
		if err != nil {
			if logger != nil {
				logger.Warn("response_format schema failed to marshal; upstream will reject with a generic schema error",
					slog.String("request_id", requestID),
					slog.String("name", rf.Name),
					slog.Any("err", err))
			}
			return nil
		}
		return &responses.TextParams{Format: b}
	default:
		return nil
	}
}

// agenticToolsToResponses translates agentic tool definitions to
// the Responses-shape flat layout ({type, name, description,
// parameters, strict}). Distinct from ChatCompletion's nested
// {type:"function", function:{...}}.
func agenticToolsToResponses(in []agentic.ToolDefinition) []responses.Tool {
	out := make([]responses.Tool, len(in))
	for i, t := range in {
		params, _ := json.Marshal(t.Parameters)
		out[i] = responses.Tool{
			Type:        "function",
			Name:        t.Name,
			Description: t.Description,
			Parameters:  params,
			Strict:      t.Strict,
		}
	}
	return out
}

// agenticToolChoiceToResponses translates agentic.ToolChoice to the
// Responses-shape JSON. The string values are identical to
// ChatCompletion ("auto", "required", "none"); the forced-function
// shape uses {type:"function", name:"..."} (no nested function
// object).
func agenticToolChoiceToResponses(tc *agentic.ToolChoice) json.RawMessage {
	if tc == nil {
		return nil
	}
	switch tc.Mode {
	case "auto", "required", "none":
		if b, err := json.Marshal(tc.Mode); err == nil {
			return b
		}
	case "function":
		envelope := map[string]any{
			"type": "function",
			"name": tc.FunctionName,
		}
		if b, err := json.Marshal(envelope); err == nil {
			return b
		}
	}
	return nil
}
