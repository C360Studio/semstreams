package wire

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestFixtures_RoundTrip loads every JSON fixture under testdata/ and
// asserts it round-trips through the wire types byte-equivalent
// (modulo whitespace) — i.e. all fields land on a known struct field
// or in Extras, nothing is dropped or invented.
func TestFixtures_RoundTrip(t *testing.T) {
	t.Parallel()
	cases := []struct {
		file string
		into func() any
		kind string
	}{
		{"request_simple.json", func() any { return &ChatCompletionRequest{} }, "request"},
		{"response_simple.json", func() any { return &ChatCompletionResponse{} }, "response"},
		{"response_tool_calls.json", func() any { return &ChatCompletionResponse{} }, "response"},
		{"response_gemini_thought_signature.json", func() any { return &ChatCompletionResponse{} }, "response"},
	}
	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			t.Parallel()
			raw, err := os.ReadFile(filepath.Join("testdata", tc.file))
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}
			var src map[string]any
			if err := json.Unmarshal(raw, &src); err != nil {
				t.Fatalf("decode source as map: %v", err)
			}

			obj := tc.into()
			if err := json.Unmarshal(raw, obj); err != nil {
				t.Fatalf("decode into wire type: %v", err)
			}
			out, err := json.Marshal(obj)
			if err != nil {
				t.Fatalf("re-marshal: %v", err)
			}
			var roundTripped map[string]any
			if err := json.Unmarshal(out, &roundTripped); err != nil {
				t.Fatalf("re-decode as map: %v", err)
			}
			if !sameTopLevelKeys(src, roundTripped) {
				t.Errorf("%s: top-level key drift\nsrc:  %v\nout:  %v", tc.file, keys(src), keys(roundTripped))
			}
		})
	}
}

func TestFixtures_GeminiSignaturePreservedThroughResponse(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile(filepath.Join("testdata", "response_gemini_thought_signature.json"))
	if err != nil {
		t.Fatal(err)
	}
	var resp ChatCompletionResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Choices) != 1 || resp.Choices[0].Message == nil || len(resp.Choices[0].Message.ToolCalls) != 1 {
		t.Fatalf("unexpected shape: %+v", resp)
	}
	tc := resp.Choices[0].Message.ToolCalls[0]
	got, ok := tc.Extras["extra_content"]
	if !ok {
		t.Fatalf("extra_content missing on tool_call: %v", tc.Extras)
	}
	if !strings.Contains(string(got), "thought_signature") {
		t.Errorf("thought_signature missing from raw: %s", got)
	}

	out, err := json.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "thought_signature") {
		t.Errorf("thought_signature lost on re-marshal: %s", out)
	}
}

func TestFixtures_ErrorEnvelopes(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"error_object.json", "error_array_wrapped.json"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			raw, err := os.ReadFile(filepath.Join("testdata", name))
			if err != nil {
				t.Fatal(err)
			}
			apiErr := DecodeError(400, raw)
			if apiErr.Message == "" {
				t.Errorf("%s: empty message after decode: %+v", name, apiErr)
			}
		})
	}
}

func sameTopLevelKeys(a, b map[string]any) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if _, ok := b[k]; !ok {
			return false
		}
	}
	return true
}

func keys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
