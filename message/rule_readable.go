// Package message - the RuleReadable behavioral interface.
//
// A sibling of the optional behaviors in behaviors.go (Locatable, Timeable,
// Observable, ...), discovered the same way and optional in the same sense.
// It lives in its own file only because behaviors.go is already at revive's
// per-file max-public-structs cap of 10.
package message

// RuleReadable exposes the fields a payload is willing to have rule
// conditions and `$message.*` substitutions read.
//
// Optional and discovered at runtime by type assertion, exactly like its
// siblings in behaviors.go. A payload that does not implement it is not
// rule-readable: the rule engine reads no fields from it, the rule does not
// fire, and the engine reports the rule/payload-type pairing rather than
// evaluating false in silence.
//
// The PAYLOAD DECLARES its projection; the engine never derives one. There is
// deliberately no reflective default — reflection would make every payload
// rule-readable by accident and expose fields the author never intended a rule
// to match on, and it is the wrong trade in the evaluation hot path.
//
// What to expose, and what not to:
//
//   - EXPOSE structural facts — identifiers, roles, outcomes, states,
//     classifications, counts, routing addresses, timestamps.
//   - WITHHOLD LLM-authored and user content — prompts, message text, model
//     output, tool-result bodies (ADR-036), and open caller-populated maps
//     whose contents the payload's author does not control.
//
// The reason is not privacy alone. A rule cannot make a quality judgement over
// unstructured text, and content in a rule payload is how a small model's
// context window silently truncates. A content-shaped decision belongs to a
// coordinator that emits a structured result which a later rule matches on
// deterministically.
//
// Keys are dotted-path addressable: a nested map[string]any value is reachable
// as `$message.parent.child`.
//
// Three sharp edges to know before you write a rule against a projection:
//
//   - ABSENCE IS THE FALSE CASE FOR BOOLEANS. A projection that mirrors
//     `omitempty` omits a false bool entirely, so `stop_loop` is absent rather
//     than present-and-false. Test for absence (or condition on the true case);
//     a condition comparing it to `false` matches nothing, silently. The same
//     applies to any zero-valued `omitempty` field.
//   - FIELD NAMES ARE NOT GLOBALLY UNIQUE. `$message.type` means the context
//     event kind on ContextEvent, the control-signal verb on UserSignal, and
//     the response kind on UserResponse. A rule subscribed to more than one
//     subject must pin the payload with a second condition, because the engine
//     resolves the path, not the type.
//   - PROJECTED VALUES FEED SUBJECT TEMPLATES. `$message.*` resolves in action
//     subjects and reasons as well as in conditions, so anything exposed here
//     can end up as a NATS subject token. Dotted values (`model`) and RFC3339
//     nano timestamps are now reliably available where they used to arrive only
//     by accident — which also means an exposed field with a `.` or a space in
//     it becomes a malformed subject. Prefer exposing tokens a subject can
//     carry.
type RuleReadable interface {
	// RuleFields returns this payload's declared rule-readable fields.
	// A nil or empty map is a valid answer meaning "nothing to match on" —
	// distinct from not implementing the interface at all, which means the
	// payload is unreadable and the engine reports it.
	RuleFields() map[string]any
}
