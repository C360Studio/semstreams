# ADR-073: The graph's three-tier ingestion + retention contract

## Status

**Proposed — 2026-07-08. Design-only (no code). Adversarially reviewed (4
code-grounded lenses); revised to fold the findings + a decentralized-reactive
reclamation model. Pending re-review before Accept.** Grounded in the
[10-product data-lifecycle audit](../proposals/graph-retention-10-product-audit.md)
and a verified prior-art pass (git, Datomic, Cassandra, Kafka, TSDBs). The review
found **no path that reintroduces D1-banned blind eviction**; the changes below are
precision/framing corrections (the ADR-055 grounding discipline), not a redesign —
the skeleton (three tiers by shape, the per-facet retention unit, firehose-by-time,
blob-by-refcount, D1) survived attack.

**Supersedes this ADR's own earlier "entity DataClass policy layer" draft.**
**Amends and completes** [ADR-068](068-graph-retention-deletion-lifecycle.md).
The prior art validates 068's mechanism *family* and this ADR supplies the policy
layer it lacked — but it **refines two load-bearing 068 mechanisms**, so the
relationship is amendment, not mere completion (stated precisely so a follow-on
cannot claim compliance under both readings):
- **068 D3 (cleanup primitive):** 068 mandates a *single shared per-entity reverse
  index* maintained on the write path. ADR-073 **replaces that with a per-owner
  choice** — each derived-store owner keeps its *own* reverse-knowledge, OR the
  tombstone carries the entity's last-known triples (the open fork in §4). There is
  no longer one central reverse index.
- **068 D5 (GC worker):** 068 frames a fixed-interval central sweeper as the
  mechanism for index cleanup, tombstone purge, and blob GC. ADR-073 **demotes the
  central sweeper to an off-by-default *backstop*** (orphan mark-sweep + purge +
  blob GC); the **primary identity-tier index cleanup is decentralized, reactive,
  per-owner** (§4).

Everything else in 068 stands (D0 per-producer watermark, D1 guardrail, D3
refuse/cascade + refcount, the tombstone). **Leaves ADR-054 intact** (indexing
profile is a retrieval dial, not retention). Mechanics live in the follow-on
OpenSpec change and the `graph-retention` spec.

## Context

The graph grows without bound. The **framework provides no reclamation of the
identity tier** — confirmed across the audit: every product that owns durable
graph entities *except* semspec (whose bespoke `lesson-curator` is the sole
exception) grows unbounded there, and every such product *except* the read-only
semstreams-ui exhibits the same anti-pattern — **firehose-cardinality data written
as identity entities** on the reclamation-less tier (semops CAP append-evidence,
semlink command-intents/GeoChat, semteams/semdev loop & trajectory entities,
semdragon `dagunit`, semconnect SystemEvents, semboids' firehose-cadence boid key).

The prior art is unambiguous: **two orthogonal idiom families, never mixed on one
tier** — *reachability* (aware; identity + relationships) and *time/cardinality*
(blind; firehose). The tier split is already real at the config level: TTL/MaxAge —
**and, in the shipped guardrail, all `MaxBytes>0`** — are banned on graph KV
(`natsclient/kv.go:158-166`, fail-closes at boot), while streams and operational/aux
KV take retention freely. The gap is that the identity tier has **no reclamation
mechanism**, and the framework offers a write verb (`triple.add`, the
`AppendEvidence` behavior) that lands firehose growth on it.

## Decision

**The graph is a three-tier store; the tier is a property of the data's *shape*,
enforced at write time; and identity-tier reclamation is decentralized and
reactive, not a central sweeper.**

### 1. Three tiers by shape
- **Firehose (TIME):** per-event, high-cardinality data → JetStream streams
  (`MaxAge` + a `MaxBytes` backstop — the audit found `MaxBytes:0` is a systemic
  bad default; on the *stream* tier a **binding** `MaxBytes` is fine, that tier is
  the blind one) or bounded in-memory buffers. Never a graph entity.
- **Identity (REACHABILITY-family):** durable entities + relationships →
  `ENTITY_STATES`, where **TTL/MaxAge/all `MaxBytes>0` remain banned** (D1;
  reconciles ADR-068's "non-binding MaxBytes OK" prose to what the code actually
  enforces — *no* size cap on the identity tier). Reclaimed as §2/§4.
- **Owned blob (REACHABILITY/refcount):** bulky content → ObjectStore via
  `ContentStorable`, reclaimed by refcount from owning entities. Host-FS bypass
  (semteams, semsource media) is a violation, not an alternative.

### 2. Identity-tier reclamation is TRIGGERED, not swept — and the criterion is
**referenced-ness, never age.** "Reachability" was overloading three mechanisms;
name them:
- **Triggered cascade (near-term, buildable — ADR-068 D3):** a root's *declared
  death-trigger* fires → its satellites cascade. This is the primary mechanism.
- **Die-with-last-referrer (refcount / refuse-if-referenced — ADR-068 D3):** a
  satellite shared by a DAG survives until its **last** referring root dies; for a
  tree this reduces to all-or-none. (Replaces the earlier "all-or-none cluster,"
  which conflicted with 068's refuse-if-referenced for DAG-shared satellites.)
- **Orphan mark-sweep (deferred, off-by-default backstop — ADR-068 D5.4):** a
  full reachability sweep for what triggers miss. It needs the graph-wide
  consistent read ADR-068 D0 says the substrate cannot cheaply give, so it stays
  optional/opt-in, exactly as 068 already scopes it. **The bounded-graph claim
  rests on triggered cascade + refcount, not on this sweep.**

Age is never a legal identity-tier criterion. On this tier the only legal
reclamation is **cardinality/compaction** (keep-latest / bounded-N per key); a
wall-clock time-window is legal **only after decomposition** onto the firehose
tier (a facet-level age-drop on `ENTITY_STATES` would be D1's anti-pattern one
layer above the bucket config, invisible to the boot guardrail).

### 3. The retention unit is the FACET (predicate-group), not the entity.
One key legitimately mixes firehose-cadence facets (semboids position),
must-not-lose transitions (`flock.lifecycle.phase`), and reachability-existence.
The per-predicate **write mode** (`pkg/ownership`: `ModeReplaceOwned`,
`ModeAppendEvidence`, `ModeCASTransition`) *describes the accretion behavior* — but
it is today an **ownership/overlap-arbitration primitive with zero reclamation
semantics, read nowhere on the graph-ingest write path.** Retention *builds on* it:
each mode must be *wired* to a reclamation idiom (new work) — `ReplaceOwned`/
`CASTransition` (single-valued) → compact-to-latest; `AppendEvidence` (multi-writer
append) → a declared cardinality/compaction bound **or reject** (§5). Attribute the
append-vs-replace behavior to the RPC verb (`triple.add` vs `update_with_triples`),
not to a retention meaning the mode does not yet carry.

### 4. Reclamation is DECENTRALIZED and REACTIVE (the Prometheus-registry shape) —
the right target, with a built exemplar, but *not* an existing capability. The
audit's clearest lesson is that centralizing index-cleanup is the gh#433 failure;
each index-owner is the authority on its own store and should self-clean. But the
re-review found the substrate is only half there, so state it honestly:

- **Watch + delete-delivery exists; self-clean mostly does not.** `graph-index` and
  `graph-embedding` watch `ENTITY_STATES` async, each with its own `revlag.Watermark`
  (ADR-068 D0), and the KV watch *does* deliver delete events. But today only
  `OUTGOING_INDEX` (+ the dead entity's own `INCOMING` key) and **`graph-index-temporal`**
  actually reclaim on a delete; `NAME`/`PREDICATE`/`ALIAS`/`CONTEXT`/spatial/
  embedding-vector/embedding-dedup/`OASF_RECORDS` silently keep stale entries
  (gh#433 realized — the very rot this ADR fixes, *not* a capability it reuses).
  `graph-clustering` is **timer-driven rebuild** (`Clear()` + full rescan), *not*
  watch+watermark — its `COMMUNITY_INDEX` self-heals by rebuild but carries no
  watermark and cannot feed the purge gate as-is. Two stores are actively broken:
  spatial's delete handler is a no-op, and `oasf-generator` watches with
  `IgnoreDeletes()` (never even sees the tombstone).
- **The load-bearing decision: the tombstone must carry what owners need to
  self-clean.** A bare-key KV delete carries only the key — enough for an
  entity-ID-keyed store (embedding-vector: a one-line wire-up), useless for a
  composite/value-keyed one (`NAME`=sha256(name), `PREDICATE`=(pred,id),
  spatial=geohash). So each owner either (a) keeps its **own reverse index** —
  which **`graph-index-temporal` already does** (`TEMPORAL_INDEX_REVERSE`, cleaning
  correctly on a bare-key delete: the built proof the pattern works), or (b) the
  **tombstone carries the entity's last-known triples** (a body), freeing owners
  from reverse records. **This decision, not the registry shape, is the crux** — the
  model is only buildable once it is made (it is what git-for-data / IVM systems
  call the retraction's *payload*; see Related work).
- **Owners register as retention participants.** The registration precedent is sound
  (`pkg/ownership.RegisterOwner`, `pkg/lifecycle.Manager.Register`). The framework's
  only central knowledge is the participant *set* + each participant's progress
  cursor, to gate tombstone **purge** on the **min across per-producer cursors**
  (never a single global one — deadlocks, ADR-068 D0 / gh#431). A rebuild-based
  owner (graph-clustering) either grows a cursor or is excluded from the gate with a
  documented per-consumer resurrection risk. Thin interface
  (`RetentionParticipant{ Cursor() revision; Compact(ctx) error }`), not a cleanup
  API. `lesson-curator` is the in-fleet precedent for the *owner-self-maintains
  shape* (its criterion is staleness, not the identity-tier criterion — §Precedent).
- The central surface shrinks to **decide the death-trigger → emit the tombstone
  (with the payload the decision above requires) → (optional) orphan-sweep
  backstop**; cleanup is owner-local. **This is net-new wiring for ~8 stores**
  (`graph-index-temporal` is the template), not reuse of an already-self-cleaning
  fleet.

### 5. Enforcement keeps the firehose off the identity tier — as a STATIC contract
rule, spanning ALL triple-writing verbs. The review found `normalizeProjection` is
*not* the single seam and is bypassed by the very append verbs that matter
(`triple.add`/`add_batch`). So enforcement is **net-new logic** that must gate
every write verb (or a shared commit path): **`AppendEvidence`-shaped writes to the
identity tier require a declared cardinality/compaction bound in the projection
contract (or a decomposition contract routing the accretion to the firehose tier),
else reject.** It is a *static* rule (contract membership + write mode), **not** a
runtime cardinality check (cardinality isn't write-time-observable, and a slow
append like semops CAP's one-per-5-min must not be false-rejected — the ADR-051
dead-gate risk). Ship it via the **owner-lease template** — `enforce_*` config flag
default false, observe-and-warn (PR-3) → config-gated reject (PR-5), reject
classified `ErrorInvalid` so ADR-060's class-on-the-wire stops it being retried as
transient; the owner-lease arc did exactly this on this seam and the same ~11
callers without breaking them. **Precondition:** all graph writes flow through the
mutation/projection API — the raw `KV.Put` bypass (semdragon) and the dead-but-
latent `graph/datamanager` direct writer must be closed first.

### 6. Roots: the axis is TRIGGERED-DEATH vs NEVER-DIE (not run vs permanent).
The earlier "run vs permanent" binary was over-fit to the agentic products; the
audit has three shapes: **run-scoped** (semteams/semdev/semspec), **domain-lifetime**
(semops track/asset/command-intent, semboids boid-by-cull, semlink vehicle — die on
a *domain* trigger), and **never-die** (semsource corpus, audit ledgers). Unify:
a root declares a **death-trigger** (run-end, despawn, cull, expiry, retire) → its
cascade fires (§2); a **never-die** root declares none. Under-declaring a never-die
root as triggered is data loss — the never-die set MUST be explicit and complete.

### 7. Loop-execution entities are DECOMPOSED, not classified (resolves the deferred
open question). They are born at firehose cardinality (one per agent call), so by
§1/§5 they do not belong on the identity tier at all — "root or satellite?" is a
false binary that both break. A compacted current-state summary lands on the
run/chain root; the per-step execution detail is a **windowed firehose** (which the
24h `AGENT_LOOPS COMPLETE_{loopID}` twin and `AGENT_TRAJECTORIES` already are —
today's immortal graph copy is redundant with a correctly-windowed operational one).
The write boundary rejects loop-as-identity-entity.

### 8. The never-die tier is bounded by COLD-TIERING, not compaction. For a
retention-first corpus (semsource, every version kept) "compact in place" would
destroy the history that is the point. Never-die data whose *live* footprint must be
bounded is **cold-tiered to cheaper storage** (ObjectStore/S3 — ADR-068 problem-1),
retaining queryable references. Where a product genuinely wants unbounded live
retention, that is a declared, eyes-open choice — never a blanket time-bound.

### 9. Decommission is a root-set operation: remove the root-set → triggered cascade
+ orphan-sweep reclaim the unreachable → **export the never-die roots first.** This
is git "delete a namespace," replacing today's only mechanism (`docker compose
down -v`, the all-or-none failure mode).

### Guardrails (all observed live in the audit)
Grace window **> max lag, as the MIN over per-producer watermarks** (never a global
one); **complete never-die root declaration**; **die-with-last-referrer** for shared
satellites; **write-side rate-matched to the reaper** (semboids births-outrun-GC).

```
  WRITE BOUNDARY (all triple verbs) — static rule: firehose/append-shaped → reject unless contracted
        ├─ firehose  → stream (MaxAge + MaxBytes) / buffer                         [TIME]
        ├─ blob      → ObjectStore (refcount GC)                                   [REACHABILITY]
        └─ identity, per facet (write mode): ReplaceOwned/CASTransition → compact-latest
                                             AppendEvidence → declared window/compaction | REJECT
  RECLAMATION (reactive): death-trigger → tombstone (KV event) → owners self-clean on own watermark
                          → purge gated on MIN participant watermark ; orphan mark-sweep = off-by-default backstop
```

## Consequences

### Positive
- Bounds the graph with proven idioms on their correct tiers; kills the universal
  firehose-as-identity misfit by gating the write verbs.
- **Reclamation is decentralized/reactive** — no central sweeper that must know every
  index (the gh#433 trap); *builds on* the existing watch + delete-delivery substrate
  (extending self-clean to the ~8 stores that don't yet reclaim — `graph-index-temporal`
  is the built template), with `lesson-curator` as the owner-self-maintains precedent.
  It is IVM/retraction-propagation with the retraction payload as the crux (§4, Related work).
- Enforcement has a **proven rollout template** (the owner-lease `enforce_*` arc:
  observe→reject, incremental, verdict-safe) — Open-Q "can it ship incrementally"
  is already answered YES in-tree.
- Decommission gets a real answer (root-set sweep + export).

### Negative / cost
- Write-boundary enforcement + per-mode reclamation wiring is **net-new work across
  all triple verbs** (not "reuse what exists" — the write modes carry no retention
  meaning today and graph-ingest reads them nowhere). A breaking tightening of the
  write contract.
- Preconditions: close the raw-`KV.Put` (semdragon) and latent `graph/datamanager`
  bypasses; make `ContentStorable`/ObjectStore the required blob path.
- ADR-068's mechanism (reverse-per-owner knowledge, tombstone, purge) is still
  unbuilt; the orphan-sweep backstop remains gated behind D0's consistency problem.

### Risks
- Enforcement mis-tuned (over-strict) false-rejects legitimate slow appends → operators
  disable it (ADR-051 dead-gate). Calibrate to contract membership, not cardinality.
- Mis-declaring a never-die root as triggered → data loss. The never-die set is the
  dangerous half.

## Related work (prior art — verified, cited)

This design is a **composition of established techniques, not a novel invention.**
No single system does the whole composition, so we borrow per-axis — read **RDFox**
(incremental graph maintenance), **XTDB** (versioned/excisable bitemporal substrate),
and the **RSP/IMaRS** stream-reasoning line (streaming + persistent + incremental).

| Our piece | Canonical technique | Reference system(s) |
|---|---|---|
| §1 firehose-window vs durable graph | **RSP stream/dataset split** (windowed stream + persistent background KB) | C-SPARQL, CQELS, RSP-QL; IMaRS/Sparkwave |
| §2 die-with-last-referrer + mark-sweep backstop | **reference-counting + tracing GC** (duals; ref-count can't reclaim cycles) | Bacon/Cheng/Rajan *Unified Theory of GC* |
| §2 cascade over derived facts | **DRed / DRed^c** (delete-and-rederive; derivation counting = "last referrer") | RDFox (`IncrementalDeletion`/`IncrementalAddition`) |
| §2 accrete/supersede-not-delete | **immutable-store default** | Datomic, XTDB, TerminusDB |
| §4 owner self-cleans on tombstone | **Incremental View Maintenance under deletion**, transported over **CDC / log-compaction tombstones** | RDFox, differential dataflow / Materialize / DBSP; Kafka KTables, Debezium |
| §4 tombstone purge on min cursor | **causal-stability GC** = `gc_grace_seconds` = **epoch-based reclamation** | CRDT (Baquero/Almeida/Shoker), Cassandra, EBR/RCU |
| hard-erase (GDPR) | **excision / evict** — rare, async, audit-stubbed, out-of-history | Datomic excision, XTDB evict |

**Through-line:** the graph's indexes are *materialized views*, deletion is a
*retraction* — the whole reclamation problem is **IVM + GC theory**, and
"die-with-last-referrer" (§2) and "a multi-supported index entry needs counting"
(§4) are the *same* DRed^c **derivation-counting** mechanism.

**Obligations the prior art imposes (for the follow-on spec):**
1. **DRed over-deletion.** Classify each derived store **1:1-supported** (adjacency
   keyed by the entity's own edges — safe to delete-on-tombstone) vs **many-supported**
   (community / embedding-centroid / FTS-posting / transitive-edge — must
   **reference-count or re-derive**, remove only at count zero). Deleting on the first
   tombstone corrupts a many-supported index. Load-bearing per-store classification.
2. **Tombstone retention window** (`delete.retention.ms`, Kafka default 24h): the
   deleted-key marker must outlive the **slowest** owner's catch-up lag, or an offline
   owner never sees the delete → phantom entry → full rebuild.
3. **Causal-stability purge traps:** a **dead consumer must be evicted** from the
   cursor set (else min blocks forever — fence/timeout vs block is a deliberate
   liveness/safety fork); a **purge watermark** must fence returning/new consumers (a
   from-zero replay past a purged tombstone **resurrects** the entity — our
   `[idempotent consumers default DeliverPolicy "all"]` hazard); min is only safe over
   **contiguous** cursors (our `[readiness = caught-up, contiguous watermark]`
   discipline); purge is **two-phase** (eligible vs physically reclaimed, resumable).
4. **Cycles leak under a pure trigger-cascade** (ref-counting can't reclaim A↔B). The
   off-by-default mark-sweep needs a **scheduled operator cadence**, not purely
   on-demand; document the cycle-leak window.
5. **Erasure is a multi-store transaction** (async, bounded, audit-stubbed). A GDPR
   erase must fan out to **embeddings (BM25 + neural) and ObjectStore blobs**, not just
   the graph — a left-behind vector/blob is reconstructible ("ghost data"). Model on
   Datomic excision / XTDB evict.
6. **Decide the bitemporal axis explicitly** — does "supersede" track ingestion time
   or world-validity? Late corrections vs genuine state changes need the distinction
   (XTDB is bitemporal on purpose).

**CRDT scope note.** §4's tombstone-purge borrows the CRDT *causal-stability* GC
criterion — but the graph is **single-writer** (ADR-055), so it needs only that GC
sliver (better read as `gc_grace`/EBR), **not** full CRDT merge/convergence. Full
CRDTs (merge functions, LWW/OR-Set, HLC) are for a **multi-writer mesh of instances**
(semlink's peer-replication design) — a *different topology* that sits **upstream** of
the graph: mesh peers converge, then the converged current-state projects into the
local single-writer graph (the raw-lane→projection pattern again). What both share is
a **canonical logical clock** — the mesh's HLC/version-vector and the graph's KV
revision are the same *kind* of causal "birth/change stamp." The one real interaction:
if a semstreams instance is mesh-replicated, its tombstone-purge min-cursor MUST
include the mesh peers, or a locally-purged tombstone resurrects from a lagging peer.
See §"CRDT: retention-GC vs mesh-replication" discussion (out of scope for this ADR's
single-instance decision; flagged for the mesh design).

## Open questions (deferred to the follow-on change / `graph-retention` spec)
1. The **write-mode → reclamation-idiom** table (incl. `ModeCASTransition`), and how
   `AppendEvidence` decomposition is expressed in the contract.
2. The **`RetentionParticipant` interface** shape + registration, and how the min-
   watermark purge gate composes over the participant set.
3. **Owner reverse-knowledge vs tombstone-with-body** — does each owner keep "what I
   indexed for X," or does the tombstone carry X's last-known triples so owners need
   no reverse record? (Real fork.)
4. **Verify every derived store is watch-based** off `ENTITY_STATES` (graph-index,
   graph-embedding confirmed; check spatial-index, community-index, embeddings-cache,
   product-owned stores) — a non-watching store would silently keep stale entries.
5. The **death-trigger declaration surface** for triggered-death roots.
6. Enforcement locus: route the append verbs through a shared commit path, or add the
   check at each verb; and the `claimReader` owned-mode/contract-membership lookup
   (today `OwnerOf` collapses unclaimed and append-evidence).
7. Cold-tier mechanism for never-die live-footprint bounding (ObjectStore/S3 refs).

## References
- [10-product audit](../proposals/graph-retention-10-product-audit.md) (grounding).
- [ADR-068](068-graph-retention-deletion-lifecycle.md) (mechanism — D3 cascade/refuse,
  D5.4 optional orphan sweep, D0 per-producer watermark; completed here).
- [ADR-054](054-semantic-indexing-eligibility.md) (indexing profile — not retention).
- [ADR-055](055-graph-write-intent-taxonomy.md)/[ADR-056](056-authoritative-semantic-state.md)
  (single-writer; per-predicate ownership — the write-path precondition; the write
  *modes* live here as ownership/overlap primitives).
- [ADR-060] class-on-the-wire (makes an `ErrorInvalid` reject non-retryable);
  the **owner-lease `enforce_owner_lease` arc** (the observe→reject rollout template).
- **Precedent, precisely:** semspec `processor/lesson-curator/retirement.go` — a valid
  precedent for the **tombstone+grace MECHANISM and owner-self-maintains shape only**;
  its criterion is *staleness/idle*, which is explicitly **not** the identity-tier
  criterion (§2). It is *not* a reachability-GC reference (it implements 1 of 068's 4
  steps). semlink mesh envelope (firehose-lifecycle model answer). Concepts doc #29
  (raw-lane vs current-state).
- Prior art: git (mark-sweep + reflog grace), Datomic (supersession + excision),
  Cassandra (tombstone + gc_grace + compaction), Kafka (log compaction), TSDBs.
