// Package rule — stateless Definition matching (gh#731).
package rule

import (
	"context"
	"fmt"
	"strings"

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
func Matches(ctx context.Context, def Definition, state *gtypes.EntityState, lifecycle LifecycleLookup) (bool, error) {
	if state == nil {
		return false, fmt.Errorf("rule.Matches: entity state is nil")
	}

	// A disabled rule owes nothing — it cannot fire, so it cannot be mid-delivery.
	// Production refuses it first (expression_factory.go), and cooldown is the ONLY
	// divergence this function claims, so anything else that gates firing has to be
	// mirrored here or that claim is false.
	if !def.Enabled {
		return false, nil
	}

	if len(def.Conditions) == 0 {
		return false, nil
	}

	// Resolve lifecycle state BEFORE deciding answerability. "A lookup was
	// supplied" and "lifecycle state resolved" are different facts: a supplied
	// lookup that fails leaves the fields unresolved, and the evaluator would then
	// answer false for an optional `$entity.lifecycle.*` condition. Refusing on the
	// lookup error is the whole point — an unregistered participant or a transient
	// KV failure must not read as "nothing owed".
	stateFields := expression.StateFields{}
	lookupErr := populateLifecycleStateFields(ctx, lifecycle, state.ID, stateFields)

	// The lookup error is CARRIED, not raised here. A failed lookup only matters if
	// some condition actually needs lifecycle state — `LookupByEntityID` errors for
	// any entity that simply is not lifecycle-managed, which is the ordinary case,
	// so raising unconditionally would refuse every ordinary definition whenever a
	// caller happened to supply a Manager. (Measured: it did. Caught by mutating
	// the guard, not by reading the code.)
	//
	// Answerability is driven by what ACTUALLY resolved, not by whether a lookup was
	// handed in. isNilLookup absorbs the typed-nil interface case.
	lifecycleResolved := len(stateFields) > 0
	for _, condition := range def.Conditions {
		err := expression.EnsureStatelessResolvable(condition, lifecycleResolved)
		if err == nil {
			continue
		}
		// When a lifecycle condition is unanswerable AND a supplied lookup failed,
		// report the real cause. The generic message says "pass a Manager to resolve
		// it", which is actively misleading for a caller that passed one.
		if lookupErr != nil && strings.HasPrefix(condition.Field, lifecycleSubstitutionPrefix) {
			return false, fmt.Errorf(
				"rule.Matches: definition %q: condition on %q is unanswerable because the "+
					"supplied lifecycle lookup failed for %q: %w",
				def.ID, condition.Field, state.ID, lookupErr)
		}
		return false, fmt.Errorf("rule.Matches: definition %q: %w", def.ID, err)
	}

	logic := def.Logic
	if logic == "" {
		logic = "and"
	}

	expr := substituteConditionsForEntity(def.Conditions, logic, state, lifecycle)

	// A template that survived substitution is a "could not resolve", and it must
	// not reach an operator. eq/contains do not error on a leftover token — they
	// compare it as an ordinary string and return a confident verdict, which is
	// this capability's defining defect wearing a different hat.
	if err := ensureNoUnresolvedConditionValues(def.ID, expr.Conditions); err != nil {
		return false, err
	}

	// A fresh evaluator per call: it carries no cross-call state, and sharing one
	// would be the first thread of exactly the stateful coupling this function
	// exists to avoid.
	return dispatchEvaluation(expression.NewExpressionEvaluator(), state, stateFields, expr)
}

// ensureNoUnresolvedConditionValues refuses any condition whose Value still carries
// a `$`-template after substitution.
//
// It reuses unresolvedTemplateVarRe — the same pattern the action path uses to warn
// about surviving tokens — rather than a second literal list, for the same
// single-source reason the stateful prefix set has one home.
func ensureNoUnresolvedConditionValues(defID string, conditions []expression.ConditionExpression) error {
	for _, condition := range conditions {
		s, ok := condition.Value.(string)
		if !ok {
			continue
		}
		if token := unresolvedTemplateVarRe.FindString(s); token != "" {
			return fmt.Errorf(
				"rule.Matches: definition %q: condition on %q has an unresolved value template %q; "+
					"it would be compared as a literal string and produce a confident wrong verdict",
				defID, condition.Field, token)
		}
	}
	return nil
}
