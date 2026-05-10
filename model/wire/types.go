package wire

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"sync"
)

// ChatCompletionRequest is the wire-shape ChatCompletion request body.
// Field names match OpenAI's documented chat-completions API. Any
// provider-specific fields not modeled here are carried in Extras.
type ChatCompletionRequest struct {
	Model            string                     `json:"model"`
	Messages         []Message                  `json:"messages"`
	Tools            []Tool                     `json:"tools,omitempty"`
	ToolChoice       json.RawMessage            `json:"tool_choice,omitempty"`
	ResponseFormat   *ResponseFormat            `json:"response_format,omitempty"`
	Stream           bool                       `json:"stream,omitempty"`
	StreamOptions    *StreamOptions             `json:"stream_options,omitempty"`
	MaxTokens        int                        `json:"max_tokens,omitempty"`
	Temperature      *float64                   `json:"temperature,omitempty"`
	TopP             *float64                   `json:"top_p,omitempty"`
	N                int                        `json:"n,omitempty"`
	Stop             []string                   `json:"stop,omitempty"`
	PresencePenalty  *float64                   `json:"presence_penalty,omitempty"`
	FrequencyPenalty *float64                   `json:"frequency_penalty,omitempty"`
	User             string                     `json:"user,omitempty"`
	Seed             *int64                     `json:"seed,omitempty"`
	ReasoningEffort  string                     `json:"reasoning_effort,omitempty"`
	Extras           map[string]json.RawMessage `json:"-"`
}

// MarshalJSON implements json.Marshaler with Extras merge.
func (r ChatCompletionRequest) MarshalJSON() ([]byte, error) {
	return marshalWithExtras(r, r.Extras)
}

// UnmarshalJSON implements json.Unmarshaler with Extras split.
func (r *ChatCompletionRequest) UnmarshalJSON(data []byte) error {
	return unmarshalWithExtras(data, r, &r.Extras)
}

// StreamOptions controls server-side streaming behavior.
type StreamOptions struct {
	IncludeUsage bool `json:"include_usage,omitempty"`
}

// ChatCompletionResponse is the non-streaming response shape.
type ChatCompletionResponse struct {
	ID                string                     `json:"id"`
	Object            string                     `json:"object,omitempty"`
	Created           int64                      `json:"created,omitempty"`
	Model             string                     `json:"model"`
	Choices           []Choice                   `json:"choices"`
	Usage             *Usage                     `json:"usage,omitempty"`
	SystemFingerprint string                     `json:"system_fingerprint,omitempty"`
	Extras            map[string]json.RawMessage `json:"-"`
}

// MarshalJSON implements json.Marshaler with Extras merge.
func (r ChatCompletionResponse) MarshalJSON() ([]byte, error) {
	return marshalWithExtras(r, r.Extras)
}

// UnmarshalJSON implements json.Unmarshaler with Extras split.
func (r *ChatCompletionResponse) UnmarshalJSON(data []byte) error {
	return unmarshalWithExtras(data, r, &r.Extras)
}

// Choice is a single response candidate.
type Choice struct {
	Index        int                        `json:"index"`
	Message      *Message                   `json:"message,omitempty"`
	Delta        *Message                   `json:"delta,omitempty"`
	FinishReason string                     `json:"finish_reason,omitempty"`
	Logprobs     json.RawMessage            `json:"logprobs,omitempty"`
	Extras       map[string]json.RawMessage `json:"-"`
}

// MarshalJSON implements json.Marshaler with Extras merge.
func (c Choice) MarshalJSON() ([]byte, error) {
	return marshalWithExtras(c, c.Extras)
}

// UnmarshalJSON implements json.Unmarshaler with Extras split.
func (c *Choice) UnmarshalJSON(data []byte) error {
	return unmarshalWithExtras(data, c, &c.Extras)
}

// Usage reports prompt + completion token counts.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// StreamChunk is a single Server-Sent-Events frame from a streaming
// ChatCompletion. Choices carry deltas; the final frame typically has
// a finish_reason set on each choice.
type StreamChunk struct {
	ID                string                     `json:"id,omitempty"`
	Object            string                     `json:"object,omitempty"`
	Created           int64                      `json:"created,omitempty"`
	Model             string                     `json:"model,omitempty"`
	Choices           []Choice                   `json:"choices"`
	Usage             *Usage                     `json:"usage,omitempty"`
	SystemFingerprint string                     `json:"system_fingerprint,omitempty"`
	Extras            map[string]json.RawMessage `json:"-"`
}

// MarshalJSON implements json.Marshaler with Extras merge.
func (s StreamChunk) MarshalJSON() ([]byte, error) {
	return marshalWithExtras(s, s.Extras)
}

// UnmarshalJSON implements json.Unmarshaler with Extras split.
func (s *StreamChunk) UnmarshalJSON(data []byte) error {
	return unmarshalWithExtras(data, s, &s.Extras)
}

// marshalWithExtras serializes v through its anonymous-alias type
// (stripping methods to avoid recursion) and merges extras into the
// resulting JSON object. Returns an error if any extras key collides
// with a known field — the caller authored a bug.
func marshalWithExtras(v any, extras map[string]json.RawMessage) ([]byte, error) {
	base, err := marshalAlias(v)
	if err != nil {
		return nil, err
	}
	if len(extras) == 0 {
		return base, nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(base, &fields); err != nil {
		return nil, fmt.Errorf("wire: re-decode for extras merge: %w", err)
	}
	for k, raw := range extras {
		if _, exists := fields[k]; exists {
			return nil, fmt.Errorf("wire: extras key %q collides with known field on %T", k, v)
		}
		fields[k] = raw
	}
	return json.Marshal(fields)
}

// unmarshalWithExtras decodes data into v through its anonymous-alias
// type, then walks the raw map to populate *extras with any keys that
// don't appear in the struct's json tags.
func unmarshalWithExtras(data []byte, v any, extras *map[string]json.RawMessage) error {
	if err := unmarshalAlias(data, v); err != nil {
		return err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	known := knownFieldsOf(v)
	for k, val := range raw {
		if known[k] {
			continue
		}
		if *extras == nil {
			*extras = make(map[string]json.RawMessage)
		}
		(*extras)[k] = val
	}
	return nil
}

// marshalAlias marshals v via an anonymous type that has the same
// fields but no methods, breaking the recursion that would otherwise
// re-enter MarshalJSON. Operates on a value (not pointer); callers
// should pass v by value. Returns the canonical JSON byte slice.
func marshalAlias(v any) ([]byte, error) {
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Pointer {
		rv = rv.Elem()
	}
	aliasType := stripMethodsType(rv.Type())
	alias := reflect.New(aliasType).Elem()
	alias.Set(rv.Convert(aliasType))
	return json.Marshal(alias.Interface())
}

// unmarshalAlias decodes data into v via the same method-stripping
// trick.
func unmarshalAlias(data []byte, v any) error {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return fmt.Errorf("wire: unmarshalAlias requires non-nil pointer, got %T", v)
	}
	elem := rv.Elem()
	aliasType := stripMethodsType(elem.Type())
	alias := reflect.New(aliasType)
	if err := json.Unmarshal(data, alias.Interface()); err != nil {
		return err
	}
	elem.Set(alias.Elem().Convert(elem.Type()))
	return nil
}

// stripMethodsCache memoizes the alias types we synthesize. Keyed by
// the original type; populated lazily from stripMethodsType.
var stripMethodsCache sync.Map // map[reflect.Type]reflect.Type

// stripMethodsType returns a struct type with the same fields as t
// but without any methods, breaking method-set inheritance for the
// purposes of marshal/unmarshal recursion. Result is cached. Panics
// on non-struct kinds; this is a programming error in callers
// inside the package, not a runtime concern.
func stripMethodsType(t reflect.Type) reflect.Type {
	if t.Kind() != reflect.Struct {
		panic(fmt.Sprintf("wire: stripMethodsType requires struct kind, got %s", t.Kind()))
	}
	if cached, ok := stripMethodsCache.Load(t); ok {
		return cached.(reflect.Type)
	}
	fields := make([]reflect.StructField, t.NumField())
	for i := range t.NumField() {
		f := t.Field(i)
		fields[i] = reflect.StructField{
			Name:      f.Name,
			Type:      f.Type,
			Tag:       f.Tag,
			Anonymous: f.Anonymous,
		}
	}
	out := reflect.StructOf(fields)
	stripMethodsCache.Store(t, out)
	return out
}

// knownFieldsCache memoizes the json-tag set per struct type.
var knownFieldsCache sync.Map // map[reflect.Type]map[string]bool

// knownFieldsOf returns the set of JSON keys present in v's struct
// tags. Tags equal to "-" are excluded; the first comma-separated
// segment is taken as the key. Result is cached.
func knownFieldsOf(v any) map[string]bool {
	t := reflect.TypeOf(v)
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if cached, ok := knownFieldsCache.Load(t); ok {
		return cached.(map[string]bool)
	}
	fields := make(map[string]bool, t.NumField())
	for i := range t.NumField() {
		f := t.Field(i)
		tag := f.Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		name := strings.SplitN(tag, ",", 2)[0]
		if name != "" && name != "-" {
			fields[name] = true
		}
	}
	knownFieldsCache.Store(t, fields)
	return fields
}

// trimJSONLeading strips leading JSON whitespace (space, tab, \n, \r)
// without allocating. Used to peek at the first non-whitespace byte.
func trimJSONLeading(b []byte) []byte {
	for i := range b {
		switch b[i] {
		case ' ', '\t', '\n', '\r':
			continue
		default:
			return b[i:]
		}
	}
	return nil
}
