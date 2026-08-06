# R1 inventory checkpoint — read-only

Repository: `/private/tmp/semstreams-gs00`  
Branch: `codex/post-gs01-r1-acquisition`  
HEAD/main: `6ce137009fe6cf019dcb0a9a2a5122e81c2f9d27`  
Runtime baseline: `d1570ef81b23096021af0d7bf3321b4c08c7e54b`  
Worktree before materialization: clean; no runtime files changed.

Authority carried unchanged:

- Approved design SHA-256: `533b2010a4da24f55915fbebf046530dcb7ae7c8cdaa134eb6853a5ce36385fb`.
- Approved roadmap SHA-256: `0f16d7de739ea70c09312a897089ca01b79c28c9e43fbf0b78bf596bdc1504a2`.
- R0 is documentation-only; R1–R9 remain unimplemented:
  `docs/proposals/post-gs01-graph-read-derived-foundation-baton.md:35-41`.
- R1 authorization is inventory-only after R0 merge:
  `docs/proposals/post-gs01-graph-read-derived-foundation-roadmap-approval.md:43-49`.
- The ten-point identity packet remains controlling, including `ENTITY_STATES` authority, graph-ingest as sole physical
  writer, request/reply mutation, honest eventual consistency, no CQRS/recovery/general client/autostub, no
  compatibility path, and no generic shared runtime without evidence.

This is an inventory candidate, not `INVENTORY PASS`; an independent review is still required before target
materialization.

## Falsifiers first

The frozen R1 proof maps to current tests as follows:

| Frozen proof | Current evidence and first falsifier |
|---|---|
| Malformed A does not block valid workflow/entity B | Current test asserts the opposite: `pkg/lifecycle/watch_atomic_bootstrap_test.go:372-403`, `TestLifecycleSharedGuardPoisonOutsideWorkflowBlocksBufferedProjection`. This is the first red test seam for poison localization. |
| Touching malformed A returns typed reset-required and performs no mutation | Typed exact failure already exists in `pkg/lifecycle/graph_state_contract_test.go:14-38` and `pkg/lifecycle/manager_test.go:642-668`, but both also latch Manager-wide poison. A cross-entity mutation-count falsifier is absent. |
| Matching malformed watch emits no participant, diagnoses entity/revision, and affects only that subscription | Existing `TestLifecycleWatchLivePoisonClosesProjectionGate`, `pkg/lifecycle/watch_atomic_bootstrap_test.go:77-117`, proves global gate closure instead. No test presently verifies entity-plus-revision diagnostics or unrelated subscription continuity. |
| No lifecycle `WatchAll`, preflight, or Manager latch | Existing tests pin exactly one shared `WatchAll`: `pkg/lifecycle/watch_atomic_bootstrap_test.go:273-370`. A zero-`WatchAll` assertion would fail immediately. |
| Rule mismatch performs no second read/request | Runtime is already one-attempt. `pkg/projection/mutation_client_test.go:270-287` classifies mismatch but does not assert total request count; `processor/rule/actions_reconcile_test.go:228-258` only counts one high-level reconciler call. A stronger request-count test may already be green, so this slice cannot honestly claim a behavior-first failure for this subpart; the presently failing artifact is canonical spec truth. |
| Lifecycle retry revalidates phase, edge, audit chain, and mutator on every attempt | Code does so at `pkg/lifecycle/manager.go:623-702`, but no current test scripts a first mismatch followed by changed authority and proves every revalidation. The current fake has only an always-mismatch flag: `pkg/lifecycle/manager_test.go:24-62`. |
| Gated-DAG declares and owns its distinct prefix watch | Runtime already owns the watch at `processor/gated-dag/executor.go:355-386`, but `InputPorts` returns none at `processor/gated-dag/component.go:272-274`. A port-declaration assertion would fail. |
| Readers open through the catalog; only graph-ingest ensures | Existing catalog integration proves reader-open does not create and names graph-ingest: `graph/kvcatalog_integration_test.go:41-61`. Named derived readers still bind through raw JetStream, so acquisition source checks would fail. |

## Current surface inventory

### 1. Catalog acquisition and Ensure-versus-Open

The mechanism already exists:

- `graph/kvcatalog.go:37-68` declares `ENTITY_STATES` authoritative, owner `graph-ingest`, History 1.
- `graph/kvcatalog.go:120-158` contains the full catalog population.
- `graph/kvcatalog.go:174-200` derives owner-only write classification.
- `graph/kvcatalog.go:214-227` implements owner `EnsureCatalogBucket`.
- `graph/kvcatalog.go:229-241` implements reader `OpenCatalogBucket`.
- `natsclient/kvspec.go:224-293` shows Ensure creates/adopts/reconciles.
- `natsclient/kvspec.go:296-319` shows Open is must-exist, never creates, and returns classified not-ready naming the
  owner.
- Binding current spec: `openspec/specs/graph-retention/spec.md:200-223`, with reader-never-creates proof at `:233-241`.
- Current adopter documentation: `docs/operations/framework-bucket-catalog.md:1-10,14-42,104-109`.

Producer:

- Graph-ingest declares `ENTITY_STATES` as its KV-write output at `processor/graph-ingest/component.go:387-418`.
- It alone ensures the bucket at `processor/graph-ingest/component.go:1083-1095`.
- Physical authority write seams are in `processor/graph-ingest/canonical_mutations.go:238,301` and
  `processor/graph-ingest/component.go:1960,2107,2149,2286,2470`.
- Production search found no `ENTITY_STATES` physical writer outside graph-ingest, matching the accepted R0 audit at
  `docs/proposals/post-gs01-graph-state-reality-audit.md:20-47`.

Named R1 reader paths:

| Consumer | Declaration/lifetime | Current acquisition |
|---|---|---|
| graph-index | `kv-watch` input at `processor/graph-index/component.go:132-205`; Start at `:570-682`; owns authority watcher at `:848-940` | Raw availability probe and two raw handles at `:944-992`, specifically `:959,973,976`. |
| graph-index-spatial | `kv-watch` input at `processor/graph-index-spatial/component.go:105-150`; watcher lifetime at `:496-505` | Raw probe/open at `:404-439`, specifically `:425,439`. |
| graph-index-temporal | `kv-watch` input at `processor/graph-index-temporal/component.go:107-151`; watcher at `:517-526` | Raw probe/open at `:414-449`, specifically `:435,448`. |
| graph-embedding | `kv-watch` declaration at `processor/graph-embedding/component.go:183-215`; Start/watcher at `:619-730` | Raw probe/open at `:1191-1237`, specifically `:1210,1223`. |
| graph-clustering | Currently declares `kv-watch` at `processor/graph-clustering/component.go:448-505` | Generic raw helper probes/opens at `:1160-1186`; the same helper binds `ENTITY_STATES`, `OUTGOING_INDEX`, and `INCOMING_INDEX` from `:1124-1157`. Runtime then performs periodic timer-driven enumeration at `:1302-1347,1869-1879`; it does not own an authority watcher. |
| rule watch path | Configuration patterns at `processor/rule/config.go:37-50`; default input metadata at `:219-237`; zero patterns return without opening at `processor/rule/entity_watcher.go:18-51` | Raw probe/open at `processor/rule/entity_watcher.go:86-140`, specifically `:125,139`. |
| rule point read | Exact action helper | Already calls `OpenCatalogBucket` at `processor/rule/entity_watcher.go:1000-1020`. |
| lifecycle List/Watch/History | Lazy full handle stored for Manager lifetime at `pkg/lifecycle/manager.go:43-58`; List at `pkg/lifecycle/manager_query.go:25-95`; watches at `:134-205` | Generic `GetKeyValueBucket` at `pkg/lifecycle/manager.go:222-240`. Exact state operations separately use `graph.ExactEntityReader` at `:242-265`. |
| gated-DAG distinct prefix | Arbitrary canonical one-to-six-part prefix at `processor/gated-dag/config.go:69-79`; own goroutine/watch lifetime at `processor/gated-dag/executor.go:355-386` | Already `OpenCatalogBucket`, then watches `UnitEntityPrefix + ".>"` at `:359-366`. Component declares no input at `processor/gated-dag/component.go:272-274`. |
| message logger query | Always-registered routes at `service/message_logger_http.go:26-47`; arbitrary caller bucket in OpenAPI at `:148-196` | Package-local provider still exposes generic `GetKeyValueBucket` at `:361-369`; query binds at `:459-490`. |
| message logger watch | Per-SSE-client Watch or WatchAll at `service/message_logger_kv_watch.go:205-232` | Generic `GetKeyValueBucket` at `:195-203`. |

Existing adjacent exported read-declaration owner:

- `component.StoreReadPort` already declares read access with outward fields `Bucket` and `Interface` at
  `component/port_store.go:5-26`.
- Its serialized grammar is `store-read`, and its resource identity is `store-read:<bucket>` at
  `component/port_store.go:15-26`.
- It describes large content read from backend-neutral storage such as NATS ObjectStore or filesystem, not NATS KV
  current-state reads: `component/port_store.go:5-8`.
- Flowgraph assigns it `PatternStore` and connection ID `store-federation` at
  `component/flowgraph/flowgraph.go:235-238,313-316`.
- It participates in advisory federation fan-in from every `StoreProvidePort`, rather than bucket-specific KV producer
  matching: `component/flowgraph/flowgraph.go:598-637`.
- ADR-063 records that the exact storage instance is producer-chosen per fetch and that `store-read` is advisory,
  backend-neutral content federation: `docs/adr/063-store-substrate-and-resolver.md:377-386,398-405`.
- No current `StoreReadPort` consumer uses it to declare point, key enumeration, snapshot, poll, or Watch access to a
  NATS KV bucket.

`StoreReadPort` occupies adjacent exported read-declaration territory. Its resource identity, interaction pattern,
provider matching, and runtime substrate differ from the current KV declarations, while the ownership relationship
for clustering's snapshot/poll declaration is unbound.

Current non-R1/deferred authority readers named in the frozen eventual disposition remain present:

- `graph/query.Client`: `graph/query/client.go:161`.
- Agentic graph tool: `processor/agentic-tools/executors/register_graph_query.go:73`.
- Always-mounted `/graph/triples`: `service/graph_triples_http.go:178`.
- E2E storage helpers use generic acquisition at `test/e2e/client/nats.go:128,156,1798` and direct authority
  acquisition at `:219,242,392,437,984`.

Those three production deletions belong to R6, not R1:
`docs/proposals/post-gs01-graph-read-derived-foundation-roadmap.md:275-315`. Pulling them into R1 would be scope creep even
though the frozen whole-program design records their eventual disposition.

### 2. Lifecycle poison localization

Current Manager-wide mechanism:

- Sticky poison pointer and full guard state occupy `pkg/lifecycle/manager.go:64-87`.
- Guard-specific types are at `pkg/lifecycle/manager.go:90-102`.
- `NewManager` creates a background guard context at `pkg/lifecycle/manager.go:110-135`.
- Exact-read contract poison invokes `latchGraphStatePoison` at `pkg/lifecycle/manager.go:242-289`; one malformed touched
  entity therefore blocks every later Manager operation.
- List checks the global latch before workflow filtering at `pkg/lifecycle/manager_query.go:25-38`.
- `startWatch` checks the latch and starts the full guard at `pkg/lifecycle/manager_query.go:208-231`.
- `ensureGraphStateGuard` opens the Manager-wide `WatchAll` at `:234-253`.
- Guard bootstrap/validation is `runGraphStateGuard` at `:256-293`.
- Global revision barrier and degraded/latch machinery occupy `:295-383`.
- Every workflow watch waits on and exits with the global guard at `:385-446`.
- Matching watch decode poison calls the same global latch at `:454-479`.
- History also checks the global latch at `:516-543`.

Lifecycle has no production Prometheus metric declaration. Current poison logging at `pkg/lifecycle/manager.go:267-279`
records code and reason but not entity ID or KV revision.

`graphStateGuardCancel` has only two production occurrences—the field and constructor assignment at
`pkg/lifecycle/manager.go:77,127`. There is no production Manager close/stop path that invokes it; only tests cancel and
join it.

Current pinned behavior:

- Atomic all-graph bootstrap/global closure: `pkg/lifecycle/watch_atomic_bootstrap_test.go:31-164`.
- Exactly one shared `WatchAll`: `:273-370`.
- Poison outside the watched workflow closes that workflow: `:372-403`.
- Touched poison typed failure remains covered by `pkg/lifecycle/graph_state_contract_test.go:14-55`.

Adjacent same-class mechanisms that are not the R1 lifecycle deletion:

- Graph-ingest's owner-side full-authority validation is distinct and remains in
  `processor/graph-ingest/component.go:924-936,1145+`.
- Rule's pattern-scoped reset/degraded latches are rule-owned at `processor/rule/entity_watcher.go:54-79`; they are not
  lifecycle Manager state.
- Whole-authority derived owners retain their own poison/readiness observations; R1 authority only names the lifecycle
  Manager guard.

### 3. Rule one-attempt versus lifecycle full-intent retry

Rule runtime is already one attempt:

- `processor/rule/actions.go:1108-1153` makes one `reconciler.Reconcile`.
- `pkg/projection/mutation_client.go:176-200` makes one exact read followed by one reconcile request.
- `processor/rule/triple_mutator.go:67-90` makes one exact read and one direct reconcile for removal.
- No loop, retry knob, or generic CAS helper surrounds these paths.
- `pkg/projection/mutation_client_test.go:189-226` proves successful reconcile is exactly two wire requests: exact read
  plus mutation.
- `processor/rule/actions_reconcile_test.go:228-258` proves one high-level reconcile call.

Current binding-truth collision:

- `openspec/specs/rule-projection-mutations/spec.md:82-94` still requires one fresh read/recompute/retry after definite
  mismatch.
- `openspec/specs/projection-mutation-client/spec.md:60-73` requires one read/one mutation and no automatic retry.
- ADR-091 requires one exact read and one mutation request with visible mismatch and no automatic retry:
  `docs/adr/091-graph-mutation-authority-without-semantic-ownership.md:40-58,66-75,99-100`.
- Frozen design identifies the canonical rule spec as stale drift:
  `docs/proposals/post-gs01-graph-read-derived-foundation-design.md:1454-1468,1490-1505`.

Lifecycle retry is operation-specific:

- `updateRetries = 5` at `pkg/lifecycle/manager.go:568-573`.
- `TransitionWith` rereads current authority on every iteration at `:623-627`.
- It revalidates current phase, terminality, and permitted edge at `:628-647`.
- It decodes and validates the transition-record/audit chain at `:648-668`.
- It recomputes time, audit triples, and transition records at `:660-674`.
- It reprojects and reruns the optional mutator at `:675-702`.
- Only definite revision mismatch advances the loop at `:714-716`.
- `UpdateFromOperator` also retries definite CAS mismatch at `:778-834`, but it does not carry the
  transition/audit/mutator proof. The R1 "full intent" proof is specifically the transition path.
- Create/attach rereads after mismatch only to distinguish concurrent lifecycle attachment from unrelated contention
  at `:456-522`; it is not the same retry loop.
- Despawn is exact-revision fenced and does not automatically retry.

Binding lifecycle spec presently permits bounded component policy but does not spell out full transition-intent
revalidation: `openspec/specs/lifecycle/spec.md:77-87`.

## Same-class collision table

### Read-port declaration collision

| Dimension | Existing `StoreReadPort` | Current KV declarations/gap |
|---|---|---|
| Semantic class | Declares consumer read capability | Declares consumer access to state storage |
| Substrate | Backend-neutral content stores, including ObjectStore/filesystem | NATS KV current state and key enumeration |
| Serialized grammar | `store-read` | `kv-watch` and `kv-write`; no `kv-read` exists |
| Resource identity | `store-read:<bucket>` | `kvwatch:<bucket>` and `kvwrite:<bucket>`; no KV read identity exists |
| Flowgraph connection identity | `store-federation` on the consumer; rendered edges carry provider `store:<instance>` IDs | Bare bucket name for both KV watch and write |
| Interaction pattern | `PatternStore` | `PatternWatch` for both KV watch and write |
| Producer matching | Advisory fan-in from every `StoreProvidePort` | Bucket-specific KV-write → KV-watch matching |
| Runtime implication | Content ref resolution/federation visibility; exact instance chosen per fetch | Watch declaration presently implies reactive observation; clustering actually polls Keys/Get |
| Present consumers | Graph-embedding content federation and store-backed content readers | Clustering is the measured consumer whose declaration does not match runtime |
| No-cross-match evidence | Store ports use federation/provider IDs and `PatternStore` | KV ports use bare bucket connection IDs and `PatternWatch` |

No existing exported `KVReadPort`, `KVSnapshotPort`, `kv-read`, or `kv-snapshot` declaration exists. An empty-name search
alone is insufficient because `StoreReadPort` owns adjacent read-declaration territory.

### R1 runtime collisions

| Dimension | Catalog acquisition | Lifecycle poison/admission | CAS retry |
|---|---|---|---|
| Semantic job | Bind existing authoritative/derived KV storage versus provision owner storage | Decide whether malformed authority may influence lifecycle reads/actions | Decide whether an intent evaluated at revision R may be replayed at a newer revision |
| Owner | Catalog descriptor plus call-site role; graph-ingest owns `ENTITY_STATES` | `pkg/lifecycle.Manager` owns the global guard/latch | Each operation owner: rule/projection one-attempt; lifecycle transition owns bounded retry |
| Catalog/storage | One catalog, no new bucket | Reuses `ENTITY_STATES`; no poison bucket/status | No retry stream, ledger, bucket, or outbox |
| Status/error | Open returns `index_not_ready` naming owner; raw readers wrap varied startup errors | Global poison returns `graph_state_reset_required`; watcher transport loss returns `index_not_ready` | Definite `revision_mismatch` is not commit-unknown; ambiguous transport is not retried |
| Lifecycle | Owner Ensure during Start; readers Start/lazy/per-call; raw paths carry their own wait budgets | Background Manager-lifetime `WatchAll`, with no production close call | Rule request lifetime is one attempt; lifecycle transition loops at most five attempts |
| Ownership enforcement | Call-site selection only; no runtime caller identity: `graph/kvcatalog.go:21-24`, `natsclient/kvspec.go:11-12` | Manager-local atomic pointer and channels | Caller-specific code; no shared generic retry owner |
| Readers/consumers | Named derived components, lifecycle, rule, gated-DAG, operator diagnostics, deferred graph readers | Every lifecycle exact/List/Watch/WatchEvents/History operation and every consumer of those watches | Rule action authors and projection users; lifecycle callers invoking transition/update |
| Writers | Graph-ingest only for `ENTITY_STATES` | Lifecycle emits requests through graph-ingest; guard itself never writes | Graph-ingest executes CAS; callers only submit typed mutation requests |
| Recovery | Owner may reconcile bucket shape; readers wait/open and report not-ready | Current reset requires canonical reingest plus process restart | No recovery subsystem; definite conflict policy is operation-local; commit-unknown is never replayed automatically |

No second authority, mutation protocol, lifecycle status, retry service, graph-source injector, or general source
registry exists on these surfaces.

## Inherited R1 deletion claim mapped to exact current identifiers

This is the approved R1 inherited delete list, not a new target proposal.

Manager fields/types:

- `graphStatePoison`
- `graphStateGuardMu`
- `graphStateGuardStarted`
- `graphStateGuardCtx`
- `graphStateGuardCancel`
- `graphStateGuardReady`
- `graphStateGuardDone`
- `graphStateGuardReadyOnce`
- `graphStateGuardDoneOnce`
- `graphStateGuardResult` field
- `graphStateGuardDegraded`
- `graphStateGuardRevision`
- `graphStateProgressMu`
- `graphStateProgress`
- `graphStateGuardWG`
- `graphStatePoisonLatch` type
- `graphStateGuardResult` type
- `graphStateGuardTransportFailure` type

Current locations: `pkg/lifecycle/manager.go:64-102,110-135`.

Guard/latch functions and gates:

- `latchGraphStatePoison`
- `graphStateContractError`
- `ensureGraphStateGuard`
- `runGraphStateGuard`
- `advanceGraphStateGuardRevision`
- `waitGraphStateGuardRevision`
- `graphStateGuardNotReady`
- `markGraphStateGuardDegraded`
- `publishGraphStateGuardReady`
- `waitGraphStateGuard`
- calls in List, startWatch, runWatchLoop, prepareWatchEntry, and History

Current locations: `pkg/lifecycle/manager.go:246-289`;
`pkg/lifecycle/manager_query.go:25-38,208-383,385-479,516-543`.

Tests that pin the retired mechanism:

- `TestLifecycleWatchBootstrapIsAtomicAcrossPredicatePoisonOrdering`
- `TestLifecycleWatchLivePoisonClosesProjectionGate`
- `TestLifecycleWatchRevisionBarrierBlocksLaterValidBehindEarlierPoison`
- `TestLifecycleManagerUsesOneAuthoritativeGuardForManyWorkflowWatches`
- `TestLifecycleManagerConcurrentWatchesStillOpenOneAuthoritativeGuard`
- `TestLifecycleSharedGuardPoisonOutsideWorkflowBlocksBufferedProjection`
- `TestLifecycleWatchUnexpectedTransportCloseDoesNotLatchResetRequired`
- `TestLifecycleWatcherStartFailuresAreTransientIndexNotReady`
- `TestLifecycleWatchCloseAfterCancellationDoesNotReportPoison`
- `TestLifecycleSharedGuardNormalShutdownClosesWatchesWithoutPoison`
- `TestLifecycleSharedGuardTransportCloseIsTransientDegraded`

The exact test ranges in `pkg/lifecycle/watch_atomic_bootstrap_test.go` are `:31-164,166-200,202-243,245-271,273-403,
405-439,441-478`; additional guard-dependent fixtures are at `:507-509` and
`pkg/lifecycle/manager_test_helper_test.go:50-53`.

Generic/raw acquisition expressions within R1-owned surfaces:

- lifecycle `GetKeyValueBucket`: `pkg/lifecycle/manager.go:234`
- graph-index raw probes/opens: `processor/graph-index/component.go:959,973,976`
- spatial: `processor/graph-index-spatial/component.go:425,439`
- temporal: `processor/graph-index-temporal/component.go:435,448`
- embedding: `processor/graph-embedding/component.go:1210,1223`
- clustering generic helper: `processor/graph-clustering/component.go:1169,1182`
- rule watch helper: `processor/rule/entity_watcher.go:125,139`
- message-logger catalog query/watch bindings: `service/message_logger_http.go:365,484`;
  `service/message_logger_kv_watch.go:198`

Canonical stale rule requirement:

- "Rule reconcile has one bounded conflict retry" at `openspec/specs/rule-projection-mutations/spec.md:82-94`.

Explicitly not in the R1 delete list:

- graph-ingest owner validation
- derived-owner whole-authority watchers
- rule-local watcher latches
- `graph/query.Client`, agentic direct reader, and `/graph/triples`—reserved to R6
- E2E storage probes
- lifecycle's typed touched-entity reset-required result
- lifecycle transition's operation-specific retry loop

## Adopter seam inventory

Specific adopter: a developer outside this repository composing a SemStreams component without reading these
implementation files.

| Surface | What must they know today? | What happens if they do nothing? | Where can they discover it? | What should they have to know? |
|---|---|---|---|---|
| Authority acquisition | Bucket literal, whether they are owner or reader, raw JetStream acquisition, startup attempt/interval knobs, watcher lifetime | A reader may fail after a locally predicted wait budget; different sibling components expose different failure wrapping | Catalog operations doc, graph-retention spec, component config comments, implementation | Only that authority is a declared dependency and that absence is typed not-ready naming its owner; no bucket shape, handle, or boot-order prediction |
| Lifecycle exact/List/Watch | Any poison anywhere currently latches the Manager; workflow watches depend on a hidden global `WatchAll`; recovery requires reset/reingest/restart | An unrelated malformed entity can stop valid reads, transitions, lists, and watches for their workflow | Primarily `manager.go`, `manager_query.go`, and tests; `pkg/lifecycle/doc.go` does not explain the global guard | Only the state they touch and the typed outcome for that scope; no knowledge of unrelated authority contents or hidden admission scans |
| Rule reconcile | Canonical spec says one retry, runtime and ADR say no retry | Their action gets visible mismatch after one attempt despite the current canonical rule spec promising a second attempt | Conflicting OpenSpecs, ADR-091, and implementation | One stable rule semantic: an old `ExecutionContext` is not replayed merely because a newer revision exists |
| Gated-DAG unit watch | Runtime uses an undeclared, best-effort distinct prefix watch plus periodic correctness backstop | Composition metadata says there is no input dependency; authority-watch startup failure only increases dispatch latency | ADR-046 and executor comments; not current gated-DAG OpenSpec | The declared semantic dependency and its observable availability contract, not the raw bucket/watch mechanics |
| Message-logger KV diagnostic | Route accepts an arbitrary bucket; service is enabled by default; framework ships no default auth middleware | A network-exposed service-manager mux may expose arbitrary KV query/watch diagnostics unless the product supplies policy | `docs/operations/debugging-data-flow.md:23,77-80,127-142,188-193`; `service/middleware.go:17-23`; message-logger OpenAPI | Only an explicit operator diagnostic contract; not catalog ownership, raw KV handles, or an assumption that "dev/test" comments enforce authorization |

## Consumer-at-birth accounting

- `graph.EnsureCatalogBucket` and `graph.OpenCatalogBucket` are existing exported seams with current production
  consumers; R1 does not need a new exported acquisition primitive.
- `component.StoreReadPort` is an existing exported read-declaration seam, and graph-clustering is the measured
  consumer whose snapshot/poll declaration is unbound. The ownership relationship between those facts is not recorded
  in current code, specs, or ADRs.
- Each named reader has an immediate same-package consumer for a package-local narrowed read/list/watch interface. No
  present evidence names a consumer for a shared graph-source/provider abstraction.
- Gated-DAG's current distinct watcher is the immediate runtime consumer corresponding to its missing input
  declaration; flowgraph/composition is the current metadata consumer.
- Lifecycle's existing exported `Manager` methods are the adopter surface; no new lifecycle status consumer is named.
- No current lifecycle metric or alert consumer exists. Any new exported metric name/labels would therefore require
  consumer-at-birth evidence not present in the repository.
- Message-logger query/watch routes have present operator/E2E consumers, including
  `docs/operations/debugging-data-flow.md` and `test/e2e/scenarios/graph_roundtrip.go:174,576`.
- The current external rule consumer is the component/rule author relying on `rule-projection-mutations`; the
  contradictory spec is therefore adopter-visible, not internal documentation drift.

## Exact searches and measurements

Executed against clean HEAD:

- `rg -n --glob '*.go' --glob '!**/*_test.go' 'EnsureCatalogBucket|OpenCatalogBucket'`
  - 34 production text matches across all catalog buckets, including comments and declarations.
  - Call-shaped inspection yields 24 code lines: two declarations and 22 production call sites.
  - For `ENTITY_STATES`, one Ensure call exists: graph-ingest.
  - Seven production Open call sites exist across five consumer paths: graph/query contributes three; gated-DAG, rule
    point read, agentic tool, and `/graph/triples` contribute one each.
  - None of graph-index, spatial, temporal, embedding, clustering, lifecycle, rule watch acquisition, or message
    logger currently uses Open at its R1 acquisition site.
- Targeted `GetKeyValueBucket|.KeyValue(` search found the exact raw acquisition expressions listed in the delete
  inventory.
- `rg ... 'graphStateGuard|graphStatePoison' pkg/lifecycle` found 60 production references.
- `rg ... 'graphStateGuardCancel' pkg/lifecycle` found two production references and no invocation.
- `rg ... '(prometheus|metric|Counter|Histogram|Gauge)' pkg/lifecycle` found zero production lifecycle metric
  declarations.
- Production `BucketEntityStates` writer search found authority write seams only in graph-ingest, including
  `processor/graph-ingest/canonical_mutations.go:238,301` and
  `processor/graph-ingest/component.go:1960,2107,2149,2286,2470`.
- Retry search found no rule/projection revision-mismatch loop or generic CAS helper; lifecycle loops are confined to
  Manager operations.
- `rg -n '^func Test'` confirms current lifecycle shared-guard tests, current projection one-attempt tests, and the
  absence of a lifecycle changed-authority-on-second-attempt test.
- E2E raw storage acquisition remains concentrated in `test/e2e/client/nats.go`; behavioral scenarios also use
  message-logger diagnostics.
- `rg -n 'StoreReadPort|store-read' component docs/adr/063-store-substrate-and-resolver.md` found the existing exported
  content-read declaration, its parser/tests, PatternStore flowgraph ownership, and ADR-063 federation contract.
- `rg -n 'KVReadPort|KVSnapshotPort|kv-read|kv-snapshot' component processor` found no production KV read/snapshot
  declaration.

## Owning truth and documentation inventory

Already binding:

- Acquisition: `openspec/specs/graph-retention/spec.md:200-241`.
- Projection client one-attempt: `openspec/specs/projection-mutation-client/spec.md:60-73`.
- ADR-091 mutation boundary: `docs/adr/091-graph-mutation-authority-without-semantic-ownership.md:40-75,99-100`.
- ADR-090 authority/no-CQRS/no-recovery boundary:
  `docs/adr/090-authoritative-current-state-and-materialized-views.md:17-44,60-76`.
- Lifecycle authority/schema background: ADR-049, especially
  `docs/adr/049-lifecycle-harness-prime-schema-over-entity-states.md:126-175,323-348`.
- Gated-DAG distinct watch rationale: `docs/adr/046-parallel-fan-out-and-gated-dag-dispatch.md:389-405`.

R1 roadmap-named owning truth:

- `pkg/lifecycle/doc.go`
- `openspec/specs/lifecycle/spec.md`
- `openspec/specs/rule-projection-mutations/spec.md`

Current gaps:

- `pkg/lifecycle/doc.go:1-91` does not document Manager-wide poison/guard behavior and still points to ADR-047 rather
  than current ADR-049.
- `openspec/specs/lifecycle/spec.md` has no affected-scope poison/watch diagnostic requirement.
- `openspec/specs/rule-projection-mutations/spec.md:82-94` contradicts merged runtime and ADR-091.
- `openspec/specs/gated-dag-dispatch/spec.md:1-93` covers durable dispatch, heartbeat, and stalls, but is silent on the
  distinct `ENTITY_STATES` prefix watch and its input declaration.
- No R1 owning spec/doc is assigned for message-logger "operator-only" semantics, although current operator docs
  advertise unauthenticated curl examples and arbitrary bucket query/watch.

## Adjacent claims held out of R1

- R1 is not a graph-source injector, graphview adoption, new registry, general application bucket API, query front
  door, recovery subsystem, CQRS runtime, or new status surface.
- `pkg/graphview` remains an adjacent same-class primitive with an `AGENT_LOOPS` consumer and no production
  `ENTITY_STATES` consumer.
- `pkg/revlag` remains graph-index/embedding convergence machinery; acquisition normalization does not alter it.
- Clustering's periodic computation/readiness model remains distinct from reactive watcher owners.
- Gated-DAG's prefix watch is a low-latency nudge; its periodic reevaluation remains the current correctness floor at
  `processor/gated-dag/executor.go:115-153`.
- Exact lifecycle point reads remain through `graph.ExactEntityReader`; List/Watch/History are the direct catalog-open
  surface.
- Downstream repositories remain unmeasured holdouts for later parity validation and do not change R1 design
  authority.

## Open evidence questions requiring owner ruling

1. **Graph-index reservation collision.** R1 explicitly names the graph-index authority-open path at
   `docs/proposals/post-gs01-graph-read-derived-foundation-roadmap.md:94`, but shared-file reservations start
   graph-index component/query/watermark at R3, not R1:
   `docs/proposals/post-gs01-graph-read-derived-foundation-roadmap.md:416-417`. Both cannot govern the same file without
   a binding clarification.

2. **Gated-DAG owning truth omission.** R1 changes an adopter-visible input declaration, but the roadmap's owning-truth
   list omits both `openspec/specs/gated-dag-dispatch/spec.md` and `framework-composition/spec.md`. Current gated-DAG
   spec does not describe the watch.

3. **Gated-DAG availability semantics.** The current distinct watch is best-effort and falls back to periodic
   reevaluation: `processor/gated-dag/executor.go:135-153`. The frozen R1 wording says the component "declares and
   owns" the watch but does not rule whether declared dependency failure remains degraded/best-effort or becomes a
   Start failure.

4. **Clustering declaration vocabulary.** Frozen authority says clustering declares snapshot/poll consumption, but
   the component port model has `kv-watch` and no KV snapshot/read port type; clustering currently declares `kv-watch`
   while polling. No binding representation is specified.

5. **Message-logger "operator-only."** Current framework ships zero default middleware, the service is enabled by
   default, routes are always registered, and bucket names are arbitrary. "Operator-only" does not currently identify
   whether the binding fact is authorization middleware, deployment enablement, catalog allowlisting, route
   classification, or documentation. Restricting the entire generic diagnostic endpoint to catalog buckets would
   also remove current off-catalog application/product diagnostics.

6. **Message-logger owning truth.** No R1 spec/doc owner is assigned despite an observable HTTP/OpenAPI behavior
   change. `docs/operations/debugging-data-flow.md` currently teaches the generic route.

7. **Bounded logs/metrics.** Lifecycle has no current metric surface or named metric consumer. The approved wording
   requires bounded logs/metrics naming entity and revision, while entity IDs are unsuitable as unbounded metric
   labels. The exact binding division between structured log fields and bounded metric dimensions is not recorded.

8. **TDD accounting for already-correct retry runtime.** Rule runtime already implements the approved one-attempt
   behavior. A stronger mismatch request-count test may begin green; only the canonical spec is presently wrong. The
   slice evidence must not falsely report a behavior-first red test.

9. **Existing read-port owner collision.** `component.StoreReadPort` already owns an exported read-declaration surface
   for backend-neutral content federation. Clustering performs bucket-specific KV state enumeration while declaring
   `KVWatchPort`. Current source and accepted records do not bind the ownership relationship between these adjacent
   facts.

## R1 validity after merged R0

R1 remains semantically valid after R0: R0 changed no runtime, and the current code still exhibits every acquisition,
poison, and retry collision recorded by the approved design.

The execution record is not yet contradiction-free:

- the graph-index shared-file reservation conflicts with R1's named primary surface;
- gated-DAG and message-logger lack assigned owning truth for their outward changes; and
- "operator-only," clustering's snapshot/poll declaration, gated-DAG failure posture, and diagnostic metric shape
  remain unbound; and
- clustering's declaration gap overlaps the existing exported `StoreReadPort` owner's semantic territory, and that
  ownership relationship remains unbound.

Therefore no R0-induced semantic target amendment is evidenced, but the R1 roadmap/materialization needs owner
rulings—and at least a reservation/ownership record clarification—before it can be declared implementation-ready.
