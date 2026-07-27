# Tasks: Rule Contract-Bound Replace-Owned

## 1. Lock the action and composition contracts with failing tests

- [x] 1.1 Extend `processor/rule/actions_replace_owned_test.go` with explicit `projection_contract` and
  `projection_group` authoring cases, including missing, unknown, unnamed, wrong-mode, and ambiguous targets.
- [x] 1.2 Add full-group behavior tests for sibling deletion, omitted/raw-empty object group clear, raw-non-empty
  object substitution resolving empty, sibling-group isolation, create-only birth-predicate preservation,
  foreign/append predicate rejection, and typed object substitution.
- [x] 1.3 Add action-boundary tests for not-found and stale-token errors, commit state and unwrap preservation, and
  non-zero `MutationReceipt.KVRevision` feedback tracking.
- [x] 1.4 Extend `processor/rule/config_projection_test.go` to lock JSON and schema round trips for the two new action
  selectors without changing projection-contract wire fields.
- [x] 1.5 Extend hot-reload tests to reject pack ID, contract, group, or client-envelope changes and accept only rules
  whose replacement targets resolve in the frozen index.
- [x] 1.6 Add a preflight/start snapshot test proving a rule file is parsed and validated before binding and is not
  reread into a different mutation target between bind and `StartAll`.
- [x] 1.7 Register every contract predicate used by unit and integration fixtures through isolated vocabulary setup;
  tests must not pass only because another test leaked global vocabulary state.

## 2. Preflight and bind the public client

- [x] 2.1 Rewrite `service/rule_pack_bind.go` so every enabled rule pack is preflighted before the first rule-pack
  bind; retain the intentionally earlier #696 built-in aggregate bind.
- [x] 2.2 Validate unique pack IDs, complete copied contract sets, exact action-target indexes, initial actions,
  required NATS for every client, Registry only for non-empty registrations, heartbeater only for owning replace/CAS
  claims, and pack-pack overlap during side-effect-free preflight.
- [x] 2.3 Call `projection.BindMutationClient` exactly once per disjoint enabled contract-bearing pack with owner
  `rule-pack.<packID>` and inject only `projection.OwnedReplacer` before `StartAll`.
- [x] 2.4 Remove all `BindAndHeartbeat`, `Registry.OwnerToken`, `SetProjectionOwnerToken`, and observe-only binding
  behavior from the rule-pack composition path.
- [ ] 2.5 Add service tests for zero, one, and multiple packs, empty packs, missing NATS for any client, missing
  Registry for a non-empty registration, missing heartbeater for owning replace/CAS claims, nil Registry and
  heartbeater accepted for claimless/birth-only contracts, nil heartbeater accepted when non-empty registration has
  no owning claim, injection failure, and proof that no processor starts after a composition error.
  Remaining: add the non-owning, non-empty registration case with a nil heartbeater; all other listed cases pass.
- [x] 2.6 Verify `cmd/semstreams/main.go` and `cmd/e2e-semstreams/main.go` already make the identical existing
  `BindRulePackContracts` call before `StartAll`; do not churn either call site unless the helper signature changes.
- [x] 2.7 Add service integration tests for duplicate pack IDs, repeated claim-bearing binding preserving
  `ErrOwnerAlreadyBound`, repeated claimless/birth-only composition failing one-time client injection without a
  sentinel assertion, pack-pack overlap, pack-vs-#696-built-in overlap, and stale external overlap; identical
  repeated inputs must never succeed, and every failure must be fail closed.

## 3. Build the immutable rule target index

- [x] 3.1 Add `processor/rule/projection_targets.go` with focused coverage in
  `processor/rule/actions_replace_owned_test.go` for copied contract/group/predicate indexing and complete group
  predicate sets.
- [x] 3.2 Extend `Action` in `processor/rule/actions.go` with `projection_contract` and `projection_group`.
- [x] 3.3 Refactor `processor/rule/rule_loader.go` and `config_validation.go` so preflight and start consume one
  validated initial-rule snapshot and hot reload validates against the same frozen index.
- [x] 3.4 Require both selectors for every `replace_owned` action and hard-reject literal predicates outside the
  selected named `replace-owned` group.
- [x] 3.5 Update `processor/rule/runtime_config.go` to reject `pack_id`, `projection_contracts`, target-index, and
  mutation-client changes atomically; hot reload must not call any bind path.

## 4. Execute through `projection.OwnedReplacer`

- [x] 4.1 Replace owner/token fields in `processor/rule/processor.go` and `actions.go` with one-time
  `projection.OwnedReplacer` injection into `Processor` and `ActionExecutor`.
- [x] 4.2 Build one `projection.ReplaceOwnedMutation` per action with the selected contract, selected group, resolved
  entity ID, complete desired state, and stable rule/action metadata.
- [x] 4.3 Preserve typed substitution; clear the complete selected group only when raw `Action.Object` is omitted or
  empty, and emit one desired triple when raw `Action.Object` is non-empty even if substitution resolves empty.
- [x] 4.4 Track non-zero `MutationReceipt.KVRevision` through the existing per-rule revision tracker.
- [x] 4.5 Wrap failures with `%w`; do not flatten mutation kind, code, class, commit state, or the underlying cause.
- [x] 4.6 Add no action-level blind retry; rely on the public client's operation-specific retry and verification.
- [x] 4.7 Add real `MutationClient`/graph-ingest integration coverage for sibling deletion, group isolation,
  create-only birth-predicate preservation, not-found, stale-token typing, and receipt revision propagation.

## 5. Defer built-in-owned rule consumption

- [x] 5.1 Remove the lesson lifecycle reference-pack/config/README/test migration from the PR2 diff.
- [x] 5.2 Prove `agentic.lesson-record` / `lesson-lifecycle` overlaps the #696 built-in owner and that a rule-pack bind
  fails closed; do not add a waiver or observe-only exception.
- [x] 5.3 Keep rules that consume built-in-owned groups under #688/shared follow-up until a separate prebound-client,
  least-privilege injection design is reviewed.
- [x] 5.4 Document birth predicates as create-only through the public client, excluded from replacement removal
  sets, and not graph-enforced immutable facts.

## 6. Delete the duplicate ReplaceOwned lane

- [x] 6.1 Run a production-caller audit for `projectionOwnerToken`, `SetProjectionOwnerToken`,
  `SetProjectionOwner`, `TripleMutator.ReplaceOwned`, and `SubjectEntityUpdateWithTriples`.
- [x] 6.2 Remove processor and executor owner/token fields and setters after the audit reports zero required callers.
- [x] 6.3 Remove `ReplaceOwned` from `TripleMutator` and delete the raw update-with-triples implementation from
  `processor/rule/triple_mutator.go`.
- [x] 6.4 Replace raw `tripleMutator`/owner-token tests with the real `MutationClient` integration evidence in 4.7;
  delete old tests only after every boundary assertion has an equivalent.
- [x] 6.5 Retain raw `AddTriple` and `RemoveTriple`; record their remaining caller inventory in #688 rather than
  expanding this PR.
- [x] 6.6 Audit the final diff for zero lesson reference-pack migration and zero implicit reuse of the #696 client.

## 7. Verification and delivery gates

- [x] 7.1 Run focused rule and service unit tests with race detection.
- [x] 7.2 Run tagged integration tests against real graph-ingest for full-group clearing, group isolation,
  create-only preservation, not-found, stale-token typing, receipt revision, and no auto-vivification.
- [x] 7.3 Run repository-wide test/race, vet, lint, build, schema generation, and generated-drift gates.
- [x] 7.4 Run production and fixture predicate audits, including the isolated vocabulary registrations from 1.7.
- [x] 7.5 Run the applicable structural/semantic E2E tier because this retires a production mutation path, then
  tear down the stack explicitly.
- [x] 7.6 Run deletion and scope audits proving no raw ReplaceOwned request/token setter remains, no deferred
  Add/Remove work moved, no lesson migration remains, and `git diff --check` passes.
- [x] 7.7 Run strict validation for this OpenSpec change and the complete OpenSpec set; run Markdown and line checks.
- [ ] 7.8 Keep #688 open for raw Add/Remove and built-in prebound-client design; cross-link delivery evidence to
  #313, PR #687, PR #696, and #688.
- [x] 7.9 Obtain independent architecture and Go review for fail-closed composition, error semantics, concurrency,
  full-group behavior, hot-reload immutability, and deletion evidence.
- [x] 7.10 Request Fable review only if implementation exposes an unresolved public projection-contract/framework
  issue or materially expands that reviewed surface; there is no automatic Fable gate for this internal migration.
- [ ] 7.11 Rerun the aggregate tagged integration suite on clean-host CI. Local Docker/testcontainers startup failed
  before assertions in processor/graph-index and agentic-loop; this aggregate gate is not locally green.

## Checkpoints

- Base: `46e1e6cb`; rebased SDD: `e8a739ec`; implementation checkpoint: `15037036`.
- Task 2.6 is verified at the checkpoint: both binaries already call the same helper before `StartAll`.
- Architecture decision: option A scopes `ErrOwnerAlreadyBound` to non-empty registrations and uses one-time client
  injection failure for repeated claimless/birth-only composition; identical contract-bearing repeats are never
  idempotent.
- Implementation and focused evidence: rule/service unit and race suites pass; PR-relevant tagged
  `MutationClient`/graph-ingest tests pass; the full tagged processor/graph-ingest race suite passes in 62.548s;
  the exact graph-index regression passes in 0.45s.
- Review evidence: architect APPROVED; independent Go reviewer APPROVED; generated schema reviewed.
- `task check:push` passed at a clean checkpoint, including build, lint, vet/tag checks, schema generation with zero
  drift, contract checks, and repository-wide `go test -race ./...` on rerun.
- Aggregate tagged integration remains pending under 7.11. Local NATS 8222 mapping and Docker daemon inspect
  timeouts prevented processor/graph-index and agentic-loop setup before assertions; agentic-loop setup failure
  persisted locally. These infrastructure failures are not recorded as a green aggregate run.
- `task e2e:structural` passes 37/37; explicit teardown left no containers.
- Predicate audits pass with `predicate:audit` reporting 503 candidates and `predicate:test-audit` reporting 2,221
  candidates with 147 exact fixtures.
- Target strict OpenSpec and all 33/33 strict validations pass. Deletion audit finds zero production legacy
  ReplaceOwned/token/update-with-triples matches; AddTriple/RemoveTriple remain intentionally inventoried for #688.
- Base-relative diffs for lesson artifacts, `pkg/ownership`, and `pkg/projection` are zero; `git diff --check` is
  clean.
- Delivery references are #313, PR #687, PR #696, and #688. Task 7.8 remains open until the PR exists and can carry
  the final delivery-evidence cross-link.
- Fable re-review was not requested because the base-relative diff does not expand the public
  projection-contract/framework API.
