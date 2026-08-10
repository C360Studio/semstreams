# Post-Foundation-B graph-foundation remap inventory

## Scope and evidence boundary

This is an inventory only. It makes no target-state, sequencing, priority, or binding policy ruling.

- Repository inspected: `/private/tmp/semstreams-gs00-recovered`
- Exact merged baseline: `967b75b6ebcb0f1b0eee9157e76c39da982aa640`
- `origin/main`: `967b75b6ebcb0f1b0eee9157e76c39da982aa640`
- Downstream repositories were not exhaustively audited. The owner-approved scope is communicate-only: downstream
  teams own tag adoption, compilation, migration fixes, flow validation, and product E2E. Archived quick examples are
  non-exhaustive and non-blocking.
- Evidence classes are only `current`, `stale`, `overlapping`, `deferred-by-ruling`, and `unverified`.
- The sole active OpenSpec change, `semantic-tier-split`, is suspended by ADR-090. It is an unimplemented draft, not
  current framework truth.
- The adjacent `.sha256` sidecar records this exact inventory checkpoint before independent review.

## Surface inventory

### Foundation checkpoint reconciliation

The old checkpoint at `docs/proposals/post-r1c-foundation-remap-roadmap.md:367-593` proposed replacing
`InputPorts()`/`OutputPorts()` with `Ports() PortConfig`. That shape did not become the accepted implementation.

The later owner-accepted declaration-generation design retained both `Discoverable` methods and made Registry
admission their only production consumer:

- The exported interface remains `InputPorts() []Port` and `OutputPorts() []Port`: `component/discovery.go:18-26`.
- Registry captures each lane once per successful generation: `component/registry.go:104-143,281-366,841-888`.
- Registry retains component, factory identity, resolved ports, normalized facts, exclusive resources, and local
  generation as one immutable record: `component/registry.go:104-143`.
- Snapshot reads and observation are internal/root-gated, defensive, complete-set, latest-state, and process-local:
  `component/registry.go:919-1077`.
- Flowgraph reads Registry snapshots: `component/flowgraph/flowgraph.go:133`.
- ComponentManager reads Registry snapshots: `service/component_manager.go:1664,2394`.
- Message logger reads initial and observed Registry sets: `service/message_logger.go:336-338,572`.
- The only production calls to component port methods are Registry capture: `component/registry.go:844,848`.

Current declaration counts:

| Census | Count | Meaning |
|---|---:|---|
| Runtime roots | 68 | 34 executable components × two methods under component/gateway/input/output/processor/storage, excluding doc/helper/tests |
| Executable repo-wide | 76 | Runtime 68 plus six example and two e2e mission methods |
| Raw repo-wide | 79 | Executable 76 plus two mock-helper methods and one documentation example |

The prior 68 and raw 79 figures describe different scopes; neither shows declaration loss.

### Configuration catalogs and effective snapshots

There are distinct catalogs and lifetimes, not one declaration authority:

1. `PortConfig` in shipped JSON is preconstruction provisioning intent.
2. Component instances retain resolved effective ports.
3. Registry captures one admitted runtime generation.
4. Stream planning consumes configuration-time declarations before components exist.
5. Message logger consumes Registry snapshots after admission.
6. Service configuration has durable desired next-boot state and an immutable running composition.

The retained 21-config census is frozen at `service/testdata/message_logger_subject_census.json:1-74` and enforced
through production loaders/factories by `service/message_logger_census_test.go:68-117`.

| Measure | Raw configured | Effective admitted | Delta |
|---|---:|---:|---:|
| Subject rows | 385 | 561 | +176 |
| Per-config exact keys | 243 | 378 | +135 |
| Global distinct strings | 51 | 66 | +15 |
| Removals | — | — | 0 |

#### Agentic-tools instances and graph-query admission

The frozen 21-config scope contains nine enabled `agentic-tools` instances, not three. The other twelve configs have
no `agentic-tools` instance.

| Config | Effective allowlist |
|---|---|
| `configs/agentic.json` | `query_entity` |
| `configs/examples/research-graph-pipeline.json` | `research_graph`, `read_loop_result`, `decide` |
| `configs/flows/crud-tools-test.json` | `create_rule`, `update_rule`, `delete_rule`, `list_rules`, `get_rule` |
| `configs/flows/deep-research-test.json` | `query_entity`, `query_entities`, `graph_query`, `read_loop_result`, `decide`, `submit_work` |
| `configs/flows/deep-research.json` | previous list plus `web_search`, `http_request` |
| `configs/flows/lesson-example.json` | `emit_lesson` |
| `configs/flows/ops-agent-test.json` | query/read/monitor/list/diagnosis/lesson tools plus `submit_work` |
| `configs/flows/ops-agent.json` | same as `ops-agent-test` |
| `configs/research-graph-e2e.json` | `research_graph`, `read_loop_result`, `decide` |

The twelve without an instance are cloud-federation, e2e-structural, edge-federation, graph-backend, hello-world,
lifecycle-flow, protocol-flow, semantic-8b, semantic-frontier, semantic, statistical, and structural.

`search_graph` and `summarize_graph` are registered built-ins, but none of the nine shipped allowlists admits either
tool. Shipped agentic-tools therefore contributes zero raw and zero effective `graph.query.searchGraph` or
`graph.query.summary` output rows. The `graph_query` allowlist entry in the two deep-research configs names a
registration group that exposes five differently named tools; it admits neither gateway-first tool
(`processor/agentic-tools/executors/register.go:193,201-206`;
`processor/agentic-tools/executors/register_graph_query.go:14-18`).

Additional retained facts:

- 41 exact collapses: 40 loop/dispatch and one governance.
- Three accepted `configs/agentic.json` containment overlaps for proposed/approved/rejected tool-call subjects.
- 61 default-only JetStream outputs: 45 agentic-loop and 16 agentic-dispatch.
- All 61 are explicitly covered by `AGENT` / `agent.>`; zero are uncovered.
- Four enabled configs without production factories were retired: `configs/http-gateway-semantic-search.json`,
  `configs/semantic-basic.json`, `configs/examples/bm25-semantic-search.json`, and
  `configs/examples/pathrag-graph-traversal.json`.
- `configs/hello-world.json:196-225` now explicitly declares `ALIAS_INDEX`.
- Message logger has zero production raw `PortConfig`/component-config interpreters.
- Archived D.5 searches find zero identity-free Registry admission, ComponentManager resource tracker/re-reads,
  dynamic service mutation, retired inner knobs, shims, durable declaration stores, or Registry readiness/cohort fields.

### Graph KV catalog

`graph.KVCatalog()` is the single literal table for framework buckets whose ownership or retention is guaranteed:
`graph/kvcatalog.go:1-138`. It contains 18 descriptors:

| Semantic class | Buckets | Declared writer |
|---|---|---|
| Authoritative | `ENTITY_STATES` | graph-ingest |
| Derived lookup | `ENTITY_SUFFIX_INDEX` | graph-ingest |
| Operational dedup | `GRAPH_INGEST_APPLIED_SEQ` | graph-ingest |
| Derived graph index | outgoing, incoming, alias, predicate, name | graph-index |
| Derived spatial/temporal | spatial, temporal, temporal reverse | spatial/temporal index owners |
| Derived semantic | embedding, embedding dedup | graph-embedding |
| Derived clustering | community, summaries, anomaly | graph-clustering |
| Operational readiness | `GRAPH_STATUS` | graph-index, embedding, ingest, rule |
| Operational inventory | `STORAGE_REPORT` | storage inventory collector |

All rows are owner-create/read-must-exist and owner-only-write. `ENTITY_STATES` has history 1, `GRAPH_STATUS` history
3, and `STORAGE_REPORT` history 10 (`graph/kvcatalog.go:35-103`). Owner acquisition is `EnsureCatalogBucket`
(`:194-207`); reader acquisition is `OpenCatalogReader` (`:229-245`). Readers expose get/watch/list/status (`:211-220`).

### Authority, mutation, and read surfaces

`ENTITY_STATES` is canonical current state, not an event ledger:

- History 1: `graph/kvcatalog.go:58-67`.
- Typed create/reconcile/delete: `processor/graph-ingest/canonical_mutations.go:199-449`.
- Graphable merge replaces only predicates present in the arrival: `processor/graph-ingest/component.go:1818-2005`.
- Local direct create/delete remain: `processor/graph-ingest/component.go:2007-2109`.
- Canonical delete is revision-fenced.

The exact read uses `graph.ingest.query.entity`, returns validated entity plus the same KV entry's revision, and is
adapted by `graph.ExactEntityReader` (`graph/exact_entity.go:15-93`). Its construction census has 11 matches: eight
production consumers, two E2E consumers, and the constructor definition. Production consumers are:

- `pkg/lifecycle/manager.go:93`
- `pkg/projection/mutation_client.go:66`
- `agentic/agentrun/nats_reader.go:31`
- `processor/graph-query/pathrag.go:82`
- `processor/agentic-loop/todos.go:62`
- `processor/agentic-tools/emit_lesson.go:201`
- `processor/rule/triple_mutator.go:31`
- `processor/gated-dag/claim.go:31`

E2E consumers are `test/e2e/scenarios/tiered_structural.go:461,511`. The constructor definition is
`graph/exact_entity.go:39`.

`pkg/lifecycle` splits enumeration/watch from exact fetch:

- `List` enumerates the reader and exact-fetches/project-matches candidates: `manager_query.go:25-88`.
- `Watch` is bootstrap/live and upsert-only: `manager_query.go:126-169`.
- `WatchEvents` includes deletes: `manager_query.go:171-205`.
- Both watch `ENTITY_STATES`: `manager_query.go:207-220`.
- Exact projections use `ExactEntityReader`: `manager.go:45-55,93,209`.

`pkg/projection.MutationClient` combines projection contracts, typed mutations, and exact reads:
`pkg/projection/mutation_client.go:25-68,119-261`.

`agentic/agentrun.NATSLoopTripleReader` exact-reads loop ancestry and treats entity/predicate absence as absence:
`agentic/agentrun/nats_reader.go:20-79`.

The service-manager operator path is distinct:

- `GET /graph/triples` is registered at `service/service_manager.go:1200`.
- It supports exact subject/predicate/object filters, default limit 100, max 1000:
  `service/graph_triples_http.go:31-102`.
- It opens `ENTITY_STATES`, enumerates keys, decodes entities, and filters: `:139-223`.
- With no NATS client it returns an empty array: `:149-153`.
- It is documented as low-throughput debugging access: `:3-13`.

There are zero in-repo production constructors of `graph/query.NewClient`; only a documentation example names it.
ADR-090 calls it provisional and says GS-12 retires or internalizes it (`docs/adr/090-*.md:42-47`). It currently opens
three catalog buckets, retains an authority watcher/cache, carries whole-client poison/watch-loss state, and lazily
watches graph-index readiness (`graph/query/client.go:31-225`).

### Public graph-query and gateway census

The public NATS graph-query surface is 16 operations. Fifteen register together at
`processor/graph-query/query.go:30-63`:

1. `entity`
2. `entityByAlias`
3. `batch`
4. `relationships`
5. `pathSearch`
6. `hierarchyStats`
7. `prefix`
8. `spatial`
9. `temporal`
10. `semantic`
11. `similar`
12. `globalSearch`
13. `summary`
14. `searchGraph`
15. `byName`

The sixteenth, `graph.query.localSearch`, registers separately at `processor/graph-query/graphrag.go:215-228`.

- The base 15 bind during every successful graph-query Start: `component.go:420-455`.
- `localSearch` binds only after `COMMUNITY_INDEX` becomes available and its watcher starts: `:456-489,578-610`.
- If absent at startup, a background resource check runs and no responder exists until availability: `:474-489`.
- A handler without cache returns transient error, but the usual absent-bucket state has no handler: `graphrag.go:231-259`.

Graph gateway advertises wildcard output `graph.query.*` (`gateway/graph-gateway/component.go:142,157`), routes
GraphQL `localSearch` (`:973-979`), and transforms it (`:1098,1656`). Its schema exposes 14 of 16 operations; `batch`
and `byName` are NATS-only. It also exposes/routes `capabilities` (`:1000,1079,1658`) but exact production search finds
no responder. This is the overlap carried by #784 and #315.

### Derived indexes and readiness

Graph-index maintains keyed projections and replacement sets:

- Incoming, predicate, and name memberships use desired-key replacement:
  `processor/graph-index/owner_reconcile.go:18-127`.
- Update/delete paths maintain keyed indexes: `processor/graph-index/component.go:1160-1455,1910-2053`.
- Alias is outside the owner-reconciliation set: `processor/graph-index/component.go:1370-1376`.
- `DeleteFromAliasIndex` exists at `:2104` but has no production caller.
- Predicate keys are raw predicate strings: `processor/graph-index/predicate_index.go:12-45`.
- Current spec has both raw-key truth and stale hash/catalog reasoning:
  `openspec/specs/graph-index/spec.md:261-276,367-405`.

`pkg/revlag.Watermark` is a process-local sparse-revision low-water tracker:

- Tracks delivered revisions, pending work, and covered commit time: `pkg/revlag/watermark.go:1-71`.
- `Observe`/`Complete`: `:74-160`; `Indexed`/`Observed`/`IndexedAt`: `:162-204`.
- Used by graph-index (`component.go:24,269,628`) and graph-embedding (`component.go:25,332,675`).
- Projected through `ComputeIndexStatus`: `graph/index_status.go:164-252`.
- It is process-local and resets with the owner.

`graph/readiness` owns keys for graph-index, graph-embedding, graph-ingest, and rule
(`graph/readiness/watcher.go:45-66`), plus publisher/watcher, held outcomes, freshness, classification, retry/rebind,
and multi-key Set. Rebind is at `watcher.go:327-380`; caller-owned keys at `set.go:11-60`; coverage at `:107-179`.
There is no `KeyGraphClustering`.

Graph-clustering consumes graph-index readiness always (`processor/graph-clustering/component.go:1361-1385`) and
graph-embedding only with semantic edges (`:1386-1401`). Thus #618's “no embedding consumer” premise is stale while
its anomaly-path fail-open premise remains current (`similarity.go:189-190`).

### Hierarchy and clustering

- Graph-ingest creates real prefix containers: `graph/inference/hierarchy.go:144-390`.
- Graphable ingest invokes hierarchy: `processor/graph-ingest/component.go:1288-1314`.
- Local direct create invokes hierarchy unconditionally: `:2007-2088`.
- Canonical mutation create is hierarchy-free: `canonical_mutations.go:199-270`.
- Each LPA “level” reruns the full set and emits `ParentID: nil`: `graph/clustering/lpa.go:585-617`.
- Graph-clustering hardcodes three levels: `processor/graph-clustering/component.go:1251-1255`.
- Entity/type/system caches build once: `processor/graph-clustering/entityid_provider.go:333-395`.
- `ClearCache` has no production call.
- Exported inference types and `InferRelationshipsFromCommunities` have no production caller:
  `graph/clustering/types.go:74-77`, `graph/clustering/lpa.go:653-835`.

### Research, fusion, and caller outcome

Graph research composes classify, route, execute, assess, synthesize, and rules. E2E intentionally drives only
`synthesize_directly` and asserts execute/assess absent: `test/e2e/scenarios/research-graph/scenario.go:9-16,357-428`.

The exported `research_graph` tool's default outcome is asynchronous dispatch
(`frameworkcapabilities/graphresearch/executor.go:162-410`):

1. validate `topic` and require parent `LoopID`;
2. create executing research LoopEntity in `AGENT_LOOPS`;
3. write `research.requested.<loopID>`;
4. best-effort birth graph entity and kickoff triples;
5. return success immediately with `StopLoop: true`; and
6. tell the caller `SearchResult` arrives on a later iteration.

Kickoff publication failure is not returned as tool failure; the tool can report success while the chain stalls
observably (`executor.go:330-394`). Registration requires `read_loop_result` but owner-creates/opens `AGENT_LOOPS`
(`register_tool.go:19-67`). `pkg/fusion` already supplies deterministic framework fusion; #376 overlaps shipped code,
while #391 remains current E2E coverage evidence.

### Retention, storage, and observability

Current graph retention is deletion of authority plus owner-local derived cleanup. There is no shared participant,
semantic mode, delete-retention subject, purge-delete runtime, checkpoint, backup, restore, or graph-wide GC.

Exact production searches find zero `RetentionParticipant`, `delete.retention`, `RetentionMode`, `PurgeDeletes`, and
`.Purge(`. `storage/objectstore.Store.Delete` exists (`storage/objectstore/store.go:358-370`), but no semantic-blob GC
caller exists.

`COMMUNITY_SUMMARIES` is content-addressed by level/membership hash, written only by the enhancement worker, watched
and joined by graph-query, and not reclaimed by bounded GC. Its current consumer lifecycle is unsound after watch
loss: `WatchSummaries` receives without the channel `ok` value, so a closed updates channel is indistinguishable from
the initial-sync `nil` sentinel and loops as false sync completion. The successful-attach once guard is never cleared,
while the resource watcher observes bucket presence only. A summary watch that closes while the bucket remains present
therefore neither unpublishes cached summaries nor rebinds; stale LLM summaries can remain reachable until component
restart. This is independent of partition readiness because summary absence already falls back statistically
(`processor/graph-query/component.go:130-140,612-688`;
`processor/graph-query/community_cache.go:37-49,191-280`).

`STORAGE_REPORT` is separate operational state:

- History 10 is part of rate computation: `graph/kvcatalog.go:76-103`.
- Storage observability owns ensure/publisher wiring: `service/storage_observability.go:377-440`.
- Publisher inventories and removes stale report keys: `natsclient/storage_report.go:145-225,696-737`.
- Consumer maintains process-local snapshot: `natsclient/storage_report_consumer.go:121-357`.
- HTTP read is `GET /storage-observability/report`: `service/storage_observability_http.go:53-175`.

It reports capacity/growth/pressure; it is not graph retention authority.

### Reproducibility

LPA sorts IDs/neighbors, uses fixed seed, and deterministic ties (`graph/clustering/lpa.go:196-223,270-303,470-519`).
Fusion and graph-index reconciliation are deterministic for a fixed observed set.

Current gaps:

- LPA type/system caches can become stale in a long-running process.
- BM25 corpus statistics are process-local, mutable, and ingestion-order dependent:
  `graph/embedding/bm25_embedder.go:59-70,106-140,178-201,338-413`.
- Restart can therefore create a different vector space from the same eventual corpus.
- Community summaries depend on an external model and currently receive no content fetcher:
  `processor/graph-clustering/component.go:2168-2194`,
  `graph/clustering/summarizer.go:508-510,526-561,602-618`.

## Collision tables

Every table includes semantic class, owners, catalogs, status, lifecycle, ownership, readers, writers, and recovery.

### Declaration/configuration collision inventory

| Surface | Semantic class | Owners | Catalogs | Status | Lifecycle | Ownership | Readers | Writers | Recovery |
|---|---|---|---|---|---|---|---|---|---|
| Shipped `PortConfig` | Preconstruction intent | Config author; constructor validates | 21-config census; factory registry; grammar | Raw; may omit defaults | Before construction/provisioning | Operator overrides; component defaults | Planner; factories | JSON/config manager | Reload/restart desired config |
| Effective ports | Runtime declaration source | Component instance | Resolved component config | Current for instance | Construct before admission; neutral updates keep shape | Component | Registry only | Constructor/config merge | Reconstruct replacement |
| Registry generation | Accepted process-local declaration | Registry | Instance map; generation counter | Admitted, not readiness | Atomic add/replace/remove | Registry owns admission/resources | Flowgraph, manager, capabilities, logger | Registry capture | Rebuild at boot/replacement; no durable replay |
| Stream planning | Provisioning control | Config/provisioner | `config.streams`; canonical facts | Planned coverage | Before live generations | Stream policy owner-local | Boot planner | Config/operator | Re-run boot; streams persist |
| Logger snapshot view | Optional diagnostic view | Started logger | Registry sets + explicit subjects | Current capture set | Outer-enabled Start; observe; Stop | Logger owns subscriptions only | Logger HTTP/KV/SSE | Registry observer/config | Replay current set after restart |
| Service composition | Next-boot desired versus sealed set | Config/service managers | `services.*`; sealed identities | Restart-required delta | Seal until restart | Service manager owns running set | Services/health/routes/OpenAPI | Desired config; boot | Restart consumes desired map |

### Authority/read collision inventory

| Surface | Semantic class | Owners | Catalogs | Status | Lifecycle | Ownership | Readers | Writers | Recovery |
|---|---|---|---|---|---|---|---|---|---|
| `ENTITY_STATES` | Canonical current authority | graph-ingest | KVCatalog; history 1 | Current value; reader poison fail-closed | Create/reconcile/delete; watch replay | graph-ingest sole writer | Exact RPC, index family, lifecycle, triples endpoint, provisional client | Typed mutations; Graphable ingest | Current-state replay; operator backup only |
| Suffix index | Derived lookup | graph-ingest | KVCatalog | May miss until fallback repair | Update create/merge; delete cleanup | graph-ingest | suffix query | ingest | Full-scan fallback repopulates found mapping |
| Applied sequence | Redelivery dedup | graph-ingest | KVCatalog | Last entity/stream seq | Around ingest delivery | graph-ingest | ingest guard | ingest | Durable KV; ordinary idempotency if absent |
| Exact RPC | Operation-specific authority read | ingest responder; adapter caller | NATS subject | Entity + same-entry revision | Bind with ingest; request-scoped | No bucket transfer | Eight adapter consumers | None | Caller re-read/retry; no cache |
| Lifecycle List/Watch | Typed projection/reactive view | lifecycle Manager | Workflow schemas; entity reader | scan/bootstrap/live/delete variants | Register; caller-cancel watch | Lifecycle owns schemas | Lifecycle gateway/library | Canonical mutations | Re-list/bootstrap |
| `/graph/triples` | Operator/debug full scan | service manager | HTTP route; entity reader | Filtered current triples | Per request | No writes | Operators/dashboards/e2e | None | Rescan next request |
| Aggregate query client | Provisional mixed direct reader | Calling process | Three buckets; readiness; cache | Whole-client poison/watch loss | Lazy bind; permanent watch | No writes | Zero production constructors | None | Reconstruct process/client |
| Projection client | Typed mutation façade | Contract author + ingest | Contracts; exact/mutation subjects | Revision/commit typed result | Request scoped | Client validates contract; ingest authority | Projection adopters | Typed operations | Explicit re-read/retry |

### Derived-index/status collision inventory

| Surface | Semantic class | Owners | Catalogs | Status | Lifecycle | Ownership | Readers | Writers | Recovery |
|---|---|---|---|---|---|---|---|---|---|
| Out/in/predicate/name | Required query views | graph-index | KVCatalog; ports; projection rules | Revision-lag key | Bootstrap/live reconcile/delete | graph-index only | query/gateway/clustering/fusion | graph-index | Rebuild authority; revlag coverage |
| Alias | Alias lookup | graph-index | KVCatalog; output port | Current writes; no owner-reconcile replacement | Projection update; delete helper unused | graph-index only | entity-by-alias | graph-index | Rebuild; stale-delete premise remains |
| Spatial/temporal/reverse | Specialized views | spatial/temporal owners | KVCatalog | Owner-local | Watch authority; reverse cleanup | respective owner | spatial/temporal query | respective owner | Rebuild authority |
| Embedding/dedup | Semantic enrichment | graph-embedding | KVCatalog; status | pending/generated/failed/readiness | Watch/workers/terminal records | embedding only | semantic query; clustering | embedding | Replay authority/pending; BM25 state local |
| Community | Clustering view | graph-clustering | KVCatalog; no key | Availability not build/ready contract | Periodic partition | clustering only | community cache/search | clustering | Periodic recompute |
| Summaries | Content-addressed enrichment | enhancement worker | KVCatalog; hash keys | Missing degrades | Optional worker; query late attach | worker only | query join | worker | Recompute; no bounded GC |
| Anomaly | Optional review view | clustering | KVCatalog | Config-dependent | Detection/review | clustering only | review worker | clustering | Recompute/cleanup |
| revlag | Process-local convergence | owner instance | No durable catalog | low-water/commit time | Owner start/drain/exit | owner-local | status projection | watcher/workers | Recreate bootstrap |
| `GRAPH_STATUS` | Operational readiness | Four producers | KVCatalog; keys | held outcomes/freshness/errors | heartbeat; retry/rebind; fold | producers own keys; consumers key sets | readiness consumers | index, embedding, ingest, rule | KV replay/history 3 |
| `STORAGE_REPORT` | Storage inventory/growth | inventory collector | KVCatalog; history 10 | capacity/growth/pressure | periodic publish/watch | collector writes; observability reads | metrics/health/HTTP | publisher | history seeds rate; stale keys removed |

### Query/gateway collision inventory

| Surface | Semantic class | Owners | Catalogs | Status | Lifecycle | Ownership | Readers | Writers | Recovery |
|---|---|---|---|---|---|---|---|---|---|
| Base 15 responders | Public application reads | graph-query | Literal handler table; wildcard declaration | Bound after Start | Start subscribe; Stop unsubscribe | graph-query subjects | NATS; GraphQL subset | None | Restart rebind |
| `localSearch` | Conditional public read | graph-query GraphRAG lifecycle | Separate subscription; gateway route | No responder until community bucket | Bind after cache watcher | graph-query while enabled | NATS/GraphQL | None | Resource watcher retries/binds |
| GraphQL reads | Required remote reads per ADR-090 | graph-gateway | Introspection + substring router + wildcard output | 14 query ops; capabilities unserved | Gateway lifecycle | HTTP translation only | HTTP callers | None | Caller retry/restart |
| NATS query subjects | Transport public reads | graph-query | 16 literals; no exported list | conditional localSearch | Component subscriptions | Per-responder subject | Gateway GraphQL subset; research classify/execute; agentic-tools search/summary; outside-repo callers unverified | None | Handle timeout/no-responder |
| Capabilities | Advertised but unserved | Gateway advertises; no responder | Schema/router only | No production responder | Always advertised | Unresolved | GraphQL callers | None | No current responder or recovery path |
| GraphQL classified errors | Public failure projection | graph-gateway | ADR-060 typed error reaches gateway; writer emits message only | Class/code discarded | Request scoped | Gateway projection only | GraphQL callers | None | Caller can only parse text |
| Optional summary watch | LLM enrichment cache | graph-query | Presence watcher plus once guard | Watch loss not represented | One attach per process | graph-query local | Community synthesis | enhancement worker | No rebind while bucket remains |

Concrete in-repo subject consumers include research classify (`processor/research-graph-classify/adapters.go:41,146`),
research execute (`processor/research-graph-execute/adapters.go:24-27,81,185,238,288,338`), agentic-tools graph search
(`processor/agentic-tools/executors/search_graph.go:20,142`), and graph summary
(`processor/agentic-tools/executors/summarize_graph.go:50,174`). Graph gateway routing is at
`gateway/graph-gateway/component.go:142,157,915-1110`.

Graph-query declares four exact inputs in `DefaultConfig` (`entity`, `batch`, `relationships`, `pathSearch`), but its
production `CreateGraphQuery` factory unmarshals into a zero `Config`, applies scalar defaults only, and never merges
`DefaultConfig`. Those four declarations are therefore schema/test defaults, not effective production rows. Each of
the eleven shipped instances has only its explicit `graph.query.>` input
(`processor/graph-query/component.go:90-101,175-205`). Eight of those configs also contain gateway's separate
`graph.query.*` output; the two research configs and graph-backend do not.

Agentic-tools also has two executor catalogs with different lifetimes. The shared dependency registry is populated
before managed component construction. The component-local registry remains mutable through exported
`RegisterToolExecutor` after construction and dispatches before the shared registry, so a local executor can override a
shared builtin with the same name (`processor/agentic-tools/component.go:779-837`;
`processor/agentic-tools/README.md:140-178`). Registry captures immutable component ports immediately from
`InputPorts`/`OutputPorts` (`component/registry.go:841-849`). `component.ToolRegistryReader` exposes execution and tool
definitions but no implementation-dependency metadata (`component/dependencies.go:42-56`). Current code therefore
cannot infer an arbitrary local executor's messaging dependencies into the already captured port generation.

Agentic model-facing discovery does not consult the component-local registry: agentic-loop's default discovery reads
only `deps.ToolRegistry.ListTools`, and agentic-dispatch resolves/drops configured `default_tools` against that same
shared registry (`processor/agentic-loop/handlers.go:436-451,908-919`;
`processor/agentic-dispatch/component.go:25-74`). A tool moved exclusively to agentic-tools' local registry remains
executable by name at that component but is absent from default model definitions and configured default-tool
resolution.

### Hierarchy/research/retention/reproducibility collision inventory

| Surface | Semantic class | Owners | Catalogs | Status | Lifecycle | Ownership | Readers | Writers | Recovery |
|---|---|---|---|---|---|---|---|---|---|
| Hierarchy containers | Authoritative modeled entities | ingest inference | ID convention; authority | Real entities | Graphable birth; direct/canonical create differ | ingest authority | prefix/hierarchy/generic reads | ingest | Replay Graphable input |
| LPA levels | Derived clustering presentation | clustering | Hardcoded 3 | Full-set reruns; nil parents | Each detection | clustering | GraphRAG/community | clustering | Periodic recompute |
| EntityID caches | Internal accelerator | clustering process | None | Build-once/stale | First enumeration; no clear call | owner-local | LPA provider | cache builder | Process restart |
| Research chain | Async orchestration | Five components + rules | declarations/rules/loops/triples | Full chain; short E2E only | dispatch then rules | Components execute; rules reference | parent continuation/tool | tool/components/ingest | Durable loop/result; kickoff best-effort |
| Fusion | Deterministic evidence assembly | package/caller | Code contract only | Implemented | Request scoped | caller-local | search/research | None | Recompute |
| ObjectStore content | Durable payloads | Store/provider/application | Registry/provider; ObjectStore | Explicit blobs; references can orphan | Put/Get/Delete | provider owns blobs; graph no GC claim | content consumers | store writers | Explicit/operator delete |
| BM25 stats | Process-local model | embedding instance | No durable model | Order/restart sensitive | Mutates per document | owner-local | embedding query/index | embedder | Replay with order variance |
| Retention/GC | Absent shared subsystem | None | No participant/mode/subject | Empty searches | None | None | None | None | Operator backup; owner rebuild |

## Adopter seam inventory

These rows describe a developer outside this repository. They make no downstream parity claim.

| Person/surface | What must they know? | If they do nothing | Discovery | What should they know? |
|---|---|---|---|---|
| Component implementer | Two Discoverable methods; stable generation; declaration change needs replacement | Existing source compiles; changing hot update rejects before mutation | interface/docs/cutover | Current bill: semantic ports, two methods, generation stability; Registry cloning/conflict/observer mechanics are not adopter inputs |
| Shipped-config author | Partial ports merge defaults; effective may exceed JSON; unknown factory fails | Defaults admit/observe; invalid factory fails | schemas/census/validation | Current bill: intentional overrides and effective-declaration inspection; defaults may add subjects absent from JSON |
| GraphQL caller | Schema/variables and human error text; ADR-060 class/code exist internally but the gateway discards them | Classified invalid/not-ready failures arrive as message-only errors, forcing text parsing | `/graphql` response | Current gap: callers cannot observe framework-owned class/code and should not have to know NATS headers or guess from text/status |
| Direct NATS query caller | One of 16 literals; JSON; timeout/no-responder; batch/byName NATS-only | wrong/absent subject times out; no exported collision list | query package files/docs | Current bill includes literal subject and wire shape because the 16-operation catalog is not exported |
| `localSearch` caller | Community availability controls responder existence | Early calls get infrastructure no-responder | logs/code/#820 | Current gap: absence does not distinguish clustering disabled, community building, or responder/infrastructure loss |
| Aggregate client caller | Three buckets, permanent watcher/cache/readiness, poison/watch-loss | Adds process-lifetime state; no production seam evidence | client/ADR-090 | Current bill includes direct-KV dependencies, retained watch/cache, readiness, and whole-client failure state; ADR-090 records no canonical general client |
| Readiness component author | Explicit producer keys, Watcher/Set, freshness/unknown; no clustering key | Omitted key is ungated; irrelevant key defers on other outage | readiness/ADRs/specs | Current bill: exact dependency keys and Set folding; acquisition/freshness/malformed/rebind are shared mechanics |
| Readiness config author | Enabled producers and consumer key requirements; semantic edges add embedding | No global list; clustering readiness unrepresentable | config/schema/docs | Current bill: enabled producers and consumer-owned key set; clustering cannot be represented as a key |
| Graph-gateway readiness-config author | `readiness_keys` is the complete selected list; `readiness_path` applies only when non-empty | Empty keys start no watchers and register no route; configured unavailable keys keep the route and report unknown | `gateway/graph-gateway/component.go:82-83,663-672,808-821`; `readiness_surface.go:35-54,78-105` | Current bill: whether the deployment requests the operator surface and which keys it displays; empty disables route/acquisition, unavailable configured keys stay visible |
| `/graph/triples` operator | O(N), exact filters, max 1000, mux auth | Broad call scans all; no NATS returns empty array | comments/OpenAPI | Current bill: diagnostic-scan semantics, exact filters, and 1000 ceiling; no high-volume/paginated contract exists |
| Lifecycle adopter | Register schema; cancel contexts; Watch omits deletes; WatchEvents includes; List O(N) | Unregistered fails; uncanceled watch pins; Watch misses reclaim | Go docs | Current bill: registration, List cost, cancellation, and Watch/WatchEvents delete difference |
| Projection adopter | Entity pattern, birth/reconcile/append groups, revision/commit handling | Undeclared/wrong modes fail; reconcile reads authority | exported contracts | Current bill: pattern, birth predicates, group modes, allowed predicates, revision/commit outcomes |
| `research_graph` caller | topic; framework supplies LoopID/role; success means dispatched | returns `StopLoop` immediately; later result; kickoff failure may still stall after success | tool result/continuation docs | Current gap: dispatch success, chain completion, and kickoff graph-birth success are distinct; loop metadata is not completion |
| Raw KV/operator reader | Must-exist catalog read does not provision | missing bucket is not-ready; reader cannot repair | KVCatalog docs | Current bill: bucket name, owner, diagnostic purpose; raw KV is not an application absence fallback |

## Live issue-premise inventory

### Mandatory checkpoint issues

| Issue | Class | Merged-tree disposition |
|---|---|---|
| #620 | overlapping | Embedding cache/max-hop gone; dimensions load-bearing. Pending gauge has no production call; community `IsAvailable` survives only in stale comment; `IsReady` has zero callers; several config knobs remain validation/default-only; aggregate client retains one TTL; exported dormant LPA inference remains. |
| #795 | overlapping | Watcher owns bind retry/rebind, so caller-retry premise is stale. No catalog-bound Set constructor; callers choose keys and wire Watcher/Set. |
| #810 | current | Default remains `tool.list`; nine configs declare `tool.>`; none overrides `discovery.tool.list`; no production collision guard found. |
| #820 | current | Four readiness keys, no clustering key; localSearch conditionally unbound. |
| #842 | deferred-by-ruling | Breaking default move waits for future wave and #810 guard; guard absent. |
| #859 | stale | Closed; shared facts/resolution and Registry capture merged; logger raw parsing deleted. |
| #862 | stale | Target conflicts with later accepted retained methods and Registry capture. |
| #868 | overlapping | Publisher/Watcher/Set remain graph-payload-shaped; SemSource reuse is outside approved downstream audit and unverified. |

### Additional same-territory issues

| Issue(s) | Class | Merged-tree disposition |
|---|---|---|
| #822 | current | Sixteen subjects across two registration sites; no exported complete list. |
| #421 | overlapping | Aggregate client lacks readiness-suffixed variant and is provisional; claimed downstream incidence unverified. |
| #618 | overlapping | Semantic-edge clustering watches embedding; anomaly `FindSimilar` still swallows/fails open. |
| #589 | current | Anomaly storage `Watch` has only its production definition. |
| #609 | overlapping | Community cache now keys by `(level, ID)` consistently. Remaining: ready has no false transition; stale `IsAvailable` comment; success path lacks loss monitoring; detection is ticker-first; localSearch remains conditional (`processor/graph-query/community_cache.go:18-31,84,94-180,404-407`). |
| #422 | overlapping | Five exported aggregate query methods have zero production call sites; `NATSContentFetcher` is gone. Downstream zero-caller claim was not re-audited. |
| #785 | current | `graph.UnwrapQueryResponse` exists and gateway uses it, while agentic-tools/research consumers still decode different bare/typed reply shapes locally. |
| #819/#823 | current | Normal global-search responses do not set `Strategy`; agentic-tools decodes digests but not full `Entities`, producing a count-only fallback when only entities/count arrive. |
| #839 | current | Prefix is max-payload-fit/paginated; batch still marshals the full caller ID set; community membership remains one inline KV record with permanent oversize rejection. |
| #725 | stale | hello-world now contains `ALIAS_INDEX`. |
| #828 | current | Spec contains raw predicate-key truth and stale hash reasoning. |
| #571 | current | Whole-client poison/permanent watch remain; zero production constructors. |
| #619 | current | BM25 state remains process-local/order-dependent. |
| #633 | current | Store delete exists; no reference-aware graph blob collector. |
| #710 | current | Content-addressed summaries exist; no bounded reclamation. |
| #829 | current | Summarizer lacks content fetcher. |
| #746 | current | Append/first-wins can hide later companion predicate. |
| #391 | current | E2E takes direct synthesis and excludes execute/assess. |
| #376 | overlapping | Fusion is implemented; broader research adoption separate. |
| #527 | overlapping | Incoming/predicate/name owner reconciliation exists; alias remains outside. |
| #606 | current | Three full-set LPA passes and nil parents remain. |
| #672 | current | Build-once caches; no `ClearCache` caller. |
| #436 | current | Real containers share searchable prefixes with workflow/unit entities. |
| #751 | overlapping | Local create does hierarchy; canonical create is hierarchy-free. |
| #875 | current | Instance-blind StorageRef fallback remains. |
| #887 | unverified | Issue is a question; no owner ruling found. |
| #784/#315 | overlapping | GraphQL advertises/routes capabilities; no responder. |
| #347 | deferred-by-ruling | Research Phase 2 gated beyond accepted Phase 1. |
| #802 | deferred-by-ruling | Post-v1 trigger conditions. |
| #211 | deferred-by-ruling | ADR-090 defers MCP read contract. |
| #578 | overlapping | Merge semantics current; ownership proposal overlaps ADR-091. |
| #818 | deferred-by-ruling | Broader semantic/principal enforcement outside ADR-091 slice. |
| #579 | overlapping | `pkg/graphview` implements shared view; broader public/cross-process claims separate. |
| #837 | overlapping | Whole-pass abort changed; #855 retains destructive partial-partition behavior. |
| #855 | current | Permanent-drop pruning can destroy prior valid membership. |
| #525 | stale | `lastProjections` and changed/unchanged behavior exist. |
| #236 | stale | Target `STRUCTURAL_INDEX` retired. |

The prior checkpoint placed #688-#690, #736, #765, and #882-#886 outside declaration-generation. They remain
`unverified` here. Separate message-logger authorization/filtering/scaling #472/#587 are also `unverified`.

## Reproducible measurements and empty searches

All commands run from `/private/tmp/semstreams-gs00-recovered`.

### Baseline

```bash
git rev-parse HEAD
git rev-parse origin/main
```

Both returned `967b75b6ebcb0f1b0eee9157e76c39da982aa640`.

### Port declaration census

Runtime-root raw count:

```bash
rg -c 'func \([^)]*\) (InputPorts|OutputPorts)\(\)' \
  component gateway input output processor storage \
  -g '*.go' -g '!**/*_test.go'
```

Result: 71 in 36 files. Excluding `component/doc.go` and `component/test_helpers.go` yields 68 executable declarations
in 34 runtime files.

Repo-wide raw count:

```bash
rg -c 'func \([^)]*\) (InputPorts|OutputPorts)\(\)' \
  . -g '*.go' -g '!**/*_test.go'
```

Result: 79 in 40 files. Excluding those two files yields 76 in 38 files. The extra executable declarations are:

```bash
rg -n 'func \([^)]*\) (InputPorts|OutputPorts)\(\)' \
  examples cmd -g '*.go' -g '!**/*_test.go'
```

Result: six example and two `cmd/e2e-semstreams/mission` methods.

### Remaining production port-method interpreters

```bash
rg -n '\.InputPorts\(\)|\.OutputPorts\(\)' \
  . -g '*.go' -g '!**/*_test.go' -g '!**/doc.go'
```

Result: only `component/registry.go:844,848`.

Message-logger raw-config interpreter:

```bash
rg -n \
  'types\.ComponentConfig|config\.Components|PortConfig|PortDefinition|BuildPortFromDefinition|MergePortConfig' \
  service -g 'message_logger*.go' -g '!**/*_test.go'
```

Result: zero.

Current logger Registry consumption:

```bash
rg -n \
  'componentRegistry\.(Snapshots|ObserveSnapshots)|collectSnapshotSubjects' \
  service/message_logger.go
```

Result: initial read at lines 336-338, projection at 344, observation at 572.

### Frozen configuration and stream counts

`service/testdata/message_logger_subject_census.json` names the 21 files and records raw 385/243/51, effective
561/378/66, delta 176/135/15, removals 0.

```bash
rg -n 'message_logger_subject_census|require.Len\(t, census.Scope, 21\)' \
  service -g '*_test.go'
```

The 61/61/0 default-only stream census uses the same frozen scope in `service/stream_planning_census_test.go`.

### Public graph-query count

```bash
rg -n \
  'graph.query.(entity|entityByAlias|batch|relationships|pathSearch|hierarchyStats|prefix|spatial|temporal|semantic|similar|globalSearch|summary|searchGraph|byName)' \
  processor/graph-query/query.go
```

Result: 15 registration rows.

```bash
rg -n 'SubscribeForRequests\(ctx, "graph.query.localSearch"' \
  processor/graph-query/graphrag.go
```

Result: one at line 219; total 16.

```bash
rg -n 'graph\.query\.capabilities' \
  processor graph gateway -g '*.go' -g '!**/*_test.go' -g '!**/doc.go'
```

Result: zero production responder matches.

Concrete research/agentic subject consumers:

```bash
rg -n \
  'graph\.query\.|graph\.index\.query\.|graph\.embedding\.query\.' \
  processor/research-graph-* frameworkcapabilities/graphresearch \
  processor/agentic-tools \
  -g '*.go' -g '!**/*_test.go' -g '!**/doc.go'
```

The result includes research classify/execute and agentic-tools search/summary sites cited in the query collision table.

### Exact-reader and provisional-client searches

```bash
rg -n 'NewExactEntityReader\(' \
  . -g '*.go' -g '!**/*_test.go' -g '!**/doc.go'
```

Result: 11 matches—eight production consumers, two E2E consumers at
`test/e2e/scenarios/tiered_structural.go:461,511`, and the constructor definition.

```bash
rg -n 'query\.NewClient\(' \
  . -g '*.go' -g '!**/*_test.go' -g '!**/doc.go'
```

Result: zero.

### Additional query-issue evidence

```bash
rg -n \
  'func \([^)]*\) (QueryPrefix|GetEntitiesByPredicate|ListPredicates|GetPredicateStats|QueryCompoundPredicates)\(|NewNATSContentFetcher\(' \
  graph -g '*.go' -g '!**/*_test.go'
```

Result: five method definitions and no `NewNATSContentFetcher`.

```bash
rg -n \
  '\.(QueryPrefix|GetEntitiesByPredicate|ListPredicates|GetPredicateStats|QueryCompoundPredicates)\(|NewNATSContentFetcher\(' \
  . -g '*.go' -g '!**/*_test.go' -g '!**/doc.go'
```

Result: zero production calls.

```bash
rg -n 'UnwrapQueryResponse' \
  gateway processor/agentic-tools processor/graph-clustering test/e2e/client \
  -g '*.go' -g '!**/*_test.go'
```

Result: graph-gateway only.

```bash
rg -n 'Strategy:' \
  processor/graph-query/graphrag.go processor/graph-query/searchgraph.go \
  -g '*.go' -g '!**/*_test.go'
```

Result: only `processor/graph-query/searchgraph.go:219`.

### Readiness searches

```bash
rg -n 'KeyGraphClustering|graph-clustering' \
  graph/readiness -g '*.go' -g '!**/*_test.go'
```

Result: no key/producer; only a comment naming a possible consumer.

```bash
rg -n 'KeyGraphEmbedding' \
  processor/graph-clustering -g '*.go' -g '!**/*_test.go'
```

Result: conditional watcher at `component.go:1392`.

### Issue #810 current premise

```bash
rg -n 'tool\.>' configs -g '*.json'
rg -n 'tool\.list|discovery\.tool\.list' configs -g '*.json'
```

Results: nine `tool.>` config matches; zero list/discovery overrides. Default is
`processor/agentic-tools/config.go:132`.

### Alias, anomaly-watch, retention, and GC negatives

```bash
rg -n 'DeleteFromAliasIndex\(' \
  . -g '*.go' -g '!**/*_test.go' -g '!**/doc.go'
```

Result: definition only at `processor/graph-index/component.go:2104`.

```bash
rg -n '\.Watch\(ctx\)|storage\.Watch|NATSAnomalyStorage.*Watch' \
  graph processor -g '*.go' -g '!**/*_test.go' -g '!**/doc.go'
```

Result: definition only at `graph/inference/storage.go:417`.

```bash
rg -n \
  'RetentionParticipant|delete\.retention|RetentionMode|PurgeDeletes|\.Purge\(' \
  . -g '*.go' -g '!**/*_test.go'
```

Result: zero.

## Scope amendment

The old checkpoint required ten downstream repositories as holdouts. The later owner ruling superseded that execution
scope. The binding archived E.9 task is
`openspec/changes/archive/2026-08-09-post-foundation-b-declaration-generation/tasks.md:243-246,282-286`:

- publish the migration notice;
- do not perform exhaustive downstream parity audit;
- do not implement downstream migrations here; and
- downstream teams own tag adoption, compilation, explicit fixes, flow validation, and product E2E.

This inventory therefore records SemStreams exported/adopter seams and marks outside-repo claims unverified where
applicable. It claims no downstream compilation or behavior parity.

## Owner decision boundaries discovered

No ruling is made here. The merged tree exposes these policy collisions for any later owner-authored target state:

- Whether conditional `localSearch` absence is a transport or capability/readiness outcome.
- Whether graph-clustering belongs in the existing readiness key contract.
- Whether the provisional aggregate query client remains exported, is internalized, or is retired under ADR-090.
- Whether GraphQL `capabilities` gains an owner or leaves the schema.
- How complete NATS graph-query subject knowledge is owned; the 16-operation list spans two unexported registration
  sites and consumers reproduce literal subjects and reply shapes.
- Whether alias cleanup joins graph-index owner reconciliation.
- Whether inert configuration and dead exported inference/watch surfaces remain contracts.
- Whether a dropped oversized community may prune prior valid membership.
- Whether content-addressed graph/ObjectStore data receives owner-specific reclamation.
- Whether BM25 process-local corpus statistics are an accepted reproducibility boundary.
- Whether readiness remains graph-specific or becomes payload-generic.
- Whether `research_graph` may return dispatch success after kickoff graph-birth failure.

These are inventory findings only. Binding decisions remain with the owner.
