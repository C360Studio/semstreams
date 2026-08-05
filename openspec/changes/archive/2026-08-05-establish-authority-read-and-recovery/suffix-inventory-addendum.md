# GS-01 `ENTITY_SUFFIX_INDEX` surface-inventory addendum

> **INVENTORY ONLY — UNREVIEWED.** This addendum corrects an omission in the reviewed GS-01 inventory. It records
> current state and measured unknowns. It selects no target state, owner, mechanism, or program increment.

## Evidence baseline

- Repository: `/private/tmp/semstreams-gs00`
- Branch: `codex/gs01-authority-recovery`
- Commit: `d322708a8ec360658d513a077fa99c9fe1ef5a81`
- Tree: `a1b5af62239ac10e2e11027739dd2d9d534b8362`
- Baseline worktree: clean
- GS-01 design SHA-256: `ce1e0da2a904a5b590e6f949d1a116c165d078e7d023a66e3ccbdb9f3d50743f`
- Revision-13 artifact: `/private/tmp/gs01-design-revision13.txt`
- Revision-13 SHA-256: `24f99453d108d4f8dd3b9b9879e7a0083a9ed6adc2eaf74bd3b5f3e124ff2103`
- External review SHA-256: `09988317d7deaeac1ef9cf42369c3721a37d4a71b704a16e230d50902bd55707`

The external review identifies the missing inventory at attachment lines 15–25. The existing GS-01 inventory records
only the prefix/suffix RPC spelling at `design.md:123-126`. It does not enumerate the durable suffix bucket, cache,
writers, delete path, caller chain, or owner-approved program disposition.

## Durable resource, catalog, owner, and guards

- `graph/constants.go:20-24` declares `BucketEntitySuffixIndex = "ENTITY_SUFFIX_INDEX"` as suffix-to-full-ID state
  owned exclusively by graph-ingest.
- `graph/kvcatalog.go:46-65,151-158` catalogs it as owner `graph-ingest`, class `derived`, retention `no-lifecycle`,
  write policy `owner-only`, posture `owner-creates`, history `1`, replicas `1`.
- `natsclient/kvspec.go:200-212` stamps the catalog description, history, and replicas into the NATS KV configuration.
  No TTL is created for a no-lifecycle descriptor.
- `graph/kvcatalog.go:207-244` derives `FrameworkOwnedBuckets`, `IsFrameworkOwnedBucket`, and `OwnerOf` from catalog
  policy.
- `graph/kvcatalog.go:247-267` defines the owner acquisition seam and reader must-exist seam. Exact search found no
  suffix-specific `OpenCatalogBucket` reader; suffix access is embedded in the owner.
- `processor/graph-ingest/component.go:1230-1263` acquires the bucket through the owner seam during `Start`; failure
  prevents graph-ingest from starting.
- `natsclient/kvspec.go:215-294` create-or-opens, reconciles retention/history, and fails the owner closed on error.
- `graph/owned_bucket_retention.go:14-82` includes the descriptor in the pre-start legacy-drift backstop.
- `service/ownership_service.go:171-193` runs that backstop through the common ownership substrate before component
  startup.
- `cmd/semstreams/main.go:162-169,441-471` and `cmd/e2e-semstreams/main.go:143-150,617-650` both reach the common
  ownership wiring and component registry.
- `componentregistry/register.go:162-180` registers graph-ingest, graph-gateway, and graph-query for both binaries.
- Generic rule writes are rejected at literal configuration validation and after runtime substitution at
  `processor/rule/config_validation.go:363-368` and `processor/rule/actions.go:1938-1944`.

Catalog ownership is review/call-site enforced rather than authenticated at runtime (`graph/kvcatalog.go:20-24`).

## Process-local suffix cache

- `processor/graph-ingest/component.go:630-634` holds the durable bucket and process-local cache.
- `processor/graph-ingest/component.go:1265-1276` requests a TTL cache with TTL `5m`, cleanup interval `1m`, declared
  maximum size `500`, and metrics identity `suffix_resolution_cache`.
- `pkg/cache/config.go:128-139` selects `NewTTL` without passing `MaxSize`.
- `pkg/cache/ttl.go:24-75,78-164` stores an unbounded map, starts a cleanup goroutine, expires entries on read or
  cleanup, and refreshes expiry on `Set`. The declared `500` therefore does not bound this cache.
- `processor/graph-ingest/component.go:1206-1209` closes the cache on component stop.
- `pkg/cache/metrics.go:21-88` exposes hit, miss, set, delete, eviction, and current-entry-count metrics under
  `component="suffix_resolution_cache"`.
- No suffix-tier latency, scan-duration, scan-count, collision, stale-hit, or durable-index-coverage metric was found.

## Writers, deletion, and recovery

- `processor/graph-ingest/component.go:3598-3608` derives exactly two durable keys from an entity ID: its final token
  (`instance`) and final two tokens (`type.instance`).
- `processor/graph-ingest/component.go:3611-3633` unconditionally `Put`s `{"id":"<full-ID>"}` for both keys. Errors
  are DEBUG-only and do not fail the authoritative operation.
- Exact call search found only committed merge (`component.go:2609-2618`), committed create/upsert/create-strict
  (`component.go:2714-2722`), and lazy scan backfill (`query.go:466-478`).
- There is no eager boot rebuild, watermark, failed set, reconciliation pass, or backfill-completion state.
- `processor/graph-ingest/component.go:2994-3020` deletes authoritative state first, invalidates the entity cache, and
  then runs best-effort suffix cleanup.
- `processor/graph-ingest/component.go:3635-3655` blindly deletes both derived keys and same-named cache entries. It
  does not verify that the durable value still points to the deleted entity; bucket errors are discarded; cache entries
  created for longer fallback suffixes or full IDs are not invalidated.
- Authority can therefore commit while suffix maintenance fails. Delete can leave a stale mapping or remove a shared
  mapping that currently points at another colliding entity.
- `docs/operations/17-predicate-cutover-clean-wipe.md:27-49` and
  `docs/operations/29-entity-id-contract-clean-cutover.md:81-116` include the bucket in destructive clean wipes.
- `docs/operations/framework-bucket-catalog.md:83-110` exposes it in adopter guidance for framework-owned buckets.

Current recovery is incidental: a durable miss scans authority and self-populates one found mapping. No complete
suffix recovery or coverage proof exists.

## Current lookup contract

`processor/graph-ingest/query.go:415-480` implements one request as:

1. Check graph-ingest's `ENTITY_STATES` snapshot readiness.
2. Apply a five-second handler-local timeout.
3. Reject malformed JSON or an empty suffix.
4. Check the five-minute process cache.
5. Check `ENTITY_SUFFIX_INDEX`.
6. On a durable miss, scan every active `ENTITY_STATES` key.
7. On a scan hit, best-effort backfill the durable index and cache.
8. Return `{"id":""}` for a completed no-match.

Detailed semantics:

- `processor/graph-ingest/query.go:24-55` registers plain NATS request/reply subject
  `graph.ingest.query.suffix` without a queue group.
- Cache and durable hits return without validating authority existence, current suffix relation, canonical ID shape,
  or entity bytes (`query.go:443-464,530-549`).
- A malformed durable value or backend error is transient `internal`; graph-query later collapses it to no resolution.
- Fallback calls `ENTITY_STATES.Keys` and returns the first key equal to the suffix or ending in `"."+suffix`
  (`query.go:551-573`).
- `natsclient.KVStore.Keys` applies its default five-second timeout and delegates to the pinned SDK
  (`natsclient/kv.go:35-42,66-71,460-475`).
- NATS Go v1.52.0 implements `Keys` with `WatchAll`, `IgnoreDeletes`, and `MetaOnly`, stops on either the initial nil
  marker or channel close, and sorts/compacts the collected slice
  (`$GOMODCACHE/github.com/nats-io/nats.go@v1.52.0/jetstream/kv.go:1372-1392`). Repository code cannot distinguish
  a complete nil-marker result from premature channel close.
- Matching is dot-boundary aware. `sensor-001` does not match an ID ending in `.temp-sensor-001`.
- A full ID can match by exact key during fallback.
- The durable index accelerates only one- and two-token suffixes; fallback accepts any dot-boundary suffix arity.
- A poisoned entity ID can resolve without state decoding; the subsequent entity read refuses poison. The active spec
  preserves this composition at `openspec/specs/graph-ingest/spec.md:610-616`.
- Readiness at `query.go:483-498` covers only authority bootstrap/watch state. There is no suffix-specific readiness.
- Graph-ingest's `GRAPH_STATUS` at `processor/graph-ingest/readiness.go:308-377` is a general backlog/readiness
  envelope and carries no suffix indexed/target revision.

## Timeout stack

- `natsclient.DefaultKVOptions` gives KV operations five seconds (`natsclient/kv.go:23-42`).
- The suffix handler nests a five-second timeout (`processor/graph-ingest/query.go:422-427`).
- Inbound request handlers default to 30 seconds and can be changed with
  `SEMSTREAMS_NATS_REQUEST_HANDLER_TIMEOUT` (`natsclient/request.go:15-44,337-369`).
- Graph-query gives alias and suffix separate two-second child timeouts and runs them sequentially
  (`processor/graph-query/entity_resolver.go:47-115`).
- The suffix caller therefore normally expires before graph-ingest's handler or KV budget.
- Open issue #833 records the general missing request-deadline propagation class. Its table describes the general
  graph-query default, while this specialized resolver has the independent two-second cap above.

## Resolver and downstream caller chain

`processor/graph-query/entity_resolver.go:11-115` resolves empty input, then strings with at least five dots as full
IDs, then alias, then suffix, then empty. All alias/suffix marshal, transport, classified-handler, and decode errors
collapse to empty. The exported resolver produces no non-nil error on these implemented paths.

Direct resolver uses:

- path-intent GraphRAG: `processor/graph-query/graphrag.go:332-405`, selected at `:599-671`;
- entity-lookup strategy: `processor/graph-query/graphrag.go:851-917`;
- `graph.query.globalSearch` and `graph.query.searchGraph`: `processor/graph-query/query.go:18-65`;
- semantic fallback after empty searchGraph result: `processor/graph-query/searchgraph.go:27-110`.

Production/external fronts that can reach those handlers:

- GraphQL `globalSearch` and `searchGraph` routing and schema at
  `gateway/graph-gateway/component.go:832-999,1582-1617`;
- GraphQL path/default registration and 60-second gateway request timeout at
  `gateway/graph-gateway/component.go:67-201,695-735,1779-1878`;
- agent tool `search_graph`, 90-second default, at `processor/agentic-tools/executors/search_graph.go:13-28,110-149`;
- research classify, 30-second default, at `processor/research-graph-classify/adapters.go:35-56,102-149`;
- research execute BM25 at `processor/research-graph-execute/adapters.go:20-52,268-338`, whose zero wire timeout
  becomes the NATS client's five-second default;
- E2E thematic evaluation at `test/e2e/scenarios/validate_thematic_eval.go:296-320`.

The MCP endpoint is a placeholder and does not expose graph reads
(`gateway/graph-gateway/component.go:1905-1925`). Exact searches found no direct production caller of the raw suffix
subject outside graph-query and no configured generic HTTP route for globalSearch/searchGraph.

An independent same-class resolver does not call this subject:

- `processor/research-graph-execute/handler.go:73-120` dispatches model-supplied `partial_id` seed references.
- `processor/research-graph-execute/subquery.go:255-277` dot-boundary matches only the upstream classifier candidate
  list and returns the first match in the raw slice order supplied to execution.
- `processor/research-graph-route/prompt.go:58-66` explicitly teaches the routing model to emit partial federated IDs
  for `walk_seeds` and promises backend resolution.
- The live component invokes resolution for `walk_seeds` at
  `processor/research-graph-execute/component.go:413-433` after rules dispatch
  `component.execute_subqueries.<loop-id>`.
- An unresolved reference is logged and dropped rather than failing execution; any drop marks the emitted
  `ExecutionOutput` degraded (`handler.go:73-105`, `component.go:281-290`).
- This resolver has no durable index, cache, bucket, or resolver-specific status. Its candidate order is its collision
  winner and each execution recomputes from its upstream classifier snapshot.
- The model does not see that same order or population. `processor/research-graph-route/prompt.go:149-188` sorts by
  relevance descending, breaks ties by entity ID, and truncates before rendering indices. Classification defaults to
  25 candidates while the prompt defaults to 10 (`research-graph-classify/config.go:66-67`,
  `research-graph-route/config.go:28-33,83-84`). Execution later passes the original untruncated raw slice at
  `research-graph-execute/component.go:431`.
- Under a partial-ID collision, execution can therefore choose a lower-relevance or undisplayed raw-list candidate.
  The same order mismatch also means a model-supplied `candidate_index` is applied to a different list than the one
  whose indices the prompt displayed (`handler.go:109-127`, `subquery.go:238-253`).

## Same-semantic-class collision inventory

- **`ENTITY_SUFFIX_INDEX`:** Partial identity to one full ID. Graph-ingest owns this cataloged derived H1,
  no-lifecycle, owner-only state; there is no suffix status. It uses two LWW keys per entity and delete blindly removes
  shared keys. Recovery is lazy miss backfill. The internal RPC reaches graph-query's NL strategies.
- **`suffix_resolution_cache`:** The same identity class, owned by the graph-ingest process. Its five-minute TTL is
  active, but the requested size 500 is unenforced. It can retain a prior winner after durable overwrite and longer
  fallback entries after canonical delete. Restart or TTL is its only recovery. It is invisible under the resolver.
- **`ENTITY_STATES` full scan:** Authority fallback for the same lookup, gated only by graph-ingest authority
  readiness. It performs dot-boundary matching and is lexical-first through the current SDK, but cannot distinguish
  complete and partial listing. Its latency/fault collapses to unresolved at the public reach.
- **`ALIAS_INDEX`:** Adjacent partial identity to canonical ID, owned by graph-index with its status family. Alias is
  attempted first and shadows suffix; stale-entry reconciliation is incomplete. It uses the graph-index repair family
  and reaches the same graph-query resolver.
- **Graph-query resolver:** Composition of full ID, alias, and suffix, owned by graph-query with no durable state or
  resolver status. Full-looking IDs bypass validation and all alias/suffix faults become unresolved. Its public reach
  includes GraphQL, the agent tool, research components, and raw NATS callers.
- **Research execute partial-ID resolver:** Candidate-scoped partial identity to full ID, owned by
  research-graph-execute with no durable resolver state or status. It returns the first raw-list classifier candidate
  whose ID exactly equals or dot-boundary-ends with the reference. The model sees a separately sorted and truncated
  list, so the selected collision winner may be lower-relevance or undisplayed. Unresolved references are dropped and
  degrade the output. Its public seam is the routing-model `walk_seeds` contract, not the raw suffix subject.

Current same-suffix outcomes:

- Concurrent writes for entities sharing `instance` or `type.instance` race on unconditional `Put`; last completed
  write wins.
- The archived keyed-dispatch design records this pre-existing LWW/no-CAS collision at
  `openspec/changes/archive/2026-07-06-graph-ingest-keyed-dispatch/design.md:138-149`.
- Cache, durable index, and fallback can name different winners.
- Deleting either colliding entity can remove the shared mapping even if it points at the other entity.
- A durable or cached hit is not authority-validated.
- For graph-ingest suffix resolution specifically, no ambiguity response, candidate list, collision marker, or stable
  documented winner contract exists. Research execute does have a candidate list and raw-list first-match rule, but
  its model prompt displays a differently ordered/truncated list.

## Tests, specs, ADRs, issues, and program claims

Present test coverage:

- handler success for instance and `type.instance`, no-match, empty bucket, malformed/empty request, and one
  dot-boundary partial miss (`processor/graph-ingest/query_test.go:20-218`);
- poisoned-state composition (`processor/graph-ingest/poison_scoping_test.go:525-555`);
- catalog and owner-only membership (`graph/kvcatalog_test.go:11-121`,
  `graph/owned_bucket_retention_test.go:14-25`);
- literal/substituted generic-rule rejection and application-bucket negative control
  (`processor/rule/entity_suffix_index_ownership_test.go:11-114`).
- candidate-scoped research partial-ID suffix, longer-suffix, exact, absent, empty, and non-boundary cases
  (`processor/research-graph-execute/subquery_test.go:282-326`), plus resolved/dropped seed accounting
  (`processor/research-graph-execute/handler_test.go:120-161`).

The handler tests construct only `entityBucket`, not the suffix bucket/cache, at
`processor/graph-ingest/component_test.go:1081-1113`; they therefore exercise fallback rather than cache or durable
hits. The real-NATS query integration starts the surface but asserts batch only
(`processor/graph-ingest/query_integration_test.go:19-115`).

Exact searches found no direct suffix E2E and no collision, stale-hit, delete-after-collision, cache-expiry,
malformed-index, index-failure, scan-order, large-cardinality, concurrent-request, or two-second-budget test.

There is indirect E2E reachability. `test/e2e/scenarios/tiered.go:320-324` registers all-tier `test-nl-path-intent`.
It sends GraphQL `globalSearch` queries containing `temp-sensor-001`, `humid-sensor-001`, and `cold-storage-1` at
`test/e2e/scenarios/tiered_structural.go:1712-1741`. Those queries can enter graph-query partial-ID resolution, but
alias-first behavior means they do not prove the suffix tier. The stage is also non-falsifiable for this capability:
zero passing probes appends a warning and the stage still returns nil at `tiered_structural.go:1798-1804`.

Durable claims:

- `openspec/specs/graph-retention/spec.md:15-39,145-177` requires the suffix bucket to remain no-lifecycle and
  owner-only.
- `openspec/specs/graph-ingest/spec.md:610-616` preserves ID-only resolution for poisoned entities.
- ADR-062 calls suffix resolution an adequate resolve primitive
  (`docs/adr/062-deterministic-graph-fusion.md:131-136`).
- The frozen inventory records the product reader, best-effort writes, authority fallback, trusted stale hits, LWW
  collisions, and missing watermark/repair (`docs/proposals/graph-state-read-write-inventory.md:112-151,416-421,
  580-591,710-738`).
- The owner-approved decision says `Suffix lookup: keep and validate stale hits` and rejects an authority scan as the
  large-graph default (`docs/proposals/graph-state-read-write-decision.md:45-52`).
- The canonical program records `ENTITY_SUFFIX_INDEX | Keep | Suffix resolution; graph-ingest owner | GS-05`
  (`docs/proposals/graph-state-read-write-program.md:391-414`). GS-05 is not started (`:507-529`).
- Closed issue #622 and the archived `framework-owned-bucket-guards` change established the current retention and
  write-ownership surface.
- Closed issue #562 measured the current write amplification at two suffix writes per entity; its cause and fix were
  unrelated to suffix correctness.
- Open issue #833 covers request-deadline propagation. No dedicated open suffix correctness/recovery issue was found.
- Revision 13 proposes suffix removal and authority scan at `design.md:1274-1324`, legacy cutover at `:1343-1402`,
  and its breaking gate at `:2003-2009`, crossing the current Keep/GS-05 program disposition.

## Exact searches closing empty categories

All searches ran at the baseline above.

- `rg -n 'ENTITY_SUFFIX_INDEX|BucketEntitySuffixIndex|graph\.ingest\.query\.suffix|suffix_resolution_cache' schemas`
  returned no matches.
- The same search under `configs` returned no matches.
- The same search across `test/**` and `*e2e*` returned no matches.
- That literal/internal-token result does not mean no public-path reach: `test-nl-path-intent` supplies partial IDs
  through GraphQL without naming the bucket, cache, or NATS suffix subject.
- Resolver/helper test searches under graph-query and graph-ingest test files returned no matches.
- Suffix-specific `OpenCatalogBucket` and literal `GetKeyValueBucket` reader searches returned no matches.
- Production `updateSuffixIndex(` search found only merge, create, and fallback backfill.
- Production `removeSuffixIndex(` search found only authoritative delete.
- Shipped config search for `graph.query.searchGraph` or `.globalSearch` returned no matches.
- Runtime searches for suffix rebuild, repair, reconciliation, watermark, status, readiness, degraded, and failure
  found no suffix-specific mechanism.
- Test searches for suffix collisions, ambiguity, staleness, and benchmarks returned no matches.

## Measurement gaps

No repository evidence currently records:

- current or target-scale authority cardinality;
- cache/index/fallback hit distribution;
- fallback key-transfer bytes or latency at target cardinality;
- p50/p95/p99 suffix latency against graph-query's two-second budget;
- concurrent request count or peak simultaneous scans;
- collision incidence for `instance` and `type.instance`;
- stale durable/cache hit incidence after overwrite or delete;
- cache memory under its unenforced declared capacity;
- best-effort write/delete failure counts; or
- a suffix coverage watermark or rebuild-completion signal.

## Adopter seam inventory

### Remote GraphQL consumer

- **Must know today:** Partial-ID resolution occurs only when classification selects entity/path intent; alias precedes
  suffix; resolution failures can fall through to broader semantic behavior.
- **If they do nothing:** Missing, timed-out, malformed, stale, or unavailable suffix resolution can appear as
  unresolved and produce an empty or alternative result rather than a suffix-specific error.
- **Where they find out:** Gateway routing and graph-query internals; the GraphQL schema does not state ambiguity,
  freshness, or the two-second internal budget.
- **Should know:** Query intent and explicit result/diagnostics only; no bucket, cache, scan, subject, or owner details.

### Raw NATS or embedded graph-query consumer

- **Must know today:** globalSearch/searchGraph can run full-ID, alias, then suffix resolution; alias and suffix each
  receive a separate two-second budget; suffix faults collapse to no match.
- **If they do nothing:** They inherit fallback and ambiguity semantics.
- **Where they find out:** `processor/graph-query/entity_resolver.go`, GraphRAG handlers, ADR-062, and package docs.
- **Should know:** A typed query operation and explicit outcome, not internal subjects or suffix-tier selection.

### External entity-ID producer

- **Must know today:** Final and final-two ID tokens enter a global collision domain; uniqueness is not validated.
- **If they do nothing:** Another entity can overwrite the mapping; deleting either collision participant can remove
  it; a warm cache can temporarily preserve another winner.
- **Where they find out:** Graph-ingest implementation and the archived keyed-dispatch design only.
- **Should know:** The six-part entity-ID contract, not derived-key construction or collision management.

### Deployment operator

- **Must know today:** Graph-ingest auto-creates the no-TTL bucket; maintenance is best-effort; no suffix coverage or
  readiness exists; clean wipes explicitly delete it.
- **If they do nothing:** Normal boot provisions it, while incomplete or stale state can remain hidden behind lazy
  fallback or trusted hits.
- **Where they find out:** Catalog notes, graph-retention spec, clean-wipe runbooks, and graph-ingest code.
- **Should know:** Component health and a capability-level readiness/recovery operation, not bucket/cache internals.

### Rule author

- **Must know today:** `ENTITY_SUFFIX_INDEX` is framework-owned and generic `update_kv` is rejected.
- **If they do nothing:** No effect; graph-ingest maintains it.
- **Where they find out:** Classified validation error, graph-retention spec, and framework-bucket catalog docs.
- **Should know:** Application buckets are writable; framework-owned graph buckets are not.

### Research routing model and prompt author

- **Must know today:** `walk_seeds` may identify a seed by partial federated ID; resolution is limited to the current
  classifier candidate list and first raw-list dot-boundary match wins. The prompt displays at most 10 candidates
  sorted by relevance while classification normally retains 25 and execution uses that original raw order.
- **If they do nothing:** An ambiguous reference can silently select a lower-relevance or undisplayed candidate; a
  prompt `candidate_index` can address a different raw-list entity; an unresolved reference is dropped and execution
  continues with degraded output.
- **Where they find out:** `processor/research-graph-route/prompt.go`, research execute handler/subquery code, and
  degraded output fields.
- **Should know:** The model should choose a visible candidate and receive explicit resolution diagnostics; it should
  not need the candidate resolver's matching algorithm or any graph-ingest suffix internals.
