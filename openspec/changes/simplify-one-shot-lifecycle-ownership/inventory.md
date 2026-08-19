# Lifecycle ownership inventory

Inventory target: merged `origin/main` at `63a733a2378dff9f09c74c461ba776d352f79221`.

Approved source: `/private/tmp/semstreams-idiomatic-lifecycle-ack-audit.md`, SHA-256
`2c9ced87dbd46a7213ab88cd89b35c8ae7fbf6e883d418cf6ada2215a5ebe398`.

Artifact verification before application:

```text
2c9ced87dbd46a7213ab88cd89b35c8ae7fbf6e883d418cf6ada2215a5ebe398  /private/tmp/semstreams-idiomatic-lifecycle-ack-audit.md
b6842dedd1b988d681c71fb9971604df9b27ba2c42d752489b4bc0e613094449  /private/tmp/semstreams-contract-supersession-handoff-v2.md
```

## Measured premises

```text
45  lifecyclejoin.NewGeneration
50  Generation.Stop
 8  executable Generation.StopWithQuiesce
10  executable Generation.Cancel outside lifecyclejoin
 1  executable Generation.Signal outside lifecyclejoin
 4  Operation.Run
21  RunPartialStartRollback call sites
42  production files importing internal/lifecyclejoin
```

Measurement searches:

```text
git grep -n 'lifecyclejoin.NewGeneration' 63a733a2378dff9f09c74c461ba776d352f79221 -- '*.go' ':!*_test.go'
git grep -n -E '(generation|Generation)\.Stop\(' 63a733a2378dff9f09c74c461ba776d352f79221 -- '*.go' ':!*_test.go'
git grep -n '\.StopWithQuiesce(' 63a733a2378dff9f09c74c461ba776d352f79221 -- '*.go' ':!*_test.go'
git grep -n -E '(generation|Generation)\.Cancel\(\)' 63a733a2378dff9f09c74c461ba776d352f79221 -- '*.go' ':!*_test.go'
git grep -n 'generation.Signal(' 63a733a2378dff9f09c74c461ba776d352f79221 -- '*.go' ':!*_test.go'
git grep -n -E '(shutdownOp|poolStop|stopOp)\.Run\(' 63a733a2378dff9f09c74c461ba776d352f79221 -- '*.go' ':!*_test.go'
git grep -n 'RunPartialStartRollback(' 63a733a2378dff9f09c74c461ba776d352f79221 -- '*.go' ':!*_test.go'
git grep -l 'internal/lifecyclejoin' 63a733a2378dff9f09c74c461ba776d352f79221 -- '*.go' ':!*_test.go'
```

The production search for goroutine-launched Stop returned no result:

```text
git grep -n -E 'go func.*Stop|go [^(]*\.Stop' 63a733a2378dff9f09c74c461ba776d352f79221 -- '*.go' ':!*_test.go'
=> empty
```

Concurrent, canceled-context rejoin, expired-deadline rejoin, and retained-result replay are manufactured by the shared
test suite at `component/lifecycle_test_suite.go:285-359`. Completed repeated Stop remains useful; no production caller
for concurrent Stop, later terminal rejoin, or retained error replay was measured.

## Complete 42-owner lifecycle census and classification

Primary classes are mutually exclusive. Facets record additional obligations.

- **S — simple cancel/done:** no context-bound native quiesce and no failed-Start helper.
- **F — failed-Start rollback:** bounded partial cleanup is a first-class path.
- **Q — quiesce:** native admission must stop while accepted-work authority remains live.
- **P — context-bound protocol:** a concrete native shutdown/pool protocol is currently wrapped in `Operation`.
- **M — manager/Start-finalization join:** Stop can race Start and must observe `startDone` before cleanup.

| # | Owner and evidence | Primary | Additional facet |
|---:|---|:---:|---|
| 1 | `agentic/agentrun/agentrun.go:643-647,707` | F | durable consumers |
| 2 | `examples/processors/document/component.go:211-217,313` | F | subscriptions |
| 3 | `examples/processors/iot_sensor/component.go:211-217,313` | F | subscriptions |
| 4 | `examples/processors/weather_station/component.go:176-182,229` | F | subscriptions |
| 5 | `gateway/graph-gateway/component.go:724-754` | Q | server shutdown and wait group |
| 6 | `input/file/file.go:404-409,429` | S | read-loop wait group |
| 7 | `input/http/http.go:273-300` | S | poll-loop wait group |
| 8 | `input/websocket/websocket_input.go:539-573,667-751` | Q | server/client admission |
| 9 | `output/file/file.go:265-267,424` | S | flush-loop wait group |
| 10 | `output/httppost/httppost.go:268,421` | S | subscriptions |
| 11 | `output/otel/component.go:203-204,507-509` | P | exporter shutdown; `Operation.Run` 1/4 |
| 12 | `output/websocket/websocket.go:608-617,789` | Q | F; server/client admission |
| 13 | `processor/agentic-dispatch/component.go:323-347,385` | M | F; `startDone` and consumers |
| 14 | `processor/agentic-governance/component.go:224-235,522` | M | F; `startDone` and consumers |
| 15 | `processor/agentic-loop/component.go:447-463,599` | M | F; `startDone` and consumers |
| 16 | `processor/agentic-model/component.go:272-288,487` | M | F; `startDone` and consumer |
| 17 | `processor/agentic-tools/component.go:180-196,481` | M | F; `startDone` and consumer |
| 18 | `processor/gated-dag/executor.go:188-209` | S | dispatcher admission and wait group |
| 19 | `processor/graph-clustering/component.go:1055-1081` | S | watcher/monitor wait group |
| 20 | `processor/graph-embedding/component.go:706-734` | S | watcher/repair/status wait group |
| 21 | `processor/graph-index-spatial/component.go:503-533` | S | watcher wait group |
| 22 | `processor/graph-index-temporal/component.go:524-553` | S | watcher wait group |
| 23 | `processor/graph-index/component.go:658-717` | P | `Signal`; pool `Operation.Run` 2/4 |
| 24 | `processor/graph-index/keyed_dispatcher.go:58-74` | S | lane admission and wait group |
| 25 | `processor/graph-ingest/component.go:969-1024` | P | consumer/pool admission; `Operation.Run` 3/4 |
| 26 | `processor/graph-query/component.go:520-542` | S | view-supervisor wait group |
| 27 | `processor/json_filter/json_filter.go:219-225,428` | F | subscriptions |
| 28 | `processor/json_generic/json_generic.go:196-202,385` | F | subscriptions |
| 29 | `processor/json_map/json_map.go:237-243,430` | F | subscriptions |
| 30 | `processor/research-graph-assess/component.go:153-161,279` | F | subscriptions |
| 31 | `processor/research-graph-classify/component.go:216-224,352` | F | subscriptions |
| 32 | `processor/research-graph-execute/component.go:191-199,281` | F | subscriptions |
| 33 | `processor/research-graph-route/component.go:159-167,301` | F | subscriptions |
| 34 | `processor/research-graph-synthesize/component.go:137-145,257` | F | subscriptions |
| 35 | `processor/rule/processor.go:927-965,1193` | S | cron/watch admission and wait group |
| 36 | `service/base.go:252-305` | S | health/context-monitor wait group |
| 37 | `service/component_manager.go:486,668,913,1002-1019,2124-2199` | M | F and Q; manager plus component runtime |
| 38 | `service/message_logger.go:568-681` | S | subscriptions/streams; early cancellation |
| 39 | `service/metrics.go:112-169` | F | base service and exporter cleanup |
| 40 | `service/milestone_service.go:51,94-120` | P | subscriber stop; `Operation.Run` 4/4 |
| 41 | `service/service_manager.go:761-986` | Q | three executable quiesce calls |
| 42 | `storage/objectstore/component.go:397-423,524` | M | F; `startDone`, consumer and store cleanup |

Primary totals: S=14, F=13, Q=4, P=4, M=7; total=42. The 21 failed-Start call sites are the 13 F owners,
the seven M owners, and Q owner WebSocket output. The eight executable quiesce sites are graph gateway, WebSocket
input, WebSocket output, two component-manager paths, and three service-manager paths.

### 2026-08-19 owner supersession note

The historical 42-owner census, classifications, and labels above remain unchanged. The owner-approved target and
complexity-budget statements below are superseded only where they proposed a shared completion-wait helper. Exact
completion waits are owner-local inline context-bounded selects. The sole final shared helper is parent-aware
`internal/lifecyclecleanup.RollbackFailedStart(parent, rollback)`, born with R1. The recovery ledger and corrected
family-wave design remain the execution authority.

## Owner-local complexity budgets

| Class | Allowed state and exact running-Stop algorithm | Shared mechanism | Explicitly out of budget |
|---|---|---|---|
| S | existing lifecycle phase, one cancel, one done/WG: fence local admission, cancel, await done, cleanup | owner-local inline context-bounded select over exact done, no shared wait | awaiting ctx-driven done before cancel; per-owner `sync.Once`, retained error, resume channel |
| F | S budget plus `cleanupPending` and retained handles; successful running Stop follows S/Q as owned resources require | only final parent-aware `lifecyclecleanup.RollbackFailedStart`, born R1 | clearing handles after timed-out rollback, detached cleanup, allowing another Start |
| Q | concrete native handles: fence, Drain/Shutdown, await exact native Closed while callbacks remain live, cancel, await done/WG, cleanup | owner-local inline selects for Closed then done, no shared wait | cancel before Closed; generic quiesce registry, catalog rediscovery, later terminal rejoin |
| P | concrete protocol authority: fence, complete native drain/pool protocol while callback authority is live, cancel, await done/WG, cleanup | direct native call plus owner-local selects | generic executor election, result accumulation, retained replay result |
| M | first await `startDone`; then choose failed-Start cleanupPending or the owned S/Q/P sequence | owner-local inline selects for startDone/native/done, no shared wait | cleanup before Start finalizes, losing failed-Start authority, a second generation abstraction |

The only final shared helper is parent-aware `internal/lifecyclecleanup.RollbackFailedStart(parent, rollback)`.
Completion waits remain owner-local inline context-bounded selects. The helper stores no context, operation, once,
phase, or result and cannot elect an executor, resume an expired operation, or retain results.

## Failed-Start authority invariant

1. Publish the owner record before the first acquisition that Stop may need to clean.
2. Close `startDone` before rollback waits for Start finalization.
3. Attempt synchronous rollback under the one bounded framework cleanup context.
4. Clear the owner record and enter stopped only after cleanup succeeds.
5. If rollback fails or expires, retain cancel, done, and every exact acquired handle; mark `cleanupPending`.
6. Reject another Start while cleanup is pending.
7. Allow later manager `Stop(ctx)` to retry retained cleanup; clear the record only after success.
8. Treat a subsequent completed Stop as nil/no-op.

## Responsibility dispositions

| Symbol/surface | Disposition | Replacement/home |
|---|---|---|
| `lifecyclejoin.Generation`, `NewGeneration`, `Stop`, `StopWithQuiesce`, `Signal`, `Cancel` | remove after owner migration | owner cancel/done/native handles plus owner-local inline context-bounded selects |
| `lifecyclejoin.Operation`, `NewOperation`, `Operation.Run` | remove | direct synchronous exporter/pool/subscriber protocol call |
| `lifecyclejoin.RunPartialStartRollback` | retain initially; delete only with equivalent bounded failed-Start helper | failed-Start authority invariant |
| proposed `natsclient.ManagedConsumer`, `DrainAndDelete`, retained drain/result state | remove from target; never land | native `ConsumeContext` |
| `Client.consumers`, `consumersMu`, `consumerBinding`, `stopAllConsumers` | remove as child lifecycle catalog | owner handles; separate observer and optional identity claim |
| `Client.StopConsumer` | remove | exact owner calls native drain and waits Closed |
| `Client.StopAndDeleteConsumer` | remove | namespace-scoped fixture/admin deletion |
| `Client.StopAllConsumers` | remove | composition invokes every owner Stop before Client Close |
| five `DeleteConsumerOnStop` fields | remove from configs and generated schemas/examples | fixture/admin teardown |
| `ConsumeDurable` | remove; zero production adopters | retained native handle plus existing heartbeat/settlement helper |
| same-name pre-stop at `natsclient/stream.go:359-372` | remove | boot validation or minimal reject-only active claim |
| `Client.OutstandingWork(stream,name)` | remove after independent observers bind exact observation | graph readiness/inflight observer, not lifecycle handle |

## Settlement evidence

The pinned census found 44 settlement calls, including 26 direct Ack calls, zero production `DoubleAck`, and zero
production `AckSync`. Settlement errors were predominantly logged or ignored. Graph poison ACKs occur before keyed
admission; the keyed guard begins later. Therefore contract proof must distinguish the existing counted pre-pool poison
disposition from keyed effect/guard/ACK convergence, and no plain Ack path may claim server-confirmed settlement.

## Current recovery checkpoint

Everything above this heading is the immutable census at `63a733a2378dff9f09c74c461ba776d352f79221`. It remains
the record of the 42-owner design review and must not be rewritten to resemble later repository state.

The current `main` checkpoint, definitions, workspace state, and zero-count exit gates are maintained in
[`recovery-ledger.md`](recovery-ledger.md). At the pinned recovery baseline `9fcc841e`, 41 production owner files still
imported `internal/lifecyclejoin`; only one of the historical 42 owners had been migrated.
