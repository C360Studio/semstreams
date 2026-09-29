# Shared lifecycle test cleanup inventory
base: 926677874711f888cb06c86584484996a001d9f6

Status: INVENTORY ONLY; independent review pending. No target state, API choice, timeout choice, baseline approval,
or production change is selected. Claim: #1418 / draft PR #1419, child of #1417. Branch:
`codex/gh1418-shared-test-cleanup`; base main `bb98043a5129f958a440ab15a7ee42243c575848`.

## Scope and measurement boundary

Read-only source inventory of shared component lifecycle support, its callers, real rule fixture, cleanup substrate,
and exact guard manifest exposure. No tests, full census, stress run, Docker operation, or source mutation was performed.
A passing earlier guard is not new lifecycle completion evidence. Local sister repositories were read-only.

Current contract/project, active proposal and tasks, component-lifecycle, runtime-context-ownership, test-cleanup-policy,
and the full testing policy were read. `pkg/lifecycle`/ADR-049 names a graph domain lifecycle harness; its name does not
make it a shared component-test cleanup helper. No new durable/runtime primitive is proposed in this phase.

## 1. Claimed gap and existing ownership paths

- `component/lifecycle_test_suite.go:16` — `type LifecycleFactory func() LifecycleComponent`
- `component/lifecycle_test_suite.go:21` — `func StandardLifecycleTests(t *testing.T, factory LifecycleFactory) {`
- `component/lifecycle_test_suite.go:52` — `comp := factory()`
- `component/lifecycle_test_suite.go:169` — `comp := factory()`

The exported factory accepts no testing owner or context and returns no error. StandardLifecycleTests invokes it in
seven portable cases, two pre-ended Start cases, 10 workers × 20 fresh instances, and 50 resource-check iterations:
259 creations on the ordinary non-short path, nine in short mode. A shared closure may therefore run concurrently.
The suite itself registers no t.Cleanup for these components. Factories can panic or capture a parent testing owner;
the API has no explicit acquisition-failure or ownership-transfer result.

| Existing path | Current owner/authority and terminal behavior | Failure/exit exposure |
|---|---|---|
| Initialize / nil Start / nil Stop | New component per case; assertions test the declared contract | No separate base finalizer in the suite |
| Controlled Stop | Background-derived Start child; separate 5s Stop; Start cancel deferred until function return | Fatal Initialize/Start/Stop assertion exits before later operations; no registered component fallback |
| Accepted parent cancellation | Start accepted, then explicitly canceled; separate 5s synchronous Stop; nonnil error logged | Failed Start assertion occurs before cancelStart is invoked; no defer for that cancel; deadline supply is not return proof |
| Completed repeat | First and second 5s Stop; Start cancel deferred | Fatal first Stop skips explicit cancelFirst; repeated Stop assumes completed first call |
| Pre-canceled/pre-expired Start | Assert exact Start error, then separate 5s Stop | Earlier fatal assertion skips that safe Stop |
| Parallel fresh instances | Worker-local Start; 5s synchronous Stop; explicit cancels; buffered errors; parent waits for every worker | Initialize failure continues without Stop; Start failure cancels and continues without Stop; Stop ignoring ctx blocks wg.Wait |
| NoLeaks | 50 fresh components; successful Start remains live during 5s Stop, then canceled | Initialize error logs and continues without Stop; Start/Stop errors merely log; aggregate process counts cannot identify exact ownership |
| ErrorInjection | Factory base wrapped in injected-error component; three contract cases and unconditional finalizer | Stop injection short-circuits both the asserted operation and finalizer, so neither reaches base Stop |
| Benchmarks | Four exported benchmark modes; fresh Start/Stop authority per applicable iteration | Initialize/Start/Stop results discarded; no separate registered finalizer |

- `component/lifecycle_test_suite.go:60` — `require.NoError(t, comp.Initialize(), "Initialize should succeed on a fresh component")`
- `component/lifecycle_test_suite.go:65` — `startCtx, cancelStart := context.WithCancel(context.Background())`
- `component/lifecycle_test_suite.go:66` — `defer cancelStart()`
- `component/lifecycle_test_suite.go:67` — `require.NoError(t, comp.Start(startCtx))`
- `component/lifecycle_test_suite.go:71` — `require.NoError(t, comp.Stop(stopCtx))`
- `component/lifecycle_test_suite.go:78` — `require.NoError(t, comp.Start(startCtx))`
- `component/lifecycle_test_suite.go:79` — `cancelStart()`
- `component/lifecycle_test_suite.go:85` — `stopErr = comp.Stop(stopCtx)`
- `component/lifecycle_test_suite.go:88` — `t.Logf("abort Stop accurately reported terminal cleanup: %v", stopErr)`
- `component/lifecycle_test_suite.go:102` — `require.NoError(t, comp.Stop(firstCtx))`
- `component/lifecycle_test_suite.go:103` — `cancelFirst()`
- `component/lifecycle_test_suite.go:126` — `comp := factory()`
- `component/lifecycle_test_suite.go:131` — `require.ErrorIs(t, comp.Start(startCtx), context.Canceled)`
- `component/lifecycle_test_suite.go:132` — `requireSafeStopAfterRejectedStart(t, comp)`
- `component/lifecycle_test_suite.go:174` — `if err := comp.Initialize(); err != nil {`
- `component/lifecycle_test_suite.go:179` — `if err := comp.Start(startCtx); err != nil {`
- `component/lifecycle_test_suite.go:180` — `cancelStart()`
- `component/lifecycle_test_suite.go:184` — `stopCtx, cancelStop := context.WithTimeout(context.Background(), 5*time.Second)`
- `component/lifecycle_test_suite.go:185` — `err := comp.Stop(stopCtx)`
- `component/lifecycle_test_suite.go:187` — `cancelStart()`
- `component/lifecycle_test_suite.go:193` — `wg.Wait()`
- `component/lifecycle_test_suite.go:220` — `err := comp.Initialize()`
- `component/lifecycle_test_suite.go:223` — `continue`
- `component/lifecycle_test_suite.go:227` — `err = comp.Start(startCtx)`
- `component/lifecycle_test_suite.go:229` — `t.Logf("Start failed on iteration %d: %v", i, err)`
- `component/lifecycle_test_suite.go:232` — `stopCtx, cancelStop := context.WithTimeout(context.Background(), 5*time.Second)`
- `component/lifecycle_test_suite.go:233` — `err = comp.Stop(stopCtx)`
- `component/lifecycle_test_suite.go:236` — `t.Logf("Stop failed on iteration %d: %v", i, err)`
- `component/lifecycle_test_suite.go:239` — `cancelStart()`
- `component/lifecycle_test_suite.go:259` — `if growth > 50*1024*1024 {`
- `component/lifecycle_test_suite.go:266` — `if goroutineGrowth > 10 {`
- `component/lifecycle_test_suite.go:283` — `_ = comp.Stop(stopCtx)`
- `component/lifecycle_test_suite.go:296` — `_ = comp.Stop(stopCtx)`
- `component/lifecycle_test_suite.go:311` — `_ = comp.Stop(stopCtx)`
- `component/lifecycle_test_suite.go:326` — `_ = comp.Stop(stopCtx)`
- `component/lifecycle_test_suite.go:384` — `func (e *ErrorInjectingComponent) Stop(ctx context.Context) error {`
- `component/lifecycle_test_suite.go:385` — `if e.injectStopError {`
- `component/lifecycle_test_suite.go:386` — `return e.stopError`
- `component/lifecycle_test_suite.go:427` — `baseComp := factory()`
- `component/lifecycle_test_suite.go:436` — `comp.Initialize() // Ensure component is initialized`
- `component/lifecycle_test_suite.go:443` — `comp.Start(ctx)`
- `component/lifecycle_test_suite.go:444` — `cancel()`
- `component/lifecycle_test_suite.go:445` — `err = comp.Stop(context.Background())`
- `component/lifecycle_test_suite.go:455` — `comp.Stop(context.Background())`

The NoLeaks call at :233 precedes cancelStart at :239. It is a controlled Stop when Start succeeded; #1416’s claim
that NoLeaks itself is already on the abort path is not true for that success path. Its later adopter fallback is
after cancellation. The finalizer at :455 is semantically separate from the injected-error operation at :445 even
though both are ordinary syntax and both currently use Background. No automatic debt-count reduction follows from
fixing an ordinary finalizer outside the guard’s deferred/Cleanup debt population.

## 2. All current API spellings and observed adopters

The exposed support surface is LifecycleFactory, StandardLifecycleTests, BenchmarkLifecycleMethods,
ErrorInjectingComponent/NewErrorInjectingComponent, its three Inject methods and lifecycle forwarding methods,
and TestErrorInjection. It is in an ordinary .go file and therefore importable outside this repository.
Default gopls references found HTTP and UDP. Repeating with GOFLAGS=-tags=integration adds graph-index and rule.
No other StandardLifecycleTests references were returned in those selected package graphs. Benchmark and
TestErrorInjection each have one observed WebSocket caller. No new symbol is proposed, so no consumer-at-birth claim
exists for a hypothetical replacement API. Present consumers are listed rather than predicting future ones.

- `gateway/http/http_lifecycle_test.go:57` — `component.StandardLifecycleTests(t, createTestComponentForLifecycle)`
- `input/udp/udp_lifecycle_test.go:88` — `component.StandardLifecycleTests(t, createTestComponent)`
- `processor/graph-index/lifecycle_integration_test.go:78` — `component.StandardLifecycleTests(t, createTestComponentForLifecycle)`
- `processor/rule/lifecycle_integration_test.go:58` — `component.StandardLifecycleTests(t, func() component.LifecycleComponent {`
- `output/websocket/websocket_test.go:1117` — `component.TestErrorInjection(t, createTestWebSocketOutput)`
- `output/websocket/websocket_test.go:1122` — `component.BenchmarkLifecycleMethods(b, createTestWebSocketOutput)`
- `component/lifecycle_test_suite.go:276` — `func BenchmarkLifecycleMethods(b *testing.B, factory LifecycleFactory) {`
- `component/lifecycle_test_suite.go:334` — `type ErrorInjectingComponent struct {`
- `component/lifecycle_test_suite.go:345` — `func NewErrorInjectingComponent(comp LifecycleComponent) *ErrorInjectingComponent {`
- `component/lifecycle_test_suite.go:392` — `func TestErrorInjection(t *testing.T, factory LifecycleFactory) {`

| Adopter | Factory and substrate facts |
|---|---|
| gateway/http | Panic-on-error factory, unconnected NATS client, no factory cleanup registration |
| input/udp | Reserves then closes an ephemeral UDP socket and later constructs the input for that port; panic-on-error; mock client |
| processor/graph-index | Integration TestMain shared NATS owner; factory uses CreateGraphIndex/default config; panic-on-error; TestMain terminates only after m.Run |
| processor/rule | One top-level TestClient; real production factory; mutex-protected component cohort; Errorf rather than Fatal in worker-capable factory |
| output/websocket | Error-injection/benchmark factory uses nil NATS and atomically incremented test ports; separate existing port-policy debt, not silently authorized by this change |

- `gateway/http/http_lifecycle_test.go:34` — `natsClient, err := natsclient.NewClient("nats://localhost:4222")`
- `gateway/http/http_lifecycle_test.go:43` — `comp, err := NewGateway(configJSON, deps)`
- `input/udp/udp_lifecycle_test.go:61` — `conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})`
- `input/udp/udp_lifecycle_test.go:66` — `if err := conn.Close(); err != nil {`
- `processor/graph-index/lifecycle_integration_test.go:22` — `sharedLifecycleNATSClient, err = natsclient.NewSharedTestClient(`
- `processor/graph-index/lifecycle_integration_test.go:32` — `code := m.Run()`
- `processor/graph-index/lifecycle_integration_test.go:35` — `if err := sharedLifecycleNATSClient.Terminate(); err != nil {`
- `processor/graph-index/lifecycle_integration_test.go:68` — `comp, err := CreateGraphIndex(configJSON, deps)`
- `output/websocket/websocket_test.go:804` — `port := atomic.AddUint32(&testPortCounter, 1)`
- `output/websocket/websocket_test.go:815` — `ws, err := NewOutput(port, "/test", []string{"test.subject"}, nil)`

## 3. Real rule cohort and the exact #1416 condition

The rule fixture configures a real ENTITY_STATES watch, so absence of watchers cannot be used as a cleanup argument.
One top-level server backs the coherent lifecycle test. Processors share readiness writes; the test asserts lifecycle
results, not readiness isolation. The mutex protects collection writes from concurrent factory calls. The cohort
cleanup is registered after NewTestClient, hence runs first by LIFO. It copies the cohort and synchronously Stops each
under one aggregate ten-second context, reports each returned error, and reports unattempted instances after expiry.
That establishes cleanup-call ordering, not that every Stop returned or every owner joined before NATS is dismantled.

- `processor/rule/lifecycle_integration_test.go:22` — `tc := natsclient.NewTestClient(t, natsclient.WithKVBuckets(graph.BucketEntityStates))`
- `processor/rule/lifecycle_integration_test.go:26` — `config.EntityWatchBuckets = map[string][]string{`
- `processor/rule/lifecycle_integration_test.go:37` — `var processors []component.LifecycleComponent`
- `processor/rule/lifecycle_integration_test.go:41` — `t.Cleanup(func() {`
- `processor/rule/lifecycle_integration_test.go:42` — `ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)`
- `processor/rule/lifecycle_integration_test.go:45` — `owned := append([]component.LifecycleComponent(nil), processors...)`
- `processor/rule/lifecycle_integration_test.go:48` — `if err := ctx.Err(); err != nil {`
- `processor/rule/lifecycle_integration_test.go:49` — `t.Errorf("rule lifecycle cleanup: %d processors unattempted after aggregate budget: %v", len(owned)-i, err)`
- `processor/rule/lifecycle_integration_test.go:52` — `if err := processor.Stop(ctx); err != nil {`
- `processor/rule/lifecycle_integration_test.go:53` — `t.Errorf("rule lifecycle cleanup: processor %d of %d: %v", i+1, len(owned), err)`
- `processor/rule/lifecycle_integration_test.go:59` — `created, err := rule.CreateRuleProcessor(rawConfig, deps)`
- `processor/rule/lifecycle_integration_test.go:62` — `t.Errorf("create rule lifecycle fixture: %v", err)`
- `processor/rule/lifecycle_integration_test.go:65` — `processor, ok := created.(component.LifecycleComponent)`
- `processor/rule/lifecycle_integration_test.go:71` — `processors = append(processors, processor)`

Issue #1416 is adjacent evidence, not an accepted specification. Its filed condition was: the eventual cohort Stop
runs after the suite cancels Start authority, and an earlier non-completed abort Stop may return a legitimate error
again, causing the cohort’s Errorf to fail. The issue explicitly said this had not yet been observed red.

Source at this base narrows that hypothesis:

- `processor/rule/processor.go:1191` — `func (rp *Processor) Stop(ctx context.Context) error {`
- `processor/rule/processor.go:1197` — `if !rp.lifecycleUsed {`
- `processor/rule/processor.go:1202` — `if rp.terminal {`
- `processor/rule/processor.go:1204` — `return nil`
- `processor/rule/processor.go:1206` — `if rp.startDone != nil {`
- `processor/rule/processor.go:1216` — `if rp.stopping {`
- `processor/rule/processor.go:1220` — `retryable := rp.cleanupPending`
- `processor/rule/processor.go:1224` — `stopErr := rp.cleanup(ctx)`
- `processor/rule/processor.go:1227` — `if retryable && stopErr != nil {`
- `processor/rule/processor.go:1231` — `rp.cleanupPending, rp.terminal = false, true`
- `processor/rule/processor.go:1232` — `rp.clearLifecycleHandles()`
- `processor/rule/processor.go:1241` — `return stopErr`
- `processor/rule/processor.go:1411` — `select {`
- `processor/rule/processor.go:1412` — `case barrierErr := <-barrier:`
- `processor/rule/processor.go:1413` — `return barrierErr`
- `processor/rule/processor.go:1427` — `barrierErr := <-barrier`
- `processor/rule/processor.go:1429` — `<-coordinatorDone`

For an accepted running generation, cleanupPending is false: once cleanup returns, Stop sets terminal true even if
stopErr is nonnil; the later cohort call returns nil at the terminal check. Thus a nonnil completed running abort
call alone does not reproduce the filed false-red hypothesis. Only the failed-Start retryable branch preserves
nonterminal cleanup authority on cleanup error. Stops that return before cleanup because Start is still in flight
or another Stop is active are separate cases. An assertion exit before any Stop also leaves a first fallback call
after cancellation; that is different from repeating an already terminal running Stop.

Terminal state is not proof of all work joined. The existing readiness regression deliberately permits Stop’s bound
to win while a held worker has not completed, then explicitly releases and joins that exact worker. The command-fence
deadline branch includes receives after ctx has ended and relies on owned commands obeying their runtime authority.
These facts forbid claiming generic hang containment from a finite Stop context or terminal flag.

- `processor/rule/lifecycle_runtime_test.go:28` — `// owner. Its expired bound permits a delayed readiness worker to remain unjoined,`
- `processor/rule/lifecycle_runtime_test.go:63` — `stopErr := processor.Stop(stopCtx)`
- `processor/rule/lifecycle_runtime_test.go:72` — `t.Error("readiness completion announced before delayed worker entry")`
- `processor/rule/lifecycle_runtime_test.go:75` — `close(releaseWorker)`
- `processor/rule/lifecycle_runtime_test.go:76` — `if panicValue := <-workerReturned; panicValue != nil {`
- `processor/rule/owner_lane.go:75` — `l.end(done, ctx.Err())`
- `processor/rule/owner_lane.go:101` — `close(done)`
- `processor/rule/owner_lane.go:104` — `command.result <- err`
- `processor/rule/processor.go:107` — `cancel             context.CancelFunc`
- `processor/rule/processor.go:141` — `statusLoopDone chan struct{}`

The directly inspected Processor ownership fields are private cancel/join state, not a retained Start context.
This is not a repository-wide context-retention certification; no production change is selected. PR #1404 actively
owns rule/config/boot surfaces and must remain untouched. Its latest read-only checkpoint was open head
`54a58a4fb856229bcad108f5f531684dcf17f091`, updated 2026-09-29T11:41:57Z; its body records a reproduced adjacent
hot-reload cleanup-order failure and a separate repeated-Stop correction in progress. That branch is not this base.
No #1416 reproduction or closure verdict was produced in this inventory.

## 4. Existing same-shape owners and collision inventory

Semantic shape: test resource acquisition with exact terminal ownership, fresh cleanup authority, error preservation,
and dependent-before-substrate teardown. The following existing owners cover parts of the shape; none is a newly
proposed framework.

| Dimension | Existing measured owners / limits |
|---|---|
| Owners / lifecycle | Shared suite performs operations; rule fixture holds cohort fallback; TestClient owns client/container teardown |
| Catalog / declaration | LifecycleFactory and exported suite functions declare support; exact cleanup_baseline.json declares reviewed debt and resolution evidence |
| Status / readers | require/assert, worker result channel, cohort Errorf, and aggregate goroutine/memory observations report different facts |
| Writers / ownership | Factory may run in ten workers; mutex owns cohort list; Start cancellation belongs to individual suite call paths; TestClient owns its own cleanupOnce |
| Recovery | Failed-Start rule cleanup may retain retry authority; running terminal Stop does not offer a later rejoin; TestClient repeats its first cleanup result without rerunning |
| Durable communication / distributed claims | No new durable or messaging surface in the active proposal; existing rule readiness writes are production state, not a test-cleanup coordination mechanism |

- `natsclient/test_client.go:854` — `func NewTestClient(t testing.TB, opts ...TestOption) *TestClient {`
- `natsclient/test_client.go:856` — `testClient, err := newTestClient(t.Context(), productionTestClientFactoryDeps, opts...)`
- `natsclient/test_client.go:863` — `t.Cleanup(func() {`
- `natsclient/test_client.go:865` — `t.Errorf("clean up NATS test infrastructure: %v", err)`
- `natsclient/test_client.go:303` — `func cleanupTestInfrastructureWithin(`
- `natsclient/test_client.go:310` — `closeCtx, closeCancel := context.WithTimeout(context.Background(), timeout)`
- `natsclient/test_client.go:317` — `terminateCtx, terminateCancel := context.WithTimeout(context.Background(), timeout)`
- `natsclient/test_client.go:323` — `return cleanupErr`
- `natsclient/test_client.go:932` — `func (tc *TestClient) Terminate() error {`
- `natsclient/test_client.go:933` — `tc.cleanupOnce.Do(func() {`
- `natsclient/test_client_readiness_test.go:424` — `func TestCleanupTestInfrastructure_PreservesCloseAndTerminateErrors(t *testing.T) {`
- `natsclient/test_client_readiness_test.go:451` — `func TestCleanupTestInfrastructure_GivesEachOperationItsOwnBudget(t *testing.T) {`
- `processor/graph-embedding/lifecycle_owner_test.go:118` — `t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })`
- `processor/graph-embedding/lifecycle_owner_test.go:151` — `go func() { stopResult <- c.Stop(stopCtx) }()`
- `processor/graph-embedding/lifecycle_owner_test.go:170` — `releaseOnce.Do(func() { close(release) })`
- `processor/graph-embedding/lifecycle_owner_test.go:171` — `require.NoError(t, c.Stop(t.Context()))`
- `test/testinfra/cleanup_process_unix_test.go:48` — `func runOwnedCleanupCommand(ctx context.Context, root string, args, env []string, probes ...cleanupProcessProbe) cleanupProcessOutcome {`
- `test/testinfra/cleanup_process_unix_test.go:90` — `command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}`
- `test/testinfra/cleanup_process_unix_test.go:101` — `waiter := newCommandWaiter(command)`

TestClient already separates close/container budgets and joins error causes, with causal fake-dependency tests.
Owner-local graph rollback tests release exact gates and observe specific terminal state; their context use is not
a blanket safe-cleanup precedent. test/testinfra has a test-only owned subprocess supervisor with process-group
ownership and one Cmd.Wait owner; it proves process fixture cleanup, not stoppability of arbitrary in-process Go
methods. These are measured problem-shape precedents, not a selected adoption or a new generic watchdog proposal.

`internal/lifecyclecleanup.RollbackFailedStart` already owns a related production shape: failed-Start rollback
runs synchronously under a fixed five-second context that preserves parent values while removing cancellation and
deadline, rejects a nil parent, and joins the rollback error with the cleanup context error. Rule's failed-Start
path calls it and preserves the original Start error alongside rollback failure. This is a stateless production
failed-Start policy, not a shared test finalizer or evidence that arbitrary Stop methods return within their bound.

- `internal/lifecyclecleanup/lifecyclecleanup.go:12` — `const failedStartRollbackTimeout = 5 * time.Second`
- `internal/lifecyclecleanup/lifecyclecleanup.go:17` — `func RollbackFailedStart(parent context.Context, rollback func(context.Context) error) error {`
- `internal/lifecyclecleanup/lifecyclecleanup.go:26` — `if parent == nil {`
- `internal/lifecyclecleanup/lifecyclecleanup.go:33` — `ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), budget)`
- `internal/lifecyclecleanup/lifecyclecleanup.go:36` — `rollbackErr := rollback(ctx)`
- `internal/lifecyclecleanup/lifecyclecleanup.go:37` — `return errors.Join(rollbackErr, ctx.Err())`
- `processor/rule/processor.go:1013` — `rollbackErr := lifecyclecleanup.RollbackFailedStart(ctx, rp.cleanup)`
- `processor/rule/processor.go:1014` — `startErr = errors.Join(startErr, rollbackErr)`

The service test fixture `liveAuthorityStopComponent` observes whether the exact accepted Start authority is still
live inside Stop and closes its own completion signal only on that controlled path. Its retained context is in a
test fake; it is not a production context-retention precedent or a shared cleanup API recommendation.

- `service/lifecycle_context_contract_test.go:169` — `type liveAuthorityStopComponent struct {`
- `service/lifecycle_context_contract_test.go:189` — `c.startCtx = ctx`
- `service/lifecycle_context_contract_test.go:193` — `func (c *liveAuthorityStopComponent) Stop(context.Context) error {`
- `service/lifecycle_context_contract_test.go:196` — `case <-c.startCtx.Done():`
- `service/lifecycle_context_contract_test.go:197` — `return errors.New("Start authority canceled before component Stop")`
- `service/lifecycle_context_contract_test.go:200` — `c.stopOnce.Do(func() { close(c.stopped) })`

No shared component cleanup helper was found by the focused gopls CleanupComponent query. Broad Cleanup/StopTest
queries returned unrelated production/cache and fixture symbols; they do not prove repository-wide absence.
component/test_helpers.go provides a discoverable SimpleMockComponent factory, not lifecycle ownership.

## 5. Exact 334-entry / 86-resolution exposure

Current manifest SHA256: `a5b7ea6f86d2b8a35b104063c1fdffba440b511cdb81f2667e591579eabcefe1`. JSON decoding measures exactly 334 entries and 86 resolutions.
The selected shared/support and cohort paths have zero direct guarded debt entries. WebSocket’s broad adopter file
contains eight existing entries in other tests; editing that file is not automatically a repair of those entries.
Four unique resolutions have source dependencies on the shared suite. No baseline change or approval is inferred.

| Path | Direct debt | Resolutions touching site or dependency |
|---|---:|---:|
| `component/lifecycle.go` | 0 | 4 |
| `component/lifecycle_test_suite.go` | 0 | 4 |
| `component/test_helpers.go` | 0 | 0 |
| `gateway/http/http_lifecycle_test.go` | 0 | 0 |
| `input/udp/udp_lifecycle_test.go` | 0 | 0 |
| `output/websocket/websocket_test.go` | 8 | 1 |
| `processor/graph-index/lifecycle_integration_test.go` | 0 | 0 |
| `processor/rule/lifecycle_integration_test.go` | 0 | 0 |

Exact exposed resolution identities and dependencies:

Identity: `component/lifecycle_test_suite.go|(*ErrorInjectingComponent).Stop|ordinary|github.com/c360studio/semstreams/component.Stop|github.com/c360studio/semstreams/component.LifecycleComponent|unknown|1`
  Result: `uncertain-owner-provenance`; applicability `ordinary-only`
  Site fingerprint: `7741631856b0a54e39240b0ce5cd88c36e2cdf61f337e671c4811a46119ebf26`
  Dependency: `component/lifecycle_test_suite.go` / `ErrorInjectingComponent.Stop` / `5ceac2af818289c586b399daf8cf871c26ab8ae1216fbbda3989e87328c23c0f`
  Dependency: `component/lifecycle.go` / `LifecycleComponent` / `c774a7da33b173323d61833523a6adc4409875223be8c5ba9aa6bd18e6b31779`
  Dependency: `component/lifecycle_test_suite.go` / `ErrorInjectingComponent` / `d13c3abdf66a15ca9b144cd1c77ccbf8c1f00737979ea3511fd349be19bd5a40`
  Dependency: `component/lifecycle_test_suite.go` / `NewErrorInjectingComponent` / `4cbdc6656728cf3e76f3d6172f80ac3f689c4e9afd46eaeed89d261766836d3d`
  Dependency: `component/lifecycle_test_suite.go` / `TestErrorInjection` / `2ec8a52d9f696c6b05565e5ede98edabf9f27a4ff54903b72ce89798b3f843af`
  Dependency: `output/websocket/websocket_test.go` / `TestWebSocketOutput_ErrorInjection` / `11d34863e638a2addba45328a0fe44ec0c4c1b0f0d0d594eeac4730d9d114173`
Identity: `component/lifecycle_test_suite.go|testNilStopContext|ordinary|github.com/c360studio/semstreams/component.Stop|github.com/c360studio/semstreams/component.LifecycleComponent|unknown|1`
  Result: `deliberate-contract-call`
  Site fingerprint: `f097d914839f2e69a0b8031eb5fea90d9d52b2d025b0c7b9306279b102383063`
  Dependency: `component/lifecycle_test_suite.go` / `testNilStopContext` / `80148799a601fa108a10e835833665cef9df784bd78e702934539a71c1a900df`
  Dependency: `component/lifecycle.go` / `LifecycleComponent` / `c774a7da33b173323d61833523a6adc4409875223be8c5ba9aa6bd18e6b31779`
  Dependency: `component/lifecycle_test_suite.go` / `testPortableLifecycleFloor` / `f45f1e206194510b9decf430b7e0d241ad394b03942f1539f3ad35bc2955e0a0`
  Dependency: `component/lifecycle_test_suite.go` / `StandardLifecycleTests` / `7cd6bade0713b19971ccdeecbee148ac63d4447ece177b23bebabb8fa3a12923`
  Dependency: `component/lifecycle_test_suite.go` / `LifecycleFactory` / `d4b3609ad39f0bfa14a407e633a64bcb7cd08669e3e587634ec501c1ab7d69eb`
Identity: `component/lifecycle_test_suite.go|testNoResourceLeaks|ordinary|github.com/c360studio/semstreams/component.Stop|github.com/c360studio/semstreams/component.LifecycleComponent|unknown|1`
  Result: `bounded-cleanup`
  Site fingerprint: `0744290e233da31c1400bfae211f5fa1bd44ad4e73c093e33a26847be1c0e2c0`
  Dependency: `component/lifecycle_test_suite.go` / `testNoResourceLeaks` / `153228727bc3f0fea732944dd319ab3c26eff75e24212dbe8561ffd069ba15d8`
  Dependency: `component/lifecycle.go` / `LifecycleComponent` / `c774a7da33b173323d61833523a6adc4409875223be8c5ba9aa6bd18e6b31779`
Identity: `component/lifecycle_test_suite.go|testParallelFreshInstances|ordinary|github.com/c360studio/semstreams/component.Stop|github.com/c360studio/semstreams/component.LifecycleComponent|unknown|1`
  Result: `bounded-cleanup`
  Site fingerprint: `0744290e233da31c1400bfae211f5fa1bd44ad4e73c093e33a26847be1c0e2c0`
  Dependency: `component/lifecycle_test_suite.go` / `testParallelFreshInstances` / `905b912c332aa7fbb712f905a16d38a4935ca7fc658a744f5b9353877f2e8cf6`
  Dependency: `component/lifecycle.go` / `LifecycleComponent` / `c774a7da33b173323d61833523a6adc4409875223be8c5ba9aa6bd18e6b31779`

Exact eight WebSocket-file debt identities (not selected for repair merely by file proximity):

1. `output/websocket/websocket_test.go|TestWebSocketOutput_AtomicCleanup|defer|github.com/c360studio/semstreams/output/websocket.Stop|*github.com/c360studio/semstreams/output/websocket.Output|unbounded|1`
2. `output/websocket/websocket_test.go|TestWebSocketOutput_BroadcastStress|defer|github.com/c360studio/semstreams/output/websocket.Stop|*github.com/c360studio/semstreams/output/websocket.Output|unbounded|1`
3. `output/websocket/websocket_test.go|TestWebSocketOutput_ConcurrentClientHandling|defer|github.com/c360studio/semstreams/output/websocket.Stop|*github.com/c360studio/semstreams/output/websocket.Output|unbounded|1`
4. `output/websocket/websocket_test.go|TestWebSocketOutput_ConcurrentClients|defer|github.com/c360studio/semstreams/output/websocket.Stop|*github.com/c360studio/semstreams/output/websocket.Output|unbounded|1`
5. `output/websocket/websocket_test.go|TestWebSocketOutput_DoubleClose|defer|github.com/c360studio/semstreams/output/websocket.Stop|*github.com/c360studio/semstreams/output/websocket.Output|unbounded|1`
6. `output/websocket/websocket_test.go|TestWebSocketOutput_MessageEnvelope|defer|github.com/c360studio/semstreams/output/websocket.Stop|*github.com/c360studio/semstreams/output/websocket.Output|unbounded|1`
7. `output/websocket/websocket_test.go|TestWebSocketOutput_PendingBufferCreation|defer|github.com/c360studio/semstreams/output/websocket.Stop|*github.com/c360studio/semstreams/output/websocket.Output|unbounded|1`
8. `output/websocket/websocket_test.go|TestWebSocketOutput_RaceConditions|defer|github.com/c360studio/semstreams/output/websocket.Stop|*github.com/c360studio/semstreams/output/websocket.Output|unbounded|1`


Full matching manifest records retained at `review/baseline-exposure.json`, SHA256
`555716939a4322dc8b4979f8e77a3cc6235e852bef58f96509a5816f42467a4d`. The four-resolution count is deduplicated; it is not four plus
the same wrapper resolution again for its WebSocket caller. Normalized declaration changes invalidate exact evidence
even when a direct call is unchanged. New cleanup ownership variants must be independently classified. No guard run
or stale-record reconciliation was performed in this inventory.

- `test/testinfra/cleanup_baseline.json:2550` — `"identity": "component/lifecycle_test_suite.go|(*ErrorInjectingComponent).Stop|ordinary|github.com/c360studio/semstreams/component.Stop|github.com/c360studio/semstreams/component.LifecycleComponent|unknown|1",`
- `test/testinfra/cleanup_baseline.json:2592` — `"identity": "component/lifecycle_test_suite.go|testNilStopContext|ordinary|github.com/c360studio/semstreams/component.Stop|github.com/c360studio/semstreams/component.LifecycleComponent|unknown|1",`
- `test/testinfra/cleanup_baseline.json:2628` — `"identity": "component/lifecycle_test_suite.go|testNoResourceLeaks|ordinary|github.com/c360studio/semstreams/component.Stop|github.com/c360studio/semstreams/component.LifecycleComponent|unknown|1",`
- `test/testinfra/cleanup_baseline.json:2649` — `"identity": "component/lifecycle_test_suite.go|testParallelFreshInstances|ordinary|github.com/c360studio/semstreams/component.Stop|github.com/c360studio/semstreams/component.LifecycleComponent|unknown|1",`

## 6. Adopter seam inventory

Person: a component author calling the exported suite with a factory, including a sister-repository author.

1. What must they know? Factories must produce fresh independent instances, tolerate concurrent calls, avoid Fatal/
   FailNow in worker goroutines, preserve substrate until all component cleanup returns, distinguish controlled Stop
   from abort results, and account for partial acquisition and early assertions. More than two facts are required.
   These obligations are spread between the lifecycle contract, testing policy, suite implementation, and real adopter.
2. If they do nothing beyond returning a component: the suite compiles and runs, but a failed Initialize/Start can skip
   terminal cleanup and a Fatal assertion can skip later Stop. ErrorInjection’s finalizer can hit the injected wrapper
   again. The generic factory signature supplies no test owner or structured acquisition error.
3. Where they find out: interface/signature mistakes fail compilation; operation failures appear as assertions or worker
   results; some cleanup errors only log; fallback Errorf is adopter-specific; missing exact joins are often nowhere
   until aggregate thresholds or an outer timeout. The exported factory’s comment only promises a new instance.
4. Knowledge gap: lifecycle contract selection remains necessary, but cleanup owner selection, ownership transfer after
   construction, and failure-safe component-before-substrate ordering are currently borne by the adopter. This states
   the gap; it does not choose an API or delegate new work to sister owners.

External read-only evidence: thirteen local Go sister/worktree roots declare a SemStreams dependency. A gopls
reference query against semconnect’s installed beta.160 StandardLifecycleTests returned no references (successful
exit). Supplemental tracked-source API-spelling searches across all thirteen roots returned zero hits for the five
exported suite/factory names. This is a lexical adoption inventory, not a claim that all remote repositories/build
selections were type-checked. Untracked sources, repositories absent from this machine, generated code, and future
adopters are outside that measurement. No sister source or Git state was mutated.

| Local repository | Source HEAD | SemStreams version |
|---|---|---|
| `semboids` | `8c03cc53836ced93a5df7064473c63ff144e64f1` | `v1.0.0-beta.160` |
| `semconnect` | `d0d06e00bf05a545f30ceea798db1c2b1ee47d4f` | `v1.0.0-beta.160` |
| `semdev` | `ca3956af2ed87d5fa5bdb8183cdb506f7beb7240` | `v1.0.0-beta.160` |
| `semdragon` | `07f4de9b65887801ff18a7273d14233023049321` | `v1.0.0-beta.135` |
| `semlink` | `985e97d8d2181a2eef6caae7ba640195e96ecd58` | `v1.0.0-beta.160` |
| `semmachina` | `841c45e8bb01af19495d4294a7f510a8a0c2e8c2` | `v1.0.0-beta.160` |
| `semops` | `602c619a9f1caa8adac624cffda9d1afa9ad80f3` | `v1.0.0-beta.145` |
| `semsage` | `4d28b4dc1210f47da84a3031125167d164de9290` | `v1.0.0-alpha.3` |
| `semsource` | `4093d3ce421371f4a99d7168e372552899bf6795` | `v1.0.0-beta.160` |
| `semspec` | `5a9496eecc453747f4bc557b95444db6304c1420` | `v1.0.0-beta.134` |
| `semspec-ui-bmad` | `c8308d7e258587f3d96e59b8d2a8b3f8acc922b7` | `v1.0.0-beta.107` |
| `semspec-ui-run-visibility` | `e30cbf78691d1a185033903869d9e6fd92ac3356` | `v1.0.0-beta.107` |
| `semteams` | `ce22c961d30014c463a09f8f8a2a90044ee1a1cf` | `v1.0.0-beta.160` |

The exact paths, dependency lines, zero-hit exits, and HEADs are retained in
`review/sister-spellings.json`. External availability still follows from the ordinary exported Go source;
zero observed consumers is not authorization to delete it or assume a breaking change harmless.

## 7. Adjacent contracts and unresolved evidence

Current component-lifecycle requires exact caller-owned Start/Stop authority, controlled Stop while Start remains
live, honest abort errors, no generic later rejoin, and owner-specific failed-Start retry. Runtime-context-ownership
states the composition order. test-cleanup-policy explicitly limits finite classification and rejects stale evidence.
The testing policy requires independent cleanup contexts, checked failure evidence, explicit synchronization, owned
work completion, one justified NATS dependency, and measured failure/cleanup budgets. The ten-second cohort budget
is existing cooperative supply, not an approved new runtime ceiling.

- `openspec/specs/component-lifecycle/spec.md:8` — ``LifecycleComponent.Start(ctx)` MUST reject nil or already-ended context before action. The accepted Start context`
- `openspec/specs/component-lifecycle/spec.md:35` — `return nil without repeating teardown. The portable contract MUST NOT promise concurrent Stop executor election,`
- `openspec/specs/component-lifecycle/spec.md:57` — `exact cleanup authority only when rollback does not complete. A later caller Stop MAY retry that retained failed-Start`
- `openspec/specs/runtime-context-ownership/spec.md:61` — `### Requirement: Lifecycle composition distinguishes controlled shutdown from abort cancellation`
- `openspec/specs/test-cleanup-policy/spec.md:136` — `#### Scenario: Attempted blanket resolution`
- `openspec/specs/test-cleanup-policy/spec.md:199` — `### Requirement: Evidence limits`
- `docs/contributing/01-testing.md:439` — `### Cleanup and Reaper`
- `docs/contributing/01-testing.md:489` — `return ctx`
- `openspec/changes/shared-lifecycle-test-cleanup/proposal.md:14` — `assertions and worker-created instances, and re-derive the #1416 condition before recommending a correction.`
- `openspec/changes/shared-lifecycle-test-cleanup/tasks.md:8` — `- [ ] 1.3 Review a bounded design, failure proof, PBT decision, and spec delta; accept the design before implementation.`

Open evidence questions for the later design/proof phase:

1. Which existing support paths are selected for repair, including benchmark result handling versus only inventory?
2. What exact ownership observation can demonstrate cleanup completion for the real rule cohort without inventing a
  portable later-rejoin or runtime-state API? Existing public Stop results and owner-local white-box evidence differ.
3. Which early-assertion, failed acquisition, worker-failure, and injected-error paths demonstrate the concrete defect?
4. What bounded failure witness gives a useful diagnostic while separately accounting for every owned goroutine/process?
  No context alone stops an implementation that ignores cancellation; an outer timeout is not a causal assertion.
5. Does a controlled/abort correction affect any exact #1416 path beyond the now-refuted running-error-repeat premise?
6. Which four manifest resolution records become stale after the selected declaration edits? Eight unrelated WebSocket
  debt entries and all other package batches remain with #1417 unless separately selected and proven repaired.


These questions identify measured seams and proof limits, not options or implementation tasks. #1417 remains open.
No #1416 issue state/milestone, PR #1404 source, production lifecycle behavior, or guard admission rule is changed.

## Search and read log

All commands ran read-only unless writing the explicitly authorized scratch artifacts. Working directory was the
claimed worktree except the named sister query. Structural queries used gopls first; source-range reads then inspected
the returned declarations. Broad query output that truncated is not used as an absence/completeness proof.

1. `cat .agents/contracts/semstreams-architect.md`; `cat openspec/project.md`; full active proposal/tasks, current
   component-lifecycle/runtime-context-ownership/test-cleanup-policy and full docs/contributing/01-testing.md read.
2. `rg --files openspec/changes/shared-lifecycle-test-cleanup`; `rg --files openspec/specs | rg
   "(lifecycle|context|test-cleanup|test-policy|runtime)"` — two active artifacts and related capability paths.
3. `git rev-parse HEAD`; `git status --short` — recorded base; clean checkout at inventory start.
4. `gh issue view {1418,1416,1417} --json title,body,comments,url` and targeted rereads — initial sandbox network
   calls failed; authorized escalated read calls succeeded. No absence conclusion from network failure.
5. `gh pr view 1404 --json title,body,files,url,headRefName`; later `--json headRefOid,url,state,updatedAt` — adjacent
   claim and current head recorded; no checkout/fetch/PR mutation.
6. `gopls references component/lifecycle_test_suite.go:21:6` — two default StandardLifecycleTests adopters.
7. `GOFLAGS=-tags=integration gopls references component/lifecycle_test_suite.go:21:6` — four adopters listed above.
8. `gopls references component/lifecycle_test_suite.go:16:6` — seven in-file factory type uses.
9. `gopls references component/lifecycle_test_suite.go:276:6` and `:392:6` — WebSocket benchmark and error-injection
   references. `gopls workspace_symbol -matcher=fuzzy Lifecycle` — symbol discovery, not absence proof.
10. `gopls workspace_symbol -matcher=fuzzy {Cleanup,TestClient,StopTest,newTestComponent,CleanupComponent,
    createTestWebSocketOutput}` — support/fixture candidates; CleanupComponent empty; broad results not exhaustive proof.
11. `gopls workspace_symbol -matcher=caseInsensitive {NewTestClient,Terminate,runLifecycleStartRollback}` — exact
    substrate and rollback declaration locations; four graph-owner rollback helper declarations returned.
12. Source reads: component/lifecycle_test_suite.go:1–458; component/test_helpers.go; all four StandardLifecycleTests
    adopter declarations; output/websocket/websocket_test.go:802–844,1064–1145; natsclient/test_client.go:288–326,840–898,
    924–952 and test_client_readiness_test.go:422–491; graph-embedding/lifecycle_owner_test.go:104–184.
13. Rule proof-boundary reads: processor.go:41–162,1120–1445; owner_lane.go:45–125; lifecycle_runtime_test.go:22–87.
    `git grep -n "abort\|returned\|terminal\|cleanupPending" -- processor/rule/lifecycle_owner_test.go
    processor/rule/lifecycle_runtime_test.go processor/rule/lifecycle_entity_coordinator_test.go` located owner evidence.
14. `rg --files component | rg "(lifecycle.*test|test.*helper)"`; corresponding natsclient/internal-boot/testinfra
    filename discovery; `rg --files test/contract | rg lifecycle` — no lifecycle-named contract test file.
15. `git grep -n` for exported suite names in docs/openspec/test/contract; focused docs/contributing/docs/development/
    docs/operations/.agents query returned zero. Historical proposals were found and treated as history only.
16. `git grep -n` for shared support/suite path in archived #1064 artifacts and historical blockers design — exact
    prior evidence homes found; current counts came from current JSON, not those histories.
17. `git grep -n` for cleanup/cancelStart/join-budget prose in named owner/substrate/runner fixtures; broad historical
    outputs truncated, used only for locating named sources then read above.
18. Python JSON decoding of cleanup_baseline.json: counts, exact identity path membership, and all explicit source
    dependency paths; unique matching resolution set retained. No analyzer execution or baseline refresh.
19. Python listed local c360 directories and direct go.mod roots; `git -C <root> grep -n
    github.com/c360studio/semstreams -- go.mod` measured thirteen dependency roots and versions.
20. In semconnect: `GOPROXY=off GOTOOLCHAIN=local go list -m -json github.com/c360studio/semstreams` resolved existing
    beta.160 cache; `GOPROXY=off GOTOOLCHAIN=local gopls references <cache>/component/lifecycle_test_suite.go:21:6`
    exited successfully with no references. No module download.
21. Supplemental API-spelling search in each of the thirteen roots: `git -C <root> grep -n -E
    "StandardLifecycleTests|LifecycleFactory|TestErrorInjection|BenchmarkLifecycleMethods|NewErrorInjectingComponent"
    -- "*.go"`; all exited 1, empty output/error. `git -C <root> rev-parse HEAD` recorded each source checkpoint.
22. `rg --files docs/adr | rg "(049|048)"` located domain lifecycle/dispatcher ADR names; no new communication or
    domain orchestration design is in this inventory, so decision skills are not triggered.
23. Current manifest and source bytes hashed for scratch exposure artifacts; no Git mutation, test, or source edit.

Coordinator review correction: independently read internal/lifecyclecleanup/lifecyclecleanup.go:1–38,
processor/rule/processor.go:1011–1032, and service/lifecycle_context_contract_test.go:167–210 for the omitted existing
owner and neighboring observation fixture. Added their measured scope above without an adoption recommendation.
Changed non-pin identity/question bullets to paragraphs or numbered lists for the canonical inventory parser;
source-pin claims remain unchanged. Relative companion paths retain the original companion content hashes.
