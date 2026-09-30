# Tasks

## 1. Inventory and design

- [ ] 1.1 Line-pinned inventory of the 44 semantic-variant stages, model producers, skip/env mechanisms, gateway
      request, timings (`inventory.md`, 366 pins at `2430ebd1`, 373 after round 1; § E corrected 2026-09-30: synthesis via
      `includeSummaries`, hits 30 < 50); independent review records `INVENTORY PASS` on PR #1425.
- [ ] 1.2 Design: D1-D8, the 41-row classification, R1-R7 (`design.md`); independent pre-owner review; owner rulings
      R1-R7 and acceptance of the design on #1117.
- [ ] 1.3 Spec deltas: MODIFIED requirement 1 + ADDED path-only requirement in `e2e-tiered-scenario`; seed
      `e2e-ladder` (Purpose + one requirement).

## 2. Delivery

- [ ] 2.1 `pathOnlySkips` (name → reason) beside the stage table naming the three rows, one pointer sentence in each
  row comment, `withoutPathOnlySkips` filter in `Execute`, the `[PATH-ONLY]` print. Unit test beside
  `tiered_warn_only_stages_test.go` asserting I1-I3 (set ⊆ table, exactly the three, order kept, non-semantic variants
  unchanged). Mutation evidence: delete one name from the set → the test reds naming that stage; restore → green (`cp`
  backup + checksum).
- [ ] 2.2 `--path-only` flag and non-empty `E2E_PATH_ONLY` override in `cmd/e2e/main.go:176-187` (set to `1` by the
  ladder and the `desc`), copied to `cfg.PathOnly` in the tiered case. Mutation evidence: delete the env read →
  `E2E_PATH_ONLY=1 ./e2e --scenario tiered --variant
      semantic …` logs `[…/44]`; restore → `[…/41]` (one local run each, or the unit test on the override if `main` is
      refactored to expose it).
- [ ] 2.3 `gatewayGlobalSearchQuery()` sends `includeSummaries: false` and `summarizeThreshold: 0` (document args +
      variables); `executeTestHTTPGateway` fails on `hitCount == 0` under the semantic variant. Mutation evidence:
      unit test on the builder's variables (delete one key from the builder → the test reds); unit test feeding a
      `strategy: graphrag`, zero-entity envelope to the stage's assertion under semantic (delete the `hitCount == 0`
      check from the stage → the test reds; restore → green). A local `task e2e:statistical` run with
      `includeSummaries:false` / `summarizeThreshold:0` recorded `graphql_gateway_search_hits` = 0 (evidence § 1)
      before the gate's scope was written. Final scope: semantic only; statistical measured 0 locally (evidence § 1),
      filed as #1441, recorded in the row comment (owner, 2026-09-30); a second unit case pins the statistical
      exemption. Measured on the PR's ladder run (36728438332), semantic: one attempt,
      `graphql_gateway_search_hits` 30, `graphql_gateway_latency_ms` 7998, `graphql_gateway_readiness_wait_ms` 8000,
      `graphql_gateway_index_not_ready_retries` 0, strategy `graphrag`; the ~8 s cause is unattributed (a cold semembed
      query embedding is a hypothesis, not verified); ~7× headroom against the 60 s client timeout.
- [ ] 2.4 `e2e-ladder.yml`: job `e2e-semantic` per D5 (no artifact step) replaces `e2e-semantic-measure` in the same
  commit; header `:21-30` rewritten. Evidence: `! git grep -q 'MEASUREMENT ONLY' -- .github/` and `! git grep -q
  prev1-program -- .github/` both exit 0; the PR's ladder shows the job once.
- [ ] 2.5 Docs per D8: `semantic.yml:8`; `02-e2e-tests.md:23,84,99,300-324,354`; `CLAUDE.md:52` = `AGENTS.md:52`;
      `README.md:180-183,203-212`, delete `:218-225`. Evidence: `go test ./internal/agentprofiles/` green;
      `! git grep -q 'Pending: wiring' -- test/e2e/README.md` and `! git grep -q 'does not yet run the' --
      docs/contributing/02-e2e-tests.md` both exit 0.
- [ ] 2.6 PR #1425's own ladder run: `e2e semantic (path-only)` green once; read from its log the `[PATH-ONLY]
  skipping 3 quality stages` line and the `[41/41]` counter (the wiring evidence for the `Execute` branch, beyond the
  unit test) and the job's wall-clock; record them on #1117; replace the "~5 min" prediction in the three doc sites
  with the measured number.
- [ ] 2.7 Review through the reviewer contract; owner rulings R1-R7 applied as ruled; owner acceptance on #1117.

Landing tasks (archive, spec sync, ticks) live on the PR checklist per the #1230 ruling, not here.
