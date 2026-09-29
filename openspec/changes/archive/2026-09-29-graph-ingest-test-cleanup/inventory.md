# Graph-ingest test cleanup inventory

base: 21c89f79945600fc42bff4b3a82730c2db2e3fc1

Scope: #1423 / draft PR #1424, branch `codex/gh1423-graph-ingest-test-cleanup`, based on merged main
`72de94b493124c48702a5536de2225d189ac5c5e`. Inventory only; no target state or implementation approval.
Repository was clean at the initial checkpoint. No test, benchmark, Docker operation, guard run or Git mutation ran.

## 1. Claimed gap and exact baseline reconciliation

Current JSON contains **329 debt entries and 89 resolutions**. Exactly **32 graph-ingest debt identities across
16 files** match current source: 20 defer roots and 12 testing-Cleanup roots; 29 are integration-tagged and three
are in unconditionally skipped default tests. All 32 supply `context.Background()` to Component.Stop and discard
its result. The count is source debt, not runtime invocations, failed test count or reproduced hang frequency.
There are zero current resolution records whose identity or explicit dependency JSON references graph-ingest.

The original #1064/#1417 population was 334. #1419's branch left those entries unchanged, but merged main also
contains the independent #1404 removal of five rule/kv_hot_reload entries. Thus 329 is this batch's current base;
334 remains the original historical population. Graph-ingest's 32 identities did not change in that reconciliation.
Current manifest SHA256: `f665fada99aeaa3fcc34848700cee57828060e67b3cfcddbdf1e869499a6c660`.

Exact full identities, fingerprints, source coordinates/text, origins, ordinals, tags and skip state are retained in
`review/baseline-exposure.json` (scratch `/private/tmp/gh1423-baseline-exposure.json`), SHA256 `761392189f16706d485d2d2303e828d11d892909c126376f39e421ff62034b4a`.
The table and pins below map every record; SiblingEdges has two distinct approved ordinal occurrences.

| Source under processor/graph-ingest | Enclosing declaration | Origin / ordinal | Current line |
|---|---|---|---|
| `authority_gate_integration_test.go` | `startAuthorityGateComponent` | cleanup / 1 | 177 |
| `batch_integration_test.go` | `startBatchTestComponent` | cleanup / 1 | 46 |
| `cas_integration_test.go` | `TestIntegration_ConcurrentCanonicalAppend` | cleanup / 1 | 33 |
| `component_test.go` | `TestComponent_Health_Running` | defer / 1 | 547 |
| `component_test.go` | `TestComponent_Start_AlreadyStarted` | defer / 1 | 627 |
| `component_test.go` | `TestComponent_Start_Success` | defer / 1 | 605 |
| `hierarchy_replay_integration_test.go` | `TestComponent_HierarchyReplay_UnchangedEntitiesAdvanceNoRevision` | defer / 1 | 143 |
| `hierarchy_sync_integration_test.go` | `TestComponent_SynchronousHierarchy_ContainerCreation` | defer / 1 | 212 |
| `hierarchy_sync_integration_test.go` | `TestComponent_SynchronousHierarchy_ContextCancellation` | defer / 1 | 596 |
| `hierarchy_sync_integration_test.go` | `TestComponent_SynchronousHierarchy_DisabledConfig` | defer / 1 | 445 |
| `hierarchy_sync_integration_test.go` | `TestComponent_SynchronousHierarchy_IncludedBeforeWrite` | defer / 1 | 57 |
| `hierarchy_sync_integration_test.go` | `TestComponent_SynchronousHierarchy_InvalidEntityID` | defer / 1 | 535 |
| `hierarchy_sync_integration_test.go` | `TestComponent_SynchronousHierarchy_MultipleEntitiesSameType` | defer / 1 | 270 |
| `hierarchy_sync_integration_test.go` | `TestComponent_SynchronousHierarchy_NoWatcherLifecycle` | defer / 1 | 430 |
| `hierarchy_sync_integration_test.go` | `TestComponent_SynchronousHierarchy_SiblingEdges` | defer / 1 | 644 |
| `hierarchy_sync_integration_test.go` | `TestComponent_SynchronousHierarchy_SiblingEdges` | defer / 2 | 742 |
| `hierarchy_sync_integration_test.go` | `TestComponent_SynchronousHierarchy_SingleWrite` | defer / 1 | 137 |
| `keyed_ingest_integration_test.go` | `TestIntegration_IngestGuardBucket_ReconcilesTTLBucketAtAcquisition` | cleanup / 1 | 59 |
| `keyed_ingest_integration_test.go` | `startKeyedWireComponent` | cleanup / 1 | 202 |
| `merge_entity_integration_test.go` | `TestIntegration_MergeEntity_HierarchyDoesNotDuplicate` | cleanup / 1 | 261 |
| `poison_scoping_integration_test.go` | `TestIntegration_BootSweepInventoriesResidentPoisonRealNATS` | cleanup / 1 | 131 |
| `poison_scoping_integration_test.go` | `TestIntegration_NoGuardConsumerOnEntityStatesAfterStart` | cleanup / 1 | 60 |
| `query_integration_test.go` | `TestIntegration_QueryHandlers` | defer / 1 | 51 |
| `query_prefix_integration_test.go` | `startPrefixTestComponent` | cleanup / 1 | 46 |
| `query_wire_contract_integration_test.go` | `TestIntegration_QueryEntityNATS_WireContract` | defer / 1 | 54 |
| `readiness_gauges_integration_test.go` | `TestIntegration_ReadinessGaugesAreEmitted` | defer / 1 | 55 |
| `readiness_integration_test.go` | `TestIntegration_ReadinessEnvelope_BacklogIsNotReady` | defer / 1 | 181 |
| `readiness_integration_test.go` | `TestIntegration_ReadinessEnvelope_NoStreamingPortIsHonestlyCaughtUp` | defer / 1 | 258 |
| `readiness_integration_test.go` | `TestIntegration_ReadyImpliesTheWritesAreDurable` | defer / 1 | 340 |
| `readiness_integration_test.go` | `startIngestForReadiness` | cleanup / 1 | 87 |
| `registered_type_gate_integration_test.go` | `startGateTestComponent` | cleanup / 1 | 51 |
| `resident_stamp_integration_test.go` | `TestResidentUnregisteredStampIsNotPoison` | cleanup / 1 | 57 |

- `processor/graph-ingest/authority_gate_integration_test.go:177` — `t.Cleanup(func() { _ = c.Stop(context.Background()) })`
- `processor/graph-ingest/batch_integration_test.go:46` — `_ = c.Stop(context.Background())`
- `processor/graph-ingest/cas_integration_test.go:33` — `t.Cleanup(func() { _ = c.Stop(context.Background()) })`
- `processor/graph-ingest/component_test.go:547` — `defer comp.Stop(context.Background())`
- `processor/graph-ingest/component_test.go:627` — `defer comp.Stop(context.Background())`
- `processor/graph-ingest/component_test.go:605` — `defer comp.Stop(context.Background())`
- `processor/graph-ingest/hierarchy_replay_integration_test.go:143` — `defer func() { _ = replay.Stop(context.Background()) }()`
- `processor/graph-ingest/hierarchy_sync_integration_test.go:212` — `defer comp.Stop(context.Background())`
- `processor/graph-ingest/hierarchy_sync_integration_test.go:596` — `defer comp.Stop(context.Background())`
- `processor/graph-ingest/hierarchy_sync_integration_test.go:445` — `defer comp.Stop(context.Background())`
- `processor/graph-ingest/hierarchy_sync_integration_test.go:57` — `defer comp.Stop(context.Background())`
- `processor/graph-ingest/hierarchy_sync_integration_test.go:535` — `defer comp.Stop(context.Background())`
- `processor/graph-ingest/hierarchy_sync_integration_test.go:270` — `defer comp.Stop(context.Background())`
- `processor/graph-ingest/hierarchy_sync_integration_test.go:430` — `defer comp.Stop(context.Background())`
- `processor/graph-ingest/hierarchy_sync_integration_test.go:644` — `defer comp.Stop(context.Background())`
- `processor/graph-ingest/hierarchy_sync_integration_test.go:742` — `defer comp.Stop(context.Background())`
- `processor/graph-ingest/hierarchy_sync_integration_test.go:137` — `defer comp.Stop(context.Background())`
- `processor/graph-ingest/keyed_ingest_integration_test.go:59` — `t.Cleanup(func() { _ = c.Stop(context.Background()) })`
- `processor/graph-ingest/keyed_ingest_integration_test.go:202` — `t.Cleanup(func() { _ = c.Stop(context.Background()) })`
- `processor/graph-ingest/merge_entity_integration_test.go:261` — `t.Cleanup(func() { _ = c.Stop(context.Background()) })`
- `processor/graph-ingest/poison_scoping_integration_test.go:131` — `t.Cleanup(func() { _ = c.Stop(context.Background()) })`
- `processor/graph-ingest/poison_scoping_integration_test.go:60` — `t.Cleanup(func() { _ = c.Stop(context.Background()) })`
- `processor/graph-ingest/query_integration_test.go:51` — `_ = component.Stop(context.Background())`
- `processor/graph-ingest/query_prefix_integration_test.go:46` — `t.Cleanup(func() { _ = c.Stop(context.Background()) })`
- `processor/graph-ingest/query_wire_contract_integration_test.go:54` — `defer func() { _ = c.Stop(context.Background()) }()`
- `processor/graph-ingest/readiness_gauges_integration_test.go:55` — `defer func() { _ = c.Stop(context.Background()) }()`
- `processor/graph-ingest/readiness_integration_test.go:181` — `defer func() { _ = c.Stop(context.Background()) }()`
- `processor/graph-ingest/readiness_integration_test.go:258` — `defer func() { _ = c.Stop(context.Background()) }()`
- `processor/graph-ingest/readiness_integration_test.go:340` — `defer func() { _ = c.Stop(context.Background()) }()`
- `processor/graph-ingest/readiness_integration_test.go:87` — `t.Cleanup(func() { _ = c.Stop(context.Background()) })`
- `processor/graph-ingest/registered_type_gate_integration_test.go:51` — `t.Cleanup(func() { _ = c.Stop(context.Background()) })`
- `processor/graph-ingest/resident_stamp_integration_test.go:57` — `t.Cleanup(func() { _ = c.Stop(context.Background()) })`

## 2. Every current cleanup spelling and ownership shape

Twelve Cleanup records consist of six setup helpers and six direct test setups. Helpers return a running component
(and sometimes context/client); a defer inside their own function would execute at helper return rather than test
completion. Authority/batch/keyed-wire/prefix/type-gate helpers create deadline-free Start/operation roots; readiness
receives a caller context. Stop registration generally follows Initialize and asserted successful Start, so failures
before registration have no test-owned terminal callback. The keyed TTL test registers after Initialize but before
Start. Authority's post-Start Flush assertion precedes its cleanup registration as an additional exit window.

- `processor/graph-ingest/authority_gate_integration_test.go:138` — `ctx := context.Background()`
- `processor/graph-ingest/authority_gate_integration_test.go:176` — `require.NoError(t, testClient.GetNativeConnection().Flush())`
- `processor/graph-ingest/keyed_ingest_integration_test.go:63` — `require.NoError(t, c.Start(ctx),`
- `processor/graph-ingest/readiness_integration_test.go:62` — `func startIngestForReadiness(ctx context.Context, t *testing.T) (*natsclient.TestClient, *Component) {`

The 20 defer roots comprise 17 live integration sites and three skipped unit sites. All are registered after Start
returns; most follow a fatal success assertion. Skipped Start_Success registers after the call but before its nonfatal
assertion. Hierarchy setup constructs NATS and a component but leaves Initialize/Start/Stop to each caller. The ten
hierarchy-sync spellings include subtest iterations; they are not ten unique runtime instances or top-level containers.

- `processor/graph-ingest/hierarchy_integration_test.go:55` — `testClient := natsclient.NewTestClient(t, natsclient.WithKV(), natsclient.WithStreams(streams...))`
- `processor/graph-ingest/hierarchy_integration_test.go:56` — `return createHierarchyComponentOnClient(t, testClient.Client, enableHierarchy)`
- `processor/graph-ingest/hierarchy_integration_test.go:78` — `comp, err := CreateGraphIngest(configJSON, deps)`
- `processor/graph-ingest/component_test.go:541` — `t.Skip("requires real NATS connection - move to integration tests")`
- `processor/graph-ingest/component_test.go:599` — `t.Skip("requires real NATS connection - move to integration tests")`
- `processor/graph-ingest/component_test.go:621` — `t.Skip("requires real NATS connection - move to integration tests")`
- `processor/graph-ingest/component_test.go:632` — `assert.NoError(t, err, "Start should be idempotent")`

Those three skipped bodies supply no runtime cleanup proof. The skipped AlreadyStarted expectation still says Start
is idempotent; production's one-shot lifecycle rejects reuse. That is an adjacent stale expectation, not authority to
change production or to present removing skipped-source debt as exercised cleanup.

Readiness/Gauges starts with 60-second authority; Backlog and Durable use 120 seconds, NoStreamingPort 60 seconds.
Their lexical Stop defers run before their earlier deferred cancels, if the work deadline has not already expired.
The two readiness-helper callers use 90-second contexts and defer cancel; their registered Stop runs AFTER those
cancels, so those exits are currently abort-shaped. The AbsentKey caller also explicitly stops the producer before
purging its status key. Changing cancellation provenance without preserving this distinction can change assertions.

- `processor/graph-ingest/readiness_gauges_integration_test.go:38` — `ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)`
- `processor/graph-ingest/readiness_integration_test.go:123` — `ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)`
- `processor/graph-ingest/readiness_integration_test.go:124` — `defer cancel()`
- `processor/graph-ingest/readiness_integration_test.go:156` — `ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)`
- `processor/graph-ingest/readiness_integration_test.go:241` — `ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)`
- `processor/graph-ingest/readiness_integration_test.go:310` — `ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)`
- `processor/graph-ingest/readiness_integration_test.go:379` — `defer cancel()`

The SynchronousHierarchy ContextCancellation test cancels a separate CreateEntity input, while Start remains on its
original Background context. It is an operation-cancellation probe, not an accepted-Start abort test. Existing sleeps
in batch/prefix/query/wire/merge setup and bounded readiness polling are observed adjacent evidence; this inventory
neither converts them to readiness proof nor expands the batch into all package timing debt.

- `processor/graph-ingest/hierarchy_sync_integration_test.go:599` — `cancelledCtx, cancel := context.WithCancel(context.Background())`
- `processor/graph-ingest/hierarchy_sync_integration_test.go:619` — `err := comp.CreateEntity(cancelledCtx, entity)`
- `processor/graph-ingest/batch_integration_test.go:49` — `time.Sleep(100 * time.Millisecond)`

## 3. Adjacent explicit operations, partial failure and resource lifetime

The typed integration-selection Stop reference query contains 49 locations inside graph-ingest: the 32 approved
cleanup sites plus 17 ordinary-syntax calls. gopls also returns interface-related references elsewhere; those are
not new graph-ingest records. This is a reference census, not a fresh semantic guard classification or whole-program
reachability proof. The adjacent 17 have these distinct roles:

| Surface | Ordinary Stop locations | Observed role |
|---|---|---|
| component_test.go | 643,652,666,915 | skipped success/timeout/abort probes; live Stop-before-Start contract |
| hierarchy_replay_integration_test.go | 135 | finish seed component before fresh replay over the same store |
| readiness_integration_test.go | 390 | fence producer before status-key purge, then helper fallback remains |
| lifecycle_integration_test.go | 102,105,191 | one-shot/repeated Stop contract; overlap with failed Start acquisition |
| lifecycle_owner_test.go | 93,169,175,195,204,226,232,238 | exact drain order, failed-Start retry, running-bound terminality, nil/pre-Start |

- `processor/graph-ingest/hierarchy_replay_integration_test.go:135` — `require.NoError(t, seed.Stop(context.Background()))`
- `processor/graph-ingest/hierarchy_replay_integration_test.go:140` — `replay := createHierarchyComponentOnClient(t, testClient.Client, true)`
- `processor/graph-ingest/readiness_integration_test.go:390` — `require.NoError(t, comp.Stop(ctx))`
- `processor/graph-ingest/component_test.go:652` — `err := comp.Stop(context.Background())`
- `processor/graph-ingest/component_test.go:670` — `_ = err`
- `processor/graph-ingest/lifecycle_integration_test.go:102` — `if err := comp.Stop(t.Context()); err != nil {`
- `processor/graph-ingest/lifecycle_integration_test.go:105` — `if err := comp.Stop(t.Context()); err != nil {`
- `processor/graph-ingest/lifecycle_integration_test.go:175` — `<-releaseSecond`
- `processor/graph-ingest/lifecycle_integration_test.go:191` — `go func() { stopResult <- comp.Stop(stopCtx) }()`

Replay's seed Stop is an explicit terminal phase boundary, with no prior seed finalizer for an earlier fatal assertion.
Readiness's explicit Stop is also a phase fence, not an API-contract test of Stop itself. These must not disappear
from the ownership inventory merely because the guard's original debt set contains only defer/Cleanup roots.
Existing owner tests use causal channels but have unbounded receive edges and early-exit release gaps; their success
paths are not proof that every failure path releases resources. No hang was reproduced here.

All live selected integration setups use NewTestClient; it owns NATS connection/container cleanup. Lexical component
Stop occurs before the enclosing test's Cleanup callbacks; a registered component cleanup comes after TestClient
registration and therefore runs first under LIFO. Readiness gauges and durability additionally defer Terminate,
duplicating the substrate owner despite Terminate's once-only behavior. This duplicates invocation and discards its
result; it is not evidence of two actual container terminations.

- `processor/graph-ingest/readiness_gauges_integration_test.go:43` — `defer func() { _ = tc.Terminate() }()`
- `processor/graph-ingest/readiness_integration_test.go:317` — `defer func() { _ = tc.Terminate() }()`
- `natsclient/test_client.go:863` — `t.Cleanup(func() {`
- `natsclient/test_client.go:864` — `if err := testClient.Terminate(); err != nil {`
- `natsclient/test_client.go:933` — `tc.cleanupOnce.Do(func() {`
- `natsclient/test_client.go:938` — `return tc.cleanupErr`

The integration TestMain additionally creates a package-shared NATS server before m.Run and terminates it afterward,
preserving a failing exit code. Selected fresh-client tests coexist with that package substrate; there is no measured
runtime/container count in this inventory and no assertion that the package has only one live server. The hierarchy
replay deliberately has two successive components over one test-owned store, not same-instance restart.

- `processor/graph-ingest/lifecycle_integration_test.go:38` — `sharedLifecycleNATSClient, err = natsclient.NewSharedTestClient(`
- `processor/graph-ingest/lifecycle_integration_test.go:48` — `code := m.Run()`
- `processor/graph-ingest/lifecycle_integration_test.go:51` — `if err := sharedLifecycleNATSClient.Terminate(); err != nil {`

## 4. Production lifecycle and proof boundary

Initialize validates configuration without I/O. Start rejects nil/ended authority before action, rejects reuse,
derives runtime/pool/submission contexts locally and retains private cancels plus completion state. The inspected
Component fields contain no retained context/provider; acquisition callbacks explicitly accept caller contexts.
This is bounded inspection of the lifecycle state, not a repository-wide context-retention certification.

- `processor/graph-ingest/component.go:924` — `// Initialize validates configuration and sets up ports (no I/O)`
- `processor/graph-ingest/component.go:526` — `cancel                  context.CancelFunc`
- `processor/graph-ingest/component.go:945` — `func (c *Component) Start(ctx context.Context) (startErr error) {`
- `processor/graph-ingest/component.go:956` — `return errs.WrapFatal(errs.ErrAlreadyStarted, "Component", "Start", "component lifecycle already used")`
- `processor/graph-ingest/component.go:971` — `runCtx, cancel := context.WithCancel(ctx)`
- `processor/graph-ingest/component.go:984` — `rollbackErr := lifecyclecleanup.RollbackFailedStart(parent, c.cleanup)`

Start acquires storage/caches, performs the snapshot guard, creates status state, builds the pool, binds consumers,
query/mutation subscriptions and starts the status worker. Failed Start synchronously calls the existing bounded
RollbackFailedStart helper; only failed rollback retains cleanupPending and exact handles for a later Stop retry.
These are existing runtime semantics, not a test-helper retry policy to infer for a running owner.

- `processor/graph-ingest/component.go:1019` — `if err := c.initStorage(runCtx); err != nil {`
- `processor/graph-ingest/component.go:1027` — `if err := c.buildIngestPool(poolCtx); err != nil {`
- `processor/graph-ingest/component.go:1030` — `if err := c.setupSubscriptions(runCtx, submitCtx); err != nil {`
- `processor/graph-ingest/component.go:1040` — `c.startStatusMetricsLoop(runCtx)`

Stop rejects nil/ended input, handles a never-started owner, waits for an in-flight Start under caller authority and
rejects concurrent Stop. Its terminal sequence is native consumer Drain then Closed observation, submit cancellation,
keyed-pool Stop, pool cancellation, core-subscription Drain, runtime cancellation, status worker observation and cache
Close. Errors aggregate and the exact caller-context error is retained. Running cleanup becomes terminal even on
returned error; failed-Start cleanupPending remains retryable on error. Its graceful log alone does not prove nil.

- `processor/graph-ingest/component.go:1069` — `if err := ctx.Err(); err != nil {`
- `processor/graph-ingest/component.go:1087` — `select {`
- `processor/graph-ingest/component.go:1096` — `return errs.WrapTransient(errors.New("stop already in progress"), "Component", "Stop", "concurrent Stop is unsupported")`
- `processor/graph-ingest/component.go:1105` — `if retryable && stopErr != nil {`
- `processor/graph-ingest/component.go:1110` — `c.terminal = true`
- `processor/graph-ingest/component.go:1128` — `binding.handle.Drain()`
- `processor/graph-ingest/component.go:1134` — `case <-c.consumers[index].handle.Closed():`
- `processor/graph-ingest/component.go:1140` — `c.ingestSubmitCancel()`
- `processor/graph-ingest/component.go:1143` — `cleanupErr = errors.Join(cleanupErr, c.ingestPool.Stop(ctx))`
- `processor/graph-ingest/component.go:1150` — `cleanupErr = errors.Join(cleanupErr, sub.Drain(ctx))`
- `processor/graph-ingest/component.go:1154` — `c.cancel()`
- `processor/graph-ingest/component.go:1158` — `case <-c.statusDone:`
- `processor/graph-ingest/component.go:1164` — `cleanupErr = errors.Join(cleanupErr, c.entityCache.Close())`
- `processor/graph-ingest/component.go:1169` — `return errors.Join(cleanupErr, ctx.Err())`

Native Drain and cache Close are contextless calls inside synchronous cleanup. HybridCache.Close and TTL.Close
each independently wait up to five seconds for their done channel; Component invokes them sequentially, outside
caller-context observation. KeyedPool.Stop can return the caller error while lanes continue; Component then cancels
pool authority. Caller finite authority therefore does not establish interruption or its own wall-clock ceiling.

- `pkg/cache/hybrid.go:290` — `return fmt.Errorf("timeout waiting for cleanup goroutine to finish")`
- `pkg/cache/ttl.go:263` — `return fmt.Errorf("timeout waiting for cleanup goroutine to finish")`
- `pkg/dispatch/keyed_pool.go:363` — `// the lanes keep draining in the background; the caller has simply`
- `pkg/dispatch/keyed_pool.go:381` — `p.logger.Warn("dispatch: Stop timed out before accepted work drained",`

A running bound winner clears lifecycle handles and grants
no later running-generation rejoin. Existing owner tests explicitly distinguish failed-Start retry from that terminal
outcome and assert admitted effect/settlement order. No inspected runtime semantic makes a controlled test-only repair
impossible, but success/error/join evidence must remain distinct; this is not a production-correctness verdict.

- `processor/graph-ingest/lifecycle_owner_test.go:43` — `func TestLifecycleOwnerRunningStopPreservesEffectSettlementOrder(t *testing.T) {`
- `processor/graph-ingest/lifecycle_owner_test.go:134` — `func TestLifecycleOwnerFailedCleanupRetainsExactHandlesForLaterStop(t *testing.T) {`
- `processor/graph-ingest/lifecycle_owner_test.go:180` — `func TestLifecycleOwnerRunningDeadlineIsTerminalWithoutReplay(t *testing.T) {`
- `processor/graph-ingest/lifecycle_owner_test.go:213` — `close(consumer.closed)`

## 5. Existing owners and problem shape

The semantic shape is test-owned lifecycle finalization after partial/normal execution, before dependent substrate.
No new durable/runtime primitive is proposed in inventory. The following collision table records available owners.

| Owner | Authority and lifetime | Error/recovery/status boundary | Present readers/consumers |
|---|---|---|---|
| component lifecycleTestOwner | private lexical test owner; fresh finite Stop then Start cancel | concrete attempt suppresses implicit retry; preserves bound and returned errors | existing standard/injection/benchmark suites, not exported to graph-ingest |
| graph-ingest Component | real runtime admission, exact consumer/pool/subscription/cache ownership | controlled drain/settlement order; failed-Start retry only; readiness is observation | real factory, six setup helpers, direct tests and owner-local proofs |
| RollbackFailedStart | stateless fixed-five-second detached rollback under caller values | synchronous error/context join; production failed-Start semantic only | Component.Start rollback |
| natsclient TestClient | connection/container owner; independent finite finalizers | once-only termination with retained error; registered testing Cleanup | selected integration clients and package TestMain shared client |
| six package setup helpers | construction/Start and registered component finalizers | unbounded ignored Stop; no early ownership in most paths | typed callers listed below |

- `component/lifecycle_test_suite.go:29` — `type lifecycleTestOwner struct {`
- `component/lifecycle_test_suite.go:59` — `stopCtx, cancelStop := context.WithTimeout(context.Background(), o.stopBudget)`
- `component/lifecycle_test_suite.go:65` — `o.attempted = true // A returned error or panic does not authorize an implicit retry.`
- `component/lifecycle_test_suite.go:87` — `defer o.cancelStart() // Stop completes before accepted Start authority ends.`
- `internal/lifecyclecleanup/lifecyclecleanup.go:12` — `const failedStartRollbackTimeout = 5 * time.Second`
- `internal/lifecyclecleanup/lifecyclecleanup.go:33` — `ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), budget)`
- `internal/lifecyclecleanup/lifecyclecleanup.go:37` — `return errors.Join(rollbackErr, ctx.Err())`
- `natsclient/test_client.go:310` — `closeCtx, closeCancel := context.WithTimeout(context.Background(), timeout)`
- `natsclient/test_client.go:317` — `terminateCtx, terminateCancel := context.WithTimeout(context.Background(), timeout)`

The selected test helpers declare no runtime catalog/configuration/claim or recovery ledger; their owned resources
come through the existing production dependencies, TestClient and Component. Runtime GRAPH_STATUS writes and native
consumer/catalog behavior are existing resources, not a second testing ownership catalog. No new exported symbol,
port, bucket, subject or configuration is a requested consumer surface for this batch.

## 6. Adopter seams and adjacency

Typed helper references are all package-local test calls: authority 10, batch 21, keyed-wire 2, prefix 5, readiness 2,
registered-type gate 4, hierarchy constructor 10. Batch callers also occur in batch_missing, entity_mutation,
merge_entity and structural_gate_wire tests; prefix callers include cache_coherence and cache_stale_repopulation.
Exact query outputs are retained in `review/helper-references.json` (scratch `/private/tmp/gh1423-helper-references.json`).
These counts are call spellings, not top-level test runs or proof that all source build selections were scanned.

A test author must currently know whether a helper returns a live runtime, who owns NATS, when cancel executes,
whether explicit Stop already formed a phase fence, and whether its error is controlled or abort evidence. Doing
nothing leaves late registration and discarded-error behavior; the current prefix helper comment even assigns Stop
to the caller while the helper itself registers it. Missing ownership is generally discovered only via a later test
failure or timeout, rather than a typed acquisition result. This names the adopter knowledge gap without selecting an API.

- `processor/graph-ingest/query_prefix_integration_test.go:23` — `// testcontainer NATS instance and returns it. The caller is responsible for`
- `processor/graph-ingest/query_prefix_integration_test.go:46` — `t.Cleanup(func() { _ = c.Stop(context.Background()) })`

No new or changed outward-facing API is proposed at this phase. The current candidate surface is package-local
`*_test.go` support; external consumer migration is therefore not established. Sisters were not re-swept, and no
claim is made that production graph-ingest has no external adopters. A later outward surface would need its own seam
inventory, not an inference from these package-local references.

### Adjacent claims

1. #1423 / #1424 execute #1417 after merged #1419; #1417 stays open. The issue requires exact debt, appropriate finite
   cleanup, honest errors, owned completion before substrate and causal proof, without a generic watchdog or runtime rewrite.
2. component-lifecycle and runtime-context-ownership distinguish controlled shutdown from abort and deny generic
   running rejoin. test-cleanup-policy requires exact new/stale/uncertain reconciliation and limits deadline claims.
3. Current testing guidance owns lexical cleanup before t.Context cancellation, no duplicate TestClient termination,
   explicit synchronization, measured budgets and the canonical integration runner/host lock. Those are constraints,
   not a new design decision in this inventory.
4. ADR-072 preserves effect/durable-guard/ack and same-key ordering. Its historical submit-cancel-first shutdown prose
   differs from current consumer-Closed-first implementation and owner tests; this batch does not rewrite that runtime
   ordering. Current component-lifecycle explicitly permits resource-specific admission drain before cancellation.
5. #1293, #1411 and #1412 retain their gate/runtime/nil-context scopes. No runtime defect or reproducible hang was
   demonstrated by this read-only pass. Skipped stale tests, explicit phase fences and native contextless calls remain
   visible rather than silently classified as repaired or safe.

## Searches and read log

1. Fully read architect contract, project purpose/boundary and both current change artifacts. Read current lifecycle,
   runtime-context-ownership, test-cleanup-policy, graph-ingest and keyed-dispatch specs, plus ADR-072 and relevant testing
   guidance. Broad graph-ingest spec output truncated; missing ranges340–850 and850–960 were reread explicitly.
2. `git rev-parse HEAD`; `git status --short`; `rg --files processor/graph-ingest`; archive/spec/ADR filename discovery.
3. Python decoded baseline entries, grouped exact graph-ingest identities, counted16files and searched entire resolution
   records for graph-ingest paths: zero. No historical count or grep line count substitutes for the JSON measurement.
4. `GOFLAGS=-tags=integration gopls workspace_symbol -matcher=fuzzy 'graph-ingest Stop'`: initial sandbox load failed
   on Go cache permissions. Authorized cache access succeeded; broad output was not used as an absence proof.
5. `GOFLAGS=-tags=integration gopls workspace_symbol -matcher=caseSensitive 'Component.Stop'` resolved1065:21.
   `gopls references processor/graph-ingest/component.go:1065:21` returned the local49 plus interface-related outer refs.
   `gopls call_hierarchy` confirmed cleanup/handle-clear callees; broad output truncated, no completeness claim from it.
6. Exact gopls workspace-symbol queries for createTestComponentWithHierarchyConfig, startAuthorityGateComponent and
   lifecycleTestOwner. Exact references queries for helper declarations136:6,23:6,184:6,25:6,62:6,32:6,48:6 in the
   respective authority,batch,keyed,prefix,readiness,registered-type,hierarchy files; outputs retained in companion JSON.
7. Line-range reads cover every baseline setup/Stop site, Component480–650,924–1265, owner/lifecycle tests, fixture
   constructors, current shared owner, rollback helper and TestClient cleanup. Targeted native boundary reads:
   pkg/cache/hybrid.go270–300, pkg/cache/ttl.go243–273, pkg/dispatch/keyed_pool.go350–394. No arbitrary production callgraph sweep.
8. `gh issue view 1423 --json body,comments` and `gh issue view 1417 --json body,comments`: read full issue/body/comment
   contracts; targeted body reread avoided combined-output truncation. Read-only network access required escalation.
9. `git grep -n -E 'graph-ingest.*(Stop|cleanup)|graph.ingest.*lifecycle' -- openspec/specs` found only unrelated entity-ID
   prose; lifecycle truth came from the explicitly named capability specs, not an absence inference from that query.
10. Python wrote only these owned scratch artifacts, matching each32record to its existing function/ordinal/source line
    and validating every canonical line pin. No repository mutation, tests, analyzer execution or new agent.

## Open evidence limits

This inventory does not contain cold/warm runtimes, new failure controls, final cleanup budgets, container measurements,
or passing guard evidence for a future repair. It does not select the treatment of skipped stale bodies, helper
lifetimes or explicit phase fences. Those bounded choices follow INVENTORY PASS under the already accepted batch
contract. Native uninterruptible behavior and failures exposed by later proof must be reported as observed evidence,
not hidden by a timeout increase or retroactive baseline approval.
