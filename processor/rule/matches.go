// Package rule — stateless Definition matching (gh#731).
package rule

import (
	"fmt"

	gtypes "github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/processor/rule/expression"
)

// Matches reports whether a rule Definition's conditions match an entity's current
// state, without a running Processor.
//
// It answers OBLIGATION — "does this rule pack still owe this entity work?" — where
// a running rule engine answers INSTANT — "would this rule fire right now?". The two
// are different questions, not a question and an approximation of it. The difference
// is cooldown: a rule inside its cooldown window still owes the entity the hop and
// will take it when the window expires, so a cooldown is a rate limiter rather than
// a match negation and plays no part here. If you need the instant answer — "may
// this rule fire at this moment" — this is not the function you want; that question
// belongs to the engine, which owns the timing state that answers it.
//
// The consequence worth stating: a Matches verdict can differ from a live engine's
// only by matching where the engine would be cooling down, never the reverse.
//
// Matches performs the same pre-processing the engine performs, by calling the same
// code the engine calls (evaluateConditionsAgainstEntity) rather than reproducing
// it. That is deliberate and load-bearing. A caller that reaches for
// expression.Evaluator directly loses condition-value substitution and lifecycle
// resolution, and inherits the evaluator's empty-condition-list semantics — three
// divergences, each of which silently reports the wrong verdict.
//
// It touches NO engine state: no trigger latch, no last-triggered timestamp, no
// match state. Calling it is not observable in a running engine's behavior.
//
// lifecycle may be nil. It governs ANSWERABILITY, not preference: with a lookup,
// `$entity.lifecycle.*` conditions resolve; without one they cannot be answered and
// Matches returns an error rather than a verdict. That is why it is a parameter and
// not an option — passing nil is an honest "I don't have one", and the refusal that
// follows is the correct outcome rather than a degraded one.
//
// Matches returns an error, never a bare false, for any condition it cannot fully
// resolve — `$state.*`, `$prev.*`, and `transition` have no meaning outside a
// stateful evaluation. A caller must be able to refuse; a fabricated "no match" is
// the failure that gh#731 was filed about, because a consumer reads it as "this
// entity is stranded" and acts.
//
// # Evaluation errors propagate — a DELIBERATE divergence from the engine
//
// EvaluateEntityState logs an evaluation error at Debug and returns false, because
// a rule that cannot be evaluated must not fire. That is right for firing and wrong
// for asking. Matches propagates the error instead, so the two paths differ on
// error handling while never differing on a verdict.
//
// The distinction the caller gets is worth the divergence:
//
//   - false, nil — evaluation completed and the conditions do not hold. Nothing is
//     owed. Safe to act on.
//   - false, err — evaluation could not complete: a Required field is absent, an
//     operator failed, a template did not resolve. NOT a statement about
//     obligation, and the caller must not read it as one.
//
// Collapsing the second into the first is the exact defect this function exists to
// remove, one level up: a malformed definition or a mis-shaped entity would report
// "nothing owed", and a recovery pass reads that as "stranded" and intervenes.
// Treat an error as "cannot tell — leave it alone", which is the safe action in
// both directions.
func Matches(def Definition, state *gtypes.EntityState, lifecycle LifecycleLookup) (bool, error) {
	if state == nil {
		return false, fmt.Errorf("rule.Matches: entity state is nil")
	}

	// Refuse before evaluating anything. A pre-scan keeps the stateful
	// evaluator untouched — the alternative, a caller-mode branch inside
	// evaluateConditionWithStateAndMessage, would put this cold path's
	// concerns inside the hot rule-firing path.
	haveLifecycle := lifecycle != nil
	for _, condition := range def.Conditions {
		if err := expression.EnsureStatelessResolvable(condition, haveLifecycle); err != nil {
			return false, fmt.Errorf("rule.Matches: definition %q: %w", def.ID, err)
		}
	}

	logic := def.Logic
	if logic == "" {
		logic = "and"
	}

	// A fresh evaluator per call: it carries no cross-call state, and
	// sharing one would be the first thread of exactly the stateful
	// coupling this function exists to avoid.
	return evaluateConditionsAgainstEntity(
		expression.NewExpressionEvaluator(), def.Conditions, logic, state, lifecycle)
}
