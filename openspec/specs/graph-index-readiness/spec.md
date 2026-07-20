# graph-index-readiness Specification

## Purpose

Defines how graph-index reports readiness and how consumers gate on it. `Ready`
means exact ENTITY_STATES revision coverage (ADR-066) — the "empty result is an
authoritative not-found" license that exact/point-query consumers (the fusion
honesty envelope, reverse-index reads) depend on. Periodic, approximate,
whole-result-re-deriving consumers (community detection) may instead gate on a
bounded revision lag (`ReadyWithinLag`, ADR-082), so a continuously-written graph
clusters instead of deferring forever, while a degraded, reset-required, empty,
or over-lagged index still hard-defers. A known-incomplete index (a failed
required write) reports `degraded` regardless of lag.

## Requirements

### Requirement: Ready reports exact revision coverage
The `graph.index.query.status` `Ready` bool SHALL be true only when the index has
applied every committed ENTITY_STATES revision at query time (`target > 0 &&
indexed >= target`) AND no required index write is unresolved; consumers that
treat an empty result as an authoritative not-found (the fusion honesty envelope,
direct reverse-index reads) SHALL gate on `Ready` and never on bounded lag. This
change does NOT alter `Ready` or the wire fields.

#### Scenario: Ready stays exact under continuous write
- **GIVEN** a bucket under continuous write so `Lag > 0` at query time
- **WHEN** the fusion honesty envelope or a reverse-index read checks readiness
- **THEN** `Ready` is false and the consumer falls back (grep / not-ready error)
- **AND** no symbol written in the last `Lag` revisions is returned as an authoritative miss

#### Scenario: The status wire fields are unchanged for all consumers
- **GIVEN** any consumer of `graph.index.query.status`
- **WHEN** it reads the response
- **THEN** `Ready`, `IndexedRevision`, `TargetRevision`, and `Lag` carry the same
  values they carried before this change

### Requirement: A known-incomplete index defers regardless of lag
A known-incomplete index (`failedCount > 0`) SHALL report `State = degraded` regardless of revision lag. When a required index write or delete has failed and is not yet repaired the reverse index can report a smaller graph than exists, so the `failedCount → degraded` projection MUST be unconditional (not gated on `Ready`). This closes the hole where a bounded-lag consumer would treat a `building`-plus-small-lag known-incomplete index as runnable.

#### Scenario: Failed required write with small lag is degraded, not building
- **GIVEN** a required index write failed (`failedCount = 1`) and continuous write
  keeps `Lag` small so `Ready` is already false
- **WHEN** the status is computed
- **THEN** `State` is `degraded` (not `building`)
- **AND** any bounded-lag consumer defers (a `degraded` state is a hard stop for any tolerance)

#### Scenario: Existing exact consumers are unaffected by the unconditional projection
- **GIVEN** `failedCount > 0` and `Lag > 0`
- **WHEN** the fusion honesty envelope or a reverse-index read checks readiness
- **THEN** it sees `Ready = false` exactly as before (only the `State` label moves `building → degraded`)

### Requirement: View-rate readiness interpretation with hard stops
The status response SHALL expose a canonical `ReadyWithinLag(n)` interpretation
that returns true if and only if `State` is neither `degraded` nor `reset_required`
AND (`Ready` is true OR (`TargetRevision > 0` AND `Lag <= n`)); the `degraded` and
`reset_required` states SHALL remain hard defers under any tolerance, an empty
graph (`TargetRevision = 0`) SHALL never be reported ready by tolerance, and `n = 0`
SHALL be equivalent to the exact `Ready` gate for every state and lag.

#### Scenario: Building within tolerance is ready
- **GIVEN** `State = building`, `TargetRevision = 500`, `Lag = 5`, `n = 100`
- **WHEN** `ReadyWithinLag(n)` is evaluated
- **THEN** it returns true

#### Scenario: Degraded and reset_required are hard stops
- **GIVEN** `State = degraded` (or `reset_required`)
- **WHEN** `ReadyWithinLag(n)` is evaluated for any `n`
- **THEN** it returns false

#### Scenario: Empty graph is never ready by tolerance
- **GIVEN** `TargetRevision = 0` (empty / pre-enumeration), so `Ready = false` and `Lag = 0`
- **WHEN** `ReadyWithinLag(n)` is evaluated for any `n`
- **THEN** it returns false (equal to `Ready`), because `Lag = 0` here does NOT mean caught-up

#### Scenario: Zero tolerance equals exact Ready
- **GIVEN** `n = 0`
- **WHEN** `ReadyWithinLag(0)` is evaluated for any state, target, and lag
- **THEN** its result equals `Ready`

### Requirement: Community detection runs under bounded lag
Community detection SHALL gate its periodic tick on a configurable
`index_lag_tolerance` (ENTITY_STATES revisions, default 0) via `ReadyWithinLag`,
so that a continuously-written graph within tolerance clusters instead of
deferring forever, while a degraded index, a reset-required index, an empty
graph, or lag beyond tolerance still defers. The tolerance counts ENTITY_STATES
revisions (entity/node writes), each of which may carry many relationships, so
a partition under sustained write reflects a bounded steady-state lag — it does
not converge to zero; the bound trades a bounded fraction of stale edges for
liveness.

#### Scenario: Continuous write within tolerance clusters
- **GIVEN** continuous write with `Lag <= index_lag_tolerance`, `State = building`, and `failedCount = 0`
- **WHEN** the detection tick fires
- **THEN** community detection runs (no longer defers forever — the #590 fix)

#### Scenario: Broken, incomplete, empty, or over-lagged index still defers
- **GIVEN** `Lag > index_lag_tolerance`, OR `State ∈ {degraded, reset_required}`, OR `TargetRevision = 0`
- **WHEN** the detection tick fires
- **THEN** community detection defers

#### Scenario: Default tolerance preserves current behavior
- **GIVEN** `index_lag_tolerance = 0` (the code default)
- **WHEN** readiness is evaluated over any run
- **THEN** community detection runs exactly when the exact `Ready` gate would have

### Requirement: Clustering under lag is observable
When community detection runs with `Lag > 0`, the lag it ran at SHALL be
operator-visible — not only at debug level — so bounded-staleness clustering
cannot become silent staleness. The derivation lag SHALL be exposed as a metric
and surfaced at info level or on the detection stage/output.

#### Scenario: A stale partition is visible
- **GIVEN** `index_lag_tolerance = 200` and detection runs at `Lag = 150`
- **WHEN** an operator inspects metrics / logs / status after the run
- **THEN** they can determine that the last partition ran at `Lag = 150` (not only that it "ran")
- **AND** the signal is not confined to a debug log
