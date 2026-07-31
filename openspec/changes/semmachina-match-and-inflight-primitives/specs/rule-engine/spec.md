# rule-engine delta — stateless Definition matching

## ADDED Requirements

### Requirement: A rule Definition MUST be matchable against an EntityState without a running Processor
The framework SHALL expose stateless entry points answering "would this rule `Definition` match this
`EntityState` now", usable without constructing a `Processor` or binding any watcher.

Lifecycle answerability SHALL be expressed by WHICH entry point the caller invokes — one that takes
a lifecycle lookup and one that does not — rather than by a nil-able parameter or an omitted option.
The lookup governs which class of conditions can be answered at all, not how they are answered, so
that fact belongs where a call site cannot miss it. A single nil-able parameter leaves a
lookup-less call reading as complete while silently giving up on every lifecycle condition, and an
optional variadic hides it further: the call compiles, reads as finished, and fails at runtime.
Both entry points SHALL share one implementation so the pair cannot drift.

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

The stateless entry point SHALL likewise propagate an **evaluation** error rather than converting it
to a no-match verdict, even though the running engine converts it. The engine logs and returns false
because a rule that cannot be evaluated must not fire; that is correct for firing and wrong for
asking. The two SHALL therefore differ on error handling while never differing on a verdict, and the
contract SHALL make the resulting three-way distinction explicit to callers:

| Result | Means |
|---|---|
| no-match, no error | evaluation completed; the conditions do not hold; nothing is owed |
| error | evaluation could not complete — absent `Required` field, operator failure, unresolved template |

Collapsing the second row into the first reproduces this capability's own defect one level up: a
malformed definition or a mis-shaped entity would report "nothing owed", and a consumer reads that as
"stranded" and intervenes.

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

#### Scenario: Lifecycle answerability is selected by which entry point is called

- **GIVEN** a definition with a condition on `$entity.lifecycle.phase`
- **WHEN** it is matched through the entry point that TAKES a lifecycle lookup
- **THEN** the field resolves and the match proceeds
- **WHEN** it is matched through the entry point that takes NO lookup
- **THEN** an error is returned rather than a verdict computed from an absent value

#### Scenario: A supplied lookup that failed is not resolved state

- **GIVEN** a lifecycle lookup is supplied but its resolution fails — an unregistered
  participant or a transient backend failure
- **WHEN** a definition carrying a lifecycle condition is matched
- **THEN** an error naming the underlying lookup failure is returned, not a no-match verdict
- **AND** a definition carrying NO lifecycle condition is unaffected by that failure

#### Scenario: An absent lookup on the lifecycle entry point is refused, not downgraded

- **GIVEN** the entry point that requires a lifecycle lookup
- **WHEN** it is called with no lookup, including a typed-nil one
- **THEN** it returns an error directing the caller to the no-lookup entry point
- **AND** it does not silently behave as though no lookup were needed

#### Scenario: An unevaluable condition is distinguishable from an unmet one

- **GIVEN** a definition whose condition marks a field `Required` and an entity lacking that field
- **WHEN** it is matched statelessly
- **THEN** an error is returned rather than a no-match verdict
- **AND** the running engine, given the same input, still returns a bare no-match as it does today

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

The per-rule cooldown SHALL NOT be applied, and the contract SHALL state that the stateless entry
point answers **obligation** — "does this rule pack still owe this entity work?" — where the running
engine answers **instant** — "would this rule fire right now?".

Cooldown is a rate limiter, not a match negation: a rule inside its cooldown window still owes the
entity the hop and will fire when the window expires. The two paths therefore answer two different
questions, and the stateless one is not an approximation of the other. It follows as a corollary that
a stateless verdict can differ from a running engine only by matching where the engine would be
cooling down, never the reverse — but the contract SHALL be written as the distinct question, not as
a caveat on the instant one, so a consumer needing the instant answer can see the primitive is not
theirs rather than reading a tolerance note and hoping it does not apply.

A definition declaring a cooldown SHALL NOT be refused on that basis.

#### Scenario: A definition with no conditions does not match

- **GIVEN** a `Definition` carrying an empty condition list
- **WHEN** it is matched statelessly
- **THEN** the verdict is no-match, agreeing with the running engine rather than with the bare evaluator

#### Scenario: A cooling-down rule still reports an outstanding obligation

- **GIVEN** a definition whose rule would currently be within its cooldown window in a running engine
- **AND** whose conditions match the entity
- **WHEN** it is matched statelessly
- **THEN** the verdict is a match, because the pack still owes this entity the hop
- **AND** the contract identifies this as the obligation question rather than as a tolerated
  divergence from the instant one

#### Scenario: A cooldown-bearing definition is answered, not refused

- **GIVEN** a definition that declares a cooldown
- **WHEN** it is matched statelessly
- **THEN** it is evaluated normally
- **AND** no error is returned on account of the cooldown field being present
