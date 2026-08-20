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
  - There is no standalone zero-owner helper wave and no shared Wait helper. R1 is the first selected owner-family wave
    and creates final `internal/lifecyclecleanup.RollbackFailedStart(parent, rollback)` using an immediately bounded
    `WithTimeout(WithoutCancel(parent), 5s)`. Legacy `lifecyclejoin.RunPartialStartRollback` remains unchanged for
    unmigrated owners because it cannot receive a parent. Each migrated wave uses the final helper; N1 deletes the
    legacy package and proves old imports/calls zero.
  - Wave readiness is dependency-based: R1/ML1 are roots; SM1 and I1 depend on R1, and I1→S1 is the shared
    native-handle spine. Unchanged independent waves reuse the global inventory/design pass and may proceed
    concurrently. Failure blocks descendants only.
  - 2026-08-19 corrected-design checkpoint: independent review returned `DESIGN APPROVE`, and the owner stated
    “agree - continue with recommendation.” Acceptance is limited to rejecting standalone F0/shared Wait, accepting
    parent-aware `RollbackFailedStart(parent, rollback)` born with R1, and selecting R1 as the first wave. Unrelated
    exported API rulings remain unapproved. At that checkpoint R1 implementation was under review with no verdict or
    owner-migrated credit.
  - 2026-08-19 R1 implementation checkpoint: independent `semstreams-reviewer` verdict `APPROVE` grants
    owner-migrated credit only to the frozen assess, classify, execute, route, and synthesize owner files and records
    final parent-aware helper birth with five real consumers. All-six race evidence passed in 1.209s/6.488s/6.783s/
    6.871s/7.424s/7.114s; the new assess/classify/route expiry case passed three times under race; lint, diff-check, and
    strict OpenSpec validation passed. Census moved owners 36→31, NewGeneration 38→33, Generation.Stop 43→38, old
    rollback 20→15, and final helper calls 0→5; Cancel 4, StopWithQuiesce 8, and NewOperation 3 were unchanged. The
    ledger records RED evidence and exact source identities. Task 2.3 and every Gate A/B/C, runtime, proof, release,
    archive, and tag requirement remain unchecked and incomplete.
  - 2026-08-19 SM1 design correction: architect binding interpretation makes SM1 depend on R1. `Manager.StartAll`
    must locally attempt bounded parent-aware rollback before returning child Start, main bind, or publisher failure;
    process-root `StopAll` is defense-in-depth only. Planned owner -1, NewGeneration -3, and StopWithQuiesce -3 deltas
    are unchanged; final helper calls move 5→6 and old rollback calls do not move. At this correction checkpoint,
    independent design re-review and SM1 implementation review were still pending; it granted no credit.
  - 2026-08-19 SM1 implementation checkpoint: independent narrow corrected-design verdict `DESIGN APPROVE` and final
    independent implementation verdict `APPROVE` grant owner-migrated credit only to
    `service/service_manager.go`. Adjacent tests and the process-root comment receive no owner credit. Focused service
    race passed in 6.772s, the lifecycle matrix passed 20 repetitions in 1.845s, and lint/diff-check/strict OpenSpec
    validation passed. Census moved owners 31→30, NewGeneration 33→30, and StopWithQuiesce 8→5; Generation.Stop 38,
    Cancel 4, NewOperation 3, and old rollback 15 were unchanged; final helper calls moved 5→6. Full repository race
    is not claimed green: two user-owned `.claude/worktrees` entered repository-wide scanners, causing duplicate census
    and old graph-ingest target failures; separately, stale policy-baseline entries still expect two removed sleeps in
    root `service/base_test.go`. Task 2.3 and every Gate A/B/C, runtime, proof, release, archive, and tag requirement
    remain unchecked and incomplete; unrelated exported API rulings remain unapproved.
  - 2026-08-19 G1 implementation checkpoint: independent `semstreams-reviewer` verdict `APPROVE` grants
    owner-migrated credit only to the frozen graph-query, graph-clustering, graph-embedding, graph-index-spatial, and
    graph-index-temporal `component.go` owners; adjacent query/test files receive no separate owner credit. Original
    no-action Stop tests were RED in all five, and the graph-query Start-path callback lock RED timed out in `Health`
    before correction. Full five-package race, integration-tag lifecycle, `TestLifecycleOwner` race x5, lint, and
    diff-check passed. Census moved owners 30→25, NewGeneration 30→25, Generation.Stop 38→33, and final helper calls
    6→11; Cancel 4, StopWithQuiesce 5, NewOperation 3, and old rollback 15 were unchanged. Lifecyclejoin and natsclient
    remained unchanged, and the ledger records exact source identities. Task 2.3 and every Gate A/B/C, runtime, proof,
    release, archive, and tag requirement remain unchecked and incomplete; unrelated exported API rulings remain
    unapproved.
  - 2026-08-19 CM1 implementation checkpoint: independent `semstreams-reviewer` verdict `APPROVE` grants
    owner-migrated credit only to `service/component_manager.go`; supporting tests receive no owner credit. The
    initial callback-borrow fence expected an error and received nil, and the health terminal-projection RED returned
    the child map before correction. Focused service race passed in 7.868s, the CM lifecycle matrix passed 10
    repetitions in 6.740s, the integration ComponentManager/framework bucket passed in 8.950s, and
    gofmt/vet/revive/diff-check passed. Census moved owners 25→24, NewGeneration 25→23, Generation.Stop 33→32,
    StopWithQuiesce 5→3, and final helper calls 11→12; Cancel 4, NewOperation 3, and old rollback 15 were unchanged.
    Lifecyclejoin, natsclient, and Metrics inventory were unchanged. Task 2.3 and every Gate A/B/C, runtime, proof,
    release, archive, and tag requirement remain unchecked and incomplete; unrelated exported API rulings remain
    unapproved.
  - 2026-08-19 ML1 implementation checkpoint: independent `semstreams-reviewer` verdict `APPROVE` grants
    owner-migrated credit only to `service/message_logger.go`; adjacent `service/message_logger_http.go` and test
    files are supporting surfaces and receive no owner credit. Three causal RED stages exposed completed-Stop error
    replay, reconciliation admission/Drain races, and Start committing success after an expired Stop. Independent
    final service race passed in 6.709s, the MessageLogger matrix passed 10 repetitions in 7.146s, the real-NATS
    integration surface passed in 3.578s, and gofmt/vet/revive/diff-check passed. Census moved owners 24→23,
    NewGeneration 23→22, Generation.Stop 32→31, and HTTP KV invented roots 2→0; StopWithQuiesce 3, final helper
    calls 12, old rollback 15, Cancel 4, and NewOperation 3 were unchanged. Lifecyclejoin, natsclient, the request-owned
    SSE watcher, and Metrics inventory were unchanged. Task 2.3 and every Gate A/B/C, runtime, proof, release, archive,
    and tag requirement remain unchecked and incomplete; unrelated exported API rulings remain unapproved.
  - 2026-08-20 I1 reviewed owner-wave checkpoint: the owner approved only the breaking I1 native-handle return,
    duplicate-live-durable rejection, and zero-consumer `Registry.SubscribeCapabilities` removal. `ConsumeDurable`,
    port consumption methods, `natsclient.Subscription`, Metrics APIs, and later N1 retirements remain excluded.
    Independent implementation review returned `APPROVE`; both causal REDs, the corrected MaxDeliver fail-loud
    contract, package/race/integration evidence, exact census, hashes, and scanner limitations are recorded in the
    ledger. Required breaking-change runs `task e2e:agentic` and `task e2e:core` both exited 0. Owner-migrated credit is
    granted only to `agentic/agentrun/agentrun.go` and `service/milestone_service.go`; supporting natsclient,
    component, and MaxDeliver files receive no owner credit. I1 is committed at
    `07c37f7319a65c5109fe31bc36136661bc6e9243`. Task 2.3, Gate A/B/C, runtime migration, proof, release, archive, and
    tag readiness remain unchecked and incomplete.
  - 2026-08-20 OT1 implementation checkpoint: independent `semstreams-reviewer` verdict `APPROVE` grants
    owner-migrated credit only to `output/otel/component.go`; supporting unit and lifecycle-integration tests receive
    no owner credit. The process-global opaque `(stream, durable)` claim, pull-loop one-shot shutdown, causal Operation
    replay RED, race/integration/stress evidence, exact census, and source hashes are recorded in the ledger. No
    outward API, configuration, natsclient, consumer-deletion, or Metrics surface changed. Task 2.3, Gate A/B/C,
    runtime migration, proof, release, archive, and tag readiness remain unchecked and incomplete.
  - 2026-08-20 S1 implementation checkpoint: the owner approved only the temporary standard port-handle bridge under
    the branch no-release/no-tag invariant; the split-context bridge is deferred to A1. Independent
    `semstreams-reviewer` verdict `APPROVE` grants owner-migrated credit only to the frozen document, IoT, Weather,
    JSON filter, JSON generic, and JSON map owner files. Supporting natsclient and test files receive no owner credit.
    The bridge blocker/correction, proof HIGH/correction, causal REDs, exact race/integration timings, authoritative
    census, scanner pollution, and all source hashes are recorded in the ledger. The canonical port method,
    split-context method, `ConsumeDurable`, `natsclient.Subscription`, Metrics APIs, and N1 retirements remain excluded.
    Task 2.3, Gate A/B/C, runtime migration, proof, release, archive, and tag readiness remain unchecked and incomplete.
  - 2026-08-20 A1 implementation checkpoint: S1's conditional split-bridge approval is exercised by Loop, its first
    real caller, under the same branch no-release/no-tag invariant. Independent `semstreams-reviewer` verdict
    `APPROVE` grants owner-migrated credit only to the frozen dispatch, governance, loop, model, and tools owner
    `component.go` files. Supporting natsclient, `http_activity`, `inflight`, and test files receive no owner credit.
    The two HIGH findings and corrections, causal REDs, exact race/integration/E2E evidence, census, and all source
    hashes are recorded in the ledger. No name-routed lifecycle, deletion, or outward change beyond the conditionally
    approved split bridge was introduced. Task 2.3, Gate A/B/C, runtime migration, proof, release, archive, and tag
    readiness remain unchecked and incomplete.
  - 2026-08-20 H1 implementation checkpoint: independent `semstreams-reviewer` verdict `APPROVE` grants
    owner-migrated credit only to `gateway/graph-gateway/component.go`, `input/websocket/websocket_input.go`, and
    `output/websocket/websocket.go`; readiness and test files receive no owner credit. The occupied-bind RED, all five
    causal review corrections, focused/full/integration race evidence, core E2E, census, and exact source identities
    are recorded in the ledger. No outward API, configuration, context surface, name-routed lifecycle, or deletion
    change was introduced. M1, ServiceManager HTTP, and pprof remain excluded; temporary bridges keep the branch under
    the no-release/no-tag invariant. Task 2.3, Gate A/B/C, runtime migration, proof, release, archive, and tag readiness
    remain unchecked and incomplete.
  - 2026-08-20 O1 implementation checkpoint: independent `semstreams-reviewer` verdict `APPROVE` grants
    owner-migrated credit only to `output/file/file.go` and `output/httppost/httppost.go`; tests and package
    documentation receive no owner credit. Adopter docs now state one-shot instances, serialized lifecycle
    transitions, caller-bounded Stop, completed repeat Stop nil, and fresh-instance reuse without changing public
    signatures or configuration. The causal REDs, HTTP idle-connection blocker/correction, exact evidence, census, and
    source identities are recorded in the ledger. M1, OS1, RU1, GI1, N1, unrelated APIs, and every broader gate remain
    excluded; temporary bridges keep the branch under the no-release/no-tag invariant. Task 2.3, Gate A/B/C, runtime
    migration, proof, release, archive, and tag readiness remain unchecked and incomplete.
  - 2026-08-20 OS1 implementation checkpoint: independent `semstreams-reviewer` verdict `APPROVE` grants
    owner-migrated credit only to `storage/objectstore/component.go`; tests and narrowly corrected package concurrency
    docs receive no owner credit. Exact-handle lifecycle ownership preserves durable topology, StoreProvider
    availability during callback drain, and store/reference-before-ACK semantics. The causal RED, exact evidence,
    integration repeat caveat, census, and live identities are recorded in the ledger. No outward API, configuration,
    context, or consumer-deletion surface changed; OS1 removes its internal name-routed `Client.StopConsumer` call
    without adding a name-routed lifecycle surface. RU1, GI1, M1, N1, and every broader gate remain excluded;
    temporary bridges preserve the no-release/no-tag invariant. Task 2.3, Gate A/B/C, runtime migration,
    proof, release, archive, and tag readiness remain unchecked and incomplete.
  - 2026-08-20 GI1 implementation checkpoint: independent `semstreams-reviewer` verdict `APPROVE` grants
    owner-migrated credit only to `processor/graph-ingest/component.go`; `keyed_ingest.go`, `readiness.go`, and tests
    receive no owner credit. The exact-handle shutdown order preserves effect → durable guard → settlement,
    readiness observation, subjects, configuration, and schema while removing two stored contexts and two unauthorized
    roots.
    The causal RED, exact race/integration/core-E2E/contract evidence, census, repository-wide limitations, and live
    identities are recorded in the ledger. No outward API, name-routed lifecycle, or deletion surface changed. RU1,
    M1, N1, and every broader gate remain excluded; temporary bridges preserve the no-release/no-tag invariant. Task
    2.3, Gate A/B/C, runtime migration, proof, release, archive, and tag readiness remain unchecked and incomplete.
  - 2026-08-20 RU1 implementation checkpoint: the owner's `approved` applies only to coherent rulings R1-R5:
    context-first Rule APIs, immediate context-bearing KV initialization, an internal cron dispatcher, nil-context
    rejection by `Matches`, and barrier-before-snapshot shutdown ordering. Implementation and re-review code findings
    are cleared, and the ledger records REDs, exact evidence, census, hashes, migration impact, and repository-wide
    limitations. Independent implementation review returned `APPROVE`; a reviewer-run isolated structural E2E from
    the final production identity passed 38/38, then removed only its isolated stack and volume. Owner-migrated credit
    is limited to `processor/rule/processor.go`; all adjacent Rule files, composition callers, docs, migration guidance,
    and tests are supporting only. Task 2.3, Gate A/B/C, runtime migration, proof, release, archive, and tag readiness
    remain unchecked and incomplete; temporary bridges preserve the no-release/no-tag invariant.
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
  the unchanged legacy rollback implementation, obsolete tests, and the entire `internal/lifecyclejoin` package; prove
  old imports/symbols zero while final lifecyclecleanup helpers retain only their reviewed stateless contracts.
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
