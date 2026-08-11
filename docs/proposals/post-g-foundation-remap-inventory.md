# Post-G foundation remap inventory and recommendation

**Status:** Post-G inventory candidate and architect recommendation awaiting owner rulings. This artifact does not
authorize implementation or issue administration.

**Exact merged baseline:** `480607d9` (`v1.0.0-beta.159-121-g480607d9`)

**Issue-queue snapshot:** 155 open issues on 2026-08-11. The adjacent
`post-g-foundation-remap-issue-census.tsv` records every issue number exactly once with its title and disposition.

**Inventory review:** The initial inventory received `INVENTORY PASS` against the exact baseline above. This corrected
candidate awaits independent re-review before owner ruling.

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

These are two bounded runtime invariants, exact coverage/release-truth gaps, and stale truth. They do not establish a
need for another general client, exported subject catalog, global readiness registry, generic storage redesign,
retention framework, or production-hardening program.

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

The collision result is consolidation already achieved, plus two concrete violations. #855 lets an incomplete
community partition masquerade as the saved set for prune. #875 lets an instance-blind legacy fallback compete with
`StoreRegistry` and read a reference owned by another store. Fixing either invariant does not authorize collapsing
the other distinct semantic classes or designing generic storage routing.

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

Representative downstream holdouts are optional evidence, not an exhaustive gate. A holdout blocks only when it
reproduces a framework contract regression in the candidate. Adoption debt is recorded after pinning and never causes
a shim or deprecated surface.

## Owner rulings requested

The architect recommends rulings 1-10 and 13 below; they are not binding until the owner approves them. Items 11 and
12 restate already-binding owner constraints and are included so the recommendation cannot weaken those boundaries.

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

## Recommended next checkpoint

Freeze this inventory and issue census by checksum, then design only the two runtime slices: #855's non-destructive
incomplete-partition rule and #875's instance-aware resolution. #391's deterministic full-fusion route, advertised
red-tier disposition, truth cleanup, and exact release remain bounded proof/release slices. Any newly discovered
finding must be classified against this inventory before it can expand the recommendation.
