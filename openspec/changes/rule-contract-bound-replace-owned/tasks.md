# Tasks: Rule Contract-Bound Replace-Owned

## 1. Lock the action and composition contracts with failing tests

- [ ] 1.1 Extend `processor/rule/actions_replace_owned_test.go` with explicit `projection_contract` and
  `projection_group` authoring cases, including missing, unknown, unnamed, wrong-mode, and ambiguous targets.
- [ ] 1.2 Add full-group behavior tests for delete-on-omission, entire-group clear, sibling-group isolation, birth
  predicate preservation, foreign/append predicate rejection, and typed object substitution.
- [ ] 1.3 Add action-boundary outcome tests proving `MutationReceipt.KVRevision` reaches the per-rule revision tracker
  and `errors.As`/`errors.Is` preserve `*projection.MutationError` and its classified cause.
- [ ] 1.4 Extend `processor/rule/config_projection_test.go` to lock JSON and schema round trips for the two new action
  selectors without changing projection-contract wire fields.
- [ ] 1.5 Extend hot-reload tests to reject pack ID, contract, group, or client-envelope changes and accept only rules
  whose replacement targets resolve in the frozen index.
- [ ] 1.6 Add a preflight/start snapshot test proving a rule file is parsed and validated before binding and is not
  reread into a different mutation target between bind and `StartAll`.

## 2. Preflight and bind the public client

- [ ] 2.1 Rewrite `service/rule_pack_bind.go` so all enabled packs are preflighted before the first side effect.
- [ ] 2.2 Validate unique pack IDs, complete contract sets, action-target indexes, action targets, NATS and
  contract-required registry/heartbeater dependencies, and enabled-pack overlap during preflight.
- [ ] 2.3 Call `projection.BindMutationClient` exactly once per enabled contract-bearing pack with owner
  `rule-pack.<packID>` and inject only `projection.OwnedReplacer` before `StartAll`.
- [ ] 2.4 Remove all `BindAndHeartbeat`, `Registry.OwnerToken`, `SetProjectionOwnerToken`, and observe-only binding
  behavior from the rule-pack composition path.
- [ ] 2.5 Extend `service/rule_pack_bind_integration_test.go` for zero, one, and multiple contract-bearing packs; no
  client for empty packs; invalid and duplicate preflight; overlap failure; missing dependencies; and no start.
- [ ] 2.6 Update both `cmd/semstreams/main.go` and `cmd/e2e-semstreams/main.go` to supply the same mutation-client
  composition dependencies and preserve the pre-`StartAll` gate.

## 3. Build the immutable rule target index

- [ ] 3.1 Add `processor/rule/projection_targets.go` and `projection_targets_test.go` for copied,
  contract/group/predicate indexing and complete group predicate sets.
- [ ] 3.2 Extend `Action` in `processor/rule/actions.go` with `projection_contract` and `projection_group`.
- [ ] 3.3 Refactor `processor/rule/rule_loader.go` and `config_validation.go` so preflight and start consume one
  validated initial-rule snapshot and hot reload validates against the same frozen index.
- [ ] 3.4 Require both selectors for every `replace_owned` action and hard-reject literal predicates outside the
  selected named `replace-owned` group.
- [ ] 3.5 Update `processor/rule/runtime_config.go` to reject `pack_id`, `projection_contracts`, target-index, and
  mutation-client changes atomically; hot reload must not call any bind path.

## 4. Execute through `projection.OwnedReplacer`

- [ ] 4.1 Replace owner/token fields in `processor/rule/processor.go` and `actions.go` with one-time
  `projection.OwnedReplacer` injection into `Processor` and `ActionExecutor`.
- [ ] 4.2 Build one `projection.ReplaceOwnedMutation` per action with the selected contract, selected group, resolved
  entity ID, complete desired state, and stable rule/action metadata.
- [ ] 4.3 Preserve typed substitution; treat empty object as an empty desired set for the complete selected group.
- [ ] 4.4 Track non-zero `MutationReceipt.KVRevision` through the existing per-rule revision tracker.
- [ ] 4.5 Wrap failures with `%w`; do not flatten mutation kind, code, class, commit state, or the underlying cause.
- [ ] 4.6 Add no action-level blind retry; rely on the public client's operation-specific retry and verification.

## 5. Migrate the lesson reference pack

- [ ] 5.1 Update `configs/rules/lessons/lesson-lifecycle-rulepack.json` with named group `lesson-lifecycle`.
- [ ] 5.2 Add the exact eleven `birth_predicates` from the design, including `agent.action.executed-by`.
- [ ] 5.3 Add `projection_contract: agentic.lesson-record` and `projection_group: lesson-lifecycle` to the illustrative
  birth-time replacement action.
- [ ] 5.4 Update `configs/rules/lessons/README.md` and focused config tests to explain complete-group omission and
  confirm the exact contract, group, birth predicates, and selectors.

## 6. Delete the duplicate ReplaceOwned lane

- [ ] 6.1 Run a production-caller audit for `projectionOwnerToken`, `SetProjectionOwnerToken`,
  `SetProjectionOwner`, `TripleMutator.ReplaceOwned`, and `SubjectEntityUpdateWithTriples`.
- [ ] 6.2 Remove processor and executor owner/token fields and setters after the audit reports zero required callers.
- [ ] 6.3 Remove `ReplaceOwned` from `TripleMutator` and delete the raw update-with-triples implementation from
  `processor/rule/triple_mutator.go`.
- [ ] 6.4 Delete or rewrite `processor/rule/owner_token_test.go`,
  `processor/rule/actions_replace_owned_integration_test.go`, and mock signatures only after equivalent public-client
  boundary evidence exists.
- [ ] 6.5 Retain raw `AddTriple` and `RemoveTriple`; record their remaining caller inventory in #688 rather than
  expanding this PR.

## 7. Verification and delivery gates

- [ ] 7.1 Run focused rule and service unit tests with race detection.
- [ ] 7.2 Run integration tests against real graph-ingest for full-group clearing, sibling and birth preservation,
  stale owner-token typing, must-exist behavior, and no auto-vivification.
- [ ] 7.3 Run `go test -race ./...`, `task lint`, `task schema:generate`, and confirm no generated-schema drift.
- [ ] 7.4 Run the applicable structural/semantic E2E tier because this retires a production mutation path.
- [ ] 7.5 Run `rg` deletion gates and prove no raw ReplaceOwned request construction or token setter remains in
  production rule code.
- [ ] 7.6 Keep #688 open with bare Add/Remove retirement explicitly outstanding; cross-link delivery evidence to
  #313, PR #687, PR #696, and #688.
- [ ] 7.7 Obtain semstreams-reviewer approval for architecture compliance, error semantics, concurrency safety,
  full-group behavior, hot-reload immutability, and deletion evidence before merge.
