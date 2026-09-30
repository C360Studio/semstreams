# Predicate-layout listing expiry — bounded inventory

base: e479605a55095027aee4b1ce87e5b70af7cf6c18

## Scope and evidence identity

This refresh covers CI 36659632924, failed required Test job on PR #1435, under #1421: the failed hash-catalog
namespace join, preceding fixture work, common KV/native ownership and teardown. It changes no accepted observer
design and establishes no production cause. Architect gh1433_inventory_design enumerated this inventory read-only;
root materialized its evidence. No tests or native experiments were run for this refresh.

Worktree was clean; inspected smoke and natsclient sources have no diff from origin/main.
Failed log: /tmp/gh1435-ci-failed.log
SHA-256: 2542c579f06dc50965823ba0ca96e75eaee9c19f927901af957cb939a958ecf8

| Source | SHA-256 |
|---|---|
| processor/graph-index/predicate_layout_smoke_integration_test.go | 6dd8002ff89433e14d05e88ded4378de1e5c0a80d8273ca30c5c421201c04817 |
| processor/graph-index/predicate_hash_candidate_test.go | 6fa852bd013b5779d6970e88131e5dbc39a3f4552f3c6a984842fcb3fd842d81 |
| natsclient/kv.go | 9b9b4c40aa6f65b66bdcb68a7db881e0fdfb4bb8d46f8b2af4d42d86d1696725 |
| natsclient/client.go | 10efcf80a68f8d5f52e3289fd5ef897d7c0372befde6b340963f9c1df017ae30 |
| natsclient/test_client.go | 5989421bccc4c22d079afaf2a8f3baad3c227aad0894134270cbae343438ca7a |

Inherited inventory: openspec/changes/archive/2026-09-29-owner-listing-expiry-recurrence/inventory.md.
Its four selected SDK hashes were checked unchanged, and its four deletion origins remain applicable.

## 1. Claimed gap and exact failure

Failed log lines3438–3457 establish TestIntegration_PredicateLayoutSmoke/hash-catalog (28.09s), default profile
5000entities/spread20/five repetitions, seed5021membershiprows/22catalogrows in397.22369ms. Exact-predicate,
entity-owner and maximum-owner each completed five repetitions. The failure label was
hash-catalog-namespace-catalog-join; nested error names robotics.status.ready, hashed membership filter and
context deadline exceeded. Error assertion at479, callers357/170/141. Infrastructure cleanup separately reports
Client.Close drain timeout14.9999892s. No failing repetition, operation duration, construction return, Stop result,
partial count, consumer identity or deadline stack is retained. Subtracting15s from28.09s does not measure the call.
Owner-load in the same run passed:110.379117ms,126.219027ms,131.128279ms,121.165223ms,143.925579ms; it does not clear this failure.

- `processor/graph-index/predicate_layout_smoke_integration_test.go:357` — `measurePredicateSmokeOperation(t, profile, codec.name+"-namespace-catalog-join", func() ([]string, error) {`
- `processor/graph-index/predicate_layout_smoke_integration_test.go:358` — `return predicateSmokeHashNamespaceJoin(ctx, stores, "robotics.*.*")`
- `processor/graph-index/predicate_layout_smoke_integration_test.go:477` — `got, err := operation()`
- `processor/graph-index/predicate_layout_smoke_integration_test.go:478` — `duration := time.Since(started)`
- `processor/graph-index/predicate_layout_smoke_integration_test.go:479` — `require.NoError(t, err, label)`
- `processor/graph-index/predicate_layout_smoke_integration_test.go:480` — `require.Less(t, duration, profile.operationBudget, "%s rep %d", label, repetition)`
- `processor/graph-index/predicate_layout_smoke_integration_test.go:483` — `durations = append(durations, duration)`
- `processor/graph-index/predicate_layout_smoke_integration_test.go:485` — `assertPredicateSmokeLatencies(t, label, durations, profile)`

Error assertion precedes duration checking, result validation, retention and output.

## 2. Current spellings and route

- `processor/graph-index/predicate_hash_candidate_test.go:8` — `// Hash predicate helpers preserve the retired candidate exclusively for the`
- `processor/graph-index/predicate_hash_candidate_test.go:9` — `// representation decision tests. Production never compiles, reads, or writes it.`
- `processor/graph-index/predicate_hash_candidate_test.go:16` — `return hashPredicateCandidateHex(predicate) + "." + entityID`
- `processor/graph-index/predicate_hash_candidate_test.go:20` — `return hashPredicateCandidateHex(predicate) + "." + wildcardPositions(6)`
- `processor/graph-index/predicate_layout_smoke_integration_test.go:47` — `membershipBucket: "PRED_SMOKE_HASH_MEMBERS",`
- `processor/graph-index/predicate_layout_smoke_integration_test.go:48` — `catalogBucket:    "PRED_SMOKE_HASH_CATALOG",`
- `processor/graph-index/predicate_layout_smoke_integration_test.go:59` — `memberKey:        predicateIndexKey,`
- `processor/graph-index/predicate_layout_smoke_integration_test.go:128` — `// TestIntegration_PredicateLayoutSmoke is a decision harness, not a production`
- `processor/graph-index/predicate_layout_smoke_integration_test.go:129` — `// selector. Both candidates must satisfy the same absolute gates; their results`
- `processor/graph-index/predicate_layout_smoke_integration_test.go:130` — `// are recorded independently and are never compared as pass/fail thresholds.`

Typed hashPredicateCandidateForwardFilter references: smoke50/387, kv_contract_test.go129/158.
Namespace helper has one typed caller: smoke358.

- `processor/graph-index/predicate_layout_smoke_integration_test.go:384` — `sort.Strings(predicates)`
- `processor/graph-index/predicate_layout_smoke_integration_test.go:386` — `for _, predicate := range predicates {`
- `processor/graph-index/predicate_layout_smoke_integration_test.go:387` — `members, listErr := stores.membership.KeysByFilter(ctx, hashPredicateCandidateForwardFilter(predicate))`
- `processor/graph-index/predicate_layout_smoke_integration_test.go:389` — `return nil, fmt.Errorf("join predicate %q: %w", predicate, listErr)`
- `processor/graph-index/predicate_layout_smoke_integration_test.go:391` — `keys = append(keys, members...)`

Catalog listing380 precedes loop. Intended successful fixture has21robotics predicates:20one-member spread keys and
5000-member hot predicate last after sorting; maximum-length predicate is outside robotics.*.*. Nominal complete join
executes22distinct listings, but actual preceding count/repetitions are unknown: catalog exact-set validation360–363
only runs after successful join. Each listing gets a fresh5s child of the15m caller.

- `natsclient/kv.go:39` — `Timeout:               5 * time.Second,`
- `natsclient/kv.go:70` — `return context.WithTimeout(ctx, kv.options.Timeout)`
- `natsclient/kv.go:538` — `ctx, cancel := kv.applyTimeout(ctx)`
- `natsclient/kv.go:541` — `lister, err := kv.bucket.ListKeysFiltered(ctx, pattern)`
- `natsclient/kv.go:550` — `keys, err := collectFilteredKeys(ctx, lister)`
- `natsclient/kv.go:583` — `defer func() { _ = lister.Stop() }()`
- `natsclient/kv.go:588` — `case <-ctx.Done():`
- `natsclient/kv.go:589` — `return nil, ctx.Err()`
- `natsclient/kv.go:590` — `case key, ok := <-lister.Keys():`

Construction547 and collection552 use same error prefix. No retry; expiry rejects partial results. Stop runs
synchronously before collection returns, discarding its error. Five-second context proves neither5s wall return
nor native joining. Both workloads reach this route; workload differences/shared error do not establish shared cause.

## 3. Fixture and cleanup ownership

- `processor/graph-index/predicate_layout_smoke_integration_test.go:148` — `testClient := natsclient.NewTestClient(t,`
- `processor/graph-index/predicate_layout_smoke_integration_test.go:150` — `natsclient.WithFileStorage(),`
- `processor/graph-index/predicate_layout_smoke_integration_test.go:153` — `natsclient.WithTestTimeout(15*time.Second),`
- `processor/graph-index/predicate_layout_smoke_integration_test.go:155` — `ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)`
- `processor/graph-index/predicate_layout_smoke_integration_test.go:156` — `defer cancel()`
- `processor/graph-index/predicate_layout_smoke_integration_test.go:208` — `membershipRaw: membershipRaw, membership: nc.NewKVStore(membershipRaw), membershipStream: membershipStream,`
- `processor/graph-index/predicate_layout_smoke_integration_test.go:219` — `stores.catalog = nc.NewKVStore(catalogRaw)`
- `processor/graph-index/predicate_layout_smoke_integration_test.go:169` — `consumerEvidence := provePredicateSmokeConsumerLifecycle(t, ctx, stores)`
- `processor/graph-index/predicate_layout_smoke_integration_test.go:170` — `measurePredicateSmokeQuiescent(t, ctx, codec, stores, truth, profile)`
- `processor/graph-index/predicate_layout_smoke_integration_test.go:171` — `measurePredicateSmokeChurn(t, ctx, codec, stores, truth, profile)`
- `processor/graph-index/predicate_layout_smoke_integration_test.go:173` — `assertPredicateSmokeConsumerReturn(t, ctx, stores, consumerEvidence)`

Each codec owns a separate file-backed fixture. WithTestTimeout15s controls setup/connection, not KV's5s.
Seed32workers queue/close/wait/error-check291–314; seed completed. Failure precedes churn. Churn407–445 failure-join
limits are adjacent facts, not this failure's explanation. Probes before measurement use same15m context:

- `processor/graph-index/predicate_layout_smoke_integration_test.go:556` — `lister, err := raw.ListKeysFiltered(ctx, filter)`
- `processor/graph-index/predicate_layout_smoke_integration_test.go:581` — `stopErr := lister.Stop()`
- `processor/graph-index/predicate_layout_smoke_integration_test.go:586` — `for range lister.Keys() {`
- `processor/graph-index/predicate_layout_smoke_integration_test.go:589` — `info, infoErr := stream.Info(ctx)`

Probes observeInfo/Stop/drainKeys/baseline. Success does not prove native watcher-callback join. Fatal before581
has no lexical lister finalization; not recorded failure. Info within5s Eventually retains15m context, so5s is not
per-Info cancellation. Fatal measurement skips remaining quiescent checks, churn, handle parity and resource assertions.
Deferred caller cancel precedes registered fixture cleanup.

- `natsclient/test_client.go:36` — `testInfrastructureCleanupTimeout = 15 * time.Second`
- `natsclient/test_client.go:310` — `closeCtx, closeCancel := context.WithTimeout(context.Background(), timeout)`
- `natsclient/test_client.go:311` — `if err := client.Close(closeCtx); err != nil {`
- `natsclient/test_client.go:317` — `terminateCtx, terminateCancel := context.WithTimeout(context.Background(), timeout)`
- `natsclient/test_client.go:318` — `if err := container.Terminate(terminateCtx); err != nil {`
- `natsclient/test_client.go:863` — `t.Cleanup(func() {`
- `natsclient/test_client.go:864` — `if err := testClient.Terminate(); err != nil {`
- `natsclient/test_client.go:933` — `tc.cleanupOnce.Do(func() {`
- `natsclient/test_client.go:938` — `return tc.cleanupErr`
- `natsclient/client.go:605` — `closeErr := m.drainAndCloseConnection(ctx, conn, drainTimeout)`
- `natsclient/client.go:692` — `closed := conn.StatusChanged(nats.CLOSED)`
- `natsclient/client.go:695` — `if err := conn.Drain(); err != nil {`
- `natsclient/client.go:711` — `case <-closed:`
- `natsclient/client.go:713` — `case <-drainTimer.C:`
- `natsclient/client.go:718` — `m.logger.Error("Drain timeout, force closing", slog.Duration("drain_timeout", drainTimeout))`
- `natsclient/client.go:720` — `conn.Close()`

Close and container termination get separate finite budgets; failed drain does not skip attempted termination.
Log has no independent container completion event. Raw NATS/SDK subscriptions are involved, not SemStreams Subscription.Drain.

## 4. Native ownership and prior measured limits

Dependency nats.go v1.52.0; SDK hashes match inherited inventory.

| Seam | Observation |
|---|---|
| jetstream/kv.go1433,1305 | ListKeysFiltered constructs WatchFiltered with OrderedConsumer |
| jetstream/kv.go1290,1299 | Callback sends entry/initial marker into updates without cancellation around send |
| jetstream/kv.go1439,1443,1451 | 256-key channel, forwarding defers watcher.Stop, unconditional key send |
| jetstream/kv.go1466,1210 | Explicit Stop delegates watcher.Stop then subscription Unsubscribe |
| nats.go5199–5201 | Unsubscribe can synchronously delete a library-created consumer |
| nats.go3580–3585 | Pending accounting/drain progress advance after callback return |

Retain four distinct deletion origins: framework Stop, forwarding cleanup, context completion unsubscribe,
asynchronous ordered recovery. Their request/response/completion observations must not be conflated.
Prior diagnostic1024keys/in-memory/controlled withholding observed prompt construction+Stop, typed nil-key expiry
and prompt Client.Close. Callback still present in immediate post-close snapshot; completion unproven. That fixture
used a diagnostic facade and drained native Keys during cleanup. It did not reproduce hosted15s drain failure.
Evidence: archived evidence/diagnostic/measurement-status.md and review/measurement-evidence-final.md.
CLOSED, Keys closure, child containment and callback joining are different observations. Current log resolves none.

## 5. Claims, history and contracts

- `openspec/specs/graph-index/spec.md:29` — `Predicate membership keys are **raw canonical predicates** (ADR-078), which superseded the`
- `openspec/specs/graph-index/spec.md:30` — `earlier hash-plus-catalog design; `PREDICATE_CATALOG` and its consistency/repair machinery were`
- `docs/adr/078-raw-canonical-predicate-membership-keys.md:35` — `PREDICATE_CATALOG is retired. Predicate identity is recoverable from the first three tokens, so production must not`
- `docs/adr/078-raw-canonical-predicate-membership-keys.md:36` — `create, repair, join, or read a catalog after cutover.`
- `docs/adr/078-raw-canonical-predicate-membership-keys.md:79` — `resource-leak gates. Hash-plus-catalog results from the companion run are comparative evidence only. They are not`
- `docs/operations/32-predicate-layout-smoke-harness.md:17` — `The executable source is `processor/graph-index/predicate_layout_smoke_integration_test.go`. Keep the proof in that`
- `docs/operations/32-predicate-layout-smoke-harness.md:47` — `- **Pre-tag predicate comparison — historical.** Still the `2.12.4-alpine` measurements; a truthful record of what`
- `docs/operations/32-predicate-layout-smoke-harness.md:48` — `was measured then, not claimed as current-pin evidence. Re-measure before citing a latency budget from it.`

Retired production candidate remains intentionally in decision harness. No deletion authority follows from no production caller.
History:5113036a2026-07-17 introduced;58893068July18/#558 consumer synchronization; c5b29a3bJuly18/#565 changed CI
budget10s/p958s/p999s citing disputed healthy-p952.65s; faa4f5b9September26/#1395 accepted already-deleted error
while retainingStop/Keysdrain/baseline; did not change failed measurement.
Root read#1286:open beta165/unclaimed/no comments/no openPR searchmatch. Existing budget/unit gap overlaps file,
but adjusting budget cannot repair KeysByFilter error. Universal dead-budget claim is unsupported for aggregate:
22 fresh5s children can sum over10s even if each succeeds under5s. No such over10s success measured here.
Runbook CI row3s/3s conflicts with source10s/8s/9s; historicalms remain explicit. Separate contract gap, not mechanism.
Graph-index spec preserves typed expiry/nil partial results; owner-load contract and historical candidate contract differ.

## 6. Category closure and adopter seam

1. Claimedgap: missing attribution/native completion, exact missing observations insection1.
2. Spellings: candidate join/framework construction+collection+Stop/native delivery+deletion/Close/termination/reporting.
3. Adjacentclaims:#1421,#1286,#1394/#1395,ADR078,spec,historical evidence and accepted diagnostic packet.
4. Consumeratbirth:no new symbol/config/bucket/API. Private candidate helpers serve smoke+KVconformance; production
   uses KVStore.
5. Problemshape: filtered snapshot collection then terminal ownership, evidence retained across failure. Closest
   measured existing instance is accepted common-KV diagnostic. Persistent owner-load observer covers another workload.

No new durable/communication/runtime-coordination primitive proposed; no establishing adoption sweep triggered.
Adopter seam: supply context+valid filter, handle wrapped errors, refuse partial snapshots. DefaultNewKVStore owns
5s child+collection+Stop; no native join handle. Failure:wrapped error, discardedStopresult, separatefixturedrainerror.
Adopters should not infer watcher completion from connection state or predict SDK timing; observed ownership gap,
not proposed publicAPI. No sister-contract or downstream migration change.

## 7. Open questions and read boundary

Unknown: constructionvscollection; repetition/priorchildcount; currentvsprior/probe delayed work; deleting owner;
blocked producers/subscription holdingdrain; callbacks alive afterforcedclose; actualserver/consumer/transportstate;
completion observablewithoutrelay/workloadchange/newAPI. Same observed symptom family/commonroute is not samecause.
No timeout/retry/deletion/newissue/repair/execution recommendation at this inventory checkpoint.

Search record (architect): contract/project; complete smoke+helper; spec layout+ownerproof; ADR078 full; runbook
profile/pin/comparison/interpretation; acceptedinventory/measurementstatus/finalevidencereview.

- git grep -n -E 'PredicateLayoutSmoke|hash-catalog-namespace-catalog-join|1286|predicate.layout|hash-catalog' -- processor/graph-index openspec docs .agents
- git grep -n -E 'predicate.layout|Predicate layout|Predicate Layout|hash.catalog|decision harness|Hash plus catalog' -- openspec/specs/graph-index/spec.md docs/adr docs/operations/32-predicate-layout-smoke-harness.md
- env GOFLAGS=-tags=integration gopls workspace_symbol -matcher=fuzzy hashPredicateCandidate
- env GOFLAGS=-tags=integration gopls references processor/graph-index/predicate_layout_smoke_integration_test.go:377:6
- env GOFLAGS=-tags=integration gopls references processor/graph-index/predicate_hash_candidate_test.go:19:6
- git log --format='%h %ad %s' --date=short -8 -- processor/graph-index/predicate_layout_smoke_integration_test.go
- git log --format='%h %ad %s' --date=short --all -S 'TestIntegration_PredicateLayoutSmoke' -- processor/graph-index/predicate_layout_smoke_integration_test.go
- git show --format=fuller --stat c5b29a3b
- git show --format=fuller --stat faa4f5b9
- git diff --stat origin/main...HEAD -- natsclient processor/graph-index/predicate_layout_smoke_integration_test.go
- SHA256 five repository sources/failedlog/fourSDKsources
- SDK rereads jetstream/kv.go1200–1215,1270–1315,1420–1470; nats.go3580–3625,5181–5210

Initial unquoted active-change glob failedzsh; quoted trackedsearchreplaced. No-hit timeoutsearch stopped chain;
hashes/diff rerun separately. No absence rests onfailedcommand. Deliberately excluded: repository-wide census,
downstreamsweep, otherCIlogdiagnosis, SDKexperiment/tests, speculativecause or rawlayout reevaluation.
