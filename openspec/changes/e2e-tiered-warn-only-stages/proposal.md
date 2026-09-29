# Change: The tiered scenario's warn-only stages assert or leave the per-PR variant

## Why

Six stages of the tiered e2e scenario pass on every outcome: the failure each exists to detect is appended to
`result.Warnings` and the stage returns nil. The per-PR statistical job is green over them today, and in the
#1117 measurement (E2E Ladder run 36589370091, 2026-09-29) the same stages hid a variant-specific failure: NL
intent passed 5/5 under statistical and 0/5 under semantic, every probe at the 10 s client deadline, both green.
Issue #1426 carries the evidence table and the file:line of each cannot-fail. The owner placed it in
v1.0.0-beta.163 on 2026-09-29 so an honest per-PR gate exists before the beta.165 wave:
https://github.com/C360Studio/semstreams/issues/1426#issuecomment-5894112756.

## What Changes

- Each of the fourteen warn-only paths the inventory pins (`test-nl-path-intent`, `test-nl-temporal-intent`,
  `test-graphrag-local`, `test-graphrag-global`, `validate-anomaly-detection` ground truth,
  `validate-community-structure` ground truth, `validate-virtual-edges`, the zero-enhanced arm of
  `validate-llm-enhancement`, and the six the inventory sweep found in `tiered_structural.go`:
  `validate-rule-transitions`, `test-spatial-query`, `test-temporal-query`, `test-zone-relationships`,
  `test-predicate-list`, `test-predicate-stats`) either returns an error on the outcome it exists to detect, or
  leaves the per-PR variants' stage table with the reason recorded in the table comment. The owner absorbed the
  six on 2026-09-29 ("agree on 1 - let's fix the class while we are in here").
- The same, for the twelve more stages review round 1's sweep of every stage function found (owner, 2026-09-29,
  "q2 absorb"): `verify-index-population`, `verify-search-quality`, `verify-outputs`,
  `validate-bidirectional-traversal`, `validate-inverse-edges-materialized`, `validate-hierarchy-inference`,
  `validate-incoming-index-predicates`, `verify-entity-retrieval`, `validate-entity-structure`,
  `test-embedding-fallback`, `validate-processing`'s health arm, and `verify-entity-count`'s nil-client arm; and
  `validate-rules` waits, bounded by `ValidationTimeout`, for the thresholds it asserts ("q1 - wait but bound").
- Where a probe's failure is a deadline, the assertion distinguishes "timed out" from "returned nothing"; a
  deadline the CI runner cannot meet is filed with its measurement, never warned past.
- The per-PR variants (statistical; the #1117 path-only semantic run) are green on main after the change, or
  each red is a real defect filed with its cause.

## Boundaries

- `AssertionsRun` for the tiered scenario is not touched: #1222 (beta.165) owns the required-check evidence
  pattern, and the owner ruled 2026-09-27 that no parallel assertion-accounting issue is created. This change
  makes stages honest; #1222 makes the count honest.
- #1117 decides which stages the per-PR semantic job runs (the three quality stages leave behind a flag). This
  change lands first; PR #1425 rebases onto it. Both edit the stage table in `test/e2e/scenarios/tiered.go`.
- No model is made faster and no 10 s deadline changes without a measurement.
- Codex's in-flight #1222 design (branch `codex/gh1222-required-e2e-proof`) edits `cmd/e2e/main.go` and the e2e
  docs, not the scenario stages; the file sets are disjoint and stay so.

## Completion evidence

Fails-without-fix: with one restored warning path (a `cp` backup of the stage file, checksum recorded), the
per-PR tier stays green over the injected failure; with the fix, it goes red on the same injection. Recorded on
the PR, never in prose only.
