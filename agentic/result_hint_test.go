package agentic_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/agentic"
)

// TestToolResult_ResultHint_JSONRoundTrip locks the wire shape of the
// new ResultHint field on ToolResult. Per the JSON-roundtrip-test
// discipline rule (feedback_polymorphic_config_needs_json_roundtrip_test.md),
// any operator-reachable surface needs a JSON-load test.
//
// ResultHint is the typed counterpart to the legacy ApprovalRequiredPrefix
// magic-string pattern — without this test, a future refactor that
// changes the json tag (e.g. to "hint" or "result_hint_kind") would
// silently break every consumer that already decodes ToolResults off
// the wire.
func TestToolResult_ResultHint_JSONRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		hint agentic.ToolResultHint
	}{
		{"too_large", agentic.HintTooLarge},
		{"empty", agentic.HintEmpty},
		{"syntax_error", agentic.HintSyntaxError},
		{"empty hint (zero value)", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			original := agentic.ToolResult{
				CallID:     "call-hint-test",
				Content:    "irrelevant",
				ResultHint: tt.hint,
			}
			data, err := json.Marshal(original)
			if err != nil {
				t.Fatalf("Marshal failed: %v", err)
			}

			// Zero-value hint must omit from JSON (omitempty)
			if tt.hint == "" && strings.Contains(string(data), "result_hint") {
				t.Errorf("empty hint should be omitted from JSON, got: %s", data)
			}
			// Non-zero hint must be present
			if tt.hint != "" && !strings.Contains(string(data), `"result_hint":"`+string(tt.hint)+`"`) {
				t.Errorf("non-empty hint missing from JSON: %s", data)
			}

			var decoded agentic.ToolResult
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("Unmarshal failed: %v", err)
			}
			if decoded.ResultHint != tt.hint {
				t.Errorf("ResultHint = %q, want %q", decoded.ResultHint, tt.hint)
			}
		})
	}
}

// TestToolResult_ResultHint_ComposesWithError asserts ResultHint and
// Error are NOT mutually exclusive on the wire. A tool that fails
// partway through and returns partial data legitimately sets both
// (Error documents the failure, ResultHint advises recovery).
func TestToolResult_ResultHint_ComposesWithError(t *testing.T) {
	original := agentic.ToolResult{
		CallID:     "call-compose-test",
		Content:    "partial results before failure",
		Error:      "upstream returned 503 after 80 of 100 rows",
		ErrorKind:  agentic.ToolErrorExternal,
		ResultHint: agentic.HintTooLarge,
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded agentic.ToolResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.Error != original.Error {
		t.Errorf("Error lost in round-trip: got %q want %q", decoded.Error, original.Error)
	}
	if decoded.ResultHint != original.ResultHint {
		t.Errorf("ResultHint lost in round-trip: got %q want %q", decoded.ResultHint, original.ResultHint)
	}
	if decoded.ErrorKind != original.ErrorKind {
		t.Errorf("ErrorKind lost in round-trip: got %q want %q", decoded.ErrorKind, original.ErrorKind)
	}
}

// TestToolDefinition_Paginated_JSONRoundTrip locks the wire shape of
// the new Paginated bool flag on ToolDefinition.
func TestToolDefinition_Paginated_JSONRoundTrip(t *testing.T) {
	tests := []struct {
		name       string
		paginated  bool
		wantInJSON bool
	}{
		{"paginated true serializes", true, true},
		{"paginated false omits (omitempty zero value)", false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			original := agentic.ToolDefinition{
				Name:        "test_tool",
				Description: "doc",
				Parameters:  map[string]any{"type": "object"},
				Paginated:   tt.paginated,
			}
			data, err := json.Marshal(original)
			if err != nil {
				t.Fatalf("Marshal failed: %v", err)
			}
			hasField := strings.Contains(string(data), `"paginated"`)
			if hasField != tt.wantInJSON {
				t.Errorf("paginated in JSON = %v, want %v; data=%s", hasField, tt.wantInJSON, data)
			}

			var decoded agentic.ToolDefinition
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("Unmarshal failed: %v", err)
			}
			if decoded.Paginated != tt.paginated {
				t.Errorf("Paginated = %v, want %v", decoded.Paginated, tt.paginated)
			}
		})
	}
}

// TestMetadataKeys_AreStableStrings is a guard against accidental
// rename of the pagination metadata keys. Constants are wire-format —
// changing the string value breaks every existing read_loop_result
// consumer (semspec is integrated against these exact strings since
// before they were lifted into constants).
//
// Update with care: changes to these values are BREAKING wire-format
// changes and need e2e + downstream coordination.
func TestMetadataKeys_AreStableStrings(t *testing.T) {
	cases := map[string]string{
		"MetadataKeyHasMore":    agentic.MetadataKeyHasMore,
		"MetadataKeyNextOffset": agentic.MetadataKeyNextOffset,
		"MetadataKeyNextCursor": agentic.MetadataKeyNextCursor,
		"MetadataKeyTotalBytes": agentic.MetadataKeyTotalBytes,
	}
	wants := map[string]string{
		"MetadataKeyHasMore":    "has_more",
		"MetadataKeyNextOffset": "next_offset",
		"MetadataKeyNextCursor": "next_cursor",
		"MetadataKeyTotalBytes": "total_bytes",
	}
	for name, got := range cases {
		if got != wants[name] {
			t.Errorf("%s = %q, want %q (changing the constant value is a BREAKING wire change)", name, got, wants[name])
		}
	}
}
