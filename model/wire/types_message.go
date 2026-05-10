package wire

import (
	"encoding/json"
	"fmt"
)

// Message is a single message in a chat conversation.
//
// Content is json.RawMessage to honor OpenAI's polymorphism: it may be
// a JSON string ("hello") or a JSON array of content parts ([{...}]).
// Use Content.IsString / AsString / AsParts helpers to consume it.
type Message struct {
	Role             string                     `json:"role"`
	Content          json.RawMessage            `json:"content,omitempty"`
	Name             string                     `json:"name,omitempty"`
	ReasoningContent string                     `json:"reasoning_content,omitempty"`
	ToolCalls        []ToolCall                 `json:"tool_calls,omitempty"`
	ToolCallID       string                     `json:"tool_call_id,omitempty"`
	Refusal          string                     `json:"refusal,omitempty"`
	Extras           map[string]json.RawMessage `json:"-"`
}

// MarshalJSON implements json.Marshaler with Extras merge.
func (m Message) MarshalJSON() ([]byte, error) {
	return marshalWithExtras(m, m.Extras)
}

// UnmarshalJSON implements json.Unmarshaler with Extras split.
func (m *Message) UnmarshalJSON(data []byte) error {
	return unmarshalWithExtras(data, m, &m.Extras)
}

// ContentString returns the message's content as a plain string when
// it is a JSON string. ok is false if the content is empty, an array,
// or otherwise non-string.
func (m Message) ContentString() (s string, ok bool) {
	if len(m.Content) == 0 {
		return "", false
	}
	if err := json.Unmarshal(m.Content, &s); err != nil {
		return "", false
	}
	return s, true
}

// ContentParts returns the message's content as a list of typed parts
// when it is a JSON array. ok is false if the content is empty, a
// string, or otherwise non-array. The error is non-nil only when the
// content is array-shaped but malformed.
func (m Message) ContentParts() (parts []ContentPart, ok bool, err error) {
	if len(m.Content) == 0 {
		return nil, false, nil
	}
	trimmed := trimJSONLeading(m.Content)
	if len(trimmed) == 0 || trimmed[0] != '[' {
		return nil, false, nil
	}
	if err := json.Unmarshal(m.Content, &parts); err != nil {
		return nil, true, fmt.Errorf("wire: content array malformed: %w", err)
	}
	return parts, true, nil
}

// SetContentString writes a plain JSON string into Content.
func (m *Message) SetContentString(s string) error {
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	m.Content = b
	return nil
}

// ContentPart is one element of a content-array message (e.g. text,
// image_url). The shape is provider-defined; Extras preserves any
// fields not in the canonical OpenAI subset.
type ContentPart struct {
	Type     string                     `json:"type"`
	Text     string                     `json:"text,omitempty"`
	ImageURL *ContentImageURL           `json:"image_url,omitempty"`
	Extras   map[string]json.RawMessage `json:"-"`
}

// MarshalJSON implements json.Marshaler with Extras merge.
func (p ContentPart) MarshalJSON() ([]byte, error) {
	return marshalWithExtras(p, p.Extras)
}

// UnmarshalJSON implements json.Unmarshaler with Extras split.
func (p *ContentPart) UnmarshalJSON(data []byte) error {
	return unmarshalWithExtras(data, p, &p.Extras)
}

// ContentImageURL is the image-url subshape of a content part.
type ContentImageURL struct {
	URL    string `json:"url"`
	Detail string `json:"detail,omitempty"`
}

// ToolCall represents a single function/tool invocation.
type ToolCall struct {
	Index    *int                       `json:"index,omitempty"`
	ID       string                     `json:"id,omitempty"`
	Type     string                     `json:"type,omitempty"`
	Function Function                   `json:"function"`
	Extras   map[string]json.RawMessage `json:"-"`
}

// MarshalJSON implements json.Marshaler with Extras merge.
func (t ToolCall) MarshalJSON() ([]byte, error) {
	return marshalWithExtras(t, t.Extras)
}

// UnmarshalJSON implements json.Unmarshaler with Extras split.
func (t *ToolCall) UnmarshalJSON(data []byte) error {
	return unmarshalWithExtras(data, t, &t.Extras)
}

// Function describes a function call's name and arguments. Arguments
// is a JSON-encoded string per OpenAI's wire shape.
type Function struct {
	Name      string                     `json:"name,omitempty"`
	Arguments string                     `json:"arguments,omitempty"`
	Extras    map[string]json.RawMessage `json:"-"`
}

// MarshalJSON implements json.Marshaler with Extras merge.
func (f Function) MarshalJSON() ([]byte, error) {
	return marshalWithExtras(f, f.Extras)
}

// UnmarshalJSON implements json.Unmarshaler with Extras split.
func (f *Function) UnmarshalJSON(data []byte) error {
	return unmarshalWithExtras(data, f, &f.Extras)
}

// Tool is a tool definition available to the model.
type Tool struct {
	Type     string             `json:"type"`
	Function FunctionDefinition `json:"function"`
}

// FunctionDefinition is the schema for a tool function.
type FunctionDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
	Strict      bool            `json:"strict,omitempty"`
}

// ResponseFormat constrains the output shape (ADR-034).
type ResponseFormat struct {
	Type       string      `json:"type"`
	JSONSchema *JSONSchema `json:"json_schema,omitempty"`
}

// JSONSchema is the schema container for a response_format with
// type=json_schema.
type JSONSchema struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Schema      json.RawMessage `json:"schema,omitempty"`
	Strict      bool            `json:"strict,omitempty"`
}
