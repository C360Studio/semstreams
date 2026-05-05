// Package rule - Stable per-action identity for firing-cap bookkeeping.
package rule

import (
	"crypto/sha256"
	"encoding/hex"
)

// DefaultActionMaxIterations is the framework default for the per-action
// firing cap when an Action.MaxIterations field is unset (nil pointer).
// Semteams's framing (2026-05-05): structured-output retries are the
// rule, not the exception — clean runs are unicorns. A cap-of-3 gives
// one corrective shot plus a margin, surfaces persistent drift loudly,
// and matches agentic-loop.config.MaxIterations's safe-by-default
// philosophy. Operators who want unbounded firing set
// `"max_iterations": 0` on the action explicitly.
const DefaultActionMaxIterations = 3

// fingerprintLen is the truncated hash length used as the
// auto-generated Action ID. 16 hex chars (8 bytes of SHA-256) gives
// 2^64 distinct values — collisions are vanishingly improbable across
// any rule-set humans will write, and the short string keeps logs and
// audit triples readable.
const fingerprintLen = 16

// effectiveID returns the stable identifier the per-action firing-cap
// bookkeeping keys MatchState.ActionIterations on. Author-supplied
// Action.ID always wins; absent that, a deterministic hash of the
// shape-defining fields takes over.
//
// The fingerprint changes only when the author meaningfully changes
// the action shape — which is the right invalidation surface. Renaming
// a rule, swapping the role, retargeting a publish subject, or
// switching action types resets the per-action counter; minor edits
// to a publish_agent's prompt or a triple's TTL do not. Authors who
// need stable counters across a rule rename set Action.ID explicitly.
func (a Action) effectiveID(ruleID string) string {
	if a.ID != "" {
		return a.ID
	}
	return a.fingerprint(ruleID)
}

// fingerprint returns the deterministic hash semteams recommended on
// 2026-05-05: SHA-256 over (rule_id, action_type, distinguisher,
// role), truncated to 16 hex chars. The distinguisher is the
// shape-defining field set per action type — see actionDistinguisher.
//
// When two actions in the same rule produce the same fingerprint
// (e.g., two `publish_agent` actions with the same role and subject
// differing only in prompt), they share a counter. Authors who want
// distinct counters set Action.ID explicitly on each.
//
// Stability under invalidation: the fingerprint changes only when
// the author meaningfully changes the action shape. Renaming a
// rule, swapping the role, retargeting a publish subject, or
// switching action types resets the per-action counter; minor edits
// to a publish_agent's prompt or a triple's TTL do not.
//
// CAVEAT for authors: rolling renames (subject `agent.task.researcher`
// → `agent.task.research-001` for a deployment rotation) reset the
// counter. Authors who want stable cap behaviour across renames
// MUST set Action.ID explicitly. The migration guide flags this.
func (a Action) fingerprint(ruleID string) string {
	distinguisher := actionDistinguisher(a)
	h := sha256.Sum256([]byte(ruleID + "|" + a.Type + "|" + distinguisher + "|" + a.Role))
	return hex.EncodeToString(h[:])[:fingerprintLen]
}

// actionDistinguisher returns the shape-defining sub-key the
// fingerprint hash includes alongside (rule_id, type, role) so
// siblings of the same Type within a rule get distinct counters by
// default.
//
// Per action type:
//   - publish / publish_agent / trigger_workflow / publish_boid_signal:
//     Subject — sibling actions of the same Type in a rule almost
//     always differ on Subject.
//   - add_triple / remove_triple / update_triple: Predicate + Object
//     — Predicate alone collides if two actions write the same
//     predicate to different Objects (e.g., status=approved vs
//     status=ready). Including Object closes that case.
//   - update_kv: Bucket + Key — natural composite key.
//   - deny: Reason — two `deny` actions in the same rule with
//     different reasons need distinct counters; otherwise an
//     operator can't bound them independently.
//   - other types (or fields all empty): empty distinguisher,
//     authors who hit this collision must set Action.ID explicitly.
func actionDistinguisher(a Action) string {
	switch a.Type {
	case ActionTypePublish, ActionTypePublishAgent, ActionTypeTriggerWorkflow, ActionTypePublishBoidSignal:
		return a.Subject
	case ActionTypeAddTriple, ActionTypeRemoveTriple, ActionTypeUpdateTriple:
		return a.Predicate + "|" + a.Object
	case ActionTypeUpdateKV:
		return a.Bucket + "/" + a.Key
	case ActionTypeDeny:
		return a.Reason
	}
	return ""
}

// effectiveMaxIterations resolves the per-action firing cap, honouring
// the nil-vs-zero distinction the wire format depends on:
//
//   - nil pointer (unset / JSON field omitted) → DefaultActionMaxIterations (3)
//   - pointer to 0 → 0 (caller treats as unlimited)
//   - pointer to N>0 → N
//
// Returns the resolved cap and a boolean indicating whether the
// resolution selected the framework default (true) vs an explicit
// operator-supplied value (false). Callers use the boolean to log
// the resolution source for observability — operators auditing why a
// cap fired can distinguish "you didn't set this; we used 3" from
// "you set 3 explicitly."
func (a Action) effectiveMaxIterations() (maxIter int, fromDefault bool) {
	if a.MaxIterations == nil {
		return DefaultActionMaxIterations, true
	}
	return *a.MaxIterations, false
}

// isUnlimited reports whether the resolved cap means "no limit." A
// value of 0 (operator's explicit unlimited sentinel) is the only
// unlimited form — the framework default is always finite.
func isUnlimited(maxIter int) bool {
	return maxIter == 0
}
