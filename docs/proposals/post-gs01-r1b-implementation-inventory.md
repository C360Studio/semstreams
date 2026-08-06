# Post-GS-01 R1b lifecycle poison-localization inventory

## Checkpoint identity

- Repository baseline: `dd02a715ac055b8f5ea8bf8cd9391740537ff6d9`
- Branch: `codex/r1b-lifecycle-poison-localization`
- Phase: inventory only; no target-state amendment or implementation
- Accepted execution boundary:
  `docs/proposals/post-gs01-r1-decomposed-execution-design.md:217-260`

## Claimed gap and measured current state

R1b concerns an existing in-process coordination primitive, not a new durable or communication primitive.

- `Manager` owns a Manager-wide sticky poison latch plus a shared `ENTITY_STATES` `WatchAll` guard, readiness
  barrier, revision barrier, transport-failure latch, and lifecycle goroutine state in
  `pkg/lifecycle/manager.go:69-105`.
- `NewManager` creates the guard context and channels before a lifecycle watch starts in
  `pkg/lifecycle/manager.go:115-140`.
- Exact reads first check the global latch, then attempt to latch a concrete `*graph.StateContractError` in
  `pkg/lifecycle/manager.go:247-294`. The production request/reply path reconstructs the classified failure with an
  `errors.New` cause in `natsclient/errors.go:221-233,379-398`, so it preserves fatal class/code but does not satisfy
  that concrete-type assertion. The production exact-RPC error path therefore does not itself latch the Manager;
  however, a separately running full-authority guard or matching pattern watch can observe the same stored poison
  and latch it globally. The direct-KV test adapter preserves the concrete type and makes its exact-read unit path
  latch Manager-wide without that concurrent observation.
- `List` checks the global latch before workflow filtering in `pkg/lifecycle/manager_query.go:25-73`.
- A lifecycle subscription starts the shared guard, opens `WatchAll`, and waits on the guard's global
  readiness/revision state in `pkg/lifecycle/manager_query.go:208-445`.
- Pattern-watch entry decoding can itself latch Manager-wide poison in
  `pkg/lifecycle/manager_query.go:448-479`.
- Existing tests pin the nonlocal behavior: repaired/deleted poison remains Manager-wide sticky in
  `pkg/lifecycle/graph_state_contract_test.go:14-38`; unrelated poison blocks a valid workflow subscription in
  `pkg/lifecycle/watch_atomic_bootstrap_test.go:372-403`.

The accepted R1b execution artifact records the deletion boundary as all `graphStateGuard*`,
`graphStatePoison*`, and associated global coordination, with exact reads limited to the touched entity, `List`
scope-first, and watch failure limited to the matching subscription in
`docs/proposals/post-gs01-r1-decomposed-execution-design.md:217-260`. The accepted roadmap records the same atomic
outcome in `docs/proposals/post-gs01-r1-roadmap-amendment.md:78-91`.

## Every current spelling on the deletion surface

Production fields/types in `pkg/lifecycle/manager.go:69-105`:

- `graphStatePoison`
- `graphStatePoisonLatch`
- `graphStateGuardMu`
- `graphStateGuardStarted`
- `graphStateGuardCtx`
- `graphStateGuardCancel`
- `graphStateGuardReady`
- `graphStateGuardDone`
- `graphStateGuardReadyOnce`
- `graphStateGuardDoneOnce`
- `graphStateGuardResult`
- `graphStateGuardDegraded`
- `graphStateGuardRevision`
- `graphStateProgressMu`
- `graphStateProgress`
- `graphStateGuardWG`
- `graphStateGuardTransportFailure`

Production methods/functions:

- `latchGraphStatePoison` and `graphStateContractError` — `pkg/lifecycle/manager.go:272-294`
- `ensureGraphStateGuard` and `runGraphStateGuard` — `pkg/lifecycle/manager_query.go:234-293`
- `advanceGraphStateGuardRevision` — `pkg/lifecycle/manager_query.go:295-310`
- `waitGraphStateGuardRevision` — `pkg/lifecycle/manager_query.go:312-339`
- `graphStateGuardNotReady`, `markGraphStateGuardDegraded`, `publishGraphStateGuardReady`, and
  `waitGraphStateGuard` — `pkg/lifecycle/manager_query.go:341-383`

Reader and test coupling:

- `entityStatesReader.WatchAll` — `pkg/lifecycle/manager.go:31-35`, pinned by
  `pkg/lifecycle/reader_capabilities_test.go:9-29`.
- Global sticky exact-read assertions — `pkg/lifecycle/graph_state_contract_test.go:14-55`.
- Atomic all-authority bootstrap, global poison gate, revision barrier, transport state, single-guard,
  concurrent-guard, unrelated-workflow blocking, and guard-shutdown tests —
  `pkg/lifecycle/watch_atomic_bootstrap_test.go:31-478`.
- `markLifecycleGuardClean` — `pkg/lifecycle/watch_atomic_bootstrap_test.go:507-510`.
- Guard fixture fields — `pkg/lifecycle/manager_test_helper_test.go:39-55`.
- `fakeBucket.watchAllFactory` and its broad `WatchAll` method —
  `pkg/lifecycle/manager_test.go:130-137,254-258`. The fake still implements broad `jetstream.KeyValue` for other
  test paths, so R1b must distinguish deleting lifecycle guard expectations from retaining any fixture method needed
  for unrelated interface conformance.

No production shutdown consumer exists for `graphStateGuardCancel` or `graphStateGuardWG.Wait`; cancellation/join
occurs only in tests. `Manager` has no exported `Close` or `Stop`.

## Adjacent claims and owners

| Surface | Current owner and semantics | Collision/locality evidence |
|---|---|---|
| Typed authority-contract classification | `graph.StateContractError`, code `graph_state_reset_required`, reason and optional `EntityID` in `graph/state_contract.go:11-109` | Shared classification remains distinct from lifecycle's response policy. Callers with key context stamp identity in `graph/entity_predicate_contract.go:241-263`. |
| Exact authoritative read | `graph.ReadExactEntity` in `graph/exact_entity.go:15-93` | Reads and validates one requested entity. |
| Current graph-state contract | `openspec/specs/graph-state-contract/spec.md:5-75` | Validation rides existing reads; a dedicated validation-only watcher is prohibited. Authority poison is per entity. |
| Lifecycle current spec | `openspec/specs/lifecycle/spec.md:1-168` | Defines lifecycle reads, writes, transitions, deletion, watch behavior, and operator outcomes. It contains no lifecycle poison-locality, global-latch, or dedicated-guard requirement. |
| Lifecycle accepted policy ownership | `docs/proposals/post-gs01-r1-execution-rulings.md:17-45` | Lifecycle owns the touched entity/workflow response, not the whole graph; shared code/reason classification remains graph-owned. |
| Graph-ingest authority writer | `openspec/specs/graph-ingest/spec.md:513-625`; `processor/graph-ingest/poison_inventory.go:1-220` | Per-entity refusal and recovery; unrelated entities continue. It separately owns bounded poison observability/status. |
| Derived projection owners | `openspec/specs/graph-state-contract/spec.md:150-168` | Whole-view sticky reset behavior is reserved for components owning a cached derived view. |
| Graph-view shared reader | `openspec/specs/graph-view-subscription/spec.md:167-186,208-214`; `pkg/graphview/view.go:410-426,625-671` | Per-key poison; unrelated keys continue. Lifecycle does not use `pkg/graphview`. |
| Rule watcher | `openspec/specs/rule-entity-watching/spec.md:20-41` | Owns separate evaluation policy; R1b leaves it unchanged. |
| Lifecycle gateway | `gateway/lifecycle-gateway/component.go:189-210`; `gateway/lifecycle-gateway/handlers.go:143-570` | Existing HTTP and WebSocket consumers expose no lifecycle poison configuration/status. |
| Gated-DAG restart recovery | `processor/gated-dag/executor.go:116-139` | Directly consumes `Manager.Watch`; its pump ends when the value channel closes and has no separate terminal-error lane. |
| Rule and agent-run lifecycle access | Shared `Manager` distribution from `cmd/semstreams/main.go:162,217-238` and `cmd/e2e-semstreams/main.go:143,198-217` | Rule actions reach lifecycle through `LookupByEntityID`; agent-run uses the same Manager for exact state. Locality must be safe for co-resident consumers sharing one Manager. |
| Lifecycle E2E | `test/e2e/scenarios/lifecycle/scenario.go:133-377` | Covers list/get/patch/transitions/history and valid WebSocket flow; no malformed-authority locality case. |

## Coordination-primitive collision table

R1b adds no bucket, stream, subject, payload, registry entry, status catalog, or metric. The collision is the current
guard duplicating authority observation.

| Dimension | Current lifecycle guard | Existing authoritative/scoped surface |
|---|---|---|
| Semantic class | Validation-only whole-authority coordination | Exact authoritative reads and caller-scoped pattern watches |
| Owner | One `lifecycle.Manager` instance | `ENTITY_STATES` owner plus each scoped reader |
| Runtime primitive | Background `WatchAll`, channels, atomics, wait group | `Get`/exact read, `ListKeys`, and `Watch(pattern)` |
| Durable primitive | None | Existing `ENTITY_STATES` KV |
| Catalog/status | Private state only | Shared typed error vocabulary; graph-ingest separately owns graph status/inventory |
| Lifecycle | Constructed with `Manager`; started on first watch; no production close/join | Request or subscription context |
| Readers affected | Every exact read, list, write precondition, and workflow watch through the same Manager | Requested entity, selected workflow, or matching subscription |
| Writers affected | Create, transition, operator update, complete, despawn, and query helpers through `getEntity` | The operation touching the poisoned entity |
| Recovery | Manager restart after the guard or a pattern watch has set the Manager latch; transport failure also remains latched | Authority-reader recovery is scoped by reader class; per-entity repair can restore exact reads when no Manager latch was independently set |
| Duplicate observation | Separate `WatchAll` beside every pattern watch | Current graph-state contract requires validation to ride existing reads |

## Consumer at birth and closure searches

There is no new exported symbol or infrastructure consumer in R1b. Existing observable consumers are external
component authors using `Manager`, the lifecycle gateway, gated-DAG restart recovery, rule actions, agent-run,
HTTP/WebSocket clients, and operators reading lifecycle logs. Current searches found:

- no poison/reset-required/guard spelling in `configs/lifecycle-flow.json`, `schemas/lifecycle-gateway.v1.json`,
  `gateway/lifecycle-gateway/openapi.go`, `pkg/lifecycle/doc.go`, `cmd/e2e-semstreams/mission`, or the lifecycle E2E;
- no `pkg/graphview` import or use in lifecycle production/gateway/E2E code;
- no production `graphStateGuardCancel` or `graphStateGuardWG.Wait` call;
- no lifecycle poison metric, `GRAPH_STATUS` entry, configuration field, subject, bucket, payload, or registry entry;
- no `docs/adr/092-lifecycle-poison-localization.md`;
- no lifecycle poison/corruption E2E scenario.

## Premise falsifications and owner rulings

- Current runtime and inverse-test premises are confirmed.
- The current graph-state contract already prohibits the lifecycle validation-only guard at
  `openspec/specs/graph-state-contract/spec.md:62-75`; R1b resolves an existing code/spec contradiction.
- `openspec/changes/establish-graph-read-write-foundation/design.md:472` preserves the guard, while the later
  accepted R1b artifacts and current graph-state spec supersede that mechanism. R1b records this disposition in
  successor ADR-092 without modifying historical evidence.
- The same stale guard-preservation claim remains in checked task 7.3 and the foundation framework-composition delta;
  R1b must update current active-change truth while preserving accepted historical artifacts.
- Lifecycle owns no cached materialized view; whole-view sticky derived-owner policy does not apply.
- “Warn once” is interpreted as once per affected subscription: the malformed matching entry closes that
  subscription immediately, so no Manager-lifetime log latch is needed.
- Exact operations know entity identity but not necessarily KV revision. The structured entity/revision warning is
  therefore a matching-watch diagnostic, where both are observed together.
- R1b does not add a public terminal-error channel. Asynchronous channel/socket closure remains the existing outward
  contract; local structured warning makes the reason operator-visible without widening the API.

## Adopter seam inventory

Specific adopter: a developer outside this repository implementing a component that uses `lifecycle.Manager`, or
consuming the lifecycle HTTP/WebSocket gateway, without reading lifecycle internals.

| Surface | What must they know today? | Do-nothing behavior | Discovery today | What should they know? |
|---|---|---|---|---|
| Exact lifecycle read/write | A prior full-authority-guard or pattern-watch poison can block later unrelated operations through the Manager latch; production exact RPC poison itself currently preserves fatal code but does not trip the concrete-type latch, while the direct test adapter does. | Co-resident healthy work can fail after guard/watch-observed poison; the exact RPC error path does not itself latch, but concurrent guard observation can still globalize the same stored poison. | Implementation/RPC reconstruction/tests only. | Only the outcome for the entity touched, with production-faithful tests. |
| `List(workflow)` | Global poison is checked before workflow narrowing. | A healthy workflow list can fail due to an unrelated entity. | Implementation only. | Requested workflow boundary and typed failure for a matching entity. |
| `Watch` / `WatchEvents` | A hidden `WatchAll` validates all authority entries. | An unrelated malformed entry can close a healthy subscription. | Implementation/tests only. | Their workflow pattern, cancellation context, and matching-entry failures. |
| HTTP gateway | Graph-state poison has no special mapping. | Sanitized 500, indistinguishable from other internal failures. | Generic handler behavior. | Stable resource-scoped outcome without guard knowledge. |
| WebSocket gateway | Startup error and asynchronous closure differ. | Later channel closure does not distinguish cancellation, transport loss, or matching poison. | Handler implementation only. | Subscription-local behavior and documented closure meaning; no hidden global coupling. |
| Recovery/operations | Current repair still leaves the Manager latch poisoned. | Healthy work stays unavailable after authority repair. | Tests only. | No restart of healthy lifecycle work because another entity was malformed. |
| Observability | Current lifecycle poison log omits entity/revision. | Operator cannot locate the offending record from lifecycle telemetry alone. | Log implementation only. | Scoped entity/revision/code/reason evidence, with no new status or metric concept. |
