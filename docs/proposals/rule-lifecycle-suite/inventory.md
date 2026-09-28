# Inventory: rule processor lifecycle-suite adoption (#1410)

base: 29429020ce45a674c209644737061bc8fb1de231

Scope: test factory integration only. Inspection was read-only; no tests ran. Working tree was clean. This is inventory evidence, pending independent `INVENTORY PASS`; it contains no target state.

## 1. Claimed gap and existing suite

The gap is confirmed: three Go tests call the suite, none under `processor/rule`.

- `component/lifecycle_test_suite.go:16` — `type LifecycleFactory func() LifecycleComponent`
- `component/lifecycle_test_suite.go:21` — `func StandardLifecycleTests(t *testing.T, factory LifecycleFactory) {`
- `gateway/http/http_lifecycle_test.go:57` — `component.StandardLifecycleTests(t, createTestComponentForLifecycle)`
- `input/udp/udp_lifecycle_test.go:88` — `component.StandardLifecycleTests(t, createTestComponent)`
- `processor/graph-index/lifecycle_integration_test.go:78` — `component.StandardLifecycleTests(t, createTestComponentForLifecycle)`

The factory has no test argument, error return, or per-instance cleanup callback. It is called concurrently. A complete non-short run constructs **259 instances**: seven portable cases, two rejected-Start cases, 20 iterations across ten goroutines, and 50 leak-check iterations.

- `component/lifecycle_test_suite.go:41` — `{"Initialize", testInitialize},`
- `component/lifecycle_test_suite.go:42` — `{"ControlledStopWithLiveStartAuthority", testControlledStopWithLiveStartAuthority},`
- `component/lifecycle_test_suite.go:43` — `{"AcceptedStartParentCancellation", testAcceptedStartParentCancellation},`
- `component/lifecycle_test_suite.go:44` — `{"CompletedRepeatedStop", testCompletedRepeatedStop},`
- `component/lifecycle_test_suite.go:45` — `{"NilStartContext", testNilStartContext},`
- `component/lifecycle_test_suite.go:46` — `{"NilStopContext", testNilStopContext},`
- `component/lifecycle_test_suite.go:47` — `{"StopBeforeStart", testStopBeforeStart},`
- `component/lifecycle_test_suite.go:158` — `const iterations = 20`
- `component/lifecycle_test_suite.go:159` — `const concurrency = 10`
- `component/lifecycle_test_suite.go:215` — `const iterations = 50`

`ErrorPaths` checks pre-canceled and pre-expired Start and safe Stop afterward. `ParallelFreshInstances` exercises distinct objects, not concurrent lifecycle calls on the same object.

**Coverage limits:** Stop runs synchronously. A timeout context does not contain an implementation that ignores cancellation; the exact-error assertion is reached only after Stop returns. The abort case does not force the deadline to win. Repeated Stop checks nil results, not teardown side-effect counts. `NoLeaks` compares process-wide memory/goroutines, logs lifecycle failures, and does not enumerate server resources.

- `component/lifecycle_test_suite.go:85` — `stopErr = comp.Stop(stopCtx)`
- `component/lifecycle_test_suite.go:90` — `if stopCtx.Err() != nil {`
- `component/lifecycle_test_suite.go:91` — `require.ErrorIs(t, stopErr, stopCtx.Err(), "Stop must preserve its exact caller-context error when the bound wins")`
- `component/lifecycle_test_suite.go:229` — `t.Logf("Start failed on iteration %d: %v", i, err)`
- `component/lifecycle_test_suite.go:236` — `t.Logf("Stop failed on iteration %d: %v", i, err)`
- `component/lifecycle_test_suite.go:259` — `if growth > 50*1024*1024 {`
- `component/lifecycle_test_suite.go:266` — `if goroutineGrowth > 10 {`

## 2. Production construction and lifecycle

Both construction paths return the actual processor. The registered factory installs platform authority, decoder, and logger. Direct construction requires a separate platform setter for the NATS-backed state/executor path; omitting it can leave Start successful with degraded internal facilities because initialization errors are logged.

- `processor/rule/factory.go:17` — `Factory:     CreateRuleProcessor,`
- `processor/rule/factory.go:109` — `func CreateRuleProcessor(rawConfig json.RawMessage, deps component.Dependencies) (component.Discoverable, error) {`
- `processor/rule/factory.go:124` — `if deps.Platform.Org == "" || deps.Platform.Platform == "" {`
- `processor/rule/factory.go:136` — `processor, err := NewProcessorWithMetrics(deps.NATSClient, &ruleConfig, deps.MetricsRegistry)`
- `processor/rule/factory.go:144` — `processor.SetPlatform(deps.Platform)`
- `processor/rule/config.go:182` — `func NewConfig(packID string) (Config, error) {`
- `processor/rule/processor.go:610` — `if rp.natsClient != nil && (rp.platform.Org == "" || rp.platform.Platform == "") {`
- `processor/rule/processor.go:895` — `if err := rp.initializeStateTracker(ctx); err != nil {`
- `processor/rule/processor.go:896` — `rp.logger.Warn("Failed to initialize state tracker, stateful rules will be disabled", "error", err)`

Initialize loads configured rules. Start creates runtime authority, cache, state/schedule trackers, scheduler, configured watchers/subscriptions, readiness publisher, hot-reload manager, and revision sweeper. Scheduler construction occurs even with no cron rules.

Stop fences runtime commands and readiness, stops cron, drains inputs, stops and joins entity watchers, stops hot reload, closes the evaluation queue, cancels/joins runtime and status loops, and closes the cache.

- `processor/rule/processor.go:554` — `func (rp *Processor) Initialize() error {`
- `processor/rule/processor.go:838` — `func (rp *Processor) Start(ctx context.Context) (startErr error) {`
- `processor/rule/processor.go:997` — `runCtx, cancel := context.WithCancel(ctx)`
- `processor/rule/processor.go:1191` — `func (rp *Processor) Stop(ctx context.Context) error {`
- `processor/rule/processor.go:1271` — `stopErrors = append(stopErrors, cronScheduler.Stop(ctx))`
- `processor/rule/processor.go:1322` — `stopErrors = append(stopErrors, hotReloadMgr.Stop())`
- `processor/rule/processor.go:1333` — `for _, done := range []<-chan struct{}{statusLoopDone, runtimeDone} {`

Bounded context inventory: the inspected processor, watcher, config-manager, readiness, owner-lane, and cron files contain private cancellation fields and context-taking operations; the search found no `Background`, `TODO`, or `WithoutCancel` roots in those files. This is not a repository-wide context-ownership audit.

## 3. Resource and isolation inventory

| Resource | Empty default configuration | Isolation evidence |
|---|---|---|
| `RULE_STATE` | Open/create bucket; no configured rules to generate match-state writes | Fixed literal; pack ID does not rename it |
| `RULE_SCHEDULES` | Open/create bucket; no cron rules to fire or record | Fixed exported bucket constant |
| `GRAPH_STATUS/rule` | Readiness loop can write immediately and on heartbeats | Fixed bucket and fixed producer key; concurrent instances share this write target |
| `semstreams_config` | Open/create bucket; watch `rules.*`; initial replay sentinel can schedule reconciliation | With no definitions, seeding returns before writing; no pack-specific watch namespace |
| `ENTITY_STATES` | No watcher with default nil `EntityWatchBuckets` | Configured patterns activate real watchers; bucket remains fixed |
| Message stream/durable | None for the default KV input port | A configured JetStream input derives durable name from subject |
| Metrics | Nil registry is supported | Non-nil readiness gauges use package singleton state |

Exact evidence:

- `processor/rule/processor.go:619` — `const bucketName = "RULE_STATE"`
- `processor/rule/schedule_tracker.go:52` — `const ScheduleBucketName = "RULE_SCHEDULES"`
- `processor/rule/readiness.go:118` — `rp.refreshReadinessStatus(ctx)`
- `processor/rule/readiness.go:199` — `rp.statusPublisher = readiness.NewPublisher(bucket, readiness.KeyRule)`
- `graph/readiness/watcher.go:62` — `KeyRule = "rule"`
- `graph/readiness/publisher.go:102` — `if _, err := p.bucket.Put(putCtx, p.key, data); err != nil {`
- `processor/rule/kv_config_integration.go:150` — `return store.Watch(watchCtx, "rules.*")`
- `processor/rule/kv_config_integration.go:382` — `if len(defs) == 0 {`
- `processor/rule/kv_config_integration.go:581` — `kv, err := graph.EnsureCatalogBucket(ctx, natsClient, graph.BucketSemStreamsConfig)`
- `processor/rule/entity_watcher.go:34` — `if len(bucketPatterns) == 0 {`
- `processor/rule/entity_watcher.go:76` — `return rp.config.EntityWatchBuckets`
- `processor/rule/processor.go:1115` — `consumerName := fmt.Sprintf("rule-processor-%s", sanitizedSubject)`

`WithBucketPrefix` applies through TestClient helpers; the processor receives `tc.Client` and its fixed-name production acquisitions bypass that prefix. No per-client rewrite was found in the inspected client/options files.

- `natsclient/test_client.go:954` — `fullName := tc.BucketPrefix + name`
- `natsclient/test_client.go:965` — `return tc.Client.GetKeyValueBucket(ctx, fullName)`
- `natsclient/test_client.go:971` — `return tc.BucketPrefix + name`

The fixed readiness write is a measured sharing constraint even for an empty factory. The inventory does not establish independent server-state isolation across the suite's instances.

## 4. Existing substrate, cleanup, and closest pattern

The closest shape is graph-index's real-NATS lifecycle factory. Its package-wide TestMain is an existing exception-shaped arrangement, not automatic authorization to copy it.

- `processor/graph-index/lifecycle_integration_test.go:53` — `tc := sharedLifecycleNATSClient`
- `processor/graph-index/lifecycle_integration_test.go:68` — `comp, err := CreateGraphIndex(configJSON, deps)`
- `natsclient/test_client.go:856` — `testClient, err := newTestClient(t.Context(), productionTestClientFactoryDeps, opts...)`
- `natsclient/test_client.go:863` — `t.Cleanup(func() {`
- `natsclient/test_client.go:864` — `if err := testClient.Terminate(); err != nil {`

Rule already uses per-test `NewTestClient` and keeps NATS alive through processor Stop:

- `processor/rule/readiness_integration_test.go:172` — `tc := natsclient.NewTestClient(t, natsclient.WithKV())`
- `processor/rule/readiness_integration_test.go:198` — `require.True(t, tc.IsReady(), "NATS must remain live throughout Processor.Stop")`
- `processor/rule/readiness_integration_test.go:208` — `t.Cleanup(stopProcessor)`

The suite does not guarantee Stop after an assertion aborts or after parallel Initialize/Start failure. Factory cleanup ownership therefore remains a concrete fixture concern. Repeated completed Stop is supported; a second running-generation rejoin after a timed-out Stop is not.

Existing focused coverage remains separate:

- `processor/rule/lifecycle_runtime_test.go:30` — `func TestRuleReadinessCompletionSurvivesStopDeadline(t *testing.T) {`
- `processor/rule/lifecycle_runtime_test.go:182` — `func TestRuleStopDeadlineArmCancelsAndJoinsCoordinator(t *testing.T) {`
- `processor/rule/readiness_integration_test.go:171` — `func TestIntegration_RuleStopAfterAcceptedStartParentCancellation(t *testing.T) {`

## 5. Adjacent authority and consumer-at-birth

Current authority is `openspec/specs/component-lifecycle/spec.md`, read in full, and ADR-095. The spec requires caller-owned runtime lifetime, caller-bounded Stop, repeated completed Stop, and owner-specific failed-Start rollback. It excludes same-instance restart and running-generation rejoin guarantees.

The archived `2026-09-28-rule-bounded-stop-fence/design.md`, read in full, explicitly files this test-only adoption as a residual at line 110. Issue #1410 requests the existing suite with a real-NATS factory and excludes CronScheduler conformance. The inspected tracked proposal inventory contains no active proposal outside `archive/`.

No new exported symbol, bucket, subject, config field, communication path, or runtime primitive is requested. The present consumer is the new rule integration test calling the existing suite. A same-class primitive collision table and establishing-pattern adoption sweep are therefore not triggered by the requested test-only scope.

#1064, #1411, #1412, and #1293 remain outside the assigned scope; their completion is not claimed.

## 6. Adopter seam inventory

No external surface changes are requested. The affected adopter is a test author supplying a factory to the already-exported suite.

| Question | Current evidence |
|---|---|
| What must they know? | Return a fresh component; tolerate concurrent factory calls; keep NATS alive through Stop; supply platform authority; understand fixed resource names; arrange failure cleanup outside the factory signature |
| What happens by default? | Default rule config has no entity watcher, while readiness and hot-reload infrastructure still start. Missing platform can degrade startup through warnings |
| Where do they find out? | Factory type gives compile-time shape only; construction errors cover some dependencies; concurrency/resource/cleanup obligations require source inspection |
| What should they have to know? | The existing signature expresses freshness but not infrastructure lifetime or concurrent invocation. That gap is recorded here, with no public API change proposed |

## 7. Open evidence questions

1. Whether one fresh top-level test container with an empty-rule cohort satisfies the isolation policy despite shared `GRAPH_STATUS/rule` writes; no per-instance namespace mechanism was found.
2. Which configured runtime facilities the fixture will actually activate, and how degradation will be distinguished from successful setup.
3. How fixture cleanup will cover early suite failures without claiming a second-rejoin contract.
4. Actual warm-host duration, container count, and race results remain unmeasured.

## Search record

All searches ran in the named worktree unless otherwise stated. Repeated reads and identical repeated searches are collapsed.


Search: `git status --short`; `git rev-parse HEAD` — clean; baseline above.

Search: `git grep -n -e 'StandardLifecycleTests' -e '#1410' -e '1283' -- component processor openspec docs .agents` — suite, three callers, archived rationale and related records.

Search: `gopls workspace_symbol -matcher=fuzzy StandardLifecycleTests`

Search: `gopls workspace_symbol -matcher=fuzzy LifecycleFactory`

Search: `gopls workspace_symbol -matcher=fuzzy NewProcessorWithMetrics`

Search: `gopls workspace_symbol -matcher=fuzzy NewTestClient`

Search: `gopls workspace_symbol -matcher=fuzzy NewConfig` — these symbol searches returned no output; not accepted as absence evidence.

Search: `gopls references component/lifecycle_test_suite.go:21:6` — no output; test-call completeness checked by tracked search.

Search: `gopls references component/lifecycle_test_suite.go:16:6` — seven in-file type uses.

Search: `gopls references processor/rule/factory.go:109:6` — registration reference.

Search: `gopls references processor/rule/processor.go:261:6` — `NewProcessor` forwarding reference.

Search: `git grep -n -e 'StandardLifecycleTests' -e 'LifecycleFactory' -- '*.go'` — declarations/helpers and the three callers above.

Search: `git grep -n -e 'StandardLifecycleTests' -e 'LifecycleFactory' -- processor/rule` — zero.

Search: `git ls-files 'openspec/changes/*/proposal.md' ':!:openspec/changes/archive/**'` — zero.

Search: `git ls-files processor/rule '*lifecycle*' 'openspec/changes/*/proposal.md' 'docs/adr/*lifecycle*'` — file/proposal/ADR inventory.

Search: `git grep -n -e 'func TestMain' -e 'NewTestClient' -e 'NewSharedTestClient' -- processor/rule '*testutil*' natsclient/test*.go` — existing test-substrate uses.

Search: `git grep -n -e '^func ' -e 'context.Context' -e 'CancelFunc' -e 'GetKeyValueBucket' -e 'CreateKeyValue' -e 'ConsumerName' -e 'Durable' -e 'Background' -e 'WithoutCancel' -- processor/rule/processor.go processor/rule/config.go processor/rule/factory.go processor/rule/readiness.go processor/rule/entity_watcher.go processor/rule/main_test.go natsclient/test_client.go` — scoped function/resource/context locators.

Search: `git grep -n -e 'InitializeKVStore' -e 'SeedFromRuntime' -e 'semstreams_config' -- processor/rule/kv_config_integration.go natsclient/kv_store.go` — config-manager locators; subsequent read established `natsclient/kv_store.go` does not exist.

Search: `git ls-files '*kv*store*.go' 'natsclient/*kv*.go'` — located `natsclient/kv.go`.

Search: `git grep -n -e 'DefaultKVConfig' -e 'DefaultConfigBucket' -e 'semstreams_config' -- natsclient/kv.go` — zero; actual acquisition is the graph catalog seam cited above.

Search: `git grep -n -e 'KeyRule' -e 'BucketGraphStatus' -- graph/readiness` — fixed status names and publisher.

Search: `git grep -n -e 'BucketPrefix' -e 'bucketPrefix' -e 'WithBucketPrefix' -- natsclient/client.go natsclient/options.go natsclient/test_client.go` — hits only in TestClient.

Search: `git grep -n -e 'context.Background' -e 'context.TODO' -e 'WithoutCancel' -e 'context.Context' -e 'CancelFunc' -- processor/rule/processor.go processor/rule/entity_watcher.go processor/rule/kv_config_integration.go processor/rule/readiness.go processor/rule/owner_lane.go processor/rule/cron_scheduler.go` — operations/private cancellation fields; no searched roots.

Search: `git grep -n -e '^##' -e 'Running Stop' -e 'one live' -e 'Test I/O contexts' -e 'NewTestClient(t' -e 'no state' -- docs/contributing/01-testing.md openspec/specs/component-lifecycle/spec.md` — policy/spec locators.

Search: `gh issue view 1410 --json number,title,body,state,comments` — open issue; body matches assigned scope; no comments.
