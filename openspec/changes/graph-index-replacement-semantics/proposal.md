## Why

PR #524 sharded graph indexes into one-membership-per-key composite layouts and fixed O(N²) and lost-update
behavior, but it completed replacement semantics for only OUTGOING and CONTEXT. The shipped update path still
appends NAME, PREDICATE, and source-owned INCOMING rows, so transitions such as `[A] -> [B] -> []` leave stale
query-visible memberships. The shipped delete path removes target-prefixed INCOMING rows while leaving the removed
entity's source-owned INCOMING, NAME, and PREDICATE rows.

The predicate (PR #532) and entity-ID (`entity-id-contract`) contracts now make fixed-arity owner filters provable:
every entity-bearing key position holds a validated bounded six-part ID, so `ListKeysFiltered` with exact-position
wildcards can enumerate one owner's rows. The generalized reconciliation helper already exists in-repo with
key-contract preflight and unit proof. This change activates replacement for the remaining stores on the CURRENT
physical layout. It deliberately does not decide predicate representation — that is the separate
`predicate-raw-key-representation` change, and this change must not wait for it.

## What Changes

- Pin the complete derived-store ownership matrix (layout, arity, semantic owner, forward filter, owner filter,
  overwrite/lifecycle/reset behavior, readiness consequence) as table-driven tests.
- Define INCOMING source ownership: source fact replacement retracts the former row; source removal retracts every
  source-owned row; target retirement/removal preserves live sources' assertions; the target-prefix hard-delete is
  removed.
- Prove the exact-arity owner filters on real NATS: correctness under concurrency/cancellation/restart, the
  existing ADR-065 5k/3-second CI guard, and one sustained-churn run on the 21k profile at the configured worker
  shape plus one stress shape. Absolute budgets: p95 ≤ 3s, p99 ≤ 5s, no operation at the 10s handler bound, no
  temporary-consumer or queue leak.
- On proof pass and ADR approval, reconcile stored owner rows against the complete desired projection on entity
  update/removal for NAME, PREDICATE, and source-owned INCOMING, preserving PR #524's keyed ordering,
  reconcile-at-execution reads, failure-held readiness, and watermarks.
- Make limited query results deterministic: sort and deduplicate the complete candidate set before limits/samples.
- Make predicate-list/namespace-list expose only predicates with current memberships (vocabulary history stays
  registry-owned).
- Activate through the already-announced pre-v1 clean wipe/reseed: affected buckets initialize behind typed
  not-ready responses and rebuild from freshly reseeded canonical ENTITY_STATES.

**BREAKING:** pre-v1 clean cutover only. No compatibility reader, dual format, in-place migration, or rollback.

## Non-goals

- Changing any physical key layout or codec (PREDICATE representation is `predicate-raw-key-representation`).
- Retention, TTL, ObjectStore reachability, cascade, or global GC (retention epic; this change publishes its
  owner-discovery evidence as an input).
- ALIAS (frozen current behavior, separately owned; audited in the matrix, blocks nothing here).
- Spatial/geohash cleanup, embedding deduplication.

## Capabilities

### Modified Capabilities

- `graph-index`: ownership matrix, replacement reconciliation for NAME/PREDICATE/source-owned INCOMING, INCOMING
  source ownership, activation gating, retained PR #524 readiness guarantees.
- `graph-query`: complete-current-projection visibility, deterministic limited results, current-membership catalog
  semantics, exact-vs-namespace predicate lookup semantics.

## Dependencies

Stated as capability gates, not other changes' task numbers:

- Canonical predicate contract enforced fail-closed at authoritative writes (shipped, PR #532).
- Bounded entity-ID contract enforced at authoritative writes and replay, with the local source corpus clean
  (`entity-id-contract` local activation gates).
- `nats-kv-keys` validators, stable errors, and budgets available (archived baseline); every newly constructed
  key/filter passes them before I/O.
- `graph-index-hardening` corrected and archived as shipped current truth (done).

## Impact

- **Framework code:** graph-index reconciliation, query handlers, readiness/rebuild integration, metrics, tests.
- **Stored data:** covered by the already-announced pre-v1 wipe/reseed; no new preservation obligation.
- **Consumers:** every product using predicate, by-name, incoming, traversal, or clustering-derived queries.
- **Architecture:** one ADR (owner discovery + INCOMING ownership) superseding the affected ADR-068 D3 inference;
  measured owner-discovery evidence handed to the retention epic.
