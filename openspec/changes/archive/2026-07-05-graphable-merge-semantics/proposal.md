# Graphable merge is predicate-level, not raw append (gh#466)

## Why

The JetStream Graphable ingest lane merges a re-arriving entity's triples by **raw
append**:

```go
// processor/graph-ingest/component.go:1802
existing.Triples = append(existing.Triples, entity.Triples...)
```

So any producer that republishes the same entity **accumulates duplicate triples
forever**. semboids reproduced it (`TestSnapshotsLandAndReplace`): a boid
publishing position/velocity at 5 Hz has `flock.position.x` ×3 after three
arrivals — growth is linear in arrivals × triples/payload. At load-dial rates
(200 boids × up to 30 Hz × ~10 triples) that is ~60k new triples/second of KV
growth; the slow version bites every Graphable producer (the `iot_sensor` example
accumulates 6+ triples per reading on the same device entity). Downstream:
unbounded KV entry size, index churn over duplicate relationships, and readers
(`GetPropertyValue`, UI) seeing multi-valued properties where one value is meant.

This is a **lane inconsistency**, not just a missing helper call:

- The **`update_with_triples` mutation handler** already uses `graph.MergeTriples`
  — replace-by-`(subject, predicate)` (`mutations.go:794, :905`, gh#244). (Note:
  the `triple.add` evidence-append lane / `AddTriples` component method appends by
  design — that is a separate, intentional lane; the inconsistency is with the
  CAS-update handler.)
- The **Graphable/JetStream lane** (`MergeEntity`) appends.

The append is an over-correction from gh#177 ("jetstream consumer upserts
(full-replace) … clobbers lifecycle-managed triples"), which fixed a Put-clobber
by switching to "merge" but implemented merge as keep-everything. The correct
middle — predicate-level merge — fixes **both** problems at once:
`graph.MergeTriples(existing, newer)` lets `newer` win on a `(subject, predicate)`
conflict while preserving non-conflicting existing triples (so lifecycle-managed
or other-writer predicates are NOT clobbered — gh#177 — and re-arrivals do NOT
duplicate — gh#466).

## What Changes

- **The Graphable merge lane replaces per `(subject, predicate)`.** `MergeEntity`'s
  existing-entity branch uses `graph.MergeTriples(existing.Triples, entity.Triples)`
  instead of `append`, making the two graph-write lanes consistent and matching the
  documented merge intent.
- **Preserve the create-time indexing profile across the merge.** The indexing
  profile (`entity.indexing.profile`) is create-time-immutable (ADR-054), but
  `MergeTriples` is newer-wins — so the incoming profile is dropped before merging
  WHEN the existing entity already has one, and a profile-less referential stub
  keeps its first real arrival's declared profile (true birth). This is the one
  immutable-predicate exception on the merge lane (hierarchy/stub markers never
  arrive on a Graphable payload, so newer-wins can't touch them).
- **Pin the merge contract.** A predicate present in the incoming entity's triples
  fully replaces that `(subject, predicate)`'s prior triples; a predicate absent
  from the incoming set is preserved untouched. This is *full-set-replace per
  predicate*, not accumulation.
- **Resolve the multi-valued-predicate contract + fix the misleading comment.**
  `MergeTriples`'s doc comment claims "for relationships, all unique relationships
  are preserved," but the implementation replaces per `(subject, predicate)` — so
  a multi-valued relationship predicate (e.g. `flock.neighbor`) is *full-set
  replaced*: a producer MUST publish the complete object set for that predicate
  each arrival; a partial publish drops the rest. This is the right behavior for
  full-set publishers (semboids); document it and correct the comment so the
  contract is not a trap.
- **Reconcile tests + add a regression.** Confirm/adjust any test asserting
  MergeEntity re-arrival growth (the `batch_integration_test.go` `base+N`
  assertions test `AddTriples`, already MergeTriples-based, and use distinct
  predicates — likely unaffected). Add a repeated-arrival regression: publishing
  the same entity N times leaves one triple per `(subject, predicate)`, not N.

## Capabilities

### New Capabilities
- `graph-ingest` — seeded with the entity-merge-semantics facet: how graph-ingest
  merges a re-arriving entity's triples into existing state on both the Graphable
  (JetStream) lane and the mutation (`AddTriples`) lane, and the
  replace-per-`(subject, predicate)` contract they share. Distilled from code, not
  backfilled.

### Modified Capabilities
- None (no existing spec covers graph-ingest yet).

## Impact

- `processor/graph-ingest/component.go`: one-line change in `MergeEntity`
  (`append` → `graph.MergeTriples`), plus test reconciliation + regression.
- `graph/helpers.go`: correct the `MergeTriples` doc comment (replace-per-predicate,
  full-set semantics for multi-valued predicates).
- **Behavior change on the highest-volume lane** — every Graphable producer stops
  accumulating duplicates. Verify no producer relies on accumulation (see
  Non-goals). Touches the graph-write-intent taxonomy (ADR-055/056) and the
  single-writer invariant.
- **Consumers:** semboids (reported — load-dial / fast-moving-graph profiling);
  every Graphable producer (`iot_sensor` example, semsource, etc.).

## Non-goals

- **Time-series / accumulation semantics on the Graphable lane.** If a producer
  genuinely wants to accumulate values over time, that is an explicit opt-in via a
  different mechanism (append-only stream, distinct predicate per sample, or
  ObjectStore) — not the silent default of the highest-volume state lane. Out of
  scope here; file separately if a real need appears.
- **Changing `AddTriples` / mutation-lane semantics** — already correct
  (MergeTriples); this only aligns the Graphable lane to match.
- **Per-object relationship diffing** (add one `flock.neighbor` without republishing
  the set) — the contract is full-set-replace per predicate; per-object append is a
  separate, larger design if ever needed.

## Consumers

`processor/graph-ingest` (framework, the single writer to `ENTITY_STATES`);
semboids (reported); all Graphable producers across the `sem*` family. The merge
contract is a cross-repo behavior, so it is pinned as a spec.
