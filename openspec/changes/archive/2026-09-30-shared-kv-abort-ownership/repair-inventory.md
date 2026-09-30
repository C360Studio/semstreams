# Shared KV abort ownership: repair-seam inventory

base: e479605a55095027aee4b1ce87e5b70af7cf6c18

Worktree: /Users/coby/.codex/worktrees/gh1397-runner-tests/semstreams
Scope: observed NewKVStore.KeysByFilter abort boundary, public aliases, minimal-reader compatibility and existing
native watcher completion precedent. Architect gh1433_inventory_design enumerated; root materialized. Inventory only:
no tests, native runs, source edits or Git mutations.

## Evidence identity and inherited scope

Inherit this change's inventory.md (original /tmp/gh1421-predicate-smoke-inventory.md), SHA
cc9be085be8b4295e2ef57b61418b43db0425b6c6ce4b3c7e914629c3d1b4973. Its hosted failure, fixture, decision-harness
boundary, SDK deletion paths and #1286 qualification remain applicable. Accepted native evidence: review/native-evidence.md.
Paths below are relative to openspec/changes/shared-kv-abort-ownership where applicable.

| Source | SHA-256 |
|---|---|
| evidence/implementation/ordinary-source.go.txt | 0245261bac9cb45e782cbac484c2e045274191fa652abdb6f12b7ebda089d569 |
| evidence/implementation/tagged-source.go.txt | 98b615957d99bbe0014da0cb6979a6af896ff8191363767c1b54e7eaae98f0eb |
| /tmp/gh1421-abort-single-native.log | 5a8c4b3892f742995cc1eca2ca992107688039078475e9fc16884981d7096084 |
| natsclient/kv.go | 9b9b4c40aa6f65b66bdcb68a7db881e0fdfb4bb8d46f8b2af4d42d86d1696725 |
| natsclient/kv_filter_test.go | 16d484a72cf09f2414e403af11da8677381bd47fc9bc606ac61e719a8bcaf311 |
| graph/kvcatalog.go | 7ce9b4012cc5a0ebb7639cfa84afd5d1d0870dc6ed88f00b7f42341db115ddb8 |
| processor/graph-ingest/component.go | 787e7e862bd63d3f783b9c868012dfe49154ffe2238bdb235800b75e11416bb1 |

The compiled diagnostic files were removed after verified preservation. Retained copies are the source of record.

## Claimed gap and limits

Control returned exactly 5,000 keys. While constructor return was held, native forwarder40 and watcher51 were
positively blocked on native sends before the actual five-second child expired. Production returned nil keys and a
typed deadline error; delegated Stop ran once and promptly returned invalid-subscription. Watcher51 remained in
chan-send after return and after Client.Close returned nil/CLOSED (about13ms). This establishes an observed ownership
defect at those terminal observations, not indefinite survival, the hosted15s drain failure or historical causation.
Forwarder absence is not a join. Diagnostic PASS is defective evidence of cleanup success:

- `openspec/changes/shared-kv-abort-ownership/evidence/implementation/tagged-source.go.txt:185` — `forward := abortSenderState(stacks, true, pair.forwardID, "jetstream/kv.go:1451")`
- `openspec/changes/shared-kv-abort-ownership/evidence/implementation/tagged-source.go.txt:186` — `watcher := abortSenderState(stacks, true, pair.watcherID, "jetstream/kv.go:1290")`
- `openspec/changes/shared-kv-abort-ownership/evidence/implementation/tagged-source.go.txt:188` — `return nil`
- `openspec/changes/shared-kv-abort-ownership/evidence/implementation/tagged-source.go.txt:320` — `if err := abortObservePair(r, "after_close", pair); err != nil {`

The helper logs the blocked sender without failing. Classifier mutation proves classification sensitivity only.

## Current spellings and public boundaries

- `natsclient/kv.go:48` — `bucket  jetstream.KeyValue`
- `natsclient/kv.go:39` — `Timeout:               5 * time.Second,`
- `natsclient/kv.go:70` — `return context.WithTimeout(ctx, kv.options.Timeout)`
- `natsclient/kv.go:525` — `return kv.KeysByFilter(ctx, prefix+">")`
- `natsclient/kv.go:541` — `lister, err := kv.bucket.ListKeysFiltered(ctx, pattern)`
- `natsclient/kv.go:550` — `keys, err := collectFilteredKeys(ctx, lister)`
- `natsclient/kv.go:561` — `ListKeysFiltered(ctx context.Context, filters ...string) (jetstream.KeyLister, error)`
- `natsclient/kv.go:563` — `lister, err := kv.ListKeysFiltered(ctx, pattern)`
- `natsclient/kv.go:583` — `defer func() { _ = lister.Stop() }()`
- `natsclient/kv.go:589` — `return nil, ctx.Err()`
- `natsclient/kv.go:592` — `if err := ctx.Err(); err != nil {`
- `natsclient/kv.go:507` — `keys, err := kv.bucket.Keys(ctx)`
- `natsclient/kv.go:605` — `watcher, err := kv.bucket.Watch(ctx, pattern)`

NewKVStore has the full native bucket. Positive timeout derives from caller; nonpositive timeout returns caller at72.
FilteredKeys requires only ListKeysFiltered and adds no framework timeout. Current collector ignores Stop error;
closed channel under expired context still discards partial keys. Unfiltered Keys and long-lived Watch are separate;
Watch deliberately has no applyTimeout. KeysByFilter treats direct ErrNoKeysFound as empty; FilteredKeys uses errors.Is
for ErrNoKeysFound and ErrKeyNotFound. Both wrap other errors with %w. Neither promises ordering or deduplication.

## Production consumers and compatibility

Typed references distinguish actual consumers from similarly named mocks:

- `processor/graph-index/owner_reconcile.go:47` — `existingKeys, err := bucket.KeysByFilter(ctx, ownerFilter)`
- `processor/graph-index/component.go:2097` — `set.keys, listErr = set.bucket.KeysByPrefix(ctx, set.filter)`
- `processor/graph-index/component.go:2099` — `set.keys, listErr = set.bucket.KeysByFilter(ctx, set.filter)`
- `processor/graph-index/query.go:293` — `keys, err := c.incomingBucket.KeysByPrefix(ctx, prefix)`
- `processor/graph-index/query.go:428` — `keys, err := c.predicateBucket.KeysByFilter(ctx, filter)`
- `processor/graph-index/query.go:567` — `keys, err := c.predicateBucket.KeysByFilter(ctx, filter)`
- `processor/graph-index/name_index.go:292` — `keys, err := c.nameBucket.KeysByPrefix(ctx, prefix)`
- `processor/graph-ingest/component.go:462` — `return a.component.entityBucket.KeysByPrefix(ctx, prefix+".")`
- `processor/graph-ingest/query.go:294` — `keys, err := c.entityBucket.KeysByPrefix(ctx, prefixDot)`
- `graph/clustering/storage.go:244` — `keys, err := natsclient.FilteredKeys(ctx, s.kv, pattern)`
- `graph/inference/storage.go:312` — `keys, err := natsclient.FilteredKeys(ctx, s.kv, prefix+">")`
- `graph/inference/storage.go:560` — `keys, err := natsclient.FilteredKeys(ctx, s.kv, prefix+">")`
- `processor/agentic-loop/trajectory_reader.go:80` — `keys, err := natsclient.FilteredKeys(ctx, r.bucket, prefix+">")`
- `processor/agentic-tools/executors/register_graph_query.go:111` — `keys, err := natsclient.FilteredKeys(ctx, reader, pattern)`
- `processor/graph-clustering/anomaly.go:123` — `keys, err := natsclient.FilteredKeys(ctx, q.incomingBucket, entityID+".>")`
- `processor/graph-clustering/component.go:2189` — `keys, err := natsclient.FilteredKeys(ctx, p.incomingBucket, entityID+".>")`
- `processor/graph-clustering/query.go:321` — `keys, err := natsclient.FilteredKeys(ctx, c.communityBucket, pattern)`
- `processor/graph-index-temporal/query.go:75` — `matched, err := natsclient.FilteredKeys(ctx, c.temporalBucket, prefix)`
- `test/e2e/client/nats.go:947` — `keys, err := natsclient.FilteredKeys(ctx, bucket, targetEntityID+".>")`

Reconciliation and deletion preflight list before writes/deletes; name query separately deduplicates. Agentic-tools
caller derives default timeout at108. Temporal query aggregates multiple listings under caller context.

- `graph/kvcatalog.go:278` — `ListKeysFiltered(ctx context.Context, filters ...string) (jetstream.KeyLister, error)`
- `processor/graph-clustering/component.go:550` — `ListKeysFiltered(context.Context, ...string) (jetstream.KeyLister, error)`
- `processor/graph-clustering/reader_capabilities_test.go:34` — `_ incomingBucketReader = minimalIncomingBucketReader{}`
- `processor/agentic-loop/trajectory_recorder.go:71` — `ListKeysFiltered(context.Context, ...string) (jetstream.KeyLister, error)`
- `natsclient/kv_filter_test.go:76` — `func TestFilteredKeysAcceptsMinimalReader(t *testing.T) {`
- `processor/graph-index/owner_filter_load_observer_test.go:243` — `func (b *ownerLoadObservedBucket) ListKeysFiltered(ctx context.Context, filters ...string) (jetstream.KeyLister, error) {`
- `processor/graph-index/owner_filter_load_observer_test.go:383` — `func (b *ownerLoadFakeBucket) ListKeysFiltered(ctx context.Context, filters ...string) (jetstream.KeyLister, error) {`

CatalogReader has Watch/WatchAll but not WatchFiltered; private facade286 embeds only that interface. incomingBucketReader
is minimal and compile-tested. Trajectory bucket otherwise needs Create/Get. Requiring WatchFiltered would change
explicitly tested minimal-reader capability. A changed constructor path would bypass the persistent owner-load observer
unless reconciled. Embedded broad interfaces do not make unimplemented methods functional.

Additional typed minimal-reader ledger: graph/embedding/memkv_test.go156, pkg/lifecycle/manager_test.go288, trajectory
observability/recorder fakes, graph-clustering component/edge-weight/query-failure fakes, graph-embedding, graph-index-
spatial/temporal/index, graph-ingest and rule mocks. Eight component/memKV WatchFiltered methods return not-implemented;
pkg/lifecycle/manager_test.go268 panics. Broad interface satisfaction is not behavioral compatibility.

## Native completion and closest existing shape

SDK paths are relative to /Users/coby/go/pkg/mod/github.com/nats-io/nats.go@v1.52.0; immutable hashes inherited.

- jetstream/kv.go1433: native ListKeysFiltered calls WatchFiltered(ctx, filters, IgnoreDeletes(), MetaOnly()).
- jetstream/kv.go1290: callback sends w.updates <- entry while holding w.mu from1278; nil-marker1299 also unconditional.
- jetstream/kv.go1451: forwarder sends kl.keys <- entry.Key() unconditionally.
- jetstream/kv.go1452: cancellation can end forwarder independently of draining Updates.
- jetstream/kv.go1442: defer close(kl.keys) signals forwarder end, not watcher callback completion.
- jetstream/kv.go1466/1210: KeyLister.Stop delegates watcher.Stop then sub.Unsubscribe.
- jetstream/kv.go1338: close(w.updates); closed handler1335–1339 takes watcher mutex at1336.
- nats.go3631: mcb(m) synchronously delivers callback in loop.
- nats.go3658: done(s.Subject) invokes closed handler after delivery loop exits.

Updates closure is consequently a stronger delivery-completion observation than Keys closure; no public goroutine join.
Existing shape: stop a finite snapshot watcher, then consume Updates through closure:

- `processor/graph-ingest/component.go:1280` — `c.stopEntityStateGuardWatcher(watcher, updates)`
- `processor/graph-ingest/component.go:1297` — `c.stopEntityStateGuardWatcher(watcher, updates)`
- `processor/graph-ingest/component.go:1315` — `_ = watcher.Stop()`
- `processor/graph-ingest/component.go:1317` — `for range updates {`
- `openspec/specs/graph-ingest/spec.md:460` — `then MUST continue consuming the watcher's update channel until it closes, discarding`
- `processor/graph-ingest/query_contract_guard_test.go:201` — `// TestEntityStateGuardDeliberateStopDrainsToCloseWithoutMisclassification:`

The same callback/mutex hazard is recognized there. Stop errors are ignored and drain is unbounded; fake watcher test
does not establish bounded behavior for uncooperative native owner. This is precedent, not approved code to copy.

## Adjacent contracts and ownership collisions

- `openspec/specs/graph-index/spec.md:231` — `- **THEN** the operation returns a context-deadline error and the guard fails on that error`
- `openspec/specs/graph-index/spec.md:233` — `- **AND** no partial key set is accepted as a successful owner snapshot`
- `openspec/specs/graph-index/spec.md:204` — `every per-store baseline, released temporary subscriptions, zero slow consumers, and the server resident-set bound.`
- `openspec/specs/nats-kv-keys/spec.md:195` — `Purge, Keys/list, KeysByPrefix, KeysByFilter, FilteredKeys, Watch, or direct raw-bucket paths. Existing accepted bytes,`

Typed failure/no partial success is explicit. Graph-index CI also checks resource baselines. Searched current specs
lack a general public filtered-listing native callback completion boundary and finite cleanup allowance. Ingest's
requirement is narrower. Current grammar, accepted bytes, native filtering, default5s and conformance remain contracts.

| Dimension | Existing owners and limits |
|---|---|
| Semantic class/owners | KVStore owns timeout/result; collector consumes/requestsStop; SDK owns forwarding, callback, subscription/consumer. |
| Catalogs | CatalogReader deliberately narrows native capability; no new catalog/config primitive proposed. |
| Status | Public typed error; KeyLister exposes Keys/Stop only. CLOSED did not prove callback completion. |
| Lifecycle | Forwarder cancellation, watcherStop, Updates closure and Client.Close differ. |
| Ownership | Caller supplies context; KVStore derives local timeout; SDK creates tasks for operation; minimal readers own implementations. |
| Readers | Production ledger, observers, smoke, conformance/cancellation proofs and sister prefix callers below. |
| Writers | No key mutation; SDK creates/deletes temporary machinery. Reconciliation/deletion follow successful listing. |
| Recovery | No listing retry/resume/partial recovery. Ingest owns separate boot/watch-loss behavior. |

- `natsclient/kv.go:47` — `type KVStore struct {`

Struct holds bucket/options/logger only. Scoped context search found parameters/returns, no Background/TODO/
WithoutCancel roots, stored context, provider closure or exported CancelFunc in this file. Canonical context-ownership
contract constrains lifecycle work. No new exported symbol, port, subject, bucket or config proposed; existing consumers
are above. No new reusable pattern established during inventory, so no establishing adoption sweep triggered.

## Existing proof and limits

Typed references cover ordinary, integration and live_llm configurations. Existing cancel tests: natsclient/
kv_filter_test.go52,65 and processor/graph-index/owner_filter_integration_test.go145,178; typed failure/nilkeys/Stop,
not completion of backpressured native delivery. Conformance: natsclient/kv_key_contract_integration_test.go
352,385,390,395,400,688,692,696,700,711; prefix natsclient/kv_integration_test.go487,497,506,513,520.
Other graph-index caller references: attack292; index_hardening187,215,283,291,297; integration29; owner_filter139,
145,166,178; owner_filter_load324,429,506,694,720,725,732,755,813; owner_filter_load_observer413,457,477,535;
predicate_layout_smoke347,350,353,360,370,373,380,387,435,456,640,648. These identify callers, not cancellation proof.
Accepted experiment is a real-path counterexample, not a failing end-to-end regression assertion.

## Adopter seam

Persona: component/sister developer using NewKVStore, KeysByPrefix or FilteredKeys without native watcher knowledge.
They supply context/filter and recognize typed cancellation/nilkeys; wrapper default timeout differs from raw helper's
caller context. Hidden callback cleanup cannot be discharged through these APIs' exposed capabilities. Do-nothing
path returns honest deadline error but observed callback survives that return. Longer caller timeout does not change
wrapper default5s. Minimal readers lack Updates handle. Capability failures are compile-time; cancellation typed;
blocked callback is absent from public result/completion handle and needed stacks. Correct keys/Close are insufficient.
Adopters should rely on stated operation/result contract without predicting buffers or managing inaccessible watcher.
This is an unexpressed/unenforced terminal obligation, not a proposed adopter knob.

Read-only local sister census: semdragon HEAD07f4de9 graphclient.go290 calls store.KeysByPrefix and propagateserror;
semspec HEAD5a9496ee agentgraph/graph.go77 declares KeysByPrefix; semspec-ui-bmad HEADc8308d7e and
semspec-ui-run-visibility HEADe30cbf78 processor/execution-manager/execution_store.go150,185 call it and return
accumulated/cache results on listing error. No sister mutation authorized or performed.
No matching spelling in local semboids, semconnect, semdev, semdev-test, semdev-test-sub, semdocs, semembed,
seminstruct, semlink, semmachina, semmem, semops, semsage, semsource, semstreams-ui or semteams. semmem-test
HEAD unresolved/unverified. Literal search does not prove absence across all external repos or aliases.

## Search record and boundaries

Structural: gopls references natsclient/kv.go537:20,522:20,560:6,582:6,503:20; implementation natsclient/kv.go561:2;
workspace_symbol -matcher=fuzzy WatchFiltered; references to pinned SDK WatchFiltered interface and
graph-ingest/component.go1314:21. Included integration; live_llm added no production filtered-call path. Authorized
read-only cache access enabled package loading. Tracked git grep covered WatchFiltered, IgnoreDeletes, MetaOnly,
stop/drain/listing lifecycle. git log -S stopEntityStateGuardWatcher identifies#572; -S collectFilteredKeys identifies
canonical predicate-contract origin. Exact scoped searches:

- git grep -n -E 'context\.(Background|TODO|WithoutCancel)|context\.Context|context\.CancelFunc' -- natsclient/kv.go
- git grep -n -E 'filtered.*(join|Stop|drain)|listing.*(join|Stop|drain)|Stop.*drain|consuming the watcher' -- openspec/specs/nats-kv-keys/spec.md openspec/specs/graph-index/spec.md openspec/specs/graph-ingest/spec.md
- git grep -n -E 'FilteredKeys\(|KeysByFilter\(|KeysByPrefix\(' -- '*.go' in named sibling checkouts with recordedHEADs

Only ingest consume-to-close matched normative search. No unrelated KV method, repo-wide async census, timeout
adjustment, upstream-fix absence claim or redesign included. Unfiltered Keys/directListKeys are adjacent distinct
paths, not claimed defective or repaired by this experiment.

## Open evidence and owner questions

1. What public terminal obligation and finite cleanup allowance govern canceled filtered listings? Existing contracts
   establish typed cancellation/no partial success, not a complete terminal boundary.
2. Can full-capability NewKVStore observe Updates closure while preserving minimal-reader compatibility and observer
   coverage? Capabilities are enumerated; target not selected.
3. Updates closure is linked to completed delivery callbacks; explicit native goroutine-join handle is absent.
4. Permanent regression must fail through the actual abort path when the terminal obligation is violated.

Stop for independent INVENTORY review; no repair recommendation at this phase.
