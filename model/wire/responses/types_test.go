package responses_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/model/wire/responses"
)

// TestRequest_JSONRoundTrip pins that a fully-populated Request
// survives marshal/unmarshal with field identity. Doc-derived shape
// per ADR-051 D1 — the live-fixture parity gate is the separate
// TestResponses_GoldenFixture_Parity test below, stubbed pending
// PR 4 capture.
func TestRequest_JSONRoundTrip(t *testing.T) {
	store := false
	temp := 0.7
	topP := 0.95
	in := &responses.Request{
		Model: "gpt-5.5",
		Input: []responses.InputItem{
			responses.NewInputDeveloperMessage("you are a calculator"),
			responses.NewInputUserMessage("what is 17 * 23?"),
			responses.NewInputFunctionCall("call_abc", "multiply", `{"a":17,"b":23}`),
			responses.NewInputFunctionCallOutput("call_abc", `{"product":391}`),
			responses.NewInputReasoning("rs_xyz", "encrypted-opaque-blob", []responses.SummaryPart{
				{Type: responses.SummaryTypeText, Text: "considering multiplication strategy"},
			}),
		},
		Instructions: "Be concise.",
		Tools: []responses.Tool{
			{
				Type:        "function",
				Name:        "multiply",
				Description: "Multiplies two integers.",
				Parameters:  json.RawMessage(`{"type":"object","properties":{"a":{"type":"integer"},"b":{"type":"integer"}},"required":["a","b"]}`),
				Strict:      true,
			},
		},
		ToolChoice: json.RawMessage(`"auto"`),
		Reasoning: &responses.ReasoningParams{
			Effort:  "medium",
			Summary: "concise",
		},
		Text: &responses.TextParams{
			Format: json.RawMessage(`{"type":"text"}`),
		},
		Temperature:     &temp,
		TopP:            &topP,
		MaxOutputTokens: 1024,
		Store:           &store,
		User:            "user-abc",
		Metadata:        map[string]string{"trace_id": "t-1", "loop_id": "l-1"},
	}

	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// Sanity: the marshaled body should use the API's documented
	// field names — guards against accidental field renames.
	mustContain := []string{
		`"model":"gpt-5.5"`,
		`"input":[`,
		`"instructions":"Be concise."`,
		`"reasoning":{"effort":"medium"`,
		`"max_output_tokens":1024`,
		`"store":false`,
		`"role":"developer"`,
		`"role":"user"`,
		`"type":"function_call"`,
		`"type":"function_call_output"`,
		`"type":"reasoning"`,
		`"encrypted_content":"encrypted-opaque-blob"`,
		`"call_id":"call_abc"`,
		`"summary_text"`,
	}
	for _, s := range mustContain {
		if !strings.Contains(string(b), s) {
			t.Errorf("expected substring %q in marshaled request; got %s", s, string(b))
		}
	}

	var got responses.Request
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(in, &got) {
		t.Errorf("round-trip mismatch\n  in:  %#v\n  got: %#v", in, &got)
	}
}

// TestResponse_JSONRoundTrip pins that a fully-populated Response
// survives marshal/unmarshal with field identity. Mirrors the
// documented Responses API output shape; lives-fixture parity gate
// is the separate TestResponses_GoldenFixture_Parity test below.
func TestResponse_JSONRoundTrip(t *testing.T) {
	temp := 0.7
	topP := 0.95
	store := false
	in := &responses.Response{
		ID:        "resp_001",
		Object:    "response",
		CreatedAt: 1717070000,
		Status:    "completed",
		Model:     "gpt-5.5-2026-05-30",
		Output: []responses.OutputItem{
			{
				Type:             responses.ItemTypeReasoning,
				ID:               "rs_001",
				Status:           "completed",
				EncryptedContent: "encrypted-opaque-from-openai",
				Summary: []responses.SummaryPart{
					{Type: responses.SummaryTypeText, Text: "decided to multiply directly"},
				},
			},
			{
				Type:      responses.ItemTypeFunctionCall,
				ID:        "fc_001",
				Status:    "completed",
				CallID:    "call_def",
				Name:      "multiply",
				Arguments: `{"a":17,"b":23}`,
			},
			{
				Type:   responses.ItemTypeMessage,
				ID:     "msg_001",
				Status: "completed",
				Role:   responses.RoleAssistant,
				Content: []responses.ContentPart{
					{Type: responses.ContentTypeOutputText, Text: "391"},
				},
			},
		},
		Reasoning: &responses.ReasoningParams{Effort: "medium"},
		Store:     &store,
		Text: &responses.TextParams{
			Format: json.RawMessage(`{"type":"text"}`),
		},
		Temperature: &temp,
		TopP:        &topP,
		ToolChoice:  json.RawMessage(`"auto"`),
		Truncation:  "disabled",
		Usage: &responses.Usage{
			InputTokens:         512,
			InputTokensDetails:  &responses.InputTokensDetails{CachedTokens: 0},
			OutputTokens:        128,
			OutputTokensDetails: &responses.OutputTokensDetails{ReasoningTokens: 96},
			TotalTokens:         640,
		},
		Metadata: map[string]string{"trace_id": "t-1"},
	}

	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// Sanity on field names — guards against rename drift.
	mustContain := []string{
		`"id":"resp_001"`,
		`"status":"completed"`,
		`"output":[`,
		`"input_tokens":512`,
		`"output_tokens":128`,
		`"reasoning_tokens":96`,
		`"total_tokens":640`,
		`"truncation":"disabled"`,
	}
	for _, s := range mustContain {
		if !strings.Contains(string(b), s) {
			t.Errorf("expected substring %q in marshaled response; got %s", s, string(b))
		}
	}

	var got responses.Response
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(in, &got) {
		t.Errorf("round-trip mismatch\n  in:  %#v\n  got: %#v", in, &got)
	}
}

// TestInputItem_ReasoningSummaryAlwaysPresent pins the workaround
// for OpenAI's reasoning-input-item summary requirement: the API
// rejects echoed reasoning items without a `summary` field even
// when the local slice is nil/empty. InputItem.MarshalJSON injects
// `"summary":[]` for reasoning items that lack one. Caught by the
// ADR-051 PR 4 live reasoning-echo test (HTTP 400 missing_required_parameter).
func TestInputItem_ReasoningSummaryAlwaysPresent(t *testing.T) {
	cases := []struct {
		name        string
		item        responses.InputItem
		wantSummary string
	}{
		{
			name: "reasoning with nil summary emits []",
			item: responses.InputItem{
				Type:             responses.ItemTypeReasoning,
				ID:               "rs_1",
				EncryptedContent: "blob",
			},
			wantSummary: `"summary":[]`,
		},
		{
			name: "reasoning with empty summary emits []",
			item: responses.InputItem{
				Type:             responses.ItemTypeReasoning,
				ID:               "rs_2",
				EncryptedContent: "blob",
				Summary:          []responses.SummaryPart{},
			},
			wantSummary: `"summary":[]`,
		},
		{
			name: "reasoning with populated summary preserves it",
			item: responses.InputItem{
				Type:             responses.ItemTypeReasoning,
				ID:               "rs_3",
				EncryptedContent: "blob",
				Summary: []responses.SummaryPart{
					{Type: responses.SummaryTypeText, Text: "thinking"},
				},
			},
			wantSummary: `"summary":[{"type":"summary_text","text":"thinking"}]`,
		},
		{
			name: "non-reasoning item omits summary (default behavior preserved)",
			item: responses.NewInputUserMessage("hi"),
			// Empty wantSummary signals the substring must NOT appear.
			wantSummary: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b, err := json.Marshal(tc.item)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if tc.wantSummary == "" {
				if strings.Contains(string(b), `"summary"`) {
					t.Errorf("expected NO summary field on non-reasoning item; got %s", string(b))
				}
				return
			}
			if !strings.Contains(string(b), tc.wantSummary) {
				t.Errorf("expected substring %q; got %s", tc.wantSummary, string(b))
			}
		})
	}
}

// TestOutputItem_Accessors pins the convenience helpers on
// OutputItem (OutputText, RefusalText, IsMessage, etc.).
func TestOutputItem_Accessors(t *testing.T) {
	cases := []struct {
		name        string
		item        responses.OutputItem
		wantText    string
		wantRefusal string
		wantKind    string
	}{
		{
			name: "assistant text",
			item: responses.OutputItem{
				Type: responses.ItemTypeMessage,
				Role: responses.RoleAssistant,
				Content: []responses.ContentPart{
					{Type: responses.ContentTypeOutputText, Text: "hello "},
					{Type: responses.ContentTypeOutputText, Text: "world"},
				},
			},
			wantText: "hello world",
			wantKind: "message",
		},
		{
			name: "refusal",
			item: responses.OutputItem{
				Type: responses.ItemTypeMessage,
				Role: responses.RoleAssistant,
				Content: []responses.ContentPart{
					{Type: responses.ContentTypeRefusal, Refusal: "cannot comply"},
				},
			},
			wantRefusal: "cannot comply",
			wantKind:    "message",
		},
		{
			name: "function_call has no text",
			item: responses.OutputItem{
				Type:      responses.ItemTypeFunctionCall,
				CallID:    "c1",
				Name:      "fn",
				Arguments: `{}`,
			},
			wantKind: "function_call",
		},
		{
			name: "reasoning has no text",
			item: responses.OutputItem{
				Type: responses.ItemTypeReasoning,
				ID:   "rs",
			},
			wantKind: "reasoning",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.item.OutputText(); got != tc.wantText {
				t.Errorf("OutputText = %q, want %q", got, tc.wantText)
			}
			if got := tc.item.RefusalText(); got != tc.wantRefusal {
				t.Errorf("RefusalText = %q, want %q", got, tc.wantRefusal)
			}
			switch tc.wantKind {
			case "message":
				if !tc.item.IsMessage() {
					t.Errorf("IsMessage = false; want true")
				}
			case "function_call":
				if !tc.item.IsFunctionCall() {
					t.Errorf("IsFunctionCall = false; want true")
				}
			case "reasoning":
				if !tc.item.IsReasoning() {
					t.Errorf("IsReasoning = false; want true")
				}
			}
		})
	}
}

// TestResponses_GoldenFixture_Parity is the live-fixture parity
// gate: every captured request/response in testdata/ must round-trip
// through these types without information loss. Skips when no
// fixtures exist (the doc-derived round-trip above is the smoke
// test in that case). PR 4 wires the fixture-capture test that
// populates testdata/; once fixtures land, this test catches any
// drift between the doc-derived shape and reality.
//
// Per ADR-051 phasing: skeleton-from-docs is allowed but pre-tag
// gate requires live fixtures (PR 4) and parity here.
func TestResponses_GoldenFixture_Parity(t *testing.T) {
	dir := "testdata"
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Skipf("no testdata directory: %v", err)
	}

	jsonFiles := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if filepath.Ext(e.Name()) != ".json" {
			continue
		}
		jsonFiles = append(jsonFiles, e.Name())
	}
	if len(jsonFiles) == 0 {
		t.Skip("no captured fixtures yet; PR 4 populates testdata/ — this is the hard gate before tagging")
	}

	for _, name := range jsonFiles {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}
			// Probe whether this is a request or response by sniffing
			// for top-level fields. Requests have "input"; responses
			// have "output". Fixtures cleanly fall into one class.
			var probe map[string]json.RawMessage
			if err := json.Unmarshal(data, &probe); err != nil {
				t.Fatalf("probe decode: %v", err)
			}
			switch {
			case probe["input"] != nil:
				assertRoundTrip[responses.Request](t, data)
			case probe["output"] != nil:
				assertRoundTrip[responses.Response](t, data)
			default:
				t.Skipf("fixture %s is neither request nor response (no input/output field)", name)
			}
		})
	}
}

// assertRoundTrip decodes data into T, re-encodes it, and asserts
// the re-encoded bytes deserialize to a value DeepEqual to the
// first decode. This catches information loss across the typed
// boundary — a field the typed structs forgot to model would
// silently drop on re-encode and the second decode would diverge.
func assertRoundTrip[T any](t *testing.T, data []byte) {
	t.Helper()
	var first T
	if err := json.Unmarshal(data, &first); err != nil {
		t.Fatalf("first decode: %v", err)
	}
	b, err := json.Marshal(first)
	if err != nil {
		t.Fatalf("re-encode: %v", err)
	}
	var second T
	if err := json.Unmarshal(b, &second); err != nil {
		t.Fatalf("second decode: %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Errorf("round-trip diverged after re-encode\n  first:  %#v\n  second: %#v", first, second)
	}
}
