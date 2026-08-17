# PR2 reset: keep/drop/rewrite inventory

Inventory target: the frozen dirty PR2a surface at baseline `991c96bb517f74350cfabce3d55fed7c130b8833`.

Approval provenance: this inventory was seeded from the owner-approved reset artifact with SHA-256
`d22ba6806f0c355062ca021106683d98c05a2c402c434bbdac9626a51fe988a0`. Its lifecycle rulings are unchanged; this
in-tree derivative replaces ephemeral companion references and records the reviewer-required conformance evidence.

- Baseline HEAD: `991c96bb517f74350cfabce3d55fed7c130b8833`
- Measured dirty surface: 36 tracked changes plus 6 untracked files, 42 unique files total.
- Final disposition: **12 KEEP unchanged, 5 DROP entirely, 25 REWRITE/mixed**.
- This is an architect draft. It does not authorize or perform source changes.

## Surface inventory

### 1. Claimed gap

The missing capability is not “a Client-wide lifecycle coordinator.” Baseline already has the useful local primitives:

- `Client` has one terminal flag and serial Close lock at baseline `natsclient/client.go:145-149`.
- `Subscription.Drain(ctx)` is already a wrapper-local operation at baseline `natsclient/client.go:864-946`.
- managed-consumer drain is already rejoinable through a local `sync.Once` plus `ConsumeContext.Closed()` at baseline
  `natsclient/client.go:627-648` and `natsclient/stream.go:689-723`.
- the NATS dependency explicitly distinguishes Drain from Unsubscribe. In nats.go v1.52.0, subscription Drain removes
  interest but finishes queued callbacks (`nats.go:5055-5076`), while Unsubscribe uses the abrupt path
  (`nats.go:5171-5203`). Connection Drain starts an asynchronous drain goroutine and returns before CLOSED
  (`nats.go:6175-6201`); `Conn.StatusChanged(CLOSED)` is available as native completion observation
  (`nats.go:6504-6516`).

The measured gaps are narrower:

1. baseline `Connect` detaches `nats.Connect` in a goroutine and starts metrics from `context.Background()`
   (`natsclient/client.go` at baseline `471-550`);
2. baseline health/metrics workers do not both expose exact completion joins;
3. baseline `Close` abruptly stops every cataloged child, then drains transport (`natsclient/client.go` at baseline
   `562-617`), instead of requiring each actual resource owner to drain its exact handle before transport Close;
4. raw native capabilities let callers bypass Client lifetime claims (`GetConnection` at baseline `205-208`,
   `JetStream` at baseline `998-1009`, plus Stream/KV-returning methods);
5. operation waits such as `WaitForConnection` and batch futures must remain caller-context-owned.

The dirty PR2a turned those gaps into a universal ownership protocol: two admission gates, connection generations,
subscription and consumer catalogs, setup/delete reservations, readiness latches, exact parent/child completion
records, forced convergence, and publisher settlement. The machinery is directly visible at dirty
`natsclient/lifecycle.go:13-446`, `natsclient/client.go:68-130,486-1039,1079-1439`, and
`natsclient/stream.go:315-503,768-980`.

Two real deadlocks prove that Client-global admission is the wrong model:

- `PublishBatchToStream` retains an I/O lease while waiting on PubAck futures (dirty
  `natsclient/client.go:1814-1869`), while Close fences and waits I/O before native publisher cleanup
  (`natsclient/client.go:875-951`). A withheld ACK therefore prevents Close from reaching the operation that can
  release the future.
- `WaitForConnection` retains an I/O lease through an indefinite polling loop (dirty
  `natsclient/client.go:353-374`), while Close waits that lease. With no caller deadline or connection success, neither
  side can progress.

### 2. Every current spelling of the modeled lifetime fact

| Fact | Current spellings and measurements |
|---|---|
| Process connection ownership | The only non-test production `Client.Connect` call is composition bootstrap at `internal/bootstrapobservability/bootstrap.go:157`. The dirty graph-ingest change removes component-owned Connect at `processor/graph-ingest/component.go:888-895`. |
| Client terminal state | Baseline `closeMu` + `closed` at `natsclient/client.go:145-149`; dirty replacement `clientLifecycle.phase`, two gates, generation and result at `natsclient/lifecycle.go:111-287`. These are competing homes; keep one terminal home. |
| Core subscription ownership | 27 production files contain `.Subscribe(`. The wrapper is returned at `natsclient/client.go:1079-1188`; dirty Client also retains a catalog at `natsclient/client.go:77,839-847,1376-1439`. Owner handle and Client catalog duplicate the same fact. |
| Managed consumer ownership | Exact constructor census below replaces the earlier 22-file aggregate: 20 standard code calls in 16 files including the zero-consumer `ConsumeDurable` delegation (19 remain after retirement), one direct Contexts caller, eight internal calls, and zero production `ConsumeDurable` callers. Fourteen production files contain `StopConsumer`. Dirty Client retains `consumers`, setup and delete maps at `natsclient/client.go:79-83` and routes Stop by string identity at `natsclient/stream.go:768-980`. The component that starts the consumer is the natural exact-handle owner. |
| Raw connection capability | `GetConnection` has 9 production-file consumers, including input/file/udp/http, objectstore, gateway HTTP and service runtime logging (`rg -n 'GetConnection\\(' --glob '*.go' --glob '!**/*_test.go'`). |
| Raw JetStream capability | `.JetStream(` has 28 non-`*_test.go` candidate calls across 24 files and 178 all-Go calls across 97 files. Returned Stream, KV, watcher, future and native response handles add further lifetime-capable surfaces. Client Close cannot revoke or count work performed through any of them. |
| Client-owned workers | Health is started from Connect; metrics poller is in `natsclient/jetstream_metrics.go:311-339`. These are the only child goroutines Client itself can and should cancel and exactly join. |
| Async publication lifetime | `PublishToStreamAsync` returns a future and `PublishBatchToStream` waits futures under caller context. Current `nats-streaming` spec makes the caller observe each future and lets a producer drain its own window. Client-global settlement duplicates operation ownership. |
| Controlled shutdown authority | Composition already aggregates service/component Stop and invokes transport Close in the binaries (`cmd/semstreams/main.go:327`, `cmd/e2e-semstreams/main.go:286`). Every controlled shutdown exits the process. Clean versus failed controls exit status and observability; it never licenses an in-process runtime or Client generation. |

Production-zero searches support the already-approved removals. No non-test production consumer remains for
`WithDisconnectCallback`, `WithReconnectCallback`, `WithHealthChangeCallback`, `WithConnectionLostCallback`,
`WithConnectionLossTimeout`, `WithDrainTimeout`, `SetConnection`, `ConnectionOptions`, or `StopAllConsumers`.
`AccountStreamLister` has a present method-value consumer at `service/storage_observability.go:219`; its returned
`StreamLister` is the reviewed narrow, caller-context-owned listing seam classified in the AST companion.

### Constructor census: every managed-consumer birth seam

The earlier 22-file aggregate hid four distinct constructors. Exact non-test, non-E2E production invocation searches
produce this inventory:

| Constructor | Production result | Required migration |
|---|---:|---|
| `ConsumeStreamWithConfig` | 20 code calls in 16 files, including its `ConsumeDurable` delegation; 19 retained calls after that delegation is deleted | Return `(*ManagedConsumer, error)` and make every direct caller retain the handle. |
| `ConsumeStreamWithConfigContexts` | two calls: the wrapper delegation at `natsclient/stream.go:289` and agentic-loop at `processor/agentic-loop/component.go:921` | Return the same handle; the wrapper propagates it and agentic-loop retains it. |
| `ConsumeInternalStreamWithConfig` | eight calls in four production files: `component/registry.go`, four `service/flow_runtime_stream.go` sites, two `agentic/agentrun` sites, and `internal/maxdelivery/observer.go` | Return the same handle. Update `service/flow_runtime_stream.go:293-296`'s `natsSubscriber` interface and every mock/implementation. |
| `ConsumeDurable` | zero non-test production callers; only its own tests and policy-callsite assertions | Retire it and its tests. Do not propagate a new handle through a surface with no present consumer. |

The constructor search is:

```text
rg -n '\.(ConsumeStreamWithConfig|ConsumeStreamWithConfigContexts|ConsumeInternalStreamWithConfig|ConsumeDurable)\('
  --glob '*.go' --glob '!**/*_test.go' --glob '!test/**' --glob '!natsclient/doc.go'
```

The implementation PR must also update compile-time signature assertions in
`natsclient/consumer_policy_callsite_test.go:90-93`, examples, docs, integration tests, and every interface or mock
found by the unfiltered form of the same search. The exact production interface search finds one explicit abstraction:
`service/flow_runtime_stream.go:293-296`'s `natsSubscriber`; the other production sites call concrete `*Client`.

### NATS-native exported-surface census

The precise rule is not zero NATS types. Retire broad mutable roots returned by Client/framework constructors: raw
Conn, JetStream, Stream, KV, ObjectStore, or equivalent unbounded capabilities. Retain narrow dependencies, messages,
values, watchers, listers and futures only when their method set is minimal and caller context plus Stop/completion
ownership is explicit. There is no `Unsafe*` transition.

Full-root input injection defaults to NARROW/REWRITE against a measured local method interface. It may remain only at
a named adapter boundary where the caller already owns the root, the callee does not close/rediscover/manage it beyond
the documented call or constructed-object lifetime, and the API is not a Client/framework convenience constructor.
No current row qualifies for that exception.

The AST-derived per-symbol inventory, including `ReportStore`, `ReportWatchStore`, `AccountLimitReader`,
`CatalogReader`, `BucketSource`, `WatcherSource`, `BucketLastSeq`, `FilteredKeys`, `NewTemporalResolverWithCache`,
`NewNATSStructuralIndexStorage`, and `NewNATSAnomalyStorage`, is the durable companion
`openspec/changes/require-restart-for-config-activation/native-surface-inventory.md`, preserved exactly from the
owner-approved artifact with SHA-256
`d79df592e7049d4f0e3412bf41e8c61d44ea0829a6fddc2734cff40ceb966617`. Its RETIRE/NARROW rows are tag gates;
RETAIN rows state the local ownership proof. Native ObjectStore remains private inside the framework Store wrapper.

### 3. Adjacent claims on the territory

- ADR-094 makes boot the composition boundary, retains rule-definition hot reload, and requires controlled and dirty
  restart proof (`docs/adr/094-boot-only-composition-and-observable-rule-activation.md`). It does not require Client to
  become an ownership ledger.
- Change design D9 already gives lifecycle owners the drain duty and transport the final drain
  (`openspec/changes/require-restart-for-config-activation/design.md:182-225`). Dirty D10 adds the overbuilt Client
  substrate (`design.md:227-280`) and must be replaced.
- Task 2.5 currently requires Client Close to rejoin every remaining consumer/subscription
  (`openspec/changes/require-restart-for-config-activation/tasks.md:22-32`). That conflicts with owner-local lifetime.
- The dirty `restart-safe-shutdown` delta requires staged gates, Client catalogs, reservations, generations, readiness,
  pDone and forced convergence (`spec.md:3-140`). The later owner-drain, controlled restart, settlement and dirty-power
  requirements (`spec.md:143-332`) are the valuable contract.
- Current `service-shutdown` requires idempotent Stop and genuine error aggregation
  (`openspec/specs/service-shutdown/spec.md:12-67`). The pending lifecycle change further requires Stop to drain before
  Start cancellation and prohibits inventing a replacement context.
- `nats-streaming` already states batch waits are bounded by caller context and async futures are caller-observed
  (`openspec/specs/nats-streaming/spec.md:45-82`).

### 4. Consumer at birth for every proposed outward change

No observability-only or future-use export is proposed.

| Proposed surface | Present consumer at birth |
|---|---|
| `ManagedConsumer` (name owner may choose) returned by all three retained consume constructors | The measured 19 retained direct `ConsumeStreamWithConfig` calls, agentic-loop's direct Contexts call, and eight internal-consume calls. `ConsumeDurable` has no present production consumer and is removed. |
| `ManagedConsumer.Drain(ctx)` and handle-local `OutstandingWork(ctx)` | Existing component `Stop` implementations plus the two `processor/graph-ingest/readiness.go` callsites and one `processor/agentic-loop/inflight.go` callsite currently calling Client by stream/name. `Client.OutstandingWork(stream,name)` retires. |
| `ManagedConsumer.DrainAndDelete(ctx)` | The four production `StopAndDeleteConsumer` callers migrate to the exact handle. It enforces local drain completion before durable deletion and is the only graceful owner-delete shape. |
| Framework Stream/KV/ObjectStore/watcher operations and values | Present callers are classified in the AST companion. Broad ownership-root returns retire; reviewed narrow values/messages/watchers/listers/futures remain with caller context and Stop/completion ownership. No `Unsafe*` root alias survives. |

`GetConnection` has no replacement export at birth: its nine non-test production users can use existing narrow Client publish,
subscribe, request, status, flush/diagnostic helpers. If one use cannot, that use must justify a narrowly named operation,
not restore the raw connection.

## Adopter seam inventory

### Component author who starts a subscription or consumer

- **Must know in the proposed contract:** retain the exact returned handle; in `Stop(ctx)`, stop local admission, call
  that handle's `Drain(ctx)`, and wait for its native completion before returning. Do not call shared Client Close.
- **If they do nothing:** the consume signature change is a compile failure. If they discard a handle explicitly,
  repository lint/review and the controlled-restart proof fail; Client Close will not silently compensate.
- **Where they find out:** compiler first, then the typed handle documentation and migration table.
- **Should know:** only that a resource they started must be stopped. They should not know Client generations, catalog
  keys, parent/child races, pDone, readiness latches, native drain timeout, or process restart policy.

### Composition-root author

- **Must know:** call `Connect(ctx)` exactly once before components start; stop all resource owners; aggregate every
  Stop error; then call terminal `Close(ctx)` and exit the process. Clean versus failed selects exit status and
  observability only; the supervisor always starts a fresh process.
- **If they do nothing:** disconnected components fail boot visibly; a failed shutdown produces a non-clean process
  result. Even a clean controlled shutdown exits rather than reusing the runtime.
- **Where they find out:** boot error / typed shutdown phase error, then composition docs.
- **Should know:** order and clean-vs-failed result only. They should not predict drain duration or enumerate Client
  children.

### Existing raw-capability adopter, including a sister repository

- **Must know:** a raw native handle is outside Client lifetime guarantees and its work must stop before transport
  Close. Sister repos are read-only to this change and migrate in their own repos.
- **If they do nothing:** removal causes a compile failure; no compatibility shim or `Unsafe*` alias silently keeps
  the old false guarantee.
- **Where they find out:** compiler and migration document, not a runtime log.
- **Should know:** ideally nothing because a narrow framework operation/value replaces the raw handle before tag.

### Deployment supervisor

- **Must know:** every controlled shutdown exits. Clean shutdown is an observed all-owner Stop + Client Close result;
  failed shutdown changes status/diagnostics; dirty power loss runs no shutdown. All three paths lead to a fresh
  supervisor-started process.
- **If they do nothing:** durable JetStream/KV recovery and boot reconciliation recover crash-critical work; the
  supervisor must not use a clean marker as a prerequisite for applying desired config.
- **Where they find out:** process exit/status and operations runbook.
- **Should know:** clean versus failed/dirty only. The framework owns storage verification, redelivery and boot
  reconciliation.

The design asks no adopter to predict a timeout, generation, readiness state, catalog identity, or buffer state.

## Exact file disposition

`KEEP` means preserve the current dirty change unchanged. `DROP` means delete the untracked file or retain the tracked
baseline. `REWRITE` means retain only the named concept and replace the current lifecycle teaching.

| File | Disposition | Reason |
|---|---|---|
| `docs/README.md` | REWRITE | Keep an operations link, but describe owner-local drain and terminal transport Close, not rejoinable Client catalogs. |
| `input/websocket/websocket_input_integration_test.go` | REWRITE | Remove exact rich Client parent/child failure assertion; the test intentionally breaks transport and should own only the resulting non-clean teardown. |
| `natsclient/README.md` | REWRITE | Keep composition ownership/API retirements; replace staged/rejoinable Client claims and handle caveat with the minimal contract. Do not discard Close error. |
| `natsclient/client.go` | REWRITE | Preserve useful non-lifecycle behavior, synchronous Connect, private FlusherTimeout 5s, terminal transport Close, conservative preclosed/LastError failure, worker joins and removals; delete gates, generations, catalogs, readiness, publisher convergence and parent/child accounting. |
| `natsclient/client_async_error_test.go` | KEEP | Correctly removes tests of retired adopter callbacks. |
| `natsclient/client_close_test.go` | KEEP | Keep deletion of baseline helper tests tied to the retired drain-timeout implementation; add new minimal tests elsewhere. |
| `natsclient/client_test.go` | KEEP | Correctly removes raw ConnectionOptions and watchdog/callback tests. |
| `natsclient/consumer_policy.go` | REWRITE | Restore baseline business logic; drop only the dirty Client admission hunk. |
| `natsclient/consumer_policy_test.go` | REWRITE | Preserve policy-order tests; drop pre-ready latch test and fake machinery. |
| `natsclient/consumer_stop_test.go` | REWRITE | Move the useful one-drain/rejoin tests to the returned owner handle; remove Client map/name ownership. |
| `natsclient/doc.go` | REWRITE | Keep retired APIs and composition ownership; teach owner handles + terminal transport only. Show bounded Close and handle its error. |
| `natsclient/heartbeat_integration_test.go` | REWRITE | Keep settlement behavior; stop asserting a specific Client-ledger failure after intentional raw close. |
| `natsclient/integration_test.go` | KEEP | Correctly deletes skipped reconnection-callback and mutable health-callback tests. |
| `natsclient/jetstream_metrics.go` | REWRITE | Keep cancel+done exact join concept using a tiny worker record local to Client; no generic `lifecycleWorker`. |
| `natsclient/options.go` | KEEP | Correctly retires callbacks/watchdog and caller drain-timeout knob with zero production consumers. |
| `natsclient/request.go` | REWRITE | Drop gates/generation/readiness/catalog. Preserve reply-size diagnostics without returning `*nats.Msg`; return owner-local Subscription and framework response values. |
| `natsclient/request_response_bounds_integration_test.go` | REWRITE | Preserve the diagnostic scenario but avoid exported `SetConnection`; use a narrow package-private fixture seam without teaching runtime mutation. |
| `natsclient/storage_inventory.go` | REWRITE | Drop admission; retain `AccountStreamLister`/`StreamLister` as the present read-only lister seam whose collection calls are caller-context-owned. |
| `natsclient/stream.go` | REWRITE | All three retained consume constructors return exact managed-consumer handles. Add handle `DrainAndDelete`; remove Client maps, setup/delete reservations and name-routed Stop/Delete. Remove broad Stream roots; retain PubAck only as the reviewed caller-context-owned value seam. |
| `natsclient/stream_integration_test.go` | KEEP | Correctly deletes public bulk abort coverage. |
| `natsclient/subscription_test.go` | REWRITE | Retain native Drain-once + CLOSED join tests on the wrapper. Delete exported Unsubscribe/Abort, catalog growth, gate arbitration, parent generation, pDone and forced abort teaching. |
| `openspec/changes/require-restart-for-config-activation/design.md` | REWRITE | Replace D10 entirely and make D9 name owner-local drain and terminal transport Close. |
| `openspec/changes/require-restart-for-config-activation/inventory.md` | REWRITE | Replace PR2a cut with this measured ownership split and raw-capability census. |
| `openspec/changes/require-restart-for-config-activation/proposal.md` | REWRITE | Preserve composition-owned Connect/API retirement, remove “Client Close rejoinable” implication. |
| `openspec/changes/require-restart-for-config-activation/specs/restart-safe-shutdown/spec.md` | REWRITE | Drop the first overbuilt requirement; retain controlled/dirty restart and rewrite graceful NATS teardown around exact owner handles. |
| `openspec/changes/require-restart-for-config-activation/tasks.md` | REWRITE | Replace task 2.5 and PR2a note; add owner-handle migration and raw-capability work. |
| `output/websocket/websocket_integration_test.go` | REWRITE | Same intentional-transport-break cleanup correction as websocket input. |
| `pkg/logging/doc.go` | REWRITE | Do not demonstrate unbounded `context.Background()` Close or discard the error; use a fresh bounded shutdown context and report failure. |
| `processor/agentic-tools/outcomes_integration_test.go` | REWRITE | Preserve redelivery proof; remove exact Client parent-before-child cleanup string. |
| `processor/agentic-tools/startup_atomic_integration_test.go` | KEEP | Invalid-subject injection tests the same rollback without raw mutable connection seam. |
| `processor/graph-ingest/README.md` | KEEP | Correct composition-owned connection contract. |
| `processor/graph-ingest/component.go` | KEEP | Correctly removes component-owned Connect/WaitForConnection. |
| `processor/graph-ingest/component_test.go` | KEEP | Correct boot prerequisite and explicit cancellation test. |
| `processor/graph-ingest/doc.go` | KEEP | Correctly documents shared connection ownership. |
| `service/doc.go` | REWRITE | Same bounded, error-observed Close documentation correction as logging. |
| `test/testinfra/policy_baseline.json` | KEEP | Correctly removes sleep debt belonging to deleted callback tests. |
| `docs/operations/migration-restart-safe-nats-client.md` | REWRITE | Replace with exact API removals, returned-handle migration, companion raw-root dispositions and controlled-shutdown process-exit sequence. |
| `natsclient/client_close_lifecycle_integration_test.go` | DROP | Entire current file tests Client catalogs, gates, parent/child generations, readiness and publisher settlement. Add a new small transport test separately for clean drain, real preclose-with-nil-LastError failure, historical-error failure and blocked-write ceiling. |
| `natsclient/client_generation_test.go` | DROP | Concurrent/competing Connect and stale-generation behavior is explicitly unsupported; no generation state remains. |
| `natsclient/close_lifecycle_test.go` | DROP | Entire file teaches gates, completion-first, readiness, catalogs, forced convergence and generic lifecycle workers. Recreate only nil-context, terminal Close result and worker-join tests. |
| `natsclient/consumer_delete_lifecycle_test.go` | DROP | Entire file exists for delete reservations and Client takeover; deletion is separate from exact owner drain. |
| `natsclient/lifecycle.go` | DROP | Entire universal lifecycle substrate violates the reset complexity budget. |

## Tests/spec/docs that currently teach the rejected contract

- `client_generation_test.go` teaches multiple/competing Connect generations.
- `close_lifecycle_test.go` teaches admission leases, staged fences, callback readiness, Client catalogs, forced
  convergence, publisher cleanup and generic worker abstractions.
- `consumer_delete_lifecycle_test.go` teaches per-key continuation reservations and Client takeover.
- `client_close_lifecycle_integration_test.go` teaches Client-owned child drains and raw-parent compensation.
- `subscription_test.go:271-520` teaches exact Client catalog release/snapshot and Drain-vs-Unsubscribe arbitration.
- dirty design D10, task 2.5/PR2a note, and spec lines `3-140` make those implementation mechanisms normative.
- dirty `natsclient/doc.go:326-365`, `natsclient/README.md`, and the migration guide advertise the same overbuilt model.

Those assertions must not be mechanically preserved under renamed helpers; they are the contract being removed.

## Additional implementation surface outside the frozen 42 files

The 42-file disposition remains **12 KEEP / 5 DROP / 25 REWRITE** because it classifies only the frozen dirty PR2a
evidence. The final rulings intentionally expand later implementation PRs beyond that set:

- retire `natsclient/consume_durable.go` and its tests because the production consumer census is zero;
- update `service/flow_runtime_stream.go`'s `natsSubscriber` interface and every constructor caller/mock;
- migrate `natsclient/kvspec.go`, `kv.go`, `test_client.go`, graph/readiness/catalog, flowstore watchers, Rule tracker,
  storage observability, and every RETIRE/NARROW caller classified by the companion AST inventory;
- update process composition and real-process tests so every controlled shutdown exits and only exit status distinguishes
  clean from failed.

These files are required tag work, not a reason to reclassify frozen evidence as already changed.
