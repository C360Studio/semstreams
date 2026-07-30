# rule-engine Specification

## Purpose

`rule-engine` governs **condition evaluation** — how a rule decides whether it matches an entity's
state. It owns the questions "can this condition read this value at all" and "does this rule get
evaluated in the first place": scalar triple values must be readable in conditions without a
spurious warning when the predicate is simply absent, and recovery-only rules must actually be
evaluated rather than silently skipped because they declare no stateful action.

**What it does NOT cover.** Rule *actions* and what they may mutate belong to
`rule-projection-mutations`. Rule identity and event shape belong to the rule-event capability.
Orchestration boundaries — what belongs in a rule versus a component — are an architecture concern
(ADR-028, `/orchestration-check`), not a spec requirement here. This capability is the evaluator's
contract with the entity state it reads.
## Requirements
### Requirement: Scalar triple values are readable in conditions without warning on absence

The substitution layer MUST support `$entity.triple.<predicate>.value` resolving to the triple's scalar
object value. The `.value` suffix MUST be recognized iff the preceding tokens parse as a canonical three-part
predicate; otherwise the full token sequence MUST be treated as the literal predicate. On an entity that does
not carry the predicate, the form MUST resolve to the empty string without emitting the unresolved-template
warning. The bare `$entity.triple.<predicate>` form's behavior is unchanged.

#### Scenario: field-to-field equality gate

- **GIVEN** an entity carrying openspec.validated "r42" and openspec.change.revision "r42"
- **WHEN** a condition compares field openspec.validated eq value "$entity.triple.openspec.change.revision.value"
- **THEN** the condition evaluates true with no substitution warning

#### Scenario: absence is silent and non-matching

- **GIVEN** an entity that does not carry openspec.change.revision
- **WHEN** the same condition is evaluated
- **THEN** the substituted value is the empty string, the condition is false, and no unresolved-template
  warning is logged

#### Scenario: a literal predicate ending in .value is not truncated

- **GIVEN** an entity carrying the canonical predicate metrics.sample.value with object "7"
- **WHEN** a condition uses "$entity.triple.metrics.sample.value"
- **THEN** it resolves to "7" as the literal three-part predicate, because "metrics.sample" is not a valid
  predicate

### Requirement: Recovery-only rules are evaluated

A rule whose only actions are declared in `on_recovery` MUST be admitted to the stateful evaluator on both
evaluation paths, MUST persist match state during live operation, and MUST fire its recovery actions on the
bootstrap path after restart. Empty enter/exit/while action lists MUST NOT cause spurious firings or exclude
the rule from evaluation.

#### Scenario: fail-closed recovery park fires on restart

- **GIVEN** a rule with conditions matching an in-flight work entity and actions only in on_recovery
- **WHEN** the entity matches during live operation and the processor restarts
- **THEN** the recovery actions fire exactly once for that entity on bootstrap

#### Scenario: never-matched entities do not recover

- **GIVEN** the same rule and an entity that never matched before the restart
- **WHEN** the processor restarts
- **THEN** no recovery action fires for that entity

