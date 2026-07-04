# ADR-068: Graph retention, deletion & GC lifecycle

## Status

**Proposed — 2026-07-04. Design-only (no code).** Names the responsibility and
fixes the shape so semsource (ADR-0008, retention-first) and any other consumer
can react before anything is built. Scopes gh#433 (index cleanup on delete) as
one increment, plus the referential-completeness and GC gaps around it. Nothing
here is on anyone's critical path today: the framework does **not** delete in
steady state, and this ADR's first job is to keep it that way safely. Implement
incrementally, only as the exception paths are actually exercised.

An adversarial code-grounded review sharpened this draft: every "what exists
today" claim was verified against source, and the forward-looking design was
corrected on three points — refuse-if-referenced is NOT race-free on its own
(the tombstone is the actual integrity mechanism); the reverse index is genuine
new work with an async-ordering interlock as its hard part (not free from
gh#430); and the GC grace gate is a min over per-producer watermarks, not one
global watermark (gh#431). See D0.

## Decision

Adopt a **retention-first graph lifecycle** and make deletion a **rare,
first-class, referential-integrity-preserving operation** — never a
storage-policy side effect. Concretely:

1. **Guardrail (build first, cheap):** the live graph NEVER uses NATS KV
   `TTL` / `MaxBytes` / `MaxAge` (or JetStream stream retention) for entity
   lifecycle. Storage caps are non-binding catastrophe backstops only, sized so
   they never bind in normal operation. Retention is a **semantic** decision;
   it is never a consequence of a bucket config.
2. **Model:** facts accrete; supersession (a retained prior version related to
   its successor, "current" as a ranking marker) is the normal path; physical
   deletion is the exception, for **mistakes / churn / erasure-requests** only.
3. **Delete is integrity-preserving:** `entity.delete` is **refuse-if-referenced
   by default, cascade opt-in** — both driven by the existing `INCOMING_INDEX` —
   and MUST clean every derived index (subsumes gh#433) and handle owned blobs.
4. **Tombstone = the death-side dual of the stub:** an application-level
   "retired-at-R" entity state, written through graph-ingest, so a reference can
   safely outlive its target during an async cascade and readers resolve to an
   authoritative "deleted" instead of an ambiguous missing key.
5. **A narrow, watermark-gated GC worker** for reclamation only (tombstone
   purge, never-born-stub reaping, orphaned-blob collection, optional
   root-reachability sweep) — NOT a general "evict old data" sweeper.

This is a **primitive-completion**, not a feature: deletion already exists in the
code but is incomplete (below). The ADR names the missing responsibility so the
increments are deliberate rather than a whack-a-mole of index patches.

## Context

### What exists today (verified against code)

Deletion is **already implemented end-to-end**, and is **incomplete in three
documented ways**:

- **The write path.** graph-ingest is the sole authoritative writer to
  `ENTITY_STATES` (`processor/graph-ingest/component.go:803`; ADR-055/049).
  Mutation subjects include real DELETE verbs: `graph.mutation.entity.delete`,
  `graph.mutation.triple.remove`, and the `RemoveTriples` half of
  `update_with_triples` (`processor/graph-ingest/mutations.go:30,65,70`).
  `Component.DeleteEntity` (`component.go:2186`) does a **soft** NATS KV
  `Delete` (a tombstone marker, not a purge — `natsclient/kv.go:342`); no code
  anywhere calls `Purge`/`PurgeDeletes` on the graph buckets.
- **No retention config.** Every graph bucket (`ENTITY_STATES` and all indexes)
  is created with `Bucket`+`Description` only (`component.go:804`;
  `processor/graph-index/component.go:631`) → NATS defaults: `History=1`, no
  TTL, no MaxBytes, no MaxAge. `natsclient.KVStore` has no per-key TTL API at all
  (ADR-056 §"no per-key TTL"). **So nothing is being evicted today** — the risk
  is purely that someone adds TTL/MaxBytes to bound growth.
- **The three gaps:**
  - **(a) Derived-index leak (gh#433).** On delete, `graph-index`
    `DeleteFromIndexes` removes only the deleted entity's OWN
    `OUTGOING_INDEX`/`INCOMING_INDEX` keys (`component.go:1421`). It does NOT
    clean the entity's `PREDICATE_INDEX` memberships, nor remove it as a stale
    `FromEntityID` inside **other** entities' `INCOMING_INDEX` lists, nor touch
    `NAME_INDEX`/`ALIAS_INDEX`/`CONTEXT_INDEX`. Deleted entities keep answering
    `byName`/predicate/prefix/alias queries forever. The blocker is real: a
    KV-watch delete event carries only the deleted key, not the entity's former
    triples (`component.go:1455-1460`), so cleanup needs a **per-entity reverse
    index** ("which predicates/names/aliases did this entity carry").
  - **(b) Referential incompleteness.** Deleting `A` cleans `A`'s own adjacency
    but does NOT rewrite a **referrer**: a triple `B —calls→ A` lives on `B`, so
    `A`'s deletion leaves a **dangling edge** on `B`. Reference-blind eviction
    (TTL/MaxBytes) has the identical failure mode — which is why it is banned.
  - **(c) Orphaned blobs.** `DeleteEntity` never reads `EntityState.StorageRef`
    (`graph/types.go:33`) nor calls `ObjectStore.Delete` (`storage/objectstore/store.go:303`),
    so deleting a blob-owning entity orphans its ObjectStore object. And
    `StorageRef` is **not unique per entity** (several entities may point at one
    key), so naive cascade-delete would break sharers — this needs refcount or
    reachability, not a cascade.

### The birth-side primitive we already have (the key analogy)

`graph/stub.go`: when `B` references `A` before `A` is born, graph-ingest
materializes a **profile-less stub** at `A`'s ID so "a relationship's target
always resolves to a node" (ADR-056 D4 lane-ii). The stub is the framework's
**birth-side** referential-integrity placeholder. This ADR's tombstone is its
**death-side dual** — the same invariant, applied when a target is retired while
references may still point at it.

### Consumer position (semsource ADR-0008)

semsource is **retention-first**: versioned sources retained + related by
supersession, "current" is a ranking marker, no delete by default. They emit via
`publish → graph-ingest MergeEntity` (append), so they remove nothing today.
Both halves below are **off their critical path**; they asked (upstream-asks #15)
that the framework make delete integrity-preserving *if/when* the mistake-cleanup
path is built, and that the ADR guardrail forbid eager/policy-driven delete in
the meantime. This ADR is that guardrail + that design.

## The three problems people conflate (and the right tool for each)

Most retention disasters come from using the tool for problem 3 (or, worse,
storage TTL) to solve problem 1.

| Problem | Right tool | Wrong tool |
|---|---|---|
| **1. Steady-state accretion** — retained/superseded versions pile up | Supersession as explicit retained entities; cold-tier old versions to ObjectStore/S3; optional `History=N` for a bounded audit tail | Deleting anything; storage TTL/MaxBytes |
| **2. Rare explicit delete** — mistakes / churn / erasure | First-class integrity-preserving op: reverse-index cleanup + refuse-or-cascade + blob policy | Bare `entity.delete` (today's incomplete path) |
| **3. Distributed consistency of a delete** — lagging index/reader/replica | Application tombstone + grace window gated on the ADR-066 caught-up watermark | Hard delete at `History=1`; purge-before-caught-up |

Retention lives via **explicit supersession entities** (durable, queryable), NOT
via KV history (`History=N` is a fragile, bounded audit tail, not a retention
store). This is semsource's existing choice and it is the correct one.

## Design

### D0. The async, per-producer indexing hazard (the load-bearing constraint)

The adversarial review of this ADR surfaced that its hardest problems all share
one root: **`graph-index` maintains the indexes ASYNCHRONOUSLY, with a
multi-worker pool (`config.Workers`, shipped 2–4), and each consumer keeps its
OWN watermark** — there is no synchronous, graph-wide "current state" a delete
can read. Two consequences the rest of the design must respect explicitly,
because the reused primitives do NOT provide the interlock for free:

- **Read-at-delete is not consistent by default.** `handleEntityDelete` runs
  synchronously on the KV-watch goroutine, while the reverse index / forward
  indexes are written on the async worker path. A delete can observe an index
  that (i) has not yet applied the entity's latest writes (cleanup misses
  memberships), or (ii) gets cleaned by the delete and then re-populated by a
  lagging worker (resurrected entries). Any "read X's state at delete time" step
  MUST be gated on an ordering interlock: process the delete only once the
  index/watermark has reached the entity's pre-delete revision, or route deletes
  through the same key-ordered path as the writes they must observe.
- **"Everyone is caught up" is per-producer, and deliberately not global.**
  `graph-index` and `graph-embedding` each hold their own `revlag.Watermark`;
  gh#431/ADR-066 established that a *shared* Target across heterogeneous
  producers **deadlocks** (embeddings only process text entities), so a single
  global watermark does not and should not exist. Any "safe to purge / safe to
  refuse" gate must therefore compose the **min across the relevant consumers'
  watermarks**, or explicitly accept per-consumer resurrection for the consumers
  it omits.

D0 is why refuse-if-referenced (D3) is not race-free on its own, why the reverse
index (D3) needs an explicit interlock to actually fix gh#433, and why tombstone
purge (D5) cannot gate on one watermark. It is the reason the tombstone (D4) is
load-bearing rather than optional.

### D1. The storage-vs-semantics guardrail

Storage-management and semantic-lifecycle are separate concerns and one must
never do the other's job:

- Live graph buckets (`ENTITY_STATES`, `PREDICATE_INDEX`, `INCOMING_INDEX`,
  `OUTGOING_INDEX`, `NAME_INDEX`, `ALIAS_INDEX`, `CONTEXT_INDEX`,
  `PREDICATE_CATALOG`) MUST NOT set `TTL`/`MaxAge`, and MUST NOT set a `MaxBytes`
  that can bind under expected load. If a size cap is set at all, it is a
  crash-prevention backstop sized to a hard multiple of projected steady state,
  and hitting it is a **paging alert**, never a designed reclamation path.
- Rationale is one sentence: age/size eviction is **reachability-blind** and will
  drop an entity with live inbound edges (problem-2/3 failure mode). Precedent:
  `pkg/ownership/epoch.go` already uses a bucket TTL ONLY as an
  `OWNER_PRESENCE` backstop, never as the reclamation mechanism (compaction on
  CAS does the real work).

This D1 is the increment worth landing first: it is a one-way door (a bucket
config change is get-or-create and hard to reverse — `natsclient/client.go:970`)
and it costs nothing to enforce as convention + a lint/boot check.

### D2. Retention-first model & supersession

The default lifecycle is **assert / supersede**, never delete:

- A new version of an entity is a new (or updated) entity related to its
  predecessor by a supersession edge; the predecessor is retained. "Current" is
  a ranking marker on the successor, resolved at query time — not a deletion of
  the predecessor.
- Time-travel / audit is served by querying retained supersession chains, not by
  KV history replay.
- Physical deletion is reserved for: an ingestion **mistake**, unbounded
  low-value **churn** a product explicitly opts to reclaim, or an **erasure
  request** (GDPR-style). All three are rare and operator/producer-initiated.

### D3. Delete as a first-class integrity-preserving mutation

`graph.mutation.entity.delete` gains an explicit **referential policy**, defaulting
to the safe one:

- **`refuse` (default).** The delete succeeds only if `INCOMING_INDEX(X)` is
  empty. It is the recommended v1 and matches "rare exception for mistakes" — but
  it is **NOT race-free on its own** (D0): `INCOMING_INDEX` is maintained
  asynchronously, so a delete that observes it empty can race an in-flight
  `B —calls→ A` (a write to *B*'s key, which the delete of *A* never serializes
  against) → a dangling edge on B. Two mitigations, and they are load-bearing,
  not optional:
  - **Gate the refuse-check on the watermark**, mirroring ADR-066's own pattern:
    treat the check as valid only once `graph-index` (and any other referrer-
    producing consumer) has caught up to the query-time `LastSeq` of
    `ENTITY_STATES`. This closes edges in-flight *at check time*.
  - **Write a tombstone anyway** (D4). A referrer that still arrives *after* the
    check resolves to a tombstone node, not a missing key — this is what makes
    refuse actually safe, so refuse does NOT get to skip the tombstone.
  Also note **over-refuse**: because `INCOMING_INDEX` is itself leaky until
  gh#433 cleanup lands (increment 3), a stale phantom `FromEntityID` from a
  prior un-cleaned delete can block a legitimate delete of X. So increment 3
  (cleanup) is a hard prerequisite of increment 4 (refuse), not merely adjacent.
- **`cascade` (opt-in per request).** Enumerate referrers via `INCOMING_INDEX`,
  then for each referrer either remove the dangling assertion or repoint it,
  per a declared rule. Cascade can fan out, so it is bounded (max depth / max
  referrers, refuse past the bound) and runs against tombstones (D4) so an
  in-flight cascade never exposes a truly missing target.

Both policies, plus gh#433 cleanup, depend on the **same new primitive**:

- **Per-entity reverse index** — `entity → {predicates, names, aliases, context,
  outgoing-targets}` it carries, maintained on the WRITE path alongside the
  forward indexes, bounded by the entity's own triple count (NOT global
  cardinality). **This is genuinely new work, not free from gh#430.** gh#430's
  composite `hash(predicate).entityID` key puts entityID in the *suffix*, so its
  prefix scan answers the *forward* question ("which entities carry predicate
  P"); the reverse question ("which predicates does entity X carry") is a
  *suffix* match that `KVStore.KeysByPrefix` cannot serve (and token-position
  wildcards are the wrong tool — see `predicate_index.go`). So the reverse index
  is a distinct structure this ADR introduces; gh#430 does not provide it. It
  must include **outgoing-targets** so cleanup can remove the deleted entity as a
  stale `FromEntityID` in each peer's `INCOMING_INDEX` (that step reads
  `OUTGOING_INDEX(A)` / the reverse record BEFORE deleting it — an
  ordering-sensitive step, see D0). This closes gh#433 (cleanup knows what to
  remove even though the KV-watch delete event carries only the key) AND feeds
  refuse/cascade (find referrers).

Owned-blob policy is **refcount-or-reachability, never naive cascade** (because
`StorageRef` is not unique per entity): on delete, a blob is reclaimed only when
no surviving entity's `StorageRef.Key` matches. This is a GC-worker job (D5),
not an inline delete step.

### D4. The application tombstone (death-side dual of the stub)

For cascade and for cross-index/reader consistency, deletion writes an
**application-level tombstone** through graph-ingest (single-writer invariant
preserved) rather than only a bare KV `Delete`:

- A tombstone is a **profile-less "retired-at-R" entity state** — structurally a
  stub with a death envelope (mirror of `StubMessageType`), carrying the
  deleting revision and reason. Like the stub, its whole job is "a reference
  still resolves to a *node*, with a known status."
- It resolves today's ambiguity: the read path currently collapses
  `ErrKeyDeleted` and `ErrKeyNotFound` into one "not found" (`natsclient/kv.go:450`),
  so a reader cannot distinguish deleted from never-existed. A tombstone is an
  authoritative "deleted at R" that flows through the normal watch + query path.
- Tombstoned buckets need `History ≥ 2` (or the tombstone as a real value, not a
  KV delete-marker) so the death record is durable rather than a single fragile
  revision under `History=1`.
- A tombstone is itself GC'd (D5) once no reader or index could still need it.

The tombstone is the integrity mechanism, not a nicety: because of D0's
race, **both** `refuse` and `cascade` write a tombstone first, then hard-purge
later (D5) — `refuse` because a referrer can still arrive after the emptiness
check, `cascade` because the referrer fix-up is async. The only case that could
skip the tombstone is a delete whose target is provably unreferenceable (e.g. a
never-born stub reap, D5.2), where there is nothing to dangle.

### D5. The narrow GC worker

A background reclamation worker — **NOT** a general evictor — with a small,
enumerable job set:

1. **Tombstone purge:** hard-purge a tombstone only when **the MIN across every
   relevant consumer's watermark** has advanced past its revision AND (for
   cascade) the cascade is complete. Per D0 there is no single global watermark
   (gh#431: a shared Target deadlocks), so the gate is the min over
   graph-index's, graph-embedding's, and any other referrer-/index-producing
   consumer's watermark — or an explicit, documented acceptance of per-consumer
   resurrection for the ones omitted. This is the Cassandra `gc_grace_seconds`
   interlock: purge before every derived index/replica caught up and a lagging
   rebuild **resurrects** the entity. Note: hard-purge needs a NEW
   `KVStore.Purge` primitive — none exists today (only soft `Delete`).
2. **Never-born-stub reaping:** a stub whose referenced entity never arrives
   within a grace window is an orphan; reap it (and re-evaluate the referrer that
   created it). Birth-side symmetry with tombstone purge.
3. **Orphaned-blob collection:** reclaim an ObjectStore blob when no surviving
   entity references its key (refcount or reachability sweep, per D3).
4. **Optional root-reachability sweep:** mark-and-sweep from product-declared
   roots, only for products whose model can orphan (e.g. cascade intermediate
   nodes). Off by default; a product opts in and declares its roots.

Build it from the two in-repo templates — but assign them correctly (the review
caught the naive mapping):

- **Compaction-on-write** (`pkg/ownership/epoch.go`) fits jobs where a *value is
  rewritten under CAS* and a *next writer exists* to piggy-back on — e.g.
  maintaining the reverse index, or reaping a never-born stub when its referrer
  is next touched. It does NOT fit tombstone purge: purge *removes a key* (a
  Purge, not a value-rewrite), and a dead entity's key has **no next writer** to
  fold the reap into.
- **Fixed-interval sweeper** (`processor/agentic-loop/approval_sweeper.go`),
  restart-safe via KV-persisted timestamps, is therefore the mechanism for the
  headline jobs: tombstone purge (over the new `Purge` API), blob GC, and the
  optional root sweep. So the honest framing is *sweeper-first for reclamation,
  compaction-on-write only where a live writer already passes through.*

## Alternatives considered

- **NATS KV TTL / MaxBytes for retention.** Rejected (D1) — reachability-blind;
  drops entities with live inbound edges. This is the core anti-pattern.
- **Tolerate dangling references (RDF/SPARQL triplestore style).** Rejected for
  the live graph — it is precisely the pain semsource reports. (Query-time
  resolve-to-nothing may still be an acceptable *transient* state during an
  async cascade, but not a steady state.)
- **KV history as the retention store (`History=N` large).** Rejected — history
  is a bounded, fragile audit tail, not durable/queryable retention. Supersession
  as explicit entities is the retention store.
- **Reference counting instead of tombstone+reachability.** Considered; refcounts
  are cheap for refuse/cascade decisions but brittle under concurrent async
  ingest (miscounts strand entities). Use the `INCOMING_INDEX` as the source of
  truth for referrers; a per-entity reverse index for cleanup. Refcounts, if
  used, are a derived optimization, not the authority.
- **Fix gh#433 standalone.** Rejected as the *unit of work* — index cleanup
  can't be done correctly without the per-entity reverse index, and that same
  index is what referential completeness (b) needs, so patching (a) alone leaves
  the responsibility unnamed and (b)/(c) unaddressed. gh#433 is increment (a)
  under this ADR.

## Consequences

### Positive

- Storage config can never silently corrupt the graph (D1 guardrail).
- Deletion becomes safe, rare, and observable; dangling edges become
  transient-and-tombstoned (a late referrer resolves to a tombstone node, not a
  missing key) rather than silent corruption. (Not "structurally impossible" —
  D0's async indexing means the tombstone, not the refuse-check, is what
  guarantees integrity.)
- gh#433 and the two unfiled halves (referential completeness, blob orphans) get
  one coherent design instead of three patches.
- Reuses existing primitives: `INCOMING_INDEX`, the stub, the ADR-066 watermark,
  the epoch compaction pattern, the sweeper pattern.

### Negative / cost

- A new per-entity reverse index to build and maintain (bounded by per-entity
  triple count).
- Tombstoned buckets need `History ≥ 2`, a small storage increase.
- A GC worker to build (narrow, but real), plus grace-window tuning.

### Risks

- **Grace too short → resurrection.** Mitigated by gating purge on the ADR-066
  watermark, not wall-clock alone.
- **Cascade fan-out.** Mitigated by bounded cascade (max depth/referrers, refuse
  past bound).
- **Blob sharers.** Mitigated by refcount/reachability, never naive cascade.

## Increments

Ordered by dependency — the review showed the naive order (refuse before
tombstone) ships an un-honorable guarantee, and that the async-ordering
interlock (D0), not the index itself, is the hard part.

1. **D1 guardrail** — convention + boot/lint check that live graph buckets carry
   no binding TTL/MaxBytes/MaxAge. Cheap, high-value, land first.
2. **Per-entity reverse index (write-path) + the D0 ordering interlock** — the
   shared enabler AND the hard part: the index is easy; making a synchronous
   delete read a consistent view of the async-maintained index (gate on the
   pre-delete revision / key-ordered routing) is the real work.
3. **(a) Derived-index cleanup on delete (gh#433)** — using increment 2 + the
   interlock. Must fully land before increment 5 (a leaky index over-refuses).
4. **D4 application tombstone + min-watermark composition** — the integrity
   primitive; both refuse and cascade depend on it, so it precedes 5.
5. **(b) Referential completeness** — `refuse` (default: watermark-gated check +
   tombstone from 4) then `cascade` (opt-in), on `INCOMING_INDEX`.
6. **D5 GC worker** — tombstone purge (new `Purge` API, min-watermark-gated,
   fixed-interval sweeper), never-born-stub reap, (c) orphaned-blob collection,
   optional root sweep.

## Open questions

1. **The D0 interlock representation** — how does a synchronous
   `handleEntityDelete` get a consistent read of the async-maintained reverse
   index? Gate the delete on the index watermark reaching the entity's pre-delete
   revision; route deletes through the same key-ordered worker path as writes; or
   snapshot the reverse record into the delete request at mutation time. This is
   the crux of increment 2/3 and is unresolved.
2. **The consumer set for the min-watermark gate** — which consumers must be
   caught up before a tombstone purge is safe (graph-index, graph-embedding,
   NATS replicas, external readers)? gh#431 forbids a single shared watermark, so
   this is a composed min over an explicitly enumerated set — who owns that list?
3. **Cascade default action per referrer** — remove the dangling assertion vs
   repoint vs refuse. Likely per-edge-class product policy; framework provides
   the mechanism.
4. **Erasure (GDPR) vs excision** — does an erasure request need the *content*
   purged from ObjectStore + KV history immediately (hard excision) while the
   tombstone/node is retained for referential integrity? (Datomic excision
   analog.) Probably yes; specify the split.
5. **Where does the reverse index live** — extend `graph-index` (it already
   watches ENTITY_STATES and owns the forward indexes) vs a new bucket. Lean
   `graph-index`.
6. **Tombstone representation + the `History≥2` one-way door** — value vs KV
   delete-marker; and note that raising `History` on the live graph buckets is
   itself a hard-to-reverse config change (get-or-create, `client.go:970`) to the
   same buckets D1 protects — sequence it deliberately. Also requires a new
   `KVStore.Purge` primitive (none today).

## References

- gh#433 (index cleanup on delete — increment 3); gh#430/ADR-065 (composite
  predicate-index keys — answers the FORWARD "entities carrying P" question; does
  NOT provide the reverse per-entity index this ADR needs — entityID is the key
  suffix); gh#431/ADR-066 (per-producer caught-up watermark — the GC grace
  interlock, and the reason there is no single global watermark).
- ADR-055 (mutation-API-is-not-a-producer, single-writer), ADR-056
  (authoritative semantic state, ownership epoch compaction-on-CAS,
  `OWNER_PRESENCE` TTL-backstop), ADR-049 (single-writer to ENTITY_STATES).
- `graph/stub.go` (birth-side placeholder — the tombstone's dual);
  `processor/graph-ingest/component.go:2186` (`DeleteEntity`);
  `processor/graph-index/component.go:1421,1455-1460` (index-cleanup gap);
  `graph/query_index_types.go:12` (`IncomingEntry`); `natsclient/kv.go:342,450`
  (soft delete; deleted-vs-not-found collapse); `graph/types.go:33`
  (`StorageRef`); `storage/objectstore/store.go:303` (`ObjectStore.Delete`);
  `pkg/ownership/epoch.go` + `processor/agentic-loop/approval_sweeper.go` (GC
  worker templates).
- semsource ADR-0008 (retention-first); `../semsource/docs/upstream/semstreams-asks.md`
  #15 (both halves of integrity-preserving delete).
- Prior art: Datomic (assert/retract + rare excision), Cassandra (tombstone +
  gc_grace_seconds + compaction), git (mark-and-sweep from refs + reflog grace),
  RDF/SPARQL (dangling-reference tolerance — the rejected alternative).
