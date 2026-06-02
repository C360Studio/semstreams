package llmwrap

import (
	"fmt"
	"strings"
)

// ErrPayloadTruncateBytes caps the raw-payload snippet rendered in
// error messages. 512 keeps the structured-emit JSON's action enum,
// per-action arg keys, and short rationales visible even when the
// model wrapped its emit in long prose.
const ErrPayloadTruncateBytes = 512

// ExtractJSON pulls a balanced JSON object out of an LLM response.
// Many models wrap structured emits in ```json fences or prose
// preface; this helper strips fences, then walks the first balanced
// {...} substring while tracking JSON string-literal context (so a
// `}` inside a string value, e.g. a rationale field like "axes
// spanning {time, entity_type}", does not truncate the extraction
// mid-object). Backslash-escaped quotes inside strings do not toggle
// the string-context flag.
//
// Returns an error if the trimmed content contains no `{`, or if the
// balanced-brace walker reaches end-of-input with unmatched depth.
func ExtractJSON(content string) ([]byte, error) {
	trimmed := strings.TrimSpace(content)
	trimmed = strings.TrimPrefix(trimmed, "```json")
	trimmed = strings.TrimPrefix(trimmed, "```JSON")
	trimmed = strings.TrimPrefix(trimmed, "```")
	trimmed = strings.TrimSuffix(trimmed, "```")
	trimmed = strings.TrimSpace(trimmed)

	start := strings.Index(trimmed, "{")
	if start < 0 {
		return nil, fmt.Errorf("no JSON object found in content")
	}
	depth := 0
	inString := false
	escaped := false
	for i := start; i < len(trimmed); i++ {
		ch := trimmed[i]
		if inString {
			switch {
			case escaped:
				escaped = false
			case ch == '\\':
				escaped = true
			case ch == '"':
				inString = false
			}
			continue
		}
		switch ch {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return []byte(trimmed[start : i+1]), nil
			}
		}
	}
	return nil, fmt.Errorf("unbalanced braces in content (depth=%d)", depth)
}

// Truncate returns s capped at n bytes with an ellipsis suffix on
// over-long input. Used in error-message construction so an over-long
// LLM response doesn't blow up log lines or trajectory captures.
func Truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
