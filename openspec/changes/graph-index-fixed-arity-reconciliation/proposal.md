## Why

PR #524 fixed O(N²) and lost-update behavior by sharding graph indexes into deterministic composite keys. It
was designed against a production corpus whose predicates violated SemStreams' intended three-part contract.
Restoring that contract removes ADR-065's principal raw-key prefix-collision example and may restore direct
namespace and watch semantics without a catalog join.

The fixed token positions also expose a simpler retention primitive than ADR-068/073 and gh#527 assume:
server-side NATS filtered key listing may enumerate an entity's PREDICATE, NAME, source-owned INCOMING, and
CONTEXT rows from a bare entity ID. Performance and concurrency behavior are unproven at production shape,
so SemStreams must spike and benchmark this path before committing to reverse manifests, payload-rich
tombstones, or another on-disk predicate format.

## What Changes

- Correct and archive the merged `graph-index-hardening` change as shipped current truth before layering a
  new graph-index delta. Its archived account must state that reverse-key codecs can encode unsafe predicates
  while raw PREDICATE_CATALOG insertion can fail and hold readiness; it must not backdate the future contract.
- Declare every index row's physical token layout, semantic owner, exact read filter, owner-reconciliation
  filter, and delete/retirement behavior.
- Prove fixed-position server-side filtered enumeration against real NATS, including concurrency,
  deduplication, clean bucket recreation, scale, and resource cost.
- Benchmark the current `hash(predicate).entityID` plus PREDICATE_CATALOG against a raw fixed-nine-token
  `domain.category.property.entityID` representation.
- Record the representation decision in a superseding ADR before implementation. No permanent dual-format
  index is permitted.
- Reconcile an entity's stored owned memberships against its complete desired projection on update and
  deletion, preserving PR #524's ordered execution, failure honesty, and readiness model.
- Correct retention scope: manifests or tombstone payloads are evidence-driven exceptions for stores that
  cannot meet filtered-enumeration requirements, not the default for every composite key.
- Define exact predicate identity lookup separately from namespace-prefix enumeration in graph-query.

**BREAKING (conditional):** if the benchmark selects raw PREDICATE_INDEX keys, the on-disk format changes
again. The old index bucket is deleted/recreated and freshly reingested canonical ENTITY_STATES is replayed
behind readiness. No dual reader, format coexistence, or in-place migration is added.

## Non-goals

- Weakening the three-part predicate contract to preserve malformed beta data.
- Undoing per-membership sharding, keyed ordering, explicit OUTGOING replacement, or fail-closed readiness.
- Re-keying NAME, CONTEXT, or INCOMING without a demonstrated query/operational need.
- Treating target retirement as authority to erase live source-owned INCOMING assertions.
- Solving ALIAS value scans, spatial/geohash cleanup, embedding deduplication, cascade/refuse semantics,
  ObjectStore reachability, or global mark/sweep in this change.

## Capabilities

### Modified Capabilities

- `graph-index`: fixed-position ownership/reconciliation, predicate representation decision, catalog
  consistency, clean bucket cutover, and retained PR #524 readiness guarantees.
- `graph-query`: exact predicate lookup and namespace enumeration have explicit semantics independent of
  physical representation.
- `graph-retention`: identify which index stores can self-reconcile from entity-position filters and which
  require another cleanup authority.

## Dependencies

- `predicate-contract-enforcement` must settle and enforce the grammar before any raw predicate key cutover.
- The merged `graph-index-hardening` change must be corrected, completed, and archived to seed the baseline
  `graph-index` specification.

## Impact

- **Framework code:** graph-index key codecs, catalog, reconciliation, query handlers, graph-query clients,
  graph-clustering readers, NATS filtered-list helpers, readiness/rebuild integration, metrics, and tests.
- **Stored data:** selected index buckets are deleted/recreated and replayed from freshly reingested canonical
  state; no compatibility reader or steady-state dual format.
- **Operators:** documented key shapes, benchmark evidence, resource budgets for wildcard enumeration,
  maintenance cutover, and honest not-ready behavior.
- **Consumers:** every product using exact predicate queries, predicate namespace listing, graph traversal,
  clustering, or semantic deletion.
- **Architecture:** superseding decisions for the affected clauses of ADR-065, ADR-068 D3, and ADR-073
  section 4; gh#527 scope correction.
