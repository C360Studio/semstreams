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
- [x] 2.2 Identify any current predicate-membership-watch consumer; if one exists, define add/remove-only
      semantics; otherwise record watch behavior as a non-public operational property.
      **→ RESOLVED 2026-08-01 by enumeration: NO watch consumer exists, so the "otherwise" branch applies and
      predicate-index watch behavior is a non-public operational property.** Measured, not assumed: no
      `Watch`/`Subscribe` against `PREDICATE_INDEX` anywhere in non-test code. Every non-test toucher is
      graph-index's own internals (`component.go`, `owner_reconcile.go`, `predicate_index.go`, `query.go`) plus
      `graph/constants.go`, `graph/kvcatalog.go`, the package docs, and e2e helpers. Enumerated from the owning
      component rather than from a router or registration table.
- [x] 2.3 Approve ADR-078, selecting raw keys, superseding ADR-065's hash-plus-catalog clauses, and recording the
      NAME/CONTEXT/INCOMING codec-keep rationale. Representation acceptance does not complete task 1.3 or the
      production activation tasks below.

## 3. Cutover (inside the announced pre-v1 wipe)

- [x] 3.1 **→ MOVED TO gh#827 (2026-08-01):** the deployment execution is an operational event, not capability
      truth, and the cutover contract it implements is spec-resident (see 4.3). Original text follows.
      Include the raw PREDICATE_INDEX bucket in the announced wipe/reseed; initialize behind typed not-ready
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

- [x] 4.1 Update vocabulary, index-reference, KV Twofer, and operator docs with the selected key shapes and
      filters.
      — **SCOPE CORRECTED 2026-07-30 (owner ruling).** SemStreams' obligation is to note the
      breaking change and publish migration guidance; **conforming to the framework is the sister
      repo's job**, and further problems they hit become new issues in this queue. Guidance is
      published (see `docs/operations/31-sister-repo-cutover-checklist.md` and the per-contract
      guides); adoption is tracked on **gh#753** and does NOT gate this archive.
- [x] 4.2 **CLOSED 2026-08-01 on owner ruling, AFTER root cause — not a waiver in the dark.** The distinction
      is the whole point: the earlier line said HOLD precisely because the cause was undetermined, and a gate
      waived on an undetermined cause is how a real regression ships. gh#830 is now root-caused with
      measurements, and the cause is **environmental, not a defect in any path this change touches**.
      Instrumenting the failing call (gh#834) produced 82 samples: median **1.208ms**, max **3.505ms**, ZERO
      exceeding the 5s client budget; the exact 68-entity batch behind the failing probe took **2.696ms**. The
      work is ~1400x faster than its deadline, so the timeout was never marginal. The same run logged 18 LLM
      answer-synthesis timeouts from three co-located `seminstruct` services on a 12-CPU host — the tier
      saturates its host, and under that contention a 5s request/reply is occasionally missed even though the
      work inside it is 3ms. Two earlier hypotheses were disproved on the way (a NATS 2.12→2.14 bump, killed
      by an A/B/A; a marginal-timeout perf boundary, killed by the measurement above). Full tally, nothing
      rerun to green: **2 FAIL / 3 PASS across 5 runs**, with runs 2 and 4 identical configs and opposite
      results. Everything else in this gate passed on `602f5ceb`: lint exit 0 · vet plain + integration clean ·
      `-race ./...` 135 ok / 0 FAIL · `-race -tags=integration -p 2` 136 ok / 0 FAIL · contracts green · zero
      schema drift · `task e2e:structural` GREEN. Tier reliability is tracked as gh#830 + gh#769, and the
      latent client/server timeout inversion the investigation surfaced (NOT the cause here) as gh#833.
      Original gate text follows.
      Run lint, race, contracts, real-NATS integration, structural + semantic e2e, and affected product suites.
      Everything else passed on merged main `32995167`: `task lint` exit 0 · `go vet` plain AND
      `-tags=integration` clean · `-race ./...` 135 ok / 0 FAIL · `-race -tags=integration -p 2 -count=1 ./...`
      136 ok / 0 FAIL · contracts green · zero schema drift · `task e2e:structural` GREEN.
      **`task e2e:semantic` FAILED — exit 201, `validate-globalsearch-known-answer`, term "forklift":
      `GraphQuery.loadEntities: request entities failed: context deadline exceeded` → gh#830.**
      NOT rerun to green; filed on one empirical run. The failure is in globalSearch/loadEntities rather than the
      predicate-index path, and the same term graded `recall=1.00 (4/4)` two steps earlier in the same run — but
      whether that is a query-path regression this change touched is UNDETERMINED, and waiving a gate on an
      undetermined cause is how a real regression ships. The cheap discriminator (a run at the `8813270c` tag on
      a cleaned host) belongs to gh#830.
      **Un-hold ONLY when gh#830 resolves with a cause, not with a passing rerun.**
- [x] 4.3 If the pre-v1 wipe window closed before 3.1, halt: record the missed window in the ADR and re-file this
      change as a post-v1 migration proposal instead of executing a second wipe.
      **→ ARMED BY SHIPPING v1, and no longer dependent on anyone reading this line.** 3.1's execution moved to
      gh#827, which states the halt prominently. The condition is also spec-resident in this change's own delta —
      the requirement "A key-format cutover never serves mixed partial truth" carries *"If the pre-v1 wipe window
      has closed, this change MUST NOT execute; it converts to an explicit post-v1 migration proposal"* plus the
      scenario "a closed window halts the cutover" (**no second wipe is performed**). Leaving this task line open
      is now bookkeeping, not the mechanism.
