# gh#606 Derived Communities — Surface Inventory

Status: DRAFT — awaiting independent inventory review (contract step 3). Not yet INVENTORY PASS.
Baseline: branch `codex/gh1054-lifecycle-test-flake`, `main` ancestor; touched surfaces diff from
`main` only in `processor/graph-clustering/lifecycle_owner_test.go` and
`processor/graph-query/lifecycle_owner_test.go` (test-only). All citations valid against `main`.
Commissioned by: owner ruling, newest comment on gh#606 (2026-08-23 triage docket).

## Problem statement

The owner ruled that LPA community detection is replaced by a partition DERIVED from the 6-part
entity ID hierarchy (`org.platform.domain.system.type.instance`), with detection surviving only as
a default-off, measurable overlay over explicit edges. Grounding measurements (semsource, 3
corpora, recorded in the ruling): largest LPA community 47–82% of the graph; zero shipped
deployments consume the community index; "better clusters are not better answers"; a system filter
yields the useful partition. This inventory enumerates the touched surface before any target state.

## 1. The claimed gap (does a derived prefix partition already exist?)

The partition-as-a-pure-function-of-IDs does NOT exist as a served partition, but the fact it
models is already computed in FIVE places (see §2). Searches that closed the gap-claim:

- `grep -rn "PrefixPartition\|prefix_partition\|derived.*community\|community.*derived" --include="*.go"`
  → no producer of a prefix-derived community record. Empty.
- The nearest existing thing: `handleQueryHierarchyStats` (`processor/graph-query/query.go:469-545`)
  already serves on-demand prefix-partition COUNTS (group entities by next ID segment under a
  prefix) via the admitted `hierarchyStats` operation (GraphQL `entityIdHierarchy`,
  `processor/graph-query/query.go:55`), backed by the paginated
  `graph.ingest.query.prefix` lane. Membership-by-prefix-scan is therefore an EXISTING, admitted,
  bounded read pattern — not a new invention.

## 2. Every current spelling of the ID-hierarchy-grouping fact

More than one home is a defect to consolidate. Today there are five-plus:

| # | Spelling | Where | Notes |
|---|---|---|---|
| 1 | Canonical prefix accessors | `pkg/types/entity_id.go:257-348` — `TypePrefix` (5), `SystemPrefix` (4), `DomainPrefix` (3), `PlatformPrefix` (2), `HasPrefix`, `IsSibling`, `IsSameSystem`, `IsSameDomain` | THE one home the design should consume |
| 2 | Ephemeral virtual-edge synthesis | `graph/clustering/entityid_provider.go:26-50` (typePrefixCache/systemCache, `:214`, `:232` re-split IDs); operator config `processor/graph-clustering/component.go:86,110-117` (`EntityIDEdgesConfig`); semantic-enabled rebalance `:218-237` | The inversion the ruling names: ID structure as LPA *input* |
| 3 | Materialized hierarchy containers + membership triples | `graph/inference/hierarchy.go` (containers `…type.group`, `…group.container`, `…group.container.level`, predicates `hierarchy.{type,system,domain}.member`, sibling edges); wired in graph-ingest `processor/graph-ingest/component.go:1352-1377`, gated by `enable_hierarchy` (default false, `component.go:342,407`) but **ON in shipped tier configs** (`configs/statistical.json:593`, `configs/semantic.json:630`, 12 configs total) | Means "explicit edges" in shipped tiers ALREADY encode the prefix hierarchy — LPA over them re-derives the ID structure twice over |
| 4 | On-demand prefix aggregation | `processor/graph-query/query.go:469-545` (`hierarchyStats`), `summary.go:64-100` (entity-type aggregation reuses the same prefix orchestration), `graphrag.go:256-266` (`extractEntityType`/`extractEntityInstance`) | Admitted, bounded, consumer-facing |
| 5 | Ad-hoc `strings.Split(entityID, ".")` interpreters | `graph/clustering/summarizer.go:687`; `processor/rule/entity_substitution.go:73`; `processor/rule/actions.go:1575`; `processor/agentic-loop/lessonmatch/lessonmatch.go:221`; `agentic/entity_ids.go:165`; `pkg/lifecycle/manager_query.go:531`; `processor/graph-ingest/component.go:2620` | Background debt; not all in scope, but the design must not add interpreter N+1 |

## 3. The current community surface (producer → stores → consumers → configs → e2e)

### Producer: `processor/graph-clustering`

- Component/config: `component.go:56-102` (`detection_interval`, `batch_size`, `enable_llm`,
  `enhancement_workers`, `min_community_size`, `max_iterations`, `allow_ungated_reads`,
  `enable_anomaly_detection`+`anomaly_config`, `entity_id_edges`, `semantic_edges`).
  **Dead knob found:** `BatchSize` is defaulted (`:416`,`:530`) and never read by any runtime path;
  the `entity_watch` KV-watch input port (`:491`,`:514`) feeds no trigger — the loop is
  ticker-only (`runDetectionLoop`, `:1426-1468`). Removed-key probe precedent exists at
  `:249-276` (`removedConfigFields` / `rejectRemovedConfigKeys`).
- Start: acquires `COMMUNITY_SUMMARIES` then `COMMUNITY_INDEX` (`:979-998`, ordering
  deliberate), binds ADR-083/085 readiness watcher (`startStatusWatcher`, `:1008`, `:1481+`),
  builds provider chain + detector (`initProviderAndDetector`, `:1325-1369`), **`WithLevels(3)`
  hardcoded at `:1363`**; semantic tier decorator `:1379-1411` (ADR-086, default-off).
- Detection loop: readiness gate per tick (`:1451`, `evaluateReadiness` `:1579`,
  `evaluateSemanticAxis` `:1630`), then `runCommunityDetection` (`:1874-1915`) then
  structural+anomaly (`:1911`, `runStructuralAndAnomalyDetection` `:1845`). The gate exists
  because detection reads INCOMING_INDEX (`kvProvider`, `:1922-2238`); `GetEdgeWeight` is
  membership-correct 1.0/0.0 (`:2223-2238`, gh#674 fixed).
- LPA: `graph/clustering/lpa.go:154-253` (`DetectCommunities` — seeded rng `:223`, sorted input
  `:204-205`, write-then-prune with stale-over-empty per ADR-085 `:243-250`, `SaveCommunity` per community `:379`);
  `detectHierarchicalLevel` `:571-604` flattens previous level's members back to the full entity
  set — **levels 1..2 are re-runs, not a hierarchy** (gh#606 Finding 2, still true on main);
  `InferRelationshipsFromCommunities` `:673` is dormant (zero production callers — grep closed).
- Storage: `graph/clustering/storage.go` — `MembershipHash` `:31-48` (ONE shared definition,
  ADR-087 §4); `SaveCommunity` `:109-150` marshals the **unbounded `Members` array** plus one
  `entity.{level}.{entityID}` mapping Put per member (`:145-150`) — the gh#839 community half;
  level-scanning `GetCommunity` `:195-238` (gh#608 half); `Prune` `:399-470`; keys
  `communityKey`/`entityCommunityKey` `:520-525`.
- Community record: `graph/clustering/types.go:10-52` (`ID`, `Level`, `Members`, `ParentID`
  always nil, `StatisticalSummary`, `LLMSummary` (legacy field), `Keywords`, `RepEntities`,
  `SummaryStatus`, `SummaryTruncated`, `Metadata`).
- Summarizers: `graph/clustering/summarizer.go:34-52` (`StatisticalSummarizer` — keywords,
  template summary), `:498+` (`LLMSummarizer`; `WithContentFetcher` `:526-528` **never applied**
  — `processor/graph-clustering/component.go:2278` constructs with no options = gh#829).
- Enhancement worker (ADR-087): `graph/clustering/enhancement_worker.go:44-60` — watches
  COMMUNITY_INDEX as TRIGGER ONLY (`:239` WatchAll, `:332` handleKVEntry), sole writer of
  COMMUNITY_SUMMARIES; failed-record backoff `:37-42`.
- Query handlers: `processor/graph-clustering/query.go:18-57` — four subjects
  `graph.clustering.query.{community,members,entity,level}`. **Production consumers: exactly one**
  — graph-query's cache-miss fallback calls `.entity`
  (`processor/graph-query/graphrag.go:2277`); `.community` appears only in graph-query's static
  route table with zero callers (`router.go:40`; `grep -rn 'Route("community")'` → none);
  `.members`/`.level` have zero non-test callers (grep closed, excluding `.claude/worktrees`).

### Stores

- `COMMUNITY_INDEX` (`graph/constants.go:36`; catalog `graph/kvcatalog.go:132`, owner
  graph-clustering, derived class). Keys: `{level}.{communityID}` + `entity.{level}.{entityID}`.
- `COMMUNITY_SUMMARIES` (`graph/constants.go:43`; catalog `:133`), content-addressed
  `{level}.{membership_hash}` (`graph/clustering/summary_store.go:35-37,252`), worker-exclusive,
  ADR-068-compliant, no GC (gh#710).

### Consumers

- `processor/graph-query` (the ONLY production reader of COMMUNITY_INDEX):
  - Generation-based cache: `community_cache.go:18-60` (WatchAll `:69`, publish-at-sentinel,
    `parseCommunityKey` `:289-311`, in-memory `byEntity` membership mappings `:311-322`);
    supervisor `component.go:548-555`; #820 lease plumbing `graphrag.go:65-121`.
  - Summary serving view: `summary_view.go:108-281` (graphview over COMMUNITY_SUMMARIES),
    hash-join `summaryFor` `:283-301`; supervisor `component.go:557-563`.
  - `localSearch`: `graphrag.go:275-375` (entity→community members→entities→answer;
    `findCommunityWithFallback` `:2210-2256`; direct-storage fallback
    `fetchEntityCommunityFromStorage` `:2258-2312`; semantic fallback `:303-326`;
    availability contract "transient index_not_ready until community cache usable"
    `query.go:65`).
  - `globalSearch`: `graphrag.go:676-995` (strategy resolution `:629-674`; graphrag strategy
    semantic-first `:814-963` with community enrichment `findCommunitiesForEntities`
    `:1663-1708` (relevance = matchCount/len(Members)); tier-2 text-based community scoring
    `globalSearchTextBased` `:1368-1490` capped by `MaxTotalEntitiesInSearch` `:30-32`;
    degradation `stripUnavailableCommunityEnrichment` `:792-806`,
    `DegradedReason=community_cache_not_ready` `:206-209`; availability contract "community-only
    tier returns transient index_not_ready; lower tiers preserve results with
    community_cache_not_ready degradation" `query.go:61`).
  - `searchGraph`: `searchgraph.go:55-127` (server-side semantic fallback; `query.go:63`).
  - Answer synthesis from community summaries: `answer.go:67-78,184-259`.
  - Request/response shapes: `graphrag.go:124-247` (`Level` fields; `CommunitySummary`
    `community_id/summary/keywords/level/relevance/member_count/entities`).
  - `pathSearch`/PathRAG: `pathrag.go` — **zero community references** (grep closed); traverses
    graph-index relationships only.
- `gateway/graph-gateway/component.go`: GraphQL fields `localSearch(entityId,query,level)`,
  `globalSearch(query,level,maxCommunities,…)`, `searchGraph(…)` (`:1835-1847`); types
  `GlobalSearchResult`/`LocalSearchResult`/`CommunitySummary` (`:1857-1859`); routing
  `:1146-1152,1216-1223`.
- `processor/research-graph-classify/adapters.go:69-96`: decodes `community_summaries` as
  `[{summary}]` only — no membership/ID dependence.
- Diagnostics/doc mentions: `doc.go:109`, `pkg/resource/doc.go:35-48` (stale example),
  `service/message_logger_http.go:476` (comment), `natsclient/client.go:1351` (comment).

### E2E / instruments

- Direct KV readers: `test/e2e/client/nats.go:507-599` (`GetAllCommunities`,
  `GetCommunitySummaries`, `WaitForCommunitySummaryEnhancement`).
- `tiered_statistical.go:383-489` — structure validation (hard-fails only on
  all-singletons); **ground truth warn-not-fail at `:479-488`** (`community_ground_truth_passed`
  never observed >1/3; owner comment on #606); expectations
  `test/e2e/scenarios/community/types.go` + `validator.go`.
- `tiered_semantic.go` — enhancement wait + summary join; `tiered.go:295-352` stage table.
- B0/B2 instruments: `validate_partition_colocation.go` (#656 recorder, NOT a pass condition per
  ADR-086 Outcome), `validate_thematic_eval.go`.
- Corpus ID shape (`validate_entity.go:295-306`): e.g.
  `c360.logistics.content.document.operations.doc-ops-001`,
  `c360.logistics.maintenance.work.completed.maint-001`,
  `c360.logistics.sensor.document.temperature.sensor-temp-001` — the 4-part system prefixes
  partition this corpus into documents / maintenance / observations / sensor-docs, which is
  EXACTLY the type/status partition ADR-086 measured LPA converging to
  (ADR-086 "The pivotal why", colocation 0.60 finding).

### Specs / ADRs / docs claiming the territory (adjacent claims)

- `openspec/specs/graph-clustering/spec.md` (452 lines): requirements for edge synthesis
  (`:6-57`), non-destructive rebuild (`:59-126`), projection agreement (`:128-156`), semantic
  edges (`:157-292`), determinism (`:294-312`), summary store (`:313-376`), contract validation
  (`:378-400`), structural-ephemeral (`:401-452`). A majority is rewritten by this change.
- `openspec/specs/graph-query/spec.md`: `:268-298` (one admitted operation family; "local search
  before the community bucket exists"), `:300-347` (community watch generations; "lower tiers
  remain available honestly"), `:348-405` (summary serving view), `:72-157` (thematic synthesis
  context + summary join), `:439-459` (strategy reporting, gh#819 closed).
- ADRs: 061 (removal precedent + recoverability recipe), 086 (semantic tier; Outcome = honest
  negative on weight tuning), 087 (summary ownership split — binding keep), 085 (write-then-prune),
  068 (no TTL lifecycle), 090 (derived views need present consumers), 091 (no ownership systems).
- Active OpenSpec changes: `openspec list` → none touch this surface (agentic/rule changes only).
- Sister-repo asks: semsource is the measuring consumer (gh#829, #837→#838/#839, #823 all filed
  by semsource); ruling records zero shipped deployments consuming COMMUNITY_INDEX directly.

## 4. Consumer at birth (for surfaces the design would introduce)

- Prefix-derived group records in COMMUNITY_INDEX: consumers at birth = graph-query community
  cache (localSearch, globalSearch enrichment, tier-2 scoring) and the enhancement-worker
  trigger — both existing, measured above.
- Membership-by-prefix reads: consumer at birth = localSearch member loading; lane already
  exists (`graph.ingest.query.prefix`, paginated, `graph/query_prefix_types.go:10-58`).
- No other new exported symbol/port/subject/bucket is proposed; anything without one of the
  named consumers is excluded from the design.

## Same-class collision table

Proposed primitive: prefix-derived community group store.

| Dimension | Evidence |
|---|---|
| Semantic class | "Which group does entity E belong to at level L, and what is that group's descriptive metadata" — the entity-grouping-by-ID-hierarchy fact plus its summary enrichment |
| Owners | `graph-clustering` (LPA partition, COMMUNITY_INDEX writer); `graph-ingest` hierarchy inference (container entities + `hierarchy.*.member` triples, `graph/inference/hierarchy.go`); `graph-query` `hierarchyStats` (on-demand counts); `pkg/types.EntityID` (the pure function) |
| Catalogs | `graph/kvcatalog.go:132-133` (COMMUNITY_INDEX, COMMUNITY_SUMMARIES, owner graph-clustering); component port declarations `component.go:489-528`; config schema (`task schema:generate` output) |
| Status | Readiness: graph-index health gate on detection (`evaluateReadiness` `:1579`); graph-query cache generation publish/unpublish (#820 lease, `community_cache.go:130-160`); `GRAPH_STATUS` keys `graph-index`/`graph-embedding` (`:1630`) |
| Lifecycle | Write-then-prune per cycle (ADR-085); COMMUNITY_SUMMARIES accumulates, no GC (gh#710); teardown `Clear` only (`storage.go:477`); no TTL (ADR-068) |
| Ownership | Detector = sole COMMUNITY_INDEX writer; worker = sole COMMUNITY_SUMMARIES writer (ADR-087, structural); single active runtime instance default (ADR-090 §5); no leases/tokens (ADR-091 posture — binding constraint #4) |
| Readers | graph-query cache + summary view; graph-query storage fallback (`graphrag.go:2277`); e2e NATS client; NO sister-repo direct readers (ruling measurement) |
| Writers | `LPADetector.SaveCommunity`+`Prune` (only two, post gh#606 triage); worker → COMMUNITY_SUMMARIES only |
| Recovery | Fully regenerable derived data: next detection cycle rebuilds; restart rehydrates via KV watch bootstrap; poison latch (`latchGraphStatePoison` `:1820`) blocks detection against incompatible authority state |

## Adopter seam inventory (current surface, reached from outside this repo)

Adopter = a flow author or gateway consumer who has never opened these files.

1. **What must they know today?** (each item is a debt) — that `entity_id_edges` weights/caps
   exist and what values suit their corpus (they cannot know; ADR-086 measured even the framework
   authors mis-tuning them); that `level` 0–2 exist but are near-identical triplicates; that
   community membership is nondeterministic across restarts (pre-#658 it changed per run); that
   an oversized graph silently drops communities (gh#837/#838/#839); that `min_community_size`,
   `max_iterations`, `batch_size` exist (`batch_size` is inert). That is FAR more than two items —
   a standing design finding this change exists to delete.
2. **What happens if they do nothing?** They get the sibling/system-peer-dominated partition
   (ADR-086: type/status groups), 3× cost for fake levels, and on large graphs a partition that
   can silently fail to persist. Silent-wrong, the worst class.
3. **Where do they find out?** Partition quality: nowhere (the e2e signal is warn-only,
   `tiered_statistical.go:479-488`). Oversize: one ERROR log line (#837). Knob typos: load-time
   probe (good precedent, `component.go:119-137,192-209`).
4. **What SHOULD they have to know?** Nothing beyond what they already own: their entity ID
   scheme. The ruled direction converts every predict-a-weight knob into observation of a fact
   the adopter already declared at entity birth.

## Open evidence questions

- No measurement exists in-repo of prefix-group cardinality/size distribution on semsource-scale
  corpora (14,802 entities); the ruling's "a system filter yields the useful partition" is the
  owner's recorded field measurement and is taken as given.
- `graph.clustering.query.{community,members,level}`: zero in-tree consumers measured; per the
  purpose rule, sister-repo use over raw NATS cannot be fully excluded from this tree — flagged
  for owner confirmation rather than assumed dead.
