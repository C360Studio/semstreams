# SemMachina match and in-flight primitives

## Why

Two consumer-facing questions have no framework answer today, so the consumer rebuilds framework
internals to ask them — and both reconstructions fail **silently, in the direction that causes
harm**. gh#731: "would this rule `Definition` match this `EntityState` right now" requires a running
`Processor`, so a caller reaches for the bare `expression.Evaluator` and loses four pre-processing
steps, every one of which turns a real match into a confident `false`. gh#733: "is this loop task
still in flight" requires the loop's consumer name, which is unexported, so a caller copies the
derivation and — when it drifts — reads `ErrConsumerNotFound` as "nothing in flight" rather than
"this deployment has no agentic-loop".

Both are the same defect class the pre-v1 program keeps finding: **an absent measurement rendered as
a measurement of absence.** The framework owns both answers; it just does not expose either.

## What Changes

- **`processor/rule` gains a stateless match entry point.** It performs the pre-processing
  `ExpressionRule.EvaluateEntityState` performs — `SubstituteConditionValues` against the entity,
  and opt-in `$entity.lifecycle.*` resolution when a `Manager` is supplied — while touching none of
  the stateful bookkeeping (`shouldTrigger`, `lastTriggered`, cooldown, `MatchState`) that belongs
  to the rule engine alone.
- **It returns an error, never a silent `false`, for any condition it cannot fully resolve in
  stateless mode.** `$state.*`, `$prev.*`, and `transition` have no meaning outside a stateful
  evaluation. Today `evaluator.go:191-198` returns `false, nil` for exactly these when `Required` is
  unset — the caller cannot refuse what it is never told about.
- **It resolves the empty-condition-list inversion.** `EvaluateWithStateAndMessage` returns `true`
  for an empty list (`evaluator.go:134-136`); `EvaluateEntityState` returns `false`
  (`expression_factory.go:180-182`). The new entry point answers "would production fire this", so it
  mirrors production.
- **`processor/agentic-loop` gains an in-flight query for a task subject**, composing
  `natsclient.OutstandingWork` rather than exposing a consumer name. This is option (2) of gh#733,
  not option (1): per the exported-surface contract, a caller needs the answer, not a name it must
  then go look up, and keeping the name private leaves the derivation free to change.
- **It corrects gh#733's stated premise, which measurement has since falsified.** The issue asserts
  the consumer's "acknowledgement floor is the only authoritative answer". `AckFloor` is **not**
  authoritative: #758 D0 measured it against both deployed NATS versions and found it lies in both
  directions — it sits behind a `MaxDeliver`-exhausted message while idle, then leaps *past* the
  never-applied message on the next unrelated ack. The rejection is recorded in ADR-088.
  `OutstandingWork` (`NumPending + NumAckPending`) is the correct source, and it already makes the
  distinction gh#733 asks for: an unbound consumer is an **error**, never `(0, nil)`.
- Both symbols are **new exported surface**, so gh#761's exported-surface contract binds them and
  **Fable design review is required BEFORE implementation**. This change is scoped to stop at that
  gate.

## Capabilities

**New Capabilities**: none. Both answers belong to capabilities that already exist; inventing a home
for them would be speculative widening.

**Modified Capabilities**:
- `rule-engine` — it already owns "how a rule decides whether it matches an entity's state". A
  stateless form of that question, and the resolution contract governing what it may silently
  answer, are requirements of that capability.
- `agentic-loop` — currently scoped to per-spawn iteration budgets. This widens its Purpose to cover
  the loop's task-execution visibility contract, which is the same substrate.

## Impact

- `processor/rule/expression_factory.go` — the four pre-processing steps get lifted into a form the
  stateless path and `EvaluateEntityState` both call, so the two cannot drift. The existing method
  keeps its behavior.
- `processor/rule/expression/evaluator.go` — the silent-`false` return becomes distinguishable to a
  stateless caller. **The stateful path's behavior must not change**; a rule pack that evaluates
  today must evaluate identically after.
- `processor/agentic-loop/component.go` — `consumerName` derivation moves behind an internal helper
  shared with the new query; `sanitizeSubject` stays unexported.
- Consumers: **SemMachina** (both primitives, for a boot-time recovery pass that decides whether a
  parked turn is stranded or still owed a hop) and **semdragon** via the same integration. Named
  callers exist at birth for both symbols, satisfying gh#761's no-phantom-exports rule.
- Additive and non-breaking. No NATS state, schema, or wire-format change.

## Non-goals

- **No rule match state, cooldown bookkeeping, or `MatchState` access.** Those belong to the rule
  engine exclusively; a consumer reading them would be the real boundary violation, and gh#731
  explicitly disclaims them.
- **Not a second evaluation pipeline.** The stateless path lifts the real one. If it re-implements
  matching, the change has failed — that is the drift this exists to remove.
- **No change to ack semantics, and no sharing or rebinding of the loop's consumer.**
  Heartbeat-and-ack-on-completion is what makes outstanding work readable at all.
- **No exported consumer name.** gh#733 offers it as the cheaper option; taking it would make a
  naming detail a public contract.
- **No implementation in this change beyond the Fable gate** — proposal, deltas, and design only.
