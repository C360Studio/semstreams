# Post-G foundation remap inventory and recommendation

**Status:** Owner approved revised Option 2 and approved adding the derived-index/current-state conformance matrix.
This artifact remains design/release authority only for the bounded Option 2 slices stated below; the matrix does not
independently authorize runtime work, issue administration, or a generic convergence programme. The 2026-08-11
recovery pass below corrects inventory truth only; it does not expand the owner-approved runtime boundary.

**Runtime evidence baseline:** `480607d9` (`v1.0.0-beta.159-121-g480607d9`)

**Recovered merged-tree design baseline:** `4593996ef56f50766dcf58fe2200081b72a59133`

**Frozen documentation commit before this correction:** `bf5bfeaf`

**Issue-queue snapshot:** 155 open issues on 2026-08-11. The adjacent
`post-g-foundation-remap-issue-census.tsv` records every issue number exactly once with its title and disposition.

**Inventory review:** The initial runtime-baseline inventory received `INVENTORY PASS`; the derived-index matrix later
received independent exact-diff approval after correction. The recovered merged-tree inventory at `4593996e` received
`INVENTORY FAIL` because it omitted an accepted ADR conflict, current contrary regression tests and completion
accounting, and the full research task/rule dispatch surface. The correction addendum below incorporates all four
findings. The exact materialized correction received independent `INVENTORY PASS` on 2026-08-11 against recovered
baseline `4593996e`; target-state drafting began only after that pass.

## Program intent and evidence boundary

The immediate goal is a stable pre-v1 tag that downstream projects can pin and migrate to. Stability means that the
SemStreams-owned substrate is coherent, its clean breaks are explicit, and deterministic tests that this repository
can honestly run are green. It does not mean implementing every open feature or predicting production behavior that
only a sister project's workload and deployment can measure.

Downstream repositories are not an exhaustive pre-tag gate. SemStreams owns the exact framework contract, migration
notice, release evidence, and tag. A representative holdout may reveal a reproducible framework regression; that is
a candidate blocker. Downstream compile failures, stale configuration, adoption work, and product-parity differences
are post-pin migration evidence. They do not justify compatibility shims, deprecated surfaces, or reopening a clean
break.

The issue queue was classified by current evidence, not used as an implementation backlog. The classifications are:
stale or changed premise, current foundation finding, overlapping or partly superseded, deliberately deferred
foundation work, unverified or measurement-triggered, current release/test substrate, and product/feature work outside
the recommended closeout.

## Mandatory surface inventory

### Claimed gap

The post-G question was whether a broad index-convergence program was necessarily next. The merged tree does not
support that premise. It already has one authority, one admitted mutation provider, an internal operation inventory,
and owner-filtered index replacement. The concrete tag-safety gaps are narrower:

- A permanently rejected oversized community can leave a partial saved set that later drives pruning. The rejection
  is classified at `graph/clustering/storage.go:108-150`; the partial set is returned at
  `graph/clustering/lpa.go:373-450`; pruning consumes that set at `graph/clustering/storage.go:387-468`. This is #855
  and contradicts the non-destructive invariant at `openspec/specs/graph-clustering/spec.md:59-100`.
- The research-graph E2E path deliberately takes `synthesize_directly` and proves classifier candidates while
  asserting execute and assess are absent: `test/e2e/scenarios/research-graph/scenario.go:1-30,446-553`. It therefore
  does not exercise `processor/research-graph-execute/handler.go:180-186` and `fusion.Fuse` (#391).
- `message.StorageReference.StorageInstance` is an admitted backend selector, but graph-embedding's legacy fallback
  can serve a reference after `StoreRegistry` fails to resolve its named instance. That selects an unrelated store,
  turns a foreign unresolved reference into a read failure, and can pin embedding failed/degraded (#875).
- Advertised deterministic tiers have known red paths: #301 in crud-tools and #844/#860 in ops/rule behavior. A
  stable tag cannot silently omit them or treat a wrapper's honest exit code as the defect.
- ADR-068 still reasons from the retired predicate layout (#828), while the current physical layouts are raw
  predicate, hashed name, and source-owned incoming.

The initial closeout identified #855 and #875 as the two owner-approved runtime slices. The subsequent store-by-store
conformance matrix also records bounded current findings in suffix, alias, spatial, payload-bound, BM25, and possibly
anomaly lifecycle behavior. Recording those findings corrects release truth; it does not silently add them to Option
2. Each requires separate owner disposition before implementation or tag acceptance.

### Recovered merged-tree inventory correction

This addendum records the exact current authorities that constrain the already-approved Option 2. It is inventory,
not a new runtime decision.

#### Accepted ADR and current-test conflicts

- **#875 conflicts with accepted ADR-063, not only implementation.** ADR-063 explicitly directs graph-embedding to
  resolve `resolver.Streamable(ref.StorageInstance)` per fetch, then use the worker's owned `contentStore` whenever
  the registry misses, preserving single-bucket BM25 deploys; it also directs `shouldFetchViaStorageRef` to admit the
  fetch lane on either an exact registry match or any wired fallback
  (`docs/adr/063-store-substrate-and-resolver.md:362-372`). Any instance-exact correction must update that ADR truth or
  carry an explicit owner-approved deviation/supersession; it cannot silently reinterpret the accepted decision.
- **#855 contradicts current permanent-failure tests.**
  `graph/clustering/lpa_error_test.go:331-377` requires one permanent oversized community to save its writable
  siblings and return partial success with no error; `:379-409` requires an error only when every community is
  permanently rejected. By contrast, `:426-485` already requires a transient partial save failure to fail the run and
  preserve the prior partition. On nil error, `processor/graph-clustering/component.go:1779-1800` increments processed
  and activity state, observes duration, and logs `community detection complete`. The correction must retain the
  #837 sibling-save behavior while changing permanent-partial terminal truth: successful sibling writes may coexist,
  but an incomplete candidate cannot prune or enter any complete-success accounting. The safety promise is no
  deletion of prior keys—a stale superset—not byte-identical rollback of candidate writes.
- **#875 contradicts current fallback tests.**
  `graph/embedding/worker_storeresolver_test.go:84-102` requires a registry miss to read the generic owned fallback,
  and `processor/graph-embedding/storageref_fallback_test.go:27-72` requires any wired `contentStore` to admit the
  StorageReference fetch lane. These tests are implementation surfaces that must be replaced or tightened. The
  existing exact registry lookup remains at `storage/storeregistry/storeregistry.go:58-88`; the existing unresolved
  exclusion and inline continuation remain at
  `processor/graph-embedding/component.go:1816-1827,1956-1978`. A resolved exact-instance `Open` failure remains an
  infrastructure failure; a foreign or unregistered instance is excluded and must not become failed/degraded merely
  because it is unresolved.

#### Research task, fixture, and rule surfaces

- The existing public task maps `e2e:research-graph` to one tier
  (`taskfile.yml:61-62`; `taskfiles/e2e/research-graph.yml:1-47`). Its scenario identity and description are explicitly
  direct-only (`test/e2e/scenarios/research-graph/scenario.go:102-123`), and its orchestration assertions require
  `synthesize_directly` while requiring execute/assess stamps to be absent (`:446-553`).
- The mock preset hardcodes `synthesize_directly` and a matching direct synthesis trace
  (`test/e2e/mock/cmd/main.go:301-369`). A second deterministic full-stack route therefore needs an explicit fixture
  selector; mutating the only preset would erase existing negative coverage.
- The production rule chain already owns both branches. R2 sends `synthesize_directly` to synthesis and sends
  `walk_seeds`/`decompose` to `component.execute_subqueries.*`
  (`configs/rules/research-graph/02-route-decision-dispatch.json:1-84`); R3 sends execute completion to assessment
  (`configs/rules/research-graph/03-execute-assesses.json:1-27`); R4 sends sufficient assessment to synthesis and an
  insufficient assessment through bounded execute refinement
  (`configs/rules/research-graph/04-assess-dispatch.json:1-90`). Focused rule integration already checks all four R2
  actions and the refine dispatch (`processor/rule/research_graph_pipeline_integration_test.go:30-31,223-235,287-312`).
  Production execute invokes `fusion.Fuse` at `processor/research-graph-execute/handler.go:158-202`. The uncovered
  surface is deterministic full-stack evidence through the existing task/tier, not missing runtime rules, subjects,
  payloads, or fusion logic.

#### Corrected collision and adopter consequences

- ADR-063 and the fallback tests are adjacent claims on the same resolution authority. Instance-exact registry
  resolution must leave one authority; retaining the owned store as an unnamed second resolution authority or adding
  a compatibility shim would preserve the collision.
- An external `Storable` producer still owes only the existing exact logical `StorageInstance`. If that name is foreign
  or currently unregistered, the framework observes the miss and takes the existing loud excluded/inline path; the
  producer never predicts a bucket, default store, readiness state, or fallback.
- A clustering adopter configures nothing. A permanent record-local rejection may leave successful candidate writes
  alongside the prior valid partition, but it cannot delete prior keys or advertise a complete cycle. A later complete
  run converges through the existing prune path.
- The research adopter retains the deterministic `synthesize_directly` proof and gains a separate deterministic
  execute/assess/fusion proof inside the existing tier; no new task family, subject, payload, or orchestration rule is
  introduced.

### Every current spelling of the modeled facts

#### Authority and mutation

- `graph/kvcatalog.go:37-74,103-138,154-180` declares 18 framework KV descriptors. `ENTITY_STATES` is authoritative
  with history 1; `GRAPH_STATUS` is operational with history 3.
- `graph/kvcatalog.go:194-245` separates owner-only `EnsureCatalogBucket` from reader-only, must-exist
  `OpenCatalogReader`.
- `processor/graph-ingest/canonical_mutations.go:180-456` admits one `semstreams.graph.mutation/v1` provider with
  create, reconcile, append, and delete.
- `graph/exact_entity.go:15-93` returns an entity and nonzero revision from the same authority entry.
- `processor/graph-ingest/component.go:1818-1823,1845-2004,2007-2109` retains direct birth, streaming merge, and
  internal revision-checked delete. Direct birth performs hierarchy inference; canonical RPC birth intentionally does
  not.

#### Read planes

- `processor/graph-query/query.go:19-69` owns one internal inventory of 16 admitted `graph.query/v1` operations. It is
  registration and conformance truth, not an exported request-subject catalog.
- GraphQL exposes 19 fields, 14 graph-backed. `capabilities`, `similaritySearch`, and `textSearch` are absent;
  `semanticSearch` is the sole semantic spelling.
- `processor/graph-ingest/query.go:20-55` separately exposes four embedded authority responders.
- `processor/graph-index/query.go:20-102` and `processor/graph-embedding/query.go:14-42` expose producer-owned derived
  reads.
- `service/service_manager.go:1188-1200` mounts `/graph/triples`;
  `service/graph_triples_http.go:165-221` scans authority and currently returns empty success without NATS. This is an
  operator diagnostic, not the application-query contract.
- The aggregate `graph/query` client is deleted. `graph.QueryResponse` contains only data and timestamp, and fusion
  and research use the shared response decoder.

#### Derived state and lifecycle

- `processor/graph-index/owner_reconcile.go:18-152` performs bounded, owner-filtered complete replacement.
- `processor/graph-index/predicate_index.go:12-45`, `name_index.go:35-112`, and `incoming_index.go:20-99` implement the
  raw-predicate, hashed-name, and source-owned-incoming layouts.
- `processor/graph-index/component.go:1359-1417,1910-2053` applies replacement and delete cleanup. ALIAS remains a
  distinct class outside owner-complete replacement; `DeleteFromAliasIndex` has no production caller.
- No production `PREDICATE_CATALOG`, `CONTEXT_INDEX`, or `STRUCTURAL_INDEX` surface remains.
- `graph/clustering/summary_store.go:144-180` writes content-addressed `COMMUNITY_SUMMARIES`; the current spec records
  observable accumulation and no GC in this increment at `openspec/specs/graph-clustering/spec.md:342-345`.

### Derived-index and current-state conformance matrix

This inventory was enumerated from `graph.KVCatalog()` and then extended only to process-local state that changes
graph/query semantics. Different physical layouts are compared by authority, ownership, replacement, retraction,
readiness, failure, bounds, rebuild, retention, and consumers—not by superficial key-shape similarity.
`STORAGE_REPORT` is excluded because it is operational capacity evidence rather than graph/query correctness state.
`GRAPH_INGEST_APPLIED_SEQ` and `GRAPH_STATUS` are included because they gate mutation correctness and derived-read
availability. All catalog KV rows use no-lifecycle retention; history is 1 except `GRAPH_STATUS`, whose history is 3:
`graph/kvcatalog.go:37-138`.

| Store/index | Semantic class / purpose | Authoritative input | Physical key / ownership | Writer | Replacement / update | Retraction / delete | Bootstrap / readiness | Poison / watch / loss | Read/write/payload bounds | Rebuild source | Retention | Current consumers | Verdict | Exact finding / owner |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| `ENTITY_STATES` plus graph-ingest entity cache | Authority anchor, not derived | Admitted graph mutation and Graphable ingest | `entityID`; graph-ingest sole writer. Component groups bucket/read-through cache (`processor/graph-ingest/component.go:490-495`) and a per-key invalidation-generation map (`processor/graph-ingest/component.go:548-562`); cache max 5,000, 30s TTL (`processor/graph-ingest/component.go:1072-1083`) | graph-ingest | Create/CAS merge/reconcile; each write bumps the generation guard before cache refill | Revision-fenced delete plus cache invalidation | Startup snapshot validation; graph-ingest status | Poison is per entity; generation guard prevents stale read refill after invalidation | One value per entity; large bodies may offload; generation map grows by distinct IDs ever written | Canonical producers/reseed | KV history 1/no lifecycle; cache TTL; generation map process lifetime | Exact/batch/prefix reads and every derived owner | Conforming authority anchor | Cache state is grouped with authority because it changes query observations; it is not a second source of truth |
| `ENTITY_SUFFIX_INDEX` plus suffix cache | Partial-ID resolver | `ENTITY_STATES` identity | Two raw keys per entity: `instance`, `type.instance`; each stores one ID. Component groups the bucket and TTL cache (`processor/graph-ingest/component.go:490-495,1085-1103`) | graph-ingest and suffix fallback repair | Blind `Put`; cache max 500/5m TTL | Entity delete blindly deletes both keys (`processor/graph-ingest/component.go:2579-2600`) | No independent completeness/readiness currency | Malformed row errors; fallback scans authority and chooses first match (`processor/graph-ingest/query.go:456-613`) | IDs bounded; full fallback scan unbounded; singular value cannot represent collision | Lazy authority scan, not complete boot rebuild | History 1, no lifecycle | `graph.ingest.query.suffix`, graph-query partial resolver | **Bounded finding** | **DI-01, graph-ingest:** same suffix can belong to multiple entities; last write wins, first scan match wins, cache freezes that choice, and either entity's delete removes the shared mapping |
| `GRAPH_INGEST_APPLIED_SEQ` plus lane-local memory tier | Index-adjacent operational correctness: redelivery guard | JetStream stream sequence after successful authority effects | Durable `entityID/streamName → uint64` plus one in-memory `laneGuard` map per keyed lane (`processor/graph-ingest/component.go:606-613`; `processor/graph-ingest/keyed_ingest.go:64-68`) | graph-ingest keyed lane | Memory is the lock-free fast path; durable `Put` follows effects before ack (`processor/graph-ingest/keyed_ingest.go:125-233`) | Memory may evict/restart; durable row has no ordinary delete and survives entity churn | Durable bucket must be available for safe stale detection; read/write failure Naks | Short/corrupt durable value is treated first-seen and restamped (`processor/graph-ingest/keyed_ingest.go:260-299`) | Eight-byte durable value; memory bounded by lane policy; durable key set entity×stream | Memory repopulates from durable checks; durable state follows successful JetStream delivery and is not derivable solely from authority | History 1, no TTL by design (`processor/graph-ingest/component.go:1105-1115`) | graph-ingest only | Conforming two-tier operational exception | Correctness is durable-tier owned; memory retention is optimization only |
| `OUTGOING_INDEX` | Source-owned complete relationship projection | Current entity triples | `entityID → JSON relationship array`; source owns one key | graph-index | Complete replacement, including explicit empty array (`openspec/specs/graph-index/spec.md:88-93`) | Source delete removes owner key (`processor/graph-index/component.go:1910-2053`) | WatchAll replay, watermark, failed-entity repair; `GRAPH_STATUS/graph-index` | Required failure withholds readiness and retries | One value grows with out-degree; NATS payload is the ceiling | `ENTITY_STATES` WatchAll | History 1, no lifecycle | graph-index queries, PathRAG, clustering/anomaly | **Bounded finding** | **#839/#857, graph-index:** lifecycle conforms, but one value scales with entity degree |
| `INCOMING_INDEX` | Source-owned reverse memberships | Current source relationship triples | `targetID.sourceID.hex(predicate) → marker`; source owns despite target prefix (`processor/graph-index/incoming_index.go:20-99`) | graph-index | Owner-filtered complete replacement | Source removal retracts its rows; target removal preserves live-source evidence (`processor/graph-index/component.go:1910-2025`) | Shared graph-index readiness/repair | Poisoned key/filter or required failure fails closed | One bounded membership per key; query result caps are consumer-level | `ENTITY_STATES` WatchAll | History 1, no lifecycle | reverse queries, PathRAG, clustering/anomaly | Conforming | Different target-first physical layout is intentional, not contract drift |
| `PREDICATE_INDEX` | Predicate membership | Current entity predicates | Raw fixed-nine-token `predicate3.entity6 → marker`; entity owner (`processor/graph-index/predicate_index.go:12-45`) | graph-index | Owner-filtered complete replacement | Entity delete/replacement retracts stale memberships | Shared graph-index readiness/repair | Malformed authority latches reset; required write fails closed | One bounded membership per key; query limit enforced | `ENTITY_STATES` WatchAll | History 1, no lifecycle | predicate query, graph-query | Conforming | Raw predicate layout intentionally differs from NAME/INCOMING codecs; no `PREDICATE_CATALOG` |
| `NAME_INDEX` | Ranked human-name membership | Configured name predicates in entity triples | `sha256(normalizedName).entityID.hex(predicate) → {original name, priority}` (`processor/graph-index/name_index.go:35-112`) | graph-index | Owner-filtered complete replacement | Entity replacement/delete retracts owned rows | Shared graph-index readiness/repair | Required failure fails closed | One bounded membership per key; huge shared-name scan returns typed `resource_exhausted` (`openspec/specs/graph-index/spec.md:169-173`) | `ENTITY_STATES` WatchAll | History 1, no lifecycle | by-name query and graph-query | Conforming | Hashed open-content axis is an intentional physical-layout distinction |
| `ALIAS_INDEX` | Exact alias resolver | Configured alias predicates in entity triples | Raw `alias → single entityID`; no owner axis (`processor/graph-index/component.go:1805-1832`) | graph-index | Blind last-writer `Put`; explicitly outside owner-complete replacement (`processor/graph-index/component.go:1359-1381`) | `DeleteFromAliasIndex(alias)` exists, but production search finds no caller; entity delete omits alias (`processor/graph-index/component.go:1910-2053,2103-2133`) | Alias writes participate in graph-index failure readiness, but absence/staleness is not owner-complete | Malformed rows are read as plain ID; no collision state | Single bounded value; raw alias must satisfy KV literal grammar | Replay can add current aliases but cannot identify/retract historical aliases | History 1, no lifecycle | alias query, graph-query/GraphRAG resolution | **Bounded finding** | **DI-02, graph-index:** same-alias last-writer collision, stale alias after predicate/entity removal, and unreachable production delete helper |
| `SPATIAL_INDEX` | Geohash-cell membership and coordinates | Coordinate triples in `ENTITY_STATES` | `geohash → JSON map(entityID→position)`; spatial component owns | graph-index-spatial | CAS cell RMW adds/updates current entity. Malformed existing aggregate JSON is treated as an empty cell, then normal `Update` rewrites that same revision (`processor/graph-index-spatial/component.go:782-823`) | Delete handler only logs “not fully implemented”; coordinate removal or cell move does not retract old cell (`processor/graph-index-spatial/component.go:668-715,834-838`) | Local WatchAll bootstrap sentinel; typed `index_not_ready`; no `GRAPH_STATUS` producer | Authority poison/watcher loss fails local reads closed, but malformed stored cell JSON does not poison: it takes the empty-cell rewrite path | Cell value grows with occupancy; bounds/polygon queries have result limits | `ENTITY_STATES` WatchAll, but replay does not remove orphan rows or restore members erased by aggregate rewrite without their redelivery | History 1, no lifecycle | spatial component queries and graph-query | **Bounded release-truth finding** | **DI-03, graph-index-spatial:** stale rows survive moves/removal/delete; malformed aggregate rewrite can silently erase every other cell member; #857 cell-value ceiling remains. No runtime authority in Option 2 |
| `TEMPORAL_INDEX` + `TEMPORAL_INDEX_REVERSE` | Time-bucket membership plus entity→current-bucket retraction aid | Observation time, then `UpdatedAt` fallback | Forward `timeBucket → JSON entity map`; reverse `entityID → timeBucket`; temporal component owns | graph-index-temporal | New forward row is written first. Malformed existing aggregate JSON is treated as an empty bucket, then normal `Update` rewrites that same revision with only the current event (`processor/graph-index-temporal/component.go:872-930`) | Reverse lookup normally drives entity delete/moved-bucket cleanup, but stale-row writes and reverse writes/deletes log/metric and fail open (`processor/graph-index-temporal/component.go:942-1057`) | Local WatchAll bootstrap; typed not-ready; no `GRAPH_STATUS` producer; aggregate rewrite and cleanup drift do not withhold readiness | Authority poison/watcher loss fails local queries closed, but malformed stored bucket JSON and cleanup/reverse failures do not | Forward bucket value grows with occupancy; reverse value bounded | `ENTITY_STATES` WatchAll; lost reverse state can strand forward rows, and erased aggregate members return only if their authority rows redeliver | History 1, no lifecycle | temporal range query and graph-query | **Bounded release-truth finding** | Reverse layout is justified, but malformed aggregate rewrite can silently erase other bucket members and cleanup failures can strand rows without readiness withholding; #857 remains. No runtime authority in Option 2 |
| `EMBEDDING_INDEX` plus process-local vector cache | Per-entity pending/generated/failed embedding state and similarity view | Entity projection plus optional offloaded body | `entityID → embedding.Record`; graph-embedding owns; memory vector cache mirrors generated rows | graph-embedding worker/storage | Revision-aware pending→terminal transition; entity identity is replacement axis | Tombstone/no-source removes entity embedding (`graph/embedding/storage.go:635-645`) | WatchAll bootstrap, watermark, repair/failed state; `GRAPH_STATUS/graph-embedding` | Vector-cache watcher loss invalidates cache; poison/read failure fails closed | Vector dimensions/config and source-text truncation bound individual records | `ENTITY_STATES` plus resolvable body references | History 1, no lifecycle | embedding query, graph-query, clustering semantic edges | **Bounded finding** | **#875, graph-embedding:** named `StorageInstance` miss can fall back to unrelated legacy store and poison failed/degraded outcome (`processor/graph-embedding/component.go:1934-1954`; `graph/embedding/worker.go:981-1007`) |
| `EMBEDDING_DEDUP` | Durable accumulated content-key cache, not current-state projection | Exact source text plus embedder identity at generation time | `DedupKey(identity,text) → vector plus accumulated entity-ID list`; graph-embedding owns. The key is durable, untimed, and never cleared (`graph/embedding/dedup.go:18-39`) | graph-embedding | Same content/identity reuses the vector and appends entity IDs | Entity deletion removes only `EMBEDDING_INDEX`; it never retracts dedup keys or entity-ID lists (`graph/embedding/storage.go:574-648`) | Used inside embedding worker; no independent readiness | Corrupt read surfaces storage error; no public watch | One vector and growing entity-ID list per unique content+identity; total cardinality has no reclamation policy | Not rebuilt as current state; old keys remain reusable until external reset | History 1, no lifecycle/TTL/clear | graph-embedding worker | **Bounded finding** | **#619, graph-embedding:** BM25 vector depends on process-local corpus state absent from durable identity; independently, dedup is accumulated historical cache rather than entity-current state |
| BM25 corpus statistics, process-local | Statistical state that changes produced/query vectors | Order of document `Generate` calls | In-memory `docCount`, average length, term-document map (`graph/embedding/bm25_embedder.go:59-70`) | BM25 embedder | `Generate` mutates incrementally; `GenerateQuery` is read-only | No document retraction | Empty on process start; no readiness/corpus-generation currency | Process loss silently resets; no poison channel | Term map grows with observed vocabulary; no persisted bound | Re-observation order only, not immutable snapshot | Process lifetime | graph-embedding and semantic/BM25 query | **Bounded finding** | **#619, graph-embedding:** unpersisted, order-dependent corpus; restarts and replay order can change vectors/rankings (`graph/embedding/bm25_embedder.go:106-140`) |
| EntityID type/system caches, process-local | Virtual sibling/system-peer topology used by clustering | Current entity-ID enumeration from wrapped provider | In-memory `typePrefixCache` and `systemCache`, built once under `cacheInitialized` (`graph/clustering/entityid_provider.go:26-50,333-395`) | clustering `EntityIDProvider` | First access builds sorted capped candidate lists | `ClearCache` exists but no automatic authority-watch invalidation is in this lifecycle (`graph/clustering/entityid_provider.go:498-510`) | No readiness or revision currency | Process restart clears; otherwise entity churn does not invalidate | Memory proportional to entity IDs grouped by type/system; configured candidate caps affect returned edges | Full entity-ID enumeration only when cache is empty | Process lifetime until explicit clear | LPA sibling/system virtual edges | **Bounded finding** | **#672, graph-clustering:** entity additions/removals can leave lifetime-stale sibling/system candidate sets |
| Mutual-kNN semantic cache, process-local | Revision-keyed semantic virtual-edge projection for clustering | Similarity results plus coarse embedding-index revision | Directed top-k and symmetric mutual-neighbor maps keyed by committed `cacheRevision`, with per-cycle settlement state (`graph/clustering/semantic_edge_provider.go:202-323`) | clustering `SemanticEdgeProvider` | `BeginCycle` advances epoch; unchanged revision reuses results; missing/errored entities are refreshed | Next-cycle/revision refresh replaces maps; process loss clears | Embedding readiness controls activation; not-ready/fatal aborts cycle, coverage abort keeps prior good cache or degrades | Abort/coverage state is cycle-scoped; a settled decision prevents same-cycle query storms (`graph/clustering/semantic_edge_provider.go:641-669`) | O(N) directed candidates bounded by k; refresh threshold bounds partial error acceptance | Similarity queries against embedding index | Process lifetime, revision/cycle scoped | LPA semantic virtual edges and applied-edge metrics | Conforming process-local projection | Revision and cycle state are material graph inputs; they are intentionally not durable membership state |
| Same-cycle K-core/pivot plus retained `previousKCore` | Structural/anomaly inputs | Current graph provider snapshot per detection cycle | Ephemeral `KCoreIndex` and `PivotIndex` computed together (`processor/graph-clustering/structural.go:13-51`); component retains prior K-core | graph-clustering | Recompute both each cycle; after successful anomaly detection, assign current K-core to `previousKCore` | Same-cycle indices drop after use; retained prior index is replaced only after successful detection | No distributed readiness of its own; enclosing cycle owns failure | Restart loses prior K-core; failed anomaly run leaves previous successful baseline | Memory proportional to current graph; pivot count fixed by structural default | Graph provider snapshot each cycle; previous comparison requires prior successful cycle | Same cycle, plus one successful prior K-core in process | Anomaly detectors, especially core demotion | **Observed process-local behavior** | Restart has no previous-cycle demotion baseline until another successful cycle (`processor/graph-clustering/anomaly.go:272-322`); record as release truth, not implicit runtime authority |
| `COMMUNITY_INDEX` | Current multi-level partition plus entity membership map | Periodic graph-index relationships and optional embeddings | `{level}.{communityID} → Community`; `entity.{level}.{entityID} → communityID` (`graph/clustering/storage.go:21-29`) | graph-clustering detector | Save candidate communities then prune keys outside saved set | `Prune` deletes all keys not represented in supplied partition (`graph/clustering/storage.go:387-468`) | Consumes graph-index and optional embedding readiness; graph-query has operation-local generation; no clustering status key | Individual malformed reads may be skipped; storage failures abort/degrade cycle | Community value contains unbounded member list; NATS max payload is explicit permanent failure (`graph/clustering/storage.go:108-150`) | Periodic whole recomputation | History 1, no lifecycle | clustering queries, graph-query community generation, anomaly detection | **Bounded finding** | **#855, graph-clustering:** partial saved set after permanent oversize rejection can drive destructive prune; **#839/#857** capacity remains |
| Clustering whole-view authority-poison latch, process-local | Sticky safety state for the entire derived clustering view | Validating reads of authoritative `ENTITY_STATES` during polled detection/enhancement | Atomic `graphStatePoison`; it is not a `COMMUNITY_INDEX` record or malformed-row marker | graph-clustering consuming read path | First authoritative `StateContractError` latches; later valid authority cannot clear it (`processor/graph-clustering/component.go:1707-1730`) | Same-instance Stop/Start does not clear; operator reset plus process restart is required | Start retains query handlers for typed reset response but blocks detector, enhancement, and action workers when latched (`processor/graph-clustering/component.go:1006-1012`) | Every clustering query returns fatal reset-required; this is distinct from individual malformed `COMMUNITY_INDEX` record handling | One process-local pointer; no payload/cardinality growth | Canonical authority reset/reingest followed by process restart | Process lifetime, deliberately across same-instance Stop/Start | detector/enhancement/action workers and clustering query handlers | **Conforming to current specification** | `openspec/specs/graph-clustering/spec.md:353-367` requires the sticky whole-view reset latch. This is release truth, not a new runtime finding |
| Graph-query community generation | Atomic process-local projection of `COMMUNITY_INDEX` | `COMMUNITY_INDEX` WatchAll | In-memory maps per independent generation (`processor/graph-query/community_cache.go:32-68`) | graph-query watcher | Build fresh generation, publish only after initial sentinel; old leases remain isolated | Delete/purge updates remove community/mappings (`processor/graph-query/community_cache.go:200-324`) | Operation-local availability; missing generation returns typed `index_not_ready` | Watch loss unpublishes current generation; restart builds a new one | Memory proportional to current community rows; query caps remain operation-specific | `COMMUNITY_INDEX` watch | Process lifetime | global/local search, summaries and community enrichment | Conforming process-local projection | Distinct generation state is justified; do not add clustering `GRAPH_STATUS` merely to replace it |
| `COMMUNITY_SUMMARIES` | Content-addressed LLM prose cache, separate from detector partition | Exact `(level, membershipHash)` plus enhancement outcome | `{level}.{sha256(sorted members)} → summary record`; enhancement worker sole writer (`graph/clustering/summary_store.go:28-69`) | graph-clustering enhancement worker | Success overwrites failed/same success; failed CAS cannot downgrade enhanced (`graph/clustering/summary_store.go:75-105,144-225`) | No current GC by declared increment | Optional; graph-query falls back to statistical summary | Poisoned row warns; watcher/bucket loss detaches view and degrades to statistical summary (`processor/graph-query/summary_view.go:108-195`) | One bounded generated summary per historical membership; total cardinality accumulates | Regenerate only when exact membership recurs | History 1, no lifecycle; no GC currently | graph-query summary graphview | **Justified semantic exception** | No-GC is explicitly current truth; #710 owns future reclamation and must not be folded into #855 |
| Graph-query summary graphview | Optional process-local mirror of summaries | `COMMUNITY_SUMMARIES` WatchAll | Typed in-memory view keyed identically | graph-query view | Atomic view updates | KV tombstone/purge removes row | Optional attach/rebind; absence is allowed | Poison warns/coalesces; watch loss clears view and retries | Memory proportional to summary bucket | `COMMUNITY_SUMMARIES` | Process lifetime | graph-query community summary join | Conforming degradation view | No separate generic helper needed; it already uses `pkg/graphview` |
| `ANOMALY_INDEX` | Durable anomaly and human/LLM review lifecycle state plus physical secondary/suppression indexes | Periodic detection and review outcomes | Primary records include pending/reviewed/applied/dismissed lifecycle (`graph/inference/types.go:31-114`); status/type indexes and dismissed-pair/entity suppression keys share the bucket | graph-clustering detector/review worker | Revision-aware primary update; status/type indexes maintained, while suppression keys prevent re-detection (`graph/inference/storage.go:172-218,570-645`) | `Delete` ignores status/type-index failures and never removes pair/entity suppression keys; `Cleanup` has no production caller (`graph/inference/storage.go:371-414,480-568`) | Optional component feature; no independent distributed readiness | Review worker uses WatchAll; startup watch failure returns error | `Count` trusts physical status-index keys, so ignored delete drift changes operator counts (`graph/inference/storage.go:570-606`) | Detection can recreate candidates, but not review history; suppression keys deliberately alter future detection | History 1, no catalog lifecycle | review worker, clustering query, optional inference-review gateway | **Uncertain bounded finding** | **DI-04 candidate, graph-clustering/inference:** missing cleanup scheduling, ignored secondary deletes, permanent suppression keys, and physical-key counts are proven. Whether suppression must survive primary cleanup is not specified; owner adjudication is required before calling it nonconforming |
| `GRAPH_STATUS` plus producer-local readiness state | Index-adjacent operational readiness/liveness distribution | Producer-local computed state | Four explicit durable keys: graph-index, graph-embedding, graph-ingest, rule (`graph/readiness/watcher.go:39-70`), backed by per-process watermark, failure, reset, watch, and bootstrap state | Each named producer owns its key and local projection state | Heartbeat `Put` every tick; `pkg/revlag.Watermark` tracks observed/completed revision floor (`graph/readiness/publisher.go:74-105`; `pkg/revlag/watermark.go:23-65`) | No normal durable deletion; process-local state resets at restart and silence ages to unknown | Graph-index groups watermark, failed entities, reset, and bootstrap latches (`processor/graph-index/component.go:266-346`); embedding groups failed map, watermark, reset/watch, and bootstrap latches (`processor/graph-embedding/component.go:311-365`) | Missing/malformed/lost/stale feed becomes unknown and fails closed; producer-local failure state withholds ready before publication | Small durable envelope; 2s write timeout; 5s heartbeat/3× freshness; local maps scale with current failures/pending revisions | Recomputed from producer state and authority replay | Durable history 3, no lifecycle; local state process lifetime | fusion, clustering, query/readiness gates, operators | Conforming operational classification | Durable status cannot be interpreted apart from its producer-local revlag/failure/bootstrap projection; do not infer a global producer registry or require clustering/spatial/temporal keys |

### Matrix collision and layout conclusions

The newly visible same-class collision defects are `ENTITY_SUFFIX_INDEX` and `ALIAS_INDEX`: both reduce a potentially
many-owner lookup to one last-writer value, but their public semantics differ. Suffix guessing, exact alias resolution,
NAME membership, incoming reverse membership, temporal reverse placement, content-addressed summaries, and anomaly
suppression are not interchangeable merely because several use reverse keys.

These distinct physical layouts are not contract drift:

- raw fixed-nine-token predicate membership;
- hashed NAME open-content axis;
- target-prefixed but source-owned INCOMING membership;
- forward temporal bucket plus entity-owned reverse placement;
- community partition rows separate from content-addressed summary rows; and
- anomaly primary records plus status/type/suppression indexes.

The adopter-seam result is to add no generic index interface, owner-reconcile helper, readiness registry, or
reverse-index abstraction in this closeout. At least two components share WatchAll mechanics, but current consumers
already use the narrower `pkg/graphview` and `pkg/revlag` primitives where their semantics match; the lifecycle
contracts above remain materially different.

#### Hierarchy, research, retention, and trajectory evidence

- `graph/inference/hierarchy.go:144-255,278-397,420-463` creates containers and inverse/sibling edges as write-side
  effects. `OnEntityCreated` is legacy and has no production caller. The only non-test direct `CreateEntity` paths are
  the graph-ingest adapter and hierarchy-container recursion.
- `frameworkcapabilities/graphresearch/register.go:127-233` validates graph research as one atomic capability bundle.
  `executor.go:343-364` requires parent metadata; kickoff birth failure is suppressed at `:381-393`, after which
  accepted dispatch and `StopLoop` return at `:395-418`. A stalled chain is therefore currently observable.
- Current retention truth is no live graph/ObjectStore eviction, with an inactive-owner backstop at
  `graph/owned_bucket_retention.go:14-82`, acquisition reconciliation at `natsclient/kvspec.go:216-321`, and generic
  writer rejection at `processor/rule/config_validation.go:363-367` and `actions.go:1926-1943`.
- There is no production `RetentionParticipant`, semantic delete-retention mode, tombstone GC, reference-aware blob
  collector, or hard-purge worker. `storage/objectstore/store.go:338-375` exposes explicit deletion, but no graph
  reference-aware collector calls it.
- `agentic/trajectory_fact.go:16-25,117-174` keeps immutable, body-free trajectory facts under 8 KiB with an optional
  `StorageReference`; `processor/agentic-loop/trajectory_recorder.go:100-224` resolves evidence through
  `StoreRegistry`.
- `message/storable.go:16-19,38-64` defines `StorageReference.StorageInstance` as the backend selector and admits
  `Storable` producers. Those producers are present consumers of the selector; ADR-062 specifically records
  SemSource's deliberate filestore use and the backend-independent selector contract at
  `docs/adr/062-deterministic-graph-fusion.md:102-120,194-198`.
- Graph-ingest lifts a `Storable` reference onto `EntityState` for authority persistence at
  `processor/graph-ingest/component.go:1663-1674`; the deterministic extraction regression is at
  `processor/graph-ingest/fact_lane_guards_test.go:131-152`.
- `processor/graph-embedding/component.go:1934-1954` first checks `StoreRegistry`, but returns true whenever any legacy
  content store is wired. `graph/embedding/worker.go:981-1007` then falls back to that store after a registry miss
  without proving it owns the requested `StorageInstance`. A foreign unresolved reference can therefore open the
  wrong store, fail, and pin embedding failed/degraded. #875 is a current foundation finding.
- `storage/objectstore/store.go:734-795` now gives every `DefaultKeyGenerator` write a UUID nonce, and
  `storage/objectstore/store_test.go:133-153` plus `component_write_key_integration_test.go:9-73` prove distinct
  same-second raw writes. #741's collision premise is stale.

#### Readiness, services, tests, and release

- `graph/readiness/watcher.go:39-109` declares four explicit producer keys: graph-index, graph-embedding,
  graph-ingest, and rule. There is intentionally no framework-global producer list.
- Clustering consumes index and optional embedding readiness at
  `processor/graph-clustering/component.go:601-607,1362-1397`. It has no `GRAPH_STATUS` producer; graph-query uses a
  valid process-local community generation and typed `index_not_ready`.
- `service/service_manager.go:78-250,295-404` seals the service set before startup, handlers, and OpenAPI. An omitted
  optional service has no instance, route, or OpenAPI; configuration changes describe next-boot state.
- `.github/workflows/e2e-ladder.yml:3-25,40-57` runs statistical E2E on pull requests. Semantic E2E remains a manual
  pre-tag requirement. `.github/workflows/sister-validation.yml:1-28,46-167` is manual and representative, while
  `.github/workflows/release.yml:1-83` publishes after tag push and does not prove the pre-tag candidate.
- The policy comments at `.github/workflows/e2e-ladder.yml:3-8` and
  `.github/workflows/sister-validation.yml:19-25` still describe sister lockstep/tag-time break detection. A bounded
  truth cleanup must propagate the current rule: sisters are non-exhaustive optional evidence, and downstream
  adoption begins after the stable pin. This changes comments only, not workflow triggers or jobs.
- The five task-wrapper families formerly implicated by #811 now preserve the scenario exit code and teardown; no
  live `ignore_error: true` remains.
- Honest task wrappers expose rather than resolve deterministic red tiers. #301 crud-tools and the #844/#860
  ops/rule paths must either become green or receive an explicit owner-approved fold/delete decision with their
  required coverage transferred before the stable tag. The inventory does not choose fix versus fold/delete.
- The G closeout evidence records lint, full race, integration, schema/no drift, contracts, strict OpenSpec 40/40,
  statistical, semantic, agentic, research, and deep-research gates, including an actively monitored semantic replay.
- `openspec/changes/semantic-tier-split` remains suspended and frozen. Its #830, #819, and #811 premises are partly
  stale; #829 and generation-quality questions remain current.

### Adjacent claims on the territory

The fixed issue census is the adjacent-claim inventory. The most important boundaries are:

- #855 is a present contract violation and belongs in tag safety.
- #875 is a present selector-resolution violation and belongs in tag safety; its correction is bounded and does not
  authorize a generic storage redesign.
- #391 is a deterministic in-repository coverage obligation and belongs in tag safety.
- #301 and #844/#860 are deterministic advertised-tier release obligations. Each needs green evidence or an
  owner-approved fold/delete with coverage transfer.
- #828 is stale architectural truth and belongs in tag safety.
- #839 is a current measured capacity limit that can cross the tag only as an explicit owner-accepted release
  limitation. #857 is the broader payload-size class and remains separate follow-on work.
- DI-01 suffix collision/retraction, DI-02 alias collision/retraction, and DI-03 spatial stale-row plus malformed-cell
  aggregate erasure are current bounded findings discovered by the conformance pass. They are not subsumed by
  #855/#875 and are not implementation-authorized by this artifact.
- #619 spans both process-local BM25 corpus state and durable dedup reuse because corpus state is absent from the
  durable identity. The durable store is an untimed, never-cleared accumulation of content keys and entity-ID lists,
  not a current-state rebuild output.
- DI-04 anomaly lifecycle remains explicitly uncertain: missing production cleanup, ignored secondary-index deletion,
  suppression keys that are never removed, and counts derived from physical status keys are observed;
  suppression-retention intent still needs owner adjudication.
- #672's lifetime type/system caches and temporal's fail-open reverse cleanup plus malformed-bucket aggregate erasure
  are additional current-state findings surfaced by the completed process-local inventory. Neither is
  runtime-authorized by Option 2.
- Clustering's sticky whole-view authority-poison latch is conforming current behavior, not a new finding. It survives
  same-instance Stop/Start and blocks all clustering work until reset/restart; malformed `COMMUNITY_INDEX` row handling
  remains a separate record-local behavior.
- `STORAGE_REPORT` was considered and excluded because it reports capacity rather than determining graph/query
  answers.
- #633/#710 reclamation, generalized readiness, hierarchy redesign, and startup hardening are distinct programs or
  measurement-triggered work.
- #829 remains a declared summary-quality limitation unless the owner separately makes generated-summary quality a
  release gate.
- The suspended semantic-tier change is not implementation authority. It receives a premise-status annotation only.

### Consumer at birth

The recommended closeout introduces no new exported symbol, port, subject, bucket, config field, or public query
operation. Its consumers already exist: community replacement/prune, `Storable` producers, graph-ingest,
graph-embedding operators, the existing research/fusion capability, current documentation readers, and release
adopters. Searches represented in the inventory found no production
`PREDICATE_CATALOG`, `CONTEXT_INDEX`, `STRUCTURAL_INDEX`, `RetentionParticipant`, semantic tombstone collector, global
readiness list, or aggregate query client. None is added for a future consumer.

## Same-class collision result

| Dimension | Inventory result |
|---|---|
| Semantic class | Authority, admitted mutation, operation inventory, producer-owned derived reads, GraphQL, operator diagnostics, index partitions, summary cache, storage-reference resolution, readiness, ObjectStore lifecycle, research composition, and service composition remain distinct classes. |
| Owners | One authority (`ENTITY_STATES`), one admitted mutation provider (graph-ingest), owner-filtered derived indexes with ALIAS distinct, community partition owner distinct from summary-cache owner, `StorageInstance` selected stores registered by instance, producer-keyed readiness, and one atomic graph-research bundle. |
| Catalogs | `graph.KVCatalog()` is the framework KV catalog; `graphQueryOperations` is internal registration/conformance truth. No parallel public subject catalog is justified. |
| Status | Readiness is producer-keyed and consumer-selected. Community query availability is operation-local; there is no clustering `GRAPH_STATUS` producer. An unresolved foreign storage instance is exclusion, not embedding failure/degradation. |
| Lifecycle | Community replacement must be complete before prune; summary accumulation currently has no GC; reference resolution occurs per fetch; ObjectStore deletion remains distinct; services seal once and change on restart. |
| Ownership | Authority and derived writers are explicit. ALIAS is outside owner-complete replacement. Community partitions and content-addressed summaries have different ownership. A store owns only references naming its registered instance. |
| Readers | Exact authority, embedded authority, producer-owned derived, GraphQL, and diagnostic readers are intentionally separate. Graph embedding is a `StorageReference` reader and must resolve the named instance. |
| Writers | Canonical mutation RPC is the admitted provider; graph-ingest persists refs supplied by admitted `Storable` producers. No new writer is proposed. |
| Recovery | Current state recovers through KV watch/rebuild and owner reconciliation. The recommended closeout adds no checkpoint, recovery service, or backup primitive. Operators retain normal NATS backup responsibility. |

Consolidation is already achieved for owner-filtered graph-index memberships, but the exhaustive pass found two
singular-lookup collision classes: suffix and alias. Spatial lacks current-owner retraction; spatial and temporal can
replace malformed aggregates as empty and erase other members; temporal cleanup can fail open; and EntityID
type/system caches can outlive authority changes. These findings do not justify one generic reverse-index or lifecycle
abstraction: their adopters, ambiguity semantics, ownership axes, and recovery sources differ. The approved Option 2
remains bounded unless the owner separately promotes one of these findings.

## Adopter seam inventory

| Adopter | What they must know now | If they do nothing | Where they find out | What they should have to know |
|---|---|---|---|---|
| External mutation component | Use the narrow projection mutation capability; carry the exact revision for reconcile/delete. | No mutation, or classified conflict/unavailable. | Projection and graph-ingest contracts; migration guide. | No subject, bucket, hierarchy, or retry prediction. |
| Remote query caller | Use admitted GraphQL fields. | Classified unavailable, `index_not_ready`, or explicit degradation. | GraphQL schema and graph-query spec. | No internal subjects or cache generations. |
| Embedded query component | Declare the exact operation consumed. | Startup or operation fails explicitly when its responder is absent. | Component ports and operation contract. | No general graph client. |
| `/graph/triples` operator | Treat it as a diagnostic snapshot, not readiness or application-query authority. | It currently returns empty success without NATS. | Operator route documentation. | An explicit diagnostic-availability result. |
| Bucket reader or owner | Readers open must-exist; owners ensure/reconcile catalog descriptors. | A reader never creates; missing owner is explicit/not ready. | KV catalog and retention/index specs. | No layout or retention prediction. |
| Derived-view consumer | Declare exact readiness keys or observe operation-local generation. | Fail closed or receive a typed transient result. | Readiness and query specs. | No inference from age, missing keys, or a global list. |
| Service adopter | Select services at boot. | Omission means no instance, route, or OpenAPI; changes wait for restart. | Service-composition config/spec. | No hot mutation of the running service set. |
| Content/trajectory reader | Resolve `StorageInstance` through the registry and stream the body. | Fact metadata remains readable; body is unresolved. | Response-bounds and trajectory contracts. | No bucket, chunk, or payload-limit guessing. |
| `Storable` producer | Stamp the registered backend instance in `StorageReference.StorageInstance`; SemSource may use its filestore rather than ObjectStore. | Graph-ingest persists the exact handle; consumers must not reinterpret it. | `message.Storable`, ADR-062, and storage component contract. | No knowledge of embedding's fallback store or downstream reader wiring. |
| Embedding operator | Register the store instance that owns each resolvable body; treat an unresolved foreign instance as explicit exclusion. | The body is excluded loudly while entity processing continues; it never poisons failed/degraded readiness. | Embedding metrics/log and storage registration contract. | No need to duplicate content into a legacy default store. |
| Partial-ID caller | A suffix can resolve to one silently selected ID and is not collision-safe. | Resolution may vary with write/cache/scan order. | Graph-ingest suffix contract and release limitation. | Ideally nothing: use canonical identity or an explicit ambiguity-aware discovery operation. |
| Exact-alias caller | Current alias storage is singular last-writer state and does not retract owner-completely. | A collision or retired alias may resolve to the wrong entity. | Graph-index alias contract and release limitation. | Exact alias plus absent/singular/ambiguous result; no KV key grammar. |
| Spatial caller | Rows may outlive movement/removal/delete, and malformed cell JSON is rewritten as an empty aggregate plus the current entity. | Bounds/polygon queries may include stale candidates or silently lose other cell members. | Spatial query contract and explicit release limitation. | No geohash/cleanup knowledge; the framework should return current-authority results without destructive repair. |
| Temporal caller | Reverse cleanup can fail open, and malformed bucket JSON is rewritten as an empty aggregate plus the current event. | Range results may include a stranded prior row or silently lose other bucket members. | Temporal query contract and explicit release limitation. | No forward/reverse-key knowledge; the framework should return current-authority results without destructive repair. |
| BM25 caller | Rankings depend on process-local corpus observation order. | Restart/replay order can change vectors and ranking. | Embedding capability limitation / #619. | A stable declared lexical contract, not corpus internals. |
| Clustering consumer | Virtual sibling/system edges can use a lifetime cache; semantic and structural comparison state has process/revision/cycle lifetimes. | Entity churn or restart can change the graph presented to clustering/anomaly detection. | Clustering capability limitations / #672 and matrix. | Declared consistency and restart behavior, not cache or watermark internals. |
| Clustering operator after authority poison | Perform canonical reset/reingest and restart the process. | Same-instance Stop/Start stays latched; workers remain blocked and queries return fatal reset-required. | Typed query outcome, logs, and graph-clustering spec. | The reset requirement and affected view; no `COMMUNITY_INDEX` row-repair guesswork. |
| Research parent | Supply loop identity and parent-role metadata; observe classify completion. | Accepted dispatch can stall after suppressed birth failure. | Graph-research contract and outcomes. | No rule subjects or KV keys; parent-role metadata remains debt. |
| Downstream repository | Pin the exact tag, then compile, migrate config, validate flows, and run product E2E. | It safely remains on its prior pin. | Release notes and migration guide. | No aliases, shims, or framework redesign for unknown holdouts. |

## Options considered

### Option 0: tag the current baseline

Use the G closeout as sufficient and accept #855, #875, advertised red tiers, and the #391 full-fusion coverage gap.
This is the lowest effort but
would call the foundation stable while one derived-state path contradicts its current non-destructive contract and
the candidate does not exercise a named post-G research path. Not recommended.

### Option 1: release proof only

Correct stale documentation and issue truth, rerun a subset of candidate gates, execute #827, and tag without runtime
change or red-tier disposition. This improves release hygiene but knowingly retains #855/#875 and advertised red
behavior. Not recommended.

### Option 2: small post-G tag-safety closeout

1. Correct ADR-068/#828, annotate the frozen semantic-tier change with present premise status without unfreezing it,
   and record stale/overlapping issue dispositions in the frozen artifact. Administrative issue closure or
   reclassification is authorized only after owner approval and only as a bounded release-truth action; it does not
   authorize taking issue work generally.
2. Fix #855 so an incomplete community partition cannot drive prune or report a complete successful cycle. Preserve
   prior valid state as a stale superset. Add permanent-failure regression coverage. Do not solve #839's general
   payload/chunking problem here.
3. Fix #875 with instance-aware resolution: use only the store registered for the reference's `StorageInstance`; a
   foreign unresolved instance takes the existing explicit excluded path and never marks the entity failed or the
   embedding producer degraded. Add a focused deterministic regression. Do not redesign generic storage.
4. Add a deterministic research `execute_subqueries` E2E branch that invokes `fusion.Fuse` and asserts execute,
   assess, nonzero evidence, and terminal synthesis. Retain `synthesize_directly` as separate orchestration coverage.
5. For every advertised deterministic red tier, specifically #301 and #844/#860, either make the path green or obtain
   an explicit owner-approved fold/delete decision and transfer its required coverage. Do not pre-decide the outcome.
6. Correct the two stale workflow comment blocks without changing workflow behavior, then freeze one exact candidate,
   prove it, review it, publish its breaks and owner-accepted known limits, execute the coordinated
   pre-v1 wipe/reseed, and tag that exact SHA.

The conformance-matrix findings DI-01 through DI-04, #619, #672, spatial/temporal malformed-aggregate erasure,
temporal cleanup, and payload bounds are inventory truth, not implicit additions to this runtime closeout. Only #855
and #875 are authorized runtime slices by this ruling. A separately approved amendment is required to promote
another matrix finding. The clustering authority-poison latch is conforming current-spec behavior, not an additional
runtime slice.

This option adds no shim, public query surface, generic client, readiness abstraction, or retention abstraction.
Recommended.

### Option 3: broad pre-tag hardening

Also solve #839/#857 payload scaling generally, #633/#710 reclamation, generalized readiness, #829 summary content,
hierarchy, and operational startup concerns. This mixes proven tag-safety work with separate-owner or
measurement-gated designs, and asks this repository to predict production behavior it cannot isolate. Not recommended
as the next program.

## Deterministic proof versus production evidence

SemStreams must prove locally deterministic framework behavior: unit and race tests, integration contracts, generated
schema stability, strict OpenSpec, focused regressions, bounded benchmarks where inputs and outcomes are owned here,
and deterministic E2E paths over the repository's own stack. A failure in those gates is SemStreams work.

Deployment-specific throughput, fleet sizing, large real-world graph distributions, account/cluster topology, long
retention economics, and product-quality parity are not honestly proven by inventing synthetic production claims in
this repository. They remain deferred until measured sister-project evidence identifies a framework-level defect or a
bounded benchmark can reproduce it. This is not permission to ignore a reproducible defect; it is a bar against
shipping speculative machinery without an observable premise.

## Tag-readiness criteria

The stable downstream pin is eligible only when:

1. One clean exact SHA is named, with generated schemas and specs clean.
2. #855 cannot let an incomplete partition prune prior valid state or report a complete successful cycle.
3. #875 resolves only the named registered instance; a foreign unresolved instance takes the explicit excluded path,
   never failed/degraded, with a focused deterministic regression green.
4. ADR-068, the frozen semantic-tier status, and the two workflow comment blocks no longer assert superseded premises.
5. The deterministic full-fusion research E2E path is green, alongside the retained direct-synthesis branch.
6. Every advertised deterministic red tier, specifically #301 and #844/#860, is green or has an explicit
   owner-approved fold/delete decision with its required coverage transferred.
7. Lint, full race, integration, schema/no drift, contracts, strict OpenSpec, and focused affected-package tests pass.
8. Statistical, semantic, agentic, deep-research, and both research-graph branches pass on the exact candidate;
   semantic is actively monitored using readiness, counters, and stage progress.
9. Independent SemStreams review approves the complete exact-candidate diff.
10. Release notes name every clean break and owner-accepted known limitation, including #839 and #829 if they remain
    unresolved.
11. #827's coordinated wipe/reseed is scheduled at the tag boundary. If a v1 tag closes that window first, tagging
   halts and the work becomes an explicit migration.
12. The published tag points to the approved SHA, and binary/container artifacts plus reported version are verified.
13. Downstreams can pin that exact tag and then own compilation, config migration, adoption, and parity proof.
14. Every matrix finding outside approved Option 2—including spatial/temporal malformed-aggregate erasure—has an
    explicit owner disposition: accepted release limitation, separately approved blocker, or deferred owner
    programme. “Inventory recorded” alone is not equivalent to “conforming.”

Representative downstream holdouts are optional evidence, not an exhaustive gate. A holdout blocks only when it
reproduces a framework contract regression in the candidate. Adoption debt is recorded after pinning and never causes
a shim or deprecated surface.

## Owner-approved rulings

The owner approves revised Option 2 and the derived-index/current-state conformance matrix as the frozen evidence
base. The matrix corrects release truth but does not broaden runtime authority: #855 and #875 remain the only approved
runtime correction slices in this closeout. No generic index, reverse-index, lifecycle, readiness, or rebuild
abstraction is authorized. Suffix, alias, spatial/temporal malformed-aggregate erasure, temporal cleanup,
EntityID-cache/#672, BM25/#619, payload-bound, and uncertain anomaly findings require explicit release disposition or
a separately approved amendment. The clustering whole-view authority-poison latch is accepted as conforming current
specification and remains distinct from malformed `COMMUNITY_INDEX` rows. Distinct physical layouts remain valid
where semantic ownership differs. A shared interface or helper may be proposed only when at least two same-class
implementations duplicate the same contract and present consumers materially benefit.

1. Option 2 is the smallest next program and the goal is a stable downstream pin.
2. The existing non-destructive community invariant stands: incomplete partitions do not prune or report complete
   success.
3. #875 is corrected before the tag with instance-aware resolution and an explicit non-degrading exclusion for an
   unresolved foreign instance; no generic storage redesign is authorized.
4. `graphQueryOperations` remains internal; no exported request-subject catalog is added.
5. Community readiness remains operation-local; no clustering `GRAPH_STATUS` producer is added merely for #820.
6. `semantic-tier-split` remains frozen. Only a current-premise annotation is allowed.
7. #829 is a known capability-quality limitation unless separately promoted to a tag gate.
8. #839 is a current measured capacity limit and crosses the tag only as an explicit owner-accepted release
   limitation; #857's broader payload-size programme remains deferred.
9. #633/#710, generalized readiness, hierarchy redesign, and speculative production hardening remain deferred to
   their owning measurements/programs.
10. Each advertised deterministic red tier, specifically #301 and #844/#860, must be fixed or receive an explicit
    owner-approved fold/delete with required coverage transferred; this recommendation does not pre-decide which.
11. Downstream repositories are not exhaustive pre-tag blockers. SemStreams publishes exact breaks and the tag;
   downstreams migrate after pinning. No compatibility shims or deprecated code are authorized.
12. The refactor takes no correctness shortcuts, but it also does not build production machinery this repository has
   no honest isolated test for.
13. #827 executes at the tag boundary with its halt-if-v1-window-closes condition intact.
14. The derived-index/current-state conformance matrix is accepted as release-truth evidence. It does not authorize a
    generic index/lifecycle abstraction or expand Option 2 runtime scope. Suffix, alias, spatial/temporal
    malformed-aggregate erasure, temporal cleanup, EntityID-cache, BM25, payload-bound, and uncertain anomaly findings
    receive explicit release disposition or separately approved work; none is silently treated as conforming.
    Clustering's sticky authority-poison latch is separately accepted as conforming to the current spec and does not
    authorize runtime work.
15. `GRAPH_INGEST_APPLIED_SEQ` and `GRAPH_STATUS` are included as index-adjacent operational state;
    `STORAGE_REPORT` is excluded from graph/query conformance.
16. Different physical layouts are not contract drift when their semantic ownership requires them. A new shared
    helper/interface is permitted only after at least two same-class implementations duplicate the same contract and
    current consumers benefit.

## Recommended next checkpoint

Freeze this inventory and issue census by checksum, then design only the two runtime slices: #855's non-destructive
incomplete-partition rule and #875's instance-aware resolution. #391's deterministic full-fusion route, advertised
red-tier disposition, truth cleanup, and exact release remain bounded proof/release slices. Any newly discovered
finding must be classified against this inventory before it can expand the recommendation.
