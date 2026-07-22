## MODIFIED Requirements

### Requirement: A known-incomplete index defers regardless of lag
A known-incomplete index (`FailedCount > 0`) SHALL report `State = degraded`
regardless of revision lag, and the shared readiness projection
(`ComputeIndexStatus`) SHALL enforce this through a `FailedCount` input evaluated
BEFORE the "ready wins" branch — so the `FailedCount > 0 → degraded` rule holds
even for a producer whose watermark has already reached the target (where `Ready`
would otherwise be `true`). When a required index write or delete has failed and
is not yet repaired the reverse index can report a smaller graph than exists, so
the `FailedCount → degraded` projection MUST be unconditional (not gated on
`Ready`). With coverage inert at the gate (ADR-085), `degraded` is the only signal
that withholds a read from a known-incomplete index: a projection gated on `Ready`
would leave that index serving silently truncated answers. `Ready` remains
coverage-accurate (a full-coverage index that also holds failures is still
covered); the health verdict lives in `State`, on which consumers gate.

#### Scenario: Failed required write with small lag is degraded, not building
- **GIVEN** a required index write failed (`FailedCount = 1`) and continuous write
  keeps `Lag` small
- **WHEN** the status is computed
- **THEN** `State` is `degraded` (not `building`)
- **AND** every consumer defers on the hard stop, at any view age

#### Scenario: Incompleteness defers on state, not on coverage
- **GIVEN** `FailedCount > 0` and `Lag > 0`
- **WHEN** a consumer evaluates the canonical gate
- **THEN** it defers with reason `hard_stop` on the `degraded` state — not
  because `Ready` is false, which defers no one

#### Scenario: A producer caught up with failures is degraded, not ready
- **GIVEN** a producer (e.g. graph-embedding) whose watermark advances on every
  terminal outcome, so `Indexed >= Target` while `FailedCount > 0`
- **WHEN** the status is computed
- **THEN** `State` is `degraded` — the `FailedCount` input wins over the "ready
  wins" branch — never `ready` over unusable coverage
- **AND** `Ready` may still be `true` (coverage is complete); consumers gate on
  `State`

## ADDED Requirements

### Requirement: The degraded envelope carries bounded failure detail
A producer that tracks per-entity failures SHALL report, on both the
`GRAPH_STATUS` envelope and its Prometheus gauges, enough bounded-cardinality
detail to distinguish a whole-dependency outage from a few persistently-failing
entities, WITHOUT placing any unbounded per-entity list on the watched key. The
envelope SHALL carry `failed_count`, a `failed_reasons` map from a fixed reason
enum to counts, and a `first_failure_at` timestamp (all additive and omitted when
zero, preserving wire compatibility). The producer SHALL expose a `failed` gauge
(current failed count) and a failures counter labeled by the same fixed reason
enum; the raw error message SHALL NOT be used as a metric label.

#### Scenario: Degraded distinguishes outage from poison entities
- **GIVEN** the dependency is down and every entity's write fails
- **WHEN** an operator reads the `GRAPH_STATUS` envelope
- **THEN** `failed_count` is high and `failed_reasons` is dominated by a single
  connectivity reason — distinguishable from a small stable `failed_count` under
  a content reason

#### Scenario: The watched key stays compact
- **WHEN** the failure-detail envelope is published on the heartbeat tick
- **THEN** it carries only bounded-cardinality aggregates (a count, a fixed-key
  reason map, a timestamp), never a per-entity list

#### Scenario: Failure reasons are a bounded metric label
- **WHEN** a failure is recorded
- **THEN** the failures counter increments under a value from the fixed reason
  enum, never under the raw error text
