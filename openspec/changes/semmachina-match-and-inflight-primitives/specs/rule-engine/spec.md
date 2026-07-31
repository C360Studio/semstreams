# rule-engine delta — stateless Definition matching

## ADDED Requirements

### Requirement: A rule Definition MUST be matchable against an EntityState without a running Processor
The framework SHALL expose a stateless entry point answering "would this rule `Definition` match this
`EntityState` now", usable without constructing a `Processor` or binding any watcher.

That entry point SHALL perform the same pre-processing the stateful evaluation performs — `$`-templated
condition **value** substitution against the entity, and `$entity.lifecycle.*` field resolution when a
`pkg/lifecycle.Manager` is supplied — by **calling the same code the stateful path calls**, not by
reproducing it. A second implementation of matching is a defect, not an alternative: the divergence it
creates is the reason this requirement exists.

The stateless path SHALL NOT read or write any rule-engine bookkeeping: match state, trigger latches,
last-triggered timestamps, and iteration counters remain the engine's exclusively. Calling it SHALL NOT
be observable in the engine's subsequent behavior.

#### Scenario: A caller matches a definition with no Processor in the process

- **GIVEN** a rule `Definition` and an `EntityState`, and no running rule `Processor`
- **WHEN** the caller asks whether the definition matches
- **THEN** it receives the same verdict the stateful evaluation would reach for those conditions
- **AND** no watcher, consumer, or component lifecycle is required

#### Scenario: Templated condition values resolve against the entity

- **GIVEN** a condition whose value is a `$`-template such as `$entity.triple.foo.length`
- **WHEN** the definition is matched statelessly
- **THEN** the template resolves against the entity before the operator is applied
- **AND** the operator never receives the literal template text

#### Scenario: A stateless match leaves the engine's state untouched

- **GIVEN** a running rule engine holding match state for a definition
- **WHEN** a stateless match is performed for that same definition and an entity
- **THEN** the engine's match state, trigger latch, and last-triggered timestamp are unchanged
- **AND** a subsequent stateful evaluation behaves exactly as if the stateless call had not happened

### Requirement: A stateless match MUST refuse a condition it cannot fully resolve, never answer it
The stateless entry point SHALL return an error naming the unresolvable field, and SHALL NOT return a
match verdict, whenever a condition depends on state that has no meaning outside a stateful
evaluation — a `$state.*` or `$prev.*` pseudo-field, or a `transition` condition.

This SHALL hold regardless of whether the condition is marked `Required`. The stateful evaluator today
returns `false, nil` for an unresolved `$state.*` / `$prev.*` / `$entity.lifecycle.*` field whose
`Required` flag is unset. That is correct for the stateful path, where the caller supplied the state
map and its absence is meaningful. It is wrong for a stateless caller, which never had the opportunity
to supply one: the caller receives a confident "no match" for a question the framework never answered.

**The failure direction is why this is normative.** A consumer asking "does this rule pack still owe
this entity a hop" reads a false negative as "this entity is stranded" and intervenes. Applied across a
pack whose first-hop rule carries such a condition, that misclassifies every entity in the world.
An error the caller can refuse on is recoverable; a fabricated verdict is not.

The stateful evaluation path's behavior SHALL NOT change. A rule pack that evaluates today MUST
evaluate identically afterward.

#### Scenario: An unresolvable state field errors instead of reporting no match

- **GIVEN** a definition with a condition on `$state.something` and `Required` unset
- **WHEN** the definition is matched statelessly with no state supplied
- **THEN** an error is returned naming the unresolvable field
- **AND** no boolean verdict is returned

#### Scenario: A transition condition is refused, not evaluated

- **GIVEN** a definition carrying a `transition` condition
- **WHEN** it is matched statelessly
- **THEN** an error is returned identifying the condition as requiring stateful evaluation

#### Scenario: Lifecycle fields resolve when a Manager is supplied and error when it is not

- **GIVEN** a definition with a condition on `$entity.lifecycle.phase`
- **WHEN** it is matched statelessly WITH a lifecycle `Manager` supplied
- **THEN** the field resolves and the match proceeds
- **WHEN** it is matched statelessly WITHOUT one
- **THEN** an error is returned rather than a verdict computed from an absent value

#### Scenario: The stateful path keeps its existing tolerance

- **GIVEN** the running rule engine evaluating the same definition through its normal path
- **WHEN** an optional `$state.*` field is absent from the supplied state map
- **THEN** the condition evaluates false as it does today, without error

### Requirement: A stateless match MUST answer the question production would answer
The stateless path SHALL mirror the stateful one wherever the two could disagree on identical input,
because the caller is asking what production would do.

Specifically, a definition carrying **no conditions** SHALL NOT match. The two existing code paths
disagree on this today — the evaluator treats an empty condition list as passing, while the rule
wrapper returns false before reaching it — and the wrapper is the production answer.

Any gate the stateless path cannot evaluate because it is inherently stateful — notably the
per-rule cooldown, which is a property of a rule instance's history rather than of the definition —
SHALL be documented as not applied, and the contract SHALL state the direction of the resulting
disagreement so a caller can reason about it.

#### Scenario: A definition with no conditions does not match

- **GIVEN** a `Definition` carrying an empty condition list
- **WHEN** it is matched statelessly
- **THEN** the verdict is no-match, agreeing with the running engine rather than with the bare evaluator

#### Scenario: Cooldown is documented as unapplied rather than silently skipped

- **GIVEN** a definition whose rule would currently be within its cooldown window in a running engine
- **WHEN** it is matched statelessly
- **THEN** the conditions are evaluated on their merits, cooldown not applied
- **AND** the contract states this explicitly, so a caller knows the stateless verdict can be
  permissive relative to a running engine and never the reverse
