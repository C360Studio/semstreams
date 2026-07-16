## 1. Absolute Gates

- [ ] 1.1 Prove the complete worst-case raw key (194-byte predicate + 256-byte entity ID = 451 bytes) and every
      forward/namespace/owner filter against the `nats-kv-keys` budgets. (Carries over: unit maxima already proven
      through the shared validators.)
- [ ] 1.2 Pass pinned real-NATS maximum-key and exact-match conformance for the raw layout, including malformed
      shorter/longer keys and neighboring-namespace controls (`domain.category.*` must not match
      `domain.categoryx.*`).
- [ ] 1.3 Run the `graph-index-replacement-semantics` lifecycle fixtures (`[A] -> [B] -> []`, restart, repair,
      shuffled replay, concurrent mutation to a declared final revision) against the raw layout.
- [ ] 1.4 Pass the ADR-065 5k/3s CI guard and one 21k sustained-churn run on the raw layout at the configured
      worker shape; same absolute budgets as replacement-semantics (p95 ≤ 3s, p99 ≤ 5s, none at 10s, no leaks).

## 2. Evidence and Decision

- [ ] 2.1 Run one comparative benchmark (raw vs hash+catalog, identical datasets/fixtures/queries) and record it
      as ADR evidence — informative, not a selection threshold.
- [ ] 2.2 Identify any current predicate-membership-watch consumer; if one exists, define add/remove-only
      semantics; otherwise record watch behavior as a non-public operational property.
- [ ] 2.3 Write and approve the ADR superseding ADR-065's hash+catalog clauses: raw keys adopted on gates passing,
      or the specific failed gate recorded and hash+catalog retained as documented fallback. Record the
      NAME/CONTEXT/INCOMING codec-keep rationale in the same ADR.

## 3. Cutover (inside the announced pre-v1 wipe)

- [ ] 3.1 Include the raw PREDICATE_INDEX bucket in the announced wipe/reseed; initialize behind typed not-ready
      from freshly reseeded canonical ENTITY_STATES; readiness held until the authoritative replay watermark.
- [ ] 3.2 Convert exact/namespace query handlers and graph-clustering readers to direct filters; retire
      PREDICATE_CATALOG and its consistency/repair machinery; remove every old-format reader and rejected-candidate
      helper.
- [ ] 3.3 Prove exact, namespace, list, stats, compound, traversal, restart, repair, and limited-result parity on
      the raw representation against the frozen fixtures; prove clustering parity after its next independently
      completed detection cycle.
- [ ] 3.4 If hash+catalog is retained instead (gate failure), remove the raw-candidate scaffolding and skip the
      storage cutover entirely.

## 4. Closeout

- [ ] 4.1 Update vocabulary, index-reference, KV Twofer, and operator docs with the selected key shapes and
      filters.
- [ ] 4.2 Run lint, race, contracts, real-NATS integration, structural + semantic e2e, and affected product suites.
- [ ] 4.3 If the pre-v1 wipe window closed before 3.1, halt: record the missed window in the ADR and re-file this
      change as a post-v1 migration proposal instead of executing a second wipe.
