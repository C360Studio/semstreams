# Tasks: bounded-lag-readiness-for-periodic-consumers

## 1. Contract + ADR (design gate)

- [x] 1.1 ADR-082 — decision/taxonomy recorded (`docs/adr/082-bounded-lag-readiness-for-periodic-consumers.md`,
      Accepted 2026-07-20): approximate whole-result consumers MAY gate bounded
      `Lag`; exact/point-query consumers MUST gate `Ready`; shared wire fields
      unchanged; one `State`-label honesty fix (`failedCount→degraded`
      unconditional); scoped "no in-place index-format migration"; G5 follow-up
      noted; mechanics kept in the spec, not the ADR (DONE)
- [ ] 1.2 Seed the `graph-index-readiness` capability spec (exact-`Ready`,
      unconditional `failedCount→degraded`, bounded-lag interpretation with hard
      stops + empty-graph guard, clustering-under-lag + observability; verified
      against `graph/index_status.go` + `pkg/revlag/watermark.go`).
- [x] 1.3 5-lens adversarial review + verify pass (DONE 2026-07-20). Verdict
      READY-WITH-CHANGES; architecture held (consumer-local seam, new ADR, G5
      deferral). Folded: BLOCKING failedCount-masked-as-building (→ unconditional
      degraded, task 2.1b); BLOCKING/HIGH empty-graph `n=0` divergence (→ target>0
      guard, task 2.1); HIGH clustering-under-lag observability (task 2.4);
      MEDIUM edge-staleness caveat + gh#474 scope (spec + 1.1); LOWs (bound N 2.2,
      ADR scope 1.1, fail-closed wording).

## 2. Implementation (DONE 2026-07-20 — semstreams-developer + semstreams-reviewer APPROVE-WITH-NITS; nits fixed; -race/lint/integration + e2e:statistical green)

- [x] 2.1 `graph/index_status.go`: add `IndexStatusResponse.ReadyWithinLag(n uint64) bool`
      = `State ∉ {degraded, reset_required}` AND (`Ready` OR (`TargetRevision > 0`
      AND `Lag <= n`)). Unit tests: building-within-lag true; degraded/reset false
      ∀n; **empty-graph (`target=0`) false ∀n**; `n=0` ≡ `Ready` across states
      INCLUDING the `target=0` row; `Lag` boundary at `n`.
- [x] 2.1b `processor/graph-index/watermark.go:90`: make `failedCount → degraded`
      **unconditional** (drop `&& status.Ready`). Test: `failedCount>0, Lag>0,
      building` → `degraded`; confirm exact consumers unaffected (Ready still
      false). Grep for any `State == "building"` branch before landing (expect none).
- [x] 2.2 `processor/graph-clustering/component.go`: add `IndexLagTolerance uint64`
      to `Config` (default 0, `schema:...,category:advanced`); rewire the parsed-
      status branch of `graphIndexReady()` (:1049) to
      `status.ReadyWithinLag(c.config.IndexLagTolerance)`; leave the
      unreachable/unparseable fail-closed `AllowUngatedReads` branches untouched.
      Validate/reject an absurd `N` (N ≪ graph size) so it can't defeat bootstrap
      gating. JSON round-trip test for the new operator-surface field.
- [x] 2.3 Integration test: firehose sim — bounded `Lag` within tolerance +
      `failedCount=0` → detection runs; `degraded`/`failedCount>0` → defers;
      `Lag > tolerance` → defers; empty graph → defers; `tolerance = 0` →
      bit-identical to the current `Ready` gate.
- [x] 2.4 Observability (HIGH): when detection runs with `Lag > 0`, surface the
      lag — a metric (e.g. `graph_clustering_index_lag_at_detection`) + info-level
      log and/or stamp on the `community_detection` stage/COMMUNITY_INDEX output.
      Not Debug-only. Test the metric/log fires.
- [x] 2.5 `-race` unit + integration, contract tests, `/preflight`;
      semstreams-reviewer pass. `task e2e:statistical` stays green (harness has
      write lulls — unaffected; confirms no regression at default 0).

## 3. Defaults + downstream (separate decisions)

- [ ] 3.1 Owner-DECIDED (2026-07-20): shipped continuous-load reference configs
      (`configs/statistical.json` et al.) carry a modest `index_lag_tolerance`
      (exact value from semboids' soak; code default stays 0). Land the config
      values with the implementation; changelog-note the shipped-behavior change.
- [ ] 3.2 semboids: set `index_lag_tolerance` in their config, re-soak; confirm
      #590 closed AND report whether a residual unbounded-lag / throughput issue
      (#480 family) remains to file separately.
- [ ] 3.3 Follow-up (file, don't build): graphview G5 (ADR-081) adopts the same
      `state-not-degraded AND (caught-up OR (target>0 AND lag <= n))` rule
      INCLUDING the empty-graph guard; at that point consider lifting
      `ReadyWithinLag` to `pkg/revlag` carrying the guard.
