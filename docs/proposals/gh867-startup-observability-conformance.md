# GH-867 startup observability implementation conformance

Active authority: `gh867-startup-observability-design-amendment.md`, SHA-256
`0de8f9cc99444236cbc6c06af458e69e22373ca27fe28fa82ac57acdef3e87d2`.
Accepted inventory SHA-256:
`d594173ab170cb53d8496f048e50cbe2ec35357d61d63ccfe5955cc5fbd1e100`.
The amendment records and supersedes the affected clauses of the prior design.

## Owner rulings and constraints

| Ruling or constraint | Evidence | Result |
|---|---|---|
| Manager owns both boot diagnostic listeners | E1 | Conforms |
| Service lifecycle retains registration order | E2 | Conforms |
| Built-in Metrics keeps standalone compatibility | E3 | Conforms |
| Startup collector is private and freshly registered | E4 | Conforms |
| Component invocation and publication are causal | E5 | Conforms |
| Boot commitment is the final transition | E6 | Conforms |
| Stop clears commitment before child code | E7 | Conforms |
| Readiness remains exact and fail-closed | E8 | Conforms |
| Bind and failed-Start cleanup remain synchronous | E9 | Conforms |
| No public priority, lifecycle, or metric writer API | E10 | Conforms |
| Current specs are corrected without a change directory | E11 | Conforms |
| Adopter timing and ownership are documented | E12 | Conforms |
| Prometheus-bind rollback publishes stopping before cleanup | E13 | Conforms |

- E1: `service/service_manager.go:423`, `:1159`, `:631`;
  `service/startup_observability_test.go:400-440`.
- E2: `service/service_manager.go:443`, `:856`; `service/startup_observability_amendment_test.go:369`.
- E3: `service/metrics.go:120`, `:200`, `:305`; `service/startup_observability_amendment_test.go:369`.
- E4: `service/startup_metrics.go:16`, `:23`;
  `service/startup_observability_amendment_test.go:28`, `:53`, `:74`, `:80`, `:104`.
- E5: `service/component_manager.go:109`, `:485`, `:525`, `:557`, `:628`;
  `service/startup_observability_amendment_test.go:131`, `:270`.
- E6: `service/service_manager.go:465`, `:471`, `:474`, `:1220`, `:1252`;
  `service/startup_observability_amendment_test.go:198`, `:242`.
- E7: `service/service_manager.go:862`, `:950`;
  `service/startup_observability_amendment_test.go:152`.
- E8: `service/service_manager.go:1778`, `:1788`;
  `service/startup_observability_amendment_test.go:152`.
- E9: `service/service_manager.go:420`, `:423`, `:431`, `:646`, `:662`;
  `service/startup_observability_test.go:370`, `:467`.
- E10: `service/startup_observability_amendment_test.go:74`, `:80`.
- E11: `openspec/specs/framework-composition/spec.md:163` and
  `openspec/specs/service-composition/spec.md:256`; no `openspec/changes` entry exists.
- E12: `docs/operations/migration-startup-observability.md:1`,
  `docs/operations/09-http-middleware.md:56`, and `docs/operations/01-local-monitoring.md:121`.
- E13: `service/service_manager.go:662-666`;
  `service/startup_observability_test.go:467`, `:545-570`.

There are no implementation deviations from the approved amendment.

## TDD and verification evidence

| Gate | Evidence | Result |
|---|---|---|
| Amendment RED | V1 | Intended undefined private seams |
| Focused race | V2 | Passed |
| Focused integration race | V3 | Passed |
| Repository race | V4 | Passed |
| Lint | V5 | Passed |
| Generated-schema drift | V6 | Passed; no drift |
| Contract tests | V7 | Passed |
| Current-spec validation | V8 | 50 passed, 0 failed |
| Amended core process E2E | V9 | Passed |
| Prometheus-bind rollback RED/GREEN | V10 | Intended `starting` mismatch; passed after fix |

- V1: The scoped service test command failed before implementation on missing
  `newStartupMetricWriter`, component preparation/launch, and boot-commit
  symbols. The reflection assertion also observed the superseded exported
  `RecordStartupUnits` method.
- V2: `go test -race ./service ./metric ./cmd/semstreams`.
- V3: the focused integration command below.

  ```bash
  go test -race -tags=integration ./service \
    -run 'StartAll|Startup|Readiness|Metrics|BootFailsClosed'
  ```
- V4: `go test -race ./...`.
- V5: `task lint`.
- V6: `task schema:generate`, then `git diff --exit-code -- schemas/ specs/`.
- V7: `go test ./test/contract/...`.
- V8: `openspec validate --specs --strict --no-interactive`.
- V9: The root-supervised `task e2e:core` run passed on the exact amended tree.
  It verified all nine fixed startup series with service/component counts,
  exact `READY`, 12/12 health and heartbeat checks, 3/3 core scenarios,
  SIGTERM exit zero with listeners released and NATS healthy, the blocked-boot
  early-SIGTERM gate, and successful teardown.
- V10: The focused race test first failed because `/services.startup.status`
  was `starting` during blocked Prometheus-bind cleanup instead of `stopping`.
  After routing that rollback through `beginStopping("")`, the same command
  passed:

  ```bash
  go test -race ./service \
    -run TestMetricsBindFailureClosesSharedAndStartsNoLaterService -count=1
  ```

## Correction-propagation sweep

Production code, current specs, package docs, operator docs, metric docs, the
migration note, and tests were searched for the superseded Metrics-first
lifecycle order, planned reverse order, core/exported startup metric writer,
and promotion-before-publisher claims. No active-truth hit remains. The prior
design retains its historical text under an explicit amendment notice; the
approved amendment is authoritative for those clauses.
