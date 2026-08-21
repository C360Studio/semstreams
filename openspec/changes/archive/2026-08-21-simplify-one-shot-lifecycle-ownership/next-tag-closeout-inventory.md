# Next-tag lifecycle closeout problem inventory

## Evidence boundary

- Logical repository baseline:
  `982cceea2e2affa3514a22eae0c0816344638c0d`.
- Current evidence HEAD:
  `8eae8436612b76b72a46feb5173581c73111678a`.
- The prior inventory checkpoint
  `a84137d2bf3f14e081f28c44970c5d6702a09ae083e21d970eb1f3fd6d1c9b40`
  is superseded because it omitted same-class lifecycle facts and adjacent
  binding contracts recorded below.
- The inventory checkpoint
  `68e6f50caf05a407eba5e6411f4dc7af2ccb4f4704ef1d89126cf99e5b5683f0`
  is superseded by this second factual correction. It omitted completed scanner
  truth, native async ownership surfaces, one production-compiled test root,
  generic metric consumers, the sibling embedding interface, and open issues
  #1005/#1006.
- Baseline-relative facts remain identified as baseline facts. Facts completed
  by commit `8eae8436` are explicitly identified as current-HEAD facts.
- This artifact records only current surfaces, callers, owners, behavior,
  binding claims, adopter seams, and empty searches. It contains no disposition,
  target state, option selection, recommendation, or task delta.
- A native NATS value, interface, or exact lifecycle handle is not evidence for
  a repository-defined wrapper.
- Binding rulings remain with the owner.

## Current scanner surfaces

Commit `912a5be6031ccd4e7132ed9dd19ad44af73013f6`
(`test: isolate repository scanners from worktrees`) completed the seven
previously inventoried `.claude` exclusions:

- component-status retirement scanner:
  `test/contract/component_status_retirement_contract_test.go:50-68`,
  exclusion at line 61;
- consumer-policy production scanner:
  `natsclient/consumer_policy_callsite_test.go:239-260`,
  exclusion at line 248;
- monitoring-consumer scanner:
  `natsclient/monitoring_consumers_test.go:28-42`,
  exclusion at line 35;
- active documentation/port grammar scanner:
  `internal/portgrammarcontrol/runtime_completeness_test.go:64-98`,
  exclusion at line 70;
- production renderer scanner:
  `internal/portgrammarcontrol/runtime_completeness_test.go:103-115`,
  exclusion at line 109;
- target-completeness scanner:
  `internal/portgrammarcontrol/target_test.go:697-712`,
  exclusion at line 705;
- test-infrastructure policy scanner:
  `test/testinfra/policy_guard_test.go:300-325`,
  exclusion at line 309.

The consumer-policy scanner has a focused worktree-contamination fixture at
`natsclient/consumer_policy_callsite_test.go:99-116`. The policy guard has a
focused fixture and asserts both zero contaminated findings and one scanned
root file at `test/testinfra/policy_guard_test.go:71-112`.

These remain package-local scanner implementations. No shared scanner service
or exclusion registry exists.

Current focused verification at HEAD
`8eae8436612b76b72a46feb5173581c73111678a`:

```text
go test ./natsclient -run \
  'TestParseProductionGoFilesIgnoresClaudeWorktrees|TestConsumerPolicyProductionCallsiteCensus|TestConsumerPolicyExportedClientAPICensus|TestMonitoringURLConsumersExplicitlyEnableMonitoring' \
  -count=1
ok github.com/c360studio/semstreams/natsclient 0.877s

go test ./internal/portgrammarcontrol -run \
  'TestRuntimePortGrammarCompleteness|Test.*Target.*Completeness' -count=1
ok github.com/c360studio/semstreams/internal/portgrammarcontrol 0.858s

go test ./test/contract -run TestComponentStatusPlaneRemainsRetired -count=1
ok github.com/c360studio/semstreams/test/contract 0.497s

go test ./test/testinfra -run \
  'TestInfrastructurePolicyGuard$|TestInfrastructurePolicyGuardIgnoresClaudeWorktrees' \
  -count=1
ok github.com/c360studio/semstreams/test/testinfra 0.910s
```

These focused current results do not restate the older full candidate-gate
claims recorded in the active tasks and recovery ledger.

## Current policy-baseline rows

The same commit removed all four stale rows previously named here:

- `TestRegistry_MultiNodeDiscovery`;
- `TestRegistry_SubscribeCapabilities`;
- `TestService_ConcurrentOperations`, ordinal 1;
- `TestService_ConcurrentOperations`, ordinal 2.

The current direct search is empty:

```text
rg -n \
  'TestRegistry_MultiNodeDiscovery|TestRegistry_SubscribeCapabilities|TestService_ConcurrentOperations' \
  test/testinfra/policy_baseline.json
```

The baseline remains present at
`test/testinfra/policy_baseline.json:1-3`. Its first remaining Registry row is
`TestRegistry_Heartbeat` at lines 4-8. Current service rows begin with
`TestService_ContextCancellation` at lines 1234-1238.

The policy guard reads the baseline and requires an exact match at
`test/testinfra/policy_guard_test.go:56-69`.

## Task 3.3 authority

Task 3.3 is unchecked at
`openspec/changes/simplify-one-shot-lifecycle-ownership/tasks.md:285-294`.

The previously approved surface census is
`openspec/changes/require-restart-for-config-activation/native-surface-inventory.md`.
Its rule and terminology are at lines 5-13, its rows at lines 15-132, and its
release check at lines 134-136. The migration guide identifies that artifact
as the separate raw-root gate at
`docs/operations/migration-restart-safe-nats-client.md:324-329`.

The old inventory predates this baseline. The following sections report
current survivors by the kind of capability actually present rather than
assuming every native interface has the same ownership meaning.

## Current broad ownership outputs and mutation surfaces

### Native connection

`Client.GetConnection` returns `*nats.Conn` at
`natsclient/client.go:205`.

Current non-test callers are:

- `input/http/http.go:55`
- `input/file/file.go:615`
- `input/udp/udp.go:721`
- `gateway/http/http.go:432`
- `service/service_manager.go:754`
- `internal/e2eslowconsumer/probe_e2e.go:35`
- `storage/objectstore/component.go:816`

`TestClient.GetNativeConnection` delegates to it at
`natsclient/test_client.go:947-948`.

`Client.SetConnection` accepts and replaces `*nats.Conn` at
`natsclient/client.go:224`. Its measured callers are integration tests:

- `natsclient/request_response_bounds_integration_test.go:165,187`
- `processor/agentic-tools/startup_atomic_integration_test.go:29-30,51`

### JetStream account root

`Client.JetStream` returns `jetstream.JetStream` at
`natsclient/client.go:844`.

Current production callers include:

- `natsclient/kvspec.go:254`
- `natsclient/storage_inventory.go:112`
- `graph/owned_bucket_retention.go:48`
- `processor/graph-ingest/readiness.go:209`
- `processor/graph-ingest/component.go:1540`
- `processor/rule/processor.go:653,798,1174`
- `config/streams.go:323,617`
- `storage/objectstore/store.go:110`
- `storage/objectstore/component.go:918`
- `gateway/graph-gateway/component.go:1037`
- `output/file/file.go:426`
- `output/httppost/httppost.go:425`
- `output/websocket/websocket.go:1165`
- `output/otel/component.go:261`
- agentic component call sites at
  `processor/agentic-dispatch/component.go:655`,
  `processor/agentic-governance/component.go:504`,
  `processor/agentic-model/component.go:452`,
  `processor/agentic-loop/component.go:752,1074`,
  `processor/agentic-loop/inflight.go:163`, and
  `processor/agentic-tools/component.go:465`

Additional callers exist under `test/e2e`.

### Stream and KV acquisition outputs

Current Client methods returning `jetstream.Stream` or
`jetstream.KeyValue` are:

- `CreateStream`: `natsclient/client.go:858`
- `GetStream`: `natsclient/client.go:1197`
- `CreateKeyValueBucket`: `natsclient/client.go:1237`
- `GetKeyValueBucket`: `natsclient/client.go:1293`
- `WaitForBucket`: `natsclient/client.go:1336`
- `EnsureStream`: `natsclient/stream.go:146`

Measured production callers include:

- `GetStream`: `agentic/agentrun/agentrun.go:712`
- `EnsureStream`: `component/registry.go:1117` and
  `processor/gated-dag/component.go:185`
- `CreateKeyValueBucket`: manager constructors at
  `config/manager.go:74`, `flowstore/manager.go:33`,
  `flowtemplate/manager.go:28`, and `persona/manager.go:34`;
  research components at
  `processor/research-graph-synthesize/component.go:197`,
  `processor/research-graph-execute/component.go:252`,
  `processor/research-graph-route/component.go:225`,
  `processor/research-graph-assess/component.go:212`, and
  `processor/research-graph-classify/component.go:276`;
  plus `processor/agentic-tools/store.go:50`,
  `processor/rule/kv_config_integration.go:573`, and
  `processor/rule/kv_writer.go:60`
- `GetKeyValueBucket`: `pkg/dispatch/completion_watcher.go:57`,
  `service/message_logger_kv_watch.go:198`,
  `graph/owned_bucket_retention.go:65`,
  `processor/agentic-dispatch/http_activity.go:200`,
  `processor/agentic-dispatch/terminal_settlement.go:89`,
  `processor/agentic-governance/violation.go:170`, and
  `processor/agentic-tools/executors/lazy_loops_kv.go:34`

`WaitForBucket` has no measured caller other than its declaration.
`CreateStream` has no measured production caller through `Client`; the
similarly named call at `config/streams.go:704` is on a JetStream value.

TestClient acquisition helpers remain at:

- `natsclient/test_client.go:953-965`
- `natsclient/test_client.go:974-986`

`ScheduleTracker.Bucket` returns its stored KV value at
`processor/rule/schedule_tracker.go:176`; no direct call was found by the
method-name search.

Additional exported acquisition functions returning `jetstream.KeyValue` are:

- `graph.EnsureCatalogBucket`: `graph/kvcatalog.go:202-209`
- `readiness.EnsureBucket`: `graph/readiness/publisher.go:43-47`
- `natsclient.EnsureFrameworkBucket`: `natsclient/kvspec.go:226-284`
- `natsclient.OpenFrameworkBucket`: `natsclient/kvspec.go:304-312`

Measured production callers are:

- `graph.EnsureCatalogBucket`:
  `service/storage_observability.go:397`,
  `processor/graph-ingest/component.go:1129,1149,1174`,
  `processor/graph-index-temporal/component.go:525,537`,
  `processor/graph-index/component.go:898`,
  `processor/graph-index-spatial/component.go:514`,
  `processor/graph-clustering/component.go:979,990`,
  `processor/graph-clustering/anomaly.go:166`,
  `processor/agentic-tools/component.go:230`, and
  `processor/graph-embedding/component.go:945,951`
- `readiness.EnsureBucket`:
  `processor/graph-ingest/readiness.go:371`,
  `processor/rule/readiness.go:191`,
  `processor/graph-index/component.go:913`, and
  `processor/graph-embedding/component.go:966`
- `natsclient.EnsureFrameworkBucket`:
  `graph/kvcatalog.go:209` and
  `processor/graph-index/component.go:885`
- `natsclient.OpenFrameworkBucket`:
  `graph/kvcatalog.go:244`

`graph.EnsureCatalogBucket` delegates to
`natsclient.EnsureFrameworkBucket`; `readiness.EnsureBucket` delegates to
`graph.EnsureCatalogBucket`; and the catalog reader path delegates to
`natsclient.OpenFrameworkBucket`.

## Existing core-interface input injection

These current surfaces accept existing NATS core interfaces as dependencies.
The inventory does not establish that the interface input transfers transport
ownership or that a repository-defined wrapper is necessary.

### KV inputs

- Community storage:
  `graph/clustering/storage.go:69,88`
- Summary storage:
  `graph/clustering/summary_store.go:120`
- Embedding storage:
  `graph/embedding/storage.go:194`
- Embedding worker:
  `graph/embedding/worker.go:245`
- Anomaly storage:
  `graph/inference/storage.go:93`
- Temporal resolver:
  `natsclient/kv_temporal.go:20,44`
- Rule schedule and state trackers:
  `processor/rule/schedule_tracker.go:97` and
  `processor/rule/state_tracker.go:77`
Additional current core-interface inputs are:

- `Client.NewKVStore` accepts `jetstream.KeyValue`:
  `natsclient/kv.go:54`
- `BucketRetention` accepts `jetstream.KeyValue`:
  `natsclient/kv.go:133`
- `EnhancementWorkerConfig.CommunityBucket` and `.SummaryBucket` are
  `jetstream.KeyValue` fields:
  `graph/clustering/enhancement_worker.go:86-94`
- `ReviewWorkerConfig.AnomalyBucket` is a `jetstream.KeyValue` field:
  `graph/inference/review_worker.go:65-73`

Measured production callers and composition sites are:

- `Client.NewKVStore`:
  `frameworkcapabilities/graphresearch/register_tool.go:55`,
  `config/manager.go:84`,
  `flowstore/manager.go:44`,
  `flowtemplate/manager.go:36`,
  `persona/manager.go:42`,
  `processor/graph-ingest/component.go:1133,1153,1178`,
  `processor/graph-index/component.go:902,928`,
  `processor/rule/kv_writer.go:69`,
  `processor/rule/kv_config_integration.go:581`,
  `processor/agentic-tools/executors/lazy_loops_kv.go:38`,
  and the research components at
  `processor/research-graph-synthesize/component.go:209`,
  `processor/research-graph-execute/component.go:264`,
  `processor/research-graph-route/component.go:237`,
  `processor/research-graph-assess/component.go:224`, and
  `processor/research-graph-classify/component.go:288`

A current test-only `Client.NewKVStore` composition site is
`service/kv_test_helpers_test.go:50`. That file is not production-compiled.
- `BucketRetention`:
  `natsclient/kv.go:177`; its other measured callers are integration tests
- `EnhancementWorkerConfig`:
  `processor/graph-clustering/component.go:2295`; additional measured
  integration callers are
  `processor/graph-query/community_summary_wire_integration_test.go:88` and
  `graph/clustering/enhancement_worker_race_integration_test.go:147`
- `ReviewWorkerConfig`:
  `processor/graph-clustering/component.go:2413`; an additional measured test
  caller is `graph/inference/review_worker_test.go:117`

These are observations of existing NATS core-interface inputs. They do not
establish transport ownership transfer or a need for repository-defined
wrappers.

Measured production composition includes:

- summary-store construction inside
  `graph/clustering/enhancement_worker.go:144`
- schedule and state tracker construction at
  `processor/rule/processor.go:820` and
  `processor/rule/processor.go:681`

Many remaining callers are unit or integration tests, including community
storage callers at
`processor/graph-query/summary_bucket_late_attach_integration_test.go:51` and
`processor/graph-query/community_summary_wire_integration_test.go:111`,
and temporal resolver callers under
`natsclient/kv_temporal_integration_test.go`.

### JetStream input

`ReconcileNoLifecycleRetention` accepts `jetstream.JetStream` at
`natsclient/kv_retention.go:62`.

Current production callers are:

- `graph/owned_bucket_retention.go:78`
- `natsclient/kvspec.go:268`

`NewNATSRelationshipApplier` accepts a JetStream input at
`graph/inference/applier.go:35`; the direct constructor-name search found no
current caller.

## Native value and exact-handle seams

`ConsumeStreamWithConfig` returns the native caller-owned
`jetstream.ConsumeContext` at `natsclient/stream.go:275-287`.

Its 15 measured production selector uses are:

- `examples/processors/document/component.go:496`
- `examples/processors/iot_sensor/component.go:496`
- `output/file/file.go:404`
- `output/httppost/httppost.go:403`
- `output/websocket/websocket.go:1141`
- `processor/agentic-dispatch/component.go:474`
- `processor/agentic-governance/component.go:477`
- `processor/agentic-model/component.go:408`
- `processor/agentic-tools/component.go:404`
- `processor/graph-ingest/component.go:1460`
- `processor/json_filter/json_filter.go:366`
- `processor/json_generic/json_generic.go:342`
- `processor/json_map/json_map.go:388`
- `processor/rule/processor.go:1155`
- `storage/objectstore/component.go:898`

The method-value uses above are selector uses even where the call occurs
through the assigned local function value.

`ConsumeStreamWithConfigContexts` returns the same exact handle and separates
setup from handler authority at `natsclient/stream.go:511-525`. Its measured
production selector use is:

- `processor/agentic-loop/component.go:1015`

`ConsumeInternalStreamWithConfig` returns the same exact handle at
`natsclient/stream.go:290-305`.

Its measured production callers are:

- `agentic/agentrun/agentrun.go:759,779`
- `internal/maxdelivery/observer.go:252`

`ObserveDirectPortConsumerPolicy` accepts an existing
`jetstream.Consumer` and returns a cleanup function at
`natsclient/consumer_policy.go:139-160`.

The current OTEL component consumes it as a method value at
`output/otel/component.go:358`. The PR990 boot-only head
`8f19ef3678a549913385b090e4de1766a7a43a27` contains the equivalent method-value
consumer at `output/otel/component.go:305`.

`ConnectionOptions` returns `[]nats.Option` at
`natsclient/client.go:401`. Its measured external use is
`natsclient/client_test.go:393-402`; production connection composition uses
the private `buildConnectionOptions` at `natsclient/client.go:406,482`.

Two old narrowing rows are already expressed as small inline interfaces:

- `BucketLastSeq`: `natsclient/kv.go:106`
- `FilteredKeys`: `natsclient/kv.go:551`

The current readiness constructors still accept `BucketSource`:

- `graph/readiness/watcher.go:233`
- `graph/readiness/set.go:41`

The source currently acquires a KV bucket at
`graph/readiness/watcher.go:348`. The Fusion adapter supplies this source and
stores the resulting watcher at `pkg/fusion/fusionnats/client.go:132-145`.

## Historical context-field claims and current compiled truth

The historical inventory names five retained runtime-owner context fields at
`openspec/changes/restore-go-lifecycle-ownership/inventory.md:16-31`, and its
task prose repeats that count at
`openspec/changes/restore-go-lifecycle-ownership/tasks.md:34-44`.

Current field-name searches find no declarations of:

- `Component.ingestPoolCtx`
- `Component.ingestSubmitCtx`
- `Processor.watcherCtx`
- `KVConfigManager.ctx`
- `CronScheduler.parentCtx`

Remaining `watcherCtx` matches are parameters or locals, including
`processor/rule/entity_watcher.go:164,238,320`.

A direct AST scan previously found context fields in
`processor/rule/kv_test_helpers.go` and `service/kv_test_helpers.go`. `go list`
placed both ordinary `.go` files in production `.GoFiles`, even though their
measured consumers were tests.

Commit `8eae8436612b76b72a46feb5173581c73111678a`
(`refactor(lifecycle)!: remove production test helpers`) changed that current
truth:

- `processor/rule/kv_test_helpers.go` is absent at current HEAD.
- `service/kv_test_helpers.go` is absent at current HEAD.
- The service helper is now test-only at
  `service/kv_test_helpers_test.go:17-24`.
- Its struct retains only `*testing.T` and `*natsclient.KVStore` at
  `service/kv_test_helpers_test.go:18-21`.
- Bucket creation uses `t.Context()` at
  `service/kv_test_helpers_test.go:40-50`.
- Helper operations use `h.t.Context()` at
  `service/kv_test_helpers_test.go:65-115,136-145`.
- Its test cleanup uses a bounded terminal context at
  `service/kv_test_helpers_test.go:52-57`.

The production AST contract introduced by the same commit:

- loads and type-checks production `./...` packages at
  `test/contract/context_ownership_contract_test.go:19-43`;
- identifies `context.Context` and `context.CancelFunc` at lines 45-59;
- inspects every production struct and reports retained contexts or exported
  cancel authority at lines 60-92;
- covers direct, alias, pointer, container, wrapper, provider, callback, and
  cancel shapes at lines 100-198;
- recursively detects retained or provided contexts at lines 200-251;
- recursively detects cancel authority at lines 254-308.

Current empty searches are:

```text
rg -n 'type KVTestHelper|SetupKVBucketForTesting' processor/rule
rg -n 'ingestPoolCtx|ingestSubmitCtx|parentCtx' --glob '*.go'
```

`Client.WaitForBucket` remains declared at `natsclient/client.go:1336` with no
measured caller. `SetConnection` and `ConnectionOptions` retain test callers but
no measured production callers.

## Current production context roots

Continuing or goroutine work using an invented or detached root is present at:

- `component/registry.go:1207`
- `natsclient/client.go:550`
- `pkg/logging/nats_handler.go:95`
- `pkg/fusion/fusionnats/client.go:140`
- `processor/agentic-tools/recording.go:63,83`

Existing owners visible at those sites are Registry, Client,
`NATSHandler`, the Fusion client holding `statusWatch`, and
`RecordingExecutor` with its worker wait group and shutdown channel,
respectively.

Constructor or adapter work using `context.Background` is present at:

- `config/manager.go:73`
- `flowstore/manager.go:32`
- `flowtemplate/manager.go:28`
- `persona/manager.go:34`
- `graph/query/classifier_embedding.go:44,55`
- `pkg/buffer/circular.go:353`

Other production uses include:

- failed-Start cleanup:
  `processor/graph-index/component.go:632`
- trace detachment helper:
  `natsclient/trace.go:51-60`
- logger-only context arguments:
  `config/stream_bounds.go:432` and
  `processor/agentic-model/client.go:477`
- bounded descendants of a supplied parent:
  `internal/lifecyclecleanup/lifecyclecleanup.go:33`,
  `metric/handler.go:206`,
  `processor/rule/stateful_evaluator.go:247`,
  `processor/agentic-loop/trajectory_handler_wiring.go:126`,
  `processor/agentic-tools/executors/httprequest.go:217`, and
  `processor/agentic-tools/executors/websearch.go:213`

Process-main, test, E2E, and documentation-example roots are separate search
classes and are not included above as production-library findings.

## Package-bounded natsclient asynchronous lifecycle census

### Native asynchronous surfaces without explicit package `go` syntax

The explicit `go`/`AfterFunc` census does not include asynchronous work owned by
NATS native handles. The current package also exposes the following native
callback, watcher, and acknowledgement surfaces.

#### Client.Subscribe and Subscription

`Client.Subscribe` is defined at `natsclient/client.go:790-820`.

- It asks the native connection to create a callback subscription at lines
  803-814.
- Each delivered callback derives a 30-second context from the context supplied
  at subscription acquisition at lines 804-813.
- It returns a caller-owned `*natsclient.Subscription` at lines 815-819.
- Client retains no subscription catalog.

`Subscription` wraps the exact native subscription and its
`SubscriptionClosed` status channel at `natsclient/client.go:711-733`.

Its two terminal operations currently differ:

- `Unsubscribe` directly calls the native operation at
  `natsclient/client.go:735-741`;
- `Drain(ctx)` uses `sync.Once`, calls native `Drain()` synchronously, and then
  observes the native closed channel at lines 743-788.

The supplied Drain context is checked before and after the native call, but it
does not enter `s.sub.Drain()` at line 763. The first invocation owns that
native drain; later invocations reuse the stored result through `drainOnce`.

Measured production `Client.Subscribe` acquisition sites are:

- `storage/objectstore/component.go:509`
- `service/message_logger.go:282`
- `examples/processors/document/component.go:281`
- `examples/processors/iot_sensor/component.go:281`
- `cmd/e2e-semstreams/mission/command.go:257`
- `processor/json_generic/json_generic.go:271`
- `processor/json_filter/json_filter.go:296`
- `processor/json_map/json_map.go:316`
- `processor/rule/processor.go:1092`
- `processor/research-graph-synthesize/component.go:252`
- `processor/research-graph-execute/component.go:276`
- `processor/research-graph-route/component.go:291`
- `processor/research-graph-assess/component.go:272`
- `processor/research-graph-classify/component.go:344`
- `output/file/file.go:330`
- `output/httppost/httppost.go:329`
- `output/websocket/websocket.go:1069`

Current holders include:

- ObjectStore write-subscription slice at
  `storage/objectstore/component.go:73,527`, drained at lines 605-607 and
  cleared at line 643;
- component subscription slices in the document and IoT examples at
  `examples/processors/document/component.go:101,291` and
  `examples/processors/iot_sensor/component.go:101,291`;
- JSON processor slices at
  `processor/json_generic/json_generic.go:89,282`,
  `processor/json_filter/json_filter.go:101,307`, and
  `processor/json_map/json_map.go:108,327`;
- rule processor slice at
  `processor/rule/processor.go:159,1098`;
- research component slices at
  `processor/research-graph-synthesize/component.go:58,259`,
  `processor/research-graph-execute/component.go:62,283`,
  `processor/research-graph-route/component.go:69,298`,
  `processor/research-graph-assess/component.go:68,279`, and
  `processor/research-graph-classify/component.go:80,351`;
- MessageLogger's subscription-record map at
  `service/message_logger.go:166-173,928`;
- output component subscription collections, including
  `output/file/file.go:346`,
  `output/httppost/httppost.go:345`, and
  `output/websocket/websocket.go:883-950,1082`;
- mission command slice at
  `cmd/e2e-semstreams/mission/command.go:154,257`.

Measured lifecycle calls include contextual Drain at:

- `examples/processors/document/component.go:378`
- `examples/processors/iot_sensor/component.go:378`
- `processor/json_generic/json_generic.go:455`
- `processor/json_filter/json_filter.go:499`
- `processor/json_map/json_map.go:501`
- `processor/rule/processor.go:1278-1298`
- `processor/research-graph-synthesize/component.go:324`
- `processor/research-graph-execute/component.go:348`
- `processor/research-graph-route/component.go:365`
- `processor/research-graph-assess/component.go:345`
- `processor/research-graph-classify/component.go:418`
- `output/file/file.go:506`
- `output/httppost/httppost.go:509`
- `output/websocket/websocket.go:922`

The E2E mission command uses native Unsubscribe at
`cmd/e2e-semstreams/mission/command.go:284`.

#### SubscribeForRequests

`Client.SubscribeForRequests` is defined at
`natsclient/request.go:341-429`.

- It creates a native callback subscription at lines 359-422.
- Each request callback derives its message context from the acquisition
  context and the configured request-handler timeout at lines 360-374.
- It replies inside the native callback at lines 375-421.
- It returns the same exact `*Subscription` wrapper at lines 423-428.
- Client retains no responder catalog.

Direct production selector calls are:

- production-compiled research-graph E2E scenario:
  `test/e2e/scenarios/research-graph/scenario.go:169`, retained at lines 84 and
  180 and unsubscribed at line 188;
- E2E lesson curation:
  `cmd/e2e-semstreams/main.go:160`;
- graph-ingest mutation responder:
  `processor/graph-ingest/mutation_runtime.go:27`;
- graph-ingest queries:
  `processor/graph-ingest/query.go:27-48`;
- graph-index queries:
  `processor/graph-index/query.go:26-75`;
- agentic-loop responder composition:
  `processor/agentic-loop/component.go:513`;
- agentic-tools responder composition:
  `processor/agentic-tools/component.go:240`.

Direct returned-handle holders and cleanup include:

- graph-ingest subscription slice and cleanup:
  `processor/graph-ingest/component.go:631,1083-1086,1114`;
- agentic-loop responder fields, assignment, cleanup, and clearing:
  `processor/agentic-loop/component.go:77-78,523,534,676-699,741-742`;
- agentic-tools responder field, assignment, cleanup, and clearing:
  `processor/agentic-tools/component.go:75,250,555-568,600`;
- E2E lesson curation holder and Unsubscribe:
  `cmd/e2e-semstreams/main.go:267,275`.

Additional components consume `SubscribeForRequests` through an existing
function field:

- graph-query:
  `processor/graph-query/component.go:41,201-202`,
  assignment at `processor/graph-query/query.go:76`;
- graph-clustering:
  `processor/graph-clustering/component.go:663-664`,
  assignment at `processor/graph-clustering/query.go:21`;
- graph-index-spatial:
  `processor/graph-index-spatial/component.go:206-207`,
  assignment at `processor/graph-index-spatial/query.go:24`;
- graph-index-temporal:
  `processor/graph-index-temporal/component.go:215-216`,
  assignment at `processor/graph-index-temporal/query.go:19`;
- graph-embedding:
  `processor/graph-embedding/component.go:329-330`,
  assignment at `processor/graph-embedding/query.go:21`.

Their exact returned handles are retained in `querySubscriptions` collections
and drained during component cleanup, including:

- graph-query:
  `processor/graph-query/query.go:91`,
  `processor/graph-query/component.go:650-686`;
- graph-clustering:
  `processor/graph-clustering/query.go:28-49`,
  `processor/graph-clustering/component.go:1190-1252`;
- graph-index:
  `processor/graph-index/query.go:30-79`,
  `processor/graph-index/component.go:789-796`;
- graph-index-spatial:
  `processor/graph-index-spatial/query.go:31-38`,
  `processor/graph-index-spatial/component.go:633-661`;
- graph-index-temporal:
  `processor/graph-index-temporal/query.go:26`,
  `processor/graph-index-temporal/component.go:655-683`;
- graph-embedding:
  `processor/graph-embedding/query.go:28-43`,
  `processor/graph-embedding/component.go:829-878`.

#### Typed subscription delegates

`Subject[T].Subscribe` and `Subject[T].SubscribeWithMsg` delegate directly to
`Client.Subscribe` and return the same caller-owned `*Subscription` at
`natsclient/typed.go:78-100`.

Both typed delegates silently return from decode failure and ignore the typed
handler's returned error at lines 81-88 and 94-100.

No production typed `Subject[T]` use was found outside its declaration and
documentation in `natsclient/typed.go`:

```text
rg -n '\bSubject\[' \
  --glob '*.go' --glob '!**/*_test.go' --glob '!natsclient/typed.go'

rg -n 'NewSubject(WithCodec)?\[' \
  --glob '*.go' --glob '!**/*_test.go' --glob '!natsclient/typed.go'
```

Both searches return no output. `Subject[T]` has exported `Pattern` and `Codec`
fields at `natsclient/typed.go:51-56`, so external code can construct it through
a composite literal as shown by the documentation example at lines 33-49 or
through the constructors at lines 103-115. The direct type-use search closes
the composite-literal shape and the constructor search closes both exported
constructor shapes in this repository.

#### KVStore.Watch and native KeyWatcher

`KVStore.Watch` delegates directly to `jetstream.KeyValue.Watch` and returns the
native `jetstream.KeyWatcher` at `natsclient/kv.go:593-601`. KVStore does not
retain or stop it.

`flowstore.Manager.Watch` re-exports that exact handle at
`flowstore/manager.go:183-187`. No production caller of
`flowstore.Manager.Watch` was found.

The production-compiled rule ConfigManager acquires `KVStore.Watch` at
`processor/rule/kv_config_integration.go:143-152`, retains the native watcher at
lines 51-60, stops a late acquisition at lines 159-165, and joins its watcher
processing before terminal cleanup at lines 225-248.

Other current production native `KeyWatcher` holders, acquired directly from
core KV interfaces, include:

- config manager slice:
  `config/manager.go:31,285-299,373`;
- graph readiness watcher:
  `graph/readiness/watcher.go:233-354`;
- lifecycle manager registration:
  `pkg/lifecycle/manager_query.go:207-233`;
- completion watcher:
  `pkg/dispatch/completion_watcher.go:57-110`;
- graph clustering enhancement worker:
  `graph/clustering/enhancement_worker.go:60`;
- graph inference review worker:
  `graph/inference/review_worker.go:47`;
- graph embedding worker:
  `graph/embedding/worker.go:190`;
- graph-ingest guard watcher:
  `processor/graph-ingest/component.go:1198-1249`;
- rule entity watchers:
  `processor/rule/entity_watcher.go:181-258,439-579`;
- MessageLogger KV watch:
  `service/message_logger_kv_watch.go:195-238`;
- graphview backing watcher:
  `pkg/graphview/view.go:608`.

In each case `Updates()` delivery and watcher termination are native async
behavior. The returned `KeyWatcher` supplies its own `Stop`; no Client or
KVStore watcher catalog exists.

#### Async publish futures and completion

`Client.PublishToStreamAsync` and
`Client.PublishToStreamAsyncWithMsgID` return the exact native
`jetstream.PubAckFuture` at `natsclient/client.go:1000-1027`.

The private operation:

- checks context only before enqueue at
  `natsclient/client.go:1044-1049`;
- calls native `PublishMsgAsync` at lines 1062-1080;
- returns the exact future at lines 1082-1091.

After successful enqueue, cancellation of the supplied context does not cancel
the native publish. Its future continues toward `Ok()` or `Err()`, and async
ack errors also reach the connection-level error handler described at
`natsclient/client.go:997-1010`.

`PublishAsyncComplete` returns the native connection-global completion channel
at `natsclient/client.go:1094-1105`.
`PublishAsyncPending` returns the native connection-global pending count at
lines 1108-1115.

`PublishBatchToStream` is the only production implementation that currently
retains a slice of `PubAckFuture` values. It enqueues at
`natsclient/client.go:1134-1152`, waits on each exact `Ok`/`Err` channel with the
supplied context at lines 1154-1185, and records that already-enqueued publishes
continue after a canceled wait at lines 1125-1133.

No production caller of the public async methods, completion accessors, or
batch helper was found:

```text
rg -n 'PublishToStreamAsync(WithMsgID)?\(' \
  --glob '*.go' --glob '!**/*_test.go' --glob '!natsclient/client.go'

rg -n 'PublishAsyncComplete\(|PublishBatchToStream\(' \
  --glob '*.go' --glob '!**/*_test.go' --glob '!natsclient/client.go'
```

Both searches return no output. The declarations and internal implementation
remain in `natsclient/client.go`.

Current callers are unit and real-NATS integration tests under
`natsclient/publish_async_test.go`,
`natsclient/publish_async_integration_test.go`, and
`natsclient/stream_capacity_circuit_integration_test.go`.

### Explicit package goroutines and timers

The production package search is:

```text
rg -n '^\s*go\b|time\.AfterFunc\(' natsclient \
  --glob '*.go' --glob '!**/*_test.go'
```

It finds only:

- `natsclient/heartbeat.go:88`
- `natsclient/stream.go:400,499`
- `natsclient/jetstream_metrics.go:345`
- `natsclient/client.go:289,486,671,1463,1466,1483,1486,1512,1547,1585`

Synchronous timer waits also exist at
`natsclient/client.go:364,682`,
`natsclient/request.go:169,548`,
`natsclient/storage_inventory.go:298`, and
`natsclient/storage_report_consumer.go:195`.

### Client connection attempt

`Client.Connect` is at `natsclient/client.go:470-559`.

- It rejects only circuit-open status at lines 472-476.
- It sets connecting status and starts a goroutine around blocking
  `nats.Connect` at lines 478-508.
- The goroutine assigns `m.conn` at lines 493-495 and `m.js` at lines 501-504.
- The caller selects between the result and `ctx.Done()` at lines 510-534.
- The cancellation branch returns without canceling or joining the connection
  goroutine at lines 528-533.
- `connectDone` is buffered, so the goroutine can assign `m.conn` and `m.js`
  and send its result after `Connect` has returned.
- A late successful connection does not execute the outer success path at
  lines 536-555; therefore that connection can exist while status remains
  disconnected or circuit-open and without that attempt starting health or
  metrics monitoring.

Repeated calls are currently admitted:

- `Connect` does not test `m.closed`, connected status, an existing `m.conn`, or
  an already-running attempt.
- A later successful attempt can overwrite `m.conn` and `m.js` at lines
  493-504.
- Each ordinary successful call restarts health monitoring at lines 541-545.
  `startHealthMonitoring` first calls `stopHealthMonitoring` at
  `natsclient/client.go:1572-1583`.
- Each successful call assigns a new `metricsCancel` at
  `natsclient/client.go:547-550`; the previous metrics cancel is not invoked
  there.

Terminal close behavior is at `natsclient/client.go:561-597`.

- `closed` is tested and set only by `Close` at lines 563-570.
- Close signals health stop, stops the retained connection-loss timer, invokes
  the currently retained metrics cancel, and drains the currently visible
  connection at lines 572-587.
- A connection attempt still running during Close can assign `m.conn` and
  `m.js` after Close returns.
- A later `Connect` call is admitted after `closed` becomes true.
- A subsequent `Close` returns immediately at lines 567-568, so a connection
  installed by Connect-after-Close is not reached by that terminal Close path.

No focused test was found for canceled-connect late mutation, overlapping
Connect calls, or Connect-after-Close. `NewTestClient` scopes its ordinary
connection attempt with a timeout at `natsclient/test_client.go:776-801`.

### Production-compiled shared test-client root

`natsclient/test_client.go` is an ordinary production-compiled `.go` file even
though its exported surface is test infrastructure.

`NewSharedTestClient` calls:

```go
return newTestClient(context.Background(), productionTestClientFactoryDeps, opts...)
```

at `natsclient/test_client.go:846-849`.

`newTestClient` receives that root at
`natsclient/test_client.go:607-640`. Individual container start, host lookup,
port observation, Connect, readiness, and resource operations derive bounded
contexts from it:

- default bounds:
  `natsclient/test_client.go:596-604`;
- container start:
  `natsclient/test_client.go:700-713`;
- host lookup:
  `natsclient/test_client.go:716-729`;
- Connect and WaitForConnection:
  `natsclient/test_client.go:758-801`;
- resource setup:
  `natsclient/test_client.go:803-839`.

Thus the Background root reaches the same `Client.Connect` implementation
inventoried above, while each present setup operation has its own timeout.

`NewTestClient` instead passes `t.Context()` at
`natsclient/test_client.go:852-869`.

The repository contains 36 `NewSharedTestClient` calls: 34 qualified selector
calls and two unqualified same-package calls. They are test/integration
consumers, including TestMain owners at:

- `component/main_test.go:17-22`
- `graph/clustering/main_test.go:20-25`
- `processor/agentic-tools/tools_integration_test.go:30-38`
- `service/main_test.go:20-25`
- `storage/objectstore/store_integration_test.go:33-40`

Additional lifecycle and integration consumers are found by:

```text
rg -n 'NewSharedTestClient\(' --glob '*.go'
```

No production runtime caller outside tests was found.

### Health monitoring

`startHealthMonitoring` creates a ticker and `healthDone` channel at
`natsclient/client.go:1572-1583`, then starts its worker at lines 1585-1623.

The worker can:

- read the current connection and call `RTT` at lines 1594-1605;
- mutate connection status at lines 1608-1613;
- invoke `onHealthChange` at lines 1615-1618.

`stopHealthMonitoring` stops the ticker, closes `healthDone`, and clears both
fields at lines 1626-1639. It retains no completion signal and does not wait for
the worker. A worker already processing a tick can therefore update status or
invoke the callback after `stopHealthMonitoring`, repeated Connect, or Close
returns.

### JetStream metrics poller

`jetstreamMetrics.startPoller` derives a cancelable context and starts its
ticker worker at `natsclient/jetstream_metrics.go:336-360`. It returns only the
cancel function and retains no completion signal. An in-flight `updateStats`
can continue after cancellation and mutate Prometheus collectors through
`natsclient/jetstream_metrics.go:252-333`.

`Connect` starts this poller from `context.Background()` and stores its cancel
at `natsclient/client.go:547-550`. Close invokes the currently stored cancel at
lines 578-581 without joining the worker. Repeated Connect overwrites that
stored cancel without first canceling the preceding poller.

### Drain goroutine

`drainAndCloseConnection` starts a goroutine calling `m.conn.Drain()` at
`natsclient/client.go:655-673`.

The caller can return from the drain wait because:

- the drain result arrives at lines 676-681;
- `time.After(drainTimeout)` fires at lines 682-687; or
- the supplied context ends at lines 688-690.

The drain goroutine has no cancellation or join. On timeout or cancellation it
can remain inside `Drain` and later send to the buffered `drainDone` channel
after `drainAndCloseConnection` has closed and cleared `m.conn` at lines
693-695. The goroutine captures `m.conn` through the receiver rather than an
operation-local connection value.

### Circuit and connection-loss timers

`recordFailure` schedules `time.AfterFunc(currentBackoff, m.testCircuit)` at
`natsclient/client.go:270-290`. The timer handle is not retained. `testCircuit`
can mutate status at lines 349-359 and does not inspect `m.closed`.

The connection-loss watchdog is retained in `m.lossTimer` and guarded by
`lossTimerMu` at `natsclient/client.go:1490-1525`. Reconnect and Close stop the
retained timer through `cancelConnectionLossTimer` at lines 1475, 575-576, and
1528-1536. `Timer.Stop` does not join a callback already running. The callback
re-reads `onConnectionLost`, checks `!m.closed`, and invokes the external
callback at lines 1517-1524.

### NATS event callbacks

NATS connection callbacks start detached external callback goroutines:

- disconnect: `natsclient/client.go:1453-1470`;
- reconnect: `natsclient/client.go:1472-1488`;
- closed: `natsclient/client.go:1539-1548`.

The spawned `onDisconnect`, `onReconnect`, and `onHealthChange` calls have no
retained completion or join. Their external mutations can continue after the
NATS event handler or Client Close returns.

### Consumer-handle cleanup workers

The internal consumer acquisition worker at
`natsclient/stream.go:397-405` waits on the exact native handle's `Closed`
channel, then removes generic observation and releases its claim.

The port-backed acquisition worker at `natsclient/stream.go:497-504` waits on
the exact native handle's `Closed` channel, then removes generic and policy
observation and releases its claim.

These workers are not joined by Client or by the acquisition call. Their
current owner signal is the exact caller-owned native `ConsumeContext.Closed`
channel. Their map and metric mutations happen after the acquisition method
returns and after the native closed signal arrives.

### Operation-owned joined worker

`ConsumeWithHeartbeat` starts its work function at
`natsclient/heartbeat.go:74-90`. Heartbeat failure and caller cancellation
cancel the work context and wait for the `done` result before returning at
lines 97-108 and 137-143. Its method contract states that cancellation and
heartbeat failure wait for work exit at lines 60-73.

### Caller-owned Run loops

`StorageInventoryCollector.Run` and `StorageReportConsumer.Run` contain ticker
loops but do not start goroutines internally:

- `natsclient/storage_inventory.go:291-309`
- `natsclient/storage_report_consumer.go:178-197`

Their current production owner starts both under a wait group at
`service/storage_observability.go:287-307` and waits for `loopDone` during Stop
at lines 314-334.

## NATS slog producer, reader, documentation, and test census

### Producer and existing async primitive

`NATSLogHandler` currently accepts only the synchronous interface
`PublishToStream(context.Context, string, []byte) error` at
`pkg/logging/nats_handler.go:12-17`.

`Handle`:

- ignores the supplied slog context at `pkg/logging/nats_handler.go:61-62`;
- emits `logs.{level}.{source}` at lines 86-88;
- starts one goroutine per accepted record at lines 90-96;
- publishes with `context.Background()` and silently drops the returned error.

The repository already exposes an async JetStream primitive:

- `Client.PublishToStreamAsync`:
  `natsclient/client.go:1000-1017`;
- `Client.PublishToStreamAsyncWithMsgID`:
  `natsclient/client.go:1019-1027`;
- async enqueue, future, trace, circuit, and error behavior:
  `natsclient/client.go:1029-1092`;
- `PublishAsyncComplete`:
  `natsclient/client.go:1094-1105`.

`NATSLogHandler` does not currently consume that async primitive because its
`NATSPublisher` interface contains only the synchronous operation.

The shipped `LOGS` stream owns `logs.>` with one-hour age, 100 MB bound, file
storage, and discard-old behavior at `config/streams.go:108-118`.

The only production `NewNATSLogHandler` composition is
`internal/bootstrapobservability/bootstrap.go:259-285`.
`PhaseALogging.Steady` composes local, metric, and optional destination handlers
at `internal/bootstrapobservability/bootstrap.go:123-130`.

### Current readers

A production search for a direct `logs.>` NATS subscription is empty:

```text
rg -n 'Subscribe(Sync)?\([^\n]*logs\.>' \
  --glob '*.go' --glob '!**/*_test.go'
```

Current direct subscribers are integration tests at:

- `cmd/semstreams/bootstrap_observability_integration_test.go:79-95`
- `cmd/semstreams/bootstrap_observability_integration_test.go:157-217`

`MessageLogger` is a generic message observer rather than a dedicated log
reader:

- explicit and registry-discovered subjects:
  `service/message_logger.go:24-45,97-114,692-780`;
- generic entry schema:
  `service/message_logger.go:138-150`;
- raw-message projection:
  `service/message_logger.go:970-1003`;
- `/entries` HTTP surface:
  `service/message_logger_http.go:55-96,245-284`.

An explicit `monitor_subjects: ["logs.>"]` can therefore capture LOGS records,
but `MessageLogEntry` has no first-class `level` or `message` field; the
published record remains in `raw_data`, and its subject shares the same circular
buffer as other observed messages.

Documentation-only component examples also show raw `logs.>` consumption at:

- `output/file/doc.go:193-204`
- `output/websocket/doc.go:145-157`
- `output/websocket/README.md:275`

No shipped production configuration using those examples was found.

Open issue #1003 records the current operator-facing gap after #997 removed
`GET /flowbuilder/flows/{id}/runtime/logs` and the
`/flowbuilder/status/stream` WebSocket `log_entry` path. The issue records that
operators currently rely on centralized logs and must use direct NATS,
explicitly configure MessageLogger, or lose the centralized read path:
https://github.com/C360Studio/semstreams/issues/1003

### Subject documentation

The implementation and README document level-first order:

- `pkg/logging/nats_handler.go:19-23,86-88`
- `pkg/logging/README.md:84-100,141-165,251-260`

Package documentation instead states source-first order and gives source-first
examples at:

- `pkg/logging/doc.go:40-65`
- `pkg/logging/doc.go:87-100`
- `pkg/logging/doc.go:140-150`

Open issue #1002 records that an adopter following `doc.go` can create a valid
subscription that silently matches zero emitted records:
https://github.com/C360Studio/semstreams/issues/1002

### Tests

Handler tests use a synchronous mock at
`pkg/logging/nats_handler_test.go:15-42`. Subject, payload, filtering, source,
attributes, and concurrency are covered at lines
44-133, 142-397, and 414-440. Async completion in these tests is observed with
fixed sleeps at lines 153-154, 185-186, and 434-435; the tests do not expose a
handler-owned completion or publish error.

The existing async Client primitive is covered by:

- unit guards:
  `natsclient/publish_async_test.go:13-109`;
- pipeline and drain:
  `natsclient/publish_async_integration_test.go:19-67`;
- per-subject ordering:
  `natsclient/publish_async_integration_test.go:77-121`;
- message-ID deduplication:
  `natsclient/publish_async_integration_test.go:148-190`;
- trace/message-ID headers:
  `natsclient/publish_async_integration_test.go:259-300`;
- circuit reset:
  `natsclient/publish_async_integration_test.go:318-355`.

Bootstrap integration proves effective forwarding policy, stream-before-handler
ordering, and the emitted level-first subject at
`cmd/semstreams/bootstrap_observability_integration_test.go:45-98`. It also
proves that same-client NATS diagnostic logging is not recursively forwarded at
lines 145-219.

## Registry heartbeat concurrency census

`Registry` retains one `heartbeatCancel` and no completion signal at
`component/registry.go:147-162`.

`StartHeartbeat`:

- derives a child context at `component/registry.go:1239-1243`;
- overwrites `heartbeatCancel` under `r.mu` at lines 1244-1246;
- starts the ticker worker at lines 1248-1261;
- returns without waiting for an initial publication.

`StopHeartbeat` reads the retained cancel and calls it at
`component/registry.go:1264-1273`. It does not clear the field or wait for the
worker.

Current semantics visible from those operations are:

- repeated Stop invokes the same cancel again;
- sequential Start after Stop installs a new cancel and worker;
- Start while a prior worker remains active overwrites the only retained cancel;
- two concurrent Starts can both create workers, with only the last stored
  cancel reachable through Stop;
- concurrent Start and Stop can cancel whichever generation Stop observes;
- Stop return means cancellation was signaled, not that the ticker worker or an
  in-progress `republishAllCapabilities` completed.

The ticker worker republishes the declaration snapshot through
`republishAllCapabilities` at `component/registry.go:1253-1259,1275-1290`.
Ordinary admission publication separately starts a detached Background-rooted
publish at `component/registry.go:1197-1210`.

The current production manager starts heartbeat once at
`service/component_manager.go:433-444` and calls Stop during cleanup at
`service/component_manager.go:715-724`.

The only focused heartbeat test starts once, defers one Stop, sleeps 250 ms, and
asserts at least one stored announcement at
`component/registry_integration_test.go:114-134`. It does not assert cessation,
join, repeated Start, repeated Stop, concurrent Start/Stop, or parent-cancel
semantics.

No current component-capability spec or ADR claim for Start/Stop concurrency was
found:

```text
rg -n 'StartHeartbeat|StopHeartbeat|Registry.*heartbeat|capabilit.*heartbeat' \
  openspec/specs docs/adr
```

The current exported comments claim only “starts periodic republishing” and
“stops the heartbeat goroutine” at `component/registry.go:1239,1264`.

## JetStream metric contracts and current consumer families

`jetstreamMetrics` currently owns:

- stream message, byte, and active-state gauges:
  `natsclient/jetstream_metrics.go:13-20,52-71`;
- consumer pending gauge and delivered, acknowledged, and redelivered counters:
  `natsclient/jetstream_metrics.go:21-28,73-100`;
- the three policy gauges:
  `natsclient/jetstream_metrics.go:26-31,101-112`;
- operation-error counter:
  `natsclient/jetstream_metrics.go:29-31,114-120`.

The exact policy families are:

```text
semstreams_jetstream_consumer_max_ack_pending_requested
semstreams_jetstream_consumer_max_ack_pending_effective
semstreams_jetstream_consumer_max_ack_pending_observation_available
```

All three use labels `component`, `port`, `stream`, `consumer`, and
`policy_source`; `policy_source` is `port`, `component`, or `server`. The
binding requirement is at
`openspec/specs/jetstream-consumer-policy/spec.md:87-105`.

The active change preserves these three families, freshness behavior, and
exact-native-Closed cleanup at
`openspec/changes/simplify-one-shot-lifecycle-ownership/specs/jetstream-consumer-policy/spec.md:91-111`.

Current observation behavior is:

- initial policy registration sets requested, effective, and availability at
  `natsclient/jetstream_metrics.go:166-184`;
- forgetting a policy removes all three families at lines 186-206;
- refresh failure retains requested, removes effective, and sets availability
  to zero at lines 310-327;
- refresh recovery restores effective and availability at lines 328-332.

Current producer families are populated from:

- stream creation/acquisition:
  `natsclient/stream.go:183-203`,
  `natsclient/client.go:858-901,1197-1232`;
- internal consumer acquisition and exact-Closed cleanup:
  `natsclient/stream.go:350-405`;
- port-backed consumer and policy acquisition and exact-Closed cleanup:
  `natsclient/stream.go:448-504`;
- direct OTEL consumer policy observation:
  `natsclient/consumer_policy.go:139-160`,
  consumed as a method value at `output/otel/component.go:358`.

Operational documentation directs operators to the exact policy families at
`docs/advanced/11-jetstream-tuning.md:395-406`. The original design enumerates
their exact names, labels, bounded cardinality, and consumer-policy recorder
ownership at `docs/proposals/gh963-max-ack-pending-design.md:780-812,1529-1542`.

Binding tests include:

- exact family names, values, shared collector identity, and absence of
  queue/drop additions:
  `natsclient/jetstream_metrics_test.go:16-79`;
- stale effective removal, recovery, and cleanup:
  `natsclient/jetstream_metrics_test.go:99-142`;
- in-flight refresh versus exact cleanup:
  `natsclient/jetstream_metrics_test.go:144-180`;
- generic stream and consumer families plus native-Closed removal:
  `natsclient/integration_test.go:345-400`;
- observation-before-delivery and identity-complete initial log:
  `natsclient/consumer_policy_test.go:75-113`;
- initial Info failure before delivery:
  `natsclient/consumer_policy_test.go:115-135`;
- real-NATS effective values:
  `natsclient/consumer_policy_integration_test.go:15-59`;
- durable in-place policy update:
  `natsclient/consumer_policy_integration_test.go:61-133`.

Generic family consumers and operational references also remain current:

- bootstrap tests require JetStream metric registration under the canonical
  registry key `jetstream.stream_messages` at
  `internal/bootstrapobservability/bootstrap_test.go:83-98,100-120`;
- ADR-072 records `consumer_pending_messages` rising from 0 to 87k as the
  observed graph-ingest backlog signal at
  `docs/adr/072-keyed-concurrent-entity-ingest.md:84-92`;
- ADR-072 distinguishes that stream backlog from delivered-unacked admission at
  `docs/adr/072-keyed-concurrent-entity-ingest.md:172-180`;
- graph-ingest code names rising `consumer_pending_messages` as the companion
  backlog signature for ingest lag at
  `processor/graph-ingest/component.go:222-227`.

These references consume the generic stream/consumer families independently of
the exact three-gauge consumer-policy contract.

## Embedding classifier interfaces, callers, and lifecycle ordering

The current exported `graph/query` interface is:

```go
type Embedder interface {
    Embed(text string) ([]float32, error)
    EmbedBatch(texts []string) ([][]float32, error)
}
```

It is defined at `graph/query/classifier_embedding.go:13-23`; neither operation
accepts context.

The BM25 adapter calls `Generate(context.Background(), ...)` at
`graph/query/classifier_embedding.go:38-56`.

`NewEmbeddingClassifier`:

- accepts no context and returns no error at
  `graph/query/classifier_embedding.go:58-63`;
- performs eager batch embedding at lines 80-95;
- ignores the batch error by attaching vectors only when `err == nil`;
- returns the classifier at lines 98-103.

`FindBestMatch` accepts context at lines 105-109 and checks it before and after
calling `embedder.Embed`, but the embedding call itself has no context at lines
120-150. `UpgradeVectors` also has no context and calls `EmbedBatch` at lines
186-233.

The complete production constructor-name census is:

- graph gateway:
  `gateway/graph-gateway/component.go:513`;
- governance injection filter:
  `processor/agentic-governance/injection_classifier.go:87`;
- measurement command:
  `cmd/measure-injection-classifier/main.go:105`.

Graph-gateway ordering is:

1. parse/default/validate configuration:
   `gateway/graph-gateway/component.go:313-335`;
2. load files and build the embedding classifier:
   `gateway/graph-gateway/component.go:337-344,439-527`;
3. resolve ports:
   `gateway/graph-gateway/component.go:345-364`;
4. `Initialize` performs validation only:
   `gateway/graph-gateway/component.go:640-657`;
5. `Start(ctx)` acquires lifecycle authority:
   `gateway/graph-gateway/component.go:665-692`;
6. listener, readiness, inference API, handlers, and server are started:
   `gateway/graph-gateway/component.go:694-753`;
7. runtime fields and running state are committed:
   `gateway/graph-gateway/component.go:755-779`.

Governance ordering is:

1. parse, validate, and resolve ports:
   `processor/agentic-governance/component.go:75-113`;
2. build the filter chain:
   `processor/agentic-governance/component.go:123-127`,
   `processor/agentic-governance/filter_chain.go:227-301`;
3. injection corpus loading and classifier construction:
   `processor/agentic-governance/injection_classifier.go:51-101`;
4. discover the first PII filter and every tool-governance sibling:
   `processor/agentic-governance/component.go:129-157`;
5. wire the live PII sibling into tool filters:
   `processor/agentic-governance/component.go:158-161`;
6. optionally append the legacy tool-governance filter:
   `processor/agentic-governance/component.go:164-170`;
7. construct violation handling and return the component:
   `processor/agentic-governance/component.go:172-191`;
8. `Initialize` is a no-op:
   `processor/agentic-governance/component.go:215-218`;
9. `Start(ctx)` acquires lifecycle authority and rollback state:
   `processor/agentic-governance/component.go:220-264`;
10. input consumers are acquired:
    `processor/agentic-governance/component.go:266-271`;
11. running state commits:
    `processor/agentic-governance/component.go:273-284`.

Thus classifier construction and PII/tool sibling wiring currently occur before
either component receives Start context or acquires runtime consumers.

### Same-class context-aware embedding interface

The repository already has a separate context-aware embedding interface at
`graph/embedding/embedder.go:9-56`:

```go
type Embedder interface {
    Generate(ctx context.Context, texts []string) ([][]float32, error)
    GenerateQuery(ctx context.Context, texts []string) ([][]float32, error)
    Dimensions() int
    Model() string
    Close() error
}
```

Current implementations accept context:

- BM25 document generation:
  `graph/embedding/bm25_embedder.go:129`;
- BM25 query generation:
  `graph/embedding/bm25_embedder.go:121`;
- HTTP document generation:
  `graph/embedding/http_embedder.go:145`;
- HTTP query generation:
  `graph/embedding/http_embedder.go:152`.

`graph/query.bm25Adapter` wraps the BM25 implementation but replaces that
context-aware input with `context.Background()` at
`graph/query/classifier_embedding.go:38-56`.

`EmbeddingClassifier.UpgradeVectors` is declared at
`graph/query/classifier_embedding.go:186-233`. Its only measured callers are
tests at `graph/query/classifier_embedding_test.go:401-526,818`. The production
call search is empty:

```text
rg -n '\.UpgradeVectors\(' --glob '*.go' --glob '!**/*_test.go'
```

## Fusion watch, freshness, Close, and caller census

The binding Fusion specification requires preservation of
`New(requester, timeout)`, optional `Close`, lazy
`GRAPH_STATUS/graph-index` readiness, and the six retrieval methods at
`openspec/specs/fusion/spec.md:340-379`.

The readiness specification requires:

- `Get` for point probes and `Watch` for held event state:
  `openspec/specs/graph-index-readiness/spec.md:279-304`;
- consumer-local arrival freshness, a 3× heartbeat window, fail-closed unknown,
  and immediate current delivery:
  `openspec/specs/graph-index-readiness/spec.md:312-333`.

ADR-083 records the same watch/state decision and rejects per-decision polling
at `docs/adr/083-readiness-as-distributed-state.md:55-110`. Its recorded costs
include one bounded first wait and held state at lines 144-149.

Current `fusionnats.Client`:

- stores lazy watch state at `pkg/fusion/fusionnats/client.go:46-65`;
- preserves `New(requester, timeout)` at lines 70-81;
- defines optional, idempotent `Close`, which calls the watcher's joining Stop,
  at lines 84-105;
- lazily binds on first Status and starts the watch with
  `context.WithoutCancel(ctx)` at lines 107-146;
- spends one bounded first-delivery wait at lines 149-169;
- reads held freshness and fails closed on unknown at lines 171-233.

Current SemStreams callers are:

- production/E2E:
  `test/e2e/scenarios/validate_batch_read.go:343-344`, which defers `Close`;
- unit status helper:
  `pkg/fusion/fusionnats/client_test.go:167-173`, which registers `Close`;
- real-NATS integration:
  `pkg/fusion/fusionnats/client_integration_test.go:116-145`, which calls Status
  without registering `Close`;
- other direct test constructors at
  `pkg/fusion/fusionnats/client_test.go:384-814` and
  `pkg/fusion/fusionnats/slice_e_contract_test.go:21-97`; calls that never use
  Status do not bind a watcher.

The read-only sister-repository census finds 13 `fusionnats.New` uses:

- production:
  `../semsource/processor/code-context/component.go:135`;
- test/integration:
  `../semsource/processor/code-context/scope_integration_test.go:101`;
  `../semsource/internal/governance/producer_e2e_integration_test.go:107`;
  `fusion_gateway_integration_test.go:75,223`;
  `staleness_lifecycle_integration_test.go:150`;
  `doc_body_storeregistry_integration_test.go:164`;
  `multi_source_lineage_integration_test.go:192`;
  `doc_tail_retrieval_integration_test.go:356`;
  `supersession_demote_integration_test.go:96`;
  `go_callgraph_impact_integration_test.go:150`;
  `multilang_reference_resolution_integration_test.go:103`;
  `python_call_graph_integration_test.go:94`.

No `fusionnats.Client.Close` call was found in that sister repository. Its
production component stores the concrete client behind
`fusion.RetrievalClient` at
`../semsource/processor/code-context/component.go:133-143`; that interface does
not contain `Close` at `pkg/fusion/retrieval.go:17-45`.

## Current open issues adjacent to next-tag evidence

The following issues are open as of current HEAD evidence:

- #1004, runtime message observations:
  `service/flow_runtime_messages.go:63-79` dereferences `fs.serviceMgr` without
  a nil guard. The sibling health path treats nil as an anticipated state at
  `service/flow_runtime_health.go:312-328`.
  https://github.com/C360Studio/semstreams/issues/1004
- #1008, create-flow error classification:
  precise invalid errors originate at `flowstore/flow.go:60-88`, pass through
  `flowstore/manager.go:48-60`, and are collapsed into an opaque 500 at
  `service/flow_service.go:250-261`.
  https://github.com/C360Studio/semstreams/issues/1008
- #1009, server-owned creation timestamp:
  `flowstore.Manager.Update` loads the current flow at
  `flowstore/manager.go:105-130`, then rewrites version, `UpdatedAt`, and
  `LastModified` without restoring `CreatedAt` at lines 132-145.
  https://github.com/C360Studio/semstreams/issues/1009
- #1010, list/delete race:
  `flowstore.Manager.List` snapshots keys then aborts on any failed Get at
  `flowstore/manager.go:163-180`; the HTTP handler special-cases only an error
  string and otherwise emits 500 at `service/flow_service.go:237-247`.
  https://github.com/C360Studio/semstreams/issues/1010

Issue #1007 is also open. It records rule/action correctness facts at
`processor/rule/message_handler.go:36-40,192-194`,
the absence of the gate in `processor/rule/stateful_evaluator.go`, and the
`submit_work` category/description references at
`processor/agentic-tools/categories.go:12,38` and
`processor/agentic-tools/emit_diagnosis.go:47,73`. It is adjacent correctness
evidence and does not currently touch FlowService, flowstore, or the NATS
lifecycle surfaces inventoried above:
https://github.com/C360Studio/semstreams/issues/1007

### #1005 run-level accounting fact gap

Issue #1005 is open:
https://github.com/C360Studio/semstreams/issues/1005

Current per-loop facts are present:

- vocabulary names:
  `vocabulary/agentic/predicates.go:425-438`;
- vocabulary registration:
  `vocabulary/agentic/register.go:455-465`;
- completion triples:
  `processor/agentic-loop/graph_writer.go:541-582`;
- failure triples:
  `processor/agentic-loop/graph_writer.go:585-625`;
- price-based cost computation:
  `processor/agentic-loop/graph_writer.go:680-704`;
- loop-to-run entity reference:
  `vocabulary/agentic/predicates.go:471-482`;
- in-memory trajectory totals:
  `agentic/trajectory.go:59-72`;
- completion and failure event totals:
  `processor/agentic-loop/handlers.go:2002-2003,2583-2584`.

No `agent.run.tokens-in`, `agent.run.tokens-out`, or `agent.run.cost-usd`
predicate exists in the current vocabulary search. Current `agent.run.*`
surfaces include the loop-side run entity link and agentrun lifecycle/audit
predicates, including:

- `vocabulary/agentic/predicates.go:471-482`
- `agentic/agentrun/agentrun.go:62-77,106-123`

The issue records two external product consumers seeking a chain/run-level
accounting fact. This inventory records the present per-loop/run-link seam and
the absent run-level aggregate only.

### #1006 trajectory reference/dereference collision

Issue #1006 is open:
https://github.com/C360Studio/semstreams/issues/1006

Current write and reference facts are:

- `TrajectoryFactV1` carries optional `message.StorageReference` but no body at
  `agentic/trajectory_fact.go:143-173`;
- the full-evidence Store contract, digest key, exact instance, size, key, and
  content type are binding at
  `openspec/specs/agentic-loop/spec.md:313-336`;
- agentic-loop resolves the configured Store per operation through the shared
  registry at
  `openspec/specs/agentic-loop/spec.md:327-332`;
- the configured logical instance defaults to `objectstore` at
  `processor/agentic-loop/config.go:48-57,374-385`.

Current read and public projection facts are:

- a trajectory page explicitly carries fact metadata and references, never
  evidence bodies, at `agentic/trajectory_query.go:34-43`;
- the agentic-loop reader is normatively prohibited from Store resolution or
  hydration at `openspec/specs/agentic-loop/spec.md:449-455,505-510`;
- GraphQL is the sole public trajectory application surface at
  `openspec/specs/gateway-response-projection/spec.md:184-203`;
- GraphQL is required to expose reference metadata without hydration, while
  authorized retrieval is stated to be a separate registered-Store operation
  outside that public query contract, at
  `openspec/specs/gateway-response-projection/spec.md:191-210`;
- the GraphQL Query root exposes `trajectory` at
  `gateway/graph-gateway/component.go:1835-1848`;
- its `StorageReference` type exposes only
  `storage_instance`, `key`, `content_type`, and `size` at
  `gateway/graph-gateway/component.go:1871-1877`;
- `TrajectoryFact.evidence` is that metadata type at
  `gateway/graph-gateway/component.go:1917-1930`;
- the `/mcp` handler remains a stub response at
  `gateway/graph-gateway/component.go:2248-2265`;
- the production HTTP-handler search for object/content/evidence/artifact/blob
  is empty:

```text
rg -n 'HandleFunc\(.*(object|content|evidence|artifact|blob)' \
  --glob '*.go' --glob '!**/*_test.go'
```

The shared live resolver is currently private ComponentManager state at
`service/component_manager.go:50-65`. It is populated and passed to managed
components through `component.Dependencies.StoreRegistry` at
`service/component_manager.go:1047-1106` and
`component/dependencies.go:98-109`.

ADR-063 states that `StorageReference.StorageInstance` is a logical live owner
name, not a bucket or address, and therefore cannot be reconstructed by a
reference holder at `docs/adr/063-store-substrate-and-resolver.md:84-106`.
It records per-fetch exact-name registry resolution at lines 224-249.

When the configured provider is unavailable, current Start records the
provider-resolution audit failure but returns nil at
`processor/agentic-loop/component.go:789-819`. Current Health reports degraded
at `processor/agentic-loop/component.go:396-427`.

The current collision is therefore factual:

- the write path produces a durable, exact registered-Store reference;
- the binding trajectory read contracts deliberately return that reference
  without its body;
- the same binding spec names a separate authorized registered-Store operation;
- no public non-Go dereference operation is currently present;
- an external browser adopter receives a logical instance/key pair but cannot
  derive the live Store handle from it.

This inventory records no release disposition for these open issues.

## PR990 overlap

The boot-only PR990 target fixes composition at boot and removes live
replacement surfaces:

- `openspec/changes/require-restart-for-config-activation/proposal.md:14-38`

It explicitly receives no lifecycle, shutdown, recovery, release, archive, or
tag-readiness credit:

- `openspec/changes/require-restart-for-config-activation/proposal.md:60-75`

The current N1 task simultaneously records that consumer-policy and direct-port
observation remain independent mechanisms:

- `openspec/changes/simplify-one-shot-lifecycle-ownership/tasks.md:287-291`

The old native-surface inventory records
`ObserveDirectPortConsumerPolicy` as a retirement row at
`openspec/changes/require-restart-for-config-activation/native-surface-inventory.md:118`.

At the current baseline the exported method still exists, delegates to the
Client's internal observation operation, and returns cleanup for the metrics
record:

- `natsclient/consumer_policy.go:139-160`

The current OTEL component consumes the method as a method value at
`output/otel/component.go:358`; therefore an empty direct-call-expression
search is not an empty consumer search. The PR990 boot-only head
`8f19ef3678a549913385b090e4de1766a7a43a27` contains the equivalent consumer at
`output/otel/component.go:305`.

This is an overlap between an old exported-surface row, a live OTEL dependency,
and the newer requirement to preserve observation behavior. The inventory
makes no ruling about that overlap.

Two files in the current closeout working tree also differ on the PR990
boot-only head
`8f19ef3678a549913385b090e4de1766a7a43a27`.

For `natsclient/consumer_policy_callsite_test.go`:

- the current closeout working tree adds the `.claude` fixture test at
  `natsclient/consumer_policy_callsite_test.go:99-116` and adds `.claude` to
  the walker exclusion at lines 239-252;
- the PR990 head rewrites the production census and exported API expectations
  at `natsclient/consumer_policy_callsite_test.go:22-98`, including older
  error-only consumer signatures, and retains a walker that excludes only
  `.git` and `vendor` at lines 132-145;
- the PR990 head also changes its direct-consumer creation census at
  `natsclient/consumer_policy_callsite_test.go:100-129`.

For `test/testinfra/policy_baseline.json`:

- the current closeout working tree removes the four stale rows identified in
  this inventory;
- the PR990 head also removes the two stale Registry rows, retains the two
  stale `TestService_ConcurrentOperations` rows, and adds two gated-DAG sleep
  entries at `test/testinfra/policy_baseline.json:734-753`.

These are overlapping file changes at different repository states. This
inventory records the overlap without selecting merge content or a target
state.

## Adopter seam inventory

| Surface | External developer | Current knowledge required | If they do nothing | Current discovery |
|---|---|---|---|---|
| `Client.Connect` / `Close` | A service composing one `natsclient.Client` | Cancellation can return before `nats.Connect`; repeated Connect and Connect-after-Close are admitted; only the currently retained connection and metrics cancel are reached by Close | A canceled or closing attempt can later install `m.conn`/`m.js`; repeated calls can overwrite connection state and leave preceding workers active | Exported signatures and source at `natsclient/client.go:470-597`; no focused behavior test or package-level lifecycle statement was found |
| Native connection, JetStream, stream, and KV outputs | External framework/component code using core NATS operations | Native return type and the exact operation's caller-context contract | Existing native composition continues | Exported Go signatures and method comments |
| Exact consume handles | Component code consuming JetStream | Retain the exact returned handle, drain it, await `Closed`, and preserve callback authority through closure | Existing explicit native-handle ownership continues | `natsclient/stream.go:275-305,507-525` |
| Native `Subscription` | External component or service registering a core NATS callback/responder | Retain the exact returned handle; choose native Unsubscribe or contextual wrapper Drain; the acquisition context becomes callback ancestry | Native callbacks continue until the handle or transport closes; Client owns no subscription catalog | `natsclient/client.go:711-820`, `natsclient/request.go:341-429` |
| Typed `Subject[T]` subscription | External typed-message adopter | Same exact Subscription ownership as Client.Subscribe; current typed adapter drops decode and handler errors | Callback delivery and handle lifecycle remain native; ignored typed errors are not returned | `natsclient/typed.go:78-100` |
| `KVStore.Watch` / `KeyWatcher` | External KV state observer | Retain the native watcher, consume Updates, and call its native Stop | The watcher continues under the supplied context/native connection; Client and KVStore retain no watcher catalog | `natsclient/kv.go:593-601`, `flowstore/manager.go:183-187` |
| `PubAckFuture` | External asynchronous JetStream producer | Inspect each future's `Ok`/`Err`, or observe connection-global `PublishAsyncComplete`; enqueue context does not cancel an accepted native publish | Accepted publishes continue resolving after caller cancellation; unobserved futures still feed the connection async-error handler | `natsclient/client.go:1000-1105` |
| `NewSharedTestClient` | Repository test/integration owner without `testing.T` | The constructor creates a Background-rooted test factory, while present setup phases derive timeouts | Test setup is not canceled by a test context; its operation-level bounds and explicit Terminate remain the available ownership | `natsclient/test_client.go:607-640,700-849` |
| Registry heartbeat | Code invoking exported `StartHeartbeat`/`StopHeartbeat` outside ComponentManager | Current source exposes one retained cancel, no generation identity, and no completion/join contract | Serialized single Start/Stop follows the production caller; repeated or concurrent calls can leave a prior worker unreachable through Stop | Method comments at `component/registry.go:1239-1273`; no normative concurrent spec or focused concurrency test was found |
| NATS slog producer | Application code installing `NATSLogHandler` | Code emits `logs.{level}.{source}`; `Handle` returns before its Background-rooted synchronous publish finishes; publish errors are dropped | Forwarding remains fire-and-forget per record | Handler source and README; `pkg/logging/doc.go` currently gives the opposite subject order |
| Central log reader | An operator/UI consuming centralized logs | Direct NATS credentials and `logs.>` layout, or explicit MessageLogger configuration plus raw-record decoding | No first-class framework log reader is present after #997 | Issue #1003, LOGS stream config, generic MessageLogger `/entries`, and direct integration-test subscriptions |
| JetStream metric families | Prometheus/dashboard/operator consumers | Exact family names, five policy labels, three allowed policy sources, and unavailable/effective semantics | Existing dashboards and alert expressions continue against the current families | Current OpenSpec requirement, tuning guide, Prometheus exposition, and metric tests |
| `graph/query.Embedder` and `NewEmbeddingClassifier` | An external embedder or classifier caller | Embed/EmbedBatch have no context; constructor has no error; eager BM25 warmup errors are not returned | Cancellation cannot enter an embedder through this interface, and constructor callers cannot observe warmup failure | Exported interfaces and constructor source in `graph/query/classifier_embedding.go:13-103` |
| `fusionnats.New` / optional `Close` | A sister-repository component constructing a retrieval client | Status lazily binds a process-lived watch; optional Close stops and joins it | Long-lived clients retain one watch for process lifetime; short-lived Status users that do not Close retain it until process exit | Binding spec, constructor/Close comments, ADR-083, and sister-repository production caller; the production sister interface does not expose Close |
| FlowService/flowstore HTTP operations | UI or API client | Current runtime behavior includes the open #1004 and #1008-#1010 conditions | The client can receive a panic/no response, opaque 500, overwritten `created_at`, or a list 500 during concurrent deletion | Runtime routes, flowstore implementation, and linked open issues |
| Trajectory evidence reference | Browser or other non-Go trajectory consumer | The public surface reveals logical storage instance, digest key, content type, and size, but no live Store or dereference operation | The adopter can display reference metadata but cannot read the body through a current public SemStreams operation | `openspec/specs/gateway-response-projection/spec.md:184-210`, `gateway/graph-gateway/component.go:1871-1930` |
| Run-level accounting | Product rule author observing unattended agent chains | Current graph facts provide per-loop tokens/cost plus a run entity link, not a run aggregate | Rules can reason about individual loop spend but not total chain spend from one run entity | `processor/agentic-loop/graph_writer.go:541-625`, `vocabulary/agentic/predicates.go:425-482` |
| Production context-field guard | External adopter | Nothing | No external API consequence | Repository contract test at `test/contract/context_ownership_contract_test.go:19-308` |
| Scanner, baseline, and PR990 overlap | None outside repository development and release verification | Nothing | No adopter runtime behavior changes | Contract-test failures, policy baseline, active change artifacts, and PR overlap census |
