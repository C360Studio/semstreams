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
The readiness envelope's `Ready` bool SHALL be true only when the index has
applied every committed ENTITY_STATES revision at compute time (`target > 0 &&
indexed >= target`) AND no required index write is unresolved; consumers that
treat an empty result as an authoritative not-found (the fusion honesty
envelope, direct reverse-index reads) SHALL gate on `Ready` and never on
bounded staleness. `Ready` semantics are unchanged bit-for-bit by this change;
what changes is distribution — the envelope lives in `GRAPH_STATUS` and the
former `graph.index.query.status` subject is removed — and one additive field
(`staleness_ms`); all pre-existing fields carry the same values as before.

#### Scenario: Ready stays exact under continuous write
- **GIVEN** a bucket under continuous write so `Lag > 0` at compute time
- **WHEN** the fusion honesty envelope or a reverse-index read checks readiness
- **THEN** `Ready` is false and the consumer falls back (grep / not-ready error)
- **AND** no symbol written in the last `Lag` revisions is returned as an authoritative miss

#### Scenario: Pre-existing envelope fields are value-compatible
- **GIVEN** any consumer decoding the envelope from `GRAPH_STATUS`
- **WHEN** it reads `Ready`, `State`, `IndexedRevision`, `TargetRevision`, and `Lag`
- **THEN** they carry the same values the request/reply envelope carried before
  this change; `staleness_ms` is purely additive

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
The canonical view-rate interpretation SHALL be bounded **staleness in time**:
a view-rate consumer proceeds if and only if `State` is neither `degraded` nor
`reset_required` AND (`Ready` is true OR (`TargetRevision > 0` AND
`staleness_ms <= max_staleness`)). The revision-count interpretation
`ReadyWithinLag(n)` is REMOVED with its only consumer (pre-1.0 break-now;
shipped one release, no known adopter configured it). An empty graph
(`TargetRevision = 0`) SHALL never be reported ready by tolerance, and
`max_staleness = 0` SHALL be equivalent to the exact `Ready` gate for every
state, target, and staleness.

#### Scenario: Building within staleness tolerance is ready
- **GIVEN** `State = building`, `TargetRevision = 500`, `staleness_ms = 1200`,
  `max_staleness = 3s`
- **WHEN** the view-rate gate is evaluated
- **THEN** it proceeds

#### Scenario: Degraded and reset_required are hard stops
- **GIVEN** `State = degraded` (or `reset_required`)
- **WHEN** the view-rate gate is evaluated for any `max_staleness`
- **THEN** it defers

#### Scenario: Empty graph is never ready by tolerance
- **GIVEN** `TargetRevision = 0` (empty / pre-enumeration)
- **WHEN** the view-rate gate is evaluated for any `max_staleness`
- **THEN** it defers

#### Scenario: Zero tolerance equals exact Ready
- **GIVEN** `max_staleness = 0`
- **WHEN** the view-rate gate is evaluated for any state, target, and staleness
- **THEN** its result equals `Ready`

### Requirement: Community detection runs under bounded lag
Community detection SHALL gate its periodic tick on a configurable
`max_staleness` (duration, default 0 = exact) via the canonical
`bounded-staleness` mode, so a continuously-written graph within the staleness
bound clusters instead of deferring forever, while a degraded index, a
reset-required index, an empty graph, an over-stale view, or an **unknown
status** still defers. The bound is wall-time and therefore invariant to write
rate and `coalesce_ms` (the gh#590 coalescer table showed a revision-count bound
shifts 2–4× with the coalesce dial alone). The former `index_lag_tolerance`
(revisions) config field is REMOVED — **BREAKING** for the .156 config surface;
no known deployment sets it.

#### Scenario: Continuous write within the staleness bound clusters
- **GIVEN** continuous write with fresh status, `State = building`, and
  `staleness_ms <= max_staleness`
- **WHEN** the detection tick fires
- **THEN** community detection runs

#### Scenario: Unknown status defers even with a generous tolerance
- **GIVEN** the status feed is stale (older than 3× heartbeat) and
  `max_staleness` is large
- **WHEN** the detection tick fires
- **THEN** detection defers with reason `status_unknown` (tolerance is never
  evaluated against unknown state)

#### Scenario: Default preserves the exact gate
- **GIVEN** `max_staleness = 0` (the code default)
- **WHEN** readiness is evaluated over any run
- **THEN** community detection runs exactly when the exact `Ready` gate would have

### Requirement: Clustering under lag is observable
When community detection runs with a stale view, the staleness it ran at SHALL
be operator-visible (metric + info-level or stage/output surface), and every
**defer** SHALL be attributable: the defer log line carries structured fields
(`status_known`, `status_age`, `state`, `lag`, `staleness_ms`,
`reason`, and the watch/bucket error when present) and a `defer_total{reason}`
counter distinguishes `hard_stop`, `over_staleness`, `status_unknown`, and
`empty`. Bounded-staleness clustering can never become silent staleness, and a
transport failure can never be mistaken for index state from the logs (the
gh#590 investigation cost three comment cycles because the defer line was a
bare constant).

#### Scenario: A stale partition is visible
- **GIVEN** `max_staleness = 3s` and detection runs at `staleness_ms = 1500`
- **WHEN** an operator inspects metrics / logs / status after the run
- **THEN** they can determine the last partition ran at ~1.5s staleness, not
  only that it "ran", and the signal is not confined to a debug log

#### Scenario: Defer reasons are countable and grep-able
- **GIVEN** a deployment where detection is deferring
- **WHEN** an operator reads `defer_total` by reason and any single defer log line
- **THEN** they can distinguish a broken index (`hard_stop`), an over-stale view
  (`over_staleness`), a dead status feed (`status_unknown`), and an empty graph
  (`empty`) without correlating multiple log lines

### Requirement: Read consumers retry the readiness transient
Reverse-index and by-name read handlers SHALL return the classified transient
`ErrorCodeIndexNotReady` while the index is catching up to ENTITY_STATES, and
consumers SHALL detect it via `errs.IsTransient` (never by message text) and
retry rather than treating it as a permanent failure. Readiness is sticky
(`indexBootstrapped`), so bounded retry converges; a consumer that wants a
self-serve bounded decision instead of retrying MAY gate on the envelope's
`IndexedRevision >= myRev` (ADR-066's finer contract), never on serving an
unmarked stale answer.

#### Scenario: A read arriving during catch-up is retryable
- **GIVEN** the index is catching up right after a write burst
- **WHEN** a reverse-index or by-name read arrives
- **THEN** it returns a classified `ErrorCodeIndexNotReady` transient
- **AND** a consumer that retries converges once readiness flips (sticky)

#### Scenario: The transient is programmatically detectable
- **GIVEN** a read consumer
- **WHEN** it inspects the error
- **THEN** `errs.IsTransient` classifies it without matching any message string

### Requirement: Fusion degrades consistently on the readiness transient
The fusion honesty envelope SHALL treat the readiness transient identically on
every core read path that lacks its own incompleteness marker: when `Resolve`,
`Entities`, or the **relations** neighbor expansion returns the classified
`ErrorCodeIndexNotReady`, `Fuse` SHALL return the empty-honest envelope
(`Ready=false`, carrying the current `IndexStatus`) — the same degrade as its
top-level `!Ready` gate — rather than propagating a hard error, and a `Ready=false`
envelope SHALL NOT carry `State="ready"`. Genuine, non-transient errors SHALL
still propagate. The facet walks (impact / paths / graph projection) are OUT of
scope: they carry their own per-facet honesty markers (`Truncated`; the graph
facet carries no coherence claim — see the fusion capability spec), so a
readiness transient there yields an honest lower-bound and is handled
identically to any other walk fault.

#### Scenario: A Resolve-path transient degrades, not errors
- **GIVEN** `Fuse`'s top `Ready` gate passed but `Resolve` hits the readiness
  transient in the narrow first-catch-up race under load
- **WHEN** `Fuse` handles it
- **THEN** it returns the empty-honest envelope (`Ready=false`), not a hard error
- **AND** the caller falls back exactly as it does on the top-gate `!Ready` path

#### Scenario: A genuine error still propagates
- **GIVEN** an internal read returns a non-transient error (e.g. a real decode or
  connection failure)
- **WHEN** `Fuse` handles it
- **THEN** it propagates the error (not degraded to an empty envelope)

### Requirement: The readiness envelope is exposed as Prometheus metrics
Every ADR-066 envelope producer (graph-index, graph-embedding) SHALL expose
the envelope as scrapeable Prometheus gauges in addition to the
`GRAPH_STATUS` KV key. At minimum the gauges are `readiness` (1 when Ready else
0), `lag` (revisions behind target), `indexed_revision`, and `target_revision`,
plus a `state`-labeled gauge distinguishing building / ready / degraded /
reset_required. The gauges MUST reflect the same values `computeIndexStatus`
returns and stay fresh independent of query traffic (refreshed on the same
periodic tick that publishes the KV key — one compute feeds both).

#### Scenario: Readiness and lag are scrapeable without a KV read
- **GIVEN** graph-index is running and catching up under continuous write
- **WHEN** Prometheus scrapes the component
- **THEN** the `readiness`, `lag`, `indexed_revision`, and `target_revision`
  gauges are present and reflect the current `computeIndexStatus` values
- **AND** no KV read is required to observe them

#### Scenario: State distinguishes catching-up from broken
- **GIVEN** the index is `building` with lag, versus `degraded` or `reset_required`
- **WHEN** an operator inspects the `state`-labeled gauge
- **THEN** the current state is identifiable (so "catching up" can be alerted
  differently from "broken"), not collapsed into `readiness=0`

#### Scenario: Metrics and the KV key stay in agreement
- **GIVEN** the periodic status tick
- **WHEN** the envelope is computed
- **THEN** the same struct is written to the gauges and to `GRAPH_STATUS`,
  never two divergent computations

### Requirement: Readiness is published as watchable KV state in a dedicated bucket
Every ADR-066 envelope producer (graph-index, graph-embedding) SHALL publish
its `IndexStatusResponse` JSON to its key in the dedicated `GRAPH_STATUS` KV
bucket (History 3; one key per producer; owned by the envelope producers,
separate from ENTITY_STATES and every graph-data bucket) on a fixed heartbeat
tick (the existing 5s status tick), unconditionally each tick so the write
doubles as a liveness heartbeat. The bucket SHALL be created by the producer at
Start, before any consumer binding is required. The former
`graph.index.query.status` request/reply subject and its handler are REMOVED
(**BREAKING**, clean break — no fallback poll path); the KV twofer replaces it:
`Get` for point-in-time probes, `Watch` for the event feed, history for the
trajectory. Readiness status is operational component state, NOT a graph write:
it never routes through graph-ingest and carries no ADR-055 semantic envelope.

#### Scenario: Consumers hold last-known readiness without polling
- **GIVEN** graph-index is running and publishing the envelope on its heartbeat tick
- **WHEN** a consumer watches the producer's status key
- **THEN** it receives the current envelope immediately on watch start and every
  subsequent update, and can gate decisions on held state with no per-decision
  NATS request

#### Scenario: A point-in-time probe is a KV Get
- **GIVEN** an operator or debug tool wanting current readiness
- **WHEN** it reads the producer's key (e.g. `nats kv get GRAPH_STATUS graph-index`)
- **THEN** it receives the same `computeIndexStatus` projection the gauges and
  watchers see (one compute feeds gauges and the KV publish)

#### Scenario: The removed status subject fails loudly, never silently
- **GIVEN** an unmigrated requester of the former `graph.index.query.status` subject
- **WHEN** it issues the request
- **THEN** it receives a no-responders transport error (loud), never a stale or
  fabricated envelope

### Requirement: Consumers distinguish not-ready from status-unknown
A readiness consumer SHALL judge status freshness by consumer-local arrival time
(no cross-process clock comparison): the held status is fresh while the time
since the last received update is within a bounded multiple (3×) of the
producer's heartbeat interval, and **unknown** otherwise. A fresh not-ready
status SHALL defer on its merits; an unknown status SHALL fail closed, with the
existing `allow_ungated_reads` escape for standalone deployments. The two
outcomes SHALL be distinguishable in logs and metrics (the gh#590 observer
discrepancy was a transport failure wearing not-ready's log line).

#### Scenario: A stalled status feed fails closed as unknown, not as not-ready
- **GIVEN** the producer stops publishing (crash, connection loss) while a
  consumer holds a last-known `Ready = true`
- **WHEN** 3× the heartbeat interval elapses with no update
- **THEN** the consumer treats readiness as unknown and fails closed
- **AND** the defer is attributed to `status_unknown`, not to index state

#### Scenario: A restarted consumer is fresh within one delivery
- **GIVEN** a consumer restarts while the producer is healthy
- **WHEN** its watch binds
- **THEN** the current value is delivered immediately and status is fresh without
  waiting a heartbeat

### Requirement: The envelope carries view staleness in time
The readiness envelope SHALL carry an additive `staleness_ms` field: `0` when
`Ready`, otherwise the age of the view — now minus the commit timestamp of the
newest fully-covered ENTITY_STATES revision (from KV entry timestamps, not
delivery-arrival times, so delivered backlog ages the metric). The field is a
floor: a revision not yet delivered to the producer cannot age it, and a total
stall is still surfaced by the wall-clock stuck-detector flipping `State` to
`degraded`. Wire compatibility: the field is additive; existing fields are
unchanged.

#### Scenario: Staleness reflects how old the served view is
- **GIVEN** continuous write with the watermark N revisions behind, the oldest
  covered revision committed at time T
- **WHEN** the envelope is computed at time `now`
- **THEN** `staleness_ms ≈ now − T` and grows if catch-up stalls, independent of
  write rate and `coalesce_ms`

#### Scenario: Caught up means zero staleness
- **GIVEN** `Ready = true`
- **WHEN** the envelope is computed
- **THEN** `staleness_ms = 0`

### Requirement: Consumers gate through the canonical readiness gate with a declared mode
Readiness gate semantics SHALL live in one canonical helper (in `graph`,
beside the envelope type) evaluated over the held status, its freshness, and a
declared per-consumer mode: `exact` (fresh `Ready` or defer/error — the fusion
top gate and `graph/query` client), `bounded-staleness` (no hard stop AND
(`Ready` OR `staleness_ms ≤ max_staleness`) — community detection),
`sticky-bootstrap` (exact until first pass, then open, local hard stops still
override — graph-index's own reverse-index query gate), and `degrade-honest`
(exact, with the caller degrading to an honest-empty result — fusion `Fuse`).
The hard stops SHALL hold under every mode and tolerance: `degraded`,
`reset_required`, and an empty / pre-enumeration graph (`TargetRevision = 0`)
always defer. Exact/point-query consumers SHALL keep gating on exact `Ready`
(the ADR-066 authoritative-absence license is unchanged), with
`IndexedRevision >= myRev` remaining the per-key escape hatch.

#### Scenario: Hard stops defer under every mode
- **GIVEN** `State ∈ {degraded, reset_required}` or `TargetRevision = 0`
- **WHEN** the gate is evaluated in any mode with any tolerance
- **THEN** it defers (or errors, per mode)

#### Scenario: Exact consumers are bit-compatible with today
- **GIVEN** the fusion top gate or a `graph/query` reverse-index read
- **WHEN** it gates through the canonical helper in `exact` mode
- **THEN** it proceeds exactly when the pre-change `Ready` check would have

#### Scenario: One gate, one semantics home
- **GIVEN** the four consumer call sites after adoption
- **WHEN** gate behavior must change
- **THEN** the semantics change in the canonical helper, not in per-consumer
  hand-rolled logic

