# Evidence: e2e-semantic-path-only-gate

Recorded by the implementing session on branch `claude/gh1117-semantic-gate` (base `8bae169b`). Every figure below is
reproducible from the command beside it; mutation checks use a `cp` backup and SHA-256 (`shasum -a 256`).

## § 1 Statistical `test-http-gateway` under D4's arguments (task 2.3, measured first per the 2026-09-30 ruling)

Tree: `8bae169b` plus the uncommitted builder change (`gatewayGlobalSearchQuery()` sending `includeSummaries: false`
and `summarizeThreshold: 0`); `task e2e:statistical` rebuilds `cmd/e2e/e2e` from the tree (`build:e2e` ran first in
the log). Host pre-check immediately before: `docker compose ls -q` → empty, `pgrep -fl e2e` → empty (after a 5 min
wait on a sister session's `run-integration-tests.sh`, which cleared at 08:47:38).

Command, 2026-09-30 08:48 CDT, macOS host:

```
task e2e:statistical > statistical-run.log 2>&1   # EXIT 0
```

Verbatim result lines:

```
[28/42] test-http-gateway completed in 5.890333ms
... graphql_gateway_index_not_ready_retries:0 graphql_gateway_latency_ms:5 graphql_gateway_readiness_wait_ms:5 graphql_gateway_search_hits:0 ...
```

(from the `msg="Scenario completed successfully" duration=55.29643225s metrics="map[...]"` line; the tier run was green,
42/42 stages.)

**Result: `graphql_gateway_search_hits` = 0, `graphql_gateway_latency_ms` = 5, under statistical, with D4's
arguments.** Owner ruling 2026-09-30 on #1117: the `hitCount == 0` gate is semantic only; the statistical zero is
filed as #1441 and recorded in `test-http-gateway`'s row comment. With `summarizeThreshold: 0` auto-summarize is off (`graphrag.go:172-178`, `:861`), so this
zero is not the summarized branch (hits in `EntityIDs`); it is either the Tier-1 path returning no entities or the
Tier-2 community-text fallback matching nothing — the log does not discriminate. Observation only, not a cause: the
same run's `test-graphrag-global` (level 1, `tiered_statistical.go:264-272`) found 6 entities
(`graphrag_global_entities_found:6`); `test-http-gateway` queries level 0.

Host-effect note: every tier task's `:e2e:check-ports` depends on `:e2e:clean` (`taskfiles/e2e/common.yml:27`), so
this run executed `e2e:clean`. The host had no compose stack up (pre-check above), and the clean's `docker volume rm`
steps printed no volume names in the log, i.e. removed none.

## § 2 Builder request shape (task 2.3, first half)

Test: `TestHTTPGatewayStageRequestsNoSummariesAndNoAutoSummarize` (`test/e2e/scenarios/http_gateway_readiness_test.go`),
reading the request body at the HTTP seam.

- Before the builder change (test only): red, `GraphQL document does not carry "summarizeThreshold: $summarizeThreshold"` (and the three sibling strings).
- Mutation: delete the `"summarizeThreshold": 0,` line from the builder.

```
BEFORE 7be553b0588bff591956e27f831803e85f7d7a83ce1291e60f7dcc94d74ebfc8  test/e2e/scenarios/http_gateway_readiness.go
--- FAIL: TestHTTPGatewayStageRequestsNoSummariesAndNoAutoSummarize (0.00s)
    http_gateway_readiness_test.go:308: variables.summarizeThreshold = <nil> (present false), want 0
FAIL
AFTER  7be553b0588bff591956e27f831803e85f7d7a83ce1291e60f7dcc94d74ebfc8  test/e2e/scenarios/http_gateway_readiness.go
ok  	github.com/c360studio/semstreams/test/e2e/scenarios	0.317s
```

## § 3 Empty-globalSearch gate, semantic only (task 2.3, second half)

Test: `TestHTTPGatewayStageEmptyGlobalSearchFailsOnlyUnderSemantic` feeds the stub gateway a served
`strategy: graphrag`, zero-entity envelope; subtest `semantic` wants an error naming "no entities", subtest
`statistical` wants none (the #1441 exemption). Both record `graphql_gateway_search_hits` = 0. Red before the check
was added (`stage passed on a served globalSearch with zero entities under semantic`).

Mutation A — delete the `if hitCount == 0 && s.effectiveVariant(result) == "semantic" { … }` block:

```
BEFORE e5e44734df0122ba1bb37c5a91a6ee5f119668f69241baadc8a14c4f0765215b  test/e2e/scenarios/validate_infra.go
--- FAIL: TestHTTPGatewayStageEmptyGlobalSearchFailsOnlyUnderSemantic (0.00s)
    --- FAIL: TestHTTPGatewayStageEmptyGlobalSearchFailsOnlyUnderSemantic/semantic (0.00s)
        http_gateway_readiness_test.go:349: stage passed on a served globalSearch with zero entities under semantic
FAIL
AFTER  e5e44734df0122ba1bb37c5a91a6ee5f119668f69241baadc8a14c4f0765215b  test/e2e/scenarios/validate_infra.go
ok  	github.com/c360studio/semstreams/test/e2e/scenarios	0.336s
```

Mutation B — drop the variant scope (`if hitCount == 0 {`), restored from the same backup:

```
--- FAIL: TestHTTPGatewayStageEmptyGlobalSearchFailsOnlyUnderSemantic (0.00s)
    --- FAIL: TestHTTPGatewayStageEmptyGlobalSearchFailsOnlyUnderSemantic/statistical (0.00s)
        http_gateway_readiness_test.go:353: stage failed under statistical, where the hit count is recorded, not asserted: GraphQL globalSearch returned no entities (strategy "graphrag", served)
FAIL
AFTER  e5e44734df0122ba1bb37c5a91a6ee5f119668f69241baadc8a14c4f0765215b  test/e2e/scenarios/validate_infra.go
```

(The checksum above is of the file while its comment still carried a placeholder for the issue number, before `#1441` was filed.)

## § 4 Declared quality set and filter (task 2.1)

Tests: `test/e2e/scenarios/tiered_path_only_test.go` — the set is exactly the three names, each with a reason, each a
semantic row (I1); `withoutPathOnlySkips` over the semantic list returns the 41 names in order and skips the three in
stage-table order; with the filter not applied the semantic list equals the 44 names dumped at `8bae169b` (I3);
structural and statistical lists unchanged with nothing skipped. Red before implementation (compile: `undefined:
pathOnlySkips`, `undefined: withoutPathOnlySkips`).

Mutation — delete `"validate-thematic-answer-eval"` from `pathOnlySkips`:

```
BEFORE c886943d8046d4dacde40ac5e91679f2ad2acb2bfa8f50a7412058b965118c36  test/e2e/scenarios/tiered.go
--- FAIL: TestPathOnlySkips_DeclaredSetIsExactlyTheThreeQualityRows (0.00s)
        	Error:      	elements differ
        	            	extra elements in list A:
        	            	 (string) (len=29) "validate-thematic-answer-eval"
--- FAIL: TestPathOnlySkips_SemanticListLosesExactlyTheDeclaredRowsInOrder (0.00s)
        	            	expected: []string{"validate-llm-enhancement", "validate-thematic-answer-eval", "validate-globalsearch-known-answer"}
FAIL
AFTER  c886943d8046d4dacde40ac5e91679f2ad2acb2bfa8f50a7412058b965118c36  test/e2e/scenarios/tiered.go
ok  	github.com/c360studio/semstreams/test/e2e/scenarios	0.352s
```

The `if s.config.PathOnly` branch first lived inline in `Execute`; review round 1 (M1) moved it into
`stagesToRun`, which is unit-tested — see § 9.

## § 5 `--path-only` / `E2E_PATH_ONLY` (task 2.2)

The env reads in `parseCommandLineFlags` moved unchanged into `applyEnvOverrides(flags, getenv)` (production passes
`os.Getenv`) so the new read is unit-testable; the task's alternative to two local Docker runs. Test:
`TestEnvOverridesPathOnlyFromNonEmptyEnv` (`cmd/e2e/path_only_test.go`): unset and empty → off, `1` → on. Red before
implementation (compile: `undefined: applyEnvOverrides`).

Mutation — delete the `E2E_PATH_ONLY` read:

```
BEFORE 3a310a1e83d40c3de9f4d08e3ad35cd4e9f0e53eb373eff718d93ea449d5244c  cmd/e2e/main.go
--- FAIL: TestEnvOverridesPathOnlyFromNonEmptyEnv (0.00s)
    --- FAIL: TestEnvOverridesPathOnlyFromNonEmptyEnv/one (0.00s)
        path_only_test.go:22: pathOnly = false, want true for env map[E2E_PATH_ONLY:1]
FAIL
AFTER  3a310a1e83d40c3de9f4d08e3ad35cd4e9f0e53eb373eff718d93ea449d5244c  cmd/e2e/main.go
ok  	github.com/c360studio/semstreams/cmd/e2e	0.299s
```

Not unit-covered: the one-line copy `cfg.PathOnly = flags.pathOnly` in `createScenario`'s tiered case (the scenario's
config is unexported to `cmd/e2e`). Its evidence is the same task 2.6 log read as § 4's `stagesToRun` call in `Execute`: a
`[PATH-ONLY]` line and `[41/41]` exist only if the env reached the flag, the flag reached the config, and the config
reached `Execute`.

## § 6 Ladder job (task 2.4)

`e2e-semantic` / `e2e semantic (path-only)` replaces `e2e-semantic-measure` in one commit; header rewritten.

```
$ ! git grep -q 'MEASUREMENT ONLY' -- .github/; echo $?
0
$ ! git grep -q prev1-program -- .github/; echo $?
0
$ actionlint .github/workflows/e2e-ladder.yml; echo $?
0
```

The job appearing once in the PR's ladder, its `[PATH-ONLY]` line, `[41/41]` and wall-clock are task 2.6 (CI facts).

## § 7 Docs (task 2.5)

```
$ cmp CLAUDE.md AGENTS.md && go test -count=1 ./internal/agentprofiles/
ok  	github.com/c360studio/semstreams/internal/agentprofiles	0.306s
$ ! git grep -q 'Pending: wiring' -- test/e2e/README.md; echo $?
0
$ ! git grep -q 'does not yet run the' -- docs/contributing/02-e2e-tests.md; echo $?
0
```

"~5 min" is the stated prediction; task 2.6 replaces it with the PR's measured wall-clock.
Outside D8's sites and left unedited: three stale `~90s` semantic figures, `README.md:189`, `test/e2e/README.md:17`,
`cmd/e2e/main.go:230` (the `--list` text).

## § 8 Pre-push gates (at `916e9d95`, tree clean)

```
gofmt -l .                                              → (no output)
go vet ./test/e2e/... ./cmd/e2e/...                     → ok
go test -race -count=1 ./test/e2e/... ./cmd/e2e/...     → ok github.com/c360studio/semstreams/cmd/e2e 2.384s (every package ok), exit 0
task lint                                               → exit 0 (last line: ok github.com/c360studio/semstreams/test/natsclient 0.581s)
go test -count=1 ./internal/agentprofiles/              → ok github.com/c360studio/semstreams/internal/agentprofiles 0.180s
openspec validate e2e-semantic-path-only-gate --strict  → Change 'e2e-semantic-path-only-gate' is valid
task spec:properties                                    → spec-properties: 464/464 citations resolve.
task check:push                                         → exit 0 (last line: [INTEGRATION] tests complete; 0 FAIL lines; tree clean after schema:generate)
```

## § 9 First path-only CI run, and review round 1 fixes

PR #1425's E2E Ladder run 36728438332 at `b9851bd2`, job `e2e semantic (path-only)`: success, 14:21:04Z → 14:25:21Z
(4m17s wall-clock). Verbatim lines from `gh run view 36728438332 --log --job <id>` (timestamps kept):

```
2026-09-30T14:24:31.2089628Z [PATH-ONLY] skipping 3 quality stages: validate-llm-enhancement, validate-thematic-answer-eval, validate-globalsearch-known-answer
2026-09-30T14:24:31.2090459Z [1/41] verify-components starting...
2026-09-30T14:24:39.6726031Z [28/41] test-http-gateway completed in 8.00012724s
2026-09-30T14:24:56.8485601Z [31/41] validate-community-structure completed in 17.111412483s
2026-09-30T14:24:57.3766404Z [41/41] verify-outputs completed in 1.406329ms
graphql_gateway_index_not_ready_retries:0
graphql_gateway_latency_ms:7998
graphql_gateway_readiness_wait_ms:8000
graphql_gateway_search_hits:30
```

The three skipped names occur on one log line only (the `[PATH-ONLY]` line; `grep -c` over the job log = 1), so none
has a `completed in` line or a `_duration_ms` metric. The two stages over 1 s are `test-http-gateway` (8.00 s, one
request, cause unattributed) and `validate-community-structure` (17.1 s, its community wait).

Review M1 — `stagesToRun(variant)` is what `Execute` runs; `TestStagesToRun_PathOnlySelectsTheFilteredList` drives it
with `PathOnly` false (44 names, nil skipped) and true (41 names, the three skipped, order kept). Mutation: remove the
`if s.config.PathOnly { stages, skipped = withoutPathOnlySkips(stages) }` call inside `stagesToRun`:

```
BEFORE f8d5f01f0a00c58920920d7d541588b60341f710ef6c792fcf6e58bc2a6b69e7  test/e2e/scenarios/tiered.go
--- FAIL: TestStagesToRun_PathOnlySelectsTheFilteredList (0.00s)
        	Error:      	Not equal: 
        	            	expected: []string{"validate-llm-enhancement", "validate-thematic-answer-eval", "validate-globalsearch-known-answer"}
        	            	actual  : []string(nil)
        	Messages:   	PathOnly skips the declared rows, in stage-table order
FAIL
AFTER  f8d5f01f0a00c58920920d7d541588b60341f710ef6c792fcf6e58bc2a6b69e7  test/e2e/scenarios/tiered.go
ok  	github.com/c360studio/semstreams/test/e2e/scenarios	0.367s
```

The `cfg.PathOnly = flags.pathOnly` copy in `cmd/e2e/main.go` stays without a unit test; its evidence is this run's
`[PATH-ONLY]` line and `[41/41]` counter, which exist only if `E2E_PATH_ONLY` reached the flag, the flag the config, and
the config `Execute`.

Gates after the round-1 fixes (tree clean, before push):

```
gofmt -l .                                              → (no output)
go vet ./test/e2e/... ./cmd/e2e/...                     → ok
go test -race -count=1 ./test/e2e/... ./cmd/e2e/...     → exit 0; last line ok github.com/c360studio/semstreams/cmd/e2e 2.832s
task lint                                               → exit 0; last line ok github.com/c360studio/semstreams/test/natsclient 0.589s
go test -count=1 ./internal/agentprofiles/              → ok github.com/c360studio/semstreams/internal/agentprofiles 0.172s
openspec validate e2e-semantic-path-only-gate --strict  → Change 'e2e-semantic-path-only-gate' is valid
task spec:properties                                    → spec-properties: 465/465 citations resolve.
actionlint .github/workflows/e2e-ladder.yml             → exit 0
```

## § 10 Second path-only CI run (head `0912fe29`) and review round 2

PR #1425's E2E Ladder run 36732308747 at `0912fe29`, job `e2e semantic (path-only)`: success, 14:51:50Z → 14:56:20Z
(4m30s wall-clock). This is the first CI run of the `stagesToRun` call in `Execute` and of the `len(skipped) > 0` print
guard (both from the round-1 fixes), so it is the wiring evidence for them and for the `cfg.PathOnly` copy. Verbatim
lines from `gh run view --job 109944885309 --log` (timestamps kept), then the scenario metrics:

```
2026-09-30T14:55:25.5802979Z [PATH-ONLY] skipping 3 quality stages: validate-llm-enhancement, validate-thematic-answer-eval, validate-globalsearch-known-answer
2026-09-30T14:55:25.5805154Z [1/41] verify-components starting...
2026-09-30T14:55:38.4238678Z [28/41] test-http-gateway completed in 10.014050339s
2026-09-30T14:55:56.1401671Z [31/41] validate-community-structure completed in 17.635691432s
2026-09-30T14:55:56.7799085Z [41/41] verify-outputs completed in 721.965µs
Scenario completed successfully duration=31.224628629s
graphql_gateway_index_not_ready_retries:0
graphql_gateway_latency_ms:10011
graphql_gateway_readiness_wait_ms:10014
graphql_gateway_search_hits:30
```

Compared with run 36728438332 (§ 9): job 4m17s → 4m30s, scenario 26.19 s → 31.22 s, `test-http-gateway` 8.00 s →
10.01 s (one attempt, zero not-ready retries both times), `validate-community-structure` 17.1 s → 17.6 s, hits 30 both.
The `[PATH-ONLY]` line occurs once; no `/44]` counter appears in the log.

Review round 2 (PR #1425 comment, 2026-09-30): APPROVE; M1-M4, N1-N2 verified fixed or honestly recorded; the M1 mutation
re-derived in a `git archive` copy (restore matched `7991775a…683471`); four nits folded into this commit (this section,
the second gateway sample in D4/D6/task 2.3, the proposal's cost line, the § 4 wording below, `CLAUDE.md:52`).
