## 1. Failing Tests First

- [x] 1.1 `.value` substitution: present predicate → scalar object; absent predicate → empty string with NO
      unresolved-template WARN; literal predicate ending in `.value` (`a.b.value`) still resolves as the
      3-part predicate; `agent.lineage.<key>.value` works; non-string objects render via the canonical
      scalar rendering; integration-style test drives NewExpressionRule(def).EvaluateEntityState (production
      wire, not helper-direct)
- [x] 1.2 on_recovery-only rule: fires recovery actions on bootstrap after restart with persisted MatchState;
      still inert for messages that never matched; empty enter/exit/while handled through the stateful
      evaluator without spurious enter/exit firings; deterministic-seam test on the hardened watcher paths
- [x] 1.3 Grammar-collision audit recorded: grep of every `$`-prefixed token regex, result noted in the PR

## 2. Implementation

- [x] 2.1 Arity-based `.value` suffix parsing in the substitution layer; suppression of the unresolved WARN
      for the `.value` form only
- [x] 2.2 Add OnRecovery to the hasStatefulActions predicate on both paths; verify MatchState persistence and
      recovery-fork classification for actionless-live rules
- [x] 2.3 Update rule-engine substitution + entity-watching docs; note the scalar-graceful family
      (.length/.triples/.value) in one table

## 3. Gates

- [x] 3.1 `task lint`, `go test -race ./...`, `go test ./test/contract/...`, schema drift clean
- [x] 3.2 `go test -race -tags=integration -p 2 ./processor/rule/...`
- [x] 3.3 `task e2e:structural` green (rule paths exercised end-to-end)
      — GREEN 2026-07-30. `Scenario completed successfully`, `validation_errors:0`,
      `rules_validation_passed:1`, `rules_evaluated_count:613`, `rules_firings_count:6`,
      `validate-rules` + `validate-rule-transitions` stages both ran. **Scope caveat, recorded
      deliberately:** the run was on the `feat/697-713-add-lane-dedup` tree, i.e. this change's
      already-merged code PLUS the add-lane dedup work — not a clean-main run. It is evidence that
      the merged rule paths pass structural e2e, and arguably stronger for carrying an unrelated
      change on top; it is NOT a clean-main measurement. Re-run on main if that distinction ever
      matters.
- [x] 3.4 Close gh#519 (noting the superseded WIP branch) and gh#530 with fix references
      — both verified CLOSED 2026-07-30 via `gh issue view`
