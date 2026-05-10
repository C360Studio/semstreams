package wire

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestMessage_RoundTripPreservesExtras(t *testing.T) {
	t.Parallel()
	in := []byte(`{
		"role":"assistant",
		"content":"hello",
		"tool_calls":[
			{"id":"c1","type":"function","function":{"name":"f","arguments":"{}"},
			 "extra_content":{"google":{"thought_signature":"abc"}}}
		],
		"unknown_top_level":{"foo":1}
	}`)

	var m Message
	if err := json.Unmarshal(in, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got, ok := m.Extras["unknown_top_level"]; !ok {
		t.Errorf("expected unknown_top_level in Extras, got %v", m.Extras)
	} else {
		var foo struct {
			Foo int `json:"foo"`
		}
		if err := json.Unmarshal(got, &foo); err != nil || foo.Foo != 1 {
			t.Errorf("Extras unknown_top_level wrong: %v err=%v", string(got), err)
		}
	}

	if len(m.ToolCalls) != 1 {
		t.Fatalf("want 1 tool call, got %d", len(m.ToolCalls))
	}
	tc := m.ToolCalls[0]
	if got, ok := tc.Extras["extra_content"]; !ok {
		t.Errorf("expected extra_content on tool_call, got %v", tc.Extras)
	} else if !strings.Contains(string(got), "thought_signature") {
		t.Errorf("extra_content payload missing thought_signature: %s", got)
	}

	out, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var roundTripped Message
	if err := json.Unmarshal(out, &roundTripped); err != nil {
		t.Fatalf("re-unmarshal: %v", err)
	}
	if !reflect.DeepEqual(roundTripped.Extras, m.Extras) {
		t.Errorf("Extras drift: before=%v after=%v", m.Extras, roundTripped.Extras)
	}
	if len(roundTripped.ToolCalls) != 1 ||
		!reflect.DeepEqual(roundTripped.ToolCalls[0].Extras, tc.Extras) {
		t.Errorf("tool_call Extras drift: before=%v after=%v", tc.Extras, roundTripped.ToolCalls[0].Extras)
	}
}

func TestRequest_RoundTrip(t *testing.T) {
	t.Parallel()
	temp := 0.7
	req := ChatCompletionRequest{
		Model:       "gpt-4o",
		Messages:    []Message{{Role: "user"}},
		Temperature: &temp,
		Extras: map[string]json.RawMessage{
			"safety_settings": json.RawMessage(`{"harm":"block_none"}`),
		},
	}
	if err := req.Messages[0].SetContentString("hi"); err != nil {
		t.Fatalf("SetContentString: %v", err)
	}

	b, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(b), `"safety_settings":{"harm":"block_none"}`) {
		t.Errorf("safety_settings missing in marshalled output: %s", b)
	}

	var got ChatCompletionRequest
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Model != "gpt-4o" {
		t.Errorf("model drift: %q", got.Model)
	}
	if got.Temperature == nil || *got.Temperature != 0.7 {
		t.Errorf("temperature drift: %v", got.Temperature)
	}
	if !reflect.DeepEqual(got.Extras, req.Extras) {
		t.Errorf("Extras drift: %v vs %v", got.Extras, req.Extras)
	}
}

func TestExtrasCollisionRejected(t *testing.T) {
	t.Parallel()
	m := Message{
		Role: "user",
		Extras: map[string]json.RawMessage{
			"role": json.RawMessage(`"injected"`),
		},
	}
	_, err := json.Marshal(m)
	if err == nil {
		t.Fatal("expected collision error, got nil")
	}
	if !strings.Contains(err.Error(), "collides") {
		t.Errorf("error wording unexpected: %v", err)
	}
}

func TestContentPolymorphism(t *testing.T) {
	t.Parallel()

	// String shape.
	in := []byte(`{"role":"user","content":"hello"}`)
	var m Message
	if err := json.Unmarshal(in, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if s, ok := m.ContentString(); !ok || s != "hello" {
		t.Errorf("ContentString got (%q,%v), want (hello,true)", s, ok)
	}
	if parts, ok, err := m.ContentParts(); ok || err != nil {
		t.Errorf("ContentParts got (%v,%v,%v), want (nil,false,nil)", parts, ok, err)
	}

	// Array shape.
	in = []byte(`{"role":"user","content":[
		{"type":"text","text":"see"},
		{"type":"image_url","image_url":{"url":"https://x/y.png"}}
	]}`)
	var m2 Message
	if err := json.Unmarshal(in, &m2); err != nil {
		t.Fatalf("unmarshal array: %v", err)
	}
	parts, ok, err := m2.ContentParts()
	if err != nil || !ok {
		t.Fatalf("ContentParts got (ok=%v err=%v), want (true,nil)", ok, err)
	}
	if len(parts) != 2 || parts[0].Type != "text" || parts[1].ImageURL.URL != "https://x/y.png" {
		t.Errorf("parts decoded wrong: %+v", parts)
	}
	if _, ok := m2.ContentString(); ok {
		t.Errorf("ContentString unexpectedly succeeded on array content")
	}
}

func TestResponse_ExtrasRoundTrip(t *testing.T) {
	t.Parallel()
	in := []byte(`{
		"id":"r1","model":"gpt-4o",
		"choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"x"},
		            "provider_field":"banana"}],
		"x_provider_extension":42
	}`)

	var resp ChatCompletionResponse
	if err := json.Unmarshal(in, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if string(resp.Extras["x_provider_extension"]) != "42" {
		t.Errorf("top-level Extras lost: %v", resp.Extras)
	}
	if len(resp.Choices) != 1 {
		t.Fatalf("want 1 choice")
	}
	if string(resp.Choices[0].Extras["provider_field"]) != `"banana"` {
		t.Errorf("choice Extras lost: %v", resp.Choices[0].Extras)
	}

	out, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(out), `"x_provider_extension":42`) {
		t.Errorf("top-level extras missing in marshal: %s", out)
	}
	if !strings.Contains(string(out), `"provider_field":"banana"`) {
		t.Errorf("choice extras missing in marshal: %s", out)
	}
}

func FuzzMessage_Unmarshal(f *testing.F) {
	seeds := []string{
		`{"role":"user","content":"x"}`,
		`{"role":"assistant","tool_calls":[{"id":"a","type":"function","function":{"name":"n","arguments":"{}"}}]}`,
		`{"role":"user","content":[{"type":"text","text":"x"}]}`,
		`{"role":"user","content":"x","unknown":42}`,
		`{}`,
		`{"role":null}`,
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(_ *testing.T, raw string) {
		var m Message
		_ = json.Unmarshal([]byte(raw), &m) // panic-free is the bar
	})
}

func FuzzRequest_Unmarshal(f *testing.F) {
	seeds := []string{
		`{"model":"x","messages":[]}`,
		`{"model":"x","messages":[{"role":"user","content":"hi"}],"unknown_root":1}`,
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(_ *testing.T, raw string) {
		var r ChatCompletionRequest
		_ = json.Unmarshal([]byte(raw), &r)
	})
}
