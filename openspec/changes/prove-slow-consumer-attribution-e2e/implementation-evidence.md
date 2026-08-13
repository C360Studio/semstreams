# Implementation evidence

Owner accepted R1-R15 on 2026-08-13. No design deviation was taken.

## Binding ruling conformance

| Ruling | Evidence | Deviation |
|---|---|---|
| R1 — independent of #586 | E1 | None |
| R2 — ordinary core remains production target | E2 | None |
| R3 — tagged gate builds `cmd/semstreams` | E3 | None |
| R4 — one hook with tagged/default implementations | E4 | None |
| R5 — untagged behavior is inert | E5 | None |
| R6 — tagged auto-run, no communication/storage primitive | E6 | None |
| R7 — existing client; no client/logger/slog-handler default | E7 | None |
| R8 — use captured #961 logger graph | E8 | None |
| R9 — gate then delegate installed callback | E9 | None |
| R10 — raw pending controls remain tag-private | E10 | None |
| R11 — structured stdout plus existing counter | E11 | None |
| R12 — actual assertion count on success/failure | E12 | None |
| R13 — no arbitrary sleeps | E13 | None |
| R14 — separate per-PR short gate | E14 | None |
| R15 — OpenSpec proof; no ADR | E15 | None |

## Exact evidence

- **E1:** The fixed test-only limit is at `internal/e2eslowconsumer/probe_e2e.go:80`. No `natsclient`, config,
  schema, or public pending-limit change exists in the diff.
- **E2:** The existing target remains `docker/compose/e2e.yml:56`. Separate target isolation is asserted at
  `cmd/semstreams/slow_consumer_build_contract_test.go:12-26`. Ordinary core passed 3/3.
- **E3:** The tagged build is `docker/Dockerfile:180-194`. Command and anti-sister assertions are at
  `cmd/semstreams/slow_consumer_build_contract_test.go:23-25`.
- **E4:** The sole hook is `cmd/semstreams/main.go:341-350`. Mutually exclusive implementations are
  `cmd/semstreams/slow_consumer_probe_disabled.go:1-13` and
  `cmd/semstreams/slow_consumer_probe_e2e.go:1-14`.
- **E5:** The no-op accepts even nil at `cmd/semstreams/slow_consumer_probe_disabled.go:11-13`, proved by
  `cmd/semstreams/slow_consumer_probe_disabled_test.go:12-14`.
- **E6:** The tagged hook directly calls the probe at `cmd/semstreams/slow_consumer_probe_e2e.go:12-14`. Isolated
  compose activation is `docker/compose/e2e-slow-consumer.yml:18-31`. The diff adds no endpoint, config, control, or
  durable primitive.
- **E7:** The connected client passes directly at `cmd/semstreams/main.go:341-347`. The probe consumes
  `GetConnection` and the captured handler at `internal/e2eslowconsumer/probe_e2e.go:28-42`; it has no client,
  logger, or slog-handler construction.
- **E8:** The probe delegates the captured installed callback at
  `internal/e2eslowconsumer/probe_e2e.go:49-61,118-124`. Assembled JSON plus the exact-one existing counter is
  asserted at `test/e2e/scenarios/core_slow_consumer.go:182-209`.
- **E9:** The matching gate and unrelated immediate delegation are
  `internal/e2eslowconsumer/probe_e2e.go:49-61`. Exact callback restoration is
  `internal/e2eslowconsumer/probe_e2e.go:63` and proved at
  `internal/e2eslowconsumer/probe_e2e_test.go:34-43`.
- **E10:** The build tag is `internal/e2eslowconsumer/probe_e2e.go:1`. Raw queue and limit calls are
  `internal/e2eslowconsumer/probe_e2e.go:72-82`. The untagged root imports no fixture package.
- **E11:** Fixture-record selection and Prometheus parsing are
  `test/e2e/scenarios/core_slow_consumer.go:142-180`. Exact field and counter assertions are
  `test/e2e/scenarios/core_slow_consumer.go:182-209`. The isolated E2E passed three clean post-implementation runs.
- **E12:** The result field is `test/e2e/scenarios/scenario.go:48-49`. Dynamic increments are
  `test/e2e/scenarios/core_slow_consumer.go:182-215`. Runner reporting is `cmd/e2e/main.go:511-526`. Both paths are
  proved at `cmd/e2e/main_test.go:27-49` and `test/e2e/scenarios/core_slow_consumer_test.go:32-53`.
- **E13:** The probe uses context, channels, flush, and a ticker at
  `internal/e2eslowconsumer/probe_e2e.go:28-138`. The scenario uses a bounded context and ticker at
  `test/e2e/scenarios/core_slow_consumer.go:59-105`. The task uses compose `--wait` at
  `taskfiles/e2e/slow-consumer.yml:9-14`. The touched-source census found no sleep.
- **E14:** The parallel 10-minute job is `.github/workflows/e2e-ladder.yml:41-58`. The statistical job remains
  separate at `.github/workflows/e2e-ladder.yml:60-63` and passed locally.
- **E15:** The target delta is
  `openspec/changes/prove-slow-consumer-attribution-e2e/specs/nats-client-diagnostics/spec.md:5-32`. No ADR is in the
  diff.

## Reproducible gate evidence

Exact RED, GREEN, mutation, restoration, broad-gate, and product-gate commands and results are recorded in
`tasks.md`. Independent SemStreams implementation review returned `APPROVE` with no findings after verifying R1-R15,
the full diff, and focused tests including five consecutive tagged real-NATS race runs.
