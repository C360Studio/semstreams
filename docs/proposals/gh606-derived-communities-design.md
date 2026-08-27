# gh#606 Derived Communities — Design

Status: DRAFT for independent pre-owner design review, then owner acceptance. Depends on the
companion inventory (`gh606-derived-communities-inventory.md`) passing inventory review.
Binding constraints (owner ruling, gh#606 newest comment): (1) base partition = ID hierarchy;
(2) detection only as a default-off measured overlay over explicit edges; (3) GraphRAG/pathrag
advertised claims must stay defensible; (4) no new ownership system (ADR-091 posture); ADR-087
split kept unless the design shows it simplifies. gh#823/#819-class globalSearch quality items
are OUT of scope.

## 1. Design premises (each measured)

- P1 — The ID prefix partition is already the answer LPA approximates. ADR-086's white-box
  measurement: partition clusters "cleanly by entity type/status" (ADR-086 "pivotal why");
  the e2e corpus's 4-part system prefixes reproduce that exact grouping
  (`test/e2e/scenarios/validate_entity.go:295-306`); shipped tiers additionally feed LPA
  hierarchy-derived *explicit* edges (`enable_hierarchy: true`, `configs/statistical.json:593`,
  `configs/semantic.json:630`), so both edge tiers already encode ID structure.
- P2 — Hierarchy levels above 0 are fake (`graph/clustering/lpa.go:571-604` flattens;
  `WithLevels(3)` at `processor/graph-clustering/component.go:1363`).
- P3 — Membership is derivable through an existing bounded lane: `graph.ingest.query.prefix`
  (paginated, max_payload-aware, `graph/query_prefix_types.go:10-58`) and is already consumed
  that way by `hierarchyStats` (`processor/graph-query/query.go:469-545`).
- P4 — Production reads level 0 only today (`graphrag.go` leaves `req.Level` zero; gh#606 Finding 2
  verification note). **RESTATED (ADR-102, O-11 on #1095, 2026-08-26):** after the canonical reorder level 1
  (source) is served by default and LLM summaries gate there; level 0 stays available by request.
- P5 — The only production consumers of COMMUNITY_INDEX are graph-query's cache + the
  enhancement-worker trigger (inventory §3); zero sister-repo direct consumers (owner ruling
  measurement).
- P6 — The canonical prefix functions exist in ONE home: `pkg/types.EntityID.DeploymentPrefix` (2),
  `SourcePrefix` (3), `TaxonomyPrefix` (4), `TypePrefix` (5) and `PrefixLevel(n)` (renamed by #1095 slice A per
  ADR-102 d6; `SystemPrefix`/`DomainPrefix`/`PlatformPrefix` no longer exist).

## 2. Options considered

### O-A: Full storage elimination (no COMMUNITY_INDEX at all)

Everything derived at query time; summaries keyed by on-demand membership hash.
Cost: globalSearch tier-2 text scoring and summary joins would need per-query member
enumeration + keyword computation over every group (unbounded query-time cost); the #820
readiness contract (`community_cache_not_ready`, `index_not_ready`) loses its referent and must
be re-invented; the enhancement worker loses its KV-twofer trigger. Rejected: it moves
summarization cost into the query path and breaks binding constraint (3) surfaces.

### O-B (RECOMMENDED): Group-metadata store, membership never stored

COMMUNITY_INDEX keeps its name, bucket, owner, and KV-twofer role, but a record becomes
bounded group METADATA — no `Members` array, no `entity.{level}.{id}` mapping keys. Membership
is derived: entity→group is a pure function (`SourcePrefix` etc.); group→members is a bounded
prefix scan through the existing paginated lane.
Cost: a BREAKING record-schema change (pre-v1 fresh-state policy applies); localSearch member
loading becomes a prefix query (it gains pagination it never had).

### O-C: Keep storing membership, just derive it from prefixes

Minimal diff. Rejected: retains the gh#839 unbounded-value class, the mapping-key fan-out
(`storage.go:145-150`), the cache's membership projections, and the write churn — the exact
compensation machinery the ruling retires.

### O-D: Do nothing / tune LPA further

Rejected by the binding ruling and by ADR-086's measured honest negative.

## 3. The design (O-B)

### 3.1 Partition function and level semantics

`community(entity, level)` = the entity ID prefix at that level. Levels are REAL:

| Level | Prefix | Parts | Example (`acme.dep1.src.git.commit.a1`, canonical order org.platform.system.domain.type.instance) |
|---|---|---|---|
| 0 | `TaxonomyPrefix` | 4 | `acme.dep1.src.git` — one taxonomy within one source (source × taxonomy) |
| 1 | `SourcePrefix` | 3 | `acme.dep1.src` — one source (the federation triple); **served by default (O-11)** |
| 2 | `DeploymentPrefix` | 2 | `acme.dep1` — one deployment |

- Community ID = the prefix string itself. IDs are level-distinct BY CONSTRUCTION (different
  arity), which structurally kills the level-collision class (#608/#609 item 1: community ID ==
  seed entity ID identical across levels).
- Level 1 = source (RESTATED under ADR-102): the "same system" the gh#606 ruling measured as the useful
  partition is the system VALUE — under the canonical order exactly the three-position source prefix — so
  level 1 is served by default and LLM summaries gate there (O-11 on #1095, re-ruling Q8). Level 0 (source ×
  taxonomy) is the same set partition the previous order's level 0 was; its ID string reorders.
- The 5-part type prefix is deliberately NOT a community level (ruling names three); type
  grouping remains served by `hierarchyStats` / `extractEntityType`. (Owner question Q2.)
- Every entity belongs to exactly one community per level, deterministically, at birth, O(1),
  restart-stable. `ParentID` becomes real for free (a level-L community's parent is its own
  prefix shortened) — derivable, so still not stored.

### 3.2 Where derivation runs (and where it does not)

The producer stays in `processor/graph-clustering`'s existing interval loop
(`runDetectionLoop`), replacing `LPADetector.DetectCommunities` with a `PrefixPartitioner`:

1. `GetAllEntityIDs` from ENTITY_STATES (as today, `component.go:1971-1981`), sorted.
2. Group by `TaxonomyPrefix`/`SourcePrefix`/`DeploymentPrefix` — O(N) string work, no edges, no
   iterations, no rng.
3. Per group: `member_count`, `membership_hash` (= `clustering.MembershipHash(members)`,
   unchanged single home, `storage.go:31-48`), keywords + statistical summary via the existing
   `StatisticalSummarizer` (reads member entity states through the poison-latched querier,
   `component.go:1419-1423`), bounded `rep_entities` (deterministic selection; see Q5).
4. Write-then-prune exactly as today (stale-over-empty per ADR-085 retained; `Prune` keyed by the new
   record set), with **write-on-change**: a record byte-identical to the stored one is skipped
   (dissolves the #661 churn class; trivial now that records are small).

Explicitly NOT in graph-ingest: derivation MUST NOT re-enter the ingest hot path (owner history
note; the 2026-01-05 monolith breakup, commit `a60ef433`, moved clustering out of it). Ingest
continues to know nothing about communities. Incremental at-birth maintenance of the store is
NOT needed for v1 — the partition is O(1) at birth by definition; the STORE is refreshed on the
existing interval. (A watch-driven incremental producer is a possible later optimization, not
part of this design.)

Consequences for the producer's dependency surface:

- The graph-index readiness gate no longer guards partitioning (it read INCOMING_INDEX; the
  partitioner reads only ENTITY_STATES keys/values). The gate (`evaluateReadiness`) and
  `allow_ungated_reads` remain SCOPED TO THE ANOMALY LEG only
  (`runStructuralAndAnomalyDetection` still reads index topology). Cold-start communities now
  appear as soon as entities exist.
- `kvProvider` edge machinery, `EntityIDProvider`, `SemanticEdgeProvider`, the seeded-shuffle
  determinism machinery, and `staleness_at_detection_ms` (as a detection property) are no longer
  needed by the base path.

### 3.3 Storage and watch shape

`COMMUNITY_INDEX` record (BREAKING, fresh-state; key `{level}.{prefix}`):

```text
CommunityGroup {
  id              string   // the prefix; also the key suffix
  level           int      // 0=taxonomy (source × domain) 1=source 2=deployment (ADR-102 order)
  member_count    int
  membership_hash string   // clustering.MembershipHash over the enumerated members
  keywords        []string
  statistical_summary string
  rep_entities    []string // bounded (<= MaxRepEntities)
}
```

- No `Members`, no `entity.{level}.{id}` keys, no `SummaryStatus`/`LLMSummary` legacy fields.
  Every field is bounded by content, not by graph size → the gh#839 community half dissolves.
- Membership reads: entity→group is computed (`pkg/types` accessors); group→members goes
  through `graph.ingest.query.prefix` (paginated). localSearch member loading inherits the
  pagination + `MaxTotalEntitiesInSearch`-class caps instead of an unbounded `loadEntities` of a
  stored array.
- `COMMUNITY_SUMMARIES` is UNCHANGED (binding constraint 4): worker-exclusive,
  content-addressed `{level}.{membership_hash}`, ADR-087 correctness property intact. The
  worker, on a trigger, enumerates members by prefix (its querier seam grows a prefix-list
  capability), summarizes, and keys by the hash of what it actually enumerated — a drifted
  membership misses the join and self-heals next cycle. The read join gets CHEAPER: graph-query
  joins via the record's stored `membership_hash` without enumerating members
  (replaces the per-query `MembershipHash(comm.Members)` at `summary_view.go:296`).
- graph-query community cache: same generation/watch/lease machinery (#820 contract untouched);
  `applyUpdate` drops the membership-mapping projections (`community_cache.go:311-322`);
  `getEntityCommunity` becomes prefix-compute + record lookup. The direct-storage fallback
  `fetchEntityCommunityFromStorage` (`graphrag.go:2258-2312`) is deleted — membership cannot be
  stale, and record-miss degrades (see 3.5).

#### kv-or-stream (4-test, recorded per skill)

Path: partitioner → COMMUNITY_INDEX group records → {graph-query cache, worker trigger}.

1. Restart: rehydrate current facts → KV.
2. Fan-out (two readers) → KV.
3. Apply is fast + idempotent → KV.
4. It is a FACT (current group metadata), not a request → KV.

No conflicts; no new stream. The worker-trigger obligation set from the skill's KV-owner rule
(idempotent desired-state apply, failed-work repair, visible degradation) is already satisfied
by ADR-087's content-addressed idempotent writes + failed-record backoff
(`enhancement_worker.go:37-42`). No new communication path is introduced.
Other decision skills: orchestration-check — not triggered (no new rule/component boundary; the
producer remains one component-internal loop). new-payload — not triggered (no new payload-
registry type; KV records and existing reply shapes only). query-pattern — no new query access;
membership reads reuse the admitted prefix operation graph-query already orchestrates.

### 3.4 The overlay contract (LPA/detection)

Binding: detection survives only as an optional overlay over EXPLICIT relationship edges,
default-off, shipping only once a fixture can measure it (the current ground-truth check —
never better than 1/3, warn-only — cannot).

- `EntityIDEdgesConfig` sibling/system-peer synthesis (`component.go:86,110-117`;
  `entityid_provider.go`) is redundant by construction and is DELETED, per the ruling.
- Contract for any overlay: input = explicit edges from graph-index (plus, if ever revived, the
  ADR-086 semantic tier); output = overlay records in a namespace the serving path does not
  read; gate = a fixture that can fail; it never writes the base partition records and never
  becomes a readiness dependency of graph-query.
- Recommended disposition (Q1 for the owner): REMOVE the LPA path from the tree now —
  `lpa.go` detector, provider chain, `semantic_edge_provider.go`, the weights/caps config
  blocks, and the detection-determinism machinery — recording an ADR-061-style recoverability
  recipe (git + the contract above). Rationale: ADR-061 already rejected "keep dormant" as a
  half-measure; a compiled default-off overlay carries the whole provider/weight/readiness
  surface and its bug classes (#672's stale caches live in exactly that code) for a capability
  that BY RULING cannot ship until a fixture exists. The alternative (keep compiled,
  default-off) is presented honestly as the literal reading of "survives"; it costs ~2.5k LOC of
  live-wired second-writer machinery and keeps #672/#618-class surfaces open.
- Note either way: with `enable_hierarchy: true`, shipped tiers' "explicit edges" include
  prefix-derived container/sibling edges (`graph/inference/hierarchy.go`), so an overlay run on
  those tiers partially re-derives the base partition; an honest overlay fixture disables
  hierarchy sibling edges or measures against explicit domain edges only. Graph-ingest hierarchy
  inference itself is OUT of scope for this change.

### 3.5 Operator/config surface (adopter seam: prediction knobs deleted)

Deleted (each added to `removedConfigFields` so an operator's stale config FAILS AT LOAD with
replacement guidance, `component.go:249-276` precedent): `entity_id_edges` (whole block),
`semantic_edges` (whole block), `min_community_size`, `max_iterations`, `batch_size` (inert
today — inventory §3). Retained: `detection_interval` (store refresh cadence), `enable_llm`,
`enhancement_workers`, `enable_anomaly_detection`+`anomaly_config`,
`allow_ungated_reads` (re-documented as anomaly-leg-only), startup knobs. The `entity_watch`
input port is dropped with `batch_size` unless the owner wants the incremental producer later.

Behavioral floors replacing knobs: groups of every size get records (singletons included —
their community is a true fact; Q6); summary/keyword generation applies to groups with ≥2
members; `MaxRepEntities` stays a framework constant.

### 3.6 Migration and blast radius (pre-v1 fresh-state policy)

BREAKING, no migration/alias/dual-reader paths: COMMUNITY_INDEX value schema + key population
change; downstreams start on newly provisioned NATS storage (ADR-090 release-premise). If
retained deployed state is ever discovered, stop for a separate owner-reviewed design.

- In-repo: graph-clustering (producer rewrite, config surface, spec rewrite), graph-query
  (cache simplification, localSearch membership path, summary join via stored hash, fallback
  deletion), gateway (no schema shape change — field names/types unchanged; `community_id`
  VALUES become prefixes, `level` gains real semantics), e2e (below), docs (below),
  `doc.go:109`, `pkg/resource/doc.go` example.
- `graph.clustering.query.*` subjects: `.entity` loses its only production caller (fallback
  deleted); `.community`/`.members`/`.level` have zero callers today. Recommend deleting all
  four and the router's `community` route (`router.go:40`) — grep-for-the-consumer — subject to
  Q7 (sister-repo raw-NATS use cannot be excluded from this tree).
- Sister repos (hands-off; communicate only): semsource — the affected consumer via
  gateway/searchGraph; visible deltas are community_id values, real level semantics, partition
  shape (system groups), deterministic membership; no field/shape changes, no code change
  required; notify via its asks file. semops/semconnect/semdev/semteams/semdragon — no known
  community consumers (ruling measurement). semboids — clustering CPU cost collapses.
- Breaking-change e2e gate (HARD RULE): `task e2e:statistical` AND `task e2e:semantic` green
  before the breaking commit lands; they cover the ingest→partition→graphrag→answer path.

### 3.7 What e2e asserts instead of the ground-truth check

The partition is now a pure function of ingested IDs, so the warn-only coherence check is
replaced by assertions that CAN fail:

1. **Partition exactness (hard fail):** compute expected groups from the corpus's entity IDs;
   assert COMMUNITY_INDEX records match exactly (ids, levels, member_count, membership_hash).
   Replaces `validateCommunityGroundTruth` (`tiered_statistical.go:477-489`) and the
   `community/` expectation machinery. Note: the current expectations pass BY CONSTRUCTION at
   level 0 on this corpus (inventory §3, corpus ID shapes), so this is strictly stronger.
2. **Write stability (hard fail):** two consecutive detection cycles over an unchanged graph
   produce zero new COMMUNITY_INDEX revisions (proves write-on-change; the #661 class).
3. **Summary join** (unchanged): enhancement wait + hash join (`WaitForCommunitySummaryEnhancement`).
4. **GraphRAG surfaces** (unchanged assertions): localSearch membership+summary; globalSearch
   enrichment maps ranked entities to their prefix groups; #820 degradation reasons.
5. **Retired:** `validate_partition_colocation.go` leaves the statistical/semantic tiers (its
   subject — does the partition co-locate themes — is answered by construction now); it is
   preserved only as part of a future overlay fixture. The thematic answer-quality eval
   (`validate_thematic_eval.go`) stays — it measures synthesis, which is partition-independent
   (ADR-086 Outcome: recall ceiling is upstream of the partition).

### 3.8 Advertised-claims defensibility table (binding constraint 3)

| Advertised claim | Where | Verdict under this design |
|---|---|---|
| GraphRAG: "entities pre-organized into communities with summaries; LLM gets organized context" | `docs/concepts/09-graphrag-pattern.md` | HOLDS — groups + statistical/LLM summaries + digests unchanged in shape |
| globalSearch semantic-first ranking; community enrichment is decoration; entities_only & other non-community strategies | `graphrag.go:629-674,814-963`; ADR-061 finding (b) | HOLDS — untouched; enrichment mapping becomes exact instead of cache-lag-prone |
| #820 operation-local readiness: localSearch `index_not_ready` until cache usable; globalSearch community-only tier transient `index_not_ready`; lower tiers degrade `community_cache_not_ready` | `query.go:61,65`; `graphrag.go:792-806` | HOLDS verbatim — the cache/generation machinery is retained (a reason O-A was rejected) |
| localSearch: entity → its community's members + summary + answer | `graphrag.go:275-375` | HOLDS, strengthened — membership exact at birth; record-miss degrades to members+no-summary instead of erroring |
| PathRAG: bounded traversal of explicit relationships | `docs/concepts/10-pathrag-pattern.md`; `pathrag.go` | HOLDS — zero community dependence (measured); only the comparison table's "GraphRAG traverses community structure" wording needs a touch |
| "Communities emerge from data — you don't define them manually" | `docs/concepts/07-community-detection.md` | **ADVERTISING MUST CHANGE** — base communities are the ID hierarchy you declared at birth (still data, not config — no knob defines them — but not emergent topology). Emergence is the overlay's claim, unshippable until measurable |
| "Discovery without questions / reveal structure you didn't know existed" | `07-community-detection.md` | **ADVERTISING MUST CHANGE** — same split: organization (base) vs discovery (overlay, gated) |
| "Community membership changes signal anomalies" | `07-community-detection.md` | **ADVERTISING MUST CHANGE** — base membership never changes (IDs are immutable); the claim was already unimplemented (anomaly path reads structural/similarity, not communities — inventory §3) |
| Tier table: "Statistical adds community detection" | `README.md:156`, `docs/concepts/00-real-time-inference.md` | **CHANGE WORDING** — "community organization (ID-derived) + summaries"; detection is no longer what the tier adds |
| Tier 2 improves communities | already corrected (gh#606 Finding 1; docs honest) | HOLDS (no regression — semantic tier never affected the partition) |
| `COMMUNITY_INDEX: community records with members and summaries` | `doc.go:109` | **CHANGE** — metadata records; members derived |
| GraphQL schema: localSearch/globalSearch/searchGraph fields, `CommunitySummary`, `entityIdHierarchy` | `gateway/graph-gateway/component.go:1835-1859` | HOLDS — no field shape changes; `level` docs updated to taxonomy/source/deployment (ADR-102 order); `entityIdHierarchy` becomes the same fact the partition serves (one home) |

### 3.9 Related-issue disposition table

| Issue | Disposition | Reason |
|---|---|---|
| #606 (epic) | RESOLVED by this design | fake WithLevels(3) replaced by real prefix levels; ownership half already closed by ADR-087 |
| #465 adaptive edge synthesis | DISSOLVES | no synthesized edges exist to make adaptive |
| #608 wrong-level markFailed + LLM-down permanent disable | RE-SCOPES to the LLM-probe/retry half | level-blind `GetCommunity` scan dies with the storage rewrite (level-distinct IDs by construction); the LLM startup-probe retry bug is untouched by this design and stays open |
| #618 embedding readiness consumer + anomaly fails open | RE-JUDGE after Q1 | if the overlay is removed, clustering's `KeyGraphEmbedding` watcher (`component.go:1630`) goes with it and the "no consumer" premise returns; the anomaly fail-open half is community-independent and stays |
| #661 SaveCommunity churn | DISSOLVES (largely) | write-on-change on small records is specified (§3.3); e2e assertion 2 proves it |
| #672 stale typePrefixCache/systemCache | DISSOLVES | EntityIDProvider deleted; the partitioner enumerates fresh each cycle |
| #701 multi-community query expansion | RE-SCOPES onto stable derived groups | the evacuation residual must be re-measured against the prefix partition (single-system corpora may collapse to one L0 group, changing the expansion question entirely) |
| #710 COMMUNITY_SUMMARIES GC | UNAFFECTED | store contract unchanged; accumulation rate likely DROPS (stable memberships → fewer distinct hashes) |
| #829 ContentFetcher never wired | RE-SCOPES onto stable derived groups | still real; fix lands in summarizer wiring (`component.go:2278`) regardless of partition source — and pays off more, since group membership is now stable enough for summaries to persist |
| #839 payload ceilings | community half DISSOLVES | no unbounded Members array in any KV value; membership rides the paginated prefix lane; the entity-batch half of #839 is untouched and stays open |
| #588 watcher consolidation | RE-SCOPES (community half), UNAFFECTED (embedding half) | COMMUNITY_INDEX still has 2 watchers (cache + trigger) but both are far cheaper; consolidation onto a shared view remains a valid follow-up at lower priority |
| #837 (closed) oversized record aborts pass | NOT REINTRODUCED | records bounded by construction; the #855 classified-rejection fence and write-then-prune discipline are retained |
| #607/#617 (closed) clobber/resurrection | NOT REINTRODUCED | single-writer topology unchanged: partitioner sole COMMUNITY_INDEX writer, worker sole COMMUNITY_SUMMARIES writer, no shared keys, no CAS needed |
| #823 globalSearch defaults below summarize threshold | OUT OF SCOPE | per binding ruling (semsource quality item, stays out of this epic) |
| #819 Strategy never reaches wire | OUT OF SCOPE (already CLOSED) | design does not touch strategy reporting; spec requirement "Successful global search reports the terminal strategy" must stay green |

### 3.10 Artifact plan (for /opsx:new after owner acceptance)

- One OpenSpec change (`gh606-derived-communities`): proposal + tasks + deltas rewriting
  `openspec/specs/graph-clustering/spec.md` (edge-synthesis, semantic-edge, determinism
  requirements replaced by: derived partition, level semantics, bounded records,
  write-on-change, summary-store unchanged, anomaly-scoped readiness) and touching
  `openspec/specs/graph-query/spec.md` (membership derivation in localSearch, stored-hash
  summary join, fallback deletion; #820 scenarios unchanged).
- One ADR (draft title: "Community partition is derived from entity identity; detection is a
  measured overlay"): records the irreversible decision, the overlay re-entry contract, the
  cross-repo visibility (community_id values, level semantics), and its relationship to
  ADR-061 (pattern), ADR-086 (mechanism disposition per Q1), ADR-087 (kept intact).
- Docs: rewrite `07-community-detection.md`; touch `09-graphrag-pattern.md`,
  `10-pathrag-pattern.md` (one table row), `00-real-time-inference.md`, `README.md:156`,
  `doc.go:109`, `pkg/resource/doc.go` example.

## 4. Open questions — RULED (owner, 2026-08-23, triage-docket session)

- Q1 (largest) — **RULED: REMOVE.** LPA + providers + `semantic_edge_provider.go` + weights/caps
  config + detection-determinism machinery leave the tree now, with an ADR-061-style
  recoverability recipe and the overlay re-entry contract (§3.4) recorded in the ADR. Owner
  basis: LPA over this entity-ID model is re-derivation of the IDs — waste of compute; every
  production consumer consumes the partition, which the derived base replaces exactly; removal
  is recoverable, and by this epic's own ruling an unmeasured overlay cannot ship anyway.
- Q2 — **RULED: NO fourth level**, conditional on coverage, which is confirmed: type-level
  grouping is already served on demand by `hierarchyStats` (`processor/graph-query/query.go:469-545`)
  / GraphQL `entityIdHierarchy`.
- Q3 — **RULED (default adopted): keep the `graph-clustering` name.** Bounds the diff;
  anomaly/structural machinery genuinely remains.
- Q4 — **RULED (default adopted): removed-key load failures** for `entity_id_edges`,
  `semantic_edges`, `min_community_size`, `max_iterations`, `batch_size` via
  `removedConfigFields`, with replacement guidance. No silent acceptance.
- Q5 — **RULED: deterministic text-bearing rep selection.** The base producer is index-free;
  the partition path has no graph-index readiness dependency. #702's query-time relevance
  selection unaffected.
- Q6 — **RULED (default adopted): record singleton groups.** Membership is a true fact;
  localSearch on a singleton entity must not error.
- Q7 — **RULED: delete all four `graph.clustering.query.*` subjects** and the router's
  `community` route. Residual caution: raw-NATS sister use cannot be proven absent from this
  tree — the deletion note in the sister notification (§3.6) covers it, and semsource
  confirmation is being sought in the open channel as cheap insurance.
- Q8 — **RE-RULED by O-11 on #1095 (2026-08-26): LLM summarization gates to level 1 (source) by default** — the served
  level; a deliberate cost decision the fake hierarchy never allowed.
