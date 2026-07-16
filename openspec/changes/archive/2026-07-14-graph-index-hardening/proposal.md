## Why

Graph-index maintenance runs away after a bulk seed: a measured **2,300 writes/sec
(3.2M total) to `INCOMING_INDEX` against ~1,019 live keys** — a rewrite loop, not
throughput — pegged a process at ~395% CPU, overflowed the shared NATS client
(`slow consumer, messages dropped`), and starved agentic dispatch, aborting a paid run
(gh#474). The GRAPH ingest backlog had drained (`pending=0`): this is index
**maintenance** churn.

Two adversarial reviews (semstreams-reviewer + architect) confirmed the mechanism and
**corrected the root cause**: the post-drain churn is not clustering/embedding write-backs
(clustering runs `CreateTriples: false` and embeddings live in a separate cache — **neither
writes to `ENTITY_STATES`**). It is a **post-ingest inference edge wave** — inference emits
fleet-wide inferred relationships (`Context: inference.hierarchy`) via `graph.mutation` →
graph-ingest → `ENTITY_STATES` → graph-index — hitting `INCOMING_INDEX`'s O(in-degree²) CAS
on hub targets. That is squarely a per-index storage problem, fixed by composite-key sharding
(the class ADR-065 fixed for `PREDICATE_INDEX`).

An audit of every graph-index write path found the same monolithic-list class in **three**
indexes (plus a lesser variant, `OUTGOING`, deferred — see Non-goals):

- `INCOMING_INDEX` — CAS list keyed by target; O(in-degree²). ADR-065 *wrongly* claimed this
  one safe ("bounded by in-degree").
- `NAME_INDEX` — CAS list keyed by `hash(name)`; O(shared-name²). ADR-065 flagged it.
- `CONTEXT_INDEX` — before this change, a list keyed by the **raw** (unhashed,
  collision-prone) context value via a **non-CAS** `Get`+`Put`; O(context-fan-in²)
  write **plus** a lost-update race. The replacement is entity-prefixed so update
  reconciliation and delete cleanup can enumerate the entity's own memberships.

The reviews also found two *deeper* candidate defects — a re-index feedback loop (change
detection) and NATS-client resource isolation — but their premises did not survive code
review (the write-backs that trigger re-index are real edges, not skippable literals; a
dedicated writer connection was tried and reverted, and the starvation is CPU-bound). Both
are **deferred behind measurement** rather than built on an unverified root cause.

## What Changes

- **Composite-key sharding** of `INCOMING`, `NAME`, and `CONTEXT`: one KV key per
  edge/membership with reversible untagged-hex predicate tokens. PR #524 selected that layout against
  the then-permissive predicate corpus; PR #532 now enforces canonical three-part predicates, and the
  codec remains physical layout rather than acceptance authority. `INCOMING` is target-prefixed,
  `NAME` is name-hash-prefixed, and `CONTEXT` is entity-prefixed. Writes become
  unconditional `Put` operations (no CAS/list rewrite), while `CONTEXT` can retract
  superseded memberships and self-clean by entity prefix.
- **Exhaustive per-index reader migration** (INCOMING + NAME; CONTEXT has no production
  reader) to prefix-scan + reconstruct; **delete paths** migrate INCOMING's legacy
  target-prefix cleanup and add correct entity-owned CONTEXT cleanup. Wire response
  types (`IncomingEntry`, etc.) are **preserved** — only the storage format changes.
- **Authoritative readiness and repair**: all entity work (update, delete, coalesced
  replay, and repair) runs through hash-keyed FIFO lanes and reconciles current
  `ENTITY_STATES` at execution. Watermark completion remains tied to the exact delivered
  revision. Required-write failures withhold readiness until bounded background repair
  succeeds.
- **Fail-closed reads**: incoming, outgoing, byName, alias, and predicate handlers gate
  on readiness. Direct query/clustering consumers also fail closed unless a standalone
  deployment explicitly sets `allow_ungated_reads`; PathRAG rejects transport, decode,
  and structurally incomplete responses instead of returning partial/empty success.
- **Instrument a re-index no-op counter** — per re-index event, record whether the entity's
  index-input projection actually changed vs. what was last indexed. This is the data gate for
  the deferred change-detection follow-up.
- **BREAKING** (on-disk index format). Per the hard rule, `e2e:structural` AND `e2e:semantic`
  must be green before merge, with the CONTEXT and incoming e2e assertions tightened to
  **hard-fail** (they are warn-only today) and NAME given production-wire integration coverage.

## Capabilities

### New Capabilities

- `graph-index`: seed the capability spec with the index-maintenance storage contract —
  sharded per-membership storage, prefix enumeration, entity-delete cleanup by prefix, and the
  rebuild-from-`ENTITY_STATES` cutover. (First OpenSpec touch of this component; distilled from
  code + ADR-065 + the two reviews.)

## Impact

- **Code**: `processor/graph-index/` (write paths for INCOMING/NAME/CONTEXT; new `*_index.go`
  key helpers; the re-index no-op counter; delete paths); readers in `graph/query/client.go`,
  `processor/graph-index/query.go`, `processor/graph-clustering/{anomaly.go,component.go}`;
  `test/e2e/client/nats.go` + scenarios (raw readers → prefix-scan, warn → hard-fail).
- **Consumers**: no NATS query-API wire-shape change. Query behavior becomes fail-closed
  while graph-index is building or degraded; standalone direct-bucket consumers must
  explicitly opt out with `allow_ungated_reads`. Ad-hoc tooling reading raw index buckets
  sees the new sharded key formats.
- **Docs**: correct ADR-065's incomplete "incoming is safe" claim and extend its sibling-index
  sweep to NAME/CONTEXT (via this spec — ADRs are history).
- **Issues**: closes gh#474; folds the NAME + CONTEXT class members (and CONTEXT's lost-update
  race) into one change. Files the change-detection and resource-isolation follow-ups gated on
  this change's no-op-counter and post-merge starvation re-measurement.

## Non-goals

- **OUTGOING sharding** — it is full-overwrite `Put` (self-bounded by out-degree, no O(N²));
  sharding it is net-negative (turns one Put into E_out Puts, and creates a phantom-on-reuse
  delete problem it must then solve). Its only value is as INCOMING's reverse-index for the
  *reciprocal* (middle-token) delete cleanup, which is out-of-scope gh#433. Left as-is.
- **Reciprocal entity-delete cleanup** (removing a deleted entity from *other* entities'
  index keys, where it appears as a middle token) — the pre-existing gh#433 gap; unchanged
  here (the in-scope delete of an entity's *own* prefix keys is migrated).
- **Retention manifests, source-owned semantic retraction, tombstone payloads, and
  upgrade-debris purge** — remain gh#527 scope. Per-entity execution ordering and
  no-stale-clobber reconciliation are correctness requirements of this change, not #527.
- **Change-detection (L2)** and **resource isolation (L3)** — deferred behind this change's
  measurement (filed as follow-ups).
