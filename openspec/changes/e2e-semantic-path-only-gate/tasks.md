# Tasks

## 1. Inventory and design

- [ ] 1.1 Line-pinned inventory of the 44 semantic-variant stages, model producers, skip/env mechanisms, gateway
      request, timings (`inventory.md`, 366 pins at `2430ebd1`; § E corrected 2026-09-30: synthesis via
      `includeSummaries`, hits 30 < 50); independent review records `INVENTORY PASS` on PR #1425.
- [ ] 1.2 Design: D1-D8, the 41-row classification, R1-R4 (`design.md`); independent pre-owner review; owner rulings
      R1-R4 and acceptance of the design on #1117.
- [ ] 1.3 Spec deltas: MODIFIED requirement 1 + ADDED path-only requirement in `e2e-tiered-scenario`; seed
      `e2e-ladder` (Purpose + one requirement).

## 2. Delivery

- [ ] 2.1 `stage.quality` on the three rows (reason in each row comment), `withoutQuality` filter in `Execute`,
      `Details` record and `[PATH-ONLY]` print. Unit test beside `tiered_warn_only_stages_test.go` asserting I1-I3.
      Mutation evidence: remove the marker from one row → the test reds naming that stage; restore → green (`cp`
      backup + checksum).
- [ ] 2.2 `--path-only` flag and `E2E_PATH_ONLY` override in `cmd/e2e/main.go:176-187`, copied to `cfg.PathOnly` in
      the tiered case. Mutation evidence: delete the env read → `E2E_PATH_ONLY=1 ./e2e --scenario tiered --variant
      semantic …` logs `[…/44]`; restore → `[…/41]` (one local run each, or the unit test on the override if `main` is
      refactored to expose it).
- [ ] 2.3 `gatewayGlobalSearchQuery()` sends `includeSummaries: false` and `summarizeThreshold: 0` (document args +
      variables). Mutation evidence: unit test on the builder's variables (delete one key → red); measured on the
      PR's ladder run: `graphql_gateway_latency_ms` < 1000 under semantic, `graphql_gateway_search_hits` 30, strategy
      `graphrag`.
- [ ] 2.4 `e2e-ladder.yml`: job `e2e-semantic` per D5 replaces `e2e-semantic-measure` in the same commit; header
      `:21-30` rewritten. Evidence: `git grep -c 'MEASUREMENT ONLY' .github/` → 0; `git grep -n prev1-program
      .github/` → 0; the PR's ladder shows the job once with the artifact.
- [ ] 2.5 Docs per D8: `semantic.yml:8`; `02-e2e-tests.md:23,84,99,300-324,354`; `CLAUDE.md:52` = `AGENTS.md:52`;
      `README.md:180-183,203-212`, delete `:218-225`. Evidence: `go test ./internal/agentprofiles/` green;
      `git grep -n 'Pending: wiring' test/e2e/README.md` → 0; `git grep -n 'does not yet run the'
      docs/contributing/02-e2e-tests.md` → 0.
- [ ] 2.6 PR #1425's own ladder run: `e2e semantic (path-only)` green once; record its wall-clock, `[…/41]`, and the
      artifact's skipped list on #1117; replace the "~5 min" prediction in the three doc sites with the measured
      number.
- [ ] 2.7 Review through the reviewer contract; owner rulings R1-R4 applied as ruled; owner acceptance on #1117.

Landing tasks (archive, spec sync, ticks) live on the PR checklist per the #1230 ruling, not here.
