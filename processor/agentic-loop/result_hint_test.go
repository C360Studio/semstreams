package agenticloop

import (
	"strings"
	"testing"

	"github.com/c360studio/semstreams/agentic"
)

// TestDecorateContentWithHint verifies that the framework prepends a
// canonical hint preamble when ResultHint is set, and is a no-op
// otherwise. The preamble must contain the bracketed kind tag so
// downstream log scraping can match it, plus the canonical advice
// text from the hintMessages registry.
func TestDecorateContentWithHint(t *testing.T) {
	tests := []struct {
		name             string
		hint             agentic.ToolResultHint
		content          string
		wantContainsKind string
		wantOriginal     bool
		wantUnchanged    bool
	}{
		{
			name:          "no hint is a passthrough",
			hint:          "",
			content:       "raw tool output",
			wantUnchanged: true,
		},
		{
			name:             "too_large prepends canonical advice",
			hint:             agentic.HintTooLarge,
			content:          "[1, 2, 3]",
			wantContainsKind: "[hint: too_large]",
			wantOriginal:     true,
		},
		{
			name:             "empty prepends canonical advice",
			hint:             agentic.HintEmpty,
			content:          "[]",
			wantContainsKind: "[hint: empty]",
			wantOriginal:     true,
		},
		{
			name:             "syntax_error prepends canonical advice",
			hint:             agentic.HintSyntaxError,
			content:          "parse failed at pos 12",
			wantContainsKind: "[hint: syntax_error]",
			wantOriginal:     true,
		},
		{
			name:             "unknown hint kind falls through to generic preamble",
			hint:             agentic.ToolResultHint("not_a_real_hint"),
			content:          "content",
			wantContainsKind: "[hint: not_a_real_hint]",
			wantOriginal:     true,
		},
		{
			name:             "hint on empty content still emits preamble",
			hint:             agentic.HintEmpty,
			content:          "",
			wantContainsKind: "[hint: empty]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := decorateContentWithHint(tt.content, tt.hint)
			if tt.wantUnchanged {
				if got != tt.content {
					t.Errorf("expected passthrough, got %q", got)
				}
				return
			}
			if !strings.Contains(got, tt.wantContainsKind) {
				t.Errorf("expected kind tag %q in output, got %q", tt.wantContainsKind, got)
			}
			if tt.wantOriginal && !strings.Contains(got, tt.content) {
				t.Errorf("original content lost: %q not found in %q", tt.content, got)
			}
		})
	}
}

// TestDecorateContentWithPagination verifies the continuation hint is
// appended when has_more=true, with cursor preferred over offset and
// graceful fallback when neither is present.
func TestDecorateContentWithPagination(t *testing.T) {
	tests := []struct {
		name          string
		metadata      map[string]any
		content       string
		wantContains  string
		wantUnchanged bool
	}{
		{
			name:          "nil metadata is passthrough",
			metadata:      nil,
			content:       "page content",
			wantUnchanged: true,
		},
		{
			name:          "empty metadata is passthrough",
			metadata:      map[string]any{},
			content:       "page content",
			wantUnchanged: true,
		},
		{
			name: "has_more=false is passthrough",
			metadata: map[string]any{
				agentic.MetadataKeyHasMore:    false,
				agentic.MetadataKeyNextOffset: 100,
			},
			content:       "last page",
			wantUnchanged: true,
		},
		{
			name: "has_more with cursor renders cursor token",
			metadata: map[string]any{
				agentic.MetadataKeyHasMore:    true,
				agentic.MetadataKeyNextCursor: "abc123",
			},
			content:      "page 1",
			wantContains: `pass cursor="abc123"`,
		},
		{
			name: "has_more with offset (int) renders offset",
			metadata: map[string]any{
				agentic.MetadataKeyHasMore:    true,
				agentic.MetadataKeyNextOffset: 4096,
			},
			content:      "page 1",
			wantContains: "pass offset=4096",
		},
		{
			name: "has_more with offset (float64 from JSON decode) renders offset",
			metadata: map[string]any{
				agentic.MetadataKeyHasMore:    true,
				agentic.MetadataKeyNextOffset: float64(4096),
			},
			content:      "page 1",
			wantContains: "pass offset=4096",
		},
		{
			name: "cursor preferred over offset when both set",
			metadata: map[string]any{
				agentic.MetadataKeyHasMore:    true,
				agentic.MetadataKeyNextCursor: "tok",
				agentic.MetadataKeyNextOffset: 100,
			},
			content:      "page 1",
			wantContains: `pass cursor="tok"`,
		},
		{
			name: "has_more without continuation token falls through to generic message",
			metadata: map[string]any{
				agentic.MetadataKeyHasMore: true,
			},
			content:      "page 1",
			wantContains: "pass the continuation token from this call's metadata",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := decorateContentWithPagination(tt.content, tt.metadata)
			if tt.wantUnchanged {
				if got != tt.content {
					t.Errorf("expected passthrough, got %q", got)
				}
				return
			}
			if !strings.Contains(got, tt.wantContains) {
				t.Errorf("expected %q in output, got %q", tt.wantContains, got)
			}
			if !strings.Contains(got, tt.content) {
				t.Errorf("original content lost: %q not found in %q", tt.content, got)
			}
			if !strings.Contains(got, "[pagination:") {
				t.Errorf("missing [pagination: ...] tag in output: %q", got)
			}
		})
	}
}

// TestDecoration_ComposesHintAndPagination verifies the documented
// integration shape: hint preamble at top, original content in the
// middle, pagination continuation at the bottom. semspec's scenario
// where TooLarge AND has_more both fire on the same result.
func TestDecoration_ComposesHintAndPagination(t *testing.T) {
	content := "some result body"
	withHint := decorateContentWithHint(content, agentic.HintTooLarge)
	final := decorateContentWithPagination(withHint, map[string]any{
		agentic.MetadataKeyHasMore:    true,
		agentic.MetadataKeyNextCursor: "next",
	})

	// Hint preamble must be at the start
	if !strings.HasPrefix(final, "[hint: too_large]") {
		t.Errorf("hint preamble not at start: %q", final)
	}
	// Pagination must be at the end
	if !strings.HasSuffix(final, `[pagination: more results available; pass cursor="next" to continue]`) {
		t.Errorf("pagination not at end: %q", final)
	}
	// Original content sandwiched between
	if !strings.Contains(final, content) {
		t.Errorf("original content lost: %q", final)
	}
}
