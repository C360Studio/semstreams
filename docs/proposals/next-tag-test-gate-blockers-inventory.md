# Next-tag test gate blockers inventory

## Evidence boundary

- Repository: SemStreams.
- Inspected runtime baseline: `35a64ee19ad86f14bd2a1fc6fe0b39984e169a35`.
- The working tree also contained the reviewed #1059 test change. It does not touch the two #1062 reproduction sites or
  #1063 runtime path, but it removes one Background Stop cleanup from the repository-wide census.
- Provenance commit:
  `61fbd48ff05045b5f2f8d1eec390e78f63a0d3c5`
  (`refactor(lifecycle)!: restore context-owned shutdown`).
- Issue evidence: #1062 and #1063, with related #220 and #736.
- Line references are from the inspected baseline.
- Each census below is bound to its named snapshot and is textual inventory, not an AST-semantic total.

Current truth read for this inventory:

- `openspec/specs/runtime-context-ownership/spec.md`
- `openspec/specs/service-shutdown/spec.md`
- `openspec/specs/agentic-tools/spec.md`
- `openspec/specs/jetstream-consumer-policy/spec.md`
- `openspec/specs/release-candidate-proof/spec.md`
- active `align-standard-lifecycle-tests` proposal, design, delta, tasks, and conformance
- ADR-095
- archived restore-context inventory, design, and proposal

This document inventories current behavior only. It does not choose target state or authorize implementation.

## #1062: unbounded test cleanup

### Claimed gap

The two Rule readiness integration tests terminate their shared NATS test authority in function defers and later run
processor cleanup with `Processor.Stop(context.Background())`. If admitted work does not settle after the early NATS
teardown, the test removes its local shutdown bound and leaves the canonical suite's 20-minute process timeout as the
first enforcing deadline.

The exact sites are:

- `processor/rule/readiness_integration_test.go:67`
- `processor/rule/readiness_integration_test.go:82`
- `processor/rule/readiness_integration_test.go:106`
- `processor/rule/readiness_integration_test.go:132`

The two earlier sites call `defer tc.Terminate()`. The two later sites register processor Stop with `t.Cleanup` after
`Start(ctx)` at lines 81 and 131. Ordinary function defers run before testing Cleanup callbacks, so each test terminates
NATS before its processor Stop begins.

`natsclient.NewTestClient` already registers `Terminate` through `t.Cleanup` at
`natsclient/test_client.go:852-869`. `Terminate` is idempotent at lines 929-938. That built-in cleanup is registered
before the later processor cleanup; testing Cleanup callbacks run last-added-first, so without the redundant explicit
defer the processor Stop would run before NATS termination.

The test operation contexts are finite 90-second roots at lines 63-64 and 102-103. Their deferred cancellation runs
before Cleanup. `testing.T.Context()` is also canceled immediately before Cleanup callbacks, so it is not a usable
cleanup authority.

The canonical runner at `scripts/run-integration-tests.sh:304-311` uses uncapped package parallelism and one
`go test ... -timeout=20m`. CI wraps the job at 25 minutes in `.github/workflows/ci.yml:91-106`.

### Current lifecycle ownership spellings

- `component/lifecycle.go:43-57` says the exact caller Stop context bounds terminal fence, cancellation, join, and
  cleanup.
- `openspec/changes/align-standard-lifecycle-tests/specs/component-lifecycle/spec.md:3-45` repeats the finite,
  caller-bounded Stop contract.
- `openspec/specs/runtime-context-ownership/spec.md:5-58` requires the exact caller context and forbids invented
  defaults at production lifecycle boundaries.
- `processor/rule/processor.go:1207-1256` forwards Stop's caller context into cleanup.
- `processor/rule/processor.go:1260-1363` uses that context for command-fence settlement, subscription drains, native
  consumer closure, watcher records, entity-borrow settlement, cron completion, and runtime joins.
- The observed entity-borrow call is `processor/rule/processor.go:1329-1331`.
- `awaitEntityBorrowSettlement` at `processor/rule/processor.go:1404-1423` selects exact completion versus caller
  cancellation, cancels the Start authority if the Stop context wins, and returns the context error.
- Borrow admission, release, and fencing are owned by `processor/rule/entity_watcher.go:644-681`.

There is an adjacent qualification. `settleRuntimeCommandFence` at `processor/rule/processor.go:1426-1445` cancels on
Stop-context completion and then synchronously joins the barrier/coordinator without a second select. The observed
entity-borrow wait is context-responsive. The repository has not established that every Rule cleanup phase always
returns at the exact caller deadline.

Existing bounded test patterns are local:

- `pkg/dispatch/keyed_pool_test.go:17-23` defines a package-local bounded Stop context.
- `service/component_manager_lifecycle_test.go:11-19` creates a finite detached Cleanup Stop context.
- The reviewed #1059 work uses the same pattern in `service/startup_observability_test.go`.
- `component/lifecycle_test_suite.go:63-230` uses finite five-second Stop contexts for ordinary shared lifecycle cases.
- Its older error-injection helper still directly calls Background Stop at lines 436 and 446 outside defer/Cleanup.

No shared cleanup helper or guard exists:

- `test/contract/context_ownership_contract_test.go:19-93` checks production-struct context retention, not test call
  expressions.
- `scripts/lint-test-ports.sh:1-55` guards fixed test ports, not Stop roots.
- Searches across `scripts`, `test/contract`, `.github`, and `Taskfile.yml` found no Background-Stop cleanup guard.

### Provenance and adjacent ownership

`git show 61fbd48f -- processor/rule/readiness_integration_test.go` shows both exact mechanical changes:

```go
processor.Stop(5 * time.Second)
```

became:

```go
processor.Stop(context.Background())
```

No other lines in that file changed in the commit.

The redundant `defer tc.Terminate()` calls predate that context migration. They became more damaging when the migration
removed the processor cleanup's five-second bound. The teardown-order defect and unbounded Stop now combine at the same
two tests.

The runtime-context specification governs production lifecycle ownership. Its archived proposal explicitly excluded
repo-wide production Background/TODO/WithoutCancel removal. Test-cleanup policy was unspecified and outside that
production scope; the silence did not authorize an unbounded test cleanup root.

The active standard-lifecycle work requires caller-bounded Stop but owns portable lifecycle proof, not the entire
test-cleanup population. #736 owns Docker/package contention and does not authorize an unbounded cleanup. Its evidence
also falsifies global `-p1` as a general fix. #220 established a lint precedent for test-substrate defects but did not
land a cleanup-context guard.

### Observed reproduction

The 2026-08-23 canonical tagged integration run showed:

```text
TestIntegration_RuleReadiness_NonEmptyReplayReportsScope
  t.Cleanup
    -> Processor.Stop(context.Background())
      -> Processor.cleanup
        -> awaitEntityBorrowSettlement
```

After roughly 12 minutes without progress, `SIGQUIT` showed the cleanup goroutine at the entity-borrow settlement seam.
The package had consumed 824.035 seconds when the diagnostic signal ended it. The exact test then passed in isolation
under `-race -tags=integration` in 2.144 seconds.

This proves the early-NATS teardown and unbounded cleanup defects and a non-settling admitted borrow after its substrate
was terminated. The dump did not retain enough of the owning goroutine to prove that production work ignores both Start
cancellation and a finite Stop context when lifecycle ownership is ordered correctly.

### Population inventory

Exact textual searches produced two snapshot-bound censuses:

```text
Snapshot                                      all calls  cleanup sites  files  integration  unit
35a64ee runtime baseline                            518            296     91          165   131
post-#1059 reviewed working tree                    517            295     90          165   130
```

High concentrations include graph-gateway query (28), graph-query component (18), graph-query attack (11),
graph-ingest hierarchy synchronization (10), graph-clustering integration (10), and agentic-tools integration (8).

These are syntax censuses only. They do not distinguish deliberate API-contract calls from cleanup roots, and they
miss multiline calls, aliases, helpers, and contexts derived from Background. The one-site delta is
`service/startup_observability_test.go:206` at `35a64ee`, corrected by reviewed #1059. The two Rule readiness Background
Stop sites are not the complete narrow reproduction surface by themselves: their two redundant TestClient termination
defers also own the failure. The complete narrow #1062 surface is those four lines. The remaining 295-site current-tree
Background Stop classification is an audit-sized adjacent surface.

### Test-author seam

A SemStreams test author currently must know:

- `testing.T.Context()` is canceled before Cleanup;
- Cleanup Stop needs a fresh detached but finite context;
- its cancel function must be called;
- Stop errors must not be discarded;
- Start-parent cancellation and Stop authority are separate;
- test-owned substrates must outlive the components that use them;
- a helper's registered Cleanup should not be duplicated with an earlier function defer.

If the author does nothing, a stuck owner can park a package until the global process timeout and obscure attribution.
This is documented across Go and lifecycle sources but is not compile-, lint-, or runtime-enforced. Ideally a test
author should not need to reconstruct cleanup ordering and context ownership; the test seam should produce a prompt,
owner-attributed failure automatically.

### Inventory questions

- Preserve the observed function identity, not stale line numbers from the captured dump.
- A population remediation needs AST/type-aware classification: which Stop method is called, whether it is a
  Cleanup/defer root, and whether a helper or derived context already provides a bound.
- A deterministic production proof would need to identify and hold the exact entity borrow, keep NATS alive through
  ordered processor Stop, then verify both Start cancellation and finite Stop behavior. The observed misordered teardown
  does not justify a production change.

## #1063: tool concurrency timing assertion

### Claimed gap

`TestIntegration_ToolConcurrentExecution` at `processor/agentic-tools/tools_integration_test.go:544-670` classifies
concurrency using elapsed wall time.

- Three fake executors each delay 200 milliseconds at lines 581-595.
- Three calls publish at lines 640-648.
- Results poll within two seconds at lines 650-656.
- Elapsed time must be less than 800 milliseconds at lines 658-669.
- The shared fake implements delay with `time.After(m.delay)` at lines 74-97 and has no entry/release/completion
  synchronization.
- Startup and subscription readiness use sleeps at lines 617 and 635.
- Cleanup at line 615 is another unbounded Background Stop site in the #1062 population, but it is not the concurrency
  oracle.

The test can accept roughly 600 milliseconds of serial execution and reject correct work when host overhead pushes a
parallel implementation over 800 milliseconds.

### Current runtime concurrency spellings

- `processor/agentic-tools/component.go:380-395` sets `MaxAckPending: 3`.
- `openspec/specs/jetstream-consumer-policy/spec.md:6-35` defines that value as server acknowledgement admission. It
  does not promise three simultaneous local executor calls.
- `processor/agentic-tools/component.go:397-430` creates one native consumer and one handler through
  `ConsumeStreamWithConfig`.
- `natsclient/stream.go:437-480` supplies one callback to the native consumer and invokes the SemStreams handler inline.
- The pinned dependency is `nats.go` v1.52.0 (`go.mod:12`). Its asynchronous subscription owns one delivery goroutine
  per subscription and invokes the callback inline before advancing (`nats.go:3569-3632,4931-4955`). Its JetStream
  pull consumer also invokes the user handler inline (`jetstream/pull.go:243-300`).
- `natsclient/heartbeat.go:60-143` starts the work callback in a goroutine but waits until that one work callback returns
  or cancellation/heartbeat settlement joins it. It does not return immediately for another delivery.
- `processor/agentic-tools/executor.go:22-26,203-232` makes registry lookup thread-safe, releases the read lock, and then
  invokes the executor directly.
- `processor/agentic-tools/executor_test.go:664-695` launches ten direct registry callers concurrently but asserts only
  completion and race freedom, not overlap.

The current wire delivery path is therefore serial. Direct registry callers can still invoke executors concurrently.

Concurrency is claimed in multiple outward documentation surfaces:

- `processor/agentic-tools/README.md:7,21-27,314-318` claims concurrent execution.
- `processor/agentic-tools/doc.go:8,268-280,295-301` says each tool call gets its own goroutine and multiple calls can
  execute concurrently.
- `docs/concepts/13-agentic-systems.md:291-315` presents parallel tool execution as current behavior.
- `docs/advanced/11-jetstream-tuning.md:55-56` correctly separates server admission from local execution, but lines
  92-102 map `MaxAckPending` to tool concurrency and lines 226-240 say value 3 permits parallel tools.

The current agentic-tools OpenSpec has no parallel or concurrent execution requirement. These documentation/test claims
collide with the wire-path mechanics and the absence of a capability contract.

The isolated 10-run evidence completed in 6.438 seconds total, approximately 0.64 seconds per run. That matches three
200-millisecond serial fake calls plus small overhead, not a 200-millisecond parallel path. The canonical tagged suite
failed at 811.816917 milliseconds under contention.

### Adjacent ownership

- #220 explicitly named this exact timing test as an unfiled Subclass 3 item and required replaceable timing assertions
  to become explicit synchronization.
- #736 explains added contention but cannot repair the logical false-positive.
- `docs/proposals/gh963-max-ack-pending-inventory.md:170-195` already records that MaxAckPending owns server delivery
  admission while local concurrency is separate, and calls out stale documentation equating them.
- `test/testinfra/policy_baseline.json:715-722` ratchets both sleeps in the exact test as migration debt.
  `test/testinfra/policy_guard_test.go:561-597` rejects stale or newly unrecorded baseline entries, so removing either
  sleep must update the guard baseline in the same change.
- `openspec/specs/agentic-loop/spec.md:294-310` conditionally defines ordering when tool results complete concurrently.
  That is adjacent result-ordering truth, not a promise that this wire consumer executes calls concurrently.
- Current agentic-tools truth governs catalog, discovery, admission, timeout, durable outcomes, and component-owned
  MaxAckPending. It does not govern local parallel execution.

### Same-class collision inventory

The semantic class is simultaneous local execution of independently delivered tool calls versus server outstanding-ACK
admission.

| Surface | Current owner |
|---|---|
| Delivery admission | NATS `MaxAckPending` policy |
| Local callback execution | one subscription delivery goroutine |
| Heartbeat work | one joined callback per delivery |
| Direct executor calls | registry callers; lookup is thread-safe |
| Result/outcome writes | agentic-tools handler and durable outcome ledger |
| Lifecycle | one native consumer handle drains and joins callbacks |
| Status | pending/ACK policy and tool request/error/timeout/outcome metrics |
| Recovery | durable COMPLETED ledger and ACK-after-result behavior |

Test synchronization debt is separately cataloged by `test/testinfra/policy_baseline.json` and enforced by the policy
guard. Conditional agentic-loop source-order handling describes how concurrent completions would sort if a caller
produced them; it does not assign concurrency ownership to agentic-tools.

There is no local executor-pool declaration or in-flight concurrency status. Adding local concurrency would alter effect
overlap, completion ordering, rate pressure, shutdown joins, MaxAckPending interaction, executor safety expectations,
and ambiguous-effect recovery. It is not a test-only correction.

### Executor-author seam

An external tool executor author currently sees README claims that wire calls execute in parallel, while the single
wire consumer serializes them. Direct registry callers may still overlap. The author has no specification or runtime
status that establishes which contract to rely on.

If nothing changes, executor code may be designed for a concurrency promise the wire runtime does not satisfy, and the
test may pass serial runtime or fail correct work under load. The framework should state and causally prove one
execution contract; authors should not infer local overlap from MaxAckPending or elapsed duration.

### Blocking inventory collision

A causal all-three-enter barrier would block or fail on the current serialized wire path. Before such a proof can be
designed, the owner must choose whether desired truth is:

1. current serialized wire execution, with stale README/test correction; or
2. a new production local-concurrency contract with separate design for ordering, lifecycle, bounds, recovery, and
   executor safety.

Inventory does not choose between those target states.

## #1062 post-design lifecycle-lane evidence addendum

This addendum records evidence discovered while implementing the accepted narrow #1062 test change. It does not
choose target state or authorize a production correction.

### Remaining teardown-order collision

Removing the two redundant `TestClient.Terminate` defers and replacing unbounded Background Stop calls with finite,
asserted Stop calls removed the original substrate-order collision. Both readiness tests still execute a second,
different lifecycle order:

- `processor/rule/readiness_integration_test.go:63-64` and `:110-111` derive the accepted Start parent from
  `context.Background()` and defer its cancellation.
- `processor/rule/readiness_integration_test.go:80-90` and `:138-148` register bounded Processor Stop through
  `t.Cleanup` after Start succeeds.
- Function defers run when the test function returns, before the testing package invokes Cleanup callbacks.
- `testing.T.Context` cancellation does not own these independently created Background children.

The resulting order is test body completion, accepted Start-parent cancellation, Processor Cleanup with live NATS,
then `TestClient` Cleanup. The finite Stop context bounds terminal work but cannot restore callback authority that the
accepted Start parent already ended.

The original twelve-minute observation additionally terminated NATS before Processor Stop and removed the Stop bound.
It therefore proves a test-owned controlled-lane teardown defect. It does not prove that the controlled production
ordering is defective.

### Current Rule owner surface

- `processor/rule/processor.go:999-1029` derives `runCtx` from the accepted Start parent and retains only its private
  cancel function.
- Entity watches, subscriptions, status publication, hot reload, the revision sweeper, and the runtime coordinator
  derive work from that runtime authority at `processor/rule/processor.go:875-885,938-973`.
- The pinned NATS KV Watch binds native cancellation to its context at `nats.go/jetstream/kv.go:1304-1319`.
- Rule cleanup fences and drains native inputs while callback authority remains live at
  `processor/rule/processor.go:1289-1300`.
- Main entity-watcher cleanup treats `nats.ErrBadSubscription` as orderly prior closure at
  `processor/rule/processor.go:1302-1307`.
- Hot-reload cleanup returns raw `watcher.Stop()` errors at `processor/rule/kv_config_integration.go:220-240` after
  parent cancellation may already have closed the native watcher.
- Joined Rule Stop output does not preserve enough phase attribution to identify which owner returned each native
  error.

The portable lifecycle floor distinguishes two scenarios:

| Lane | Start authority at Stop entry | Portable observation |
|---|---|---|
| Controlled shutdown | Live | Terminal fence/drain may use live callback authority before cancellation |
| Accepted-parent cancellation | Ended | Continuing work exits and a separately bounded Stop observes completion |

The shared cases are `component/lifecycle_test_suite.go:63-84` and the active component-lifecycle delta. The standard
suite is instantiated only for UDP, gateway/http, and graph-index; Rule does not currently run it.

The readiness tests assert controlled runtime readiness behavior but currently execute the accepted-parent-cancellation
lane during cleanup.

### Empirical evidence after removing early TestClient termination

- The real NATS substrate remained live before every Processor Stop attempt.
- The unchanged `awaitEntityBorrowSettlement` white-box proof passed under `-race -count=100` in 1.508 seconds.
- The focused readiness integration package failed 20 of 20 cleanup attempts in 16.6 seconds after accepted-parent
  cancellation.
- Most Stop failures reported `nats: invalid subscription`; one reported `nats: consumer not found`.
- Every failure returned promptly under the finite Stop bound; none reproduced the original wedge.

This is evidence of a Rule accepted-parent-cancellation cleanup defect relative to the current portable floor. The
exact failing native owner remains unclassified because Rule cleanup aggregates errors without phase attribution.
The evidence does not justify changing the controlled Start/Stop model or extending the terminal timeout.

### Same-class authority inventory

| Authority or resource | Current owner and observed interaction |
|---|---|
| Start/work authority | Test context; Processor derives `runCtx`; KV watchers bind native cancellation to it |
| Terminal authority | Fresh finite Stop context; bounds cleanup but does not restore ended callback authority |
| Substrate authority | `NewTestClient` Cleanup; now later than Processor Cleanup |
| Native watcher teardown | Parent cancellation and explicit exact-handle Stop can both close a watcher |
| Error interpretation | Main watcher normalizes prior closure; hot reload returns raw Stop error; Rule joins phases |
| Recovery | Processor is one-shot; terminal Stop does not provide running-generation replay or rejoin |

### Test-author seam

A lifecycle test author must currently coordinate three LIFO relationships: Processor Stop before Start-parent
cancellation, and both before TestClient termination, when the test intends controlled shutdown. If it instead intends
accepted-parent cancellation, it must name that lane and assert its weaker completion contract rather than controlled
drain behavior.

If the author does nothing, a controlled-lane test can silently execute the abort lane and return low-level native
closure errors. The current distinction is distributed across lifecycle GoDoc, the active capability delta, ADR-095,
and owner-local tests. No external or exported product surface is introduced by recording this seam.

### Open evidence questions

1. Which exact Rule watcher or cleanup phase produces `invalid subscription` and `consumer not found`?
2. Does an isolated real-NATS Rule test of accepted-parent cancellation followed by bounded Stop reproduce the same
   failure without readiness assertions or test-substrate termination?
3. Is the correction limited to already-closed native error normalization, or does one Rule owner fail to join work
   after its accepted parent ends?
