# Tasks

## 1. Accepted design

- [x] 1.1 Complete the bounded admission/reload inventory and independent review.
  `inventory.md`, SHA-256 `b915b346fe72a30a2b229ff233d03001397ab990ffbac812779300923d8899cb`;
  105/105 pins verified and independent INVENTORY PASS at the claim baseline.
- [x] 1.2 Complete the publication-only contract and independent design review.
  Accepted pre-status-edit `design.md`, SHA-256
  `d561f666bdf6a5ec423140dd5968360b27b2cc3ed0b59e50bb214b98f637d186`; see `review.md`.
- [x] 1.3 Specify the stacked integration sequence without copied helpers or frozen-parent changes.
- [x] 1.4 Obtain explicit owner acceptance of the corrected design and stacked Git arrangement.
  Ruling: https://github.com/C360Studio/semstreams/issues/1311#issuecomment-5676602813.
  Acceptance does not establish prerequisite implementation or authorize widening the target.

## 2. Materialization and prerequisite gate

- [x] 2.1 Materialize and independently review the accepted rule-engine delta and finite tasks.
  Validate OpenSpec. The invariant sources below pin the materialized requirement/scenario lines.
  Reconcile proposal/review/task status without rewriting frozen evidence.
  Independent delta review PASS and strict validation 54/54 on 2026-09-15; exact identities in `review.md`.
- [ ] 2.2 HOLD implementation and restacking until an exact reviewed #1159 prerequisite commit is published.
  Confirm the stacked #1156 semantic delivery types, #1159 SettleDelivery and correlation contract, and required
  R8 declared-output/PubAck behavior with exact-revision evidence. Approval is not proof R8 is implemented.
  Prove required publish/approve/rejection output through the production codec and correlation reader.
  Known unresolved boundary: generic executePublish still emits raw JSON on the inspected #1159 source.
  Its wire contract is not fixed by the publish_agent TaskMessage envelope or automatically by R8 transport work.
  If a necessary wire correction lacks an accepted owner contract, return that bounded dependency to the owner;
  do not duplicate serialization, silently absorb a publisher migration, or retire publish-plus-deny.
- [ ] 2.3 After 2.2, perform the owner-approved restack onto that published #1159 commit.
  Target #1312 at the #1159 branch; preserve reviewed WIP and refresh only changed evidence.
  No shared-helper copy, API hoist, or modification of the frozen #1156 parent.
  Gate 2.2 blocks this task and all implementation below.

## 3. Finite TDD implementation and proof

- [ ] 3.1 RED then GREEN: admission and decoding through production configuration entry points.
  Cover dedicated/mixed/misrouted inputs; multiple built-in rules; custom factories not invoked;
  accepted publish/approve/deny composition; refused stateful/non-message/unsafe controls;
  malformed/unknown/lossily decoded fields; and zero proposal effects on refusal.
  Add spec-cited grammar properties/fuzz seeds for accepted and rejected forms.
- [ ] 3.2 RED then GREEN: per-delivery evaluation and semantic settlement.
  Cover action ordering, guards, permissive approve, rule-local deny, immediate publication-error propagation,
  no StateTracker access or persisted counter mutation, Complete/Retry/Terminate cases, cancellation,
  uncertain PubAck, and no second different settlement method after settlement failure.
- [ ] 3.3 RED then GREEN: prepare-then-install and current-policy replay.
  Cover failed required KV reads, malformed snapshots, proposal preparation failure, unrelated entity failure,
  IDs entering/leaving the proposal portion, complete old-policy preservation, and one locked final install.
  Prove in-flight snapshot stability and use of a newer active policy on the next attempt.
  Do not assert rollback or atomicity for unrelated entity changes.
- [ ] 3.4 Prove native JetStream delivery and publication ordering.
  Use the installed production callback, a real source delivery, actual PubAck, and authoritative redelivery.
  Cover a later publication failure repeating an earlier success, and cold restart after publication before ACK.
  A fake source with real output publication is not sufficient evidence.
- [ ] 3.5 Prove the integrated production wire and governing outcomes.
  Exercise publish-rejection-then-deny and approve through the actual codec/correlation consumer.
  Prove no-match/no-action/bare-deny completion grants no enforce approval, audit remains nongating,
  and audit/optional notification cannot substitute for required PubAck.
  Reuse unchanged parent proof where its exact revision and boundary still apply.
- [ ] 3.6 Prove neighboring contracts and lifecycle preservation.
  Cover ordinary message/entity recovery, projection one-attempt/no-old-context-replay controls,
  optional notifications, best-effort audit, exact consumer shutdown, and settlement-control failure.

All new tests follow the canonical testing policy: lowest sufficient tier, race detection, explicit synchronization,
bounded diagnostic waits, and isolated resources. Native integration uses the canonical runner and host lock.
Do not introduce sleeps, extra test infrastructure, or broad suite repairs to obtain this proof.

## 4. Review, verification, and landing

- [ ] 4.1 Obtain independent implementation/proof review against the accepted delta and exact stacked revision.
  Record RED/GREEN assertions, native evidence, remaining prerequisites, and scope limits.
- [ ] 4.2 Update migration/concept documentation and shipped configurations as required by the accepted change.
  Explain dedicated input, publication-only admission, per-delivery semantics, current-policy retry,
  saved-versus-active truth, and integrated wire/correlation requirements.
  Preserve rule multiplicity and publish-plus-deny. Sister owners perform downstream changes.
- [ ] 4.3 Run applicable preflight gates and relevant agentic E2E on the combined stack.
  Run the breaking-change E2E before landing; report actual assertions, revision, and results.
  This task does not close the remaining loop-side #1146 R7 gates.
- [ ] 4.4 Review archive/spec sync as the last content commit and record the accepted landing handoff.
  This branch-checkable task records readiness and constraints, not future merge or post-merge results.

Landing constraints (not child checklist completion): merge the approved child into #1159, not independently to main.
Land #1156 through its existing gates; reconcile and reverify the combined #1159 branch afterward.
Claim the end-to-end guarantee only with this prerequisite and remaining R7 proof present.
The final default-branch PR carries the agreed closing references; a non-default child merge is not closure.

## Spec-cited invariant sources

All sources below are requirements/scenarios in `specs/rule-engine/spec.md`.
Properties must derive expectations from these clauses, not source code.

| Invariant | Spec source |
| --- | --- |
| Unsupported input/composition produces no proposal effects or ordinary successful ACK | `spec.md:3`, `spec.md:40` |
| No custom factory invocation or match bookkeeping in proposal evaluation | `spec.md:33`, `spec.md:77` |
| Within-rule order and local denial without new cross-rule arbitration | `spec.md:94` |
| Complete never precedes owed PubAck; transient/uncertain publication failure retries | `spec.md:109`, `spec.md:130`, `spec.md:137` |
| Poison is not success; no second terminal method after settlement failure | `spec.md:145` |
| Completion without a usable verdict grants no enforce approval | `spec.md:153` |
| Rejected replacement preserves the old proposal portion, including cross-lane IDs | `spec.md:181`, `spec.md:212` |
| An attempt retains its snapshot; a later attempt may use newer policy | `spec.md:220`, `spec.md:229` |
