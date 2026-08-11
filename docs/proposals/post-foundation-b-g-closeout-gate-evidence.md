# Post-Foundation-B G closeout gate evidence

All timestamps are UTC. Every command below is fail-closed and exited 0; a nonzero exit would not count as evidence.

## Commit attribution

- Implementation commit `cbbc907e` contains the reviewed test-only semantic gateway timeout correction. The final
  statistical and semantic tiers ran on this commit.
- Archive candidate `344e58bf` is a documentation/OpenSpec-only descendant of `cbbc907e`. The timestamped fast/local
  gates and the agentic, research-graph, and deep-research tiers below ran on this candidate.
- Initial unprivileged lint/race evidence reruns hit sandbox Go-cache permission errors and are excluded. The
  escalated host reruns recorded below are authoritative.

## Archive-candidate local gates

- Lint: 2026-08-11T13:19:05Z–2026-08-11T13:19:09Z; `task lint`; exit 0. `go vet`, `go fmt`, revive,
  fixed-port guard, and request guard passed; request-guard `natsclient` completed in 0.527s.
- Full race: 2026-08-11T13:19:21Z–2026-08-11T13:19:41Z; `go test -race ./...`; exit 0. No `FAIL` result.
- Integration: 2026-08-11T13:19:49Z–2026-08-11T13:21:52Z; `task test:integration`; exit 0. Terminal
  `[INTEGRATION] tests complete`; `natsclient` completed in 94.246s.
- Schema generation: 2026-08-11T13:21:58Z–2026-08-11T13:21:59Z; `task schema:generate`; exit 0. Generated 33
  component schemas and 6 service OpenAPI specs.
- Generated drift: 2026-08-11T13:21:58Z–2026-08-11T13:21:59Z; `git diff --exit-code -- schemas specs`; exit 0. No
  generated schema/spec drift.
- Contract: 2026-08-11T13:18:29Z–2026-08-11T13:18:35Z; `go test ./test/contract/...`; exit 0. The package completed
  in 3.905s.
- Strict OpenSpec: 2026-08-11T13:18:29Z; `task openspec:validate`; exit 0. All 40 specs and changes passed.

## Archive-candidate E2E gates

- Agentic: 2026-08-11T13:22:12Z–2026-08-11T13:22:40Z; `task e2e:agentic`; exit 0. The scenario succeeded in
  569.427708ms with `tool_executions=1`, `trajectory_facts=10`, and governance 1/1.
- Research graph: 2026-08-11T13:22:49Z–2026-08-11T13:23:04Z; `task e2e:research-graph`; exit 0. The scenario
  succeeded in 1.040986792s with `research_classify_candidate_count=1`, `loops_completed_total=2`, and
  `orchestration_triples_total=17`.
- Deep research: 2026-08-11T13:23:11Z–2026-08-11T13:23:25Z; `task e2e:deep-research`; exit 0. The scenario succeeded
  in 118.218875ms with `loops_completed=7`, `evidence_entries=3`, and `coordinator_fan_out_decisions=1`.

## Implementation-commit E2E gates

### Statistical

- Commit: `cbbc907e`.
- Report interval: 2026-08-11T13:02:45.060102Z–2026-08-11T13:03:14.243545Z.
- Command: `task e2e:statistical`; exit 0.
- Hard results: `success=true`, `error_count=0`, 41 stages, 7/7 known answers, 29183ms scenario duration.
- The ephemeral raw JSON report was intentionally not committed; these embedded fields are the durable evidence.
- Raw report SHA-256: `d034d671d0ae550e22722773b4d8f56db1b468ac8a1c166c0c8e0a63476ac2df`.
- Metrics SHA-256: `b68b4c7834fc4823b1d96b0b8b5c20ff799471406a0dc7ee1788aeb1d585a75e`.

### Semantic

- Commit: `cbbc907e`.
- Report interval: 2026-08-11T12:43:52.997605Z–2026-08-11T12:56:48.757575Z.
- Command: `task e2e:semantic`; exit 0.
- Hard results: `success=true`, `error_count=0`, 48 stages, 7/7 known answers, 677432ms scenario duration,
  `test-http-gateway` 43600ms, and known-answer stage 88487ms.
- The authoritative terminal assertion reported exact strategy `graphrag` and 30 hits; the metrics counter reported
  `graphrag=10`. Optional GraphRAG warnings were not hard failures and are not represented as such.
- The ephemeral raw JSON report was intentionally not committed; these embedded fields are the durable evidence.
- Raw report SHA-256: `3a7fb7ff6dbdfc23af50c9dbc79414566868661923e461b619a6c43a3cd89b34`.
- Metrics SHA-256: `b98cf846af90febab4ed986f2377d8ccb6d58a9db913a0997c96990bb3324f5a`.

The first semantic attempt failed at the test caller's 10s timeout. Bounded correction `cbbc907e`, independently
reviewed before the final run, changed that caller to the existing 60s helper. The final semantic run above passed.

## Active-monitoring semantic replay

At 2026-08-11T13:49:48Z,
`git diff --exit-code cbbc907e..HEAD -- . ':(exclude)docs/**' ':(exclude)openspec/**'` exited 0. The replay therefore
used unchanged tracked runtime, test, and configuration content from corrected implementation `cbbc907e`; only
documentation and OpenSpec content differed. This is why `cbbc907e` remains the durable tested runtime identity.

- Outer command: `task e2e:semantic`; 2026-08-11T13:33:03Z–2026-08-11T13:48:42Z; exit 0.
- Scenario: 2026-08-11T13:35:10.090064Z–2026-08-11T13:48:23.181533Z; `success=true`, `error_count=0`, 48 stages,
  7/7 known answers, 793093ms duration, gateway 42995ms, and known-answer stage 84926ms.
- Hard final metrics: `global_search_known_answer_failures=0`, `probes=3`, `graphql_gateway_search_hits=30`,
  `validation_errors=0`, and `graphrag=10`.
- Replay report SHA-256: `b4c730a521dd60c2c15c3e46cfb3b61792ef3ca4a5c1c1c65bd4d4b380814591`.
- Metrics SHA-256: `b4a0881f6690c0fc37b1c2aa92049eac72b8594023ec0726162b7007d31e43ff`.
- Raw local reports were intentionally not committed; the exact durable fields are embedded above.

The stated abort policy was to terminate after either two consecutive approximately 45s readiness failures or 420s
with no E2E stage output and no authoritative counter movement. Neither condition occurred.

| UTC | Active authoritative observation |
|---|---|
| 13:36:37 | `/readyz` returned HTTP 200; embedding pending=0 and failed=0; output had reached stages 5–6. |
| 13:38:28 | `/readyz` returned HTTP 200; community summaries size=13, generated=8, failed=5; LLM enhancement advanced from an earlier size=5, generated=2, failed=3. |
| 13:39:30 | `/readyz` returned HTTP 200; community summaries size=17, generated=14, failed=7. |
| 13:40:15 | `/readyz` returned HTTP 200; generated=19 and `graphrag=1`; stage 22 thematic evaluation was active. |
| 13:41:01 | `/readyz` returned HTTP 200; generated=21, `graphrag=1`, and `semantic=1`. |
| 13:42:27 | `/readyz` returned HTTP 200; `graphrag=4`; counters continued moving during long stage 22. |
| 13:44:35 | `/readyz` returned HTTP 200; `graphrag=8`. |
| 13:45:21 | `/readyz` returned HTTP 200; `graphrag=8`, `pathrag=3`, `semantic=2`; stage 22 had completed and later stages were progressing. |
| 13:46:05 | `/readyz` returned HTTP 200; `graphrag=9`, `pathrag=3`, `semantic=2`, `temporal=2`; gateway and late stages were active. |
| 13:47:24 | `/readyz` returned HTTP 200; `entity_lookup=1`, `graphrag=10`, `pathrag=3`, `semantic=2`, `temporal=2`; known-answer stage was active. |
| 13:48:04 | `/readyz` returned HTTP 200; `entity_lookup` advanced from 1 to 3; known-answer stage was progressing. |
| 13:48:23–13:48:42 | Scenario completed 48/48 at 13:48:23; teardown completed and the outer command exited 0 at 13:48:42. |

Stage output independently advanced 6→21→22→31→39→41→48 between polls. Together with readiness and counter
movement, this supplies the active-monitoring proof for the long semantic gate.
