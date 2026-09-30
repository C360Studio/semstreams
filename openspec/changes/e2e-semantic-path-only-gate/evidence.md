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

Not unit-covered: the `if s.config.PathOnly` branch in `Execute` (it needs a live deployment for
`EffectiveTierAuthority`). Its wiring evidence is task 2.6's read of the `[PATH-ONLY]` line and the `[41/41]` counter
from the PR's ladder log, per design D1 / Risks.

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
config is unexported to `cmd/e2e`). Its evidence is the same task 2.6 log read as § 4's `Execute` branch: a
`[PATH-ONLY]` line and `[41/41]` exist only if the env reached the flag, the flag reached the config, and the config
reached `Execute`.
