# Owner load reliability inventory

base: e811399e950d4eda4bed9f140a0ff73fa6882001

## Scope and evidence identity

This inventory covers #1421's owner-filter load failure through the real KV listing, SDK producer,
client drain, test substrate, and canonical runner. It separately records nearby harness failure
paths that can delay or bypass cleanup. Those paths are not automatically the cause of this incident.

The claimed gap is causal attribution and reliable failure/cleanup behavior, not an absent timeout:
a framework listing deadline, client-drain bound, container-cleanup bound, package timeout, and
outer CI timeout already exist.

Incident: CI run 36567709902, job 109403662339, PR #1404 head
49a540d628594345b7c743768cc6b13561e26768, 2026-09-29.

Raw log: 644,363 bytes, SHA256
8ac79b8a8359e1d441248dd87f3c12a1cd19aa7bf5728b81ba4b6d33dd553daa.

The coordinator retained the log and six complete issue snapshots in
evidence/incident-and-issue-snapshots.zip; evidence/provenance.json records the individual identities.
The original log is also available at /private/tmp/gh1421-incident-job.log.

`git diff --stat 49a540d6 e811399e -- natsclient/kv.go natsclient/client.go
natsclient/test_client.go processor/graph-index/owner_filter_load_integration_test.go
processor/graph-index/keyed_dispatcher.go scripts/run-integration-tests.sh
.github/workflows/ci.yml` returned no changes. These critical paths are unchanged from the incident.

No fresh execution evidence is claimed here.

## 1. What the incident establishes

The failed operation was NAME-forward convergence, not predicate-forward.

- `processor/graph-index/owner_filter_load_integration_test.go:204` — `name := "Owner Hotspot"`
- `processor/graph-index/name_index.go:40` — `sum := sha256.Sum256([]byte(normalizeName(name)))`
- `processor/graph-index/name_index.go:46` — `return strings.ToLower(strings.TrimSpace(name))`
- `processor/graph-index/kv_contract_benchmark.go:14` — `return nameIndexKey(name) + "." + wildcardPositions(7)`
- `processor/graph-index/owner_filter_load_integration_test.go:483` — `keys, err := fixture.store.KeysByFilter(ctx, fixture.forwardFilter)`
- `processor/graph-index/owner_filter_load_integration_test.go:484` — `require.NoError(t, err)`

SHA256("owner hotspot") is
b49342a7640db5849208cb35a8d254e95f5b05bd100d223279a3884cf3898742.
With seven `*` tokens, it exactly matches the error's filter.

The raw log records:

| Original log lines | Observed fact |
|---|---|
| 351 | Docker info latency 275 ms, observed before the suite, not at the failure |
| 3761–3768 | Top-level test 28.55 s; workers-4 subtest 12.22 s; 5k CI profile, pinned NATS and SDK |
| 3763 | Seed completed: 15,020 rows in 815.431175 ms |
| 3769–3774 | All six initial owner/forward distributions completed |
| 3772 | NAME-forward p50 118.765947 ms, p95 120.238888 ms, max 120.596977 ms |
| 3775–3778 | Fifteen concurrent owner-list results recorded; catch-up 31.392963 ms |
| 3779 | Per-store and aggregate temporary-consumer counts returned to baseline zero |
| 3781–3785 | Convergence KeysByFilter returned context deadline exceeded |
| 3760, 3786 | Client cleanup then reported a 14.999987802 s drain timeout |
| 3788 | graph-index package failed after 49.416 s |
| 3791–3794 | Other package work continued; suite failure and distributions were emitted later |

The consumer-baseline log is reached only after the churn workers, sampler, and dispatcher join.
It rules out a skipped join in those earlier harness phases on this particular execution.

The log does not record convergence-list duration, partial key count, native subscription state,
the failed consumer identity, a failure-time server snapshot, goroutine stacks, or a transport trace.
The final resource/slow-consumer observation was after the failed assertion and was never reached.

Consequently:

1. Deadline expiry and subsequent drain failure are observed.
1. Shared-runner contention is an issue hypothesis, not a measured cause.
1. A blocked SDK producer is a source-supported possibility, not an established incident cause.
1. The elapsed duration of the failed listing itself cannot be reconstructed precisely.
1. A consumer count observed before the failed listing is not proof that the failed listing left no work.
1. The original log contains no panic, DATA RACE, or goroutine dump.

## 2. Harness acquisition, workload, and execution authority

- `processor/graph-index/owner_filter_load_integration_test.go:57` — `name: "ci", entities: 5_000, nameContext: 5_000, spread: 20,`
- `processor/graph-index/owner_filter_load_integration_test.go:58` — `repetitions: 5, churnPerWriter: 50, workerShapes: []int{4},`
- `processor/graph-index/owner_filter_load_integration_test.go:98` — `p95Budget: 3 * time.Second, p99Budget: 3 * time.Second,`
- `processor/graph-index/owner_filter_load_integration_test.go:105` — `name: "full", entities: 21_000, nameContext: 5_000, spread: 20,`
- `processor/graph-index/owner_filter_load_integration_test.go:106` — `repetitions: 30, churnPerWriter: 200, workerShapes: []int{4, maxGraphIndexWorkers},`
- `processor/graph-index/owner_filter_load_integration_test.go:146` — `testClient := natsclient.NewTestClient(t,`
- `processor/graph-index/owner_filter_load_integration_test.go:151` — `natsclient.WithTestTimeout(15*time.Second),`
- `processor/graph-index/owner_filter_load_integration_test.go:153` — `ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)`
- `processor/graph-index/owner_filter_load_integration_test.go:154` — `defer cancel()`
- `processor/graph-index/owner_filter_load_integration_test.go:194` — `raw, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: bucketName, Storage: jetstream.FileStorage})`
- `processor/graph-index/owner_filter_load_integration_test.go:196` — `stores[bucketName] = nc.NewKVStore(raw)`

This test acquires its own file-backed, monitored, immutable-version NATS container. It directly
creates OWNER_LOAD_PREDICATE, OWNER_LOAD_NAME, OWNER_LOAD_INCOMING, MAXIMA, and LIFECYCLE buckets.
It does not start a graph-index Component or use its production ENTITY_STATES watcher/reconciler.

The load harness uses production key/filter constructors, KVStore methods, and keyedDispatcher.
Its final convergence means restoring directly mutated fixture rows and checking forward counts.
Component reconciliation and watermark correctness have separate tests.

The root I/O context is detached from testing authority by construction and spans both worker shapes
in the full profile. It is canceled when the top-level test function returns. Subtest failure does
not itself cancel this parent before the next full-profile worker shape.

WithTestTimeout is not the listing deadline and does not control terminal cleanup.

### Seed ownership

- `processor/graph-index/owner_filter_load_integration_test.go:259` — `jobs := make(chan seedRow, 256)`
- `processor/graph-index/owner_filter_load_integration_test.go:260` — `errs := make(chan error, 32)`
- `processor/graph-index/owner_filter_load_integration_test.go:262` — `for range 32 {`
- `processor/graph-index/owner_filter_load_integration_test.go:268` — `errs <- putErr`
- `processor/graph-index/owner_filter_load_integration_test.go:274` — `jobs <- row`
- `processor/graph-index/owner_filter_load_integration_test.go:277` — `wg.Wait()`
- `processor/graph-index/owner_filter_load_integration_test.go:279` — `for seedErr := range errs {`

Thirty-two workers continue after Put errors. Errors are drained only after all input has been sent
and every worker has joined. Neither the error send nor job send observes cancellation.

There is a source-demonstrable saturation cycle: enough failed Puts fill errs, workers block sending
further errors, jobs fills, and the producer blocks before close/join/error observation. The parent
deadline does not interrupt these channel operations. This cycle was not exercised by the incident:
its seed completed successfully.

### Worker shape ownership

- `processor/graph-index/owner_filter_load_integration_test.go:321` — `resultCount := profile.repetitions * len(fixtures)`
- `processor/graph-index/owner_filter_load_integration_test.go:322` — `results := make(chan listResult, resultCount)`
- `processor/graph-index/owner_filter_load_integration_test.go:323` — `dispatchCtx, cancelDispatch := context.WithCancel(ctx)`
- `processor/graph-index/owner_filter_load_integration_test.go:328` — `keys, err := job.fixture.store.KeysByFilter(runCtx, job.fixture.ownerFilter)`
- `processor/graph-index/owner_filter_load_integration_test.go:332` — `dispatcher.Start(dispatchCtx)`
- `processor/graph-index/owner_filter_load_integration_test.go:344` — `sampleCtx, stopSampling := context.WithCancel(ctx)`
- `processor/graph-index/owner_filter_load_integration_test.go:353` — `info, infoErr := fixture.stream.Info(sampleCtx)`
- `processor/graph-index/owner_filter_load_integration_test.go:365` — `runtime.Gosched()`
- `processor/graph-index/owner_filter_load_integration_test.go:369` — `churnErrs := make(chan error, workers)`
- `processor/graph-index/owner_filter_load_integration_test.go:380` — `mutationErr = fixture.store.Delete(ctx, key)`
- `processor/graph-index/owner_filter_load_integration_test.go:382` — `_, mutationErr = fixture.store.Put(ctx, key, value)`
- `processor/graph-index/owner_filter_load_integration_test.go:397` — `require.NoError(t, dispatcher.Submit(ctx, listJob{fixture: fixture, serial: repetition}))`
- `processor/graph-index/owner_filter_load_integration_test.go:411` — `result := <-results`
- `processor/graph-index/owner_filter_load_integration_test.go:412` — `require.NoError(t, result.err, result.label)`
- `processor/graph-index/owner_filter_load_integration_test.go:423` — `churnWG.Wait()`
- `processor/graph-index/owner_filter_load_integration_test.go:428` — `stopSampling()`
- `processor/graph-index/owner_filter_load_integration_test.go:429` — `samplerWG.Wait()`
- `processor/graph-index/owner_filter_load_integration_test.go:434` — `cancelDispatch()`
- `processor/graph-index/owner_filter_load_integration_test.go:436` — `case <-dispatcher.done:`
- `processor/graph-index/owner_filter_load_integration_test.go:438` — `t.Fatal("owner-filter dispatcher did not join after parent cancellation")`

The sampler repeatedly calls stream.Info for all three stores without a polling interval; Gosched is
a scheduler yield, not an I/O-rate bound. It owns a one-error channel and returns after the error.
Each churn worker owns at most one error; the churn channel has one slot per worker. The result
channel can hold every normally admitted result.

Cleanup is success-path code, not lexical finalization. Fatal Submit/result/count/missing-sample/churn
assertions can skip sampler cancellation and all later joins. Parent cancellation eventually reaches
the goroutines when the top-level function returns, but there is no failure-path join before NATS
cleanup. A missing result blocks the bare receive even if the parent expires. The existing source
comment explicitly acknowledges waiting for the package timeout in that case.

The normal path joins churn, cancels/joins sampling, cancels/joins dispatch, checks consumer baselines,
restores rows, and only then executes the failed convergence listing.

### Production keyed dispatcher

- `processor/graph-index/keyed_dispatcher.go:14` — `process func(context.Context, T)`
- `processor/graph-index/keyed_dispatcher.go:38` — `func (d *keyedDispatcher[T]) Start(ctx context.Context) {`
- `processor/graph-index/keyed_dispatcher.go:47` — `case <-ctx.Done():`
- `processor/graph-index/keyed_dispatcher.go:53` — `d.process(ctx, item)`
- `processor/graph-index/keyed_dispatcher.go:59` — `d.wg.Wait()`
- `processor/graph-index/keyed_dispatcher.go:60` — `close(done)`
- `processor/graph-index/keyed_dispatcher.go:69` — `case <-ctx.Done():`

Start creates lane workers plus one join goroutine. Cancellation is checked between work items.
An admitted process callback receives the exact Start context; the dispatcher cannot interrupt a
callback that does not return. The harness observes dispatcher.done on its normal path.

gopls found construction in component.go:1118, lifecycle_order_test.go:138/189,
ordered_dispatch_test.go:382, and this harness:324. The production dispatcher itself is not
a newly missing primitive.

## 3. Framework listing and SDK completion

- `natsclient/kv.go:26` — `MaxRetries            int           // Maximum CAS retry attempts`
- `natsclient/kv.go:39` — `Timeout:               5 * time.Second,`
- `natsclient/kv.go:55` — `options := DefaultKVOptions()`
- `natsclient/kv.go:70` — `return context.WithTimeout(ctx, kv.options.Timeout)`
- `natsclient/kv.go:525` — `return kv.KeysByFilter(ctx, prefix+">")`
- `natsclient/kv.go:538` — `ctx, cancel := kv.applyTimeout(ctx)`
- `natsclient/kv.go:541` — `lister, err := kv.bucket.ListKeysFiltered(ctx, pattern)`
- `natsclient/kv.go:550` — `keys, err := collectFilteredKeys(ctx, lister)`
- `natsclient/kv.go:563` — `lister, err := kv.ListKeysFiltered(ctx, pattern)`
- `natsclient/kv.go:571` — `keys, err := collectFilteredKeys(ctx, lister)`
- `natsclient/kv.go:583` — `defer func() { _ = lister.Stop() }()`
- `natsclient/kv.go:588` — `case <-ctx.Done():`
- `natsclient/kv.go:589` — `return nil, ctx.Err()`
- `natsclient/kv.go:592` — `if err := ctx.Err(); err != nil {`
- `natsclient/kv.go:595` — `return keys, nil`

There are three entry spellings sharing the filtered collection boundary:

1. KVStore.KeysByFilter applies the store timeout.
2. KVStore.KeysByPrefix delegates to it after appending `>`.
3. package FilteredKeys takes a minimal raw reader and applies no additional timeout.

Both exported filtered helpers discard accumulated keys on an observed context error. They return
wrapped errors preserving errors.Is; KeysByFilter does not implement an operation retry.
MaxRetries belongs to CAS update operations, not a shared listing-retry policy. The issue's suggested
analogy to MaxRetries is therefore not evidence of an existing equivalent policy.

collectFilteredKeys invokes Stop on return and ignores its result. It does not explicitly join the
lister producer or its watcher callback. Its post-close context check prevents the specified
cancellation-as-success shape; KeyLister has no terminal error channel.

A finite supplied context is not a strict wall-clock bound on a return that also synchronously
executes a contextless Stop.

### Pinned dependency behavior

- `go.mod:12` — `github.com/nats-io/nats.go v1.52.0`
- `processor/graph-index/nats_pin_test.go:17` — `graphIndexNATSServerPin = "2.14.4-alpine@sha256:f2123f533c2b0cada0a5c5ec434fb2b8cfe1cf220215ef9d7517e1372917ad66"`
- `processor/graph-index/nats_pin_test.go:18` — `graphIndexNATSGoPin     = "v1.52.0"`

Inspected the selected module under
/Users/coby/go/pkg/mod/github.com/nats-io/nats.go@v1.52.0.

| SDK location | Current behavior |
|---|---|
| jetstream/kv.go 1247 | Watcher updates channel capacity 256 |
| jetstream/kv.go 1278–1299 | Callback holds watcher mutex; plain sends of entry and initial nil marker |
| jetstream/kv.go 1305–1318 | Ordered push consumer, DeliverLastPerSubject, HeadersOnly, caller context |
| jetstream/kv.go 1335–1338 | Subscription closed handler locks watcher and closes updates |
| jetstream/kv.go 1344–1350 | InitialConsumerPending determines initial snapshot marker |
| jetstream/kv.go 1432–1458 | ListKeysFiltered wraps WatchFiltered; key channel capacity 256; forwarding goroutine |
| jetstream/kv.go 1451 | Plain `kl.keys <- entry.Key()` outside a context select |
| jetstream/kv.go 1452–1453 | Context is observed only when the outer select is reached |
| jetstream/kv.go 1465–1466 | KeyLister.Stop delegates to watcher.Stop |
| jetstream/kv.go 1206–1210 | watcher.Stop calls native subscription.Unsubscribe |
| js.go 2050–2054 | Context completion launches Unsubscribe through the subscription-owned goroutine |
| nats.go 5181–5203 | Unsubscribe removes subscription and may synchronously delete its JS consumer |
| js.go 1452–1467 | deleteConsumer calls legacy JetStream DeleteConsumer |
| nats.go 3578–3658 | Delivery goroutine invokes callback; closed handler runs only after callback returns |

Thus the SDK has two producer stages with blocking output sends. Canceling the context and
unsubscribing does not itself prove either blocked send has completed. Likewise, closure of the
SDK key channel is not an exposed terminal-error report.

These are dependency facts, not a demonstration that either condition happened during #1421.
In particular, native Unsubscribe removes the subscription from the connection map; an abandoned
callback does not automatically explain a later connection-drain timeout.

Dependency SHA256 identities:

1. jetstream/kv.go: fdd64fbd20753bd658aafbc299278deef3fbd44b6ffea0c476c506633d464c4a
1. nats.go: 88b6e77a878bc560c7c37974c43f25e0fcc84b98ebd00453cea02a4f7a1871ba
1. js.go: 01551a520646ec211cf0b41d764f30fe179d6bc590ff475263fd5a7d803c38e1

## 4. Cleanup and substrate authority

- `natsclient/test_client.go:36` — `testInfrastructureCleanupTimeout = 15 * time.Second`
- `natsclient/test_client.go:310` — `closeCtx, closeCancel := context.WithTimeout(context.Background(), timeout)`
- `natsclient/test_client.go:311` — `if err := client.Close(closeCtx); err != nil {`
- `natsclient/test_client.go:317` — `terminateCtx, terminateCancel := context.WithTimeout(context.Background(), timeout)`
- `natsclient/test_client.go:318` — `if err := container.Terminate(terminateCtx); err != nil {`
- `natsclient/test_client.go:760` — `WithTimeout(cfg.timeout),`
- `natsclient/test_client.go:761` — `WithMaxReconnects(0),`
- `natsclient/test_client.go:762` — `WithHealthInterval(0),`
- `natsclient/test_client.go:856` — `testClient, err := newTestClient(t.Context(), productionTestClientFactoryDeps, opts...)`
- `natsclient/test_client.go:865` — `t.Errorf("clean up NATS test infrastructure: %v", err)`
- `natsclient/test_client.go:933` — `tc.cleanupOnce.Do(func() {`
- `natsclient/test_client.go:938` — `return tc.cleanupErr`

TestClient cleanup attempts Client.Close and then container termination, each with fresh independent
authority. A close error is retained and does not suppress container cleanup. Terminate records its
first result and does not rerun cleanup.

The harness's 15-second WithTestTimeout configures connection/resource setup. The equal 15-second
cleanup constant is separately owned.

- `natsclient/client.go:580` — `m.closeMu.Lock()`
- `natsclient/client.go:586` — `m.closed.Store(true)`
- `natsclient/client.go:589` — `m.stopHealthMonitoring()`
- `natsclient/client.go:592` — `m.cancelConnectionLossTimer()`
- `natsclient/client.go:605` — `closeErr := m.drainAndCloseConnection(ctx, conn, drainTimeout)`
- `natsclient/client.go:692` — `closed := conn.StatusChanged(nats.CLOSED)`
- `natsclient/client.go:695` — `if err := conn.Drain(); err != nil {`
- `natsclient/client.go:707` — `drainTimer := time.NewTimer(drainTimeout)`
- `natsclient/client.go:713` — `case <-drainTimer.C:`
- `natsclient/client.go:720` — `conn.Close()`
- `natsclient/client.go:723` — `case <-ctx.Done():`

Client.Close serializes terminal admission, stops background facilities, starts native drain, and
waits for CLOSED or its selected bound. Native connection drain defaults to 30 seconds; the wrapper
reduces its own wait to the remaining cleanup deadline. On expiry it force-closes and returns an error.

SDK nats.go 6077–6172 snapshots subscriptions, initiates their drains, waits for subscription counts,
drains the response multiplexer, flushes publishers, and closes. Its native 30-second setting is not
changed by the wrapper's remaining-context calculation. SDK checkDrained at 5220 flushes and waits for
pending callbacks/messages; the client force-close path does not establish that every application
callback returned.

The observed 15-second error is the wrapper's bounded failure signal. The log does not identify
which native drain phase or subscription prevented completion.

## 5. Isolation and runner limits

- `processor/graph-index/lifecycle_integration_test.go:22` — `sharedLifecycleNATSClient, err = natsclient.NewSharedTestClient(`
- `processor/graph-index/lifecycle_integration_test.go:32` — `code := m.Run()`
- `processor/graph-index/lifecycle_integration_test.go:35` — `if err := sharedLifecycleNATSClient.Terminate(); err != nil {`
- `scripts/run-integration-tests.sh:8` — `readonly default_lock_dir="/tmp/semstreams-integration.lock"`
- `scripts/run-integration-tests.sh:44` — `"$(dirname -- "${BASH_SOURCE[0]}")/check-cleanup-roots.sh"`
- `scripts/run-integration-tests.sh:333` — `export GRAPH_INDEX_LATENCY_LOG="$latency_log"`
- `scripts/run-integration-tests.sh:341` — `go test -race -failfast -tags=integration -timeout=20m -count=1 -p 2 "${packages[@]}"`
- `.github/workflows/ci.yml:141` — `timeout-minutes: 25`
- `.github/workflows/ci.yml:156` — `run: scripts/run-integration-tests.sh`

The package TestMain unconditionally maintains its shared lifecycle container across m.Run, including
a focused load-harness run. The load harness additionally creates its own container. Its buckets
are isolated on that second server, but CPU, memory, filesystem, Docker, and test-process scheduling
remain shared resources.

The runner permits at most two test/build programs concurrently; it does not cap total containers
created within a program or reserve CPU/memory for a load test. The canonical lock serializes
integration invocations, not all unrelated work on the host.

The harness does not use t.Parallel. Its 15-minute parent is below the 20-minute package alarm,
but bare channel operations can still outlive that parent. CI adds a 25-minute whole-job ceiling.

## 6. Existing proofs and their boundaries

- `natsclient/kv_filter_test.go:20` — `func (l *testKeyLister) Stop() error {`
- `natsclient/kv_filter_test.go:21` — `l.stopped = true`
- `natsclient/kv_filter_test.go:57` — `cancel()`
- `natsclient/kv_filter_test.go:59` — `keys, err := collectFilteredKeys(ctx, lister)`
- `natsclient/kv_filter_test.go:68` — `cancel()`
- `natsclient/kv_filter_test.go:70` — `keys, err := collectFilteredKeys(ctx, lister)`
- `processor/graph-index/owner_filter_integration_test.go:144` — `cancelNow()`
- `processor/graph-index/owner_filter_integration_test.go:145` — `keys, listErr = store.KeysByFilter(cancelled, tt.filter)`
- `processor/graph-index/owner_filter_load_integration_test.go:726` — `cancel()`
- `processor/graph-index/owner_filter_load_integration_test.go:727` — `keys, err = store.KeysByFilter(cancelled, ">")`

The unit fake is a channel plus stopped bool; it has no producer, watcher, transport, or join.
The cancellation tests cancel before invoking collection. Real-NATS owner-filter cancellation
also supplies an already-canceled context. They prove error/partial-result policy for those inputs,
not cancellation while a real listing is producing.

The load test's normal path proves temporary consumer return before convergence and checks
subscriptions/slow-consumer/resource bounds only after successful convergence. Its failure path
does not retain those later observations.

- `natsclient/client_close_integration_test.go:51` — `draining := conn.StatusChanged(nats.DRAINING_SUBS)`
- `natsclient/client_close_integration_test.go:58` — `closeDone <- client.Close(ctx)`
- `natsclient/client_close_integration_test.go:88` — `releaseOnce.Do(func() { close(releaseHandler) })`
- `natsclient/subscription_integration_test.go:83` — `func TestIntegration_SubscriptionDrainAfterConnectionCloseJoinsCallback(t *testing.T) {`
- `natsclient/test_client_readiness_test.go:424` — `func TestCleanupTestInfrastructure_PreservesCloseAndTerminateErrors(t *testing.T) {`
- `natsclient/test_client_readiness_test.go:451` — `func TestCleanupTestInfrastructure_GivesEachOperationItsOwnBudget(t *testing.T) {`

Existing native drain tests establish callback ordering for ordinary subscriptions, and helper
tests establish error retention and independent cleanup budgets. They do not exercise the SDK's
two-stage filtered-list producer at the incident seam.

The temporary-consumer convergence poll has a five-second assertion window, but its callback calls Info with
the harness's fifteen-minute parent context. That window does not cancel or join an in-flight callback. The
callback reduces an Info error to false, so the final assertion does not preserve the last error. The recorded
incident passed this phase; this is a separate boundedness and diagnostic gap, not its established cause.

- `processor/graph-index/owner_filter_load_integration_test.go:450` — `require.Eventually(t, func() bool {`
- `processor/graph-index/owner_filter_load_integration_test.go:451` — `info, infoErr := fixture.stream.Info(ctx)`
- `processor/graph-index/owner_filter_load_integration_test.go:452` — `return infoErr == nil && info.State.Consumers == baselines[fixture.name]`
- `processor/graph-index/owner_filter_load_integration_test.go:453` — `}, 5*time.Second, 20*time.Millisecond, "%s temporary consumers did not return to baseline", fixture.name)`

The owner harness's measured filter results and final forward convergence assertions compare cardinality.
They do not establish the exact match-set identity required by the current specification: a wrong set of the
same size can pass. The sibling predicate smoke harness compares sorted returned keys against seeded truth.
This is a proof gap, not an explanation of the deadline error or a scope expansion into unrelated coverage.

- `processor/graph-index/owner_filter_load_integration_test.go:485` — `require.Len(t, keys, fixture.wantForward, "%s did not converge", fixture.name)`
- `processor/graph-index/owner_filter_load_integration_test.go:530` — `require.Len(t, keys, want, label)`
- `openspec/specs/graph-index/spec.md:201` — `The continuously-running CI profile is a regression guard, not activation evidence. It MUST assert exact match-set`
- `openspec/specs/graph-index/spec.md:202` — `correctness for every owner and forward filter, exact convergence after churn, the typed-error ceiling, p95 and p99`
- `openspec/specs/graph-index/spec.md:203` — `latency budgets derived from the supervised record, a bounded dispatcher queue, temporary consumers returning to`
- `openspec/specs/graph-index/spec.md:204` — `every per-store baseline, released temporary subscriptions, zero slow consumers, and the server resident-set bound.`
- `processor/graph-index/predicate_layout_smoke_integration_test.go:456` — `final, err := stores.membership.KeysByFilter(ctx, codec.exactFilter(predicateSmokeHot))`
- `processor/graph-index/predicate_layout_smoke_integration_test.go:457` — `require.NoError(t, err)`
- `processor/graph-index/predicate_layout_smoke_integration_test.go:458` — `sort.Strings(final)`
- `processor/graph-index/predicate_layout_smoke_integration_test.go:459` — `require.Equal(t, truth.hotKeys, final, "churn must converge to exact seeded truth")`

## 7. Current consumers and adjacent existing shapes

Production direct KeysByFilter consumers include graph-index owner reconciliation, delete preflight,
and predicate query handlers. KeysByPrefix extends the same path to NAME/incoming reads and
graph-ingest queries. Package FilteredKeys serves clustering, inference, trajectory reading,
graph-query tools, temporal queries, and E2E incoming reads.

- `processor/graph-index/owner_reconcile.go:47` — `existingKeys, err := bucket.KeysByFilter(ctx, ownerFilter)`
- `processor/graph-index/owner_reconcile.go:52` — `if err != nil {`
- `processor/graph-index/owner_reconcile.go:54` — `return errs.WrapTransient(err, "Component", "reconcileOwnedRows",`
- `processor/graph-index/owner_reconcile.go:63` — `existing[key] = struct{}{}`
- `processor/graph-index/component.go:2099` — `set.keys, listErr = set.bucket.KeysByFilter(ctx, set.filter)`
- `processor/graph-index/component.go:2123` — `return errs.WrapTransient(errIndexWritePartial, "Component", "DeleteFromIndexes",`

Reconciliation already refuses a failed list and deduplicates successful keys. Delete preflight
collects all required lists before deleting anything. These paths make partial-list success a
production correctness issue, not merely test data.

The problem shapes are owned asynchronous production, bounded failure observation, complete-snapshot
refusal, and terminal work before substrate teardown. Existing instances on other planes are:

| Existing owner | Evidence | What it already owns |
|---|---|---|
| Shared lifecycle test owner | component/lifecycle_test_suite.go 64–92 | Attempt-before-call fence, concrete/bound error accounting, Stop-before-cancel |
| Shared parallel lifecycle suite | component/lifecycle_test_suite.go 254–289 | Stop admission after a failure, drain results while already-owned workers finish |
| Core NATS Subscription | natsclient/client.go 763–778, 796–818 | Closed-handler completion signal distinct from initiating native Drain |
| Graph-ingest entity-state guard | processor/graph-ingest/component.go 1280, 1297, 1307–1323 | Stop native watcher, then drain Updates to closure to release callback/mutex |
| Readiness watcher | graph/readiness/watcher.go 258–281, 347–378 | Local child cancellation, owned outer goroutine done signal, watcher close/error handling |
| Storage-report reclamation | natsclient/storage_report.go 696–717 | Fully collect before mutation and reject expired context after collection |
| TestClient substrate | natsclient/test_client.go 303–323 | Independent terminal budgets and preservation of both cleanup errors |
| Canonical integration runner | scripts/run-integration-tests.sh 152–178 | Terminate and reap an exact owned image-pull process before relinquishing ownership |

- `component/lifecycle_test_suite.go:65` — `o.attempted = true // A returned error or panic does not authorize an implicit retry.`
- `component/lifecycle_test_suite.go:87` — `defer o.cancelStart() // Stop completes before accepted Start authority ends.`
- `component/lifecycle_test_suite.go:282` — `workers.Wait()`
- `component/lifecycle_test_suite.go:287` — `report(err) // Caller reports while workers finish already-owned instances.`
- `natsclient/client.go:767` — `sub.SetClosedHandler(func(string) { s.closeDone() })`
- `graph/readiness/watcher.go:280` — `w.cancel()`
- `graph/readiness/watcher.go:281` — `<-w.done`
- `natsclient/storage_report.go:715` — `if err := ctx.Err(); err != nil {`

Graph-ingest already owns the closely matching shape of stopping a native KV watcher and continuing to consume
updates to closure. Its source identifies a full-channel callback holding the watcher mutex and the resulting
closed-handler hazard. The guard invokes this helper on context cancellation and the end-of-snapshot marker.

- `processor/graph-ingest/component.go:1280` — `c.stopEntityStateGuardWatcher(watcher, updates)`
- `processor/graph-ingest/component.go:1297` — `c.stopEntityStateGuardWatcher(watcher, updates)`
- `processor/graph-ingest/component.go:1308` — `// (design D1 mandatory shape): Stop(), then KEEP READING the updates channel`
- `processor/graph-ingest/component.go:1309` — `// until nats.go closes it, discarding entries. The nats.go update callback`
- `processor/graph-ingest/component.go:1310` — `// blocks on a full channel while holding the watcher mutex, so an unread`
- `processor/graph-ingest/component.go:1311` — `// channel would wedge the connection's async-callback dispatcher (via`
- `processor/graph-ingest/component.go:1312` — `// SetClosedHandler). The post-Stop closure here is deliberate and MUST NOT be`
- `processor/graph-ingest/component.go:1313` — `// classified as watch loss.`
- `processor/graph-ingest/component.go:1314` — `func (c *Component) stopEntityStateGuardWatcher(watcher jetstream.KeyWatcher, updates <-chan jetstream.KeyValueEntry) {`
- `processor/graph-ingest/component.go:1315` — `_ = watcher.Stop()`
- `processor/graph-ingest/component.go:1317` — `for range updates {`
- `processor/graph-ingest/component.go:1318` — `discarded++ // deliberate drain-to-close; entries are discarded unvalidated`

This helper consumes native watcher updates directly. It does not traverse the extra KeyLister forwarding
goroutine and keys channel in the SDK inventory. It ignores the Stop error, and its synchronous range has no
independent deadline. This is a repository precedent for the callback-release problem shape, not evidence of
#1421's cause, bounded completion in every failure case, or a chosen replacement for filtered listing.

These are existing shapes, not automatic precedents for every edge. Readiness Stop is contextless;
the SDK watcher Stop beneath it still is not a native callback join. The subscription-drain
change deliberately declined several speculative SDK edge cases; its archived design requires
new occurrence evidence before reintroducing them.

No new durable, communication, or runtime-coordination primitive has been proposed. Therefore no
new-primitive collision or adoption sweep is triggered at this inventory stage. The existing-owner
table identifies the relevant overlap if a later design proposes one.

## 8. Contracts, rulings, and neighboring work

- `openspec/specs/graph-index/spec.md:184` — `Performance MUST be gated by absolute budgets, not comparison, and exactly one absolute ceiling MUST apply to a`
- `openspec/specs/graph-index/spec.md:201` — `The continuously-running CI profile is a regression guard, not activation evidence. It MUST assert exact match-set`
- `openspec/specs/graph-index/spec.md:231` — `- **THEN** the operation returns a context-deadline error and the guard fails on that error`
- `openspec/specs/graph-index/spec.md:233` — `- **AND** no partial key set is accepted as a successful owner snapshot`
- `docs/contributing/01-testing.md:482` — `Test I/O contexts MUST derive from `t.Context()` and then narrow the deadline:`
- `docs/contributing/01-testing.md:546` — `Failures involving asynchronous or external state MUST identify the condition, elapsed time, attempts, last observed`
- `docs/contributing/01-testing.md:596` — `Blanket conversion to shared containers is explicitly out of scope. The runner's `-p 2` package cap is not a`

Read in full: current graph-index, nats-kv-keys, nats-subscription-lifecycle,
nats-client-diagnostics, and test-cleanup-policy specs; ADR-077; active proposal/tasks;
the six supplied issue snapshots; the subscription-drain archived design.

Binding #1284 ruling:
https://github.com/C360Studio/semstreams/issues/1284#issuecomment-5635299542

The supervised run owns activation evidence. CI retains the framework deadline as its one absolute
listing ceiling, with genuine deadline breaches remaining red. The subsequent seven-part ruling
keeps CI repetitions and percentile calibration with #1287 and rejects a dedicated CI job as a
replacement for supervised evidence:
https://github.com/C360Studio/semstreams/issues/1284#issuecomment-5640631023

| Record | Current relation |
|---|---|
| #1421, open beta.165 | This observed expiry/drain sequence; suggestions in its body are unruled |
| #1284, closed | Retired artificial per-operation budget; current red-on-framework-expiry authority |
| #1287, open beta.165 | Both profiles' percentile calibration, sample adjacency, repetitions, p50/per-class questions |
| #1286, open | Sibling smoke harness's unreachable 10-second comparison and unit/budget defects |
| #1054, closed | Prior drain symptoms across packages; similarity of error text does not establish same cause |
| #736, closed at inspection | Historical container-pressure/start/mapping evidence and measured reason for runner `-p 2` |
| #1293, open rc.1 | Common-gate duplication, failure visibility, cancellation and descendant ownership |
| #1417, open beta.163 | Remaining cleanup remediation; graph-index package batch remains separate |
| #1349, open | Shared KV fake fidelity; does not substitute a fake for real SDK lifecycle evidence |
| #1372 / merged #1373 | Core Subscription.Drain joins callback completion; deliberate excluded edge cases remain recorded |
| #1426 / #1427 | Claude's independently owned E2E work; no overlap inferred |

No active target-state change other than owner-load-reliability matched the bounded current-change
search. At the initial inventory checkpoint, rule cleanup PR #1429 was pending merge. The coordinator
subsequently confirmed all eight checks passed and #1429 merged as 10f04fe0. This inventory remains pinned
to the unchanged #1421 claim baseline e811399e950d4eda4bed9f140a0ff73fa6882001.

The no-new-exported-surface category is empty: the claim introduces no symbol, bucket, configuration
field, subject, or public test helper. That emptiness follows from the proposal and source diff,
not from an assertion that no future design could require one.

## 9. Adopter seam inventory

This is required because the traced failure crosses exported natsclient listing APIs. The present
claim has not selected a production change.

Persona: a SemDragon developer calling its existing GraphClient.ListEntitiesByType, without knowing
the SDK implements list as a watcher plus a forwarding goroutine.

Actual external consumer:
SemDragon at 07f4de9b65887801ff18a7273d14233023049321,
graphclient.go line 290: `keys, err := store.KeysByPrefix(ctx, prefix)`.
Lines 291–292 preserve the error. Lines 284–290 acquire the real KVStore and invoke the public path.
Its local listing limit is applied only after full key collection.

A bounded tracked-Go survey also found SemSpec's KVStore interface declares KeysByPrefix at
agentgraph/graph.go line 77, at 5a9496eecc453747f4bc557b95444db6304c1420.
The inspected file was not locally modified; the SemSpec checkout contains unrelated ongoing work.
No sister repository was edited.

### What must this adopter know now?

1. Construct the prefix/filter grammar correctly; KeysByPrefix appends `>`.
2. Supply nonnil operation context and handle the returned error; KVStore supplies the default five-second child bound.
3. Package FilteredKeys is a different entry point that relies on the caller's bound.
4. A successful list is not a stable ordered, deduplicated transaction under arbitrary concurrent mutation.
5. Public synchronous return/Stop does not expose native producer/callback join evidence.

The first four are visible API/semantic obligations. The fifth is an ownership finding if correct
teardown requires downstream knowledge of hidden SDK stages. More than two correctness facts are
already being carried at this seam; this inventory records that debt without selecting its remedy.

### What happens if the developer does nothing?

The existing KVStore default bounds the listing and returns an errors.Is-compatible context error
when the deadline is observed. They do not need to invent a second five-second number.
KeysByPrefix callers propagate that error through their own domain wrapper.

The framework owns lister.Stop; the adopter cannot access that hidden lister to join it. No API
reports how many SDK producer stages have finished. A caller that treats operation return as proof
of all internal completion has no supporting contract or observation at this seam.

### Where do they find out?

1. Invalid/nil context usage: runtime behavior, not a compile-time ownership contract.
1. Expired listing: returned wrapped context error.
1. Partial-snapshot refusal: production wrapper behavior and tests.
1. Native teardown delay: later cleanup error/log; no relationship to the original listing is exposed.
1. Hidden producer completion: nowhere on the public listing result.

### What should they have to know?

The operation's semantic input and its success/error result. They should not need to predict the
SDK's channel capacity, scheduling delay, temporary-consumer lifetime, or hidden unsubscribe/join
ordering to safely use a synchronous framework listing.

The measured gap is between the synchronous operation/error surface and unobserved native work
completion. Whether that gap explains #1421 is unresolved.

### External discovery boundary

Tracked-Go spellings searched in semops, semsource, semconnect, semdev, semteams, semspec,
semdragon, and semboids: KeysByFilter, KeysByPrefix, FilteredKeys, NewKVStore.

Only SemDragon had a direct production call to the shared prefix-list path in this bounded survey;
SemSpec declared the interface method. Other NewKVStore consumers used Get/Watch or other methods.
Zero direct spelling hits do not prove zero transitive graph consumers or authorize deleting a surface.

## 10. Unresolved causal questions and missing evidence

These are evidence questions, not implementation tasks or treatment options.

1. Did the failed NAME listing stop in consumer creation, message delivery, initial-snapshot completion,
   framework collection, or deferred SDK Stop? The original wrapped error does not distinguish them.

2. Did cancellation leave either SDK channel producer or delivery callback alive? Source permits
   blocked sends; current tests and the incident log do not demonstrate the real canceled path.

3. Which native subscription or drain phase consumed the subsequent cleanup budget? The log has only
   the aggregate timeout. An unjoined callback after Unsubscribe cannot by itself be equated to an
   active subscription holding the connection drain.

4. Did CPU scheduling, disk latency, server overload, transport loss, or ordered-consumer behavior
   precede the deadline? Initial /varz and the suite's Docker-info measurement are not failure-time evidence.

5. Does premature SDK channel closure with a live caller context preserve complete-snapshot truth?
   The KeyLister interface has no terminal error; the wrapper checks context, not a completion marker.
   This adjacent correctness question is not an observed #1421 result.

6. What is the exact wall-clock and owned-work behavior when seed errors saturate the buffer, a
   worker result is absent, or an assertion exits the concurrent phase? The source hazards are definite;
   bounded causal execution evidence has not yet been collected.

7. The docs describe a ten-second cleanup ceiling while canonical TestClient uses fifteen seconds.
   This existing discrepancy is not permission to alter either bound in this issue.

Evidence needed to resolve attribution includes phase-specific operation timing, the native SDK
stage at cancellation, completion observations for the relevant producer/callbacks, and native
subscription/consumer/server state during the failure and teardown. Any controlled reproduction must
identify its injected condition and distinguish reproducing a reachable hazard from reproducing the
historical incident. A passing quiet-host run cannot establish the cause or removal of a rare stall.


## Searches and limitations

All structural searches were initiated with gopls. Initial sandboxed workspace_symbol calls
returned empty results, and references returned only the opened package. Verbose loading showed
incomplete package loading/cache-permission failures. Those initial empty results were rejected
as absence evidence. Repeating with authorized normal cache access loaded 523 integration package
variants and returned the reference sets used above.

Structural commands, repeated as necessary with `GOFLAGS=-tags=integration`:

- `gopls workspace_symbol -matcher=fuzzy KeysByFilter`
- `gopls workspace_symbol -matcher=fuzzy ownerLoad`
- `gopls workspace_symbol -matcher=fuzzy NewTestClient`
- `gopls references natsclient/kv.go:547:19` — initial wrong position; not evidence
- `gopls references natsclient/kv.go:537:20` — 32 in-repo direct references
- `gopls references natsclient/kv.go:560:6` — 14 in-repo references
- `gopls references natsclient/kv.go:522:20` — 19 in-repo references
- `gopls -v references processor/graph-index/owner_filter_load_integration_test.go:483:31`
  — incomplete sandbox load; no-object result rejected
- `gopls call_hierarchy natsclient/kv.go:582:6`
  — both shared callers and KeyLister Keys/Stop dependencies
- `gopls call_hierarchy processor/graph-index/owner_filter_load_integration_test.go:288:6`
  — caller plus harness's production/test/dependency callees
- `gopls references processor/graph-index/keyed_dispatcher.go:20:6`
  — five construction sites
- `gopls workspace_symbol -matcher=fuzzy CollectFiltered`
- `gopls workspace_symbol -matcher=fuzzy ListKeys`
- `gopls implementation /Users/coby/go/pkg/mod/github.com/nats-io/nats.go@v1.52.0/jetstream/kv.go:282:2`
  — concrete selected SDK lister plus local fake/interface implementations

Repository tracked-content searches included:

- `git grep -n -E 'OwnerFilterLoadHarness|KeysByFilter|drain timeout|test:integration|integration.*(parallel|lock)|5s|5 s' -- openspec/specs docs/adr docs/contributing scripts/run-integration-tests.sh .github/workflows/ci.yml Taskfile.yml`
- `git grep -n -E 'KeysByFilter|FilteredKeys|collectFilteredKeys' -- '*.go'`
- `git grep -n -E 'KeyLister|collectFilteredKeys|ListKeysFiltered|KeysByFilter' -- natsclient/*test.go`
- `git grep -n -E 'drain|Drain|Closed|func .*Close' -- natsclient/client.go`
- `git grep -n -E 'TestClient|WithTestTimeout|WithFileStorage|WithMonitoring|testCleanup|cleanup|SHARED|NATS_URL' -- natsclient/test_client.go`
- `git grep -n -E 'Close\(|drainTimeout|DrainTimeout|WithoutCancel|context.Context' -- natsclient/client.go`
- `git grep -n -E 'js.New|jetstream.New|metricsCancel|startMetrics|startHealth|Context\(|SetLogger' -- natsclient/client.go`
- `git grep -n -E 'func (predicateIndexForwardFilter|nameIndexForwardFilter|incomingIndexTargetFilter|nameIndexKey)|b49342a7' -- processor/graph-index`
- `git grep -n -E 'bounded|filtered|cancel|partial|retries|temporary|snapshot|Stop' -- openspec/specs/nats-kv-keys/spec.md`
- `git grep -n -E 'Owner|owner|harness|deadline|KeyLister|Drain' -- openspec/changes/*/proposal.md openspec/changes/*/tasks.md`
  — active owner-load-reliability only
- `git grep -n 'func TestMain' -- processor/graph-index`
  — lifecycle_integration_test.go only
- `git grep -n -E 'SetLogger|Skip\(|sync.Once|context.Background|init\(|NewSharedTestClient|GetTestNATSClient|Terminate|testing.M' -- test/integration/nats.go test/integration/nats_client.go processor/graph-index/*integration_test.go`
- `git grep -n -E 'timeout|cancell|deadline|partial|blocked|saturat' -- natsclient/kv_filter_test.go processor/graph-index/owner_filter_integration_test.go processor/graph-index/owner_filter_load_integration_test.go natsclient/kv_key_contract_integration_test.go`
- `git grep -n -E 'worker|joined|fail|pending|release' -- component/lifecycle_test_suite.go`
- `git grep -n -E 'Cleanup|cleanup|deadline|Terminate.*failure|Client.*failure|close.*error' -- natsclient/test_client_factory_test.go natsclient/test_client_readiness_test.go`
- `git grep -n -E 'func.*(Watch|Drain|Close|snapshot|Snapshot)|watcher.Stop|lister.Stop|case <-ctx.Done' -- natsclient/storage_report.go graph/readiness/watcher.go graph/embedding/watchkv_test.go pkg/lifecycle component/lifecycle_test_suite.go`
- `git grep -n -E 'jsMetrics:|metricsInterval:|healthInterval:' -- natsclient/client.go`
- `git grep -n -E 'drain|cleanup|watcher|KeyLister' -- openspec/changes/archive/2026-08-*/*proposal.md`
- `git grep -n -E 'func.*client|newTestClient|testClientFactory|DefaultKVOptions|Timeout:' -- natsclient/test_client.go`
- `git grep -n 'nats.go\|testify' -- go.mod`

Zero-hit searches used as bounded evidence:

- `git grep -n -E 'time.Sleep|Cleanup|defer .*stopSampling|defer .*cancelDispatch|runtime.Stack|pprof|retry|Retry|WithoutCancel|Parallel' -- processor/graph-index/owner_filter_load_integration_test.go`
  — zero; no lexical worker cancellation/finalization or in-test stack/retry mechanism under those spellings.
- `git grep -n -E 'max.*(cpu|memory)|Cpu|CPU|NanoCPUs|Memory:|HostConfigModifier' -- natsclient/test_client.go scripts/run-integration-tests.sh .github/workflows/ci.yml`
  — zero configured CPU/memory reservation under these spellings.
- `git grep -n -E 'KeysByFilter|FilteredKeys|KeysByPrefix' -- schemas specs examples configs`
  — zero; no identified configuration/schema surface for these APIs.
- The bounded sister searches and their positive/negative results are recorded in §9.

A few discovery commands used unmatched shell globs (`.gopls*`, `natsclient/*watch*.go`, an assumed
archive directory); zsh rejected them before search. Those are not zero-hit evidence. Subsequent
quoted tracked-file searches and explicit paths supplied the facts above.

Dependency searches used `rg -n` against the exact selected module files for ListKeysFiltered,
WatchFiltered, Keys, Stop, Updates, Unsubscribe, drainConnection, checkDrained, waitForMsgs,
deleteConsumer, StatusChanged, and RemoveStatusListener; their complete implementations were
read in bounded ranges.

Incident searches:

- `rg -n 'OwnerFilterLoadHarness|phase=|drain timeout|context deadline|NATS_SERVER_URL|NATS_CONTAINER|graph-index|go test|Concurrency|parallelism|Started' /private/tmp/gh1421-incident-job.log`
  — broad discovery was too large; narrowed to the full failure interval.
- `rg -n 'goroutine [0-9]+ \[|panic:|DATA RACE|phase=resource|slow consumer|Slow consumer|Consumer.*ERROR|deadline|Drain timeout|docker info latency' /private/tmp/gh1421-incident-job.log`
  — Docker info, deadline and drain records only; no stack/panic/race/final-resource record.

History and issue reads:

- `git log --oneline -8 -- natsclient/kv.go natsclient/client.go processor/graph-index/owner_filter_load_integration_test.go`
- Supplied complete snapshots #1421, #1284, #1287, #1054, #1293, #1417.
- Read-only `gh issue view` #736, #1286, #1349 and #1054 closed-event timeline.
- #1054's closed event has no commit_id; this inventory does not infer a repair from closure alone.

No test, stress run, mutation, timeout change, baseline change, or new infrastructure was performed.

Bounded review follow-up reads:

```text
nl -ba processor/graph-ingest/component.go | sed -n '1255,1330p'
nl -ba processor/graph-index/owner_filter_load_integration_test.go | sed -n '435,540p'
nl -ba openspec/specs/graph-index/spec.md | sed -n '190,210p'
nl -ba processor/graph-index/owner_filter_integration_test.go | sed -n '445,465p'
rg --files processor/graph-index -g '*predicate*' -g '*smoke*' -g '*contract*'
nl -ba processor/graph-index/predicate_layout_smoke_integration_test.go | sed -n '445,465p'
```

The attempted owner_filter_integration_test.go range returned no lines and was the wrong file for the sibling
proof; it is not absence evidence. The bounded filename lookup identified predicate_layout_smoke_integration_test.go,
whose requested range contains the exact seeded-truth assertion. No causal experiment or broad enumeration ran.
