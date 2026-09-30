# Design: e2e-semantic-path-only-gate

Drafted by the project architect (read-only contract) from `inventory.md` (366 pins at `2430ebd1`) and written by the
coordinating session, 2026-09-30. Rulings needed are § Rulings needed; nothing there is decided here.

## Context

Issue #1117 (beta.163), draft PR #1425, base `2430ebd1`. Words that bind, verbatim: 2026-08-27 "The per-PR semantic
gate asserts the PATH, not the QUALITY"; 2026-09-29T16:14Z "path-only gate for 1117". The bullets under the latter
(three stages named — `validate-thematic-answer-eval`, `validate-llm-enhancement`'s 120 s wait,
`validate-globalsearch-known-answer` — "behind an env flag the ladder sets"; they stay in `:8b`/`:frontier`; ~5 min)
are the transcriber's reading, uncorrected by the owner. The issue body's scope box 3 says "an env flag the ladder
does not set" (opposite polarity); the later ruling governs (D2).

Two CI measurements: run 36589370091 (48 stages, 878/844 s) and run 36654106894 at `2430ebd1` (44 stages, 682/677 s;
scenario 9m25s/8m57s; the three quality stages 8m08s/8m40s of it). #1426 (`0121a535`) already made every per-PR stage
assert or leave.

## § 1 Premises (each measured; pins in inventory.md)

- P1 The only membership gate is `variants` (`test/e2e/scenarios/tiered.go:442-456`); none of the 8 env reads under
  `test/e2e`/`cmd/e2e` gates membership (inventory § Claimed gap).
- P2 A stage returning nil prints `completed in` and writes `<name>_duration_ms` (`tiered.go:498-500`); no skipped
  vocabulary exists (`git grep -i skipped test/e2e cmd/e2e` → agentic/throughput comments only).
- P3 `TieredConfig.Variant` is a json field (`tiered.go:74-76`) and the saved run carries `Config`
  (`cmd/e2e/main.go:762-764`).
- P4 Env pattern: non-empty `E2E_VARIANT`/`E2E_OUTPUT_DIR` override flags (`cmd/e2e/main.go:176-187`); scenario-level
  duration overrides live in `globalsearch_timeout.go:20-50`, whose `durationEnvOr` rejects `d <= 0` — the existing
  wait env cannot express "no wait".
- P5 `validate-llm-enhancement` (`tiered_semantic.go:474-560`): arms nil-client `:479`, `waitForCommunities`
  `:487/:491`, the wait `:211` (2 min; measured `llm_wait_duration_ms` 120236/120241), re-fetch `:506`, summaries read
  `:515` (H2), recorder arms `:535-553`. `validate-community-structure` (`tiered_statistical.go:399-505`) has the same
  `waitForCommunities` `:409`, the same summaries read `:422`, and records `communities_llm_enhanced` `:449-458,:470`
  without asserting on it.
- P6 Gateway: `needsCommunity` `graphrag.go:861-866`; `includeSummaries` → `enrichGlobalResponse` `:943-947` →
  `synthesizeQueryAnswer` `:2156`; requested-not-required lease → enrichment stripped, success `:700-722`; Tier-2 text
  fallback requires the generation `:979-982` (the readiness transient survives `includeSummaries:false`). Gateway
  maps `variables["summarizeThreshold"|"includeSummaries"]` (`gateway/graph-gateway/component.go:1421-1425`; schema
  `:1855`). Precedent `tiered_structural.go:1696,1710`. Hits 30 in both runs (below the 50 auto-summarize threshold, so
  the synthesis comes from `includeSummaries` alone).
- P7 Per-stage timings: job 109694542289, both runs (D6 table).
- P8 Ladder: concurrency `e2e-ladder.yml:36-38`; measurement job `:124-195`; statistical fixture tests `:101-119` run
  once there; no disk-reclaim step exists and `df` showed 84 GB free (#1117, 2026-09-29); images
  `ghcr.io/c360studio/*` (`docker/compose/tiered.yml:33,67,103,139`).
- P9 Post-stage: `validateSemanticRequirements` (`validate_search.go:509-551`) hard-fails on `semembed_available`
  false and on known-answer 0/N; `validateFallbackBehavior` is `semantic-fallback` only (`:477`).
- P10 Doc sites: `taskfiles/e2e/semantic.yml:8`; `docs/contributing/02-e2e-tests.md:23,84,99,300-324,354`;
  `CLAUDE.md:52` = `AGENTS.md:52` (guard `internal/agentprofiles/profile_contract_test.go:111-114`);
  `test/e2e/README.md:180-183,203-212,218-225`.
- P11 TODO gone in `614a4e65` (this branch, not main); `git grep prev1-program .github` → only `e2e-ladder.yml:127`
  (measurement job); `:15` "Epic D" is attribution, not tracking.
- P12 #1436: keyword-only classifier chain in every shipped config (`processor/graph-query/component.go:274`);
  path-intent probes are keyword-routed (`tiered.go:342-345`); nothing here depends on the LLM classifier.
- P13 Four tests call `getStagesForVariant(variant)` with one return (`tiered_warn_only_stages_test.go:264`,
  `graph_roundtrip_test.go:29,48,60`).
- P14 Spec: leave sentence `specs/e2e-tiered-scenario/spec.md:12-16`; scenario 3 `:36-39`; the
  `includeSummaries: false` SHALL `:45-46` already binds D4.
- P15 `validate-batch-read-reconciliation`'s score arm asserts similarity > 0 and descending order
  (`validate_batch_read.go:410-470`): shape, not value.

## § 1b Adopter seam (the ladder job; every PR pays it)

Surfaces: the job `e2e semantic (path-only)`; `E2E_PATH_ONLY`/`--path-only`; the results artifact. No sister reads
any of them (test-only; sisters are read-only inventory, none probed).

1. Must know (a contributor who has never opened the workflow): (a) the job exists, ~5 min, parallel with statistical
   (critical path ~4 → ~5 min); (b) its red is a framework path break, never model quality; (c) reproduce with
   `E2E_PATH_ONLY=1 task e2e:semantic` (Docker + ghcr access, same as today's `task e2e:semantic`); (d) it is not a
   required check until the owner's ruleset edit. (d) is the gap.
2. Do nothing: the job runs anyway; unset flag = full variant, so a forgotten flag costs time, never assertions (the
   polarity reason, D2); a red ignored while non-required merges — process-level silent loss, bounded by the
   merge-gate rule.
3. Find out: PR checks list (name carries "path-only") > job log `[PATH-ONLY] skipping 3 quality stages: …` and
   `[n/41]` counts > artifact `details.path_only_skipped_stages` > docs.
4. Should know: only (b). Gap = (d), owner's edit outside the tree; recorded on #1117, not designed around.

Prefer observation to prediction: the table declares which rows are quality (a fact the framework owns); nothing asks
the adopter to predict a value. `timeout-minutes` is the one prediction, from measurement with stated margin (D5).

## Goals / Non-Goals

**Goals:** the ladder runs the default semantic variant once per PR with the three quality stages skipped and
recorded; every staying stage classified; `test-http-gateway` a path probe; measurement job deleted; docs say what the
per-PR run skips.

**Non-Goals:** `AssertionsRun`/evidence count (#1222); #643/#769; quality bars; the LLM classifier (#1436); the
ruleset edit; re-measuring.

## Decisions

**D1 Skip mechanism — stage-table marker, filtered in `Execute`, recorded as skipped.** `stage` gains `quality bool`;
the three rows set it with the reason in the row comment (the #1426 convention, P14 scenario 3). `Execute`
(`tiered.go:599`) does `stages := s.getStagesForVariant(variant); if s.config.PathOnly { stages, skipped =
withoutQuality(stages) }`, writes `result.Details["path_only_skipped_stages"] = skipped` and
`["path_only_skip_reason"] = "quality stage: the outcome is the small model's (#1117 path-only); runs in task
e2e:semantic (full), :8b, :frontier"`, prints `[PATH-ONLY] skipping N quality stages: …`. A skipped stage never
enters `executeStages`, so no `completed` line and no `_duration_ms` (P2). `getStagesForVariant` keeps its signature
(P13). Alternatives: in-stage early return — the stage records as completed with a duration (P2), three copies,
rejected; a fourth variant name (`semantic-path`) — `semantic-fallback` shows the cost: variant string compared at
`validate_search.go:28,518`, `cmd/e2e/main.go:405,410,764`, `config.EffectiveTierAuthority` (`tiered.go:590`), plus
`"semantic-path"` added to 44 `variants` lists — a second spelling of "semantic", rejected.

**D2 The flag — `--path-only` / `E2E_PATH_ONLY` → `TieredConfig.PathOnly`; the ladder sets the env; the taskfile
exposes nothing new but the `desc`.** Read in `cmd/e2e/main.go`'s override block beside `E2E_VARIANT` (P4; non-empty
= on, like its siblings), copied to `cfg.PathOnly` in the tiered case (`:396-410`), serialized with the run (P3) — the
results self-describe. Polarity: unset = full variant, per the 2026-09-29 ruling ("a flag the ladder sets"); reasons
beyond "later governs": `:8b`/`:frontier` need no edit, and a forgotten flag yields more evidence, never less.
Consequence: local `task e2e:semantic` stays the full ~12 min run; `E2E_PATH_ONLY=1 task e2e:semantic` is the CI
shape, one command (task inherits the process env). Alternatives: scenario-level `os.Getenv` like `SEMSTREAMS_E2E_*`
— those tune patience, this changes what the run is, and it would not reach the results `Config`; a `:path` task
target — duplicates the 7 compose lines (`semantic.yml:14-21`) and can drift from `default`; a `PATH_ONLY` VAR — a
second name for the same env.

**D3 `validate-llm-enhancement` — the whole stage leaves under path-only.** Path arms (client, `waitForCommunities`,
summaries read) are each duplicated by `validate-community-structure`, which stays (P5), so no path evidence is lost;
the wait is 148 s of the stage's 148 s; a wait-only skip would add an in-stage branch and record enhanced≈0/pending=N
as a measurement of nothing. Path = bucket readable (kept via community-structure); quality = summaries enhanced by
the model (leaves). The transcribed bullet names only the wait → R1.

**D4 `test-http-gateway` — `gatewayGlobalSearchQuery()` sends `includeSummaries: false` and `summarizeThreshold: 0`**
(document args + variables, `http_gateway_readiness.go:102-115`, gateway P6). `includeSummaries:false` removes the
synthesis (P6, the measured 18-56 s; the spec already requires it, P14); `summarizeThreshold:0` pins the
non-summarized branch so `Entities` (which the stage decodes, `:87-97`) is never nil (in the summarized branch
`EntityIDs` carries hits and the stage would read 0). Stays a path probe: request shape, status, GraphQL errors,
decode, `strategy == "graphrag"` (`graphrag.go:934`), hits recorded. Stops observing: community enrichment and answer
synthesis on the Tier-1 path (never decoded; on an unready generation today they are stripped, not errored,
`:700-722`). Still observes: the gh#1336 readiness transient on the Tier-2 fallback (`:979-982`). Under statistical
`test-graphrag-global` asserts summaries through the same handler (`tiered.go:400-406`). Expected: ms in both
variants. Alternative: a measured budget with margin — keeps a model cost on a path stage; rejected.

**D5 Ladder job `e2e-semantic` / name `e2e semantic (path-only)`**, replacing `e2e-semantic-measure` in the same
commit. Shape from the measurement (P8): `runs-on: ubuntu-latest`; `permissions: contents: read, packages: read`;
`env: E2E_PATH_ONLY: "1"`; steps checkout@v5 → setup-go@v6 (cache) → install task v3.53.1 → ghcr login
(docker/login-action@v3, `GITHUB_TOKEN`) → `scripts/e2e-reserve-ports.sh` → `task e2e:semantic` (once) →
upload-artifact@v4 `if: always()`, name `e2e-semantic-path-only-results`, path `cmd/e2e/test/e2e/results`. Dropped:
runner-shape, separate pull (compose pulls; it existed to time the pull), second run, `[MEASURE]` echoes. Not added:
disk reclaim (never a step; 84 GB free), statistical's fixture tests (run once in the job they protect). Login kept:
present in every measured run, so not proven necessary by a failure; images are ghcr (P8); dropping it is a later
one-line experiment. `timeout-minutes: 20`: expected ≈ 682 s − 488 s (quality stages) − 18 s (D4) + ~60 s pull ≈ 4-5
min, so 20 is ~4×, and it exceeds one full-variant run (~13 min) so a flag that fails to take effect finishes and is
diagnosed from the artifact instead of a kill. Concurrency: the workflow group (`:36-38`) already covers the new job.
Header comment `:21-30` rewritten to state the job/flag/stages. Not required in the ruleset: its first runs (PR
#1425's own, then the first ~5 PRs after merge) are the flake and wall-clock sample, read from the job durations and
the artifact's `path_only_skipped_stages`/`_duration_ms`, recorded on #1117 for the owner's ruleset edit.

**D6 Staying stages (41 of 44)** — path / quality / RECORDER; cold / warm from P7:

| Stage | Class | Cold / warm | Basis |
|---|---|---|---|
| verify-components | path | 1 / 2 ms | component list |
| send-mixed-data | path | 24 / 12 ms | UDP send |
| validate-processing | path | 26 / 24 ms | health |
| wait-for-embeddings | path | 455 / 261 ms | queue drain; semembed HTTP health (`validate_infra.go:256`), vectors never read |
| validate-embedding-queue-health | path | 12 / 18 ms | queue metrics |
| wait-for-entity-stabilization | path | 40 / 30 ms | count |
| graph-roundtrip | path | 1.1 / 1.9 s | trace |
| validate-hierarchy-inference | path | 16 / 14 ms | containers |
| verify-entity-count | path | 9 / 8 ms | KV |
| verify-entity-retrieval | path | 6 / 5 ms | KV |
| validate-entity-structure | path | 8 / 5 ms | KV |
| verify-index-population | path | 78 / 68 ms | indexes |
| test-pathrag-sensor | path | 11 / 6 ms | PathRAG |
| test-pathrag-boundary | path | 5 / 4 ms | PathRAG |
| test-pathrag-document | path | 7 / 5 ms | PathRAG |
| test-entityid-hierarchy | path | 46 / 36 ms | gateway |
| test-entities-by-prefix | path | 9 / 7 ms | gateway |
| test-spatial-query | path | 4 / 3 ms | index |
| test-temporal-query | path | 7 / 4 ms | index |
| test-zone-relationships | path | 7 / 4 ms | index |
| validate-partition-colocation | RECORDER (declared) | 11 / 14 ms | level-0 partition, LPA, no model (`validate_partition_colocation.go:133-148`); records without its B0 pair |
| test-nl-path-intent | path | 85 / 96 ms | keyword-routed, `includeSummaries:false` (P12) |
| test-entity-by-alias | path | 3 / 3 ms | index |
| test-predicate-list | path | 10 / 9 ms | index |
| test-predicate-stats | path | 10 / 15 ms | index |
| test-predicate-compound | path | 6 / 7 ms | index |
| verify-search-quality | path (hits arm) + RECORDER (known-answer, avg score; #1426) | 62 / 96 ms | ruled #1426 |
| test-http-gateway | path (after D4) | 18.7 / 27.5 s → ms | P6 |
| validate-gateway-response-shape | path | 169 / 153 ms | 3 probes, no globalSearch (`gateway_response_shape.go:57-77`) |
| test-embedding-fallback | path | 4 / 2 ms | reads `semembed_available` (health) + component health (`validate_infra.go:398-433`) |
| validate-community-structure | path (exists, non-singleton) + RECORDER (ground truth) | 26.9 / 14.1 s | community wait; reads `communities_llm_enhanced`, unasserted → R2 |
| validate-authoritative-hierarchy-provenance | path | 42 / 55 ms | KV |
| validate-incoming-index-predicates | path | 4 / 5 ms | index |
| validate-bidirectional-traversal | path | 4 / 6 ms | index |
| validate-inverse-edges-materialized | path | 4 / 5 ms | index |
| validate-batch-read-reconciliation | path | 109 / 187 ms | score > 0 and descending (P15) |
| validate-virtual-edges | path | 2 / 3 ms | engine off |
| wait-for-rule-stabilization | path | 210 / 220 ms | metrics |
| validate-rules | path | 14 / 24 ms | metrics |
| validate-metrics | path | 3 / 5 ms | scrape |
| verify-outputs | path | <1 / 4 ms | components |

Leaving: `validate-llm-enhancement` 148.4/147.9 s, `validate-thematic-answer-eval` 284.0/288.0 s,
`validate-globalsearch-known-answer` 55.9/84.2 s. Post-stage reader outside the table: `validateSemanticRequirements`
known-answer 0/N (P9) → R3.

**D7 Scope box 5** — done on this branch (`614a4e65`, P11). The baton pointer left with the TODO; its last echo is
the measurement job's comment (`:127`), deleted with the job (D5); `:15` stays as attribution. After D5:
`git grep -n prev1-program .github/` → 0.

**D8 Docs** — `semantic.yml:8` desc: "Semantic tier: neural embeddings + LLM (~12 min on a 4-vCPU CI runner,
2026-09-30); `E2E_PATH_ONLY=1` skips the three quality stages (validate-llm-enhancement, validate-thematic-answer-eval,
validate-globalsearch-known-answer) — the per-PR ladder shape, ~5 min (gh#1117)". `02-e2e-tests.md:23` and `:84`
carry the same two figures; `:99` gains "(per PR: path only; quality rows in `:8b`/`:frontier`)"; `:300-324` § CI
Integration rewritten from the workflow (three jobs, which are required); `:354` → "The per-PR ladder runs the
semantic path-only job on every PR (gh#1117); agentic is gh#769." `CLAUDE.md:52`: `# semantic ~12m (~5m path-only:
E2E_PATH_ONLY=1, the per-PR ladder shape), agentic ~5m35s, all = every tier in sequence`, copied to `AGENTS.md`.
`README.md:180-183` job table gains the row; `:203-212` drops `task e2e:semantic` from local-only (adds the env
note); `:218-225` § Pending deleted. The predicted "~5 min" is replaced by the PR's own first run (task 2.6).

## Risks / Trade-offs

- [Risk] The env does not reach `./e2e` (task/runner) → the full tier runs inside 20 min; visible as `[n/44]` and a
  missing `path_only_skipped_stages`. → Unit test on `withoutQuality` (marker); task 2.6 reads `[…/41]` from the PR's
  job log.
- [Risk] D4 stops exercising enrichment on the Tier-1 path in both variants. → Never asserted; statistical
  `test-graphrag-global` covers it; Tier-2 transient intact (P6).
- [Risk] Per-PR ML stack (2 GB ghcr pulls). → Measured 30-54 s; `cancel-in-progress`; 84 GB free.
- [Risk] Flake class (#643) in a job that will become required. → Non-required first; sample recorded on #1117; no
  rerun-to-green once required.
- [Risk] `validate-community-structure` becomes the first `waitForCommunities` (one detection cycle). → ≤ 90 s
  ceiling (`tiered_semantic.go:67`); recorded in its `_duration_ms`.

## Migration Plan

None: additive CI, no exported surface, no sister impact; `E2E_PATH_ONLY` unset = today.

## Open Questions (deferrable)

- The job's measured wall-clock (filled by PR #1425's own ladder run, task 2.6).
- Whether the ghcr login is load-bearing (later one-line experiment).

## Rulings needed (cheaper alternative first)

- R1 `validate-llm-enhancement`: whole stage leaves (D3, no new branch, path arms duplicated in community-structure)
  vs wait-only (in-stage branch; records enhanced≈0). The transcribed bullet says "120 s wait".
- R2 `validate-community-structure` reads COMMUNITY_SUMMARIES to record `communities_llm_enhanced` (unasserted;
  `tiered_statistical.go:449-458`): leave it with a row-comment sentence ("records, asserts nothing on it") vs nothing
  cheaper exists.
- R3 `validateSemanticRequirements` hard-fails on known-answer 0/N (`validate_search.go:534-540`), a post-stage reader
  of the semantic ranker: leave it (fires only when every known answer misses — 7/7 in both runs; a total miss under
  hybrid BM25+embedding is the search path, #1426's reasoning for the hits arm) vs gate the clause on `!PathOnly` (a
  branch).
- R4 The README-vs-workflow job-name contract test (owner offer 2026-08-27, "owner's call"): not in this change (one
  table row edited by task 2.5) vs file separately.

## Skills, invariants, tests

kv-or-stream, orchestration-check, new-payload: not triggered (a filter; no path, no multi-step, no payload).
query-pattern: D4 uses the admitted `globalSearch` args (`component.go:1855`); no new access.
Invariants (named examples suffice; one fixed table × boolean, PBT decision per 01-testing.md): I1
`stages(semantic, PathOnly) == stages(semantic) − quality rows`, order kept → ADDED req. scenario 1; I2 a skipped
stage has no `_duration_ms` and is listed → scenario 2; I3 flag unset = today's 44 → scenario 3.
