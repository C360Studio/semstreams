## Context

gh#474 measured `INCOMING_INDEX` at 2,300 writes/s / 3.2M total vs ~1k live keys after a bulk
seed, starving the shared NATS client. Two adversarial reviews (semstreams-reviewer, architect)
confirmed the mechanism and corrected the root cause.

**Root cause (corrected).** The post-drain churn is NOT clustering/embedding write-backs:
`graph/clustering/storage.go` runs `CreateTriples: false` (community `member_of` triples are not
emitted) and graph-embedding writes a separate `EMBEDDINGS_CACHE` — **neither writes to
`ENTITY_STATES`**. The driver is a post-ingest **inference edge wave**: inference stamps fleet-wide
inferred relationships (`Context: inference.hierarchy`, Subject+Object are entity IDs —
`graph/inference/hierarchy.go`) via `graph.mutation` → graph-ingest → `ENTITY_STATES` → the
graph-index watch, hitting INCOMING's O(in-degree²) CAS on hub targets. It keeps churning after
`pending=0` because inference is a *second* wave.

Audit of every graph-index write path:

| Index | Pre-change write | Class | Delivered shape |
|---|---|---|---|
| PREDICATE | composite-key `Put` (sharded, ADR-065) | fixed | — |
| **INCOMING** | CAS list per target | O(in-degree²) | `targetID.sourceID.hex(predicate)` |
| **NAME** | CAS `Items` per `hash(name)` | O(shared-name²) | `hash(name).entityID.hex(predicate)` |
| **CONTEXT** | non-CAS list per raw context | O(fan-in²) + race | entity-prefixed; reconcile/delete |
| OUTGOING | full overwrite per entity | self-bounded | **deferred (Non-goal)** |
| ALIAS / spatial / temporal / community / structural | single-value or composite | safe | — |

## Goals / Non-Goals

**Goals:** bounded per-index write cost for INCOMING/NAME/CONTEXT; fix CONTEXT's lost-update +
raw-key collision; migrate every reader and the in-scope delete without silent breakage;
instrument the re-index no-op rate; both-tier e2e-verified cutover.

**Non-Goals:** OUTGOING sharding (net-negative — see proposal); reciprocal entity-delete cleanup
(gh#433); change-detection (L2) and resource isolation (L3) — deferred behind this change's
measurement (filed as follow-ups). A query-readiness envelope (gh#397 extension).

## Decisions

### D1 — Per-index composite key (hash open axes, keep entity-ID axes raw + validated)

| Index | Prefix (scan) axis | Key | Value | Notes |
|---|---|---|---|---|
| INCOMING | target (raw 6-token) | `targetID.sourceID.hex(predicate)` | empty | source + reversible predicate |
| NAME | `hash(name)` | `hash(name).entityID.hex(predicate)` | `{name, priority}` | value retains case/priority |
| CONTEXT | entity6 | `entityID.hash(context).hex(predicate)` | `{contextValue}` | reconcile/delete prefix |

- **Raw entity-ID prefixes are collision-safe** (fixed 6-token; no ID is a token-prefix of another —
  `IsValidEntityID` `message/triple.go:145`), matching graph-ingest's `handleQueryPrefixNATS`.
- **Guard `IsValidEntityID` at key construction** (target AND source) and skip-log on failure —
  structural, not a doc caveat (the source axis is only upstream-validated). Reject empty predicate
  (no trailing-dot key).
- NAME/CONTEXT need a **small value** (original-case name/priority; context provenance) — not
  recoverable from a hashed prefix. Empty marker for INCOMING.
- Unconditional `Put`, no CAS — ADR-065 footgun comment (key uniqueness → CAS unnecessary).
- Predicate tokens retain PR #524's reversible untagged hex layout. PR #524 selected that defensive
  representation while graph-ingest still accepted KV-unsafe predicate text. PR #532 now enforces
  canonical three-part predicates, and graph-index revalidates replay; the codec reconstructs accepted
  identity but does not authorize input. Name/context values use hashes and remain recoverable from
  their small values.

### D2 — Definitive reader inventory (do not defer the audit — the first review found a missed reader)

**INCOMING** (all migrate to prefix-scan + reconstruct):
- `processor/graph-index/query.go` `handleQueryIncomingNATS`
- `graph/query/client.go` `GetIncomingEdges` — **broken today** (unmarshals `{"incoming":[...]}` vs
  written `[]IncomingEntry`); migration is a *bugfix*, parity test asserts *correct* edges.
- `processor/graph-clustering/anomaly.go:100` `kvRelationshipQuerier` + `:39` `graphProviderAdapter`
- `processor/graph-clustering/component.go:1160` `kvProvider.getNeighborsFromBucket` — direction-agnostic
  today; its **incoming** branch must become prefix-scan; the **outgoing** branch stays on the old
  format (OUTGOING is not sharded) → split by direction.
- e2e `test/e2e/client/nats.go:919` `GetIncomingEntries`

**NAME**: `name_index.go:156` `handleQueryByNameNATS`, `:108` `nameIndexIsReady` (both in-package;
`Keys()`-len check stays valid). No e2e today (see D5).

**CONTEXT**: **no production reader** (only its own RMW `Get`, `component.go:1620`); e2e only
(`nats.go:962 GetContextEntries`, `:976 GetAllContexts`). Migrate the write + the e2e readers; do
NOT hunt for a query consumer.

Readers holding a raw `jetstream.KeyValue` (client.go, anomaly.go, clustering component.go) MUST use
`natsclient.FilteredKeys(ctx, kv, prefix+">")` (`kv.go:469`), not `KVStore.KeysByPrefix` (only
graph-index's own bucket is a `*KVStore`).

### D3 — Delete path (in-scope = clean prefix scan; reciprocal = gh#433)

`DeleteFromIndexes` prefix-scans INCOMING rows where the deleted entity is the target and
CONTEXT rows owned by the deleted entity. CONTEXT cleanup is semantically correct because the
entity owns those provenance memberships. INCOMING target-prefix cleanup remains explicitly a
legacy hard-delete behavior: those rows are supported by source entities and logical retirement
must not discard that evidence. Source-owned retraction, reciprocal cleanup, and its durable
manifest remain gh#527 scope.

### D4 — Wire-type preservation

`graph.IncomingEntry` is the `graph.index.query.incoming` **wire element** (`query_index_types.go:33`,
consumed by PathRAG `pathrag.go:274`) — NOT dead code. Do NOT delete/rename it or its JSON tags; only
the stored blob format dies; readers reconstruct `[]IncomingEntry` from keys. Same for NAME wire types.

### D5 — Cutover, e2e gate, and NAME coverage gap

Same bucket names (ADR-065's ~9-config lesson). Rebuild from ENTITY_STATES on boot; old monolithic
keys inert (bare key can't match `prefix.>`). BREAKING format → **`e2e:structural` AND `e2e:semantic`
green before merge** (ADR-065 precedent). Tighten to **hard-fail** (not warn) and migrate the raw
e2e readers: incoming `GetIncomingEntries`; CONTEXT `validateContextIndexHierarchy` (matches the
literal key `inference.hierarchy` today — must read the reconstructed context *value*),
`GetAllContexts`, `GetContextEntries`. **NAME has NO e2e tier** — gate it on an integration test
driving `graph.index.query.byName` through the production wire (assert reconstructed
`{EntityID,Name,Predicate,Priority}`), and note the e2e gap explicitly per the breaking-change rule.

### D6 — Re-index no-op instrumentation (the L2/L3 data gate)

Add a counter in the re-index funnel (`processEntityUpdateFromData`, `component.go:878`): compute the
entity's index-input projection (relationship `(predicate,target)` pairs, the full distinct-predicate
set, `(namePredicate,name)`, `(context,predicate)` pairs — NOT raw object values) and compare to the
last-indexed projection; increment `unchanged` or `changed`. **Observe only — do not skip.** This
tests the inference-wave hypothesis: if unchanged≈0%, change-detection (L2) is dead code; if material,
L2 is justified and the projection defined here is its signature primitive.

### Deferred follow-ups (filed, not built)

- **L2 change-detection** — gated on D6's counter. If built: signature over the FULL projection above
  (NOT excluding literals — `community.member_of` is a literal but still a predicate-index membership;
  excluding it silently drops memberships), invalidated on entity delete (else delete→recreate-identical
  leaves the entity unindexed), concurrency-safe.
- **L3 resource isolation** — gated on post-merge starvation re-measurement. Prefer a Put-path CPU/rate
  bound (the starvation is CPU-bound: ~4 workers on O(N²) CAS+JSON); a dedicated `*nats.Conn` was tried
  and reverted (`674cbcb6`→`eb4da982`) and does not buy back CPU.

## Risks / Trade-offs

- **Breaking on-disk format** — mitigated by the both-tier hard-fail e2e gate + rebuild-on-boot; old
  keys provably inert.
- **CONTEXT has no production query reader** — the storage migration is justified by its write
  amplification and lost-update race, while entity-prefix reconciliation/delete are required to
  avoid retaining superseded or deleted memberships.
- **Source-axis validation** — enforced structurally in the key helper (skip-log), converting a silent
  mis-split into an observable non-corrupting skip.
- **NAME e2e gap** — closed with a production-wire integration test, and flagged per the breaking-change
  discipline.

## Codex P1 review revisions (PR #524) — corrections to the frozen storage contract

The initial L1 design (above) shipped, then a retention-contract review (Codex) blocked merge
because #524 FREEZES the reverse-index storage format and several correctness gaps would be cast in
concrete. The following revisions supersede the shapes above where they differ:

- **P1a — predicate token is `hex(predicate)`, not raw.** At PR #524 design time, graph-ingest accepted
  KV-unsafe predicates. A raw reverse token could fail after ENTITY_STATES and hashed
  PREDICATE_INDEX writes succeeded; the raw PREDICATE_CATALOG key could fail independently and hold
  readiness. Reversible `graph.EncodePredicateToken` kept INCOMING a pure key-scan. PR #532 now rejects
  noncanonical predicates at authoritative writes and graph-index replay revalidation before
  membership, catalog, or reverse-index I/O. The shipped untagged hex remains layout, not permission.
  Key shapes remain INCOMING `targetID.sourceID.hex(pred)` and NAME
  `hash(name).entityID.hex(pred)`.

- **P1f — CONTEXT is entity-prefixed and self-reconciling: `entityID.hash(context).hex(pred)`.** The
  original hash(context)-prefix layout could not retract superseded memberships on update
  (`C:{p1,p2}→C:{p1}` leaked `p2`) — a regression vs the replaced merge-list writer. Because CONTEXT
  has no reader, keying by entity costs nothing on reads and makes update a prefix-scan
  `entityID.` + retract-then-write (bounded per entity) and delete a prefix-scan — correct retraction
  AND self-cleaning, without the O(fan-in) CAS class this change removed.

- **P1e — D3 correction (source-support ownership).** An INCOMING row `(target=A, source=B, pred=p)`
  is supported by B's live triple, not by A. So `DeleteFromIndexes(A)` deleting the whole `A.*`
  target-prefix is a LEGACY HARD-DELETE of a leaf entity, NOT semantically-complete cleanup — it
  discards B's still-live evidence. It is explicitly labeled MUST-NOT-be-reused-by-logical-retirement;
  source-owned retraction + a durable reverse manifest is gh#527 (retention Increment-0).

- **P1b — write failures withhold readiness.** Required index writes aggregate + return errors,
  retry (idempotent) up to 3×, and on ultimate failure mark the entity failed; `computeIndexStatus`
  withholds `Ready` while any entity is unindexed. The no-op baseline is stored only on success.

- **P1c — deterministic reads.** `handleQueryIncomingNATS` / `GetIncomingEdges` sort by
  `(FromEntityID, Predicate)` so a no-op replay can't reshuffle a capped PathRAG result set.

- **P1d — authoritative readiness gate.** incoming, outgoing, byName, alias, and all predicate
  handlers return `ErrorCodeIndexNotReady` while initial replay is incomplete or any required
  entity work remains failed. The initial-enumeration sentinel authorizes only the empty 0/0 case;
  non-empty replay becomes ready only when the watermark catches the exact target revision.

- **P2b — no-op counter is a real metric + covers alias.** Exposed as
  `graph_index_reindex_events_total{result}` and `graph_index_write_failures_total`;
  `computeIndexProjection` now includes the ALIAS axis (an alias-only change was miscounted unchanged).

- **Upgrade debris** (from the retention review) — a one-time versioned purge of pre-#524 monolithic
  keys before v1 is required (rollback would reactivate stale indexes); tracked in gh#527. Do not
  teach steady-state GC both formats.

### D7 — Ordered execution, authoritative reconciliation, and fail-closed consumers

- Updates, deletes, coalesced work, and repair use one hash-keyed FIFO dispatcher. Every work item
  re-reads authoritative `ENTITY_STATES` when it reaches its lane, so an old queued event applies
  current presence/state rather than clobbering a newer write or resurrecting a deleted entity.
- Reconciliation of a present entity replaces `OUTGOING[entityID]` with its complete current
  relationship array, including explicit `[]`. Only authoritative `ENTITY_STATES` absence deletes
  the owner key. Empty values are bounded by live-entity cardinality and prevent stale outgoing
  results from accumulating across relationship churn.
- Coalescing retains the greatest delivered revision per pending key; watermark completion uses the
  exact revision represented by the detached batch. Repair is bounded and keeps the failed entity
  in the readiness gate until reconciliation succeeds.
- Status/`LastSeq` reads use a dedicated ENTITY_STATES KV handle because the NATS handle caches
  stream information and concurrent `Get`/`Status` use of one handle races under `-race`.
- Direct graph-query and clustering readers fail closed when graph-index status is unavailable,
  malformed, building, or degraded. `allow_ungated_reads` is an explicit standalone/test-only
  opt-out, not a cutover default.
- PathRAG propagates request/decode failures and rejects syntactically valid but structurally
  incomplete envelopes (missing `relationships`); direction `both` fails if either leg fails.
- gh#527 retains semantic retention work: manifests, source-owned retraction, tombstone payload,
  blob reclamation, and legacy-format purge. It does not own per-entity ordering correctness.
