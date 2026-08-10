# Post-Foundation-B Slice C community-generation supervisor inventory

**Status:** Post-Slice-B implementation inventory. This records the current baseline and the already approved minimal
Slice C target. It does not reopen the owner rulings or mark C.1-C.7 complete.

**Baseline:** `1db4c39e1d0fc95f96657ba757b8966cada6212a` (`feat(graph-query): unify operation port contract
(#920)`). HEAD and the merge base with `origin/main` were both this commit when the inventory was captured.

**Accepted evidence:**

- Frozen inventory: `docs/proposals/post-foundation-b-graph-foundation-remap-inventory.md`, SHA-256
  `c87cdf12506ac62272f340f975f14a27f28e78307207a6aae554ede595a99040`.
- Owner-reviewed roadmap: `docs/proposals/post-foundation-b-graph-query-contract-closure-roadmap.md`, SHA-256
  `ff23db51ce7bf6e3d45da09a1706bf70ee548ae5e6aa2b12201ceeae64c4f343`.
- Active owner approval: `openspec/changes/post-foundation-b-graph-query-contract-closure/approval.md:3-31`.
- Active Slice C task truth: `openspec/changes/post-foundation-b-graph-query-contract-closure/tasks.md:49-62`.

This checkpoint updates the accepted pre-implementation inventory only where Slice B changed the graph-query surface.
The approved generation-supervisor ruling remains internally consistent with the merged baseline.

## Problem statement

Slice B made all sixteen `graph.query/v1` responders unconditional and made `localSearch` return classified transient
`index_not_ready` before the community cache is usable (`processor/graph-query/query.go:45-65`,
`processor/graph-query/component_test.go:240-259`). It did not make the `COMMUNITY_INDEX` projection generation-safe.

The remaining defect is projection authority after watch loss. The current cache mutates one process-lifetime map,
sets a permanent `ready` latch on a nil watch value, and neither distinguishes a closed updates channel from the
initial-enumeration sentinel nor unpublishes stale data. Bucket-presence polling cannot repair a watch that closes while
the bucket remains present (`processor/graph-query/community_cache.go:20-50,62-100`;
`processor/graph-query/component.go:496-529,724-747`).

## Current lifecycle and state

- `Config` already has generic startup attempts/interval and `RecheckInterval`; the default recheck is five seconds
  (`processor/graph-query/component.go:47-58,93-112`). Slice C needs no new knob.
- `Component` owns one `CommunityCache` and one bucket-presence `resource.Watcher`; summary watcher state is separate
  (`processor/graph-query/component.go:135-170`).
- Start creates the cache before installing all responders, then configures presence callbacks for
  `COMMUNITY_INDEX`. If present, it opens once and starts `WatchAndSync`; if absent, the presence watcher polls
  (`processor/graph-query/component.go:486-535,616-633`).
- `resource.Watcher` monitors successful resource checks, not the health of a `WatchAll`. Its state machine calls
  `OnLost` only when the bucket check fails (`pkg/resource/watcher.go:14-39,106-227`).
- `WatchAndSync` obtains exactly one `WatchAll`, applies every update directly to the shared maps, and sets `ready=true`
  on any nil receive (`processor/graph-query/community_cache.go:62-100,102-189`). Because the receive omits the
  channel `ok` value, channel close is repeatedly interpreted as the initial sentinel.
- `disableGraphRAG` logs bucket loss but deliberately does not stop the watch or clear cache readiness/data
  (`processor/graph-query/component.go:739-747`). Watch failure while the bucket remains does not cause another open.
- Stop cancels the component context, waits for component goroutines, stops both presence watchers, and finally stops
  the cache watchers (`processor/graph-query/component.go:542-613`; `community_cache.go:436-444`). There is no
  generation token that distinguishes orderly cancellation from unexpected generation loss.
- Component health is only component-started plus NATS-connected, with accumulated handler error state. It carries no
  community-generation status (`processor/graph-query/component.go:317-348`).

## Current projection and readers

`CommunityCache` keeps three maps behind one mutex: level-qualified communities, entity-to-community membership, and
optional enhanced summaries. Only the first two belong to Slice C; summaries remain Slice D
(`processor/graph-query/community_cache.go:20-50`).

- `GetCommunity` returns the stored community pointer under an independent read lock
  (`community_cache.go:326-336`). There is no lease or final validation; the pointer can outlive cache authority.
- `GetEntityCommunity` resolves the entity mapping and community under one independent read lock (`:338-356`). It
  cannot prove the same watch remains current before response return.
- `GetCommunitiesByLevel` copies pointers and sorts by ID (`:358-378`). A later watch loss cannot invalidate the copied
  slice.
- `GetAllCommunities` copies pointers across all levels and sorts them (`:380-401`). It has the same loss window and no
  generation identity.
- `IsReady` and `Stats` read the permanent bool and shared-map counts (`:403-425`). `ready` never becomes false after a
  usable watch is lost.

Production community reads fan out from those getters:

- `localSearch` gates on `IsReady`, then uses the cache and direct clustering fallback
  (`processor/graph-query/graphrag.go:215-315,2013-2111`).
- semantic/global enrichment finds communities for result entities, enriches digests, resolves summaries, and builds
  sources (`graphrag.go:659-801,1473-1519,1778-1839,1931-1957,2187-2204,2299-2344`).
- the community-only global text tier reads communities and statistics, scores them, enriches summaries, and builds
  the response (`graphrag.go:1185-1305`).
- entity, path, temporal, spatial, semantic, and default global strategies may call the shared enrichment path before
  returning (`graphrag.go:320-378,850-1140,1931-1957`).
- `searchGraph` composes `globalSearch` and a final direct semantic fallback
  (`processor/graph-query/searchgraph.go:53-110,188-224`). It does not own a separate community projection.

The approved operation boundary is therefore handler-level: only `localSearch`, `globalSearch`, and `searchGraph`
may return community-derived data. Their internal global-search strategies share the reader sites above.

## Current query behavior

### Local search

`localSearch` now has a stable responder and returns transient `index_not_ready` when the cache pointer is nil or its
ready latch is false (`processor/graph-query/graphrag.go:215-237`). After that one check, every subsequent cache read,
summary read, entity load, synthesis step, and return is unvalidated (`:239-315`). A generation can be lost during the
request and stale community data can still be returned.

On an entity membership miss, `findCommunityWithFallback` calls `graph.clustering.query.entity` directly
(`processor/graph-query/graphrag.go:2013-2111`). This bypass is an existing query fallback, not cache recovery. Today it
can serve without proving that the cache generation is still usable. Slice C must keep it inside the same request lease
and final validation and must not use its answer to seed or repair the cache.

### Global search

Global search tries lower tiers before the community text tier (`processor/graph-query/graphrag.go:584-801`). Semantic,
entity, path, temporal, and spatial successes can be enriched from community state through `enrichGlobalResponse`.
The text fallback checks only whether the cache pointer is nil (`:804-834`); Slice B now constructs that pointer on
every successful Start, so an absent, staging, or lost watch can fall into the shared maps and return invented empty or
stale success. The exact baseline behavior is pinned as unmarked empty success
(`processor/graph-query/operation_inventory_test.go:128-155`).

#### Owner-ruling target enrichment contract

The request already has every control the target needs (`processor/graph-query/graphrag.go:98-125`):

- omitted `include_summaries` means true;
- explicit `include_summaries=false` suppresses community summaries and community-based answer synthesis on every
  lower-tier strategy, except when the independently evaluated auto-summary threshold triggers;
- omitted `summarize_threshold` uses the existing default 50; a value less than or equal to zero disables
  auto-summary; a value greater than zero triggers when the lower-tier semantic hit count exceeds that value
  (`:49-52,109-116,712-758`);
- `include_sources=true` requests source rows, but only each row's community-dependent attribution requires community
  state; and
- `include_relationships` requests entity relationships and never requests community state by itself.

GraphQL advertises all four controls, but its current variable transformer forwards `includeSummaries`,
`includeRelationships`, and `includeSources` while omitting `summarizeThreshold`
(`gateway/graph-gateway/component.go:1231-1252,1674-1685`). Slice C owns forwarding that existing argument as
`summarize_threshold` and adding a focused gateway variable-transform test. This corrects an advertised argument; it
adds no field or surface.

The target applies those controls uniformly across the current strategy topology:

- `localSearch` is always community-required and has no enrichment flags (`graphrag.go:64-69,215-315`).
- successful path, entity-lookup, pure-semantic, temporal, and spatial strategies currently call
  `enrichGlobalResponse` unconditionally (`graphrag.go:320-378,850-901,904-1020,1023-1140,1931-1957`). Slice C makes
  their community summary/answer enrichment obey `include_summaries`; none has an independent auto-summary trigger.
- on the default GraphRAG semantic-hit path, the threshold is evaluated independently. A threshold crossing requests
  community summary/answer enrichment even when `include_summaries=false` (`graphrag.go:704-758`). Below the threshold,
  only omitted/true `include_summaries` requests that enrichment (`:760-784`).
- `include_sources=true` preserves source rows for lower-tier entities. Community membership may decorate a row with a
  community ID, but generation unavailability removes only that decoration; entity ID and lower-tier relevance remain
  (`graphrag.go:785-790,2299-2344`).
- `include_relationships=true` preserves the lower-tier relationship result independently of generation state
  (`graphrag.go:785-787,1293-1296,2271-2297`).
- the community-text tier always needs a generation because communities select the candidate entities, even when
  summaries and sources are disabled (`graphrag.go:1185-1305`).
- `searchGraph` passes the same request through `globalSearch`, returns a non-empty global result unchanged, and only
  then composes the direct semantic fallback (`processor/graph-query/searchgraph.go:53-110`). It inherits this target
  contract.

C.5 emits `community_cache_not_ready` only when a valid lower-tier result is returned and the target contract requests
community-derived enrichment, but no usable generation can be acquired or finally validated. The response preserves
lower-tier entity IDs, full entities, semantic scores and digests, relationships, count/strategy, and source rows that
do not depend on community state. It removes only community summaries, community-based synthesized answer/model,
community membership/ID/label attribution or decoration, and the portions of source attribution that require community
state. A digest retains its ID, type, semantic relevance, and any label derived independently of community membership.

A community-required local or text path has no independent result to preserve and returns transient
`index_not_ready`. Community synthesis does not run without a valid generation, so this condition cannot also produce
an answer-synthesis reason; with a valid generation, the existing answer-synthesis reasons remain unchanged. No new
intent field is introduced.

### SearchGraph

`searchGraph` first runs `globalSearch`, then on empty success executes direct semantic search. A successful semantic
fallback currently returns `strategy=semantic_fallback`, `degraded=true`, and
`degraded_reason=global_search_empty_semantic_fallback` (`processor/graph-query/searchgraph.go:53-110,188-224`). Slice C
must preserve that strategy. It uses `community_cache_not_ready` only when unavailable requested community enrichment
is why the direct semantic result is the honest lower tier; other empty-global fallback causes retain their existing
reason.

## Degradation vocabulary and consumer ownership

The wire already has open string fields; Slice C adds a bounded producer value, not a new field:

- graph-query currently produces `answer_synthesis_timeout`, `answer_synthesis_cancelled`, and
  `answer_synthesis_error` through centralized constants/classification
  (`processor/graph-query/answer.go:88-109,190-233`). SearchGraph separately produces
  `global_search_empty_semantic_fallback` (`processor/graph-query/searchgraph.go:188-224`).
- `GlobalSearchResponse` and `LocalSearchResponse` comments describe only answer-synthesis degradation
  (`processor/graph-query/graphrag.go:71-95,127-150`). Slice C owns the producer constant and the precise
  `community_cache_not_ready` semantics above; local generation absence remains an error, not degraded success.
- the `global_search_degraded_total{reason}` metric already accepts the open reason string, but its Help and method
  comment enumerate only answer-synthesis values (`processor/graph-query/metrics.go:144-149,271-276`). Slice C owns
  adding the bounded community reason to that vocabulary and metric help, plus focused producer/metric tests.
- live adopter documentation currently calls these fields answer-synthesis-only
  (`docs/concepts/09-graphrag-pattern.md:162-174`). Slice C owns correcting that live vocabulary. The beta.44-to-beta.45
  migration guide accurately records that historical three-value addition
  (`docs/operations/migration-beta44-to-beta45.md:1-83`); the new downstream migration notice belongs to Slice G rather
  than rewriting beta.45 history.
- graph-gateway exposes both fields as untyped GraphQL result fields and preserves the success bytes after one canonical
  unwrap (`gateway/graph-gateway/component.go:1331-1355,1674-1696,1868-1897`). Slice C needs a projection fixture for
  the new value, not a gateway-owned enum or classifier.
- the direct thematic E2E response records the raw reason (`test/e2e/scenarios/validate_thematic_eval.go:165-206`). C.7
  may assert the new producer outcome there or in the focused statistical path.
- research-classify's SearchGraph adapter copies `degraded` and `degraded_reason`
  (`processor/research-graph-classify/adapters.go:61-74,144-170`), while research-execute's BM25 adapter decodes only
  entity digests and drops both (`processor/research-graph-execute/adapters.go:266-317`). Slice E owns canonical unwrap
  and preservation of degradation metadata through embedded research consumers; Slice C does not redesign them.
- the unadmitted agentic `search_graph` wrapper copies and renders the reason
  (`processor/agentic-tools/executors/search_graph.go:200-255`). Slice F deletes that complete wrapper, so Slice C must
  not extend its vocabulary, formatting, tests, or documentation.

## Durable ownership, status, and configuration negatives

- The KV catalog declares `COMMUNITY_INDEX` as a derived bucket owned by `graph-clustering`; graph-query is a reader,
  not a writer (`graph/kvcatalog.go:37-56,126-133,209-245`). Slice C does not add or change a bucket.
- `GRAPH_STATUS` is a separate operational bucket whose current producers are graph-index, graph-embedding,
  graph-ingest, and rule (`graph/kvcatalog.go:69-74`; `graph/readiness/watcher.go:39-70`). There is no clustering or
  community readiness key.
- Exact negative searches for `Key(Graph)?Clustering`, `KeyCommunity`, `COMMUNITY_(READY|STATUS)`,
  `community_generation`, `community_supervisor`, and `generation_retry` under `graph/readiness`,
  `processor/graph-query`, `configs`, and `schemas` returned zero matches at the baseline.
- Existing schemas expose ports, query timeout, and max depth only
  (`processor/graph-query/component.go:296-315,787-806`; `schemas/graph-query.v1.json:1-23`). The Go config's existing
  `recheck_interval` is reused; Slice C adds no supervisor or retry configuration.
- Existing community cache/search metrics describe hits, misses, request counts, strategy, and degradation. There is
  no generation metric contract (`processor/graph-query/metrics.go:11-46,55-113,205-235`), and Slice C adds none.

## Same-class collision inventory

The semantic class is a component-owned, in-process current-state projection used to answer reads while a KV watch is
fully enumerated and healthy. Adjacent owners are evidence and constraints, not permission to create a new shared
abstraction in Slice C.

- **Semantic class:** Graph-query's community cache (`community_cache.go:17-50`), the embedding vector cache
  (`graph/embedding/storage.go:172-190`), and generic `pkg/graphview.View` (`pkg/graphview/view.go:135-169`) are three
  projection spellings. Slice C changes only graph-query's private spelling.
- **Catalogs:** `COMMUNITY_INDEX` is cataloged as graph-clustering-owned derived state
  (`graph/kvcatalog.go:126-133`). Supervisor ownership cannot imply write or creation authority.
- **Status:** The community cache has a process-local `ready` bool (`community_cache.go:45-50,403-425`); graphview has
  internal view state (`pkg/graphview/view.go:150-168,319-353`). Slice C adds no `GRAPH_STATUS` owner or public fact.
- **Lifecycle:** `resource.Watcher` retries presence (`pkg/resource/watcher.go:106-227`); graphview supports bootstrap,
  failure, explicit restart, and Stop (`pkg/graphview/view.go:201-317`). Presence recovery is not watch recovery, and
  Slice C is not a lifecycle-framework generalization.
- **Ownership:** Graph-clustering owns durable writes; graph-query owns its local serving projection. Graphview is
  explicitly constructed with no global registry (`pkg/graphview/view.go:135-139`). Slice C keeps one component owner
  and private generations.
- **Readers:** Graph-query getters and the `localSearch`/`globalSearch`/`searchGraph` paths above consume community
  state. Embedding reads only while warm and watcher-healthy (`graph/embedding/storage.go:879-929`). All community
  readers need one request-scoped generation proof.
- **Writers:** Graph-clustering writes the bucket; graph-query's watch loop writes only its memory maps
  (`community_cache.go:102-189`). Slice C adds no direct KV writer, repair write, or second durable owner.
- **Recovery:** Embedding invalidates on close/poison and falls back to KV
  (`graph/embedding/storage.go:776-877`); graphview fail-closes and can re-bootstrap
  (`pkg/graphview/view.go:225-283,422-490`). Slice C reuses the observed invariants, not either public type or its
  fallback policy. Community replacement uses fresh maps.

ADR-081 classifies both community and embedding caches as serving projections and records future graphview conversion
(`docs/adr/081-graph-view-subscription.md:35-55,197-205`). That adjacent claim does not authorize Slice C to migrate
multiple owners or change `pkg/graphview`; the approved slice is a private graph-query correction.

## Existing tests and missing evidence

- Unit cache tests cover level-qualified identity, updates/deletes, deterministic ordering, and replacement within one
  shared map (`processor/graph-query/community_cache_test.go:43-305`). They do not drive watch lifecycle or generation
  replacement.
- Component tests provide a mock bucket seam and pin the stable pre-bucket `localSearch` responder/error
  (`processor/graph-query/component_test.go:28-120,240-259,920-947`). The mock does not expose controllable
  `WatchAll` generations.
- The operation inventory test explicitly pins the current unmarked empty `globalSearch` and `searchGraph` fallback
  (`processor/graph-query/operation_inventory_test.go:128-155`). This is failing-first evidence to invert in C.5.
- Real-NATS lifecycle tests cover absent-then-created and delete/recreate buckets, but not watch close while the bucket
  remains. One recovery test uses an arbitrary `time.Sleep`
  (`processor/graph-query/component_integration_test.go:262-426`, especially `:405-425`).
- Real-NATS community tests cover cross-level update/delete projection and the summary join
  (`processor/graph-query/component_integration_test.go:709-854`;
  `processor/graph-query/community_summary_wire_integration_test.go:142-163`). They do not prove staging isolation,
  sentinel publication, exact unpublish, late old-generation exclusion, or final validation.
- SearchGraph tests pin the current semantic fallback shape but do not drive community-generation transitions
  (`processor/graph-query/searchgraph_test.go:66-135`).

## Stale and adjacent documentation

- `processor/graph-query/README.md:95-129,328-356` documents per-operation input ports and says GraphRAG is disabled
  until bucket presence. Slice B replaced that port shape and made responders stable; Slice C replaces the presence
  lifecycle with usable-generation outcomes.
- `processor/graph-query/doc.go:102-117` repeats the pre-Slice-B disabled/enabled model.
- The current graph-query spec correctly keeps summary readiness independent and preserves the statistical floor, but
  has no generation-safe partition lifecycle (`openspec/specs/graph-query/spec.md:118-152`). The active change delta
  supplies that target; summary lifecycle remains Slice D.
- Existing integration-test comments say handlers become available after the bucket appears even though Slice B made
  them unconditional (`processor/graph-query/component_integration_test.go:255-261`).

## C.1-C.7 baseline assessment

- **C.1 — not implemented.** Existing tests cover map correctness and bucket appearance, not generation lifecycle.
  Add failing-first, explicitly synchronized tests for absent, staging, sentinel including empty, usable update/delete,
  same-bucket watch loss, replacement, late N events, and orderly cancellation.
- **C.2 — not implemented.** One process-lifetime shared map and one `WatchAll` are started through bucket presence.
  Prove component-lifetime must-exist open/`WatchAll` retry, monotonic IDs, fresh private maps, and no N-to-N+1
  seed/copy/retention.
- **C.3 — not implemented.** Pre-sentinel updates are immediately reachable, close is mistaken for sentinel, and no
  exact unpublish exists. Prove sentinel-only publication, exact-N unpublish before retry, generation-checked
  updates/exits, and orderly cancellation that is not loss.
- **C.4 — not implemented.** Getters independently lock and return pointers/slices without generation identity or a
  final check. Prove one private lease per community-backed request and validation of that same generation immediately
  before return, including direct clustering fallback.
- **C.5 — partial only.** Stable `localSearch` already returns transient `index_not_ready`; global/searchGraph still
  pin unmarked empty success. Community-required local/text paths must return `index_not_ready`; lower-tier results
  follow the owner-ruling target contract, preserve all non-community result data, remove only community-derived
  portions, and report `community_cache_not_ready`. Slice C also forwards the existing GraphQL
  `summarizeThreshold` argument and pins that correction with a gateway transform test.
- **C.6 — baseline negative satisfied; preserve it.** Add no readiness producer/key, service, bucket, stream, public
  metric contract, retry knob, or other external surface.
- **C.7 — not implemented.** Existing real-NATS recovery evidence does not cover same-bucket watch loss and contains
  a sleep. Run focused race and real-NATS integration with explicit synchronization only, then independent SemStreams
  review.

## Approved minimal target state

This is a restatement of owner-approved target state, not a new option or recommendation:

1. Graph-query owns one private `COMMUNITY_INDEX` supervisor for the component lifetime. Each must-exist open and
   `WatchAll` attempt allocates a monotonic generation ID and fresh private community/membership maps.
2. Updates and deletes before the initial sentinel affect staging only. The sentinel atomically publishes the complete
   generation, including a valid empty one. Close or error before sentinel publishes nothing.
3. Unexpected loss unpublishes exactly the current generation before retry, even while the bucket remains. Late N
   updates/exits cannot mutate or invalidate N+1. Component cancellation is orderly.
4. Every community-backed request acquires one private generation lease and validates that same generation immediately
   before return. Direct clustering fallback stays within the lease and does not seed or repair cache state.
5. `localSearch` and the community-only text tier require a usable generation and return classified transient
   `index_not_ready` without one. Global/searchGraph lower tiers remain available and set `degraded=true`,
   `degraded_reason=community_cache_not_ready` when requested enrichment is unavailable. They preserve entity IDs,
   entities, semantic scores/digests, relationships, count/strategy, and non-community source data. They remove only
   community summaries, community-based answer/model, community membership/ID/label decoration, and source attribution
   dependent on community state. SearchGraph's direct semantic fallback retains `strategy=semantic_fallback`.
6. Reuse the component's existing `RecheckInterval`. Do not alter `COMMUNITY_SUMMARIES`; its independent generation
   supervisor is Slice D. Add no public API, config, status, catalog, infrastructure, or metric contract.
7. Forward graph-gateway's existing `summarizeThreshold` argument as `summarize_threshold`; this is an advertised-
   argument correction with a focused transform test, not a new surface.

## Adopter seam inventory

The adopter is a developer outside this repository calling the admitted GraphQL operations or declaring the existing
`graph.query/v1` component port. They have never opened `community_cache.go`.

### What must they know?

1. `localSearch` can return classified transient `index_not_ready`; retry is safe once the optional view becomes
   usable.
2. `globalSearch` and `searchGraph` can return a lower-tier success with
   `degraded_reason=community_cache_not_ready`. Valid entities, IDs, semantic scores/digests, relationships, and
   non-community source data remain; only unavailable community-derived portions are absent.

They must not know bucket names, watch sentinels, generation IDs, retry cadence, owner components, or which cache
getter a strategy uses.

### What happens if they do nothing?

An ordinary lower-tier caller continues to receive useful global/searchGraph results. A local caller that does not
retry sees a typed transient failure rather than stale data or transport no-responder. A caller that ignores degradation
still receives valid lower-tier data, but must not infer that omitted community enrichment was fully searched.

### Where do they find out?

- `localSearch`: typed runtime/GraphQL error class and code, then the graph-query spec and migration note.
- `globalSearch`/`searchGraph`: response fields `degraded` and `degraded_reason`, then the graph-query spec.
- component authors: the already admitted `graph.query/v1` port contract; Slice C adds no declaration.

### What should they have to know?

Only the two operation outcomes above. The framework owns the bucket, initial-enumeration boundary, generation, and
retry. The caller is not asked to predict readiness, a subject, a delay, or a cache epoch; graph-query acts, observes
the actual watch outcome, and classifies the response.

### Open implementation evidence

- Prove each lower-tier strategy honors the target contract. On generation acquisition or final-validation failure,
  preserve lower-tier entities/IDs, semantic scores/digests, relationships, and non-community source rows; remove only
  community summaries, community-based answer/model, community membership/ID/label decorations, and source attribution
  that depends on community state.
- Prove omitted/default, disabled, and triggered thresholds; explicit `include_summaries=false`; and forwarding of the
  existing GraphQL `summarizeThreshold` argument with a focused gateway transform test.
- Prove `community_cache_not_ready` reaches GraphQL and the existing metric label without being rewritten or mistaken
  for answer-synthesis degradation.
- Defer embedded research preservation to Slice E and verify the Slice F deletion leaves no agentic consumer that
  silently depends on the new value.
