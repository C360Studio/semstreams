package agenticloop

import (
	"fmt"
	"strings"

	"github.com/c360studio/semstreams/agentic"
)

// hintMessages maps each ToolResultHint to the canonical English advice
// the framework injects into the model's next message. Kept in the
// framework (not the executor) so the phrasing is consistent across
// every tool that signals the same hint — executors pick the right
// enum value, the framework renders the text.
//
// The text is deliberately imperative and short so small/mid-tier
// models pick it up at the top of the message. Empirical observation
// (semspec 2026-05-09): qwen3.6-27b retries the same broad query 3+
// times when the advice is buried in a free-form error string;
// hoisting it to a structured preamble is the proposed mitigation.
//
// Update with care: changing phrasing changes how every existing flow
// reacts. Add new hints as new map entries; don't rewrite existing
// text without a downstream sweep.
var hintMessages = map[agentic.ToolResultHint]string{
	agentic.HintTooLarge:    "The result was truncated because the response exceeded the executor's size cap. Narrow your query — add a more specific filter, entity_id, or a smaller limit — before retrying.",
	agentic.HintEmpty:       "The query succeeded but returned no results. Try a broader filter, drop one of the predicates, or use a different tool to find candidate entities.",
	agentic.HintSyntaxError: "The tool's query parser rejected your request. Call the tool's introspect/help facility before retrying with the same shape.",
}

// decorateContentWithHint prepends a canonical hint preamble to the
// model-facing content when ResultHint is set. Returns content
// unchanged when no hint applies.
//
// Format: `[hint: <kind>] <canonical advice>\n\n<original content>`.
// The bracketed kind tag is machine-parseable for downstream log
// scraping or trajectory analytics; the canonical advice is what the
// model actually reads.
func decorateContentWithHint(content string, hint agentic.ToolResultHint) string {
	if hint == "" {
		return content
	}
	advice, ok := hintMessages[hint]
	if !ok {
		// Unknown hint kind — fall through to a generic preamble so the
		// model still sees a structured cue, but log nothing here
		// because this is the rendering path (any per-hint metrics or
		// warns live at the consumer site in HandleToolResult).
		advice = "The tool flagged this result with a non-canonical hint; refine your approach before retrying."
	}
	preamble := fmt.Sprintf("[hint: %s] %s", string(hint), advice)
	if content == "" {
		return preamble
	}
	return preamble + "\n\n" + content
}

// decorateContentWithPagination appends a canonical continuation hint
// when the result metadata declares more pages remain. Composes with
// decorateContentWithHint when both fire on the same ToolResult: the
// hint preamble lands at the top of the message and the pagination
// continuation at the bottom.
//
// Reads MetadataKeyHasMore (bool); when true, includes either
// MetadataKeyNextCursor (opaque token, preferred when present) or
// MetadataKeyNextOffset (byte/index offset). Tools choose ONE of the
// continuation keys — the framework prefers cursor when both are set
// (a contract violation, but recover gracefully).
func decorateContentWithPagination(content string, metadata map[string]any) string {
	if metadata == nil {
		return content
	}
	hasMore, ok := metadata[agentic.MetadataKeyHasMore].(bool)
	if !ok || !hasMore {
		return content
	}

	var continuation strings.Builder
	continuation.WriteString("\n\n[pagination: more results available")
	if cursor, ok := metadata[agentic.MetadataKeyNextCursor].(string); ok && cursor != "" {
		fmt.Fprintf(&continuation, "; pass cursor=%q to continue", cursor)
	} else if offset, ok := metadataInt(metadata[agentic.MetadataKeyNextOffset]); ok {
		fmt.Fprintf(&continuation, "; pass offset=%d to continue", offset)
	} else {
		continuation.WriteString("; pass the continuation token from this call's metadata to continue")
	}
	continuation.WriteString("]")
	return content + continuation.String()
}

// metadataInt extracts an integer from a metadata value, handling the
// json.Unmarshal-into-map[string]any case where numeric values land as
// float64. Returns (value, true) on success, (0, false) on a missing
// or unparseable value.
func metadataInt(v any) (int64, bool) {
	switch n := v.(type) {
	case int:
		return int64(n), true
	case int64:
		return n, true
	case float64:
		return int64(n), true
	case nil:
		return 0, false
	default:
		return 0, false
	}
}
