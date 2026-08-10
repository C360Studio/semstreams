# Slice E post-merge inventory

**Baseline:** HEAD `7c7c243fb9400af3fd09bfb906e58268b0857f2e`, tree-identical to `origin/main`
`360ffc3ff25e8f3f46ebbc9c120110fbe5fb57cc` at tree `c36eca70b9ccc36cb237722085bae6e8a0e6d3c0`.

This is inventory only. It makes no target-state recommendation or binding ruling.

## Problem statement

The current Slice E plan assumes one coherent embedded-decoding/fusion surface. The merged tree contains:

- one canonical reply-envelope discriminator;
- multiple independent embedded graph-read reply interpreters inside and outside Slice E;
- two distinct fusion entry points and client interfaces;
- one fusion NATS adapter with no in-repo production constructor;
- a graph-embedding component unrelated to that adapter;
- multiple successful global-search representations whose terminal strategy is generally not reported; and
- E2E coverage that reaches individual pieces but not the complete claimed composition.

This inventory records those current facts without selecting a target state.

## Production reply-interpreter census

`graph.UnwrapQueryResponse` is defined at `graph/query_contracts.go:31`. Its only production adopter is the GraphQL
gateway at `gateway/graph-gateway/component.go:1847`.

### In Slice E

- Research classify decodes a copied `searchGraph` response directly at
  `processor/research-graph-classify/adapters.go:139`.
- Research execute directly decodes:
  - batch at `processor/research-graph-execute/adapters.go:84`;
  - relationships at `processor/research-graph-execute/adapters.go:179`;
  - temporal at `processor/research-graph-execute/adapters.go:232`; and
  - BM25/`searchGraph` at `processor/research-graph-execute/adapters.go:281`.
- `fusionnats.Client` directly decodes:
  - prefix at `pkg/fusion/fusionnats/client.go:280`;
  - semantic at `pkg/fusion/fusionnats/client.go:306`;
  - entity at `pkg/fusion/fusionnats/client.go:370`;
  - batch at `pkg/fusion/fusionnats/client.go:392`;
  - relationships at `pkg/fusion/fusionnats/client.go:488`;
  - by-name at `pkg/fusion/fusionnats/client.go:554`; and
  - readiness separately from `GRAPH_STATUS` KV at `pkg/fusion/fusionnats/client.go:171`.

### Current production interpreters outside Slice E

- GraphQL gateway, already using the canonical helper, plus typed prefix/trajectory validation:
  `gateway/graph-gateway/component.go:1847`.
- Exact authority reader, explicitly preserved by F.2: `graph/exact_entity.go:51`.
- Provisional aggregate client, scheduled for deletion by F.1:
  - prefix: `graph/query/prefix.go:35`; and
  - predicate, predicate-list, stats, and compound: `graph/query/client.go:1132`.
- Agentic wrappers, scheduled for deletion by F.3:
  - `search_graph`: `processor/agentic-tools/executors/search_graph.go:138`; and
  - `summarize_graph`: `processor/agentic-tools/executors/summarize_graph.go:169`.
- Graph-clustering similarity reader: `processor/graph-clustering/similarity.go:77`.
- Direct authority-prefix readers:
  - gated DAG: `processor/gated-dag/reader.go:55`; and
  - agentic lessons: `processor/agentic-loop/lessons.go:45`.
- Graph-query's own downstream composition readers:
  - summary prefix/predicate-list: `processor/graph-query/summary.go:69`;
  - PathRAG relationships: `processor/graph-query/pathrag.go:252`;
  - alias/suffix resolver: `processor/graph-query/entity_resolver.go:50`;
  - exact, batch, relationships, hierarchy-prefix, and prefix passthrough:
    `processor/graph-query/query.go:96`;
  - temporal, spatial, batch, embedding, and clustering composition:
    `processor/graph-query/graphrag.go:1186`; and
  - internal `searchGraph` interpretation and semantic fallback:
    `processor/graph-query/searchgraph.go:55`.

`gateway/http/http.go:244-263` forwards bytes without interpreting their shape. `pkg/projection/mutation_client.go`
interprets mutation replies, not graph-read replies. `processor/graph-query/component.go` contains interfaces rather than
a reply decode site.

## Producer and routing census

The active public catalog contains 16 operations at `processor/graph-query/query.go:45-65`. The following eight are the operations directly consumed by the research or fusion adapters reached by Slice E. Every row names both the current producer and the public routing/composition point.

| Operation | Current success payload / envelope | Producer and public route | Slice E disposition |
|---|---|---|---|
| `entity` | `graph.ExactEntity`, bare | graph-ingest subscribes at `processor/graph-ingest/query.go:27-31` and produces the reply at `:60-122`; graph-query passes it through at `processor/graph-query/query.go:96-133` via the `entity` route at `processor/graph-query/router.go:19` | In Slice E through `fusionnats` |
| `batch` | `graph.EntityBatchResponse`, bare | graph-ingest subscribes at `processor/graph-ingest/query.go:33-38` and produces the reply at `:125-177`; graph-query passes it through at `processor/graph-query/query.go:223-251` via the `entityBatch` route at `processor/graph-query/router.go:20` | In Slice E through research execute and `fusionnats` |
| `relationships` | `[]relationshipWire`, bare | graph-index subscribes to outgoing/incoming at `processor/graph-index/query.go:23-37`; graph-query routes to those subjects at `processor/graph-query/router.go:23-25` and normalizes their envelopes at `processor/graph-query/query.go:340-425` | In Slice E through research execute and `fusionnats` |
| `prefix` | `graph.PrefixQueryResponse`, bare | graph-ingest subscribes at `processor/graph-ingest/query.go:40-45` and owns the producer handler at `:237-452`; graph-query passes it through at `processor/graph-query/query.go:253-293` via `processor/graph-query/router.go:21` | In Slice E through `fusionnats` |
| `temporal` | `[]TemporalResult`, bare | graph-index-temporal subscribes and produces at `processor/graph-index-temporal/query.go:13-83`; graph-query passes it through at `processor/graph-query/query.go:631-647` via `processor/graph-query/router.go:35` | In Slice E through research execute |
| `semantic` | graph-embedding `SearchResponse`, bare | graph-embedding subscribes at `processor/graph-embedding/query.go:17-31` and produces at `:171-205`; graph-query passes it through at `processor/graph-query/query.go:666-690` via `processor/graph-query/router.go:38` | In Slice E through `fusionnats` |
| `searchGraph` | `GlobalSearchResponse`, bare | graph-query registers the public operation from its catalog at `processor/graph-query/query.go:63,72-89` and composes the result at `processor/graph-query/searchgraph.go:55-130` | In Slice E through both research adapters; the agentic wrapper is outside Slice E and scheduled for deletion by F.3 |
| `byName` | `graph.NameData`, standard `graph.QueryResponse` | graph-index subscribes at `processor/graph-index/query.go:74-79` and produces the envelope at `processor/graph-index/name_index.go:258-310`; graph-query passes it through at `processor/graph-query/query.go:649-664` via `processor/graph-query/router.go:31` | In Slice E through `fusionnats` |

Today only fusion's `byName` call receives a standard envelope. The other seven research/fusion paths in the table are bare.

The remaining eight catalog operations have explicit current owners and are outside the direct Slice E research/fusion adapter set:

- `entityByAlias`: graph-query composition at `processor/graph-query/query.go:135-202`; current catalog consumer is graph-gateway.
- `pathSearch`: graph-query composition at `processor/graph-query/query.go:427-464`; current catalog consumer is graph-gateway.
- `hierarchyStats`: graph-query composition at `processor/graph-query/query.go:466-520`; current catalog consumer is graph-gateway.
- `spatial`: graph-index-spatial producer at `processor/graph-index-spatial/query.go:20-42`; graph-query passthrough at `processor/graph-query/query.go:614-628` through `processor/graph-query/router.go:34`; current catalog consumer is graph-gateway. Its result uses `json:"id"` at `processor/graph-index-spatial/query.go:45-55`.
- `similar`: graph-embedding producer registration at `processor/graph-embedding/query.go:17-24`; graph-query passthrough at `processor/graph-query/query.go:695` through `processor/graph-query/router.go:39`; current catalog consumer is graph-gateway.
- `globalSearch`: graph-query composition at `processor/graph-query/graphrag.go:674-787`; current catalog consumer is graph-gateway.
- `summary`: graph-query composition at `processor/graph-query/summary.go:58`; graph-gateway remains the admitted consumer, while the agentic wrapper is outside Slice E and scheduled for deletion by F.3.
- `localSearch`: graph-query composition at `processor/graph-query/graphrag.go:275`; current catalog consumer is graph-gateway.

The class-fidelity warning at `graph/query/prefix.go:24-32` is stale: the public prefix passthrough now uses `RequestClassified` at `processor/graph-query/query.go:275-284`.

Temporal result rows use `json:"id"` and `json:"type"` at `processor/graph-index-temporal/query.go:30-34`.

## Proven behavioral defects

- Research classify converts a valid full-entity-only `searchGraph` success into zero candidates. Non-summarized
  global search returns `Entities`, while the adapter iterates only `EntityDigests` at
  `processor/research-graph-classify/adapters.go:149-170`. The E2E fixture confirms these are two valid
  representations at `test/e2e/scenarios/tiered_semantic_known_answer.go:157-174`.
- Fusion entity decoding expects `graph.EntityState`, while the producer returns `graph.ExactEntity` with the
  same-entry KV revision:
  - producer contract: `graph/exact_entity.go:14-24`;
  - authority responder: `processor/graph-ingest/query.go:60-121`; and
  - incompatible decoder: `pkg/fusion/fusionnats/client.go:370-390`.
- Global-search `Strategy` is blank for nearly every successful path. The only production response assignment is
  semantic fallback at `processor/graph-query/searchgraph.go:289-295`.
- Global-search temporal and spatial strategies parse `entity_id` at
  `processor/graph-query/graphrag.go:1319`, while both producers emit `id`. Valid nonempty replies therefore become
  zero IDs in those paths.

The agentic `search_graph` wrapper also omits full entities and formats digests, but it is outside Slice E and
scheduled for deletion under F.3.

## Narrow or unresolved projections—not established defects

The following are not proven data loss without a receiving contract:

- Research execute projects batch entities, relationships, temporal results, and BM25 results into `fusion.Evidence`.
  That type contains only entity ID, tier, source, score, snippet, and ObjectStore reference at
  `pkg/fusion/evidence.go:19`. It has no receiver for triples, relationship predicates/direction, temporal type,
  summaries, or synthesized answers.
- Research classify returns `CandidateSet`, whose candidates contain identity, label/type, relevance, snippet, tier,
  and source. It has no receiver for summaries, answer, count, or strategy. The full-entity-to-zero-candidate collapse
  is proven; omission of the other fields is not independently proven defective.
- `fusion.Entity` carries only ID and triples at `pkg/fusion/lens.go:26`. E.5's revision-preservation claim currently
  has no receiving field and remains an unresolved premise.

## Global-search strategy and alternate representation facts

`GlobalSearchResponse.Strategy` exists at `processor/graph-query/graphrag.go:187-217`. The initial routing vocabulary
is produced at `processor/graph-query/graphrag.go:629-667`:

- `graphrag`;
- `entity_lookup`;
- `spatial`;
- `pathrag`;
- `semantic`; and
- `temporal`.

The chosen strategy is dispatched at `processor/graph-query/graphrag.go:719-787`. Fallback complicates terminal truth:

- temporal/spatial may execute GraphRAG instead at `graphrag.go:1191-1197,1258-1264`;
- `searchGraph` may retain the original blank response when semantic fallback errors or returns empty at
  `searchgraph.go:81-130`; and
- only successful non-empty semantic fallback identifies itself.

Research classify decodes `Strategy` but never consumes it at
`processor/research-graph-classify/adapters.go:68,149-170`. Research execute does not decode it. The semantic thematic
E2E reads and reports it at `test/e2e/scenarios/validate_thematic_eval.go:172-207,320-341,695-696`.

`processor/graph-query/searchgraph.go:218-269` still accepts a GraphQL wrapper named `similaritySearch`. The gateway's
admitted field is now only `semanticSearch` at `gateway/graph-gateway/component.go:1054,1674` and
`gateway/graph-gateway/README.md:70-76`; tests explicitly reject `similaritySearch` at
`gateway/graph-gateway/query_contract_closure_test.go:72-75`. The actual internal `searchGraph` fallback receives the
bare NATS response, as its own comment records.

## Fusion interface and subject census

The research execute path uses `fusion.GraphQueryClient`, a four-method pre-built-subquery interface at
`pkg/fusion/client.go:24`, through package function `fusion.Fuse` at `pkg/fusion/engine.go:96-198`.

The lens-driven engine uses `fusion.RetrievalClient` at `pkg/fusion/retrieval.go:17`. It has exactly six methods total:

1. `Status`
2. `Resolve`
3. `Entity`
4. `Entities`
5. `Neighbors`
6. `Names`

`fusionnats` separately defines six NATS subjects—by-name, prefix, semantic, entity, batch, and relationships—at
`pkg/fusion/fusionnats/client.go:25-32`. The mapping is not one-to-one:

- `Status` uses `GRAPH_STATUS` KV and no request subject;
- `Resolve` dispatches across by-name, prefix, and semantic;
- `Names` reuses by-name; and
- `Entity`, `Entities`, and `Neighbors` map to entity, batch, and relationships.

`fusionnats.Client` implements `RetrievalClient`, not the four-method `GraphQueryClient`. There is no in-repo production
constructor for `fusionnats.Client`; the only executable construction found is the E2E batch-read scenario at
`test/e2e/scenarios/validate_batch_read.go:343`, which calls only `Entities` at `:361`.

The graph-embedding component is not a fusion embedding host. It requires at least one input, rejects every configured
output, and writes KV directly rather than through ports at `processor/graph-embedding/component.go:74-134`. Its
default inputs are `ENTITY_STATES` KV-watch and optional `MESSAGES` store-read at
`processor/graph-embedding/component.go:198-218`. It publishes `GRAPH_STATUS/graph-embedding` at
`processor/graph-embedding/component.go:884-902,1351-1391`; fusion readiness instead watches
`GRAPH_STATUS/graph-index` at `pkg/fusion/fusionnats/client.go:125-147,171-234`.

No production component constructs `fusionnats.Client`, so no existing component declares the six NATS subjects or a
fusion readiness KV-read port. The active design acknowledges no current production component constructor for
`byName` at `openspec/changes/post-foundation-b-graph-query-contract-closure/design.md:140`, then assigns outputs and a
readiness declaration to an unspecified embedding component at `:160-163`.

## Every current spelling of the modeled facts

| Fact | Current homes |
|---|---|
| Query reply envelope discrimination | Canonical helper in `graph/query_contracts.go:31-117`; gateway adoption at `gateway/graph-gateway/component.go:1847-1881`; direct interpreters enumerated above |
| Operation payload/envelope declaration | Central `graphQueryOperations` table at `processor/graph-query/query.go:45-65`; prose copies in research adapters; typed/manual copies in fusion |
| Global-search representation | Producer struct at `processor/graph-query/graphrag.go:187-217`; classify subset at `research-graph-classify/adapters.go:67-100`; execute digest subset at `research-graph-execute/adapters.go:290-297`; E2E mirrors at `tiered_semantic_known_answer.go:216-245` and `validate_thematic_eval.go:172-207` |
| Strategy | Classifier/router strings at `graphrag.go:629-667`; metrics at `:567-583`; one response assignment at `searchgraph.go:289-295`; ignored classify field; E2E reporting |
| Fusion execution | Four-operation `GraphQueryClient` plus package `Fuse`; six-method `RetrievalClient` plus lens `Engine`; six request subjects in the NATS adapter with non-one-to-one method mapping |
| Readiness | Shared `graph/readiness` bucket, keys, watcher, and freshness at `graph/readiness/watcher.go:40-110`; graph-embedding publishes its own key; fusion lazily watches graph-index |
| Exact entity | `graph.ExactEntity` and `ExactEntityReader` at `graph/exact_entity.go:14-93`; authority wire producer at `graph-ingest/query.go:60-121`; fusion's obsolete `EntityState` interpretation |
| Query envelope request ID | Field and closed discriminator key at `graph/query_contracts.go:11-38`; constructor at `:21-29` never sets it |

Focused production search for `QueryResponse.RequestID` assignments or reads finds none on the graph-query response
surface. Other `RequestID` uses are mutation and agentic contracts, not this field.

## Adjacent claims on the territory

Current accepted artifacts constrain this slice:

- Exact reads must carry the canonical entity and same-entry KV revision:
  `openspec/specs/graph-query/spec.md:237-249`.
- Batch consumers, explicitly including fusion and research, must tolerate and surface missing reports:
  `openspec/specs/graph-query/spec.md:32-66`.
- Gateway reply detection already belongs to the canonical helper:
  `openspec/specs/gateway-response-projection/spec.md:39-63`.
- ADR-062 defines both the extracted research fusion lineage and the intended lens-driven interface:
  `docs/adr/062-deterministic-graph-fusion.md:11-68`.
- ADR-083 makes readiness distributed per-producer KV state:
  `docs/adr/083-readiness-as-distributed-state.md:59-68`.
- ADR-084 says readiness licenses health, not absence:
  `docs/adr/084-readiness-licenses-health-not-absence.md:35-80`.
- The active `semantic-tier-split` change is explicitly suspended; it supplies no active target for this slice.

Named issue adjacency, all currently open:

- #785 canonical decoder class closure;
- #786 phantom `QueryResponse.RequestID`;
- #819 missing global-search strategy;
- #823 sub-threshold global-search representation loss;
- #391 research E2E bypasses fusion;
- #621 fusion/PathRAG silent truncation;
- #643 live fusion batch reorder/cache-residency coverage gap;
- #830 semantic E2E red on main;
- #795 readiness consumer front door;
- #820 missing graph-clustering readiness;
- #868 broader readiness generalization; and
- #875, #881, and #887 embedding storage/reference/outcome accounting.

#820 is adjacent but not evidence that Slice E should couple fusion to graph-clustering; current fusion consumes only
`KeyGraphIndex`. The embedding issues concern the actual graph-embedding component and should not be conflated with the
absent component that would embed `fusionnats.Client`.

#822's request for an exported subject list overlaps the operation-inventory work, but the merged central internal
operation inventory and component port family have changed its premise; the issue remains open.

## Present consumers of the inventoried surfaces

- `graph.UnwrapQueryResponse` is already exported at `graph/query_contracts.go:91`; its only current production caller is graph-gateway at `gateway/graph-gateway/component.go:1871-1881`. No research or fusion production caller currently uses it.
- `GlobalSearchResponse.Strategy` already exists. The semantic thematic E2E reads it, SemSource is named as a downstream consumer in #819, research classify decodes and discards it, and research execute does not decode it.
- Bare/enveloped discrimination has a present gateway consumer because operations use different envelope shapes. No current research/fusion producer emits both shapes for the same operation.
- `fusionnats.Client` is exported and documented for sister repositories, but no in-repo production component constructs it. The in-repo E2E harness constructs it only for batch hydration.
- The implementation privately owns six request subjects and reads `GRAPH_STATUS/graph-index`; no present in-repo component declares those six subjects or constructs the client.
- `fusion.Entity` has no field receiving an exact-read KV revision.
- Current research `CandidateSet` and `Evidence` types have no fields receiving most full global-search representations.

## Same-class collision table

Semantic class: interpreting graph-query success replies and projecting truthful retrieval results over the existing
NATS request/reply and `GRAPH_STATUS` KV primitives.

| Dimension | Current state |
|---|---|
| Owners | `graph` owns envelope classification; graph-ingest and specialized indexes produce primitive replies; graph-query routes/composes them; gateway, research, fusion, aggregate client, clustering, gated DAG, lessons, and agentic tools interpret replies. |
| Catalogs | `graphQueryOperations` is the central public operation catalog; graph-query's router maps operations to actual producer subjects; several out-of-slice consumers retain literal subjects. |
| Status | Fusion `Status` watches `GRAPH_STATUS/graph-index`; research readers do not consume readiness; graph-clustering's embedding dependency is separate. |
| Lifecycle | Component-owned readers follow component lifecycle; fusion binds a lazy readiness watcher with optional `Close`; the aggregate client owns its own state. |
| Writers | Prefix: graph-ingest. Temporal: graph-index-temporal. Spatial: graph-index-spatial. Semantic/similar: graph-embedding. Relationships/by-name: graph-index. Global/searchGraph/summary: graph-query composition. |
| Recovery | Request/reply has no durable replay. `GRAPH_STATUS` replays held KV state. Research persists only its projected output, so omitted information is unavailable there unless another durable source is reread. |

The table shows multiple owners interpreting the same success-reply class and two fusion contracts, but no new
communication primitive is presently proposed or present.

## Test and E2E inventory

### Focused tests

- Research classify tests substitute `CandidateRetriever`; none drive `searchGraphRetriever` production decoding.
- Research execute tests substitute `GraphQueryClient`. Only the batch decode helper has direct production-decoder
  tests at `processor/research-graph-execute/adapters_entity_state_test.go:13-56`.
- There are no direct production-decoder tests for research relationships, temporal, BM25/`searchGraph`, or
  bare/enveloped parity.
- `fusionnats` unit tests cover readiness and all six request subjects, but only `byName` uses `QueryResponse`; entity
  fixtures use obsolete `EntityState`.
- `pkg/fusion/fusionnats/client_integration_test.go:30-180` drives the six request subjects and readiness over real
  NATS, but its entity responder also returns obsolete `EntityState`.
- No research/fusion test mentions `UnwrapQueryResponse`.

### E2E reachability

- Per-PR CI runs only `task e2e:statistical` at `.github/workflows/e2e-ladder.yml:1-51`.
- `task e2e:semantic` is outside that gate, and #830 records it red on a prior main revision.
- Semantic E2E directly exercises `fusionnats.Client.Entities` only at
  `test/e2e/scenarios/validate_batch_read.go:322-390`. Its file explicitly says the profile has no fusion/research
  route at `:55-63`.
- `e2e:research-graph` deliberately scripts `synthesize_directly` and asserts execute/assess triples are absent at
  `test/e2e/scenarios/research-graph/scenario.go:1-31,338-371`. It therefore does not exercise research execute or
  either fusion engine. #391 records this gap.
- `e2e:deep-research` validates a separate rules-driven agent chain, and its required components omit all
  `research-graph-*` processors at `test/e2e/scenarios/deep-research/scenario.go:151-190`.
- The semantic known-answer test recognizes full-entity and summarized/digest representations at
  `test/e2e/scenarios/tiered_semantic_known_answer.go:157-245`, but it does not validate research adapters.
- The semantic thematic test reads strategy but reports it rather than requiring non-empty strategy.

## Adopter seam inventory

- Research component config author:
  - Must know the exact operation and required output declaration.
  - Doing nothing or misdeclaring it fails boot.
  - Discovery is the boot validation error.
- External `fusionnats.Client` user:
  - Must know the six-method interface, mixed request/KV transport requirement, lazy status binding, and optional
    `Close`.
  - Doing nothing about the current entity mismatch makes valid producer replies fail decoding.
  - Discovery is a runtime decode or wiring error.
- Global-search consumer:
  - Must understand the alternative successful representations and that blank strategy currently does not identify
    the terminal path.
  - Doing nothing can turn populated results into apparent emptiness or misreport provenance.
- No present adopter receives the omitted research summary/triple/relationship fields or fusion KV revision. Those
  are open receiving-contract questions, not established migration obligations.

The present tree contains no component that constructs `fusionnats.Client`. Its subject mappings are private constants inside `pkg/fusion/fusionnats`, while readiness additionally requires the supplied transport to expose the underlying KV-capable bucket source. Consequently, there is no current in-repo component-config adopter for those mappings; external Go callers encounter the transport capability requirement when constructing or first using the client. Whether an in-repo component owner is intended is not established by the current tree.

## Reproducible searches

```text
rg -l "Request(Classified|ReadyClassified)\\(" graph processor gateway pkg \
  -g '*.go' -g '!**/*_test.go' -g '!**/doc.go' | sort
```

This is the bounded caller-file census used above. Three files were adjudicated out: opaque HTTP forwarding, mutation
replies, and interface-only declarations.

```text
rg -n "UnwrapQueryResponse" . \
  -g '*.go' -g '!**/*_test.go' -g '!**/doc.go'
```

Returns only the helper definition and gateway adoption.

```text
rg -n "UnwrapQueryResponse" \
  processor/research-graph-classify processor/research-graph-execute pkg/fusion \
  -g '*.go'
```

Returns zero.

```text
rg -n "fusionnats\\.New|fusionnats\\.Client|fusion\\.RetrievalClient" \
  . -g '*.go' -g '!**/*_test.go' -g '!**/doc.go'
```

Returns definitions and comments only; no production constructor.

```text
rg -n "Strategy\\s*:" processor/graph-query \
  -g '*.go' -g '!**/*_test.go'
```

Returns only the semantic-fallback response assignment.

```text
rg -n "newSearchGraphRetriever|FetchCandidates|newGraphQueryAdapter|PredicateWalk\\(|TemporalRange\\(|BM25\\(" \
  processor/research-graph-classify processor/research-graph-execute -g '*_test.go'
```

Returns fake interfaces only; zero production-wire tests for classify, relationships, temporal, or BM25.

```text
rg -n "research-graph-(classify|execute)|pkg/fusion|fusionnats|graph.query.searchGraph|graph.query.batch" \
  test/e2e/scenarios/deep-research docker/compose/deep-research.yml
```

Returns zero.

```text
rg -n "RequestID\\s*:|\\.RequestID" graph processor/graph-query gateway/graph-gateway pkg/fusion \
  -g '*.go' -g '!**/*_test.go'
```

On `graph.QueryResponse`, this finds zero producer assignments and zero consumer reads; unrelated mutation/agentic
request IDs remain.

## Open evidence questions

1. What concrete production component, in this repo or a named downstream, is the "component embedding fusion"
   referred to by E.6? No in-repo component presently satisfies that premise.
2. Does "preserve full entities, digests, summaries, answer, and degradation" mean preservation only through decoding,
   or persistence into `CandidateSet`/`Evidence`? The present output types cannot carry most representations.
3. Which operations have a live requirement to accept both bare and standard-enveloped replies? Current producers
   select one stable shape per operation; no current research/fusion producer emits both variants of one operation.
4. What is the closed terminal-strategy vocabulary after fallbacks? The tree has initial route names plus
   `semantic_fallback`, but temporal/spatial and `searchGraph` may return results produced by another terminal path.
5. Where would `ExactEntity.KVRevision` be represented in the existing `fusion.Entity` result, which currently
   contains only ID and triples?
6. Is the stale `similaritySearch` wrapper in `searchGraph` reachable from a supported caller after the GraphQL clean
   break? In-repo GraphQL exposes only `semanticSearch`; internal composition receives bare NATS.
7. What live gate can establish the complete fusion composition? Semantic E2E reaches only batch hydration, research
   E2E bypasses execute/fusion, and deep-research is a separate chain.
8. Is `QueryResponse.RequestID` still part of the success-envelope contract? It has no producer or consumer but
   remains one of the discriminator's accepted keys.

This is inventory-only and is ready for independent `INVENTORY PASS`.
