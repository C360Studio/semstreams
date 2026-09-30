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
arguments.** Per design D4 the `hitCount == 0` gate therefore stays semantic-scoped and the statistical zero is the
owner's question on #1117. With `summarizeThreshold: 0` auto-summarize is off (`graphrag.go:172-178`, `:861`), so this
zero is not the summarized branch (hits in `EntityIDs`); it is either the Tier-1 path returning no entities or the
Tier-2 community-text fallback matching nothing — the log does not discriminate. Observation only, not a cause: the
same run's `test-graphrag-global` (level 1, `tiered_statistical.go:264-272`) found 6 entities
(`graphrag_global_entities_found:6`); `test-http-gateway` queries level 0.

Side effect recorded: every tier task's `:e2e:check-ports` depends on `:e2e:clean` (`taskfiles/e2e/common.yml:27`), so
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
