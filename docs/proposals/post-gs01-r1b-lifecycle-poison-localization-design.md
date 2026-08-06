# Post-GS-01 R1b lifecycle poison-localization design

## Checkpoint identity and status

- Repository baseline: `dd02a715ac055b8f5ea8bf8cd9391740537ff6d9`
- Reviewed inventory: `docs/proposals/post-gs01-r1b-implementation-inventory.md`
- Inventory SHA-256: `7330438a1da038d29da3b8615c835f5392c22e46094ee44016fe16fe9285e77a`
- Inventory review: `INVENTORY PASS`
- Accepted execution boundary:
  `docs/proposals/post-gs01-r1-decomposed-execution-design.md:217-260`

R1b requires one bounded current-target amendment: the active GS-01 `framework-composition` delta must stop requiring
the lifecycle graph-state guard, and checked task 7.3 must mark its guard-preservation clause superseded. This corrects
current active truth to the already accepted R1b runtime outcome; it does not add a durable or communication primitive,
public API, configuration, status, metric, or framework concept. The accepted GS-01 design, approval, reviews, and
implementation evidence remain byte-identical historical evidence.

Owner rulings:

- a malformed matching watch entry warns once per affected subscription;
- that warning carries `workflow`, `entity`, `revision`, `code`, and `reason` because the watch observes all five;
- no public terminal-error channel, status, metric, configuration, or export is added;
- asynchronous value-channel and WebSocket closure remain the outward terminal behavior;
- ADR-079 and ADR-081 remain byte-identical; ADR-092 narrowly supersedes only ADR-081's lifecycle-guard ruling.

## Measured premises

| Premise | Evidence | Consequence |
|---|---|---|
| Global coordination is redundant validation. | `pkg/lifecycle/manager.go:69-105,247-294`; `pkg/lifecycle/manager_query.go:208-445` | Delete it without replacement. |
| Exact reads validate the requested entity. | `graph/exact_entity.go:15-93`; `pkg/lifecycle/manager.go:247-269` | Return the existing classified outcome for that operation only. |
| Production RPC preserves class/code but reconstructs the concrete cause. | `natsclient/errors.go:221-233,379-398` | Delete concrete-type-dependent latching and test production-faithful classification. |
| List already filters by workflow before exact read. | `pkg/lifecycle/manager_query.go:25-73` | Remove the global precheck; preserve whole-call failure/no partial result for matching poison. |
| Watches already use workflow patterns and validating decode. | `pkg/lifecycle/manager_query.go:208-233,448-479` | Retain one pattern watcher per subscription; delete `WatchAll`. |
| Transport close and cancellation already terminate at a subscription loop. | `pkg/lifecycle/manager_query.go:385-445` | Keep closure local; cancellation stays quiet. |
| Lifecycle owns no cached derived view. | Reviewed inventory collision table | Derived-owner sticky whole-view policy does not apply. |
| No public terminal error/status/metric/config exists. | Reviewed inventory closure searches | Add none. |

## Options

### A. Do nothing

Keeps the Manager latch, validation-only `WatchAll`, readiness/revision barriers, and transport-degradation latch.
Rejected: contradicts the current graph-state contract, couples unrelated workflows, duplicates decode, and retains
restart-only recovery after a guard-observed poison.

### B. Replace the global latch with workflow/entity poison maps

Rejected: adds synchronization, cleanup, and repair prediction; duplicates validation already performed at exact and
pattern reads; and creates operational semantics with no present consumer.

### C. Move lifecycle onto `pkg/graphview`

Rejected: lifecycle does not own a cached materialized view. This would add full-projection memory, readiness, and
coalescing semantics and change workflow-pattern delivery.

### D. Delete global coordination and use existing scoped reads

Recommended:

- exact operations validate only the requested entity;
- List filters to the workflow before exact decode;
- each Watch/WatchEvents call validates only entries delivered by its workflow pattern;
- matching poison closes that subscription before projection/callback;
- unrelated operations and subscriptions continue.

This is the only option that removes code and adopter knowledge without adding another owner.

## Exact runtime contract

### Exact operations

`getEntity` performs no Manager-global precheck and creates no latch.

- not-found retains `ErrEntityNotFound` mapping;
- poison/unavailable/deadline/internal outcomes retain their existing classification and causal wrapping;
- poison returns no entity, participant, history, relationship result, or mutation request;
- later reads observe repaired current authority;
- a failure touching A does not alter a later operation touching B.

This applies transitively to exact/list/history/reference and mutation precondition paths using `getEntity`; their
operation-specific retry and partial-result rules otherwise remain unchanged.

### List

`List(workflow)` resolves the workflow, lists keys, rejects nonmatching keys with the registered pattern, and then
exact-reads/projects matching keys. A poisoned matching entity fails the whole call with no partial slice. Poison
outside the workflow is never decoded.

### Watch and WatchEvents

Each successful call owns exactly one `bucket.Watch(ctx, workflow.EntityIDPattern)` watcher. For each non-tombstone
entry:

1. validating-decode the stored entity state;
2. on typed poison, invoke no projection/callback;
3. warn once for that subscription;
4. stop the watcher and close only that call's returned channel.

The warning is level WARN, message
`lifecycle workflow watch encountered poisoned graph state; closing subscription`, with fields:

```text
workflow=<registered workflow>
entity=<entry.Key()>
revision=<entry.Revision()>
code=graph_state_reset_required
reason=<canonical graph.StateResetReason>
```

There is no Manager-lifetime log deduplication; immediate subscription termination makes the warning naturally once.
Tombstones remain unchanged. Projection errors after a valid authority decode retain their existing skip-and-warn
behavior.

Unexpected pattern-watcher transport close warns once with existing `index_not_ready` classification, closes only
that subscription, and does not block a later subscription. Context cancellation is quiet and local.

The public API remains `Watch(ctx, workflow) (<-chan Participant, error)` and
`WatchEvents(ctx, workflow) (<-chan Event, error)`. Open failures remain synchronous. After successful open, channel
closure remains the sole terminal signal; gateway WebSockets retain close-on-channel-close behavior.

## Explicit non-additions

R1b adds no bucket, stream, subject, payload, service, coordinator, supervisor, retry helper, poison cache, repair
watcher, status key, health state, metric, debug endpoint, log-dedup registry, terminal-error lane, exported symbol,
configuration, schema/OpenAPI field, gateway mapping, or `pkg/graphview` dependency.

Decision skills are not triggered: there is no new communication path (`kv-or-stream`), orchestration
(`orchestration-check`), payload (`new-payload`), or query access (`query-pattern`).

## Current-truth artifacts

### Lifecycle OpenSpec

Add requirements that:

1. authority poison is scoped to the operation observing it; exact poison produces no partial projection/mutation,
   affects no later B operation, and repaired A is evaluated on the next real read;
2. List filters by workflow pattern before decode; nonmatching poison is irrelevant and matching poison fails the
   whole call without a partial slice;
3. Watch poison is subscription-local; matching poison emits no value/callback/mutation, warns once with the five
   fields, and closes only that subscription; transport loss is local and cancellation quiet;
4. asynchronous termination retains the existing value-channel contract, with no terminal-error/status/metric/config
   addition.

No graph-state-contract amendment is required.

### ADR-092

Add `docs/adr/092-lifecycle-poison-localization.md` recording:

- lifecycle is an authoritative scoped reader, not a cached derived-view owner;
- delete the Manager latch, full-authority `WatchAll`, barriers, degradation latch, and guard lifecycle;
- validation rides exact and workflow-pattern reads;
- exact/List/watch locality and diagnostics follow this design;
- no replacement coordinator/status/API is admitted;
- ADR-092 narrowly supersedes ADR-081's lifecycle validation-guard classification only; ADR-079 remains accepted.

### Package documentation

Update `pkg/lifecycle/doc.go` to describe exact/List/watch locality, no partial output/mutation, subscription-local
warning/closure, quiet cancellation, and the unchanged terminal channel contract. Replace the stale statement that
the Manager handles KV storage with exact-read/canonical-mutation wording.

### Active GS-01 disposition

Amend the active current-target surfaces in place:

1. In `openspec/changes/establish-graph-read-write-foundation/specs/framework-composition/spec.md`, remove the
   lifecycle graph-state guard from the requirements that both composition roots wire and keep. Preserve the graph
   mutation protocol, exact-read adapter, local projection dependencies, catalog cleanliness check, generic
   graph-bucket write protection, and sister-binary parity requirement.
2. In `openspec/changes/establish-graph-read-write-foundation/tasks.md`, retain checked task 7.3 as completion history
   but annotate its graph-state-guard clause as superseded by R1b/ADR-092. Its catalog-cleanliness clause remains
   current.

Preserve the accepted GS-01 `design.md`, approval, reviews, inventory, and implementation-evidence bytes as historical
evidence. Add
`openspec/changes/establish-graph-read-write-foundation/successor-dispositions/r1b-lifecycle-guard.md` recording the
exact current-target amendments above and stating that guard preservation was truthful for the GS-01 landing but is
not current framework behavior after R1b.

### Lifecycle E2E

Extend the existing production-wire lifecycle scenario, not a parallel stack:

1. connect a test-only NATS validation client to the lifecycle tier;
2. open the mission WebSocket and consume valid bootstrap, which deterministically proves the current full-authority
   guard has started and completed initial replay before fault injection;
3. directly inject malformed bytes under valid nonmatching key `c360.test.other.gcs.device.poison-a` in
   `ENTITY_STATES` as explicit fault injection;
4. wait for direct authority readback of the exact injected bytes/revision so the fault write itself is proven, then
   issue an operator patch to B and require the already-open B WebSocket to deliver B's later KV revision. On the
   baseline, the ordered full-authority guard must consume A's earlier poison revision before its revision barrier can
   release B's later update, so it closes/blocks B deterministically; the target has no guard/barrier and delivers B;
5. after that synchronization, assert valid mission B list, exact GET, and new WebSocket bootstrap also succeed;
6. retain transition/history stages and delete the injected key during teardown.

Matching-poison closure and warning fields remain deterministic unit/race tests; E2E proves nonmatching locality
through production gateways. Production request/reply classification is covered separately: an exact read of poisoned
A must preserve fatal `graph_state_reset_required` classification and causal detail even though RPC reconstruction does
not preserve the concrete `*graph.StateContractError` type. That characterization is not used as the baseline global-
latch trigger. No NATS CLI is introduced.

## Behavior-first tasks and conformance

| Task | RED proof | Implementation | Green proof |
|---|---|---|---|
| Baseline global-blast locality | start a matching A subscription, release current guard bootstrap, deliver poisoned A through the controlled pattern watcher, wait for A's channel to close, then require exact/list/mutation-precondition work on valid B to succeed; baseline deterministically latches from the matching watch and blocks B | remove global precheck/latch; matching poison terminates A subscription only | focused race with channel-based synchronization |
| Exact production classification | production-faithful request/reply adapter returns poisoned A as fatal `graph_state_reset_required` with causal detail and zero mutation; do not use this reconstructed concrete error to trigger the baseline latch | preserve classified wrapping while deleting concrete-type-dependent latching | focused unit/contract characterization |
| Exact repair locality | after a scoped poisoned exact read of A, repair A and prove a later real read observes it while valid B remains usable | retain no poison state between calls | focused race |
| List locality | nonmatching poison not decoded; matching poison returns no partial slice | preserve filter-before-exact, remove global precheck | focused race |
| Independent subscriptions | poison closes A while B continues | delete `WatchAll` and all guard/barrier state | focused race with explicit sync |
| No poisoned output | both Watch variants emit no value/callback | decode before projection/callback and return on poison | focused race |
| One diagnostic | assert message and five fields exactly once | local warning before loop return, no dedup state | structured-log test |
| Transport/cancel | later subscription works after transport close; cancellation quiet | no Manager degradation; local branches only | focused race |
| Capability narrowing | method set is exactly ListKeys/Watch | delete lifecycle WatchAll capability | compile/method-set test |
| Current truth | active framework-composition still requires the guard and task 7.3 still presents preservation as current | amend active framework-composition and task 7.3 truth; add lifecycle OpenSpec, ADR-092, package docs, and successor disposition while preserving historical design/review evidence | strict docs/spec validation |
| Production-wire locality | bootstrap B WebSocket, inject/read back nonmatching poison A, patch B, and require B's later-revision WebSocket update before list/exact/new-WS checks; the baseline revision barrier cannot release B without first observing A and therefore blocks or closes B | extend lifecycle scenario | `task e2e:lifecycle` |

## Deletion proof

Current lifecycle production and tests must have zero occurrences of:

```text
graphStateGuard
graphStatePoison
graphStateProgress
latchGraphStatePoison
graphStateContractError
ensureGraphStateGuard
runGraphStateGuard
advanceGraphStateGuardRevision
waitGraphStateGuardRevision
markGraphStateGuardDegraded
publishGraphStateGuardReady
waitGraphStateGuard
markLifecycleGuardClean
```

Production lifecycle must have zero `WatchAll`. The package-local reader method set must be exactly `ListKeys` and
`Watch`. `fakeBucket.WatchAll` may remain only if unrelated broad `jetstream.KeyValue` fixture conformance still
requires it; no lifecycle guard test may call it.

Record pre-change hashes of ADR-079 and ADR-081 and prove them unchanged after implementation.

## Verification

```bash
go test -race ./pkg/lifecycle
go test ./test/contract/...
task check:push
task e2e:lifecycle
```

No unrelated E2E tier is required.

## Adopter result

The same exact, List, Watch, and WatchEvents APIs remain. Adopters gain no configuration or recovery procedure. They
need know only that exact/List poison belongs to the requested scope, asynchronous watches terminate by channel
closure, and canceling the context stops their subscription. The hidden Manager-wide authority guard disappears.
