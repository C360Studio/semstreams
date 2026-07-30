## Why

Two semdev blockers on rule-engine evaluation completeness (verified still present at beta.147):

- **gh#519**: there is no warn-free scalar field-to-field comparison. A condition value of
  `$entity.triple.<pred>` floods a misleading "likely silent-pass bug" WARN on every entity that legitimately
  does not carry the predicate (hundreds-to-thousands per run), because substitution is eager over all
  conditions and the graceful forms (`.length`, `.triples`) are count/array — not scalar. The enforced 3-part
  predicate contract (PR #532) now makes a clean fix possible: a `.value` suffix disambiguated purely by
  ARITY — `$entity.triple.a.b.c.value` is predicate `a.b.c` + suffix because `a.b.c` parses as a canonical
  predicate, while `$entity.triple.a.b.value` is the literal predicate `a.b.value` because `a.b` cannot be
  one. No presence heuristic, no ambiguity, by contract.
- **gh#530**: an `on_recovery`-only rule (empty on_enter/on_exit/while_true) is completely inert — the
  `hasStatefulActions` gate on both evaluation paths excludes `OnRecovery`, so the rule never reaches the
  stateful evaluator, never persists MatchState, and its recovery actions never fire on restart. A pure
  fail-closed recovery park is inexpressible.

## What Changes

- Add the `$entity.triple.<predicate>.value` substitution form: resolves to the triple's scalar object value;
  on an absent predicate it resolves to the empty string WITHOUT the unresolved-variable WARN — the
  documented scalar-graceful member completing the `.length`/`.triples` family. Arity disambiguation: the
  `.value` suffix applies iff the preceding token sequence parses as a canonical 3-part predicate.
- Include `len(OnRecovery) > 0` in the stateful-routing predicate on BOTH evaluation paths, so
  on_recovery-only rules reach the stateful evaluator, persist MatchState during live operation, and fire
  their recovery actions on the bootstrap path. Prove empty enter/exit/while lists are handled through the
  hardened watcher seams.
- Grammar-collision audit (house rule): grep every `$`-token regex before landing the new suffix; document
  the form in the rule-engine substitution reference.

## Non-goals

- Multi-valued predicate selection semantics for `.value` (first-match on the canonical ordering; anything
  richer is future work with a consumer).
- Per-rule entity filters on the entity-state path (separate design).
- Warn-behavior changes for the existing bare `$entity.triple.<pred>` form (unchanged; `.value` is the
  supported scalar form).

## Capabilities

### Modified Capabilities

- `rule-engine` (spec seeded by this change): scalar triple substitution and recovery-only rule evaluation.

## Impact

- `processor/rule` (execution_context substitution, message_handler gate, stateful evaluator, docs).
- semdev unblocked: warn-free run-scoped gate rules + expressible fail-closed recovery parks. Closes gh#519,
  gh#530. Supersedes the stale `fix/gh519-rule-scalar-value-substitution` branch (pre-wave; abandoned).
