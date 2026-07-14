## Why

**Status:** Proposed post-PR #524 correctness spike and decision work. Current-layout reconciliation and corrected
INCOMING ownership require their real-NATS owner-filter proof and ADR approval. A PREDICATE key/catalog cutover has
a separate representation benchmark and ADR gate. Helper code landed alongside predicate enforcement remains
inactive experimental scaffolding until its applicable gate passes.

PR #524 fixed O(N²) and lost-update behavior by sharding graph indexes into deterministic composite keys. It
was designed against a production corpus whose predicates violated SemStreams' intended three-part contract.
Restoring that contract removes ADR-065's principal raw-key prefix-collision example and may restore direct
namespace lookup and membership-key observability without a catalog join.

PR #524 did not complete replacement semantics for every sharded membership. Production still appends NAME,
PREDICATE, and source-owned INCOMING rows, so transitions such as `[A] -> [B] -> []` can leave stale query-visible
memberships. The fixed token positions may let one entity enumerate those rows with exact-position NATS filters,
but performance and concurrency behavior are unproven at production shape. This change closes that specific
hardening gap and records what a later retention change may rely on; it does not implement general retention.

## What Changes

- Correct and archive the merged `graph-index-hardening` change as shipped current truth before layering a
  new graph-index delta. Its archived account must state that reverse-key codecs can encode unsafe predicates
  while raw PREDICATE_CATALOG insertion can fail and hold readiness; it must not backdate the future contract.
- Declare every index row's physical token layout, semantic owner, read filter, owner-reconciliation capability,
  and delete/retirement behavior, including explicit variable-arity/non-filterable declarations where applicable.
- Prove public query replacement semantics for NAME, PREDICATE, and INCOMING plus physical current-projection
  semantics for reader-less CONTEXT across `[A] -> [B] -> []`, restart, repair, and deletion.
- Prove fixed-position server-side filtered enumeration against real NATS, including concurrency,
  deduplication, clean bucket recreation, scale, and resource cost.
- Benchmark the current `hash(predicate).entityID` plus PREDICATE_CATALOG against a raw fixed-nine-token
  `domain.category.property.entityID` representation.
- Record owner-discovery and representation decisions independently. Current-layout replacement correctness MUST
  NOT wait for the optional raw-key decision. No permanent dual-format index is permitted.
- Treat a failed owner-filter proof as a blocking dependency, not a correctness waiver: every affected
  query-visible store must have an approved and implemented bounded replacement mechanism before this change can
  claim query parity or archive, even when that mechanism is delivered by a dependent specification.
- After each store's owner filter and ownership ADR pass, reconcile its stored owned memberships against the
  complete desired projection on semantic-owner update or removal, preserving PR #524's ordered execution,
  failure honesty, and readiness model.
- When current-layout reconciliation activates, recreate and rebuild the affected PREDICATE,
  PREDICATE_CATALOG, NAME, and INCOMING buckets behind typed not-ready responses so pre-release orphan rows cannot
  survive as ready query truth.
- Produce a measured owner-discovery result that the separate retention epic can consume without making
  retention or ObjectStore policy part of this change.
- Define exact predicate identity lookup separately from namespace-prefix enumeration in graph-query.
- Make limited query results deterministic by sorting the complete candidate set before applying limits.

**BREAKING:** current-layout reconciliation resets the affected derived-index buckets once and rebuilds them from
already-canonical authoritative ENTITY_STATES behind readiness. If the later benchmark selects raw PREDICATE_INDEX
keys, that selected derived-index bucket changes format through the same reset/rebuild contract. ENTITY_STATES is
never reset. The benchmark-selected graph-index worker maximum becomes a validated configuration bound. No dual
reader, format coexistence, or in-place migration is added.

## Non-goals

- Weakening the three-part predicate contract to preserve malformed beta data.
- Undoing per-membership sharding, keyed ordering, explicit OUTGOING replacement, or fail-closed readiness.
- Re-keying NAME, CONTEXT, or INCOMING without a demonstrated query/operational need.
- Defining a new semantic maximum or storage codec for six-part entity IDs inside graph-index.
- Treating target retirement as authority to erase live source-owned INCOMING assertions.
- Solving ALIAS value scans, spatial/geohash cleanup, embedding deduplication, cascade/refuse semantics,
  ObjectStore reachability, or global mark/sweep in this change.
- Defining operator retention policy, tombstone payloads, stream limits, TTLs, or resource-starvation controls.

## Capabilities

### Modified Capabilities

- `graph-index`: fixed-position ownership/reconciliation, predicate representation decision, catalog
  consistency, clean bucket cutover, and retained PR #524 readiness guarantees.
- `graph-query`: exact predicate lookup and namespace enumeration have explicit semantics independent of
  physical representation; limited results are deterministic and never expose stale memberships.

## Dependencies

- The standalone `nats-kv-key-contract` change MUST land and archive first. This change consumes its exported
  literal-token, literal-key, and wildcard-filter validators, opaque-codec contract, stable errors, and versioned
  budgets in graph-index's new/changed proof and activation paths; it does not require the prerequisite to change
  existing KV wrapper behavior or define a graph-local variant of NATS key syntax.
- Every current PR #524 layout and constructed filter MUST fit that contract before current-layout activation. Because
  six-part entity IDs currently have no governed total-length bound, a separate approved entity-ID bound or physical
  codec contract is an explicit blocking dependency if worst-case current layouts cannot be proven. This change does
  not silently choose that semantic bound or codec.
- PR #532 has enforced the canonical predicate grammar. Remaining sister-product migration tasks are release
  gates, but do not block this framework-local spike.
- The merged `graph-index-hardening` change must be corrected and archived before this change modifies
  production index behavior or seeds the new baseline `graph-index` specification. That governance work does
  not block benchmark-only code.

## Impact

- **Framework code:** graph-index key codecs, catalog, reconciliation, query handlers, graph-query clients,
  graph-clustering readers, shared NATS KV key-contract helpers, readiness/rebuild integration, metrics, and tests.
- **Stored data:** only selected derived-index buckets are deleted/recreated and replayed from canonical
  ENTITY_STATES; this change does not reset authoritative entity state and adds no compatibility reader or
  steady-state dual format.
- **Operators:** documented key shapes, benchmark evidence, resource budgets for wildcard enumeration,
  an enforced graph-index worker maximum, maintenance cutover, and honest not-ready behavior.
- **Consumers:** every product using exact predicate queries, predicate namespace listing, graph traversal,
  clustering, or semantic deletion.
- **Architecture:** superseding decisions for the affected clauses of ADR-065 and ADR-068 D3, plus an explicit
  evidence handoff to ADR-073/gh#527 without absorbing the retention epic.
