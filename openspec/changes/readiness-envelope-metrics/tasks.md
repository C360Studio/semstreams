# Tasks: readiness-envelope-metrics

## 1. Implement

- [x] 1.1 graph-index: add readiness gauges (`readiness` 0/1, `lag`,
      `indexed_revision`, `target_revision`, `state` gaugevec one-hot) to
      `processor/graph-index/metrics.go` via the existing `RegisterGauge`/
      `RegisterGaugeVec` plumbing; freshen on a periodic tick calling
      `computeIndexStatus` (reuse the repair ticker at `component.go:991` if
      clean, else a dedicated status tick). Optionally set `indexed_revision`
      on watermark advance for finer granularity.
- [x] 1.2 graph-embedding: the same gauges from its readiness/status compute
      (`processor/graph-embedding/readiness.go` + `metrics.go`), freshened on a
      tick.
- [x] 1.3 Tests: gauge values track a fabricated `IndexStatusResponse`
      (ready→1/lag→N/indexed/target; degraded/reset→state label); the tick sets
      them without a NATS query. Deterministic, no sleeps (drive the set-gauges
      helper directly). JSON/metric round-trip not needed (no operator config).
- [x] 1.4 Gates: `-race` unit + integration for both packages, `task lint`,
      `go build`, `go vet -tags=integration`, `task schema:generate` (no drift),
      semstreams-reviewer pass.

## 2. Spec

- [x] 2.1 Spec delta: readiness envelope exposed as metrics (this change).
