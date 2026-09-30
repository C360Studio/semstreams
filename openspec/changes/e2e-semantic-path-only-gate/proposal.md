# Proposal: e2e-semantic-path-only-gate

Issue #1117 (milestone `v1.0.0-beta.163`), scope boxes 2–5, on draft PR #1425. Owner rulings on #1117: per-PR, not
nightly; path, not quality (2026-08-27); gate shape **path-only** (2026-09-29, verbatim "path-only gate for 1117").

## Why

The default semantic variant is the only tier that exercises the small-model path (embedding model, LLM query
classifier, answer model, community summarizer) and it runs in no per-PR workflow, so it drifts RED unnoticed
(#830: the globalSearch known-answer probe timed out in `loadEntities` and nobody saw it). Measured on `ubuntu-latest`
(E2E Ladder run 36589370091, scope box 1): 878 s cold and 844 s warm for one tier run, 12 min of scenario, 9.6 min of
it in three quality stages whose outcome no PR can change. #1426 (PR #1427, merged as `0121a535`) made every per-PR
stage assert the outcome it exists to detect or leave the per-PR variants, so the tier is now honest to gate on.

## What Changes

- The three quality stages the owner named leave the per-PR semantic run behind an env flag the ladder sets:
  `validate-thematic-answer-eval`, the whole `validate-llm-enhancement` stage (R1), and
  `validate-globalsearch-known-answer`. They stay in the `:8b` and `:frontier` variants, which run pre-tag. The
  flag's name, the skip mechanism (stage-table membership vs. an in-stage early return), and what the skipped stages
  record are design questions; the inventory (`inventory.md`) grounds them.
- Every remaining assertion in the default semantic run is classified **path** (the plumbing worked, framework-owned
  at every step) or **quality** (the small model's answer was good) against the inventory; anything quality-classified
  beyond the three named stages is an owner question, not a silent fourth skip.
- The `test-http-gateway` residual on this path (a 56 s globalSearch under semantic, measured 2026-09-29 on #1117:
  server latency, not a readiness poll) is fixed on the path stage so the gate does not pay a quality cost wearing a
  path stage's name; the candidate fix recorded on #1117 is `includeSummaries:false` + `summarizeThreshold:0` on that
  request.
- `e2e-ladder.yml` gains the per-PR job `e2e semantic (path-only)` in the shape the measurement supports (registry
  login, port reservation, timeout), and the `gh#1117 MEASUREMENT ONLY` job on this branch is deleted
  in the same commit. Expected cost about 5 min; ladder critical path from about 4 min to about 5.
- The dangling `TODO` about the semantic gate and its baton reference are deleted (scope box 5); the tracking is #1117.
- Docs: the tier table in `docs/contributing/02-e2e-tests.md` and the taskfile `desc` say which stages the per-PR
  semantic run skips and why; the ~90s figure is corrected in this change at every site.

No **BREAKING** change: no exported Go surface, no payload, no bucket, no port. The gate is additive CI.

## Capabilities

- **Modified Capabilities**
  - `e2e-tiered-scenario` (seeded by #1426): a requirement for the per-PR semantic run — its quality stages leave the
    run behind the flag, every stage that stays is path-classified, and a skipped stage is recorded as skipped, never
    as passed.
- **New Capabilities**
  - `e2e-ladder`: the per-PR E2E ladder as a capability — which variants run per PR, what each job sets, and what a
    green job is evidence of. `openspec/specs/` has no spec for the ladder today; `e2e-tiered-scenario`'s Purpose
    already says "which variants are per-PR is the E2E Ladder's decision", so this change is the ladder's first
    toucher and seeds it lazily, with only the requirements this change establishes (no backfill).

Which check the ruleset `main-required-checks` requires is an owner edit outside the tree (today: `CI Status Check`
and `e2e statistical`); whether the new job becomes required is an owner decision recorded on #1117, not a task here.

## Non-goals

- The nightly `e2e:semantic` + `e2e:agentic` run (#769) and the semantic-tier cache-control seam for determinism
  (#643): a per-PR gate raises the cost of flake, but hardening is their scope.
- Improving what the small model answers: the quality stages keep measuring it in `:8b` / `:frontier`; this change
  never raises or lowers a quality bar.
- Assertion accounting (`AssertionsRun`, the tiered scenario's evidence count): #1222, Codex's lane.
- The anomaly engine's disabled e2e leg: deletes with the engine under #620 (ruling D2 on #1426).
- Making `e2e semantic (path-only)` a required check: owner's ruleset edit, after the job has a run history.
- Re-measuring cold vs warm: done (scope box 1); the new job's first runs are its own measurement.

## Impact

- `test/e2e/scenarios/tiered.go` (stage table or per-stage skip), the three quality-stage functions, `cmd/e2e`
  (flag read, if the mechanism lives there), `test/e2e/scenarios/tiered_statistical.go` / `validate_search.go` for the
  `test-http-gateway` request.
- `.github/workflows/e2e-ladder.yml` (new job replaces the measurement job), `taskfiles/e2e/semantic.yml` (variant
  `desc` and any env the task passes), `docs/contributing/02-e2e-tests.md`, `CLAUDE.md` = `AGENTS.md` (one line,
  byte-identical, guarded by `internal/agentprofiles`).
- Consumers: no sem* product consumes the e2e tier; it is SemStreams' own gate. Sister repositories are unaffected;
  no migration note.
- Risk: the semantic job adds a Docker ML stack (seminstruct qwen3-1.7b and 0.6b, semembed) to every PR's ladder;
  runner disk and registry pull time are part of the job shape the measurement already exercised.
