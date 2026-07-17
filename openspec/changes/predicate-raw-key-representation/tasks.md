## 1. Absolute Gates

- [x] 1.1 Prove the complete worst-case raw key (194-byte predicate + 256-byte entity ID = 451 bytes) and every
      forward/namespace/owner filter against the `nats-kv-keys` budgets. (Carries over: unit maxima already proven
      through the shared validators.)
- [x] 1.2 Pass pinned real-NATS maximum-key and exact-match conformance for the raw layout, including malformed
      shorter/longer keys and neighboring-namespace controls (`domain.category.*` must not match
      `domain.categoryx.*`).
- [x] 1.3 Run the `graph-index-replacement-semantics` lifecycle fixtures (`[A] -> [B] -> []`, restart, repair,
      shuffled replay, concurrent mutation to a declared final revision) against the raw layout.
      Evidence: `replacement_reconcile_integration_test.go` runs those fixtures against production
      `predicate3.entity6` keys, including public readiness, repair, restart, and replay parity.
- [x] 1.4 Pass the ADR-065 5k/3s CI guard and one 21k sustained-churn run on the raw layout at the configured
      worker shape; same absolute budgets as replacement-semantics (p95 ≤ 3s, p99 ≤ 5s, none at 10s, no leaks).

## 2. Evidence and Decision

- [x] 2.1 Run one comparative benchmark (raw vs hash+catalog, identical datasets/fixtures/queries) and record it
      as ADR evidence — informative, not a selection threshold.
- [ ] 2.2 Identify any current predicate-membership-watch consumer; if one exists, define add/remove-only
      semantics; otherwise record watch behavior as a non-public operational property.
- [x] 2.3 Approve ADR-078, selecting raw keys, superseding ADR-065's hash-plus-catalog clauses, and recording the
      NAME/CONTEXT/INCOMING codec-keep rationale. Representation acceptance does not complete task 1.3 or the
      production activation tasks below.

## 3. Cutover (inside the announced pre-v1 wipe)

- [ ] 3.1 Include the raw PREDICATE_INDEX bucket in the announced wipe/reseed; initialize behind typed not-ready
      from freshly reseeded canonical ENTITY_STATES; readiness held until the authoritative replay watermark.
      Implementation-ready evidence: operations guide 29 contains the combined deployment-derived wipe and the
      replacement integration proves raw initialization, watermark gating, and canonical replay. Check this task
      only after the coordinated tag and deployment wipe/reseed execute.
- [x] 3.2 Convert exact/namespace query handlers and graph-clustering readers to direct filters; retire
      PREDICATE_CATALOG and its consistency/repair machinery; remove every old-format reader and rejected-candidate
      helper.
      Evidence: production graph-index and graph-clustering use raw direct filters; catalog/hash code remains only in
      the explicit comparative test candidate.
- [x] 3.3 Prove exact, namespace, list, stats, compound, traversal, restart, repair, and limited-result parity on
      the raw representation against the frozen fixtures; prove clustering parity after its next independently
      completed detection cycle.
      Evidence: `replacement_reconcile_integration_test.go` and focused graph-index/query tests cover the frozen
      public fixtures and a later production clustering cycle.
- [x] 3.4 Close the fallback branch: the absolute representation gates passed and ADR-078 selected raw keys.
      Hash-plus-catalog remains comparison evidence only, not an activation option or selection threshold.

## 4. Closeout

- [ ] 4.1 Update vocabulary, index-reference, KV Twofer, and operator docs with the selected key shapes and
      filters.
- [ ] 4.2 Run lint, race, contracts, real-NATS integration, structural + semantic e2e, and affected product suites.
- [ ] 4.3 If the pre-v1 wipe window closed before 3.1, halt: record the missed window in the ADR and re-file this
      change as a post-v1 migration proposal instead of executing a second wipe.
