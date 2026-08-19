> **Execution authority:** follow [`recovery-ledger.md`](recovery-ledger.md). This file records task state; it does not
> turn a merged contract PR, strict OpenSpec validation, or a green test suite for the old machinery into runtime
> completion. No runtime task below is complete until its ledger evidence and mechanical exit gate both pass.

## 1. Contract transaction

- [x] 1.1 Record pinned inventory and owner-approved audit digest.
- [x] 1.2 Add ADR-095, new change artifacts, verbatim inventories, and exact-title deltas.
- [x] 1.3 Transfer every retained PR #984 restart-safe requirement and scenario before removing its old deltas.
- [x] 1.4 Reconcile every inventoried normative and adopter surface across both older active changes and migration
  guidance; preserve compatible context/boot truth.
- [x] 1.5 Validate all three active changes and all specs strictly.
- [x] 1.6 Archive the new change successfully on a throwaway copy and prove the original worktree did not change.

## 2. Owner handles/admission/lifecycle simplification

- [ ] 2.1 Reorder consume commit and retain native handles.
  - `ConsumeDurable` is not a zero-consumer deletion. N1 alone retires it after an owner-approved stateless
    `NewDurableHandler` preserves effective-AckWait validation and `ConsumeWithHeartbeat` settlement composition, and
    migration guidance names ten sibling production calls plus affected interfaces. No earlier wave receives removal
    credit.
- [ ] 2.2 Add reject-not-replace identity validation/claim.
- [ ] 2.3 Migrate every production owner in the rebased recovery census; preserve failed-Start/startDone authority and
  make terminal callback-borrow shutdown fence new borrows, wait for admitted callbacks to return outside manager/gate
  locks, then let outer composition request Stop without callback self-stop.
  - 2026-08-19 process authority: the independently passed global inventory and target-wave design amortize
    inventory/design review across every unchanged frozen wave. Each wave still preserves per-owner
    TDD/race/source-identity/census evidence and passes independent implementation review. Only split membership,
    premise-changing drift, a new/changed outward surface, a new protocol/context/observation exception, or
    prerequisite API-shape change returns that wave to inventory/design review. A failed wave blocks its dependents
    only; any independent reviewed wave with complete prerequisites may proceed concurrently in an isolated worktree.
    This grants no task, owner, Gate A/B/C, proof, release, archive, or tag credit; all existing gates remain unchanged
    and unchecked.
  - F0 lands final `internal/lifecyclecleanup.Wait` and `RollbackFailedStart`. Unmigrated lifecyclejoin rollback calls
    may forward temporarily; every migrated wave uses the final package. N1 deletes lifecyclejoin, so old imports and
    old rollback calls reach zero.
  - 2026-08-18 checkpoint: independent `semstreams-reviewer` verdict `CORRECTIONS CONFIRMED` grants owner-migrated
    credit to `input/file` and `input/http` only for focused owner-local implementation and race evidence at dirty
    worktree base `cd6f570ec9fc8e0fed43eabb2c353b4de36a6d29`. Task 2.3 and Gate A remain unchecked and incomplete.
    `output/file` and `output/httppost` remain Q-primary owners with F facets; neither was changed or receives credit.
  - 2026-08-18 graph-index checkpoint: dirty worktree based on merged `main` `e7789f6c` replaces the private keyed
    dispatcher's Generation/independent Stop with parent cancellation and exact `done`, and corrects parent
    failed-Start authority/cleanup using the same owner-local shutdown path. Focused and package race tests pass;
    production owner files move 39 to 38. Owner-migrated credit is limited to
    `processor/graph-index/keyed_dispatcher.go`; the already lifecyclejoin-free parent receives no census credit.
    Task 2.3, Gate A, runtime migration, and every proof/release gate remain unchecked and incomplete.
  - 2026-08-18 dispatcher prerequisite: dirty worktree based on merged `main` `0f7687a7` makes
    `BoundedDispatcher.Stop` return pool/context/join failure and bounds completion-watcher join. Focused, package,
    real-NATS integration, and lint gates pass. Counts are unchanged and this grants zero owner, Gate, proof, release,
    archive, or tag credit; task 2.3 remains unchecked.
  - 2026-08-18 gated-DAG checkpoint: dirty worktree based on merged `main` `a1a68a78` replaces the executor's
    `Generation` with owner-local cancel/done/WaitGroup ownership, retains exact dispatcher and goroutine-local KV
    watcher authority, makes the Component boot-only, and proves fresh-Component recovery against retained NATS.
    Focused/package and real-NATS integration race tests pass; production owner files move 38 to 37. Owner-migrated
    credit is limited to `processor/gated-dag/executor.go`; task 2.3, Gate A, runtime migration, and every proof/release
    gate remain unchecked and incomplete.
  - 2026-08-19 BaseService checkpoint: final independent reviewer verdict `APPROVE` grants owner-migrated credit only
    to `service/base.go` in the dirty worktree based on clean `main` `c5953972`. Owner-local cancel/done/WaitGroup,
    one-shot Stop, fresh-instance restart, exact completion status, focused/package race tests, lint, and strict
    OpenSpec validation pass; production owner files move 37 to 36. Task 2.3, Gate A, runtime migration, and every
    proof/release/archive/tag gate remain unchecked and incomplete.
- [ ] 2.4 After every frozen owner wave is implementation-reviewed, delete Generation, Operation, StopWithQuiesce,
  compatibility forwarding, obsolete tests, and the entire `internal/lifecyclejoin` package; prove old imports/symbols
  zero while final lifecyclecleanup helpers retain only their reviewed stateless contracts.
- [ ] 2.5 Remove lifecycle deletion and provide fixture/admin teardown.

## 3. Client minimal and raw roots

- [ ] 3.1 Remove child lifecycle catalogs and same-name replacement; retain read-only observation.
- [ ] 3.2 Make Close terminal transport-only.
- [ ] 3.3 Execute every approved native-surface RETIRE/NARROW row.

## 4. Controlled and dirty proof

- [ ] 4.1 Prove exact controlled ordering, failed-Start cleanup, duplicate rejection, process exit, and fresh boot.
- [ ] 4.2 Prove settlement/outbound flush and declared external-effect/DoubleAck posture.
- [ ] 4.3 Prove durable-only crash-critical communication, live storage/replica validation, process and NATS kill/restart,
  clean-marker independence, and latest-desired-state recovery.
