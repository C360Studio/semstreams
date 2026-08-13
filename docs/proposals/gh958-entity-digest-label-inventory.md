# GitHub #958 EntityDigest human-label inventory

Baseline: `baa59cf1147d4ea8e3ea41000e477995a6d2044f`

Phase: `inventory-only`

Body SHA-256: `892d44fa1d8b8f412b32d62c79026dc4d82e3176c9f42c4cc3efd506933b3d17`

Hash method: `sed -n '/^## Inventory body$/,$p' <file> | tail -n +2 | shasum -a 256`

## Inventory body

No files were changed and no tests were run during the read-only architecture inventory. The SemStreams worktree was
clean before and after inspection. The baseline is `origin/main`, eight commits after `v1.0.0-beta.160`; the relevant
graph-query, graph-index, vocabulary, and schema files have no diff from beta.160. The request/reply response-bounds
spec changed in `b039e344`, but its cited publish-observation requirement is unchanged. SemSource adopter evidence was
inspected at clean commit `87a3b07f6cce0f8d6578aad7f9e20e02685fc6a9`.

## Claimed gap and current behavior

The reported label predicate is already registered and is the one label convention shared by graph-query and the
vocabulary registry:

- `dc.terms.title` is the first hard-coded graph-query label predicate at
  `processor/graph-query/graphrag.go:1711-1718`.
- Framework vocabulary registers it as `AliasTypeLabel` at `vocabulary/labels.go:3-23`.
- Registry discovery exposes registered label predicates at `vocabulary/registry.go:465-484`.
- Graph-index snapshots those discovered predicates at `processor/graph-index/component.go:597-607`, constructs
  desired name rows at `processor/graph-index/component.go:1307-1317`, applies them at `component.go:1359-1381`, and
  physically reconciles `NAME_INDEX` at `processor/graph-index/name_index.go:91-111`.

Graph-query and the vocabulary registry otherwise implement overlapping, non-identical label vocabularies:

| Label class | Graph-query | Vocabulary registry / `NAME_INDEX` |
|---|---|---|
| Shared | `dc.terms.title` | `dc.terms.title`, registered as `AliasTypeLabel` |
| Graph-query only | `agent.identity.display-name`, `agent.capability.name`, `agent.model.name` | Registered as ordinary predicates without `AliasTypeLabel` |
| Registry/index only | No dynamically discovered predicate | Any product/application predicate registered with `AliasTypeLabel` and a priority |

Graph-query's fixed order is defined at `processor/graph-query/graphrag.go:1711-1718`. The three agentic registrations
have description/type/IRI metadata but no label alias at `vocabulary/agentic/register.go:205-215,398-408,673-683`.
`DiscoverLabelPredicates` admits application registrations at `vocabulary/registry.go:465-484`, but graph-query never
calls it. Graph-index snapshots the discovered map once during `Start`, so a later registration is not visible until
a later component start.

Consequently, product label predicates can become `NAME_INDEX`-visible while remaining invisible to graph-query, and
the three agentic names can become digest labels while remaining absent from `NAME_INDEX`. Graph-query uses slice
position rather than registry alias priority. This divergence does not cause the reported SemSource failure: its
entities use the shared `dc.terms.title` convention. Their failure is the partial-hydration producer path.

The observed failure is in two digest producers.

### Auto-summary GraphRAG

- Semantic retrieval is capped at 100 IDs at `processor/graph-query/graphrag.go:814-833`.
- Auto-summary starts at `processor/graph-query/graphrag.go:856-872`.
- Only selected community representatives are loaded and enriched at
  `processor/graph-query/graphrag.go:1968-2053`.
- The top-level label map is assembled solely from those representatives at
  `processor/graph-query/graphrag.go:892-898`.
- Every result ID is projected through `buildEntityDigests` at `processor/graph-query/graphrag.go:900-917`.
- Missing label-map entries fall back to the sixth entity-ID segment at
  `processor/graph-query/graphrag.go:1931-1947`.

Therefore title-bearing non-representatives silently receive hash/path-like ID-instance labels.

### Direct `searchGraph` semantic fallback

- `adaptSemanticToGlobalSearchResponse` creates top-level digests containing only ID and relevance at
  `processor/graph-query/searchgraph.go:230-263`.
- Later community enrichment does not write labels or types back to those top-level digests at
  `processor/graph-query/searchgraph.go:132-176`.

These rows expose empty labels rather than the hash fallback.

A complete entity can resolve the expected title: `resolveLabel` tries `dc.terms.title` first at
`processor/graph-query/graphrag.go:1912-1929`. An existing `resolveEntityLabels` helper loads result IDs and resolves
them, but repository search found only its definition and no production caller.

### Exhaustive `EntityDigest` producer and response-branch inventory

Exact assignment search found top-level `EntityDigests` production only at `graphrag.go:322`, `:366`, `:905`, and
`searchgraph.go:259`. `CommunitySummary.Entities` is a separate representative-digest surface.

| Response branch | Top-level shape and current label behavior | Bound |
|---|---|---|
| LocalSearch, no community, semantic fallback | Full `Entities` plus digests from loaded entities; titles resolve before ID fallback (`graphrag.go:300-322`) | Semantic request limit 50; digests at most 50 |
| LocalSearch, normal community | Full `Entities` plus digests from all loaded/filtered community members (`graphrag.go:330-366`) | No explicit member/digest cap |
| GlobalSearch GraphRAG, auto-summary | IDs plus digests; labels come only from selected representatives, so other rows use ID instance (`graphrag.go:856-917`) | At most 100 digests |
| GlobalSearch GraphRAG, below threshold | Full `Entities`; no top-level digests (`graphrag.go:920-963`) | Semantic retrieval at most 100 |
| GlobalSearch pure semantic | Full `Entities`; no top-level digests (`graphrag.go:1061-1147`) | Semantic retrieval at most 100 |
| GlobalSearch entity lookup | Full `Entities`; no top-level digests (`graphrag.go:998-1032`) | One resolved entity |
| GlobalSearch PathRAG | Full `Entities`; no top-level digests (`graphrag.go:424-445`) | Existing path-query bound |
| GlobalSearch temporal/spatial | Full `Entities`; no top-level digests (`graphrag.go:1207-1306`) | Request limit 100 |
| GlobalSearch text/community fallback | Full `Entities`; no top-level digests (`graphrag.go:1369-1460`) | Candidate union capped at 10,000 |
| `searchGraph`, non-empty GlobalSearch | Returns the selected GlobalSearch branch unchanged (`searchgraph.go:29-36`) | Inherits branch bound |
| `searchGraph`, direct semantic fallback | Digests contain only ID/relevance; enrichment does not backfill label/type (`searchgraph.go:117-176,230-263`) | At most 8 digests |
| `CommunitySummary.Entities` | Separate selected representative digests; loaded labels resolve, otherwise ID fallback (`graphrag.go:1968-2076`) | Five representatives per returned community |
| Full-entity digest helper | Used by both LocalSearch branches; one resolved digest per loaded entity (`graphrag.go:1950-1965`) | Inherits caller bound |

Research consumption differs by shape. Digest replies copy label/type/relevance into `research.Candidate`, while
full-entity replies deliberately project only ID/type and leave label/relevance empty at
`processor/research-graph-classify/adapters.go:153-200`; the latter is specified at
`openspec/specs/graph-query/spec.md:423-433`. A below-threshold entity may therefore carry `dc.terms.title` while the
research candidate remains unlabeled.

Gateway exposure is asymmetric. `GlobalSearchResult` advertises `entity_digests` and `EntityDigest.label` at
`gateway/graph-gateway/component.go:1698-1704`. The Go `LocalSearchResponse` has `EntityDigests` at
`processor/graph-query/graphrag.go:131-145`, but gateway `LocalSearchResult` omits that field at
`gateway/graph-gateway/component.go:1699-1701`.

The archived thematic-synthesis change was scoped to answer-synthesis representatives and tags, not complete
top-level digest labels (`openspec/changes/archive/2026-07-27-thematic-synthesis-context/design.md:19-52` and
`proposal.md:39-43`). It replaced label-only representative hydration with a full-entity representative batch. The
fixed label list, unused all-ID resolver, and instance fallback predate that archive and blame to `03c0d560`.

## Same and near spellings

| Spelling or surface | Existing meaning |
|---|---|
| `EntityDigest.Label`, JSON `label` | Human-readable name from key predicates; `processor/graph-query/graphrag.go:244-252` |
| `entity_digests` | Compact top-level search results; `processor/graph-query/graphrag.go:187-217` |
| `dc.terms.title` | Framework label predicate; `vocabulary/standards.go:120-127`, `vocabulary/labels.go:3-23` |
| `AliasTypeLabel` | Display-only vocabulary alias classification; `vocabulary/registry.go:26-38` |
| `DiscoverLabelPredicates` | Registry-owned label-predicate discovery; `vocabulary/registry.go:465-484` |
| `NAME_INDEX` | Derived normalized name to ranked IDs index; `graph/constants.go:13-20` |
| `graph.query.byName` | Exact name to ID lookup, not entity ID to label; `processor/graph-index/name_index.go:258-369` |
| `summarize_threshold` | Caller-selected full-entity/compact shape switch; `processor/graph-query/graphrag.go:158-176` |
| GraphQL `EntityDigest.label` | Hand-declared gateway field; `gateway/graph-gateway/component.go:1698-1704` |
| SemSource `titlePredicate` | Local full-entity fallback reader for `dc.terms.title`; `processor/mcp-gateway/graph_matches.go:15-26` |

Exact empty-search evidence:

- `resolveEntityLabels(` under `processor/graph-query`: definition only.
- `DiscoverLabelPredicates` under `processor/graph-query`: no matches.
- `NAME_INDEX`, `BucketNameIndex`, `graph.query.byName`, or `byName` in `graphrag.go` and `searchgraph.go`: no matches.
- Label-predicate or digest-property configuration under graph-query, schemas, specs, and config: no matches.
- `EntityDigest`, `entity_digests`, or `summarize_threshold` in generated `schemas/` and `specs/`: no response-schema
  matches.

The issue's “NAME_INDEX-style key” is not read from `NAME_INDEX`; code derives it directly from the entity ID's
instance segment at `processor/graph-query/graphrag.go:264-272`.

## Adjacent claims and scope boundaries

- `resolveLabel` has a non-registry convention: after its four hard-coded predicates, it returns the first
  non-entity-ID string triple at `processor/graph-query/graphrag.go:1922-1928`. That value need not be a human name.
- Graph-query and registry label sets overlap without either being a subset. Product-defined `AliasTypeLabel`
  predicates can enter `NAME_INDEX` while remaining invisible to graph-query; the three fixed agentic label
  predicates can resolve in graph-query while remaining absent from `NAME_INDEX`.
- SemSource's latest downstream discussion also asks for digest property values. `EntityDigest` has no property field;
  its fields are ID, type, label, relevance, and tags at `processor/graph-query/graphrag.go:244-252`. That is an
  adjacent wire-shape request, not the observed label-value defect.
- Full-entity responses have no operation-owned byte budget. The shared request/reply layer observes an actually
  oversized publish and returns `response_too_large`; callers should not predict NATS payload size. See
  `openspec/specs/request-reply-response-bounds/spec.md:9-45`.
- That transport contract requires encode-and-publish observation before oversize classification. The implementation
  first calls `msg.Respond`, then classifies only observed `nats.ErrMaxPayload` with actual response bytes and the
  rejecting connection's limit at `natsclient/request.go:394-411`. `Client.MaxPayload` is diagnostic, not predictive,
  at `natsclient/client.go:211-222`.
- Generated `schemas/graph-query.v1.json` is component configuration only. Response shape is held in Go structs and
  gateway introspection.

### Active-change constraints

Two unarchived OpenSpec changes overlap this territory without authorizing #958 implementation:

- `openspec/changes/semantic-tier-split` is explicitly **SUSPENDED AND FROZEN** by the ADR-090 graph-state program;
  it must not be implemented, promoted, or archived without a fresh inventory and owner release
  (`proposal.md:3-12`, `tasks.md:3-11`, `specs/e2e-tiers/spec.md:1-5`). Its preserved evidence says neither semantic
  E2E scenario exercises `search_graph` (`proposal.md:95-103`). Tasks 3b.3-3b.4 record that sub-threshold behavior is
  exercised but not asserted end to end, and that the agent-facing full-`Entities` versus `EntityDigests` formatter
  path is uncovered (`tasks.md:86-94`). #958's test design must close its own path without treating those frozen tasks
  as executable or release authority.
- `openspec/changes/post-g-tag-safety-closeout` remains an active candidate package. Its graph-index delta requires
  shipped replacement/INCOMING behavior to remain unchanged until its proof and ADR gates pass. Any later adoption
  begins on newly provisioned storage and rebuilds fresh raw `NAME_INDEX` behind typed not-ready responses until the
  authoritative watermark is reached; no legacy key, dual format, migration, or rollback path is admitted
  (`specs/graph-index/spec.md:3-18`). #958 must not accidentally activate or depend on that gated index adoption.

## Consumer-at-birth inventory

The existing admitted operation is `graph.query.searchGraph`; this is not a new query access path.

- Canonical operation inventory: `processor/graph-query/query.go:45-69`.
- Declared consumers: graph-gateway, research-graph-classify, and research-graph-execute at
  `processor/graph-query/query.go:63-68`.
- GraphQL exposure: `gateway/graph-gateway/component.go:1687-1704`.
- Research classify copies digest label directly into `research.Candidate` at
  `processor/research-graph-classify/adapters.go:153-171`.
- Research route renders that label in prompts at `processor/research-graph-route/prompt.go:153-173`.
- Name-based research seed resolution matches candidate labels at
  `processor/research-graph-execute/handler.go:123-151`.
- Research execute's BM25 adapter ignores label and consumes ID/relevance only at
  `processor/research-graph-execute/adapters.go:268-315`.

External adopter: a SemSource MCP-gateway developer who has never opened graph-query internals.

- Current SemSource sends `summarize_threshold: 1` to `graph.query.searchGraph` at
  `processor/mcp-gateway/query_tools.go:87-105`.
- It prefers digests and copies the upstream label without repair at
  `processor/mcp-gateway/graph_matches.go:116-144`.
- It caps rendered matches at 25 at `processor/mcp-gateway/graph_matches.go:15-18`.
- Gradle dependencies emit `dc.terms.title` and the dependency name at `handler/cfgfile/entities.go:529-551`.
- AST code symbols emit `dc.terms.title` at `source/ast/entities.go:220-242`.
- SemSource explicitly registers `dc.terms.title` as a label predicate at `source/ast/vocabulary.go:267-280`.
- Current durable downstream text says no local stopgap ships and the threshold-zero experiment was rejected at
  `docs/upstream/semstreams-asks.md:1029-1059`.

Thus the issue body's threshold-zero wording describes an experiment, not the current adopter implementation.

## Ownership and collision matrix

No new durable primitive is inventoried.

| Dimension | Vocabulary registry | Graph-ingest / `ENTITY_STATES` | Graph-index / `NAME_INDEX` | Graph-query digest projection |
|---|---|---|---|---|
| Semantic class | Declares which predicates mean display labels and their salience | Holds authoritative label facts as entity triples | Projects label strings into name-to-entity memberships | Chooses one human-readable label for each compact result |
| Owners | Framework/product initializers amend process-global metadata; no durable row owner | `processor/graph-ingest` is sole authority writer | `processor/graph-index` owns the bucket; each entity owns rows selected by its owner filter | `processor/graph-query` owns response shaping; no durable label state |
| Catalogs | Process-global predicate registry | Framework KV catalog entry for authoritative `ENTITY_STATES`; owns read cache | Framework KV catalog declares graph-index-owned `NAME_INDEX` (`graph/kvcatalog.go:115-128`) | Internal operation catalog declares `globalSearch`, `searchGraph`, and `localSearch` (`processor/graph-query/query.go:45-69`) |
| Status | No runtime readiness surface | Committed state is queryable; cache coherence is internal | Publishes revision-lag/readiness in `GRAPH_STATUS`; queries fail closed while incomplete | `Degraded` covers strategy/community/synthesis, not label completeness |
| Lifecycle | Package registration/amendment; no live notification to started consumers | Mutations commit authority and synchronously invalidate that entity's cache | Startup watch replay; asynchronous ordered update/delete reconciliation | Per-request projection; no durable label state or rebuild |
| Label multiplicity | Many label predicates with independent priorities | Multiple strings and multiple triples for one predicate | Every discovered label-string membership | One fixed-priority value, then one arbitrary-string fallback |
| Writers | Framework/product vocabulary initialization | Graph-ingest canonical mutation lanes | Graph-index only | None to durable state |
| Readers | Graph-index and registry callers | Graph-query batch hydration and graph consumers | `graph.query.byName` | NATS/GraphQL/research/adopter consumers |
| Rename/replacement | Metadata amendment does not rewrite triples | Title mutation changes authority | Reconciliation retracts old name and writes new metadata | Later entity load projects corrected triple |
| Retraction/delete | No entity-membership lifecycle | Entity deletion removes authority | Entity absence deletes owner-selective rows; failures withhold readiness and enter repair | Missing/unloaded entity falls back or disappears by branch |
| Ownership | Process-global mutex-protected registry | Sole-writer logical rule | Owner-filtered derived rows | No durable claim; per-request projection |
| Claims / leases | No claim or lease protocol | Local projection contracts are validation schemas; retired owner-lease fields are rejected | Owner filters select retractable rows; they are not semantic claims or leases | Community request leases validate one cache generation only, not label ownership |
| Singleton | One registry per process; no cross-process singleton | No distributed singleton or leader-election primitive | No singleton or leader-election contract | One active community-cache generation per component; no label singleton |
| Partitioning | None | Graphable arrivals partition by entity ID: one ID serial, different IDs concurrent | Per-entity keyed FIFO reconciliation; NAME rows shard by normalized name/entity/predicate | Per request; community records remain level-qualified |
| Active-active | No cross-process registry coordination | Sole-writer invariant provides no active-active multi-writer contract | Idempotent row writes do not establish active-active deployment safety | Stateless handlers do not establish cross-instance cache coherence or label authority |
| Recovery | Process initialization | KV authority survives restart | Watch replay rebuild; failed entities retry | Recomputed per query |

Graph-index is derived and rebuildable from canonical state, with explicit owner-selective retraction and cold replay
(`openspec/specs/graph-index/spec.md:3-48`). Known-incomplete indexes publish not-ready status through `GRAPH_STATUS`;
initial watch delivery alone is insufficient, and watermark completion gates readiness (`graph-index/spec.md:70-170`,
`processor/graph-index/component.go:614-659,873-890`). Updates/deletes use the same ordered per-entity lane, reread
current `ENTITY_STATES`, and delete index rows on absence (`component.go:892-917,1196-1224`).

Name reconciliation builds the complete desired set, owner reconciliation deletes stale keys and writes desired keys,
and clean entity deletion removes every owner-selective row (`processor/graph-index/name_index.go:91-111`,
`owner_reconcile.go:18-83`, `component.go:1910-1991`). Failures keep readiness false and are resubmitted on the
30-second repair loop (`component.go:1062-1081,1170-1194`); readiness compares the applied watermark with current
authority and failed-set overrides (`processor/graph-index/watermark.go:21-67`). Rename/removal semantics are specified
at `openspec/specs/graph-index/spec.md:454-493`.

`NAME_INDEX` remains name to IDs, not entity ID to best display label. Graph-index's capability boundary explicitly
assigns result shaping to graph-query at `openspec/specs/graph-index/spec.md:40-46`.

Operational ownership is explicit at the authority boundary: graph-ingest is the sole writer to `ENTITY_STATES`
(`openspec/project.md:94-95`, `openspec/specs/graph-ingest/spec.md:5-16`). Its Graphable lane partitions work by entity
ID, preserving serial order for one ID while allowing different IDs to proceed concurrently
(`graph-ingest/spec.md:130-151`). Exact searches for label-specific claims, leases, singleton assumptions, and
active/active rules under `vocabulary`, `processor/graph-ingest`, `processor/graph-index`, and
`processor/graph-query` found none beyond the mechanisms recorded above.

There is no live semantic owner/claim/lease subsystem on this surface. Runtime schemas reject retired owner-lease,
token, registry, presence, heartbeat, foreign-edge, and semantic-ownership fields
(`openspec/specs/component-runtime-config/spec.md:232-243`). Projection contracts explicitly cannot register or
imply owners, claims, leases, heartbeats, tokens, presence, foreign-edge modes, or global overlap rules
(`openspec/specs/projection-mutation-client/spec.md:9-21`). The graph-index boundary's reference to owner-lease
enforcement at `openspec/specs/graph-index/spec.md:35-36` is stale relative to those retirement requirements and is
not evidence of a live lease mechanism.

Graph-query's `communityLease` is unrelated to semantic ownership: it identifies and validates one published cache
generation (`processor/graph-query/community_cache.go:43-48,161-190`, `graphrag.go:73-116`). The vocabulary registry
is process-global at `vocabulary/registry.go:94-98`; no cross-process singleton, leader election, or active-active
synchronization exists. Likewise, graph-ingest's sole-writer contract is an ownership rule, not a distributed
active-active writer protocol.

## Bounds, ranking, caching, and failure semantics

`MaxCommunities` is not globally capped. Validation rejects only an empty query; a non-positive value defaults to five,
while every positive integer is accepted (`processor/graph-query/graphrag.go:721-737`).
`findCommunitiesForEntities` scans every cached community and member and sorts all matches before callers truncate
(`graphrag.go:1662-1708`). The cache can include every hierarchy level, with producer depth capped at ten
(`graph/clustering/lpa.go:22-26,109-118`), and cross-level behavior is intentional
(`processor/graph-query/graphrag_enrich_test.go:119-184`). Both auto-summary and direct fallback scan before truncation.

Therefore five representatives times the default five communities is a usual 25-ID load, not a framework maximum.
A positive caller `MaxCommunities` can enlarge returned summaries and representative hydration. IDs are deduplicated
before one batch load, but scan cost covers the full cache. Independently, auto-summary top-level digests are capped at
100, direct fallback at 8, LocalSearch semantic fallback at 50, and normal-community LocalSearch has no explicit digest
cap. Text fallback scores communities at one requested level, selects caller `MaxCommunities`, then caps the full-entity
union at 10,000 (`graphrag.go:1369-1460`).

Top-level digest order preserves semantic-hit order. Representative selection uses relevance descending and ID
ascending. Several community/text sorts lack an equal-score tie-break, but label-map assembly does not reorder the
top-level digests. Community-cache generation safety is specified at
`openspec/specs/graph-query/spec.md:301-329`; this defect is independent of generation validity.

Ordinary representative-load failures degrade to ID fallback; authoritative graph-state contract errors propagate.
Hash/path fallback has no typed error, degradation flag, provenance, or completeness indicator.

### Cache and correction propagation

Graph-query hydrates labels from canonical entity state through graph-ingest, not from `NAME_INDEX`. `loadEntities`
issues one `graph.ingest.query.batch` request (`processor/graph-query/graphrag.go:1497-1527`). Graph-ingest uses a
5,000-entry hybrid LRU/TTL cache with a 30-second TTL (`processor/graph-ingest/component.go:1072-1083`); batch reads
check it first, then perform bounded concurrent KV reads, returning cache hits before misses rather than request order
(`processor/graph-ingest/query.go:616-700`).

Admitted canonical mutations do not wait for TTL expiry. Create, merge, delete, single append, and batch append
invalidate after commit at `processor/graph-ingest/component.go:1985-1991,2067-2072,2090-2105,2294-2299,2489-2493`.
Canonical mutation receipts use the same primitive at `processor/graph-ingest/canonical_mutations.go:633-637`. The
generation guard at `processor/graph-ingest/component.go:2116-2178` prevents a concurrent slow read from repopulating
a pre-write revision after invalidation. Thus the next admitted graph-ingest-backed load observes a committed title
correction; the TTL matters for out-of-band authority writes that bypass the admitted invalidation seam.

`NAME_INDEX` converges differently. Graph-index observes `ENTITY_STATES` asynchronously, dispatches ordered work, and
rereads current authority so a stale queued event cannot restore an old label
(`processor/graph-index/component.go:850-917,1196-1224`). Rename/retraction becomes visible after reconciliation and
watermark/status catch-up; failures make `GRAPH_STATUS` not ready and enter repair. Graph-query does not consult this
index or its readiness, so digest correction follows graph-ingest cache coherence rather than index convergence.

### Label multiplicity and ranking

Graph-query and `NAME_INDEX` reduce multiplicity differently. `EntityState.GetTriple` returns the first matching
triple in slice order and `GetPropertyValue` delegates to it (`graph/types.go:50-76`). `resolveLabel` walks four fixed
predicates and accepts the first non-empty string, then walks all triples for the first non-empty non-entity-ID string
(`processor/graph-query/graphrag.go:1912-1929`). Multiple values for one predicate therefore resolve by triple order,
not vocabulary priority or deterministic value sorting.

Graph-index instead iterates every triple; every string with a predicate in its startup label snapshot becomes a
`nameIndexWrite` (`processor/graph-index/component.go:1282-1313`). Physical identity combines normalized-name hash,
entity ID, and predicate, while the value retains original case and alias priority
(`processor/graph-index/name_index.go:29-73`). Distinct strings produce distinct memberships; exact duplicate keys
collapse in the desired map. `byName` starts with a caller name, scans only that normalized-name prefix, ranks exact
case then predicate priority then entity ID, and keeps one best match per entity within that matched-name set
(`name_index.go:258-401`). It does not provide an entity ID to preferred display-name operation.

## Existing test posture

- `TestResolveLabel` proves `dc.terms.title` wins when an entity is loaded:
  `processor/graph-query/graphrag_test.go:52-96`.
- `TestBuildEntityDigests` explicitly locks missing-map fallback to the ID instance:
  `processor/graph-query/graphrag_test.go:133-166`.
- Integration coverage checks a representative summary digest title, not every top-level digest:
  `processor/graph-query/component_integration_test.go:631-653`.
- Semantic fallback tests assert ID and relevance but do not require label/type hydration:
  `processor/graph-query/searchgraph_test.go:66-106`.
- The tiered semantic known-answer E2E accepts a term appearing in either digest ID or label, so it can pass with an
  ID-style label: `test/e2e/scenarios/tiered_semantic_known_answer.go:157-208`.

Targeted existing tests passed during the inventory verification pass:

```text
go test ./processor/graph-query ./processor/graph-index ./vocabulary
ok processor/graph-query
ok processor/graph-index
ok vocabulary
```

## Adopter seam inventory

Specific adopter: the SemSource MCP-gateway developer.

1. **What must they know today?** They must know that `summarize_threshold` changes the response shape; compact
   digest labels are hydrated only for selected representatives; other rows may contain an ID fragment, and direct
   semantic fallback may contain no label. They must also know compact digests contain no property values.
2. **What happens if they do nothing?** The default threshold is 50. A broad search silently switches to compact
   results, reports successful ranked rows, and exposes hash/path/empty labels even when authoritative entity state
   carries `dc.terms.title`. No error tells the adopter that a title was skipped.
3. **Where do they find out?** Not in the admitted query contract, generated schema, GraphQL field description,
   degradation fields, or current top-level digest requirement. Today it is discoverable through implementation
   reading, GitHub #958, and SemSource's upstream-ask document.
4. **What should they have to know?** Ideally only that `searchGraph` returns ranked `EntityDigest` rows whose
   `label` is the framework's human-readable projection when authoritative entity state supplies one. They should not
   need to know representative caps, community selection, `NAME_INDEX` layout, title-predicate spelling, or NATS
   payload limits.

This seam exposes more than two undocumented adopter facts and is therefore an architectural-debt finding under the
SemStreams architect contract.

## Specification and history classification

Observed:

- Current graph-query OpenSpec specifies representative labels/tags for synthesis context at
  `openspec/specs/graph-query/spec.md:73-98`.
- It does not specify label completeness or fallback semantics for every top-level
  `GlobalSearchResponse.EntityDigests` row.
- Existing success shapes are declared wire-stable at `openspec/specs/graph-query/spec.md:269-299`.

Inferences requiring owner ruling:

- Correcting only the value carried by the existing `label` field need not introduce a new subject, payload type,
  GraphQL field, configuration field, or generated response schema.
- If top-level digest-label behavior becomes a durable adopter guarantee, graph-query is the current contract home
  requiring an explicit behavioral spec delta.
- Whether graph-query should continue its hard-coded predicate convention or defer to vocabulary-registry ownership
  remains an owner decision.

Historical evidence shows the digest/fallback implementation originated in `03c0d560` on 2026-03-21 and predates
beta.160. The baseline demonstrates presence at beta.160, not a beta.160-caused regression.

## Shared-decision skill routing

No shared decision skill triggers:

- `query-pattern`: no new access point; this is an existing admitted query result.
- `kv-or-stream`: no new communication path or storage primitive.
- `orchestration-check`: no orchestration boundary.
- `new-payload`: no new polymorphic payload.
