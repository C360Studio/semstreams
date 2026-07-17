## 1. Store and Query Contract

- [ ] 1.1 Pin the complete ownership/filter matrix (layout, arity, owner, literal forward/owner filters,
      overwrite, lifecycle, reset, readiness) in table-driven tests; every literal filter passes the
      `nats-kv-keys` validators. (Carries over: unit maxima at `E = 256` already proven — 321/710/902/256.)
- [ ] 1.2 Define and test the INCOMING source-ownership contract: source fact replacement, source
      removal/tombstone, target retirement preserving live-source assertions, authorized cascade — without
      production wiring.
- [ ] 1.3 Freeze public acceptance fixtures for `[A] -> [B] -> []` at the graph-index status watermark across
      OUTGOING, exact/value predicate, list, stats, compound, by-name, incoming, and traversal queries; track
      clustering only after its independently completed next detection cycle.
- [ ] 1.4 Record ALIAS, spatial/geohash, embedding, blob/ObjectStore, cascade, and global GC as separately owned.

## 2. Owner-Filter Proof (real NATS)

- [ ] 2.1 Correctness: exact match sets, malformed shorter/longer keys, neighboring-owner and reversed-axis
      controls, concurrent Put/Delete with exact-key dedup before diffing, cancellation, empty buckets, restart,
      clean bucket recreation, convergence to a declared final ENTITY_STATES revision. (Carries over: `[A] -> [B]
      -> []` stale-deletion/insertion proofs and NAME stable-key overwrite already landed in isolated tests.)
- [ ] 2.2 Performance: existing ADR-065 5k/3s CI guard + one sustained-churn run on the 21k profile at the
      configured worker shape and one stress shape; record latency (p95 ≤ 3s, p99 ≤ 5s, none at 10s), ingest
      throughput, queue growth, catch-up time, temporary-consumer high-water/return-to-baseline.
- [ ] 2.3 Select and enforce the graph-index worker maximum in validated configuration.
- [ ] 2.4 Add malformed-axis and complete-key/filter pre-I/O controls for production CONTEXT, OUTGOING, and every
      remaining graph-index I/O path (watcher, Get, lister, Put, Delete) before activation. Obligation inherited
      from the superseded fixed-arity change's entity-id-contract handoff; it is not waived by the split.
- [ ] 2.5 Pass pinned real-NATS maximum key/filter exact-match conformance for every bounded graph-index layout at
      the canonical 256-byte entity bound; representative-corpus success does not substitute for governed maxima.
      Inherited obligation — the shared unit bound is a prerequisite, not duplicate proof.

## 3. Decision and Activation

- [ ] 3.1 Write and approve the owner-discovery + INCOMING-ownership ADR from the correctness and budget evidence;
      a failed store defers to an explicitly specified dependent bounded mechanism, which becomes a completion
      dependency of this change.
- [ ] 3.2 Activate reconciliation for NAME, PREDICATE, and source-owned INCOMING inside the announced pre-v1
      wipe/reseed: affected buckets initialize behind typed not-ready, rebuild from reseeded canonical
      ENTITY_STATES, readiness held until the authoritative replay watermark. Preserve keyed ordering,
      reconcile-at-execution, bounded repair, exact watermarks.
- [ ] 3.3 Replace source-owned INCOMING on source fact change/removal and remove the target-prefix hard-delete,
      covered by readiness.
- [ ] 3.4 Make predicate-list/namespace-list expose only current non-zero memberships; vocabulary history stays in
      the registry.

## 4. Query Determinism

- [ ] 4.1 Sort and deduplicate complete exact, value-filtered, compound, list, namespace-list, and stats results;
      apply limits/samples only on wire surfaces that expose them; preserve existing INCOMING/NAME total orders.

## 5. Closeout

- [ ] 5.1 Prove query parity after update, empty projection, source removal, target retirement, repair, restart,
      and shuffled replay; prove the next completed clustering cycle consumes reconciled truth.
- [ ] 5.2 Publish the measured owner-discovery matrix as retention-epic input (no retention policy here); correct
      gh#527 cross-links.
- [ ] 5.3 Supersede the affected ADR-068 D3 clauses; update KV Twofer, index-reference, reset, and query-ordering
      docs.
- [ ] 5.4 Run lint, race, contracts, real-NATS integration, structural e2e, and affected product suites.
- [ ] 5.5 Archive only after every affected query-visible store satisfies `[A] -> [B] -> []` through an approved,
      implemented bounded mechanism (including any delivered by explicit dependent changes).
