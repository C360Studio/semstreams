package expression

import "strings"

// Stateful field prefixes.
//
// This is the SINGLE SOURCE for "this field is resolved from the StateFields
// carrier, not from entity triples". Both readers derive from it:
// evaluateConditionWithStateAndMessage branches on it to decide where a field
// comes from, and EnsureStatelessResolvable refuses on it so a caller that
// cannot supply state is told rather than answered.
//
// Keeping one list is not tidiness. A stateless pre-scan carrying its own copy
// of these prefixes would drift from the evaluator's, and a prefix present in
// one list and absent from the other resolves to the exact defect gh#731 was
// filed about: a confident `false` for a question nobody answered.
const (
	statePrefix     = "$state."
	prevPrefix      = "$prev."
	lifecyclePrefix = "$entity.lifecycle."
)

// hasStatefulPrefix reports whether a condition field is read from StateFields
// rather than from the entity's own triples.
func hasStatefulPrefix(field string) bool {
	return strings.HasPrefix(field, statePrefix) ||
		strings.HasPrefix(field, prevPrefix) ||
		strings.HasPrefix(field, lifecyclePrefix)
}

// EnsureStatelessResolvable reports whether a condition can be answered without a
// stateful evaluation, returning nil when it can and an error naming the offending
// field when it cannot.
//
// It exists because the evaluator's own behavior for an unresolved stateful field
// is correct for the stateful caller and wrong for a stateless one. When
// `Required` is unset, evaluateConditionWithStateAndMessage returns `false, nil`
// for a missing `$state.*` / `$prev.*` / `$entity.lifecycle.*` field. That is
// right when the caller SUPPLIED the state map and its absence is therefore
// meaningful. It is wrong when the caller never had the opportunity to supply
// one: it hands back a confident "no match" for a question that was never asked.
//
// The failure direction is what makes this a guard rather than a nicety. A
// consumer asking "does this rule pack still owe this entity a hop" reads a false
// negative as "this entity is stranded" and intervenes. Across a pack whose
// first-hop rule carries such a condition, that misclassifies every entity in the
// world. An error a caller can refuse on is recoverable; a fabricated verdict is
// not.
//
// lifecycleResolved reports whether the caller supplied a lifecycle Manager. With
// one, `$entity.lifecycle.*` fields are pre-resolved into StateFields and are
// answerable; without one they are not, which is why answerability — not
// preference — is what the Manager governs.
//
// This is a pre-scan, deliberately: it runs BEFORE evaluation and leaves the
// evaluator untouched, so the stateful path cannot regress in service of the
// stateless one.
func EnsureStatelessResolvable(condition ConditionExpression, lifecycleResolved bool) error {
	// The transition operator compares against $prev.* tracked across
	// evaluations. There is no stateless form of "did this change" — a single
	// EntityState is one observation, and one observation has no previous.
	if condition.Operator == OpTransition {
		return &EvaluationError{
			Field:    condition.Field,
			Operator: OpTransition,
			Message:  "transition requires stateful evaluation: a single entity state carries no previous value to compare against",
		}
	}

	if !hasStatefulPrefix(condition.Field) {
		return nil
	}

	// Lifecycle fields are the one stateful class a stateless caller CAN make
	// answerable, by supplying the Manager they are resolved from.
	if strings.HasPrefix(condition.Field, lifecyclePrefix) {
		if lifecycleResolved {
			return nil
		}
		return &EvaluationError{
			Field:   condition.Field,
			Message: "lifecycle field requires a lifecycle Manager: pass one to resolve it, or the field cannot be answered",
		}
	}

	return &EvaluationError{
		Field:   condition.Field,
		Message: "field requires stateful evaluation: it is resolved from rule match state, which does not exist outside a running rule engine",
	}
}
