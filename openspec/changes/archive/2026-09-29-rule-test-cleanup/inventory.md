# Rule cleanup inventory — completed source ledger, review pending

base: caa98f5acae60efbc669ad1e1795ab6e903abd42

Inventory only. This index synthesizes the supplied prior inventory records; it is not an independent enumeration or INVENTORY PASS. No target state, adoption decision, implementation task, or cleanup-safety finding is asserted. The coordinator materializes and submits this checkpoint for independent inventory review. Binding rulings remain with the owner.

## Evidence identity and limits

Issue #1428 / draft PR #1429 is the rule package batch under #1417. Source main was `7a47c7c60d40345aa1c7ca8bbaddd9d3348b8b27`; the frozen claim baseline is the `base` above. The active proposal and tasks explicitly gate design and implementation on inventory review. SemStreams owns the framework and shared runtime, including rule execution; no product-domain scope is inferred.

Canonical companion paths are relative to `openspec/changes/rule-test-cleanup/`:

| Companion | SHA256 / contents |
|---|---|
| `review/baseline-exposure.json` | `3bdbe017eef127968a5a200dbb2c1273acbf0f425782ca2b253cfcac60f22e89`; exact 24 identities, source pins, package/build/function boundaries, five exposed resolutions |
| `review/helper-callers.json` | `385ad4b442ab3a64f50af87e8ed511be031491e14072187c85dce9ddb5fad6cf`; 11 structural queries, 119 reference records including repeated build variants, source hashes and enclosing declarations |
| `review/evidence/inventory-raw.zip` | `176cc5bb977cec91272b6cfb5399e8f40f35aa8e85f2662347cc18eec4de871b`; raw helper/terminal/search/claim records; coordinator supplies member SHA manifest |

The baseline file measured by the prior inventory had SHA256 `909d254426e4b190d3fe10c86f936dcea742d8a6998b7b90a93bbe02ccab2615`: 297 debt entries and 90 reviewed resolutions. These are recorded cleanup liabilities, not 297 reproduced hangs. The supplied exposure records reconcile to 24 rule roots in ten files: 19 Processor, three CronScheduler, and two graph-ingest collaborator roots. Sixteen are in package `rule`, eight in external test package `rule_test`; external test package does not mean outside this repository. Three roots are default-build tests and 21 require integration.

## Surface inventory category 1 — claimed gap

The measured fact is legacy test cleanup ownership and authority: exact unbounded terminal roots, setup exits before ownership registration, terminal results, and component/substrate ordering. It is not absence of a context-taking Stop API. Every listed baseline entry has classification `unbounded-terminal-cleanup`, provenance `unbounded`, ordinal 1, and owner issue #1064 in the supplied source record. Exact full semantic identities and fingerprints remain authoritative in `review/baseline-exposure.json`; row numbers below are its zero-based `entries` indexes.

| Row | Enclosing declaration | Owner / origin / build / package | Exact source pin |
|---|---|---|---|
| 0 | `newRunScopeHarness` | graph-ingest.Component; cleanup; integration; rule | `processor/rule/actions_run_scope_integration_test.go:130` — `_ = ingest.Stop(context.Background())` |
| 1 | `startCronProcessorForTest` | Processor; cleanup; integration; rule | `processor/rule/cron_scheduler_integration_test.go:87` — `t.Cleanup(func() { _ = proc.Stop(context.Background()) })` |
| 2 | `TestCronScheduler_StartActuallyFiresFromRobfig` | CronScheduler; defer; default; rule | `processor/rule/cron_scheduler_test.go:585` — `if err := s.Stop(context.Background()); err != nil {` |
| 3 | `TestCronScheduler_StartTwiceFails` | CronScheduler; defer; default; rule | `processor/rule/cron_scheduler_test.go:245` — `if err := s.Stop(context.Background()); err != nil {` |
| 4 | `startSchedulerForTest` | CronScheduler; cleanup; default; rule | `processor/rule/cron_scheduler_test.go:100` — `if err := scheduler.Stop(context.Background()); err != nil {` |
| 5 | `TestIntegration_Processor_DebounceNonZero_CoalescingSetCreated` | Processor; defer; integration; rule | `processor/rule/entity_watcher_debounce_integration_test.go:202` — `defer processor.Stop(context.Background())` |
| 6 | `TestIntegration_Processor_DebounceZero_ConfigValidation` | Processor; defer; integration; rule | `processor/rule/entity_watcher_debounce_integration_test.go:324` — `defer processor.Stop(context.Background())` |
| 7 | `TestIntegration_Processor_DebounceZero_EdgeCases` | Processor; defer; integration; rule | `processor/rule/entity_watcher_debounce_integration_test.go:390` — `defer processor.Stop(context.Background())` |
| 8 | `TestIntegration_Processor_DebounceZero_ImmediateProcessing` | Processor; defer; integration; rule | `processor/rule/entity_watcher_debounce_integration_test.go:118` — `defer processor.Stop(context.Background())` |
| 9 | `TestIntegration_Processor_DebounceZero_NoCoalescingSet` | Processor; defer; integration; rule | `processor/rule/entity_watcher_debounce_integration_test.go:70` — `defer processor.Stop(context.Background())` |
| 10 | `TestIntegration_Processor_DebounceZero_NoTickerSpinning` | Processor; defer; integration; rule | `processor/rule/entity_watcher_debounce_integration_test.go:158` — `defer processor.Stop(context.Background())` |
| 11 | `TestIntegration_Processor_DebounceZero_Transition` | Processor; defer; integration; rule | `processor/rule/entity_watcher_debounce_integration_test.go:280` — `defer processor.Stop(context.Background())` |
| 12 | `TestEntityWatcherHardeningRealNATS` | Processor; cleanup; integration; rule | `processor/rule/entity_watcher_hardening_integration_test.go:75` — `_ = processor.Stop(context.Background())` |
| 13 | `TestEntityWatcher_BoundedEvaluations` | Processor; cleanup; integration; rule_test | `processor/rule/entity_watcher_integration_test.go:411` — `processor.Stop(context.Background())` |
| 14 | `TestEntityWatcher_RuleTriggerDebouncing` | Processor; cleanup; integration; rule_test | `processor/rule/entity_watcher_integration_test.go:122` — `processor.Stop(context.Background())` |
| 15 | `TestIntegration_DynamicRuleCRUD` | Processor; defer; integration; rule_test | `processor/rule/rule_integration_test.go:247` — `defer processor.Stop(context.Background())` |
| 16 | `TestIntegration_DynamicWatchPatterns` | Processor; defer; integration; rule_test | `processor/rule/rule_integration_test.go:541` — `defer processor.Stop(context.Background())` |
| 17 | `TestIntegration_GraphIntegration` | Processor; defer; integration; rule_test | `processor/rule/rule_integration_test.go:681` — `defer processor.Stop(context.Background())` |
| 18 | `TestIntegration_KVEntityStateWatch` | Processor; defer; integration; rule_test | `processor/rule/rule_integration_test.go:147` — `defer processor.Stop(context.Background())` |
| 19 | `TestIntegration_PrometheusMetrics` | Processor; defer; integration; rule_test | `processor/rule/rule_integration_test.go:447` — `defer processor.Stop(context.Background())` |
| 20 | `TestIntegration_TransitionOperator_UpdateKV` | Processor; defer; integration; rule_test | `processor/rule/rule_integration_test.go:831` — `defer processor.Stop(context.Background())` |
| 21 | `TestEntityWatcher_DeletedEntityCleansRuleState` | Processor; cleanup; integration; rule | `processor/rule/state_cleanup_integration_test.go:47` — `t.Cleanup(func() { _ = proc.Stop(context.Background()) })` |
| 22 | `TestStatefulEvaluator_Integration` | Processor; defer; integration; rule | `processor/rule/stateful_integration_test.go:44` — `defer processor.Stop(context.Background())` |
| 23 | `newRevisionClaimHarness` | graph-ingest.Component; cleanup; integration; rule | `processor/rule/triple_mutator_revision_integration_test.go:74` — `_ = ingest.Stop(context.Background())` |

No new absence claim is closed by grep. `gopls workspace_symbol -matcher=fuzzy 'rule Stop'` (search records 4 and 8) is existing-surface evidence, not evidence that all terminal ownership has been enumerated.

## Surface inventory category 2 — current spellings and ownership

The fact appears in direct defers, testing cleanup closures, helper-installed cleanup, explicit phase Stop calls, test-owned cancellation, and the native terminal implementations. The 24 roots are only the exact baseline population; ordinary probes and adjacent support remain distinct.

### Helper/caller index

Query indexes below refer to `review/helper-callers.json.queries`. Its reference entries carry exact call text, enclosing declarations, evidence lines, and file hashes. Raw structural output has 15 helper queries; four later integration queries are not expanded into this annotated companion.

| Query index | Helper declaration | References |
|---|---|---|
| 0 (default) | `processor/rule/cron_scheduler_test.go:94` — `func startSchedulerForTest(ctx context.Context, t *testing.T, scheduler *CronScheduler) {` | 24 |
| 1 (default) | `processor/rule/cron_scheduler_test.go:76` — `func newSchedulerForTest(t *testing.T, exec ActionExecutorInterface) *CronScheduler {` | 16 |
| 2 (default) | `processor/rule/cron_scheduler_test.go:82` — `func newUnstartedSchedulerForTest(t *testing.T, exec ActionExecutorInterface) *CronScheduler {` | 8 |
| 3 (integration) | `processor/rule/cron_scheduler_test.go:94` — `func startSchedulerForTest(ctx context.Context, t *testing.T, scheduler *CronScheduler) {` | 24 |
| 4 (integration) | `processor/rule/cron_scheduler_integration_test.go:60` — `func startCronProcessorForTest(t *testing.T, natsClient *natsclient.Client, rules []Definition) (*Processor, *metric.MetricsRegistry) {` | 6 |
| 5 (integration) | `processor/rule/cron_scheduler_integration_test.go:41` — `func getIntegrationNATSClient(t *testing.T) *natsclient.Client {` | 4 |
| 6 (integration) | `processor/rule/actions_run_scope_integration_test.go:108` — `func newRunScopeHarness(t *testing.T) *runScopeHarness {` | 5 |
| 7 (integration) | `processor/rule/triple_mutator_revision_integration_test.go:46` — `func newRevisionClaimHarness(t *testing.T) *revisionClaimHarness {` | 2 |
| 8 (integration) | `processor/rule/rule_integration_test.go:31` — `func getTestNATSClient(t *testing.T) *natsclient.Client {` | 9 |
| 9 (default) | `processor/rule/cron_scheduler_test.go:627` — `func newSchedulerWithTrackerForTest(t *testing.T, exec ActionExecutorInterface) (*CronScheduler, *ScheduleTracker) {` | 9 |
| 10 (default) | `processor/rule/cron_scheduler_test.go:937` — `func newSchedulerWithMetricsForTest(t *testing.T, exec ActionExecutorInterface) (*CronScheduler, *cronMetrics) {` | 12 |

The **37 physical lifecycle-helper callers** are the union of query indexes 0, 3, 4, 6, and 7, deduplicated by `(path, line, column)`: scheduler 24, cron Processor six, run-scope five, revision two. This number excludes constructor/tracker/metrics/substrate helper references. It is not a total count of Stop callers. The 119 annotated reference records must not be reported as 119 distinct lifecycle consumers.

| Cleanup-owning helper | Physical caller lines in its own file |
|---|---|
| `startSchedulerForTest` | 350, 388, 415, 436, 458, 487, 501, 524, 651, 681, 697, 829, 861, 894, 986, 1007, 1032, 1052, 1079, 1131, 1194, 1225, 1336, 1366 |
| `startCronProcessorForTest` | 121, 165, 229, 260, 313, 327 |
| `newRunScopeHarness` | 210, 285, 360, 400, 422 |
| `newRevisionClaimHarness` | 158, 189 |

### Measured authority and setup escapes

Scheduler construction returns the owner without starting it; `startSchedulerForTest` calls Start before installing cleanup. A fatal Start assertion can exit before that registration: `processor/rule/cron_scheduler_test.go:96` — `if err := scheduler.Start(ctx); err != nil {`; `processor/rule/cron_scheduler_test.go:99` — `t.Cleanup(func() {`.
Cron Processor construction precedes Initialize, Start and cleanup registration. Its Start authority is a 30-second Background descendant; cancellation and Stop are separate cleanup registrations: `processor/rule/cron_scheduler_integration_test.go:76` — `proc, err := NewProcessorWithMetrics(natsClient, &cfg, registry)`; `processor/rule/cron_scheduler_integration_test.go:84` — `ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)`; `processor/rule/cron_scheduler_integration_test.go:85` — `t.Cleanup(cancel)`.
Both graph-ingest harnesses use a cancellable testing-context child, then initialize, start and flush before registering Stop. This is an observed acquisition-to-registration escape interval: `processor/rule/actions_run_scope_integration_test.go:110` — `ctx, cancel := context.WithCancel(t.Context())`; `processor/rule/actions_run_scope_integration_test.go:126` — `require.NoError(t, ingest.Initialize())`; `processor/rule/actions_run_scope_integration_test.go:128` — `require.NoError(t, testClient.GetNativeConnection().Flush())`; `processor/rule/triple_mutator_revision_integration_test.go:48` — `ctx, cancel := context.WithCancel(t.Context())`; `processor/rule/triple_mutator_revision_integration_test.go:70` — `require.NoError(t, ingest.Initialize())`; `processor/rule/triple_mutator_revision_integration_test.go:72` — `require.NoError(t, testClient.GetNativeConnection().Flush())`.
The run-scope and revision harness return records carry operation context to later mutations; their cleanup callbacks discard Stop errors. Their tracker Processor values are collaborator state, not separately started runtime owners (source records 9 and 50).
Explicit cross-restart fences coexist with helper fallback cleanup: `processor/rule/cron_scheduler_integration_test.go:235` — `require.NoError(t, proc1.Stop(context.Background()))`; `processor/rule/cron_scheduler_integration_test.go:315` — `require.NoError(t, proc1.Stop(context.Background()))`; `processor/rule/cron_scheduler_integration_test.go:329` — `require.NoError(t, proc2.Stop(context.Background()))`. A direct explicit call is not automatically another baseline cleanup root.
Dedicated `NewTestClient` registers checked termination; shared-client helpers install their own callbacks: `natsclient/test_client.go:862` — `// Register cleanup`; `processor/rule/cron_scheduler_integration_test.go:52` — `t.Cleanup(func() { testClient.Terminate() })`. The complete per-case ordering ledger remains a gap below.

### Native terminal contracts and evidence limits

Processor Stop accepts exact caller authority, rejects nil, serializes terminal admission, preserves owner-specific failed-Start cleanup retry, and marks a running generation terminal after its attempted cleanup: `processor/rule/processor.go:1191` — `func (rp *Processor) Stop(ctx context.Context) error {`; `processor/rule/processor.go:1220` — `retryable := rp.cleanupPending`; `processor/rule/processor.go:1231` — `rp.cleanupPending, rp.terminal = false, true`.
Its cleanup fences runtime mutations before snapshots, then joins cron, drains message inputs, closes watcher admission, waits for callbacks/borrows, closes the coalescer, cancels continuing work, joins owner runtime and closes cache. Evidence: `processor/rule/processor.go:1248` — `barrier := rp.commandLane.fence()`; `processor/rule/processor.go:1255` — `stopErrors := []error{settleRuntimeCommandFence(ctx, barrier, cancel, coordinatorDone)}`; `processor/rule/processor.go:1270` — `stopErrors = append(stopErrors, cronScheduler.Stop(ctx))`; `processor/rule/processor.go:1289` — `if err := watcher.Stop(); err != nil && !errors.Is(err, nats.ErrBadSubscription) {`; `processor/rule/processor.go:1313` — `if err := awaitEntityBorrowSettlement(ctx, entityBorrowDone, cancel); err != nil {`; `processor/rule/processor.go:1317` — `if err := rp.closeEntityEvaluationQueue(); err != nil {`; `processor/rule/processor.go:1325` — `for _, done := range []<-chan struct{}{statusLoopDone, runtimeDone} {`; `processor/rule/processor.go:1341` — `if err := messageCache.Close(); err != nil {`.
Processor has concrete limits on a blanket deadline claim. Native watcher Stop and cache Close take no caller context. The runtime-fence expiry arm deliberately receives the barrier and coordinator completion after cancellation; its comment identifies a command ignoring its own context as the residual blocker: `processor/rule/processor.go:1415` — `// the only blocker left is a command ignoring its own ctx.`; `processor/rule/processor.go:1419` — `barrierErr := <-barrier`; `processor/rule/processor.go:1421` — `<-coordinatorDone`. Hybrid and TTL cache Close have their own five-second waits, not the caller deadline: `pkg/cache/hybrid.go:289` — `case <-time.After(5 * time.Second):`; `pkg/cache/ttl.go:262` — `case <-time.After(5 * time.Second):`.
CronScheduler fences dispatch, requests native cron stop, then observes native callbacks, the dispatch barrier and dispatcher completion under the caller context: `processor/rule/cron_scheduler.go:414` — `barrier := s.dispatch.fence()`; `processor/rule/cron_scheduler.go:415` — `nativeStop := s.cron.Stop()`; `processor/rule/cron_scheduler.go:418` — `err := s.awaitStop(ctx, nativeStop.Done(), barrier, cancel)`; `processor/rule/cron_scheduler.go:455` — `if dispatchDone := s.dispatch.done(); dispatchDone != nil {`. It sets completed state only on nil return: `processor/rule/cron_scheduler.go:422` — `s.stopped = err == nil`. Its documented later-Stop join retry is concrete scheduler behavior; it does not create a portable Processor rejoin contract.
graph-ingest Stop has separate failed-Start pending-cleanup semantics. Cleanup preserves callback/submission/pool authority until native consumer closure, drains subscriptions, then cancels and joins status work and closes caches (search record 40, source component.go:1065–1171). This collaborator cannot be treated as a rule Processor merely because both expose Stop(ctx).
Finite supplied context is not wall-clock interruption, native return, or completed join. That limitation is explicit in current `test-cleanup-policy`, `component-lifecycle`, and `runtime-context-ownership` specs read in search records 1 and 5. No test execution or new production-path proof was performed for this synthesis.

### Exact reviewed-resolution exposure

Five existing resolutions reference the touched rule surface, separately from the 24 debt roots. Preserve their full source dependency fingerprints; membership is not automatic permission to refresh them:

| Resolution index | Existing resolved site |
|---|---|
| 0 | `processor/rule/config_validation_test.go\|TestValidateExpressionRule_RejectsRuleOpaqueField\|defer\|vocabulary.SnapshotRegistry()\|unresolved callback\|unknown\|1` |
| 1 | `processor/rule/lifecycle_integration_test.go\|(*ruleLifecycleObservation).Stop\|ordinary\|github.com/c360studio/semstreams/component.Stop\|github.com/c360studio/semstreams/component.LifecycleComponent\|unknown\|1` |
| 2 | `processor/rule/lifecycle_integration_test.go\|(*ruleLifecycleObservation).Stop\|ordinary\|github.com/c360studio/semstreams/component.Stop\|github.com/c360studio/semstreams/component.LifecycleComponent\|unknown\|2` |
| 3 | `processor/rule/lifecycle_runtime_test.go\|TestRuleMessageCacheOneGuard\|defer\|cancelStart\|unresolved callback\|unknown\|1` |
| 4 | `processor/rule/lifecycle_runtime_test.go\|TestRuleStopDeadlineArmCancelsAndJoinsCoordinator\|defer\|cancelStart\|unresolved callback\|unknown\|1` |

## Surface inventory category 3 — adjacent claims and regressions

#1062 records the readiness cleanup hang and the causal substrate-before-component teardown ordering. Current regression anchors: `processor/rule/readiness_integration_test.go:65` — `func TestIntegration_RuleReadiness_EmptyReplayIsAuthoritativelyNothingToDo(t *testing.T) {`; `processor/rule/readiness_integration_test.go:116` — `func TestIntegration_RuleReadiness_NonEmptyReplayReportsScope(t *testing.T) {`; `processor/rule/readiness_integration_test.go:171` — `func TestIntegration_RuleStopAfterAcceptedStartParentCancellation(t *testing.T) {`. Source record 28 and live-claim record 3 preserve the evidence; the historical unbounded count is not the current 24-root measure.
#1283 is the orphaned fence/owner-lane regression, with deadline-arm joining and cache/watcher lifetime checks. Preserve `processor/rule/owner_lane_test.go:15` — `func TestRuleRuntimeLaneFenceAfterLastDrainSettles(t *testing.T) {`; `processor/rule/owner_lane_test.go:66` — `func TestRuleRuntimeFenceOnUnstartedLaneSelfSettles(t *testing.T) {`; `processor/rule/lifecycle_runtime_test.go:182` — `func TestRuleStopDeadlineArmCancelsAndJoinsCoordinator(t *testing.T) {`; `processor/rule/lifecycle_runtime_test.go:214` — `func TestRuleMessageCacheOneGuard(t *testing.T) {`; `processor/rule/lifecycle_runtime_test.go:244` — `func TestRuleManagedWatcherSpawnRefusedAfterRuntimeEnd(t *testing.T) {`. Its deliberate ordinary Background calls are not interchangeable with cleanup debt.
#1404 already repaired five former rule baseline roots. Its archive is `openspec/changes/archive/2026-09-28-rule-bounded-stop-fence/` (source record 41). Existing bounded helper evidence: `processor/rule/lifecycle_owner_test.go:24` — `const processorStopBudget = 30 * time.Second`; `processor/rule/lifecycle_owner_test.go:28` — `func stopProcessorWithinBudget(t *testing.T, proc *Processor) {`. This batch does not relitigate that production fix or weaken its regression expectations.
Deliberate scheduler probes include nil Stop, never-started/repeated Stop, concurrent Stop, and admitted dispatch settlement: `processor/rule/cron_scheduler_test.go:262` — `func TestCronScheduler_StopOnNeverStartedIsSafe(t *testing.T) {`; `processor/rule/cron_scheduler_test.go:276` — `func TestCronScheduler_StopRejectsNilContext(t *testing.T) {`; `processor/rule/cron_scheduler_test.go:286` — `func TestCronScheduler_StandaloneStartContextAndStopSettlement(t *testing.T) {`. Preserve their inputs and observable contract distinctions.
#1417 owns remaining test-only package batches; #1293 and #1411/#1412 retain separate verification and production responsibilities. #1416 remains open for owner disposition. The retrieved multi-issue output is truncated; it is not a complete current claim ledger.
#1421 remains the graph-index required-Test flake gate. The recorded waiver comment `5890540170` was limited to #1404; it does not authorize #1429 merge. Coordinator gate state: #1429 is held for a fix or an explicit owner waiver while rule work continues without graph-index scope expansion.
Claude owns #1426 / #1427 E2E warn-only assertion work. Those claims do not transfer to this batch.
ADR-031 places cron in the rule processor and identifies product consumers; its historic options are not options offered by this inventory. Current rule-engine spec concerns evaluation, while cleanup and ownership obligations are carried by the three specs above.

## Surface inventory category 4 — consumer at birth

No exported symbol, port, subject, bucket, config field, or runtime primitive is introduced in this inventory-phase claim. The present consumers measured here are existing rule tests and four cleanup-owning helpers. Category 4 is non-triggered by the claim, not an unsupported assertion that no external consumer exists. The broad Stop reference queries include other repository packages; those are adjacency, not additional #1428 repair scope.

## Surface inventory category 5 — closest existing problem shape

The problem shape is lexical test-fixture ownership across acquisition, fallible setup, optional explicit terminal fences, ownership transfer and substrate teardown. Existing instances already handle much of this shape:

Shared-support #1419: a case-local owner stores cancellation and terminal-attempt state; it records concrete Stop and caller-bound errors separately and suppresses implicit repeat attempts: `component/lifecycle_test_suite.go:29` — `type lifecycleTestOwner struct {`; `component/lifecycle_test_suite.go:65` — `o.attempted = true // A returned error or panic does not authorize an implicit retry.`; `component/lifecycle_test_suite.go:67` — `o.stopBoundErr = stopCtx.Err()`; `component/lifecycle_test_suite.go:85` — `func (o *lifecycleTestOwner) finish(workCtx context.Context, abort bool) error {`.
Graph-ingest #1424: an owner has private Start cancellation, terminal-attempt and transfer state; its Stop observes terminal expiry and operation expiry, and provisional finalization is conditional on transfer: `processor/graph-ingest/test_owner_support_test.go:11` — `type graphIngestTestOwner struct {`; `processor/graph-ingest/test_owner_support_test.go:32` — `o.attempted = true // A returned error or panic never grants an implicit second attempt.`; `processor/graph-ingest/test_owner_support_test.go:36` — `stopCtx, cancelStop := context.WithTimeout(context.WithoutCancel(operationCtx), 5*time.Second)`; `processor/graph-ingest/test_owner_support_test.go:58` — `func (o *graphIngestTestOwner) provisionalFinish(operationCtx context.Context, t *testing.T) {`; `processor/graph-ingest/test_owner_support_test.go:65` — `func (o *graphIngestTestOwner) transfer() {`.

These are measured existing shapes, not an adoption choice. Their budgets, abort expectations, authority and retry rules cannot be presumed identical to all three concrete owner classes. No new reusable pattern is proposed; the establishing-pattern adoption sweep and same-class durable/communication/runtime-primitive collision table are not triggered at this inventory checkpoint.

## Adopter seam inventory

Existing outward-facing lifecycle contracts are reached by a developer composing a component or writing a factory without reading its implementation. This test-only claim introduces no new outward surface, but exposes existing lifecycle assumptions.

1. **What must they know?** A live accepted Start parent owns continuing work; controlled Stop needs separate finite authority before Start cancellation; native callback/substrate resources must remain available through their drain; a timeout does not imply a complete join; terminal errors are observable; completed repeated Stop differs from failed-Start retry and unsupported running-generation rejoin. These are more than two independent knowledge debts, documented by the current specs and concrete native paths above.
2. **What happens if they do nothing?** In the baseline helpers, successful Start is followed by later testing cleanup registration, some Stop errors are discarded, and fallible setup before registration can exit without that callback. Testing-context-derived Start authority is not guaranteed live when registered cleanup executes. In a component composition, ending Start first produces abort cleanup rather than controlled success. The native implementations can return accurate errors and cannot promise completed joins when caller bounds win.
3. **Where do they find out?** Invalid nil context is a runtime error; cancellation/deadline failures are returned by the terminal call; some current helper callbacks discard that evidence. Compile-time context types alone do not enforce authority order, native completion, or substrate lifetime. Order and join limits are currently specified in lifecycle/cleanup contracts and exercised by focused tests.
4. **What should they have to know / observed gap?** A fixture author should have an observable, attributed terminal outcome for each acquired owner. The gap is the set of ownership/order/attempt facts they currently reconstruct across helper and caller scopes. The inventory does not select how to close that gap. Deadline selection is caller prediction; observing the actual terminal error and native completion is separate evidence.

No sister repository was inspected in this bounded synthesis. No statement about sister adoption or absence of external consumers follows from the external `rule_test` package or broad in-repository gopls output. If a later design changes an exported surface, sister inventory remains necessary before design acceptance.

## Search index and provenance

All 55 original command/result records are retained in the raw archive. Indexes below are zero-based positions in `gh1428-search-log.json`; exact commands and complete available outputs are the audit record.

| Records | Scope |
|---|---|
| 0–3 | Contract, project/spec/change reads, baseline and tracked-source accounting |
| 4, 8, 30–32, 39, 46 | gopls declarations for Stop, queue closure, Start, helper variants, coalescer and NATS factory |
| 5, 9–11, 14–16, 27–29, 33–34, 40–41, 44, 47–50 | Source/spec/ADR ranges, root/package/build annotations and helper caller annotation |
| 12–13 | `GOFLAGS=-tags=integration gopls references processor/rule/processor.go:1191:22` and scheduler `:381:25`; broad adjacency only |
| 17–25, 37–38, 51–54 | 15 raw helper reference queries; default/integration selections, not test execution |
| 6, 26, 35–36, 42, 45 | Issue/PR claims; record 26 failed network retrieval, 45 has truncation |
| 7 | `git grep -n -E '#1062|#1283|#1404|#1416|#1417|#1293|#1411|#1412|#1426|#1427' -- openspec/changes openspec/specs docs/contributing processor/rule` |
| 29 | `git grep -n -E 'CronScheduler|cron scheduler|cron scheduling|#1062' -- openspec/specs docs/adr docs/contributing/01-testing.md` |
| 43 | Historical #1062 commit lookup and testing-policy literal search; no additional history search in this synthesis |

This synthesis re-read only supplied records, required project/change documents, and already-recorded source ranges at the frozen base for exact pins. It ran no fresh gopls/source search, GitHub request, test, Docker operation, or repository mutation. No zero-hit search was converted into an absence claim.

## Initial inventory gaps — checkpoint 8a305ed1

1. **Independent re-derivation outstanding.** This is packaging of prior evidence, not independent INVENTORY PASS. All completeness claims remain open to the reviewer.
2. **Per-case ownership ledger incomplete.** The exact 24 roots and helper references are enumerated; the supplied annotations do not constitute a complete semantic ledger of every caller's acquired resources, fatal setup exits, Start/operation authority, transfer point, explicit terminal attempt, join observation and substrate teardown order. That gap must remain visible when judging inventory acceptance.
3. **Four raw-only helper queries.** The later integration references for constructor, unstarted constructor, tracker and metrics helpers (search 51–54) are in raw evidence but absent from the 11-query annotated companion. The 37-call dedup claim is narrower and verified above.
4. **Native dependency limits.** The records show contextless native watcher/cache operations and the post-deadline owner-lane receives. They do not prove cancellation responsiveness for every dependency or every admitted action. No complete wall-clock/owned-join proof can be inferred.
5. **Claim-record incompleteness.** `gh1428-adjacent-claims.json` contains network errors, not issue absence; search record 45 is truncated. Coordinator-provided gate/ownership state is explicitly attributed above and is not newly verified here.
6. **Source execution status.** Build tags and recorded `skip_lines` are annotations only; empty local skip lists do not prove all enclosing paths execute. No skipped-source or execution-success claim is made.
7. **External adopter enumeration absent.** No exported change is proposed, but any later outward-facing design needs its own outside-repository adopter seam evidence; in-repo external test packages do not satisfy that obligation.

## Completion supplement and current review boundary

The initial checkpoint above is preserved at `8a305ed1`; its review is retained verbatim in
`review/inventory-review-initial.md`. It requested one bounded correction: a per-case ownership ledger.
The current supplement is `review/ownership-ledger.md`, SHA256
`1b74ac431316ec3d945780bbab0768b0b6a3072c856960609d931623effbb1b6`.
It maps B00–B23 to all 24 exact roots and H01–H37 to all 37 physical lifecycle-helper calls.
It records acquisition, fallible exits, authority, explicit attempts, transfer, substrate ordering and actual
completion observations. Shared source facts are factored without choosing a target-state abstraction.
Its mechanical pin companion is `review/ownership-ledger-pins.md`; exact identities and hashes for this supplement
are in `review/evidence/inventory-completion-manifest.json`.

`review/helper-query-supplement.json` closes the four raw-only query annotations: 45 integration records with exact
physical parity to the earlier default sets. These records add no lifecycle-helper callers.
The coordinator successfully retrieved all 14 bounded adjacent issue/PR records on 2026-09-29; complete bytes and
hashes are in `review/evidence/verified-claims.zip` and its manifest. Earlier retrieval failures remain historical
failed evidence, not claims of absence. This confirms #1421 remains open, its waiver is limited to #1404, #1429 is
an open draft, #1416 remains open for its recorded disposition, and Claude owns the separate #1426/#1427 work.

Initial gaps 2, 3 and 5 now have concrete supplemental evidence. Gap 1 remains pending the independent final review.
Gaps 4, 6 and 7 remain explicit evidence limits: no universal native deadline/join proof, no execution claim, and no
external-adopter claim for a new exported surface. Those are not additional repair populations or authority to
expand this test-only batch. No design or implementation authorization is issued by this inventory supplement.

## Verifier source pins

The following pins duplicate the inline evidence in the canonical verifier grammar.

- `processor/rule/actions_run_scope_integration_test.go:130` — `_ = ingest.Stop(context.Background())`
- `processor/rule/cron_scheduler_integration_test.go:87` — `t.Cleanup(func() { _ = proc.Stop(context.Background()) })`
- `processor/rule/cron_scheduler_test.go:585` — `if err := s.Stop(context.Background()); err != nil {`
- `processor/rule/cron_scheduler_test.go:245` — `if err := s.Stop(context.Background()); err != nil {`
- `processor/rule/cron_scheduler_test.go:100` — `if err := scheduler.Stop(context.Background()); err != nil {`
- `processor/rule/entity_watcher_debounce_integration_test.go:202` — `defer processor.Stop(context.Background())`
- `processor/rule/entity_watcher_debounce_integration_test.go:324` — `defer processor.Stop(context.Background())`
- `processor/rule/entity_watcher_debounce_integration_test.go:390` — `defer processor.Stop(context.Background())`
- `processor/rule/entity_watcher_debounce_integration_test.go:118` — `defer processor.Stop(context.Background())`
- `processor/rule/entity_watcher_debounce_integration_test.go:70` — `defer processor.Stop(context.Background())`
- `processor/rule/entity_watcher_debounce_integration_test.go:158` — `defer processor.Stop(context.Background())`
- `processor/rule/entity_watcher_debounce_integration_test.go:280` — `defer processor.Stop(context.Background())`
- `processor/rule/entity_watcher_hardening_integration_test.go:75` — `_ = processor.Stop(context.Background())`
- `processor/rule/entity_watcher_integration_test.go:411` — `processor.Stop(context.Background())`
- `processor/rule/entity_watcher_integration_test.go:122` — `processor.Stop(context.Background())`
- `processor/rule/rule_integration_test.go:247` — `defer processor.Stop(context.Background())`
- `processor/rule/rule_integration_test.go:541` — `defer processor.Stop(context.Background())`
- `processor/rule/rule_integration_test.go:681` — `defer processor.Stop(context.Background())`
- `processor/rule/rule_integration_test.go:147` — `defer processor.Stop(context.Background())`
- `processor/rule/rule_integration_test.go:447` — `defer processor.Stop(context.Background())`
- `processor/rule/rule_integration_test.go:831` — `defer processor.Stop(context.Background())`
- `processor/rule/state_cleanup_integration_test.go:47` — `t.Cleanup(func() { _ = proc.Stop(context.Background()) })`
- `processor/rule/stateful_integration_test.go:44` — `defer processor.Stop(context.Background())`
- `processor/rule/triple_mutator_revision_integration_test.go:74` — `_ = ingest.Stop(context.Background())`
- `processor/rule/cron_scheduler_test.go:94` — `func startSchedulerForTest(ctx context.Context, t *testing.T, scheduler *CronScheduler) {`
- `processor/rule/cron_scheduler_test.go:76` — `func newSchedulerForTest(t *testing.T, exec ActionExecutorInterface) *CronScheduler {`
- `processor/rule/cron_scheduler_test.go:82` — `func newUnstartedSchedulerForTest(t *testing.T, exec ActionExecutorInterface) *CronScheduler {`
- `processor/rule/cron_scheduler_integration_test.go:60` — `func startCronProcessorForTest(t *testing.T, natsClient *natsclient.Client, rules []Definition) (*Processor, *metric.MetricsRegistry) {`
- `processor/rule/cron_scheduler_integration_test.go:41` — `func getIntegrationNATSClient(t *testing.T) *natsclient.Client {`
- `processor/rule/actions_run_scope_integration_test.go:108` — `func newRunScopeHarness(t *testing.T) *runScopeHarness {`
- `processor/rule/triple_mutator_revision_integration_test.go:46` — `func newRevisionClaimHarness(t *testing.T) *revisionClaimHarness {`
- `processor/rule/rule_integration_test.go:31` — `func getTestNATSClient(t *testing.T) *natsclient.Client {`
- `processor/rule/cron_scheduler_test.go:627` — `func newSchedulerWithTrackerForTest(t *testing.T, exec ActionExecutorInterface) (*CronScheduler, *ScheduleTracker) {`
- `processor/rule/cron_scheduler_test.go:937` — `func newSchedulerWithMetricsForTest(t *testing.T, exec ActionExecutorInterface) (*CronScheduler, *cronMetrics) {`
- `processor/rule/cron_scheduler_test.go:96` — `if err := scheduler.Start(ctx); err != nil {`
- `processor/rule/cron_scheduler_test.go:99` — `t.Cleanup(func() {`
- `processor/rule/cron_scheduler_integration_test.go:76` — `proc, err := NewProcessorWithMetrics(natsClient, &cfg, registry)`
- `processor/rule/cron_scheduler_integration_test.go:84` — `ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)`
- `processor/rule/cron_scheduler_integration_test.go:85` — `t.Cleanup(cancel)`
- `processor/rule/actions_run_scope_integration_test.go:110` — `ctx, cancel := context.WithCancel(t.Context())`
- `processor/rule/actions_run_scope_integration_test.go:126` — `require.NoError(t, ingest.Initialize())`
- `processor/rule/actions_run_scope_integration_test.go:128` — `require.NoError(t, testClient.GetNativeConnection().Flush())`
- `processor/rule/triple_mutator_revision_integration_test.go:48` — `ctx, cancel := context.WithCancel(t.Context())`
- `processor/rule/triple_mutator_revision_integration_test.go:70` — `require.NoError(t, ingest.Initialize())`
- `processor/rule/triple_mutator_revision_integration_test.go:72` — `require.NoError(t, testClient.GetNativeConnection().Flush())`
- `processor/rule/cron_scheduler_integration_test.go:235` — `require.NoError(t, proc1.Stop(context.Background()))`
- `processor/rule/cron_scheduler_integration_test.go:315` — `require.NoError(t, proc1.Stop(context.Background()))`
- `processor/rule/cron_scheduler_integration_test.go:329` — `require.NoError(t, proc2.Stop(context.Background()))`
- `natsclient/test_client.go:862` — `// Register cleanup`
- `processor/rule/cron_scheduler_integration_test.go:52` — `t.Cleanup(func() { testClient.Terminate() })`
- `processor/rule/processor.go:1191` — `func (rp *Processor) Stop(ctx context.Context) error {`
- `processor/rule/processor.go:1220` — `retryable := rp.cleanupPending`
- `processor/rule/processor.go:1231` — `rp.cleanupPending, rp.terminal = false, true`
- `processor/rule/processor.go:1248` — `barrier := rp.commandLane.fence()`
- `processor/rule/processor.go:1255` — `stopErrors := []error{settleRuntimeCommandFence(ctx, barrier, cancel, coordinatorDone)}`
- `processor/rule/processor.go:1270` — `stopErrors = append(stopErrors, cronScheduler.Stop(ctx))`
- `processor/rule/processor.go:1289` — `if err := watcher.Stop(); err != nil && !errors.Is(err, nats.ErrBadSubscription) {`
- `processor/rule/processor.go:1313` — `if err := awaitEntityBorrowSettlement(ctx, entityBorrowDone, cancel); err != nil {`
- `processor/rule/processor.go:1317` — `if err := rp.closeEntityEvaluationQueue(); err != nil {`
- `processor/rule/processor.go:1325` — `for _, done := range []<-chan struct{}{statusLoopDone, runtimeDone} {`
- `processor/rule/processor.go:1341` — `if err := messageCache.Close(); err != nil {`
- `processor/rule/processor.go:1415` — `// the only blocker left is a command ignoring its own ctx.`
- `processor/rule/processor.go:1419` — `barrierErr := <-barrier`
- `processor/rule/processor.go:1421` — `<-coordinatorDone`
- `pkg/cache/hybrid.go:289` — `case <-time.After(5 * time.Second):`
- `pkg/cache/ttl.go:262` — `case <-time.After(5 * time.Second):`
- `processor/rule/cron_scheduler.go:414` — `barrier := s.dispatch.fence()`
- `processor/rule/cron_scheduler.go:415` — `nativeStop := s.cron.Stop()`
- `processor/rule/cron_scheduler.go:418` — `err := s.awaitStop(ctx, nativeStop.Done(), barrier, cancel)`
- `processor/rule/cron_scheduler.go:455` — `if dispatchDone := s.dispatch.done(); dispatchDone != nil {`
- `processor/rule/cron_scheduler.go:422` — `s.stopped = err == nil`
- `processor/rule/readiness_integration_test.go:65` — `func TestIntegration_RuleReadiness_EmptyReplayIsAuthoritativelyNothingToDo(t *testing.T) {`
- `processor/rule/readiness_integration_test.go:116` — `func TestIntegration_RuleReadiness_NonEmptyReplayReportsScope(t *testing.T) {`
- `processor/rule/readiness_integration_test.go:171` — `func TestIntegration_RuleStopAfterAcceptedStartParentCancellation(t *testing.T) {`
- `processor/rule/owner_lane_test.go:15` — `func TestRuleRuntimeLaneFenceAfterLastDrainSettles(t *testing.T) {`
- `processor/rule/owner_lane_test.go:66` — `func TestRuleRuntimeFenceOnUnstartedLaneSelfSettles(t *testing.T) {`
- `processor/rule/lifecycle_runtime_test.go:182` — `func TestRuleStopDeadlineArmCancelsAndJoinsCoordinator(t *testing.T) {`
- `processor/rule/lifecycle_runtime_test.go:214` — `func TestRuleMessageCacheOneGuard(t *testing.T) {`
- `processor/rule/lifecycle_runtime_test.go:244` — `func TestRuleManagedWatcherSpawnRefusedAfterRuntimeEnd(t *testing.T) {`
- `processor/rule/lifecycle_owner_test.go:24` — `const processorStopBudget = 30 * time.Second`
- `processor/rule/lifecycle_owner_test.go:28` — `func stopProcessorWithinBudget(t *testing.T, proc *Processor) {`
- `processor/rule/cron_scheduler_test.go:262` — `func TestCronScheduler_StopOnNeverStartedIsSafe(t *testing.T) {`
- `processor/rule/cron_scheduler_test.go:276` — `func TestCronScheduler_StopRejectsNilContext(t *testing.T) {`
- `processor/rule/cron_scheduler_test.go:286` — `func TestCronScheduler_StandaloneStartContextAndStopSettlement(t *testing.T) {`
- `component/lifecycle_test_suite.go:29` — `type lifecycleTestOwner struct {`
- `component/lifecycle_test_suite.go:65` — `o.attempted = true // A returned error or panic does not authorize an implicit retry.`
- `component/lifecycle_test_suite.go:67` — `o.stopBoundErr = stopCtx.Err()`
- `component/lifecycle_test_suite.go:85` — `func (o *lifecycleTestOwner) finish(workCtx context.Context, abort bool) error {`
- `processor/graph-ingest/test_owner_support_test.go:11` — `type graphIngestTestOwner struct {`
- `processor/graph-ingest/test_owner_support_test.go:32` — `o.attempted = true // A returned error or panic never grants an implicit second attempt.`
- `processor/graph-ingest/test_owner_support_test.go:36` — `stopCtx, cancelStop := context.WithTimeout(context.WithoutCancel(operationCtx), 5*time.Second)`
- `processor/graph-ingest/test_owner_support_test.go:58` — `func (o *graphIngestTestOwner) provisionalFinish(operationCtx context.Context, t *testing.T) {`
- `processor/graph-ingest/test_owner_support_test.go:65` — `func (o *graphIngestTestOwner) transfer() {`
