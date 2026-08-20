// Package rule — stateless Definition matching (gh#731).
package rule

import (
	"context"
	"fmt"
	"strings"

	"github.com/c360studio/semstreams/pkg/lifecycle"

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
// # The enumeration behind that claim
//
// "Cooldown is the only divergence" is a claim about a CLASS — every gate that can
// stop the engine firing — so here is the class, enumerated from
// EvaluateEntityState rather than asserted:
//
//	!r.enabled                   -> mirrored (false, nil)
//	entityState == nil           -> mirrored (error; a caller passing nil is a bug)
//	cooldown window open         -> DELIBERATELY not mirrored; see above
//	len(conditions) == 0         -> mirrored (false, nil)
//	evaluation returned an error -> deliberately NOT collapsed to false; see below
//
// The `Enabled` row is why this enumeration is written down instead of trusted: it
// was missing in the first implementation, which falsified this very claim while
// the doc comment went on asserting it. Any new gate added to EvaluateEntityState
// must be added here and mirrored, or the claim above is false again.
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
// Matches answers WITHOUT a lifecycle lookup, so `$entity.lifecycle.*` conditions
// cannot be resolved and are refused with an error rather than evaluated against an
// absent value. If you have a lookup, call MatchesWithLifecycle — the pair is split
// precisely so that this limitation is visible in the name you typed, rather than
// hidden in a nil argument or an omitted option.
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
func Matches(ctx context.Context, def Definition, state *gtypes.EntityState) (bool, error) {
	return matchesWithLookup(ctx, "Matches", def, state, nil)
}

// MatchesWithLifecycle is Matches for a caller that HAS a lifecycle lookup, so
// `$entity.lifecycle.*` conditions can be answered instead of refused.
//
// The pair exists rather than one function with a nil-able parameter because the
// lookup governs ANSWERABILITY, not preference — with it a class of conditions can
// be evaluated, without it that class cannot be. Splitting puts that fact in the
// function NAME, where a call site cannot miss it. A single nil-able parameter
// leaves `Matches(ctx, def, state, nil)` looking complete while silently giving up
// on every lifecycle condition, and a variadic option would hide it further still:
// `Matches(ctx, def, state)` would compile, read as finished, and fail at runtime.
//
// manager is REQUIRED here. Passing nil is a caller mistake, not a degraded mode —
// the function for "I have no manager" is Matches — so it is refused rather than
// silently downgraded.
//
// The parameter is the CONCRETE *lifecycle.Manager (owner ruling 2026-07-31,
// matching what §1 approved). Concrete is what makes the nil check total: `manager
// == nil` on a pointer is always correct, so a typed nil cannot exist here and the
// panic-inside-LookupByEntityID hazard is unrepresentable rather than merely
// guarded. The nil-checked pointer is only then widened to the interface the
// internals take, so no typed nil can reach them either.
func MatchesWithLifecycle(
	ctx context.Context, def Definition, state *gtypes.EntityState, manager *lifecycle.Manager,
) (bool, error) {
	if manager == nil {
		return false, fmt.Errorf(
			"rule.MatchesWithLifecycle: lifecycle manager is nil; call Matches instead if " +
				"you have none — lifecycle conditions are then refused explicitly rather " +
				"than silently unanswered")
	}
	return matchesWithLookup(ctx, "MatchesWithLifecycle", def, state, manager)
}

// matchesWithLookup is the single implementation behind both entry points, so the
// pair cannot drift.
func matchesWithLookup(
	ctx context.Context, caller string, def Definition,
	state *gtypes.EntityState, lifecycle LifecycleManager,
) (bool, error) {
	if ctx == nil {
		return false, fmt.Errorf("rule.%s: context is nil", caller)
	}
	if state == nil {
		return false, fmt.Errorf("rule.%s: entity state is nil", caller)
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
	// Answerability is driven by what ACTUALLY resolved, not by whether a manager was
	// handed in.
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
				"rule.%s: definition %q: condition on %q is unanswerable because the "+
					"supplied lifecycle lookup failed for %q: %w",
				caller, def.ID, condition.Field, state.ID, lookupErr)
		}
		return false, fmt.Errorf("rule.%s: definition %q: %w", caller, def.ID, err)
	}

	logic := def.Logic
	if logic == "" {
		logic = "and"
	}

	expr := substituteConditionsForEntity(ctx, def.Conditions, logic, state, lifecycle)

	// A template that survived substitution is a "could not resolve", and it must
	// not reach an operator. eq/contains do not error on a leftover token — they
	// compare it as an ordinary string and return a confident verdict, which is
	// this capability's defining defect wearing a different hat.
	if err := ensureNoUnresolvedConditionValues(caller, def.ID, expr.Conditions); err != nil {
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
func ensureNoUnresolvedConditionValues(caller, defID string, conditions []expression.ConditionExpression) error {
	for _, condition := range conditions {
		s, ok := condition.Value.(string)
		if !ok {
			continue
		}
		if token := unresolvedTemplateVarRe.FindString(s); token != "" {
			return fmt.Errorf(
				"rule.%s: definition %q: condition on %q has an unresolved value template %q; "+
					"it would be compared as a literal string and produce a confident wrong verdict",
				caller, defID, condition.Field, token)
		}
	}
	return nil
}
