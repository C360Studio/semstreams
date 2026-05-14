// Package expression — Dotted-path access into MessageFields payloads.
//
// Both `$message.*` substitution (in processor/rule/message_substitution.go)
// and `$message.*` condition evaluation (Evaluator.EvaluateWithStateAndMessage)
// walk the same map shape via the same dotted-path rules. This helper is
// the single source of truth — shared via the expression package so the
// rule package can import it without introducing a cycle.
package expression

import "strings"

// ExtractMessageValue walks data along a dotted path. Returns the
// terminal value and true on success; the second return is false (and
// the caller must treat the result as "field not present") when:
//
//   - any non-terminal segment is missing from the current map, OR
//   - any non-terminal segment resolves to a non-map value (can't
//     descend further), OR
//   - the terminal segment is missing.
//
// A nil terminal value is considered a successful resolution (the JSON
// `null` distinct from "missing"). Callers can render `<nil>` via fmt;
// the surfacing safety net is the regex-leftover warning at the
// substitution layer, or the `Required` flag at the condition layer.
//
// The path is the bare dotted form (no `$message.` prefix). Callers
// trim the prefix before calling.
func ExtractMessageValue(data MessageFields, path string) (any, bool) {
	if len(data) == 0 || path == "" {
		return nil, false
	}
	parts := strings.Split(path, ".")
	var current any = map[string]any(data)
	for i, part := range parts {
		m, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		value, exists := m[part]
		if !exists {
			return nil, false
		}
		if i == len(parts)-1 {
			return value, true
		}
		current = value
	}
	return nil, false
}
