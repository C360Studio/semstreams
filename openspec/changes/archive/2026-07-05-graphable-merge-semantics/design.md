# Design — Graphable merge is predicate-level (gh#466)

## The decision

Make the Graphable (JetStream) ingest lane merge triples with
`graph.MergeTriples` — **replace per `(subject, predicate)`** — the same helper
the mutation lane already uses. Pin that as the shared merge contract for
graph-ingest, and correct the one place the documented contract lies.

## Why MergeTriples is correct (satisfies gh#177 AND gh#466)

`MergeTriples(existing, newer)` (`graph/helpers.go:101`):
1. starts from `newer` (incoming wins),
2. keeps each `existing` triple only if no `newer` triple shares its
   `(subject, predicate)`.

Consequences on a re-arrival of entity E carrying predicates {P1, P2}:
- **gh#466 (duplication) fixed:** P1/P2 replace their prior values — no growth.
- **gh#177 (clobber) still fixed:** a predicate P3 the arrival does NOT carry
  (e.g. a lifecycle-managed or other-writer predicate) has no conflict, so it is
  preserved. The append version preserved P3 too — but at the cost of duplicating
  P1/P2. MergeTriples gets both right.

This is why the fix is not "revert gh#177": gh#177 correctly stopped the
full-replace clobber; it just used the wrong merge. Append and full-replace are
the two wrong extremes; predicate-level merge is the intended middle.

## The multi-valued-predicate contract (the real decision)

`MergeTriples` conflicts on `(subject, predicate)` only — NOT on object. So a
multi-valued predicate (a relationship like `flock.neighbor`, where one subject
has several triples with the same predicate and different objects) is **full-set
replaced**: if the incoming arrival contains any `flock.neighbor` triple, *all*
prior `flock.neighbor` triples are dropped and replaced by the incoming set.

- **Publish-the-full-set producers (semboids, sensor meshes): correct.** Each
  arrival carries the complete current neighbor set; replace is exactly right.
- **Publish-one-at-a-time producers: would lose the rest.** This lane does not
  support incremental relationship append.

**Decision:** the graph-ingest merge contract is *full-set-replace per
`(subject, predicate)`*. Producers own publishing the complete object set for a
predicate on each arrival. This matches the mutation lane and the KV-twofer model
(the write IS the current state of that predicate group).

The misleading `MergeTriples` doc comment ("For relationships, all unique
relationships are preserved") MUST be corrected to state replace-per-predicate /
full-set semantics — the comment currently describes behavior the code does not
have, which is a latent trap for the next producer author.

## Test reconciliation

- `batch_integration_test.go` `base+5 / +3 / +2` assertions exercise **`AddTriples`**
  (already `MergeTriples`-based) with distinct predicates over a bare-stub
  pre-create, so `base=0` and no conflict → counts unaffected by this change.
  Confirm during implementation; do not assume.
- Find any test that drives **`MergeEntity`** with a repeated same-predicate
  arrival and asserts growth — reconcile it to the replace contract.
- **Add the regression:** publish the same entity (same `(subject, predicate)`
  triples) N times via the Graphable lane → exactly one triple per
  `(subject, predicate)`, not N. Mirror semboids' `TestSnapshotsLandAndReplace`
  intent. Also assert a non-conflicting predicate written by a prior arrival
  survives (the gh#177 half).

## Risk

- **A hidden producer relying on accumulation.** The only way append-semantics is
  load-bearing is if some consumer reads the *history* of a predicate from the
  duplicated triples in `ENTITY_STATES` (rather than from KV revision history or a
  stream). That would be an anti-pattern (duplicated current-state triples are not
  an event log), but the implementation MUST grep Graphable producers/consumers to
  confirm none does before landing. If one does, it needs the explicit-opt-in path
  (Non-goals), not the silent default.
- **Ordering within a predicate group.** `MergeTriples` places `newer` first then
  non-conflicting `existing`; downstream readers that assume triple order should be
  spot-checked (predicate-keyed readers like `GetPropertyValue` are order-independent
  for distinct predicates).
