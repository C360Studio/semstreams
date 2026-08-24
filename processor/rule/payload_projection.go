// Package rule - payload projection for rule evaluation and substitution.
package rule

import (
	"fmt"

	"github.com/c360studio/semstreams/message"
)

// ruleFields projects an inbound message's payload into the field map that
// `$message.*` conditions and `$message.*` substitutions read.
//
// This is the ONE home for interpreting a payload on the rule lane. Every
// caller that needs message data goes through it — ExpressionRule.Evaluate,
// extractMessageData, TestRule.Evaluate — so the rule engine cannot drift
// into two answers about what a payload exposes. Before this helper there
// were four hand-rolled `payload.(*message.GenericJSONPayload)` assertions,
// and every typed payload in the registry was unreadable at all four.
//
// Deliberately NOT a caller: extractEntityID (message_handler.go). It reads
// the generic payload's legacy `entity_id` directly because it answers a
// DIFFERENT question — durable state identity, not field visibility — and
// deriving the RULE_STATE key from this projection would let a typed payload
// exposing `entity_id` for matching silently collapse distinct messages onto
// one state record. See the seam comment on extractEntityID.
//
// A payload is readable when it implements message.RuleReadable. That is the
// whole rule: message.GenericJSONPayload is not special-cased here because it
// implements the interface too (generic_json.go), returning its Data map, so
// every rule that worked before this helper works after it.
//
// The bool is the difference between two answers that used to be the same
// silent `false`:
//
//   - (nil|empty, true)  — readable, nothing to match on. A condition over it
//     is legitimately false; no signal is warranted.
//   - (nil, false)       — the engine cannot read this payload AT ALL. Every
//     `$message.*` condition against it is false forever. Callers surface this
//     rather than treating it as a verdict.
func ruleFields(msg message.Message) (map[string]any, bool) {
	if msg == nil {
		return nil, false
	}
	payload := msg.Payload()
	if payload == nil {
		return nil, false
	}
	if readable, ok := payload.(message.RuleReadable); ok {
		return readable.RuleFields(), true
	}
	return nil, false
}

// payloadTypeLabel names the payload in the unreadable report and keys the
// once-per-pairing suppression.
//
// The registered wire type is the identity a rule author recognises and it is
// bounded by the payload registry, which is what keeps the suppression map
// from growing with traffic. The Go type is the fallback for a message that
// carries no valid type — a hand-built message or a partially-decoded one —
// because "unreadable payload of unknown type" is not an actionable report.
func payloadTypeLabel(msg message.Message) string {
	if msgType := msg.Type(); msgType.IsValid() {
		return msgType.String()
	}
	if payload := msg.Payload(); payload != nil {
		return fmt.Sprintf("%T", payload)
	}
	return "none"
}
