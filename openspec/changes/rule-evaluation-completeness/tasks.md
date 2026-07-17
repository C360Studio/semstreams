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
- [ ] 3.3 `task e2e:structural` green (rule paths exercised end-to-end)
- [ ] 3.4 Close gh#519 (noting the superseded WIP branch) and gh#530 with fix references
